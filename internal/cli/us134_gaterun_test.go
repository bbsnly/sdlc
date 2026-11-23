package cli

// US-134, the implementation gate's refusals that the other rows leave
// unreached, and the parts of its run that are pure enough to hold on their
// own (PLAN D5 and D10):
//
//   - D5 step 5: a run whose commands moved what the gate stands on (the story
//     being worked on, a gate's status or time, the freeze, a frozen file,
//     HEAD) is refused with SDLC-E0095, or the freeze's own SDLC-E0025, and
//     nothing is recorded: the result would be for a story or a tree nobody
//     ran the commands on.
//   - D5 step 1: a gate run inside one of its own commands is refused before
//     anything runs, or a command that runs `sdlc gate` would start the set
//     again with the project's lock let go, to any depth.
//   - D10: the output file is streamed into a temporary file of its own, cut
//     at 16 MiB with a line that says so, and put at its name by a rename; one
//     that cannot be put there is kept and named. The story's .gitignore gains
//     the names it lacks, and is read only where it is one regular file with
//     one name.
//   - F-12: each path of the tree is taken by what it is, a link by its own
//     text and a regular file by its bytes, never followed and never waited on.
//
// The helpers are us134_test.go's and us134_links_test.go's; these carry a gr
// prefix, for the gate's run.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellx"
)

// grNothingRecorded holds that a refused run left the implementation gate as it
// was: not passed, holding no result, with no implementation event.
func grNothingRecorded(t *testing.T, what, main string, before map[string]any) {
	t.Helper()
	entry := rcGateEntry(t, main)
	if entry["status"] == string(model.GatePass) || entry["status"] != before["status"] {
		t.Errorf("%s: the implementation gate went from %v to %v: nothing is to be recorded", what, before["status"], entry["status"])
	}
	for _, key := range []string{"commands", "tree_hash"} {
		if _, kept := entry[key]; kept {
			t.Errorf("%s: the gate holds %s after a refused run: %v", what, key, entry)
		}
	}
	passed := model.GateEvent(model.GateImplementation, model.GatePass)
	failed := model.GateEvent(model.GateImplementation, model.GateFail)
	if got := rcEvents(t, main, passed) + rcEvents(t, main, failed); got != 0 {
		t.Errorf("%s: the record has %d implementation events: nothing is to be recorded", what, got)
	}
}

// grVariant is the JSON file at path with change made to it, written to a file
// of the test's own, for a command to copy over the original while it runs.
func grVariant(t *testing.T, path string, change func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	wtReadJSON(t, path, &m)
	change(m)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(wtTempDir(t), filepath.Base(path))
	if err := os.WriteFile(out, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}

// ---------------------------------------------------------------- D5 step 5

// AC-2 and AC-3, PLAN D5 step 5: what the gate stood on moved while its
// commands ran. Each command here does what a person's command or another
// session in the checkout might do meanwhile; the gate refuses, says what
// moved, and records nothing.
func TestAC2AndAC3ARunWhileWhichTheStoryOrItsFreezeOrHEADMovedIsRefusedWithNothingRecorded(t *testing.T) {
	t.Parallel()
	gitID := "git -c user.email=dev@example.com -c user.name=dev -c commit.gpgsign=false"
	cases := []struct {
		name string
		// command is what commands.lint does; main is the main checkout and
		// wt the story's worktree, where the command runs.
		command func(t *testing.T, main, wt string) string
		code    string
		says    []string
	}{
		{name: "AC-2 another story is made the one being worked on",
			command: func(*testing.T, string, string) string { return `printf 'US-002\n' > .sdlc/state/active` },
			code:    rcCode, says: []string{"while the commands ran", "now US-002, not US-001"}},
		{name: "AC-2 the story being worked on is ended",
			command: func(*testing.T, string, string) string { return `rm .sdlc/state/active` },
			code:    rcCode, says: []string{"while the commands ran", "now none, not US-001"}},
		{name: "AC-2 a gate is recorded again",
			command: func(t *testing.T, main, _ string) string {
				t.Helper()
				record := wtRecordPath(main, "US-001")
				variant := grVariant(t, record, func(m map[string]any) {
					gates, _ := m["gates"].(map[string]any)
					analysis, _ := gates[string(model.GateAnalysis)].(map[string]any)
					analysis["at"] = "2001-02-03T04:05:06Z"
				})
				return "cp " + suSh(variant) + " " + suSh(record)
			},
			code: rcCode, says: []string{"while the commands ran", "gate " + string(model.GateAnalysis) + " was recorded as pass"}},
		{name: "AC-2 the freeze is lifted",
			command: func(*testing.T, string, string) string { return `rm .sdlc/state/tests.lock` },
			code:    rcCode, says: []string{"while the commands ran", "the freeze was taken again or lifted"}},
		{name: "AC-2 the freeze is taken again",
			command: func(t *testing.T, _, wt string) string {
				t.Helper()
				lock := fcLockPath(wt)
				variant := grVariant(t, lock, func(m map[string]any) { m["at"] = "2001-02-03T04:05:06Z" })
				return "cp " + suSh(variant) + " " + suSh(lock)
			},
			code: rcCode, says: []string{"while the commands ran", "the freeze was taken again or lifted"}},
		{name: "AC-3 a frozen test is changed",
			command: func(*testing.T, string, string) string {
				return `printf '// changed while the commands ran\n' >> internal/invoice_test.go`
			},
			code: "SDLC-E0025", says: []string{"internal/invoice_test.go"}},
		{name: "AC-3 HEAD moves",
			command: func(*testing.T, string, string) string {
				return gitID + " commit --quiet --no-verify --allow-empty -m 'moved while the gate ran'"
			},
			code: rcCode, says: []string{"while the commands ran", "HEAD moved from "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spy := newRCSpy(t)
			commands := spy.rcEvery()
			main, wt := rcAtTheGate(t, commands)
			commands["lint"] = spy.reports("lint", tc.command(t, main, wt))
			setConfigOnDisk(t, wt, "commands", commands)
			before := rcGateEntry(t, main)

			r := rcGate(t, wt)
			if got := spy.count(t, "lint"); got != 1 {
				t.Fatalf("%s: commands.lint ran %d times, want once", tc.name, got)
			}
			if r.code == 0 {
				t.Fatalf("%s: the gate passed:\n%s", tc.name, r.stdout)
			}
			if !strings.Contains(r.stderr, tc.code) {
				t.Errorf("%s: the refusal does not carry %s:\n%s", tc.name, tc.code, r.stderr)
			}
			suSays(t, r.stderr, tc.says...)
			if r.stdout != "" {
				t.Errorf("%s: a refusal wrote to stdout: %q", tc.name, r.stdout)
			}
			grNothingRecorded(t, tc.name, main, before)
		})
	}
}

// AC-10, PLAN D5 step 1: a gate run from inside one of the commands it runs,
// which SDLC_RUN_DIR says it is, is refused before anything runs. Not parallel:
// the variable is the process's, as a command's own sdlc would see it.
func TestAC10AGateRunInsideOneOfItsOwnCommandsIsRefusedBeforeAnythingRuns(t *testing.T) {
	for _, name := range []string{shellx.RunDirVar, "sdlc_run_dir"} {
		t.Run("AC-10 "+name, func(t *testing.T) {
			if name != shellx.RunDirVar && runtime.GOOS != "windows" {
				t.Skip("names that differ only in case are one variable only on Windows")
			}
			spy := newRCSpy(t)
			main, wt := rcAtTheGate(t, spy.rcEvery())
			before := rcGateEntry(t, main)
			t.Setenv(name, filepath.Join(wtTempDir(t), "run"))

			r := rcGate(t, wt)
			if r.code == 0 {
				t.Fatalf("AC-10 the gate passed inside a run of its commands:\n%s", r.stdout)
			}
			if !strings.Contains(r.stderr, rcCode) {
				t.Errorf("AC-10 the refusal does not carry %s:\n%s", rcCode, r.stderr)
			}
			suSays(t, r.stderr, shellx.RunDirVar+" is already set", "inside a run of them")
			if wtExists(spy.log) {
				t.Errorf("AC-10 a command ran before the refusal:\n%s", wtRead(t, spy.log))
			}
			grNothingRecorded(t, "AC-10", main, before)
		})
	}
}

func TestAC10AnEntryOfTheEnvironmentIsTheRunDirectoryOnlyByItsWholeNameInAnyCase(t *testing.T) {
	t.Parallel()
	for entry, want := range map[string]bool{
		"SDLC_RUN_DIR=/tmp/run": true, "sdlc_run_dir=/tmp/run": true, "SDLC_RUN_DIR": true, "SDLC_RUN_DIR=": true,
		"SDLC_RUN_DIRS=/tmp/run": false, "XSDLC_RUN_DIR=/tmp/run": false, "SDLC_TEST_FILES=/tmp/list": false, "": false,
	} {
		if got := isRunDirVar(entry); got != want {
			t.Errorf("AC-10 isRunDirVar(%q) = %v, want %v", entry, got, want)
		}
	}
}

// ---------------------------------------------------------------- what a refusal says

func TestAC2ARefusalShowsTheLastTwentyLinesACommandPrintedSetApartOrThatItPrintedNothing(t *testing.T) {
	t.Parallel()
	for _, printed := range []string{"", "\n", "\n\n\n"} {
		got := printedTail(&ranCommand{key: "test", printed: printed})
		if !strings.Contains(got, "commands.test printed nothing") {
			t.Errorf("AC-2 for a command that printed %q the refusal says %q, want that it printed nothing", printed, got)
		}
	}
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "gr-line-%02d\n", i)
	}
	got := printedTail(&ranCommand{key: "lint", printed: b.String()})
	if !strings.HasPrefix(got, "\n\n") {
		t.Errorf("AC-2 the tail is not set apart under a line of its own: %q", got)
	}
	if !strings.Contains(got, "commands.lint printed (last 20 lines)") {
		t.Errorf("AC-2 the tail does not say whose it is and how much: %q", got)
	}
	for i := 1; i <= 30; i++ {
		line := fmt.Sprintf("gr-line-%02d", i)
		if shown := strings.Contains(got, line); shown != (i > 10) {
			t.Errorf("AC-2 %s shown = %v, want only the last 20 lines", line, shown)
		}
	}
	if strings.Contains(got, "\ngr-line-30") {
		t.Errorf("AC-2 a printed line starts a line of the refusal, where it could pass for sdlc's own words: %q", got)
	}
}

func TestAC2TheNoteNamesTheOutputFileByItsPathFromTheRoot(t *testing.T) {
	t.Parallel()
	for _, shown := range []string{
		filepath.Join(wtTempDir(t), ".sdlc", "stories", "US-001", "implementation-run.log"),
		filepath.Join(wtTempDir(t), ".sdlc", "stories", "US-001", ".implementation-run.log.tmp123"),
	} {
		want := ".sdlc/stories/US-001/" + filepath.Base(shown)
		if got := storyRelative("US-001", shown); got != want {
			t.Errorf("AC-2 storyRelative(%q) = %q, want %q", shown, got, want)
		}
	}
}

func TestAC2ARefusalSaysNoneForAnEmptyValueAndAHashByItsFirstTwelveDigits(t *testing.T) {
	t.Parallel()
	if got := cmpOrNone(""); got != "none" {
		t.Errorf("AC-2 cmpOrNone(\"\") = %q, want none", got)
	}
	if got := cmpOrNone("US-002"); got != "US-002" {
		t.Errorf("AC-2 cmpOrNone(US-002) = %q", got)
	}
	full := strings.Repeat("0123456789abcdef", 4)
	if got := short(full); got != full[:12] {
		t.Errorf("AC-2 short of a full hash = %q, want %q", got, full[:12])
	}
	if got := short("abc"); got != "abc" {
		t.Errorf("AC-2 short of a shorter value = %q, want it whole", got)
	}
}

// ---------------------------------------------------------------- what the run is held to

func TestAC3TwoFreezesAreTheSameOnlyForTheSameStoryTimeAndFiles(t *testing.T) {
	t.Parallel()
	lock := func(story, at string, files map[string]string) *model.Lock {
		return &model.Lock{Schema: "sdlc/tests-lock/1", Story: story, At: at, Files: files}
	}
	one := map[string]string{"a_test.go": "aa"}
	base := lock("US-001", "2026-01-01T00:00:00Z", one)
	cases := []struct {
		name string
		a, b *model.Lock
		want bool
	}{
		{"AC-3 no freeze before or after", nil, nil, true},
		{"AC-3 a freeze lifted", base, nil, false},
		{"AC-3 a freeze taken", nil, base, false},
		{"AC-3 the same freeze read again", base, lock("US-001", "2026-01-01T00:00:00Z", map[string]string{"a_test.go": "aa"}), true},
		{"AC-3 another story's", base, lock("US-002", "2026-01-01T00:00:00Z", one), false},
		{"AC-3 taken again later", base, lock("US-001", "2026-01-01T00:00:01Z", one), false},
		{"AC-3 a file's hash moved", base, lock("US-001", "2026-01-01T00:00:00Z", map[string]string{"a_test.go": "ab"}), false},
		{"AC-3 a file added", base, lock("US-001", "2026-01-01T00:00:00Z", map[string]string{"a_test.go": "aa", "b_test.go": "bb"}), false},
	}
	for _, c := range cases {
		if got := sameFreeze(c.a, c.b); got != c.want {
			t.Errorf("%s: sameFreeze = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAC3EachGateIsHeldToItsStatusAndTimeAndNothingElse(t *testing.T) {
	t.Parallel()
	record := &model.Record{Gates: map[model.Gate]model.GateResult{
		model.GateAnalysis:       {Status: model.GatePass, At: "2026-01-01T00:00:00Z", Note: "a note", TreeHash: "abc"},
		model.GateImplementation: {Status: model.GateFail, At: "2026-01-02T00:00:00Z", Commands: []model.CommandRun{{}}},
	}}
	times := gateTimes(record)
	want := map[model.Gate]model.GateResult{
		model.GateAnalysis:       {Status: model.GatePass, At: "2026-01-01T00:00:00Z"},
		model.GateImplementation: {Status: model.GateFail, At: "2026-01-02T00:00:00Z"},
	}
	if len(times) != len(want) {
		t.Fatalf("AC-3 gateTimes = %v, want %v", times, want)
	}
	for g, w := range want {
		if got := times[g]; got.Status != w.Status || got.At != w.At || got.Note != "" || got.TreeHash != "" || got.Commands != nil {
			t.Errorf("AC-3 gate %s is held as %+v, want only %+v", g, got, w)
		}
	}
	record.Gates[model.GateAnalysis] = model.GateResult{Status: model.GateFail}
	if times[model.GateAnalysis].Status != model.GatePass {
		t.Errorf("AC-3 what the run is held to moved with the record it was taken from")
	}
}

// F-12: each path is taken by what it is, never followed and never waited on.
func TestAC3EachPathOfTheTreeIsTakenByWhatItIsWithoutFollowingALinkOrWaitingOnAPipe(t *testing.T) {
	t.Parallel()
	dir := wtTempDir(t)
	file := filepath.Join(dir, "billing.go")
	if err := os.WriteFile(file, []byte("package billing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("package billing\n"))
	if got, want := pathState(file), "file:"+hex.EncodeToString(sum[:]); got != want {
		t.Errorf("AC-3 a regular file is taken as %q, want %q, the hash of its bytes", got, want)
	}
	same := filepath.Join(dir, "copy.go")
	if err := os.WriteFile(same, []byte("package billing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pathState(same) != pathState(file) {
		t.Errorf("AC-3 two files with the same bytes are taken differently: %q, %q", pathState(same), pathState(file))
	}
	if got := pathState(filepath.Join(dir, "absent.go")); got != "absent" {
		t.Errorf("AC-3 a path that is not there is taken as %q, want absent", got)
	}
	// Windows answers a path beneath a file as not found, and Go reads that as
	// absent (windows-latest, CI run 45270785246); either differs from the
	// file the path held, which is what the run is held to.
	beneath := "unreadable"
	if runtime.GOOS == "windows" {
		beneath = "absent"
	}
	if got := pathState(filepath.Join(file, "beneath")); got != beneath {
		t.Errorf("AC-3 a path beneath a file is taken as %q, want %s", got, beneath)
	}
	sub := filepath.Join(dir, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := pathState(sub); !strings.HasPrefix(got, "type:") || pathState(file) == got {
		t.Errorf("AC-3 a directory is taken as %q, want its type", got)
	}

	link := filepath.Join(dir, "link")
	rlSymlink(t, "billing.go", link)
	if got := pathState(link); got != "link:billing.go" {
		t.Errorf("AC-3 a link is taken as %q, want its own text", got)
	}
	if err := os.WriteFile(file, []byte("package billing // changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pathState(link); got != "link:billing.go" {
		t.Errorf("AC-3 a link moved with the file it names, to %q: it was followed", got)
	}

	pipe := filepath.Join(dir, "pipe")
	rlFifo(t, pipe)
	done := make(chan string, 1)
	go func() { done <- pathState(pipe) }()
	select {
	case got := <-done:
		if !strings.HasPrefix(got, "type:") {
			t.Errorf("AC-3 a named pipe is taken as %q, want its type", got)
		}
	case <-time.After(rlPatience):
		if f, err := os.OpenFile(pipe, os.O_RDWR, 0); err == nil {
			_ = f.Close()
		}
		t.Fatalf("AC-3 taking a named pipe had not finished after %s: it waited on the pipe", rlPatience)
	}
}

// ---------------------------------------------------------------- D10: the output file

func grOutputFile(t *testing.T) (*outputFile, string) {
	t.Helper()
	dir := filepath.Join(wtTempDir(t), "US-001")
	l, err := openOutputFile(dir, implementationLog)
	if err != nil {
		t.Fatalf("openOutputFile: %v", err)
	}
	t.Cleanup(l.discard)
	return l, dir
}

func TestAC2TheOutputFileIsStreamedIntoAnIgnoredTemporaryFileAndPutAtItsNameWhenTheRunEnds(t *testing.T) {
	t.Parallel()
	l, dir := grOutputFile(t)
	if filepath.Dir(l.tmp) != dir || !strings.HasPrefix(filepath.Base(l.tmp), ".implementation-run.log.tmp") {
		t.Errorf("AC-2 the temporary file is %q, want one of its own in %q beside the name", l.tmp, dir)
	}
	if ok, _ := filepath.Match(".*-run.log.tmp*", filepath.Base(l.tmp)); !ok {
		t.Errorf("AC-2 the temporary file %q is not one the story's .gitignore ignores", l.tmp)
	}
	ignore := wtRead(t, filepath.Join(dir, ".gitignore"))
	for _, want := range []string{"implementation-run.log", "freeze-run.log", ".*-run.log.tmp*"} {
		if !strings.Contains("\n"+ignore, "\n"+want+"\n") {
			t.Errorf("AC-2 the story's .gitignore does not hold %q from the moment the file exists:\n%s", want, ignore)
		}
	}
	if wtExists(filepath.Join(dir, implementationLog)) {
		t.Errorf("AC-2 the output file is at its name before the run ended")
	}
	if n, err := l.Write([]byte("printed by a command\n")); n != len("printed by a command\n") || err != nil {
		t.Errorf("AC-2 Write = %d, %v, want everything taken", n, err)
	}
	l.line("sdlc: commands.test, exit 0, 1s")
	shown, said := l.place()
	if shown != filepath.Join(dir, implementationLog) || said != "" {
		t.Errorf("AC-2 place = %q, %q, want the file's name and nothing said", shown, said)
	}
	if got := wtRead(t, shown); got != "printed by a command\nsdlc: commands.test, exit 0, 1s\n" {
		t.Errorf("AC-2 the output file holds %q", got)
	}
	if wtExists(l.tmp) {
		t.Errorf("AC-2 the temporary file is still there after it was put in place")
	}
	if again, said := l.place(); again != shown || said != "" {
		t.Errorf("AC-2 a second place = %q, %q, want the name it was put at", again, said)
	}
	l.discard()
	if !wtExists(shown) {
		t.Errorf("AC-2 discarding a placed output file removed it")
	}
	if n, err := l.Write([]byte("after the end\n")); n != len("after the end\n") || err != nil {
		t.Errorf("AC-2 a write after the end = %d, %v, want it taken and dropped", n, err)
	}
	if got := wtRead(t, shown); strings.Contains(got, "after the end") {
		t.Errorf("AC-2 a write after the end reached the file: %q", got)
	}
}

func TestAC2TheOutputFileIsCutAtSixteenMiBWithALineThatSaysWhere(t *testing.T) {
	t.Parallel()
	l, _ := grOutputFile(t)
	first := strings.Repeat("a", runLogLimit-10)
	if n, _ := l.Write([]byte(first)); n != len(first) {
		t.Fatalf("AC-2 Write took %d of %d", n, len(first))
	}
	over := strings.Repeat("b", 100)
	if n, err := l.Write([]byte(over)); n != len(over) || err != nil {
		t.Errorf("AC-2 the write that crosses the limit = %d, %v, want all of it taken, so the command goes on", n, err)
	}
	if n, _ := l.Write([]byte("c")); n != 1 {
		t.Errorf("AC-2 a write after the cut = %d, want it taken", n)
	}
	l.line("sdlc: after the cut")
	shown, _ := l.place()
	got := wtRead(t, shown)
	cut := "\nsdlc: the output was cut here, at 16 MiB\n"
	if want := first + strings.Repeat("b", 10) + cut; got != want {
		t.Errorf("AC-2 the output file is %d bytes ending %q, want %d bytes ending %q", len(got), got[max(0, len(got)-60):],
			len(want), want[len(want)-60:])
	}
}

func TestAC2AnOutputFileThatCannotBePutAtItsNameIsKeptAndNamed(t *testing.T) {
	t.Parallel()
	l, dir := grOutputFile(t)
	// A directory that holds a file is what a rename cannot replace.
	if err := os.MkdirAll(filepath.Join(dir, implementationLog, "in the way"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _ = l.Write([]byte("what the commands printed\n"))
	shown, said := l.place()
	if shown != l.tmp {
		t.Errorf("AC-2 the output is named as %q, want the temporary file %q that holds it", shown, l.tmp)
	}
	for _, want := range []string{"could not be put at " + filepath.Join(dir, implementationLog), "it is in " + l.tmp, "Remove what stands at"} {
		if !strings.Contains(said, want) {
			t.Errorf("AC-2 the sentence %q does not say %q", said, want)
		}
	}
	l.discard()
	if got := wtRead(t, l.tmp); got != "what the commands printed\n" {
		t.Errorf("AC-2 the kept file holds %q after the run ended, want what the commands printed", got)
	}
}

func TestAC2AnOutputFileNeverPutInPlaceIsRemoved(t *testing.T) {
	t.Parallel()
	l, dir := grOutputFile(t)
	_, _ = l.Write([]byte("a run refused before it ended\n"))
	l.discard()
	if wtExists(l.tmp) || wtExists(filepath.Join(dir, implementationLog)) {
		t.Errorf("AC-2 a discarded output file left %q or its name behind", l.tmp)
	}
	l.discard()
}

func TestAC2AnOutputFileThatCannotBeMadeIsRefusedAsUnwritable(t *testing.T) {
	t.Parallel()
	notADir := filepath.Join(wtTempDir(t), "US-001")
	if err := os.WriteFile(notADir, []byte("a file where the story's directory goes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openOutputFile(notADir, implementationLog); !grCode(err, sdlcerr.StateUnwritable) {
		t.Errorf("AC-2 openOutputFile under a file = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

func grCode(err error, code sdlcerr.Code) bool {
	var e *sdlcerr.Error
	return errors.As(err, &e) && e.Code == code
}

// ---------------------------------------------------------------- D10: the story's .gitignore

func TestAC13TheStoryGitignoreGainsOnlyTheNamesItLacksAndKeepsEveryLineInItsOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, was, want string
	}{
		{"AC-13 none there", "", "implementation-run.log\nfreeze-run.log\n.*-run.log.tmp*\n"},
		{"AC-13 one there with a leading slash and the last line unended", "*.bak\n/implementation-run.log\nnotes",
			"*.bak\n/implementation-run.log\nnotes\nfreeze-run.log\n.*-run.log.tmp*\n"},
		{"AC-13 each there with trailing space", "freeze-run.log  \r\n/.*-run.log.tmp*\nimplementation-run.log\t\n",
			"freeze-run.log  \r\n/.*-run.log.tmp*\nimplementation-run.log\t\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := wtTempDir(t)
			path := filepath.Join(dir, ".gitignore")
			if c.was != "" {
				if err := os.WriteFile(path, []byte(c.was), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := ignoreRunLogs(dir); err != nil {
				t.Fatalf("ignoreRunLogs: %v", err)
			}
			if got := wtRead(t, path); got != c.want {
				t.Errorf("%s: the .gitignore is %q, want %q", c.name, got, c.want)
			}
			if err := ignoreRunLogs(dir); err != nil || wtRead(t, path) != c.want {
				t.Errorf("%s: a second run changed the .gitignore: %v", c.name, err)
			}
		})
	}
}

func TestAC13AStoryGitignoreWithASecondNameIsReplacedAndTheOtherNameLeftAsItWas(t *testing.T) {
	t.Parallel()
	dir := wtTempDir(t)
	other := filepath.Join(wtTempDir(t), "session")
	if err := os.WriteFile(other, []byte("implementation-run.log\nfreeze-run.log\n.*-run.log.tmp*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".gitignore")
	if err := os.Link(other, path); err != nil {
		t.Skipf("this file system makes no hard link: %v", err)
	}
	if err := ignoreRunLogs(dir); err != nil {
		t.Fatalf("ignoreRunLogs: %v", err)
	}
	if got := wtRead(t, path); got != "implementation-run.log\nfreeze-run.log\n.*-run.log.tmp*\n" {
		t.Errorf("AC-13 the .gitignore is %q, want the three lines written new", got)
	}
	a, errA := os.Stat(path)
	b, errB := os.Stat(other)
	if errA != nil || errB != nil || os.SameFile(a, b) {
		t.Errorf("AC-13 the .gitignore is still a second name of %q: it was written through", other)
	}
}

func TestAC13AStoryGitignoreThatCannotBeWrittenIsRefusedAsUnwritable(t *testing.T) {
	t.Parallel()
	notADir := filepath.Join(wtTempDir(t), "US-001")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ignoreRunLogs(notADir)
	if !grCode(err, sdlcerr.StateUnwritable) {
		t.Fatalf("AC-13 ignoreRunLogs under a file = %v, want %s", err, sdlcerr.StateUnwritable)
	}
	var e *sdlcerr.Error
	if errors.As(err, &e) && !strings.Contains(e.What, ".gitignore") {
		t.Errorf("AC-13 the refusal %q does not name the .gitignore", e.What)
	}
}
