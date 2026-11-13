package hook

// US-129 AC-2 for the Stop reason (code review F-2). The base read the whole
// record into model.Record for it, and a record that is valid JSON but whose
// events do not decode failed that read: the reason then said "the next
// gate". Reading the gates alone names a gate the base did not, or says every
// gate has passed, and the Stop reply on stdout is AC-2's to keep.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

func TestAC2AStopOnARecordWhoseEventsDoNotDecodeIsSentBackToTheNextGateAsAtTheBase(t *testing.T) {
	at := time.Now()
	gates := func(gs ...model.Gate) string {
		r := model.NewRecord("A-1", at)
		for _, g := range gs {
			r.SetGate(g, model.GatePass, "", at)
		}
		raw, err := json.Marshal(r.Gates)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	cases := map[string]string{
		"events that are a number": `{"story":"A-1","gates":` + gates(model.GateDoR) + `,"events":5}`,
		"an event with a number for its type": `{"story":"A-1","gates":` + gates(model.GateDoR) +
			`,"events":[{"type":7,"at":"2026-11-12T09:00:00Z"}]}`,
		"every gate passed and events that are a string": `{"story":"A-1","gates":` + gates(model.Gates...) + `,"events":"none"}`,
	}
	for name, body := range cases {
		t.Run("AC-2 "+name, func(t *testing.T) {
			var base model.Record
			if err := json.Unmarshal([]byte(body), &base); err == nil {
				t.Fatalf("the base's decode of %s succeeds; the row needs one that fails", body)
			}
			root := storyUnderWay(t)
			write(t, root, ".sdlc/stories/A-1/gate-record.json", body)
			r := stop(t, root, false, noEnv)
			if r.Decision != "block" || !strings.Contains(r.Reason, "A-1 is still being worked on. Work the next gate and record it") {
				t.Errorf("AC-2 the Stop on a record whose events do not decode answered %q, %q; the base sent it back to "+
					"\"the next gate\"", r.Decision, r.Reason)
			}
		})
	}
}
