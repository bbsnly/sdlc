package store

// US-133 AC-2 and AC-3, held where the store works out what the pause is
// decided from: where the story left trunk, the configurations a story cannot
// narrow, the story's tier, and the files its change touches. The rows through
// the commit gate and the hook hold the same end to end; these hold each input
// where it is read, in one checkout and in a story's own worktree, so that a
// change base one commit too late, a configuration left out, or a failure read
// as no pause is named at the place it happens.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

const cpStory = "US-001"

func cpGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cpCommit(t *testing.T, dir, message string) string {
	t.Helper()
	commitAll(t, dir, message)
	return cpGit(t, dir, "rev-parse", "HEAD")
}

func cpConfig(gates string) string {
	return `{"version":1,"git":{"trunk_branch":"main"},"human_gates":` + gates + "}\n"
}

func cpBacklog(tier string) string {
	return `{"stories":[{"id":"` + cpStory + `","title":"A story","status":"in_progress","risk_tier":"` + tier + `"}]}` + "\n"
}

// cpOpen is a store for the checkout at root, with the configuration on its
// disk, as a command run there opens it.
func cpOpen(t *testing.T, root string) *Store {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load %s: %v", root, err)
	}
	return New(&config.Project{Root: root, Config: cfg}).WithClock(clStore(root).now)
}

// cpOneCheckout is a repository with the configuration and backlog given
// committed, as a project in one checkout has them where its story starts.
func cpOneCheckout(t *testing.T, gates, tier string) (root, start string) {
	t.Helper()
	root = clTempDir(t)
	initGit(t, root)
	clWrite(t, filepath.Join(root, ".sdlc", "config.json"), cpConfig(gates))
	clWrite(t, filepath.Join(root, "user_stories.json"), cpBacklog(tier))
	clWrite(t, filepath.Join(root, "internal", "hook", "hook.go"), "package hook\n")
	clWrite(t, filepath.Join(root, "README.md"), "readme\n")
	// The loop's own state is not the story's change.
	clWrite(t, filepath.Join(root, ".git", "info", "exclude"), ".sdlc/state/\n.sdlc/worktrees/\n")
	return root, cpCommit(t, root, "the project")
}

// cpStoryWorktree is a main checkout with the configuration and backlog given
// committed, and the story's own worktree at .sdlc/worktrees/US-001 on its
// branch, cut from trunk's tip.
func cpStoryWorktree(t *testing.T, gates, tier string) (main, wt, cut string) {
	t.Helper()
	main, cut = cpOneCheckout(t, gates, tier)
	wt = filepath.Join(main, ".sdlc", "worktrees", cpStory)
	cpGit(t, main, "worktree", "add", "--quiet", "-b", StoryBranch(cpStory), wt)
	return main, wt, cut
}

func cpRecord(cut, commitBase string, codeReviewed bool) *model.Record {
	r := model.NewRecord(cpStory, fixedTime)
	r.Cut, r.CommitBase = cut, commitBase
	if codeReviewed {
		r.Gates[model.GateCodeReview] = model.GateResult{Status: model.GatePass}
	}
	return r
}

func cpFiles(p config.CommitPause) []string {
	var out []string
	for _, m := range p.Matches {
		out = append(out, m.File+" ~ "+m.Glob)
	}
	return out
}

// AC-2: the change is measured from where the story's branch left trunk. Each
// row is a place the story can stand against trunk; a base one commit too
// late hides the story's own commits, and one too early counts trunk's.
func TestAC2TheChangeBaseIsWhereTheStorysBranchLeftTrunk(t *testing.T) {
	t.Parallel()
	root := clTempDir(t)
	initGit(t, root)
	clWrite(t, filepath.Join(root, "a.txt"), "a\n")
	before := cpCommit(t, root, "before the cut")
	clWrite(t, filepath.Join(root, "b.txt"), "b\n")
	cut := cpCommit(t, root, "the cut")

	// trunk moves on after the cut.
	clWrite(t, filepath.Join(root, "t.txt"), "trunk\n")
	later := cpCommit(t, root, "a later trunk commit")

	// The story, cut where it was cut and never rebased.
	cpGit(t, root, "checkout", "--quiet", "-b", "story", cut)
	clWrite(t, filepath.Join(root, "s.txt"), "story\n")
	onCut := cpCommit(t, root, "the story on its cut")

	// The story put onto the later trunk commit, which holds the cut.
	cpGit(t, root, "checkout", "--quiet", "-b", "rebased", later)
	clWrite(t, filepath.Join(root, "s.txt"), "story\n")
	onLater := cpCommit(t, root, "the story on a later trunk commit")

	// A person's rebase that took the cut out of head: the story on the
	// commit before the cut.
	cpGit(t, root, "checkout", "--quiet", "-b", "rewritten", before)
	clWrite(t, filepath.Join(root, "s.txt"), "story\n")
	rewritten := cpCommit(t, root, "the story without its cut")

	for _, c := range []struct {
		name, cut, head, trunk, want string
	}{
		{"AC2 a story on its cut, trunk unmoved", cut, onCut, cut, cut},
		{"AC2 a story on its cut, trunk moved on", cut, onCut, later, cut},
		{"AC2 a story put onto a later trunk commit", cut, onLater, later, later},
		{"AC2 a trunk gone back behind the cut", cut, onCut, before, cut},
		{"AC2 a rebase that took the cut out of head", cut, rewritten, later, before},
		{"AC2 a record that names no cut", "", onLater, later, later},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := ChangeBase(t.Context(), root, c.cut, c.head, c.trunk)
			if err != nil {
				t.Fatalf("ChangeBase: %v", err)
			}
			if got != c.want {
				t.Errorf("AC-2 ChangeBase = %.12s, want %.12s", got, c.want)
			}
		})
	}
}

// AC-3: a change that cannot be measured is an error, never a base that
// would leave the story's commits out.
func TestAC3AChangeBaseThatCannotBeToldIsAnError(t *testing.T) {
	t.Parallel()
	root := clTempDir(t)
	initGit(t, root)
	clWrite(t, filepath.Join(root, "a.txt"), "a\n")
	trunk := cpCommit(t, root, "trunk")
	cpGit(t, root, "checkout", "--quiet", "--orphan", "unrelated")
	clWrite(t, filepath.Join(root, "u.txt"), "u\n")
	unrelated := cpCommit(t, root, "shares no history")

	_, err := ChangeBase(t.Context(), root, "", unrelated, trunk)
	if err == nil {
		t.Fatal("AC-3 a head with no history in common with trunk was given a base")
	}
	if code := codeOf(t, err); code != sdlcerr.RepositoryUnreadable {
		t.Errorf("AC-3 code = %s, want %s", code, sdlcerr.RepositoryUnreadable)
	}
	var e *sdlcerr.Error
	if errors.As(err, &e) && (!strings.Contains(e.Why, unrelated[:12]) || !strings.Contains(e.Why, trunk[:12])) {
		t.Errorf("AC-3 the refusal does not name both commits: %q", e.Why)
	}
	if _, err := ChangeBase(t.Context(), root, "", unrelated, "0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Error("AC-3 a trunk tip that is no commit gave a base")
	}
	if _, err := ChangeBase(t.Context(), root, "0123456789abcdef0123456789abcdef01234567", unrelated, trunk); err == nil {
		t.Error("AC-3 a cut that is no commit was read as a base")
	}
}

func TestAC2ACommitSitsPastTheCutOnlyWhereItIsNeitherTheCutNorTheTip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, on string
		want     bool
	}{
		{"AC2 a later trunk commit", "later", true},
		{"AC2 the cut", "cut", false},
		{"AC2 the story's tip", "tip", false},
		{"AC2 none", "", false},
	} {
		if got := SitsPastCut(c.on, "cut", "tip"); got != c.want {
			t.Errorf("%s: SitsPastCut(%q) = %v, want %v", c.name, c.on, got, c.want)
		}
	}
}

// AC-3: in one checkout the configuration committed where the story started
// is added to the file on disk, its tiers as well as its paths. The file on
// disk is not safe from the story: a git operation that rewrites it without
// naming it could empty either list, and the committed one does not move
// (red-team F-3; verifier F-1 found the tiers read from the disk alone).
func TestAC3InOneCheckoutTheCommittedPathsAreAddedToTheFileOnDisk(t *testing.T) {
	t.Parallel()
	root, start := cpOneCheckout(t, `{"pre_commit_pause_tiers":["low"],"pre_commit_pause_paths":["internal/hook"]}`, "low")
	clWrite(t, filepath.Join(root, ".sdlc", "config.json"), cpConfig(`{"pre_commit_pause_tiers":["high"],"pre_commit_pause_paths":["docs/**"]}`))
	s := cpOpen(t, root)

	for name, record := range map[string]*model.Record{
		"AC3 where the commits start":                   cpRecord("", start, true),
		"AC3 where the iteration began":                 {Story: cpStory, Base: start},
		"AC3 past a start git cannot read, to the base": {Story: cpStory, CommitBase: "0123456789abcdef0123456789abcdef01234567", Base: start},
	} {
		t.Run(name, func(t *testing.T) {
			gates, err := s.PauseGates(t.Context(), record)
			if err != nil {
				t.Fatalf("PauseGates: %v", err)
			}
			if want := []string{"docs/**", "internal/hook"}; !slices.Equal(gates.PreCommitPausePaths, want) {
				t.Errorf("AC-3 paths = %q, want %q: the committed list was taken away", gates.PreCommitPausePaths, want)
			}
			if want := []string{"high", "low"}; !slices.Equal(gates.PreCommitPauseTiers, want) {
				t.Errorf("AC-3 tiers = %q, want %q: the committed list was taken away", gates.PreCommitPauseTiers, want)
			}
		})
	}

	// The disk's tiers emptied, as a rewrite of the file would leave them,
	// while the start committed ["high"]: a high story is still held, by its
	// tier and not by anything unread.
	held, heldStart := cpOneCheckout(t, `{"pre_commit_pause_tiers":["high"],"pre_commit_pause_paths":[]}`, "high")
	clWrite(t, filepath.Join(held, ".sdlc", "config.json"), cpConfig(`{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":[]}`))
	emptied := cpOpen(t, held)
	for name, record := range map[string]*model.Record{
		"AC3 tiers emptied on disk, where the commits start":   cpRecord("", heldStart, true),
		"AC3 tiers emptied on disk, where the iteration began": {Story: cpStory, Base: heldStart},
	} {
		t.Run(name, func(t *testing.T) {
			gates, err := emptied.PauseGates(t.Context(), record)
			if err != nil || !slices.Equal(gates.PreCommitPauseTiers, []string{"high"}) {
				t.Errorf("AC-3 tiers = %q, %v; want the committed [\"high\"]", gates.PreCommitPauseTiers, err)
			}
			pause, err := emptied.PauseFor(t.Context(), cpStory, record, "")
			if err != nil {
				t.Fatalf("PauseFor: %v", err)
			}
			if !pause.TierPauses || pause.Tier != "high" || pause.Unread != "" {
				t.Errorf("AC-3 a high story was not held by the committed tier: %+v", pause)
			}
		})
	}

	for name, record := range map[string]*model.Record{
		"AC3 no record":                                nil,
		"AC3 a record naming no commit":                {Story: cpStory},
		"AC3 a repository with no commit at the start": {Story: cpStory, CommitBase: gitx.NoCommits, Base: gitx.NoCommits},
	} {
		t.Run(name, func(t *testing.T) {
			gates, err := s.PauseGates(t.Context(), record)
			if err != nil || !slices.Equal(gates.PreCommitPausePaths, []string{"docs/**"}) ||
				!slices.Equal(gates.PreCommitPauseTiers, []string{"high"}) {
				t.Errorf("AC-3 with no commit to read: paths %q, tiers %q, %v; want the file on disk's",
					gates.PreCommitPausePaths, gates.PreCommitPauseTiers, err)
			}
		})
	}

	// A start the record names that git cannot read, at either place, is an
	// error: the committed list is unknown, not empty.
	unreadable := &model.Record{Story: cpStory, CommitBase: "0123456789abcdef0123456789abcdef01234567",
		Base: "1123456789abcdef0123456789abcdef01234567"}
	if gates, err := s.PauseGates(t.Context(), unreadable); err == nil {
		t.Errorf("AC-3 a start git cannot read was read as no committed list: %q", gates.PreCommitPausePaths)
	}
}

// AC-3: a checkout whose .git leads nowhere cannot be placed, so neither the
// main checkout's lists nor its backlog can be read, and that is an error,
// never this checkout's copy alone.
func TestAC3ACheckoutThatCannotBePlacedCannotSayWhatPauses(t *testing.T) {
	t.Parallel()
	root := clTempDir(t)
	clWrite(t, filepath.Join(root, ".sdlc", "config.json"), cpConfig(`{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":[]}`))
	clWrite(t, filepath.Join(root, "user_stories.json"), cpBacklog("low"))
	clWrite(t, filepath.Join(root, ".git"), "gitdir: "+filepath.Join(root, "no-such-git-directory")+"\n")
	s := cpOpen(t, root)
	if gates, err := s.PauseGates(t.Context(), nil); err == nil {
		t.Errorf("AC-3 PauseGates answered %+v for a checkout it could not place", gates)
	}
	if pause, err := s.PauseFor(t.Context(), cpStory, nil, ""); err == nil && !pause.Pauses() {
		t.Errorf("AC-3 a checkout that cannot be placed decided no pause: %+v", pause)
	}
}

// AC-3: a start whose commit holds no configuration adds nothing, and one
// whose configuration does not parse is an error, never the defaults.
func TestAC3InOneCheckoutACommittedConfigurationThatDoesNotReadIsAnError(t *testing.T) {
	t.Parallel()
	root := clTempDir(t)
	initGit(t, root)
	clWrite(t, filepath.Join(root, "README.md"), "readme\n")
	bare := cpCommit(t, root, "no configuration")
	clWrite(t, filepath.Join(root, ".sdlc", "config.json"), "{ not json\n")
	broken := cpCommit(t, root, "a configuration that does not parse")
	clWrite(t, filepath.Join(root, ".sdlc", "config.json"), cpConfig(`{"pre_commit_pause_paths":["docs/**"]}`))
	clWrite(t, filepath.Join(root, "user_stories.json"), cpBacklog("low"))
	s := cpOpen(t, root)

	gates, err := s.PauseGates(t.Context(), cpRecord("", bare, true))
	if err != nil || !slices.Equal(gates.PreCommitPausePaths, []string{"docs/**"}) {
		t.Errorf("AC-3 a start with no committed configuration: paths %q, %v", gates.PreCommitPausePaths, err)
	}

	gates, err = s.PauseGates(t.Context(), cpRecord("", broken, true))
	if err == nil {
		t.Fatal("AC-3 a committed configuration that does not parse was read without a word")
	}
	if code := codeOf(t, err); code != sdlcerr.ConfigInvalid {
		t.Errorf("AC-3 code = %s, want %s", code, sdlcerr.ConfigInvalid)
	}
	if !strings.Contains(err.Error(), broken[:12]) {
		t.Errorf("AC-3 the error does not name the commit: %v", err)
	}
	if !slices.Equal(gates.PreCommitPausePaths, []string{"docs/**"}) {
		t.Errorf("AC-3 what did read was dropped: %q", gates.PreCommitPausePaths)
	}

	pause, err := s.PauseFor(t.Context(), cpStory, cpRecord("", broken, true), "")
	if err != nil {
		t.Fatalf("PauseFor: %v", err)
	}
	if !pause.Pauses() || !strings.Contains(pause.Unread, broken[:12]) {
		t.Errorf("AC-3 a committed configuration that does not read did not pause, naming it: %+v", pause)
	}
}

// AC-3: in the story's own worktree the main checkout's lists count with the
// worktree's own, so a story that empties its copy takes no pause away.
func TestAC3InAStoryWorktreeTheMainCheckoutsListsCountWithItsOwn(t *testing.T) {
	t.Parallel()
	_, wt, _ := cpStoryWorktree(t, `{"pre_commit_pause_tiers":["high"],"pre_commit_pause_paths":["internal/hook"]}`, "low")
	clWrite(t, filepath.Join(wt, ".sdlc", "config.json"), cpConfig(`{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":["docs/**"]}`))
	gates, err := cpOpen(t, wt).PauseGates(t.Context(), nil)
	if err != nil {
		t.Fatalf("PauseGates: %v", err)
	}
	if !slices.Equal(gates.PreCommitPausePaths, []string{"docs/**", "internal/hook"}) {
		t.Errorf("AC-3 paths = %q, want the worktree's and the main checkout's", gates.PreCommitPausePaths)
	}
	if !slices.Equal(gates.PreCommitPauseTiers, []string{"high"}) {
		t.Errorf("AC-3 tiers = %q, want the main checkout's high", gates.PreCommitPauseTiers)
	}
}

// AC-2 and AC-3: in the story's own worktree the change runs from where the
// branch left trunk, its commits, staged, unstaged and untracked work and a
// file moved away from a listed path all included; the tier is the main
// checkout's backlog's; and the story's copy of the configuration narrows
// nothing.
func TestAC2InAStoryWorktreeEveryChangeSinceTheCutIsMatched(t *testing.T) {
	t.Parallel()
	_, wt, cut := cpStoryWorktree(t, `{"pre_commit_pause_tiers":["high"],"pre_commit_pause_paths":["internal/hook","INTERNAL/CLI/APPROVE.GO"]}`, "low")
	clWrite(t, filepath.Join(wt, "internal", "hook", "new.go"), "package hook\n")
	cpGit(t, wt, "mv", "internal/hook/hook.go", "moved.go")
	cpCommit(t, wt, "the story's commit before code review")
	// Staged, and matched only with case folded.
	clWrite(t, filepath.Join(wt, "internal", "cli", "approve.go"), "package cli\n")
	cpGit(t, wt, "add", "internal/cli/approve.go")
	clWrite(t, filepath.Join(wt, "internal", "hook", "deep", "er", "untracked.go"), "package er\n")
	clWrite(t, filepath.Join(wt, "README.md"), "changed, and listed nowhere\n")
	// The story's own copy, emptied and with the story's tier lowered in it.
	clWrite(t, filepath.Join(wt, ".sdlc", "config.json"), cpConfig(`{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":[]}`))

	pause, err := cpOpen(t, wt).PauseFor(t.Context(), cpStory, cpRecord(cut, "", false), "")
	if err != nil {
		t.Fatalf("PauseFor: %v", err)
	}
	if pause.Unread != "" {
		t.Fatalf("PauseFor could not read: %s", pause.Unread)
	}
	want := []string{
		"internal/cli/approve.go ~ INTERNAL/CLI/APPROVE.GO",
		"internal/hook/deep/er/untracked.go ~ internal/hook",
		"internal/hook/hook.go ~ internal/hook",
		"internal/hook/new.go ~ internal/hook",
	}
	if got := cpFiles(pause); !slices.Equal(got, want) {
		t.Errorf("AC-2 matches = %q\nwant %q", got, want)
	}
	if pause.Tier != "low" || pause.TierPauses || !pause.Pauses() {
		t.Errorf("AC-2 tier %q pausing %v, paused %v: want low, not pausing, and paused on the files", pause.Tier, pause.TierPauses, pause.Pauses())
	}
}

// AC-3: the tier is the main checkout's backlog's: a story's own copy of the
// backlog, or of the configuration naming another backlog, lowers nothing.
func TestAC3InAStoryWorktreeTheTierIsTheMainCheckoutsBacklogs(t *testing.T) {
	t.Parallel()
	_, wt, cut := cpStoryWorktree(t, `{"pre_commit_pause_tiers":["high"]}`, "high")
	clWrite(t, filepath.Join(wt, "user_stories.json"), cpBacklog("low"))
	clWrite(t, filepath.Join(wt, "mine.json"), cpBacklog("low"))
	clWrite(t, filepath.Join(wt, ".sdlc", "config.json"),
		`{"version":1,"backlog":{"path":"mine.json"},"human_gates":{"pre_commit_pause_tiers":["high"]}}`+"\n")

	pause, err := cpOpen(t, wt).PauseFor(t.Context(), cpStory, cpRecord(cut, "", false), "")
	if err != nil {
		t.Fatalf("PauseFor: %v", err)
	}
	if pause.Tier != "high" || !pause.TierPauses {
		t.Errorf("AC-3 tier %q pausing %v: the story's own backlog lowered the tier", pause.Tier, pause.TierPauses)
	}
}

// AC-3: in the story's own worktree, a main checkout whose configuration does
// not read is a pause that says so, never the defaults.
func TestAC3InAStoryWorktreeAMainConfigurationThatDoesNotReadPauses(t *testing.T) {
	t.Parallel()
	main, wt, cut := cpStoryWorktree(t, `{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":["internal/hook"]}`, "low")
	clWrite(t, filepath.Join(main, ".sdlc", "config.json"), "{ not json\n")
	s := cpOpen(t, wt)

	if _, err := s.PauseGates(t.Context(), nil); err == nil {
		t.Error("AC-3 a main configuration that does not read was left out without a word")
	}
	pause, err := s.PauseFor(t.Context(), cpStory, cpRecord(cut, "", false), "")
	if err != nil {
		t.Fatalf("PauseFor: %v", err)
	}
	if pause.Unread == "" || !pause.Pauses() {
		t.Errorf("AC-3 a main configuration that does not read did not pause: %+v", pause)
	}
	if strings.Count(pause.Unread, "not valid JSON") > 1 {
		t.Errorf("the same failure is said more than once: %q", pause.Unread)
	}
}

// AC-2 and AC-3: in the story's own worktree, trunk is the branch the main
// checkout's configuration names; where that branch cannot be read the change
// cannot be measured from it, and the commit waits.
func TestAC2AndAC3InAStoryWorktreeATrunkThatCannotBeReadPauses(t *testing.T) {
	t.Parallel()
	main, wt, cut := cpStoryWorktree(t, `{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":["internal/hook"]}`, "low")
	clWrite(t, filepath.Join(main, ".sdlc", "config.json"),
		`{"version":1,"git":{"trunk_branch":"no-such-trunk"},"human_gates":{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":["internal/hook"]}}`+"\n")
	clWrite(t, filepath.Join(wt, "README.md"), "changed, and listed nowhere\n")
	pause, err := cpOpen(t, wt).PauseFor(t.Context(), cpStory, cpRecord(cut, "", false), "")
	if err != nil {
		t.Fatalf("PauseFor: %v", err)
	}
	if pause.Unread == "" || !pause.Pauses() {
		t.Errorf("AC-3 a change measured from a trunk nobody could read did not pause: %+v", pause)
	}
}

// AC-2: a main checkout that names no trunk branch has the default one, and
// the change is measured from where the story left it.
func TestAC2InAStoryWorktreeAMainCheckoutNamingNoTrunkMeasuresFromTheDefault(t *testing.T) {
	t.Parallel()
	main, wt, cut := cpStoryWorktree(t, `{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":["internal/hook"]}`, "low")
	clWrite(t, filepath.Join(main, ".sdlc", "config.json"),
		`{"version":1,"git":{"trunk_branch":""},"human_gates":{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":["internal/hook"]}}`+"\n")
	clWrite(t, filepath.Join(wt, "internal", "hook", "hook.go"), "package hook // changed\n")
	cpCommit(t, wt, "the story's commit")
	pause, err := cpOpen(t, wt).PauseFor(t.Context(), cpStory, cpRecord(cut, "", false), "")
	if err != nil || pause.Unread != "" {
		t.Fatalf("PauseFor: %v, unread %q", err, pause.Unread)
	}
	if got := cpFiles(pause); !slices.Equal(got, []string{"internal/hook/hook.go ~ internal/hook"}) {
		t.Errorf("AC-2 a committed change measured from the default trunk = %q", got)
	}
}

// AC-2: in one checkout nothing is committed before code review, so the
// change is the uncommitted work; after it the story's commits start where
// the record says.
func TestAC2InOneCheckoutTheChangeIsWhatTheStoryHasNotCommittedThenItsCommits(t *testing.T) {
	t.Parallel()
	gates := `{"pre_commit_pause_tiers":["high"],"pre_commit_pause_paths":["internal/hook"]}`

	t.Run("AC2 before code review an untracked listed file pauses", func(t *testing.T) {
		t.Parallel()
		root, _ := cpOneCheckout(t, gates, "low")
		clWrite(t, filepath.Join(root, "internal", "hook", "new", "x.go"), "package x\n")
		pause, err := cpOpen(t, root).PauseFor(t.Context(), cpStory, cpRecord("", "", false), "")
		if err != nil || pause.Unread != "" {
			t.Fatalf("PauseFor: %v, unread %q", err, pause.Unread)
		}
		if got := cpFiles(pause); !slices.Equal(got, []string{"internal/hook/new/x.go ~ internal/hook"}) {
			t.Errorf("AC-2 matches = %q", got)
		}
	})

	t.Run("AC2 before code review a deletion of a listed file pauses", func(t *testing.T) {
		t.Parallel()
		root, _ := cpOneCheckout(t, gates, "low")
		if err := os.Remove(filepath.Join(root, "internal", "hook", "hook.go")); err != nil {
			t.Fatal(err)
		}
		pause, _ := cpOpen(t, root).PauseFor(t.Context(), cpStory, cpRecord("", "", false), "")
		if got := cpFiles(pause); !slices.Equal(got, []string{"internal/hook/hook.go ~ internal/hook"}) {
			t.Errorf("AC-2 matches = %q", got)
		}
	})

	t.Run("AC2 before code review a change to no listed file does not pause", func(t *testing.T) {
		t.Parallel()
		root, _ := cpOneCheckout(t, gates, "low")
		clWrite(t, filepath.Join(root, "README.md"), "changed\n")
		pause, err := cpOpen(t, root).PauseFor(t.Context(), cpStory, cpRecord("", "", false), "")
		if err != nil || pause.Pauses() {
			t.Errorf("AC-2 a change no glob matches paused: %+v, %v", pause, err)
		}
		if pause.Globs != 1 || pause.Tier != "low" {
			t.Errorf("the decision does not carry the glob count and tier: %+v", pause)
		}
	})

	t.Run("AC2 after code review the commits from the record's start are measured", func(t *testing.T) {
		t.Parallel()
		root, start := cpOneCheckout(t, gates, "low")
		clWrite(t, filepath.Join(root, "internal", "hook", "hook.go"), "package hook // changed\n")
		head := cpCommit(t, root, "the story's commit")
		pause, err := cpOpen(t, root).PauseFor(t.Context(), cpStory, cpRecord("", start, true), head)
		if err != nil || pause.Unread != "" {
			t.Fatalf("PauseFor: %v, unread %q", err, pause.Unread)
		}
		if got := cpFiles(pause); !slices.Equal(got, []string{"internal/hook/hook.go ~ internal/hook"}) {
			t.Errorf("AC-2 a committed change to a listed file = %q", got)
		}
	})

	t.Run("AC2 after code review a record that does not say where the commits start pauses", func(t *testing.T) {
		t.Parallel()
		root, _ := cpOneCheckout(t, gates, "low")
		pause, err := cpOpen(t, root).PauseFor(t.Context(), cpStory, cpRecord("", "", true), "")
		if err != nil {
			t.Fatalf("PauseFor: %v", err)
		}
		if !pause.Pauses() || !strings.Contains(pause.Unread, "the story's change could not be measured") {
			t.Errorf("AC-2 an unmeasured change did not pause, saying why: %+v", pause)
		}
	})

	t.Run("AC2 in a repository with no commit an untracked listed file pauses", func(t *testing.T) {
		t.Parallel()
		root := clTempDir(t)
		initGit(t, root)
		clWrite(t, filepath.Join(root, ".sdlc", "config.json"), cpConfig(gates))
		clWrite(t, filepath.Join(root, "user_stories.json"), cpBacklog("low"))
		clWrite(t, filepath.Join(root, "internal", "hook", "hook.go"), "package hook\n")
		pause, err := cpOpen(t, root).PauseFor(t.Context(), cpStory, cpRecord("", "", false), "")
		if err != nil || pause.Unread != "" {
			t.Fatalf("PauseFor: %v, unread %q", err, pause.Unread)
		}
		if got := cpFiles(pause); !slices.Contains(got, "internal/hook/hook.go ~ internal/hook") {
			t.Errorf("AC-2 matches = %q", got)
		}
	})

	t.Run("AC2 with no path listed only the tier is asked", func(t *testing.T) {
		t.Parallel()
		root, _ := cpOneCheckout(t, `{"pre_commit_pause_tiers":["high"],"pre_commit_pause_paths":[]}`, "high")
		clWrite(t, filepath.Join(root, "internal", "hook", "hook.go"), "changed\n")
		pause, err := cpOpen(t, root).PauseFor(t.Context(), cpStory, cpRecord("", "", true), "")
		if err != nil || pause.Unread != "" {
			t.Fatalf("PauseFor: %v, unread %q", err, pause.Unread)
		}
		if !pause.TierPauses || len(pause.Matches) != 0 || pause.Tier != "high" {
			t.Errorf("AC-2 with no path listed = %+v, want the tier alone", pause)
		}
	})
}

// AC-3: in one checkout the tier is read from the backlog the committed
// configuration names too, and the one of the two that pauses counts.
func TestAC3InOneCheckoutTheCommittedBacklogsTierCountsWhereItPauses(t *testing.T) {
	t.Parallel()
	root, start := cpOneCheckout(t, `{"pre_commit_pause_tiers":["high"]}`, "high")
	// The file on disk names another backlog, where the story is low.
	clWrite(t, filepath.Join(root, "mine.json"), cpBacklog("low"))
	clWrite(t, filepath.Join(root, ".sdlc", "config.json"),
		`{"version":1,"backlog":{"path":"mine.json"},"human_gates":{"pre_commit_pause_tiers":["high"]}}`+"\n")
	s := cpOpen(t, root)

	pause, err := s.PauseFor(t.Context(), cpStory, cpRecord("", start, true), "")
	if err != nil {
		t.Fatalf("PauseFor: %v", err)
	}
	if pause.Tier != "high" || !pause.TierPauses {
		t.Errorf("AC-3 tier %q pausing %v: another backlog lowered the tier", pause.Tier, pause.TierPauses)
	}

	// Where the backlog the committed configuration names no longer holds the
	// story, that is said, and the commit waits.
	clWrite(t, filepath.Join(root, "user_stories.json"), `{"stories":[]}`+"\n")
	pause, err = s.PauseFor(t.Context(), cpStory, &model.Record{Story: cpStory, Base: start}, "")
	if err != nil {
		t.Fatalf("PauseFor: %v", err)
	}
	if pause.Unread == "" || !pause.Pauses() {
		t.Errorf("AC-3 a committed backlog without the story did not pause: %+v", pause)
	}
}

// A story the backlog does not hold has no tier to decide on: that is the
// one failure that is an error, as the hook's backlog refusal says.
func TestAC2AStoryTheBacklogDoesNotHoldIsAnError(t *testing.T) {
	t.Parallel()
	root, _ := cpOneCheckout(t, `{"pre_commit_pause_tiers":["high"]}`, "low")
	if _, err := cpOpen(t, root).PauseFor(t.Context(), "US-999", nil, ""); err == nil {
		t.Error("PauseFor decided for a story the backlog does not hold")
	}
}

// AC-2 and AC-5: asked away from the story's work, the pause is decided where
// the work is, the story's own worktree; and where that cannot be read it is
// not said to be measured.
func TestAC2AndAC5ThePauseIsAskedWhereTheStorysWorkIs(t *testing.T) {
	t.Parallel()
	gates := `{"pre_commit_pause_tiers":["high"],"pre_commit_pause_paths":["internal/hook"]}`

	t.Run("AC2 from the main checkout, of the story's worktree", func(t *testing.T) {
		t.Parallel()
		main, wt, cut := cpStoryWorktree(t, gates, "low")
		clWrite(t, filepath.Join(wt, "internal", "hook", "hook.go"), "changed in the worktree\n")
		pause, measured, err := cpOpen(t, main).PauseForStory(t.Context(), cpStory, cpRecord(cut, "", false))
		if err != nil || !measured {
			t.Fatalf("PauseForStory: measured %v, %v, unread %q", measured, err, pause.Unread)
		}
		if got := cpFiles(pause); !slices.Equal(got, []string{"internal/hook/hook.go ~ internal/hook"}) {
			t.Errorf("AC-2 the main checkout's answer does not hold the worktree's change: %q", got)
		}
	})

	t.Run("AC2 from the story's worktree itself", func(t *testing.T) {
		t.Parallel()
		_, wt, cut := cpStoryWorktree(t, gates, "low")
		clWrite(t, filepath.Join(wt, "internal", "hook", "hook.go"), "changed\n")
		pause, measured, err := cpOpen(t, wt).PauseForStory(t.Context(), cpStory, cpRecord(cut, "", false))
		if err != nil || !measured || len(pause.Matches) != 1 {
			t.Errorf("AC-2 asked in the worktree: %+v, measured %v, %v", pause, measured, err)
		}
	})

	t.Run("AC5 a story worktree whose configuration does not load is not measured", func(t *testing.T) {
		t.Parallel()
		main, wt, cut := cpStoryWorktree(t, gates, "low")
		clWrite(t, filepath.Join(wt, ".sdlc", "config.json"), "{ not json\n")
		_, measured, err := cpOpen(t, main).PauseForStory(t.Context(), cpStory, cpRecord(cut, "", false))
		if err != nil {
			t.Fatalf("PauseForStory: %v", err)
		}
		if measured {
			t.Error("AC-5 a worktree that could not be opened was said to be measured")
		}
	})

	t.Run("AC5 a story with no worktree is asked here", func(t *testing.T) {
		t.Parallel()
		root, _ := cpOneCheckout(t, gates, "low")
		clWrite(t, filepath.Join(root, "internal", "hook", "hook.go"), "changed\n")
		pause, measured, err := cpOpen(t, root).PauseForStory(t.Context(), cpStory, cpRecord("", "", false))
		if err != nil || !measured || len(pause.Matches) != 1 {
			t.Errorf("AC-5 asked in one checkout: %+v, measured %v, %v", pause, measured, err)
		}
	})

	t.Run("AC5 a decision that could not read is not measured", func(t *testing.T) {
		t.Parallel()
		root, _ := cpOneCheckout(t, gates, "low")
		pause, measured, err := cpOpen(t, root).PauseForStory(t.Context(), cpStory, cpRecord("", "", true))
		if err != nil {
			t.Fatalf("PauseForStory: %v", err)
		}
		if measured || !pause.Pauses() {
			t.Errorf("AC-5 an unread decision: measured %v, pauses %v", measured, pause.Pauses())
		}
	})
}
