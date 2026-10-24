package hook

// US-019, the evasions the design reviews found. Letting a path placed outside
// the repository go is only safe if everything that reaches the repository from
// outside stays the repository's, however it is spelled. These rows are put to
// the hook with real directories, real links and real hard links, and each of
// them is refused today and has to be refused after the story: they are the
// evidence that removing the refusals for an outside path opens nothing. A
// refusal is held by its decision and, where one rule is the rule the target
// belongs to, by its id; no sentence of it is held.
//
// What is not here, because the released tool allows it and this story does not
// change that: a link made and used in one command under a name that does not
// repeat the path it leads to, a reassigned HOME or a user that does not exist
// read for loop state or a protected path, the repository under another name
// (/System/Volumes/Data, /.vol) read for loop state or a protected path, a
// glob in the middle of a path read for loop state, and `rm -rf` over a glob.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// orFileRules are the rules that read a word of a command as a file of the loop.
var orFileRules = []string{
	"frozen-test-through-the-tool", "no-new-test-after-the-freeze",
	"implementer-does-not-write-tests",
	"loop-state-through-the-tool", "protected-path-through-the-tool",
}

// orEvasion is a command, the directory it runs from, and the rule it belongs
// to: "" is any of the rules that read a word as a file.
type orEvasion struct{ dir, command, rule string }

const (
	orFrozenRule    = "frozen-test-through-the-tool"
	orLoopStateRule = "loop-state-through-the-tool"
	orProtectedRule = "protected-path-through-the-tool"
)

// orTarget is what the row's first half is trying to make a relative name mean:
// a frozen test, the loop's own record, and a protected path.
type orTarget struct{ command, rule string }

var orTargets = []orTarget{
	{"rm -f internal/x_test.go", orFrozenRule},
	{"printf x > .sdlc/state/active", orLoopStateRule},
	{"echo x > CLAUDE.md", orProtectedRule},
}

// orRefusedAs holds a reply to the decision and, for the main conversation
// where one rule is the rule the target belongs to, to that rule. The
// implementer is refused by whichever of the rules that read a file speaks first.
func orRefusedAs(t *testing.T, r reply, agent, rule string) {
	t.Helper()
	got := reasonOf(r)
	if !denied(r) {
		t.Errorf("allowed: a spelling that reaches the repository from outside went through")
		return
	}
	if rule != "" && agent == "" {
		if !strings.Contains(got, "["+rule+"]") {
			t.Errorf("refused, but not by %s:\n%s", rule, got)
		}
		return
	}
	for _, id := range orFileRules {
		if strings.Contains(got, "["+id+"]") {
			return
		}
	}
	t.Errorf("refused, but not by a rule that reads a file:\n%s", got)
}

// orRun puts each row to the hook for both agents.
func orRun(t *testing.T, w orWorld, rows []orEvasion) {
	t.Helper()
	for _, agent := range orAgents {
		for _, row := range rows {
			t.Run(agent+" from "+row.dir+" "+row.command, func(t *testing.T) {
				orRefusedAs(t, w.ask(t, agent, row.dir, row.command), agent, row.rule)
			})
			// US-023 AC-5: the row again, for AGENTS.md.
			agAgentsMD(t, row.command, []string{w.root, w.out}, func(t *testing.T, respelt string) {
				t.Helper()
				orRefusedAs(t, w.ask(t, agent, row.dir, respelt), agent, row.rule)
			})
		}
	}
}

// orSymlink makes a link, or skips where one cannot be made.
func orSymlink(t *testing.T, target, at string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
}

// orHardLink gives the file at old a second name, or skips where that cannot be
// done.
func orHardLink(t *testing.T, old, at string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(old, at); err != nil {
		t.Skipf("hard links cannot be made here: %v", err)
	}
}

// ---------------------------------------------------------------- a cd that is not made

// A `cd` to a directory outside the repository that the shell does not make, or
// does not make for the command that follows, leaves the shell where it was. A
// relative name after it is then the repository's, whatever the directory the
// `cd` named: if the hook took the `cd` for made, a frozen test, the loop's own
// record or the contract could be written from outside.
func TestAC1AndAC2ACdTheShellDoesNotMakeThenARelativeNameStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	if err := os.MkdirAll(filepath.Join(w.out, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	var rows []orEvasion
	for _, prefix := range []string{
		// The directory is not there.
		"cd {out}/gone; ",
		"cd /nonexistent; ",
		// The directory is taken away or moved earlier in the same command.
		"rm -rf {out}/copy; cd {out}/copy; ",
		"rmdir {out}/empty; cd {out}/empty; ",
		"mv {out}/copy {out}/copy2; cd {out}/copy; ",
		// The `cd` is in a pipeline, in the background, or in a function that
		// is never called.
		"cd {out} | cat; ",
		"cd {out} & ",
		"f() { cd {out}; }; ",
		// The `cd` is in a branch that is not taken.
		"if false; then cd {out}; fi; ",
		"while false; do cd {out}; done; ",
		"false && cd {out}; ",
		"true || cd {out}; ",
		"test -d {out}/gone && cd {out}/gone; ",
		"! cd {out}; ",
		"(cd {out}); ",
		"sh -c 'cd {out}'; ",
		// What is not a `cd` that moves the shell.
		"pushd -n {out}; ",
		"cd {out} extra; ",
		"cd {out}/flat/notes.txt; ",
		"cd {out} 2>/dev/null; ",
		"cd {out}\n",
	} {
		for _, target := range orTargets {
			rows = append(rows, orEvasion{"", prefix + target.command, target.rule})
		}
	}
	orRun(t, w, rows)
}

// ---------------------------------------------------------------- a glob in a find's root

// A find rooted at a glob is rooted at everything the glob expands to, and the
// parent of the repository, or the repository's own name followed by a star,
// expands to the repository. Only a root written out is outside or not.
func TestAC1AndAC2AGlobAsTheLastSegmentOfAFindRootStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	orSymlink(t, w.parent, filepath.Join(w.out, "up"))
	var rows []orEvasion
	for _, action := range []string{"-name '*_test*' -delete", "-name active -delete"} {
		rows = append(rows,
			orEvasion{"", "find {parent}/* " + action, ""},
			orEvasion{"", "find {parent}/[0-9]* " + action, ""},
			orEvasion{"", "find {root}* " + action, ""},
			orEvasion{"{parent}", "find * " + action, ""},
		)
	}
	rows = append(rows,
		// The same root spelled without a star: a brace list, a pipe, an option
		// the find may follow a link with, and a link to the parent.
		orEvasion{"", "find {parent}/{{rootname},x} -name '*_test*' -delete", orFrozenRule},
		orEvasion{"", "echo {root} | xargs -I{} find {} -name '*_test*' -delete", orFrozenRule},
		orEvasion{"", "find {out} -follow -name '*_test*' -delete", ""},
		orEvasion{"", "find -L {out} -name '*_test*' -delete", ""},
		orEvasion{"", "find {out} -L -name '*_test*' -delete", ""},
		orEvasion{"", "find {out}/up -name '*_test*' -delete", orFrozenRule},
	)
	orRun(t, w, rows)
}

// ---------------------------------------------------------------- target-directory spellings

// A copy, move or install names the directory it puts files in with an option,
// and GNU tools take that option spelled out, abbreviated, bundled with others
// and with its operand attached. A source outside the repository that is let go
// must not be the way a frozen test is overwritten: each spelling lands the
// file on internal/x_test.go. Which rule speaks is not held: today it is the
// outside source, named as a new test, and afterwards it may be either.
func TestAC2ATargetDirectorySpellingCopyingAnOutsideSourceOverAFrozenTestStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	var rows []orEvasion
	for _, command := range []string{
		"cp --target=internal {out}/flat/x_test.go",
		"cp --t=internal {out}/flat/x_test.go",
		"cp --target internal {out}/flat/x_test.go",
		"cp --target-directory=internal {out}/flat/x_test.go",
		"cp -rtinternal {out}/flat/x_test.go",
		"cp -tinternal {out}/flat/x_test.go",
		"cp -Zt internal {out}/flat/x_test.go",
		"cp -vt internal {out}/flat/x_test.go",
		"cp -S .bak -t internal {out}/flat/x_test.go",
		"mv --target=internal {out}/flat/x_test.go",
		"mv --target-directory=internal {out}/flat/x_test.go",
		"mv -Zt internal {out}/flat/x_test.go",
		"mv -ft internal {out}/flat/x_test.go",
		"mv -ftinternal {out}/flat/x_test.go",
		"install --target=internal {out}/flat/x_test.go",
		"install -Ct internal {out}/flat/x_test.go",
		"install -m644 -t internal {out}/flat/x_test.go",
	} {
		rows = append(rows, orEvasion{"", command, ""})
	}
	orRun(t, w, rows)
}

// ---------------------------------------------------------------- a link made, or carried, in the command

// A link made in the command does not exist when the hook reads it, so a word
// through it reads as outside. Nothing may be taken for outside because of a
// command that could have made the link, however that command is spelled: a
// backslash, another program's name, a quoted name, a variable holding the name,
// or a tool that carries a link already on the disk to a second place.
func TestAC1AnAlternateSpellingOfALinkMakerThenAWriteThroughTheOutsideNameStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	orLinks(t, w)
	var rows []orEvasion
	for _, command := range []string{
		// Another spelling of `ln`.
		`\ln -s {root}/internal {out}/l && echo x > {out}/l/x_test.go`,
		`gln -s {root}/internal {out}/l && echo x > {out}/l/x_test.go`,
		`l''n -s {root}/internal {out}/l && echo x > {out}/l/x_test.go`,
		`L=ln; $L -s {root}/internal {out}/l; echo x > {out}/l/x_test.go`,
		// A tool that copies or moves a link that is already on the disk.
		`cp -a {out}/in {out}/l && echo x > {out}/l/x_test.go`,
		`cp -P {out}/in {out}/l && echo x > {out}/l/x_test.go`,
		`cp -rs {root}/internal {out}/l && echo x > {out}/l/x_test.go`,
		`cp --symbolic-link -r {root}/internal {out}/l && echo x > {out}/l/x_test.go`,
		`cp -al internal {out}/snap && echo x > {out}/snap/internal/x_test.go`,
		`rsync -a {out}/in {out}/l && echo x > {out}/l/x_test.go`,
		`tar -xf {out}/links.tar -C {out} && echo x > {out}/l/x_test.go`,
		`python3 -c 'import os; os.rename("{out}/in", "{out}/l")'; echo x > {out}/l/x_test.go`,
		`mv {out}/in {out}/l && echo x > {out}/l/x_test.go`,
		`mv {out}/flat {out}/flat.old && mv {out}/in {out}/flat && rm -f {out}/flat/x_test.go`,
		// A second name for the frozen test, made and then written through.
		`link {root}/internal/x_test.go {out}/flat/hl2_test.go && echo x > {out}/flat/hl2_test.go`,
	} {
		rows = append(rows, orEvasion{"", command, ""})
	}
	orRun(t, w, rows)
}

// ---------------------------------------------------------------- a tilde word

// A tilde word is a path the hook cannot place: HOME is the shell's to change in
// the command, a named user is looked up by the shell, and `~PWD` is a user's
// directory and not a variable. None of them is taken for outside.
func TestAC1AAReassignedHomeOrAUserTheHookCannotPlaceStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	rows := []orEvasion{
		{"", "export HOME={root} && rm -f ~/internal/x_test.go", orFrozenRule},
		{"", "export USERPROFILE={root} && rm -f ~/internal/x_test.go", orFrozenRule},
		{"", "HOME={root}; rm -f ~/internal/x_test.go", orFrozenRule},
		{"", "export HOME={root} && echo x > ~/internal/brand_new_test.go", ""},
		{"", "rm -f ~nosuchuserzz/internal/x_test.go", orFrozenRule},
		{"", "rm -f ~PWD/internal/x_test.go", orFrozenRule},
		{"", "rm -f ~OLDPWD/internal/x_test.go", orFrozenRule},
		{"", "R={root}; rm -f ~R/internal/x_test.go", orFrozenRule},
	}
	orRun(t, w, rows)
}

// ---------------------------------------------------------------- a relative `..` after a link

// `..` after a directory change through a link is the link's target's parent in
// a shell that resolves links first, and the link's parent in one that does not.
// A program that changes directory itself does the first. The hook cannot know
// which, so it takes neither for outside.
func TestAC1ARelativeDotDotAfterALinkStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	orLinks(t, w)
	var rows []orEvasion
	for _, command := range []string{
		"cd {out}/in && cd .. && rm -f internal/x_test.go",
		"cd {out}/in && env -C .. rm -f internal/x_test.go",
		"cd {out}/in && sudo -D .. rm -f internal/x_test.go",
		"env -C {out}/in rm -f ../internal/x_test.go",
		"set -P; cd {out}/in; cd ..; rm -f internal/x_test.go",
		"set -o physical; cd {out}/in; cd ..; rm -f internal/x_test.go",
		"set -w; cd {out}/in; cd ..; rm -f internal/x_test.go",
		"cd -P {out}/in && cd .. && rm -f internal/x_test.go",
		"cd -Pe {out}/in && cd .. && rm -f internal/x_test.go",
		"cd -LP {out}/in && cd .. && rm -f internal/x_test.go",
		"cd -L -P {out}/in && cd .. && rm -f internal/x_test.go",
		"builtin cd -P {out}/in && cd .. && rm -f internal/x_test.go",
		"env -C {out}/top rm -f internal/x_test.go",
		"env -C {out}/gone rm -f internal/x_test.go",
	} {
		rows = append(rows, orEvasion{"", command, orFrozenRule})
	}
	orRun(t, w, rows)
}

// ---------------------------------------------------------------- BSD sed

// BSD sed (macOS) reads the word after -i as a backup suffix and the next word
// as its script, and a script that ends in `w` followed by a file name writes
// that file. `sed -i s/a/b/ <out>/w<repo>/internal/x_test.go` is a file outside
// the repository to GNU sed and "write the repository's frozen test" to BSD sed.
func TestAC1AndAC2ABSDSedWriteCommandInTheFirstFileWordStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	orRun(t, w, []orEvasion{
		{"", "sed -i s/a/b/ {out}/w{root}/internal/x_test.go", orFrozenRule},
		{"{out}", "sed -i s/a/b/ w{root}/internal/x_test.go", orFrozenRule},
		{"", "sed -i s/a/b/ {out}/W{root}/internal/x_test.go", orFrozenRule},
	})
}

// ---------------------------------------------------------------- a second name

// A file outside the repository that is a second name of a frozen test, or of
// the loop's own record or the contract, is that file: writing through the name
// outside writes the repository's. The disk says so by the link count and the
// text of the path does not.
func TestAC1AndAC2AFileOutsideThatIsASecondNameOfAFrozenTestStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	state := filepath.Join(w.root, ".sdlc", "state")
	orHardLink(t, filepath.Join(w.root, "internal", "x_test.go"), filepath.Join(w.out, "flat", "hl_test.go"))
	orSymlink(t, filepath.Join(w.root, "internal", "x_test.go"), filepath.Join(w.out, "flat", "lnk"))
	// A snapshot made with `cp -al`: every loop file under its own name.
	orHardLink(t, filepath.Join(state, "active"), filepath.Join(w.out, "snap", ".sdlc", "state", "active"))
	orHardLink(t, filepath.Join(state, "tests.lock"), filepath.Join(w.out, "snap", ".sdlc", "state", "tests.lock"))
	orHardLink(t, filepath.Join(w.root, "CLAUDE.md"), filepath.Join(w.out, "snap", "CLAUDE.md"))
	orHardLink(t, filepath.Join(w.root, "internal", "x_test.go"), filepath.Join(w.out, "snap", "internal", "x_test.go"))
	// One loop file under another's name.
	orHardLink(t, filepath.Join(state, "active"), filepath.Join(w.out, "cross", ".sdlc", "state", "tests.lock"))
	orRun(t, w, []orEvasion{
		{"", "echo x > {out}/flat/hl_test.go", ""},
		{"", "printf x >> {out}/flat/hl_test.go", ""},
		{"", "tee {out}/flat/hl_test.go", ""},
		{"", "echo x > {out}/flat/lnk", ""},
		{"{out}/flat", "echo x > hl_test.go", ""},
		{"{out}/snap", "printf x > .sdlc/state/active", orLoopStateRule},
		{"{out}/snap", "printf x > .sdlc/state/tests.lock", orLoopStateRule},
		{"{out}/snap", "tee .sdlc/state/active", orLoopStateRule},
		{"{out}/snap", "echo x > CLAUDE.md", orProtectedRule},
		{"{out}/snap", "echo x > internal/x_test.go", ""},
		{"{out}/cross", "printf x > .sdlc/state/tests.lock", orLoopStateRule},
		{"", "echo x > {out}/snap/internal/x_test.go", ""},
	})
}

// ---------------------------------------------------------------- CDPATH

// A relative `cd` is read from the session's CDPATH when it carries one: `cd
// flat` goes to the repository's flat directory, though there is one beside the
// session's too.
func TestAC1ARelativeCdWithACDPATHCarriedStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	write(t, w.root, "flat/notes.txt", "notes\n")
	for _, agent := range orAgents {
		t.Run(agent, func(t *testing.T) {
			e := map[string]any{
				"hook_event_name": "PreToolUse", "session_id": working,
				"tool_name": "Bash", "cwd": w.out,
				"tool_input": map[string]string{"command": "cd flat && echo x > brand_new_test.go"},
			}
			if agent != "" {
				e["agent_type"] = agent
			}
			r := call(t, mustMarshal(e), env(map[string]string{
				"CLAUDE_PROJECT_DIR": w.root,
				"CDPATH":             w.root,
			}))
			orRefusedAs(t, r, agent, "")
		})
	}
}
