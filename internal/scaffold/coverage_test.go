package scaffold

// US-017: the coverage command `sdlc init` writes is one the hook permits
// (AC-1, AC-2), puts its profile outside the loop's records and prints its
// percentage as the last number on stdout (AC-2), and leaves nothing in the
// project for the commit gate to refuse (AC-4).
//
// The spelling of the command is the implementer's: these tests pin what is
// observable. What the written command names, what the hook's classifier says
// of it, and what a real run in a project leaves behind.

import (
	"bytes"
	"crypto/sha256"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// coverageStacks is every stack init detects, with a file that makes it
// detected, and whether it is given a coverage command.
var coverageStacks = []struct {
	name     string
	files    map[string]string
	coverage bool
}{
	{"Go", map[string]string{"go.mod": "module example.com/demo\n\ngo 1.21\n"}, true},
	{"Python", map[string]string{"pyproject.toml": "[project]\nname = \"demo\"\n"}, true},
	{"Rust", map[string]string{"Cargo.toml": "[package]\nname = \"demo\"\n"}, false},
	{"Node", map[string]string{"package.json": `{"scripts":{"test":"vitest run","build":"tsc"}}`}, false},
}

// writtenCommands is the commands section of the configuration `sdlc init`
// writes into a project made of files: what a person's project holds, and what
// an agent is told to run, not what Detect returned before it was rendered.
func writtenCommands(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		touch(t, root, name, body)
	}
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Commands
}

// AC-1 holds today and is a guard: the command init writes is not one the hook
// refuses, for any stack, from any role. It stays true of whatever replaces the
// command, which is what the story's change has to keep.
func TestAC1AndAC2TheCoverageCommandInitWritesIsNotOneTheHookRefuses(t *testing.T) {
	for _, stack := range coverageStacks {
		if !stack.coverage {
			continue
		}
		t.Run(stack.name, func(t *testing.T) {
			command := writtenCommands(t, stack.files)["coverage"]
			if command == "" {
				t.Fatalf("init wrote no coverage command for a %s project", stack.name)
			}
			for _, agent := range []string{"", "implementer", "verifier"} {
				if f, refused := shellpolicy.Inspect(command, shellpolicy.State{Agent: agent}); refused {
					t.Errorf("the hook refuses %q (agent %q): %s", command, agent, f.Message())
				}
			}
		})
	}
}

// A stack with no coverage command is not given a broken one: an empty command
// is skipped by the gates that use it, and a command that cannot run fails them.
func TestAC2AStackWithNoCoverageCommandIsNotGivenOne(t *testing.T) {
	for _, stack := range coverageStacks {
		if stack.coverage {
			continue
		}
		t.Run(stack.name, func(t *testing.T) {
			if command := writtenCommands(t, stack.files)["coverage"]; command != "" {
				t.Errorf("init wrote coverage %q for a %s project, which has no coverage tool this version knows", command, stack.name)
			}
		})
	}
}

// The profile goes where the hook permits a write and outside the loop's
// records. The one thing a command's text can say about that is which paths it
// names: not one under .sdlc/, where the loop keeps its state, and not one
// under .git/.
func TestAC2TheWrittenCoverageCommandNamesNoPathUnderTheLoopsRecords(t *testing.T) {
	for _, stack := range coverageStacks {
		if !stack.coverage {
			continue
		}
		t.Run(stack.name, func(t *testing.T) {
			command := writtenCommands(t, stack.files)["coverage"]
			for _, place := range []string{".sdlc", ".git/", ".git\\"} {
				if strings.Contains(command, place) {
					t.Errorf("the coverage command names %s, where the loop keeps its own files: %q", place, command)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- running it

// shellFor is the shell the commands run under, or skips with the reason.
func shellFor(t *testing.T) string {
	t.Helper()
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to run the command with; the written command is a POSIX shell command, which sdlc runs through Git's sh on Windows")
	}
	return shell
}

// gitIn runs git in dir and fails the test if it does not work.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git",
		append([]string{"-c", "user.name=Test", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// withoutVariables is env less the named variables.
func withoutVariables(env []string, names ...string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(names, name) {
			out = append(out, kv)
		}
	}
	return out
}

// runCoverage runs command in dir under sh, with the temp directory in tmp and
// extra added, and returns what it printed on stdout. The temp directory is the
// test's own, so a profile that goes there is found and removed with it, and
// one that does not is not hidden by the machine's.
func runCoverage(t *testing.T, dir, command, tmp string, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), shellFor(t), "-c", command)
	cmd.Dir = dir
	cmd.Env = append(withoutVariables(os.Environ(), "TMPDIR", "TMP", "TEMP", "COVERAGE_FILE"),
		"TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
	cmd.Env = append(cmd.Env, extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		t.Logf("%s\n%s", stdout.String(), stderr.String())
	}
	return stdout.String(), err
}

var number = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// lastNumber is the last number on stdout, which is how the gates read a
// coverage command.
func lastNumber(stdout string) string {
	all := number.FindAllString(stdout, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// snapshot is every file in dir but git's own, with a hash of what it holds,
// so that a file written, ignored or not, shows.
func snapshot(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = sha256.Sum256(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// changedFiles names what is in after and not the same in before.
func changedFiles(before, after map[string][32]byte) []string {
	var out []string
	for name, sum := range after {
		if was, ok := before[name]; !ok || was != sum {
			out = append(out, name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			out = append(out, name+" (removed)")
		}
	}
	slices.Sort(out)
	return out
}

// goProject is a Go project as one in use is: `sdlc init` run in it, a package
// with one function its test covers and one it does not, the loop's state
// directory holding a tracked file, and everything committed. The coverage
// command is the one init wrote.
func goProject(t *testing.T) (root, command string) {
	t.Helper()
	root = t.TempDir()
	touch(t, root, "go.mod", "module example.com/demo\n\ngo 1.21\n")
	touch(t, root, "calc/calc.go", "package calc\n\nfunc Used() int { return 1 }\n\nfunc Unused() int { return 2 }\n")
	touch(t, root, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestUsed(t *testing.T) {\n\tif Used() != 1 {\n\t\tt.Fatal(\"Used\")\n\t}\n}\n")
	gitIn(t, root, "init", "--quiet", "--initial-branch=main")
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	touch(t, root, ".sdlc/state/tests.lock", "{}\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "--quiet", "-m", "demo")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	command = cfg.Commands["coverage"]
	if command == "" {
		t.Fatal("init wrote no coverage command for a Go project")
	}
	return root, command
}

// wantedPercentage is what the project covers, worked out without the written
// command: one statement of two is covered. It is not a number the command
// could print by being written to print it.
func wantedPercentage(t *testing.T, root string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go to run the command with")
	}
	profile := filepath.Join(t.TempDir(), "profile")
	run := exec.CommandContext(t.Context(), "go", "test", "./...", "-count=1", "-coverprofile="+profile)
	run.Dir = root
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(t.Context(), "go", "tool", "cover", "-func="+profile)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var total string
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "total:") {
			total = lastNumber(line)
		}
	}
	if total != "50.0" {
		t.Fatalf("the fixture covers %q, want 50.0: one statement of two", total)
	}
	return total
}

// AC-2: run for real, the Go command prints the project's percentage as the
// last number on stdout, and puts its profile somewhere else than the loop's
// records: nothing under .sdlc/ is written.
func TestAC2TheGoCoverageCommandPrintsItsPercentageLastAndWritesNothingUnderTheLoopsRecords(t *testing.T) {
	root, command := goProject(t)
	want := wantedPercentage(t, root)
	before := snapshot(t, root)

	stdout, err := runCoverage(t, root, command, t.TempDir())
	if err != nil {
		t.Fatalf("%q failed: %v", command, err)
	}
	if got := lastNumber(stdout); got != want {
		t.Errorf("the last number on stdout is %q, want %q; stdout was %q", got, want, stdout)
	}
	for _, name := range changedFiles(before, snapshot(t, root)) {
		if strings.HasPrefix(name, ".sdlc/") {
			t.Errorf("the coverage command wrote %s, in the loop's own directory", name)
		}
	}
}

// AC-4: whatever the profile is, it is not left in the project to be committed
// or ignored. In the checkout init was run in, and in a story's linked
// worktree, where `sdlc start` ignores .sdlc/state with a .gitignore of its own
// and a profile written there is hidden from git, not elsewhere.
func TestAC4TheGoCoverageCommandLeavesNothingInTheProjectForTheCommitGateToRefuse(t *testing.T) {
	root, command := goProject(t)
	worktree := filepath.Join(t.TempDir(), "US-017")
	gitIn(t, root, "worktree", "add", "--quiet", "-b", "story/US-017", worktree)
	// What EnsureStateIgnored writes in a story's worktree.
	touch(t, worktree, ".sdlc/state/.gitignore", "*\n")

	for name, dir := range map[string]string{"the checkout init ran in": root, "a story's linked worktree": worktree} {
		t.Run(name, func(t *testing.T) {
			before := snapshot(t, dir)
			if status := gitIn(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
				t.Fatalf("the project is not clean before the command runs:\n%s", status)
			}
			if _, err := runCoverage(t, dir, command, t.TempDir()); err != nil {
				t.Fatalf("%q failed: %v", command, err)
			}
			if status := gitIn(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
				t.Errorf("the coverage command left uncommitted work, which the commit gate (SDLC-E0032) refuses:\n%s", status)
			}
			if left := changedFiles(before, snapshot(t, dir)); len(left) != 0 {
				t.Errorf("the coverage command left %v in the project, ignored by git or not", left)
			}
		})
	}
}

// fakePytest stands in for pytest with pytest-cov, which is not on every
// machine: it writes its data file where pytest-cov does, at COVERAGE_FILE
// when that is set and .coverage in the working directory when it is not, and
// prints the terminal report pytest-cov prints.
const fakePytest = `#!/bin/sh
printf 'data' > "${COVERAGE_FILE:-.coverage}"
printf '%s\n' '---------- coverage: platform linux, python 3.12 ----------' \
	'Name         Stmts   Miss  Cover' 'app.py          10      2    80%' \
	'--------------------------------' 'TOTAL           10      2    80%'
`

// pythonProject is a Python project as one in use is, committed, with the
// coverage command init wrote for it.
func pythonProject(t *testing.T, files map[string]string) (root, command string) {
	t.Helper()
	root = t.TempDir()
	touch(t, root, "pyproject.toml", "[project]\nname = \"demo\"\n")
	for name, body := range files {
		touch(t, root, name, body)
	}
	gitIn(t, root, "init", "--quiet", "--initial-branch=main")
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	touch(t, root, ".sdlc/state/tests.lock", "{}\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "--quiet", "-m", "demo")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	command = cfg.Commands["coverage"]
	if command == "" {
		t.Fatal("init wrote no coverage command for a Python project")
	}
	return root, command
}

// AC-2 and AC-4 for Python, against a stand-in for the tool that behaves as
// pytest-cov documents: the data file is written in the working directory
// unless COVERAGE_FILE says otherwise. The command has to print the
// percentage last and leave no data file in the project.
func TestAC2AndAC4ThePythonCoverageCommandKeepsItsDataFileOutOfTheProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in pytest is a POSIX shell script, which Windows does not run by name")
	}
	root, command := pythonProject(t, nil)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "pytest"), []byte(fakePytest), 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)

	stdout, err := runCoverage(t, root, command, t.TempDir(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("%q failed: %v", command, err)
	}
	if got := lastNumber(stdout); got != "80" {
		t.Errorf("the last number on stdout is %q, want 80, the TOTAL of the report; stdout was %q", got, stdout)
	}
	if left := changedFiles(before, snapshot(t, root)); len(left) != 0 {
		t.Errorf("the coverage command left %v in the project: pytest-cov writes .coverage in the working directory unless told where", left)
	}
	if status := gitIn(t, root, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Errorf("the coverage command left uncommitted work, which the commit gate (SDLC-E0032) refuses:\n%s", status)
	}
}

// The same with the real tool, where it is installed. Not run on a machine
// that has no pytest-cov, which the stand-in above covers by its documented
// behaviour; this one is the check that the documented behaviour is the tool's.
func TestAC2AndAC4ThePythonCoverageCommandRunWithPytestCovLeavesTheProjectClean(t *testing.T) {
	pytest, err := exec.LookPath("pytest")
	if err != nil {
		t.Skip("no pytest on this machine, so the Python command is not run for real")
	}
	help, err := exec.CommandContext(t.Context(), pytest, "--help").Output()
	if err != nil || !strings.Contains(string(help), "--cov") {
		t.Skip("pytest is here without pytest-cov, so the Python command is not run for real")
	}
	root, command := pythonProject(t, map[string]string{
		"app.py":      "def used():\n    return 1\n",
		"test_app.py": "from app import used\n\n\ndef test_used():\n    assert used() == 1\n",
	})
	before := snapshot(t, root)

	stdout, runErr := runCoverage(t, root, command, t.TempDir())
	if runErr != nil {
		t.Fatalf("%q failed: %v", command, runErr)
	}
	if got := lastNumber(stdout); got != "100" {
		t.Errorf("the last number on stdout is %q, want 100: every line of the project is run; stdout was %q", got, stdout)
	}
	for _, name := range changedFiles(before, snapshot(t, root)) {
		// Python's own caches are not the coverage command's doing.
		if !strings.HasPrefix(name, "__pycache__/") && !strings.HasPrefix(name, ".pytest_cache/") {
			t.Errorf("the coverage command left %s in the project", name)
		}
	}
}
