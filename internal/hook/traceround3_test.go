package hook

// US-112, design review round 2. The red team named the cheap ways past the
// round-2 tests (V1, V6 and V7), and security found that a named pipe at the
// trace's name hangs whatever reads it (N1). Each test here fails the cheap
// implementation its comment names.

import (
	"bytes"
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/hook/claudecode"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/trace"
)

// trAttr is one key=value of slog's text form, the value quoted or bare.
var trAttr = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)=("(?:[^"\\]|\\.)*"|\S*)`)

// trAttrs is every attribute of the lines, keyed by the line's place and the
// key, without slog's time.
func trAttrs(lines []string) map[string]string {
	out := map[string]string{}
	for i, line := range lines {
		for _, m := range trAttr.FindAllStringSubmatch(line, -1) {
			if m[1] != "time" {
				out[string(rune('0'+i))+"."+m[1]] = m[2]
			}
		}
	}
	return out
}

// trAgentCountsMissing runs a subagent whose transcript plant made, in a fresh
// project, and returns the debug lines that name the transcript.
func trAgentCountsMissing(t *testing.T, plant func(t *testing.T, path string)) []string {
	t.Helper()
	root := trProject(t)
	dir := filepath.Join(root, "transcripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-a1.jsonl")
	plant(t, path)
	log := debugLog(t)
	h := Host{Transcripts: claudecode.ReadTranscript}
	trSendHosted(t, "SubagentStart", trPayload(root, "SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1"}), noEnv, h)
	trSendHosted(t, "SubagentStop", trPayload(root, "SubagentStop", map[string]any{
		"agent_type": "sdlc:verifier", "agent_id": "a1", "agent_transcript_path": path, "last_assistant_message": "Done.",
	}), noEnv, h)
	return linesHolding(log.String(), "transcript")
}

// AC-4, V1: the four cases differ in what stays the same from one run of a
// case to the next. A constant line with something that varies anyway (the
// time taken, the project's directory, a counter) differs between the cases
// only in that, and nothing of it is the same on two runs of one case.
func TestAC4TheDebugLinesOfTheFourCasesDifferInAPartThatIsFixed(t *testing.T) {
	cases := trUnreadableTranscripts()
	fixed := map[string]map[string]string{}
	for _, c := range cases {
		first, second := trAttrs(trAgentCountsMissing(t, c.plant)), trAttrs(trAgentCountsMissing(t, c.plant))
		same := map[string]string{}
		for k, v := range first {
			if second[k] == v {
				same[k] = v
			}
		}
		if len(same) == 0 {
			t.Errorf("AC-4 %s: no part of the debug line is the same on two runs: %v and %v", c.name, first, second)
		}
		fixed[c.name] = same
	}
	for i, a := range cases {
		for _, b := range cases[i+1:] {
			if len(fixed[a.name]) > 0 && maps.Equal(fixed[a.name], fixed[b.name]) {
				t.Errorf("AC-4 %q and %q differ only in what changes from run to run: %v", a.name, b.name, fixed[a.name])
			}
		}
	}
}

// trUnreadableTranscripts is AC-4's four cases, each a way to plant a
// transcript the hook cannot count.
func trUnreadableTranscripts() []struct {
	name  string
	plant func(t *testing.T, path string)
} {
	return []struct {
		name  string
		plant func(t *testing.T, path string)
	}{
		{"missing", func(*testing.T, string) {}},
		{"unreadable, a directory at its name", func(t *testing.T, path string) {
			t.Helper()
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"not in the shape the hook reads", func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("this is not a transcript\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"larger than 64 MB", func(t *testing.T, path string) {
			t.Helper()
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			// Sparse where the filesystem allows it.
			if err := f.Truncate(64<<20 + 1); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}},
	}
}

// trID is what a bounded label may be: an effort, a permission mode, a
// background task's type.
var trID = regexp.MustCompile(`^[A-Za-z0-9:._-]{1,32}$`)

// AC-3, AC-6 and AC-12, V6: the labels a payload gives besides the agent's
// type and id are bounded too. An effort or permission mode that is not a
// short label, a prompt id over 64 bytes and a background task type with a
// space or a newline are absent, and the records are still written; the same
// fields as Claude Code writes them, a 64-byte prompt id among them, are kept.
func TestAC3AC6AC12EffortModePromptIDAndTaskTypeFromAPayloadAreBounded(t *testing.T) {
	t.Parallel()
	send := func(root, effort, mode, prompt string, types ...string) {
		t.Helper()
		fake := &trFake{usage: trUsage()}
		trSubagent(t, root, "sdlc:verifier", "a1", map[string]any{"effort": map[string]any{"level": effort}, "permission_mode": mode}, noEnv, fake)
		var tasks []map[string]any
		for i, typ := range types {
			tasks = append(tasks, map[string]any{"id": string(rune('a' + i)), "type": typ, "status": "running"})
		}
		trSend(t, "Stop", trStopPayload(root, map[string]any{"prompt_id": prompt, "effort": map[string]any{"level": effort}, "background_tasks": tasks}), noEnv, fake)
	}
	t.Run("AC-12 labels that are not labels", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		send(root, "x MARKER", "a\nMARKER", strings.Repeat("p", 65), "local bash MARKER", "x\nMARKER", "local_bash")
		run := trOne(t, root, "agent_run")
		trAbsent(t, run, "effort", "permission_mode")
		turn := trOne(t, root, "turn")
		trAbsent(t, turn, "prompt_id", "effort")
		bg, _ := turn["background_tasks"].(map[string]any)
		if trNumber(bg, "local_bash") != 1 {
			t.Errorf("AC-6 the one well-named background task was not counted: %v", turn["background_tasks"])
		}
		for typ := range bg {
			if typ != "other" && !trID.MatchString(typ) {
				t.Errorf("AC-12 a background task type that is not a label was kept: %q", typ)
			}
		}
		trNothingHolds(t, root, "MARKER", strings.Repeat("p", 65))
	})
	t.Run("AC-3 AC-6 labels as Claude Code writes them", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		prompt := strings.Repeat("p", 64)
		send(root, "high", "acceptEdits", prompt, "local_bash", "remote_agent")
		run := trOne(t, root, "agent_run")
		if run["effort"] != "high" || run["permission_mode"] != "acceptEdits" {
			t.Errorf("AC-3 the run's effort and permission mode = %v and %v, want high and acceptEdits", run["effort"], run["permission_mode"])
		}
		turn := trOne(t, root, "turn")
		if turn["prompt_id"] != prompt || turn["effort"] != "high" {
			t.Errorf("AC-6 the turn's prompt id and effort = %v and %v, want a 64-byte id and high", turn["prompt_id"], turn["effort"])
		}
	})
}

// AC-2, AC-5 and AC-13, V7: the hand-back's status is read, and the run
// appended, with the store's lock let go. From the moment the transcript is
// read the test holds the lock itself, so a hook that took it again for
// either would wait it out and lose the run or its status; and the hand-back
// arrives while the transcript is read, so a status read under the lock,
// before the read, would not see it.
func TestAC2AC5AC13TheHandBackIsReadAndTheRunAppendedWithTheLockFree(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	trSend(t, "SubagentStart", trPayload(root, "SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1"}), noEnv, nil)
	handBack := trPayload(root, "PostToolUse", map[string]any{
		"agent_type": "sdlc:verifier", "agent_id": "a1", "tool_name": "SubagentHandback",
		"tool_input": map[string]any{"message": `{"status":"done"}`}, "tool_response": map[string]any{"ok": true},
	})
	var mu sync.Mutex
	var held *store.Guard
	var lockErr error
	// The hand-back as Claude Code sends it, through the hook.
	deliver := func() { _ = Run([]string{"PostToolUse"}, strings.NewReader(handBack), io.Discard, io.Discard, noEnv) }
	read := func(context.Context, string, int64) (trace.Usage, int64, error) {
		deliver()
		g, err := store.Lock(root, "the test, from the transcript read on")
		mu.Lock()
		held, lockErr = g, err
		mu.Unlock()
		return trUsage(), 1000, nil
	}
	out := trSendHosted(t, "SubagentStop", trPayload(root, "SubagentStop", map[string]any{
		"agent_type": "sdlc:verifier", "agent_id": "a1", "last_assistant_message": "Handed back.",
		"agent_transcript_path": filepath.Join(root, "transcripts", "a1.jsonl"),
	}), noEnv, Host{Transcripts: read})
	mu.Lock()
	if held != nil {
		held.Release()
	}
	mu.Unlock()
	switch {
	case lockErr != nil:
		t.Fatalf("O4 the store's lock was held while the transcript was read: %v", lockErr)
	case held == nil:
		t.Fatal("AC-3 the subagent's transcript was not read, so the run was not recorded")
	}
	if out != trCarryOn {
		t.Errorf("SubagentStop was answered %q", out)
	}
	if n := len(trEventsOfType(t, root, "agent_end")); n != 1 {
		t.Errorf("AC-2 the stop left %d agent_end events", n)
	}
	runs := trOfKind(trRecords(t, root), "agent_run")
	if len(runs) != 1 {
		t.Fatalf("V7 the run was not appended while the test held the store's lock: %d agent_run records", len(runs))
	}
	if runs[0]["status"] != "done" || trNumber(runs[0], "responses") != 3 {
		t.Errorf("V7 the run's status = %v and responses = %v, want done from the hand-back that came during the read, and 3", runs[0]["status"], runs[0]["responses"])
	}
}

// AC-1 and AC-13, security's N1: a named pipe at the trace's name, made
// before the first record, is never written and never waited on. A refusal
// is still a refusal, and a subagent's start and stop still answer at once
// and reach the gate record, though the stop reads the trace for the
// hand-back.
func TestAC1AC13ANamedPipeAtTheTracesNameDoesNotHoldUpTheHook(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	path := trTracePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	u120Fifo(t, path)
	refusal := command(root, "sdlc-implementer", "rm .sdlc/state/tests.lock")
	start := trPayload(root, "SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1"})
	stop := trPayload(root, "SubagentStop", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1", "last_assistant_message": `{"status":"done"}`})
	done := make(chan []string, 1)
	go func() {
		var answers []string
		for _, e := range []struct{ event, payload string }{{"PreToolUse", refusal}, {"SubagentStart", start}, {"SubagentStop", stop}} {
			var out bytes.Buffer
			RunHosted([]string{e.event}, strings.NewReader(e.payload), &out, io.Discard, noEnv, Host{Transcripts: (&trFake{usage: trUsage()}).read})
			answers = append(answers, out.String())
		}
		done <- answers
	}()
	var answers []string
	select {
	case answers = <-done:
	case <-time.After(10 * time.Second):
		for _, flag := range []int{os.O_RDONLY, os.O_WRONLY} {
			if f, err := os.OpenFile(path, flag|syscall.O_NONBLOCK, 0); err == nil {
				_ = f.Close()
			}
		}
		t.Fatal("N1 the hook was still waiting on a named pipe at the trace's name after ten seconds")
	}
	if !strings.Contains(answers[0], `"deny"`) {
		t.Errorf("AC-13 the refusal was answered %q", answers[0])
	}
	if answers[1] != trCarryOn || answers[2] != trCarryOn {
		t.Errorf("AC-13 SubagentStart and SubagentStop were answered %q and %q", answers[1], answers[2])
	}
	if n := len(trEventsOfType(t, root, "agent_end")); n != 1 {
		t.Errorf("AC-2 the stop left %d agent_end events, so the hook did not get that far", n)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("AC-1 the named pipe was replaced or removed: %v", err)
	}
}
