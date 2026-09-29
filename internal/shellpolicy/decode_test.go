package shellpolicy

// US-029.2, decode.go: the reading of a POSIX command once the shell has
// decoded `$'...'`, taken a backslash out, and expanded a brace list. Each
// spelling here ran as the git command it spells while the other readings saw
// a program or a subcommand nobody had heard of.

import "testing"

// AC-6 and T1: bash decodes `$'...'` before git sees it, with every escape it
// knows, and leaves the ones it does not know as written.
func TestAC6AnANSICStringIsDecodedAsBashDecodesIt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`git $'worktree' prune`, `git 'worktree' prune`},
		{`git $'\x77orktree' prune`, `git 'worktree' prune`},
		{`git $'\167orktree' prune`, `git 'worktree' prune`},
		{`git $'\u0077orktree' prune`, `git 'worktree' prune`},
		{`git $'\U00000077orktree' prune`, `git 'worktree' prune`},
		{`echo $'\u00e9'`, "echo 'é'"},
		{`echo $'\a\b\e\E\f\n\r\t\v'`, "echo '\a\b\x1b\x1b\f\n\r\t\v'"},
		{`echo $'\\\"\?'`, `echo '\"?'`},
		{`echo $'\cA\cz'`, "echo '\x01\x1a'"},
		// What bash does not decode it leaves as written.
		{`echo $'\q'`, `echo '\q'`},
		{`echo $'\xZ'`, `echo '\xZ'`},
		// A quote in the value is written back in double quotes, escaped.
		{`git $'it\'s' x`, `git "it's" x`},
		{`echo $'a\'$b'`, `echo "a'\$b"`},
		// A NUL ends the string, as it ends the one the shell hands a program.
		{`git $'a\0b' x`, `git 'a' x`},
		// An unterminated string is decoded to the end.
		{`echo $'abc`, `echo 'abc'`},
		// $"..." is its own string untranslated.
		{`git $"worktree" prune`, `git "worktree" prune`},
	} {
		if got := decoded(tc.in); got != tc.want {
			t.Errorf("decoded(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// AC-6 and T1: outside quotes the shell takes a backslash out of an ordinary
// character and keeps one that would change more than its word; inside
// quotes, and in a comment, nothing is decoded.
func TestAC6ABackslashIsTakenOutWhereTheShellTakesItOut(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`g\it work\tree prune`, `git worktree prune`},
		{`git com\mit`, `git commit`},
		{`echo a\ b\;c\$d\*e\\f`, `echo a\ b\;c\$d\*e\\f`},
		{`echo 'g\it'`, `echo 'g\it'`},
		{`echo 'unterminated \x`, `echo 'unterminated \x`},
		{`echo "g\it \" \$'x'"`, `echo "g\it \" \$'x'"`},
		{`echo "unterminated \q`, `echo "unterminated \q`},
		{"git status # g\\it $'x'\ng\\it log", "git status # g\\it $'x'\ngit log"},
		{`git status # $'x' g\it`, `git status # $'x' g\it`},
		{`echo a#b\c`, `echo a#bc`},
		{`plain words only`, `plain words only`},
		{`trailing\`, `trailing\`},
	} {
		if got := decoded(tc.in); got != tc.want {
			t.Errorf("decoded(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// AC-6 and T1: the rules read the decoded command, so a subcommand spelled in
// escapes is the subcommand, and a comment or a quoted string is not a
// command.
func TestAC6AGitCommandSpelledInEscapesIsTheCommandItSpells(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		`git $'\x77orktree' prune`,
		`git $'\167orktree' prune`,
		`git $'\u0077orktree' prune`,
		`git $'work\x74ree' prune`,
		`git $'worktree' $'prune'`,
		`git $"worktree" prune`,
		`g\it work\tree prune # a comment`,
		`echo "a \" b"; git $'worktree' prune`,
	} {
		wtRefusedAs(t, command, s, "worktree-through-the-tool")
	}
	wtRefusedAs(t, `git $'com\x6dit' -m x`, withShared(notReady, sharedLandings), "commit-gate")
	for _, command := range []string{
		`git worktree list # $'x' then git worktree prune`,
		`echo 'git $'\x77orktree' prune'`,
		`git $'it\'s' x`,
		`echo $'\a\b\e\E\f\n\r\t\v\\\'\"\?\cA\q'`,
		`echo $'abc`,
	} {
		allowed(t, command, s)
	}
}

// AC-6 and T1: bash expands a brace list into words before the command runs,
// and drops an empty word; one with more lists than the rules expand is kept
// whole, as one word they cannot finish.
func TestAC6ABraceListIsExpandedIntoTheWordsBashMakes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"git {worktree,} prune", "git worktree prune"},
		{"{git,commit,-m,x}", "git commit -m x"},
		{"echo a{b,c}d; ls", "echo abd acd; ls"},
		{"echo plain", "echo plain"},
		// Inside quotes as well, where bash leaves it: a script handed to a
		// shell expands it when it runs, and this reading only adds refusals.
		{`echo '{a,b}' "{c,d}"`, `echo 'a' 'b' "c" "d"`},
		{"echo ${X}", "echo ${X}"},
		{"echo ${X}{a,b} $(true)", "echo ${X}a ${X}b $(true)"},
		{"echo {a,b}{c,d}{e,f}{g,h}{i,j}{k,l}{m,n}{o,p}", "echo '{a,b}{c,d}{e,f}{g,h}{i,j}{k,l}{m,n}{o,p}'"},
	} {
		if got := bracesExpanded(tc.in); got != tc.want {
			t.Errorf("bracesExpanded(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct {
		s      string
		base   int
		width  int
		n      int
		digits int
	}{
		{"41z", 16, 2, 0x41, 2},
		{"4", 16, 2, 4, 1},
		{"z", 16, 2, 0, 0},
		{"1017", 8, 3, 0o101, 3},
		{"", 8, 3, 0, 0},
	} {
		if n, used := digits(tc.s, tc.width, tc.base); n != tc.n || used != tc.digits {
			t.Errorf("digits(%q, %d, %d) = %d, %d; want %d, %d", tc.s, tc.width, tc.base, n, used, tc.n, tc.digits)
		}
	}
}
