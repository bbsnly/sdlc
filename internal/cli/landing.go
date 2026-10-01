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

// landingCommands are the configured commands a landing runs on the tree that
// lands, in the order it runs them.
var landingCommands = []string{"build", "test", "lint", "fmt_check", "coverage", "mutation"}

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
}

// landState is what L1 to L8 found: T is trunkTip, S tip, B base and R from.
type landState struct {
	record                    *model.Record
	trunkTip, tip, base, from string
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
	// Held until stop returns, the commands included, so that a second stop
	// cannot land the story beside this one while the project's lock is let go.
	guard, err := s.LockLanding(id)
	if err != nil {
		return true, l.refused(err)
	}
	defer guard.Release()
	return true, l.land()
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
	m, err := mainSettingsOf(l.s)
	if err != nil {
		return l.refused(err)
	}
	l.m = m
	st, err := l.check(ctx)
	if err != nil {
		return l.refused(err)
	}
	if st.needsCommands() {
		if err := l.runLandingCommands(ctx, st); err != nil {
			return l.refused(err)
		}
		if st, err = l.askAgain(ctx, st); err != nil {
			return l.refused(err)
		}
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
	if err := requireIntactFreeze(ctx, l.s, l.id); err != nil {
		return st, l.freezeFix(err)
	}
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
			"what lands is "+branch+" as it is committed, and part-way through "+op+
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
			"what lands is "+branch+", and the worktree's HEAD is not on it").
			WithFix(`a person keeps any commit made there on no branch, then switches the worktree back with "git switch ` +
				branch + `" in ` + in + ` and runs "sdlc stop" there again; ` + assistantEscalates)
	case on != branch:
		// Named as a shell reads it back: a branch name can hold anything.
		return "", sdlcerr.New(sdlcerr.LandingCheckFailed,
			"the story's worktree is on "+shellWord(on)+", not "+branch,
			"what lands is "+branch+", and the worktree's HEAD is on another branch").
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
		var known *sdlcerr.Error
		if errors.As(err, &known) {
			why += ", and " + known.What + ": " + known.Why
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
	root, trunk, branch := l.s.Root(), l.m.trunk, store.StoryBranch(l.id)
	T, S := st.trunkTip, st.tip
	st.base = st.record.ChangeBase
	if st.base == "" {
		// Committed before the change was measured at the commit gate: the
		// story's change is what S has that trunk has not.
		base, err := gitx.MergeBase(ctx, root, T, S)
		if err != nil {
			return err
		}
		if base == "" {
			return sdlcerr.New(sdlcerr.BranchBehindTrunk,
				branch+" shares no history with "+trunk,
				trunk+" is at "+T[:12]+" and "+branch+" at "+S[:12]+", and neither has a commit of the other's").
				WithFix(`a person decides how the story's commits are to reach ` + trunk + `; ` + assistantEscalates)
		}
		st.base = base
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
		if st.marked {
			return nil
		}
		holds, err := gitx.IsAncestor(ctx, root, st.base, S)
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
	forward, err := gitx.IsAncestor(ctx, root, T, S)
	if err != nil {
		return err
	}
	if !forward {
		what := trunk + " was rewritten since " + branch + " was cut"
		if moved, err := gitx.IsAncestor(ctx, root, st.base, T); err == nil && moved {
			what = trunk + " has moved on since " + branch + " was cut"
		}
		return sdlcerr.New(sdlcerr.BranchBehindTrunk, what,
			trunk+" is at "+T[:12]+", which "+branch+" at "+S[:12]+" does not have in its history, and a landing "+
				"only ever moves "+trunk+" forward to the story's commits").
			WithFix(l.rebaseFix(ctx, st.base, T, S))
	}
	if T != st.base {
		behind, err := gitx.IsAncestor(ctx, root, T, st.base)
		if err != nil {
			return err
		}
		if behind {
			return sdlcerr.New(sdlcerr.BranchBehindTrunk,
				trunk+" went back behind the commit "+branch+"'s reviewed change began from",
				trunk+" is at "+T[:12]+", before "+st.base[:12]+", so moving it to "+S[:12]+
					" would carry commits that are not the story's").
				WithFix(l.rebaseFix(ctx, st.base, T, S))
		}
	}
	if !st.marked {
		st.from = T
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
			"a landing records the story's commits as a line on "+trunk+", and "+st.from[:12]+".."+st.tip[:12]+
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
	for _, f := range files {
		if atOrUnderFold(f, config.Dir+"/state") {
			fix += `; where they are the loop's state, which ` + trunk + ` tracks, they stop tracking it on ` + trunk +
				` and ignore it first`
			break
		}
	}
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
	root, branch, in := l.s.Root(), store.StoryBranch(l.id), worktreeIn(l.id)
	change, err := gitx.Fingerprint(ctx, root, st.from, st.tip, leftOut...)
	if err != nil {
		return err
	}
	st.change = change
	st.rebased = st.from != st.base
	st.changed = st.record.Change == "" || change != st.record.Change
	if st.changed {
		holds, err := gitx.IsAncestor(ctx, root, st.base, st.trunkTip)
		if err != nil {
			return err
		}
		if !holds {
			return sdlcerr.New(sdlcerr.ReviewsMissing,
				branch+" is not the change its reviewers saw",
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
		if err := requireFreshReviews(ctx, l.s, l.id, st.record); err != nil {
			var known *sdlcerr.Error
			if !errors.As(err, &known) || known.Code != sdlcerr.ReviewsMissing {
				return err
			}
			return sdlcerr.New(sdlcerr.ReviewsMissing, branch+" is not the change its reviewers saw", known.Why).
				WithFix(`in ` + in + `, have the verifier and the code reviewers review ` + branch + ` again as it is ` +
					`committed and record their reviews ("sdlc review list" names who), then run "sdlc stop" there ` +
					`again, which runs the configured ` +
					`commands on it; if one of them blocks, hand the story to a person with "sdlc escalate" and stop`)
		}
	}
	if st.rebased || st.changed {
		return l.requireApprovalAgain(ctx, *st)
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
	since := "since it was rebased"
	if !st.rebased {
		since = "since its change changed"
	}
	return sdlcerr.New(sdlcerr.ApprovalRequired, quote(l.id)+" has not been approved by a person "+since, why).
		WithFix(`hand it to a person with "sdlc escalate pre_commit_approval --message ..." and stop; once they have ` +
			`run "sdlc approve ` + l.id + `" in ` + worktreeIn(l.id) + ` on the work as it now is, "sdlc stop" there ` +
			`lands it`)
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

// rebaseFix is SDLC-E0054's fix against trunk at trunkTip: the plain rebase
// the session may run where trunk still holds B, and otherwise the rebase a
// person runs, which replays only the story's own commits. A command in it is
// built from the checked id, trunk as a shell reads it, and B's checked hash.
func (l *lander) rebaseFix(ctx context.Context, base, trunkTip, tip string) string {
	root, trunk, branch, in := l.s.Root(), shellWord(l.m.trunk), store.StoryBranch(l.id), worktreeIn(l.id)
	if holds, err := gitx.IsAncestor(ctx, root, base, trunkTip); err == nil && holds {
		return `in the story's worktree, ` + in + ` in the main checkout, run "git rebase ` + trunk + `" to put ` +
			branch + ` on top of ` + trunk + `, then "sdlc stop" there again, which runs the configured commands on the ` +
			`rebased tree; a rebase that stops on a conflict is abandoned with "git rebase --abort", and an assistant ` +
			`then hands the story to a person with "sdlc escalate" and stops`
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
	branch, in := store.StoryBranch(l.id), worktreeIn(l.id)
	switch e.Code {
	case sdlcerr.FreezeBroken:
		return e.WithFix(`a person restores the frozen tests on ` + branch + ` and runs "sdlc stop" in ` + in +
			` again; or, where trunk's change to them is to stay, they reopen the story: ` + unfreezeRoute(l.id))
	case sdlcerr.UnfrozenTests:
		return e.WithFix(`a person decides whether the test files the freeze does not hold, which a rebase can bring ` +
			`in from trunk, belong with the story's: they take them out of ` + branch + ` and run "sdlc stop" in ` + in +
			` again; or, to keep them, they reopen the story: ` + unfreezeRoute(l.id))
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
	root := l.s.Root()
	dirty, err := gitx.Uncommitted(ctx, root, true)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return sdlcerr.New(sdlcerr.TreeNotCommitted,
			"the story's worktree has changes nobody committed: "+quoteSome(dirty),
			"the configured commands run on the tree that lands, and would run with these in it").
			WithFix(`commit them on ` + store.StoryBranch(l.id) + ` -- the change is then not the one reviewed, and ` +
				`the reviews are recorded again -- or stash them with "git stash push -- <path>", in ` +
				worktreeIn(l.id) + `, then run "sdlc stop" there again`)
	}
	// The project's lock is let go for the run: the commands are the
	// project's own, and can run for longer than any other command waits for
	// it. The landing's own lock stays held.
	(*l.unlock)()
	*l.unlock = func() {}
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

// runCommand runs one configured command in the worktree, in a process group
// of its own, and holds it to the pass rule and to leaving S as it was.
func (l *lander) runCommand(ctx context.Context, st landState, key, command string) error {
	fmt.Fprintf(l.cmd.ErrOrStderr(), "sdlc: running commands.%s on %s in %s\n", key, st.tip[:12], worktreeIn(l.id))
	c, err := shellx.Command(ctx, l.s.Root(), command)
	if err != nil {
		return sdlcerr.New(sdlcerr.LandingCheckFailed,
			"commands."+key+" could not be run", err.Error()).WithCause(err)
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
			"commands."+key+" could not be run", runErr.Error()).WithCause(runErr)
	}
	slog.Debug("landing ran a command", "story", l.id, "command", "commands."+key, "trunk", st.trunkTip[:12],
		"tip", st.tip[:12], "exit", exit, "duration", time.Since(started))
	if ok, how := passes(key, exit, stdout.String(), thresholdOf(st.trunkCfg, key)); !ok {
		return l.commandFailed(st, key, how, output.String())
	}
	return l.unmoved(ctx, st, key)
}

// commandFailed is SDLC-E0055 for a command that did not pass. What it
// printed is set apart under a line of its own, after everything sdlc says,
// so that none of it can pass for the refusal's why or its fix.
func (l *lander) commandFailed(st landState, key, how, printed string) error {
	why := how + "; the landing runs the configured commands on the tree that lands whenever the story was rebased " +
		"or its change is not the one that was reviewed. " + l.nothingLanded()
	if printed == "" {
		why += "\n\n  commands." + key + " printed nothing"
	} else {
		why += "\n\n  commands." + key + " printed:\n" + setApart(printed)
	}
	return closedRefusal{sdlcerr.New(sdlcerr.LandingCheckFailed,
		"commands."+key+", as trunk commits it at "+st.trunkTip[:12]+", failed on "+store.StoryBranch(l.id)+" at "+
			st.tip[:12], why)}
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
			"commands."+key+" moved HEAD or "+branch+" while it ran",
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
			"commands."+key+" changed files the story commits: "+quoteSome(changed),
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
			"they passed "+before.tip[:12]+", and "+store.StoryBranch(l.id)+" is at "+after.tip[:12]+" now").
			WithFix(`run "sdlc stop" in ` + worktreeIn(l.id) + ` again, which runs them on the tree as it is now`)
	case after.trunkTip != before.trunkTip:
		return before, sdlcerr.New(sdlcerr.BranchBehindTrunk,
			l.m.trunk+" moved while the configured commands ran",
			"it was at "+before.trunkTip[:12]+" when they started, and is at "+after.trunkTip[:12]+" now").
			WithFix(l.rebaseFix(ctx, after.base, after.trunkTip, after.tip))
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
		if now, terr := gitx.BranchTip(ctx, l.s.Root(), l.m.trunk); terr == nil {
			return refusal.WithFix(l.rebaseFix(ctx, st.base, now, st.tip))
		}
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
			moved += "; the files in " + quote(holding[0]) + ", which has " + trunk + " checked out, moved with it."
		default:
			moved += "; the files in " + quoteAll(holding) + ", which have " + trunk + " checked out, moved with them."
		}
		lines = append(lines, moved)
	}
	if st.from != st.base {
		lines = append(lines, fmt.Sprintf("The reviewed change began at %s; the record names %s..%s as what landed.",
			st.base[:12], st.from[:12], st.tip[:12]))
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
	// Out of the worktree first: Windows does not remove a directory that is
	// a process's current one. Elsewhere the process stays where it was.
	if runtime.GOOS == "windows" {
		if err := os.Chdir(l.m.main); err != nil {
			slog.Debug("stop could not move into the main checkout", "err", err)
		}
	}
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
		line += refusal.What + ", and " + refusal.Why + "."
	default:
		line += err.Error() + "."
	}
	line += " `sdlc doctor` in the main checkout says what to do with it."
	if err != nil && runtime.GOOS == "windows" {
		line += " On Windows, a shell whose current directory is in the worktree keeps it there: leave it, then run " +
			"`sdlc doctor` again."
	}
	return line
}

// closedRefusal marks a refusal whose Why already ends with the sentence every
// landing refusal ends with, put there by the caller itself for its own
// reasons (commandFailed, to keep it ahead of what a command printed). refused
// reads this flag rather than searching Why for the words "Nothing landed":
// a quoted escalation message or a command's own output can contain them
// without meaning the sentence is there.
type closedRefusal struct{ err *sdlcerr.Error }

func (c closedRefusal) Error() string { return c.err.Error() }
func (c closedRefusal) Unwrap() error { return c.err }

// refused ends a refusal of the landing as every one ends: nothing landed, and
// where the story is.
func (l *lander) refused(err error) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		return err
	}
	slog.Debug("landing refused", "story", l.id, "code", e.Code.String())
	var closed closedRefusal
	if errors.As(err, &closed) {
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
	case "coverage":
		return cfg.Thresholds.CoverageMin
	case "mutation":
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
	case key != "coverage" && key != "mutation" || threshold <= 0:
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
