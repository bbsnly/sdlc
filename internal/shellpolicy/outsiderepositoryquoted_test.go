package shellpolicy

// US-019, the second round of evasions: what the reviews of the implementation
// found, or showed the tests do not hold. Every row is refused by the released
// tool and has to be refused after the story, for the reason the story sells:
// letting a path go is only safe where the command is read whole, and a command
// that is not read whole is judged by name. They are put here with the stub
// lookup of outsiderepository_test.go; the same rows with real directories are
// in internal/hook.
//
// Not here: what is allowed and has to stay allowed (`cd /tmp/out/copy && rm -f
// internal/shellpolicy/abbreviated_test.go` with nothing quoted before it, a
// copy that lands outside), which outsiderepository_test.go holds.

import (
	"strings"
	"testing"
)

// orNewTestRule is the rule that refuses a test file the freeze does not hold.
const orNewTestRule = "no-new-test-after-the-freeze"

// orRepositoryTargets are what a relative name is trying to mean once the
// shell is in the repository after all: a frozen test, the loop's own record, a
// protected path, a new test, a find over everything, and a copy of an outside
// test into internal/.
var orRepositoryTargets = []struct{ command, rule string }{
	{"rm -f " + orFrozenTest, orFrozenRule},
	{"printf x > .sdlc/state/active", "loop-state-through-the-tool"},
	{"echo x > CLAUDE.md", "protected-path-through-the-tool"},
	{"echo x > internal/shellpolicy/brand_new_test.go", orNewTestRule},
	{"find . -name '*_test*' -delete", orFrozenRule},
	{"cp /tmp/out/flat/x_test.go internal/shellpolicy/", ""},
}

// A separator inside quotes is not a separator, and the walk that follows a
// `cd` splits the command at every one, in quotes or out of them. If the walk
// is let to follow a `cd` that the shell reads as text, the relative name after
// it is read from an outside directory when the shell runs it in the
// repository. Each spelling is a `cd` the shell never makes.
func TestAC1AndAC2ASeparatorInsideQuotesThenACdThenARelativeNameStaysRefused(t *testing.T) {
	var rows []orEvasionRow
	for _, prefix := range []string{
		`echo 'a && cd /tmp/out/copy' && `,
		`echo "a && cd /tmp/out/copy" && `,
		`echo 'a; cd /tmp/out/copy' && `,
		`echo 'a; cd /tmp/out/copy;' && `,
		`echo "a; cd /tmp/out/copy" && `,
		`echo 'a || cd /tmp/out/copy' && `,
		`echo 'a | cd /tmp/out/copy' && `,
		`echo 'a & cd /tmp/out/copy' && `,
		`echo a '&&' cd /tmp/out/copy '&&' echo b && `,
		`echo a "&&" cd /tmp/out/copy && `,
		`echo a'&&'cd /tmp/out/copy && `,
		`tee '/tmp/out/a && cd /tmp/out/copy' && `,
		`echo x > '/tmp/out/a && cd /tmp/out/copy' && `,
		`printf 'a && cd /tmp/out/copy' && `,
		`touch '/tmp/out/a && cd /tmp/out/copy' && `,
		`echo 'a && cd /tmp/out/copy && b' && `,
	} {
		for _, target := range orRepositoryTargets {
			rows = append(rows, orEvasionRow{"", prefix + target.command, target.rule})
		}
	}
	orEvasionsRefused(t, nil, rows)
}

// A pipe is not a joiner. `cd` in a pipeline runs in a subshell and leaves the
// shell where it was, so what follows `&&` after it is read from the
// repository, whatever the `cd` named.
func TestAC1AndAC2APipeAsTheJoinerBeforeARelativeNameStaysRefused(t *testing.T) {
	var rows []orEvasionRow
	for _, prefix := range []string{
		"cd /tmp/out/copy | echo x && ",
		"cd /tmp/out | cat && ",
		"true | cd /tmp/out/flat && ",
	} {
		for _, target := range orRepositoryTargets[:3] {
			rows = append(rows, orEvasionRow{"", prefix + target.command, target.rule})
		}
	}
	orEvasionsRefused(t, nil, rows)
}

// orDottedRoot is a repository whose directory name has a dot, which the old
// reader of a copy's destination took for a file and not a directory.
const orDottedRoot = "/work/my.app"

// orDottedState is orState with the project at orDottedRoot, and links to it
// made earlier: one with a dot in its name, one without, and one to its
// internal directory.
func orDottedState(agent, dir string) State {
	s := orState(agent, dir, nil)
	links := map[string]string{"/tmp/out/r.x": ".", "/tmp/out/top": ".", "/tmp/out/in.x": "internal"}
	s.Resolve = func(word string) string {
		word = strings.TrimSuffix(word, "/")
		for from, to := range links {
			if rest, ok := strings.CutPrefix(word, from); ok && (rest == "" || rest[0] == '/') {
				return orRelative(to + rest)
			}
		}
		switch rest, ok := strings.CutPrefix(word, orDottedRoot); {
		case !ok:
			return ""
		case rest == "":
			return "."
		case rest[0] == '/':
			return rest[1:]
		}
		return ""
	}
	return s
}

// orRelative is a link's target with the project's own name for it: "." and
// "./x" are the project, and a deeper path is relative to it.
func orRelative(p string) string {
	if p == "." {
		return p
	}
	return strings.TrimPrefix(p, "./")
}

// A copy or a move whose destination is the repository is a write to what it
// lands on, and an outside source does not make it an outside write. The
// destination is a directory the reader did not take for one (it has a dot), or
// a link made earlier that leads to the repository.
func TestAC1AndAC2ACopyOrMoveOfAnOutsideFileLandingInTheRepositoryStaysRefused(t *testing.T) {
	const (
		protected = "protected-path-through-the-tool"
		state     = "loop-state-through-the-tool"
	)
	for _, agent := range orWho {
		for _, c := range []orEvasionRow{
			// A relative source, from an outside copy of the project.
			{orOutside + "/copy", "cp CLAUDE.md " + orDottedRoot, protected},
			{orOutside + "/copy", "mv CLAUDE.md " + orDottedRoot, protected},
			{orOutside + "/copy", "cp -p CLAUDE.md " + orDottedRoot, protected},
			{orOutside + "/copy", "cp -t " + orDottedRoot + " CLAUDE.md", protected},
			{orOutside + "/copy", "cp CLAUDE.md user_stories.json " + orDottedRoot, protected},
			{orOutside + "/copy", "cp CLAUDE.md " + orDottedRoot + "/", protected},
			// The same onto a link made earlier, with a dot in its name and without.
			{orOutside + "/copy", "cp CLAUDE.md /tmp/out/r.x", protected},
			{orOutside + "/copy", "mv CLAUDE.md /tmp/out/r.x", protected},
			{orOutside + "/copy", "cp -p CLAUDE.md /tmp/out/r.x", protected},
			{orOutside + "/copy", "cp CLAUDE.md /tmp/out/top", protected},
			{orOutside + "/copy", "mv CLAUDE.md /tmp/out/top", protected},
			{"", "cd /tmp/out/copy && cp CLAUDE.md " + orDottedRoot, protected},
			{"", "cd /tmp/out/copy && cp -p CLAUDE.md /tmp/out/r.x", protected},
			// An absolute source: the released tool lets these through, and the
			// story closes them, because the landing is what the write is.
			{"", "cp /tmp/out/copy/CLAUDE.md " + orDottedRoot, protected},
			{"", "mv /tmp/out/copy/CLAUDE.md " + orDottedRoot, protected},
			{"", "cp /tmp/out/copy/CLAUDE.md /tmp/out/r.x", protected},
			{"", "cp -p /tmp/out/copy/CLAUDE.md " + orDottedRoot, protected},
			// Loop state, by where it lands.
			{orOutside + "/copy/.sdlc", "cp config.json " + orDottedRoot + "/.sdlc", state},
			{orOutside + "/copy/.sdlc/state", "cp active " + orDottedRoot + "/.sdlc/state", state},
			{orOutside + "/copy/.sdlc/state", "cp active /tmp/out/r.x/.sdlc/state", state},
			// A test, onto the repository's internal directory or a link to it.
			{orOutside + "/flat", "cp x_test.go " + orDottedRoot + "/internal", ""},
			{orOutside + "/flat", "cp x_test.go /tmp/out/in.x", ""},
			{orOutside + "/flat", "mv x_test.go /tmp/out/in.x", ""},
			{orOutside + "/flat", "cp x_test.go /tmp/out/r.x/internal", ""},
			{"", "cp /tmp/out/flat/x_test.go /tmp/out/in.x", ""},
			{"", "mv /tmp/out/flat/x_test.go /tmp/out/in.x", ""},
		} {
			check := func(t *testing.T, command string, s State) {
				t.Helper()
				f, ok := Inspect(command, s)
				if !ok {
					t.Fatalf("allowed: %s", command)
				}
				if c.rule != "" && agent == "" && f.Rule != c.rule {
					t.Errorf("refused by %s, want %s: %s", f.Rule, c.rule, f.Reason)
				}
			}
			t.Run(agent+" from "+c.dir+" "+c.command, func(t *testing.T) {
				s := orDottedState(agent, c.dir)
				check(t, c.command, s)
				// US-023 AC-5: the row again, for AGENTS.md.
				alsoRefusedAsAgents(t, c.command, s, []string{c.rule}, check)
			})
		}
	}
}

// The fixes that are one clause each, and that nothing held: the clause that
// keeps `rm -r` out of a plain command, a word that starts with `=`, a tilde
// word read from an outside directory, `..` in a word through a link, and a
// glob in the operand of a `cd`.
func TestAC1AndAC2AClausesThatKeepAWordOutOfTheOutsideStayRefused(t *testing.T) {
	links := map[string]string{"/tmp/out/in": "internal"}
	const (
		name  = "abbreviated_test.go"
		state = "loop-state-through-the-tool"
	)
	orEvasionsRefused(t, links, []orEvasionRow{
		// `rm -r` and `rm -rf` are not a plain `rm`, whatever `cd` came before.
		{"", "cd /tmp/out/copy && rm -r " + orFrozenTest, orFrozenRule},
		{"", "cd /tmp/out/copy && rm -rf " + orFrozenTest, orFrozenRule},
		// zsh reads a word that starts with `=` as the path of a program.
		{orOutside, "touch =x_test.go", ""},
		{orOutside, "rm -f =" + orFrozenTest, orFrozenRule},
		// A tilde word from an outside directory is the user's, not that directory's.
		{orOutside, "rm -f ~nosuchuserzz/" + orFrozenTest, orFrozenRule},
		{orOutside, "rm -f ~/" + orFrozenTest, orFrozenRule},
		{orOutside, "echo x > ~nosuchuserzz/internal/shellpolicy/brand_new_test.go", orNewTestRule},
		// `..` after a link leaves the link's target, not the link's directory.
		{"", "rm -f /tmp/out/in/../" + orFrozenTest, orFrozenRule},
		{orOutside, "rm -f in/../" + orFrozenTest, orFrozenRule},
		{orOutside, "rm -f flat/../in/shellpolicy/" + name, orFrozenRule},
		// A glob in the operand of a `cd` leads to whichever directory it matches.
		{"", "cd /work/* && rm -f " + orFrozenTest, orFrozenRule},
		{"", "cd /work/project* && rm -f " + orFrozenTest, orFrozenRule},
		{"", "cd /work/[a-z]* && printf x > .sdlc/state/active", state},
		{"", "cd /work/* && echo x > CLAUDE.md", "protected-path-through-the-tool"},
		{"/work", "cd * && rm -f " + orFrozenTest, orFrozenRule},
		{orOutside, "cd to* && rm -f " + orFrozenTest, orFrozenRule},
		{orOutside, "cd fl* && rm -f " + name, orFrozenRule},
		{"", "cd /tmp/out/fl* && rm -f " + name, orFrozenRule},
		{"", "cd /tmp/out/to* && printf x > .sdlc/state/active", state},
		{"", "cd /tmp/out/i* && echo x > brand_new_test.go", orNewTestRule},
	})
}

// The single-quoted backslash. A plain command holds no backslash, and a
// backslash in single quotes is a letter of a file's name: `h\x_test.go` is a
// file in /tmp/out/flat and not the path `h/x_test.go`. A reader that cleans
// the backslash into a slash asks the disk about a different path from the one
// the shell writes, so the name is judged by its look, as a new test.
func TestAC1AndAC2ASingleQuotedBackslashNameWithATestLikeEndStaysRefused(t *testing.T) {
	var rows []orEvasionRow
	for _, command := range []string{
		`echo x > '/tmp/out/flat/h\x_test.go'`,
		`printf x >> '/tmp/out/flat/h\x_test.go'`,
		`touch '/tmp/out/flat/h\x_test.go'`,
		`tee '/tmp/out/flat/h\x_test.go'`,
		`echo x > '/tmp/out/flat/h\abbreviated_test.go'`,
		`touch '/tmp/out/flat/h\abbreviated_test.go'`,
		`tee '/tmp/out/flat/h\abbreviated_test.go'`,
	} {
		rows = append(rows, orEvasionRow{"", command, ""})
	}
	rows = append(rows, orEvasionRow{orOutside + "/flat", `echo x > 'h\x_test.go'`, ""})
	orEvasionsRefused(t, nil, rows)
}

// AC-4: the three test rules say the same thing of a path written out in full
// that is not the repository's and was not placed outside it: that the hook
// could not place it, and which word. A command that is not read whole is
// judged by name, and the refusal has to let a person see it was the command
// and not the path. Not held: the sentence, only the word as written.
func TestAC4ARefusalOfAnOutsidePathInACommandThatIsNotPlainNamesTheWordItCouldNotPlace(t *testing.T) {
	const word = "/tmp/out/flat/brand_new_test.go"
	for _, agent := range orWho {
		for _, command := range []string{
			"echo x > " + word + "; true",
			"cat > " + word + " <<EOF\nx\nEOF",
			`echo "two words" > ` + word,
			"mkdir -p /tmp/out/flat && touch " + word,
			"echo x > " + word + " 2>&1",
		} {
			t.Run(agent+" "+command, func(t *testing.T) {
				f, ok := Inspect(command, orState(agent, "", nil))
				if !ok {
					t.Fatalf("allowed: %s", command)
				}
				// Which of the two rules speaks first for the implementer is
				// not what is held; that it is one of them is.
				if f.Rule != orNewTestRule && f.Rule != "implementer-does-not-write-tests" {
					t.Errorf("refused by %s, want a rule on a test that is not frozen: %s", f.Rule, f.Reason)
				}
				if !strings.Contains(f.Reason, word) {
					t.Errorf("the refusal does not name %q, the path it could not place:\n%s", word, f.Reason)
				}
				// The released text never mentions the repository; the root it
				// measured against is the hook's to name (see internal/hook).
				if !strings.Contains(f.Reason, "repository") {
					t.Errorf("the refusal does not say what the path was measured against:\n%s", f.Reason)
				}
			})
		}
	}
}
