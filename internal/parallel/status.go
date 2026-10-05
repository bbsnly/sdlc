package parallel

import (
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// State is what a story reads as once its session has ended.
type State string

const (
	// StateWaiting is a story that needs a person: an escalation nobody has
	// answered, or every gate passed and the landing still to do.
	StateWaiting State = "waiting"
	// StateStopped is a session that ended cleanly with the story unfinished and
	// nobody told: the usual cause is a tool the headless session was denied.
	StateStopped State = "stopped"
	// StateFailed is a session that exited with a non-zero status, or never started.
	StateFailed State = "failed"
)

// Row is one story in the account a person is given at the end of a run.
type Row struct {
	Story   string `json:"story"`
	Gate    string `json:"gate"`
	State   State  `json:"state"`
	Command string `json:"command"`
	Exit    int    `json:"exit"`
}

// Classify is what a story reads as, from its record and not from what the
// session said: a session ended by a person's decision is not a failure, and a
// crash or a quiet end must never read as waiting for one. The exit status
// counts only when the record says nobody is wanted. A nil record is a story
// nobody has started.
func Classify(story string, rec *model.Record, exit int) Row {
	if rec == nil {
		rec = model.NewRecord(story, time.Time{})
	}
	row := Row{Story: story, Exit: exit}
	if g, ok := rec.NextGate(); ok {
		row.Gate = string(g)
	}
	if esc, ok := rec.PendingEscalation(); ok {
		row.State = StateWaiting
		row.Command = "sdlc approve " + story
		if esc.Type == "freeze_broken" {
			// A comment, so that the line stays one a person can paste.
			row.Command += " # or, to change the frozen tests first: sdlc unfreeze --reason ..."
		}
		return row
	}
	switch {
	case row.Gate == "":
		row.State = StateWaiting
		row.Command = "sdlc stop"
	case exit != 0:
		row.State = StateFailed
	default:
		row.State = StateStopped
	}
	return row
}
