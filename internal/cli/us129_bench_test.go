package cli

// US-129 AC-10: a benchmark for each command of AC-3, run as cmd/sdlc runs it
// (ExecuteIn), each iteration on a project of its own: a copy of a template
// the real commands made, as startedAt makes one, with a backlog the size of
// this repository's (about 130 stories in about 10,000 lines, committed before
// the story started), US-001's gate record padded to 190 KB and a trace of
// 2,000 lines. Nothing is measured twice on one project, so no iteration reads
// what an earlier one left (US-129 red team, V11), and the copy is made off
// the clock. Gate is the commit gate and Stop the landing, the two commands
// that ask git the most. A command that exits other than 0 fails its
// benchmark with what it printed: a refusal is not the work.
//
// The fixture builders take a *testing.T, which a benchmark has not, so they
// run in a process of this test binary of their own (TestUS129BenchFixtures),
// which makes one project for each line the benchmark sends it and keeps them
// all until the benchmark closes its input.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/trace"
)

const us129FixtureLine = "U129_FIXTURE "

type us129Fixtures struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Scanner
	said strings.Builder
}

// us129Serve starts the process that builds kind's projects.
func us129Serve(b *testing.B, kind string) *us129Fixtures {
	b.Helper()
	f := &us129Fixtures{cmd: exec.CommandContext(b.Context(), os.Args[0], "-test.run=^TestUS129BenchFixtures$", "-test.count=1")}
	f.cmd.Env = append(os.Environ(), "U129_FIXTURES="+kind)
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

// next is a new project of the kind f builds.
func (f *us129Fixtures) next(b *testing.B) string {
	b.Helper()
	if _, err := io.WriteString(f.in, "next\n"); err != nil {
		b.Fatalf("the fixture process is gone: %v\n%s", err, f.said.String())
	}
	for f.out.Scan() {
		if path, ok := strings.CutPrefix(f.out.Text(), us129FixtureLine); ok {
			return path
		}
		f.said.WriteString(f.out.Text() + "\n")
	}
	b.Fatalf("the fixture process ended without a project:\n%s", f.said.String())
	return ""
}

// TestUS129BenchFixtures is the process the AC-10 benchmarks build their
// projects in, and nothing on its own.
func TestUS129BenchFixtures(t *testing.T) {
	kind := os.Getenv("U129_FIXTURES")
	if kind == "" {
		t.Skip("the process the AC-10 benchmarks of this package build their projects in")
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		fmt.Println(us129FixtureLine + us129Fixture(t, kind))
	}
}

// us129Fixture is a project of kind: "initialised", "stopped" (started, at the
// implementation, then stopped) or "started:<gate>", the story through every
// gate before the one named, or through all of them where none is.
func us129Fixture(t *testing.T, kind string) string {
	t.Helper()
	if kind == "initialised" {
		root := walkedCopy(t, "us129 bench initialised", func(t *testing.T, root string) {
			t.Helper()
			gitProjectInitialisedAt(t, root)
			us129PadBacklog(t, root)
		})
		chdir(t, root)
		return root
	}
	upTo, stopped := model.Gate(strings.TrimPrefix(kind, "started:")), kind == "stopped"
	if stopped {
		upTo = model.GateImplementation
	}
	root := walkedCopy(t, "us129 bench started "+string(upTo), func(t *testing.T, root string) {
		t.Helper()
		gitProjectInitialisedAt(t, root)
		us129PadBacklog(t, root)
		chdir(t, root)
		mustRun(t, "start")
		reach(t, root, upTo)
		us129PadStory(t, root)
	})
	chdir(t, root)
	if upTo == model.GateCommit {
		// The commit gate records that the work reached trunk.
		commitEverything(t, root)
	}
	if upTo == model.GateTestsFrozen {
		// A freeze holds the story's tests.
		writeFile(t, root, "us129_test.go", "package us129\n")
	}
	if stopped {
		mustRun(t, "stop")
	}
	return root
}

// us129PadBacklog adds 129 finished stories to the project's backlog, each
// with the criteria, non-goals and notes a real one carries, and commits it.
func us129PadBacklog(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	path := cfg.BacklogPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var b model.Backlog
	if err := json.Unmarshal(raw, &b); err != nil {
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
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "--", path}, {"commit", "--quiet", "-m", "chore: the backlog as long as a real one"}} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=Test", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// us129PadStory pads US-001's gate record to 190 KB with the agent runs a
// long story records, and its trace with 2,000 lines of shell commands.
func us129PadStory(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(&config.Project{Root: root, Config: cfg})
	record, err := s.Record("US-001")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	for i := 0; ; i++ {
		when := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		agent := fmt.Sprintf("agent-%04d", i)
		record.Events = append(record.Events,
			model.Event{At: when, Type: model.EventAgentStart, Message: "sdlc:researcher started", AgentType: "sdlc:researcher", AgentID: agent},
			model.Event{At: when, Type: model.EventAgentEnd, Message: "sdlc:researcher finished", AgentType: "sdlc:researcher", AgentID: agent})
		if i%16 != 0 {
			continue
		}
		raw, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) >= 190<<10 {
			break
		}
	}
	if err := s.SaveRecord(record); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".sdlc", "stories", "US-001")
	for range 2000 {
		if err := trace.Append(dir, trace.Record{"kind": "shell", trace.KeyAgentType: "sdlc:implementer",
			"duration_ms": 2000, "class": "test", "program": "go", "outcome": "ok", "exit_code": 0}); err != nil {
			t.Fatal(err)
		}
	}
}

// us129AsAPerson is the environment with nothing that marks a model's
// session, as a person's terminal has it: release refuses a session.
func us129AsAPerson(name string) string {
	if name == store.SessionEnv || name == "AI_AGENT" || strings.HasPrefix(name, "CLAUDE") {
		return ""
	}
	return os.Getenv(name)
}

// us129Command times sdlc args, run with stdin, on a new project of kind for
// each iteration.
func us129Command(b *testing.B, kind, stdin string, getenv func(string) string, args ...string) {
	b.Helper()
	f := us129Serve(b, kind)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		dir := f.next(b)
		var out, errb bytes.Buffer
		b.StartTimer()
		code := ExecuteIn(dir, getenv, args, strings.NewReader(stdin), &out, &errb)
		b.StopTimer()
		// doctor exits 1 when it names a problem, having run every check: the
		// project configures no commands, and on Linux more is missing.
		if code != 0 && (args[0] != "doctor" || code != 1 || !strings.Contains(out.String(), "no commands are configured")) {
			b.Fatalf("sdlc %s exited %d on the benchmark's project:\n%s%s", strings.Join(args, " "), code, out.String(), errb.String())
		}
		b.StartTimer()
	}
}

const us129AtImplementation = "started:" + string(model.GateImplementation)

func BenchmarkCommandStart(b *testing.B) {
	us129Command(b, "initialised", "", os.Getenv, "start")
}

func BenchmarkCommandStatus(b *testing.B) {
	us129Command(b, us129AtImplementation, "", os.Getenv, "status", "--json")
}

func BenchmarkCommandStoryList(b *testing.B) {
	us129Command(b, us129AtImplementation, "", os.Getenv, "story", "list")
}

func BenchmarkCommandStaffing(b *testing.B) {
	us129Command(b, us129AtImplementation, "", os.Getenv, "staffing", "show")
}

func BenchmarkCommandArtifactWrite(b *testing.B) {
	us129Command(b, us129AtImplementation, "# Plan\n\nOne step.\n", os.Getenv, "artifact", "write", "plan")
}

func BenchmarkCommandReviewAdd(b *testing.B) {
	us129Command(b, "started:"+string(model.GateCodeReview), us129Review, os.Getenv,
		"review", "add", "code_review", "security", "approve", "--note", "benchmarked")
}

func BenchmarkCommandReviewList(b *testing.B) {
	us129Command(b, us129AtImplementation, "", os.Getenv, "review", "list")
}

func BenchmarkCommandGate(b *testing.B) {
	us129Command(b, "started:"+string(model.GateCommit), "", os.Getenv, "gate", "commit", "pass", "--note", "benchmarked")
}

func BenchmarkCommandFreeze(b *testing.B) {
	us129Command(b, "started:"+string(model.GateTestsFrozen), "", os.Getenv, "freeze")
}

func BenchmarkCommandCost(b *testing.B) {
	us129Command(b, us129AtImplementation, "", os.Getenv, "cost")
}

func BenchmarkCommandTrace(b *testing.B) {
	us129Command(b, us129AtImplementation, "", os.Getenv, "trace", "US-001")
}

func BenchmarkCommandStop(b *testing.B) {
	us129Command(b, "started:", "", os.Getenv, "stop")
}

func BenchmarkCommandRelease(b *testing.B) {
	us129Command(b, "stopped", "", us129AsAPerson, "release", "US-001", "--reason", "benchmarked")
}

func BenchmarkCommandDoctor(b *testing.B) {
	us129Command(b, us129AtImplementation, "", os.Getenv, "doctor")
}

// us129Review is the review a reviewer pipes to review add.
const us129Review = "# Security review\n\nNothing found.\n"
