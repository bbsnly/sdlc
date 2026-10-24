package shellpolicy

import (
	"strings"
	"testing"
)

// A path that is only named. The released tool read every `>`, `;`, `|` and `&`
// on a line as the shell's, quoted or not, and every word of a line that also
// held an interpreter as that interpreter's to write; so an escalation whose
// message said what had been refused was refused for it, and so was a diff that
// left the loop's files out. Three things are let through here and nothing else:
// the quoted text of an sdlc flag that only carries text, a pathspec git reads
// as an exclusion, and a request for a command's help.
//
// Every table of what is let through sits beside the table of the acts nearest
// to it, which are measured against the released tool and keep its rule. The
// tables are built from parts, a carrier and a text and a quoting, so that a
// reading which knows the reported commands by heart fails them.

const (
	refusedAsLoopState   = "loop-state-through-the-tool"
	refusedAsProtected   = "protected-path-through-the-tool"
	refusedAsNewTest     = "no-new-test-after-the-freeze"
	refusedAsImplementer = "implementer-does-not-write-tests"
	refusedAsUnfreeze    = "unfreeze-is-a-human-decision"
	refusedAsAck         = "acknowledgement-is-a-human-decision"
	refusedAsRelease     = "release-is-a-human-decision"
	refusedAsStop        = "stop-is-a-human-decision"
	refusedAsReview      = "review-is-recorded-by-its-reviewer"
)

// corpusState is the session a row is asked in: the main conversation of a
// story whose gates have not passed, unless the row says who or what else.
func corpusState(who string, ps bool) State {
	isTest := func(p string) bool { return strings.HasSuffix(p, "_test.go") }
	s := State{CommitWhy: "code_review has not passed", Backlog: "user_stories.json", PowerShell: ps}
	switch who {
	case "":
	case "passed":
		s.CommitReady, s.CommitWhy = true, ""
	case "frozen":
		s.Frozen = []string{aFrozenTest}
		s.NewTest = func(p string) bool { return isTest(p) && p != aFrozenTest }
	case "implementer":
		s.Agent = "implementer"
		s.ImplementerTest = isTest
	case "architect":
		s.Agent = "architect"
	case "manager":
		s.Agent = "project-manager"
	default:
		panic("no such session: " + who)
	}
	return s
}

// corpusRow is one command and what both entry points have to say of it.
type corpusRow struct {
	who     string // "" the main conversation; or passed, frozen, implementer, architect, manager
	ps      bool   // the command arrived from the PowerShell tool
	command string
	rule    string // the rule Inspect names
	person  string // the rule HumanDecisions names while a story waits for a person; "" where that is not pinned
}

func (r corpusRow) name(criterion string) string {
	name := criterion
	if r.who != "" {
		name += " " + r.who
	}
	if r.ps {
		name += " PowerShell"
	}
	return name + " " + r.command
}

// notRefused holds a row to being let through at both entry points.
func (r corpusRow) notRefused(t *testing.T) {
	t.Helper()
	if f, ok := Inspect(r.command, corpusState(r.who, r.ps)); ok {
		t.Errorf("refused under %s, and the path is only named: %s\n%s", f.Rule, r.command, f.Reason)
	}
	for _, waits := range []bool{true, false} {
		if f, ok := HumanDecisions(r.command, r.ps, func(string) bool { return waits }); ok {
			t.Errorf("HumanDecisions (a story waits: %v) reported %s for %s", waits, f.Rule, r.command)
		}
	}
	// US-023 AC-5: a mention of AGENTS.md is let through as one of CLAUDE.md is.
	alsoAllowedAsAgents(t, r.command, corpusState(r.who, r.ps), func(t *testing.T, command string, _ State) {
		t.Helper()
		respelt := r
		respelt.command = command
		respelt.notRefused(t)
	})
}

// stillRefused holds a row to the rule the released tool refuses it under.
func (r corpusRow) stillRefused(t *testing.T) Finding {
	t.Helper()
	f := refused(t, r.command, corpusState(r.who, r.ps), r.rule)
	if r.person != "" {
		got, ok := HumanDecisions(r.command, r.ps, aStoryWaits)
		if !ok || got.Rule != r.person {
			t.Errorf("HumanDecisions(%q) = %q, %v with a story waiting; want %s", r.command, got.Rule, ok, r.person)
		}
	}
	return f
}

// saysItCouldNotTell is the one thing held of the words of a refusal the
// classifier makes because it cannot place an argument.
func saysItCouldNotTell(f Finding) bool {
	reason := strings.ToLower(f.Reason)
	return strings.Contains(reason, "cannot tell") || strings.Contains(reason, "could not tell")
}

// allowedOrNotTold holds a row the shell reads as text, written in a way a
// classifier need not follow: it is let through, or it is refused under the
// released rule and told why. Refused in silence, as today, is neither.
func (r corpusRow) allowedOrNotTold(t *testing.T) {
	t.Helper()
	// US-023 AC-5: the same of AGENTS.md.
	alsoRefusedAsAgents(t, r.command, corpusState(r.who, r.ps), []string{r.rule}, func(t *testing.T, command string, _ State) {
		t.Helper()
		respelt := r
		respelt.command = command
		respelt.allowedOrNotTold(t)
	})
	f, ok := Inspect(r.command, corpusState(r.who, r.ps))
	if !ok {
		return
	}
	if f.Rule != r.rule {
		t.Errorf("%s was refused by %s, want %s or no refusal", r.command, f.Rule, r.rule)
	}
	if !saysItCouldNotTell(f) {
		t.Errorf("%s only names the path and was refused without being told the classifier could not tell:\n%s", r.command, f.Reason)
	}
}

func corpusNotRefused(t *testing.T, r corpusRow) {
	t.Helper()
	r.notRefused(t)
}

func corpusStillRefused(t *testing.T, r corpusRow) {
	t.Helper()
	r.stillRefused(t)
}

func corpusAllowedOrNotTold(t *testing.T, r corpusRow) {
	t.Helper()
	r.allowedOrNotTold(t)
}

func corpusRun(t *testing.T, criterion string, rows []corpusRow, check func(*testing.T, corpusRow)) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatal("the table is empty, so nothing was asked")
	}
	for _, r := range rows {
		t.Run(r.name(criterion), func(t *testing.T) { check(t, r) })
	}
}

// --- AC-1: the text of an sdlc flag -----------------------------------------

// corpusCarriers are sdlc commands a session may run, each with one flag that
// only carries text. VALUE is where the text goes.
var corpusCarriers = []struct{ who, command string }{
	{"", `sdlc escalate spec_unclear --message VALUE`},
	{"", `sdlc escalate spec_unclear --message=VALUE`},
	{"", `sdlc escalate pre_commit_approval --message VALUE --json`},
	{"", `sdlc --json escalate spec_unclear --message VALUE`},
	{"", `sdlc gate analysis pass --note VALUE`},
	{"", `sdlc gate analysis fail --note=VALUE`},
	{"", `sdlc gate tests_frozen pass --note VALUE --json`},
	{"", `sdlc cost add --usd 1 --note VALUE`},
	{"", `sdlc escalate spec_unclear --message "a" --message VALUE`},
	{"", `sdlc escalate spec_unclear --message VALUE && sdlc status`},
	{"", `sdlc status && sdlc escalate spec_unclear --message VALUE`},
	{"architect", `sdlc review add design_review architect block --note VALUE`},
	{"manager", `sdlc staffing amend perf --left-out --reason VALUE`},
	{"manager", `sdlc staffing amend --reason VALUE`},
}

// corpusText is something a session says about a path, and the rule the
// released tool names when the same characters are the shell's to act on.
type corpusText struct {
	text   string
	rule   string
	frozen bool // the path is a test, asked while internal/x_test.go is frozen
}

var corpusTexts = []corpusText{
	{text: `see -> .sdlc/config.json`, rule: refusedAsLoopState},
	{text: `see -> CLAUDE.md`, rule: refusedAsProtected},
	{text: `see -> user_stories.json`, rule: refusedAsProtected},
	{text: `see -> .sdlc/stories/A-1/gate-record.json`, rule: refusedAsLoopState},
	{text: `see >> .sdlc/state/active`, rule: refusedAsLoopState},
	{text: `see -> .claude/settings.json`, rule: refusedAsProtected},
	{text: `see -> .git/config`, rule: refusedAsProtected},
	{text: `refused; rm .sdlc/state/active`, rule: refusedAsLoopState},
	{text: `blocked; rm .sdlc/state/active was refused`, rule: refusedAsLoopState},
	{text: `plan edits; touch .sdlc/config.json by hand`, rule: refusedAsLoopState},
	{text: `perf back; cp .sdlc/config.json shows why`, rule: refusedAsLoopState},
	{text: `refused | tee .sdlc/state/active`, rule: refusedAsLoopState},
	{text: `refused && rm CLAUDE.md`, rule: refusedAsProtected},
	{text: `refused || rm -rf .git`, rule: refusedAsProtected},
	{text: `refused & rm .sdlc/state/tests.lock`, rule: refusedAsLoopState},
	{text: "line one\nrm .sdlc/state/active", rule: refusedAsLoopState},
	{text: "the hook refused `rm .sdlc/state/active`", rule: refusedAsLoopState},
	{text: "the hook refused `echo x > CLAUDE.md`", rule: refusedAsProtected},
	{text: `the hook refused $(rm .sdlc/state/active)`, rule: refusedAsLoopState},
	{text: `see -> internal/x_test.go`, rule: throughTheTool, frozen: true},
	{text: `see -> internal/new_test.go`, rule: refusedAsNewTest, frozen: true},
	{text: `refused; rm internal/x_test.go`, rule: throughTheTool, frozen: true},
	{text: `refused; touch internal/new_test.go`, rule: refusedAsNewTest, frozen: true},
}

// The three ways a text reaches the flag. Between single quotes a POSIX shell
// runs nothing; between double quotes it runs what a backtick or `$(` opens;
// bare, every operator in the text is the shell's own.
const (
	inSingleQuotes = iota
	inDoubleQuotes
	outsideQuotes
)

func corpusQuoted(text string, quoting int) string {
	switch quoting {
	case inSingleQuotes:
		return "'" + text + "'"
	case inDoubleQuotes:
		return `"` + text + `"`
	}
	return text
}

// shellRunsIt says whether a POSIX shell acts on a text quoted this way. The
// shell itself is asked the same of every row in namedonly_shell_test.go.
func (x corpusText) shellRunsIt(quoting int) bool {
	switch quoting {
	case inSingleQuotes:
		return false
	case inDoubleQuotes:
		return strings.ContainsAny(x.text, "`$")
	}
	return true
}

// corpusTextRows is every carrier with every text in every quoting, kept where
// the shell's reading is the one asked for.
func corpusTextRows(runsIt bool) []corpusRow {
	var rows []corpusRow
	for _, c := range corpusCarriers {
		for _, x := range corpusTexts {
			who := c.who
			if x.frozen {
				if c.who != "" {
					continue
				}
				who = "frozen"
			}
			for _, quoting := range []int{inSingleQuotes, inDoubleQuotes, outsideQuotes} {
				if x.shellRunsIt(quoting) != runsIt {
					continue
				}
				rows = append(rows, corpusRow{who: who, rule: x.rule,
					command: strings.ReplaceAll(c.command, "VALUE", corpusQuoted(x.text, quoting))})
			}
		}
	}
	return rows
}

func TestAC1APathInTheQuotedTextOfAnSdlcFlagIsNotRefused(t *testing.T) {
	corpusRun(t, "AC-1", corpusTextRows(false), corpusNotRefused)
}

// The same parts as a PowerShell session writes them. PowerShell runs `$(...)`
// between double quotes and nothing between single ones; a backslash is a
// character like any other there.
var (
	corpusPowerShellCarriers = []struct{ who, command string }{
		{"", `sdlc escalate spec_unclear --message VALUE`},
		{"", `sdlc.exe escalate spec_unclear --message VALUE`},
		{"", `& sdlc escalate spec_unclear --message VALUE`},
		{"", `sdlc gate analysis pass --note VALUE`},
		{"", `sdlc cost add --usd 1 --note VALUE; Write-Output done`},
		{"manager", `sdlc staffing amend perf --left-out --reason VALUE`},
	}
	corpusPowerShellTexts = []corpusText{
		{text: `see -> .sdlc/config.json`, rule: refusedAsLoopState},
		{text: `see -> CLAUDE.md`, rule: refusedAsProtected},
		{text: `see >> .sdlc\state\active`, rule: refusedAsLoopState},
		{text: `refused; Remove-Item .sdlc/state/active`, rule: refusedAsLoopState},
		{text: `refused; rm .sdlc\state\active`, rule: refusedAsLoopState},
		{text: `refused | Set-Content CLAUDE.md`, rule: refusedAsProtected},
		{text: `refused | Out-File .sdlc/config.json`, rule: refusedAsLoopState},
		{text: "line one\nRemove-Item .sdlc/state/active", rule: refusedAsLoopState},
		{text: `the hook refused $(Remove-Item .sdlc/state/active)`, rule: refusedAsLoopState},
		{text: `see -> internal/x_test.go`, rule: throughTheTool, frozen: true},
		{text: `refused; Remove-Item internal\x_test.go`, rule: throughTheTool, frozen: true},
		{text: `see -> internal/new_test.go`, rule: refusedAsNewTest, frozen: true},
	}
)

func corpusPowerShellRows(runsIt bool, quotings ...int) []corpusRow {
	var rows []corpusRow
	for _, c := range corpusPowerShellCarriers {
		for _, x := range corpusPowerShellTexts {
			who := c.who
			if x.frozen {
				if c.who != "" {
					continue
				}
				who = "frozen"
			}
			for _, quoting := range quotings {
				if x.shellRunsIt(quoting) != runsIt {
					continue
				}
				rows = append(rows, corpusRow{who: who, ps: true, rule: x.rule,
					command: strings.ReplaceAll(c.command, "VALUE", corpusQuoted(x.text, quoting))})
			}
		}
	}
	return rows
}

func TestAC1APathInTheQuotedTextOfAnSdlcFlagIsNotRefusedInPowerShell(t *testing.T) {
	corpusRun(t, "AC-1", corpusPowerShellRows(false, inSingleQuotes, inDoubleQuotes), corpusNotRefused)
}

// The text is quoted and something else on the line is not sdlc: a program
// that reads the command's output and is handed no file, or a brace in the
// text, which the released tool expanded before it looked. sdlc prints the
// text it is given, and nothing tells a program that prints what it reads from
// one that runs it; held to being let through, the rows with a pipe took the
// rows of the next test through with them. So a row is let through, or it is
// refused under the rule the released tool names and told why. Refused in
// silence, as today, is neither.
func TestAC1AC6ATextFlagBesideAProgramThatReadsItsInputIsAllowedOrToldItCouldNotTell(t *testing.T) {
	corpusRun(t, "AC-1 AC-6", []corpusRow{
		{command: `sdlc escalate spec_unclear --message "about .sdlc/config.json" | python3 -c 'import sys; print(sys.stdin.read())'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message 'about CLAUDE.md' | python3 -c 'import sys; print(sys.stdin.read())'`, rule: refusedAsProtected},
		{command: `sdlc gate analysis pass --note "read .sdlc/config.json" --json | perl -ne 'print'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "about .sdlc/state/active" --json | node -e 'process.stdin.pipe(process.stdout)'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "about user_stories.json" | ruby -e 'puts STDIN.read'`, rule: refusedAsProtected},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "about internal/x_test.go" | awk '{print}'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'about internal/x_test.go' | sed -n 1,3p`, rule: throughTheTool},
		{who: "frozen", command: `sdlc gate implementation fail --note "internal/x_test.go fails" | gawk '{print}'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "about internal/new_test.go" | awk '{print}'`, rule: refusedAsNewTest},
		{who: "implementer", command: `sdlc escalate spec_unclear --message "about internal/x_test.go" | awk '{print}'`, rule: refusedAsImplementer},
		{who: "implementer", command: `sdlc escalate frozen_test_wrong --message "see -> internal/x_test.go"`, rule: refusedAsImplementer},
		{who: "implementer", command: `sdlc escalate frozen_test_wrong --message 'internal/x_test.go asserts 0; rm internal/x_test.go was refused'`, rule: refusedAsImplementer},
		{command: `sdlc escalate spec_unclear --message "choose {a,b}; rm .sdlc/state/active was refused"`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message 'see {x,y} -> .sdlc/config.json'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "it's refused; rm .sdlc/state/active"`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message 'he said "no"; rm .sdlc/state/active'`, rule: refusedAsLoopState},
	}, corpusAllowedOrNotTold)
}

// The value of the flag is an act on a protected path, quoted, and the program
// after the pipe runs what it reads: sdlc prints the text, and the program
// does to the path what the text says. These are the lines of the test above
// with the reading program changed for one that runs its input, and each is
// refused under the rule the released tool names, whatever is decided of the
// lines above. awk, gawk and sed are here with a test, where the released tool
// reads the whole line for them; with the loop's own files it lets them
// through, and so does it a shell on standard input, which is not this table's
// to close.
func TestAC5ATextFlagPipedToAProgramThatRunsItsInputIsStillRefused(t *testing.T) {
	const (
		system = `python3 -c 'import os,sys; os.system(sys.stdin.read())'`
		execs  = `node -e 'require("child_process").execSync(require("fs").readFileSync(0,"utf8"))'`
	)
	corpusRun(t, "AC-5", []corpusRow{
		{command: `sdlc escalate spec_unclear --message 'rm .sdlc/state/active' | ` + system, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "rm .sdlc/state/active" | python3 -c 'import subprocess,sys; subprocess.run(sys.stdin.read(), shell=True)'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message 'echo x > .sdlc/config.json' | ` + system, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "rm CLAUDE.md" | ` + system, rule: refusedAsProtected},
		{command: `sdlc escalate spec_unclear --message='rm .sdlc/state/active' | python3 -c 'exec(open(0).read())'`, rule: refusedAsLoopState},
		{command: `sdlc gate analysis pass --note 'rm .sdlc/state/tests.lock' --json | perl -ne 'system($_)'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "rm .sdlc/state/active" | perl -e 'system(<STDIN>)'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message 'rm -rf .git' | perl -ne 'system($_)'`, rule: refusedAsProtected},
		{command: `sdlc escalate spec_unclear --message 'rm .sdlc/state/active' --json | ` + execs, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "rm user_stories.json" | ` + execs, rule: refusedAsProtected},
		{command: `sdlc escalate spec_unclear --message 'rm .sdlc/state/active' | ruby -e 'system(STDIN.read)'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "rm user_stories.json" | ruby -ne 'system($_)'`, rule: refusedAsProtected},
		{command: `sdlc cost add --usd 1 --note 'cp /tmp/x .sdlc/config.json' | ruby -e 'system(STDIN.read)'`, rule: refusedAsLoopState},
		// The texts of the first table of this file, which are text to the
		// shell and a command to the program they are piped to.
		{command: `sdlc escalate spec_unclear --message 'refused; rm .sdlc/state/active' | ` + system, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "see -> .sdlc/config.json" | perl -ne 'system($_)'`, rule: refusedAsLoopState},
		{who: "architect", command: `sdlc review add design_review architect block --note 'rm .sdlc/state/active' | ` + system, rule: refusedAsLoopState},
		{who: "manager", command: `sdlc staffing amend --reason 'rm .sdlc/state/active' | perl -ne 'system($_)'`, rule: refusedAsLoopState},
		// A test, while one is frozen: awk, gawk and sed run their input too.
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | awk '{system($0)}'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "rm internal/x_test.go" | awk '{ system($0) }'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc gate implementation fail --note 'rm internal/x_test.go' | gawk '{system($0)}'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | awk '{print | "sh"}'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | sed -n 'e'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "rm internal/x_test.go" | sed e`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | sed 's/^/ /e'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'touch internal/new_test.go' | awk '{system($0)}'`, rule: refusedAsNewTest},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "touch internal/new_test.go" | sed e`, rule: refusedAsNewTest},
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | ` + system, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "rm internal/x_test.go" | perl -ne 'system($_)'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message 'touch internal/new_test.go' | ruby -e 'system(STDIN.read)'`, rule: refusedAsNewTest},
		// The implementer's.
		{who: "implementer", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | awk '{system($0)}'`, rule: refusedAsImplementer},
		{who: "implementer", command: `sdlc escalate spec_unclear --message "echo x > internal/y_test.go" | gawk '{system($0)}'`, rule: refusedAsImplementer},
		{who: "implementer", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | sed e`, rule: refusedAsImplementer},
		{who: "implementer", command: `sdlc escalate spec_unclear --message 'rm internal/x_test.go' | ` + execs, rule: refusedAsImplementer},
		// PowerShell.
		{ps: true, command: `sdlc escalate spec_unclear --message 'Remove-Item .sdlc/state/active' | ` + system, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message "Remove-Item .sdlc/state/active" | perl -ne 'system($_)'`, rule: refusedAsLoopState},
	}, corpusStillRefused)
}

// What the released tool already let through, and has to go on letting
// through: text with no operator in it, a variable the classifier cannot see
// into, and a gate's document on standard input, which is how every agent
// stores one.
func TestAC1TextTheReleasedToolAllowsStaysAllowed(t *testing.T) {
	corpusRun(t, "AC-1", []corpusRow{
		{command: `sdlc escalate pre_commit_approval --message "Should .sdlc/config.json raise coverage_min?"`},
		{command: `sdlc escalate pre_commit_approval --message 'Should .sdlc/config.json raise coverage_min?'`},
		{command: `sdlc escalate pre_commit_approval --message=".sdlc/config.json is wrong"`},
		{command: `sdlc gate analysis pass --note "read .sdlc/config.json and user_stories.json"`},
		{command: `sdlc escalate spec_unclear --message "see -> $F"`},
		{who: "frozen", command: `sdlc escalate frozen_test_wrong --message "internal/x_test.go asserts 0 where AC-1 says 3"`},
		{command: "sdlc artifact write analysis <<'SDLC_DOCUMENT'\nsee -> .sdlc/config.json; rm .sdlc/state/active\nSDLC_DOCUMENT"},
		{ps: true, command: "@'\nsee -> .sdlc/config.json\n'@ | sdlc artifact write analysis"},
	}, corpusNotRefused)
}

// A POSIX shell reads each of these as one text and runs nothing in it, but
// only by a rule of quoting beyond a pair of quotes: an escaped quote, two
// strings set side by side, the $'...' form, a backslash before a newline; or
// sdlc is reached through another word. Which of them a classifier follows is
// its own to decide. What it may not do is refuse one and not say why.
func TestAC1AC6TextTheClassifierNeedNotFollowIsAllowedOrToldItCouldNotTell(t *testing.T) {
	rows := append(append([]corpusRow{}, corpusHardQuoting...), corpusOtherSpellings...)
	corpusRun(t, "AC-1 AC-6", rows, corpusAllowedOrNotTold)
}

// corpusHardQuoting is the quoting; the shell is asked about each row in
// namedonly_shell_test.go.
var corpusHardQuoting = []corpusRow{
	{command: `sdlc escalate spec_unclear --message "a\"; rm .sdlc/state/active; echo \"b"`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message $'a\'; rm .sdlc/state/active; echo \'b'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message 'a'\''; rm .sdlc/state/active; echo '\''b'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message "a""; rm .sdlc/state/active; echo ""b"`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message 'a''; rm .sdlc/state/active; echo ''b'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message "see \\> .sdlc/config.json"`, rule: refusedAsLoopState},
	{command: "sdlc escalate spec_unclear --message \"see \\\n-> .sdlc/config.json\"", rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message $'see -> .sdlc/config.json'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message $'refused;\n rm .sdlc/state/active'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message "costs \$5; rm .sdlc/state/active"`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message "see -> .sdlc/config.json" && echo $'done'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message 'refused; rm .sdlc/state/active' && echo a\ b`, rule: refusedAsLoopState},
	{command: `echo {a,b} && sdlc gate analysis pass --note "see -> CLAUDE.md"`, rule: refusedAsProtected},
	{command: `sdlc escalate spec_unclear --message "see -> .sdlc/config.json" # then a person reads it`, rule: refusedAsLoopState},
	{command: "sdlc gate analysis pass --note 'see -> .sdlc/config.json' <<'SDLC_DOCUMENT'\nbody\nSDLC_DOCUMENT", rule: refusedAsLoopState},
}

// corpusOtherSpellings reach sdlc through another word.
var corpusOtherSpellings = []corpusRow{
	{command: `/usr/local/bin/sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
	{command: `npx @bbsnly/sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
	{command: `SDLC escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
	{command: `env sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
	{command: `timeout 60 sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
	{command: `(sdlc escalate spec_unclear --message "see -> .sdlc/config.json")`, rule: refusedAsLoopState},
	{command: `cd /tmp && sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
}

// --- AC-5: the acts nearest to that text ------------------------------------

// The texts of AC-1 with the quotes left off, or between double quotes with a
// substitution in them: now the shell acts on what the text says.
func TestAC5TheSameTextWhereTheShellActsOnItIsStillRefused(t *testing.T) {
	rows := append(corpusTextRows(true), corpusPowerShellRows(true, inDoubleQuotes)...)
	corpusRun(t, "AC-5", rows, corpusStillRefused)
}

func TestAC5AnActBesideTheTextOfAnSdlcFlagIsStillRefused(t *testing.T) {
	corpusRun(t, "AC-5", append([]corpusRow{
		// The text is closed and the shell's own redirect follows it.
		{command: `sdlc escalate spec_unclear --message "x" > .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"> .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x">.sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message x>.sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message="x" > .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message see > .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" &> .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" >| .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" 1> .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" >&.sdlc/state/active`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" --message y > .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "see -> .sdlc/config.json" > .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" --file > .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" 2> CLAUDE.md`, rule: refusedAsProtected},
		{command: `sdlc escalate spec_unclear --message "x" >> user_stories.json`, rule: refusedAsProtected},
		// Another command follows the text.
		{command: `sdlc escalate spec_unclear --message "x"; rm .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message="x"; rm .sdlc/state/active`, rule: refusedAsLoopState},
		{command: "sdlc escalate spec_unclear --message \"x\"\nrm .sdlc/state/active", rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" | tee .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `sdlc gate analysis pass --note "x" & rm .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `sdlc gate analysis pass --note "x" || rm -rf .git`, rule: refusedAsProtected},
		{command: `sdlc escalate spec_unclear --message "x" && cd .sdlc/state && rm active`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; echo .sdlc/state/tests.lock | xargs rm`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; find . -name active -delete`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" ; rm .sdlc/state/active # "`, rule: refusedAsLoopState},
		{command: "sdlc escalate spec_unclear --message \"x\" # \"comment\nrm .sdlc/state/active", rule: refusedAsLoopState},
		{command: "sdlc escalate spec_unclear --message x # it's a comment\nrm .sdlc/state/active", rule: refusedAsLoopState},
		// The shell runs a substitution between double quotes.
		{command: "sdlc escalate spec_unclear --message \"x `rm .sdlc/state/active`\"", rule: refusedAsLoopState},
		{command: "sdlc escalate spec_unclear --message \"`rm .sdlc/state/active`\"", rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x $(echo y > .sdlc/config.json)"`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "$(rm .sdlc/state/active)"`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "${X:-$(rm .sdlc/state/active)}"`, rule: refusedAsLoopState},
		{command: "sdlc escalate spec_unclear --message \"x `echo y > CLAUDE.md`\"", rule: refusedAsProtected},
		// The spellings of a path earlier work closed, after a text flag.
		{command: `sdlc escalate spec_unclear --message "x"; rm '.sdlc'/state/active`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; rm .sd"lc"/state/active`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; rm $'.sdlc/state/active'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; rm .sd\lc/state/active`, rule: refusedAsLoopState},
		{command: "sdlc escalate spec_unclear --message \"x\"; rm .sd\\\nlc/state/active", rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; rm .sdlc/{state/active,config.json}`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; rm $(pwd)/.sdlc/state/active`, rule: refusedAsLoopState},
		{command: "sdlc escalate spec_unclear --message \"x\"; rm `pwd`/.sdlc/state/active", rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; echo y > 'CLAUDE'.md`, rule: refusedAsProtected},
		// A program that is handed the path in its own words, or a shell
		// handed the line as a script.
		{command: `sdlc escalate spec_unclear --message "about docs" | python3 -c 'open(".sdlc/config.json","w")'`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "about .sdlc/config.json" | python3 -c 'open(".sdlc/config.json","w")'`, rule: refusedAsLoopState},
		{command: `sdlc status --json | python3 -c 'open(".sdlc/config.json","w")'`, rule: refusedAsLoopState},
		{command: `sh -c 'sdlc escalate spec_unclear --message "x; rm .sdlc/state/active"'`, rule: refusedAsLoopState},
		{command: `bash -c "sdlc escalate spec_unclear --message 'x'; rm .sdlc/state/active"`, rule: refusedAsLoopState},
		{command: "bash -c 'sdlc escalate spec_unclear --message \"see -> .sdlc/config.json\" > .sdlc/config.json'", rule: refusedAsLoopState},
		{command: "sh <<'EOF'\nsdlc escalate spec_unclear --message \"x\"\nrm .sdlc/state/active\nEOF", rule: refusedAsLoopState},
		{command: `eval sdlc escalate spec_unclear --message "x; rm .sdlc/state/active"`, rule: refusedAsLoopState},
		{command: `eval "sdlc escalate spec_unclear --message x; rm .sdlc/state/active"`, rule: refusedAsLoopState},
		// A person's decision inside the text, where the shell runs it.
		{command: `sdlc escalate spec_unclear --message "$(sdlc approve A-1)"`, rule: approvalRule, person: approvalRule},
		{command: "sdlc escalate spec_unclear --message \"A person runs `sdlc approve A-1`\"", rule: approvalRule, person: approvalRule},
		{command: `sdlc escalate spec_unclear --message "$(sdlc unfreeze --reason x)"`, rule: refusedAsUnfreeze, person: refusedAsUnfreeze},
		{command: `sdlc gate analysis pass --note "$(sdlc ack --through HEAD)"`, rule: refusedAsAck, person: refusedAsAck},
		// A test, while one is frozen, and the implementer's.
		{who: "frozen", command: `sdlc escalate spec_unclear --message "x" > internal/x_test.go`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "x" > internal/new_test.go`, rule: refusedAsNewTest},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "about internal/x_test.go" | awk '{print > "internal/x_test.go"}'`, rule: throughTheTool},
		{who: "frozen", command: `sdlc escalate spec_unclear --message "about docs" | sed -n 'w internal/x_test.go'`, rule: throughTheTool},
		{who: "implementer", command: `sdlc escalate spec_unclear --message "x" > internal/y_test.go`, rule: refusedAsImplementer},
		// PowerShell. The last row is text to a POSIX shell and an act here,
		// where a backslash does not escape the quote after it.
		{ps: true, command: `sdlc escalate spec_unclear --message "x" > .sdlc/config.json`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message 'x'; Remove-Item .sdlc/state/active`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message "x" | Out-File .sdlc\config.json`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message "x $(Remove-Item .sdlc/state/active)"`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message 'a'; Remove-Item .sdlc/state/active; echo 'b'`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message refused; Remove-Item .sdlc/state/active`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message refused | Out-File .sdlc/config.json`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message refused | Set-Content CLAUDE.md`, rule: refusedAsProtected},
		{ps: true, command: `sdlc escalate spec_unclear --message see >> .sdlc\state\active`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear --message "a\"; Remove-Item .sdlc/state/active; echo "b"`, rule: refusedAsLoopState},
	}, corpusQuoteClosers...), corpusStillRefused)
}

// corpusQuoteClosers look like text in quotes and are not: the quote closes
// before the operator and the shell runs what follows, or a quote is never
// closed and the shell runs nothing. The first two are rows of
// corpusHardQuoting with one character changed. The shell is asked about each
// in namedonly_shell_test.go.
var corpusQuoteClosers = []corpusRow{
	{command: `sdlc escalate spec_unclear --message "a\\"; rm .sdlc/state/active; echo "b"`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message 'a\'; rm .sdlc/state/active; echo \'b'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message "a" "; rm .sdlc/state/active; echo "b"`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message 'a' '; rm .sdlc/state/active; echo 'b'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message $"a"; rm .sdlc/state/active`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message \"a; rm .sdlc/state/active\"`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message \'a; rm .sdlc/state/active\'`, rule: refusedAsLoopState},
	{command: "sdlc escalate spec_unclear --message \"a\\\n\"; rm .sdlc/state/active", rule: refusedAsLoopState},
	// A quote is never closed, and a shell runs nothing. Still refused.
	{command: `sdlc escalate spec_unclear --message 'it doesn't say > .sdlc/config.json'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message 'it doesn't say; rm .sdlc/config.json'`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message "unterminated; rm .sdlc/state/active`, rule: refusedAsLoopState},
	{command: `sdlc escalate spec_unclear --message 'unterminated; rm .sdlc/state/active`, rule: refusedAsLoopState},
}

// A reading that follows quotes walks text a session wrote, and a walk that
// runs off the end of it is a panic, which the hook answers by letting the
// call through. However long the text and however many quotes it opens, the
// act after it is refused under the same rule.
func TestAC5AnActAfterAVeryLongOrManyQuotedTextIsStillRefused(t *testing.T) {
	const act = `; rm .sdlc/state/active`
	for name, text := range map[string]string{
		"a long text":                      `"` + strings.Repeat("refused; rm .sdlc/state/active ", 2000) + `"`,
		"many quoted words":                strings.Repeat(`"see -> .sdlc/config.json" 'and; rm it' `, 1500),
		"many escaped quotes":              `"` + strings.Repeat(`\"; rm .sdlc/state/active `, 2000) + `"`,
		"substitutions nested deep":        `"` + strings.Repeat("$(echo ", 24) + "x" + strings.Repeat(")", 24) + `"`,
		"quotes that alternate to the end": strings.Repeat(`"'`, 4000) + strings.Repeat(`'"`, 4000),
		"a text that is never closed":      `"` + strings.Repeat("see -> .sdlc/config.json ", 2000),
	} {
		t.Run("AC-5 "+name, func(t *testing.T) {
			corpusRow{command: `sdlc escalate spec_unclear --message ` + text + act, rule: refusedAsLoopState}.stillRefused(t)
		})
	}
}

// The two exceptions have not spread: text in an argument of another program,
// in an sdlc argument that is not a text flag, or in a command only a person
// runs, is refused as the released tool refuses it.
func TestAC5TextOutsideAnSdlcTextFlagIsStillRefused(t *testing.T) {
	corpusRun(t, "AC-5", []corpusRow{
		{command: `echo "a; rm CLAUDE.md"`, rule: refusedAsProtected},
		{command: `grep -n "x; rm .sdlc/state/active" docs/enforcement.md`, rule: refusedAsLoopState},
		{command: `printf '%s\n' "see -> CLAUDE.md"`, rule: refusedAsProtected},
		{command: `gh issue create --title x --body "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{command: `gh pr comment 1 --note "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{command: `git log --grep "x; rm .sdlc/state/active"`, rule: refusedAsLoopState},
		{command: `mytool --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{who: "passed", command: `git commit -m "docs: explain why > .sdlc/config.json is refused"`, rule: refusedAsLoopState},
		{who: "passed", command: `git commit -m "fix: x; rm .sdlc/state/active is refused"`, rule: refusedAsLoopState},
		{who: "passed", command: `git commit --message "see -> CLAUDE.md"`, rule: refusedAsProtected},
		{ps: true, command: `echo "a; Remove-Item CLAUDE.md"`, rule: refusedAsProtected},
		{ps: true, command: `Write-Output 'see -> .sdlc/config.json'`, rule: refusedAsLoopState},
		// An sdlc argument that names a file or a story is not text.
		{command: `sdlc artifact write analysis --file "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{command: `sdlc escalate "see -> .sdlc/config.json" --message x`, rule: refusedAsLoopState},
		{command: `sdlc start "x; rm .sdlc/state/active"`, rule: refusedAsLoopState},
		{command: `sdlc gate analysis pass --story "x; rm .sdlc/state/active"`, rule: refusedAsLoopState},
		// A text flag does not make a person's command the session's.
		{command: `sdlc unfreeze --reason "see -> .sdlc/config.json"`, rule: refusedAsUnfreeze, person: refusedAsUnfreeze},
		{command: `sdlc approve A-1 --reject "see -> .sdlc/config.json"`, rule: approvalRule, person: approvalRule},
		{command: `sdlc release A-1 --reason "refused; rm .sdlc/state/active"`, rule: refusedAsRelease, person: refusedAsRelease},
		{command: `sdlc staffing assign security --reason "see -> CLAUDE.md"`, rule: assignRule, person: assignRule},
	}, corpusStillRefused)
}

// --- AC-2: a pathspec that excludes the path --------------------------------

// corpusGitReads are git commands that list or show and change nothing. EXCL
// is where the excluding pathspec goes.
var corpusGitReads = []string{
	`git diff --numstat -- . EXCL`,
	`git diff --stat -- . EXCL`,
	`git diff --name-only HEAD~1 -- . EXCL`,
	`git diff --numstat main...HEAD -- . EXCL`,
	`git log --oneline -- . EXCL`,
	`git status --short -- . EXCL`,
	`git ls-files -- . EXCL`,
	`git grep -n TODO -- . EXCL`,
	`git show --stat HEAD -- . EXCL`,
	`git diff --cached --numstat -- . EXCL`,
}

// corpusExcludes are the ways git is told to leave a path out, each asked of
// git itself in the hook's tests, and the session each is asked in.
var corpusExcludes = []struct{ who, word string }{
	{"", `':(exclude).sdlc'`},
	{"", `":(exclude).sdlc"`},
	{"", `':!.sdlc'`},
	{"", `':^.sdlc'`},
	{"", `:^.sdlc`},
	{"", `':(exclude).sdlc/state'`},
	{"", `':(exclude).sdlc' ':(exclude)CODEMAP.md'`},
	{"", `':(exclude)CLAUDE.md'`},
	{"", `':!CLAUDE.md'`},
	{"", `':(exclude)user_stories.json'`},
	{"", `':^user_stories.json'`},
	{"", `':(exclude).claude'`},
	{"", `':(exclude).git'`},
	{"", `':(exclude,top).sdlc'`},
	{"", `':(top,exclude).sdlc'`},
	{"", `':(exclude,icase).SDLC'`},
	{"", `':(exclude,glob).sdlc/**'`},
	{"", `':/!.sdlc'`},
	{"", `':!/.sdlc'`},
	{"frozen", `':(exclude)internal/x_test.go'`},
	{"frozen", `':!internal/x_test.go'`},
	{"frozen", `':^internal/x_test.go'`},
	{"frozen", `':(exclude)*_test.go'`},
	{"frozen", `':(exclude,glob)**/x_test.go'`},
	{"frozen", `':!*_test.go' ':!testdata'`},
	{"frozen", `':(exclude)internal/new_test.go'`},
	{"implementer", `':(exclude)*_test.go'`},
	{"implementer", `':!*_test.go' ':!testdata'`},
	{"implementer", `':(exclude)internal/x_test.go'`},
	{"implementer", `':^internal/x_test.go'`},
}

// Programs that read the listing on standard input and are handed no file. The
// released tool took every word on a line with one of the first four for a
// file it might write, and, while a test is frozen or the implementer asks,
// every word on a line with awk or sed as well.
var (
	corpusInterpreters = []string{
		`python3 -c 'import sys; print(sys.stdin.read())'`,
		`perl -ne 'print'`,
		`node -e 'process.stdin.pipe(process.stdout)'`,
		`ruby -e 'puts STDIN.read'`,
	}
	corpusFilters = []string{
		`awk '{a+=$1; d+=$2} END {print a+d}'`,
		`gawk '{print}'`,
		`sed -n 1,3p`,
	}
)

// corpusExcludeRows pairs every read and every exclude with the readers given
// to the main conversation and with those given where a test is the path.
func corpusExcludeRows(forState, forTests []string) []corpusRow {
	var rows []corpusRow
	for _, read := range corpusGitReads {
		for _, e := range corpusExcludes {
			readers := forTests
			if e.who == "" {
				readers = forState
			}
			for _, reader := range readers {
				rows = append(rows, corpusRow{who: e.who, command: strings.ReplaceAll(read, "EXCL", e.word) + " | " + reader})
			}
		}
	}
	return rows
}

func TestAC2AnExcludedPathBesideAProgramThatReadsTheListingIsNotRefused(t *testing.T) {
	corpusRun(t, "AC-2", corpusExcludeRows(corpusInterpreters, append(corpusInterpreters, corpusFilters...)), corpusNotRefused)
}

func TestAC2AnExcludedPathBesideAProgramThatReadsTheListingIsNotRefusedInPowerShell(t *testing.T) {
	corpusRun(t, "AC-2", []corpusRow{
		{ps: true, command: `git diff --numstat -- . ':(exclude).sdlc' | perl -ne 'print'`},
		{ps: true, command: `git diff --numstat -- . ":(exclude).sdlc" | perl -ne 'print'`},
		{ps: true, command: `git diff --numstat -- . ':!CLAUDE.md' | node -e 'process.stdin.pipe(process.stdout)'`},
		{who: "frozen", ps: true, command: `git diff --numstat -- . ':(exclude)internal/x_test.go' | awk '{print}'`},
	}, corpusNotRefused)
}

// What the released tool already let through beside an exclude: the read on
// its own, a filter the path rules do not hold against the loop's files, a
// sort, and an exclude built from a variable.
func TestAC2AnExcludeTheReleasedToolAllowsStaysAllowed(t *testing.T) {
	rows := corpusExcludeRows(corpusFilters, nil)
	rows = append(rows, corpusExcludeRows([]string{`sort -n`}, []string{`sort -n`})...)
	rows = append(rows,
		corpusRow{command: `git diff --numstat -- . ':(exclude).sdlc'`},
		corpusRow{command: `git add -- . ':(exclude).sdlc'`},
		corpusRow{command: `git stash push -- ':(exclude)build' src`},
		corpusRow{command: `git diff -- . ":(exclude)$D" | perl -ne 'print'`},
		corpusRow{who: "frozen", command: `git diff --numstat -- . ':(exclude)internal/x_test.go'`},
		corpusRow{who: "implementer", command: `git diff --numstat -- . ':(exclude)*_test.go'`},
		corpusRow{ps: true, command: `git diff --numstat -- . ':(exclude).sdlc'`},
		corpusRow{ps: true, command: `git diff --numstat -- . ':(exclude).sdlc' | Measure-Object -Line`},
	)
	corpusRun(t, "AC-2", rows, corpusNotRefused)
}

func TestAC5AWordThatDoesNotExcludeThePathIsStillRefused(t *testing.T) {
	const reads, prints = `python3 -c 'import sys; print(sys.stdin.read())'`, `perl -ne 'print'`
	corpusRun(t, "AC-5", []corpusRow{
		// The path is excluded from the read, and the program beside it is
		// handed the same path in its own words.
		{command: `git diff -- . ':(exclude)docs' | python3 -c 'open(".sdlc/config.json","w")'`, rule: refusedAsLoopState},
		{command: `git diff -- . ':(exclude).sdlc' | python3 -c 'open(".sdlc/config.json","w")'`, rule: refusedAsLoopState},
		{command: `git diff -- . ':(exclude).sdlc/config.json' | python3 -c 'open(".sdlc/config.json","w")'`, rule: refusedAsLoopState},
		{command: `git diff -- . ':(exclude).sdlc' | python3 -c 'open(".sdlc/state/active","w")' ':(exclude).sdlc'`, rule: refusedAsLoopState},
		{command: `echo ':(exclude).sdlc' | python3 -c 'open(".sdlc/config.json","w")'`, rule: refusedAsLoopState},
		{command: `git diff -- . ':!CLAUDE.md' | perl -e 'open(F,">CLAUDE.md")'`, rule: refusedAsProtected},
		{command: `git diff -- . ':(exclude)CLAUDE.md' | node -e 'require("fs").writeFileSync("CLAUDE.md","")'`, rule: refusedAsProtected},
		{command: `git log --oneline -- . ':^user_stories.json' | ruby -e 'File.write("user_stories.json","")'`, rule: refusedAsProtected},
		// The pathspec includes the path: magic that is not exclude, a second
		// pathspec, or an exclude git does not read as one.
		{command: `git diff -- .sdlc ':(exclude)docs' | ` + reads, rule: refusedAsLoopState},
		{command: `git diff -- ':(top).sdlc' | ` + reads, rule: refusedAsLoopState},
		{command: `git diff -- ':(glob).sdlc/**' | ` + reads, rule: refusedAsLoopState},
		{command: `git diff -- ':(icase).SDLC' | ` + reads, rule: refusedAsLoopState},
		{command: `git diff -- ':(literal).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- ':/.sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- ':(attr:x).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- . ':(exclude)docs' .sdlc/config.json | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- CLAUDE.md ':(exclude)docs' | ` + prints, rule: refusedAsProtected},
		{command: `git diff -- ':(top)CLAUDE.md' | ruby -e 'puts STDIN.read'`, rule: refusedAsProtected},
		{command: `git diff -- ':(excluded).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- ':(exclud).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- ':(Exclude).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- '::!.sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- ':.sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- '!.sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- '^.sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- ':(exclude) .sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git diff -- . ':(exclude)docs:.sdlc' | ` + prints, rule: refusedAsLoopState},
		// The path is written at the other end of the line.
		{command: `git diff --numstat -- . ':(exclude).sdlc' > .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `git diff --numstat -- . ':(exclude).sdlc' | tee .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `git diff --numstat -- . ':(exclude).sdlc' | ` + reads + ` > .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `git diff --numstat -- . ':(exclude)CLAUDE.md' | ` + prints + ` > CLAUDE.md`, rule: refusedAsProtected},
		{command: `git diff --numstat -- . ':(exclude).sdlc' | ` + prints + `; rm .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `git diff --numstat -- . ':(exclude).sdlc' | ` + prints + ` && rm -rf .sdlc`, rule: refusedAsLoopState},
		{command: `git diff --name-only -- . ':(exclude).sdlc' | xargs rm`, rule: refusedAsLoopState},
		{command: `git diff --name-only -- . ':(exclude).sdlc' | xargs sed -i s/a/b/`, rule: refusedAsLoopState},
		{command: `git diff ':(exclude).sdlc' | python3 -c 'print(1)'`, rule: refusedAsLoopState},
		// A git command that changes files reads an exclude as everything else.
		{command: `git checkout -- . ':(exclude).sdlc'`, rule: refusedAsLoopState},
		{command: `git checkout -- . ':(exclude).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `git restore -- . ':(exclude)docs'`, rule: refusedAsLoopState},
		{command: `git clean -fdx -- . ':(exclude)docs'`, rule: refusedAsLoopState},
		{command: `git rm -r -- . ':(exclude).sdlc'`, rule: refusedAsLoopState},
		{command: `git stash push -- . ':(exclude).sdlc'`, rule: refusedAsLoopState},
		{command: `git checkout -- .sdlc ':(exclude)docs'`, rule: refusedAsLoopState},
		{command: `git restore -- .sdlc/config.json ':(exclude).sdlc/state'`, rule: refusedAsLoopState},
		{command: `git rm -r -- .sdlc ':(exclude).sdlc/state'`, rule: refusedAsLoopState},
		{command: `git mv .sdlc/config.json ':(exclude)x'`, rule: refusedAsLoopState},
		{command: `git clean -fdx -- .sdlc ':(exclude).sdlc/worktrees'`, rule: refusedAsLoopState},
		{command: `git stash push -- ':(exclude)build'`, rule: refusedAsLoopState},
		{command: `git stash push -- ':(top,exclude)build'`, rule: refusedAsLoopState},
		{command: `git stash push -- ':!build'`, rule: refusedAsLoopState},
		{command: `git clean -fd -- ':!build'`, rule: refusedAsLoopState},
		{command: `git checkout -- ':(exclude)docs'`, rule: refusedAsLoopState},
		{command: `git restore -- ':^docs'`, rule: refusedAsLoopState},
		{command: `git -c alias.d=checkout d -- . ':(exclude)docs'`, rule: refusedAsLoopState},
		{command: `git restore -- CLAUDE.md ':(exclude)docs'`, rule: refusedAsProtected},
		{command: `git checkout HEAD -- CLAUDE.md ':!docs'`, rule: refusedAsProtected},
		// The word is an exclude only to git, and git is not the program.
		{command: `rm -rf ':(exclude).sdlc' .sdlc`, rule: refusedAsLoopState},
		{command: `python3 -c 'import sys; print(sys.argv)' ':(exclude).sdlc'`, rule: refusedAsLoopState},
		{command: `echo ':(exclude).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `grep -rn x -- . ':(exclude).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `hg diff -- . ':(exclude).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `gitk -- . ':(exclude).sdlc' | ` + prints, rule: refusedAsLoopState},
		{command: `perl -ne 'print' ':!CLAUDE.md'`, rule: refusedAsProtected},
		// A test, while one is frozen, and the implementer's.
		{who: "frozen", command: `git diff -- . ':(exclude).sdlc' | awk '{print > "internal/x_test.go"}'`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- . ':(exclude)internal/x_test.go' | awk '{print > "internal/x_test.go"}'`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- . ':(exclude)docs' | sed -n 'w internal/x_test.go'`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- . ':(exclude)internal/x_test.go' | sed -n 'w internal/x_test.go'`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- internal/x_test.go ':(exclude)docs' | awk '{print}'`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- ':(top)internal/x_test.go' | awk '{print}'`, rule: throughTheTool},
		{who: "frozen", command: `git restore --source=HEAD -- internal ':(exclude)internal/y.go'`, rule: throughTheTool},
		{who: "frozen", command: `git rm -- internal/x_test.go ':(exclude).sdlc'`, rule: throughTheTool},
		{who: "frozen", command: `git checkout -- internal ':(exclude)internal/x_test.go'`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- . ':(exclude)internal/x_test.go' | awk '{print}' > internal/x_test.go`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- . ':(exclude)internal/x_test.go' > internal/x_test.go`, rule: throughTheTool},
		{who: "frozen", command: `git diff -- ':(glob)internal/*_test.go' | sed -n 1,3p`, rule: refusedAsNewTest},
		{who: "frozen", command: `git diff -- . ':(exclude)*_test.go' | awk '{print > "internal/new_test.go"}'`, rule: refusedAsNewTest},
		{who: "frozen", command: `git checkout -- . ':(exclude)internal/x_test.go'`, rule: refusedAsLoopState},
		{who: "implementer", command: `git diff -- . ':(exclude)*_test.go' | awk '{print > "internal/y_test.go"}'`, rule: refusedAsImplementer},
		{who: "implementer", command: `git diff -- internal/y_test.go ':(exclude)docs' | awk '{print}'`, rule: refusedAsImplementer},
		{who: "implementer", command: `git diff -- . ':!*_test.go' > internal/y_test.go`, rule: refusedAsImplementer},
		// PowerShell.
		{ps: true, command: `git diff -- .sdlc ':(exclude)docs' | perl -ne 'print'`, rule: refusedAsLoopState},
		{ps: true, command: `git diff -- . ':(exclude).sdlc' | perl -e 'open(F,">.sdlc/config.json")'`, rule: refusedAsLoopState},
		{ps: true, command: `git checkout -- . ':(exclude).sdlc'`, rule: refusedAsLoopState},
		{ps: true, command: `git diff --numstat -- . ':(exclude).sdlc' > .sdlc\state\active`, rule: refusedAsLoopState},
		{ps: true, command: `git stash push -- ':(exclude)build'`, rule: refusedAsLoopState},
	}, corpusStillRefused)
}

// --- AC-3: a request for help -----------------------------------------------

// corpusPersons are the commands only a person runs, with arguments that would
// run each, and the rule each is refused under.
var corpusPersons = []struct{ verb, args, rule string }{
	{"approve", "A-1", approvalRule},
	{"unfreeze", "--reason x", refusedAsUnfreeze},
	{"ack", "--through HEAD", refusedAsAck},
	{"release", "A-1 --reason x", refusedAsRelease},
	{"parallel", "A-1 B-2", parallelRule},
	{"stop", "", refusedAsStop},
	{"staffing assign", "security --model opus --reason x", assignRule},
}

// corpusShape writes one shape of a command line for one command. A newline
// in a shape is kept; other runs of spaces, which an empty ARGS leaves, go.
func corpusShape(shape, verb, args string) string {
	command := strings.ReplaceAll(strings.ReplaceAll(shape, "VERB", verb), "ARGS", args)
	lines := strings.Split(command, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

// corpusShapes is each shape for each command, once.
func corpusShapes(shapes []string, each func(verb, args, rule, command string) corpusRow) []corpusRow {
	var rows []corpusRow
	seen := map[string]bool{}
	for _, p := range corpusPersons {
		for _, shape := range shapes {
			command := corpusShape(shape, p.verb, p.args)
			if seen[command] {
				continue
			}
			seen[command] = true
			rows = append(rows, each(p.verb, p.args, p.rule, command))
		}
	}
	return rows
}

// Each of these the command tree answers with the command's help and nothing
// else; internal/cli asks the tree itself.
var corpusHelpShapes = []string{
	"sdlc VERB --help",
	"sdlc VERB -h",
	"sdlc VERB ARGS --help",
	"sdlc VERB ARGS -h",
	"sdlc VERB --help ARGS",
	"sdlc VERB -h ARGS",
	"sdlc VERB --json --help",
	"sdlc --json VERB --help",
	"sdlc VERB --help --json",
	"sdlc VERB --help --help",
	"sdlc VERB --help 2>&1",
	"sdlc VERB --help | cat",
	"sdlc VERB --help 2>&1 | head -20",
	"sdlc VERB -h | grep -n reason",
}

func TestAC3AskingForTheHelpOfACommandOnlyAPersonRunsIsNotRefused(t *testing.T) {
	rows := corpusShapes(corpusHelpShapes, func(_, _, _, command string) corpusRow { return corpusRow{command: command} })
	rows = append(rows,
		corpusRow{ps: true, command: `sdlc approve --help`},
		corpusRow{ps: true, command: `sdlc.exe approve -h`},
		corpusRow{ps: true, command: `& sdlc unfreeze --help`},
		corpusRow{ps: true, command: `sdlc ack --help; Write-Output done`},
		corpusRow{who: "manager", command: `sdlc approve --help`},
		corpusRow{who: "manager", command: `sdlc unfreeze -h`},
		corpusRow{who: "manager", command: `sdlc staffing assign security --help`},
		corpusRow{who: "implementer", command: `sdlc approve A-1 --help`},
	)
	corpusRun(t, "AC-3", rows, corpusNotRefused)
}

// The same request of the two commands only their owner records with: the
// staffing, which is the project manager's, and a review, which is its
// reviewer's.
func TestAC3AskingForTheHelpOfACommandOnlyItsOwnerRecordsWithIsNotRefused(t *testing.T) {
	corpusRun(t, "AC-3", []corpusRow{
		{command: `sdlc staffing set --help`},
		{command: `sdlc staffing set -h`},
		{command: `sdlc staffing set --file s.json --help`},
		{command: `sdlc staffing set --help --file s.json`},
		{command: `sdlc staffing amend --help`},
		{command: `sdlc staffing amend perf --left-out --reason x -h`},
		{who: "implementer", command: `sdlc staffing set --help`},
		{command: `sdlc review add code_review code-reviewer approve --help`},
		{command: `sdlc review add code_review code-reviewer approve -h`},
		{command: `sdlc review add --help code_review code-reviewer approve`},
		{command: `sdlc review add -h code_review code-reviewer approve`},
		{who: "architect", command: `sdlc review add code_review code-reviewer approve --help`},
		{who: "manager", command: `sdlc review add code_review code-reviewer approve --help`},
	}, corpusNotRefused)
}

// corpusManagerVerbs are commands the project manager may not run, since it
// only staffs, with arguments that would run each.
var corpusManagerVerbs = []struct{ verb, args string }{
	{"gate", "analysis pass"},
	{"escalate", "spec_unclear --message x"},
	{"freeze", ""},
	{"start", ""},
	{"artifact write", "analysis --file a.md"},
	{"cost add", "--usd 1"},
	{"init", ""},
}

func corpusManagerRows(shapes []string, rule string) []corpusRow {
	var rows []corpusRow
	seen := map[string]bool{}
	for _, v := range corpusManagerVerbs {
		for _, shape := range shapes {
			command := corpusShape(shape, v.verb, v.args)
			if seen[command] {
				continue
			}
			seen[command] = true
			rows = append(rows, corpusRow{who: "manager", command: command, rule: rule})
		}
	}
	return rows
}

func TestAC3HelpTheReleasedToolAllowsStaysAllowed(t *testing.T) {
	rows := corpusShapes([]string{"sdlc help VERB"}, func(_, _, _, command string) corpusRow { return corpusRow{command: command} })
	rows = append(rows, corpusManagerRows(corpusHelpShapes[:10], "")...)
	rows = append(rows,
		corpusRow{command: `sdlc --help`},
		corpusRow{command: `sdlc escalate --help`},
		corpusRow{command: `sdlc review add --help`},
		corpusRow{who: "manager", command: `sdlc --help`},
		corpusRow{who: "manager", command: `sdlc staffing --help`},
	)
	corpusRun(t, "AC-3", rows, corpusNotRefused)
}

// --- AC-5: a help flag beside the command it does not stop ------------------

// Each of these runs the command, or may: the command tree takes the last
// value a repeated flag is given, a flag that takes a value takes `--help` for
// it, nothing after `--` is a flag, and the shell fills in a word the
// classifier cannot see. internal/cli asks the tree about the first group.
var corpusNotHelpShapes = []string{
	"sdlc VERB --help=false ARGS",
	"sdlc VERB --help=0 ARGS",
	"sdlc VERB -h=false ARGS",
	"sdlc VERB --help=f ARGS",
	"sdlc VERB --help=F ARGS",
	"sdlc VERB --help=False ARGS",
	"sdlc VERB --help=FALSE ARGS",
	"sdlc VERB --help --help=false ARGS",
	"sdlc VERB -h -h=false ARGS",
	"sdlc VERB --help -h=false ARGS",
	"sdlc VERB -h --help=0 ARGS",
	"sdlc VERB ARGS --help --help=false",
	"sdlc VERB --help ARGS --help=false",
	"sdlc VERB --help=true --help=false ARGS",

	"sdlc VERB ARGS",
	"sdlc VERB --help; sdlc VERB ARGS",
	"sdlc VERB --help && sdlc VERB ARGS",
	"sdlc VERB --help || sdlc VERB ARGS",
	"sdlc VERB ARGS; sdlc VERB --help",
	"sdlc VERB --help\nsdlc VERB ARGS",
	"(sdlc VERB --help); sdlc VERB ARGS",
	"sdlc VERB $H ARGS",
	"sdlc VERB --help$X ARGS",
	"HELP=--help sdlc VERB ARGS",
	"sh -c 'sdlc VERB ARGS' --help",
	"bash -c 'sdlc VERB ARGS' -h",
	"echo --help | xargs sdlc VERB",
	"echo ARGS | xargs sdlc VERB --help",
	"echo --help=false | xargs sdlc VERB --help",
	"sdlc VERB --help | sh",
	"sdlc VERB --help | bash",
	"sdlc VERB -h | sh -s",
	"sdlc VERB --help | xargs sdlc VERB",
	"eval \"$(sdlc VERB --help)\"",
}

func TestAC5AHelpFlagThatDoesNotStopTheCommandIsStillRefused(t *testing.T) {
	rows := corpusShapes(corpusNotHelpShapes, func(verb, _, rule, command string) corpusRow {
		r := corpusRow{command: command, rule: rule, person: rule}
		if verb == "stop" {
			// A stop is refused to the session that holds the story, which
			// only Inspect knows.
			r.person = ""
		}
		return r
	})
	rows = append(rows,
		// A flag that takes a value takes the help flag for it, and nothing
		// after `--` is a flag.
		corpusRow{command: `sdlc approve --reject --help`, rule: approvalRule, person: approvalRule},
		corpusRow{command: `sdlc approve A-1 --reject -h`, rule: approvalRule, person: approvalRule},
		corpusRow{command: `sdlc unfreeze --reason --help`, rule: refusedAsUnfreeze, person: refusedAsUnfreeze},
		corpusRow{command: `sdlc ack --through --help`, rule: refusedAsAck, person: refusedAsAck},
		corpusRow{command: `sdlc release A-1 --reason --help`, rule: refusedAsRelease, person: refusedAsRelease},
		corpusRow{command: `sdlc staffing assign security --reason --help`, rule: assignRule, person: assignRule},
		corpusRow{command: `sdlc approve -- --help`, rule: approvalRule, person: approvalRule},
		corpusRow{command: `sdlc approve -- -h`, rule: approvalRule, person: approvalRule},
		corpusRow{command: `sdlc approve help`, rule: approvalRule, person: approvalRule},
		corpusRow{command: `sdlc parallel A-1 B-2 -- --help`, rule: parallelRule, person: parallelRule},
		corpusRow{command: `sdlc parallel help`, rule: parallelRule, person: parallelRule},
		// The project manager, and PowerShell.
		corpusRow{who: "manager", command: `sdlc approve --help=false A-1`, rule: approvalRule, person: approvalRule},
		corpusRow{who: "manager", command: `sdlc unfreeze --help --help=false --reason x`, rule: refusedAsUnfreeze, person: refusedAsUnfreeze},
		corpusRow{ps: true, command: `sdlc approve --help=false A-1`, rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: `sdlc approve --help --help=false A-1`, rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: `sdlc approve --help; sdlc approve A-1`, rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: `sdlc approve --reject --help`, rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: `sdlc approve -- --help`, rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: "sdlc approve --he`lp=false A-1", rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: `sdlc approve --help | iex`, rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: `sdlc approve --help | Invoke-Expression`, rule: approvalRule, person: approvalRule},
		corpusRow{ps: true, command: `sdlc approve $h A-1`, rule: approvalRule, person: approvalRule},
		// The commands only their owner records with.
		corpusRow{command: `sdlc staffing set --help=false --file s.json`, rule: recorderRule},
		corpusRow{command: `sdlc staffing set --help --help=false --file s.json`, rule: recorderRule},
		corpusRow{command: `sdlc staffing set --file --help`, rule: recorderRule},
		corpusRow{command: `sdlc staffing set --file s.json`, rule: recorderRule},
		corpusRow{command: `sdlc review add --help=false code_review code-reviewer approve`, rule: refusedAsReview},
		corpusRow{command: `sdlc review add --help --help=false code_review code-reviewer approve`, rule: refusedAsReview},
		corpusRow{command: `sdlc review add code_review code-reviewer approve --help --help=false`, rule: refusedAsReview},
		corpusRow{command: `sdlc review add --help; sdlc review add code_review code-reviewer approve`, rule: refusedAsReview},
		corpusRow{command: `sdlc review add code_review code-reviewer approve`, rule: refusedAsReview},
	)
	corpusRun(t, "AC-5", rows, corpusStillRefused)
}

// The help is asked for and its output is the shell's to put over a protected
// path. The released tool refuses these as the command; refused as the write
// they are is as good. Let through is not.
func TestAC5HelpWrittenOverAProtectedPathIsStillRefused(t *testing.T) {
	for _, p := range corpusPersons {
		for shape, pathRule := range map[string]string{
			"sdlc VERB --help > .sdlc/state/active": refusedAsLoopState,
			"sdlc VERB --help >> .sdlc/config.json": refusedAsLoopState,
			"sdlc VERB --help | tee CLAUDE.md":      refusedAsProtected,
		} {
			command := corpusShape(shape, p.verb, p.args)
			t.Run("AC-5 "+command, func(t *testing.T) {
				f, ok := Inspect(command, corpusState("", false))
				if !ok {
					t.Fatalf("allowed: %s", command)
				}
				if f.Rule != p.rule && f.Rule != pathRule {
					t.Errorf("%s was refused by %s, want %s or %s", command, f.Rule, p.rule, pathRule)
				}
			})
		}
	}
}

// The project manager only staffs, and a help flag is how it reads what a
// command takes. A help flag the command tree does not act on, because a later
// one turns it off, was read as help all the same, and the command ran.
var corpusCancelledHelpShapes = []string{
	"sdlc VERB --help --help=false ARGS",
	"sdlc VERB -h -h=false ARGS",
	"sdlc VERB --help -h=false ARGS",
	"sdlc VERB -h --help=0 ARGS",
	"sdlc VERB ARGS --help --help=false",
	"sdlc VERB --help ARGS --help=false",
}

func TestAC3AC5TheProjectManagerCannotRunACommandBehindAHelpFlagThatIsTurnedOff(t *testing.T) {
	corpusRun(t, "AC-3 AC-5", corpusManagerRows(corpusCancelledHelpShapes, managerRule), corpusStillRefused)
}

func TestAC5TheProjectManagerIsStillRefusedACommandWithItsHelpTurnedOff(t *testing.T) {
	rows := corpusManagerRows([]string{
		"sdlc VERB ARGS",
		"sdlc VERB --help=false ARGS",
		"sdlc VERB --help=0 ARGS",
		"sdlc VERB -h=false ARGS",
		"sdlc VERB --help=true --help=false ARGS",
		"sdlc VERB --help; sdlc VERB ARGS",
	}, managerRule)
	rows = append(rows,
		corpusRow{who: "manager", command: `sdlc gate analysis pass --note --help`, rule: managerRule},
		corpusRow{who: "manager", command: `sdlc gate analysis pass -- --help`, rule: managerRule},
		corpusRow{who: "manager", command: `sdlc gate -- --help`, rule: managerRule},
	)
	corpusRun(t, "AC-5", rows, corpusStillRefused)
}

// --- AC-6: an argument the classifier cannot place --------------------------

// The text is quoted and the path in it is only named, as far as the shell
// goes. What the classifier cannot be sure of is that sdlc takes it for text:
// the flag is cut short, it comes after `--`, the program is some other file
// called sdlc; or that git takes the pathspec for an exclusion, when it has
// been told to read pathspecs literally. Refused, under the rule the released
// tool names, and told so.
func TestAC6AnArgumentTheClassifierCannotPlaceIsRefusedAndToldItCouldNotTell(t *testing.T) {
	corpusRun(t, "AC-6", []corpusRow{
		{command: `sdlc escalate spec_unclear --messag "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --mess 'refused; rm .sdlc/state/active'`, rule: refusedAsLoopState},
		{command: `sdlc gate analysis pass --not "see -> CLAUDE.md"`, rule: refusedAsProtected},
		{command: `sdlc escalate spec_unclear -- --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{command: `sdlc gate analysis pass -- --note 'refused; rm .sdlc/state/active'`, rule: refusedAsLoopState},
		{command: `./sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{command: `./bin/sdlc gate analysis pass --note 'refused; rm .sdlc/state/active'`, rule: refusedAsLoopState},
		{command: `go run ./cmd/sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`, rule: refusedAsLoopState},
		{command: `go run ./cmd/sdlc gate analysis pass --note 'see -> CLAUDE.md'`, rule: refusedAsProtected},
		{who: "frozen", command: `sdlc escalate spec_unclear --messag "see -> internal/x_test.go"`, rule: throughTheTool},
		{who: "frozen", command: `./sdlc escalate spec_unclear --message "see -> internal/new_test.go"`, rule: refusedAsNewTest},
		{command: `git --literal-pathspecs diff -- . ':(exclude).sdlc' | perl -ne 'print'`, rule: refusedAsLoopState},
		{command: `GIT_LITERAL_PATHSPECS=1 git diff -- . ':(exclude).sdlc' | perl -ne 'print'`, rule: refusedAsLoopState},
		{command: `git --literal-pathspecs log --oneline -- . ':!CLAUDE.md' | python3 -c 'import sys; print(sys.stdin.read())'`, rule: refusedAsProtected},
		{command: `GIT_LITERAL_PATHSPECS=1 git ls-files -- . ':^.sdlc/state' | ruby -e 'puts STDIN.read'`, rule: refusedAsLoopState},
		{who: "frozen", command: `git --literal-pathspecs diff -- . ':(exclude)internal/x_test.go' | awk '{print}'`, rule: throughTheTool},
		{who: "frozen", command: `GIT_LITERAL_PATHSPECS=1 git diff -- . ':!internal/x_test.go' | sed -n 1,3p`, rule: throughTheTool},
		{ps: true, command: `sdlc escalate spec_unclear --messag 'see -> .sdlc/config.json'`, rule: refusedAsLoopState},
		{ps: true, command: `./sdlc escalate spec_unclear --message 'see -> .sdlc/config.json'`, rule: refusedAsLoopState},
		{ps: true, command: `sdlc escalate spec_unclear -- --message 'see -> .sdlc/config.json'`, rule: refusedAsLoopState},
		{ps: true, command: `git --literal-pathspecs diff -- . ':(exclude).sdlc' | perl -ne 'print'`, rule: refusedAsLoopState},
	}, func(t *testing.T, r corpusRow) {
		t.Helper()
		if f := r.stillRefused(t); !saysItCouldNotTell(f) {
			t.Errorf("%s was refused without being told the classifier could not tell:\n%s", r.command, f.Reason)
		}
	})
}

// A refusal that is not about a path the command may only be naming keeps the
// words it was released with: these are refused for what they are, and saying
// the classifier could not tell would be untrue of them.
func TestAC6ARefusalThatIsNotAboutANamedPathDoesNotSayItCouldNotTell(t *testing.T) {
	corpusRun(t, "AC-6", []corpusRow{
		{command: `echo "a; rm CLAUDE.md"`, rule: refusedAsProtected},
		{command: `grep -n "x; rm .sdlc/state/active" docs/enforcement.md`, rule: refusedAsLoopState},
		{command: `rm .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x" > .sdlc/config.json`, rule: refusedAsLoopState},
		{command: `sdlc escalate spec_unclear --message "x"; rm .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `git checkout -- . ':(exclude).sdlc'`, rule: refusedAsLoopState},
		{command: `git diff --numstat -- . ':(exclude).sdlc' > .sdlc/state/active`, rule: refusedAsLoopState},
		{command: `sdlc approve A-1`, rule: approvalRule},
		{command: `sdlc approve --help=false A-1`, rule: approvalRule},
		{who: "frozen", command: `rm internal/x_test.go`, rule: throughTheTool},
	}, func(t *testing.T, r corpusRow) {
		t.Helper()
		if f := r.stillRefused(t); saysItCouldNotTell(f) {
			t.Errorf("%s is refused for what it does, and the refusal says the classifier could not tell:\n%s", r.command, f.Reason)
		}
	})
}
