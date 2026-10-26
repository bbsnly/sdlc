package pathrules

// US-035 (from US-052): placing a word is bounded. A word that goes through
// more than 64 links passes the bound of one word, in place of being read by
// name after the 64th; and every link a decision follows, in Place's walk and
// in Rel's final resolve alike, is charged to one budget for the whole
// command. Before this, Place stopped after 64 links and read the rest of the
// word by name, Rel's resolve followed up to Go's own 255 with no count at
// all, and a command of many such words held the hook to its deadline.
//
// What these tests bind, and the implementation has to provide:
//
//	const CommandLinks int       the budget of links one command may follow,
//	                             the figure the plan derives (D4); above zero
//	var ErrTooManyLinks error    a word goes through more than 64 links, or
//	                             its placing stopped at a link it could not
//	                             read (D7)
//	var ErrLinkBudget error      the links followed for the command have
//	                             passed its budget
//	func (res Resolver) Budgeted(links int) Resolver
//	    res, with every link it follows from then on, by Rel, Place and
//	    Bound, charged to one count shared by every copy of it
//	func (res Resolver) Bound(p string) error
//	    nil where p is placed within both bounds; otherwise an error that
//	    errors.Is one of the two above
//
// A word through a chain is built here three ways, which are the three ways
// a word crosses links: every link at its first name, one link in each
// directory along it, and a link and then `..`, again and again.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// lbChainAtOneName makes name in root the head of a chain of n symlinks, each
// the next one's name and the last leading to the directory target, and
// returns the word name/x, which goes through all n at its first name.
func lbChainAtOneName(t *testing.T, root, name string, n int, target string) string {
	t.Helper()
	for i := range n {
		at := filepath.Join(root, lbLinkName(name, i))
		to := lbLinkName(name, i+1)
		if i == n-1 {
			to = target
		}
		if err := os.Symlink(to, at); err != nil {
			t.Skipf("this platform will not make a link here: %v", err)
		}
	}
	return name + "/x"
}

// lbLinkName is the name of the i-th link of the chain called name.
func lbLinkName(name string, i int) string {
	if i == 0 {
		return name
	}
	return fmt.Sprintf("%s.%d", name, i)
}

// lbChainADirectoryAtATime makes n directories, name-1 to name-n, each holding
// a link n to the next and the last a link to target, made by kind; and
// returns the word name-1/n/n/.../n/x, which crosses one link in each of n
// directories. A lookup of the whole word is past the limit the OS puts on
// one lookup (macOS 32, Linux 40) long before 64.
func lbChainADirectoryAtATime(t *testing.T, kind, root, name string, n int, target string) string {
	t.Helper()
	for i := 1; i <= n; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("%s-%d", name, i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= n; i++ {
		to := filepath.Join(root, fmt.Sprintf("%s-%d", name, i+1))
		if i == n {
			to = target
		}
		acLink(t, kind, to, filepath.Join(root, fmt.Sprintf("%s-%d", name, i), "n"))
	}
	return fmt.Sprintf("%s-1/%sx", name, strings.Repeat("n/", n))
}

// lbChainWithDotDot makes n links, name-k1 to name-kn, each to a directory of
// its own given by its absolute path, and returns the word
// name-k1/../name-k2/../.../name-kn/../x: each link is followed and left by
// its `..`, and an absolute target sends the walk back to the root each time.
func lbChainWithDotDot(t *testing.T, root, name string, n int) string {
	t.Helper()
	var word strings.Builder
	for i := 1; i <= n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("%s-s%d", name, i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		link := fmt.Sprintf("%s-k%d", name, i)
		if err := os.Symlink(dir, filepath.Join(root, link)); err != nil {
			t.Skipf("this platform will not make a link here: %v", err)
		}
		word.WriteString(link + "/../")
	}
	word.WriteString("x")
	return word.String()
}

// lbTarget is a directory in root that nothing protects, for a chain to lead
// to.
func lbTarget(t *testing.T, root string) string {
	t.Helper()
	target := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

// AC-5: a word through 65 links passes the bound of one word, however it
// crosses them, and a word through 30 does not. Asked of an unmemoised
// resolver and of the memoised one the hook uses.
func TestAC5AWordThroughMoreThan64LinksPassesTheBoundOfOneWord(t *testing.T) {
	for _, shape := range []struct {
		name string
		make func(t *testing.T, root, name string, n int) string
	}{
		{"every link at its first name", func(t *testing.T, root, name string, n int) string {
			t.Helper()
			return lbChainAtOneName(t, root, name, n, lbTarget(t, root))
		}},
		{"one link in each directory", func(t *testing.T, root, name string, n int) string {
			t.Helper()
			return lbChainADirectoryAtATime(t, "symlink", root, name, n, lbTarget(t, root))
		}},
		{"a link and then dot-dot, again and again", lbChainWithDotDot},
	} {
		t.Run("AC-5 "+shape.name, func(t *testing.T) {
			root := project(t)
			over := shape.make(t, root, "over", 65)
			within := shape.make(t, root, "within", 30)
			for name, res := range acResolvers(root) {
				if err := res.Bound(over); !errors.Is(err, ErrTooManyLinks) {
					t.Errorf("%s: Bound(%s), through 65 links, = %v; want ErrTooManyLinks", name, over, err)
				}
				if err := res.Bound(within); err != nil {
					t.Errorf("%s: Bound(%s), through 30 links, = %v; want nil: 30 is within every platform's own limit", name, within, err)
				}
			}
		})
	}
}

// AC-5 and AC-7 at the edge: 64 links is not more than 64. A word through 34
// to 64 links is either within the bound or refused for a link its placing
// could not read (D7), and whichever it is, a word within the bound is placed
// where its links lead: it is never read by name past a link that was not
// followed. Read so, a chain of 40 to 64 links into the loop's state was
// allowed for touch, tee, a redirect and install -D.
func TestAC5AC7AWordWithinTheBoundIsPlacedWhereItsLinksLeadAndNeverByName(t *testing.T) {
	for _, n := range []int{34, 41, 50, 64} {
		t.Run(fmt.Sprintf("AC-7 %d links, one in each directory", n), func(t *testing.T) {
			root := project(t)
			target := lbTarget(t, root)
			word := lbChainADirectoryAtATime(t, "symlink", root, "chain", n, target)
			for name, res := range acResolvers(root) {
				err := res.Bound(word)
				if err != nil {
					if !errors.Is(err, ErrTooManyLinks) {
						t.Errorf("%s: Bound(%s) = %v; want nil or ErrTooManyLinks", name, word, err)
					}
					continue
				}
				if rel, outside := res.Rel(word); outside || rel != "elsewhere/x" {
					t.Errorf("%s: Bound(%s) is nil, and Rel places it at %q (outside %v); want elsewhere/x, where its links lead", name, word, rel, outside)
				}
				if got, ok := res.Place(word); !ok || got != filepath.Join(target, "x") {
					t.Errorf("%s: Bound(%s) is nil, and Place places it at %q (%v); want %s", name, word, got, ok, filepath.Join(target, "x"))
				}
			}
		})
	}
}

// D5: on Windows a junction is a link any user may make, and Windows passes
// through a word's links in one call it bounds itself, so the count there is
// the links the walk crossed. 65 junctions, one in each directory, pass the
// bound; 30 do not. Nothing here needs a privilege.
func TestAC5OnWindowsAWordThroughMoreThan64JunctionsPassesTheBoundOfOneWord(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions exist only on Windows")
	}
	root := project(t)
	target := lbTarget(t, root)
	over := lbChainADirectoryAtATime(t, "junction", root, "over", 65, target)
	within := lbChainADirectoryAtATime(t, "junction", root, "within", 30, target)
	for name, res := range acResolvers(root) {
		if err := res.Bound(over); !errors.Is(err, ErrTooManyLinks) {
			t.Errorf("%s: Bound of a word through 65 junctions = %v; want ErrTooManyLinks", name, err)
		}
		if err := res.Bound(within); err != nil {
			t.Errorf("%s: Bound of a word through 30 junctions = %v; want nil", name, err)
		}
	}
}

// lbWords makes count words in root, each through a chain of its own of n
// links at its first name, to a directory nothing protects.
func lbWords(t *testing.T, root, prefix string, count, n int) []string {
	t.Helper()
	target := lbTarget(t, root)
	words := make([]string, count)
	for i := range count {
		words[i] = lbChainAtOneName(t, root, fmt.Sprintf("%s%d", prefix, i), n, target)
	}
	return words
}

// AC-6: the budget is for the whole command. Ten words of 30 links each follow
// 300 links, and a budget of 100 is passed on the way: the first word is
// within it, and once it is passed every word after is refused for it,
// whichever copy of the resolver is asked.
func TestAC6EveryLinkACommandFollowsIsChargedToOneBudget(t *testing.T) {
	t.Run("AC-6 an everyday command is within the hook's budget, and a resolver with none is never over one", lbEverydayAndUnbudgeted)
	root := project(t)
	words := lbWords(t, root, "w", 10, 30)
	res := NewResolver(root).Memoised().Budgeted(100)
	if err := res.Bound(words[0]); err != nil {
		t.Fatalf("Bound of the first word, 30 links of a budget of 100, = %v; want nil", err)
	}
	passed := -1
	for i, w := range words[1:] {
		copied := res
		err := copied.Bound(w)
		switch {
		case err == nil && passed >= 0:
			t.Errorf("word %d was within the budget after word %d had passed it", i+2, passed+2)
		case err != nil && !errors.Is(err, ErrLinkBudget):
			t.Errorf("Bound of word %d = %v; want nil or ErrLinkBudget", i+2, err)
		case err != nil && passed < 0:
			passed = i
		}
	}
	if passed < 0 {
		t.Fatal("ten words of 30 links each followed 300 links and never passed a budget of 100")
	}
	if err := res.Bound("plain.txt"); !errors.Is(err, ErrLinkBudget) {
		t.Errorf("a word with no link, asked after the budget was passed, = %v; want ErrLinkBudget", err)
	}
}

// AC-6: the count includes the final resolve of each word, Rel's reading, and
// Place's walk: four words of 30 links read by Rel alone pass a budget of
// 100, and so do four words of 30 links and a `..` read by Place alone. A
// word asked of Bound afterwards, with no link of its own, is over the
// budget the readings spent.
func TestAC6RelsFinalResolveAndPlacesWalkAreBothCharged(t *testing.T) {
	t.Run("AC-6 Rel", func(t *testing.T) {
		root := project(t)
		res := NewResolver(root).Memoised().Budgeted(100)
		for _, w := range lbWords(t, root, "r", 4, 30) {
			res.Rel(w)
		}
		if err := res.Bound("plain.txt"); !errors.Is(err, ErrLinkBudget) {
			t.Errorf("after Rel followed 120 links, Bound = %v; want ErrLinkBudget", err)
		}
	})
	t.Run("AC-6 Place", func(t *testing.T) {
		root := project(t)
		res := NewResolver(root).Memoised().Budgeted(100)
		for _, w := range lbWords(t, root, "p", 4, 30) {
			// head/../x is beside where the chain leads, read through every
			// link of it.
			res.Place(strings.TrimSuffix(w, "/x") + "/../x")
		}
		if err := res.Bound("plain.txt"); !errors.Is(err, ErrLinkBudget) {
			t.Errorf("after Place followed 120 links, Bound = %v; want ErrLinkBudget", err)
		}
	})
}

// lbEverydayAndUnbudgeted holds the other side of AC-6: a budget is the hook's
// to set for a command. A resolver nobody gave one is never over it, and an
// everyday command, twenty words through two links each, is well within the
// one the hook gives (D4).
func lbEverydayAndUnbudgeted(t *testing.T) {
	if CommandLinks <= 0 {
		t.Fatalf("CommandLinks is %d; a budget of none refuses every command with a link in it", CommandLinks)
	}
	root := project(t)
	budgeted := NewResolver(root).Memoised().Budgeted(CommandLinks)
	for _, w := range lbWords(t, root, "e", 20, 2) {
		if err := budgeted.Bound(w); err != nil {
			t.Errorf("an everyday word, %s through two links, = %v under the hook's budget of %d", w, err, CommandLinks)
		}
	}
	plain := NewResolver(root).Memoised()
	for _, w := range lbWords(t, root, "m", 10, 30) {
		if err := plain.Bound(w); errors.Is(err, ErrLinkBudget) {
			t.Fatalf("a resolver given no budget passed one at %s", w)
		}
	}
}

// AC-8: follow is handed what is known to hold no link, and the path it goes
// on to. Where the second does not start with the first it says not ok, and
// leaves the count of links alone: walkLinks slices the path at the length of
// what is known, which panics where the path is shorter, and a panic in the
// hook is an allow. Where the path starts with the bytes of what is known but
// not at a separator, the walk would go on from a place the path never names.
func TestAC8FollowRefusesAPathThatDoesNotStartWithWhatIsKnown(t *testing.T) {
	root := project(t)
	for _, dir := range []string{"ab/c", "abc", "sub"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	other := project(t)
	cases := []struct{ name, known, at string }{
		{"AC-8 a path shorter than what is known", filepath.Join(root, "ab", "c"), filepath.Join(root, "ab")},
		{"AC-8 a path that shares the bytes but not the name", filepath.Join(root, "ab"), filepath.Join(root, "abc")},
		{"AC-8 a path somewhere else altogether", filepath.Join(root, "ab"), filepath.Join(other, "elsewhere", "x")},
	}
	for name, res := range acResolvers(root) {
		for _, tc := range cases {
			t.Run(tc.name+", "+name, func(t *testing.T) {
				left := maxLinks
				to, ok, crashed := lbFollow(res, tc.known, tc.at, &left)
				if crashed != nil {
					t.Fatalf("follow(%s, %s) panicked: %v", tc.known, tc.at, crashed)
				}
				if ok {
					t.Errorf("follow(%s, %s) = %s, ok; want not ok", tc.known, tc.at, to)
				}
				if left != maxLinks {
					t.Errorf("follow(%s, %s) left %d links of %d; want the count untouched", tc.known, tc.at, left, maxLinks)
				}
			})
		}
		// And a path that does start with what is known is followed, as before.
		left := maxLinks
		at := filepath.Join(root, "sub")
		if to, ok, crashed := lbFollow(res, root, at, &left); crashed != nil || !ok || to != at {
			t.Errorf("%s: follow(%s, %s) = %s, %v (panic %v); want %s, ok", name, root, at, to, ok, crashed, at)
		}
	}
}

// lbFollow is res.follow, with a panic caught and handed back.
func lbFollow(res Resolver, known, at string, left *int) (to string, ok bool, crashed any) {
	defer func() { crashed = recover() }()
	to, ok = res.follow(known, at, 0, left)
	return to, ok, nil
}

// AC-5 and AC-7, for a word that begins with a dash: the shell rules skipped
// such a word, so the resolver is held here to treating it as any other name
// (US-035, the verifier's first gap). An option that names nothing on disk is
// within every bound.
func TestAC5AC7AWordThatBeginsWithADashIsBoundAndPlacedLikeAnyOther(t *testing.T) {
	root := project(t)
	over := lbChainAtOneName(t, root, "-d65", 65, lbTarget(t, root))
	gap := lbChainADirectoryAtATime(t, "symlink", root, "-g", 50, lbTarget(t, root))
	state := filepath.Join(root, ".sdlc", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(state, filepath.Join(root, "-st")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	res := NewResolver(root).Memoised().Budgeted(CommandLinks)
	for word, want := range map[string]error{over: ErrTooManyLinks, gap: ErrTooManyLinks, "-c": nil, "-rf": nil} {
		if err := res.Bound(word); !errors.Is(err, want) {
			t.Errorf("AC-5: Bound(%.40s) = %v, want %v", word, err, want)
		}
	}
	if rel, outside := res.Rel(filepath.Join(root, "-st", "active")); outside || rel != ".sdlc/state/active" {
		t.Errorf("AC-7: Rel(-st/active) = %q, outside %v; want .sdlc/state/active, through the link named -st", rel, outside)
	}
}
