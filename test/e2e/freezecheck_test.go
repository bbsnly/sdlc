// US-112.2, commands.freeze_check put to the built binary in a sandbox: a failing
// check refuses the freeze, a passing one takes it, and the check leaves the tree
// as it found it. internal/cli holds the rest of the story without a build; what
// is here is what only separate processes can show: the lock let go around the
// check, and what a command the check starts is allowed to do.

package e2e

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

const (
	fcCode = "SDLC-E0085"
	fcKey  = "commands.freeze_check"
)

// fcAtTheTests is a sandbox with command as its freeze check, committed on trunk
// as a person's edit is, and its story at Gate 3 in its own worktree.
func fcAtTheTests(t *testing.T, command string) sandbox {
	t.Helper()
	sb := newSandbox(t)
	setConfig(t, sb.repo, "commands", "freeze_check", command)
	sb.mustGit(t, sb.repo, "add", "--all")
	sb.mustGit(t, sb.repo, "commit", "--quiet", "--message", "chore: choose a freeze check")
	sb.works(t, sb.repo, "", "", "start")
	sb.worksGate(t, model.GateDoR)
	sb.worksGate(t, model.GateAnalysis)
	sb.works(t, sb.tree(), "sdet", sandboxTestPlan, "artifact", "write", "test_plan")
	return sb
}

// fcTreeNow is what git sees of the story's worktree, the loop's own files aside,
// and the content of each of those files.
func (sb sandbox) fcTreeNow(t *testing.T) string {
	t.Helper()
	var seen strings.Builder
	for line := range strings.SplitSeq(sb.mustGit(t, sb.tree(), "status", "--porcelain", "--untracked-files=all"), "\n") {
		path := strings.TrimSpace(line)
		if len(path) > 3 {
			path = path[3:]
		}
		if path == "" || strings.HasPrefix(path, ".sdlc/") {
			continue
		}
		seen.WriteString(line + "\n" + readFile(t, filepath.Join(sb.tree(), filepath.FromSlash(path))) + "\n")
	}
	return seen.String()
}

func (sb sandbox) fcFreezeEvents(t *testing.T) []string {
	t.Helper()
	var record model.Record
	readJSON(t, filepath.Join(sb.repo, ".sdlc", "stories", "US-001", model.RecordFile), &record)
	var messages []string
	for _, e := range record.Events {
		if e.Type == "freeze" {
			messages = append(messages, e.Message)
		}
	}
	return messages
}

// fcShellPath is a path as one word of a POSIX shell command.
func fcShellPath(path string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", `'\''`) + "'"
}

func TestAC6AFailingCheckRefusesTheFreezeAndAPassingOneTakesItInTheSandbox(t *testing.T) {
	parallel(t)
	// It judges the files it is handed by what they hold, so that the second run
	// differs from the first because the test does.
	sb := fcAtTheTests(t, `for f in $SDLC_FREEZE_FILES; do if grep -q NOT-READY "$f"; then echo "$f is not ready"; exit 1; fi; done`)
	tree := sb.tree()
	lock := filepath.Join(tree, ".sdlc", "state", "tests.lock")

	sb.writesFile(t, "sdet", "invoice_test.go", "package invoice\n\n// NOT-READY\n")
	before := sb.fcTreeNow(t)
	r := sb.does(t, tree, "sdet", "", "freeze")
	if r.code != 1 || r.stdout != "" {
		t.Errorf("a failing check: exit %d, stdout %q", r.code, r.stdout)
	}
	for _, want := range []string{fcCode, fcKey, "invoice_test.go is not ready", "sdlc freeze"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
		}
	}
	if exists(lock) {
		t.Error("a failing check left a freeze on disk")
	}
	if got := sb.fcFreezeEvents(t); len(got) != 0 {
		t.Errorf("a failing check left a freeze on the record: %q", got)
	}
	if after := sb.fcTreeNow(t); after != before {
		t.Errorf("the refused freeze changed the tree:\n%s\nwas\n%s", after, before)
	}
	if g := sb.does(t, tree, "", "", "gate", "tests_frozen", "pass", "--note", "not frozen"); g.code == 0 {
		t.Errorf("Gate 3 passed with no freeze:\n%s", g.stdout)
	}

	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)
	before = sb.fcTreeNow(t)
	sb.holds(t,
		ran{dir: tree, agent: "sdet", args: []string{"freeze"}, stdout: "US-001  1 test file frozen\n  invoice_test.go\n"},
		ran{dir: tree, args: []string{"gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test"},
			stdout: passed("tests_frozen", "1 criterion, 1 failing test")},
	)
	if !exists(lock) {
		t.Error("a passing check left no freeze on disk")
	}
	if after := sb.fcTreeNow(t); after != before {
		t.Errorf("the freeze changed the tree:\n%s\nwas\n%s", after, before)
	}
	got := sb.fcFreezeEvents(t)
	if len(got) != 1 || !strings.Contains(got[0], fcKey) || !strings.Contains(got[0], "passed") {
		t.Errorf("the record does not say the check ran and passed: %q", got)
	}
}

// Decision 3: the check can run for longer than anything waits for the project's
// lock, so it runs with the lock let go, as the landing's commands do. Held, the
// command here waits out its ten seconds and fails, and so does the check. And
// the freeze reads the record again afterwards, or it writes the cost away.
func TestAC1Decision3AnotherCommandCanWriteTheLoopsStateWhileTheCheckRuns(t *testing.T) {
	parallel(t)
	listed := filepath.Join(scratch(t), "listed")
	sb := fcAtTheTests(t, `printf '%s' "$SDLC_FREEZE_FILES" > `+fcShellPath(listed)+` && "$SDLC_BIN" cost add --usd 0.25`)
	tree := sb.tree()
	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)
	sb.works(t, tree, "sdet", "", "freeze")

	if got := readFile(t, listed); got != "invoice_test.go" {
		t.Errorf("SDLC_FREEZE_FILES held %q", got)
	}
	if got := sb.works(t, tree, "", "", "cost"); !strings.Contains(got, "$0.25") {
		t.Errorf("what the command wrote during the check is gone: %s", got)
	}
	if got := sb.fcFreezeEvents(t); len(got) != 1 {
		t.Errorf("the record has %d freeze events: %q", len(got), got)
	}
}

// A check that runs `sdlc freeze` is refused from inside, where only separate
// processes can show it: the guard reads the environment the outer freeze handed
// its check. The check counts its runs and gives up at the third, so that a freeze
// with no guard fails this rather than starting itself to any depth.
func TestAC1Decision3ACheckThatRunsSdlcFreezeIsRefusedFromInsideAndFreezesNothing(t *testing.T) {
	parallel(t)
	runs := filepath.Join(scratch(t), "runs")
	count := fcShellPath(runs)
	sb := fcAtTheTests(t, `echo ran >> `+count+`; [ "$(wc -l < `+count+`)" -lt 3 ] || exit 9; "$SDLC_BIN" freeze`)
	tree := sb.tree()
	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)

	r := sb.does(t, tree, "sdet", "", "freeze")
	if r.code != 1 || r.stdout != "" {
		t.Errorf("exit %d, stdout %q", r.code, r.stdout)
	}
	for _, want := range []string{fcCode, "SDLC_FREEZE_FILES"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
		}
	}
	if got := strings.Count(readFile(t, runs), "ran\n"); got != 1 {
		t.Errorf("the check ran %d times, want once", got)
	}
	if exists(filepath.Join(tree, ".sdlc", "state", "tests.lock")) {
		t.Error("a freeze was taken")
	}
	if got := sb.fcFreezeEvents(t); len(got) != 0 {
		t.Errorf("the record says it was frozen: %q", got)
	}
}

// Ctrl-C at the built binary, where the signal handler is: the docs say it stops
// the check and everything in its process group. The child holds a named pipe of
// the test's open for as long as it lives, so that its end is seen without a clock;
// the guard is a deadline, not a pause.
func TestAC1Decision3CtrlCStopsTheCheckAndEverythingItStartedAndFreezesNothing(t *testing.T) {
	parallel(t)
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot be sent to a process on Windows, and the child is a POSIX background job")
	}
	pipe := filepath.Join(scratch(t), "held")
	if out, err := exec.CommandContext(t.Context(), "mkfifo", pipe).CombinedOutput(); err != nil {
		t.Skipf("no named pipe can be made here: %v\n%s", err, out)
	}
	sb := fcAtTheTests(t, `sh -c 'echo $$; exec tail -f /dev/null' > `+fcShellPath(pipe)+` 2>&1 & wait`)
	tree := sb.tree()
	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)

	said := make(chan string, 1)
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		f, err := os.Open(pipe)
		if err != nil {
			said <- ""
			return
		}
		defer func() { _ = f.Close() }()
		r := bufio.NewReader(f)
		line, err := r.ReadString('\n')
		if err != nil {
			said <- ""
			return
		}
		said <- strings.TrimSpace(line)
		_, _ = io.Copy(io.Discard, r)
	}()

	cmd := exec.CommandContext(t.Context(), sb.sdlc, "freeze")
	cmd.Dir = tree
	cmd.Env = sb.env(t, sandboxSession)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	guard := time.After(time.Minute)
	select {
	case line := <-said:
		pid, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("the check's child did not say its process id: %q", line)
		}
		t.Cleanup(func() {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		})
	case err := <-waited:
		t.Fatalf("sdlc freeze ended before the check's child had started: %v\n%s", err, out.String())
	case <-guard:
		t.Fatalf("the check's child did not start:\n%s", out.String())
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		// 130 where the handler ends the process, and 1 where the refusal of the
		// killed check gets there first: which one is a race, and neither froze.
		if err == nil {
			t.Errorf("sdlc freeze ended well after an interrupt:\n%s", out.String())
		}
	case <-guard:
		t.Fatalf("sdlc freeze did not end after an interrupt:\n%s", out.String())
	}
	select {
	case <-gone:
	case <-guard:
		t.Error("after an interrupt, a process the check started is still running")
	}
	if exists(filepath.Join(tree, ".sdlc", "state", "tests.lock")) {
		t.Error("an interrupted freeze left a freeze on disk")
	}
	if got := sb.fcFreezeEvents(t); len(got) != 0 {
		t.Errorf("the record says it was frozen: %q", got)
	}
	// The lock went back. A freeze that took it again as soon as the check was killed
	// held it as the handler exited, having found nothing held, and this write then
	// waited out its ten seconds and failed with SDLC-E0006: 3 runs in 25 on a loaded
	// machine. TestAC2ACheckThatDidNotPassTakesNoLockBackBeforeItsRefusal holds the
	// order every time; this holds what a person sees.
	sb.works(t, tree, "", "", "cost", "add", "--usd", "0.25")
}

// THREATS 2: a child of the binary is not a tool call and the hook never sees it,
// so what keeps a person's commands from a model's check is the environment it
// inherits. A check that scrubbed it would hand a model the unfreeze.
func TestAC1APersonsCommandRunByTheCheckIsRefusedInsideASession(t *testing.T) {
	parallel(t)
	said := filepath.Join(scratch(t), "said")
	sb := fcAtTheTests(t, `"$SDLC_BIN" unfreeze --reason "from inside the check" > `+fcShellPath(said)+` 2>&1; exit 0`)
	tree := sb.tree()
	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)

	r := sb.insideClaudeCode(t, sandboxSession, tree, bashMarkers, "freeze")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	if got := readFile(t, said); !strings.Contains(got, inClaudeCodeCode) {
		t.Errorf("a person's command run by the check was not refused:\n%s", got)
	}
	if !exists(filepath.Join(tree, ".sdlc", "state", "tests.lock")) {
		t.Error("the freeze is not on disk")
	}
}

// The landing's commands run with git's location variables taken away, for the
// same reason: a freeze started from a git hook inherits them, and a check that
// asks git about this repository would be answered about another, or fail.
func TestAC1TheCheckIsNotPointedAtAnotherRepositoryByGitsOwnVariables(t *testing.T) {
	parallel(t)
	top := filepath.Join(scratch(t), "top")
	sb := fcAtTheTests(t, `git rev-parse --show-toplevel > `+fcShellPath(top))
	tree := sb.tree()
	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)
	elsewhere := scratch(t)

	r := sb.insideClaudeCode(t, sandboxSession, tree,
		[]string{"GIT_DIR=" + filepath.Join(elsewhere, "not-a-repository"), "GIT_INDEX_FILE=" + filepath.Join(elsewhere, "index")}, "freeze")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	if got := strings.TrimSpace(readFile(t, top)); got != filepath.ToSlash(tree) {
		t.Errorf("the check's git found %q, want %q", got, filepath.ToSlash(tree))
	}
	if _, err := os.Stat(filepath.Join(tree, ".sdlc", "state", "tests.lock")); err != nil {
		t.Errorf("the freeze is not on disk: %v", err)
	}
}
