package e2e

// US-058, `sdlc parallel`, through the real binary and the shipped hook
// launcher: what a test in internal/cli cannot do, which is send the process an
// interrupt and look at what is left running, and ask the hook as Claude Code
// does.
//
// The sessions are a fake claude, a shell script first on PATH. It holds a
// FIFO open for as long as it lives, and so does everything it starts, which
// is how a test waits for a session to be up and for the last process of its
// tree to be gone without a clock: a reader of the FIFO gets end of file when
// the last holder has died.

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAC4TheShippedHookRefusesParallelFromAModelAndTheBinaryRefusesAModelSession(t *testing.T) {
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1})

	// Through the launcher the plugin ships, as Claude Code asks it, with no
	// story being worked on and from the session working one.
	for _, command := range []string{
		"sdlc parallel US-001",
		"sdlc parallel --unattended US-001",
		"sdlc --max 2 parallel US-001 US-002",
		"env sdlc parallel US-001",
	} {
		payload := `{"hook_event_name":"PreToolUse","tool_name":"Bash","cwd":` +
			quote(main.dir) + `,"tool_input":{"command":` + quote(command) + `}}`
		got := parse(t, launcher(t, binary, main.dir, payload))
		if got.HookSpecificOutput.PermissionDecision != "deny" {
			t.Errorf("the hook let a model run %q", command)
		} else if !strings.Contains(got.HookSpecificOutput.PermissionDecisionReason, "parallel-is-a-human-decision") {
			t.Errorf("%q was refused for another reason: %s", command, got.HookSpecificOutput.PermissionDecisionReason)
		}
	}

	// And past the hook, which cannot read every spelling, the command itself
	// refuses a process that says it is a session, and starts nothing.
	before := footprintOf(t, main)
	out, err := tool(t, binary, main.dir, "parallel", "US-001")
	if err == nil || !strings.Contains(out, "SDLC-E0061") {
		t.Errorf("sdlc parallel run with a Claude Code session id was not refused with SDLC-E0061 (%v):\n%s", err, out)
	}
	before.unchanged(t, footprintOf(t, main), "a run refused for being a model's")
}

func quote(s string) string {
	var b bytes.Buffer
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestAC8AnInterruptEndsEverySessionTheRunStartedAndLeavesTheRecordsAsTheyWere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot be sent to a process on Windows, and the fake claude is a POSIX script")
	}
	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("mkfifo is not available, and the test waits on FIFOs")
	}
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})

	fake, bin := t.TempDir(), t.TempDir()
	ids := []string{"US-001", "US-002"}
	for _, name := range []string{"never", "alive.US-001", "alive.US-002"} {
		if out, err := exec.CommandContext(t.Context(), "mkfifo", filepath.Join(fake, name)).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo %s: %v\n%s", name, err, out)
		}
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do last=\"$a\"; done\n" +
		"id=\"${last##* }\"\n" +
		"exec 3> \"$E2E_FAKE/alive.$id\"\n" +
		"cat \"$E2E_FAKE/never\" &\n" +
		"wait\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// A reader per session: up when the session opens the FIFO, gone when
	// every process holding it has died.
	up, gone := make(chan string, len(ids)), make(chan string, len(ids))
	for _, id := range ids {
		go func() {
			f, err := os.Open(filepath.Join(fake, "alive."+id))
			if err != nil {
				return
			}
			defer f.Close()
			up <- id
			_, _ = io.Copy(io.Discard, f)
			gone <- id
		}()
	}

	before := footprintOf(t, main)
	cmd := exec.CommandContext(t.Context(), binary, "parallel", "--max", "2", "US-001", "US-002")
	cmd.Dir = main.dir
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "E2E_FAKE="+fake)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	guard := time.After(2 * time.Minute)
	for range ids {
		select {
		case <-up:
		case err := <-waited:
			t.Fatalf("sdlc parallel ended before both sessions were up: %v\n%s", err, out.String())
		case <-guard:
			t.Fatalf("the sessions were not both started:\n%s", out.String())
		}
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waited:
	case <-guard:
		t.Fatalf("sdlc parallel did not end after an interrupt:\n%s", out.String())
	}
	for range ids {
		select {
		case <-gone:
		case <-guard:
			t.Fatal("a session, or something it started, was still running after the launcher ended")
		}
	}

	before.unchanged(t, footprintOf(t, main), "a run that was interrupted")
	for id, status := range storyStatuses(t, main) {
		if status == "done" {
			t.Errorf("%s was recorded done by an interrupted run", id)
		}
	}
	// An interrupt that ends every session is the result this case expects of
	// the run, so it is the run the guard in zcommands_test.go counts.
	noteRun("parallel", "--max", "2", "US-001", "US-002")
}

// storyStatuses is each story's status in the main checkout's backlog.
func storyStatuses(t *testing.T, main checkout) map[string]string {
	t.Helper()
	var backlog struct {
		Stories []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"stories"`
	}
	readJSON(t, filepath.Join(main.dir, "user_stories.json"), &backlog)
	out := map[string]string{}
	for _, s := range backlog.Stories {
		out[s.ID] = s.Status
	}
	return out
}
