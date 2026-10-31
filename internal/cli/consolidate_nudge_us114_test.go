package cli

// US-114: a person is told, at the foot of `sdlc status` and of an `sdlc stop`
// that ends an iteration, when finished stories are waiting for
// /sdlc:consolidate.
//
// Everything here goes through the command surface, so that no test names a
// function the implementation has yet to write: if it did, the package would
// stop compiling and every test in it would fail for the wrong reason. The two
// shapes this file commits the implementation to are the line's content (it
// says how many stories, names /sdlc:consolidate and addresses a person) and
// the field in `status --json`:
//
//	"consolidate": {"finished": <count>, "last": "<YYYY-MM-DD>" | null}
//
// present at and above consolidate.nudge_after, absent below it and when it is 0.
//
// The date rules are the person's answers to the analysis's questions: a
// story's finish is its landed event, else its retro gate; its calendar date
// is taken in the machine's own zone and has to be strictly later than the
// file's date. Fixtures are built in time.Local, whatever the machine's zone
// is, so that a zone cannot decide a result here; the sandbox tests in
// test/e2e set TZ on the built binary to prove the zone rule itself.

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// us114Local is a moment on a calendar day in the machine's own zone, which is
// the zone the nudge reads a story's finish in.
func us114Local(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, time.Local)
}

// us114Story is a story the fixture adds to the backlog and, unless noRecord,
// gives a record of.
type us114Story struct {
	id, status string
	// retro and landed are when the retro gate passed and when the landed
	// event was appended; the zero time leaves each out.
	retro, landed time.Time
	noRecord      bool
}

func us114Done(id string, retro time.Time) us114Story {
	return us114Story{id: id, status: "done", retro: retro}
}

func us114Config(t *testing.T, root string, change func(cfg map[string]any)) {
	t.Helper()
	path := filepath.Join(root, ".sdlc", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	change(cfg)
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// us114Threshold sets consolidate.nudge_after, or leaves the whole section out
// where value is nil.
func us114Threshold(t *testing.T, root string, value any) {
	t.Helper()
	us114Config(t, root, func(cfg map[string]any) {
		if value == nil {
			delete(cfg, "consolidate")
			return
		}
		cfg["consolidate"] = map[string]any{"nudge_after": value}
	})
}

// us114Seed adds the stories to the backlog, writes their records, and commits
// the lot, so that `sdlc start` has a clean trunk to begin from.
func us114Seed(t *testing.T, root string, stories ...us114Story) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var backlog map[string]any
	if err := json.Unmarshal(raw, &backlog); err != nil {
		t.Fatal(err)
	}
	list, _ := backlog["stories"].([]any)
	template, _ := list[0].(map[string]any)
	for i, s := range stories {
		next := maps.Clone(template)
		next["id"], next["title"], next["status"], next["priority"] = s.id, "Story "+s.id, s.status, 50+i
		list = append(list, next)
		if s.noRecord {
			continue
		}
		rec := model.Record{
			Story: s.id, Created: model.Timestamp(us114Local(time.January, 5, 12, 0)),
			Gates: map[model.Gate]model.GateResult{}, Events: []model.Event{},
			Escalations: []model.Escalation{}, Approvals: []model.Approval{},
		}
		switch {
		case !s.retro.IsZero():
			rec.Gates[model.GateRetro] = model.GateResult{Status: model.GatePass, At: model.Timestamp(s.retro)}
		case s.status == "done" && s.landed.IsZero():
			// A done story whose record says nothing of a finish.
			rec.Gates[model.GateRetro] = model.GateResult{Status: model.GatePending}
		}
		if !s.landed.IsZero() {
			rec.Events = append(rec.Events, model.Event{At: model.Timestamp(s.landed), Type: model.EventLanded, Message: "landed"})
		}
		body, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, ".sdlc/stories/"+s.id+"/gate-record.json", string(body)+"\n")
	}
	backlog["stories"] = list
	out, err := json.MarshalIndent(backlog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "user_stories.json", string(out)+"\n")
	commitEverything(t, root)
}

// us114File is a file of that name in .sdlc, which holds nothing: the nudge
// reads names and never contents.
func us114File(t *testing.T, root, name string) {
	t.Helper()
	writeFile(t, root, ".sdlc/"+name, "")
}

// us114Run is what a command printed on both streams, for a test that wants a
// refusal to be one.
func us114Run(t *testing.T, args ...string) result {
	t.Helper()
	return run(t, args...)
}

// us114Base is what a command prints with the nudge switched off, which is
// what it printed before the nudge existed: the configuration is put back as
// it was.
func us114Base(t *testing.T, root string, args ...string) result {
	t.Helper()
	path := filepath.Join(root, ".sdlc", "config.json")
	was, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	us114Threshold(t, root, 0)
	r := run(t, args...)
	if err := os.WriteFile(path, was, 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

// us114Added is the one line a command printed after what it printed with the
// nudge off: nothing else about its output differs.
func us114Added(t *testing.T, with, base string) string {
	t.Helper()
	if !strings.HasPrefix(with, base) {
		t.Fatalf("the output is not the output without the nudge followed by one line\nwith the nudge:\n%q\nwithout it:\n%q", with, base)
	}
	rest := strings.TrimLeft(with[len(base):], "\n")
	line := strings.TrimSuffix(rest, "\n")
	if line == "" || strings.Contains(line, "\n") || !strings.HasSuffix(rest, "\n") {
		t.Fatalf("what follows the usual output is not exactly one line ending in a newline: %q", with[len(base):])
	}
	return line
}

// us114CheckLine is what the line has to say: how many stories, that a person
// can run /sdlc:consolidate, in words addressed to a person.
func us114CheckLine(t *testing.T, line string, count int) {
	t.Helper()
	if !strings.Contains(line, "/sdlc:consolidate") {
		t.Errorf("the line does not name /sdlc:consolidate: %q", line)
	}
	if !regexp.MustCompile(fmt.Sprintf(`\b%d\b`, count)).MatchString(line) {
		t.Errorf("the line does not say %d: %q", count, line)
	}
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "stor") {
		t.Errorf("the line does not say what is counted, stories: %q", line)
	}
	if !strings.Contains(lower, "person") && !strings.Contains(lower, "you ") {
		t.Errorf("the line does not address a person, as the loop's other lines do: %q", line)
	}
}

// us114Nudged is a run of the command that must end with the line.
func us114Nudged(t *testing.T, root string, count int, args ...string) {
	t.Helper()
	base := us114Base(t, root, args...)
	with := mustRun(t, args...)
	if base.code != 0 {
		t.Fatalf("sdlc %s with the nudge off failed: %s", strings.Join(args, " "), base.stderr)
	}
	us114CheckLine(t, us114Added(t, with.stdout, base.stdout), count)
}

// us114Field is what status --json says of the nudge: the count and the last
// consolidation's date. present is whether the field is there at all.
type us114Field struct {
	present  bool
	finished int
	last     *string
	lastKey  bool
}

func us114StatusField(t *testing.T) us114Field {
	t.Helper()
	r := mustRun(t, "status", "--json")
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.stdout), &top); err != nil {
		t.Fatalf("status --json is not one JSON object: %v\n%s", err, r.stdout)
	}
	raw, ok := top["consolidate"]
	if !ok {
		return us114Field{}
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		t.Fatalf("status --json's consolidate is not an object: %v\n%s", err, raw)
	}
	f := us114Field{present: true}
	if err := json.Unmarshal(inner["finished"], &f.finished); err != nil {
		t.Fatalf("consolidate.finished is not a count: %v\n%s", err, raw)
	}
	_, f.lastKey = inner["last"]
	if err := json.Unmarshal(inner["last"], &f.last); err != nil {
		t.Fatalf("consolidate.last is not a date or null: %v\n%s", err, raw)
	}
	return f
}

func us114WantField(t *testing.T, finished int, last string) {
	t.Helper()
	f := us114StatusField(t)
	if !f.present {
		t.Errorf("status --json has no consolidate field, want finished %d and last %q", finished, last)
		return
	}
	if f.finished != finished {
		t.Errorf("consolidate.finished = %d, want %d", f.finished, finished)
	}
	switch {
	case last == "" && f.last != nil:
		t.Errorf("consolidate.last = %q, want null: there has been no consolidation", *f.last)
	case last == "" && !f.lastKey:
		t.Error("consolidate.last is left out, want null: a reader has to tell no date from no field")
	case last != "" && (f.last == nil || *f.last != last):
		t.Errorf("consolidate.last = %v, want %q", f.last, last)
	}
}

// ------------------------------------------------------------------ AC-1

// AC-1, AC-3: at the threshold, status ends with one line saying how many
// stories finished since the last consolidation, and what is printed before it
// is what status printed without the line.
func TestAC1AC3StatusEndsWithOneLineSayingHowManyStoriesFinishedSinceTheLastConsolidation(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 20, 12, 0)),
		us114Done("US-104", us114Local(time.July, 13, 12, 0)), // before the file: counts for nothing
		us114Done("US-105", us114Local(time.July, 15, 12, 0)), // the file's own day: counts for nothing
	)
	us114File(t, root, "consolidation-2026-07-15.md")

	us114Nudged(t, root, 3, "status")
	us114WantField(t, 3, "2026-07-15")
}

// AC-1: the count is how many finished, not a number the line always carries.
func TestAC1TheLineAndTheFieldFollowTheCountAsStoriesFinish(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	us114Nudged(t, root, 3, "status")

	us114Seed(t, root, us114Done("US-104", us114Local(time.July, 21, 12, 0)))
	us114Nudged(t, root, 4, "status")
	us114WantField(t, 4, "2026-07-15")

	us114Seed(t, root, us114Done("US-105", us114Local(time.July, 22, 12, 0)))
	us114Nudged(t, root, 5, "status")
	us114WantField(t, 5, "2026-07-15")

	// A later consolidation starts the count again.
	us114File(t, root, "consolidation-2026-07-21.md")
	us114Seed(t, root, us114Done("US-106", us114Local(time.July, 23, 12, 0)))
	if f := us114StatusField(t); f.present {
		t.Errorf("a consolidation on the 21st, with two stories after it, left the field there, with %+v", f)
	}
	us114Threshold(t, root, 1)
	us114WantField(t, 2, "2026-07-21")
}

// AC-1: with no consolidation, the date is null and every finished story counts.
func TestAC1WithNoConsolidationTheDateIsNullAndEveryFinishedStoryCounts(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.January, 3, 12, 0)),
		us114Done("US-102", us114Local(time.February, 3, 12, 0)),
		us114Done("US-103", us114Local(time.March, 3, 12, 0)),
	)
	us114Nudged(t, root, 3, "status")
	us114WantField(t, 3, "")
}

// AC-1, AC-2: the line is the last thing status says, after whatever else it
// says of a story being worked on.
func TestAC1AC2TheLineComesAfterEveryOtherLineWhileAStoryIsBeingWorked(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
	)
	mustRun(t, "start")
	us114Nudged(t, root, 3, "status")
}

// AC-2: below the threshold the output is what it was before the line existed,
// to the byte, in both forms; at the threshold, and not before, the line comes.
func TestAC2BelowTheThresholdStatusIsUnchangedAndTheThresholdItselfIsEnough(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")

	below := mustRun(t, "status")
	if want := us114Base(t, root, "status"); below.stdout != want.stdout || below.stderr != want.stderr {
		t.Errorf("status with 2 finished of 3 differs from status with the nudge off:\n%q\n%q", below.stdout, want.stdout)
	}
	if !strings.HasSuffix(below.stdout, "to begin.\n") || strings.Contains(below.stdout, "consolidate") {
		t.Errorf("status below the threshold is not what it was, ending with the line about starting:\n%s", below.stdout)
	}
	if got, want := mustRun(t, "status", "--json").stdout, us114Base(t, root, "status", "--json").stdout; got != want {
		t.Errorf("status --json below the threshold differs from status --json with the nudge off:\n%s\n%s", got, want)
	}
	if f := us114StatusField(t); f.present {
		t.Errorf("status --json carries the field below the threshold: %+v", f)
	}

	us114Seed(t, root, us114Done("US-103", us114Local(time.July, 19, 12, 0)))
	us114Nudged(t, root, 3, "status")
}

// AC-1, AC-5: the threshold is the setting's, not a 3 in the code.
func TestAC1AC5TheThresholdIsWhateverNudgeAfterSays(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
		us114Done("US-104", us114Local(time.July, 20, 12, 0)),
	)
	us114Threshold(t, root, 5)
	if out := mustRun(t, "status").stdout; strings.Contains(out, "consolidate") {
		t.Errorf("status says something of consolidating with 4 finished and nudge_after 5:\n%s", out)
	}
	if f := us114StatusField(t); f.present {
		t.Errorf("status --json carries the field with 4 finished and nudge_after 5: %+v", f)
	}

	us114Seed(t, root, us114Done("US-105", us114Local(time.July, 21, 12, 0)))
	us114Nudged(t, root, 5, "status")

	us114Threshold(t, root, 1)
	us114Nudged(t, root, 5, "status")
}

// ------------------------------------------------------------------ AC-5

// AC-5: 0 turns the line and the field off, in both commands, however many
// stories wait.
func TestAC5NudgeAfterZeroPrintsNoLineAndNoField(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
		us114Done("US-104", us114Local(time.July, 20, 12, 0)),
	)
	// The control: with the setting left alone, four stories are enough.
	us114Nudged(t, root, 4, "status")
	us114Threshold(t, root, 0)
	if out := mustRun(t, "status").stdout; strings.Contains(out, "consolidate") {
		t.Errorf("status with nudge_after 0 says something of consolidating:\n%s", out)
	}
	if f := us114StatusField(t); f.present {
		t.Errorf("status --json with nudge_after 0 carries the field: %+v", f)
	}
	mustRun(t, "start")
	if out := mustRun(t, "stop").stdout; strings.Contains(out, "consolidate") {
		t.Errorf("stop with nudge_after 0 says something of consolidating:\n%s", out)
	}
}

// AC-5: a project that sets nothing is nudged, at three, whether the section
// is left out or is there without the key.
func TestAC5AnAbsentNudgeAfterMeansThree(t *testing.T) {
	t.Parallel()
	for name, section := range map[string]any{
		"no consolidate section":           nil,
		"a consolidate section, no key":    map[string]any{},
		"a consolidate section, a comment": map[string]any{"_doc": "nothing set"},
	} {
		t.Run(strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			us114Seed(t, root,
				us114Done("US-101", us114Local(time.July, 17, 12, 0)),
				us114Done("US-102", us114Local(time.July, 18, 12, 0)),
			)
			us114Config(t, root, func(cfg map[string]any) {
				delete(cfg, "consolidate")
				if section != nil {
					cfg["consolidate"] = section
				}
			})
			if f := us114StatusField(t); f.present {
				t.Errorf("two finished stories and no setting: the field is there: %+v", f)
			}
			us114Seed(t, root, us114Done("US-103", us114Local(time.July, 19, 12, 0)))
			us114Nudged(t, root, 3, "status")
		})
	}
}

// AC-5: a value the loop cannot honour is refused, naming the setting, as the
// other limits are, and not read as something else.
func TestAC5ANegativeOrNonNumericNudgeAfterIsRefusedNamingTheSetting(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]any{
		"negative":  -1,
		"text":      "three",
		"numeral":   "3",
		"fraction":  1.5,
		"truth":     true,
		"a list":    []int{3},
		"an object": map[string]any{"after": 3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			us114Threshold(t, root, value)
			r := us114Run(t, "status")
			if r.code == 0 {
				t.Fatalf("status accepted consolidate.nudge_after %v:\n%s", value, r.stdout)
			}
			if !strings.Contains(r.stderr, "nudge_after") || !strings.Contains(r.stderr, "SDLC-E") {
				t.Errorf("the refusal does not name the setting and carry a code:\n%s", r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("a refusal wrote to stdout: %q", r.stdout)
			}
		})
	}
}

// AC-5: doctor accepts the key, where it reported it as a setting it does not
// know.
func TestAC5DoctorAcceptsTheNudgeAfterKey(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Threshold(t, root, 5)
	r := us114Run(t, "doctor")
	if out := r.stdout + r.stderr; strings.Contains(out, "does not know") || strings.Contains(out, "nudge_after") {
		t.Errorf("doctor does not accept consolidate.nudge_after:\n%s", out)
	}
}

// ------------------------------------------------------------------ AC-3

// AC-3: only a name of the form consolidation-<a real date>[-N].md sets the
// date. Everything else in .sdlc is passed over, however late it reads: a name
// that sorts after the real one is the case a lexicographic maximum gets wrong.
func TestAC3OnlyAWellFormedDatedFileNameSetsTheDateOfTheLastConsolidation(t *testing.T) {
	t.Parallel()
	for _, junk := range []string{
		"consolidation-2026-13-45.md",     // not a month
		"consolidation-2026-02-29.md",     // 2026 is not a leap year
		"consolidation-2026-04-31.md",     // April has thirty days
		"consolidation-2026-00-10.md",     // no month 0
		"consolidation-2026-07-00.md",     // no day 0
		"consolidation-2026-8-01.md",      // not zero padded
		"consolidation-20260801.md",       // no hyphens
		"consolidation-2026-08-01.md.bak", // not .md
		"consolidation-2026-08-01.txt",
		"consolidation-2026-08-01-x.md",  // a suffix that is not a number
		"consolidation-2026-08-01--2.md", // two hyphens
		"consolidation-2026-08-01-.md",   // a hyphen and no number
		"Consolidation-2026-08-01.md",    // not the case the skill writes
		"consolidation-2026-08-01 .md",   // a space
		"notes-consolidation-2026-08-01.md",
		"consolidation-2026-08-01.md~",
	} {
		t.Run(junk, func(t *testing.T) {
			t.Parallel()
			if strings.ContainsAny(junk, " ") && runtime.GOOS == "windows" {
				t.Skip("windows does not keep a trailing space in a file name")
			}
			root := gitProjectInitialised(t)
			us114Seed(t, root,
				us114Done("US-101", us114Local(time.July, 17, 12, 0)),
				us114Done("US-102", us114Local(time.July, 18, 12, 0)),
				us114Done("US-103", us114Local(time.July, 19, 12, 0)),
			)
			us114File(t, root, "consolidation-2026-07-15.md")
			us114File(t, root, junk)
			us114Nudged(t, root, 3, "status")
			us114WantField(t, 3, "2026-07-15")
		})
	}
}

// AC-3: a directory or a link with the right name is not a consolidation: the
// binary reads names, and follows nothing.
func TestAC3ADirectoryOrALinkNamedLikeAConsolidationIsNotOne(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	if err := os.MkdirAll(filepath.Join(root, ".sdlc", "consolidation-2026-09-01.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("consolidation-2026-07-15.md", filepath.Join(root, ".sdlc", "consolidation-2026-09-02.md")); err != nil {
		t.Skipf("this platform will not make a symbolic link here: %v", err)
	}
	us114Nudged(t, root, 3, "status")
	us114WantField(t, 3, "2026-07-15")
}

// AC-3: the newest is the one with the latest date, not the one written last
// and not the one a suffix makes sort last; a -2 suffix has the date it names.
func TestAC3TheNewestFileIsFoundByItsDateAndASuffixKeepsTheDate(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 21, 12, 0)),
		us114Done("US-102", us114Local(time.July, 22, 12, 0)),
		us114Done("US-103", us114Local(time.July, 23, 12, 0)),
	)
	// Written newest date first, so that the last file written is the oldest.
	for _, name := range []string{
		"consolidation-2026-07-20-2.md", "consolidation-2026-07-20.md",
		"consolidation-2026-07-10-3.md", "consolidation-2026-07-10.md", "consolidation-2026-01-31-2.md",
		"consolidation-2026-01-31.md",
	} {
		us114File(t, root, name)
		// A later modification time on the older file, where the platform
		// keeps one: a reader going by age would take it for the newest.
		later := time.Now().Add(time.Duration(len(name)) * time.Second)
		_ = os.Chtimes(filepath.Join(root, ".sdlc", name), later, later)
	}
	us114Nudged(t, root, 3, "status")
	us114WantField(t, 3, "2026-07-20")
}

// AC-3: a file whose only name is the suffixed one still dates the
// consolidation, and a story finished on that day does not count after it.
func TestAC3ASuffixedFileAloneSetsTheDate(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 15, 9, 0)),
		us114Done("US-102", us114Local(time.July, 16, 9, 0)),
		us114Done("US-103", us114Local(time.July, 17, 9, 0)),
		us114Done("US-104", us114Local(time.July, 18, 9, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15-2.md")
	us114Threshold(t, root, 1)
	us114WantField(t, 3, "2026-07-15")
}

// AC-3: the binary reads the names and not the contents: a file it could not
// open, and one that says another date, change nothing.
func TestAC3TheFilesContentsAreNeverRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a read-only mode does not stop a read on windows")
	}
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
	)
	writeFile(t, root, ".sdlc/consolidation-2026-07-15.md", "# Consolidation — 2026-12-31\n")
	if err := os.Chmod(filepath.Join(root, ".sdlc", "consolidation-2026-07-15.md"), 0); err != nil {
		t.Skipf("this platform will not take the permissions away: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, ".sdlc", "consolidation-2026-07-15.md"), 0o600) })
	us114Nudged(t, root, 3, "status")
	us114WantField(t, 3, "2026-07-15")
}

// AC-3: a story that finished on the day of the file does not count, one that
// finished the day after does, and one the day before does not: the calendar
// date, in the machine's zone, strictly later.
func TestAC3AStoryFinishedOnTheDayOfTheLastConsolidationDoesNotCount(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 14, 23, 30)),
		us114Done("US-102", us114Local(time.July, 15, 0, 30)),
		us114Done("US-103", us114Local(time.July, 15, 12, 0)),
		us114Done("US-104", us114Local(time.July, 15, 23, 30)),
		us114Done("US-105", us114Local(time.July, 16, 0, 30)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	us114Threshold(t, root, 1)
	us114WantField(t, 1, "2026-07-15")
	us114Nudged(t, root, 1, "status")
}

// AC-3: a story is done when the backlog says done, and nothing else counts,
// however late its retro was: not one being worked, not one ready, not one
// waiting for a person, and not a record whose story the backlog has lost.
func TestAC3OnlyAStoryTheBacklogCallsDoneCounts(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	late := us114Local(time.July, 20, 12, 0)
	us114Seed(t, root,
		us114Done("US-101", late),
		us114Story{id: "US-102", status: "in_progress", retro: late},
		us114Story{id: "US-103", status: "ready", retro: late},
		us114Story{id: "US-104", status: "awaiting_human", retro: late},
		us114Story{id: "US-105", status: "blocked", retro: late},
	)
	// A story gone from the backlog, with a record that says it finished.
	writeFile(t, root, ".sdlc/stories/US-900/gate-record.json", fmt.Sprintf(
		`{"story":"US-900","created":"2026-01-05T12:00:00Z","gates":{"retro":{"status":"pass","at":%q}},"events":[],"escalations":[],"approvals":[]}`+"\n",
		model.Timestamp(late)))
	us114File(t, root, "consolidation-2026-07-15.md")
	us114Threshold(t, root, 1)
	us114WantField(t, 1, "2026-07-15")
}

// AC-3: a story's finish is the time of its landed event where it has one, and
// the retro gate's where it has none.
func TestAC3AStorysFinishIsItsLandedEventElseItsRetroGate(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Story{id: "US-101", status: "done", retro: us114Local(time.July, 10, 12, 0), landed: us114Local(time.July, 17, 12, 0)},
		us114Story{id: "US-102", status: "done", retro: us114Local(time.July, 17, 12, 0), landed: us114Local(time.July, 10, 12, 0)},
		us114Story{id: "US-103", status: "done", retro: us114Local(time.July, 18, 12, 0)},
		us114Story{id: "US-104", status: "done", landed: us114Local(time.July, 19, 12, 0)},
		us114Story{id: "US-105", status: "done", retro: us114Local(time.July, 12, 12, 0)},
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	us114Threshold(t, root, 1)
	// US-101 (landed after), US-103 (retro after) and US-104 (landed after).
	us114WantField(t, 3, "2026-07-15")
}

// AC-3: a done story whose record gives no finish counts where there is no
// consolidation, since every finished story does, and not where there is one,
// since nothing says it finished after it.
func TestAC3ADoneStoryWithNoFinishOnRecordCountsOnlyWhereThereIsNoConsolidation(t *testing.T) {
	t.Parallel()
	for name, withFile := range map[string]bool{"no file": false, "a file": true} {
		t.Run(strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			us114Seed(t, root,
				us114Story{id: "US-101", status: "done", noRecord: true},
				us114Story{id: "US-102", status: "done"}, // a record, and a retro that never passed
				us114Done("US-103", us114Local(time.July, 17, 12, 0)),
			)
			us114Threshold(t, root, 1)
			if !withFile {
				us114WantField(t, 3, "")
				return
			}
			us114File(t, root, "consolidation-2026-07-15.md")
			us114WantField(t, 1, "2026-07-15")
		})
	}
}

// AC-1, AC-2, THREATS 3: a record the count cannot read never fails status or
// stop, and nothing it says goes to stdout: stop is the way out of a broken
// state.
func TestAC1AC2ARecordTheCountCannotReadNeverFailsStatusOrStop(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
		us114Done("US-104", us114Local(time.July, 20, 12, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	// The control: with every record sound, four stories finished and the line says so.
	us114Nudged(t, root, 4, "status")
	writeFile(t, root, ".sdlc/stories/US-102/gate-record.json", "{not json")
	commitEverything(t, root)

	status := mustRun(t, "status")
	base := us114Base(t, root, "status")
	if !strings.HasPrefix(status.stdout, base.stdout) {
		t.Errorf("status over a broken record is not status:\n%q\nwant it to begin with\n%q", status.stdout, base.stdout)
	}
	mustRun(t, "status", "--json")

	mustRun(t, "start")
	stop := mustRun(t, "stop")
	want := "Stopped the iteration on US-001. Nothing was recorded; `sdlc start` picks it up again.\n"
	if !strings.HasPrefix(stop.stdout, want) {
		t.Errorf("stop over a broken record printed %q, want it to begin with %q", stop.stdout, want)
	}
}

// ------------------------------------------------------------------ AC-2

const us114PutDown = "Stopped the iteration on US-001. Nothing was recorded; `sdlc start` picks it up again.\n"

// us114ThreeFinished is a project at the threshold: three stories finished
// since the last consolidation. Its control is status, which says so; it is
// there so that a stop that prints no line is one that had a line to print.
func us114ThreeFinished(t *testing.T) string {
	t.Helper()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
		us114Done("US-103", us114Local(time.July, 19, 12, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	us114Nudged(t, root, 3, "status")
	return root
}

// AC-2: a stop that puts an iteration down ends with the line at the
// threshold, after what it always says.
func TestAC2AStopThatPutsAnIterationDownEndsWithTheLineAtTheThreshold(t *testing.T) {
	t.Parallel()
	us114ThreeFinished(t)
	mustRun(t, "start")
	r := mustRun(t, "stop")
	if !strings.HasPrefix(r.stdout, us114PutDown) {
		t.Fatalf("stop does not begin with what it always says:\n%q", r.stdout)
	}
	us114CheckLine(t, us114Added(t, r.stdout, us114PutDown), 3)
}

// AC-2: with the setting 0, and with it above the count, stop prints what it
// printed before the line existed, and nothing else.
func TestAC2BelowTheThresholdAStopPrintsExactlyWhatItAlwaysDid(t *testing.T) {
	t.Parallel()
	for name, threshold := range map[string]int{"the_setting_at_zero": 0, "the_setting_above_the_count": 4} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := us114ThreeFinished(t)
			us114Threshold(t, root, threshold)
			mustRun(t, "start")
			if r := mustRun(t, "stop"); r.stdout != us114PutDown {
				t.Errorf("stop printed %q, want exactly %q", r.stdout, us114PutDown)
			}
		})
	}
}

// AC-2: one short of the threshold is below it, and the stop after the story
// that makes it three has the line.
func TestAC2OneShortOfTheThresholdStopSaysNothingAboutConsolidating(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	mustRun(t, "start")
	if r := mustRun(t, "stop"); r.stdout != us114PutDown {
		t.Errorf("stop with 2 finished of 3 printed %q, want exactly %q", r.stdout, us114PutDown)
	}

	us114Seed(t, root, us114Done("US-103", us114Local(time.July, 19, 12, 0)))
	mustRun(t, "start")
	r := mustRun(t, "stop")
	us114CheckLine(t, us114Added(t, r.stdout, us114PutDown), 3)
}

// AC-2: a stop with no iteration running ends no iteration, and prints no line.
func TestAC2AStopWithNoIterationRunningPrintsNoLine(t *testing.T) {
	t.Parallel()
	us114ThreeFinished(t)
	r := mustRun(t, "stop")
	if r.stdout != "No iteration was running.\n" {
		t.Errorf("stop with nothing running printed %q, want exactly %q", r.stdout, "No iteration was running.\n")
	}
}

// AC-2: where the file that names the iteration is unusable, stop ends it off
// the record and says nothing was running, and prints no line.
func TestAC2AStopOverAnUnusableIterationFilePrintsNoLine(t *testing.T) {
	t.Parallel()
	root := us114ThreeFinished(t)
	writeFile(t, root, ".sdlc/state/active", "")
	r := mustRun(t, "stop")
	if r.stdout != "No iteration was running.\n" {
		t.Errorf("stop printed %q, want exactly %q", r.stdout, "No iteration was running.\n")
	}
}

// AC-2: a refused stop ended nothing, and prints no line.
func TestAC2ARefusedStopPrintsNoLine(t *testing.T) {
	t.Parallel()
	us114ThreeFinished(t)
	setenv(t, store.SessionEnv, "session-owner")
	mustRun(t, "start")
	setenv(t, store.SessionEnv, "session-other")
	r := run(t, "stop")
	if r.code == 0 || !sessionRefused(r) {
		t.Fatalf("a stop from another session was not refused: exit %d\n%s", r.code, r.stderr)
	}
	if out := r.stdout + r.stderr; strings.Contains(out, "/sdlc:consolidate") {
		t.Errorf("a refusal carries the line:\n%s", out)
	}
	if r.stdout != "" {
		t.Errorf("a refusal wrote to stdout: %q", r.stdout)
	}
}

// AC-2: the story that finishes with this stop counts, so the stop that ends
// the third is the one that says so.
func TestAC2AStopThatFinishesAStoryCountsTheStoryItFinished(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	us114Seed(t, root,
		us114Done("US-101", us114Local(time.July, 17, 12, 0)),
		us114Done("US-102", us114Local(time.July, 18, 12, 0)),
	)
	us114File(t, root, "consolidation-2026-07-15.md")
	mustRun(t, "start")

	// Every gate passed, the retro the last of them, at a moment after the file.
	rec := model.Record{
		Story: "US-001", Created: model.Timestamp(us114Local(time.July, 16, 9, 0)),
		Gates: map[model.Gate]model.GateResult{}, Events: []model.Event{},
		Escalations: []model.Escalation{}, Approvals: []model.Approval{},
	}
	for _, g := range model.Gates {
		rec.Gates[g] = model.GateResult{Status: model.GatePass, At: model.Timestamp(us114Local(time.July, 20, 12, 0))}
	}
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".sdlc/stories/US-001/gate-record.json", string(body)+"\n")

	r := mustRun(t, "stop")
	const want = "Ended the iteration on US-001. Every gate passed, so the story is done and `sdlc start` moves on to the next one.\n"
	if !strings.HasPrefix(r.stdout, want) {
		t.Fatalf("stop did not finish the story:\n%q", r.stdout)
	}
	us114CheckLine(t, us114Added(t, r.stdout, want), 3)
}

// AC-2, THREATS 4: with --json stdout stays one JSON object, with no prose and
// no field for the nudge, at the threshold as below it.
func TestAC2StopJSONCarriesNothingOfTheNudge(t *testing.T) {
	t.Parallel()
	us114ThreeFinished(t)
	mustRun(t, "start")
	r := mustRun(t, "stop", "--json")
	if strings.Contains(r.stdout, "consolidate") || strings.Count(r.stdout, "\n") != 1 {
		t.Errorf("stop --json is not one line of the usual object:\n%q", r.stdout)
	}
	got := decode[stopPayload](t, r)
	if want := (stopPayload{OK: true, Story: "US-001", WasA: true}); got != want {
		t.Errorf("stop --json = %+v, want %+v", got, want)
	}
}
