package cli

// US-134 in process: `sdlc gate implementation pass` runs the configured
// commands the landing runs, in the story's worktree, refuses while one fails
// and records what passed against the tree hash; `sdlc status --json` says
// whether that result is for the tree as it is now; `sdlc stop` lands on it
// without running the commands again only where the tree and the commands are
// the ones recorded; and `sdlc freeze` runs the new and changed tests and
// refuses where they pass.
//
// Every command here reports from inside itself into a file outside the
// project: which key it is, whether it ran in the story's worktree, the
// SDLC_RUN_DIR it was handed and what that directory held when it began. Nothing
// reads a command's text back, and the commands vary, so neither special-casing
// one nor ignoring one passes. Refusals are matched by the code they print.
//
// Where the story leaves a shape to the plan, these tests pin the one ANALYSIS.md
// recommends, and TEST-PLAN.md lists each: SDLC-E0095 for the gate's refusal,
// gates.implementation.commands (name, exit, duration_ms) and tree_hash on the
// record, freeze_run beside the freeze, and a commands object in
// `sdlc status --json`. Helpers carry an rc prefix, for run commands.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	// rcCode is the gate's refusal of a command that failed (AC-2), spelled here
	// and not taken from the code, so that a renumbered code fails.
	rcCode = "SDLC-E0095"
	// rcLandingCode is the landing's own refusal of a command, which it keeps
	// (ANALYSIS 6.2, reading (b)).
	rcLandingCode = "SDLC-E0055"
	// rcAnyCode is a freeze refusal, whose code AC-13 leaves to the plan.
	rcAnyCode = "SDLC-E"
)

// rcLanding is landingCommands as AC-1 names them, in the landing's order.
var rcLanding = []string{"build", "test", "lint", "fmt_check", "coverage", "mutation"}

// rcSpy is the file every command of a test reports into.
type rcSpy struct{ log string }

func newRCSpy(t *testing.T) rcSpy {
	t.Helper()
	return rcSpy{log: filepath.Join(wtTempDir(t), "ran")}
}

// reports is the shell words with which a command named key reports itself and
// then does then. refund.go is a file only the story's worktree has (ldWork), so
// finding it says the command ran at the worktree's root.
func (s rcSpy) reports(key, then string) string {
	return `d="${SDLC_RUN_DIR-}"; e=absent; h=""; if [ -n "$d" ] && [ -d "$d" ]; then e=present; h=$(ls -A "$d" | tr '\n' ','); fi; ` +
		`w=elsewhere; [ -f refund.go ] && w=worktree; ` +
		`printf '%s|%s|%s|%s|%s\n' '` + key + `' "$w" "$d" "$e" "$h" >> ` + suSh(s.log) + `; ` + then
}

// rcRun is one command's report.
type rcRun struct{ key, where, dir, exists, held string }

func (s rcSpy) runs(t *testing.T) []rcRun {
	t.Helper()
	var out []rcRun
	for line := range strings.Lines(wtRead(t, s.log)) {
		f := strings.SplitN(strings.TrimRight(line, "\r\n"), "|", 5)
		if len(f) != 5 {
			t.Fatalf("a command reported %q", line)
		}
		out = append(out, rcRun{key: f[0], where: f[1], dir: f[2], exists: f[3], held: f[4]})
	}
	return out
}

func (s rcSpy) keys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for _, r := range s.runs(t) {
		keys = append(keys, r.key)
	}
	return keys
}

func (s rcSpy) count(t *testing.T, key string) int {
	t.Helper()
	n := 0
	for _, r := range s.runs(t) {
		if r.key == key {
			n++
		}
	}
	return n
}

// rcEvery is each landing command reporting itself and passing: coverage and
// mutation print a figure above rcThresholds'.
func (s rcSpy) rcEvery() map[string]string {
	commands := map[string]string{}
	for _, key := range rcLanding {
		commands[key] = s.reports(key, "exit 0")
	}
	commands["coverage"] = s.reports("coverage", "echo 90")
	commands["mutation"] = s.reports("mutation", "echo 80")
	return commands
}

var rcThresholds = map[string]any{"coverage_min": 80, "mutation_min": 70}

// rcAtTheGate is US-001 in its worktree at the implementation, its work written
// and not committed, with commands and thresholds as the worktree's own copy of
// the configuration gives them: the copy `sdlc gate` reads.
func rcAtTheGate(t *testing.T, commands map[string]string) (main, wt string) {
	t.Helper()
	main, wt = ldWalked(t, ldWalk{})
	ldWork(t, wt)
	setConfigOnDisk(t, wt, "commands", commands)
	setConfigOnDisk(t, wt, "thresholds", rcThresholds)
	return main, wt
}

func rcGate(t *testing.T, wt string) result {
	t.Helper()
	return wtRun(t, wt, ldSession, "", "gate", "implementation", "pass", "--note", "the work is done")
}

func rcGatePasses(t *testing.T, wt string) {
	t.Helper()
	if r := rcGate(t, wt); r.code != 0 {
		t.Fatalf("sdlc gate implementation pass was refused:\n%s%s", r.stdout, r.stderr)
	}
}

// rcGateEntry is the implementation gate as the record on disk holds it.
func rcGateEntry(t *testing.T, main string) map[string]any {
	t.Helper()
	gates, _ := lxRecord(t, main)["gates"].(map[string]any)
	entry, _ := gates[string(model.GateImplementation)].(map[string]any)
	return entry
}

// rcResults is what the implementation gate recorded of each command.
func rcResults(t *testing.T, main string) []map[string]any {
	t.Helper()
	list, _ := rcGateEntry(t, main)["commands"].([]any)
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func rcResultNames(results []map[string]any) []string {
	var names []string
	for _, r := range results {
		name, _ := r["name"].(string)
		names = append(names, strings.TrimPrefix(name, commandsPrefix))
	}
	return names
}

// rcSubject is the tree hash the loop stamps reviews and approvals with, of the
// checkout at root as it is now.
func rcSubject(t *testing.T, root string) string {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// rcStatusCommands is the commands object `sdlc status --json` gives in wt.
func rcStatusCommands(t *testing.T, wt string) map[string]any {
	t.Helper()
	var status map[string]any
	r := wtMust(t, wt, ldSession, "", "status", "--json")
	if err := json.Unmarshal([]byte(r.stdout), &status); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, r.stdout)
	}
	commands, _ := status["commands"].(map[string]any)
	return commands
}

func rcCurrent(t *testing.T, wt string) bool {
	t.Helper()
	current, _ := rcStatusCommands(t, wt)["current"].(bool)
	return current
}

var rcStoryPath = regexp.MustCompile(`[^\s"'` + "`" + `]*\.sdlc[/\\]stories[/\\]US-001[/\\][^\s"'` + "`" + `]+`)

// rcOutputFile is the file a refusal names under .sdlc/stories/US-001/, and what
// it holds: the refusal may give it from the main checkout's root, from the
// worktree's, or whole.
func rcOutputFile(t *testing.T, said string, roots ...string) string {
	t.Helper()
	for _, found := range rcStoryPath.FindAllString(said, -1) {
		found = strings.TrimRight(found, ".,;:)")
		candidates := []string{found}
		if !filepath.IsAbs(found) {
			candidates = nil
			for _, root := range roots {
				candidates = append(candidates, filepath.Join(root, filepath.FromSlash(found)))
			}
		}
		for _, c := range candidates {
			if raw, err := os.ReadFile(c); err == nil && !strings.HasSuffix(c, model.RecordFile) {
				return string(raw)
			}
		}
	}
	t.Fatalf("the refusal names no file under .sdlc/stories/US-001/ that is there:\n%s", said)
	return ""
}

// rcStoryFiles is every file directly under .sdlc/stories/US-001/ in main but the
// record, with what it holds.
func rcStoryFiles(t *testing.T, main string) map[string]string {
	t.Helper()
	dir := filepath.Join(main, ".sdlc", "stories", "US-001")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || e.Name() == model.RecordFile {
			continue
		}
		files[e.Name()] = wtRead(t, filepath.Join(dir, e.Name()))
	}
	return files
}

// rcNumbered prints fifty numbered lines, as a test runner's output.
const rcNumbered = `i=1; while [ $i -le 50 ]; do printf 'rc-line-%02d\n' $i; i=$((i+1)); done`

func rcEvents(t *testing.T, main, message string) int {
	t.Helper()
	n := 0
	events, _ := lxRecord(t, main)["events"].([]any)
	for _, e := range events {
		if m, ok := e.(map[string]any); ok && m["type"] == model.EventGate && m["message"] == message {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------- AC-1

// AC-1: the gate runs build, test, lint, fmt_check, coverage and mutation, the
// ones set, in that order, at the root of the story's worktree, before it
// records the pass; the commands the landing does not run are not run here.
func TestAC1TheImplementationGateRunsTheLandingsCommandsInTheWorktreeInTheLandingsOrder(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 every landing command set", func(t *testing.T) {
		t.Parallel()
		spy := newRCSpy(t)
		commands := spy.rcEvery()
		for _, other := range []string{"smoke", "npm_test", "freeze_check", "test_files"} {
			commands[other] = spy.reports(other, "exit 0")
		}
		main, wt := rcAtTheGate(t, commands)
		rcGatePasses(t, wt)
		if got := spy.keys(t); !slices.Equal(got, rcLanding) {
			t.Errorf("AC-1 the gate ran %q, want %q in that order", got, rcLanding)
		}
		for _, r := range spy.runs(t) {
			if r.where != "worktree" {
				t.Errorf("AC-1 commands.%s ran %s, not at the root of the story's worktree", r.key, r.where)
			}
		}
		if got := rcGateEntry(t, main)["status"]; got != string(model.GatePass) {
			t.Errorf("AC-1 the implementation gate is %v after its commands passed, want pass", got)
		}
	})
	t.Run("AC-1 only those set", func(t *testing.T) {
		t.Parallel()
		spy := newRCSpy(t)
		_, wt := rcAtTheGate(t, map[string]string{
			"test": spy.reports("test", "exit 0"), "lint": spy.reports("lint", "exit 0"), "build": "  ",
		})
		rcGatePasses(t, wt)
		if got := spy.keys(t); !slices.Equal(got, []string{"test", "lint"}) {
			t.Errorf("AC-1 the gate ran %q, want only test and lint, the two set", got)
		}
	})
}

// AC-1: the thresholds the landing holds coverage and mutation to hold them at
// the gate too: the last number on standard output, at least the threshold.
func TestAC1TheGateHoldsCoverageAndMutationToTheThresholdsTheLandingApplies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, key, command string
		refused            bool
	}{
		{"AC-1 coverage below its threshold", "coverage", "echo 'covered 90 of 100'; echo 79.9", true},
		{"AC-1 coverage at its threshold", "coverage", "echo 80", false},
		{"AC-1 mutation below its threshold", "mutation", "echo 69", true},
		{"AC-1 a figure on standard error only", "coverage", "echo 90 >&2; echo 12", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spy := newRCSpy(t)
			commands := spy.rcEvery()
			commands[tc.key] = spy.reports(tc.key, tc.command)
			_, wt := rcAtTheGate(t, commands)
			r := rcGate(t, wt)
			if !tc.refused {
				if r.code != 0 {
					t.Fatalf("AC-1 %s was refused:\n%s", tc.name, r.stderr)
				}
				return
			}
			if r.code == 0 || !strings.Contains(r.stderr, rcCode) {
				t.Fatalf("%s did not refuse the gate with %s: exit %d\n%s%s", tc.name, rcCode, r.code, r.stdout, r.stderr)
			}
			suSays(t, r.stderr, "commands."+tc.key)
		})
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: a failing command refuses the gate with the new code, naming the first
// command that failed and its exit status, and the file under
// .sdlc/stories/US-001/ that holds all it printed; its last 20 lines are on
// stderr, and nothing is on stdout, which a skill reads as data.
func TestAC2AFailingCommandRefusesTheGateNamingItItsStatusAndTheFileThatHoldsItsOutput(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	commands := spy.rcEvery()
	commands["test"] = spy.reports("test", rcNumbered+"; exit 3")
	commands["lint"] = spy.reports("lint", "echo lint-is-not-the-first; exit 4")
	main, wt := rcAtTheGate(t, commands)

	r := rcGate(t, wt)
	if r.code == 0 {
		t.Fatalf("AC-2 the gate passed with commands.test failing:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, rcCode) {
		t.Fatalf("AC-2 the refusal does not carry %s:\n%s", rcCode, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("AC-2 a refusal wrote to stdout: %q", r.stdout)
	}
	said, _ := ldRefusal(t, r.stderr, rcCode)
	what, _, _ := strings.Cut(said, "\n")
	if !strings.Contains(what, "commands.test") || strings.Contains(what, "commands.lint") {
		t.Errorf("AC-2 the refusal's first line does not name commands.test, the first command that failed: %q", what)
	}
	if !regexp.MustCompile(`status 3\b`).MatchString(r.stderr) {
		t.Errorf("AC-2 the refusal does not give commands.test's exit status, 3:\n%s", r.stderr)
	}
	body := rcOutputFile(t, r.stderr, main, wt)
	for i := 1; i <= 50; i++ {
		if line := fmt.Sprintf("rc-line-%02d", i); !strings.Contains(body, line) {
			t.Errorf("AC-2 the output file does not hold %s: it is to hold all commands.test printed", line)
		}
	}
	for i := 1; i <= 50; i++ {
		line := fmt.Sprintf("rc-line-%02d", i)
		switch shown := strings.Contains(r.stderr, line); {
		case i >= 32 && !shown:
			t.Errorf("AC-2 %s, one of the last 20 lines, is not on stderr", line)
		case i <= 30 && shown:
			t.Errorf("AC-2 %s is on stderr, which carries only the last 20 lines", line)
		}
	}
	if strings.Contains(r.stderr, "lint-is-not-the-first") {
		t.Errorf("AC-2 the refusal shows what a later command printed:\n%s", r.stderr)
	}
}

// AC-2: a refused run counts as a recorded fail of the gate: the gate stands
// failed, the event `implementation fail` is on the record, and at
// loop.max_rework_rounds the story is handed to a person, as a recorded fail
// hands it over.
func TestAC2ARefusedRunIsRecordedAsAFailOfTheGateAndCountsTowardsTheReworkLimit(t *testing.T) {
	t.Parallel()
	main, wt := rcAtTheGate(t, map[string]string{"test": "echo the test failed; exit 1"})
	setConfigOnDisk(t, wt, "loop", map[string]any{"max_review_rounds": 2, "max_rework_rounds": 2, "max_stop_blocks": 3})
	failed := model.GateEvent(model.GateImplementation, model.GateFail)

	if r := rcGate(t, wt); r.code == 0 || !strings.Contains(r.stderr, rcCode) {
		t.Fatalf("AC-2 the gate was not refused with %s: exit %d\n%s", rcCode, r.code, r.stderr)
	}
	if got := rcGateEntry(t, main)["status"]; got != string(model.GateFail) {
		t.Errorf("AC-2 after a refused run the implementation gate is %v, want fail", got)
	}
	if got := rcEvents(t, main, failed); got != 1 {
		t.Errorf("AC-2 the record has %d %q events after one refused run, want 1", got, failed)
	}
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusAwaitingHuman) {
		t.Fatalf("AC-2 one refused run of two allowed handed the story over")
	}

	r := rcGate(t, wt)
	if r.code == 0 {
		t.Fatalf("AC-2 the second run passed:\n%s", r.stdout)
	}
	if got := rcEvents(t, main, failed); got != 2 {
		t.Errorf("AC-2 the record has %d %q events after two refused runs, want 2", got, failed)
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusAwaitingHuman) {
		t.Errorf("AC-2 at loop.max_rework_rounds the story is %q, want it handed to a person (awaiting_human)", got)
	}
	found := false
	escalations, _ := lxRecord(t, main)["escalations"].([]any)
	for _, e := range escalations {
		if m, ok := e.(map[string]any); ok && m["type"] == "gate_failing" {
			found = true
		}
	}
	if !found {
		t.Errorf("AC-2 the hand-over at the limit is not the gate_failing one a recorded fail makes: %v", escalations)
	}
}

// ---------------------------------------------------------------- AC-3

// AC-3: a passed run is recorded under the implementation gate, each command
// that ran with its name, exit status and duration, and the tree hash it ran
// on, which is the one reviews and approvals are stamped with.
func TestAC3APassedRunRecordsEachCommandsNameStatusAndDurationAndTheTreeHash(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	commands := map[string]string{
		"build":    spy.reports("build", "exit 0"),
		"test":     spy.reports("test", "exit 0"),
		"coverage": spy.reports("coverage", "echo 90"),
	}
	main, wt := rcAtTheGate(t, commands)
	want := rcSubject(t, wt)
	rcGatePasses(t, wt)

	results := rcResults(t, main)
	if got := rcResultNames(results); !slices.Equal(got, []string{"build", "test", "coverage"}) {
		t.Fatalf("AC-3 the gate recorded %q, want build, test and coverage, the commands that ran: %v", got, rcGateEntry(t, main))
	}
	for _, r := range results {
		if exit, ok := r["exit"].(float64); !ok || exit != 0 {
			t.Errorf("AC-3 %v has no exit status of 0", r)
		}
		if ms, ok := r["duration_ms"].(float64); !ok || ms < 0 {
			t.Errorf("AC-3 %v has no duration in milliseconds", r)
		}
	}
	if got := rcGateEntry(t, main)["tree_hash"]; got != want {
		t.Errorf("AC-3 the gate recorded tree hash %v, want %s, the tree the commands ran on", got, want)
	}
	raw := wtRead(t, wtRecordPath(main, "US-001"))
	for _, command := range commands {
		if strings.Contains(raw, command) {
			t.Errorf("AC-3 the record holds a command's text, which can hold a token and is committed")
		}
	}
}

// AC-3, THREATS B1: what a pass recorded does not outlive it. The gate recorded
// again as a fail, or reopened by an earlier gate, holds no result.
func TestAC3AResultDoesNotOutliveThePassThatRecordedIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		then []string
	}{
		{"AC-3 the gate recorded fail", []string{"gate", "implementation", "fail", "--note", "the reviewer found a hole"}},
		{"AC-3 the gate recorded pending", []string{"gate", "implementation", "pending"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := rcAtTheGate(t, map[string]string{"test": "exit 0"})
			rcGatePasses(t, wt)
			if len(rcResults(t, main)) == 0 {
				t.Fatal("AC-3 the pass recorded no result to outlive")
			}
			wtMust(t, wt, ldSession, "", tc.then...)
			entry := rcGateEntry(t, main)
			if _, kept := entry["commands"]; kept {
				t.Errorf("AC-3 the results stayed on the gate: %v", entry)
			}
			if _, kept := entry["tree_hash"]; kept {
				t.Errorf("AC-3 the tree hash stayed on the gate: %v", entry)
			}
			if rcCurrent(t, wt) {
				t.Error("AC-3 status still calls the result current")
			}
		})
	}
}

// ---------------------------------------------------------------- AC-4

// AC-4: the binary says whether it holds results for the worktree's tree as it
// is now, which is what the runbook gives the verifier: true after the pass, and
// false once the tree, a command or a threshold differs from the one recorded.
func TestAC4StatusSaysWhetherTheResultIsForTheTreeAndTheCommandsAsTheyAreNow(t *testing.T) {
	t.Parallel()
	_, wt := rcAtTheGate(t, map[string]string{"test": "exit 0", "coverage": "echo 90"})
	if rcCurrent(t, wt) {
		t.Fatal("AC-4 status says a result is current before any command ran")
	}
	rcGatePasses(t, wt)
	commands := rcStatusCommands(t, wt)
	if commands["current"] != true {
		t.Fatalf("AC-4 status does not say the result just recorded is current: %v", commands)
	}
	if got := commands["tree_hash"]; got != rcSubject(t, wt) {
		t.Errorf("AC-4 status gives tree hash %v, want the worktree's, %s", got, rcSubject(t, wt))
	}
	list, _ := commands["results"].([]any)
	var names []string
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			name, _ := m["name"].(string)
			names = append(names, strings.TrimPrefix(name, commandsPrefix))
		}
	}
	if !slices.Equal(names, []string{"test", "coverage"}) {
		t.Errorf("AC-4 status gives the results %q, want test and coverage", names)
	}

	t.Run("AC-4 the tree changed", func(t *testing.T) {
		writeFile(t, wt, "refund.go", "package billing\n\ntype Refund struct{ Reason string }\n")
		if rcCurrent(t, wt) {
			t.Error("AC-4 status calls a result current for a tree that has changed since")
		}
		writeFile(t, wt, "refund.go", "package billing\n\n// Refund is what US-001 adds.\ntype Refund struct{}\n")
		if !rcCurrent(t, wt) {
			t.Error("AC-4 the tree put back as it was is not current again")
		}
	})
	t.Run("AC-4 a command changed", func(t *testing.T) {
		setConfigOnDisk(t, wt, "commands", map[string]string{"test": "true", "coverage": "echo 90"})
		if rcCurrent(t, wt) {
			t.Error("AC-4 status calls a result current though commands.test is not the command that ran")
		}
		setConfigOnDisk(t, wt, "commands", map[string]string{"test": "exit 0", "coverage": "echo 90"})
	})
	t.Run("AC-4 a threshold changed", func(t *testing.T) {
		setConfigOnDisk(t, wt, "thresholds", map[string]any{"coverage_min": 10, "mutation_min": 70})
		if rcCurrent(t, wt) {
			t.Error("AC-4 status calls a result current though thresholds.coverage_min is not the one it was held to")
		}
	})
}

// ---------------------------------------------------------------- AC-6

// rcFinishedWith is US-001 finished in its worktree, its commands trunk's:
// every landing command passing, the test one reporting to spy, and the narrow
// run of its new tests failing at the freeze, as new tests do.
func rcFinishedWith(t *testing.T, spy rcSpy) (main, wt string) {
	t.Helper()
	commands := map[string]string{"test": spy.reports("test", "exit 0"), "test_files": "exit 1"}
	main = lxRepository(t, commands, nil)
	return main, ldFinished(t, main)
}

// AC-6: a story whose tree and commands are the ones recorded lands on that
// result, with no command run again, and says so in one line that names the
// tree it was recorded for.
func TestAC6AStoryLandsOnTheResultRecordedForItsTreeWithoutRunningTheCommandsAgain(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	main, wt := rcFinishedWith(t, spy)
	if got := spy.count(t, "test"); got != 1 {
		t.Fatalf("AC-6 commands.test ran %d times before the landing, want once, at the implementation gate", got)
	}
	tree, _ := rcGateEntry(t, main)["tree_hash"].(string)
	if len(tree) < 12 {
		t.Fatalf("AC-6 the implementation gate recorded no tree hash: %v", rcGateEntry(t, main))
	}
	tip := swRev(t, wt, "HEAD")

	r := wtMust(t, wt, ldSession, "", "stop")
	if trunk := ldTrunk(t, main); trunk != tip {
		t.Fatalf("AC-6 trunk is at %s after the stop, want the story's tip %s:\n%s%s", trunk, tip, r.stdout, r.stderr)
	}
	if got := spy.count(t, "test"); got != 1 {
		t.Errorf("AC-6 the landing ran commands.test again on the tree it was recorded for (%d runs)", got)
	}
	saying := 0
	for line := range strings.Lines(r.stdout + r.stderr) {
		if strings.Contains(line, tree[:12]) {
			saying++
		}
	}
	if saying != 1 {
		t.Errorf("AC-6 %d lines of the stop name the recorded tree %s, want one saying it landed on that result:\n%s%s",
			saying, tree[:12], r.stdout, r.stderr)
	}
}

// AC-6, THREATS B1: a record with no result, as an sdlc before this one wrote,
// is not read as a pass: the landing runs the commands, where before it ran none
// on a story neither rebased nor changed.
func TestAC6ARecordWithNoResultHasTheCommandsRunAtTheLanding(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	main, wt := rcFinishedWith(t, spy)
	path := wtRecordPath(main, "US-001")
	record := lxRecord(t, main)
	gates, _ := record["gates"].(map[string]any)
	entry, _ := gates[string(model.GateImplementation)].(map[string]any)
	delete(entry, "commands")
	delete(entry, "tree_hash")
	wtWriteJSON(t, path, record)
	before := spy.count(t, "test")
	tip := swRev(t, wt, "HEAD")

	ldLanded(t, main, wt, tip)
	if got := spy.count(t, "test"); got != before+1 {
		t.Errorf("AC-6 the landing ran commands.test %d times on a record with no result, want once", got-before)
	}
}

// AC-6, THREATS B1: a result recorded for commands other than trunk's is not
// trunk's result. The story's checkout gave the gate its own commands.test, or
// a lowered threshold, and put the file back before it committed: the landing
// runs trunk's commands, and holds them to trunk's thresholds.
func TestAC6ACommandOrThresholdOtherThanTheRecordedOneHasTheCommandsRunAtTheLanding(t *testing.T) {
	t.Parallel()
	// walk takes the story to the end, with the worktree's copy of the
	// configuration changed by change for the implementation gate only.
	walk := func(t *testing.T, main string, change func(wt string)) string {
		t.Helper()
		wt := ldStarted(t, main)
		ldWork(t, wt)
		change(wt)
		swPass(t, wt, ldSession, model.GateImplementation, model.GateVerification)
		wtGit(t, wt, "checkout", "--", ".sdlc/config.json")
		swPass(t, wt, ldSession, model.GateVerification, "")
		return wt
	}
	t.Run("AC-6 commands.test other than trunk's", func(t *testing.T) {
		t.Parallel()
		spy := newRCSpy(t)
		main := lxRepository(t, map[string]string{"test": spy.reports("test", "exit 0"), "test_files": "exit 1"}, nil)
		wt := walk(t, main, func(wt string) {
			commands := ldPassing()
			commands["test"] = "exit 0"
			setConfigOnDisk(t, wt, "commands", commands)
		})
		if got := spy.count(t, "test"); got != 0 {
			t.Fatalf("AC-6 trunk's commands.test ran %d times before the landing, want none", got)
		}
		ldLanded(t, main, wt, swRev(t, wt, "HEAD"))
		if got := spy.count(t, "test"); got != 1 {
			t.Errorf("AC-6 the landing ran trunk's commands.test %d times, want once: the result was for another command", got)
		}
	})
	t.Run("AC-6 a threshold other than trunk's", func(t *testing.T) {
		t.Parallel()
		main := lxRepository(t, map[string]string{"coverage": "echo 75", "test_files": "exit 1"}, nil)
		wt := walk(t, main, func(wt string) {
			setConfigOnDisk(t, wt, "thresholds", map[string]any{"coverage_min": 70, "mutation_min": 70})
		})
		before := ldTrunk(t, main)
		out := swRefused(t, rcLandingCode, wt, ldSession, "stop")
		suSays(t, out, "commands.coverage")
		if got := ldTrunk(t, main); got != before {
			t.Errorf("AC-6 trunk moved to %s: coverage of 75 landed under trunk's coverage_min of 80", got)
		}
	})
}

// AC-6: a story rebased onto a trunk that moved has the commands run on the
// tree that lands, and that run is recorded the same way, against that tree.
func TestAC6ARebasedStoryHasTheCommandsRunOnTheTreeThatLandsAndRecorded(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	main, wt := rcFinishedWith(t, spy)
	// The stop makes the story's commits again on the moved trunk, which needs
	// a committer; git only makes one up where the host has a full name, and
	// ubuntu-latest has none (CI run 45256250859).
	otIdentity(t, main)
	ldMoveTrunk(t, main)
	before := spy.count(t, "test")

	r := wtMust(t, wt, ldSession, "", "stop")
	if got := spy.count(t, "test"); got != before+1 {
		t.Fatalf("AC-6 a rebased landing ran commands.test %d times, want once:\n%s%s", got-before, r.stdout, r.stderr)
	}
	landed := rcSubject(t, main)
	if raw := wtRead(t, wtRecordPath(main, "US-001")); !strings.Contains(raw, `"`+landed+`"`) {
		t.Errorf("AC-6 the record does not hold the result of the landing's run against the tree that landed, %s", landed)
	}
}

// ---------------------------------------------------------------- AC-7

// AC-7: `sdlc trace` shows the time the Gate 5 commands took, from the record,
// apart from the test runs the trace counts; with no trace file too, since the
// figure is the record's.
func TestAC7TraceShowsTheTimeTheGate5CommandsTookApartFromOtherTestTime(t *testing.T) {
	t.Parallel()
	main, wt := rcAtTheGate(t, map[string]string{"build": "exit 0", "test": "exit 0"})
	rcGatePasses(t, wt)
	path := wtRecordPath(main, "US-001")
	record := lxRecord(t, main)
	gates, _ := record["gates"].(map[string]any)
	entry, _ := gates[string(model.GateImplementation)].(map[string]any)
	results, _ := entry["commands"].([]any)
	if len(results) != 2 {
		t.Fatalf("AC-7 the gate recorded %d results, want build and test: %v", len(results), entry)
	}
	// Fixed durations, so the figure is the record's and not a clock's.
	for i, ms := range []float64{1500, 61000} {
		if m, ok := results[i].(map[string]any); ok {
			m["duration_ms"] = ms
		}
	}
	wtWriteJSON(t, path, record)

	text := wtMust(t, wt, ldSession, "", "trace", "US-001").stdout
	found := false
	for line := range strings.Lines(text) {
		if strings.HasPrefix(strings.TrimSpace(line), "test runs") {
			continue
		}
		if strings.Contains(line, "1m2s") && (strings.Contains(line, "Gate 5") || strings.Contains(line, "implementation")) {
			found = true
		}
	}
	if !found {
		t.Errorf("AC-7 sdlc trace has no line of its own giving 1m2s, the time the Gate 5 commands took:\n%s", text)
	}
	raw := wtMust(t, wt, ldSession, "", "trace", "US-001", "--json").stdout
	if !regexp.MustCompile(`:\s*(62\.5|62500)\b`).MatchString(raw) {
		t.Errorf("AC-7 sdlc trace --json does not give the Gate 5 commands' time, 62.5 s:\n%s", raw)
	}
}

// ---------------------------------------------------------------- AC-10

// AC-10: one run of the command set is handed one directory: new, empty when
// its first command begins, the same for each of its commands, outside the
// project, so that coverage reads what test left there; and gone after the run,
// whether it passed or failed.
func TestAC10EveryCommandOfOneRunIsHandedOneNewEmptyRunDirectoryThatIsGoneAfter(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	commands := spy.rcEvery()
	commands["test"] = spy.reports("test", `printf 91 > "$SDLC_RUN_DIR/profile"`)
	commands["coverage"] = spy.reports("coverage", `cat "$SDLC_RUN_DIR/profile"`)
	main, wt := rcAtTheGate(t, commands)

	rcGatePasses(t, wt)
	first := spy.runs(t)
	if len(first) != len(rcLanding) {
		t.Fatalf("AC-10 %d commands reported, want %d", len(first), len(rcLanding))
	}
	dir := first[0].dir
	if dir == "" || !filepath.IsAbs(filepath.FromSlash(dir)) || first[0].exists != "present" {
		t.Fatalf("AC-10 commands.build was handed SDLC_RUN_DIR %q (%s), want a directory that is there", dir, first[0].exists)
	}
	if first[0].held != "" {
		t.Errorf("AC-10 the run directory held %q when the first command began, want it empty", first[0].held)
	}
	for _, r := range first {
		if r.dir != dir {
			t.Errorf("AC-10 commands.%s was handed %q, and commands.build %q: one run, one directory", r.key, r.dir, dir)
		}
	}
	if got := first[4]; got.key != "coverage" || !strings.Contains(got.held, "profile") {
		t.Errorf("AC-10 commands.coverage did not find what commands.test left in the run directory: %+v", got)
	}
	for _, root := range []string{main, wt} {
		if rel, err := filepath.Rel(root, filepath.FromSlash(dir)); err == nil && !strings.HasPrefix(rel, "..") {
			t.Errorf("AC-10 the run directory %s is inside the project at %s", dir, root)
		}
	}
	if _, err := os.Stat(filepath.FromSlash(dir)); !os.IsNotExist(err) {
		t.Errorf("AC-10 the run directory %s is still there after the run: %v", dir, err)
	}

	t.Run("AC-10 the next run has a new one", func(t *testing.T) {
		rcGatePasses(t, wt)
		second := spy.runs(t)[len(first):]
		if len(second) == 0 || second[0].dir == dir || second[0].held != "" || second[0].exists != "present" {
			t.Errorf("AC-10 the second run was handed %+v, want a new empty directory, not %s", second, dir)
		}
	})
	t.Run("AC-10 gone after a run that failed", func(t *testing.T) {
		setConfigOnDisk(t, wt, "commands", map[string]string{
			"test": spy.reports("failing", `printf left > "$SDLC_RUN_DIR/report"; exit 1`),
		})
		if r := rcGate(t, wt); r.code == 0 {
			t.Fatal("AC-10 the failing run passed")
		}
		runs := spy.runs(t)
		last := runs[len(runs)-1]
		if last.key != "failing" || last.dir == "" {
			t.Fatalf("AC-10 the failing command did not report a run directory: %+v", last)
		}
		if _, err := os.Stat(filepath.FromSlash(last.dir)); !os.IsNotExist(err) {
			t.Errorf("AC-10 the run directory %s is still there after a run that failed: %v", last.dir, err)
		}
	})
}

// ---------------------------------------------------------------- AC-11

// AC-11: a coverage command that prints no figure refuses the gate with the code
// of AC-2. Each run's directory is new, so what one run's test left there is not
// there for the next run's coverage: a figure from another tree cannot be read.
func TestAC11ACoverageCommandThatPrintsNoFigureRefusesTheGateWithTheCodeOfAC2(t *testing.T) {
	t.Parallel()
	main, wt := rcAtTheGate(t, map[string]string{
		// test leaves a figure only while the tree has no NOFIGURE in it.
		"test":     `[ -f NOFIGURE ] || printf 90 > "$SDLC_RUN_DIR/figure"`,
		"coverage": `cat "$SDLC_RUN_DIR/figure" 2>/dev/null; exit 0`,
	})
	rcGatePasses(t, wt)

	writeFile(t, wt, "NOFIGURE", "")
	r := rcGate(t, wt)
	if r.code == 0 {
		t.Fatalf("AC-11 the gate passed on a coverage figure another run left:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, rcCode) {
		t.Errorf("AC-11 a coverage command that printed no figure was not refused with %s:\n%s", rcCode, r.stderr)
	}
	suSays(t, r.stderr, "commands.coverage")
	rcOutputFile(t, r.stderr, main, wt)
}

// ---------------------------------------------------------------- AC-12

// AC-12: a rework that changed the tree has the commands run on the new tree by
// recording the gate again, which leaves the gates before it standing: their
// inputs did not change.
func TestAC12AReworkThatChangedTheTreeIsRunAgainByTheGateAndTheGatesBeforeItStand(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	main, wt := rcAtTheGate(t, map[string]string{"test": spy.reports("test", "exit 0")})
	rcGatePasses(t, wt)
	before, _ := rcGateEntry(t, main)["tree_hash"].(string)

	writeFile(t, wt, "refund.go", "package billing\n\n// Refund is what the rework changed.\ntype Refund struct{ Amount int }\n")
	if rcCurrent(t, wt) {
		t.Fatal("AC-12 status calls the result current after a rework changed the tree")
	}
	rcGatePasses(t, wt)
	if got := spy.count(t, "test"); got != 2 {
		t.Errorf("AC-12 commands.test ran %d times, want twice: once per tree", got)
	}
	after, _ := rcGateEntry(t, main)["tree_hash"].(string)
	if after == before || after != rcSubject(t, wt) {
		t.Errorf("AC-12 the gate recorded tree %q after the rework, want the new tree %s (before: %s)", after, rcSubject(t, wt), before)
	}
	if !rcCurrent(t, wt) {
		t.Error("AC-12 status does not call the new tree's result current")
	}
	gates := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json")).Gates
	for _, g := range []model.Gate{model.GateTestsFrozen, model.GatePlan, model.GateDesignReview} {
		if gates[string(g)] != string(model.GatePass) {
			t.Errorf("AC-12 %s is %q after the gate was recorded again, want it standing", g, gates[string(g)])
		}
	}
}

// ---------------------------------------------------------------- AC-13

// rcAtTheFreeze is US-001 under way in its own checkout, story worktrees off,
// with the files given committed before it started and commands as given.
func rcAtTheFreeze(t *testing.T, committed map[string]string, commands map[string]string) string {
	t.Helper()
	root := fcStarted(t, committed)
	setConfigOnDisk(t, root, "commands", commands)
	return root
}

// AC-13: the freeze runs commands.test_files over its new and changed tests on
// the tree it is about to freeze, and refuses where they pass: a test written to
// pass before the code exists tests nothing.
func TestAC13TheFreezeRunsTheNewTestsAndRefusesWhereTheyPass(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	root := rcAtTheFreeze(t, nil, map[string]string{"test_files": spy.reports("test_files", "exit 0")})
	writeFile(t, root, "internal/invoice_test.go", fcTest)
	record := wtRead(t, fcRecord(root))

	r := run(t, "freeze")
	if got := spy.count(t, "test_files"); got != 1 {
		t.Errorf("AC-13 commands.test_files ran %d times, want once", got)
	}
	if r.code == 0 || !strings.Contains(r.stderr, rcAnyCode) {
		t.Fatalf("AC-13 the freeze went ahead though the new tests pass: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("AC-13 a refusal wrote to stdout: %q", r.stdout)
	}
	suSays(t, r.stderr, "commands.test_files")
	fcUnfrozen(t, root)
	if got := wtRead(t, fcRecord(root)); got != record {
		t.Errorf("AC-13 the refused freeze changed the record:\n%s\nwas\n%s", got, record)
	}
}

// AC-13: where they fail, the freeze is taken, the run's whole output is kept in
// a file under .sdlc/stories/US-001/, and the command, its exit status, its
// duration and the tree hash are recorded with the freeze.
func TestAC13AFreezeWhoseNewTestsFailIsTakenWithTheRunKeptAndRecorded(t *testing.T) {
	t.Parallel()
	root := rcAtTheFreeze(t, nil, map[string]string{"test_files": rcNumbered + "; echo rc-undefined-Invoice; exit 2"})
	writeFile(t, root, "internal/invoice_test.go", fcTest)
	tree := rcSubject(t, root)

	mustRun(t, "freeze")
	if len(lockOnDisk(t, root).Files) == 0 {
		t.Fatal("AC-13 no freeze was taken")
	}
	kept := false
	for _, body := range rcStoryFiles(t, root) {
		if strings.Contains(body, "rc-line-01") && strings.Contains(body, "rc-line-50") && strings.Contains(body, "rc-undefined-Invoice") {
			kept = true
		}
	}
	if !kept {
		t.Errorf("AC-13 no file under .sdlc/stories/US-001/ holds the run's whole output: %v", rcStoryFiles(t, root))
	}
	ran, _ := rcRecordAt(t, root)["freeze_run"].(map[string]any)
	if name, _ := ran["name"].(string); !strings.Contains(name, "test_files") {
		t.Errorf("AC-13 the freeze recorded command %v, want commands.test_files: %v", ran["name"], ran)
	}
	if exit, ok := ran["exit"].(float64); !ok || exit != 2 {
		t.Errorf("AC-13 the freeze recorded exit status %v, want 2", ran["exit"])
	}
	if ms, ok := ran["duration_ms"].(float64); !ok || ms < 0 {
		t.Errorf("AC-13 the freeze recorded no duration: %v", ran)
	}
	if got := ran["tree_hash"]; got != tree {
		t.Errorf("AC-13 the freeze recorded tree hash %v, want %s, the tree it froze", got, tree)
	}
}

// rcRecordAt is the record of US-001 in the checkout at root.
func rcRecordAt(t *testing.T, root string) map[string]any {
	t.Helper()
	var r map[string]any
	wtReadJSON(t, fcRecord(root), &r)
	return r
}

// AC-13 and AC-17: with commands.test_files empty the whole commands.test is
// run, and a configuration without the key behaves as before it existed; with
// neither set nothing is run and the freeze is taken.
func TestAC13WithNoNarrowCommandTheWholeTestCommandIsRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		commands func(spy rcSpy) map[string]string
		ran      int
		frozen   bool
	}{
		{"AC-13 test_files absent, the tests fail", func(spy rcSpy) map[string]string {
			return map[string]string{"test": spy.reports("test", "exit 1")}
		}, 1, true},
		{"AC-13 test_files empty, the tests pass", func(spy rcSpy) map[string]string {
			return map[string]string{"test": spy.reports("test", "exit 0"), "test_files": ""}
		}, 1, false},
		{"AC-17 neither set", func(rcSpy) map[string]string { return map[string]string{"lint": "exit 0"} }, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := newRCSpy(t)
			root := rcAtTheFreeze(t, nil, tc.commands(spy))
			writeFile(t, root, "internal/invoice_test.go", fcTest)
			r := run(t, "freeze")
			if got := spy.count(t, "test"); got != tc.ran {
				t.Errorf("%s: commands.test ran %d times, want %d", tc.name, got, tc.ran)
			}
			if frozen := r.code == 0 && wtExists(fcLockPath(root)); frozen != tc.frozen {
				t.Errorf("%s: frozen %v, want %v: exit %d\n%s", tc.name, frozen, tc.frozen, r.code, r.stderr)
			}
		})
	}
}

// AC-13: a story that freezes no new or changed test file is frozen without the
// run, and so is a test added to a freeze that allows it (ANALYSIS 3.2): a test
// written after the code may pass, which is why that is off by default.
func TestAC13AFreezeWithNoNewOrChangedTestFileRunsNothing(t *testing.T) {
	t.Parallel()
	t.Run("AC-13 nothing new or changed", func(t *testing.T) {
		t.Parallel()
		spy := newRCSpy(t)
		root := rcAtTheFreeze(t, map[string]string{"internal/kept_test.go": fcTest},
			map[string]string{"test_files": spy.reports("test_files", "exit 0")})
		mustRun(t, "freeze")
		if got := spy.count(t, "test_files"); got != 0 {
			t.Errorf("AC-13 commands.test_files ran %d times with no new or changed test file", got)
		}
		rcFrozen(t, root)
	})
	t.Run("AC-13 a test added to the freeze", func(t *testing.T) {
		t.Parallel()
		spy := newRCSpy(t)
		root := rcAtTheFreeze(t, nil, map[string]string{"test_files": spy.reports("test_files", "exit 1")})
		setConfigOnDisk(t, root, "freeze", map[string]any{"allow_new_test_files": true})
		writeFile(t, root, "internal/invoice_test.go", fcTest)
		mustRun(t, "freeze")
		writeFile(t, root, "internal/later_test.go", fcTest)
		mustRun(t, "freeze")
		if got := spy.count(t, "test_files"); got != 1 {
			t.Errorf("AC-13 commands.test_files ran %d times, want once: at the freeze, not when a test was added to it", got)
		}
		if _, held := lockOnDisk(t, root).Files["internal/later_test.go"]; !held {
			t.Error("AC-13 the test added was not taken into the freeze")
		}
	})
}

// rcFrozen holds that a freeze is on disk at root.
func rcFrozen(t *testing.T, root string) {
	t.Helper()
	if !wtExists(fcLockPath(root)) {
		t.Error("no freeze was taken")
	}
}

// AC-13, THREATS B4 and ANALYSIS 7.8: a run that could not start its tool (the
// shell's 126 and 127) says nothing of the tests, and is not taken for tests
// that fail.
func TestAC13ARunThatCouldNotStartItsToolIsNotTakenForFailingTests(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"126", "127"} {
		t.Run("AC-13 exit "+status, func(t *testing.T) {
			t.Parallel()
			root := rcAtTheFreeze(t, nil, map[string]string{"test_files": "echo not found >&2; exit " + status})
			writeFile(t, root, "internal/invoice_test.go", fcTest)
			r := run(t, "freeze")
			if r.code == 0 || !strings.Contains(r.stderr, rcAnyCode) {
				t.Errorf("AC-13 a run that ended %s was taken for failing tests: exit %d\n%s", status, r.code, r.stdout)
			}
			fcUnfrozen(t, root)
		})
	}
}

// ---------------------------------------------------------------- AC-17

// AC-17: commands.test_files runs from the repository root with SDLC_TEST_FILES
// naming a file that lists the new and changed test files, one to a line,
// slash-separated and relative to the root.
func TestAC17TheNarrowCommandIsHandedAFileListingTheTestsOnePerLineSlashSeparatedAndRelative(t *testing.T) {
	t.Parallel()
	listed := filepath.Join(wtTempDir(t), "listed")
	root := rcAtTheFreeze(t, map[string]string{
		".gitignore":               "/.sdlc/state/\nignored_test.go\n",
		"internal/kept_test.go":    fcTest,
		"internal/changed_test.go": fcTest,
		"ROOT-MARKER":              "the repository root\n",
	}, map[string]string{"test_files": `test -f ROOT-MARKER || { echo not at the root; exit 9; }; ` +
		`test -f "$SDLC_TEST_FILES" || { echo no file; exit 9; }; cp "$SDLC_TEST_FILES" ` + suSh(listed) + `; exit 1`})
	writeFile(t, root, "internal/changed_test.go", fcTwo)
	writeFile(t, root, "internal/deep/er/new_test.go", fcTest)
	writeFile(t, root, "testdata/new fixture.txt", "new\n")
	writeFile(t, root, "internal/café_test.go", fcTest)
	writeFile(t, root, "ignored_test.go", fcTest)

	mustRun(t, "freeze")
	raw := wtRead(t, listed)
	if raw == "" {
		t.Fatal("AC-17 commands.test_files was not handed a file in SDLC_TEST_FILES, or did not run at the root")
	}
	names := strings.Split(strings.TrimSuffix(strings.ReplaceAll(raw, "\r\n", "\n"), "\n"), "\n")
	slices.Sort(names)
	want := []string{"internal/café_test.go", "internal/changed_test.go", "internal/deep/er/new_test.go", "testdata/new fixture.txt"}
	if !slices.Equal(names, want) {
		t.Errorf("AC-17 SDLC_TEST_FILES listed\n%q\nwant\n%q", names, want)
	}
	for _, n := range names {
		if strings.Contains(n, `\`) || filepath.IsAbs(n) || strings.HasPrefix(n, "/") {
			t.Errorf("AC-17 %q is not slash-separated and relative to the root", n)
		}
	}
}

// AC-17, THREATS B5: one name to a line cannot carry a name with a line break
// in it, so such a name is refused before anything runs.
func TestAC17ATestFileWhoseNameHasALineBreakIsRefusedBeforeTheNarrowRun(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow a line break in a file name")
	}
	spy := newRCSpy(t)
	root := rcAtTheFreeze(t, nil, map[string]string{"test_files": spy.reports("test_files", "exit 1")})
	writeFile(t, root, "internal/two\nlines_test.go", fcTest)
	if r := run(t, "freeze"); r.code == 0 || !strings.Contains(r.stderr, rcAnyCode) {
		t.Errorf("AC-17 a name with a line break in it was listed: exit %d\n%s", r.code, r.stderr)
	}
	if got := spy.count(t, "test_files"); got != 0 {
		t.Errorf("AC-17 commands.test_files ran %d times over a list it cannot be handed", got)
	}
	fcUnfrozen(t, root)
}

// AC-17, security review F-8: a reader that splits lines at a carriage return
// too (Python's splitlines) would turn such a name into two that are not there,
// the command would fail, and the freeze would be taken for tests that fail. So
// a carriage return is refused as a line feed is, before anything runs.
func TestAC17ATestFileWhoseNameHasACarriageReturnIsRefusedBeforeTheNarrowRun(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow a carriage return in a file name")
	}
	for _, name := range []string{"internal/two\rparts_test.go", "internal/\rfirst_test.go"} {
		t.Run("AC-17 "+strings.ReplaceAll(name, "\r", "<CR>"), func(t *testing.T) {
			spy := newRCSpy(t)
			root := rcAtTheFreeze(t, nil, map[string]string{"test_files": spy.reports("test_files", "exit 1")})
			writeFile(t, root, name, fcTest)
			if r := run(t, "freeze"); r.code == 0 || !strings.Contains(r.stderr, rcAnyCode) {
				t.Errorf("AC-17 a name with a carriage return in it was listed: exit %d\n%s", r.code, r.stderr)
			}
			if got := spy.count(t, "test_files"); got != 0 {
				t.Errorf("AC-17 commands.test_files ran %d times over a list it cannot be handed", got)
			}
			fcUnfrozen(t, root)
		})
	}
}

// AC-17 is met here already and this holds it: doctor reads every command it is
// given, so commands.test_files is looked up as the others are.
func TestAC17DoctorChecksTheNarrowTestCommandAsItChecksTheOthers(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "commands", map[string]string{"test_files": "rc-no-such-program-us134 $SDLC_TEST_FILES"})
	r := run(t, "doctor")
	if !strings.Contains(r.stdout, "rc-no-such-program-us134") || !strings.Contains(r.stdout, "test_files") {
		t.Errorf("AC-17 doctor does not name the program commands.test_files would run:\n%s", r.stdout)
	}
}
