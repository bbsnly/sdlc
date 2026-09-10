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
	Flags       map[string]any      `json:"flags,omitempty"`
	Metrics     map[string]any      `json:"metrics,omitempty"`
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
