package shellpolicy

import (
	"strings"
	"testing"
)

// reading is what the shell that runs a command does with the line under test.
type reading int

const (
	runsIt  reading = iota // the line is a command, and the shell runs it
	asText                 // the line is inside a string or a document
	nothing                // the script does not parse, so none of it runs
)

// hereStringCase is one command with `@'` or `@"` at the end of a line. RUN
// stands where the command under test goes: the classifier is asked about the
// real one, and herestring_shell_test.go puts a stand-in that only prints there
// and asks the shell itself, so `shell` is a checked fact and not a reading of
// a grammar.
type hereStringCase struct {
	name    string
	ps      bool
	command string
	run     string // "" is the commit
	shell   reading
	rule    string // "" when the command is allowed
}

const (
	aCommit      = "git commit -m x"
	anApproval   = "sdlc approve A-1"
	freezeRemove = "rm .sdlc/state/tests.lock"

	approvalRule = "approval-is-a-human-decision"
)

// hereStringEvasions went through when this table was written. A here-string is
// PowerShell's: bash reads the same bytes as two quoted strings with a live
// command between them, and the body was dropped as a document in both shells.
// In PowerShell a line that only ends like a header was taken for one, and a
// header never closed left a quote open over every line after it.
var hereStringEvasions = []hereStringCase{
	{name: "AC-1 bash, a commit between two quoted strings", command: "echo @'\n'; RUN; echo '\n'@", shell: runsIt, rule: "commit-gate"},
	{name: "AC-2 bash, an approval between two quoted strings", command: "echo @'\n'; RUN; echo '\n'@", run: anApproval, shell: runsIt, rule: approvalRule},
	{name: "AC-3 bash, the freeze removed between two quoted strings", command: "echo @'\n'; RUN; echo '\n'@", run: freezeRemove, shell: runsIt, rule: "loop-state-through-the-tool"},
	{name: "AC-6 bash, the double-quoted form", command: "echo @\"\n\"; RUN; echo \"\n\"@", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, the double-quoted form with an approval", command: "echo @\"\n\"; RUN; echo \"\n\"@", run: anApproval, shell: runsIt, rule: approvalRule},
	{name: "AC-6 bash, the marker after other words and the closer indented", command: "echo hi @'\n'; RUN; echo '\n  '@", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, white space after the marker", command: "echo @' \t\n'; RUN; echo '\n'@", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, the marker in a comment", command: "# @'\nRUN\n'@'", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, the marker closing an ordinary string", command: "echo 'user@'\nRUN\n'@'", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, an assignment in front of the marker", command: "s=@'\n'; RUN; echo '\n'@", shell: runsIt, rule: "commit-gate"},

	{name: "AC-5 PowerShell, a single-quoted here-string never closed", ps: true, command: "@'\nRUN\n", shell: nothing, rule: "commit-gate"},
	{name: "AC-5 PowerShell, a double-quoted here-string never closed", ps: true, command: "@\"\nRUN\n", shell: nothing, rule: "commit-gate"},
	{name: "AC-5 PowerShell, an approval after a here-string never closed", ps: true, command: "@'\nRUN\n", run: anApproval, shell: nothing, rule: approvalRule},
	{name: "AC-5 PowerShell, never closed after an assignment and an earlier line", ps: true, command: "Get-Date\n$plan = @\"\nsome text\nRUN", shell: nothing, rule: "commit-gate"},

	{name: "AC-7 PowerShell, the marker closing an ordinary string", ps: true, command: "echo 'user@'\nRUN\n'@'", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, the double-quoted marker closing an ordinary string", ps: true, command: "echo \"user@\"\nRUN\n\"@\"", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, the marker inside a double-quoted string", ps: true, command: "echo \"a @'\n\"; RUN; echo \"\n'@\"", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, the double-quoted marker inside a single-quoted string", ps: true, command: "echo 'a @\"\n'; RUN; echo '\n\"@'", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, the marker inside a comment", ps: true, command: "# @'\nRUN\n'@'", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, the marker inside a comment after a command", ps: true, command: "Get-Date # see user@\"\nRUN\n\"@\"", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, a backtick-escaped quote", ps: true, command: "echo user@`'\nRUN", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, a backtick-escaped double quote", ps: true, command: "echo user@`\"\nRUN", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, a backtick-escaped quote with a closing line", ps: true, command: "echo user@`'\nRUN\n'@'", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, the marker glued to a bare word", ps: true, command: "echo x@'\n'; RUN\n'@'", shell: runsIt, rule: "commit-gate"},
	{name: "AC-7 PowerShell, an approval after the marker glued to a bare word", ps: true, command: "echo x@'\n'; RUN\n'@'", run: anApproval, shell: runsIt, rule: approvalRule},
}

// hereStringGuards answered this way before the change and still must: they are
// what a fix could break. A header test that is too narrow leaves the raw
// marker in, and its quote swallows the closing line and `| iex` with it; an
// unclosed marker made harmless by taking its quote off unbalances the
// balanced `'user@'` and hides the commit after it.
var hereStringGuards = []hereStringCase{
	{name: "AC-6 PowerShell, the commit of AC-1 is the body of a document", ps: true, command: "echo @'\n'; RUN; echo '\n'@", shell: asText},
	{name: "AC-6 PowerShell, the approval of AC-2 is the body of a document", ps: true, command: "echo @'\n'; RUN; echo '\n'@", run: anApproval, shell: asText},
	{name: "AC-6 PowerShell, the removal of AC-3 is the body of a document", ps: true, command: "echo @'\n'; RUN; echo '\n'@", run: freezeRemove, shell: asText},
	{name: "AC-6 PowerShell, the double-quoted form", ps: true, command: "echo @\"\n\"; RUN; echo \"\n\"@", shell: asText},
	{name: "AC-6 PowerShell, the marker after other words", ps: true, command: "echo hi @'\nRUN\n'@", shell: asText},
	{name: "AC-6 PowerShell, a space after the marker", ps: true, command: "echo @' \nRUN\n'@", shell: asText},
	{name: "AC-6 PowerShell, lines that end in a carriage return", ps: true, command: "echo @'\r\nRUN\r\n'@\r\n", shell: asText},
	{name: "AC-6 PowerShell, a closing marker not at the start of its line closes nothing", ps: true, command: "echo @'\ntext '@; RUN\n'@", shell: asText},
	{name: "AC-5 PowerShell, a closing marker not at the start of its line and none after", ps: true, command: "echo @'\ntext '@; RUN", shell: nothing, rule: "commit-gate"},
	{name: "AC-4 PowerShell, a here-string piped to iex", ps: true, command: "@'\nRUN\n'@ | iex", shell: runsIt, rule: "commit-gate"},
	{name: "AC-4 PowerShell, a here-string with a space after the marker piped to iex", ps: true, command: "@' \nRUN\n'@ | iex", shell: runsIt, rule: "commit-gate"},
	{name: "AC-4 PowerShell, a here-string run from its variable on a later line", ps: true, command: "$s = @'\nRUN\n'@\nInvoke-Expression $s", shell: runsIt, rule: "commit-gate"},
	{name: "AC-5 PowerShell, a balanced quote that ends in the marker and no closer", ps: true, command: "echo 'user@'\nRUN", shell: runsIt, rule: "commit-gate"},

	{name: "AC-6 bash, a balanced quote that ends in the marker and no closer", command: "echo 'user@'\nRUN", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, a closing marker not at the start of its line", command: "echo @'\ntext '@; RUN", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, a quoted string over three lines is text", command: "echo @'\nRUN\n'", shell: asText},
	{name: "AC-6 bash, a here-document that holds the marker is read as before", command: "cat <<'EOF'\necho @'\nRUN\nEOF", shell: asText},
}

// hereStringNet holds the shapes refused today only by the reading the released
// tool makes of a command, which the change keeps and asks beside its new one.
// Every row of the two tables above executes that reading, and not one of them
// fails if it is never written: for a control that is the difference between a
// test and a description. Each row here the new reading alone allows.
var hereStringNet = []hereStringCase{
	{name: "AC-5 PowerShell, a quote from line 1 closed by a line that is only the marker", ps: true, command: "echo '\n@'\nRUN", shell: runsIt, rule: "commit-gate"},
	{name: "AC-4 PowerShell, a here-string added to a string and run from its variable", ps: true, command: "$s = ''+@'\nRUN\n'@\niex $s", shell: runsIt, rule: "commit-gate"},
	{name: "AC-6 bash, a here-string piped to a shell", command: "echo @'\nRUN\n'@ | sh", shell: runsIt, rule: "commit-gate"},
	// The command under test is `rm`, which keeps this row out of the shell half
	// as it keeps every row that names one: the row holds no marker, so there is
	// no reading of a grammar in it for a shell to settle, and nothing here runs
	// `rm`. What it holds is that the loop-state rule keeps the spelling of the
	// path the released tool hands it, backticks deleted, and not the raw one.
	{name: "AC-3 PowerShell, a loop-state path spelled with backticks piped to xargs", ps: true, command: "echo .`s`d`l`c/state/tests.lock | xargs RUN", run: "rm", shell: runsIt, rule: "loop-state-through-the-tool"},
}

// text is the command the classifier is asked about, or the one the shell is
// handed when standIn is what runs.
func (c hereStringCase) text(standIn string) string {
	if standIn == "" {
		standIn = c.run
	}
	if standIn == "" {
		standIn = aCommit
	}
	return strings.ReplaceAll(c.command, "RUN", standIn)
}

// check asks Inspect and HumanDecisions about the row. What the shell runs is
// refused: a row that says the shell runs the line and allows it is a hole
// written down as an expectation, so the row itself is checked first.
func (c hereStringCase) check(t *testing.T) {
	if c.shell == runsIt && c.rule == "" || c.shell == asText && c.rule != "" {
		t.Fatalf("the row says the shell reads the line as %d and asks for the rule %q", c.shell, c.rule)
	}
	command := c.text("")
	f, reported := HumanDecisions(command, c.ps, aStoryWaits)
	if want := c.rule == approvalRule; reported != want || reported && f.Rule != c.rule {
		t.Errorf("HumanDecisions(%q): reported = %v under %q, want %v", command, reported, f.Rule, want)
	}
	s := notReady
	s.PowerShell = c.ps
	if c.rule == "" {
		allowed(t, command, s)
		return
	}
	refused(t, command, s, c.rule)
}

func TestAHereStringIsADocumentOnlyToTheShellThatReadsItAsOne(t *testing.T) {
	for _, c := range hereStringEvasions {
		t.Run(c.name, c.check)
	}
}

func TestWhatAHereStringAlreadyAnswersIsUnchanged(t *testing.T) {
	for _, c := range hereStringGuards {
		t.Run(c.name, c.check)
	}
}

func TestTheShapesOnlyTheReleasedReadingRefusesStayRefused(t *testing.T) {
	for _, c := range hereStringNet {
		t.Run(c.name, c.check)
	}
}

// AC-4: the PowerShell cases of TestADocumentIsTextHoweverItIsHandedOver, held
// under the criterion's name so that a change to how a here-string is found
// cannot move one of them without the run saying which.
func TestATerminatedHereStringAnswersAsItDid(t *testing.T) {
	plan := "| step | when |\n| git commit | after gate 7 |\nthe retro runs sdlc stop\n"
	for _, c := range []struct {
		name, command string
		passed        bool
		rule          string
	}{
		{name: "AC-4 the plan piped to sdlc artifact write plan is allowed", command: "@'\n" + plan + "'@ | sdlc artifact write plan"},
		{name: "AC-4 the plan in a variable piped to sdlc artifact write plan is allowed", command: "$plan = @\"\n" + plan + "\"@\n$plan | sdlc artifact write plan"},
		{name: "AC-4 a later line that does not use the variable does not run it", command: "$plan = @'\n" + plan + "'@\n$plan | sdlc artifact write plan\nnode scripts/check.js"},
		{name: "AC-4 a commit message in a variable is not commands", command: "$msg = @'\nsdlc stop stays with a person\n'@\ngit commit -m $msg\npython -m pytest", passed: true},
		{name: "AC-4 piped to pwsh -Command - is refused", command: "@'\ngit commit -m x\n'@ | pwsh -Command -", rule: "commit-gate"},
		{name: "AC-4 piped to sdlc approve is refused", command: "@'\nlooks right\n'@ | sdlc approve A-1", rule: approvalRule},
		{name: "AC-4 piped to iex is refused", command: "@'\ngit commit -m x\n'@ | iex", rule: "commit-gate"},
		{name: "AC-4 handed to Invoke-Expression on a later line is refused", command: "$s = @'\ngit commit -m x\n'@\nInvoke-Expression $s", rule: "commit-gate"},
		{name: "AC-4 the double-quoted form piped to Invoke-Expression is refused", command: "@\"\nsdlc approve A-1\n\"@ | Invoke-Expression", rule: approvalRule},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := notReady
			if c.passed {
				s = ready
			}
			s.PowerShell = true
			if c.rule == "" {
				allowed(t, c.command, s)
				return
			}
			refused(t, c.command, s, c.rule)
		})
	}
}
