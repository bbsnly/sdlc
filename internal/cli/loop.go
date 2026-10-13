package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

const securityFlag = "security-sensitive"

// ---------------------------------------------------------------- start

type startPayload struct {
	OK     bool   `json:"ok"`
	Story  string `json:"story"`
	Title  string `json:"title"`
	Resume bool   `json:"resume"`
	// NextGate is the gate to work now. A resumed story has passed some gates
	// already, and a session told only that it resumed starts again at Gate 1.
	NextGate string `json:"next_gate,omitempty"`
	// Session is the Claude Code session this start ran in and recorded as the
	// one working the story, and empty when it ran in none. The hook holds only
	// that session to the loop's rules, so a runbook that finds it empty has
	// nothing enforcing anything, and stops.
	Session string `json:"session"`
	// Worktree and Branch are the story's own worktree and its branch, where
	// the checkout the story is held by is one; Main is then the main
	// checkout, where the backlog and the story's record are kept. A story
	// held anywhere else has none of the three.
	Worktree string `json:"worktree,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Main     string `json:"main,omitempty"`
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [STORY]",
		Short: "Begin an iteration on the next story, or on the one you name",
		Long: "start marks a story as the one being worked on. Everything the loop does\n" +
			"afterwards -- gates, hooks, the record it keeps -- is about that story.\n\n" +
			"With no argument it takes the next runnable story: one already under way\n" +
			"if there is one, otherwise the highest-priority story whose dependencies\n" +
			"are done. Running it again on the same story is safe and changes nothing.",
		Example: "  sdlc start\n  sdlc start AUTH-3",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			asked := ""
			if len(args) == 1 {
				asked = args[0]
			}
			said := &startSaid{}
			err := runStart(cmd, asked, said)
			// Once start knows where it went on to, if anywhere. A refusal is
			// printed after the command returns, so this still comes first.
			notePassedOver(cmd.ErrOrStderr(), said)
			return err
		},
	}
}

// runStart is start: in a worktree of the story's own where git.worktrees is
// on in this checkout, and in this checkout, as the released tool did,
// where it is off.
func runStart(cmd *cobra.Command, asked string, said *startSaid) error {
	// A store that will not open is said by the start below that opens it.
	if s, _, err := openStore(cmd); err == nil {
		if s.Config().Git.WorktreesOn() {
			return startInWorktree(cmd, asked, said)
		}
		slog.Debug("start begins new work in this checkout: git.worktrees is false")
	}
	checked, err := checkTrunkForNewWork(cmd, asked, false, said)
	if err != nil {
		return err
	}
	unchecked, err := startLocked(cmd, asked, checked, said)
	if err != nil || !unchecked {
		return err
	}
	// What start begins was decided again under the lock, and another
	// checkout had moved the story on in between: trunk is checked for it now,
	// outside the lock, and start decides once more.
	if _, err := checkTrunkForNewWork(cmd, asked, true, said); err != nil {
		return err
	}
	_, err = startLocked(cmd, asked, true, said)
	return err
}

// startLocked is start under the repository's lock. It does nothing and says so
// when the story it would begin is new and trunk has not been checked for it:
// the check runs the project's own command, and is never run holding the lock.
func startLocked(cmd *cobra.Command, asked string, checked bool, said *startSaid) (unchecked bool, err error) {
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return false, err
	}
	defer unlock()

	plan, err := decideStart(s, asked, s.CommandSession())
	said.passed = plan.passedOver
	switch {
	case err != nil:
		return false, err
	case plan.fresh && !checked:
		return true, nil
	case plan.refused != nil:
		return false, plan.refused
	}
	// The iteration's own files go to the checkout that holds the story: the
	// story's worktree, where the plan names it, or this checkout. The backlog
	// and the record are the repository's, and go through this store.
	target := s
	if plan.at != "" {
		if target, err = s.At(plan.at); err != nil {
			return false, err
		}
	}
	if plan.running {
		return false, carryOnStory(cmd, target, plan, said)
	}
	return false, beginIteration(cmd, s, target, plan, said)
}

// carryOnStory picks up an iteration that is already running in the target.
func carryOnStory(cmd *cobra.Command, target *store.Store, plan startPlan, said *startSaid) error {
	// Picked up in another session, the story is that session's now.
	was, _ := target.WorkingSession()
	session, err := target.BindSession(true)
	if err != nil {
		return err
	}
	if session != "" && was != "" && was != session {
		slog.Debug("start took the story over from the session working it",
			"story", plan.id, "checkout", target.Root(), "from", was, "to", session)
	}
	if err := target.Claim(plan.id); err != nil {
		return err
	}
	said.went = plan.id
	return reportStart(cmd, target, plan.id, true, session, plan.at != "")
}

// beginIteration starts an iteration on the story the plan names, or picks it
// up again.
func beginIteration(cmd *cobra.Command, s, target *store.Store, plan startPlan, said *startSaid) error {
	id := plan.id
	// A story that is already in progress is being picked up again, whether
	// or not the iteration that started it is still running.
	resume := !plan.fresh
	// Picking a story up again is the no-op the help promises it is:
	// SetStoryStatus writes nothing when the status is already the one asked
	// for, which keeps a resume out of the tree the reviewers were stamped
	// against.
	if err := s.SetStoryStatus(id, model.StatusInProgress); err != nil {
		return err
	}
	if err := target.SetActive(id); err != nil {
		return err
	}
	session, err := target.BindSession(false)
	if err != nil {
		return err
	}

	record, err := s.Record(id)
	if err != nil {
		return err
	}
	released := releasedSinceStart(record)
	// Where trunk is now is where this iteration's work starts from. Outside a
	// repository there is no trunk to hold it to.
	if head, err := gitx.Head(cmd.Context(), target.Root()); err == nil {
		record.Base = head
	}
	record.Append("loop_start", "iteration started", s.Now())
	if err := s.SaveRecord(record); err != nil {
		return err
	}
	// Last, so that a start killed before it leaves a story in progress,
	// which the next start in any checkout picks up first.
	if err := target.Claim(id); err != nil {
		return err
	}
	if released {
		noteRelease(cmd.ErrOrStderr(), target, id)
	}
	said.went = id
	return reportStart(cmd, target, id, resume, session, plan.at != "")
}

// refuseIfNotRunnable holds a story named to `sdlc start` to what picking one
// would. Naming a story chooses it over the priority order; it did not mean
// starting one that was dropped, blocked, marked done, or waiting on a story
// that is not done -- and all four started without a word.
func refuseIfNotRunnable(b *model.Backlog, story *model.Story) error {
	id := story.ID
	switch story.Status {
	case model.StatusDropped:
		return sdlcerr.New(sdlcerr.NoRunnableStory,
			quote(id)+" is dropped",
			"the backlog marks it dropped, and nobody is coming back to a dropped story").
			WithFix("set its status to ready in the backlog if it is wanted after all")
	case model.StatusBlocked:
		return sdlcerr.New(sdlcerr.NoRunnableStory,
			quote(id)+" is blocked",
			"the backlog marks it blocked, so something has to change before it can be worked").
			WithFix("set its status back to ready once what blocked it is dealt with")
	case model.StatusDone:
		return sdlcerr.New(sdlcerr.NoRunnableStory,
			quote(id)+" is marked done",
			"the backlog says it is finished, though its gate record does not").
			WithFix("set its status back to ready in the backlog if the work is not finished")
	case model.StatusTodo, model.StatusReady, model.StatusInProgress, model.StatusAwaitingHuman:
		// Runnable by status; what it waits on is checked below.
	}
	blocked := b.BlockedBy(story)
	if len(blocked) == 0 {
		return nil
	}
	refusal := sdlcerr.New(sdlcerr.NoRunnableStory,
		quote(id)+" waits on "+strings.Join(b.Waiting(story), ", "),
		"a story starts once every story in its depends_on is done")
	if b.NeverDone(story) {
		return refusal.WithFix("take the dropped or missing story out of " + id + "'s depends_on " +
			"-- it will never be done, so nothing else unblocks this one")
	}
	return refusal.WithFix("finish " + strings.Join(blocked, ", ") + " first, or take " +
		plural(len(blocked), "it", "them") + " out of " + id + "'s depends_on")
}

// resuming is whether starting a story in this status picks it up again rather
// than beginning it.
func resuming(status model.Status) bool {
	return status == model.StatusInProgress || status == model.StatusAwaitingHuman
}

// refuseIfFinished stops a story whose gates have all passed from being put
// back to work by starting it. Without this, `sdlc start US-001` would reset a
// finished story to in_progress with nothing on the record saying why, and the
// loop would work it a second time -- which is the thing the gate record was
// made the authority on status to prevent.
//
// The way back in is a gate recorded as failed, so that reopening is a decision
// somebody made and not a side effect of a command.
//
// It returns the concrete type so that a caller can aim the fix at its own
// case; nothing assigns the result to an error before checking it for nil.
func refuseIfFinished(s *store.Store, id string) (*sdlcerr.Error, error) {
	record, err := s.Record(id)
	if err != nil {
		return nil, err
	}
	if _, remaining := record.NextGate(); remaining {
		return nil, nil //nolint:nilnil // nil, nil is the documented "not finished": every caller tests the refusal for nil
	}
	// The story is named: no iteration runs when this refuses a start, and a
	// gate recorded without --story is refused for that, which sent whoever
	// followed the fix back to start and round again.
	return sdlcerr.New(sdlcerr.StoryAlreadyFinished,
		quote(id)+" is finished",
		"every gate on it has passed, so starting it would put work that is "+
			"already done back in progress").
		WithFix(`record a gate as failed to reopen it -- "sdlc gate code_review fail --story ` + id +
			` --note ..." says on the record why the work came back`), nil
}

// nextRunnable picks the story to work on, and explains itself when there is
// none rather than just refusing.
//
// A story another checkout holds is not there to pick, and one waiting on it
// waits as if it were not done: resuming the first story under way handed a
// worktree the story the main checkout was working.
//
// It also returns the stories it passed over because the checkout holding
// them no longer exists, whether or not it found one to pick.
func nextRunnable(s *store.Store) (string, []goneHolder, error) {
	backlog, err := s.Backlog()
	if err != nil {
		return "", nil, err
	}
	holders, err := s.Holders()
	if err != nil {
		return "", nil, err
	}
	held, passed := elsewhere(backlog, holders), goneHolders(backlog, holders)
	if sel, ok := backlog.Without(held...).Next(); ok {
		return sel.Story.ID, passed, nil
	}
	return "", passed, sdlcerr.New(sdlcerr.NoRunnableStory,
		"there is no story to start",
		describeWhyNothingRuns(backlog)+heldNote(held))
}

func describeWhyNothingRuns(b *model.Backlog) string {
	if len(b.Stories) == 0 {
		return "the backlog has no stories in it"
	}
	counts := map[model.Status]int{}
	waiting := 0
	for i := range b.Stories {
		counts[b.Stories[i].Status]++
		if len(b.BlockedBy(&b.Stories[i])) > 0 {
			waiting++
		}
	}
	total := len(b.Stories)
	switch {
	case counts[model.StatusDone] == total:
		return plural(total, "the one story in the backlog is finished",
			"every story in the backlog is finished")
	case counts[model.StatusDone]+counts[model.StatusDropped] == total:
		// Dropped is abandoned, not finished, and saying "finished" would
		// credit the loop with work nobody did.
		return "every story in the backlog is either finished or dropped"
	}
	desc := fmt.Sprintf("of %d %s, %d done and %d blocked",
		total, plural(total, "story", "stories"),
		counts[model.StatusDone], counts[model.StatusBlocked])
	if waiting > 0 {
		desc += fmt.Sprintf(", and %d waiting on a dependency that is not done", waiting)
	}
	return desc
}

// nothingLeftToStart separates a backlog the loop has worked through from one
// it is stuck on. The two want opposite things from the reader -- write the
// next story, or go and look at what is blocking -- so one sentence for both
// sends half of them the wrong way.
func nothingLeftToStart(b *model.Backlog) bool {
	for i := range b.Stories {
		switch b.Stories[i].Status {
		case model.StatusDone, model.StatusDropped:
		default:
			return false
		}
	}
	return true
}

// writeAStory is the action in both cases where there is nothing to be stuck on.
// It names the backlog the project configured: backlog.path need not be the
// default, and there is no command that writes a story for you.
func writeAStory(path string) string {
	return "Add a story to " + path + " (schema: .sdlc/templates/story.schema.json).\n"
}

// nothingToStart is what `sdlc status` says when no story can be started.
// held is every story another checkout holds.
func nothingToStart(b *model.Backlog, path string, held []string) string {
	if len(b.Stories) == 0 {
		return "\nThe backlog is empty. " + writeAStory(path)
	}
	said := "\nNothing to start: " + describeWhyNothingRuns(b) + heldNote(held) + ".\n"
	if nothingLeftToStart(b) {
		return said + writeAStory(path)
	}
	return said + "`sdlc story list` shows what is holding each story back.\n"
}

// reportStart says what start did. elsewhere is whether it ran in another
// checkout than the one it acted in, the story's worktree.
func reportStart(cmd *cobra.Command, s *store.Store, id string, resume bool, session string, elsewhere bool) error {
	story, _, err := s.Story(id)
	if err != nil {
		return err
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	tree := storyTreeOf(cmd.Context(), s, id)
	if wantJSON(cmd) {
		payload := startPayload{OK: true, Story: id, Title: story.Title, Resume: resume, Session: session,
			Worktree: tree.path, Branch: tree.branch, Main: tree.main}
		if next, ok := record.NextGate(); ok {
			payload.NextGate = string(next)
		}
		return emitJSON(cmd.OutOrStdout(), payload)
	}
	w := cmd.OutOrStdout()
	verb := "Started"
	if resume {
		verb = "Resumed"
	}
	fmt.Fprintf(w, "%s %s  %s\n\n", verb, id, oneLine(story.Title))
	if tree.path == "" {
		printGates(w, record)
		fmt.Fprint(w, "\nNext: a person types /sdlc:next in Claude Code to work the story.\n")
		return nil
	}
	fmt.Fprintf(w, "Worktree: %s\nBranch: %s\n\n", sdlcerr.QuotePath(tree.path), tree.branch)
	printGates(w, record)
	if elsewhere {
		// A session opened here holds no story, so /sdlc:next here starts
		// again, and passes this story over as held by its worktree. The
		// session that ran start holds it, which is why a new one is named.
		fmt.Fprint(w, "\nNext: a person opens Claude Code in the worktree above and types /sdlc:next there to work "+
			"the story; /sdlc:next typed in a new session in this checkout begins the next story instead.\n")
		return nil
	}
	fmt.Fprint(w, "\nNext: a person types /sdlc:next in Claude Code, opened in this checkout or in the worktree above, "+
		"to work the story there.\n")
	return nil
}

// storyTree is where a story's own worktree is, as start reports it.
type storyTree struct{ path, branch, main string }

// storyTreeOf is the story's own worktree, where s is it, and nothing for any
// other checkout: a story the main checkout holds, or a worktree a person made,
// is reported as the released tool reported it. The path is the one start
// makes it at, spelled the same from every checkout, so that a runbook can
// compare what two starts said.
func storyTreeOf(ctx context.Context, s *store.Store, id string) storyTree {
	if of, ok := s.StoryWorktreeOf(); !ok || of != id {
		return storyTree{}
	}
	path, err := s.StoryWorktree(id)
	if err != nil {
		return storyTree{}
	}
	co, _ := s.Checkout()
	branch, err := gitx.Branch(ctx, s.Root())
	if err != nil || branch == "" {
		// Detached, or unreadable: the branch start made it on.
		branch = store.StoryBranch(id)
	}
	return storyTree{path: path, branch: branch, main: co.Main}
}

// ---------------------------------------------------------------- stop

type stopPayload struct {
	OK    bool   `json:"ok"`
	Story string `json:"story,omitempty"`
	WasA  bool   `json:"was_active"`
	// Done distinguishes putting a story down from finishing it, which is the
	// difference between `sdlc start` picking it up again and moving on.
	Done bool `json:"done"`
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "End the iteration; in a finished story's worktree, land it on trunk",
		Long: "stop ends the iteration. It records nothing about the story and changes no\n" +
			"gate: the work stays exactly where it was, and starting again picks it up.\n\n" +
			"In a finished story's own worktree, .sdlc/worktrees/<ID>, stop lands the story\n" +
			"instead: it checks the story's branch against trunk, runs the configured\n" +
			"commands where the story was rebased or its change is not the one reviewed,\n" +
			"moves trunk to the story's commits by fast-forward, marks the story done, and\n" +
			"takes the worktree and its branch away. A finished story a person answered for\n" +
			"after an escalation lands the same way, with no iteration running. To put a\n" +
			"finished story down without landing it, hand it to a person with\n" +
			"`sdlc escalate`.\n\n" +
			"Stopping when nothing is running is not an error.",
		Example: "  sdlc stop",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runStop(cmd) },
	}
}

func runStop(cmd *cobra.Command) error {
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	// Called through the variable: a landing lets the lock go while
	// its commands run, and takes it again.
	defer func() { unlock() }()
	active, broken, err := activeToStop(cmd, s)
	if err != nil {
		return err
	}
	if !broken {
		if landed, err := landIfFinished(cmd, s, active, &unlock); landed || err != nil {
			return err
		}
	}
	done, closing, err := endIteration(cmd, s, active)
	if err != nil {
		return err
	}
	if err := s.ClearActive(); err != nil {
		return err
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(),
			stopPayload{OK: true, Story: active, WasA: active != "", Done: done})
	}
	switch {
	case active == "":
		fmt.Fprintln(cmd.OutOrStdout(), "No iteration was running.")
	case done:
		fmt.Fprintf(cmd.OutOrStdout(),
			"Ended the iteration on %s. Every gate passed, so the story is done "+
				"and `sdlc start` moves on to the next one.\n", active)
	case closing != "":
		fmt.Fprint(cmd.OutOrStdout(), closing)
	default:
		fmt.Fprintf(cmd.OutOrStdout(),
			stoppedIterationOn+"%s. Nothing was recorded; `sdlc start` picks it up again.\n",
			active)
	}
	return nil
}

// activeToStop is the story the iteration is on. broken is set where the file
// that names it cannot be used, and the iteration is ended without one.
func activeToStop(cmd *cobra.Command, s *store.Store) (active string, broken bool, err error) {
	active, err = s.Active()
	var unusable *sdlcerr.Error
	switch {
	case errors.As(err, &unusable) &&
		(unusable.Code == sdlcerr.UnsafeStoryID || unusable.Code == sdlcerr.StateUnreadable):
		// While the file names no story the hook enforces nothing, and
		// doctor sends you here. Stop is the way out of a broken state,
		// so it ends the iteration rather than refuse over the file.
		fmt.Fprintln(cmd.ErrOrStderr(), "sdlc: .sdlc/state/active does not name a story, "+
			"so this stop removes it without writing to any record.")
		return "", true, nil
	case err != nil:
		return active, true, err
	}
	return active, false, nil
}

// endIteration writes the end of the iteration to the story's record where
// stop may, and reports whether that finished the story. closing is the line
// stop ends with in place of its usual one.
func endIteration(cmd *cobra.Command, s *store.Store, active string) (done bool, closing string, err error) {
	if active == "" {
		return false, "", nil
	}
	onRecord, closing := onTheRecord(cmd, s, active)
	if !onRecord {
		return false, closing, nil
	}
	done, err = recordLoopEnd(cmd, s, active)
	return done, closing, err
}

// recordLoopEnd appends the end of the iteration to the story's record, and
// settles the story if every gate has passed.
func recordLoopEnd(cmd *cobra.Command, s *store.Store, active string) (bool, error) {
	record, err := s.Record(active)
	var unreadable *sdlcerr.Error
	switch {
	case errors.As(err, &unreadable) && unreadable.Code == sdlcerr.StateUnreadable:
		// The hook refuses a commit while the record cannot be
		// read, and its refusal sends you here to commit as
		// yourself. So stop must not be one more thing the broken
		// record traps: the iteration ends, off the record.
		fmt.Fprintln(cmd.ErrOrStderr(), "sdlc: .sdlc/stories/"+active+"/"+model.RecordFile+
			" could not be read, so this stop is not written to it. Run `sdlc doctor` to see why.")
		return false, nil
	case err != nil:
		return false, err
	}
	record.Append("loop_end", "iteration ended", s.Now())
	if err := s.SaveRecord(record); err != nil {
		return false, err
	}
	// The same function that owns the invariant, so that the
	// sentence stop prints and the backlog cannot disagree.
	done, err := settleStory(s, active, record)
	if err != nil {
		// A story the backlog has lost must not trap the
		// iteration: stop is the way out of a broken state.
		var known *sdlcerr.Error
		if !errors.As(err, &known) || known.Code != sdlcerr.StoryNotFound {
			return done, err
		}
	}
	if done {
		if err := releaseFreeze(s, active); err != nil {
			return done, err
		}
		forgetWatched(cmd.Context(), s, active)
	} else if err := s.KeepClaim(active); err != nil {
		// Put down, the story stays this checkout's until it is
		// finished or a person releases it.
		return done, err
	}
	return done, nil
}

// What stop says when it puts an iteration down without writing to a record.
const (
	stoppedIterationOn = "Stopped the iteration on "
	notRecordedHere    = " in this checkout. Nothing was recorded; "
)

// onTheRecord reports whether stop writes to the story's record. It does not
// where this checkout has no business writing it: with no main checkout to keep
// it in, or when another checkout holds the story. Stop is the way out of
// either, so it says why nothing is recorded and ends the iteration anyway.
//
// closing is the line stop ends with instead of its usual one, where that one
// would send the reader to a start that refuses the story or passes it over.
func onTheRecord(cmd *cobra.Command, s *store.Store, active string) (onRecord bool, closing string) {
	if s.SharedErr() != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "sdlc: the backlog and the story's record are kept in the repository's "+
			"main checkout, and this repository has none, so this stop is not written to any record. "+
			"Run `sdlc doctor` to see why.")
		return false, ""
	}
	h, elsewhere, err := s.HeldElsewhere(active)
	switch {
	case err != nil || !elsewhere:
		// What cannot be read is said by the stop that reads it.
		return true, ""
	case h.Tied():
		fmt.Fprintf(cmd.ErrOrStderr(), "sdlc: %s is named by more than one checkout of this repository, "+
			"so this stop puts it down here and writes nothing to its record.\n", active)
		return false, stoppedIterationOn + active + notRecordedHere +
			"once one checkout alone names it, `sdlc start` there picks it up again.\n"
	case !h.Live:
		fmt.Fprintf(cmd.ErrOrStderr(), "sdlc: %s is held by a checkout that no longer exists, "+
			"so this stop puts down this checkout's copy and writes nothing to its record.\n", active)
		return false, stoppedIterationOn + active + notRecordedHere +
			"the checkout that holds it is gone: a person runs `sdlc release " + active + " --reason \"...\"` " +
			"in their own terminal, then `sdlc start " + active + "`.\n"
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "sdlc: %s is held by another checkout of this repository, "+
			"so this stop puts down this checkout's copy and writes nothing to its record.\n", active)
		return false, stoppedIterationOn + active + notRecordedHere +
			"the story carries on in the checkout that holds it.\n"
	}
}

// ---------------------------------------------------------------- gate

type gatePayload struct {
	OK     bool   `json:"ok"`
	Story  string `json:"story"`
	Gate   string `json:"gate"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
	// CommitBase and Commit bound the story's commits, when this was the commit
	// gate passing: they are the ones after CommitBase, up to and including
	// Commit. CommitBase is left out when it is not known.
	CommitBase string `json:"commit_base,omitempty"`
	Commit     string `json:"commit,omitempty"`
	// RangeUnknown is the commit gate recording, deliberately, no range.
	RangeUnknown bool `json:"range_unknown,omitempty"`
	// HeadPastCommit is HEAD at this pass, where it was past Commit.
	HeadPastCommit string `json:"head_past_commit,omitempty"`
	// Reopened is the gates this record took back, in gate order.
	Reopened []string `json:"reopened,omitempty"`
	// Done reports that this gate was the last one: the story left the backlog.
	Done bool `json:"done"`
	// HandedOver is the kind of escalation, when this failure was one too many
	// and the story went to a person.
	HandedOver string `json:"handed_over,omitempty"`
}

// settleStory keeps the backlog honest about a story whose gates have all
// passed. Nothing else moves a story to done, and a story that stays
// in_progress after its retro is one `sdlc start` picks up again forever.
//
// The record decides, not the caller: "done" means there is no gate left, and
// a gate recorded as failed afterwards puts the story back to work.
//
// It reads the backlog itself so that every caller gets the write as well as
// the answer. A caller that worked out "finished" for itself and only printed
// it would say one thing while the backlog said another.
func settleStory(s *store.Store, id string, record *model.Record) (bool, error) {
	story, _, err := s.Story(id)
	if err != nil {
		return false, err
	}
	_, remaining := record.NextGate()
	switch {
	case !remaining && story.Status != model.StatusDone && inItsWorktree(s, id):
		// In its own worktree a story is done once trunk has its commits, and
		// only the landing gives it them: until then it keeps its claim.
		return false, nil
	case !remaining && story.Status != model.StatusDone:
		if err := s.SetStoryStatus(id, model.StatusDone); err != nil {
			return false, err
		}
		// Finished, the story is nobody's to hold.
		return true, s.ReleaseClaim(id)
	case remaining && story.Status == model.StatusDone:
		return false, s.SetStoryStatus(id, model.StatusInProgress)
	}
	return !remaining, nil
}

// releaseFreeze lifts a finished story's test freeze.
//
// The freeze belongs to the iteration it was taken in. It holds across every
// gate and across sessions, which is the whole point -- and it is lifted only
// where the iteration ends, on a story that has nothing left to do. Lifting it
// when the last gate passes instead would be a step too early: a gate can be
// re-recorded as a failure, which puts the story back in progress, and it
// would then run gates 5 to 8 again with nothing frozen.
//
// Left behind, it becomes the next story's problem: `sdlc freeze` at Gate 3
// refuses with "already frozen" and sends the reader to `sdlc unfreeze`, an
// override meant for changing tests mid-story and recorded as a deviation.
// Every project's second story stopped there.
//
// Only the finished story's own freeze is lifted. One that names a different
// story is not this story's to release.
//
// It goes on the record like any other lifting. `sdlc freeze` refuses a story
// whose freeze is gone without one, and a finished story can be put back in
// progress by re-recording a gate.
func releaseFreeze(s *store.Store, id string) error {
	lock, err := s.Lock()
	if err != nil || lock == nil || lock.Story != id {
		return err
	}
	if err := s.ClearLock(); err != nil {
		return err
	}
	return appendEvent(s, id, unfreezeEvent, finishedUnfreeze)
}

// forgetWatched clears what the hook wrote down about a finished story's
// commits, in the git directory, where nothing else would ever remove it. It
// runs where the freeze is lifted, on a story with nothing left to do, and not
// at a gate: a commit gate passed a second time needs the first pass's
// observations. Best effort, because stop is the way out of a broken state and
// a leftover observation costs one small file.
//
// Two things can leave one behind. The hook asks only for the gates before the
// commit, so a commit made after the commit gate passed is still watched, and
// that write can land after this clear: one file for a finished story, cleared
// the next time it finishes. And a second session that runs /sdlc:next mid-story
// takes the story over, so the first session's later commits are neither
// refused nor watched, and drop out of what the gate reads: whichever session
// most recently claimed the story is the one watched, which under-claims and
// answers no range rather than a wrong one.
func forgetWatched(ctx context.Context, s *store.Store, id string) {
	if err := s.ClearObservedCommits(ctx, id); err != nil {
		slog.Debug("stop could not forget the commits the loop watched", "story", id, "err", err)
	}
}

// finishedUnfreeze is what lifting a finished story's freeze says on its
// record. It is the loop tidying up rather than somebody changing the tests,
// and `sdlc log` leaves it out of what it flags.
const finishedUnfreeze = "the story is finished, so its freeze is lifted"

// unfreezeEvent is the type of the event that records a freeze lifted, by a
// person or by the stop.
const unfreezeEvent = "unfreeze"

func newGateCmd() *cobra.Command {
	var note, storyID string
	var securitySensitive bool
	cmd := &cobra.Command{
		Use:   "gate GATE STATUS",
		Short: "Record the outcome of one gate",
		Long: "gate writes down what happened at one of the loop's checkpoints. It records\n" +
			"a decision; it does not make one.\n\n" +
			"Gates: " + model.GateNames() + "\n" +
			"Outcomes: pass, fail, pending",
		Example: "  sdlc gate analysis pass --note \"threats assessed, no trust boundary crossed\"\n" +
			"  sdlc gate code_review fail --note \"AC-2 is untested\"",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGate(cmd, args, gateFlags{note: note, story: storyID, securitySensitive: securitySensitive})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "why the gate came out this way, in one line")
	cmd.Flags().BoolVar(&securitySensitive, securityFlag, false,
		"the story touches a trust boundary, so the security review blocks "+
			"(set at the analysis gate; assumed true until it is set)")
	cmd.Flags().StringVar(&storyID, "story", "",
		"record against this story instead of the one being worked on")
	return cmd
}

// gateFlags are the flags `sdlc gate` was given.
type gateFlags struct {
	note, story       string
	securitySensitive bool
}

// gateRun is one `sdlc gate` in progress: the story, the gate and outcome being
// recorded, and the record they are recorded on.
type gateRun struct {
	cmd    *cobra.Command
	s      *store.Store
	rd     *reading
	id     string
	gate   model.Gate
	status model.GateStatus
	note   string
	record *model.Record
}

func runGate(cmd *cobra.Command, args []string, flags gateFlags) error {
	gate, status, err := parseGate(args)
	if err != nil {
		return err
	}

	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	id := flags.story
	if id == "" {
		if id, err = activeStory(s, "gate records an outcome for the story being worked on"); err != nil {
			return err
		}
	}
	if err := refuseIfHeldElsewhere(s, id, "what a gate records is measured on the work in that checkout, not this one"); err != nil {
		return err
	}

	// A gate is recorded against a story in the backlog, and the
	// backlog is read before anything is written: a mistyped --story
	// is refused rather than starting a record nothing will ever read.
	if _, _, err := s.Story(id); err != nil {
		return err
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	// What this gate asks git about the tree and HEAD, up to saving the
	// record, which writes nothing either of them measures.
	g := &gateRun{cmd: cmd, s: s, rd: readingOf(s), id: id, gate: gate, status: status, note: flags.note, record: record}
	// The one thing a gate writes whether or not it is then refused, and before
	// checkAndStaff has put anything else on the record: which commits are the
	// story's own.
	if err := noteForks(cmd.Context(), s, g.rd, id, record); err != nil {
		return err
	}
	if err := g.checkAndStaff(flags); err != nil {
		return err
	}
	reopened := record.SetGate(gate, status, flags.note, s.Now())
	if err := g.noteCommits(); err != nil {
		return err
	}
	record.Append(model.EventGate, model.GateEvent(gate, status), s.Now())
	handedOver, message, err := g.save()
	if err != nil {
		return err
	}
	// A story handed over is the backlog's awaiting_human, written with
	// the hand-over; there is nothing left to settle, and nothing left
	// that could fail once a person has been asked.
	done := false
	if handedOver == "" {
		if done, err = settleStory(s, id, record); err != nil {
			return err
		}
	}
	return g.report(reopened, done, handedOver, message)
}

// parseGate is the gate and the outcome the command line names.
func parseGate(args []string) (model.Gate, model.GateStatus, error) {
	gate := model.Gate(args[0])
	if !gate.Valid() {
		return "", "", sdlcerr.New(sdlcerr.UnknownGate,
			"there is no gate called "+quote(args[0]),
			"this version knows "+model.GateNames())
	}
	status := model.GateStatus(args[1])
	if !status.Valid() {
		return "", "", sdlcerr.New(sdlcerr.UnknownGateStatus,
			quote(args[1])+" is not a gate outcome",
			"a gate either passed, failed, or has not been reached yet")
	}
	return gate, status, nil
}

// checkAndStaff holds the gate to the evidence it needs, and takes what it
// measures onto the record, short of recording the gate itself.
func (g *gateRun) checkAndStaff(flags gateFlags) error {
	ctx, rd, record := g.cmd.Context(), g.rd, g.record
	// A passed code review is where the story's own commit is allowed,
	// so HEAD moved since then is that commit, and a gate reopened on it
	// measures from there. Held to where the story started, a code review
	// reopened on work already committed could never pass again.
	//
	// The commit gate passing reads HEAD once, after its checks, below:
	// read here, the HEAD it records would come before the clean check,
	// and a commit made in between would hold work the record leaves out.
	committing := g.gate == model.GateCommit && g.status == model.GatePass
	if record.Pass(model.GateCodeReview) && !committing {
		g.moveBase(ctx)
	}
	if g.status == model.GatePass {
		if err := requireEvidence(ctx, rd, g.id, g.gate, record); err != nil {
			return err
		}
	}
	if err := staffTheGate(ctx, rd, g.id, g.gate, g.status, record); err != nil {
		return err
	}
	if g.gate == model.GateAnalysis && g.cmd.Flags().Changed(securityFlag) {
		noteSensitivity(g.s, record, flags.securitySensitive)
		record.SetSecuritySensitive(flags.securitySensitive)
	}
	// Only a gate that judged the freeze says what it accepted: a pass
	// of any other would wipe what the last one that did had recorded.
	if g.status == model.GatePass && rd.judged {
		if recordAccepted(g.s, g.id, record, rd.trunkTests) != nil {
			fmt.Fprintln(g.cmd.ErrOrStderr(), "sdlc: "+acceptedLine(g.id, trunkName(g.s), rd.trunkTests, false)+".")
		}
	}
	return nil
}

// moveBase measures the story from HEAD as it is now, where HEAD can be read.
func (g *gateRun) moveBase(ctx context.Context) {
	if head, err := g.rd.headOf(ctx); err == nil {
		g.record.Base = head
	}
}

// noteCommits takes the story's commits onto the record when the gate that
// passes is the one that bounds them.
//
// The story's commits are among the ones after HEAD when code review
// first passed, up to and including HEAD when the commit gate last
// passed.
//
// The hook refuses them until code review has passed, and
// requireUnmoved has just held HEAD to where the story started, so
// HEAD at the first pass is trunk before any of them. Only that pass
// takes the start, and the record's history says which pass it was,
// not an empty field: a later pass would take a HEAD after whatever
// the story had committed by then. A first pass with no commit to
// name, or one an older sdlc recorded, leaves the start unknown.
//
// The commit gate refused anything uncommitted, so HEAD at its pass
// holds all of the reviewed work, and a pass where HEAD names no
// commit is not recorded. Which of the commits since the code review
// are the story's is placeCommits' to say.
func (g *gateRun) noteCommits() error {
	if g.status != model.GatePass {
		return nil
	}
	switch {
	case g.gate == model.GateCodeReview &&
		g.record.Count(model.EventGate, model.GateEvent(model.GateCodeReview, model.GatePass)) == 0:
		var err error
		g.record.CommitBase, err = headCommit(g.cmd.Context(), g.rd)
		return err
	case g.gate == model.GateCommit:
		return g.noteCommitGate()
	}
	return nil
}

// noteCommitGate records where the story landed when the commit gate passes.
func (g *gateRun) noteCommitGate() error {
	ctx, record := g.cmd.Context(), g.record
	head, err := headCommit(ctx, g.rd)
	if err != nil {
		return err
	}
	// Keyed on HEAD, not on what the record ends up naming: no
	// range is an outcome now, and a pass everywhere else.
	if head == "" {
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"HEAD names no commit, so the commit gate cannot record where the story landed",
			"the gate records the commit that holds the story's work, and git reports none")
	}
	// requireEvidence has just held this gate to a passed code
	// review, so this is the HEAD a gate reopened on the story
	// measures from, as every other gate takes it above.
	record.Base = head
	var at changeStart
	own := inItsWorktree(g.s, g.id)
	if own {
		if at, err = startOfChange(ctx, g.s, record, head); err != nil {
			return err
		}
	}
	landed := placeCommits(ctx, g.s, g.id, record, head, at)
	if err := requireNoteInRange(ctx, g.s.Root(), landed, g.note); err != nil {
		return err
	}
	if own {
		return recordChange(g.cmd, g.s, g.id, record, head, at)
	}
	return nil
}

// save writes the record. A failure is counted on it before it is saved: one
// that reaches the limit is saved with the hand-over, or not at all.
func (g *gateRun) save() (handedOver, message string, err error) {
	if g.status != model.GateFail {
		return "", "", g.s.SaveRecord(g.record)
	}
	limit := g.s.Config().Loop.MaxReworkRounds
	failures := g.record.SinceDecision(model.EventGate, model.GateEvent(g.gate, model.GateFail))
	message = fmt.Sprintf("%s has failed %d times, and loop.max_rework_rounds is %d, "+
		"so another attempt would meet the same wall. The last note: %s",
		g.gate, failures, limit, lastNote(g.note))
	handedOver, err = handOverAt(g.cmd, g.s, g.record, nil, limitCount{
		count: failures, limit: limit, kind: "gate_failing", message: message, what: "the gate failure",
	})
	return handedOver, message, err
}

// report says, or emits, what the gate recorded.
func (g *gateRun) report(reopened []string, done bool, handedOver, message string) error {
	cmd, record, id, gate, note := g.cmd, g.record, g.id, g.gate, g.note
	start, end, past := "", "", ""
	if gate == model.GateCommit {
		start, end, past = record.CommitBase, record.Commit, record.HeadPastCommit
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), gatePayload{
			OK: true, Story: id, Gate: string(gate), Status: string(g.status), Note: note,
			CommitBase: start, Commit: end, RangeUnknown: gate == model.GateCommit && record.RangeUnknown,
			HeadPastCommit: past, Reopened: reopened,
			Done: done, HandedOver: handedOver,
		})
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s  %s  %s\n", id, gate, g.status)
	if note != "" {
		fmt.Fprintf(w, "  %s\n", oneLine(note))
	}
	if len(reopened) > 0 {
		fmt.Fprintf(w, "  reopened: %s\n", strings.Join(reopened, ", "))
	}
	if end != "" {
		g.sayLanded(start, end, past)
	}
	if gate == model.GateCommit && record.RangeUnknown {
		fmt.Fprintln(w, "  no range recorded: "+noSingleRange)
	}
	if done {
		fmt.Fprintf(w, "\n%s is done: every gate has passed.\n", id)
	} else if _, remaining := record.NextGate(); !remaining && inItsWorktree(g.s, id) {
		fmt.Fprintf(w, "\nEvery gate on %s has passed. `sdlc stop` in %s lands it on %s.\n",
			id, worktreeIn(id), trunkName(g.s))
	}
	if handedOver != "" {
		sayHandedOver(w, id, message)
	}
	return nil
}

// sayLanded says where the commit gate found the story's commits.
func (g *gateRun) sayLanded(start, end, past string) {
	w := g.cmd.OutOrStdout()
	landed := end[:min(len(end), 12)]
	if start != "" {
		landed = start[:min(len(start), 12)] + ".." + landed
	}
	if inItsWorktree(g.s, g.id) {
		// Committed on its branch: trunk has none of it until the
		// story lands.
		fmt.Fprintf(w, "  committed on %s in %s\n", store.StoryBranch(g.id), landed)
	} else {
		fmt.Fprintf(w, "  landed in %s\n", landed)
	}
	// Not a refusal: the sign that the range may stop short, kept on
	// the record for `sdlc log`, and true in every case it fires, the
	// ordinary one included, where a person committed after the
	// session did. The reverse carries no sign: an end that is HEAD
	// can still be somebody else's commit.
	if past != "" {
		fmt.Fprintln(w, "  "+headPast(past, end))
	}
}

// ---------------------------------------------------------------- shared

func printGates(w io.Writer, record *model.Record) {
	recorded := 0
	for _, g := range model.Gates {
		result, ok := record.Gates[g]
		if !ok {
			continue
		}
		recorded++
		line := fmt.Sprintf("  %-16s %s", g, oneLine(string(result.Status)))
		if result.Note != "" {
			line += "  " + oneLine(result.Note)
		}
		fmt.Fprintln(w, line)
	}
	if recorded == 0 {
		fmt.Fprintln(w, "  no gates recorded yet")
	}
}

// quote escapes as well as quotes, as store's does: an id or a name can come
// from a file a clone brought, and one that ended the quotation would read as
// sdlc's.
func quote(s string) string { return strconv.Quote(s) }

// requireEvidence refuses to record a pass for a gate that has not left behind
// what the gates after it read.
//
// This is where the tool stops being a notebook. Asking the assistant to check
// first is exactly the instruction this project exists to stop relying on: the
// hole that started this line of work was a skill saying "do not do this
// yourself" and the model doing it anyway.
func requireEvidence(ctx context.Context, rd *reading, id string, gate model.Gate, record *model.Record) error {
	s := rd.s
	if err := requireOrder(gate, record); err != nil {
		return err
	}
	if err := requireDocuments(s, id, gate); err != nil {
		return err
	}
	if err := requireReviews(ctx, rd, id, gate, record); err != nil {
		return err
	}
	if slices.Contains(model.GateCommit.Before(), gate) {
		if err := requireUnmoved(ctx, rd, record); err != nil {
			return err
		}
	}
	switch gate {
	case model.GateDoR:
		return requireCriteria(s, id)
	case model.GateTestsFrozen:
		return requireFreeze(ctx, rd, id, record)
	case model.GatePlan, model.GateImplementation, model.GateVerification:
		// The freeze is only worth anything if it is still intact every time
		// the loop moves. Checking once at Gate 3 would let it be lifted in
		// silence the moment the implementation got difficult.
		return rd.requireIntactFreeze(ctx, id, record)
	case model.GateCommit:
		if err := rd.requireIntactFreeze(ctx, id, record); err != nil {
			return err
		}
		if err := requireFreshReviews(ctx, rd, id, record); err != nil {
			return err
		}
		if err := requireApproval(ctx, rd, id, record); err != nil {
			return err
		}
		return requireCommitted(ctx, s)
	default:
		return nil
	}
}

// headCommit is the commit HEAD is on, for a record to name. A repository with
// no commits yet has none, and git's all-zero name for that is not a commit, so
// it comes back as "".
func headCommit(ctx context.Context, rd *reading) (string, error) {
	head, err := rd.headOf(ctx)
	if err != nil || head == gitx.NoCommits {
		return "", err
	}
	return head, nil
}

// requireUnmoved holds the gates before the commit to a trunk the story has not
// moved. The hook refuses the ways to commit that it can read, and git has more:
// an alias in the user's own configuration, a commit made in a clone and
// fetched. Work committed that way was on trunk with no gate passed, and every
// gate after it measured the change against a HEAD that already held it, so the
// reviewers were shown a diff without it.
func requireUnmoved(ctx context.Context, rd *reading, record *model.Record) error {
	if record.Base == "" {
		return nil
	}
	head, err := rd.headOf(ctx)
	if err != nil {
		return err
	}
	if head == record.Base {
		return nil
	}
	return sdlcerr.New(sdlcerr.TrunkMoved,
		fmt.Sprintf("HEAD has moved since the story started, from %s to %s",
			record.Base[:min(len(record.Base), 12)], head[:min(len(head), 12)]),
		"the story's work is committed at the commit gate, so a commit before it reached trunk "+
			"with no gate passed, and the reviewers read the change against a HEAD that holds it")
}

// requireOrder keeps the loop a loop.
//
// Gates are not a checklist to tick off in any order: each one is done by
// somebody who could only do it because the one before it happened. Recording
// a pass out of order is how a story arrives at the commit gate having skipped
// the one that would have stopped it.
func requireOrder(gate model.Gate, record *model.Record) error {
	var missing []string
	for _, earlier := range gate.Before() {
		if !record.Pass(earlier) {
			missing = append(missing, string(earlier))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return sdlcerr.New(sdlcerr.GateOutOfOrder,
		string(gate)+" comes after "+strings.Join(missing, ", "),
		"each gate is done by somebody who could only do it because the one "+
			"before it happened, so "+plural(len(missing), "that gate has", "those gates have")+
			" to be recorded first")
}

// requireApproval holds a story whose risk tier the project pauses for a person
// until that person has approved the work being committed.
//
// It comes before requireCommitted because the loop asks before committing: a
// story that was never handed over is told that first, not that the tree is
// dirty.
func requireApproval(ctx context.Context, rd *reading, id string, record *model.Record) error {
	tier, pauses, err := rd.s.CommitPause(id)
	if err != nil || !pauses {
		return err
	}
	tree, err := rd.subject(ctx)
	if err != nil {
		return err
	}
	why := record.WaitsForApproval(tier, tree)
	if why == "" {
		return nil
	}
	return sdlcerr.New(sdlcerr.ApprovalRequired,
		quote(id)+" has not been approved by a person for committing", why)
}

// requireIntactFreeze is the freeze, checked again, at a gate: the story's test
// files are judged at HEAD, and what was accepted as trunk's own is kept for
// the gate to say and record.
func (r *reading) requireIntactFreeze(ctx context.Context, id string, record *model.Record) error {
	h, err := requireIntactFreeze(ctx, r.s, id, r.freezeAt(ctx, id, record))
	r.trunkTests, r.judged = h.accepted, err == nil
	return err
}

// freezeAt is where a gate judges the story's test files: at HEAD, which the
// command has asked for already or asks for once here, and only where a test
// file could be trunk's own.
func (r *reading) freezeAt(ctx context.Context, id string, record *model.Record) freezeAt {
	at := freezeAt{cut: record.Cut, forks: forksOf(record)}
	if at.cut != "" && inItsWorktree(r.s, id) {
		at.tip, _ = r.headOf(ctx)
	}
	return at
}

// requireIntactFreeze is the freeze, checked again. It returns the freeze as
// it was judged, with the test files accepted as trunk's own.
func requireIntactFreeze(ctx context.Context, s *store.Store, id string, at freezeAt) (heldState, error) {
	lock, err := s.Lock()
	if err != nil {
		return heldState{}, err
	}
	if lock == nil || lock.Story != id {
		return heldState{}, sdlcerr.New(sdlcerr.NotFrozen,
			"the acceptance tests are not frozen for this story",
			"every gate from here on is measured against them, and a freeze that "+
				"is not there cannot say whether they changed")
	}
	return requireHeldTests(ctx, s, id, lock, at)
}

// requireHeldTests refuses a freeze that no longer describes the tests: a
// frozen file that changed or went, or a test file it never held.
//
// A test added after the freeze was checked by nothing. The hook refuses one
// written through an agent's tools, but one that arrived any other way -- or
// one the test author added with freeze.allow_new_test_files on and nobody
// froze -- ran at verification as though it had been written before the code,
// and could have been written to pass.
//
// In a story's own worktree a test file that only trunk changed since the
// story was cut is neither: heldTests says which those are, and they come back
// for the caller to say and record.
//
// Where the story's list of its own commits is why a file is refused, the
// refusal says so and sorts the files by what a person does about each
// (listed.explain), with the way to freeze again that a gate has.
func requireHeldTests(ctx context.Context, s *store.Store, id string, lock *model.Lock, at freezeAt) (heldState, error) {
	h, err := heldTests(ctx, s, id, lock, at)
	const route = `"sdlc unfreeze --reason ..." in their own terminal, which sends the story back to Gate 3, where ` +
		`"sdlc freeze" takes the test files as they are then`
	switch {
	case len(h.broken) > 0:
		return h, h.list.explain(s, id, brokenFreeze(h.list.here(h.broken)), route)
	case err != nil:
		return h, err
	case len(h.unheld)+len(h.taken) == 0:
		return h, nil
	}
	var why []string
	if unheld := h.list.here(h.unheld); len(unheld) > 0 {
		why = append(why, strings.Join(unheld, ", ")+" appeared after the freeze was taken, so nothing says "+
			"it was written before the code")
	}
	if len(h.taken) > 0 {
		why = append(why, takenWhy(trunkName(s), h.taken))
	}
	refusal := sdlcerr.New(sdlcerr.UnfrozenTests,
		"there are test files the freeze does not hold", strings.Join(why, "; "))
	switch {
	case h.list.is():
		return h, h.list.explain(s, id, refusal, route)
	case len(h.taken) > 0:
		return h, refusal.WithFix(takenFix(s, id, h.taken[0]) + `; or they reopen the story to change the tests: ` +
			`"sdlc unfreeze --reason ..." is theirs alone`)
	case s.Config().Freeze.AllowNewTestFiles:
		return h, refusal.WithFix(`run "sdlc freeze" again -- freeze.allow_new_test_files is on, ` +
			"so it adds them to the freeze")
	}
	return h, refusal
}

// requireFreshReviews re-asks the reviewed gates about the tree as it is now.
//
// An approval is stamped with what was in front of it, and that is checked
// when the gate it belongs to is recorded -- but nothing was checking it
// again afterwards. Between `code_review pass` and `commit pass` the code
// could be changed freely: the freeze covers test files, and the commit gate
// asked only that the tree be committed, not that it be the tree anybody
// reviewed. Adding a function to a source file after the review and
// committing it went through without a word.
//
// The subject for both review gates is the whole tree, and computing it stages
// the working tree into a temporary index, so it does not change when the work
// is committed. A story that goes straight from review to commit passes here
// untouched; one that was edited in between does not.
func requireFreshReviews(ctx context.Context, rd *reading, id string, record *model.Record) error {
	var stale, why []string
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		err := requireReviews(ctx, rd, id, gate, record)
		if err == nil {
			continue
		}
		var known *sdlcerr.Error
		if !errors.As(err, &known) {
			return err
		}
		stale = append(stale, string(gate))
		why = append(why, known.Why)
	}
	if len(stale) == 0 {
		return nil
	}
	// Both gates, not the first one found: a change to the tree makes every
	// review of it stale at once, and being sent back twice in a row is worse
	// than being told the whole of it.
	return sdlcerr.New(sdlcerr.ReviewsMissing,
		"the code being committed is not the code that was reviewed",
		strings.Join(why, "; ")).
		WithFix("send it back to " + strings.Join(stale, " and ") +
			": what changed after they approved has been reviewed by nobody")
}

// requireCommitted holds the commit gate to its own name.
func requireCommitted(ctx context.Context, s *store.Store) error {
	clean, err := gitx.Clean(ctx, s.Root())
	if err != nil {
		return err
	}
	if !clean {
		why := "the gate records that this story reached trunk, and a working tree with changes in it has not"
		if id, ok := s.StoryWorktreeOf(); ok {
			// In its worktree the story is committed on its branch, and trunk
			// does not move until the story lands.
			why = "the gate records that the story is committed on " + store.StoryBranch(id) +
				", and a working tree with changes in it is not"
		}
		return sdlcerr.New(sdlcerr.TreeNotCommitted, "the commit gate cannot pass while there is uncommitted work", why)
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// requireFreeze holds the test gate to the thing it exists for.
func requireFreeze(ctx context.Context, rd *reading, id string, record *model.Record) error {
	s := rd.s
	lock, err := s.Lock()
	if err != nil {
		return err
	}
	switch {
	case lock == nil:
		return sdlcerr.New(sdlcerr.NotFrozen,
			"the test gate cannot pass until the tests are frozen",
			"nothing has been recorded, so a later edit to an acceptance test would "+
				"leave no trace, and every gate after this one assumes it would")
	case lock.Story != id:
		return sdlcerr.New(sdlcerr.NotFrozen,
			"the freeze on disk belongs to "+lock.Story+", not "+id,
			"a freeze covers one story's acceptance tests, and this one was taken "+
				"for a different story")
	}
	h, err := requireHeldTests(ctx, s, id, lock, rd.freezeAt(ctx, id, record))
	rd.trunkTests, rd.judged = h.accepted, err == nil
	return err
}

// requireDocuments refuses to record a pass for a gate whose documents are not
// on disk.
//
// The gates after this one read the documents, not the summary that said they
// exist. Checking here rather than asking the assistant to check is the whole
// argument of this tool: a gate nobody verifies is a gate that will eventually
// be passed on a story where the work did not happen.
//
// Only a pass is held to this. A gate can fail precisely because its work could
// not be done, and refusing to record that would leave the loop with no way to
// say so.
func requireDocuments(s *store.Store, id string, gate model.Gate) error {
	var missing []string
	for _, a := range model.ArtifactsFor(gate) {
		if !s.ArtifactStored(id, a) {
			missing = append(missing, a.Name+" ("+store.ArtifactPath(id, a)+")")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return sdlcerr.New(sdlcerr.GateDocumentsMissing,
		string(gate)+" cannot pass until its documents are stored",
		"the gates after this one read "+strings.Join(missing, " and ")+
			", and nothing is there")
}

// requireCriteria refuses to pass Gate 1 for a story with nothing to test.
//
// Reading the criteria as a test author would is the conversation's job, but a
// story with none gives it nothing to read: Gate 3 would freeze tests for no
// behaviour, and every gate after it would check the work against an empty
// spec. A criterion with no text is no criterion.
func requireCriteria(s *store.Store, id string) error {
	story, _, err := s.Story(id)
	if err != nil {
		return err
	}
	for _, ac := range story.AcceptanceCriteria {
		if strings.TrimSpace(ac.Text) != "" {
			return nil
		}
	}
	what := "dor cannot pass for " + quote(id) + ", which has no acceptance criteria"
	if len(story.AcceptanceCriteria) > 0 {
		// Criteria written in some other shape -- given/when/then, say -- are
		// there to a person reading the file and empty to the loop, which reads
		// only "text". Saying "none" would send that person looking for nothing.
		what = "dor cannot pass for " + quote(id) + ": none of its acceptance criteria has a \"text\""
	}
	return sdlcerr.New(sdlcerr.NoAcceptanceCriteria, what,
		"Gate 3 writes and freezes the tests from them, and with none there is nothing "+
			"to test and nothing to verify the work against")
}
