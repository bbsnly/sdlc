package cli

// US-099 in process: a story whose commits add the markers of an unresolved
// conflict does not land. A line beginning with seven "<" and a space, a later
// line that is exactly seven "=", and a later line beginning with seven ">" and
// a space, among the lines the story's commits add to one file, with a line
// beginning with seven "|" and a space allowed between the first two, is
// refused by the commit gate and by `sdlc stop` under SDLC-E0080, naming the
// file and the line of each marker, and leaving trunk where it was.
//
// Everything here reads what a caller can see: what the commands printed, the
// exit code, git's answers about trunk, the record and the backlog. The
// refusal is matched by its code. What these hold the package to, beyond that
// code:
//
//   - the configuration key paths.conflict_markers_intended, a list of
//     patterns (config.Paths.ConflictMarkersIntended), whose paths a story
//     may add the lines in; the stop reads trunk's committed configuration
//     for it and the commit gate the worktree's (decision 5 of 2026-10-24);
//   - `sdlc doctor`'s configuration check saying, on a line of its own, the
//     key and the paths separated by commas, or "none" (decision 6);
//   - the check made on the net change a story makes from where it started or
//     from the trunk commit it sits on, with git.worktrees off as well
//     (decision 3), and not on a story trunk already holds (decision 8).
//
// The refusal is made before the reviews are asked for again, so that a story
// rebased by hand is told about its markers rather than that its reviews are
// stale; these tests record the reviews again where they mean to show that
// the markers alone are what stops the landing, and do not depend on the
// order where they do.
//
// No marker line is written out in this file: they are built at run time, so
// that this story's own commits do not hold a set for its own check to find.
//
// Helpers carry a cmk prefix, for conflict markers.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	// cmkCode is the refusal's code.
	cmkCode = "SDLC-E0080"
	// cmkKey is the setting, under paths, that lists where the lines are meant.
	cmkKey = "conflict_markers_intended"
)

func cmkOpen(label string) string  { return strings.Repeat("<", 7) + " " + label }
func cmkBase(label string) string  { return strings.Repeat("|", 7) + " " + label }
func cmkSep() string               { return strings.Repeat("=", 7) }
func cmkClose(label string) string { return strings.Repeat(">", 7) + " " + label }

func cmkText(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func cmkCRLF(text string) string { return strings.ReplaceAll(text, "\n", "\r\n") }

func cmkFiller(from, to int) []string {
	var lines []string
	for i := from; i <= to; i++ {
		lines = append(lines, "filler line "+strconv.Itoa(i))
	}
	return lines
}

func cmkJoin(parts ...[]string) []string {
	var all []string
	for _, p := range parts {
		all = append(all, p...)
	}
	return all
}

// cmkSet is the five lines a conflict leaves.
func cmkSet() []string {
	return []string{cmkOpen("HEAD"), "our side", cmkSep(), "their side", cmkClose("topic (a change)")}
}

// cmkMarkerLines is the number of every marker line in body, counted from 1:
// the lines a refusal has to name, found here by the plainest reading of them.
func cmkMarkerLines(body string) []int {
	var found []int
	for i, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, cmkOpen("")) || strings.HasPrefix(line, cmkBase("")) ||
			line == cmkSep() || strings.HasPrefix(line, cmkClose("")) {
			found = append(found, i+1)
		}
	}
	return found
}

// cmkNames reports whether out names the line n: as a number on its own, not
// the digits inside a longer number or a hash.
func cmkNames(out string, n int) bool {
	return regexp.MustCompile(`(^|[^0-9A-Za-z])` + strconv.Itoa(n) + `($|[^0-9A-Za-z])`).MatchString(out)
}

// cmkHoldsNamed holds a refusal to naming the file and the line of each marker
// of body, which is the file's content as the story committed it.
func cmkHoldsNamed(t *testing.T, out, path, body string) {
	t.Helper()
	if !wtNames(out, path) {
		t.Errorf("the refusal does not name %q:\n%s", path, out)
	}
	lines := cmkMarkerLines(body)
	if len(lines) < 3 {
		t.Fatalf("precondition: the file the test committed has %d marker lines, want a set", len(lines))
	}
	for _, n := range lines {
		if !cmkNames(out, n) {
			t.Errorf("the refusal does not name line %d of %s, a marker:\n%s", n, path, out)
		}
	}
}

// cmkHoldsFix holds the refusal's fix to what a person and an assistant are
// each told: the setting that lists where the lines are meant is a person's
// to change, and an assistant hands the story over.
func cmkHoldsFix(t *testing.T, out string) {
	t.Helper()
	_, fix := ldRefusal(t, out, cmkCode)
	for _, want := range []string{cmkKey, "sdlc escalate"} {
		if !strings.Contains(fix, want) {
			t.Errorf("the fix does not say %q:\n%s", want, fix)
		}
	}
}

// cmkGate is the commit gate asked for, in the worktree, with the session.
func cmkGate(t *testing.T, wt string) result {
	t.Helper()
	return wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed")
}

// cmkPathsOnDisk sets one key of the configuration's paths section.
func cmkPathsOnDisk(t *testing.T, root string, value any) {
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
	section, _ := cfg["paths"].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	if value == nil {
		delete(section, cmkKey)
	} else {
		section[cmkKey] = value
	}
	cfg["paths"] = section
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// cmkCommitAll commits every change in wt, as a person commits what they did.
func cmkCommitAll(t *testing.T, wt, message string) string {
	t.Helper()
	swGit(t, wt, "add", "-A")
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
	return swRev(t, wt, "HEAD")
}

// cmkLate is a finished story with its bookkeeper's change committed and a
// commit of its own after the reviews, adding files, and the reviews recorded
// again on the change as it now is; so that the markers are the one thing in
// the way of the landing. It returns trunk as it was.
func cmkLate(t *testing.T, files map[string]string) (main, wt, trunk string) {
	t.Helper()
	main, wt = ldWalked(t, ldWalk{finished: true, bookkept: true})
	for rel, body := range files {
		writeFile(t, wt, rel, body)
	}
	cmkCommitAll(t, wt, "late: files after the reviews")
	ldReviewAgain(t, wt)
	return main, wt, ldTrunk(t, main)
}

// ---------------------------------------------------------------- the stop

// AC-1: the stop refuses a story that added a set after its gates, names the
// file and the line of each marker, and leaves trunk and the story where they
// were; put right, it lands.
func TestAC1TheStopRefusesAStoryWhoseCommitsAddAConflictsMarkersAndNamesEachLine(t *testing.T) {
	t.Parallel()
	const file = "notes ledger.txt"
	for _, c := range []struct{ name, body string }{
		{"AC-1 the markers git writes", cmkText(cmkJoin(cmkFiller(1, 36), cmkSet(), cmkFiller(37, 40))...)},
		{"AC-1 with the base side between, as diff3 writes it",
			cmkText(cmkJoin(cmkFiller(1, 36), []string{cmkOpen("HEAD"), "our side", cmkBase("parent of 1a2b3c4 (x)"), "was", cmkSep(), "their side", cmkClose("1a2b3c4 (x)")}, cmkFiller(37, 40))...)},
		{"AC-1 in a file with CRLF line endings (decision 1)", cmkCRLF(cmkText(cmkJoin(cmkFiller(1, 36), cmkSet(), cmkFiller(37, 40))...))},
		{"AC-1 with the sides of the conflict split by lines the story did not add (decision 2)",
			cmkText(cmkJoin(cmkFiller(1, 5), []string{cmkOpen("HEAD")}, cmkFiller(6, 30), []string{cmkSep()}, cmkFiller(31, 40), []string{cmkClose("topic")})...)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			main, wt, trunk := cmkLate(t, map[string]string{file: c.body})

			out := swRefused(t, cmkCode, wt, ldSession, "stop")
			cmkHoldsNamed(t, out, file, c.body)
			cmkHoldsFix(t, out)
			if got := ldTrunk(t, main); got != trunk {
				t.Errorf("the refused stop moved trunk to %s", got)
			}
			ldStillHeld(t, main, wt)

			// Put right, with the reviews of the change as it now is, the same
			// story lands: the markers were what stopped it.
			swCommitFile(t, wt, file, cmkText(cmkFiller(1, 40)...), "put the file right")
			ldReviewAgain(t, wt)
			ldLanded(t, main, wt, swRev(t, wt, "HEAD"))
		})
	}
}

// AC-1 and AC-4: a person rebases the story by hand onto a trunk that moved,
// git stops on a conflict, and the person adds the file as it stands, markers
// and all, and carries the rebase on. The stop refuses it under its own code
// and not as stale reviews or a story behind trunk, names the file and every
// marker's line as the file now has them, and trunk stays where it was; and
// once the person has resolved the file, the story lands.
func TestAC1AndAC4AStoryRebasedByHandWithTheConflictLeftHalfResolvedIsRefusedByTheStop(t *testing.T) {
	t.Parallel()
	const file = "ledger.txt"
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	common := cmkFiller(1, 36)
	swCommitFile(t, wt, file, cmkText(append(append([]string{}, common...), "the story's line")...), "late: the ledger")
	trunk := swCommitFile(t, main, file, cmkText(append(append([]string{}, common...), "trunk's line")...), "trunk: the ledger")

	if _, ok := swGitTry(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "main"); ok {
		t.Fatal("precondition: the rebase did not stop on a conflict")
	}
	held := wtRead(t, filepath.Join(wt, file))
	if len(cmkMarkerLines(held)) < 3 {
		t.Fatalf("precondition: git left no set in %s:\n%s", file, held)
	}
	swGit(t, wt, "add", "--", file)
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "-c", "core.editor=true", "rebase", "--continue")
	ldReviewAgain(t, wt)

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	cmkHoldsNamed(t, out, file, held)
	cmkHoldsFix(t, out)
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("the refused stop moved trunk to %s", got)
	}
	ldStillHeld(t, main, wt)

	// The person resolves the file, commits it and records the reviews again.
	tip := swCommitFile(t, wt, file, cmkText(append(append([]string{}, common...), "the resolved line")...), "resolve the ledger")
	ldReviewAgain(t, wt)
	ldLanded(t, main, wt, tip)
}

// AC-1 and AC-4, the stop's own replay: trunk moved, the story's commits do
// not conflict with it, and a set was added by the story itself. The check is
// made on the tree the stop would land, however the stop got the story there.
func TestAC1TheStopRefusesASetOnTheTreeItsOwnReplayMakes(t *testing.T) {
	t.Parallel()
	const file = "notes.txt"
	body := cmkText(cmkJoin(cmkFiller(1, 10), cmkSet())...)
	main, wt, _ := cmkLate(t, map[string]string{file: body})
	ldMoveTrunk(t, main)
	trunk := ldTrunk(t, main)

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	cmkHoldsNamed(t, out, file, body)
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("the refused stop moved trunk to %s", got)
	}
	ldStillHeld(t, main, wt)
}

// AC-1 and decision 7: CODEMAP.md is read like any other file. The root one is
// left out of the fingerprint of a change, which is how the bookkeeper's file
// lands without a review, and is not left out of this.
func TestAC1TheStopRefusesASetInTheBookkeepersCODEMAP(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	body := cmkText(cmkJoin([]string{"# CODEMAP", ""}, cmkSet())...)
	swCommitFile(t, wt, "CODEMAP.md", body, "docs: map US-001")
	trunk := ldTrunk(t, main)

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	cmkHoldsNamed(t, out, "CODEMAP.md", body)
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("the refused stop moved trunk to %s", got)
	}

	tip := swCommitFile(t, wt, "CODEMAP.md", "# CODEMAP\n\nbilling.go keeps invoices.\n", "docs: map US-001, put right")
	ldLanded(t, main, wt, tip)
}

// AC-1 and decision 1: a file's own line endings are what they are; CRLF in a
// rebase conflict means the separator is the seven characters and a carriage
// return, and is named all the same.
func TestAC1TheStopRefusesAConflictInAFileWithCRLFLineEndingsLeftByAHandRebase(t *testing.T) {
	t.Parallel()
	const file = "crlf.txt"
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	common := cmkFiller(1, 12)
	swGit(t, wt, "config", "core.autocrlf", "false")
	swGit(t, main, "config", "core.autocrlf", "false")
	swCommitFile(t, main, file, cmkCRLF(cmkText(append(append([]string{}, common...), "the base line")...)), "trunk: crlf, before the conflict")
	ldRebase(t, wt)
	swCommitFile(t, wt, file, cmkCRLF(cmkText(append(append([]string{}, common...), "the story's line")...)), "late: crlf")
	trunk := swCommitFile(t, main, file, cmkCRLF(cmkText(append(append([]string{}, common...), "trunk's line")...)), "trunk: crlf")

	if _, ok := swGitTry(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "main"); ok {
		t.Fatal("precondition: the rebase did not stop on a conflict")
	}
	held := wtRead(t, filepath.Join(wt, file))
	if !strings.Contains(held, cmkSep()+"\r\n") {
		t.Fatalf("precondition: git did not write the separator with a carriage return:\n%q", held)
	}
	swGit(t, wt, "add", "--", file)
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "-c", "core.editor=true", "rebase", "--continue")
	ldReviewAgain(t, wt)

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	cmkHoldsNamed(t, out, file, held)
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("the refused stop moved trunk to %s", got)
	}
}

// AC-1 and THREATS D2: the refusal is bounded however many sets the story
// adds, and names the first of them.
func TestAC1TheRefusalIsBoundedHoweverManySetsTheStoryAdds(t *testing.T) {
	t.Parallel()
	lines := cmkFiller(1, 10)
	for range 300 {
		lines = append(lines, cmkSet()...)
	}
	body := cmkText(lines...)
	main, wt, _ := cmkLate(t, map[string]string{"many.txt": body})

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	for _, n := range []int{11, 13, 15} {
		if !cmkNames(out, n) {
			t.Errorf("the refusal does not name line %d, the first set's:\n%s", n, out)
		}
	}
	if !wtNames(out, "many.txt") {
		t.Errorf("the refusal does not name many.txt")
	}
	if cmkNames(out, 1510) {
		t.Errorf("the refusal names the last marker of 1200, so it grows with the story's change")
	}
	if len(out) > 12000 {
		t.Errorf("the refusal is %d bytes for one file", len(out))
	}
	ldStillHeld(t, main, wt)
}

// AC-1: only a set is a conflict. A story that adds ordinary text that has
// the look of one part of it is refused for the file that holds a set, and the
// others are not named; taken out, the story lands with them.
func TestAC1TheStopNamesTheFileWithASetAndNotTheFilesThatHaveOnlyTheLookOfOne(t *testing.T) {
	t.Parallel()
	withASet := cmkText(cmkJoin(cmkFiller(1, 3), cmkSet())...)
	main, wt, _ := cmkLate(t, map[string]string{
		"real.txt":           withASet,
		"README-draft.md":    cmkText("Title", cmkSep(), "", "Text under the title.", "", "Another", cmkSep()),
		"docs/indented.md":   cmkText("    "+cmkOpen("HEAD"), "    ours", "    "+cmkSep(), "    theirs", "    "+cmkClose("topic")),
		"partial.txt":        cmkText(cmkOpen("HEAD"), "ours", cmkSep(), "theirs"),
		"out-of-order.txt":   cmkText(cmkClose("topic"), cmkSep(), cmkOpen("HEAD")),
		"spread-a.txt":       cmkText(cmkOpen("HEAD"), "ours"),
		"spread-b.txt":       cmkText(cmkSep(), "theirs"),
		"spread-c.txt":       cmkText(cmkClose("topic")),
		"bare-markers.txt":   cmkText(strings.Repeat("<", 7), "ours", cmkSep(), "theirs", strings.Repeat(">", 7)),
		"longer-markers.txt": cmkText(strings.Repeat("<", 10)+" HEAD", "ours", strings.Repeat("=", 10), "theirs", strings.Repeat(">", 10)+" topic"),
	})

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	cmkHoldsNamed(t, out, "real.txt", withASet)
	for _, other := range []string{"README-draft.md", "indented.md", "partial.txt", "out-of-order.txt", "spread-a.txt", "spread-b.txt", "spread-c.txt", "bare-markers.txt", "longer-markers.txt"} {
		if strings.Contains(out, other) {
			t.Errorf("the refusal names %s, which holds no set:\n%s", other, out)
		}
	}

	if err := os.Remove(filepath.Join(wt, "real.txt")); err != nil {
		t.Fatal(err)
	}
	cmkCommitAll(t, wt, "take the conflict out")
	ldReviewAgain(t, wt)
	ldLanded(t, main, wt, swRev(t, wt, "HEAD"))
}

// ---------------------------------------------------------------- AC-2

// AC-2: a file that already holds a set on trunk, where the story adds none,
// lands as it does today; and a story that adds a second set to it is refused
// for that one, by its own lines.
func TestAC2AFileThatAlreadyHoldsASetOnTrunkLandsAndASecondSetInItIsRefusedByItsOwnLines(t *testing.T) {
	t.Parallel()
	const file = "notes.txt"
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 24))
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	swCommitFile(t, main, file, cmkText(held...), "trunk: a file a person left a conflict in")
	ldRebase(t, wt)

	added := cmkText(cmkJoin(held, []string{"tail one", "tail two"}, cmkSet())...)
	swCommitFile(t, wt, file, added, "a second conflict")
	ldReviewAgain(t, wt)
	trunk := ldTrunk(t, main)

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	for _, n := range []int{32, 34, 36} {
		if !cmkNames(out, n) {
			t.Errorf("the refusal does not name line %d, a marker the story added:\n%s", n, out)
		}
	}
	for _, n := range []int{21, 23, 25} {
		if cmkNames(out, n) {
			t.Errorf("the refusal names line %d, a marker that was on trunk before the story:\n%s", n, out)
		}
	}
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("the refused stop moved trunk to %s", got)
	}

	// The second set taken out and an unrelated line edited, the file still
	// holds the one that was there, and the story lands.
	edited := slices.Clone(held)
	edited[2] = "an unrelated line, edited"
	tip := swCommitFile(t, wt, file, cmkText(edited...), "edit a line, and take the second conflict out")
	ldReviewAgain(t, wt)
	ldLanded(t, main, wt, tip)
}

// AC-2 and decision 4: a story that moves a file that holds a set does not add
// it, and one that adds another to the moved file is refused for that one.
func TestAC2AndDecision4AMovedFileThatHoldsASetAddsNoneButASecondInItIsRefused(t *testing.T) {
	t.Parallel()
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 40))
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	swCommitFile(t, main, "notes.txt", cmkText(held...), "trunk: a file a person left a conflict in")
	ldRebase(t, wt)

	if err := os.MkdirAll(filepath.Join(wt, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	swGit(t, wt, "mv", "notes.txt", "archive/notes.txt")
	writeFile(t, wt, "archive/notes.txt", cmkText(cmkJoin(held, cmkSet())...))
	cmkCommitAll(t, wt, "move the file, and add a conflict to it")
	ldReviewAgain(t, wt)

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	if !wtNames(out, "archive/notes.txt") {
		t.Errorf("the refusal does not name archive/notes.txt:\n%s", out)
	}
	for _, n := range []int{46, 48, 50} {
		if !cmkNames(out, n) {
			t.Errorf("the refusal does not name line %d, a marker the story added:\n%s", n, out)
		}
	}
	for _, n := range []int{21, 23, 25} {
		if cmkNames(out, n) {
			t.Errorf("the refusal names line %d, a marker that moved with the file and was not added:\n%s", n, out)
		}
	}

	// Only the move: the file's own set came with it and the story lands.
	tip := swCommitFile(t, wt, "archive/notes.txt", cmkText(held...), "take the second conflict out")
	ldReviewAgain(t, wt)
	ldLanded(t, main, wt, tip)
}

// ---------------------------------------------------------------- AC-3

// AC-3: a path the project's configuration lists is one where a story may add
// the lines. The stop reads trunk's committed configuration, so a listing a
// person committed to trunk after the story's worktree was cut is honoured,
// and the story lands.
func TestAC3TheStopHonoursAnExemptionOnTrunkThatTheStorysWorktreeNeverHad(t *testing.T) {
	t.Parallel()
	const file = "docs/merging.md"
	body := cmkText(cmkJoin([]string{"# Merging", ""}, cmkSet())...)
	main, wt, _ := cmkLate(t, map[string]string{file: body})
	// The stop puts the story's commits onto trunk itself here, because trunk
	// moved, and a rebase needs a committer. The tests' git has no identity of
	// its own (testenv), so a runner without one refused it: "Committer
	// identity unknown" on ubuntu-24.04-arm. The other tests that reach that
	// replay give the repository one the same way.
	otIdentity(t, main)

	swRefused(t, cmkCode, wt, ldSession, "stop")

	cmkPathsOnDisk(t, main, []string{"docs/**"})
	suCommit(t, main, "a person lists where conflict markers are meant", ".sdlc/config.json")
	got := decode[stopPayload](t, wtMust(t, wt, ldSession, "", "stop", "--json"))
	if !got.OK || got.Story != "US-001" || !got.Done {
		t.Errorf("stop --json = %+v, want US-001 done", got)
	}
	if out := swGit(t, main, "show", "main:"+file); !strings.Contains(out, cmkOpen("")[:7]) {
		t.Errorf("trunk does not hold %s as the story wrote it:\n%s", file, out)
	}
}

// AC-3 and THREATS T8: the listing in the story's own worktree does not count
// at the stop. Trunk listed the path when the story was cut, so the worktree's
// configuration does; trunk stopped listing it, and the stop reads trunk's.
func TestAC3AnExemptionOnlyTheStorysWorktreeHasDoesNotSurviveTheStop(t *testing.T) {
	t.Parallel()
	const file = "docs/merging.md"
	body := cmkText(cmkJoin([]string{"# Merging", ""}, cmkSet())...)
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	cmkPathsOnDisk(t, main, []string{"docs/**"})
	suCommit(t, main, "a person lists where conflict markers are meant", ".sdlc/config.json")
	ldRebase(t, wt)
	if got := wtRead(t, filepath.Join(wt, ".sdlc", "config.json")); !strings.Contains(got, cmkKey) {
		t.Fatalf("precondition: the worktree's configuration does not list the path:\n%s", got)
	}
	swCommitFile(t, wt, file, body, "late: a document about conflicts")
	ldReviewAgain(t, wt)
	cmkPathsOnDisk(t, main, nil)
	suCommit(t, main, "a person takes the listing off again", ".sdlc/config.json")
	trunk := ldTrunk(t, main)

	out := swRefused(t, cmkCode, wt, ldSession, "stop")
	cmkHoldsNamed(t, out, file, body)
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("the refused stop moved trunk to %s", got)
	}
	ldStillHeld(t, main, wt)
}

// AC-3: the commit gate reads the worktree's configuration. A story that adds
// the lines in a listed path is not refused for them, and one that adds them
// anywhere else -- the other case of a listed directory's name and the start
// of another name included -- is, and is told which file and no other. Each
// story is refused with the unlisted file in it and passes once it is out.
func TestAC3TheCommitGateLetsAStoryAddTheLinesInAListedPathAndNowhereElse(t *testing.T) {
	t.Parallel()
	body := cmkText(cmkJoin([]string{"# Merging", ""}, cmkSet())...)
	for _, c := range []struct {
		name             string
		listed, unlisted []string
	}{
		{"AC-3 a file under a listed directory, at any depth", []string{"docs/merging.md", "docs/a/b/more.md"}, []string{"elsewhere/trap.md"}},
		{"AC-3 a listed file", []string{"notes/merging.md"}, []string{"elsewhere/trap.md"}},
		{"AC-3 a file beside a listed file", nil, []string{"notes/other.md"}},
		{"AC-3 a directory of the same name in another case", nil, []string{"Docs/merging.md"}},
		{"AC-3 a listed directory's name as the start of another", nil, []string{"docs2/merging.md"}},
		{"AC-3 a listed file and an unlisted one together", []string{"docs/merging.md"}, []string{"src/merging.md"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			main := ldRepository(t, func(root string) {
				cmkPathsOnDisk(t, root, []string{"docs/**", "notes/merging.md"})
			})
			wt := ldStarted(t, main)
			ldWork(t, wt)
			for _, rel := range append(slices.Clone(c.listed), c.unlisted...) {
				writeFile(t, wt, rel, body)
			}
			swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
			satisfy(t, wt, model.GateCommit)

			out := swRefused(t, cmkCode, wt, ldSession, "gate", "commit", "pass", "--note", "committed")
			for _, rel := range c.unlisted {
				cmkHoldsNamed(t, out, rel, body)
			}
			for _, rel := range c.listed {
				if strings.Contains(out, rel) {
					t.Errorf("the refusal names %s, which is listed:\n%s", rel, out)
				}
			}
			if got := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json")).Gates["commit"]; got == "pass" {
				t.Error("the refused commit gate is recorded as passed")
			}

			for _, rel := range c.unlisted {
				if err := os.Remove(filepath.Join(wt, filepath.FromSlash(rel))); err != nil {
					t.Fatal(err)
				}
			}
			tip := cmkCommitAll(t, wt, "take the conflict markers out of the files they are not meant in")
			ldReviewAgain(t, wt)
			if r := cmkGate(t, wt); r.code != 0 {
				t.Fatalf("the commit gate refused a set in a listed path:\n%s%s", r.stdout, r.stderr)
			}
			if rec := swRecord(t, main, "US-001"); rec.Commit != tip {
				t.Errorf("the record names %s as the story's last commit, want %s", rec.Commit, tip)
			}
		})
	}
}

// AC-3: a story that adds the lines in a listed path lands through the commit
// gate and the stop when trunk lists the path too; with the lines in a path
// that is not listed beside them it does not, and lands once they are out.
func TestAC3AStoryThatAddsTheLinesInAListedPathLandsThroughTheCommitGateAndTheStop(t *testing.T) {
	t.Parallel()
	body := cmkText(cmkJoin([]string{"# Merging", ""}, cmkSet())...)
	main := ldRepository(t, func(root string) { cmkPathsOnDisk(t, root, []string{"docs/**"}) })
	wt := ldStarted(t, main)
	ldWork(t, wt)
	writeFile(t, wt, "docs/merging.md", body)
	writeFile(t, wt, "elsewhere/trap.md", body)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	satisfy(t, wt, model.GateCommit)
	swRefused(t, cmkCode, wt, ldSession, "gate", "commit", "pass", "--note", "committed")

	if err := os.Remove(filepath.Join(wt, "elsewhere", "trap.md")); err != nil {
		t.Fatal(err)
	}
	cmkCommitAll(t, wt, "take the trap out")
	ldReviewAgain(t, wt)
	wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed")
	swPass(t, wt, ldSession, model.GateRetro, "")
	tip := ldBookkeeping(t, wt)
	ldLanded(t, main, wt, tip)
	if out := swGit(t, main, "show", "main:docs/merging.md"); !strings.Contains(out, "Merging") {
		t.Errorf("trunk does not hold docs/merging.md:\n%s", out)
	}
}

// AC-3: doctor lists the paths on the configuration check, or says none.
func TestAC3DoctorListsThePathsWhereConflictMarkersAreIntendedOrSaysNone(t *testing.T) {
	t.Parallel()
	root := project(t)
	mustRun(t, "init")
	line := func() (check, string) {
		t.Helper()
		for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
			if c.Name != "configuration" {
				continue
			}
			for l := range strings.SplitSeq(c.Detail, "\n") {
				if strings.Contains(l, cmkKey) {
					return c, l
				}
			}
			t.Fatalf("the configuration check has no line about %s:\n%s", cmkKey, c.Detail)
		}
		t.Fatal("doctor ran no configuration check")
		return check{}, ""
	}

	c, l := line()
	if c.State != stateOK {
		t.Fatalf("the configuration sdlc init wrote is a problem: %s (%s)", c.Detail, c.Fix)
	}
	if !strings.Contains(l, "none") {
		t.Errorf("with no path listed the line is %q, want it to say none", l)
	}
	if r := run(t, "doctor"); !strings.Contains(r.stdout, cmkKey) {
		t.Errorf("sdlc doctor, as text, does not mention %s:\n%s", cmkKey, r.stdout)
	}

	cmkPathsOnDisk(t, root, []string{"docs/**", "testdata/*.diff", "notes/merging.md"})
	c, l = line()
	if c.State != stateOK {
		t.Errorf("a configuration that lists paths is a problem: %s (%s)", c.Detail, c.Fix)
	}
	for _, want := range []string{"docs/**", "testdata/*.diff", "notes/merging.md"} {
		if !strings.Contains(l, want) {
			t.Errorf("the line %q does not list %s", l, want)
		}
	}
	if strings.Contains(l, "none") {
		t.Errorf("the line %q says none where paths are listed", l)
	}
	if r := run(t, "doctor"); !strings.Contains(r.stdout, "testdata/*.diff") {
		t.Errorf("sdlc doctor, as text, does not list the paths:\n%s", r.stdout)
	}

	// A list that cannot mean what it says is the configuration's problem,
	// named by its setting.
	cmkPathsOnDisk(t, root, []string{"../docs/**"})
	for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
		if c.Name == "configuration" && (c.State != stateProblem || !strings.Contains(c.Detail, "paths."+cmkKey)) {
			t.Errorf("a path that leaves the repository was not named: %s %q", c.State, c.Detail)
		}
	}
}

// ---------------------------------------------------------------- decisions 3 and 8

// Decision 3 and THREATS T12: with story worktrees off nothing lands through
// the stop, so the commit gate is the only look, and it measures from where
// the story started: a set added in the first of a story's commits is
// refused, and one taken out again by a later commit is not there to refuse.
func TestAC1AndDecision3WithStoryWorktreesOffTheCommitGateRefusesASetAddedInTheFirstOfTheStorysCommits(t *testing.T) {
	t.Parallel()
	const file = "notes.txt"
	body := cmkText(cmkJoin(cmkFiller(1, 8), cmkSet())...)
	root := wtRepository(t, wtStory{id: "US-001"})
	wtStarted(t, root, ldSession)
	swReach(t, root, ldSession, model.GateImplementation)
	// Both files are there when the reviewers read the tree; the story commits
	// them in two commits at the commit gate, the markers in the first.
	writeFile(t, root, file, body)
	writeFile(t, root, "later.txt", "a later, clean file\n")
	swPass(t, root, ldSession, model.GateImplementation, model.GateCommit)
	swGit(t, root, "add", "--", file)
	swGit(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "first: notes")
	satisfy(t, root, model.GateCommit)

	out := swRefused(t, cmkCode, root, ldSession, "gate", "commit", "pass", "--note", "committed")
	cmkHoldsNamed(t, out, file, body)
	if got := decode[statusPayload](t, wtMust(t, root, ldSession, "", "status", "--json")).Gates["commit"]; got == "pass" {
		t.Error("the refused commit gate is recorded as passed")
	}

	// The set taken out by a later commit is not in the story's change.
	swCommitFile(t, root, file, cmkText(cmkFiller(1, 8)...), "put the notes right")
	ldReviewAgain(t, root)
	// With worktrees off the reviews are written into the one checkout, and
	// the commit gate wants the loop's own files committed before it looks.
	cmkCommitAll(t, root, "record the reviews again")
	r := cmkGate(t, root)
	if r.code != 0 {
		t.Fatalf("the commit gate refused a story whose change adds no set:\n%s%s", r.stdout, r.stderr)
	}
	if rec := swRecord(t, root, "US-001"); rec.Commit != swRev(t, root, "HEAD") {
		t.Errorf("the record names %s as the story's last commit, want HEAD", rec.Commit)
	}
}

// The commit gate in a story's own worktree: refused with the set named, not
// recorded, and passed once the file is put right; the net change is what is
// looked at, so a set added by one commit and taken out by the next is not
// there.
func TestAC1TheCommitGateRefusesAStoryThatAddsASetAndRecordsNothingUntilItIsPutRight(t *testing.T) {
	t.Parallel()
	const file = "notes ledger.txt"
	for _, c := range []struct{ name, body string }{
		{"AC-1 the markers git writes", cmkText(cmkJoin(cmkFiller(1, 36), cmkSet(), cmkFiller(37, 40))...)},
		{"AC-1 with the base side between", cmkText(cmkJoin(cmkFiller(1, 6), []string{cmkOpen("HEAD"), "ours", cmkBase("parent of 1a2b3c4 (x)"), "was", cmkSep(), "theirs", cmkClose("1a2b3c4 (x)")})...)},
		{"AC-1 in a file with CRLF line endings (decision 1)", cmkCRLF(cmkText(cmkJoin(cmkFiller(1, 6), cmkSet())...))},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			main, wt := ldWalked(t, ldWalk{})
			ldWork(t, wt)
			writeFile(t, wt, file, c.body)
			swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
			satisfy(t, wt, model.GateCommit)

			out := swRefused(t, cmkCode, wt, ldSession, "gate", "commit", "pass", "--note", "committed")
			cmkHoldsNamed(t, out, file, c.body)
			cmkHoldsFix(t, out)
			if got := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json")).Gates["commit"]; got == "pass" {
				t.Error("the refused commit gate is recorded as passed")
			}
			if rec := swRecord(t, main, "US-001"); rec.Commit != "" {
				t.Errorf("the refused commit gate recorded the range up to %s", rec.Commit)
			}

			writeFile(t, wt, file, cmkText(cmkFiller(1, 40)...))
			tip := cmkCommitAll(t, wt, "put the file right")
			ldReviewAgain(t, wt)
			r := cmkGate(t, wt)
			if r.code != 0 {
				t.Fatalf("the commit gate refused the file put right:\n%s%s", r.stdout, r.stderr)
			}
			if rec := swRecord(t, main, "US-001"); rec.Commit != tip {
				t.Errorf("the record names %s as the story's last commit, want %s", rec.Commit, tip)
			}
		})
	}
}

// Decision 8: a story trunk already holds -- a person brought it there by hand
// -- has nothing left to move, and the stop records it and does not refuse it
// for what is on trunk already. The same story was refused before that.
func TestAC1AndDecision8AStoryThatTrunkAlreadyHoldsIsNotRefusedForItsMarkers(t *testing.T) {
	t.Parallel()
	body := cmkText(cmkJoin(cmkFiller(1, 3), cmkSet())...)
	main, wt, _ := cmkLate(t, map[string]string{"notes.txt": body})
	swRefused(t, cmkCode, wt, ldSession, "stop")

	tip := swRev(t, wt, "HEAD")
	swGit(t, main, "merge", "--quiet", "--ff-only", "story/US-001")
	ldLanded(t, main, wt, tip)
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s, want done", got)
	}
}

// ---------------------------------------------------------------- the wording, without a repository

// A change that cannot be read is an error and not a pass: the look at a
// change is not skipped because git could not give it.
func TestAC1ARangeTheCheckCannotReadIsAnErrorAndNotAPass(t *testing.T) {
	t.Parallel()
	err := requireNoConflictMarkers(t.Context(), t.TempDir(), "HEAD", "HEAD", config.Paths{}, inPlace())
	if err == nil {
		t.Fatal("a range named by branch names, which the check cannot read, passed")
	}
	var e *sdlcerr.Error
	if errors.As(err, &e) && e.Code == sdlcerr.ConflictMarkersAdded {
		t.Errorf("a range that could not be read was refused as if it held a conflict: %v", err)
	}
}

// AC-1: the refusal names a bounded number of files, and says how many it left
// out, so that a story that adds a conflict to every file in a tree still gets
// a message a person reads; and a file is named quoted, so that a name with a
// line break in it cannot forge a line of the message.
func TestAC1TheRefusalNamesFiveFilesAndCountsTheRestAndQuotesWhatItNames(t *testing.T) {
	t.Parallel()
	var sets []gitx.MarkerSet
	for i := 1; i <= 7; i++ {
		sets = append(sets, gitx.MarkerSet{Path: "f" + strconv.Itoa(i) + ".txt", Open: 3, Separator: 5, Close: 7})
	}
	sets = append(sets, gitx.MarkerSet{Path: "a\nforged: SDLC-E0001.txt", Open: 1, Separator: 2, Close: 3})
	slices.SortFunc(sets, func(a, b gitx.MarkerSet) int { return strings.Compare(a.Path, b.Path) })
	got := describeMarkerSets(gitx.MarkerScan{Sets: sets})

	for _, want := range []string{`"a\nforged: SDLC-E0001.txt" (conflict 1: lines 1, 2, 3)`, `"f1.txt"`, `"f2.txt"`, `"f3.txt"`, `"f4.txt"`, "; and 3 more files"} {
		if !strings.Contains(got, want) {
			t.Errorf("the description does not say %s:\n%s", want, got)
		}
	}
	for _, left := range []string{`"f5.txt"`, `"f6.txt"`, `"f7.txt"`} {
		if strings.Contains(got, left) {
			t.Errorf("the description names %s, past the five files it shows:\n%s", left, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("the description holds a line break, which a file name could have put there:\n%q", got)
	}
}

// AC-1: a scan that read past its cap says that there are more conflicts than
// it names, and that the figure is a lower bound.
func TestAC1TheRefusalSaysHowManyConflictsTheScanReadPastItsCap(t *testing.T) {
	t.Parallel()
	got := describeMarkerSets(gitx.MarkerScan{
		Sets:   []gitx.MarkerSet{{Path: "notes.txt", Open: 2, Base: 4, Separator: 6, Close: 8}},
		Beyond: 12,
	})
	for _, want := range []string{`"notes.txt" (conflict 1: lines 2, 4, 6, 8)`, "and at least 12 more conflicts"} {
		if !strings.Contains(got, want) {
			t.Errorf("the description does not say %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "more files") {
		t.Errorf("the description counts files left out where only one file was found:\n%s", got)
	}
}

// AC-1: a scan that stopped short with no set to name is worded without a
// file, and says that no file is named.
func TestAC1TheRefusalForAScanThatStoppedShortWithNoSetNamesNoFile(t *testing.T) {
	t.Parallel()
	got := describeMarkerSets(gitx.MarkerScan{Beyond: 7})
	for _, want := range []string{"at least 7", "no file is named"} {
		if !strings.Contains(got, want) {
			t.Errorf("the description does not say %q:\n%s", want, got)
		}
	}
}

// cmkPairs is n pairs of an opening marker and a closing one with nothing
// between: each is a set only if the file has a separator there, and a file
// made of them has none.
func cmkPairs(n int) string {
	return strings.Repeat(cmkOpen("a")+"\n"+cmkClose("b")+"\n", n)
}

// maxCandidates is how many such pairs the scan can wait on, which is
// gitx's cap on the sets it keeps (maxMarkerSets, 100000): written out here
// because it is not exported, and a change to it is a change to a documented
// limit that this test is to be changed with.
const maxCandidates = 100000

// AC-1: a scan that stopped short is not one that found nothing. A story that
// adds more pairs than the scan can wait on, and so no set it can name, is
// refused, with the wording that names no file; one with as many as the scan
// can wait on, none of them a set, is not. Security review of 2026-10-24,
// round 1, note 1: the check passed when there were no sets and ignored
// Beyond, so a file of pairs hid a real set in another file.
func TestAC1ARefusalIsMadeWhenTheScanStoppedShortWithNoSetToName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		pairs    int
		refused  bool
		andASet  bool
		wantNote string
	}{
		{"AC-1 more pairs than the scan waits on", maxCandidates + 1, true, false, "at least 1 "},
		{"AC-1 more pairs than the scan waits on, and a set in another file", maxCandidates + 3, true, true, "b.txt"},
		{"AC-1 as many pairs as the scan waits on, none a set", maxCandidates, false, false, ""},
		{"AC-1 as many pairs as the scan waits on, and a set in another file", maxCandidates, true, true, "b.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := wtRepository(t, wtStory{id: "US-001"})
			base := swRev(t, root, "HEAD")
			writeFile(t, root, "a.txt", cmkPairs(c.pairs))
			if c.andASet {
				writeFile(t, root, "b.txt", cmkText(cmkSet()...))
			}
			tip := cmkCommitAll(t, root, "add the files")

			err := requireNoConflictMarkers(t.Context(), root, base, tip, config.Paths{}, inPlace())
			if !c.refused {
				if err != nil {
					t.Fatalf("a change with no set in it was refused: %v", err)
				}
				return
			}
			var e *sdlcerr.Error
			if !errors.As(err, &e) || e.Code != sdlcerr.ConflictMarkersAdded {
				t.Fatalf("the check answered %v, want the refusal under %s", err, sdlcerr.ConflictMarkersAdded)
			}
			if !strings.Contains(e.What, c.wantNote) {
				t.Errorf("the refusal does not say %q:\n%s", c.wantNote, e.What)
			}
			if !c.andASet && !strings.Contains(e.What, "no file is named") {
				t.Errorf("the refusal for a scan that found no set names a file, or does not say that none is named:\n%s", e.What)
			}
		})
	}
}

// cmkStory is a story with git.worktrees off, as the commit gate sees it:
// base is where it began, head the commit that adds a set to notes.txt after
// it, and g the gate run requireNoMarkersFrom is made on.
func cmkStory(t *testing.T) (g *gateRun, base, head string) {
	t.Helper()
	root := wtRepository(t, wtStory{id: "US-001"})
	base = swRev(t, root, "HEAD")
	head = swCommitFile(t, root, "notes.txt", cmkText(cmkJoin(cmkFiller(1, 4), cmkSet())...), "add the notes")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return &gateRun{s: store.New(&config.Project{Root: root, Config: cfg}), id: "US-001"}, base, head
}

// cmkRefusedForNotes holds err to the refusal under SDLC-E0080 that names the
// file the story added the set to.
func cmkRefusedForNotes(t *testing.T, err error) {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.ConflictMarkersAdded || !strings.Contains(e.What, `"notes.txt"`) {
		t.Errorf("the check answered %v, want the refusal under %s that names notes.txt", err, sdlcerr.ConflictMarkersAdded)
	}
}

// Decision 3: with git.worktrees off the commit gate is the only look. Where
// the record's CommitBase is empty or git cannot read it, the check measures
// from where the record says the story began, and finds a set added after
// it, and does not pass on a record it could not use. Code review of
// 2026-10-24, round 1, the blocking finding.
func TestAC1AndDecision3WithStoryWorktreesOffACommitBaseThatIsEmptyOrUnreadableFallsBackToWhereTheStoryBegan(t *testing.T) {
	t.Parallel()
	g, began, head := cmkStory(t)
	for _, started := range []string{
		"",
		strings.Repeat("f", 40),
		"deadbee",
		"-x",
		"--all",
	} {
		t.Run("AC-1 the commit base "+strconv.Quote(started), func(t *testing.T) {
			t.Parallel()
			cmkRefusedForNotes(t, g.requireNoMarkersFrom(t.Context(), started, began, head))
		})
	}
}

// The commit base is the first choice: where it is readable it is the start of
// the range, whatever the record says the story began at.
func TestAC1AndDecision3WithStoryWorktreesOffAReadableCommitBaseIsUsedBeforeWhereTheStoryBegan(t *testing.T) {
	t.Parallel()
	g, began, head := cmkStory(t)
	if err := g.requireNoMarkersFrom(t.Context(), head, began, head); err != nil {
		t.Errorf("the check measured from where the story began and not from the commit base, which is the tip: %v", err)
	}
	cmkRefusedForNotes(t, g.requireNoMarkersFrom(t.Context(), began, head, head))
	// A start the record gives that git cannot read is not asked about at all
	// where the commit base is readable.
	if err := g.requireNoMarkersFrom(t.Context(), head, strings.Repeat("f", 40), head); err != nil {
		t.Errorf("a readable commit base was not enough: %v", err)
	}
}

// Where the record names no start git can read, there is nothing to measure
// from and the check is skipped, as the gate has never refused over it.
func TestAC1AndDecision3WithStoryWorktreesOffAStoryWithNoStartGitCanReadIsNotLookedAt(t *testing.T) {
	t.Parallel()
	g, _, head := cmkStory(t)
	unreadable := strings.Repeat("f", 40)
	for _, c := range []struct{ name, started, began string }{
		{"AC-1 both empty", "", ""},
		{"AC-1 both unreadable", unreadable, "-x"},
		{"AC-1 one empty and one unreadable", "", unreadable},
		{"AC-1 one unreadable and one empty", "deadbee", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if err := g.requireNoMarkersFrom(t.Context(), c.started, c.began, head); err != nil {
				t.Errorf("a record with no start to measure from was refused: %v", err)
			}
		})
	}
}

// cmkKilled is a context whose Done channel stays open and whose Err is
// cancelled from the first call: what a context looks like to the code
// reading git's exit after the context killed it. os/exec reports a killed git
// as an exit error, which Resolve reads as a name that is no commit, so the
// check has to ask the context itself.
type cmkKilled struct{ done <-chan struct{} }

func (cmkKilled) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c cmkKilled) Done() <-chan struct{}     { return c.done }
func (cmkKilled) Err() error                  { return context.Canceled }
func (cmkKilled) Value(any) any               { return nil }

// A context that has ended is its own error, never "the record names no
// commit": the gate does not pass on a look it could not make, however the
// start reads.
func TestAC1AndDecision3WithStoryWorktreesOffAContextThatEndedIsItsOwnErrorAndNeverAPass(t *testing.T) {
	t.Parallel()
	g, began, head := cmkStory(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := g.requireNoMarkersFrom(cancelled, began, began, head); !errors.Is(err, context.Canceled) {
		t.Errorf("the check answered %v for a context cancelled before the call, want the context's error", err)
	}
}

// The same where the context ended while git ran, which is what a killed git
// looks like to the code that reads its exit.
func TestAC1AndDecision3WithStoryWorktreesOffAGitTheContextKilledIsTheContextsErrorAndNotANameThatIsNoCommit(t *testing.T) {
	t.Parallel()
	g, began, head := cmkStory(t)
	unreadable := strings.Repeat("f", 40)
	for _, c := range []struct{ name, started, began string }{
		{"AC-1 a start to fall back to", unreadable, began},
		{"AC-1 no start to fall back to", unreadable, ""},
		{"AC-1 the fallback unreadable too", unreadable, "-x"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := g.requireNoMarkersFrom(cmkKilled{t.Context().Done()}, c.started, c.began, head)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("the check answered %v for a context that ended while git ran, want the context's error", err)
			}
		})
	}
}
