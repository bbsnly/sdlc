package shellpolicy

import (
	"strings"
	"testing"
)

// frozenProgramCase is one command that hands awk, gawk, sed or perl a program,
// asked while a test is frozen. The freeze is `aFrozenTest` unless the row names
// its own, so that a row can show the rule is about the freeze and not about a
// name ending in _test.go.
type frozenProgramCase struct {
	name    string
	ps      bool     // the command arrived from the PowerShell tool
	dir     string   // where the session is, State.Dir
	frozen  []string // the freeze, where it is not aFrozenTest alone
	command string
	rule    string // "" when the command is allowed
}

const (
	aFrozenTest    = "internal/x_test.go"
	throughTheTool = "frozen-test-through-the-tool"
)

func (c frozenProgramCase) check(t *testing.T) {
	frozen := c.frozen
	if frozen == nil {
		frozen = []string{aFrozenTest}
	}
	s := State{CommitReady: true, Frozen: frozen, Dir: c.dir, PowerShell: c.ps}
	if c.rule == "" {
		allowed(t, c.command, s)
		return
	}
	refused(t, c.command, s, c.rule)
}

// frozenProgramEvasions wrote a frozen test while the hook allowed it. awk's
// `print > FILE` and sed's `w` are the tool's own output and not a shell
// redirect, so the writer predicate the freeze asks -- which wants `-i` from sed
// and the word `inplace` from awk -- says no; and the redirect the segment is
// split on takes the closing brace with the target, so `internal/x_test.go}`
// never matched the freeze either. Both were run against a scratch file with the
// real tools and both overwrote it.
//
// These rows are what witnesses the control: each is allowed by the released
// tool, and each names a spelling AC-6 asks the change to be tried against.
var frozenProgramEvasions = []frozenProgramCase{
	{name: "AC-1 AC-6 awk writes it with print >, the target quoted", command: `awk 'BEGIN{print "x" > "internal/x_test.go"}'`, rule: throughTheTool},
	{name: "AC-6 awk writes it with the target bare", command: `awk 'BEGIN{print "x" > internal/x_test.go}'`, rule: throughTheTool},
	{name: "AC-6 awk appends to it with print >>", command: `awk 'BEGIN{print "x" >> "internal/x_test.go"}'`, rule: throughTheTool},
	{name: "AC-6 awk writes it with a ./ in front", command: `awk 'BEGIN{print "x" > "./internal/x_test.go"}'`, rule: throughTheTool},
	{name: "AC-6 awk writes it from the directory the session is in", dir: "internal", command: `awk 'BEGIN{print "x" > "x_test.go"}'`, rule: throughTheTool},
	{name: "AC-2 sed writes it with the w command", command: `sed -n 'w internal/x_test.go' /dev/null`, rule: throughTheTool},
	{name: "AC-2 sed writes it with the w flag of s///", command: `sed 's/a/b/w internal/x_test.go' /tmp/scratch`, rule: throughTheTool},
	{name: "AC-6 sed writes it from the directory the session is in", dir: "internal", command: `sed -n 'w x_test.go' /dev/null`, rule: throughTheTool},
	{name: "AC-3 gawk writes it with print >", command: `gawk 'BEGIN{print "x" > "internal/x_test.go"}'`, rule: throughTheTool},
	{name: "AC-3 gawk appends to it with print >>", command: `gawk 'BEGIN{print "x" >> "internal/x_test.go"}'`, rule: throughTheTool},

	// The target is never written whole on the line: awk assembles it from a
	// variable it was handed, or the shell holds it in one of its own from an
	// earlier segment. Candidates come from the whole command line for this
	// reason.
	{name: "AC-1 awk writes it through a variable given with -v", command: `awk -v f=internal/x_test.go 'BEGIN{print "x" > f}'`, rule: throughTheTool},
	{name: "AC-1 awk writes it through a shell variable set in an earlier segment", command: `F=internal/x_test.go; awk 'BEGIN{print "x" > F}'`, rule: throughTheTool},

	// The program is not on the command line at all. Under -f, --file and
	// gawk's --source the first operand is no longer the program, so every
	// operand is a data file the program that cannot be read may write.
	{name: "AC-1 gawk runs a program from a file over it", command: `gawk -f /tmp/w.awk internal/x_test.go`, rule: throughTheTool},
	{name: "AC-1 awk runs a program from a file over it", command: `awk -f /tmp/w.awk internal/x_test.go`, rule: throughTheTool},
	{name: "AC-1 gawk runs a program from a file named with --file=", command: `gawk --file=/tmp/w.awk internal/x_test.go`, rule: throughTheTool},
	{name: "AC-2 sed runs a program from a file over it", command: `sed -f /tmp/s.sed internal/x_test.go`, rule: throughTheTool},
	{name: "AC-2 sed runs a program from a file named with --file=", command: `sed --file=/tmp/s.sed internal/x_test.go`, rule: throughTheTool},
	{name: "AC-3 gawk takes its program from --source and the operand is data", command: `gawk --source 'BEGIN{print "x" > ARGV[1]}' internal/x_test.go`, rule: throughTheTool},
	{name: "AC-3 gawk takes its program from -e, the short spelling of --source", command: `gawk -e 'BEGIN{print "x" > ARGV[1]}' internal/x_test.go`, rule: throughTheTool},

	// sed's -e needs no inversion: the program is on the line, so the frozen
	// name in it is read like any other program text.
	{name: "AC-2 sed writes it from a program given with -e", command: `sed -e 'w internal/x_test.go' /dev/null`, rule: throughTheTool},

	// The program asks the shell instead of writing the file itself. A design
	// that only looked for `>` or a `w` command would let these two through,
	// which is why the whole of a program is searched rather than parsed.
	{name: "AC-1 awk reaches it through system()", command: `awk 'BEGIN{system("rm internal/x_test.go")}'`, rule: throughTheTool},
	{name: "AC-2 sed reaches it through the e command", command: `sed -e '1e rm internal/x_test.go' /tmp/scratch`, rule: throughTheTool},

	// The rule is about the freeze, not about a name that ends in _test.go: a
	// freeze holds whatever the test gate locked, and a golden fixture is as
	// rewritable as a Go test. A change keyed to the one path these rows would
	// otherwise share passes nothing here.
	{name: "AC-1 awk writes a frozen test under another path", frozen: []string{"internal/calc/add_test.go"}, command: `awk 'BEGIN{print "x" > "internal/calc/add_test.go"}'`, rule: throughTheTool},
	{name: "AC-2 sed writes a frozen fixture that is not a Go test", frozen: []string{"testdata/golden/report.json"}, command: `sed -n 'w testdata/golden/report.json' /dev/null`, rule: throughTheTool},

	// Windows is first class, and PowerShell is its own entry point with its own
	// reading of the same bytes.
	{name: "AC-6 PowerShell, awk writes it with print >", ps: true, command: `awk 'BEGIN{print "x" > "internal/x_test.go"}'`, rule: throughTheTool},
	{name: "AC-6 PowerShell, sed writes it with the w command", ps: true, command: `sed -n 'w internal/x_test.go' /dev/null`, rule: throughTheTool},
	{name: "AC-6 PowerShell, awk writes it from the directory the session is in", ps: true, dir: "internal", command: `awk 'BEGIN{print "x" > "x_test.go"}'`, rule: throughTheTool},
	{name: "AC-6 PowerShell, awk writes it with the target spelled with backticks", ps: true, command: "awk 'BEGIN{print \"x\" > \"internal/x`_test.go\"}'", rule: throughTheTool},
}

// frozenProgramGuards answer the same before the change and after it. A freeze
// rule that refuses a read is the one a person turns off, so the reads of AC-4
// are here beside the in-place writes of AC-5, which were refused all along
// through a different predicate and must not start arriving under another rule.
//
// The perl rows are guards too, and cannot be anything else: perl has been read
// as running an inline program since long before this story, so a frozen path
// anywhere in a -e or -ne program is already a candidate. They are kept because
// AC-3 names them, and none of them witnesses the control.
var frozenProgramGuards = []frozenProgramCase{
	{name: "AC-4 awk only prints what it reads", command: `awk '{print}' internal/x_test.go`},
	{name: "AC-4 sed prints a range of it", command: `sed -n '1,5p' internal/x_test.go`},
	{name: "AC-4 grep is not one of these tools", command: `grep -n func internal/x_test.go`},
	{name: "AC-4 sed prints a line of it after a --", command: `sed -n 1p -- internal/x_test.go`},
	{name: "AC-4 awk reads it from the directory the session is in", dir: "internal", command: `awk '{print}' x_test.go`},
	// sed's -e needs no inversion and does not get one: the program is on the
	// line, so a frozen name in it is read where it stands and an operand beside
	// it is still the file sed reads.
	{name: "AC-4 sed prints a range of it from a program given with -e", command: `sed -e '1,5p' -n internal/x_test.go`},
	{name: "AC-4 PowerShell, awk only prints what it reads", ps: true, command: `awk '{print}' internal/x_test.go`},
	{name: "AC-4 PowerShell, grep is not one of these tools", ps: true, command: `grep -n func internal/x_test.go`},

	{name: "AC-5 awk edits it in place", command: `awk -i inplace '{print}' internal/x_test.go`, rule: throughTheTool},
	{name: "AC-5 gawk edits it in place", command: `gawk -i inplace '{print}' internal/x_test.go`, rule: throughTheTool},
	{name: "AC-5 sed edits it in place", command: `sed -i s/a/b/ internal/x_test.go`, rule: throughTheTool},
	{name: "AC-5 sed edits it in place from the directory the session is in", dir: "internal", command: `sed -i s/a/b/ x_test.go`, rule: throughTheTool},
	{name: "AC-5 PowerShell, sed edits it in place", ps: true, command: `sed -i s/a/b/ internal/x_test.go`, rule: throughTheTool},

	{name: "AC-3 perl opens it for writing", command: `perl -e 'open(F, ">internal/x_test.go")'`, rule: throughTheTool},
	{name: "AC-3 perl opens it for reading and writing", command: `perl -e 'open(F, "+<internal/x_test.go")'`, rule: throughTheTool},
	{name: "AC-3 perl opens it for appending", command: `perl -e 'open(F, ">>internal/x_test.go")'`, rule: throughTheTool},
	{name: "AC-3 perl unlinks it", command: `perl -ne 'unlink "internal/x_test.go"'`, rule: throughTheTool},
	{name: "AC-3 PowerShell, perl opens it for writing", ps: true, command: `perl -e 'open(F, ">internal/x_test.go")'`, rule: throughTheTool},

	// One space is the whole difference between these two rows and the first two
	// of the table above, and it is why the spellings AC-6 asks for do not share
	// one answer from the released tool: the segment is split on `>` without
	// quoting read, so the closing brace rides on the target only where it is
	// glued to it. Written with a space, the target arrives clean and the freeze
	// has always refused it.
	{name: "AC-6 awk writes it with a space before the closing brace, the target quoted", command: `awk 'BEGIN{print "x" > "internal/x_test.go" }'`, rule: throughTheTool},
	{name: "AC-6 awk writes it with a space before the closing brace, the target bare", command: `awk 'BEGIN{print "x" > internal/x_test.go }'`, rule: throughTheTool},

	// A backtick in a single-quoted awk program is a character to bash, so the
	// file this writes is not the frozen test and refusing it would be a false
	// refusal. The PowerShell row of the same spelling is an evasion above,
	// because PowerShell takes the backtick out and the path is the frozen one.
	{name: "AC-6 awk writes a target spelled with backticks, which bash does not read as the frozen one", command: "awk 'BEGIN{print \"x\" > \"internal/x`_test.go\"}'"},

	// Reading a program as inline code is one thing and matching a bare name
	// against the end of every frozen path is another, and these tools get the
	// first without the second. Taken together, a fixture's main.go in another
	// package made an ordinary edit a write to a frozen test.
	{name: "AC-5 sed edits another package's file of the same name as a frozen fixture", frozen: []string{"internal/gen/testdata/main.go"}, dir: "cmd/app", command: `sed -i '' 's/a/b/' main.go`},
}

// frozenProgramCosts are refusals nobody asked for, weighed against the hole and
// accepted with it. Candidates come from the whole command line rather than the
// segment, so a frozen test read in one part of a pipeline is refused wherever
// one of these tools runs in another; and where the program is off the line
// there is nothing to tell a read from a write. This is what the hook already
// did for perl and python, extended to the two commonest tools in a pipeline.
//
// They are rows and not accidents: a refusal that surprises the person who meets
// it is how a control gets loosened, and the loosening should have to change a
// test that says the cost was known.
var frozenProgramCosts = []frozenProgramCase{
	{name: "a frozen test read in one segment with awk in another", command: `grep -n func internal/x_test.go | awk '{print $2}'`, rule: throughTheTool},
	{name: "a frozen test read in one segment with sed in another", command: `head -5 internal/x_test.go | sed -n 1,5p`, rule: throughTheTool},
	{name: "a read whose operand carries quotes", command: `awk '{print}' "internal/x_test.go"`, rule: throughTheTool},
	{name: "a pure read whose program comes from --source", command: `gawk --source '{print}' internal/x_test.go`, rule: throughTheTool},
	{name: "a pure read whose program comes from a file", command: `gawk -f /tmp/fmt.awk internal/x_test.go`, rule: throughTheTool},
	{name: "a program that only matches the name of a frozen test", command: `awk '$1 == "internal/x_test.go" {print}' list.txt`, rule: throughTheTool},
	{name: "a scratch copy of the tree under an absolute path", command: `awk 'BEGIN{print "x" > "/tmp/snap/internal/x_test.go"}'`, rule: throughTheTool},
}

// The freeze is the hinge the loop turns on, and these tools write files by
// their own means: `print > FILE` is awk's own output and `w` is sed's, neither
// of them a shell redirect the classifier reads. Each row here was allowed by
// the released tool.
func TestAProgramOfItsOwnCannotWriteAFrozenTest(t *testing.T) {
	for _, c := range frozenProgramEvasions {
		t.Run(c.name, c.check)
	}
}

// What these tools already answered on a frozen test, and must still: a read is
// allowed and an in-place edit is refused, both as they were.
func TestWhatTheseToolsAlreadyAnswerOnAFrozenTestIsUnchanged(t *testing.T) {
	for _, c := range frozenProgramGuards {
		t.Run(c.name, c.check)
	}
}

// The refusals that come with the fix, each one weighed and taken rather than
// discovered by whoever meets it.
func TestAFrozenTestNamedBesideOneOfTheseToolsIsRefusedWithIt(t *testing.T) {
	for _, c := range frozenProgramCosts {
		t.Run(c.name, c.check)
	}
}

// Two shells read one command two ways, and a rule name is what a person reads.
// PowerShell takes a backtick out of a path, so the target is the frozen test;
// bash reads the same backtick as the start of a substitution and splits the
// word, so what is left is a test file the freeze does not hold. Both are
// refused, under the rule each reading earns -- and a change that answered from
// one reading only would move one of these.
func TestABacktickInTheTargetIsReadAsEachShellReadsIt(t *testing.T) {
	// The matchers the hook builds from the project's test globs, which is what
	// makes the bash reading land on the neighbouring rule rather than nothing.
	state := func(ps bool) State {
		return State{
			CommitReady: true,
			Frozen:      []string{aFrozenTest},
			NewTest:     func(p string) bool { return strings.HasSuffix(p, "_test.go") && p != aFrozenTest },
			PowerShell:  ps,
		}
	}
	command := "perl -e 'open(F, \">internal/x`_test.go\")'"
	t.Run("AC-6 bash reads the backtick as a substitution and the tail as a new test", func(t *testing.T) {
		refused(t, command, state(false), "no-new-test-after-the-freeze")
	})
	t.Run("AC-6 PowerShell takes the backtick out and the path is the frozen test", func(t *testing.T) {
		refused(t, command, state(true), throughTheTool)
	})
}

// The freeze's two neighbours read the same words as the freeze does, so they
// widen with it: a test file added after the freeze, and a test file written by
// the implementer, are refused in an awk or sed program exactly as they already
// are in a perl one. No criterion asks for this; it is here so that it is a
// fact on the record rather than a surprise at the next gate.
func TestTheNeighbouringFreezeRulesReadTheSameProgramText(t *testing.T) {
	added := State{
		CommitReady: true,
		Frozen:      []string{aFrozenTest},
		NewTest:     func(p string) bool { return strings.HasSuffix(p, "_test.go") && p != aFrozenTest },
	}
	implementer := State{
		CommitReady:     true,
		ImplementerTest: func(p string) bool { return strings.HasSuffix(p, "_test.go") },
	}
	for _, c := range []struct {
		name, command string
		state         State
		rule          string
	}{
		{name: "awk adds a test file after the freeze", command: `awk 'BEGIN{print "x" > "internal/new_test.go"}'`, state: added, rule: "no-new-test-after-the-freeze"},
		{name: "sed adds a test file after the freeze", command: `sed -n 'w internal/new_test.go' /dev/null`, state: added, rule: "no-new-test-after-the-freeze"},
		{name: "awk writes a test file for the implementer", command: `awk 'BEGIN{print "x" > "internal/y_test.go"}'`, state: implementer, rule: "implementer-does-not-write-tests"},
		{name: "sed writes a test file for the implementer", command: `sed -n 'w internal/y_test.go' /dev/null`, state: implementer, rule: "implementer-does-not-write-tests"},
	} {
		t.Run(c.name, func(t *testing.T) { refused(t, c.command, c.state, c.rule) })
	}
}

// Before the freeze there is nothing to hold, and the sdet writes these files
// with whatever tool it likes. The freeze rules answer from the freeze, and not
// from the name of the tool.
func TestBeforeTheFreezeTheseToolsAreAsFreeAsTheyWere(t *testing.T) {
	for _, command := range []string{
		`awk 'BEGIN{print "x" > "internal/x_test.go"}'`,
		`sed -n 'w internal/x_test.go' /dev/null`,
		`gawk -f /tmp/w.awk internal/x_test.go`,
	} {
		allowed(t, command, State{CommitReady: true})
	}
}
