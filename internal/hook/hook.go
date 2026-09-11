// Package hook is the fast path.
//
// Claude Code runs a hook on every matching tool call, so this code is on the
// latency path of the user's whole session. Nothing here builds a command tree,
// parses configuration it does not need, or touches the network. The rest of
// the CLI is reached only when the first argument is not `hook`.
//
// It fails open. Every error -- an unreadable payload, a missing file, a path
// that cannot be resolved -- ends in "carry on". A hook that blocks a session
// because of its own bug is worse than the mistake it was trying to prevent,
// and the loop's real guarantee is the gate record, which a wrong write cannot
// forge.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
func Run(args []string, stdin io.Reader, stdout io.Writer, getenv func(string) string) int {
	// The payload is always drained, even when the answer is already known:
	// leaving it unread can give the caller a broken pipe.
	raw, _ := io.ReadAll(io.LimitReader(stdin, maxPayload))
	if len(args) == 0 {
		return emit(stdout, Allow())
	}

	verdict, event, ok := decide(args[0], raw, getenv)
	// Why a hook did nothing is the hardest thing to find out from the outside,
	// so every decision is available at debug level. SDLC_DEBUG_FILE is the way
	// to see it: a hook's stderr is often invisible.
	slog.Debug("hook decision",
		"event", event, "considered", ok, "allowed", verdict.Allowed, "rule", verdict.Rule)
	if !ok || verdict.Allowed {
		return emit(stdout, Allow())
	}
	return emit(stdout, preToolUseReply{HookSpecificOutput: preToolUseOutput{
		HookEventName:            event,
		PermissionDecision:       "deny",
		PermissionDecisionReason: verdict.Message(),
	}})
}

// decide works out whether this event should be refused. ok is false whenever
// the answer cannot be reached at all, which is treated exactly like an allow.
func decide(event string, raw []byte, getenv func(string) string) (policy.Verdict, string, bool) {
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
	if !exists(filepath.Join(project, ".sdlc", "config.json")) {
		return policy.Allowed, event, false
	}
	story := activeStory(project)
	if story == "" {
		return policy.Allowed, event, false
	}

	if p.ToolName == "Bash" {
		return inspectShell(project, story, p), event, true
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
		Tests:   testState(project, story, rel),
	}), event, true
}

// inspectShell applies the shell rules, which exist because every other rule in
// this tool governs the file-writing tools and a shell command is not one.
func inspectShell(project, story string, p payload) policy.Verdict {
	ready, why := commitReady(project, story)
	slog.Debug("hook considering a command",
		"agent", p.AgentType, "story", story, "commit_ready", ready, "why", why)

	finding, refused := shellpolicy.Inspect(p.ToolInput.Command,
		shellpolicy.State{CommitReady: ready, CommitWhy: why})
	if !refused {
		return policy.Allowed
	}
	return policy.Verdict{Rule: finding.Rule, Reason: finding.Reason, Route: finding.Route}
}

// commitReady reports whether the story has been through the gates that come
// before committing, and names the first one that has not.
//
// Anything unreadable answers "ready". A story whose record cannot be read is
// not a story this hook should stand in front of a commit for: the loop would
// be blocking work it cannot explain, which is the failure mode that teaches
// people to switch a tool off.
func commitReady(project, story string) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "stories", story, model.RecordFile))
	if err != nil {
		return true, ""
	}
	var record model.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return true, ""
	}
	for _, g := range model.GateCommit.Before() {
		if !record.Pass(g) {
			return false, string(g) + " has not passed"
		}
	}
	return true, ""
}

// testState works out what the freeze says about this path.
//
// Anything unreadable answers "not a test". The freeze is one rule among
// several, and a project whose configuration will not parse should still have
// its protected paths and its separation of duties enforced -- doctor is where
// a broken configuration gets reported, not here.
func testState(project, story, rel string) policy.Tests {
	if rel == "" {
		return policy.Tests{}
	}
	cfg, err := config.Load(project)
	if err != nil {
		slog.Debug("hook could not read the configuration", "err", err)
		return policy.Tests{}
	}
	m := testset.New(cfg.Paths.Tests)
	t := policy.Tests{IsTest: m.Match(rel), AllowNew: cfg.Freeze.AllowNewTestFiles}

	lock, err := store.New(&config.Project{Root: project, Config: cfg}).Lock()
	if err != nil || lock == nil || lock.Story != story {
		return t
	}
	t.Frozen, t.Locked = true, lock.Holds(rel)
	return t
}

// activeStory reads the story being worked on, directly rather than through the
// store: this runs on every tool call, and parsing the configuration to learn
// something that is not in it would be work for nothing.
func activeStory(project string) string {
	raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "state", "active"))
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(raw))
	// The CLI checks this when it writes the file. Checking it again here is
	// cheap, and this is the one place the value becomes part of a path.
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return ""
	}
	return id
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
