package cli

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
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

// vet runs what the freeze runs before it is taken, over the files it is about
// to hold: commands.freeze_check, and on a first freeze the narrow run of the
// new and changed tests (PLAN D8). The freeze is taken only where the check
// passes and the narrow run fails, as tests written before their code do, and
// neither changed a test file it was given. A test file the check creates is
// refused at the freeze too, since the second pass lists the tests again; one
// the narrow run alone creates is not, since that pass keeps the first
// listing, and from Gate 3 on the tests_frozen gate and each one after it
// refuse it as a test the freeze does not hold (SDLC-E0043). Its result is the
// result of the whole command.
//
// The project's lock is let go while they run, as it is for the landing's
// commands: a linter or a test run can take longer than any other command
// waits for the lock. So the world can move meanwhile, and nothing the first
// pass asked is trusted after it. The second pass is the whole freeze again
// from the top, with passed set so that it does not come back here.
func (f *freezer) vet(id string, extending bool, before map[string]string) error {
	// A check or a run that runs `sdlc freeze` would start them again with
	// the lock let go and no time limit, to any depth.
	if slices.ContainsFunc(os.Environ(), isFreezeFiles) || slices.ContainsFunc(os.Environ(), isRunDirVar) {
		return f.nested()
	}
	list, fellBack, err := f.newOrChanged(id, before)
	if err != nil {
		return err
	}
	narrow := f.narrow
	if extending || len(list) == 0 {
		// An addition to a freeze keeps the check alone (ANALYSIS 3.2), and
		// with nothing new or changed there is nothing to run.
		narrow = ""
	}
	if f.check == "" && narrow == "" {
		return f.frozenWithNothingToRun(id, before)
	}
	// One name to a line cannot carry a name with a line break in it, nor one
	// a reader such as Python's splitlines splits at: it would arrive as two
	// names, neither of them real.
	if bad := slices.DeleteFunc(slices.Clone(list), func(n string) bool { return !breaksALine(n) }); len(bad) > 0 {
		return f.lineInName(bad)
	}
	held, err := f.s.LockRun(id, store.RunFreeze)
	if err != nil {
		return err
	}
	defer held.Release()
	f.keys = f.runKeys(narrow)
	if err := f.runCommands(id, list, narrow, fellBack); err != nil {
		return err
	}
	if err := f.retake(before); err != nil {
		return err
	}
	f.passed, f.id, f.extending, f.judged = true, id, extending, before
	return f.take()
}

// frozenWithNothingToRun takes the freeze where neither the check nor the
// narrow run has anything to run.
func (f *freezer) frozenWithNothingToRun(id string, before map[string]string) error {
	if err := f.s.SaveLock(model.NewLock(id, before, f.s.Now())); err != nil {
		return err
	}
	return f.sayFrozen(id, slices.Sorted(maps.Keys(before)), nil)
}

// freezeCommands are the freeze's two commands, made before the lock is let
// go, and the narrow run's output file.
type freezeCommands struct {
	check, tests *oneCommand
	log          *outputFile
}

// runCommands runs the check and then the narrow run, with the project's
// lock let go, and keeps the narrow run's failure for the record.
func (f *freezer) runCommands(id string, list []string, narrow string, fellBack bool) error {
	ctx := f.cmd.Context()
	var tree string
	var err error
	if narrow != "" {
		if tree, err = f.s.ReviewSubject(ctx); err != nil {
			return err
		}
	}
	run, err := shellx.NewRunDir()
	if err != nil {
		return f.noRunDir(f.keys, "the run's directory", err)
	}
	defer func() { _ = run.Close() }()
	fc, err := f.prepareCommands(ctx, id, run, list, narrow)
	if err != nil {
		return err
	}
	if fc.log != nil {
		defer fc.log.discard()
	}

	f.lock.letGo()
	// A run that did not end as the freeze needs takes no lock back. The lock
	// was retaken here straight after the run once, and a Ctrl-C that landed on
	// that gap left it behind: the interrupt handler had already looked for a
	// lock held and found none, so the next write failed with SDLC-E0006 until
	// the lock went stale.
	if fc.check != nil {
		// How the check ended, not what it left holding its output: a pass
		// with a daemon behind it is still a pass.
		if ran := fc.check.run(ctx); ran.startErr != nil || ran.exit != 0 {
			return f.failed(ran.err, ran.printed)
		}
	}
	if fc.tests != nil {
		return f.runNarrow(ctx, id, fc, tree, fellBack)
	}
	return nil
}

// prepareCommands makes the freeze's commands while the lock is still held:
// with no shell nothing has been let go. The check gets one writer for both
// streams, so that they keep the order they were written in.
func (f *freezer) prepareCommands(ctx context.Context, id string, run *shellx.RunDir, list []string, narrow string) (freezeCommands, error) {
	var fc freezeCommands
	var err error
	if f.check != "" {
		if fc.check, err = prepareOne(ctx, f.s.Root(), "freeze_check", f.check, run, nil, freezeCheckBytes,
			freezeFilesVariable+"="+strings.Join(list, "\n")); err != nil {
			return fc, f.noShell(freezeCheckKey, err)
		}
		fc.check.inOrder()
	}
	if narrow == "" {
		return fc, nil
	}
	if err := run.ListTests(list); err != nil {
		return fc, f.noRunDir(commandsPrefix+f.narrowKey, "the list of test files it is handed as $"+shellx.TestFilesVar, err)
	}
	dir, err := f.s.StoryDir(id)
	if err != nil {
		return fc, err
	}
	if fc.log, err = openOutputFile(dir, freezeLog); err != nil {
		return fc, err
	}
	if fc.tests, err = prepareOne(ctx, f.s.Root(), f.narrowKey, narrow, run, fc.log, gateTailBytes); err != nil {
		fc.log.discard()
		return fc, f.noShell(commandsPrefix+f.narrowKey, err)
	}
	return fc, nil
}

// runNarrow runs the new and changed tests, and refuses where they pass or
// could not run: tests written before their code fail.
func (f *freezer) runNarrow(ctx context.Context, id string, fc freezeCommands, tree string, fellBack bool) error {
	key, log := commandsPrefix+f.narrowKey, fc.log
	fmt.Fprintf(f.cmd.ErrOrStderr(), "sdlc: running %s for %s's freeze in %s; its output is put in %s when the run ends\n",
		key, id, f.s.Root(), log.final)
	log.line("sdlc: " + key)
	ran := fc.tests.run(ctx)
	log.line(fmt.Sprintf("sdlc: %s, exit %d, %s", key, ran.exit, humanTime(ran.duration)))
	shown, said := log.place()
	switch {
	case ran.couldNotRun():
		return f.narrowCouldNotRun(key, ran, shown+said)
	case ran.exit == 0:
		return f.narrowPassed(key, ran, shown+said)
	}
	f.ran = &model.FreezeRun{CommandRun: model.CommandRun{Name: key, Exit: ran.exit,
		DurationMS: ran.duration.Milliseconds(), Digest: commandDigest(f.p.Config, f.narrowKey)}, TreeHash: tree}
	f.ranLog, f.fellBack = shown+said, fellBack
	return nil
}

// retake takes the project's lock back after the run, and refuses a check
// that changed a test file.
func (f *freezer) retake(before map[string]string) error {
	g, err := lockFresh(f.cmd.Context(), func() (*store.Guard, error) {
		return store.LockOn(f.s.LockRoot(), "freeze", envOf(f.cmd).lockClock)
	})
	if err != nil {
		return err
	}
	f.lock.release = g.Release
	if f.check == "" {
		return nil
	}
	changed, err := f.changedByCheck(before)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return f.changed(freezeCheckKey, changed)
	}
	return nil
}

// breaksALine reports a name that one name to a line cannot carry: a C0
// control character, a line feed and a carriage return among them, DEL, the
// next-line character U+0085, or the separators U+2028 and U+2029 (security
// F-8, red-team F-7). A tab is kept: no reader splits a line at it, and git
// allows it in a name the check has always been handed as it is.
func breaksALine(name string) bool {
	return strings.ContainsFunc(name, func(r rune) bool {
		return r < 0x20 && r != '\t' || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029
	})
}

// isFreezeFiles is whether an entry of the environment sets the variable the
// check is handed, compared as Windows compares names.
func isFreezeFiles(entry string) bool {
	name, _, _ := strings.Cut(entry, "=")
	return strings.EqualFold(name, freezeFilesVariable)
}

// newOrChanged is the files of before that are new or changed since the story's
// base, sorted: the cut it was branched from, else the base its record holds. A
// base git cannot read, or none, cannot say what changed, so every file is listed,
// and fellBack says so: the check sees more, never less.
func (f *freezer) newOrChanged(id string, before map[string]string) (list []string, fellBack bool, err error) {
	record, err := f.s.Record(id)
	if err != nil {
		return nil, false, err
	}
	names := slices.Sorted(maps.Keys(before))
	touched, known := touchedSince(f.cmd.Context(), f.s.Root(), cmp.Or(record.Cut, record.Base))
	if !known {
		return names, true, nil
	}
	return slices.DeleteFunc(names, func(n string) bool { return !slices.Contains(touched, n) }), false, nil
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
func refuse(what, why, fix string) *sdlcerr.Error {
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

// changed is a check or a run that left test files other than it found.
func (f *freezer) changed(key string, names []string) error {
	return refuse(key+" changed test files, so nothing was frozen",
		"it left "+countFiles(len(names))+" different from what it judged: "+quoteSome(names),
		`the test author looks at what the check changed: a change to a file not yet frozen is kept if it is `+
			`right, and "sdlc freeze" is run again, which judges the files as they now are; a change to a frozen `+
			`test is not kept, and the hook refuses the author's edit, so the author stops and a person restores `+
			`it or lifts the freeze. A check that rewrites a test file on every run cannot be cleared by `+
			"correcting tests, so a person changes "+freezeCheckKey+" in "+config.File)
}

func (f *freezer) lineInName(names []string) error {
	return refuse("a test file's name holds a line break or a character a reader splits lines at, so nothing was frozen",
		freezeFilesVariable+" and "+shellx.TestFilesVar+" list one name to a line, so "+quoteSome(names)+
			" would arrive as two names, neither of them real, and the freeze would hold a file the check or the run "+
			"never saw; a control character other than a tab, DEL, U+0085, U+2028 and U+2029 are refused",
		`rename it ("git mv") and run "sdlc freeze" again`)
}

// narrowPassed is a narrow run that passed: tests that pass before the code
// they test exists test nothing.
func (f *freezer) narrowPassed(key string, ran ranCommand, file string) error {
	return refuse(key+" passed over the new and changed tests, so nothing was frozen",
		"a test written before its code fails until the code exists, and these passed, so they test nothing yet; "+
			"everything it printed is in "+file+printedTail(&ran),
		`the test author makes each new test fail for the reason it is there, against the code as it is now, and runs `+
			`"sdlc freeze" again; nobody needs a person for this. Where `+key+` runs tests other than the ones listed in $`+
			shellx.TestFilesVar+`, a person corrects it in `+config.File)
}

// narrowCouldNotRun is a narrow run that says nothing of the tests: it never
// started, a signal ended it, or the shell answered 126 or 127.
func (f *freezer) narrowCouldNotRun(key string, ran ranCommand, file string) error {
	why := "it ended with status " + strconv.Itoa(ran.exit)
	if ran.startErr != nil {
		why = "it could not be started: " + ran.startErr.Error()
	}
	return refuse(key+" could not be run, so nothing was frozen",
		why+", which says nothing of the tests, so it is not taken for tests that fail; everything it printed is in "+
			file+printedTail(&ran),
		`a person puts the program, the shell or `+key+` in `+config.File+` right, and "sdlc freeze" is run again; `+
			`the test author does not change a test to clear it`)
}

// runKeys names the commands this freeze runs: the check, the narrow run, or
// both, in the order they run.
func (f *freezer) runKeys(narrow string) string {
	var keys []string
	if f.check != "" {
		keys = append(keys, freezeCheckKey)
	}
	if narrow != "" {
		keys = append(keys, commandsPrefix+f.narrowKey)
	}
	return strings.Join(keys, " and ")
}

// noShell is a command that could not be made because neither sh nor bash is
// on PATH, the one cause shellx.Command has.
func (f *freezer) noShell(key string, err error) error {
	return refuse(key+" could not be run, so nothing was frozen", err.Error(),
		`a person puts sh or bash on PATH (Git Bash on Windows), and "sdlc freeze" is run again`).WithCause(err)
}

// noRunDir is a run that could not be set up in the temporary directory,
// where its directory and the list of test files are written. Named as no
// shell on PATH, it sent a person to install one when the temporary directory
// was full or not writable (code review, code-reviewer F-1).
func (f *freezer) noRunDir(keys, what string, err error) error {
	return refuse(keys+" could not be run, so nothing was frozen",
		what+" could not be made in the temporary directory: "+err.Error(),
		`a person makes the temporary directory writable and frees room in it (TMPDIR names it, TMP or TEMP on `+
			`Windows), and "sdlc freeze" is run again; the test author does not change a test to clear it`).WithCause(err)
}

// moved is a freeze whose story or freeze changed while its commands ran.
func (f *freezer) moved(now string) error {
	return refuse(f.keys+" ran while the freeze moved, so nothing was frozen",
		"the run was for "+f.id+", and when it ended the story being worked on was "+now+
			"; if both are one story, the freeze that stood when it began has been lifted",
		`run "sdlc status" to see which story is being worked on and whether its tests are frozen, `+
			`and run "sdlc freeze" again only if that is the story meant`)
}

func (f *freezer) nested() error {
	return refuse("sdlc freeze would start a freeze inside a run of the commands, so nothing was frozen",
		freezeFilesVariable+" or "+shellx.RunDirVar+" is already set, which is what a command sdlc runs is given, "+
			"so this freeze is running inside one and would start the run again with the project's lock let go",
		`a person removes "sdlc freeze" from `+freezeCheckKey+`, the command run or the script it runs, and whoever exported `+
			freezeFilesVariable+" or "+shellx.RunDirVar+` unsets it; then "sdlc freeze" is run again`)
}
