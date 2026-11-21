package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// US-131: `sdlc decide` records an implementation decision the loop made, on the
// story's record and in DECISIONS.md beside it, and `sdlc status` and `sdlc trace
// --all` count them. The criteria name the command's flags; the event's shape
// inside the record is the implementation's, so the record is read here as JSON
// and an event is found by its type and the text it holds.

const dcCode = "SDLC-E0094"

// The text of the decisions these tests record. Each is a sentence no other part
// of a record holds, so that finding it in an event is finding the decision.
const (
	dcQ1 = "Which total does AC-2 mean when a refund is pending?"
	dcO1 = "the gross total before the refund"
	dcO2 = "the net total after the refund"
	dcR1 = "AC-2 says what the customer is charged, which is the net total"

	dcQ2 = "Does an empty invoice keep its number?"
	dcO3 = "it keeps the number it was given"
	dcO4 = "it gives the number back"
	dcR2 = "a number given back is one an auditor finds missing"
)

// dcFirst is `sdlc decide` for the first decision, by the researcher.
func dcFirst() []string {
	return []string{"decide", "US-001", "--question", dcQ1, "--option", dcO1, "--option", dcO2,
		"--chosen", dcO2, "--reason", dcR1, "--by", "researcher"}
}

// dcSecond is the second, by the test author.
func dcSecond() []string {
	return []string{"decide", "US-001", "--question", dcQ2, "--option", dcO3, "--option", dcO4,
		"--chosen", dcO3, "--reason", dcR2, "--by", "sdet"}
}

// dcRecordText is the story's record on disk, as bytes are compared.
func dcRecordText(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", model.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// dcEvents is every event of the type `decision` on the story's record, each as
// the JSON it is written as.
func dcEvents(t *testing.T, root string) []string {
	t.Helper()
	var record struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal([]byte(dcRecordText(t, root)), &record); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, raw := range record.Events {
		var ev struct {
			Type string `json:"type"`
			At   string `json:"at"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type == "decision" {
			if ev.At == "" {
				t.Errorf("AC-3 a decision event has no time: %s", raw)
			}
			out = append(out, string(raw))
		}
	}
	return out
}

// dcHolds reports whether an event, as JSON, holds a value as a string would be
// written into it.
func dcHolds(event, value string) bool {
	quoted, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return strings.Contains(event, strings.Trim(string(quoted), `"`))
}

// dcFile is the story's DECISIONS.md, and whether it is there.
func dcFile(t *testing.T, root, id string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", id, "DECISIONS.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), true
}

// dcUnread is decisions_unread from `sdlc status --json`, which is there whatever
// its value: a script reads 0 as read, and a key left out as an older sdlc.
func dcUnread(t *testing.T) int {
	t.Helper()
	got := decode[struct {
		DecisionsUnread *int `json:"decisions_unread"`
	}](t, mustRun(t, "status", "--json"))
	if got.DecisionsUnread == nil {
		t.Fatal("AC-9 sdlc status --json has no decisions_unread")
	}
	return *got.DecisionsUnread
}

// dcFixOf is what a refusal prints after "fix", to the blank line.
func dcFixOf(stderr string) string {
	m := regexp.MustCompile(`(?s)\n  fix  (.*?)\n\n`).FindStringSubmatch(stderr)
	if m == nil {
		return ""
	}
	return m[1]
}

func TestAC3ADecisionIsRecordedOnTheRecordAndInDecisionsMdAndPrintsOneLine(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	before := readRecord(t, root, "US-001")

	out := mustRun(t, dcFirst()...).stdout
	if strings.TrimSpace(out) == "" || strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Errorf("AC-3 decide printed %q, want one line", out)
	}

	events := dcEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("AC-3 the record holds %d decision events, want 1: %v", len(events), events)
	}
	for what, value := range map[string]string{
		"the question": dcQ1, "the first option": dcO1, "the second option, the one chosen": dcO2,
		"the reason": dcR1, "the role that decided": "researcher", "the gate the story is at": string(model.GateDoR),
	} {
		if !dcHolds(events[0], value) {
			t.Errorf("AC-3 the decision event does not hold %s, %q: %s", what, value, events[0])
		}
	}

	// It changes no gate, review, approval, escalation or staffing, and adds the
	// one event.
	after := readRecord(t, root, "US-001")
	for what, pair := range map[string][2]any{
		"gates":       {before.Gates, after.Gates},
		"reviews":     {before.Reviews, after.Reviews},
		"approvals":   {before.Approvals, after.Approvals},
		"escalations": {before.Escalations, after.Escalations},
		"staffing":    {before.Staffing, after.Staffing},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("AC-4 a decision changed the record's %s: %+v, then %+v", what, pair[0], pair[1])
		}
	}
	if len(after.Events) != len(before.Events)+1 {
		t.Errorf("AC-3 the record went from %d events to %d, want one more", len(before.Events), len(after.Events))
	}

	// The next is made at the next gate, and both are in DECISIONS.md in the order made.
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	mustRun(t, dcSecond()...)
	events = dcEvents(t, root)
	if len(events) != 2 || !dcHolds(events[1], dcQ2) || !dcHolds(events[1], string(model.GateAnalysis)) {
		t.Errorf("AC-3 the second decision is not on the record at the analysis gate: %v", events)
	}
	doc, ok := dcFile(t, root, "US-001")
	if !ok {
		t.Fatal("AC-3 there is no .sdlc/stories/US-001/DECISIONS.md")
	}
	for _, value := range []string{dcQ1, dcO1, dcO2, dcR1, "researcher", dcQ2, dcO3, dcO4, dcR2, "sdet"} {
		if !strings.Contains(doc, value) {
			t.Errorf("AC-3 DECISIONS.md does not hold %q:\n%s", value, doc)
		}
	}
	if first, second := strings.Index(doc, dcQ1), strings.Index(doc, dcQ2); first < 0 || second < first {
		t.Errorf("AC-3 DECISIONS.md does not hold the decisions in the order they were made:\n%s", doc)
	}
}

// The settled IQ-7: a decision records the gate the story is at, and `done` once
// every gate has passed.
func TestAC3ADecisionMadeOnceEveryGateHasPassedIsRecordedAtDone(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, dcFirst()...)
	events := dcEvents(t, root)
	if len(events) != 1 || !dcHolds(events[0], "done") {
		t.Errorf("AC-3 a decision after the last gate is not recorded at done: %v", events)
	}
}

// IQ-7: a decision belongs to a story the loop has started; one never started, or
// not in the backlog, is refused and nothing is made for it.
func TestAC3DecideRefusesAStoryNeverStartedOrNotInTheBacklog(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	addStory(t, root)
	mustRun(t, "start", "US-001")
	// The same decision is taken on the story under way, so each refusal below is
	// of the story it names, not of a command that does not exist.
	mustRun(t, dcFirst()...)
	for _, id := range []string{"US-002", "US-404"} {
		args := dcFirst()
		args[1] = id
		if r := run(t, args...); r.code == 0 {
			t.Errorf("AC-3 a decision on %s, which was never started, was recorded:\n%s", id, r.stdout)
		}
		if _, err := os.Stat(filepath.Join(root, ".sdlc", "stories", id)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("AC-3 the refused decision on %s left .sdlc/stories/%s behind (%v)", id, id, err)
		}
	}
	if r := run(t, dcFirst()[2:]...); r.code == 0 {
		t.Error("AC-3 decide with no story named was recorded")
	}
	if n := len(dcEvents(t, root)); n != 1 {
		t.Errorf("AC-3 the story under way holds %d decisions, want the one it took: a refused decision was recorded on it", n)
	}
}

// AC-4: each malformed decision is refused with the decision code, whose fix names
// the part that is missing, and records nothing anywhere.
func TestAC4AMalformedDecisionIsRefusedWithTheDecisionCodeNamingWhatIsMissing(t *testing.T) {
	t.Parallel()
	q, o1, o2, ch, rs, by := "--question", "--option", "--option", "--chosen", "--reason", "--by"
	for _, c := range []struct {
		name  string
		args  []string
		names []string
	}{
		{"AC-4 no question", []string{o1, "a", o2, "b", ch, "a", rs, "r", by, "researcher"}, []string{q}},
		{"AC-4 an empty question", []string{q, "", o1, "a", o2, "b", ch, "a", rs, "r", by, "researcher"}, []string{q}},
		{"AC-4 a blank question", []string{q, "  \t ", o1, "a", o2, "b", ch, "a", rs, "r", by, "researcher"}, []string{q}},
		{"AC-4 nothing chosen", []string{q, "q", o1, "a", o2, "b", rs, "r", by, "researcher"}, []string{ch}},
		{"AC-4 an empty choice", []string{q, "q", o1, "a", o2, "b", ch, "", rs, "r", by, "researcher"}, []string{ch}},
		{"AC-4 no reason", []string{q, "q", o1, "a", o2, "b", ch, "a", by, "researcher"}, []string{rs}},
		{"AC-4 a blank reason", []string{q, "q", o1, "a", o2, "b", ch, "a", rs, " ", by, "researcher"}, []string{rs}},
		{"AC-4 no option", []string{q, "q", ch, "a", rs, "r", by, "researcher"}, []string{o1}},
		{"AC-4 one option", []string{q, "q", o1, "a", ch, "a", rs, "r", by, "researcher"}, []string{o1}},
		{"AC-4 one option and a blank one", []string{q, "q", o1, "a", o2, " ", ch, "a", rs, "r", by, "researcher"}, []string{o1}},
		{"AC-4 the same option twice", []string{q, "q", o1, "a", o2, "a", ch, "a", rs, "r", by, "researcher"}, []string{o1}},
		{"AC-4 a choice that is not an option", []string{q, "q", o1, "a", o2, "b", ch, "c", rs, "r", by, "researcher"}, []string{ch, o1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			mustRun(t, "start")
			before := dcRecordText(t, root)
			r := run(t, append([]string{"decide", "US-001"}, c.args...)...)
			if r.code == 0 {
				t.Fatalf("AC-4 the decision was recorded:\n%s", r.stdout)
			}
			if !strings.Contains(r.stderr, dcCode) {
				t.Errorf("AC-4 the refusal is not %s:\n%s", dcCode, r.stderr)
			}
			fix := dcFixOf(r.stderr)
			for _, flag := range c.names {
				if !strings.Contains(fix, flag) {
					t.Errorf("AC-4 the fix does not name %s: %q\n%s", flag, fix, r.stderr)
				}
			}
			if after := dcRecordText(t, root); after != before {
				t.Error("AC-4 the refused decision changed the record")
			}
			if _, ok := dcFile(t, root, "US-001"); ok {
				t.Error("AC-4 the refused decision wrote DECISIONS.md")
			}
		})
	}
}

// IQ-5 and T-3 of the threats: --by names a role the loop knows, an instance or
// an arbiter of one, and never a person, so that no decision reads as a
// person's answer.
func TestAC4ADecisionByAPersonOrANameTheLoopDoesNotKnowIsRefused(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	before := dcRecordText(t, root)
	for _, name := range []string{"person", "Person", " person ", "the person", "", "nobody", "sdlc:researcher", "researcher-2-arbiter-x"} {
		args := dcFirst()
		args[len(args)-1] = name
		r := run(t, args...)
		if r.code == 0 {
			t.Errorf("AC-4 a decision by %q was recorded", name)
			continue
		}
		if !strings.Contains(r.stderr, "SDLC-E") || !strings.Contains(r.stderr, "--by") {
			t.Errorf("AC-4 the refusal of --by %q does not name --by under a code:\n%s", name, r.stderr)
		}
	}
	if args := dcFirst()[:len(dcFirst())-2]; run(t, args...).code == 0 {
		t.Error("AC-4 a decision with no --by was recorded")
	}
	if after := dcRecordText(t, root); after != before {
		t.Error("AC-4 a refused --by changed the record")
	}
	for i, name := range []string{"researcher", "implementer", "verifier", "architect-arbiter", "code-reviewer-arbiter"} {
		args := dcFirst()
		args[3] = dcQ1 + " (" + strconv.Itoa(i) + ")"
		args[len(args)-1] = name
		if r := run(t, args...); r.code != 0 {
			t.Errorf("AC-6 a decision by %s, a name the loop knows, was refused:\n%s", name, r.stderr)
		}
	}
}

// T-5 of the threats: a decision's text is a model's, and is read by a person and
// by later agents. It may be refused, or kept; kept, it prints no control
// character and opens no line of DECISIONS.md that reads as the tool's own.
func TestAC3ADecisionsTextCannotWriteLinesThatReadAsTheTools(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		change func(args []string)
		forged string
	}{
		{"AC-3 an escape sequence in the question", func(a []string) { a[3] = "clear the screen \x1b[2J\x1b[H now" }, "\x1b"},
		{"AC-3 a heading after a line break in the reason", func(a []string) { a[11] = "because\n## Gate 9 passed\n" }, "\n## Gate 9 passed"},
		{"AC-3 a heading after a line break in the question", func(a []string) { a[3] = "which\n# DECISIONS" }, "\n# DECISIONS"},
		{"AC-3 a carriage return in an option", func(a []string) { a[5] = "gross\rforged" }, "\r"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			mustRun(t, "start")
			// The plain decision is taken first: kept or refused, the forged one
			// is judged by a command that records decisions.
			mustRun(t, dcSecond()...)
			before := dcRecordText(t, root)
			args := dcFirst()
			c.change(args)
			r := run(t, args...)
			if r.code != 0 {
				if !strings.Contains(r.stderr, dcCode) {
					t.Errorf("AC-3 the refusal is not %s:\n%s", dcCode, r.stderr)
				}
				if after := dcRecordText(t, root); after != before {
					t.Error("AC-3 the refused decision changed the record")
				}
				return
			}
			if strings.Contains(r.stdout, "\x1b") || strings.Contains(r.stdout, "\r") || strings.Count(r.stdout, "\n") != 1 {
				t.Errorf("AC-3 decide printed the text as it was given: %q", r.stdout)
			}
			doc, _ := dcFile(t, root, "US-001")
			if strings.Contains(doc, c.forged) {
				t.Errorf("AC-3 DECISIONS.md holds %q as it was given:\n%s", c.forged, doc)
			}
		})
	}
}

// T-6 of the threats: every hook call reads the record, so a decision does not
// grow it without bound.
func TestAC3ADecisionOfUnboundedSizeIsRefusedAndRecordsNothing(t *testing.T) {
	t.Parallel()
	many := make([]string, 0, 20002)
	for i := range 10000 {
		many = append(many, "--option", "option "+strconv.Itoa(i))
	}
	for _, c := range []struct {
		name string
		args []string
	}{
		{"AC-3 a question of a megabyte", func() []string { a := dcFirst(); a[3] = strings.Repeat("x", 1<<20); return a }()},
		{"AC-3 ten thousand options", append([]string{"decide", "US-001", "--question", "q", "--chosen", "option 1",
			"--reason", "r", "--by", "researcher"}, many...)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			mustRun(t, "start")
			before := dcRecordText(t, root)
			if r := run(t, c.args...); r.code == 0 {
				t.Error("AC-3 the decision was recorded")
			} else if !strings.Contains(r.stderr, dcCode) {
				t.Errorf("AC-3 the refusal is not %s:\n%s", dcCode, r.stderr)
			}
			if after := dcRecordText(t, root); after != before {
				t.Error("AC-3 the refused decision changed the record")
			}
			// A decision of a size the record takes is still recorded after it.
			mustRun(t, dcFirst()...)
			if n := len(dcEvents(t, root)); n != 1 {
				t.Errorf("AC-3 the record holds %d decisions after one of a size it takes, want 1", n)
			}
		})
	}
}

// T-2 of the threats: a decision is not a person's answer. It starts no count
// again, and answers nothing.
func TestAC3ADecisionIsNoPersonsAnswer(t *testing.T) {
	t.Parallel()
	root := planned(t)
	block := []string{"review", "add", "design_review", "architect", "block", "--important", "1",
		"--finding", "important the one finding", "--note", "AC-3 has no step"}
	mustRunWith(t, "# architect\n", block...)
	mustRun(t, "decide", "US-001", "--question", "Does the plan need a step for AC-3?",
		"--option", "add one", "--option", "decline", "--chosen", "add one", "--reason", "AC-3 is observable", "--by", "implementer")
	record := readRecord(t, root, "US-001")
	if n := record.SinceDecision(model.EventReview, model.ReviewEvent(model.GateDesignReview, "architect", model.VerdictBlock)); n != 1 {
		t.Errorf("AC-3 the architect's blocks since a person answered = %d after a decision, want 1", n)
	}
	if len(record.Approvals) != 0 {
		t.Errorf("AC-3 a decision was recorded as a person's answer: %+v", record.Approvals)
	}
	if _, ok := record.LatestDecision(); ok {
		t.Error("AC-3 a decision reads as a person's latest answer")
	}
}

// AC-3 and AC-9, with the settled IQ-2: a decision is read once a person's ack
// covers the commit of its story. Until then `sdlc status` counts it, whether the
// story is committed and listed by `sdlc log`, or not yet committed.
func TestAC3AC9TheLogListsADecisionAndStatusCountsItUntilThePersonsAck(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	addStory(t, root)
	commitEverything(t, root)
	mustRun(t, "start", "US-001")
	if n := dcUnread(t); n != 0 {
		t.Errorf("AC-9 decisions_unread is %d before any decision, want 0", n)
	}
	mustRun(t, dcFirst()...)
	mustRun(t, dcSecond()...)
	if n := dcUnread(t); n != 2 {
		t.Errorf("AC-9 decisions_unread is %d with two decisions in a story under way, want 2", n)
	}
	if line := dcStatusLine(t); !regexp.MustCompile(`\b2\b`).MatchString(line) {
		t.Errorf("AC-9 sdlc status does not say there are 2 decisions: %q", line)
	}

	reach(t, root, "")
	mustRun(t, "stop")
	if n := dcUnread(t); n != 2 {
		t.Errorf("AC-9 decisions_unread is %d once the story is committed and not acknowledged, want 2", n)
	}
	text := mustRun(t, "log").stdout
	for _, value := range []string{dcQ1, dcO2, dcQ2, dcO3} {
		if !strings.Contains(text, value) {
			t.Errorf("AC-3 sdlc log does not show %q:\n%s", value, text)
		}
	}
	if raw := mustRun(t, "log", "--json").stdout; !strings.Contains(raw, dcQ1) || !strings.Contains(raw, dcQ2) {
		t.Errorf("AC-3 sdlc log --json does not hold the decisions:\n%s", raw)
	}

	mustRun(t, "ack", "--through", "HEAD")
	if n := dcUnread(t); n != 0 {
		t.Errorf("AC-9 decisions_unread is %d after the person's ack, want 0", n)
	}
	if entries := decode[logPayload](t, mustRun(t, "log", "--json")).Entries; len(entries) != 0 {
		t.Errorf("AC-3 the log lists %d stories after the ack, want none", len(entries))
	}

	mustRun(t, "start", "US-002")
	args := dcFirst()
	args[1] = "US-002"
	mustRun(t, args...)
	if n := dcUnread(t); n != 1 {
		t.Errorf("AC-9 decisions_unread is %d with one decision in the next story, want 1", n)
	}
}

// dcStatusLine is the line of `sdlc status` that speaks of decisions.
func dcStatusLine(t *testing.T) string {
	t.Helper()
	out := mustRun(t, "status").stdout
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(strings.ToLower(line), "decision") {
			return line
		}
	}
	t.Errorf("AC-9 sdlc status has no line about decisions:\n%s", out)
	return ""
}

// AC-9: `sdlc trace --all` gives each story's decisions on its second line,
// beside its escalations, and in --json.
func TestAC9TraceAllCountsEachStorysDecisionsBesideItsEscalations(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	mustRun(t, dcFirst()...)
	mustRun(t, dcSecond()...)

	text := mustRun(t, "trace", "--all").stdout
	lines := strings.Split(text, "\n")
	second := ""
	for i, line := range lines {
		if strings.HasPrefix(line, "US-001 ") && i+1 < len(lines) {
			second = lines[i+1]
		}
	}
	if !strings.Contains(second, "escalations:") || !regexp.MustCompile(`decisions:? 2\b`).MatchString(second) {
		t.Errorf("AC-9 US-001's second line does not give its 2 decisions beside its escalations: %q\n%s", second, text)
	}
	var doc struct {
		Stories []struct {
			Story     string `json:"story"`
			Decisions *int   `json:"decisions"`
		} `json:"stories"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, "trace", "--all", "--json").stdout), &doc); err != nil {
		t.Fatal(err)
	}
	for _, s := range doc.Stories {
		if s.Story == "US-001" && (s.Decisions == nil || *s.Decisions != 2) {
			t.Errorf("AC-9 trace --all --json gives US-001 decisions %v, want 2", s.Decisions)
		}
	}
	_ = root
}

// AC-5, AC-7 and the settled IQ-1: escalate keeps every reason it took before,
// and takes product_decision as one more, on the story under way. Each row is
// accepted today, as any word is, so the row first holds that product_decision
// is a reason the loop names, where a person reads what escalate takes.
func TestAC5AC7EscalateKeepsEveryReasonAndTakesProductDecision(t *testing.T) {
	t.Parallel()
	if escalate := us131Section(t, "`sdlc escalate`"); !strings.Contains(escalate, "product_decision") {
		t.Fatal("AC-7 the `sdlc escalate` section of docs/commands.md does not name product_decision")
	}
	root := gitProjectInitialised(t)
	for _, kind := range []string{
		"product_decision", "spec_unclear", "design_decision", "frozen_test_wrong", "untestable",
		"analysis_incomplete", "work_by_hand", "freeze_broken", "pre_commit_approval",
		"review_not_converging", "loop_stalled", "spec_change", "human_check", "a_word_of_the_projects_own",
	} {
		mustRun(t, "start", "US-001")
		mustRun(t, "escalate", kind, "--message", "Q1: who is it for? A or B; I recommend A")
		record := readRecord(t, root, "US-001")
		pending, ok := record.PendingEscalation()
		if !ok || pending.Type != kind {
			t.Errorf("AC-7 escalate %s left %+v pending", kind, pending)
		}
		mustRun(t, "approve", "US-001")
	}
}
