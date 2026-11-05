package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

func newTeamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Report what happened to an agent of the team",
		Long: "team records what the session saw of an agent's run. The team itself is set in\n" +
			"the team section of .sdlc/config.json, which a person edits.",
		Example: `  sdlc team failure researcher --reason "it returned nothing"`,
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newTeamFailureCmd())
	return cmd
}

func newTeamFailureCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "failure AGENT",
		Short: "Record that an agent's run failed, and hand the story over past team.retries",
		Long: "failure records that a run of the agent failed at the gate the story is at:\n" +
			"it returned nothing usable, or broke off. The session runs it, and runs the\n" +
			"agent again while team.retries allows; the failure after that hands the story\n" +
			"to a person, who reads the reasons and decides. The count is the agent's at\n" +
			"that gate, since a person last decided on the story.\n\n" +
			"It records nothing else: no gate, review, staffing or freeze changes.",
		Example: `  sdlc team failure researcher --reason "it returned nothing"`,
		// The count is checked below, so a missing agent is refused with a
		// code and a fix rather than cobra's bare line.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runTeamFailure(cmd, args, reason) },
	}
	cmd.Flags().StringVar(&reason, "reason", "", "what went wrong with the run, in a sentence")
	return cmd
}

type teamFailurePayload struct {
	OK         bool   `json:"ok"`
	Story      string `json:"story"`
	Agent      string `json:"agent"`
	Gate       string `json:"gate"`
	Failures   int    `json:"failures"`
	HandedOver string `json:"handed_over,omitempty"`
}

func runTeamFailure(cmd *cobra.Command, args []string, reason string) error {
	if len(args) != 1 {
		return usage(cmd, "name one agent: `sdlc team failure AGENT --reason TEXT`")
	}
	if err := requireReason(reason, "a failure"); err != nil {
		return err
	}
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	id, err := activeStory(s, "a failure is counted on the story being worked on")
	if err != nil {
		return err
	}
	if err := refuseIfHeldElsewhere(s, id, "a failure is counted on the work in that checkout, not this one"); err != nil {
		return err
	}
	team := s.Config().Team
	// One spelling, so one budget whatever the session typed.
	agent := staffingName(args[0])
	if !team.Staffable(agent) {
		return notStaffable(team, agent)
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	gate, remaining := record.NextGate()
	if !remaining {
		gate = model.Gates[len(model.Gates)-1]
	}
	event := model.AgentFailureEvent(gate, agent)
	record.Events = append(record.Events, model.Event{
		At: model.Timestamp(s.Now()), Type: model.EventAgentFailure, Message: event, Reason: reason,
	})
	failures := record.SinceDecision(model.EventAgentFailure, event)
	// handOverAt reads a limit at or below nought as off; the clamp on
	// retries keeps this between 1 and 21, so retries 0 hands over at once.
	limit := team.RetriesAllowed() + 1
	message, err := failureMessage(s, record, id, agent, gate, failures)
	if err != nil {
		return err
	}
	handedOver, err := handOverAt(cmd, s, record, nil, limitCount{
		count: failures, limit: limit, kind: model.KindAgentFailed, message: message, what: "the failure",
	})
	if err != nil {
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), teamFailurePayload{
			OK: true, Story: id, Agent: agent, Gate: string(gate), Failures: failures, HandedOver: handedOver,
		})
	}
	w := cmd.OutOrStdout()
	// The limit is the failure that hands the story over, not a number of runs:
	// "1 of 3 runs" read as a run that failed out of three made.
	next := "; run it again"
	if handedOver != "" {
		next = ""
	}
	fmt.Fprintf(w, "%s  %s  %s failed: %s at this gate, and a person is asked at %d%s\n  %s\n",
		id, gate, agent, failureCount(failures), limit, next, quote(reason))
	if handedOver != "" {
		sayHandedOver(w, id, message)
	}
	return nil
}

// failureCount is a count of failures in words.
func failureCount(n int) string {
	return fmt.Sprintf("%d %s", n, plural(n, "failure", "failures"))
}

// failureMessage is what the person is told at the hand-over: the agent, the
// gate, the count, and every reason since the last decision, each quoted, since
// a model wrote it. That the answer does not answer the pre-commit question is
// said only where the story's tier waits for one.
func failureMessage(s *store.Store, record *model.Record, id, agent string, gate model.Gate, failures int) (string, error) {
	var reasons []string
	event := model.AgentFailureEvent(gate, agent)
	for i := len(record.Events) - 1; i >= 0 && record.Events[i].Type != model.EventApproval; i-- {
		if e := record.Events[i]; e.Type == model.EventAgentFailure && e.Message == event {
			reasons = append([]string{quote(e.Reason)}, reasons...)
		}
	}
	message := agent + " has failed " + strconv.Itoa(failures) + " times at " + string(gate) +
		", and team.retries allows " + strconv.Itoa(s.Config().Team.RetriesAllowed()) +
		" retries. The reasons: " + strings.Join(reasons, "; ") + ". Approving lets the loop run it again"
	_, pauses, err := s.CommitPause(id)
	if err != nil {
		return "", err
	}
	if pauses {
		message += "; it does not answer the pre-commit question, which is still asked before the commit"
	}
	return message + ".", nil
}

// failuresSince is each name's failures at the gate since a person last
// decided, for `sdlc status`, in the order each first failed.
func failuresSince(record *model.Record, gate model.Gate) (names []string, counts map[string]int) {
	counts = map[string]int{}
	prefix := string(gate) + " "
	for i := len(record.Events) - 1; i >= 0 && record.Events[i].Type != model.EventApproval; i-- {
		e := record.Events[i]
		if e.Type != model.EventAgentFailure || !strings.HasPrefix(e.Message, prefix) {
			continue
		}
		name := strings.TrimPrefix(e.Message, prefix)
		if counts[name] == 0 {
			names = append([]string{name}, names...)
		}
		counts[name]++
	}
	return names, counts
}
