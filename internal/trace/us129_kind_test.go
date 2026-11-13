package trace

// US-129 AC-2 for ReadKind, which the hook reads an agent's hand-back with in
// place of the base's Read: what it keeps, and the Skipped, Truncated and
// TruncatedAt it says, are Read's with only the records of the kind asked for
// kept. Read decodes each line into a map, where "kind" and "KIND" are two
// keys; the probe that reads a line's kind first must read it the same way,
// or a hand-back is lost, another kind is handed back as one, or the reading
// stops at a "truncated" Read goes past (US-129 verification, F-3).

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAC2ReadKindKeepsWhatReadKeepsOfTheKindAndSaysWhatReadSaysOfEveryOtherLine(t *testing.T) {
	t.Parallel()
	handback := `{"kind":"handback","agent_id":"a","status":"done"}`
	traces := map[string][]string{
		"the kind's key in capitals":                     {`{"KIND":"handback","agent_id":"a"}`, handback},
		"the kind's key in title case":                   {`{"Kind":"shell"}`, handback},
		"a hand-back with another kind in capitals":      {`{"kind":"handback","KIND":"shell","agent_id":"a","status":"done"}`},
		"another kind with a hand-back in capitals":      {`{"kind":"shell","KIND":"handback","agent_id":"b"}`, handback},
		"truncated in capitals":                          {`{"KIND":"truncated","at":"2026-11-06T09:00:00.000Z"}`, handback},
		"truncated with its time in capitals":            {`{"kind":"truncated","AT":"2026-11-06T09:00:00.000Z"}`, handback},
		"truncated with its time twice":                  {`{"kind":"truncated","at":"one","at":"two"}`, handback},
		"truncated with a time that is not a string":     {`{"kind":"truncated","at":5}`, handback},
		"the kind given twice, a hand-back last":         {`{"kind":"shell","kind":"handback","agent_id":"c"}`, handback},
		"the kind given twice, a hand-back first":        {`{"kind":"handback","kind":"shell","agent_id":"c"}`, handback},
		"a kind that is not a string":                    {`{"kind":5,"agent_id":"d"}`, handback},
		"a kind that is not a string, then a hand-back":  {`{"kind":5,"kind":"handback","agent_id":"d"}`},
		"a kind that is null":                            {`{"kind":null}`, handback},
		"a line that is null":                            {`null`, handback},
		"a line that is an array":                        {`["kind","handback"]`, handback},
		"a line that is a string":                        {`"handback"`, handback},
		"a line that is not JSON":                        {`{"kind":"handback"`, handback},
		"a kind nobody knows":                            {`{"kind":"later"}`, handback},
		"a hand-back nested in another kind":             {`{"kind":"shell","detail":{"kind":"handback"}}`, handback},
		"every line a hand-back":                         {handback, handback},
		"a hand-back after a truncated":                  {`{"kind":"truncated","at":"x"}`, handback},
		"the kind's key in another case and a Unicode K": {`{"Kind":"handback","agent_id":"e"}`, handback},
	}
	for name, lines := range traces {
		t.Run("AC-2 "+name, func(t *testing.T) {
			t.Parallel()
			dir := storyDir(t)
			if err := os.WriteFile(filepath.Join(dir, traceName), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			all, errAll := Read(dir)
			only, errOnly := ReadKind(dir, "handback")
			if (errAll == nil) != (errOnly == nil) {
				t.Fatalf("AC-2 Read said %v and ReadKind %v", errAll, errOnly)
			}
			want := all
			want.Records = nil
			for _, r := range all.Records {
				if r["kind"] == "handback" {
					want.Records = append(want.Records, r)
				}
			}
			if !reflect.DeepEqual(only, want) {
				t.Errorf("AC-2 ReadKind of the hand-backs in\n%s\nis %+v,\nwant what Read says with only its hand-backs kept: %+v",
					strings.Join(lines, "\n"), only, want)
			}
		})
	}
}

// The trace as the hook writes it: ReadKind keeps its hand-backs and says
// what Read says of the rest, and stops at a truncated record as Read does.
func TestAC2ReadKindOfAnOrdinaryTraceKeepsItsHandBacksAsReadDoes(t *testing.T) {
	t.Parallel()
	lines := []string{`{"kind":"shell","program":"go"}`, `{"kind":"handback","agent_id":"a","status":"done"}`, `not json`,
		`{"kind":"later"}`, `{"kind":"handback","agent_id":"b","status":"blocked"}`, `{"kind":"truncated","at":"2026-11-06T09:00:00.000Z"}`,
		`{"kind":"handback","agent_id":"c"}`}
	dir := storyDir(t)
	if err := os.WriteFile(filepath.Join(dir, traceName), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadKind(dir, "handback")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 2 || got.Records[0]["agent_id"] != "a" || got.Records[1]["agent_id"] != "b" ||
		got.Skipped != 2 || !got.Truncated || got.TruncatedAt != "2026-11-06T09:00:00.000Z" {
		t.Errorf("AC-2 ReadKind of an ordinary trace is %+v, want the hand-backs of a and b, two lines skipped and truncated at its time", got)
	}
	if _, err := ReadKind(filepath.Join(t.TempDir(), "A-2"), "handback"); err == nil {
		t.Error("AC-2 ReadKind of a story with no trace said nothing")
	}
}
