package hook

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunAlwaysEmitsExactlyOneJSONObject(t *testing.T) {
	for _, tc := range []struct {
		name, stdin string
		args        []string
	}{
		{"no args", "", nil},
		{"event, empty stdin", "", []string{"PreToolUse"}},
		{"event with payload", `{"tool_name":"Bash"}`, []string{"PreToolUse"}},
		{"garbage stdin", "not json at all", []string{"PreToolUse"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := Run(tc.args, strings.NewReader(tc.stdin), &out); code != 0 {
				t.Fatalf("exit %d", code)
			}
			var d Decision
			if err := json.Unmarshal(out.Bytes(), &d); err != nil {
				t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
			}
			if !d.Continue {
				t.Error("no policy exists yet, so nothing should be blocked")
			}
		})
	}
}

func TestOversizedStdinDoesNotHangOrGrowUnbounded(t *testing.T) {
	// A hook that reads forever would hang the user's session.
	var out bytes.Buffer
	huge := strings.Repeat("x", 4<<20)
	if code := Run([]string{"PreToolUse"}, strings.NewReader(huge), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() > 200 {
		t.Errorf("output should stay small, got %d bytes", out.Len())
	}
}

func TestDenyCarriesItsReason(t *testing.T) {
	d := Deny("tests are frozen at Gate 3 -- run `sdlc gate unfreeze` in your terminal")
	if d.Continue {
		t.Error("Deny must not continue")
	}
	if d.StopReason == "" {
		t.Error("a denial with no reason teaches people to work around the tool")
	}
}
