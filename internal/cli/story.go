package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
)

type storyRow struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Priority  *int     `json:"priority,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
	Next      bool     `json:"next,omitempty"`
}

type storyListPayload struct {
	OK      bool       `json:"ok"`
	Stories []storyRow `json:"stories"`
}

func newStoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "story",
		Short:   "Work with the backlog",
		Example: "  sdlc story list",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newStoryListCmd())
	return cmd
}

func newStoryListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the backlog, and say what is holding each story back",
		Long: "list shows every story with its status and priority, marks the one that\n" +
			"`sdlc start` would pick, and names the unfinished dependencies of any story\n" +
			"that cannot start yet.",
		Example: "  sdlc story list\n  sdlc story list --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, _, err := openStore()
			if err != nil {
				return err
			}
			backlog, err := s.Backlog()
			if err != nil {
				return err
			}

			nextID := ""
			if sel, ok := backlog.Next(); ok {
				nextID = sel.Story.ID
			}

			rows := make([]storyRow, 0, len(backlog.Stories))
			for i := range backlog.Stories {
				story := &backlog.Stories[i]
				rows = append(rows, storyRow{
					ID:        story.ID,
					Title:     story.Title,
					Status:    string(story.Status),
					Priority:  story.Priority,
					BlockedBy: backlog.BlockedBy(story),
					Next:      story.ID == nextID,
				})
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), storyListPayload{OK: true, Stories: rows})
			}
			printStories(cmd, backlog, rows)
			return nil
		},
	}
}

func printStories(cmd *cobra.Command, backlog *model.Backlog, rows []storyRow) {
	w := cmd.OutOrStdout()
	if len(rows) == 0 {
		fmt.Fprint(w, "The backlog is empty.\n\nAdd a story to user_stories.json, "+
			"or ask Claude Code for one with /sdlc:story.\n")
		return
	}

	idWidth, statusWidth := 0, 0
	for _, r := range rows {
		idWidth = max(idWidth, len(r.ID))
		statusWidth = max(statusWidth, len(r.Status))
	}

	for _, r := range rows {
		marker := "  "
		if r.Next {
			marker = "> "
		}
		priority := " -"
		if r.Priority != nil {
			priority = fmt.Sprintf("%2s", strconv.Itoa(*r.Priority))
		}
		line := fmt.Sprintf("%s%-*s  %-*s  %s  %s",
			marker, idWidth, r.ID, statusWidth, r.Status, priority, r.Title)
		if len(r.BlockedBy) > 0 {
			line += "  (waiting on " + strings.Join(r.BlockedBy, ", ") + ")"
		}
		fmt.Fprintln(w, line)
	}

	for _, r := range rows {
		if r.Next {
			fmt.Fprintf(w, "\n> is what `sdlc start` would pick.\n")
			return
		}
	}
	// The table above already shows every status, so this says why without the
	// fault framing that "nothing is runnable" carries into a finished backlog.
	fmt.Fprintf(w, "\nNothing to start: %s.\n", describeWhyNothingRuns(backlog))
}
