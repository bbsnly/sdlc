package cli

// US-029.2, the paths storyworktree_test.go does not reach. Every way a story
// worktree cannot be made, or made and then not recorded, is refused, says
// what to do, and leaves nothing start made (AC-4); a start whose backlog
// changes under it goes round, and gives up after a second change (AC-8);
// resuming a story from where its worktree runs it (AC-1, AC-5); doctor's
// lines about git's version and the story worktrees start leaves (the plan's
// step 10); and the switched-off start's refusal after it passed over a moved
// checkout's story (D7).
//
// Where a test needs something to happen at one exact point in start -- the
// backlog changing while start asks origin, or while it cuts the story's
// branch -- it is git that does it, through a command git itself runs there:
// origin's upload-pack, or the reference-transaction hook. Nothing here waits
// on the clock.
//
// Helpers carry an su prefix, for story worktree unit.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

const (
	suConfigUnreadable = "SDLC-E0004"
	suStateUnreadable  = "SDLC-E0005"
	suStateUnwritable  = "SDLC-E0006"
	suNoRunnableStory  = "SDLC-E0010"
	suFinished         = "SDLC-E0033"
	suAwaitingPerson   = "SDLC-E0036"
)

// suSh is p as one word of a POSIX shell command.
func suSh(p string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(p), "'", `'\''`) + "'"
}

// suCommit commits the paths given on whatever main has checked out.
func suCommit(t *testing.T, main, message string, paths ...string) {
	t.Helper()
	swGit(t, main, append([]string{"add", "--"}, paths...)...)
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
}

// suBacklogWith writes main's backlog to path with the stories given marked
// done: the backlog as another start, or a person, leaves it.
func suBacklogWith(t *testing.T, main, path string, done ...string) {
	t.Helper()
	var backlog map[string]any
	wtReadJSON(t, filepath.Join(main, "user_stories.json"), &backlog)
	for _, s := range backlog["stories"].([]any) {
		if story := s.(map[string]any); slices.Contains(done, story["id"].(string)) {
			story["status"] = string(model.StatusDone)
		}
	}
	wtWriteJSON(t, path, backlog)
}

// suHook makes body one of the repository's hooks, which git runs where it
// runs that hook.
func suHook(t *testing.T, main, name, body string) {
	t.Helper()
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	swGit(t, main, "config", "core.hooksPath", filepath.ToSlash(hooks))
}

// suNothingMade holds a refused start to having made nothing for id: no
// worktree git lists or directory for it, no branch of its own, no claim, and
// no record.
func suNothingMade(t *testing.T, main, id string) {
	t.Helper()
	wt := swWorktreeOf(main, id)
	if swListed(t, main, wt) {
		t.Errorf("git lists a worktree for %s after the refusal: %v", id, swWorktrees(t, main))
	}
	if wtExists(wt) {
		t.Errorf("%s is there after the refusal", wt)
	}
	if swHasBranch(t, main, swBranchOf(id)) {
		t.Errorf("%s is there after the refusal", swBranchOf(id))
	}
	if got := swClaimed(t, main, id); got != "" {
		t.Errorf("the refused start claimed %s for %s", id, got)
	}
	if wtExists(wtRecordPath(main, id)) {
		t.Errorf("the refused start wrote a record for %s", id)
	}
}

// suSays holds text to naming every one of want.
func suSays(t *testing.T, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("the output does not say %q:\n%s", w, text)
		}
	}
}

// suNeverForces holds a fix to never forcing anybody's work away.
func suNeverForces(t *testing.T, text string) {
	t.Helper()
	for _, forced := range []string{"branch -D", "--force", " -f ", "reset --hard", "rm -rf"} {
		if strings.Contains(text, forced) {
			t.Errorf("the fix forces something away with %q:\n%s", forced, text)
		}
	}
}

// ---------------------------------------------------------------- AC-4: E0053's causes through start

// Each cause start meets on the way to a worktree is SDLC-E0053 with a fix for
// that cause, and none of them leaves a worktree, a branch, a claim, a record
// or a change of status behind.
func TestAC4EveryCauseOfAWorktreeThatCannotBeMadeHasItsOwnFixAndLeavesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, main string)
		says    []string
	}{
		{"AC-4 a .gitignore in .sdlc/worktrees that shows the worktrees", func(t *testing.T, main string) {
			writeFile(t, main, ".sdlc/worktrees/.gitignore", "*\n!US-001\n")
		}, []string{".gitignore", `put a line "*"`}},
		{"AC-4 trunk has no .sdlc/config.json committed", func(t *testing.T, main string) {
			swGit(t, main, "rm", "--cached", "--quiet", "--", ".sdlc/config.json")
			swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "untracked")
		}, []string{`"main", has no .sdlc/config.json committed`, "commit .sdlc/config.json and CLAUDE.md to trunk"}},
		{"AC-4 trunk's .sdlc/config.json does not read", func(t *testing.T, main string) {
			good := wtRead(t, filepath.Join(main, ".sdlc", "config.json"))
			writeFile(t, main, ".sdlc/config.json", "{\n")
			suCommit(t, main, "broken", ".sdlc/config.json")
			writeFile(t, main, ".sdlc/config.json", good)
		}, []string{"the .sdlc/config.json trunk commits cannot be read", "commit it to trunk"}},
		{"AC-4 trunk is not a branch here", func(t *testing.T, main string) {
			setGitOnDisk(t, main, "trunk_branch", "develop")
			suCommit(t, main, "trunk", ".sdlc/config.json")
		}, []string{`"develop"`, "create the branch git.trunk_branch names"}},
		{"AC-4 trunk_branch is an option", func(t *testing.T, main string) {
			setGitOnDisk(t, main, "trunk_branch", "--orphan")
			suCommit(t, main, "trunk", ".sdlc/config.json")
		}, []string{"set git.trunk_branch in .sdlc/config.json"}},
		{"AC-4 git cannot finish checking the worktree out", func(t *testing.T, main string) {
			suHook(t, main, "post-checkout", "echo 'post-checkout: not today' >&2\nexit 1")
		}, []string{"could not be made", "set git.worktrees to false"}},
		{"AC-4 a .gitignore that shows the worktrees appears while one is made", func(t *testing.T, main string) {
			// git runs the hook in the new worktree, .sdlc/worktrees/US-001.
			suHook(t, main, "post-checkout", `printf '!US-001\n' > ../.gitignore`)
		}, []string{".gitignore", `put a line "*"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			tc.prepare(t, main)
			refs, worktrees := swRefs(t, main), swWorktrees(t, main)
			ignore := wtRead(t, filepath.Join(main, ".sdlc", "worktrees", ".gitignore"))

			out := swRefused(t, swWorktreeNotMade, main, swSessionA, "start")
			suSays(t, out, tc.says...)
			suNothingMade(t, main, "US-001")
			if got := swRefs(t, main); got != refs {
				t.Errorf("the refused start changed the branches:\nbefore:\n%s\nafter:\n%s", refs, got)
			}
			if got := swWorktrees(t, main); !slices.Equal(got, worktrees) {
				t.Errorf("the refused start changed the worktrees from %v to %v", worktrees, got)
			}
			if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusReady) {
				t.Errorf("the refused start left US-001 %s", got)
			}
			if ignore != "" {
				if got := wtRead(t, filepath.Join(main, ".sdlc", "worktrees", ".gitignore")); got != ignore {
					t.Errorf("the refused start rewrote the .gitignore it found: %q", got)
				}
			}
		})
	}
}

// A worktree whose directory was deleted is still registered with git, with
// its branch: the fix says to prune it and delete the branch, never to force
// either, and start leaves both as it found them.
func TestAC4AStoryWorktreeWhoseDirectoryIsGoneIsPrunedNotForced(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"})
	wt := swWorktreeOf(main, "US-001")
	swGit(t, main, "worktree", "add", "--quiet", "-b", swBranchOf("US-001"), wt, "main")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	refs := swRefs(t, main)

	out := swRefused(t, swWorktreeNotMade, main, swSessionA, "start", "US-001")
	suSays(t, out, `"git worktree prune"`, `"git branch -d story/US-001"`, `no "sdlc start" is still running`)
	if strings.Contains(out, "git worktree remove") {
		t.Errorf("the fix removes a worktree whose directory is gone:\n%s", out)
	}
	suNeverForces(t, out)
	if !swListed(t, main, wt) || swRefs(t, main) != refs {
		t.Error("start took away the registration or the branch it did not make")
	}
}

// Architect 1: a named story that cannot start is refused after trunk is
// checked and before anything is made.
func TestAC4ANamedStoryWaitingOnAnotherIsRefusedBeforeAWorktreeIsMade(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002", dependsOn: []string{"US-001"}})
	out := swRefused(t, suNoRunnableStory, main, swSessionA, "start", "US-002")
	suSays(t, out, "waits on US-001")
	suNothingMade(t, main, "US-002")
	if wtExists(filepath.Join(main, ".sdlc", "worktrees")) {
		t.Error("a refused start made .sdlc/worktrees")
	}
}

// AC-8: the story's lock is the first thing start writes; a start that cannot
// take it has made nothing.
func TestAC8AStartThatCannotTakeTheStorysLockMakesNothing(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"})
	refs := swRefs(t, main)
	blocker := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, filepath.Dir(blocker), filepath.Base(blocker), "not a directory\n")
	home := t.TempDir()
	writeFile(t, home, "Library", "not a directory\n")
	writeFile(t, home, ".cache", "not a directory\n")
	t.Setenv("XDG_CACHE_HOME", blocker)
	t.Setenv("LocalAppData", blocker)
	t.Setenv("HOME", home)

	out := swRefused(t, suStateUnwritable, main, swSessionA, "start")
	suSays(t, out, "lock")
	suNothingMade(t, main, "US-001")
	if got := swRefs(t, main); got != refs {
		t.Errorf("the refused start changed the branches:\nbefore:\n%s\nafter:\n%s", refs, got)
	}
}

// ---------------------------------------------------------------- AC-4: made, and then not recorded

// Whatever refuses the start after the worktree is made takes it away again:
// a smoke check that leaves the worktree unable to read its own
// configuration, or to hold its own loop state, is such a refusal.
func TestAC4AWorktreeThatCannotBeRecordedIsTakenAwayAgain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, smoke, code string
	}{
		{"AC-4 its configuration no longer reads", `printf '{' > .sdlc/config.json`, suConfigUnreadable},
		{"AC-4 its loop state cannot be written", `rm -rf .sdlc/state && printf 'x' > .sdlc/state`, suStateUnwritable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := swRepository(t, wtStory{id: "US-001"})
			swSmoke(t, main, tc.smoke)
			refs := swRefs(t, main)

			swRefused(t, tc.code, main, swSessionA, "start")
			suNothingMade(t, main, "US-001")
			swCheckNothingLeft(t, main, "US-001", refs)
		})
	}
}

// A rollback that cannot take the worktree away -- the smoke check moved it
// to another branch -- is said, with what a person removes first, and the
// refusal is still the smoke check's.
func TestAC4ASmokeCheckThatFailsAndAWorktreeThatStaysAreBothSaid(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"})
	swSmoke(t, main, `git switch --quiet --create elsewhere && echo "broken build" && exit 3`)
	wt := swWorktreeOf(main, "US-001")

	out := swRefused(t, swTrunkBroken, main, swSessionA, "start")
	suSays(t, out, "broken build", "could not all be taken away again",
		"first take away what start left", `"git worktree remove .sdlc/worktrees/US-001"`,
		`"git branch -d story/US-001"`, "commands.smoke")
	if strings.Contains(out, "Start took the worktree and its branch away again") {
		t.Errorf("the refusal says the worktree was taken away, and it is still there:\n%s", out)
	}
	suNeverForces(t, out)
	// What it says is left is what is left.
	if !swListed(t, main, wt) || !swHasBranch(t, main, swBranchOf("US-001")) {
		t.Error("the refusal names a worktree and a branch that are not there")
	}
	if got := swClaimed(t, main, "US-001"); got != "" || wtStatuses(t, main)["US-001"] != string(model.StatusReady) {
		t.Errorf("a refused start claimed US-001 (%q) or moved it on", got)
	}
}

// ---------------------------------------------------------------- AC-8: deciding again

// A backlog that changes while the worktree is made sends start round again;
// one that changes again gives up with SDLC-E0053, and neither round leaves
// its worktree or branch. The smoke check here marks done the story whose
// worktree it runs in, as a second start finishing first would leave it.
func TestAC8AStartWhoseBacklogKeepsChangingGivesUpAndLeavesNothing(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"}, wtStory{id: "US-003"})
	after := t.TempDir()
	suBacklogWith(t, main, filepath.Join(after, "after-US-001.json"), "US-001")
	suBacklogWith(t, main, filepath.Join(after, "after-US-002.json"), "US-001", "US-002")
	swSmoke(t, main, "cp "+suSh(after)+`/after-"$(basename "$(pwd -P)")".json `+suSh(filepath.Join(main, "user_stories.json")))

	r := wtRun(t, main, swSessionA, "", "start")
	if r.code == 0 {
		t.Fatalf("start went ahead on a backlog that changed under it twice:\n%s%s", r.stdout, r.stderr)
	}
	suSays(t, r.stderr, swWorktreeNotMade, "kept changing", `run "sdlc start" again`, retryLine)
	for _, id := range []string{"US-001", "US-002"} {
		suNothingMade(t, main, id)
	}
	if got := swWorktrees(t, main); len(got) != 1 {
		t.Errorf("git knows these worktrees after start gave up: %v", got)
	}
}

// A backlog that changes before start takes the story's lock -- here while it
// asks origin whether trunk is behind -- sends it round once, to the story it
// then picks, which it begins as it would have begun the first.
func TestAC8ABacklogThatChangesBeforeTheStorysLockSendsStartToTheNextStory(t *testing.T) {
	t.Parallel()
	main := wtRepositoryWith(t, func(root string) { setGitOnDisk(t, root, "remote", true) },
		wtStory{id: "US-001"}, wtStory{id: "US-002"})
	swGit(t, main, "remote", "add", "origin", bareRemote(t))
	swGit(t, main, "push", "--quiet", "origin", "main")
	changed := filepath.Join(t.TempDir(), "after-US-001.json")
	suBacklogWith(t, main, changed, "US-001")
	// git runs this for origin as start fetches trunk from it.
	swGit(t, main, "config", "remote.origin.uploadpack",
		"cp "+suSh(changed)+" "+suSh(filepath.Join(main, "user_stories.json"))+" && git upload-pack")

	r := wtMust(t, main, swSessionA, "", "start", "--json")
	if n := strings.Count(r.stderr, retryLine); n != 1 {
		t.Errorf("start said it was deciding again %d times, want once:\n%s", n, r.stderr)
	}
	swCheckStarted(t, main, "US-002", swSessionA, decode[swStart](t, r))
	suNothingMade(t, main, "US-001")
}

// A start that finds the story's branch being created as it tries to create
// it, and the story taken meanwhile, goes round to the next rather than
// refusing. The reference-transaction hook stands in for the start that won:
// it refuses story/US-001 to this one and marks US-001 done.
func TestAC8AStartThatLosesTheStorysBranchToAnotherGoesRoundToTheNext(t *testing.T) {
	t.Parallel()
	if major, minor, err := gitx.Version(t.Context()); err != nil || major < 2 || major == 2 && minor < 28 {
		t.Skipf("git %d.%d has no reference-transaction hook, which came in 2.28 (%v)", major, minor, err)
	}
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	changed := filepath.Join(t.TempDir(), "after-US-001.json")
	suBacklogWith(t, main, changed, "US-001")
	suHook(t, main, "reference-transaction", `[ "$1" = prepared ] || exit 0
while read -r old new ref; do
	if [ "$ref" = refs/heads/story/US-001 ]; then
		cp `+suSh(changed)+" "+suSh(filepath.Join(main, "user_stories.json"))+`
		exit 1
	fi
done
exit 0`)

	r := wtMust(t, main, swSessionA, "", "start", "--json")
	if n := strings.Count(r.stderr, retryLine); n != 1 {
		t.Errorf("start said it was deciding again %d times, want once:\n%s", n, r.stderr)
	}
	swCheckStarted(t, main, "US-002", swSessionA, decode[swStart](t, r))
	suNothingMade(t, main, "US-001")
}

// A backlog that changed, and a worktree that could not be taken away, is
// SDLC-E0053 with both said, and the fix to take away what is left first.
func TestAC8ABacklogThatChangedAndAWorktreeThatStaysAreBothSaid(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	changed := filepath.Join(t.TempDir(), "after-US-001.json")
	suBacklogWith(t, main, changed, "US-001")
	swSmoke(t, main, "cp "+suSh(changed)+" "+suSh(filepath.Join(main, "user_stories.json"))+
		" && git switch --quiet --create elsewhere")

	out := swRefused(t, swWorktreeNotMade, main, swSessionA, "start")
	suSays(t, out, "the backlog changed while start was making it", "could not all be taken away again",
		"first take away what start left", `"git worktree remove .sdlc/worktrees/US-001"`, `run "sdlc start" again`)
	suNeverForces(t, out)
	if got := swClaimed(t, main, "US-001"); got != "" {
		t.Errorf("the refused start claimed US-001 for %s", got)
	}
}

// ---------------------------------------------------------------- AC-1, AC-5: picked up where it is held

// A story worktree whose HEAD someone detached still reports the branch start
// made it on.
func TestAC1AStoryWorktreeWithHeadDetachedStillReportsTheStorysBranch(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"})
	wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	swGit(t, wt, "switch", "--quiet", "--detach")

	got := swStarted(t, wt, swSessionA)
	if !got.Resume || got.Branch != swBranchOf("US-001") || !config.SamePath(got.Worktree, wt) {
		t.Errorf("start from a worktree with HEAD detached gave %+v, want US-001 resumed on %s in %s",
			got, swBranchOf("US-001"), wt)
	}
}

// AC-5: the session whose story is finished in its worktree is sent to stop
// there, rather than resuming it. Every gate passing marks the story done; a
// backlog that says in progress again -- edited by hand, or brought back by a
// merge -- is a story the session still holds, with nothing left to do.
func TestAC5ASessionWhoseStoryIsFinishedInItsWorktreeIsToldToStopThere(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	swReach(t, wt, swSessionA, "")
	wtSetStatus(t, main, "US-001", model.StatusInProgress)
	refs := swRefs(t, main)

	out := swRefused(t, suFinished, main, swSessionA, "start")
	suSays(t, out, `"sdlc stop" in the story's worktree, .sdlc/worktrees/US-001`)
	if got := swRefs(t, main); got != refs {
		t.Errorf("the refused start changed the branches:\nbefore:\n%s\nafter:\n%s", refs, got)
	}
	if swListed(t, main, swWorktreeOf(main, "US-002")) {
		t.Error("the refused start began another story in a worktree")
	}
}

// AC-5: a story waiting on a person in its worktree is not taken over by
// naming it from the main checkout.
func TestAC5AStoryWaitingOnAPersonInItsWorktreeIsNotTakenOverByName(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	wtMust(t, wt, swSessionA, "", "escalate", "spec_unclear", "--message", "which currency?")

	swRefused(t, suAwaitingPerson, main, swSessionB, "start", "US-001")
	if got := swClaimed(t, main, "US-001"); !config.SamePath(got, wt) {
		t.Errorf("the claim on US-001 names %q, want it left with %s", got, wt)
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusAwaitingHuman) {
		t.Errorf("US-001 is %s after the refusal, want it still waiting on a person", got)
	}
	if bound := strings.TrimSpace(wtRead(t, filepath.Join(wt, ".sdlc", "state", "session"))); bound == swSessionB {
		t.Error("the refused start bound its session in the story's worktree")
	}
}

// AC-5: resuming from the main checkout acts in the story's worktree, with
// that worktree's configuration; one that does not read is said, and nothing
// is moved.
func TestAC5ResumingAStoryWhoseWorktreesConfigurationDoesNotReadIsRefused(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"})
	wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	writeFile(t, wt, ".sdlc/config.json", "{\n")

	swRefused(t, suConfigUnreadable, main, swSessionA, "start")
	if got := swClaimed(t, main, "US-001"); !config.SamePath(got, wt) {
		t.Errorf("the claim on US-001 names %q, want it left with %s", got, wt)
	}
	if wtExists(filepath.Join(main, ".sdlc", "state", "active")) {
		t.Error("the refused resume moved the story to the main checkout")
	}
}

// A record that does not read is said, and neither resumed from nor written
// over: for the story a session holds in its worktree, and for a story named.
func TestAC5AStoryWhoseRecordDoesNotReadIsRefusedAndTheRecordKept(t *testing.T) {
	t.Parallel()
	t.Run("AC-5 held in its worktree by this session", func(t *testing.T) {
		main := swRepository(t, wtStory{id: "US-001"})
		wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
		writeFile(t, main, ".sdlc/stories/US-001/"+model.RecordFile, "{\n")

		suSays(t, swRefused(t, suStateUnreadable, main, swSessionA, "start"), model.RecordFile)
		if got := wtRead(t, wtRecordPath(main, "US-001")); got != "{\n" {
			t.Errorf("the record that did not read was written over: %q", got)
		}
		if got := swClaimed(t, main, "US-001"); !config.SamePath(got, wt) {
			t.Errorf("the claim on US-001 names %q, want it left with %s", got, wt)
		}
	})
	t.Run("AC-4 named, and never begun", func(t *testing.T) {
		main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		writeFile(t, main, ".sdlc/stories/US-002/"+model.RecordFile, "{\n")

		suSays(t, swRefused(t, suStateUnreadable, main, swSessionA, "start", "US-002"), model.RecordFile)
		if wt := swWorktreeOf(main, "US-002"); wtExists(wt) || swHasBranch(t, main, swBranchOf("US-002")) {
			t.Error("a start refused over its record made the story's worktree or branch")
		}
		if got := swClaimed(t, main, "US-002"); got != "" {
			t.Errorf("the refused start claimed US-002 for %s", got)
		}
		if got := wtRead(t, wtRecordPath(main, "US-002")); got != "{\n" {
			t.Errorf("the record that did not read was written over: %q", got)
		}
	})
}

// ---------------------------------------------------------------- D7: switched off

// A start that passed over a story whose checkout is gone, and then meets work
// nobody committed, says that work may be the passed-over story's own, and
// never tells the person to discard it.
func TestD7UncommittedWorkAfterAPassedOverStoryMayBeThatStorysOwn(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	gone := wtWorktree(t, main, "gone")
	wtStarted(t, gone, wtSessionWorktree)
	wtMust(t, gone, "", "", "stop")
	chdir(t, main)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	writeFile(t, main, "notes/idea.md", "half a thought\n")

	out := swRefused(t, swUncommitted, main, swSessionA, "start")
	suSays(t, out, "passed-over story's own work", "releases that story here", "commit it or stash it")
	for _, discard := range []string{"git checkout --", "git restore", "git clean", "discard"} {
		if strings.Contains(out, discard) {
			t.Errorf("the refusal advises discarding the work with %q:\n%s", discard, out)
		}
	}
	if note, refusal := strings.Index(out, "no longer exists"), strings.Index(out, swUncommitted); note < 0 || note > refusal {
		t.Errorf("the note about US-001 does not come before the refusal:\n%s", out)
	}
	if got := wtStatuses(t, main)["US-002"]; got != string(model.StatusReady) {
		t.Errorf("the refused start moved US-002 to %s", got)
	}
}

// ---------------------------------------------------------------- doctor

// suChecked is doctor's check of that name, and whether it ran.
func suChecked(t *testing.T, dir, name string) (check, bool) {
	t.Helper()
	for _, c := range decode[doctorPayload](t, wtRun(t, dir, "", "", "doctor", "--json")).Checks {
		if c.Name == name {
			return c, true
		}
	}
	return check{}, false
}

// suFakeGit puts a git on PATH that says it is version and nothing else.
func suFakeGit(t *testing.T, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script on PATH is not a command here")
	}
	dir := t.TempDir()
	writeFile(t, dir, "git", "#!/bin/sh\necho 'git version "+version+"'\n")
	if err := os.Chmod(filepath.Join(dir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// With worktrees on, a git older than the worktree commands is a problem
// doctor names, with the two ways out; with them off, it is not.
func TestDoctorHoldsGitToTheOldestVersionTheWorktreeCommandsNeedOnlyWithWorktreesOn(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		worktrees     bool
		problem       bool
	}{
		{"AC-9 on, 2.16", "2.16.3", true, true},
		{"AC-9 on, 1.9", "1.9.0", true, true},
		{"AC-9 on, 2.17", "2.17.0", true, false},
		{"AC-9 on, 2.45 for Windows", "2.45.1.windows.1", true, false},
		{"AC-9 on, 3.0", "3.0.0", true, false},
		{"AC-9 off, 2.16", "2.16.3", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suFakeGit(t, tc.version)
			c := gitCheck(t.Context(), tc.worktrees)
			if got := c.State == stateProblem; got != tc.problem {
				t.Fatalf("git %s with worktrees %v: %+v, want a problem %v", tc.version, tc.worktrees, c, tc.problem)
			}
			if !tc.problem {
				if c.Detail != "found on PATH" {
					t.Errorf("a git that passes has the detail %q, want the released one", c.Detail)
				}
				return
			}
			major, minor, _ := strings.Cut(tc.version, ".")
			minor, _, _ = strings.Cut(minor, ".")
			suSays(t, c.Detail, "git is "+major+"."+minor, "2.17")
			suSays(t, c.Fix, "install git 2.17 or newer", "set git.worktrees to false")
		})
	}
}

// Each story worktree nobody holds is described by what is true of it, with a
// fix for that case that never forces anything away; the one a story is held
// in, a worktree a person made elsewhere, and one not named for a story are
// not doctor's to describe.
func TestDoctorDescribesEachStoryWorktreeNobodyHoldsByWhatIsTrueOfIt(t *testing.T) {
	t.Parallel()
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"}, wtStory{id: "US-003"},
		wtStory{id: "US-004"}, wtStory{id: "US-005"}, wtStory{id: "US-006"})
	if got, ok := suChecked(t, main, "story worktrees"); ok {
		t.Errorf("a repository with no story worktrees has the check %+v", got)
	}
	swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	if got, ok := suChecked(t, main, "story worktrees"); ok {
		t.Errorf("a story worktree that holds its story is described: %+v", got)
	}

	add := func(id string) string {
		wt := swWorktreeOf(main, id)
		swGit(t, main, "worktree", "add", "--quiet", "-b", swBranchOf(id), wt, "main")
		return wt
	}
	add("US-002")
	swCommitFile(t, add("US-003"), "work.go", "package work\n", "US-003: the work")
	writeFile(t, add("US-004"), "draft.go", "package draft\n")
	if err := os.RemoveAll(add("US-005")); err != nil {
		t.Fatal(err)
	}
	swGit(t, main, "worktree", "add", "--quiet", "--detach", swWorktreeOf(main, "US-006"), "main")
	swGit(t, main, "worktree", "add", "--quiet", "-b", "odd", filepath.Join(main, ".sdlc", "worktrees", "not a story"), "main")
	swGit(t, main, "worktree", "add", "--quiet", "-b", "US-007", filepath.Join(wtTempDir(t), "US-007"), "main")

	c, ok := suChecked(t, main, "story worktrees")
	if !ok || c.State != stateProblem {
		t.Fatalf("doctor has no story worktrees problem: %+v", c)
	}
	suSays(t, c.Detail,
		"US-002's worktree, .sdlc/worktrees/US-002, is held by no checkout, and has nothing trunk does not",
		"US-003's worktree, .sdlc/worktrees/US-003, and story/US-003 hold 1 commit trunk does not have",
		"US-004's worktree, .sdlc/worktrees/US-004, has changes nobody committed",
		"US-005's worktree, .sdlc/worktrees/US-005, is gone",
		"US-006's worktree, .sdlc/worktrees/US-006, is held by no checkout")
	suSays(t, c.Fix,
		`"git worktree remove .sdlc/worktrees/US-002" and "git branch -d story/US-002"`,
		`"git merge --ff-only story/US-003"`,
		"may be somebody's work",
		`"git worktree prune"`,
		`"git worktree remove .sdlc/worktrees/US-006" in the main checkout`)
	for _, never := range []string{"US-001", "not a story", "US-007", `"git branch -d story/US-003"`, `"git worktree remove .sdlc/worktrees/US-004"`} {
		if strings.Contains(c.Detail+c.Fix, never) {
			t.Errorf("doctor's story worktrees check names %q:\n%s\n%s", never, c.Detail, c.Fix)
		}
	}
	suNeverForces(t, c.Fix)
}

// With worktrees on, a trunk with no configuration committed is a story that
// cannot begin, and doctor says so before start does; with them off, a start
// in this checkout needs no such thing.
func TestDoctorSaysATrunkWithNoConfigurationCommittedCannotBeginAStoryInAWorktree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		on   bool
	}{
		{"AC-4 worktrees on", true},
		{"AC-9 worktrees off", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := wtRepository
			if tc.on {
				repository = swRepository
			}
			main := repository(t, wtStory{id: "US-001"})
			swGit(t, main, "rm", "--cached", "--quiet", "--", ".sdlc/config.json")
			swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "untracked")

			c, ok := suChecked(t, main, "story worktrees")
			if !tc.on {
				if ok {
					t.Errorf("with worktrees off, doctor reports %+v", c)
				}
				return
			}
			if !ok || c.State != stateProblem {
				t.Fatalf("doctor has no problem with a trunk that has no configuration: %+v", c)
			}
			suSays(t, c.Detail, `trunk, "main", has no .sdlc/config.json committed`, "sdlc start refuses")
			suSays(t, c.Fix, "commit .sdlc/config.json and CLAUDE.md to trunk")
		})
	}
}

// A story two checkouts name is a tie the loop state check describes, and a
// backlog that does not read is the backlog check's: neither is described
// again as a story worktree nobody holds.
func TestDoctorLeavesATiedStoryAndAnUnreadableBacklogToTheirOwnChecks(t *testing.T) {
	t.Parallel()
	t.Run("a tie", func(t *testing.T) {
		main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		wt := swWorktreeOf(main, "US-001")
		swGit(t, main, "worktree", "add", "--quiet", "-b", swBranchOf("US-001"), wt, "main")
		writeFile(t, wt, ".sdlc/state/active", "US-001\n")
		writeFile(t, main, ".sdlc/state/active", "US-001\n")
		wtSetStatus(t, main, "US-001", model.StatusInProgress)

		if c, ok := suChecked(t, main, "story worktrees"); ok {
			t.Errorf("a worktree tied for its story is described as held by nobody: %+v", c)
		}
	})
	t.Run("an unreadable backlog", func(t *testing.T) {
		main := swRepository(t, wtStory{id: "US-001"})
		swGit(t, main, "worktree", "add", "--quiet", "-b", swBranchOf("US-001"), swWorktreeOf(main, "US-001"), "main")
		writeFile(t, main, "user_stories.json", "{\n")

		if c, ok := suChecked(t, main, "story worktrees"); ok {
			t.Errorf("with a backlog that does not read, doctor describes %+v", c)
		}
	})
}

// ---------------------------------------------------------------- what start says, unit by unit

// rolledBack adds what was left to a refusal, and leaves a refusal alone where
// nothing was.
func TestAC4ARefusalSaysWhatTheRollbackLeftAndOnlyThen(t *testing.T) {
	t.Parallel()
	refusal := sdlcerr.New(sdlcerr.TrunkBroken, "trunk is broken", "it exited 1").WithFix("fix trunk")
	var e *sdlcerr.Error
	if !errors.As(rolledBack(refusal, "US-001", nil), &e) || e != refusal {
		t.Errorf("with nothing left, rolledBack changed the refusal to %v", e)
	}

	left := sdlcerr.New(sdlcerr.WorktreeNotMade, "the worktree could not be taken away", "a file is open in it")
	if !errors.As(rolledBack(refusal, "US-001", left), &e) {
		t.Fatal("rolledBack lost the refusal's code")
	}
	if e.Code != sdlcerr.TrunkBroken || e.What != "trunk is broken" {
		t.Errorf("rolledBack turned the refusal into %s %q", e.Code, e.What)
	}
	suSays(t, e.Why, "it exited 1; and the worktree start made could not all be taken away again: "+
		"the worktree could not be taken away: a file is open in it")
	if !strings.HasPrefix(e.Fix, "first take away what start left") || !strings.HasSuffix(e.Fix, "; then fix trunk") {
		t.Errorf("fix = %q, want what was left first, then the refusal's own fix", e.Fix)
	}
	if refusal.Why != "it exited 1" {
		t.Error("rolledBack changed the refusal it was given")
	}

	if !errors.As(rolledBack(errors.New("disk full"), "US-001", errors.New("still there")), &e) {
		t.Fatal("rolledBack of an error with no code gave none")
	}
	if e.Code != sdlcerr.WorktreeNotMade || !strings.Contains(e.What, "disk full") {
		t.Errorf("rolledBack of an error with no code = %s %q, want SDLC-E0053 saying what failed", e.Code, e.What)
	}
	suSays(t, e.Why, "could not all be taken away again: still there")
	if strings.HasPrefix(e.Why, "; ") {
		t.Errorf("why = %q starts with a separator", e.Why)
	}
}

func TestDescribeReadsAnErrorAsTheWhyOfAnother(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want string
	}{
		{sdlcerr.New(sdlcerr.WorktreeNotMade, "what", "why"), "what: why"},
		{sdlcerr.New(sdlcerr.WorktreeNotMade, "what", ""), "what"},
		{errors.New("plain"), "plain"},
	} {
		if got := describe(tc.err); got != tc.want {
			t.Errorf("describe(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// suUnchanged holds got to being err as it was given: the same error, saying
// the same, with no code put on it.
func suUnchanged(t *testing.T, what string, got, err error) {
	t.Helper()
	var given, e *sdlcerr.Error
	switch {
	case !errors.Is(got, err) || got.Error() != err.Error():
		t.Errorf("%s changed %v to %v", what, err, got)
	case errors.As(err, &given):
		if !errors.As(got, &e) || e != given {
			t.Errorf("%s made a new refusal of %v", what, err)
		}
	case errors.As(got, &e):
		t.Errorf("%s put the code %s on %v", what, e.Code, err)
	}
}

// A refusal start has no fix of its own for keeps the one it came with.
func TestAC4ARefusalWithACauseStartDoesNotKnowKeepsItsOwnFix(t *testing.T) {
	t.Parallel()
	plain := errors.New("not one of ours")
	suUnchanged(t, "notMadeFor", notMadeFor(t.Context(), plain, "", "", "US-001", "main"), plain)
	coded := sdlcerr.New(sdlcerr.WorktreeNotMade, "it could not be made", "git said no")
	suUnchanged(t, "notMadeFor", notMadeFor(t.Context(), coded, "", "", "US-001", "main"), coded)
}

// AC-3: trunk behind origin is fixed where trunk is, which need not be checked
// out anywhere; any other failure is passed on as it is.
func TestAC3ATrunkBehindItsRemoteIsFixedWithoutCheckingItOut(t *testing.T) {
	t.Parallel()
	behind := sdlcerr.New(sdlcerr.TrunkBehind, `"main" is 2 commits behind origin`, "")
	var e *sdlcerr.Error
	if !errors.As(behindInWorktrees(behind, "main"), &e) {
		t.Fatal("the refusal lost its code")
	}
	suSays(t, e.Fix, `"git pull --ff-only"`, `"git fetch origin main:main"`)
	for _, other := range []error{errors.New("no network"), sdlcerr.New(sdlcerr.TrunkBroken, "x", "y")} {
		suUnchanged(t, "behindInWorktrees", behindInWorktrees(other, "main"), other)
	}
}

// A configuration that names no trunk is cut from the default one.
func TestAStoryIsCutFromTheDefaultTrunkWhereTheConfigurationNamesNone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ named, want string }{
		{"", config.Default().Git.TrunkBranch},
		{"   ", config.Default().Git.TrunkBranch},
		{" develop ", "develop"},
	} {
		cfg := config.Default()
		cfg.Git.TrunkBranch = tc.named
		if got := trunkOf(cfg); got != tc.want {
			t.Errorf("trunkOf(%q) = %q, want %q", tc.named, got, tc.want)
		}
	}
}

// A trunk configuration that fails for a reason with no code is still said.
func TestAC4ATrunkConfigurationThatFailsWithNoCodeIsStillSaid(t *testing.T) {
	t.Parallel()
	var e *sdlcerr.Error
	if !errors.As(noConfigOnTrunk(errors.New("permission denied"), "main"), &e) {
		t.Fatal("noConfigOnTrunk gave no code")
	}
	if e.Code != sdlcerr.WorktreeNotMade || !strings.Contains(e.What, "permission denied") {
		t.Errorf("noConfigOnTrunk = %s %q, want SDLC-E0053 saying what failed", e.Code, e.What)
	}
	suSays(t, e.Fix, "commit it to trunk", "set git.worktrees to false")
}
