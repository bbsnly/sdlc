package pathrules

// Windows can answer a lookup past its 63 reparse points as a name not
// there, where macOS and Linux give ELOOP (limitReadsAsNotThere). lookUp then
// asks again, by following the links one at a time (reachable). That code
// runs only on Windows, and no Windows machine is to hand when it changes, so
// it is held here on every platform: reachable directly, and the bound with
// lstat given Windows' answer (withWindowsLookups).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withWindowsLookups gives lstat Windows' answer to a lookup past the
// system's limit, a name not there, for the rest of the test. The test must
// not run in parallel: limitUnsaid is the package's.
func withWindowsLookups(t *testing.T) {
	t.Helper()
	was := limitUnsaid
	limitUnsaid = true
	t.Cleanup(func() { limitUnsaid = was })
}

// llSymlink makes a symlink at, to target, or skips where the platform will
// not.
func llSymlink(t *testing.T, target, at string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
}

func TestReachableFollowsEachLinkOnItsOwnAndSaysWhereTheWayEnds(t *testing.T) {
	root := project(t)
	elsewhere := lbTarget(t, root)
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	lbChainAtOneName(t, root, "dangling", 3, filepath.Join(root, "nothing"))
	lbChainAtOneName(t, root, "chain", 3, elsewhere)
	llSymlink(t, "loop.1", filepath.Join(root, "loop"))
	llSymlink(t, "loop", filepath.Join(root, "loop.1"))
	llSymlink(t, filepath.Join(root, "file"), filepath.Join(root, "tofile"))
	llSymlink(t, filepath.Join("..", "elsewhere"), filepath.Join(root, "sub", "up"))
	past := lookupLinks() + 1
	lbChainAtOneName(t, root, "past", past, filepath.Join(root, "nothing"))
	for _, row := range []struct {
		name, dir string
		there     bool
		links     int
	}{
		{"a chain of 3 links to nothing is not there", "dangling", false, 3},
		{"a chain of 3 links to a directory is there", "chain", true, 3},
		{"a loop is followed to past the bound of one word", "loop", true, maxLinks + 1},
		{"a name below a link to a file is not there", "tofile/below", false, 1},
		{"a link to ../elsewhere leads beside its own directory", "sub/up", true, 1},
		{"a directory with no link on the way is there", "sub", true, 0},
		{"a name not there with no link on the way is not there", "missing", false, 0},
		{fmt.Sprintf("a chain of %d links to nothing, more than one lookup follows, reads as there", past),
			"past", true, past},
	} {
		t.Run(row.name, func(t *testing.T) {
			for name, res := range acResolvers(root) {
				there, links, _ := res.reachable(filepath.Join(root, filepath.FromSlash(row.dir)))
				if there != row.there || links != row.links {
					t.Errorf("%s: reachable(%s) = (%v, %d), want (%v, %d)", name, row.dir, there, links, row.there, row.links)
				}
			}
		})
	}
}

// pastTheLimit asks reachable once for each directory where the resolver is
// memoised, and charges what it followed to the budget once, and only for a
// directory past the limit: within it, the walk there has counted its links.
func TestPastTheLimitChargesTheLinksItFollowsOnceForEachDirectoryPastTheLimit(t *testing.T) {
	root := project(t)
	past := lookupLinks() + 1
	lbChainAtOneName(t, root, "dangling", 3, filepath.Join(root, "nothing"))
	lbChainAtOneName(t, root, "over", past, filepath.Join(root, "nothing"))
	within, over := filepath.Join(root, "dangling"), filepath.Join(root, "over")
	res := NewResolver(root).Memoised().Budgeted(1000)
	for range 3 {
		if p, _ := res.pastTheLimit(within); p {
			t.Fatalf("pastTheLimit(%s) is true for 3 links to nothing", within)
		}
		if p, _ := res.pastTheLimit(over); !p {
			t.Fatalf("pastTheLimit(%s) is false for %d links to nothing", over, past)
		}
	}
	if got, want := res.budget.left, 1000-past; got != want {
		t.Errorf("the budget has %d links left after 3 and %d links asked three times, want %d", got, past, want)
	}
	unmemoised := NewResolver(root).Budgeted(1000)
	for range 2 {
		unmemoised.pastTheLimit(within)
		unmemoised.pastTheLimit(over)
	}
	if got, want := unmemoised.budget.left, 1000-2*past; got != want {
		t.Errorf("unmemoised, the budget has %d links left after 3 and %d links asked twice, want %d", got, past, want)
	}
}

// With Windows' answer, a word past the system's limit on one lookup is still
// refused, whether its links lead somewhere or to nothing: a link to nothing
// past the limit is a name the system could not have looked up, not one that
// is not there. Read as not there, a chain of 40 links to nothing was allowed
// where macOS refuses it with ELOOP (review of 8b8658c). Words within the
// limit keep their answers.
func TestAC5AC7WithWindowsLookupsAWordPastTheSystemsLimitIsRefusedWhereverItLeads(t *testing.T) {
	withWindowsLookups(t)
	root := project(t)
	target, nothing := lbTarget(t, root), filepath.Join(root, "nothing")
	past := lookupLinks() + 1
	refused := map[string]string{
		"65 links, one in each directory":                                         lbChainADirectoryAtATime(t, "symlink", root, "over", 65, target),
		fmt.Sprintf("%d links to nothing at one name", past):                      lbChainAtOneName(t, root, "end", past, nothing),
		fmt.Sprintf("%d links, one in each directory, the last to nothing", past): lbChainADirectoryAtATime(t, "symlink", root, "gap", past, nothing),
	}
	allowed := map[string]string{
		"3 links to nothing at one name":  lbChainAtOneName(t, root, "short", 3, nothing),
		"30 links, one in each directory": lbChainADirectoryAtATime(t, "symlink", root, "within", 30, target),
		"30 links, the last to nothing":   lbChainADirectoryAtATime(t, "symlink", root, "near", 30, nothing),
		"a name not there below no link":  "missing/x",
		"a name not there below one link": "short.2/x",
	}
	for name, res := range acResolvers(root) {
		for row, word := range refused {
			if err := res.Bound(word); !errors.Is(err, ErrTooManyLinks) {
				t.Errorf("AC-5 %s: %s: Bound(%s) = %v, want ErrTooManyLinks", name, row, word, err)
			}
		}
		for row, word := range allowed {
			if err := res.Bound(word); err != nil {
				t.Errorf("AC-7 %s: %s: Bound(%s) = %v, want nil", name, row, word, err)
			}
		}
	}
}

// reachable says where the links lead a directory it reaches, which lookUp
// asks the name in when the system would not look the directory up.
func TestReachableSaysWhereTheLinksLeadTheDirectory(t *testing.T) {
	root := project(t)
	elsewhere := lbTarget(t, root)
	word := lbChainAtOneName(t, root, "chain", 3, elsewhere)
	for name, res := range acResolvers(root) {
		if there, _, at := res.reachable(filepath.Join(root, filepath.Dir(filepath.FromSlash(word)))); !there || at != elsewhere {
			t.Errorf("%s: reachable(chain) = (%v, %q), want (true, %q)", name, there, at, elsewhere)
		}
	}
}

// Windows will not go through a link to a file, which os.Symlink makes where
// the target is not there yet, so a lookup below one comes back not there
// whatever the directory it leads to holds. A name that is there, past such a
// link, is refused or placed where it leads; read as not there, `e0/x` was
// placed at d/x by name, though x is a link on into the project (review of
// b0cfef3).
func TestAC5ANameBehindALinkWindowsWillNotGoThroughIsNotReadAsNotThere(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows makes a link to a file that it will not go through as a directory")
	}
	root := project(t)
	if err := os.WriteFile(filepath.Join(root, "secret"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	llSymlink(t, "d", filepath.Join(root, "e0"))
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	llSymlink(t, filepath.Join(root, "secret"), filepath.Join(root, "d", "x"))
	for name, res := range acResolvers(root) {
		err := res.Bound("e0/x")
		rel, outside := res.Rel("e0/x")
		if !errors.Is(err, ErrTooManyLinks) && (outside || rel != "secret") {
			t.Errorf("%s: Bound(e0/x) = %v and Rel(e0/x) = (%q, outside %v); want it refused, or placed at secret", name, err, rel, outside)
		}
	}
}
