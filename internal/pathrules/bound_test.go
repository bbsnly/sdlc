package pathrules

// US-035, the parts of the bound on links that the hook's tests reach only
// through a whole command: a resolver At another project, which the
// configuration checks place words with (D8), and Bound("") and the spent
// budget, which decide a command no single word decided. A chain of n links
// at one name costs n links of the budget, Bound and Rel together, since the
// resolver is memoised.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestAC6AResolverAtAnotherProjectPlacesWordsThereAndChargesTheSameBudget(t *testing.T) {
	t.Run("AC-6 a word is placed against the other project", func(t *testing.T) {
		home, other := project(t), project(t)
		res := NewResolver(home).Memoised().Budgeted(CommandLinks)
		at := res.At(other)
		if rel, outside := at.Rel(filepath.Join(other, "notes.txt")); outside || rel != "notes.txt" {
			t.Errorf("At(other).Rel(other/notes.txt) = %q, outside %v; want notes.txt inside", rel, outside)
		}
		if _, outside := at.Rel(filepath.Join(home, "notes.txt")); !outside {
			t.Error("At(other) placed a word of the first project inside the other")
		}
		if rel, outside := res.Rel(filepath.Join(home, "notes.txt")); outside || rel != "notes.txt" {
			t.Errorf("after At, the first resolver's Rel(home/notes.txt) = %q, outside %v; want notes.txt inside", rel, outside)
		}
	})

	t.Run("AC-6 the other project is named through a link", func(t *testing.T) {
		home, other := project(t), project(t)
		link := filepath.Join(home, "to-other")
		if err := os.Symlink(other, link); err != nil {
			t.Skipf("this platform will not make a link here: %v", err)
		}
		at := NewResolver(home).Memoised().At(link)
		if rel, outside := at.Rel(filepath.Join(other, "notes.txt")); outside || rel != "notes.txt" {
			t.Errorf("At(a link to other).Rel(other/notes.txt) = %q, outside %v; want notes.txt inside: the project's own link was not followed", rel, outside)
		}
	})

	t.Run("AC-6 links followed at either project are charged to one budget", func(t *testing.T) {
		home, other := project(t), project(t)
		inHome := lbChainAtOneName(t, home, "a", 6, lbTarget(t, home))
		inOther := lbChainAtOneName(t, other, "b", 6, lbTarget(t, other))

		alone := NewResolver(other).Memoised().Budgeted(10)
		if err := alone.Bound(inOther); err != nil {
			t.Fatalf("a word through 6 links, alone against a budget of 10: %v, want nil", err)
		}

		res := NewResolver(home).Memoised().Budgeted(10)
		if err := res.Bound(inHome); err != nil {
			t.Fatalf("the first word through 6 links against a budget of 10: %v, want nil", err)
		}
		if err := res.At(other).Bound(inOther); !errors.Is(err, ErrLinkBudget) {
			t.Errorf("the second word, 12 links in all against 10, asked At the other project: %v, want ErrLinkBudget", err)
		}
		if err := res.Bound(""); !errors.Is(err, ErrLinkBudget) {
			t.Errorf("the first resolver, after its copy At the other project spent the budget: Bound(\"\") = %v, want ErrLinkBudget", err)
		}
	})
}

func TestAC5AC6BoundOfTheCommandAloneIsTheFirstBoundAnyWalkPassed(t *testing.T) {
	t.Run("AC-6 a resolver with no budget has passed nothing", func(t *testing.T) {
		root := project(t)
		res := NewResolver(root)
		res.Rel(filepath.Join(root, lbChainAtOneName(t, root, "a", 65, lbTarget(t, root))))
		if err := res.Bound(""); err != nil {
			t.Errorf("Bound(\"\") without a budget = %v, want nil", err)
		}
	})

	t.Run("AC-5 a word past the bound of one word, met by Rel, is the command's answer", func(t *testing.T) {
		root := project(t)
		word := lbChainAtOneName(t, root, "a", 65, lbTarget(t, root))
		res := NewResolver(root).Memoised().Budgeted(CommandLinks)
		if err := res.Bound(""); err != nil {
			t.Fatalf("Bound(\"\") before any walk = %v, want nil", err)
		}
		if rel, _ := res.Rel(filepath.Join(root, word)); rel == "elsewhere/x" {
			t.Errorf("Rel followed all 65 links of %s to %s; past the bound of one word it reads no link", word, rel)
		}
		if err := res.Bound(""); !errors.Is(err, ErrTooManyLinks) {
			t.Errorf("Bound(\"\") after Rel met a word through 65 links = %v, want ErrTooManyLinks", err)
		}
		if err := res.Bound("notes.txt"); err != nil {
			t.Errorf("a plain word after it: %v, want nil; one word over its bound does not spend the command's budget", err)
		}
	})

	t.Run("AC-6 once the budget is spent every word is over it and no link is followed", func(t *testing.T) {
		root := project(t)
		long := lbChainAtOneName(t, root, "a", 8, lbTarget(t, root))
		short := lbChainAtOneName(t, root, "b", 1, lbTarget(t, root))
		res := NewResolver(root).Memoised().Budgeted(5)
		res.Rel(filepath.Join(root, long))
		if err := res.Bound(""); !errors.Is(err, ErrLinkBudget) {
			t.Errorf("Bound(\"\") after 8 links against 5 = %v, want ErrLinkBudget", err)
		}
		if err := res.Bound("notes.txt"); !errors.Is(err, ErrLinkBudget) {
			t.Errorf("a plain word once the budget is spent: %v, want ErrLinkBudget", err)
		}
		if rel, _ := res.Rel(filepath.Join(root, short)); rel != "b/x" {
			t.Errorf("Rel(%s) once the budget is spent = %q, want b/x, by name: no link is followed after it", short, rel)
		}
	})

	t.Run("AC-6 a word whose place cannot be known is over no bound", func(t *testing.T) {
		// C:x is relative to a working directory on drive C on Windows, and
		// a file name elsewhere: neither is a path through links.
		res := NewResolver(project(t)).Memoised().Budgeted(CommandLinks)
		if err := res.Bound(`C:x`); err != nil {
			t.Errorf("Bound(C:x) = %v, want nil", err)
		}
	})
}

// AC-8, for the count of links as for follow: a place to count from that the
// path does not start with is past the bound, never a slice past its end,
// since a panic in the hook is an allow.
func TestAC8CountRefusesAPathThatDoesNotStartWithWhatIsKnown(t *testing.T) {
	root := project(t)
	res := NewResolver(root).Memoised().Budgeted(CommandLinks)
	known := filepath.Join(root, "longer", "than", "the", "path")
	var got followed
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("count panicked: %v", r)
			}
		}()
		got = res.count(known, filepath.Join(root, "x"), 10)
	}()
	if got.links <= 10 {
		t.Errorf("count from %s of a path it does not hold = %d links, want past the limit of 10", known, got.links)
	}
	if err := res.Bound(""); !errors.Is(err, ErrTooManyLinks) {
		t.Errorf("Bound(\"\") after it = %v, want ErrTooManyLinks", err)
	}
}

// AC-8, for Place's walk: followCounting's guard is the same, and it records
// the bound, so the command is refused for it even where no word's own
// answer said so.
func TestAC8FollowCountingRecordsAPathThatDoesNotStartWithWhatIsKnown(t *testing.T) {
	root := project(t)
	res := NewResolver(root).Memoised().Budgeted(CommandLinks)
	known := filepath.Join(root, "longer", "than", "the", "path")
	left := maxLinks
	var ok bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("followCounting panicked: %v", r)
			}
		}()
		_, ok, _ = res.followCounting(known, filepath.Join(root, "x"), 0, &left)
	}()
	if ok {
		t.Error("followCounting placed a path that does not start with what it was told is known")
	}
	if err := res.Bound(""); !errors.Is(err, ErrTooManyLinks) {
		t.Errorf("Bound(\"\") after it = %v, want ErrTooManyLinks: the guard did not record the bound", err)
	}
}

// AC-5 and AC-7: where Rel's or Place's walk stops at a name the system will
// not look up for the links before it, a budgeted resolver records the bound,
// so that Bound("") refuses the command whoever walked the word. Recorded
// nowhere, a word in a spelling Bound was not asked about was placed by name
// past the bound (US-035, code review). 41 is past both systems' limit on one
// lookup: macOS follows 32 links, Linux 40; on Windows, which follows 63
// reparse points, only 64 is (lbPastTheLookup). 65 links, one in each
// directory, stop the walk 33 or 41 links in on those two, within the bound,
// and on Windows 65 links in, past it.
func TestAC5AC7ABudgetedResolverRecordsTheBoundWhereAWalkStopsAtTheLookupLimit(t *testing.T) {
	past := lbPastTheLookup(41)
	for _, tc := range []struct {
		name   string
		make   func(t *testing.T, root string) string
		want   error
		lookup bool
	}{
		{fmt.Sprintf("%d links, one in each directory", past), func(t *testing.T, root string) string {
			t.Helper()
			return lbChainADirectoryAtATime(t, "symlink", root, "g", past, lbTarget(t, root))
		}, ErrLookupLimit, true},
		{"65 links, one in each directory", func(t *testing.T, root string) string {
			t.Helper()
			return lbChainADirectoryAtATime(t, "symlink", root, "g", 65, lbTarget(t, root))
		}, ErrTooManyLinks, runtime.GOOS != "windows"},
		{fmt.Sprintf("%d links at one name", past), func(t *testing.T, root string) string {
			t.Helper()
			return lbChainAtOneName(t, root, "a", past, lbTarget(t, root))
		}, ErrLookupLimit, true},
		{"65 links at one name", func(t *testing.T, root string) string {
			t.Helper()
			return lbChainAtOneName(t, root, "a", 65, lbTarget(t, root))
		}, ErrTooManyLinks, false},
	} {
		for _, walk := range []string{"Rel", "Place"} {
			t.Run("AC-7 "+walk+", "+tc.name, func(t *testing.T) {
				root := project(t)
				word := filepath.Join(root, tc.make(t, root))
				res := NewResolver(root).Memoised().Budgeted(CommandLinks)
				if walk == "Rel" {
					res.Rel(word)
				} else {
					res.Place(word)
				}
				err := res.Bound("")
				if !errors.Is(err, tc.want) || errors.Is(err, ErrLookupLimit) != tc.lookup {
					t.Errorf("Bound(\"\") after %s's walk = %v, want %v, the lookup limit %v\n%s",
						walk, err, tc.want, tc.lookup, lbLookups(root, word))
				}
			})
		}
	}

	t.Run("AC-11 a resolver with no budget answers as before", func(t *testing.T) {
		root := project(t)
		word := filepath.Join(root, lbChainADirectoryAtATime(t, "symlink", root, "g", 41, lbTarget(t, root)))
		res := NewResolver(root).Memoised()
		rel, outside := res.Rel(word)
		plainRel, plainOutside := NewResolver(root).Rel(word)
		if rel != plainRel || outside != plainOutside {
			t.Errorf("memoised Rel = %q, %v; unmemoised %q, %v", rel, outside, plainRel, plainOutside)
		}
		if err := res.Bound(""); err != nil {
			t.Errorf("Bound(\"\") without a budget = %v, want nil", err)
		}
	})
}

// lbLookupLength is the length of the shortest path this system will not look
// up, probed below root with short names, or 0 where it looked up every path
// tried or is Windows, whose error for one is not checked on a real system.
func lbLookupLength(root string) int {
	if runtime.GOOS == "windows" {
		return 0
	}
	long := root + "/" + strings.Repeat(strings.Repeat("a", 99)+"/", 700)
	tooLong := func(p string) bool {
		_, err := os.Lstat(p)
		return errors.Is(err, syscall.ENAMETOOLONG)
	}
	if !tooLong(long) {
		return 0
	}
	lo, hi := len(root)+1, len(long)
	for lo < hi {
		if mid := (lo + hi) / 2; tooLong(long[:mid]) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// lbLongWorkingDirectory makes in root a directory dir, and in it a directory
// whose full path is 6 bytes short of what the system looks up, holding a link
// l to target. It returns dir and the word, relative to dir, that goes through
// l: its full path is past the system's length, while a shell in dir writes to
// it by its shorter relative name. Where the system's length cannot be found,
// it skips.
func lbLongWorkingDirectory(t *testing.T, root, target string) (dir, word string) {
	t.Helper()
	limit := lbLookupLength(root)
	if limit == 0 {
		t.Skipf("no path below %s up to 70,000 bytes is refused as too long here, or this is Windows, whose error for one (206) is not checked on a real system; pathTooLong's mapping holds it", root)
	}
	dir = filepath.Join(root, "d")
	for rest := limit - 6 - len(dir) - 1; rest > 200; rest = limit - 6 - len(dir) - 1 {
		dir += "/" + strings.Repeat("A", min(200, rest-101))
	}
	b := strings.Repeat("b", limit-6-len(dir)-1)
	if err := os.MkdirAll(filepath.Join(dir, b), 0o755); err != nil {
		t.Skipf("this system will not make a directory %d bytes long: %v", limit-6, err)
	}
	if err := os.Symlink(target, filepath.Join(dir, b, "l")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	return dir, b + "/l/active"
}

// AC-7 (SDLC-E0083): pathTooLong is what the walk takes for a lookup of a path
// longer than the system looks up, as lookupLimit is for one past its links.
// Before it, lstat recorded nothing for that answer, and the walk read the rest
// of the word by name (US-035, security review). On Windows the error is 206,
// ERROR_FILENAME_EXCED_RANGE.
func TestAC7APathLongerThanTheSystemLooksUpIsToldApartFromOtherFailures(t *testing.T) {
	var long error = syscall.ENAMETOOLONG
	if runtime.GOOS == "windows" {
		long = syscall.Errno(206)
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"the system's error itself", long, true},
		{"inside the error Lstat gives", &os.PathError{Op: "lstat", Path: "p", Err: long}, true},
		{"a missing path", &os.PathError{Op: "lstat", Path: "p", Err: os.ErrNotExist}, false},
		{"a lookup past the links", &os.PathError{Op: "lstat", Path: "p", Err: syscall.ELOOP}, false},
		{"no error", nil, false},
	} {
		if got := pathTooLong(tc.err); got != tc.want {
			t.Errorf("AC-7: pathTooLong(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// AC-7 (SDLC-E0083), on disk: a word whose full path is past the system's
// length, through a link, is past a bound on a budgeted resolver, word and
// command, under ErrPathTooLong; a long word within the length is not; and the
// file tools' resolver, which has no budget, answers as it did.
func TestAC7ABudgetedResolverBoundsAWordWhoseFullPathIsTooLong(t *testing.T) {
	root := project(t)
	dir, word := lbLongWorkingDirectory(t, root, lbTarget(t, root))
	abs := filepath.Join(dir, word)
	t.Run("AC-7 the word past the length", func(t *testing.T) {
		res := NewResolver(root).Memoised().Budgeted(CommandLinks)
		if err := res.Bound(abs); !errors.Is(err, ErrPathTooLong) {
			t.Errorf("Bound(word) = %v, want %v", err, ErrPathTooLong)
		}
		if err := res.Bound(""); !errors.Is(err, ErrPathTooLong) {
			t.Errorf("Bound(\"\") = %v, want %v", err, ErrPathTooLong)
		}
	})
	t.Run("AC-7 Rel's walk alone records it", func(t *testing.T) {
		res := NewResolver(root).Memoised().Budgeted(CommandLinks)
		res.Rel(abs)
		if err := res.Bound(""); !errors.Is(err, ErrPathTooLong) {
			t.Errorf("Bound(\"\") after Rel's walk = %v, want %v", err, ErrPathTooLong)
		}
	})
	t.Run("AC-11 a long word within the length", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(dir, strings.Repeat("c", 50)), 0o755); err != nil {
			t.Fatal(err)
		}
		res := NewResolver(root).Memoised().Budgeted(CommandLinks)
		if err := res.Bound(filepath.Join(dir, strings.Repeat("c", 50), "x")); err != nil {
			t.Errorf("Bound(a word within the length) = %v, want nil", err)
		}
	})
	t.Run("AC-11 a resolver with no budget answers as before", func(t *testing.T) {
		res := NewResolver(root).Memoised()
		rel, outside := res.Rel(abs)
		plainRel, plainOutside := NewResolver(root).Rel(abs)
		if rel != plainRel || outside != plainOutside {
			t.Errorf("memoised Rel = %q, %v; unmemoised %q, %v", rel, outside, plainRel, plainOutside)
		}
		if err := res.Bound(""); err != nil {
			t.Errorf("Bound(\"\") without a budget = %v, want nil", err)
		}
	})
}

// AC-7: whether a lookup refused as too long, ending in name, reads as not
// there. A name over 255 UTF-16 units is held by no system the hook runs on,
// so it is not there anywhere. macOS and NTFS hold 255 units, so a name within
// them may be there and the path is too long (SDLC-E0083). Linux holds 255
// bytes, so there a name over 255 bytes is not there, unless the full path is
// 4,096 bytes or more, where the path is past the system's length whatever the
// name. Counted in bytes alone on every system, a legal macOS name of 130 "é"
// (260 bytes) on the way to a path past the length made that path read as not
// there, and a relative write through a link into the loop's state was allowed
// (US-035, verifier round 7). Counted in units alone on Linux, a word that is
// no path, as `python3 -c` code holding 200 Cyrillic letters, would read as a
// path too long and be refused, as every long word was in round 6. The rows
// with linux false are the ones this test held as nameOverLimit's before the
// landing, unchanged.
func TestAC7ANameIsNotThereOnlyWhereTheSystemCannotHoldIt(t *testing.T) {
	const emoji = "\U0001F600" // 4 bytes, 2 UTF-16 units
	for _, tc := range []struct {
		name  string
		in    string
		full  int
		linux bool
		want  bool
	}{
		{"no name", "", 1000, false, false},
		{"no name", "", 1000, true, false},
		{"255 ASCII", strings.Repeat("x", 255), 1000, false, false},
		{"255 ASCII", strings.Repeat("x", 255), 1000, true, false},
		{"256 ASCII", strings.Repeat("x", 256), 1000, false, true},
		{"256 ASCII", strings.Repeat("x", 256), 1000, true, true},
		{"256 ASCII", strings.Repeat("x", 256), 5000, true, true},
		{"130 é, 260 bytes", strings.Repeat("é", 130), 1000, false, false},
		{"130 é, 260 bytes", strings.Repeat("é", 130), 1000, true, true},
		{"130 é, 260 bytes", strings.Repeat("é", 130), 4095, true, true},
		{"130 é, 260 bytes", strings.Repeat("é", 130), 4096, true, false},
		{"130 é, 260 bytes", strings.Repeat("é", 130), 5000, true, false},
		{"255 é, 510 bytes", strings.Repeat("é", 255), 1000, false, false},
		{"255 é, 510 bytes", strings.Repeat("é", 255), 1000, true, true},
		{"255 é, 510 bytes", strings.Repeat("é", 255), 4096, true, false},
		{"256 é", strings.Repeat("é", 256), 1000, false, true},
		{"256 é", strings.Repeat("é", 256), 5000, true, true},
		{"127 emoji, 254 units", strings.Repeat(emoji, 127), 1000, false, false},
		{"127 emoji, 254 units", strings.Repeat(emoji, 127), 1000, true, true},
		{"127 emoji, 254 units", strings.Repeat(emoji, 127), 4096, true, false},
		{"128 emoji, 256 units", strings.Repeat(emoji, 128), 1000, false, true},
		{"128 emoji, 256 units", strings.Repeat(emoji, 128), 5000, true, true},
		{"200 Cyrillic, 400 bytes", strings.Repeat("ж", 200), 1000, false, false},
		{"200 Cyrillic, 400 bytes", strings.Repeat("ж", 200), 1000, true, true},
		{"200 Cyrillic, 400 bytes", strings.Repeat("ж", 200), 4095, true, true},
		{"200 Cyrillic, 400 bytes", strings.Repeat("ж", 200), 4096, true, false},
		{"255 CJK, 765 bytes", strings.Repeat("中", 255), 1000, false, false},
		{"255 CJK, 765 bytes", strings.Repeat("中", 255), 1000, true, true},
		{"256 CJK", strings.Repeat("中", 256), 1000, false, true},
		{"254 ASCII and one emoji, 256 units in 258 bytes", strings.Repeat("x", 254) + emoji, 1000, false, true},
		{"253 ASCII and one emoji, 255 units in 257 bytes", strings.Repeat("x", 253) + emoji, 1000, false, false},
		{"253 ASCII and one emoji, 255 units in 257 bytes", strings.Repeat("x", 253) + emoji, 1000, true, true},
	} {
		if got := notThereOn(tc.in, tc.full, tc.linux); got != tc.want {
			t.Errorf("AC-7: notThereOn(%s, %d bytes, linux %v) = %v, want %v", tc.name, tc.full, tc.linux, got, tc.want)
		}
	}
}

// lbPastTheLength makes in root a directory first, under it directories of
// 201-byte names until the full path is past limit, and in the last a link l
// to target. Each is made by its name in the one above, as a shell that walks
// a directory at a time makes it, since the system will not take the full
// path. It returns the names, from first down; where the platform will not
// make first, it skips.
func lbPastTheLength(t *testing.T, root, first, target string, limit int) []string {
	t.Helper()
	names := []string{first}
	for full := len(root) + 1 + len(first); full <= limit; full += 202 {
		names = append(names, fmt.Sprintf("d%d", len(names)-1)+strings.Repeat("n", 200))
	}
	at, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		if err := at.Mkdir(name, 0o755); err != nil {
			_ = at.Close()
			if i == 0 {
				t.Skipf("this platform will not make a directory of a %d-byte name: %v", len(first), err)
			}
			t.Fatal(err)
		}
		next, err := at.OpenRoot(name)
		_ = at.Close()
		if err != nil {
			t.Fatal(err)
		}
		at = next
	}
	defer func() { _ = at.Close() }()
	if err := at.Symlink(target, "l"); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	return names
}

// AC-7 (SDLC-E0083), on disk: a path past the system's length is bounded
// under ErrPathTooLong where a legal multibyte name is on the way to it; the
// name that failed is what decides, not an earlier one. A word whose last name
// is over the limit on one name cannot be there, so it is not bounded; nor is a
// short word under the multibyte name.
func TestAC7APathPastTheLengthThroughALegalMultibyteNameIsBounded(t *testing.T) {
	root := project(t)
	limit := lbLookupLength(root)
	if limit == 0 {
		t.Skip("no path below the project up to 70,000 bytes is refused as too long here, or this is Windows; nameOverLimit's rows hold the rule")
	}
	names := lbPastTheLength(t, root, strings.Repeat("é", 130), lbTarget(t, root), limit)
	word := filepath.Join(append(append([]string{root}, names...), "l", "active")...)
	t.Run("AC-7 the word through the link", func(t *testing.T) {
		res := NewResolver(root).Memoised().Budgeted(CommandLinks)
		if err := res.Bound(word); !errors.Is(err, ErrPathTooLong) {
			t.Errorf("Bound(a word of %d bytes) = %v, want %v", len(word), err, ErrPathTooLong)
		}
		if err := res.Bound(""); !errors.Is(err, ErrPathTooLong) {
			t.Errorf("Bound(\"\") = %v, want %v", err, ErrPathTooLong)
		}
	})
	for name, p := range map[string]string{
		"a short word under the multibyte name":             filepath.Join(root, names[0], "x"),
		"a last name of 256 ASCII bytes":                    filepath.Join(root, strings.Repeat("x", 256)),
		"a last name of 256 bytes under the multibyte name": filepath.Join(root, names[0], strings.Repeat("x", 256)),
	} {
		t.Run("AC-11 "+name, func(t *testing.T) {
			res := NewResolver(root).Memoised().Budgeted(CommandLinks)
			if err := res.Bound(p); err != nil {
				t.Errorf("Bound = %v, want nil", err)
			}
			if err := res.Bound(""); err != nil {
				t.Errorf("Bound(\"\") = %v, want nil", err)
			}
		})
	}
}

// AC-6: once the command's budget is spent, a word below a link is not looked
// up name by name past the link; the rest of it is read by name. The system
// follows every link on the way to each name it looks up, outside the budget,
// and 400 deep words below one chain of 31 links held the hook six seconds
// after the budget was spent (US-035, landing round). The memoised resolver
// keeps every path it asked the disk about, so what the walk asked is read
// from it, not timed. Within the budget, the walk goes down as it did, and
// Rel's answer is the unbudgeted resolver's.
func TestAC6OnceTheBudgetIsSpentAWordBelowALinkIsReadByNamePastIt(t *testing.T) {
	root := project(t)
	if err := os.MkdirAll(filepath.Join(root, "real", "a", "b", "c", "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "L")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	spender := filepath.Join(root, lbChainAtOneName(t, root, "s", 3, lbTarget(t, root)))
	word := filepath.Join(root, "L", "a", "b", "c", "d", "x")
	below := filepath.Join(root, "L") + string(filepath.Separator)
	askedBelow := func(res Resolver) []string {
		var asked []string
		for p := range res.disk.there {
			if strings.HasPrefix(p, below) {
				asked = append(asked, p)
			}
		}
		return asked
	}
	plainRel, plainOutside := NewResolver(root).Rel(word)

	t.Run("AC-6 the budget spent", func(t *testing.T) {
		res := NewResolver(root).Memoised().Budgeted(2)
		if err := res.Bound(spender); !errors.Is(err, ErrLinkBudget) {
			t.Fatalf("Bound(a word through 3 links) on a budget of 2 = %v, want %v", err, ErrLinkBudget)
		}
		// Past the budget Rel answers by name, as it did; the command is refused.
		res.Rel(word)
		if asked := askedBelow(res); len(asked) != 0 {
			t.Errorf("the walk looked up %d paths below the link after the budget was spent: %q", len(asked), asked)
		}
	})
	t.Run("AC-11 within the budget", func(t *testing.T) {
		res := NewResolver(root).Memoised().Budgeted(CommandLinks)
		rel, outside := res.Rel(word)
		if len(askedBelow(res)) == 0 {
			t.Error("the walk looked up nothing below the link within the budget, so the row above shows nothing")
		}
		if rel != plainRel || outside != plainOutside {
			t.Errorf("Rel = %q, %v; without a budget %q, %v", rel, outside, plainRel, plainOutside)
		}
	})
}
