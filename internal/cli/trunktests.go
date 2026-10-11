package cli

import (
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
type freezeAt struct{ cut, on, tip string }

// heldState is the freeze as it stands for a story: the frozen files that are
// not what was frozen, as verifyFreeze names them; the test files the freeze
// does not hold; trunk's test files the story's commits changed or took away;
// and what was accepted as trunk's own, nil where nothing was.
type heldState struct {
	broken, unheld, taken []string
	accepted              *model.TrunkTests
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
	on, err := gitx.MergeBase(ctx, s.Root(), tip, head)
	if err != nil {
		return ""
	}
	return on
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
func heldTests(ctx context.Context, s *store.Store, id string, lock *model.Lock, at freezeAt) (heldState, error) {
	differing := differingFrozen(s, lock)
	unheld, err := unfrozenTests(ctx, s, lock)
	h := heldState{broken: namesOf(differing), unheld: unheld}
	if err != nil || at.cut == "" || !inItsWorktree(s, id) {
		return h, err
	}
	at, judged, err := settleFreezeAt(ctx, s, at)
	if err != nil || !judged {
		return h, err
	}
	root := s.Root()
	held, err := gitx.IsAncestor(ctx, root, at.cut, at.on)
	trunkChanged, terr := gitx.ChangedBetween(ctx, root, at.cut, at.on)
	storyChanged, serr := gitx.ChangedBetween(ctx, root, at.on, at.tip)
	if err := errors.Join(err, terr, serr); err != nil || !held {
		// What cannot be read accepts nothing, which is the refusing side.
		slog.Debug("no test file is judged as trunk's own", "story", id, "cut", at.cut, "on", at.on, "err", err)
		return h, nil
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
	return h, nil
}

// settleFreezeAt fills in the commits a judgement is made between: the story's
// tip and the commit it sits on. judged is false where there is nothing between
// them to judge.
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
	if at.on == "" || at.on == at.cut || at.on == at.tip {
		return at, false, nil
	}
	return at, true, nil
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

func (f frozenFile) String() string {
	if f.gone {
		return f.path + " (gone)"
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
