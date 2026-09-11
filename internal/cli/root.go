// Package cli builds the command tree.
//
// The tree is built inside New rather than in package-level variables on
// purpose: package-level construction runs on every invocation, including the
// hook fast path that exists to avoid exactly that cost.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/version"
)

// jsonFlag is the name of the machine-output flag. It is persistent, so that a
// skill can put it anywhere in the command line without having to know which
// subcommand it belongs to.
const jsonFlag = "json"

// New builds the root command with its subcommands attached.
func New(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
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
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().Bool(jsonFlag, false,
		"print one line of JSON instead of prose, for a script or a skill to read")
	root.AddCommand(
		newInitCmd(),
		newStatusCmd(),
		newStoryCmd(),
		newStartCmd(),
		newStopCmd(),
		newGateCmd(),
		newArtifactCmd(),
		newReviewCmd(),
		newFreezeCmd(),
		newUnfreezeCmd(),
		newDoctorCmd(),
		newVersionCmd(),
	)
	return root
}

// Execute builds and runs the tree, returning a process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := New(stdin, stdout, stderr)
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		return 0
	}
	// A command that has already said its piece just wants an exit code.
	var quiet quietExit
	if errors.As(err, &quiet) {
		return quiet.code
	}
	// The flag is read after the fact because cobra parses it during Execute,
	// and a failure before parsing still has to print something.
	if machine, _ := root.PersistentFlags().GetBool(jsonFlag); machine {
		_ = emitJSON(stdout, newErrorPayload(err))
		return 1
	}
	fmt.Fprint(stderr, sdlcerr.Render(err))
	return exitCode(err)
}

// quietExit ends the command with a code and no error text. doctor uses it:
// its report is the message, and "sdlc: " followed by nothing would be noise.
type quietExit struct{ code int }

func (q quietExit) Error() string { return "" }

// exitCode keeps 1 for "this did not work" and lets a command choose its own.
func exitCode(err error) int {
	var quiet quietExit
	if errors.As(err, &quiet) {
		return quiet.code
	}
	return 1
}

// errorPayload is the machine form of a failure. It carries the same three
// fields a person would read, so a skill can show them rather than inventing
// its own wording.
type errorPayload struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Why   string `json:"why,omitempty"`
	Fix   string `json:"fix,omitempty"`
	Code  string `json:"code,omitempty"`
	Docs  string `json:"docs,omitempty"`
}

func newErrorPayload(err error) errorPayload {
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		return errorPayload{Error: err.Error()}
	}
	return errorPayload{
		Error: e.What,
		Why:   e.Why,
		Fix:   e.Fix,
		Code:  e.Code.String(),
		Docs:  e.Code.URL(),
	}
}

// emitJSON writes one compact line. Stdout is a protocol surface: everything
// that reads it reads it a line at a time.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// wantJSON reports whether the caller asked for machine output.
func wantJSON(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool(jsonFlag)
	return v
}

// openStore resolves the project the command is being run in.
func openStore() (*store.Store, *config.Project, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, nil, sdlcerr.New(sdlcerr.NotAGitRepo,
			"the current directory could not be read",
			"sdlc works relative to where it is run").WithCause(err)
	}
	p, err := config.Open(wd)
	if err != nil {
		return nil, nil, err
	}
	return store.New(p), p, nil
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

// activeStory is the story the iteration is on. Every command that changes loop
// state needs it, and every one of them has to explain the same thing when
// there is none, so it is explained once here.
func activeStory(s *store.Store, what string) (string, error) {
	id, err := s.Active()
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", sdlcerr.New(sdlcerr.NoActiveIteration,
			"there is no story being worked on",
			what+", and no iteration is running")
	}
	return id, nil
}

// appendEvent adds one line to a story's record. The record is how a later gate
// finds out what happened without trusting a conversation it cannot see.
func appendEvent(s *store.Store, id, kind, message string) error {
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	record.Append(kind, message, s.Now())
	return s.SaveRecord(record)
}
