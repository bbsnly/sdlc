package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
//
// Every test in this file runs with git.worktrees off, which initialised sets:
// there, start begins a story in the checkout it runs in, and SDLC-E0038 to
// SDLC-E0041 are the released tool's (US-029.2 AC-9). With story worktrees on,
// storyworktree_test.go holds which of them still apply.
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
	setConfigOnDisk(t, root, "git", map[string]any{"trunk_branch": "main", "remote": true, "worktrees": false})
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
	setConfigOnDisk(t, root, "git", map[string]any{"trunk_branch": "main", "remote": true, "worktrees": false})

	if r := mustRun(t, "start"); !strings.Contains(r.stderr, "could not be asked") {
		t.Errorf("start did not say it could not ask the remote:\n%s", r.stderr)
	}
}

// git.trunk_branch left blank is not a project that named no trunk: the
// default takes over, the same one a freshly scaffolded project has.
// A blank trunk_branch is answered by config.Default, not by whatever branch
// happens to be checked out, so this pins the name it falls back to rather
// than only trying it from the branch the fallback and gitProject agree on.
func TestABlankTrunkBranchFallsBackToTheDefault(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "git", map[string]any{"trunk_branch": "", "remote": false, "worktrees": false})

	// A blank setting that fell back to "" rather than "main" would refuse
	// every branch, including this one, so a branch other than main has to be
	// the one that is refused, and refused by name.
	inGit(t, "switch", "--quiet", "--create", "spike")
	if stderr := startRefused(t, "SDLC-E0038"); !strings.Contains(stderr, `"main"`) {
		t.Errorf("the blank trunk_branch did not fall back to \"main\":\n%s", stderr)
	}

	inGit(t, "switch", "--quiet", "main")
	mustRun(t, "start")
}

// A tree with a great many stray files is a refusal to read, not a refusal to
// notice: the message names a few and counts the rest.
func TestManyStrayFilesAreSummarisedRatherThanListed(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	for i := range 7 {
		writeFile(t, root, fmt.Sprintf("idea-%d.md", i), "half a thought\n")
	}

	stderr := startRefused(t, "SDLC-E0039")
	if !strings.Contains(stderr, "and 2 more") {
		t.Errorf("7 stray files were not summarised as 5 and 2 more:\n%s", stderr)
	}
}

// gitOnlyPath is a PATH with git on it and nothing else -- in particular
// neither sh nor bash, which commands.smoke needs to run at all.
func gitOnlyPath(t *testing.T) string {
	t.Helper()
	found, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH to isolate")
	}
	dir := t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}
	if err := os.Symlink(found, filepath.Join(dir, name)); err != nil {
		t.Skipf("could not symlink git into an isolated PATH: %v", err)
	}
	return dir
}

// A project without a POSIX shell on PATH -- Windows outside Git Bash -- is
// said out loud rather than refusing every story: the check that could not run
// is not the same as one that ran and failed.
func TestASmokeCheckWithNoShellDoesNotHoldAStoryBack(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "commands", map[string]string{"smoke": "exit 1"})
	t.Setenv("PATH", gitOnlyPath(t))

	r := mustRun(t, "start")
	if !strings.Contains(r.stderr, "sh nor bash") {
		t.Errorf("start did not say commands.smoke could not run:\n%s", r.stderr)
	}
}
