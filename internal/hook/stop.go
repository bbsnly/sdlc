package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/store"
)

// turnReply is what Claude Code reads from a Stop or PostToolUse hook. Decision
// "block" sends the session back to work, and Reason is what it is told.
type turnReply struct {
	Continue      bool   `json:"continue"`
	Decision      string `json:"decision,omitempty"`
	Reason        string `json:"reason,omitempty"`
	SystemMessage string `json:"systemMessage,omitempty"`
}

// RunbookLastSection is the last section of the /sdlc:next runbook. Compaction
// keeps only the start of a long skill, so a runbook that no longer ends with it
// was cut short, and a stop sent back says so.
const RunbookLastSection = "Rework, at any gate"

// subagentWait is how long a listed subagent holds the count, measured from the
// first stop let through at the story's recorded progress. US-006 was handed to
// a person while its researcher was still at work, because every stop made
// waiting for it was counted. Without an end, a subagent that never finishes,
// or a new one started each time the last ends, would hold the count for good.
// It is fixed and not a setting, so the session it bounds cannot lengthen it.
const subagentWait = 30 * time.Minute

// decideStop is the stop guard.
//
// A session that ends its turn mid-story -- without finishing the gate, handing
// the story to a person, or ending the iteration -- leaves the story where
// nobody is looking: the next session may not come, and nothing says this one
// gave up. So the stop is sent back once, with what to do instead.
//
// A stop already sent back goes through, so the session is never held in a
// loop. The count is per story and starts again whenever anything is recorded
// on it, and when it reaches loop.max_stop_blocks the story goes to a person.
// Like every other rule here it fails open: anything it cannot read lets the
// stop through.
//
// A stop made while a subagent of the session is still running is not the
// session giving up: it has nothing to record until the subagent reports, and
// Claude Code wakes it then. US-006 was handed to a person after three such
// stops, with its researcher still at work. So while the Stop event lists a
// subagent the stop is let through with the ordinary allow and is not counted.
// The one thing the task list cannot do is make the payload unreadable or the
// stop uncounted by being wrong: a list the guard cannot read counts the stop
// as before, and the debug log says so.
//
// That wait has an end, subagentWait from the first stop let through at the
// story's recorded progress, kept as waiting_since beside the count. Only
// recording something on the story starts another: not a new stop, a new
// subagent or a stop counted in between, or one subagent after another would
// hold the count for good. The clock is a parameter because the end of a wait
// is tested by moving time, not by waiting for it.
func decideStop(raw []byte, getenv func(string) string, warn func(string), now func() time.Time) turnReply {
	allow := turnReply{Continue: true}
	project, story, ok := stopStory(raw, getenv, warn)
	if !ok {
		return allow
	}

	cfg, err := config.Load(project)
	if err != nil {
		cfg = config.Default()
	}
	limit := cfg.Loop.MaxStopBlocks
	if limit <= 0 {
		return allow
	}
	pl := placeOf(project)
	s := store.NewIn(&config.Project{Root: project, Config: cfg}, pl.co, pl.err)
	// Decided from what is there once the lock is held. Read before it, a stop
	// that arrived while a command was recording progress counted the record as
	// it was, and handed the story to a person the moment after it moved; two
	// sessions stopping at once each kept a count the other overwrote.
	g, err := store.Lock(s.LockRoot(), "hook Stop")
	if err != nil {
		slog.Debug("stop guard could not take the lock", "err", err)
		return allow
	}
	defer g.Release()
	if !stopIsHeldHere(s, story) {
		return allow
	}
	progress, err := s.RecordProgress(story)
	if err != nil {
		slog.Debug("stop guard could not read the record", "err", err)
		return allow
	}
	count := s.StopCount()
	if count.Story != story || count.Progress != progress {
		count = store.StopCount{Story: story, Progress: progress}
	}
	// Read last, and only for a stop that would otherwise be counted: a session
	// not working the story, a stop already sent back and a guard turned off are
	// answered above with nothing read from the list.
	//
	// And before the limit: a story already at it is not handed over on the
	// strength of one more stop made while its subagent is still at work.
	//
	// The lines logged here carry a fixed message, the wait's label and the
	// count. Never the list, and never the stored time, which is whatever the
	// file held.
	count, letThrough := applySubagentWait(s, raw, count, now)
	if letThrough {
		return allow
	}
	if count.Blocks >= limit {
		return handOver(s, story, count.Blocks)
	}
	count.Blocks++
	if err := s.SaveStopCount(count); err != nil {
		slog.Debug("stop guard could not keep its count", "err", err)
		return allow
	}
	return turnReply{Continue: true, Decision: "block", Reason: stopReason(pl, story, count.Blocks, limit)}
}

// stopStory is the story a Stop event could send back to, and whether there is
// one: not with enforcement turned off, an unreadable event, a session that
// holds no story, or a stop the hook has already sent back once.
func stopStory(raw []byte, getenv func(string) string, warn func(string)) (project, story string, ok bool) {
	if getenv("SDLC_ENFORCE") == "0" {
		return "", "", false
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", "", false
	}
	project, story = findLoop(getenv, p, "", warn)
	if story == "" || p.StopHookActive {
		return "", "", false
	}
	return project, story, true
}

// stopIsHeldHere reports whether the story is the active one and this checkout
// holds it.
func stopIsHeldHere(s *store.Store, story string) bool {
	if active, err := s.Active(); err != nil || active != story {
		return false
	}
	// A copy of a story another checkout holds is not this session's to be
	// sent back to: the work carries on where it is held.
	if _, elsewhere, err := s.HeldElsewhere(story); err == nil && elsewhere {
		return false
	}
	return true
}

// applySubagentWait applies the wait a listed subagent earns a stop. letThrough is
// true when the stop is let through uncounted; the count comes back with the
// time the wait began, if this stop began it.
func applySubagentWait(s *store.Store, raw []byte, count store.StopCount, now func() time.Time) (store.StopCount, bool) {
	listed, known := subagentListed(raw)
	switch {
	case !known:
		slog.Debug("stop guard cannot tell whether a subagent is running, so the stop is counted")
	case listed:
		at := now().UTC()
		wait := waitFrom(count.WaitingSince, at)
		if wait == waitBegins {
			// To the second, so the time kept is never after the moment it was
			// taken, which would read back as a clock that had gone backwards.
			count.WaitingSince = at.Format(time.RFC3339)
			if err := s.SaveStopCount(count); err != nil {
				slog.Debug("stop guard could not keep when its wait began", "err", err)
			}
		}
		// A wait that is running writes nothing: the stop gains the session the
		// wait and no more, and the time it began is not any later stop's to move.
		if wait == waitBegins || wait == waitRunning {
			slog.Debug("stop guard let the stop through for a subagent", "wait", wait, "blocks", count.Blocks)
			return count, true
		}
		slog.Debug("stop guard counted the stop with a subagent listed", "wait", wait, "blocks", count.Blocks)
	}
	return count, false
}

// subagentListed reads `background_tasks` from a Stop event: whether an entry
// is a subagent, and whether the list could be read at all.
//
// It is read apart from the payload. As a field of that type, a list of the
// wrong shape made the whole payload unreadable, and an unreadable payload is
// let through uncounted: a list that will not read would have been a way never
// to be counted. Here it is "cannot tell", which the caller counts. So is no
// list, a null one, and one with an entry that is not an object.
//
// Of an entry only `type` is decoded, and it is the label when it is the string
// "subagent" and nothing near it: the key by its exact spelling, because an
// entry is a map and not a struct. The reference lists no values for `status`,
// and the rest of an entry is text the model asked for, so none of it is read,
// kept, logged or said back. An entry whose label is missing or not a string is
// some other work, and does not spoil the list.
//
// The list's own key is not matched that closely. It is found as encoding/json
// finds a struct field: in any letter case, and the last one if the event
// repeats it. Claude Code writes neither, so only an event made by hand carries
// one, and that gains what a real event gains anyway: the one wait.
func subagentListed(raw []byte) (listed, known bool) {
	var event struct {
		Tasks []map[string]json.RawMessage `json:"background_tasks"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Tasks == nil {
		return false, false
	}
	for _, entry := range event.Tasks {
		var label string
		if json.Unmarshal(entry["type"], &label) == nil && label == "subagent" {
			return true, true
		}
	}
	return false, true
}

// What waitFrom says of the wait a count holds. They are also what the debug
// log calls it.
const (
	waitBegins     = "begins"
	waitRunning    = "running"
	waitRunOut     = "run out"
	waitUnreadable = "unreadable"
	waitAhead      = "ahead of the clock"
)

// waitFrom reads when a count says its wait began, against the time now.
//
// No wait has begun only when since is the empty string, which is a count file
// with no time in it. It is not trimmed, and the year 1 is not taken for
// absent: a value that is there and is not a time the wait can be measured
// from counts the stop, and starts no new wait. Read as absent, spoiling the
// time would have been the way to another thirty minutes.
//
// A time after now is asked about on its own, before the subtraction. Left to
// "less than thirty minutes have passed", it is a wait that never ends.
func waitFrom(since string, now time.Time) string {
	if since == "" {
		return waitBegins
	}
	began, err := time.Parse(time.RFC3339, since)
	switch {
	case err != nil:
		return waitUnreadable
	case began.After(now):
		return waitAhead
	case now.Sub(began) >= subagentWait:
		return waitRunOut
	}
	return waitRunning
}

// stopReason says what to do instead of stopping, naming the gate to work.
func stopReason(pl place, story string, blocks, limit int) string {
	next := "the next gate"
	if record, _, err := pl.record(story); record != nil && err == nil {
		gate, remaining := record.NextGate()
		if !remaining {
			return fmt.Sprintf("every gate on %s has passed and the iteration is still open: "+
				"end it with `sdlc stop`.", story)
		}
		next = string(gate)
	}
	return fmt.Sprintf("%s is still being worked on. Work %s and record it, or hand the story to a "+
		"person with `sdlc escalate <type> --message \"...\"` if it needs one. If the /sdlc:next "+
		"runbook in this conversation no longer ends with %q, compaction cut it short: do not work from "+
		"memory, tell the person to type /sdlc:next to pick the story up where it is, and stop. A story "+
		"left mid-gate "+
		"is one nobody is looking at, so after %d stops in a "+
		"row with nothing recorded the loop hands it to a person itself (this was %d).",
		story, next, RunbookLastSection, limit, blocks)
}

// handOver gives a story that keeps stopping to a person, and lets the stop
// through. Holding the session any longer would not change what it does. The
// caller holds the project's lock.
func handOver(s *store.Store, story string, blocks int) turnReply {
	reply := turnReply{Continue: true}
	failed := "sdlc: " + story + " kept stopping with nothing recorded, and could not be handed " +
		"to a person. Run `sdlc doctor` to see why."

	ctx, cancel := context.WithTimeout(context.Background(), treeTimeout)
	defer cancel()
	// Read from a linked worktree, the record is not beside the reader: the
	// worktree's own copy, if git checked one out, is not it.
	where := ""
	if co, err := s.Checkout(); err == nil && co.Linked() {
		where = " in the repository's main checkout"
	}
	question := fmt.Sprintf("the session stopped %d times in a row without recording anything on "+
		"the story. Read .sdlc/stories/%s/gate-record.json%s for where it got to, and answer with "+
		"`sdlc approve %s` to let the loop carry on.", blocks+1, story, where, story)
	if _, err := s.Escalate(ctx, story, "loop_stalled", question); err != nil {
		slog.Debug("stop guard could not escalate", "err", err)
		reply.SystemMessage = failed
		return reply
	}
	if err := s.ClearStopCount(); err != nil {
		slog.Debug("stop guard could not clear its count", "err", err)
	}
	reply.SystemMessage = fmt.Sprintf("sdlc: %s stopped %d times in a row with nothing recorded, so "+
		"it has been handed to a person and the iteration has ended. `sdlc status` shows the question; "+
		"answer it with `sdlc approve %s`.", story, blocks+1, story)
	return reply
}
