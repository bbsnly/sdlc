package shellpolicy

// US-112 AC-8 and AC-23: the class and program name a `shell` trace record
// keeps of a command, and no other word of it. The reading is this package's
// lexer, the one the evasions are tried against, and it decides nothing.

import (
	"strings"
	"testing"
)

// configured is a project's own commands, by the class each stands for, as
// .sdlc/config.json names them.
var configured = map[string]string{
	"test":  "./task test",
	"build": "./task build",
	"lint":  "./task lint",
}

type classified struct{ class, program string }

func classifyRows(t *testing.T, powerShell bool, rows map[string]classified) {
	t.Helper()
	for command, want := range rows {
		class, program := Classify(command, powerShell, configured)
		if class != want.class || program != want.program {
			t.Errorf("%q is class %q, program %q; want %q, %q", command, class, program, want.class, want.program)
		}
	}
}

func TestAC8ACommandIsClassedByWhatItRuns(t *testing.T) {
	t.Parallel()
	classifyRows(t, false, map[string]classified{
		// The project's own commands, as configured or started with.
		"./task test":                   {"test", "task"},
		"./task test -run TestX":        {"test", "task"},
		"./task build":                  {"build", "task"},
		"./task lint":                   {"lint", "task"},
		"./task check":                  {"other", "task"},
		"go test ./...":                 {"test", "go"},
		"go test -race ./... -count=1":  {"test", "go"},
		"go build ./...":                {"build", "go"},
		"go vet ./...":                  {"other", "go"},
		"cargo test":                    {"test", "cargo"},
		"cargo build --release":         {"build", "cargo"},
		"npm test":                      {"test", "npm"},
		"npm run test":                  {"test", "npm"},
		"npm run build":                 {"build", "npm"},
		"pnpm test":                     {"test", "pnpm"},
		"yarn test":                     {"test", "yarn"},
		"yarn build":                    {"build", "yarn"},
		"pytest -q tests/":              {"test", "pytest"},
		"make test":                     {"test", "make"},
		"make build":                    {"build", "make"},
		"go test ./... 2>&1 | tail -30": {"test", "go"},
		"git status --short":            {"git", "git"},
		"git commit -m 'go test'":       {"git", "git"},
		"/usr/bin/git log":              {"git", "git"},
		"sdlc status":                   {"sdlc", "sdlc"},
		"sdlc gate tests_frozen pass":   {"sdlc", "sdlc"},
		"ls -la":                        {"other", "ls"},
		"cat README.md":                 {"other", "cat"},
		// Decided by nothing here: a command the rules refuse is classed all
		// the same.
		"rm .sdlc/state/active": {"other", "rm"},
	})
}

// AC-8: leading `VAR=value` words are not the program.
func TestAC8LeadingAssignmentsAreSkipped(t *testing.T) {
	t.Parallel()
	classifyRows(t, false, map[string]classified{
		"GOFLAGS=-mod=mod go test ./...":    {"test", "go"},
		"A=1 B=2 git status":                {"git", "git"},
		"CGO_ENABLED=0 go build ./cmd/sdlc": {"build", "go"},
	})
}

// A person's decision on AC-8 (2026-10-28): the class and program come from the
// first command after any leading directory changes joined by && or ;. The
// runbook starts every command in a story's worktree with `cd -- '<tree>' &&`,
// and read literally every test run there was class other, program cd.
func TestAC8TheProgramIsReadAfterLeadingDirectoryChanges(t *testing.T) {
	t.Parallel()
	classifyRows(t, false, map[string]classified{
		"cd -- '/x/y' && go test ./...":                    {"test", "go"},
		"cd /x; go test ./...":                             {"test", "go"},
		"cd /x && cd internal && git status":               {"git", "git"},
		"pushd /x && make test":                            {"test", "make"},
		"chdir /x && ./task lint":                          {"lint", "task"},
		"cd -- '/Users/dev/my repo' && ./task test -run X": {"test", "task"},
		"cd /x && FOO=1 cargo test":                        {"test", "cargo"},
		"cd /x":                                            {"other", "cd"},
		// Only && and ; join a directory change to what it runs in; after ||
		// the next command runs only where the change failed.
		"cd /x || go test ./...": {"other", "cd"},
	})
	classifyRows(t, true, map[string]classified{
		`Set-Location C:\work; go test ./...`: {"test", "go"},
		`sl C:\work; git status`:              {"git", "git"},
		`cd C:\work; ./task test`:             {"test", "task"},
	})
}

// AC-23: the program name is read the same from a Windows path, an .exe and a
// bare name, and in PowerShell as in Bash.
func TestAC23TheProgramNameIsTheSameFromAWindowsPathAnExeAndABareName(t *testing.T) {
	t.Parallel()
	for _, powerShell := range []bool{false, true} {
		for _, command := range []string{`C:\Go\bin\go.exe test ./...`, "go.exe test ./...", "go test ./..."} {
			class, program := Classify(command, powerShell, configured)
			if class != "test" || program != "go" {
				t.Errorf("AC-23 %q (PowerShell %v) is %q, %q; want test, go", command, powerShell, class, program)
			}
		}
	}
	if _, program := Classify(`C:\tools\git.exe status`, true, configured); program != "git" {
		t.Errorf("AC-23 git.exe under a Windows path is program %q, want git", program)
	}
}

// AC-8: a command the parser cannot read is class other with no program.
func TestAC8ACommandThatCannotBeReadIsOtherWithNoProgram(t *testing.T) {
	t.Parallel()
	for _, command := range []string{`echo "unterminated`, "", "   ", "echo " + strings.Repeat("$(", 100)} {
		class, program := Classify(command, false, configured)
		if class != "other" || program != "" {
			t.Errorf("%q is %q, %q; want other with no program", command, class, program)
		}
	}
}

// AC-12 and THREATS I2: the program word is the model's, so a name that is not
// a plain file name is not kept, and nothing is expanded.
func TestAC12AProgramWordThatIsNotAPlainFileNameIsNotKept(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		`"/tmp/$(cat ~/.ssh/id_rsa)" run`,
		`$HOME/bin/tool run`,
		"${GOBIN}/go test ./...",
		"`which go` test ./...",
		`'my tool' run`,
		strings.Repeat("a", 300) + " run",
		"./tool=x run",
	} {
		_, program := Classify(command, false, configured)
		if program != "" {
			t.Errorf("%q kept program %q", command, program)
		}
		if strings.Contains(program, "id_rsa") || strings.Contains(program, "/") || strings.Contains(program, "$") {
			t.Errorf("%q kept text of the command as its program: %q", command, program)
		}
	}
	// A plain file name is kept whatever it says; it is the one word AC-8
	// keeps.
	if _, program := Classify("/opt/x/run-it_2.sh --flag secret", false, configured); program != "run-it_2.sh" {
		t.Errorf("a plain program name was not kept: %q", program)
	}
}

// AC-8: only the program is kept. Every other word of the command, its
// arguments and paths, is in neither result.
func TestAC8NoWordButTheProgramComesBack(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"cd /secret/MARKER && go test ./MARKER/...",
		"MARKER=1 go test -run MARKER",
		"git commit -m MARKER",
		"./task test MARKER",
	} {
		class, program := Classify(command, false, configured)
		if strings.Contains(class+program, "MARKER") || strings.Contains(class+program, "secret") {
			t.Errorf("%q gave %q, %q, which holds a word of the command", command, class, program)
		}
	}
}

// AC-8: with no project commands configured, the built-in readings still hold.
func TestAC8WithNothingConfiguredGoAndTheOthersAreStillRead(t *testing.T) {
	t.Parallel()
	for command, want := range map[string]string{"go test ./...": "test", "cargo build": "build", "./task test": "other"} {
		if class, _ := Classify(command, false, nil); class != want {
			t.Errorf("%q with nothing configured is %q, want %q", command, class, want)
		}
	}
}
