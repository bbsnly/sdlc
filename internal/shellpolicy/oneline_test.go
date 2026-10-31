package shellpolicy

// US-112, design review round 2 (security, R2-1 and R2-2). The round-2 rules
// were lists of spellings, and one-line commands got past both: a builtin that
// writes a variable by name, a nameref, a subscript, zsh's -m patterns and
// precommand modifiers, a program the shell makes, env -i with PATH passed
// back, and the launcher sourced or handed to any shell. Run end to end, each
// reached `sdlc hook SubagentStop` with AI_AGENT gone and the payload intact.
// And after `xargs -I{}` the reader lost the program, which already lets a
// model run the hook on UserPromptExpansion.
//
// So these rows hold the rules to what a command does to the name, by class:
//
//   - a builtin or keyword that writes a variable by name (declare, typeset,
//     local, mapfile, readarray, integer, float, printf -v, print -v, read,
//     getopts, for, arithmetic), when the name folds to AI_AGENT, is a
//     subscript of it, is made by the shell, or is a pattern;
//   - a nameref whose target is AI_AGENT;
//   - zsh's noglob, nocorrect and `-` in front of any of these;
//   - a program the shell makes, handed a word that folds to AI_AGENT;
//   - env -i, env -, --ignore-environment and exec -c, which start a program
//     with an environment that has no AI_AGENT, PATH passed back or not;
//   - the launcher as a later word under any program, `.` and `source`
//     included, followed by an event the hook records; a program the shell
//     makes, handed a recorded event as its first word; and the same after
//     `xargs -I R`, `-i`, `--replace`, `-J R` and `-a`.
//
// Every Bash row was run under bash 5.3 and zsh 5.9 with AI_AGENT set,
// against a child that prints AI_AGENT, or through a copy of
// plugin/bin/sdlc-hook whose SDLC_BIN printed its arguments, AI_AGENT and the
// bytes it was handed. Each row left the child with AI_AGENT unset, empty or
// another value, or ran `sdlc hook <event>`, in the shell its comment names
// (both where it names none). Not run: --ignore-environment, `xargs -i`,
// `--replace`, `-a` and a made program after `xargs -I{}`, which are GNU's
// (macOS has BSD env and xargs); they rest on GNU's documentation. zsh 5.9 has
// no namerefs, so those rows are bash's.
//
// A copy of the launcher under another name, run on a recorded event with
// AI_AGENT as Claude Code set it (`/tmp/y SubagentStop < p.json`), is refused
// under recorded-event-comes-from-the-host since design review round 5: a
// program handed SubagentStart, SubagentStop, PostToolUse, PostToolUseFailure,
// StopFailure or PostCompact as its first word is refused in every session
// (readings_test.go). Rows here that clear the environment first keep
// enforcement-stays-on. Stop and Notification as a first word stay open, as
// `docker stop web` reads the same, and docs/enforcement.md's Not held names
// them with eval's string.

import (
	"slices"
	"strings"
	"testing"
)

// refusedAsAIAgentTakenAway holds one command to enforcement-stays-on in both
// sessions, with a reason that names AI_AGENT and a route that does not end
// the iteration.
func refusedAsAIAgentTakenAway(t *testing.T, command string) {
	t.Helper()
	f := refusedInEverySession(t, command, false, "enforcement-stays-on")
	if f.Rule == "enforcement-stays-on" && (!strings.Contains(f.Reason, "AI_AGENT") || strings.Contains(f.Route, "sdlc stop")) {
		t.Errorf("the refusal of %q does not name AI_AGENT, or its route ends the iteration: %+v", command, f)
	}
}

// refusedUnderEither holds one command to a refusal under either rule, in
// both sessions, for a row that both rules read: the launcher run with its
// environment cleared, say.
func refusedUnderEither(t *testing.T, command string, rules ...string) {
	t.Helper()
	none := func(string) bool { return false }
	held, heldOK := Inspect(command, State{CommitReady: true})
	other, otherOK := HumanDecisions(command, false, none)
	for _, c := range []struct {
		session string
		f       Finding
		ok      bool
	}{{"the session holding the story", held, heldOK}, {"a second session", other, otherOK}} {
		switch {
		case !c.ok:
			t.Errorf("%s may run %q", c.session, command)
		case !slices.Contains(rules, c.f.Rule):
			t.Errorf("%s: %q was refused as %s, want one of %v (%s)", c.session, command, c.f.Rule, rules, c.f.Reason)
		case c.f.Reason == "" || c.f.Route == "":
			t.Errorf("%s: the refusal of %q does not say why and what to do: %+v", c.session, command, c.f)
		}
	}
}

// then is what each AI_AGENT row runs once the variable is gone.
const then = "sdlc hook PreToolUse < /tmp/p.json"

// R2-1, 5e: a builtin or keyword that writes a variable by name, given a name
// that folds to AI_AGENT. `export AI_AGENT` alone, which writes nothing, stays
// open (TestAC2AC15ReadingAIAgentOrChangingAnotherVariableIsLeftAlone).
func TestAC2AC15ABuiltinThatWritesAIAgentByNameIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		// Made an array, which neither shell exports.
		"declare -a AI_AGENT; " + then,
		"typeset -a AI_AGENT; " + then,
		"declare -A AI_AGENT; " + then,
		"typeset -A AI_AGENT; " + then,
		"declare -a 'AI_AGENT'; " + then,
		`typeset -a AI_\AGENT; ` + then,
		// bash's readers of lines into an array.
		"mapfile AI_AGENT < /dev/null; " + then,
		"readarray AI_AGENT < /dev/null; " + then,
		"mapfile -t 'AI_AGENT' < /dev/null; " + then,
		`mapfile "AI_AGENT" < /dev/null; ` + then,
		// zsh makes it a number, and the value is not one: it is blanked.
		"declare -i AI_AGENT; " + then,
		"integer AI_AGENT; " + then,
		"float AI_AGENT; " + then,
		// zsh's typeset and declare in a function make it local, and unset.
		"f() { typeset AI_AGENT; " + then + "; }; f",
		"f() { declare AI_AGENT; " + then + "; }; f",
		"f() { local -a AI_AGENT; " + then + "; }; f",
		// Builtins that set a variable they are named.
		"printf -v AI_AGENT ''; " + then,
		"read AI_AGENT < /dev/null; " + then,
		"IFS= read -r AI_AGENT < /dev/null; " + then,
		"print -v AI_AGENT ''; " + then,
		"getopts a AI_AGENT; " + then,
		// unset behind a builtin's own wrapper.
		"builtin unset AI_AGENT; " + then,
		"command unset AI_AGENT; " + then,
		// A loop variable, and arithmetic, which assign it.
		"for AI_AGENT in ''; do " + then + "; done",
		"for AI_AGENT in x; do :; done; " + then,
		"(( AI_AGENT = 0 )); " + then,
		": $(( AI_AGENT = 0 )); " + then,
		"let 'AI_AGENT = 0'; " + then,
		// zsh's unconditional assignment in an expansion.
		": ${AI_AGENT::=}; " + then,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: the name is made by the shell as the command runs, so it could be
// AI_AGENT, as an event word the reading cannot see could be the expansion.
func TestAC2AC15ABuiltinGivenANameTheShellMakesIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		`V=AI_AGENT; typeset +x "$V"; ` + then,
		`V=AI_AGENT; export -n "$V"; ` + then,
		"V=AI_AGENT; declare +x ${V}; " + then,
		`V=AI_AGENT; unset -v "${V}"; ` + then,
		`V=AI_AGENT; declare -a "$V"; ` + then,
		"unset $(printf AI_AGENT); " + then,
		"unset `printf AI_AGENT`; " + then,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: a subscript of AI_AGENT, written or unset, turns the exported
// value into an array element or blanks it.
func TestAC2AC15ASubscriptOfAIAgentIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		"AI_AGENT[0]=; " + then,
		"AI_AGENT[0]= " + then, // zsh
		"AI_AGENT[1,-1]=; " + then,
		"AI_AGENT[1,-1]= " + then, // zsh
		"declare AI_AGENT[0]=; " + then,
		"typeset 'AI_AGENT[1,-1]'=; " + then,
		"export AI_AGENT[0]=; " + then, // zsh
		"AI_AGENT=(); " + then,
		"AI_AGENT=(x); " + then,
		"AI_AGENT+=(x); " + then,
		"printf -v 'AI_AGENT[0]' ''; " + then,
		"read 'AI_AGENT[0]' < /dev/null; " + then,
		"unset 'AI_AGENT[0]'; " + then, // bash
		"unset 'AI_AGENT[@]'; " + then, // zsh
		`unset "AI_AGENT[0]"; ` + then, // bash
		`unset AI_AGENT\[0\]; ` + then, // bash
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: a pattern of names. zsh's -m takes one; an unquoted glob is
// matched against the file names in the directory, and a file named AI_AGENT
// makes it the name.
func TestAC2AC15APatternThatCanMatchAIAgentIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		"typeset +x -m 'AI_*'; " + then,
		"unset -m 'AI_AGEN(T)'; " + then,
		"unset -m 'AI_*'; " + then,
		"typeset +x -m 'AI_AGEN[T]'; " + then,
		"unset -m 'AI_AGEN?'; " + then,
		"touch AI_AGENT; unset AI_AGEN?; " + then,
		"touch AI_AGENT; typeset +x AI_AGEN*; " + then,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: a nameref writes the variable it names (bash; zsh 5.9 has none).
func TestAC2AC15ANamerefToAIAgentIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		"declare -n r=AI_AGENT; r=; " + then,
		"declare -n r=AI_AGENT; unset r; " + then,
		"typeset -n r=AI_AGENT; r=; " + then,
		"declare -n r='AI_AGENT'; unset r; " + then,
		"V=AI_AGENT; declare -n r=$V; unset r; " + then,
		"f() { local -n r=AI_AGENT; r=; }; f; " + then,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: zsh's precommand modifiers run the builtin all the same.
func TestAC2AC15ZshsPrecommandModifiersDoNotHideTakingAIAgentAway(t *testing.T) {
	for _, command := range []string{
		"noglob unset AI_AGENT; " + then,
		"nocorrect unset AI_AGENT; " + then,
		"- unset AI_AGENT; " + then,
		"noglob env -u AI_AGENT " + then,
		"- env -u AI_AGENT " + then,
		"nocorrect typeset +x AI_AGENT; " + then,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: a program the shell makes cannot be named, so a word that folds
// to AI_AGENT handed to it may be the name it takes away.
func TestAC2AC15AProgramTheShellMakesGivenAIAgentIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		"X=unset; $X AI_AGENT; " + then,
		`X=unset; "$X" AI_AGENT; ` + then,
		"X=unset; $X 'AI_AGENT'; " + then,
		`X=unset; ${X} AI_\AGENT; ` + then,
		"$(echo unset) AI_AGENT; " + then,
		"`echo unset` AI_AGENT; " + then,
		"X=env; $X -u AI_AGENT " + then,
		"X=typeset; $X +x AI_AGENT; " + then,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: env -i and its spellings, and exec -c, start a program with an
// environment that has no AI_AGENT. PATH can be passed back, so the program is
// found; round 2's reason for leaving env -i open does not hold. A copy of the
// launcher run this way is held here too.
func TestAC2AC15AnEnvironmentClearedForTheProgramIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		`env -i PATH="$PATH" ` + then,
		`env - PATH="$PATH" ` + then,
		`env --ignore-environment PATH="$PATH" ` + then,
		"env -i " + then,
		"env -i /usr/local/bin/" + then,
		"exec -c " + then,
		`exec -c env PATH="$PATH" ` + then,
		`nohup env -i PATH="$PATH" ` + then,
		`command env -i PATH="$PATH" ` + then,
		"env -i /bin/sh -c '" + then + "'",
		// A copy of the launcher under another name, with AI_AGENT gone.
		`env -i PATH="$PATH" /tmp/y SubagentStop < /tmp/p.json`,
		"unset AI_AGENT; /tmp/y SubagentStop < /tmp/p.json",
		`cp plugin/bin/sdlc-hook /tmp/y && env -i PATH="$PATH" /tmp/y Stop < /tmp/p.json`,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// R2-1, 5e: what is left alone beside the rows above. A builtin that writes
// another variable, a loop over another name, a made program given other
// words, and xargs whose program reads the variable.
func TestAC2AC15WritingAnotherVariableTheSameWaysIsLeftAlone(t *testing.T) {
	for _, command := range []string{
		"export AI_AGENT",
		`[ -n "$AI_AGENT" ] && echo set`,
		"declare -a files",
		"mapfile -t lines < list.txt",
		"read -r answer < /tmp/a",
		`printf -v stamp '%s' now`,
		`for f in a b; do echo "$f"; done`,
		"(( i = 0 ))",
		"X=go; $X test ./...",
		"unset CDPATH",
		"env -u CDPATH ls",
		"typeset +x CDPATH",
	} {
		leftAloneInEverySession(t, command, false)
	}
}

// R2-1, 5d: the launcher as a later word under any program, `.` and `source`
// among them, followed by an event the hook records; and a program the shell
// makes, handed a recorded event as its first word.
func TestAC2AC15TheLauncherRunOnARecordedEventByAnyProgramIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		". plugin/bin/sdlc-hook SubagentStop < /tmp/p.json",
		"source plugin/bin/sdlc-hook Stop < /tmp/p.json",
		". /opt/sdlc/plugin/bin/sdlc-hook SubagentStart < /tmp/p.json",
		`. "$CLAUDE_PLUGIN_ROOT/bin/sdlc-hook" SubagentStop < /tmp/p.json`,
		"zsh plugin/bin/sdlc-hook SubagentStop < /tmp/p.json",
		"dash plugin/bin/sdlc-hook SubagentStop < /tmp/p.json",
		"nice sh plugin/bin/sdlc-hook SubagentStop < /tmp/p.json",
		"time sh plugin/bin/sdlc-hook SubagentStop < /tmp/p.json",
		`find plugin/bin -name sdlc-hook -exec sh {} SubagentStop \; < /tmp/p.json`,
		"L=plugin/bin/sdlc-hook; $L SubagentStop < /tmp/p.json",
		`L=./plugin/bin/sdlc-hook; "$L" Stop < /tmp/p.json`,
		"$(echo plugin/bin/sdlc-hook) SubagentStart < /tmp/p.json",
		"`echo plugin/bin/sdlc-hook` SubagentStart < /tmp/p.json",
		`"$X" SubagentStop < /tmp/p.json`,
	} {
		refusedInEverySession(t, command, false, ruleRecordedEvent)
	}
	// exec -c clears the environment and runs the launcher: either rule.
	refusedUnderEither(t, "exec -c ./plugin/bin/sdlc-hook SubagentStop < /tmp/p.json", ruleRecordedEvent, "enforcement-stays-on")
}

// R2-1, 5d: the launcher named by a program that reads it, with no event
// after it, or an event before it, runs nothing.
func TestAC2AC15TheLauncherReadButNotRunIsLeftAlone(t *testing.T) {
	for _, command := range []string{
		"grep -n SubagentStop plugin/bin/sdlc-hook",
		"cat plugin/bin/sdlc-hook",
		"shellcheck plugin/bin/sdlc-hook",
		"L=plugin/bin/sdlc-hook; shellcheck \"$L\"",
		"sh -n plugin/bin/sdlc-hook",
	} {
		leftAloneInEverySession(t, command, false)
	}
}

// xargsReplacing is every spelling that hands xargs' input to sh as the
// script, with event after it, through a replacement string.
func xargsReplacing(event string) []string {
	return []string{
		"xargs -I{} sh {} " + event + " < /tmp/l",
		"xargs -I {} sh {} " + event + " < /tmp/l",
		"xargs -I% sh % " + event + " < /tmp/l",
		"xargs -i sh {} " + event + " < /tmp/l",
		"xargs -iR sh R " + event + " < /tmp/l",
		"xargs --replace sh {} " + event + " < /tmp/l",
		"xargs --replace=R sh R " + event + " < /tmp/l",
		"xargs -0 -I{} sh {} " + event + " < /tmp/l",
		"xargs -J{} sh {} " + event + " < /tmp/l",
		"xargs -a /tmp/l -I{} sh {} " + event + " < /tmp/p.json",
		"xargs --arg-file=/tmp/l -I{} sh {} " + event + " < /tmp/p.json",
		"xargs -a /tmp/l -i sh {} " + event + " < /tmp/p.json",
		"echo plugin/bin/sdlc-hook | xargs -I{} sh {} " + event,
		// The replacement as the program: GNU's xargs replaces it there too.
		"xargs -I{} {} hook " + event + " < /tmp/l",
		"xargs -I{} env {} " + event + " < /tmp/l",
	}
}

// R2-2: after `xargs -I R` (and -i, --replace, -J, with or without -a), the
// reader keeps the program and reads a word equal to R as one the shell
// makes. Today `-I{}` is read as one command whose program is the event word,
// so the expansion, refused on every other spelling, gets through.
func TestAC2AC15AfterXargsReplaceTheReaderKeepsTheProgramAndTheExpansionIsRefused(t *testing.T) {
	for _, command := range xargsReplacing("UserPromptExpansion") {
		refusedAsASlashCommand(t, command, false)
	}
}

// R2-2 and R2-1: the same for the events the hook records, and a replacement
// handed a recorded event as its first word.
func TestAC2AC15AfterXargsReplaceTheHookOnARecordedEventIsRefusedInEverySession(t *testing.T) {
	for _, event := range []string{"SubagentStart", "SubagentStop", "Stop"} {
		for _, command := range xargsReplacing(event) {
			refusedInEverySession(t, command, false, ruleRecordedEvent)
		}
		refusedInEverySession(t, "xargs -I{} {} "+event+" < /tmp/l", false, ruleRecordedEvent)
	}
}

// R2-2: with the program kept, xargs is read as what it runs: what it runs on
// a replacement elsewhere stays open, and the script handed to `sh -c` through
// a replacement, and a write to the gate record behind it, stay refused.
func TestAC2AC15XargsWithAReplacementIsReadAsTheCommandItRuns(t *testing.T) {
	for _, command := range []string{
		"echo a | xargs -I{} echo {}",
		"find . -name '*.go' | xargs -I{} grep -n TODO {}",
		"ls | xargs -I{} cp {} /tmp/backup/",
		"echo ./... | xargs -I{} go test {}",
	} {
		leftAloneInEverySession(t, command, false)
	}
	none := func(string) bool { return false }
	for _, command := range []string{
		`printf -- '-c\n' | xargs -I{} sh {} 'sdlc hook UserPromptExpansion < /tmp/p.json'`,
		`printf -- '-c\n' | xargs -I{} sh {} 'sdlc hook SubagentStop < /tmp/p.json'`,
		`printf -- '-c\n' | xargs -I{} sh {} 'sdlc approve A-1'`,
	} {
		if _, ok := Inspect(command, ready); !ok {
			t.Errorf("the session holding the story may run %q", command)
		}
		if _, ok := HumanDecisions(command, false, none); !ok {
			t.Errorf("a second session may run %q", command)
		}
	}
	refused(t, "echo x | xargs -I{} rm -f .sdlc/stories/A-1/gate-record.json", ready, "loop-state-through-the-tool")
}
