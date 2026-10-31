package cli

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/trace"
)

// The two wrong forms of sdlc trace each say which mistake was made: one
// summary for both read as though a person had done the other (human
// advocate, Gate 7, note 3).
const (
	traceUsageNeither = "sdlc trace was given no story's id and no --all, or more than one id: name one story, " +
		"as in sdlc trace US-001, or add --all for every story"
	traceUsageBoth = "sdlc trace takes a story's id or --all, not both"
)

func newTraceCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "trace [<ID>] [--all]",
		Short: "Show where a story's time and tokens went, or every story's side by side",
		Long: "trace reads a story's gate record and its trace.jsonl, and shows where its time went,\n" +
			"gate by gate: to subagents, to waiting on a person, between iterations, and the rest. With\n" +
			"the trace it shows the tokens each kind of agent used, the shell commands and test runs, the\n" +
			"refusals, the size of the change and the review rounds. --all puts every story with a\n" +
			"record side by side, with the median of each column. It only reads.",
		Example: "  sdlc trace US-112\n  sdlc trace US-112 --json\n  sdlc trace --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case all && len(args) == 0:
				return traceAll(cmd)
			case !all && len(args) == 1:
				return traceOne(cmd, args[0])
			case all:
				return usage(cmd, traceUsageBoth)
			}
			return usage(cmd, traceUsageNeither)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "every story with a record, side by side")
	return cmd
}

// ------------------------------------------------------------------ one story

// spanFigures is one part of a story's time, in seconds. A figure that is not
// recorded is null, never zero.
type spanFigures struct {
	Gate        string   `json:"gate,omitempty"`
	Wall        *float64 `json:"wall"`
	Agents      *float64 `json:"agents"`
	Person      *float64 `json:"person"`
	Between     *float64 `json:"between"`
	Unaccounted *float64 `json:"unaccounted"`
	// PastGateHours is on a gate's row when the record holds the budget event
	// for it. It reports the event, not the configuration, so it stays after
	// gate_hours is taken out.
	PastGateHours bool `json:"past_gate_hours,omitempty"`
}

type unfinishedRun struct {
	AgentType string `json:"agent_type"`
	AgentID   string `json:"agent_id"`
	Start     string `json:"start"`
}

type askedPair struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type agentTypeRow struct {
	AgentType       string                  `json:"agent_type"`
	Runs            int                     `json:"runs"`
	Time            float64                 `json:"time"`
	Models          map[string]trace.Tokens `json:"models"`
	Responses       int64                   `json:"responses"`
	PeakContext     int64                   `json:"peak_context"`
	CacheReadRatio  *float64                `json:"cache_read_ratio"`
	Tools           map[string]int64        `json:"tools"`
	Asked           []askedPair             `json:"asked"`
	ObservedModels  []string                `json:"observed_models"`
	ObservedEfforts []string                `json:"observed_efforts"`
	Done            int                     `json:"done"`
	Blocked         int                     `json:"blocked"`
}

type mainRow struct {
	Gate   string                  `json:"gate"`
	Models map[string]trace.Tokens `json:"models"`
}

type shellRow struct {
	Class   string  `json:"class"`
	Count   int     `json:"count"`
	Total   float64 `json:"total"`
	Longest float64 `json:"longest"`
}

type testRuns struct {
	Count    int     `json:"count"`
	Time     float64 `json:"time"`
	Passes   int     `json:"passes"`
	Failures int     `json:"failures"`
}

type refusalRow struct {
	Rule      string `json:"rule"`
	AgentType string `json:"agent_type"`
	Code      string `json:"code"`
	Count     int    `json:"count"`
}

type diffFigures struct {
	Files       int64 `json:"files"`
	Added       int64 `json:"added"`
	Removed     int64 `json:"removed"`
	TestFiles   int64 `json:"test_files"`
	TestAdded   int64 `json:"test_added"`
	TestRemoved int64 `json:"test_removed"`
}

type reviewRow struct {
	Gate   string `json:"gate"`
	Rounds int    `json:"rounds"`
	Blocks int    `json:"blocks"`
}

// storyTrace is `sdlc trace <ID> --json`. The measures that come from the
// trace are null for a story that has none.
type storyTrace struct {
	Story        string          `json:"story"`
	Traced       bool            `json:"traced"`
	SkippedLines int             `json:"skipped_lines"`
	Truncated    bool            `json:"truncated"`
	TruncatedAt  *string         `json:"truncated_at"`
	Total        spanFigures     `json:"total"`
	Gates        []spanFigures   `json:"gates"`
	Unfinished   []unfinishedRun `json:"unfinished"`
	AgentTypes   []agentTypeRow  `json:"agent_types"`
	Main         []mainRow       `json:"main"`
	Shell        []shellRow      `json:"shell"`
	Tests        *testRuns       `json:"tests"`
	Refusals     []refusalRow    `json:"refusals"`
	Diff         *diffFigures    `json:"diff"`
	Reviews      []reviewRow     `json:"reviews"`

	// What the text says besides: the title, the runs that ended and left no
	// measurement, the runs and turns recorded without their counts, and the
	// agent events an older sdlc saved without their id.
	title          string
	unmeasured     int
	uncountedRuns  int
	uncountedTurns int
	unpaired       int
}

// measured is one trace line as `sdlc trace` reads it: every kind's keys in
// one shape, each absent where its kind has none.
type measured struct {
	Kind        string                  `json:"kind"`
	At          string                  `json:"at"`
	AgentType   string                  `json:"agent_type"`
	AgentID     string                  `json:"agent_id"`
	Start       string                  `json:"start"`
	End         string                  `json:"end"`
	Effort      string                  `json:"effort"`
	AskedModel  string                  `json:"asked_model"`
	AskedEffort string                  `json:"asked_effort"`
	Models      map[string]trace.Tokens `json:"models"`
	Responses   *int64                  `json:"responses"`
	PeakContext int64                   `json:"peak_context"`
	Tools       map[string]int64        `json:"tools"`
	Status      string                  `json:"status"`
	Class       string                  `json:"class"`
	Outcome     string                  `json:"outcome"`
	DurationMS  *int64                  `json:"duration_ms"`
	Rule        string                  `json:"rule"`
	Code        string                  `json:"code"`
	diffFigures
}

func traceOne(cmd *cobra.Command, id string) error {
	s, _, err := openStore(cmd)
	if err != nil {
		return err
	}
	// The id becomes a path, so it is checked before anything is read
	// (THREATS E4).
	dir, err := s.StoryDir(id)
	if err != nil {
		return err
	}
	b, err := s.Backlog()
	if err != nil {
		return err
	}
	story, ok := b.Find(id)
	if !ok {
		return sdlcerr.New(sdlcerr.StoryNotFound, "no story "+quote(id)+" in the backlog",
			"sdlc trace reads the record of a story the backlog holds").
			WithFix(`"sdlc story list" shows the stories, and a story has a record once it has been started`)
	}
	if !fsx.Exists(filepath.Join(dir, model.RecordFile)) {
		return sdlcerr.New(sdlcerr.StoryNotFound,
			id+" is in the backlog but has not been started, so it has no record to trace",
			"a story's record is made when it is started").
			WithFix(`"sdlc story list" shows each story's status, and a story has a record once it has been started`)
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	reading, traced, err := readTrace(dir, id)
	if err != nil {
		return err
	}
	st := storyReport(id, replayOf(record, traced), record, reading, traced)
	st.title = story.Title
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), st)
	}
	return writeStoryTrace(cmd.OutOrStdout(), st)
}

// readTrace reads a story's trace. One that is not there is a story that
// predates tracing; one that is there and cannot be read is said to be, and
// never read as missing, which would say something false.
func readTrace(dir, id string) (trace.Reading, bool, error) {
	reading, err := trace.Read(dir)
	switch {
	case err == nil:
		return reading, true, nil
	case errors.Is(err, fs.ErrNotExist):
		return trace.Reading{}, false, nil
	}
	return trace.Reading{}, false, sdlcerr.New(sdlcerr.StateUnreadable,
		"the trace of "+id+" could not be read",
		"it is there, and it is not one regular file sdlc can read").
		WithFix("check that .sdlc/stories/" + id + "/" + model.TraceFile + " is one regular file you can read, " +
			"not a directory, a link or a named pipe; sdlc only ever appends to it").WithCause(err)
}

// storyReport is a story's figures, from its record and its trace.
func storyReport(id string, rp *replay, record *model.Record, reading trace.Reading, traced bool) storyTrace {
	lines, bad := measuredLines(reading.Records)
	st := storyTrace{
		Story: id, Traced: traced, SkippedLines: reading.Skipped + bad, Truncated: reading.Truncated,
		Total:      figuresOf("", rp.splitOf(rp.whole), rp.agentsKnown, rp.agentsKnown),
		Gates:      []spanFigures{},
		Unfinished: []unfinishedRun{},
		Reviews:    reviewRows(record),
		unpaired:   rp.unpaired,
	}
	if reading.TruncatedAt != "" {
		st.TruncatedAt = &reading.TruncatedAt
	}
	// A gate's rest is the time in it that nothing the record holds accounts
	// for, and is given whether or not the story was traced. The story's own
	// rest is left out where its subagents' time is not recorded (AC-21).
	gates, splits := rp.byGate()
	marks := budgetMarks(record)
	for i, g := range gates {
		f := figuresOf(string(g), splits[i], rp.agentsKnown, true)
		f.PastGateHours = marks[g]
		st.Gates = append(st.Gates, f)
	}
	for _, u := range rp.unfinished {
		st.Unfinished = append(st.Unfinished, unfinishedRun{u.agentType, u.agentID, trace.Stamp(u.at)})
	}
	if !traced {
		return st
	}
	st.AgentTypes = agentTypeRows(lines, rp)
	st.Main = mainRows(lines, rp)
	st.Shell, st.Tests = shellRows(lines)
	st.Refusals = refusalRows(lines)
	st.Diff = lastDiff(lines)
	st.unmeasured, st.uncountedRuns, st.uncountedTurns = unmeasuredOf(lines, rp)
	return st
}

// measuredLines is each record in measured's shape. A record whose fields
// will not take it is counted with the lines skipped.
func measuredLines(records []trace.Record) (lines []measured, bad int) {
	for _, r := range records {
		raw, err := json.Marshal(r)
		var m measured
		if err != nil || json.Unmarshal(raw, &m) != nil {
			bad++
			continue
		}
		lines = append(lines, m)
	}
	return lines, bad
}

func secondsOf(d time.Duration) *float64 {
	s := d.Seconds()
	return &s
}

func figuresOf(gate string, s split, agentsKnown, restKnown bool) spanFigures {
	f := spanFigures{Gate: gate, Wall: secondsOf(s.wall), Person: secondsOf(s.person), Between: secondsOf(s.between)}
	if agentsKnown {
		f.Agents = secondsOf(s.agents)
	}
	if restKnown {
		f.Unaccounted = secondsOf(s.rest)
	}
	return f
}

// agentTypeRows is the cost of each kind of agent, from its runs.
func agentTypeRows(lines []measured, rp *replay) []agentTypeRow {
	rows := []agentTypeRow{}
	index := map[string]int{}
	for _, l := range lines {
		if l.Kind != trace.KindAgentRun {
			continue
		}
		i, ok := index[l.AgentType]
		if !ok {
			i = len(rows)
			index[l.AgentType] = i
			rows = append(rows, agentTypeRow{
				AgentType: l.AgentType, Models: map[string]trace.Tokens{}, Tools: map[string]int64{},
				Asked: []askedPair{}, ObservedModels: []string{}, ObservedEfforts: []string{},
			})
		}
		rows[i].add(l, rp)
	}
	for i := range rows {
		rows[i].finish()
	}
	slices.SortFunc(rows, func(a, b agentTypeRow) int { return cmp.Compare(a.AgentType, b.AgentType) })
	return rows
}

func (a *agentTypeRow) add(l measured, rp *replay) {
	a.Runs++
	a.Time += runTime(l, rp).Seconds()
	addTokens(a.Models, l.Models)
	if l.Responses != nil {
		a.Responses += *l.Responses
	}
	a.PeakContext = max(a.PeakContext, l.PeakContext)
	for name, n := range l.Tools {
		a.Tools[name] += n
	}
	if asked := (askedPair{l.AskedModel, l.AskedEffort}); asked != (askedPair{}) && !slices.Contains(a.Asked, asked) {
		a.Asked = append(a.Asked, asked)
	}
	for name := range l.Models {
		if !slices.Contains(a.ObservedModels, name) {
			a.ObservedModels = append(a.ObservedModels, name)
		}
	}
	if l.Effort != "" && !slices.Contains(a.ObservedEfforts, l.Effort) {
		a.ObservedEfforts = append(a.ObservedEfforts, l.Effort)
	}
	switch l.Status {
	case "done":
		a.Done++
	case "blocked":
		a.Blocked++
	}
}

// finish works out what is worked out from the sums: the share of the input
// read from the cache, of every input token, where there was any.
func (a *agentTypeRow) finish() {
	var read, in int64
	for _, t := range a.Models {
		read += t.CacheRead
		in += t.Input + t.CacheRead + t.CacheWritten
	}
	if in > 0 {
		ratio := float64(read) / float64(in)
		a.CacheReadRatio = &ratio
	}
	slices.Sort(a.ObservedModels)
	slices.Sort(a.ObservedEfforts)
}

// runTime is how long a run took: from its own start and end, else from the
// agent events on the gate record with its id.
func runTime(l measured, rp *replay) time.Duration {
	start, err1 := time.Parse(time.RFC3339Nano, l.Start)
	end, err2 := time.Parse(time.RFC3339Nano, l.End)
	if err1 == nil && err2 == nil && end.After(start) {
		return end.Sub(start)
	}
	return rp.runs[l.AgentID].length()
}

func addTokens(into, from map[string]trace.Tokens) {
	for name, t := range from {
		sum := into[name]
		sum.Input += t.Input
		sum.CacheRead += t.CacheRead
		sum.CacheWritten += t.CacheWritten
		sum.Output += t.Output
		into[name] = sum
	}
}

// mainRows is the main conversation's tokens, by the gate in force when each
// turn ended.
func mainRows(lines []measured, rp *replay) []mainRow {
	byGate := map[model.Gate]map[string]trace.Tokens{}
	for _, l := range lines {
		at, err := time.Parse(time.RFC3339Nano, l.At)
		if l.Kind != trace.KindTurn || err != nil {
			continue
		}
		g := rp.gateAt(at)
		if byGate[g] == nil {
			byGate[g] = map[string]trace.Tokens{}
		}
		addTokens(byGate[g], l.Models)
	}
	rows := []mainRow{}
	for _, g := range append(slices.Clone(model.Gates), "") {
		if models, ok := byGate[g]; ok {
			rows = append(rows, mainRow{Gate: string(g), Models: models})
		}
	}
	return rows
}

// shellRows is the shell commands by class, and the test runs among them.
func shellRows(lines []measured) ([]shellRow, *testRuns) {
	rows := []shellRow{}
	totals := map[string]int64{}
	tests := &testRuns{}
	var testTime int64
	for _, l := range lines {
		if l.Kind != "shell" {
			continue
		}
		i := slices.IndexFunc(rows, func(r shellRow) bool { return r.Class == l.Class })
		if i < 0 {
			i = len(rows)
			rows = append(rows, shellRow{Class: l.Class})
		}
		rows[i].Count++
		if l.DurationMS != nil {
			totals[l.Class] += *l.DurationMS
			rows[i].Longest = max(rows[i].Longest, float64(*l.DurationMS)/1000)
		}
		if l.Class != shellpolicy.ClassTest {
			continue
		}
		tests.Count++
		if l.DurationMS != nil {
			testTime += *l.DurationMS
		}
		switch l.Outcome {
		case "pass":
			tests.Passes++
		case "fail":
			tests.Failures++
		}
	}
	for i := range rows {
		rows[i].Total = float64(totals[rows[i].Class]) / 1000
	}
	tests.Time = float64(testTime) / 1000
	slices.SortFunc(rows, func(a, b shellRow) int { return cmp.Compare(a.Class, b.Class) })
	return rows, tests
}

// refusalRows counts the refusals by rule, agent type and code.
func refusalRows(lines []measured) []refusalRow {
	rows := []refusalRow{}
	for _, l := range lines {
		if l.Kind != "refusal" {
			continue
		}
		key := refusalRow{Rule: l.Rule, AgentType: l.AgentType, Code: l.Code}
		i := slices.IndexFunc(rows, func(r refusalRow) bool {
			return r.Rule == key.Rule && r.AgentType == key.AgentType && r.Code == key.Code
		})
		if i < 0 {
			i = len(rows)
			rows = append(rows, key)
		}
		rows[i].Count++
	}
	slices.SortFunc(rows, func(a, b refusalRow) int {
		return cmp.Or(cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.AgentType, b.AgentType), cmp.Compare(a.Code, b.Code))
	})
	return rows
}

// lastDiff is the change the commit gate last measured, or nil where it
// measured none.
func lastDiff(lines []measured) *diffFigures {
	for _, l := range slices.Backward(lines) {
		if l.Kind == "diff" {
			d := l.diffFigures
			return &d
		}
	}
	return nil
}

// reviewRows is each gate's review rounds and blocks, in the loop's order.
func reviewRows(record *model.Record) []reviewRow {
	rows := []reviewRow{}
	for _, g := range model.Gates {
		row := reviewRow{Gate: string(g)}
		found := false
		for _, r := range record.Reviews {
			if r.Gate != g {
				continue
			}
			found = true
			row.Rounds = max(row.Rounds, r.Round)
			if r.Verdict == model.VerdictBlock {
				row.Blocks++
			}
		}
		if found {
			rows = append(rows, row)
		}
	}
	return rows
}

// unmeasuredOf counts what the trace should hold and does not: subagent runs
// that ended with no agent_run, and runs and turns recorded without counts.
func unmeasuredOf(lines []measured, rp *replay) (unmeasured, uncountedRuns, uncountedTurns int) {
	ran := map[string]bool{}
	for _, l := range lines {
		switch {
		case l.Kind == trace.KindAgentRun:
			ran[l.AgentID] = true
			if l.Responses == nil {
				uncountedRuns++
			}
		case l.Kind == trace.KindTurn && l.Responses == nil:
			uncountedTurns++
		}
	}
	for id := range rp.endedIDs {
		if !ran[id] {
			unmeasured++
		}
	}
	return unmeasured, uncountedRuns, uncountedTurns
}

// ------------------------------------------------------------------ text

// humanTime is a duration as a person reads it: hours and two-digit minutes
// from an hour up, minutes and seconds below it, seconds alone below a
// minute.
func humanTime(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", d/time.Hour, d%time.Hour/time.Minute)
	case d >= time.Minute && d%time.Minute/time.Second == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%ds", d/time.Minute, d%time.Minute/time.Second)
	}
	return fmt.Sprintf("%gs", d.Round(time.Millisecond).Seconds())
}

// shown is a figure in seconds as the text gives it, or "not recorded".
func shown(seconds *float64) string {
	if seconds == nil {
		return unrecorded
	}
	return humanTime(time.Duration(*seconds * float64(time.Second)))
}

const unrecorded = "not recorded"

func writeStoryTrace(w io.Writer, st storyTrace) error {
	p := &printer{w: w}
	p.linef("%s  %s", st.Story, st.title)
	writeTraceState(p, st)
	writeTimeTable(p, st)
	if st.Traced {
		writeAgentTypes(p, st.AgentTypes)
		writeMeasures(p, st)
	}
	writeReviews(p, st.Reviews)
	return p.err
}

func writeTraceState(p *printer, st storyTrace) {
	if !st.Traced {
		p.linef("no %s for this story: it predates tracing, or nothing was recorded "+
			`(see "sdlc trace shows nothing was recorded" in the troubleshooting guide)`, model.TraceFile)
	}
	if st.SkippedLines > 0 {
		p.linef("%d lines of the trace were skipped: they could not be read, or were written by a newer sdlc", st.SkippedLines)
	}
	if st.Truncated {
		after := "the 4 MiB limit"
		if st.TruncatedAt != nil {
			after = *st.TruncatedAt
		}
		p.linef("records after %s are missing: the trace reached 4 MiB. To record again, a person must move "+
			".sdlc/stories/%s/%s aside (sdlc never deletes it); sdlc trace then shows only what is recorded after that",
			after, st.Story, model.TraceFile)
	}
	if st.unmeasured > 0 {
		p.linef("%d subagent runs ended and left no measurement: their tokens were not recorded", st.unmeasured)
	}
	if st.uncountedRuns+st.uncountedTurns > 0 {
		p.linef("%d subagent runs and %d turns of the main conversation were recorded without their tokens: "+
			"the transcript could not be read, and the hook's debug log says why", st.uncountedRuns, st.uncountedTurns)
	}
	if st.unpaired > 0 {
		p.linef("%d agent events have no agent id, so their runs cannot be paired: an older sdlc saved the gate "+
			"record and dropped it; run the same sdlc release for the hook and the agents", st.unpaired)
	}
}

func writeTimeTable(p *printer, st storyTrace) {
	p.linef("")
	p.linef("time, by the gate the story was working towards:")
	row := func(name string, f spanFigures) {
		mark := ""
		if f.PastGateHours {
			mark = "  <- past budget.gate_hours"
		}
		p.linef("  %-16s %-12s %-12s %-20s %-19s %s%s", name, shown(f.Wall), shown(f.Agents), shown(f.Person),
			shown(f.Between), shown(f.Unaccounted), mark)
	}
	p.linef("  %-16s %-12s %-12s %-20s %-19s %s", "gate", "wall", "agents", "waiting on a person",
		"between iterations", "no agent and no person accounts for")
	for _, g := range st.Gates {
		row(g.Gate, g)
	}
	row("in all", st.Total)
	if !st.Traced {
		// The verifier read a gate's last column as wrong for such a story: it
		// is the wall less what the record holds, so it says so.
		p.linef("  with no agent's time recorded, a gate's last column holds its agents' time as well")
	}
	for _, u := range st.Unfinished {
		p.linef("  unfinished: %s %s, started %s and never ended, so it is left out", cmp.Or(u.AgentType, "an agent"), u.AgentID, u.Start)
	}
}

func writeAgentTypes(p *printer, rows []agentTypeRow) {
	p.linef("")
	if len(rows) == 0 {
		p.linef("by kind of agent: no subagent run was recorded")
		return
	}
	p.linef("by kind of agent:")
	for _, a := range rows {
		ratio := unrecorded
		if a.CacheReadRatio != nil {
			ratio = fmt.Sprintf("%.0f%%", *a.CacheReadRatio*100)
		}
		p.linef("  %s: %d %s, %s, %d %s, the largest context %d tokens, read from the cache %s",
			cmp.Or(a.AgentType, "an agent of no recorded kind"), a.Runs, plural(a.Runs, "run", "runs"),
			humanTime(time.Duration(a.Time*float64(time.Second))),
			a.Responses, plural(int(a.Responses), "response", "responses"), a.PeakContext, ratio)
		p.linef("    tokens: %s", tokensText(a.Models))
		p.linef("    tools: %s", countsText(a.Tools))
		var asked []string
		for _, x := range a.Asked {
			asked = append(asked, cmp.Or(x.Model, "any model")+" at "+cmp.Or(x.Effort, "any effort"))
		}
		p.linef("    asked for: %s; ran on %s at %s", listText(asked, "nothing named"),
			listText(a.ObservedModels, "no model recorded"), listText(a.ObservedEfforts, "no effort recorded"))
		p.linef("    handed back: %d done, %d blocked", a.Done, a.Blocked)
	}
}

func writeMeasures(p *printer, st storyTrace) {
	p.linef("")
	p.linef("the main conversation, by gate:")
	for _, m := range st.Main {
		p.linef("  %s: %s", cmp.Or(m.Gate, "after the last gate"), tokensText(m.Models))
	}
	p.linef("shell commands, by class:")
	for _, s := range st.Shell {
		p.linef("  %s: %d, %s in all, the longest %s", s.Class, s.Count,
			humanTime(time.Duration(s.Total*float64(time.Second))), humanTime(time.Duration(s.Longest*float64(time.Second))))
	}
	p.linef("test runs: %d, %s in all, %d passed, %d failed", st.Tests.Count,
		humanTime(time.Duration(st.Tests.Time*float64(time.Second))), st.Tests.Passes, st.Tests.Failures)
	p.linef("refusals, by rule, agent and code:")
	for _, r := range st.Refusals {
		p.linef("  %s, %s%s: %d", r.Rule, cmp.Or(r.AgentType, "the main conversation"), prefixed(", ", r.Code), r.Count)
	}
	if d := st.Diff; d != nil {
		p.linef("the change at the commit gate: %d files, %d lines added and %d removed; of them the frozen tests "+
			"%d files, %d added and %d removed", d.Files, d.Added, d.Removed, d.TestFiles, d.TestAdded, d.TestRemoved)
	} else {
		p.linef("the change at the commit gate: %s", unrecorded)
	}
}

func writeReviews(p *printer, rows []reviewRow) {
	if len(rows) == 0 {
		return
	}
	p.linef("")
	p.linef("reviews, by gate:")
	for _, r := range rows {
		p.linef("  %s: %d %s, %d %s", r.Gate, r.Rounds, plural(r.Rounds, "round", "rounds"),
			r.Blocks, plural(r.Blocks, "block", "blocks"))
	}
}

func prefixed(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

func tokensText(models map[string]trace.Tokens) string {
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(models)) {
		t := models[name]
		parts = append(parts, fmt.Sprintf("%s %d in, %d read from the cache, %d written to it, %d out",
			name, t.Input, t.CacheRead, t.CacheWritten, t.Output))
	}
	return listText(parts, "none recorded")
}

func countsText(counts map[string]int64) string {
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%s %d", name, counts[name]))
	}
	return listText(parts, "none recorded")
}

func listText(items []string, none string) string {
	if len(items) == 0 {
		return none
	}
	return strings.Join(items, "; ")
}

// printer writes lines and keeps the first error.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) linef(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format+"\n", args...)
	}
}

// ------------------------------------------------------------------ --all

// allTraceRow is one story of `sdlc trace --all --json`.
type allTraceRow struct {
	Story             string         `json:"story"`
	Start             string         `json:"start"`
	End               *string        `json:"end"`
	Wall              *float64       `json:"wall"`
	Agents            *float64       `json:"agents"`
	Person            *float64       `json:"person"`
	Escalations       map[string]int `json:"escalations"`
	Unfreezes         int            `json:"unfreezes"`
	ReviewRounds      map[string]int `json:"review_rounds"`
	FirstPass         *bool          `json:"first_pass"`
	DesignToEnd       *float64       `json:"design_to_end"`
	FailedAfterCommit bool           `json:"failed_after_commit"`
	Tokens            *float64       `json:"tokens"`
	PeakContext       *float64       `json:"peak_context"`
	TestTime          *float64       `json:"test_time"`
	Refusals          *float64       `json:"refusals"`
	DiffFiles         *float64       `json:"diff_files"`
	DiffLines         *float64       `json:"diff_lines"`
	SkippedLines      int            `json:"skipped_lines"`
	Truncated         bool           `json:"truncated"`
}

type allTrace struct {
	Stories        []allTraceRow       `json:"stories"`
	Medians        map[string]*float64 `json:"medians"`
	FirstPassShare *float64            `json:"first_pass_share"`
}

// medianColumns are the columns --all takes a median of, in the text's order.
// columnPerson is the wait on a person, as a column of --all is keyed.
const columnPerson = "person"

var medianColumns = []string{"wall", "agents", columnPerson, "design_to_end", "tokens", "peak_context",
	"test_time", "refusals", "diff_files", "diff_lines"}

func traceAll(cmd *cobra.Command) error {
	s, _, err := openStore(cmd)
	if err != nil {
		return err
	}
	b, err := s.Backlog()
	if err != nil {
		return err
	}
	doc := allTrace{Stories: []allTraceRow{}}
	for _, story := range b.Stories {
		row, ok, err := allRowOf(cmd, s, story.ID)
		if err != nil {
			return err
		}
		if ok {
			doc.Stories = append(doc.Stories, row)
		}
	}
	doc.Medians, doc.FirstPassShare = mediansOf(doc.Stories)
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), doc)
	}
	return writeAllTrace(cmd.OutOrStdout(), doc)
}

// allRowOf is one story's row, and false for a story with no record. A trace
// that cannot be read leaves the row with what the record holds, and says so
// on stderr: one bad file does not take every other story's row with it.
func allRowOf(cmd *cobra.Command, s *store.Store, id string) (allTraceRow, bool, error) {
	dir, err := s.StoryDir(id)
	if err != nil || !fsx.Exists(filepath.Join(dir, model.RecordFile)) {
		return allTraceRow{}, false, nil //nolint:nilerr // a backlog id that is no directory has no record to show
	}
	record, err := s.Record(id)
	if err != nil {
		return allTraceRow{}, false, err
	}
	reading, traced, err := readTrace(dir, id)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "sdlc: the trace of %s could not be read, so its row shows only what its "+
			"gate record holds; \"sdlc trace %s\" says why\n", id, id)
	}
	rp := replayOf(record, traced)
	st := storyReport(id, rp, record, reading, traced)
	lines, _ := measuredLines(reading.Records)
	row := allTraceRow{
		Story: id, Start: trace.Stamp(rp.whole.from), Wall: st.Total.Wall, Agents: st.Total.Agents, Person: st.Total.Person,
		Escalations: map[string]int{}, ReviewRounds: map[string]int{}, FirstPass: firstPass(record, rp.ended),
		FailedAfterCommit: failedAfterCommit(rp), SkippedLines: st.SkippedLines, Truncated: st.Truncated,
	}
	if rp.ended {
		end := trace.Stamp(rp.whole.to)
		row.End = &end
		row.DesignToEnd = designToEnd(rp)
	}
	for _, e := range record.Escalations {
		row.Escalations[e.Type]++
	}
	for _, e := range record.Events {
		if e.Type == unfreezeEvent {
			row.Unfreezes++
		}
	}
	for _, r := range st.Reviews {
		row.ReviewRounds[r.Gate] = r.Rounds
	}
	if traced {
		addTracedColumns(&row, lines, st)
	}
	return row, true, nil
}

func addTracedColumns(row *allTraceRow, lines []measured, st storyTrace) {
	var tokens, peak int64
	for _, l := range lines {
		if l.Kind != trace.KindAgentRun && l.Kind != trace.KindTurn {
			continue
		}
		for _, t := range l.Models {
			tokens += t.Input + t.CacheRead + t.CacheWritten + t.Output
		}
		peak = max(peak, l.PeakContext)
	}
	refusals := 0
	for _, r := range st.Refusals {
		refusals += r.Count
	}
	row.Tokens, row.PeakContext = new(float64(tokens)), new(float64(peak))
	row.TestTime, row.Refusals = new(st.Tests.Time), new(float64(refusals))
	if d := st.Diff; d != nil {
		row.DiffFiles, row.DiffLines = new(float64(d.Files)), new(float64(d.Added+d.Removed))
	}
}

// firstPass is whether Gates 6 and 7 passed first time: no block at the
// verifier's review or the code review, and none of their gates failed, the
// verification itself among them (red team, V3). A block is the answer at
// once, ended or not (red team, V18); with none, a story under way has no
// answer yet, since a script that read true took it for a verdict (human
// advocate, Gate 7, note 1).
func firstPass(record *model.Record, ended bool) *bool {
	gates := []model.Gate{model.GateVerification, model.GateVerifierReview, model.GateCodeReview}
	for _, r := range record.Reviews {
		if r.Verdict == model.VerdictBlock && slices.Contains(gates, r.Gate) {
			return new(false)
		}
	}
	for _, e := range record.Events {
		for _, g := range gates {
			if e.Type == model.EventGate && e.Message == model.GateEvent(g, model.GateFail) {
				return new(false)
			}
		}
	}
	if !ended {
		return nil
	}
	return new(true)
}

// failedAfterCommit is whether a gate failed after the commit gate first
// passed.
func failedAfterCommit(rp *replay) bool {
	committed := false
	for _, e := range rp.events {
		switch {
		case e.Type != model.EventGate:
		case e.Message == model.GateEvent(model.GateCommit, model.GatePass):
			committed = true
		case committed && strings.HasSuffix(e.Message, " "+string(model.GateFail)):
			return true
		}
	}
	return false
}

// designToEnd is the time from the last design review pass to the end.
func designToEnd(rp *replay) *float64 {
	for _, e := range slices.Backward(rp.events) {
		if e.Type == model.EventGate && e.Message == model.GateEvent(model.GateDesignReview, model.GatePass) {
			return secondsOf(max(rp.whole.to.Sub(e.at), 0))
		}
	}
	return nil
}

// mediansOf is each column's median over the stories that ended and have the
// figure, and the share of those stories that passed first time. A story
// still under way is in none of them.
func mediansOf(rows []allTraceRow) (map[string]*float64, *float64) {
	medians := map[string]*float64{}
	ended := 0
	first := 0
	columns := map[string][]float64{}
	for _, r := range rows {
		if r.End == nil {
			continue
		}
		ended++
		if r.FirstPass != nil && *r.FirstPass {
			first++
		}
		for name, v := range r.columns() {
			if v != nil {
				columns[name] = append(columns[name], *v)
			}
		}
	}
	for _, name := range medianColumns {
		medians[name] = median(columns[name])
	}
	if ended == 0 {
		return medians, nil
	}
	return medians, new(float64(first) / float64(ended))
}

func (r allTraceRow) columns() map[string]*float64 {
	return map[string]*float64{
		"wall": r.Wall, "agents": r.Agents, columnPerson: r.Person, "design_to_end": r.DesignToEnd, "tokens": r.Tokens,
		"peak_context": r.PeakContext, "test_time": r.TestTime, "refusals": r.Refusals,
		"diff_files": r.DiffFiles, "diff_lines": r.DiffLines,
	}
}

func median(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	slices.Sort(values)
	n := len(values)
	if n%2 == 1 {
		return new(values[n/2])
	}
	return new((values[n/2-1] + values[n/2]) / 2)
}

func writeAllTrace(w io.Writer, doc allTrace) error {
	p := &printer{w: w}
	if len(doc.Stories) == 0 {
		p.linef(`no story has a record yet: a story gets one when it is started ("sdlc start <ID>")`)
		return p.err
	}
	const format = "%-10s %-25s %-25s %-12s %-12s %-20s %-21s %-14s %-16s %-12s %-12s %-12s %-12s %s"
	p.linef(format, "story", "start", "end", "wall", "agents", "waiting on a person", "design review to end", "tokens",
		"largest context", "test time", "refusals", "diff files", "diff lines", "first pass")
	gates := make([]string, len(model.Gates))
	for i, g := range model.Gates {
		gates[i] = string(g)
	}
	for _, r := range doc.Stories {
		// A story under way has no end to measure to, and with no block yet
		// has not passed first time; it may still be blocked.
		end, toEnd, first := "in progress", "not yet", "not yet"
		if r.End != nil {
			end, toEnd = *r.End, shown(r.DesignToEnd)
		}
		if r.FirstPass != nil {
			first = yesNo(*r.FirstPass)
		}
		p.linef(format, r.Story, r.Start, end, shown(r.Wall), shown(r.Agents), shown(r.Person), toEnd, count(r.Tokens),
			count(r.PeakContext), shown(r.TestTime), count(r.Refusals), count(r.DiffFiles), count(r.DiffLines), first)
		// What the gate record counts goes on a line of its own under the row,
		// without the story's id: a table twenty columns wide is not read.
		p.linef("%-10s escalations: %s; unfreezes: %d; review rounds: %s; a gate failed after the commit gate: %s", "",
			namedCounts(r.Escalations, slices.Sorted(maps.Keys(r.Escalations))), r.Unfreezes,
			namedCounts(r.ReviewRounds, gates), yesNo(r.FailedAfterCommit))
	}
	m := doc.Medians
	p.linef("%s", strings.TrimRight(fmt.Sprintf(format, "median", "", "", shown(m["wall"]), shown(m["agents"]),
		shown(m[columnPerson]), shown(m["design_to_end"]), count(m["tokens"]), count(m["peak_context"]),
		shown(m["test_time"]), count(m["refusals"]), count(m["diff_files"]), count(m["diff_lines"]), ""), " "))
	if doc.FirstPassShare != nil {
		p.linef("first-pass success, of the stories that ended: %.0f%%", *doc.FirstPassShare*100)
	}
	p.linef("a story still in progress is left out of the medians; %s is a figure its story did not record", unrecorded)
	return p.err
}

func count(v *float64) string {
	if v == nil {
		return unrecorded
	}
	return fmt.Sprintf("%.0f", *v)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// namedCounts is each of names that has a count, with it, in names' order, or
// "none".
func namedCounts(counts map[string]int, names []string) string {
	var parts []string
	for _, name := range names {
		if n, ok := counts[name]; ok {
			parts = append(parts, fmt.Sprintf("%s %d", name, n))
		}
	}
	if len(parts) == 0 {
		return noCounts
	}
	return strings.Join(parts, ", ")
}

// noCounts is what namedCounts gives where nothing was counted.
const noCounts = "none"

// pathless is an error's text for a debug line, with no path in it. Where the
// chain holds an *fs.PathError, as it is, wrapped by fmt or under an sdlcerr
// cause, it is that error's operation and inner error and nothing of the chain
// around it, since a wrapper's own text may repeat the path (THREATS I4). Its
// callers rename and link nothing, so no *os.LinkError reaches it.
func pathless(err error) string {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Op + ": " + pe.Err.Error()
	}
	return err.Error()
}
