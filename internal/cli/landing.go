package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellx"
	"github.com/bbsnly/sdlc/internal/store"
)

// codemap is the bookkeeper's file at the root, which the retro commits after
// the reviews.
const codemap = "CODEMAP.md"

// leftOut is what a story's change leaves out: the loop's own directory, and
// the bookkeeper's CODEMAP.md. A path left out takes everything beneath it
// with it, which is why the landing takes CODEMAP.md only as a plain file.
var leftOut = []string{config.Dir, codemap}

// mainSettings is what the landing reads from the main checkout's own
// configuration, never the story branch's: the trunk's name and the backlog's
// path, slash-separated from the root.
type mainSettings struct {
	main, trunk, backlog string
	cfg                  config.Config
}

func mainSettingsOf(s *store.Store) (mainSettings, error) {
	co, err := s.Checkout()
	if err != nil {
		return mainSettings{}, err
	}
	cfg, err := config.Load(co.Main)
	if err != nil {
		return mainSettings{}, err
	}
	return mainSettings{main: co.Main, trunk: trunkOf(cfg), backlog: backlogIn(cfg), cfg: cfg}, nil
}

// backlogIn is the backlog's path in cfg, slash-separated from the root.
func backlogIn(cfg config.Config) string {
	p := cfg.Backlog.Path
	if p == "" {
		p = config.Default().Backlog.Path
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// inItsWorktree reports whether the store's checkout is story id's own
// worktree, where the story lands at the end rather than being done at the
// retro.
func inItsWorktree(s *store.Store, id string) bool {
	sid, ok := s.StoryWorktreeOf()
	return ok && sid == id
}

// trunkName is the trunk a story lands on, as the main checkout's configuration
// names it, for a message; where that cannot be read, this checkout's.
func trunkName(s *store.Store) string {
	if m, err := mainSettingsOf(s); err == nil {
		return m.trunk
	}
	return trunkOf(s.Config())
}

// waitingToLand is SDLC-E0033 for a story in its own worktree with no gate
// left: it is not done until it lands, and start sends the reader to the stop
// that lands it rather than resuming it. running is an iteration running in the
// worktree; with none, the story may be one a person sent back, and the
// released way to rework it is said as well.
func waitingToLand(id, trunk string, running bool) *sdlcerr.Error {
	fix := `run "sdlc stop" in the story's worktree, ` + worktreeIn(id) + ` in the main checkout, to land it on ` + trunk
	if !running {
		fix += `; or, to rework it instead, record a gate as failed there -- "sdlc gate code_review fail --story ` +
			id + ` --note ..." says on the record why the work came back`
	}
	return sdlcerr.New(sdlcerr.StoryAlreadyFinished, quote(id)+" is finished",
		"every gate on it has passed, and it waits to land on "+trunk).WithFix(fix)
}

// changeBase is where a story's change is measured from: the commit its branch
// was cut from wherever head still holds it, so that none of the story's own
// commits is ever outside the change, even where trunk came to hold some of
// them first. Only where a person's rebase took the cut out of head is it
// trunk's merge base with head.
func changeBase(ctx context.Context, root, cut, head, trunkTip string) (string, error) {
	if cut != "" {
		held, err := gitx.IsAncestor(ctx, root, cut, head)
		if err != nil {
			return "", err
		}
		if held {
			return cut, nil
		}
	}
	base, err := gitx.MergeBase(ctx, root, trunkTip, head)
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the story's change could not be measured",
			"HEAD, "+head[:12]+", shares no history with trunk, "+trunkTip[:12])
	}
	return base, nil
}

// recordChange writes onto the record, at the commit gate's pass, the change
// the reviewers saw and where it is measured from, for a story in its own
// worktree: the landing lands that change, or one reviewed again. It says on
// stderr what the landing will refuse in the range.
func recordChange(cmd *cobra.Command, s *store.Store, id string, record *model.Record, head string) error {
	ctx := cmd.Context()
	m, err := mainSettingsOf(s)
	if err != nil {
		return err
	}
	// Trunk is read only where the cut is not held, and its refusal is the
	// one said where it is needed.
	tip, tipErr := gitx.BranchTip(ctx, s.Root(), m.trunk)
	base, err := changeBase(ctx, s.Root(), record.Cut, head, tip)
	if err != nil {
		if tipErr != nil {
			return tipErr
		}
		return err
	}
	if record.Cut != "" && base != record.Cut {
		fmt.Fprintf(cmd.ErrOrStderr(), "sdlc: HEAD no longer holds %s, where %s was cut, so the change is "+
			"measured from %s, where it meets %s; at the landing it counts as rebased.\n",
			record.Cut[:12], store.StoryBranch(id), base[:12], m.trunk)
	}
	change, err := gitx.Fingerprint(ctx, s.Root(), base, head, leftOut...)
	if err != nil {
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the story's change could not be measured",
			"the commit gate records the change the reviewers saw, and git could not read it").WithCause(err)
	}
	record.ChangeBase, record.Change = base, change
	loopFiles, err := loopFilesIn(ctx, s.Root(), base, head, m.backlog)
	if err != nil {
		return err
	}
	if len(loopFiles) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "sdlc: %s changes %s, which the landing refuses: no story lands the "+
			"loop's own files or the backlog. A person takes those changes out of the story's commits, or "+
			"brings them to %s themselves, before `sdlc stop` lands the story.\n",
			store.StoryBranch(id), quoteAll(loopFiles), m.trunk)
	}
	return nil
}

// loopFilesIn is every path base..tip changes under the loop's own directory,
// or at one of the backlog paths, compared without regard to case.
func loopFilesIn(ctx context.Context, root, base, tip string, backlogs ...string) ([]string, error) {
	var found []string
	for _, p := range append([]string{config.Dir}, backlogs...) {
		changed, err := gitx.ChangedUnder(ctx, root, base, tip, p)
		if err != nil {
			return nil, err
		}
		for _, c := range changed {
			if !slices.Contains(found, c) {
				found = append(found, c)
			}
		}
	}
	return found, nil
}

// quoteAll is names, each quoted, in one list.
func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = quote(n)
	}
	return strings.Join(quoted, ", ")
}

// quotePaths is quoteAll for paths on disk, each as sdlcerr.QuotePath names it.
func quotePaths(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = sdlcerr.QuotePath(p)
	}
	return strings.Join(quoted, ", ")
}

// quoteSome is quoteAll for a list that can be long: the first few, and how
// many more.
func quoteSome(names []string) string {
	const shown = 5
	if len(names) <= shown {
		return quoteAll(names)
	}
	return fmt.Sprintf("%s, and %d more", quoteAll(names[:shown]), len(names)-shown)
}

// ---------------------------------------------------------------- the landing

// commandCoverage and commandMutation are the configured commands whose last
// number is held to a threshold.
const (
	commandCoverage = "coverage"
	commandMutation = "mutation"
)

// landingCommands are the configured commands a landing runs on the tree that
// lands, in the order it runs them.
var landingCommands = []string{"build", "test", "lint", "fmt_check", commandCoverage, commandMutation}

const (
	// commandOutputLimit is how much of what a failed command printed its
	// refusal shows.
	commandOutputLimit = 2000
	// numberLimit is how much of a command's standard output is kept to find
	// the last number it printed in. A coverage tool that prints its figure
	// and then more than this with no digit in it is refused as printing no
	// number: the check fails closed.
	numberLimit = 64 << 10
)

// assistantEscalates ends every landing fix that is a person's to carry out.
const assistantEscalates = `an assistant hands the story to a person with "sdlc escalate" and stops`

// lander is one landing, as `sdlc stop` runs it in a finished story's own
// worktree.
type lander struct {
	cmd *cobra.Command
	s   *store.Store
	id  string
	// running is an iteration on the story running in the worktree, the first
	// of L0's cases; without one, the story is finished and held here.
	running bool
	// unlock gives the project's lock back. It is let go while the commands
	// run, and taken again after.
	unlock *func()
	m      mainSettings
	// held is the freeze as the check that refused it judged it, for the fix.
	held heldState
	// put is the replay this stop made, where it put the story onto trunk.
	put *model.Replay
}

// landState is what L1 to L8 found: T is trunkTip, S tip, B base and R from.
type landState struct {
	record                    *model.Record
	trunkTip, tip, base, from string
	// fork is where S left trunk's history, set only where trunk has moved on
	// past it: the story is then put onto T before it lands.
	fork string
	// trunkCfg is the configuration trunk commits at T, which says what the
	// commands are and what passing them means.
	trunkCfg config.Config
	// backlogs are the backlog's path as the main checkout's configuration
	// and trunk's name it.
	backlogs []string
	// already is trunk holding S before anything moves: case (A).
	already bool
	// marked is R taken from the mark of a landing cut short.
	marked           bool
	change           string
	rebased, changed bool
	// accepted is the test files the freeze check took as trunk's own.
	accepted *model.TrunkTests
}

// needsCommands reports whether the configured commands run before the story
// lands: wherever the tree is not the one verified, rebased or changed, except
// where a landing cut short after its move stands for commands that passed on
// this very tip.
func (st landState) needsCommands() bool {
	if !st.rebased && !st.changed {
		return false
	}
	return !st.already || !st.marked || st.record.Landing.To != st.tip
}

// landIfFinished lands the story where stop runs in a finished story's own
// worktree, and reports whether it did or was refused doing so. Anywhere else
// stop is the released one, and this reports false having done nothing.
func landIfFinished(cmd *cobra.Command, s *store.Store, active string, unlock *func()) (bool, error) {
	id, running, err := whenItLands(s, active)
	if id == "" {
		return false, nil
	}
	l := &lander{cmd: cmd, s: s, id: id, running: running, unlock: unlock}
	if err != nil {
		return true, l.refused(err)
	}
	// Stories land one at a time: every read of trunk, the replay, the
	// commands and the move are inside the landing lock, so that the second of
	// two stops finds trunk as the first left it.
	turn, landed, err := l.waitItsTurn()
	if turn != nil {
		defer turn.Release()
	}
	if err != nil {
		return true, l.refused(err)
	}
	if landed {
		return true, l.landedMeanwhile()
	}
	// Held until stop returns, the commands included, so that a second stop
	// cannot land the story beside this one while the project's lock is let go.
	guard, err := s.LockLanding(id)
	if err != nil {
		return true, l.refused(err)
	}
	defer guard.Release()
	return true, l.land()
}

// Phrases the landing's refusals share, which each have to say the same thing
// in the same words.
const (
	notWhatReviewersSaw = " is not the change its reviewers saw"
	whatLandsIs         = "what lands is "
	andClause           = ", and "
	isAtClause          = " is at "
	commandsPrefix      = "commands."
)

// noUnlock stands in for a lock that is not held, so that a deferred unlock
// has something to call.
func noUnlock() {
	// Empty on purpose: the lock was let go before this was put in its place.
}

// landingWait is how long a stop waits for the landing in front of it. It is
// under the ten minutes a session's command may run for, with room for the
// stop's own work after it.
const landingWait = 8 * time.Minute

// waitItsTurn takes the lock every landing holds. Where another stop holds it,
// it says which, lets the project's lock go so that every other command goes
// on, waits, and asks L0 again once it has both: a person may have answered,
// or sent the story back, while it waited. It reports true where the story
// landed meanwhile.
func (l *lander) waitItsTurn() (*store.Guard, bool, error) {
	// Tried at once, with the project's lock held. On Windows a lock its
	// holder is letting go of reads as a denial, which the store waits out for
	// up to its guardWait before it says so: the same wait any command gives
	// the project's lock, and only while a holder is on its way out.
	turn, err := l.s.LockLandings(l.id, 0)
	if err == nil {
		return turn, false, nil
	}
	// Only a lock somebody holds is waited for. One that could not be made
	// has no holder to name, and waiting would not make it.
	var busy *sdlcerr.Error
	if !errors.Is(err, store.ErrLandingHeld) || !errors.As(err, &busy) {
		return nil, false, err
	}
	// The landing lock is never waited for with the project's lock held: the
	// stop in front takes that one back once its commands have run.
	(*l.unlock)()
	*l.unlock = noUnlock
	fmt.Fprintf(l.cmd.ErrOrStderr(), "sdlc: %s; this waits up to %s for it. Nothing has changed: a stop cut short "+
		"while it waits can be run again at once\n", busy.Why, minutes(landingWait))
	if turn, err = l.s.LockLandings(l.id, landingWait); err != nil {
		if errors.As(err, &busy) {
			still := *busy
			still.What = "another story was still landing after " + minutes(landingWait)
			return nil, false, &still
		}
		return nil, false, err
	}
	g, err := store.Lock(l.s.LockRoot(), "stop")
	if err != nil {
		return turn, false, err
	}
	*l.unlock = g.Release
	landed, err := l.landedWhileWaiting()
	return turn, landed, err
}

// landedWhileWaiting asks L0 again once the stop has both locks, and reports
// true where the story landed meanwhile.
func (l *lander) landedWhileWaiting() (bool, error) {
	// The first stop of the same story takes its worktree away as it lands,
	// and so can a person, for a story that did not land: the backlog says
	// which, never the missing directory.
	if _, err := os.Stat(l.s.Root()); errors.Is(err, fs.ErrNotExist) {
		if l.isDone() {
			return true, nil
		}
		return false, sdlcerr.New(sdlcerr.LandingCheckFailed,
			worktreeIn(l.id)+" is gone, and "+quote(l.id)+" has not landed",
			"the worktree was taken away while this stop waited its turn to land, and the backlog does not have "+
				"the story as done").
			WithFix(`run "sdlc status" and "sdlc doctor" in the main checkout, which say where the story and its ` +
				`branch stand; ` + assistantStops)
	}
	active, err := l.s.Active()
	if err != nil {
		return false, err
	}
	id, running, err := whenItLands(l.s, active)
	if err != nil {
		return false, err
	}
	if id == l.id && running == l.running {
		return false, nil
	}
	if l.isDone() {
		return true, nil
	}
	return false, sdlcerr.New(sdlcerr.LandingCheckFailed,
		quote(l.id)+" changed while this stop waited its turn to land",
		"the landing asks again, once the story in front has landed, whether this one is still finished and held "+
			"here, and it no longer is: a gate on it was recorded again, a question went to a person, or the "+
			"iteration ended").
		WithFix(`nothing is to be put right for the landing: "sdlc status" in ` + worktreeIn(l.id) +
			` says where the story stands now. Where a gate is open again, the loop carries on from it; where a ` +
			`question is with a person, an assistant stops and tells the person`)
}

// isDone reports whether the backlog has the story as done.
func (l *lander) isDone() bool {
	story, _, err := l.s.Story(l.id)
	return err == nil && story.Status == model.StatusDone
}

// minutes is a wait as a person reads it.
func minutes(d time.Duration) string { return strconv.Itoa(int(d/time.Minute)) + " minutes" }

// landedMeanwhile is what a stop says where another stop of the same story
// landed it while this one waited: there is nothing left for it to do.
func (l *lander) landedMeanwhile() error {
	if wantJSON(l.cmd) {
		return emitJSON(l.cmd.OutOrStdout(), stopPayload{OK: true, Story: l.id, Done: true})
	}
	fmt.Fprintf(l.cmd.OutOrStdout(), "%s landed while this stop waited, by another stop: there is nothing left to "+
		"land here, and `sdlc status` in the main checkout says where things stand.\n", l.id)
	return nil
}

// whenItLands is L0: the story stop lands in this checkout and whether an
// iteration on it runs here, or "" where stop is the released one. A finished
// story with no iteration running is refused while a question to a person is
// open, and where the person's latest answer sent it back.
func whenItLands(s *store.Store, active string) (string, bool, error) {
	id, record, ok := finishedHere(s, active)
	if !ok {
		return "", false, nil
	}
	if active == id {
		return id, true, nil
	}
	// With no iteration running, only a story this worktree holds by its
	// claim, which a story that is done no longer has.
	if !claimedHere(s, id) {
		return "", false, nil
	}
	if pending, ok := record.PendingEscalation(); ok {
		return id, false, sdlcerr.New(sdlcerr.AwaitingPerson,
			quote(id)+" is waiting for a person",
			"it was handed over at "+pending.At+" ("+quote(pending.Type)+"): "+quote(pending.Message)).
			WithFix(`a person answers first, in their own terminal, in ` + worktreeIn(id) + `: "sdlc approve ` + id +
				`", after which "sdlc stop" there lands it, or "sdlc approve ` + id + ` --reject ..." to send the work ` +
				`back; an assistant stops and tells the person`)
	}
	if d, ok := record.LatestDecision(); ok && d.Decision == model.DecisionRejected {
		return id, false, sdlcerr.New(sdlcerr.StoryAlreadyFinished,
			quote(id)+" is finished, and a person sent it back",
			"every gate on it has passed, and the latest answer to it, at "+d.At+", was a rejection: "+quote(d.Reason)).
			WithFix(`record a gate as failed to reopen it, in ` + worktreeIn(id) + ` -- "sdlc gate code_review fail --story ` +
				id + ` --note ..." says on the record why the work came back -- then type /sdlc:next in Claude Code`)
	}
	return id, false, nil
}

// finishedHere is the story this worktree was made for, and its record, where
// every gate on it has passed and the checkout's state reads. What cannot be
// read is the released stop's to say, as it always was: stop is the way out of
// a broken state.
func finishedHere(s *store.Store, active string) (string, *model.Record, bool) {
	id, ok := s.StoryWorktreeOf()
	if !ok || active != "" && active != id || s.SharedErr() != nil {
		return "", nil, false
	}
	if _, elsewhere, err := s.HeldElsewhere(id); err != nil || elsewhere {
		return "", nil, false
	}
	record, err := s.Record(id)
	if err != nil {
		return "", nil, false
	}
	if _, remaining := record.NextGate(); remaining {
		return "", nil, false
	}
	return id, record, true
}

// claimedHere reports whether this checkout holds id by its claim, and the
// claim reads.
func claimedHere(s *store.Store, id string) bool {
	holders, err := s.Holders()
	h, held := holders[id]
	return err == nil && held && h.Claimed && h.Here && !h.Tied()
}

// land is L1 to L12, once L0 has found the story to land and its lock is held.
func (l *lander) land() error {
	ctx := l.cmd.Context()
	st, err := l.readyToMove(ctx)
	if err != nil {
		return l.refused(err)
	}
	holding, err := gitx.CheckedOut(ctx, l.s.Root(), l.m.trunk)
	if err != nil {
		return l.refused(err)
	}
	if !st.already {
		if err := l.move(ctx, st); err != nil {
			return l.refused(err)
		}
	}
	// Trunk has the story's commits from here on. What follows records it,
	// and nothing it meets is a refusal that says nothing landed.
	if err := l.finish(ctx, st); err != nil {
		return err
	}
	return l.reportLanded(ctx, st, holding)
}

// readyToMove is L1 to L10, up to the move itself: the checks, the story put
// onto trunk where trunk moved, and the commands the landing runs.
func (l *lander) readyToMove(ctx context.Context) (landState, error) {
	m, err := mainSettingsOf(l.s)
	if err != nil {
		return landState{}, err
	}
	l.m = m
	st, err := l.check(ctx)
	if err != nil {
		return st, err
	}
	// Trunk moved on since the story left it: everything above was asked of
	// the story as it stands, and only then is it put onto trunk.
	if st.fork != "" {
		if st, err = l.ontoTrunk(ctx, st); err != nil {
			return st, err
		}
	}
	if st.needsCommands() {
		if err := l.runLandingCommands(ctx, st); err != nil {
			return st, err
		}
		if st, err = l.askAgain(ctx, st); err != nil {
			return st, err
		}
	}
	return st, nil
}

// reportLanded says what landed, on stderr where stdout is the JSON.
func (l *lander) reportLanded(ctx context.Context, st landState, holding []string) error {
	notes := l.cmd.OutOrStdout()
	if wantJSON(l.cmd) {
		notes = l.cmd.ErrOrStderr()
	}
	for _, line := range landedLines(l.id, l.m.trunk, st, holding, l.running) {
		fmt.Fprintln(notes, line)
	}
	fmt.Fprintln(notes, l.cleanUp(ctx, st))
	if wantJSON(l.cmd) {
		return emitJSON(l.cmd.OutOrStdout(), stopPayload{OK: true, Story: l.id, WasA: l.running, Done: true})
	}
	return nil
}

// check is L1 to L8, every one of them a read.
func (l *lander) check(ctx context.Context) (landState, error) {
	var st landState
	var err error
	if st.tip, err = l.worktreeReady(ctx); err != nil {
		return st, err
	}
	if st.record, err = l.s.Record(l.id); err != nil {
		return st, err
	}
	if err := l.readTrunk(ctx, &st); err != nil {
		return st, err
	}
	if err := l.whereTrunkStands(ctx, &st); err != nil {
		return st, err
	}
	if err := l.rangeChecks(ctx, st); err != nil {
		return st, err
	}
	// The freeze as it is, across a rebase too: nothing is frozen again.
	// A test file that only trunk changed since the story was cut is trunk's.
	held, err := requireIntactFreeze(ctx, l.s, l.id, freezeAt{cut: st.record.Cut, on: st.from, tip: st.tip})
	if err != nil {
		l.held = held
		return st, l.freezeFix(err)
	}
	st.accepted = held.accepted
	if err := l.changeChecks(ctx, &st); err != nil {
		return st, err
	}
	slog.Debug("landing checked", "story", l.id, "trunk", st.trunkTip[:12], "tip", st.tip[:12],
		"from", st.from[:12], "already", st.already, "rebased", st.rebased, "changed", st.changed)
	return st, nil
}

// worktreeReady is L1: the worktree on story/<ID>, with nothing part-way. It
// returns S.
func (l *lander) worktreeReady(ctx context.Context) (string, error) {
	root, branch, in := l.s.Root(), store.StoryBranch(l.id), worktreeIn(l.id)
	op, err := gitx.InProgress(ctx, root)
	if err != nil {
		return "", err
	}
	if op != "" {
		return "", sdlcerr.New(sdlcerr.LandingCheckFailed,
			"the story's worktree is part-way through "+op,
			whatLandsIs+branch+" as it is committed, and part-way through "+op+
				" the worktree is on neither what was committed nor what will be").
			WithFix(`a person finishes ` + op + ` in ` + in + `, or abandons it -- "git rebase --abort" abandons a ` +
				`rebase -- then runs "sdlc stop" there again; an assistant abandons a rebase it started with ` +
				`"git rebase --abort", hands the story to a person with "sdlc escalate" and stops`)
	}
	tip, err := gitx.BranchTip(ctx, root, branch)
	if errors.Is(err, fs.ErrNotExist) {
		return "", sdlcerr.New(sdlcerr.LandingCheckFailed,
			branch+" is gone",
			"what lands is the story's branch, and this repository has no "+branch).
			WithFix(`where HEAD in ` + in + ` is the story's work, a person makes the branch again there with ` +
				`"git branch ` + branch + `", switches to it with "git switch ` + branch + `" and runs "sdlc stop" ` +
				`there again; otherwise they reopen the story: ` + reopenRoute(l.id, model.GateImplementation) + `; ` +
				assistantEscalates)
	}
	if err != nil {
		return "", err
	}
	on, err := gitx.Branch(ctx, root)
	if err != nil {
		return "", err
	}
	switch {
	case on == "":
		return "", sdlcerr.New(sdlcerr.LandingCheckFailed,
			"the story's worktree is on no branch",
			whatLandsIs+branch+", and the worktree's HEAD is not on it").
			WithFix(`a person keeps any commit made there on no branch, then switches the worktree back with "git switch ` +
				branch + `" in ` + in + ` and runs "sdlc stop" there again; ` + assistantEscalates)
	case on != branch:
		// Named as a shell reads it back: a branch name can hold anything.
		return "", sdlcerr.New(sdlcerr.LandingCheckFailed,
			"the story's worktree is on "+shellWord(on)+", not "+branch,
			whatLandsIs+branch+", and the worktree's HEAD is on another branch").
			WithFix(`a person keeps what ` + shellWord(on) + ` holds, then switches the worktree back with "git switch ` +
				branch + `" in ` + in + ` and runs "sdlc stop" there again; ` + assistantEscalates)
	}
	return tip, nil
}

// readTrunk is L2: T, read from the branch the main checkout's configuration
// names, and trunk's own configuration as it commits it there.
func (l *lander) readTrunk(ctx context.Context, st *landState) error {
	root, trunk, in := l.s.Root(), l.m.trunk, worktreeIn(l.id)
	tip, err := gitx.BranchTip(ctx, root, trunk)
	if errors.Is(err, fs.ErrNotExist) {
		return sdlcerr.New(sdlcerr.ConfigInvalid,
			quote(trunk)+", the trunk git.trunk_branch names, is not a branch here",
			"the main checkout's "+config.File+" names it, and git has no "+quote("refs/heads/"+trunk)+
				", so there is nothing to land the story on").
			WithFix(`a person makes the branch again, or sets git.trunk_branch in the main checkout's ` + config.File +
				` to the branch the project lands on, then runs "sdlc stop" in ` + in + ` again; ` + assistantEscalates)
	}
	if err != nil {
		return err
	}
	raw, err := gitx.FileAt(ctx, root, tip, config.File)
	if err == nil {
		st.trunkCfg, err = config.Parse(raw)
	}
	if err != nil {
		why := "the landing reads the commands, and what passing them means, from trunk as it commits them"
		if known, ok := errors.AsType[*sdlcerr.Error](err); ok {
			why += andClause + known.What + ": " + known.Why
		}
		return sdlcerr.New(sdlcerr.ConfigInvalid,
			trunk+"'s "+config.File+" at "+tip[:12]+" could not be read", why).
			WithFix(`a person commits a ` + config.File + ` on ` + trunk + ` that sdlc reads, then runs "sdlc stop" in ` +
				in + ` again; ` + assistantEscalates).WithCause(err)
	}
	st.trunkTip = tip
	st.backlogs = []string{l.m.backlog}
	if b := backlogIn(st.trunkCfg); !strings.EqualFold(b, l.m.backlog) {
		st.backlogs = append(st.backlogs, b)
	}
	return nil
}

// whereTrunkStands is L3: B, and R from where trunk stands against S.
func (l *lander) whereTrunkStands(ctx context.Context, st *landState) error {
	root := l.s.Root()
	T, S := st.trunkTip, st.tip
	if err := l.findBase(ctx, st); err != nil {
		return err
	}
	if mk := st.record.Landing; mk != nil && l.markStands(ctx, *mk, T, S) {
		st.from, st.marked = mk.From, true
	}

	already := T == S
	if !already {
		var err error
		if already, err = gitx.IsAncestor(ctx, root, S, T); err != nil {
			return err
		}
	}
	if already {
		// A person brought the story in, or a landing was cut short: nothing
		// moves.
		st.already = true
		return l.alreadyOnTrunk(ctx, st)
	}
	forward, err := gitx.IsAncestor(ctx, root, T, S)
	if err != nil {
		return err
	}
	if !forward {
		return l.notAheadOfTrunk(ctx, st)
	}
	if err := l.refuseTrunkBehindBase(ctx, st); err != nil {
		return err
	}
	if !st.marked {
		st.from = T
	}
	return nil
}

// findBase sets B: the commit the reviewed change began from.
func (l *lander) findBase(ctx context.Context, st *landState) error {
	st.base = st.record.ChangeBase
	if st.base != "" {
		return nil
	}
	trunk, branch := l.m.trunk, store.StoryBranch(l.id)
	T, S := st.trunkTip, st.tip
	// Committed before the change was measured at the commit gate: the
	// story's change is what S has that trunk has not.
	base, err := gitx.MergeBase(ctx, l.s.Root(), T, S)
	if err != nil {
		return err
	}
	if base == "" {
		return sdlcerr.New(sdlcerr.BranchBehindTrunk,
			branch+" shares no history with "+trunk,
			trunk+isAtClause+T[:12]+" and "+branch+" at "+S[:12]+", and neither has a commit of the other's").
			WithFix(`a person decides how the story's commits are to reach ` + trunk + `; ` + assistantEscalates)
	}
	st.base = base
	return nil
}

// alreadyOnTrunk is L3 where trunk holds the story: it is refused unless what
// trunk holds is the change the reviewers saw.
func (l *lander) alreadyOnTrunk(ctx context.Context, st *landState) error {
	if st.marked {
		return nil
	}
	trunk, branch, S := l.m.trunk, store.StoryBranch(l.id), st.tip
	holds, err := gitx.IsAncestor(ctx, l.s.Root(), st.base, S)
	if err != nil {
		return err
	}
	if !holds {
		return sdlcerr.New(sdlcerr.ReviewsMissing,
			trunk+" already holds "+branch+", and not the change its reviewers saw",
			branch+" at "+S[:12]+" no longer holds "+st.base[:12]+", where the reviewed change began").
			WithFix(l.reopenFix())
	}
	st.from = st.base
	return nil
}

// notAheadOfTrunk is L3 where S is not ahead of trunk. A trunk that only moved
// on, as it does each time another story lands, is one the stop puts the story
// onto itself; one that was rewritten is refused.
func (l *lander) notAheadOfTrunk(ctx context.Context, st *landState) error {
	root, trunk, branch := l.s.Root(), l.m.trunk, store.StoryBranch(l.id)
	T, S := st.trunkTip, st.tip
	// The story is judged first from where it left trunk's history.
	moved, err := gitx.IsAncestor(ctx, root, st.base, T)
	if err != nil {
		return err
	}
	if moved {
		fork, err := gitx.MergeBase(ctx, root, T, S)
		if err != nil {
			return err
		}
		if fork != "" {
			st.fork, st.from, st.marked = fork, fork, false
			return nil
		}
	}
	what := trunk + " was rewritten since " + branch + " was cut"
	return sdlcerr.New(sdlcerr.BranchBehindTrunk, what,
		trunk+isAtClause+T[:12]+", which "+branch+" at "+S[:12]+" does not have in its history, and a landing "+
			"only ever moves "+trunk+" forward to the story's commits").
		WithFix(l.rebaseFix(ctx, st.base, T, S))
}

// refuseTrunkBehindBase refuses a trunk that went back behind the commit the
// reviewed change began from.
func (l *lander) refuseTrunkBehindBase(ctx context.Context, st *landState) error {
	T, S := st.trunkTip, st.tip
	if T == st.base {
		return nil
	}
	trunk, branch := l.m.trunk, store.StoryBranch(l.id)
	behind, err := gitx.IsAncestor(ctx, l.s.Root(), T, st.base)
	if err != nil {
		return err
	}
	if behind {
		return sdlcerr.New(sdlcerr.BranchBehindTrunk,
			trunk+" went back behind the commit "+branch+"'s reviewed change began from",
			trunk+isAtClause+T[:12]+", before "+st.base[:12]+", so moving it to "+S[:12]+
				" would carry commits that are not the story's").
			WithFix(l.rebaseFix(ctx, st.base, T, S))
	}
	return nil
}

// markStands reports whether a landing mark is one cut short after its move:
// S holds its end, trunk holds it too, and its start comes before it.
func (l *lander) markStands(ctx context.Context, mk model.Mark, trunkTip, tip string) bool {
	for _, pair := range [][2]string{{mk.To, tip}, {mk.To, trunkTip}, {mk.From, mk.To}} {
		// A mark that cannot be read is no mark: without it the landing asks
		// every check, and runs the commands wherever the tree changed.
		if ok, err := gitx.IsAncestor(ctx, l.s.Root(), pair[0], pair[1]); err != nil || !ok {
			return false
		}
	}
	return true
}

// rangeChecks is L4 and L5: no merge commit, and nothing of the loop's own or
// the backlog, in R..S.
func (l *lander) rangeChecks(ctx context.Context, st landState) error {
	root, trunk, branch, in := l.s.Root(), l.m.trunk, store.StoryBranch(l.id), worktreeIn(l.id)
	merges, err := gitx.HasMerges(ctx, root, st.from, st.tip)
	if err != nil {
		return err
	}
	switch {
	case merges && st.already:
		return sdlcerr.New(sdlcerr.LandingCheckFailed,
			trunk+" already holds "+branch+", with a merge commit among the story's commits",
			"a landing records the story's commits as a line on "+trunk+andClause+st.from[:12]+".."+st.tip[:12]+
				" holds a merge").
			WithFix(`a person reads what reached ` + trunk + `, and either reverts it there and runs "sdlc stop" in ` + in +
				` again, or ends the story by hand; ` + assistantEscalates)
	case merges:
		return sdlcerr.New(sdlcerr.BranchBehindTrunk,
			branch+" has a merge commit",
			"a landing puts the story's commits on "+trunk+" in a line, and the merge between "+st.from[:12]+" and "+
				st.tip[:12]+" would reach "+trunk+" with them").
			WithFix("rebase instead of merging: " + l.rebaseFix(ctx, st.base, st.trunkTip, st.tip))
	}
	files, err := loopFilesIn(ctx, root, st.from, st.tip, st.backlogs...)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return l.codemapCheck(ctx, st)
	}
	fix := `a person takes those changes out of the story's commits, or brings them to ` + trunk + ` themselves, then ` +
		`runs "sdlc stop" in ` + in + ` again`
	var fixSb892 strings.Builder
	for _, f := range files {
		if atOrUnderFold(f, config.Dir+"/state") {
			fixSb892.WriteString(`; where they are the loop's state, which ` + trunk + ` tracks, they stop tracking it on ` + trunk +
				` and ignore it first`)
			break
		}
	}
	fix += fixSb892.String()
	return sdlcerr.New(sdlcerr.LandingCheckFailed,
		branch+" changes the loop's own files or the backlog: "+quoteSome(files),
		"no story lands those: the tool writes the records and the backlog in the main checkout, and the "+
			"configuration is a person's, so a change to them in a story's commits would reach "+trunk+" past every gate").
		WithFix(fix + "; " + assistantEscalates)
}

// codemapCheck is L5's other half. The change is measured without the root
// CODEMAP.md, and a path left out takes everything beneath it, so a commit
// that put a directory, a link or a submodule there, or a name that differs
// only in case, would reach trunk with no reviewer and no command having seen
// it. The landing takes CODEMAP.md only as a plain file by exactly that name.
func (l *lander) codemapCheck(ctx context.Context, st landState) error {
	root, branch, in := l.s.Root(), store.StoryBranch(l.id), worktreeIn(l.id)
	changed, err := gitx.ChangedUnder(ctx, root, st.from, st.tip, codemap)
	if err != nil {
		return err
	}
	var odd []string
	for _, name := range changed {
		if name != codemap {
			odd = append(odd, name)
			continue
		}
		mode, err := gitx.ModeAt(ctx, root, st.tip, codemap)
		if err != nil {
			return err
		}
		if mode != "100644" && mode != "100755" {
			odd = append(odd, name)
		}
	}
	if len(odd) == 0 {
		return nil
	}
	return sdlcerr.New(sdlcerr.LandingCheckFailed,
		branch+" changes the root "+codemap+" as something other than a plain file: "+quoteSome(odd),
		"the landing measures the story's change without the root "+codemap+", the bookkeeper's, so it takes that "+
			"path only as a plain file by exactly that name; anything else there, or beneath it, would reach "+
			l.m.trunk+" with no reviewer and no command having seen it").
		WithFix(`a person takes those changes out of the story's commits, leaving ` + codemap + ` a plain file, then ` +
			`runs "sdlc stop" in ` + in + ` again; ` + assistantEscalates)
}

// atOrUnderFold reports whether the slash path name is dir or under it,
// without regard to case.
func atOrUnderFold(name, dir string) bool {
	name, dir = strings.ToLower(name), strings.ToLower(dir)
	return name == dir || strings.HasPrefix(name, dir+"/")
}

// changeChecks is L7 and L8: a change that is not the one reviewed is
// reviewed again, and one rebased or changed in a tier that pauses is
// approved again.
func (l *lander) changeChecks(ctx context.Context, st *landState) error {
	root := l.s.Root()
	change, err := gitx.Fingerprint(ctx, root, st.from, st.tip, leftOut...)
	if err != nil {
		return err
	}
	st.change = change
	st.rebased = st.from != st.base
	// A tip the stop made itself was held, as it was made, to the change the
	// check before it had just passed. Every other tip is held to the change
	// the commit gate recorded.
	st.changed = !ownReplay(*st) && (st.record.Change == "" || change != st.record.Change)
	if st.changed {
		if err := l.requireReviewsAgain(ctx, st); err != nil {
			return err
		}
	}
	if st.rebased || st.changed {
		return l.requireApprovalAgain(ctx, *st)
	}
	return nil
}

// requireReviewsAgain is L7 where the change is not the one reviewed: it has to
// be committed whole, still reachable from trunk, and reviewed afresh.
func (l *lander) requireReviewsAgain(ctx context.Context, st *landState) error {
	root, branch, in := l.s.Root(), store.StoryBranch(l.id), worktreeIn(l.id)
	holds, err := gitx.IsAncestor(ctx, root, st.base, st.trunkTip)
	if err != nil {
		return err
	}
	if !holds {
		return sdlcerr.New(sdlcerr.ReviewsMissing,
			branch+notWhatReviewersSaw,
			"its change from "+st.from[:12]+" differs from the one reviewed, and "+l.m.trunk+" no longer holds "+
				st.base[:12]+", where that one began, so the reviews cannot be recorded again on it").
			WithFix(l.reopenFix())
	}
	dirty, err := gitx.Uncommitted(ctx, root, true)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return sdlcerr.New(sdlcerr.ReviewsMissing,
			branch+" is not the change its reviewers saw, and its worktree has changes nobody committed: "+
				quoteSome(dirty),
			"the reviews are recorded again on the change as it is committed, and a worktree with anything else "+
				"in it is not that").
			WithFix(`commit them on ` + branch + `, or stash them with "git stash push -- <path>", in ` + in +
				`; then have the verifier and the code reviewers review the change again and record their reviews ` +
				`("sdlc review list" names who), and run "sdlc stop" there again; if one of them blocks, hand the ` +
				`story to a person with ` +
				`"sdlc escalate" and stop`)
	}
	if err := requireFreshReviews(ctx, readingOf(l.s), l.id, st.record); err != nil {
		var known *sdlcerr.Error
		if !errors.As(err, &known) || known.Code != sdlcerr.ReviewsMissing {
			return err
		}
		return sdlcerr.New(sdlcerr.ReviewsMissing, branch+notWhatReviewersSaw, known.Why).
			WithFix(`in ` + in + `, have the verifier and the code reviewers review ` + branch + ` again as it is ` +
				`committed and record their reviews ("sdlc review list" names who), then run "sdlc stop" there ` +
				`again, which runs the configured ` +
				`commands on it; if one of them blocks, hand the story to a person with "sdlc escalate" and stop`)
	}

	return nil
}

// requireApprovalAgain is L8: an approval holds for the work it was given for,
// and a story rebased or changed in a tier that pauses is not that work. A
// tier pauses where the main checkout's configuration or trunk's says so.
func (l *lander) requireApprovalAgain(ctx context.Context, st landState) error {
	story, _, err := l.s.Story(l.id)
	if err != nil {
		return err
	}
	tier := strings.TrimSpace(story.RiskTier)
	if tier == "" {
		tier = config.DefaultRiskTier
	}
	if !st.trunkCfg.HumanGates.PausesBeforeCommit(tier) && !l.m.cfg.HumanGates.PausesBeforeCommit(tier) {
		return nil
	}
	tree, err := l.s.ReviewSubject(ctx)
	if err != nil {
		return err
	}
	why := st.record.WaitsForApproval(tier, tree)
	if why == "" {
		return nil
	}
	since := " since it was rebased"
	switch {
	case ownReplay(st):
		// The stop's own replay is the reviewed change line for line, so the
		// question is the one the commit gate asked, of the tree it asked it
		// of: no tree after the replay is one a person saw.
		since = ""
		if reviewed := reviewedTree(st.record); reviewed != "" {
			if why = st.record.WaitsForApproval(tier, reviewed); why == "" {
				return nil
			}
		}
	case !st.rebased:
		since = " since its change changed"
	}
	return sdlcerr.New(sdlcerr.ApprovalRequired, quote(l.id)+" has not been approved by a person"+since, why).
		WithFix(`hand it to a person with "sdlc escalate pre_commit_approval --message ..." and stop; once they have ` +
			`run "sdlc approve ` + l.id + `" in ` + worktreeIn(l.id) + ` on the work as it now is, "sdlc stop" there ` +
			`lands it`)
}

// ownReplay reports whether S is the commit the stop itself made when it put
// the story onto the trunk commit the change is now measured from. Commits are
// named by their content, so a tip the mark names is the very commit that stop
// made and measured, and no other tip is taken for it.
func ownReplay(st landState) bool {
	if st.record == nil || st.record.Replay == nil {
		return false
	}
	r := st.record.Replay
	return r.To == st.tip && r.Onto == st.from
}

// reviewedTree is the tree the story's code reviews are stamped with, as the
// commit gate found them when it last passed, or "" where there is none to
// name: no pass, no review, reviews of different trees, or a code review
// recorded since that pass.
//
// A review is stamped with the tree as it is when it is recorded, whatever its
// verdict, and a reviewer can record one at any time. One recorded after the
// commit gate could otherwise choose which of a person's approvals the landing
// reads: put the files back as they were for an earlier answer, record the
// reviews, put the files back again. It is read from the record's events,
// which are in the order they were written, because two timestamps in one
// second do not say which came first.
func reviewedTree(r *model.Record) string {
	passed := -1
	for i, e := range r.Events {
		switch {
		case e.Type == model.EventGate && e.Message == model.GateEvent(model.GateCommit, model.GatePass):
			passed = i
		case e.Type == model.EventReview:
			// Any review at all, whichever gate it names: the texts are the
			// ones the gate and review commands write, by the same constants,
			// and a review event this could not read must not pass unseen.
			passed = -1
		}
	}
	if passed < 0 {
		return ""
	}
	tree, seen := "", map[string]bool{}
	for _, rv := range slices.Backward(r.Reviews) {
		if rv.Gate != model.GateCodeReview || seen[rv.Role] {
			continue
		}
		seen[rv.Role] = true
		if rv.Subject == "" || tree != "" && rv.Subject != tree {
			return ""
		}
		tree = rv.Subject
	}
	return tree
}

// reopenFix is the fix where the reviewed change cannot be told apart from
// what the branch holds now: a person decides, and the way back in is the
// released one.
func (l *lander) reopenFix() string {
	return `a person reads what ` + store.StoryBranch(l.id) + ` holds against ` + l.m.trunk + ` and decides: where ` +
		`the story's own commits are still the change that was reviewed and ` + l.m.trunk + ` does not hold them yet, ` +
		`they replay only those onto it in ` + worktreeIn(l.id) + ` and run "sdlc stop" there again; otherwise, to ` +
		`have it reviewed and committed again, they reopen it: ` + reopenRoute(l.id, model.GateCodeReview) + `; ` +
		assistantEscalates
}

// reopenRoute is the way back in for a person, on a story the landing has
// handed to them. The escalation is answered first: until it is, a story
// waiting for a person is not carried on (SDLC-E0036). The answer is given in
// the story's worktree, the checkout that holds it (SDLC-E0050 elsewhere).
func reopenRoute(id string, g model.Gate) string {
	in := worktreeIn(id)
	return `answer the escalation first, with "sdlc approve ` + id + `" or "sdlc approve ` + id + ` --reject ..." in ` +
		in + `, then run "sdlc gate ` + string(g) + ` fail --story ` + id + ` --note ..." there and type /sdlc:next`
}

// stopAgainFix is SDLC-E0054's fix where trunk moved under the landing: the
// next stop finds trunk where it is then.
func (l *lander) stopAgainFix() string {
	return `run "sdlc stop" in ` + shellPath(l.s.Root()) + ` again: it puts ` + store.StoryBranch(l.id) + ` onto ` +
		l.m.trunk + ` as it is then and runs the configured commands on that tree; refused the same way again, hand ` +
		`the story to a person with "sdlc escalate" and stop`
}

// rebaseFix is SDLC-E0054's fix against trunk at trunkTip, for a state the
// stop does not put right itself: the plain rebase a person runs where trunk
// still holds B, and otherwise the one that replays only the story's own
// commits. A command in it is built from the checked id, trunk as a shell
// reads it, and B's checked hash.
func (l *lander) rebaseFix(ctx context.Context, base, trunkTip, tip string) string {
	root, trunk, branch, in := l.s.Root(), shellWord(l.m.trunk), store.StoryBranch(l.id), worktreeIn(l.id)
	if holds, err := gitx.IsAncestor(ctx, root, base, trunkTip); err == nil && holds {
		// Reached for a merge commit alone: a trunk that only moved on is
		// one the stop puts the story onto itself.
		return `a person, in their own terminal: cd -- ` + shellPath(root) + `; then git rebase ` + trunk +
			`, which leaves the merge out; then sdlc stop, which runs the configured commands on the rebased tree; ` +
			assistantEscalates
	}
	how := "a rebase that replays onto " + trunk + " only the story's own commits"
	if held, err := gitx.IsAncestor(ctx, root, base, tip); err == nil && held {
		how = `"git rebase --onto ` + trunk + ` ` + base + ` ` + branch + `"`
	} else if err == nil {
		how += ", those after " + base[:12] + ", where its reviewed change began and which " + branch +
			" no longer holds"
	}
	return `a person replays only the story's own commits onto ` + trunk + ` in ` + in + `, with ` + how + ` -- a plain ` +
		`rebase would carry commits that are not the story's -- then runs "sdlc stop" there again, which runs the ` +
		`configured commands on the rebased tree; ` + assistantEscalates
}

// freezeFix gives a refusal of the freeze its landing fix.
func (l *lander) freezeFix(err error) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		return err
	}
	trunk, branch, in := l.m.trunk, store.StoryBranch(l.id), worktreeIn(l.id)
	switch e.Code {
	case sdlcerr.FreezeBroken:
		// Restoring is right only where the branch alone changed the test:
		// where trunk changed it too, the frozen copy is without trunk's side.
		return e.WithFix(`a frozen test that only ` + trunk + ` changed or removed is accepted as ` + trunk +
			` has it, so the ones named were changed on ` + branch + ` itself, or by ` + branch + ` and ` + trunk +
			` both. Where ` + branch + ` alone changed one, a person restores it there as it was frozen and runs ` +
			`"sdlc stop" in ` + in + ` again. Where ` + trunk + ` changed it too, restoring it would take ` + trunk +
			`'s change away, so there, and wherever the change is to stay, they reopen the story: ` +
			unfreezeRoute(l.id))
	case sdlcerr.UnfrozenTests:
		var fix string
		if len(l.held.taken) > 0 {
			fix = takenFix(l.s, l.id, l.held.taken[0]) + `, and runs "sdlc stop" in ` + in + ` again; `
		}
		if len(l.held.unheld) > 0 || fix == "" {
			fix += `the test files named are neither held by the freeze nor ` + trunk + `'s own as ` + trunk +
				` has them: a person takes them out of ` + branch + `, and runs "sdlc stop" in ` + in + ` again; `
		}
		return e.WithFix(fix + `or, to keep them as they are, they reopen the story: ` + unfreezeRoute(l.id))
	}
	return err
}

// unfreezeRoute is E0025's and E0043's way back in, where the tests are to
// change: "sdlc unfreeze" lifts the freeze only with an iteration running, and
// a finished story handed to a person has none until it is reopened. Lifting it
// reopens Gate 3 and every gate after it, so what follows is /sdlc:next, never
// a stop.
func unfreezeRoute(id string) string {
	return reopenRoute(id, model.GateCodeReview) + `; once the iteration is running, they run "sdlc unfreeze ` +
		`--reason ..." there in their own terminal, which sends the story back to have its tests frozen again; ` +
		assistantEscalates
}

// runLandingCommands is L9: the configured commands, as trunk commits them at
// T, run in the worktree on S, each held to leaving S as it found it.
func (l *lander) runLandingCommands(ctx context.Context, st landState) error {
	if err := l.nothingUncommitted(ctx,
		"the configured commands run on the tree that lands, and would run with these in it"); err != nil {
		return err
	}
	// The project's lock is let go for the run: the commands are the
	// project's own, and can run for longer than any other command waits for
	// it. The landing's own lock stays held.
	(*l.unlock)()
	*l.unlock = noUnlock
	for _, key := range landingCommands {
		command := strings.TrimSpace(st.trunkCfg.Commands[key])
		if command == "" {
			continue
		}
		if err := l.runCommand(ctx, st, key, command); err != nil {
			return err
		}
	}
	g, err := store.Lock(l.s.LockRoot(), "stop")
	if err != nil {
		return err
	}
	*l.unlock = g.Release
	return nil
}

// nothingUncommitted refuses a worktree holding anything nobody committed,
// before the stop does to it what why says. Nothing is ever stashed for it.
func (l *lander) nothingUncommitted(ctx context.Context, why string) error {
	dirty, err := gitx.Uncommitted(ctx, l.s.Root(), true)
	if err != nil || len(dirty) == 0 {
		return err
	}
	return sdlcerr.New(sdlcerr.TreeNotCommitted,
		"the story's worktree has changes nobody committed: "+quoteSome(dirty), why).
		WithFix(`commit them on ` + store.StoryBranch(l.id) + ` -- the change is then not the one reviewed, and ` +
			`the reviews are recorded again -- or stash them with "git stash push -- <path>", in ` +
			worktreeIn(l.id) + `, then run "sdlc stop" there again`)
}

// runCommand runs one configured command in the worktree, in a process group
// of its own, and holds it to the pass rule and to leaving S as it was.
func (l *lander) runCommand(ctx context.Context, st landState, key, command string) error {
	fmt.Fprintf(l.cmd.ErrOrStderr(), "sdlc: running commands.%s on %s in %s\n", key, st.tip[:12], worktreeIn(l.id))
	c, err := shellx.Command(ctx, l.s.Root(), command)
	if err != nil {
		return sdlcerr.New(sdlcerr.LandingCheckFailed,
			commandsPrefix+key+" could not be run", err.Error()).WithCause(err)
	}
	shellx.InGroup(c)
	c.Env = gitx.Environ()
	output, stdout := shellx.NewTail(commandOutputLimit), shellx.NewTail(numberLimit)
	c.Stdout, c.Stderr = io.MultiWriter(stdout, output), output
	started := time.Now()
	runErr := shellx.Run(c)
	exit := 0
	var exited *exec.ExitError
	switch {
	case errors.As(runErr, &exited):
		exit = exited.ExitCode()
	case errors.Is(runErr, exec.ErrWaitDelay):
		// Something it started held its output open after it had finished:
		// what counts is how the command itself ended.
		exit = c.ProcessState.ExitCode()
	case runErr != nil:
		return sdlcerr.New(sdlcerr.LandingCheckFailed,
			commandsPrefix+key+" could not be run", runErr.Error()).WithCause(runErr)
	}
	slog.Debug("landing ran a command", "story", l.id, "command", commandsPrefix+key, "trunk", st.trunkTip[:12],
		"tip", st.tip[:12], "exit", exit, "duration", time.Since(started))
	if ok, how := passes(key, exit, stdout.String(), thresholdOf(st.trunkCfg, key)); !ok {
		return l.commandFailed(ctx, st, key, how, output.String())
	}
	return l.unmoved(ctx, st, key)
}

// commandFailed is SDLC-E0055 for a command that did not pass. What it
// printed is set apart under a line of its own, after everything sdlc says,
// so that none of it can pass for the refusal's why or its fix.
func (l *lander) commandFailed(ctx context.Context, st landState, key, how, printed string) error {
	trunk, branch, wt := l.m.trunk, store.StoryBranch(l.id), shellPath(l.s.Root())
	why := how + "; the landing runs the configured commands on the tree that lands whenever the story was put onto " +
		"a trunk that moved, or its change is not the one that was reviewed. "
	if l.put != nil {
		why += branch + " now sits on " + trunk + " at " + st.trunkTip[:12] + ", where this stop put it. "
	}
	why += l.nothingLanded()
	if printed == "" {
		why += "\n\n  commands." + key + " printed nothing"
	} else {
		why += "\n\n  commands." + key + " printed:\n" + setApart(printed)
	}
	// The story stays with its session: a command that fails on the tree
	// about to land is rework, or trunk's own fault, and neither is a question
	// only a person can answer.
	return closedRefusalError{sdlcerr.New(sdlcerr.LandingCheckFailed,
		commandsPrefix+key+", as trunk commits it at "+st.trunkTip[:12]+", failed on "+branch+" at "+st.tip[:12], why).
		WithFix(`the story is still the session's. Read what commands.` + key + ` printed, above, then run that same ` +
			`command on ` + trunk + ` as it is: ` + l.whereTrunkIs(ctx, st) + `. Where it fails there too, the failure is ` +
			trunk + `'s and not the story's: tell the person, who puts ` + trunk + ` right, and then run "sdlc stop" in ` +
			wt + ` again. Where it passes there, rework the story: in ` + wt + `, sdlc gate implementation fail --story ` +
			l.id + ` --note "<what failed>", put it right, take the story through its gates again, and "sdlc stop" ` +
			`then lands it`)}
}

// whereTrunkIs says where a session finds trunk's files as trunk commits them:
// a checkout that has trunk checked out, where there is one. The main checkout
// usually is, though it need not be, and it can hold work nobody committed.
func (l *lander) whereTrunkIs(ctx context.Context, st landState) string {
	trunk := l.m.trunk
	holding, err := gitx.CheckedOut(ctx, l.s.Root(), trunk)
	if err != nil || len(holding) == 0 {
		return `no checkout has ` + trunk + ` checked out, so ask the person to run it on ` + trunk + ` at ` + st.trunkTip[:12]
	}
	return `in ` + shellPath(holding[0]) + `, which has ` + trunk + ` checked out, once "git status" there shows ` +
		`no change to a file ` + trunk + ` tracks`
}

// unmoved holds a command to what it ran on: HEAD and story/<ID> still at S,
// and no tracked file changed, staged or not. What lands is the tree that
// passed.
func (l *lander) unmoved(ctx context.Context, st landState, key string) error {
	root, branch, in := l.s.Root(), store.StoryBranch(l.id), worktreeIn(l.id)
	on, err := gitx.Branch(ctx, root)
	if err != nil {
		return err
	}
	head, err := gitx.Head(ctx, root)
	if err != nil {
		return err
	}
	tip, err := gitx.BranchTip(ctx, root, branch)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if on != branch || head != st.tip || tip != st.tip {
		return sdlcerr.New(sdlcerr.LandingCheckFailed,
			commandsPrefix+key+" moved HEAD or "+branch+" while it ran",
			"the configured commands ran on "+st.tip[:12]+", and what lands has to be the tree they passed").
			WithFix(`a person puts ` + branch + ` and the worktree's HEAD back at ` + st.tip[:12] + ` in ` + in +
				`, keeping anything the command made that is worth keeping, and changes commands.` + key + ` on trunk ` +
				`so that it leaves them alone; then "sdlc stop" there again; ` + assistantEscalates)
	}
	changed, err := gitx.Uncommitted(ctx, root, false)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return sdlcerr.New(sdlcerr.LandingCheckFailed,
			commandsPrefix+key+" changed files the story commits: "+quoteSome(changed),
			"the configured commands passed a tree that is not "+branch+" as committed, and what lands has to be "+
				"the tree they passed").
			WithFix(`a person puts those files back as ` + branch + ` commits them, in ` + in + `, and changes commands.` +
				key + ` on trunk so that it leaves them as it found them; then "sdlc stop" there again; ` +
				assistantEscalates)
	}
	return nil
}

// askAgain is L0 to L8 asked again once the commands have run and the
// project's lock is held again, of the record as it is on disk now: a person
// may have written it meanwhile, and what they wrote is neither lost nor
// landed over.
func (l *lander) askAgain(ctx context.Context, before landState) (landState, error) {
	active, err := l.s.Active()
	if err != nil {
		return before, err
	}
	id, running, err := whenItLands(l.s, active)
	if err != nil {
		return before, err
	}
	if id != l.id || running != l.running {
		return before, sdlcerr.New(sdlcerr.LandingCheckFailed,
			quote(l.id)+" changed while the configured commands ran",
			"the landing asks again, once they have run, whether the story is still finished and held here, and it "+
				"no longer is: a gate on it was recorded again, a question went to a person, or the iteration ended").
			WithFix(`nothing is to be done for the landing: "sdlc status" in ` + worktreeIn(l.id) +
				` says where the story stands now, and the loop carries on from there`)
	}
	after, err := l.check(ctx)
	if err != nil {
		return before, err
	}
	switch {
	case after.tip != before.tip:
		return before, sdlcerr.New(sdlcerr.LandingCheckFailed,
			store.StoryBranch(l.id)+" moved while the configured commands ran",
			"they passed "+before.tip[:12]+andClause+store.StoryBranch(l.id)+isAtClause+after.tip[:12]+" now").
			WithFix(`run "sdlc stop" in ` + worktreeIn(l.id) + ` again, which runs them on the tree as it is now`)
	case after.trunkTip != before.trunkTip:
		return before, sdlcerr.New(sdlcerr.BranchBehindTrunk,
			l.m.trunk+" moved while the configured commands ran",
			"it was at "+before.trunkTip[:12]+" when they started, and is at "+after.trunkTip[:12]+" now").
			WithFix(l.stopAgainFix())
	}
	return after, nil
}

// move is L10: the mark, then the fast-forward. Where the fast-forward is
// refused, the mark alone is taken off again, and nothing else was written.
func (l *lander) move(ctx context.Context, st landState) error {
	mark := model.Mark{From: st.from, To: st.tip}
	st.record.Landing = &mark
	if err := l.s.SaveRecord(st.record); err != nil {
		return err
	}
	err := gitx.FastForward(ctx, l.s.Root(), l.m.trunk, st.trunkTip, st.tip)
	if err == nil {
		return nil
	}
	l.takeMarkOff(mark)
	var refusal *sdlcerr.Error
	if errors.Is(err, gitx.ErrNotFastForward) && errors.As(err, &refusal) {
		return refusal.WithFix(l.stopAgainFix())
	}
	return err
}

// takeMarkOff takes the landing's own mark off the record as it is on disk
// now. One left behind is harmless: it is read only where trunk holds its end.
func (l *lander) takeMarkOff(mark model.Mark) {
	r, err := l.s.Record(l.id)
	if err == nil && r.Landing != nil && *r.Landing == mark {
		r.Landing = nil
		err = l.s.SaveRecord(r)
	}
	if err != nil {
		slog.Debug("the landing could not take its mark off the record", "story", l.id, "err", err)
	}
}

// finish is L11: the record says what landed, the backlog says done, and the
// story is let go.
func (l *lander) finish(ctx context.Context, st landState) error {
	r, now := st.record, l.s.Now()
	r.CommitBase, r.Commit, r.Base = st.from, st.tip, st.tip
	r.HeadPastCommit, r.RangeUnknown = "", false
	r.ChangeBase, r.Change, r.Landing = st.from, st.change, nil
	recordAccepted(l.s, l.id, r, st.accepted)
	r.Append("landed", landedEvent(l.m.trunk, st), now)
	if l.running {
		r.Append("loop_end", "iteration ended", now)
	}
	if err := l.s.SaveRecord(r); err != nil {
		return err
	}
	if err := l.s.SetStoryStatus(l.id, model.StatusDone); err != nil {
		return err
	}
	if err := l.s.ReleaseClaim(l.id); err != nil {
		return err
	}
	if err := releaseFreeze(l.s, l.id); err != nil {
		return err
	}
	forgetWatched(ctx, l.s, l.id)
	return l.s.ClearActive()
}

// landedEvent is what the record's landed event says: the range that landed,
// and where it is not the reviewed change's own start, that start, so that
// commits of the change trunk already had show on the record.
func landedEvent(trunk string, st landState) string {
	msg := trunk + " moved from " + st.trunkTip[:12] + " to " + st.tip[:12] + " by fast-forward: " +
		st.from[:12] + ".." + st.tip[:12] + " landed"
	if st.already {
		msg = "already on " + trunk + " at " + st.tip[:12] + ": " + st.from[:12] + ".." + st.tip[:12] +
			" recorded as landed"
	}
	if st.from != st.base {
		msg += "; the reviewed change began at " + st.base[:12]
		if ownReplay(st) {
			msg += ", and sdlc stop put it onto " + trunk + " at " + st.from[:12]
		}
	}
	return msg
}

// landedLines is what stop says it did, before the clean-up.
func landedLines(id, trunk string, st landState, holding []string, running bool) []string {
	var lines []string
	if st.already {
		lines = append(lines, fmt.Sprintf("%s was already on %s at %s; recorded it as landed.", id, trunk, st.tip[:12]))
	} else {
		moved := fmt.Sprintf("%s moved from %s to %s by fast-forward", trunk, st.trunkTip[:12], st.tip[:12])
		switch len(holding) {
		case 0:
			moved += "; no checkout has " + trunk + " checked out, so only the branch moved."
		case 1:
			moved += "; the files in " + sdlcerr.QuotePath(holding[0]) + ", which has " + trunk + " checked out, moved with it."
		default:
			moved += "; the files in " + quotePaths(holding) + ", which have " + trunk + " checked out, moved with them."
		}
		lines = append(lines, moved)
	}
	if ownReplay(st) {
		lines = append(lines, replayedLine(id, trunk, *st.record.Replay, false)+".")
	}
	if st.from != st.base {
		lines = append(lines, fmt.Sprintf("The reviewed change began at %s; the record names %s..%s as what landed.",
			st.base[:12], st.from[:12], st.tip[:12]))
	}
	if st.accepted != nil {
		lines = append(lines, acceptedLine(id, trunk, st.accepted, false)+".")
	}
	lines = append(lines, "The backlog and "+id+"'s record are changed in the main checkout and not committed.")
	if running {
		return append(lines, "Ended the iteration on "+id+". Every gate passed, so the story is done "+
			"and `sdlc start` moves on to the next one.")
	}
	return append(lines, "Every gate on "+id+" passed, so the story is done and `sdlc start` moves on to the next one.")
}

// cleanUp is L12: the worktree and its branch taken away, or kept and said.
func (l *lander) cleanUp(ctx context.Context, st landState) string {
	leaveWorktree(l.m.main, l.s.Root())
	kept, err := gitx.RemoveLandedWorktree(ctx, l.m.main, l.s.Root(), store.StoryBranch(l.id), st.tip)
	if err == nil && len(kept) == 0 {
		return "Removed " + worktreeIn(l.id) + ", with the files git ignored in it, and " + store.StoryBranch(l.id) + "."
	}
	return keptLine(l.id, kept, err)
}

// keptLine says a landed story's worktree was kept: what nobody committed in
// it, quoted, or why git would not take it away, and where to go next.
func keptLine(id string, kept []string, err error) string {
	line := "Kept " + worktreeIn(id) + " and " + store.StoryBranch(id) + ": "
	var refusal *sdlcerr.Error
	switch {
	case err == nil:
		line += "it holds changes nobody committed, " + quoteSome(kept) + "."
	case errors.As(err, &refusal):
		line += refusal.What + andClause + refusal.Why + "."
	default:
		line += err.Error() + "."
	}
	line += " `sdlc doctor` in the main checkout says what to do with it."
	if err != nil && runtime.GOOS == goosWindows {
		line += " On Windows, a shell whose current directory is in the worktree keeps it there: leave it, then run " +
			"`sdlc doctor` again."
	}
	return line
}

// closedRefusalError marks a refusal whose Why already ends with the sentence
// every landing refusal ends with, put there by the caller itself for its own
// reasons (commandFailed, to keep it ahead of what a command printed). refused
// reads this flag rather than searching Why for the words "Nothing landed":
// a quoted escalation message or a command's own output can contain them
// without meaning the sentence is there.
type closedRefusalError struct{ err *sdlcerr.Error }

func (c closedRefusalError) Error() string { return c.err.Error() }
func (c closedRefusalError) Unwrap() error { return c.err }

// refused ends a refusal of the landing as every one ends: nothing landed, and
// where the story is.
func (l *lander) refused(err error) error {
	e, ok := errors.AsType[*sdlcerr.Error](err)
	if !ok {
		return err
	}
	slog.Debug("landing refused", "story", l.id, "code", e.Code.String())
	if _, closed := errors.AsType[closedRefusalError](err); closed {
		return err
	}
	out := *e
	why := strings.TrimSuffix(strings.TrimSpace(out.Why), ".")
	if why != "" {
		why += ". "
	}
	out.Why = why + l.nothingLanded()
	return &out
}

// nothingLanded is the sentence every landing refusal ends with.
func (l *lander) nothingLanded() string {
	trunk := l.m.trunk
	if trunk == "" {
		trunk = trunkName(l.s)
	}
	if l.running {
		return "Nothing landed: " + trunk + " is unchanged, and the iteration on " + l.id + " is still running"
	}
	return "Nothing landed: " + trunk + " is unchanged, and " + l.id + " waits in " + worktreeIn(l.id)
}

// thresholdOf is the threshold a command's last number is held to, where it
// has one.
func thresholdOf(cfg config.Config, key string) float64 {
	switch key {
	case commandCoverage:
		return cfg.Thresholds.CoverageMin
	case commandMutation:
		return cfg.Thresholds.MutationMin
	}
	return 0
}

// passes is the pass rule: the command exits 0, and coverage and mutation, held
// to a threshold above zero, print on standard output a last number at least
// that. Standard error is shown but never searched. It says how the command
// failed where it did.
func passes(key string, exit int, stdout string, threshold float64) (bool, string) {
	switch {
	case exit < 0:
		return false, "it was stopped before it finished"
	case exit != 0:
		return false, fmt.Sprintf("it exited with status %d", exit)
	case key != commandCoverage && key != commandMutation || threshold <= 0:
		return true, ""
	}
	name := "thresholds." + key + "_min"
	n, ok := lastNumber(stdout)
	if !ok {
		return false, fmt.Sprintf("it printed no number on standard output, and %s is %s", name, number(threshold))
	}
	if n < threshold {
		return false, fmt.Sprintf("the last number it printed on standard output is %s, below %s of %s",
			number(n), name, number(threshold))
	}
	return true, ""
}

var numberPattern = regexp.MustCompile(`[0-9]+(\.[0-9]+)?`)

// lastNumber is the last number in text.
func lastNumber(text string) (float64, bool) {
	found := numberPattern.FindAllString(text, -1)
	if len(found) == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(found[len(found)-1], 64)
	return n, err == nil
}

func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// setApart is what a command printed, shown under the line that introduces
// it: every line indented, and every character that does not print shown as ?,
// so that nothing in it can pass for sdlc's own words.
func setApart(printed string) string {
	lines := strings.Split(printed, "\n")
	for i, line := range lines {
		lines[i] = "    " + strings.Map(func(r rune) rune {
			if unicode.IsPrint(r) {
				return r
			}
			return '?'
		}, line)
	}
	return strings.Join(lines, "\n")
}

// ontoTrunk puts the story's commits onto trunk at T, where trunk has moved on
// since S left its history, and returns the landing's checks asked again of
// the result. It is reached only once every check has passed on S.
//
// The four commits it measures are the ones those checks read: S, the fork F
// and T by their full hashes, never HEAD read again, and S' as the worktree
// has it once the replay is done. A commit slipped in beside the stop, before
// the replay or after it, fails the measure or is not the tip the mark names.
func (l *lander) ontoTrunk(ctx context.Context, st landState) (landState, error) {
	root, trunk, branch := l.s.Root(), l.m.trunk, store.StoryBranch(l.id)
	S, F, T := st.tip, st.fork, st.trunkTip
	if err := l.nothingUncommitted(ctx, "putting "+branch+"'s commits onto "+trunk+" writes the worktree's files, "+
		"and nothing a person left there is stashed or written over"); err != nil {
		return st, err
	}
	fmt.Fprintf(l.cmd.ErrOrStderr(), "sdlc: %s is at %s; putting %s's commits onto it in %s\n",
		trunk, T[:12], branch, worktreeIn(l.id))
	if err := gitx.Replay(ctx, root, T, F); err != nil {
		return st, l.notReplayed(ctx, st, err)
	}
	after, err := l.worktreeReady(ctx)
	if err != nil {
		return st, err
	}
	// The result is the reviewed change line for line only where both hold:
	// the story's change is the same on the new trunk, and what came under
	// the story is trunk's change and nothing else. The first alone passes a
	// line that moved within its file.
	same, err := l.sameLines(ctx, [2]string{F, S}, [2]string{T, after})
	if err == nil && same {
		same, err = l.sameLines(ctx, [2]string{F, T}, [2]string{S, after})
	}
	if err != nil {
		return st, err
	}
	if !same {
		return st, sdlcerr.New(sdlcerr.ReviewsMissing, branch+notWhatReviewersSaw,
			"its change onto "+trunk+" at "+T[:12]+" is not, line for line, the change that was reviewed at "+S[:12]+
				"; "+branch+" now sits on "+trunk+", at "+after[:12]).
			WithFix(`in ` + worktreeIn(l.id) + `, have the verifier and the code reviewers review ` + branch +
				` again as it is committed and record their reviews ("sdlc review list" names who), then run ` +
				`"sdlc stop" there again, which runs the configured commands on it; if one of them blocks, hand the ` +
				`story to a person with "sdlc escalate" and stop`)
	}
	// Only now, and in one save with its event: a mark is what lets a later
	// stop take this tip for the reviewed change without asking again.
	r, err := l.s.Record(l.id)
	if err != nil {
		return st, err
	}
	put := model.Replay{From: S, To: after, Onto: T}
	r.Replay = &put
	r.Append("replayed", replayedLine(l.id, trunk, put, true), l.s.Now())
	if err := l.s.SaveRecord(r); err != nil {
		return st, err
	}
	l.put = &put
	slog.Debug("landing put the story onto trunk", "story", l.id, "from", S[:12], "onto", T[:12], "to", after[:12])

	again, err := l.check(ctx)
	if err != nil {
		return again, err
	}
	switch {
	case again.trunkTip != T:
		return again, sdlcerr.New(sdlcerr.BranchBehindTrunk,
			trunk+" moved while "+branch+" was being put onto it",
			"it was at "+T[:12]+" when the story's commits were put there, and is at "+again.trunkTip[:12]+" now").
			WithFix(l.stopAgainFix())
	case again.tip != after:
		return again, sdlcerr.New(sdlcerr.LandingCheckFailed,
			branch+" moved while it was being put onto "+trunk,
			"the stop left it at "+after[:12]+", and it is at "+again.tip[:12]+" now").
			WithFix(`run "sdlc stop" in ` + worktreeIn(l.id) + ` again, which judges the branch as it is now`)
	}
	return again, nil
}

// sameLines reports whether two ranges change the same lines of the same
// files, the lines around them left out. A fingerprint that could not be read
// is an error and never an equal.
func (l *lander) sameLines(ctx context.Context, a, b [2]string) (bool, error) {
	one, err := gitx.LinesFingerprint(ctx, l.s.Root(), a[0], a[1], leftOut...)
	if err != nil {
		return false, err
	}
	other, err := gitx.LinesFingerprint(ctx, l.s.Root(), b[0], b[1], leftOut...)
	if err != nil {
		return false, err
	}
	return one == other, nil
}

// replayedLine says what a replay did: the commit the story was at, the trunk
// commit it was put onto and the commit it became, in full for the record.
func replayedLine(id, trunk string, r model.Replay, full bool) string {
	short := func(h string) string {
		if full {
			return h
		}
		return h[:min(len(h), 12)]
	}
	return "Replayed " + id + " from " + short(r.From) + " onto " + trunk + " at " + short(r.Onto) + "; " +
		store.StoryBranch(id) + " is now " + short(r.To)
}

// notReplayed is the refusal for commits that did not apply onto trunk, once
// the worktree is back as it was. A replay that stops is abandoned at once:
// resolving a conflict is a person's, and a rebase left part-way would be read
// by the next command as somebody's work in progress.
func (l *lander) notReplayed(ctx context.Context, st landState, cause error) error {
	root, trunk, branch, in := l.s.Root(), l.m.trunk, store.StoryBranch(l.id), worktreeIn(l.id)
	wt, T, S := shellPath(root), st.trunkTip[:12], st.tip
	files, _ := gitx.Unmerged(ctx, root)
	if op, err := gitx.InProgress(ctx, root); err != nil || op != "" {
		if err := gitx.AbandonReplay(ctx, root); err != nil {
			slog.Debug("the replay could not be abandoned", "story", l.id, "err", err)
		}
	}
	if tip, err := l.worktreeReady(ctx); err != nil || tip != S {
		return sdlcerr.New(sdlcerr.LandingCheckFailed, in+" could not be put back as it was",
			"putting "+branch+"'s commits onto "+trunk+" did not finish, and abandoning it did not leave "+branch+
				" at "+S+" with nothing part-way").
			WithFix(`a person, in their own terminal: cd -- ` + wt + `; then git rebase --abort; if git names a lock ` +
				`file left behind, remove that file and abort again; then git status, which must name ` + branch +
				` and nothing part-way; where ` + branch + ` is not at ` + S + `, git reset --hard ` + S +
				`, which discards what the stop left and nothing of the story's, whose commits are all at or before ` +
				`that commit; then sdlc stop. Until then the story's commits are safe: they are in ` + S +
				` and in the reflog. An assistant hands the story to a person with "sdlc escalate" and stops`).WithCause(cause)
	}
	back := branch + " is back at " + S[:12] + " with its tracked files, index and stashes as they were"
	if len(files) > 0 {
		return sdlcerr.New(sdlcerr.StoryConflictsWithTrunk,
			branch+"'s commits do not apply onto "+trunk+" at "+T,
			"putting them onto "+trunk+" stopped on a conflict in "+quoteSome(files)+"; the rebase was abandoned, and "+
				back+". Resolving a conflict is a person's").
			WithFix(`a person, in their own terminal: cd -- ` + wt + `; then git rebase ` + shellWord(trunk) +
				`; resolve the files it stops on, "git add" them and carry the rebase on as git says until it ` +
				`finishes; then, where an assistant handed this story over, sdlc approve ` + l.id + `, after resolving ` +
				`and not before, so that the approval is of the work as it now is; then sdlc stop, which runs the ` +
				`configured commands on the result. In a tier that waits for a person the rebased work is approved ` +
				`again: where no question is open on the story, that stop says so with SDLC-E0037, and its fix says ` +
				`how the question is asked and answered. That rebase is git's own and not sdlc's: it runs the hooks, ` +
				`merge drivers, editor and filters the repository's configuration names, with that person's ` +
				`privileges, so they read "git config --show-origin --list" and .gitattributes there first. Where ` +
				`the only file is ` + codemap + `, docs/the-loop.md, "Parallel stories and CODEMAP.md", says how ` +
				`"merge=union" lets two stories' lines join with nobody resolving them. An assistant does not ` +
				`resolve it: it hands the story to a person with "sdlc escalate" and stops`).WithCause(cause)
	}
	said := "nothing"
	if git, ok := errors.AsType[*sdlcerr.Error](cause); ok {
		first, _, _ := strings.Cut(strings.TrimSpace(git.Why), "\n")
		said = quote(first)
	}
	return sdlcerr.New(sdlcerr.LandingCheckFailed,
		branch+"'s commits could not be put onto "+trunk+" at "+T,
		"git would not make them again there, saying "+said+"; "+back).
		WithFix(`a person reads what git said, above. Where it names no committer: git config user.name and ` +
			`user.email, then sdlc stop in ` + wt + `. Where it is a conflict git names no file for, such as a ` +
			`directory against a file: cd -- ` + wt + `; then git rebase ` + shellWord(trunk) + `; resolve it; then ` +
			`sdlc stop. An assistant hands the story to a person with "sdlc escalate" and stops`).WithCause(cause)
}
