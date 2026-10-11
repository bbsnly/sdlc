package store

// US-029.1, the plan's steps 4 and 5: the backlog and the records are the main
// checkout's, the claim on a story is kept in the git directory every checkout
// shares and names only the story and the checkout (C21), who holds a story is
// worked out in one place, and a tie adopts nobody. The expectations are the
// story's decisions D3, D4 and D6, and THREATS.md's T4 and T5.

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func clTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

func clWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func clStore(root string) *Store {
	return New(&config.Project{Root: root, Config: config.Default()}).WithClock(func() time.Time { return fixedTime })
}

// clBacklog writes the main checkout's backlog: US-001 to US-006 at the
// statuses given, and ready otherwise.
func clBacklog(t *testing.T, main string, statuses map[string]model.Status) {
	t.Helper()
	var stories []string
	for i := 1; i <= 6; i++ {
		id := "US-00" + strconv.Itoa(i)
		status, ok := statuses[id]
		if !ok {
			status = model.StatusReady
		}
		stories = append(stories, `{"id":"`+id+`","title":"Story `+id+`","status":"`+string(status)+`"}`)
	}
	clWrite(t, filepath.Join(main, "user_stories.json"), `{"stories":[`+strings.Join(stories, ",")+"]}\n")
}

// clRepository is a main checkout with its backlog committed, and a linked
// worktree named wt.
func clRepository(t *testing.T, statuses map[string]model.Status) (main, wt string) {
	t.Helper()
	main = clTempDir(t)
	initGit(t, main)
	clBacklog(t, main, statuses)
	commitAll(t, main, "the project")
	return main, clWorktree(t, main, "wt")
}

func clWorktree(t *testing.T, main, name string) string {
	t.Helper()
	dir := filepath.Join(clTempDir(t), name)
	runGit(t, main, "worktree", "add", "--quiet", "-b", name, dir)
	return dir
}

// clState sets a checkout's own loop state, as `sdlc start` leaves it.
func clState(t *testing.T, dir, active, session string) {
	t.Helper()
	clWrite(t, filepath.Join(dir, ".sdlc", "state", "active"), active+"\n")
	if session != "" {
		clWrite(t, filepath.Join(dir, ".sdlc", "state", "session"), session+"\n")
	}
}

func clClaimFile(main, id string) string {
	return filepath.Join(main, ".git", "sdlc", "claims", id+".json")
}

func clHolders(t *testing.T, s *Store) map[string]Holder {
	t.Helper()
	holders, err := s.Holders()
	if err != nil {
		t.Fatalf("Holders: %v", err)
	}
	return holders
}

func clError(t *testing.T, err error, code sdlcerr.Code) *sdlcerr.Error {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error %v, want %s", err, code)
	}
	return e
}

func clExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Step 5, C5, C6, C21, T8: the claim is in the common git directory, where git
// never shows, commits or pushes it, and says which story and which checkout,
// and nothing else.
func TestAClaimIsKeptWhereGitNeverShowsItAndNamesOnlyTheStoryAndTheCheckout(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
	s := clStore(wt)
	if err := s.Claim("US-001"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(clClaimFile(main, "US-001"))
	if err != nil {
		t.Fatalf("no claim in the common git directory: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(fields)); !slices.Equal(got, []string{"checkout", "schema", "story"}) {
		t.Errorf("the claim holds %v, want the schema, the story and the checkout only", got)
	}
	checkout, _ := fields["checkout"].(string)
	if fields["schema"] != "sdlc/claim/1" || fields["story"] != "US-001" || !filepath.IsAbs(checkout) || !config.SamePath(checkout, wt) {
		t.Errorf("the claim is %s, want sdlc/claim/1 on US-001 naming %s", raw, wt)
	}
	for _, dir := range []string{main, wt} {
		cmd := exec.CommandContext(t.Context(), "git", "status", "--porcelain", "--untracked-files=all")
		cmd.Dir = dir
		if out, err := cmd.Output(); err != nil || len(out) != 0 {
			t.Errorf("git status in %s shows %q (%v) after the claim", dir, out, err)
		}
	}

	if err := s.Claim("US-001"); err != nil {
		t.Errorf("claiming a story this checkout holds again: %v", err)
	}
	if again, _ := os.ReadFile(clClaimFile(main, "US-001")); string(again) != string(raw) {
		t.Errorf("claiming again rewrote the claim:\n%s", again)
	}

	if h := clHolders(t, s)["US-001"]; !config.SamePath(h.Checkout, wt) || !h.Here || !h.Live || !h.Claimed || h.Tied() {
		t.Errorf("from the worktree, US-001's holder is %+v", h)
	}
	if _, elsewhere, err := s.HeldElsewhere("US-001"); err != nil || elsewhere {
		t.Errorf("HeldElsewhere in the holding checkout = %v, %v", elsewhere, err)
	}
	from := clStore(main)
	if h := clHolders(t, from)["US-001"]; h.Here || !config.SamePath(h.Checkout, wt) {
		t.Errorf("from the main checkout, US-001's holder is %+v", h)
	}
	if h, elsewhere, err := from.HeldElsewhere("US-001"); err != nil || !elsewhere || !config.SamePath(h.Checkout, wt) {
		t.Errorf("HeldElsewhere in the main checkout = %+v, %v, %v", h, elsewhere, err)
	}
}

// T5: a claim is read back only when its file name, schema and story agree and
// its checkout is an absolute path; anything else says nothing reliable about
// whose state to read.
func TestT5AClaimFileThatDoesNotReadIsNoClaim(t *testing.T) {
	statuses := map[string]model.Status{}
	for i := 1; i <= 6; i++ {
		statuses["US-00"+strconv.Itoa(i)] = model.StatusInProgress
	}
	main, wt := clRepository(t, statuses)
	claim := func(schema, story, checkout string) string {
		return `{"schema":"` + schema + `","story":"` + story + `","checkout":` + strconv.Quote(checkout) + "}\n"
	}
	clWrite(t, clClaimFile(main, "US-001"), claim("sdlc/claim/2", "US-001", wt))
	clWrite(t, clClaimFile(main, "US-002"), claim("sdlc/claim/1", "US-003", wt))
	clWrite(t, clClaimFile(main, "US-003"), claim("sdlc/claim/1", "US-003", "wt"))
	clWrite(t, clClaimFile(main, "US-004"), "not json\n")
	clWrite(t, filepath.Join(main, ".git", "sdlc", "claims", "-US-006.json"), claim("sdlc/claim/1", "-US-006", wt))
	clWrite(t, filepath.Join(main, ".git", "sdlc", "claims", "US-006.txt"), claim("sdlc/claim/1", "US-006", wt))
	clWrite(t, clClaimFile(main, "US-005"), claim("sdlc/claim/1", "US-005", wt))

	holders := clHolders(t, clStore(main))
	if got := slices.Sorted(maps.Keys(holders)); !slices.Equal(got, []string{"US-005"}) {
		t.Errorf("stories held: %v, want only US-005, whose claim reads", got)
	}
}

// D3: a claim lasts until the story is done or dropped, or leaves the backlog;
// the first writing command then sweeps it away, and writes the claim for an
// iteration held by its active alone.
func TestD3AClaimOnAFinishedStoryHoldsNothingAndIsTidiedAway(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{
		"US-001": model.StatusInProgress, "US-002": model.StatusInProgress, "US-004": model.StatusInProgress,
	})
	s := clStore(wt)
	for _, id := range []string{"US-001", "US-002", "US-003"} {
		clWrite(t, clClaimFile(main, id), `{"schema":"sdlc/claim/1","story":"`+id+`","checkout":`+strconv.Quote(wt)+"}\n")
	}
	clBacklog(t, main, map[string]model.Status{
		"US-001": model.StatusDone, "US-002": model.StatusDropped, "US-003": model.StatusInProgress,
		"US-004": model.StatusInProgress,
	})
	// US-003 is claimed and under way; US-004 is this checkout's by its active.
	clState(t, wt, "US-004", "")

	holders := clHolders(t, s)
	for _, id := range []string{"US-001", "US-002"} {
		if h, ok := holders[id]; ok {
			t.Errorf("%s is finished and still held, by %+v", id, h)
		}
	}
	s.TidyClaims()
	for id, want := range map[string]bool{"US-001": false, "US-002": false, "US-003": true, "US-004": true} {
		if got := clExists(clClaimFile(main, id)); got != want {
			t.Errorf("after TidyClaims the claim on %s exists: %v, want %v", id, got, want)
		}
	}
	if h := clHolders(t, clStore(main))["US-004"]; !h.Claimed || !config.SamePath(h.Checkout, wt) {
		t.Errorf("the worktree's adopted US-004 is held as %+v, want claimed by %s", h, wt)
	}
}

// Housekeeping fails soft: a backlog that will not read is the next command's
// to report, not the tidying's.
func TestTidyClaimsLeavesEverythingAsItWasWhenItCannotRead(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusDone})
	clWrite(t, clClaimFile(main, "US-001"), `{"schema":"sdlc/claim/1","story":"US-001","checkout":`+strconv.Quote(wt)+"}\n")
	clWrite(t, filepath.Join(main, "user_stories.json"), "{ not a backlog")
	clStore(wt).TidyClaims()
	if !clExists(clClaimFile(main, "US-001")) {
		t.Error("TidyClaims removed a claim it could not check against the backlog")
	}
}

// D4: an active nobody's claim covers holds its story while the backlog has
// the story in progress or waiting for a person, and not otherwise.
func TestD4AnActiveAloneHoldsAStoryOnlyWhileTheBacklogHasItUnderWay(t *testing.T) {
	for _, tc := range []struct {
		status model.Status
		held   bool
	}{
		{model.StatusInProgress, true},
		{model.StatusAwaitingHuman, true},
		{model.StatusReady, false},
		{model.StatusBlocked, false},
		{model.StatusDone, false},
	} {
		t.Run("D4 "+string(tc.status), func(t *testing.T) {
			main, wt := clRepository(t, map[string]model.Status{"US-001": tc.status})
			clState(t, wt, "US-001", "sess-wt")
			h, held := clHolders(t, clStore(main))["US-001"]
			switch {
			case held != tc.held:
				t.Errorf("US-001 %s with the worktree's active naming it: held %v (%+v), want %v", tc.status, held, h, tc.held)
			case held && (h.Claimed || !h.Live || h.Here || !config.SamePath(h.Checkout, wt)):
				t.Errorf("the holder is %+v, want the worktree, live and unclaimed", h)
			}
		})
	}
}

// D4, red-team R1-3 and R2-6: two checkouts naming one unclaimed story is a
// tie, which adopts nobody, is refused to each, and names each contender with
// what a person needs to tell them apart. The fix is fixed text (T9).
func TestD4TwoCheckoutsNamingOneUnclaimedStoryTieAndNeitherHoldsIt(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
	clState(t, main, "US-001", "sess-main")
	clState(t, wt, "US-001", "")
	written := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(main, ".sdlc", "state", "active"), written, written); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{main, wt} {
		s := clStore(dir)
		h, elsewhere, err := s.HeldElsewhere("US-001")
		if err != nil || !elsewhere || !h.Tied() || h.Checkout != "" {
			t.Fatalf("from %s HeldElsewhere = %+v, %v, %v; want a tie nobody holds", dir, h, elsewhere, err)
		}
		if len(h.Contenders) != 2 || !slices.ContainsFunc(h.Contenders, func(c Contender) bool {
			return config.SamePath(c.Checkout, main) && c.Session == "sess-main" && c.ActiveWritten.Equal(written)
		}) || !slices.ContainsFunc(h.Contenders, func(c Contender) bool {
			return config.SamePath(c.Checkout, wt) && c.Session == ""
		}) {
			t.Errorf("the contenders are %+v", h.Contenders)
		}
		if err := s.KeepClaim("US-001"); err != nil {
			t.Fatal(err)
		}
		if clExists(clClaimFile(main, "US-001")) {
			t.Fatalf("keeping the claim from %s claimed a tied story", dir)
		}

		e := clError(t, HeldElsewhereError("US-001", h, "a reason"), sdlcerr.StoryHeldElsewhere)
		for _, want := range []string{strconv.Quote(filepath.ToSlash(main)), strconv.Quote(filepath.ToSlash(wt)), "sess-main", "2026-01-02"} {
			if !strings.Contains(e.Why, want) {
				t.Errorf("the tie's why does not name %s: %s", want, e.Why)
			}
		}
		if !strings.Contains(e.Fix, `"sdlc stop"`) || !strings.Contains(e.Fix, "an assistant stops and tells the person") {
			t.Errorf("the tie's fix does not say what a person does and what an assistant does: %s", e.Fix)
		}
		if strings.Contains(e.Fix, main) || strings.Contains(e.Fix, wt) {
			t.Errorf("the tie's fix names a path: %s", e.Fix)
		}
	}
}

// AC-8 and T4: a holder that is not a checkout of this repository any more,
// gone or never one, is not live, and its story is refused with SDLC-E0051,
// naming the path and not in the fix.
func TestAC8AClaimWhoseCheckoutIsNotACheckoutOfThisRepositoryIsNotLive(t *testing.T) {
	stranger := clTempDir(t)
	initGit(t, stranger)
	for _, tc := range []struct {
		name  string
		named func(t *testing.T, wt string) string
	}{
		{"AC-8 a worktree removed from disk", func(t *testing.T, wt string) string {
			t.Helper()
			if err := os.RemoveAll(wt); err != nil {
				t.Fatal(err)
			}
			return wt
		}},
		{"AC-8 a directory of another repository", func(*testing.T, string) string { return stranger }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
			named := tc.named(t, wt)
			clWrite(t, clClaimFile(main, "US-001"), `{"schema":"sdlc/claim/1","story":"US-001","checkout":`+strconv.Quote(named)+"}\n")

			h, elsewhere, err := clStore(main).HeldElsewhere("US-001")
			if err != nil || !elsewhere || h.Live || !h.Claimed {
				t.Fatalf("HeldElsewhere = %+v, %v, %v; want a claimed holder that is not live", h, elsewhere, err)
			}
			e := clError(t, HeldElsewhereError("US-001", h, "a reason"), sdlcerr.HoldingCheckoutMissing)
			if !strings.Contains(e.What+e.Why, strconv.Quote(filepath.ToSlash(named))) {
				t.Errorf("the refusal does not name %s: %s / %s", named, e.What, e.Why)
			}
			if strings.Contains(e.Fix, named) || !strings.Contains(e.Fix, "sdlc release") {
				t.Errorf("the fix names the path or not the release command: %s", e.Fix)
			}
		})
	}
}

// Step 6: a live holder elsewhere is refused with SDLC-E0050, naming the
// checkout and why the command is that checkout's to run.
func TestAC3AStoryALiveCheckoutHoldsElsewhereIsRefusedNamingItAndTheReason(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
	if err := clStore(wt).Claim("US-001"); err != nil {
		t.Fatal(err)
	}
	h, _, err := clStore(main).HeldElsewhere("US-001")
	if err != nil {
		t.Fatal(err)
	}
	e := clError(t, HeldElsewhereError("US-001", h, "what it records is measured on that checkout's tree"),
		sdlcerr.StoryHeldElsewhere)
	if !strings.Contains(e.What, "US-001") || !strings.Contains(e.Why, strconv.Quote(filepath.ToSlash(wt))) ||
		!strings.Contains(e.Why, "measured on that checkout's tree") {
		t.Errorf("the refusal is %q / %q", e.What, e.Why)
	}
	if strings.Contains(e.Fix, wt) {
		t.Errorf("the fix names a path: %s", e.Fix)
	}
}

func TestReleaseClaimLetsTheStoryGoAndReleasingNothingIsNotAnError(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
	s := clStore(main)
	if err := clStore(wt).Claim("US-001"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseClaim("US-001"); err != nil {
		t.Fatal(err)
	}
	if clExists(clClaimFile(main, "US-001")) {
		t.Error("the claim is still there")
	}
	if _, held := clHolders(t, s)["US-001"]; held {
		t.Error("US-001 is still held")
	}
	if err := s.ReleaseClaim("US-001"); err != nil {
		t.Errorf("releasing a story nobody claims: %v", err)
	}
	for _, id := range []string{"../US-001", ""} {
		if err := s.ReleaseClaim(id); err == nil {
			t.Errorf("ReleaseClaim(%q) went through", id)
		}
		if err := s.Claim(id); err == nil {
			t.Errorf("Claim(%q) went through", id)
		}
	}
}

// Step 5: a start that cannot write its claim fails; it is not soft.
func TestAClaimThatCannotBeWrittenOrRemovedIsTheCommandsFailure(t *testing.T) {
	skipIfPermissionsDoNotBind(t)
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress, "US-002": model.StatusInProgress})
	s := clStore(wt)
	if err := s.Claim("US-002"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(clClaimFile(main, "US-001"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0o600); err == nil {
		t.Skip("this filesystem does not deny a write to a read-only directory")
	}
	clError(t, s.Claim("US-001"), sdlcerr.StateUnwritable)
	clError(t, s.ReleaseClaim("US-002"), sdlcerr.StateUnwritable)
}

// A session is held where its checkout's session file names it and that
// checkout holds the story its active names (architect R1-1). The checkouts
// claims name are all read, whatever the limit on the others (R2-B1).
func TestSessionHolderIsTheCheckoutHoldingTheStoryItsActiveNames(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{
		"US-001": model.StatusInProgress, "US-002": model.StatusInProgress,
	})
	first := clWorktree(t, main, "a-first")
	s := clStore(main)
	if err := clStore(wt).Claim("US-001"); err != nil {
		t.Fatal(err)
	}
	clState(t, wt, "US-001", "sess-wt")

	for _, limit := range []int{0, 1} {
		if dir, story := s.SessionHolder("sess-wt", limit); !config.SamePath(dir, wt) || story != "US-001" {
			t.Errorf("SessionHolder(sess-wt, %d) = %q, %q; want %s holding US-001", limit, dir, story, wt)
		}
	}
	for _, session := range []string{"sess-other", "", "not a session!"} {
		if dir, story := s.SessionHolder(session, 0); dir != "" || story != "" {
			t.Errorf("SessionHolder(%q) = %q, %q", session, dir, story)
		}
	}
	if dir, _ := clStore(wt).SessionHolder("sess-wt", 0); dir != "" {
		t.Errorf("from the holding checkout itself, the session is held elsewhere, in %s", dir)
	}

	// A checkout whose session file names the session, but whose active names
	// a story another checkout holds, holds nothing.
	clState(t, first, "US-001", "sess-copied")
	if dir, _ := s.SessionHolder("sess-copied", 0); dir != "" {
		t.Errorf("a worktree with a copied session and another checkout's story holds it, in %s", dir)
	}

	// A contender in a tie is where its session is held.
	clState(t, main, "US-002", "sess-main")
	clState(t, first, "US-002", "sess-first")
	if dir, story := clStore(wt).SessionHolder("sess-first", 0); !config.SamePath(dir, first) || story != "US-002" {
		t.Errorf("SessionHolder(sess-first) = %q, %q; want the contender %s on US-002", dir, story, first)
	}
}

// C13 and AC-10: holders are named where the repository has another checkout,
// or a claim names one that is not the main checkout, and not otherwise.
func TestShowsHoldersOnlyWhereThereIsAnotherCheckout(t *testing.T) {
	single := clTempDir(t)
	initGit(t, single)
	clBacklog(t, single, map[string]model.Status{"US-001": model.StatusInProgress})
	clState(t, single, "US-001", "sess")
	s := clStore(single)
	if s.ShowsHolders(clHolders(t, s)) || s.HasLinkedWorktree() {
		t.Error("a single checkout names the checkout holding its story")
	}

	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress, "US-002": model.StatusDone})
	s = clStore(main)
	if !s.ShowsHolders(clHolders(t, s)) || !s.HasLinkedWorktree() {
		t.Error("a repository with a linked worktree does not name holders")
	}

	if err := clStore(wt).Claim("US-001"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "worktree", "prune")
	if s.HasLinkedWorktree() {
		t.Fatal("the pruned worktree is still listed")
	}
	if !s.ShowsHolders(clHolders(t, s)) {
		t.Error("a claim naming a gone worktree is not shown once the worktree is pruned")
	}
	if err := s.ReleaseClaim("US-001"); err != nil {
		t.Fatal(err)
	}
	clWrite(t, clClaimFile(main, "US-002"), `{"schema":"sdlc/claim/1","story":"US-002","checkout":`+strconv.Quote(wt)+"}\n")
	if s.ShowsHolders(clHolders(t, s)) {
		t.Error("a claim on a done story makes holders shown")
	}
}

// D3: escalating keeps the claim, so the story's uncommitted work is not handed
// to another checkout; and a story another checkout holds is handed to a person
// from that checkout.
func TestD3EscalatingKeepsTheClaimAndIsRefusedForAStoryHeldElsewhere(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
	clState(t, wt, "US-001", "sess-wt")

	if _, err := clStore(wt).Escalate(t.Context(), "US-001", "spec_unclear", "which greeting?"); err != nil {
		t.Fatal(err)
	}
	if h := clHolders(t, clStore(main))["US-001"]; !h.Claimed || !config.SamePath(h.Checkout, wt) {
		t.Errorf("after escalating, US-001 is held as %+v, want claimed by %s", h, wt)
	}
	_, err := clStore(main).Escalate(t.Context(), "US-001", "spec_unclear", "again")
	clError(t, err, sdlcerr.StoryHeldElsewhere)
}

// Step 4, AC-1, AC-4 and D6: a worktree's backlog, records and lock are the
// main checkout's; with no main checkout every shared path refuses with
// SDLC-E0052, and the lock falls back to the checkout itself.
func TestAWorktreesSharedPathsAndLockAreTheMainCheckouts(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
	s := clStore(wt)
	if err := s.SharedErr(); err != nil {
		t.Fatal(err)
	}
	dir, err := s.StoryDir("US-001")
	if err != nil {
		t.Fatal(err)
	}
	for got, want := range map[string]string{
		s.BacklogPath():             filepath.Join(main, "user_stories.json"),
		s.LockRoot():                main,
		clStore(main).LockRoot():    main,
		dir:                         filepath.Join(main, ".sdlc", "stories", "US-001"),
		s.CheckoutPath():            wt,
		clStore(main).BacklogPath(): filepath.Join(main, "user_stories.json"),
	} {
		if !config.SamePath(got, want) {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	if co, err := s.Checkout(); err != nil || !config.SamePath(co.Main, main) {
		t.Errorf("Checkout() = %+v, %v", co, err)
	}

	src := clTempDir(t)
	initGit(t, src)
	clBacklog(t, src, nil)
	commitAll(t, src, "the project")
	bare := filepath.Join(clTempDir(t), "repo.git")
	runGit(t, src, "clone", "--quiet", "--bare", src, bare)
	bwt := clWorktree(t, bare, "bwt")
	b := clStore(bwt)
	clError(t, b.SharedErr(), sdlcerr.NoMainCheckout)
	_, err = b.StoryDir("US-001")
	clError(t, err, sdlcerr.NoMainCheckout)
	_, err = b.Backlog()
	clError(t, err, sdlcerr.NoMainCheckout)
	_, err = b.Record("US-001")
	clError(t, err, sdlcerr.NoMainCheckout)
	if b.LockRoot() != bwt || b.BacklogPath() != filepath.Join(bwt, "user_stories.json") {
		t.Errorf("with no main checkout the lock is on %s and the backlog %s", b.LockRoot(), b.BacklogPath())
	}
}

// perf R1-1: the hook finds the checkout once, and the store it opens uses that
// answer rather than finding it again.
func TestNewInUsesTheCheckoutItIsGiven(t *testing.T) {
	_, wt := clRepository(t, nil)
	given := errors.New("the hook could not find it")
	s := NewIn(&config.Project{Root: wt, Config: config.Default()}, config.Checkout{Root: wt}, given)
	if err := s.SharedErr(); !errors.Is(err, given) {
		t.Errorf("SharedErr() = %v, want the error it was given", err)
	}
	if s.LockRoot() != wt {
		t.Errorf("LockRoot() = %s, want the checkout itself where the main checkout was not found", s.LockRoot())
	}
}

// A clone brings links with everything else: the main checkout's records
// reached through a link out of it are not written, from a worktree either.
func TestARecordInTheMainCheckoutIsNotWrittenThroughALinkOutOfIt(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{"US-001": model.StatusInProgress})
	elsewhere := clTempDir(t)
	if err := os.MkdirAll(filepath.Join(main, ".sdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(main, ".sdlc", "stories")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	err := clStore(wt).SaveRecord(model.NewRecord("US-001", fixedTime))
	clError(t, err, sdlcerr.StateUnwritable)
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("the record was written through the link: %v", entries)
	}
}
