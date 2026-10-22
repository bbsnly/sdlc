package sdlcerr

// US-108 AC-12: every fix and every troubleshooting section that names one of a
// person's commands names its slash command first and the terminal command
// second. A person in Claude Desktop has no terminal; a fix that offered only
// the terminal command told them nothing they could do.
//
// The wording rule is the story's test plan's: the first terminal-form mention
// of a person-only command is preceded by its slash command, in the same
// quoting, then ", or "; a lead-in to a list of answers reads "in Claude Code or
// in their own terminal:". `sdlc staffing assign` is /sdlc:assign, and
// `sdlc parallel` has no slash command and is left as it is.
//
// Where the terminal command names the story, the slash command names it too,
// with the same arguments: re-pinned at Gate 7, after the first pins gave a
// bare "/sdlc:approve" beside "sdlc approve <ID>". After an escalation no story
// is active, so the bare form a Desktop user types gave SDLC-E0011 at the one
// moment the person was needed.

import (
	"regexp"
	"strings"
	"testing"
)

// personsCommands is each person-only command in its terminal form, and the
// slash command that stands for it.
var personsCommands = []struct{ terminal, slash string }{
	{"sdlc staffing assign", "/sdlc:assign"},
	{"sdlc approve", "/sdlc:approve"},
	{"sdlc unfreeze", "/sdlc:unfreeze"},
	{"sdlc ack", "/sdlc:ack"},
	{"sdlc remap", "/sdlc:remap"},
	{"sdlc release", "/sdlc:release"},
}

func TestAC12EveryFixThatNamesAPersonsCommandNamesItsSlashCommandFirst(t *testing.T) {
	for _, c := range []struct {
		code Code
		want string
	}{
		{AwaitingPerson, `a person decides first, in Claude Code or in their own terminal: "/sdlc:approve <ID>", or "sdlc approve <ID>", or "sdlc approve <ID> --reject ..." to send the work back`},                                                                                                                                                                                                                                // SDLC-E0036
		{ApprovalRequired, `hand it to a person with "sdlc escalate pre_commit_approval --message ..." and stop; once they have run "/sdlc:approve <ID>", or "sdlc approve <ID>", commit the work they approved`},                                                                                                                                                                                                                   // SDLC-E0037
		{UnfrozenTests, `remove the test files the freeze does not hold, or have a person run "/sdlc:unfreeze", or "sdlc unfreeze --reason ..." in their own terminal and freeze again, so they are frozen with the rest; in a story's own worktree a test file that only trunk changed since the story was cut is not held against the story, and one of trunk's that the story changed or took away is put back as trunk has it`}, // SDLC-E0043
		{AlreadyFrozen, `if the tests really have to change, run "/sdlc:unfreeze", or "sdlc unfreeze --reason ..." in your own terminal first`},                                                                                                                                                                                                                                                                                     // SDLC-E0022
		{FreezeBroken, `restore the frozen tests, or have a person run "/sdlc:unfreeze", or "sdlc unfreeze --reason ..." in their own terminal and freeze again, so the change is on the record`},                                                                                                                                                                                                                                   // SDLC-E0025
		{TrunkMoved, `record the gate as failed, hand the story to a person with "sdlc escalate", and stop; a person keeps the commits or reverts them, and answers with "/sdlc:approve <ID>", or "sdlc approve <ID>", after which "sdlc start" picks the story up from where trunk is then`},                                                                                                                                       // SDLC-E0046
		{AcknowledgedUnknown, `run "/sdlc:ack", or "sdlc ack --through <commit>" in your own terminal to acknowledge a commit git has, or remove .sdlc/state/acknowledged to list every story; meanwhile "sdlc log --since <commit>" starts after a commit you name, and "sdlc log --all" lists every story`},                                                                                                                       // SDLC-E0047
		{StoryHeldElsewhere, "a person runs the command in the checkout that holds the story, named above, or moves the story here with `/sdlc:release <ID> --reason \"...\"`, or `sdlc release <ID> --reason \"...\"` in their own terminal, then `sdlc start <ID>` in this checkout; an assistant stops and tells the person which checkout holds the story, and runs neither \"sdlc start\" nor \"sdlc release\""},               // SDLC-E0050
		{HoldingCheckoutMissing, "if that checkout is gone for good, a person runs `/sdlc:release <ID> --reason \"...\"`, or `sdlc release <ID> --reason \"...\"` in their own terminal, after which `sdlc start <ID>` works in any checkout; until then no checkout can record anything for the story; an assistant stops and tells the person, and does not run it"},                                                              // SDLC-E0051
		{LandingCheckFailed, "read what the message above says the check or the configured commands found. Where a configured command failed, the story stays with its session: run the command in a checkout that has trunk checked out with nothing uncommitted, which the main checkout usually is, and where it passes there, record `sdlc gate implementation fail --story <ID> --note \"...\"` in .sdlc/worktrees/<ID>, rework the story through its gates and run \"sdlc stop\" there again; where it fails there too the failure is trunk's own, and \"sdlc stop\" lands the story once a person has put trunk right. Any other check is a person's to put right: they do what the message says and run \"sdlc stop\" in .sdlc/worktrees/<ID> again, or reopen the story: answer the escalation first with \"/sdlc:approve <ID>\", or \"sdlc approve <ID>\" or its --reject in .sdlc/worktrees/<ID>, then run `sdlc gate implementation fail --story <ID> --note \"...\"` there, then /sdlc:next; \"sdlc stop\" run by a person runs the story's code and the repository's git settings with that person's own privileges, so read the story's changes first; for those checks an assistant hands the story to a person with \"sdlc escalate\" and stops"}, // SDLC-E0055
		{ParallelHeldElsewhere, `the story is being worked in another checkout: finish it there, or a person runs "release" in that checkout, or "/sdlc:release <ID> --reason ...", or "sdlc release <ID> --reason ..." here; an assistant stops and tells the person`},                  // SDLC-E0059
		{ParallelAwaitingPerson, `the story is waiting for a person's answer, and a run again would act as that answer: run "/sdlc:approve <ID>", or "sdlc approve <ID>" (or its --reject) in .sdlc/worktrees/<ID>, then name the story again; an assistant stops and tells the person`}, // SDLC-E0064
		{StaffingAssigned, `a person chose this for the agent, so it stays as it is: leave it out of the change; only a person changes it, with "/sdlc:assign", or "sdlc staffing assign" in their own terminal`},                                                                        // SDLC-E0072
		// Re-pinned after the first pin offered /sdlc:assign and then said it is
		// run only outside Claude Code: the slash command is typed in Claude
		// Code, the terminal command outside it.
		{StaffingAssignInSession, `"/sdlc:assign", or "sdlc staffing assign", is a person's: a person types the first in Claude Code, or runs the second in their own terminal, outside Claude Code; an assistant stops here and says what it would have asked for`}, // SDLC-E0074
		{StoryConflictsWithTrunk, `a person, in their own terminal, in the story's worktree, .sdlc/worktrees/<ID> in the main checkout: "git rebase <trunk>"; they resolve the files it stops on, "git add" them and carry the rebase on as git says until it finishes; then "sdlc stop" there, which runs the configured commands on the result. Where an assistant handed the story over, "/sdlc:approve <ID>", or "sdlc approve <ID>", comes before that stop and after the resolving, so that the approval is of the work as it now is; in a tier that waits for a person the rebased work is approved again, and where no question is open that stop says so with SDLC-E0037. That rebase is git's own and not sdlc's: it runs the hooks, merge drivers, editor and filters the repository's configuration names, with that person's privileges, so read "git config --show-origin --list" and .gitattributes there first. An assistant does not resolve the conflict: it hands the story to a person with "sdlc escalate" and stops`}, // SDLC-E0075
		// Re-pinned for the same reason as E0074: the first pin set the whole
		// sentence, /sdlc:remap included, in the person's own terminal.
		{MapChangeNotOnTrunk, `the story has landed and stays landed; only its change to CODEMAP.md is waiting, kept in the repository's git directory. A person, in the main checkout, puts right what is named above and then uses "/sdlc:remap <ID>", or "sdlc remap <ID>", typing the first in Claude Code or running the second in their own terminal; either makes the change where it still applies, "/sdlc:remap <ID> --replace", or "sdlc remap <ID> --replace", puts the kept sections over what trunk's map holds under their headings, and "/sdlc:remap <ID> --drop", or "sdlc remap <ID> --drop", discards the kept change. An assistant says so and runs none of them`}, // SDLC-E0076
	} {
		t.Run("AC-12 "+c.code.id, func(t *testing.T) {
			if got := New(c.code, "", "").Fix; got != c.want {
				t.Errorf("the fix of %s is\n%q\nwant\n%q", c.code.id, got, c.want)
			}
		})
	}
}

// namesAStory matches an argument that names a story: the placeholder, or an
// id such as US-001.
var namesAStory = regexp.MustCompile(`<ID>|<story>|\b[A-Z][A-Z0-9]*-[0-9]+\b`)

// slashArgumentsDiffer is each place in text where a slash command and its
// terminal command are offered as a pair, "/sdlc:x ARGS", or "sdlc x ARGS", in
// the same quoting, and the slash command is not given the arguments the
// terminal command is. A slash command runs the terminal command with the
// arguments typed after it, so the two are one answer only when the arguments
// agree. A bare slash command stays beside arguments that name no story,
// "--reason ...": what it lacks there the command asks for.
func slashArgumentsDiffer(text string) []string {
	var differ []string
	for _, c := range personsCommands {
		for _, q := range []string{"`", `"`} {
			arg := "((?: [^" + q + "]*)?)"
			pair := regexp.MustCompile(regexp.QuoteMeta(q+c.slash) + arg + regexp.QuoteMeta(q+", or "+q+c.terminal) + arg + regexp.QuoteMeta(q))
			for _, m := range pair.FindAllStringSubmatch(text, -1) {
				slashArgs, terminalArgs := m[1], m[2]
				if slashArgs == terminalArgs || slashArgs == "" && !namesAStory.MatchString(terminalArgs) {
					continue
				}
				differ = append(differ, m[0])
			}
		}
	}
	return differ
}

// Every fix, pinned above or not, holds to the arguments: a fix that offers a
// bare slash command beside a terminal command naming the story is one the
// Desktop user cannot carry out where no story is active.
func TestAC12EveryFixGivesTheSlashCommandTheArgumentsItsTerminalCommandIsGiven(t *testing.T) {
	// The reading itself: the pair as the fixes are to have it, and as the
	// first pins had it.
	if got := slashArgumentsDiffer(`run "/sdlc:approve <ID>", or "sdlc approve <ID>", and "/sdlc:unfreeze", or "sdlc unfreeze --reason ..."`); len(got) != 0 {
		t.Fatalf("slashArgumentsDiffer reads agreeing pairs as differing: %q", got)
	}
	if got := slashArgumentsDiffer("run `/sdlc:release`, or `sdlc release <ID> --reason \"...\"`, or \"/sdlc:remap <ID>\", or \"sdlc remap <ID> --drop\""); len(got) != 2 {
		t.Fatalf("slashArgumentsDiffer does not read both differing pairs: %q", got)
	}
	for _, e := range catalogue {
		for _, pair := range slashArgumentsDiffer(e.fix) {
			t.Errorf("AC-12 %s: the slash command is not given the terminal command's arguments: %s", e.code.id, pair)
		}
	}
}

// fenced matches a fenced block, which quotes a terminal and is left as it is.
// It is replaced by a sentence break, so that the words around it are not read
// as one sentence.
var fenced = regexp.MustCompile("(?ms)^```.*?^```[^\n]*$")

// sentenceEnd is where one sentence of folded prose ends and the next begins.
var sentenceEnd = regexp.MustCompile("[.;] +[A-Z`*-]")

// instructionCue is how the words before a command end where the page tells the
// person to run it: "a person runs", "answer it with", "first, with", "as",
// or a colon before the command to run.
var instructionCue = regexp.MustCompile(`(?:\b(?:run|runs|Run|running)(?: plain)?|` +
	`\b(?:answers?|answered|answering)(?: it)? with|\bdiscard it with|, with|\bas|:) *$`)

// describing is a word that, straight after a command, makes the command the
// subject of a description ("`sdlc unfreeze` needs an iteration running").
var describing = map[string]bool{
	"is": true, "are": true, "was": true, "carries": true, "carry": true, "needs": true, "reopens": true,
	"answers": true, "refuses": true, "prints": true, "sends": true, "approves": true, "works": true,
}

func sentences(text string) []string {
	var out []string
	for {
		at := sentenceEnd.FindStringIndex(text)
		if at == nil {
			return append(out, text)
		}
		out = append(out, text[:at[1]-1])
		text = text[at[1]-1:]
	}
}

// instructed is where, in one sentence, the page first tells the person to run
// the command, with the quote around it; at is -1 where it does not, or where
// the sentence named the slash command before it.
func instructed(sentence string, c struct{ terminal, slash string }) (at int, quote string) {
	mention := regexp.MustCompile("([\"`]?)" + regexp.QuoteMeta(c.terminal) + `\b`)
	for _, m := range mention.FindAllStringSubmatchIndex(sentence, -1) {
		before := sentence[:m[0]]
		if strings.Contains(before, c.slash) {
			return -1, ""
		}
		q := sentence[m[2]:m[3]]
		rest := sentence[m[1]:]
		if q != "" {
			if i := strings.Index(rest, q); i >= 0 {
				rest = rest[i+1:]
			}
		}
		next := ""
		if words := strings.Fields(rest); len(words) > 0 {
			next = strings.Trim(words[0], ".,;:")
		}
		trimmed := strings.TrimRight(before, " ")
		if !instructionCue.MatchString(before) || strings.HasSuffix(trimmed, "not run") ||
			strings.HasSuffix(trimmed, "**") || describing[next] {
			continue
		}
		return m[0], q
	}
	return -1, ""
}

// The troubleshooting page says the same as the fixes, in more words, and is
// held where it tells the person what to do: where a sentence of a code's
// section tells them to run one of their commands, the slash command comes just
// before it, quoted the same way, then ", or ". A sentence that names a command
// as its subject ("`sdlc approve` with no story named carries this code"), a
// list heading, an assistant told not to run it, and a quoted terminal session
// are description and are left as they are. Where the two are offered as a
// pair, the slash command is given the terminal command's arguments, as in the
// fixes: `/sdlc:approve <ID>`, or `sdlc approve <ID>`.
func TestAC12EveryTroubleshootingSectionNamesAPersonsSlashCommandBeforeItsTerminalCommand(t *testing.T) {
	for id, body := range documentedCodes(t) {
		if _, live := byID[id]; !live {
			continue
		}
		text := fenced.ReplaceAllString(strings.ReplaceAll(body, "\r\n", "\n"), ". ")
		folded := strings.Join(strings.Fields(text), " ")
		for _, pair := range slashArgumentsDiffer(folded) {
			t.Errorf("AC-12 %s: the section offers the slash command without the terminal command's arguments: %s", id, pair)
		}
		for _, sentence := range sentences(folded) {
			for _, c := range personsCommands {
				at, quote := instructed(sentence, c)
				if at < 0 {
					continue
				}
				if want := quote + c.slash + quote + ", or "; !strings.HasSuffix(sentence[:at], want) {
					t.Errorf("AC-12 %s: the section tells the person to run %s without %s first:\n%s",
						id, c.terminal, c.slash, sentence)
				}
			}
		}
	}
}
