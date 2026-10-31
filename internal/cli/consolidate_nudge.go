package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// consolidateState is what `sdlc status` says of stories waiting for
// /sdlc:consolidate: how many have finished since the last one, and the date of
// it. Last is null where there has been none, so a reader can tell no date from
// no field.
type consolidateState struct {
	Finished int     `json:"finished"`
	Last     *string `json:"last"`
}

// The names of a consolidation file, which the skill writes into the main
// checkout's .sdlc: consolidation-<YYYY-MM-DD>.md, or with -2, -3 and so on
// where that day already has one.
const (
	consolidationPrefix = "consolidation-"
	consolidationSuffix = ".md"
	dateLayout          = "2006-01-02"
)

// consolidationDate is the date a file's name gives, and whether it is the name
// of a consolidation at all. Nothing else is read of the file: the binary opens
// no file here, so a name can say only what it says, and only the digits that
// passed a calendar check are ever printed.
//
// Not a regular expression: a package-level one is compiled at start-up, and
// start-up is on the latency path of every tool call.
func consolidationDate(name string) (time.Time, bool) {
	mid, ok := strings.CutPrefix(name, consolidationPrefix)
	if !ok {
		return time.Time{}, false
	}
	mid, ok = strings.CutSuffix(mid, consolidationSuffix)
	if !ok || len(mid) < len(dateLayout) {
		return time.Time{}, false
	}
	date, rest := mid[:len(dateLayout)], mid[len(dateLayout):]
	// The number is never converted, so one of any length is harmless.
	if rest != "" && (len(rest) < 2 || rest[0] != '-' || strings.Trim(rest[1:], "0123456789") != "") {
		return time.Time{}, false
	}
	t, err := time.Parse(dateLayout, date)
	return t, err == nil
}

// lastConsolidation is the date of the newest consolidation file in the main
// checkout's .sdlc, by the date in its name: not by sorting the names, which
// puts -31-2 before -31, and not by age, which a copy or a checkout resets.
// A directory or a link with the right name is not one.
func lastConsolidation(root string) (date time.Time, found bool, err error) {
	entries, err := os.ReadDir(filepath.Join(root, config.Dir))
	if err != nil {
		return time.Time{}, false, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if t, ok := consolidationDate(e.Name()); ok && (!found || t.After(date)) {
			date, found = t, true
		}
	}
	return date, found, nil
}

// finishOf is when a story finished: when it landed, else when its retro gate
// passed, the last of the nine. ok is false where its record says neither.
func finishOf(r *model.Record) (finish time.Time, ok bool) {
	for _, e := range slices.Backward(r.Events) {
		if e.Type != model.EventLanded {
			continue
		}
		t, err := time.Parse(time.RFC3339, e.At)
		return t, err == nil
	}
	if g := r.Gates[model.GateRetro]; g.Status == model.GatePass {
		t, err := time.Parse(time.RFC3339, g.At)
		return t, err == nil
	}
	return time.Time{}, false
}

// consolidateNudge is how many done stories have finished since the last
// consolidation, and whether that is enough to say so: the setting is not 0 and
// the count has reached it. An error is a count that cannot be stated, never
// one that is short by the story that could not be read.
func consolidateNudge(s *store.Store, backlog *model.Backlog) (state consolidateState, due bool, err error) {
	threshold := s.Config().Consolidate.NudgeAfter
	var done []string
	for _, story := range backlog.Stories {
		if story.Status == model.StatusDone {
			done = append(done, story.ID)
		}
	}
	// A count cannot be above the stories that are done, and with no record
	// read there is nothing to fail.
	if threshold <= 0 || len(done) < threshold {
		return state, false, nil
	}
	state.Finished = len(done)
	last, found, err := lastConsolidation(s.LockRoot())
	if err != nil {
		return state, false, err
	}
	if found {
		day := last.Format(dateLayout)
		state.Last = &day
		if state.Finished, err = finishedAfter(s, done, last); err != nil {
			return state, false, err
		}
	}
	return state, state.Finished >= threshold, nil
}

// finishedAfter is how many of the stories finished on a calendar day after
// last. The day is the machine's own: the skill writes the file's date as the
// person's day, and a UTC date would put a story finished at five in the
// evening, west of Greenwich, on the next one, and count it again straight
// after the run that read it. A story finished on the day of the file is not
// counted, which is the only reading under which that run ends the nudge. A
// story whose record gives no finish is not counted: nothing says it finished
// after.
func finishedAfter(s *store.Store, ids []string, last time.Time) (int, error) {
	n := 0
	for _, id := range ids {
		record, err := s.Record(id)
		if err != nil {
			return 0, err
		}
		finish, ok := finishOf(record)
		if !ok {
			continue
		}
		y, m, d := finish.Local().Date()
		if time.Date(y, m, d, 0, 0, 0, 0, time.UTC).After(last) {
			n++
		}
	}
	return n, nil
}

// consolidateOf is consolidateNudge for a command that must not fail over it:
// status, and stop, which is the way out of a broken state. A count that cannot
// be made says nothing; it is logged for whoever asks why the line is missing.
func consolidateOf(s *store.Store, backlog *model.Backlog) *consolidateState {
	state, due, err := consolidateNudge(s, backlog)
	if err != nil {
		slog.Debug("the line about /sdlc:consolidate was left out: the stories finished since the last one could not be counted",
			"err", pathless(err))
		return nil
	}
	if !due {
		return nil
	}
	return &state
}

// consolidateLine is the line a command ends with, addressed to a person: the
// skill is theirs to run, and a model that reads this cannot start it.
func consolidateLine(c *consolidateState) string {
	since := "and none has been consolidated"
	if c.Last != nil {
		since = "since the last consolidation on " + *c.Last
	}
	return strconv.Itoa(c.Finished) + " " + plural(c.Finished, "story has", "stories have") + " finished " + since +
		": a person can run /sdlc:consolidate to see what they have in common."
}

// printConsolidateNudge ends a stop with the line, where there is one. It reads
// the backlog again, since the stop that ended the iteration may have just made
// a story done.
func printConsolidateNudge(w io.Writer, s *store.Store) {
	backlog, err := s.Backlog()
	if err != nil {
		slog.Debug("the line about /sdlc:consolidate was left out: the backlog could not be read", "err", pathless(err))
		return
	}
	if state := consolidateOf(s, backlog); state != nil {
		fmt.Fprintln(w, consolidateLine(state))
	}
}
