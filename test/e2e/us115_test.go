package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// rpAtDesignReview is a sandbox whose story has reached the design review, its
// tree holding what the sandbox's `sdlc init` wrote and a person committed.
func rpAtDesignReview(t *testing.T) (sandbox, string) {
	t.Helper()
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	tree := sb.tree()
	for gate := sb.nextGate(t); gate != string(model.GateDesignReview); gate = sb.nextGate(t) {
		if gate == "" {
			t.Fatal("the story passed its last gate without stopping at the design review")
		}
		sb.worksGate(t, model.Gate(gate))
	}
	return sb, tree
}

// rpPersonSetsPolicy is a person editing the configuration by hand, before a
// story starts, as the file rules make them.
func rpPersonSetsPolicy(t *testing.T, sb sandbox, path string) {
	t.Helper()
	configPath := filepath.Join(sb.repo, ".sdlc", "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["reviews"] = map[string]any{"policy": path}
	// What else doctor counts as a problem in a sandbox with no stack, so that
	// its exit code is the warning's to answer for.
	doc["commands"] = map[string]any{"test": "git --version", "lint": "git --version"}
	edited, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, edited, 0o600); err != nil {
		t.Fatal(err)
	}
}

// rpStored is the record's bytes and the review files beside it.
func rpStored(t *testing.T, sb sandbox) (string, []string) {
	t.Helper()
	dir := filepath.Join(sb.repo, ".sdlc", "stories", "US-001")
	raw, err := os.ReadFile(filepath.Join(dir, "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "reviews"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return string(raw), names
}

// US-115 AC-8. Through the real binary and the plugin's launcher, in a real git
// repository: init writes the policy, review list names it, and a block with no
// important finding is refused with nothing recorded. Then the same block with
// its count goes through as it always did, and the command a reviewer is told to
// run passes the hook.
func TestAC8InitWritesThePolicyReviewListNamesItAndABlockWithoutAnImportantFindingIsRefused(t *testing.T) {
	t.Parallel()
	sb := newSandbox(t)

	body, err := os.ReadFile(filepath.Join(sb.repo, "REVIEW.md"))
	if err != nil {
		t.Fatalf("AC-8 sdlc init did not write REVIEW.md: %v", err)
	}
	for _, heading := range []string{"pass", "important", "nit", "cap", "not to review"} {
		if !strings.Contains(strings.ToLower(string(body)), heading) {
			t.Errorf("AC-8 the policy init wrote has nothing on %q:\n%s", heading, body)
		}
	}

	sb.works(t, sb.repo, "", "", "start")
	tree := sb.tree()
	for gate := sb.nextGate(t); gate != string(model.GateDesignReview); gate = sb.nextGate(t) {
		if gate == "" {
			t.Fatal("the story passed its last gate without stopping at the design review")
		}
		sb.worksGate(t, model.Gate(gate))
	}

	listed := sb.works(t, tree, "", "", "review", "list", "--gate", "design_review")
	first, rest, _ := strings.Cut(listed, "\n")
	if !strings.Contains(first, "REVIEW.md") {
		t.Errorf("AC-8 the first line of review list does not name the policy: %q", first)
	}
	if !strings.Contains(rest, "architect") {
		t.Errorf("AC-8 review list lost its reviewers:\n%s", listed)
	}
	var payload struct {
		Policy string `json:"policy"`
	}
	asJSON := sb.works(t, tree, "", "", "review", "list", "--json")
	if err := json.Unmarshal([]byte(asJSON), &payload); err != nil || payload.Policy != "REVIEW.md" {
		t.Errorf("AC-8 review list --json: policy %q (%v)\n%s", payload.Policy, err, asJSON)
	}

	beforeRecord, beforeReviews := rpStored(t, sb)
	for _, args := range [][]string{
		{"review", "add", "design_review", "architect", "block"},
		{"review", "add", "design_review", "architect", "block", "--important", "0"},
		{"review", "add", "design_review", "architect", "block", "--nits", "4"},
	} {
		r := sb.does(t, tree, "architect", "# architect\n\nOnly nits.\n", args...)
		if r.code == 0 {
			t.Errorf("AC-8 %v was recorded:\n%s", args, r.stdout)
		}
		if !strings.Contains(r.stderr, "SDLC-E0086") || !strings.Contains(r.stderr, "important finding") {
			t.Errorf("AC-8 %v: the refusal does not say why:\n%s", args, r.stderr)
		}
		afterRecord, afterReviews := rpStored(t, sb)
		if afterRecord != beforeRecord || len(afterReviews) != len(beforeReviews) {
			t.Errorf("AC-8 %v recorded something", args)
		}
	}

	// Red team: refusing only at the design review, or only for the architect,
	// passes every row above.
	for _, c := range []struct{ gate, role string }{
		{"code_review", "code-reviewer"},
		{"verifier_review", "verifier"},
		{"design_review", "security"},
	} {
		r := sb.does(t, tree, c.role, "# "+c.role+"\n", "review", "add", c.gate, c.role, "block")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0086") {
			t.Errorf("AC-8 a block by %s at %s: exit %d\n%s%s", c.role, c.gate, r.code, r.stdout, r.stderr)
		}
	}
	if afterRecord, afterReviews := rpStored(t, sb); afterRecord != beforeRecord || len(afterReviews) != len(beforeReviews) {
		t.Error("AC-8 a refused block recorded something")
	}

	r := sb.does(t, tree, "architect", "# architect\n\nAC-3 has no step.\n",
		"review", "add", "design_review", "architect", "block", "--important", "1", "--nits", "2", "--note", "AC-3 has no step")
	if r.code != 0 || !strings.Contains(r.stdout, "block (round 1)") {
		t.Fatalf("AC-3 a block with a count: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	afterRecord, _ := rpStored(t, sb)
	if !strings.Contains(afterRecord, `"important": 1`) && !strings.Contains(afterRecord, `"important":1`) {
		t.Errorf("AC-3 the count is not on the record:\n%s", afterRecord)
	}
}

// T1 and the brief's reproduction: the spelling that got past the released rule,
// through the real hook, as a session's Bash call.
func TestAC3TheRealHookKeepsAReviewWithCountsItsReviewersOwn(t *testing.T) {
	t.Parallel()
	sb, _ := rpAtDesignReview(t)
	const rule = "review-is-recorded-by-its-reviewer"
	for _, command := range []string{
		"sdlc review add code_review --important 3 code-reviewer block",
		"sdlc review add code_review --nits 3 code-reviewer block",
		"sdlc review add code_review --important=3 code-reviewer block",
		"sdlc review add --important 3 code_review code-reviewer block",
		"sdlc review add code_review --important 3 --important 4 code-reviewer block",
	} {
		sb.refusesAs(t, "implementer", command, rule)
	}
}

// The person's Q1, through the real hook. A person writes the policy, by init and
// by hand before the story starts; every agent is then refused, by the file tools
// and the shell alike, wherever the configuration says it is.
func TestQ1TheRealHookHoldsThePolicyAgainstEveryAgentWhileAStoryRuns(t *testing.T) {
	t.Parallel()
	const fileRule, shellRule = "review-policy-is-not-edited", "protected-path-through-the-tool"
	writes := func(p string) []string {
		return []string{
			"echo weakened > " + p,
			"echo weakened >> " + p,
			"echo weakened | tee " + p,
			"cp REVIEW.copy " + p,
			"mv REVIEW.copy " + p,
			"sed -i 's/5 per review/500 per review/' " + p,
			"rm " + p,
		}
	}
	refusesFile := func(t *testing.T, sb sandbox, tree, agent, rel string) {
		t.Helper()
		stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, agent, "Write",
			map[string]any{"file_path": filepath.Join(tree, filepath.FromSlash(rel)), "content": "weakened\n"}))
		if code != 0 {
			t.Fatalf("the hook exited %d: %s", code, stderr)
		}
		var r refusal
		if stdout == carryOn {
			t.Errorf("%q wrote %s with the file tools", agent, rel)
			return
		}
		oneLine(t, stdout, &r)
		if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "["+fileRule+"]") && agent == "implementer" {
			t.Errorf("%s was refused under another rule:\n%s", rel, r.HookSpecificOutput.PermissionDecisionReason)
		}
	}

	t.Run("Q1 the default path, written by init", func(t *testing.T) {
		t.Parallel()
		sb, tree := rpAtDesignReview(t)
		for _, p := range []string{"REVIEW.md", "review.md"} {
			for _, command := range writes(p) {
				sb.refusesAs(t, "implementer", command, shellRule)
			}
			refusesFile(t, sb, tree, "implementer", p)
		}
		for _, agent := range []string{"", "sdet", "code-reviewer", "architect"} {
			sb.refusesAs(t, agent, "rm REVIEW.md", shellRule, "reviewer-reviews")
			refusesFile(t, sb, tree, agent, "REVIEW.md")
		}
		sb.allows(t, tree, "code-reviewer", "Bash", map[string]any{"command": "cat REVIEW.md"})
	})

	t.Run("Q1 a path a person configured before the story", func(t *testing.T) {
		t.Parallel()
		sb := newSandbox(t)
		rpPersonSetsPolicy(t, sb, "docs/policy.md")
		writeFile(t, filepath.Join(sb.repo, "docs", "policy.md"), "# Ours\n")
		sb.mustGit(t, sb.repo, "add", "--all")
		sb.mustGit(t, sb.repo, "commit", "--quiet", "--message", "chore: move the review policy")
		sb.works(t, sb.repo, "", "", "start")
		tree := sb.tree()

		for _, command := range writes("docs/policy.md") {
			sb.refusesAs(t, "implementer", command, shellRule)
		}
		refusesFile(t, sb, tree, "implementer", "docs/policy.md")

		listed := sb.works(t, tree, "", "", "review", "list", "--gate", "design_review")
		if first, _, _ := strings.Cut(listed, "\n"); !strings.Contains(first, "docs/policy.md") {
			t.Errorf("AC-1 review list does not name the configured path: %q", first)
		}
		// Moved, the old name is an ordinary file.
		sb.allows(t, tree, "implementer", "Bash", map[string]any{"command": "echo mine > REVIEW.md"})
	})
}

// AC-6 and Q3, through the real binary: doctor warns and exits 0.
func TestAC6DoctorWarnsAboutAMissingPolicyAndExitsZero(t *testing.T) {
	t.Parallel()
	sb := newSandbox(t)
	rpPersonSetsPolicy(t, sb, "docs/gone.md")
	r := sb.person(t, sb.repo, "doctor")
	if r.code != 0 {
		t.Errorf("exit %d: a warning is not a problem:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "warning") || !strings.Contains(r.stdout, "docs/gone.md") {
		t.Errorf("the report does not warn about the file:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "1 problem") {
		t.Errorf("the warning counted as a problem:\n%s", r.stdout)
	}
}

// Security B1 and B2 and the architect's B2, through the real hook, as a session's
// Bash calls: a bare redirect, an editor, a target assigned on the line, and the
// globs a rule added at one call site would miss. Each under the rule the path
// already has, in a story past its freeze, and an ordinary file stays free.
func TestQ1TheRealHookJudgesARedirectAloneAnEditorAndAnAssignedTarget(t *testing.T) {
	t.Parallel()
	sb, tree := rpAtDesignReview(t)
	const protected, state, frozen = "protected-path-through-the-tool", "loop-state-through-the-tool", "frozen-test-through-the-tool"
	for _, c := range []struct{ path, rule string }{
		{"REVIEW.md", protected},
		{"invoice_test.go", frozen},
		{".sdlc/state/tests.lock", state},
		{".sdlc/config.json", state},
		{"CLAUDE.md", protected},
		{"user_stories.json", protected},
		{".sdlc/state/active", state},
		{".sdlc/state/session", state},
	} {
		sb.refusesAs(t, "implementer", "> "+c.path, c.rule)
		sb.refusesAs(t, "implementer", "ex -sc %d -c wq "+c.path, c.rule)
		sb.refusesAs(t, "implementer", "vim -es -c %d -c wq "+c.path, c.rule)
	}
	for _, c := range []struct{ path, rule string }{
		{"REVIEW.md", protected},
		{"invoice_test.go", frozen},
		{".sdlc/state/active", state},
	} {
		for _, command := range []string{
			`T=` + c.path + `; echo w > "$T"`,
			`T=` + c.path + `; rm "$T"`,
			`export T=` + c.path + `; echo w > "${T}"`,
			`(T=` + c.path + `); echo w > "$T"`,
			`if true; then T=` + c.path + `; fi; rm "$T"`,
		} {
			sb.refusesAs(t, "implementer", command, c.rule)
		}
	}
	for _, command := range []string{"rm REV*", "rm REVIEW.m?"} {
		sb.refusesAs(t, "implementer", command, protected)
	}
	for _, command := range []string{
		"> notes.md",
		"ex -sc %d -c wq notes.md",
		`T=notes.md; echo w > "$T"`,
		`echo x > "$OUT"`,
		"cat < REVIEW.md",
		"> /dev/null",
	} {
		sb.allows(t, tree, "implementer", "Bash", map[string]any{"command": command})
	}
}

// Security B3, through the real binary. Nothing here gets past a rule: the test
// writes as a program the implementer wrote would, and the landing is what stops
// it reaching trunk.
func TestQ1TheLandingRefusesAStoryThatChangesThePolicyInASandbox(t *testing.T) {
	t.Parallel()
	sb, tree := rpAtDesignReview(t)
	writeFile(t, filepath.Join(tree, "REVIEW.md"), "# Weakened\n")
	sb.worksEveryGate(t)
	before := sb.mustGit(t, sb.repo, "rev-parse", "main")

	r := sb.does(t, tree, "", "", "stop")
	if r.code == 0 {
		t.Fatalf("the story landed:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, "SDLC-E0055") || !strings.Contains(r.stderr, "REVIEW.md") || !strings.Contains(r.stderr, "review policy") {
		t.Errorf("the refusal does not name the code, the file and the policy:\n%s", r.stderr)
	}
	if after := sb.mustGit(t, sb.repo, "rev-parse", "main"); after != before {
		t.Errorf("trunk moved from %s to %s", before, after)
	}
}

// Red team, careless operator 1, through the real binary: the file is in the
// checkout and not on trunk, so a story's worktree would have none.
func TestAC6DoctorWarnsAboutAPolicyThatIsNotOnTrunkInASandbox(t *testing.T) {
	t.Parallel()
	sb := newSandbox(t)
	rpPersonSetsPolicy(t, sb, "REVIEW.md")
	sb.mustGit(t, sb.repo, "rm", "--cached", "--quiet", "REVIEW.md")
	sb.mustGit(t, sb.repo, "add", ".sdlc")
	sb.mustGit(t, sb.repo, "commit", "--quiet", "--message", "chore: leave the policy out")
	r := sb.person(t, sb.repo, "doctor")
	if r.code != 0 {
		t.Errorf("exit %d: a warning is not a problem:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "warning") || !strings.Contains(r.stdout, "REVIEW.md") || !strings.Contains(r.stdout, "main") {
		t.Errorf("the report does not warn that the policy is not on trunk:\n%s", r.stdout)
	}
}
