package cli

// US-134, the rows the security design review asks for in its notes:
//
//   - F-3: the gate's result is bound to the tree it hashed before the run, so
//     a command that rewrites or deletes a file of that tree, which the
//     implementer could then put back, has the gate refused, naming the file,
//     with nothing recorded. A command that writes only into SDLC_RUN_DIR or an
//     ignored path, or writes a file back as it was, is accepted.
//   - F-5: an output file is never committed, so a .gitignore already in the
//     story's directory gains each of the two names it lacks and keeps every
//     other line as it was.
//   - F-6: two configurations whose key, command and threshold run together to
//     the same text are not taken for the same.
//   - F-7: a result on an implementation gate that is not passed is not
//     current, so it is never reused.
//
// The helpers are us134_test.go's, and these carry an rs prefix, for the review's
// security rows.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// rsOutputNames are the gate's and the freeze's output files, as PLAN.md D10
// names them.
var rsOutputNames = []string{"implementation-run.log", "freeze-run.log"}

// ---------------------------------------------------------------- F-3, AC-3

// AC-3, security review F-3: a command that changed the tree it ran on leaves a
// result for a tree nobody ran the commands on, once the change is put back. The
// gate refuses it, naming the file, and records nothing: the gate is not passed,
// holds no result, and no event says it passed or failed.
func TestAC3ACommandThatRewritesOrDeletesAFileOfTheTreeItRanOnHasTheGateRefusedNamingIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, command, file string
	}{
		// billing.go is tracked and changed by the story's work, so it is the
		// work's content that the command moves.
		{"AC-3 a command rewrites a tracked file", `printf '// a generator was here\n' >> billing.go`, "billing.go"},
		// README.md is tracked and the work leaves it as trunk has it.
		{"AC-3 a command deletes a tracked file", `rm README.md`, "README.md"},
		// refund.go is the work itself, not yet committed: in the tree the gate
		// hashed all the same.
		{"AC-3 a command rewrites a file not yet committed", `printf 'package billing\n' > refund.go`, "refund.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spy := newRCSpy(t)
			commands := spy.rcEvery()
			commands["lint"] = spy.reports("lint", tc.command)
			main, wt := rcAtTheGate(t, commands)
			before := rcGateEntry(t, main)
			passed := model.GateEvent(model.GateImplementation, model.GatePass)
			failed := model.GateEvent(model.GateImplementation, model.GateFail)

			r := rcGate(t, wt)
			if got := spy.count(t, "lint"); got != 1 {
				t.Fatalf("%s: commands.lint ran %d times, want once", tc.name, got)
			}
			if r.code == 0 {
				t.Fatalf("%s: the gate passed though commands.lint changed %s in the tree it ran on:\n%s", tc.name, tc.file, r.stdout)
			}
			if !strings.Contains(r.stderr, rcCode) {
				t.Errorf("%s: the refusal does not carry %s:\n%s", tc.name, rcCode, r.stderr)
			}
			suSays(t, r.stderr, tc.file)
			if r.stdout != "" {
				t.Errorf("%s: a refusal wrote to stdout: %q", tc.name, r.stdout)
			}
			entry := rcGateEntry(t, main)
			if entry["status"] == string(model.GatePass) {
				t.Errorf("%s: the implementation gate is passed", tc.name)
			}
			for _, key := range []string{"commands", "tree_hash"} {
				if _, kept := entry[key]; kept {
					t.Errorf("%s: the gate holds %s after a run that changed its tree: %v", tc.name, key, entry)
				}
			}
			if entry["status"] != before["status"] {
				t.Errorf("%s: the gate went from %v to %v: nothing is to be recorded", tc.name, before["status"], entry["status"])
			}
			if got := rcEvents(t, main, passed) + rcEvents(t, main, failed); got != 0 {
				t.Errorf("%s: the record has %d implementation events: nothing is to be recorded", tc.name, got)
			}
		})
	}
}

// AC-3, security review F-3: what the check is not about. A command that writes
// only into its run directory or an ignored path, or writes a file back with
// what it held, as a formatter with nothing to fix does, leaves the tree as it
// was, and the gate records the tree it hashed before the run.
func TestAC3ACommandThatLeavesTheTreeAsItWasIsAcceptedThoughItWrote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, command string }{
		{"AC-3 a command writes into SDLC_RUN_DIR", `printf 91 > "$SDLC_RUN_DIR/profile"`},
		{"AC-3 a command writes under an ignored path", `mkdir -p .scratch && printf 91 > .scratch/cover.out`},
		{"AC-3 a command writes a file back as it was", `cp billing.go "$SDLC_RUN_DIR/b" && cp "$SDLC_RUN_DIR/b" billing.go`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spy := newRCSpy(t)
			commands := spy.rcEvery()
			commands["test"] = spy.reports("test", tc.command)
			main, wt := rcAtTheGate(t, commands)
			writeFile(t, wt, ".gitignore", "/.sdlc/state/\n.scratch/\n")
			want := rcSubject(t, wt)

			rcGatePasses(t, wt)
			if got := spy.count(t, "test"); got != 1 {
				t.Fatalf("%s: commands.test ran %d times, want once", tc.name, got)
			}
			if got := rcGateEntry(t, main)["tree_hash"]; got != want {
				t.Errorf("%s: the gate recorded tree hash %v, want %s, the tree hashed before the run", tc.name, got, want)
			}
		})
	}
}

// ---------------------------------------------------------------- F-5, AC-13 and AC-2

// rsIgnoreFile is the .gitignore of US-001's directory in the checkout at root.
func rsIgnoreFile(root string) string {
	return filepath.Join(root, ".sdlc", "stories", "US-001", ".gitignore")
}

// rsIgnores holds the .gitignore of root's story directory to what F-5 asks:
// every line it held before, in its order; and each output name once, so that
// the files under it that a run wrote are listed, and the name it held already
// is not added again.
func rsIgnores(t *testing.T, root, was string) {
	t.Helper()
	raw, err := os.ReadFile(rsIgnoreFile(root))
	if err != nil {
		t.Fatalf("the story's .gitignore is gone: %v", err)
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(string(raw), "\n"), "\r\n", "\n"), "\n")
	at := 0
	for line := range strings.SplitSeq(strings.TrimRight(was, "\n"), "\n") {
		i := slices.Index(lines[at:], line)
		if i < 0 {
			t.Errorf("the story's .gitignore no longer holds %q, in its place, the line it held before:\n%s", line, raw)
			continue
		}
		at += i + 1
	}
	for _, name := range rsOutputNames {
		n := 0
		for _, line := range lines {
			if strings.TrimPrefix(strings.TrimSpace(line), "/") == name {
				n++
			}
		}
		if n != 1 {
			t.Errorf("the story's .gitignore lists %s %d times, want once:\n%s", name, n, raw)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(rsIgnoreFile(root)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") && !slices.Contains(rsOutputNames, e.Name()) {
			t.Errorf("the run wrote %s, which is not one of %q, the names the story's .gitignore lists", e.Name(), rsOutputNames)
		}
	}
}

// AC-13, security review F-5: in a project that commits its story directories,
// an output file holds whatever a failing test printed. A .gitignore already in
// the story's directory, a person's own, gains each name it lacks and keeps the
// rest as it was, its last line with no line break included.
func TestAC13AStoryGitignoreThatIsThereAlreadyGainsTheOutputNamesItLacksAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, was string }{
		{"AC-13 a person's own lines", "# kept by hand\ndraft notes.md\n!keep.log\n"},
		{"AC-13 a last line with no line break", "# kept by hand\ndraft notes.md"},
		{"AC-13 the gate's name already there", "implementation-run.log\n"},
		{"AC-13 the freeze's name already there", "# mine\n/freeze-run.log\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := rcAtTheFreeze(t, nil, map[string]string{"test_files": "echo rs-the-new-test-fails; exit 1"})
			if err := os.WriteFile(rsIgnoreFile(root), []byte(tc.was), 0o644); err != nil {
				t.Fatal(err)
			}
			writeFile(t, root, "internal/invoice_test.go", fcTest)
			mustRun(t, "freeze")
			rcFrozen(t, root)
			kept := false
			for _, body := range rcStoryFiles(t, root) {
				kept = kept || strings.Contains(body, "rs-the-new-test-fails")
			}
			if !kept {
				t.Fatalf("%s: the freeze kept no output file to keep out of a commit: %v", tc.name, rcStoryFiles(t, root))
			}
			rsIgnores(t, root, tc.was)
		})
	}
}

// AC-2, security review F-5: the gate's output file, written in the main
// checkout's story directory, is kept out of a commit the same way.
func TestAC2TheGatesOutputFileIsAddedToAStoryGitignoreThatIsThereAlready(t *testing.T) {
	t.Parallel()
	main, wt := rcAtTheGate(t, map[string]string{"test": "echo rs-the-test-failed; exit 1"})
	was := "# kept by hand\ndraft notes.md\n"
	if err := os.WriteFile(rsIgnoreFile(main), []byte(was), 0o644); err != nil {
		t.Fatal(err)
	}
	r := rcGate(t, wt)
	if r.code == 0 || !strings.Contains(r.stderr, rcCode) {
		t.Fatalf("AC-2 the gate was not refused with %s: exit %d\n%s", rcCode, r.code, r.stderr)
	}
	if body := rcOutputFile(t, r.stderr, main, wt); !strings.Contains(body, "rs-the-test-failed") {
		t.Fatalf("AC-2 the output file does not hold what commands.test printed:\n%s", body)
	}
	rsIgnores(t, main, was)
}

// ---------------------------------------------------------------- F-6 and F-7, AC-6

// AC-6, security review F-6: trunk's coverage is `echo 9` held to 50, and the
// gate ran `echo 95` held to 0 from the story's own copy of the configuration.
// Run together, key, command and threshold read the same, "coverageecho 950",
// and they are not the same configuration: the landing runs trunk's, which
// fails, and nothing lands.
func TestAC6ConfigurationsThatReadTheSameRunTogetherAreNotTakenForTheSame(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, map[string]string{"coverage": "echo 9"}, map[string]any{"coverage_min": 50, "mutation_min": 70})
	wt := ldStarted(t, main)
	ldWork(t, wt)
	commands := ldPassing()
	commands["coverage"] = "echo 95"
	setConfigOnDisk(t, wt, "commands", commands)
	setConfigOnDisk(t, wt, "thresholds", map[string]any{"coverage_min": 0, "mutation_min": 70})
	swPass(t, wt, ldSession, model.GateImplementation, model.GateVerification)
	if len(rcResults(t, main)) == 0 {
		t.Fatalf("AC-6 the gate recorded no result to compare: %v", rcGateEntry(t, main))
	}
	wtGit(t, wt, "checkout", "--", ".sdlc/config.json")
	swPass(t, wt, ldSession, model.GateVerification, "")
	before := ldTrunk(t, main)

	out := swRefused(t, rcLandingCode, wt, ldSession, "stop")
	suSays(t, out, "commands.coverage")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("AC-6 trunk moved to %s: coverage of 9 landed under trunk's coverage_min of 50", got)
	}
}

// AC-4 and AC-6, security review F-7: a result counts only where the gate that
// holds it is passed. No command of sdlc's leaves one on a gate that is not, so
// the record here is changed by hand. The landing cannot be asked: it lands only
// a story whose every gate is passed (finishedHere, through NextGate), so with
// the gate at fail or pending the stop lands nothing and reuses nothing whatever
// the result says. `sdlc status --json` is asked instead, whose `current` is the
// rule the landing reuses a result by (PLAN.md D11, resultHolds).
func TestAC4AndAC6AResultOnAnImplementationGateThatIsNotPassedIsNotCurrent(t *testing.T) {
	t.Parallel()
	for _, status := range []model.GateStatus{model.GateFail, model.GatePending} {
		t.Run("AC-6 the gate is "+string(status), func(t *testing.T) {
			t.Parallel()
			main, wt := rcAtTheGate(t, map[string]string{"test": "exit 0", "coverage": "echo 90"})
			rcGatePasses(t, wt)
			if !rcCurrent(t, wt) {
				t.Fatalf("AC-4 status does not call the result just recorded current: %v", rcStatusCommands(t, wt))
			}
			path := wtRecordPath(main, "US-001")
			record := lxRecord(t, main)
			gates, _ := record["gates"].(map[string]any)
			entry, _ := gates[string(model.GateImplementation)].(map[string]any)
			entry["status"] = string(status)
			wtWriteJSON(t, path, record)
			if _, held := rcGateEntry(t, main)["tree_hash"]; !held {
				t.Fatalf("AC-6 the hand-edited gate holds no result: %v", rcGateEntry(t, main))
			}
			if rcCurrent(t, wt) {
				t.Errorf("AC-6 status calls a result current on an implementation gate that is %s", status)
			}
		})
	}
}
