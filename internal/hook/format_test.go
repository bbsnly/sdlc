package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func written(t *testing.T, root, path string, getenv func(string) string) stopOutcome {
	t.Helper()
	event, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "Write", "cwd": root, "session_id": working,
		"tool_input": map[string]string{"file_path": path},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Run([]string{"PostToolUse"}, bytes.NewReader(event), &out, io.Discard, getenv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r stopOutcome
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
	}
	if !r.Continue {
		t.Fatal("formatting a file ended the session outright")
	}
	return r
}

func withFormatter(t *testing.T, root, command string) {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{"version": 1, "commands": map[string]string{"fmt_file": command}})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/config.json", string(cfg))
}

func contents(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// commands.fmt_file was written into the configuration and run by nothing.
func TestAFileAToolWroteIsFormatted(t *testing.T) {
	root := loopProject(t)
	withFormatter(t, root, `printf 'formatted\n' > "$FILE"`)
	write(t, root, "internal/invoice.go", "package  invoice\n")

	if r := written(t, root, filepath.Join(root, "internal", "invoice.go"), noEnv); r.Decision != "" {
		t.Fatalf("a formatter that succeeded sent the session back: %s", r.Reason)
	}
	if got := contents(t, root, "internal/invoice.go"); got != "formatted\n" {
		t.Errorf("the file was not formatted: %q", got)
	}
}

func TestAFormatterThatFailsIsReportedToTheSession(t *testing.T) {
	root := loopProject(t)
	withFormatter(t, root, `echo "invoice.go:1:9: expected 'package'" >&2; exit 2`)
	write(t, root, "invoice.go", "pakage invoice\n")

	r := written(t, root, filepath.Join(root, "invoice.go"), noEnv)
	if r.Decision != "block" {
		t.Fatal("a formatter that failed said nothing")
	}
	for _, want := range []string{"invoice.go", "expected 'package'"} {
		if !strings.Contains(r.Reason, want) {
			t.Errorf("the report does not say %q: %s", want, r.Reason)
		}
	}
}

// A tool that runs a command rather than writing a file has no file_path at
// all, and there is nothing for commands.fmt_file to be run on.
func TestAPostToolUseEventWithNoFilePathIsIgnored(t *testing.T) {
	root := loopProject(t)
	withFormatter(t, root, `printf 'formatted\n' > "$FILE"`)
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "Bash", "cwd": root, "session_id": working,
		"tool_input": map[string]string{"command": "echo hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Run([]string{"PostToolUse"}, bytes.NewReader(raw), &out, io.Discard, noEnv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r stopOutcome
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
	}
	if r.Decision != "" || r.SystemMessage != "" {
		t.Errorf("a command with no file path was formatted: %+v", r)
	}
}

// Formatting is skipped, and the file left exactly as written, for every
// reason short of a formatter that ran and failed: enforcement turned off,
// a configuration that will not read, no fmt_file command configured at
// all, and a project with no shell to run one on. The last of those is the
// only one worth telling the session about, since it is the only one where a
// formatter was supposed to run and did not.
func TestAFileIsLeftUnformattedWhenFormattingCannotRun(t *testing.T) {
	for _, tc := range []struct {
		name          string
		setup         func(t *testing.T, root string)
		getenv        func(string) string
		wantSystemMsg string
	}{
		{
			name: "a human turned enforcement off",
			setup: func(t *testing.T, root string) {
				t.Helper()
				withFormatter(t, root, `printf 'formatted\n' > "$FILE"`)
			},
			getenv: env(map[string]string{"SDLC_ENFORCE": "0"}),
		},
		{
			name: "the configuration will not read",
			setup: func(t *testing.T, root string) {
				t.Helper()
				write(t, root, ".sdlc/config.json", "{not json")
			},
			getenv: noEnv,
		},
		{
			name:   "no fmt_file command is configured",
			setup:  func(*testing.T, string) {},
			getenv: noEnv,
		},
		{
			name: "no shell is on PATH",
			setup: func(t *testing.T, root string) {
				t.Helper()
				withFormatter(t, root, `printf 'formatted\n' > "$FILE"`)
				t.Setenv("PATH", "")
			},
			getenv:        noEnv,
			wantSystemMsg: "commands.fmt_file was not run",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := loopProject(t)
			tc.setup(t, root)
			write(t, root, "invoice.go", "package  invoice\n")

			r := written(t, root, filepath.Join(root, "invoice.go"), tc.getenv)
			if r.Decision != "" {
				t.Errorf("Decision = %q, want none: %s", r.Decision, r.Reason)
			}
			if got := contents(t, root, "invoice.go"); got != "package  invoice\n" {
				t.Errorf("the file was formatted: %q", got)
			}
			if tc.wantSystemMsg != "" && !strings.Contains(r.SystemMessage, tc.wantSystemMsg) {
				t.Errorf("systemMessage = %q, want it to contain %q", r.SystemMessage, tc.wantSystemMsg)
			}
		})
	}
}

func TestOnlyTheStorysOwnFilesAreFormatted(t *testing.T) {
	root := loopProject(t)
	withFormatter(t, root, `printf 'formatted\n' > "$FILE"`)
	write(t, root, ".sdlc/stories/A-1/notes.md", "as written\n")
	write(t, root, "outside.go", "as written\n")

	written(t, root, filepath.Join(root, ".sdlc", "stories", "A-1", "notes.md"), noEnv)
	if got := contents(t, root, ".sdlc/stories/A-1/notes.md"); got != "as written\n" {
		t.Error("the formatter ran on the loop's own files")
	}

	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	written(t, root, filepath.Join(root, "outside.go"), noEnv)
	if got := contents(t, root, "outside.go"); got != "as written\n" {
		t.Error("the formatter ran with no story being worked on")
	}
}
