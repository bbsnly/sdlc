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
		"hook_event_name": "PostToolUse", "tool_name": "Write", "cwd": root,
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
