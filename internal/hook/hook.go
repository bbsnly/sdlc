// Package hook is the fast path.
//
// Claude Code runs a hook on every matching tool call, so this code is on the
// latency path of the user's whole session. Nothing here builds a command
// tree, reads config it does not need, or touches the network. The rest of the
// CLI is reached only when the first argument is not `hook`.
//
// The verbs land in a later phase; today this is the shape and the contract:
// a payload arrives as JSON on stdin, a decision leaves as JSON on stdout, and
// stdout carries nothing else, ever.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
)

// Decision is what Claude Code reads back from a hook. Field names match the
// hook protocol and are not ours to rename.
type Decision struct {
	// Continue false stops the action. Omitted when true, because the common
	// case should be the smallest payload.
	Continue bool `json:"continue"`
	// StopReason is shown to the user when Continue is false. It must say what
	// happened and what to do instead; a denial that explains nothing trains
	// people to work around the tool.
	StopReason string `json:"stopReason,omitempty"`
	// SuppressOutput hides this hook's stdout from the transcript.
	SuppressOutput bool `json:"suppressOutput,omitempty"`
}

// Allow is the decision for the overwhelmingly common case.
func Allow() Decision { return Decision{Continue: true} }

// Deny refuses an action, with a reason the reader can act on.
func Deny(reason string) Decision { return Decision{Continue: false, StopReason: reason} }

// Run dispatches a hook event. args is everything after the `hook` verb.
func Run(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) == 0 {
		return emit(stdout, Allow())
	}
	// Payloads are small and bounded; a hook that hangs reading stdin would
	// hang the session, so the read is limited rather than unbounded.
	_, _ = io.Copy(io.Discard, io.LimitReader(stdin, 1<<20))

	// Until the policy engine lands, every event is allowed. This is the
	// honest state: the plumbing is real, the decisions are not yet.
	return emit(stdout, Allow())
}

func emit(stdout io.Writer, d Decision) int {
	b, err := json.Marshal(d)
	if err != nil {
		// Unreachable for this struct, but a hook must always produce valid
		// JSON: a parse error on the other side is worse than a denial.
		fmt.Fprint(stdout, `{"continue":true}`)
		return 0
	}
	fmt.Fprintln(stdout, string(b))
	return 0
}
