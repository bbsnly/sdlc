// Package model holds the loop's vocabulary: a story, the gates it passes
// through, and the record kept of each one.
//
// Nothing here touches the disk. The types are the shapes the JSON files on
// disk already have, so that a project set up by an earlier version of this
// tool -- or by the shell kit it replaces -- keeps working.
package model

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// Status is where a story stands in the backlog.
type Status string

// The statuses a story can hold. A story moves forward through them; the loop
// never invents one that is not here.
const (
	StatusTodo          Status = "todo"
	StatusReady         Status = "ready"
	StatusInProgress    Status = "in_progress"
	StatusAwaitingHuman Status = "awaiting_human"
	StatusBlocked       Status = "blocked"
	StatusDone          Status = "done"
	StatusDropped       Status = "dropped"
)

// Statuses lists every valid status, in the order a story usually meets them.
var Statuses = []Status{
	StatusTodo, StatusReady, StatusInProgress,
	StatusAwaitingHuman, StatusBlocked, StatusDone, StatusDropped,
}

// Valid reports whether s is a status the loop knows.
func (s Status) Valid() bool { return slices.Contains(Statuses, s) }

// AcceptanceCriterion is one testable statement about the finished story,
// written in EARS form so that it can be read as a test without translation.
type AcceptanceCriterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Type string `json:"type,omitempty"`
}

// Decision records a human choice made about the story, so that the reasoning
// survives the session it was made in.
type Decision struct {
	At   string `json:"at,omitempty"`
	By   string `json:"by,omitempty"`
	Text string `json:"text,omitempty"`
}

// Story is one unit of work: small enough to finish in a single iteration, and
// described well enough that its tests can be written before its code.
type Story struct {
	ID                 string                `json:"id"`
	Title              string                `json:"title"`
	AsA                string                `json:"as_a,omitempty"`
	IWant              string                `json:"i_want,omitempty"`
	SoThat             string                `json:"so_that,omitempty"`
	Priority           *int                  `json:"priority,omitempty"`
	Status             Status                `json:"status"`
	RiskTier           string                `json:"risk_tier,omitempty"`
	DependsOn          []string              `json:"depends_on,omitempty"`
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria"`
	NonGoals           []string              `json:"non_goals,omitempty"`
	Decisions          []Decision            `json:"decisions,omitempty"`
	Notes              string                `json:"notes,omitempty"`
	Created            string                `json:"created,omitempty"`
	Updated            string                `json:"updated,omitempty"`
}

// Backlog is the ordered set of stories a project intends to build. It is a
// plain file in the repository on purpose: a backlog outside version control
// cannot be reviewed and cannot be recovered.
type Backlog struct {
	Schema  string  `json:"_schema,omitempty"`
	Stories []Story `json:"stories"`
}

// Find returns the story with the given id.
func (b *Backlog) Find(id string) (*Story, bool) {
	for i := range b.Stories {
		if b.Stories[i].ID == id {
			return &b.Stories[i], true
		}
	}
	return nil, false
}

// IDs returns every story id in backlog order.
func (b *Backlog) IDs() []string {
	out := make([]string, 0, len(b.Stories))
	for _, s := range b.Stories {
		out = append(out, s.ID)
	}
	return out
}

// Selection is the answer to "what should I work on now", with the reason
// attached so that a user who disagrees can see what drove it.
type Selection struct {
	Story  *Story
	Resume bool
}

// Next picks the story to work on.
//
// A story already under way wins, always: the loop finishes what it started
// rather than accumulating half-done work. Otherwise the highest-priority story
// whose dependencies are all done, with the id breaking ties so that the same
// backlog always yields the same choice.
func (b *Backlog) Next() (Selection, bool) {
	for i := range b.Stories {
		if s := &b.Stories[i]; s.Status == StatusInProgress || s.Status == StatusAwaitingHuman {
			return Selection{Story: s, Resume: true}, true
		}
	}

	done := map[string]bool{}
	for i := range b.Stories {
		if b.Stories[i].Status == StatusDone {
			done[b.Stories[i].ID] = true
		}
	}

	var runnable []*Story
	for i := range b.Stories {
		s := &b.Stories[i]
		if s.Status != StatusReady && s.Status != StatusTodo {
			continue
		}
		if !b.dependenciesMet(s, done) {
			continue
		}
		runnable = append(runnable, s)
	}
	if len(runnable) == 0 {
		return Selection{}, false
	}
	sort.SliceStable(runnable, func(i, j int) bool {
		pi, pj := priorityOf(runnable[i]), priorityOf(runnable[j])
		if pi != pj {
			return pi < pj
		}
		return runnable[i].ID < runnable[j].ID
	})
	return Selection{Story: runnable[0]}, true
}

// BlockedBy lists the dependencies of s that are not done yet, so that "nothing
// is runnable" can say why rather than just refusing.
func (b *Backlog) BlockedBy(s *Story) []string {
	done := map[string]bool{}
	for i := range b.Stories {
		if b.Stories[i].Status == StatusDone {
			done[b.Stories[i].ID] = true
		}
	}
	var out []string
	for _, dep := range s.DependsOn {
		if !done[dep] {
			out = append(out, dep)
		}
	}
	return out
}

func (b *Backlog) dependenciesMet(s *Story, done map[string]bool) bool {
	for _, dep := range s.DependsOn {
		if !done[dep] {
			return false
		}
	}
	return true
}

// priorityOf treats an unset priority as the lowest, so that a story nobody
// prioritised never jumps the queue.
//
// Priority is a pointer because the backlog schema gives 0 a meaning of its own
// -- fix-forward, ahead of everything -- so "absent" and "most urgent" must not
// collapse into the same value. They are the two ends of the queue.
func priorityOf(s *Story) int {
	if s.Priority == nil {
		return 999
	}
	return *s.Priority
}

// Gate is one of the checkpoints a story passes through. The names are the keys
// used in the gate record on disk, and they are API.
type Gate string

// The gates, in the order a story meets them. Gate 0 is preflight and keeps no
// record: it is about the repository, not the story.
const (
	GateDoR            Gate = "dor"             // 1. the story is ready to start
	GateAnalysis       Gate = "analysis"        // 2. analysis and threat assessment
	GateTestsFrozen    Gate = "tests_frozen"    // 3. acceptance tests, then hash-locked
	GatePlan           Gate = "plan"            // 4. the plan
	GateDesignReview   Gate = "design_review"   // 4. the plan, reviewed
	GateImplementation Gate = "implementation"  // 5. the code
	GateVerification   Gate = "verification"    // 6. independent verification
	GateVerifierReview Gate = "verifier_review" // 7. the verifier's findings, reviewed
	GateCodeReview     Gate = "code_review"     // 7. the diff, reviewed
	GateCommit         Gate = "commit"          // 8. committed to trunk
	GateRetro          Gate = "retro"           // 9. retro and close
)

// Gates lists every gate in order.
var Gates = []Gate{
	GateDoR, GateAnalysis, GateTestsFrozen, GatePlan, GateDesignReview,
	GateImplementation, GateVerification, GateVerifierReview, GateCodeReview,
	GateCommit, GateRetro,
}

// Valid reports whether g is a gate this version knows.
func (g Gate) Valid() bool { return slices.Contains(Gates, g) }

// GateNames returns every gate name, for a message that has to list them.
func GateNames() string {
	names := make([]string, 0, len(Gates))
	for _, g := range Gates {
		names = append(names, string(g))
	}
	return strings.Join(names, ", ")
}

// Artifact is a document a gate produces and later gates read.
//
// The loop writes these through the sdlc command rather than letting an agent
// write the file directly. That is not a preference: Claude Code refuses a
// subagent's Write when the filename reads like a report -- "ANALYSIS.md" is
// refused, "THREATS.md" is not -- so an agent writing its own artifacts works
// for one of them and silently fails for the other. Going through the tool is
// also what the loop already does with every other piece of its state.
type Artifact struct {
	Name string // what a person types: "analysis"
	File string // the file it becomes, inside the story's directory
	Gate Gate   // the gate that produces it
	Role string // the agent whose gate it is
}

// RecordFile is the gate record's name inside a story's directory. It is what
// the loop remembers: what happened at each gate, when, and why. It is named
// here rather than in the store because the rules that protect it need it too.
const RecordFile = "gate-record.json"

// Artifacts is every document the loop knows how to store.
var Artifacts = []Artifact{
	{Name: "analysis", File: "ANALYSIS.md", Gate: GateAnalysis, Role: "researcher"},
	{Name: "threats", File: "THREATS.md", Gate: GateAnalysis, Role: "researcher"},
	{Name: "test_plan", File: "TEST-PLAN.md", Gate: GateTestsFrozen, Role: "sdet"},
	{Name: "plan", File: "PLAN.md", Gate: GatePlan, Role: "implementer"},
	{Name: "verification", File: "VERIFICATION.md", Gate: GateVerification, Role: "verifier"},
	{Name: "retro", File: "RETRO.md", Gate: GateRetro, Role: "bookkeeper"},
}

// ArtifactsFor lists the documents a gate is expected to produce.
func ArtifactsFor(g Gate) []Artifact {
	var out []Artifact
	for _, a := range Artifacts {
		if a.Gate == g {
			out = append(out, a)
		}
	}
	return out
}

// FindArtifact looks one up by the name a person types.
func FindArtifact(name string) (Artifact, bool) {
	for _, a := range Artifacts {
		if a.Name == strings.ToLower(strings.TrimSpace(name)) {
			return a, true
		}
	}
	return Artifact{}, false
}

// ArtifactByFile looks one up by its file name, for a rule that has a path.
func ArtifactByFile(file string) (Artifact, bool) {
	for _, a := range Artifacts {
		if strings.EqualFold(a.File, file) {
			return a, true
		}
	}
	return Artifact{}, false
}

// ArtifactNames lists what can be written, for a message that has to say.
func ArtifactNames() string {
	names := make([]string, 0, len(Artifacts))
	for _, a := range Artifacts {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}

// LockSchema versions the freeze file, so that a future change to its shape can
// be recognised rather than guessed at.
const LockSchema = "sdlc/tests-lock/1"

// Lock is the freeze: the acceptance tests as they stood when the test gate
// passed, recorded by content.
//
// This is the hinge the whole loop turns on. An agent that can edit its own
// acceptance tests will eventually edit them -- not maliciously, just by taking
// the shortest path to green -- and every gate after that is theatre. Hashing
// the files is what makes a later change visible instead of arguable.
type Lock struct {
	Schema string            `json:"schema"`
	Story  string            `json:"story"`
	At     string            `json:"at"`
	Files  map[string]string `json:"files"` // repository-relative path -> sha256
}

// NewLock records a freeze.
func NewLock(story string, files map[string]string, at time.Time) *Lock {
	return &Lock{Schema: LockSchema, Story: story, At: Timestamp(at), Files: files}
}

// Holds reports whether this repository-relative path is part of the freeze.
// The nil lock holds nothing, which is what "no freeze yet" means.
func (l *Lock) Holds(path string) bool {
	if l == nil {
		return false
	}
	_, ok := l.Files[path]
	return ok
}

// Paths lists the frozen files in a stable order, for a message that has to
// name them.
func (l *Lock) Paths() []string {
	if l == nil {
		return nil
	}
	out := make([]string, 0, len(l.Files))
	for p := range l.Files {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Before reports the gates a story meets before this one. An earlier gate that
// has not passed means this one is being recorded out of order, which is how a
// loop stops being a loop.
func (g Gate) Before() []Gate {
	i := slices.Index(Gates, g)
	if i < 0 {
		return nil
	}
	return Gates[:i]
}

// ---------------------------------------------------------------- reviews

// Verdict is what a reviewer concluded.
type Verdict string

// The verdicts a reviewer can reach. A blocking reviewer's approve is what lets
// a gate pass; a note is a finding worth recording that does not stand in the
// way.
const (
	VerdictApprove Verdict = "approve"
	VerdictBlock   Verdict = "block"
	VerdictNote    Verdict = "note"
)

// Verdicts lists every valid verdict.
var Verdicts = []Verdict{VerdictApprove, VerdictBlock, VerdictNote}

// Valid reports whether v is a verdict the loop knows.
func (v Verdict) Valid() bool { return slices.Contains(Verdicts, v) }

// VerdictNames lists them, for a message that has to say.
func VerdictNames() string {
	names := make([]string, 0, len(Verdicts))
	for _, v := range Verdicts {
		names = append(names, string(v))
	}
	return strings.Join(names, ", ")
}

// Reviewer is one role that reads one gate's work.
//
// Blocking and advisory are both real. An advisory reviewer cannot stop a gate,
// but it still has to report: the point of it is that its findings are on the
// record, and a reviewer that can be skipped by not running it is not a
// reviewer at all.
type Reviewer struct {
	Role                  string
	Gate                  Gate
	Blocking              bool // always blocks
	WhenSecuritySensitive bool // blocks only when the story touches a trust boundary
}

// Blocks reports whether this reviewer can refuse the gate for this story.
func (r Reviewer) Blocks(securitySensitive bool) bool {
	return r.Blocking || (r.WhenSecuritySensitive && securitySensitive)
}

// Reviewers is every review the loop expects, by gate.
var Reviewers = []Reviewer{
	{Role: "architect", Gate: GateDesignReview, Blocking: true},
	{Role: "red-team", Gate: GateDesignReview},
	{Role: "security", Gate: GateDesignReview, WhenSecuritySensitive: true},
	{Role: "perf", Gate: GateDesignReview},
	{Role: "human-advocate", Gate: GateDesignReview},

	{Role: "verifier", Gate: GateVerifierReview, Blocking: true},

	{Role: "code-reviewer", Gate: GateCodeReview, Blocking: true},
	{Role: "security", Gate: GateCodeReview, WhenSecuritySensitive: true},
	{Role: "perf", Gate: GateCodeReview},
	{Role: "human-advocate", Gate: GateCodeReview},
}

// ReviewersFor lists the reviews one gate expects.
func ReviewersFor(g Gate) []Reviewer {
	var out []Reviewer
	for _, r := range Reviewers {
		if r.Gate == g {
			out = append(out, r)
		}
	}
	return out
}

// FindReviewer looks one up by the gate and the role a person types.
func FindReviewer(g Gate, role string) (Reviewer, bool) {
	role = strings.ToLower(strings.TrimSpace(role))
	for _, r := range Reviewers {
		if r.Gate == g && r.Role == role {
			return r, true
		}
	}
	return Reviewer{}, false
}

// ReviewerNames lists the roles one gate expects, for a message that has to say.
func ReviewerNames(g Gate) string {
	rs := ReviewersFor(g)
	names := make([]string, 0, len(rs))
	for _, r := range rs {
		names = append(names, r.Role)
	}
	if len(names) == 0 {
		return "no reviewers"
	}
	return strings.Join(names, ", ")
}

// Review is one reviewer's conclusion about one gate.
//
// Subject is what was reviewed, by content -- the plan's hash at the design
// gate, the tree's hash at the code gate. An approval of a plan that has since
// changed is not an approval, and without recording what was in front of the
// reviewer there is no way to tell the two apart.
type Review struct {
	At      string  `json:"at"`
	Gate    Gate    `json:"gate"`
	Role    string  `json:"role"`
	Verdict Verdict `json:"verdict"`
	Round   int     `json:"round"`
	Subject string  `json:"subject,omitempty"`
	File    string  `json:"file,omitempty"`
	Note    string  `json:"note,omitempty"`
}

// GateStatus is the outcome recorded for a gate.
type GateStatus string

// The outcomes a gate can record.
const (
	GatePass    GateStatus = "pass"
	GateFail    GateStatus = "fail"
	GatePending GateStatus = "pending"
)

// GateStatuses lists every valid outcome.
var GateStatuses = []GateStatus{GatePass, GateFail, GatePending}

// Valid reports whether s is an outcome the loop knows.
func (s GateStatus) Valid() bool { return slices.Contains(GateStatuses, s) }

// GateResult is what happened at one gate, and when.
type GateResult struct {
	Status   GateStatus `json:"status"`
	At       string     `json:"at"`
	Note     string     `json:"note,omitempty"`
	TreeHash string     `json:"tree_hash,omitempty"`
}

// Event is one thing that happened during the iteration, in order. The record
// is append-only: it is the story's history, not its current state.
type Event struct {
	At      string `json:"at"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Escalation is a point where the loop stopped and asked a person.
type Escalation struct {
	At       string `json:"at"`
	Type     string `json:"type"`
	Message  string `json:"message"`
	TreeHash string `json:"tree_hash,omitempty"`
	Resolved bool   `json:"resolved,omitempty"`
	Story    string `json:"story,omitempty"`
}

// Approval is a person's answer to an escalation, bound to the tree it was
// given for -- an approval of code that has since changed is not an approval.
type Approval struct {
	At       string `json:"at"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Type     string `json:"type,omitempty"`
	TreeHash string `json:"tree_hash,omitempty"`
}

// Record is everything known about one story's iteration.
type Record struct {
	Story       string              `json:"story"`
	Created     string              `json:"created"`
	Gates       map[Gate]GateResult `json:"gates"`
	Events      []Event             `json:"events"`
	Escalations []Escalation        `json:"escalations"`
	Approvals   []Approval          `json:"approvals"`
	Reviews     []Review            `json:"reviews,omitempty"`
	Security    *bool               `json:"security_sensitive,omitempty"`
	Flags       map[string]any      `json:"flags,omitempty"`
	Metrics     map[string]any      `json:"metrics,omitempty"`
}

// SecuritySensitive reports whether the story touches a trust boundary. It
// decides whether the security reviewer can block, and until Gate 2 has said
// one way or the other the safe answer is yes.
func (r *Record) SecuritySensitive() bool { return r.Security == nil || *r.Security }

// SetSecuritySensitive records Gate 2's decision.
func (r *Record) SetSecuritySensitive(v bool) { r.Security = &v }

// AddReview appends a review and returns it with its round filled in. Reviews
// are history: a second round does not overwrite the first, because "what did
// the architect say last time" is a question the next round needs answered.
func (r *Record) AddReview(rev Review, at time.Time) Review {
	rev.At = Timestamp(at)
	rev.Round = r.Round(rev.Gate, rev.Role) + 1
	r.Reviews = append(r.Reviews, rev)
	return rev
}

// Round counts how many times a role has already reviewed a gate.
func (r *Record) Round(g Gate, role string) int {
	n := 0
	for _, rev := range r.Reviews {
		if rev.Gate == g && rev.Role == role {
			n++
		}
	}
	return n
}

// LatestReview is the most recent review of a gate by a role.
func (r *Record) LatestReview(g Gate, role string) (Review, bool) {
	for i := len(r.Reviews) - 1; i >= 0; i-- {
		if r.Reviews[i].Gate == g && r.Reviews[i].Role == role {
			return r.Reviews[i], true
		}
	}
	return Review{}, false
}

// NewRecord starts a record for a story.
func NewRecord(story string, at time.Time) *Record {
	return &Record{
		Story:   story,
		Created: Timestamp(at),
		Gates:   map[Gate]GateResult{},
		Events:  []Event{},
		Metrics: map[string]any{},
	}
}

// Pass reports whether the gate has been recorded as passed.
func (r *Record) Pass(g Gate) bool {
	return r.Gates[g].Status == GatePass
}

// NextGate reports the first gate that has not passed: where a resumed loop
// picks up. It returns false once every gate is behind it.
//
// The loop's own rule is that a gate cannot be recorded before the gates in
// front of it, so "the first that has not passed" is the only answer, and
// working it out belongs here rather than in each caller that needs it.
func (r *Record) NextGate() (Gate, bool) {
	for _, g := range Gates {
		if !r.Pass(g) {
			return g, true
		}
	}
	return "", false
}

// SetGate records an outcome for a gate.
func (r *Record) SetGate(g Gate, status GateStatus, note string, at time.Time) {
	if r.Gates == nil {
		r.Gates = map[Gate]GateResult{}
	}
	prev := r.Gates[g]
	prev.Status = status
	prev.At = Timestamp(at)
	prev.Note = note
	r.Gates[g] = prev
}

// Append adds an event to the story's history.
func (r *Record) Append(kind, message string, at time.Time) {
	r.Events = append(r.Events, Event{At: Timestamp(at), Type: kind, Message: message})
}

// Timestamp is the one time format the loop writes: UTC, to the second, so that
// two records written on different machines sort and compare.
func Timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
