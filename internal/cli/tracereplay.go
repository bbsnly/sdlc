package cli

// The replay `sdlc trace` makes of a story's gate record: where its time went,
// split between subagents, a person and the time between iterations, and
// charged to the gate the story was working towards at each instant (US-112,
// AC-18).

import (
	"slices"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// period is a stretch of time, from its start up to its end.
type period struct{ from, to time.Time }

func (p period) length() time.Duration { return max(p.to.Sub(p.from), 0) }

// merged is periods as one sorted set, overlaps joined, so time that two of
// them hold is counted once.
func merged(ps []period) []period {
	ps = slices.DeleteFunc(slices.Clone(ps), func(p period) bool { return !p.to.After(p.from) })
	slices.SortFunc(ps, func(a, b period) int { return a.from.Compare(b.from) })
	var out []period
	for _, p := range ps {
		if n := len(out); n > 0 && !p.from.After(out[n-1].to) {
			if p.to.After(out[n-1].to) {
				out[n-1].to = p.to
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

// within is the part of a merged set that falls in w.
func within(ps []period, w period) []period {
	var out []period
	for _, p := range ps {
		from, to := maxTime(p.from, w.from), minTime(p.to, w.to)
		if to.After(from) {
			out = append(out, period{from, to})
		}
	}
	return out
}

// less is a merged set less another.
func less(ps, cut []period) []period {
	var out []period
	for _, p := range ps {
		rest := []period{p}
		for _, c := range cut {
			var next []period
			for _, r := range rest {
				next = append(next, r.without(c)...)
			}
			rest = next
		}
		out = append(out, rest...)
	}
	return out
}

// without is the period with c taken out: itself, the parts either side of
// c, or nothing.
func (p period) without(c period) []period {
	if !c.from.Before(p.to) || !c.to.After(p.from) {
		return []period{p}
	}
	var out []period
	if c.from.After(p.from) {
		out = append(out, period{p.from, c.from})
	}
	if c.to.Before(p.to) {
		out = append(out, period{c.to, p.to})
	}
	return out
}

func total(ps []period) time.Duration {
	var d time.Duration
	for _, p := range ps {
		d += p.length()
	}
	return d
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// stretch is a part of the story's time and the gate in force over it: the
// first in the loop's order not passed, or none once every gate has.
type stretch struct {
	period
	gate model.Gate
}

// openRun is an agent_start no agent_end answered.
type openRun struct {
	agentType, agentID string
	at                 time.Time
}

// replay is a story's gate record, walked.
type replay struct {
	whole period
	// ended is whether the story landed or passed its commit gate; a story
	// that did neither is still under way, measured to its last instant.
	ended     bool
	stretches []stretch
	// agents is known where the story was traced or its record holds an agent
	// event: a story from before tracing has no subagents' time to give.
	agents      []period
	agentsKnown bool
	person      []period
	between     []period
	// runs are the paired agent events by agent id, the last of each.
	runs       map[string]period
	unfinished []openRun
	endedIDs   map[string]bool
	// unpaired is agent events with no agent id: an older sdlc that saved the
	// record dropped it.
	unpaired int
	events   []timedEvent
}

// timedEvent is an event of the record with its time read.
type timedEvent struct {
	model.Event
	at time.Time
}

// replayOf walks the record. traced is whether the story has a trace.
func replayOf(record *model.Record, traced bool) *replay {
	events := timedEvents(record)
	rp := &replay{runs: map[string]period{}, endedIDs: map[string]bool{}, events: events}
	rp.whole.from, rp.whole.to, rp.ended = storyBounds(record, events)
	rp.stretches = gateStretches(events, rp.whole)
	rp.pairAgents(events)
	rp.agentsKnown = traced || len(rp.runs) > 0 || len(rp.unfinished) > 0 || rp.unpaired > 0
	rp.person = merged(within(spansBetween(events, model.EventEscalate, model.EventApproval, rp.whole.to), rp.whole))
	rp.between = less(merged(within(spansBetween(events, model.EventLoopEnd, model.EventLoopStart, rp.whole.to), rp.whole)), rp.person)
	return rp
}

// timedEvents is the record's events with a time that reads, in order of it.
func timedEvents(record *model.Record) []timedEvent {
	var out []timedEvent
	for _, e := range record.Events {
		if at, err := time.Parse(time.RFC3339, e.At); err == nil {
			out = append(out, timedEvent{e, at})
		}
	}
	slices.SortStableFunc(out, func(a, b timedEvent) int { return a.at.Compare(b.at) })
	return out
}

// storyBounds is when the story began and ended: its record's creation, and
// its landing, else its last commit gate pass, else its last event.
func storyBounds(record *model.Record, events []timedEvent) (from, to time.Time, ended bool) {
	from, err := time.Parse(time.RFC3339, record.Created)
	if err != nil && len(events) > 0 {
		from = events[0].at
	}
	to = from
	var landed, committed time.Time
	for _, e := range events {
		to = maxTime(to, e.at)
		switch {
		case e.Type == model.EventLanded:
			landed = e.at
		case e.Type == model.EventGate && e.Message == model.GateEvent(model.GateCommit, model.GatePass):
			committed = e.at
		}
	}
	switch {
	case !landed.IsZero():
		return from, maxTime(from, landed), true
	case !committed.IsZero():
		return from, maxTime(from, committed), true
	}
	return from, to, false
}

// gateStretches cuts the story's time at each gate recorded and each reopen,
// so that every part is charged to the gate in force then.
func gateStretches(events []timedEvent, whole period) []stretch {
	passed := map[model.Gate]bool{}
	inForce := func() model.Gate {
		for _, g := range model.Gates {
			if !passed[g] {
				return g
			}
		}
		return ""
	}
	var out []stretch
	cur, gate := whole.from, inForce()
	for _, e := range events {
		if !changesGates(e.Event) {
			continue
		}
		at := minTime(maxTime(e.at, whole.from), whole.to)
		if at.After(cur) {
			out = append(out, stretch{period{cur, at}, gate})
			cur = at
		}
		applyGateEvent(passed, e.Event)
		gate = inForce()
	}
	if whole.to.After(cur) {
		out = append(out, stretch{period{cur, whole.to}, gate})
	}
	return out
}

func changesGates(e model.Event) bool {
	return e.Type == model.EventGate || e.Type == model.EventGatesReopened
}

// applyGateEvent is what a gate's outcome or a reopen does to the gates
// passed: a gate recorded reopens every gate after it, as SetGate does.
func applyGateEvent(passed map[model.Gate]bool, e model.Event) {
	if e.Type == model.EventGatesReopened {
		names, _, _ := strings.Cut(e.Message, " reopened")
		for name := range strings.SplitSeq(names, ",") {
			passed[model.Gate(strings.TrimSpace(name))] = false
		}
		return
	}
	// A message that is not a gate and a status is passed over.
	name, status, _ := strings.Cut(e.Message, " ")
	g := model.Gate(name)
	i := slices.Index(model.Gates, g)
	if i < 0 || status == "" {
		return
	}
	for _, later := range model.Gates[i+1:] {
		passed[later] = false
	}
	passed[g] = model.GateStatus(status) == model.GatePass
}

// spansBetween is each event of kind opens up to the next of kind closes, or
// up to end where none follows.
func spansBetween(events []timedEvent, opens, closes string, end time.Time) []period {
	var out []period
	for i, e := range events {
		if e.Type != opens {
			continue
		}
		to := end
		for _, later := range events[i+1:] {
			if later.Type == closes {
				to = later.at
				break
			}
		}
		out = append(out, period{e.at, to})
	}
	return out
}

// pairAgents pairs each agent_end with the latest agent_start of the same id
// before it. A start left unanswered is a run that never ended, and is left
// out of the agents' time.
func (rp *replay) pairAgents(events []timedEvent) {
	open := map[string][]timedEvent{}
	var spans []period
	for _, e := range events {
		if e.Type != model.EventAgentStart && e.Type != model.EventAgentEnd {
			continue
		}
		if e.AgentID == "" {
			rp.unpaired++
			continue
		}
		if e.Type == model.EventAgentStart {
			open[e.AgentID] = append(open[e.AgentID], e)
			continue
		}
		rp.endedIDs[e.AgentID] = true
		starts := open[e.AgentID]
		if len(starts) == 0 {
			continue
		}
		start := starts[len(starts)-1]
		open[e.AgentID] = starts[:len(starts)-1]
		p := period{start.at, e.at}
		rp.runs[e.AgentID] = p
		spans = append(spans, p)
	}
	for _, starts := range open {
		for _, s := range starts {
			rp.unfinished = append(rp.unfinished, openRun{s.AgentType, s.AgentID, s.at})
		}
	}
	slices.SortFunc(rp.unfinished, func(a, b openRun) int { return a.at.Compare(b.at) })
	rp.agents = merged(within(merged(spans), rp.whole))
}

// gateAt is the gate in force at t.
func (rp *replay) gateAt(t time.Time) model.Gate {
	gate := model.Gate("")
	for i, s := range rp.stretches {
		if i == 0 || !s.from.After(t) {
			gate = s.gate
		}
	}
	return gate
}

// split is one part of the story's time: its wall time, and how much of it
// subagents, a person and the time between iterations account for, with the
// rest, which none of them does. Time two of them hold is counted once in the
// rest, so it is never below zero.
type split struct {
	wall, agents, person, between, rest time.Duration
}

// splitOf is the split of the time w.
func (rp *replay) splitOf(w period) split {
	accounted := slices.Concat(rp.person, rp.between)
	if rp.agentsKnown {
		accounted = append(accounted, rp.agents...)
	}
	s := split{
		wall:    w.length(),
		agents:  total(within(rp.agents, w)),
		person:  total(within(rp.person, w)),
		between: total(within(rp.between, w)),
	}
	s.rest = max(s.wall-total(within(merged(accounted), w)), 0)
	return s
}

// byGate is each gate's split, in the loop's order, for the gates that had
// any of the story's time.
func (rp *replay) byGate() (gates []model.Gate, splits []split) {
	for _, g := range model.Gates {
		var sum split
		found := false
		for _, s := range rp.stretches {
			if s.gate != g {
				continue
			}
			found = true
			part := rp.splitOf(s.period)
			sum.wall += part.wall
			sum.agents += part.agents
			sum.person += part.person
			sum.between += part.between
			sum.rest += part.rest
		}
		if found {
			gates, splits = append(gates, g), append(splits, sum)
		}
	}
	return gates, splits
}
