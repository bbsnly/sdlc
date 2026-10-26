// Command hooktime times the hook's classifier on three commands that are slow
// by construction, and fails when one takes longer than its budget. `./task
// hook-time` runs it, and `./task check` runs that.
//
// The hook gives itself 25 seconds and refuses a call it has not decided by
// then, so a classifier that gets slower refuses, for time, long commands it
// used to answer. `./task init-time` cannot see that: package init has run
// before a command is read. So the budget here is a ceiling on
// shellpolicy.Inspect at a named size, 8,192 bytes, because an everyday command
// takes microseconds and only a stated pathological one makes a ceiling
// measurable at all.
//
// It is a program and not a test because `./task test` runs every test under
// the race detector, where the three commands take two minutes and not nine
// seconds.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/testset"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, shellpolicy.Inspect, time.Now)) }

const (
	// size is the named input size: each command is the longest whole one of
	// its shape that is not over it. Nothing pads a command up to it, because
	// padding changes what the dot-glob command resolves to, and so its answer.
	size = 8192
	// runs is how often each command is timed. The fastest counts, as
	// init-time takes its floor: under load the median of an unchanged tree
	// went over budget.
	runs = 5
)

// shape is one command the leg times.
type shape struct {
	// name is what the leg prints, and the flag its budget arrives in.
	name string
	// variable is where Taskfile.yml keeps the budget, for the refusal of one
	// that cannot be read. It is not the name in capitals: the here-string
	// variable has no hyphen.
	variable   string
	command    string
	powerShell bool
	// refused is the answer the leg expects of Inspect. A time taken from
	// another answer is not a time of the same work: the fastest way to
	// classify a command is to stop reading it.
	refused bool
}

// shapes are the three commands, built here and handed to Inspect as a string.
// They are never run, and never written into a command line: one of them is a
// removal.
func shapes() []shape {
	const (
		perlHead, perlTail = "perl -", " file"
		block              = "$s = @'\nx\n'@\n"
		removeHead         = "rm "
		segment, glob      = "a/", ".*"
	)
	return []shape{
		{
			name: "perl", variable: "HOOK_PERL_BUDGET_MS",
			command: perlHead + strings.Repeat("a", size-len(perlHead)-len(perlTail)) + perlTail,
		},
		{
			name: "here-string", variable: "HOOK_HERESTRING_BUDGET_MS",
			command: strings.Repeat(block, size/len(block)), powerShell: true,
		},
		{
			name: "dot-glob", variable: "HOOK_DOTGLOB_BUDGET_MS",
			command: removeHead + strings.Repeat(segment, (size-len(removeHead)-len(glob))/len(segment)) + glob,
			refused: true,
		},
	}
}

// state is the State internal/hook builds in inspectShell for the implementer's
// command, in a project whose story holds a freeze and whose configuration is
// the default one, at the project root, in a repository with one checkout. dir
// stands for the project.
//
// It is a second copy, because the hook builds its own from the freeze, the
// story's record and git, none of which an empty directory has. Three things
// the hook does are left out. Its Resolve reads `~` and a drive letter first,
// and no word of the three commands begins with either. Its Checkouts lists
// the repository's checkouts, which none of the three commands asks for; nor
// does any of them ask the questions only a git command asks, which are
// answered here without git. And its Outside asks the disk about a path Resolve
// puts outside the project, in a command the hook reads whole, which no word of
// the three commands is: it is never called, and answers as the hook's does
// where it is in doubt.
func state(dir string, powerShell bool) shellpolicy.State {
	cfg := config.Default()
	frozen := []string{"internal/alpha/alpha_test.go", "internal/beta/beta_test.go", "test/e2e/gamma_test.go"}
	// As frozenTestsIn builds it: a test file the freeze does not hold is new.
	tests := testset.New(cfg.Paths.Tests)
	held := make(map[string]bool, len(frozen))
	for _, p := range frozen {
		held[pathrules.Fold(p)] = true
	}
	// Memoised as the hook's is, for the one command.
	resolver := pathrules.NewResolver(dir).Memoised()
	return shellpolicy.State{
		Frozen:          frozen,
		NewTest:         func(p string) bool { return tests.Match(p) && !held[pathrules.Fold(p)] },
		ImplementerTest: tests.Match,
		Agent:           "implementer",
		Backlog:         cfg.Backlog.Path,
		PowerShell:      powerShell,
		// The perl command asks this about 8,000 times, which is half its
		// time: without it the leg passes a classifier four times slower than
		// today's.
		Resolve: func(word string) string {
			// As onDisk does: a `..` is read where a link before it leads.
			if slices.Contains(strings.Split(filepath.ToSlash(word), "/"), "..") {
				if placed, ok := resolver.Place(filepath.FromSlash(word)); ok {
					word = placed
				}
			}
			rel, outside := resolver.Rel(filepath.FromSlash(word))
			if outside {
				return ""
			}
			return rel
		},
		Root:      filepath.ToSlash(dir),
		Outside:   func(string, bool) bool { return false },
		Trunk:     func() string { return cfg.Git.TrunkBranch },
		IsTrunk:   func(string) bool { return false },
		Fresh:     func() (bool, string) { return false, "" },
		Commits:   func() {},
		Nested:    func(string) bool { return false },
		Carried:   func(string) string { return "" },
		Checkouts: func() shellpolicy.CheckoutSet { return shellpolicy.CheckoutSet{} },
		// Stand-ins, as Outside and IsTrunk are: the hook asks the disk where
		// the home is, and this leg times the classifier that asks it.
		ClaudeConfig:     func(string) bool { return false },
		ClaudeConfigHeld: func(string) bool { return false },
		AgentsConfig:     func(string) bool { return false },
		AgentsConfigHeld: func(string) bool { return false },
		// State.WithDisk is left out: the hook's listings and archive reads
		// are disk I/O bounded by the caps in internal/hook/disk.go, not the
		// classifier's CPU this leg times.
	}
}

// run times each shape and returns the program's exit code: 0 when every shape
// was answered as the leg expects and its fastest run was inside its budget.
func run(args []string, stdout, stderr io.Writer,
	inspect func(string, shellpolicy.State) (shellpolicy.Finding, bool), now func() time.Time) int {
	list := shapes()
	budgets, ok := readBudgets(args, list, stderr)
	if !ok {
		return 1
	}
	// The program's own directory, not one the shell made and passed in: on
	// Windows the shell and Go read /tmp as two different places.
	dir, err := os.MkdirTemp("", "sdlc-hook-time-")
	if err != nil {
		fmt.Fprintf(stderr, "sdlc: hook-time could not make its temporary directory, so nothing was timed:\n"+
			"      %v\n"+
			"      Make sure the temporary directory exists and can be written to (TMPDIR, or\n"+
			"      TMP and TEMP on Windows), then run ./task hook-time again.\n", err)
		return 1
	}
	// Empty, so there is nothing to say if it will not go.
	defer os.RemoveAll(dir)
	// The budgets were set over a directory with its links resolved, as a
	// project ordinarily is; on macOS the temporary directory is behind one.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	// Each kind of failure is kept with the advice that belongs to it: with
	// one shape over and another answered differently, advice printed after
	// both sent the reader of one to the other's next step.
	var changed, over strings.Builder
	for i, t := range timeShapes(list, dir, inspect, now) {
		s := list[i]
		if t.other > 0 {
			fmt.Fprintf(&changed, "sdlc: shellpolicy.Inspect answered the %s shape as %s on %d of its %d runs; the leg expects it %s.\n",
				s.name, answer(!s.refused), t.other, runs, answer(s.refused))
			continue
		}
		// The first run beside the fastest, because the hook is a new process
		// for every command and pays for the first: a classifier that remembers
		// between calls would show here and nowhere else.
		fmt.Fprintf(stdout, "hook-time %s: %s ms (the fastest of %d runs; the first took %s ms), budget %d ms\n",
			s.name, millis(t.fastest), runs, millis(t.first), budgets[i])
		// On the duration and not on the figure printed, which is rounded.
		if t.fastest > time.Duration(budgets[i])*time.Millisecond {
			fmt.Fprintf(&over, "sdlc: the %s shape took %s ms (the fastest of %d runs), over its %d ms budget.\n",
				s.name, millis(t.fastest), runs, budgets[i])
		}
	}
	if changed.Len() > 0 {
		fmt.Fprint(stderr, changed.String(), changedAdvice)
	}
	if over.Len() > 0 {
		fmt.Fprint(stderr, over.String(), overAdvice)
	}
	if changed.Len()+over.Len() > 0 {
		return 1
	}
	return 0
}

const changedAdvice = "      The leg fails on this however fast the shape was: a time taken from another\n" +
	"      answer is not a time of the same work. The dot-glob shape is expected to be\n" +
	"      refused, and the perl and here-string shapes to be allowed.\n" +
	"      If it differed on every run, running the leg again will not change it: find\n" +
	"      what changed the answer in internal/shellpolicy. If nobody meant it to\n" +
	"      change, that is a defect there; if a story meant it, the answer the leg\n" +
	"      expects changes with that story, in internal/tools/hooktime.\n" +
	"      If it differed on only some of the runs, the answer depends on something\n" +
	"      besides the command (state, time or ordering): look for that.\n"

// A busy machine does not slow the three alike. The dot-glob command allocates
// most and leans on spare cores for its collections, so it goes over first, and
// the perl command rises least. Least is not little: the message once said perl
// hardly moves, and under heavier load its fastest of five was 2,419 ms and
// then 7,343 ms against about 1,100 quiet, which sent the reader of a perl line
// over budget to their own change.
const overAdvice = "      Run ./task hook-time again when this machine is quiet; on a CI runner, run\n" +
	"      the job again. A busy machine goes over on a tree that has not changed: the\n" +
	"      dot-glob shape rises first, then here-string, and perl least; under heavy\n" +
	"      load all three go over.\n" +
	"      To tell a busy machine from a change of yours, run ./task hook-time on trunk\n" +
	"      without the change (in a second checkout, or after git stash). Slow there\n" +
	"      too means the machine; fast there means the change.\n" +
	"      If it is the change, the classifier has become slower on that shape. The\n" +
	"      hook refuses a call it has not decided within 25 seconds, so a slower\n" +
	"      classifier refuses, for time, long commands it used to answer. Look for the\n" +
	"      cause in internal/shellpolicy, and in what internal/hook hands it.\n" +
	"      A green ./task hook-time is not a green ./task check: run that before you\n" +
	"      commit. CONTRIBUTING.md, under \"Running the checks\", says what a person\n" +
	"      does when the leg fails twice on a tree that did not change.\n"

// timing is what the runs of one shape came to.
type timing struct {
	first, fastest time.Duration
	// other is how many runs were not answered as the shape expects.
	other int
}

// timeShapes times each shape runs times, the three in turn and not one shape
// five times over: back to back, a burst of load two seconds long covered all
// five runs of a shape. A State is built for every call and outside the clock,
// because the hook starts each command with nothing remembered.
func timeShapes(list []shape, dir string,
	inspect func(string, shellpolicy.State) (shellpolicy.Finding, bool), now func() time.Time) []timing {
	out := make([]timing, len(list))
	for round := range runs {
		for i, s := range list {
			st := state(dir, s.powerShell)
			start := now()
			_, refused := inspect(s.command, st)
			took := now().Sub(start)
			t := &out[i]
			if refused != s.refused {
				t.other++
			}
			if round == 0 {
				t.first, t.fastest = took, took
			}
			t.fastest = min(t.fastest, took)
		}
	}
	return out
}

// readBudgets is the three budgets in milliseconds, in the order of list. A
// budget it cannot read is refused and nothing is timed: init-time once passed
// on every Windows run because what it could not read compared as 0 ms.
func readBudgets(args []string, list []shape, stderr io.Writer) ([]int64, bool) {
	flags := flag.NewFlagSet("hooktime", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	given := make([]*string, len(list))
	for i, s := range list {
		given[i] = flags.String(s.name, "", "")
	}
	problem := ""
	if err := flags.Parse(args); err != nil {
		problem = err.Error()
	} else if flags.NArg() > 0 {
		problem = "an argument that is not a budget: " + flags.Arg(0)
	}
	if problem != "" {
		fmt.Fprintf(stderr, "sdlc: hook-time was given something it does not take, so nothing was timed: %s\n"+
			"      It takes -perl=N, -here-string=N and -dot-glob=N and nothing else, each a\n"+
			"      whole number of milliseconds from 1 to 2147483647. Run it as ./task hook-time,\n"+
			"      which hands it the three figures from the vars of Taskfile.yml.\n", problem)
		return nil, false
	}
	budgets := make([]int64, len(list))
	ok := true
	for i, s := range list {
		// 32 bits, so that the budget as a time.Duration cannot wrap round to
		// one that nothing is over.
		n, err := strconv.ParseInt(*given[i], 10, 32)
		budgets[i] = n
		if err == nil && n > 0 {
			continue
		}
		ok = false
		what := fmt.Sprintf("was given %q as the %s budget", *given[i], s.name)
		if *given[i] == "" {
			what = "was not given a " + s.name + " budget"
		}
		fmt.Fprintf(stderr, "sdlc: hook-time %s, so nothing was timed.\n"+
			"      A budget is a whole number of milliseconds from 1 to 2147483647. The leg,\n"+
			"      ./task hook-time, hands this one over as -%s=N from %s\n"+
			"      in the vars of Taskfile.yml. Run the leg and not the program; if the leg\n"+
			"      says this, the figure there is missing or is not such a number.\n", what, s.name, s.variable)
	}
	return budgets, ok
}

func answer(refused bool) string {
	if refused {
		return "refused"
	}
	return "allowed"
}

// millis is d in milliseconds to the microsecond, with a full stop whatever the
// locale and no thousands separator.
func millis(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 3, 64)
}
