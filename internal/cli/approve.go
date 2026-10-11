package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

type escalatePayload struct {
	OK      bool   `json:"ok"`
	Story   string `json:"story"`
	Type    string `json:"type"`
	Message string `json:"message"`
	Tree    string `json:"tree,omitempty"`
}

type approvePayload struct {
	OK       bool   `json:"ok"`
	Story    string `json:"story"`
	Decision string `json:"decision"`
	Type     string `json:"type"`
	Reason   string `json:"reason,omitempty"`
	Tree     string `json:"tree,omitempty"`
}

func newEscalateCmd() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "escalate <type>",
		Short: "Hand the story to a person, and end the iteration",
		Long: "escalate stops the loop to ask a person something it should not decide\n" +
			"for itself.\n\n" +
			"The type is a word for the kind of question -- pre_commit_approval,\n" +
			"spec_unclear, loop_stalled -- and --message is the question. It goes on the\n" +
			"story's record, bound to the work as it stands. The story becomes\n" +
			"awaiting_human and the iteration ends, so the session stops rather than\n" +
			"carrying on past the question. `sdlc start` refuses the story until a person\n" +
			"answers with `sdlc approve`.",
		Example: `  sdlc escalate pre_commit_approval --message "the migration has no down step"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := strings.TrimSpace(args[0])
			if kind == "" || strings.TrimSpace(message) == "" {
				return sdlcerr.New(sdlcerr.BadArgument,
					"an escalation needs a type and a --message",
					"the person it goes to reads the message to know what they are being asked")
			}
			s, _, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			id, err := activeStory(s, "escalate hands over the story being worked on")
			if err != nil {
				return err
			}
			tree, err := s.Escalate(cmd.Context(), id, kind, message)
			if err != nil {
				return err
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), escalatePayload{
					OK: true, Story: id, Type: kind, Message: message, Tree: tree,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  waiting for a person: %s\n  %s\n\n"+
				"The iteration has ended. A person reads the work and answers in their own terminal:\n"+
				"  sdlc approve %s\n  sdlc approve %s --reject \"why\"\n",
				id, oneLine(kind), oneLine(message), id, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "what the person is being asked to decide")
	return cmd
}

func newApproveCmd() *cobra.Command {
	var reject string
	cmd := &cobra.Command{
		Use:   "approve [story]",
		Short: "Answer an escalation: approve the work, or send it back",
		Long: "approve is a person's answer to `sdlc escalate`.\n\n" +
			"Run it in your own terminal. The hook refuses it from a tool call in any session\n" +
			"while a story waits for an answer or is under way, because an agent that could\n" +
			"answer would be approving its own work. The answer goes on the story's record\n" +
			"with the tree it was given for, the story goes back to in_progress, and\n" +
			"`sdlc start` picks it up again.\n\n" +
			"Without a story, it answers for the story being worked on.",
		Example: "  sdlc approve US-001\n" +
			`  sdlc approve US-001 --reject "the migration has no way back"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runApprove(cmd, args, reject) },
	}
	cmd.Flags().StringVar(&reject, "reject", "", "send the work back instead, saying why")
	return cmd
}

func runApprove(cmd *cobra.Command, args []string, reject string) error {
	rejected := cmd.Flags().Changed("reject")
	if rejected && strings.TrimSpace(reject) == "" {
		return sdlcerr.New(sdlcerr.BadArgument,
			"a rejection needs a reason",
			"the work goes back to whoever does it next, and they need to know what to change")
	}
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	id, err := approveTarget(s, args)
	if err != nil {
		return err
	}
	if err := refuseIfHeldElsewhere(s, id, "a person's answer is about the work in that checkout, not this one"); err != nil {
		return err
	}
	record, err := recordAwaitingAnswer(s, id)
	if err != nil {
		return err
	}
	tree, err := s.ReviewSubject(cmd.Context())
	if err != nil {
		return err
	}
	approval, _ := record.Decide(!rejected, reject, tree, s.Now())
	event := approval.Decision + " (" + approval.Type + ")"
	if rejected {
		event += ": " + reject
	}
	record.Append(model.EventApproval, event, s.Now())
	if err := s.SaveRecord(record); err != nil {
		return err
	}
	if err := s.SetStoryStatus(id, model.StatusInProgress); err != nil {
		return err
	}
	if releasedSinceStart(record) {
		noteRelease(cmd.ErrOrStderr(), s, id)
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), approvePayload{
			OK: true, Story: id, Decision: approval.Decision, Type: approval.Type,
			Reason: approval.Reason, Tree: tree,
		})
	}
	sayApproved(cmd.OutOrStdout(), s, record, id, event, rejected)
	return nil
}

// approveTarget is the story named, or the one being worked on.
func approveTarget(s *store.Store, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	return activeStory(s, "approve answers for the story named, or the one being worked on")
}

// recordAwaitingAnswer is the story's record, once it is known to hold an
// escalation nobody has answered.
func recordAwaitingAnswer(s *store.Store, id string) (*model.Record, error) {
	if _, _, err := s.Story(id); err != nil {
		return nil, err
	}
	record, err := s.Record(id)
	if err != nil {
		return nil, err
	}
	if _, ok := record.PendingEscalation(); !ok {
		return nil, sdlcerr.New(sdlcerr.NothingToApprove,
			"nothing on "+quote(id)+" is waiting for a decision",
			"no escalation is pending: a story waits for a person only after `sdlc escalate`")
	}
	return record, nil
}

// sayApproved prints what follows a person's answer: where the work carries on.
func sayApproved(w io.Writer, s *store.Store, record *model.Record, id, event string, rejected bool) {
	if _, remaining := record.NextGate(); !rejected && !remaining && inItsWorktree(s, id) {
		// Start refuses a finished story, and the stop here lands it.
		fmt.Fprintf(w, "%s  %s\n\n`sdlc stop` in %s lands it: run it in your own terminal, "+
			"or type /sdlc:next in Claude Code.\n", id, oneLine(event), worktreeIn(id))
		return
	}
	fmt.Fprintf(w, "%s  %s\n\nA person runs `sdlc start %s`, or types /sdlc:next in Claude Code, to carry on.\n",
		id, oneLine(event), id)
}

// handOverAt hands a story to a person when one of the loop's limits is reached:
// a gate that keeps failing, a reviewer that keeps blocking. Another attempt
// would meet the same wall, and a person deciding to carry on is what starts
// the count again. It returns the kind of hand-over, or "" when the limit has
// not been reached. A limit of zero is no limit.
//
// The record is the one the command has counted on and not yet saved, and this
// is the one place it is saved: below the limit as it always was, and at the
// limit together with the hand-over or not at all. Saved first, a failure whose
// hand-over git then refused was counted with nobody asked, and counted again
// each time the command was run. what names the thing being recorded, for the
// refusal.
//
// The caller holds the project's lock.
func handOverAt(cmd *cobra.Command, s *store.Store, record *model.Record, review *store.ReviewDocument,
	at limitCount,
) (string, error) {
	if at.limit <= 0 || at.count < at.limit {
		return "", saveRecorded(s, record, review)
	}
	_, err := s.HandOver(cmd.Context(), store.HandOver{
		Story: record.Story, Kind: at.kind, Message: at.message, Record: record, Review: review,
	})
	if err != nil {
		return "", notRecorded(err, at.what)
	}
	return at.kind, nil
}

// limitCount is how far a story has got towards one of the loop's limits, and
// the hand-over to make when it gets there.
type limitCount struct {
	count, limit int
	kind         string // the kind of hand-over the limit makes
	message      string // what the person is told
	what         string // the thing being recorded, for a refusal
}

// saveRecorded saves a record, after the reviewer's document it lists where
// there is one.
func saveRecorded(s *store.Store, record *model.Record, review *store.ReviewDocument) error {
	if review != nil {
		if _, err := s.WriteReview(record.Story, review.Gate, review.Role, review.Round, review.Content); err != nil {
			return err
		}
	}
	return s.SaveRecord(record)
}

// notRecorded ends the refusal of a hand-over at a limit by saying what the
// refusal means for the command: the failure it was recording is not on the
// record, so the command is run again and counts it once. It is said here and
// not in the store or the catalogue, where it would be said of refusals it is
// not true of, and not of a hand-over that could not be taken back.
func notRecorded(err error, what string) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) || store.PartlyHandedOver(err) {
		return err
	}
	out := *e
	why := strings.TrimSuffix(strings.TrimSpace(out.Why), ".")
	if why != "" {
		why += ". "
	}
	// what begins a sentence here and sits inside one in the fix.
	out.Why = why + strings.ToUpper(what[:1]) + what[1:] + " was not recorded, because recording it would reach the limit " +
		"at which the story is handed to a person, and the story could not be handed over"
	out.Fix = strings.TrimSuffix(strings.TrimSpace(out.Fix), ".") +
		"; then run the same command again, which is safe: " + what + " is counted once"
	return &out
}

// sayHandedOver is what a command prints after handOverAt handed a story over.
func sayHandedOver(w io.Writer, id, message string) {
	fmt.Fprintf(w, "\n%s is handed to a person: %s\n\n"+
		"A person reads the work and answers in their own terminal:\n"+
		"  sdlc approve %s\n  sdlc approve %s --reject \"why\"\n", id, oneLine(message), id, id)
}

// lastNote is a note for a message that quotes it.
func lastNote(note string) string {
	if strings.TrimSpace(note) == "" {
		return "no note was given"
	}
	return note
}

// refuseIfWaiting keeps a story that was handed to a person with that person
// until they answer. Starting it again would carry on past the question the
// loop stopped to ask.
func refuseIfWaiting(s *store.Store, id string) error {
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	pending, ok := record.PendingEscalation()
	if !ok {
		return nil
	}
	return sdlcerr.New(sdlcerr.AwaitingPerson,
		quote(id)+" is waiting for a person",
		"it was handed over at "+pending.At+" ("+pending.Type+"): "+pending.Message)
}
