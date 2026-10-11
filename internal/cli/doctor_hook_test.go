package cli

// US-017 AC-3: a configured command the hook would refuse is a problem doctor
// reports, naming the command by its key under commands and the rule that
// refuses it, and saying what to change. The rule is the classifier's, so the
// tests ask the classifier rather than hold the rule names themselves: a
// doctor that carried its own idea of what the hook refuses would drift from
// the hook the day either changed.
//
// What the finding is called, and which check carries it, is the
// implementation's; the tests find it by what it says.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/scaffold"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// configuredWith is a project `sdlc init` set up, with exactly these commands in
// its configuration.
func configuredWith(t *testing.T, commands map[string]string) string {
	t.Helper()
	root := project(t)
	mustRun(t, "init")
	var cfg config.Config
	wtReadJSON(t, filepath.Join(root, config.File), &cfg)
	cfg.Commands = commands
	wtWriteJSON(t, filepath.Join(root, config.File), cfg)
	return root
}

// said is everything one check says.
func said(c check) string {
	return c.Name + "\n" + c.Detail + "\n" + c.Fix
}

// aboutRule is the problems doctor reports that name a rule.
func aboutRule(p doctorPayload, rule string) []check {
	var out []check
	for _, c := range p.Checks {
		if c.State == stateProblem && strings.Contains(said(c), rule) {
			out = append(out, c)
		}
	}
	return out
}

// asWord is whether text has word as a word of its own: "lint" is in
// "linting" and not the command called lint.
func asWord(text, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

// refusedByTheHook is what the classifier says of command, with the state a
// project has when no story is being worked on.
func refusedByTheHook(command string) (rule string, refused bool) {
	f, refused := shellpolicy.Inspect(command, shellpolicy.State{})
	return f.Rule, refused
}

// refusedCommands are commands under standard keys and a custom one that the
// classifier really refuses, one rule each at least twice over.
var refusedCommands = []struct{ key, command, rule string }{
	{"lint", "rm -f .sdlc/state/cover.out", "loop-state-through-the-tool"},
	{"test", "go test ./... | tee .sdlc/state/cover.out", "loop-state-through-the-tool"},
	{"coverage", "go test ./... > .sdlc/state/cover.out", "loop-state-through-the-tool"},
	{"tidy_up", "rm -rf .sdlc", "loop-state-through-the-tool"},
	{"stamp", "echo reviewed > CLAUDE.md", "protected-path-through-the-tool"},
	{"hooky", "echo x > .git/hooks/pre-commit", "protected-path-through-the-tool"},
	{"cleanup", "git worktree remove old", "worktree-through-the-tool"},
}

// Words that would teach an agent to take the hook out of the way rather than
// change the command (THREATS.md: doctor's fix).
var loosening = []string{
	"disable", "bypass", "turn off", "switch off", "loosen", "weaken",
	"sdlc stop", "sdlc_enforce", "hooks.json", "--no-verify",
}

func TestAC3DoctorReportsAConfiguredCommandTheHookWouldRefuse(t *testing.T) {
	t.Parallel()
	for _, tc := range refusedCommands {
		t.Run(tc.key+" "+tc.rule, func(t *testing.T) {
			t.Parallel()
			// The table is only worth anything while it is true.
			if rule, refused := refusedByTheHook(tc.command); !refused || rule != tc.rule {
				t.Fatalf("the classifier says %q of %q (refused %v), the table expects %s", rule, tc.command, refused, tc.rule)
			}
			configuredWith(t, map[string]string{tc.key: tc.command, "fine_one": "true"})

			r := run(t, "doctor", "--json")
			if r.code != 1 {
				t.Errorf("doctor exited %d with a command the hook refuses configured, want 1", r.code)
			}
			got := decode[doctorPayload](t, r)
			if got.OK || got.Problems == 0 {
				t.Errorf("doctor found nothing wrong with commands.%s = %q: %+v", tc.key, tc.command, got)
			}
			found := aboutRule(got, tc.rule)
			if len(found) == 0 {
				t.Fatalf("no problem names the rule %s that refuses commands.%s = %q:\n%+v", tc.rule, tc.key, tc.command, got.Checks)
			}
			for _, c := range found {
				text := said(c)
				if !asWord(text, tc.key) {
					t.Errorf("the problem names the rule and not the command %s: %q", tc.key, text)
				}
				if strings.Contains(text, "fine_one") {
					t.Errorf("the problem names a command the hook allows: %q", text)
				}
				if strings.TrimSpace(c.Fix) == "" {
					t.Errorf("the problem says what is wrong and not what to change: %q", text)
				}
				if !strings.Contains(c.Fix, config.File) && !strings.Contains(c.Fix, "commands."+tc.key) {
					t.Errorf("the fix does not say where to change it (%s, or commands.%s): %q", config.File, tc.key, c.Fix)
				}
				for _, word := range loosening {
					if strings.Contains(strings.ToLower(c.Fix), word) {
						t.Errorf("the fix suggests getting round the hook (%q): %q", word, c.Fix)
					}
				}
			}

			// The same finding in the text a person reads.
			text := run(t, "doctor")
			if text.code != 1 {
				t.Errorf("doctor exited %d, want 1", text.code)
			}
			for _, want := range []string{tc.rule, "fix:"} {
				if !strings.Contains(text.stdout, want) {
					t.Errorf("doctor's output does not contain %q:\n%s", want, text.stdout)
				}
			}
			if !asWord(text.stdout, tc.key) {
				t.Errorf("doctor's output does not name the command %s:\n%s", tc.key, text.stdout)
			}
		})
	}
}

// Several at once: each is named with a rule, and the rules are the ones that
// refuse them, not one rule said of all.
func TestAC3DoctorNamesEveryRefusedCommandAndTheRuleThatRefusesIt(t *testing.T) {
	t.Parallel()
	commands := map[string]string{"fine_one": "true"}
	for _, tc := range refusedCommands {
		commands[tc.key] = tc.command
	}
	configuredWith(t, commands)

	got := decode[doctorPayload](t, run(t, "doctor", "--json"))
	for _, tc := range refusedCommands {
		var named bool
		for _, c := range aboutRule(got, tc.rule) {
			named = named || asWord(said(c), tc.key)
		}
		if !named {
			t.Errorf("no problem names both commands.%s and the rule %s that refuses %q:\n%+v", tc.key, tc.rule, tc.command, got.Checks)
		}
	}
}

// The verdict is the classifier's, command by command: refused where it
// refuses, with the rule it names, and silent where it allows. The table's
// verdicts come from asking it.
func TestAC3DoctorAgreesWithTheClassifierOnEveryCommandInATable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	touch := func(name string) {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch("go.mod")
	goStack, _ := scaffold.Detect(root)
	table := []string{
		// Refused.
		"rm -f .sdlc/state/cover.out",
		"go test ./... > .sdlc/state/cover.out",
		"echo hi > .sdlc/config.json",
		"sed -i s/a/b/ .sdlc/state/session",
		"echo hi > CLAUDE.md",
		"echo x > .git/config",
		"git worktree remove old",
		// Allowed.
		"true",
		"echo 100",
		"go test ./... -count=1",
		"rm -f cover.out",
		"go test ./... -coverprofile=/tmp/cover.out",
		`test -z "$(gofmt -l .)"`,
		"git config core.hooksPath hooks",
		"cp a .sdlc/stories/US-001/record/x",
		// What init writes for Go, whatever it is by the time this runs.
		goStack.Commands["coverage"],
		goStack.Commands["test"],
	}
	var refused, allowed int
	for _, command := range table {
		rule, isRefused := refusedByTheHook(command)
		if isRefused {
			refused++
		} else {
			allowed++
		}
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			configuredWith(t, map[string]string{"probe_cmd": command, "fine_one": "true"})
			got := decode[doctorPayload](t, run(t, "doctor", "--json"))

			var about []check
			for _, c := range got.Checks {
				if c.State == stateProblem && c.Name != "commands on PATH" && asWord(said(c), "probe_cmd") {
					about = append(about, c)
				}
			}
			if !isRefused {
				if len(about) != 0 {
					t.Errorf("doctor reports %q, which the hook allows: %+v", command, about)
				}
				return
			}
			var named bool
			for _, c := range about {
				named = named || strings.Contains(said(c), rule)
			}
			if !named {
				t.Errorf("doctor does not report %q with the rule %s that refuses it: %+v", command, rule, got.Checks)
			}
		})
	}
	if refused < 5 || allowed < 5 {
		t.Fatalf("the table has %d refused and %d allowed commands, and tests nothing unless it has both", refused, allowed)
	}
}

// Doctor reads the command and never runs it: the command that is refused here
// would delete the file it names.
func TestAC3DoctorJudgesAConfiguredCommandWithoutRunningIt(t *testing.T) {
	t.Parallel()
	root := configuredWith(t, map[string]string{"tidy_up": "rm -f .sdlc/state/sentinel"})
	writeFile(t, root, ".sdlc/state/sentinel", "still here\n")

	got := decode[doctorPayload](t, run(t, "doctor", "--json"))
	if len(aboutRule(got, "loop-state-through-the-tool")) == 0 {
		t.Errorf("doctor did not report the command the hook refuses: %+v", got.Checks)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "sentinel")); err != nil {
		t.Errorf("doctor ran the configured command, which removed the file it names: %v", err)
	}
}

// Commands the hook allows are not a finding, and a healthy project's report
// is the report it was: the same checks, in the same order, all ok.
func TestAC3DoctorSaysNothingMoreOfAProjectWhoseCommandsTheHookAllows(t *testing.T) {
	t.Parallel()
	root := project(t)
	writeFile(t, root, "go.mod", "module example.com/x\n")
	mustRun(t, "init")
	var cfg config.Config
	wtReadJSON(t, filepath.Join(root, config.File), &cfg)
	// Shell builtins, so that no program has to be installed for the report
	// to be healthy, as TestDoctorSaysEverythingChecksOutOnACleanProject has it.
	cfg.Commands["lint"] = "true"
	cfg.Commands["coverage"] = "echo 100"
	wtWriteJSON(t, filepath.Join(root, config.File), cfg)
	name := "sdlc"
	if runtime.GOOS == "windows" {
		name = "sdlc.exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	setenv(t, "SDLC_BIN", bin)
	setenv(t, "HOME", t.TempDir())
	setenv(t, "LOCALAPPDATA", t.TempDir())

	r := mustRun(t, "doctor")
	if !strings.Contains(r.stdout, "Everything checks out.") {
		t.Errorf("stdout = %s", r.stdout)
	}
	var names []string
	for _, c := range decode[doctorPayload](t, mustRun(t, "doctor", "--json")).Checks {
		names = append(names, c.Name)
		if c.State != stateOK {
			t.Errorf("%s = %s (%s) in a healthy project", c.Name, c.State, c.Detail)
		}
	}
	want := "git repository,git command,configuration,backlog,loop state,project contract,commands,sdlc on PATH"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("the healthy report's checks are %s, want %s", got, want)
	}
}

// What init writes for a Go project is not a finding, whatever it is now: the
// story moves the coverage profile so that the command is one the hook allows,
// and doctor must agree. Programs missing from the machine are another
// check's business.
func TestAC3DoctorDoesNotReportTheCommandsInitWritesForAGoProject(t *testing.T) {
	t.Parallel()
	root := project(t)
	writeFile(t, root, "go.mod", "module example.com/x\n")
	mustRun(t, "init")
	var cfg config.Config
	wtReadJSON(t, filepath.Join(root, config.File), &cfg)
	if cfg.Commands["coverage"] == "" {
		t.Fatal("init wrote no coverage command for a Go project")
	}

	for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
		if c.State == stateProblem && c.Name != "commands on PATH" && c.Name != "sdlc on PATH" {
			t.Errorf("doctor reports %s on a project nothing was changed in: %s / %s", c.Name, c.Detail, c.Fix)
		}
		if c.State == stateProblem && strings.Contains(said(c), "-through-the-tool") {
			t.Errorf("doctor says the hook refuses a command init wrote: %s", said(c))
		}
	}
}
