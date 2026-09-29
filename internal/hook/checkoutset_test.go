package hook

// US-029.2: the checkouts the take-away rule is given, and where it is told a
// word of the command lands, when the main checkout could not be found and
// when the word names a home nobody has.

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// AC-6 and T1: where the main checkout could not be found, the rule stands for
// the project at least, and a word is still placed from it.
func TestAC6WithoutTheMainCheckoutTheProjectIsStillACheckoutThatCannotBeTakenAway(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolver := pathrules.NewResolver(dir)
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.ToSlash(root)
	set := checkoutSet(place{project: dir, err: errors.New("no main checkout")}, resolver)
	if !slices.Equal(set.Checkouts, []string{root}) {
		t.Errorf("checkouts without the main one = %q, want only %s", set.Checkouts, root)
	}
	if set.Place == nil {
		t.Fatal("the set places no word")
	}
	if got := set.Place("sub/x"); len(got) == 0 || got[0] != root+"/sub/x" {
		t.Errorf("Place(sub/x) = %q, want %s/sub/x first", got, root)
	}
	// A word that lands nowhere the hook can say is placed nowhere, rather
	// than in the project: an empty word, and the home of a user with none.
	for _, word := range []string{"", "~no-such-user-sdlc-us0292/x"} {
		if got := set.Place(word); got != nil {
			t.Errorf("Place(%q) = %q, want nothing", word, got)
		}
	}
}
