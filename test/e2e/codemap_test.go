// US-089 end to end: stories worked in worktrees each change the root
// CODEMAP.md at the same place and land one after the other with nobody in
// between. Each story's bookkeeper stores its sections with
// `sdlc codemap write`; each session runs the runbook's one `sdlc stop`, which
// lands the story and then makes the map change on trunk as that landing left
// it, in a commit of its own (AC-9, with AC-1 to AC-4 as a session meets
// them). Where a section is no longer what the bookkeeper saw, the story still
// lands, the stop says what a person runs, the hook refuses that command to
// every session, and a person's `sdlc remap` in each of its three forms ends
// it (AC-6). The pages a person reads describe the new order (AC-9).
//
// Helpers carry a map prefix.

package e2e

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	mapFile  = "CODEMAP.md"
	mapStart = "# CODEMAP\n\nWhat each file is for.\n\n## invoice.go\n\nKeeps invoices.\n"
)

// mapRepository is ontoRepository with a root CODEMAP.md of one section on
// trunk.
func mapRepository(t *testing.T, binary string, stories ...spec) checkout {
	t.Helper()
	main := ontoRepository(t, binary, stories...)
	writeFile(t, filepath.Join(main.dir, mapFile), mapStart)
	commitEverything(t, main.dir, "a map of one section")
	return main
}

// mapStored is the story's bookkeeper storing its sections, as the runbook
// directs: asked of the hook first, then run in the story's worktree with the
// sections on standard input.
func mapStored(s *ontoStory, sections string) {
	s.t.Helper()
	s.do("bookkeeper", "Bash", running(s.at.in(s.tree, "sdlc codemap write")))
	s.here().runWith(s.t, sections, "codemap", "write")
}

// mapAdds is a set that adds one section of the story's own, and the section
// as it then reads in the map.
func mapAdds(s *ontoStory) (set, section string) {
	section = "## " + s.source + "\n\nKeeps what " + s.story + " added.\n"
	return "<!-- sdlc:add -->\n" + section, section
}

// mapReplaces is a set that replaces the one section trunk's map began with
// and adds one of the story's own, and the two as they then read in the map.
func mapReplaces(s *ontoStory) (set, replaced, added string) {
	replaced = "## invoice.go\n\nKeeps invoices and, since " + s.story + ", what it added.\n"
	_, added = mapAdds(s)
	return "<!-- sdlc:replace -->\n" + replaced + "\n<!-- sdlc:add -->\n" + added, replaced, added
}

func mapOnTrunk(t *testing.T, main checkout) string {
	t.Helper()
	return gitShow(t, main.dir, "refs/heads/main:"+mapFile)
}

// gitShow is a file as a commit holds it, byte for byte.
func gitShow(t *testing.T, dir, spec string) string {
	t.Helper()
	return gitRaw(t, dir, "cat-file", "blob", spec)
}

type mapRecord struct {
	Commit        string `json:"commit"`
	CodemapCommit string `json:"codemap_commit"`
	Events        []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"events"`
	Approvals []any `json:"approvals"`
}

func mapRecordOf(t *testing.T, main checkout, id string) mapRecord {
	t.Helper()
	var r mapRecord
	readJSON(t, main.record(id), &r)
	return r
}

// mapCommitted holds trunk's tip to being the story's map commit: named on its
// record, one parent, which is the story's last landed commit, and CODEMAP.md
// the only path it changes.
func mapCommitted(t *testing.T, main checkout, id string) string {
	t.Helper()
	tip := landTrunk(t, main)
	r := mapRecordOf(t, main, id)
	if r.CodemapCommit != tip {
		t.Fatalf("%s's record names %q as its map commit, want trunk's tip %s", id, r.CodemapCommit, tip)
	}
	if got := gitOut(t, main.dir, "rev-list", "--parents", "-n", "1", tip); got != tip+" "+r.Commit {
		t.Errorf("%s's map commit and its parents are %q, want the one parent %s, the story's last landed commit", id, got, r.Commit)
	}
	if got := gitOut(t, main.dir, "diff-tree", "-r", "--no-renames", "--name-only", "--no-commit-id", tip+"^", tip); got != mapFile {
		t.Errorf("%s's map commit changes %q, want CODEMAP.md and no other path", id, got)
	}
	if got := readFile(t, filepath.Join(main.dir, mapFile)); got != mapOnTrunk(t, main) {
		t.Errorf("the main checkout's CODEMAP.md is not trunk's after %s's map commit:\n%q", id, got)
	}
	return tip
}

var mapCodes = regexp.MustCompile(`SDLC-E(\d{4})`)

// mapOwnCode is the code the story adds, as a command or a page has it: every
// code up to SDLC-E0075 was there before, for other conditions.
func mapOwnCode(said string) string {
	for _, m := range mapCodes.FindAllStringSubmatch(said, -1) {
		if n, _ := strconv.Atoi(m[1]); n > 75 {
			return m[0]
		}
	}
	return ""
}

// mapNobodyBetween holds a story's record to having nothing of a person's on
// it: no approval, no release, and no unfreeze but the loop's own.
func mapNobodyBetween(t *testing.T, main checkout, id string) {
	t.Helper()
	r := mapRecordOf(t, main, id)
	for _, e := range r.Events {
		if e.Type == "unfreeze" && e.Message == "the story is finished, so its freeze is lifted" {
			continue
		}
		if e.Type == "unfreeze" || e.Type == "release" {
			t.Errorf("%s's record has a %s on it: a person's command came between the stops", id, e.Type)
		}
	}
	if len(r.Approvals) != 0 {
		t.Errorf("%s's record has %d approvals on it: a person's command came between the stops", id, len(r.Approvals))
	}
}

// ---------------------------------------------------------------- AC-9

// AC-9, with AC-1 to AC-4: three stories from three worktrees, each begun
// before the first landed, each with a section to add to CODEMAP.md at the
// same place, its end. Each bookkeeper stores its section; each session stops
// once. Between the three stops there is no command from a person: every
// command here is one a session runs, asked of the hook first. Each stop
// lands its story and then its map change, in a commit of its own on the map
// the stop before it left, and trunk's map ends with each section once and
// whole.
func TestAC9ThreeStoriesEachWithAMapChangeAtTheSamePlaceLandWithNobodyBetween(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := mapRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2}, spec{id: "US-003", priority: 3})
	cut := landTrunk(t, main)

	stories := []*ontoStory{
		ontoBegun(t, binary, main, sessionFirst, "US-001"),
		ontoBegun(t, binary, main, sessionSecond, "US-002"),
		ontoBegun(t, binary, main, sessionOther, "US-003"),
	}
	for _, s := range stories {
		if got := gitOut(t, s.tree, "rev-parse", "HEAD"); got != cut {
			t.Fatalf("%s was begun at %s, want every story begun before the first landed, at %s", s.story, got, cut)
		}
	}
	for _, s := range stories {
		s.worked()
		set, _ := mapAdds(s)
		mapStored(s, set)
		// Nothing is left uncommitted in the worktree for the landing to refuse.
		if status := gitOut(t, s.tree, "status", "--porcelain", "--", mapFile); status != "" {
			t.Fatalf("storing %s's sections left CODEMAP.md changed in its worktree: %s", s.story, status)
		}
	}
	if got := landTrunk(t, main); got != cut {
		t.Fatalf("precondition: trunk moved to %s before any story was stopped", got)
	}

	want, before := mapStart, cut
	for _, s := range stories {
		s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
		out, code := s.here().try(t, "stop")
		if code != 0 {
			t.Fatalf("the stop of %s exited %d:\n%s", s.story, code, out)
		}
		// AC-1: no refusal, and none that names CODEMAP.md.
		if strings.Contains(out, "SDLC-E") {
			t.Fatalf("the stop of %s printed a code, and its story and its map change should both be on trunk:\n%s", s.story, out)
		}
		tip := mapCommitted(t, main, s.story)
		if gitOut(t, main.dir, "merge-base", before, tip) != before {
			t.Fatalf("after %s landed trunk at %s does not descend from %s, where the story before left it", s.story, tip, before)
		}
		// AC-3: the parent holds the map the landing before this one left, and
		// the commit is that map with this story's section after it.
		if got := gitShow(t, main.dir, tip+"^:"+mapFile); got != want {
			t.Errorf("the parent of %s's map commit holds\n%q\nwant the map the landing before it left\n%q", s.story, got, want)
		}
		_, section := mapAdds(s)
		want += "\n" + section
		if got := mapOnTrunk(t, main); got != want {
			t.Fatalf("after %s landed trunk's CODEMAP.md is\n%q\nwant\n%q", s.story, got, want)
		}
		// AC-4: after the story's own commits, which hold its two files.
		for _, f := range []string{s.test, s.source} {
			if !ontoHolds(t, main, f) {
				t.Errorf("trunk does not hold %s after %s landed", f, s.story)
			}
		}
		if exists(s.tree) {
			t.Errorf("the landed %s's worktree is still there", s.story)
		}
		if exists(filepath.Join(main.dir, ".git", "sdlc", "codemap", s.story+".md")) {
			t.Errorf("%s's sections are still kept after they reached trunk", s.story)
		}
		before = tip
	}

	// AC-2: each change once and whole, no marker, nothing dropped.
	final := mapOnTrunk(t, main)
	for _, s := range stories {
		_, section := mapAdds(s)
		if n := strings.Count(final, section); n != 1 {
			t.Errorf("trunk's CODEMAP.md holds %s's section %d times, want once and whole:\n%s", s.story, n, final)
		}
	}
	if n := strings.Count(final, "## invoice.go\n\nKeeps invoices.\n"); n != 1 {
		t.Errorf("trunk's CODEMAP.md holds the section it began with %d times:\n%s", n, final)
	}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(final, "\n") {
		if line != "" && seen[line] {
			t.Errorf("trunk's CODEMAP.md holds the line %q twice", line)
		}
		seen[line] = true
		for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"} {
			if strings.HasPrefix(line, marker) {
				t.Errorf("trunk's CODEMAP.md holds a conflict marker: %q", line)
			}
		}
	}
	if merges := gitOut(t, main.dir, "rev-list", "--merges", cut+"..main"); merges != "" {
		t.Errorf("trunk has merge commits: %s", merges)
	}
	for id, status := range statuses(t, main.backlog()) {
		if status != "done" {
			t.Errorf("%s is %s after all three stops, want done", id, status)
		}
	}
	for _, s := range stories {
		mapNobodyBetween(t, main, s.story)
		if _, ok := branchesOf(t, main.dir)["story/"+s.story]; ok {
			t.Errorf("story/%s is still there", s.story)
		}
	}
	// Nothing is left for a person: no command says a map change is waiting.
	if out := main.as("").run(t, "status"); strings.Contains(out, "sdlc remap") {
		t.Errorf("status speaks of a repair after three clean landings:\n%s", out)
	}
}

// ---------------------------------------------------------------- AC-6

// AC-6 end to end, with the hook between: three stories each replace the one
// section trunk's map began with. The first lands with its map change. The
// second and the third land too, and each one's stop says, under a code of its
// own, that CODEMAP.md was not brought up to date and that `sdlc remap <ID>`
// is what to run. That command is refused by the hook to every session, with
// a story held and with none, in each form; and a person's own run of each
// form ends it: the plain one refuses and names the section, --replace puts
// the story's section over trunk's, and --drop discards the change with no
// commit.
func TestAC6AStoryWhoseMapChangeNoLongerAppliesStillLandsAndOnlyAPersonRepairsIt(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := mapRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2}, spec{id: "US-003", priority: 3})
	first := ontoBegun(t, binary, main, sessionFirst, "US-001")
	second := ontoBegun(t, binary, main, sessionSecond, "US-002")
	third := ontoBegun(t, binary, main, sessionOther, "US-003")
	stories := []*ontoStory{first, second, third}
	for _, s := range stories {
		s.worked()
		set, _, _ := mapReplaces(s)
		mapStored(s, set)
	}

	forms := []string{"sdlc remap US-002", "sdlc remap US-002 --replace", "sdlc remap US-002 --drop", "sdlc remap US-003 --drop"}
	refusedToEverySession := func(t *testing.T, when string) {
		t.Helper()
		for _, s := range stories {
			for _, form := range forms {
				for _, agent := range []string{"", "bookkeeper"} {
					r := askAt(t, binary, s.id, s.at, agent, "Bash", running(form))
					if !denied(r) {
						t.Errorf("%s: the hook let %s's session run %q", when, s.story, form)
						continue
					}
					if reason := r.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "remap-is-a-human-decision") {
						t.Errorf("%s: %q was refused by something other than the rule: %s", when, form, reason)
					}
				}
			}
		}
	}
	refusedToEverySession(t, "with three stories held")

	first.run("", "stop")
	afterFirst := mapCommitted(t, main, "US-001")
	_, firstReplaced, firstAdded := mapReplaces(first)
	held := "# CODEMAP\n\nWhat each file is for.\n\n" + firstReplaced + "\n" + firstAdded
	if got := mapOnTrunk(t, main); got != held {
		t.Fatalf("after US-001 landed trunk's CODEMAP.md is\n%q\nwant\n%q", got, held)
	}

	landed := afterFirst
	for _, s := range []*ontoStory{second, third} {
		s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
		out, code := s.here().try(t, "stop")
		if code != 0 {
			t.Fatalf("the stop of %s exited %d: its code landed, so the landing stands and the exit is 0:\n%s", s.story, code, out)
		}
		tip := landTrunk(t, main)
		r := mapRecordOf(t, main, s.story)
		if tip == landed || r.Commit != tip || r.CodemapCommit != "" {
			t.Fatalf("after %s's stop trunk is at %s, its record names %q as landed and %q as its map commit: want the story landed and no map commit",
				s.story, tip, r.Commit, r.CodemapCommit)
		}
		if mapOwnCode(out) == "" || strings.Contains(out, "SDLC-E0075") || strings.Contains(strings.ToLower(out), "nothing landed") {
			t.Errorf("the stop of %s does not say, under a code of its own, that the story landed and its map change did not:\n%s", s.story, out)
		}
		for _, want := range []string{mapFile, "sdlc remap " + s.story, "invoice.go"} {
			if !strings.Contains(out, want) {
				t.Errorf("the stop of %s does not say %q:\n%s", s.story, want, out)
			}
		}
		if got := mapOnTrunk(t, main); got != held {
			t.Fatalf("after %s's stop trunk's CODEMAP.md is\n%q\nwant it as US-001 left it\n%q", s.story, got, held)
		}
		if !ontoHolds(t, main, s.source) || exists(s.tree) {
			t.Errorf("%s did not land whole: its source is not on trunk, or its worktree is still there", s.story)
		}
		set, _, _ := mapReplaces(s)
		if got := readFile(t, filepath.Join(main.dir, ".git", "sdlc", "codemap", s.story+".md")); got != set {
			t.Errorf("%s's sections are kept as\n%q\nwant what its bookkeeper stored\n%q", s.story, got, set)
		}
		mapNobodyBetween(t, main, s.story)
		landed = tip
	}
	for id, status := range statuses(t, main.backlog()) {
		if status != "done" {
			t.Errorf("%s is %s after all three stops, want done", id, status)
		}
	}

	// Every command says so until a person has dealt with it, to a session as
	// to a person, and the session cannot run what it names.
	for _, c := range []checkout{main.as(""), main.as(sessionSecond)} {
		for range 2 {
			stdout, stderr, _ := c.outcome(t, "", "status")
			for _, want := range []string{"sdlc remap US-002", "sdlc remap US-003", mapFile} {
				if !strings.Contains(stdout+stderr, want) {
					t.Errorf("status does not say %q of the map changes that did not reach trunk:\n%s%s", want, stdout, stderr)
				}
			}
		}
	}
	refusedToEverySession(t, "with no story held and two map changes waiting")

	person := main.as("")
	t.Run("AC-6 the plain form refuses and names the section", func(t *testing.T) {
		out, code := person.try(t, "remap", "US-002")
		if code == 0 {
			t.Fatalf("sdlc remap US-002 went ahead over a section that is not what the bookkeeper saw:\n%s", out)
		}
		for _, want := range []string{"invoice.go", "--replace", "--drop", "SDLC-E"} {
			if !strings.Contains(out, want) {
				t.Errorf("the refusal does not say %q:\n%s", want, out)
			}
		}
		if landTrunk(t, main) != landed || mapOnTrunk(t, main) != held {
			t.Error("the refused repair changed trunk")
		}
	})
	t.Run("AC-6 --replace puts the story's section over trunk's", func(t *testing.T) {
		person.run(t, "remap", "US-002", "--replace")
		tip := landTrunk(t, main)
		r := mapRecordOf(t, main, "US-002")
		if r.CodemapCommit != tip || tip == landed {
			t.Fatalf("after --replace trunk is at %s and US-002's record names %q as its map commit", tip, r.CodemapCommit)
		}
		if got := gitOut(t, main.dir, "rev-list", "--parents", "-n", "1", tip); got != tip+" "+landed {
			t.Errorf("the repair's commit and its parents are %q, want the one parent %s", got, landed)
		}
		if got := gitOut(t, main.dir, "diff-tree", "-r", "--no-renames", "--name-only", "--no-commit-id", landed, tip); got != mapFile {
			t.Errorf("the repair's commit changes %q, want CODEMAP.md and no other path", got)
		}
		_, replaced, added := mapReplaces(second)
		held = "# CODEMAP\n\nWhat each file is for.\n\n" + replaced + "\n" + firstAdded + "\n" + added
		if got := mapOnTrunk(t, main); got != held {
			t.Errorf("after --replace trunk's CODEMAP.md is\n%q\nwant US-002's section over trunk's and its added one at the end\n%q", got, held)
		}
		landed = tip
	})
	t.Run("AC-6 --drop discards the change with no commit", func(t *testing.T) {
		person.run(t, "remap", "US-003", "--drop")
		if got := landTrunk(t, main); got != landed {
			t.Errorf("--drop moved trunk to %s", got)
		}
		if got := mapOnTrunk(t, main); got != held {
			t.Errorf("--drop changed trunk's CODEMAP.md to\n%q", got)
		}
		if r := mapRecordOf(t, main, "US-003"); r.CodemapCommit != "" {
			t.Errorf("US-003's record names %q as its map commit after a drop", r.CodemapCommit)
		}
		for _, id := range []string{"US-002", "US-003"} {
			if exists(filepath.Join(main.dir, ".git", "sdlc", "codemap", id+".md")) {
				t.Errorf("%s's sections are still kept after a person dealt with them", id)
			}
		}
		stdout, stderr, _ := person.outcome(t, "", "status")
		if strings.Contains(stdout+stderr, "sdlc remap") {
			t.Errorf("status still speaks of a repair after a person dealt with both:\n%s%s", stdout, stderr)
		}
	})
}

// ---------------------------------------------------------------- AC-9, the pages

// AC-9: docs/the-loop.md, docs/troubleshooting.md, the bookkeeper's
// instructions and the /sdlc:next runbook describe the new order. Each is held
// to no longer saying what the story makes untrue, and to naming what it adds:
// the bookkeeper stores sections, the stop makes the map change on trunk after
// the story has landed, and what is left over is a person's `sdlc remap`.
func TestAC9TheLoopPageTroubleshootingTheBookkeeperAndTheRunbookDescribeTheNewOrder(t *testing.T) {
	read := func(t *testing.T, parts ...string) string {
		t.Helper()
		text := readFile(t, filepath.Join(append([]string{"..", ".."}, parts...)...))
		if text == "" {
			t.Fatalf("%s is missing or empty", filepath.Join(parts...))
		}
		return text
	}
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	hold := func(t *testing.T, what, text string, says, gone []string) {
		t.Helper()
		if text == "" {
			t.Fatalf("%s has no such section", what)
		}
		for _, want := range says {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not mention %q", what, want)
			}
		}
		for _, old := range gone {
			if strings.Contains(text, old) {
				t.Errorf("%s still says %q, which is no longer so", what, old)
			}
		}
	}

	t.Run("AC-9 the runbook's Gate 9", func(t *testing.T) {
		gate9 := flat(section(read(t, "plugin", "skills", "next", "SKILL.md"), "## Gate 9"))
		// The session commits nothing for the map: the bookkeeper stored it
		// and the stop makes the commit. What the stop could not do is a
		// person's, and the session says so and does not run it.
		hold(t, "the runbook's Gate 9", gate9,
			[]string{"sdlc stop", "sdlc remap", mapFile},
			[]string{"commit only the bookkeeper's"})
		if mapOwnCode(gate9) == "" {
			t.Error("the runbook's Gate 9 does not name the code a stop prints when the map change did not reach trunk")
		}
	})
	t.Run("AC-9 the bookkeeper's instructions", func(t *testing.T) {
		page := flat(read(t, "plugin", "agents", "bookkeeper.md"))
		hold(t, "the bookkeeper's instructions", page,
			[]string{"sdlc codemap write", "<!-- sdlc:add -->", "<!-- sdlc:replace -->", mapFile, "section"},
			nil)
	})
	t.Run("AC-9 the-loop.md, parallel stories and the map", func(t *testing.T) {
		page := read(t, "docs", "the-loop.md")
		parallel := flat(section(page, "### Parallel stories and `CODEMAP.md`"))
		hold(t, "docs/the-loop.md, Parallel stories and CODEMAP.md", parallel,
			[]string{"sdlc codemap write", "sdlc stop", "sdlc remap", "--replace", "--drop", "commit of its own"},
			[]string{"it still ends with a person", "usually do not apply onto trunk at its bookkeeper's commit"})
		if mapOwnCode(parallel) == "" {
			t.Error("docs/the-loop.md, Parallel stories and CODEMAP.md, does not name the code a stop prints when the map change did not reach trunk")
		}
	})
	t.Run("AC-9 troubleshooting.md", func(t *testing.T) {
		page := read(t, "docs", "troubleshooting.md")
		hold(t, "docs/troubleshooting.md, SDLC-E0075", flat(section(page, "### SDLC-E0075")),
			[]string{"conflict"},
			[]string{"meet this most often"})
		found := false
		for _, m := range mapCodes.FindAllString(page, -1) {
			if mapOwnCode(m) == "" {
				continue
			}
			own := flat(section(page, "### "+m))
			if !strings.Contains(own, "sdlc remap") {
				continue
			}
			found = true
			hold(t, "docs/troubleshooting.md, "+m, own,
				[]string{mapFile, "sdlc remap", "--replace", "--drop", "landed"},
				nil)
		}
		if !found {
			t.Error("docs/troubleshooting.md has no section, for a code above SDLC-E0075, that names `sdlc remap`")
		}
	})
}
