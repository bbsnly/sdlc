package cli

// US-112 AC-10: where git cannot measure the change, the commit gate passes or
// fails exactly as it would have, the diff record is left out, and the debug
// log says why. Only the measurement is made to fail: measureChange is the
// seam the commit gate measures through, gitx.Numstat unless a test says
// otherwise.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
)

// toTheCommitGate is a project whose story has passed every gate before the
// commit gate, with code written at its gate and everything committed.
func toTheCommitGate(t *testing.T) string {
	t.Helper()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	reach(t, root, model.GateImplementation)
	writeFile(t, root, "internal/invoice.go", "package internal\n\nfunc Invoice() {}\n")
	for _, gate := range model.Gates[slices.Index(model.Gates, model.GateImplementation):slices.Index(model.Gates, model.GateCommit)] {
		satisfy(t, root, gate)
		mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test")
	}
	satisfy(t, root, model.GateCommit)
	return root
}

var commitGateNoise = regexp.MustCompile(`\b[0-9a-f]{7,64}\b|\d{4}-\d{2}-\d{2}T[0-9:.]+Z`)

// Not parallel: it swaps a package variable, and the logger.
func TestAC10WhereGitCannotMeasureTheChangeTheCommitGateIsUnchangedAndNoDiffIsRecorded(t *testing.T) {
	normal := func(root string, r result) string {
		out := strings.ReplaceAll(r.stdout+"\x00"+r.stderr, root, "<root>")
		return commitGateNoise.ReplaceAllString(out, "<x>")
	}

	measured := toTheCommitGate(t)
	want := run(t, "gate", "commit", "pass", "--note", "measured")
	if want.code != 0 {
		t.Fatalf("the commit gate did not pass where the change could be measured:\n%s", want.stderr)
	}

	root := toTheCommitGate(t)
	const why = "numstat fell over for the test"
	before := measureChange
	measureChange = func(context.Context, string, string, string, ...string) ([]gitx.FileStat, error) {
		return nil, errors.New(why)
	}
	t.Cleanup(func() { measureChange = before })
	var log bytes.Buffer
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(logger) })

	got := run(t, "gate", "commit", "pass", "--note", "measured")
	if got.code != want.code {
		t.Errorf("AC-10 the commit gate exited %d where git could not measure, and %d where it could", got.code, want.code)
	}
	if normal(root, got) != normal(measured, want) {
		t.Errorf("AC-10 the commit gate said something else where git could not measure:\n%q\nwhere it could:\n%q",
			normal(root, got), normal(measured, want))
	}
	story := decode[statusPayload](t, mustRun(t, "status", "--json")).Active
	if gates := decode[statusPayload](t, mustRun(t, "status", "--json")).Gates; gates[string(model.GateCommit)] != "pass" {
		t.Errorf("AC-10 the commit gate is %q where git could not measure, want pass", gates[string(model.GateCommit)])
	}
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", story, "trace.jsonl"))
	if err == nil && strings.Contains(string(raw), `"kind":"diff"`) {
		t.Errorf("AC-10 a diff record was written where git could not measure:\n%s", raw)
	}
	if !strings.Contains(log.String(), why) {
		t.Errorf("AC-10 the debug log does not say why the diff was not recorded:\n%s", log.String())
	}

	// The project where git could measure did record it, so the absence above
	// is the failure's and not recording's being off.
	if raw, err := os.ReadFile(filepath.Join(measured, ".sdlc", "stories", story, "trace.jsonl")); err != nil ||
		!strings.Contains(string(raw), `"kind":"diff"`) {
		t.Errorf("AC-10 the commit gate recorded no diff where git could measure (%v):\n%s", err, raw)
	}
}
