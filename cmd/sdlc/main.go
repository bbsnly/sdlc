// Command sdlc runs a story-driven delivery loop for Claude Code.
//
// See https://github.com/bbsnly/sdlc for what the loop is and how to use it.
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/bbsnly/sdlc/internal/cli"
	"github.com/bbsnly/sdlc/internal/hook"
	"github.com/bbsnly/sdlc/internal/logging"
	"github.com/bbsnly/sdlc/internal/store"
)

func main() {
	exitOnInterrupt()
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

// exitOnInterrupt ends the process on Ctrl+C or a request to terminate, as it
// would have ended anyway, but gives back the project's lock first. Without it,
// a command interrupted while it held the lock left every other sdlc command in
// the project refusing until the lock was old enough to break open.
func exitOnInterrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signals
		store.ReleaseHeld(2 * time.Second)
		code := 1
		if s, ok := sig.(syscall.Signal); ok {
			code = 128 + int(s)
		}
		os.Exit(code)
	}()
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
		//
		// And say nothing to the session, with exit 0 below. The hook runs in
		// every session the plugin is installed for, and a crash comes before
		// it knows whether this one is working a story; one that is not must
		// not hear from the plugin at all. The report goes to stderr, which is
		// Claude Code's debug log.
		fmt.Fprintln(stdout, `{"continue":true}`)
	}

	fmt.Fprintf(stderr, "%s\n\nThis is a bug. Please report it, with the command you ran:\n  %s\n", msg, issues)
	if withStack {
		fmt.Fprintf(stderr, "\n%s\n", stack)
	} else {
		fmt.Fprintf(stderr, "Re-run with %s=1 to include a stack trace in the report.\n", logging.EnvDebug)
	}
	if isHook {
		// So that the reply above is read at all.
		return 0
	}
	return 1
}
