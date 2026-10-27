// US-099 end to end: a story whose commits add the markers of an unresolved
// conflict does not land, through the built binary and the shipped hook, in a
// real repository.
//
// A session works a story in its worktree. A conflict half resolved is how a
// person who rebases by hand when the stop cannot ends up with one: git stops
// on a conflict, the person adds the file as it stands and carries the rebase
// on. The commit gate run again and `sdlc stop` then refuse it under
// SDLC-E0080, naming the file and the line of each marker, and trunk stays
// where it was (AC-1, AC-4). A file that already held a set on trunk lands
// when the story adds none, and a story that moves such a file lands (AC-2).
// A path the configuration lists is one where a story may add the lines, and
// `sdlc doctor` lists the paths (AC-3).
//
// No marker line is written out in this file: they are built at run time, so
// that this story's own commits do not hold a set for its own check to find.
// Helpers carry a cmk prefix, for conflict markers.

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

const (
	cmkCode = "SDLC-E0080"
	cmkKey  = "conflict_markers_intended"
)

func cmkOpen(label string) string  { return strings.Repeat("<", 7) + " " + label }
func cmkSep() string               { return strings.Repeat("=", 7) }
func cmkClose(label string) string { return strings.Repeat(">", 7) + " " + label }

func cmkText(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func cmkFiller(from, to int) []string {
	var lines []string
	for i := from; i <= to; i++ {
		lines = append(lines, "entry "+strconv.Itoa(i))
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

func cmkSet() []string {
	return []string{cmkOpen("HEAD"), "our side", cmkSep(), "their side", cmkClose("topic (a change)")}
}

// cmkMarkerLines is the number of every marker line in body, counted from 1.
func cmkMarkerLines(body string) []int {
	var found []int
	for i, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, cmkOpen("")) || strings.HasPrefix(line, strings.Repeat("|", 7)+" ") ||
			line == cmkSep() || strings.HasPrefix(line, cmkClose("")) {
			found = append(found, i+1)
		}
	}
	return found
}

// cmkNames reports whether out names the line n as a number on its own.
func cmkNames(out string, n int) bool {
	return regexp.MustCompile(`(^|[^0-9A-Za-z])` + strconv.Itoa(n) + `($|[^0-9A-Za-z])`).MatchString(out)
}

// cmkRefusal holds what a command printed to being SDLC-E0080's refusal that
// names the file and every marker line of body, in a fix that sends a person
// to the setting and an assistant to a person.
func cmkRefusal(t *testing.T, out string, code int, path, body string) {
	t.Helper()
	if code == 0 || !strings.Contains(out, cmkCode) {
		t.Fatalf("exit %d, want a refusal under %s:\n%s", code, cmkCode, out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("the refusal does not name %s:\n%s", path, out)
	}
	lines := cmkMarkerLines(body)
	if len(lines) < 3 {
		t.Fatalf("precondition: %s holds %d marker lines, want a set", path, len(lines))
	}
	for _, n := range lines {
		if !cmkNames(out, n) {
			t.Errorf("the refusal does not name line %d of %s, a marker:\n%s", n, path, out)
		}
	}
	for _, want := range []string{cmkKey, "sdlc escalate"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal's fix does not say %q:\n%s", want, out)
		}
	}
}

// cmkWorked takes the story through the gates as ontoStory.worked does, with
// change made in the worktree at the implementation. It stops at the commit
// gate if that refuses, and returns what the gate printed; called again, it
// goes on from the gate the story is at. Every other gate has to pass.
func cmkWorked(s *ontoStory, change func()) (out string, refused bool) {
	s.t.Helper()
	again := s.nextGate() == string(model.GateCommit)
	for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
		switch model.Gate(gate) {
		case model.GateDoR, model.GateDesignReview, model.GateVerifierReview, model.GateCodeReview:
			// Nothing to produce: the reviewers of the gate are added below.
		case model.GateAnalysis:
			s.stores("researcher", "analysis", "# Analysis\n\nAC-1 is testable.\n")
			s.stores("researcher", "threats", "# Threats\n\nNothing crosses a trust boundary.\n")
		case model.GateTestsFrozen:
			s.writes("sdet", s.test, "package main\n\n// AC-1 of "+s.story+"\n")
			s.stores("sdet", "test_plan", "# Test plan\n\nAC-1 -> TestRejectsZero\n")
			s.run("sdet", "freeze")
		case model.GatePlan:
			s.stores("implementer", "plan", "# Plan\n\nOne step.\n")
		case model.GateImplementation:
			s.writes("implementer", s.source, "package main\n\n// "+s.story+"\n")
			if change != nil {
				change()
			}
		case model.GateVerification:
			s.stores("verifier", "verification", "# Verification\n\nEvery command green.\n")
		case model.GateCommit:
			if again {
				// A person's session records the reviews of the work as it now
				// stands before it commits it; the hook refuses a commit of
				// work that changed since the reviewers read it.
				gateReviewedAgain(s)
			}
			git(s.t, s.tree, "add", "-A")
			s.do("", "Bash", running(s.at.in(s.tree, "git commit -m "+s.story)))
			git(s.t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "--allow-empty", "-m", s.story)
		case model.GateRetro:
			s.stores("bookkeeper", "retro", "# Retro\n\nNo deviations.\n")
		}
		for _, r := range model.ReviewersFor(model.Gate(gate)) {
			if r.WhenDoRAdvocate {
				continue
			}
			s.do(r.Role, "Bash", running(s.at.in(s.tree, "sdlc review add "+gate+" "+r.Role+" approve")))
			s.here().runWith(s.t, "# "+r.Role+"\n", "review", "add", gate, r.Role, "approve")
		}
		if model.Gate(gate) == model.GateCommit {
			s.do("", "Bash", running(s.at.in(s.tree, "sdlc gate commit pass --note worked")))
			out, code := s.here().try(s.t, "gate", "commit", "pass", "--note", "worked by the session")
			if code != 0 {
				return out, true
			}
			continue
		}
		s.run("", "gate", gate, "pass", "--note", "worked by the session")
	}
	return "", false
}

// cmkRecordedCommit is the last commit the story's record names, "" for none.
func cmkRecordedCommit(t *testing.T, main checkout) string {
	t.Helper()
	var r struct {
		Commit string `json:"commit"`
	}
	readJSON(t, main.record("US-001"), &r)
	return r.Commit
}

// cmkLands is the bookkeeper's commit and the stop, which land the story on
// trunk by fast-forward to its tip.
func cmkLands(s *ontoStory, main checkout) {
	s.t.Helper()
	head := landBookkeeping(s.t, s.treeSession, true)
	s.run("", "stop")
	if got := landTrunk(s.t, main); got != head {
		s.t.Errorf("trunk is at %s after the stop, want the story's tip %s", got, head)
	}
	if got := statuses(s.t, main.backlog())["US-001"]; got != "done" {
		s.t.Errorf("US-001 is %s after the stop, want done", got)
	}
}

// cmkHolds reports whether trunk's file at rel holds the lines of a set.
func cmkHolds(t *testing.T, main checkout, rel string) bool {
	t.Helper()
	return len(cmkMarkerLines(gitOut(t, main.dir, "show", "refs/heads/main:"+rel))) >= 3
}

// cmkLedgerRepository is a project whose trunk holds ledger.txt, sixty lines,
// and a story begun on it.
func cmkLedgerRepository(t *testing.T, binary string) (checkout, *ontoStory, []string) {
	t.Helper()
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	ledger := cmkFiller(1, 60)
	writeFile(t, filepath.Join(main.dir, "ledger.txt"), cmkText(ledger...))
	commitEverything(t, main.dir, "a ledger")
	return main, ontoBegun(t, binary, main, sessionFirst, "US-001"), ledger
}

// cmkEdited is ledger with line 37 replaced.
func cmkEdited(ledger []string, line string) string {
	edited := append([]string(nil), ledger...)
	edited[36] = line
	return cmkText(edited...)
}

// cmkRebasedHalfWay is a finished story, rebased by hand onto a trunk that
// changed the same line of the ledger: git stops on the conflict, and the
// person adds the file as git left it, markers and all, and carries on. It
// returns trunk's commit and the ledger as the rebase left it.
func cmkRebasedHalfWay(t *testing.T, main checkout, s *ontoStory, ledger []string) (on, held string) {
	t.Helper()
	if out, refused := cmkWorked(s, func() {
		writeFile(t, filepath.Join(s.tree, "ledger.txt"), cmkEdited(ledger, "entry 37: edited by the story"))
	}); refused {
		t.Fatalf("precondition: the commit gate refused a story with no markers:\n%s", out)
	}
	landBookkeeping(t, s.treeSession, true)
	writeFile(t, filepath.Join(main.dir, "ledger.txt"), cmkEdited(ledger, "entry 37: edited on trunk"))
	on = commitPaths(t, main.dir, "another story edits the ledger", "ledger.txt")

	cmd := exec.CommandContext(t.Context(), "git", "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "main")
	cmd.Dir = s.tree
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("precondition: the rebase did not stop on a conflict:\n%s", out)
	}
	held = readFile(t, filepath.Join(s.tree, "ledger.txt"))
	if len(cmkMarkerLines(held)) < 3 {
		t.Fatalf("precondition: git left no set in ledger.txt:\n%s", held)
	}
	git(t, s.tree, "add", "--", "ledger.txt")
	git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "-c", "core.editor=true", "rebase", "--continue")
	return on, held
}

// ---------------------------------------------------------------- AC-1, AC-4

// AC-1: the commit gate refuses a story that adds a set, names the file and
// the line of each marker, records nothing, and passes once the file is put
// right; the story then lands.
func TestAC1TheCommitGateRefusesAStoryThatAddsAConflictsMarkersAndTheStoryLandsOncePutRight(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	cut := landTrunk(t, main)
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")

	body := cmkText(cmkJoin(cmkFiller(1, 36), cmkSet(), cmkFiller(37, 40))...)
	out, refused := cmkWorked(s, func() { writeFile(t, filepath.Join(s.tree, "notes.txt"), body) })
	if !refused {
		t.Fatal("the commit gate passed a story that adds a set")
	}

	cmkRefusal(t, out, 1, "notes.txt", body)
	if got := s.nextGate(); got != string(model.GateCommit) {
		t.Errorf("after the refusal the story is at %q, want the commit gate still to pass", got)
	}
	if got := cmkRecordedCommit(t, main); got != "" {
		t.Errorf("the refused commit gate recorded the range up to %s", got)
	}
	if got := landTrunk(t, main); got != cut {
		t.Errorf("trunk moved to %s", got)
	}

	writeFile(t, filepath.Join(s.tree, "notes.txt"), cmkText(cmkFiller(1, 40)...))
	if out, refused := cmkWorked(s, nil); refused {
		t.Fatalf("the commit gate refused the file put right:\n%s", out)
	}
	cmkLands(s, main)
}

// AC-1 and AC-4: a person rebases the finished story onto a trunk that moved
// by hand, leaves the conflict half resolved, and runs `sdlc stop`: refused
// under its own code, naming the file and the line of each marker as the file
// has them, and not as stale reviews or a story behind trunk. Trunk does not
// move and the story is still held. Resolved, with the reviews and the commit
// gate again, the same story lands.
func TestAC1AndAC4SdlcStopRefusesAStoryRebasedByHandWithTheConflictLeftHalfResolved(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main, s, ledger := cmkLedgerRepository(t, binary)
	on, held := cmkRebasedHalfWay(t, main, s, ledger)

	s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
	out, code := s.here().try(t, "stop")
	cmkRefusal(t, out, code, "ledger.txt", held)
	if got := landTrunk(t, main); got != on {
		t.Errorf("the refused stop moved trunk to %s", got)
	}
	if got := statuses(t, main.backlog())["US-001"]; got == "done" {
		t.Error("a story that did not land is done")
	}
	if !exists(s.tree) {
		t.Error("the refused stop took the story's worktree away")
	}

	// The person resolves the file, and the story goes through again.
	writeFile(t, filepath.Join(s.tree, "ledger.txt"), cmkEdited(ledger, "entry 37: resolved"))
	git(t, s.tree, "add", "--", "ledger.txt")
	gateReviewedAgain(s)
	s.do("", "Bash", running(s.at.in(s.tree, "git commit -m resolve")))
	git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "resolve the ledger")
	head := headOf(t, s.tree)
	gateCommitAgain(s)
	gateLands(s, main, head)
}

// AC-1 and AC-4: the commit gate run again after the same rebase refuses the
// same way, and records nothing: the record still names the range the gate
// recorded before the rebase.
func TestAC1AndAC4TheCommitGateRunAgainAfterARebaseByHandRefusesAConflictLeftHalfResolved(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main, s, ledger := cmkLedgerRepository(t, binary)
	on, held := cmkRebasedHalfWay(t, main, s, ledger)
	recordedBefore := cmkRecordedCommit(t, main)
	gateReviewedAgain(s)

	s.do("", "Bash", running(s.at.in(s.tree, "sdlc gate commit pass --note again")))
	out, code := s.here().try(t, "gate", "commit", "pass", "--note", "again")
	cmkRefusal(t, out, code, "ledger.txt", held)
	if got := cmkRecordedCommit(t, main); got != recordedBefore || got == headOf(t, s.tree) {
		t.Errorf("the refused commit gate changed the recorded range's end from %s to %s", recordedBefore, got)
	}
	if got := landTrunk(t, main); got != on {
		t.Errorf("trunk moved to %s", got)
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: a file that already holds a set on trunk lands when the story adds
// none. A story that adds a second set to it is refused for the lines it
// added and not those that were there; put right, the file still holds the
// first set and the story lands.
func TestAC2AFileThatAlreadyHoldsASetOnTrunkLandsAndASecondSetInItIsRefusedByItsOwnLines(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 24))
	writeFile(t, filepath.Join(main.dir, "notes.txt"), cmkText(held...))
	commitEverything(t, main.dir, "a file a person left a conflict in")
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")

	added := cmkText(cmkJoin(held, []string{"tail one", "tail two"}, cmkSet())...)
	out, refused := cmkWorked(s, func() { writeFile(t, filepath.Join(s.tree, "notes.txt"), added) })
	if !refused || !strings.Contains(out, cmkCode) {
		t.Fatalf("the commit gate did not refuse a second set (refused %v):\n%s", refused, out)
	}
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

	edited := append([]string(nil), held...)
	edited[2] = "an unrelated line, edited"
	writeFile(t, filepath.Join(s.tree, "notes.txt"), cmkText(edited...))
	if out, refused := cmkWorked(s, nil); refused {
		t.Fatalf("the commit gate refused a story that adds no set:\n%s", out)
	}
	cmkLands(s, main)
	if !cmkHolds(t, main, "notes.txt") {
		t.Error("trunk's notes.txt lost the set that was in it")
	}
}

// AC-2 and decision 4: a story that moves such a file lands, though git shows
// the moved file whole when it is not asked to look for renames; one that adds
// a set to the moved file is refused for its own lines.
func TestAC2AndDecision4AStoryThatMovesAFileHoldingASetLandsAndOneThatAddsToItIsRefused(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	held := cmkJoin(cmkFiller(1, 20), cmkSet(), cmkFiller(21, 40))
	writeFile(t, filepath.Join(main.dir, "notes.txt"), cmkText(held...))
	commitEverything(t, main.dir, "a file a person left a conflict in")
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")

	out, refused := cmkWorked(s, func() {
		if err := os.MkdirAll(filepath.Join(s.tree, "archive"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(s.tree, "notes.txt"), filepath.Join(s.tree, "archive", "notes.txt")); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(s.tree, "archive", "notes.txt"), cmkText(cmkJoin(held, cmkSet())...))
	})
	if !refused || !strings.Contains(out, cmkCode) || !strings.Contains(out, "archive/notes.txt") {
		t.Fatalf("the commit gate did not refuse a second set in the moved file (refused %v):\n%s", refused, out)
	}
	for _, n := range []int{46, 48, 50} {
		if !cmkNames(out, n) {
			t.Errorf("the refusal does not name line %d, a marker the story added:\n%s", n, out)
		}
	}
	for _, n := range []int{21, 23, 25} {
		if cmkNames(out, n) {
			t.Errorf("the refusal names line %d, a marker that moved with the file:\n%s", n, out)
		}
	}

	writeFile(t, filepath.Join(s.tree, "archive", "notes.txt"), cmkText(held...))
	if out, refused := cmkWorked(s, nil); refused {
		t.Fatalf("the commit gate refused a file that was only moved:\n%s", out)
	}
	cmkLands(s, main)
	if !cmkHolds(t, main, "archive/notes.txt") || ontoHolds(t, main, "notes.txt") {
		t.Error("trunk does not hold the file at its new name only")
	}
}

// ---------------------------------------------------------------- AC-3

// AC-3: where the configuration lists a path, a story may add the lines
// there and `sdlc doctor` lists the path. A story that adds them there and
// somewhere that is not listed is refused for the one that is not, and lands
// with the lines in the listed path once the other is out.
func TestAC3AStoryThatAddsTheLinesInAListedPathLandsAndDoctorListsThePath(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	setConfig(t, main.dir, "paths", cmkKey, []string{"docs/**", "notes/merging.md"})
	commitEverything(t, main.dir, "a person lists where conflict markers are meant")

	stdout, _, _ := main.as("").outcome(t, "", "doctor", "--json")
	var payload struct {
		Checks []struct {
			Name   string `json:"name"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("sdlc doctor --json is not JSON: %v\n%s", err, stdout)
	}
	listed := ""
	for _, c := range payload.Checks {
		if c.Name != "configuration" {
			continue
		}
		for l := range strings.SplitSeq(c.Detail, "\n") {
			if strings.Contains(l, cmkKey) {
				listed = l
			}
		}
	}
	for _, want := range []string{"docs/**", "notes/merging.md"} {
		if !strings.Contains(listed, want) {
			t.Errorf("doctor's configuration check does not list %s on a line about %s: %q", want, cmkKey, listed)
		}
	}

	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	body := cmkText(cmkJoin([]string{"# Merging", ""}, cmkSet())...)
	out, refused := cmkWorked(s, func() {
		writeFile(t, filepath.Join(s.tree, "docs", "merging.md"), body)
		writeFile(t, filepath.Join(s.tree, "elsewhere", "trap.md"), body)
	})
	if !refused || !strings.Contains(out, cmkCode) || !strings.Contains(out, "elsewhere/trap.md") {
		t.Fatalf("the commit gate did not refuse the lines outside the listed paths (refused %v):\n%s", refused, out)
	}
	if strings.Contains(out, "docs/merging.md") {
		t.Errorf("the refusal names docs/merging.md, which is listed:\n%s", out)
	}

	if err := os.Remove(filepath.Join(s.tree, "elsewhere", "trap.md")); err != nil {
		t.Fatal(err)
	}
	if out, refused := cmkWorked(s, nil); refused {
		t.Fatalf("the commit gate refused a set in a listed path:\n%s", out)
	}
	cmkLands(s, main)
	if !cmkHolds(t, main, "docs/merging.md") {
		t.Error("trunk does not hold docs/merging.md as the story wrote it")
	}
}

// AC-3: with nothing listed doctor says so.
func TestAC3SdlcDoctorSaysNoneWhereNoPathIsListed(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	root := project(t, binary)
	stdout, _, _ := checkout{binary: binary, dir: root}.outcome(t, "", "doctor", "--json")
	var payload struct {
		Checks []struct {
			Name   string `json:"name"`
			State  string `json:"state"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("sdlc doctor --json is not JSON: %v\n%s", err, stdout)
	}
	for _, c := range payload.Checks {
		if c.Name != "configuration" {
			continue
		}
		if c.State != "ok" {
			t.Errorf("the configuration sdlc init wrote is a problem: %s", c.Detail)
		}
		for l := range strings.SplitSeq(c.Detail, "\n") {
			if strings.Contains(l, cmkKey) {
				if !strings.Contains(l, "none") {
					t.Errorf("with nothing listed the line is %q, want it to say none", l)
				}
				return
			}
		}
		t.Fatalf("the configuration check has no line about %s:\n%s", cmkKey, c.Detail)
	}
	t.Fatal("doctor ran no configuration check")
}
