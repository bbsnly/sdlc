package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

type reviewPayload struct {
	OK        bool   `json:"ok"`
	Story     string `json:"story"`
	Gate      string `json:"gate"`
	Role      string `json:"role"`
	Verdict   string `json:"verdict"`
	Round     int    `json:"round"`
	Path      string `json:"path"`
	Subject   string `json:"subject,omitempty"`
	Important *int   `json:"important,omitempty"`
	Nits      *int   `json:"nits,omitempty"`
	// HandedOver is the kind of escalation, when this block was one too many
	// and the story went to a person.
	HandedOver string `json:"handed_over,omitempty"`
}

type reviewListPayload struct {
	OK         bool         `json:"ok"`
	Story      string       `json:"story"`
	PolicyFile string       `json:"policy"`
	Reviews    []reviewInfo `json:"reviews"`
}

// reviewInfo is one expected review and where it has got to. Stale is the field
// that matters: an approval of a plan that has since changed is not an
// approval, and nothing else in the output would say so.
type reviewInfo struct {
	Gate     string `json:"gate"`
	Role     string `json:"role"`
	Blocking bool   `json:"blocking"`
	// BlocksOnBreach is a reviewer that need not approve, and whose block
	// still stops the gate: perf, on a stated performance budget.
	BlocksOnBreach bool   `json:"blocks_on_breach,omitempty"`
	Verdict        string `json:"verdict,omitempty"`
	Round          int    `json:"round,omitempty"`
	Stale          bool   `json:"stale,omitempty"`
	// ReviewedSubject is the kept revision of the plan a stale design review was
	// stamped with, where its subject could name one: what a reviewer asked to
	// look again diffs from.
	ReviewedSubject string `json:"reviewed_subject,omitempty"`
	// LeftOut is a reviewer the story's staffing says it does not need, which
	// the gate does not wait for; Reason is the staffing's reason. BroughtBackBy
	// is the changed path that has the code review wait for it all the same.
	LeftOut       bool   `json:"left_out,omitempty"`
	Reason        string `json:"reason,omitempty"`
	BroughtBackBy string `json:"brought_back_by,omitempty"`
	// Disabled is a reviewer the project disables in team.agents, which the
	// gate does not wait for, bar the verifier and the code reviewer, whose
	// review a person then records.
	Disabled bool `json:"disabled,omitempty"`
}

func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Record what a reviewer concluded, and about what",
		Long: "A gate that is reviewed does not pass because an agent said it should. It\n" +
			"passes because every reviewer it expects has reported, the blocking ones\n" +
			"approved, and what they approved is still what is there.\n\n" +
			"That last part is why reviews go through this command: it stamps each one\n" +
			"with the thing that was reviewed, so a later change makes the approval\n" +
			"stale instead of silently standing.",
		Example: "  sdlc review list\n  sdlc review add design_review architect approve < review.md",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newReviewAddCmd(), newReviewListCmd())
	return cmd
}

func newReviewAddCmd() *cobra.Command {
	var storyID, file, note string
	var important, nits int
	var findings []string
	cmd := &cobra.Command{
		Use:   "add GATE ROLE VERDICT",
		Short: "Record one reviewer's conclusion",
		Long: "add stores a reviewer's document under the story and records the verdict\n" +
			"against what was in front of them.\n\n" +
			"Verdicts: " + model.VerdictNames() + ". A blocking reviewer's approve is what\n" +
			"lets the gate pass; a note is a finding worth recording that does not stand\n" +
			"in the way.\n\n" +
			"A block names at least one important finding, counted with --important: a\n" +
			"review with only nits is an approve. The counts are the reviewer's own; at\n" +
			"design_review and code_review, --finding lists what they count, one flag\n" +
			"for each, numbered F-1, F-2 and so on across the reviewer's rounds. A path\n" +
			"with a space in it is read as text. Keep $, backticks and double quotes out\n" +
			"of a finding and a note: the shell expands them.\n\n" +
			"The document comes from standard input, or from --file.",
		Example: "  sdlc review add design_review architect block --important 1 \\\n" +
			"      --finding \"important the plan: AC-3 has no step\" < review.md\n" +
			"  sdlc review add code_review code-reviewer approve --nits 1 --file review.md \\\n" +
			"      --finding \"nit internal/cli/review.go:42: the name says nothing\"\n" +
			"  sdlc review add code_review perf note --file /tmp/perf.md",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewAdd(cmd, args, reviewFlags{story: storyID, file: file, note: note,
				important: givenCount(cmd, flagImportant, important), nits: givenCount(cmd, flagNits, nits), findings: findings})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the review from this path instead of standard input")
	cmd.Flags().StringVar(&note, "note", "", "the headline finding, in one line")
	cmd.Flags().StringVar(&storyID, "story", "", "record against this story instead of the one being worked on")
	cmd.Flags().IntVar(&important, flagImportant, 0, "how many important findings this review names")
	cmd.Flags().IntVar(&nits, flagNits, 0, "how many nits this review found")
	// An array, not a slice: a slice flag splits its value at commas, and a
	// finding's text has them.
	cmd.Flags().StringArrayVar(&findings, flagFinding, nil,
		`one finding, as "<important|nit> [path]: <text>"; once for each finding the review counts, and left out at 0`)
	return cmd
}

const (
	flagImportant = "important"
	flagNits      = "nits"
)

// givenCount is n where the flag was given and nil where it was not, which is not
// the same as 0.
func givenCount(cmd *cobra.Command, flag string, n int) *int {
	if !cmd.Flags().Changed(flag) {
		return nil
	}
	return &n
}

// maxReviewCount is the most a review may count: the log takes a higher number
// read from a record as nonsense, so the flag refuses it rather than record what
// is then never shown.
const maxReviewCount = 1_000_000

// usableCount is n, or nil for what no review counts, read from a record that
// could say anything.
func usableCount(n *int) *int {
	if n == nil || *n < 0 || *n > maxReviewCount {
		return nil
	}
	return n
}

func countOutOfRange(n int) string {
	if n < 0 {
		return "a count is a number of findings, so it cannot be negative"
	}
	return "no review counts more than 1,000,000 findings, and a higher count is taken as a mistake"
}

// refuseCounts holds a review's counts up before anything is read or recorded.
func refuseCounts(verdict model.Verdict, f reviewFlags) error {
	for _, c := range []struct {
		flag string
		n    *int
	}{{flagImportant, f.important}, {flagNits, f.nits}} {
		if c.n != nil && usableCount(c.n) == nil {
			return sdlcerr.New(sdlcerr.BadArgument, "--"+c.flag+" is "+strconv.Itoa(*c.n), countOutOfRange(*c.n))
		}
	}
	if verdict == model.VerdictBlock && (f.important == nil || *f.important == 0) {
		return sdlcerr.New(sdlcerr.BlockWithoutImportant,
			"a block has to name at least one important finding under the review policy",
			"a review with only nits is an approve with the nits listed")
	}
	return nil
}

// reviewFlags are the flags `sdlc review add` was given.
type reviewFlags struct {
	story, file, note string
	important, nits   *int
	findings          []string
}

// readReviewArgs reads what the command line says of the review, before
// anything is read from the store: the gate, the reviewer, the verdict, and the
// findings the counts list.
func readReviewArgs(args []string, f reviewFlags) (model.Gate, model.Reviewer, model.Verdict, []model.Finding, error) {
	gate := model.Gate(args[0])
	if !gate.Valid() {
		return "", model.Reviewer{}, "", nil, sdlcerr.New(sdlcerr.UnknownGate,
			"there is no gate called "+quote(args[0]),
			"this version knows "+model.GateNames())
	}
	reviewer, ok := model.FindReviewer(gate, args[1])
	if !ok {
		return "", model.Reviewer{}, "", nil, sdlcerr.New(sdlcerr.UnknownReviewer,
			string(gate)+" does not expect a review from "+quote(args[1]),
			"it expects "+model.ReviewerNames(gate))
	}
	verdict := model.Verdict(strings.ToLower(strings.TrimSpace(args[2])))
	if !verdict.Valid() {
		return "", model.Reviewer{}, "", nil, sdlcerr.New(sdlcerr.UnknownVerdict,
			quote(args[2])+" is not a verdict",
			"a reviewer approves, blocks, or leaves a note")
	}
	if err := refuseCounts(verdict, f); err != nil {
		return "", model.Reviewer{}, "", nil, err
	}
	findings, err := readFindings(gate, f)
	if err != nil {
		return "", model.Reviewer{}, "", nil, err
	}
	return gate, reviewer, verdict, findings, nil
}

func runReviewAdd(cmd *cobra.Command, args []string, f reviewFlags) error {
	gate, reviewer, verdict, findings, err := readReviewArgs(args, f)
	if err != nil {
		return err
	}

	// Read before the lock is taken, as artifact write does: every
	// other writer would wait on standard input otherwise.
	content, err := readDocument(cmd, f.file)
	if err != nil {
		return err
	}

	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	id, err := reviewedStory(s, f.story)
	if err != nil {
		return err
	}
	if err := refuseIfHeldElsewhere(s, id, "a review is of the work in that checkout, not this one"); err != nil {
		return err
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	// Said whether or not the review is then refused; recorded only with the
	// review, below.
	late := gateHoursOver(s.Config().Budget, record)
	late.say(cmd.ErrOrStderr())
	if !reviewer.Expected(reviewPolicy(s, record)) {
		return sdlcerr.New(sdlcerr.UnknownReviewer,
			string(gate)+" does not expect a review from "+quote(reviewer.Role)+" in this project",
			"it does only when human_gates.dor_advocate_check is on in .sdlc/config.json")
	}
	if err := refuseUnwaited(cmd.Context(), s, record, gate, reviewer.Role); err != nil {
		return err
	}
	subject, err := gateSubject(cmd.Context(), readingOf(s), id, gate)
	if err != nil {
		return err
	}

	// The document is written with the record and not before it: a
	// block whose hand-over is refused leaves no file the record does
	// not list, and the round it would have been is still to come.
	doc := &store.ReviewDocument{
		Gate: gate, Role: reviewer.Role, Round: record.Round(gate, reviewer.Role) + 1, Content: content,
	}
	path := store.ReviewPath(id, gate, reviewer.Role, doc.Round)
	rev := record.AddReview(model.Review{
		Gate: gate, Role: reviewer.Role, Verdict: verdict,
		Subject: subject, File: path, Note: f.note, Important: f.important, Nits: f.nits, Findings: findings,
	}, s.Now())
	record.Append(model.EventReview, model.ReviewEvent(gate, reviewer.Role, verdict), s.Now())
	noteListedFindings(record, rev, s.Now())
	late.record(record, s.Now())
	handedOver, message, err := saveReview(cmd, s, record, reviewer, doc, verdict, f.note)
	if err != nil {
		return err
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), reviewPayload{
			OK: true, Story: id, Gate: string(gate), Role: reviewer.Role,
			Verdict: string(verdict), Round: rev.Round, Path: path, Subject: subject,
			HandedOver: handedOver, Important: f.important, Nits: f.nits,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s (round %d)\n  %s\n",
		id, gate, reviewer.Role, verdict, rev.Round, path)
	if handedOver != "" {
		sayHandedOver(cmd.OutOrStdout(), id, message)
	}
	return nil
}

// refuseUnwaited refuses a review the gate does not wait for, before anything
// is recorded or counted: a reviewer the gate does not wait for cannot block
// it, or hand the story over by blocking. Each case says only itself, since
// the fixes differ: a disabled agent is the file's, a reviewer not picked the
// project manager's, and an instance past instances the file's other setting.
func refuseUnwaited(ctx context.Context, s *store.Store, record *model.Record, gate model.Gate, name string) error {
	team := s.Config().Team
	base := model.BaseRole(name)
	what := string(gate) + " does not wait for a review from " + quote(name)
	if _, n, ok := model.InstanceOf(name); ok && team.Enabled(base) && n > team.Seats(base) {
		return sdlcerr.New(sdlcerr.UnknownReviewer, what,
			fmt.Sprintf("%s is past team.agents.%s.instances, which gives %s %d", name, base, base, team.Seats(base)))
	}
	out, _, err := waited(ctx, readingOf(s), record, gate, name)
	switch {
	case err != nil:
		return err
	case !out:
		return nil
	case !team.Enabled(base):
		return sdlcerr.New(sdlcerr.UnknownReviewer, what,
			base+" is disabled in team.agents."+base+".enabled, so it takes no part in any story")
	}
	return sdlcerr.New(sdlcerr.UnknownReviewer, what,
		"the staffing left "+name+" out of this story; delegate to sdlc:project-manager to amend the staffing if the story needs it")
}

// reviewedStory is the story a review is recorded against: the one named, or
// the one being worked on.
func reviewedStory(s *store.Store, named string) (string, error) {
	if named == "" {
		return activeStory(s, "a review belongs to the story being worked on")
	}
	// As gate --story does: a mistyped id is refused, not given a
	// review that no gate will ever read.
	if _, _, err := s.Story(named); err != nil {
		return "", err
	}
	return named, nil
}

// saveReview saves the record with the review on it. A block that cannot stop
// the gate is a finding, not a round the rework lost, so only a block that
// stops it counts towards handing the story over.
func saveReview(cmd *cobra.Command, s *store.Store, record *model.Record, reviewer model.Reviewer,
	doc *store.ReviewDocument, verdict model.Verdict, note string,
) (handedOver, message string, err error) {
	if !reviewer.Stops(reviewPolicy(s, record), verdict) {
		return "", "", saveRecorded(s, record, doc)
	}
	gate := doc.Gate
	// The agent's own limit where it has one, counted for each name, so an
	// instance counts its own blocks under its role's setting.
	limit, setting := s.Config().Loop.MaxReviewRounds, "loop.max_review_rounds"
	if own, ok := s.Config().Team.MaxRounds(reviewer.Role); ok {
		limit, setting = own, "team.agents."+model.BaseRole(reviewer.Role)+".max_rounds"
	}
	blocks := record.SinceDecision(model.EventReview,
		model.ReviewEvent(gate, reviewer.Role, model.VerdictBlock))
	message = fmt.Sprintf("%s has blocked %s %d times, and %s is %d, "+
		"so the rework is not converging on what it asks for. The last note: %s",
		reviewer.Role, gate, blocks, setting, limit, lastNote(note))
	handedOver, err = handOverAt(cmd, s, record, doc, limitCount{
		count: blocks, limit: limit, kind: "review_not_converging", message: message, what: "the block",
	})
	return handedOver, message, err
}

func newReviewListCmd() *cobra.Command {
	var storyID, gateName string
	cmd := &cobra.Command{
		Use:     verbList,
		Short:   "Show which reviews each gate expects and where they have got to",
		Example: "  sdlc review list\n  sdlc review list --gate code_review",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runReviewList(cmd, storyID, gateName) },
	}
	cmd.Flags().StringVar(&gateName, "gate", "", "only this gate's reviews")
	cmd.Flags().StringVar(&storyID, "story", "", "for this story instead of the one being worked on")
	return cmd
}

func runReviewList(cmd *cobra.Command, storyID, gateName string) error {
	var only model.Gate
	if gateName != "" {
		only = model.Gate(gateName)
		if !only.Valid() {
			return sdlcerr.New(sdlcerr.UnknownGate,
				"there is no gate called "+quote(gateName),
				"this version knows "+model.GateNames())
		}
	}

	s, _, err := openStore(cmd)
	if err != nil {
		return err
	}
	id := storyID
	if id == "" {
		if id, err = activeStory(s, "reviews belong to the story being worked on"); err != nil {
			return err
		}
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}

	infos, err := describeReviewers(cmd.Context(), s, record, id, only)
	if err != nil {
		return err
	}

	policyFile := policyFileIn(s.Config(), s.Root())
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), reviewListPayload{OK: true, Story: id, PolicyFile: policyFile, Reviews: infos})
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, describePolicyFile(s.Config(), policyFile))
	for _, i := range infos {
		fmt.Fprintf(w, "  %-15s %-15s %-9s %s\n", i.Gate, i.Role,
			describeReviewPower(i), describeReviewState(i))
	}
	return nil
}

// policyFileIn is the review policy's path as written, or "" where there is no
// policy at root: nothing there, not a regular file, or a link that leads out of
// the repository, whose text a reviewer would copy into a committed review.
func policyFileIn(cfg config.Config, root string) string {
	p := cfg.ReviewPolicyPath()
	abs := filepath.Join(root, filepath.FromSlash(p))
	if _, outside := pathrules.NewResolver(root).Rel(abs); outside {
		return ""
	}
	if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return p
}

// describePolicyFile is the first line of `sdlc review list`. A mistyped
// reviews.policy shows here, where every reviewer looks.
func describePolicyFile(cfg config.Config, p string) string {
	if p != "" {
		return "Review policy: " + oneLine(p)
	}
	if cfg.Reviews.Policy != "" {
		return "This project has no review policy: reviews.policy names " + oneLine(cfg.Reviews.Policy) +
			", which is not a file in the repository."
	}
	return "This project has no review policy."
}

// describeReviewers is where each expected reviewer's review stands, for one
// gate or for all of them, each instance the team gives a reviewer included.
func describeReviewers(ctx context.Context, s *store.Store, record *model.Record, id string, only model.Gate) ([]reviewInfo, error) {
	infos := make([]reviewInfo, 0, len(model.Reviewers))
	policy := reviewPolicy(s, record)
	// One tree for every reviewer listed: they were all stamped
	// against the same one.
	rd := readingOf(s)
	team := s.Config().Team
	for _, gate := range model.Gates {
		if only != "" && gate != only {
			continue
		}
		for _, r := range team.ReviewersFor(gate) {
			if !r.Expected(policy) {
				continue
			}
			info, err := describeReviewer(ctx, rd, record, id, r, policy)
			if err != nil {
				return nil, err
			}
			info.Disabled = !team.Enabled(r.Role)
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// describeReviewer is where one expected reviewer's review of the story stands.
func describeReviewer(ctx context.Context, rd *reading, record *model.Record, id string,
	r model.Reviewer, policy model.ReviewPolicy,
) (reviewInfo, error) {
	subject, err := gateSubject(ctx, rd, id, r.Gate)
	if err != nil {
		return reviewInfo{}, err
	}
	info := reviewInfo{
		Gate: string(r.Gate), Role: r.Role,
		Blocking:       r.Blocks(policy),
		BlocksOnBreach: !r.Blocks(policy) && r.OnBreach(policy),
	}
	if latest, ok := record.LatestReview(r.Gate, r.Role); ok {
		info.Verdict, info.Round = string(latest.Verdict), latest.Round
		info.Stale = latest.Subject != subject
		if info.Stale && r.Gate == model.GateDesignReview && store.IsPlanRevision(latest.Subject) {
			info.ReviewedSubject = latest.Subject
		}
	}
	if info.LeftOut, info.BroughtBackBy, err = waited(ctx, rd, record, r.Gate, r.Role); err != nil {
		return reviewInfo{}, err
	}
	if info.LeftOut || info.BroughtBackBy != "" {
		row, _ := staffingRow(record, r.Role)
		info.Reason = row.Reason
	}
	return info, nil
}

func describeReviewPower(i reviewInfo) string {
	switch {
	case i.Blocking:
		return "blocking"
	case i.BlocksOnBreach && model.BaseRole(i.Role) == "perf":
		return "on budget"
	case i.BlocksOnBreach:
		return "can block"
	default:
		return "advisory"
	}
}

func describeReviewState(i reviewInfo) string {
	// A verdict is read back from the record, which a clone can bring.
	verdict := oneLine(i.Verdict)
	if by := i.BroughtBackBy; by != "" {
		i.BroughtBackBy = ""
		return describeReviewState(i) + " (brought back by " + quote(by) + ")"
	}
	base := model.BaseRole(i.Role)
	switch {
	case i.Disabled && i.LeftOut:
		return "disabled in team.agents." + base + ".enabled; a person enables it there if the story needs it"
	case i.Disabled:
		i.Disabled = false
		return describeReviewState(i) + " (disabled in team.agents." + base + ".enabled: a person records this review by hand)"
	case i.LeftOut:
		// The reason is a model's, and is printed quoted so that it cannot end
		// the line and start one that reads as the tool's.
		return "left out by the staffing -- " + quote(i.Reason) +
			"; delegate to sdlc:project-manager to amend the staffing if the story needs it"
	case i.Verdict == "":
		return "not reviewed"
	case i.ReviewedSubject != "":
		// The role is the reviewers' table's, and the subject has passed the check
		// that it is a hash: neither is free text from the record.
		return verdict + " (round " + strconv.Itoa(i.Round) + ", stale -- what was reviewed has changed since; reviewed revision " +
			i.ReviewedSubject + "; see sdlc artifact diff plan --since " + i.Role + ")"
	case i.Stale:
		return verdict + " (round " + strconv.Itoa(i.Round) + ", stale -- what was reviewed has changed since)"
	default:
		return verdict + " (round " + strconv.Itoa(i.Round) + ")"
	}
}

// gateSubject is what a reviewer of this gate is looking at, by content.
//
// The definition-of-ready gate reviews the story, and the design gate a
// document, so their content is the subject. The gates that review work review
// the whole tree, because a change anywhere can invalidate what they concluded.
// A gate with no reviewers has no subject, and that is not an error.
func gateSubject(ctx context.Context, rd *reading, id string, gate model.Gate) (string, error) {
	s := rd.s
	switch gate {
	case model.GateDoR:
		return s.StorySubject(id)
	case model.GateDesignReview:
		// No plan yet is a state, not a failure. It leaves the subject empty,
		// which makes every review of a plan that is not there stale by
		// definition -- exactly the right answer.
		plan, ok := model.FindArtifact("plan")
		if !ok || !s.ArtifactStored(id, plan) {
			return "", nil
		}
		return s.HashArtifact(id, plan)
	case model.GateVerifierReview, model.GateCodeReview:
		return rd.subject(ctx)
	default:
		return "", nil
	}
}

// reviewPolicy is who is expected and who can block, for this story in this
// project: the team's can_block. It is not the review policy, the file every
// reviewer reads, which policyFileIn names; no string a person reads calls
// this that.
func reviewPolicy(s *store.Store, record *model.Record) model.ReviewPolicy {
	return model.ReviewPolicy{
		SecuritySensitive: record.SecuritySensitive(),
		CanBlock:          s.Config().Team.CanBlock(),
		DoRAdvocate:       s.Config().HumanGates.DoRAdvocateCheck,
	}
}

// requireReviews holds a reviewed gate to what being reviewed means.
func requireReviews(ctx context.Context, rd *reading, id string, gate model.Gate, record *model.Record) error {
	policy := reviewPolicy(rd.s, record)
	reviewers, back, err := expectedReviewers(ctx, rd, record, gate, policy)
	if err != nil {
		return err
	}
	if len(reviewers) == 0 {
		return nil
	}
	subject, err := gateSubject(ctx, rd, id, gate)
	if err != nil {
		return err
	}

	var outstanding, blocked []string
	for _, r := range reviewers {
		blocking := r.Blocks(policy)
		latest, ok := record.LatestReview(gate, r.Role)
		switch {
		case !ok:
			outstanding = append(outstanding, r.Role+" has not reviewed"+back[r.Role])
		case latest.Subject != subject:
			outstanding = append(outstanding, r.Role+" reviewed something that has changed since"+back[r.Role])
		case r.Stops(policy, latest.Verdict):
			blocked = append(blocked, r.Role+" blocked"+describeNote(latest.Note))
		case blocking && latest.Verdict != model.VerdictApprove:
			outstanding = append(outstanding, r.Role+" has not approved")
		}
	}
	if len(blocked) > 0 {
		return sdlcerr.New(sdlcerr.ReviewBlocks,
			string(gate)+" is blocked by a reviewer",
			strings.Join(blocked, "; "))
	}
	if len(outstanding) > 0 {
		return sdlcerr.New(sdlcerr.ReviewsMissing,
			string(gate)+" cannot pass until its reviews are in",
			strings.Join(outstanding, "; "))
	}
	return nil
}

// expectedReviewers is who the gate waits for, and for each one brought back
// by a changed path, the words that say so.
func expectedReviewers(ctx context.Context, rd *reading, record *model.Record, gate model.Gate,
	policy model.ReviewPolicy,
) ([]model.Reviewer, map[string]string, error) {
	var reviewers []model.Reviewer
	back := map[string]string{}
	for _, r := range rd.s.Config().Team.ReviewersFor(gate) {
		out, by, err := waited(ctx, rd, record, gate, r.Role)
		if err != nil {
			return nil, nil, err
		}
		if r.Expected(policy) && !out {
			reviewers = append(reviewers, r)
			if by != "" {
				back[r.Role] = " (the staffing leaves it out, and it is brought back by " + quote(by) + ")"
			}
		}
	}
	return reviewers, back, nil
}

func describeNote(note string) string {
	if strings.TrimSpace(note) == "" {
		return ""
	}
	return ": " + note
}
