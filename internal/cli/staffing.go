package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// The staffing: which agents a story needs, and what model and effort each is
// asked for. The project manager records it once a story is found ready and
// amends it as the story goes; a person pins what they want with assign.
//
// What is recorded is what was asked for. The environment can override a model
// and an effort, and nothing here observes a delegation, so no output says
// what an agent was given.

// maxReason bounds a reason, which a model writes and every later reader is
// shown: an unbounded one is a way to fill the record and a session's context.
const maxReason = 1000

func newStaffingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "staffing",
		Short: "Say which agents a story needs, and what each is asked for",
		Long: "staffing records, for one story, which of the plugin's agents the story needs\n" +
			"and the model and effort each is asked for. A gate then waits only for the\n" +
			"reviewers the story needs.\n\n" +
			"It is off unless the project turns it on with staffing.enabled. The project\n" +
			"manager agent records it with set and changes it with amend; a person pins a\n" +
			"model or an effort for an agent with assign, in their own terminal; anyone\n" +
			"reads it with show.\n\n" +
			"The model and effort are what is asked for. The environment can override\n" +
			"both, and sdlc does not observe what a delegation is given.",
		Example: "  sdlc staffing show\n  sdlc staffing show --gate code_review --json",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newStaffingShowCmd(), newStaffingSetCmd(), newStaffingAmendCmd(), newStaffingAssignCmd())
	return cmd
}

// staffingStory opens the project for a staffing command and finds the story
// it is about. All four go through it, so they fail the same way.
//
// set and amend write only the story being worked on. The hook holds them to
// the project manager in the session working a story and asks nothing where no
// story is held, so a command that took any story let a session that had
// handed its story to a person, or not started one, write a staffing itself.
// assign is a person's, and takes a story that waits for them as well.
func staffingStory(cmd *cobra.Command, named string) (*store.Store, string, *model.Record, func(), error) {
	nothing := noUnlock
	verb := cmd.Name()
	var s *store.Store
	var err error
	unlock := nothing
	if verb == verbShow {
		s, _, err = openStore(cmd)
	} else {
		s, _, unlock, err = openStoreForWriting(cmd)
	}
	if err != nil {
		return nil, "", nil, nothing, err
	}
	fail := func(err error) (*store.Store, string, *model.Record, func(), error) {
		unlock()
		return nil, "", nil, nothing, err
	}
	if !s.Config().Staffing.Enabled {
		return fail(sdlcerr.New(sdlcerr.StaffingOff,
			"the staffing is off in this project",
			"staffing.enabled is not true in "+config.File+", so every agent takes part in every story"))
	}
	id, err := staffingStoryID(cmd, s, verb, named)
	if err != nil {
		return fail(err)
	}
	if verb != verbShow {
		if err := refuseIfHeldElsewhere(s, id, "the staffing is of the work in that checkout, not this one"); err != nil {
			return fail(err)
		}
	}
	record, err := s.Record(id)
	if err != nil {
		return fail(err)
	}
	return s, id, record, unlock, nil
}

// staffingStoryID is the story a staffing command is about: the one named, or
// the one being worked on.
func staffingStoryID(cmd *cobra.Command, s *store.Store, verb, named string) (string, error) {
	if verb == verbShow && named != "" {
		return named, nil
	}
	active, err := s.Active()
	if err != nil {
		return "", err
	}
	switch {
	case verb == "assign" && named != "":
		story, _, err := s.Story(named)
		if err != nil {
			return "", err
		}
		if story.Status != model.StatusInProgress && story.Status != model.StatusAwaitingHuman {
			return "", usageWith(cmd, quote(named)+" has not been started, and a model or an effort is "+
				"assigned to a story that has", "start the story with /sdlc:next, then assign")
		}
		return named, nil
	case active == "":
		return "", sdlcerr.New(sdlcerr.NoActiveIteration,
			"there is no story being worked on",
			"the staffing belongs to the story being worked on, and no iteration is running")
	case named != "" && named != active:
		return "", usageWith(cmd, "the staffing is written for the story being worked on, which is "+
			quote(active)+", not "+quote(named), "leave --story out; /sdlc:next picks a story up")
	}
	return active, nil
}

// usageWith is a command line refused with its own way on, where "see --help"
// would not say what to do.
func usageWith(cmd *cobra.Command, what, fix string) error {
	return sdlcerr.New(sdlcerr.BadArgument, what,
		"`"+cmd.CommandPath()+"` did not understand its command line").WithFix(fix)
}

// saveStaffing writes the record with one more line of its history. The line
// is what an sdlc from before the staffing keeps when it drops the rest.
func saveStaffing(s *store.Store, record *model.Record, kind, message string) error {
	record.Append(kind, message, s.Now())
	return s.SaveRecord(record)
}

// staffed reports whether a staffing is recorded for the story. A record that
// holds only what a person assigned has none yet.
func staffed(record *model.Record) bool {
	return record.Staffing != nil && record.Staffing.At != ""
}

func noStaffing(id string) error {
	return sdlcerr.New(sdlcerr.StaffingNone,
		"no staffing is recorded for "+id,
		"the staffing is on, and the project manager has not said who this story needs")
}

// staffingRow is the staffing's own row for an agent.
func staffingRow(record *model.Record, agent string) *model.Staffed {
	for i := range record.Staffing.Agents {
		if record.Staffing.Agents[i].Agent == agent {
			return &record.Staffing.Agents[i]
		}
	}
	return &model.Staffed{Agent: agent, Needed: true}
}

// assigned is what a person pinned for an agent, and whether they pinned
// anything.
func assigned(record *model.Record, agent string) (model.Assigned, bool) {
	if record.Staffing != nil {
		for _, a := range record.Staffing.Assigned {
			if a.Agent == agent {
				return a, true
			}
		}
	}
	return model.Assigned{}, false
}

func personAssigned(agent, what string) error {
	return sdlcerr.New(sdlcerr.StaffingAssigned,
		"a person assigned "+what+" to "+agent,
		"what `sdlc staffing assign` recorded is a person's choice, and no staffing or amendment changes it")
}

// refuseOutsideLimits holds what is asked for to the project's lists, from the
// agent's lowest up. The order of a list is the ranking: no name ranks here.
func refuseOutsideLimits(cfg config.Staffing, agent, askedModel, askedEffort string) error {
	low := cfg.Lowest[agent]
	for _, l := range []struct {
		kind, asked, lowest, setting string
		names                        []string
	}{
		{"model", askedModel, low.Model, "staffing.models", cfg.Models},
		{"effort", askedEffort, low.Effort, "staffing.efforts", cfg.Efforts},
	} {
		allowed := l.names[max(slices.Index(l.names, l.lowest), 0):]
		if l.asked != "" && !slices.Contains(allowed, l.asked) {
			return sdlcerr.New(sdlcerr.StaffingOutsideLimits,
				agent+" cannot be asked for the "+l.kind+" "+quote(l.asked),
				l.setting+" and staffing.lowest allow "+agent+", from the lowest up: "+strings.Join(allowed, ", "))
		}
	}
	return nil
}

// refuseLeavingOut is asked by set for every agent it leaves out and by amend:
// the floor, and a block that stands. A staffing recorded after reviews exist,
// where the setting was turned on mid-story or an older sdlc dropped the
// first, would otherwise pass a gate over a block an amendment could not.
func refuseLeavingOut(record *model.Record, agent string) error {
	if model.NeverLeftOut(agent, record.SecuritySensitive()) {
		e := sdlcerr.New(sdlcerr.StaffingNeverLeftOut,
			agent+" is never left out of a story",
			"the agents that do a gate's work, the verifier, the code reviewer, and security on a "+
				"security-sensitive story always take part")
		if agent == "security" && record.Security == nil {
			e = e.WithFix("security stays in until the analysis records the story as not security-sensitive: " +
				"leave it in now, and amend once `sdlc gate analysis pass --security-sensitive=false` has said so")
		}
		return e
	}
	for _, r := range model.Reviewers {
		// The latest review, stale or not: a block of work that has since
		// changed is still the reviewer's last word on it.
		if latest, ok := record.LatestReview(r.Gate, r.Role); r.Role == agent && !record.Pass(r.Gate) &&
			ok && latest.Verdict == model.VerdictBlock {
			return sdlcerr.New(sdlcerr.StaffingReviewBlocks,
				agent+" blocked "+string(r.Gate)+", and that gate has not passed",
				"leaving the reviewer out would pass the gate over its block")
		}
	}
	return nil
}

// waited reports whether a gate does not wait for a reviewer, because the
// staffing leaves it out, and the changed path that brings it back to the code
// review where one does. The gates, `review list` and `staffing show` all ask
// here, with one reading, so what one says another does.
func waited(ctx context.Context, rd *reading, record *model.Record, gate model.Gate, role string) (out bool, broughtBack string, err error) {
	cfg := rd.s.Config().Staffing
	if !cfg.Enabled || !record.LeftOut(role) {
		return false, "", nil
	}
	listed := cfg.BringBack[role]
	if gate != model.GateCodeReview || len(listed) == 0 {
		return true, "", nil
	}
	changed, err := rd.changedPaths(ctx, record)
	if err != nil {
		return false, "", err
	}
	if rd.unreadRange {
		return false, "what the story committed, which could not be read", nil
	}
	for _, p := range changed {
		if pathrules.UnderAny(p, listed...) {
			return false, p, nil
		}
	}
	return true, "", nil
}

// broughtBack is every reviewer a changed path brings back to the code review,
// for the record: the hook reads it there, because it makes no git call.
func broughtBack(ctx context.Context, rd *reading, record *model.Record) (map[string]string, error) {
	var back map[string]string
	for _, r := range model.ReviewersFor(model.GateCodeReview) {
		_, by, err := waited(ctx, rd, record, model.GateCodeReview, r.Role)
		if err != nil {
			return nil, err
		}
		if by != "" {
			if back == nil {
				back = map[string]string{}
			}
			back[r.Role] = by
		}
	}
	return back, nil
}

// staffTheGate is what the staffing asks of a gate that passes. The analysis
// waits for one to be recorded, because every gate after it is delegated by
// what the staffing says. The code review and the commit gate record who a
// changed path brought back, with the reading that has just held the gate to
// them, so the hook and the gate cannot disagree about who was waited for.
func staffTheGate(ctx context.Context, rd *reading, id string, gate model.Gate, status model.GateStatus, record *model.Record) error {
	if status != model.GatePass || !rd.s.Config().Staffing.Enabled {
		return nil
	}
	switch {
	case !staffed(record):
		if gate == model.GateAnalysis {
			return noStaffing(id)
		}
	case gate == model.GateCodeReview || gate == model.GateCommit:
		back, err := broughtBack(ctx, rd, record)
		if err != nil {
			return err
		}
		record.Staffing.BroughtBack = back
	}
	return nil
}

// noteSensitivity keeps, among the staffing's amendments, an analysis that
// changes whether the story is security-sensitive: it changes whether security
// can be left out, and a reader of the staffing would otherwise see security
// come or go with no entry saying why.
func noteSensitivity(s *store.Store, record *model.Record, sensitive bool) {
	if !staffed(record) || record.Security == nil || *record.Security == sensitive {
		return
	}
	change := "the story is recorded as not security-sensitive, so the staffing decides whether security takes part"
	if sensitive {
		change = "the story is recorded as security-sensitive, so security takes part whatever the staffing says"
	}
	record.Staffing.Amendments = append(record.Staffing.Amendments, model.Amendment{
		At: model.Timestamp(s.Now()), Agent: "security", Change: change,
		Reason: "sdlc gate analysis pass --security-sensitive=" + strconv.FormatBool(sensitive), By: "gate analysis",
	})
	record.Append(model.EventStaffingAmend, "security: "+change, s.Now())
}

// staffingRoster is what config cannot check, because it does not know the
// agents: that a lowest names one, and a bring_back a reviewer of the code.
func staffingRoster(cfg config.Staffing) error {
	if !cfg.Enabled {
		return nil
	}
	var problems []string
	for agent := range cfg.Lowest {
		if !slices.Contains(model.Agents, agent) {
			problems = append(problems, "staffing.lowest names "+quote(agent)+", which is not an agent: "+
				agentsAre+strings.Join(model.Agents, ", "))
		}
	}
	for reviewer := range cfg.BringBack {
		if _, ok := model.FindReviewer(model.GateCodeReview, reviewer); !ok || reviewer != strings.ToLower(strings.TrimSpace(reviewer)) {
			problems = append(problems, "staffing.bring_back names "+quote(reviewer)+", which does not review the code: "+
				"the reviewers there are "+model.ReviewerNames(model.GateCodeReview))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	slices.Sort(problems)
	return sdlcerr.New(sdlcerr.ConfigInvalid,
		config.File+" has settings sdlc cannot use",
		strings.Join(problems, "; "))
}

// staffedInfo is one agent of a staffing as it is shown. set_by is the tool's
// to write: it says `sdlc staffing assign` recorded the row.
type staffedInfo struct {
	Agent         string `json:"agent"`
	Needed        bool   `json:"needed"`
	Reason        string `json:"reason,omitempty"`
	AskedModel    string `json:"asked_model,omitempty"`
	AskedEffort   string `json:"asked_effort,omitempty"`
	AskedReason   string `json:"asked_reason,omitempty"`
	SetBy         string `json:"set_by,omitempty"`
	BroughtBackBy string `json:"brought_back_by,omitempty"`
}

type atGateInfo struct {
	Agent       string `json:"agent"`
	AskedModel  string `json:"asked_model"`
	AskedEffort string `json:"asked_effort"`
}

type staffingPayload struct {
	OK         bool              `json:"ok"`
	Story      string            `json:"story"`
	Gate       string            `json:"gate"`
	Agents     []staffedInfo     `json:"agents"`
	AtGate     []atGateInfo      `json:"at_gate"`
	Amendments []model.Amendment `json:"amendments"`
}

// staffingInfos is the staffing a row to an agent, with the floor applied as a
// gate applies it: an agent the staffing leaves out and the floor keeps reads
// as needed.
func staffingInfos(record *model.Record) []staffedInfo {
	infos := make([]staffedInfo, 0, len(record.Staffing.Agents))
	for _, a := range record.Staffing.Agents {
		info := staffedInfo{
			Agent: a.Agent, Needed: !record.LeftOut(a.Agent), Reason: a.Reason,
			AskedModel: a.AskedModel, AskedEffort: a.AskedEffort, AskedReason: a.AskedReason,
		}
		if _, person := assigned(record, a.Agent); person {
			info.SetBy = "person"
		}
		infos = append(infos, info)
	}
	return infos
}

// describeStaffing is the staffing in one line of `sdlc status`: who is left
// out, by name. The reasons are `sdlc staffing show`'s to print.
func describeStaffing(record *model.Record) string {
	if !staffed(record) {
		return "none recorded, so every agent is needed"
	}
	var out []string
	for _, agent := range model.Agents {
		if record.LeftOut(agent) {
			out = append(out, agent)
		}
	}
	if len(out) == 0 {
		return "recorded: every agent is needed"
	}
	return "recorded: left out " + strings.Join(out, ", ")
}

func newStaffingShowCmd() *cobra.Command {
	var storyID, gateName string
	cmd := &cobra.Command{
		Use:   verbShow,
		Short: "Show the story's staffing, and who to delegate to at a gate",
		Long: "show prints the staffing: for every agent whether the story needs it and why,\n" +
			"and the model and effort it is asked for.\n\n" +
			"at_gate, in --json, is who to delegate to at the gate the story is at, or at\n" +
			"--gate: the agent that does the gate's work and each reviewer the gate waits\n" +
			"for, a reviewer a changed path brought back included.",
		Example: "  sdlc staffing show\n  sdlc staffing show --gate code_review --json",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runStaffingShow(cmd, storyID, gateName) },
	}
	cmd.Flags().StringVar(&gateName, "gate", "", "who to delegate to at this gate, instead of the one the story is at")
	cmd.Flags().StringVar(&storyID, "story", "", "for this story instead of the one being worked on")
	return cmd
}

func runStaffingShow(cmd *cobra.Command, storyID, gateName string) error {
	gate := model.Gate(gateName)
	if gateName != "" && !gate.Valid() {
		return sdlcerr.New(sdlcerr.UnknownGate,
			"there is no gate called "+quote(gateName),
			"this version knows "+model.GateNames())
	}
	s, id, record, _, err := staffingStory(cmd, storyID)
	if err != nil {
		return err
	}
	if !staffed(record) {
		return noStaffing(id)
	}
	if gateName == "" {
		gate, _ = record.NextGate()
	}

	payload := staffingPayload{
		OK: true, Story: id, Gate: string(gate), Agents: staffingInfos(record),
		AtGate: []atGateInfo{}, Amendments: append([]model.Amendment{}, record.Staffing.Amendments...),
	}
	if err := staffedAtGate(cmd.Context(), s, record, gate, &payload); err != nil {
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), payload)
	}
	printStaffing(cmd.OutOrStdout(), s.Config().Staffing, payload)
	return nil
}

// staffedAtGate lists who to delegate to at the gate: the agent that does its
// work and each reviewer it waits for, and marks a reviewer a changed path
// brought back.
func staffedAtGate(ctx context.Context, s *store.Store, record *model.Record, gate model.Gate, payload *staffingPayload) error {
	at := func(agent string) {
		row := staffingRow(record, agent)
		payload.AtGate = append(payload.AtGate, atGateInfo{Agent: agent, AskedModel: row.AskedModel, AskedEffort: row.AskedEffort})
	}
	for _, d := range model.Doers {
		if d.Gate == gate {
			at(d.Agent)
		}
	}
	rd, policy := readingOf(s), reviewPolicy(s, record)
	for _, r := range model.ReviewersFor(gate) {
		out, by, err := waited(ctx, rd, record, gate, r.Role)
		if err != nil {
			return err
		}
		if !out && r.Expected(policy) {
			at(r.Role)
		}
		payload.markBroughtBack(r.Role, by)
	}
	return nil
}

// markBroughtBack names who brought the reviewer back, on its row.
func (p *staffingPayload) markBroughtBack(role, by string) {
	for i := range p.Agents {
		if p.Agents[i].Agent == role {
			p.Agents[i].BroughtBackBy = by
		}
	}
}

func printStaffing(w io.Writer, cfg config.Staffing, p staffingPayload) {
	where := "every gate has passed"
	if p.Gate != "" {
		where = "at " + p.Gate
	}
	fmt.Fprintf(w, "%s  staffing, %s\n\n", p.Story, where)
	fmt.Fprintf(w, "  may be asked for, lowest first:  models %s;  efforts %s\n",
		strings.Join(cfg.Models, ", "), strings.Join(cfg.Efforts, ", "))
	for _, reviewer := range slices.Sorted(maps.Keys(cfg.BringBack)) {
		fmt.Fprintf(w, "  %s is brought back to code_review by a change under %s\n", reviewer, quotePaths(cfg.BringBack[reviewer]))
	}
	fmt.Fprintln(w)
	for _, a := range p.Agents {
		fmt.Fprintf(w, "  %-15s %s\n", a.Agent, describeStaffed(a))
	}
	if len(p.Amendments) > 0 {
		fmt.Fprintln(w, "\n  amended")
		for _, a := range p.Amendments {
			fmt.Fprintf(w, "    %s\n", describeAmendment(a))
		}
	}
}

// describeStaffed is one agent's line. Every reason is printed quoted: a model
// or a person wrote it, and printed as it came it could end the line and start
// one that reads as the tool's own.
func describeStaffed(a staffedInfo) string {
	person := ""
	if a.SetBy != "" {
		person = ", a person chose"
	}
	switch {
	case a.BroughtBackBy != "":
		return "left out, brought back at code_review by " + quote(a.BroughtBackBy) + person + " -- " + quote(a.Reason)
	case !a.Needed:
		return "left out -- " + quote(a.Reason)
	case a.AskedModel == "" && a.AskedEffort == "":
		return "needed, nothing asked for: its definition decides" + person + " -- " + quote(a.Reason)
	}
	return "asked for " + a.AskedModel + " at " + a.AskedEffort + person + " -- " + quote(a.Reason) + "; " + quote(a.AskedReason)
}

func describeAmendment(a model.Amendment) string {
	return oneLine(a.At) + "  " + oneLine(a.Agent) + ": " + oneLine(a.Change) + " -- " + quote(a.Reason) + " (" + oneLine(a.By) + ")"
}

func newStaffingSetCmd() *cobra.Command {
	var storyID, file string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Record the story's staffing (the project manager's)",
		Long: "set records the staffing for the story being worked on. The project manager\n" +
			"agent runs it, once, when the story is found ready; after that the staffing is\n" +
			"changed with amend.\n\n" +
			"The staffing is a JSON document, from standard input or --file:\n\n" +
			`  {"agents": [{"agent": "perf", "needed": false, "reason": "..."},` + "\n" +
			`              {"agent": "verifier", "needed": true, "reason": "...",` + "\n" +
			`               "asked_model": "opus", "asked_effort": "high", "asked_reason": "..."}]}` + "\n\n" +
			"It names every agent the plugin ships, once. An agent the story needs comes\n" +
			"with the model and effort asked for and why. Nothing else in the document is\n" +
			"read.",
		Example: "  sdlc staffing set < staffing.json\n  sdlc staffing set --file /tmp/staffing.json",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runStaffingSet(cmd, file, storyID) },
	}
	cmd.Flags().StringVar(&file, "file", "", "read the staffing from this path instead of standard input")
	cmd.Flags().StringVar(&storyID, "story", "", "the story being worked on, which is the only one a staffing is recorded for")
	return cmd
}

func runStaffingSet(cmd *cobra.Command, file, storyID string) error {
	content, err := readDocument(cmd, file)
	if err != nil {
		return err
	}
	s, id, record, unlock, err := staffingStory(cmd, storyID)
	if err != nil {
		return err
	}
	defer unlock()
	// Its own type, with the rows and nothing else: decoded into the
	// record's, a document could carry what only assign and amend write.
	var doc struct {
		Agents []model.Staffed `json:"agents"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return staffingNotRecorded(id, "the document is not a staffing: "+oneLine(err.Error()))
	}
	if staffed(record) {
		return staffingNotRecorded(id, id+" already has a staffing, which `sdlc staffing amend` changes an agent at a time")
	}
	rows, err := rowsForEveryAgent(id, doc.Agents)
	if err != nil {
		return err
	}
	needed, err := checkStaffingRows(id, doc.Agents)
	if err != nil {
		return err
	}
	if err := checkAgainstPins(s, record, rows); err != nil {
		return err
	}
	if record.Staffing == nil {
		record.Staffing = &model.Staffing{}
	}
	// Counted from the record's history, which an sdlc from before the
	// staffing keeps when it drops the staffing itself: a number above
	// nought on a first staffing says one was lost.
	before := 0
	for _, ev := range record.Events {
		if slices.Contains([]string{model.EventStaffingSet, model.EventStaffingAmend, model.EventStaffingAssign}, ev.Type) {
			before++
		}
	}
	record.Staffing.At, record.Staffing.Agents = model.Timestamp(s.Now()), rows
	said := fmt.Sprintf("%d agents needed, %d left out", needed, len(rows)-needed)
	if err := saveStaffing(s, record, model.EventStaffingSet,
		fmt.Sprintf("%s; staffing changes on the record before this: %d", said, before)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s  staffing recorded: %s\n", id, said)
	return err
}

// rowsForEveryAgent is the document's row for each agent, in the order the
// agents are listed.
func rowsForEveryAgent(id string, agents []model.Staffed) ([]model.Staffed, error) {
	rows := make([]model.Staffed, 0, len(model.Agents))
	for _, agent := range model.Agents {
		at := slices.IndexFunc(agents, func(a model.Staffed) bool { return a.Agent == agent })
		if at < 0 {
			return nil, staffingNotRecorded(id, "it has no row for "+agent+": a staffing names every agent, needed or not")
		}
		rows = append(rows, agents[at])
	}
	return rows, nil
}

// staffingNotRecorded is the refusal of a staffing document, with why.
func staffingNotRecorded(id, why string) error {
	return sdlcerr.New(sdlcerr.StaffingInvalid, "the staffing for "+id+" was not recorded", why)
}

// checkStaffingRows holds each row of the document to what a row has to say,
// and counts the agents it needs.
func checkStaffingRows(id string, agents []model.Staffed) (needed int, err error) {
	for _, a := range agents {
		n := 0
		for _, other := range agents {
			if other.Agent == a.Agent {
				n++
			}
		}
		switch {
		case !slices.Contains(model.Agents, a.Agent) || n > 1:
			return 0, staffingNotRecorded(id, quote(a.Agent)+" is not an agent the plugin ships, or is named more than once: "+
				agentsAre+strings.Join(model.Agents, ", "))
		case blankOrLong(a.Reason):
			return 0, staffingNotRecorded(id, a.Agent+" has no reason, or one over "+strconv.Itoa(maxReason)+" characters: say why the story needs it or does not")
		case a.Needed && (a.AskedModel == "" || a.AskedEffort == "" || blankOrLong(a.AskedReason)):
			return 0, staffingNotRecorded(id, a.Agent+" is needed and has no asked_model, asked_effort or asked_reason")
		}
		if a.Needed {
			needed++
		}
	}
	return needed, nil
}

// checkAgainstPins holds the rows to what a person assigned, to what may be
// left out, and to the project's limits. A row left out is cleared of what was
// asked for.
func checkAgainstPins(s *store.Store, record *model.Record, rows []model.Staffed) error {
	for i := range rows {
		a := &rows[i]
		pin, pinned := assigned(record, a.Agent)
		switch {
		case pinned && !a.Needed:
			return personAssigned(a.Agent, "a place in the story")
		case pinned && (pin.Model != "" && pin.Model != a.AskedModel || pin.Effort != "" && pin.Effort != a.AskedEffort):
			return personAssigned(a.Agent, strings.TrimSpace("the model "+quote(pin.Model)+" and the effort "+quote(pin.Effort)))
		case !a.Needed:
			if err := refuseLeavingOut(record, a.Agent); err != nil {
				return err
			}
			a.AskedModel, a.AskedEffort, a.AskedReason = "", "", ""
		default:
			if err := refuseOutsideLimits(s.Config().Staffing, a.Agent, a.AskedModel, a.AskedEffort); err != nil {
				return err
			}
		}
	}
	return nil
}

// agentsAre begins the list of the agents a staffing may name.
const agentsAre = "the agents are "

func blankOrLong(reason string) bool {
	return strings.TrimSpace(reason) == "" || len(reason) > maxReason
}

// requireReason is a reason a command will keep: one that says something, and
// is short enough to print back to every reader after.
func requireReason(reason, what string) error {
	if blankOrLong(reason) {
		// The catalogue's fix for this code is about lifting the freeze.
		return sdlcerr.New(sdlcerr.ReasonRequired,
			what+" needs a reason",
			"the reason is what a later reader has to go on, so it says something and is at most "+
				strconv.Itoa(maxReason)+" characters").
			WithFix("give --reason with one or two sentences that say why, at most " + strconv.Itoa(maxReason) + " characters")
	}
	return nil
}

func newStaffingAmendCmd() *cobra.Command {
	var storyID, askedModel, askedEffort, reason string
	var needed, leftOut bool
	cmd := &cobra.Command{
		Use:   "amend AGENT",
		Short: "Change one agent of the staffing, with the reason (the project manager's)",
		Long: "amend changes the staffing for one agent: whether the story needs it, or the\n" +
			"model or effort it is asked for. The project manager agent runs it, when the\n" +
			"analysis is recorded and whenever an agent's work fell short or the work turned\n" +
			"out other than expected. What changed, when and why is kept and shown.\n\n" +
			"An agent brought in with --needed comes with --model and --effort. A gate\n" +
			"that has passed stays passed: a reviewer brought in is waited for from then on.\n\n" +
			"What a person assigned is not amended, and a reviewer whose latest review is a\n" +
			"block at a gate that has not passed is not left out.",
		Example: `  sdlc staffing amend perf --left-out --reason "nothing here is on a hot path"` + "\n" +
			`  sdlc staffing amend researcher --model opus --reason "its first pass missed the callers"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStaffingAmend(cmd, args[0], amendFlags{story: storyID, model: askedModel, effort: askedEffort,
				reason: reason, needed: needed, leftOut: leftOut})
		},
	}
	cmd.Flags().BoolVar(&needed, "needed", false, "the story needs the agent after all; give --model and --effort with it")
	cmd.Flags().BoolVar(&leftOut, "left-out", false, "the story does not need the agent")
	cmd.Flags().StringVar(&askedModel, "model", "", "the model to ask for, one of staffing.models")
	cmd.Flags().StringVar(&askedEffort, "effort", "", "the effort to ask for, one of staffing.efforts")
	cmd.Flags().StringVar(&reason, "reason", "", "why, from what happened or what the work is")
	cmd.Flags().StringVar(&storyID, "story", "", "the story being worked on, which is the only one a staffing is amended for")
	return cmd
}

// amendFlags are the flags `sdlc staffing amend` was given.
type amendFlags struct {
	story, model, effort, reason string
	needed, leftOut              bool
}

func runStaffingAmend(cmd *cobra.Command, agent string, f amendFlags) error {
	// The project first: with the staffing off, that is the answer,
	// whatever else the command line lacks.
	s, id, record, unlock, err := staffingStory(cmd, f.story)
	if err != nil {
		return err
	}
	defer unlock()
	if err := requireReason(f.reason, "an amendment"); err != nil {
		return err
	}
	switch {
	case !f.needed && !f.leftOut && f.model == "" && f.effort == "":
		return usage(cmd, "nothing to change: give --needed, --left-out, --model or --effort")
	case f.leftOut && (f.needed || f.model != "" || f.effort != ""):
		return usage(cmd, "--left-out takes no --needed, --model or --effort: an agent left out is asked for nothing")
	}
	if !slices.Contains(model.Agents, agent) {
		return sdlcerr.New(sdlcerr.StaffingInvalid,
			quote(agent)+" is not an agent the plugin ships",
			agentsAre+strings.Join(model.Agents, ", "))
	}
	if !staffed(record) {
		return noStaffing(id)
	}
	change, err := amendRow(cmd, s, record, agent, f)
	if err != nil {
		return err
	}
	record.Staffing.Amendments = append(record.Staffing.Amendments, model.Amendment{
		At: model.Timestamp(s.Now()), Agent: agent, Change: change, Reason: f.reason, By: "staffing amend",
	})
	if err := saveStaffing(s, record, model.EventStaffingAmend, agent+": "+change); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s  staffing amended: %s: %s\n", id, agent, change)
	return err
}

// amendRow changes the agent's row as the flags say, and returns the change as
// the amendment keeps it.
func amendRow(cmd *cobra.Command, s *store.Store, record *model.Record, agent string, f amendFlags) (string, error) {
	row := staffingRow(record, agent)
	pin, pinned := assigned(record, agent)
	switch {
	case f.leftOut && !row.Needed:
		return "", usage(cmd, agent+" is already left out: there is nothing to change")
	case f.leftOut:
		return leaveAgentOut(record, row, agent, f.reason, pinned)
	case !row.Needed && (f.model == "" || f.effort == ""):
		return "", usage(cmd, agent+" is left out, and an agent brought in comes with --model and --effort")
	case f.needed && row.Needed && f.model == "" && f.effort == "":
		return "", usage(cmd, agent+" is already needed: give --model or --effort to change what it is asked for")
	case f.model != "" && pin.Model != "" && f.model != pin.Model:
		return "", personAssigned(agent, "the model "+quote(pin.Model))
	case f.effort != "" && pin.Effort != "" && f.effort != pin.Effort:
		return "", personAssigned(agent, "the effort "+quote(pin.Effort))
	}
	to := model.Staffed{
		Agent: agent, Needed: true, Reason: row.Reason, AskedReason: f.reason,
		AskedModel: cmpOr(f.model, row.AskedModel), AskedEffort: cmpOr(f.effort, row.AskedEffort),
	}
	if err := refuseOutsideLimits(s.Config().Staffing, agent, to.AskedModel, to.AskedEffort); err != nil {
		return "", err
	}
	change := describeAsked(*row) + " -> " + describeAsked(to)
	if !row.Needed {
		to.Reason = f.reason
	}
	*row = to
	return change, nil
}

// leaveAgentOut takes the agent out of the staffing, if nothing stops it.
func leaveAgentOut(record *model.Record, row *model.Staffed, agent, reason string, pinned bool) (string, error) {
	if pinned {
		return "", personAssigned(agent, "a place in the story")
	}
	if err := refuseLeavingOut(record, agent); err != nil {
		return "", err
	}
	*row = model.Staffed{Agent: agent, Reason: reason}
	return "needed -> left out", nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// describeAsked is one side of a change: what an agent was asked for.
func describeAsked(a model.Staffed) string {
	if !a.Needed {
		return "left out"
	}
	return a.AskedModel + " at " + a.AskedEffort
}

func newStaffingAssignCmd() *cobra.Command {
	var storyID, askedModel, askedEffort, reason string
	cmd := &cobra.Command{
		Use:   "assign AGENT",
		Short: "Pin the model or effort an agent is asked for (a person's)",
		Long: "assign is how a person says what an agent is asked for on a story: the\n" +
			"model, the effort or both. The agent is then needed at every gate it has a\n" +
			"part in, and no staffing or amendment changes what was assigned. Running it\n" +
			"again changes it.\n\n" +
			"A person runs it in their own terminal, outside Claude Code: it is refused\n" +
			"wherever a Claude Code session id is set, and the hook refuses it to every\n" +
			"session. It is held to the project's limits like any staffing, and it cannot\n" +
			"leave an agent out.\n\n" +
			"What it records as a person's choice is that this command recorded it.",
		Example: `  sdlc staffing assign security --model opus --effort high --reason "this one touches the installer"`,
		// The count is checked below, after the session: checked here, a session
		// would learn from the error what the command takes.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStaffingAssign(cmd, args, assignFlags{story: storyID, model: askedModel, effort: askedEffort, reason: reason})
		},
	}
	cmd.Flags().StringVar(&askedModel, "model", "", "the model to ask for, one of staffing.models")
	cmd.Flags().StringVar(&askedEffort, "effort", "", "the effort to ask for, one of staffing.efforts")
	cmd.Flags().StringVar(&reason, "reason", "", "why")
	cmd.Flags().StringVar(&storyID, "story", "", "a story that has been started, instead of the one being worked on")
	return cmd
}

// assignFlags are the flags `sdlc staffing assign` was given.
type assignFlags struct {
	story, model, effort, reason string
}

func runStaffingAssign(cmd *cobra.Command, args []string, f assignFlags) error {
	// The hook refuses this to every session. This is the second barrier,
	// for a spelling the hook cannot read, and it comes before anything is
	// read, so the error answers no question. It holds only while the
	// session leaves its own environment alone.
	if envOf(cmd).getenv(store.SessionEnv) != "" {
		return sdlcerr.New(sdlcerr.StaffingAssignInSession,
			"sdlc staffing assign was run from inside a Claude Code session",
			"choosing a model or an effort for an agent in a person's name is a person's decision")
	}
	if err := requireReason(f.reason, "an assignment"); err != nil {
		return err
	}
	if len(args) != 1 || f.model == "" && f.effort == "" {
		return usage(cmd, "name one agent, and give --model, --effort or both")
	}
	agent := args[0]
	s, id, record, unlock, err := staffingStory(cmd, f.story)
	if err != nil {
		return err
	}
	defer unlock()
	if !slices.Contains(model.Agents, agent) {
		return sdlcerr.New(sdlcerr.StaffingInvalid,
			quote(agent)+" is not an agent the plugin ships",
			agentsAre+strings.Join(model.Agents, ", "))
	}
	if record.Staffing == nil {
		record.Staffing = &model.Staffing{}
	}
	change, err := assignAgent(cmd, s, record, agent, f)
	if err != nil {
		return err
	}
	record.Staffing.Amendments = append(record.Staffing.Amendments, model.Amendment{
		At: model.Timestamp(s.Now()), Agent: agent, Change: change, Reason: f.reason, By: "person",
	})
	if err := saveStaffing(s, record, model.EventStaffingAssign, agent+": "+change); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s  assigned by a person: %s: %s\n", id, agent, change)
	return err
}

// assignAgent pins what the agent is asked for, and returns the change as the
// amendment keeps it.
func assignAgent(cmd *cobra.Command, s *store.Store, record *model.Record, agent string, f assignFlags) (string, error) {
	row := staffingRow(record, agent)
	if (!staffed(record) || !row.Needed) && (f.model == "" || f.effort == "") {
		return "", usage(cmd, agent+" has nothing asked for yet, so give both --model and --effort")
	}
	to := model.Staffed{
		Agent: agent, Needed: true, Reason: row.Reason, AskedReason: f.reason,
		AskedModel: cmpOr(f.model, row.AskedModel), AskedEffort: cmpOr(f.effort, row.AskedEffort),
	}
	if err := refuseOutsideLimits(s.Config().Staffing, agent, to.AskedModel, to.AskedEffort); err != nil {
		return "", err
	}
	pin, _ := assigned(record, agent)
	pin = model.Assigned{Agent: agent, Model: cmpOr(f.model, pin.Model), Effort: cmpOr(f.effort, pin.Effort)}
	record.Staffing.Assigned = append(slices.DeleteFunc(record.Staffing.Assigned,
		func(a model.Assigned) bool { return a.Agent == agent }), pin)
	change := "a person assigned " + strings.TrimSpace("model "+quote(pin.Model)+" effort "+quote(pin.Effort))
	if staffed(record) {
		if !row.Needed {
			to.Reason = f.reason
		}
		change = describeAsked(*row) + " -> " + describeAsked(to)
		*row = to
	}
	return change, nil
}
