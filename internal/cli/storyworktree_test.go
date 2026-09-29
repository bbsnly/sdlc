package cli

// US-029.2 in process: `sdlc start` gives each new story a git worktree of its
// own, at .sdlc/worktrees/<ID> in the main checkout, on a branch story/<ID> cut
// from the tip of trunk, and the loop works inside it. git.worktrees turns it
// off, and a configuration that does not name it has it on.
//
// The repositories are real and made as a person makes them, with git's own
// variables taken out of the environment (wtNoGitEnv): sdlc finds each checkout
// by its .git, as it does for a person. Everything here reads what a caller
// can see -- start's output, git's answers, the files on disk -- and decodes
// start's JSON into a struct of its own, so that nothing names an identifier
// the story has still to add. Refusals are matched by the code they print.
//
// Helpers carry an sw prefix, for story worktree.

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// The Claude Code sessions stories are worked in. One session holds one story.
const (
	swSessionA = "sw-session-a"
	swSessionB = "sw-session-b"
	swSessionC = "sw-session-c"
)

// The codes the story's refusals are matched by. SDLC-E0053 is the one the
// story adds: the worktree could not be made (AC-4).
const (
	swWorktreeNotMade = "SDLC-E0053"
	swNotOnTrunk      = "SDLC-E0038"
	swUncommitted     = "SDLC-E0039"
	swTrunkBehind     = "SDLC-E0040"
	swTrunkBroken     = "SDLC-E0041"
	swSessionHere     = "SDLC-E0048"
)

// swStart is what `sdlc start --json` says, with the two fields AC-1 adds.
type swStart struct {
	OK       bool   `json:"ok"`
	Story    string `json:"story"`
	Resume   bool   `json:"resume"`
	Session  string `json:"session"`
	NextGate string `json:"next_gate"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
}

// swRepository is wtRepository with git.worktrees left out of the
// configuration, as every project has it that has not turned them off.
func swRepository(t *testing.T, stories ...wtStory) string {
	t.Helper()
	return wtRepositoryWith(t, nil, stories...)
}

// swWorktreeOf is where the story's worktree is made: inside the main
// checkout, under .sdlc (the story's third decision).
func swWorktreeOf(main, id string) string {
	return filepath.Join(main, ".sdlc", "worktrees", id)
}

// swBranchOf is the story's branch (the story's third decision).
func swBranchOf(id string) string { return "story/" + id }

// swStarted is `sdlc start --json` in dir from session, which has to work.
func swStarted(t *testing.T, dir, session string, args ...string) swStart {
	t.Helper()
	return decode[swStart](t, wtMust(t, dir, session, "", append(append([]string{"start"}, args...), "--json")...))
}

// swRefused runs a command that has to be refused with code, and returns what
// it printed on both streams.
func swRefused(t *testing.T, code, dir, session string, args ...string) string {
	t.Helper()
	r := wtRun(t, dir, session, "", args...)
	out := r.stdout + r.stderr
	if r.code == 0 {
		t.Fatalf("sdlc %s in %s went ahead where it should have been refused with %s:\n%s",
			strings.Join(args, " "), dir, code, out)
	}
	if !strings.Contains(out, code) {
		t.Fatalf("sdlc %s in %s was refused with something other than %s:\n%s",
			strings.Join(args, " "), dir, code, out)
	}
	return out
}

// swGitTry runs git in dir with none of git's own directory variables, so
// that what it reports is dir's repository whatever the test has set, and says
// whether it worked.
func swGitTry(t *testing.T, dir string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		switch name, _, _ := strings.Cut(kv, "="); name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// swGitRaw is swGitTry's standard output as git wrote it, for output whose
// leading spaces mean something: trimmed, the first line of `git status
// --porcelain` lost the blank index column and read a letter short.
func swGitRaw(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		switch name, _, _ := strings.Cut(kv, "="); name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return string(out)
}

func swGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, ok := swGitTry(t, dir, args...)
	if !ok {
		t.Fatalf("git %s in %s:\n%s", strings.Join(args, " "), dir, out)
	}
	return out
}

// swRev is the commit rev names in dir's repository.
func swRev(t *testing.T, dir, rev string) string {
	t.Helper()
	return swGit(t, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
}

// swHasBranch reports whether the repository at dir has the branch.
func swHasBranch(t *testing.T, dir, branch string) bool {
	t.Helper()
	_, ok := swGitTry(t, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return ok
}

// swWorktrees is every working tree git lists for the repository at dir.
func swWorktrees(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for line := range strings.Lines(swGit(t, dir, "worktree", "list", "--porcelain")) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// swListed reports whether git lists dir among the repository's working trees.
func swListed(t *testing.T, main, dir string) bool {
	t.Helper()
	return slices.ContainsFunc(swWorktrees(t, main), func(p string) bool { return config.SamePath(p, dir) })
}

// swRefs is every branch of the repository at dir, with the commit it is on.
func swRefs(t *testing.T, dir string) string {
	t.Helper()
	return swGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
}

// swCommitFile commits one file on whatever dir has checked out, and nothing
// else, and returns the commit.
func swCommitFile(t *testing.T, dir, rel, body, message string) string {
	t.Helper()
	writeFile(t, dir, rel, body)
	swGit(t, dir, "add", "--", rel)
	swGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
	return swRev(t, dir, "HEAD")
}

// swRecord is the story's record, from the main checkout.
func swRecord(t *testing.T, main, id string) *model.Record {
	t.Helper()
	var r model.Record
	wtReadJSON(t, wtRecordPath(main, id), &r)
	return &r
}

// swClaimed is the checkout the claim on id names, or "" for no claim.
func swClaimed(t *testing.T, main, id string) string {
	t.Helper()
	path := rlClaimFile(main, id)
	if !wtExists(path) {
		return ""
	}
	var c struct {
		Checkout string `json:"checkout"`
	}
	wtReadJSON(t, path, &c)
	return c.Checkout
}

// swSmoke commits commands.smoke on trunk, which is the configuration a new
// story worktree is checked out with.
func swSmoke(t *testing.T, main, command string) {
	t.Helper()
	setConfigOnDisk(t, main, "commands", map[string]string{"smoke": command})
	swGit(t, main, "add", "--", ".sdlc/config.json")
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "smoke")
}

// swReach takes the story dir holds through every gate before upTo, in the
// session given, as the loop's own helper does in one checkout.
func swReach(t *testing.T, dir, session string, upTo model.Gate) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv(store.SessionEnv, session)
	reach(t, dir, upTo)
}

// swPass takes the story dir holds through the gates from `from` up to, and
// not including, upTo, where reach would begin again at the first.
func swPass(t *testing.T, dir, session string, from, upTo model.Gate) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv(store.SessionEnv, session)
	on := false
	for _, gate := range model.Gates {
		if gate == upTo {
			return
		}
		if on = on || gate == from; on {
			satisfy(t, dir, gate)
			mustRun(t, "gate", string(gate), "pass", "--note", "passed by the test helper")
		}
	}
}

// swEventsSince is the kind of every event on r after the first n.
func swEventsSince(r *model.Record, n int) []string {
	var out []string
	for _, e := range r.Events[n:] {
		out = append(out, e.Type)
	}
	return out
}

// swCheckStarted holds a start that began or resumed id to AC-1: the worktree
// at .sdlc/worktrees/<ID> of the main checkout, on story/<ID>, is a working
// tree git lists, and holds the story and the session.
func swCheckStarted(t *testing.T, main, id, session string, got swStart) string {
	t.Helper()
	wt := swWorktreeOf(main, id)
	if got.Story != id {
		t.Fatalf("start gave %q, want %s", got.Story, id)
	}
	if got.Worktree == "" || !filepath.IsAbs(got.Worktree) || !config.SamePath(got.Worktree, wt) {
		t.Fatalf("start --json gives worktree %q, want %s", got.Worktree, wt)
	}
	if got.Branch != swBranchOf(id) {
		t.Errorf("start --json gives branch %q, want %q", got.Branch, swBranchOf(id))
	}
	if !swListed(t, main, wt) {
		t.Fatalf("git does not list %s as a working tree of the repository: %v", wt, swWorktrees(t, main))
	}
	if head, _ := swGitTry(t, wt, "symbolic-ref", "--short", "HEAD"); head != swBranchOf(id) {
		t.Errorf("the worktree is on %q, want %q", head, swBranchOf(id))
	}
	if active := strings.TrimSpace(wtRead(t, filepath.Join(wt, ".sdlc", "state", "active"))); active != id {
		t.Errorf("the worktree's .sdlc/state/active names %q, want %s", active, id)
	}
	if session != "" {
		if bound := strings.TrimSpace(wtRead(t, filepath.Join(wt, ".sdlc", "state", "session"))); bound != session {
			t.Errorf("the worktree's .sdlc/state/session names %q, want %s", bound, session)
		}
	}
	if claimed := swClaimed(t, main, id); claimed == "" || !config.SamePath(claimed, wt) {
		t.Errorf("the claim on %s names %q, want the worktree %s", id, claimed, wt)
	}
	return wt
}

// ---------------------------------------------------------------- AC-1

func TestAC1StartMakesAWorktreeOnAStoryBranchCutFromTheTipOfTrunk(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	trunk := swRev(t, main, "refs/heads/main")

	got := swStarted(t, main, swSessionA)
	if got.Resume {
		t.Error("a story begun reads as resumed")
	}
	if got.Session != swSessionA {
		t.Errorf("start recorded session %q, want %s", got.Session, swSessionA)
	}
	wt := swCheckStarted(t, main, "US-001", swSessionA, got)

	if tip := swRev(t, main, "refs/heads/"+swBranchOf("US-001")); tip != trunk {
		t.Errorf("%s is at %s, want the tip of trunk %s", swBranchOf("US-001"), tip, trunk)
	}
	if head := swRev(t, wt, "HEAD"); head != trunk {
		t.Errorf("the worktree's HEAD is %s, want the tip of trunk %s", head, trunk)
	}
	// The story's evidence stays where US-029.1 keeps it: its record in the
	// main checkout, measured from the commit the branch was cut from.
	if r := swRecord(t, main, "US-001"); r.Base != trunk {
		t.Errorf("the record's base is %q, want the commit the branch was cut from, %s", r.Base, trunk)
	}
	if wtExists(filepath.Join(wt, ".sdlc", "stories", "US-001")) {
		t.Error("the story's record was written in the worktree")
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusInProgress) {
		t.Errorf("the main checkout's backlog has US-001 %s, want in_progress", got)
	}

	t.Run("AC-1 printed", func(t *testing.T) {
		main := swRepository(t, wtStory{id: "US-001"})
		out := wtMust(t, main, swSessionA, "", "start").stdout
		if !wtNames(out, swWorktreeOf(main, "US-001")) {
			t.Errorf("start does not print the worktree's path:\n%s", out)
		}
		if !strings.Contains(out, swBranchOf("US-001")) {
			t.Errorf("start does not print the branch's name:\n%s", out)
		}
	})
}

// ---------------------------------------------------------------- AC-2

// swMainAsItIs is what AC-2 holds still in the main checkout: HEAD and the
// branch it is on, the index, and every file outside git's directory, the
// story worktrees, and the loop's own bookkeeping for the story -- the
// backlog and its record (the analysis gate's first question).
type swMainAsItIs struct {
	head, branch, index string
	files               map[string]string
}

func swMainNow(t *testing.T, main, id string) swMainAsItIs {
	t.Helper()
	branch, _ := swGitTry(t, main, "symbolic-ref", "--quiet", "HEAD")
	files := wtFiles(t, main)
	maps.DeleteFunc(files, func(p, _ string) bool {
		return strings.HasPrefix(p, ".git/") || strings.HasPrefix(p, ".sdlc/worktrees/") ||
			strings.HasPrefix(p, ".sdlc/stories/"+id+"/") || p == "user_stories.json"
	})
	return swMainAsItIs{
		head:   swRev(t, main, "HEAD"),
		branch: branch,
		index:  swGit(t, main, "ls-files", "--stage"),
		files:  files,
	}
}

// swStatus is `git status` in the main checkout, a line per path.
func swStatus(t *testing.T, main string) []string {
	t.Helper()
	var out []string
	for line := range strings.Lines(swGitRaw(t, main, "status", "--porcelain", "--untracked-files=all")) {
		if line = strings.TrimRight(line, "\n"); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestAC2StartLeavesTheMainCheckoutAsItWasApartFromTheLoopsOwnBookkeeping(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, main string)
	}{
		{"AC-2 a clean main checkout on trunk", func(*testing.T, string) {}},
		{"AC-2 a main checkout on another branch with work nobody committed", func(t *testing.T, main string) {
			swGit(t, main, "switch", "--quiet", "--create", "spike")
			swCommitFile(t, main, "spike.md", "a person's spike\n", "spike")
			writeFile(t, main, "notes/idea.md", "half a thought\n")
			writeFile(t, main, "CLAUDE.md", "edited, and not committed\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			tc.prepare(t, main)
			before, statusBefore := swMainNow(t, main, "US-001"), swStatus(t, main)
			statuses := wtStatuses(t, main)

			swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))

			after := swMainNow(t, main, "US-001")
			if after.head != before.head || after.branch != before.branch {
				t.Errorf("the main checkout moved from %s %s to %s %s",
					before.branch, before.head, after.branch, after.head)
			}
			if after.index != before.index {
				t.Errorf("the main checkout's index changed:\nbefore:\n%s\nafter:\n%s", before.index, after.index)
			}
			for p, body := range after.files {
				if old, ok := before.files[p]; !ok || old != body {
					t.Errorf("start wrote %s in the main checkout", p)
				}
			}
			for p := range before.files {
				if _, ok := after.files[p]; !ok {
					t.Errorf("start took %s away from the main checkout", p)
				}
			}
			// git status says nothing new but the backlog's status and the
			// story's record: not the worktree, and not the loop's state, which
			// is the worktree's.
			for _, line := range swStatus(t, main) {
				if slices.Contains(statusBefore, line) {
					continue
				}
				p := line[3:]
				if p != "user_stories.json" && !strings.HasPrefix(p, ".sdlc/stories/US-001/") {
					t.Errorf("git status in the main checkout reports %q, which it did not before start", line)
				}
			}
			if dry := swGit(t, main, "add", "--all", "--dry-run"); strings.Contains(dry, ".sdlc/worktrees") {
				t.Errorf("git add -A in the main checkout would take in the story worktree:\n%s", dry)
			}
			statuses["US-001"] = string(model.StatusInProgress)
			if got := wtStatuses(t, main); !maps.Equal(got, statuses) {
				t.Errorf("the backlog has %v, want only US-001 in progress: %v", got, statuses)
			}
		})
	}
}

// ---------------------------------------------------------------- AC-3

// A worktree is cut from trunk, so the main checkout's branch and its
// uncommitted work are the person's, not the story's, and hold nothing back.
func TestAC3AStoryBeginsInAWorktreeWhateverTheMainCheckoutIsOnOrHasNotCommitted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, main string)
	}{
		{"AC-3 on another branch", func(t *testing.T, main string) {
			swGit(t, main, "switch", "--quiet", "--create", "spike")
			swCommitFile(t, main, "spike.md", "a person's spike\n", "spike")
		}},
		{"AC-3 detached", func(t *testing.T, main string) {
			swGit(t, main, "switch", "--quiet", "--detach", "main")
		}},
		{"AC-3 on a branch trunk has moved past", func(t *testing.T, main string) {
			swGit(t, main, "branch", "old")
			swCommitFile(t, main, "CHANGES.md", "trunk moved on\n", "later work")
			swGit(t, main, "switch", "--quiet", "old")
		}},
		{"AC-3 with work nobody committed", func(t *testing.T, main string) {
			writeFile(t, main, "notes/idea.md", "half a thought\n")
			writeFile(t, main, "CLAUDE.md", "edited, and not committed\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := swRepository(t, wtStory{id: "US-001"})
			tc.prepare(t, main)
			trunk := swRev(t, main, "refs/heads/main")
			head := swRev(t, main, "HEAD")
			stray := map[string]string{}
			for _, rel := range []string{"notes/idea.md", "CLAUDE.md"} {
				stray[rel] = wtRead(t, filepath.Join(main, rel))
			}

			r := wtRun(t, main, swSessionA, "", "start", "--json")
			for _, code := range []string{swNotOnTrunk, swUncommitted} {
				if strings.Contains(r.stdout+r.stderr, code) {
					t.Fatalf("start refused over the main checkout with %s:\n%s%s", code, r.stdout, r.stderr)
				}
			}
			if r.code != 0 {
				t.Fatalf("start exited %d:\n%s%s", r.code, r.stdout, r.stderr)
			}
			wt := swCheckStarted(t, main, "US-001", swSessionA, decode[swStart](t, r))
			if got := swRev(t, wt, "HEAD"); got != trunk {
				t.Errorf("the story branch was cut from %s, want the tip of trunk %s", got, trunk)
			}
			if got := swRev(t, main, "HEAD"); got != head {
				t.Errorf("the main checkout's HEAD moved from %s to %s", head, got)
			}
			// The person's work stays in their checkout, and the story's starts
			// from trunk as it was committed.
			if wtExists(filepath.Join(wt, "notes", "idea.md")) || wtExists(filepath.Join(wt, "spike.md")) {
				t.Error("the worktree has work that is not on trunk")
			}
			if got, want := wtRead(t, filepath.Join(wt, "CLAUDE.md")), swGit(t, main, "show", "main:CLAUDE.md"); strings.TrimSpace(got) != want {
				t.Errorf("the worktree's CLAUDE.md is not trunk's:\n%s", got)
			}
			for rel, body := range stray {
				if got := wtRead(t, filepath.Join(main, rel)); got != body {
					t.Errorf("%s in the main checkout changed", rel)
				}
			}
		})
	}
}

// commands.smoke is about trunk, which the new worktree is: it runs there, and
// never in the main checkout, whose files are nobody's story.
func TestAC3TheSmokeCheckRunsInTheNewWorktree(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"})
	// Fails wherever the person's own file is, which is only the main checkout.
	swSmoke(t, main, "test ! -f only-in-the-main-checkout && touch smoke-ran-here")
	writeFile(t, main, "only-in-the-main-checkout", "a person's file\n")

	r := wtRun(t, main, swSessionA, "", "start", "--json")
	if r.code != 0 {
		t.Fatalf("start was refused, so the smoke check ran somewhere other than the new worktree:\n%s%s", r.stdout, r.stderr)
	}
	wt := swCheckStarted(t, main, "US-001", swSessionA, decode[swStart](t, r))
	if !wtExists(filepath.Join(wt, "smoke-ran-here")) {
		t.Error("the smoke check did not run in the new worktree")
	}
	if wtExists(filepath.Join(main, "smoke-ran-here")) {
		t.Error("the smoke check ran in the main checkout")
	}
}

// swRemote is a repository with trunk pushed to an origin and git.remote on,
// and the commit trunk and origin are both at.
func swRemote(t *testing.T) (main, first string) {
	t.Helper()
	main = wtRepositoryWith(t, func(root string) { setGitOnDisk(t, root, "remote", true) }, wtStory{id: "US-001"})
	swGit(t, main, "remote", "add", "origin", bareRemote(t))
	swGit(t, main, "push", "--quiet", "origin", "main")
	return main, swRev(t, main, "HEAD")
}

// Trunk behind origin is still trunk behind origin, and it is trunk that is
// counted -- the branch the story is cut from -- not whatever the main
// checkout has checked out, which AC-3 no longer holds to trunk.
func TestAC3ATrunkBehindItsRemoteStillHoldsANewStoryBack(t *testing.T) {
	t.Run("AC-3 the main checkout on trunk", func(t *testing.T) {
		main, first := swRemote(t)
		swCommitFile(t, main, "CHANGES.md", "someone else's work\n", "theirs")
		swGit(t, main, "push", "--quiet", "origin", "main")
		swGit(t, main, "reset", "--quiet", "--hard", first)
		refs := swRefs(t, main)

		if out := swRefused(t, swTrunkBehind, main, swSessionA, "start"); !strings.Contains(out, "1 commit behind") {
			t.Errorf("the refusal does not say how far behind:\n%s", out)
		}
		swCheckNothingLeft(t, main, "US-001", refs)
	})
	t.Run("AC-3 the main checkout on a branch that has what trunk lacks", func(t *testing.T) {
		main, first := swRemote(t)
		swCommitFile(t, main, "CHANGES.md", "someone else's work\n", "theirs")
		swGit(t, main, "push", "--quiet", "origin", "main")
		// HEAD has origin's commit, and trunk, which the story is cut from,
		// does not: counted from HEAD, trunk would read as up to date.
		swGit(t, main, "switch", "--quiet", "--create", "spike")
		swGit(t, main, "branch", "--force", "main", first)
		refs := swRefs(t, main)

		if out := swRefused(t, swTrunkBehind, main, swSessionA, "start"); !strings.Contains(out, "1 commit behind") {
			t.Errorf("the refusal does not say how far trunk is behind:\n%s", out)
		}
		swCheckNothingLeft(t, main, "US-001", refs)
	})
	t.Run("AC-3 the main checkout on a branch behind a trunk that is up to date", func(t *testing.T) {
		main, first := swRemote(t)
		latest := swCommitFile(t, main, "CHANGES.md", "someone else's work\n", "theirs")
		swGit(t, main, "push", "--quiet", "origin", "main")
		swGit(t, main, "switch", "--quiet", "--create", "old", first)

		wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
		if got := swRev(t, wt, "HEAD"); got != latest {
			t.Errorf("the story branch was cut from %s, want trunk's tip %s", got, latest)
		}
	})
}

// ---------------------------------------------------------------- AC-4

// swCheckNothingLeft holds a refused start to AC-4: no worktree for the story,
// no branch of its own that was not there before (refs is every branch before
// the start), no claim, no change of status, and nothing written for it in the
// main checkout's loop state.
func swCheckNothingLeft(t *testing.T, main, id, refs string) {
	t.Helper()
	if got := swRefs(t, main); got != refs {
		t.Errorf("the refused start changed the branches:\nbefore:\n%s\nafter:\n%s", refs, got)
	}
	if wt := swWorktreeOf(main, id); swListed(t, main, wt) {
		t.Errorf("the refused start left a worktree at %s", wt)
	}
	if got := swClaimed(t, main, id); got != "" {
		t.Errorf("the refused start left a claim on %s naming %s", id, got)
	}
	if got := wtStatuses(t, main)[id]; got != string(model.StatusReady) {
		t.Errorf("the refused start left %s %s, want ready", id, got)
	}
	if wtExists(filepath.Join(main, ".sdlc", "state", "active")) {
		t.Error("the refused start wrote the main checkout's .sdlc/state/active")
	}
	if wtExists(wtRecordPath(main, id)) {
		t.Errorf("the refused start wrote a record for %s", id)
	}
}

func TestAC4ASmokeCheckThatFailsInTheNewWorktreeLeavesNothingBehind(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	swSmoke(t, main, `echo "invoice.go:3:18: undefined: x"; exit 1`)
	refs, backlog := swRefs(t, main), wtRead(t, filepath.Join(main, "user_stories.json"))

	out := swRefused(t, swTrunkBroken, main, swSessionA, "start")
	if !strings.Contains(out, "undefined: x") {
		t.Errorf("the refusal does not carry what the smoke check said:\n%s", out)
	}
	if !strings.Contains(out, "worktree") {
		t.Errorf("the refusal does not say the smoke check ran in the new worktree:\n%s", out)
	}
	if strings.Contains(out, swWorktreeNotMade) {
		t.Errorf("a failing smoke check reads as a worktree that could not be made:\n%s", out)
	}
	swCheckNothingLeft(t, main, "US-001", refs)
	if got := wtRead(t, filepath.Join(main, "user_stories.json")); got != backlog {
		t.Error("the refused start rewrote the backlog")
	}
	if entries, _ := os.ReadDir(filepath.Join(main, ".sdlc", "worktrees")); len(entries) > 0 {
		for _, e := range entries {
			if e.Name() == "US-001" {
				t.Error("the refused start left the worktree's directory behind")
			}
		}
	}

	// Nothing left behind is nothing in the way: trunk fixed, the same story
	// starts.
	swSmoke(t, main, "true")
	swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
}

func TestAC4AWorktreeThatCannotBeMadeIsRefusedWithSDLCE0053AndLeavesNothingBehind(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		// prepare puts something in the way, and returns what must still be
		// there, by path, afterwards: what start did not make, it never removes.
		prepare func(t *testing.T, main string) map[string]string
		// says is what the refusal has to name.
		says []string
		// place is whether it has to name where the worktree was to go.
		place bool
	}{
		{"AC-4 a branch of that name is already there", "US-001", func(t *testing.T, main string) map[string]string {
			swGit(t, main, "switch", "--quiet", "--create", swBranchOf("US-001"))
			swCommitFile(t, main, "mine.md", "a person's work on a branch of that name\n", "mine")
			swGit(t, main, "switch", "--quiet", "main")
			return nil
		}, []string{swBranchOf("US-001")}, false},
		{"AC-4 a directory is already where the worktree goes", "US-001", func(t *testing.T, main string) map[string]string {
			notes := filepath.Join(swWorktreeOf(main, "US-001"), "notes.md")
			writeFile(t, filepath.Dir(notes), "notes.md", "a person's notes\n")
			return map[string]string{notes: "a person's notes\n"}
		}, []string{"US-001"}, true},
		{"AC-4 a worktree an interrupted start left", "US-001", func(t *testing.T, main string) map[string]string {
			wt := swWorktreeOf(main, "US-001")
			swGit(t, main, "worktree", "add", "--quiet", "-b", swBranchOf("US-001"), wt, "main")
			writeFile(t, wt, "draft.go", "package draft\n")
			return map[string]string{filepath.Join(wt, "draft.go"): "package draft\n"}
		}, []string{"git worktree remove"}, true},
		{"AC-4 the story id cannot name a branch", "US-1.lock", func(*testing.T, string) map[string]string {
			return nil
		}, []string{"US-1.lock"}, false},
		{"AC-4 the worktrees directory is a file", "US-001", func(t *testing.T, main string) map[string]string {
			path := filepath.Join(main, ".sdlc", "worktrees")
			writeFile(t, main, ".sdlc/worktrees", "not a directory\n")
			return map[string]string{path: "not a directory\n"}
		}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := swRepository(t, wtStory{id: tc.id}, wtStory{id: "US-900"})
			kept := tc.prepare(t, main)
			refs, worktrees := swRefs(t, main), swWorktrees(t, main)
			backlog := wtRead(t, filepath.Join(main, "user_stories.json"))

			out := swRefused(t, swWorktreeNotMade, main, swSessionA, "start", tc.id)
			for _, want := range tc.says {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal does not name %q:\n%s", want, out)
				}
			}
			if dir := filepath.Join(main, ".sdlc", "worktrees"); tc.place &&
				!wtNames(out, dir) && !strings.Contains(out, ".sdlc/worktrees") {
				t.Errorf("the refusal does not say where the worktree was to go:\n%s", out)
			}
			if got := swRefs(t, main); got != refs {
				t.Errorf("the refused start changed the branches:\nbefore:\n%s\nafter:\n%s", refs, got)
			}
			if got := swWorktrees(t, main); !slices.Equal(got, worktrees) {
				t.Errorf("the refused start changed the worktrees from %v to %v", worktrees, got)
			}
			if got := wtRead(t, filepath.Join(main, "user_stories.json")); got != backlog {
				t.Error("the refused start rewrote the backlog")
			}
			if got := swClaimed(t, main, tc.id); got != "" {
				t.Errorf("the refused start claimed %s for %s", tc.id, got)
			}
			if wtExists(filepath.Join(main, ".sdlc", "state", "active")) || wtExists(wtRecordPath(main, tc.id)) {
				t.Error("the refused start wrote loop state for the story")
			}
			for path, body := range kept {
				if got := wtRead(t, path); got != body {
					t.Errorf("the refused start changed or removed %s, which it did not make", path)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- AC-5

// swShape is everything a start that creates nothing leaves as it was: the
// working trees, every branch and where it is, and what .sdlc/worktrees holds.
func swShape(t *testing.T, main string) string {
	t.Helper()
	var names []string
	if entries, err := os.ReadDir(filepath.Join(main, ".sdlc", "worktrees")); err == nil {
		for _, e := range entries {
			names = append(names, e.Name())
		}
	}
	return strings.Join(swWorktrees(t, main), "\n") + "\n" + swRefs(t, main) + "\n" + strings.Join(names, " ")
}

func TestAC5StartingAStoryThatHasAWorktreeAgainPrintsTheSamePathAndBranchAndMakesNothing(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	first := swStarted(t, main, swSessionA)
	wt := swCheckStarted(t, main, "US-001", swSessionA, first)
	shape := swShape(t, main)

	for _, tc := range []struct {
		name, dir string
		args      []string
	}{
		{"AC-5 from the main checkout", main, nil},
		{"AC-5 from the main checkout, named", main, []string{"US-001"}},
		{"AC-5 from the worktree", wt, nil},
		{"AC-5 from the worktree, named", wt, []string{"US-001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := swStarted(t, tc.dir, swSessionA, tc.args...)
			if !got.Resume {
				t.Error("start did not resume the story")
			}
			if got.Story != "US-001" || got.Worktree != first.Worktree || got.Branch != first.Branch {
				t.Errorf("start said %s in %q on %q, want what the first start said: %s in %q on %q",
					got.Story, got.Worktree, got.Branch, first.Story, first.Worktree, first.Branch)
			}
			if now := swShape(t, main); now != shape {
				t.Errorf("start made something:\nbefore:\n%s\nafter:\n%s", shape, now)
			}
		})
	}
	t.Run("AC-5 printed", func(t *testing.T) {
		out := wtMust(t, main, swSessionA, "", "start").stdout
		if !wtNames(out, wt) || !strings.Contains(out, swBranchOf("US-001")) {
			t.Errorf("start does not print the same path and branch:\n%s", out)
		}
	})
}

// A new Claude Code session picks a story up from the main checkout by naming
// it, as /sdlc:next takes a story over in one checkout: in the story's own
// worktree, which the session is then bound in, with the takeover on the
// record as a takeover in one checkout puts it there (the analysis gate's
// fifth question; THREATS T10).
func TestAC5ANewSessionTakesAStoryOverFromTheMainCheckoutInItsWorktree(t *testing.T) {
	// What a takeover leaves on the record in one checkout, with worktrees off.
	single := wtRepository(t, wtStory{id: "US-001"})
	wtStarted(t, single, swSessionA)
	before := len(swRecord(t, single, "US-001").Events)
	wtMust(t, single, swSessionB, "", "start", "US-001")
	want := swEventsSince(swRecord(t, single, "US-001"), before)

	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	first := swStarted(t, main, swSessionA)
	wt := swCheckStarted(t, main, "US-001", swSessionA, first)
	before = len(swRecord(t, main, "US-001").Events)

	got := swStarted(t, main, swSessionB, "US-001")
	if !got.Resume || got.Worktree != first.Worktree || got.Branch != first.Branch || got.Session != swSessionB {
		t.Errorf("the takeover said %+v, want US-001 resumed in %q on %q in %s", got, first.Worktree, first.Branch, swSessionB)
	}
	swCheckStarted(t, main, "US-001", swSessionB, got)
	if wtExists(filepath.Join(main, ".sdlc", "state", "session")) {
		t.Error("the takeover bound the session in the main checkout")
	}
	if got := swEventsSince(swRecord(t, main, "US-001"), before); !slices.Equal(got, want) {
		t.Errorf("the takeover from the main checkout put %v on the record, and one in a single checkout puts %v", got, want)
	}
	// The story is the new session's now, and the one that had it is told so.
	swRefused(t, swSessionHere, wt, swSessionA, "gate", "dor", "pass", "--note", "x")
}

// One session works one story (US-029.1's decision, kept): a session whose
// story is in a worktree is refused another from the main checkout, and
// nothing is made for it.
func TestAC5ASessionWorkingAStoryInItsWorktreeIsRefusedAnotherFromTheMainCheckout(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	refs := swRefs(t, main)

	swRefused(t, wtHeldElsewhere, main, swSessionA, "start", "US-002")
	swCheckNothingLeft(t, main, "US-002", refs)
}

// A story already held where it is -- the main checkout, as an iteration
// running at upgrade is -- resumes there, and nothing is made for it (the
// analysis gate's fourth question).
func TestAC5AStoryTheMainCheckoutAlreadyHoldsResumesThereAndNoWorktreeIsMade(t *testing.T) {
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	if got := wtStarted(t, main, swSessionA); got.Story != "US-001" {
		t.Fatalf("start with worktrees off gave %q", got.Story)
	}
	// The upgrade: the switch goes, and worktrees are on.
	var cfg map[string]any
	wtReadJSON(t, filepath.Join(main, ".sdlc", "config.json"), &cfg)
	delete(cfg["git"].(map[string]any), "worktrees")
	wtWriteJSON(t, filepath.Join(main, ".sdlc", "config.json"), cfg)
	shape := swShape(t, main)

	got := swStarted(t, main, swSessionA)
	if got.Story != "US-001" || !got.Resume {
		t.Fatalf("start gave %+v, want US-001 resumed", got)
	}
	if got.Worktree != "" || got.Branch != "" {
		t.Errorf("start names a worktree %q and branch %q for a story the main checkout holds", got.Worktree, got.Branch)
	}
	if now := swShape(t, main); now != shape {
		t.Errorf("start made something:\nbefore:\n%s\nafter:\n%s", shape, now)
	}
}

// ---------------------------------------------------------------- AC-7

// Another story landing on trunk is not this story's business until it
// commits: every gate before the commit passes in the worktree, and what the
// gates measure is the worktree, against the commit the branch was cut from.
func TestAC7TrunkMovingRefusesNoGateBeforeTheCommitAndTheStoryIsMeasuredInItsWorktree(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"})
	cut := swRev(t, main, "refs/heads/main")
	wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))

	moved := swCommitFile(t, main, "CHANGES.md", "another story's work\n", "another story lands")
	if moved == cut {
		t.Fatal("precondition: trunk did not move")
	}

	swReach(t, wt, swSessionA, model.GateCommit)

	r := swRecord(t, main, "US-001")
	if r.Base != cut {
		t.Errorf("the record's base is %s, want the commit the branch was cut from, %s", r.Base, cut)
	}
	if r.CommitBase != cut {
		t.Errorf("the code review measured from %s, want the commit the branch was cut from, %s", r.CommitBase, cut)
	}
	subject := func(root string) string {
		t.Helper()
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return tree
	}
	inWorktree, inMain := subject(wt), subject(main)
	if inWorktree == inMain {
		t.Fatal("precondition: the worktree and the main checkout hold the same tree")
	}
	// The code review is stamped with the tree it reviewed; the design
	// review's subject is the plan, and is not the tree's.
	reviewed := 0
	for _, review := range r.Reviews {
		if review.Gate != model.GateCodeReview {
			continue
		}
		reviewed++
		if review.Subject != inWorktree {
			t.Errorf("%s's code review is stamped %s, want the worktree's tree %s", review.Role, review.Subject, inWorktree)
		}
	}
	if reviewed == 0 {
		t.Fatal("no code review was recorded")
	}
	if got := swRev(t, wt, "HEAD"); got != cut {
		t.Errorf("the worktree's HEAD is %s, want %s: the story has not committed", got, cut)
	}
}

// ---------------------------------------------------------------- AC-8

func TestAC8ASecondSessionBeginsASecondStoryInASecondWorktreeAndTheFirstGoesOnAsAlone(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt1 := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	swReach(t, wt1, swSessionA, model.GateTestsFrozen)

	second := swStarted(t, main, swSessionB)
	wt2 := swCheckStarted(t, main, "US-002", swSessionB, second)
	if config.SamePath(wt1, wt2) {
		t.Fatal("the second story was begun in the first story's worktree")
	}
	if claimed := swClaimed(t, main, "US-001"); !config.SamePath(claimed, wt1) {
		t.Errorf("the claim on US-001 names %q after the second start, want %s", claimed, wt1)
	}
	wtMust(t, wt2, swSessionB, "", "gate", "dor", "pass", "--note", "ready")
	// The second story's work lands on its own branch.
	swCommitFile(t, wt2, "refunds.go", "package refunds\n", "the second story's work")

	// The first story's gates fail and pass as they would with nobody else about.
	swRefused(t, "SDLC-E0020", wt1, swSessionA, "gate", "tests_frozen", "pass", "--note", "x")
	swPass(t, wt1, swSessionA, model.GateTestsFrozen, model.GateDesignReview)

	if got := decode[statusPayload](t, wtMust(t, wt1, swSessionA, "", "status", "--json")); got.Active != "US-001" {
		t.Errorf("the first worktree's status names %q", got.Active)
	}
	if got := decode[statusPayload](t, wtMust(t, wt2, swSessionB, "", "status", "--json")); got.Active != "US-002" {
		t.Errorf("the second worktree's status names %q", got.Active)
	}
	if wtExists(filepath.Join(wt1, "refunds.go")) || wtExists(filepath.Join(wt2, "internal", "invoice_test.go")) {
		t.Error("one story's files are in the other's worktree")
	}
	if wtExists(filepath.Join(wt2, ".sdlc", "state", "tests.lock")) {
		t.Error("the first story's freeze reached the second story's worktree")
	}
}

// ---------------------------------------------------------------- AC-9

func TestAC9TheSwitchDecidesWhetherStartMakesAWorktreeAndAbsentMeansOn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(t *testing.T) func(root string)
		made      bool
	}{
		{"AC-9 absent", func(*testing.T) func(string) { return nil }, true},
		{"AC-9 true", func(t *testing.T) func(string) {
			return func(root string) { setGitOnDisk(t, root, "worktrees", true) }
		}, true},
		{"AC-9 false", func(t *testing.T) func(string) {
			return func(root string) { worktreesOff(t, root) }
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := wtRepositoryWith(t, tc.configure(t), wtStory{id: "US-001"})
			r := wtMust(t, main, swSessionA, "", "start", "--json")
			got := decode[swStart](t, r)
			if tc.made {
				swCheckStarted(t, main, "US-001", swSessionA, got)
				return
			}
			// Off, start is the released tool's: the story is this checkout's,
			// and nothing about a worktree is made or said.
			var fields map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"worktree", "branch"} {
				if _, ok := fields[key]; ok {
					t.Errorf("start --json with worktrees off has %q: %s", key, r.stdout)
				}
			}
			if wtExists(filepath.Join(main, ".sdlc", "worktrees")) || swHasBranch(t, main, swBranchOf("US-001")) {
				t.Error("start made a worktree or a branch with worktrees off")
			}
			if active := strings.TrimSpace(wtRead(t, filepath.Join(main, ".sdlc", "state", "active"))); active != "US-001" {
				t.Errorf("the main checkout's active names %q, want US-001", active)
			}
			if claimed := swClaimed(t, main, "US-001"); !config.SamePath(claimed, main) {
				t.Errorf("the claim names %q, want the main checkout", claimed)
			}
			out := wtMust(t, main, swSessionA, "", "start").stdout
			if strings.Contains(out, "worktree") || strings.Contains(out, swBranchOf("US-001")) {
				t.Errorf("start with worktrees off talks of a worktree:\n%s", out)
			}
		})
	}
}

// With worktrees off, a new story begins in the checkout start runs in, so
// that checkout is held to trunk as the released tool held it. SDLC-E0040 is
// trunk_test.go's, whose fixtures turn worktrees off.
func TestAC9WithWorktreesOffANewStoryIsHeldToTrunkAsTheReleasedToolHeldIt(t *testing.T) {
	t.Run("AC-9 SDLC-E0038", func(t *testing.T) {
		main := wtRepository(t, wtStory{id: "US-001"})
		swGit(t, main, "switch", "--quiet", "--create", "spike")
		if out := swRefused(t, swNotOnTrunk, main, swSessionA, "start"); !strings.Contains(out, `"spike"`) {
			t.Errorf("the refusal does not say which branch HEAD is on:\n%s", out)
		}
	})
	t.Run("AC-9 SDLC-E0039", func(t *testing.T) {
		main := wtRepository(t, wtStory{id: "US-001"})
		writeFile(t, main, "notes/idea.md", "half a thought\n")
		if out := swRefused(t, swUncommitted, main, swSessionA, "start"); !strings.Contains(out, "notes/") {
			t.Errorf("the refusal does not name the stray work:\n%s", out)
		}
	})
	t.Run("AC-9 SDLC-E0041", func(t *testing.T) {
		main := wtRepository(t, wtStory{id: "US-001"})
		swSmoke(t, main, `echo "broken on trunk"; exit 1`)
		if out := swRefused(t, swTrunkBroken, main, swSessionA, "start"); !strings.Contains(out, "broken on trunk") {
			t.Errorf("the refusal does not carry what the smoke check said:\n%s", out)
		}
	})
}

// ---------------------------------------------------------------- T3

// THREATS T3: git's own variables, exported in the session start runs in,
// name another repository. They must not decide where the worktree is made,
// what it is cut from, or what the gates measure in it afterwards.
func TestT3GitsVariablesDoNotSteerTheWorktreeStartMakesOrWhatTheGatesMeasure(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_COMMON_DIR", "GIT_INDEX_FILE"} {
		t.Run("T3 "+name, func(t *testing.T) {
			main := swRepository(t, wtStory{id: "US-001"})
			trunk := swRev(t, main, "refs/heads/main")
			decoy := wtTempDir(t)
			swGit(t, decoy, "init", "--quiet", "--initial-branch=main")
			swCommitFile(t, decoy, "decoy.md", "somebody else's repository\n", "decoy")
			value := filepath.Join(decoy, ".git")
			if name == "GIT_INDEX_FILE" {
				value = filepath.Join(decoy, ".git", "index")
			}
			decoyBefore := swRefs(t, decoy) + "\n" + strings.Join(swWorktrees(t, decoy), "\n") +
				"\n" + wtRead(t, filepath.Join(decoy, ".git", "index"))
			t.Setenv(name, value)

			r := wtRun(t, main, swSessionA, "", "start", "--json")
			if r.code != 0 {
				t.Fatalf("start with %s at another repository exited %d:\n%s%s", name, r.code, r.stdout, r.stderr)
			}
			wt := swCheckStarted(t, main, "US-001", swSessionA, decode[swStart](t, r))
			if got := swRev(t, wt, "HEAD"); got != trunk {
				t.Errorf("the story branch was cut from %s, want this repository's trunk %s", got, trunk)
			}
			if got := swRecord(t, main, "US-001").Base; got != trunk {
				t.Errorf("the record's base is %s, want %s", got, trunk)
			}
			// Measured in the worktree: HEAD there is where the story started.
			wtMust(t, wt, swSessionA, "", "gate", "dor", "pass", "--note", "ready")

			decoyAfter := swRefs(t, decoy) + "\n" + strings.Join(swWorktrees(t, decoy), "\n") +
				"\n" + wtRead(t, filepath.Join(decoy, ".git", "index"))
			if decoyAfter != decoyBefore {
				t.Errorf("start or the gate changed the repository %s names", name)
			}
		})
	}
}

// A story stopped in its worktree is that worktree's until a person releases
// it: a plain start from the main checkout passes it over for the next story,
// and naming it, or starting in its worktree, picks it up there (the analysis
// gate's third question).
func TestAC5AndAC8APlainStartPassesOverAStoppedStoryItsWorktreeHoldsAndNamingItResumesItThere(t *testing.T) {
	for _, tc := range []struct {
		name string
		// resume picks the stopped story up again, from main or its worktree.
		resume func(t *testing.T, main, wt string) swStart
	}{
		{"AC-5 named from the main checkout", func(t *testing.T, main, _ string) swStart {
			return swStarted(t, main, swSessionC, "US-001")
		}},
		{"AC-5 from its worktree", func(t *testing.T, _, wt string) swStart {
			return swStarted(t, wt, swSessionC)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
			wtMust(t, wt, "", "", "stop")

			swCheckStarted(t, main, "US-002", swSessionB, swStarted(t, main, swSessionB))

			got := tc.resume(t, main, wt)
			if !got.Resume {
				t.Error("the stopped story was begun again rather than resumed")
			}
			swCheckStarted(t, main, "US-001", swSessionC, got)
		})
	}
}

// ---------------------------------------------------------------- D7

// The story's last decision carries six items from US-029.1's code review;
// these are the ones about what start and release say and write.

// swMoved is a single checkout whose story, US-001, had its tests frozen and
// was stopped, after which the checkout itself was moved: the claim names
// where it was, which no longer exists.
func swMoved(t *testing.T) (moved string) {
	t.Helper()
	was := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wtStarted(t, was, swSessionA)
	swReach(t, was, swSessionA, model.GatePlan)
	wtMust(t, was, "", "", "stop")
	moved = filepath.Join(wtTempDir(t), "moved")
	t.Chdir(filepath.Dir(moved))
	if err := os.Rename(was, moved); err != nil {
		t.Fatal(err)
	}
	return moved
}

// A checkout that moved is still the checkout its story's work and freeze are
// in. Release, and the start after it, told the person the opposite: that the
// freeze stayed behind and has to be lifted and made again, and that the work
// the checkout had not committed was somewhere else.
func TestD7AMovedCheckoutIsNotToldItsFreezeAndItsWorkStayedBehind(t *testing.T) {
	moved := swMoved(t)
	lock := filepath.Join(moved, ".sdlc", "state", "tests.lock")
	frozen := wtRead(t, lock)
	if frozen == "" {
		t.Fatal("precondition: the moved checkout has no freeze")
	}
	swRefused(t, wtHolderMissing, moved, "", "start", "US-001")

	out := wtMust(t, moved, "", "", "release", "US-001", "--reason", "the checkout was moved").stdout
	for _, untrue := range []string{"the freeze does not move with it", "sdlc unfreeze"} {
		if strings.Contains(out, untrue) {
			t.Errorf("release from the checkout the tests were frozen in says %q:\n%s", untrue, out)
		}
	}
	r := wtMust(t, moved, swSessionA, "", "start", "US-001")
	if strings.Contains(r.stdout+r.stderr, "is not in this one") {
		t.Errorf("start in the checkout that has the story's work says the work is somewhere else:\n%s%s", r.stdout, r.stderr)
	}
	if got := wtRead(t, lock); got != frozen {
		t.Error("the freeze changed")
	}
	// The freeze here is the story's, and the next gate passes on it.
	swPass(t, moved, swSessionA, model.GatePlan, model.GateDesignReview)
}

// Where the checkout that held the story really is gone, what release says
// about the freeze is still true, and is still said.
func TestD7AStoryReleasedFromACheckoutThatIsGoneIsStillToldItsFreezeStayedThere(t *testing.T) {
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	gone := wtWorktree(t, main, "gone")
	wtStarted(t, gone, swSessionA)
	swReach(t, gone, swSessionA, model.GatePlan)
	wtMust(t, gone, "", "", "stop")
	t.Chdir(main)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	out := wtMust(t, main, "", "", "release", "US-001", "--reason", "the worktree was deleted").stdout
	for _, want := range []string{"Released US-001", "sdlc unfreeze", "sdlc freeze"} {
		if !strings.Contains(out, want) {
			t.Errorf("release does not say %q:\n%s", want, out)
		}
	}
}

// movedHere carries a single checkout's story on where the checkout now is,
// when the move came while the story ran.
func TestD7ACheckoutMovedWhileItsStoryRunsCarriesOnWhereItIsNow(t *testing.T) {
	was := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wtStarted(t, was, swSessionA)
	swReach(t, was, swSessionA, model.GateAnalysis)
	moved := filepath.Join(wtTempDir(t), "moved")
	t.Chdir(filepath.Dir(moved))
	if err := os.Rename(was, moved); err != nil {
		t.Fatal(err)
	}

	if got := decode[startPayload](t, wtMust(t, moved, swSessionA, "", "start", "--json")); got.Story != "US-001" || !got.Resume {
		t.Errorf("start in the moved checkout gave %+v, want US-001 carried on", got)
	}
	swPass(t, moved, swSessionA, model.GateAnalysis, model.GatePlan)
	if claimed := swClaimed(t, moved, "US-001"); !config.SamePath(claimed, moved) {
		t.Errorf("the claim names %q, want the checkout where it is now, %s", claimed, moved)
	}
}

// swNoteOn is the line of text that talks about id, or "".
func swNoteOn(text, id string) string {
	for line := range strings.Lines(text) {
		if strings.Contains(line, id) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// A plain start cannot tell a checkout that was deleted from one that was
// moved, so its note says both, says who acts, and says how.
func TestD7APlainStartPassingOverAGoneHolderSaysTheCheckoutMayHaveMovedAndWhatToDo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status model.Status
		// last is the command the note ends with.
		last string
	}{
		{"D7 in progress", model.StatusInProgress, "sdlc start US-001"},
		{"D7 waiting on a person", model.StatusAwaitingHuman, "sdlc approve US-001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			gone := wtWorktree(t, main, "gone")
			wtStarted(t, gone, wtSessionWorktree)
			wtMust(t, gone, "", "", "stop")
			t.Chdir(main)
			if err := os.RemoveAll(gone); err != nil {
				t.Fatal(err)
			}
			wtSetStatus(t, main, "US-001", tc.status)

			r := wtRun(t, main, swSessionA, "", "start")
			note := swNoteOn(r.stderr, "US-001")
			if note == "" {
				t.Fatalf("start says nothing of the story it passed over:\n%s%s", r.stdout, r.stderr)
			}
			for _, want := range []string{"moved", "an assistant stops and tells the person", "kept",
				`sdlc release US-001 --reason "..."`, tc.last} {
				if !strings.Contains(note, want) {
					t.Errorf("the note does not say %q:\n%s", want, note)
				}
			}
			if !strings.Contains(note, "uncommitted") && !strings.Contains(note, "not committed") {
				t.Errorf("the note does not say what becomes of the work nobody committed here:\n%s", note)
			}
			if at := strings.LastIndex(note, "sdlc "); at < 0 || !strings.HasPrefix(note[at:], tc.last) {
				t.Errorf("the note does not end with %q:\n%s", tc.last, note)
			}
			if tc.status == model.StatusInProgress && strings.Contains(note, "sdlc approve") {
				t.Errorf("the note sends a person to approve a story nobody escalated:\n%s", note)
			}
			if strings.Contains(note, `\`) {
				t.Errorf("the note prints a backslash:\n%s", note)
			}
		})
	}
}

// A start that carries the session's own story on picks nothing, so it passes
// nothing over and has nothing to say about a story a gone checkout holds. A
// start that picks does say it.
func TestD7AStartThatResumesTheSessionsOwnStoryPassesNothingOverAndSaysNothingAboutIt(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"}, wtStory{id: "US-003"})
	swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	// US-003 was started in a checkout that is gone.
	wtSetStatus(t, main, "US-003", model.StatusInProgress)
	gone := filepath.Join(wtTempDir(t), "gone")
	wtWriteJSON(t, rlClaimFile(main, "US-003"),
		map[string]string{"schema": "sdlc/claim/1", "story": "US-003", "checkout": gone})

	r := wtRun(t, main, swSessionA, "", "start", "--json")
	if got := decode[swStart](t, r); got.Story != "US-001" || !got.Resume {
		t.Fatalf("start gave %+v, want the session's own US-001 carried on", got)
	}
	if note := swNoteOn(r.stderr, "US-003"); note != "" {
		t.Errorf("a start that picked nothing talks of a story it passed over:\n%s", note)
	}

	r = wtRun(t, main, swSessionC, "", "start", "--json")
	if got := decode[swStart](t, r); got.Story != "US-002" {
		t.Fatalf("a new session's start gave %q, want US-002", got.Story)
	}
	if note := swNoteOn(r.stderr, "US-003"); note == "" {
		t.Errorf("a start that passed over US-003 does not say so:\n%s", r.stderr)
	}
}

// THREATS T10 and US-029.1's code review: a checkout's path goes in its claim
// and nowhere else the loop keeps, and a story worked in a worktree writes its
// record from there.
func TestD7ARecordWrittenFromAWorktreeNamesNoCheckoutPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		// start begins US-001 in a worktree, and returns the main checkout and it.
		start func(t *testing.T) (main, wt string)
	}{
		{"D7 a story worktree", func(t *testing.T) (string, string) {
			main := swRepository(t, wtStory{id: "US-001"})
			return main, swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
		}},
		{"D7 a worktree made by hand", func(t *testing.T) (string, string) {
			main := wtRepository(t, wtStory{id: "US-001"})
			wt := wtWorktree(t, main, "wt")
			wtStarted(t, wt, swSessionA)
			return main, wt
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := tc.start(t)
			swReach(t, wt, swSessionA, model.GateCommit)
			wtMust(t, wt, swSessionA, "", "stop")

			// Every directory the test made is under this one, whichever
			// spelling of it a path would be written with.
			marker := filepath.Base(filepath.Dir(main))
			kept := wtFiles(t, filepath.Join(main, ".sdlc", "stories", "US-001"))
			kept["user_stories.json"] = wtRead(t, filepath.Join(main, "user_stories.json"))
			if len(kept) < 2 {
				t.Fatalf("precondition: nothing was written for the story: %v", slices.Collect(maps.Keys(kept)))
			}
			for name, body := range kept {
				if strings.Contains(body, marker) {
					t.Errorf("%s names a checkout's path:\n%s", name, body)
				}
			}
		})
	}
}
