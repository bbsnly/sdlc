package hook

import (
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/bbsnly/sdlc/internal/policy"
)

// hookDeadline is how long the hook gives itself for a PreToolUse call, from
// the moment it starts. Claude Code kills a hook that outlasts the timeout
// hooks.json gives it, and a killed hook lets the call through with no rule
// applied and nothing said: a command long enough to read was the way round
// every rule. So the hook answers for itself first, with a refusal.
//
// It is five seconds short of that timeout, and the five seconds are for what
// this clock cannot see: the launcher finding the binary, the process
// starting, and the answer being written and read. A test holds the two that
// far apart.
const hookDeadline = 25 * time.Second

// deadlineRule is the rule name the refusal for running out of time carries,
// for a command and for a file alike.
const deadlineRule = "not-checked-in-time"

// deadlineEnv can shorten the deadline and cannot lengthen it. It is read from
// the hook's own environment, which a command in the session cannot change.
const deadlineEnv = "SDLC_HOOK_DEADLINE_MS"

// The two refusals. Neither takes anything from the call, and each says that
// it is the hook that ran out of time: told a rule was broken, a session looks
// for the rule. They say "the sdlc hook" because a command is refused this way
// in a session that holds no story too, where nobody may know what the hook is.
const (
	lateCommandReason = "the sdlc hook ran out of time before it had finished checking this command, " +
		"so the command was not checked and has not been run; that is the limit of the hook, " +
		"not a rule the command broke"
	lateCommandRoute = "run it in shorter parts, one call for each; if a short command or a commit " +
		"is refused this way too, stop and tell the person running the session"
	lateChangeReason = "the sdlc hook ran out of time before it had checked this change, so the change " +
		"was not checked and has not been made; that is the limit of the hook, not a rule the change broke"
	lateChangeRoute = "try it again as it is; if it is refused this way again, stop and tell the " +
		"person running the session"
)

// clock is the time the hook runs on, and where the part of a decision that
// can outlast the deadline is run. They are parameters because a deadline is
// tested by moving time, not by waiting for it.
type clock struct {
	now   func() time.Time
	after func(time.Duration) <-chan time.Time
	// spawn runs work on a goroutine of its own. The hook does not wait for
	// it to return.
	spawn func(work func())
}

// deadlineIn is the deadline in force. Only a whole number of milliseconds
// above zero and below the fixed figure is taken: a sign, a space, a unit or
// a number too large leaves the fixed figure, so that nothing in the
// environment can give the hook longer than Claude Code does.
func deadlineIn(getenv func(string) string) time.Duration {
	value := getenv(deadlineEnv)
	ms, err := strconv.ParseUint(value, 10, 64)
	if err != nil || ms == 0 || ms >= uint64(hookDeadline/time.Millisecond) {
		if value != "" {
			// The one place a person can see that the value they set is not
			// the deadline in force. The value itself is the person's, not the
			// session's.
			slog.Debug("hook: "+deadlineEnv+" is not a whole number of milliseconds above 0 and below "+
				"the fixed deadline, so the fixed deadline is in force", "value", value, "deadline", hookDeadline.String())
		}
		return hookDeadline
	}
	return time.Duration(ms) * time.Millisecond
}

// deadlineVerdict is the refusal for a call the hook checks and could not
// decide in time, in the words of the tool that made it.
func deadlineVerdict(shell bool) policy.Verdict {
	if shell {
		return policy.Verdict{Rule: deadlineRule, Reason: lateCommandReason, Route: lateCommandRoute}
	}
	return policy.Verdict{Rule: deadlineRule, Reason: lateChangeReason, Route: lateChangeRoute}
}

// outcome is what the rest of a decision hands back to run.
type outcome struct {
	verdict policy.Verdict
	// ok is false where the answer could not be reached at all, which is
	// treated exactly like an allow.
	ok bool
	// record writes down the head an allowed commit is made on, or is nil. It
	// is run's to call and nobody else's: a head written down for a commit the
	// deadline refused is one the next commit on that head, anybody's, is
	// recorded against.
	record func()
	// crashed is what the work panicked with, and stack where.
	crashed any
	stack   []byte
}

// warnings is what the hook has to say about itself in the session working a
// story. The rest of a decision runs on a goroutine of its own and warns from
// there, while run reads what was said, so it is locked.
type warnings struct {
	stderr io.Writer
	mu     sync.Mutex
	said   []string
	closed bool
}

// warn says msg once: as part of the answer, and on stderr, which is where
// Claude Code's debug log picks it up.
//
// Once the answer has been taken it says nothing. When the deadline answers,
// git is cancelled under the work still running, and that work then warns
// that the tree could not be measured: a line about a commit that was refused.
func (w *warnings) warn(msg string) {
	w.mu.Lock()
	if w.closed || slices.Contains(w.said, msg) {
		w.mu.Unlock()
		return
	}
	w.said = append(w.said, msg)
	w.mu.Unlock()
	// Not under the lock: a stderr nobody drains would otherwise hold the
	// answer the deadline is there to give.
	fmt.Fprintln(w.stderr, "sdlc: "+msg)
}

// taken is every warning said so far, and the end of them.
func (w *warnings) taken() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return w.said
}
