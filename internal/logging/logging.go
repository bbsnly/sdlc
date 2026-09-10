// Package logging wires the one logger this product uses.
//
// Diagnostics go to stderr or to a file, never to stdout: stdout is a protocol
// surface. Claude Code reads a hook's stdout as JSON, and a stray log line
// there is a parse error rather than a helpful message.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Environment variables this package reads. They are documented here because
// this is the only place that reads them.
const (
	// EnvDebug turns on debug-level logging when set to a true-ish value.
	EnvDebug = "SDLC_DEBUG"
	// EnvDebugFile sends logs to a file instead of stderr. A hook's stderr is
	// often invisible, so this is how you see what a hook did.
	EnvDebugFile = "SDLC_DEBUG_FILE"
	// EnvTrace adds per-stage timings at debug level.
	EnvTrace = "SDLC_TRACE"
)

// Options are resolved from the environment by Setup.
type Options struct {
	Debug bool
	Trace bool
	File  string
}

// FromEnv reads the logging options out of the environment.
func FromEnv(getenv func(string) string) Options {
	return Options{
		Debug: truthy(getenv(EnvDebug)),
		Trace: truthy(getenv(EnvTrace)),
		File:  getenv(EnvDebugFile),
	}
}

func truthy(v string) bool {
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true // any non-empty, non-boolean value means "on"
	}
	return b
}

// Setup installs the default logger and returns a closer for any file it
// opened. The closer is never nil.
//
// A file that cannot be opened is reported once on stderr and then ignored:
// failing to write a debug log is not a reason to fail the command the user
// actually asked for.
func Setup(opts Options, stderr io.Writer) (closer func() error) {
	closer = func() error { return nil }

	level := slog.LevelInfo
	if opts.Debug || opts.Trace {
		level = slog.LevelDebug
	}

	dest := stderr
	if opts.File != "" {
		f, err := openLog(opts.File)
		if err == nil {
			dest = f
			closer = f.Close
		} else {
			// Say so. Asking for a log file and silently getting none is the
			// worst of both: no log, and no reason to go looking for one.
			_, _ = io.WriteString(stderr,
				"sdlc: cannot write "+EnvDebugFile+"="+opts.File+" ("+err.Error()+")\n"+
					"      Logging to stderr instead. Point "+EnvDebugFile+" at a writable path.\n")
		}
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(dest, &slog.HandlerOptions{Level: level})))
	return closer
}

func openLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

// Stage times a unit of work and logs it when SDLC_TRACE is on. The returned
// function is meant to be deferred.
func Stage(opts Options, name string) func() {
	if !opts.Trace {
		return func() {}
	}
	start := time.Now()
	return func() {
		slog.Debug("stage", "name", name, "ms", time.Since(start).Milliseconds())
	}
}
