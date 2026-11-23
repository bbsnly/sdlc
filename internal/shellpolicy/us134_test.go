package shellpolicy

// US-134 AC-1 and AC-13, as the shell rules have to hold them (design review,
// security F-1). With the story, `sdlc gate implementation pass` and `sdlc
// freeze` run the configured commands themselves and record the result, and
// the landing and the verifier reuse that result in place of a run of their
// own. The commands inherit the environment sdlc starts with, and the session
// chooses it: `GOFLAGS='-run=^$'`, a PATH whose `go` exits 0, or
// NODE_OPTIONS=--require in front of the call makes a recorded pass of a run
// that tested nothing. So the hook refuses a call that runs the configured
// commands where the same line changes the environment it starts with: an
// assignment in front of it, env with a setting or an unset, an export, a
// declaration, `set -a`, an unset, an alias or a function defined on the line,
// or the call handed to a shell or an interpreter as text, which the rule
// cannot read. The rule refuses any change, not a list of names, so that it
// names no tool's variable. `cd`, the call alone, and every other sdlc call,
// with or without settings, stay as the other rules leave them, as does
// `sdlc gate implementation fail`, which runs nothing.
//
// The rule's name, commands-run-as-configured, is held here and in
// internal/cli/docs_test.go, which asks docs/enforcement.md for its section.
//
// How the rows were tried (2026-11-21, bash 5.3.20 and zsh 5.9 on macOS). The
// 59 spellings of `sdlc freeze` whose effect turns on the shell were run under
// `bash -c` and `zsh -f -c`, with GOFLAGS unset and PATH led by a directory
// holding a stand-in `sdlc` that prints GOFLAGS, SHLVL and whether a bash
// function was handed on, and a `fake` directory holding another: every
// spelling of the call alone below ran the stand-in unchanged in both shells,
// every text row ran it, and every other refused row changed what it saw in
// at least one shell, except those whose comment says otherwise. A row whose
// effect differs between the shells says so beside it. Not run: the rows that
// differ from a run one only in the name, the call or the separator (the
// unsets, `export -n`, `declare -x`, the compounds, the gate's flags), the rows
// of a path that holds no stand-in (/usr/local/bin, npx), `env --unset`, which
// macOS's env does not have, the endless loop, node, perl, `cmd /c` and the
// PowerShell rows. Every row was asked of Inspect against today's code, where
// each refused row but the controls named below was allowed, and against a
// prototype of the rule built in a scratch copy, where every row held and the
// rest of internal/shellpolicy and internal/hook passed.
//
// The controls, refused today under a rule of their own, and kept so that the
// new rule does not take them over by accident: `env -i` and `exec -c`
// (enforcement-stays-on), a name export is given in `$'…'` quoting (the same),
// and `xargs` and a function named sdlc (assignment-is-a-human-decision). The PowerShell rows were not run, as no
// PowerShell was at hand.
//
// The third round of the review (F-16, F-17 and F-20), tried 2026-11-22 with
// bash 5.3.20 and zsh 5.9 (-f) and a stand-in sdlc first on PATH and another
// under fake: what the builtins agentenv.go reads as writing a protected name
// do to any name, bash's keyword option, an assigning `${…}` with flags or a
// nested name, and PowerShell's compound assignments. Each row's comment says
// which shell it changed. The rule reads these as agentenv.go reads them for
// the protected names, through one list, so that the two cannot drift.

import (
	"slices"
	"strings"
	"testing"
)

// us134EnvRule is the rule a call that runs the configured commands is
// refused under where the line changes its environment.
const us134EnvRule = "commands-run-as-configured"

// us134Calls are the two calls that run the configured commands, with the
// program word left as @SDLC@ so that a row can spell it another way.
var us134Calls = []string{"@SDLC@ gate implementation pass --note green", "@SDLC@ freeze"}

// us134Sessions are the sessions that may run either call: the main
// conversation, the implementer, the SDET and the verifier.
var us134Sessions = []State{
	{CommitReady: true},
	{CommitReady: true, Agent: "implementer"},
	{CommitReady: true, Agent: "sdet"},
	{CommitReady: true, Agent: "verifier"},
}

// us134Rows fills each row with each call: @RUN@ is the call with sdlc as its
// program word, and @SDLC@ and @ARGS@ the two halves of it, for a row that
// spells the program word itself.
func us134Rows(rows []string) []string {
	var out []string
	for _, call := range us134Calls {
		args := strings.TrimPrefix(call, "@SDLC@ ")
		run := "sdlc " + args
		for _, row := range rows {
			out = append(out, strings.NewReplacer("@RUN@", run, "@ARGS@", args, "@SDLC@", "sdlc").Replace(row))
		}
	}
	return out
}

// refusedAsAChangedEnvironment holds one command to a refusal in every session
// that may run the call, under the new rule or one of also. A refusal under the
// new rule says what to do next, sends nobody to end the iteration, and names
// no tool's variable: the rule is language-neutral.
func refusedAsAChangedEnvironment(t *testing.T, command string, powerShell bool, also ...string) {
	t.Helper()
	for _, s := range us134Sessions {
		s.PowerShell = powerShell
		f, ok := Inspect(command, s)
		who := s.Agent
		if who == "" {
			who = "the main conversation"
		}
		switch {
		case !ok:
			t.Errorf("AC-1 AC-13 %s may run %q, which changes the environment the configured commands run with, "+
				"or hands the call to a shell or an interpreter the rule cannot read", who, command)
			continue
		case f.Rule != us134EnvRule && !slices.Contains(also, f.Rule):
			t.Errorf("AC-1 AC-13 %s: %q was refused as %s, want %s (%s)", who, command, f.Rule, us134EnvRule, f.Reason)
			continue
		case f.Reason == "" || f.Route == "":
			t.Errorf("AC-1 AC-13 %s: the refusal of %q does not say why and what to do: %+v", who, command, f)
			continue
		}
		if f.Rule != us134EnvRule {
			continue
		}
		if !strings.Contains(f.Route, ".sdlc/config.json") || !strings.Contains(f.Route, "person") {
			t.Errorf("AC-1 AC-13 the route of %q does not say that a setting the commands need goes in .sdlc/config.json, "+
				"which a person edits: %s", command, f.Route)
		}
		if strings.Contains(f.Route, "sdlc stop") {
			t.Errorf("AC-1 AC-13 the route of %q ends the iteration: %s", command, f.Route)
		}
		for _, name := range []string{"GOFLAGS", "PYTEST_ADDOPTS", "NODE_OPTIONS", "CARGO", "RUSTFLAGS"} {
			if strings.Contains(f.Reason+" "+f.Route, name) && !strings.Contains(command, name) {
				t.Errorf("AC-1 AC-13 the refusal of %q names %s, a tool's variable the command does not: %+v", command, name, f)
			}
		}
	}
}

// allowedInEverySession holds one command to no refusal in every session.
func allowedInEverySession(t *testing.T, command string, powerShell bool) {
	t.Helper()
	for _, s := range us134Sessions {
		s.PowerShell = powerShell
		if f, ok := Inspect(command, s); ok {
			t.Errorf("AC-1 AC-13 %q (agent %q) was refused as %s, and it changes nothing the configured commands run with: %s",
				command, s.Agent, f.Rule, f.Reason)
		}
	}
}

// AC-1 and AC-13: the spellings security F-1 found allowed, each a setting in
// front of the call or given to it by a wrapper.
func TestAC1AC13ASettingInFrontOfACallThatRunsTheConfiguredCommandsIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"GOFLAGS='-run=^$' @RUN@",
		"PYTEST_ADDOPTS='-k nothing' @RUN@",
		`PATH="$PWD/fake:$PATH" @RUN@`,
		"NODE_OPTIONS=--require=./x.js @RUN@",
		// A name no tool reads: the rule refuses any change, not a list.
		"ZZ_US134=1 @RUN@",
		"GOFLAGS= @RUN@",
		"GOFLAGS+=-v @RUN@",
		"GOFLAGS=$'-run=^$' @RUN@",
		"A=1 B=2 @RUN@",
		"env GOFLAGS=-v @RUN@",
		"/usr/bin/env GOFLAGS=-v @RUN@",
		"env -- GOFLAGS=-v @RUN@",
		"env -u GOFLAGS @RUN@",
		// macOS's env takes the joined form; GNU env takes both.
		"env -uGOFLAGS @RUN@",
		"env --unset=GOFLAGS @RUN@",
		"GOFLAGS=-v exec @RUN@",
		"exec env GOFLAGS=-v @RUN@",
		"GOFLAGS=-v command @RUN@",
		"command env NODE_OPTIONS=--require=./x.js @RUN@",
		"command -p env GOFLAGS=-v @RUN@",
		"nice env GOFLAGS=-v @RUN@",
		"nohup env GOFLAGS=-v @RUN@",
		"GOFLAGS=-v nice @RUN@",
		"GOFLAGS=-v npx @RUN@",
		// `time` and `!` are keywords in both shells: the assignment after
		// them is the call's, and the stand-in printed it in both.
		"time GOFLAGS=-v @RUN@",
		"! GOFLAGS=-v @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	// Controls: refused today under the rule that holds an emptied environment.
	for _, command := range us134Rows([]string{"env -i @RUN@", "exec -c @RUN@"}) {
		refusedAsAChangedEnvironment(t, command, false, "enforcement-stays-on")
	}
}

// AC-1 and AC-13: an environment changed earlier on the line, or in the same
// compound, before the call.
func TestAC1AC13AnEnvironmentChangedEarlierOnTheLineIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"export GOFLAGS=-v; @RUN@",
		"export GOFLAGS=-v && @RUN@",
		"export GOFLAGS=-v\n@RUN@",
		`export PATH="$PWD/fake:$PATH"; @RUN@`,
		// bash takes a name out of the environment this way; zsh refuses -n.
		"export -n GOFLAGS; @RUN@",
		"builtin export GOFLAGS=-v; @RUN@",
		// bash exports it; zsh's command runs no builtin, and finds none.
		"command export GOFLAGS=-v; @RUN@",
		"declare -x GOFLAGS=-v; @RUN@",
		"declare -gx GOFLAGS=-v; @RUN@",
		"typeset -x GOFLAGS=-v; @RUN@",
		"f() { local -x GOFLAGS=-v; @RUN@; }; f",
		"readonly GOFLAGS=-v; export GOFLAGS; @RUN@",
		// zsh's readonly is typeset -r and takes -x; bash's refuses -x and
		// changes nothing. Refused for the shell that takes it.
		"readonly -x GOFLAGS=-v; @RUN@",
		"set -a; GOFLAGS=-v; @RUN@",
		// `set -a` alone changes nothing yet, but every assignment after it
		// is exported, and one can hide in what the line sources or reads.
		"set -a; @RUN@",
		"set -ea; @RUN@",
		"set -o allexport; @RUN@",
		// zsh only; bash has no setopt and runs the call unchanged.
		"setopt allexport; @RUN@",
		"unset GOFLAGS; @RUN@",
		"unset PATH; @RUN@",
		// An assignment alone exports nothing new, but GOFLAGS or PATH may
		// already be exported, and the reading cannot tell: PATH always is,
		// and the stand-in found the fake directory first in both shells.
		"GOFLAGS=-v; @RUN@",
		`PATH="$PWD/fake:$PATH"; @RUN@`,
		// bash: PATH[0] is PATH, and the stand-in was no longer found; zsh
		// refuses the subscript. zsh: path is tied to PATH, and the fake was
		// found first; bash's path is a name of its own.
		`PATH[0]="$PWD/fake"; @RUN@`,
		`path=("$PWD/fake" $path); @RUN@`,
		`printf -v PATH '%s' "$PWD/fake:$PATH"; @RUN@`,
		`read -r PATH <<< "$PWD/fake"; @RUN@`,
		"IFS= read -r GOFLAGS < flags; export GOFLAGS; @RUN@",
		"getopts v GOFLAGS -v; export GOFLAGS; @RUN@",
		// Neither shell passed it on: bash exports no array, and zsh has no
		// mapfile builtin. Refused as a builtin that writes a variable it is
		// named, as agentenv.go's readers read one.
		"mapfile -t GOFLAGS < flags; export GOFLAGS; @RUN@",
		`for PATH in "$PWD/fake:$PATH"; do @RUN@; done`,
		// zsh's ::= assigns whatever PATH held; bash refuses the spelling.
		": ${PATH::=$PWD/fake}; @RUN@",
		": ${GOFLAGS:=-v}; export GOFLAGS; @RUN@",
		// SHLVL is exported in both shells, so arithmetic changes it.
		"(( SHLVL = 9 )); @RUN@",
		"let SHLVL=9; @RUN@",
		// An alias defined on the line is not expanded on it in either
		// shell, but is in the persistent shell's next call; refused here.
		"alias sdlc='GOFLAGS=-v sdlc'; @RUN@",
		// bash hands the function on as BASH_FUNC_go%%, which a bash the
		// configured commands start reads; zsh has no export -f.
		"go() { :; }; export -f go; @RUN@",
		"source ./env.sh; @RUN@",
		". ./env.sh; @RUN@",
		"eval 'export GOFLAGS=-v'; @RUN@",
		"{ export GOFLAGS=-v; @RUN@; }",
		"( export GOFLAGS=-v; @RUN@ )",
		"( GOFLAGS=-v @RUN@ )",
		// The subshell's export does not reach the call in either shell. The
		// rule reads the line, and the line exports, so it is refused: the
		// call alone is the way round, and it costs nothing.
		"(export GOFLAGS=-v); @RUN@",
		"true && GOFLAGS=-v @RUN@",
		"false || GOFLAGS=-v @RUN@",
		"true; GOFLAGS=-v @RUN@",
		"GOFLAGS=-v \\\n@RUN@",
		"cd internal && GOFLAGS=-v @RUN@",
		"cd internal && export GOFLAGS=-v && @RUN@",
		// The export comes after the first call and before the second.
		"while :; do @RUN@; export GOFLAGS=-v; done",
		"if true; then export GOFLAGS=-v; fi; @RUN@",
		"case x in x) export GOFLAGS=-v;; esac; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	// Controls: a function named sdlc is refused today as a made program word,
	// and a name export is given in `$'…'` quoting as one that may be a
	// protected name (the shell makes it).
	for _, command := range us134Rows([]string{`sdlc() { GOFLAGS=-v command sdlc "$@"; }; @RUN@`}) {
		refusedAsAChangedEnvironment(t, command, false, "assignment-is-a-human-decision")
	}
	for _, command := range us134Rows([]string{"export $'GOFLAGS=-v'; @RUN@"}) {
		refusedAsAChangedEnvironment(t, command, false, "enforcement-stays-on")
	}
}

// AC-1 and AC-13: a call handed to a shell or an interpreter as text, which the
// rule cannot read, is refused, as the project refuses an ambiguous reading.
func TestAC1AC13ACallHandedToAShellOrAnInterpreterAsTextIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"sh -c '@RUN@'",
		"bash -c '@RUN@'",
		"zsh -c '@RUN@'",
		"/bin/sh -c '@RUN@'",
		"bash -c 'export GOFLAGS=-v; @RUN@'",
		"sh -c 'GOFLAGS=-v @RUN@'",
		"eval '@RUN@'",
		`eval "GOFLAGS=-v @RUN@"`,
		`find . -maxdepth 0 -exec @RUN@ \;`,
		"sh <<< '@RUN@'",
		"echo '@RUN@' | sh",
		`printf '%s\n' '@RUN@' | bash`,
		"bash <<'EOF'\n@RUN@\nEOF",
		`python3 -c 'import os; os.system("@RUN@")'`,
		`node -e 'require("child_process").execSync("@RUN@")'`,
		`perl -e 'system("@RUN@")'`,
		`cmd /c "set GOFLAGS=-v && @RUN@"`,
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	// Control: xargs hands sdlc words the line does not show, and is refused
	// today for it.
	for _, command := range us134Rows([]string{"xargs @RUN@ < /dev/null"}) {
		refusedAsAChangedEnvironment(t, command, false, "assignment-is-a-human-decision")
	}
}

// AC-1 and AC-13: the program word and the call's words spelt another way are
// the same call.
func TestAC1AC13TheCallSpeltAnotherWayIsStillRefused(t *testing.T) {
	var rows []string
	for _, program := range []string{`\sdlc`, `"sdlc"`, `'sdlc'`, `s\dlc`, `sdl''c`, "/usr/local/bin/sdlc", "./plugin/bin/sdlc"} {
		rows = append(rows, "GOFLAGS=-v "+program+" @ARGS@", "export GOFLAGS=-v; "+program+" @ARGS@")
	}
	for _, command := range us134Rows(rows) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	for _, command := range []string{
		"GOFLAGS=-v sdlc gate implementation pass",
		"GOFLAGS=-v sdlc gate --note green implementation pass",
		"GOFLAGS=-v sdlc gate implementation --note green pass",
		`GOFLAGS=-v sdlc "freeze"`,
		"GOFLAGS=-v sdlc fr''eeze",
		`GOFLAGS=-v sdlc gate 'implementation' "pass"`,
	} {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13: a PowerShell line that changes the environment before the
// call. Not run: no PowerShell was at hand.
func TestAC1AC13APowerShellLineThatChangesTheEnvironmentIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"$env:GOFLAGS = '-run=^$'; @RUN@",
		"$env:GOFLAGS='-v'; @RUN@",
		`${env:PATH} = "$PWD\fake;$env:PATH"; @RUN@`,
		"Set-Item Env:GOFLAGS '-v'; @RUN@",
		"Remove-Item Env:GOFLAGS; @RUN@",
		"[Environment]::SetEnvironmentVariable('GOFLAGS', '-v'); @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	for _, command := range us134Rows([]string{"@RUN@"}) {
		allowedInEverySession(t, command, true)
	}
	// On PowerShell the call stands alone on its line (security F-30): a cd
	// beside it is refused there, as it is not in bash.
	for _, command := range us134Rows([]string{"cd internal; @RUN@"}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	allowedInEverySession(t, "$env:GOFLAGS = '-v'; sdlc status", true)
}

// AC-1 and AC-13: what changes nothing the configured commands run with stays
// allowed: the call alone, through a wrapper that sets nothing, after a cd, and
// with its output sent elsewhere.
func TestAC1AC13TheCallWithNothingChangedInFrontOfItStaysAllowed(t *testing.T) {
	for _, command := range us134Rows([]string{
		"@RUN@",
		`\@RUN@`,
		"/usr/local/bin/@RUN@",
		"./plugin/bin/@RUN@",
		"cd internal && @RUN@",
		"cd .. && cd US-134 && @RUN@",
		"set -euo pipefail; @RUN@",
		"set -e; @RUN@",
		"time @RUN@",
		"! @RUN@",
		"exec @RUN@",
		"command @RUN@",
		"nice @RUN@",
		"nohup @RUN@",
		"@RUN@ 2>&1 | tail -n 5",
		"@RUN@ > /dev/null 2>&1",
		"@RUN@ < /dev/null",
		"@RUN@; echo done",
		"@RUN@ && git status",
		"echo '@RUN@'",
	}) {
		allowedInEverySession(t, command, false)
	}
	// A setting named in the call's own text changes nothing: a rule that
	// refused any `=` on the line would refuse these.
	for _, command := range []string{
		`sdlc gate implementation pass --note "GOFLAGS=-v is no longer needed"`,
		`sdlc gate implementation pass --note 'export PATH is gone; set -a too'`,
	} {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13: every other sdlc call, and every command that is not sdlc,
// keeps what the other rules make of it, settings and all. Recording the
// implementation gate as failed runs no command.
func TestAC1AC13EveryOtherCallKeepsItsSettings(t *testing.T) {
	for _, command := range []string{
		"GOFLAGS=-v sdlc status",
		"export GOFLAGS=-v; sdlc status",
		"NO_COLOR=1 sdlc gate dor pass",
		"GOFLAGS=-v sdlc gate plan pass",
		"GOFLAGS=-v sdlc gate implementation fail --note red",
		"export GOFLAGS=-v; sdlc gate implementation fail --note red",
		"sh -c 'sdlc status'",
		"export GOFLAGS=-v",
		"GOFLAGS=-v go test ./...",
		"export GOFLAGS=-v; go test ./...",
		`PATH="$PWD/bin:$PATH" go vet ./...`,
	} {
		allowedInEverySession(t, command, false)
	}
	// The code reviewer records its own review, settings or not.
	s := State{CommitReady: true, Agent: "code-reviewer"}
	if f, ok := Inspect("GOFLAGS=-v sdlc review add code_review code-reviewer approve", s); ok {
		t.Errorf("AC-1 AC-13 the code reviewer's own review, with a setting in front, was refused as %s: %s", f.Rule, f.Reason)
	}
}

// AC-1 and AC-13 (security round 3, F-16): every builtin that agentenv.go reads
// as writing a protected name it is given is read here as writing whatever name
// it is given, so that the two lists cannot drift. PATH is always exported, so
// a builtin that writes it changes what the configured commands run. In zsh
// the stand-in under fake was found first after zformat -f, zstyle -s,
// strftime -s, getln and sysread wrote PATH, and pcre_match -v left no sdlc to
// be found; in bash, coproc and `wait -p` did the same. bash has none of the
// zsh builtins and ran the call unchanged. The rest (zparseopts, zregexparse,
// zstat, zselect, zsystem, ztie, compadd) are refused as the line reads, as
// agentenv.go reads them.
func TestAC1AC13ABuiltinThatWritesAVariableItIsNamedIsAChange(t *testing.T) {
	for _, command := range us134Rows([]string{
		`zformat -f PATH "$PWD/fake:$PATH"; @RUN@`,
		`zstyle ':x' y "$PWD/fake:$PATH"; zstyle -s ':x' y PATH; @RUN@`,
		`zstyle -s ':x' y PATH; @RUN@`,
		`zmodload zsh/datetime; strftime -s PATH "$PWD/fake:$PATH" 0; @RUN@`,
		`print -z "$PWD/fake:$PATH"; getln PATH; @RUN@`,
		"getln PATH; @RUN@",
		"zmodload zsh/system; sysread PATH < flags; @RUN@",
		"zmodload zsh/pcre; pcre_compile x; pcre_match -v PATH x; @RUN@",
		"coproc PATH { :; }; @RUN@",
		"sleep 0 & wait -p PATH; @RUN@",
		"zparseopts -D -A PATH p:; @RUN@",
		"zregexparse PATH x x; @RUN@",
		"zstat -A PATH +mode /; @RUN@",
		"zselect -a PATH -r 0; @RUN@",
		"zsystem flock -f PATH ./lock; @RUN@",
		"ztie -d db/gdbm -f ./db PATH; @RUN@",
		"compadd -O PATH x; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13 (F-16): an assigning operator closing any `${…}` head is a
// change, flags and a nested name included: zsh's P flag assigns the name the
// nested expansion makes, and the stand-in under fake was found first in zsh.
// bash refuses the spelling as a bad substitution and runs nothing after it.
func TestAC1AC13AnExpansionThatAssignsThroughFlagsOrANestedNameIsAChange(t *testing.T) {
	for _, command := range us134Rows([]string{
		": ${(P)${:-PATH}::=$PWD/fake:$PATH}; @RUN@",
		"echo ${(P)${:-PATH}::=$PWD/fake:$PATH} > /dev/null; @RUN@",
		": ${(P)${:-PATH}:=x}; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13 (F-16): reading a variable, through print, an expansion with a
// default or flags, or its length, changes nothing.
func TestAC1AC13ReadingAVariableThroughPrintOrAnExpansionChangesNothing(t *testing.T) {
	for _, command := range us134Rows([]string{
		`print -r -- "$PATH"; @RUN@`,
		`echo "${GOFLAGS:-none=set}"; @RUN@`,
		"echo ${(P)${:-PATH}}; @RUN@",
		"echo ${#PATH}; @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13 (F-17): under bash's keyword option a NAME=value word anywhere
// among a command's words goes into its environment and out of its arguments,
// so the call is handed a setting written after it. In bash 5.3.20, `set -k`,
// `set -ek` and `set -o keyword` each had the stand-in under fake found first.
// zsh has no such option: its -k is another, and `set -o keyword` is refused as
// unknown; the rows are refused for the shell that takes them. An option
// cluster or a `set -o` word without it keeps its answer.
func TestAC1AC13BashsKeywordOptionIsAChange(t *testing.T) {
	for _, command := range us134Rows([]string{
		`set -k; @RUN@ PATH="$PWD/fake:$PATH"`,
		`set -ek; @RUN@ PATH="$PWD/fake:$PATH"`,
		`set -o keyword; @RUN@ PATH="$PWD/fake:$PATH"`,
		"set -uk; @RUN@ GOFLAGS=-v",
		// The option alone changes nothing yet, as `set -a` alone does not.
		"set -k; @RUN@",
		"set -o keyword; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	for _, command := range us134Rows([]string{
		"set -o pipefail; @RUN@",
		"set -eux; @RUN@",
		"set -o errexit -o nounset; @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13 (F-20): every assignment operator after `$env:NAME` and
// `${env:NAME}` is a change: `+=` sets a variable that was unset, `*=`
// repeats a string, and PowerShell 7's `??=` assigns where the variable is
// null. Not run: no PowerShell was at hand.
func TestAC1AC13APowerShellCompoundAssignmentToTheEnvironmentIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"$env:GOFLAGS += '-run=^$'; @RUN@",
		"$env:GOFLAGS ??= '-run=^$'; @RUN@",
		"$env:GOFLAGS??='-v'; @RUN@",
		"${env:GOFLAGS} += '-run=^$'; @RUN@",
		"${env:GOFLAGS} ??= '-run=^$'; @RUN@",
		`$env:PATH += ";$PWD\fake"; @RUN@`,
		"$Env:GOFLAGS *= 2; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	// A read and a comparison change nothing, but on PowerShell the call
	// stands alone on its line (security F-30), so each is refused beside it.
	for _, command := range us134Rows([]string{
		"$env:GOFLAGS; @RUN@",
		"if ($env:GOFLAGS -eq '-v') { 'set' }; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, true)
	}
	allowedInEverySession(t, "$env:GOFLAGS ??= '-v'; sdlc status", true)
}
