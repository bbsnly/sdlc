// Package cli builds the command tree.
//
// The tree is built inside New rather than in package-level variables on
// purpose: package-level construction runs on every invocation, including the
// hook fast path that exists to avoid exactly that cost.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/version"
)

// New builds the root command with its subcommands attached.
func New(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "sdlc",
		Short: "Run a story-driven delivery loop with enforced gates",
		Long: "sdlc runs a story-driven delivery loop for Claude Code.\n\n" +
			"One story per session, nine gates. Acceptance tests are frozen before any\n" +
			"implementation code is written, and the agent that implements cannot edit them.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// A bare `sdlc` should teach, not error.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newVersionCmd(), newDoctorCmd())
	return root
}

// Execute builds and runs the tree, returning a process exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	root := New(stdout, stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(stderr, "sdlc: %v\n", err)
		return 1
	}
	return 0
}

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:     "version",
		Short:   "Print the version of sdlc you are running",
		Example: "  sdlc version\n  sdlc version --short",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Get()
			if short {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), info.Version)
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), info.String())
			return err
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the semantic version")
	return cmd
}
