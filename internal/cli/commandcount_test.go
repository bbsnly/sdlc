package cli

// US-055 AC-3, the half production code has to do: one sdlc command asks git
// for the reviewed tree, for HEAD and for the list of the project's files at
// most once each, because nothing in between can change the answer. At
// d89109d `gate commit pass` hashed the tree twice and read HEAD twice,
// `gate code_review pass` read HEAD twice, and `review list` hashed the tree
// once for every reviewer it listed -- five times for the same tree.
//
// The other half is the threat THREATS.md names: an answer kept for the length
// of one command must not outlive it. A tree changed between two commands run
// in one process -- as every test here runs them -- is seen by the second.
//
// Calls are counted the way the story counts them: by a git on PATH that logs
// each call before running the real one. Setting PATH is the process's, so
// these tests are not parallel.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// gitAsks is what one command asked git for, by kind.
type gitAsks struct {
	tree, head, files, all int
}

// countingGit puts a git on PATH that logs every call to a file before running
// the git found there now, and returns the file.
func countingGit(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the counting git is a shell script; AC-3 is counted on macOS, and Linux runs this too")
	}
	found, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH to count")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + suSh(log) + "\nexec " + suSh(found) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// asked runs one command, which has to work, and returns what it asked git for.
func asked(t *testing.T, log, stdin string, args ...string) gitAsks {
	t.Helper()
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunWith(t, stdin, args...)
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var got gitAsks
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimRight(line, "\n")
		got.all++
		switch {
		case slices.Contains(strings.Fields(line), "write-tree"):
			got.tree++
		case strings.HasSuffix(line, "rev-parse --verify --quiet HEAD"):
			got.head++
		case strings.Contains(line, "ls-files -z --cached --others --exclude-standard"):
			got.files++
		}
	}
	return got
}

// atMostOnce fails t for each kind the command asked git for more than once.
func atMostOnce(t *testing.T, command string, got gitAsks) {
	t.Helper()
	for _, k := range []struct {
		what string
		n    int
	}{
		{"hashed the working tree (write-tree)", got.tree},
		{"read HEAD (rev-parse --verify --quiet HEAD)", got.head},
		{"listed the project's files (ls-files --cached --others)", got.files},
	} {
		if k.n > 1 {
			t.Errorf("sdlc %s %s %d times in one command, want at most once (%d git processes in all)",
				command, k.what, k.n, got.all)
		}
	}
}

// AC-3: each gate command, through a whole story worked in the main checkout,
// asks git for the reviewed tree, HEAD and the file list at most once.
func TestAC3EachGateCommandAsksGitForTheTreeHEADAndTheFilesAtMostOnce(t *testing.T) {
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	log := countingGit(t)
	for _, gate := range model.Gates {
		satisfy(t, root, gate)
		atMostOnce(t, "gate "+string(gate)+" pass",
			asked(t, log, "", "gate", string(gate), "pass", "--note", "counted"))
	}
}

// AC-3: the same in a story's own worktree, from the implementation to the
// retro, where the landing tests take every story.
func TestAC3EachGateCommandInAStoryWorktreeAsksGitForTheTreeHEADAndTheFilesAtMostOnce(t *testing.T) {
	_, wt := ldWalked(t, ldWalk{})
	ldWork(t, wt)
	log := countingGit(t)
	for _, gate := range model.Gates[slices.Index(model.Gates, model.GateImplementation):] {
		satisfy(t, wt, gate)
		atMostOnce(t, "gate "+string(gate)+" pass",
			asked(t, log, "", "gate", string(gate), "pass", "--note", "counted"))
	}
}

// AC-3: listing the reviews hashes the tree they are compared with once, not
// once for each reviewer, and recording one review hashes it once.
func TestAC3ReviewCommandsHashTheTreeOnce(t *testing.T) {
	startedAt(t, model.GateCommit)
	log := countingGit(t)

	for _, args := range [][]string{
		{"review", "list"},
		{"review", "list", "--json"},
		{"review", "list", "--gate", "code_review"},
	} {
		if got := asked(t, log, "", args...); got.tree > 1 {
			t.Errorf("sdlc %s hashed the working tree %d times, want once: the reviews are all of one tree",
				strings.Join(args, " "), got.tree)
		}
	}
	if got := asked(t, log, "# security, again\n", "review", "add", "code_review", "security", "approve"); got.tree > 1 {
		t.Errorf("sdlc review add hashed the working tree %d times, want once", got.tree)
	}
}

// AC-3 and THREATS.md, Tampering: an answer kept for one command is not kept
// for the next. Reviews of the tree as it was read as current; a file added
// between two commands, in the same process, makes them stale in the next
// listing and keeps the commit gate shut, however many times the tree was
// asked about before.
func TestAC3ATreeChangedBetweenTwoCommandsIsTheOneTheSecondCommandReads(t *testing.T) {
	t.Parallel()
	root := startedAt(t, model.GateCommit)
	stale := func() []string {
		var out []string
		for _, r := range decode[reviewListPayload](t, mustRun(t, "review", "list", "--json")).Reviews {
			if r.Stale {
				out = append(out, r.Gate+"/"+r.Role)
			}
		}
		return out
	}

	for range 2 {
		if got := stale(); len(got) != 0 {
			t.Fatalf("reviews of the tree as it is read as stale: %v", got)
		}
	}
	writeFile(t, root, "internal/late.go", "package internal\n\n// Late is nobody's reviewed work.\n")

	got := stale()
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if r.WhenDoRAdvocate {
				continue
			}
			if !slices.Contains(got, string(gate)+"/"+r.Role) {
				t.Errorf("%s/%s does not read as stale after a file was added: stale %v", gate, r.Role, got)
			}
		}
	}
	commitEverything(t, root)
	if r := run(t, "gate", "commit", "pass", "--note", "on trunk"); r.code == 0 {
		t.Error("the commit gate passed on a tree nobody reviewed, after the reviewed one had been asked about")
	}
}

// AC-3 and THREATS.md, Tampering: HEAD moved between two gate commands is the
// HEAD the second reads. A commit made after a gate passed, around the loop,
// holds the next gate back, however many gates read HEAD before it.
func TestAC3AHEADMovedBetweenTwoGateCommandsIsTheOneTheSecondReads(t *testing.T) {
	t.Parallel()
	root := startedAt(t, model.GatePlan)
	satisfy(t, root, model.GatePlan)
	mustRun(t, "gate", "plan", "pass")
	writeFile(t, root, "internal/early.go", "package internal\n")
	commitEverything(t, root)

	satisfy(t, root, model.GateDesignReview)
	if r := run(t, "gate", "design_review", "pass"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0046") {
		t.Errorf("a gate passed, or was refused for something else, after HEAD moved around the loop since the gate before it: code %d\n%s",
			r.code, r.stderr)
	}
}

// AC-3 and THREATS.md, Tampering: a test file written between two commands is
// one the second lists. The freeze holds the tests the first command saw; the
// next gate refuses the one it did not.
func TestAC3ATestFileWrittenBetweenTwoCommandsIsOneTheSecondLists(t *testing.T) {
	t.Parallel()
	root := startedAt(t, model.GatePlan)
	mustRun(t, "gate", "plan", "fail", "--note", "lists the files once")
	writeFile(t, root, "internal/sneaky_test.go", "package internal\n")

	satisfy(t, root, model.GatePlan)
	r := run(t, "gate", "plan", "pass")
	if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0043") {
		t.Errorf("a test file written after the freeze, and after a gate had listed the files, was not refused:\n%s", r.stderr)
	}
}
