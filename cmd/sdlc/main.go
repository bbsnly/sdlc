// Command sdlc runs a story-driven delivery loop for Claude Code.
//
// See https://github.com/bbsnly/sdlc for what the loop is and how to use it.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/bbsnly/sdlc/internal/cli"
	"github.com/bbsnly/sdlc/internal/hook"
	"github.com/bbsnly/sdlc/internal/logging"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

// run is main without the process. Everything it needs is passed in so the
// whole binary is testable, including its failure modes.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) (code int) {
	// A hook is invoked by Claude Code, which reads stdout as JSON. A panic
	// that printed a Go stack there would be a parse error on the other side
	// and an unexplained failure to the user, so the top-level recover always
	// leaves stdout well-formed.
	isHook := len(args) > 0 && args[0] == "hook"

	// Whether to include a stack is read from the environment, and the panic
	// being handled may have come from that read. The crash handler is the last
	// line of defence and must be total, so it is told what was learned rather
	// than going back to look: if the panic beat the lookup, we do not know
	// whether debugging is on and take the quiet path.
	var debugKnown bool
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		code = crash(r, debug.Stack(), isHook, stdout, stderr, debugKnown)
	}()

	opts := logging.FromEnv(getenv)
	debugKnown = opts.Debug
	closeLog := logging.Setup(opts, stderr)
	defer func() { _ = closeLog() }()

	// The fast path. Hooks fire on every matching tool call, so they must not
	// pay for building the command tree; internal/cli builds it inside a
	// function precisely so that not calling it costs nothing.
	if isHook {
		defer logging.Stage(opts, "hook")()
		return hook.Run(args[1:], stdin, stdout, stderr, getenv)
	}

	defer logging.Stage(opts, "cli")()
	return cli.Execute(args, stdin, stdout, stderr)
}

// crash turns a panic into something the caller can act on: a machine-readable
// refusal for a hook, a short human line otherwise. The stack goes to stderr
// only when debugging is on, and never to stdout.
func crash(r any, stack []byte, isHook bool, stdout, stderr io.Writer, withStack bool) int {
	const issues = "https://github.com/bbsnly/sdlc/issues"
	msg := fmt.Sprintf("sdlc crashed: %v", r)

	if isHook {
		// Continue rather than block. A bug in this tool must not be able to
		// wedge someone's session; the loop's guarantees are worth less than
		// the user's ability to keep working.
		b, err := json.Marshal(hook.Decision{
			Continue:   true,
			StopReason: msg,
		})
		if err != nil {
			fmt.Fprint(stdout, `{"continue":true}`)
		} else {
			fmt.Fprintln(stdout, string(b))
		}
	}

	fmt.Fprintf(stderr, "%s\n\nThis is a bug. Please report it, with the command you ran:\n  %s\n", msg, issues)
	if withStack {
		fmt.Fprintf(stderr, "\n%s\n", stack)
	} else {
		fmt.Fprintf(stderr, "Re-run with %s=1 to include a stack trace in the report.\n", logging.EnvDebug)
	}
	return 1
}
