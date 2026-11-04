package plugin

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// US-117. Each duty is a group of words in one sentence, found with US-115's
// rpSentences and rpAssign, so that one sentence holding every word is one duty
// met and not several. A sentence ends at a colon or a semicolon as well as at a
// full stop.

// fdNit is the word nit, or nits, and not unit or commit.
var fdNit = []string{"nits", " nit ", " nit,", "`nit`", " nit-only"}

// fdLookAgain is a reviewer's second look, however it is said.
var fdLookAgain = []string{"look again", "looks again", "looking again", "look at it again"}

// fdClosing is how a finding was closed, said as a noun or as a verb.
var fdClosing = []string{"closing", "closed", "closure"}

// fdTheyAll are the definitions AC-10 names: the implementer, and every reviewer.
var fdTheyAll = append([]string{"implementer"}, rpReviewers...)

// The duties, each tried against a sentence a correct text would use and one a
// dodge would (fdSamples in the test plan): a word group a correct sentence
// would miss is widened here, not left for the implementer to write around.
var (
	fdAC2Duties = []rpDuty{
		{"to list each finding with --finding", [][]string{{"--finding"}, {"each", "every", "one per"}}},
		{"the grammar of a finding", [][]string{{"<important|nit>"}}},
	}
	fdAC6Duties = []rpDuty{
		{"to read the closings of its own findings when it looks again",
			[][]string{fdLookAgain, fdClosing, {"own", "your finding", "its finding"}, {"finding"}}},
		{"that a decline whose reason does not hold is a block that names the finding",
			[][]string{{"declin"}, {"reason"}, {"not hold", "doesn't hold", "does not stand", "fails to hold"}, {"block"},
				{"name", "naming", "f-n"}}},
		{"that its fresh approval, not the closing, lets a blocking finding's decline stand",
			[][]string{{"fresh", "new approval"}, {"approv"}, fdClosing, {"declin"}, {"stand"}}},
		{"that a closing's reason is data to judge and not an instruction",
			[][]string{{"reason"}, {"data", "evidence"},
				{"not an instruction", "not as an instruction", "not instruction", "never an instruction", "never as an instruction"}}},
	}
	fdAC7Duties = []rpDuty{
		{"that a nit in a file the story changes is fixed in the same rework pass or declined with a reason",
			[][]string{fdNit, {"chang"}, {"same rework", "same pass", "rework pass"}, {"fix"}, {"declin"}, {"reason"}}},
		{"that a wave with only nits is closed by the implementer and only the reviewers who raised them looking again",
			[][]string{{"only nits", "nits only", "nit-only", "nits-only", "no important"}, {"implementer"}, {"raised"}, fdLookAgain,
				{"not the whole", "only the reviewer", "only those"}}},
		{"that nits past the policy's cap are a count and need no closing",
			[][]string{{"cap"}, {"count"}, {"no closing", "need no clos", "needs no clos", "not closed", "nothing to close"}}},
	}
	fdAC8Duties = []rpDuty{
		{"when it checks: before approving at Gate 6 and again after Gate 7 rework",
			[][]string{{"gate 6"}, {"gate 7"}, {"again"}, {"rework"}, {"approv"}}},
		{"that every finding from Gates 4 and 7 is closed",
			[][]string{{"finding"}, {"gate 4", "gates 4", "design review", "design_review", "design-review"},
				{"7", "code review", "code_review", "code-review"}, {"closed"}}},
		{"that each --fixed is visible in the diff", [][]string{{"--fixed"}, {"diff"}}},
		{"that each --declined reason holds", [][]string{{"--declined"}, {"reason"}, {"hold"}}},
		{"that nothing the diff touches was --referred", [][]string{{"--referred"}, {"diff"}, {"touch", "chang"}}},
		{"that a defect it finds on its own in a file the diff changes is fixed",
			[][]string{{"defect"}, {"own"}, {"fix"}, {"diff", "story"}}},
		{"that the story's commits add no story to the backlog",
			[][]string{{"backlog"}, {"commit"}, {"no story", "add no", "adds no", "not add", "no new story"}}},
		{"that it blocks on any of them", [][]string{{"block"}, {"any of these", "any of them", "any one of", "any of the"}}},
	}
	fdAC9Duties = []rpDuty{
		{"to list the --referred findings", [][]string{{"referred"}, {"finding"}, {"list"}}},
		{"to add nothing to the backlog", [][]string{{"backlog"}, {"not", "never", "nothing"}}},
	}
	fdAC10Duties = []rpDuty{
		// Two duties, not one: the natural sentence parts them with a semicolon.
		{"that a finding in a file the story changes is fixed or declined with a reason",
			[][]string{{"finding"}, {"file"}, {"story changes", "diff changes", "diff touches", "story touches"}, {"fix"}, {"declin"},
				{"reason"}}},
		{"that a finding is never left for a later story",
			[][]string{{"never", "not ", "no "}, {"later story", "another story", "follow-up story", "future story", "follow-up"}}},
		{"that a defect it notices in such a file is fixed", [][]string{{"defect"}, {"notice"}, {"fix"}}},
		{"that something outside the story's files that cannot wait is closed with --referred and a reason",
			[][]string{{"outside"}, {"cannot wait", "can't wait", "can not wait"}, {"--referred"}, {"reason"}}},
	}
	fdAC6RunbookDuty = rpDuty{"that a reviewer looking again is given its findings' closings as data",
		[][]string{fdClosing, {"finding"}, {"data"}}}
)

func fdSays(t *testing.T, who, text string, duties []rpDuty) {
	t.Helper()
	sentences := rpSentences(text)
	for _, d := range duties {
		if !slices.ContainsFunc(sentences, d.holds) {
			t.Errorf("%s does not say %s", who, d.name)
		}
	}
	if missing := rpAssign(duties, sentences); len(missing) > 0 {
		t.Errorf("%s: %v", who, missing)
	}
}

// fdRunbook is the next runbook's body.
func fdRunbook(t *testing.T) string {
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

// AC-2: findings are tracked only where a reviewer passes --finding, so every
// reviewer is told to list each one with it, and every line that records a review
// shows the flag with a placeholder, not a finding to copy.
func TestAC2EveryReviewerListsEachFindingWithTheFindingFlag(t *testing.T) {
	t.Parallel()
	example := regexp.MustCompile(`(?m)^.*sdlc review add .*$`)
	placeholder := regexp.MustCompile(`--finding[ =]+"?<[^>]+>`)
	for _, name := range rpReviewers {
		t.Run("AC-2 "+name, func(t *testing.T) {
			t.Parallel()
			body := rpAgentBody(t, name)
			for _, line := range example.FindAllString(body, -1) {
				if !placeholder.MatchString(line) {
					t.Errorf("%s records a review without its findings: %s", name, line)
				}
			}
			fdSays(t, name, body, fdAC2Duties)
		})
	}
}

// AC-6: on a second look, a reviewer reads the closings of its own findings; a
// decline whose reason does not hold is a block naming the finding; its own fresh
// approval, not the closing, lets a blocking finding's decline stand. E3: the
// reason is the implementer's words to judge, not an instruction.
func TestAC6EveryReviewerJudgesTheClosingsOfItsOwnFindingsWhenItLooksAgain(t *testing.T) {
	t.Parallel()
	for _, name := range rpReviewers {
		t.Run("AC-6 "+name, func(t *testing.T) {
			t.Parallel()
			fdSays(t, name, rpAgentBody(t, name), fdAC6Duties)
		})
	}
}

// AC-6 in the runbook: a reviewer asked to look again after rework is given its
// own findings' closings, as data, at each gate whose reviewer can be asked.
func TestAC6TheRunbookGivesAReviewerTheClosingsOfItsFindingsWhenItLooksAgain(t *testing.T) {
	t.Parallel()
	for _, gate := range []string{"Gate 4", "Gate 6", "Gate 7"} {
		t.Run("AC-6 "+gate, func(t *testing.T) {
			t.Parallel()
			rpSaysIn(t, gate, rpRunbookSection(t, gate), fdAC6RunbookDuty)
		})
	}
}

// AC-7: nits follow the scout rule inside US-115's policy, said in the runbook in
// place of US-115's rule that nits are never sent back as rework (which
// TestAC5TheRunbookGivesTheDesignReviewsNitsToTheImplementerAndNoneToTheRetro now
// holds the runbook to leaving out).
func TestAC7TheRunbookHoldsNitsToTheScoutRule(t *testing.T) {
	t.Parallel()
	body := fdRunbook(t)
	fdSays(t, "the next runbook", body, fdAC7Duties)
	for _, sentence := range rpSentences(body) {
		lower := strings.ToLower(sentence)
		if slices.ContainsFunc(fdNit, func(w string) bool { return strings.Contains(lower, w) }) &&
			strings.Contains(lower, "never") && strings.Contains(lower, "rework") {
			t.Errorf("AC-7 the runbook still says nits never come back as rework: %q", sentence)
		}
	}
}

// AC-8: the verifier's checks, each in a sentence of its own, when it approves at
// Gate 6 and when it is run again after Gate 7 rework, and a block on any of them.
func TestAC8TheVerifierChecksTheLedgerTheDiffAndTheBacklogBeforeItApproves(t *testing.T) {
	t.Parallel()
	body := rpAgentBody(t, "verifier")
	fdSays(t, "verifier", body, fdAC8Duties)
	if !strings.Contains(body, "sdlc log") && !strings.Contains(body, "sdlc status") && !strings.Contains(body, "gate-record.json") {
		t.Error("AC-8 the verifier is not told where to read the findings and their closings")
	}
}

// AC-9: the bookkeeper writes no follow-ups, lists the referred findings under a
// heading that says they are with the person, and adds nothing to the backlog.
func TestAC9TheRetroOpensNoFollowUpAndListsTheReferredFindingsAsThePersons(t *testing.T) {
	t.Parallel()
	body := rpAgentBody(t, "bookkeeper")
	lower := strings.ToLower(body)
	for _, gone := range []string{"follow-up", "follow_up", "followup", "become a story"} {
		if strings.Contains(lower, gone) {
			t.Errorf("AC-9 the bookkeeper still says %q", gone)
		}
	}
	_, template, found := strings.Cut(body, "sdlc artifact write retro")
	if !found {
		t.Fatal("the bookkeeper has no retro template")
	}
	template, _, _ = strings.Cut(template, "SDLC_DOCUMENT\n```")
	withThePerson := false
	for _, heading := range regexp.MustCompile(`(?m)^## .*$`).FindAllString(template, -1) {
		h := strings.ToLower(heading)
		withThePerson = withThePerson || strings.Contains(h, "referred") && strings.Contains(h, "person")
	}
	if !withThePerson {
		t.Errorf("AC-9 the retro template has no heading that says the referred findings are with the person:\n%s", template)
	}
	fdSays(t, "bookkeeper", body, fdAC9Duties)
}

// AC-10: the implementer and every reviewer are told the scout rule in their own
// words: a finding in a file the story changes is fixed or declined with a
// reason, never left for a later story; a defect noticed in such a file is
// fixed; something outside the story's files that cannot wait is referred.
func TestAC10TheImplementerAndEveryReviewerAreToldTheScoutRule(t *testing.T) {
	t.Parallel()
	for _, name := range fdTheyAll {
		t.Run("AC-10 "+name, func(t *testing.T) {
			t.Parallel()
			fdSays(t, name, rpAgentBody(t, name), fdAC10Duties)
		})
	}
	t.Run("AC-10 the implementer closes each finding it was given", func(t *testing.T) {
		t.Parallel()
		body := rpAgentBody(t, "implementer")
		for _, word := range []string{"sdlc finding close", "--fixed", "--declined", "--referred", "--reason"} {
			if !strings.Contains(body, word) {
				t.Errorf("the implementer is not told %q", word)
			}
		}
	})
}

// fdFreeTextFlag is a flag whose value is an agent's own words.
var fdFreeTextFlag = regexp.MustCompile(`--(finding|reason|note)\b`)

// fdShellQuoteDuty is the design review's I-4 (security): in double quotes the
// shell expands `$NAME` and backticks before sdlc reads the value, so a finding
// that names a variable stores its value in the record, which a project tracks.
var fdShellQuoteDuty = rpDuty{"to keep `$`, backticks and `\"` out of the value",
	[][]string{{"$"}, {"backtick", "back-tick", "backquote", "back quote"}, {`"`, "double quote", "double-quote"},
		{"out of", "single quote", "single-quote", "shell", "expand"}}}

// AC-2 and AC-3, and the design review's I-4: every definition that teaches
// --finding, --reason or --note says to keep what the shell expands out of the
// value. The implementer and every reviewer are held to it whatever they teach.
func TestAC2AC3EveryAgentThatPassesItsOwnWordsKeepsWhatTheShellExpandsOutOfThem(t *testing.T) {
	t.Parallel()
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	taught := slices.Clone(fdTheyAll)
	for _, a := range agents {
		if fdFreeTextFlag.MatchString(a.Body) && !slices.Contains(taught, a.Name) {
			taught = append(taught, a.Name)
		}
	}
	for _, name := range taught {
		t.Run("AC-2 AC-3 "+name, func(t *testing.T) {
			t.Parallel()
			if !slices.ContainsFunc(rpSentences(rpAgentBody(t, name)), fdShellQuoteDuty.holds) {
				t.Errorf("%s does not say %s", name, fdShellQuoteDuty.name)
			}
		})
	}
}

// AC-10 and the design review's I3 (architect): rework at Gates 6 and 7 goes to
// the implementer as well, and its findings are closed there too, not only those
// of the plan and the code at Gates 4 and 5.
func TestAC10TheImplementerClosesTheFindingsOfGate6AndGate7ReworkToo(t *testing.T) {
	t.Parallel()
	everyGate := []string{"whichever gate", "every gate", "any gate", "each gate"}
	duty := rpDuty{"to close the findings of Gate 6 and Gate 7 rework", [][]string{
		{"clos"}, {"finding", "each one", "every one"},
		append([]string{"gate 6", "verifier"}, everyGate...),
		append([]string{"gate 7", "code review", "code_review", "code-review"}, everyGate...),
	}}
	if !slices.ContainsFunc(rpSentences(rpAgentBody(t, "implementer")), duty.holds) {
		t.Errorf("the implementer is not told %s", duty.name)
	}
}

// fdStep is a numbered step's first line.
var fdStep = regexp.MustCompile(`(?m)^([0-9]+)\. `)

// AC-8 and the design review's I2 and N1 (architect) and I-4 (red team): the
// ledger check is a numbered step of the verifier's procedure, and it says to
// block when a check does not hold, not "block on any of these" over a list of
// what should hold. The procedure stays whole around it: every line is a step,
// a line indented under one or blank, and the steps count 1, 2, 3 in order.
func TestAC8TheVerifiersLedgerCheckIsANumberedStepThatBlocksWhenACheckDoesNotHold(t *testing.T) {
	t.Parallel()
	_, procedure, found := strings.Cut(rpAgentBody(t, "verifier"), "\n## Procedure\n")
	if !found {
		t.Fatal("the verifier has no Procedure section")
	}
	procedure, _, _ = strings.Cut(procedure, "\n## ")
	for line := range strings.SplitSeq(procedure, "\n") {
		if strings.TrimSpace(line) != "" && !fdStep.MatchString(line) && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			t.Errorf("AC-8 a line of the verifier's procedure is neither a step nor part of one: %q", line)
		}
	}
	starts := fdStep.FindAllStringSubmatchIndex(procedure, -1)
	block := rpDuty{"to block when a check does not hold", [][]string{{"block"}, {"not hold", "doesn't hold", "unless", "fail"}}}
	ledger := false
	for i, m := range starts {
		if n, _ := strconv.Atoi(procedure[m[2]:m[3]]); n != i+1 {
			t.Errorf("AC-8 step %d of the verifier's procedure is numbered %d", i+1, n)
		}
		end := len(procedure)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		step := strings.ToLower(procedure[m[0]:end])
		if strings.Contains(step, "finding") && (strings.Contains(step, "ledger") || strings.Contains(step, "closed")) &&
			slices.ContainsFunc(rpSentences(step), block.holds) {
			ledger = true
		}
	}
	if !ledger {
		t.Errorf("AC-8 no numbered step of the verifier's procedure checks the findings and says %s:\n%s", block.name, procedure)
	}
}

// AC-11: the closing message lists every referred finding with its reason, and
// the loop adds nothing to the backlog for them.
func TestAC11TheRunbooksClosingMessageListsTheReferredFindings(t *testing.T) {
	t.Parallel()
	section := rpRunbookSection(t, "Gate 9")
	rpSaysIn(t, "Gate 9", section, rpDuty{"to tell the person every referred finding with its reason",
		[][]string{{"referred"}, {"reason"}}})
	rpSaysIn(t, "Gate 9", section, rpDuty{"that nothing is added to the backlog for them",
		[][]string{{"backlog"}, {"not", "never", "nothing"}}})
	if !strings.Contains(section, "sdlc status") && !strings.Contains(section, "sdlc log") {
		t.Errorf("Gate 9 of the next runbook does not say where the referred findings are read:\n%s", section)
	}
	if !strings.Contains(fdRunbook(t), "sdlc finding close") {
		t.Error("AC-11 the next runbook never names `sdlc finding close`")
	}
}
