package shellpolicy

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// find reads its own globs, and a glob the classifier read differently, or
// could not read and dropped, picked a file no rule saw: with
// internal/x_test.go frozen, `find . -path '*[]x]_test.go' -delete` went
// through, because `[]x]` was taken for an empty class and the pattern built
// from it did not compile. In a glob, a `]` first in a class is one of its
// members, so `[]x]` is `]` or `x` and spells any one character of any name.
//
// Every state here is a freeze and nothing else. NewTest and ImplementerTest
// each refuse a word that ends in _test.go under a rule of their own, and would
// answer for the glob without it being read at all.

const (
	wantFrozen = "frozen-test-through-the-tool"
	wantState  = "loop-state-through-the-tool"
)

const (
	xTest   = "internal/x_test.go"
	yTest   = "internal/y_test.go"
	oneTest = "internal/1_test.go"
)

func freezing(paths ...string) State {
	return State{CommitReady: true, Frozen: paths}
}

// A triedGlob is a glob find is given. For a case that is refused, frozen is
// what every reading the story allows picks out, find's own and the one that
// matches more where platforms or locales disagree, so that the case is refused
// whether the glob is read or, read not at all, refused for what its root
// holds. For a case that is allowed, frozen is what the glob picks none of.
type triedGlob struct {
	option string // -name or -path; the other spellings of each are tried too
	shell  string // the glob as the command spells it, quotes and all
	glob   string // the glob as find is given it
	frozen []string
	before string // what the command sets in front of find, as LC_ALL=C
}

func nameTried(glob string, frozen ...string) triedGlob {
	return triedGlob{option: "-name", shell: "'" + glob + "'", glob: glob, frozen: frozen}
}

func pathTried(glob string, frozen ...string) triedGlob {
	return triedGlob{option: "-path", shell: "'" + glob + "'", glob: glob, frozen: frozen}
}

// optionsLike are the options that read a glob the way option does.
func optionsLike(option string) []string {
	if option == "-name" || option == "-iname" {
		return []string{"-name", "-iname"}
	}
	return []string{"-path", "-ipath", "-wholename", "-iwholename"}
}

// A rootTried is where find starts, and the rules a glob that cannot be read
// may be refused under from there: the freeze where the root holds a frozen
// test and no loop state, and either where it holds both, since which is
// looked at first is the classifier's to say.
type rootTried struct {
	before, root string
	rules        []string
}

var (
	asFrozen = []string{wantFrozen}
	asEither = []string{wantState, wantFrozen}
)

// rootsHoldingX hold internal/x_test.go: `.` and `..`, the directories above
// it, and every root that cannot be placed. The first is `.`.
var rootsHoldingX = []rootTried{
	{"", ".", asEither},
	{"", "..", asEither},
	{"", "internal", asFrozen},
	{"", "./internal/", asFrozen},
	{"cd internal && ", ".", asFrozen},
	{"cd internal && ", "..", asEither},
	{"", `"$ROOT"`, asEither},
	{"", "intern*", asEither},
	{"", "/work/project", asEither},
	{`cd "$OUT" && `, ".", asEither},
}

// rootsHoldingState hold the loop's own files. The first is `.`.
var rootsHoldingState = []rootTried{
	{"", ".", []string{wantState}},
	{"", "..", []string{wantState}},
	{"cd internal && ", "..", []string{wantState}},
	{"", `"$ROOT"`, []string{wantState}},
	{"", "intern*", []string{wantState}},
	{"", "/work/project", []string{wantState}},
	{`cd "$OUT" && `, ".", []string{wantState}},
}

// What find goes on to do with what it finds. A deleter changes what it finds,
// a printer writes the list of it to a file, and a reader changes nothing.
var (
	deletingActions = []string{"-delete", "-exec rm {} +", `-execdir rm -f {} \;`, `-ok rm {} \;`, `-okdir rm {} \;`}
	printingActions = []string{"-fprint /tmp/list", "-fprint0 /tmp/list", "-fprintf /tmp/list %p", "-fls /tmp/list"}
	readingActions  = []string{"-exec cat {} +", "-exec grep -l x {} +", "-print"}
)

func findAt(r rootTried, c triedGlob, option, action string) string {
	return r.before + c.before + "find " + r.root + " " + option + " " + c.shell + " " + action
}

// eachWay runs a case every way it is tried: each spelling of its option from
// the first root, and its own option from every root.
func eachWay(c triedGlob, roots []rootTried, actions []string, try func(command string, r rootTried)) {
	for _, option := range optionsLike(c.option)[1:] {
		for _, a := range actions {
			try(findAt(roots[0], c, option, a), roots[0])
		}
	}
	for _, r := range roots {
		for _, a := range actions {
			try(findAt(r, c, c.option, a), r)
		}
	}
}

// inspectNoPanic is Inspect, with a panic reported as the failure it is: the hook
// answers a panic by letting the call through.
func inspectNoPanic(t *testing.T, command string, s State) (f Finding, ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%q panicked: %v", command, r)
			f, ok = Finding{}, false
		}
	}()
	return Inspect(command, s)
}

// refusedUnder is refused for a table: it goes on past a case that fails, and
// takes every rule the case may rightly be refused under.
func refusedUnder(t *testing.T, command string, s State, rules ...string) {
	t.Helper()
	f, ok := inspectNoPanic(t, command, s)
	switch {
	case !ok:
		t.Errorf("allowed: %s", command)
	case !slices.Contains(rules, f.Rule):
		t.Errorf("%s was refused by %s, want %s (%s)", command, f.Rule, strings.Join(rules, " or "), f.Reason)
	case f.Route == "" || f.Reason == "":
		t.Errorf("%s: finding = %+v", command, f)
	}
	// US-023 AC-5 and AC-11: the row again, for AGENTS.md and .agents.
	alsoRefusedAsAgents(t, command, s, rules, func(t *testing.T, command string, s State) {
		t.Helper()
		refusedUnder(t, command, s, rules...)
	})
}

func allowedGlob(t *testing.T, command string, s State) {
	t.Helper()
	if f, ok := inspectNoPanic(t, command, s); ok {
		t.Errorf("%s was refused by %s: %s", command, f.Rule, f.Reason)
	}
	alsoAllowedAsAgents(t, command, s, func(t *testing.T, command string, s State) {
		t.Helper()
		allowedGlob(t, command, s)
	})
}

func triedName(c triedGlob) string {
	return strings.TrimSpace(c.before + c.option + " " + c.shell)
}

// refusedEveryWay is a case refused under rule from every root that holds what
// it picks, with every deleter.
func refusedEveryWay(t *testing.T, cases []triedGlob, roots []rootTried, rules func(rootTried) []string) {
	t.Helper()
	for _, c := range cases {
		t.Run(triedName(c), func(t *testing.T) {
			s := freezing(c.frozen...)
			eachWay(c, roots, deletingActions, func(command string, r rootTried) {
				refusedUnder(t, command, s, rules(r)...)
			})
		})
	}
}

func alwaysFrozen(rootTried) []string { return asFrozen }

func asTheRootHolds(r rootTried) []string { return r.rules }

var ac1Globs = []triedGlob{
	pathTried("*[]x]_test.go", xTest),
	// No glob for a test matches this word, so nothing but reading it can
	// refuse it.
	pathTried("*[]x]_test.g[o]", xTest),
	pathTried("*/[]x]_test.g[o]", xTest),
	nameTried("[]x]_test.go", xTest),
	nameTried("[]x]_test.g[o]", xTest),
	{option: "-iname", shell: "'[]X]_TEST.G[O]'", glob: "[]X]_TEST.G[O]", frozen: []string{xTest}},
	nameTried("[]x]_test.g[o]", "internal/]_test.go"),
}

func TestAC1AGlobWhoseClassStartsWithABracketPicksTheFrozenTestItMatches(t *testing.T) {
	refusedUnder(t, "find . -path '*[]x]_test.go' -delete", freezing(xTest), wantFrozen)
	refusedEveryWay(t, ac1Globs, rootsHoldingX, alwaysFrozen)
}

var ac2Globs = []triedGlob{
	pathTried("*[]a-z]_test.go", xTest),
	pathTried("*[]a-z]_test.g[o]", xTest),
	nameTried("[]a-z]_test.go", xTest),
	nameTried("[]a-z]_test.g[o]", "internal/q_test.go"),
	nameTried("[]a-z]_test.g[o]", "internal/]_test.go"),
}

func TestAC2AGlobWithARangeAfterALeadingBracketPicksTheFrozenTestItMatches(t *testing.T) {
	refusedUnder(t, "find . -path '*[]a-z]_test.go' -delete", freezing(xTest), wantFrozen)
	refusedEveryWay(t, ac2Globs, rootsHoldingX, alwaysFrozen)
}

var ac4Globs = []triedGlob{
	nameTried("[!]x]_test.go", yTest),
	pathTried("*[!]x]_test.g[o]", yTest),
	nameTried("[^]x]_test.g[o]", yTest),
	nameTried("[!x]_test.g[o]", yTest),
	nameTried("[^x]_test.g[o]", yTest),
	pathTried("*[^x]_test.g[o]", yTest),
	nameTried("[!]]_test.g[o]", xTest),
	nameTried("[[:alpha:]]_test.go", xTest),
	pathTried("*[[:alpha:]]_test.g[o]", xTest),
	nameTried("[![:alpha:]]_test.g[o]", oneTest),
	nameTried("[[:digit:]]_test.g[o]", oneTest),
	nameTried("[[:alpha:][:digit:]]_test.g[o]", oneTest),
	nameTried("[[:alnum:]]_test.g[o]", xTest),
	pathTried("*internal[/]x_test.g[o]", xTest),
}

// The globs that pick none of the frozen tests beside them, read as find reads
// them.
var ac4Misses = []triedGlob{
	nameTried("[!]x]_test.go", xTest),
	nameTried("[!]x]_test.go", "internal/]_test.go"),
	pathTried("*[!]x]_test.go", xTest),
	nameTried("[^]x]_test.go", xTest),
	nameTried("[!x]_test.go", xTest),
	nameTried("[^x]_test.go", xTest),
	nameTried("[[:alpha:]]_test.go", oneTest),
	pathTried("*[[:alpha:]]_test.go", oneTest),
	nameTried("[![:alpha:]]_test.go", xTest),
	nameTried("[[:digit:]]_test.go", xTest),
	nameTried("[]x]_test.go", yTest),
	pathTried("*[]x]_test.go", yTest),
	nameTried("[]a-m]_test.go", xTest),
	pathTried("*[]a-m]_test.go", xTest),
	nameTried("[]]_test.go", xTest),
	pathTried("*internal[!/]x_test.go", xTest),
}

func TestAC4ANegationOrANamedClassPicksWhatFindPicks(t *testing.T) {
	refusedEveryWay(t, ac4Globs, rootsHoldingX, alwaysFrozen)
	for _, c := range ac4Misses {
		t.Run("misses "+triedName(c), func(t *testing.T) {
			s := freezing(c.frozen...)
			eachWay(c, []rootTried{rootsHoldingX[0], rootsHoldingX[2]}, deletingActions, func(command string, _ rootTried) {
				allowedGlob(t, command, s)
			})
		})
	}
}

// A class read the way find reads it names the files it matches, whether or
// not the glob has a letter outside its classes.
func TestAC4AGlobOfClassesStillPicksTheFrozenTestsItMatches(t *testing.T) {
	s := freezing(xTest)
	for _, command := range []string{
		"find internal -name '*[]o]' -delete",
		"find internal -name '[]x]*' -delete",
		"find internal -name '[!]y]*' -delete",
		"find internal -name '[^]y]*' -exec rm {} +",
		"find internal -name '[[:alpha:]]*' -delete",
		"find internal -path '*[]/]*' -delete",
		`find internal -path '*[]x]*' -okdir rm {} \;`,
	} {
		refusedUnder(t, command, s, wantFrozen)
	}
}

func TestAC5AFindWhoseGlobNamesNothingProtectedIsAllowed(t *testing.T) {
	s := freezing(xTest)
	allowed(t, "find . -name '*.orig' -delete", s)
	allowed(t, "find . -name '*.orig' -delete", ready)

	roots := []rootTried{
		{"", ".", nil}, {"", "..", nil}, {"", "internal", nil}, {"", "build", nil},
		{"cd internal && ", "..", nil}, {"", `"$ROOT"`, nil}, {"", "intern*", nil},
		{"", "/work/project", nil}, {`cd "$OUT" && `, ".", nil},
	}
	for _, c := range []triedGlob{
		nameTried("*.orig"),
		nameTried("*.[oa]rig"),
		nameTried("[!.]*.orig"),
		nameTried("[[:digit:]]*.orig"),
		nameTried("[]x]_test.orig"),
		pathTried("*[]x]_test.orig"),
		pathTried("*/[]b]uild/*.o"),
		pathTried("*[!]x]_test.go.orig"),
	} {
		t.Run(triedName(c), func(t *testing.T) {
			eachWay(c, roots, slices.Concat(deletingActions, printingActions), func(command string, _ rootTried) {
				allowedGlob(t, command, s)
			})
		})
	}

	// What the find tests already allow, with the story's freeze in place.
	for _, command := range []string{
		"find . -name '*.pyc' -delete",
		"find internal -name '*.tmp' -delete",
		"find internal -name '*.tmp' -print",
		"find build -name '*' -delete",
		"find build -name '*.txt' -delete",
		"find build -name config.json -delete",
		"find . -name '*.md' -fprint /tmp/list",
		"find .sdlc -fprint /tmp/list",
		"find internal -name '*_test.go' -fls /tmp/tests",
		"find . -fprint",
		"find . -name active -print",
		"find . -name '*' -print",
		"find internal -path '*state/active' -delete",
		"find internal -path '*[]t]ests.lock' -delete",
		`find internal -name x_test.go -exec cat {} \;`,
		`find internal -name other.go -exec rm {} \;`,
		"find internal -name x_test.go -fprint /tmp/list",
		"find internal -type f",
	} {
		allowedGlob(t, command, s)
	}
}

// The globs find was tried against in the story's own spellings and in the
// forms the decisions bring into it. Each picks the frozen test beside it
// however it is read, and a reader that cannot read one refuses it.
var triedGlobs = []triedGlob{
	// A backslash escapes what follows it, outside a class and in one.
	pathTried(`*x_test\.go`, xTest),
	pathTried(`*[\]x]_test.go`, xTest),
	nameTried(`\x_test.go`, xTest),
	nameTried(`x\_test.g\o`, xTest),
	nameTried(`[\x]_test.go`, xTest),
	// Equivalence classes and collating symbols.
	nameTried("[[=x=]]_test.go", xTest),
	pathTried("*[[=x=]]_test.g[o]", xTest),
	nameTried("[[.x.]]_test.go", xTest),
	pathTried("*[[.x.]]_test.g[o]", xTest),
	// A dash first or last in a class is one of its members.
	nameTried("[x-]_test.go", xTest),
	nameTried("[-x]_test.go", xTest),
	pathTried("*[x-]_test.g[o]", xTest),
	nameTried("[]-a]_test.go", "internal/a_test.go"),
	pathTried("*[]-a]_test.g[o]", "internal/a_test.go"),
	// Range ends of different case: in the C locale Z to x runs through
	// [, \, ], ^, _ and a backtick to the lower case. The command sets its own
	// locale, and without one the reading that matches more is taken.
	{option: "-name", shell: "'[Z-x]_test.go'", glob: "[Z-x]_test.go", frozen: []string{xTest}, before: "LC_ALL=C "},
	{option: "-path", shell: "'*[Z-x]_test.g[o]'", glob: "*[Z-x]_test.g[o]", frozen: []string{xTest}, before: "LC_ALL=C "},
	pathTried("*[Z-x]_test.g[o]", xTest),
	// A class of one case, against names folded as the filesystem folds them.
	nameTried("[[:upper:]]_test.go", "internal/X_test.go"),
	nameTried("[[:upper:]]_test.go", xTest),
	pathTried("*[[:upper:]]_test.g[o]", xTest),
	nameTried("[[:lower:]]_test.go", "internal/X_test.go"),
	// White space inside the quotes is part of the glob.
	pathTried("*[ x]_test.go", xTest),
	nameTried("[ x]_test.go", xTest),
	nameTried("x_test[ .]go", xTest),
	{option: "-name", shell: `$'[\tx]_test.go'`, glob: "[\tx]_test.go", frozen: []string{xTest}},
	// A byte that is no character, in a class with the one that matters.
	{option: "-name", shell: `$'[\377\170]_test.go'`, glob: "[\xffx]_test.go", frozen: []string{xTest}},
	{option: "-path", shell: `$'*[\377\170]_test.go'`, glob: "*[\xffx]_test.go", frozen: []string{xTest}},
	// An unclosed bracket, read as the bracket it is; and the "empty class",
	// which is no class: `[]` opens one whose first member is `]`, and with no
	// `]` after that it is an unclosed bracket as well.
	nameTried("x[_test.g?", "internal/x[_test.go"),
	pathTried("*/x[_test.g?", "internal/x[_test.go"),
	nameTried("[]_test.g?", "internal/[]_test.go"),
	pathTried("*[]_test.g?", "internal/[]_test.go"),
	nameTried("[!]_test.g?", "internal/[!]_test.go"),
	pathTried("*[!]_test.g?", "internal/[!]_test.go"),
}

func TestAC6EveryGlobTriedPicksTheFrozenTestItCouldReach(t *testing.T) {
	refusedEveryWay(t, triedGlobs, rootsHoldingX, asTheRootHolds)
}

// The same globs, against the loop's own files from a root that holds them.
var stateGlobs = []triedGlob{
	pathTried("*[]t]ests.lock"),
	nameTried("[]t]ests.lock"),
	pathTried("*[]t]ests.loc[]k]"),
	pathTried("*[]s]tate/active"),
	nameTried("[]a]ctive"),
	nameTried("[!]x]ctive"),
	nameTried("[[:alpha:]]ctive"),
	nameTried("[]s]ession"),
	nameTried("[]a]cknowledged"),
	nameTried("stop-blocks.jso[]n]"),
	pathTried("*/.sdlc/[]c]onfig.json"),
	nameTried("[]g]ate-record.json"),
}

// Forms a reader may read or refuse; either way they are refused here.
var stateGlobsTried = []triedGlob{
	pathTried(`*[\]t]ests.lock`),
	pathTried(`*tests\.lock`),
	nameTried("[[=a=]]ctive"),
	nameTried("[[.a.]]ctive"),
	nameTried("[a-]ctive"),
	nameTried("[-a]ctive"),
	nameTried("[]-a]ctive"),
	{option: "-name", shell: "'[Z-a]ctive'", glob: "[Z-a]ctive", before: "LC_ALL=C "},
	nameTried("[[:upper:]]ctive"),
	pathTried("*[ t]ests.lock"),
	{option: "-name", shell: `$'[\377\141]ctive'`, glob: "[\xffa]ctive"},
}

func TestAC6AGlobWithABracketFirstInItsClassPicksLoopStateFromTheRoot(t *testing.T) {
	refusedUnder(t, "find . -path '*[]t]ests.lock' -delete", ready, wantState)
	refusedUnder(t, "find . -path '*[]t]ests.lock' -delete", freezing(xTest), wantState)
	refusedUnder(t, "find . -name '[]C]LAUDE.md' -delete", ready, "protected-path-through-the-tool")
	for _, s := range []State{ready, freezing(xTest)} {
		for _, c := range stateGlobs {
			t.Run(fmt.Sprintf("%s frozen %v", triedName(c), s.Frozen), func(t *testing.T) {
				eachWay(c, rootsHoldingState, deletingActions, func(command string, r rootTried) {
					refusedUnder(t, command, s, r.rules...)
				})
			})
		}
	}
	for _, c := range stateGlobsTried {
		t.Run(triedName(c), func(t *testing.T) {
			eachWay(c, rootsHoldingState, deletingActions, func(command string, r rootTried) {
				refusedUnder(t, command, ready, r.rules...)
			})
		})
	}
}

// Globs that a reader may not be able to read at all, beyond those above.
var hardGlobs = []string{
	"[[:foo:]]_test.go", "*[[:foo:]]*", "[[.foo.]]_test.go", "[[=xy=]]_test.go", "[[:alpha:]_x",
	`x_test.go\`, `*\`, "[z-a]_test.go", "\xff_test.go", "*[\xff]*", "[", "[!", "[[:", "[[:alpha", "[]", "[!]",
}

// unreadableTried is every glob tried whose -path reading findPattern cannot make
// into a pattern, with the option it is given under.
func unreadableTried() []triedGlob {
	var out []triedGlob
	all := slices.Concat(ac1Globs, ac2Globs, ac4Globs, triedGlobs, stateGlobs, stateGlobsTried)
	for _, g := range hardGlobs {
		all = append(all, nameTried(g), pathTried(g))
	}
	for _, c := range all {
		if findPattern(c.glob) == nil {
			out = append(out, c)
		}
	}
	return out
}

// A glob that cannot be made into a pattern says nothing about which files
// find goes on to change, so from a root that holds a frozen test or the
// loop's files, changing them is refused: deleting what it finds, running a
// writer on it, and writing the list of it to a file.
func TestAC3AGlobThatCannotBeReadIsRefusedWhenFindGoesOnToChangeFiles(t *testing.T) {
	changers := slices.Concat(deletingActions, printingActions)
	for _, c := range unreadableTried() {
		t.Run(triedName(c), func(t *testing.T) {
			s := freezing(xTest)
			for _, r := range rootsHoldingX {
				for _, a := range changers {
					refusedUnder(t, findAt(r, c, c.option, a), s, r.rules...)
				}
			}
			for _, r := range rootsHoldingState {
				for _, a := range changers {
					refusedUnder(t, findAt(r, c, c.option, a), ready, r.rules...)
				}
			}
		})
	}
}

// Reading what find finds changes nothing, whatever the glob: `-exec cat`, and
// a list written to a file where the glob is read and picks only what is
// listed.
func TestAC3AFindThatOnlyReadsWhatItFindsIsAllowedWhateverItsGlob(t *testing.T) {
	s := freezing(xTest)
	all := slices.Concat(ac1Globs, ac2Globs, ac4Globs, triedGlobs, stateGlobs, stateGlobsTried)
	for _, g := range hardGlobs {
		all = append(all, nameTried(g), pathTried(g))
	}
	roots := []rootTried{rootsHoldingX[0], rootsHoldingX[2], rootsHoldingX[6]}
	for _, c := range all {
		for _, r := range roots {
			for _, a := range readingActions {
				allowedGlob(t, findAt(r, c, c.option, a), s)
			}
		}
	}
	for _, command := range []string{
		"find . -path '*[]x]_test.go' -fprint /tmp/list",
		"find internal -path '*[]x]_test.go' -fprint /tmp/list",
		"find internal -name '[]x]_test.go' -fls /tmp/list",
		"find . -path '*[]t]ests.lock' -fprint /tmp/list",
	} {
		allowedGlob(t, command, s)
	}
}

// expandedGlobs are globs the shell fills in before find sees them, from a
// variable or from what a command prints. What they pick is known only when
// the command runs.
var expandedGlobs = []triedGlob{
	{option: "-name", shell: `"$G"`},
	{option: "-path", shell: `"$G"`},
	{option: "-name", shell: `"${G}"`},
	{option: "-name", shell: `$G`},
	{option: "-name", shell: `"x_test$G"`},
	{option: "-path", shell: `"*/$G"`},
	{option: "-name", shell: `"$(printf x_test.go)"`},
	{option: "-path", shell: `"$(printf '*x_test.go')"`},
	{option: "-name", shell: "\"`printf x_test.go`\""},
}

func TestAC3AGlobTheShellFillsInIsRefusedWhenFindGoesOnToChangeFiles(t *testing.T) {
	s := freezing(xTest)
	for _, command := range []string{
		`find . -name "$G" -delete`,
		`find . -path "$G" -delete`,
		`find . -name "$(printf x_test.go)" -delete`,
		`G='x_test.go'; find . -name "$G" -delete`,
	} {
		refusedUnder(t, command, s, asEither...)
	}
	changers := slices.Concat(deletingActions, printingActions)
	for _, c := range expandedGlobs {
		t.Run(triedName(c), func(t *testing.T) {
			eachWay(c, rootsHoldingX, changers, func(command string, r rootTried) {
				refusedUnder(t, command, s, r.rules...)
			})
			eachWay(c, rootsHoldingState, changers, func(command string, r rootTried) {
				refusedUnder(t, command, ready, r.rules...)
			})
		})
	}
}

func TestAC3AFindThatOnlyReadsWhatAFilledInGlobPicksIsAllowed(t *testing.T) {
	s := freezing(xTest)
	roots := []rootTried{rootsHoldingX[0], rootsHoldingX[2], rootsHoldingX[6]}
	for _, c := range expandedGlobs {
		eachWay(c, roots, readingActions, func(command string, _ rootTried) {
			allowedGlob(t, command, s)
		})
	}
}
