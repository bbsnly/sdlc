package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
)

type nextUp struct {
	Story string `json:"story"`
	Title string `json:"title"`
}

type statusPayload struct {
	OK      bool              `json:"ok"`
	Active  string            `json:"active,omitempty"`
	Title   string            `json:"title,omitempty"`
	Gates   map[string]string `json:"gates,omitempty"`
	Backlog map[string]int    `json:"backlog"`
	Next    *nextUp           `json:"next,omitempty"`
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
			} else if sel, ok := backlog.Next(); ok {
				payload.Next = &nextUp{Story: sel.Story.ID, Title: sel.Story.Title}
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), payload)
			}

			w := cmd.OutOrStdout()
			if active == "" {
				fmt.Fprintln(w, "No iteration running.")
			} else {
				fmt.Fprintf(w, "%s  in progress  %s\n\n", active, payload.Title)
				printGates(w, record)
			}
			fmt.Fprintf(w, "\n  backlog  %s\n", describeCounts(payload.Backlog))
			switch {
			case active != "":
				fmt.Fprint(w, "\nRun /sdlc:next in Claude Code to carry on, "+
					"or `sdlc stop` to put it down.\n")
			case payload.Next != nil:
				fmt.Fprintf(w, "  next up  %s  %s\n", payload.Next.Story, payload.Next.Title)
				fmt.Fprint(w, "\nRun `sdlc start` to begin.\n")
			default:
				fmt.Fprint(w, "\nNothing is runnable. `sdlc story list` shows what is holding "+
					"each story back.\n")
			}
			return nil
		},
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
