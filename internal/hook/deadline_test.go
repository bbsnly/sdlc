package hook

// US-011: a call the hook was going to check, and has not decided by its
// deadline, is refused by the hook instead of being let through by Claude
// Code's kill.
//
// Everything here reaches the hook through a seam the story adds, and nothing
// else in the package's tests does, so this is the one file that does not
// compile until the seam exists:
//
//	type clock struct {
//		now   func() time.Time
//		after func(time.Duration) <-chan time.Time
//		spawn func(work func())
//	}
//	func run(args []string, stdin io.Reader, stdout, stderr io.Writer,
//		getenv func(string) string, c clock) int
//	const hookDeadline time.Duration // the fixed figure
//	const deadlineRule string         // the rule name the refusal carries
//
// Run keeps its five arguments and is run with the machine's clock, a real
// timer and a goroutine.
//
// What the tests hold the seam to:
//
//   - now is read first when the hook starts, before the input is read, and
//     again when the hook learns the call is one it checks. A deadline that has
//     passed by then is refused there and then: neither after nor spawn is
//     called.
//   - Otherwise after is called once, with what is left of the deadline, and
//     the rest of the decision (reading the command, the file rules, measuring
//     the tree) is handed to spawn. Whichever of the returned channel and the
//     work comes first is the one answer.
//   - spawn may run the work on another goroutine, keep it for later, or run it
//     to the end before it returns, so the work never waits for the hook to
//     take its result. A panic inside it stays inside it.
//   - The budget git runs under (gitBudget) reads this same clock, which is how
//     a test finds the moment the tree is being measured.
//
// No test waits on the wall clock. deadlineGuard is how long a test waits for
// an answer that should already be there before it fails; a passing run never
// reaches it.

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/policy"
)

const deadlineGuard = 30 * time.Second

// carriesOn is all the hook prints for a call it has nothing to say about.
const carriesOn = "{\"continue\":true}\n"

// Text of a call that a refusal for running out of time has no reason to
// repeat.
const (
	markedCommand = "zq7-the-command-as-typed"
	markedPath    = "zq7-the-path-as-named"
	markedContent = "zq7-the-content-as-written"
)

// lockedBuffer is a stream the hook writes, read by the test while a worker
// may still be running.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// How a fakeDeadline treats the work the hook hands to spawn.
const (
	workRuns   = iota // on a goroutine of its own, at once
	workIsKept        // not until the test lets it go
	workInline        // to the end, before spawn returns
)

// fakeDeadline is the clock a test hands the hook: time that moves only when
// the test moves it, a timer that fires only when the test fires it, and the
// hook's worker where the test can hold it and wait for it.
type fakeDeadline struct {
	mu sync.Mutex
	at time.Time
	// timer is the one channel after hands out.
	timer chan time.Time
	// asked is every duration after was called with.
	asked []time.Duration
	// passOnAsk moves the clock past the deadline and fires the timer the
	// moment the hook sets it: the deadline passes just after the hook learnt
	// the call is one it checks.
	passOnAsk bool
	work      int
	kept      []func()
	workers   []chan struct{}
	// escaped is every panic that left the work handed to spawn. In the
	// released binary each one ends the process with nothing on stdout.
	escaped []any
	// onNow runs each time the clock is read, before it answers.
	onNow func()
}

func newFakeDeadline() *fakeDeadline {
	// From the machine's own time, because the budget git runs under is a real
	// context with its deadline worked out from this clock.
	return &fakeDeadline{at: time.Now(), timer: make(chan time.Time, 1)}
}

func (f *fakeDeadline) clock() clock {
	return clock{now: f.now, after: f.after, spawn: f.spawn}
}

func (f *fakeDeadline) now() time.Time {
	f.mu.Lock()
	reading := f.onNow
	f.mu.Unlock()
	if reading != nil {
		reading()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.at
}

func (f *fakeDeadline) after(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, d)
	if f.passOnAsk {
		f.at = f.at.Add(d + time.Millisecond)
		f.fireLocked()
	}
	return f.timer
}

func (f *fakeDeadline) spawn(work func()) {
	done := make(chan struct{})
	guarded := func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				f.mu.Lock()
				f.escaped = append(f.escaped, r)
				f.mu.Unlock()
			}
		}()
		work()
	}
	f.mu.Lock()
	f.workers = append(f.workers, done)
	how := f.work
	if how == workIsKept {
		f.kept = append(f.kept, guarded)
	}
	f.mu.Unlock()
	switch how {
	case workRuns:
		go guarded()
	case workInline:
		guarded()
	}
}

// move is time passing.
func (f *fakeDeadline) move(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at = f.at.Add(d)
}

// fire is the timer going off.
func (f *fakeDeadline) fire() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fireLocked()
}

func (f *fakeDeadline) fireLocked() {
	select {
	case f.timer <- f.at:
	default:
	}
}

func (f *fakeDeadline) spawned() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.workers)
}

func (f *fakeDeadline) timers() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.asked...)
}

// finish lets go of the work a test kept and waits for every worker to end,
// which is the only way to say that nothing more was written after the answer.
func (f *fakeDeadline) finish(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	kept, workers := f.kept, append([]chan struct{}(nil), f.workers...)
	f.kept = nil
	f.mu.Unlock()
	for _, work := range kept {
		go work()
	}
	for _, done := range workers {
		select {
		case <-done:
		case <-time.After(deadlineGuard):
			t.Fatalf("the hook's worker had not ended %s after it was let go", deadlineGuard)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.escaped) > 0 {
		t.Errorf("a panic left the hook's worker, which in the released binary ends the process "+
			"with nothing on stdout: %v", f.escaped)
	}
}

// deadlineAnswer is what one call to the hook left behind.
type deadlineAnswer struct {
	stdout, stderr *lockedBuffer
	// panicked is what run panicked with on the goroutine that called it, where
	// cmd/sdlc recovers and answers, or nil.
	panicked any
}

// ask puts one PreToolUse call to the hook on this clock and returns once the
// hook has answered.
func (f *fakeDeadline) ask(t *testing.T, stdin io.Reader, getenv func(string) string) deadlineAnswer {
	t.Helper()
	a := deadlineAnswer{stdout: &lockedBuffer{}, stderr: &lockedBuffer{}}
	type ended struct {
		code     int
		panicked any
	}
	end := make(chan ended, 1)
	go func() {
		var e ended
		defer func() {
			e.panicked = recover()
			end <- e
		}()
		e.code = run([]string{"PreToolUse"}, stdin, a.stdout, a.stderr, getenv, f.clock())
	}()
	select {
	case e := <-end:
		if e.panicked == nil && e.code != 0 {
			t.Fatalf("the hook exited %d, so its reply is never read", e.code)
		}
		a.panicked = e.panicked
	case <-time.After(deadlineGuard):
		t.Fatalf("the hook had not answered after %s: it is waiting for a decision that the deadline "+
			"should have answered for, or for work it was meant to hand to spawn", deadlineGuard)
	}
	return a
}

// oneAnswer holds stdout to one line holding one JSON object, and reads it.
func oneAnswer(t *testing.T, stdout string) reply {
	t.Helper()
	if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("stdout is not one line: %q", stdout)
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	var r reply
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, stdout)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one JSON answer: %q", stdout)
	}
	return r
}

// refusalShape is `<reason>. Instead: <route> [<rule>]`, the form every refusal
// of the hook's takes.
var refusalShape = regexp.MustCompile(`^(.+)\. Instead: (.+) \[([^\]\s]+)\]$`)

// wantOutOfTime holds an answer to being the refusal for running out of time, and
// returns its reason and its route.
func wantOutOfTime(t *testing.T, what, stdout string) (reason, route string) {
	t.Helper()
	r := oneAnswer(t, stdout)
	if !denied(r) || r.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Fatalf("%s: the hook did not refuse the call: %q", what, stdout)
	}
	m := refusalShape.FindStringSubmatch(r.HookSpecificOutput.PermissionDecisionReason)
	if m == nil {
		t.Fatalf("%s: the refusal is not `<reason>. Instead: <route> [<rule>]`: %q",
			what, r.HookSpecificOutput.PermissionDecisionReason)
	}
	if m[3] != deadlineRule {
		t.Fatalf("%s: refused under %s, want the rule for running out of time, %s:\n%s",
			what, m[3], deadlineRule, r.HookSpecificOutput.PermissionDecisionReason)
	}
	return m[1], m[2]
}

// saysOutOfTime reports whether an answer is the refusal for running out of
// time, for the tests that hold a call to not being given one.
func saysOutOfTime(stdout string) bool {
	return strings.Contains(stdout, "["+deadlineRule+"]")
}

// toolCallFrom is the JSON Claude Code sends before a tool call.
func toolCallFrom(session, cwd, tool string, input map[string]any) string {
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": session, "cwd": cwd,
		"tool_name": tool, "tool_input": input,
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// checkedCase is one call the hook was going to check.
type checkedCase struct {
	name  string
	stdin string
	// shell is whether it is a command, whose refusal is worded for one.
	shell bool
}

// checkedCalls is one of each kind of call the hook was going to check: any
// tool while a story is held in the session, and a command in a project that
// takes part from a session that holds none. Each is in a project of its own.
func checkedCalls(t *testing.T) []checkedCase {
	t.Helper()
	file := map[string]any{"file_path": "internal/" + markedPath + ".go", "content": markedContent}
	return []checkedCase{
		{"a command while a story is held",
			toolCallFrom(working, loopProject(t), "Bash", map[string]any{"command": "echo " + markedCommand}), true},
		{"a PowerShell command while a story is held",
			toolCallFrom(working, loopProject(t), "PowerShell", map[string]any{"command": "Write-Output " + markedCommand}), true},
		{"a command from a session that holds no story, in a project that takes part",
			toolCallFrom("session-b", loopProject(t), "Bash", map[string]any{"command": "echo " + markedCommand}), true},
		{"a file written while a story is held",
			toolCallFrom(working, loopProject(t), "Write", file), false},
		{"a file edited while a story is held",
			toolCallFrom(working, loopProject(t), "Edit", map[string]any{
				"file_path": "internal/" + markedPath + ".go", "old_string": "x", "new_string": markedContent}), false},
		{"a notebook edited while a story is held",
			toolCallFrom(working, loopProject(t), "NotebookEdit", map[string]any{
				"notebook_path": "notes/" + markedPath + ".ipynb", "new_source": markedContent}), false},
	}
}

// slowToRead is a payload that takes d to arrive: the clock moves as the hook
// begins to read it.
func slowToRead(f *fakeDeadline, payload string, d time.Duration) io.Reader {
	return &movingReader{r: strings.NewReader(payload), move: func() { f.move(d) }}
}

type movingReader struct {
	r    io.Reader
	once sync.Once
	move func()
}

func (m *movingReader) Read(p []byte) (int, error) {
	m.once.Do(m.move)
	return m.r.Read(p)
}

// slowToFind is an environment in which finding the loop takes d: the clock
// moves when the hook asks which directory the session was opened in, which is
// the first thing finding the loop does and nothing before it does.
func slowToFind(f *fakeDeadline, d time.Duration, pairs map[string]string) func(string) string {
	var once sync.Once
	return func(k string) string {
		if k == "CLAUDE_PROJECT_DIR" {
			once.Do(func() { f.move(d) })
		}
		return pairs[k]
	}
}

// readingTheCommand reports whether a variable is one the classifier asks for
// while it reads a git command: what git would take from the environment the
// session's commands run in. Nothing asks for these before the command is read.
func readingTheCommand(variable string) bool {
	return strings.HasPrefix(variable, "GIT_") && variable != "GIT_CEILING_DIRECTORIES"
}

// calledFrom reports whether a function of this name is on the stack.
func calledFrom(name string) bool {
	pcs := make([]uintptr, 128)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, "."+name) || strings.Contains(frame.Function, "."+name+".") {
			return true
		}
		if !more {
			return false
		}
	}
}

// ------------------------------------------------------------------ AC-1

// The three kinds of call the hook was going to check, each with the deadline
// already past when the hook learns it is one of them. Today Claude Code kills
// a hook that late and the call runs with no rule applied.
func TestAC1ACallTheHookWasGoingToCheckIsRefusedOnceTheDeadlineHasPassed(t *testing.T) {
	for _, c := range checkedCalls(t) {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeDeadline()
			a := f.ask(t, slowToRead(f, c.stdin, hookDeadline+time.Millisecond), noEnv)
			wantOutOfTime(t, c.name, a.stdout.String())

			// The control: the same call, with a millisecond of the deadline
			// left, is decided as it always was.
			f = newFakeDeadline()
			a = f.ask(t, slowToRead(f, c.stdin, hookDeadline-time.Millisecond), noEnv)
			f.finish(t)
			if saysOutOfTime(a.stdout.String()) {
				t.Errorf("refused for running out of time with a millisecond of the deadline left: %q", a.stdout.String())
			}
			var want bytes.Buffer
			Run([]string{"PreToolUse"}, strings.NewReader(c.stdin), &want, io.Discard, noEnv)
			if got := a.stdout.String(); got != want.String() {
				t.Errorf("inside the deadline the hook answered %q, and it answers %q with no clock handed in", got, want.String())
			}
		})
	}
}

// The deadline passing while the call is still being decided, which is the
// case the story is named for: the answer is the refusal, and it is given
// while the decision has not been made.
func TestAC1AndAC3ACallStillBeingDecidedWhenTheDeadlinePassesIsRefusedWithOneAnswer(t *testing.T) {
	calls := checkedCalls(t)
	// One the hook would refuse on its own account, so that a deadline refusal
	// here is known not to be that decision under another name.
	calls = append(calls, checkedCase{"an acknowledgement from a session that holds no story",
		toolCallFrom("session-b", loopProject(t), "Bash", map[string]any{"command": "sdlc ack --through HEAD"}), true})
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeDeadline()
			f.passOnAsk, f.work = true, workIsKept
			a := f.ask(t, strings.NewReader(c.stdin), noEnv)

			first := a.stdout.String()
			wantOutOfTime(t, c.name, first)
			if f.spawned() != 1 {
				t.Fatalf("the hook handed %d pieces of work to spawn, want the one decision", f.spawned())
			}
			// The decision is made after all, and comes to nothing.
			f.finish(t)
			if after := a.stdout.String(); after != first {
				t.Errorf("the decision arrived after the deadline had answered, and stdout went from %q to %q", first, after)
			}
		})
	}
}

// ------------------------------------------------------------------ AC-2

func TestAC2TheRefusalSaysTheHookRanOutOfTimeInWordsThatFollowTheTool(t *testing.T) {
	said := map[bool]map[string]bool{true: {}, false: {}}
	for _, c := range checkedCalls(t) {
		f := newFakeDeadline()
		a := f.ask(t, slowToRead(f, c.stdin, hookDeadline+time.Millisecond), noEnv)
		stdout := a.stdout.String()
		reason, route := wantOutOfTime(t, c.name, stdout)
		said[c.shell][reason+" / "+route] = true

		for _, text := range []string{markedCommand, markedPath, markedContent} {
			if strings.Contains(stdout, text) || strings.Contains(a.stderr.String(), text) {
				t.Errorf("%s: the refusal repeats %s from the call:\nstdout %q\nstderr %q", c.name, text, stdout, a.stderr.String())
			}
		}
		low := strings.ToLower(reason)
		if !strings.Contains(low, "time") {
			t.Errorf("%s: the reason does not say the hook ran out of time: %q", c.name, reason)
		}
		if c.shell {
			if !strings.Contains(low, "command") {
				t.Errorf("%s: the reason does not say it is the command the hook could not read: %q", c.name, reason)
			}
			if !strings.Contains(strings.ToLower(route), "shorter parts") {
				t.Errorf("%s: the route does not say to run it in shorter parts: %q", c.name, route)
			}
			continue
		}
		if !strings.Contains(low, "change") || strings.Contains(low, "command") {
			t.Errorf("%s: the reason is not about a change the hook could not check: %q", c.name, reason)
		}
		if r := strings.ToLower(route); !strings.Contains(r, "again") || !strings.Contains(r, "person") || strings.Contains(r, "shorter parts") {
			t.Errorf("%s: the route does not say to try again and to tell a person if it is refused again: %q", c.name, route)
		}
	}
	if len(said[true]) != 1 || len(said[false]) != 1 {
		t.Errorf("want one wording for a command and one for a file, got %d and %d:\n%v\n%v",
			len(said[true]), len(said[false]), said[true], said[false])
	}
	for words := range said[true] {
		if said[false][words] {
			t.Errorf("a command and a file are refused in the same words: %q", words)
		}
	}
}

// TestEveryRuleIsDocumented, in internal/cli, reads the rules of internal/policy
// and the ones the shell policy gives. This rule is neither, so nothing else
// would notice it had no section to look it up in.
func TestAC2TheRuleForRunningOutOfTimeIsItsOwnAndHasASectionInTheEnforcementPage(t *testing.T) {
	if !regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)+$`).MatchString(deadlineRule) {
		t.Errorf("the rule is named %q, which is not spelled as every other rule name is", deadlineRule)
	}
	for _, r := range policy.Rules {
		if r.ID == deadlineRule {
			t.Errorf("%s is already the name of a rule on writing files", deadlineRule)
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "enforcement.md"))
	if err != nil {
		t.Fatal(err)
	}
	heading := "### `" + deadlineRule + "`"
	// A checkout with CRLF line endings reads the same.
	_, body, found := strings.Cut(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"+heading+"\n")
	if !found {
		t.Fatalf("docs/enforcement.md has no section for the %s rule", deadlineRule)
	}
	if strings.Contains(body, "\n"+heading+"\n") {
		t.Errorf("docs/enforcement.md has more than one section headed %s", heading)
	}
	if next := strings.Index(body, "\n#"); next >= 0 {
		body = body[:next]
	}
	// A section about time, so that the name is not one borrowed from a rule
	// that was already there.
	if flat := strings.Join(strings.Fields(body), " "); !strings.Contains(flat, "in time") {
		t.Errorf("the section headed %s does not say the hook could not read or check the call in time:\n%s", heading, body)
	}
}

// ------------------------------------------------------------------ AC-3

// The decision first: the timer never fires, and what is written is what the
// hook writes with no clock handed in.
func TestAC3OneAnswerIsWrittenWhenTheDecisionComesFirst(t *testing.T) {
	calls := checkedCalls(t)
	root := loopProject(t)
	calls = append(calls,
		checkedCase{"loop state written by hand", event(root, "Write", "", ".sdlc/state/active"), false},
		checkedCase{"an early commit", command(root, "", "git commit -m x"), true},
		checkedCase{"an approval", command(root, "", "sdlc approve A-1"), true},
		checkedCase{"an acknowledgement from a session that holds no story",
			inSession(command(root, "", "sdlc ack --through HEAD"), "session-b"), true},
	)
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			var want bytes.Buffer
			Run([]string{"PreToolUse"}, strings.NewReader(c.stdin), &want, io.Discard, noEnv)

			f := newFakeDeadline()
			a := f.ask(t, strings.NewReader(c.stdin), noEnv)
			f.finish(t)
			oneAnswer(t, a.stdout.String())
			if got := a.stdout.String(); got != want.String() || saysOutOfTime(got) {
				t.Errorf("with the timer never fired the hook answered %q, want %q", got, want.String())
			}
			if f.spawned() != 1 {
				t.Errorf("the hook handed %d pieces of work to spawn, want the one decision", f.spawned())
			}
		})
	}
}

// Both at once: the decision is made and the timer has fired before the hook
// looks at either. Which one answers is the hook's to choose, and it answers
// once.
func TestAC3OneAnswerIsWrittenWhenTheDecisionAndTheDeadlineAreBothThere(t *testing.T) {
	for _, c := range checkedCalls(t) {
		t.Run(c.name, func(t *testing.T) {
			var decision bytes.Buffer
			Run([]string{"PreToolUse"}, strings.NewReader(c.stdin), &decision, io.Discard, noEnv)

			f := newFakeDeadline()
			f.passOnAsk, f.work = true, workInline
			a := f.ask(t, strings.NewReader(c.stdin), noEnv)
			f.finish(t)
			got := a.stdout.String()
			oneAnswer(t, got)
			if got != decision.String() {
				wantOutOfTime(t, c.name, got)
			}
		})
	}
}

// ------------------------------------------------------------------ AC-4

// preToolUseTimeouts is every timeout plugin/hooks/hooks.json gives a
// PreToolUse hook, which is how long Claude Code waits before it kills one.
func preToolUseTimeouts(t *testing.T) []time.Duration {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Timeout float64 `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	var out []time.Duration
	for _, entry := range cfg.Hooks["PreToolUse"] {
		for _, h := range entry.Hooks {
			out = append(out, time.Duration(h.Timeout*float64(time.Second)))
		}
	}
	if len(out) == 0 {
		t.Fatal("hooks.json gives no PreToolUse hook a timeout")
	}
	return out
}

// The five seconds are for what the deadline cannot see: the launcher finding
// the binary, the process starting, and the answer being written and read.
func TestAC4TheDeadlineIsAtLeastFiveSecondsShortOfTheTimeoutClaudeCodeGivesTheHook(t *testing.T) {
	const margin = 5 * time.Second
	if hookDeadline <= 0 {
		t.Fatalf("the deadline is %s", hookDeadline)
	}
	for _, given := range preToolUseTimeouts(t) {
		if given-hookDeadline < margin {
			t.Errorf("hooks.json gives PreToolUse %s and the hook's deadline is %s: %s apart, want at least %s",
				given, hookDeadline, given-hookDeadline, margin)
		}

		// And the figure is the one the hook goes by: a call five seconds short
		// of the kill is refused, whatever the constant says.
		root := loopProject(t)
		f := newFakeDeadline()
		a := f.ask(t, slowToRead(f, command(root, "", "ls"), given-margin+time.Millisecond), noEnv)
		wantOutOfTime(t, "a call five seconds short of the kill", a.stdout.String())
	}
}

// One figure: with nothing in the environment, the timer the hook sets for a
// call it checks is the fixed figure and no other.
func TestAC4WithNothingSetTheDeadlineInForceIsTheFixedFigure(t *testing.T) {
	root := loopProject(t)
	f := newFakeDeadline()
	a := f.ask(t, strings.NewReader(command(root, "", "ls")), noEnv)
	f.finish(t)
	if got := a.stdout.String(); got != carriesOn {
		t.Fatalf("`ls` inside the deadline was answered %q", got)
	}
	if got := f.timers(); len(got) != 1 || got[0] != hookDeadline {
		t.Errorf("the hook set its timer for %v, want the fixed figure once, %s", got, hookDeadline)
	}
}

// ------------------------------------------------------------------ AC-5

// Stage one, reading the input. The hook does not yet know what the call is,
// so it carries on, and refuses at the moment it learns the call is one it
// checks: nothing is handed to a worker, so no command is read and no head is
// written down for a call that was refused.
func TestAC5ADeadlinePassedWhileTheInputWasReadIsRefusedWhenTheHookLearnsItChecksTheCall(t *testing.T) {
	for _, c := range checkedCalls(t) {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeDeadline()
			a := f.ask(t, slowToRead(f, c.stdin, hookDeadline+time.Second), noEnv)
			wantOutOfTime(t, c.name, a.stdout.String())
			if f.spawned() != 0 {
				t.Errorf("the hook went on to decide a call it had already run out of time for")
			}
		})
	}
}

// Stage two, finding the loop.
func TestAC5ADeadlinePassedWhileTheLoopWasFoundIsRefusedWhenTheHookLearnsItChecksTheCall(t *testing.T) {
	for _, c := range checkedCalls(t) {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeDeadline()
			a := f.ask(t, strings.NewReader(c.stdin), slowToFind(f, hookDeadline+time.Second, nil))
			wantOutOfTime(t, c.name, a.stdout.String())
			if f.spawned() != 0 {
				t.Errorf("the hook went on to decide a call it had already run out of time for")
			}
		})
	}
}

// The clock starts when the hook starts, not when the hook learns what the
// call is: time spent reading the input and finding the loop comes off what is
// left for the decision.
func TestAC5TheClockStartsWhenTheHookStartsAndTheTimerIsSetForWhatIsLeft(t *testing.T) {
	for _, c := range checkedCalls(t) {
		t.Run(c.name, func(t *testing.T) {
			reading, finding := 10*time.Second, 5*time.Second
			if hookDeadline <= reading+finding {
				t.Fatalf("the deadline is %s, which this test took to be more than %s", hookDeadline, reading+finding)
			}
			f := newFakeDeadline()
			a := f.ask(t, slowToRead(f, c.stdin, reading), slowToFind(f, finding, nil))
			f.finish(t)
			if saysOutOfTime(a.stdout.String()) {
				t.Fatalf("refused for running out of time %s into a deadline of %s: %q",
					reading+finding, hookDeadline, a.stdout.String())
			}
			want := hookDeadline - reading - finding
			if got := f.timers(); len(got) != 1 || got[0] != want {
				t.Errorf("after %s reading the input and %s finding the loop the hook set its timer for %v, want %s once",
					reading, finding, got, want)
			}
		})
	}
}

// Stage three, reading the command. The hook knows by now that it checks the
// call, so the deadline does not wait to be looked at: the classifier is
// stopped part-way through a git command, where it asks what git would take
// from the environment, the deadline passes, and the refusal is given while
// the classifier is still there.
func TestAC5ADeadlineThatPassesWhileTheCommandIsReadIsRefusedWithoutWaitingForTheDecision(t *testing.T) {
	root := loopProject(t)
	f := newFakeDeadline()
	reached, letGo := make(chan struct{}), make(chan struct{})
	var once, release sync.Once
	t.Cleanup(func() { release.Do(func() { close(letGo) }) })
	getenv := func(k string) string {
		if readingTheCommand(k) {
			once.Do(func() {
				f.move(hookDeadline + time.Second)
				f.fire()
				close(reached)
				<-letGo
			})
		}
		return ""
	}

	a := f.ask(t, strings.NewReader(command(root, "", "git status --porcelain")), getenv)
	select {
	case <-reached:
	default:
		t.Fatalf("the hook answered %q before the command was being read at all", a.stdout.String())
	}
	first := a.stdout.String()
	reason, _ := wantOutOfTime(t, "a command still being read", first)
	if !strings.Contains(strings.ToLower(reason), "command") {
		t.Errorf("the refusal of a command is not worded for one: %q", reason)
	}

	// Only now does the classifier get its answer and finish.
	release.Do(func() { close(letGo) })
	f.finish(t)
	if after := a.stdout.String(); after != first {
		t.Errorf("the decision arrived after the deadline had answered, and stdout went from %q to %q", first, after)
	}
}

// Stage four, measuring the tree: a commit past every gate, whose reviews are
// compared with the tree as it stands. The budget git runs under reads the
// hook's clock when the first git call is about to be made, and that is where
// the deadline passes.
func TestAC5ADeadlineThatPassesWhileTheTreeIsMeasuredIsRefusedWithoutWaitingForTheDecision(t *testing.T) {
	root := readyToCommit(t)
	f := newFakeDeadline()
	reached, letGo := make(chan struct{}), make(chan struct{})
	var once, release sync.Once
	t.Cleanup(func() { release.Do(func() { close(letGo) }) })
	f.onNow = func() {
		if !calledFrom("reviewsFresh") {
			return
		}
		once.Do(func() {
			f.move(hookDeadline + time.Second)
			f.fire()
			close(reached)
			<-letGo
		})
	}

	a := f.ask(t, strings.NewReader(command(root, "", `git commit -m "the work"`)), noEnv)
	select {
	case <-reached:
	default:
		t.Fatalf("the hook answered %q and its clock was never read while the tree was measured: "+
			"the budget git runs under has to be given the clock the hook was handed", a.stdout.String())
	}
	first := a.stdout.String()
	wantOutOfTime(t, "a commit whose tree is still being measured", first)

	release.Do(func() { close(letGo) })
	f.finish(t)
	if after := a.stdout.String(); after != first {
		t.Errorf("the decision arrived after the deadline had answered, and stdout went from %q to %q", first, after)
	}
	// The commit was refused, so it was never made: a head written down for it
	// is one the next commit on that head, anybody's, is recorded against.
	if got := commits(t, root); len(got) != 0 {
		t.Errorf("the loop wrote down %v for a commit the hook refused", got)
	}
}

// The same, with the whole decision still to make when the deadline answers:
// the classifier then runs to the end, allows the commit, and has to leave no
// trace of a commit the session was told is refused.
func TestNoHeadIsWrittenDownForACommitTheDeadlineRefused(t *testing.T) {
	root := readyToCommit(t)
	stdin := command(root, "", `git commit -m "the work"`)

	f := newFakeDeadline()
	f.passOnAsk, f.work = true, workIsKept
	a := f.ask(t, strings.NewReader(stdin), noEnv)
	wantOutOfTime(t, "a commit the deadline answered for", a.stdout.String())
	f.finish(t)
	if got := commits(t, root); len(got) != 0 {
		t.Errorf("the loop wrote down %v for a commit the hook refused", got)
	}

	// The control. Nothing written down means nothing only once the recorder
	// runs at all on this clock: the same commit, decided in time, is allowed
	// and its head written down.
	f = newFakeDeadline()
	a = f.ask(t, strings.NewReader(stdin), noEnv)
	f.finish(t)
	if got := a.stdout.String(); got != carriesOn {
		t.Fatalf("a commit past every gate, decided inside the deadline, was answered %q", got)
	}
	if got := commits(t, root); len(got) != 1 {
		t.Fatalf("the loop wrote down %v for a commit it allowed, so nothing above was the "+
			"recorder's answer rather than its absence", got)
	}
}

// ------------------------------------------------------------------ AC-6 and AC-7

// uncheckedCase is a call the hook learns it does not check.
type uncheckedCase struct {
	name, stdin string
	env         map[string]string
}

// uncheckedCalls is one call for each way the hook learns that: enforcement
// turned off, a payload that names no session, a project that does not take
// part, and a file written from a session that holds no story.
func uncheckedCalls(t *testing.T) []uncheckedCase {
	t.Helper()
	held, apart := loopProject(t), t.TempDir()
	noStory := loopProject(t)
	if err := os.Remove(filepath.Join(noStory, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	file := map[string]any{"file_path": "internal/" + markedPath + ".go", "content": markedContent}
	off := map[string]string{"SDLC_ENFORCE": "0"}
	return []uncheckedCase{
		{"enforcement off, an early commit in the session working a story", command(held, "", "git commit -m x"), off},
		{"enforcement off, loop state written in the session working a story", event(held, "Write", "", ".sdlc/state/active"), off},
		{"a payload that cannot be read", "not json at all", nil},
		{"a command in a project that does not take part",
			toolCallFrom(working, apart, "Bash", map[string]any{"command": "echo " + markedCommand}), nil},
		{"a file in a project that does not take part", toolCallFrom(working, apart, "Write", file), nil},
		{"a file from a session that holds no story", toolCallFrom("session-b", held, "Write", file), nil},
		{"loop state from a session that holds no story", inSession(event(held, "Write", "", ".sdlc/state/active"), "session-b"), nil},
		{"a file where no story is being worked on", toolCallFrom(working, noStory, "Write", file), nil},
	}
}

// A session that is not working a story hears nothing from the plugin, and a
// person who turned enforcement off is refused nothing: not when the deadline
// passed long ago, not when the timer has fired, and not when the deadline is
// a millisecond.
func TestAC7TheDeadlineRefusesNothingWhereTheHookHasNothingToCheck(t *testing.T) {
	for _, c := range uncheckedCalls(t) {
		for _, variable := range []string{"", "1"} {
			t.Run(c.name+", SDLC_HOOK_DEADLINE_MS="+variable, func(t *testing.T) {
				pairs := map[string]string{"SDLC_HOOK_DEADLINE_MS": variable}
				maps.Copy(pairs, c.env)
				f := newFakeDeadline()
				f.passOnAsk = true
				f.fire()
				a := f.ask(t, slowToRead(f, c.stdin, time.Hour), slowToFind(f, time.Hour, pairs))
				f.finish(t)
				if got := a.stdout.String(); got != carriesOn {
					t.Errorf("stdout = %q, want exactly %q", got, carriesOn)
				}
				if got := a.stderr.String(); got != "" {
					t.Errorf("the hook said something on stderr: %q", got)
				}
			})
		}
	}
}

// debugLines collects what the hook logs, at debug level, until the test ends.
func debugLines(t *testing.T) *lockedBuffer {
	t.Helper()
	var buf lockedBuffer
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(before) })
	return &buf
}

// aboutTheDeadline is the debug lines that speak of the deadline.
func aboutTheDeadline(log string) []string {
	var out []string
	for line := range strings.SplitSeq(log, "\n") {
		if strings.Contains(line, "level=DEBUG") && strings.Contains(strings.ToLower(line), "deadline") {
			out = append(out, line)
		}
	}
	return out
}

// A deadline that passes before the hook knows whether it checks the call
// answers nothing. The hook carries on finding out; the call it then learns it
// does not check is answered as it is today; and the debug log is the one
// place that says the deadline went by, which is all a person has to go on
// when a hook that slow is killed the next time.
func TestAC6ADeadlinePassedBeforeTheHookKnowsIsSaidInTheDebugLogAndNowhereElse(t *testing.T) {
	for _, c := range uncheckedCalls(t) {
		if c.env != nil || !json.Valid([]byte(c.stdin)) {
			// Enforcement off and a payload that cannot be read are known at
			// once, before there is anything to carry on with.
			continue
		}
		for stage, late := range map[string]func(f *fakeDeadline) (io.Reader, func(string) string){
			"while the input was read": func(f *fakeDeadline) (io.Reader, func(string) string) {
				return slowToRead(f, c.stdin, hookDeadline+time.Second), noEnv
			},
			"while the loop was found": func(f *fakeDeadline) (io.Reader, func(string) string) {
				return strings.NewReader(c.stdin), slowToFind(f, hookDeadline+time.Second, nil)
			},
		} {
			t.Run(c.name+", "+stage, func(t *testing.T) {
				log := debugLines(t)

				// Inside the deadline first, for what the log says of it then.
				f := newFakeDeadline()
				a := f.ask(t, strings.NewReader(c.stdin), noEnv)
				f.finish(t)
				if got := a.stdout.String(); got != carriesOn {
					t.Fatalf("inside the deadline, stdout = %q, want exactly %q", got, carriesOn)
				}
				inTime := len(aboutTheDeadline(log.String()))

				f = newFakeDeadline()
				stdin, getenv := late(f)
				a = f.ask(t, stdin, getenv)
				f.finish(t)
				if got := a.stdout.String(); got != carriesOn {
					t.Errorf("stdout = %q, want exactly %q: the call is not one the hook checks", got, carriesOn)
				}
				if got := a.stderr.String(); got != "" {
					t.Errorf("the hook said something on stderr: %q", got)
				}
				said := aboutTheDeadline(log.String())
				if len(said) <= inTime {
					t.Errorf("the debug log says nothing of a deadline that passed before the hook knew "+
						"whether it checks the call:\n%s", log.String())
				}
				for _, line := range said {
					for _, text := range []string{markedCommand, markedContent} {
						if strings.Contains(line, text) {
							t.Errorf("the debug line about the deadline repeats %s from the call: %s", text, line)
						}
					}
				}
			})
		}
	}
}

// ------------------------------------------------------------------ AC-8

// What the hook prints today, byte for byte, for calls decided inside the
// deadline: through Run as cmd/sdlc calls it, and through run on a clock that
// never moves. The warnings are here because the worker raises them and the
// answer carries them.
func TestAC8ACallDecidedInsideTheDeadlineIsAnsweredByteForByteAsItWasBefore(t *testing.T) {
	const (
		configWarning = ".sdlc/config.json could not be read, so tests are being recognised by the default " +
			"patterns until it can be. Run `sdlc doctor` to see why."
		freezeWarning = ".sdlc/state/tests.lock could not be read, so every test file is being treated as " +
			"frozen until it can be. Run `sdlc doctor` to see why."
	)
	refusal := func(reason string) string {
		raw, err := json.Marshal(reason)
		if err != nil {
			t.Fatal(err)
		}
		return `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny",` +
			`"permissionDecisionReason":` + string(raw) + `}}` + "\n"
	}
	warned := func(warning string) string {
		raw, err := json.Marshal("sdlc: " + warning)
		if err != nil {
			t.Fatal(err)
		}
		return `{"continue":true,"systemMessage":` + string(raw) + `}` + "\n"
	}

	root := loopProject(t)
	brokenConfig := loopProject(t)
	write(t, brokenConfig, ".sdlc/config.json", "{")
	brokenFreeze := loopProject(t)
	write(t, brokenFreeze, ".sdlc/state/tests.lock", "{")

	for _, tc := range []struct {
		name, stdin, stdout, stderr string
	}{
		{"a command the loop allows", command(root, "", "ls"), carriesOn, ""},
		{"a file the implementer may write", event(root, "Write", "sdlc:implementer", "internal/x.go"), carriesOn, ""},
		{"a command from a session that holds no story", inSession(command(root, "", "ls"), "session-b"), carriesOn, ""},
		{"loop state written by hand", event(root, "Write", "", ".sdlc/state/active"),
			refusal(".sdlc/state/active is protected while a story is being worked on: it is either human-owned " +
				"configuration or the loop's own record of what happened. Instead: configuration is yours to change " +
				"by hand outside a running iteration, and loop state changes through the sdlc command [write-protected-path]"), ""},
		{"a test written by the implementer", event(root, "Write", "sdlc:implementer", "internal/x_test.go"),
			refusal("the agent that implements a story does not write its tests -- that is the separation the loop " +
				"is made of. Instead: make the existing tests pass; if they are wrong, say so rather than changing " +
				"them [implementer-does-not-write-tests]"), ""},
		{"an early commit", command(root, "", "git commit -m x"),
			refusal("this story has not been through the gates that come before committing: " +
				".sdlc/stories/A-1/gate-record.json is missing, so no gate can be shown to have passed (`sdlc doctor` " +
				"says more). Instead: finish the gates -- `sdlc status` shows where this story stands; committing " +
				"without them is for the person running the session to decide [commit-gate]"), ""},
		{"an approval", command(root, "", "sdlc approve A-1"),
			refusal("an approval is a person's answer to a question the loop stopped to ask them, and an agent that " +
				"gave it would be approving its own work. Instead: hand the question over with `sdlc escalate <type> " +
				"--message \"...\"` and stop; the person reads the work and runs `/sdlc:approve`, or `sdlc approve` in their own terminal " +
				"[approval-is-a-human-decision]"), ""},
		{"an acknowledgement from a session that holds no story",
			inSession(command(root, "", "sdlc ack --through HEAD"), "session-b"),
			refusal("acknowledging says a person has read what landed on trunk, and an agent that gave it would be " +
				"deciding which stories nobody needs to look at. Instead: say what `sdlc log` lists and stop; the " +
				"person reads it and runs `/sdlc:ack`, or `sdlc ack --through <commit>` in their own terminal " +
				"[acknowledgement-is-a-human-decision]"), ""},
		{"a file written where the configuration will not read", event(brokenConfig, "Write", "sdlc:implementer", "internal/x.go"),
			warned(configWarning), "sdlc: " + configWarning + "\n"},
		{"a file written where the freeze will not read", event(brokenFreeze, "Write", "sdlc:implementer", "internal/x.go"),
			warned(freezeWarning), "sdlc: " + freezeWarning + "\n"},
		{"a command where the freeze will not read", command(brokenFreeze, "", "ls"),
			warned(freezeWarning), "sdlc: " + freezeWarning + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := Run([]string{"PreToolUse"}, strings.NewReader(tc.stdin), &out, &errOut, noEnv); code != 0 {
				t.Fatalf("exit %d", code)
			}
			if out.String() != tc.stdout || errOut.String() != tc.stderr {
				t.Errorf("Run:\nstdout %q\nstderr %q\nwant\nstdout %q\nstderr %q", out.String(), errOut.String(), tc.stdout, tc.stderr)
			}

			f := newFakeDeadline()
			a := f.ask(t, strings.NewReader(tc.stdin), noEnv)
			f.finish(t)
			if a.stdout.String() != tc.stdout || a.stderr.String() != tc.stderr {
				t.Errorf("on a clock that never moves:\nstdout %q\nstderr %q\nwant\nstdout %q\nstderr %q",
					a.stdout.String(), a.stderr.String(), tc.stdout, tc.stderr)
			}
		})
	}
}

// ------------------------------------------------------------------ AC-11

// The variable only shortens. What it leaves in force is read from the timer
// the hook sets for a call it checks, on a clock that has not moved: the whole
// of the deadline in force, to the nanosecond.
func TestAC11TheVariableShortensTheDeadlineAndNothingElse(t *testing.T) {
	figure := int64(hookDeadline / time.Millisecond)
	if time.Duration(figure)*time.Millisecond != hookDeadline || figure < 3 {
		t.Fatalf("the fixed figure is %s, which is not a whole number of milliseconds above 2", hookDeadline)
	}
	itoa := func(n int64) string {
		raw, err := json.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	type row struct {
		name, value string
		want        time.Duration
	}
	rows := []row{
		{"the smallest", "1", time.Millisecond},
		{"a shorter one", "5000", 5 * time.Second},
		{"one below the figure", itoa(figure - 1), hookDeadline - time.Millisecond},
		{"the figure itself", itoa(figure), hookDeadline},
		{"one above the figure", itoa(figure + 1), hookDeadline},
		{"ten times the figure", itoa(figure * 10), hookDeadline},
		{"zero", "0", hookDeadline},
		{"minus zero", "-0", hookDeadline},
		{"a negative number", "-1", hookDeadline},
		{"a negative number below the figure", "-5000", hookDeadline},
		{"a plus sign", "+5", hookDeadline},
		{"a decimal point", "5.0", hookDeadline},
		{"a fraction", "1.5", hookDeadline},
		{"hexadecimal", "0x10", hookDeadline},
		{"an exponent", "1e3", hookDeadline},
		{"an underscore", "1_000", hookDeadline},
		{"a leading space", " 5", hookDeadline},
		{"a trailing space", "5 ", hookDeadline},
		{"a trailing newline", "5\n", hookDeadline},
		{"a unit", "5ms", hookDeadline},
		{"a duration", "5s", hookDeadline},
		{"a word", "abc", hookDeadline},
		{"empty", "", hookDeadline},
		{"too large for any integer", "99999999999999999999999999", hookDeadline},
		{"one that wraps a 64-bit integer to a small number", "18446744073709551617", hookDeadline},
		{"one that overflows once multiplied into nanoseconds", "9223372036854775807", hookDeadline},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			root := loopProject(t)
			f := newFakeDeadline()
			a := f.ask(t, strings.NewReader(command(root, "", "ls")), env(map[string]string{"SDLC_HOOK_DEADLINE_MS": r.value}))
			f.finish(t)
			if got := a.stdout.String(); got != carriesOn {
				t.Fatalf("SDLC_HOOK_DEADLINE_MS=%q on a clock that has not moved: `ls` was answered %q", r.value, got)
			}
			if got := f.timers(); len(got) != 1 || got[0] != r.want {
				t.Errorf("SDLC_HOOK_DEADLINE_MS=%q: the hook set its timer for %v, want %s once", r.value, got, r.want)
			}
		})
	}
}

// And the shortened deadline is the one the hook refuses by, on either side of
// it; a value it does not take refuses nothing early.
func TestAC11AShortenedDeadlineRefusesWhereTheFixedFigureWouldNot(t *testing.T) {
	if hookDeadline <= 6*time.Second {
		t.Fatalf("the deadline is %s, which this test took to be more than six seconds", hookDeadline)
	}
	for _, tc := range []struct {
		value   string
		taken   time.Duration
		refused bool
	}{
		{"5000", 5*time.Second + time.Millisecond, true},
		{"5000", 5*time.Second - time.Millisecond, false},
		{"1", 2 * time.Millisecond, true},
		{"", 5*time.Second + time.Millisecond, false},
		{"0", time.Millisecond, false},
		{"-1", time.Millisecond, false},
		{"+5", 6 * time.Millisecond, false},
		{"5.0", 6 * time.Millisecond, false},
		{" 5", 6 * time.Millisecond, false},
		{"0x10", 17 * time.Millisecond, false},
		{"99999999999", hookDeadline + time.Millisecond, true},
		{"99999999999", hookDeadline - time.Millisecond, false},
	} {
		for _, c := range checkedCalls(t) {
			f := newFakeDeadline()
			a := f.ask(t, slowToRead(f, c.stdin, tc.taken), env(map[string]string{"SDLC_HOOK_DEADLINE_MS": tc.value}))
			f.finish(t)
			if got := saysOutOfTime(a.stdout.String()); got != tc.refused {
				t.Errorf("SDLC_HOOK_DEADLINE_MS=%q, %s, %s into the call: refused for running out of time = %v, want %v (%q)",
					tc.value, c.name, tc.taken, got, tc.refused, a.stdout.String())
			}
		}
	}
}

// The variable alone refuses nothing: a millisecond's deadline on a clock that
// has not moved, with a timer that does not fire, is a call decided in time.
func TestAC11AShortDeadlineThatHasNotPassedRefusesNothing(t *testing.T) {
	for _, c := range checkedCalls(t) {
		var want bytes.Buffer
		Run([]string{"PreToolUse"}, strings.NewReader(c.stdin), &want, io.Discard, noEnv)

		f := newFakeDeadline()
		a := f.ask(t, strings.NewReader(c.stdin), env(map[string]string{"SDLC_HOOK_DEADLINE_MS": "1"}))
		f.finish(t)
		if got := a.stdout.String(); got != want.String() {
			t.Errorf("%s, with a deadline of a millisecond that has not passed: answered %q, want %q", c.name, got, want.String())
		}
	}
}

// ------------------------------------------------------------------ the worker

// The hook fails open on its own bugs, and a refusal is a decision the code
// made. A panic while the command is read used to reach cmd/sdlc, which
// answers "carry on". On a goroutine of its own it would end the process with
// a stack and no answer, so it has to be caught there, and it must not come
// out as the refusal for running out of time: that would make a crash the way
// to be refused, and the session would be told to try shorter parts of a
// command the hook cannot read at any length.
func TestAPanicWhileTheCommandIsReadAnswersCarryOnAndNeverARefusal(t *testing.T) {
	root := loopProject(t)
	for _, late := range []bool{false, true} {
		f := newFakeDeadline()
		getenv := func(k string) string {
			if readingTheCommand(k) {
				if late {
					f.move(hookDeadline + time.Second)
				}
				panic("the classifier gave way")
			}
			return ""
		}
		a := f.ask(t, strings.NewReader(command(root, "", "git status --porcelain")), getenv)
		f.finish(t)
		stdout := a.stdout.String()
		if a.panicked != nil {
			// Raised again where cmd/sdlc recovers, which then writes the answer.
			if stdout != "" {
				t.Errorf("the hook panicked on its caller's goroutine after writing %q, so two answers are written", stdout)
			}
			continue
		}
		if stdout != carriesOn {
			t.Errorf("after a panic while the command was read (deadline passed by then: %v) stdout = %q, want exactly %q",
				late, stdout, carriesOn)
		}
	}
}
