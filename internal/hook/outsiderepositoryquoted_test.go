package hook

// US-019, the second round of evasions, through the hook with real directories:
// what the reviews of the implementation found, or showed the tests do not
// hold. Every row is refused by the released tool and has to be refused after
// the story (the absolute-source copies of N2 are the exception: the released
// tool lets them through and the story closes them, on purpose). The rows that
// need no disk are also put to the policy in internal/shellpolicy.
//
// Not here: what is allowed and has to stay allowed (`cd <out>/copy && rm -f
// internal/x_test.go` with nothing quoted before it, a copy that lands
// outside), which outsiderepository_test.go holds.

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// orRepositoryTargets are what a relative name is trying to mean once the shell
// is in the repository after all: a frozen test, the loop's own record, a
// protected path, a new test, a find over everything, and a copy of an outside
// test into internal/.
var orRepositoryTargets = []orTarget{
	{"rm -f internal/x_test.go", orFrozenRule},
	{"printf x > .sdlc/state/active", orLoopStateRule},
	{"echo x > CLAUDE.md", orProtectedRule},
	{"echo x > internal/brand_new_test.go", "no-new-test-after-the-freeze"},
	{"find . -name '*_test*' -delete", orFrozenRule},
	{"cp {out}/flat/x_test.go internal/", ""},
}

// A separator inside quotes is not a separator, and the walk that follows a
// `cd` splits the command at every one, in quotes or out of them. If the walk
// follows a `cd` that the shell reads as text, the relative name after it is
// read from an outside directory when the shell runs it in the repository.
func TestAC1AndAC2ASeparatorInsideQuotesThenACdThenARelativeNameStaysRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	var rows []orEvasion
	for _, prefix := range []string{
		`echo 'a && cd {out}/copy' && `,
		`echo "a && cd {out}/copy" && `,
		`echo 'a; cd {out}/copy' && `,
		`echo 'a; cd {out}/copy;' && `,
		`echo "a; cd {out}/copy" && `,
		`echo 'a || cd {out}/copy' && `,
		`echo 'a | cd {out}/copy' && `,
		`echo 'a & cd {out}/copy' && `,
		`echo a '&&' cd {out}/copy '&&' echo b && `,
		`echo a "&&" cd {out}/copy && `,
		`echo a'&&'cd {out}/copy && `,
		`tee '{out}/a && cd {out}/copy' && `,
		`echo x > '{out}/a && cd {out}/copy' && `,
		`printf 'a && cd {out}/copy' && `,
		`touch '{out}/a && cd {out}/copy' && `,
		`echo 'a && cd {out}/copy && b' && `,
	} {
		for _, target := range orRepositoryTargets {
			rows = append(rows, orEvasion{"", prefix + target.command, target.rule})
		}
	}
	orRun(t, w, rows)
}

// A pipe is not a joiner. `cd` in a pipeline runs in a subshell and leaves the
// shell where it was, so what follows `&&` after it is read from the
// repository, whatever the `cd` named.
func TestAC1AndAC2APipeAsTheJoinerBeforeARelativeNameStaysRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	var rows []orEvasion
	for _, prefix := range []string{
		"cd {out}/copy | echo x && ",
		"cd {out} | cat && ",
		"true | cd {out}/flat && ",
	} {
		for _, target := range orRepositoryTargets[:3] {
			rows = append(rows, orEvasion{"", prefix + target.command, target.rule})
		}
	}
	orRun(t, w, rows)
}

// orDotted is a project whose directory name has a dot, and a directory
// outside it with copies of what a copy would bring into it. The old reader of
// a copy's destination took a name with a dot for a file and not a directory.
// r.x leads to the project, and top (no dot) too; in.x leads to its internal.
func orDotted(t *testing.T) orWorld {
	t.Helper()
	parent := wtTempDir(t)
	root := filepath.Join(parent, "my.app")
	write(t, root, ".sdlc/config.json", `{"version":1}`)
	write(t, root, ".sdlc/state/active", "A-1\n")
	write(t, root, ".sdlc/state/session", working+"\n")
	write(t, root, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	freeze(t, root, "A-1", "internal/x_test.go", "package x\n")
	write(t, root, "CLAUDE.md", "# contract\n")
	out := wtTempDir(t)
	write(t, out, "flat/x_test.go", "package x\n")
	write(t, out, "copy/CLAUDE.md", "# contract\n")
	write(t, out, "copy/user_stories.json", "{}\n")
	write(t, out, "copy/.sdlc/config.json", `{"version":1}`)
	write(t, out, "copy/.sdlc/state/active", "A-1\n")
	for name, target := range map[string]string{
		"r.x":  root,
		"top":  root,
		"in.x": filepath.Join(root, "internal"),
		"in":   filepath.Join(root, "internal"),
	} {
		orSymlink(t, target, filepath.Join(out, name))
	}
	return orWorld{root: root, out: out, parent: parent}
}

// A copy or a move whose destination is the repository is a write to what it
// lands on, and an outside source does not make it an outside write. The
// destination is a directory the reader did not take for one (it has a dot), or
// a link made earlier that leads to the repository. The absolute-source rows
// are the ones the released tool lets through and the story closes: the landing
// is what the write is, not the source's name.
func TestAC1AndAC2ACopyOrMoveOfAnOutsideFileLandingInTheRepositoryStaysRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orDotted(t)
	orRun(t, w, []orEvasion{
		// A relative source, from an outside copy of the project.
		{"{out}/copy", "cp CLAUDE.md {root}", orProtectedRule},
		{"{out}/copy", "mv CLAUDE.md {root}", orProtectedRule},
		{"{out}/copy", "cp -p user_stories.json {root}", orProtectedRule},
		{"{out}/copy", "cp -t {root} CLAUDE.md", orProtectedRule},
		{"{out}/copy", "cp CLAUDE.md user_stories.json {root}", orProtectedRule},
		{"{out}/copy", "cp CLAUDE.md {root}/", orProtectedRule},
		// The same onto a link made earlier, with a dot in its name and without.
		{"{out}/copy", "cp CLAUDE.md {out}/r.x", orProtectedRule},
		{"{out}/copy", "mv CLAUDE.md {out}/r.x", orProtectedRule},
		{"{out}/copy", "cp -p user_stories.json {out}/r.x", orProtectedRule},
		{"{out}/copy", "cp CLAUDE.md {out}/top", orProtectedRule},
		{"{out}/copy", "mv CLAUDE.md {out}/top", orProtectedRule},
		{"", "cd {out}/copy && cp CLAUDE.md {root}", orProtectedRule},
		{"", "cd {out}/copy && cp -p user_stories.json {out}/r.x", orProtectedRule},
		// An absolute source, from the repository.
		{"", "cp {out}/copy/CLAUDE.md {root}", orProtectedRule},
		{"", "mv {out}/copy/CLAUDE.md {root}", orProtectedRule},
		{"", "cp {out}/copy/CLAUDE.md {out}/r.x", orProtectedRule},
		{"", "cp -p {out}/copy/user_stories.json {root}", orProtectedRule},
		// Loop state, by where it lands.
		{"{out}/copy/.sdlc", "cp config.json {root}/.sdlc", orLoopStateRule},
		{"{out}/copy/.sdlc/state", "cp active {root}/.sdlc/state", orLoopStateRule},
		{"{out}/copy/.sdlc/state", "cp active {out}/r.x/.sdlc/state", orLoopStateRule},
		// A test, onto the repository's internal directory or a link to it.
		{"{out}/flat", "cp x_test.go {root}/internal", ""},
		{"{out}/flat", "cp x_test.go {out}/in.x", ""},
		{"{out}/flat", "mv x_test.go {out}/in.x", ""},
		{"{out}/flat", "cp x_test.go {out}/r.x/internal", ""},
		{"{out}/flat", "cp x_test.go {out}/in", ""},
		{"{out}/flat", "mv x_test.go {out}/in", ""},
		{"", "cp {out}/flat/x_test.go {out}/in.x", ""},
		{"", "mv {out}/flat/x_test.go {out}/in.x", ""},
		{"", "cp {out}/flat/x_test.go {out}/in", ""},
	})
}

// The fixes that are one clause each, and that nothing held: the clause that
// keeps `rm -r` out of a plain command, a word that starts with `=`, a tilde
// word read from an outside directory, `..` in a word through a link, and a
// glob in the operand of a `cd`.
func TestAC1AndAC2AClausesThatKeepAWordOutOfTheOutsideStayRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	orLinks(t, w)
	orSymlink(t, w.parent, filepath.Join(w.out, "up"))
	orRun(t, w, []orEvasion{
		// `rm -r` and `rm -rf` are not a plain `rm`, whatever `cd` came before.
		{"", "cd {out}/copy && rm -r internal/x_test.go", orFrozenRule},
		{"", "cd {out}/copy && rm -rf internal/x_test.go", orFrozenRule},
		// zsh reads a word that starts with `=` as the path of a program.
		{"{out}", "touch =x_test.go", ""},
		{"{out}", "rm -f =internal/x_test.go", orFrozenRule},
		// A tilde word from an outside directory is the user's, not that directory's.
		{"{out}", "rm -f ~nosuchuserzz/internal/x_test.go", orFrozenRule},
		{"{out}", "rm -f ~/internal/x_test.go", orFrozenRule},
		{"{out}", "echo x > ~nosuchuserzz/internal/brand_new_test.go", ""},
		// `..` after a link leaves the link's target, not the link's directory.
		{"", "rm -f {out}/in/../internal/x_test.go", orFrozenRule},
		{"{out}", "rm -f in/../internal/x_test.go", orFrozenRule},
		{"{out}", "rm -f flat/../in/x_test.go", orFrozenRule},
		// A glob in the operand of a `cd` leads to whichever directory it matches.
		{"", "cd {parent}/* && rm -f internal/x_test.go", orFrozenRule},
		{"", "cd {root}* && rm -f internal/x_test.go", orFrozenRule},
		{"", "cd {parent}/[0-9]* && printf x > .sdlc/state/active", orLoopStateRule},
		{"", "cd {parent}/* && echo x > CLAUDE.md", orProtectedRule},
		{"{parent}", "cd * && rm -f internal/x_test.go", orFrozenRule},
		{"{out}", "cd to* && rm -f internal/x_test.go", orFrozenRule},
		{"{out}", "cd i? && rm -f x_test.go", orFrozenRule},
		{"{out}", "cd u* && cd 0* && rm -f internal/x_test.go", orFrozenRule},
		{"{out}", "cd fl* && rm -f x_test.go", orFrozenRule},
		{"", "cd {out}/fl* && rm -f x_test.go", orFrozenRule},
		{"", "cd {out}/to* && printf x > .sdlc/state/active", orLoopStateRule},
		{"", "cd {out}/to* && echo x > CLAUDE.md", orProtectedRule},
		{"", "cd {out}/i* && echo x > brand_new_test.go", ""},
	})
}

// The single-quoted backslash. A plain command holds no backslash, and a
// backslash in single quotes is a letter of a file's name: `h\x_test.go` is a
// file in <out>/flat and not the path `h/x_test.go`. A reader that cleans the
// backslash into a slash asks the disk about a different path from the one the
// shell writes, finds nothing there, and lets the word go. The file by that name
// can be a link, or a second name, for a frozen test, made earlier by anything
// that was not looking at test names. Held with the name by itself, with a
// symbolic link of that name to the frozen test and with a hard link.
func TestAC1AndAC2ASingleQuotedBackslashNameWithATestLikeEndStaysRefusedByTheHook(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a backslash is a separator in a Windows path, so no file has one in its name")
	}
	w := orMake(t)
	frozen := filepath.Join(w.root, "internal", "x_test.go")
	orSymlink(t, frozen, filepath.Join(w.out, "flat", `h\x_test.go`))
	orHardLink(t, frozen, filepath.Join(w.out, "flat", `h\y_test.go`))
	var rows []orEvasion
	for _, name := range []string{`h\x_test.go`, `h\y_test.go`, `h\z_test.go`} {
		for _, command := range []string{
			`echo x > '{out}/flat/` + name + `'`,
			`printf x >> '{out}/flat/` + name + `'`,
			`touch '{out}/flat/` + name + `'`,
			`tee '{out}/flat/` + name + `'`,
		} {
			rows = append(rows, orEvasion{"", command, ""})
		}
		rows = append(rows, orEvasion{"{out}/flat", `echo x > '` + name + `'`, ""})
	}
	orRun(t, w, rows)
}

// AC-4: the three test rules say the same of a path written out in full that is
// not the repository's and was not placed outside it: the hook could not place
// it, which word, and which repository it measured against. A command that is
// not read whole is judged by name, and the refusal has to let a person read
// that it was the command and not the path. Not held: the sentence, only the
// word as written and the root, as the freeze rule's refusals are held in
// TestAC3AFrozenTestRefusalNamesTheWordOrRootAndTheRepository.
func TestAC4ARefusalOfAnOutsidePathInACommandThatIsNotPlainNamesTheWordAndTheRepository(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, agent := range orAgents {
		for _, c := range []struct{ command, word string }{
			{"echo x > {out}/flat/brand_new_test.go; true", "{out}/flat/brand_new_test.go"},
			{"cat > {out}/flat/brand_new_test.go <<EOF\nx\nEOF", "{out}/flat/brand_new_test.go"},
			{`echo "two words" > {out}/flat/brand_new_test.go`, "{out}/flat/brand_new_test.go"},
			{"mkdir -p {out}/n && touch {out}/n/brand_new_test.go", "{out}/n/brand_new_test.go"},
			{"echo x > {out}/flat/brand_new_test.go 2>&1", "{out}/flat/brand_new_test.go"},
		} {
			t.Run(agent+" "+c.command, func(t *testing.T) {
				r := w.ask(t, agent, "", c.command)
				got := reasonOf(r)
				if !denied(r) {
					t.Fatalf("allowed: %s", c.command)
				}
				if !strings.Contains(got, "[no-new-test-after-the-freeze]") && !strings.Contains(got, "[implementer-does-not-write-tests]") {
					t.Fatalf("not refused by a rule on a test that is not frozen:\n%s", got)
				}
				if word := w.words(c.word); !strings.Contains(got, word) {
					t.Errorf("the refusal does not name %q, the path it could not place:\n%s", word, got)
				}
				if !strings.Contains(got, slash(w.root)) && !strings.Contains(got, w.root) {
					t.Errorf("the refusal does not name the repository %s it measured against:\n%s", w.root, got)
				}
			})
		}
	}
}
