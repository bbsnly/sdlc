// Package hook is the fast path.
//
// Claude Code runs a hook on every matching tool call, so this code is on the
// latency path of the user's whole session. Nothing here builds a command tree,
// parses configuration it does not need, or touches the network. The rest of
// the CLI is reached only when the first argument is not `hook`.
//
// It speaks only in the Claude Code session working a story. The plugin is
// installed for every session, and in any other one the hook refuses nothing
// but a person's decisions, holds no turn open, and says nothing at all.
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
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
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
	var warnings []string
	warn := func(msg string) {
		for _, w := range warnings {
			if w == msg {
				return
			}
		}
		warnings = append(warnings, msg)
		fmt.Fprintln(stderr, "sdlc: "+msg)
	}

	if args[0] == "Stop" || args[0] == "PostToolUse" {
		var reply turnReply
		if args[0] == "Stop" {
			reply = decideStop(raw, getenv, warn)
		} else {
			reply = formatWritten(raw, getenv, warn)
		}
		slog.Debug("hook turn decision", "event", args[0], "decision", reply.Decision)
		if len(warnings) > 0 {
			reply.SystemMessage = strings.TrimSpace(reply.SystemMessage + " sdlc: " + strings.Join(warnings, " "))
		}
		return emit(stdout, reply)
	}

	verdict, event, ok := decide(args[0], raw, getenv, warn)
	// Why a hook did nothing is the hardest thing to find out from the outside,
	// so every decision is available at debug level. SDLC_DEBUG_FILE is the way
	// to see it: stderr from a hook that allowed the call is not shown.
	slog.Debug("hook decision",
		"event", event, "considered", ok, "allowed", verdict.Allowed, "rule", verdict.Rule)
	message := ""
	if len(warnings) > 0 {
		message = "sdlc: " + strings.Join(warnings, " ")
	}
	if !ok || verdict.Allowed {
		d := Allow()
		d.SystemMessage = message
		return emit(stdout, d)
	}
	return emit(stdout, preToolUseReply{
		SystemMessage: message,
		HookSpecificOutput: preToolUseOutput{
			HookEventName:            event,
			PermissionDecision:       "deny",
			PermissionDecisionReason: verdict.Message(),
		},
	})
}

// decide works out whether this event should be refused. ok is false whenever
// the answer cannot be reached at all, which is treated exactly like an allow.
func decide(event string, raw []byte, getenv func(string) string, warn func(string)) (policy.Verdict, string, bool) {
	// A human who started the session can turn enforcement off. This is read
	// from the environment, which a session cannot change from the inside.
	if getenv("SDLC_ENFORCE") == "0" {
		return policy.Allowed, event, false
	}

	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		// A call that cannot be read names no session, so there is no telling
		// whether it came from the one working a story, and a session that did
		// not start one hears nothing from the plugin. It goes through, and the
		// debug log says why.
		why := "it is not valid JSON"
		if len(raw) > maxPayload {
			why = fmt.Sprintf("it is larger than the %d MiB the hook reads", maxPayload>>20)
		}
		slog.Debug("hook: the call could not be read, so it was not checked", "why", why)
		return policy.Allowed, event, false
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
		// Except the decisions that are a person's, in a project that takes
		// part: an escalation ends the iteration, so its approval always came
		// while this allowed everything.
		if shellpolicy.Tools[p.ToolName] && inLoop(getenv, p) {
			if f, refused := shellpolicy.HumanDecisions(p.ToolInput.Command, p.ToolName == "PowerShell"); refused {
				return policy.Verdict{Rule: f.Rule, Reason: f.Reason, Route: f.Route}, event, true
			}
		}
		return policy.Allowed, event, false
	}

	if shellpolicy.Tools[p.ToolName] {
		return inspectShell(project, story, p, warn), event, true
	}

	rel, outside := pathrules.Rel(project, path)
	slog.Debug("hook considering",
		"tool", p.ToolName, "agent", p.AgentType, "role", policy.NormalizeAgent(p.AgentType),
		"path", path, "rel", rel, "outside", outside, "story", story, "project", project)

	return policy.Evaluate(policy.Request{
		Tool:    p.ToolName,
		Agent:   p.AgentType,
		Path:    rel,
		Outside: outside && path != "",
		Story:   story,
		Tests:   testState(project, story, rel, warn),
		Backlog: backlogPath(project),
	}), event, true
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
	// The shell expands ~ before the command sees it. Read as a directory
	// called ~ in the repository, the user's own ~/.claude was the project's.
	if rest, ok := strings.CutPrefix(word, "~"); ok && (rest == "" || rest[0] == '/') {
		home, err := os.UserHomeDir()
		if err != nil {
			// Somebody's home, even when HOME is not set to say where.
			return ""
		}
		word = filepath.ToSlash(home) + rest
	} else if name, after, named := strings.Cut(rest, "/"); ok && named {
		// `~someone/.claude` is someone's home, not the project's; but the
		// someone can be the user running the session, with the project in it.
		u, err := user.Lookup(name)
		if err != nil || u.HomeDir == "" {
			return ""
		}
		word = filepath.ToSlash(u.HomeDir) + "/" + after
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
func inspectShell(project, story string, p payload, warn func(string)) policy.Verdict {
	ready, why := commitReady(project, story)
	slog.Debug("hook considering a command",
		"agent", p.AgentType, "story", story, "commit_ready", ready, "why", why)

	frozen, isTest, newTest := frozenTests(project, story, warn)
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
	resolver := pathrules.NewResolver(project)
	dir := ""
	if p.CWD != "" {
		switch rel, outside := resolver.Rel(p.CWD); {
		case outside:
			dir = filepath.ToSlash(p.CWD)
		case rel != ".":
			dir = rel
		}
	}
	finding, refused := shellpolicy.Inspect(p.ToolInput.Command, shellpolicy.State{
		CommitReady: ready, CommitWhy: why, Frozen: frozen, IsTest: isTest, NewTest: newTest,
		ImplementerTest: implementerTest, Dir: dir, Agent: policy.NormalizeAgent(p.AgentType),
		Backlog: backlogPath(project), PowerShell: p.ToolName == "PowerShell",
		StoryFinished: storyFinished(project, story),
		Resolve:       func(word string) string { return onDisk(resolver, word) },
		Fresh:         func() (bool, string) { return reviewsFresh(project, story, warn) },
	})
	if !refused {
		return policy.Allowed
	}
	return policy.Verdict{Rule: finding.Rule, Reason: finding.Reason, Route: finding.Route}
}

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

func lostFreeze(story string) string {
	return ".sdlc/state/tests.lock does not hold the freeze " + story + "'s record says its tests " +
		"are under, so every test file is being treated as frozen until it does. Run `sdlc doctor` to see why."
}

// freezeRecorded reports whether the story's record says its tests are frozen
// and nothing has lifted that since. It is asked only when tests.lock holds no
// freeze for the story: deleting tests.lock, or writing another story's name
// or none into it, was otherwise the way to unfreeze the tests. A record that
// does not read says nothing here; the commit is refused over it anyway.
func freezeRecorded(project, story string) bool {
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "stories", story, model.RecordFile))
	if err != nil {
		return false
	}
	var record model.Record
	if json.Unmarshal(raw, &record) != nil {
		return false
	}
	_, standing := record.StandingFreeze()
	return standing
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
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "state", "tests.lock"))
	var lock model.Lock
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = nil
	case err == nil:
		err = json.Unmarshal(raw, &lock)
	}
	// A freeze belonging to another story says nothing about this one.
	if gone := lock.Story != story && freezeRecorded(project, story); err != nil || gone {
		cfg, cfgErr := config.Load(project)
		if cfgErr != nil {
			warn(unreadableFreezeAndConfig)
			return nil, testset.New(config.Default().Paths.Tests).Match, nil
		}
		if err != nil {
			warn(unreadableFreeze)
		} else {
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
func commitReady(project, story string) (bool, string) {
	rel := ".sdlc/stories/" + story + "/" + model.RecordFile
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "stories", story, model.RecordFile))
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
func storyFinished(project, story string) bool {
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "stories", story, model.RecordFile))
	if err != nil {
		return false
	}
	var record model.Record
	if json.Unmarshal(raw, &record) != nil {
		return false
	}
	_, remaining := record.NextGate()
	return !remaining
}

// treeTimeout bounds measuring the working tree for a commit. A repository big
// enough to take longer than this is one where the hook should say it could not
// check, not hold the session. hooks.json has to give the hook longer than this:
// Claude Code kills a hook that outlasts its timeout, and a killed hook says
// nothing -- the commit it was checking goes through unchecked.
const treeTimeout = 20 * time.Second

// reviewsFresh reports whether the verifier's and the code reviewers' reviews
// are of the tree as it is now, and, for a story whose risk tier waits for a
// person, whether that person approved this tree -- the same questions
// `sdlc gate commit pass` asks, asked before the commit instead of after it.
//
// It measures the tree only when there is something to compare it with, and a
// tree it cannot measure lets the commit through with a warning: git failing
// here is git failing for the commit too.
func reviewsFresh(project, story string, warn func(string)) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "stories", story, model.RecordFile))
	if err != nil {
		return true, "" // commitReady has already refused over this
	}
	var record model.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return true, ""
	}
	type stamped struct {
		gate    model.Gate
		role    string
		subject string
	}
	var reviews []stamped
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if latest, ok := record.LatestReview(gate, r.Role); ok {
				reviews = append(reviews, stamped{gate, r.Role, latest.Subject})
			}
		}
	}

	cfg, err := config.Load(project)
	if err != nil {
		if len(reviews) > 0 {
			slog.Debug("hook could not read the configuration to measure the tree", "err", err)
			warn(unmeasuredTree)
		}
		return true, ""
	}
	s := store.New(&config.Project{Root: project, Config: cfg})
	tier, pauses, err := s.CommitPause(story)
	if err != nil {
		slog.Debug("hook could not read the story's risk tier", "err", err)
		return false, "the story could not be read from the backlog, so there is no telling " +
			"whether its risk tier waits for a person's approval (`sdlc doctor` says why)"
	}
	if len(reviews) == 0 && !pauses {
		return true, ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), treeTimeout)
	defer cancel()
	tree, err := s.ReviewSubject(ctx)
	if err != nil {
		slog.Debug("hook could not measure the working tree", "err", err)
		warn(unmeasuredTree)
		return true, ""
	}
	var stale []string
	for _, r := range reviews {
		if r.subject != tree {
			stale = append(stale, r.role+" ("+string(r.gate)+")")
		}
	}
	if len(stale) > 0 {
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
		if freezeRecorded(project, story) {
			warn(lostFreeze(story))
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
	for _, start := range starts(getenv, p, target) {
		root, ok := projectRoot(start, walked, ceilings)
		if !ok {
			continue
		}
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
	return "", ""
}

// inLoop reports whether the session is in a project that takes part in the
// loop, whether or not a story is being worked on.
func inLoop(getenv func(string) string, p payload) bool {
	walked, ceilings := map[string]bool{}, config.CeilingDirectories(getenv)
	for _, start := range starts(getenv, p, "") {
		if _, ok := projectRoot(start, walked, ceilings); ok {
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
		for _, word := range shellpolicy.Places(p.ToolInput.Command, p.ToolName == "PowerShell") {
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
	_, err := os.Lstat(path)
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
