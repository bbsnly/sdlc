package shellpolicy

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// find joins its tests with operators, and what it picks is what the whole
// expression picks: a test under `!` or `-not` picks the files the test does
// not match, and each side of `-o` or `-or` picks files of its own. This is the
// table of the forms the reading was tried against. findglob_test.go holds the
// globs; nothing here turns on how a glob is read, only on what stands between
// the tests.
//
// A case that is refused is refused from every directory that holds a frozen
// test or the loop's files; a case that is allowed picks none of them, or
// changes nothing.

const wantProtected = "protected-path-through-the-tool"

// The spellings of a negation, and the options that are given a glob.
var (
	negations = []string{"!", `\!`, "'!'", `"!"`, "-not"}
	globTests = []string{"-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename"}
)

// notHolding starts find where nothing the rules protect is below it.
var notHolding = []rootTried{{"", "build", nil}, {"cd build && ", ".", nil}, {"", "build/out", nil}}

// findDoing is a find from r with an expression and what it goes on to do.
// The action stands where the expression says ACTION, and last where it does
// not say.
func findDoing(r rootTried, expression, action string) string {
	if !strings.Contains(expression, "ACTION") {
		expression += " ACTION"
	}
	return r.before + "find " + r.root + " " + strings.ReplaceAll(expression, "ACTION", action)
}

// inParallel has a test that takes seconds under the race detector run beside
// the others marked so. One after another they took the package to go test's
// ten minutes on a loaded macOS runner, where it was killed. Each of them only
// calls Inspect, with a State of its own, so none can disturb another; and they
// start once every other test has ended, so the one test here with a clock in
// it is not slowed.
func inParallel(t *testing.T) {
	t.Helper()
	t.Parallel()
}

// pickedFrom are the rules a find may be refused under for what its expression
// picks from r. An expression that picks every file picks CLAUDE.md and .git
// along with the loop's files, and which is named first is the classifier's to
// say.
func pickedFrom(r rootTried) []string {
	if slices.Contains(r.rules, wantState) {
		return append(slices.Clone(r.rules), wantProtected)
	}
	return r.rules
}

// refusedFromEveryRoot is an expression refused with each action from every
// root that holds a frozen test, and from every root that holds the loop's
// files with no freeze at all.
func refusedFromEveryRoot(t *testing.T, expression string, actions []string, rules func(rootTried) []string) {
	t.Helper()
	for _, a := range actions {
		for _, r := range rootsHoldingX {
			refusedUnder(t, findDoing(r, expression, a), freezing(xTest), rules(r)...)
		}
		for _, r := range rootsHoldingState {
			refusedUnder(t, findDoing(r, expression, a), ready, rules(r)...)
		}
	}
}

// allowedFromEveryRoot is the same roots and states, with the find allowed.
func allowedFromEveryRoot(t *testing.T, expression string, actions []string) {
	t.Helper()
	for _, a := range actions {
		for _, r := range slices.Concat(rootsHoldingX, notHolding) {
			allowedGlob(t, findDoing(r, expression, a), freezing(xTest))
		}
		for _, r := range slices.Concat(rootsHoldingState, notHolding) {
			allowedGlob(t, findDoing(r, expression, a), ready)
		}
	}
}

// allowedBelowNothingProtected is the find allowed from where nothing the
// rules protect is below it.
func allowedBelowNothingProtected(t *testing.T, expression string, actions []string) {
	t.Helper()
	for _, a := range actions {
		for _, r := range notHolding {
			allowedGlob(t, findDoing(r, expression, a), freezing(xTest))
			allowedGlob(t, findDoing(r, expression, a), ready)
		}
	}
}

func TestAC1ANegatedTestIsJudgedByTheFilesItLeavesIn(t *testing.T) {
	inParallel(t)
	refusedUnder(t, "find . ! -name '*.orig' -delete", ready, wantState, wantProtected)
	refusedUnder(t, "find internal -not -name '*.orig' -delete", freezing(xTest), wantFrozen)
	for _, not := range negations {
		for _, option := range globTests {
			expression := not + " " + option + " '*.orig'"
			t.Run(expression, func(t *testing.T) {
				refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
			})
		}
	}
}

// What a negation governs is the one test, or the one group, after it. The
// test beside it, in front or behind, narrows what is left in and does not
// undo it.
var negatedBesideOtherTests = []string{
	"-type f ! -name '*.orig'",
	"! -name '*.orig' -type f",
	"-newer ref ! -name '*.orig'",
	"-mindepth 1 -not -name '*.orig'",
	"! -name '*.orig' ! -name '*.bak'",
	"-not -name '*.orig' -not -path '*.bak'",
	"! ! ! -name '*.orig'",
	"-not -not -not -path '*.orig'",
	// The other operators that stand between tests: read, or not read and
	// refused for it.
	"-type f -a ! -name '*.orig'",
	"-type f -and -not -name '*.orig'",
	"-name '*.orig' , -type f",
}

func TestAC1ANegationGovernsTheOneTestAfterItAndNoOther(t *testing.T) {
	inParallel(t)
	for _, expression := range negatedBesideOtherTests {
		t.Run(expression, func(t *testing.T) {
			refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
		})
	}
}

// A glob under negation leaves a file out only where find is certain to match
// it. The reading of a glob that matches more than find does, in case, by
// folding, in a range a locale orders, or past a bracket nothing closes, is the
// wrong way round here: what it matches and find does not is left in.
var negatedGlobsFindReadsMoreNarrowly = []struct {
	expression string
	frozen     string
}{
	{"! -name X_TEST.GO", xTest},
	{"! -name '[X]_test.go'", xTest},
	{"-not -path '*/X_TEST.GO'", xTest},
	{"! -name 'strasse_test.go'", "internal/straße_test.go"},
	{"! -name '[a-m]_test.go'", "internal/é_test.go"},
	{"! -name '[a-m]_test.go'", xTest},
	{"! -name '[[=a=]]_test.go'", xTest},
	{"-not -path '*[[=a=]]_test.go'", xTest},
	{"! -name '[{-b]_test.go'", xTest},
	{"! -name 'x[_test.go'", "internal/x[_test.go"},
	{"! -name '[[:foo:]]_test.go'", xTest},
	{"! -name '" + strings.Repeat("a", maxGlob+1) + "'", xTest},
	{"! -path '*" + strings.Repeat("?", maxGlob) + "'", xTest},
}

func TestAC1ANegatedGlobLeavesInWhatFindItselfMightNotMatch(t *testing.T) {
	roots := []rootTried{rootsHoldingX[0], rootsHoldingX[2], rootsHoldingX[4], rootsHoldingX[6]}
	for _, c := range negatedGlobsFindReadsMoreNarrowly {
		name := c.expression
		if len(name) > 60 {
			name = name[:60]
		}
		t.Run(name+" with "+c.frozen, func(t *testing.T) {
			for _, r := range roots {
				for _, a := range deletingActions {
					refusedUnder(t, findDoing(r, c.expression, a), freezing(c.frozen), pickedFrom(r)...)
				}
			}
		})
	}
}

// An alternative with no name or path test in it picks every file, and one
// under negation picks what its test leaves in. Each is read wherever it
// stands, with the action in it or not, and after the command an -exec runs.
var alternativesThatPick = []string{
	"-name '*.orig' -o -type f",
	"-name '*.orig' -or -type f",
	"-name '*.orig' -o -newer ref",
	"-name '*.orig' -o -size +1k",
	"-name '*.orig' -o -true",
	"-name '*.orig' -o -regex '[a-z]+'",
	"-name '*.orig' -o ! -name '*.bak'",
	"-name '*.orig' -or -not -path '*.bak'",
	"-name '*.orig' -o -name '*.bak' -o -type f",
	"-type f -o -name '*.orig'",
	"-name '*.orig' -o -type f -o -name '*.bak'",
	"-type f ACTION -o -name '*.orig'",
	"! -name '*.orig' ACTION -or -name '*.bak'",
	`-name '*.orig' -exec rm {} \; -o -type f`,
	"-name '*.orig' -exec rm {} + -o -type f",
	`-name '*.orig' -exec cat {} \; -o -type f`,
	`-name '*.orig' -ok rm {} \; -or ! -name '*.bak'`,
	`-name '*.orig' -execdir mv {} {}.old \; -o -newer ref`,
}

func TestAC2EachAlternativeIsReadAndOneThatPicksAProtectedFileRefuses(t *testing.T) {
	inParallel(t)
	refusedUnder(t, "find . -name '*.orig' -o -type f -delete", ready, wantState, wantProtected)
	refusedUnder(t, "find internal -name '*.orig' -or -type f -delete", freezing(xTest), wantFrozen)
	for _, expression := range alternativesThatPick {
		t.Run(expression, func(t *testing.T) {
			refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
		})
	}
}

// The alternative that names the protected file may be the first, the last or
// one between.
func TestAC2AnAlternativeThatNamesAFrozenTestRefusesWhereverItStands(t *testing.T) {
	roots := []rootTried{rootsHoldingX[0], rootsHoldingX[2], rootsHoldingX[6]}
	for _, expression := range []string{
		"-name '[x]_test.go' -o -name '*.orig' -o -name '*.bak'",
		"-name '*.orig' -o -name '[x]_test.go' -o -name '*.bak'",
		"-name '*.orig' -o -name '*.bak' -o -name '[x]_test.go'",
		"-name '*.orig' -or -path '*/[x]_test.go'",
		"-path '*/[x]_test.go' -or -name '*.orig'",
		`\( -name '*.orig' -o -name '[x]_test.go' \)`,
		`\( -name '*.orig' \) -o \( -name '[x]_test.go' \)`,
	} {
		t.Run(expression, func(t *testing.T) {
			for _, r := range roots {
				for _, a := range deletingActions {
					refusedUnder(t, findDoing(r, expression, a), freezing(xTest), r.rules...)
				}
			}
		})
	}
}

// A group, in each spelling of its parentheses. Whether the reader follows
// the group or refuses the find for not following it, each of these picks a
// protected file and is refused.
var groupsThatPick = []string{
	`\( ! -name '*.orig' \)`,
	`'(' ! -name '*.orig' ')'`,
	`"(" -not -name '*.orig' ")"`,
	`! \( -name '*.orig' -o -name '*.bak' \)`,
	`-not '(' -name '*.orig' -or -name '*.bak' ')'`,
	`\( -name '*.orig' -o -type f \)`,
	`\( -name '*.orig' \) -o \( -type f \)`,
	`\( -name '*.orig' -o -name '*.bak' \) -o -type f`,
	`-type f \( ! -name '*.orig' \)`,
	`\( \( ! -name '*.orig' \) \)`,
	`\( -type f \( -name '*.orig' -o ! -name '*.bak' \) \)`,
	`! \( ! \( ! -name '*.orig' \) \)`,
	`\( ! -name '*.orig' ACTION \)`,
	`\( -name '*.orig' -o -type f ACTION \) -o -name '*.bak'`,
}

func TestAC3AGroupThatPicksAProtectedFileIsRefusedWhetherOrNotItIsFollowed(t *testing.T) {
	inParallel(t)
	for _, expression := range groupsThatPick {
		t.Run(expression, func(t *testing.T) {
			refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
		})
	}
}

// With no directory given find starts where the command runs, whatever the
// first word of its expression is: an operator is no directory.
func TestAC1AFindWithNoDirectoryIsJudgedFromWhereTheCommandRuns(t *testing.T) {
	here := rootTried{"", "", asEither}
	inInternal := rootTried{"cd internal && ", "", asFrozen}
	state := rootTried{"", "", []string{wantState}}
	for _, expression := range []string{
		"! -name '*.orig'",
		`\! -name '*.orig'`,
		"'!' -name '*.orig'",
		`"!" -path '*.orig'`,
		"-not -name '*.orig'",
		`\( ! -name '*.orig' \)`,
		"'(' -name '*.orig' -o -type f ')'",
		"-name '*.orig' -o -type f",
		"-type f ! -name '*.orig'",
		"-L ! -name '*.orig'",
		"-H -not -name '*.orig'",
	} {
		t.Run(expression, func(t *testing.T) {
			for _, a := range deletingActions {
				refusedUnder(t, findDoing(here, expression, a), freezing(xTest), pickedFrom(here)...)
				refusedUnder(t, findDoing(inInternal, expression, a), freezing(xTest), pickedFrom(inInternal)...)
				refusedUnder(t, findDoing(state, expression, a), ready, pickedFrom(state)...)
			}
		})
	}
	// And where nothing protected is below the command, it is allowed.
	for _, a := range deletingActions {
		allowedGlob(t, "cd build && find ! -name '*.orig' "+a, freezing(xTest))
		allowedGlob(t, "cd build && find -name '*.orig' -o -type f "+a, ready)
	}
}

// What a find with operators picks none of, it may change as before: a
// positive test narrowed by a negated one, alternatives that each name
// something harmless, a pruned directory, and a group of any of them.
var expressionsThatPickNothingProtected = []string{
	"-name '*.orig' ! -name keep.orig",
	"-name '*.orig' -not -name 'keep*'",
	"! -name 'keep*' -name '*.orig'",
	"-path '*.orig' ! -path '*keep.orig'",
	"! -type d -name '*.orig'",
	"-name '*.orig' ! -type d",
	"-not -newer ref -name '*.orig'",
	"-type f ! -empty -name '*.orig'",
	"-newer ref -type f -name '*.orig'",
	"-name '*.orig' -o -name '*.bak'",
	"-name '*.orig' -or -name '*.bak' -o -name '*.rej'",
	"-type f -name '*.orig' -o -type f -name '*.bak'",
	"-name '*.orig' ACTION -o -name '*.bak'",
	"-name '*.orig' ACTION -o -name '*.bak' ACTION",
	"-name node_modules -prune -o -name '*.pyc'",
	"-type d -name node_modules -prune -o -type f -name '*.pyc'",
	`\( -name '*.orig' -o -name '*.bak' \)`,
	`'(' -name '*.orig' -or -name '*.bak' ')'`,
	`"(" -name '*.orig' -o -name '*.bak' ")"`,
	`-type f \( -name '*.orig' -o -name '*.bak' \)`,
	`\( -name '*.orig' -o -name '*.bak' \) ! -name keep.orig`,
	`\( -name '*.orig' \) -o \( -name '*.bak' \)`,
	// A word that looks like an operator, where find reads a value.
	"-name '!'",
	"-name '-o'",
	"-name '('",
	"-newer '!' -name '*.orig'",
	"-newer -o -name '*.orig'",
}

func TestAC4AFindWhoseOperatorsPickNothingProtectedIsAllowed(t *testing.T) {
	inParallel(t)
	allowedGlob(t, "find . ! -name '*.orig' -print", ready)
	allowedGlob(t, "find . ! -name '*.orig' -print", freezing(xTest))
	all := slices.Concat(deletingActions, printingActions, readingActions)
	for _, expression := range expressionsThatPickNothingProtected {
		t.Run(expression, func(t *testing.T) {
			allowedFromEveryRoot(t, expression, all)
		})
	}
}

// A find that only reads what it finds changes nothing, whatever its operators
// pick; and one that changes what it finds is allowed where nothing protected
// is below it.
func TestAC4AFindThatOnlyReadsOrStartsBelowNothingProtectedIsAllowedWhateverItsOperatorsPick(t *testing.T) {
	inParallel(t)
	var all []string
	for _, not := range negations {
		all = append(all, not+" -name '*.orig'")
	}
	all = slices.Concat(all, negatedBesideOtherTests, alternativesThatPick, groupsThatPick)
	for _, expression := range all {
		t.Run(expression, func(t *testing.T) {
			if !strings.Contains(expression, "-exec") && !strings.Contains(expression, "-ok") {
				allowedFromEveryRoot(t, expression, readingActions)
			}
			allowedBelowNothingProtected(t, expression, slices.Concat(deletingActions, printingActions))
		})
	}
}

// A find that only writes its list to a file is not judged by what its
// expression picks: the same expression that is refused with a deleter is
// allowed with a printer, from the same roots. The file it prints to is still
// a file it writes.
func TestAC4AFindThatOnlyWritesItsListToAFileIsAllowedWhateverAReadableExpressionPicks(t *testing.T) {
	inParallel(t)
	for _, expression := range []string{
		"! -name '*.orig'",
		`\! -name '*.orig'`,
		"'!' -name '*.orig'",
		"-not -path '*.orig'",
		"-type f ! -name '*.orig'",
		"! -name '*.orig' ! -name '*.bak'",
		"-name '*.orig' -o -type f",
		"-name '*.orig' -or -newer ref",
		"-name '*.orig' -o ! -name '*.bak'",
	} {
		t.Run(expression, func(t *testing.T) {
			allowedFromEveryRoot(t, expression, printingActions)
			refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
		})
	}
	s := freezing(xTest)
	refusedUnder(t, "find . ! -name '*.orig' -fprint .sdlc/state/active", s, wantState)
	refusedUnder(t, "find . -name '*.orig' -o -type f -fprint0 .sdlc/state/tests.lock", s, wantState)
	refusedUnder(t, `find . \( -name '*.orig' -o -name '*.bak' \) -fls CLAUDE.md`, s, wantProtected)
	refusedUnder(t, "find internal -not -name '*.orig' -fprint "+xTest, s, wantFrozen)
}

// A word of the expression that the shell fills in or unescapes may be an
// operator by the time find reads it. The forms are a variable, a command
// substitution and a quote the shell unescapes; the places are after a test
// that takes no further value: between two tests, before a name test, before
// the action, and after an option whose one value has been given.
var (
	filledWords = []string{`"$OP"`, `$OP`, `"${OP}"`, `"$(printf x)"`, `$(printf x)`, "\"`printf x`\"", `$'\041'`, `$'-\157'`}
	filledAt    = []string{
		"-name '*.orig' WORD -name '*.bak'",
		"-type f WORD -name '*.orig'",
		"-mindepth 1 WORD -name '*.orig'",
		"-name '*.orig' WORD",
		"-name '*.orig' -type f WORD -newer ref",
	}
	// A word only part of which the shell fills in, an option the table of
	// options does not know, and so cannot say how many values it takes, and a
	// second word after an option that takes one.
	partlyFilled = []string{
		`-name '*.orig' -ty"$K" f`,
		`-name '*.orig' "-$K" -name '*.bak'`,
		`-name '*.orig' -"${K}" -name '*.bak'`,
		`-name '*.orig' -na$'\155'e '*.bak'`,
		`-name '*.orig' -o"$K" -name '*.bak'`,
		`-frobnicate "$V" -name '*.orig'`,
		`-name '*.orig' -nosuchtest $V`,
		`-newer "$REF" "$OP" -name '*.orig'`,
		`-name '*.orig' -exec cat {} \; "$OP" -name '*.bak'`,
		`-name '*.orig' -exec cat {} + $OP`,
	}
	// Forms that are refused whether the word is read as the shell leaves it
	// or refused for what the shell does to it.
	unescapedEitherWay = []string{
		`-n\ot -name '*.orig'`,
		`-name '*.orig' \-o -type f`,
		`-name '*.orig' -\o -type f`,
		`-name '*.orig' {-o,-true}`,
	}
)

func filledExpressions() []string {
	var out []string
	for _, at := range filledAt {
		for _, w := range filledWords {
			out = append(out, strings.ReplaceAll(at, "WORD", w))
		}
	}
	return slices.Concat(out, partlyFilled)
}

func asTheRootHoldsIt(r rootTried) []string { return r.rules }

func TestAC3AWordTheShellFillsInWhereAnOperatorCouldBeIsRefusedWhenFindGoesOnToChangeFiles(t *testing.T) {
	inParallel(t)
	refusedUnder(t, `find . -name '*.orig' "$OP" -name '*.bak' -delete`, ready, wantState)
	refusedUnder(t, `find internal -name '*.orig' "$OP" -name '*.bak' -fprint /tmp/list`, freezing(xTest), wantFrozen)
	changers := slices.Concat(deletingActions, printingActions)
	for _, expression := range filledExpressions() {
		t.Run(expression, func(t *testing.T) {
			refusedFromEveryRoot(t, expression, changers, asTheRootHoldsIt)
		})
	}
	for _, expression := range unescapedEitherWay {
		t.Run(expression, func(t *testing.T) {
			refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
		})
	}
	// With no directory given, the expression starts at the first word.
	for _, a := range changers {
		refusedUnder(t, `find -name '*.orig' "$OP" -name '*.bak' `+a, freezing(xTest), asEither...)
		refusedUnder(t, `find -name '*.orig' "$OP" -name '*.bak' `+a, ready, wantState)
	}
}

func TestAC3AWordTheShellFillsInIsAllowedWhereFindOnlyReadsOrNothingProtectedIsBelow(t *testing.T) {
	inParallel(t)
	for _, expression := range slices.Concat(filledExpressions(), unescapedEitherWay) {
		t.Run(expression, func(t *testing.T) {
			if !strings.Contains(expression, "-exec") {
				allowedFromEveryRoot(t, expression, readingActions)
			}
			allowedBelowNothingProtected(t, expression, slices.Concat(deletingActions, printingActions))
		})
	}
}

// The spellings every find user writes for the operators themselves are the
// operators, not words the shell fills in: a find that only lists is allowed
// with them.
func TestAC3TheQuotedAndEscapedSpellingsOfAnOperatorAreReadAsTheOperator(t *testing.T) {
	for _, expression := range []string{
		`\! -name '*.orig'`,
		`'!' -name '*.orig'`,
		`"!" -name '*.orig'`,
		`\( -name '*.orig' -o -name '*.bak' \)`,
		`'(' -name '*.orig' -o -name '*.bak' ')'`,
		`"(" -name '*.orig' "-o" -name '*.bak' ")"`,
		`-name '*.orig' '-o' -name '*.bak'`,
		`-name '*.orig' "-or" -name '*.bak'`,
		`'-not' -name '*.orig'`,
	} {
		t.Run(expression, func(t *testing.T) {
			allowedFromEveryRoot(t, expression, slices.Concat(printingActions, readingActions))
		})
	}
}

// The options of find that take a value, and how many. One per line of the
// table the reading needs: a value the shell fills in is no place an operator
// could be, and the word after the last value is.
var (
	// One value, in GNU find and in BSD find alike.
	oneValue = []string{
		"-amin", "-anewer", "-atime", "-cmin", "-cnewer", "-ctime", "-fstype", "-gid", "-group", "-ilname",
		"-inum", "-links", "-lname", "-mmin", "-mtime", "-newer", "-perm", "-regex", "-iregex", "-samefile",
		"-size", "-type", "-uid", "-user", "-maxdepth", "-mindepth", "-newermt", "-newerat", "-newerct",
	}
	// One value, in one of the two only.
	oneValueGNU = []string{"-used", "-xtype", "-printf"}
	oneValueBSD = []string{"-flags", "-Btime", "-Bmin", "-Bnewer"}
	// No value in one find or the other, so the next word is a test, an
	// action or an operator.
	noValue = []string{
		"-empty", "-true", "-false", "-print", "-print0", "-prune", "-daystart", "-follow", "-xdev", "-mount",
		"-noleaf", "-nouser", "-nogroup", "-readable", "-writable", "-executable", "-ls", "-quit", "-depth",
		"-delete",
	}
	// Not an option of either find.
	noOption = []string{"-frobnicate", "-nosuchtest", "-Name", "-names"}
)

func TestAC3AValueTheShellFillsInIsAllowedAndTheWordAfterTheLastValueIsNot(t *testing.T) {
	states := []State{ready, freezing(xTest)}
	actions := []string{"-delete", "-exec rm {} +", "-fprint /tmp/list"}
	for _, option := range slices.Concat(oneValue, oneValueGNU, oneValueBSD) {
		t.Run(option, func(t *testing.T) {
			for _, s := range states {
				for _, a := range actions {
					allowedGlob(t, "find . "+option+` "$V" -name '*.orig' `+a, s)
					allowedGlob(t, "find . -name '*.orig' "+option+` $V `+a, s)
					refusedUnder(t, "find . "+option+` "$V" "$W" -name '*.orig' `+a, s, asEither...)
					refusedUnder(t, "find . -name '*.orig' "+option+` "$V" $W `+a, s, asEither...)
				}
			}
		})
	}
	for _, option := range slices.Concat(noValue, noOption, []string{"-context", "-regextype"}) {
		t.Run(option+" twice over", func(t *testing.T) {
			for _, s := range states {
				for _, a := range actions {
					refusedUnder(t, "find . -name '*.orig' "+option+` "$V" "$W" `+a, s, asEither...)
				}
			}
		})
	}
	for _, option := range slices.Concat(noValue, noOption) {
		t.Run(option, func(t *testing.T) {
			for _, s := range states {
				for _, a := range actions {
					refusedUnder(t, "find . -name '*.orig' "+option+` "$V" `+a, s, asEither...)
				}
			}
		})
	}
	// A value written out leaves the find as readable as it was.
	for _, option := range slices.Concat(oneValue, oneValueGNU, oneValueBSD) {
		allowedGlob(t, "find . "+option+" 1 -name '*.orig' -delete", freezing(xTest))
	}
	// The file a find prints to is one value, and its format a second.
	for _, s := range states {
		for _, option := range []string{"-fprint", "-fprint0", "-fls"} {
			allowedGlob(t, "find . -name '*.orig' "+option+` "$OUT"`, s)
			refusedUnder(t, "find . -name '*.orig' "+option+` "$OUT" "$W"`, s, asEither...)
		}
		allowedGlob(t, `find . -name '*.orig' -fprintf "$OUT" "$FORMAT"`, s)
		refusedUnder(t, `find . -name '*.orig' -fprintf "$OUT" "$FORMAT" "$W"`, s, asEither...)
	}
}

// The command an -exec runs is given words of its own, and a variable among
// them is no operator: refusing it would refuse every find that moves what it
// finds to a directory the shell names.
func TestAC4AVariableAmongTheArgumentsOfTheCommandAnExecRunsIsAllowed(t *testing.T) {
	for _, s := range []State{ready, freezing(xTest)} {
		for _, command := range []string{
			`find . -name '*.orig' -exec mv {} "$DEST" \;`,
			`find . -name '*.orig' -execdir mv {} "$DEST" \;`,
			`find . -name '*.orig' ! -name keep.orig -exec mv {} "$DEST" \;`,
			`find . -name '*.orig' -exec grep -e -o {} \;`,
			`find . -name '*.orig' -exec grep "$PATTERN" {} +`,
			`find . ! -name '*.orig' -exec grep "$PATTERN" {} +`,
		} {
			allowedGlob(t, command, s)
		}
	}
}

// The refusal of a word the shell fills in names the word as the command
// spells it and the directory, under the rule of what the directory holds, and
// its route is about the word: the route for a glob does not fit it.
func TestAC3TheRefusalOfAnExpressionThatCannotBeReadNamesTheWordAndTheDirectoryAndSaysWhatToDo(t *testing.T) {
	for _, c := range []struct {
		command string
		s       State
		rule    string
		names   []string
		wayOut  string
	}{
		{`find internal -name '*.orig' "$OP" -name '*.bak' -delete`, freezing(xTest), wantFrozen, []string{"$OP", "`internal`"}, "sdlc unfreeze"},
		{`find internal -name '*.orig' "$OP" -name '*.bak' -fprint /tmp/list`, freezing(xTest), wantFrozen, []string{"$OP", "`internal`"}, "sdlc unfreeze"},
		{`find .. -type f "$NOT" -name '*.orig' -delete`, ready, wantState, []string{"$NOT", "`..`"}, "sdlc"},
		{`find . -name '*.orig' -frobnicate "$VALUE" -fls /tmp/list`, ready, wantState, []string{"$VALUE"}, "sdlc"},
	} {
		f, ok := inspectNoPanic(t, c.command, c.s)
		if !ok {
			t.Errorf("allowed: %s", c.command)
			continue
		}
		if f.Rule != c.rule {
			t.Errorf("%s was refused by %s, want %s (%s)", c.command, f.Rule, c.rule, f.Reason)
		}
		for _, name := range c.names {
			if !strings.Contains(f.Reason, name) {
				t.Errorf("%s: the reason does not name %s: %s", c.command, name, f.Reason)
			}
		}
		if !strings.Contains(f.Route, c.wayOut) {
			t.Errorf("%s: the route does not end with the rule's own way out (%s): %s", c.command, c.wayOut, f.Route)
		}
		if strings.HasPrefix(f.Route, "give find a glob") {
			t.Errorf("%s: the route is the one for a glob, and the word is no glob: %s", c.command, f.Route)
		}
	}
}

// The same reading against the tree of checkouts: a find that starts at
// another checkout, or above one, takes it away where its operators leave the
// checkout in.
func TestAC1AndAC2AFindAtOrAboveAnotherCheckoutIsJudgedByWhatItsOperatorsLeaveIn(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"find ../B-2 ! -name '*.orig' -delete",
		"find ../B-2 -not -name '*.orig' -exec rm -rf {} +",
		`find ../B-2 \( ! -name '*.orig' \) -delete`,
		"find ../B-2 -name '*.orig' -o -type d -exec rm -rf {} +",
		"find ../B-2 -type d ! -name '*.orig' -exec rm -rf {} +",
	} {
		f, ok := inspectNoPanic(t, command, s)
		switch {
		case !ok:
			t.Errorf("allowed: %s", command)
		case f.Rule != wantState || f.Route == "":
			t.Errorf("%s: finding = %+v", command, f)
		case !strings.Contains(f.Reason, "B-2"):
			t.Errorf("%s: the reason does not name the checkout: %s", command, f.Reason)
		}
	}
	for _, command := range []string{
		"find .. ! -name 'C-*' -delete",
		"find .. -maxdepth 1 ! -name 'C-*' -exec rm -rf {} +",
		"find .. -maxdepth 1 -not -iname 'c-*' -exec rm -rf {} +",
		"find .. -name 'C-*' -o -type d -exec rm -rf {} +",
		"find .. -maxdepth 1 -name 'C-*' -or -maxdepth 1 -exec rm -rf {} +",
		"find /work/main -not -path '*/worktrees/C-3' -exec rm -rf {} +",
		`find .. -maxdepth 1 \( -name 'C-*' -o -type d \) -exec rm -rf {} +`,
	} {
		refusedUnder(t, command, s, wantState, wantProtected)
	}
	for _, command := range []string{
		"find .. -maxdepth 1 -name 'C-*' ! -name C-9 -exec rm -rf {} +",
		"find .. -maxdepth 1 -name 'C-*' -o -maxdepth 1 -name 'D-*' -exec rm -rf {} +",
		`find .. -maxdepth 1 \( -name 'C-*' -o -name 'D-*' \) -exec rm -rf {} +`,
		"find ../B-2 -name '*.orig' ! -name keep.orig -delete",
		"find ../B-2 -name '*.orig' -o -name '*.bak' -delete",
		"find .. ! -name 'C-*' -print",
		"find .. -name 'C-*' -o -type d -print",
		"find ../B-2 ! -name '*.orig' -exec cat {} +",
	} {
		allowedGlob(t, command, s)
	}
}

// An expression find itself would not accept is still answered: the hook
// answers a panic by letting the call through.
func TestAC5AnExpressionFindWouldNotAcceptIsAnsweredWithoutAPanic(t *testing.T) {
	inParallel(t)
	ends := slices.Concat(deletingActions, printingActions, readingActions, []string{""})
	for _, expression := range []string{
		"!", "-not", "-o", "-or", "-a", "-and", ",", `\(`, `\)`, `\( \)`, "'('", "')'",
		"! !", "! -o", "-o !", "-o -o", "! -a", "-a -o -a", ", ,", `! \(`, `\( !`, `\( -o \)`, `\) \(`,
		"-name", "! -name", "-name '*.orig' -o -name", "-newer", "-fprintf", "-fprintf /tmp/list", "-fprint", "-exec", "-exec rm",
		`-exec \;`, "-exec +", "-ok", "-mindepth", "-maxdepth x", "-type",
		"-name '*.orig' !", "-name '*.orig' -o", "-name '*.orig' -not", "-name '*.orig' -a", "-name '*.orig' ,",
		`-name '*.orig' \(`, `-name '*.orig' \)`, `\( -name '*.orig'`, `-name '*.orig' \) \(`,
		`\( \( -name '*.orig' \)`, `\( -name '*.orig' \) \)`, `! \( \)`, `\( ! \)`,
		"-o -name '*.orig'", "-a -name '*.orig'", ", -name '*.orig'", `\) -name '*.orig'`,
		"-name '*.orig' -o -o -name '*.bak'", "-name '*.orig' ! -o -name '*.bak'",
		`"$OP"`, `"$OP" "$OP"`, `-name '*.orig' "$OP"`, `"$A" -name`, `-newer "$A"`, `-fprintf "$A"`,
		`-name '*.orig' $'\`, `-name '*.orig' "$OP`, `-name '*.orig' '!`, "-name '*.orig' `", `-name '*.orig' \`,
		`-name '*.orig' > /tmp/out !`, `X=1 -name '*.orig' "$OP"`,
		"! ! -name '*.orig'", "! ! ! ! -name '*.orig'",
	} {
		for _, end := range ends {
			for _, root := range []string{"", ".", "internal", "build", `"$ROOT"`, "!", `\(`} {
				command := strings.Join(strings.Fields("find "+root+" "+expression+" "+end), " ")
				inspectNoPanic(t, command, freezing(xTest))
				inspectNoPanic(t, command, ready)
				inspectNoPanic(t, "cd internal && "+command, freezing(xTest))
			}
		}
	}
}

// An expression is as long and as deep as the command makes it, and the reader
// has to take time in proportion to it. One that tries every combination of
// forty alternatives two ways takes 2^40 steps on the grouped form here; this
// test cannot finish against it, and go test's -timeout reports it. There is
// no clock in it. Each of these is refused whether the reader follows it to
// the end or gives up at a bound.
func TestAC5ALongOrDeepExpressionIsAnsweredInStepWithItsLength(t *testing.T) {
	inParallel(t)
	const many = 2000
	var alternatives strings.Builder
	for i := range many {
		alternatives.WriteString("-name 'a" + strconv.Itoa(i) + ".orig' -o ")
	}
	for name, expression := range map[string]string{
		"nested groups":          strings.Repeat(`\( `, many) + "! -name '*.orig' " + strings.Repeat(`\) `, many),
		"alternatives":           alternatives.String() + "-type f",
		"negations":              strings.Repeat("! ", many+1) + "-name '*.orig'",
		"negated groups":         strings.Repeat(`! \( `, many+1) + "-name '*.orig' " + strings.Repeat(`\) `, many+1),
		"groups of alternatives": strings.Repeat(`\( -name 'a.orig' -o -name 'b.orig' \) `, 40) + "-o -type f",
		"alternatives of groups": strings.Repeat(`\( -name 'a.orig' -name 'b.orig' \) -o `, 40) + "! -name '*.orig'",
		"words the shell fills":  "-name '*.orig' " + strings.Repeat(`"$OP" `, many),
	} {
		t.Run(name, func(t *testing.T) {
			for _, a := range []string{"-delete", "-exec rm {} +"} {
				refusedUnder(t, "find . "+expression+" "+a, freezing(xTest), wantState, wantFrozen, wantProtected)
				refusedUnder(t, "find internal "+expression+" "+a, freezing(xTest), wantFrozen)
				refusedUnder(t, "find . "+expression+" "+a, ready, wantState, wantProtected)
				allowedGlob(t, "find build "+expression+" "+a, freezing(xTest))
			}
			inspectNoPanic(t, "find . "+expression+" -fprint /tmp/list", freezing(xTest))
			allowedGlob(t, "find . "+expression+" -print", freezing(xTest))
		})
	}
	// Groups nothing closes are no expression of find's, and are answered all
	// the same.
	for _, a := range []string{"-delete", "-fprint /tmp/list", "-print"} {
		inspectNoPanic(t, "find . "+strings.Repeat(`\( `, many)+"! -name '*.orig' "+a, freezing(xTest))
		inspectNoPanic(t, "find . "+strings.Repeat(`! \( `, many)+a, ready)
	}
}

// A redirect of a numbered descriptor is the shell's, and no word of the
// find: `2>/dev/null` after a find leaves its expression as readable as it
// was. Read as a word of the expression, it made every find that changes
// files one the hook could not follow.
var (
	descriptorRedirects = []string{
		"2>/dev/null", "2> /dev/null", "2>&1", "2>&-", "2> /tmp/find.err", "2>>/tmp/find.err", "1>/tmp/find.out",
		"1>/dev/null", ">/dev/null 2>&1", "2>/dev/null || true", "2>&1 || true", ">/dev/null 2>&1 || true",
		"2>/dev/null | wc -l",
	}
	// What the shell already took for a redirect, tried beside them.
	plainRedirects = []string{">/dev/null", "> /tmp/find.out", ">> /tmp/find.out", "&>/dev/null", "< /dev/null"}
)

// redirected is each action with a redirect after it.
func redirected(actions []string, redirect string) []string {
	out := make([]string, len(actions))
	for i, a := range actions {
		out[i] = a + " " + redirect
	}
	return out
}

func TestAC4AFindWithADescriptorRedirectIsAllowedWhereItPicksNothingProtected(t *testing.T) {
	inParallel(t)
	allowedGlob(t, "find . -name '*.orig' -delete 2>/dev/null", ready)
	allowedGlob(t, "find . -type f -name '*.pyc' -delete 2>/dev/null || true", freezing(xTest))
	changers := slices.Concat(deletingActions, printingActions)
	for _, redirect := range slices.Concat(descriptorRedirects, plainRedirects) {
		for _, expression := range []string{
			"-name '*.orig'",
			"-type f -name '*.pyc'",
			"-name '*.orig' ! -name keep.orig",
			"-name '*.orig' -o -name '*.bak'",
			"-name node_modules -prune -o -name '*.pyc'",
			`\( -name '*.orig' -o -name '*.bak' \)`,
		} {
			t.Run(expression+" "+redirect, func(t *testing.T) {
				allowedFromEveryRoot(t, expression, redirected(changers, redirect))
			})
		}
		// And a find that only reads, or starts below nothing protected, whatever
		// it picks.
		t.Run("! -name '*.orig' "+redirect, func(t *testing.T) {
			allowedFromEveryRoot(t, "! -name '*.orig'", redirected(readingActions, redirect))
			allowedBelowNothingProtected(t, "! -name '*.orig'", redirected(changers, redirect))
		})
	}
	// The redirect may stand anywhere on the line.
	for _, s := range []State{ready, freezing(xTest)} {
		for _, command := range []string{
			"2>/dev/null find . -name '*.orig' -delete",
			"find . 2>/dev/null -name '*.orig' -delete",
			"find . -name '*.orig' 2>/dev/null -delete",
			"find 2>/dev/null . -name '*.orig' ! -name keep.orig -delete",
			"find . -name '*.orig' 2>&1 -o -name '*.bak' -exec rm {} +",
		} {
			allowedGlob(t, command, s)
		}
	}
}

// The redirect hides nothing either: a find that picks a protected file is
// refused with one as it is without, wherever the redirect stands.
func TestAC1AndAC2ADescriptorRedirectDoesNotHideWhatAFindsOperatorsPick(t *testing.T) {
	inParallel(t)
	changers := slices.Concat(deletingActions, printingActions)
	for _, redirect := range slices.Concat(descriptorRedirects, plainRedirects) {
		t.Run("after "+redirect, func(t *testing.T) {
			refusedFromEveryRoot(t, "! -name '*.orig'", redirected(deletingActions, redirect), pickedFrom)
			refusedFromEveryRoot(t, "-name '*.orig' -o -type f", redirected(deletingActions, redirect), pickedFrom)
			refusedFromEveryRoot(t, `-name '*.orig' "$OP" -name '*.bak'`, redirected(changers, redirect), asTheRootHoldsIt)
		})
	}
	for _, redirect := range []string{"2>/dev/null", "2>&1", "2> /tmp/find.err", "1>/dev/null", ">/dev/null"} {
		t.Run("before "+redirect, func(t *testing.T) {
			for _, expression := range []string{
				redirect + " ! -name '*.orig'",
				"-type f " + redirect + " -not -name '*.orig'",
				"-name '*.orig' " + redirect + " -o -type f",
				"-name '*.orig' -o " + redirect + " -type f",
			} {
				refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
			}
			for _, a := range deletingActions {
				for _, r := range rootsHoldingX {
					r.before += redirect + " "
					refusedUnder(t, findDoing(r, "! -name '*.orig'", a), freezing(xTest), pickedFrom(r)...)
					refusedUnder(t, findDoing(r, "-name '*.orig' -o -type f", a), freezing(xTest), pickedFrom(r)...)
					refusedUnder(t, findDoing(r, `-name '*.orig' "$OP" -name '*.bak'`, a), freezing(xTest), r.rules...)
				}
				for _, r := range rootsHoldingState {
					r.before += redirect + " "
					refusedUnder(t, findDoing(r, "! -name '*.orig'", a), ready, pickedFrom(r)...)
					refusedUnder(t, findDoing(r, "-name '*.orig' -o -type f", a), ready, pickedFrom(r)...)
				}
			}
		})
	}
}

// A find handed to a shell as a script, with `-c` or `eval`, is the same find.
// The script is written here with Q for the quote inside it, which is the
// other kind from the quote around it, and W for a word the shell that runs
// the script fills in.
var scriptRunners = []struct {
	before, after string
	inner         string   // what Q is inside this runner's quotes
	filled        []string // what W is: a word filled in by one shell or the other
}{
	{`sh -c "`, `"`, "'", []string{`$OP`, `\$OP`, `\${OP}`}},
	{`bash -c "`, `"`, "'", []string{`$OP`, `\$OP`, `\$(printf x)`}},
	{`eval "`, `"`, "'", []string{`$OP`, `\$OP`}},
	{`sh -c '`, `'`, `"`, []string{`$OP`, `"$OP"`, `${OP}`}},
	{`bash -c '`, `'`, `"`, []string{`$OP`, `"$OP"`, `$(printf x)`}},
	{`eval '`, `'`, `"`, []string{`$OP`, `"$OP"`}},
}

// scriptRoots are where a script's find starts, and the state it is tried in.
var scriptRoots = []struct {
	root rootTried
	s    State
}{
	{rootTried{"", ".", asEither}, freezing(xTest)},
	{rootTried{"", "", asEither}, freezing(xTest)},
	{rootTried{"", "..", asEither}, freezing(xTest)},
	{rootTried{"", "internal", asFrozen}, freezing(xTest)},
	{rootTried{"cd internal && ", ".", asFrozen}, freezing(xTest)},
	{rootTried{"", ".", []string{wantState}}, ready},
	{rootTried{"", "", []string{wantState}}, ready},
	{rootTried{"", "..", []string{wantState}}, ready},
}

// findAsScripts is the find of expression and action from r, as each runner is
// given it, once for each word W stands for.
func findAsScripts(r rootTried, expression, action string, try func(command string)) {
	for _, runner := range scriptRunners {
		words := runner.filled
		if !strings.Contains(expression, "W") {
			words = words[:1]
		}
		for _, w := range words {
			script := "find " + r.root + " " + expression + " " + action
			script = strings.ReplaceAll(strings.ReplaceAll(script, "Q", runner.inner), "W", w)
			try(r.before + runner.before + script + runner.after)
		}
	}
}

func TestAC1AndAC2AFindRunAsAScriptIsJudgedByWhatItsOperatorsPick(t *testing.T) {
	inParallel(t)
	refusedUnder(t, `sh -c "find . ! -name '*.orig' -delete"`, ready, wantState, wantProtected)
	refusedUnder(t, `bash -c 'find internal -name "*.orig" -o -type f -delete'`, freezing(xTest), wantFrozen)
	refusedUnder(t, `eval "find . -not -name '*.orig' -delete"`, ready, wantState, wantProtected)
	for _, expression := range []string{
		"! -name Q*.origQ",
		"-not -path Q*.origQ",
		"-type f ! -name Q*.origQ",
		"-name Q*.origQ -o -type f",
		"-name Q*.origQ -or -newer ref",
		`\( ! -name Q*.origQ \)`,
		`! \( -name Q*.origQ -o -name Q*.bakQ \)`,
	} {
		t.Run(expression, func(t *testing.T) {
			for _, at := range scriptRoots {
				for _, a := range deletingActions {
					findAsScripts(at.root, expression, a, func(command string) {
						refusedUnder(t, command, at.s, pickedFrom(at.root)...)
					})
				}
			}
		})
	}
}

func TestAC3AWordTheShellFillsInInsideAScriptIsRefusedWhenItsFindGoesOnToChangeFiles(t *testing.T) {
	inParallel(t)
	changers := slices.Concat(deletingActions, printingActions)
	for _, expression := range []string{
		"-name Q*.origQ W -name Q*.bakQ",
		"-type f W -name Q*.origQ",
		"-name Q*.origQ W",
	} {
		t.Run(expression, func(t *testing.T) {
			for _, at := range scriptRoots {
				for _, a := range changers {
					findAsScripts(at.root, expression, a, func(command string) {
						refusedUnder(t, command, at.s, at.root.rules...)
					})
				}
			}
			for _, r := range notHolding {
				for _, a := range changers {
					findAsScripts(r, expression, a, func(command string) {
						allowedGlob(t, command, freezing(xTest))
						allowedGlob(t, command, ready)
					})
				}
			}
			for _, at := range scriptRoots {
				findAsScripts(at.root, expression, "-print", func(command string) { allowedGlob(t, command, at.s) })
			}
		})
	}
}

func TestAC4AFindRunAsAScriptIsAllowedWhereItsOperatorsPickNothingProtected(t *testing.T) {
	changers := slices.Concat(deletingActions, printingActions)
	for _, expression := range []string{
		"-name Q*.origQ",
		"-name Q*.origQ ! -name keep.orig",
		"-name Q*.origQ -o -name Q*.bakQ",
		"-name node_modules -prune -o -name Q*.pycQ",
		`\( -name Q*.origQ -o -name Q*.bakQ \)`,
	} {
		t.Run(expression, func(t *testing.T) {
			for _, at := range scriptRoots {
				for _, a := range slices.Concat(changers, readingActions) {
					findAsScripts(at.root, expression, a, func(command string) { allowedGlob(t, command, at.s) })
				}
			}
		})
	}
	// What picks a protected file, where the find only reads or only lists, or
	// starts below nothing protected.
	for _, expression := range []string{"! -name Q*.origQ", "-name Q*.origQ -o -type f"} {
		t.Run(expression, func(t *testing.T) {
			for _, at := range scriptRoots {
				for _, a := range slices.Concat(printingActions, readingActions) {
					findAsScripts(at.root, expression, a, func(command string) { allowedGlob(t, command, at.s) })
				}
			}
			for _, r := range notHolding {
				for _, a := range changers {
					findAsScripts(r, expression, a, func(command string) {
						allowedGlob(t, command, freezing(xTest))
						allowedGlob(t, command, ready)
					})
				}
			}
		})
	}
}

// A `;`, `)`, `|` or `&` inside a quoted word is a character of the word. Taken
// for the end of the command, it ended the find there for every rule, and the
// rest of its expression with it.
func separatorWords() []string {
	var out []string
	for _, w := range []string{"a;b", "a)b", "a|b", "a&b", "a&&b", "a||b", "x;", "*;*"} {
		out = append(out, "'"+w+"'", `"`+w+`"`)
	}
	return out
}

func TestAC1AndAC2ASeparatorInsideAQuotedWordDoesNotEndTheFind(t *testing.T) {
	inParallel(t)
	refusedUnder(t, "find . ! -name 'x;y' -delete", ready, wantState, wantProtected)
	refusedUnder(t, "find internal -name 'a;b' -o -type f -delete", freezing(xTest), wantFrozen)
	for _, expression := range []string{
		"! -name WORD",
		"-not -path WORD",
		"-name WORD -o -type f",
		"-name '*.orig' -o -path WORD -o -type f",
		"-newer WORD ! -name '*.orig'",
		`-name '*.orig' -exec echo WORD \; -o -type f`,
		"-name '*.orig' -exec grep -l WORD {} + -o -type f",
		`-name '*.orig' -exec echo WORD \; ! -name '*.bak' -o ! -name '*.orig'`,
	} {
		for _, w := range separatorWords() {
			with := strings.ReplaceAll(expression, "WORD", w)
			t.Run(with, func(t *testing.T) {
				refusedFromEveryRoot(t, with, deletingActions, pickedFrom)
			})
		}
	}
	// A script the find hands to a shell of its own is one quoted word too.
	for _, expression := range []string{
		`-name '*.orig' -exec sh -c 'echo "$1"; echo y' _ {} \; -o -type f`,
		`-name '*.orig' -exec sh -c 'echo "$1" && echo y' _ {} \; -o -type f`,
		`-name '*.orig' -exec sh -c 'echo "$1" | cat' _ {} \; -o -type f`,
		`-name '*.orig' -exec sh -c 'echo "$1" & wait' _ {} \; -o ! -name '*.bak'`,
		`-name '*.orig' -exec sh -c "echo x; (echo y)" \; -or -type f`,
		`-name '*.orig' -execdir sh -c 'test -f "$1" || exit 1' _ {} \; -o -type f`,
	} {
		t.Run(expression, func(t *testing.T) {
			refusedFromEveryRoot(t, expression, deletingActions, pickedFrom)
		})
	}
}

func TestAC4AFindWithASeparatorInsideAQuotedWordIsAllowedWhereItPicksNothingProtected(t *testing.T) {
	inParallel(t)
	all := slices.Concat(deletingActions, printingActions, readingActions)
	for _, w := range separatorWords() {
		t.Run(w, func(t *testing.T) {
			for _, expression := range []string{
				"-name WORD",
				"-name '*.orig' ! -name WORD",
				"-name WORD -o -name '*.bak'",
				"-path WORD -name '*.orig'",
				"-name '*.orig' -newer WORD",
			} {
				allowedFromEveryRoot(t, strings.ReplaceAll(expression, "WORD", w), all)
			}
			// With the word given to a command that only reads, and from where
			// nothing protected is below, whatever the find picks.
			allowedFromEveryRoot(t, "-name '*.orig' -exec grep -l "+w+" {} +", []string{""})
			allowedFromEveryRoot(t, "! -name "+w, readingActions)
			allowedBelowNothingProtected(t, "! -name "+w, slices.Concat(deletingActions, printingActions))
			allowedBelowNothingProtected(t, "-name "+w+" -o -type f", deletingActions)
			allowedBelowNothingProtected(t, `-name '*.orig' -exec echo `+w+` \; -o -type f`, deletingActions)
		})
	}
}

// An option the reader does not know takes a number of values it cannot know,
// so a find that changes files with one in its expression is one it cannot
// follow.
var unknownOptions = []string{
	"-name '*.orig' -foo",
	"-foo -name '*.orig'",
	"-name '*.orig' -frobnicate",
	"-type f -nosuchtest -name '*.orig'",
	"-name '*.orig' -Delete",
	"-name '*.orig' -o -name '*.bak' -foo",
	"-name '*.orig' -foo bar",
}

func TestAC3AnOptionTheReaderDoesNotKnowMakesAFindThatChangesFilesOneItCannotFollow(t *testing.T) {
	refusedUnder(t, "find . -name '*.orig' -foo -delete", ready, wantState)
	refusedUnder(t, "find internal -name '*.orig' -foo -fprint /tmp/list", freezing(xTest), wantFrozen)
	changers := slices.Concat(deletingActions, printingActions)
	for _, expression := range unknownOptions {
		t.Run(expression, func(t *testing.T) {
			refusedFromEveryRoot(t, expression, changers, asTheRootHoldsIt)
			allowedFromEveryRoot(t, expression, readingActions)
			allowedBelowNothingProtected(t, expression, changers)
		})
	}
}

// A find the reader gives up on is refused where it changes files from a root
// that holds or could hold a protected file, and says why in terms a person can
// act on. Each row here is one way of giving up: what the reason has to name,
// and what the route has to offer for that cause. ROOT is where the find
// starts and ACTION what it goes on to do.
type gaveUp struct {
	command string
	names   []string // what the reason names: the word, or the cause
	route   string   // what the route offers for this cause
	// writes is set where the command changes files whatever ACTION is, and
	// anywhere where its find could start somewhere the command does not show.
	writes, anywhere bool
}

const (
	routeOwnCommand = "command of its own"
	routeWriteOut   = "single quotes"
	routeOneFind    = "one find for each filter"
)

var gaveUpOn = []gaveUp{
	// The find is inside something the reader does not take apart.
	{command: "find ROOT -name '*.orig' -newer <(date) ACTION", names: []string{"process substitution"}, route: routeOwnCommand},
	{command: "find ROOT -name '*.orig' ACTION > >(cat)", names: []string{"process substitution"}, route: routeOwnCommand},
	{command: `sh -c "sh -c 'find ROOT -name x.orig ACTION'"`, names: []string{"script inside a script"}, route: routeOwnCommand},
	{command: `sh -c 'bash -c "find ROOT -name x.orig ACTION"'`, names: []string{"script inside a script"}, route: routeOwnCommand},
	{command: "eval find ROOT -name '*.orig' ACTION", names: []string{"`eval`"}, route: routeOwnCommand},
	{command: `eval eval 'find ROOT -name "*.orig" ACTION'`, names: []string{"`eval`"}, route: routeOwnCommand},
	{command: `eval 'find ROOT -name "*.orig" ACTION' 'x'`, route: routeOwnCommand},
	{command: `sh -c 'find ROOT -name "*.orig" ACTION' sh`, route: routeOwnCommand},
	{command: `sh -c "find ROOT -newer \"ref\" -name '*.orig' ACTION"`, names: []string{"backslash", `\"ref\"`}, route: routeOwnCommand},
	{command: `sh -c "find ROOT -name '*.orig' -newer a\\\\b ACTION"`, names: []string{"backslash"}, route: routeOwnCommand},
	{command: `sh -c "find ROOT -name '*.orig' -exec echo {} \\; ACTION"`, names: []string{"separator"}, route: routeOwnCommand, writes: true},
	// A quote the reader cannot see the end of, or that is open where the
	// find starts.
	{command: "find ROOT -name '*.orig' ACTION # don't", names: []string{"never closed"}, route: routeOwnCommand},
	{command: `sh -c 'find ROOT -name x.orig ACTION`, names: []string{"never closed"}, route: routeOwnCommand},
	{command: `sh -c 'find ROOT -name x.orig ACTION "'`, names: []string{"never closed"}, route: routeOwnCommand},
	{command: `echo "a; find ROOT -name '*.orig' ACTION`, names: []string{"never closed"}, route: routeOwnCommand},
	{command: "echo it's; find ROOT -name '*.orig' ACTION", route: routeOwnCommand},
	{command: "echo $'it; find ROOT -name x.orig ACTION", route: routeOwnCommand},
	{command: "echo `find ROOT -name '*.orig' ACTION", names: []string{"never closed"}, route: routeOwnCommand},
	// A substitution nothing closes, around a find with an option the reader
	// does not know.
	{command: "echo $(find ROOT -name '*.orig' -foo ACTION", names: []string{"`-foo`"}, route: "options the hook knows"},
	{command: "find ROOT -name '*.orig' -foo ACTION $(", names: []string{"`-foo`"}, route: "options the hook knows"},
	// An escaped separator written against another word.
	{command: `find ROOT -name x\; ACTION`, names: []string{"separator", "`;`"}, route: routeOwnCommand},
	{command: `find ROOT -name '*.orig' -exec echo {} \;ACTION`, names: []string{"separator", "`;`"}, route: routeOwnCommand, writes: true},
	{command: `find ROOT -name '*.orig' -exec echo a\)b {} + ACTION`, names: []string{"separator", "`)`"}, route: routeOwnCommand, writes: true},
	// The word that is the program, quoted or escaped.
	{command: `\find ROOT -name '*.orig' ACTION`, names: []string{`\find`}, route: "written out plainly"},
	{command: "'find' ROOT -name '*.orig' ACTION"},
	{command: `"find" ROOT -name '*.orig' ACTION`},
	// A separator the shell hands find as a word, where no test can stand.
	{command: `find ROOT -name '*.orig' \; ACTION`, names: []string{"separator", "`;`"}},
	{command: `find ROOT -name '*.orig' \| ACTION`, names: []string{"separator", "`|`"}},
	{command: `find ROOT -name '*.orig' \& ACTION`, names: []string{"separator", "`&`"}},
	{command: `find ROOT -name '*.orig' '|' ACTION`, names: []string{"separator", "`|`"}},
	{command: `find ROOT -name '*.orig' ";" ACTION`, names: []string{"separator", "`;`"}},
	// A word after the tests that is neither a test nor an operator.
	{command: "find ROOT -name '*.orig' ACTION # a note", names: []string{"`#`"}, route: "comment"},
	{command: "find ROOT -name '*.orig' later ACTION", names: []string{"`later`"}, route: "every directory before the first test"},
	// The expression's own shape.
	{command: `find ROOT -name '*.orig' \) ACTION`, names: []string{"`)`"}, route: routeOneFind},
	{command: `find ROOT \( -name '*.orig' ACTION`, names: []string{"`(`"}, route: routeOneFind},
	{command: "find ROOT -o -name '*.orig' ACTION", names: []string{"`-o`"}, route: routeOneFind},
	{command: "find ROOT -and -name '*.orig' ACTION", names: []string{"`-and`"}, route: routeOneFind},
	{command: "find ROOT -name '*.orig' ACTION -o", route: routeOneFind},
	{command: "find ROOT -name '*.orig' , ACTION", names: []string{"`,`"}, route: routeOneFind},
	// A word the shell fills in: where an operator could be, as a second place
	// to start, and in the command an -exec runs, where it could become the
	// terminator or stand before a word that reads as the rest of the expression.
	{command: `find ROOT -name '*.orig' "$OP" ACTION`, names: []string{`"$OP"`}, route: routeWriteOut},
	{command: `find ROOT "$DIR" -name '*.orig' ACTION`, names: []string{`"$DIR"`}, route: routeWriteOut, anywhere: true},
	{command: `find ROOT $DIR -name '*.orig' ACTION`, names: []string{"`$DIR`"}, route: routeWriteOut, anywhere: true},
	{command: `find ROOT -name '*.orig' -exec rm $X {} \;`, names: []string{"`$X`"}, route: routeWriteOut, writes: true},
	{command: `find ROOT -name '*.orig' -exec mv {} $DEST \;`, names: []string{"`$DEST`"}, route: routeWriteOut, writes: true},
	{command: `find ROOT -name '*.orig' -exec mv "$A" -f {} \;`, names: []string{"`-f`"}, route: routeWriteOut, writes: true},
	// An option the reader does not know, alone and with a filled word after it.
	{command: "find ROOT -name '*.orig' -foo ACTION", names: []string{"`-foo`"}, route: "options the hook knows"},
	{command: `find ROOT -name '*.orig' -foo "$V" ACTION`, names: []string{"`-foo`", `"$V"`}, route: "options the hook knows"},
}

// given is a row's command from root with action.
func (g gaveUp) given(root, action string) string {
	return strings.ReplaceAll(strings.ReplaceAll(g.command, "ROOT", root), "ACTION", action)
}

func TestAC3AFindTheReaderGivesUpOnIsRefusedAndToldWhatToDoForThatCause(t *testing.T) {
	for _, g := range gaveUpOn {
		t.Run(g.command, func(t *testing.T) {
			command := g.given(".", "-delete")
			f, ok := inspectNoPanic(t, command, ready)
			switch {
			case !ok:
				t.Errorf("allowed: %s", command)
			case f.Rule != wantState:
				t.Errorf("%s was refused by %s, want %s (%s)", command, f.Rule, wantState, f.Reason)
			default:
				for _, name := range g.names {
					if !strings.Contains(f.Reason, name) {
						t.Errorf("%s: the reason does not name %s: %s", command, name, f.Reason)
					}
				}
				if !strings.Contains(f.Route, g.route) || !strings.Contains(f.Route, "sdlc") {
					t.Errorf("%s: the route does not offer %q and the rule's own way out: %s", command, g.route, f.Route)
				}
			}
			under := asFrozen
			if g.anywhere {
				under = asEither
			}
			refusedUnder(t, g.given("internal", "-exec rm {} +"), freezing(xTest), under...)
			refusedUnder(t, g.given("..", "-fprint /tmp/list"), ready, wantState)
			if !g.writes {
				allowedGlob(t, g.given(".", "-print"), ready)
				allowedGlob(t, g.given("internal", "-exec cat {} +"), freezing(xTest))
			}
			if !g.anywhere {
				allowedGlob(t, g.given("build", "-delete"), ready)
				allowedGlob(t, g.given("build", "-exec rm {} +"), freezing(xTest))
			}
		})
	}
}

// The words of the command an -exec runs are the command's own: a separator
// the shell hands find among them ends nothing but, for `;`, the command; and a
// command that runs to the end of the words is answered like any other.
func TestAC2TheCommandOfAnExecIsReadToItsTerminatorWhateverSeparatorsItHolds(t *testing.T) {
	for _, s := range []State{ready, freezing(xTest)} {
		refusedUnder(t, `find . -name '*.orig' -exec grep -e \) -e \| -e \& {} \; -o -type f -delete`, s, wantState, wantFrozen, wantProtected)
		refusedUnder(t, `find . -name '*.orig' -exec grep -e '|' -e ")" {} + -o -type f -exec rm {} +`, s, wantState, wantFrozen, wantProtected)
		allowedGlob(t, `find . -name '*.orig' -exec grep -e \) -e \| -e \& {} \; -delete`, s)
		allowedGlob(t, `find . -name '*.orig' -exec grep -e \) -e \| -e \& {} \; -o -name '*.bak' -delete`, s)
		allowedGlob(t, `find . -name '*.orig' -exec grep -e \) -e \| -e \& {} \; -o -type f -print`, s)
		for _, command := range []string{
			"find . -name '*.orig' -exec rm {}",
			"find . ! -name '*.orig' -exec rm {}",
			"find . -name '*.orig' -o -type f -exec rm",
			`find . -name '*.orig' -exec rm \) {}`,
		} {
			inspectNoPanic(t, command, s)
		}
	}
}

// Where the reader gives up on a find it has not found the end of, the find is
// judged with every word from its start to the end of the command: a separator
// inside a quoted word ends nothing, and an action after it is still the
// find's. SEP is the separator, in a word quoted the one way and the other.
var gaveUpBeforeTheEnd = []string{
	// The program word in quotes.
	"'find' ROOT -name 'aSEPb' ACTION",
	`"find" ROOT -name "aSEPb" ACTION`,
	// A comment after the find, with an apostrophe in it, and one before it.
	"find ROOT -name 'aSEPb' ACTION # don't",
	`find ROOT -name "aSEPb" ACTION # don't`,
	"# it's\nfind ROOT -name 'aSEPb' ACTION",
	// A script given to eval that is not one quoted word, and eval twice over.
	`eval find ROOT -name "'aSEPb'" ACTION`,
	`eval find ROOT -name '"aSEPb"' ACTION`,
	`eval "find ROOT -name 'aSEPb'" ACTION`,
	`eval 'find ROOT -name "aSEPb"' ACTION`,
	`eval eval 'find ROOT -name "aSEPb" ACTION'`,
	// A script inside a script.
	`sh -c "sh -c 'find ROOT -name \"aSEPb\" ACTION'"`,
	`bash -c 'sh -c "find ROOT -name \"aSEPb\" ACTION"'`,
	// A script whose quote closes before the end of its command.
	`sh -c 'find ROOT -name "aSEPb" ACTION' sh`,
	`sh -c "find ROOT -name 'aSEPb' ACTION" sh`,
	// A script in double quotes with an escaped quote in it.
	`sh -c "find ROOT -name \"aSEPb\" ACTION"`,
	// The program word escaped.
	`\find ROOT -name 'aSEPb' ACTION`,
}

func TestAC3AFindTheReaderGivesUpOnIsJudgedToTheEndOfTheCommandPastASeparatorInAQuotedWord(t *testing.T) {
	for _, form := range gaveUpBeforeTheEnd {
		t.Run(form, func(t *testing.T) {
			for _, sep := range []string{";", "|", "&", ")"} {
				g := gaveUp{command: strings.ReplaceAll(form, "SEP", sep)}
				refusedUnder(t, g.given(".", "-delete"), ready, wantState)
				refusedUnder(t, g.given("internal", "-exec rm {} +"), freezing(xTest), wantFrozen)
				refusedUnder(t, g.given("..", "-fprint /tmp/list"), ready, wantState)
				allowedGlob(t, g.given(".", "-print"), ready)
				allowedGlob(t, g.given("build", "-delete"), freezing(xTest))
			}
		})
	}
}

// The reader marks, in its own copy of the command, where the shell hid a
// separator from find, with control characters no command needs. A command
// that holds one of those characters itself is judged like any other, and a
// PowerShell command, where nothing is hidden that way, is answered.
func TestAC5ACommandHoldingOneOfTheReadersOwnMarksIsJudgedLikeAnyOther(t *testing.T) {
	for _, mark := range []string{"\x1c", "\x1d", "\x1e", "\x1f"} {
		for _, s := range []State{ready, freezing(xTest)} {
			refusedUnder(t, "find . ! -name 'a"+mark+"b' -delete", s, wantState, wantFrozen, wantProtected)
			refusedUnder(t, "find . -name 'a"+mark+"b' -o -type f -exec rm {} +", s, wantState, wantFrozen, wantProtected)
			refusedUnder(t, "find . -name '*.orig' "+mark+" -o -type f -delete", s, wantState, wantFrozen, wantProtected)
			allowedGlob(t, "find . ! -name 'a"+mark+"b' -print", s)
			allowedGlob(t, "find build ! -name 'a"+mark+"b' -delete", s)
		}
	}
	shell := ready
	shell.PowerShell = true
	for _, command := range []string{
		"find . ! -name '*.orig' -delete",
		"find . -name '*.orig' -o -type f -delete",
		`find . \( ! -name '*.orig' \) -delete`,
		"find . -name 'a;b' -delete",
		"find . -name '*.orig' -print",
	} {
		inspectNoPanic(t, command, shell)
	}
}

// A find that starts at another checkout takes the checkout away only where
// the checkout itself passes its tests: a test for files that are not
// directories, or for empty ones, beside a negation leaves the checkout out,
// and a glob under the negation that cannot be read leaves it in.
func TestAC1AFindAtAnotherCheckoutIsJudgedByWhetherTheCheckoutItselfPassesItsTests(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"find ../B-2 ! -name '*.orig' -type f -delete",
		"find ../B-2 -not -name '*.orig' -empty -delete",
		`find ../B-2 \( ! -name '*.orig' -type f \) -o -name '*.bak' -delete`,
	} {
		allowedGlob(t, command, s)
	}
	for _, command := range []string{
		"find ../B-2 ! -name '*.orig' -type d -exec rm -rf {} +",
		"find ../B-2 -type f -o ! -name '*.orig' -exec rm -rf {} +",
		"find ../B-2 -name '[[:foo:]]' -o -name '*.orig' -exec rm -rf {} +",
		"find ../B-2 -path '[[:foo:]]' -o -name '*.orig' -exec rm -rf {} +",
		"find ../B-2 -path '*/B-2' -o -name '*.orig' -exec rm -rf {} +",
	} {
		refusedUnder(t, command, s, wantState)
	}
}
