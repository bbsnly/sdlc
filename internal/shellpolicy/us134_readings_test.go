package shellpolicy

// US-134 AC-1 and AC-13, on the rule us134_test.go pins,
// commands-run-as-configured: readings of configuredrun.go that no earlier row
// reached, each a way a line changes what the configured commands run with
// before the call, or hands the call to a shell the rule cannot read.
//
//   - An arithmetic command or expansion that assigns or steps a name: an
//     exported GOFLAGS or GOMAXPROCS that `(( … ))` sets is what the call's
//     commands are handed. One that only compares or reads changes nothing.
//   - An expansion whose head an assigning operator closes, however the head
//     is spelled: after zsh's flags, or with a subscript.
//   - su and runuser given the call as text to run, which is a shell of
//     another account's, with that account's environment.
//   - env given -- and then a setting.
//   - prlimit given a process: `--pid $$` sets the limits of the shell that
//     runs the call, which the call's commands inherit, as ulimit does
//     (security F-11). This row fails on the implementation as it stands: the
//     long form is allowed where `-p $$` is refused.
//
// How the rows were tried (2026-11-23, bash 5.3 by full path and zsh 5.9 with
// -f, on macOS), with a stand-in sdlc first on PATH that prints GOFLAGS and
// GOMAXPROCS, both exported beforehand: each arithmetic row that assigns or
// steps changed what the stand-in printed in both shells, and each that reads
// left it as it was. The row never closed is run by neither shell, which
// refuses to parse it; the reader takes its body to the end of the line, and
// refusing it costs nothing. With GOFLAGS exported empty, both head rows set
// it to what they assign in zsh; bash reads the flags as a bad substitution
// and the subscript as an array element, and runs the call unchanged. The env
// rows set GOFLAGS in both shells. su, runuser and prlimit were not run: the
// first two need another account, and prlimit is Linux's.

import "testing"

func TestAC1AC13AnArithmeticCommandThatAssignsOrStepsANameBeforeTheCallIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"(( GOFLAGS = 1 )); @RUN@",
		"(( GOMAXPROCS++ )); @RUN@",
		"(( --GOMAXPROCS )); @RUN@",
		": $((GOMAXPROCS=1)) && @RUN@",
		"(( GOMAXPROCS <<= 1 )); @RUN@",
		"(( a[1]=2 )); @RUN@",
		// Never closed: read to the end of the line.
		"(( GOMAXPROCS=1 ; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	// Guards: arithmetic that only compares or reads.
	for _, command := range us134Rows([]string{
		"(( 1 == 1 )) && @RUN@",
		"(( GOMAXPROCS >= 1 )) && @RUN@",
		"echo $((1 + 2)); @RUN@",
		"echo $(( GOMAXPROCS )) && @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
}

func TestAC1AC13AnExpansionWhoseHeadAssignsIsRefusedHoweverTheHeadIsSpelled(t *testing.T) {
	for _, command := range us134Rows([]string{
		": ${(L)GOFLAGS:=-V}; @RUN@",
		": ${GOFLAGS[1]:=-v}; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	// Guards: a default given, not assigned, and a length read.
	for _, command := range us134Rows([]string{": ${GOFLAGS:-none=set}; @RUN@", ": ${#GOFLAGS}; @RUN@"}) {
		allowedInEverySession(t, command, false)
	}
}

func TestAC1AC13TheCallHandedToAnotherAccountsShellIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"su -c '@RUN@' dev",
		"runuser --command='@RUN@' dev",
		"su dev -c true; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

func TestAC1AC13EnvGivenASettingAfterItsEndOfOptionsIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"env -- GOFLAGS=-v @RUN@",
		"env -- GOFLAGS=-v true; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

func TestAC13APrlimitThatSetsALimitOfTheShellThatRunsTheCallIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"prlimit -p $$ -n 10; @RUN@",
		"prlimit --pid $$ --nofile=10:10; @RUN@",
		"prlimit --pid=$$ --fsize=0; @RUN@",
		"prlimit --output RESOURCE --pid $$ --nofile=10 && @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}
