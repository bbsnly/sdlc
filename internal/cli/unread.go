package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// unreadDecisions is what `sdlc status` says of the decisions no ack of a
// person's has covered (US-131 AC-9, the settled IQ-2): a decision is read once
// the ack covers its story's commit, as `sdlc log` reads a story, so a story
// under way counts whole and a committed one where the ack does not cover it.
type unreadDecisions struct {
	total, committed int
	underWay         []storyDecisions
	// unknown is every story whose record could not be read, so that a count
	// short of them is never shown as complete (security F-5 of the design
	// review).
	unknown []string
}

// storyDecisions is how many decisions one story holds.
type storyDecisions struct {
	story string
	n     int
}

// committedDecisions is a committed story's record and its decisions.
type committedDecisions struct {
	record *model.Record
	n      int
}

// countUnreadDecisions counts the decisions of every story the backlog holds
// and has not dropped: a dropped story, or one the backlog no longer holds,
// never commits, so no ack could ever clear it, and its decisions stay in its
// DECISIONS.md and in `sdlc trace --all` (human-advocate F-3). A record that
// cannot be read does not fail status: it is said on warn, and listed.
func countUnreadDecisions(ctx context.Context, s *store.Store, backlog *model.Backlog, warn io.Writer) unreadDecisions {
	var u unreadDecisions
	var committed []committedDecisions
	for i := range backlog.Stories {
		id := backlog.Stories[i].ID
		if backlog.Stories[i].Status == model.StatusDropped || !s.RecordStored(id) {
			continue
		}
		record, err := s.Record(id)
		if err != nil {
			u.unknown = append(u.unknown, id)
			fmt.Fprintf(warn, "sdlc: the decisions of %s are not counted: %s could not be read (%s); "+
				"they are counted again once it reads, and git history holds the last committed copy of it\n",
				id, store.RecordPath(id), oneLine(err.Error()))
			continue
		}
		n := 0
		for _, e := range record.Events {
			if e.Type == model.EventDecision {
				n++
			}
		}
		switch {
		case n == 0:
		case record.Pass(model.GateCommit):
			committed = append(committed, committedDecisions{record, n})
		default:
			u.underWay = append(u.underWay, storyDecisions{id, n})
			u.total += n
		}
	}
	for _, c := range unreadCommitted(ctx, s, committed, warn) {
		u.committed += c.n
	}
	u.total += u.committed
	return u
}

// unreadCommitted is the committed stories whose commit the person's ack does
// not cover. With no ack, git is not asked: nothing is covered (red-team F-10).
// An ack git cannot read covers nothing either, and is said, since counting a
// story read that nobody read is the unsafe direction. Every commit named in
// full is asked of git at once (gitx.NotUnder); one the record names otherwise,
// or every one where git fails, is placed as `sdlc log` places it.
func unreadCommitted(ctx context.Context, s *store.Store, committed []committedDecisions, warn io.Writer) []committedDecisions {
	if len(committed) == 0 {
		return nil
	}
	since, ok := acknowledgedSince(ctx, s, warn)
	if !ok {
		return committed
	}
	return notCoveredBy(ctx, s, since, committed)
}

// acknowledgedSince is the commit a person's last ack names, resolved, and
// false where there is none or git cannot read it, which is said.
func acknowledgedSince(ctx context.Context, s *store.Store, warn io.Writer) (string, bool) {
	named, ok, err := s.Acknowledged()
	if err != nil {
		fmt.Fprintf(warn, "sdlc: every committed story's decisions are counted as not acknowledged: %s\n", oneLine(err.Error()))
		return "", false
	}
	if !ok {
		return "", false
	}
	since, err := gitx.Resolve(ctx, s.Root(), named)
	if err != nil {
		fmt.Fprintf(warn, "sdlc: every committed story's decisions are counted as not acknowledged: %s names %s, which git "+
			"cannot read as a commit (%s); %s\n", store.AcknowledgedFile, quote(named), oneLine(err.Error()),
			sdlcerr.New(sdlcerr.AcknowledgedUnknown, "", "").Fix)
		return "", false
	}
	return since, true
}

// notCoveredBy is the committed stories whose commit since does not cover.
func notCoveredBy(ctx context.Context, s *store.Store, since string, committed []committedDecisions) []committedDecisions {
	var full []string
	for _, c := range committed {
		if gitx.IsFullHash(c.record.Commit) {
			full = append(full, c.record.Commit)
		}
	}
	notUnder, err := gitx.NotUnder(ctx, s.Root(), since, full)
	if err != nil {
		notUnder = nil
	}
	// HEAD is asked once, and only where a commit is left to place as the log does.
	head := sync.OnceValues(func() (string, error) { return gitx.Head(ctx, s.Root()) })
	var unread []committedDecisions
	for _, c := range committed {
		answer, asked := notUnder[c.record.Commit]
		if !asked {
			answer = unplaced(ctx, s.Root(), head, since, c.record)
		}
		if answer {
			unread = append(unread, c)
		}
	}
	return unread
}

// unplaced reports whether `sdlc log` would list a committed story, placed as
// it places one, so that a story git cannot place counts as not acknowledged.
func unplaced(ctx context.Context, root string, head func() (string, error), since string, record *model.Record) bool {
	at, err := head()
	if err != nil {
		return true
	}
	entry := logEntry{Story: record.Story}
	read, err := placeAndCheck(ctx, root, at, since, &entry, record)
	return err != nil || !read
}

// line is status's text line, or "" where there is nothing to say. Each story
// under way is named with its own DECISIONS.md, up to three of them
// (human-advocate F-14).
func (u unreadDecisions) line() string {
	if u.total == 0 && len(u.unknown) == 0 {
		return ""
	}
	count := strconv.Itoa(u.total)
	if len(u.unknown) > 0 {
		count = "at least " + count
	}
	var parts []string
	if u.committed > 0 {
		parts = append(parts, strconv.Itoa(u.committed)+" in stories committed since your last ack, listed by sdlc log")
	}
	if len(u.underWay) > 0 {
		const named = 3
		var stories []string
		for _, sd := range u.underWay[:min(len(u.underWay), named)] {
			stories = append(stories, fmt.Sprintf("%d in %s (%s)", sd.n, sd.story, store.DecisionsPath(sd.story)))
		}
		if more := len(u.underWay) - named; more > 0 {
			stories = append(stories, strconv.Itoa(more)+" more")
		}
		parts = append(parts, andList(stories)+", under way")
	}
	if len(u.unknown) > 0 {
		parts = append(parts, "not counted: "+strings.Join(u.unknown, ", ")+", whose records could not be read")
	}
	return "Decisions: " + count + " not yet acknowledged (" + strings.Join(parts, "; ") + ")"
}

// andList joins items as a sentence does: "a", "a and b", "a, b and c".
func andList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
