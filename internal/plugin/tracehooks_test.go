package plugin

// US-112 AC-11, AC-14 and AC-24: the events the hook records are registered in
// hooks.json, each with a timeout no longer than 30 seconds, and the ones
// registered already keep what they had.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type hookEntry struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	} `json:"hooks"`
}

func registered(t *testing.T) map[string][]hookEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(pluginDir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]hookEntry `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Hooks
}

// matched is whether an entry of event matches tool, and the timeout of the
// hook that does.
func matched(entries []hookEntry, tool string) (int, bool) {
	for _, e := range entries {
		if e.Matcher == "" || slices.Contains(strings.Split(e.Matcher, "|"), tool) {
			for _, h := range e.Hooks {
				return h.Timeout, true
			}
		}
	}
	return 0, false
}

func TestAC11AC14AC24EveryRecordedEventIsRegisteredWithin30Seconds(t *testing.T) {
	t.Parallel()
	hooks := registered(t)
	for _, event := range []string{"SubagentStart", "SubagentStop", "Notification", "StopFailure", "PostCompact"} {
		entries := hooks[event]
		if len(entries) == 0 {
			t.Errorf("AC-11 AC-24 %s is not registered", event)
			continue
		}
		for _, e := range entries {
			for _, h := range e.Hooks {
				if h.Timeout <= 0 || h.Timeout > 30 {
					t.Errorf("AC-14 %s has a timeout of %d s, want 1 to 30", event, h.Timeout)
				}
				if !strings.HasSuffix(h.Command, `/bin/sdlc-hook" `+event) {
					t.Errorf("AC-24 %s runs %q, want the launcher on that event", event, h.Command)
				}
			}
		}
	}
	for _, tool := range []string{"Bash", "PowerShell"} {
		timeout, ok := matched(hooks["PostToolUseFailure"], tool)
		if !ok {
			t.Errorf("AC-9 AC-24 PostToolUseFailure does not match %s", tool)
		} else if timeout <= 0 || timeout > 30 {
			t.Errorf("AC-14 PostToolUseFailure on %s has a timeout of %d s", tool, timeout)
		}
	}
	for _, tool := range []string{"Bash", "PowerShell", "SubagentHandback"} {
		timeout, ok := matched(hooks["PostToolUse"], tool)
		if !ok {
			t.Errorf("AC-8 AC-24 PostToolUse does not match %s", tool)
		} else if timeout <= 0 || timeout > 30 {
			t.Errorf("AC-14 PostToolUse on %s has a timeout of %d s", tool, timeout)
		}
	}
	// What was registered before stays: the formatter after a write.
	for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
		if _, ok := matched(hooks["PostToolUse"], tool); !ok {
			t.Errorf("PostToolUse no longer matches %s", tool)
		}
	}
	if _, ok := matched(hooks["Stop"], ""); !ok {
		t.Error("Stop is no longer registered")
	}
}
