// Package hook is the fast path.
//
// Claude Code runs a hook on every matching tool call, so this code is on the
// latency path of the user's whole session. Nothing here builds a command tree,
// parses configuration it does not need, or touches the network. The rest of
// the CLI is reached only when the first argument is not `hook`.
//
// It fails open. An error -- an unreadable payload, a missing configuration, a
// path that cannot be resolved -- ends in "carry on", and says so. A hook that
// blocks a session because of its own bug is worse than the mistake it was
// trying to prevent.
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
	"path/filepath"
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

// maxPayload bounds the read. A hook that reads forever hangs the session.
const maxPayload = 1 << 20

// Decision is the generic reply: keep going, or stop with a reason. It is what
// a hook says when it has nothing specific to add, and what the crash handler
// falls back to.
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
	CWD           string `json:"cwd"`
	ToolInput     struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	} `json:"tool_input"`
}

// Run dispatches a hook event. args is everything after the `hook` verb, so
// args[0] is the event name.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	// The payload is always drained, even when the answer is already known:
	// leaving it unread can give the caller a broken pipe.
	raw, _ := io.ReadAll(io.LimitReader(stdin, maxPayload))
	if len(args) == 0 {
		return emit(stdout, Allow())
	}

	// Failing open is the right answer and being quiet about it is not. A hook
	// that has decided to enforce nothing looks exactly like a hook with
	// nothing to enforce, and the session then reports a freeze that is not
	// there.
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
		return policy.Allowed, event, false
	}
	if p.HookEventName != "" {
		event = p.HookEventName
	}

	project := getenv("CLAUDE_PROJECT_DIR")
	if project == "" {
		project = p.CWD
	}
	if project == "" {
		return policy.Allowed, event, false
	}

	// Two files decide whether the loop has any business here: the project
	// takes part, and a story is being worked on. Outside those, this does
	// nothing at all.
	project, ok := projectRoot(project)
	if !ok {
		return policy.Allowed, event, false
	}
	story := activeStory(project, warn)
	if story == "" {
		return policy.Allowed, event, false
	}

	if p.ToolName == "Bash" {
		return inspectShell(project, story, p, warn), event, true
	}

	path := p.ToolInput.FilePath
	if path == "" {
		path = p.ToolInput.NotebookPath
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
	}), event, true
}

// inspectShell applies the shell rules, which exist because every other rule in
// this tool governs the file-writing tools and a shell command is not one.
func inspectShell(project, story string, p payload, warn func(string)) policy.Verdict {
	ready, why := commitReady(project, story)
	slog.Debug("hook considering a command",
		"agent", p.AgentType, "story", story, "commit_ready", ready, "why", why)

	frozen, isTest, newTest := frozenTests(project, story, warn)
	finding, refused := shellpolicy.Inspect(p.ToolInput.Command, shellpolicy.State{
		CommitReady: ready, CommitWhy: why, Frozen: frozen, IsTest: isTest, NewTest: newTest,
		Fresh: func() (bool, string) { return reviewsFresh(project, story, warn) },
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
)

// frozenTests is what the freeze holds, for the shell rules to refuse writes
// to. No freeze yet means nothing frozen, which is the ordinary state before
// Gate 3 and leaves the shell as free as it was.
//
// A freeze that is there and will not read is not that. Every file the
// configuration calls a test counts as frozen until it reads, as it does for a
// file write -- otherwise corrupting tests.lock was the way to `echo` into a
// frozen test. When the configuration will not read either, the default
// patterns say what a test is, and it says so.
func frozenTests(project, story string, warn func(string)) (frozen []string, isTest, newTest func(string) bool) {
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "state", "tests.lock"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	var lock model.Lock
	if err == nil {
		err = json.Unmarshal(raw, &lock)
	}
	if err != nil {
		cfg, cfgErr := config.Load(project)
		if cfgErr != nil {
			warn(unreadableFreezeAndConfig)
			return nil, testset.New(config.Default().Paths.Tests).Match, nil
		}
		warn(".sdlc/state/tests.lock could not be read, so every test file is " +
			"being treated as frozen until it can be. Run `sdlc doctor` to see why.")
		return nil, testset.New(cfg.Paths.Tests).Match, nil
	}
	// A freeze belonging to another story says nothing about this one.
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
		m := testset.New(cfg.Paths.Tests)
		newTest = func(p string) bool { return m.Match(p) && !lock.Holds(p) }
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

// treeTimeout bounds measuring the working tree for a commit. A repository big
// enough to take longer than this is one where the hook should say it could not
// check, not hold the session.
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
		warn(".sdlc/state/tests.lock could not be read, so every test file is " +
			"being treated as frozen until it can be. Run `sdlc doctor` to see why.")
		t.Frozen, t.Locked = true, t.IsTest
		return t
	}
	if lock == nil || lock.Story != story {
		return t
	}
	t.Frozen, t.Locked = true, lock.Holds(rel)
	return t
}

// activeStory reads the story being worked on, directly rather than through the
// store: this runs on every tool call, and parsing the configuration to learn
// something that is not in it would be work for nothing.
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

// projectRoot finds the directory holding `.sdlc/config.json`, starting at the
// session's own directory and walking up.
//
// Walking up is the whole point. A session started anywhere below the
// repository root -- `cd backend && claude`, a workspace whose folder is a
// subdirectory, anything at all -- reports that directory, and looking for the
// configuration only there found nothing and turned every rule off without
// saying so. A write to `.sdlc/config.json` was refused from the root and
// allowed from one directory down.
//
// The walk stops at the repository, so a project that does not take part never
// picks up the configuration of one further up the filesystem.
func projectRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if exists(filepath.Join(dir, ".sdlc", "config.json")) {
			return dir, true
		}
		// A repository boundary is as far as this goes: outside it, whatever
		// is above belongs to somebody else.
		if exists(filepath.Join(dir, ".git")) {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
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
