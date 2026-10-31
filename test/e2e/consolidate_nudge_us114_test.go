// US-114 end to end: the nudge to run /sdlc:consolidate, as the built binary
// prints it. What these hold that the unit tests in internal/cli cannot:
//
//   - the zone rule. A story's finish is a UTC instant and the file's date has
//     no zone, so the date is compared in the machine's own zone. Go reads TZ
//     once, when the process starts, so a test process cannot change it for
//     itself, and the binary is run here with TZ in its environment instead.
//     Windows takes no TZ at all, and these tests skip there with the reason.
//   - a stop that lands a story from its worktree, where the line ends stdout
//     and the main checkout's .sdlc is the one read.
//   - a status in a story's worktree, which sees none of the main checkout's
//     untracked files and has to give the main checkout's answer.
//
// Helpers carry a nudge prefix.

package e2e

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// nudgeStory is a done story of a fixture, whose retro gate passed at the
// instant given, as the loop writes one: UTC, to the second.
type nudgeStory struct {
	id, retro string
}

// nudgeSeed adds the stories to the backlog as done, gives each a record whose
// retro gate passed when it says, and commits the lot.
func nudgeSeed(t *testing.T, dir string, stories ...nudgeStory) {
	t.Helper()
	path := filepath.Join(dir, "user_stories.json")
	var backlog map[string]any
	readJSON(t, path, &backlog)
	list, _ := backlog["stories"].([]any)
	template, _ := list[0].(map[string]any)
	for i, s := range stories {
		next := maps.Clone(template)
		next["id"], next["title"], next["status"], next["priority"] = s.id, "Story "+s.id, "done", 50+i
		list = append(list, next)
		rec := model.Record{
			Story: s.id, Created: "2026-01-05T12:00:00Z",
			Gates:  map[model.Gate]model.GateResult{model.GateRetro: {Status: model.GatePass, At: s.retro}},
			Events: []model.Event{}, Escalations: []model.Escalation{}, Approvals: []model.Approval{},
		}
		if err := os.MkdirAll(filepath.Join(dir, ".sdlc", "stories", s.id), 0o755); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, filepath.Join(dir, ".sdlc", "stories", s.id, "gate-record.json"), rec)
	}
	backlog["stories"] = list
	writeJSON(t, path, backlog)
	commitEverything(t, dir, "finished stories")
}

// nudgeIn runs the binary in c with TZ in its environment, or the machine's
// own where tz is "".
func nudgeIn(t *testing.T, c checkout, tz string, args ...string) string {
	t.Helper()
	cmd := c.command(t, "", args...)
	if tz != "" {
		cmd.Env = append(cmd.Env, "TZ="+tz)
	}
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("sdlc %s (TZ=%s) in %s: %v\n%s%s", strings.Join(args, " "), tz, c.dir, err, out.String(), errOut.String())
	}
	noteRun(args...)
	return out.String()
}

// nudgeSaid is what a status in zone tz says of consolidating: whether it ends
// with the line, and what status --json carries (finished is -1 and last "" where
// the field is absent).
func nudgeSaid(t *testing.T, c checkout, tz string) (line bool, finished int, last string) {
	t.Helper()
	text := nudgeIn(t, c, tz, "status")
	line = strings.Contains(text, "/sdlc:consolidate")
	if line {
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		if final := lines[len(lines)-1]; !strings.Contains(final, "/sdlc:consolidate") {
			t.Errorf("status (TZ=%s) names /sdlc:consolidate and does not end with it:\n%s", tz, text)
		}
	}
	raw := nudgeIn(t, c, tz, "status", "--json")
	var doc struct {
		Consolidate *struct {
			Finished int     `json:"finished"`
			Last     *string `json:"last"`
		} `json:"consolidate"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("status --json (TZ=%s) is not JSON: %v\n%s", tz, err, raw)
	}
	if doc.Consolidate == nil {
		return line, -1, ""
	}
	if doc.Consolidate.Last != nil {
		last = *doc.Consolidate.Last
	}
	return line, doc.Consolidate.Finished, last
}

// nudgeZones skips where the zones the test names cannot be had: Windows reads
// no TZ, and a machine without the zone database would fall back to UTC and
// make every case below read the same way.
func nudgeZones(t *testing.T, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Go takes the zone from the system on Windows and ignores TZ, so the binary cannot be run in another zone")
	}
	for _, name := range names {
		if _, err := time.LoadLocation(name); err != nil {
			t.Skipf("this machine has no zone %s, so the binary cannot be run in it: %v", name, err)
		}
	}
}

// AC-3: the date of a finish is its calendar date in the machine's zone, which
// is the zone the file's date is written in. The same repository gives
// another answer in another zone, and neither is the UTC date's.
func TestAC3AStorysFinishIsDatedInTheMachinesOwnZoneNotInUTC(t *testing.T) {
	parallel(t)
	nudgeZones(t, "Pacific/Auckland", "America/Los_Angeles")
	binary := build(t)

	// Three stories finished at 12:30 UTC on 19 July. That is half past
	// midnight on the 20th in Auckland, and half past five in the morning of
	// the 19th in Los Angeles: after the file's day in the one, on it in the other.
	east := repository(t, binary)
	nudgeSeed(t, east.dir,
		nudgeStory{"US-101", "2026-07-19T12:30:00Z"},
		nudgeStory{"US-102", "2026-07-19T12:30:00Z"},
		nudgeStory{"US-103", "2026-07-19T12:30:00Z"})
	writeFile(t, filepath.Join(east.dir, ".sdlc", "consolidation-2026-07-19.md"), "")
	// Three more at 03:00 UTC on the 20th: still the evening of the 19th in
	// Los Angeles, and the 20th everywhere east of it.
	west := repository(t, binary)
	nudgeSeed(t, west.dir,
		nudgeStory{"US-101", "2026-07-20T03:00:00Z"},
		nudgeStory{"US-102", "2026-07-20T03:00:00Z"},
		nudgeStory{"US-103", "2026-07-20T03:00:00Z"})
	writeFile(t, filepath.Join(west.dir, ".sdlc", "consolidation-2026-07-19.md"), "")

	for _, tc := range []struct {
		name string
		repo checkout
		tz   string
		want bool
	}{
		{"half past midnight on the 20th in Auckland is after the 19th", east, "Pacific/Auckland", true},
		{"half past five on the 19th in Los Angeles is the 19th", east, "America/Los_Angeles", false},
		{"half past twelve on the 19th in UTC is the 19th", east, "UTC", false},
		{"the evening of the 19th in Los Angeles is the 19th", west, "America/Los_Angeles", false},
		{"three in the morning on the 20th in UTC is the 20th", west, "UTC", true},
		{"three in the afternoon on the 20th in Auckland is the 20th", west, "Pacific/Auckland", true},
	} {
		line, finished, last := nudgeSaid(t, tc.repo, tc.tz)
		switch {
		case tc.want && (!line || finished != 3 || last != "2026-07-19"):
			t.Errorf("%s: TZ=%s printed the line %v, finished %d, last %q; want the line, 3 and 2026-07-19", tc.name, tc.tz, line, finished, last)
		case !tc.want && (line || finished != -1):
			t.Errorf("%s: TZ=%s printed the line %v, finished %d; want neither the line nor the field", tc.name, tc.tz, line, finished)
		}
	}
}

// AC-1, AC-3, THREATS 3: a status in the worktree of a story reads the main
// checkout's .sdlc, as it reads its backlog and records, where the worktree has
// no copy of an untracked file: the date comes from a file only the main checkout
// holds, and the count is the one status gives there.
func TestAC1AC3AStatusInAStoryWorktreeGivesTheMainCheckoutsCount(t *testing.T) {
	parallel(t)
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
	nudgeSeed(t, main.dir,
		// Before the file: none of these counts, and a status that did not
		// see the file would count them.
		nudgeStory{"US-101", "2026-07-05T12:00:00Z"},
		nudgeStory{"US-102", "2026-07-05T12:00:00Z"},
		nudgeStory{"US-103", "2026-07-05T12:00:00Z"},
		nudgeStory{"US-104", "2026-07-17T12:00:00Z"},
		nudgeStory{"US-105", "2026-07-17T12:00:00Z"},
		nudgeStory{"US-106", "2026-07-17T12:00:00Z"})
	// Untracked, so that no worktree is cut with a copy of it.
	writeFile(t, filepath.Join(main.dir, ".sdlc", "consolidation-2026-07-10.md"), "")
	w := startIn(t, main, main.as(sessionFirst))

	for _, c := range []checkout{main.as(""), w.checkout} {
		line, finished, last := nudgeSaid(t, c, "")
		if !line || finished != 3 || last != "2026-07-10" {
			t.Errorf("status in %s: the line %v, finished %d, last %q; want the line, 3 and 2026-07-10", c.dir, line, finished, last)
		}
	}
	if _, err := os.Stat(filepath.Join(w.dir, ".sdlc", "consolidation-2026-07-10.md")); err == nil {
		t.Fatal("the worktree holds a copy of the main checkout's untracked file, so this proves nothing")
	}
}

// AC-2: the stop that lands a story from its worktree ends stdout with the
// line, counting the story it has just landed, in a project that asks for it
// after one.
func TestAC2TheStopThatLandsAStoryEndsItsOutputWithTheLine(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := landRepository(t, binary)
	setConfig(t, main.dir, "consolidate", "nudge_after", 1)
	commitEverything(t, main.dir, "ask to be told after one story")
	s := landFinished(t, binary, main)
	landBookkeeping(t, s, true)

	s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
	stdout, stderr, code := s.here().outcome(t, "", "stop")
	if code != 0 {
		t.Fatalf("the landing stop exited %d:\n%s%s", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "/sdlc:consolidate") || !regexp.MustCompile(`\b1\b`).MatchString(last) {
		t.Errorf("the landing stop's stdout does not end with the line for one finished story:\n%s", stdout)
	}
	if strings.Count(stdout, "/sdlc:consolidate") != 1 {
		t.Errorf("the landing stop says it more than once:\n%s", stdout)
	}
	if strings.Contains(stderr, "/sdlc:consolidate") {
		t.Errorf("the line went to stderr, which is where the warnings go:\n%s", stderr)
	}
}
