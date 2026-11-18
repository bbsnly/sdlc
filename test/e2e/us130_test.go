package e2e

// US-130 AC-5: the built hook, through the plugin's launcher, with the
// environment and payload Claude Desktop hands a person's typed /sdlc:approve
// and /sdlc:unfreeze (ANALYSIS 3.1, Claude Code 2.1.321, entrypoint
// claude-desktop), runs both; and with each of AC-2's model routes, runs
// neither, and the freeze and the waiting story stay as they were. The values
// are made up in the shape Gate 2 recorded (THREATS I2).

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// desktopHook is the environment Claude Code 2.1.321 gives a hook in Claude
// Desktop, for a session a person opened, besides the session id and the
// project directory: realHookMarkers, with the plugin's two, the effort and
// the shell. It has no CLAUDE_CODE_EXECPATH and no GIT_EDITOR.
func desktopHook(t *testing.T, sb sandbox) []string {
	t.Helper()
	return append(append([]string{}, realHookMarkers...),
		"CLAUDE_PLUGIN_ROOT="+filepath.Join(moduleRoot(t), "plugin"),
		"CLAUDE_PLUGIN_DATA="+sb.own(t, "plugin-data"),
		"CLAUDE_EFFORT=high",
		"SHELL=/bin/zsh",
	)
}

// desktopTyped is the payload Claude Code sends for a line a person typed in
// Claude Desktop: typedLine's, with the scratchpad every payload of 2.1.321
// carries.
func desktopTyped(session, dir, name, args string) map[string]any {
	p := typedLine(session, dir, name, args)
	p["scratchpad_dir"] = filepath.Join(dir, ".scratchpad")
	return p
}

// execPath is CLAUDE_CODE_EXECPATH in its shape.
const execPath = "CLAUDE_CODE_EXECPATH=/opt/claude-desktop/claude-code/2.1.321/claude"

// e2eRoute is one of AC-2's routes to the hook: the hook's environment, and an
// edit to the payload.
type e2eRoute struct {
	name    string
	markers []string
	edit    func(p map[string]any)
}

func modelRoutesThroughTheLauncher(t *testing.T, sb sandbox) []e2eRoute {
	t.Helper()
	bash := func(entry string) []string {
		return []string{
			"CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_CODE_SESSION_ATTENDED=1",
			"CLAUDE_CODE_ENTRYPOINT=" + entry, "CLAUDE_PID=4242", "AI_AGENT=claude-code_2-1-321_agent",
			execPath, "GIT_EDITOR=true",
		}
	}
	hookAnd := func(extra ...string) []string { return append(desktopHook(t, sb), extra...) }
	return []e2eRoute{
		{name: "AC-5 the hook run by hand from a model's Bash in Claude Desktop", markers: bash("claude-desktop")},
		{name: "AC-5 the hook run by hand from a model's Bash in Claude Code in a terminal", markers: bash("cli")},
		{name: "AC-5 the hook run by hand with a hook's label set", markers: append(bash("claude-desktop"),
			"AI_AGENT=claude-code_2-1-321_harness")},
		{name: "AC-5 a claude -p started from a model's Bash, keeping CLAUDE_CODE_EXECPATH and GIT_EDITOR",
			markers: hookAnd("CLAUDE_CODE_SESSION_ATTENDED=0", execPath, "GIT_EDITOR=true")},
		{name: "AC-5 a claude -p started from a model's Bash, with both cleared",
			markers: hookAnd("CLAUDE_CODE_SESSION_ATTENDED=0")},
		{name: "AC-5 an interactive claude started from a model's Bash, keeping both",
			markers: hookAnd(execPath, "GIT_EDITOR=true")},
		{name: "AC-5 a subagent's expansion in the person's own session", markers: desktopHook(t, sb),
			edit: func(p map[string]any) { p["agent_type"], p["agent_id"] = "sdlc:researcher", "a1b2c3" }},
	}
}

// noModelRouteRuns puts /<name> <args> to the hook by every model route, and
// holds that none ran it: the record is as it was, and an answer the person
// reads says the command did not run and what they type instead.
func (sb sandbox) noModelRouteRuns(t *testing.T, dir, name, args, terminal string) {
	t.Helper()
	before := loopRecord(t, sb.repo)
	for _, route := range modelRoutesThroughTheLauncher(t, sb) {
		payload := desktopTyped(sandboxOther, dir, name, args)
		if route.edit != nil {
			route.edit(payload)
		}
		e := sb.expandsInClaudeCode(t, sandboxOther, dir, route.markers, payload)
		if e.blocked() && (!strings.Contains(e.shown(), "/"+name+" did not run") || !strings.Contains(e.shown(), terminal)) {
			t.Errorf("%s: the answer does not say that /%s did not run and what a person types instead (%s): %q",
				route.name, name, terminal, e.raw)
		}
		if e.HookSpecificOutput.AdditionalContext != "" {
			t.Errorf("%s: the hook put text into the model's context: %q", route.name, e.HookSpecificOutput.AdditionalContext)
		}
		if !maps.Equal(before, loopRecord(t, sb.repo)) {
			t.Fatalf("%s: /%s changed the record", route.name, name)
		}
	}
}

func TestAC5InTheSandboxClaudeDesktopsTypedApproveAndUnfreezeRunThroughTheLauncherAndNoModelRouteDoes(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GatePlan); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}

	t.Run("AC-5 no model route lifts the freeze", func(t *testing.T) {
		sb.noModelRouteRuns(t, tree, "sdlc:unfreeze", `--reason "the model wants it lifted"`, "sdlc unfreeze")
		if gate := sb.nextGate(t); gate != string(model.GatePlan) {
			t.Fatalf("the next gate is %q after the model's routes, want plan: the freeze was lifted", gate)
		}
	})

	t.Run("AC-5 the shell's routes to them are refused through the launcher, in every session", func(t *testing.T) {
		for _, session := range []string{sandboxSession, sandboxOther} {
			for command, rule := range map[string]string{
				"unset CLAUDE_CODE_CHILD_SESSION":                                              "enforcement-stays-on",
				"env -u CLAUDE_CODE_EXECPATH -u GIT_EDITOR sdlc hook PreToolUse < /tmp/p.json": "enforcement-stays-on",
				"export CLAUDE_CODE_ENTRYPOINT=claude-desktop":                                 "enforcement-stays-on",
				"export AI_AGENT=claude-code_2-1-321_harness":                                  "enforcement-stays-on",
				`claude -p "/sdlc:approve US-001"`:                                             "story-stays-in-its-session",
				// The verifier's F-1: starts the shell rules did not read as Claude Code.
				`osascript -e 'tell application "Terminal" to do script "claude /sdlc:approve US-001"'`: "story-stays-in-its-session",
				`socat - EXEC:'claude /sdlc:approve US-001',pty`:                                        "story-stays-in-its-session",
				`script -q /dev/null "$CLAUDE_CODE_EXECPATH"`:                                           "story-stays-in-its-session",
			} {
				sb.refuses(t, session, tree, map[string]any{"command": command}, rule)
			}
		}
	})

	t.Run("AC-5 /sdlc:unfreeze typed in Claude Desktop lifts the freeze", func(t *testing.T) {
		e := sb.expandsInClaudeCode(t, sandboxSession, tree, desktopHook(t, sb),
			desktopTyped(sandboxSession, tree, "sdlc:unfreeze", `--reason "AC-1's test asserted the wrong total"`))
		if strings.Contains(e.shown(), "did not run") {
			t.Fatalf("/sdlc:unfreeze typed in Claude Desktop did not run: %q", e.raw)
		}
		sb.showsWhatTheTerminalPrints(t, e, "US-001  freeze lifted on 1 test file\n  AC-1's test asserted the wrong total\n  reopened: tests_frozen\n")
		if gate := sb.nextGate(t); gate != string(model.GateTestsFrozen) {
			t.Fatalf("the next gate after /sdlc:unfreeze is %q, want tests_frozen: the freeze was not lifted", gate)
		}
	})
	sb.worksGate(t, model.GateTestsFrozen)
	sb.works(t, tree, "", "", "escalate", "spec_unclear", "--message", "is a zero total refused too?")

	t.Run("AC-5 no model route answers the escalation", func(t *testing.T) {
		sb.noModelRouteRuns(t, tree, "sdlc:approve", "US-001", "sdlc approve")
		if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
			t.Fatalf("a model's route answered the escalation: the story is %q", got["US-001"])
		}
	})

	t.Run("AC-5 /sdlc:approve typed in Claude Desktop answers it", func(t *testing.T) {
		e := sb.expandsInClaudeCode(t, sandboxSession, tree, desktopHook(t, sb), desktopTyped(sandboxSession, tree, "sdlc:approve", "US-001"))
		if strings.Contains(e.shown(), "did not run") {
			t.Fatalf("/sdlc:approve typed in Claude Desktop did not run: %q", e.raw)
		}
		sb.showsWhatTheTerminalPrints(t, e,
			"US-001  approved (spec_unclear)\n\nA person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.\n")
		if got := storiesOf(t, main); got["US-001"] != "in_progress" {
			t.Fatalf("the story is %q after /sdlc:approve, want in_progress", got["US-001"])
		}
	})
}
