package hook

// US-129 AC-10: a benchmark for each hook event of AC-8, run as cmd/sdlc runs
// it (RunHosted), on AC-8's story in the middle of its walk (us129MidWalk:
// its gate record at 190 KB), with the main checkout's backlog as long as
// this repository's (about 130 stories in about 10,000 lines) and a trace of
// 2,000 lines. Before each iteration, off the clock, every file under the
// checkouts' .sdlc/ is put back as it was, so that each call reads the disk
// a fresh call reads and none reads what the one before it wrote (US-129 red
// team, V11). A call that exits other than 0 fails its benchmark.
//
// The fixture builders take a *testing.T, which a benchmark has not, so the
// story is built in a process of this test binary of its own
// (TestUS129BenchFixtures), which keeps it until the benchmark closes its
// input.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/trace"
)

const us129FixtureLine = "U129_FIXTURE "

type us129Fixtures struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Scanner
	said strings.Builder
}

func us129Serve(b *testing.B) *us129Fixtures {
	b.Helper()
	f := &us129Fixtures{cmd: exec.CommandContext(b.Context(), os.Args[0], "-test.run=^TestUS129BenchFixtures$", "-test.count=1")}
	f.cmd.Env = append(os.Environ(), "U129_FIXTURES=mid-walk")
	in, err := f.cmd.StdinPipe()
	if err != nil {
		b.Fatal(err)
	}
	out, err := f.cmd.StdoutPipe()
	if err != nil {
		b.Fatal(err)
	}
	f.cmd.Stderr = &f.said
	if err := f.cmd.Start(); err != nil {
		b.Fatal(err)
	}
	f.in, f.out = in, bufio.NewScanner(out)
	b.Cleanup(func() {
		_ = f.in.Close()
		_ = f.cmd.Wait()
	})
	return f
}

func (f *us129Fixtures) next(b *testing.B) swLayout {
	b.Helper()
	if _, err := io.WriteString(f.in, "next\n"); err != nil {
		b.Fatalf("the fixture process is gone: %v\n%s", err, f.said.String())
	}
	for f.out.Scan() {
		if line, ok := strings.CutPrefix(f.out.Text(), us129FixtureLine); ok {
			p := strings.Split(line, "\x1f")
			return swLayout{main: p[0], wt: p[1], other: p[2]}
		}
		f.said.WriteString(f.out.Text() + "\n")
	}
	b.Fatalf("the fixture process ended without a story:\n%s", f.said.String())
	return swLayout{}
}

// TestUS129BenchFixtures is the process the AC-10 benchmarks build their
// story in, and nothing on its own.
func TestUS129BenchFixtures(t *testing.T) {
	if os.Getenv("U129_FIXTURES") == "" {
		t.Skip("the process the AC-10 benchmarks of this package build their story in")
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		l := us129MidWalk(t)
		us129PadBacklog(t, l.main)
		dir := filepath.Join(l.main, ".sdlc", "stories", "A-1")
		for range 2000 {
			if err := trace.Append(dir, trace.Record{"kind": "shell", trace.KeyAgentType: "sdlc:implementer",
				"duration_ms": 2000, "class": "test", "program": "go", "outcome": "ok", "exit_code": 0}); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Println(us129FixtureLine + l.main + "\x1f" + l.wt + "\x1f" + l.other)
	}
}

// us129PadBacklog is swCheckouts's backlog, as it stands mid-walk, and 129
// finished stories with the criteria, non-goals and notes a real one carries.
func us129PadBacklog(t *testing.T, main string) {
	t.Helper()
	var b model.Backlog
	if err := json.Unmarshal([]byte(swBacklog("in_progress", "in_progress", "ready")), &b); err != nil {
		t.Fatal(err)
	}
	words := strings.Repeat("the hook answers every call it is asked without reading a file twice ", 3)
	for i := range 129 {
		s := model.Story{
			ID: fmt.Sprintf("P-%03d", i+1), Title: fmt.Sprintf("Finished story %d", i+1), Status: model.StatusDone,
			AsA: "person working a story", IWant: words, SoThat: words, Notes: words,
		}
		for j := range 14 {
			s.AcceptanceCriteria = append(s.AcceptanceCriteria, model.AcceptanceCriterion{ID: fmt.Sprintf("AC-%d", j+1), Text: words})
		}
		for range 6 {
			s.NonGoals = append(s.NonGoals, words)
		}
		b.Stories = append(b.Stories, s)
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, main, "user_stories.json", string(raw)+"\n")
}

type us129File struct {
	body []byte
	mode fs.FileMode
	mod  time.Time
}

// us129Loops are the directories a hook call writes in: each checkout's
// .sdlc/, the main checkout's without the worktrees inside it.
func us129Loops(l swLayout) []string {
	return []string{filepath.Join(l.main, ".sdlc"), filepath.Join(l.wt, ".sdlc"), filepath.Join(l.other, ".sdlc")}
}

func us129Walk(b *testing.B, l swLayout, each func(path string, info fs.FileInfo)) {
	b.Helper()
	skip := filepath.Join(l.main, ".sdlc", "worktrees")
	for _, root := range us129Loops(l) {
		err := filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
			switch {
			case err != nil:
				return err
			case info.IsDir() && path == skip:
				return filepath.SkipDir
			case info.Mode().IsRegular():
				each(path, info)
			}
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func us129Snapshot(b *testing.B, l swLayout) map[string]us129File {
	b.Helper()
	snap := map[string]us129File{}
	us129Walk(b, l, func(path string, info fs.FileInfo) {
		body, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		snap[path] = us129File{body: body, mode: info.Mode().Perm(), mod: info.ModTime()}
	})
	return snap
}

// us129Restore puts every file of snap back, and takes away any other.
func us129Restore(b *testing.B, l swLayout, snap map[string]us129File) {
	b.Helper()
	us129Walk(b, l, func(path string, _ fs.FileInfo) {
		if _, ok := snap[path]; !ok {
			if err := os.Remove(path); err != nil {
				b.Fatal(err)
			}
		}
	})
	for path, f := range snap {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, f.body, f.mode); err != nil {
			b.Fatal(err)
		}
		if err := os.Chtimes(path, f.mod, f.mod); err != nil {
			b.Fatal(err)
		}
	}
}

// us129Event times the hook event named, as us129Call runs it.
func us129Time(b *testing.B, name string) {
	b.Helper()
	var e us129Event
	for _, each := range us129Events() {
		if each.name == name {
			e = each
		}
	}
	if e.name == "" {
		b.Fatalf("no hook event %q", name)
	}
	l := us129Serve(b).next(b)
	snap := us129Snapshot(b, l)
	payload := e.payload(l)
	payload["hook_event_name"], payload["session_id"], payload["cwd"] = e.event, swHeld, l.wt
	stdin, err := json.Marshal(payload)
	if err != nil {
		b.Fatal(err)
	}
	getenv := env(map[string]string{"CLAUDE_PROJECT_DIR": l.wt})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		us129Restore(b, l, snap)
		var out bytes.Buffer
		b.StartTimer()
		code := RunHosted([]string{e.event}, bytes.NewReader(stdin), &out, io.Discard, getenv, Host{Transcripts: us129NoTranscripts})
		b.StopTimer()
		if code != 0 {
			b.Fatalf("%s exited %d:\n%s", e.name, code, out.String())
		}
		b.StartTimer()
	}
}

func BenchmarkHookPreToolUseBash(b *testing.B)  { us129Time(b, "PreToolUse Bash") }
func BenchmarkHookPreToolUseWrite(b *testing.B) { us129Time(b, "PreToolUse Write") }
func BenchmarkHookPreToolUseEdit(b *testing.B)  { us129Time(b, "PreToolUse Edit") }
func BenchmarkHookPostToolUse(b *testing.B)     { us129Time(b, "PostToolUse") }
func BenchmarkHookSubagentStart(b *testing.B)   { us129Time(b, "SubagentStart") }
func BenchmarkHookSubagentStop(b *testing.B)    { us129Time(b, "SubagentStop") }
func BenchmarkHookStop(b *testing.B)            { us129Time(b, "Stop") }
func BenchmarkHookSessionStart(b *testing.B)    { us129Time(b, "SessionStart") }
