package cli

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

const flagFinding = "finding"

// findingExample is a finding as --finding takes it, for a refusal to show.
const findingExample = `write it as --finding "important internal/a.go: the zero case is not tested", ` +
	`or with no path, as --finding "nit the plan: step 2 is vague"`

// readFindings is what --finding gave, read and held to the review's counts
// before anything else is read or recorded. A value is named by its place and
// never echoed: it is a reviewer's free text, and a refusal is read as sdlc's.
func readFindings(gate model.Gate, f reviewFlags) ([]model.Finding, error) {
	if len(f.findings) > model.MaxFindingsPerReview {
		return nil, sdlcerr.New(sdlcerr.BadArgument,
			"--finding was given "+strconv.Itoa(len(f.findings))+" times",
			"a review lists at most "+strconv.Itoa(model.MaxFindingsPerReview)+" findings").
			WithFix("list the findings that matter most, and put the rest in the review's document")
	}
	var out []model.Finding
	for i, v := range f.findings {
		which := "--" + flagFinding
		if len(f.findings) > 1 {
			which = fmt.Sprintf("--finding %d of %d", i+1, len(f.findings))
		}
		if err := refuseFreeText(which, v); err != nil {
			return nil, err
		}
		parsed, err := model.ParseFinding(v)
		if err != nil {
			return nil, sdlcerr.New(sdlcerr.BadArgument, which+" cannot be read", err.Error()).WithFix(findingExample)
		}
		out = append(out, parsed)
	}
	rev := model.Review{Gate: gate, Important: f.important, Nits: f.nits, Findings: out}
	if rev.FindingsMatchCounts() {
		return out, nil
	}
	counted := "the review counts " + describeImportant(f.important) + " and " + describeNits(f.nits)
	if len(out) == 0 {
		return nil, sdlcerr.New(sdlcerr.FindingsDisagree, "--finding was not given, and "+counted,
			"at design_review and code_review, each finding a review counts is listed, so that it can be closed")
	}
	important := 0
	for _, x := range out {
		if x.Importance == model.FindingImportant {
			important++
		}
	}
	return nil, sdlcerr.New(sdlcerr.FindingsDisagree,
		"the findings listed with --finding do not agree with --important and --nits",
		fmt.Sprintf("--finding lists %d important and %s, and %s", important, nits(len(out)-important), counted))
}

// describeImportant is a count of important findings as given, or "no
// important findings" where it was not.
func describeImportant(n *int) string {
	if n == nil {
		return "no important findings"
	}
	return strconv.Itoa(*n) + " important"
}

// describeNits is a count of nits as given, or "no nits" where it was not.
func describeNits(n *int) string {
	if n == nil {
		return "no nits"
	}
	return nits(*n)
}

// nits is n nits as a sentence says it: "1 nit", "2 nits".
func nits(n int) string {
	if n == 1 {
		return "1 nit"
	}
	return strconv.Itoa(n) + " nits"
}

// noteListedFindings writes how far a review's findings were numbered into the
// record's history, where an sdlc older than the findings keeps it when it saves
// the record and drops the findings themselves. The gates compare the two, so a
// record an older plugin binary saved cannot pass with its findings erased.
func noteListedFindings(record *model.Record, rev model.Review, at time.Time) {
	if n := len(rev.Findings); n > 0 {
		record.Append(model.EventFindings, string(rev.Gate)+" "+rev.Role+" listed "+rev.Findings[0].ID+" to "+rev.Findings[n-1].ID, at)
	}
}

// listedFindings reads noteListedFindings' message. Made when first needed and
// not at start-up, which every tool call the hook answers pays for.
var listedFindings = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^(\S+) (\S+) listed F-[0-9]+ to F-([0-9]+)$`)
})

func newFindingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "finding",
		Short: "Close a finding a review listed",
		Long: "A finding a review lists with --finding stays open until it is closed: fixed,\n" +
			"declined with a reason, or referred to a person with a reason. A design\n" +
			"review's open finding stops the verification gate, and a design or code\n" +
			"review's stops the commit gate.",
		Example: "  sdlc finding close code_review code-reviewer F-1 --fixed",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newFindingCloseCmd())
	return cmd
}

func newFindingCloseCmd() *cobra.Command {
	var c findingClose
	cmd := &cobra.Command{
		Use:   "close GATE ROLE F-N",
		Short: "Close one finding as fixed, declined or referred",
		Long: "close records how a finding of the story being worked on was closed, with the\n" +
			"session it ran in and the time, in the story's record. It takes exactly one\n" +
			"of --fixed, --declined and --referred; a decline and a referral need\n" +
			"--reason. A finding about a path the story changes is fixed or declined,\n" +
			"never referred, and the verification and commit gates ask that again.\n" +
			"Closing it again replaces the earlier closing, and both stay on the record.\n\n" +
			"Keep $, backticks and double quotes out of a reason: the shell expands them.",
		Example: "  sdlc finding close code_review code-reviewer F-1 --fixed\n" +
			"  sdlc finding close design_review architect F-2 --declined --reason \"step 4 covers it\"",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error { return runFindingClose(cmd, args, c) },
	}
	cmd.Flags().BoolVar(&c.fixed, model.ClosedFixed, false, "the finding is fixed in the story")
	cmd.Flags().BoolVar(&c.declined, model.ClosedDeclined, false, "the finding is declined, with --reason")
	cmd.Flags().BoolVar(&c.referred, model.ClosedReferred, false,
		"the finding is outside the story's files, cannot wait, and goes to a person, with --reason")
	cmd.Flags().StringVar(&c.reason, "reason", "", "why it was declined or referred, in one line")
	return cmd
}

// findingClose is the flags `sdlc finding close` was given.
type findingClose struct {
	fixed, declined, referred bool
	reason                    string
}

// read is the closing, the gate and the reviewer the command line names, or a
// refusal, before anything is opened.
func (c findingClose) read(args []string) (string, model.Gate, model.Reviewer, error) {
	none := model.Reviewer{}
	as, err := c.as()
	if err != nil {
		return "", "", none, err
	}
	gate := model.Gate(args[0])
	if !gate.Valid() {
		return "", "", none, sdlcerr.New(sdlcerr.UnknownGate, "there is no gate called "+quote(args[0]), "this version knows "+model.GateNames())
	}
	reviewer, ok := model.FindReviewer(gate, args[1])
	if !ok {
		return "", "", none, sdlcerr.New(sdlcerr.UnknownReviewer,
			string(gate)+" has no reviewer called "+quote(args[1]), "it has "+model.ReviewerNames(gate))
	}
	if !findingID().MatchString(args[2]) {
		return "", "", none, sdlcerr.New(sdlcerr.BadArgument, quote(args[2])+" is not a finding's number",
			"a finding is named by the number its review gave it").
			WithFix(`write it as F-1, F-2 and so on, as "sdlc status --json" lists it under "findings"`)
	}
	return as, gate, reviewer, nil
}

// as is the one closing the flags name, or a refusal.
func (c findingClose) as() (string, error) {
	var named []string
	for _, x := range []struct {
		on   bool
		name string
	}{{c.fixed, model.ClosedFixed}, {c.declined, model.ClosedDeclined}, {c.referred, model.ClosedReferred}} {
		if x.on {
			named = append(named, x.name)
		}
	}
	if len(named) != 1 {
		return "", sdlcerr.New(sdlcerr.BadArgument,
			"a finding is closed one way, and this names "+strconv.Itoa(len(named)),
			"each closing is fixed, declined or referred, and the latest is the finding's state").
			WithFix(`give exactly one of --fixed, --declined --reason "<why>" or --referred --reason "<why>"`)
	}
	as := named[0]
	if as != model.ClosedFixed && strings.TrimSpace(c.reason) == "" {
		return "", sdlcerr.New(sdlcerr.ClosingWithoutReason,
			"--"+as+" was given no reason",
			"a decline or a referral is a decision, and the reviewer who raised the finding and the verifier judge its reason")
	}
	if err := refuseFreeText("--reason", c.reason); err != nil {
		return "", err
	}
	return as, nil
}

var findingID = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`^F-[1-9][0-9]{0,8}$`) })

func runFindingClose(cmd *cobra.Command, args []string, c findingClose) error {
	as, gate, reviewer, err := c.read(args)
	if err != nil {
		return err
	}
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	id, err := activeStory(s, "a finding belongs to the story being worked on")
	if err != nil {
		return err
	}
	if err := refuseIfHeldElsewhere(s, id, "a finding is closed in the checkout whose work answers it"); err != nil {
		return err
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	f := findingOf(record, gate, reviewer.Role, args[2])
	if f == nil {
		return sdlcerr.New(sdlcerr.BadArgument,
			string(gate)+" "+reviewer.Role+" has no finding "+args[2]+" on "+id,
			"a finding is closed by the gate, role and number its review gave it").
			WithFix(`"sdlc status --json" lists the story's findings under "findings", each with its gate, role and id`)
	}
	if as == model.ClosedReferred && f.Path != "" {
		if err := refuseReferralInTheStory(cmd.Context(), readingOf(s), record, f.Path); err != nil {
			return err
		}
	}
	closing := model.Closing{At: model.Timestamp(s.Now()), As: as, Reason: strings.TrimSpace(c.reason)}
	if session := s.CommandSession(); store.CheckSession(session) {
		closing.Session = session
	}
	f.Closings = append(f.Closings, closing)
	if err := s.SaveRecord(record); err != nil {
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), findingClosePayload{
			OK: true, Story: id, Gate: string(gate), Role: reviewer.Role, ID: f.ID, Closing: closing,
		})
	}
	closed := as
	if as == model.ClosedReferred {
		closed = "referred for a person to read when the story ends"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s  %s %s %s  %s; %s\n", id, gate, reviewer.Role, f.ID, closed, whatIsLeft(record))
	return nil
}

// whatIsLeft is how many findings of the gates that hold them are still open,
// and the first of them, so that whoever closes a wave knows when it is done
// without asking status. Counted for the closed finding's gate alone, it said
// none was open while a code review finding still stopped the commit gate.
func whatIsLeft(record *model.Record) string {
	of := string(model.GateDesignReview) + " or " + string(model.GateCodeReview)
	var open []string
	for _, h := range findingsAt(record, []model.Gate{model.GateDesignReview, model.GateCodeReview}) {
		if len(h.Closings) == 0 {
			open = append(open, h.name())
		}
	}
	switch len(open) {
	case 0:
		return "no finding of " + of + " is open"
	case 1:
		return "1 finding of " + of + " is still open: " + open[0]
	}
	return fmt.Sprintf("%d findings of %s are still open, the first %s", len(open), of, open[0])
}

type findingClosePayload struct {
	OK      bool          `json:"ok"`
	Story   string        `json:"story"`
	Gate    string        `json:"gate"`
	Role    string        `json:"role"`
	ID      string        `json:"id"`
	Closing model.Closing `json:"closing"`
}

// refuseFreeText holds a finding or a reason to one line of bounded length. It
// is read back by the next model, in status, the log and the gates' refusals,
// and a line break in it could start a line that reads as sdlc's own. A line
// or paragraph separator does the same in some terminals, a bidirectional
// control makes the text read otherwise than it was written, and a tag
// character or a variation selector carries text a person never sees to the
// model that reads it next.
func refuseFreeText(what, s string) error {
	switch {
	case len(s) > model.MaxFindingBytes:
		return sdlcerr.New(sdlcerr.BadArgument, what+" is longer than "+strconv.Itoa(model.MaxFindingBytes)+" bytes",
			"a finding or a reason is read on every status and log").
			WithFix("say it in a line or two, and put the detail in the review's document")
	case strings.ContainsFunc(s, hiddenText):
		return sdlcerr.New(sdlcerr.BadArgument, what+" holds a line break, a control character or an invisible one",
			"the text is shown on one line of sdlc's own, and these would change how it reads").
			WithFix("write it on one line, in plain text")
	}
	return nil
}

// hiddenText is a character that breaks a line, or one a terminal draws as
// nothing. The format characters (Cf) are the bidirectional controls, the
// zero-width ones, the byte order mark and the tag characters; with the
// variation selectors and Unicode's other default-ignorable code points (the
// combining grapheme joiner, the Hangul fillers, two Khmer vowels and the
// unassigned ones) they cover its whole default-ignorable set. A hand list of
// that set let the Khmer vowels and 128 unassigned tag-plane code points carry
// hidden text. Accents, dashes, curly quotes and combining marks are seen.
func hiddenText(r rune) bool {
	return unicode.IsControl(r) ||
		unicode.In(r, unicode.Zl, unicode.Zp, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// findingOf is the finding of a gate's reviewer with this id, in whichever
// round raised it, to be changed in place.
func findingOf(record *model.Record, gate model.Gate, role, id string) *model.Finding {
	for i := range record.Reviews {
		rev := &record.Reviews[i]
		if rev.Gate != gate || rev.Role != role {
			continue
		}
		for j := range rev.Findings {
			if rev.Findings[j].ID == id {
				return &rev.Findings[j]
			}
		}
	}
	return nil
}

// namesChanged reports whether a finding's file is the changed path, or a
// directory holding it, matched segment by segment and with case folded as macOS
// and Windows fold it, so `internal/bill` is not `internal/billing.go` and
// `Internal/Billing.go` is. A path with fewer leading directories than the
// changed one, or more, matches where its last segments are the other's: a
// reviewer writes `billing.go:3` or `cli/billing.go` from inside a package, or
// a path from another checkout's root, and refusing a referral too many is the
// safe side, since a decline is always there.
func namesChanged(file, changed string) bool {
	return pathrules.Under(changed, file) || endsWithSegments(changed, file) || endsWithSegments(file, changed)
}

// endsWithSegments reports whether p's last segments are all of tail's, with
// case folded as pathrules folds it, and p has more segments than tail.
func endsWithSegments(p, tail string) bool {
	ps, ts := strings.Split(pathrules.Fold(p), "/"), strings.Split(pathrules.Fold(tail), "/")
	return len(ps) > len(ts) && slices.Equal(ps[len(ps)-len(ts):], ts)
}

// changedUnder is the first path the story changes that the finding's path
// names, past its quoting and line number (model.FindingFile). ok is false where
// the story's commits cannot be read, which a referral is refused for rather
// than let through.
func changedUnder(ctx context.Context, rd *reading, record *model.Record, findingPath string) (string, bool, error) {
	changed, err := rd.changedPaths(ctx, record)
	if err != nil || rd.unreadRange {
		return "", !rd.unreadRange, err
	}
	file := model.FindingFile(findingPath)
	if i := slices.IndexFunc(changed, func(c string) bool { return namesChanged(file, c) }); i >= 0 {
		return changed[i], true, nil
	}
	return "", true, nil
}

// refuseReferralInTheStory keeps a finding about a path the story changes in
// the story.
func refuseReferralInTheStory(ctx context.Context, rd *reading, record *model.Record, findingPath string) error {
	changed, ok, err := changedUnder(ctx, rd, record, findingPath)
	switch {
	case err != nil:
		return err
	case !ok:
		return sdlcerr.New(sdlcerr.ReferredInTheStory,
			"whether the story changes "+quote(findingPath)+" cannot be told",
			"the record does not say where HEAD was when code review first passed, so the story's commits cannot be read").
			WithFix(`close it with --declined --reason "<why>", or with --fixed once it is fixed`)
	case changed != "":
		return sdlcerr.New(sdlcerr.ReferredInTheStory,
			"the finding is about "+quote(findingPath)+", and the story changes "+quote(changed),
			"a defect in a file the story changes is fixed in the story, or declined with a reason, and not referred").
			WithFix(`fix it and close it with --fixed, or close it with --declined --reason "<why>"`)
	}
	return nil
}

// requireFindings holds a gate to the findings of the gates named: none an
// older sdlc dropped, none open, and none referred whose file the story has
// changed since.
func requireFindings(ctx context.Context, rd *reading, gate model.Gate, record *model.Record, of ...model.Gate) error {
	held := findingsAt(record, of)
	if err := refuseDroppedFindings(gate, record, of, held); err != nil {
		return err
	}
	if err := requireClosedFindings(gate, of, held); err != nil {
		return err
	}
	return refuseStandingReferrals(ctx, rd, gate, record, held)
}

// heldFinding is a finding with the gate and role of the review that listed it.
type heldFinding struct {
	gate model.Gate
	role string
	model.Finding
}

// name is the finding as "sdlc finding close" takes it.
func (h heldFinding) name() string {
	return string(h.gate) + " " + oneLine(h.role) + " " + oneLine(h.ID)
}

// findingsAt is every finding the reviews of the gates named listed, in every
// round, the rounds a later review superseded included, in record order.
func findingsAt(record *model.Record, of []model.Gate) []heldFinding {
	var out []heldFinding
	for _, rev := range record.Reviews {
		if !slices.Contains(of, rev.Gate) {
			continue
		}
		for _, f := range rev.Findings {
			out = append(out, heldFinding{rev.Gate, rev.Role, f})
		}
	}
	return out
}

// listedCounts is, for each gate and role of the gates named, the last F-n the
// record's history says its reviews listed, and the order they came in.
func listedCounts(record *model.Record, of []model.Gate) (map[string]int, []string) {
	listed := map[string]int{}
	var order []string
	for _, e := range record.Events {
		m := listedFindings().FindStringSubmatch(e.Message)
		if e.Type != model.EventFindings || m == nil || !slices.Contains(of, model.Gate(m[1])) {
			continue
		}
		key := m[1] + " " + m[2]
		if _, seen := listed[key]; !seen {
			order = append(order, key)
		}
		n, _ := strconv.Atoi(m[3])
		listed[key] = max(listed[key], n)
	}
	return listed, order
}

// refuseDroppedFindings refuses a record that holds fewer findings for a
// reviewer than its reviews listed: an sdlc that predates them saved it. Read
// as no findings, that record would pass a gate an open finding should stop.
func refuseDroppedFindings(gate model.Gate, record *model.Record, of []model.Gate, held []heldFinding) error {
	listed, order := listedCounts(record, of)
	holds := map[string]int{}
	for _, h := range held {
		holds[string(h.gate)+" "+h.role]++
	}
	var lost []string
	for _, key := range order {
		if holds[key] < listed[key] {
			lost = append(lost, fmt.Sprintf("%s (listed %d, the record holds %d)", oneLine(key), listed[key], holds[key]))
		}
	}
	if len(lost) == 0 {
		return nil
	}
	return sdlcerr.New(sdlcerr.FindingsDropped,
		string(gate)+" cannot pass: findings the reviews listed are gone from the record",
		"an older sdlc saved the record, and dropped the findings of "+strings.Join(lost, ", "))
}

// requireClosedFindings holds a gate to every finding of the gates named being
// closed.
func requireClosedFindings(gate model.Gate, of []model.Gate, held []heldFinding) error {
	var open []string
	for _, h := range held {
		if len(h.Closings) == 0 {
			open = append(open, h.name())
		}
	}
	if len(open) == 0 {
		return nil
	}
	names := make([]string, len(of))
	for i, g := range of {
		names[i] = string(g)
	}
	return sdlcerr.New(sdlcerr.FindingsOpen,
		string(gate)+" cannot pass while a finding of "+strings.Join(names, " or ")+" is open",
		"a finding nobody has closed is a decision nobody has made; to close one that is fixed: "+
			"sdlc finding close "+open[0]+" --fixed; open: "+strings.Join(open, ", ")+"; sdlc status shows each with its text")
}

// refuseStandingReferrals asks again of every referral whether the story now
// changes its file. Asked only at the closing, a finding referred before Gate 5
// or Gate 7 rework wrote its file left the story by referral, though the story
// had made it a defect in a file it changes.
func refuseStandingReferrals(ctx context.Context, rd *reading, gate model.Gate, record *model.Record, held []heldFinding) error {
	var stands, names []string
	for _, h := range held {
		n := len(h.Closings)
		if n == 0 || h.Closings[n-1].As != model.ClosedReferred || h.Path == "" {
			continue
		}
		changed, ok, err := changedUnder(ctx, rd, record, h.Path)
		if err != nil {
			return err
		}
		if ok && changed == "" {
			continue
		}
		cause := "the story's commits cannot be read"
		if ok {
			cause = "the story has since changed " + quote(changed)
		}
		stands = append(stands, h.name()+" was referred, and "+cause)
		names = append(names, h.name())
	}
	if len(stands) == 0 {
		return nil
	}
	return sdlcerr.New(sdlcerr.ReferredInTheStory,
		string(gate)+" cannot pass while a finding in a file the story changes stands referred",
		strings.Join(stands, "; ")+"; to close one that is fixed: sdlc finding close "+names[0]+" --fixed")
}

// findingInfo is one finding as status and the log show it. Subject is the tree
// the review that raised it was given, so that a reader can tell whether the
// tree changed after it.
type findingInfo struct {
	Gate       string          `json:"gate"`
	Role       string          `json:"role"`
	Round      int             `json:"round"`
	Subject    string          `json:"subject,omitempty"`
	ID         string          `json:"id"`
	Importance string          `json:"importance"`
	Path       string          `json:"path,omitempty"`
	Text       string          `json:"text"`
	State      string          `json:"state"`
	Reason     string          `json:"reason,omitempty"`
	Closings   []model.Closing `json:"closings,omitempty"`
}

const findingOpen = "open"

// findingsOf is every finding on the record, in the order the reviews were
// recorded, as its latest closing leaves it.
func findingsOf(record *model.Record) []findingInfo {
	var out []findingInfo
	for _, rev := range record.Reviews {
		for _, f := range rev.Findings {
			i := findingInfo{Gate: string(rev.Gate), Role: rev.Role, Round: rev.Round, Subject: rev.Subject, ID: f.ID,
				Importance: f.Importance, Path: f.Path, Text: f.Text, State: findingOpen, Closings: f.Closings}
			if n := len(f.Closings); n > 0 {
				i.State, i.Reason = f.Closings[n-1].As, f.Closings[n-1].Reason
			}
			out = append(out, i)
		}
	}
	return out
}

// printFinding is one finding on a line of its own, under label. Everything from
// the record is flattened onto the line: none of it starts one.
func printFinding(w io.Writer, label string, i findingInfo) {
	where := ""
	if i.Path != "" {
		where = ", " + oneLine(i.Path)
	}
	line := fmt.Sprintf("  %-9s  %s %s %s %s, %s%s: %s", label, oneLine(i.Gate), oneLine(i.Role), oneLine(i.ID),
		oneLine(i.State), oneLine(i.Importance), where, oneLine(i.Text))
	if i.Reason != "" {
		line += "; reason: " + oneLine(i.Reason)
	}
	if n := len(i.Closings); n > 1 {
		line += fmt.Sprintf(" (closed %d times)", n)
	}
	fmt.Fprintln(w, line)
}

// printHeldFindings is the open findings of the gates they hold, with how the
// first is closed directly under them, and then the declined ones. A decline's
// reason is in status --json and the log: a line here holds only a referral's.
func printHeldFindings(w io.Writer, findings []findingInfo) {
	var held []findingInfo
	for _, f := range findings {
		if model.Gate(f.Gate).HoldsFindings() {
			held = append(held, f)
		}
	}
	first := ""
	for _, f := range held {
		if f.State != findingOpen {
			continue
		}
		if first == "" {
			first = oneLine(f.Gate) + " " + oneLine(f.Role) + " " + oneLine(f.ID)
		}
		printFinding(w, "finding", f)
	}
	if first != "" {
		fmt.Fprintln(w, "  to close   sdlc finding close "+first+` --fixed once it is fixed, or --declined --reason "<why>"`)
	}
	for _, f := range held {
		if f.State == model.ClosedDeclined {
			f.Reason = ""
			printFinding(w, model.ClosedDeclined, f)
		}
	}
}
