package cli

// US-112, design review round 2. The red team named the cheap readings the
// round-2 fixtures still let through (V2 to V5), and a careless operator's
// project: one initialised before this release has no .sdlc/.gitignore, so
// with worktrees off the untracked trace leaves the commit gate refused for
// good (red-team N1, human advocate's note 1). Security found that a named
// pipe at the trace's name hangs `sdlc trace` (N1), and that a store error
// with a path in it would put the path in the commit gate's debug line (I4).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// AC-18, V2: spans that overlap in part. An agent from 20m to 50m and a
// question open from 40m to 90m, in a story of two hours: the union is 20m to
// 90m, so the rest is 50m. The wall less the sum gives 40m, and the wall less
// the longest gives 70m.
func TestAC18TheRestOfSpansThatOverlapInPartIsTheWallLessTheirUnion(t *testing.T) {
	t.Parallel()
	d := traceOf(t, newGateRecord("T-1").
		pass(model.GateDoR, 10*time.Minute).
		agent("sdlc:researcher", "a1", 20*time.Minute, 50*time.Minute).
		escalate("spec_unclear", 40*time.Minute, 90*time.Minute).
		event("loop_start", "", 90*time.Minute).
		pass(model.GateAnalysis, 2*time.Hour),
		record(t, "compaction", 30*time.Minute, map[string]any{"trigger": "auto"}))
	seconds(t, "AC-18 agents' time", d.Total.Agents, 30*time.Minute)
	seconds(t, "AC-18 waiting on a person", d.Total.Person, 50*time.Minute)
	seconds(t, "AC-18 the rest, two hours less 20m to 90m", d.Total.Unaccounted, 50*time.Minute)
	seconds(t, "AC-18 analysis's rest, 10m to 2h less 20m to 90m", d.gate(t, model.GateAnalysis).Unaccounted, 40*time.Minute)
}

// AC-19, V3: first-pass success is the code review's and the verifier's. A
// block at the design review, or any review before the code is written,
// leaves it true.
func TestAC19ABlockBeforeTheCodeReviewKeepsFirstPassSuccess(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-3", "T-4", "T-5")
	walkedStory("T-3", 5*time.Hour, model.GateDesignReview).save(t, root)
	walkedStory("T-4", 5*time.Hour, model.GateTestsFrozen).save(t, root)
	walkedStory("T-5", 5*time.Hour, model.GateCodeReview).save(t, root)
	var doc struct {
		Stories []allRow `json:"stories"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, "trace", "--all", "--json").stdout), &doc); err != nil {
		t.Fatal(err)
	}
	rows := map[string]allRow{}
	for _, s := range doc.Stories {
		rows[s.Story] = s
	}
	if !rows["T-3"].FirstPass || !rows["T-4"].FirstPass || rows["T-5"].FirstPass {
		t.Errorf("AC-19 first-pass success: T-3 %v (a design review block), T-4 %v (a tests_frozen block), T-5 %v (a code review block); want true, true, false",
			rows["T-3"].FirstPass, rows["T-4"].FirstPass, rows["T-5"].FirstPass)
	}
}

// AC-19, V4: the text of --all has a row for a story under way, saying so; a
// text that leaves it out says nothing false about it, and nothing at all.
func TestAC19TheTextOfTraceAllHasARowForAStoryUnderWay(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-2", "T-7")
	walkedStory("T-2", 10*time.Hour, "").save(t, root)
	newGateRecord("T-7").pass(model.GateDoR, time.Minute).pass(model.GateAnalysis, 2*time.Minute).save(t, root)
	text := mustRun(t, "trace", "--all").stdout
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, "T-7") && strings.Contains(line, "in progress") {
			return
		}
	}
	t.Errorf("AC-19 no row of sdlc trace --all names T-7 as in progress:\n%s", text)
}

// AC-18, V5: the text gives durations as a person reads them, under columns
// headed in the story's words, and not as seconds.
func TestAC18TheTextGivesDurationsAsAPersonReadsThem(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	text := mustRun(t, "trace", "T-1").stdout
	lines := strings.Split(text, "\n")
	first := -1
	for gate, want := range map[string]string{"dor": "10m", "analysis": "35m", "tests_frozen": "3h15m"} {
		found := false
		for i, line := range lines {
			if !strings.Contains(line, gate) {
				continue
			}
			if inSeconds.MatchString(line) {
				t.Errorf("AC-18 %s's row gives its time in seconds: %s", gate, line)
			}
			if strings.Contains(line, want) {
				found = true
				if first < 0 || i < first {
					first = i
				}
			}
		}
		if !found {
			t.Errorf("AC-18 no row names %s with its wall time as %s:\n%s", gate, want, text)
		}
	}
	for i := range max(first, 0) {
		if strings.Contains(lines[i], "wall") && strings.Contains(lines[i], "agents") {
			return
		}
	}
	t.Errorf("AC-18 the gates' rows have no heading naming wall and agents above them:\n%s", text)
}

// AC-26, red-team N1 and the human advocate's note 1: a project initialised
// before this release has no .sdlc/.gitignore, and `sdlc init` is not run
// again. `sdlc start` writes it when it is not there, and leaves one that is
// as it is, the project's own lines and all.
func TestAC26StartIgnoresTheTraceInAProjectInitialisedBeforeTracing(t *testing.T) {
	t.Parallel()
	ignored := func(t *testing.T, root, rel string) bool {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", "check-ignore", "-q", "--no-index", rel)
		cmd.Dir = root
		return cmd.Run() == nil
	}
	t.Run("AC-26 no ignore file", func(t *testing.T) {
		t.Parallel()
		root := gitProjectInitialised(t)
		ignore := filepath.Join(root, ".sdlc", ".gitignore")
		if _, err := os.Stat(ignore); err == nil {
			if err := os.Remove(ignore); err != nil {
				t.Fatal(err)
			}
			commitEverything(t, root)
		}
		mustRun(t, "start")
		if _, err := os.Stat(ignore); err != nil {
			t.Fatalf("AC-26 sdlc start did not write .sdlc/.gitignore in a project that had none: %v", err)
		}
		story := decode[statusPayload](t, mustRun(t, "status", "--json")).Active
		if !ignored(t, root, ".sdlc/stories/"+story+"/trace.jsonl") {
			t.Error("AC-26 the trace is not ignored after sdlc start")
		}
		if ignored(t, root, ".sdlc/stories/"+story+"/gate-record.json") {
			t.Error("AC-26 sdlc start ignored the gate record, which the project decides")
		}
	})
	t.Run("AC-26 an ignore file the project keeps", func(t *testing.T) {
		t.Parallel()
		root := gitProjectInitialised(t)
		const own = "# the project's own\n/scratch/\n"
		writeFile(t, root, ".sdlc/.gitignore", own)
		commitEverything(t, root)
		mustRun(t, "start")
		if raw, _ := os.ReadFile(filepath.Join(root, ".sdlc", ".gitignore")); string(raw) != own {
			t.Errorf("AC-26 sdlc start rewrote the project's .sdlc/.gitignore: %q", raw)
		}
	})
}

// AC-21 and AC-22, security's N1: a named pipe at the story's trace is a
// trace that cannot be read. `sdlc trace` says so with SDLC-E0005 and does not
// wait for a writer; it never says the story predates tracing.
func TestAC21ATraceThatIsANamedPipeIsUnreadableAndNotWaitedOn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no named pipe in the file system, and no mkfifo")
	}
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-1")
	newGateRecord("T-1").pass(model.GateDoR, 10*time.Minute).save(t, root)
	pipe := filepath.Join(root, ".sdlc", "stories", "T-1", "trace.jsonl")
	if out, err := exec.CommandContext(t.Context(), "mkfifo", pipe).CombinedOutput(); err != nil {
		t.Skipf("mkfifo would not make a named pipe here: %v\n%s", err, out)
	}
	done := make(chan result, 1)
	go func() { done <- run(t, "trace", "T-1") }()
	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		for _, flag := range []int{os.O_RDONLY, os.O_WRONLY} {
			if f, err := os.OpenFile(pipe, flag|syscall.O_NONBLOCK, 0); err == nil {
				_ = f.Close()
			}
		}
		t.Fatal("N1 sdlc trace was still waiting on a named pipe after ten seconds")
	}
	if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0005") {
		t.Errorf("AC-21 sdlc trace on a named pipe exited %d, want SDLC-E0005:\n%s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout+r.stderr, "predates tracing") {
		t.Errorf("AC-21 sdlc trace says a story whose trace is a named pipe predates tracing:\n%s%s", r.stdout, r.stderr)
	}
}

// AC-10 and THREATS I4: the debug line for a diff that could not be measured
// says why, and never names a path, though the cause is an *fs.PathError
// from reading the freeze or the story, as it is or wrapped. Not parallel: it
// swaps a package variable and the logger.
func TestAC10ADiffFailureCausedByAPathErrorIsLoggedWithoutThePath(t *testing.T) {
	for name, wrap := range map[string]func(error) error{
		"as it is": func(err error) error { return err },
		"wrapped":  func(err error) error { return fmt.Errorf("reading the freeze: %w", err) },
		"under an sdlcerr": func(err error) error {
			return sdlcerr.New(sdlcerr.StateUnreadable, "the freeze could not be read", "").WithCause(err)
		},
	} {
		t.Run("AC-10 "+name, func(t *testing.T) {
			root := toTheCommitGate(t)
			cause := &fs.PathError{Op: "open", Path: filepath.Join(root, "I4-MARKER", "tests.lock"), Err: fs.ErrPermission}
			before := measureChange
			measureChange = func(context.Context, string, string, string, ...string) ([]gitx.FileStat, error) {
				return nil, wrap(cause)
			}
			t.Cleanup(func() { measureChange = before })
			var log bytes.Buffer
			logger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(logger) })
			if r := run(t, "gate", "commit", "pass", "--note", "measured"); r.code != 0 {
				t.Fatalf("the commit gate did not pass where the change could not be measured:\n%s", r.stderr)
			}
			if strings.Contains(log.String(), "I4-MARKER") || strings.Contains(log.String(), filepath.ToSlash(filepath.Join(root, "I4-MARKER"))) {
				t.Errorf("I4 the debug log names the path in the error's cause:\n%s", log.String())
			}
			if !strings.Contains(log.String(), fs.ErrPermission.Error()) {
				t.Errorf("AC-10 the debug log does not say why the diff was not recorded (%q):\n%s", fs.ErrPermission, log.String())
			}
			if !errors.Is(wrap(cause), fs.ErrPermission) {
				t.Fatal("the wrapped cause is not the permission error, so the test shows nothing")
			}
		})
	}
}

// inSeconds is dor's, analysis's or tests_frozen's wall time in seconds.
var inSeconds = regexp.MustCompile(`\b(600|2100|11700)\b`)
