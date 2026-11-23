// US-134 AC-9, put to the built binary in a sandbox: `sdlc gate implementation
// pass` runs the project's lint itself, is refused while it fails, naming it and
// the file that holds what it printed, and is taken once it passes, with the
// result recorded against the tree it ran on. internal/cli holds the rest of the
// story without a build.

package e2e

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// us134Lint is commands.lint judging the code by what it holds, so that the
// second run differs from the first because the code does.
const us134Lint = `if grep -q LINT-FAILS invoice.go; then echo "invoice.go:3: LINT-FAILS is not allowed"; exit 1; fi`

var us134StoryFile = regexp.MustCompile(`[^\s"'` + "`" + `]*\.sdlc[/\\]stories[/\\]US-001[/\\][^\s"'` + "`" + `]+`)

func TestAC9TheImplementationGateIsRefusedWhileLintFailsAndTakenWithItsResultOnceItPassesInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	setConfig(t, sb.repo, "commands", "lint", us134Lint)
	sb.mustGit(t, sb.repo, "add", "--all")
	sb.mustGit(t, sb.repo, "commit", "--quiet", "--message", "chore: choose a lint")
	sb.works(t, sb.repo, "", "", "start")
	for _, gate := range []model.Gate{model.GateDoR, model.GateAnalysis, model.GateTestsFrozen, model.GatePlan, model.GateDesignReview} {
		sb.worksGate(t, gate)
	}
	tree := sb.tree()

	sb.writesFile(t, "implementer", "invoice.go", "package main\n\n// LINT-FAILS\nfunc Invoice() {}\n")
	r := sb.does(t, tree, "", "", "gate", "implementation", "pass", "--note", "the work is done")
	if r.code == 0 {
		t.Fatalf("AC-9 the gate passed while commands.lint fails:\n%s", r.stdout)
	}
	if r.stdout != "" {
		t.Errorf("AC-9 a refusal wrote to stdout: %q", r.stdout)
	}
	for _, want := range []string{"SDLC-E0095", "commands.lint", "LINT-FAILS is not allowed"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("AC-9 the refusal does not say %q:\n%s", want, r.stderr)
		}
	}
	named := ""
	for _, found := range us134StoryFile.FindAllString(r.stderr, -1) {
		found = strings.TrimRight(found, ".,;:)")
		for _, path := range []string{found, filepath.Join(sb.repo, filepath.FromSlash(found)), filepath.Join(tree, filepath.FromSlash(found))} {
			if strings.Contains(readFile(t, path), "LINT-FAILS is not allowed") {
				named = path
			}
		}
	}
	if named == "" {
		t.Errorf("AC-9 the refusal names no file under .sdlc/stories/US-001/ that holds what commands.lint printed:\n%s", r.stderr)
	}

	sb.writesFile(t, "implementer", "invoice.go", sandboxCode)
	sb.works(t, tree, "", "", "gate", "implementation", "pass", "--note", "the work is done")
	var record struct {
		Gates map[string]struct {
			Status   string `json:"status"`
			TreeHash string `json:"tree_hash"`
			Commands []struct {
				Name string `json:"name"`
				Exit *int   `json:"exit"`
			} `json:"commands"`
		} `json:"gates"`
		Reviews []model.Review `json:"reviews"`
	}
	readJSON(t, filepath.Join(sb.repo, ".sdlc", "stories", "US-001", model.RecordFile), &record)
	gate := record.Gates[string(model.GateImplementation)]
	if gate.Status != string(model.GatePass) || len(gate.Commands) != 1 ||
		strings.TrimPrefix(gate.Commands[0].Name, "commands.") != "lint" || gate.Commands[0].Exit == nil || *gate.Commands[0].Exit != 0 {
		t.Errorf("AC-9 the passed gate recorded %+v, want commands.lint with exit status 0", gate)
	}

	// The tree hash is the one the loop stamps reviews with: the verifier's,
	// recorded on the same tree, has to name it.
	sb.worksGate(t, model.GateVerification)
	sb.worksGate(t, model.GateVerifierReview)
	readJSON(t, filepath.Join(sb.repo, ".sdlc", "stories", "US-001", model.RecordFile), &record)
	subject := ""
	for _, rv := range record.Reviews {
		if rv.Gate == model.GateVerifierReview {
			subject = rv.Subject
		}
	}
	if subject == "" || record.Gates[string(model.GateImplementation)].TreeHash != subject {
		t.Errorf("AC-9 the gate recorded tree hash %q, and the verifier reviewed tree %q: the result is not against the tree it ran on",
			record.Gates[string(model.GateImplementation)].TreeHash, subject)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(sb.works(t, tree, "", "", "status", "--json")), &status); err != nil {
		t.Fatal(err)
	}
	if commands, _ := status["commands"].(map[string]any); commands["current"] != true {
		t.Errorf("AC-9 status does not say the recorded result is for the tree as it is: %v", status["commands"])
	}
}
