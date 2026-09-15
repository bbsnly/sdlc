package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
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
			if err := checkTrunkForNewWork(cmd, asked); err != nil {
				return err
			}
			s, _, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()

			active, err := s.Active()
			if err != nil {
				return err
			}
			if active != "" {
				if asked != "" && asked != active {
					return sdlcerr.New(sdlcerr.IterationAlreadyActive,
						"an iteration is already running on "+quote(active),
						"the loop works one story at a time, which is what keeps a diff small "+
							"enough to review honestly")
				}
				finished, err := refuseIfFinished(s, active)
				if err != nil {
					return err
				}
				if finished != nil {
					return finished.WithFix(`run "sdlc stop" to end the iteration -- ` +
						"the story is done, and there is nothing left to resume")
				}
				return reportStart(cmd, s, active, true)
			}

			id := asked
			if id == "" {
				if id, err = nextRunnable(s); err != nil {
					return err
				}
			}

			story, backlog, err := s.Story(id)
			if err != nil {
				return err
			}
			finished, err := refuseIfFinished(s, id)
			if err != nil {
				return err
			}
			if finished != nil {
				return finished
			}
			if err := refuseIfWaiting(s, id); err != nil {
				return err
			}
			if !resuming(story.Status) {
				if err := refuseIfNotRunnable(backlog, story); err != nil {
					return err
				}
			}
			// A story that is already in progress is being picked up again,
			// whether or not the iteration that started it is still running.
			resume := resuming(story.Status)
			// Picking a story up again is the no-op the help promises it is:
			// SetStoryStatus writes nothing when the status is already the one
			// asked for, which keeps a resume out of the tree the reviewers
			// were stamped against.
			if err := s.SetStoryStatus(id, model.StatusInProgress); err != nil {
				return err
			}
			if err := s.SetActive(id); err != nil {
				return err
			}

			record, err := s.Record(id)
			if err != nil {
				return err
			}
			// Where trunk is now is where this iteration's work starts from.
			// Outside a repository there is no trunk to hold it to.
			if head, err := gitx.Head(cmd.Context(), s.Root()); err == nil {
				record.Base = head
			}
			record.Append("loop_start", "iteration started", s.Now())
			if err := s.SaveRecord(record); err != nil {
				return err
			}
			return reportStart(cmd, s, id, resume)
		},
	}
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
		return nil, nil
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
func nextRunnable(s *store.Store) (string, error) {
	backlog, err := s.Backlog()
	if err != nil {
		return "", err
	}
	if sel, ok := backlog.Next(); ok {
		return sel.Story.ID, nil
	}
	return "", sdlcerr.New(sdlcerr.NoRunnableStory,
		"there is no story to start",
		describeWhyNothingRuns(backlog))
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
func nothingToStart(b *model.Backlog, path string) string {
	if len(b.Stories) == 0 {
		return "\nThe backlog is empty. " + writeAStory(path)
	}
	said := "\nNothing to start: " + describeWhyNothingRuns(b) + ".\n"
	if nothingLeftToStart(b) {
		return said + writeAStory(path)
	}
	return said + "`sdlc story list` shows what is holding each story back.\n"
}

func reportStart(cmd *cobra.Command, s *store.Store, id string, resume bool) error {
	story, _, err := s.Story(id)
	if err != nil {
		return err
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	if wantJSON(cmd) {
		payload := startPayload{OK: true, Story: id, Title: story.Title, Resume: resume}
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
	fmt.Fprintf(w, "%s %s  %s\n\n", verb, id, story.Title)
	printGates(w, record)
	fmt.Fprint(w, "\nNext: run /sdlc:next in Claude Code to work the story.\n")
	return nil
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
		Short: "End the current iteration without recording a result",
		Long: "stop ends the iteration. It records nothing about the story and changes no\n" +
			"gate: the work stays exactly where it was, and starting again picks it up.\n\n" +
			"Stopping when nothing is running is not an error.",
		Example: "  sdlc stop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, _, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			active, err := s.Active()
			var unusable *sdlcerr.Error
			switch {
			case errors.As(err, &unusable) &&
				(unusable.Code == sdlcerr.UnsafeStoryID || unusable.Code == sdlcerr.StateUnreadable):
				// While the file names no story the hook enforces nothing, and
				// doctor sends you here. Stop is the way out of a broken state,
				// so it ends the iteration rather than refuse over the file.
				fmt.Fprintln(cmd.ErrOrStderr(), "sdlc: .sdlc/state/active does not name a story, "+
					"so this stop removes it without writing to any record.")
				active = ""
			case err != nil:
				return err
			}
			done := false
			if active != "" {
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
				case err != nil:
					return err
				default:
					record.Append("loop_end", "iteration ended", s.Now())
					if err := s.SaveRecord(record); err != nil {
						return err
					}
					// The same function that owns the invariant, so that the
					// sentence stop prints and the backlog cannot disagree.
					if done, err = settleStory(s, active, record); err != nil {
						// A story the backlog has lost must not trap the
						// iteration: stop is the way out of a broken state.
						var known *sdlcerr.Error
						if !errors.As(err, &known) || known.Code != sdlcerr.StoryNotFound {
							return err
						}
					}
					if done {
						if err := releaseFreeze(s, active); err != nil {
							return err
						}
					}
				}
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
			default:
				fmt.Fprintf(cmd.OutOrStdout(),
					"Stopped the iteration on %s. Nothing was recorded; `sdlc start` picks it up again.\n",
					active)
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------- gate

type gatePayload struct {
	OK     bool   `json:"ok"`
	Story  string `json:"story"`
	Gate   string `json:"gate"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
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
	case !remaining && story.Status != model.StatusDone:
		return true, s.SetStoryStatus(id, model.StatusDone)
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
	return appendEvent(s, id, "unfreeze", "the story is finished, so its freeze is lifted")
}

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
			gate := model.Gate(args[0])
			if !gate.Valid() {
				return sdlcerr.New(sdlcerr.UnknownGate,
					"there is no gate called "+quote(args[0]),
					"this version knows "+model.GateNames())
			}
			status := model.GateStatus(args[1])
			if !status.Valid() {
				return sdlcerr.New(sdlcerr.UnknownGateStatus,
					quote(args[1])+" is not a gate outcome",
					"a gate either passed, failed, or has not been reached yet")
			}

			s, _, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			id := storyID
			if id == "" {
				if id, err = activeStory(s, "gate records an outcome for the story being worked on"); err != nil {
					return err
				}
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
			// A passed code review is where the story's own commit is allowed,
			// so HEAD moved since then is that commit, and a gate reopened on it
			// measures from there. Held to where the story started, a code review
			// reopened on work already committed could never pass again.
			if record.Pass(model.GateCodeReview) {
				if head, err := gitx.Head(cmd.Context(), s.Root()); err == nil {
					record.Base = head
				}
			}
			if status == model.GatePass {
				if err := requireEvidence(cmd.Context(), s, id, gate, record); err != nil {
					return err
				}
			}
			if gate == model.GateAnalysis && cmd.Flags().Changed(securityFlag) {
				record.SetSecuritySensitive(securitySensitive)
			}
			record.SetGate(gate, status, note, s.Now())
			record.Append(model.EventGate, model.GateEvent(gate, status), s.Now())
			if err := s.SaveRecord(record); err != nil {
				return err
			}
			done, err := settleStory(s, id, record)
			if err != nil {
				return err
			}
			handedOver, message := "", ""
			if status == model.GateFail {
				limit := s.Config().Loop.MaxReworkRounds
				failures := record.SinceDecision(model.EventGate, model.GateEvent(gate, model.GateFail))
				message = fmt.Sprintf("%s has failed %d times, and loop.max_rework_rounds is %d, "+
					"so another attempt would meet the same wall. The last note: %s",
					gate, failures, limit, lastNote(note))
				if handedOver, err = handOverAt(cmd, s, id, failures, limit, "gate_failing", message); err != nil {
					return err
				}
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), gatePayload{
					OK: true, Story: id, Gate: string(gate), Status: string(status), Note: note, Done: done,
					HandedOver: handedOver,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", id, gate, status)
			if note != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", note)
			}
			if done {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s is done: every gate has passed.\n", id)
			}
			if handedOver != "" {
				sayHandedOver(cmd.OutOrStdout(), id, message)
			}
			return nil
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

// ---------------------------------------------------------------- shared

func printGates(w io.Writer, record *model.Record) {
	recorded := 0
	for _, g := range model.Gates {
		result, ok := record.Gates[g]
		if !ok {
			continue
		}
		recorded++
		line := fmt.Sprintf("  %-16s %s", g, result.Status)
		if result.Note != "" {
			line += "  " + result.Note
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
func requireEvidence(ctx context.Context, s *store.Store, id string, gate model.Gate,
	record *model.Record,
) error {
	if err := requireOrder(gate, record); err != nil {
		return err
	}
	if err := requireDocuments(s, id, gate); err != nil {
		return err
	}
	if err := requireReviews(ctx, s, id, gate, record); err != nil {
		return err
	}
	if slices.Contains(model.GateCommit.Before(), gate) {
		if err := requireUnmoved(ctx, s, record); err != nil {
			return err
		}
	}
	switch gate {
	case model.GateDoR:
		return requireCriteria(s, id)
	case model.GateTestsFrozen:
		return requireFreeze(ctx, s, id)
	case model.GatePlan, model.GateImplementation, model.GateVerification:
		// The freeze is only worth anything if it is still intact every time
		// the loop moves. Checking once at Gate 3 would let it be lifted in
		// silence the moment the implementation got difficult.
		if err := requireIntactFreeze(ctx, s, id); err != nil {
			return err
		}
		if gate == model.GatePlan {
			return nil
		}
		return requireSize(ctx, s)
	case model.GateCodeReview:
		return requireSize(ctx, s)
	case model.GateCommit:
		if err := requireIntactFreeze(ctx, s, id); err != nil {
			return err
		}
		if err := requireFreshReviews(ctx, s, id, record); err != nil {
			return err
		}
		if err := requireApproval(ctx, s, id, record); err != nil {
			return err
		}
		return requireCommitted(ctx, s)
	default:
		return nil
	}
}

// requireSize holds a story to thresholds.diff_size_cap from the gate that
// produces the change to the last one that sees it uncommitted. The agents that
// size and plan a story read the cap, and nothing checked that the change which
// came out of them kept to it. Code review is the last gate that can: anything
// changed after it makes its reviews stale, and the commit is refused for that.
func requireSize(ctx context.Context, s *store.Store) error {
	limit := s.Config().Thresholds.DiffSizeCap
	if limit <= 0 {
		return nil
	}
	size, err := s.ChangeSize(ctx)
	if err != nil {
		return err
	}
	if size <= limit {
		return nil
	}
	return sdlcerr.New(sdlcerr.DiffTooLarge,
		fmt.Sprintf("the change is %d lines, and thresholds.diff_size_cap is %d", size, limit),
		"a change bigger than the project trusts one review to read is not made smaller "+
			"by reviewing it anyway; lines are counted as git diff --numstat counts them "+
			"against the last commit, tests included, .sdlc/ and the backlog not")
}

// requireUnmoved holds the gates before the commit to a trunk the story has not
// moved. The hook refuses the ways to commit that it can read, and git has more:
// an alias in the user's own configuration, a commit made in a clone and
// fetched. Work committed that way was on trunk with no gate passed, and every
// gate after it measured the change against a HEAD that already held it, so the
// size cap counted none of it and the reviewers were shown a diff without it.
func requireUnmoved(ctx context.Context, s *store.Store, record *model.Record) error {
	if record.Base == "" {
		return nil
	}
	head, err := gitx.Head(ctx, s.Root())
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
func requireApproval(ctx context.Context, s *store.Store, id string, record *model.Record) error {
	tier, pauses, err := s.CommitPause(id)
	if err != nil || !pauses {
		return err
	}
	tree, err := s.ReviewSubject(ctx)
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

// requireIntactFreeze is the freeze, checked again.
func requireIntactFreeze(ctx context.Context, s *store.Store, id string) error {
	lock, err := s.Lock()
	if err != nil {
		return err
	}
	if lock == nil || lock.Story != id {
		return sdlcerr.New(sdlcerr.NotFrozen,
			"the acceptance tests are not frozen for this story",
			"every gate from here on is measured against them, and a freeze that "+
				"is not there cannot say whether they changed")
	}
	return requireHeldTests(ctx, s, lock)
}

// requireHeldTests refuses a freeze that no longer describes the tests: a
// frozen file that changed or went, or a test file it never held.
//
// A test added after the freeze was checked by nothing. The hook refuses one
// written through an agent's tools, but one that arrived any other way -- or
// one the test author added with freeze.allow_new_test_files on and nobody
// froze -- ran at verification as though it had been written before the code,
// and could have been written to pass.
func requireHeldTests(ctx context.Context, s *store.Store, lock *model.Lock) error {
	if changed := verifyFreeze(s, lock); len(changed) > 0 {
		return brokenFreeze(changed)
	}
	unheld, err := unfrozenTests(ctx, s, lock)
	if err != nil {
		return err
	}
	if len(unheld) == 0 {
		return nil
	}
	refusal := sdlcerr.New(sdlcerr.UnfrozenTests,
		"there are test files the freeze does not hold",
		strings.Join(unheld, ", ")+" appeared after the freeze was taken, so nothing says "+
			"it was written before the code")
	if s.Config().Freeze.AllowNewTestFiles {
		return refusal.WithFix(`run "sdlc freeze" again -- freeze.allow_new_test_files is on, ` +
			"so it adds them to the freeze")
	}
	return refusal
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
func requireFreshReviews(ctx context.Context, s *store.Store, id string,
	record *model.Record,
) error {
	var stale, why []string
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		err := requireReviews(ctx, s, id, gate, record)
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
		return sdlcerr.New(sdlcerr.TreeNotCommitted,
			"the commit gate cannot pass while there is uncommitted work",
			"the gate records that this story reached trunk, and a working tree with "+
				"changes in it has not")
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
func requireFreeze(ctx context.Context, s *store.Store, id string) error {
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
	return requireHeldTests(ctx, s, lock)
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
