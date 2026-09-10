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
			s, _, err := openStore()
			if err != nil {
				return err
			}
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
	desc := fmt.Sprintf("of %d stories, %d are done and %d are blocked",
		len(b.Stories), counts[model.StatusDone], counts[model.StatusBlocked])
	if waiting > 0 {
		desc += fmt.Sprintf(", and %d are waiting on a dependency that is not done", waiting)
	}
	return desc
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
			s, _, err := openStore()
			if err != nil {
				return err
			}
			active, err := s.Active()
			if err != nil {
				return err
			}
			if active != "" {
				record, err := s.Record(active)
				if err != nil {
					return err
				}
				record.Append("loop_end", "iteration ended", s.Now())
				if err := s.SaveRecord(record); err != nil {
					return err
				}
			}
			if err := s.ClearActive(); err != nil {
				return err
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), stopPayload{OK: true, Story: active, WasA: active != ""})
			}
			if active == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No iteration was running.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Stopped the iteration on %s. Nothing was recorded; `sdlc start` picks it up again.\n",
				active)
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
}

func newGateCmd() *cobra.Command {
	var note, storyID string
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

			s, _, err := openStore()
			if err != nil {
				return err
			}
			id := storyID
			if id == "" {
				if id, err = s.Active(); err != nil {
					return err
				}
			}
			if id == "" {
				return sdlcerr.New(sdlcerr.NoActiveIteration,
					"there is no story to record this against",
					"gate records an outcome for the story being worked on, and no "+
						"iteration is running")
			}

			if status == model.GatePass {
				if err := requireDocuments(s, id, gate); err != nil {
					return err
				}
			}

			record, err := s.Record(id)
			if err != nil {
				return err
			}
			record.SetGate(gate, status, note, s.Now())
			record.Append("gate", string(gate)+" "+string(status), s.Now())
			if err := s.SaveRecord(record); err != nil {
				return err
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), gatePayload{
					OK: true, Story: id, Gate: string(gate), Status: string(status), Note: note,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", id, gate, status)
			if note != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", note)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "why the gate came out this way, in one line")
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
