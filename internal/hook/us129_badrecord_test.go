package hook

// US-129 AC-2 for a gate record cut off just after its events array opens
// (US-129 verification, F-1). The base read the record with json.Unmarshal:
// the commit gate refused a commit because the record is not valid JSON, and
// every other rule answered as it does for any record. The record decoder's
// size scan indexed past the white space and panicked, and cmd/sdlc answers a
// panic with {"continue":true}: the refusal became a crash, and the crash an
// allow. Each answer here is the base's, byte for byte, taken from the base
// source with this file's calls.

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// us129BaseAnswers is what the base said to each command with the record cut
// off: the commit refused by the commit gate, a test run let through, and a
// write to the frozen test refused by the freeze.
var us129BaseAnswers = map[string]string{
	"git commit -m x": `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":` +
		`"this story has not been through the gates that come before committing: .sdlc/stories/A-1/gate-record.json is not valid JSON, ` +
		"so no gate can be shown to have passed (`sdlc doctor` says more). Instead: finish the gates -- `sdlc status` shows where this " +
		`story stands; committing without them is for the person running the session to decide [commit-gate]"}}` + "\n",
	"go test ./...": `{"continue":true}` + "\n",
	"echo x > invoice_test.go": `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":` +
		`"invoice_test.go is a frozen acceptance test, and a shell command is the one way around every rule that protects it. ` +
		"Instead: leave it alone -- it was locked by content when the test gate passed, and every gate after that is measured " +
		"against it. If it genuinely has to change, say which and why: the person running the session lifts the freeze with " +
		"`/sdlc:unfreeze`, or `sdlc unfreeze --reason ...`, which puts the reason on the record [frozen-test-through-the-tool]\"}}\n",
}

func TestAC2PreToolUseBashOnARecordCutOffAfterItsEventsOpenAnswersAsAtTheBase(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"story":"A-1","events":[ `, "{\"story\":\"A-1\",\"events\":[\r\n", "{\"events\":[\n\n"} {
		for command, want := range us129BaseAnswers {
			t.Run("AC-2 "+command+" "+strings.TrimSpace(body), func(t *testing.T) {
				t.Parallel()
				l := swCheckouts(t)
				write(t, l.main, ".sdlc/stories/A-1/gate-record.json", body)
				payload := map[string]any{"tool_name": "Bash", "agent_type": "sdlc:implementer",
					"tool_input": map[string]any{"command": command}, "hook_event_name": "PreToolUse", "session_id": swHeld, "cwd": l.wt}
				getenv := env(map[string]string{"CLAUDE_PROJECT_DIR": l.wt})
				var out bytes.Buffer
				code := func() (code int) {
					defer func() {
						if p := recover(); p != nil {
							t.Errorf("AC-2 the hook panicked on %q, which cmd/sdlc answers with {\"continue\":true}: %v", command, p)
							code = -1
						}
					}()
					return RunHosted([]string{"PreToolUse"}, strings.NewReader(mustMarshal(payload)), &out, io.Discard, getenv,
						Host{Transcripts: us129NoTranscripts})
				}()
				if code != 0 || out.String() != want {
					t.Errorf("AC-2 PreToolUse Bash %q with the record %q exited %d and answered\n%s\nwant 0 and the base's\n%s",
						command, body, code, out.String(), want)
				}
			})
		}
	}
}
