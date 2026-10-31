package cli

import (
	"cmp"
	"context"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellx"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	// freezeCheckKey is how a refusal and the record name the check. Its text can
	// hold a token, and a record is committed.
	freezeCheckKey = commandsPrefix + "freeze_check"
	// freezeFilesVariable is what the check is handed: the test files that are
	// new or changed, one to a line.
	freezeFilesVariable = "SDLC_FREEZE_FILES"
	// freezeCheckBytes bounds what a failed check puts into a refusal. A linter's
	// last forty lines are a few kilobytes; one line as long as a minified file
	// would otherwise fill the session.
	freezeCheckBytes = 16 << 10
	freezeCheckLines = 40
)

// vetting is whether this freeze still has a check to run: one is configured,
// and the freeze has not already been through it.
func (f *freezer) vetting() bool { return f.check != "" && !f.passed }

// vet runs commands.freeze_check over the files the freeze is about to hold and,
// if it passes and has left every test file as it found it, takes the freeze.
// Its result is the result of the whole command.
//
// The project's lock is let go while the check runs, as it is for the landing's
// commands: a linter can run for longer than any other command waits for the
// lock. So the world can move meanwhile, and nothing the first pass asked is
// trusted after it. The second pass is the whole freeze again from the top, with
// passed set so that it does not come back here.
func (f *freezer) vet(id string, extending bool, before map[string]string) error {
	// A check that runs `sdlc freeze` would start the check again with the
	// lock let go and no time limit, to any depth.
	if slices.ContainsFunc(os.Environ(), isFreezeFiles) {
		return f.nested()
	}
	list, err := f.newOrChanged(id, before)
	if err != nil {
		return err
	}
	// One name to a line cannot carry a name with a line in it: it would arrive
	// as two names, neither of them real.
	if bad := slices.DeleteFunc(slices.Clone(list), func(n string) bool { return !strings.Contains(n, "\n") }); len(bad) > 0 {
		return f.lineInName(bad)
	}
	// Made while the lock is still held: with no shell nothing has been let go.
	c, err := shellx.Command(f.cmd.Context(), f.s.Root(), f.check)
	if err != nil {
		return f.noShell(err)
	}
	shellx.InGroup(c)
	c.Env = gitx.Environ(freezeFilesVariable + "=" + strings.Join(list, "\n"))
	// One writer for both streams, so that they keep the order they were written in.
	tail := shellx.NewTail(freezeCheckBytes)
	c.Stdout, c.Stderr = tail, tail

	f.lock.letGo()
	runErr := shellx.Run(c)

	// How the check ended, not what it left holding its output: a pass with a
	// daemon behind it is still a pass. This relies on shellx.Run returning an
	// error for every end that is not exit 0, and for a start that failed, so
	// that failed can read runErr without a nil check.
	//
	// A check that did not pass takes no lock back. The lock was retaken here
	// straight after the run once, and a Ctrl-C that landed on that gap left it
	// behind: the interrupt handler had already looked for a lock held and found
	// none, so the next write failed with SDLC-E0006 until the lock went stale.
	if c.ProcessState == nil || c.ProcessState.ExitCode() != 0 {
		return f.failed(runErr, tail.String())
	}
	g, err := store.LockOn(f.s.LockRoot(), "freeze", envOf(f.cmd).lockClock)
	if err != nil {
		return err
	}
	f.lock.release = g.Release
	changed, err := f.changedByCheck(before)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return f.changed(changed)
	}
	f.passed, f.id, f.extending = true, id, extending
	return f.take()
}

// isFreezeFiles is whether an entry of the environment sets the variable the
// check is handed, compared as Windows compares names.
func isFreezeFiles(entry string) bool {
	name, _, _ := strings.Cut(entry, "=")
	return strings.EqualFold(name, freezeFilesVariable)
}

// newOrChanged is the files of before that are new or changed since the story's
// base, sorted: the cut it was branched from, else the base its record holds. A
// base git cannot read, or none, cannot say what changed, so every file is listed:
// the check sees more, never less.
func (f *freezer) newOrChanged(id string, before map[string]string) ([]string, error) {
	record, err := f.s.Record(id)
	if err != nil {
		return nil, err
	}
	names := slices.Sorted(maps.Keys(before))
	touched, known := touchedSince(f.cmd.Context(), f.s.Root(), cmp.Or(record.Cut, record.Base))
	if !known {
		return names, nil
	}
	return slices.DeleteFunc(names, func(n string) bool { return !slices.Contains(touched, n) }), nil
}

// touchedSince is every path committed since base, plus every one staged,
// changed in the working tree or not yet tracked, and whether git could say. A
// file deleted or renamed away is among them by its old name, which is no file
// the freeze holds.
func touchedSince(ctx context.Context, root, base string) ([]string, bool) {
	head, headErr := gitx.Head(ctx, root)
	changed, changedErr := gitx.ChangedBetween(ctx, root, base, head)
	dirty, dirtyErr := gitx.Uncommitted(ctx, root, true)
	return append(changed, dirty...), cmp.Or(headErr, changedErr, dirtyErr) == nil
}

// changedByCheck is each test file, of those the freeze was about to hold and
// those there are now, that the check rewrote, deleted or created. Judgement is
// by content: one it touched or put back is not changed.
func (f *freezer) changedByCheck(before map[string]string) ([]string, error) {
	now, err := findTests(f.cmd, f.p, f.s)
	if err != nil {
		return nil, err
	}
	names := slices.Concat(now, slices.Collect(maps.Keys(before)))
	slices.Sort(names)
	return slices.DeleteFunc(slices.Compact(names), func(name string) bool {
		sum, err := f.s.HashFile(name)
		return err == nil && sum == before[name]
	}), nil
}

// lastLines is the last n lines of text.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}

// ---------------------------------------------------------------- the refusals

// refuse is a refusal of the freeze check, whose code is the same for each state
// and whose fix is not: a test author whose file fails and a person without a
// shell are not told the same thing.
func refuse(what, why, fix string) error {
	return sdlcerr.New(sdlcerr.FreezeCheckRefused, what, why).WithFix(fix)
}

// failed is a check that did not pass. What it printed is set apart under a line
// of its own, after everything sdlc says, so that none of it can pass for the
// refusal's own words.
func (f *freezer) failed(runErr error, printed string) error {
	// A command that does not parse is quoted back by the shell, and its text
	// can hold a token. A one-word command is left alone: it would turn a file
	// name that contains it into a name that does not exist.
	if len(strings.Fields(f.check)) > 1 {
		printed = shellx.Tail(strings.ReplaceAll(printed, f.check, freezeCheckKey), freezeCheckBytes)
	}
	said := freezeCheckKey + " printed nothing"
	if printed != "" {
		said = freezeCheckKey + " printed (last 40 lines):\n" + setApart(lastLines(printed, freezeCheckLines))
	}
	return refuse(freezeCheckKey+" failed, so nothing was frozen",
		"it ended with "+runErr.Error()+"\n\n  "+said,
		`the test author corrects the files the check names and runs "sdlc freeze" again, and nobody needs a person `+
			`for this; but if the output says a program was not found or could not be started, the check fails `+
			`whatever the tests hold, or it fails only because a test names code that is not written yet `+
			`("undefined: X"), the author stops and does not change a test to clear it: a person installs the `+
			`program, settles the test, corrects `+freezeCheckKey+" in "+config.File+
			`, or shortens the list of test files if the output says the argument list is too long `+
			`("sdlc doctor" looks up the first word of each simple command as its scanner reads them, so it can `+
			`miss a program in a compound command, and it cannot see the programs a script calls)`)
}

// changed is a check that passed and left test files other than it found.
func (f *freezer) changed(names []string) error {
	return refuse(freezeCheckKey+" changed test files, so nothing was frozen",
		"it left "+countFiles(len(names))+" different from what it judged: "+quoteSome(names),
		`the test author looks at what the check changed: a change to a file not yet frozen is kept if it is `+
			`right, and "sdlc freeze" is run again, which judges the files as they now are; a change to a frozen `+
			`test is not kept, and the hook refuses the author's edit, so the author stops and a person restores `+
			`it or lifts the freeze. A check that rewrites a test file on every run cannot be cleared by `+
			"correcting tests, so a person changes "+freezeCheckKey+" in "+config.File)
}

func (f *freezer) lineInName(names []string) error {
	return refuse(freezeCheckKey+" cannot be handed a name with a line break in it, so nothing was frozen",
		freezeFilesVariable+" lists one name to a line, so "+quoteSome(names)+" would arrive as two names, "+
			"neither of them real, and the freeze would hold a file the check never saw",
		`rename it ("git mv") and run "sdlc freeze" again`)
}

func (f *freezer) noShell(err error) error {
	return refuse(freezeCheckKey+" could not be run, so nothing was frozen", err.Error(),
		`a person puts sh or bash on PATH (Git Bash on Windows), and "sdlc freeze" is run again`)
}

// moved is a freeze whose story or freeze changed while the check ran.
func (f *freezer) moved(now string) error {
	return refuse(freezeCheckKey+" ran while the freeze moved, so nothing was frozen",
		"the check ran for "+f.id+", and when it ended the story being worked on was "+now+
			"; if both are one story, the freeze that stood when it began has been lifted",
		`run "sdlc status" to see which story is being worked on and whether its tests are frozen, `+
			`and run "sdlc freeze" again only if that is the story meant`)
}

func (f *freezer) nested() error {
	return refuse(freezeCheckKey+" would start a freeze inside a freeze, so nothing was frozen",
		freezeFilesVariable+" is already set, which is what a check run by sdlc freeze is given, so this "+
			"freeze is running inside a check and would start the check again with the project's lock let go",
		`a person removes "sdlc freeze" from `+freezeCheckKey+" or the script it runs, and whoever exported "+
			freezeFilesVariable+` unsets it; then "sdlc freeze" is run again`)
}
