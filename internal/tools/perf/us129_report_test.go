package main

// US-129 AC-11 in the tool's own package: test/checklegs runs ./task
// perf-report as a process, which no coverage profile of this package sees.
// Here the report is run in this process against the checkout, with a base
// binary built from it, and its parts are held to what AC-11 asks of them:
// each name's median and 95th percentile of wall time, CPU time and peak
// memory, the base's beside them with the change, and a column a platform
// does not report saying so rather than showing zero.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }

// us129Checkout is the root of the checkout this package is in.
func us129Checkout(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("%s is not the checkout's root: %v", dir, err)
	}
	return dir
}

func TestAC11TheReportTimesEveryNameAgainstABaseWithItsMedianAnd95thPercentile(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary twice and runs every name of the report; not run with -short")
	}
	checkout := us129Checkout(t)
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	base := filepath.Join(t.TempDir(), "sdlc-base"+exeSuffix)
	build := exec.CommandContext(t.Context(), "go", "build", "-o", base, "./cmd/sdlc")
	build.Dir = checkout
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	t.Chdir(checkout)
	var out, errb bytes.Buffer
	if code := run([]string{"report", "-runs", "1", "-base", base}, &out, &errb); code != 0 {
		t.Fatalf("AC-11 perf report exited %d:\n%s%s", code, out.String(), errb.String())
	}
	report := out.String()
	figure := `\d+\.\d, \d+\.\d`
	rssCol := `\d+, \d+`
	if runtime.GOOS == windows {
		rssCol = "not reported"
	}
	for _, n := range names(filepath.Join(tmp, "repo")) {
		row := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(n.name) + `\s+` + n.class + `\s+` + figure + `\s+` + figure + `\s+` + rssCol + `\s+.*\(`)
		if !row.MatchString(report) {
			t.Errorf("AC-11 the report has no row for %s with p50 and p95 of wall, CPU and peak memory and the base's change:\n%s", n.name, report)
		}
	}
	for _, want := range []string{"1 runs of each name", "the base " + base, "wall ms (p50, p95)", "cpu ms (p50, p95)",
		"peak rss KB (p50, p95)", "base p50 wall, cpu, rss (change)", sandboxSizes, "start-up split of one PreToolUse call"} {
		if !strings.Contains(report, want) {
			t.Errorf("AC-11 the report does not say %q:\n%s", want, report)
		}
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Errorf("AC-11 the report left %d entries in the temporary directory", len(entries))
	}
}

func TestAC11TheReportRefusesArgumentsItDoesNotTakeAndMeasuresNothing(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"measure"}, {"report"}, {"report", "-runs", "0"}, {"report", "-runs", "x"},
		{"report", "-runs", "1", "extra"}, {"report", "-nothing"}} {
		var out, errb bytes.Buffer
		code := run(args, &out, &errb)
		if code != 2 || out.Len() != 0 || !strings.Contains(errb.String(), "nothing was measured") ||
			!strings.Contains(errb.String(), "./task perf-report") {
			t.Errorf("AC-11 perf %q exited %d, printed %q and said %q; want 2, nothing on stdout, and the task to run",
				args, code, out.String(), errb.String())
		}
	}
}

func TestAC11TheReportOutsideTheCheckoutsRootSaysWhereToRunIt(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	if code := run([]string{"report", "-runs", "1"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no go.mod") {
		t.Errorf("AC-11 perf report outside the checkout exited %d and said %q, want 1 naming go.mod", code, errb.String())
	}
}

func TestAC11PercentilesAreNearestRankAndAnUnreportedMeasureSaysSo(t *testing.T) {
	t.Parallel()
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	for p, want := range map[int]float64{50: 10, 95: 19, 100: 20, 1: 1} {
		if got := percentile(sorted, p); got != want {
			t.Errorf("AC-11 p%d of 1..20 is %v, want %v", p, got, want)
		}
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("AC-11 p50 of nothing is %v, want 0", got)
	}
	if got := percentile([]float64{7}, 95); got != 7 {
		t.Errorf("AC-11 p95 of one run is %v, want that run", got)
	}

	runs := []measures{{wall: 30 * time.Millisecond, cpu: 3 * time.Millisecond, rss: 300, rssKnown: true},
		{wall: 10 * time.Millisecond, cpu: time.Millisecond, rss: 100, rssKnown: true},
		{wall: 20 * time.Millisecond, cpu: 2 * time.Millisecond, rss: 200, rssKnown: true}}
	for measure, want := range map[string]string{wall: "20.0, 30.0", cpu: "2.0, 3.0", rss: "200, 300"} {
		if got := pair(runs, measure); got != want {
			t.Errorf("AC-11 %s over three runs is %q, want %q", measure, got, want)
		}
	}
	unknown := []measures{{wall: 10 * time.Millisecond}}
	if got := pair(unknown, rss); got != "not reported" {
		t.Errorf("AC-11 peak memory a platform does not report reads %q, want \"not reported\"", got)
	}
	if got := pair(nil, rss); got != "not reported" {
		t.Errorf("AC-11 peak memory of no runs reads %q, want \"not reported\"", got)
	}
	base := []measures{{wall: 40 * time.Millisecond, cpu: 4 * time.Millisecond, rss: 400, rssKnown: true}}
	if got, want := changes(runs, base), "40.0 (-50%); 4.0 (-50%); 400 (-50%)"; got != want {
		t.Errorf("AC-11 the change against a base is %q, want %q", got, want)
	}
	if got, want := changes(runs, unknown), "10.0 (+100%); 0.0; n/a"; got != want {
		t.Errorf("AC-11 the change against a base with no memory and no CPU is %q, want %q", got, want)
	}
}

func TestAC11EveryNameIsTimedOnTheSandboxItNeedsAndEachSandboxIsBuiltOnce(t *testing.T) {
	t.Parallel()
	list := names("/repo")
	got := kinds(list)
	seen := map[string]bool{}
	for _, k := range got {
		if seen[k] {
			t.Errorf("AC-11 the sandbox %s is built twice", k)
		}
		seen[k] = true
	}
	for _, n := range list {
		if !seen[n.on] {
			t.Errorf("AC-11 %s runs on %s, which is never built", n.name, n.on)
		}
		if n.ok == nil {
			t.Errorf("AC-11 %s has no check of its exit", n.name)
		}
	}
	for _, n := range list {
		want := classCommand
		switch {
		case n.launcher:
			want = "launcher path"
		case n.event != "":
			want = "hook event"
		}
		if n.kind() != want {
			t.Errorf("AC-11 %s is a %s, want %s", n.name, n.kind(), want)
		}
	}
}

func TestAC11ARunThatDoesNotEndAsExpectedFailsTheReport(t *testing.T) {
	dir := t.TempDir()
	sb := &sandbox{dir: dir, repo: filepath.Join(dir, "repo"), home: filepath.Join(dir, "home"), tmp: filepath.Join(dir, "tmp"),
		snapshot: map[string]snap{"kept.txt": {data: []byte("x\n"), mode: fileMode, modTime: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH")
	}
	refused := name{name: "a refusal", args: []string{"no-such-subcommand"}, ok: exits0}
	if _, err := sb.once(t.Context(), git, refused); err == nil || !strings.Contains(err.Error(), "a refusal command") {
		t.Errorf("AC-11 a run that exited non-zero was timed as the work: %v", err)
	}
	missing := name{name: "no program", args: []string{"version"}, ok: exits0}
	if _, err := sb.once(t.Context(), filepath.Join(dir, "absent"), missing); err == nil || !strings.Contains(err.Error(), "did not run") {
		t.Errorf("AC-11 a program that could not start was timed: %v", err)
	}
	ok := name{name: "git version", args: []string{"version"}, ok: exits0}
	m, err := sb.once(t.Context(), git, ok)
	if err != nil || m.wall <= 0 {
		t.Errorf("AC-11 a run that ended as expected gave %+v (%v)", m, err)
	}
	if runtime.GOOS != windows && !m.rssKnown {
		t.Error("AC-11 a Unix run's peak memory is not reported")
	}
}

func TestAC11TheSandboxIsPutBackAsItsSnapshotHadIt(t *testing.T) {
	dir := t.TempDir()
	sb := &sandbox{dir: dir, repo: filepath.Join(dir, "repo")}
	if err := sb.write("a/b.txt", "kept\n"); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(sb.repo, "a", "b.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := sb.take(); err != nil {
		t.Fatal(err)
	}
	if err := sb.write("a/b.txt", "changed by a run\n"); err != nil {
		t.Fatal(err)
	}
	if err := sb.write("new.txt", "written by a run\n"); err != nil {
		t.Fatal(err)
	}
	if err := sb.restore(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(sb.repo, "a", "b.txt"))
	if err != nil || string(raw) != "kept\n" {
		t.Errorf("AC-11 after the restore the file holds %q (%v), want what the snapshot had", raw, err)
	}
	if info, err := os.Stat(filepath.Join(sb.repo, "a", "b.txt")); err != nil || !info.ModTime().Equal(old) {
		t.Errorf("AC-11 after the restore the file's time is not the snapshot's: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sb.repo, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("AC-11 a file a run wrote outlived the restore: %v", err)
	}
	sb.close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("AC-11 the sandbox's directory outlived its close: %v", err)
	}
}

func TestAC11AChildSeesNoneOfTheCallersSessionAndOnlyTheVariablesItNeeds(t *testing.T) {
	t.Setenv(store.SessionEnv, "the caller's")
	t.Setenv("GIT_DIR", "/elsewhere")
	sb := &sandbox{home: "/h", tmp: "/t"}
	for _, inSession := range []bool{false, true} {
		env := sb.env(inSession, "EXTRA=1")
		joined := strings.Join(env, "\n")
		if strings.Contains(joined, "the caller's") || strings.Contains(joined, "GIT_DIR=") {
			t.Errorf("AC-11 a child's environment carries the caller's: %v", env)
		}
		if got := strings.Contains(joined, "="+session); got != inSession {
			t.Errorf("AC-11 a child in session %v is marked with the sandbox's session: %v", inSession, got)
		}
		if !strings.HasSuffix(joined, "EXTRA=1") || !strings.Contains(joined, "GIT_CONFIG_GLOBAL="+os.DevNull) {
			t.Errorf("AC-11 a child's environment lacks what it is given: %v", env)
		}
	}
}
