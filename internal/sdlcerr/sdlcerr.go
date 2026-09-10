// Package sdlcerr is the one way this tool tells a person that something went
// wrong.
//
// Every error carries three fields and a code. What happened, why — the rule or
// the condition, never a stack trace — and the one command that fixes it. The
// code is stable, has a heading in docs/troubleshooting.md, and is the string to
// paste into a search or an issue.
//
// Codes cannot be minted outside this package. [Code] is a struct with an
// unexported field, so only the catalogue can produce one and an error invented
// elsewhere does not compile. That is a stronger guarantee than a test: a test
// can only check the constructions it was told about.
package sdlcerr

import (
	"errors"
	"strings"
)

// docsBase is where a code's heading lives. One constant, because the docs may
// move to a published site later and the codes must keep resolving.
const docsBase = "https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md"

// Code identifies one failure condition for the lifetime of the project. It is
// a struct rather than a string type on purpose: an untyped constant satisfies
// a defined string type, so Code would otherwise be forgeable from any package.
type Code struct{ id string }

// String returns the code as a user sees it, for example "SDLC-E0002".
func (c Code) String() string { return c.id }

// URL is the documentation heading for the code.
func (c Code) URL() string { return docsBase + "#" + strings.ToLower(c.id) }

// Error is a failure a user will read. Construct it with [New]; the Fix comes
// from the catalogue, so there is no way to build one without an answer to
// "what do I do now?".
type Error struct {
	Code  Code
	What  string // what happened, in the user's terms
	Why   string // the rule or condition that produced it
	Fix   string // the one command that resolves it
	cause error  // kept for errors.Is; never shown in Render
}

// New builds an error for the code, taking the fix from the catalogue.
func New(c Code, what, why string) *Error {
	return &Error{Code: c, What: what, Why: why, Fix: fixFor(c)}
}

// WithFix replaces the catalogue's fix with one that names this specific case —
// a story id, a path — where the general advice would make the user guess.
func (e *Error) WithFix(fix string) *Error {
	out := *e
	out.Fix = fix
	return &out
}

// WithCause attaches the underlying error so that errors.Is and errors.As keep
// working across the boundary. It is not shown to the user.
func (e *Error) WithCause(err error) *Error {
	out := *e
	out.cause = err
	return &out
}

// Error is the one-line form, for logs and for wrapping.
func (e *Error) Error() string {
	msg := e.What + " (" + e.Code.id + ")"
	if e.cause != nil {
		msg += ": " + e.cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause.
func (e *Error) Unwrap() error { return e.cause }

// Render is the form a user reads at the end of a command: what happened, why,
// what to do, and where to read more.
func (e *Error) Render() string {
	var b strings.Builder
	b.WriteString("sdlc: " + e.What + "\n\n")
	if e.Why != "" {
		b.WriteString("  why  " + e.Why + "\n")
	}
	b.WriteString("  fix  " + e.Fix + "\n\n")
	b.WriteString("  " + e.Code.id + "  " + e.Code.URL() + "\n")
	return b.String()
}

// Render returns the rich form when err is or wraps an [Error], and a plain
// line otherwise. Command surfaces call this rather than type-switching.
func Render(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Render()
	}
	return "sdlc: " + err.Error() + "\n"
}
