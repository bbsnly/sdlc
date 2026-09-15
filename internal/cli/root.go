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
	"strings"

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
		newLogCmd(),
		newAckCmd(),
		newStartCmd(),
		newStopCmd(),
		newGateCmd(),
		newArtifactCmd(),
		newReviewCmd(),
		newFreezeCmd(),
		newUnfreezeCmd(),
		newEscalateCmd(),
		newApproveCmd(),
		newCostCmd(),
		newDoctorCmd(),
		newVersionCmd(),
	)
	explainUsageErrors(root)
	return root
}

// explainUsageErrors gives a command line that does not parse the same shape
// as every other failure: a code, what was wrong, and what to do. Cobra's own
// errors carried only the message, so `--json` came back with no code to act
// on, and prose said nothing about --help.
func explainUsageErrors(root *cobra.Command) {
	root.SetFlagErrorFunc(usageError) // inherited by every subcommand
	root.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usage(cmd, "unknown command "+quote(args[0])+" for "+quote(cmd.CommandPath()))
		}
		return nil
	}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if check := sub.Args; check != nil {
				sub.Args = func(cmd *cobra.Command, args []string) error {
					if err := check(cmd, args); err != nil {
						return usageError(cmd, err)
					}
					return nil
				}
			}
			walk(sub)
		}
	}
	walk(root)
}

func usageError(cmd *cobra.Command, err error) error {
	return usage(cmd, err.Error())
}

func usage(cmd *cobra.Command, what string) error {
	return sdlcerr.New(sdlcerr.BadArgument, what,
		"`"+cmd.CommandPath()+"` did not understand its command line").
		WithFix(`run "` + cmd.CommandPath() + ` --help" to see what it takes`)
}

// askedForJSON reports whether the command line asks for --json, for a failure
// that came before the flags were parsed. It stops where a `--` ends the flags.
func askedForJSON(args []string) bool {
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "--" + jsonFlag, "--" + jsonFlag + "=true":
			return true
		}
	}
	return false
}

// Execute builds and runs the tree, returning a process exit code.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// Errors go through controls too: a story id or a document name quoted in
	// one comes from the same places as a title.
	out, errs := &controls{w: stdout}, &controls{w: stderr}
	defer func() {
		_ = out.Flush()
		_ = errs.Flush()
	}()
	stdout, stderr = out, errs
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
	// and a failure before parsing still has to print something -- in JSON, if
	// the command line asked for it, whether or not it got as far as saying so.
	if machine, _ := root.PersistentFlags().GetBool(jsonFlag); machine || askedForJSON(args) {
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

// openStoreForWriting is openStore for a command that changes loop state: it
// also takes the project's lock, and returns the function that gives it back.
//
// Two sdlc processes reading, changing and writing the record at the same time
// is not hypothetical -- the runbook tells the session to run the reviewers in
// parallel, and each of them records a verdict. Without this, four of five
// concurrent `sdlc review add` calls reported success and were discarded.
//
// Reading commands do not take it. A write lands as one atomic file
// replacement, so a reader never sees half of one; the most it can see is a
// reading taken a moment before the write, which is what reading concurrently
// means anywhere.
func openStoreForWriting(cmd *cobra.Command) (*store.Store, *config.Project, func(), error) {
	nothing := func() {}
	s, p, err := openStore()
	if err != nil {
		return nil, nil, nothing, err
	}
	g, err := store.Lock(p.Root, strings.TrimPrefix(cmd.CommandPath(), "sdlc "))
	if err != nil {
		return nil, nil, nothing, err
	}
	// Under the lock, so that a start moving the story to another session cannot
	// come between the check and the write, and before anything is cleared, so
	// that a command refused here changes nothing in the project. `sdlc start` is
	// how a session takes a story over, and `sdlc ack` is about trunk, not a story.
	switch cmd.CommandPath() {
	case "sdlc start", "sdlc ack":
	default:
		if err := refuseAnotherSession(s); err != nil {
			g.Release()
			return nil, nil, nothing, err
		}
	}
	// Holding the lock, nothing else is part-way through a write, so what a
	// killed command left behind can be cleared before it blocks the commit gate.
	s.ClearInterruptedWrites()
	return s, p, g.Release, nil
}

// refuseAnotherSession keeps one Claude Code session's sdlc commands from
// writing into a story another session is working. The hook holds the session
// working the story and its agents, and nothing held any other: a model in
// another session ran `sdlc artifact write` or `sdlc gate` into a story it did
// not start. It is not a barrier against a model set on getting round it:
// `sdlc start` takes the story over, and `.sdlc/` can be written without sdlc.
//
// A command with no session set, or one that cannot be a session id, reads as
// a terminal, which is a person, as it does to `sdlc start`. A story with no
// session recorded has nothing here to keep.
func refuseAnotherSession(s *store.Store) error {
	here := os.Getenv(store.SessionEnv)
	if !store.CheckSession(here) {
		return nil
	}
	owner, held := s.WorkingSession()
	if !held || owner == here {
		return nil
	}
	// A story that cannot be read is said by the command that reads it.
	if active, err := s.Active(); err == nil && active != "" {
		return sdlcerr.New(sdlcerr.SessionElsewhere,
			quote(active)+" is being worked on in another Claude Code session",
			"this command runs in session "+quote(here)+", and .sdlc/state/session records "+
				quote(owner)+" as the session working the story, so what it changed would go into "+
				"a story this session did not start")
	}
	return nil
}

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:     "version",
		Short:   "Print the version of sdlc you are running",
		Example: "  sdlc version\n  sdlc version --short\n  sdlc version --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Get()
			switch {
			case wantJSON(cmd) && short:
				return emitJSON(cmd.OutOrStdout(), map[string]string{"version": info.Version})
			case wantJSON(cmd):
				return emitJSON(cmd.OutOrStdout(), info)
			case short:
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
