package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

type nextUp struct {
	Story string `json:"story"`
	Title string `json:"title"`
}

type statusPayload struct {
	OK     bool              `json:"ok"`
	Active string            `json:"active,omitempty"`
	Title  string            `json:"title,omitempty"`
	Gates  map[string]string `json:"gates,omitempty"`
	// NextGate is where a resumed loop picks up. A skill reads this rather
	// than working out the gate order for itself, which is the kind of
	// derivation that drifts from the tool that enforces it.
	NextGate string         `json:"next_gate,omitempty"`
	Backlog  map[string]int `json:"backlog"`
	Next     *nextUp        `json:"next,omitempty"`
	Freeze   *freezeState   `json:"freeze,omitempty"`
}

// freezeState is what the freeze looks like from outside: how many acceptance
// tests it covers, and whether they are still what was frozen. A skill reads
// this to know whether Gate 3 has actually happened.
type freezeState struct {
	Story   string   `json:"story"`
	At      string   `json:"at"`
	Files   int      `json:"files"`
	Intact  bool     `json:"intact"`
	Changed []string `json:"changed,omitempty"`
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what the loop is working on and where it has got to",
		Long: "status answers three questions: which story is being worked on, which\n" +
			"gates it has passed, and what is waiting in the backlog.\n\n" +
			"It changes nothing, so it is safe to run at any point.",
		Example: "  sdlc status\n  sdlc status --json",
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
			backlog, err := s.Backlog()
			if err != nil {
				return err
			}

			payload := statusPayload{OK: true, Active: active, Backlog: countStatuses(backlog)}
			var record *model.Record
			if active != "" {
				if story, ok := backlog.Find(active); ok {
					payload.Title = story.Title
				}
				if record, err = s.Record(active); err != nil {
					return err
				}
				payload.Gates = map[string]string{}
				for gate, result := range record.Gates {
					payload.Gates[string(gate)] = string(result.Status)
				}
				if next, ok := record.NextGate(); ok {
					payload.NextGate = string(next)
				}
				if payload.Freeze, err = describeFreeze(s, active); err != nil {
					return err
				}
			} else if sel, ok := backlog.Next(); ok {
				payload.Next = &nextUp{Story: sel.Story.ID, Title: sel.Story.Title}
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), payload)
			}

			// An active story with no gate left is finished but not yet put
			// down, and calling that "in progress" would invite a session to
			// carry on working something that has nothing left to work.
			finished := active != "" && payload.NextGate == ""

			w := cmd.OutOrStdout()
			if active == "" {
				fmt.Fprintln(w, "No iteration running.")
			} else {
				state := "in progress"
				if finished {
					state = "finished   "
				}
				fmt.Fprintf(w, "%s  %s  %s\n\n", active, state, payload.Title)
				printGates(w, record)
				printFreeze(w, payload.Freeze)
				if payload.NextGate != "" {
					fmt.Fprintf(w, "\n  next   %s\n", payload.NextGate)
				}
			}
			fmt.Fprintf(w, "\n  backlog  %s\n", describeCounts(payload.Backlog))
			switch {
			case finished:
				fmt.Fprint(w, "\nEvery gate has passed. Run `sdlc stop` to end the iteration.\n")
			case active != "":
				fmt.Fprint(w, "\nRun /sdlc:next in Claude Code to carry on, "+
					"or `sdlc stop` to put it down.\n")
			case payload.Next != nil:
				fmt.Fprintf(w, "  next up  %s  %s\n", payload.Next.Story, payload.Next.Title)
				fmt.Fprint(w, "\nRun `sdlc start` to begin.\n")
			default:
				fmt.Fprint(w, nothingToStart(backlog))
			}
			return nil
		},
	}
}

// describeFreeze reports the freeze as it applies to this story. A freeze taken
// for a different story is stale, and reporting it here would be worse than
// reporting nothing: a skill would read it as this story's tests being locked.
func describeFreeze(s *store.Store, story string) (*freezeState, error) {
	lock, err := s.Lock()
	if err != nil || lock == nil || lock.Story != story {
		return nil, err
	}
	changed := verifyFreeze(s, lock)
	return &freezeState{
		Story: lock.Story, At: lock.At, Files: len(lock.Files),
		Intact: len(changed) == 0, Changed: changed,
	}, nil
}

func printFreeze(w io.Writer, f *freezeState) {
	switch {
	case f == nil:
		fmt.Fprintf(w, "\n  tests  not frozen\n")
	case f.Intact:
		fmt.Fprintf(w, "\n  tests  %s frozen at %s\n", countFiles(f.Files), f.At)
	default:
		fmt.Fprintf(w, "\n  tests  %s frozen at %s, and %s changed since\n",
			countFiles(f.Files), f.At, strings.Join(f.Changed, ", "))
	}
}

func countStatuses(b *model.Backlog) map[string]int {
	counts := map[string]int{}
	for i := range b.Stories {
		counts[string(b.Stories[i].Status)]++
	}
	return counts
}

// describeCounts renders the backlog in the order a story moves through it, so
// the same backlog always reads the same way.
func describeCounts(counts map[string]int) string {
	var parts []string
	seen := map[string]bool{}
	for _, s := range model.Statuses {
		if n := counts[string(s)]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ReplaceAll(string(s), "_", " ")))
			seen[string(s)] = true
		}
	}
	// A status the backlog invented still gets counted rather than vanishing.
	var extra []string
	for name := range counts {
		if !seen[name] {
			extra = append(extra, fmt.Sprintf("%d %s", counts[name], name))
		}
	}
	sort.Strings(extra)
	parts = append(parts, extra...)

	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, " · ")
}
