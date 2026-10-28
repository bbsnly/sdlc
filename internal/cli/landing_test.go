package cli

// US-029.3 in process: a story worked in its own worktree lands on trunk by
// fast-forward, and its worktree and branch are taken away.
//
// Landing is `sdlc stop`, run in the story's worktree by the session that
// holds it, once every gate has passed: the command SDLC-E0033 already sends a
// session with a finished story to, and the one AC-11 names beside the commit
// gate. Before it, the bookkeeper's CODEMAP.md change is committed on
// story/<ID> while the session is still held (the analysis's route R1), so it
// is carried to trunk with the story. A landing that is refused leaves the
// iteration running: the story is still held in its worktree, and a stop
// after the refusal has been dealt with lands it.
//
// The repositories are real, made as a person makes them, and ignore the
// loop's own state (/.sdlc/state/), so that what a story commits is its work.
// Everything here reads what a caller can see -- stop's output and exit code,
// git's answers, the record and backlog on disk -- and refusals are matched by
// the code they print, so that nothing names an identifier the story has still
// to add. The three codes the story adds are SDLC-E0054 (trunk moved since the
// branch was cut, AC-3), SDLC-E0055 (a configured command failed on the tree
// about to land, AC-4) and SDLC-E0056 (the checkout that has trunk checked out
// holds changes the fast-forward would write over, AC-6).
//
// Helpers carry an ld prefix, for landing.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	ldSession      = "ld-session"
	ldOtherSession = "ld-session-other"
)

// The codes a landing is refused with.
const (
	ldBehindTrunk  = "SDLC-E0054"
	ldCheckFailed  = "SDLC-E0055"
	ldUncommitted  = "SDLC-E0056"
	ldStale        = "SDLC-E0029"
	ldFrozenMoved  = "SDLC-E0025"
	ldNotApproved  = "SDLC-E0037"
	ldUnfrozenTest = "SDLC-E0043"
	// ldAny is any refusal, where the story leaves the code to the plan.
	ldAny = "SDLC-E"
)

const ldBilling = "package billing\n\n// Billing keeps the accounts.\n"

// ldQuality is every configured command AC-4 runs again on a rebased tree.
var ldQuality = []string{"build", "test", "lint", "fmt_check", "coverage", "mutation"}

// ldRepository is a main checkout, story worktrees on, whose trunk holds the
// project's own files and ignores the loop's state; configure, where there is
// one, runs before the first commit. With no stories given, US-001 is the one.
func ldRepository(t *testing.T, configure func(root string), stories ...wtStory) string {
	t.Helper()
	root := wtTempDir(t)
	ldRepositoryAt(t, root, configure, stories...)
	return root
}

// ldRepositoryAt is ldRepository in root, an empty directory, for a walked
// template's builder.
func ldRepositoryAt(t *testing.T, root string, configure func(root string), stories ...wtStory) {
	t.Helper()
	if len(stories) == 0 {
		stories = []wtStory{{id: "US-001"}}
	}
	wtRepositoryAt(t, root, func(root string) {
		writeFile(t, root, ".gitignore", "/.sdlc/state/\n")
		writeFile(t, root, "billing.go", ldBilling)
		writeFile(t, root, "README.md", "# Billing\n")
		writeFile(t, root, "CODEMAP.md", "# CODEMAP\n")
		writeFile(t, root, "price list.txt", "tea 1\n")
		if configure != nil {
			configure(root)
		}
	}, stories...)
}

// ldPassing is every quality command passing, coverage and mutation above the
// thresholds ldThresholds sets.
func ldPassing() map[string]string {
	commands := map[string]string{}
	for _, key := range ldQuality {
		commands[key] = "exit 0"
	}
	commands["coverage"] = "echo 95"
	commands["mutation"] = "echo 90"
	return commands
}

func ldThresholds(t *testing.T, root string) {
	t.Helper()
	setConfigOnDisk(t, root, "thresholds", map[string]any{"diff_size_cap": 0, "coverage_min": 80, "mutation_min": 70})
}

// ldWork is the story's own change: billing.go and a file with a space in its
// name changed, refund.go added.
func ldWork(t *testing.T, wt string) {
	t.Helper()
	writeFile(t, wt, "billing.go", ldBilling+"\n// Invoice is what US-001 adds.\ntype Invoice struct{}\n")
	writeFile(t, wt, "refund.go", "package billing\n\n// Refund is what US-001 adds.\ntype Refund struct{}\n")
	writeFile(t, wt, "price list.txt", "tea 1\ncoffee 2\n")
}

// ldStarted starts US-001 in its worktree from the main checkout, and takes it
// to the implementation.
func ldStarted(t *testing.T, main string) string {
	t.Helper()
	wt := swCheckStarted(t, main, "US-001", ldSession, swStarted(t, main, ldSession))
	swReach(t, wt, ldSession, model.GateImplementation)
	return wt
}

// ldFinished is US-001 worked in its worktree through every gate, the retro
// included, with its change committed on story/US-001 at the commit gate.
func ldFinished(t *testing.T, main string) string {
	t.Helper()
	wt := ldStarted(t, main)
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, "")
	return wt
}

// ldWalk says which walked template a landing test starts from (US-055 AC-3):
// the repository it is made from, and how far the story is taken in it.
type ldWalk struct {
	// finished is ldFinished's story, through the retro; otherwise ldStarted's,
	// up to the implementation.
	finished bool
	// lx is lxRepository(t, set, thresholds) rather than ldRepository(t, nil).
	lx         bool
	set        map[string]string
	thresholds map[string]any
	// stories are the backlog's stories where there is more than US-001.
	stories []wtStory
	// tier is US-001's risk tier in the backlog, where it is given one.
	tier string
	// taxTest is a test file, internal/tax_test.go, on trunk before the story.
	taxTest bool
	// trunkMoved is a commit to README.md on trunk before the story starts,
	// after the project's first: the base is then not the first commit.
	trunkMoved bool
	// spike is the main checkout switched to a branch of its own, spike, before
	// the story starts, so that no checkout has trunk checked out.
	spike bool
}

// ldWalked is the main checkout and the story's worktree that ldStarted or
// ldFinished leaves, from the repository w names, as a copy of a template the
// real commands made. The test's commands run in the worktree from ldSession,
// as they do after either.
func ldWalked(t *testing.T, w ldWalk) (main, wt string) {
	t.Helper()
	main, wt, _ = ldWalkedFrom(t, w)
	return main, wt
}

// ldWalkedFrom is ldWalked and trunk's tip as it was before the story
// started, read then by the template's builder: the commit the story's branch
// was cut from, which is also the spike branch's commit where w has one.
func ldWalkedFrom(t *testing.T, w ldWalk) (main, wt, cut string) {
	t.Helper()
	main, notes := walkedCopyNoted(t, fmt.Sprintf("landing %+v", w), func(t *testing.T, root string) map[string]string {
		t.Helper()
		return map[string]string{"cut": ldWalkAt(t, w, root)}
	})
	wt = swWorktreeOf(main, "US-001")
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	return main, wt, notes["cut"]
}

// ldWalkAt is what ldWalkedFrom's template is made of, for a builder in root:
// the repository w names, with US-001 started or finished in it, and trunk's
// tip before the story started. A template that takes the story further starts
// from here, so that it starts from what ldWalkedFrom's copies start from.
func ldWalkAt(t *testing.T, w ldWalk, root string) (cut string) {
	t.Helper()
	configure := func(root string) {
		if w.tier != "" {
			ldTier(t, root, w.tier)
		}
		if w.taxTest {
			writeFile(t, root, "internal/tax_test.go", "package internal\n")
		}
	}
	if w.lx {
		if w.tier != "" || w.taxTest {
			t.Fatalf("%+v: lxRepository takes no tier or test file of its own", w)
		}
		lxRepositoryAt(t, root, w.set, w.thresholds, w.stories...)
	} else {
		ldRepositoryAt(t, root, configure, w.stories...)
	}
	if w.trunkMoved {
		swCommitFile(t, root, "README.md", "# Billing, before the story\n", "trunk before the story")
	}
	if w.spike {
		swGit(t, root, "switch", "--quiet", "-c", "spike")
	}
	cut = ldTrunk(t, root)
	if w.finished {
		ldFinished(t, root)
	} else {
		ldStarted(t, root)
	}
	return cut
}

// ldBookkeeping is the bookkeeper's CODEMAP.md change, committed on the story's
// branch after the retro, and the story's tip it makes.
func ldBookkeeping(t *testing.T, wt string) string {
	t.Helper()
	return swCommitFile(t, wt, "CODEMAP.md", "# CODEMAP\n\nbilling.go keeps invoices; refund.go keeps refunds.\n",
		"docs: map US-001")
}

func ldTrunk(t *testing.T, main string) string {
	t.Helper()
	return swRev(t, main, "refs/heads/main")
}

// ldMoveTrunk is another story, or a person, landing on trunk after the story's
// branch was cut: a file of its own, which the story does not touch.
func ldMoveTrunk(t *testing.T, main string) string {
	t.Helper()
	return swCommitFile(t, main, "CHANGES.md", "another story's work\n", "another story lands")
}

// ldRebase rebases the story's branch onto trunk, as the session does after
// AC-3's refusal, and returns the story's new tip.
func ldRebase(t *testing.T, wt string) string {
	t.Helper()
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--quiet", "main")
	return swRev(t, wt, "HEAD")
}

// ldLand is the session stopping its finished story in its worktree, which
// has to work.
func ldLand(t *testing.T, wt string) stopPayload {
	t.Helper()
	return decode[stopPayload](t, wtMust(t, wt, ldSession, "", "stop", "--json"))
}

// ldLanded holds a landing to AC-1: it worked, said the story is done, and
// trunk is exactly the story's tip.
func ldLanded(t *testing.T, main, wt, tip string) {
	t.Helper()
	got := ldLand(t, wt)
	if !got.OK || got.Story != "US-001" || !got.Done {
		t.Errorf("stop --json = %+v, want US-001 done", got)
	}
	if trunk := ldTrunk(t, main); trunk != tip {
		t.Fatalf("trunk is at %s after the stop, want the story's tip %s: nothing landed", trunk, tip)
	}
}

// ldStillHeld holds a refused landing to leaving the story where it was: the
// iteration running in its worktree, the claim on it, and not done.
func ldStillHeld(t *testing.T, main, wt string) {
	t.Helper()
	if got := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json")).Active; got != "US-001" {
		t.Errorf("after the refused landing the worktree's iteration is on %q, want US-001 still running", got)
	}
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
		t.Error("a story that did not land is done")
	}
	if !swListed(t, main, wt) {
		t.Error("the refused landing took the story's worktree away")
	}
	if got := swClaimed(t, main, "US-001"); !config.SamePath(got, wt) {
		t.Errorf("the claim on US-001 names %q, want it left with %s", got, wt)
	}
}

// ldNext is the story `sdlc status` offers to start next in the main
// checkout, or "".
func ldNext(t *testing.T, main string) string {
	t.Helper()
	if next := decode[statusPayload](t, wtMust(t, main, "", "", "status", "--json")).Next; next != nil {
		return next.Story
	}
	return ""
}

// ldTier gives the landing fixture's one story, US-001, a risk tier in the backlog.
func ldTier(t *testing.T, root, tier string) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	var backlog map[string]any
	wtReadJSON(t, path, &backlog)
	stories, ok := backlog["stories"].([]any)
	if !ok {
		t.Fatalf("%s has no list of stories: %v", path, backlog["stories"])
	}
	for _, s := range stories {
		story, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("%s has a story that is not an object: %v", path, s)
		}
		if story["id"] == "US-001" {
			story["risk_tier"] = tier
		}
	}
	wtWriteJSON(t, path, backlog)
}

// ldApproved is a person approving the story before its commit, as a tier
// that pauses asks: the session escalates, a person approves at a terminal in
// the worktree, and the session resumes.
func ldApproved(t *testing.T, wt string) {
	t.Helper()
	wtMust(t, wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to commit?")
	wtMust(t, wt, "", "", "approve", "US-001")
	if got := swStarted(t, wt, ldSession); got.Story != "US-001" || !got.Resume {
		t.Fatalf("start after the approval gave %+v, want US-001 resumed", got)
	}
}

// ldReviewAgain records the verifier's and the code reviews again, on the tree
// as it stands.
func ldReviewAgain(t *testing.T, wt string) {
	t.Helper()
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if r.WhenDoRAdvocate {
				continue
			}
			wtMust(t, wt, ldSession, "# "+r.Role+", again\n", "review", "add", string(gate), r.Role, "approve")
		}
	}
}

type ldLogEntry struct {
	Story      string `json:"story"`
	CommitBase string `json:"commit_base"`
	Commit     string `json:"commit"`
	OffBranch  bool   `json:"off_branch"`
	Missing    bool   `json:"missing"`
}

// ldLogged is US-001's entry in `sdlc log --json` run in dir.
func ldLogged(t *testing.T, dir string) ldLogEntry {
	t.Helper()
	var payload struct {
		Entries []ldLogEntry `json:"entries"`
	}
	r := wtMust(t, dir, "", "", "log", "--json")
	if err := json.Unmarshal([]byte(r.stdout), &payload); err != nil {
		t.Fatalf("log --json in %s: %v\n%s", dir, err, r.stdout)
	}
	for _, e := range payload.Entries {
		if e.Story == "US-001" {
			return e
		}
	}
	t.Fatalf("sdlc log in %s does not list US-001:\n%s", dir, r.stdout)
	return ldLogEntry{}
}

type ldGatePayload struct {
	OK         bool   `json:"ok"`
	Story      string `json:"story"`
	Gate       string `json:"gate"`
	Status     string `json:"status"`
	CommitBase string `json:"commit_base"`
	Commit     string `json:"commit"`
	Done       bool   `json:"done"`
}

// ldRefusal is the refusal with code as sdlc renders it at the end of what a
// command printed -- what happened and why, then the fix -- and nothing else
// the command wrote on the way.
func ldRefusal(t *testing.T, out, code string) (said, fix string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	end := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), code+" ") {
			end = i
		}
	}
	fixAt := -1
	for i := end - 1; i >= 0 && fixAt < 0; i-- {
		if strings.HasPrefix(lines[i], "  fix  ") {
			fixAt = i
		}
	}
	whyAt := fixAt
	for i := fixAt - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "  why  ") {
			whyAt = i
			break
		}
	}
	start := -1
	for i := whyAt - 1; i >= 0 && start < 0; i-- {
		if strings.HasPrefix(lines[i], "sdlc: ") {
			start = i
		}
	}
	if end < 0 || fixAt < 0 || start < 0 {
		t.Fatalf("no %s refusal rendered in what stop printed:\n%s", code, out)
	}
	return strings.Join(lines[start:fixAt], "\n"), strings.Join(lines[fixAt:end], "\n")
}

var (
	ldExitOne = regexp.MustCompile(`(?i)exit(ed)?( with)?( status| code)? 1\b`)
	ldPrinted = regexp.MustCompile(`(?m)^\s+(50|10)\s*$`)
)

// ---------------------------------------------------------------- AC-1

// AC-1: the finished story's commits reach trunk by fast-forward, with nobody
// running git: trunk is the story's tip itself, no merge commit was made, and
// the main checkout, which has trunk checked out, shows the story's work.
func TestAC1AFinishedStoryLandsOnTrunkByFastForwardWhenItsSessionStopsIt(t *testing.T) {
	t.Parallel()
	main, wt, cut := ldWalkedFrom(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	if ldTrunk(t, main) != cut {
		t.Fatal("precondition: trunk moved before the landing")
	}

	ldLanded(t, main, wt, tip)
	if merges := swGit(t, main, "rev-list", "--merges", cut+"..main"); merges != "" {
		t.Errorf("trunk has merge commits after the landing: %s", merges)
	}
	if got := swGitRaw(t, main, "status", "--porcelain", "--", "billing.go", "refund.go", "price list.txt", "CODEMAP.md"); got != "" {
		t.Errorf("the main checkout's working tree is not trunk's where the story landed:\n%s", got)
	}
	if got := wtRead(t, filepath.Join(main, "refund.go")); !strings.Contains(got, "type Refund") {
		t.Errorf("the main checkout has refund.go %q, want the story's", got)
	}
	if got := wtRead(t, filepath.Join(main, "CODEMAP.md")); !strings.Contains(got, "refund.go keeps refunds") {
		t.Errorf("the bookkeeper's CODEMAP.md did not reach trunk with the story: %q", got)
	}

	// The second call is a stop with nothing running, and moves nothing.
	again := wtMust(t, main, ldSession, "", "stop")
	if !strings.Contains(again.stdout, "No iteration was running.") {
		t.Errorf("a second stop said:\n%s", again.stdout)
	}
	if got := ldTrunk(t, main); got != tip {
		t.Errorf("the second stop moved trunk to %s", got)
	}
}

// AC-1: where no checkout has trunk checked out, the landing moves the branch
// alone, and the main checkout on a person's branch is left as it was.
func TestAC1WhereNoCheckoutHasTrunkCheckedOutTheLandingMovesOnlyTheBranch(t *testing.T) {
	t.Parallel()
	main, wt, spike := ldWalkedFrom(t, ldWalk{finished: true, spike: true})
	tip := ldBookkeeping(t, wt)

	ldLanded(t, main, wt, tip)
	if got := swGit(t, main, "symbolic-ref", "HEAD"); got != "refs/heads/spike" {
		t.Errorf("the main checkout is on %s after the landing, want spike", got)
	}
	if got := swRev(t, main, "HEAD"); got != spike {
		t.Errorf("spike moved to %s", got)
	}
	if got := wtRead(t, filepath.Join(main, "billing.go")); got != ldBilling {
		t.Errorf("the main checkout's billing.go changed under a person's branch: %q", got)
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: every gate passing is not done for a story worked in a worktree; trunk
// holding its commits is. A story that depends on it becomes runnable then and
// not before.
func TestAC2AStoryWorkedInAWorktreeIsDoneOnlyOnceTrunkHasItsCommits(t *testing.T) {
	t.Parallel()
	main := ldRepository(t, nil, wtStory{id: "US-001"}, wtStory{id: "US-002", dependsOn: []string{"US-001"}})
	wt := ldFinished(t, main)
	tip := ldBookkeeping(t, wt)

	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
		t.Error("US-001 is done with every gate passed and trunk without its commits")
	}
	if got := ldNext(t, main); got == "US-002" {
		t.Error("US-002 is offered to start while trunk does not have the story it depends on")
	}

	ldLanded(t, main, wt, tip)
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s after it landed, want done", got)
	}
	if got := ldNext(t, main); got != "US-002" {
		t.Errorf("status offers %q next after US-001 landed, want US-002", got)
	}
}

// AC-2 and THREATS T9: a story whose commits a person already brought to
// trunk has landed. The stop records it and finishes it rather than refuse.
func TestAC2AndT9AStoryAPersonAlreadyBroughtToTrunkIsRecordedAsLandedAndDone(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	swGit(t, main, "merge", "--quiet", "--ff-only", "story/US-001")

	ldLanded(t, main, wt, tip)
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s, want done", got)
	}
	if got := swRecord(t, main, "US-001").Commit; got != tip {
		t.Errorf("the record names %s as the story's last commit, want %s", got, tip)
	}
	if swListed(t, main, wt) {
		t.Error("the landed story's worktree is still there")
	}
}

// ---------------------------------------------------------------- AC-3

var ldRebaseCommand = regexp.MustCompile(`git (-C \S+ )?rebase (refs/heads/)?main\b`)

// AC-3: a trunk rewritten so that it no longer holds the commit the branch was
// cut from is a refusal of its own, and moves no branch. A trunk that only
// moved on is one the stop puts the story onto itself (US-088,
// landing_ontrunk_test.go).
func TestAC3ATrunkRewrittenSinceTheCutIsRefusedAndMovesNoBranch(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true, spike: true})
	ldBookkeeping(t, wt)
	cut := ldTrunk(t, main)
	rewritten := swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit-tree", cut+"^{tree}", "-m", "trunk rewritten")
	swGit(t, main, "update-ref", "refs/heads/main", rewritten, cut)
	refs := swRefs(t, main)

	swRefused(t, ldBehindTrunk, wt, ldSession, "stop")
	if got := swRefs(t, main); got != refs {
		t.Errorf("the refused landing moved a branch:\nbefore\n%s\nafter\n%s", refs, got)
	}
	ldStillHeld(t, main, wt)
}

// ---------------------------------------------------------------- AC-4

// AC-4: rebased onto trunk, the story lands once its tests and quality
// commands have run on the rebased tree -- the one that lands -- and the
// reviews are not asked for again, since the change is the one reviewed.
func TestAC4ARebasedStoryLandsOnceItsTestsAndQualityCommandsPassOnTheRebasedTree(t *testing.T) {
	t.Parallel()
	marks := wtTempDir(t)
	mark := func(key string) string {
		return "git rev-parse HEAD > " + suSh(filepath.ToSlash(filepath.Join(marks, key)))
	}
	main := ldRepository(t, func(root string) {
		commands := map[string]string{}
		for _, key := range ldQuality {
			commands[key] = mark(key)
		}
		commands["coverage"] += " && echo 95"
		commands["mutation"] += " && echo 90"
		setConfigOnDisk(t, root, "commands", commands)
		ldThresholds(t, root)
	})
	wt := ldFinished(t, main)
	ldBookkeeping(t, wt)
	reviews := len(swRecord(t, main, "US-001").Reviews)
	ldMoveTrunk(t, main)
	rebased := ldRebase(t, wt)

	ldLanded(t, main, wt, rebased)
	for _, key := range ldQuality {
		if got := strings.TrimSpace(wtRead(t, filepath.Join(marks, key))); got != rebased {
			t.Errorf("commands.%s last ran on %q, want the rebased tree that landed, %s", key, got, rebased)
		}
	}
	if got := len(swRecord(t, main, "US-001").Reviews); got != reviews {
		t.Errorf("the record has %d reviews after the landing, want the %d it had: none asked for again", got, reviews)
	}
}

// AC-4: a configured command that fails on the rebased tree, and only there,
// keeps the story off trunk, and the refusal names the command. The story
// stays with its session to rework (US-088 AC-11): the fix hands it to nobody.
func TestAC4ACommandThatFailsOnTheRebasedTreeKeepsTheStoryOffTrunk(t *testing.T) {
	t.Parallel()
	for _, key := range ldQuality {
		t.Run("AC-4 commands."+key, func(t *testing.T) {
			commands := ldPassing()
			switch key {
			case "coverage":
				commands[key] = "if test -f CHANGES.md; then echo 50; else echo 95; fi"
			case "mutation":
				commands[key] = "if test -f CHANGES.md; then echo 10; else echo 90; fi"
			default:
				commands[key] = "test ! -f CHANGES.md"
			}
			main := ldRepository(t, func(root string) {
				setConfigOnDisk(t, root, "commands", commands)
				ldThresholds(t, root)
			})
			wt := ldFinished(t, main)
			ldBookkeeping(t, wt)
			ldMoveTrunk(t, main)
			ldRebase(t, wt)
			before := ldTrunk(t, main)

			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			// The refusal itself names the command, the trunk it was read
			// from and how it failed: the line stop prints before running
			// each command names the key too, and does not count.
			said, fix := ldRefusal(t, out, ldCheckFailed)
			suSays(t, said, "commands."+key, "as trunk commits it at "+before[:12])
			switch key {
			case "coverage", "mutation":
				suSays(t, said, "commands."+key+" printed:")
				if !ldPrinted.MatchString(said) {
					t.Errorf("the refusal does not show the number commands.%s printed:\n%s", key, said)
				}
			default:
				if !ldExitOne.MatchString(said) {
					t.Errorf("the refusal does not say commands.%s exited 1:\n%s", key, said)
				}
			}
			otNotHandedOver(t, main, fix)
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s though commands.%s failed on the tree", got, key)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// AC-4 and the freeze across a rebase: the freeze is the freeze as it is, and
// nothing is frozen again. A test file the story's own branch brought in after
// the rebase, new or a frozen one changed, is refused as the freeze refuses it
// anywhere. The test files trunk brought in are trunk's (US-088 AC-1, AC-2).
func TestAC4ATestFileTheStoryChangedAfterARebaseBreaksTheFreezeAndNothingIsFrozenAgain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code, rel string
	}{
		{"a test file the story added", ldUnfrozenTest, "internal/refund_test.go"},
		{"a frozen test the story changed", ldFrozenMoved, "internal/tax_test.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true, taxTest: true})
			ldBookkeeping(t, wt)
			lock := wtRead(t, filepath.Join(wt, ".sdlc", "state", "tests.lock"))
			if lock == "" {
				t.Fatal("precondition: the story's worktree holds no freeze")
			}
			ldMoveTrunk(t, main)
			ldRebase(t, wt)
			swCommitFile(t, wt, tc.rel, "package internal\n\n// changed by the story\n", "the story's own change to a test")
			ldReviewAgain(t, wt)
			before := ldTrunk(t, main)

			swRefused(t, tc.code, wt, ldSession, "stop")
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s past a broken freeze", got)
			}
			if got := wtRead(t, filepath.Join(wt, ".sdlc", "state", "tests.lock")); got != lock {
				t.Error("the landing froze the tests again")
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// AC-4 and the person's answer to OQ-2: a pre-commit approval holds for the
// work it was given for. A story in a tier that pauses lands on the approval
// it has where its work is still that work.
func TestAC4AndQ2AStoryInATierThatPausesLandsOnTheApprovalItHas(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	tip := ldBookkeeping(t, wt)

	ldLanded(t, main, wt, tip)
}

// ---------------------------------------------------------------- AC-5

// AC-5: after a rebase, a change that is not the one the reviewers saw makes
// the reviews stale, however small: more code, one space, a CODEMAP.md that is
// not the bookkeeper's.
func TestAC5AChangeThatIsNotTheOneTheReviewersSawIsRefusedAsStale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rel, body string
	}{
		{"AC-5 code committed after the reviews", "refund.go",
			"package billing\n\n// Refund is what US-001 adds.\ntype Refund struct{ Amount int }\n"},
		{"AC-5 one space committed after the reviews", "refund.go",
			"package billing\n\n// Refund is what US-001 adds.\ntype Refund struct{} \n"},
		{"AC-5 a CODEMAP.md below the root", "docs/CODEMAP.md", "# not the bookkeeper's\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true})
			ldBookkeeping(t, wt)
			ldMoveTrunk(t, main)
			ldRebase(t, wt)
			swCommitFile(t, wt, tc.rel, tc.body, "after the reviews")
			before := ldTrunk(t, main)

			swRefused(t, ldStale, wt, ldSession, "stop")
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s with a change nobody reviewed", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// AC-5 and THREATS T2: with no rebase at all, a commit after the reviews is
// not landed either.
func TestAC5AndT2ACommitAfterTheReviewsIsNotLandedWithoutARebaseEither(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	swCommitFile(t, wt, "refund.go", "package billing\n\ntype Refund struct{ Amount int }\n", "after the reviews")
	before := ldTrunk(t, main)

	swRefused(t, ldAny, wt, ldSession, "stop")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s with a change nobody reviewed", got)
	}
	ldStillHeld(t, main, wt)
}

// AC-5: the reviews recorded again, on the change as it now is, let it land.
func TestAC5ReviewsRecordedAgainOnTheChangedChangeLetItLand(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	ldMoveTrunk(t, main)
	ldRebase(t, wt)
	tip := swCommitFile(t, wt, "refund.go", "package billing\n\ntype Refund struct{ Amount int }\n", "after the reviews")
	swRefused(t, ldStale, wt, ldSession, "stop")

	ldReviewAgain(t, wt)
	ldLanded(t, main, wt, tip)
}

// ---------------------------------------------------------------- AC-6

// AC-6: the main checkout, with trunk checked out, holding an uncommitted
// change to a file the fast-forward writes, is a refusal that names the file
// and leaves trunk and the change alone. Put back, the story lands.
func TestAC6AnUncommittedChangeToAFileTheLandingWritesIsRefusedAndNamed(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	cut := ldTrunk(t, main)
	edit := ldBilling + "// a person's edit\n"
	writeFile(t, main, "billing.go", edit)

	out := swRefused(t, ldUncommitted, wt, ldSession, "stop")
	if !wtNames(out, "billing.go") {
		t.Errorf("the refusal does not name billing.go:\n%s", out)
	}
	if got := ldTrunk(t, main); got != cut {
		t.Errorf("trunk moved to %s", got)
	}
	if got := wtRead(t, filepath.Join(main, "billing.go")); got != edit {
		t.Errorf("the person's change to billing.go is now %q", got)
	}
	ldStillHeld(t, main, wt)

	writeFile(t, main, "billing.go", ldBilling)
	ldLanded(t, main, wt, tip)
}

// AC-6 and THREATS T7: what is in the way is what the fast-forward writes, or
// would write over; a change anywhere else is kept and does not stop it.
func TestAC6WhatTheMainCheckoutHoldsIsInTheWayOnlyWhereTheLandingWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rel, body string
		stage, refused  bool
	}{
		{"AC-6 a change to a file the story does not touch", "README.md", "# Billing, a person's edit\n", false, false},
		{"AC-6 a change that is only staged", "billing.go", ldBilling + "// staged\n", true, true},
		{"AC-6 a file whose name has a space", "price list.txt", "tea 3\n", false, true},
		{"T7 an untracked file where the story adds one", "refund.go", "package billing // a person's own\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true})
			tip := ldBookkeeping(t, wt)
			cut := ldTrunk(t, main)
			writeFile(t, main, tc.rel, tc.body)
			if tc.stage {
				swGit(t, main, "add", "--", tc.rel)
			}

			if !tc.refused {
				ldLanded(t, main, wt, tip)
			} else {
				out := swRefused(t, ldUncommitted, wt, ldSession, "stop")
				if !wtNames(out, tc.rel) {
					t.Errorf("the refusal does not name %s:\n%s", tc.rel, out)
				}
				if got := ldTrunk(t, main); got != cut {
					t.Errorf("trunk moved to %s", got)
				}
				ldStillHeld(t, main, wt)
			}
			if got := wtRead(t, filepath.Join(main, filepath.FromSlash(tc.rel))); got != tc.body {
				t.Errorf("the person's %s is now %q", tc.rel, got)
			}
		})
	}
}

// THREATS T7: trunk checked out in a person's own worktree, not the main
// checkout, is held to the same: its changes are not written over.
func TestT7TrunkCheckedOutInAPersonsOwnWorktreeIsHeldToTheSame(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true, spike: true})
	ldBookkeeping(t, wt)
	cut := ldTrunk(t, main)
	own := filepath.Join(wtTempDir(t), "trunk")
	swGit(t, main, "worktree", "add", "--quiet", own, "main")
	edit := ldBilling + "// a person's edit in their own worktree\n"
	writeFile(t, own, "billing.go", edit)

	out := swRefused(t, ldAny, wt, ldSession, "stop")
	if !wtNames(out, "billing.go") {
		t.Errorf("the refusal does not name billing.go:\n%s", out)
	}
	if got := ldTrunk(t, main); got != cut {
		t.Errorf("trunk moved to %s", got)
	}
	if got := wtRead(t, filepath.Join(own, "billing.go")); got != edit {
		t.Errorf("the person's billing.go is now %q", got)
	}
}

// ---------------------------------------------------------------- AC-7

// AC-7: landed, the story leaves nothing behind: no worktree, no branch, no
// claim.
func TestAC7ALandedStorysWorktreeBranchAndClaimAreTakenAway(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)

	ldLanded(t, main, wt, tip)
	if swListed(t, main, wt) {
		t.Error("git still lists the landed story's worktree")
	}
	if wtExists(wt) {
		t.Error("the landed story's worktree directory is still there")
	}
	if swHasBranch(t, main, "story/US-001") {
		t.Error("story/US-001 is still there")
	}
	if wtExists(rlClaimFile(main, "US-001")) {
		t.Error("the claim on the landed story was not released")
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s, want done", got)
	}
}

// AC-7: a worktree holding work nobody committed is kept, and the stop says
// which files; the story still landed and is done, and nothing uncommitted
// reached trunk. Doctor then describes the worktree as the work nobody
// committed it is, with no merge to make.
func TestAC7AWorktreeHoldingWorkNobodyCommittedIsKeptAndNamedAndTheStoryIsStillDone(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	writeFile(t, wt, "scratch.txt", "a thought\n")
	writeFile(t, wt, "README.md", "# Billing, being edited\n")

	r := wtMust(t, wt, ldSession, "", "stop")
	out := r.stdout + r.stderr
	for _, name := range []string{"scratch.txt", "README.md"} {
		if !wtNames(out, name) {
			t.Errorf("the stop does not say it kept %s:\n%s", name, out)
		}
	}
	if got := ldTrunk(t, main); got != tip {
		t.Fatalf("trunk is at %s, want the story's tip %s", got, tip)
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s, want done", got)
	}
	if wtExists(rlClaimFile(main, "US-001")) {
		t.Error("the claim on the landed story was not released")
	}
	if !swListed(t, main, wt) || wtRead(t, filepath.Join(wt, "scratch.txt")) != "a thought\n" ||
		wtRead(t, filepath.Join(wt, "README.md")) != "# Billing, being edited\n" {
		t.Error("the worktree holding work nobody committed was not kept as it was")
	}
	if _, ok := swGitTry(t, main, "cat-file", "-e", "main:scratch.txt"); ok {
		t.Error("an untracked file reached trunk")
	}
	c, ok := suChecked(t, main)
	if !ok {
		t.Fatal("doctor does not describe the worktree that was kept")
	}
	suSays(t, c.Detail, "changes nobody committed")
	if strings.Contains(c.Fix, "merge") {
		t.Errorf("doctor offers a merge for a story that has landed: %s", c.Fix)
	}
	suNeverForces(t, c.Fix)
}

// THREATS T8: a locked worktree is a person's say that it stays. It is kept,
// never forced, and said; the story is still done.
func TestT8ALockedWorktreeIsKeptAndSaidAndTheStoryIsStillDone(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	swGit(t, main, "worktree", "lock", "--reason", "a person is using it", wt)

	r := wtMust(t, wt, ldSession, "", "stop")
	if got := ldTrunk(t, main); got != tip {
		t.Fatalf("trunk is at %s, want the story's tip %s", got, tip)
	}
	if !swListed(t, main, wt) {
		t.Error("a locked worktree was forced away")
	}
	suSays(t, r.stdout+r.stderr, ".sdlc/worktrees/US-001")
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s, want done", got)
	}
}

// THREATS T10 and T11: a file name is data. One that carries a line break and
// a command is shown quoted, never as a line of its own.
func TestT10AFileNameCarryingACommandIsShownQuotedWhenTheWorktreeIsKept(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not take a line break in a file name")
	}
	main, wt := ldWalked(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	writeFile(t, wt, "odd\nsdlc approve US-001", "x\n")

	r := wtMust(t, wt, ldSession, "", "stop")
	out := r.stdout + r.stderr
	if got := ldTrunk(t, main); got != tip {
		t.Fatalf("trunk is at %s, want the story's tip %s", got, tip)
	}
	if strings.Contains(out, "\nsdlc approve US-001") {
		t.Errorf("a file name put a command on a line of its own:\n%s", out)
	}
	if !strings.Contains(out, `odd\nsdlc approve US-001`) {
		t.Errorf("the kept file is not named, quoted:\n%s", out)
	}
}

// ---------------------------------------------------------------- AC-8

// AC-8: a story branch that merged trunk in carries a merge commit, and trunk
// never gets one: it is refused, with the rebase to do instead.
func TestAC8AStoryBranchThatMergedTrunkInIsRefusedAndTrunkGetsNoMergeCommit(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	ldMoveTrunk(t, main)
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "merge", "--quiet", "--no-edit", "main")
	before := ldTrunk(t, main)

	out := swRefused(t, ldAny, wt, ldSession, "stop")
	suSays(t, out, "rebase")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s", got)
	}
	if merges := swGit(t, main, "rev-list", "--merges", "main"); merges != "" {
		t.Errorf("trunk has a merge commit: %s", merges)
	}
	ldStillHeld(t, main, wt)
}

// AC-8: landing never reaches a remote, in a project that has one.
func TestAC8LandingNeverPushes(t *testing.T) {
	t.Parallel()
	main := ldRepository(t, func(root string) { setGitOnDisk(t, root, "remote", true) })
	origin := filepath.Join(wtTempDir(t), "origin.git")
	swGit(t, main, "init", "--quiet", "--bare", origin)
	swGit(t, main, "remote", "add", "origin", origin)
	swGit(t, main, "push", "--quiet", "origin", "main")
	swGit(t, main, "config", "push.default", "current")
	pushed := swRefs(t, origin)
	wt := ldFinished(t, main)
	tip := ldBookkeeping(t, wt)

	ldLanded(t, main, wt, tip)
	if got := swRefs(t, origin); got != pushed {
		t.Errorf("origin changed:\nbefore\n%s\nafter\n%s", pushed, got)
	}
}

// ---------------------------------------------------------------- AC-10

// AC-10: the record names the commits that reached trunk as they are after
// any rebase -- from trunk's tip before the landing to the tip that landed --
// never the commits the rebase left behind, and `sdlc log` prints that range
// in the main checkout and in another story's worktree alike.
func TestAC10TheRecordNamesWhatReachedTrunkAndLogPrintsItInEveryCheckout(t *testing.T) {
	t.Parallel()
	for _, rebase := range []bool{false, true} {
		name := map[bool]string{false: "AC-10 landed as it was", true: "AC-10 landed rebased"}[rebase]
		t.Run(name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true, stories: []wtStory{{id: "US-001"}, {id: "US-002"}}})
			old := ldBookkeeping(t, wt)
			tip := old
			if rebase {
				ldMoveTrunk(t, main)
				tip = ldRebase(t, wt)
			}
			base := ldTrunk(t, main)
			ldLanded(t, main, wt, tip)

			r := swRecord(t, main, "US-001")
			if r.CommitBase != base || r.Commit != tip {
				t.Errorf("the record names %s..%s, want %s..%s", r.CommitBase, r.Commit, base, tip)
			}
			if rebase {
				for field, got := range map[string]string{"commit": r.Commit, "commit_base": r.CommitBase, "head_past_commit": r.HeadPastCommit} {
					if got == old {
						t.Errorf("the record's %s names %s, which the rebase left behind", field, old)
					}
				}
			}

			other := swCheckStarted(t, main, "US-002", ldOtherSession, swStarted(t, main, ldOtherSession))
			for _, dir := range []string{main, other} {
				e := ldLogged(t, dir)
				if e.CommitBase != base || e.Commit != tip || e.OffBranch || e.Missing {
					t.Errorf("sdlc log in %s gives %+v, want %s..%s on the branch", dir, e, base, tip)
				}
				text := wtMust(t, dir, "", "", "log").stdout
				if want := base[:12] + ".." + tip[:12]; !strings.Contains(text, want) {
					t.Errorf("sdlc log in %s does not print %s:\n%s", dir, want, text)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- AC-11

// AC-11: with worktrees off, in a single checkout, the commit gate and stop
// are the released tool's: the gate records the range on trunk, the retro
// finishes the story, and stop says so and moves nothing.
func TestAC11WithWorktreesOffTheCommitGateAndStopAreTheReleasedOnes(t *testing.T) {
	t.Parallel()
	for _, asJSON := range []bool{true, false} {
		name := map[bool]string{true: "AC-11 the commit gate as JSON, stop as text", false: "AC-11 the commit gate as text, stop as JSON"}[asJSON]
		t.Run(name, func(t *testing.T) {
			root := wtRepository(t, wtStory{id: "US-001"})
			wtStarted(t, root, ldSession)
			swReach(t, root, ldSession, model.GateCommit)
			satisfy(t, root, model.GateCommit)
			args := []string{"gate", "commit", "pass", "--note", "committed"}
			if asJSON {
				args = append(args, "--json")
			}
			r := wtMust(t, root, ldSession, "", args...)
			head := swRev(t, root, "HEAD")
			rec := swRecord(t, root, "US-001")
			if rec.Commit != head || rec.CommitBase == "" {
				t.Fatalf("the record names %s..%s, want the range up to HEAD, %s", rec.CommitBase, rec.Commit, head)
			}
			if asJSON {
				g := decode[ldGatePayload](t, r)
				if !g.OK || g.Story != "US-001" || g.Gate != "commit" || g.Status != "pass" ||
					g.Commit != head || g.CommitBase != rec.CommitBase || g.Done {
					t.Errorf("gate commit pass --json = %+v", g)
				}
			} else {
				suSays(t, r.stdout, "US-001  commit  pass", "landed in "+rec.CommitBase[:12]+".."+head[:12])
			}
			swPass(t, root, ldSession, model.GateRetro, "")
			if got := wtStatuses(t, root)["US-001"]; got != string(model.StatusDone) {
				t.Errorf("US-001 is %s after the retro, want done as the released tool has it", got)
			}
			refs := swRefs(t, root)
			if asJSON {
				out := wtMust(t, root, ldSession, "", "stop").stdout
				want := "Ended the iteration on US-001. Every gate passed, so the story is done " +
					"and `sdlc start` moves on to the next one.\n"
				if out != want {
					t.Errorf("stop said %q, want %q", out, want)
				}
			} else {
				got := decode[stopPayload](t, wtMust(t, root, ldSession, "", "stop", "--json"))
				if got != (stopPayload{OK: true, Story: "US-001", WasA: true, Done: true}) {
					t.Errorf("stop --json = %+v", got)
				}
			}
			if got := swRefs(t, root); got != refs {
				t.Errorf("stop moved a branch:\nbefore\n%s\nafter\n%s", refs, got)
			}
		})
	}
}

// AC-11: a worktree a person made (US-029.1), with story worktrees off, works
// its story on its own trunk as one checkout does, and stop moves no branch.
func TestAC11AStoryInAWorktreeAPersonMadeIsDoneAtTheRetroAndStopMovesNoBranch(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, wt, ldSession)
	swReach(t, wt, ldSession, "")
	refs := swRefs(t, main)

	got := decode[stopPayload](t, wtMust(t, wt, ldSession, "", "stop", "--json"))
	if !got.Done {
		t.Errorf("stop --json = %+v, want the story done", got)
	}
	if after := swRefs(t, main); after != refs {
		t.Errorf("stop moved a branch:\nbefore\n%s\nafter\n%s", refs, after)
	}
	if !swListed(t, main, wt) {
		t.Error("stop took away a worktree a person made")
	}
}

// AC-11 and the person's answer to OQ-4: where a story is worked decides how
// it ends. One worked in the main checkout is the released tool's even after
// worktrees are turned on.
func TestAC11AndQ4AStoryWorkedInTheMainCheckoutEndsAsReleasedWhenWorktreesAreTurnedOn(t *testing.T) {
	t.Parallel()
	root := wtRepository(t, wtStory{id: "US-001"})
	wtStarted(t, root, ldSession)
	swReach(t, root, ldSession, model.GateCommit)
	setGitOnDisk(t, root, "worktrees", true)
	swPass(t, root, ldSession, model.GateCommit, "")
	refs := swRefs(t, root)

	out := wtMust(t, root, ldSession, "", "stop").stdout
	if !strings.Contains(out, "Every gate passed, so the story is done") {
		t.Errorf("stop said:\n%s", out)
	}
	if got := swRefs(t, root); got != refs {
		t.Errorf("stop moved a branch:\nbefore\n%s\nafter\n%s", refs, got)
	}
}

// OQ-4: a story started in a worktree lands as one, though a person turned
// worktrees off on trunk while it was worked.
func TestQ4AStoryStartedInAWorktreeLandsAsOneAfterWorktreesAreTurnedOff(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	setGitOnDisk(t, main, "worktrees", false)
	suCommit(t, main, "turn story worktrees off", ".sdlc/config.json")
	tip := ldRebase(t, wt)

	ldLanded(t, main, wt, tip)
	if swListed(t, main, wt) {
		t.Error("the landed story's worktree is still there")
	}
}

// ---------------------------------------------------------------- threats

// THREATS T4: what the checks ran on is what lands. A command that commits
// while it runs does not put its commit on trunk.
func TestT4ACommandThatCommitsWhileItRunsDoesNotPutItsCommitOnTrunk(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true, lx: true,
		set: map[string]string{"test": "git -c user.email=t@example.com -c user.name=Test commit --quiet --allow-empty -m sneaky"}})
	ldBookkeeping(t, wt)
	ldMoveTrunk(t, main)
	rebased := ldRebase(t, wt)
	before := ldTrunk(t, main)

	r := wtRun(t, wt, ldSession, "", "stop")
	if log := swGit(t, main, "log", "--format=%s", "main"); strings.Contains(log, "sneaky") {
		t.Errorf("a commit made while the checks ran reached trunk:\n%s", log)
	}
	switch got := ldTrunk(t, main); {
	case r.code == 0 && got != rebased:
		t.Errorf("stop went ahead and trunk is at %s, want the verified tip %s", got, rebased)
	case r.code != 0 && got != before:
		t.Errorf("stop was refused and trunk moved to %s", got)
	case r.code != 0 && !strings.Contains(r.stderr, ldAny):
		t.Errorf("stop exited %d with no code:\n%s", r.code, r.stderr)
	}
}

// THREATS T5: what "passed" means is not the story branch's to decide. A
// configuration the story committed that makes a failing command pass does not
// let the story land.
func TestT5ACommandTheStoryBranchRewroteDoesNotDecideWhetherItLands(t *testing.T) {
	t.Parallel()
	main := ldRepository(t, func(root string) {
		commands := ldPassing()
		commands["test"] = "exit 1"
		setConfigOnDisk(t, root, "commands", commands)
		ldThresholds(t, root)
	})
	wt := ldStarted(t, main)
	ldWork(t, wt)
	passing := ldPassing()
	setConfigOnDisk(t, wt, "commands", passing)
	swPass(t, wt, ldSession, model.GateImplementation, "")
	ldBookkeeping(t, wt)
	ldMoveTrunk(t, main)
	ldRebase(t, wt)
	before := ldTrunk(t, main)

	swRefused(t, ldAny, wt, ldSession, "stop")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s on commands the story branch rewrote", got)
	}
}

// THREATS T6: the repository's hooks are anyone's to write, and none of them
// runs when the tool lands a story and cleans up after it.
func TestT6NoHookOfTheRepositoryRunsWhenAStoryLands(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the hooks here are shell scripts marked executable, which Windows does not run as such")
	}
	main, wt := ldWalked(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	hooks, marks := wtTempDir(t), wtTempDir(t)
	for _, name := range []string{"post-merge", "post-checkout", "post-rewrite", "reference-transaction", "pre-push", "pre-commit"} {
		body := "#!/bin/sh\necho ran > " + suSh(filepath.Join(marks, name)) + "\n"
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	swGit(t, main, "config", "core.hooksPath", hooks)

	ldLanded(t, main, wt, tip)
	ran, err := os.ReadDir(marks)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ran {
		t.Errorf("the repository's %s hook ran while the story landed", e.Name())
	}
}

// THREATS T6: git's replacement objects would show the tool the reviewed
// commit where the branch holds another. The one the branch holds is what is
// measured.
func TestT6AReplacementObjectDoesNotLetAChangedCommitPassForTheReviewedOne(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	reviewed := ldBookkeeping(t, wt)
	changed := swCommitFile(t, wt, "evil.go", "package billing\n", "after the reviews")
	swGit(t, main, "replace", changed, reviewed)
	before := ldTrunk(t, main)

	swRefused(t, ldAny, wt, ldSession, "stop")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s on a replaced commit", got)
	}
}
