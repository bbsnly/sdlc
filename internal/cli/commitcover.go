package cli

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// What a person's approval covers, for a story in its own worktree in a tier
// that waits for one.
//
// The approval is given before the story is committed, so it is of the files:
// no commit exists yet for a person to read. The messages and authors are
// fixed afterwards, when the commit gate passes, and from then on the story is
// held to those commits -- at the commit gate run again, and at the stop that
// lands it. US-092: a message or an author amended after the gate left the
// tree, the change and every review as they were, and landed on the approval
// the story had.
//
// Anything the loop itself comes to commit above the recorded commit in a tier
// that waits needs its own rule in heldTo: today that is the bookkeeper's
// CODEMAP.md commit and nothing else.

// eventCommitRecorded is the event that keeps the commit the commit gate
// recorded once Record.forgetCommit has dropped it: its message is that
// commit's full hash. Nothing else writes an event of this type.
const eventCommitRecorded = "commit_recorded"

// eventReplayed is the event ontoTrunk writes beside the replay mark.
const eventReplayed = "replayed"

// approvalCovers ends every refusal here: what the person's answer was of.
const approvalCovers = "A person's approval covers the files they were shown; the messages and authors of the " +
	"story's commits are fixed when the commit gate passes, and a commit changed or added after that is approved again"

// notApprovedAsCommitted is what every such refusal says first.
func notApprovedAsCommitted(id string) string {
	return quote(id) + " has not been approved by a person as its commits now stand"
}

// written is one commit as gitx.Written says it: its hash and its words, the
// author's name, the author's email and the whole message. torn is an entry
// made of fields left over, which no commit is.
type written struct {
	hash, name, email, message string
	torn                       bool
}

// parseWritten takes apart what gitx.Written returned: four fields a commit,
// each ended by a NUL. Nothing is trimmed from a field: a blank line, a space
// at the end of a line and a final newline that is not there are each part of
// the message. Fields left over after the last four are not a commit, and are
// never read as a shorter list: they make one more entry, torn.
func parseWritten(out string) []written {
	if out == "" {
		return nil
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var list []written
	for ; len(fields) >= 4; fields = fields[4:] {
		list = append(list, written{hash: fields[0], name: fields[1], email: fields[2], message: fields[3]})
	}
	if len(fields) > 0 {
		list = append(list, written{torn: true})
	}
	return list
}

// sameWords reports whether now is was one for one: as many commits, and for
// each pair by position the same author name, author email and message. The
// three are compared apart and never joined, since "ab" with "c" and "a" with
// "bc" are one text once joined. The hash, the committer and the dates are not
// words: a replay changes every one of them. A list with a torn entry is the
// same as nothing, itself included.
func sameWords(was, now []written) bool {
	if len(was) != len(now) {
		return false
	}
	for i := range was {
		a, b := was[i], now[i]
		if a.torn || b.torn || a.name != b.name || a.email != b.email || a.message != b.message {
			return false
		}
	}
	return true
}

// fullHex reports whether s has the shape of a commit's full hash. Only such a
// value is put into a command a person is told to paste.
func fullHex(s string) bool {
	return (len(s) == 40 || len(s) == 64) && strings.Trim(s, "0123456789abcdef") == ""
}

// newestEvent is the message of the newest event of one type, by its place in
// the record and never by its time, and whether there is one. The type is
// compared whole: another event's message can hold anything.
func newestEvent(record *model.Record, kind string) (string, bool) {
	for _, e := range slices.Backward(record.Events) {
		if e.Type == kind {
			return e.Message, true
		}
	}
	return "", false
}

// recordedAtGate is the commit the commit gate last recorded, for the gate run
// again and for an answer: the record's own where the pass stands, and
// otherwise the one the newest commit_recorded event names, since a gate
// recorded as failed or reopened drops the record's.
//
// The stop does not ask this. There the recorded commit is Record.Commit and
// nothing else: a record that names none has no anchor and is judged as it was
// before (the person's fifth decision), whatever its history holds.
func recordedAtGate(record *model.Record) string {
	if record.Commit != "" {
		return record.Commit
	}
	last, _ := newestEvent(record, eventCommitRecorded)
	return last
}

// rememberCommit writes the commit the record names into its history, where
// the newest commit_recorded event does not name it already.
//
// migrating is the call made before a gate is recorded, for a record whose
// pass an older sdlc recorded: it has a commit and no such event, and `sdlc
// gate commit fail` would otherwise leave the next pass nothing to hold the
// story to. It writes only where the record has no such event at all, so that
// a commit an older sdlc recorded over a newer one's still shows as a commit
// the newest event does not name.
func rememberCommit(record *model.Record, at time.Time, migrating bool) {
	last, written := newestEvent(record, eventCommitRecorded)
	if record.Commit == "" || last == record.Commit || migrating && written {
		return
	}
	record.Append(eventCommitRecorded, record.Commit, at)
}

// approvedCommit is the commit the person's latest answer names, where that
// answer approved. An earlier answer's commit is never read: approved and then
// sent back is sent back.
func approvedCommit(record *model.Record) string {
	if d, ok := record.LatestDecision(); ok && d.Decision == model.DecisionApproved {
		return d.Commit
	}
	return ""
}

// answeredSince reports whether a person has answered since the commit gate
// last passed: the newest approval event comes later in the record than the
// newest commit pass. By place and not by time, since two timestamps in one
// second do not say which came first; and from the events, since the gate's
// standing result is gone once it is recorded as failed.
func answeredSince(record *model.Record) bool {
	pass := model.GateEvent(model.GateCommit, model.GatePass)
	for _, e := range slices.Backward(record.Events) {
		switch {
		case e.Type == model.EventApproval:
			return true
		case e.Type == model.EventGate && e.Message == pass:
			return false
		}
	}
	return false
}

// inHistory reports whether commit is tip or in its history, as the objects
// have it. A commit git cannot read, a value that is no commit at all and a
// read that failed are all a no: none of them is evidence that the story holds
// the commit, and a no is a refusal a person's answer clears.
func inHistory(ctx context.Context, root, commit, tip string) bool {
	ok, err := gitx.IsAncestor(ctx, root, commit, tip)
	return err == nil && ok
}

// loose says why a tip is not held to an anchor: the anchor is not in its
// history (gone), or commit, above the anchor, is not a bookkeeper's.
type loose struct {
	gone   bool
	commit string
	// paths is how many paths commit changes; notLine is set where the commits
	// above the anchor could not be read as a line, and notFile where commit
	// changes CODEMAP.md alone and leaves something other than a plain file.
	paths            int
	notLine, notFile bool
}

// heldTo says why tip is not held to anchor, or nil where it is: anchor is tip
// or in its history, the commits between them are a line, and each of them
// changes the root CODEMAP.md alone and leaves it a plain file, as the
// bookkeeper's commit does.
//
// Each commit is judged against its own parent, never the range's net change
// or the tip alone: a file added in one commit and taken away in the next is
// two commits nobody saw. The parent is the entry before it only because the
// range was first found to hold no merge, so that what git lists is a line.
// The name is compared whole and as written; codemapCheck reads the mode at
// the tip only, and a link there in one commit and a file again in the next
// would pass it. Any read that fails is a no.
func heldTo(ctx context.Context, root, anchor, tip string) *loose {
	if !inHistory(ctx, root, anchor, tip) {
		return &loose{gone: true}
	}
	if anchor == tip {
		return nil
	}
	var above []written
	merges, err := gitx.HasMerges(ctx, root, anchor, tip)
	if err == nil && !merges {
		var out string
		out, err = gitx.Written(ctx, root, anchor, tip)
		above = parseWritten(out)
	}
	if err != nil || merges || len(above) == 0 || above[len(above)-1].hash != tip {
		return &loose{commit: tip, notLine: true}
	}
	parent := anchor
	for _, c := range above {
		paths, err := gitx.ChangedBetween(ctx, root, parent, c.hash)
		if err != nil || len(paths) != 1 || paths[0] != codemap {
			return &loose{commit: c.hash, paths: len(paths)}
		}
		mode, err := gitx.ModeAt(ctx, root, c.hash, codemap)
		if err != nil || mode != "100644" && mode != "100755" {
			return &loose{commit: c.hash, paths: 1, notFile: true}
		}
		parent = c.hash
	}
	return nil
}

// requireSameCommits holds the commit gate run again, in a tier that waits, to
// the commits it recorded before: the person's third decision, that a second
// pass with other commits needs a person. It passes where nothing was recorded
// yet; where HEAD is held to the recorded commit; and where a person's latest
// answer approved, was given since the gate last passed, and names a commit
// HEAD holds. That last is "holds" and not "is held to", so that a rework
// round passes on its own approval, which names the tip before the rework. One
// answer covers one pass: once that pass is on the record the answer is older
// than it.
//
// It compares commits and never counts passes or failures: a gate recorded as
// failed and passed again with nothing amended needs nobody.
func requireSameCommits(ctx context.Context, rd *reading, id string, record *model.Record) error {
	recorded := recordedAtGate(record)
	if recorded == "" || !inItsWorktree(rd.s, id) {
		return nil
	}
	head, err := rd.headOf(ctx)
	if err != nil {
		return err
	}
	root := rd.s.Root()
	if heldTo(ctx, root, recorded, head) == nil ||
		answeredSince(record) && inHistory(ctx, root, approvedCommit(record), head) {
		return nil
	}
	return sdlcerr.New(sdlcerr.ApprovalRequired, notApprovedAsCommitted(id),
		"the commit gate last recorded the story's commits as the range ending at "+shortCommit(recorded)+
			", and HEAD, "+shortCommit(head)+", is not that commit with at most the bookkeeper's "+codemap+
			" commits on top of it: a commit of the story was changed or added since. In a tier that waits for a "+
			"person the story's commits are fixed when the commit gate passes, and passing it again with other "+
			"commits needs a person's answer given after that pass. "+gateNotPassed).
		WithFix(gateCommitsFix(rd.s, id))
}

// gateNotPassed ends a refusal of the commit gate that would otherwise read as
// advice about a pass that happened.
const gateNotPassed = "The commit gate has not passed again, and nothing was recorded"

// requireAnAnchorLeft refuses a commit pass, in a tier that waits, that would
// leave a story that was held to a commit held to none: the gate had recorded
// one (known), this pass placed no range, and no approving answer names a
// commit for the stop to hold the tip to instead. An unwatched commit beside a
// watched one is all it takes to make a pass record no range, and the stop
// judges a record with no anchor as it did before.
func requireAnAnchorLeft(s *store.Store, id string, record *model.Record, known string) error {
	if known == "" || record.Commit != "" || approvedCommit(record) != "" {
		return nil
	}
	_, pauses, err := s.CommitPause(id)
	if err != nil || !pauses {
		return err
	}
	return sdlcerr.New(sdlcerr.ApprovalRequired, quote(id)+" would no longer be held to its commits",
		"this pass would record no range for the story ("+noSingleRange+"), where the commit gate had recorded "+
			"the range ending at "+shortCommit(known)+". In a tier that waits for a person a story is held to the "+
			"commit its range ends at, and a pass that leaves none takes that hold off. "+gateNotPassed).
		WithFix(gateCommitsFix(s, id))
}

// readCommits is the command a person reads the story's commits with before
// they answer for them. It reads the commits themselves: a plain `git log`
// shows a replaced commit's words under the original's hash and an author as a
// .mailmap renames them, and the stop lands neither.
func readCommits(id, trunk string) string {
	return `"git --no-replace-objects log --no-mailmap --format=fuller ` + shellWord(trunk) + `..` +
		store.StoryBranch(id) + `"`
}

// handOverLead begins a fix that ends in a person's answer, in the state the
// refusal is printed in: with an iteration running the story is handed over;
// with none, `sdlc escalate` has no story to hand over and `sdlc start`
// refuses a finished one, so the commit gate is first recorded as failed.
func handOverLead(id string, running bool) string {
	const escalate = `"sdlc escalate pre_commit_approval --message ..."`
	if running {
		return `hand the story to a person with ` + escalate + ` and stop. `
	}
	return noIterationToHandOver(id) + `; where that gate refuses with this code, the story is handed to a person ` +
		`with ` + escalate + `. `
}

// noIterationToHandOver says how a finished story with no iteration running
// gets to a person: `sdlc escalate` hands over the story being worked on, and
// `sdlc start` refuses one whose every gate has passed.
func noIterationToHandOver(id string) string {
	return `no iteration is running on ` + id + `, so it cannot be handed over from here: a person or the session ` +
		`runs, in ` + worktreeIn(id) + `, "sdlc gate commit fail --story ` + id + ` --note 'the story changed after ` +
		`the commit gate; a person is to read it'" and types /sdlc:next, which takes the story up again at the commit gate`
}

// personReads is the middle of such a fix: what the person does. shown is the
// last commit the gate recorded, where there is one to show.
func personReads(id, trunk, shown string) string {
	also := ""
	if fullHex(shown) {
		also = ` ("git --no-replace-objects show --no-mailmap --no-patch --format=fuller ` + shortCommit(shown) +
			`" shows the last one the commit gate recorded, if git still has it)`
	}
	return `A person then, in their own terminal, in ` + worktreeIn(id) + `, reads the story's commits as they now ` +
		`are with ` + readCommits(id, trunk) + `, which shows each one's author and message` + also +
		`, and answers with "sdlc approve ` + id + `"`
}

// gateCommitsFix is the fix of a commit gate refused for the story's commits.
func gateCommitsFix(s *store.Store, id string) string {
	active, _ := s.Active()
	return handOverLead(id, active == id) + personReads(id, trunkName(s), "") +
		`; after that the session records the commit gate again, and the story is held to its commits as that ` +
		`answer found them`
}

// link is one replay sdlc stop made: the tip before, the trunk commit it put
// the story onto, and the tip it made. unread is an event that says there was
// one and does not read.
type link struct {
	model.Replay
	unread bool
}

// replays reads back every replayed event of the record, in order. An event is
// taken by its type alone, and its line is read by position against the id
// and the trunk it was written with -- the trunk's name sits in the middle of
// it -- and then written again and compared, so that this reader and
// replayedLine cannot drift apart unseen. One that does not read is kept as
// unread: the walk refuses on it and never steps over it.
func replays(record *model.Record, id, trunk string) []link {
	var links []link
	for _, e := range record.Events {
		if e.Type != eventReplayed {
			continue
		}
		var r model.Replay
		rest, _ := strings.CutPrefix(e.Message, "Replayed "+id+" from ")
		r.From, rest, _ = strings.Cut(rest, " onto "+trunk+" at ")
		r.Onto, r.To, _ = strings.Cut(rest, "; "+store.StoryBranch(id)+" is now ")
		links = append(links, link{Replay: r,
			unread: !fullHex(r.From) || !fullHex(r.Onto) || !fullHex(r.To) || replayedLine(id, trunk, r, true) != e.Message})
	}
	return links
}

// The ways a story's commits fail to stand.
const (
	// faultLoose: the tip is not held to the recorded commit or the answer's.
	faultLoose = iota + 1
	// faultNotKept: a replay of the stop's did not keep the commits' words.
	faultNotKept
	// faultUnread: a replay's commits, or its line on the record, do not read.
	faultUnread
	// faultNoAnchor: the gate's last pass left no range where one was known.
	faultNoAnchor
)

// fault is why a story's commits do not stand, for the refusal.
type fault struct {
	kind int
	// anchor is the commit the tip was held to and at is that tip; loose says
	// how it is not held. beforeReplay is set where at is a tip the stop
	// replaced, not the story's own.
	anchor, at   string
	loose        *loose
	beforeReplay bool
	// link is the replay at fault, and made and had the commits on each side.
	link      model.Replay
	made, had int
}

// standing is how a story's commits were judged: the rule that decided, and
// how many replays the stop's own chain held.
type standing struct {
	rule  string
	links int
}

// commitsStand judges a tip against the commits a person's approval holds the
// story to: recorded, the commit the gate recorded, and answered, the one the
// person's latest approving answer names; either may be empty. mark is the
// replay the tip is the end of, where the stop made it.
//
// The tip stands where it is held to either commit. Asked first and of both,
// before any replay is read: a person's answer on the tip clears a chain that
// no longer reads.
//
// Otherwise it stands only as the stop's own replay, by a chain. The mark is a
// link; the links before it are the record's replayed events. A link holds
// where the commits it made are, one for one, the ones it replaced, and the
// tip it replaced is itself held, or is the end of an earlier link that holds.
// An event is only ever a pointer to three commits: whether a link kept the
// words is read again from git each time, and the walk ends at a tip held to
// recorded or answered or it refuses. It moves strictly towards the start of
// the record, which is what ends it.
func commitsStand(ctx context.Context, root string, recorded, answered, tip string, mark *model.Replay, links []link) (standing, *fault) {
	rule, f := heldToEither(ctx, root, recorded, answered, tip)
	if f == nil || mark == nil {
		return standing{rule: rule}, f
	}
	cur, before := *mark, len(links)
	for n := 1; ; n++ {
		if f := kept(ctx, root, cur); f != nil {
			return standing{}, f
		}
		if _, f := heldToEither(ctx, root, recorded, answered, cur.From); f == nil {
			return standing{rule: "the stop's own replay", links: n}, nil
		}
		// The newest event before this one that made the tip this link
		// replaced. One that does not read ends the search: it may be the one.
		before--
		for before >= 0 && links[before].To != cur.From && !links[before].unread {
			before--
		}
		switch {
		case before < 0:
			return standing{}, &fault{kind: faultLoose, anchor: cmp.Or(recorded, answered), at: cur.From,
				loose: &loose{gone: true}, beforeReplay: true}
		case links[before].unread:
			return standing{}, &fault{kind: faultUnread, link: cur}
		}
		cur = links[before].Replay
	}
}

// heldToEither is the rule a tip is held by, to the recorded commit or to the
// answer's, or how it is not held to the first of them that is known.
func heldToEither(ctx context.Context, root, recorded, answered, tip string) (string, *fault) {
	var first *fault
	for _, hold := range []struct{ rule, anchor string }{
		{"held to the recorded commit", recorded}, {"held to the answer's commit", answered},
	} {
		if hold.anchor == "" {
			continue
		}
		l := heldTo(ctx, root, hold.anchor, tip)
		if l == nil {
			return hold.rule, nil
		}
		if first == nil {
			first = &fault{kind: faultLoose, anchor: hold.anchor, at: tip, loose: l}
		}
	}
	if first == nil {
		// Neither is known: the callers ask only where one is, and a tip
		// held to nothing is never one that stands.
		first = &fault{kind: faultLoose, at: tip, loose: &loose{gone: true}}
	}
	return "", first
}

// kept says how a replay did not keep its commits, or nil where it did: what
// it made on the trunk commit is, one for one, what the story had above where
// it left trunk's history, by author name, author email and whole message.
//
// Both lists are read by the link's own hashes, never HEAD or the reflog. The
// ancestry is read first: gitx.Written lists whatever git can reach, and a
// range read without its ancestry known is the first read that a hidden
// history shortens. A link either of whose lists is empty kept nothing: two
// empty lists are the same under sameWords, and a line on the record naming
// three commits with nothing between them would otherwise hold.
func kept(ctx context.Context, root string, l model.Replay) *fault {
	onTrunk, err := gitx.IsAncestor(ctx, root, l.Onto, l.To)
	var fork, was, now string
	if err == nil {
		fork, err = gitx.MergeBase(ctx, root, l.Onto, l.From)
	}
	if err == nil && fork != "" {
		was, err = gitx.Written(ctx, root, fork, l.From)
	}
	if err == nil && fork != "" {
		now, err = gitx.Written(ctx, root, l.Onto, l.To)
	}
	if err != nil {
		return &fault{kind: faultUnread, link: l}
	}
	had, made := parseWritten(was), parseWritten(now)
	if !onTrunk || len(had) == 0 || len(made) == 0 || !sameWords(had, made) {
		return &fault{kind: faultNotKept, link: l, had: len(had), made: len(made)}
	}
	return nil
}

// requireCommitsApproved is L8's other half: in a tier that waits for a
// person, a story is landed only on the commits its approval holds it to.
//
// It is asked in every check -- before a replay, after it, and after the
// commands -- so a tip tampered with before the stop puts the story onto trunk
// is refused before anything is replayed, and never laundered by the replay.
//
// The recorded commit here is Record.Commit and nothing else (recordedAtGate
// says why). A record with none, and no commit on the person's latest
// approving answer, has no anchor and is judged as it was before, with one
// exception: a record that says its last pass placed no range and whose
// history holds a commit an earlier pass recorded once had an anchor, and the
// answer that stood in for it is gone.
func (l *lander) requireCommitsApproved(ctx context.Context, st *landState) error {
	_, pauses, err := l.pausedTier(*st)
	if err != nil || !pauses {
		return err
	}
	record := st.record
	recorded, answered := record.Commit, approvedCommit(record)
	if recorded == "" && answered == "" {
		if once, _ := newestEvent(record, eventCommitRecorded); record.RangeUnknown && once != "" {
			return l.commitsRefusal(recorded, &fault{kind: faultNoAnchor, anchor: once})
		}
		slog.Debug("landing judged the story's commits", "story", l.id, "rule", "no anchor", "tip", shortCommit(st.tip))
		return nil
	}
	var mark *model.Replay
	if ownReplay(*st) {
		mark = record.Replay
	}
	stands, f := commitsStand(ctx, l.s.Root(), recorded, answered, st.tip, mark, replays(record, l.id, l.m.trunk))
	if f != nil {
		slog.Debug("landing judged the story's commits", "story", l.id, "rule", "refused", "tip", shortCommit(st.tip),
			"recorded", shortCommit(recorded), "answered", shortCommit(answered))
		return l.commitsRefusal(recorded, f)
	}
	slog.Debug("landing judged the story's commits", "story", l.id, "rule", stands.rule, "links", stands.links,
		"tip", shortCommit(st.tip), "recorded", shortCommit(recorded), "answered", shortCommit(answered))
	return nil
}

// commitsRefusal is SDLC-E0037 for a story whose commits do not stand. It
// prints hashes, counts, the story's id and the trunk's name, and nothing a
// session chose: no message, no author and no path. A message is read back by
// an assistant inside sdlc's own refusal, and after a squash or a split there
// is no pairing of old and new commits to quote from honestly.
func (l *lander) commitsRefusal(recorded string, f *fault) error {
	trunk, branch, in := l.m.trunk, store.StoryBranch(l.id), worktreeIn(l.id)
	var why string
	reset := false
	switch f.kind {
	case faultNoAnchor:
		why = "the commit gate's last pass recorded no range for the story, where an earlier pass had recorded the " +
			"range ending at " + shortCommit(f.anchor) + ", and no answer of a person's that approved it names a " +
			"commit, so nothing says which commits the story is held to"
	case faultNotKept:
		why = fmt.Sprintf("sdlc stop made %s's commits again on top of %s at %s, ending at %s, and the %d it made are "+
			"not, one for one, the %d the story had before, which ended at %s: a commit is missing or added, or one "+
			"has another author or message. %s has not moved. The stop compares each commit's author name, author "+
			"email and whole message with the one it replaces; git made them with its rebase, under this "+
			"repository's configuration and with its hooks off, so check "+
			`"git config --show-origin --get-regexp '^(commit\.cleanup|i18n\.)'" in %s, and whether anything `+
			"committed on %s while the stop ran",
			branch, trunk, shortCommit(f.link.Onto), shortCommit(f.link.To), f.made, f.had, shortCommit(f.link.From),
			trunk, in, branch)
	case faultUnread:
		why = "sdlc stop put " + branch + " onto " + trunk + " before, and what it replaced then no longer reads: the " +
			"commits that ended at " + shortCommit(f.link.From) + `, or the "replayed" line of the story's record ` +
			`that names the replay before that one (git removes commits no branch holds, after a while or on "git gc ` +
			`--prune"). So the commits the story has now cannot be compared with the ones the commit gate recorded. ` +
			"Nothing says they were changed, and nothing says they were not"
	default:
		why, reset = looseWhy(branch, trunk, recorded, f), recorded != "" && !f.beforeReplay
	}
	fix := handOverLead(l.id, l.running) + personReads(l.id, trunk, recorded) +
		`; after that, "sdlc stop" there, run by the person or by the session, lands the story`
	if reset && fullHex(recorded) {
		rec := shortCommit(recorded)
		fix = `where the commits were changed by mistake and git still has ` + rec + `, a person puts the story back ` +
			`on it, in ` + in + `, with "git reset --hard ` + rec + `" (this drops every commit above ` + rec +
			` and any uncommitted change; the bookkeeper's ` + codemap + ` commit is then made again), and "sdlc ` +
			`stop" there lands it. Otherwise ` + fix
	}
	return sdlcerr.New(sdlcerr.ApprovalRequired, notApprovedAsCommitted(l.id), why+". "+approvalCovers).WithFix(fix)
}

// looseWhy says how a tip is not held to its anchor: which commit that is,
// where the tip is, and what is in the way.
func looseWhy(branch, trunk, recorded string, f *fault) string {
	anchored := "the commit gate recorded the story's commits as the range ending at " + shortCommit(f.anchor)
	named := "the end of the range the commit gate recorded"
	if recorded == "" {
		// Held to the answer's commit alone: the gate's last pass placed no
		// range, and there is no recorded commit to print.
		anchored = "the commit gate recorded no range for the story, so it is held to " + shortCommit(f.anchor) +
			", where a person's latest answer approved its commits"
		named = "where a person's latest answer approved the story's commits"
	}
	if f.loose.gone {
		where := "where " + branch + " is now"
		if f.beforeReplay {
			where = "where " + branch + " was before sdlc stop put it onto " + trunk
		}
		return anchored + ", and that commit is not in the history of " + shortCommit(f.at) + ", " + where +
			": a commit of the story was made again after the gate passed (amended, reworded, squashed, split, " +
			"reordered or rebased, by a session or by a person)"
	}
	does := fmt.Sprintf("changes %d paths, and not the root %s alone", f.loose.paths, codemap)
	switch {
	case f.loose.notLine:
		does = "is a merge or stands beside one, or the commits under it do not read as a line"
	case f.loose.notFile:
		does = "leaves the root " + codemap + " as something other than a plain file"
	case f.loose.paths == 0:
		does = "changes no path"
	case f.loose.paths == 1:
		does = "changes one path that is not the root " + codemap
	}
	return shortCommit(f.loose.commit) + ", which is above " + shortCommit(f.anchor) + ", " + named + ", " + does +
		"; the only commit a story gains after the gate without a person is the bookkeeper's, which changes the " +
		"root " + codemap + " alone and leaves it a plain file. This is also what a person's own commit made on " +
		"the story after the gate is"
}

// answerForCommits is the commit a person's answer names, and what an
// approving answer in a tier that waits is told about it. It is asked before
// the answer goes on the record, so that whether the story's commits stood is
// said of the record as the person found it, and never because of the commit
// this answer itself is about to name.
//
// An answer names a commit once the commit gate has passed on the story in its
// own worktree, whatever the gates' state now: keyed on the pass's event, since
// the record's own commit is gone after `sdlc gate commit fail`. Before the
// first pass there is no commit of the story to name. The commit is the tip of
// the story's branch, read from the ref, and only where the worktree is on
// that branch: with HEAD detached or on another branch, HEAD is not what `sdlc
// stop` lands, and an answer naming it would hold the story to commits nobody
// was shown.
func answerForCommits(ctx context.Context, s *store.Store, id string, record *model.Record) (commit, say string, err error) {
	if !inItsWorktree(s, id) || record.Count(model.EventGate, model.GateEvent(model.GateCommit, model.GatePass)) == 0 {
		return "", "", nil
	}
	root, branch, trunk := s.Root(), store.StoryBranch(id), trunkName(s)
	on, err := gitx.Branch(ctx, root)
	if err != nil {
		return "", "", err
	}
	// Nothing is said in a tier that does not wait: nothing holds the commits
	// there, and the lines would send a person to undo commits nobody is held to.
	_, pauses, err := s.CommitPause(id)
	if err != nil {
		return "", "", err
	}
	if on != branch {
		say = "This answer names no commit of the story, because " + worktreeIn(id) + " is not on " + branch +
			": it is for the files only, and clears no refusal about the story's commits."
		return "", pausesOnly(pauses, say), nil
	}
	tip, err := gitx.BranchTip(ctx, root, branch)
	if err != nil {
		return "", "", err
	}
	say = "This answer is also for the story's commits as they stand: " + branch + " is at " + shortCommit(tip) + "."
	recorded, answered := recordedAtGate(record), approvedCommit(record)
	if !pauses || recorded == "" && answered == "" {
		return tip, pausesOnly(pauses, say), nil
	}
	links := replays(record, id, trunk)
	var mark *model.Replay
	for i := range links {
		if links[i].To == tip && !links[i].unread {
			mark = &links[i].Replay
		}
	}
	if _, f := commitsStand(ctx, root, recorded, answered, tip, mark, links); f == nil {
		return tip, say, nil
	}
	say += " They are not the commits the story was held to: one was changed or added since the commit gate passed. In " +
		worktreeIn(id) + ", " + readCommits(id, trunk) + " shows each one's author and message."
	if fullHex(recorded) {
		rec := shortCommit(recorded)
		say += " The commit gate's range ended at " + rec + `. If that is not what you meant to approve, and git still ` +
			`has ` + rec + `, "git reset --hard ` + rec + `" there puts the story back on it before anyone runs "sdlc ` +
			`stop" (this drops every commit above ` + rec + ` and any uncommitted change).`
	}
	return tip, say, nil
}

// pausesOnly is say in a tier that waits for a person, and nothing elsewhere.
func pausesOnly(pauses bool, say string) string {
	if pauses {
		return say
	}
	return ""
}
