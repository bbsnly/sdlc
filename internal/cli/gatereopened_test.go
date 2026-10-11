package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// reopenedLinePrefix is how `sdlc unfreeze` opens the line that names the gates
// it reopened (freeze.go), and the form `sdlc gate` is to use for the same thing.
const reopenedLinePrefix = "  reopened: "

// passedBefore is every gate of the record that stands passed.
func passedBefore(t *testing.T, root string) map[model.Gate]bool {
	t.Helper()
	record := readRecord(t, root, "US-001")
	passed := map[model.Gate]bool{}
	for _, g := range model.Gates {
		passed[g] = record.Pass(g)
	}
	return passed
}

// goneFrom is the gates that stood passed in before and do not in the record
// now, other than the one just recorded, in gate order. It is read from the
// record on disk, not worked out from which gate was recorded, so it holds
// whichever gates the loop decides to reopen.
func goneFrom(t *testing.T, root string, before map[model.Gate]bool, recorded model.Gate) []string {
	t.Helper()
	record := readRecord(t, root, "US-001")
	var gone []string
	for _, g := range model.Gates {
		if g != recorded && before[g] && !record.Pass(g) {
			gone = append(gone, string(g))
		}
	}
	return gone
}

// proseReopened is the gates the prose output names on its reopened line, and
// how many such lines there are.
func proseReopened(out string) (names []string, lines int) {
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, reopenedLinePrefix); ok {
			lines++
			names = strings.Split(rest, ", ")
		}
	}
	return names, lines
}

// jsonReopened is the gates `--json` names under `reopened`, and whether the
// key is there at all.
func jsonReopened(t *testing.T, out string) (names []string, present bool) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, out)
	}
	value, present := raw["reopened"]
	if !present {
		return nil, false
	}
	if err := json.Unmarshal(value, &names); err != nil {
		t.Fatalf("reopened is not an array of gate names: %v\n%s", err, out)
	}
	return names, true
}

// namesInLogMessage is the gates a `gates_reopened` message starts with:
// "a, b reopened: why".
func namesInLogMessage(t *testing.T, message string) []string {
	t.Helper()
	names, _, ok := strings.Cut(message, " reopened: ")
	if !ok {
		t.Fatalf("the log's message does not read \"<gates> reopened: <why>\": %q", message)
	}
	return strings.Split(names, ", ")
}

// A session that records a gate again is told what that took back, instead of
// finding out from the refusal of the next `git commit`.
func TestAC1RecordingAGateAgainNamesEveryGateItReopenedInProseAndJSON(t *testing.T) {
	cases := []struct {
		name string
		// gate is recorded again on a story that has passed every gate.
		gate   model.Gate
		status string
		// upTo is where the story stands: every gate before it has passed, or
		// all of them where it is empty.
		upTo model.Gate
		// atLeast is how many gates the case is meant to reopen, so a case that
		// stopped reopening them cannot pass as a case about none.
		atLeast int
	}{
		{"several gates after it", model.GateVerification, "pass", "", 2},
		{"a failure reopens the gates after it too", model.GateVerifierReview, "fail", "", 2},
		{"exactly one gate after it", model.GateCodeReview, "fail", model.GateRetro, 1},
	}
	for _, tc := range cases {
		for _, asJSON := range []bool{false, true} {
			form := "prose"
			if asJSON {
				form = "json"
			}
			t.Run(tc.name+"/"+form, func(t *testing.T) {
				t.Parallel()
				root := startedAt(t, tc.upTo)
				before := passedBefore(t, root)

				args := []string{"gate", string(tc.gate), tc.status, "--note", "again"}
				if asJSON {
					args = append(args, "--json")
				}
				out := mustRun(t, args...).stdout

				want := goneFrom(t, root, before, tc.gate)
				if len(want) < tc.atLeast || (tc.atLeast == 1 && len(want) != 1) {
					t.Fatalf("the case reopened %v, want %d gate(s): the setup is not the one the test is about", want, tc.atLeast)
				}
				if slices.Contains(want, string(tc.gate)) {
					t.Fatalf("the recorded gate is among those reopened: %v", want)
				}

				var got []string
				if asJSON {
					names, present := jsonReopened(t, out)
					if !present {
						t.Fatalf("--json does not name the reopened gates under \"reopened\", though %v were reopened:\n%s", want, out)
					}
					got = names
				} else {
					names, lines := proseReopened(out)
					if lines != 1 {
						t.Fatalf("the output has %d lines of the form %q, want one, though %v were reopened:\n%s",
							lines, reopenedLinePrefix+"<gates>", want, out)
					}
					got = names
				}
				if !slices.Equal(got, want) {
					t.Errorf("the command names %v as reopened, want %v, in gate order", got, want)
				}
			})
		}
	}
}

// What was recorded stays in the output beside what was reopened.
func TestAC1TheReopenedGatesAreReportedBesideTheGateRecorded(t *testing.T) {
	t.Parallel()
	finishedStory(t)

	prose := mustRun(t, "gate", "verification", "pass", "--note", "redone").stdout
	if !strings.HasPrefix(prose, "US-001  verification  pass\n  redone\n") {
		t.Errorf("the gate recorded, and its note, are no longer where they were:\n%s", prose)
	}

	finishedAgain := decode[gatePayload](t, mustRun(t, "gate", "verification", "pass", "--json"))
	if !finishedAgain.OK || finishedAgain.Gate != "verification" || finishedAgain.Status != "pass" || finishedAgain.Done {
		t.Errorf("the keys that were there are not what they were: %+v", finishedAgain)
	}
}

// The prose and the JSON are guards for the released tool's output, and pass
// before the change: AC-2 asks for no difference where nothing is reopened.
func TestAC2RecordingAGateThatReopensNothingPrintsWhatItPrintedBefore(t *testing.T) {
	t.Run("pass with a gate after it still pending", func(t *testing.T) {
		t.Parallel()
		root := startedAt(t, model.GateVerification)
		satisfy(t, root, model.GateVerification)
		out := mustRun(t, "gate", "verification", "pass", "--note", "all commands green").stdout
		if want := "US-001  verification  pass\n  all commands green\n"; out != want {
			t.Errorf("prose = %q, want %q", out, want)
		}
	})
	t.Run("pass as json", func(t *testing.T) {
		t.Parallel()
		root := startedAt(t, model.GateVerification)
		satisfy(t, root, model.GateVerification)
		out := mustRun(t, "gate", "verification", "pass", "--note", "all commands green", "--json").stdout
		want := `{"ok":true,"story":"US-001","gate":"verification","status":"pass","note":"all commands green","done":false}` + "\n"
		if out != want {
			t.Errorf("json = %q, want %q", out, want)
		}
		if _, present := jsonReopened(t, out); present {
			t.Error("the reopened key is there though nothing was reopened")
		}
	})
	t.Run("a failure with nothing passed after it", func(t *testing.T) {
		t.Parallel()
		startedAt(t, model.GateVerification)
		out := mustRun(t, "gate", "verification", "fail", "--note", "AC-2 is untested").stdout
		if want := "US-001  verification  fail\n  AC-2 is untested\n"; out != want {
			t.Errorf("prose = %q, want %q", out, want)
		}
	})
	t.Run("the last gate recorded again", func(t *testing.T) {
		t.Parallel()
		finishedStory(t)
		out := mustRun(t, "gate", "retro", "pass").stdout
		if _, lines := proseReopened(out); lines != 0 || strings.Contains(out, "reopened") {
			t.Errorf("the last gate has nothing after it to reopen, and the output says:\n%s", out)
		}
		if _, present := jsonReopened(t, mustRun(t, "gate", "retro", "pass", "--json").stdout); present {
			t.Error("the reopened key is there though the last gate has nothing after it")
		}
	})
}

// The command and `sdlc log` both describe the event that took the gates back,
// and they are to agree on which gates it was, in what order, however many.
// Both are read from running the commands.
func TestAC3TheCommandAndTheLogNameTheSameReopenedGates(t *testing.T) {
	cases := []struct {
		name   string
		gate   model.Gate
		status string
		// upTo is where the story stands, as in AC-1's cases.
		upTo model.Gate
	}{
		{"several gates", model.GateVerification, "pass", ""},
		{"one gate", model.GateCodeReview, "fail", model.GateRetro},
	}
	for _, tc := range cases {
		for _, asJSON := range []bool{false, true} {
			form := "prose"
			if asJSON {
				form = "json"
			}
			t.Run(tc.name+"/"+form, func(t *testing.T) {
				t.Parallel()
				root := startedAt(t, tc.upTo)

				args := []string{"gate", string(tc.gate), tc.status, "--note", "again"}
				if asJSON {
					args = append(args, "--json")
				}
				out := mustRun(t, args...).stdout
				var told []string
				if asJSON {
					told, _ = jsonReopened(t, out)
				} else {
					told, _ = proseReopened(out)
				}
				if len(told) == 0 {
					t.Fatalf("the command names no reopened gates:\n%s", out)
				}

				// Bring the story back to a committed one, so that the log lists it.
				if tc.status == "fail" {
					mustRun(t, "gate", string(tc.gate), "pass", "--note", "fixed")
				}
				for _, g := range told {
					switch model.Gate(g) {
					case model.GateRetro:
					case model.GateCommit:
						commitEverything(t, root)
						mustRun(t, "gate", g, "pass", "--note", "on trunk")
					default:
						mustRun(t, "gate", g, "pass", "--note", "redone")
					}
				}

				suffix := ": " + string(tc.gate) + " was recorded " + tc.status
				entry := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json")))
				var found int
				for _, ev := range entry.Reopened {
					if !strings.HasSuffix(ev.Message, suffix) {
						continue
					}
					found++
					if logged := namesInLogMessage(t, ev.Message); !slices.Equal(logged, told) {
						t.Errorf("the command named %v and the log names %v for the same event", told, logged)
					}
				}
				if found != 1 {
					t.Fatalf("the log has %d reopenings for %s being recorded %s, want 1: %+v", found, tc.gate, tc.status, entry.Reopened)
				}

				var printed int
				for line := range strings.SplitSeq(mustRun(t, "log").stdout, "\n") {
					rest, ok := strings.CutPrefix(line, "  reopened   ")
					if !ok || !strings.HasSuffix(rest, suffix) {
						continue
					}
					printed++
					if logged := namesInLogMessage(t, rest); !slices.Equal(logged, told) {
						t.Errorf("the command named %v and the printed log names %v for the same event", told, logged)
					}
				}
				if printed != 1 {
					t.Errorf("the printed log has %d reopened lines for the event, want 1", printed)
				}
			})
		}
	}
}
