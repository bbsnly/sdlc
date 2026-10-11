// Package hook is the fast path.
//
// Claude Code runs a hook on every matching tool call, so this code is on the
// latency path of the user's whole session. Nothing here builds a command tree,
// parses configuration it does not need, or touches the network. The rest of
// the CLI is reached only when the first argument is not `hook`.
//
// It speaks only in the Claude Code session working a story. The plugin is
// installed for every session, and in any other one the hook holds no turn open
// and says nothing at all. It refuses nothing there but a person's decisions, in
// a project that takes part: acknowledging the log, and approving work handed
// over or lifting the freeze while a story has one waiting.
//
// It fails open. An error -- an unreadable payload, a missing configuration, a
// path that cannot be resolved -- ends in "carry on". In the session working a
// story it says what is off, except when the payload cannot be read, since then
// there is no telling which session made the call. A hook that blocks a session
// because of its own bug is worse than the mistake it was trying to prevent.
//
// Except where the unreadable file is the thing a rule protects. A test freeze
// or a gate record that is there and cannot be read is not treated as absent,
// because then breaking it would be the way round it: tests stay frozen and the
// commit waits until they read.
//
// And except when it runs out of time. Claude Code kills a hook that outlasts
// its timeout and lets the call through, so a command long enough to read got
// round every rule with nothing said. A call the hook knows it checks, and has
// not decided by its own deadline, is refused. What is left is a hook killed
// before it knows whether it checks the call: that one still goes through.
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/policy"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/testset"
)

// maxPayload bounds the read, so a hook that is never sent the end of its input
// cannot hold the session for good. It is far above what a model can put into
// one tool call: a call cut off at the bound will not parse, and goes unchecked.
const maxPayload = 64 << 20

// Decision is the generic reply: keep going, or stop with a reason. It is what
// a hook says when it has nothing specific to add, and what the crash handler
// falls back to.
type Decision struct {
	// Continue false stops the action. It is written either way, true
	// included, so a reply never leaves the host to assume what it meant.
	Continue bool `json:"continue"`
	// StopReason is shown to the user when Continue is false. It must say what
	// happened and what to do instead; a denial that explains nothing trains
	// people to work around the tool.
	StopReason string `json:"stopReason,omitempty"`
	// SuppressOutput hides this hook's stdout from the transcript.
	SuppressOutput bool `json:"suppressOutput,omitempty"`
	// SystemMessage is how a hook that allowed the call still says something.
	SystemMessage string `json:"systemMessage,omitempty"`
}

// Allow is the decision for the overwhelmingly common case.
func Allow() Decision { return Decision{Continue: true} }

// Deny refuses an action, with a reason the reader can act on.
func Deny(reason string) Decision { return Decision{Continue: false, StopReason: reason} }

// preToolUseOutput is the shape Claude Code reads to refuse a tool call. The
// field names belong to the hook protocol and are not ours to rename.
type preToolUseOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

type preToolUseReply struct {
	HookSpecificOutput preToolUseOutput `json:"hookSpecificOutput"`
	SystemMessage      string           `json:"systemMessage,omitempty"`
}

// payload is the part of the hook event this needs.
type payload struct {
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name"`
	AgentType     string `json:"agent_type"`
	// SessionID is the Claude Code session the call comes from. A subagent's
	// calls carry the session that spawned it.
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	// StopHookActive is set on a Stop event when the session is already
	// carrying on because a stop hook sent it back.
	StopHookActive bool `json:"stop_hook_active"`
	ToolInput      struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	} `json:"tool_input"`
}

// Run dispatches a hook event. args is everything after the `hook` verb, so
// args[0] is the event name.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	return run(args, stdin, stdout, stderr, getenv, clock{
		now:   time.Now,
		after: time.After,
		spawn: func(work func()) { go work() },
	})
}

// run is Run on the clock it is handed.
//
// A PreToolUse call has a deadline, hookDeadline, counted from here. Until the
// hook knows whether the call is one it checks, a deadline that passes changes
// nothing: the plugin is installed for every session, and one that is not
// working a story must not be refused, or told anything, because the hook was
// slow. At the moment the hook knows, a deadline already passed refuses the
// call there and then. After that the rest of the decision runs on a goroutine
// of its own against a timer, because nothing can interrupt the reading of a
// command, and whichever comes first is the one answer.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, c clock) int {
	// Before the input is read: a call that takes long to arrive has spent
	// the time all the same.
	start := c.now()
	// The payload is read even when the answer is already known: leaving it
	// unread can give the caller a broken pipe. One byte past the bound is read,
	// so that a payload cut off there is known to be one.
	raw, _ := io.ReadAll(io.LimitReader(stdin, maxPayload+1))
	if len(args) == 0 {
		return emit(stdout, Allow())
	}

	// Failing open is the right answer and being quiet about it is not, in the
	// session working a story: a hook that has decided to enforce nothing looks
	// exactly like a hook with nothing to enforce, and the session then reports
	// a freeze that is not there. Nothing warns anywhere else, because nothing
	// is reached anywhere else: findLoop finds no story for another session.
	//
	// The warning goes out as systemMessage. Stderr from a hook that exits 0
	// goes to Claude Code's debug log and nowhere else, so a warning written
	// only there is one nobody reads. It still goes to stderr as well, which
	// is where the debug log picks it up.
	said := &warnings{stderr: stderr}
	warn := said.warn

	if args[0] == "Stop" || args[0] == "PostToolUse" {
		return answerTurn(stdout, args[0], raw, getenv, said)
	}

	limit := deadlineIn(getenv)
	left := func() time.Duration { return limit - c.now().Sub(start) }
	// A deadline that passes before the hook knows is not an answer, and a
	// hook Claude Code kills while it is still finding out says nothing at
	// all. This line is then all a person has to go on, so it is written as
	// soon as the hook can notice, and it carries nothing of the call.
	unknown := func(stage string, remaining time.Duration) {
		slog.Debug("hook: the deadline passed before the hook knew whether it checks this call",
			"stage", stage, "deadline", limit.String(), "elapsed", (limit - remaining).String())
	}
	remaining := left()
	late := remaining <= 0
	if late {
		unknown("call read", remaining)
	}

	// Everything decide does runs here, on this goroutine, with no timer: a
	// call the hook turns out not to check is answered as it always was.
	rest, event, shell := decide(args[0], raw, getenv, warn, c.now)
	// Read once decide has returned, so that it holds for every way the hook
	// learns that it checks the call.
	remaining = left()
	if remaining <= 0 && !late {
		unknown("loop found", remaining)
	}

	answer := func(o outcome, warnings []string) int {
		return answerPreToolUse(stdout, event, o, warnings)
	}
	if rest == nil {
		return answer(outcome{verdict: policy.Allowed}, said.taken())
	}

	// What git runs under for the rest of the decision. It is cancelled when
	// the deadline answers, so that a git still measuring the tree for a
	// refused commit is not left running behind the hook, and only then: git
	// that stops at the deadline of its own accord reads as a tree that could
	// not be measured, which lets a commit through.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	refuse := func(when string) int {
		// The warnings are taken before git is cancelled. The work still
		// running warns that the tree could not be measured the moment its git
		// call is cancelled, and that would be said of a commit this refuses.
		warnings := said.taken()
		stop()
		// With the deadline in force, because one shortened in a shell
		// profile is otherwise nowhere to be seen.
		slog.Debug("hook: the call was not decided by the deadline, so it is refused",
			"when", when, "deadline", limit.String(), "elapsed", c.now().Sub(start).String())
		return answer(outcome{verdict: deadlineVerdict(shell), ok: true}, warnings)
	}
	if remaining <= 0 {
		// Nothing is handed on: no command is read, and no head written down,
		// for a call that is already refused.
		return refuse("as the hook learnt it checks the call")
	}

	timer := c.after(remaining)
	// Room for the one result, so that the work never waits for run to take
	// it: by then run may have answered and gone.
	done := make(chan outcome, 1)
	c.spawn(func() { decideInto(ctx, done, rest) })

	o, decided := awaitOutcome(done, timer)
	if !decided {
		return refuse("while the call was being decided")
	}
	if o.crashed != nil {
		// The stack cmd/sdlc prints is the one below, of this goroutine. Where
		// the work gave way is only here.
		slog.Debug("hook: the decision crashed on its own goroutine", "panic", o.crashed, "stack", string(o.stack))
		panic(o.crashed)
	}
	if o.record != nil {
		o.record()
	}
	return answer(o, said.taken())
}

// answerTurn answers a Stop or PostToolUse call, which have no deadline race
// and no verdict of their own: the reply is the whole answer.
func answerTurn(stdout io.Writer, event string, raw []byte, getenv func(string) string, said *warnings) int {
	warn := said.warn
	var reply turnReply
	if event == "Stop" {
		reply = decideStop(raw, getenv, warn, time.Now)
	} else {
		reply = formatWritten(raw, getenv, warn)
	}
	slog.Debug("hook turn decision", "event", event, "decision", reply.Decision)
	if warnings := said.taken(); len(warnings) > 0 {
		reply.SystemMessage = strings.TrimSpace(reply.SystemMessage + " sdlc: " + strings.Join(warnings, " "))
	}
	return emit(stdout, reply)
}

// answerPreToolUse is the one place a PreToolUse call is answered.
func answerPreToolUse(stdout io.Writer, event string, o outcome, warnings []string) int {
	// Why a hook did nothing is the hardest thing to find out from the outside,
	// so every decision is available at debug level. SDLC_DEBUG_FILE is the way
	// to see it: stderr from a hook that allowed the call is not shown.
	slog.Debug("hook decision",
		"event", event, "considered", o.ok, "allowed", o.verdict.Allowed, "rule", o.verdict.Rule)
	message := ""
	if len(warnings) > 0 {
		message = "sdlc: " + strings.Join(warnings, " ")
	}
	if !o.ok || o.verdict.Allowed {
		d := Allow()
		d.SystemMessage = message
		return emit(stdout, d)
	}
	return emit(stdout, preToolUseReply{
		SystemMessage: message,
		HookSpecificOutput: preToolUseOutput{
			HookEventName:            event,
			PermissionDecision:       "deny",
			PermissionDecisionReason: o.verdict.Message(),
		},
	})
}

// decideInto runs the rest of the decision on the goroutine run spawned and
// leaves its one result in done.
func decideInto(ctx context.Context, done chan<- outcome, rest func(context.Context) outcome) {
	var o outcome
	defer func() {
		// A panic on this goroutine is one cmd/sdlc cannot recover: the
		// process would end with a stack and no answer. It is handed to
		// run, which raises it again where cmd/sdlc does recover.
		if r := recover(); r != nil {
			o = outcome{crashed: r, stack: debug.Stack()}
		}
		done <- o
	}()
	o = rest(ctx)
}

// awaitOutcome is whichever of the decision and the deadline comes first;
// decided is false when it was the deadline.
func awaitOutcome(done <-chan outcome, timer <-chan time.Time) (o outcome, decided bool) {
	select {
	case o = <-done:
		return o, true
	case <-timer:
		select {
		// Both were there when the hook looked. The decision answers: it is
		// the fuller answer, and a crash must never come out as a refusal.
		case o = <-done:
			return o, true
		default:
			return outcome{}, false
		}
	}
}

// decide works out whether the call is one the hook checks. Where it is, it
// returns the rest of the decision, which run then races against the deadline;
// where it is not, or there is no telling, rest is nil and the call goes
// through. shell is whether the call is a command, which a refusal for running
// out of time is worded by.
//
// Nothing here may be handed to another goroutine: until this returns, the
// hook does not know that it has anything to say to the session at all.
func decide(event string, raw []byte, getenv func(string) string, warn func(string),
	now func() time.Time) (rest func(context.Context) outcome, name string, shell bool) {
	// A human who started the session can turn enforcement off. This is read
	// from the environment, which a session cannot change from the inside.
	if getenv("SDLC_ENFORCE") == "0" {
		return nil, event, false
	}

	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		// A call that cannot be read names no session, so there is no telling
		// whether it came from the one working a story, and a session that did
		// not start one hears nothing from the plugin. It goes through, and the
		// debug log says why.
		slog.Debug("hook: the call could not be read, so it was not checked", "why", unreadableBecause(raw))
		return nil, event, false
	}
	if p.HookEventName != "" {
		event = p.HookEventName
	}

	path := p.ToolInput.FilePath
	if path == "" {
		path = p.ToolInput.NotebookPath
	}

	// Two files decide whether the loop has any business here: the project
	// takes part, and a story is being worked on. Outside those, this does
	// nothing at all.
	project, story := findLoop(getenv, p, path, warn)
	if story == "" {
		// Except a person's decisions, from any session, in a project that takes
		// part. The log of what landed on trunk is always there for a person to
		// read, so acknowledging it is always refused, and so is a command too
		// deep to read, which could hold that. A decision on a story waits for a
		// story that has one: an escalation ends the iteration, so its approval
		// always comes while no session is held, and a session the story is not
		// held in could otherwise approve it or lift its freeze. Where no story
		// has one waiting, that is refused nobody. The command is read before the
		// project's state, which is read only for a decision on a story.
		if rest := humanDecisionsOnly(getenv, p); rest != nil {
			return rest, event, true
		}
		return nil, event, false
	}

	if shellpolicy.Tools[p.ToolName] {
		return func(ctx context.Context) outcome {
			return inspectShell(ctx, placeOf(project), story, p, getenv, warn, now)
		}, event, true
	}

	return func(context.Context) outcome {
		rel, outside := pathrules.Rel(project, path)
		slog.Debug("hook considering",
			"tool", p.ToolName, "agent", p.AgentType, "role", policy.NormalizeAgent(p.AgentType),
			"path", path, "rel", rel, "outside", outside, "story", story, "project", project)

		return outcome{ok: true, verdict: policy.Evaluate(policy.Request{
			Tool:    p.ToolName,
			Agent:   p.AgentType,
			Path:    rel,
			Outside: outside && path != "",
			// Asked only of a path inside the project, and only where the
			// repository has another checkout that could be nested in it.
			OtherCheckout: !outside && nestedCheckout(placeOf(project), path, p.CWD),
			Story:         story,
			Tests:         testState(project, story, rel, warn),
			Backlog:       backlogPath(project),
		})}
	}, event, false
}

// unreadableBecause says why a call's payload could not be read, for the debug
// log.
func unreadableBecause(raw []byte) string {
	if len(raw) > maxPayload {
		return fmt.Sprintf("it is larger than the %d MiB the hook reads", maxPayload>>20)
	}
	return "it is not valid JSON"
}

// humanDecisionsOnly is the rest of the decision for a command in a project
// that takes part when the session holds no story, and nil for any other call.
func humanDecisionsOnly(getenv func(string) string, p payload) func(context.Context) outcome {
	if !shellpolicy.Tools[p.ToolName] {
		return nil
	}
	projects := loopProjects(getenv, p)
	if len(projects) == 0 {
		return nil
	}
	// A command in a project that takes part is one the hook
	// checks, whoever's session it is from.
	return func(context.Context) outcome {
		waits := storyWaits(getenv, p, projects)
		if f, refused := shellpolicy.HumanDecisions(p.ToolInput.Command, p.ToolName == toolPowerShell, waits); refused {
			return outcome{verdict: policy.Verdict{Rule: f.Rule, Reason: f.Reason, Route: f.Route}, ok: true}
		}
		return outcome{verdict: policy.Allowed}
	}
}

// backlogPath is the backlog file as the configuration names it,
// repository-relative. The name, and not where a link by that name leads: a
// backlog linked in from elsewhere resolved outside the repository, and
// protecting nothing there left `echo >> linked.json` free. The configuration
// refuses a name that itself leaves the repository. One that will not read
// leaves the backlog where the defaults put it, which is where it most likely
// is.
func backlogPath(project string) string {
	cfg, err := config.Load(project)
	if err != nil {
		cfg = config.Default()
	}
	p := cfg.Backlog.Path
	if p == "" {
		p = config.Default().Backlog.Path
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
}

// onDisk is the file a word of a shell command is on disk, repository-relative:
// through a link, and on Windows through a short name such as SDLC~1. Empty
// when it is outside the repository, where the shell rules have nothing to
// protect.
func onDisk(resolver pathrules.Resolver, word string) string {
	word, ok := expandHome(word)
	if !ok {
		return ""
	}
	// `C:.sdlc\state` is relative to a working directory on that drive, which
	// only the shell knows. Read as outside, it was nobody's to protect; read
	// without the drive, it is a name the rules look for wherever it is.
	if volume := filepath.VolumeName(word); volume != "" && !filepath.IsAbs(word) {
		return filepath.ToSlash(word[len(volume):])
	}
	rel, outside := resolver.Rel(filepath.FromSlash(word))
	if outside {
		return ""
	}
	return rel
}

// inspectShell applies the shell rules, which exist because every other rule in
// this tool governs the file-writing tools and a shell command is not one.
//
// It runs against the hook's deadline, on a goroutine run does not wait for
// once the deadline has answered. So it writes nothing down itself: the head
// an allowed commit is made on is handed back as the outcome's record, for run
// to write once this is known to be the answer. And git runs under ctx, which
// run cancels when the deadline answers.
func inspectShell(ctx context.Context, pl place, story string, p payload, getenv func(string) string,
	warn func(string), now func() time.Time) outcome {
	project := pl.project
	ready, why := commitReady(pl, story)
	slog.Debug("hook considering a command",
		"agent", p.AgentType, "story", story, "commit_ready", ready, "why", why)

	frozen, isTest, newTest := frozenTestsIn(pl, story, warn)
	var implementerTest func(string) bool
	if policy.NormalizeAgent(p.AgentType) == "implementer" {
		// A configuration that will not read is said by the rules that read it
		// first; the defaults are a better guess at what a test is than none.
		cfg, err := config.Load(project)
		if err != nil {
			cfg = config.Default()
		}
		implementerTest = testset.New(cfg.Paths.Tests).Match
	}
	// Where the command runs from. The Bash tool keeps a `cd` from one call to
	// the next, and the payload's cwd is where that left it.
	// A session in another directory altogether keeps that directory, so that a
	// commit made there is known to be another repository's.
	// Memoised, because every word of the command is placed on disk, most of
	// them down the same directories, and the disk does not change while one
	// command is judged.
	resolver := pathrules.NewResolver(project).Memoised()
	dir := ""
	if p.CWD != "" {
		switch rel, outside := resolver.Rel(p.CWD); {
		case outside:
			dir = filepath.ToSlash(p.CWD)
		case rel != ".":
			dir = rel
		}
	}
	// Created just before the parse, and started only at the first git call.
	// The cancel it returns is not kept: the budget has to outlive this
	// function for the record, and it ends with ctx, which is run's to cancel.
	// Calling that cancel from run would also read what a budget starting on
	// this goroutine writes.
	budget, _ := gitBudget(ctx, now)
	commits := false
	// Each asked at most once for the command, however many of its commands
	// ask: every one of them is a read of the disk or a run of git.
	trunk := sync.OnceValue(func() string { return trunkOf(pl) })
	resolved := map[string]bool{}
	isTrunk := func(name string) bool { //nolint:contextcheck // budget() is the command's own deadline, started on first use; it cannot be the caller's context without changing when the deadline begins
		if is, ok := resolved[name]; ok {
			return is
		}
		t := trunk()
		is, err := gitx.ResolvesTo(budget(), project, name, "refs/heads/"+t)
		is = is && err == nil && t != ""
		resolved[name] = is
		return is
	}
	fresh := sync.OnceValues(func() (bool, string) { return reviewsFresh(pl, story, warn, budget) }) //nolint:contextcheck // reviewsFresh takes the command's lazily started budget, not ctx: the deadline must begin at the first git call
	state := shellpolicy.State{
		CommitReady: ready, CommitWhy: why, Frozen: frozen, IsTest: isTest, NewTest: newTest,
		ImplementerTest: implementerTest, Dir: dir, Agent: policy.NormalizeAgent(p.AgentType),
		Backlog: backlogPath(project), PowerShell: p.ToolName == toolPowerShell,
		StoryFinished: storyFinished(pl, story),
		Committed:     storyCommitted(pl, story),
		Trunk:         trunk,
		IsTrunk:       isTrunk,
		Resolve:       func(word string) string { return onDisk(resolver, word) },
		Fresh:         fresh,
		Commits:       func() { commits = true },
		Nested:        func(dir string) bool { return nestedRepository(project, dir) },
		// What git reads from the session's environment: the hook runs with
		// the environment Claude Code runs the session's commands with.
		Carried:   getenv,
		Checkouts: func() shellpolicy.CheckoutSet { return checkoutSet(pl, resolver) },
	}
	// The repository's other checkouts are held as this one is. Where it is
	// the only one, nothing is asked and nothing changes.
	if l := newLandings(pl, resolver); l != nil {
		state.Shared = l.land
	}
	finding, refused := shellpolicy.Inspect(p.ToolInput.Command, state)
	if !refused {
		o := outcome{verdict: policy.Allowed, ok: true}
		if commits {
			o.record = func() { observe(project, story, budget) } //nolint:contextcheck // observe shares the command's budget so its git calls stay inside the same deadline
		}
		return o
	}
	return outcome{verdict: policy.Verdict{Rule: finding.Rule, Reason: finding.Reason, Route: finding.Route}, ok: true}
}

// toolPowerShell is the tool name Claude Code gives a PowerShell command, which
// the shell policy reads with PowerShell's own quoting.
const toolPowerShell = "PowerShell"

// The warnings for a configuration that will not read. Each names what is still
// enforced, because "not enforced" was the easy sentence and was not true.
const (
	unreadableConfig = ".sdlc/config.json could not be read, so tests are being recognised " +
		"by the default patterns until it can be. Run `sdlc doctor` to see why."
	unreadableFreezeAndConfig = ".sdlc/state/tests.lock could not be read, and nor could " +
		".sdlc/config.json, so every file the default patterns call a test is being treated " +
		"as frozen. Run `sdlc doctor` to see why."
	unreadableFreeze = ".sdlc/state/tests.lock could not be read, so every test file is " +
		"being treated as frozen until it can be. Run `sdlc doctor` to see why."
)

// unfoundRecordFreeze is the warning where the story's record cannot be read
// because the main checkout it is kept in cannot be found.
const unfoundRecordFreeze = "the story's record is kept in the repository's main checkout, which could " +
	"not be found, so every test file is being treated as frozen until it can be. Run `sdlc doctor` to see why."

func lostFreeze(story string) string {
	return ".sdlc/state/tests.lock does not hold the freeze " + story + "'s record says its tests " +
		"are under, so every test file is being treated as frozen until it does. Run `sdlc doctor` to see why."
}

// freezeRecorded reports whether the story's record says its tests are frozen
// and nothing has lifted that since. It is asked only when tests.lock holds no
// freeze for the story: deleting tests.lock, or writing another story's name
// or none into it, was otherwise the way to unfreeze the tests. A record that
// does not read says nothing here; the commit is refused over it anyway.
//
// found is false where the record's main checkout could not be found, and a
// freeze is then taken to stand: what cannot be read is not a freeze lifted.
func freezeRecorded(pl place, story string) (standing, found bool) {
	record, found, err := pl.record(story)
	if !found {
		return true, false
	}
	if err != nil {
		return false, true
	}
	_, standing = record.StandingFreeze()
	return standing, true
}

// frozenTests is what the freeze holds, for the shell rules to refuse writes
// to. No freeze yet means nothing frozen, which is the ordinary state before
// Gate 3 and leaves the shell as free as it was.
//
// A freeze that is there and will not read is not that. Every file the
// configuration calls a test counts as frozen until it reads, as it does for a
// file write -- otherwise corrupting tests.lock was the way to `echo` into a
// frozen test. When the configuration will not read either, the default
// patterns say what a test is, and it says so. So is a freeze that went while
// the story's record still holds it.
func frozenTests(project, story string, warn func(string)) (frozen []string, isTest, newTest func(string) bool) {
	return frozenTestsIn(placeOf(project), story, warn)
}

// frozenTestsIn is frozenTests for a project already placed among its
// repository's checkouts. The freeze is the checkout's own; the record that
// says one stands is kept in the main checkout.
func frozenTestsIn(pl place, story string, warn func(string)) (frozen []string, isTest, newTest func(string) bool) {
	project := pl.project
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "state", "tests.lock"))
	var lock model.Lock
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = nil
	case err == nil:
		err = json.Unmarshal(raw, &lock)
	}
	// A freeze belonging to another story says nothing about this one.
	gone, found := false, true
	if lock.Story != story {
		gone, found = freezeRecorded(pl, story)
	}
	if err != nil || gone {
		cfg, cfgErr := config.Load(project)
		if cfgErr != nil {
			warn(unreadableFreezeAndConfig)
			return nil, testset.New(config.Default().Paths.Tests).Match, nil
		}
		switch {
		case err != nil:
			warn(unreadableFreeze)
		case !found:
			warn(unfoundRecordFreeze)
		default:
			warn(lostFreeze(story))
		}
		return nil, testset.New(cfg.Paths.Tests).Match, nil
	}
	if lock.Story != story {
		return nil, nil, nil
	}
	// A test file the freeze does not hold is a new one, and adding one after
	// the freeze is refused unless the project allows it -- through the shell as
	// through the file tools.
	cfg, err := config.Load(project)
	if err != nil {
		warn(unreadableConfig)
		cfg = config.Default()
	}
	if !cfg.Freeze.AllowNewTestFiles {
		// Folded once: Lock.Holds folds the whole freeze for each path it is
		// not holding exactly, and the shell rules ask about every word.
		m := testset.New(cfg.Paths.Tests)
		held := make(map[string]bool, len(lock.Files))
		for path := range lock.Files {
			held[pathrules.Fold(path)] = true
		}
		newTest = func(p string) bool { return m.Match(p) && !held[pathrules.Fold(p)] }
	}
	return lock.Paths(), nil, newTest
}

// commitReady reports whether the story has been through the gates that come
// before committing, and names the first one that has not.
//
// A record that is missing or will not parse answers "not ready", and says
// which file it is. `sdlc start` always writes one, so either is damage, and
// reading damage as "ready" made deleting the record the way to commit past
// every gate -- silently. The refusal explains itself, and `sdlc stop` still
// ends an iteration whose record cannot be read, so it blocks nothing a person
// cannot see a way through.
func commitReady(pl place, story string) (bool, string) {
	rel := ".sdlc/stories/" + story + "/" + model.RecordFile
	dir, found := pl.records()
	if !found {
		return false, noMainCheckout
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".sdlc", "stories", story, model.RecordFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, rel + " is missing, so no gate can be shown to have passed " +
			"(`sdlc doctor` says more)"
	}
	if err != nil {
		return false, rel + " could not be read, so no gate can be shown to have passed " +
			"(`sdlc doctor` says why)"
	}
	var record model.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return false, rel + " is not valid JSON, so no gate can be shown to have passed " +
			"(`sdlc doctor` says more)"
	}
	for _, g := range model.GateCommit.Before() {
		if !record.Pass(g) {
			return false, string(g) + " has not passed"
		}
	}
	return true, ""
}

// storyFinished reports whether every gate on the story has passed. A record
// that is missing or will not read is not a finished story.
func storyFinished(pl place, story string) bool {
	record, _, err := pl.record(story)
	if record == nil || err != nil {
		return false
	}
	_, remaining := record.NextGate()
	return !remaining
}

// storyCommitted reports whether the story's commit gate has passed, after
// which the one rebase its session may run is onto trunk.
func storyCommitted(pl place, story string) bool {
	record, _, err := pl.record(story)
	return record != nil && err == nil && record.Pass(model.GateCommit)
}

// trunkOf is trunk's name as the main checkout's configuration has it on disk.
// A worktree's copy of the configuration is the one its branch checked out,
// and says what trunk was when the story started, or what the session wrote.
func trunkOf(pl place) string {
	dir, found := pl.records()
	if !found {
		return ""
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return ""
	}
	return cfg.Git.TrunkBranch
}

// treeTimeout bounds measuring the working tree for a commit. A repository big
// enough to take longer than this is one where the hook should say it could not
// check, not hold the session. hooks.json has to give the hook longer than this:
// Claude Code kills a hook that outlasts its timeout, and a killed hook says
// nothing -- the commit it was checking goes through unchecked.
//
// For a command it sits inside hookDeadline, which is counted from the start of
// the hook and not from the first git call. Whichever ends first is the one
// that answers: a tree still being measured when the deadline passes is a
// commit refused, and one that runs out of this first is let through with a
// warning, as it always was.
const treeTimeout = 20 * time.Second

// reviewsFresh reports whether the verifier's and the code reviewers' reviews
// are of the tree as it is now, and, for a story whose risk tier waits for a
// person, whether that person approved this tree -- the same questions
// `sdlc gate commit pass` asks, asked before the commit instead of after it.
//
// It measures the tree only when there is something to compare it with, and a
// tree it cannot measure lets the commit through with a warning: git failing
// here is git failing for the commit too. Git runs under budget, which the
// whole command shares (see gitBudget).
func reviewsFresh(pl place, story string, warn func(string), budget func() context.Context) (bool, string) {
	project := pl.project
	record, _, err := pl.record(story)
	if record == nil || err != nil {
		return true, "" // commitReady has already refused over this
	}
	reviews := stampedReviews(record)

	cfg, err := config.Load(project)
	if err != nil {
		if len(reviews) > 0 {
			slog.Debug("hook could not read the configuration to measure the tree", "err", err)
			warn(unmeasuredTree)
		}
		return true, ""
	}
	s := store.NewIn(&config.Project{Root: project, Config: cfg}, pl.co, pl.err)
	tier, pauses, err := s.CommitPause(story)
	if err != nil {
		slog.Debug("hook could not read the story's risk tier", "err", err)
		return false, "the story could not be read from the backlog, so there is no telling " +
			"whether its risk tier waits for a person's approval (`sdlc doctor` says why)"
	}
	if len(reviews) == 0 && !pauses {
		return true, ""
	}

	tree, refusal, fresh := reviewedTree(s, record, project, budget, warn)
	if !fresh {
		return refusal.allowed, refusal.why
	}
	if stale := staleReviewers(reviews, cfg, record, tree); len(stale) > 0 {
		return false, "the work has changed since " + strings.Join(stale, ", ") + " reviewed it, " +
			"so this commit is not what was approved; have the change reviewed again and record " +
			"those gates again"
	}
	if why := record.WaitsForApproval(tier, tree); pauses && why != "" {
		return false, why + "; hand it to a person with " +
			"`sdlc escalate pre_commit_approval --message \"...\"` and stop"
	}
	return true, ""
}

// stampedReview is the latest review a reviewer recorded, and the tree it
// was of.
type stampedReview struct {
	gate    model.Gate
	role    string
	subject string
}

// stampedReviews are the latest reviews recorded at the two review gates.
func stampedReviews(record *model.Record) []stampedReview {
	var reviews []stampedReview
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if latest, ok := record.LatestReview(gate, r.Role); ok {
				reviews = append(reviews, stampedReview{gate, r.Role, latest.Subject})
			}
		}
	}
	return reviews
}

// freshnessAnswer is what reviewsFresh returns where the tree could not be
// measured to a value to compare.
type freshnessAnswer struct {
	allowed bool
	why     string
}

// reviewedTree measures the tree the reviews are compared with. Where it
// cannot be measured to one, fresh is false and the answer says what
// reviewsFresh returns instead.
func reviewedTree(s *store.Store, record *model.Record, project string, budget func() context.Context,
	warn func(string)) (tree string, answer freshnessAnswer, fresh bool) {
	// Route R1: once the retro has passed, the bookkeeper's CODEMAP.md at the
	// root is committed on the story's branch, a plain file the reviews did not
	// see change. Measured as HEAD has it, that first commit is the work the
	// reviews saw, and a second, or one with any other file, is not.
	var also []gitx.Override
	if _, remaining := record.NextGate(); !remaining {
		if head, err := gitx.Head(budget(), project); err == nil && head != gitx.NoCommits {
			also = append(also, gitx.Override{Path: "CODEMAP.md", At: head})
		}
	}
	tree, err := s.ReviewSubject(budget(), also...)
	if errors.Is(err, gitx.ErrOverrideNotAFile) {
		// Files beneath CODEMAP.md are a shape the session chose, not a tree
		// that could not be measured: answering fresh there let R1's one
		// commit carry anything under that name.
		return "", freshnessAnswer{false, "CODEMAP.md at the root holds files beneath it, so it is not the bookkeeper's plain file " +
			"and this commit is not what was reviewed; after the retro only CODEMAP.md, as a plain file, is " +
			"committed on its own"}, false
	}
	if err != nil && len(also) > 0 {
		// The override could not be applied where the tree measures without
		// it: the path holds something git will not take in its place, which
		// is an answer about the work, and not one that fails open.
		if _, plain := s.ReviewSubject(budget()); plain == nil {
			slog.Debug("hook could not measure the tree with CODEMAP.md as HEAD has it", "err", err)
			return "", freshnessAnswer{false, "CODEMAP.md at the root could not be measured as HEAD has it, so this commit is not " +
				"what was reviewed; after the retro only CODEMAP.md, as a plain file, is committed on its own"}, false
		}
	}
	if err != nil {
		slog.Debug("hook could not measure the working tree", "err", err)
		warn(unmeasuredTree)
		return "", freshnessAnswer{true, ""}, false
	}
	return tree, freshnessAnswer{}, true
}

// staleReviewers names the reviewers whose latest review is of another tree.
func staleReviewers(reviews []stampedReview, cfg config.Config, record *model.Record, tree string) []string {
	var stale []string
	for _, r := range reviews {
		// A reviewer the staffing left out, that no changed path brought back
		// when code review passed, is one the gate did not wait for: its old
		// review of a tree since changed holds nothing. The record says who was
		// brought back, because the hook makes no git call to work it out.
		if cfg.Staffing.Enabled && record.StoodDown(r.role) {
			continue
		}
		if r.subject != tree {
			stale = append(stale, r.role+" ("+string(r.gate)+")")
		}
	}
	return stale
}

// unmeasuredTree is the warning for a commit the hook could not check.
const unmeasuredTree = "the working tree could not be measured, so this commit was not checked " +
	"against what was reviewed and approved. `sdlc gate commit pass` will check it afterwards."

// testState works out what the freeze says about this path.
//
// A configuration that will not parse answers "not a test". The freeze is one
// rule among several, and a project whose configuration is broken should still
// have its protected paths and its separation of duties enforced. A freeze
// that will not parse is different, and is handled below.
func testState(project, story, rel string, warn func(string)) policy.Tests {
	if rel == "" {
		return policy.Tests{}
	}
	cfg, err := config.Load(project)
	if err != nil {
		// Reading no file as a test turned off every rule that asks what a test
		// is: the implementer could write them, and a frozen one could be
		// edited. The freeze names its own files, and the defaults are a better
		// guess at the rest than nothing.
		slog.Debug("hook could not read the configuration", "err", err)
		warn(unreadableConfig)
		cfg = config.Default()
	}
	m := testset.New(cfg.Paths.Tests)
	t := policy.Tests{IsTest: m.Match(rel), AllowNew: cfg.Freeze.AllowNewTestFiles}

	lock, err := store.New(&config.Project{Root: project, Config: cfg}).Lock()
	if err != nil {
		// A freeze that is there and cannot be read is not an absent freeze.
		// Reading it as absent made corrupting tests.lock the way to edit a
		// frozen test, with nothing said. Until it reads again, every test
		// is frozen.
		warn(unreadableFreeze)
		t.Frozen, t.Locked = true, t.IsTest
		return t
	}
	if lock == nil || lock.Story != story {
		// Nor is one that went, or that was rewritten to name another story or
		// none, while the story's record still holds it.
		if standing, found := freezeRecorded(placeOf(project), story); standing {
			if found {
				warn(lostFreeze(story))
			} else {
				warn(unfoundRecordFreeze)
			}
			t.Frozen, t.Locked = true, t.IsTest
		}
		return t
	}
	t.Frozen, t.Locked = true, lock.Holds(rel)
	return t
}

// heldHere reports whether this call comes from the Claude Code session that
// started or resumed the story in project, or from one of its agents.
//
// A story is held in the session working it, and nowhere else. The state that
// says a story is under way is the repository's, and every session opened in
// the repository read it: somebody doing ordinary work there while a story was
// part-way through could not write a line of code, commit, or end a turn.
// `sdlc start` records the session it runs in, and a subagent's calls carry the
// session that spawned it, so the session running the story and its agents
// are held, and nobody else is.
//
// Where no session is recorded, the call names none, or the file names nothing
// to compare with, the story holds no session and nothing is said. The plugin
// is installed for every session, and one that never started the story in
// front of it was refused edits, sent back at the end of its turn and told
// about state it did not make: a tool acting where nobody asked it to. A story
// started from a terminal is picked up in a session by /sdlc:next, which
// records that session.
func heldHere(project, session string) bool {
	if session == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "state", "session"))
	if err != nil {
		return false
	}
	owner := strings.TrimSpace(string(raw))
	return store.CheckSession(owner) && owner == session
}

// activeStory reads the story being worked on, directly rather than through the
// store: this runs on every tool call, and parsing the configuration to learn
// something that is not in it would be work for nothing.
//
// findLoop reads it only for the session working a story, so a warning here
// reaches that session and no other.
func activeStory(project string, warn func(string)) string {
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "state", "active"))
	if err != nil {
		// Not being there is the ordinary case: no iteration is running.
		// Being there and unreadable turns every rule below off, and looks
		// identical from the outside.
		if !errors.Is(err, fs.ErrNotExist) {
			warn(".sdlc/state/active could not be read, so nothing is being " +
				"enforced in this session. Run `sdlc doctor` to see why.")
		}
		return ""
	}
	id := strings.TrimSpace(string(raw))
	// The CLI checks this when it writes the file, and never writes it empty:
	// ending an iteration removes it. Checking it again here is cheap, and this
	// is the one place the value becomes part of a path. It is the same check,
	// so that no id is accepted by one and refused by the other. A value that
	// fails it -- an empty file included -- turns every rule off, so it says so.
	if store.CheckID(id) != nil {
		warn(".sdlc/state/active does not name a story, so nothing is being " +
			"enforced in this session. Run `sdlc doctor` to see why.")
		return ""
	}
	return id
}

// findLoop finds the loop project a tool call acts in, and the story being
// worked on there: the first of Claude Code's project directory, the directory
// the call runs in, and the file it writes that is in a project with a story
// running.
//
// The project directory alone is not enough. A session opened above the
// repository, or one that added it as another directory, has a project
// directory the loop is not in, and every rule was off for every file in the
// loop project -- while `sdlc`, run from inside it, carried on with the story.
func findLoop(getenv func(string) string, p payload, target string, warn func(string)) (project, story string) {
	if target != "" && p.CWD != "" {
		if abs, ok := pathrules.Abs(p.CWD, target); ok {
			target = abs
		} else {
			target = ""
		}
	}
	walked, ceilings := map[string]bool{}, config.CeilingDirectories(getenv)
	var reached []string
	for _, start := range starts(getenv, p, target) {
		root, ok := projectRoot(start, walked, ceilings)
		if !ok {
			continue
		}
		reached = append(reached, root)
		// The session first, so that a session the story is not held in hears
		// nothing about the state of a story that is not its own.
		if !heldHere(root, p.SessionID) {
			slog.Debug("hook: no story here is this session's", "project", root)
			continue
		}
		if story := activeStory(root, warn); story != "" {
			return root, story
		}
	}
	return heldElsewhere(reached, p.SessionID)
}

// heldElsewhere is the checkout of a reached project's repository the session
// holds a story in, and that story, where it is not a checkout the call
// reached. A session held in a linked worktree that worked from the main
// checkout, or named its files, was judged where it held nothing, and every
// rule was off for it there.
//
// Every checkout a claim names is asked, and then as many linked worktrees as
// store.SessionScanLimit allows, for an iteration nothing has claimed yet. A
// repository with no other checkout asks nothing.
func heldElsewhere(reached []string, session string) (project, story string) {
	if !store.CheckSession(session) {
		return "", ""
	}
	for _, root := range reached {
		pl := placeOf(root)
		if !pl.co.Pointer() && !pl.co.HasWorktrees() {
			continue
		}
		cfg, err := config.Load(root)
		if err != nil {
			cfg = config.Default()
		}
		s := store.NewIn(&config.Project{Root: root, Config: cfg}, pl.co, pl.err)
		if checkout, id := s.SessionHolder(session, store.SessionScanLimit); checkout != "" {
			slog.Debug("hook: the session holds a story in another checkout", "checkout", checkout, "story", id)
			return checkout, id
		}
	}
	return "", ""
}

// loopProjects is every project that takes part in the loop the call is in or
// names, whether or not a story is being worked on there.
func loopProjects(getenv func(string) string, p payload) []string {
	var out []string
	walked, ceilings := map[string]bool{}, config.CeilingDirectories(getenv)
	for _, start := range starts(getenv, p, "") {
		if root, ok := projectRoot(start, walked, ceilings); ok {
			out = append(out, root)
		}
	}
	return out
}

// storyWaits is the question HumanDecisions asks of a decision on a story:
// whether a story waits for a person in a project the call reaches, or in the
// one the command has moved to, dir, relative to where the call runs. Each
// project's state is read once at most, and only for a decision on a story.
func storyWaits(getenv func(string) string, p payload, reached []string) func(dir string) bool {
	known := map[string]bool{}
	waits := func(root string) bool {
		w, seen := known[root]
		if !seen {
			w = awaitsDecision(placeOf(root))
			known[root] = w
		}
		return w
	}
	return func(dir string) bool {
		if slices.ContainsFunc(reached, waits) {
			return true
		}
		if dir == "." {
			return false
		}
		// A move from a directory the call does not name leads nowhere known.
		abs, ok := pathrules.Abs(p.CWD, dir)
		if p.CWD == "" || !ok {
			return true
		}
		root, found := projectRoot(abs, map[string]bool{}, config.CeilingDirectories(getenv))
		return found && waits(root)
	}
}

// awaitsDecision reports whether a story in project has a decision waiting for a
// person: a story is under way, whose freeze is the person's to lift, or a story
// was handed to a person and nobody has answered. These are the states in which
// `sdlc unfreeze` and `sdlc approve` do anything. A file that will not read says
// nothing is waiting, as it does to those commands.
//
// The records are the main checkout's, and where it cannot be found, a
// decision is taken to be waiting: an escalation ends the iteration, so the
// record is all that says one is, and not reading it was approving.
func awaitsDecision(pl place) bool {
	if raw, err := os.ReadFile(filepath.Join(pl.project, ".sdlc", "state", "active")); err == nil &&
		store.CheckID(strings.TrimSpace(string(raw))) == nil {
		return true
	}
	shared, found := pl.records()
	if !found {
		return true
	}
	sdlc := filepath.Join(shared, ".sdlc")
	stories, err := os.ReadDir(filepath.Join(sdlc, "stories"))
	if err != nil {
		return false
	}
	// Not only directories: a story directory linked in from elsewhere is read
	// through the link, as the store reads it.
	for _, story := range stories {
		raw, err := os.ReadFile(filepath.Join(sdlc, "stories", story.Name(), model.RecordFile))
		if err != nil {
			continue
		}
		var record model.Record
		if json.Unmarshal(raw, &record) != nil {
			continue
		}
		if _, waiting := record.PendingEscalation(); waiting {
			return true
		}
	}
	return false
}

// starts is where to look for the loop project: the session's project
// directory, where the call runs, the file a file tool writes, and, for a
// command, every path it names. A command has no file of its own, so from a
// session opened above the repository `rm repo/internal/invoice_test.go`,
// `git -C repo commit` and `cd repo && sdlc unfreeze` were in no project at
// all, and every shell rule was off without a word.
func starts(getenv func(string) string, p payload, target string) []string {
	out := []string{getenv("CLAUDE_PROJECT_DIR"), p.CWD, target}
	if shellpolicy.Tools[p.ToolName] && p.CWD != "" {
		for _, word := range shellpolicy.Places(p.ToolInput.Command, p.ToolName == toolPowerShell) {
			if abs, ok := pathrules.Abs(p.CWD, word); ok {
				out = append(out, abs)
			}
		}
	}
	return slices.DeleteFunc(out, func(s string) bool { return s == "" })
}

// projectRoot finds the directory holding `.sdlc/config.json`, starting at the
// given directory and walking up.
//
// Walking up is the whole point. A session started anywhere below the
// repository root -- `cd backend && claude`, a workspace whose folder is a
// subdirectory, anything at all -- reports that directory, and looking for the
// configuration only there found nothing and turned every rule off without
// saying so. A write to `.sdlc/config.json` was refused from the root and
// allowed from one directory down.
//
// The walk stops at the repository, so a project that does not take part never
// picks up the configuration of one further up the filesystem. It stops where
// GIT_CEILING_DIRECTORIES stops git, too: sdlc, run there, says it is not in a
// repository, and the rules of a project it cannot see do not apply either.
//
// walked holds the directories earlier walks went through. A walk that reaches
// one ends there, with the answer that walk already gave, so a command naming
// thousands of paths in one tree climbs it once.
func projectRoot(start string, walked map[string]bool, ceilings []string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if walked[dir] {
			return "", false
		}
		walked[dir] = true
		if exists(filepath.Join(dir, ".sdlc", "config.json")) {
			return dir, true
		}
		// A repository boundary is as far as this goes: outside it, whatever
		// is above belongs to somebody else.
		if exists(filepath.Join(dir, ".git")) {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir || atCeiling(parent, ceilings) {
			return "", false
		}
		dir = parent
	}
}

// atCeiling reports whether GIT_CEILING_DIRECTORIES names dir, which git does not
// look in from below it. Git compares the entries with the directory resolved,
// and so does this: a path through a link, as /tmp is on macOS, matches an
// entry naming where the link leads, and never one naming the link itself,
// which an entry after an empty one is left as.
func atCeiling(dir string, ceilings []string) bool {
	if len(ceilings) == 0 {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return slices.Contains(ceilings, dir)
}

func exists(path string) bool {
	_, err := os.Lstat(path) //nolint:gosec // G703: the path comes from the session, and this only asks whether it is there; nothing is read or written
	return err == nil
}

func emit(stdout io.Writer, v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		// Unreachable for these types, but a hook must always produce valid
		// JSON: a parse error on the other side is worse than a denial.
		fmt.Fprint(stdout, `{"continue":true}`)
		return 0
	}
	fmt.Fprintln(stdout, string(b))
	return 0
}
