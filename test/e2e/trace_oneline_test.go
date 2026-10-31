package e2e

// US-112, design review round 2 (security, R2-1, R2-2 and N1): the built hook,
// through the launcher, refuses the one-line spellings that took AI_AGENT
// away or ran the launcher on a recorded event, and a named pipe at the
// trace's name, from the session holding the story and from another; nothing
// reaches the gate record.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAC2AC15AOneLineForgeOfARecordedEventIsRefusedInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	record := filepath.Join(main, ".sdlc", "stories", "US-001", "gate-record.json")
	before, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	dir := sb.own(t, "forged")
	payload, list := filepath.Join(dir, "p.json"), filepath.Join(dir, "l")
	from := ` < "` + filepath.ToSlash(payload) + `"`
	for _, session := range []string{sandboxSession, sandboxOther} {
		for command, rule := range map[string]string{
			`. "$CLAUDE_PLUGIN_ROOT/bin/sdlc-hook" SubagentStart` + from:        "recorded-event-comes-from-the-host",
			`L="$CLAUDE_PLUGIN_ROOT/bin/sdlc-hook"; $L SubagentStop` + from:     "recorded-event-comes-from-the-host",
			`xargs -I{} sh {} SubagentStart < "` + filepath.ToSlash(list) + `"`: "recorded-event-comes-from-the-host",
			"X=unset; $X AI_AGENT; sdlc hook PreToolUse" + from:                 "enforcement-stays-on",
			"declare -a AI_AGENT; sdlc hook PreToolUse" + from:                  "enforcement-stays-on",
			`env -i PATH="$PATH" sdlc hook PreToolUse` + from:                   "enforcement-stays-on",
		} {
			sb.refuses(t, session, tree, map[string]any{"command": command}, rule)
		}
	}
	sb.refuses(t, sandboxSession, tree, map[string]any{"command": "mkfifo .sdlc/stories/US-001/trace.jsonl"}, "trace-is-written-by-the-tool")
	if after, _ := os.ReadFile(record); string(after) != string(before) {
		t.Error("R2-1 the refusals left an agent event on the gate record")
	}
}
