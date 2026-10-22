package main

// US-108 AC-8: what the hook prints on stdout for PreToolUse, PostToolUse and
// Stop is held byte for byte, before the story and after it. The story adds a
// new event and new refusals; none of that may change an answer Claude Code
// already reads for these three. The golden was captured from the hook as it
// was before the story, through run, which is the binary's own entry point.
//
// AC-12 rewords every message that names one of a person's commands, so no call
// below is one whose answer names approve, unfreeze, ack, remap, release or
// staffing assign: those answers are meant to change.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// goldenSession is the session that started the story in goldenProject.
const goldenSession = "session-a"

// goldenProject is a project that takes part, with story A-1 under way in
// goldenSession and a formatter that fails on a file named broken.go.
func goldenProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	for rel, body := range map[string]string{
		".sdlc/config.json":   `{"version":1,"commands":{"fmt_file":"exit 3"}}`,
		".sdlc/state/active":  "A-1\n",
		".sdlc/state/session": goldenSession + "\n",
		"user_stories.json":   `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}` + "\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A story that has passed Gate 1, so that a stop is sent back to the gate
	// after it, in a checkout, which is a git repository's.
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	record := model.NewRecord("A-1", at)
	record.SetGate(model.GateDoR, model.GatePass, "", at)
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".sdlc", "stories", "A-1", "gate-record.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	git.Dir = root
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return root
}

func goldenPayload(t *testing.T, fields map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// hookReplies runs every call of the golden through run and writes, for each,
// its name and then the hook's stdout with the project's place replaced.
func hookReplies(t *testing.T) string {
	t.Helper()
	root := goldenProject(t)
	plain := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(plain); err == nil {
		plain = resolved
	}
	elsewhere := filepath.Join(t.TempDir(), "notes.txt")

	tool := func(event, session, cwd, name string, input map[string]string) string {
		return goldenPayload(t, map[string]any{
			"hook_event_name": event, "session_id": session, "cwd": cwd,
			"tool_name": name, "tool_input": input,
		})
	}
	byAgent := func(agent, stdin string) string {
		var fields map[string]any
		if err := json.Unmarshal([]byte(stdin), &fields); err != nil {
			t.Fatal(err)
		}
		fields["agent_type"] = agent
		return goldenPayload(t, fields)
	}
	stop := func(session string, active bool) string {
		return goldenPayload(t, map[string]any{
			"hook_event_name": "Stop", "session_id": session, "cwd": root, "stop_hook_active": active,
		})
	}
	off := func(k string) string {
		if k == "SDLC_ENFORCE" {
			return "0"
		}
		return ""
	}

	calls := []struct {
		name, event, stdin string
		getenv             func(string) string
	}{
		{"PreToolUse: a write in a project that does not take part", "PreToolUse",
			tool("PreToolUse", goldenSession, plain, "Write", map[string]string{"file_path": filepath.Join(plain, ".git", "config")}), noEnv},
		{"PreToolUse: a write of code by the main conversation during an iteration", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Write", map[string]string{"file_path": filepath.Join(root, "internal", "invoice.go")}), noEnv},
		{"PreToolUse: a write of code by the implementer", "PreToolUse",
			byAgent("sdlc-implementer", tool("PreToolUse", goldenSession, root, "Write",
				map[string]string{"file_path": filepath.Join(root, "internal", "invoice.go")})), noEnv},
		{"PreToolUse: a write to the loop's own state", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Write", map[string]string{"file_path": filepath.Join(root, ".sdlc", "state", "active")}), noEnv},
		{"PreToolUse: a write outside the repository", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Write", map[string]string{"file_path": elsewhere}), noEnv},
		{"PreToolUse: an edit from another session of the same project", "PreToolUse",
			tool("PreToolUse", "session-b", root, "Edit", map[string]string{"file_path": filepath.Join(root, "internal", "invoice.go")}), noEnv},
		{"PreToolUse: a command that pushes", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Bash", map[string]string{"command": "git push origin main"}), noEnv},
		{"PreToolUse: a command that turns enforcement off", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Bash", map[string]string{"command": "SDLC_ENFORCE=0 go test ./..."}), noEnv},
		{"PreToolUse: a command that writes the loop's own state", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Bash", map[string]string{"command": "echo B-2 > .sdlc/state/active"}), noEnv},
		{"PreToolUse: a command the session working the story may run", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Bash", map[string]string{"command": "go test ./..."}), noEnv},
		{"PreToolUse: a command from another session of the same project", "PreToolUse",
			tool("PreToolUse", "session-b", root, "Bash", map[string]string{"command": "go vet ./..."}), noEnv},
		{"PreToolUse: a write to the loop's own state with enforcement off", "PreToolUse",
			tool("PreToolUse", goldenSession, root, "Write", map[string]string{"file_path": filepath.Join(root, ".sdlc", "state", "active")}), off},
		{"PreToolUse: a payload that is not JSON", "PreToolUse", "not json at all", noEnv},
		{"PostToolUse: a write the formatter fails on", "PostToolUse",
			tool("PostToolUse", goldenSession, root, "Write", map[string]string{"file_path": filepath.Join(root, "internal", "broken.go")}), noEnv},
		{"PostToolUse: a write to the loop's own directory, which is not formatted", "PostToolUse",
			tool("PostToolUse", goldenSession, root, "Write", map[string]string{"file_path": filepath.Join(root, ".sdlc", "notes.md")}), noEnv},
		{"PostToolUse: a write from another session", "PostToolUse",
			tool("PostToolUse", "session-b", root, "Write", map[string]string{"file_path": filepath.Join(root, "internal", "broken.go")}), noEnv},
		{"PostToolUse: a write in a project that does not take part", "PostToolUse",
			tool("PostToolUse", goldenSession, plain, "Write", map[string]string{"file_path": filepath.Join(plain, "x.go")}), noEnv},
		{"PostToolUse: a payload that is not JSON", "PostToolUse", "not json at all", noEnv},
		{"Stop: the session working the story stops for the first time", "Stop", stop(goldenSession, false), noEnv},
		{"Stop: and a second time, with nothing recorded", "Stop", stop(goldenSession, false), noEnv},
		{"Stop: a stop already sent back once", "Stop", stop(goldenSession, true), noEnv},
		{"Stop: another session of the same project stops", "Stop", stop("session-b", false), noEnv},
		{"Stop: with enforcement off", "Stop", stop(goldenSession, false), off},
		{"Stop: a payload that is not JSON", "Stop", "not json at all", noEnv},
	}

	var b strings.Builder
	for _, c := range calls {
		var out, errOut bytes.Buffer
		code := run([]string{"hook", c.event}, strings.NewReader(c.stdin), &out, &errOut, c.getenv)
		text := out.String()
		for _, p := range []struct{ from, to string }{
			{filepath.Dir(elsewhere), "<elsewhere>"},
			{root, "<root>"},
			{plain, "<plain>"},
		} {
			// The place as it is on disk, and as JSON writes it on Windows.
			text = strings.ReplaceAll(text, strings.ReplaceAll(p.from, `\`, `\\`), p.to)
			text = strings.ReplaceAll(text, p.from, p.to)
		}
		b.WriteString("== " + c.name + "\n")
		if code != 0 {
			fmt.Fprintf(&b, "exit %d\n", code)
		}
		b.WriteString(text)
	}
	return b.String()
}

func TestAC8TheHooksAnswersToPreToolUsePostToolUseAndStopAreTheOnesItGaveBeforeTheStory(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "hook-replies.golden"))
	if err != nil {
		t.Fatalf("the answers the hook gave before the story are not there to compare with: %v", err)
	}
	got := hookReplies(t)
	if got == string(want) {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("the hook's stdout changed at line %d of testdata/hook-replies.golden:\ngot:  %s\nwant: %s", i+1, g, w)
		}
	}
}
