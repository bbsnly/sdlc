package cli

// A gate whose agents' time has passed what the project expects for it
// (budget.gate_hours, US-112.6) says so on stderr, on every command that has
// the story's record in hand, and once on the record. Nothing is blocked: the
// check decides nothing, and a story stopped halfway costs more than the
// overrun.

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
)

// gateOver is a gate whose agents' time is past its number, as the record
// stood when the command read it.
type gateOver struct {
	gate   model.Gate
	agents time.Duration
	hours  float64
	// recorded is whether the record already holds this gate's budget event.
	recorded bool
}

// gateOvers is every gate over its number, in the loop's order. It is made once
// per command, so the line and the event say the same thing.
type gateOvers []gateOver

// gateHoursOver is the gates of the record that are over their numbers. With no
// number set it returns before the record is replayed, so a project that does
// not use the setting pays nothing.
//
// It reads the gate record alone, as "sdlc trace" does for the agents' column,
// and has no error to return: what the figure comes from cannot make a command
// decide anything.
func gateHoursOver(budget config.Budget, record *model.Record) gateOvers {
	limits, _ := budget.GateHoursSet()
	if len(limits) == 0 {
		return nil
	}
	gates, splits := replayOf(record, false).byGate()
	var over gateOvers
	for i, g := range gates {
		// 0 turns a gate off, and so does having no entry.
		hours := limits[string(g)]
		// In hours and not as a product: 4.1*3600 is a hair under 14760 in a
		// float, so a gate at exactly 4.1 hours read as past it. The agents'
		// time is a whole number of seconds, and dividing it by 3600 rounds
		// the same real number as reading the number set does, so a tie is
		// equal here. A time.Duration made from the hours would overflow for
		// a large number.
		if hours <= 0 || splits[i].agents.Seconds()/3600 <= hours {
			continue
		}
		over = append(over, gateOver{gate: g, agents: splits[i].agents, hours: hours, recorded: budgetRecorded(record, g)})
	}
	return over
}

// say writes one line for each gate over its number, as the per-story alerts
// are written: on stderr, whether the command is then refused or not.
func (o gateOvers) say(w io.Writer) {
	for _, g := range o {
		fmt.Fprintf(w, "sdlc: %s has had %s of agents' time, past the %sh that budget.gate_hours sets for it; "+
			"nothing is blocked\n", g.gate, humanTime(g.agents), hoursText(g.hours))
	}
}

// record puts one event on the record for each gate over its number that has
// none, for the save the command is about to make. It is called only on the
// path that saves: a command that is refused changes nothing.
func (o gateOvers) record(r *model.Record, at time.Time) {
	for _, g := range o {
		if !g.recorded {
			r.Append(model.EventBudget, budgetMessage(g), at)
		}
	}
}

// budgetMessage is the event for a gate. The gate key comes first, and the
// words after it name no other gate key, because a reader finds the gate in it
// by its first words. It says when sdlc found the gate over its number, not
// when it passed it: the figure is the record as the command read it.
func budgetMessage(g gateOver) string {
	return string(g.gate) + ": agents' time was " + humanTime(g.agents) +
		" when sdlc found it past the " + hoursText(g.hours) + "h that budget.gate_hours set"
}

// hoursText is a number of hours as it is written, never as an exponent.
func hoursText(hours float64) string { return strconv.FormatFloat(hours, 'f', -1, 64) }

// budgetGateOf is the gate a budget event is for. The per-story alerts are
// budget events too, and begin with a percentage, so they are for no gate.
func budgetGateOf(e model.Event) (model.Gate, bool) {
	if e.Type != model.EventBudget {
		return "", false
	}
	key, _, found := strings.Cut(e.Message, ": ")
	gate := model.Gate(key)
	return gate, found && gate.Valid()
}

// budgetRecorded is whether the record holds the event for gate.
func budgetRecorded(record *model.Record, gate model.Gate) bool {
	for _, e := range record.Events {
		if g, ok := budgetGateOf(e); ok && g == gate {
			return true
		}
	}
	return false
}

// budgetMarks is the gates "sdlc trace" marks: those the record holds an
// event for, whatever the configuration says now.
func budgetMarks(record *model.Record) map[model.Gate]bool {
	marks := map[model.Gate]bool{}
	for _, e := range record.Events {
		if g, ok := budgetGateOf(e); ok {
			marks[g] = true
		}
	}
	return marks
}
