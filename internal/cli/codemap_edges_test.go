package cli

// US-089, the edges of the map step, `sdlc codemap write` and `sdlc remap`
// that codemap_test.go leaves unreached: the two kinds of failure no flag of
// the repair answers, a kept file left over after its commit, a kept file that
// cannot be removed, a record that cannot be written or names a commit git
// does not have, the JSON of the repair, storing that cannot read or keep the
// sections, and the drop at a release.
//
// Each is held to what a caller sees: the exit, the code and the fix printed,
// trunk and the kept file on disk, and the record. Helpers carry a cx prefix.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/store"
)

// cxFix is the fix line of what a command printed.
func cxFix(t *testing.T, said string) string {
	t.Helper()
	for line := range strings.SplitSeq(said, "\n") {
		if fix, ok := strings.CutPrefix(line, "  fix  "); ok {
			return fix
		}
	}
	t.Fatalf("nothing printed a fix:\n%s", said)
	return ""
}

// cxReadOnly makes dir one this test's process cannot change, until the test
// ends. Where the platform or the user lets a read-only directory be written
// all the same, the test cannot arrange what it needs, and says so.
func cxReadOnly(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not refuse a file's removal from a directory marked read-only")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o644); err == nil {
		_ = os.Remove(probe)
		t.Skip("this user can write in a directory marked read-only, so nothing here can be made to fail")
	}
}

// cxWritable undoes cxReadOnly before the test ends.
func cxWritable(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func cxSectionsDir(main string) string { return filepath.Dir(cmSectionsFile(main, "US-001")) }

type cxBrokenReader struct{}

func (cxBrokenReader) Read([]byte) (int, error) { return 0, errors.New("the pipe broke") }

// ---------------------------------------------------------------- storing

// `sdlc codemap` alone is the help, which names the one subcommand, and exits
// 0 wherever it is run.
func TestCodemapAlonePrintsItsHelpAndNamesWrite(t *testing.T) {
	t.Parallel()
	r := wtRun(t, wtTempDir(t), "", "", "codemap")
	if r.code != 0 {
		t.Fatalf("sdlc codemap exited %d:\n%s", r.code, cmSaid(r))
	}
	for _, want := range []string{"Usage:", "write", cmFile} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("sdlc codemap does not print %q:\n%s", want, r.stdout)
		}
	}
}

// AC-1: what cannot be read from standard input is not stored in part. The
// command is refused with a code and a fix, nothing is kept, and the record
// says nothing was stored.
func TestAC1SectionsThatCannotBeReadFromStandardInputAreRefusedAndNothingIsStored(t *testing.T) {
	t.Parallel()
	main, _, _ := cmWalked(t)
	var out, errb strings.Builder
	code := execute(t, []string{"codemap", "write"}, cxBrokenReader{}, &out, &errb)
	said := out.String() + errb.String()
	if code == 0 {
		t.Fatalf("sdlc codemap write went ahead on input that could not be read:\n%s", said)
	}
	if !strings.Contains(said, "SDLC-E") || !strings.Contains(said, "standard input") || cxFix(t, said) == "" {
		t.Errorf("the refusal does not give a code, say standard input could not be read, and say what to do:\n%s", said)
	}
	if wtExists(cmSectionsFile(main, "US-001")) {
		t.Error("sections were kept from input that could not be read")
	}
	if events := cmMapEvents(t, main, "US-001"); len(events) != 0 {
		t.Errorf("the record has codemap events %q, and nothing was stored", events)
	}
}

// AC-1: where the git directory will not take the file, storing is refused,
// nothing is kept, the worktree stays clean, and storing again once the
// directory can be written keeps the set byte for byte.
func TestAC1SectionsThatCannotBeKeptAreRefusedAndStoringAgainKeepsThem(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	status := swGit(t, wt, "status", "--porcelain")
	dir := cxSectionsDir(main)
	cxReadOnly(t, dir)

	r := wtRun(t, wt, ldSession, cmRefund, "codemap", "write")
	said := cmRefusedRepair(t, r, "codemap", "write")
	if !strings.Contains(said, "could not be written") {
		t.Errorf("the refusal does not say the sections could not be written:\n%s", said)
	}
	if wtExists(cmSectionsFile(main, "US-001")) {
		t.Error("a set was kept where the directory would take none")
	}
	if events := cmMapEvents(t, main, "US-001"); len(events) != 0 {
		t.Errorf("the record has codemap events %q, and nothing was stored", events)
	}
	if got := swGit(t, wt, "status", "--porcelain"); got != status {
		t.Errorf("the refused store changed the worktree:\nbefore\n%s\nafter\n%s", status, got)
	}

	cxWritable(t, dir)
	cmStores(t, wt, ldSession, cmRefund)
	if got := wtRead(t, cmSectionsFile(main, "US-001")); got != cmRefund {
		t.Errorf("the set kept on the second try is %q, want what was stored", got)
	}
}

// AC-1: storing is said on the story's record. Where the record cannot be
// written the command is refused with a code, and the worktree is left clean,
// so the stop is not held up by anything the refusal left there.
func TestAC1StoringSectionsWhoseRecordCannotBeWrittenIsRefusedAndTheWorktreeStaysClean(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	status := swGit(t, wt, "status", "--porcelain")
	cxReadOnly(t, filepath.Dir(wtRecordPath(main, "US-001")))

	said := cmRefusedRepair(t, wtRun(t, wt, ldSession, cmRefund, "codemap", "write"), "codemap", "write")
	if strings.Contains(said, "Stored 1 section") {
		t.Errorf("the refused store says it stored the section:\n%s", said)
	}
	if got := swGit(t, wt, "status", "--porcelain"); got != status {
		t.Errorf("the refused store changed the worktree:\nbefore\n%s\nafter\n%s", status, got)
	}
}

// A directory that is no sdlc project has no story to store sections for or
// to repair: both commands are refused with a code, and nothing is written.
func TestStoringAndRepairingOutsideAProjectAreRefusedAndWriteNothing(t *testing.T) {
	t.Parallel()
	dir := wtTempDir(t)
	for _, tc := range []struct {
		stdin string
		args  []string
	}{
		{cmRefund, []string{"codemap", "write"}},
		{"", []string{"remap", "US-001"}},
		{"", []string{"remap", "US-001", "--drop"}},
	} {
		r := wtRun(t, dir, "", tc.stdin, tc.args...)
		if r.code == 0 {
			t.Errorf("sdlc %s went ahead outside a project:\n%s", strings.Join(tc.args, " "), cmSaid(r))
		}
		if !strings.Contains(cmSaid(r), "SDLC-E") {
			t.Errorf("sdlc %s was refused outside a project without a code:\n%s", strings.Join(tc.args, " "), cmSaid(r))
		}
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("the directory holds %d entries after the refusals (%v), want none", len(entries), err)
	}
}

// A story worked in the main checkout is not landed by a stop, so nothing
// would put stored sections on trunk. Storing is refused, the fix says to edit
// CODEMAP.md in place, and nothing is kept.
func TestStoringSectionsForAStoryNotInAWorktreeOfItsOwnIsRefusedWithTheMapEditedInPlace(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wtStarted(t, main, wtSessionMain, "US-001")

	r := wtRun(t, main, wtSessionMain, cmRefund, "codemap", "write")
	said := cmRefusedRepair(t, r, "codemap", "write")
	if fix := cxFix(t, said); !strings.Contains(fix, "edit "+cmFile+" in place") {
		t.Errorf("the fix does not say to edit CODEMAP.md in place: %s", fix)
	}
	if !strings.Contains(said, "worktree of its own") {
		t.Errorf("the refusal does not say the story is not worked in a worktree of its own:\n%s", said)
	}
	if wtExists(cmSectionsFile(main, "US-001")) {
		t.Error("sections were kept for a story no stop lands")
	}
}

// What the checkout that held a story stored for the map goes with the claim
// when a person releases it: the kept file is removed and the record says the
// sections were dropped with no commit. Where the file cannot be removed the
// release still stands, the file stays, and the record does not say it went.
func TestReleasingAStoryDropsItsKeptSectionsAndTheRecordSaysSo(t *testing.T) {
	t.Parallel()
	for _, removable := range []bool{true, false} {
		name := map[bool]string{true: "the kept file can be removed", false: "the kept file cannot be removed"}[removable]
		t.Run(name, func(t *testing.T) {
			main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			wt := wtWorktree(t, main, "wt")
			wtStarted(t, wt, wtSessionWorktree, "US-001")
			wtMust(t, wt, "", "", "stop")
			writeFile(t, main, ".git/sdlc/codemap/US-001.md", cmRefund)
			if !removable {
				cxReadOnly(t, cxSectionsDir(main))
			}

			r := wtMust(t, main, "", "", "release", "US-001", "--reason", "moving it")
			if !strings.Contains(r.stdout, "Released US-001") {
				t.Errorf("release does not say it released US-001:\n%s", cmSaid(r))
			}
			events := cmMapEvents(t, main, "US-001")
			if removable {
				if wtExists(cmSectionsFile(main, "US-001")) {
					t.Error("the sections kept for the released story are still there")
				}
				if len(events) != 1 || !strings.Contains(events[0], "dropped") || !strings.Contains(events[0], "released") {
					t.Errorf("the record's codemap events are %q, want one saying the sections were dropped because the story was released", events)
				}
				return
			}
			if got := wtRead(t, cmSectionsFile(main, "US-001")); got != cmRefund {
				t.Errorf("the kept file is %q after a drop that could not be made, want it as it was", got)
			}
			if len(events) != 0 {
				t.Errorf("the record says %q of sections that are still kept", events)
			}
		})
	}
}

// ---------------------------------------------------------------- the stop

// AC-6: where trunk's map already holds every kept section exactly as stored,
// the step has nothing to commit. The landing stands, and the only repair
// offered is the drop, which commits nothing; the plain repair is refused the
// same way, and the drop then ends it.
func TestAC6AStopWhoseSectionsTrunkHoldsAlreadyLandsAndOffersOnlyTheDrop(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	swCommitFile(t, main, cmFile, cmMap+"\n"+cmRefundSection, "another commit puts the same section on trunk")

	said := cmLandedWithoutMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	fix := cxFix(t, said)
	if !strings.Contains(fix, `"sdlc remap US-001 --drop"`) || !strings.Contains(fix, "holds the text already") {
		t.Errorf("the fix does not offer the drop because trunk holds the text already: %s", fix)
	}
	if strings.Contains(fix, `"sdlc remap US-001 --replace"`) || strings.Contains(fix, `"sdlc remap US-001"`) {
		t.Errorf("the fix offers a form of the repair that has nothing to commit: %s", fix)
	}
	if !strings.Contains(said, "refund.go") {
		t.Errorf("the stop does not name the section trunk holds already:\n%s", said)
	}
	if events := cmMapEvents(t, main, "US-001"); len(events) == 0 || !strings.Contains(events[len(events)-1], "exactly as it was stored") {
		t.Errorf("the record's codemap events are %q, want the last to say trunk holds the sections exactly as stored", events)
	}

	trunk := ldTrunk(t, main)
	refused := cmRefusedRepair(t, cmPerson(t, main, "remap", "US-001"), "remap", "US-001")
	if !strings.Contains(cxFix(t, refused), `"sdlc remap US-001 --drop"`) {
		t.Errorf("the plain repair's refusal does not offer the drop:\n%s", refused)
	}
	cmUnchanged(t, main, "US-001", trunk, cmRefund, "the plain repair of a change trunk holds")

	if r := cmPerson(t, main, "remap", "US-001", "--drop"); r.code != 0 {
		t.Fatalf("sdlc remap US-001 --drop exited %d:\n%s", r.code, cmSaid(r))
	}
	if wtExists(cmSectionsFile(main, "US-001")) || ldTrunk(t, main) != trunk {
		t.Error("the drop left the kept change, or moved trunk")
	}
}

// AC-6: where a section on trunk the set would replace holds a heading
// written by underlining, where it ends cannot be told. The landing stands;
// the fix says a person edits the map by hand and then drops the kept copy,
// and that --replace is refused for the same reason; and it is.
func TestAC6AStopWhoseSectionOnTrunkCannotBeToldWhereItEndsLandsAndAPersonEditsTheMap(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmBilling)
	swCommitFile(t, main, cmFile, strings.Replace(cmMap, "Keeps the accounts.\n", "Keeps the accounts.\n\nInvoices\n--------\n\nKept by hand.\n", 1),
		"a person underlines a heading in billing.go's section")

	said := cmLandedWithoutMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	if !strings.Contains(said, "billing.go") || !strings.Contains(said, "cannot be told") {
		t.Errorf("the stop does not name the section whose end cannot be told:\n%s", said)
	}
	fix := cxFix(t, said)
	for _, want := range []string{"by hand", `"sdlc remap US-001 --drop"`, "--replace form is refused"} {
		if !strings.Contains(fix, want) {
			t.Errorf("the fix does not say %q: %s", want, fix)
		}
	}

	trunk := ldTrunk(t, main)
	for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
		refused := cmRefusedRepair(t, cmPerson(t, main, args...), args...)
		if !strings.Contains(refused, "cannot be told") {
			t.Errorf("sdlc %s is not refused because the section's end cannot be told:\n%s", strings.Join(args, " "), refused)
		}
		cmUnchanged(t, main, "US-001", trunk, cmBilling, "sdlc "+strings.Join(args, " "))
	}
	if got, _ := cmTrunkMap(t, main); !strings.Contains(got, "Kept by hand.\n") {
		t.Errorf("a person's lines under the underlined heading are gone from trunk's map:\n%s", got)
	}
}

// ---------------------------------------------------------------- the repair

// AC-4: once the record names the map's commit, a kept file is a leftover. No
// notice is said of it, the forms that commit refuse it by naming that commit
// and offering the drop, and the drop removes it with nothing committed.
func TestAC4AKeptFileLeftOverAfterTheRecordNamesTheMapCommitIsOnlyDropped(t *testing.T) {
	t.Parallel()
	main, wt, _ := cmWalked(t)
	cmStores(t, wt, ldSession, cmRefund)
	mapped := cmLandedWithMap(t, main, "US-001", wtRun(t, wt, ldSession, "", "stop"))
	writeFile(t, main, ".git/sdlc/codemap/US-001.md", cmRefund)

	if said := cmSaid(cmPerson(t, main, "status")); strings.Contains(said, "sdlc remap") {
		t.Errorf("status speaks of a repair for a story whose record names its map commit:\n%s", said)
	}
	for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
		said := cmRefusedRepair(t, cmPerson(t, main, args...), args...)
		if !strings.Contains(said, mapped[:12]) || !strings.Contains(said, "left over") {
			t.Errorf("sdlc %s does not say the kept file is left over from %s:\n%s", strings.Join(args, " "), mapped[:12], said)
		}
		if fix := cxFix(t, said); !strings.Contains(fix, `"sdlc remap US-001 --drop"`) {
			t.Errorf("the fix does not offer the drop: %s", fix)
		}
		if ldTrunk(t, main) != mapped || wtRead(t, cmSectionsFile(main, "US-001")) != cmRefund {
			t.Errorf("sdlc %s moved trunk or changed the kept file", strings.Join(args, " "))
		}
	}
	if r := cmPerson(t, main, "remap", "US-001", "--drop"); r.code != 0 {
		t.Fatalf("sdlc remap US-001 --drop exited %d:\n%s", r.code, cmSaid(r))
	}
	if wtExists(cmSectionsFile(main, "US-001")) || ldTrunk(t, main) != mapped {
		t.Error("the drop left the kept file, or moved trunk")
	}
	if got := cmText(cmRecord(t, main, "US-001"), "codemap_commit"); got != mapped {
		t.Errorf("the drop changed the record's map commit to %q, want %s", got, mapped)
	}
}

// AC-6: a kept file that cannot be removed. The drop is refused and keeps it;
// the plain repair still makes the commit and names it on the record, and the
// file left behind is then a leftover, of which nothing is said and which the
// forms that commit refuse.
func TestAC6AKeptFileThatCannotBeRemovedIsRefusedByTheDropAndLeftOverByTheRepair(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	dir := cxSectionsDir(main)
	cxReadOnly(t, dir)

	said := cmRefusedRepair(t, cmPerson(t, main, "remap", "US-001", "--drop"), "remap", "US-001", "--drop")
	if !strings.Contains(said, "could not be removed") {
		t.Errorf("the drop's refusal does not say the file could not be removed:\n%s", said)
	}
	cmUnchanged(t, main, "US-001", landed, cmRefund, "the drop that could not remove the file")
	cmSaysMapWaits(t, cmPerson(t, main, "status"), "US-001")

	r := cmPerson(t, main, "remap", "US-001")
	if r.code != 0 {
		t.Fatalf("sdlc remap US-001 exited %d, want the commit made though the file cannot be removed:\n%s", r.code, cmSaid(r))
	}
	mapped := cmText(cmRecord(t, main, "US-001"), "codemap_commit")
	cmMapCommit(t, main, mapped, landed, "US-001")
	if ldTrunk(t, main) != mapped {
		t.Errorf("trunk is not at the map's commit %s", mapped)
	}
	if got := wtRead(t, cmSectionsFile(main, "US-001")); got != cmRefund {
		t.Errorf("the file that could not be removed is now %q", got)
	}
	if said := cmSaid(cmPerson(t, main, "status")); strings.Contains(said, "sdlc remap") {
		t.Errorf("status speaks of a repair after the record named the map commit:\n%s", said)
	}
	again := cmRefusedRepair(t, cmPerson(t, main, "remap", "US-001"), "remap", "US-001")
	if !strings.Contains(again, mapped[:12]) {
		t.Errorf("the second repair does not name the commit the file is left over from:\n%s", again)
	}

	cxWritable(t, dir)
	if r := cmPerson(t, main, "remap", "US-001", "--drop"); r.code != 0 || wtExists(cmSectionsFile(main, "US-001")) {
		t.Errorf("the drop, once the file can be removed, exited %d and left it there: %v\n%s", r.code, wtExists(cmSectionsFile(main, "US-001")), cmSaid(r))
	}
}

// AC-6: the repair's commit is made and trunk moved, and then the record
// cannot be written to name it. The repair fails saying the commit stands and
// naming it; the kept change stays, so the notice is still said; and once the
// record can be written the plain repair finds trunk holds the text already
// and offers the drop.
func TestAC6ARepairWhoseRecordCannotBeWrittenSaysTheCommitStandsAndKeepsTheChange(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	stories := filepath.Dir(wtRecordPath(main, "US-001"))
	cxReadOnly(t, stories)

	r := cmPerson(t, main, "remap", "US-001")
	said := cmRefusedRepair(t, r, "remap", "US-001")
	tip := ldTrunk(t, main)
	if tip == landed {
		t.Fatalf("trunk did not move, and this test is of the record failing after the commit:\n%s", said)
	}
	cmMapCommit(t, main, tip, landed, "US-001")
	if !strings.Contains(said, tip[:12]) || !strings.Contains(said, "could not be written") {
		t.Errorf("the repair does not say the commit %s stands and the record could not be written:\n%s", tip[:12], said)
	}
	if got := wtRead(t, cmSectionsFile(main, "US-001")); got != cmRefund {
		t.Errorf("the kept change is %q after the record could not be written, want it as it was", got)
	}

	cxWritable(t, stories)
	if got := cmText(cmRecord(t, main, "US-001"), "codemap_commit"); got != "" {
		t.Errorf("the record names %s, and it could not be written", got)
	}
	cmSaysMapWaits(t, cmPerson(t, main, "status"), "US-001")
	again := cmRefusedRepair(t, cmPerson(t, main, "remap", "US-001"), "remap", "US-001")
	if !strings.Contains(cxFix(t, again), `"sdlc remap US-001 --drop"`) || !strings.Contains(again, "exactly as it was stored") {
		t.Errorf("the repair after the commit stood does not say trunk holds the text and offer the drop:\n%s", again)
	}
	if ldTrunk(t, main) != tip {
		t.Error("the second repair moved trunk")
	}
}

// AC-6: the drop is written on the record before the file goes, so that a
// drop nobody recorded cannot happen. Where the record cannot be written the
// drop is refused, and the kept change and trunk are as they were; once it
// can, the drop goes ahead.
func TestAC6ADropWhoseRecordCannotBeWrittenIsRefusedAndKeepsTheChange(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	stories := filepath.Dir(wtRecordPath(main, "US-001"))
	cxReadOnly(t, stories)

	cmRefusedRepair(t, cmPerson(t, main, "remap", "US-001", "--drop"), "remap", "US-001", "--drop")
	if ldTrunk(t, main) != landed || wtRead(t, cmSectionsFile(main, "US-001")) != cmRefund {
		t.Error("the refused drop moved trunk or changed the kept change")
	}

	cxWritable(t, stories)
	cmSaysMapWaits(t, cmPerson(t, main, "status"), "US-001")
	if r := cmPerson(t, main, "remap", "US-001", "--drop"); r.code != 0 || wtExists(cmSectionsFile(main, "US-001")) {
		t.Errorf("the drop, once the record can be written, exited %d and the kept change is there: %v\n%s", r.code, wtExists(cmSectionsFile(main, "US-001")), cmSaid(r))
	}
	if events := cmMapEvents(t, main, "US-001"); len(events) == 0 || !strings.Contains(events[len(events)-1], "--drop") {
		t.Errorf("the record's codemap events are %q, want the last to be the drop", events)
	}
}

// AC-6: the record names a cut git does not have, so what the bookkeeper read
// cannot be worked out. The repair is refused with the plain form's fix and
// changes nothing.
func TestAC6ARepairWhoseRecordNamesACutGitDoesNotHaveIsRefusedAndKeepsTheChange(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	path := wtRecordPath(main, "US-001")
	var rec map[string]any
	wtReadJSON(t, path, &rec)
	rec["cut"] = strings.Repeat("1", 40)
	wtWriteJSON(t, path, rec)

	for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
		said := cmRefusedRepair(t, cmPerson(t, main, args...), args...)
		if fix := cxFix(t, said); !strings.Contains(fix, `"sdlc remap US-001"`) || !strings.Contains(fix, "nothing is lost") {
			t.Errorf("sdlc %s does not say to put it right and run the plain repair: %s", strings.Join(args, " "), fix)
		}
		cmUnchanged(t, main, "US-001", landed, cmRefund, "sdlc "+strings.Join(args, " "))
	}
}

// A record that cannot be read is no ground for a commit: the repair is
// refused with a code, and trunk and the kept change are as they were.
func TestARepairOfAStoryWhoseRecordCannotBeReadIsRefusedAndKeepsTheChange(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	writeFile(t, main, ".sdlc/stories/US-001/"+filepath.Base(wtRecordPath(main, "US-001")), "{ not a record")

	for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}} {
		r := cmPerson(t, main, args...)
		if r.code == 0 || !strings.Contains(cmSaid(r), "SDLC-E") {
			t.Errorf("sdlc %s exited %d on a record that cannot be read, want a refusal with a code:\n%s", strings.Join(args, " "), r.code, cmSaid(r))
		}
		if ldTrunk(t, main) != landed || wtRead(t, cmSectionsFile(main, "US-001")) != cmRefund {
			t.Errorf("sdlc %s moved trunk or changed the kept change", strings.Join(args, " "))
		}
	}
}

// What is in the kept file's place is no file: every form of the repair is
// refused with a code, trunk stays, and what is there is left alone.
func TestARepairWhereTheKeptSectionsAreNoFileIsRefusedWhateverTheForm(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	if err := os.Remove(cmSectionsFile(main, "US-001")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, main, ".git/sdlc/codemap/US-001.md/inside.txt", "in the way\n")

	for _, args := range [][]string{{"remap", "US-001"}, {"remap", "US-001", "--replace"}, {"remap", "US-001", "--drop"}} {
		said := cmRefusedRepair(t, cmPerson(t, main, args...), args...)
		if !strings.Contains(said, "not a plain file") {
			t.Errorf("sdlc %s does not say the kept sections are not a plain file:\n%s", strings.Join(args, " "), said)
		}
		if ldTrunk(t, main) != landed || !wtExists(filepath.Join(cmSectionsFile(main, "US-001"), "inside.txt")) {
			t.Errorf("sdlc %s moved trunk or took away what was in the kept file's place", strings.Join(args, " "))
		}
	}
}

// AC-4 with --json: the repair's standard output is one JSON object naming the
// story and the map's commit, the one the record names; the drop's says it
// dropped and names no commit.
func TestAC4TheRepairWithJSONNamesTheMapCommitAndTheDropSaysItDropped(t *testing.T) {
	t.Parallel()
	t.Run("AC-4 the plain repair", func(t *testing.T) {
		t.Parallel()
		main, landed := cmStopped(t)
		r := cmPerson(t, main, "remap", "US-001", "--json")
		if r.code != 0 {
			t.Fatalf("sdlc remap US-001 --json exited %d:\n%s", r.code, cmSaid(r))
		}
		got := decode[remapPayload](t, r)
		mapped := cmText(cmRecord(t, main, "US-001"), "codemap_commit")
		if !got.OK || got.Story != "US-001" || got.Dropped || got.CodemapCommit != mapped {
			t.Errorf("remap --json = %+v, want US-001 and the commit the record names, %s", got, mapped)
		}
		cmMapCommit(t, main, got.CodemapCommit, landed, "US-001")
	})
	t.Run("AC-6 the drop", func(t *testing.T) {
		t.Parallel()
		main, landed := cmStopped(t)
		r := cmPerson(t, main, "remap", "US-001", "--drop", "--json")
		if r.code != 0 {
			t.Fatalf("sdlc remap US-001 --drop --json exited %d:\n%s", r.code, cmSaid(r))
		}
		if got := decode[remapPayload](t, r); !got.OK || got.Story != "US-001" || !got.Dropped || got.CodemapCommit != "" {
			t.Errorf("remap --drop --json = %+v, want US-001 dropped and no commit", got)
		}
		if wtExists(cmSectionsFile(main, "US-001")) || ldTrunk(t, main) != landed {
			t.Error("the drop left the kept change, or moved trunk")
		}
	})
}

// ---------------------------------------------------------------- the notice

// The notice must not break the command it comes before. Where the backlog
// cannot be read it says nothing, and the command says exactly what it says
// with nothing kept.
func TestTheNoticeSaysNothingWhereTheBacklogCannotBeRead(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	writeFile(t, main, "user_stories.json", "{ not a backlog")
	without := cmPerson(t, main, "status")

	writeFile(t, main, ".git/sdlc/codemap/US-001.md", cmRefund)
	with := cmPerson(t, main, "status")
	if strings.Contains(cmSaid(with), "sdlc remap") {
		t.Errorf("a notice was said from a backlog that cannot be read:\n%s", cmSaid(with))
	}
	if with != without {
		t.Errorf("with sections kept, status said\n%+v\nand without them\n%+v", with, without)
	}
}

// ---------------------------------------------------------------- the step's parts

// The map step reads trunk's tip first. Where the branch it is told is trunk
// is not there, it fails as something in the way, commits nothing and keeps
// the change, and the warning built from it offers the plain repair.
func TestTheMapStepWithNoTrunkBranchFailsAsBlockedAndKeepsTheChange(t *testing.T) {
	t.Parallel()
	main, landed := cmStopped(t)
	cfg, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(&config.Project{Root: main, Config: cfg})
	step := mapStep{s: s, root: main, trunk: "no-such-branch", id: "US-001", by: "a test", kept: keptAt(s, "US-001")}

	commit, what, failed := step.run(t.Context())
	if failed == nil || failed.kind != mapBlocked {
		t.Fatalf("the step with no trunk branch gave %q, %+v, want it blocked", commit, failed)
	}
	if commit != "" || what != `adds "refund.go"` {
		t.Errorf("the step gave the commit %q and the sections %q, want none and the one kept", commit, what)
	}
	if fix := step.notOnTrunk(failed).Fix; !strings.Contains(fix, `"sdlc remap US-001"`) || !strings.Contains(fix, "nothing is lost") {
		t.Errorf("the warning does not offer the plain repair: %s", fix)
	}
	if ldTrunk(t, main) != landed || wtRead(t, cmSectionsFile(main, "US-001")) != cmRefund {
		t.Error("the step that could not read trunk moved it or changed the kept change")
	}
}

// A record that names no commit has no map at it, and that is no error: an
// older record keeps no cut.
func TestAMapIsNothingAtACommitTheRecordDoesNotName(t *testing.T) {
	t.Parallel()
	got, err := mapAt(t.Context(), wtTempDir(t), "")
	if got != nil || err != nil {
		t.Errorf("the map at no commit is %q, %v, want nothing and no error", got, err)
	}
}

// Where no checkout can be found, where the change is kept is said without a
// path: one worked out from nothing would name a place no file is at.
func TestWhereTheChangeIsKeptIsSaidWithNoPathWhereNoCheckoutIsFound(t *testing.T) {
	t.Parallel()
	dir := wtTempDir(t)
	if got := keptAt(store.New(&config.Project{Root: dir}), "US-001"); got != "in the repository's git directory" {
		t.Errorf("keptAt outside a checkout says %q, want no path", got)
	}
}
