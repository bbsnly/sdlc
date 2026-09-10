package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/scaffold"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// initPayload is what a skill reads after running init, so that it can tell the
// user what happened without parsing prose.
type initPayload struct {
	OK       bool     `json:"ok"`
	Root     string   `json:"root"`
	Stack    string   `json:"stack,omitempty"`
	AlsoSeen []string `json:"also_seen,omitempty"`
	Created  []string `json:"created"`
	Kept     []string `json:"kept,omitempty"`
}

func newInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set this repository up to run the loop",
		Long: "init writes the four files a project needs to take part in the loop:\n" +
			"its configuration, the story schema, a starter backlog, and the contract\n" +
			"section the assistant reads from CLAUDE.md.\n\n" +
			"It reads the repository first and fills in commands that match it, so the\n" +
			"configuration works before you edit it. It never overwrites your backlog or\n" +
			"a CLAUDE.md you have already filled in, and it writes no .gitignore: what a\n" +
			"project commits is the project's decision.",
		Example: "  sdlc init\n  sdlc init --force   # restore the default settings, keeping your stories",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return sdlcerr.New(sdlcerr.NotAGitRepo,
					"the current directory could not be read",
					"sdlc sets up the repository you run it in").WithCause(err)
			}
			root, err := config.FindRoot(wd)
			if err != nil {
				return err
			}
			res, err := scaffold.Init(root, force)
			if err != nil {
				return err
			}
			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), initPayload{
					OK: true, Root: res.Root, Stack: res.Stack,
					AlsoSeen: res.AlsoSeen, Created: res.Created, Kept: res.Kept,
				})
			}
			printInit(cmd.OutOrStdout(), res)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false,
		"restore the default settings over an existing setup, keeping your stories")
	return cmd
}

func printInit(w io.Writer, res *scaffold.Result) {
	fmt.Fprintf(w, "Set up sdlc in %s\n\n", res.Root)
	for _, p := range res.Created {
		fmt.Fprintf(w, "  created  %s\n", p)
	}
	for _, p := range res.Kept {
		fmt.Fprintf(w, "  kept     %s\n", p)
	}

	fmt.Fprintln(w)
	switch {
	case res.Stack == "":
		fmt.Fprintf(w, "No stack was recognised, so the commands in %s are empty.\n", config.File)
		fmt.Fprintf(w, "sdlc looks for %s.\n", joinWords(scaffold.StackNames()))
	case len(res.AlsoSeen) > 0:
		fmt.Fprintf(w, "Guessed the commands from a %s project. This repository also has %s in it,\n",
			res.Stack, joinWords(res.AlsoSeen))
		fmt.Fprintf(w, "so check %s before you rely on it.\n", config.File)
	default:
		fmt.Fprintf(w, "Guessed the commands from a %s project.\n", res.Stack)
	}

	fmt.Fprint(w, "\nNext:\n"+
		"  1. sdlc doctor              check the guesses\n"+
		"  2. edit user_stories.json   replace the example with a story of your own\n"+
		"  3. /sdlc:next               in Claude Code, to work the first story\n")
}

// joinWords renders a list the way a sentence needs it.
func joinWords(items []string) string {
	switch len(items) {
	case 0:
		return "nothing"
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}
