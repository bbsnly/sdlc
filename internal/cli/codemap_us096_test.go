package cli

// US-096 AC-11 (from US-097): CODEMAP.md's entries for internal/cli/landing.go
// and internal/model name the stop's own replay, its mark on the record and
// trunk_tests. The AC numbers in this file are US-096's.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmapSection is the text under the first `## ` heading of CODEMAP.md that
// begins with prefix, up to the next `## `, and whether there is one.
func cmapSection(t *testing.T, prefix string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "CODEMAP.md"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	in, found := false, false
	for line := range strings.SplitSeq(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			if found {
				break
			}
			in = strings.HasPrefix(line, "## "+prefix)
			found = in
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.Join(strings.Fields(strings.Join(out, "\n")), " "), found
}

func TestAC11TheCodemapsLandingAndModelEntriesNameTheStopsReplayItsMarkAndTrunkTests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, heading string
		want          []string
	}{
		{"AC-11 internal/cli/landing.go", "internal/cli/landing.go", []string{
			// The stop's own replay of the story onto trunk.
			"ontoTrunk", "gitx.Replay",
			// Its mark on the record, and the event it records.
			"Record.Replay", "`replay`", "`replayed`",
			// The trunk's test files the landing took, recorded as it finishes.
			"`trunk_tests`",
		}},
		{"AC-11 internal/model", "internal/model", []string{
			"Replay", "`replay`", "`landing`", "TrunkTests", "`trunk_tests`",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			section, ok := cmapSection(t, tc.heading)
			if !ok {
				t.Fatalf("CODEMAP.md has no `## %s` entry", tc.heading)
			}
			for _, w := range tc.want {
				if !strings.Contains(section, w) {
					t.Errorf("CODEMAP.md's `## %s` entry does not name %s", tc.heading, w)
				}
			}
		})
	}
}
