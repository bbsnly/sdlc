package cli

// US-089 in process: a story's change to the root CODEMAP.md is made on trunk
// as its landing left it, after the story's code has landed, and not in the
// story's own branch.
//
// The bookkeeper stores whole named sections with `sdlc codemap write`, on
// standard input, while the story is still held. `sdlc stop` lands the story
// as it always did and then, still under the landing lock, applies the
// sections to trunk's CODEMAP.md, makes one commit of that file alone and
// moves trunk to it; the gate record names the commit in `codemap_commit`
// (AC-1 to AC-4, AC-7). Where that cannot be done the landing stands: the stop
// exits 0, prints a code of its own as a warning, leaves the sections where
// they were stored, and names `sdlc remap <ID>`, which only a person runs
// (AC-5, AC-6). A conflict in any other file is SDLC-E0075 as before (AC-8).
//
// The names these tests fix, for the plan to meet:
//
//	sdlc codemap write            the sections, on standard input
//	sdlc remap STORY              apply what a stop could not, where it still applies
//	sdlc remap STORY --replace    put the stored sections over trunk's
//	sdlc remap STORY --drop       discard them, with no commit
//	<git-dir>/sdlc/codemap/<ID>.md   where they are kept, in the common git directory
//	codemap_commit                the record's field naming the map's commit
//	codemap                       the type of the record's events about the map
//
// The warning's code is whichever the catalogue gives it: it is found here as
// the code above SDLC-E0075 that the command prints, so that only
// internal/sdlcerr names the number.
//
// Everything here reads what a caller can see: what a command printed and how
// it exited, git's own answers, the record and the backlog on disk. Helpers
// carry a cm prefix, for code map.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	cmFile         = "CODEMAP.md"
	cmThirdSession = "cm-session-third"
	cmAddLine      = "<!-- sdlc:add -->"
	cmReplaceLine  = "<!-- sdlc:replace -->"
)

// cmMap is trunk's map before any story starts: a title, a line under it and
// two sections.
const cmMap = "# CODEMAP\n\nWhat each file is for.\n\n## billing.go\n\nKeeps the accounts.\n\n## price list.txt\n\nThe prices.\n"

// The sections the stories here store.
const (
	cmRefund          = cmAddLine + "\n## refund.go\n\nKeeps refunds, which US-001 adds.\n"
	cmRefundSection   = "## refund.go\n\nKeeps refunds, which US-001 adds.\n"
	cmShipping        = cmAddLine + "\n## shipping.go\n\nKeeps shipping, which US-002 adds.\n"
	cmShippingSection = "## shipping.go\n\nKeeps shipping, which US-002 adds.\n"
	cmBilling         = cmReplaceLine + "\n## billing.go\n\nKeeps the accounts and, since US-001, invoices.\n"
	cmBillingSection  = "## billing.go\n\nKeeps the accounts and, since US-001, invoices.\n"
)

// cmWalked is US-001 worked in its worktree through every gate, the retro
// included, in a repository whose trunk holds cmMap and has a committer; and
// the commit its branch was cut from. Nothing is stored for the map yet. The
// test's commands run in the worktree from ldSession.
func cmWalked(t *testing.T) (main, wt, cut string) {
	t.Helper()
	main, notes := walkedCopyNoted(t, "codemap: one finished story on a map of two sections", func(t *testing.T, root string) map[string]string {
		t.Helper()
		ldRepositoryAt(t, root, func(root string) { writeFile(t, root, cmFile, cmMap) })
		cut := ldTrunk(t, root)
		ldFinished(t, root)
		return map[string]string{"cut": cut}
	})
	otIdentity(t, main)
	wt = swWorktreeOf(main, "US-001")
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	return main, wt, notes["cut"]
}

// cmTwoWalked is cmWalked with US-002 worked beside it by another session,
// both begun from the same trunk commit and neither stopped.
func cmTwoWalked(t *testing.T) (main, first, second, cut string) {
	t.Helper()
	main, notes := walkedCopyNoted(t, "codemap: two finished stories on a map of two sections", func(t *testing.T, root string) map[string]string {
		t.Helper()
		ldRepositoryAt(t, root, func(root string) { writeFile(t, root, cmFile, cmMap) }, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		cut := ldTrunk(t, root)
		ldFinished(t, root)
		otSecondStory(t, root)
		return map[string]string{"cut": cut}
	})
	otIdentity(t, main)
	first, second = swWorktreeOf(main, "US-001"), swWorktreeOf(main, "US-002")
	for _, wt := range []string{first, second} {
		if got := swGit(t, wt, "merge-base", "HEAD", "refs/heads/main"); got != notes["cut"] {
			t.Fatalf("precondition: %s sits on %s, want both stories begun from the trunk commit %s", wt, got, notes["cut"])
		}
	}
	chdir(t, first)
	setenv(t, store.SessionEnv, ldSession)
	return main, first, second, notes["cut"]
}

// cmSectionsFile is where a story's sections are kept until they are on trunk.
func cmSectionsFile(main, id string) string {
	return filepath.Join(main, ".git", "sdlc", "codemap", id+".md")
}

// cmStores is the bookkeeper storing its sections in the story's worktree,
// which has to work.
func cmStores(t *testing.T, wt, session, sections string) {
	t.Helper()
	wtMust(t, wt, session, sections, "codemap", "write")
}

// cmTrunkMap is trunk's CODEMAP.md as git holds it, and whether trunk has one.
func cmTrunkMap(t *testing.T, main string) (string, bool) {
	t.Helper()
	return otAt(t, main, "refs/heads/main", cmFile)
}

// cmRecord is a story's record as the JSON on disk, so that a field the story
// adds is read by the name it has there.
func cmRecord(t *testing.T, main, id string) map[string]any {
	t.Helper()
	var r map[string]any
	wtReadJSON(t, wtRecordPath(main, id), &r)
	return r
}

func cmText(r map[string]any, key string) string {
	s, _ := r[key].(string)
	return s
}

// cmEvents is the message of every codemap event on a story's record.
func cmEvents(t *testing.T, main, id string) []string {
	t.Helper()
	var out []string
	for _, e := range swRecord(t, main, id).Events {
		if e.Type == "codemap" {
			out = append(out, e.Message)
		}
	}
	return out
}

var (
	cmCodes    = regexp.MustCompile(`SDLC-E(\d{4})`)
	cmFullHash = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// cmOwnCode is the code the story adds, as a command printed it: every code up
// to SDLC-E0075 was registered before, for other conditions.
func cmOwnCode(said string) string {
	for _, m := range cmCodes.FindAllStringSubmatch(said, -1) {
		if n, _ := strconv.Atoi(m[1]); n > 75 {
			return m[0]
		}
	}
	return ""
}

func cmSaid(r result) string { return r.stdout + r.stderr }

// cmMapCommit holds a commit to AC-3 and AC-4: its one parent is parent, it
// changes CODEMAP.md and no other path, as a plain file, it is no merge, and
// its subject is a docs commit that names the story.
func cmMapCommit(t *testing.T, main, commit, parent, id string) {
	t.Helper()
	if !cmFullHash.MatchString(commit) {
		t.Fatalf("the map's commit is named %q, want a commit's full hash", commit)
	}
	if got := swGit(t, main, "rev-list", "--parents", "-n", "1", commit); got != commit+" "+parent {
		t.Errorf("the map's commit and its parents are %q, want the one parent %s", got, parent)
	}
	if got := swGit(t, main, "diff-tree", "-r", "--no-renames", "--name-only", "--no-commit-id", parent, commit); got != cmFile {
		t.Errorf("the map's commit changes %q, want CODEMAP.md and no other path", got)
	}
	if got := swGit(t, main, "ls-tree", commit, "--", cmFile); !strings.HasPrefix(got, "100644 blob ") {
		t.Errorf("the map's commit has CODEMAP.md as %q, want a plain file", got)
	}
	subject := swGit(t, main, "log", "-1", "--format=%s", commit)
	if !strings.HasPrefix(subject, "docs") || !strings.Contains(subject, id) {
		t.Errorf("the map's commit is %q, want a docs commit that names %s", subject, id)
	}
}

// cmLandedWithMap holds a stop to having landed the story and then put its
// map change on trunk: exit 0, no code printed, the story done; trunk at a
// commit of CODEMAP.md alone whose parent is the story's last landed commit;
// the record naming it in codemap_commit and keeping the story's own range;
// the sections no longer kept; and the stop saying so. It returns the map's
// commit.
func cmLandedWithMap(t *testing.T, main, id string, r result) string {
	t.Helper()
	said := cmSaid(r)
	if r.code != 0 {
		t.Fatalf("sdlc stop exited %d, want %s landed and its map change on trunk:\n%s", r.code, id, said)
	}
	if strings.Contains(said, "SDLC-E") {
		t.Errorf("the stop that landed %s and its map change printed a code:\n%s", id, said)
	}
	if got := wtStatuses(t, main)[id]; got != string(model.StatusDone) {
		t.Errorf("%s is %s after the stop, want done", id, got)
	}
	rec := cmRecord(t, main, id)
	landed, mapped := cmText(rec, "commit"), cmText(rec, "codemap_commit")
	if mapped == "" {
		t.Fatalf("%s's record names no commit in codemap_commit:\n%s", id, said)
	}
	cmMapCommit(t, main, mapped, landed, id)
	if got := ldTrunk(t, main); got != mapped {
		t.Errorf("trunk is at %s after the stop, want the map's commit %s", got, mapped)
	}
	if base := cmText(rec, "commit_base"); base == "" || landed == mapped {
		t.Errorf("the record names %s..%s as the story's own commits and %s as the map's: the two are not kept apart", base, landed, mapped)
	}
	if wtExists(cmSectionsFile(main, id)) {
		t.Errorf("%s's sections are still kept under the git directory after they reached trunk", id)
	}
	if !strings.Contains(said, cmFile) || !strings.Contains(said, mapped[:12]) {
		t.Errorf("the stop does not say CODEMAP.md was brought up to date at %s:\n%s", mapped[:12], said)
	}
	if len(cmEvents(t, main, id)) == 0 {
		t.Errorf("%s's record has no codemap event for the commit", id)
	}
	if swListed(t, main, swWorktreeOf(main, id)) || swHasBranch(t, main, swBranchOf(id)) {
		t.Errorf("the landed %s's worktree or branch is still there", id)
	}
	return mapped
}

// cmLandedWithoutMap holds a stop to AC-6: the landing stands and the map
// step did not happen. Exit 0 and the story done, its worktree gone; trunk at
// the story's landed commits and no further; no commit named for the map; the
// stop saying, under a code of its own with a fix, that CODEMAP.md was not
// brought up to date and that `sdlc remap <ID>` is what to run; and never
// that nothing landed. It returns what the stop said.
func cmLandedWithoutMap(t *testing.T, main, id string, r result) string {
	t.Helper()
	said := cmSaid(r)
	if r.code != 0 {
		t.Fatalf("sdlc stop exited %d: the story's code landed, so the landing stands and the exit is 0:\n%s", r.code, said)
	}
	if got := wtStatuses(t, main)[id]; got != string(model.StatusDone) {
		t.Errorf("%s is %s after the stop, want done: its landing stands", id, got)
	}
	rec := cmRecord(t, main, id)
	landed := cmText(rec, "commit")
	if got := ldTrunk(t, main); landed == "" || got != landed {
		t.Errorf("trunk is at %s after the stop, want it left at the story's landed commits, %s", got, landed)
	}
	if got := cmText(rec, "codemap_commit"); got != "" {
		t.Errorf("the record names %s as the map's commit, and no map change reached trunk", got)
	}
	if swListed(t, main, swWorktreeOf(main, id)) || swHasBranch(t, main, swBranchOf(id)) {
		t.Errorf("the landed %s's worktree or branch is still there", id)
	}
	if cmOwnCode(said) == "" {
		t.Errorf("the stop prints no code of its own for the map change that did not reach trunk:\n%s", said)
	}
	for _, want := range []string{cmFile, "sdlc remap " + id, "  fix  "} {
		if !strings.Contains(said, want) {
			t.Errorf("the stop does not say %q:\n%s", want, said)
		}
	}
	if strings.Contains(strings.ToLower(said), "nothing landed") || strings.Contains(said, "SDLC-E0075") {
		t.Errorf("the stop speaks of a landing that did not happen, and the story landed:\n%s", said)
	}
	return said
}

// cmUnchanged holds a refused repair to having changed nothing: trunk where
// it was, the sections as they were kept, and no commit named on the record.
func cmUnchanged(t *testing.T, main, id, trunk, kept, what string) {
	t.Helper()
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("%s moved trunk from %s to %s", what, trunk, got)
	}
	if got := wtRead(t, cmSectionsFile(main, id)); got != kept {
		t.Errorf("%s left the kept sections as\n%q\nwant them as they were\n%q", what, got, kept)
	}
	if got := cmText(cmRecord(t, main, id), "codemap_commit"); got != "" {
		t.Errorf("%s put %s on the record as the map's commit", what, got)
	}
}

// cmRefusedRepair holds a repair to having been refused in words a person can
// act on: a non-zero exit, a code and a fix.
func cmRefusedRepair(t *testing.T, r result, args ...string) string {
	t.Helper()
	said := cmSaid(r)
	if r.code == 0 {
		t.Fatalf("sdlc %s went ahead where it should have been refused:\n%s", strings.Join(args, " "), said)
	}
	if !strings.Contains(said, "SDLC-E") || !strings.Contains(said, "  fix  ") {
		t.Errorf("sdlc %s was refused without a code and a fix:\n%s", strings.Join(args, " "), said)
	}
	return said
}

// cmPerson is a person running a command in the main checkout, in their own
// terminal: no session.
func cmPerson(t *testing.T, main string, args ...string) result {
	t.Helper()
	return wtRun(t, main, "", "", args...)
}

// cmStopped is a stop whose map step could not be done because the main
// checkout, which has trunk checked out, held a person's uncommitted edit to
// CODEMAP.md (THREATS T8). The story landed; its sections, cmRefund, are still
// kept; the person's edit is taken back, so that the kept change now applies.
func cmStopped(t *testing.T) (main, landed string) {
	t.Helper()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	writeFile(t, main, cmFile, cmMap+"\nA person's line, not committed.\n")
	cmLandedWithoutMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	writeFile(t, main, cmFile, cmMap)
	return main, ldTrunk(t, main)
}

// ---------------------------------------------------------------- storing

// The first answer: the bookkeeper stores its map change before the stop, as
// whole named sections, through an sdlc command reading standard input. What
// it stores is kept in the common git directory, byte for byte, and nothing
// in the worktree changes: an uncommitted file there would stop the landing.
func TestAC1TheBookkeeperStoresItsSectionsAndTheWorktreeStaysClean(t *testing.T) {
	t.Parallel()
	main, wt, cut := cmWalked(t)
	set := cmRefund + "\n" + cmBilling

	r := wtMust(t, wt, ldSession, set, "codemap", "write", "--json")
	got := decode[struct {
		OK       bool   `json:"ok"`
		Story    string `json:"story"`
		Sections int    `json:"sections"`
	}](t, r)
	if !got.OK || got.Story != "US-001" || got.Sections != 2 {
		t.Errorf("codemap write --json = %+v, want ok, US-001 and 2 sections", got)
	}
	if kept := wtRead(t, cmSectionsFile(main, "US-001")); kept != set {
		t.Errorf("the sections are kept as\n%q\nwant what was stored\n%q", kept, set)
	}
	for _, dir := range []string{wt, main} {
		if status := swGitRaw(t, dir, "status", "--porcelain", "--untracked-files=all", "--", cmFile, ".git"); status != "" {
			t.Errorf("git status in %s reports %q after the sections were stored", dir, status)
		}
	}
	if status := swGitRaw(t, wt, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Errorf("the story's worktree holds something uncommitted after the sections were stored:\n%s", status)
	}
	if got := ldTrunk(t, main); got != cut {
		t.Errorf("storing the sections moved trunk to %s", got)
	}
	if got := cmText(cmRecord(t, main, "US-001"), "codemap_commit"); got != "" {
		t.Errorf("the record names %s as the map's commit before the story has landed", got)
	}

	// Stored again, the second set is the story's: one change, not two.
	plain := wtMust(t, wt, ldSession, cmRefund, "codemap", "write")
	if !strings.Contains(plain.stdout, "US-001") {
		t.Errorf("codemap write does not say which story it stored for:\n%s", plain.stdout)
	}
	if kept := wtRead(t, cmSectionsFile(main, "US-001")); kept != cmRefund {
		t.Errorf("after a second write the sections kept are\n%q\nwant the second set", kept)
	}
}

// THREATS T2: the sections are checked when they are stored, while the
// bookkeeper can still put them right, and a set that is refused replaces
// nothing. One of each kind of malformed set; internal/codemap holds them all.
func TestAC6ASetThatIsNotWellFormedIsRefusedWhenItIsStoredAndStoresNothing(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	big := cmAddLine + "\n## refund.go\n\n" + strings.Repeat("Keeps refunds, at length.\n", (2<<20)/26)
	for _, tc := range []struct {
		name, set string
		names     string
	}{
		{"nothing on standard input", "", ""},
		{"blank lines only", "\n\n", ""},
		{"a section with no directive", cmRefundSection, ""},
		{"the whole map, as the file itself", cmMap, ""},
		{"a heading that escapes its section", cmAddLine + "\n## refund.go\n\nKeeps refunds.\n## billing.go\n\nGone.\n", "refund.go"},
		{"a title in a body", cmAddLine + "\n## refund.go\n\n# Another map\n", "refund.go"},
		{"a replacement addressed to the title", cmReplaceLine + "\n# CODEMAP\n\nEverything, rewritten.\n", ""},
		{"a replacement with no body", cmReplaceLine + "\n## billing.go\n", "billing.go"},
		{"a heading with a space after it", cmAddLine + "\n## refund.go \n\nKeeps refunds.\n", ""},
		{"a heading with a closing run of marks", cmAddLine + "\n## refund.go ##\n\nKeeps refunds.\n", ""},
		{"one heading twice", cmRefund + "\n" + cmReplaceLine + "\n## refund.go\n\nAgain.\n", "refund.go"},
		{"a conflict marker", cmAddLine + "\n## refund.go\n\n<<<<<<< HEAD\nKeeps refunds.\n=======\nKeeps nothing.\n>>>>>>> story/US-002\n", "refund.go"},
		{"a fence left open", cmAddLine + "\n## refund.go\n\n```go\nrefund()\n", "refund.go"},
		{"CRLF line endings", strings.ReplaceAll(cmRefund, "\n", "\r\n"), ""},
		{"a NUL", strings.Replace(cmRefund, "Keeps", "Ke\x00eps", 1), ""},
		{"invalid UTF-8", strings.Replace(cmRefund, "Keeps", "Ke\xffeps", 1), ""},
		{"two mebibytes", big, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := wtRun(t, wt, ldSession, tc.set, "codemap", "write")
			said := cmRefusedRepair(t, r, "codemap", "write")
			if tc.names != "" && !strings.Contains(said, tc.names) {
				t.Errorf("the refusal does not name the section %q:\n%s", tc.names, said)
			}
			if len(said) > 4096 {
				t.Errorf("the refusal is %d bytes long: it repeats what it was given", len(said))
			}
			if kept := wtRead(t, cmSectionsFile(main, "US-001")); kept != cmRefund {
				t.Errorf("the refused set replaced what was stored: %q", kept)
			}
		})
	}
}

// The sections belong to the story being worked on. With none, there is no
// story to store them for, and nothing is written.
func TestStoringSectionsWithNoStoryBeingWorkedOnIsRefused(t *testing.T) {
	t.Parallel()
	main := ldRepository(t, func(root string) { writeFile(t, root, cmFile, cmMap) })
	r := wtRun(t, main, ldSession, cmRefund, "codemap", "write")
	if said := cmRefusedRepair(t, r, "codemap", "write"); !strings.Contains(said, "SDLC-E0011") {
		t.Errorf("the refusal is not SDLC-E0011, no story being worked on:\n%s", said)
	}
	if wtExists(filepath.Join(main, ".git", "sdlc", "codemap")) {
		t.Error("sections were kept for no story")
	}
}

// ---------------------------------------------------------------- the stop

// AC-4: the story's map change reaches trunk in a commit of its own that
// changes no other path, after the story's own commits, and the gate record
// names that commit. AC-3: the commit's one parent is trunk as the landing
// left it, and it is that map with the section added.
func TestAC4AndAC3TheStopPutsTheMapChangeOnTrunkInACommitOfItsOwnAndTheRecordNamesIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"AC-4 stop", []string{"stop"}},
		{"AC-4 stop --json", []string{"stop", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt, cut := cmWalked(t)
			tip := swRev(t, wt, "HEAD")
			cmStores(t, wt, ldSession, cmRefund)

			r := wtRun(t, wt, ldSession, "", tc.args...)
			mapped := cmLandedWithMap(t, main, "US-001", r)
			if len(tc.args) > 1 {
				if got := decode[stopPayload](t, r); !got.OK || got.Story != "US-001" || !got.Done {
					t.Errorf("stop --json = %+v, want US-001 done", got)
				}
			}
			rec := swRecord(t, main, "US-001")
			if rec.CommitBase != cut || rec.Commit != tip {
				t.Errorf("the record names %s..%s as the story's own commits, want %s..%s: the map's commit is not one of them",
					rec.CommitBase, rec.Commit, cut, tip)
			}
			if got := swGit(t, main, "rev-list", "--count", tip+".."+mapped); got != "1" {
				t.Errorf("trunk gained %s commits after the story's own, want the one for the map", got)
			}
			if got, _ := cmTrunkMap(t, main); got != cmMap+"\n"+cmRefundSection {
				t.Errorf("trunk's CODEMAP.md is\n%q\nwant the map with the section added\n%q", got, cmMap+"\n"+cmRefundSection)
			}
			// The checkout that has trunk checked out moved with it.
			if got := wtRead(t, filepath.Join(main, cmFile)); got != cmMap+"\n"+cmRefundSection {
				t.Errorf("the main checkout's CODEMAP.md is\n%q\nwant trunk's", got)
			}
			if got := swGitRaw(t, main, "status", "--porcelain", "--", cmFile, "billing.go", "refund.go"); got != "" {
				t.Errorf("the main checkout's working tree is not trunk's where the story and its map landed:\n%s", got)
			}
			if merges := swGit(t, main, "rev-list", "--merges", cut+".."+mapped); merges != "" {
				t.Errorf("trunk has merge commits: %s", merges)
			}
			if e := ldLogged(t, main); e.CommitBase != cut || e.Commit != tip {
				t.Errorf("sdlc log names %s..%s for the story, want its own commits %s..%s", e.CommitBase, e.Commit, cut, tip)
			}
		})
	}
}

// A replacement and an addition in one set are one commit: the section on
// trunk is what the bookkeeper read, so the stored one takes its place.
func TestAC4AReplacedSectionAndAnAddedOneReachTrunkInTheOneCommit(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmBilling+"\n"+cmRefund)

	cmLandedWithMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	want := "# CODEMAP\n\nWhat each file is for.\n\n" + cmBillingSection + "\n## price list.txt\n\nThe prices.\n\n" + cmRefundSection
	if got, _ := cmTrunkMap(t, main); got != want {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant\n%q", got, want)
	}
}

// AC-3: trunk moved since the story began, by a commit that changed the map
// elsewhere. The story's commits are put onto it, and the map change is made
// against the map that commit left: what trunk gained stays.
func TestAC3TheMapChangeIsMadeAgainstTrunksMapAsTheLandingLeftIt(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund+"\n"+cmBilling)
	theirs := strings.Replace(cmMap, "The prices.\n", "The prices, as another story left them.\n", 1) + "\n## ledger.go\n\nAnother story's.\n"
	moved := swCommitFile(t, main, cmFile, theirs, "another story's map change")

	mapped := cmLandedWithMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	if _, ok := swGitTry(t, main, "merge-base", "--is-ancestor", moved, mapped); !ok {
		t.Fatalf("trunk at %s does not descend from %s, where it was before the stop", mapped[:12], moved[:12])
	}
	want := "# CODEMAP\n\nWhat each file is for.\n\n" + cmBillingSection +
		"\n## price list.txt\n\nThe prices, as another story left them.\n\n## ledger.go\n\nAnother story's.\n\n" + cmRefundSection
	if got, _ := cmTrunkMap(t, main); got != want {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant what the other story left with this one's sections applied\n%q", got, want)
	}
}

// AC-1, AC-2 and AC-3: two stories begun from the same trunk commit, each with
// a section to add at the same place, the end of the map, each stopped once.
// Both land with nothing from a person between them and no SDLC-E0075; trunk's
// map holds each section once and whole; and the second story's map commit is
// the first one's map with a section added, on a parent that already held the
// first.
func TestAC1AC2AC3TwoStoriesWithAMapChangeAtTheSamePlaceBothLandWithNobodyBetween(t *testing.T) {
	t.Parallel()
	main, first, second, cut := cmTwoWalked(t)
	cmStores(t, first, ldSession, cmRefund)
	cmStores(t, second, otSecondSession, cmShipping)

	mapOne := cmLandedWithMap(t, main, "US-001", wtRun(t, first, ldSession, "", "stop"))
	mapTwo := cmLandedWithMap(t, main, "US-002", wtRun(t, second, otSecondSession, "", "stop"))

	afterOne, _ := otAt(t, main, mapOne, cmFile)
	afterTwo, _ := otAt(t, main, mapTwo, cmFile)
	if afterOne != cmMap+"\n"+cmRefundSection {
		t.Errorf("after US-001 trunk's CODEMAP.md is\n%q", afterOne)
	}
	if afterTwo != cmMap+"\n"+cmRefundSection+"\n"+cmShippingSection {
		t.Errorf("after both, trunk's CODEMAP.md is\n%q\nwant each story's section once and whole\n%q",
			afterTwo, cmMap+"\n"+cmRefundSection+"\n"+cmShippingSection)
	}
	// AC-3: the second map commit's parent holds the map the first landing
	// left, and the commit adds to it and takes nothing away.
	parent := swRev(t, main, mapTwo+"^")
	if got, _ := otAt(t, main, parent, cmFile); got != afterOne {
		t.Errorf("the parent of US-002's map commit holds\n%q\nwant the map US-001's landing left\n%q", got, afterOne)
	}
	if _, ok := swGitTry(t, main, "merge-base", "--is-ancestor", mapOne, parent); !ok {
		t.Errorf("US-002's map commit is not made on a trunk that held US-001's map commit")
	}
	if got := swGit(t, main, "diff", "--numstat", parent, mapTwo, "--", cmFile); !strings.HasPrefix(got, "4\t0\t") {
		t.Errorf("US-002's map commit changes CODEMAP.md by %q, want four lines added and none removed: a plain change to its parent", got)
	}
	for _, line := range []string{"## refund.go", "Keeps refunds, which US-001 adds.", "## shipping.go", "Keeps shipping, which US-002 adds.", "## billing.go", "## price list.txt"} {
		if n := strings.Count(afterTwo, line+"\n"); n != 1 {
			t.Errorf("trunk's CODEMAP.md holds %q %d times, want once", line, n)
		}
	}
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
		if strings.Contains(afterTwo, marker) {
			t.Errorf("trunk's CODEMAP.md holds a conflict marker, %s", marker)
		}
	}
	if merges := swGit(t, main, "rev-list", "--merges", cut+".."+mapTwo); merges != "" {
		t.Errorf("trunk has merge commits: %s", merges)
	}
	for _, id := range []string{"US-001", "US-002"} {
		r := swRecord(t, main, id)
		if len(r.Approvals) != 0 {
			t.Errorf("%s's record has %d approvals: a person's command came between the stops", id, len(r.Approvals))
		}
		for _, e := range r.Events {
			if e.Type == "release" || (e.Type == "unfreeze" && e.Message != "the story is finished, so its freeze is lifted") {
				t.Errorf("%s's record has a %s on it: a person's command came between the stops", id, e.Type)
			}
		}
	}
	for _, f := range []string{"refund.go", "shipping.go"} {
		if _, ok := otAt(t, main, mapTwo, f); !ok {
			t.Errorf("trunk does not hold %s after both stops", f)
		}
	}
}

// AC-7: a story whose bookkeeper stored nothing lands as it did, with no added
// commit, nothing said of the map and nothing on the record about it.
func TestAC7AStoryWithNoMapChangeLandsAsBeforeWithNoAddedCommit(t *testing.T) {
	t.Parallel()
	main, wt, cut := cmWalked(t)
	tip := swRev(t, wt, "HEAD")

	landed := otLands(t, main, wt, cut)
	if landed.tip != tip {
		t.Errorf("trunk is at %s after the stop, want the story's tip %s and no commit after it", landed.tip, tip)
	}
	rec := cmRecord(t, main, "US-001")
	if _, has := rec["codemap_commit"]; has {
		t.Errorf("the record has a codemap_commit, %v, for a story that changed nothing in the map", rec["codemap_commit"])
	}
	if got := cmEvents(t, main, "US-001"); len(got) != 0 {
		t.Errorf("the record has codemap events for a story with no map change: %q", got)
	}
	if got, _ := cmTrunkMap(t, main); got != cmMap {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant it as it was", got)
	}
	if strings.Contains(landed.said, "remap") {
		t.Errorf("the stop speaks of a repair for a story with no map change:\n%s", landed.said)
	}
	for _, args := range [][]string{{"status"}, {"story", "list"}, {"log"}} {
		if said := cmSaid(cmPerson(t, main, args...)); strings.Contains(said, "remap") {
			t.Errorf("sdlc %s speaks of a repair after a story with no map change landed:\n%s", strings.Join(args, " "), said)
		}
	}
}

// AC-7 and the fourth answer: in a project with no CODEMAP.md, a story that
// stored nothing lands with no added commit and no file made; one that stored
// sections has the file made for it, in the one commit.
func TestAC7InAProjectWithNoMapNothingIsAddedUnlessTheBookkeeperStoredSections(t *testing.T) {
	t.Parallel()
	for _, stored := range []bool{false, true} {
		name := map[bool]string{false: "AC-7 nothing stored", true: "AC-7 sections stored"}[stored]
		t.Run(name, func(t *testing.T) {
			main := ldRepository(t, func(root string) {
				if err := os.Remove(filepath.Join(root, cmFile)); err != nil {
					t.Fatal(err)
				}
			})
			otIdentity(t, main)
			cut := ldTrunk(t, main)
			if _, has := cmTrunkMap(t, main); has {
				t.Fatal("precondition: trunk has a CODEMAP.md")
			}
			wt := ldFinished(t, main)
			tip := swRev(t, wt, "HEAD")
			if !stored {
				if landed := otLands(t, main, wt, cut); landed.tip != tip {
					t.Errorf("trunk is at %s after the stop, want the story's tip %s and no commit after it", landed.tip, tip)
				}
				if got, has := cmTrunkMap(t, main); has || wtExists(filepath.Join(main, cmFile)) {
					t.Errorf("a CODEMAP.md was made for a story that stored nothing: %q", got)
				}
				return
			}
			cmStores(t, wt, ldSession, cmRefund)
			cmLandedWithMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
			got, has := cmTrunkMap(t, main)
			if !has || !strings.HasSuffix(got, cmRefundSection) || strings.Count(got, "## refund.go\n") != 1 {
				t.Errorf("trunk's new CODEMAP.md is %q, want it to hold the section once and whole", got)
			}
		})
	}
}

// The fifth answer and THREATS T9: a CODEMAP.md commit on the story's own
// branch is still carried to trunk with the story, and stored sections go on
// top of it in the commit of their own.
func TestAMapCommitOnTheStorysOwnBranchStillLandsAndTheSectionsGoOnTopOfIt(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	branch := swCommitFile(t, wt, cmFile, cmMap+"\n## invoice.go\n\nOn the story's own branch.\n", "docs: map US-001")
	cmStores(t, wt, ldSession, cmRefund)

	r := wtRun(t, wt, ldSession, "", "stop")
	mapped := cmLandedWithMap(t, main, "US-001", r)
	if parent := swRev(t, main, mapped+"^"); parent != branch {
		t.Errorf("the map's commit is on %s, want the story's own tip %s", parent, branch)
	}
	want := cmMap + "\n## invoice.go\n\nOn the story's own branch.\n\n" + cmRefundSection
	if got, _ := cmTrunkMap(t, main); got != want {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant the branch's change with the stored section after it\n%q", got, want)
	}
}

// Where no checkout has trunk checked out only the branch moves, for the map's
// commit as for the story's.
func TestAC4TheMapCommitIsMadeWhereNoCheckoutHasTrunkCheckedOut(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	swGit(t, main, "switch", "--quiet", "-c", "spike")
	cmStores(t, wt, ldSession, cmRefund)

	cmLandedWithMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	if got := wtRead(t, filepath.Join(main, cmFile)); got != cmMap {
		t.Errorf("the main checkout, on another branch, had its CODEMAP.md changed to %q", got)
	}
	if got := swGit(t, main, "symbolic-ref", "--short", "HEAD"); got != "spike" {
		t.Errorf("the main checkout is on %q after the stop, want it left on spike", got)
	}
}

// THREATS T1: the commit is built from trunk's own objects, never from a
// checkout. What a person left staged or loose in the main checkout does not
// ride along, and stays where they left it.
func TestAC4NothingStagedOrLooseInTheMainCheckoutRidesAlongWithTheMap(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	writeFile(t, main, "staged.go", "package billing\n\n// a person's, staged\n")
	swGit(t, main, "add", "--", "staged.go")
	writeFile(t, main, "loose.txt", "a person's, loose\n")
	writeFile(t, main, "README.md", "# Billing\n\nA person's edit, not committed.\n")

	mapped := cmLandedWithMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	for _, f := range []string{"staged.go", "loose.txt"} {
		if _, ok := otAt(t, main, mapped, f); ok {
			t.Errorf("trunk holds %s, which a person had only left in the main checkout", f)
		}
		if !wtExists(filepath.Join(main, f)) {
			t.Errorf("the person's %s is gone from the main checkout", f)
		}
	}
	if got, _ := otAt(t, main, mapped, "README.md"); got != "# Billing\n" {
		t.Errorf("trunk's README.md is %q: a person's uncommitted edit reached trunk", got)
	}
	if got := wtRead(t, filepath.Join(main, "README.md")); !strings.Contains(got, "A person's edit") {
		t.Errorf("the person's uncommitted README.md was written over: %q", got)
	}
	if got := swGitRaw(t, main, "diff", "--cached", "--name-only"); strings.TrimSpace(got) != "staged.go" {
		t.Errorf("the main checkout's index holds %q staged, want the person's staged.go alone", got)
	}
}

// ---------------------------------------------------------------- AC-5, AC-6: the step that fails

// cmCase is one way the map step cannot be done after the story has landed.
type cmCase struct {
	name string
	// stored is what the bookkeeper stores, cmRefund where it is "".
	stored string
	// bare leaves the repository without a committer.
	bare bool
	// arrange runs after the sections are stored and before the stop.
	arrange func(t *testing.T, main string)
	// names is what the stop has to name: a section, or a path (AC-5).
	names []string
	// kept is what is left under the git directory, "" for what was stored and
	// "-" for a place a test filled with something that is not a file.
	kept string
}

func cmCommitOnTrunk(t *testing.T, main, message string) {
	t.Helper()
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
}

func cmCases() []cmCase {
	huge := cmAddLine + "\n## refund.go\n\n" + strings.Repeat("Keeps refunds, at length.\n", (2<<20)/26)
	return []cmCase{
		{name: "AC-6 a replaced section that a commit on trunk changed since the story began", stored: cmBilling + "\n" + cmRefund,
			arrange: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, cmFile, strings.Replace(cmMap, "Keeps the accounts.\n", "Keeps the accounts, as another story left it.\n", 1), "another story's map change")
			}, names: []string{"billing.go"}},
		{name: "AC-6 an added section that trunk gained since the story began",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, cmFile, cmMap+"\n## refund.go\n\nAnother story's refunds.\n", "another story's map change")
			}, names: []string{"refund.go"}},
		{name: "AC-6 an added section whose heading trunk has in another case",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, cmFile, cmMap+"\n## Refund.GO\n\nAnother story's refunds.\n", "another story's map change")
			}, names: []string{"refund.go"}},
		{name: "AC-6 a replacement of a heading trunk lacks", stored: cmReplaceLine + "\n## ledger.go\n\nKeeps the ledger.\n",
			names: []string{"ledger.go"}},
		{name: "AC-6 an added section whose heading trunk has", stored: cmAddLine + "\n## billing.go\n\nA second billing.\n",
			names: []string{"billing.go"}},
		{name: "AC-6 a person's uncommitted CODEMAP.md in the checkout that has trunk checked out",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				writeFile(t, main, cmFile, cmMap+"\nA person's line, not committed.\n")
			}},
		{name: "AC-6 no committer for the commit", bare: true},
		{name: "AC-5 trunk's CODEMAP.md is a directory with a file beneath it",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				swGit(t, main, "rm", "--quiet", "--", cmFile)
				writeFile(t, main, "CODEMAP.md/notes.md", "kept by hand\n")
				swGit(t, main, "add", "--", "CODEMAP.md/notes.md")
				cmCommitOnTrunk(t, main, "the map becomes a directory")
			}, names: []string{"CODEMAP.md/notes.md"}},
		{name: "AC-5 trunk has codemap.md and no CODEMAP.md",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				swGit(t, main, "rm", "--quiet", "--cached", "--", cmFile)
				if err := os.Rename(filepath.Join(main, cmFile), filepath.Join(main, "map.tmp")); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(main, "map.tmp"), filepath.Join(main, "codemap.md")); err != nil {
					t.Fatal(err)
				}
				swGit(t, main, "add", "--", "codemap.md")
				cmCommitOnTrunk(t, main, "the map is renamed")
				if got := swGit(t, main, "ls-tree", "--name-only", "refs/heads/main"); !strings.Contains(got, "codemap.md") || strings.Contains(got, cmFile) {
					t.Fatalf("precondition: trunk holds %q, want codemap.md and no CODEMAP.md", got)
				}
			}, names: []string{"codemap.md"}},
		{name: "AC-6 trunk's CODEMAP.md is a symbolic link",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				if err := os.Remove(filepath.Join(main, cmFile)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("README.md", filepath.Join(main, cmFile)); err != nil {
					t.Skipf("this platform makes no symbolic link here: %v", err)
				}
				swGit(t, main, "add", "--", cmFile)
				cmCommitOnTrunk(t, main, "the map becomes a link")
			}},
		{name: "AC-6 the stored sections were changed after they were stored, and are no set",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				writeFile(t, main, ".git/sdlc/codemap/US-001.md", cmAddLine+"\n## refund.go\n\nKeeps refunds.\n## billing.go\n\nGone.\n")
			}, kept: cmAddLine + "\n## refund.go\n\nKeeps refunds.\n## billing.go\n\nGone.\n"},
		{name: "AC-6 the stored sections were changed to the whole map",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				writeFile(t, main, ".git/sdlc/codemap/US-001.md", "# CODEMAP\n\nEverything, rewritten.\n")
			}, kept: "# CODEMAP\n\nEverything, rewritten.\n"},
		{name: "AC-6 the stored sections were changed to more than the bound",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				writeFile(t, main, ".git/sdlc/codemap/US-001.md", huge)
			}, kept: huge},
		{name: "AC-6 the stored sections cannot be read",
			arrange: func(t *testing.T, main string) {
				t.Helper()
				if err := os.Remove(cmSectionsFile(main, "US-001")); err != nil {
					t.Fatal(err)
				}
				writeFile(t, main, ".git/sdlc/codemap/US-001.md/inside.txt", "in the way\n")
			}, kept: "-"},
	}
}

// AC-5 and AC-6, and the third answer: where the map change cannot be made or
// committed after the story's code has landed, the landing stands. The stop
// exits 0, says under a code of its own that CODEMAP.md was not brought up to
// date and that `sdlc remap <ID>` is what to run, names the section or the
// path in the way, commits nothing of it, and leaves the sections kept under
// the git directory as they were. Trunk stays at the story's landed commits,
// and every command a person runs next says the change is still waiting.
func TestAC5AC6AMapChangeThatCannotBeMadeLeavesTheLandingStandingAndSaysWhatToRun(t *testing.T) {
	t.Parallel()
	for _, tc := range cmCases() {
		for _, json := range []bool{false, true} {
			name := tc.name
			if json {
				name += ", stop --json"
			}
			t.Run(name, func(t *testing.T) {
				main, wt, _ := cmWalked(t)
				if tc.bare {
					swGit(t, main, "config", "--unset", "user.email")
					swGit(t, main, "config", "--unset", "user.name")
					swGit(t, main, "config", "user.useConfigOnly", "true")
				}
				stored := tc.stored
				if stored == "" {
					stored = cmRefund
				}
				cmStores(t, wt, ldSession, stored)
				if tc.arrange != nil {
					tc.arrange(t, main)
				}
				readme, _ := otAt(t, main, "refs/heads/main", "README.md")
				mapBefore, hadMap := cmTrunkMap(t, main)
				mainFile := wtRead(t, filepath.Join(main, "README.md"))

				args := []string{"stop"}
				if json {
					args = append(args, "--json")
				}
				r := wtRun(t, wt, ldSession, "", args...)
				said := cmLandedWithoutMap(t, main, "US-001", r)
				if json {
					if got := decode[stopPayload](t, r); !got.OK || got.Story != "US-001" || !got.Done {
						t.Errorf("stop --json = %+v, want US-001 done: its landing stands", got)
					}
					if cmOwnCode(r.stderr) == "" || cmOwnCode(r.stdout) != "" {
						t.Errorf("the warning is not on standard error alone, where stdout is the JSON:\nstdout %s\nstderr %s", r.stdout, r.stderr)
					}
				}
				for _, want := range tc.names {
					if !strings.Contains(said, want) {
						t.Errorf("the stop does not name %q:\n%s", want, said)
					}
				}
				// Nothing of the step reached trunk.
				if got, has := cmTrunkMap(t, main); got != mapBefore || has != hadMap {
					t.Errorf("trunk's CODEMAP.md is %q (there: %v) after the step that failed, want it as it was, %q (there: %v)", got, has, mapBefore, hadMap)
				}
				if got, _ := otAt(t, main, "refs/heads/main", "README.md"); got != readme {
					t.Errorf("trunk's README.md changed to %q in the step that failed", got)
				}
				if got := wtRead(t, filepath.Join(main, "README.md")); got != mainFile {
					t.Errorf("the main checkout's README.md changed to %q in the step that failed", got)
				}
				if got, _ := otAt(t, main, "refs/heads/main", "refund.go"); !strings.Contains(got, "type Refund") {
					t.Errorf("trunk does not hold the story's refund.go: its landing did not stand")
				}
				// The change is kept as it was.
				switch tc.kept {
				case "-":
					if !wtExists(filepath.Join(cmSectionsFile(main, "US-001"), "inside.txt")) {
						t.Error("what was in the sections' place is gone")
					}
				case "":
					if got := wtRead(t, cmSectionsFile(main, "US-001")); got != stored {
						t.Errorf("the sections kept under the git directory are\n%q\nwant what was stored\n%q", got, stored)
					}
				default:
					if got := wtRead(t, cmSectionsFile(main, "US-001")); got != tc.kept {
						t.Errorf("the sections kept under the git directory are no longer what was there (%d bytes, want %d)", len(got), len(tc.kept))
					}
				}
				// A person's uncommitted map is never written over (THREATS T8).
				if strings.Contains(tc.name, "uncommitted") {
					if got := wtRead(t, filepath.Join(main, cmFile)); got != cmMap+"\nA person's line, not committed.\n" {
						t.Errorf("the person's uncommitted CODEMAP.md is now %q", got)
					}
				}
				// THREATS T6: said until a person has dealt with it, not once.
				// Where the sections' place holds no file there is nothing
				// kept to speak of, and the stop's own warning is the word.
				for range 2 {
					if tc.kept == "-" {
						break
					}
					cmSaysMapWaits(t, cmPerson(t, main, "status"), "US-001")
				}
			})
		}
	}
}

// cmSaysMapWaits holds a command to saying that a story's map change did not
// reach trunk, and what a person runs.
func cmSaysMapWaits(t *testing.T, r result, id string) {
	t.Helper()
	said := cmSaid(r)
	for _, want := range []string{id, cmFile, "sdlc remap " + id} {
		if !strings.Contains(said, want) {
			t.Errorf("the command does not say %q of the map change that did not reach trunk:\n%s", want, said)
		}
	}
}

// THREATS T2 and the first answer: two stories that each replace the same
// existing section. The first lands with its map change. The second lands
// too, and its map step is refused by the section's name, because the section
// on trunk is no longer what its bookkeeper saw; nothing is merged, and the
// first story's text is what trunk holds.
func TestAC6TwoStoriesReplacingTheSameSectionTheSecondLandsAndItsMapStepIsRefused(t *testing.T) {
	t.Parallel()
	main, first, second, _ := cmTwoWalked(t)
	theirs := cmReplaceLine + "\n## billing.go\n\nKeeps the accounts and, since US-002, shipping.\n"
	cmStores(t, first, ldSession, cmBilling)
	cmStores(t, second, otSecondSession, theirs+"\n"+cmShipping)

	mapOne := cmLandedWithMap(t, main, "US-001", wtRun(t, first, ldSession, "", "stop"))
	afterOne, _ := otAt(t, main, mapOne, cmFile)

	said := cmLandedWithoutMap(t, main, "US-002", wtRun(t, second, otSecondSession, "", "stop"))
	if !strings.Contains(said, "billing.go") {
		t.Errorf("the stop does not name the section that is not what the bookkeeper saw:\n%s", said)
	}
	if got, _ := cmTrunkMap(t, main); got != afterOne {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant it as US-001's landing left it: a story's sections are applied whole or not at all\n%q", got, afterOne)
	}
	if got := wtRead(t, cmSectionsFile(main, "US-002")); got != theirs+"\n"+cmShipping {
		t.Errorf("US-002's sections are kept as %q, want what was stored", got)
	}
	if _, ok := otAt(t, main, "refs/heads/main", "shipping.go"); !ok {
		t.Error("trunk does not hold US-002's shipping.go: its landing did not stand")
	}
}

// AC-8: a conflict in any file other than CODEMAP.md is SDLC-E0075 naming the
// files, as before. Nothing lands, no map commit is made, the story is still
// held, and the sections stay stored for the stop that does land it.
func TestAC8AConflictInAnotherFileIsStillSDLCE0075AndTheSectionsStayStored(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	moved := swCommitFile(t, main, "billing.go", ldBilling+"\n// Ledger is what another story adds.\ntype Ledger struct{}\n", "another story changes the same lines")

	out := swRefused(t, "SDLC-E0075", wt, ldSession, "stop")
	said, _ := ldRefusal(t, out, "SDLC-E0075")
	if !wtNames(said, "billing.go") {
		t.Errorf("the refusal does not name billing.go:\n%s", said)
	}
	if wtNames(said, cmFile) {
		t.Errorf("the refusal names CODEMAP.md, which nothing of the story's conflicts in:\n%s", said)
	}
	if cmOwnCode(out) != "" || strings.Contains(out, "sdlc remap") {
		t.Errorf("the refused landing speaks of a map change, and nothing landed:\n%s", out)
	}
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("the refused landing moved trunk to %s", got)
	}
	ldStillHeld(t, main, wt)
	if got := wtRead(t, cmSectionsFile(main, "US-001")); got != cmRefund {
		t.Errorf("the refused landing left the stored sections as %q", got)
	}
	if got := cmText(cmRecord(t, main, "US-001"), "codemap_commit"); got != "" {
		t.Errorf("the record names %s as the map's commit, and nothing landed", got)
	}
	// A story that is still being worked on is not one whose map change is late.
	if said := cmSaid(cmPerson(t, main, "status")); strings.Contains(said, "sdlc remap") {
		t.Errorf("status speaks of a repair for a story that has not landed:\n%s", said)
	}
}

// ---------------------------------------------------------------- the next command says so

// The requirement on the plan no criterion states, and THREATS T6: a stop
// killed between the landing and the map commit leaves the story landed, its
// sections stored and nothing printed. The next sdlc command in the repository
// says the map change did not reach trunk and what a person runs, on standard
// error where standard output is JSON, every time until a person has dealt
// with it; and the repair it names puts the change on trunk.
//
// What the kill leaves is made here by landing the story with nothing stored
// and then putting its sections where the stop would have read them: a done
// story, sections kept for it, and no map commit on its record.
func TestAStopKilledBetweenTheLandingAndTheMapCommitIsSaidByTheNextCommand(t *testing.T) {
	t.Parallel()
	main, wt, cut := cmWalked(t)
	landed := otLands(t, main, wt, cut)
	writeFile(t, main, ".git/sdlc/codemap/US-001.md", cmRefund)

	for _, args := range [][]string{{"status"}, {"story", "list"}, {"log"}, {"status"}} {
		cmSaysMapWaits(t, cmPerson(t, main, args...), "US-001")
	}
	for _, args := range [][]string{{"status", "--json"}, {"story", "list", "--json"}, {"log", "--json"}} {
		r := cmPerson(t, main, args...)
		if r.code != 0 {
			t.Fatalf("sdlc %s exited %d:\n%s", strings.Join(args, " "), r.code, cmSaid(r))
		}
		wtDocument(t, r)
		if strings.Contains(r.stdout, "sdlc remap") && !strings.HasPrefix(strings.TrimSpace(r.stdout), "{") {
			t.Errorf("sdlc %s wrote the notice into its JSON's stream as text:\n%s", strings.Join(args, " "), r.stdout)
		}
		if !strings.Contains(r.stderr, "sdlc remap US-001") || !strings.Contains(r.stderr, cmFile) {
			t.Errorf("sdlc %s does not say on standard error that US-001's map change did not reach trunk:\n%s", strings.Join(args, " "), r.stderr)
		}
	}
	// It is said to a session too, which reports it and does not run the repair.
	cmSaysMapWaits(t, wtRun(t, main, ldOtherSession, "", "status"), "US-001")
	if got := ldTrunk(t, main); got != landed.tip {
		t.Fatalf("saying so moved trunk to %s", got)
	}

	r := cmPerson(t, main, "remap", "US-001")
	if r.code != 0 {
		t.Fatalf("sdlc remap US-001 exited %d, want the stored sections put on trunk:\n%s", r.code, cmSaid(r))
	}
	mapped := cmText(cmRecord(t, main, "US-001"), "codemap_commit")
	cmMapCommit(t, main, mapped, landed.tip, "US-001")
	if got := ldTrunk(t, main); got != mapped {
		t.Errorf("trunk is at %s after the repair, want the map's commit %s", got, mapped)
	}
	for _, args := range [][]string{{"status"}, {"story", "list"}, {"log"}} {
		if said := cmSaid(cmPerson(t, main, args...)); strings.Contains(said, "sdlc remap") {
			t.Errorf("sdlc %s still speaks of the repair after it was run:\n%s", strings.Join(args, " "), said)
		}
	}
}

// Sections stored for a story that has not landed are not late: nothing is
// said of them, in the story's worktree or the main checkout.
func TestSectionsStoredForAStoryStillBeingWorkedOnAreNotSaidToBeWaiting(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	for _, dir := range []string{wt, main} {
		for _, args := range [][]string{{"status"}, {"story", "list"}} {
			if said := cmSaid(wtRun(t, dir, ldSession, "", args...)); strings.Contains(said, "sdlc remap") {
				t.Errorf("sdlc %s in %s speaks of a repair for a story that has not landed:\n%s", strings.Join(args, " "), dir, said)
			}
		}
	}
}

// ---------------------------------------------------------------- the repair

// The sixth answer, the plain form: run by a person once what was in the way
// is gone, it applies the kept change where it still applies. One commit on
// trunk at CODEMAP.md alone, by the same checks as the stop's; the record
// names it; the kept change is removed; and nothing more is said of it.
func TestAC6ThePlainRepairAppliesTheKeptChangeWhereItStillApplies(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)

	r := cmPerson(t, main, "remap", "US-001")
	if r.code != 0 {
		t.Fatalf("sdlc remap US-001 exited %d, want the kept change put on trunk:\n%s", r.code, cmSaid(r))
	}
	mapped := cmText(cmRecord(t, main, "US-001"), "codemap_commit")
	cmMapCommit(t, main, mapped, landed, "US-001")
	if got := ldTrunk(t, main); got != mapped {
		t.Errorf("trunk is at %s after the repair, want the map's commit %s", got, mapped)
	}
	if got, _ := cmTrunkMap(t, main); got != cmMap+"\n"+cmRefundSection {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant the map with the kept section added", got)
	}
	if got := wtRead(t, filepath.Join(main, cmFile)); got != cmMap+"\n"+cmRefundSection {
		t.Errorf("the main checkout's CODEMAP.md is %q, want trunk's", got)
	}
	if wtExists(cmSectionsFile(main, "US-001")) {
		t.Error("the kept change is still under the git directory after it reached trunk")
	}
	if said := cmSaid(r); !strings.Contains(said, cmFile) || !strings.Contains(said, mapped[:12]) {
		t.Errorf("the repair does not say CODEMAP.md was brought up to date at %s:\n%s", mapped[:12], said)
	}
	if rec := swRecord(t, main, "US-001"); rec.Commit != landed {
		t.Errorf("the repair changed the story's own range to end at %s, want %s", rec.Commit, landed)
	}
	if events := cmEvents(t, main, "US-001"); len(events) == 0 || !strings.Contains(events[len(events)-1], "remap") {
		t.Errorf("the record's codemap events are %q, want the last to say a person's remap made the commit", events)
	}
	if said := cmSaid(cmPerson(t, main, "status")); strings.Contains(said, "sdlc remap") {
		t.Errorf("status still speaks of the repair after it was run:\n%s", said)
	}

	// THREATS: run twice. The second finds nothing kept, says so and commits nothing.
	again := cmPerson(t, main, "remap", "US-001")
	cmRefusedRepair(t, again, "remap", "US-001")
	if got := ldTrunk(t, main); got != mapped {
		t.Errorf("the second repair moved trunk to %s", got)
	}
}

// cmSecondKept is two stories that each replaced billing.go's section, both
// stopped: US-001's is on trunk, and US-002's set, a replacement of the same
// section and an added one, is kept because the section is no longer what its
// bookkeeper saw. It returns trunk's tip and the kept set.
func cmSecondKept(t *testing.T) (main, trunk, kept string) {
	t.Helper()
	main, first, second, _ := cmTwoWalked(t)
	kept = cmReplaceLine + "\n## billing.go\n\nKeeps the accounts and, since US-002, shipping.\n" + "\n" + cmShipping
	cmStores(t, first, ldSession, cmBilling)
	cmStores(t, second, otSecondSession, kept)
	cmLandedWithMap(t, main, "US-001", wtRun(t, first, ldSession, "", "stop"))
	cmLandedWithoutMap(t, main, "US-002", wtRun(t, second, otSecondSession, "", "stop"))
	return main, ldTrunk(t, main), kept
}

// The sixth answer: where a section is not what the bookkeeper saw the plain
// repair refuses and names the section, and says what a person may choose.
// Trunk is unchanged and the kept change stays, however often it is run.
func TestAC6ThePlainRepairRefusesAndNamesASectionThatIsNotWhatTheBookkeeperSaw(t *testing.T) {
	t.Parallel()
	main, trunk, kept := cmSecondKept(t)
	for range 2 {
		r := cmPerson(t, main, "remap", "US-002")
		said := cmRefusedRepair(t, r, "remap", "US-002")
		for _, want := range []string{"billing.go", "--replace", "--drop"} {
			if !strings.Contains(said, want) {
				t.Errorf("the refusal does not say %q:\n%s", want, said)
			}
		}
		if strings.Contains(said, "## shipping.go") {
			t.Errorf("the refusal names shipping.go's section, which still applies:\n%s", said)
		}
		cmUnchanged(t, main, "US-002", trunk, kept, "the refused repair")
	}
	cmSaysMapWaits(t, cmPerson(t, main, "status"), "US-002")
}

// The sixth answer, --replace: the kept section is put over trunk's and the
// rest applied, in one commit. What trunk held in that section is gone from
// the file and stays in history; the record says which form a person used.
func TestAC6RepairWithReplacePutsTheKeptSectionOverTrunks(t *testing.T) {
	t.Parallel()
	main, trunk, _ := cmSecondKept(t)
	before, _ := cmTrunkMap(t, main)

	r := cmPerson(t, main, "remap", "US-002", "--replace")
	if r.code != 0 {
		t.Fatalf("sdlc remap US-002 --replace exited %d:\n%s", r.code, cmSaid(r))
	}
	mapped := cmText(cmRecord(t, main, "US-002"), "codemap_commit")
	cmMapCommit(t, main, mapped, trunk, "US-002")
	if got := ldTrunk(t, main); got != mapped {
		t.Errorf("trunk is at %s after the repair, want the map's commit %s", got, mapped)
	}
	want := "# CODEMAP\n\nWhat each file is for.\n\n## billing.go\n\nKeeps the accounts and, since US-002, shipping.\n\n## price list.txt\n\nThe prices.\n\n" + cmShippingSection
	if got, _ := cmTrunkMap(t, main); got != want {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant the kept section over trunk's and the added one after it\n%q", got, want)
	}
	if got, _ := otAt(t, main, trunk, cmFile); got != before || !strings.Contains(got, "since US-001, invoices") {
		t.Errorf("what trunk held before the repair is not in history as it was: %q", got)
	}
	if wtExists(cmSectionsFile(main, "US-002")) {
		t.Error("the kept change is still under the git directory after --replace")
	}
	if events := cmEvents(t, main, "US-002"); len(events) == 0 || !strings.Contains(events[len(events)-1], "--replace") {
		t.Errorf("the record's codemap events are %q, want the last to say --replace was used", events)
	}
	if said := cmSaid(cmPerson(t, main, "status")); strings.Contains(said, "sdlc remap") {
		t.Errorf("status still speaks of the repair after --replace:\n%s", said)
	}
}

// --replace where every section still applies is the plain form.
func TestAC6RepairWithReplaceWhereEverySectionStillAppliesIsThePlainRepair(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	if r := cmPerson(t, main, "remap", "US-001", "--replace"); r.code != 0 {
		t.Fatalf("sdlc remap US-001 --replace exited %d:\n%s", r.code, cmSaid(r))
	}
	cmMapCommit(t, main, cmText(cmRecord(t, main, "US-001"), "codemap_commit"), landed, "US-001")
	if got, _ := cmTrunkMap(t, main); got != cmMap+"\n"+cmRefundSection {
		t.Errorf("trunk's CODEMAP.md is\n%q\nwant the map with the kept section added", got)
	}
}

// The sixth answer, --drop: the kept change is discarded with no commit, for
// after a person's own edit. Trunk's map is whatever a person made it; the
// record says the change was dropped, so that the story does not read as one
// nobody mapped; and nothing more is said of it.
func TestAC6RepairWithDropDiscardsTheKeptChangeWithNoCommit(t *testing.T) {
	t.Parallel()
	for _, applies := range []bool{true, false} {
		name := map[bool]string{true: "AC-6 where it still applies", false: "AC-6 where a section is not what the bookkeeper saw"}[applies]
		t.Run(name, func(t *testing.T) {
			var main, trunk string
			id := "US-001"
			if applies {
				main, trunk = cmStopped(t)
			} else {
				main, trunk, _ = cmSecondKept(t)
				id = "US-002"
			}
			before, _ := cmTrunkMap(t, main)
			refs := swRefs(t, main)

			r := cmPerson(t, main, "remap", id, "--drop")
			if r.code != 0 {
				t.Fatalf("sdlc remap %s --drop exited %d:\n%s", id, r.code, cmSaid(r))
			}
			if got := swRefs(t, main); got != refs || ldTrunk(t, main) != trunk {
				t.Errorf("--drop changed the repository's refs:\nbefore\n%s\nafter\n%s", refs, got)
			}
			if got, _ := cmTrunkMap(t, main); got != before {
				t.Errorf("--drop changed trunk's CODEMAP.md to %q", got)
			}
			if wtExists(cmSectionsFile(main, id)) {
				t.Error("the kept change is still under the git directory after --drop")
			}
			if got := cmText(cmRecord(t, main, id), "codemap_commit"); got != "" {
				t.Errorf("the record names %s as the map's commit after a drop", got)
			}
			if events := cmEvents(t, main, id); len(events) == 0 || !strings.Contains(events[len(events)-1], "--drop") {
				t.Errorf("the record's codemap events are %q, want the last to say the change was dropped with --drop", events)
			}
			for _, args := range [][]string{{"status"}, {"story", "list"}, {"log"}} {
				if said := cmSaid(cmPerson(t, main, args...)); strings.Contains(said, "sdlc remap") {
					t.Errorf("sdlc %s still speaks of the repair after the change was dropped:\n%s", strings.Join(args, " "), said)
				}
			}
			// Nothing is left to repair or to drop.
			for _, form := range [][]string{{"remap", id}, {"remap", id, "--drop"}, {"remap", id, "--replace"}} {
				cmRefusedRepair(t, cmPerson(t, main, form...), form...)
			}
			if ldTrunk(t, main) != trunk {
				t.Error("a repair with nothing kept moved trunk")
			}
		})
	}
}

// THREATS T3 and T4: every way of asking the repair for something it does not
// do is refused, and none of them moves trunk or touches the kept change.
func TestARepairAskedForWhatItDoesNotDoIsRefusedAndChangesNothing(t *testing.T) {
	t.Parallel()
	main, trunk, kept := cmSecondKept(t)
	for _, args := range [][]string{
		{"remap"},
		{"remap", "US-002", "--replace", "--drop"},
		{"remap", "US-002", "--drop", "--replace"},
		{"remap", "--replace", "--drop", "US-002"},
		{"remap", "US-002", "US-001"},
		{"remap", "US-002", "--force"},
		{"remap", "US-002", "--rep"},
		{"remap", "US-002", "--replace=maybe"},
		{"remap", "US-404"},
		{"remap", "US-404", "--drop"},
		{"remap", ""},
		{"remap", "../US-002"},
		{"remap", "../../outside", "--drop"},
		{"remap", "US-002/../US-002", "--replace"},
		{"remap", "--", "--drop"},
		// US-001's change reached trunk at its stop: nothing is kept for it.
		{"remap", "US-001"},
		{"remap", "US-001", "--replace"},
		{"remap", "US-001", "--drop"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := cmPerson(t, main, args...)
			if r.code == 0 {
				t.Fatalf("sdlc %s went ahead:\n%s", strings.Join(args, " "), cmSaid(r))
			}
			cmUnchanged(t, main, "US-002", trunk, kept, "sdlc "+strings.Join(args, " "))
			if wtExists(filepath.Join(filepath.Dir(main), "outside.md")) || wtExists(filepath.Join(main, ".git", "outside.md")) {
				t.Error("a story id that is no id became a path")
			}
		})
	}
	if got := cmText(cmRecord(t, main, "US-001"), "codemap_commit"); !cmFullHash.MatchString(got) {
		t.Errorf("US-001's record names %q as its map commit after the refused repairs", got)
	}
}

// A kept change is for a story that has landed. Sections stored for a story
// that is still held are the stop's to apply, and the repair leaves them.
func TestARepairOfAStoryThatHasNotLandedIsRefused(t *testing.T) {
	t.Parallel()
	main, wt, cut := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}, {"remap", "US-001", "--drop"}} {
		cmRefusedRepair(t, cmPerson(t, main, args...), args...)
		cmUnchanged(t, main, "US-001", cut, cmRefund, "sdlc "+strings.Join(args, " "))
	}
	ldStillHeld(t, main, wt)
	// And the stop still applies them.
	cmLandedWithMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
}

// THREATS T4: the kept change is read as untrusted every time. One edited
// under the git directory since it was kept is refused by the forms that
// commit, which leave it there; a person can still drop it.
func TestAKeptChangeEditedBeforeTheRepairIsRefusedAndNotCommitted(t *testing.T) {
	t.Parallel()
	for name, edited := range map[string]string{
		"a heading that escapes its section": cmAddLine + "\n## refund.go\n\nKeeps refunds.\n## billing.go\n\nGone.\n",
		"the whole map":                      "# CODEMAP\n\nEverything, rewritten.\n",
		"a replacement with no body":         cmReplaceLine + "\n## billing.go\n",
		"conflict markers":                   cmAddLine + "\n## refund.go\n\n<<<<<<< HEAD\nKeeps refunds.\n>>>>>>> theirs\n",
		"more than the bound":                cmAddLine + "\n## refund.go\n\n" + strings.Repeat("Keeps refunds, at length.\n", (2<<20)/26),
		"nothing":                            "",
	} {
		t.Run(name, func(t *testing.T) {
			main, landed := cmStopped(t)
			writeFile(t, main, ".git/sdlc/codemap/US-001.md", edited)
			for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
				cmRefusedRepair(t, cmPerson(t, main, args...), args...)
				cmUnchanged(t, main, "US-001", landed, edited, "sdlc "+strings.Join(args, " "))
			}
			if r := cmPerson(t, main, "remap", "US-001", "--drop"); r.code != 0 {
				t.Fatalf("sdlc remap US-001 --drop exited %d on a kept change that is no set: a person may still discard it\n%s", r.code, cmSaid(r))
			}
			if wtExists(cmSectionsFile(main, "US-001")) || ldTrunk(t, main) != landed {
				t.Error("--drop left the kept change, or moved trunk")
			}
		})
	}
}

// THREATS T4 and T7: a repair that reads the kept change and then cannot
// commit loses nothing. With no committer, and with a person's uncommitted
// CODEMAP.md in the checkout that has trunk checked out, it is refused, trunk
// stays, and the kept change is exactly as it was.
func TestARepairThatCannotCommitKeepsTheChange(t *testing.T) {
	t.Parallel()
	t.Run("no committer", func(t *testing.T) {
		main, landed := cmStopped(t)
		swGit(t, main, "config", "--unset", "user.email")
		swGit(t, main, "config", "--unset", "user.name")
		swGit(t, main, "config", "user.useConfigOnly", "true")
		for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
			cmRefusedRepair(t, cmPerson(t, main, args...), args...)
			cmUnchanged(t, main, "US-001", landed, cmRefund, "sdlc "+strings.Join(args, " "))
		}
		otIdentity(t, main)
		if r := cmPerson(t, main, "remap", "US-001"); r.code != 0 {
			t.Fatalf("with a committer again, sdlc remap US-001 exited %d:\n%s", r.code, cmSaid(r))
		}
	})
	t.Run("a person's uncommitted CODEMAP.md", func(t *testing.T) {
		main, landed := cmStopped(t)
		mine := cmMap + "\nA person's line, not committed.\n"
		writeFile(t, main, cmFile, mine)
		for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
			said := cmRefusedRepair(t, cmPerson(t, main, args...), args...)
			if !strings.Contains(said, cmFile) {
				t.Errorf("the refusal does not name CODEMAP.md:\n%s", said)
			}
			cmUnchanged(t, main, "US-001", landed, cmRefund, "sdlc "+strings.Join(args, " "))
			if got := wtRead(t, filepath.Join(main, cmFile)); got != mine {
				t.Errorf("the person's uncommitted CODEMAP.md is now %q", got)
			}
		}
	})
	t.Run("AC-5 trunk's CODEMAP.md became a directory", func(t *testing.T) {
		main, _ := cmStopped(t)
		swGit(t, main, "rm", "--quiet", "--", cmFile)
		writeFile(t, main, "CODEMAP.md/notes.md", "kept by hand\n")
		swGit(t, main, "add", "--", "CODEMAP.md/notes.md")
		cmCommitOnTrunk(t, main, "the map becomes a directory")
		trunk := ldTrunk(t, main)
		for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
			said := cmRefusedRepair(t, cmPerson(t, main, args...), args...)
			if !strings.Contains(said, "CODEMAP.md/notes.md") {
				t.Errorf("the refusal does not name the path that would be taken away:\n%s", said)
			}
			cmUnchanged(t, main, "US-001", trunk, cmRefund, "sdlc "+strings.Join(args, " "))
		}
	})
}

// THREATS T5: the repair is a second writer of trunk, so it takes the lock
// every landing holds. With another story's landing in front it says whom it
// waits for and commits nothing; once that landing is done it either makes
// its commit or says it did not, and loses nothing either way.
func TestARepairTakesItsTurnBehindALandingInFrontOfIt(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	inFront := otLandingInFront(t, main)

	heard := &otHeard{waiting: make(chan struct{})}
	done := make(chan result, 1)
	go func() {
		var out bytes.Buffer
		getenv := func(name string) string {
			if name == store.SessionEnv {
				return ""
			}
			return os.Getenv(name)
		}
		code := ExecuteIn(main, getenv, []string{"remap", "US-001"}, strings.NewReader(""), &out, io.Writer(heard))
		done <- result{stdout: out.String(), stderr: heard.said(), code: code}
	}()
	select {
	case <-heard.waiting:
	case r := <-done:
		t.Fatalf("the repair ended, exiting %d, while another landing held the lock, and did not say whose it waits for:\n%s", r.code, cmSaid(r))
	case <-time.After(otPatience):
		t.Fatalf("the repair said nothing of the landing in front of it:\n%s", heard.said())
	}
	if got := ldTrunk(t, main); got != landed {
		t.Fatalf("the repair moved trunk to %s while another landing held the lock", got)
	}
	if got := wtRead(t, cmSectionsFile(main, "US-001")); got != cmRefund {
		t.Fatalf("the repair changed the kept sections while it waited: %q", got)
	}
	inFront.Release()

	var r result
	select {
	case r = <-done:
	case <-time.After(otPatience):
		t.Fatalf("the repair never ended once the landing in front of it had:\n%s", heard.said())
	}
	mapped := cmText(cmRecord(t, main, "US-001"), "codemap_commit")
	switch r.code {
	case 0:
		cmMapCommit(t, main, mapped, landed, "US-001")
		if ldTrunk(t, main) != mapped || wtExists(cmSectionsFile(main, "US-001")) {
			t.Errorf("the repair exited 0, and trunk is not at its commit or the kept change is still there:\n%s", cmSaid(r))
		}
	default:
		cmUnchanged(t, main, "US-001", landed, cmRefund, "the repair that gave up waiting")
	}
}

// ---------------------------------------------------------------- the contract

// Confirm 1 and the third requirement on the plan: the contract's glossary
// line for "a person" names the repair with approve, unfreeze and ack, as a
// command that is theirs alone.
func TestTheContractsGlossaryNamesRemapAmongTheCommandsThatAreAPersonsAlone(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, contract, found := strings.Cut(string(raw), "## SDLC Contract")
	if !found {
		t.Fatal("CLAUDE.md has no SDLC Contract section")
	}
	_, glossary, found := strings.Cut(contract, "### Domain glossary")
	if !found {
		t.Fatal("the contract has no domain glossary")
	}
	glossary, _, _ = strings.Cut(glossary, "\n### ")
	_, person, found := strings.Cut(glossary, "- a person")
	if !found {
		t.Fatal("the glossary has no entry for a person")
	}
	person = strings.Join(strings.Fields(person), " ")
	for _, command := range []string{"`approve`", "`unfreeze`", "`ack`", "`remap`"} {
		if !strings.Contains(person, command) {
			t.Errorf("the glossary's entry for a person does not name %s: %s", command, person)
		}
	}
	if !strings.Contains(person, "theirs alone") || !strings.Contains(person, "security bug") {
		t.Errorf("the glossary's entry for a person no longer says the commands are theirs alone, and that a session reaching one is a security bug: %s", person)
	}
}
