package gitx

// US-099 in gitx: the lines a story's commits add to a file hold the markers
// of an unresolved conflict. This is the git half of the check -- reading the
// patch between two commits and finding, per file and in order, a line that
// begins with seven "<" and a space, a later line that is exactly seven "=",
// and a later line that begins with seven ">" and a space, with a line that
// begins with seven "|" and a space allowed between the first two. What a
// caller does with a finding (refusing, the exempt paths, the wording) is
// internal/cli's and is held there.
//
// The function and type these tests hold gitx to:
//
//	type MarkerSet struct {
//		Path                           string // slash-separated, repository-relative, the new name, unquoted
//		Open, Base, Separator, Close int    // new-file line numbers, 1-based; Base is 0 where there is no "|||||||" line
//	}
//	ConflictMarkers(ctx context.Context, root, base, tip string) ([]MarkerSet, error)
//
// base and tip are full commit hashes, as for Fingerprint. The result is every
// set that the net change from base to tip adds, one MarkerSet each, whatever
// order they come in; a change that adds none gives no sets and no error.
//
// The decisions of 2026-10-24 that these hold: one trailing carriage return on
// a marker line is tolerated (1); the set is ordered across the hunks of one
// file with no limit on the distance (2); a file is read with rename
// detection, so moving a file that already holds a set adds none (4); every
// file is read as text whatever its attributes say, and CODEMAP.md is read
// like any other (7); only seven characters make a marker (9).
//
// No marker line is written out in this file: they are built at run time, so
// that this story's own commits do not hold a set for its own check to find.
//
// Helpers carry a cmk prefix, for conflict markers.

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func cmkOpen(label string) string  { return strings.Repeat("<", 7) + " " + label }
func cmkBase(label string) string  { return strings.Repeat("|", 7) + " " + label }
func cmkSep() string               { return strings.Repeat("=", 7) }
func cmkClose(label string) string { return strings.Repeat(">", 7) + " " + label }

// cmkText is lines as a file whose every line ends with a line feed.
func cmkText(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// cmkFiller is the numbered lines from to to, text no marker is mistaken for.
func cmkFiller(from, to int) []string {
	var lines []string
	for i := from; i <= to; i++ {
		lines = append(lines, fmt.Sprintf("filler line %02d", i))
	}
	return lines
}

// cmkJoin is the parts one after the other.
func cmkJoin(parts ...[]string) []string {
	var all []string
	for _, p := range parts {
		all = append(all, p...)
	}
	return all
}

// cmkSet is the five lines a conflict leaves: both sides between the markers.
func cmkSet() []string {
	return []string{cmkOpen("HEAD"), "our side", cmkSep(), "their side", cmkClose("topic (a change)")}
}

// cmkRepo is an empty repository whose line endings git leaves alone.
func cmkRepo(t *testing.T) string {
	t.Helper()
	root := repo(t)
	run(t, root, "config", "core.autocrlf", "false")
	run(t, root, "config", "core.safecrlf", "false")
	return root
}

// cmkScan commits before, then after (a file in before and not in after is
// deleted), and asks for the sets the second commit adds to the first, in
// order of path and line.
func cmkScan(t *testing.T, before, after map[string]string) []MarkerSet {
	t.Helper()
	root := cmkRepo(t)
	return cmkScanIn(t, root, before, after)
}

func cmkScanIn(t *testing.T, root string, before, after map[string]string) []MarkerSet {
	t.Helper()
	for p, body := range before {
		write(t, root, p, body)
	}
	base := lgCommit(t, root, "before")
	for p := range before {
		if _, kept := after[p]; !kept {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(p))); err != nil {
				t.Fatal(err)
			}
		}
	}
	for p, body := range after {
		write(t, root, p, body)
	}
	tip := lgCommit(t, root, "after")
	return cmkAsked(t, root, base, tip)
}

// cmkAsked is ConflictMarkers, which has to work, sorted.
func cmkAsked(t *testing.T, root, base, tip string) []MarkerSet {
	t.Helper()
	got, err := ConflictMarkers(t.Context(), root, base, tip)
	if err != nil {
		t.Fatalf("ConflictMarkers(%s..%s): %v", base[:12], tip[:12], err)
	}
	return cmkSorted(got)
}

func cmkSorted(sets []MarkerSet) []MarkerSet {
	out := slices.Clone(sets)
	slices.SortFunc(out, func(a, b MarkerSet) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), cmp.Compare(a.Open, b.Open))
	})
	return out
}

// cmkHolds holds the sets found to the sets wanted, in the order cmkSorted
// gives both.
func cmkHolds(t *testing.T, got, want []MarkerSet) {
	t.Helper()
	want = cmkSorted(want)
	if !slices.Equal(got, want) {
		t.Errorf("the sets found are\n  %+v\nwant\n  %+v", got, want)
	}
}

// ---------------------------------------------------------------- AC-1

// AC-1: an added set is found, with the line of each of its markers, however
// the file came to hold it.
func TestAC1ConflictMarkersASetTheChangeAddsIsFoundWithTheLineOfEachMarker(t *testing.T) {
	t.Parallel()
	crlf := func(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }
	for _, c := range []struct {
		name          string
		before, after map[string]string
		want          []MarkerSet
	}{
		{"AC-1 a set in a new file",
			nil,
			map[string]string{"notes.txt": cmkText("alpha", "beta", cmkOpen("HEAD"), "ours", cmkSep(), "theirs", cmkClose("topic"), "omega")},
			[]MarkerSet{{Path: "notes.txt", Open: 3, Separator: 5, Close: 7}}},
		{"AC-1 a set with the base side between, as diff3 writes it",
			nil,
			map[string]string{"notes.txt": cmkText("alpha", "beta", cmkOpen("HEAD"), "ours", cmkBase("parent of 1a2b3c4 (x)"), "was", cmkSep(), "theirs", cmkClose("1a2b3c4 (x)"), "omega")},
			[]MarkerSet{{Path: "notes.txt", Open: 3, Base: 5, Separator: 7, Close: 9}}},
		{"AC-1 a set in a file that was there, a long way into it",
			map[string]string{"ledger.txt": cmkText(cmkFiller(1, 40)...)},
			map[string]string{"ledger.txt": cmkText(cmkJoin(cmkFiller(1, 36), cmkSet(), cmkFiller(37, 40))...)},
			[]MarkerSet{{Path: "ledger.txt", Open: 37, Separator: 39, Close: 41}}},
		{"AC-1 a set at the first line of the file",
			nil,
			map[string]string{"notes.txt": cmkText(cmkJoin(cmkSet(), cmkFiller(1, 2))...)},
			[]MarkerSet{{Path: "notes.txt", Open: 1, Separator: 3, Close: 5}}},
		{"AC-1 three marker lines and nothing between them",
			nil,
			map[string]string{"notes.txt": cmkText(cmkOpen("a"), cmkSep(), cmkClose("b"))},
			[]MarkerSet{{Path: "notes.txt", Open: 1, Separator: 2, Close: 3}}},
		{"AC-1 markers with an empty label, which keep their space",
			nil,
			map[string]string{"notes.txt": cmkText("alpha", cmkOpen(""), "ours", cmkSep(), "theirs", cmkClose(""))},
			[]MarkerSet{{Path: "notes.txt", Open: 2, Separator: 4, Close: 6}}},
		{"AC-1 a set whose last line has no line feed after it",
			nil,
			map[string]string{"notes.txt": strings.Join(cmkJoin(cmkFiller(1, 2), cmkSet()), "\n")},
			[]MarkerSet{{Path: "notes.txt", Open: 3, Separator: 5, Close: 7}}},
		{"AC-1 a set in a file with CRLF line endings (decision 1)",
			nil,
			map[string]string{"notes.txt": crlf(cmkJoin(cmkFiller(1, 2), cmkSet(), cmkFiller(3, 3))...)},
			[]MarkerSet{{Path: "notes.txt", Open: 3, Separator: 5, Close: 7}}},
		{"AC-1 a set with a base side in a file with CRLF line endings (decision 1)",
			nil,
			map[string]string{"notes.txt": crlf(cmkOpen("HEAD"), "ours", cmkBase("parent"), "was", cmkSep(), "theirs", cmkClose("topic"))},
			[]MarkerSet{{Path: "notes.txt", Open: 1, Base: 3, Separator: 5, Close: 7}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cmkHolds(t, cmkScan(t, c.before, c.after), c.want)
		})
	}
}

// AC-1 and decision 2: a conflict from a rebase has the side that is trunk's
// unchanged beside the markers, so a minimal patch holds the opening marker in
// one hunk and the separator in the next. The set is one set all the same, and
// so is one whose markers are far apart.
func TestAC1AndDecision2ConflictMarkersASetSplitAcrossHunksIsOneSet(t *testing.T) {
	t.Parallel()
	body := cmkFiller(1, 30)
	for _, c := range []struct {
		name  string
		after []string
		want  MarkerSet
	}{
		{"AC-1 the side that is already in the file between the markers",
			cmkJoin(body[:14], []string{cmkOpen("HEAD")}, body[14:15], []string{cmkSep(), "our edit", cmkClose("topic")}, body[15:]),
			MarkerSet{Path: "notes.txt", Open: 15, Separator: 17, Close: 19}},
		{"AC-1 the base side unchanged as well",
			cmkJoin(body[:9], []string{cmkOpen("HEAD")}, body[9:11], []string{cmkBase("parent")}, body[11:13], []string{cmkSep(), "our edit", cmkClose("topic")}, body[13:]),
			MarkerSet{Path: "notes.txt", Open: 10, Base: 13, Separator: 16, Close: 18}},
		{"AC-1 decision 2: markers a dozen lines apart with nothing of the story's between",
			cmkJoin(body[:2], []string{cmkOpen("a")}, body[2:14], []string{cmkSep()}, body[14:26], []string{cmkClose("b")}, body[26:]),
			MarkerSet{Path: "notes.txt", Open: 3, Separator: 16, Close: 29}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := cmkScan(t,
				map[string]string{"notes.txt": cmkText(body...)},
				map[string]string{"notes.txt": cmkText(c.after...)})
			cmkHolds(t, got, []MarkerSet{c.want})
		})
	}
}

// AC-1: a file that gets two conflicts has two sets, each with its own lines,
// and the second is found after the first has closed.
func TestAC1ConflictMarkersEverySetOfAFileIsFoundInOrder(t *testing.T) {
	t.Parallel()
	got := cmkScan(t, nil, map[string]string{"notes.txt": cmkText(cmkJoin(
		cmkFiller(1, 2), cmkSet(), cmkFiller(3, 6),
		[]string{cmkOpen("HEAD"), cmkBase("parent"), "was", cmkSep(), "theirs", cmkClose("topic")}, cmkFiller(7, 8))...)})
	cmkHolds(t, got, []MarkerSet{
		{Path: "notes.txt", Open: 3, Separator: 5, Close: 7},
		{Path: "notes.txt", Open: 12, Base: 13, Separator: 15, Close: 17},
	})
}

// AC-1 and THREATS T4: a set is one file's. Markers spread over files are no
// set in any of them, and what one file added does not carry to the next.
func TestAC1ConflictMarkersASetIsOneFilesAndTheStateDoesNotCarryOver(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		after map[string]string
	}{
		{"AC-1 the three markers in three files", map[string]string{
			"a.txt": cmkText("a", cmkOpen("HEAD")), "b.txt": cmkText("b", cmkSep()), "c.txt": cmkText("c", cmkClose("topic"))}},
		{"AC-1 the opening marker in one file and the other two in the next", map[string]string{
			"a.txt": cmkText("a", cmkOpen("HEAD"), "ours"), "b.txt": cmkText("b", cmkSep(), "theirs", cmkClose("topic"))}},
		{"AC-1 the opening and the separator in one file and the closing in the next", map[string]string{
			"a.txt": cmkText(cmkOpen("HEAD"), "ours", cmkSep()), "b.txt": cmkText("theirs", cmkClose("topic"))}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := cmkScan(t, nil, c.after); len(got) != 0 {
				t.Errorf("a set was found in markers that are in separate files: %+v", got)
			}
		})
	}
}

// AC-1, decision 2 and THREATS T4: a file holding a set is found beside files
// that hold only part of one, each named for itself.
func TestAC1ConflictMarkersASetIsNamedForItsOwnFileBesideOtherFiles(t *testing.T) {
	t.Parallel()
	got := cmkScan(t, nil, map[string]string{
		"a.txt":     cmkText("a", cmkOpen("HEAD")),
		"dir/b.txt": cmkText(cmkJoin(cmkFiller(1, 3), cmkSet())...),
		"c.txt":     cmkText(cmkSep(), "c", cmkClose("topic")),
	})
	cmkHolds(t, got, []MarkerSet{{Path: "dir/b.txt", Open: 4, Separator: 6, Close: 8}})
}

// AC-1: the text that is a conflict is not a conflict where it is not a set.
// Each of these is ordinary text, a conflict half made, or a conflict in a
// shape the criterion does not name, and none is found.
func TestAC1ConflictMarkersNearMissesAreNotASet(t *testing.T) {
	t.Parallel()
	bare := func(c string) string { return strings.Repeat(c, 7) }
	for _, c := range []struct {
		name string
		body string
	}{
		{"AC-1 a lone separator", cmkText("alpha", cmkSep(), "beta")},
		{"AC-1 a setext heading", cmkText("Title", cmkSep(), "", "Some text under it.")},
		{"AC-1 two setext headings", cmkText("One", cmkSep(), "", "Two", cmkSep())},
		{"AC-1 the closing marker, the separator, then the opening", cmkText(cmkClose("b"), cmkSep(), cmkOpen("a"))},
		{"AC-1 the opening, the closing, then the separator", cmkText(cmkOpen("a"), cmkClose("b"), cmkSep())},
		{"AC-1 the separator, then the opening and the closing", cmkText(cmkSep(), cmkOpen("a"), cmkClose("b"))},
		{"AC-1 the opening and the separator only", cmkText(cmkOpen("a"), "ours", cmkSep(), "theirs")},
		{"AC-1 the separator and the closing only", cmkText("ours", cmkSep(), "theirs", cmkClose("b"))},
		{"AC-1 the opening and the closing only", cmkText(cmkOpen("a"), "ours", cmkClose("b"))},
		{"AC-1 the base marker and the separator and the closing", cmkText(cmkBase("p"), "was", cmkSep(), "theirs", cmkClose("b"))},
		{"AC-1 every marker indented by spaces", cmkText("  "+cmkOpen("a"), "  "+cmkSep(), "  "+cmkClose("b"))},
		{"AC-1 every marker indented by a tab", cmkText("\t"+cmkOpen("a"), "\t"+cmkSep(), "\t"+cmkClose("b"))},
		{"AC-1 the opening marker indented and the others not", cmkText(" "+cmkOpen("a"), cmkSep(), cmkClose("b"))},
		{"AC-1 the closing marker indented and the others not", cmkText(cmkOpen("a"), cmkSep(), " "+cmkClose("b"))},
		{"AC-1 markers with no label and no space after them", cmkText(bare("<"), "ours", cmkSep(), "theirs", bare(">"))},
		{"AC-1 markers of eight characters (decision 9: seven only)",
			cmkText(strings.Repeat("<", 8)+" a", "ours", strings.Repeat("=", 8), "theirs", strings.Repeat(">", 8)+" b")},
		{"AC-1 markers of ten characters, as a conflict-marker-size of ten writes them (decision 9)",
			cmkText(strings.Repeat("<", 10)+" a", "ours", strings.Repeat("=", 10), "theirs", strings.Repeat(">", 10)+" b")},
		{"AC-1 markers of six characters",
			cmkText(strings.Repeat("<", 6)+" a", "ours", strings.Repeat("=", 6), "theirs", strings.Repeat(">", 6)+" b")},
		{"AC-1 a separator with a space after it", cmkText(cmkOpen("a"), "ours", cmkSep()+" ", "theirs", cmkClose("b"))},
		{"AC-1 a separator with a label after it", cmkText(cmkOpen("a"), "ours", cmkSep()+" x", "theirs", cmkClose("b"))},
		{"AC-1 a separator with two carriage returns (decision 1: one)",
			strings.Join([]string{cmkOpen("a"), "ours", cmkSep() + "\r\r", "theirs", cmkClose("b")}, "\n") + "\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := cmkScan(t, nil, map[string]string{"notes.md": c.body}); len(got) != 0 {
				t.Errorf("found %+v in text that is not an unresolved conflict:\n%s", got, c.body)
			}
		})
	}
}

// AC-1: all three have to be lines the story added. A story that touches a
// conflict that is already in the file, and not all of it, adds no conflict.
func TestAC1ConflictMarkersASetOnlyPartlyAddedIsNotAdded(t *testing.T) {
	t.Parallel()
	held := cmkJoin(cmkFiller(1, 10), cmkSet(), cmkFiller(11, 20))
	edit := func(at int, to string) []string {
		out := slices.Clone(held)
		out[at] = to
		return out
	}
	// held has the opening marker at index 10, the separator at 12 and the
	// closing marker at 14.
	for _, c := range []struct {
		name  string
		after []string
	}{
		{"AC-1 only the label of the opening marker changed", edit(10, cmkOpen("another label"))},
		{"AC-1 only the label of the closing marker changed", edit(14, cmkClose("another label"))},
		{"AC-1 only the text between the markers changed", edit(11, "our side, edited")},
		{"AC-1 only the other side changed", edit(13, "their side, edited")},
		{"AC-1 only a line below the set added", cmkJoin(held, []string{"tail"})},
		{"AC-1 only a line above the set added", cmkJoin([]string{"head"}, held)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := cmkScan(t,
				map[string]string{"notes.txt": cmkText(held...)},
				map[string]string{"notes.txt": cmkText(c.after...)})
			if len(got) != 0 {
				t.Errorf("found %+v where the change added part of a set that was already there", got)
			}
		})
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: a file that already held a set, where the story adds none, adds none:
// nothing is found, and a set the story does add is found by its own lines in
// the file as it is now, not the lines that were there.
func TestAC2ConflictMarkersASetAlreadyInTheFileIsNotTheChangesButANewOneIs(t *testing.T) {
	t.Parallel()
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 24))
	before := map[string]string{"notes.txt": cmkText(held...)}
	if got := cmkScan(t, before, map[string]string{"notes.txt": cmkText(cmkJoin(held, []string{"an unrelated line"})...)}); len(got) != 0 {
		t.Errorf("AC-2: found %+v in a file whose set was there before the change", got)
	}
	// A line is added above the set, which moves it down; it is still not added.
	if got := cmkScan(t, before, map[string]string{"notes.txt": cmkText(cmkJoin([]string{"a new first line"}, held)...)}); len(got) != 0 {
		t.Errorf("AC-2: found %+v where the change only moved a set that was there", got)
	}
	// The change adds a second set below the first: only the second is its own.
	got := cmkScan(t, before, map[string]string{"notes.txt": cmkText(cmkJoin(held, cmkFiller(25, 26), cmkSet())...)})
	cmkHolds(t, got, []MarkerSet{{Path: "notes.txt", Open: 32, Separator: 34, Close: 36}})
}

// AC-2 and decision 4: moving a file that holds a set is not adding one. The
// move may come with edits to the file, and the edits are the change.
func TestAC2AndDecision4ConflictMarkersAMovedFileAddsNoSetButAnEditThatDoesIsFound(t *testing.T) {
	t.Parallel()
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 40))
	for _, c := range []struct {
		name string
		// edit is the file as it is after the move, from the file as it was.
		edit func([]string) []string
		want []MarkerSet
	}{
		{"AC-2 a pure rename", func(l []string) []string { return l }, nil},
		{"AC-2 a rename with a line edited elsewhere", func(l []string) []string {
			out := slices.Clone(l)
			out[2] = "edited"
			return out
		}, nil},
		{"AC-2 a rename with the set's own text edited", func(l []string) []string {
			out := slices.Clone(l)
			out[21] = "our side, edited"
			return out
		}, nil},
		{"AC-1 a rename with a second set added below", func(l []string) []string { return cmkJoin(l, cmkSet()) },
			[]MarkerSet{{Path: "archive/notes.txt", Open: 46, Separator: 48, Close: 50}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := cmkRepo(t)
			write(t, root, "notes.txt", cmkText(held...))
			base := lgCommit(t, root, "before")
			if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
				t.Fatal(err)
			}
			run(t, root, "mv", "notes.txt", "archive/notes.txt")
			write(t, root, "archive/notes.txt", cmkText(c.edit(held)...))
			tip := lgCommit(t, root, "moved")
			cmkHolds(t, cmkAsked(t, root, base, tip), c.want)
		})
	}
}

// AC-2 and decision 4: a rename is found whatever the repository's settings
// say about renames, since the call asks for them itself.
func TestAC2AndDecision4ConflictMarkersAMovedFileIsKnownWhateverTheRepositoryIsSetToDiff(t *testing.T) {
	t.Parallel()
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 40))
	root := cmkRepo(t)
	run(t, root, "config", "diff.renames", "false")
	write(t, root, "notes.txt", cmkText(held...))
	base := lgCommit(t, root, "before")
	run(t, root, "mv", "notes.txt", "moved.txt")
	tip := lgCommit(t, root, "moved")
	if got := cmkAsked(t, root, base, tip); len(got) != 0 {
		t.Errorf("AC-2: found %+v in a file that was only moved, with diff.renames off", got)
	}
}

// Decision 4: copies are not asked for, so a copy of a file that holds a set
// is a file the change adds, and its set is the change's.
func TestDecision4ConflictMarkersACopyOfAFileThatHoldsASetIsAnAdd(t *testing.T) {
	t.Parallel()
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 40))
	got := cmkScan(t,
		map[string]string{"notes.txt": cmkText(held...)},
		map[string]string{"notes.txt": cmkText(held...), "copy.txt": cmkText(held...)})
	cmkHolds(t, got, []MarkerSet{{Path: "copy.txt", Open: 21, Separator: 23, Close: 25}})
}

// AC-2: what the change removes is not what it adds. A set deleted along with
// its file, or taken out of the file, is not found.
func TestAC2ConflictMarkersASetTheChangeTakesAwayIsNotFound(t *testing.T) {
	t.Parallel()
	held := cmkJoin(cmkFiller(1, 5), cmkSet(), cmkFiller(6, 8))
	before := map[string]string{"notes.txt": cmkText(held...), "keep.txt": "kept\n"}
	if got := cmkScan(t, before, map[string]string{"keep.txt": "kept\n"}); len(got) != 0 {
		t.Errorf("found %+v in a file the change deleted", got)
	}
	if got := cmkScan(t, before, map[string]string{"notes.txt": cmkText(cmkFiller(1, 8)...), "keep.txt": "kept\n"}); len(got) != 0 {
		t.Errorf("found %+v in a file the change put right", got)
	}
}

// AC-1: the change is the net of the range. A set that one commit adds and a
// later one takes out is not in the change; and the range starts at base, so
// a set that was there at base is not either.
func TestAC1ConflictMarkersTheChangeIsTheNetOfTheRangeAndStartsAtBase(t *testing.T) {
	t.Parallel()
	root := cmkRepo(t)
	write(t, root, "notes.txt", cmkText(cmkFiller(1, 4)...))
	base := lgCommit(t, root, "base")
	write(t, root, "notes.txt", cmkText(cmkJoin(cmkFiller(1, 4), cmkSet())...))
	added := lgCommit(t, root, "adds a set")
	write(t, root, "notes.txt", cmkText(cmkFiller(1, 4)...))
	removed := lgCommit(t, root, "takes it out again")
	write(t, root, "notes.txt", cmkText(cmkJoin(cmkFiller(1, 4), cmkSet(), cmkFiller(5, 6), cmkSet())...))
	second := lgCommit(t, root, "adds two")

	if got := cmkAsked(t, root, base, removed); len(got) != 0 {
		t.Errorf("found %+v where one commit added a set and the next took it out", got)
	}
	cmkHolds(t, cmkAsked(t, root, base, added), []MarkerSet{{Path: "notes.txt", Open: 5, Separator: 7, Close: 9}})
	cmkHolds(t, cmkAsked(t, root, base, second), []MarkerSet{
		{Path: "notes.txt", Open: 5, Separator: 7, Close: 9},
		{Path: "notes.txt", Open: 12, Separator: 14, Close: 16},
	})
	cmkHolds(t, cmkAsked(t, root, added, second), []MarkerSet{{Path: "notes.txt", Open: 12, Separator: 14, Close: 16}})
}

// ---------------------------------------------------------------- THREATS

// THREATS T1, decision 7: an attribute the story commits cannot hide a set.
// With "-diff" or "binary", git prints "Binary files differ" for the file and
// no line of it; a call that reads the patch as text prints them.
func TestAC1AndT1ConflictMarkersNoAttributeAndNoBinaryContentHidesASet(t *testing.T) {
	t.Parallel()
	set := cmkText(cmkJoin(cmkFiller(1, 2), cmkSet())...)
	want := []MarkerSet{{Path: "notes.txt", Open: 3, Separator: 5, Close: 7}}
	for _, c := range []struct {
		name       string
		attributes string
		body       string
	}{
		{"T1 the file is -diff", "notes.txt -diff\n", set},
		{"T1 the file is binary", "notes.txt binary\n", set},
		{"T1 every file is -diff", "* -diff\n", set},
		{"T1 the file is -text", "notes.txt -text\n", set},
		{"T1 the file is -diff and -text", "notes.txt -diff -text\n", set},
		{"T1 the file names a diff driver nobody defined", "notes.txt diff=nothing\n", set},
		{"T1 the file has a null byte, which git reads as binary", "", "\x00 binary\n" + set},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			after := map[string]string{"notes.txt": c.body}
			wantHere := want
			if c.body != set {
				wantHere = []MarkerSet{{Path: "notes.txt", Open: 4, Separator: 6, Close: 8}}
			}
			if c.attributes != "" {
				after[".gitattributes"] = c.attributes
			}
			cmkHolds(t, cmkScan(t, nil, after), wantHere)
		})
	}
}

// THREATS T1: an attribute that is already in the base commit hides nothing
// either, and nor does one in the configuration of the repository.
func TestAC1AndT1ConflictMarkersAnAttributeAlreadyInForceHidesNothing(t *testing.T) {
	t.Parallel()
	root := cmkRepo(t)
	write(t, root, ".gitattributes", "*.txt binary\n")
	write(t, root, "keep.md", "kept\n")
	base := lgCommit(t, root, "attributes")
	write(t, root, "notes.txt", cmkText(cmkJoin(cmkFiller(1, 2), cmkSet())...))
	tip := lgCommit(t, root, "a set in a binary file")
	cmkHolds(t, cmkAsked(t, root, base, tip), []MarkerSet{{Path: "notes.txt", Open: 3, Separator: 5, Close: 7}})
}

// THREATS T2: the repository's configuration, which a session can write,
// does not move what the call finds: not how a patch names its files, how
// much context it carries, the algorithm that cuts it, nor whether a name is
// quoted.
func TestAC1AndT2ConflictMarkersTheRepositorysDiffSettingsDoNotMoveTheFindings(t *testing.T) {
	t.Parallel()
	for _, settings := range [][]string{
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"diff.srcPrefix", "x/", "diff.dstPrefix", "y/"},
		{"diff.context", "0"},
		{"diff.context", "40", "diff.interHunkContext", "40"},
		{"diff.algorithm", "patience"},
		{"diff.indentHeuristic", "false"},
		{"diff.suppressBlankEmpty", "true"},
		{"diff.relative", "true"},
		{"core.quotePath", "false"},
		{"diff.renameLimit", "1"},
	} {
		t.Run("T2 "+strings.Join(settings, " "), func(t *testing.T) {
			t.Parallel()
			root := cmkRepo(t)
			for i := 0; i < len(settings); i += 2 {
				run(t, root, "config", settings[i], settings[i+1])
			}
			body := cmkFiller(1, 30)
			write(t, root, "café notes.txt", cmkText(body...))
			base := lgCommit(t, root, "before")
			after := cmkJoin(body[:14], []string{cmkOpen("HEAD")}, body[14:15], []string{cmkSep(), "our edit", cmkClose("topic")}, body[15:])
			write(t, root, "café notes.txt", cmkText(after...))
			tip := lgCommit(t, root, "after")
			cmkHolds(t, cmkAsked(t, root, base, tip), []MarkerSet{{Path: "café notes.txt", Open: 15, Separator: 17, Close: 19}})
		})
	}
}

// THREATS T6: a path is reported as it is, whatever git does to write it into
// a patch: quoted for a byte outside ASCII, a quote, a tab; split at a space.
func TestAC1AndT6ConflictMarkersAPathIsNamedAsItIsWithNoQuotesAround(t *testing.T) {
	t.Parallel()
	names := []string{"café it.txt", "dir with space/x y.txt", "日本語/ノート.txt", "a b c d.txt", "back-tick`.txt", "dollar$name.txt", "semi;colon.txt"}
	if runtime.GOOS != "windows" {
		names = append(names, "tab\there.txt", "quo\"te.txt", "back\\slash.txt", "newline\nname.txt")
	}
	after := map[string]string{}
	var want []MarkerSet
	for _, name := range names {
		after[name] = cmkText(cmkJoin(cmkFiller(1, 2), cmkSet())...)
		want = append(want, MarkerSet{Path: name, Open: 3, Separator: 5, Close: 7})
	}
	cmkHolds(t, cmkScan(t, nil, after), want)
}

// THREATS T7: text the story adds that reads like a patch -- a header, a hunk
// header, a removed line that looks like a file header -- does not move the
// set to another file or another line, and does not end the read early.
func TestAC1AndT7ConflictMarkersAddedTextThatLooksLikeAPatchMovesNothing(t *testing.T) {
	t.Parallel()
	lines := []string{
		"+++ b/other.txt",
		"++ b/other.txt",
		"@@ -1,1 +40,9 @@",
		"diff --git a/other.txt b/other.txt",
		"--- a/other.txt",
		"-- a/other.txt",
		"index 0000000..1111111 100644",
		"\\ No newline at end of file",
		"new file mode 100644",
		"rename from other.txt",
	}
	body := cmkJoin(lines, cmkSet(), lines)
	got := cmkScan(t,
		map[string]string{"a.txt": cmkText("--- a/other.txt", "-- a/other.txt", "+++ b/other.txt", "keep")},
		map[string]string{"a.txt": cmkText(body...), "other.txt": "other\n"})
	cmkHolds(t, got, []MarkerSet{{Path: "a.txt", Open: len(lines) + 1, Separator: len(lines) + 3, Close: len(lines) + 5}})

	// And the set after a hunk header that claims a different place in the file.
	got = cmkScan(t, nil, map[string]string{"b.txt": cmkText(cmkJoin([]string{"@@ -1 +900 @@"}, cmkSet())...)})
	cmkHolds(t, got, []MarkerSet{{Path: "b.txt", Open: 2, Separator: 4, Close: 6}})
}

// THREATS D1: a line of any length is read without being held, and it is one
// line however long it is: the markers after it are on the lines they are on.
func TestAC1AndD1ConflictMarkersALineOfAnyLengthIsOneLine(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("x", 3<<20+7)
	got := cmkScan(t, nil, map[string]string{
		"big.txt": cmkText(cmkJoin([]string{huge}, cmkSet(), []string{huge})...),
	})
	cmkHolds(t, got, []MarkerSet{{Path: "big.txt", Open: 2, Separator: 4, Close: 6}})

	// A marker's own line may be long: the label is anything.
	got = cmkScan(t, nil, map[string]string{
		"labels.txt": cmkText(cmkOpen(huge), "ours", cmkSep(), "theirs", cmkClose(huge)),
	})
	cmkHolds(t, got, []MarkerSet{{Path: "labels.txt", Open: 1, Separator: 3, Close: 5}})
}

// Decision 7 and THREATS T13: CODEMAP.md is read like any file. The root
// one is left out of the fingerprint of a change and is not left out here.
func TestAC1AndT13ConflictMarkersCODEMAPIsScannedAtTheRootAndBelow(t *testing.T) {
	t.Parallel()
	got := cmkScan(t,
		map[string]string{"CODEMAP.md": "# CODEMAP\n", "docs/CODEMAP.md": "# docs\n"},
		map[string]string{
			"CODEMAP.md":      cmkText(cmkJoin([]string{"# CODEMAP", ""}, cmkSet())...),
			"docs/CODEMAP.md": cmkText(cmkJoin([]string{"# docs", ""}, cmkSet())...),
		})
	cmkHolds(t, got, []MarkerSet{
		{Path: "CODEMAP.md", Open: 3, Separator: 5, Close: 7},
		{Path: "docs/CODEMAP.md", Open: 3, Separator: 5, Close: 7},
	})
}

// A hash that is not a full one is refused, as for Fingerprint: a name could
// be moved by the story between the check and the landing.
func TestAC1ConflictMarkersAnAbbreviatedOrNamedCommitIsRefused(t *testing.T) {
	t.Parallel()
	root := cmkRepo(t)
	write(t, root, "a.txt", "a\n")
	base := lgCommit(t, root, "base")
	write(t, root, "a.txt", "b\n")
	tip := lgCommit(t, root, "tip")
	for _, c := range []struct{ name, base, tip string }{
		{"a branch name", "HEAD", tip},
		{"an abbreviated hash", base[:12], tip},
		{"an abbreviated tip", base, tip[:12]},
		{"an option", "--output=/dev/null", tip},
		{"nothing", "", tip},
	} {
		if _, err := ConflictMarkers(t.Context(), root, c.base, c.tip); err == nil {
			t.Errorf("ConflictMarkers accepted %s", c.name)
		}
	}
}

// A commit that is not there is an error and not an empty answer: a check
// that found nothing because it could not look would pass the story.
func TestAC1ConflictMarkersACommitThatCannotBeReadIsAnErrorNotAnEmptyAnswer(t *testing.T) {
	t.Parallel()
	root := cmkRepo(t)
	write(t, root, "a.txt", "a\n")
	base := lgCommit(t, root, "base")
	missing := strings.Repeat("0123456789abcdef", 4)[:len(base)]
	got, err := ConflictMarkers(t.Context(), root, base, missing)
	if err == nil {
		t.Fatalf("ConflictMarkers(base, %s) = %+v with no error for a commit the repository does not have", missing, got)
	}
}

// ---------------------------------------------------------------- the reading

// cmkRead gives a scan the patch as git would have printed it, without git.
func cmkRead(patch string) (*markerScan, error) {
	s := &markerScan{}
	return s, s.read(bufio.NewReader(strings.NewReader(patch)))
}

// cmkFile is the lines of a patch that name one file, up to its first hunk.
const cmkFile = "diff --git a/f b/f\nindex 1111111..2222222 100644\n--- a/f\n+++ b/f\n"

// A patch git cannot have written is an error that says the change could not
// be read: a check that read "nothing" from it would pass a story it never
// looked at.
func TestAC1ConflictMarkersAPatchGitCannotHaveWrittenIsAnErrorAndNotNothing(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, patch string }{
		{"AC-1 a file name longer than the scan holds", "diff --git a/f b/f\n+++ b/" + strings.Repeat("a", headerKeep+10) + "\n"},
		{"AC-1 a quoted file name that is not a quotation", "diff --git a/f b/f\n+++ \"b/\\q\"\n"},
		{"AC-1 a file name without the prefix the call asked for", "diff --git a/f b/f\n+++ c/f\n"},
		{"AC-1 a file name that is only the prefix", "diff --git a/f b/f\n+++ b/\n"},
		{"AC-1 a hunk header that is not one", cmkFile + "@@ nothing like it @@\n"},
		{"AC-1 a hunk header whose count is too large to be a number", cmkFile + "@@ -0,0 +1,99999999999999999999 @@\n"},
		{"AC-1 a line the hunk has no room for", cmkFile + "@@ -0,0 +1,1 @@\n-gone\n"},
		{"AC-1 a context line the hunk has no room for", cmkFile + "@@ -0,0 +1,1 @@\n kept\n"},
		{"AC-1 a line with a prefix no hunk line has", cmkFile + "@@ -0,0 +1,1 @@\n?what\n"},
		{"AC-1 added lines in a file with no name", "diff --git a/f b/f\n@@ -0,0 +1,1 @@\n+added\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := cmkRead(c.patch)
			var e *sdlcerr.Error
			if !errors.As(err, &e) || e.Code != sdlcerr.RepositoryUnreadable {
				t.Errorf("the scan answered %v, want the error for a change that could not be read", err)
			}
		})
	}
}

// A patch that ends inside a hunk is an error however it ends, and so is a
// read that fails: the check fails closed.
func TestAC1ConflictMarkersAPatchThatEndsInsideAHunkOrCannotBeReadIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := cmkRead(cmkFile + "@@ -0,0 +1,2 @@\n+one line of two\n"); err == nil {
		t.Error("a patch that ends with a line of its hunk to come was read as complete")
	}
	boom := errors.New("the pipe broke")
	s := &markerScan{}
	if err := s.read(bufio.NewReader(iotest.ErrReader(boom))); !errors.Is(err, boom) {
		t.Errorf("the scan answered %v for a patch that could not be read, want the read's own error", err)
	}
}

// The shapes of a patch that are not lines added: context, a line without its
// line feed in the old file, a context line git printed without its space, a
// section heading after the header, and a last line with no line feed. The
// numbers are the lines of the new file.
func TestAC1ConflictMarkersTheLinesOfAHunkAreNumberedAsTheNewFileHasThem(t *testing.T) {
	t.Parallel()
	patch := cmkFile +
		"@@ -1,3 +1,5 @@ func heading()\n" +
		" kept\n" +
		"\n" +
		"-old last line\n" +
		"\\ No newline at end of file\n" +
		"+" + cmkOpen("HEAD") + "\n" +
		"+" + cmkSep() + "\n" +
		"+" + cmkClose("topic") // no line feed: git ends a file this way
	s, err := cmkRead(patch)
	if err != nil {
		t.Fatal(err)
	}
	cmkHolds(t, s.out.Sets, []MarkerSet{{Path: "f", Open: 3, Separator: 4, Close: 5}})
	if len(s.pending) != 0 {
		t.Errorf("%+v is left to be read from the file, where the patch showed every line", s.pending)
	}
}

// The cap on what a scan keeps: a diff built to hold more sets than that holds
// no more of them, and says how many were left out.
func TestAC1ConflictMarkersASetPastTheCapIsCountedAndNotKept(t *testing.T) {
	t.Parallel()
	const sets = maxMarkerSets + 1
	var b strings.Builder
	b.WriteString(cmkFile + "@@ -0,0 +1," + strconv.Itoa(3*sets) + " @@\n")
	for range sets {
		b.WriteString("+" + cmkOpen("a") + "\n+" + cmkSep() + "\n+" + cmkClose("b") + "\n")
	}
	s, err := cmkRead(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.out.Sets) != maxMarkerSets || s.out.Beyond != 1 {
		t.Errorf("kept %d sets and counted %d beyond, want %d and 1", len(s.out.Sets), s.out.Beyond, maxMarkerSets)
	}
}

// ---------------------------------------------------------------- the separator the patch does not show

// A conflict put around a separator line the file had already: git aligns that
// line with the one it had, so the patch shows an opening marker and a closing
// marker and no separator between them, and the file has to say whether there
// is one.
func TestAC1ConflictMarkersASeparatorTheFileHadAlreadyIsTheSeparatorOfTheSetAroundIt(t *testing.T) {
	t.Parallel()
	other := cmkJoin(cmkFiller(1, 2), []string{cmkOpen("x"), cmkSep(), cmkClose("y")}, cmkFiller(3, 4))
	for _, c := range []struct {
		name          string
		before, after []string
		want          []MarkerSet
		crlf          bool
	}{
		{"AC-1 an opening marker and a closing one around the separator the file had",
			cmkJoin(cmkFiller(1, 3), []string{cmkSep()}, cmkFiller(4, 6)),
			cmkJoin(cmkFiller(1, 3), []string{cmkOpen("HEAD"), "ours", cmkSep(), "theirs", cmkClose("topic")}, cmkFiller(4, 6)),
			[]MarkerSet{{Path: "notes.txt", Open: 4, Separator: 6, Close: 8}}, false},
		{"AC-1 with a base side between the opening marker and the separator",
			cmkJoin(cmkFiller(1, 3), []string{cmkSep()}, cmkFiller(4, 6)),
			cmkJoin(cmkFiller(1, 3), []string{cmkOpen("HEAD"), "ours", cmkBase("parent"), "was", cmkSep(), "theirs", cmkClose("topic")}, cmkFiller(4, 6)),
			[]MarkerSet{{Path: "notes.txt", Open: 4, Base: 6, Separator: 8, Close: 10}}, false},
		{"AC-1 in a file with CRLF line endings (decision 1)",
			cmkJoin(cmkFiller(1, 3), []string{cmkSep()}, cmkFiller(4, 6)),
			cmkJoin(cmkFiller(1, 3), []string{cmkOpen("HEAD"), "ours", cmkSep(), "theirs", cmkClose("topic")}, cmkFiller(4, 6)),
			[]MarkerSet{{Path: "notes.txt", Open: 4, Separator: 6, Close: 8}}, true},
		{"AC-1 two such sets in one file, each with the separator of its own",
			[]string{"top", cmkSep(), "middle one", "middle two", cmkSep(), "bottom"},
			[]string{"top", cmkOpen("a"), "a ours", cmkSep(), "a theirs", cmkClose("a"), "middle one", "middle two", cmkOpen("b"), "b ours", cmkSep(), "b theirs", cmkClose("b"), "bottom"},
			[]MarkerSet{{Path: "notes.txt", Open: 2, Separator: 4, Close: 6}, {Path: "notes.txt", Open: 9, Separator: 11, Close: 13}}, false},
		{"AC-1 the file's separator is above the opening marker, so it is not between them",
			cmkJoin(cmkFiller(1, 2), []string{cmkSep()}, cmkFiller(3, 6)),
			cmkJoin(cmkFiller(1, 2), []string{cmkSep()}, cmkFiller(3, 4), []string{cmkOpen("HEAD"), "ours", cmkClose("topic")}, cmkFiller(5, 6)),
			nil, false},
		{"AC-1 the file's separator is below the closing marker",
			cmkJoin(cmkFiller(1, 4), []string{cmkSep()}, cmkFiller(5, 6)),
			cmkJoin(cmkFiller(1, 2), []string{cmkOpen("HEAD"), "ours", cmkClose("topic")}, cmkFiller(3, 4), []string{cmkSep()}, cmkFiller(5, 6)),
			nil, false},
		{"AC-1 the file has no separator at all",
			cmkFiller(1, 6),
			cmkJoin(cmkFiller(1, 3), []string{cmkOpen("HEAD"), "ours", cmkClose("topic")}, cmkFiller(4, 6)),
			nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			text := cmkText
			if c.crlf {
				text = func(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }
			}
			// A second file, in which the patch shows the separator, is found
			// beside whatever the first gives: the files are told apart.
			got := cmkScan(t,
				map[string]string{"notes.txt": text(c.before...), "other.txt": text(cmkFiller(1, 4)...)},
				map[string]string{"notes.txt": text(c.after...), "other.txt": text(other...)})
			want := append(slices.Clone(c.want), MarkerSet{Path: "other.txt", Open: 3, Separator: 4, Close: 5})
			cmkHolds(t, got, want)
		})
	}
}

// What the scan reads of a file to find the separator: the file is read once,
// holds no line, and says nothing where the file ends before the closing
// marker, where there is nothing to look for, and where it cannot be read.
func TestAC1ConflictMarkersTheSeparatorIsLookedForOnlyBetweenTheMarkersOfTheFile(t *testing.T) {
	t.Parallel()
	cand := func(open, base, closing int) candidate {
		return candidate{set: MarkerSet{Path: "f", Open: open, Base: base, Close: closing}}
	}
	file := cmkText("one", "two", cmkSep(), "four", cmkSep(), "six")

	found := make([]int, 2)
	cands := []candidate{cand(1, 0, 4), cand(4, 0, 6)}
	if err := separatorLines(bufio.NewReader(strings.NewReader(file)), cands, found); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(found, []int{3, 5}) {
		t.Errorf("found the separators at %v, want [3 5]", found)
	}

	// The base side moves where the search starts: the separator at 3 is above it.
	found = make([]int, 1)
	if err := separatorLines(bufio.NewReader(strings.NewReader(file)), []candidate{cand(1, 4, 6)}, found); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(found, []int{5}) {
		t.Errorf("found the separator at %v with the base side at 4, want [5]", found)
	}

	// A file that ends before the closing marker has nothing more to say.
	found = make([]int, 1)
	if err := separatorLines(bufio.NewReader(strings.NewReader("one\ntwo\n")), []candidate{cand(1, 0, 9)}, found); err != nil || found[0] != 0 {
		t.Errorf("a file shorter than the closing marker gave %v and %v, want no separator and no error", found, err)
	}

	if err := separatorLines(bufio.NewReader(strings.NewReader(file)), nil, nil); err != nil {
		t.Errorf("with no candidate the file was read and gave %v", err)
	}

	boom := errors.New("the object could not be read")
	if err := separatorLines(bufio.NewReader(iotest.ErrReader(boom)), []candidate{cand(1, 0, 4)}, make([]int, 1)); !errors.Is(err, boom) {
		t.Errorf("a file that could not be read gave %v, want the read's own error", err)
	}
}

// A candidate whose object cannot be named or cannot be read is an error and
// not a set that was not found: the check fails closed.
func TestAC1ConflictMarkersACandidateWhoseFileCannotBeReadIsAnError(t *testing.T) {
	t.Parallel()
	root := cmkRepo(t)
	for _, c := range []struct{ name, blob string }{
		{"AC-1 no object named", ""},
		{"AC-1 an object named by an abbreviation", "2222222"},
		{"AC-1 an object the repository does not have", strings.Repeat("0", 40)},
	} {
		s := &markerScan{pending: []candidate{{set: MarkerSet{Path: "f", Open: 1, Close: 3}, blob: c.blob}}}
		if err := s.resolve(t.Context(), root); err == nil {
			t.Errorf("%s: resolve found no error", c.name)
		}
	}
}

// ---------------------------------------------------------------- the files left out

// AC-3's other half in git: a file the caller says may hold the lines is not
// read, and is not counted, so a set in it neither shows nor pushes another
// out.
func TestAC3ConflictMarkersAFileTheCallerLeavesOutIsNotReadAndAnotherStillIs(t *testing.T) {
	t.Parallel()
	root := cmkRepo(t)
	write(t, root, "keep.txt", "a\n")
	base := lgCommit(t, root, "before")
	body := cmkText(cmkJoin([]string{"# Merging", ""}, cmkSet())...)
	write(t, root, "docs/merging.md", body)
	write(t, root, "elsewhere/trap.md", body)
	tip := lgCommit(t, root, "after")

	var asked []string
	scan, err := ConflictMarkersOutside(t.Context(), root, base, tip, func(path string) bool {
		asked = append(asked, path)
		return strings.HasPrefix(path, "docs/")
	})
	if err != nil {
		t.Fatal(err)
	}
	cmkHolds(t, scan.Sets, []MarkerSet{{Path: "elsewhere/trap.md", Open: 3, Separator: 5, Close: 7}})
	if scan.Beyond != 0 {
		t.Errorf("counted %d sets beyond the cap in a change that adds two", scan.Beyond)
	}
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"docs/merging.md", "elsewhere/trap.md"}) {
		t.Errorf("the caller was asked about %v, want each file the change adds once", asked)
	}
}

// A repository that cannot be asked about its diff drivers is an error and
// not an empty answer.
func TestAC1ConflictMarkersARepositoryThatCannotBeReadIsAnErrorNotAnEmptyAnswer(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 40)
	_, err := ConflictMarkersOutside(t.Context(), filepath.Join(t.TempDir(), "not-there"), hash, hash, nil)
	if err == nil {
		t.Error("a root that is not there gave sets with no error")
	}
}

// ---------------------------------------------------------------- the cap and the candidates

// cmkPairs is n pairs of an opening and a closing marker with nothing between,
// a candidate each: a set only if the file has a separator there, and a file
// made of them has none.
func cmkPairs(n int) string {
	return strings.Repeat(cmkOpen("a")+"\n"+cmkClose("b")+"\n", n)
}

// cmkPatchOf is a patch that adds the named files whole, each with a blob the
// patch gives a placeholder hash for: the scan is given it without git, and
// does not read the blobs.
func cmkPatchOf(files ...[2]string) string {
	var b strings.Builder
	for _, f := range files {
		name, body := f[0], f[1]
		b.WriteString("diff --git a/" + name + " b/" + name + "\nnew file mode 100644\nindex 0000000..2222222\n--- /dev/null\n+++ b/" + name + "\n")
		b.WriteString("@@ -0,0 +1," + strconv.Itoa(strings.Count(body, "\n")) + " @@\n")
		for line := range strings.SplitSeq(strings.TrimSuffix(body, "\n"), "\n") {
			b.WriteString("+" + line + "\n")
		}
	}
	return b.String()
}

// A pair that is not yet known to be a set does not count against the cap on
// the sets. Counted at once, a file of 100000 of them used up what the scan
// keeps and a set in the next file was read past: no sets found, a landing
// passed. Security review of 2026-10-24, round 1, note 1.
func TestAC1ConflictMarkersCandidatePairsDoNotUseUpTheCapASetInAnotherFileNeeds(t *testing.T) {
	t.Parallel()
	set := cmkText(cmkSet()...)
	for _, c := range []struct {
		name        string
		pairs       int
		beyond      int
		wantPending int
	}{
		{"AC-1 exactly the cap's worth of pairs", maxMarkerSets, 0, maxMarkerSets},
		{"AC-1 one pair past what waits to be confirmed", maxMarkerSets + 1, 1, maxMarkerSets},
		{"AC-1 a few pairs", 3, 0, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, err := cmkRead(cmkPatchOf([2]string{"a.txt", cmkPairs(c.pairs)}, [2]string{"b.txt", set}))
			if err != nil {
				t.Fatal(err)
			}
			cmkHolds(t, s.out.Sets, []MarkerSet{{Path: "b.txt", Open: 1, Separator: 3, Close: 5}})
			if s.out.Beyond != c.beyond || len(s.pending) != c.wantPending {
				t.Errorf("%d pairs: counted %d beyond with %d waiting, want %d and %d",
					c.pairs, s.out.Beyond, len(s.pending), c.beyond, c.wantPending)
			}
		})
	}
}

// The same through git: a story whose file is made of nothing but pairs, a
// set beside it, and the set is found. And a file of more pairs than the scan
// can wait on is not "no sets": the scan says that it stopped short, which the
// check refuses on.
func TestAC1ConflictMarkersAFileOfCandidatePairsDoesNotHideASetInAnotherFile(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		pairs     int
		wantSets  []MarkerSet
		wantStops bool
	}{
		{"AC-1 as many pairs as the scan waits on, and a set in b.txt", maxMarkerSets,
			[]MarkerSet{{Path: "b.txt", Open: 1, Separator: 3, Close: 5}}, false},
		{"AC-1 more pairs than the scan waits on, and a set in b.txt", maxMarkerSets + 5,
			[]MarkerSet{{Path: "b.txt", Open: 1, Separator: 3, Close: 5}}, true},
		{"AC-1 more pairs than the scan waits on, and nothing else", maxMarkerSets + 1, nil, true},
		{"AC-1 as many pairs as the scan waits on, and nothing else", maxMarkerSets, nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := cmkRepo(t)
			write(t, root, "keep.txt", "a\n")
			base := lgCommit(t, root, "before")
			write(t, root, "a.txt", cmkPairs(c.pairs))
			if len(c.wantSets) > 0 {
				write(t, root, "b.txt", cmkText(cmkSet()...))
			}
			tip := lgCommit(t, root, "after")
			scan, err := ConflictMarkersOutside(t.Context(), root, base, tip, nil)
			if err != nil {
				t.Fatal(err)
			}
			cmkHolds(t, scan.Sets, c.wantSets)
			if stopped := scan.Beyond > 0; stopped != c.wantStops {
				t.Errorf("the scan reports Beyond %d, want it to say it stopped short: %v", scan.Beyond, c.wantStops)
			}
		})
	}
}
