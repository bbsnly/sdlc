package main

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
)

// The classes the report groups its names in, so that the eye finds one among
// twenty-eight rows: the hook events that decide and the three that record
// (SubagentStop at four times SessionStart), the short reads and the commands
// that work a story's documents (review add at eight times staffing), start
// and stop against gate commit (at five times stop), and the launcher's path.
// Nothing is derived from the class.
const (
	classHook      = "hook"
	classRecording = "recording"
	classCommand   = "command"
	classStory     = "story"
	classLanding   = "landing"
	classCommit    = "commit"
	classLauncher  = "launcher"
)

// The kinds of sandbox a name runs on: a project with sdlc initialised and
// nothing started; a story started and taken through every gate before the
// one named; and a story stopped after its implementation.
const (
	kindInitialised = "initialised"
	kindStarted     = "started:"
	kindStopped     = "stopped"
)

func startedAt(g model.Gate) string { return kindStarted + string(g) }

// The payload keys more than one event's payload carries.
const (
	windows        = "windows"
	implementer    = "sdlc:implementer"
	keyAgentType   = "agent_type"
	keyDuration    = "duration_ms"
	keyStopActive  = "stop_hook_active"
	keyToolInput   = "tool_input"
	keyToolName    = "tool_name"
	keyCommand     = "command"
	goTestCommand  = "go test ./..."
	preToolUseBash = "PreToolUse Bash"
)

// name is one thing the tool times: a hook event with its payload, a command
// with its arguments, or the launcher around a PreToolUse call.
type name struct {
	name, class string
	// on is the kind of sandbox the name runs on.
	on string
	// event and payload are a hook event's; args and stdin a command's.
	event   string
	payload map[string]any
	args    []string
	stdin   string
	// launcher runs plugin/bin/sdlc-hook instead of the binary.
	launcher bool
	// person runs the command with nothing in the environment that marks a
	// session, as release has to be.
	person bool
	// ok reports whether a run's exit code and output are the ones expected:
	// a refusal is not the work, and a run that ends otherwise fails the tool.
	ok func(code int, stdout string) bool
}

func (n name) kind() string {
	switch {
	case n.launcher:
		return "launcher path"
	case n.event != "":
		return "hook event"
	}
	return classCommand
}

func exits0(code int, _ string) bool { return code == 0 }

// names is every name in the order the sandbox is built for them.
func names(repo string) []name {
	tool := func(tool string, input map[string]any) map[string]any {
		return map[string]any{keyToolName: tool, keyAgentType: implementer, keyToolInput: input}
	}
	event := func(label, event string, payload map[string]any) name {
		return name{name: label, class: classHook, on: startedAt(model.GateImplementation), event: event, payload: payload, ok: exits0}
	}
	command := func(label, class, kind, stdin string, args ...string) name {
		return name{name: label, class: class, on: kind, args: args, stdin: stdin, ok: exits0}
	}
	at := startedAt(model.GateImplementation)
	out := []name{
		// The eight events of AC-8 with the payloads its rows send, then the
		// five hooks.json also starts.
		event(preToolUseBash, "PreToolUse", tool("Bash", map[string]any{keyCommand: "go test ./... 2>&1 | tail -n 40"})),
		event("PreToolUse Write", "PreToolUse", tool("Write", map[string]any{"file_path": filepath.Join(repo, "invoice.go"), "content": "package invoice\n"})),
		event("PreToolUse Edit", "PreToolUse", tool("Edit", map[string]any{"file_path": filepath.Join(repo, "invoice.go"), "old_string": "package", "new_string": "package"})),
		event("PostToolUse", "PostToolUse", map[string]any{keyToolName: "Bash", keyDuration: 2000,
			keyToolInput: map[string]any{keyCommand: goTestCommand}, "tool_response": map[string]any{"stdout": "ok", "stderr": "", "interrupted": false}}),
		event("SubagentStart", "SubagentStart", map[string]any{keyAgentType: implementer, "agent_id": "perf"}),
		event("SubagentStop", "SubagentStop", map[string]any{keyAgentType: implementer, "agent_id": "perf", keyStopActive: false,
			"last_assistant_message": `{"status":"done"}`}),
		event("Stop", "Stop", map[string]any{keyStopActive: false}),
		event("SessionStart", "SessionStart", map[string]any{"source": "startup"}),
		event("PostToolUseFailure", "PostToolUseFailure", map[string]any{keyToolName: "Bash", keyDuration: 2000,
			keyToolInput: map[string]any{keyCommand: goTestCommand}, "error": "exit status 1"}),
		event("Notification", "Notification", map[string]any{"message": "sdlc:implementer needs your permission to use Bash", "notification_type": "permission_prompt"}),
		event("StopFailure", "StopFailure", map[string]any{keyStopActive: false, "error": "the turn failed"}),
		event("PostCompact", "PostCompact", map[string]any{"trigger": "auto"}),
		event("UserPromptExpansion", "UserPromptExpansion", map[string]any{"command_name": "sdlc:status", "prompt": "/sdlc:status"}),
	}
	for i := range out {
		switch out[i].name {
		case "SubagentStart", "SubagentStop", "Stop":
			out[i].class = classRecording
		}
	}
	if runtime.GOOS != windows {
		l := event(launcherName, "PreToolUse", out[0].payload)
		l.class, l.launcher = classLauncher, true
		out = append(out, l)
	}
	out = append(out,
		command("status", classCommand, at, "", "status", "--json"),
		command("story list", classCommand, at, "", "story", "list"),
		command("staffing", classCommand, at, "", "staffing", "show"),
		command("artifact write", classStory, at, "# Plan\n\nOne step.\n", "artifact", "write", "plan"),
		command("review list", classStory, at, "", "review", "list"),
		command("cost", classCommand, at, "", "cost"),
		command("trace", classStory, at, "", "trace", "US-001"),
		// doctor exits 1 when it names a problem, having run every check: the
		// sandbox configures no commands.
		name{name: "doctor", class: classStory, on: at, args: []string{"doctor"},
			ok: func(code int, stdout string) bool {
				return code == 0 || code == 1 && strings.Contains(stdout, "no commands are configured")
			}},
		command("review add", classStory, startedAt(model.GateCodeReview), "# Security review\n\nNothing found.\n",
			"review", "add", string(model.GateCodeReview), "security", "approve", "--note", "measured"),
		command("gate commit", classCommit, startedAt(model.GateCommit), "", "gate", string(model.GateCommit), "pass", "--note", "measured"),
		command("freeze", classStory, startedAt(model.GateTestsFrozen), "", "freeze"),
		command("stop", classLanding, kindStarted, "", "stop"),
		command("start", classLanding, kindInitialised, "", "start"),
	)
	release := command("release", classStory, kindStopped, "", "release", "US-001", "--reason", "measured")
	release.person = true
	return append(out, release)
}

// launcherName is the PreToolUse Bash event run through plugin/bin/sdlc-hook,
// as hooks.json runs every event.
const launcherName = "PreToolUse Bash through the launcher"
