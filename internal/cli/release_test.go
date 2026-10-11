package cli

// US-029.1, `sdlc release` and how a held story reads. The expectations are
// the plan's steps 6, 7, 8, 10 and 12 and the story's D3 and D4: release is a
// person's, needs a reason, is refused while an iteration runs on the story
// anywhere, and moves nothing but the claim; a tie is refused to every
// contender and settled by `sdlc stop`; a gone holder and a tie read as such.

import (
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// rlClaimFile is where the plan keeps the claim on a story: in the git
// directory every checkout of the repository shares.
func rlClaimFile(main, id string) string {
	return filepath.Join(main, ".git", "sdlc", "claims", id+".json")
}

// rlFixLine is the fix line of a rendered refusal.
func rlFixLine(out string) string {
	for line := range strings.Lines(out) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "fix"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// rlHeld is a repository whose worktree started US-001 and was then stopped by
// a person, so the worktree holds the story with no iteration running on it.
func rlHeld(t *testing.T) string {
	t.Helper()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := wtWorktree(t, main, "wt")
	if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-001" {
		t.Fatalf("start in the worktree gave %q", got.Story)
	}
	wtMust(t, wt, "", "", "stop")
	if !wtExists(rlClaimFile(main, "US-001")) {
		t.Fatal("precondition: stop let go of the claim on an unfinished story (D3)")
	}
	return main
}

func TestAC8ReleaseWithoutAReasonIsRefusedAndChangesNothing(t *testing.T) {
	t.Parallel()
	main := rlHeld(t)
	before := wtLoopState(t, main)
	for _, args := range [][]string{
		{"release", "US-001"},
		{"release", "US-001", "--reason", "   "},
	} {
		r := wtRun(t, main, "", "", args...)
		if r.code == 0 || !strings.Contains(r.stderr, sdlcerr.ReasonRequired.String()) {
			t.Errorf("sdlc %s exited %d, want %s:\n%s", strings.Join(args, " "), r.code, sdlcerr.ReasonRequired, r.stderr)
		}
	}
	if !wtExists(rlClaimFile(main, "US-001")) {
		t.Error("a release refused for want of a reason let go of the claim")
	}
	if !maps.Equal(before, wtLoopState(t, main)) {
		t.Error("a release refused for want of a reason changed the main checkout's loop state")
	}
}

func TestAC8ReleasingAStoryNobodyHoldsSaysSo(t *testing.T) {
	t.Parallel()
	main := rlHeld(t)
	r := wtMust(t, main, "", "", "release", "US-002", "--reason", "tidying up")
	if !strings.Contains(r.stdout, "Nothing to release") || !strings.Contains(r.stdout, strconv.Quote("US-002")) {
		t.Errorf("releasing a story nobody holds said:\n%s", r.stdout)
	}
	payload := decode[releasePayload](t, wtMust(t, main, "", "", "release", "US-002", "--reason", "tidying up", "--json"))
	if !payload.OK || payload.Story != "US-002" || payload.Checkout != "" {
		t.Errorf("release --json of a story nobody holds = %+v", payload)
	}
	for _, args := range [][]string{
		{"release", "../US-001", "--reason", "x"},
		{"release", "US-404", "--reason", "x"},
	} {
		if r := wtRun(t, main, "", "", args...); r.code == 0 {
			t.Errorf("sdlc %s went through:\n%s", strings.Join(args, " "), r.stdout)
		}
	}
}

// Step 10: a story an iteration is running on is ended where it runs first.
// The refusal names the checkout in its what and why, and its fix names none
// (T9).
func TestAC8ReleaseIsRefusedWhileAnIterationRunsOnTheStory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setUp func(t *testing.T, main, wt string)
		named []string
	}{
		{"AC-8 an iteration running in the worktree", func(t *testing.T, _, wt string) {
			t.Helper()
			wtStarted(t, wt, wtSessionWorktree)
		}, []string{"wt"}},
		{"AC-8 an iteration held by its active alone", func(t *testing.T, main, wt string) {
			t.Helper()
			wtSetStatus(t, main, "US-001", model.StatusInProgress)
			writeFile(t, wt, ".sdlc/state/active", "US-001\n")
		}, []string{"wt"}},
		{"AC-8 two checkouts naming the story", func(t *testing.T, main, wt string) {
			t.Helper()
			wtSetStatus(t, main, "US-001", model.StatusInProgress)
			writeFile(t, wt, ".sdlc/state/active", "US-001\n")
			writeFile(t, main, ".sdlc/state/active", "US-001\n")
		}, []string{"wt", "main"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			wt := wtWorktree(t, main, "wt")
			tc.setUp(t, main, wt)
			claimed := wtExists(rlClaimFile(main, "US-001"))

			r := wtRun(t, main, "", "", "release", "US-001", "--reason", "moving it")
			if r.code == 0 || !strings.Contains(r.stderr, sdlcerr.IterationAlreadyActive.String()) {
				t.Fatalf("release exited %d, want %s:\n%s%s", r.code, sdlcerr.IterationAlreadyActive, r.stdout, r.stderr)
			}
			for _, name := range tc.named {
				dir := map[string]string{"wt": wt, "main": main}[name]
				if !strings.Contains(r.stderr, strconv.Quote(filepath.ToSlash(dir))) {
					t.Errorf("the refusal does not name %s:\n%s", dir, r.stderr)
				}
			}
			fix := rlFixLine(r.stderr)
			if !strings.Contains(fix, `"sdlc stop"`) || strings.Contains(fix, wt) || strings.Contains(fix, main) {
				t.Errorf("the fix does not send a person to sdlc stop, or names a path: %q", fix)
			}
			if wtExists(rlClaimFile(main, "US-001")) != claimed {
				t.Error("a refused release changed the claim")
			}
		})
	}
}

// D3 and step 10: a person moves a story a live checkout put down. The claim
// goes, the record says why and names no checkout (C21), the output says what
// stayed behind and that the freeze does not move (C15), and start then picks
// the story up elsewhere, saying so.
func TestAC8ReleaseLetsAStoryALiveCheckoutPutDownBeStartedElsewhere(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, wt, wtSessionWorktree)
	reach(t, wt, model.GatePlan)
	wtMust(t, wt, "", "", "stop")

	out := wtMust(t, main, "", "", "release", "US-001", "--reason", "the person moves it").stdout
	for _, want := range []string{"Released US-001", strconv.Quote(filepath.ToSlash(wt)), "not committed", "sdlc unfreeze", "sdlc freeze"} {
		if !strings.Contains(out, want) {
			t.Errorf("release does not say %q:\n%s", want, out)
		}
	}
	if wtExists(rlClaimFile(main, "US-001")) {
		t.Error("the claim is still there after the release")
	}
	var record model.Record
	wtReadJSON(t, wtRecordPath(main, "US-001"), &record)
	last := record.Events[len(record.Events)-1]
	// The reason and nothing else: no checkout's path reaches the record.
	if last.Type != "release" || last.Message != "the person moves it" {
		t.Errorf("the record ends with %+v, want the release and the person's reason", last)
	}

	r := wtRun(t, main, wtSessionMain, "", "start", "--json")
	if got := decode[startPayload](t, r); got.Story != "US-001" {
		t.Errorf("start in the main checkout after the release gave %q, want US-001", got.Story)
	}
	if !strings.Contains(r.stderr, "was released from the checkout that held it") {
		t.Errorf("start does not say the story was released and its uncommitted work stayed behind:\n%s", r.stderr)
	}
}

// AC-8: a gone checkout's claim is let go, with nothing said about work left
// in a checkout that is not there.
func TestAC8ReleasingTheClaimOfAGoneCheckoutNamesItAndNothingLeftThere(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	gone1, gone2 := wtWorktree(t, main, "gone1"), wtWorktree(t, main, "gone2")
	wtStarted(t, gone1, wtSessionWorktree, "US-001")
	wtStarted(t, gone2, wtSessionThird, "US-002")
	chdir(t, main)
	for _, dir := range []string{gone1, gone2} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}

	payload := decode[releasePayload](t, wtMust(t, main, "", "", "release", "US-001", "--reason", "deleted", "--json"))
	if !payload.OK || payload.Story != "US-001" || payload.Checkout != gone1 {
		t.Errorf("release --json = %+v, want US-001 let go from %s", payload, gone1)
	}
	out := wtMust(t, main, "", "", "release", "US-002", "--reason", "deleted").stdout
	if !strings.Contains(out, "Released US-002") || !strings.Contains(out, strconv.Quote(filepath.ToSlash(gone2))) || strings.Contains(out, "not committed") {
		t.Errorf("releasing a gone checkout's story said:\n%s", out)
	}
}

// Step 12: a gone holder and a tie read as what they are, wherever holders are
// shown.
func TestAC6AGoneHolderAndATieReadAsWhatTheyAre(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"}, wtStory{id: "US-003"})
	gone, wt := wtWorktree(t, main, "gone"), wtWorktree(t, main, "wt")
	wtStarted(t, gone, wtSessionWorktree, "US-001")
	chdir(t, main)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	wtSetStatus(t, main, "US-002", model.StatusInProgress)
	writeFile(t, main, ".sdlc/state/active", "US-002\n")
	writeFile(t, wt, ".sdlc/state/active", "US-002\n")

	for _, args := range [][]string{{"status"}, {"story", "list"}, {"doctor"}} {
		r := wtRun(t, main, "", "", args...)
		text := r.stdout + r.stderr
		for _, want := range []string{
			strconv.Quote(filepath.ToSlash(gone)), "gone: see sdlc release",
			"more than one checkout names it", strconv.Quote(filepath.ToSlash(main)), strconv.Quote(filepath.ToSlash(wt)),
		} {
			if !strings.Contains(text, want) {
				t.Errorf("sdlc %s does not say %q:\n%s", strings.Join(args, " "), want, text)
			}
		}
	}
}

// Step 8: stop in a checkout whose active names a story another checkout holds
// puts this checkout's copy down, says so, and writes nothing to the story's
// record or its claim.
func TestAC3StopPutsDownAStaleCopyOfAStoryHeldElsewhereAndWritesNothing(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, main, wtSessionMain)
	writeFile(t, wt, ".sdlc/state/active", "US-001\n")
	record, claim := wtRead(t, wtRecordPath(main, "US-001")), wtRead(t, rlClaimFile(main, "US-001"))

	r := wtMust(t, wt, "", "", "stop")
	if !strings.Contains(r.stderr, "held by another checkout") || !strings.Contains(r.stderr, "writes nothing to its record") {
		t.Errorf("stop does not say the story is held elsewhere and nothing is recorded:\n%s", r.stderr)
	}
	if wtExists(filepath.Join(wt, ".sdlc", "state", "active")) {
		t.Error("stop left the worktree's active")
	}
	if wtRead(t, wtRecordPath(main, "US-001")) != record || wtRead(t, rlClaimFile(main, "US-001")) != claim {
		t.Error("stop in the worktree changed the record or the claim of a story the main checkout holds")
	}
}

// D4 and step 8: a tie is refused to each contender, naming the others, and a
// person settles it with `sdlc stop` in all but one, which writes nothing to
// the story's record; the one left then carries on.
func TestD4ATieIsRefusedToEachContenderAndSettledByStop(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := wtWorktree(t, main, "wt")
	wtSetStatus(t, main, "US-001", model.StatusInProgress)
	writeFile(t, main, ".sdlc/state/active", "US-001\n")
	writeFile(t, main, ".sdlc/state/session", wtSessionMain+"\n")
	writeFile(t, wt, ".sdlc/state/active", "US-001\n")
	writeFile(t, wt, ".sdlc/state/session", wtSessionWorktree+"\n")

	for dir, session := range map[string]string{main: wtSessionMain, wt: wtSessionWorktree} {
		out := wtRefused(t, wtHeldElsewhere, dir, session, "", "start")
		for _, want := range []string{strconv.Quote(filepath.ToSlash(main)), strconv.Quote(filepath.ToSlash(wt)), wtSessionMain, wtSessionWorktree} {
			if !strings.Contains(out, want) {
				t.Errorf("the tie's refusal in %s does not name %s:\n%s", dir, want, out)
			}
		}
		if fix := rlFixLine(out); !strings.Contains(fix, `"sdlc stop"`) || strings.Contains(fix, main) || strings.Contains(fix, wt) {
			t.Errorf("the tie's fix is %q", fix)
		}
	}

	wtMust(t, main, "", "", "stop")
	if wtExists(filepath.Join(main, ".sdlc", "state", "active")) {
		t.Error("stop left the main checkout's active")
	}
	if wtExists(wtRecordPath(main, "US-001")) {
		t.Error("stop on a tied story wrote the story's record")
	}
	if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-001" || !got.Resume {
		t.Errorf("with the tie settled, start in the worktree gave %+v, want US-001 carried on", got)
	}
}
