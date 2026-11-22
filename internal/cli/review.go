package cli

import (
	"cmp"
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
	// Arbiter is the <role>-arbiter the gate goes to, on the reviewer's
	// limit-th stopping block (US-131 AC-6).
	Arbiter string `json:"arbiter,omitempty"`
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
	// ReviewedBy is the arbiter whose review stands for the reviewer, where
	// one does: the verdict and the round are then the arbiter's.
	ReviewedBy string `json:"reviewed_by,omitempty"`
	// GoneTo is the arbiter the gate has gone to while the reviewer's own
	// review still stands, from its limit-th block to the arbiter's first
	// review: the row showed only the refused reviewer's block (verifier F-9).
	GoneTo string `json:"gone_to,omitempty"`
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
	if err := refuseOutOfTurn(s, record, gate, reviewer.Role); err != nil {
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
	saved, err := saveReview(cmd, s, record, reviewer, doc, verdict, f.note)
	if err != nil {
		return err
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), reviewPayload{
			OK: true, Story: id, Gate: string(gate), Role: reviewer.Role,
			Verdict: string(verdict), Round: rev.Round, Path: path, Subject: subject,
			HandedOver: saved.handedOver, Arbiter: saved.arbiter, Important: f.important, Nits: f.nits,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s (round %d)\n  %s\n",
		id, gate, reviewer.Role, verdict, rev.Round, path)
	switch {
	case saved.handedOver != "":
		sayHandedOver(cmd.OutOrStdout(), id, saved.message)
	case saved.arbiter != "":
		for line := range strings.SplitSeq(saved.message, "\n") {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", line)
		}
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
	// An arbiter sits in its reviewer's seat.
	seat := name
	if reviewer, ok := model.ArbiterOf(name); ok {
		seat = reviewer
	}
	if _, n, ok := model.InstanceOf(seat); ok && team.Enabled(base) && n > team.Seats(base) {
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

// refuseOutOfTurn refuses whichever of a reviewer and its arbiter the gate is
// not with, before anything is recorded (US-131 AC-6, T-1 of the threats). An
// arbiter reviews only from the block that reaches its reviewer's limit, or
// while its own review stands, so its name cannot dodge a blocking review. The
// reviewer is refused from that block on: the runbook could otherwise send the
// work back to it until it approved, and pass the gate with neither the arbiter
// nor a person (security F-1 of the design review). One of the two is always
// taken, so nothing deadlocks.
func refuseOutOfTurn(s *store.Store, record *model.Record, gate model.Gate, name string) error {
	role, isArbiter := model.ArbiterOf(name)
	if !isArbiter {
		role = name
	}
	reviewer, ok := model.FindReviewer(gate, role)
	if !ok {
		return nil
	}
	arbiter := model.ArbiterName(role)
	due, blocks, limit, setting := arbiterDue(s, record, reviewer)
	stands := arbiterStands(record, gate, role)
	switch {
	case isArbiter && !stands && !s.Config().Team.Enabled(reviewer.Role):
		// A disabled verifier or code reviewer is still waited for, so its
		// arbiter's name gets this far; its block does stop the gate, by a
		// hand-over, and nobody is delegated to (code-reviewer F-2). An arbiter
		// whose review already stands keeps its turn: a role disabled after
		// that had both names refused, and the gate taking neither
		// (security F-1 of the code review, verifier F-3 of the verifier's review).
		base := model.BaseRole(role)
		return sdlcerr.New(sdlcerr.UnknownReviewer,
			name+" does not review "+string(gate),
			role+" is disabled in team.agents."+base+".enabled, so it has no arbiter: its limit-th block hands the story to a person").
			WithFix("a person records " + role + "'s review by hand, under its own name")
	case isArbiter && !due && !stands && !arbitrated(s, record, reviewer):
		// Without this the refusal of an advisory reviewer's arbiter read as
		// a count not yet reached, though the count had reached the limit.
		return sdlcerr.New(sdlcerr.UnknownReviewer,
			name+" has nothing to rule on at "+string(gate),
			role+"'s block does not stop "+string(gate)+" here, so no number of them sends the gate to an arbiter").
			WithFix("delegate to sdlc:" + model.BaseRole(role) + " as " + role + " for the reviewer's own review")
	case isArbiter && !due && !stands:
		return sdlcerr.New(sdlcerr.UnknownReviewer,
			name+" reviews "+string(gate)+" only from the block that reaches "+role+"'s limit since a person last answered",
			fmt.Sprintf("%s is %d, and %s %s", setting, limit, role, blockedIt(blocks))).
			WithFix("delegate to sdlc:" + model.BaseRole(role) + " as " + role + " for the reviewer's own review")
	case isArbiter || !due && !stands:
		return nil
	}
	why := fmt.Sprintf("%s %s since a person last answered, and %s is %d", role, blockedIt(blocks), setting, limit)
	if stands {
		why = arbiter + "'s review stands for " + role + " at " + string(gate)
	}
	return sdlcerr.New(sdlcerr.UnknownReviewer, string(gate)+" has gone to "+arbiter, why).
		WithFix("delegate to a fresh sdlc:" + model.BaseRole(role) + " as " + arbiter + ", asked for what sdlc staffing show --gate " +
			string(gate) + " gives it; " + turnBack(role, gate, stands))
}

// blockedIt is a reviewer's count of stopping blocks in words: the refusal of
// an arbiter not yet due is most often read at 0 or 1, where "1 times" read
// badly (human-advocate F-6).
func blockedIt(n int) string {
	switch n {
	case 0:
		return "has not blocked it yet"
	case 1:
		return "has blocked it once"
	}
	return "has blocked it " + strconv.Itoa(n) + " times"
}

// raisedAt names the one hand-over whose answer gives a reviewer its turn back
// once its arbiter has reviewed. An answer to another role's at the same gate
// does not, so the words name the role (human-advocate F-15 of the design
// review).
func raisedAt(role string, gate model.Gate) string {
	return "a review_not_converging hand-over raised by " + role + " or " + model.ArbiterName(role) + " at " + string(gate)
}

// turnBack says what gives a reviewer its turn back at a gate that has gone to
// its arbiter. Until the arbiter's first review the reviewer's blocks are
// counted since a person last answered, so any answer starts the count again;
// from that review only an answer to the role's own hand-over ends its
// standing. The words once named only the second, and a runbook that trusted
// them delegated to an arbiter that was then refused (verifier F-1).
func turnBack(role string, gate model.Gate, stands bool) string {
	own := "a person's answer to " + raisedAt(role, gate) + " gives " + role + " its turn again"
	if stands {
		return "only " + own
	}
	arbiter := model.ArbiterName(role)
	return "until " + arbiter + " first reviews, any answer a person gives the story starts " + role +
		"'s count again and gives it its turn, and " + arbiter + " waits for the next limit; from that review, only " + own
}

// reviewLimit is the most stopping blocks a reviewer gives a gate: the agent's
// own max_rounds where it has one, counted for each name, so that an instance
// and an arbiter count their own blocks under the role's setting; else
// loop.max_review_rounds. Zero is no limit.
func reviewLimit(cfg config.Config, role string) (limit int, setting string) {
	if own, ok := cfg.Team.MaxRounds(role); ok {
		return own, "team.agents." + model.BaseRole(role) + ".max_rounds"
	}
	return cfg.Loop.MaxReviewRounds, "loop.max_review_rounds"
}

// arbiterDue reports whether a reviewer's stopping blocks of its gate since a
// person last answered have reached its limit, so that the gate goes to its
// arbiter, with the count, the limit and the setting it came from. A disabled
// role has no arbiter, and hands over at its reviewer's limit as before.
func arbiterDue(s *store.Store, record *model.Record, reviewer model.Reviewer) (due bool, blocks, limit int, setting string) {
	limit, setting = reviewLimit(s.Config(), reviewer.Role)
	blocks = record.SinceDecision(model.EventReview, model.ReviewEvent(reviewer.Gate, reviewer.Role, model.VerdictBlock))
	due = arbitrated(s, record, reviewer) && limit > 0 && blocks >= limit
	return due, blocks, limit, setting
}

// arbitrated reports whether a reviewer's blocks can send its gate to an
// arbiter at all: only a block that stops the gate counts, and a disabled role
// has no arbiter.
func arbitrated(s *store.Store, record *model.Record, reviewer model.Reviewer) bool {
	return s.Config().Team.Enabled(reviewer.Role) && reviewer.Stops(reviewPolicy(s, record), model.VerdictBlock)
}

// arbiterStands reports whether the arbiter's review is the one that stands
// for the role at the gate.
func arbiterStands(record *model.Record, gate model.Gate, role string) bool {
	standing, ok := record.StandingReview(gate, role)
	return ok && standing.Role == model.ArbiterName(role)
}

// savedReview is what saving a review led to: the kind of hand-over, or the
// arbiter the gate went to, and what the person or the runbook is told.
type savedReview struct {
	handedOver, arbiter, message string
}

// saveReview saves the record with the review on it. A block that cannot stop
// the gate is a finding, not a round the rework lost, so only a block that
// stops it counts. A reviewer's limit-th sends the gate to its arbiter in the
// same save, and the iteration goes on; the arbiter's own limit-th, or a
// disabled role's reviewer's, hands the story over (US-131 AC-6).
//
// A hand-over is written straight after the review, its findings and any
// budget event, and nothing else: model.Record.answeredReviews finds the block
// that raised it by that, and an event of another kind in between would make a
// person's answer end no arbiter's standing.
func saveReview(cmd *cobra.Command, s *store.Store, record *model.Record, reviewer model.Reviewer,
	doc *store.ReviewDocument, verdict model.Verdict, note string,
) (savedReview, error) {
	if !reviewer.Stops(reviewPolicy(s, record), verdict) {
		return savedReview{}, saveRecorded(s, record, doc)
	}
	gate := doc.Gate
	if _, isArbiter := model.ArbiterOf(reviewer.Role); !isArbiter {
		if due, blocks, limit, setting := arbiterDue(s, record, reviewer); due {
			// One line a sentence: as one line of 600 characters it had to be
			// read three times at the terminal (human-advocate F-17 of the
			// design review).
			role, arbiter := reviewer.Role, model.ArbiterName(reviewer.Role)
			lines := []string{
				fmt.Sprintf("%s has blocked %s %d times, and %s is %d: the gate goes to %s, a fresh sdlc:%s "+
					"that rules on the open findings.", role, gate, blocks, setting, limit, arbiter, model.BaseRole(role)),
				fmt.Sprintf("Its review then stands for %s at %s, and %s is refused there.", role, gate, role),
				fmt.Sprintf("Until %s first reviews, any answer a person gives the story starts %s's count again and gives it its turn.",
					arbiter, role),
				fmt.Sprintf("From then on, only a person's answer to %s gives %s its turn again.", raisedAt(role, gate), role),
			}
			return savedReview{arbiter: arbiter, message: strings.Join(lines, "\n")}, saveRecorded(s, record, doc)
		}
	}
	limit, setting := reviewLimit(s.Config(), reviewer.Role)
	blocks := record.SinceDecision(model.EventReview,
		model.ReviewEvent(gate, reviewer.Role, model.VerdictBlock))
	message := fmt.Sprintf("%s has blocked %s %d times, and %s is %d, "+
		"so the rework is not converging on what it asks for. The last note: %s",
		reviewer.Role, gate, blocks, setting, limit, lastNote(note))
	handedOver, err := handOverAt(cmd, s, record, doc, limitCount{
		count: blocks, limit: limit, kind: model.KindReviewNotConverging, message: message, what: "the block",
	})
	return savedReview{handedOver: handedOver, message: message}, err
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
	info := reviewInfo{
		Gate: string(r.Gate), Role: r.Role,
		Blocking:       r.Blocks(policy),
		BlocksOnBreach: !r.Blocks(policy) && r.OnBreach(policy),
	}
	if latest, ok := record.StandingReview(r.Gate, r.Role); ok {
		// Asked only where there is a review to compare it with: the tests
		// gate's subject hashes every test file.
		subject, err := gateSubject(ctx, rd, id, r.Gate)
		if err != nil {
			return reviewInfo{}, err
		}
		info.Verdict, info.Round = string(latest.Verdict), latest.Round
		if latest.Role != r.Role {
			info.ReviewedBy = latest.Role
		} else if gone(rd.s, record, r) {
			info.GoneTo = model.ArbiterName(r.Role)
		}
		info.Stale = latest.Subject != subject
		if info.Stale && r.Gate == model.GateDesignReview && store.IsPlanRevision(latest.Subject) {
			info.ReviewedSubject = latest.Subject
		}
	}
	var err error
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
	}
	// The review that stands is the arbiter's where ReviewedBy names one: the
	// reviewer's own name with the arbiter's suffix, which StandingReview
	// matched exactly, so no free text from the record. Its revision is the
	// arbiter's, so the diff is asked from the arbiter's review too; from the
	// reviewer's own it began at another plan (code-reviewer F-1).
	round := " (round " + strconv.Itoa(i.Round)
	if i.ReviewedBy != "" {
		round = " (by " + i.ReviewedBy + ", round " + strconv.Itoa(i.Round)
	}
	end := ")"
	if i.GoneTo != "" {
		// Built from the role, as ReviewedBy is: no free text from the record.
		end = "; the gate has gone to " + i.GoneTo + ", which reviews it next)"
	}
	switch {
	case i.ReviewedSubject != "":
		// The role is the reviewers' table's, and the subject has passed the check
		// that it is a hash: neither is free text from the record.
		return verdict + round + ", stale -- what was reviewed has changed since; reviewed revision " +
			i.ReviewedSubject + "; see sdlc artifact diff plan --since " + cmp.Or(i.ReviewedBy, i.Role) + end
	case i.Stale:
		return verdict + round + ", stale -- what was reviewed has changed since" + end
	default:
		return verdict + round + end
	}
}

// gateSubject is what a reviewer of this gate is looking at, by content.
//
// The definition-of-ready gate reviews the story, and the design gate a
// document, so their content is the subject. The tests gate reviews the test
// files the freeze is to hold, by name and content, so a test changed, added or
// renamed makes its review stale. The gates that review work review the whole
// tree, because a change anywhere can invalidate what they concluded. A gate
// with no reviewers has no subject, and that is not an error.
func gateSubject(ctx context.Context, rd *reading, id string, gate model.Gate) (string, error) {
	s := rd.s
	switch gate {
	case model.GateDoR:
		return s.StorySubject(id)
	case model.GateTestsFrozen:
		return rd.testsSubject(ctx)
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

	var outstanding, blocked, ruling []string
	for _, r := range reviewers {
		blocking := r.Blocks(policy)
		// The review that stands, the arbiter's where one does, and named so.
		latest, ok := record.StandingReview(gate, r.Role)
		by := r.Role
		if ok {
			by = latest.Role
		}
		switch {
		case !ok:
			outstanding = append(outstanding, r.Role+" has not reviewed"+back[r.Role])
		case r.Stops(policy, latest.Verdict) && by == r.Role && gone(rd.s, record, r):
			// Between the limit-th block and the arbiter's first review the
			// block still stands, but the reviewer is refused: "have the same
			// reviewer look again" sent the runbook to a refusal (red-team
			// F-13 of the design review). Asked before staleness, which named
			// the refused reviewer too once the work changed (verifier F-9).
			why, fix := goneToArbiter(gate, latest, latest.Subject != subject)
			blocked, ruling = append(blocked, why), append(ruling, fix)
		case latest.Subject != subject:
			outstanding = append(outstanding, by+" reviewed something that has changed since"+back[r.Role])
		case r.Stops(policy, latest.Verdict):
			blocked = append(blocked, by+" blocked"+describeNote(latest.Note))
		case blocking && latest.Verdict != model.VerdictApprove:
			outstanding = append(outstanding, by+" has not approved")
		}
	}
	if len(blocked) > 0 {
		return blockedRefusal(gate, blocked, ruling)
	}
	if len(outstanding) > 0 {
		return sdlcerr.New(sdlcerr.ReviewsMissing,
			string(gate)+" cannot pass until its reviews are in",
			strings.Join(outstanding, "; "))
	}
	return nil
}

// goneToArbiter is requireReviews' why and fix for a reviewer whose block
// stands at a gate that has gone to its arbiter, of the work as it is or as it
// was: either way only the arbiter may review it next.
func goneToArbiter(gate model.Gate, block model.Review, stale bool) (why, fix string) {
	arbiter := model.ArbiterName(block.Role)
	why = block.Role + " blocked"
	if stale {
		why += " something that has changed since"
	}
	why += describeNote(block.Note) + ", and " + string(gate) + " has gone to " + arbiter
	fix = "delegate " + block.Role + "'s open findings to a fresh sdlc:" + model.BaseRole(block.Role) +
		" as " + arbiter + ", asked for what sdlc staffing show --gate " + string(gate) + " gives it"
	return why, fix
}

// blockedRefusal is requireReviews' refusal of a gate a block stops. Where a
// blocking reviewer's gate has gone to its arbiter, the fix names the arbiter
// for it, and keeps the catalogue's for any other reviewer that blocked.
func blockedRefusal(gate model.Gate, blocked, ruling []string) error {
	e := sdlcerr.New(sdlcerr.ReviewBlocks,
		string(gate)+" is blocked by a reviewer",
		strings.Join(blocked, "; "))
	if len(ruling) == 0 {
		return e
	}
	if len(ruling) < len(blocked) {
		ruling = append(ruling, "for the rest, "+e.Fix)
	}
	return e.WithFix(strings.Join(ruling, "; "))
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
	// The note is a reviewer's, printed in a refusal of sdlc's own, the
	// freeze's among them: a line break in it would start a line that reads
	// as sdlc's.
	return ": " + oneLine(note)
}
