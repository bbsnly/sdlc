package hook

import (
	"os"
	"path/filepath"
	"testing"
)

// The record says when a story's tests were frozen. A tests.lock that went, or
// that was rewritten to name another story or none, unfroze them in the hook
// with nothing said, while the record still held the freeze -- so the edit was
// caught at the next gate, but not stopped.
func TestAFreezeTheRecordHoldsIsNotLiftedByRewritingTheLock(t *testing.T) {
	root := loopProject(t)
	write(t, root, "invoice_test.go", "package x\n")
	frozenOnRecord := `{"story":"A-1","gates":{},"events":[` +
		`{"at":"2026-09-10T08:30:00Z","type":"freeze","message":"froze 1 test file"}]}`
	write(t, root, ".sdlc/stories/A-1/gate-record.json", frozenOnRecord)
	lockPath := filepath.Join(root, ".sdlc", "state", "tests.lock")

	// A project that allows new test files still does not allow the frozen
	// ones: every test is held, not only new ones refused.
	for _, config := range []string{`{"version":1}`, `{"version":1,"freeze":{"allow_new_test_files":true}}`} {
		write(t, root, ".sdlc/config.json", config)
		for name, lock := range map[string]string{
			"gone":          "",
			"null":          "null",
			"no story":      "{}",
			"another story": `{"story":"OTHER-1","files":{"invoice_test.go":"abc"}}`,
		} {
			t.Run(name+" "+config, func(t *testing.T) {
				if lock == "" {
					if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
				} else {
					write(t, root, ".sdlc/state/tests.lock", lock)
				}
				if !denied(call(t, event(root, "Write", "sdlc:sdet", "invoice_test.go"), noEnv)) {
					t.Error("a test the record holds frozen was written through the file tools")
				}
				if !denied(call(t, command(root, "sdlc:sdet", "echo cheat >> invoice_test.go"), noEnv)) {
					t.Error("a test the record holds frozen was written through the shell")
				}
				if denied(call(t, command(root, "sdlc:sdet", "echo fine > invoice.go"), noEnv)) {
					t.Error("a file that is not a test was frozen")
				}
			})
		}
	}

	// Lifted on the record, the tests are the test author's again.
	write(t, root, ".sdlc/stories/A-1/gate-record.json", `{"story":"A-1","gates":{},"events":[`+
		`{"at":"2026-09-10T08:30:00Z","type":"freeze","message":"froze 1 test file"},`+
		`{"at":"2026-09-10T09:00:00Z","type":"unfreeze","message":"the test was wrong"}]}`)
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if denied(call(t, event(root, "Write", "sdlc:sdet", "invoice_test.go"), noEnv)) {
		t.Error("a freeze lifted on the record still froze the tests")
	}
	if denied(call(t, command(root, "sdlc:sdet", "echo fine >> invoice_test.go"), noEnv)) {
		t.Error("a freeze lifted on the record still froze the tests through the shell")
	}

	// A record that does not read holds no freeze. The commit is refused over
	// it; the tests, before any freeze was taken, stay the test author's.
	write(t, root, ".sdlc/stories/A-1/gate-record.json", "{not json")
	if denied(call(t, event(root, "Write", "sdlc:sdet", "invoice_test.go"), noEnv)) {
		t.Error("a record that does not read froze the tests")
	}
	if denied(call(t, command(root, "sdlc:sdet", "echo fine >> invoice_test.go"), noEnv)) {
		t.Error("a record that does not read froze the tests through the shell")
	}
}
