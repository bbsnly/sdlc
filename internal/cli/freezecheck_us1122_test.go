package cli

// US-112.2: `sdlc freeze` runs commands.freeze_check first, and a test file that
// CI would refuse is corrected by its author before the freeze, not after the
// plan and its reviews at the cost of a person's unfreeze.
//
// Every check here reports from inside itself, through a file outside the
// project: what SDLC_FREEZE_FILES held, and that it ran. Nothing reads the
// command's text back, and the commands vary, so that neither special-casing a
// command nor ignoring the list passes. Helpers carry an fc prefix.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/shellx"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	// fcCode is what every refusal of the freeze check takes (decision 12).
	fcCode = "SDLC-E0085"
	// fcKey is the name the record and the refusals give the command, whose text
	// can hold a token (decision 11).
	fcKey = "commands.freeze_check"
	// fcDoctor is the line doctor gives the check, which the staffing-off files
	// and the sandbox's transcript hold word for word.
	fcDoctor = "freeze check"
	// fcNotSet is what that line says with no check configured.
	fcNotSet = "commands.freeze_check is not set, so sdlc freeze runs no check"

	// fcFilesVariable is spelled here, and not taken from the code, so that a
	// renamed variable fails.
	fcFilesVariable = "SDLC_FREEZE_FILES"

	fcFailing = "exit 1"
	fcPassing = "exit 0"
	fcTest    = "package internal\n"
	fcTwo     = "package internal\n\n// the second version\n"
)

// fcSpy is a check's report from inside itself, in files outside the project.
type fcSpy struct{ files, runs string }

func newFCSpy(t *testing.T) fcSpy {
	t.Helper()
	dir := wtTempDir(t)
	return fcSpy{files: filepath.Join(dir, "listed"), runs: filepath.Join(dir, "runs")}
}

// reports is the shell words that make the report, ending with success.
func (s fcSpy) reports() string {
	return `printf '%s' "${SDLC_FREEZE_FILES-UNSET}" > ` + suSh(s.files) + `; echo ran >> ` + suSh(s.runs)
}

// raw is what the check last found in SDLC_FREEZE_FILES.
func (s fcSpy) raw(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(s.files)
	if err != nil {
		t.Fatalf("the check did not run, or could not report: %v", err)
	}
	return string(raw)
}

// listed is the files the check was last handed, a name to a line, sorted.
func (s fcSpy) listed(t *testing.T) []string {
	t.Helper()
	raw := s.raw(t)
	if raw == "" {
		return nil
	}
	names := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	slices.Sort(names)
	return names
}

// times is how many times the check has run.
func (s fcSpy) times(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(s.runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "ran\n")
}

func (s fcSpy) wantListed(t *testing.T, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := s.listed(t); !slices.Equal(got, want) {
		t.Errorf("SDLC_FREEZE_FILES held\n%q\nwant\n%q", got, want)
	}
}

// fcSet makes command this project's freeze check and its only command.
func fcSet(t *testing.T, root, command string) {
	t.Helper()
	setConfigOnDisk(t, root, "commands", map[string]any{"freeze_check": command})
}

// fcStarted is US-001 under way in a checkout of its own, story worktrees off,
// with the files given committed before it started.
func fcStarted(t *testing.T, committed map[string]string) string {
	t.Helper()
	root := gitProjectInitialised(t)
	if len(committed) > 0 {
		for rel, body := range committed {
			writeFile(t, root, rel, body)
		}
		commitEverything(t, root)
	}
	mustRun(t, "start")
	return root
}

// fcCommitStaged commits what is staged in dir, and only that.
func fcCommitStaged(t *testing.T, dir, message string) {
	t.Helper()
	wtGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
}

// fcRecord is the path of US-001's record in the main checkout.
func fcRecord(root string) string { return wtRecordPath(root, "US-001") }

// fcFreezes is the messages of the record's freeze events, in order.
func fcFreezes(t *testing.T, recordPath string) []string {
	t.Helper()
	var record model.Record
	wtReadJSON(t, recordPath, &record)
	var messages []string
	for _, e := range record.Events {
		if e.Type == "freeze" {
			messages = append(messages, e.Message)
		}
	}
	return messages
}

// fcSetRecord sets one top-level key of a record, or takes it away where value
// is empty: where a gate has moved the base, or none was ever recorded.
func fcSetRecord(t *testing.T, recordPath, key, value string) {
	t.Helper()
	var record map[string]any
	wtReadJSON(t, recordPath, &record)
	if value == "" {
		delete(record, key)
	} else {
		record[key] = value
	}
	wtWriteJSON(t, recordPath, record)
}

// fcLockPath is the freeze of the checkout at root.
func fcLockPath(root string) string {
	return filepath.Join(root, ".sdlc", "state", "tests.lock")
}

// fcUnfrozen holds that no freeze is on disk at root.
func fcUnfrozen(t *testing.T, root string) {
	t.Helper()
	if wtExists(fcLockPath(root)) {
		t.Errorf("a freeze was taken:\n%s", wtRead(t, fcLockPath(root)))
	}
}

// fcRefused holds that r is a refusal of the freeze check that froze nothing and
// recorded nothing in the record given, whose bytes before it are recordBefore.
func fcRefused(t *testing.T, r result, root, recordBefore string) {
	t.Helper()
	if r.code == 0 {
		t.Errorf("the freeze went ahead:\n%s%s", r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, fcCode) {
		t.Errorf("the refusal does not carry %s: exit %d\n%s", fcCode, r.code, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("a refusal wrote to stdout, which a skill parses as data: %q", r.stdout)
	}
	fcUnfrozen(t, root)
	if got := wtRead(t, fcRecord(root)); got != recordBefore {
		t.Errorf("the refused freeze changed the record:\n%s\nwas\n%s", got, recordBefore)
	}
}

// fcFlat is text in lower case with its white space collapsed.
func fcFlat(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// ---------------------------------------------------------------------- AC-1

func TestAC1TheCheckIsHandedTheNewAndChangedTestFilesOnePerLineSlashSeparated(t *testing.T) {
	t.Parallel()
	root := fcStarted(t, map[string]string{
		".gitignore":                 "/.sdlc/state/\nignored_test.go\n",
		"internal/kept_test.go":      fcTest,
		"internal/changed_test.go":   fcTest,
		"internal/committed_test.go": fcTest,
		"testdata/kept.txt":          "kept\n",
		"testdata/changed.txt":       "was\n",
		"notes.md":                   "notes\n",
	})
	writeFile(t, root, "internal/committed_test.go", fcTwo)
	writeFile(t, root, "internal/added_later_test.go", fcTest)
	suCommit(t, root, "tests committed since the base", "internal/committed_test.go", "internal/added_later_test.go")

	writeFile(t, root, "internal/changed_test.go", fcTwo)
	writeFile(t, root, "testdata/changed.txt", "is\n")
	writeFile(t, root, "internal/deep/er/new_test.go", fcTest)
	writeFile(t, root, "testdata/new fixture.txt", "new\n")
	writeFile(t, root, "internal/café_test.go", fcTest)
	writeFile(t, root, "internal/staged_test.go", fcTest)
	wtGit(t, root, "add", "--", "internal/staged_test.go")
	// None of these is the check's business.
	writeFile(t, root, "ignored_test.go", fcTest)
	writeFile(t, root, "notes.md", "changed notes\n")
	writeFile(t, root, "more notes.md", "new notes\n")

	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())
	mustRun(t, "freeze")

	spy.wantListed(t,
		"internal/added_later_test.go",
		"internal/café_test.go",
		"internal/changed_test.go",
		"internal/committed_test.go",
		"internal/deep/er/new_test.go",
		"internal/staged_test.go",
		"testdata/changed.txt",
		"testdata/new fixture.txt",
	)
	// The list is what the check judges. What the freeze holds is every test.
	lock := lockOnDisk(t, root)
	for _, held := range []string{"internal/kept_test.go", "testdata/kept.txt", "internal/changed_test.go"} {
		if len(lock.Files[held]) != 64 {
			t.Errorf("the freeze does not hold %s by content: %v", held, lock.Files)
		}
	}
	if _, held := lock.Files["ignored_test.go"]; held {
		t.Errorf("the freeze holds a file git ignores: %v", lock.Files)
	}
}

func TestAC1TheCheckRunsFromTheRepositoryRootBeforeAnyHashIsRecorded(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	writeFile(t, root, "at-the-root.txt", "here\n")
	if err := os.MkdirAll(filepath.Join(root, "internal", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	where := filepath.Join(wtTempDir(t), "where")
	// Each of these exits 0 only where it holds, and says which did not.
	fcSet(t, root, `test -f at-the-root.txt || { echo "not at the root" > `+suSh(where)+`; exit 3; }
if test -e .sdlc/state/tests.lock; then echo "a freeze is on disk" > `+suSh(where)+`; exit 4; fi
if grep -q '"type": *"freeze"' .sdlc/stories/US-001/gate-record.json; then echo "the freeze is on the record" > `+suSh(where)+`; exit 5; fi
echo "ran" > `+suSh(where))

	chdir(t, filepath.Join(root, "internal", "deep"))
	r := run(t, "freeze")
	if said := strings.TrimSpace(wtRead(t, where)); said != "ran" {
		t.Errorf("the check says %q: exit %d\n%s", said, r.code, r.stderr)
	}
	if r.code != 0 || !wtExists(fcLockPath(root)) {
		t.Errorf("the freeze was not taken after the check passed: exit %d\n%s", r.code, r.stderr)
	}
}

// However the command is written, it is the shell that runs it and the list that
// it sees: a list worked out from the words of the command would pass the rest.
func TestAC1EveryFormOfCommandSeesTheSameList(t *testing.T) {
	t.Parallel()
	root := fcStarted(t, map[string]string{".gitignore": "/.sdlc/state/\n", "old_test.go": fcTest})
	writeFile(t, root, "pkg/new_test.go", fcTest)
	script := filepath.Join(wtTempDir(t), "the check.sh")
	spy := newFCSpy(t)
	if err := os.WriteFile(script, []byte(spy.reports()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, command := range map[string]string{
		"AC-1 a plain command":    spy.reports(),
		"AC-1 in a subshell":      "( " + spy.reports() + " )",
		"AC-1 under a condition":  `if [ -n "$SDLC_FREEZE_FILES" ]; then ` + spy.reports() + `; else exit 9; fi`,
		"AC-1 in a script":        "sh " + suSh(script),
		"AC-1 after another":      "true && " + spy.reports(),
		"AC-1 with a leading tab": "\t" + spy.reports(),
	} {
		t.Run(name, func(t *testing.T) {
			before := spy.times(t)
			fcSet(t, root, command)
			mustRun(t, "freeze")
			if spy.times(t) != before+1 {
				t.Fatalf("the check ran %d times, want one more than %d", spy.times(t), before)
			}
			spy.wantListed(t, "pkg/new_test.go")
			mustRun(t, "unfreeze", "--reason", "the next form of the command")
		})
	}
}

// A second freeze with nothing new or changed still runs the check, with the
// variable there and holding nothing: a check that loops over it has no work.
func TestAC1TheCheckRunsWithAnEmptyListWhereNothingIsNewOrChanged(t *testing.T) {
	t.Parallel()
	root := fcStarted(t, map[string]string{"old_test.go": fcTest})
	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())
	mustRun(t, "freeze")
	if spy.times(t) != 1 {
		t.Fatalf("the check ran %d times, want once", spy.times(t))
	}
	if got := spy.raw(t); got != "" {
		t.Errorf("SDLC_FREEZE_FILES holds %q, want it set and empty", got)
	}
}

// Names git allows reach the check as they are. Each is a line of the variable:
// none is quoted, escaped or expanded on the way, and none runs.
func TestAC1ANameIsListedAsGitHasItAndNothingInItRuns(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow these characters in a file name")
	}
	root := fcStarted(t, map[string]string{".gitignore": "/.sdlc/state/\n", "old_test.go": fcTest})
	names := []string{
		"a name with spaces_test.go",
		"$(touch pwned)_test.go",
		"`touch pwned`_test.go",
		"-leading-dash_test.go",
		"it's_test.go",
		`quo"ted_test.go`,
		`back\slash_test.go`,
		"tab\there_test.go",
		"internal/*_test.go",
	}
	for _, name := range names {
		writeFile(t, root, name, fcTest)
	}
	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())
	mustRun(t, "freeze")
	spy.wantListed(t, names...)
	if wtExists(filepath.Join(root, "pwned")) {
		t.Error("a file name was run as a command")
	}
}

// ---------------------------------------------------------- AC-1, decision 1

func TestAC1Decision1TheBaseIsTheCutOfAStoryWorktreeWhereverTheBaseHasMovedTo(t *testing.T) {
	t.Parallel()
	main := ldRepository(t, func(root string) { writeFile(t, root, "old_test.go", fcTest) })
	wt := swCheckStarted(t, main, "US-001", ldSession, swStarted(t, main, ldSession))
	writeFile(t, wt, "early_test.go", fcTest)
	suCommit(t, wt, "a test on the story's branch", "early_test.go")
	// What a gate does to the base once code review has passed.
	fcSetRecord(t, wtRecordPath(main, "US-001"), "base", strings.TrimSpace(wtGit(t, wt, "rev-parse", "HEAD")))
	writeFile(t, wt, "late_test.go", fcTest)

	spy := newFCSpy(t)
	fcSet(t, wt, spy.reports())
	wtMust(t, wt, ldSession, "", "freeze")

	spy.wantListed(t, "early_test.go", "late_test.go")
	if lock := lockOnDisk(t, wt); len(lock.Files["old_test.go"]) != 64 {
		t.Errorf("the freeze does not hold the test trunk had: %v", lock.Files)
	}
}

func TestAC1Decision1WithNoCutTheBaseIsTheRecordsBaseAsItNowStands(t *testing.T) {
	t.Parallel()
	root := fcStarted(t, map[string]string{"old_test.go": fcTest})
	writeFile(t, root, "early_test.go", fcTest)
	suCommit(t, root, "a test since the start", "early_test.go")
	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())

	t.Run("AC-1 the base where the story started", func(t *testing.T) {
		mustRun(t, "freeze")
		spy.wantListed(t, "early_test.go")
		mustRun(t, "unfreeze", "--reason", "the base is about to move")
	})
	t.Run("AC-1 the base a gate moved it to, which an unfreeze does not reset", func(t *testing.T) {
		fcSetRecord(t, fcRecord(root), "base", strings.TrimSpace(wtGit(t, root, "rev-parse", "HEAD")))
		writeFile(t, root, "late_test.go", fcTest)
		mustRun(t, "freeze")
		spy.wantListed(t, "late_test.go")
		lock := lockOnDisk(t, root)
		for _, held := range []string{"old_test.go", "early_test.go", "late_test.go"} {
			if len(lock.Files[held]) != 64 {
				t.Errorf("a shorter list changed what the freeze holds, which is every test: %v", lock.Files)
			}
		}
	})
}

func TestAC1Decision1WithNoCutAndNoBaseEveryTestFileIsListed(t *testing.T) {
	t.Parallel()
	root := fcStarted(t, map[string]string{"old_test.go": fcTest, "testdata/old.txt": "old\n", "notes.md": "notes\n"})
	writeFile(t, root, "new_test.go", fcTest)
	fcSetRecord(t, fcRecord(root), "base", "")
	fcSetRecord(t, fcRecord(root), "cut", "")

	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())
	mustRun(t, "freeze")
	spy.wantListed(t, "new_test.go", "old_test.go", "testdata/old.txt")
}

// A base git cannot read cannot say what changed, so the check is handed every
// test file and the freeze is taken: it sees more, never less. A refusal here
// would stop a story for a record no test author can correct, and a shorter list
// would freeze files the check was not shown.
func TestAC1Decision1ABaseGitCannotReadHandsTheCheckEveryTestFileWithNoRefusal(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ key, value string }{
		"AC-1 a commit git does not have":                {"base", strings.Repeat("a", 40)},
		"AC-1 the name git gives no commit":              {"base", strings.Repeat("0", 40)},
		"AC-1 no commit's name at all":                   {"base", "not-a-revision"},
		"AC-1 a cut git does not have, with a good base": {"cut", strings.Repeat("b", 40)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := fcStarted(t, map[string]string{"old_test.go": fcTest, "testdata/old.txt": "old\n", "notes.md": "notes\n"})
			writeFile(t, root, "new_test.go", fcTest)
			fcSetRecord(t, fcRecord(root), c.key, c.value)

			spy := newFCSpy(t)
			fcSet(t, root, spy.reports())
			mustRun(t, "freeze")
			spy.wantListed(t, "new_test.go", "old_test.go", "testdata/old.txt")
			if !wtExists(fcLockPath(root)) {
				t.Error("no freeze was taken")
			}
		})
	}
}

// ---------------------------------------------------------- AC-1, decision 5

func TestAC1Decision5ADeletedTestFileIsLeftOutOfTheListAndARenameListsTheNewPathOnly(t *testing.T) {
	t.Parallel()
	committed := func() map[string]string {
		return map[string]string{"old_test.go": fcTest, "gone_test.go": fcTest, "moved_test.go": fcTest, "reworked_test.go": fcTest + "// a long enough file for git to call a rename\n"}
	}
	t.Run("AC-1 deleted and committed", func(t *testing.T) {
		t.Parallel()
		root := fcStarted(t, committed())
		wtGit(t, root, "rm", "--quiet", "--", "gone_test.go")
		fcCommitStaged(t, root, "the test goes")
		writeFile(t, root, "new_test.go", fcTest)
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports())
		mustRun(t, "freeze")
		spy.wantListed(t, "new_test.go")
		if _, held := lockOnDisk(t, root).Files["gone_test.go"]; held {
			t.Error("the freeze holds a file that is gone")
		}
	})
	t.Run("AC-1 deleted and staged", func(t *testing.T) {
		t.Parallel()
		root := fcStarted(t, committed())
		wtGit(t, root, "rm", "--quiet", "--", "gone_test.go")
		writeFile(t, root, "new_test.go", fcTest)
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports())
		mustRun(t, "freeze")
		spy.wantListed(t, "new_test.go")
	})
	t.Run("AC-1 renamed and committed", func(t *testing.T) {
		t.Parallel()
		root := fcStarted(t, committed())
		wtGit(t, root, "mv", "--", "moved_test.go", "arrived_test.go")
		fcCommitStaged(t, root, "the test moves")
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports())
		mustRun(t, "freeze")
		spy.wantListed(t, "arrived_test.go")
	})
	t.Run("AC-1 renamed and staged", func(t *testing.T) {
		t.Parallel()
		root := fcStarted(t, committed())
		wtGit(t, root, "mv", "--", "moved_test.go", "arrived_test.go")
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports())
		mustRun(t, "freeze")
		spy.wantListed(t, "arrived_test.go")
	})
	t.Run("AC-1 renamed and changed", func(t *testing.T) {
		t.Parallel()
		root := fcStarted(t, committed())
		wtGit(t, root, "mv", "--", "reworked_test.go", "rewritten_test.go")
		writeFile(t, root, "rewritten_test.go", fcTest+"// a long enough file for git to call a rename\n// and one line more\n")
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports())
		mustRun(t, "freeze")
		spy.wantListed(t, "rewritten_test.go")
	})
}

// A tracked test deleted and not yet staged is refused by the freeze as it is
// today, check or no check, because the freeze cannot read it. If the check runs
// first, the file is not in what it is handed.
func TestAC1Decision5ADeletedTestGitStillTracksIsNotListedAndIsStillRefused(t *testing.T) {
	t.Parallel()
	root := fcStarted(t, map[string]string{"old_test.go": fcTest, "gone_test.go": fcTest})
	if err := os.Remove(filepath.Join(root, "gone_test.go")); err != nil {
		t.Fatal(err)
	}
	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())
	r := run(t, "freeze")
	if r.code == 0 || !strings.Contains(r.stderr, suStateUnreadable) || !strings.Contains(r.stderr, "gone_test.go") {
		t.Errorf("the freeze of a test that is gone: exit %d\n%s", r.code, r.stderr)
	}
	fcUnfrozen(t, root)
	if spy.times(t) > 0 && slices.Contains(spy.listed(t), "gone_test.go") {
		t.Errorf("the check was handed a file that is gone: %q", spy.listed(t))
	}
}

// ---------------------------------------------------------------- newlines

// One name to a line cannot carry a name with a line in it: it would arrive as
// two names, neither of them real, and the freeze would hold a file the check
// never saw. So the freeze is refused, and the check is not run on a list that
// says something false.
func TestAC1ATestFileWhoseNameHoldsANewlineRefusesTheFreezeAndSaysToRenameIt(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow a newline in a file name")
	}
	root := fcStarted(t, map[string]string{"old_test.go": fcTest})
	writeFile(t, root, "bad\nname_test.go", fcTest)
	writeFile(t, root, "good_test.go", fcTest)
	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())
	before := wtRead(t, fcRecord(root))

	r := run(t, "freeze")
	fcRefused(t, r, root, before)
	if spy.times(t) != 0 {
		t.Errorf("the check ran on a list holding %q", spy.raw(t))
	}
	if !strings.Contains(fcFlat(r.stderr), "rename") {
		t.Errorf("the refusal does not say to rename the file:\n%s", r.stderr)
	}
	if strings.Contains(r.stderr, "bad\nname") {
		t.Errorf("the refusal prints the name's newline, which lets a file name write sdlc's words:\n%s", r.stderr)
	}
	if !strings.Contains(r.stderr, `bad\nname_test.go`) {
		t.Errorf("the refusal does not name the file:\n%s", r.stderr)
	}

	// The author does what it says, and that is all it takes.
	if err := os.Rename(filepath.Join(root, "bad\nname_test.go"), filepath.Join(root, "renamed_test.go")); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "freeze")
	spy.wantListed(t, "good_test.go", "renamed_test.go")
}

// ---------------------------------------------------------------------- AC-2

func TestAC2AFailingCheckRecordsNothingTakesNoFreezeAndSaysWhatToDo(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	for _, exit := range []int{1, 2, 126, 127, 255} {
		t.Run(fmt.Sprintf("AC-2 exit %d", exit), func(t *testing.T) {
			fcSet(t, root, fmt.Sprintf("echo the check said this; exit %d", exit))
			before := wtRead(t, fcRecord(root))
			r := run(t, "freeze")
			fcRefused(t, r, root, before)
			if r.code != 1 {
				t.Errorf("exit %d: a refusal exits 1, whatever the check did", r.code)
			}
			flat := fcFlat(r.stderr)
			for _, want := range []string{fcKey, "the check said this", "test author", `sdlc freeze`, "person"} {
				if !strings.Contains(flat, strings.ToLower(want)) {
					t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
				}
			}
		})
	}
	// The story is where it was: this is the test author's to put right.
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")); got.Active != "US-001" {
		t.Errorf("after a refused freeze, status = %+v", got)
	}
}

func TestAC2ACheckKilledByASignalIsAFailingCheck(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a process does not end by a signal here")
	}
	root := frozenStory(t)
	fcSet(t, root, "echo about to go; kill -9 $$")
	before := wtRead(t, fcRecord(root))
	r := run(t, "freeze")
	fcRefused(t, r, root, before)
	if !strings.Contains(r.stderr, "about to go") {
		t.Errorf("the refusal does not show what the check printed:\n%s", r.stderr)
	}
}

// fcNumbered is shell words that print n numbered lines, odd ones on stderr, as
// a linter and the tools under it share one terminal, and then fail.
func fcNumbered(n int) string {
	return fmt.Sprintf(`i=1; while [ $i -le %d ]; do if [ $((i %% 2)) -eq 1 ]; then printf 'L%%04d\n' $i >&2; else printf 'L%%04d\n' $i; fi; i=$((i+1)); done; exit 1`, n)
}

// The last of what the check said is where it says what went wrong, and a tail
// of each stream by itself would show neither in the order it was said.
func TestAC2TheRefusalShowsTheLastFortyLinesOfBothStreamsTogether(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	for _, c := range []struct {
		name         string
		lines, first int
	}{
		{"AC-2 one line", 1, 1},
		{"AC-2 thirty-nine lines", 39, 1},
		{"AC-2 exactly forty lines", 40, 1},
		{"AC-2 forty-one lines", 41, 2},
		{"AC-2 a hundred lines", 100, 61},
		{"AC-2 five thousand lines", 5000, 4961},
	} {
		t.Run(c.name, func(t *testing.T) {
			fcSet(t, root, fcNumbered(c.lines))
			r := run(t, "freeze")
			if !strings.Contains(r.stderr, fcCode) {
				t.Fatalf("exit %d:\n%s", r.code, r.stderr)
			}
			shown := regexp.MustCompile(`L\d{4}`).FindAllString(r.stderr, -1)
			var want []string
			for i := c.first; i <= c.lines; i++ {
				want = append(want, fmt.Sprintf("L%04d", i))
			}
			if !slices.Equal(shown, want) {
				t.Errorf("the refusal shows %v, want %v", shown, want)
			}
		})
	}
}

func TestAC2ACheckThatPrintsNothingStillRefusesAndSaysSo(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	fcSet(t, root, fcFailing)
	before := wtRead(t, fcRecord(root))
	r := run(t, "freeze")
	fcRefused(t, r, root, before)
	if !strings.Contains(r.stderr, fcKey) {
		t.Errorf("the refusal does not name the command:\n%s", r.stderr)
	}
}

func TestAC2ALastLineWithNoNewlineIsShown(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	fcSet(t, root, `echo first; printf 'the very last'; exit 1`)
	r := run(t, "freeze")
	for _, want := range []string{"first", "the very last"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal does not show %q:\n%s", want, r.stderr)
		}
	}
}

// Decision 8: forty lines of what a linter really prints all fit, and one line
// as long as a minified file does not flood the session. The end of it stays.
func TestAC2Decision8TheTailIsCappedInBytesAndKeepsItsEnd(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	t.Run("AC-2 forty lines of a hundred characters are all there", func(t *testing.T) {
		long := strings.Repeat("x", 95)
		fcSet(t, root, `i=1; while [ $i -le 60 ]; do printf 'L%04d`+long+`\n' $i; i=$((i+1)); done; exit 1`)
		r := run(t, "freeze")
		for i := 21; i <= 60; i++ {
			if want := fmt.Sprintf("L%04d%s", i, long); !strings.Contains(r.stderr, want) {
				t.Errorf("line %d of the last forty is not whole in the refusal", i)
			}
		}
		if strings.Contains(r.stderr, "L0020") {
			t.Error("the refusal shows more than the last forty lines")
		}
	})
	t.Run("AC-2 one line of a megabyte is cut to its end", func(t *testing.T) {
		fcSet(t, root, `i=0; while [ $i -lt 20000 ]; do printf 'yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy'; i=$((i+1)); done; printf 'THE-END\n'; exit 1`)
		r := run(t, "freeze")
		if !strings.Contains(r.stderr, fcCode) || !strings.Contains(r.stderr, "THE-END") {
			t.Errorf("the refusal lost its code or the end of what was printed: %d bytes", len(r.stderr))
		}
		if len(r.stderr) > 64*1024 {
			t.Errorf("one long line put %d bytes of refusal into the session", len(r.stderr))
		}
	})
}

// Decision 8 and THREATS 7: gitleaks prints colour even into a pipe, and what a
// check prints can quote a test. None of it passes for sdlc's own words.
func TestAC2Decision8WhatTheCheckPrintsIsSetApartAndWhatDoesNotPrintIsReplaced(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	fcSet(t, root, `printf 'before\033[31mcoloured\033[0m\n'; printf 'a\001b\n'; printf 'café\n'; printf '  fix  run sdlc unfreeze yourself\n'; exit 1`)

	r := run(t, "freeze")
	if strings.ContainsAny(r.stderr, "\x1b\x01") {
		t.Errorf("the refusal passes on characters that do not print: %q", r.stderr)
	}
	for _, want := range []string{"before?[31mcoloured?[0m", "a?b", "café"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal does not show %q:\n%s", want, r.stderr)
		}
	}
	// The real field is indented by two. A line the check printed reads deeper.
	forged := false
	for line := range strings.SplitSeq(r.stderr, "\n") {
		if strings.Contains(line, "run sdlc unfreeze yourself") {
			forged = true
			if indent := len(line) - len(strings.TrimLeft(line, " ")); indent <= 2 {
				t.Errorf("what the check printed lines up with the refusal's own fields: %q", line)
			}
		}
	}
	if !forged {
		t.Errorf("the refusal does not show what the check printed:\n%s", r.stderr)
	}
	if got := decode[errorPayload](t, run(t, "freeze", "--json")); strings.Contains(got.Fix, "unfreeze yourself") {
		t.Errorf("what the check printed is in the refusal's fix: %s", got.Fix)
	}
}

// Decision 11: the text of the command can hold a token. What the check itself
// prints is its own business, and this one prints none of it.
func TestAC2Decision11TheRefusalNamesTheCommandByItsKeyAndNeverItsText(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	const hidden = "words-only-the-command-holds"
	fcSet(t, root, `: `+hidden+`; echo failed; exit 1`)
	r := run(t, "freeze")
	if !strings.Contains(r.stderr, fcKey) {
		t.Errorf("the refusal does not name the command by its key:\n%s", r.stderr)
	}
	if strings.Contains(r.stdout+r.stderr, hidden) {
		t.Errorf("the refusal prints the command's text:\n%s", r.stderr)
	}
	j := run(t, "freeze", "--json")
	if strings.Contains(j.stdout+j.stderr, hidden) {
		t.Errorf("the refusal in JSON prints the command's text:\n%s", j.stdout)
	}
}

func TestAC2TheRefusalInJSONCarriesTheCodeTheFixAndWhatTheCheckPrinted(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	fcSet(t, root, `echo the-last-thing-it-said; exit 1`)
	r := run(t, "freeze", "--json")
	got := decode[errorPayload](t, r)
	if r.code != 1 || got.OK || got.Code != fcCode {
		t.Errorf("exit %d, %+v", r.code, got)
	}
	if !strings.Contains(got.Fix, "sdlc freeze") {
		t.Errorf("the fix does not say to freeze again: %s", got.Fix)
	}
	if !strings.Contains(r.stdout+r.stderr, "the-last-thing-it-said") {
		t.Errorf("what the check printed is nowhere in the refusal:\n%s%s", r.stdout, r.stderr)
	}
	fcUnfrozen(t, root)
}

// AC-2's promise end to end: the author corrects the file, runs it again, and no
// person is asked. The check reads the file the list names, so the second run is
// a different answer because the file is different, and for no other reason.
func TestAC2TheAuthorCorrectsTheFileAndTheFreezeIsTakenWithNoPerson(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// LINT-ERROR\n")
	fcSet(t, root, `for f in $SDLC_FREEZE_FILES; do if grep -q LINT-ERROR "$f"; then echo "$f: lint error"; exit 1; fi; done`)

	r := run(t, "freeze")
	if !strings.Contains(r.stderr, "internal/invoice_test.go: lint error") {
		t.Errorf("the refusal does not show which file the check named:\n%s", r.stderr)
	}
	fcUnfrozen(t, root)

	writeFile(t, root, "internal/invoice_test.go", "package internal\n")
	mustRun(t, "freeze")
	if lock := lockOnDisk(t, root); lock.Story != "US-001" || len(lock.Files["internal/invoice_test.go"]) != 64 {
		t.Errorf("the freeze after the correction: %+v", lock)
	}
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test")
}

// A refusal the freeze already made is the one a person sees: the check is not
// run to find out something that does not matter yet.
func TestAC2ACheckDoesNotHideTheRefusalsTheFreezeAlreadyMakes(t *testing.T) {
	t.Parallel()
	t.Run("AC-2 nothing to freeze", func(t *testing.T) {
		t.Parallel()
		root := fcStarted(t, nil)
		fcSet(t, root, fcFailing)
		if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0024") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
	})
	t.Run("AC-2 already frozen", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		mustRun(t, "freeze")
		held := wtRead(t, fcLockPath(root))
		fcSet(t, root, fcFailing)
		if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0022") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		if wtRead(t, fcLockPath(root)) != held {
			t.Error("the refused freeze changed the freeze on disk")
		}
	})
	t.Run("AC-2 a freeze that went missing", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		mustRun(t, "freeze")
		if err := os.Remove(fcLockPath(root)); err != nil {
			t.Fatal(err)
		}
		fcSet(t, root, fcFailing)
		if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0025") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
	})
}

// A finished story's freeze is replaced by this one's. A refused check takes no
// freeze for this story, whatever became of the old one, and the next run works.
func TestAC2AFailingCheckTakesNoFreezeOverAFinishedStorysLeftover(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wtStarted(t, main, wtSessionMain, "US-001")
	writeFile(t, main, "old_test.go", fcTest)
	wtSetStatus(t, main, "US-002", model.StatusDone)
	writeFile(t, main, ".sdlc/state/tests.lock",
		`{"schema":"`+model.LockSchema+`","story":"US-002","at":"2026-10-31T00:00:00Z","files":{}}`)

	fcSet(t, main, fcFailing)
	if r := wtRun(t, main, wtSessionMain, "", "freeze"); !strings.Contains(r.stderr, fcCode) {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	if wtExists(fcLockPath(main)) {
		if lock := lockOnDisk(t, main); lock.Story == "US-001" {
			t.Errorf("a refused check took a freeze for the story: %+v", lock)
		}
	}

	fcSet(t, main, fcPassing)
	wtMust(t, main, wtSessionMain, "", "freeze")
	if lock := lockOnDisk(t, main); lock.Story != "US-001" {
		t.Errorf("the freeze after a passing check is %q's", lock.Story)
	}
}

// A check is judged by how it ended, and a daemon it left behind holding its
// output open neither hangs the freeze nor changes the answer.
func TestAC2ACheckIsJudgedByHowItEndedWhateverItLeftBehindHoldingItsOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the background process here is POSIX shell; the group is held by internal/shellx on Windows")
	}
	// It outlives the wait for its output, and ends by itself soon after.
	const daemon = `(sleep 7 &) ; `
	t.Run("AC-2 a pass stays a pass", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		spy := newFCSpy(t)
		fcSet(t, root, daemon+spy.reports())
		mustRun(t, "freeze")
		if spy.times(t) != 1 {
			t.Errorf("the check ran %d times, want once", spy.times(t))
		}
		if !wtExists(fcLockPath(root)) {
			t.Error("no freeze was taken")
		}
	})
	t.Run("AC-2 a failure stays a failure", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		fcSet(t, root, daemon+`echo it-said-this; exit 1`)
		r := run(t, "freeze")
		if !strings.Contains(r.stderr, fcCode) {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		fcUnfrozen(t, root)
	})
}

// ---------------------------------------------------------- AC-2, decision 6

func TestAC2Decision6ACheckThatChangesATestFileIsRefusedAndTheFileIsNamed(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, command, names string }{
		{"AC-2 it rewrites a file on the list", `printf '// formatted\n' >> new_test.go`, "new_test.go"},
		{"AC-2 it rewrites a test file not on the list", `printf '// formatted\n' >> kept_test.go`, "kept_test.go"},
		{"AC-2 it writes a test file nobody listed", `printf 'package x\n' > generated_test.go`, "generated_test.go"},
		{"AC-2 it deletes a test file", `rm kept_test.go`, "kept_test.go"},
		{"AC-2 it writes under a directory of tests", `printf 'x\n' > testdata/report.txt`, "testdata/report.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := fcStarted(t, map[string]string{"kept_test.go": fcTest})
			writeFile(t, root, "new_test.go", fcTest)
			fcSet(t, root, c.command)
			before := wtRead(t, fcRecord(root))

			r := run(t, "freeze")
			fcRefused(t, r, root, before)
			if !strings.Contains(r.stderr, c.names) {
				t.Errorf("the refusal does not name %s:\n%s", c.names, r.stderr)
			}
		})
	}
}

// What the check writes elsewhere is not what the freeze holds, and a file put
// back as it was is no change.
func TestAC2Decision6ACheckThatLeavesEveryTestFileAsItWasIsNotRefused(t *testing.T) {
	t.Parallel()
	for name, command := range map[string]string{
		"AC-2 it writes a file that is not a test": `printf 'report\n' > report.log`,
		"AC-2 it touches a test file":              `touch kept_test.go`,
		"AC-2 it writes a file and puts it back":   `cp kept_test.go kept.bak; printf 'x' >> kept_test.go; mv kept.bak kept_test.go`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := fcStarted(t, map[string]string{"kept_test.go": fcTest})
			spy := newFCSpy(t)
			fcSet(t, root, spy.reports()+"; "+command)
			mustRun(t, "freeze")
			if spy.times(t) != 1 {
				t.Errorf("the check ran %d times, want once", spy.times(t))
			}
			if !wtExists(fcLockPath(root)) {
				t.Error("no freeze was taken")
			}
		})
	}
}

// A formatter as the check: refused once for what it did, and passed the second
// time, because the file is by then what it judged. What the freeze holds is
// what the check saw, byte for byte.
func TestAC2Decision6AFormatterChangesTheFileOnceAndTheSecondRunFreezesWhatItJudged(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	fcSet(t, root, `grep -q formatted internal/invoice_test.go || printf '// formatted\n' >> internal/invoice_test.go`)
	r := run(t, "freeze")
	if !strings.Contains(r.stderr, fcCode) || !strings.Contains(r.stderr, "internal/invoice_test.go") {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	fcUnfrozen(t, root)
	mustRun(t, "freeze")
	if !strings.Contains(wtRead(t, filepath.Join(root, "internal", "invoice_test.go")), "formatted") {
		t.Fatal("the formatter did not run")
	}
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test")
}

// --------------------------------------------- AC-2, decisions 3 and 12: no shell

// fcShellless is a directory holding git and nothing else: PATH with no shell on
// it, on which the rest of the freeze still works.
func fcShellless(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in program is a symlink, and Git for Windows finds its own shell")
	}
	found, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("no git: %v", err)
	}
	bin := wtTempDir(t)
	if err := os.Symlink(found, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	return bin
}

// Not parallel: PATH is the process's, and only a test Go runs by itself may
// change it. Smoke warns and goes on with no shell; a freeze that did that would
// hold tests no check saw.
func TestAC2Decision3WithNoShellTheFreezeIsRefusedRatherThanTakenUnchecked(t *testing.T) {
	root := frozenStory(t)
	fcSet(t, root, fcPassing)
	before := wtRead(t, fcRecord(root))
	t.Setenv("PATH", fcShellless(t))

	r := run(t, "freeze")
	fcRefused(t, r, root, before)
	flat := fcFlat(r.stderr)
	if !strings.Contains(flat, fcKey) {
		t.Errorf("the refusal does not name the command:\n%s", r.stderr)
	}
	if !regexp.MustCompile(`\b(sh|bash|shell)\b`).MatchString(flat) || !strings.Contains(flat, "path") {
		t.Errorf("the refusal does not say what has to be on PATH:\n%s", r.stderr)
	}
}

// AC-3 the other way round: with no check there is nothing that needs a shell,
// and a project with none freezes as it does today.
func TestAC3WithNoCheckAndNoShellTheFreezeIsTakenAsItIsToday(t *testing.T) {
	root := frozenStory(t)
	fcSet(t, root, "  ")
	t.Setenv("PATH", fcShellless(t))
	if r := run(t, "freeze"); r.code != 0 || !wtExists(fcLockPath(root)) {
		t.Errorf("exit %d\n%s", r.code, r.stderr)
	}
}

// Decision 12: one code, and a fix of its own for each state, built where the
// error is made. One fix for all three would tell the author of a broken test to
// install a shell.
func TestAC2Decision12EveryFreezeCheckRefusalTakesOneCodeAndAFixOfItsOwn(t *testing.T) {
	root := fcStarted(t, map[string]string{"kept_test.go": fcTest})
	refused := func(command string) errorPayload {
		t.Helper()
		fcSet(t, root, command)
		r := run(t, "freeze", "--json")
		got := decode[errorPayload](t, r)
		if got.Code != fcCode || r.code != 1 {
			t.Fatalf("exit %d, %+v", r.code, got)
		}
		fcUnfrozen(t, root)
		return got
	}
	failed := refused(fcFailing)
	changed := refused(`printf '// formatted\n' >> kept_test.go`)
	writeFile(t, root, "kept_test.go", fcTest)

	fixes := map[string]string{"failed": failed.Fix, "changed": changed.Fix}
	if runtime.GOOS != "windows" {
		writeFile(t, root, "bad\nname_test.go", fcTest)
		fixes["newline"] = refused(fcPassing).Fix
		if err := os.Remove(filepath.Join(root, "bad\nname_test.go")); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", fcShellless(t))
	fixes["no shell"] = refused(fcPassing).Fix

	seen := map[string]string{}
	for state, fix := range fixes {
		if strings.TrimSpace(fix) == "" {
			t.Errorf("the refusal for %q has no fix", state)
		}
		if other, twice := seen[fix]; twice {
			t.Errorf("%q and %q have one fix between them: %s", state, other, fix)
		}
		seen[fix] = state
	}
	flatFailed := fcFlat(failed.Fix)
	for _, want := range []string{"test author", "sdlc freeze", "person"} {
		if !strings.Contains(flatFailed, want) {
			t.Errorf("the fix for a check that failed does not say %q: %s", want, failed.Fix)
		}
	}
}

// ---------------------------------------------------------- AC-2 and AC-1, the
// lock is let go around the check, so everything is asked again after it.

// The check runs with the project's lock let go, so the world can move under it,
// and a freeze for a story that is no longer being worked on is the worst thing
// to take. These commands do by hand what another session could do meanwhile.
func TestAC1Decision3TheStoryIsAskedForAgainAfterTheCheckWhereTheLockWasLetGo(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 the iteration ended", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		fcSet(t, root, `rm -f .sdlc/state/active`)
		r := run(t, "freeze")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0011") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		fcUnfrozen(t, root)
	})
	t.Run("AC-1 another story is being worked on", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		wtStarted(t, main, wtSessionMain, "US-001")
		writeFile(t, main, "new_test.go", fcTest)
		fcSet(t, main, `printf 'US-002\n' > .sdlc/state/active`)
		r := wtRun(t, main, wtSessionMain, "", "freeze")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		fcUnfrozen(t, main)
		if got := fcFreezes(t, wtRecordPath(main, "US-001")); len(got) != 0 {
			t.Errorf("the first story's record says it was frozen: %q", got)
		}
	})
	t.Run("AC-1 a freeze was taken meanwhile, and is not written over", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		const theirs = `{"schema":"` + model.LockSchema + `","story":"US-001","at":"2026-10-31T00:00:00Z","files":{}}`
		fcSet(t, root, `printf '%s' '`+theirs+`' > .sdlc/state/tests.lock`)
		r := run(t, "freeze")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0022") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		if got := wtRead(t, fcLockPath(root)); got != theirs {
			t.Errorf("the freeze taken meanwhile was written over: %s", got)
		}
	})
	t.Run("AC-1 the story went to another checkout", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain, "US-001")
		writeFile(t, main, "new_test.go", fcTest)
		claim, err := filepath.Abs(filepath.Join(main, ".git", "sdlc", "claims", "US-001.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !wtExists(claim) {
			t.Fatalf("the story has no claim at %s", claim)
		}
		var theirs map[string]any
		wtReadJSON(t, claim, &theirs)
		theirs["checkout"] = wt
		spare := filepath.Join(wtTempDir(t), "claim")
		wtWriteJSON(t, spare, theirs)
		fcSet(t, main, `cp `+suSh(spare)+` .git/sdlc/claims/US-001.json`)
		r := wtRun(t, main, wtSessionMain, "", "freeze")
		if r.code == 0 || !strings.Contains(r.stderr, wtHeldElsewhere) {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		fcUnfrozen(t, main)
	})
}

// What cannot be read after the check says nothing about what it judged, so each
// of these refuses with the reason it has, and takes no freeze on the strength of
// the answers it had before the lock was let go.
func TestAC1Decision3WhatCannotBeReadAfterTheCheckRefusesTheFreeze(t *testing.T) {
	t.Parallel()
	const notAFreeze = "this is not a freeze\n"
	t.Run("AC-1 git stopped working", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		fcSet(t, root, `mv .git .git-gone`)
		r := run(t, "freeze")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		fcUnfrozen(t, root)
	})
	t.Run("AC-1 the freeze a first freeze would be taken over does not read", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		fcSet(t, root, `printf '`+notAFreeze+`' > .sdlc/state/tests.lock`)
		if r := run(t, "freeze"); r.code == 0 || !strings.Contains(r.stderr, suStateUnreadable) {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		if got := wtRead(t, fcLockPath(root)); got != notAFreeze {
			t.Errorf("the file was written over: %s", got)
		}
	})
	t.Run("AC-1 the freeze being added to does not read", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		setConfigOnDisk(t, root, "freeze", map[string]any{"allow_new_test_files": true})
		mustRun(t, "freeze")
		writeFile(t, root, "internal/later_test.go", fcTest)
		fcSet(t, root, `printf '`+notAFreeze+`' > .sdlc/state/tests.lock`)
		if r := run(t, "freeze"); r.code == 0 || !strings.Contains(r.stderr, suStateUnreadable) {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		if got := wtRead(t, fcLockPath(root)); got != notAFreeze {
			t.Errorf("the file was written over: %s", got)
		}
	})
}

// The base comes from the record, so a record that does not read cannot say what
// the check is to be handed, and the freeze being added to stays as it is.
func TestAC4Decision10AnAdditionWhoseRecordDoesNotReadAddsNothing(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	setConfigOnDisk(t, root, "freeze", map[string]any{"allow_new_test_files": true})
	fcSet(t, root, fcPassing)
	mustRun(t, "freeze")
	held := wtRead(t, fcLockPath(root))
	writeFile(t, root, "internal/later_test.go", fcTest)
	if err := os.WriteFile(fcRecord(root), []byte("this is not a record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "freeze"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E") {
		t.Errorf("exit %d\n%s", r.code, r.stderr)
	}
	if wtRead(t, fcLockPath(root)) != held {
		t.Error("the freeze changed with the story's record unreadable")
	}
}

// A person can lift the freeze while the check runs: the lock is let go, and an
// unfreeze takes it. Putting the old freeze back with one file more would undo
// their decision, with the event that records it left standing on the record.
func TestAC4Decision3AFreezeLiftedOnTheRecordWhileTheCheckRanIsNotPutBack(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	setConfigOnDisk(t, root, "freeze", map[string]any{"allow_new_test_files": true})
	fcSet(t, root, fcPassing)
	mustRun(t, "freeze")
	writeFile(t, root, "internal/later_test.go", fcTest)

	var record model.Record
	wtReadJSON(t, fcRecord(root), &record)
	record.Append(unfreezeEvent, "a person lifted it", time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	lifted := filepath.Join(wtTempDir(t), "record")
	wtWriteJSON(t, lifted, record)
	fcSet(t, root, `cp `+suSh(lifted)+` .sdlc/stories/US-001/`+model.RecordFile+` && rm -f .sdlc/state/tests.lock`)

	before := wtRead(t, lifted)
	if r := run(t, "freeze"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E") {
		t.Errorf("exit %d\n%s", r.code, r.stderr)
	}
	fcUnfrozen(t, root)
	if got := wtRead(t, fcRecord(root)); got != before {
		t.Errorf("the refused addition changed the record:\n%s", got)
	}
}

// fcLockTaker is a lock clock that lets another command take the project's lock
// the first time it is asked the time once the check has run, which is when the
// freeze comes back for the lock it let go. It then runs out at its second look,
// as lwClock does, so that this costs no time on the wall.
type fcLockTaker struct {
	*lwClock
	t            *testing.T
	root, marker string
	once         sync.Once
}

func (c *fcLockTaker) Now() time.Time {
	if wtExists(c.marker) {
		c.once.Do(func() {
			g, err := store.Lock(c.root, "a command that came in while the check ran")
			if err != nil {
				c.t.Errorf("another command could not take the lock: %v", err)
				return
			}
			c.t.Cleanup(g.Release)
		})
	}
	return c.lwClock.Now()
}

// Decision 3 and the analysis's first fact: taking the lock back can fail like
// any other wait for it. Nothing is frozen, and the deferred release of the lock
// that was let go does not take the other command's.
func TestAC1Decision3ALockThatCannotBeTakenBackRefusesTheFreezeAndFreezesNothing(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	marker := filepath.Join(wtTempDir(t), "ran")
	fcSet(t, root, `: > `+suSh(marker))
	clock := &fcLockTaker{lwClock: newLWClock(), t: t, root: root, marker: marker}

	var out, errb strings.Builder
	code := executeAt(commandEnv{dir: root, getenv: os.Getenv, lockClock: clock},
		[]string{"freeze"}, strings.NewReader(""), &out, &errb)

	if !wtExists(marker) {
		t.Fatalf("the check did not run: exit %d\n%s", code, errb.String())
	}
	if code == 0 || !strings.Contains(errb.String(), suStateUnwritable) {
		t.Errorf("the freeze did not refuse when the lock could not be taken back, on the run's own clock: exit %d\n%s%s",
			code, out.String(), errb.String())
	}
	fcUnfrozen(t, root)
	if got := fcFreezes(t, fcRecord(root)); len(got) != 0 {
		t.Errorf("the record says it was frozen: %q", got)
	}
}

// A check that did not pass has nothing left to write, so the refusal needs no
// lock, and the landing's commands return before they take theirs back. Taking it
// here is what Ctrl-C ran into: the check killed, the lock taken as the handler
// exited having found nothing held, and every other write refused with SDLC-E0006
// until the lock went stale. The race itself shows only some of the time, so this
// holds the order: another command has the lock by the time the check has ended,
// and a freeze that asks for it reports that in place of what the check said.
func TestAC2ACheckThatDidNotPassTakesNoLockBackBeforeItsRefusal(t *testing.T) {
	t.Parallel()
	for name, fails := range map[string]string{
		"AC-2 it exits with a failure": "exit 1",
		"AC-2 it is killed":            "kill -9 $$",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("a process does not end by a signal here")
			}
			root := frozenStory(t)
			marker := filepath.Join(wtTempDir(t), "ran")
			fcSet(t, root, `: > `+suSh(marker)+`; echo the-check-said-this; `+fails)
			clock := &fcLockTaker{lwClock: newLWClock(), t: t, root: root, marker: marker}
			before := wtRead(t, fcRecord(root))

			var out, errb strings.Builder
			code := executeAt(commandEnv{dir: root, getenv: os.Getenv, lockClock: clock},
				[]string{"freeze"}, strings.NewReader(""), &out, &errb)

			if !wtExists(marker) {
				t.Fatalf("the check did not run: exit %d\n%s", code, errb.String())
			}
			fcRefused(t, result{stdout: out.String(), stderr: errb.String(), code: code}, root, before)
			if strings.Contains(errb.String(), suStateUnwritable) {
				t.Errorf("the freeze waited for the lock before it refused:\n%s", errb.String())
			}
			if !strings.Contains(errb.String(), "the-check-said-this") {
				t.Errorf("the refusal does not show what the check printed:\n%s", errb.String())
			}
		})
	}
}

// ---------------------------------------------------------------------- AC-3

func TestAC3WithTheKeyAbsentEmptyOrBlankTheFreezePrintsAndRecordsWhatItDoesToday(t *testing.T) {
	t.Parallel()
	for name, commands := range map[string]map[string]any{
		"AC-3 absent":  {},
		"AC-3 empty":   {"freeze_check": ""},
		"AC-3 a blank": {"freeze_check": " \t "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := frozenStory(t)
			setConfigOnDisk(t, root, "commands", commands)
			setConfigOnDisk(t, root, "freeze", map[string]any{"allow_new_test_files": true})

			r := run(t, "freeze")
			if r.code != 0 || r.stderr != "" || r.stdout != "US-001  1 test file frozen\n  internal/invoice_test.go\n" {
				t.Errorf("exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
			}
			writeFile(t, root, "internal/later_test.go", fcTest)
			r = run(t, "freeze")
			if r.code != 0 || r.stderr != "" || r.stdout != "US-001  1 test file added to the freeze\n  internal/later_test.go\n" {
				t.Errorf("exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
			}
			want := []string{"1 test file frozen", "1 test file added to the freeze"}
			if got := fcFreezes(t, fcRecord(root)); !slices.Equal(got, want) {
				t.Errorf("the record says %q, want %q", got, want)
			}

			writeFile(t, root, "internal/third_test.go", fcTest)
			j := run(t, "freeze", "--json")
			const added = `{"ok":true,"story":"US-001","files":["internal/invoice_test.go","internal/later_test.go","internal/third_test.go"],"count":3,"added":["internal/third_test.go"]}` + "\n"
			if j.code != 0 || j.stdout != added {
				t.Errorf("exit %d\nstdout %q\nwant   %q", j.code, j.stdout, added)
			}
		})
	}
}

// The freeze holds a name it could not hand to a check, as it always has: the
// rule that refuses it belongs to the check, and there is none here.
func TestAC3WithNoCheckATestFileWhoseNameHoldsANewlineIsFrozenAsItIsToday(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow a newline in a file name")
	}
	root := frozenStory(t)
	writeFile(t, root, "bad\nname_test.go", fcTest)
	mustRun(t, "freeze")
	if _, held := lockOnDisk(t, root).Files["bad\nname_test.go"]; !held {
		t.Error("the freeze does not hold the file")
	}
}

// ---------------------------------------------------------- AC-4, decision 7

func TestAC4APassIsRecordedInTheFreezeEventAndNothingElseChanges(t *testing.T) {
	t.Parallel()
	const hidden = "words-only-the-command-holds"
	t.Run("AC-4 in prose", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		// What it prints goes nowhere near stdout, which a skill parses as data.
		fcSet(t, root, `: `+hidden+`; echo printed on stdout; echo printed on stderr >&2`)
		r := mustRun(t, "freeze")
		if r.stdout != "US-001  1 test file frozen\n  internal/invoice_test.go\n" {
			t.Errorf("stdout = %q", r.stdout)
		}
		got := fcFreezes(t, fcRecord(root))
		if len(got) != 1 {
			t.Fatalf("the record has %d freeze events: %q", len(got), got)
		}
		for _, want := range []string{"1 test file frozen", fcKey, "passed"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("the freeze event does not say %q: %s", want, got[0])
			}
		}
		if strings.Contains(wtRead(t, fcRecord(root)), hidden) {
			t.Error("the record holds the command's text, which can hold a token")
		}
	})
	t.Run("AC-4 in JSON", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports()+`; echo printed on stdout`)
		r := mustRun(t, "freeze", "--json")
		const want = `{"ok":true,"story":"US-001","files":["internal/invoice_test.go"],"count":1}` + "\n"
		if r.stdout != want {
			t.Errorf("stdout = %q, want %q", r.stdout, want)
		}
		if spy.times(t) != 1 {
			t.Errorf("the check ran %d times, want once", spy.times(t))
		}
	})
	t.Run("AC-4 the event still reads as a standing freeze", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports())
		mustRun(t, "freeze")
		if spy.times(t) != 1 {
			t.Errorf("the check ran %d times, want once", spy.times(t))
		}
		if err := os.Remove(fcLockPath(root)); err != nil {
			t.Fatal(err)
		}
		if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0025") {
			t.Errorf("a freeze recorded with a check is not recognised when it goes missing: exit %d\n%s", r.code, r.stderr)
		}
	})
}

func TestAC4TheCheckRunsAgainOnAFreezeAfterAnUnfreeze(t *testing.T) {
	t.Parallel()
	root := fcStarted(t, map[string]string{"old_test.go": fcTest})
	writeFile(t, root, "new_test.go", fcTest)
	spy := newFCSpy(t)
	fcSet(t, root, spy.reports())
	mustRun(t, "freeze")
	if spy.times(t) != 1 {
		t.Fatalf("the check ran %d times at the first freeze", spy.times(t))
	}

	mustRun(t, "unfreeze", "--reason", "AC-1's test asserted the wrong thing")
	writeFile(t, root, "old_test.go", fcTwo)
	mustRun(t, "freeze")
	if spy.times(t) != 2 {
		t.Errorf("the check ran %d times by the second freeze, want 2", spy.times(t))
	}
	spy.wantListed(t, "new_test.go", "old_test.go")
	got := fcFreezes(t, fcRecord(root))
	if len(got) != 2 || !strings.Contains(got[1], fcKey) || !strings.Contains(got[1], "passed") {
		t.Errorf("the second freeze's event does not say the check passed: %q", got)
	}

	mustRun(t, "unfreeze", "--reason", "and again")
	before := wtRead(t, fcRecord(root))
	fcSet(t, root, fcFailing)
	fcRefused(t, run(t, "freeze"), root, before)
}

func TestAC4Decision10TheCheckRunsOnTheAdditionsWithTheSameListAsAnyFreeze(t *testing.T) {
	t.Parallel()
	additions := func(t *testing.T) (string, fcSpy) {
		t.Helper()
		root := fcStarted(t, map[string]string{"old_test.go": fcTest})
		writeFile(t, root, "first_test.go", fcTest)
		setConfigOnDisk(t, root, "freeze", map[string]any{"allow_new_test_files": true})
		spy := newFCSpy(t)
		fcSet(t, root, spy.reports())
		mustRun(t, "freeze")
		return root, spy
	}
	t.Run("AC-4 a pass adds the file and says so on the record", func(t *testing.T) {
		t.Parallel()
		root, spy := additions(t)
		writeFile(t, root, "later_test.go", fcTest)
		got := decode[freezePayload](t, mustRun(t, "freeze", "--json"))
		if len(got.Added) != 1 || got.Added[0] != "later_test.go" || got.Count != 3 {
			t.Errorf("freeze = %+v", got)
		}
		if spy.times(t) != 2 {
			t.Errorf("the check ran %d times, want 2", spy.times(t))
		}
		// The whole list, not only what is being added.
		spy.wantListed(t, "first_test.go", "later_test.go")
		events := fcFreezes(t, fcRecord(root))
		if last := events[len(events)-1]; !strings.Contains(last, "1 test file added to the freeze") ||
			!strings.Contains(last, fcKey) || !strings.Contains(last, "passed") {
			t.Errorf("the event for the addition is %q", last)
		}
	})
	t.Run("AC-4 a failure adds nothing", func(t *testing.T) {
		t.Parallel()
		root, _ := additions(t)
		held := wtRead(t, fcLockPath(root))
		before := wtRead(t, fcRecord(root))
		writeFile(t, root, "later_test.go", "package internal\n\n// LINT-ERROR\n")
		fcSet(t, root, `echo no; exit 1`)
		r := run(t, "freeze")
		if r.code != 1 || !strings.Contains(r.stderr, fcCode) || r.stdout != "" {
			t.Errorf("exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
		}
		if wtRead(t, fcLockPath(root)) != held {
			t.Error("the refused addition changed the freeze")
		}
		if wtRead(t, fcRecord(root)) != before {
			t.Error("the refused addition changed the record")
		}
	})
	t.Run("AC-4 a check that changes a frozen file adds nothing", func(t *testing.T) {
		t.Parallel()
		root, _ := additions(t)
		held := wtRead(t, fcLockPath(root))
		writeFile(t, root, "later_test.go", fcTest)
		fcSet(t, root, `printf '// formatted\n' >> first_test.go`)
		r := run(t, "freeze")
		if r.code != 1 || !strings.Contains(r.stderr, fcCode) || !strings.Contains(r.stderr, "first_test.go") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		if wtRead(t, fcLockPath(root)) != held {
			t.Error("the refused addition changed the freeze")
		}
	})
	t.Run("AC-4 a check that writes another test adds nothing", func(t *testing.T) {
		t.Parallel()
		root, _ := additions(t)
		held := wtRead(t, fcLockPath(root))
		writeFile(t, root, "later_test.go", fcTest)
		fcSet(t, root, `printf 'package x\n' > generated_test.go`)
		r := run(t, "freeze")
		if r.code != 1 || !strings.Contains(r.stderr, fcCode) || !strings.Contains(r.stderr, "generated_test.go") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		if wtRead(t, fcLockPath(root)) != held {
			t.Error("the refused addition changed the freeze")
		}
	})
	t.Run("AC-4 with nothing to add it is the second freeze it always was", func(t *testing.T) {
		t.Parallel()
		root, _ := additions(t)
		before := wtRead(t, fcRecord(root))
		fcSet(t, root, fcFailing)
		if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0022") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		if wtRead(t, fcRecord(root)) != before {
			t.Error("a refused second freeze changed the record")
		}
	})
	t.Run("AC-4 a frozen test that changed is refused as it always was", func(t *testing.T) {
		t.Parallel()
		root, _ := additions(t)
		writeFile(t, root, "first_test.go", fcTwo)
		writeFile(t, root, "later_test.go", fcTest)
		fcSet(t, root, fcFailing)
		if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0025") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
	})
	t.Run("AC-4 a freeze lifted while the check ran is not put back", func(t *testing.T) {
		t.Parallel()
		root, _ := additions(t)
		writeFile(t, root, "later_test.go", fcTest)
		fcSet(t, root, `rm -f .sdlc/state/tests.lock`)
		r := run(t, "freeze")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		fcUnfrozen(t, root)
	})
	t.Run("AC-4 a test frozen meanwhile by another freeze is not added twice", func(t *testing.T) {
		t.Parallel()
		root, _ := additions(t)
		writeFile(t, root, "later_test.go", fcTest)
		held := wtRead(t, fcLockPath(root))
		// What another freeze would leave: the same story, holding the new file.
		other := strings.Replace(held, `"files": {`, `"files": {`+"\n"+`    "later_test.go": "`+strings.Repeat("0", 64)+`",`, 1)
		spare := filepath.Join(wtTempDir(t), "lock")
		if err := os.WriteFile(spare, []byte(other), 0o600); err != nil {
			t.Fatal(err)
		}
		fcSet(t, root, `cp `+suSh(spare)+` .sdlc/state/tests.lock`)
		r := run(t, "freeze")
		if r.code == 0 {
			t.Errorf("the addition went ahead over a freeze that moved under it:\n%s", r.stdout)
		}
		if got := wtRead(t, fcLockPath(root)); got != other {
			t.Errorf("the freeze another command left was written over:\n%s", got)
		}
	})
}

// ---------------------------------------------------------- AC-5, decision 4

// fcDoctorLine is doctor's line about the check.
func fcDoctorLine(t *testing.T, p doctorPayload) check {
	t.Helper()
	for _, c := range p.Checks {
		if c.Name == fcDoctor {
			return c
		}
	}
	t.Fatalf("doctor has no %q line: %+v", fcDoctor, p.Checks)
	return check{}
}

func TestAC5Decision4DoctorSaysAFreezeCheckIsNotConfiguredWithoutCallingItAProblem(t *testing.T) {
	t.Parallel()
	for name, commands := range map[string]map[string]string{
		"AC-5 absent":             {"test": "exit 0"},
		"AC-5 empty":              {"test": "exit 0", "freeze_check": ""},
		"AC-5 a blank":            {"test": "exit 0", "freeze_check": "  "},
		"AC-5 no commands at all": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configuredWith(t, commands)
			c := fcDoctorLine(t, decode[doctorPayload](t, run(t, "doctor", "--json")))
			if c.State != stateOK || c.Detail != fcNotSet || c.Fix != "" {
				t.Errorf("doctor's line is %+v, want state ok, detail %q and no fix", c, fcNotSet)
			}
			if prose := run(t, "doctor").stdout; !strings.Contains(prose, fcDoctor) || !strings.Contains(prose, fcNotSet) {
				t.Errorf("doctor does not say so in prose:\n%s", prose)
			}
		})
	}
}

func TestAC5Decision11DoctorSaysAFreezeCheckIsConfiguredWithoutPrintingIt(t *testing.T) {
	t.Parallel()
	const hidden = "words-only-the-command-holds"
	configuredWith(t, map[string]string{"test": "exit 0", "freeze_check": ": " + hidden + "; exit 0"})
	r := run(t, "doctor", "--json")
	c := fcDoctorLine(t, decode[doctorPayload](t, r))
	if c.State != stateOK || c.Fix != "" {
		t.Errorf("doctor's line is %+v, want state ok and no fix", c)
	}
	if !strings.Contains(c.Detail, fcKey) || !strings.Contains(c.Detail, "is set") || strings.Contains(c.Detail, "not set") {
		t.Errorf("doctor does not say the check is configured: %s", c.Detail)
	}
	for _, text := range []string{r.stdout, run(t, "doctor").stdout} {
		if strings.Contains(text, hidden) {
			t.Errorf("doctor prints the command's text, which can hold a token:\n%s", text)
		}
	}
}

func TestAC5DoctorSaysWhereTheHookWouldRefuseTheFreezeCheckAsItDoesForTheOthers(t *testing.T) {
	t.Parallel()
	const command = "rm -f .sdlc/state/cover.out"
	rule, refused := refusedByTheHook(command)
	if !refused {
		t.Fatalf("the classifier allows %q, so this says nothing", command)
	}
	configuredWith(t, map[string]string{"test": "exit 0", "freeze_check": command})
	p := decode[doctorPayload](t, run(t, "doctor", "--json"))
	found := aboutRule(p, rule)
	if len(found) != 1 || !strings.Contains(said(found[0]), fcKey) {
		t.Errorf("doctor does not report commands.freeze_check under %s: %+v", rule, p.Checks)
	}
	if c := fcDoctorLine(t, p); c.State != stateOK {
		t.Errorf("the line that says it is configured is %+v; the finding is the problem, not the line", c)
	}
}

func TestAC5DoctorSaysWhereAProgramTheFreezeCheckNamesIsNotInstalled(t *testing.T) {
	t.Parallel()
	configuredWith(t, map[string]string{"test": "exit 0", "freeze_check": "no-such-program-for-the-check --all"})
	p := decode[doctorPayload](t, run(t, "doctor", "--json"))
	if c := fcDoctorLine(t, p); c.State != stateOK || !strings.Contains(c.Detail, "is set") {
		t.Errorf("a check whose program is missing is still configured: %+v", c)
	}
	for _, c := range p.Checks {
		if c.State == stateProblem && strings.Contains(said(c), "no-such-program-for-the-check") {
			if !strings.Contains(said(c), "freeze_check") {
				t.Errorf("the finding does not name the command: %+v", c)
			}
			return
		}
	}
	t.Errorf("doctor does not report the missing program: %+v", p.Checks)
}

// checkOrder is what the report walks when it has to stop early, so that nothing
// appears to have silently passed.
func TestAC5DoctorReportsTheFreezeCheckLineAsSkippedWhereTheConfigurationDoesNotRead(t *testing.T) {
	t.Parallel()
	root := project(t)
	mustRun(t, "init")
	writeFile(t, root, ".sdlc/config.json", "{ this is not JSON\n")
	c := fcDoctorLine(t, decode[doctorPayload](t, run(t, "doctor", "--json")))
	if c.State != stateSkipped {
		t.Errorf("doctor's line is %+v, want it skipped", c)
	}
}

// fcNotInstalled is the programs doctor says it cannot find.
func fcNotInstalled(p doctorPayload) []string {
	var programs []string
	for _, c := range p.Checks {
		if name, _, found := strings.Cut(c.Detail, " is not installed"); found && c.Name == "commands on PATH" {
			programs = append(programs, name)
		}
	}
	slices.Sort(programs)
	return programs
}

// A check that opens with a loop is the shape the pages teach, and `for` is no
// program to install. docs/commands.md says doctor looks up the first word of each
// command after an operator, so what follows `;`, `|` or `&&` inside the loop is
// still looked up. It says doctor can miss what follows `do`, and this does not
// hold it to missing that: finding one more program would be no fault.
func TestAC5DoctorReadsACheckThatOpensWithALoopAsTheCommandsPageSays(t *testing.T) {
	t.Parallel()
	const missing = "no-such-program-for-the-check"
	for name, c := range map[string]struct {
		command string
		want    []string
	}{
		"AC-5 for, with nothing to find": {
			`for f in $SDLC_FREEZE_FILES; do if grep -q NOT-READY "$f"; then echo "$f"; exit 1; fi; done`, nil,
		},
		"AC-5 while, with nothing to find": {
			`printf '%s\n' "$SDLC_FREEZE_FILES" | while IFS= read -r f; do [ -n "$f" ] || exit 1; done`, nil,
		},
		"AC-5 until, with nothing to find": {`until true; do exit 1; done`, nil},
		"AC-5 if, with nothing to find":    {`if [ -n "$SDLC_FREEZE_FILES" ]; then true; elif false; then exit 1; else exit 0; fi`, nil},
		"AC-5 what follows a semicolon in the loop": {
			`for f in $SDLC_FREEZE_FILES; do true; ` + missing + ` "$f"; done`, []string{missing},
		},
		"AC-5 what follows a pipe in the loop": {
			`for f in $SDLC_FREEZE_FILES; do echo "$f" | ` + missing + `; done`, []string{missing},
		},
		"AC-5 what follows an and in the loop": {
			`for f in $SDLC_FREEZE_FILES; do true && ` + missing + ` "$f"; done`, []string{missing},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configuredWith(t, map[string]string{"freeze_check": c.command})
			p := decode[doctorPayload](t, run(t, "doctor", "--json"))
			if got := fcNotInstalled(p); !slices.Equal(got, c.want) {
				t.Errorf("doctor says %q are not installed, want %q: %+v", got, c.want, p.Checks)
			}
			if line := fcDoctorLine(t, p); line.State != stateOK || !strings.Contains(line.Detail, "is set") {
				t.Errorf("doctor's line is %+v", line)
			}
		})
	}
}

// The shell runs these itself, and POSIX does not require any of them to be on
// PATH, so a command using one is no program to install. Doctor said `continue is
// not installed`, a problem and exit 1, of a loop that works; the example on the
// configuration page has that very line. Each is read here as a first word in the
// three places doctor's scanner starts one: opening the command, after `;` in a
// loop, and after `||`.
func TestAC5DoctorReportsNoSpecialBuiltinAsAProgramToInstall(t *testing.T) {
	t.Parallel()
	shape := func(use string) string {
		return use + `; for f in x; do true; ` + use + `; true || ` + use + `; done`
	}
	configuredWith(t, map[string]string{"freeze_check": shape("true")})
	without := run(t, "doctor", "--json")
	if got := fcNotInstalled(decode[doctorPayload](t, without)); len(got) != 0 {
		t.Fatalf("the same loop without them has %q not installed", got)
	}
	for word, use := range map[string]string{
		"break":    "break",
		"continue": "continue",
		":":        ": nothing",
		".":        ". ./lib.sh",
		"eval":     `eval "$SDLC_FREEZE_FILES"`,
		"exec":     "exec true",
		"exit":     "exit 0",
		"export":   "export A=1",
		"readonly": "readonly A=1",
		"return":   "return 0",
		"set":      "set -e",
		"shift":    "shift",
		"times":    "times",
		"trap":     "trap : EXIT",
		"unset":    "unset A",
	} {
		t.Run("AC-5 "+word, func(t *testing.T) {
			t.Parallel()
			command := shape(use)
			configuredWith(t, map[string]string{"freeze_check": command})
			r := run(t, "doctor", "--json")
			p := decode[doctorPayload](t, r)
			if got := fcNotInstalled(p); len(got) != 0 {
				t.Errorf("doctor says %q are not installed, of a command the shell runs itself", got)
			}
			if r.code != without.code {
				t.Errorf("doctor exits %d, and %d for the same loop without it: %+v", r.code, without.code, p.Checks)
			}
		})
	}
}

// ------------------------------------------- what the first look could not fail

// The first pass asked before the lock was let go, and a person can move the
// story to another session while the check runs. A freeze taken on that answer
// goes into a story this session no longer holds, which was the plan's blocking
// security finding. What the check writes is what `sdlc start` from another
// session would.
func TestAC1Decision3TheSessionIsAskedAgainAfterTheCheckWhereTheStoryMovedToAnother(t *testing.T) {
	t.Parallel()
	const elsewhere = "SDLC-E0048"
	const session = ".sdlc/state/session"
	started := func(t *testing.T) string {
		t.Helper()
		main := wtRepository(t, wtStory{id: "US-001"})
		wtStarted(t, main, wtSessionMain, "US-001")
		writeFile(t, main, "new_test.go", fcTest)
		if got := strings.TrimSpace(wtRead(t, filepath.Join(main, filepath.FromSlash(session)))); got != wtSessionMain {
			t.Fatalf("the story is held by %q, want %q", got, wtSessionMain)
		}
		return main
	}
	t.Run("AC-1 another session took the story", func(t *testing.T) {
		t.Parallel()
		main := started(t)
		fcSet(t, main, `printf '`+wtSessionThird+`\n' > `+session)
		before := wtRead(t, fcRecord(main))
		r := wtRun(t, main, wtSessionMain, "", "freeze")
		if r.code != 1 || !strings.Contains(r.stderr, elsewhere) {
			t.Errorf("exit %d, want the refusal %s:\n%s%s", r.code, elsewhere, r.stdout, r.stderr)
		}
		fcUnfrozen(t, main)
		if got := fcFreezes(t, fcRecord(main)); len(got) != 0 {
			t.Errorf("the record says it was frozen: %q", got)
		}
		if got := wtRead(t, fcRecord(main)); got != before {
			t.Errorf("the refused freeze changed the record:\n%s\nwas\n%s", got, before)
		}
	})
	t.Run("AC-1 the same session still holds it", func(t *testing.T) {
		t.Parallel()
		main := started(t)
		fcSet(t, main, `printf '`+wtSessionMain+`\n' > `+session)
		wtMust(t, main, wtSessionMain, "", "freeze")
		if lock := lockOnDisk(t, main); lock.Story != "US-001" {
			t.Errorf("the freeze is %q's", lock.Story)
		}
	})
}

// A check that runs `sdlc freeze` would start the check again, with the lock let
// go and no time limit, to any depth. Not parallel: the guard reads the process's
// environment, which only a test Go runs by itself may change.
func TestAC1Decision3AFreezeWithTheCheckVariableAlreadySetRefusesAndFreezesNothing(t *testing.T) {
	for name, value := range map[string]string{
		"AC-1 set to a list":      "internal/invoice_test.go",
		"AC-1 exported and empty": "",
	} {
		t.Run(name, func(t *testing.T) {
			root := frozenStory(t)
			spy := newFCSpy(t)
			fcSet(t, root, spy.reports())
			before := wtRead(t, fcRecord(root))
			t.Setenv(fcFilesVariable, value)

			r := run(t, "freeze")
			fcRefused(t, r, root, before)
			if spy.times(t) != 0 {
				t.Errorf("the check ran %d times inside a freeze", spy.times(t))
			}
			flat := fcFlat(r.stderr)
			for _, want := range []string{fcKey, strings.ToLower(fcFilesVariable), "unset", "sdlc freeze"} {
				if !strings.Contains(flat, want) {
					t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
				}
			}
			if got := decode[errorPayload](t, run(t, "freeze", "--json")); got.Code != fcCode {
				t.Errorf("the refusal in JSON is %+v", got)
			}
			fcUnfrozen(t, root)
		})
	}
	t.Run("AC-3 with no check the variable means nothing", func(t *testing.T) {
		root := frozenStory(t)
		t.Setenv(fcFilesVariable, "internal/invoice_test.go")
		mustRun(t, "freeze")
		if !wtExists(fcLockPath(root)) {
			t.Error("no freeze was taken")
		}
	})
}

// Decision 11 and THREATS 8: a shell quotes a command it cannot parse back in its
// own error, and the command's text can hold a token. Which shell quotes it, and
// how, differs, so the first case prints the command's words from a file on every
// shell, and the second is whatever the shell here does with a real syntax error.
func TestAC2Decision11TheCommandsTextIsReplacedByItsKeyWhereTheShellQuotesItBack(t *testing.T) {
	t.Parallel()
	const hidden = "words-only-the-command-holds"
	said := func(t *testing.T, args ...string) string {
		t.Helper()
		r := run(t, args...)
		if r.code != 1 || !strings.Contains(r.stdout+r.stderr, fcCode) {
			t.Fatalf("exit %d:\n%s%s", r.code, r.stdout, r.stderr)
		}
		return r.stdout + r.stderr
	}
	t.Run("AC-2 what the check prints holds the whole command", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		printed := filepath.Join(wtTempDir(t), "printed")
		command := `: ` + hidden + `; cat ` + suSh(printed) + `; exit 1`
		if err := os.WriteFile(printed, []byte("sh: syntax error in `"+command+"' here\nand its own words\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fcSet(t, root, command)
		for _, args := range [][]string{{"freeze"}, {"freeze", "--json"}} {
			text := said(t, args...)
			if strings.Contains(text, hidden) {
				t.Errorf("sdlc %v prints the command's text:\n%s", args, text)
			}
			for _, want := range []string{"sh: syntax error in `" + fcKey + "' here", "and its own words"} {
				if !strings.Contains(text, want) {
					t.Errorf("sdlc %v does not show %q:\n%s", args, want, text)
				}
			}
		}
	})
	t.Run("AC-2 a command the shell cannot parse", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		fcSet(t, root, `echo `+hidden+` )`)
		for _, args := range [][]string{{"freeze"}, {"freeze", "--json"}} {
			if text := said(t, args...); strings.Contains(text, hidden) {
				t.Errorf("sdlc %v prints the command's text:\n%s", args, text)
			}
		}
	})
}

// A one-word command is the name of a program, and it holds no token to protect.
// Replaced, a missing program would not be named, and a file whose name contains
// the word would be shown under a name that does not exist.
func TestAC2Decision11AOneWordCommandIsLeftAloneInWhatTheCheckPrinted(t *testing.T) {
	t.Parallel()
	t.Run("AC-2 a program that is not installed is still named", func(t *testing.T) {
		t.Parallel()
		root := frozenStory(t)
		const missing = "no-such-program-for-the-check"
		fcSet(t, root, missing)
		r := run(t, "freeze")
		if !strings.Contains(r.stderr, fcCode) {
			t.Fatalf("exit %d:\n%s", r.code, r.stderr)
		}
		_, printed, found := strings.Cut(r.stderr, "printed (last 40 lines)")
		if !found || !strings.Contains(printed, missing) {
			t.Errorf("what the shell said of the missing program is gone:\n%s", r.stderr)
		}
	})
	t.Run("AC-2 a file whose name contains the word keeps its name", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("the check is a script run by its mode, which Windows does not have")
		}
		root := frozenStory(t)
		writeFile(t, root, "fclint", "#!/bin/sh\necho 'lint/./fclint.go is not formatted'\nexit 1\n")
		if err := os.Chmod(filepath.Join(root, "fclint"), 0o755); err != nil {
			t.Fatal(err)
		}
		fcSet(t, root, "./fclint")
		r := run(t, "freeze")
		if !strings.Contains(r.stderr, "lint/./fclint.go is not formatted") {
			t.Errorf("exit %d, the file is not shown under its name:\n%s", r.code, r.stderr)
		}
	})
}

// fcGroupPatience bounds how long a test waits for a pipe to close before it says
// the process that holds it was not stopped. It is a deadline, not a pause, as
// grPatience is in internal/shellx: nothing waits for it when the group is gone.
const fcGroupPatience = 20 * time.Second

// fcHeld is a child of the check that holds a pipe of the test's open for as long
// as it lives, so that its end is seen without a clock.
type fcHeld struct {
	words string        // what the check runs to start it in the background
	said  chan string   // its process id, or "" where the pipe could not be read
	gone  chan struct{} // closed when nothing holds the pipe any longer
}

func newFCHeld(t *testing.T) fcHeld {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the child here is a POSIX background job on a named pipe, and internal/shellx holds the job object on Windows")
	}
	pipe := filepath.Join(wtTempDir(t), "held")
	if out, err := exec.CommandContext(t.Context(), "mkfifo", pipe).CombinedOutput(); err != nil {
		t.Skipf("no named pipe can be made here: %v\n%s", err, out)
	}
	h := fcHeld{
		words: `sh -c 'echo $$; exec tail -f /dev/null' > ` + suSh(pipe) + ` 2>&1 &`,
		said:  make(chan string, 1),
		gone:  make(chan struct{}),
	}
	go func() {
		defer close(h.gone)
		f, err := os.Open(pipe)
		if err != nil {
			h.said <- ""
			return
		}
		defer func() { _ = f.Close() }()
		r := bufio.NewReader(f)
		line, err := r.ReadString('\n')
		if err != nil {
			h.said <- ""
			return
		}
		h.said <- strings.TrimSpace(line)
		_, _ = io.Copy(io.Discard, r)
	}()
	return h
}

// up waits for the child to say its process id, and has it killed at the end of
// the test whatever became of the stop. ended is closed if the freeze has ended
// without it.
func (h fcHeld) up(t *testing.T, ended <-chan struct{}) {
	t.Helper()
	select {
	case line := <-h.said:
		pid, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("the check's child did not say its process id: %q", line)
		}
		t.Cleanup(func() {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		})
	case <-ended:
		t.Fatal("the freeze ended before the check's child had started")
	case <-time.After(fcGroupPatience):
		t.Fatal("the check's child did not start")
	}
}

// The docs promise that a stop ends the check and everything in its process
// group. Without a group a stop kills the shell alone, and a linter's workers go
// on writing to the tree. This holds the group by what it is for, and not only
// that it was asked for.
//
// Not parallel: StopAll stops every command this process has running.
func TestAC1Decision3AStopEndsTheCheckAndEverythingItStartedAndFreezesNothing(t *testing.T) {
	root := frozenStory(t)
	held := newFCHeld(t)
	fcSet(t, root, held.words+` wait`)
	before := wtRead(t, fcRecord(root))

	ended := make(chan struct{})
	var r result
	go func() {
		defer close(ended)
		r = run(t, "freeze")
	}()
	held.up(t, ended)

	shellx.StopAll()
	select {
	case <-held.gone:
	case <-time.After(fcGroupPatience):
		t.Error("after a stop, a process the check started is still running")
	}
	select {
	case <-ended:
	case <-time.After(fcGroupPatience):
		t.Fatal("the freeze did not end after a stop")
	}
	fcRefused(t, r, root, before)
}

// The group itself, which a stop relies on and which needs no stop to see: the
// shell the check runs in leads a group, and so is not in the group of whatever
// started sdlc, where a terminal's Ctrl-C would reach it behind sdlc's back.
func TestAC1Decision3TheCheckRunsInAProcessGroupOfItsOwn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a command runs in a job object on Windows, which internal/shellx holds")
	}
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skipf("no ps to ask: %v", err)
	}
	root := frozenStory(t)
	report := filepath.Join(wtTempDir(t), "group")
	fcSet(t, root, `{ echo "$$"; ps -o pgid= -p "$$"; } > `+suSh(report))
	mustRun(t, "freeze")
	fields := strings.Fields(wtRead(t, report))
	if len(fields) != 2 {
		t.Fatalf("the check reported %q", fields)
	}
	if fields[0] != fields[1] {
		t.Errorf("the check is process %s in group %s: it does not lead a group of its own", fields[0], fields[1])
	}
}
