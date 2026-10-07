package shellpolicy

import (
	"fmt"
	"strings"
	"testing"
)

// The glob reader on its own: findPattern is -path and nameMatches -name, and
// segmentMatches is a segment of a shell glob, which reads a class the same way.
// A path is given as foundBy gives it, from `.` and folded.

// A globRead is a glob, a path or a name it is matched against, and whether find
// picks it.
type globRead struct {
	glob, against string
	want          bool
}

// findReads are read exactly as find reads them, and the glob has to be read.
var findReads = []struct {
	ac           string
	paths, names []globRead
}{
	{"AC1", []globRead{
		{"*[]x]_test.go", "./internal/x_test.go", true},
		{"*[]x]_test.go", "./internal/]_test.go", true},
		{"*[]x]_test.go", "./internal/y_test.go", false},
		{"*[]x]_test.g[o]", "./internal/x_test.go", true},
		{"[]x]", "]", true},
		{"[]x]", "x", true},
		{"[]x]", "]x]", false},
		{"*[]]_test.go", "./internal/]_test.go", true},
		{"*[]]_test.go", "./internal/x_test.go", false},
	}, []globRead{
		{"[]x]_test.go", "x_test.go", true},
		{"[]x]_test.go", "]_test.go", true},
		{"[]x]_test.go", "y_test.go", false},
		{"[]x]_test.g[o]", "x_test.go", true},
		{"*[]o]", "x_test.go", true},
		{"*[]o]", "x_test.gz", false},
		{"[]]_test.go", "]_test.go", true},
		{"[]]_test.go", "x_test.go", false},
	}},
	{"AC2", []globRead{
		{"*[]a-z]_test.go", "./internal/q_test.go", true},
		{"*[]a-z]_test.go", "./internal/]_test.go", true},
		{"*[]a-z]_test.go", "./internal/1_test.go", false},
		{"*[]a-z]_test.go", "./internal/-_test.go", false},
		{"*[]a-m]_test.go", "./internal/x_test.go", false},
	}, []globRead{
		{"[]a-z]_test.go", "x_test.go", true},
		{"[]a-z]_test.go", "]_test.go", true},
		{"[]a-z]_test.go", "1_test.go", false},
		{"[]a-z]_test.go", "-_test.go", false},
		{"[]a-m]_test.go", "x_test.go", false},
	}},
	{"AC4", []globRead{
		{"*[!x]_test.go", "./internal/y_test.go", true},
		{"*[!x]_test.go", "./internal/x_test.go", false},
		{"*[^x]_test.go", "./internal/y_test.go", true},
		{"*[^x]_test.go", "./internal/x_test.go", false},
		{"*[!]x]_test.go", "./internal/y_test.go", true},
		{"*[!]x]_test.go", "./internal/x_test.go", false},
		{"*[!]x]_test.go", "./internal/]_test.go", false},
		{"*[^]x]_test.go", "./internal/y_test.go", true},
		{"*[^]x]_test.go", "./internal/]_test.go", false},
		{"*[!]]_test.go", "./internal/x_test.go", true},
		{"*[!]]_test.go", "./internal/]_test.go", false},
		{"*[[:alpha:]]_test.go", "./internal/x_test.go", true},
		{"*[[:alpha:]]_test.go", "./internal/1_test.go", false},
		{"*[[:alpha:]]_test.go", "./internal/]_test.go", false},
		{"*[![:alpha:]]_test.go", "./internal/1_test.go", true},
		{"*[![:alpha:]]_test.go", "./internal/x_test.go", false},
		{"*[[:digit:]]_test.go", "./internal/1_test.go", true},
		{"*[[:digit:]]_test.go", "./internal/x_test.go", false},
		{"*[[:alpha:][:digit:]]_test.go", "./internal/1_test.go", true},
		{"*[[:alpha:][:digit:]]_test.go", "./internal/x_test.go", true},
		{"*[[:alpha:][:digit:]]_test.go", "./internal/-_test.go", false},
		{"*[a[:digit:]]_test.go", "./internal/a_test.go", true},
		{"*[a[:digit:]]_test.go", "./internal/1_test.go", true},
		{"*[a[:digit:]]_test.go", "./internal/b_test.go", false},
		{"*[[:punct:]]_test.go", "./internal/]_test.go", true},
		{"*[[:punct:]]_test.go", "./internal/x_test.go", false},
		{"*[!a-w]_test.go", "./internal/x_test.go", true},
		{"*[!a-w]_test.go", "./internal/b_test.go", false},
		{"*[^a-w]_test.go", "./internal/x_test.go", true},
		{"*[üx]_test.go", "./internal/ü_test.go", true},
		{"*[[:alpha:]]_test.go", "./internal/ü_test.go", true},
		// -path reads a `/` in a class, and under `?`, as any other character.
		{"*internal[/]x_test.go", "./internal/x_test.go", true},
		{"*internal[!/]x_test.go", "./internal/x_test.go", false},
		{"./internal?x_test.go", "./internal/x_test.go", true},
	}, []globRead{
		{"[!x]_test.go", "y_test.go", true},
		{"[!x]_test.go", "x_test.go", false},
		{"[^x]_test.go", "y_test.go", true},
		{"[^x]_test.go", "x_test.go", false},
		{"[!]x]_test.go", "y_test.go", true},
		{"[!]x]_test.go", "x_test.go", false},
		{"[!]x]_test.go", "]_test.go", false},
		{"[^]x]_test.go", "y_test.go", true},
		{"[^]x]_test.go", "x_test.go", false},
		{"[^]x]_test.go", "]_test.go", false},
		{"[!]]_test.go", "x_test.go", true},
		{"[!]]_test.go", "]_test.go", false},
		{"[[:alpha:]]_test.go", "x_test.go", true},
		{"[[:alpha:]]_test.go", "1_test.go", false},
		{"[![:alpha:]]_test.go", "1_test.go", true},
		{"[![:alpha:]]_test.go", "x_test.go", false},
		{"[[:digit:]]_test.go", "1_test.go", true},
		{"[[:digit:]]_test.go", "x_test.go", false},
		{"[[:alpha:]]*", "x_test.go", true},
		{"[!.]*", "x_test.go", true},
		{"[!.]*", ".sdlc", false},
		{"[!a-w]_test.go", "x_test.go", true},
		{"[!a-w]_test.go", "b_test.go", false},
		{"[üx]_test.go", "ü_test.go", true},
		{"?_test.go", "ü_test.go", true},
		// Every class POSIX names, each against a character in it and one not.
		{"[[:alnum:]]_test.go", "x_test.go", true},
		{"[[:alnum:]]_test.go", "-_test.go", false},
		{"[[:alpha:]]_test.go", "ü_test.go", true},
		{"[[:blank:]]_test.go", " _test.go", true},
		{"[[:blank:]]_test.go", "x_test.go", false},
		{"[[:cntrl:]]_test.go", "\x01_test.go", true},
		{"[[:cntrl:]]_test.go", "x_test.go", false},
		{"[[:graph:]]_test.go", "x_test.go", true},
		{"[[:graph:]]_test.go", " _test.go", false},
		{"[[:print:]]_test.go", "x_test.go", true},
		{"[[:print:]]_test.go", "\x01_test.go", false},
		{"[[:punct:]]_test.go", "]_test.go", true},
		{"[[:punct:]]_test.go", "x_test.go", false},
		{"[[:space:]]_test.go", " _test.go", true},
		{"[[:space:]]_test.go", "x_test.go", false},
		{"[[:xdigit:]]_test.go", "a_test.go", true},
		{"[[:xdigit:]]_test.go", "g_test.go", false},
		{"[[:lower:]]_test.go", "1_test.go", false},
		{"[[:upper:]]_test.go", "1_test.go", false},
	}},
	// An unclosed bracket is the bracket it is, and so is `[]` with no `]`
	// after it, which is not an empty class.
	{"AC6", []globRead{
		{"*/x[_test.go", "./internal/x[_test.go", true},
		{"*/x[_test.go", "./internal/x_test.go", false},
		{"*[]_test.go", "./internal/[]_test.go", true},
		{"*[!]_test.go", "./internal/[!]_test.go", true},
		{"x[", "x[", true},
	}, []globRead{
		{"x[_test.go", "x[_test.go", true},
		{"x[_test.go", "x_test.go", false},
		{"[]_test.go", "[]_test.go", true},
		{"[!]_test.go", "[!]_test.go", true},
		{"[", "[", true},
		{"a[]", "a[]", true},
	}},
}

func TestTheGlobReaderReadsAClassTheWayFindDoes(t *testing.T) {
	for _, group := range findReads {
		t.Run(group.ac, func(t *testing.T) {
			for _, c := range group.paths {
				re := findPattern(c.glob)
				if re == nil {
					t.Errorf("-path %q could not be read, and find reads it", c.glob)
					continue
				}
				if got := re.MatchString(c.against); got != c.want {
					t.Errorf("-path %q against %q = %v, want %v", c.glob, c.against, got, c.want)
				}
			}
			for _, c := range group.names {
				if got := nameMatches(c.glob, c.against); got != c.want {
					t.Errorf("-name %q against %q = %v, want %v", c.glob, c.against, got, c.want)
				}
			}
		})
	}
}

// The shell expands a glob's classes as find reads them, so the reading of a
// segment and the reading of -name are the same one.
func TestAC4AShellGlobReadsAClassAsFindsNameDoes(t *testing.T) {
	for _, group := range findReads {
		for _, c := range group.names {
			if strings.HasPrefix(c.against, ".") {
				// The shell's `*` and `?` do not match a leading dot, and find's do.
				continue
			}
			if seg, name := segmentMatches(c.glob, c.against), nameMatches(c.glob, c.against); seg != name {
				t.Errorf("%s: %q against %q is %v as a shell glob and %v as -name", group.ac, c.glob, c.against, seg, name)
			}
		}
	}
}

// Each of these find matches against the path beside it. A reader may read it
// as find does, or as matching more where platforms or locales disagree, or
// not at all, when the find is refused; what it may not do is read it and miss.
var findReadsOrRefuses = []globRead{
	{`*x_test\.go`, "./internal/x_test.go", true},
	{`*\x_test.go`, "./internal/x_test.go", true},
	{`*x\_test.g\o`, "./internal/x_test.go", true},
	{`*[\]x]_test.go`, "./internal/x_test.go", true},
	{`*[\]x]_test.go`, "./internal/]_test.go", true},
	{`*[\a-\z]_test.go`, "./internal/x_test.go", true},
	{"*[[=x=]]_test.go", "./internal/x_test.go", true},
	{"*[[.x.]]_test.go", "./internal/x_test.go", true},
	{"*[[.-.]]_test.go", "./internal/-_test.go", true},
	{"*[x-]_test.go", "./internal/x_test.go", true},
	{"*[x-]_test.go", "./internal/-_test.go", true},
	{"*[-x]_test.go", "./internal/-_test.go", true},
	{"*[-x]_test.go", "./internal/x_test.go", true},
	{"*[]-a]_test.go", "./internal/]_test.go", true},
	{"*[]-a]_test.go", "./internal/a_test.go", true},
	{"*[Z-x]_test.go", "./internal/x_test.go", true},
	{"*[Z-x]_test.go", "./internal/__test.go", true},
	{"*[Z-x]_test.go", "./internal/a_test.go", true},
	{"*[[:upper:]]_test.go", "./internal/x_test.go", true},
	{"*[[:lower:]]_test.go", "./internal/x_test.go", true},
	{"*[ x]_test.go", "./internal/x_test.go", true},
	{"*[\tx]_test.go", "./internal/x_test.go", true},
	{"*[\xffx]_test.go", "./internal/x_test.go", true},
}

func TestAC6EachGlobTriedIsReadAsFindReadsItOrNotReadAtAll(t *testing.T) {
	for _, c := range findReadsOrRefuses {
		if re := findPattern(c.glob); re != nil && !re.MatchString(c.against) {
			t.Errorf("-path %q was read, and misses %q, which find picks", c.glob, c.against)
		}
	}
}

// malformedGlobs end part-way through what they open, name what does not
// exist, or hold a byte that is no character.
func malformedGlobs() []string {
	return []string{
		"[", "[!", "[^", "[]", "[!]", "[^]", "[]]", "[[", "[[:", "[[:alpha", "[[:alpha:", "[[:alpha:]",
		"[[:alpha:]_x", "[[=", "[[=x", "[[=x=", "[[=x=]", "[[.", "[[.x", "[[.x.", "[[.x.]",
		"[[:]]", "[[::]]", "[[==]]", "[[..]]", "[[:foo:]]", "[[.foo.]]", "[[=xy=]]", "[[:alpha:]-z]",
		`\`, `x\`, `[\`, `[x\`, `[\]`, `*\`, `[a-`, `[a-\`, `[-`, `[]-`, `[!-]`, `[z-a]`, `[a-a]`, `[--]`,
		"\xff", "[\xff", "[\xff-\xfe]", "x\xff*", "[[=\xff=]]", "[[:\xff:]]", "[[.\xff.]]", "\xe2\x82",
		"", "*", "**", "?", "[*", "*[", "[?]", "[/]", "a/[", "[!/]", "/", "./[",
		strings.Repeat("[", 1000), strings.Repeat("[]", 500), strings.Repeat("[[:", 300),
		strings.Repeat("[!", 300), strings.Repeat(`\`, 501), strings.Repeat("[[=", 300) + "]",
	}
}

func noPanic(t *testing.T, what, glob string, read func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s(%q) panicked: %v", what, glob, r)
		}
	}()
	read()
}

// A reader that indexes past the end of a malformed glob panics, and the hook
// answers a panic by letting the call through.
func TestAMalformedGlobIsReadWithoutPanicking(t *testing.T) {
	s := freezing(xTest)
	s.Checkouts = func() CheckoutSet { return CheckoutSet{Checkouts: []string{"/work/project", "/work/main"}} }
	for i, glob := range malformedGlobs() {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			noPanic(t, "findPattern", glob, func() {
				if re := findPattern(glob); re != nil {
					re.MatchString("./internal/x_test.go")
				}
			})
			noPanic(t, "nameMatches", glob, func() { nameMatches(glob, "x_test.go") })
			noPanic(t, "segmentMatches", glob, func() { segmentMatches(glob, "x_test.go") })
			noPanic(t, "hasLiteral", glob, func() { hasLiteral(glob) })
			for _, command := range []string{
				"find . -name '" + glob + "' -delete",
				"find . -path '" + glob + "' -delete",
				"find internal -ipath '" + glob + "' -fprint0 /tmp/list",
				"find /work -name '" + glob + "' -exec rm -rf {} +",
				"find /work -path '" + glob + "' -delete",
				"rm 'internal/" + glob + "'",
			} {
				inspectNoPanic(t, command, s)
			}
		})
	}
}

// A glob is matched against paths as long as the command and the freeze make
// them, so the reader has to take time in proportion to the glob and the path.
// One that backtracks over its stars takes the ways of placing forty stars in
// two hundred characters, past 10^40 steps, on the globs here; this test cannot
// finish against it, and go test's -timeout reports it. There is no clock in it:
// a matcher that is linear does each in a few thousand steps.
func TestAPathologicalGlobIsMatchedWithoutBacktracking(t *testing.T) {
	long := strings.Repeat("a", 200)
	frozen := "internal/" + long + "_test.go"
	s := freezing(frozen)
	for _, glob := range []string{
		strings.Repeat("*a", 40) + "*b",
		strings.Repeat("*[a]", 40) + "*b",
		strings.Repeat("*[!b]", 40) + "*b",
		strings.Repeat("*[[:alpha:]]", 40) + "*b",
		strings.Repeat("*?", 40) + "*b",
		strings.Repeat("*[]a]", 40) + "*b",
	} {
		nameMatches(glob, long+"_test.go")
		segmentMatches(glob, long+"_test.go")
		hasLiteral(glob)
		if re := findPattern(glob); re != nil && re.MatchString("./"+frozen) {
			t.Errorf("-path %q matched %q, which does not end in b", glob, frozen)
		}
		for _, command := range []string{
			"find . -name '" + glob + "' -delete",
			"find . -path '" + glob + "' -delete",
			"find " + long + " -path '" + glob + "' -delete",
			"rm internal/" + glob,
		} {
			inspectNoPanic(t, command, s)
		}
	}
	// And where the glob does pick the frozen test, it is still found.
	refusedUnder(t, "find internal -name '"+strings.Repeat("*a", 40)+"*_test.go' -delete", s, wantFrozen)
	refusedUnder(t, "find internal -path '"+strings.Repeat("*[]a]", 40)+"*_test.go' -delete", s, wantFrozen)
}
