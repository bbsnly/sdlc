package shellpolicy

// US-019: the loop governs the repository and nothing else. A path the hook can
// place outside it is not the project's, whatever the name it ends in, so no
// shell rule refuses it for looking like a frozen test, a new test, a test the
// implementer may not write, or loop state. A word the hook cannot place is
// still judged by its name, and so is anything that reaches the repository by a
// link, a `..`, a variable or a glob. These tests put the words to Inspect
// with a lookup that stands for the hook's; the hook's own answers, with real
// directories and links, are in internal/hook, and the built binary's in
// test/e2e.
//
// The tests named AC1, AC2 and AC3 fail until the story is done. The ones named
// "StaysRefused" and "Unchanged" hold today and have to go on holding: this
// story removes refusals, and each of them is an evasion it was tried against.

import (
	"path"
	"slices"
	"strings"
	"testing"
)

// fileRules are the rules that read a word of a command as a file of the loop.
var fileRules = []string{
	"frozen-test-through-the-tool", "no-new-test-after-the-freeze",
	"implementer-does-not-write-tests",
	"loop-state-through-the-tool", "protected-path-through-the-tool",
}

const (
	orRoot    = "/work/project"
	orOutside = "/tmp/out"
)

// orLookup is State.Resolve for a project at /work/project, the way the hook's
// is: a path is cleaned, links are followed (those in links, by prefix), a path
// in the project is given repository-relative, a path anywhere else is "", and
// a relative word is the project's own, whatever directory the command is in.
func orLookup(links map[string]string) func(string) string {
	return func(word string) string {
		if !strings.HasPrefix(word, "/") {
			return path.Clean(word)
		}
		word = path.Clean(word)
		for from, to := range links {
			if rest, ok := strings.CutPrefix(word, from); ok && (rest == "" || rest[0] == '/') {
				return path.Clean(to + rest)
			}
		}
		switch rest, ok := strings.CutPrefix(word, orRoot); {
		case !ok:
			return ""
		case rest == "":
			return "."
		case rest[0] == '/':
			return rest[1:]
		}
		return ""
	}
}

// orState is a frozen project at /work/project: two frozen tests, any other
// *_test.go a new test, and the implementer's own rule where agent is
// "implementer".
func orState(agent, dir string, links map[string]string) State {
	frozen := []string{"internal/shellpolicy/abbreviated_test.go", "cmd/sdlc/main_test.go"}
	isTest := func(p string) bool { return strings.HasSuffix(p, "_test.go") }
	s := State{
		Frozen:  frozen,
		Dir:     dir,
		Agent:   agent,
		Resolve: orLookup(links),
		NewTest: func(p string) bool {
			if !isTest(p) {
				return false
			}
			return !slices.Contains(frozen, p)
		},
	}
	if agent == "implementer" {
		s.ImplementerTest = isTest
	}
	return s
}

// orWho is the two agents the rules tell apart: the main conversation, and the
// implementer, for whom every test-like name is refused.
var orWho = []string{"", "implementer"}

// orDirs are where the command starts: the repository's root, and a directory
// the lookup puts outside it, as the hook passes it (absolute) when the
// session's directory is not in the project.
var orDirs = []string{"", orOutside, orOutside + "/copy/internal/shellpolicy"}

// AC-1 and AC-2: a path placed outside the repository is allowed, whether the
// copy there is flat or keeps the repository's own path, for the main
// conversation and for the implementer, from the repository and from outside.
func TestAC1AndAC2APathPlacedOutsideTheRepositoryIsNotAFrozenTestOrANewTest(t *testing.T) {
	for _, agent := range orWho {
		for _, dir := range orDirs {
			s := orState(agent, dir, nil)
			for _, command := range []string{
				// A flat copy, and one that keeps the repository's own path.
				"rm -f /tmp/out/shellpolicy/*_test.go",
				"rm -f /tmp/out/shellpolicy/abbreviated_test.go",
				"rm -f /tmp/out/copy/internal/shellpolicy/*_test.go",
				"rm -f /tmp/out/copy/internal/shellpolicy/abbreviated_test.go",
				"rm -f /tmp/out/copy/cmd/sdlc/main_test.go",
				"echo x > /tmp/out/copy/internal/shellpolicy/abbreviated_test.go",
				"printf x >> /tmp/out/copy/cmd/sdlc/main_test.go",
				"sed -i s/a/b/ /tmp/out/copy/internal/shellpolicy/abbreviated_test.go",
				"tee /tmp/out/copy/internal/shellpolicy/abbreviated_test.go",
				"touch /tmp/out/copy/internal/shellpolicy/abbreviated_test.go",
				// A test the freeze does not hold, made outside.
				"echo x > /tmp/out/shellpolicy/brand_new_test.go",
				"touch /tmp/out/shellpolicy/brand_new_test.go",
				"cp /tmp/out/shellpolicy/abbreviated_test.go /tmp/out/shellpolicy/brand_new_test.go",
				// Cleaning up a scratch copy, and making one.
				"rm -rf /tmp/out/copy/internal",
				"cp -r internal/shellpolicy /tmp/out/shellpolicy",
			} {
				t.Run(agent+" from "+dir+" "+command, func(t *testing.T) {
					allowed(t, command, s)
				})
			}
		}
	}
}

// AC-1 and AC-2, the copy that makes a scratch copy in the first place (row O4d
// of the analysis). `cp` is read as writing every operand it is given, the
// source too, so a frozen test named only to be read is refused whatever the
// destination is. A copy that lands outside the repository only reads it; `mv`
// takes the source away, and stays refused, as does a copy whose destination is
// the repository's.
func TestAC1AndAC2CopyingAFrozenTestToAPathPlacedOutsideTheRepositoryIsNotRefused(t *testing.T) {
	for _, agent := range orWho {
		s := orState(agent, "", nil)
		for _, command := range []string{
			"cp internal/shellpolicy/abbreviated_test.go /tmp/out/shellpolicy/",
			"cp internal/shellpolicy/abbreviated_test.go /tmp/out/shellpolicy/abbreviated_test.go",
			"cp -p internal/shellpolicy/abbreviated_test.go /tmp/out/",
			"cp /work/project/internal/shellpolicy/abbreviated_test.go /tmp/out/",
			"cp -t /tmp/out internal/shellpolicy/abbreviated_test.go",
		} {
			t.Run(agent+" "+command, func(t *testing.T) {
				allowed(t, command, s)
			})
		}
		for _, command := range []string{
			"mv internal/shellpolicy/abbreviated_test.go /tmp/out/",
			"cp internal/shellpolicy/abbreviated_test.go internal/shellpolicy/brand_new_test.go",
			"cp internal/shellpolicy/abbreviated_test.go internal/shellpolicy/abbreviated_test.go.bak && mv internal/shellpolicy/abbreviated_test.go /tmp/out/",
		} {
			t.Run(agent+" stays refused "+command, func(t *testing.T) {
				if _, ok := Inspect(command, s); !ok {
					t.Errorf("allowed: %s", command)
				}
			})
		}
	}
}

// AC-1: a word that is relative is read from the directory the session is in.
// With that directory placed outside the repository, a name that equals a
// frozen path, or is the loop's state, is the outside directory's file and not
// the repository's. The loop-state row is the one change to that rule.
func TestAC1ARelativeNameInADirectoryPlacedOutsideIsNotTheRepositorys(t *testing.T) {
	for _, agent := range orWho {
		for _, c := range []struct{ dir, command string }{
			{orOutside + "/copy", "rm -f internal/shellpolicy/abbreviated_test.go"},
			{orOutside + "/copy", "echo x > internal/shellpolicy/abbreviated_test.go"},
			{orOutside + "/copy", "rm -f cmd/sdlc/main_test.go"},
			{orOutside + "/copy/internal/shellpolicy", "rm -f *_test.go"},
			{orOutside + "/copy/internal/shellpolicy", "rm -f abbreviated_test.go"},
			{orOutside + "/shellpolicy", "rm -f *_test.go"},
			{orOutside + "/shellpolicy", "echo x > brand_new_test.go"},
			{orOutside, "cd shellpolicy && rm -f *_test.go"},
			{"", "cd " + orOutside + "/shellpolicy && rm -f *_test.go"},
			{"", "cd " + orOutside + "/copy && rm -f internal/shellpolicy/abbreviated_test.go"},
			// The one change to the loop-state rule.
			{orOutside + "/copy", "printf x > .sdlc/state/active"},
			{orOutside + "/copy", "rm .sdlc/state/tests.lock"},
			{orOutside + "/copy", "printf x > CLAUDE.md"},
			// Written out in full it was never refused.
			{"", "printf x > " + orOutside + "/copy/.sdlc/state/active"},
			{"", "printf x > " + orOutside + "/copy/CLAUDE.md"},
		} {
			t.Run(agent+" from "+c.dir+" "+c.command, func(t *testing.T) {
				allowed(t, c.command, orState(agent, c.dir, nil))
			})
		}
	}
}

// The rest of the same decision, for words the rules took nothing from and
// still take nothing from: the cases that hold today.
func TestAC1APathOutsideTheRepositoryThatNoRuleReadsIsStillAllowedUnchanged(t *testing.T) {
	for _, agent := range orWho {
		for _, command := range []string{
			"echo x > /tmp/out/shellpolicy/notes.txt",
			"rm -f /tmp/out/shellpolicy/notes.txt",
			"printf x > /tmp/out/copy/.sdlc/state/active",
			"rm -f /tmp/out/copy/.sdlc/state/tests.lock",
			"printf x > /tmp/out/copy/CLAUDE.md",
			"cp /tmp/out/copy/CLAUDE.md /tmp/out/CLAUDE.md.bak",
		} {
			allowed(t, command, orState(agent, "", nil))
		}
	}
}

// AC-3: a frozen-test refusal of a word the hook cannot place says which word
// it objected to, as written. Not asserted: a whole sentence, or whether a
// frozen test named plainly inside the repository says it too.
func TestAC3AFrozenTestRefusalOfAWordItCannotPlaceNamesTheWord(t *testing.T) {
	for _, c := range []struct{ command, word string }{
		{`rm -f "$S/internal/shellpolicy/abbreviated_test.go"`, "$S/internal/shellpolicy/abbreviated_test.go"},
		{`echo x > "$S/cmd/sdlc/main_test.go"`, "$S/cmd/sdlc/main_test.go"},
		{`find "$S" -name '*_test*' -delete`, "$S"},
		{`find "$SCRATCH"/copy -name '*_test*' -delete`, "$SCRATCH"},
		{`find "$(mktemp -d)" -name '*_test*' -delete`, "mktemp"},
	} {
		t.Run(c.command, func(t *testing.T) {
			f := refused(t, c.command, orState("", "", nil), "frozen-test-through-the-tool")
			if !strings.Contains(f.Reason, c.word) {
				t.Errorf("the refusal does not name %q, the word it could not place:\n%s", c.word, f.Reason)
			}
			if !strings.Contains(f.Route, "sdlc unfreeze") {
				t.Errorf("the way on is no longer the unfreeze: %s", f.Route)
			}
		})
	}
}

// AC-3: a glob that the hook cannot place is refused for the freeze, and the
// refusal names the glob as written, not only a frozen path it expanded to.
func TestAC3AFrozenTestRefusalOfAGlobItCannotPlaceNamesTheGlob(t *testing.T) {
	for _, c := range []struct{ command, word string }{
		{`rm -f "$S"/internal/shellpolicy/*_test.go`, "$S"},
		{`rm -f /work/pro*/internal/shellpolicy/abbreviated_test.go`, "/work/pro*/internal/shellpolicy/abbreviated_test.go"},
		{`rm -f /{tmp,work/project}/internal/shellpolicy/abbreviated_test.go`, "/{tmp,work/project}/internal/shellpolicy/abbreviated_test.go"},
	} {
		t.Run(c.command, func(t *testing.T) {
			f := refused(t, c.command, orState("", "", nil), "frozen-test-through-the-tool")
			if !strings.Contains(f.Reason, c.word) {
				t.Errorf("the refusal does not name %q, the word it objected to:\n%s", c.word, f.Reason)
			}
		})
	}
}

// ---------------------------------------------------------------- the evasions

// A path that reaches the repository is the repository's, however it is
// spelled: through a link made earlier, a `..`, a variable, a glob or a brace in
// the directory part, a name tried from a directory the hook lost, or beside an
// outside path on the same line. Each of these is refused today, and removing
// the by-its-end match for words placed outside must not let one through.
func TestAC1WhatReachesTheRepositoryFromOutsideStaysRefused(t *testing.T) {
	links := map[string]string{"/tmp/link": "internal", "/tmp/rootlink": "."}
	const frozen, rule = "internal/shellpolicy/abbreviated_test.go", "frozen-test-through-the-tool"
	for _, agent := range orWho {
		for _, c := range []struct{ dir, command, rule string }{
			// The project, written from the root of the disk.
			{"", "rm -f /work/project/" + frozen, rule},
			{"", "rm -f /work/project/./" + frozen, rule},
			{"", "rm -f /work/project//" + frozen, rule},
			{orOutside, "rm -f /work/project/" + frozen, rule},
			{"", "rm -f /work/project/internal/shellpolicy/*_test.go", rule},
			// A link outside that leads in, made before.
			{"", "rm -f /tmp/link/shellpolicy/abbreviated_test.go", rule},
			{"", "rm -f /tmp/rootlink/" + frozen, rule},
			{"", "echo x > /tmp/link/shellpolicy/abbreviated_test.go", rule},
			{"/tmp/link", "rm -f shellpolicy/abbreviated_test.go", rule},
			{"/tmp/link/shellpolicy", "rm -f abbreviated_test.go", rule},
			{"/tmp/rootlink", "rm -f " + frozen, rule},
			{"/tmp/rootlink", "printf x > .sdlc/state/active", "loop-state-through-the-tool"},
			{"/tmp/rootlink", "printf x > CLAUDE.md", "protected-path-through-the-tool"},
			{"", "printf x > /tmp/rootlink/.sdlc/state/active", "loop-state-through-the-tool"},
			{"", "printf x > /tmp/rootlink/CLAUDE.md", "protected-path-through-the-tool"},
			{"/tmp/link", "echo x > brand_new_test.go", "no-new-test-after-the-freeze"},
			// `..` from an outside directory, and out and back in.
			{orOutside, "rm -f ../../work/project/" + frozen, rule},
			{"", "rm -f /tmp/out/../../work/project/" + frozen, rule},
			{"", "cd /tmp/out && rm -f ../../work/project/" + frozen, rule},
			{orOutside + "/copy", "rm -f ../../../work/project/" + frozen, rule},
			{"", "cd /tmp/out/copy/internal && rm -f ../../../../work/project/" + frozen, rule},
			{orOutside, "printf x > ../../work/project/.sdlc/state/active", "loop-state-through-the-tool"},
			// A variable, a substitution or the shell's own directory as the
			// directory part: not placeable, so not outside.
			{"", `R=$PWD; rm -f "$R/` + frozen + `"`, rule},
			{"", `rm -f "$(pwd)/` + frozen + `"`, rule},
			{"", "rm -f $PWD/" + frozen, rule},
			{"", `rm -f "$OLDPWD/` + frozen + `"`, rule},
			{"", "rm -f ~+/" + frozen, rule},
			{"", "rm -f ~-/" + frozen, rule},
			{"", "rm -f `pwd`/" + frozen, rule},
			{orOutside, `rm -f "$R/` + frozen + `"`, rule},
			{"", `printf x > "$R/.sdlc/state/active"`, "loop-state-through-the-tool"},
			{"", `printf x > "$(pwd)/CLAUDE.md"`, "protected-path-through-the-tool"},
			// A glob or a brace in the directory part of a path.
			{"", "rm -f /work/pro*/" + frozen, rule},
			{"", "rm -f /work/pro?ect/" + frozen, rule},
			{"", "rm -f /work/[p]roject/" + frozen, rule},
			{"", "rm -f /{tmp,work/project}/" + frozen, rule},
			{"", "rm -f /*/project/" + frozen, rule},
			{"", "rm -f /tmp/out/../*/project/" + frozen, rule},
			{"", "rm -f /work/project/internal/*/abbreviated_test.go", rule},
			// A directory the hook lost, then a name only the repository has.
			{orOutside, `cd "$D" && rm -f ` + frozen, rule},
			{orOutside, "cd - && rm -f " + frozen, rule},
			{orOutside, "popd && rm -f " + frozen, rule},
			{orOutside, "cd ~- && rm -f " + frozen, rule},
			{orOutside, `cd "$D" && printf x > .sdlc/state/active`, "loop-state-through-the-tool"},
			{"", `cd "$D" && rm -f ` + frozen, rule},
			// An outside path on the same line as one inside, or as a program
			// that names a frozen test bare.
			{"", "rm -f /tmp/out/a_test.go " + frozen, rule},
			{orOutside, "rm -f /tmp/out/a_test.go /work/project/" + frozen, rule},
			{"", "cp /tmp/out/a.go /tmp/out/b.go; rm -f " + frozen, rule},
			{"", "sed -i s/a/b/ /tmp/out/f.go " + frozen, rule},
			{"", `python3 -c 'import os; os.remove("` + frozen + `")' /tmp/out/notes.txt`, rule},
			{"", "echo /tmp/out/notes.txt | xargs rm " + frozen, rule},
		} {
			check := func(t *testing.T, command string, s State) {
				t.Helper()
				f, ok := Inspect(command, s)
				if !ok {
					t.Fatalf("allowed: %s", command)
				}
				// Which of the rules that read a file's name speaks first is
				// not what is held here -- a word placed in the repository by a
				// link is a new test as written and a frozen one once placed --
				// the refusal is, and that it is one of theirs.
				if !slices.Contains(fileRules, f.Rule) {
					t.Errorf("refused by %s, want one of %v (%s): %s", f.Rule, fileRules, c.rule, f.Reason)
				}
			}
			t.Run(agent+" from "+c.dir+" "+c.command, func(t *testing.T) {
				s := orState(agent, c.dir, links)
				check(t, c.command, s)
				// US-023 AC-5: the row again, for AGENTS.md.
				alsoRefusedAsAgents(t, c.command, s, []string{c.rule}, check)
			})
		}
	}
}

// The destination is the repository's when a copy, move, extract or download
// lands in it, wherever the source is; and a frozen test taken or linked out is
// still taken out of the repository. The sources here are not named like tests,
// so only the destination can speak.
func TestAC1ACopyOrMoveBetweenOutsideAndTheRepositoryStaysRefused(t *testing.T) {
	const frozen = "internal/shellpolicy/abbreviated_test.go"
	s := orState("", "", nil)
	for _, command := range []string{
		"cp /tmp/out/shellpolicy/other.go " + frozen,
		"mv /tmp/out/x.go " + frozen,
		"mv " + frozen + " /tmp/out/",
		"mv internal/shellpolicy /tmp/out/",
		"mv -t /tmp/out " + frozen,
		"install /tmp/out/x.go " + frozen,
		"curl -o " + frozen + " https://example.invalid/x",
		"wget -O " + frozen + " https://example.invalid/x",
		"rm -rf internal/shellpolicy",
		"rm -rf /work/project/internal",
		"ln -sf /tmp/out/x.go " + frozen,
	} {
		t.Run(command, func(t *testing.T) {
			refused(t, command, s, "frozen-test-through-the-tool")
		})
	}
}

// With the session's directory placed outside, a copy into the repository by
// an absolute path is still the repository's file.
func TestAC1ACopyIntoTheRepositoryFromAnOutsideDirectoryStaysRefused(t *testing.T) {
	s := orState("", orOutside, nil)
	for _, command := range []string{
		"cp a.go /work/project/internal/shellpolicy/abbreviated_test.go",
		"mv a.go /work/project/internal/shellpolicy/abbreviated_test.go",
		"echo x > /work/project/cmd/sdlc/main_test.go",
	} {
		t.Run(command, func(t *testing.T) {
			refused(t, command, s, "frozen-test-through-the-tool")
		})
	}
}

// AC-2: a file with a test's name copied from outside the repository over a
// frozen test is refused for where it lands. Today the outside source is
// refused first, as a new test added after the freeze, which is an outside
// path called the repository's; once it is let go, the destination still is the
// frozen test, under the freeze's own rule.
func TestAC2ACopyFromOutsideOverAFrozenTestIsRefusedForTheDestination(t *testing.T) {
	for _, c := range []struct{ dir, command string }{
		{"", "cp /tmp/out/shellpolicy/abbreviated_test.go internal/shellpolicy/abbreviated_test.go"},
		{"", "cp /tmp/out/abbreviated_test.go internal/shellpolicy/"},
		{"", "cp -t internal/shellpolicy /tmp/out/abbreviated_test.go"},
		{"", "rsync -a /tmp/out/copy/internal/shellpolicy/abbreviated_test.go internal/shellpolicy/"},
		{"", "mv /tmp/out/abbreviated_test.go internal/shellpolicy/"},
		{orOutside, "cp abbreviated_test.go /work/project/internal/shellpolicy/"},
		{orOutside, "cp /tmp/out/abbreviated_test.go /work/project/internal/shellpolicy/"},
		{orOutside, "rsync -a copy/internal/shellpolicy/abbreviated_test.go /work/project/internal/shellpolicy/"},
	} {
		t.Run("from "+c.dir+" "+c.command, func(t *testing.T) {
			f := refused(t, c.command, orState("", c.dir, nil), "frozen-test-through-the-tool")
			if strings.HasPrefix(f.Reason, "/tmp/out/") || strings.HasPrefix(f.Reason, "abbreviated_test.go ") {
				t.Errorf("the refusal calls the outside source the repository's file:\n%s", f.Reason)
			}
		})
	}
}

// The three refusals AC-3 does not change, word for word: a variable holding a
// path in a loop-state or protected path is judged by its name, with the text
// the released tool gave it. The page says why (docs/enforcement.md).
func TestAC3TheLoopStateAndProtectedPathRefusalsOfAnUnplacedWordAreUnchanged(t *testing.T) {
	const protectedWay = " and a shell command is the one way around the rule that protects it"
	const stateWay = " and a shell command is the one way around every rule that protects it"
	for _, c := range []struct {
		command, rule, reason, route string
	}{
		{
			`printf x > "$S/copy/.sdlc/state/active"`, "loop-state-through-the-tool",
			".sdlc/state is the loop's own record," + stateWay,
			"the sdlc command owns this: `sdlc artifact write` for a gate's documents, `sdlc review add` for a review, " +
				"`sdlc gate` for an outcome; the freeze is lifted by the person running the session",
		},
		{
			`printf x > "$S/CLAUDE.md"`, "protected-path-through-the-tool",
			"CLAUDE.md is protected while a story is being worked on," + protectedWay,
			"change it by hand outside a running iteration; if what it says is wrong, stop and say what rather than editing it",
		},
	} {
		for _, agent := range orWho {
			t.Run(agent+" "+c.command, func(t *testing.T) {
				f := refused(t, c.command, orState(agent, "", nil), c.rule)
				if f.Reason != c.reason || f.Route != c.route {
					t.Errorf("the refusal changed:\nreason %q\nroute  %q\nwant\nreason %q\nroute  %q", f.Reason, f.Route, c.reason, c.route)
				}
			})
		}
	}
}

// The choices that are a person's stay a person's, from a directory outside the
// repository as from inside it.
func TestAC1APersonsDecisionsAreRefusedFromADirectoryOutsideTheRepository(t *testing.T) {
	for _, dir := range orDirs {
		s := orState("", dir, nil)
		refused(t, "sdlc approve A-1", s, "approval-is-a-human-decision")
		refused(t, "sdlc unfreeze --reason x", s, "unfreeze-is-a-human-decision")
	}
}
