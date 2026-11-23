package shellpolicy

// US-134 AC-1 and AC-13, the second round of design review security (F-9 and
// F-11), on the rule us134_test.go pins, commands-run-as-configured.
//
// F-9: a program word the shell makes, a substitution, an expansion, a glob or
// a word put together from parts, is not `sdlc` to the reader, so a line that
// changes the environment and runs the call through one got past the first
// round's rows. Where the line changes the environment and a run's program
// word is made, and the words after it could be the call, it is refused: it
// cannot be ruled out as `sdlc gate implementation pass` or `sdlc freeze`.
//
// F-11: a wrapper can hand the call an environment with no assignment on the
// line (firejail --env, bwrap --setenv, direnv exec, uv run --env-file, npx's
// node_modules/.bin, op run, doppler run), and a limit (ulimit, umask, zsh's
// limit) can fail the freeze's run with tests that pass. So the call is
// refused unless every word before sdlc in its run, as written, is one the
// allowed rows use: time (with or without -p), !, exec and command with
// nothing or -p, nice, nohup, a leading backslash or a path to sdlc. A limit
// set earlier on the line or in the same compound counts as a change.
//
// How the rows were tried (2026-11-21, bash 5.3.20 by full path and zsh 5.9
// with -f, on macOS), with a stand-in sdlc that prints GOFLAGS, `ulimit -f`
// and `umask`, first on PATH and also under a directory a glob matches: every
// made program word ran the stand-in with GOFLAGS set in both shells, the
// glob rows with the glob pointed at a directory of the scratch copy; `ulimit
// -f` and umask changed what it saw in both, and `ulimit -t`, which it does
// not print, ran it; zsh's limit changed it in zsh, and bash, which has no
// limit, ran the call unchanged. Of the allowed wrappers, `time -p` ran the
// stand-in in bash only (zsh's time takes no -p and runs `-p`), and `command
// -p` found none in either shell, as its PATH holds no stand-in; the others
// ran it unchanged in both. Neither unlimit nor the if row was run. The
// wrappers were not run:
// firejail, bwrap, pipenv, op, doppler, prlimit and setsid are not installed
// here, and what each hands its program is the review's reading of them. Every refused row fails on today's code, which
// allows the line; the guards pass on it. All pass against the prototype in a
// scratch copy.
//
// The third round (F-15 and F-18), tried 2026-11-22 the same way: a made word
// that stands for the whole call, or for the program and part of it, and a
// made sdlc word behind a wrapper the allowed rows do not use. Every F-15 row
// handed the stand-in the call with GOFLAGS set in both shells, and `"$@"`
// after `set --` the same; with nothing changed on the line, the stand-in saw
// none. The F-18 wrappers were not run, for the reason above. A made word with
// nothing after it is read only where the shell can split it into several
// words (an unquoted substitution or expansion, `"$@"`): read wherever it
// stands, the prototype changed the answer of TestAC7InspectAnswersEveryCommandOfTheCorpusAsItDidAtTheBase,
// the unclosed-prefix rows of team_test.go and the exec-path rows of
// us130_test.go (`X=claude; script -q /dev/null "$X"`).

import "testing"

// AC-1 and AC-13 (F-9): the call run through a program word the shell makes,
// on a line that changes the environment, is refused.
func TestAC1AC13AProgramWordTheShellMakesIsRefusedWhereTheLineChangesTheEnvironment(t *testing.T) {
	for _, command := range us134Rows([]string{
		"GOFLAGS='-run=^$' $(command -v sdlc) @ARGS@",
		"GOFLAGS='-run=^$' `which sdlc` @ARGS@",
		// Holds no literal sdlc, so a first test for one must not skip it.
		"GOFLAGS='-run=^$' $(printf sdl)c @ARGS@",
		`s=sd; GOFLAGS=-v "${s}lc" @ARGS@`,
		"GOFLAGS=-v /usr/local/bin/sdl? @ARGS@",
		"GOFLAGS=-v ./plugin/bin/sdl* @ARGS@",
		"GOFLAGS=-v $SDLC @ARGS@",
		"GOFLAGS=-v ${SDLC:-sdlc} @ARGS@",
		"export GOFLAGS=-v; $(command -v sdlc) @ARGS@",
		"export GOFLAGS=-v; `which sdlc` @ARGS@",
		"export GOFLAGS=-v; $(printf sdl)c @ARGS@",
		`s=sd; export GOFLAGS=-v; "${s}lc" @ARGS@`,
		"export GOFLAGS=-v; /usr/local/bin/sdl? @ARGS@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
	// The subcommand made too: it may be freeze.
	refusedAsAChangedEnvironment(t, "export GOFLAGS=-v; $(command -v sdlc) $(echo freeze)", false)
}

// AC-1 and AC-13 (F-9): a made program word with nothing changed on the line
// is not this rule's, and a substitution elsewhere on an ordinary line changes
// nothing.
func TestAC1AC13AMadeProgramWordWithNothingChangedIsNotRefusedAsAChangedEnvironment(t *testing.T) {
	for _, command := range []string{
		"$(command -v sdlc) gate implementation pass --note x",
		`"$(command -v sdlc)" freeze`,
		"`which sdlc` freeze",
	} {
		for _, s := range us134Sessions {
			if f, ok := Inspect(command, s); ok && f.Rule == us134EnvRule {
				t.Errorf("AC-1 AC-13 %q (agent %q) was refused as %s, and nothing on it changes the environment: %s",
					command, s.Agent, f.Rule, f.Reason)
			}
		}
	}
	for _, command := range []string{
		`echo "$(date)"; sdlc status`,
		`echo "$(date)"; sdlc gate implementation pass --note green`,
		`sdlc freeze; echo "$(git rev-parse HEAD)"`,
	} {
		allowedInEverySession(t, command, false)
	}
}

// AC-1 and AC-13 (F-11): a wrapper the allowed rows do not use, in front of the
// call, is refused, whatever it does with the environment.
func TestAC1AC13AWrapperTheAllowedRowsDoNotUseIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"firejail --env=GOFLAGS=-run=^$ @RUN@",
		"bwrap --setenv GOFLAGS -v @RUN@",
		// direnv loads .envrc into what it runs.
		"direnv exec . @RUN@",
		"uv run --env-file .env @RUN@",
		// pipenv loads .env.
		"pipenv run @RUN@",
		// npx puts node_modules/.bin first on PATH.
		"npx @RUN@",
		"op run -- @RUN@",
		"doppler run -- @RUN@",
		"prlimit --fsize=0 @RUN@",
		"sudo @RUN@",
		"foo @RUN@",
		"go run ./cmd/@RUN@",
		// Known wrappers that set nothing are still not among the allowed:
		// the rule reads the words, not what each wrapper does.
		"env @RUN@",
		"timeout 600 @RUN@",
		"setsid @RUN@",
		"exec -a other @RUN@",
		"ssh localhost @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13 (F-11): a limit set earlier on the line, or in the same
// compound, is a change.
func TestAC1AC13ALimitSetEarlierOnTheLineIsRefused(t *testing.T) {
	for _, command := range us134Rows([]string{
		"ulimit -f 0; @RUN@",
		"ulimit -t 1 && @RUN@",
		"umask 777 && @RUN@",
		"umask 0777; @RUN@",
		// zsh only; bash has no limit and runs the call unchanged.
		"limit filesize 0; @RUN@",
		"unlimit; @RUN@",
		"{ ulimit -f 0; @RUN@; }",
		"( umask 777; @RUN@ )",
		"if true; then ulimit -f 0; fi; @RUN@",
	}) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13 (F-11): the wrappers the allowed rows use stay allowed, -p
// included, and alone or together.
func TestAC1AC13TheWrappersTheAllowedRowsUseStayAllowed(t *testing.T) {
	for _, command := range us134Rows([]string{
		// zsh's time takes no -p and runs `-p` as the program, which fails;
		// nothing is changed either way.
		"time -p @RUN@",
		"command -p @RUN@",
		"nohup nice @RUN@",
		"! time @RUN@",
		"exec command @RUN@",
	}) {
		allowedInEverySession(t, command, false)
	}
	// A limit or a wrapper on a line that runs neither call is not this
	// rule's.
	for _, command := range []string{
		"ulimit -n 4096; go test ./...",
		"umask 022 && sdlc status",
		"npx sdlc status",
		"timeout 600 sdlc gate implementation fail --note red",
	} {
		for _, s := range us134Sessions {
			if f, ok := Inspect(command, s); ok && f.Rule == us134EnvRule {
				t.Errorf("AC-1 AC-13 %q (agent %q) was refused as %s, and it runs neither call: %s",
					command, s.Agent, f.Rule, f.Reason)
			}
		}
	}
}

// AC-1 and AC-13 (F-15): a made word that stands for more than the program, the
// whole call or the program and part of it, has nothing after it or only a
// tail of the call (`pass`, `implementation pass`, flags and their values
// skipped). Both shells split an unquoted substitution into words, and the
// stand-in was handed the call with GOFLAGS set in each, `"$@"` after `set --`
// included.
func TestAC1AC13AMadeWordThatStandsForMoreThanTheProgramIsRefusedWhereTheLineChangesTheEnvironment(t *testing.T) {
	for _, command := range []string{
		"GOFLAGS='-run=^$' $(printf 'sd%s gate implementation pass' lc)",
		"GOFLAGS='-run=^$' $(printf 'sd%s freeze' lc)",
		"GOFLAGS=-v $(printf 'sd%s freeze' lc)",
		"set -- $(printf sd%s lc) gate implementation pass; GOFLAGS='-run=^$' \"$@\"",
		"set -- $(printf sd%s lc) freeze; GOFLAGS='-run=^$' \"$@\"",
		// The whole call in one made word, so that nothing follows it in set's
		// words either: only `"$@"` with nothing after it can be the call.
		"set -- $(printf 'sd%s freeze' lc); GOFLAGS=-v \"$@\"",
		"set -- $(printf 'sd%s gate implementation pass' lc); GOFLAGS=-v \"$@\"",
		"GOFLAGS=-v $(printf 'sd%s gate' lc) implementation pass",
		"GOFLAGS=-v $(printf 'sd%s gate implementation' lc) pass",
		"GOFLAGS=-v $(printf 'sd%s gate' lc) implementation pass --note green",
		"GOFLAGS=-v $(printf 'sd%s gate' lc) --note green implementation pass",
		"GOFLAGS=-v `printf 'sd%s freeze' lc`",
		"GOFLAGS=-v `printf 'sd%s gate implementation pass' lc`",
		// One substitution standing alone as a command, the environment
		// changed earlier on the line.
		"export GOFLAGS=-v; $(printf 'sd%s freeze' lc)",
		"export GOFLAGS=-v; $(printf 'sd%s gate implementation pass' lc)",
		"export GOFLAGS=-v; set -- $(printf sd%s lc) freeze; \"$@\"",
		"export GOFLAGS=-v; set -- $(printf sd%s lc) gate implementation pass; \"$@\"",
		"export GOFLAGS=-v; set -- $(printf 'sd%s freeze' lc); \"$@\"",
	} {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13 (F-15): the same made words with nothing changed on the line
// are not this rule's, and a made program word on a line that changes the
// environment, followed by words that are neither the call nor a tail of it,
// is not either.
func TestAC1AC13AMadeWholeCallWithNothingChangedOrAMadeProgramThatIsNotTheCallIsNotRefusedAsAChangedEnvironment(t *testing.T) {
	for _, command := range []string{
		"$(printf 'sd%s freeze' lc)",
		"$(printf 'sd%s gate implementation pass' lc)",
		"set -- $(printf 'sd%s freeze' lc); \"$@\"",
		"GOFLAGS=-v $(go env GOROOT)/bin/go test ./...",
		`GOFLAGS=-v "$(command -v go)" vet ./...`,
	} {
		for _, s := range us134Sessions {
			if f, ok := Inspect(command, s); ok && f.Rule == us134EnvRule {
				t.Errorf("AC-1 AC-13 %q (agent %q) was refused as %s, and it cannot run the call with a changed environment: %s",
					command, s.Agent, f.Rule, f.Reason)
			}
		}
	}
}

// AC-1 and AC-13 (F-18): each wrapper row of F-11 with sdlc made by the shell,
// as `$(command -v sdlc)` and as `$(printf sd%s lc)`, which holds no literal
// sdlc. A made word followed by the call's words is read as sdlc, behind a
// wrapper as at the front. The wrappers were not run, as in F-11's rows.
func TestAC1AC13AMadeSdlcWordBehindAWrapperTheAllowedRowsDoNotUseIsRefused(t *testing.T) {
	wrappers := []string{
		"firejail --env=GOFLAGS=-run=^$",
		"bwrap --setenv GOFLAGS -v",
		"direnv exec .",
		"uv run --env-file .env",
		"pipenv run",
		"npx",
		"op run --",
		"doppler run --",
		"prlimit --fsize=0",
		"sudo",
		"foo",
		"env",
		"timeout 600",
		"setsid",
		"exec -a other",
		"ssh localhost",
	}
	var rows []string
	for _, wrapper := range wrappers {
		for _, made := range []string{"$(command -v sdlc)", "$(printf sd%s lc)"} {
			rows = append(rows, wrapper+" "+made+" @ARGS@")
		}
	}
	rows = append(rows,
		"go run ./cmd/$(printf sd%s lc) @ARGS@",
		`sudo "$(command -v sdlc)" @ARGS@`,
		"direnv exec . `which sdlc` @ARGS@",
	)
	for _, command := range us134Rows(rows) {
		refusedAsAChangedEnvironment(t, command, false)
	}
}

// AC-1 and AC-13 (F-18): a made sdlc word behind the wrappers the allowed rows
// use, and a made word behind another wrapper that is not followed by the
// call, are not this rule's.
func TestAC1AC13AMadeWordBehindAnAllowedWrapperOrNotFollowedByTheCallIsNotRefusedAsAChangedEnvironment(t *testing.T) {
	for _, command := range []string{
		"time $(command -v sdlc) freeze",
		`nohup "$(command -v sdlc)" freeze`,
		"command -p $(printf sd%s lc) gate implementation pass --note x",
		"sudo $(command -v sdlc) status",
		"timeout 600 $(command -v go) test ./...",
		`go test -run "$(cat pattern)" ./...`,
	} {
		for _, s := range us134Sessions {
			if f, ok := Inspect(command, s); ok && f.Rule == us134EnvRule {
				t.Errorf("AC-1 AC-13 %q (agent %q) was refused as %s, and nothing in front of the call changes the environment: %s",
					command, s.Agent, f.Rule, f.Reason)
			}
		}
	}
}
