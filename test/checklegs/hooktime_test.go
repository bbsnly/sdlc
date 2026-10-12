package checklegs

// US-038, the leg: `./task hook-time`, its three budgets, its place in
// `./task check`, and the page that says what to do when it fails. The program
// the leg runs is tested beside it, in internal/tools/hooktime.
//
// Nothing here times anything. The leg's command is read from task's own dry
// run, which resolves the budgets as a real run does and runs nothing, so a
// figure in a comment or a task nobody calls does not pass for the leg; and
// the one real run is of a leg whose budget is 0, which the program refuses
// before it times a shape.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// The three budgets of AC-4, in milliseconds, by the variable that holds each
// in Taskfile.yml. AC-7 lets a person raise one after the leg has failed twice
// on a tree that did not change: that person changes the figure in
// Taskfile.yml and the figure here, in the one commit, and the reason goes in
// its message. Held here so that a raise is never a one-line edit nobody sees.
var hookBudgets = []struct {
	variable, flag, value string
}{
	{"HOOK_PERL_BUDGET_MS", "-perl", "3000"},
	{"HOOK_HERESTRING_BUDGET_MS", "-here-string", "1000"},
	{"HOOK_DOTGLOB_BUDGET_MS", "-dot-glob", "1000"},
}

const hookTimeProgram = "go run ./internal/tools/hooktime"

func liveTaskfile(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "Taskfile.yml"))
	if err != nil {
		t.Fatalf("reading Taskfile.yml: %v", err)
	}
	return strings.ReplaceAll(string(body), "\r\n", "\n")
}

// taskfileVars is the top-level `vars` block of a Taskfile: every name set at
// the block's own indentation, with the value as written. A name anywhere else
// in the file (a task's own vars, a comment) is not in it.
func taskfileVars(taskfile string) map[string]string {
	vars := map[string]string{}
	in := false
	entry := regexp.MustCompile(`^  ([A-Za-z_][A-Za-z0-9_]*):[ \t]*(.*?)[ \t]*$`)
	for line := range strings.SplitSeq(taskfile, "\n") {
		switch {
		case line == "vars:":
			in = true
		case in && line != "" && line[0] != ' ' && line[0] != '#':
			in = false
		case in:
			if m := entry.FindStringSubmatch(line); m != nil {
				vars[m[1]] = m[2]
			}
		}
	}
	return vars
}

// taskWith runs task against the Taskfile at taskfile, in this repository's
// root, as runTask does against a fixture: the pinned task, from the local
// module cache, with no network.
func taskWith(t *testing.T, taskfile string, args ...string) (string, error) {
	t.Helper()
	version := pinnedVersion(t, "TASK_VERSION")
	requireGoModule(t, "github.com/go-task/task/v3", version,
		"run any ./task command once with network access to fetch it")

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	argv := append([]string{"run", "github.com/go-task/task/v3/cmd/task@v" + version,
		"-d", repoRoot(t), "-t", taskfile}, args...)
	cmd := exec.CommandContext(ctx, "go", argv...)
	cmd.Env = append(os.Environ(), fileProxyEnv(t), "npm_config_offline=true")
	out, err := cmd.CombinedOutput()
	return strings.ReplaceAll(string(out), "\r\n", "\n"), err
}

// dryRun is what task would run for name, every variable resolved, with
// nothing run. --verbose because a silent task's commands are otherwise left
// out of a dry run.
func dryRun(t *testing.T, taskfile, name string) string {
	t.Helper()
	out, err := taskWith(t, taskfile, "--dry", "--verbose", name)
	if err != nil {
		t.Fatalf("task --dry %s: %v\n%s", name, err, out)
	}
	return out
}

// hookTimeCommands is every line of a dry run that runs the program.
func hookTimeCommands(dry string) []string {
	var out []string
	for line := range strings.SplitSeq(dry, "\n") {
		if strings.Contains(line, "./internal/tools/hooktime") {
			out = append(out, line)
		}
	}
	return out
}

// taskfileCopy is the live Taskfile with each named variable of its top-level
// block given another value, in a directory of the test's own.
func taskfileCopy(t *testing.T, values map[string]string) string {
	t.Helper()
	body := liveTaskfile(t)
	for name, value := range values {
		re := regexp.MustCompile(`(?m)^(  ` + regexp.QuoteMeta(name) + `:[ \t]*)\S+[ \t]*$`)
		if !re.MatchString(body) {
			t.Fatalf("Taskfile.yml sets no %s in its vars", name)
		}
		body = re.ReplaceAllString(body, "${1}"+value)
	}
	path := filepath.Join(t.TempDir(), "Taskfile.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAC4TheThreeBudgetsAreVarsBesideTheInitBudgetAtTheFiguresTheStoryGives(t *testing.T) {
	vars := taskfileVars(liveTaskfile(t))
	if _, ok := vars["INIT_BUDGET_MS"]; !ok {
		t.Fatal("this test no longer finds INIT_BUDGET_MS in the vars of Taskfile.yml, so it cannot say what is beside it")
	}
	for _, b := range hookBudgets {
		got, ok := vars[b.variable]
		switch {
		case !ok:
			t.Errorf("the vars of Taskfile.yml, where INIT_BUDGET_MS is, set no %s", b.variable)
		case got != b.value:
			t.Errorf("%s is %s, want %s. If a person raised it after the leg failed twice on a tree "+
				"that did not change, the figure in this test changes in the same commit", b.variable, got, b.value)
		}
	}
}

func TestAC4TheLegHandsTheProgramTheThreeFiguresFromTheVars(t *testing.T) {
	t.Run("AC-4 the live Taskfile", func(t *testing.T) {
		commands := hookTimeCommands(dryRun(t, filepath.Join(repoRoot(t), "Taskfile.yml"), "hook-time"))
		if len(commands) != 1 {
			t.Fatalf("the leg runs the program on %d lines, want one:\n%s", len(commands), strings.Join(commands, "\n"))
		}
		if !strings.Contains(commands[0], hookTimeProgram) {
			t.Errorf("the leg does not run %q: %q", hookTimeProgram, commands[0])
		}
		words := strings.Fields(commands[0])
		for _, b := range hookBudgets {
			if want := b.flag + "=" + b.value; !slices.Contains(words, want) {
				t.Errorf("the leg does not hand the program %s: %q", want, commands[0])
			}
		}
	})

	// A figure written into the command, with the vars beside it for show,
	// reads the same as one taken from them until the var changes.
	t.Run("AC-4 a Taskfile whose three vars say something else", func(t *testing.T) {
		other := map[string]string{
			"HOOK_PERL_BUDGET_MS": "4242", "HOOK_HERESTRING_BUDGET_MS": "1717", "HOOK_DOTGLOB_BUDGET_MS": "1313",
		}
		commands := hookTimeCommands(dryRun(t, taskfileCopy(t, other), "hook-time"))
		if len(commands) != 1 {
			t.Fatalf("the leg runs the program on %d lines, want one:\n%s", len(commands), strings.Join(commands, "\n"))
		}
		words := strings.Fields(commands[0])
		for _, b := range hookBudgets {
			if want := b.flag + "=" + other[b.variable]; !slices.Contains(words, want) {
				t.Errorf("with %s at %s the leg does not hand the program %s: %q",
					b.variable, other[b.variable], want, commands[0])
			}
		}
	})
}

func TestAC5CheckRunsTheLegWhereverItRunsInitTime(t *testing.T) {
	t.Run("AC-5 a dry run of check starts the leg as often as init-time", func(t *testing.T) {
		dry := dryRun(t, filepath.Join(repoRoot(t), "Taskfile.yml"), "check")
		initTime := strings.Count(dry, `task: "init-time" started`)
		hookTime := strings.Count(dry, `task: "hook-time" started`)
		if initTime == 0 {
			t.Fatalf("this test no longer finds init-time in a dry run of check:\n%s", dry)
		}
		if hookTime != initTime {
			t.Errorf("check starts init-time %d times and hook-time %d times", initTime, hookTime)
		}
		if got := len(hookTimeCommands(dry)); got != initTime {
			t.Errorf("check runs the program %d times and init-time %d times", got, initTime)
		}
	})

	// A dry run of check sees check. These see every other place that names
	// init-time as something to run.
	t.Run("AC-5 every task that calls init-time calls the leg", func(t *testing.T) {
		caller := func(leg string) *regexp.Regexp {
			return regexp.MustCompile(`(?m)^\s*-?\s*task:\s*` + leg + `\s*$`)
		}
		taskfile := liveTaskfile(t)
		initTime := len(caller("init-time").FindAllString(taskfile, -1))
		hookTime := len(caller("hook-time").FindAllString(taskfile, -1))
		if initTime == 0 {
			t.Fatal("this test no longer finds a task that calls init-time")
		}
		if hookTime != initTime {
			t.Errorf("Taskfile.yml calls init-time from %d places and hook-time from %d", initTime, hookTime)
		}
	})
	t.Run("AC-5 a workflow that names init-time names the leg", func(t *testing.T) {
		workflows, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "workflows", "*"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := taskWith(t, filepath.Join(repoRoot(t), "Taskfile.yml"), "--dry", "hook-time"); err != nil {
			t.Fatal("there is no leg hook-time for a workflow to name")
		}
		for _, path := range workflows {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "init-time") && !strings.Contains(string(body), "hook-time") {
				t.Errorf("%s runs init-time by name and not hook-time", filepath.Base(path))
			}
		}
	})
}

// The program is tested for refusing a budget it cannot use. This is the leg
// around it: that the refusal is the leg's exit, and is not swallowed by a
// pipe or read by the shell as a time of nothing, which is how init-time once
// passed on every Windows run. A budget of 0 is refused before a shape is
// timed, so this costs a build and no measurement.
func TestAC3TheLegExitsNonZeroWhenTheProgramDoes(t *testing.T) {
	taskfile := taskfileCopy(t, map[string]string{"HOOK_PERL_BUDGET_MS": "0"})
	if commands := hookTimeCommands(dryRun(t, taskfile, "hook-time")); len(commands) != 1 {
		t.Fatalf("the leg runs the program on %d lines, want one", len(commands))
	}
	out, err := taskWith(t, taskfile, "hook-time")
	if err == nil {
		t.Fatalf("the leg exits 0 with a perl budget of 0:\n%s", out)
	}
	if !strings.Contains(out, "perl") {
		t.Errorf("the leg's output does not carry the program's own words about the perl budget:\n%s", out)
	}
	for _, shape := range []string{"here-string", "dot-glob"} {
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), shape) {
				t.Errorf("the leg printed a line for %s though nothing was to be timed: %q", shape, line)
			}
		}
	}
}

func TestAC7ContributingSaysHowToRunTheLegWhatItTimesAndWhatToDoWhenItFails(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "CONTRIBUTING.md"))
	if err != nil {
		t.Fatal(err)
	}
	page := strings.ReplaceAll(string(raw), "\r\n", "\n")
	const heading = "\n## Running the checks\n"
	_, section, found := strings.Cut(page, heading)
	if !found {
		t.Fatal("CONTRIBUTING.md has no section \"Running the checks\"")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	at := strings.Index(section, "./task hook-time")
	if at < 0 {
		t.Fatal("\"Running the checks\" does not say how to run the leg: no `./task hook-time`")
	}
	// From the first mention on, with the line breaks of a wrapped paragraph
	// taken out.
	said := strings.Join(strings.Fields(section[at:]), " ")

	oneOf := func(t *testing.T, what string, spellings ...string) {
		t.Helper()
		for _, s := range spellings {
			if strings.Contains(said, s) {
				return
			}
		}
		t.Errorf("after `./task hook-time`, the section does not say %s (looked for %s)",
			what, strings.Join(spellings, " or "))
	}
	t.Run("AC-7 the three shapes, by the names the leg prints", func(t *testing.T) {
		oneOf(t, "that it times the perl shape", "perl")
		oneOf(t, "that it times the here-string shape", "here-string")
		oneOf(t, "that it times the dot-glob shape", "dot-glob")
	})
	t.Run("AC-7 their sizes", func(t *testing.T) {
		oneOf(t, "the size of the perl shape", "8,192", "8192")
		oneOf(t, "the size of the here-string shape", "8,190", "8190")
		oneOf(t, "the size of the dot-glob shape", "8,191", "8191")
	})
	t.Run("AC-7 on your own machine, run it again when the machine is quiet", func(t *testing.T) {
		oneOf(t, "to run it again when the machine is quiet", "quiet")
		oneOf(t, "to run it again", "again")
	})
	t.Run("AC-7 on a CI runner, run the job again", func(t *testing.T) {
		oneOf(t, "what to do on a CI runner", "CI")
		oneOf(t, "to run the job again", "job")
	})
	t.Run("AC-7 after two failures on an unchanged tree, a person raises the figure in Taskfile.yml", func(t *testing.T) {
		oneOf(t, "that it has to fail twice", "twice")
		oneOf(t, "that the tree did not change", "did not change", "has not changed", "unchanged")
		oneOf(t, "that a person does it", "a person")
		oneOf(t, "to raise the figure", "raise")
		oneOf(t, "where the figure is", "Taskfile.yml")
	})
	// The shapes are 8 KB each, and one of them is a removal: the page
	// describes them and does not carry them.
	t.Run("AC-7 the page describes the shapes and does not write them out", func(t *testing.T) {
		for line := range strings.SplitSeq(section, "\n") {
			if len(line) > 400 {
				t.Errorf("a line of the section is %d bytes long", len(line))
			}
		}
	})
}
