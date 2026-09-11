package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/scaffold"
	"github.com/bbsnly/sdlc/internal/store"
)

// state is how one check came out.
type state string

const (
	stateOK      state = "ok"
	stateProblem state = "problem"
	stateSkipped state = "skipped"
)

// check is one thing doctor looked at. A problem always carries a fix: a check
// that only reports leaves the reader to guess, which is how a tool teaches
// people to ignore it.
type check struct {
	Name   string `json:"name"`
	State  state  `json:"state"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

type doctorPayload struct {
	OK       bool    `json:"ok"`
	Problems int     `json:"problems"`
	Checks   []check `json:"checks"`
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check this project and say how to fix what is wrong",
		Long: "doctor looks at the things that stop the loop working, and prints the\n" +
			"command that fixes each one.\n\n" +
			"It changes nothing. Run it after `sdlc init`, after editing\n" +
			".sdlc/config.json, or whenever something behaves in a way you did not\n" +
			"expect.",
		Example: "  sdlc doctor\n  sdlc doctor --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := runChecks()
			problems := 0
			for _, c := range checks {
				if c.State == stateProblem {
					problems++
				}
			}

			if wantJSON(cmd) {
				if err := emitJSON(cmd.OutOrStdout(), doctorPayload{
					OK: problems == 0, Problems: problems, Checks: checks,
				}); err != nil {
					return err
				}
			} else {
				printChecks(cmd.OutOrStdout(), checks, problems)
			}
			if problems > 0 {
				return quietExit{code: 1}
			}
			return nil
		},
	}
}

func printChecks(w io.Writer, checks []check, problems int) {
	fmt.Fprintln(w, "sdlc doctor")
	fmt.Fprintln(w)
	for _, c := range checks {
		fmt.Fprintf(w, "  %-8s %-20s %s\n", c.State, c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Fprintf(w, "           %-20s fix: %s\n", "", c.Fix)
		}
	}
	fmt.Fprintln(w)
	switch problems {
	case 0:
		fmt.Fprintln(w, "Everything checks out.")
	case 1:
		fmt.Fprintln(w, "1 problem. The fix is above it.")
	default:
		fmt.Fprintf(w, "%d problems. The fix is above each one.\n", problems)
	}
}

// runChecks walks from the outside in: each check assumes the ones before it
// passed, and says it was skipped rather than reporting a second failure with
// the same cause.
func runChecks() []check {
	var out []check
	add := func(c check) { out = append(out, c) }

	wd, err := os.Getwd()
	if err != nil {
		add(check{Name: "working directory", State: stateProblem,
			Detail: "the current directory could not be read",
			Fix:    "change to a directory that still exists"})
		return out
	}

	root, err := config.FindRoot(wd)
	if err != nil {
		add(check{Name: "git repository", State: stateProblem,
			Detail: "this is not inside a Git repository",
			Fix:    `run "git init", or change to a directory inside your repository`})
		return append(out, skipRest("git repository")...)
	}
	add(check{Name: "git repository", State: stateOK, Detail: root})
	add(gitCheck())

	cfg, err := config.Load(root)
	if err != nil {
		add(check{Name: "configuration", State: stateProblem,
			Detail: err.Error(),
			Fix:    `run "sdlc init" in ` + root})
		return append(out, skipRest("configuration")...)
	}
	stack, _ := scaffold.Detect(root)
	detail := config.File
	if stack.Name != "" {
		detail += "  (" + stack.Name + " project)"
	}
	add(check{Name: "configuration", State: stateOK, Detail: detail})

	project := &config.Project{Root: root, Config: cfg}
	add(backlogCheck(store.New(project), cfg.BacklogPath(root), root))
	add(contractCheck(root))
	out = append(out, commandChecks(cfg)...)
	add(binaryCheck())
	return out
}

// checkOrder is every check doctor makes, in the order it makes them. It is
// also what skipRest walks, so the report has the same shape whether or not it
// got all the way through.
var checkOrder = []string{
	"git repository", "git command", "configuration", "backlog",
	"project contract", "commands", "sdlc on PATH",
}

// skipRest reports the checks that could not run, so that nothing appears to
// have silently passed. One real problem should not produce a cascade of
// unrelated ones either.
func skipRest(after string) []check {
	var out []check
	seen := false
	for _, name := range checkOrder {
		if !seen {
			seen = name == after
			continue
		}
		out = append(out, check{Name: name, State: stateSkipped,
			Detail: "not checked: " + after + " has to work first"})
	}
	return out
}

func backlogCheck(s *store.Store, path, root string) check {
	backlog, err := s.Backlog()
	if err != nil {
		return check{Name: "backlog", State: stateProblem, Detail: err.Error(),
			Fix: `run "sdlc init" to create it, or point backlog.path at the file you use`}
	}
	if len(backlog.Stories) == 0 {
		return check{Name: "backlog", State: stateProblem,
			Detail: relativeTo(root, path) + " has no stories",
			Fix:    "add a story to it, or ask Claude Code for one with /sdlc:next"}
	}
	noun := "stories"
	if len(backlog.Stories) == 1 {
		noun = "story"
	}
	runnable := "none runnable"
	if _, ok := backlog.Next(); ok {
		runnable = "one runnable"
	}
	return check{Name: "backlog", State: stateOK, Detail: fmt.Sprintf("%s  (%d %s, %s)",
		relativeTo(root, path), len(backlog.Stories), noun, runnable)}
}

// gitCheck asks out loud for the thing two gates depend on. The freeze asks git
// which files are part of the working tree, and a review is stamped with the
// hash of what it reviewed -- both fail at the moment they are needed, which is
// the middle of a story, if git is not on the PATH.
func gitCheck() check {
	if _, err := exec.LookPath("git"); err != nil {
		return check{Name: "git command", State: stateProblem,
			Detail: "git is not on your PATH",
			Fix: "install git -- the test freeze and every review are recorded " +
				"against what git says is in the working tree"}
	}
	return check{Name: "git command", State: stateOK, Detail: "found on PATH"}
}

func contractCheck(root string) check {
	raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		return check{Name: "project contract", State: stateProblem,
			Detail: "there is no CLAUDE.md",
			Fix:    `run "sdlc init" -- it writes one without touching anything else`}
	}
	if !strings.Contains(string(raw), scaffold.ContractHeading) {
		return check{Name: "project contract", State: stateProblem,
			Detail: `CLAUDE.md has no "` + scaffold.ContractHeading + `" section`,
			Fix:    `run "sdlc init" -- it appends the section and leaves the rest alone`}
	}
	return check{Name: "project contract", State: stateOK,
		Detail: `CLAUDE.md carries the "` + scaffold.ContractHeading + `" section`}
}

// commandChecks looks for the programs the configured commands would run. It
// does not run them: a test suite can take minutes, and doctor is meant to be
// the fast answer to "why is this not working".
func commandChecks(cfg config.Config) []check {
	names := make([]string, 0, len(cfg.Commands))
	for name, command := range cfg.Commands {
		if strings.TrimSpace(command) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	if len(names) == 0 {
		return []check{{Name: "commands", State: stateProblem,
			Detail: "no commands are configured, so every gate that runs one is skipped",
			Fix:    "fill in commands.test and commands.lint in " + config.File}}
	}

	out := []check{{Name: "commands", State: stateOK, Detail: strings.Join(names, ", ")}}
	missing := map[string][]string{}
	for _, name := range names {
		for _, program := range programsIn(cfg.Commands[name]) {
			if _, err := exec.LookPath(program); err != nil {
				missing[program] = append(missing[program], name)
			}
		}
	}
	for _, program := range sortedKeys(missing) {
		out = append(out, check{
			Name:   "commands on PATH",
			State:  stateProblem,
			Detail: program + " is not installed, so " + strings.Join(missing[program], " and ") + " would fail",
			Fix: "install " + program + `, or clear commands.` + missing[program][0] +
				" in " + config.File,
		})
	}
	return out
}

// shellBuiltins never need to be on PATH.
var shellBuiltins = map[string]bool{
	"test": true, "[": true, "echo": true, "printf": true, "true": true,
	"false": true, "cd": true, ":": true, "exit": true, "read": true,
}

// programsIn pulls the program names out of a shell command. It splits on the
// operators that start a new command and takes the first word of each, which is
// enough for the commands people actually configure and never guesses at more.
func programsIn(command string) []string {
	// $( ) and backticks start a command of their own, and the interesting
	// program is often inside one: `test -z "$(gofmt -l .)"` runs gofmt.
	replacer := strings.NewReplacer(
		"&&", "\n", "||", "\n", "|", "\n", ";", "\n", "$(", "\n", "`", "\n")
	seen := map[string]bool{}
	var out []string
	for _, segment := range strings.Split(replacer.Replace(command), "\n") {
		fields := strings.Fields(segment)
		if len(fields) == 0 {
			continue
		}
		program := strings.TrimLeft(fields[0], "$(<>\"'")
		if program == "" || shellBuiltins[program] || strings.Contains(program, "=") {
			continue
		}
		if !seen[program] {
			seen[program] = true
			out = append(out, program)
		}
	}
	return out
}

// binaryCheck asks the question the hook asks. A hook that cannot find the
// binary allows the tool call and says so on stderr -- which is the right
// behaviour and easy to miss, so doctor asks it out loud.
func binaryCheck() check {
	path, err := exec.LookPath("sdlc")
	if err != nil {
		return check{Name: "sdlc on PATH", State: stateProblem,
			Detail: "the hooks cannot find sdlc, so nothing is enforced",
			Fix:    `put the sdlc binary on PATH -- "./task build" prints the line to use`}
	}
	return check{Name: "sdlc on PATH", State: stateOK, Detail: path}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}
