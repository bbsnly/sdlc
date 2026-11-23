package plugin

// US-134: what the runbook and the agents are told about the configured
// commands, now that the binary runs them. The runbook no longer has the
// session run the project's test command (AC-15) and sends a refused gate run to
// the implementer (AC-2); the verifier cites the binary's results and keeps its
// own checks (AC-4, AC-5); after a rework the gate is recorded again before the
// same verifier, resumed, looks again (AC-12); the SDET and the implementer run
// the narrow form (AC-14); and docs/the-loop.md holds the one table of who runs
// which command at which gate, against which every instruction to run
// commands.test is held (AC-16).
//
// Text is read with its lines joined, so that where a line wraps decides
// nothing, and with fenced code left out: an example is not an instruction.

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// us134Prose is body without its fenced blocks, white space collapsed.
func us134Prose(body string) string {
	var kept []string
	fenced := false
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			kept = append(kept, line)
		}
	}
	return strings.Join(strings.Fields(strings.Join(kept, "\n")), " ")
}

// us134Runbook is the next skill's body.
func us134Runbook(t *testing.T) string {
	t.Helper()
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name == "next" {
			return s.Body
		}
	}
	t.Fatal("there is no next skill")
	return ""
}

// us134RunbookSection is the runbook's section whose heading starts with
// heading, as prose.
func us134RunbookSection(t *testing.T, heading string) string {
	t.Helper()
	for _, section := range regexp.MustCompile(`(?m)^## `).Split(us134Runbook(t), -1) {
		if strings.HasPrefix(section, heading) {
			return us134Prose(section)
		}
	}
	t.Fatalf("the next runbook has no section %q", heading)
	return ""
}

// us134Agent is an agent definition's body, by its file's name.
func us134Agent(t *testing.T, name string) string {
	t.Helper()
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name == name {
			return a.Body
		}
	}
	t.Fatalf("there is no %s agent", name)
	return ""
}

// us134AgentSection is the part of an agent's body under the heading that
// starts with heading, to the next heading of its level, as prose.
func us134AgentSection(t *testing.T, name, heading string) string {
	t.Helper()
	return us134Prose(us114Section(t, us134Agent(t, name), heading))
}

func us134Says(t *testing.T, where, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s does not say %q", where, w)
		}
	}
}

// us134SaysInASentence holds that one sentence of text matches every pattern:
// the meaning the criterion states, in whatever words the definition puts it.
func us134SaysInASentence(t *testing.T, where, what, text string, all ...*regexp.Regexp) {
	t.Helper()
	for _, s := range us134Sentence.FindAllString(text, -1) {
		if !slices.ContainsFunc(all, func(re *regexp.Regexp) bool { return !re.MatchString(s) }) {
			return
		}
	}
	t.Errorf("%s has no sentence saying %s:\n%s", where, what, text)
}

var (
	us134Output  = regexp.MustCompile(`(?i)\boutput\b`)
	us134File    = regexp.MustCompile(`(?i)\bfile\b`)
	us134Refused = regexp.MustCompile(`SDLC-E0095|(?i)\brefus`)
	us134Result  = regexp.MustCompile(`(?i)\bresults?\b`)
)

// ---------------------------------------------------------------- the detector

var (
	// us134Sentence ends at a stop followed by a space or the end, so that
	// `commands.test` and `.sdlc/config.json` are not cut in two.
	us134Sentence = regexp.MustCompile(`(?:[^.!?;]|[.!?;][^\s])+[.!?;]?`)
	// us134TheTest is commands.test itself, not commands.test_files, or the
	// project's test command by name.
	us134TheTest = regexp.MustCompile("`commands\\.test`|commands\\.test([^_a-z]|$)|\\b(project's|the) test command\\b")
	us134Run     = regexp.MustCompile(`(?i)\b(run|runs|running|rerun|re-run)\b`)
	// us134Not is a sentence that says what is not to be done.
	us134Not = regexp.MustCompile(`(?i)\b(not|never|no|nor|without|nobody|nothing|none|instead of)\b|n't\b`)
)

// us134Instructions is every sentence of body that tells its reader to run the
// whole of commands.test.
func us134Instructions(body string) []string {
	var out []string
	for _, s := range us134Sentence.FindAllString(us134Prose(body), -1) {
		if us134TheTest.MatchString(s) && us134Run.MatchString(s) && !us134Not.MatchString(s) {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// The detector is held to the lines the story deletes, and to the sentence that
// replaces one of them, so that a detector that finds nothing does not pass the
// check below on its own.
func TestAC16TheCheckFindsAnInstructionToRunTheTestCommandAndNotOneThatForbidsIt(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"Otherwise run `commands.test` and read the output yourself.",
		"When it reports that the frozen tests pass, run the project's test command and read the output.",
		"One plan step at a time. After each, run `commands.test`.",
		"4. Run them with `commands.test` from `.sdlc/config.json`.",
	} {
		if len(us134Instructions(s)) == 0 {
			t.Errorf("AC-16 the check does not find the instruction in %q", s)
		}
	}
	for _, s := range []string{
		"Run the tests you wrote with `commands.test_files`; the whole `commands.test` is not yours to run.",
		"Do not run `commands.test` yourself: the gate runs it.",
	} {
		if got := us134Instructions(s); len(got) != 0 {
			t.Errorf("AC-16 the check takes %q for an instruction to run commands.test", got)
		}
	}
}

// ---------------------------------------------------------------- the table

// us134Row is one row of the-loop.md's table of who runs which command.
type us134Row struct{ gate, command, who string }

// us134Table is the one table in docs/the-loop.md whose header names the gate,
// the command and who runs it, by those columns.
func us134Table(t *testing.T) []us134Row {
	t.Helper()
	page := us114Page(t, "docs/the-loop.md")
	var tables [][]us134Row
	var rows []us134Row
	gateAt, commandAt, whoAt := -1, -1, -1
	inTable := false
	for line := range strings.SplitSeq(strings.ReplaceAll(page, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			if inTable && gateAt >= 0 && commandAt >= 0 && whoAt >= 0 {
				tables = append(tables, rows)
			}
			inTable, rows, gateAt, commandAt, whoAt = false, nil, -1, -1, -1
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if !inTable {
			inTable = true
			for i, c := range cells {
				switch lower := strings.ToLower(c); {
				case strings.Contains(lower, "gate") && gateAt < 0:
					gateAt = i
				case strings.Contains(lower, "command") && commandAt < 0:
					commandAt = i
				case strings.Contains(lower, "who") && whoAt < 0:
					whoAt = i
				}
			}
			continue
		}
		if strings.Trim(strings.Join(cells, ""), "-: ") == "" || max(gateAt, commandAt, whoAt) >= len(cells) {
			continue
		}
		if gateAt >= 0 && commandAt >= 0 && whoAt >= 0 {
			rows = append(rows, us134Row{gate: cells[gateAt], command: cells[commandAt], who: strings.ToLower(cells[whoAt])})
		}
	}
	if inTable && gateAt >= 0 && commandAt >= 0 && whoAt >= 0 {
		tables = append(tables, rows)
	}
	if len(tables) != 1 {
		t.Fatalf("AC-16 docs/the-loop.md has %d tables with a gate, a command and a who column, want one", len(tables))
	}
	return tables[0]
}

// us134TheWholeTest is a command cell that names commands.test, not the narrow
// form alone.
var us134TheWholeTest = regexp.MustCompile(`commands\.test([^_a-z]|$)`)

// AC-16: the table says the binary runs commands.test at Gate 5 and the narrow
// form at the freeze, and who runs the narrow form after; and every instruction
// to run the whole commands.test in the runbook or an agent's definition is one
// of its rows. The runbook's reader is the session, which the table names so.
func TestAC16EveryInstructionToRunTheTestCommandIsARowOfTheLoopsTable(t *testing.T) {
	t.Parallel()
	rows := us134Table(t)
	has := func(command *regexp.Regexp, who, gate string) bool {
		return slices.ContainsFunc(rows, func(r us134Row) bool {
			return command.MatchString(r.command) && strings.Contains(r.who, who) && strings.Contains(r.gate, gate)
		})
	}
	narrow := regexp.MustCompile(`commands\.test_files`)
	for _, want := range []struct {
		command   *regexp.Regexp
		who, gate string
		what      string
	}{
		{us134TheWholeTest, "sdlc", "5", "the binary runs commands.test at Gate 5"},
		{narrow, "sdlc", "3", "the binary runs commands.test_files at the freeze"},
		{narrow, "sdet", "3", "the SDET runs commands.test_files at Gate 3"},
		{narrow, "implementer", "5", "the implementer runs commands.test_files at Gate 5"},
	} {
		if !has(want.command, want.who, want.gate) {
			t.Errorf("AC-16 the table in docs/the-loop.md has no row saying %s: %+v", want.what, rows)
		}
	}

	readers := map[string]string{"session": us134Runbook(t)}
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		readers[a.Name] = a.Body
	}
	for who, body := range readers {
		for _, s := range us134Instructions(body) {
			if !slices.ContainsFunc(rows, func(r us134Row) bool {
				return us134TheWholeTest.MatchString(r.command) && strings.Contains(r.who, who)
			}) {
				t.Errorf("AC-16 the %s is told to run commands.test, and the table in docs/the-loop.md has no row for it: %q", who, s)
			}
		}
	}
}

// ---------------------------------------------------------------- AC-15, AC-2

// AC-15: the runbook does not have the session run the test command at Gate 3
// or Gate 5, and reads test output from the binary's file only when a run
// fails. AC-2: a refused gate run goes to the implementer as rework.
func TestAC15AndAC2TheRunbookRunsNoTestCommandAndSendsARefusedRunToTheImplementer(t *testing.T) {
	t.Parallel()
	for _, heading := range []string{"Gate 3", "Gate 5"} {
		if got := us134Instructions(us134RunbookSection(t, heading)); len(got) != 0 {
			t.Errorf("AC-15 the runbook's %s still tells the session to run the test command: %q", heading, got)
		}
	}
	gate5 := us134RunbookSection(t, "Gate 5")
	us134Says(t, "AC-2 and AC-15 the runbook's Gate 5", gate5, "SDLC-E0095", "sdlc:implementer")
	us134SaysInASentence(t, "AC-15 the runbook's Gate 5", "that a refused run's output is read from its file", gate5,
		us134Output, us134File)
	if strings.Contains(gate5, "frozen tests green") {
		t.Errorf("AC-15 the runbook's Gate 5 still has the session say the tests are green, which it no longer reads:\n%s", gate5)
	}
}

// ---------------------------------------------------------------- AC-4, AC-5

// AC-4: the runbook gives the verifier the results the binary holds for the
// worktree's tree, and the verifier cites them in VERIFICATION.md's Commands
// table marked run by sdlc, running a command itself only where no result exists
// for the tree.
func TestAC4TheVerifierIsGivenTheBinarysResultsAndCitesThemRunBySdlc(t *testing.T) {
	t.Parallel()
	gate6 := us134RunbookSection(t, "Gate 6")
	us134Says(t, "AC-4 the runbook's Gate 6", gate6, "sdlc status", "run by sdlc")
	// The Outputs section's example document has headings of its own, inside a
	// fence, so the section is read to the heading after it.
	if !regexp.MustCompile(`(?s)\n## Outputs\n.*\n## Commands\n[^#]*run by sdlc.*\n## Never\n`).MatchString(us134Agent(t, "verifier")) {
		t.Error("AC-4 the verifier's VERIFICATION.md has no row of its Commands table marked `run by sdlc`")
	}
	procedure := us134AgentSection(t, "verifier", "## Procedure")
	us134Says(t, "AC-4 the verifier's Procedure", procedure, "run by sdlc")
	us134SaysInASentence(t, "AC-4 the verifier's Procedure", "that it runs a command itself only where sdlc holds no result for the tree",
		procedure, us134Result, us134Run, us134Not)
}

// AC-5: the verifier keeps its own end-to-end check of one criterion, its
// re-derivation of the criteria and its gaming check; only the configured
// commands are not repeated, so no step has it run commands.test where the
// binary holds a result.
func TestAC5TheVerifierKeepsItsOwnChecksAndRepeatsOnlyNoneOfTheConfiguredCommands(t *testing.T) {
	t.Parallel()
	procedure := us134AgentSection(t, "verifier", "## Procedure")
	us134Says(t, "AC-5 the verifier's Procedure", procedure,
		"Re-derive", "Hunt for gaming", "verify it end to end yourself")
	if got := us134Instructions(us134Agent(t, "verifier")); len(got) != 0 {
		t.Errorf("AC-5 the verifier is told to run commands.test whatever the binary holds: %q", got)
	}
}

// ---------------------------------------------------------------- AC-12

// AC-12: after a rework that changed the tree, the runbook records
// `sdlc gate implementation pass` again, which has the binary run the commands on
// the new tree, before anyone is asked to look again; and the verifier asked is
// the same one, resumed, given the change and how each of its findings was
// closed.
func TestAC12AfterAReworkTheGateIsRecordedAgainAndTheSameVerifierIsResumed(t *testing.T) {
	t.Parallel()
	for _, heading := range []string{"Gate 6", "Gate 7"} {
		section := us134RunbookSection(t, heading)
		again := strings.Index(section, "sdlc gate implementation pass")
		look := strings.Index(section, "look again")
		if again < 0 || look < 0 || again > look {
			t.Errorf("AC-12 the runbook's %s does not record `sdlc gate implementation pass` before anyone looks again:\n%s", heading, section)
		}
	}
	us134Says(t, "AC-12 the runbook's Gate 6", us134RunbookSection(t, "Gate 6"), "SendMessage", "closings", "sdlc status")
}

// ---------------------------------------------------------------- AC-14

// AC-14: the SDET runs the tests it wrote with commands.test_files to see each
// fail, and the whole commands.test is not its to run. The implementer reads
// the freeze's output file and TEST-PLAN.md at the plan, runs the frozen tests
// and the tests of each step with the narrow form, and reads the gate's output
// file when the gate is refused: the whole commands.test is the gate's run.
func TestAC14TheSDETAndTheImplementerRunTheNarrowFormAndLeaveTheWholeSuiteToTheGate(t *testing.T) {
	t.Parallel()
	sdet := us134Prose(us134Agent(t, "sdet"))
	us134Says(t, "AC-14 the SDET's definition", sdet, "commands.test_files")
	us134SaysInASentence(t, "AC-14 the SDET's definition", "that the whole commands.test is not its to run", sdet,
		us134TheTest, us134Run, us134Not)
	if got := us134Instructions(us134Agent(t, "sdet")); len(got) != 0 {
		t.Errorf("AC-14 the SDET is told to run the whole commands.test: %q", got)
	}

	gate4 := us134AgentSection(t, "implementer", "## Gate 4")
	us134Says(t, "AC-14 the implementer's Gate 4", gate4, "TEST-PLAN.md", "sdlc freeze")
	us134SaysInASentence(t, "AC-14 the implementer's Gate 4", "that it reads the freeze's output file", gate4,
		us134Output, us134File)
	gate5 := us134AgentSection(t, "implementer", "## Gate 5")
	us134Says(t, "AC-14 the implementer's Gate 5", gate5, "commands.test_files")
	us134SaysInASentence(t, "AC-14 the implementer's Gate 5", "that it reads the gate's output file when the gate is refused", gate5,
		us134Output, us134File, us134Refused)
	if got := us134Instructions(us134Agent(t, "implementer")); len(got) != 0 {
		t.Errorf("AC-14 the implementer is told to run the whole commands.test: %q", got)
	}
}

// ---------------------------------------------------------------- security review F-2 and F-4

var (
	// us134Yourself is the verifier doing it in its own shell, not reading it.
	us134Yourself = regexp.MustCompile(`(?i)\b(yourself|itself|your own)\b`)
	us134Current  = regexp.MustCompile(`(?i)\bcurrent\b`)
	us134Brief    = regexp.MustCompile(`(?i)\bbrief\b`)
	us134Cite     = regexp.MustCompile(`(?i)\b(cite|cites|citing|results?)\b`)
	// us134NonZero is an exit status other than 0, however it is put.
	us134NonZero = regexp.MustCompile(`(?i)non-?zero|other than (0|zero)\b|not (0|zero)\b`)
	us134Why     = regexp.MustCompile(`(?i)\b(why|reason)\b`)
	// us134NoTests is a runner that found or collected no test to run.
	us134NoTests = regexp.MustCompile(`(?i)\b(found|finds|collected|collects|ran|runs) no tests?\b|\bno tests? (were |was )?(found|collected)\b`)
	// us134WrongNarrow is a commands.test_files that is wrong in itself.
	us134WrongNarrow = regexp.MustCompile("(?i)\\b(wrong|mistaken|broken|misconfigured|faulty)\\b[^.;]*commands\\.test_files|commands\\.test_files`?[^.;]*\\b(wrong|mistaken|broken|misconfigured|faulty)\\b")
	us134Reviewer    = regexp.MustCompile(`(?i)\breviewer\b`)
	us134Right       = regexp.MustCompile(`(?i)\bright reason\b|\bthe reason (its|the) criterion\b`)
)

// AC-4, security review F-2: the brief is the main conversation's, so a result
// it copies is not evidence. The verifier asks `sdlc status --json` in the
// worktree itself, cites as run by sdlc only what that calls current, and is
// told that the brief's copy does not count.
func TestAC4TheVerifierAsksSdlcStatusItselfAndCitesOnlyWhatItCallsCurrent(t *testing.T) {
	t.Parallel()
	procedure := us134AgentSection(t, "verifier", "## Procedure")
	us134SaysInASentence(t, "AC-4 the verifier's Procedure", "that it runs `sdlc status --json` in the worktree itself",
		procedure, regexp.MustCompile(regexp.QuoteMeta("sdlc status --json")), us134Yourself)
	us134SaysInASentence(t, "AC-4 the verifier's Procedure", "that it cites as run by sdlc only a result sdlc calls current",
		procedure, regexp.MustCompile(`run by sdlc`), us134Current)
	us134SaysInASentence(t, "AC-4 the verifier's Procedure", "that a result its brief gives is not evidence",
		procedure, us134Brief, us134Not)
	for _, s := range us134Sentence.FindAllString(procedure, -1) {
		if us134Brief.MatchString(s) && us134Cite.MatchString(s) && !us134Not.MatchString(s) {
			t.Errorf("AC-4 the verifier is told to take results from its brief: %q", strings.TrimSpace(s))
		}
	}
}

// AC-13 and AC-14, security review F-4: a freeze taken proves that the narrow
// command exited non-zero, not why. A runner that found no tests and a wrong
// commands.test_files exit non-zero too, so the SDET's own run, and the test
// reviewer, are what show each test failing for the right reason.
func TestAC13AndAC14TheSDETIsToldWhatATakenFreezeProvesAndWhatItDoesNot(t *testing.T) {
	t.Parallel()
	sdet := us134Prose(us134Agent(t, "sdet"))
	us134SaysInASentence(t, "AC-13 the SDET's definition", "that a taken freeze proves a non-zero exit and not why",
		sdet, us134NonZero, us134Not, us134Why)
	us134SaysInASentence(t, "AC-13 the SDET's definition", "that a runner that found no tests exits non-zero too",
		sdet, us134NoTests)
	us134SaysInASentence(t, "AC-13 the SDET's definition", "that a wrong commands.test_files exits non-zero too",
		sdet, us134WrongNarrow)
	us134SaysInASentence(t, "AC-14 the SDET's definition", "that its own run and the test reviewer show each test failing for the right reason",
		sdet, us134Reviewer, us134Right)
}
