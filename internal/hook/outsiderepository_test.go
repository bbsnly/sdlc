package hook

// US-019: the loop governs the repository and nothing else. Put to the hook with
// real directories and real links, a path outside the repository is not refused
// by a shell rule for looking like a frozen test, a new test, a test the
// implementer may not write, or loop state; the file tools still refuse it, for
// being outside; and whatever reaches the repository -- through a link, a `..`,
// a variable, a glob, a find with another root, or another checkout of the same
// repository -- is still the repository's.
//
// The tests named AC1, AC2 and AC3 fail until the story is done. The ones named
// "StaysRefused" and "Unchanged" hold today and have to go on holding.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// orWorld is a frozen project, a directory outside it, and what each holds.
type orWorld struct {
	// root is the project, frozen at internal/x_test.go; out is a directory
	// that is not under it, holding a flat copy of a test (out/flat), a copy
	// that keeps the project's own path (out/copy/internal), and a copy of its
	// loop state (out/copy/.sdlc/state). parent is the directory both are in.
	root, out, parent string
}

func orMake(t *testing.T) orWorld {
	t.Helper()
	root := loopProject(t)
	freeze(t, root, "A-1", "internal/x_test.go", "package x\n")
	write(t, root, "CLAUDE.md", "# contract\n")
	out := wtTempDir(t)
	write(t, out, "flat/x_test.go", "package x\n")
	write(t, out, "flat/notes.txt", "notes\n")
	write(t, out, "copy/internal/x_test.go", "package x\n")
	write(t, out, "copy/.sdlc/state/active", "A-1\n")
	write(t, out, "copy/CLAUDE.md", "# contract\n")
	return orWorld{root: root, out: out, parent: filepath.Dir(root)}
}

// slash is a path as the shell is given it, on every platform.
func slash(p string) string { return filepath.ToSlash(p) }

// words are the placeholders a table of commands is written with.
func (w orWorld) words(command string) string {
	return strings.NewReplacer(
		"{root}", slash(w.root),
		"{out}", slash(w.out),
		"{parent}", slash(w.parent),
		"{rootname}", filepath.Base(w.root),
		"{outname}", filepath.Base(w.out),
		"{viaout}", slash(filepath.Join("..", filepath.Base(w.root))),
	).Replace(command)
}

// cwd is where a row's command runs from: "" is the project's root, and a
// path under {out} is written with the placeholder.
func (w orWorld) cwd(dir string) string {
	if dir == "" {
		return w.root
	}
	return filepath.FromSlash(w.words(dir))
}

// ask is the Bash call of agent ("" is the main conversation) from dir, in a
// session whose project is the loop's, as Claude Code sends it.
func (w orWorld) ask(t *testing.T, agent, dir, cmd string) reply {
	t.Helper()
	e := map[string]any{
		"hook_event_name": "PreToolUse", "session_id": working,
		"tool_name": "Bash", "cwd": w.cwd(dir),
		"tool_input": map[string]string{"command": w.words(cmd)},
	}
	if agent != "" {
		e["agent_type"] = agent
	}
	return call(t, mustMarshal(e), env(map[string]string{"CLAUDE_PROJECT_DIR": w.root}))
}

func reasonOf(r reply) string { return r.HookSpecificOutput.PermissionDecisionReason }

// orAgents are the two the rules tell apart.
var orAgents = []string{"", "sdlc:implementer"}

type orRow struct{ dir, command string }

// AC-1 and AC-2: through the built hook, a command acting on a file outside the
// repository whose name looks like a frozen test is not refused, however the
// copy there is shaped, for the main conversation and for the implementer, from
// a directory in the repository and from one outside it.
func TestAC1AndAC2AFrozenLookingNameOutsideTheRepositoryIsNotRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	rows := []orRow{
		// From the repository, naming the outside copy in full.
		{"", "rm -f {out}/flat/*_test.go"},
		{"", "rm -f {out}/flat/x_test.go"},
		{"", "rm -f {out}/copy/internal/*_test.go"},
		{"", "rm -f {out}/copy/internal/x_test.go"},
		{"", "echo x > {out}/copy/internal/x_test.go"},
		{"", "echo x > {out}/flat/brand_new_test.go"},
		{"", "touch {out}/flat/brand_new_test.go"},
		{"", "rm -rf {out}/copy/internal"},
		{"", "find {out}/flat -name '*_test*' -delete"},
		{"", "find {out}/copy -name '*_test*' -delete"},
		{"", "find {out}/copy -path '*/internal/x_test.go' -delete"},
		{"", "cd {out}/flat && find . -name '*_test*' -delete"},
		{"", "cd {out}/copy && rm -f internal/x_test.go"},
		{"", "cd {out}/flat && rm -f *_test.go"},
		// From a directory outside it, by a name relative to that directory.
		{"{out}", "rm -f flat/*_test.go"},
		{"{out}", "find flat -name '*_test*' -delete"},
		{"{out}", "find . -name '*_test*' -delete"},
		{"{out}/flat", "rm -f *_test.go"},
		{"{out}/flat", "echo x > brand_new_test.go"},
		{"{out}/flat", "find . -name '*_test*' -delete"},
		{"{out}/copy", "rm -f internal/x_test.go"},
		{"{out}/copy", "echo x > internal/x_test.go"},
		{"{out}/copy", "find . -name '*_test*' -delete"},
		{"{out}/copy/internal", "rm -f *_test.go"},
		{"{out}/copy/internal", "rm -f x_test.go"},
		{"{out}/copy/internal", "find . -name '*_test*' -delete"},
		// Loop state and the contract, from an outside directory: the one
		// change to that rule. Written out in full it was never refused.
		{"{out}/copy", "printf x > .sdlc/state/active"},
		{"{out}/copy", "rm .sdlc/state/active"},
		{"{out}/copy", "echo x > CLAUDE.md"},
		{"", "printf x > {out}/copy/.sdlc/state/active"},
		{"", "echo x > {out}/copy/CLAUDE.md"},
	}
	for _, agent := range orAgents {
		for _, row := range rows {
			t.Run(agent+" from "+row.dir+" "+row.command, func(t *testing.T) {
				if r := w.ask(t, agent, row.dir, row.command); denied(r) {
					t.Errorf("refused: %s", reasonOf(r))
				}
			})
			// US-023 AC-5: an AGENTS.md outside the repository is not its.
			agAgentsMD(t, row.command, []string{w.root, w.out}, func(t *testing.T, respelt string) {
				t.Helper()
				if r := w.ask(t, agent, row.dir, respelt); denied(r) {
					t.Errorf("refused: %s", reasonOf(r))
				}
			})
		}
	}
}

// AC-1 and AC-2, the copy that makes a scratch copy in the first place. Today
// `cp` is read as writing every operand it is given, the source included, so a
// frozen test named only to be read is refused whatever the destination is
// (that is the cause of the refusal the analysis could not trace, O4d). Once
// the copy lands outside the repository, the frozen test is only read, and the
// destination is not the repository's. `mv` takes its source away and stays
// refused; both are held below.
func TestAC1AndAC2CopyingAFrozenTestOutOfTheRepositoryIsNotRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, agent := range orAgents {
		for _, command := range []string{
			"cp internal/x_test.go {out}/flat/",
			"cp internal/x_test.go {out}/flat/x_test.go",
			"cp {root}/internal/x_test.go {out}/flat/x_test.go",
			"cp -p internal/x_test.go {out}/copy/internal/",
		} {
			if r := w.ask(t, agent, "", command); denied(r) {
				t.Errorf("%q by %q was refused: %s", command, agent, reasonOf(r))
			}
		}
	}
	for _, agent := range orAgents {
		for _, command := range []string{
			"mv internal/x_test.go {out}/flat/",
			"mv {root}/internal/x_test.go {out}/flat/x_test.go",
			"cp internal/x_test.go internal/y_test.go",
			"cp internal/x_test.go internal/x_test.go.bak && mv internal/x_test.go {out}/flat/",
		} {
			if r := w.ask(t, agent, "", command); !denied(r) {
				t.Errorf("%q by %q went through", command, agent)
			}
		}
	}
}

// What no rule refused before is still let through, so a test that only passes
// because everything is allowed is held by the refusals below it.
func TestAC1APathOutsideTheRepositoryThatNoRuleReadsIsStillAllowedUnchanged(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, agent := range orAgents {
		for _, command := range []string{
			"echo x > {out}/flat/notes.txt",
			"rm -f {out}/flat/notes.txt",
			"printf x > {out}/copy/.sdlc/state/active",
			"echo x > {out}/copy/CLAUDE.md",
			"find {out}/flat -name '*.txt' -delete",
		} {
			if r := w.ask(t, agent, "", command); denied(r) {
				t.Errorf("%q was refused: %s", command, reasonOf(r))
			}
		}
	}
}

// AC-1: the file tools refuse a path outside the repository, with the text and
// the rule they have always had, whatever agent asks and whatever the name:
// that is the one place an outside path is refused, and for the reason that it
// is outside.
func TestAC1TheWriteToolStillRefusesAnOutsidePathAsOutsideUnchanged(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	const reason = "this path is outside the repository, and the loop only governs what it can review"
	const route = "work inside the repository; if you need a file elsewhere, ask the user to create it"
	for _, agent := range []string{"", "sdlc:implementer", "sdlc:sdet"} {
		for _, tool := range []string{"Write", "Edit"} {
			for _, path := range []string{
				filepath.Join(w.out, "flat", "notes.txt"),
				filepath.Join(w.out, "flat", "x_test.go"),
				filepath.Join(w.out, "copy", "internal", "x_test.go"),
				filepath.Join(w.out, "copy", ".sdlc", "state", "active"),
				filepath.Join(w.out, "copy", "CLAUDE.md"),
			} {
				r := call(t, event(w.root, tool, agent, path), env(map[string]string{"CLAUDE_PROJECT_DIR": w.root}))
				got := reasonOf(r)
				if !denied(r) || !strings.Contains(got, "[write-outside-repository]") ||
					!strings.Contains(got, reason) || !strings.Contains(got, route) {
					t.Errorf("%s of %s by %q: %+v", tool, path, agent, r)
				}
			}
		}
	}
}

// ---------------------------------------------------------------- AC-3

// AC-3: where the frozen-test rule still refuses, the refusal names the word or
// root it objected to, as written, and the repository it measured it against:
// the checkout the session is held in, as the hook holds it. Not asserted: the
// sentence around them, or whether a frozen test named plainly inside the
// repository says them too.
func TestAC3AFrozenTestRefusalNamesTheWordOrRootAndTheRepository(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, c := range []struct{ dir, command, word string }{
		{"", `find "$S" -name '*_test*' -delete`, "$S"},
		{"{out}", `find "$S" -name '*_test*' -delete`, "$S"},
		{"", `find "$SCRATCH"/copy -name '*_test*' -delete`, "$SCRATCH"},
		{"", `rm -f "$S/internal/x_test.go"`, "$S/internal/x_test.go"},
		{"", `rm -f "$S"/internal/*_test.go`, "$S"},
		{"", `rm -f {parent}/*/internal/x_test.go`, "{parent}/*/internal/x_test.go"},
		{"", `rm -f {parent}/{{rootname},{outname}}/internal/x_test.go`, "{parent}/{{rootname},{outname}}/internal/x_test.go"},
	} {
		t.Run(c.dir+" "+c.command, func(t *testing.T) {
			r := w.ask(t, "", c.dir, c.command)
			if !denied(r) || !strings.Contains(reasonOf(r), "[frozen-test-through-the-tool]") {
				t.Fatalf("not refused under the freeze: %+v", r)
			}
			got := reasonOf(r)
			if word := w.words(c.word); !strings.Contains(got, word) {
				t.Errorf("the refusal does not name %q, the word it objected to:\n%s", word, got)
			}
			if !strings.Contains(got, slash(w.root)) && !strings.Contains(got, w.root) {
				t.Errorf("the refusal does not name the repository %s it measured against:\n%s", w.root, got)
			}
			if !strings.Contains(got, "sdlc unfreeze") {
				t.Errorf("the way on is no longer the unfreeze:\n%s", got)
			}
		})
	}
}

// AC-3: a find from the repository itself, or from a root above it, is refused
// naming the repository, and the root where the root has a name of its own.
func TestAC3AFindRootedInOrAboveTheRepositoryIsRefusedNamingTheRepository(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, c := range []struct{ dir, command string }{
		{"", "find . -name '*_test*' -delete"},
		{"", "find {root} -name '*_test*' -delete"},
		{"", "find .. -name '*_test*' -delete"},
		{"{out}", "find {root} -name '*_test*' -delete"},
		{"{out}", "find {parent} -name '*_test*' -delete"},
		{"", "find / -name '*_test*' -delete"},
		{"", "find {out} . -name '*_test*' -delete"},
		{"", "find . {out} -name '*_test*' -delete"},
	} {
		t.Run(c.dir+" "+c.command, func(t *testing.T) {
			r := w.ask(t, "", c.dir, c.command)
			if !denied(r) || !strings.Contains(reasonOf(r), "[frozen-test-through-the-tool]") {
				t.Fatalf("not refused under the freeze: %+v", r)
			}
			if got := reasonOf(r); !strings.Contains(got, slash(w.root)) && !strings.Contains(got, w.root) {
				t.Errorf("the refusal does not name the repository %s it measured against:\n%s", w.root, got)
			}
		})
	}
}

// AC-3: the root named is the checkout the session is held in. A session held
// in a story's worktree is told that worktree's root, and not the other
// story's, which it has no reason to be told.
func TestAC3TheRepositoryNamedIsTheCheckoutTheSessionIsHeldIn(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	o := swOpenings(l)[0]
	r := swCall(t, swHeld, swBash("find from a variable", o, `find "$S" -name '*_test*' -delete`), nil)
	got := swReason(r)
	if !denied(r) || !strings.Contains(got, "[frozen-test-through-the-tool]") {
		t.Fatalf("not refused under the freeze: %+v", r)
	}
	if !strings.Contains(got, slash(l.wt)) && !strings.Contains(got, l.wt) {
		t.Errorf("the refusal does not name the checkout the session is held in, %s:\n%s", l.wt, got)
	}
	if strings.Contains(got, l.other) || strings.Contains(got, "B-2") {
		t.Errorf("the refusal names another story's checkout:\n%s", got)
	}
}

// AC-3: the word is echoed as written and bounded, so a refusal never prints
// more than the model itself wrote and never the value of a variable the hook
// did not resolve.
func TestAC3AWordTheRefusalNamesIsPrintedAsWrittenAndBounded(t *testing.T) {
	w := orMake(t)
	long := strings.Repeat("a", 20000)
	r := w.ask(t, "", "", `rm -f "$S/`+long+`/internal/x_test.go"`)
	if !denied(r) || !strings.Contains(reasonOf(r), "[frozen-test-through-the-tool]") {
		t.Fatalf("not refused under the freeze: %+v", r)
	}
	if n := len(reasonOf(r)); n > 8000 {
		t.Errorf("the refusal is %d bytes for a word of %d", n, len(long))
	}
	t.Setenv("S", "/value/of/the/variable/the/hook/did/not/resolve")
	r = w.ask(t, "", "", `rm -f "$S/internal/x_test.go"`)
	if strings.Contains(reasonOf(r), "/value/of/the/variable") {
		t.Errorf("the refusal prints the value of a variable the hook did not resolve:\n%s", reasonOf(r))
	}
}

// AC-1 and AC-3: a word the hook cannot place is judged by its name, with the
// text it has always had, for loop state and for a protected path.
func TestAC3TheLoopStateAndProtectedPathRefusalsOfAnUnplacedWordAreUnchanged(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, agent := range orAgents {
		r := w.ask(t, agent, "", `printf x > "$S/copy/.sdlc/state/active"`)
		want := ".sdlc/state is the loop's own record, and a shell command is the one way around every rule that protects it. " +
			"Instead: the sdlc command owns this: `sdlc artifact write` for a gate's documents, `sdlc review add` for a review, " +
			"`sdlc gate` for an outcome; the freeze is lifted by the person running the session [loop-state-through-the-tool]"
		if !denied(r) || reasonOf(r) != want {
			t.Errorf("%q: loop state through $S changed:\n%s\nwant\n%s", agent, reasonOf(r), want)
		}
		r = w.ask(t, agent, "", `printf x > "$S/CLAUDE.md"`)
		want = "CLAUDE.md is protected while a story is being worked on, and a shell command is the one way around the rule that protects it. " +
			"Instead: change it by hand outside a running iteration; if what it says is wrong, stop and say what rather than editing it " +
			"[protected-path-through-the-tool]"
		if !denied(r) || reasonOf(r) != want {
			t.Errorf("%q: the protected path through $S changed:\n%s\nwant\n%s", agent, reasonOf(r), want)
		}
	}
}

// ---------------------------------------------------------------- the evasions

// orLinks makes the links the evasions use, or skips where they cannot be made:
// in leads from outside into the project's internal, and top into the project.
func orLinks(t *testing.T, w orWorld) {
	t.Helper()
	for name, target := range map[string]string{
		"in":  filepath.Join(w.root, "internal"),
		"top": w.root,
	} {
		if err := os.Symlink(target, filepath.Join(w.out, name)); err != nil {
			t.Skipf("symbolic links cannot be made here: %v", err)
		}
	}
}

// Everything that reaches the repository from outside it is the repository's,
// however it is spelled. Each of these is refused today, and none of them may
// be let through by letting the paths placed outside go.
func TestAC1WhatReachesTheRepositoryFromOutsideStaysRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	orLinks(t, w)
	rows := []orRow{
		// The project, written from the root of the disk and from outside.
		{"", "rm -f {root}/internal/x_test.go"},
		{"{out}", "rm -f {root}/internal/x_test.go"},
		{"{out}", "echo x > {root}/internal/brand_new_test.go"},
		{"{out}", "printf x > {root}/.sdlc/state/active"},
		{"{out}", "printf x > {root}/CLAUDE.md"},
		// A link outside that leads in, made before the command.
		{"", "rm -f {out}/in/x_test.go"},
		{"", "echo x > {out}/in/brand_new_test.go"},
		{"", "printf x > {out}/top/.sdlc/state/active"},
		{"", "printf x > {out}/top/CLAUDE.md"},
		{"", "find {out}/in -name '*_test*' -delete"},
		{"", "find {out}/top -name '*_test*' -delete"},
		{"", "find {out}/top -name active -delete"},
		{"{out}", "rm -f in/x_test.go"},
		{"{out}", "cd in && rm -f x_test.go"},
		{"{out}", "find in -name '*_test*' -delete"},
		{"{out}", "find top -name '*_test*' -delete"},
		{"{out}/in", "rm -f x_test.go"},
		{"{out}/in", "echo x > brand_new_test.go"},
		{"{out}/in", "find . -name '*_test*' -delete"},
		{"{out}/top", "rm -f internal/x_test.go"},
		{"{out}/top", "printf x > .sdlc/state/active"},
		{"{out}/top", "echo x > CLAUDE.md"},
		{"{out}/top", "find . -name '*_test*' -delete"},
		// `..` from an outside directory, and from one reached by `cd`.
		{"{out}", "rm -f {viaout}/internal/x_test.go"},
		{"{out}", "rm -f {out}/../{rootname}/internal/x_test.go"},
		{"{out}", "cd {out} && rm -f {viaout}/internal/x_test.go"},
		{"", "cd {out}/flat && rm -f ../{viaout}/internal/x_test.go"},
		{"{out}/copy", "rm -f ../{viaout}/internal/x_test.go"},
		{"{out}", "printf x > {viaout}/.sdlc/state/active"},
		{"{out}", "printf x > {viaout}/CLAUDE.md"},
		{"{out}", "find {viaout} -name '*_test*' -delete"},
		// A variable, a substitution or the shell's own directory as the
		// directory part: the hook cannot place it, so it is not outside.
		{"", `R=$PWD; rm -f "$R/internal/x_test.go"`},
		{"", `rm -f "$(pwd)/internal/x_test.go"`},
		{"", "rm -f $PWD/internal/x_test.go"},
		{"", `rm -f "$OLDPWD/internal/x_test.go"`},
		{"", "rm -f ~+/internal/x_test.go"},
		{"", "rm -f `pwd`/internal/x_test.go"},
		{"{out}", `D={root}; rm -f "$D/internal/x_test.go"`},
		{"{out}", `D={root}; printf x > "$D/.sdlc/state/active"`},
		{"{out}", `D={root}; printf x > "$D/CLAUDE.md"`},
		{"", `D={root}; find "$D" -name '*_test*' -delete`},
		// A glob or a brace in the directory part of an absolute path.
		{"", "rm -f {parent}/*/internal/x_test.go"},
		{"", "rm -f {parent}/{{rootname},{outname}}/internal/x_test.go"},
		{"", "rm -f {parent}/[!x]*/internal/x_test.go"},
		{"{out}", "rm -f {parent}/*/internal/x_test.go"},
		{"", "rm -f {out}/../*/internal/x_test.go"},
		// A directory the hook lost, then a name only the repository has.
		{"{out}", `cd "$D" && rm -f internal/x_test.go`},
		{"{out}", "cd - && rm -f internal/x_test.go"},
		{"{out}", "popd && rm -f internal/x_test.go"},
		{"{out}", `cd "$D" && printf x > .sdlc/state/active`},
		{"{out}", `cd "$D" && find . -name '*_test*' -delete`},
		// A find with several roots of which one is the repository or above it.
		{"", "find {out} . -name '*_test*' -delete"},
		{"", "find . {out} -name '*_test*' -delete"},
		{"", "find {out}/flat {root} -name '*_test*' -delete"},
		{"", "find {out}/flat {root}/internal -name '*_test*' -delete"},
		{"", "find {out} {parent} -name '*_test*' -delete"},
		{"", "find {out}/.. -name '*_test*' -delete"},
		{"", "find / -name '*_test*' -delete"},
		{"", "find .. -name '*_test*' -delete"},
		{"{out}", "find {root} -name '*_test*' -delete"},
		{"{out}", "find {parent} -name '*_test*' -delete"},
		{"{out}", "find {parent} -path '*/internal/x_test.go' -delete"},
		{"{out}", "find {out}/.. -wholename '*/internal/x_test.go' -delete"},
		{"{out}", `find "$S" -name '*_test*' -delete`},
		{"", `find "$S" -name '*_test*' -delete`},
		// A copy, move or download into the repository from outside, or the
		// frozen test taken out.
		{"", "cp {out}/flat/notes.txt {root}/internal/x_test.go"},
		{"", "mv {out}/flat/notes.txt internal/x_test.go"},
		{"{out}", "cp flat/notes.txt {root}/internal/x_test.go"},
		{"", "mv internal/x_test.go {out}/"},
		{"", "mv -t {out} internal/x_test.go"},
		{"", "mv {root}/internal/x_test.go {out}/flat/"},
		{"", "rm -rf {root}/internal"},
		{"", "curl -o internal/x_test.go https://example.invalid/x"},
		// An outside path on the same line as an inside one, or a program that
		// names the frozen test bare.
		{"", "rm -f {out}/flat/x_test.go internal/x_test.go"},
		{"{out}", "rm -f {out}/flat/x_test.go {root}/internal/x_test.go"},
		{"", "sed -i s/a/b/ {out}/flat/notes.txt internal/x_test.go"},
		{"", `python3 -c 'import os; os.remove("internal/x_test.go")' {out}/flat/notes.txt`},
		{"", "echo {out}/flat/notes.txt | xargs rm internal/x_test.go"},
		// The choices that are a person's, from outside as from inside.
		{"{out}", "sdlc approve A-1"},
		{"{out}", "sdlc unfreeze --reason x"},
	}
	for _, agent := range orAgents {
		for _, row := range rows {
			t.Run(agent+" from "+row.dir+" "+row.command, func(t *testing.T) {
				if r := w.ask(t, agent, row.dir, row.command); !denied(r) {
					t.Errorf("allowed: a spelling that reaches the repository from outside went through")
				}
			})
			// US-023 AC-5: the row again, for AGENTS.md.
			agAgentsMD(t, row.command, []string{w.root, w.out}, func(t *testing.T, respelt string) {
				t.Helper()
				if r := w.ask(t, agent, row.dir, respelt); !denied(r) {
					t.Errorf("allowed: a spelling that reaches AGENTS.md from outside went through")
				}
			})
		}
	}
}

// A link made and used in one command does not exist when the hook reads it,
// so the word through it is placed outside. Where it is named like the path it
// leads to, the by-its-end match refuses it today, and has to go on refusing it:
// the one thing a placed-outside word is no longer matched by is its end.
func TestAC1ALinkMadeAndUsedInOneCommandStaysRefused(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, agent := range orAgents {
		for _, command := range []string{
			"ln -s {root}/internal {out}/internal && rm -f {out}/internal/x_test.go",
			"ln -s {root}/internal {out}/internal; echo x > {out}/internal/x_test.go",
			"ln -sfn {root}/internal {out}/copy/internal && rm -f {out}/copy/internal/x_test.go",
			"ln -s {root} {out}/project && rm -f {out}/project/internal/x_test.go",
			"ln -s {root}/.sdlc {out}/.sdlc && rm -f {out}/.sdlc/state/active",
		} {
			if r := w.ask(t, agent, "", command); !denied(r) {
				t.Errorf("%q: %q went through", agent, command)
			}
		}
	}
}

// AC-2: a file with a test's name copied from outside over a frozen test is
// refused for where it lands, under the freeze's rule, and does not call the
// outside source the repository's.
func TestAC2ACopyFromOutsideOverAFrozenTestIsRefusedForTheDestination(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	for _, c := range []orRow{
		{"", "cp {out}/flat/x_test.go internal/x_test.go"},
		{"", "cp {out}/flat/x_test.go internal/"},
		{"", "cp {out}/copy/internal/x_test.go {root}/internal/x_test.go"},
		{"", "mv {out}/flat/x_test.go internal/"},
		{"", "cp -t internal {out}/flat/x_test.go"},
		{"{out}", "cp flat/x_test.go {root}/internal/"},
		{"{out}/flat", "cp x_test.go {root}/internal/x_test.go"},
	} {
		t.Run(c.dir+" "+c.command, func(t *testing.T) {
			r := w.ask(t, "", c.dir, c.command)
			got := reasonOf(r)
			if !denied(r) || !strings.Contains(got, "[frozen-test-through-the-tool]") {
				t.Fatalf("not refused for the frozen test it lands on: %+v", r)
			}
			if strings.HasPrefix(got, slash(w.out)) || strings.HasPrefix(got, "x_test.go ") {
				t.Errorf("the refusal calls the outside source the repository's file:\n%s", got)
			}
		})
	}
}

// ---------------------------------------------------------------- other checkouts

// Another checkout of the same repository is the repository's, though it is
// outside the session's own checkout: a frozen test at a path this story's
// freeze also holds is refused in any checkout, and docs/enforcement.md says so.
// The same checkout, and one beside it on the disk, from the story's worktree
// and from the main checkout.
func TestAC1AFrozenPathInAnotherCheckoutOfTheRepositoryStaysRefused(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	beside := filepath.Join(wtTempDir(t), "beside")
	wtGit(t, l.main, "worktree", "add", "--quiet", "-b", "beside", beside, "main")
	for _, o := range swOpenings(l) {
		t.Run(o.name, func(t *testing.T) {
			var rows []swRow
			for name, dir := range map[string]string{"main": l.main, "the other story's worktree": l.other, "a worktree beside": beside} {
				frozen := slash(filepath.Join(dir, "invoice_test.go"))
				rows = append(rows,
					swBash("append to the frozen path in "+name, o, "echo cheat >> "+frozen),
					swBash("remove the frozen path in "+name, o, "rm -f "+frozen),
				)
			}
			swRefused(t, swHeld, rows, "frozen-test-through-the-tool", nil)
		})
	}
	// An ordinary file in another checkout is the shell's to write.
	o := swOpenings(l)[0]
	swAllowed(t, swHeld, []swRow{
		swBash("an ordinary file in the main checkout", o, "echo x > "+slash(filepath.Join(l.main, "refund.go"))),
		swBash("an ordinary file in a worktree beside", o, "echo x > "+slash(filepath.Join(beside, "refund.go"))),
	}, nil)
}
