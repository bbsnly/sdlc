package scaffold

// US-134 AC-10 and AC-17: the configuration `sdlc init` writes for each stack
// it supports. Its test and coverage commands share one run through
// $SDLC_RUN_DIR, so the suite runs once per tree; and it gives a narrow test
// command, commands.test_files, that runs only the test files the list in
// $SDLC_TEST_FILES names.
//
// The spelling of each command is the implementer's: these tests run what init
// wrote and judge what it did. Go runs with the real toolchain, which every
// machine that runs these tests has; pytest, cargo and npm are stand-ins that
// write down how they were called, as the stand-in in coverage_test.go does.
// Every command is run under bash and under zsh, where each is installed: the
// binary hands a command to the shell it finds, and a person's may be either.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// us134Shells is bash and zsh where each is installed, a subtest each.
func us134Shells(t *testing.T, each func(t *testing.T, shell string)) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are POSIX shell scripts, which Windows does not run by name; sdlc runs a command through Git's bash there")
	}
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			shell, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("no %s on this machine to run the command under", name)
			}
			each(t, shell)
		})
	}
}

// us134Run runs command under shell in dir with env added, and returns stdout,
// and stdout and stderr together for a failure's message.
func us134Run(t *testing.T, shell, dir, command string, env ...string) (stdout, all string, err error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), shell, "-c", command)
	cmd.Dir = dir
	cmd.Env = append(withoutVariables(os.Environ(), "SDLC_RUN_DIR", "SDLC_TEST_FILES", "COVERAGE_FILE"), env...)
	var out, both bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &both
	err = cmd.Run()
	return out.String(), out.String() + both.String(), err
}

// us134StandIn is a program that writes the words it was called with, one
// call to a line and its words split by a unit separator, to $US134_LOG.
const us134StandIn = `#!/bin/sh
{ printf '%s' "${0##*/}"; for a in "$@"; do printf '\037%s' "$a"; done; printf '\n'; } >> "$US134_LOG"
`

// us134Bin is a directory of stand-ins for the programs named, and the log they
// write to.
func us134Bin(t *testing.T, programs ...string) (bin, log string) {
	t.Helper()
	bin = t.TempDir()
	for _, p := range programs {
		if err := os.WriteFile(filepath.Join(bin, p), []byte(us134StandIn), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin, filepath.Join(t.TempDir(), "calls")
}

// us134Calls is every call the stand-ins logged: the program, then its words.
func us134Calls(t *testing.T, log string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for line := range strings.Lines(string(raw)) {
		calls = append(calls, strings.Split(strings.TrimSuffix(line, "\n"), "\x1f"))
	}
	return calls
}

// us134List writes the list SDLC_TEST_FILES names: one path to a line.
func us134List(t *testing.T, names ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-files")
	if err := os.WriteFile(path, []byte(strings.Join(names, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// us134Names is whether call names every one of files, as given or from ./.
func us134Names(call []string, files ...string) bool {
	for _, f := range files {
		if !slices.Contains(call, f) && !slices.Contains(call, "./"+f) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- AC-17

// AC-17: init writes commands.test_files for every stack it supports, and the
// key, empty, where it knows no stack; and what it writes, with the test and
// coverage commands beside it, is not refused by the hook from any role that
// runs it.
func TestAC17InitWritesTheNarrowTestCommandForEveryStackItSupports(t *testing.T) {
	for _, stack := range coverageStacks {
		t.Run("AC-17 "+stack.name, func(t *testing.T) {
			commands := writtenCommands(t, stack.files)
			if commands["test_files"] == "" {
				t.Fatalf("AC-17 init wrote no commands.test_files for a %s project", stack.name)
			}
			for _, key := range []string{"test", "test_files", "coverage"} {
				for _, agent := range []string{"", "sdet", "implementer", "verifier"} {
					if f, refused := shellpolicy.Inspect(commands[key], shellpolicy.State{Agent: agent}); refused {
						t.Errorf("AC-17 the hook refuses commands.%s %q (agent %q): %s", key, commands[key], agent, f.Message())
					}
				}
			}
		})
	}
	t.Run("AC-17 no stack", func(t *testing.T) {
		root := t.TempDir()
		if _, err := Init(root, false); err != nil {
			t.Fatal(err)
		}
		if raw := read(t, root, ".sdlc/config.json"); !strings.Contains(raw, `"test_files"`) {
			t.Errorf("AC-17 the configuration init writes has no commands.test_files key to fill in (%d bytes)", len(raw))
		}
	})
}

// AC-17, Go: the packages that hold the files listed, each once, and no other:
// a test file at the root is the root package's, and a fixture under testdata/
// is the package's above it.
func TestAC17TheGoNarrowCommandTestsThePackagesThatHoldTheListedFiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on this machine to run the command with")
	}
	command := writtenCommands(t, map[string]string{"go.mod": "module example.com/demo\n\ngo 1.21\n"})["test_files"]
	root := t.TempDir()
	passing := func(pkg, name string) string {
		return "package " + pkg + "\n\nimport \"testing\"\n\nfunc Test" + name + "(t *testing.T) {}\n"
	}
	touch(t, root, "go.mod", "module example.com/demo\n\ngo 1.21\n")
	touch(t, root, "demo_test.go", passing("demo", "Here"))
	touch(t, root, "internal/billing/invoice_test.go", passing("billing", "Here"))
	touch(t, root, "internal/billing/refund_test.go", passing("billing", "Refund"))
	touch(t, root, "internal/billing/testdata/invoice.golden", "golden\n")
	touch(t, root, "internal/tax/tax_test.go", passing("tax", "Here"))
	touch(t, root, "internal/tax/testdata/rates.json", "{}\n")
	touch(t, root, "internal/unlisted/unlisted_test.go", passing("unlisted", "Here"))
	touch(t, root, "cmd/demo/main_test.go", passing("main", "Here"))
	list := us134List(t, "demo_test.go", "internal/billing/invoice_test.go", "internal/billing/refund_test.go",
		"internal/billing/testdata/invoice.golden", "internal/tax/testdata/rates.json", "cmd/demo/main_test.go")

	us134Shells(t, func(t *testing.T, shell string) {
		t.Helper()
		stdout, all, err := us134Run(t, shell, root, command, "SDLC_TEST_FILES="+list)
		if err != nil {
			t.Fatalf("AC-17 %q failed under %s: %v\n%s", command, filepath.Base(shell), err, all)
		}
		var tested []string
		for line := range strings.Lines(stdout) {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "ok" {
				tested = append(tested, f[1])
			}
		}
		slices.Sort(tested)
		want := []string{"example.com/demo", "example.com/demo/cmd/demo", "example.com/demo/internal/billing", "example.com/demo/internal/tax"}
		if !slices.Equal(tested, want) {
			t.Errorf("AC-17 %q tested %q, want %q\n%s", command, tested, want, all)
		}
	})
}

// AC-17, Python and Node: the files listed, a name with a space in it whole.
func TestAC17ThePythonAndNodeNarrowCommandsRunTheListedFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    map[string]string
		programs []string
		listed   []string
	}{
		{"AC-17 Python", map[string]string{"pyproject.toml": "[project]\nname = \"demo\"\n"},
			[]string{"pytest", "python", "python3"}, []string{"tests/test_invoice.py", "tests/test refund.py"}},
		{"AC-17 Node", map[string]string{"package.json": `{"scripts":{"test":"vitest run"}}`},
			[]string{"npm", "npx", "node"}, []string{"test/invoice.test.js", "test/refund one.test.js"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := writtenCommands(t, tc.files)["test_files"]
			if command == "" {
				t.Fatalf("%s: init wrote no commands.test_files", tc.name)
			}
			root := t.TempDir()
			for _, f := range tc.listed {
				touch(t, root, f, "// a test\n")
			}
			list := us134List(t, tc.listed...)
			us134Shells(t, func(t *testing.T, shell string) {
				t.Helper()
				bin, log := us134Bin(t, tc.programs...)
				_, all, err := us134Run(t, shell, root, command, "SDLC_TEST_FILES="+list, "US134_LOG="+log,
					"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				if err != nil {
					t.Fatalf("%s: %q failed: %v\n%s", tc.name, command, err, all)
				}
				calls := us134Calls(t, log)
				if !slices.ContainsFunc(calls, func(c []string) bool { return us134Names(c, tc.listed...) }) {
					t.Errorf("%s: no call of %q named every listed file, each as one word: %q", tc.name, command, calls)
				}
			})
		})
	}
}

// AC-17, Rust: an integration test under tests/ is selected by its name; where
// a file listed cannot be selected that way, the whole of cargo test runs.
func TestAC17TheRustNarrowCommandSelectsIntegrationTestsAndRunsAllWhereOneCannotBe(t *testing.T) {
	command := writtenCommands(t, map[string]string{"Cargo.toml": "[package]\nname = \"demo\"\n"})["test_files"]
	if command == "" {
		t.Fatal("AC-17 init wrote no commands.test_files for a Rust project")
	}
	root := t.TempDir()
	touch(t, root, "Cargo.toml", "[package]\nname = \"demo\"\n")
	for _, tc := range []struct {
		name   string
		listed []string
		// selected is the integration tests named; nil is the whole of cargo test.
		selected []string
	}{
		{"AC-17 integration tests", []string{"tests/billing.rs", "tests/refund.rs"}, []string{"billing", "refund"}},
		{"AC-17 a unit test file in src", []string{"tests/billing.rs", "src/lib_test.rs"}, nil},
		{"AC-17 a module under tests", []string{"tests/common/mod.rs"}, nil},
		{"AC-17 a fixture", []string{"fixtures/invoice.json"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, f := range tc.listed {
				touch(t, root, f, "// a test\n")
			}
			list := us134List(t, tc.listed...)
			us134Shells(t, func(t *testing.T, shell string) {
				t.Helper()
				bin, log := us134Bin(t, "cargo")
				_, all, err := us134Run(t, shell, root, command, "SDLC_TEST_FILES="+list, "US134_LOG="+log,
					"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				if err != nil {
					t.Fatalf("%s: %q failed: %v\n%s", tc.name, command, err, all)
				}
				calls := us134Calls(t, log)
				if len(calls) != 1 || len(calls[0]) < 2 || calls[0][0] != "cargo" || calls[0][1] != "test" {
					t.Fatalf("%s: %q called %q, want one cargo test", tc.name, command, calls)
				}
				var named []string
				words := calls[0][2:]
				for i, w := range words {
					switch {
					case w == "--test" && i+1 < len(words):
						named = append(named, words[i+1])
					case strings.HasPrefix(w, "--test="):
						named = append(named, strings.TrimPrefix(w, "--test="))
					}
				}
				slices.Sort(named)
				if !slices.Equal(named, tc.selected) {
					t.Errorf("%s: cargo test selected %q, want %q (none: the whole of cargo test)", tc.name, named, tc.selected)
				}
				if tc.selected == nil && len(words) > 0 && !strings.HasPrefix(words[0], "-") {
					t.Errorf("%s: cargo test was given a filter, %q, and did not run the whole suite", tc.name, words)
				}
			})
		})
	}
}

// ---------------------------------------------------------------- AC-10

// AC-10, Go: the test command leaves its profile in $SDLC_RUN_DIR and the
// coverage command reads the figure from it, without running the suite again: in
// a run directory that test did not fill, coverage prints no figure.
func TestAC10GosTestAndCoverageCommandsShareOneRunThroughTheRunDirectory(t *testing.T) {
	root, coverage := goProject(t)
	want := wantedPercentage(t, root)
	written := writtenCommands(t, map[string]string{"go.mod": "module example.com/demo\n\ngo 1.21\n"})
	for _, key := range []string{"test", "coverage"} {
		if !strings.Contains(written[key], "SDLC_RUN_DIR") {
			t.Errorf("AC-10 the Go commands.%s init writes does not use $SDLC_RUN_DIR: %q", key, written[key])
		}
	}
	before := snapshot(t, root)
	us134Shells(t, func(t *testing.T, shell string) {
		t.Helper()
		dir := t.TempDir()
		if _, all, err := us134Run(t, shell, root, written["test"], "SDLC_RUN_DIR="+dir); err != nil {
			t.Fatalf("AC-10 commands.test failed: %v\n%s", err, all)
		}
		stdout, all, err := us134Run(t, shell, root, coverage, "SDLC_RUN_DIR="+dir)
		if err != nil || lastNumber(stdout) != want {
			t.Errorf("AC-10 commands.coverage after commands.test printed %q (%v), want %s last\n%s", stdout, err, want, all)
		}
		if stdout, _, _ := us134Run(t, shell, root, coverage, "SDLC_RUN_DIR="+t.TempDir()); lastNumber(stdout) != "" {
			t.Errorf("AC-10 commands.coverage printed %q in a run directory commands.test did not fill: it ran the suite again", stdout)
		}
		if left := changedFiles(before, snapshot(t, root)); len(left) != 0 {
			t.Errorf("AC-10 the two commands left %v in the project", left)
		}
	})
}

// us134PythonTools stand in for pytest with pytest-cov and for coverage.py, by
// what each documents: pytest-cov writes its data at COVERAGE_FILE, else
// .coverage, and prints its report; `coverage report` reads the same data, and
// has none to report where there is none. Each call is logged.
const us134Pytest = `#!/bin/sh
printf 'pytest\n' >> "$US134_LOG"
printf 'data' > "${COVERAGE_FILE:-.coverage}"
printf '%s\n' 'Name         Stmts   Miss  Cover' 'app.py          10      2    80%' 'TOTAL           10      2    80%'
`

const us134Coverage = `#!/bin/sh
case "$1" in -m) shift; [ "$1" = pytest ] && { shift; exec pytest "$@"; }; shift ;; esac
printf 'coverage %s\n' "$*" >> "$US134_LOG"
if [ -f "${COVERAGE_FILE:-.coverage}" ]; then
  printf '%s\n' 'Name         Stmts   Miss  Cover' 'app.py          10      2    80%' 'TOTAL           10      2    80%'
else
  echo 'No data to report.'; exit 1
fi
`

// AC-10, Python: the same, with pytest and coverage.py stood in for.
func TestAC10PythonsTestAndCoverageCommandsShareOneRunThroughTheRunDirectory(t *testing.T) {
	root, coverage := pythonProject(t, nil)
	written := writtenCommands(t, map[string]string{"pyproject.toml": "[project]\nname = \"demo\"\n"})
	for _, key := range []string{"test", "coverage"} {
		if !strings.Contains(written[key], "SDLC_RUN_DIR") {
			t.Errorf("AC-10 the Python commands.%s init writes does not use $SDLC_RUN_DIR: %q", key, written[key])
		}
	}
	bin := t.TempDir()
	for name, body := range map[string]string{"pytest": us134Pytest, "coverage": us134Coverage, "python": us134Coverage, "python3": us134Coverage} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")
	before := snapshot(t, root)
	us134Shells(t, func(t *testing.T, shell string) {
		t.Helper()
		log := filepath.Join(t.TempDir(), "calls")
		dir := t.TempDir()
		if _, all, err := us134Run(t, shell, root, written["test"], "SDLC_RUN_DIR="+dir, "US134_LOG="+log, path); err != nil {
			t.Fatalf("AC-10 commands.test failed: %v\n%s", err, all)
		}
		stdout, all, err := us134Run(t, shell, root, coverage, "SDLC_RUN_DIR="+dir, "US134_LOG="+log, path)
		if err != nil || lastNumber(stdout) != "80" {
			t.Errorf("AC-10 commands.coverage after commands.test printed %q (%v), want 80 last\n%s", stdout, err, all)
		}
		raw, _ := os.ReadFile(log)
		if got := strings.Count(string(raw), "pytest\n"); got != 1 {
			t.Errorf("AC-10 pytest ran %d times for one test and one coverage, want once:\n%s", got, raw)
		}
		if stdout, _, _ := us134Run(t, shell, root, coverage, "SDLC_RUN_DIR="+t.TempDir(), "US134_LOG="+log, path); lastNumber(stdout) != "" {
			t.Errorf("AC-10 commands.coverage printed %q in a run directory commands.test did not fill", stdout)
		}
		if left := changedFiles(before, snapshot(t, root)); len(left) != 0 {
			t.Errorf("AC-10 the two commands left %v in the project", left)
		}
	})
}
