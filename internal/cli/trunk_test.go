package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// inGit runs git in the project gitProject made.
func inGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// bareRemote is a repository to push to. It is made outside the project that
// gitProject points git at through the environment.
func bareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.CommandContext(t.Context(), "git", "init", "--quiet", "--bare", "--initial-branch=main", remote)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return remote
}

// startRefused is `sdlc start` refused with code.
func startRefused(t *testing.T, code string) string {
	t.Helper()
	r := run(t, "start")
	if r.code == 0 {
		t.Fatalf("start went ahead where it should have refused with %s:\n%s", code, r.stdout)
	}
	if !strings.Contains(r.stderr, code) {
		t.Fatalf("start refused with something other than %s:\n%s", code, r.stderr)
	}
	return r.stderr
}

// commands.smoke and git.remote were written into the configuration and read
// by nothing, and trunk_branch was only ever advice: a story could be started
// anywhere, on top of anything.
func TestANewStoryStartsOnlyOnTrunk(t *testing.T) {
	gitProject(t)
	initialised(t)

	inGit(t, "switch", "--quiet", "--create", "spike")
	if stderr := startRefused(t, "SDLC-E0038"); !strings.Contains(stderr, `"spike"`) {
		t.Errorf("the refusal does not say which branch HEAD is on:\n%s", stderr)
	}
	inGit(t, "switch", "--quiet", "--detach", "main")
	if stderr := startRefused(t, "SDLC-E0038"); !strings.Contains(stderr, "detached") {
		t.Errorf("the refusal does not say HEAD is detached:\n%s", stderr)
	}

	inGit(t, "switch", "--quiet", "main")
	mustRun(t, "start")
}

func TestWorkNobodyCommittedHoldsANewStoryBack(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	writeFile(t, root, "notes/idea.md", "half a thought\n")
	writeFile(t, root, "CLAUDE.md", "edited, and not committed\n")

	stderr := startRefused(t, "SDLC-E0039")
	for _, want := range []string{"notes/", "CLAUDE.md"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal does not name %s:\n%s", want, stderr)
		}
	}

	// The loop's own files are not somebody's stray work: it writes its state
	// and the backlog's statuses itself, and they are committed with the story.
	inGit(t, "checkout", "--quiet", "--", "CLAUDE.md")
	if err := os.RemoveAll(filepath.Join(root, "notes")); err != nil {
		t.Fatal(err)
	}
	addStory(t, root, "US-002")
	writeFile(t, root, ".sdlc/stories/US-001/scratch.md", "the loop's own\n")
	mustRun(t, "start")
}

// An unfinished story has its own work in the tree, and a smoke check that
// fails because of that work is the story's to fix, not a reason to refuse
// picking it up.
func TestAStoryIsPickedUpAgainWithoutCheckingTrunk(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	mustRun(t, "stop")

	writeFile(t, root, "internal/invoice.go", "package internal\n")
	setConfigOnDisk(t, root, "commands", map[string]string{"smoke": "exit 1"})
	mustRun(t, "start")
}

func TestABrokenTrunkHoldsANewStoryBack(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "commands",
		map[string]string{"smoke": `echo "invoice.go:3:18: undefined: x"; exit 1`})

	if stderr := startRefused(t, "SDLC-E0041"); !strings.Contains(stderr, "undefined: x") {
		t.Errorf("the refusal does not carry what the smoke check said:\n%s", stderr)
	}

	setConfigOnDisk(t, root, "commands", map[string]string{"smoke": "test -f user_stories.json"})
	mustRun(t, "start")
}

func TestATrunkBehindItsRemoteHoldsANewStoryBack(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	inGit(t, "remote", "add", "origin", bareRemote(t))
	setConfigOnDisk(t, root, "git", map[string]any{"trunk_branch": "main", "remote": true})
	commitEverything(t, root)
	inGit(t, "push", "--quiet", "origin", "main")

	// Somebody else lands a commit on trunk that this clone does not have.
	writeFile(t, root, "CHANGES.md", "someone else's work\n")
	commitEverything(t, root)
	inGit(t, "push", "--quiet", "origin", "main")
	inGit(t, "reset", "--quiet", "--hard", "HEAD~1")

	if stderr := startRefused(t, "SDLC-E0040"); !strings.Contains(stderr, "1 commit behind") {
		t.Errorf("the refusal does not say how far behind:\n%s", stderr)
	}

	inGit(t, "merge", "--quiet", "--ff-only", "origin/main")
	mustRun(t, "start")
}

// Working offline is allowed. A remote that cannot be asked is said out loud,
// rather than refusing every story until the network comes back.
func TestARemoteThatCannotBeAskedDoesNotHoldAStoryBack(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	inGit(t, "remote", "add", "origin", filepath.Join(t.TempDir(), "nowhere.git"))
	setConfigOnDisk(t, root, "git", map[string]any{"trunk_branch": "main", "remote": true})

	if r := mustRun(t, "start"); !strings.Contains(r.stderr, "could not be asked") {
		t.Errorf("start did not say it could not ask the remote:\n%s", r.stderr)
	}
}
