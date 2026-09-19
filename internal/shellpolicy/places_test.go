package shellpolicy

import (
	"slices"
	"testing"
)

// Places is a caller's first look at where a command may act, before it
// knows which project that is in, so it keeps every word made of path
// characters, once, in the order they first appear -- a program name shaped
// the same way, such as rm or cat, included, since Places has no way yet to
// tell one from a path.
func TestPlacesListsEveryPathShapedWordOnceInOrder(t *testing.T) {
	got := Places("rm .sdlc/state/active && cat .sdlc/state/active", false)
	want := []string{"rm", ".sdlc/state/active", "cat"}
	if !slices.Equal(got, want) {
		t.Errorf("Places = %q, want %q", got, want)
	}
}

// PowerShell's backtick is its escape character, not a path character
// anywhere else in the shell it would matter to, so ".sdlc`/state/active"
// spelled with one in the middle is still the one path.
func TestPlacesTakesPowerShellsBackticksOut(t *testing.T) {
	got := Places("Remove-Item .sdlc`/state/active", true)
	want := []string{"Remove-Item", ".sdlc/state/active"}
	if !slices.Equal(got, want) {
		t.Errorf("Places = %q, want %q", got, want)
	}
	// Read the same way without PowerShell set, the backtick splits the word.
	got = Places("Remove-Item .sdlc`/state/active", false)
	want = []string{"Remove-Item", ".sdlc", "/state/active"}
	if !slices.Equal(got, want) {
		t.Errorf("Places without PowerShell = %q, want %q", got, want)
	}
}

// A command with nothing path-shaped in it names no places at all: the
// operators that join and redirect are not themselves paths.
func TestPlacesIsEmptyWithNoPathShapedWord(t *testing.T) {
	if got := Places("; | & > <", false); got != nil {
		t.Errorf("Places = %q, want none", got)
	}
}
