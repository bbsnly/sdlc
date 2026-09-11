package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
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
			s, _, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			asked := ""
			if len(args) == 1 {
				asked = args[0]
			}

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
			// A story that is already in progress is being picked up again,
			// whether or not the iteration that started it is still running.
			resume := story.Status == model.StatusInProgress ||
				story.Status == model.StatusAwaitingHuman
			story.Status = model.StatusInProgress
			story.Updated = model.Timestamp(s.Now())
			if err := s.SaveBacklog(backlog); err != nil {
				return err
			}
			if err := s.SetActive(id); err != nil {
				return err
			}

			record, err := s.Record(id)
			if err != nil {
				return err
			}
			record.Append("loop_start", "iteration started", s.Now())
			if err := s.SaveRecord(record); err != nil {
				return err
			}
			return reportStart(cmd, s, id, resume)
		},
	}
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
	return sdlcerr.New(sdlcerr.StoryAlreadyFinished,
		quote(id)+" is finished",
		"every gate on it has passed, so starting it would put work that is "+
			"already done back in progress"), nil
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
const writeAStory = "Add a story to user_stories.json, " +
	"or ask Claude Code for one with /sdlc:story.\n"

// nothingToStart is what `sdlc status` says when no story can be started.
func nothingToStart(b *model.Backlog) string {
	if len(b.Stories) == 0 {
		return "\nThe backlog is empty. " + writeAStory
	}
	said := "\nNothing to start: " + describeWhyNothingRuns(b) + ".\n"
	if nothingLeftToStart(b) {
		return said + writeAStory
	}
	return said + "`sdlc story list` shows what is holding each story back.\n"
}

func reportStart(cmd *cobra.Command, s *store.Store, id string, resume bool) error {
	story, _, err := s.Story(id)
	if err != nil {
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), startPayload{
			OK: true, Story: id, Title: story.Title, Resume: resume,
		})
	}
	w := cmd.OutOrStdout()
	verb := "Started"
	if resume {
		verb = "Resumed"
	}
	fmt.Fprintf(w, "%s %s  %s\n\n", verb, id, story.Title)
	record, err := s.Record(id)
	if err != nil {
		return err
	}
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
			if err != nil {
				return err
			}
			done := false
			if active != "" {
				record, err := s.Record(active)
				if err != nil {
					return err
				}
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
func releaseFreeze(s *store.Store, id string) error {
	lock, err := s.Lock()
	if err != nil || lock == nil || lock.Story != id {
		return err
	}
	return s.ClearLock()
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
			if status == model.GatePass {
				if err := requireEvidence(cmd.Context(), s, id, gate, record); err != nil {
					return err
				}
			}
			if gate == model.GateAnalysis && cmd.Flags().Changed(securityFlag) {
				record.SetSecuritySensitive(securitySensitive)
			}
			record.SetGate(gate, status, note, s.Now())
			record.Append("gate", string(gate)+" "+string(status), s.Now())
			if err := s.SaveRecord(record); err != nil {
				return err
			}
			done, err := settleStory(s, id, record)
			if err != nil {
				return err
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), gatePayload{
					OK: true, Story: id, Gate: string(gate), Status: string(status), Note: note, Done: done,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", id, gate, status)
			if note != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", note)
			}
			if done {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s is done: every gate has passed.\n", id)
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

func quote(s string) string { return `"` + s + `"` }

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
	switch gate {
	case model.GateTestsFrozen:
		return requireFreeze(s, id)
	case model.GatePlan, model.GateImplementation, model.GateVerification:
		// The freeze is only worth anything if it is still intact every time
		// the loop moves. Checking once at Gate 3 would let it be lifted in
		// silence the moment the implementation got difficult.
		return requireIntactFreeze(s, id)
	case model.GateCommit:
		if err := requireIntactFreeze(s, id); err != nil {
			return err
		}
		return requireCommitted(ctx, s)
	default:
		return nil
	}
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

// requireIntactFreeze is the freeze, checked again.
func requireIntactFreeze(s *store.Store, id string) error {
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
	if changed := verifyFreeze(s, lock); len(changed) > 0 {
		return sdlcerr.New(sdlcerr.FreezeBroken,
			"the frozen tests are not what was frozen",
			strings.Join(changed, ", ")+" changed after the freeze was taken")
	}
	return nil
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
func requireFreeze(s *store.Store, id string) error {
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
	if changed := verifyFreeze(s, lock); len(changed) > 0 {
		return sdlcerr.New(sdlcerr.FreezeBroken,
			"the frozen tests are not what was frozen",
			strings.Join(changed, ", ")+" changed after the freeze was taken")
	}
	return nil
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
