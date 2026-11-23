package shellpolicy

// US-134 AC-13, on the rule us134_test.go pins, commands-run-as-configured:
// the readings Gate 7's code reviews found missing, each held by the line a
// review gave and the spellings tried beside it.
//
//   - Security round 1 F-1: a zsh glob qualifier on the program word, `sdlc(N)`
//     or `~/.local/bin/sdlc(:A)`, is still sdlc to zsh, so a setting in front of
//     it, or exported earlier on the line, reaches the configured commands. A
//     qualifier that runs code, (e:…:), (+name) or (P:…:), may run the call.
//   - Security round 1 F-2: a program handed sdlc as the program it runs, rg's
//     --pre, man's -P and the pager variables man and less read, runs the call
//     with the line's settings.
//   - Code review round 2 F-5: an assignment or a step inside an arithmetic
//     expression that a nested `((…))` comes before, or in a for header.
//   - Security round 2 F-4: an arithmetic name made by an expansion, a command
//     substitution or a backtick, which the shell reads as the name it expands
//     to, or arithmetic read by a test, let, or zsh's shift, return and printf.
//   - Named descriptors: `{HOME}>file` assigns the descriptor's number to HOME
//     in the shell that runs the call.
//   - Security round 2 F-5: `hash -p` and zsh's `hash name=path` set the path
//     the shell runs for sdlc, which can be any program.
//
// Each refused row fails on trunk, which has no such rule, and each allowed
// row is a guard that passes on trunk and here. How the rows that run the call
// were tried in bash and zsh is in TEST-PLAN.md, sixteenth run.

import (
	"strings"
	"testing"
)

// g7Programs are the two program words F-1 names: sdlc on PATH and by its path.
var g7Programs = []string{"@SDLC@", "~/.local/bin/@SDLC@"}

func TestAC13AGlobQualifierOnTheProgramWordDoesNotHideASettingFromTheRule(t *testing.T) {
	var refused, allowed []string
	for _, program := range g7Programs {
		for _, q := range []string{"(N)", "(:A)", "(.)", "(#q.)"} {
			word := program + q + " @ARGS@"
			refused = append(refused, "GOFLAGS=-run=nothing "+word, "export GOFLAGS=-run=nothing; "+word)
			allowed = append(allowed, word)
		}
	}
	for _, command := range us134Rows(refused) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	for _, command := range us134Rows(allowed) {
		allowedInEverySession(t, command, false)
	}
}

func TestAC13AGlobQualifierThatRunsCodeBesideTheCallIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"echo *(e:'GOFLAGS=-v':); @RUN@",
		"echo *(+setflags); @RUN@",
		"echo *(P:-v:); @RUN@",
		"@SDLC@(e:'true':) @ARGS@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

func TestAC13AProgramHandedSdlcAsTheProgramItRunsIsRefusedWithASetting(t *testing.T) {
	for _, command := range []string{
		"GOFLAGS=-run=nothing rg --pre sdlc --regexp=x freeze",
		"GOFLAGS=-run=nothing rg --pre=sdlc --regexp=x freeze",
		"GOFLAGS=-run=nothing man -P 'sdlc freeze #' ls",
		"GOFLAGS=-run=nothing MANPAGER='sdlc freeze #' man ls",
		"GOFLAGS=-run=nothing PAGER='sdlc freeze #' man ls",
		"GOFLAGS=-run=nothing LESSOPEN='|sdlc freeze %s' less README.md",
		"GOFLAGS=-run=nothing man -P 'sdlc gate implementation pass --note x #' ls",
	} {
		refusedAsAChangedEnvironment(t, command, false)
	}
	for _, command := range []string{"rg sdlc freeze docs", "man sdlc"} {
		allowedInEverySession(t, command, false)
	}
}

func TestAC13AnArithmeticStepAfterANestedArithmeticOrInAForHeaderIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"(( ((1)), GOMAXPROCS++ )); @RUN@",
		"(( CI-- )); @RUN@",
		"(( ++CI )); @RUN@",
		": $(( ((1)) + ((2)), PATH++ )) ; @RUN@",
		"for (( i = 0; i < 1; i++, CI++ )); do :; done; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

func TestAC13AnArithmeticNameMadeByAnExpansionOrReadByATestOrABuiltinIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		": $(( ${x:-HOME}=1 )); @RUN@",
		"(( ${x:-HOME}=1 )); @RUN@",
		": $(( $(echo HOME)=1 )); @RUN@",
		": $(( `echo HOME`=1 )); @RUN@",
		": $[ ${x:-HOME}=1 ]; @RUN@",
		": ${a[${x:-HOME}=3]}; @RUN@",
		"[[ 1 -eq ${x:-HOME}=1 ]]; @RUN@",
		"let ${x:-HOME}=1; @RUN@",
		"shift 'HOME = 0'; @RUN@",
		"f() { return 'HOME++'; }; f; @RUN@",
		"printf %d 'HOME++' >/dev/null; @RUN@",
		"set -- '$(( HOME = 3 ))'; : ${1@P}; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	for _, command := range us134Rows([]string{
		"[[ $a == b ]] && @RUN@",
		"echo $((1+2)); @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
	// A quoted message that names the call is text, not a run of it.
	allowedInEverySession(t, `git commit -m "(( HOME=1 )); sdlc freeze"`, false)
}

func TestAC13ANamedDescriptorThatAssignsANameBeforeTheCallIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		": {HOME}>/dev/null; @RUN@",
		"exec {HOME}>/dev/null; @RUN@",
		": {HOME}<>/dev/null; @RUN@",
		": {HOME}>>/dev/null; @RUN@",
		": {HOME}>|/dev/null; @RUN@",
		": {HOME}<<<x; @RUN@",
		": {HOME}&>/dev/null; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

func TestAC13HashGivenAPathForSdlcIsRefusedAndHashThatOnlyReadsIsAllowed(t *testing.T) {
	for _, command := range us134Rows([]string{
		"hash -p /tmp/w @SDLC@; @RUN@",
		"hash -lp /tmp/w @SDLC@; @RUN@",
		"hash -tp /tmp/w @SDLC@; @RUN@",
		"hash -p/tmp/w @SDLC@; @RUN@",
		"builtin hash -p /tmp/w @SDLC@; @RUN@",
		"command hash -p /tmp/w @SDLC@; @RUN@",
		"hash @SDLC@=/tmp/w; @RUN@",
		"hash -d @SDLC@=/tmp/w; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	for _, command := range us134Rows([]string{
		"hash; @RUN@",
		"hash -r; @RUN@",
		"hash -t @SDLC@; @RUN@",
		"hash @SDLC@; @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
}

// The arithmetic scanner's places, each with an assignment in it: a test's
// words, a quoted operand of a test or of arithmetic, an offset after a
// subscript, zsh's width flags, and a subscript inside arithmetic.
func TestAC13AnAssignmentWhereverTheArithmeticScannerReadsArithmeticIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"[[ HOME=1 -eq 1 ]]; @RUN@",
		"[[ HOME++ -eq 1 ]]; @RUN@",
		"[[ 1 -eq ++HOME ]]; @RUN@",
		`[[ 1 -eq "HOME = 1" ]]; @RUN@`,
		"[[ 1 -eq 'HOME=1' ]]; @RUN@",
		// Read as a change: `--help` is a decrement of help where it stands
		// as an operand of a test that also compares numbers.
		"[[ $n -ge 1 && $flag == --help ]] && @RUN@",
		": $(( 'HOME=1' )); @RUN@",
		": $(( a[HOME=1] )); @RUN@",
		"a=(1 2); : ${a[@]:HOME++}; @RUN@",
		": ${#a[HOME++]}; @RUN@",
		": ${(l:HOME=3:)y}; @RUN@",
		"printf -v y %d 'HOME++'; @RUN@",
		"printf -- %d 'HOME++'; @RUN@",
		"print -f %d 'HOME++' >/dev/null; @RUN@",
		`: "$(( GOMAXPROCS=1 ))"; @RUN@`,
		`: "${a[GOMAXPROCS++]}"; @RUN@`,
		`[[ "${a[GOMAXPROCS++]}" -eq 1 ]]; @RUN@`,
		": ${!a[GOMAXPROCS++]}; @RUN@",
		": ${a[@]:GOMAXPROCS++}; @RUN@",
		"[[ 1 -eq 'GOMAXPROCS++' ]]; @RUN@",
		": ${(l:GOMAXPROCS++:)y}; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// A glob qualifier nested deeper than the reader follows, beside the call,
// is read as one that may run code: the reader cannot rule out an (e:…:)
// inside it.
func TestAC13AGlobQualifierNestedTooDeepToReadBesideTheCallIsRefused(t *testing.T) {
	deep := strings.Repeat("(", 2*maxNesting) + "N" + strings.Repeat(")", 2*maxNesting)
	for _, command := range us134Rows([]string{"echo *(N" + deep + "); @RUN@"}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// Guards: what the scanner reads and passes as no change.
func TestAC13WhatTheArithmeticScannerReadsAsCommandsOrAsReadsIsAllowed(t *testing.T) {
	for _, command := range us134Rows([]string{
		"echo ${PWD:1:2} && @RUN@",
		`echo "$(git rev-parse HEAD)"; @RUN@`,
		"echo `git rev-parse HEAD` $((1)); @RUN@",
		"(cd docs && true); @RUN@",
		"echo ${HOME} $(pwd); @RUN@",
		"printf '%s\\n' a; @RUN@",
		"echo hi # a (note)\n@RUN@",
		"@RUN@ # (done)",
		"echo `pwd` x; (( 1 )) && @RUN@",
		": ${a[$((1))]}; @RUN@",
		": ${(l:1:)GOMAXPROCS}; @RUN@",
		": $(( a[1] )) ; @RUN@",
		"[[ -n ${a[1]} ]] && @RUN@",
		": $[ 1 ]; @RUN@",
		"echo $(( (1) )); @RUN@",
		"[[ 1 -eq 1 ]] && @RUN@",
		"printf -v; @RUN@",
		"echo ${PWD:1:2} $(pwd) && @RUN@",
		"ls a[1] $(pwd); @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
}

// An offset of a substring expansion is arithmetic in bash: `${PWD:V++}`
// steps V and the call runs with it. The line holds no `(` or `[`, which is
// where the reader looks for arithmetic, so the implementation allows it.
// This row fails on the implementation as it stands.
func TestAC13AnOffsetOfASubstringExpansionThatStepsANameIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		": ${PWD:GOMAXPROCS++}; @RUN@",
		": ${PWD: -1:GOMAXPROCS++}; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}
