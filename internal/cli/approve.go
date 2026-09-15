package cli

import (
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
		RunE: func(cmd *cobra.Command, args []string) error {
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
			var id string
			if len(args) == 1 {
				id = args[0]
			} else if id, err = activeStory(s, "approve answers for the story named, or the one being worked on"); err != nil {
				return err
			}
			if _, _, err := s.Story(id); err != nil {
				return err
			}
			record, err := s.Record(id)
			if err != nil {
				return err
			}
			if _, ok := record.PendingEscalation(); !ok {
				return sdlcerr.New(sdlcerr.NothingToApprove,
					"nothing on "+quote(id)+" is waiting for a decision",
					"no escalation is pending: a story waits for a person only after `sdlc escalate`")
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

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), approvePayload{
					OK: true, Story: id, Decision: approval.Decision, Type: approval.Type,
					Reason: approval.Reason, Tree: tree,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n\nA person runs `sdlc start %s`, or types /sdlc:next in Claude Code, to carry on.\n",
				id, oneLine(event), id)
			return nil
		},
	}
	cmd.Flags().StringVar(&reject, "reject", "", "send the work back instead, saying why")
	return cmd
}

// handOverAt hands a story to a person when one of the loop's limits is reached:
// a gate that keeps failing, a reviewer that keeps blocking. Another attempt
// would meet the same wall, and a person deciding to carry on is what starts
// the count again. It returns the kind of hand-over, or "" when the limit has
// not been reached. A limit of zero is no limit.
//
// The caller holds the project's lock.
func handOverAt(cmd *cobra.Command, s *store.Store, id string, count, limit int, kind, message string) (string, error) {
	if limit <= 0 || count < limit {
		return "", nil
	}
	if _, err := s.Escalate(cmd.Context(), id, kind, message); err != nil {
		return "", err
	}
	return kind, nil
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
