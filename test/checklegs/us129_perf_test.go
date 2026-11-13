package checklegs

// US-129 AC-1, AC-10 and AC-11, the criteria that live in the
// repository rather than in a package: no base test file changed on the
// story's branch; `./task bench` and the benchmarks it runs; the
// `./task perf-report` tool.
//
// The names these rows look for are the test plan's (Gate 3), since nothing of
// AC-10 and AC-11 exists at the base:
//
//   - each benchmark of AC-10 is a top-level Benchmark function whose name,
//     lower-cased with everything but letters and digits taken out, holds
//     one of us129Benchmarks: BenchmarkHookPreToolUseBash,
//     BenchmarkCommandStoryList, BenchmarkRecordRead and so on;
//   - perf-report is a task of Taskfile.yml that runs one program under
//     ./internal/tools/, takes RUNS=<n> for the number of runs and
//     BASE=<binary> for the base's binary to compare with.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/testset"
)

// us129Events are AC-8's hook events, and us129Commands AC-3's commands, as
// a report or a refusal names them.
var (
	us129Events   = []string{"PreToolUse", "PostToolUse", "SubagentStart", "SubagentStop", "Stop", "SessionStart"}
	us129Commands = []string{"start", "status", "story list", "staffing", "artifact write", "review add",
		"review list", "gate", "freeze", "cost", "trace", "stop", "release", "doctor"}
)

// us129Benchmarks are the benchmarks AC-10 asks for, by the word each name
// holds.
var us129Benchmarks = []string{
	"hookpretoolusebash", "hookpretoolusewrite", "hookpretooluseedit", "hookposttooluse",
	"hooksubagentstart", "hooksubagentstop", "hookstop", "hooksessionstart",
	"recordread", "recordwrite", "backlogload", "configload",
	"commandstart", "commandstatus", "commandstorylist", "commandstaffing", "commandartifactwrite",
	"commandreviewadd", "commandreviewlist", "commandgate", "commandfreeze", "commandcost", "commandtrace",
	"commandstop", "commandrelease", "commanddoctor",
}

func us129Git(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// AC-1 as the story's branch has it: against the commit the branch left
// trunk at, nothing the configuration calls a test is changed, renamed or
// removed, committed or not. Off the branch the story is not being worked,
// and the row says so. It passes at the base by construction.
func TestAC1NoTestFileThatExistsAtTheStorysBaseIsChangedRenamedOrRemoved(t *testing.T) {
	branch, err := us129Git(t, "branch", "--show-current")
	if err != nil || branch != "story/US-129" {
		t.Skipf("AC-1 is US-129's branch against its base, and this checkout is on %q", branch)
	}
	cfg, err := config.Load(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	base, err := us129Git(t, "merge-base", "HEAD", "refs/heads/"+cfg.Git.TrunkBranch)
	if err != nil {
		t.Fatalf("no commit where story/US-129 left %s: %v", cfg.Git.TrunkBranch, err)
	}
	out, err := us129Git(t, "diff", "--name-status", "--find-renames", base)
	if err != nil {
		t.Fatal(err)
	}
	tests := testset.New(cfg.Paths.Tests)
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) < 2 || f[0] == "A" {
			continue
		}
		if was := filepath.ToSlash(f[1]); tests.Match(was) {
			t.Errorf("AC-1 %s, a test file at the base %s, is %s", was, base[:12], map[byte]string{
				'M': "changed", 'D': "removed", 'R': "renamed", 'C': "copied over", 'T': "changed in type",
			}[f[0][0]])
		}
	}
}

// us129Task runs task with args against the live Taskfile, as taskWith does,
// with the time a measuring task needs.
func us129Task(t *testing.T, taskfile string, args ...string) (string, error) {
	t.Helper()
	version := pinnedVersion(t, "TASK_VERSION")
	requireGoModule(t, "github.com/go-task/task/v3", version, "run any ./task command once with network access to fetch it")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	argv := append([]string{"run", "github.com/go-task/task/v3/cmd/task@v" + version, "-d", repoRoot(t), "-t", taskfile}, args...)
	cmd := exec.CommandContext(ctx, "go", argv...)
	cmd.Env = append(os.Environ(), fileProxyEnv(t), "npm_config_offline=true")
	out, err := cmd.CombinedOutput()
	return strings.ReplaceAll(string(out), "\r\n", "\n"), err
}

func us129Live(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "Taskfile.yml")
}

var us129Squash = regexp.MustCompile(`[^a-z0-9]+`)

func TestAC10TheBenchTaskRunsABenchmarkForEveryHookEventStoreLoadAndCommand(t *testing.T) {
	dry := dryRun(t, us129Live(t), "bench")
	if !strings.Contains(dry, "-bench") || !strings.Contains(dry, "./...") {
		t.Fatalf("AC-10 a dry run of bench runs no benchmarks of the whole module:\n%s", dry)
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-list", "^Benchmark", "./...")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test -list: %v\n%s", err, out)
	}
	byPackage := map[string][]string{}
	var names []string
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Benchmark"):
			names = append(names, line)
		case strings.HasPrefix(line, "ok "):
			if f := strings.Fields(line); len(f) > 1 && len(names) > 0 {
				byPackage[f[1]] = names
			}
			names = nil
		}
	}
	found := map[string]string{} // word -> package
	var runs []string
	for pkg, list := range byPackage {
		for _, name := range list {
			squashed := us129Squash.ReplaceAllString(strings.ToLower(strings.TrimPrefix(name, "Benchmark")), "")
			for _, word := range us129Benchmarks {
				if strings.Contains(squashed, word) {
					found[word] = pkg
					runs = append(runs, pkg+" "+name)
				}
			}
		}
	}
	for _, word := range us129Benchmarks {
		if _, ok := found[word]; !ok {
			t.Errorf("AC-10 no benchmark of the module is named for %s", word)
		}
	}
	if t.Failed() {
		return
	}
	// Each runs once, and has to run: one that fails or skips measures nothing.
	slices.Sort(runs)
	for _, r := range slices.Compact(runs) {
		pkg, name, _ := strings.Cut(r, " ")
		cmd := exec.CommandContext(t.Context(), "go", "test", "-run", "^$", "-bench", "^"+name+"$", "-benchtime", "1x", "-benchmem", "-v", pkg)
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		if err != nil || strings.Contains(string(out), "--- SKIP") || !strings.Contains(string(out), "allocs/op") {
			t.Errorf("AC-10 %s in %s does not run to a measure: %v\n%s", name, pkg, err, out)
		}
	}
}

// us129Program is the program under ./internal/tools/ the dry run of a task
// runs, or "".
func us129Program(dry string) string {
	m := regexp.MustCompile(`\./internal/tools/[a-z0-9_-]+`).FindString(dry)
	return m
}

func us129Says(out string, words ...string) bool {
	lower := strings.ToLower(out)
	return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(lower, strings.ToLower(w)) })
}

func TestAC11PerfReportTimesEveryEventAndCommandAndComparesTwoBinaries(t *testing.T) {
	if program := us129Program(dryRun(t, us129Live(t), "perf-report")); program == "" {
		t.Fatal("AC-11 perf-report runs no program under ./internal/tools/")
	}
	base := filepath.Join(t.TempDir(), "sdlc-base")
	if runtime.GOOS == "windows" {
		base += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", base, "./cmd/sdlc")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := us129Task(t, us129Live(t), "perf-report", "RUNS=3", "BASE="+base)
	if err != nil {
		t.Fatalf("AC-11 ./task perf-report failed: %v\n%s", err, out)
	}
	for _, name := range append(slices.Clone(us129Events), us129Commands...) {
		if !strings.Contains(out, name) {
			t.Errorf("AC-11 the report names no %s", name)
		}
	}
	for _, want := range [][]string{{"median", "p50"}, {"p95", "95th"}, {"wall"}, {"cpu"}, {"peak", "rss", "memory"}, {"base"}, {"change", "delta"}} {
		if !us129Says(out, want...) {
			t.Errorf("AC-11 the report says none of %q", want)
		}
	}
	if runtime.GOOS == "windows" && !us129Says(out, "not reported") {
		t.Error("AC-11 on Windows the peak memory column does not say it is not reported")
	}
}
