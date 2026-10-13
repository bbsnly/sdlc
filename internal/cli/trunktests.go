package cli

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/testset"
)

// Test files that are trunk's own. A story in its own worktree comes to sit on
// a later trunk commit than the one it was cut from each time another story
// lands, and the test files that landing brought are in its worktree without
// being its own. Held against it, every story but the first to land was
// refused with SDLC-E0043 or SDLC-E0025 and needed a person to unfreeze.

// freezeAt is where a story's test files are judged: the commit its branch was
// cut from, the trunk commit it sits on, and the commit judged. on and tip are
// read here where they are not given.
type freezeAt struct {
	cut, on, tip string
	// forks is the story's list of its own commits, nil where its record has
	// none that reads.
	forks []model.Fork
	// ahead is the trunk commit a stop is about to put the story onto, where
	// trunk has moved on since the story left it.
	ahead string
	// sat is the trunk commit the story sits on, which on is too until the
	// list lowers it, and own the entry that lowered it. Both are worked out
	// here.
	sat string
	own model.Fork
}

// heldState is the freeze as it stands for a story: the frozen files that are
// not what was frozen, as verifyFreeze names them; the test files the freeze
// does not hold; trunk's test files the story's commits changed or took away;
// and what was accepted as trunk's own, nil where nothing was.
type heldState struct {
	broken, unheld, taken []string
	accepted              *model.TrunkTests
	// list is what the story's list of its own commits has to do with what is
	// refused.
	list listed
}

// listed is set where it is the story's list of its own commits, and not the
// tree alone, that a test file is refused by: the record has no list, so no
// test file is taken as trunk's, or the list names a commit trunk holds, so
// none from that commit up is. It sorts the files a refusal names by what a
// person does about each.
type listed struct {
	// none is a record with no list, and noted the commit a list names that
	// trunk holds.
	none  bool
	noted string
	// own are the files named that trunk does not hold as the worktree has
	// them, and same the ones it does. coming are trunk's, changed between the
	// commit the story sits on and ahead, the trunk commit a stop was about to
	// put it onto: they are not in the worktree yet, and are refused before
	// the stop rewrites the branch and not after.
	own, same, coming []string
	ahead             string
}

// is reports whether the list is why.
func (l listed) is() bool { return l.none || l.noted != "" }

// here is the names among those given that are in the worktree.
func (l listed) here(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return slices.Contains(l.coming, name) })
}

// explain adds to a refusal of the freeze what the list has to do with it, and
// gives it the fix that goes with that. route is how a person has the tests
// frozen again from where the refusal is given.
func (l listed) explain(s *store.Store, id string, e *sdlcerr.Error, route string) *sdlcerr.Error {
	if !l.is() {
		return e
	}
	trunk := trunkName(s)
	why := []string{e.Why}
	if len(l.coming) > 0 {
		why = append(why, quoteAll(l.coming)+" is "+trunk+"'s at "+l.ahead[:12]+" and not in this worktree yet: the stop would "+
			"bring it here, and it could not then be taken as "+trunk+"'s")
	}
	if l.none {
		why = append(why, `this story's record has no list of its own commits ("forks"): the record predates the list, or an `+
			`older sdlc saved it without one. sdlc cannot tell `+trunk+`'s test files from the story's then, so it takes none as `+
			trunk+`'s`)
	} else {
		why = append(why, trunk+` holds `+l.noted[:min(len(l.noted), 12)]+`, which sdlc noted as one of this story's own commits `+
			`("forks" in its record) at an earlier gate, freeze or stop, so it takes no test file from that commit up as `+trunk+
			`'s. If `+trunk+` came to hold that commit some other way than from this story, the files are `+trunk+
			`'s, and sdlc cannot tell`)
	}
	out := *e
	out.Why = strings.Join(slices.DeleteFunc(why, func(part string) bool { return part == "" }), "; ")
	return out.WithFix(l.fix(s, id, route))
}

// fix is what a person does about the files a refusal names where the list is
// why: each kind of file under the sentence that is true of it, the kinds
// there are none of left out, and then the acts in the one order that works.
func (l listed) fix(s *store.Store, id, route string) string {
	trunk, branch := trunkName(s), store.StoryBranch(id)
	var groups, acts []string
	if len(l.own) > 0 {
		groups = append(groups, quoteAll(l.own)+" is not here as "+trunk+" has it, so it is the story's own")
	}
	if len(l.same) > 0 {
		groups = append(groups, quoteAll(l.same)+" is here exactly as "+trunk+" has it, and sdlc cannot tell whether "+trunk+
			" has it from this story")
	}
	if len(l.coming) > 0 {
		groups = append(groups, quoteAll(l.coming)+" is "+trunk+"'s and not in this worktree yet")
	}
	if len(l.own)+len(l.same) > 0 {
		acts = append(acts, "a person takes each file that is the story's own out of "+branch+", or restores it as it was frozen")
	}
	if len(l.coming) > 0 {
		acts = append(acts, "a person puts "+branch+" onto "+trunk+" in their own terminal, so that the files are there to "+
			"freeze: cd -- "+shellPath(s.Root())+"; git rebase "+shellWord(trunk))
	}
	if len(l.same)+len(l.coming) > 0 {
		acts = append(acts, "a person who has checked that each file left is "+trunk+"'s, and is to stay, has it frozen with "+
			"the rest: "+route+". That is needed once, because a freeze taken after an unfreeze starts the story's list again")
	}
	return strings.Join(groups, ". ") + ". In this order: " + strings.Join(acts, "; then ") + "; " + assistantEscalates
}

// sittingOn is the trunk commit the story's commits sit on in this checkout:
// where head meets trunk, as the main checkout's configuration names trunk. It
// is "" where that cannot be told, and nothing is accepted as trunk's then.
//
// It is never the cut wherever head still holds it: after a rebase onto a trunk
// that only moved on, head holds the cut and sits on a later commit.
func sittingOn(ctx context.Context, s *store.Store, head string) string {
	m, err := mainSettingsOf(s)
	if err != nil {
		return ""
	}
	tip, err := gitx.BranchTip(ctx, s.Root(), m.trunk)
	if err != nil {
		return ""
	}
	return meetsTrunk(ctx, s.Root(), tip, head)
}

// meetsTrunk is where head meets the trunk tip: "" where git cannot tell. Every
// question about the trunk commit a story sits on starts here: sittingOn, and
// laterTrunkCommit for the commit gate.
func meetsTrunk(ctx context.Context, root, tip, head string) string {
	on, err := gitx.MergeBase(ctx, root, tip, head)
	if err != nil {
		return ""
	}
	return on
}

// sitsPastCut reports whether on is a trunk commit the story's commits sit on,
// and not the commit the branch was cut from or the story's own tip (tip is
// the story's, not trunk's): the one answer to "was the story put onto a later
// commit" that the freeze check, committedSince and the commit gate all give.
// The freeze check asks it of the commit the story's list of its own commits
// leaves it with (settleFreezeAt), which is lower than the one the other two
// ask it of wherever trunk holds a commit the list names.
func sitsPastCut(on, cut, tip string) bool {
	return on != "" && on != cut && on != tip
}

// heldTests judges the story's test files against its freeze.
//
// Outside the story's own worktree, and for a story that still sits on the
// commit it was cut from, it is exactly what it was: every frozen file as
// frozen, and no test file the freeze does not hold. That also holds where
// the cut is not known or is not in the history of the commit the story sits
// on, and where trunk already holds the commit judged: with no story commit
// of its own to tell from trunk's, a test the story added after its freeze
// would read as trunk's.
//
// Otherwise a path is trunk's own only on evidence that trunk changed it and
// the story did not: it differs between the cut and the commit the story sits
// on, it does not differ between that commit and the story's tip, and the file
// on disk is, byte for byte, the file trunk commits there. A frozen file needs
// one thing more: the freeze holds it as the cut commits it, so the story never
// changed it. Where the story and trunk both changed a test, it stays the
// refusal it was, and a person's.
//
// The commit the story sits on is not taken as trunk's on git's word alone:
// the story's list of its own commits says how far down trunk's part goes
// (settleFreezeAt), and a record with no list has nothing judged as trunk's.
func heldTests(ctx context.Context, s *store.Store, id string, lock *model.Lock, at freezeAt) (heldState, error) {
	differing := differingFrozen(s, lock)
	unheld, err := unfrozenTests(ctx, s, lock)
	h := heldState{broken: namesOf(differing), unheld: unheld}
	if err != nil || at.cut == "" || !inItsWorktree(s, id) {
		return h, err
	}
	at, judged, err := settleFreezeAt(ctx, s, at)
	if judged {
		h.takeTrunks(ctx, s, id, lock, at, differing)
	}
	if err == nil {
		err = h.readList(ctx, s, lock, at)
	}
	return h, err
}

// takeTrunks takes out of what is refused the test files that are trunk's own,
// between the commits at names.
func (h *heldState) takeTrunks(ctx context.Context, s *store.Store, id string, lock *model.Lock, at freezeAt, differing []frozenFile) {
	root := s.Root()
	held, err := gitx.IsAncestor(ctx, root, at.cut, at.on)
	trunkChanged, terr := gitx.ChangedBetween(ctx, root, at.cut, at.on)
	storyChanged, serr := gitx.ChangedBetween(ctx, root, at.on, at.tip)
	if err := errors.Join(err, terr, serr); err != nil || !held {
		// What cannot be read accepts nothing, which is the refusing side.
		slog.Debug("no test file is judged as trunk's own", "story", id, "cut", at.cut, "on", at.on, "err", err)
		return
	}
	c := trunkChange{
		judge:        judge{s: s, lock: lock, at: at},
		trunkChanged: trunkChanged, storyChanged: storyChanged,
	}
	accepted := &model.TrunkTests{Commit: at.on}
	h.unheld = slices.DeleteFunc(h.unheld, func(path string) bool { return c.acceptUnheld(ctx, accepted, path) })
	h.broken = c.acceptDiffering(ctx, accepted, differing)
	// A test file of trunk's that the story's commits took away is on neither
	// list above: it is not on disk, and the freeze never held it.
	for _, path := range testset.New(s.Config().Paths.Tests).Filter(storyChanged) {
		if slices.Contains(trunkChanged, path) && !lock.Holds(path) && !slices.Contains(h.unheld, path) {
			h.taken = append(h.taken, path)
		}
	}
	if len(accepted.Files)+len(accepted.Gone) > 0 {
		slices.Sort(accepted.Files)
		h.accepted = accepted
	}
}

// readList works out what the story's list of its own commits has to do with
// what is refused, once the tree has been judged: nothing, unless the record
// has no list where one would have been used, or the list lowered the commit
// trunk's part ends at. Where it has, the files refused are sorted by whether
// trunk holds each as the worktree has it.
func (h *heldState) readList(ctx context.Context, s *store.Store, lock *model.Lock, at freezeAt) error {
	switch {
	case len(at.forks) == 0 && (at.ahead != "" || sitsPastCut(at.sat, at.cut, at.tip)):
		h.list.none = true
	case at.own.From != "":
		h.list.noted = at.own.First
	default:
		return nil
	}
	sat := judge{s: s, lock: lock, at: freezeAt{on: at.sat}}
	for _, name := range slices.Concat(h.unheld, h.taken, h.broken) {
		path, gone := strings.CutSuffix(name, goneSuffix)
		if gone && sat.goneOnTrunk(ctx, path) || !gone && sat.asTrunkHasIt(ctx, path) {
			h.list.same = append(h.list.same, name)
		} else {
			h.list.own = append(h.list.own, name)
		}
	}
	if at.ahead == "" {
		return nil
	}
	return h.refuseComing(ctx, s, lock, at)
}

// refuseComing refuses, where the list is why and a stop is about to put the
// story onto a later trunk commit, the test files trunk changed up to that
// commit. The stop would bring them into the worktree, nothing would take them
// as trunk's there, and the refusal would come after the branch had been
// rewritten.
func (h *heldState) refuseComing(ctx context.Context, s *store.Store, lock *model.Lock, at freezeAt) error {
	h.list.ahead = at.ahead
	changed, err := gitx.ChangedBetween(ctx, s.Root(), at.sat, at.ahead)
	for _, path := range testset.New(s.Config().Paths.Tests).Filter(changed) {
		refused := &h.unheld
		if lock.Holds(path) {
			refused = &h.broken
		}
		if !slices.Contains(*refused, path) && !slices.Contains(*refused, path+goneSuffix) {
			*refused = append(*refused, path)
			h.list.coming = append(h.list.coming, path)
		}
	}
	return err
}

// settleFreezeAt fills in the commits a judgement is made between: the story's
// tip and the commit it sits on, lowered to where the story's list of its own
// commits says trunk's part ends. Trunk's branch pointed at a commit of the
// story's own makes that commit the one the story sits on, and everything
// under it then read as trunk's; the entry noted for it before says it was the
// story's, and the judgement is made from the trunk commit that entry was
// noted on. judged is false where there is nothing between the commits to
// judge, and for a record with no list, which says of no commit whose it is.
func settleFreezeAt(ctx context.Context, s *store.Store, at freezeAt) (freezeAt, bool, error) {
	var err error
	if at.tip == "" {
		if at.tip, err = gitx.Head(ctx, s.Root()); err != nil {
			return at, false, err
		}
	}
	if at.on == "" {
		at.on = sittingOn(ctx, s, at.tip)
	}
	at.sat = at.on
	if at.on != "" && len(at.forks) > 0 {
		// Asked of the trunk commit a stop is about to put the story onto,
		// where there is one: an entry whose commit only that one holds lowers
		// nothing until the story is on it, and the refusal then came after
		// the stop had rewritten the branch. What cannot be read accepts
		// nothing: a list git could not be asked about is an error, and never
		// a list that names nothing.
		at.own, err = trunkPart(ctx, s.Root(), at.forks, cmp.Or(at.ahead, at.on))
	}
	if at.own.From != "" {
		at.on = at.own.From
	}
	return at, err == nil && len(at.forks) > 0 && sitsPastCut(at.on, at.cut, at.tip), err
}

// trunkChange is what trunk and the story each changed since the cut, with the
// judge that reads the files.
type trunkChange struct {
	judge
	trunkChanged, storyChanged []string
}

// onlyTrunk reports whether trunk changed the path and the story did not.
func (c trunkChange) onlyTrunk(path string) bool {
	return slices.Contains(c.trunkChanged, path) && !slices.Contains(c.storyChanged, path)
}

// acceptUnheld takes a test file the freeze does not hold as trunk's, if it is.
func (c trunkChange) acceptUnheld(ctx context.Context, accepted *model.TrunkTests, path string) bool {
	if !c.onlyTrunk(path) || !c.asTrunkHasIt(ctx, path) {
		return false
	}
	accepted.Files = append(accepted.Files, path)
	return true
}

// acceptDiffering takes the frozen files that differ as trunk's where it is
// trunk that changed them, and returns the ones that stay the refusal.
func (c trunkChange) acceptDiffering(ctx context.Context, accepted *model.TrunkTests, differing []frozenFile) []string {
	var broken []string
	for _, f := range differing {
		switch {
		case !c.onlyTrunk(f.path) || !c.frozenAsCut(ctx, f.path):
			broken = append(broken, f.String())
		case f.gone && c.goneOnTrunk(ctx, f.path):
			accepted.Gone = append(accepted.Gone, f.path)
		case !f.gone && c.asTrunkHasIt(ctx, f.path):
			accepted.Files = append(accepted.Files, f.path)
		default:
			broken = append(broken, f.String())
		}
	}
	return broken
}

// frozenFile is a frozen file that is not what was frozen, and whether it is
// gone.
type frozenFile struct {
	path string
	gone bool
}

// goneSuffix is what the name of a frozen file that is gone ends with.
const goneSuffix = " (gone)"

func (f frozenFile) String() string {
	if f.gone {
		return f.path + goneSuffix
	}
	return f.path
}

func namesOf(files []frozenFile) []string {
	var names []string
	for _, f := range files {
		names = append(names, f.String())
	}
	return names
}

// judge answers for one path at a time what git's objects and the disk say.
// Every answer that cannot be read is a no.
type judge struct {
	s    *store.Store
	lock *model.Lock
	at   freezeAt
}

// blobSum is the hash the freeze would keep for path as commit has it.
func (j judge) blobSum(ctx context.Context, commit, path string) (string, bool) {
	raw, err := gitx.FileAt(ctx, j.s.Root(), commit, path)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), true
}

// frozenAsCut reports whether the freeze holds path exactly as the cut commits
// it: the story's own test author never changed it.
func (j judge) frozenAsCut(ctx context.Context, path string) bool {
	sum, ok := j.blobSum(ctx, j.at.cut, path)
	return ok && sum == j.lock.Files[path]
}

// goneOnTrunk reports whether the trunk commit the story sits on has nothing
// at path.
func (j judge) goneOnTrunk(ctx context.Context, path string) bool {
	mode, err := gitx.ModeAt(ctx, j.s.Root(), j.at.on, path)
	return err == nil && mode == ""
}

// asTrunkHasIt reports whether the file on disk at path is the file the trunk
// commit the story sits on has there: a plain file of the same mode with the
// same bytes. The bytes are read here and hashed, as the freeze hashes them.
// Asked of git status, an edit made under an index flag, or behind a filter
// the configuration names, read as no change at all; and a checkout that
// converts line endings is not byte for byte what trunk commits, so there the
// answer is no.
func (j judge) asTrunkHasIt(ctx context.Context, path string) bool {
	mode, err := gitx.ModeAt(ctx, j.s.Root(), j.at.on, path)
	if err != nil || mode != "100644" && mode != "100755" {
		return false
	}
	info, err := os.Lstat(filepath.Join(j.s.Root(), filepath.FromSlash(path)))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	// Windows keeps no executable bit on disk to compare.
	if runtime.GOOS != goosWindows && (info.Mode().Perm()&0o111 != 0) != (mode == "100755") {
		return false
	}
	want, ok := j.blobSum(ctx, j.at.on, path)
	got, err := j.s.HashFile(path)
	return ok && err == nil && got == want
}

// acceptedLine says which test files were accepted as trunk's own, and the
// trunk commit they were compared with. Each list is cut short at ten names;
// the record's event names them all.
func acceptedLine(id, trunk string, t *model.TrunkTests, all bool) string {
	list := quoteAll
	if !all {
		list = func(names []string) string {
			const shown = 10
			if len(names) <= shown {
				return quoteAll(names)
			}
			return fmt.Sprintf("%s, and %d more", quoteAll(names[:shown]), len(names)-shown)
		}
	}
	line := fmt.Sprintf("%s accepted as %s's, not held against %s (compared with %s at %s)",
		countFiles(len(t.Files)+len(t.Gone)), trunk, id, trunk, t.Commit[:12])
	if len(t.Files) > 0 {
		line += ": " + list(t.Files)
	}
	if len(t.Gone) > 0 {
		line += "; removed on " + trunk + ": " + list(t.Gone)
	}
	return line
}

// recordAccepted puts what was accepted as trunk's own on the record, with an
// event where it is not what the record already says, and returns it. Where
// nothing was accepted the record says nothing of any.
func recordAccepted(s *store.Store, id string, record *model.Record, t *model.TrunkTests) *model.TrunkTests {
	was := record.TrunkTests
	record.TrunkTests = t
	if t != nil && (was == nil || was.Commit != t.Commit || !slices.Equal(was.Files, t.Files) || !slices.Equal(was.Gone, t.Gone)) {
		record.Append("trunk_tests", acceptedLine(id, trunkName(s), t, true), s.Now())
	}
	return t
}

// takenWhy says of trunk's test files the story changed or took away what
// they are.
func takenWhy(trunk string, taken []string) string {
	return strings.Join(taken, ", ") + " is " + trunk + "'s, changed on " + trunk + " since the story was cut, and the " +
		"story's commits change it or take it away"
}

// takenFix is how a person puts one of trunk's test files back as trunk has
// it. Each command is a clause of its own, so that it pastes into any shell.
func takenFix(s *store.Store, id, path string) string {
	trunk := shellWord(trunkName(s))
	return `a person puts it back as ` + trunk + ` has it, in their own terminal: cd -- ` + shellPath(s.Root()) +
		`; then git checkout ` + trunk + ` -- ` + shellWord(path) + `, or "git rm" it where ` + trunk +
		` has none; then commit it on ` + store.StoryBranch(id)
}

// shellPath is a path on disk as a person pastes it after "cd --": always in
// single quotes, so that the fix reads the same whatever the path holds.
func shellPath(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
