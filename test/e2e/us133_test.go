// US-133 end to end: the commit gate pauses for a person because of what the
// change touches, driven through the built binary and the plugin launcher in
// a real git repository, with the story worked in its worktree as the runbook
// directs (AC-7).
//
// Three sandboxes, as AC-7 names them: a low story whose change touches a path
// trunk's pre_commit_pause_paths lists pauses, at the hook before `git commit`
// and at `sdlc gate commit pass`, naming the file and the glob, and is
// committed once a person approves it; a high story in a project that pauses
// no tier, touching no listed path, is committed with no pause, lands, and
// leaves landed_without_pause in `sdlc log`; and a story whose change edits
// .sdlc/config.json is refused at the landing with SDLC-E0055, as it is today.
// A fourth holds AC-3 through the binary: the story's own copy of the
// configuration, emptied in its commits, does not take the pause away.
//
// Helpers carry a cpe prefix, for commit pause end to end.

package e2e

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

const (
	cpeHookGlob = "internal/hook/**"
	cpeHookFile = "internal/hook/guard.go"
	cpeLanded   = "landed_without_pause"
)

// cpePauseLine is a line of prose about the commit's pause, with the blank line
// before it where it is a block of its own.
var cpePauseLine = regexp.MustCompile(`(?mi)(^\n)?^[^\n]*pause[^\n]*\n`)

// withoutCommitPause is printed text with US-133's lines taken out: AC-5 has
// `sdlc status` say whether the commit will pause and AC-4 has `sdlc log` show
// a landing without one, so the tests that hold those commands to the text
// they printed before US-133 compare what is left. This file holds the lines.
func withoutCommitPause(text string) string { return cpePauseLine.ReplaceAllString(text, "") }

// cpeRepository is ontoRepository with US-001 in the risk tier given and
// human_gates as given, committed on trunk.
func cpeRepository(t *testing.T, binary, tier string, tiers, globs []string) checkout {
	t.Helper()
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1})
	var backlog map[string]any
	readJSON(t, main.backlog(), &backlog)
	stories, _ := backlog["stories"].([]any)
	for _, s := range stories {
		if story, ok := s.(map[string]any); ok && story["id"] == "US-001" {
			story["risk_tier"] = tier
		}
	}
	writeJSON(t, main.backlog(), backlog)
	setConfig(t, main.dir, "human_gates", "pre_commit_pause_tiers", tiers)
	setConfig(t, main.dir, "human_gates", "pre_commit_pause_paths", globs)
	commitEverything(t, main.dir, "US-001 is "+tier+", and human_gates says what pauses")
	return main
}

// cpeToTheCommit takes the story through every gate before the commit as the
// runbook directs, with change done by the implementer beside its source file.
func cpeToTheCommit(s *ontoStory, change func(s *ontoStory)) {
	s.t.Helper()
	for gate := s.nextGate(); gate != "" && gate != string(model.GateCommit); gate = s.nextGate() {
		switch model.Gate(gate) {
		case model.GateDoR, model.GateDesignReview, model.GateVerifierReview, model.GateCodeReview:
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
				change(s)
			}
		case model.GateVerification:
			s.stores("verifier", "verification", "# Verification\n\nEvery command green.\n")
		default:
			s.t.Fatalf("the story is at %s, which comes after the commit", gate)
		}
		for _, r := range fixtureReviewers(model.Gate(gate)) {
			if r.WhenDoRAdvocate {
				continue
			}
			s.do(r.Role, "Bash", running(s.at.in(s.tree, "sdlc review add "+gate+" "+r.Role+" approve")))
			s.here().runWith(s.t, "# "+r.Role+"\n", "review", "add", gate, r.Role, "approve")
		}
		s.run("", "gate", gate, "pass", "--note", "worked by the session")
	}
}

// cpeCommitted is the story's work committed on its branch with git alone,
// the hook not asked.
func cpeCommitted(s *ontoStory) {
	s.t.Helper()
	git(s.t, s.tree, "add", "-A")
	git(s.t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", s.story)
}

// cpeGate is the session asking to pass the commit gate: the hook allows the
// command, and the binary answers.
func cpeGate(s *ontoStory) (string, int) {
	s.t.Helper()
	s.do("", "Bash", running(s.at.in(s.tree, "sdlc gate commit pass --note committed")))
	return s.here().try(s.t, "gate", "commit", "pass", "--note", "committed")
}

// cpeEvents is the type of every event on US-001's record.
func cpeEvents(t *testing.T, main checkout) []string {
	t.Helper()
	var record struct {
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	readJSON(t, main.record("US-001"), &record)
	var out []string
	for _, e := range record.Events {
		out = append(out, e.Type)
	}
	return out
}

func cpeHas(events []string, kind string) bool { return slices.Contains(events, kind) }

// AC-7: a low story that changes a file matching pre_commit_pause_paths pauses,
// naming the file and the glob, at the hook and at the commit gate, and is
// committed once a person has approved it, with no landed_without_pause.
func TestAC7ALowStoryThatChangesAListedFilePausesNamingTheFileAndTheGlob(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := cpeRepository(t, binary, "low", []string{"high"}, []string{cpeHookGlob})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	cpeToTheCommit(s, func(s *ontoStory) {
		s.writes("implementer", cpeHookFile, "package hook\n\n// Guard is US-001's.\n")
	})

	git(t, s.tree, "add", "-A")
	r := askAt(t, binary, s.id, s.at, "", "Bash", running(s.at.in(s.tree, "git commit -m US-001")))
	reason := r.HookSpecificOutput.PermissionDecisionReason
	if !denied(r) {
		t.Errorf("AC-7 the hook let `git commit` through on a low story that changed %s", cpeHookFile)
	} else if !strings.Contains(reason, cpeHookFile) || !strings.Contains(reason, cpeHookGlob) {
		t.Errorf("AC-7 the hook's refusal does not name %s and %s: %s", cpeHookFile, cpeHookGlob, reason)
	}

	cpeCommitted(s)
	out, code := cpeGate(s)
	refusedWith(t, out, code != 0, "SDLC-E0037")
	if flat := strings.Join(strings.Fields(out), " "); !strings.Contains(flat, cpeHookFile) || !strings.Contains(flat, cpeHookGlob) {
		t.Errorf("AC-7 the commit gate's refusal does not name %s and %s:\n%s", cpeHookFile, cpeHookGlob, out)
	}

	s.run("", "escalate", "pre_commit_approval", "--message", "ready to commit")
	if out := s.here().as("").run(t, "approve", s.story); !strings.Contains(out, s.story) {
		t.Fatalf("approve printed %q, want it to name %s", out, s.story)
	}
	s.do("", "Bash", running(s.at.in(s.tree, "sdlc start --json")))
	s.here().run(t, "start", "--json")
	if out, code := cpeGate(s); code != 0 {
		t.Fatalf("AC-7 the commit gate refused the work a person approved:\n%s", out)
	}
	if cpeHas(cpeEvents(t, main), cpeLanded) {
		t.Error("AC-7 a story a person approved is recorded as " + cpeLanded)
	}
}

// AC-7: a high story, in a project with pre_commit_pause_tiers [] and no file
// matching, commits with no pause, lands, and leaves landed_without_pause in
// `sdlc log`, naming the tier and pre_commit_pause_paths.
func TestAC7AHighStoryWithNoTierPausedAndNoFileMatchingLandsAndIsInTheLog(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := cpeRepository(t, binary, "high", []string{}, []string{cpeHookGlob})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	s.worked()
	tip := landBookkeeping(t, s.treeSession, true)

	s.run("", "stop")
	if got := landTrunk(t, main); got != tip {
		t.Fatalf("AC-7 trunk is at %s after the stop, want the story's tip %s", got, tip)
	}
	if !cpeHas(cpeEvents(t, main), cpeLanded) {
		t.Errorf("AC-7 the record has no %s: %v", cpeLanded, cpeEvents(t, main))
	}
	prose := main.run(t, "log")
	if !strings.Contains(prose, "US-001") || !strings.Contains(prose, "pre_commit_pause_paths") ||
		!regexp.MustCompile(`\bhigh\b`).MatchString(prose) {
		t.Errorf("AC-7 sdlc log does not show that US-001, tier high, landed with no file matching pre_commit_pause_paths:\n%s", prose)
	}
	e := landLogged(t, main)
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), cpeLanded) {
		t.Errorf("AC-7 sdlc log --json does not carry %s for US-001: %s", cpeLanded, raw)
	}
}

// AC-7: a story whose change edits .sdlc/config.json is refused at the landing
// with SDLC-E0055, as it is today, and trunk stays where it was.
func TestAC7AStoryWhoseChangeEditsTheConfigurationIsRefusedAtTheLanding(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := cpeRepository(t, binary, "low", []string{"high"}, []string{cpeHookGlob})
	cut := landTrunk(t, main)
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	cpeToTheCommit(s, func(s *ontoStory) {
		// Written by the test as a git operation brings it in: no agent may
		// write the file, and the hook refuses one that tries.
		setConfig(t, s.tree, "human_gates", "pre_commit_pause_paths", []string{})
	})
	cpeCommitted(s)
	if out, code := cpeGate(s); code != 0 {
		t.Fatalf("the commit gate refused a low story that touched no listed file:\n%s", out)
	}
	for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
		if gate != string(model.GateRetro) {
			t.Fatalf("the story is at %s after the commit", gate)
		}
		s.stores("bookkeeper", "retro", "# Retro\n\nNo deviations.\n")
		s.run("", "gate", gate, "pass", "--note", "worked by the session")
	}
	landBookkeeping(t, s.treeSession, true)

	s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
	out, code := s.here().try(t, "stop")
	refusedWith(t, out, code != 0, "SDLC-E0055")
	if !strings.Contains(out, ".sdlc/config.json") {
		t.Errorf("AC-7 the landing's refusal does not name .sdlc/config.json:\n%s", out)
	}
	if got := landTrunk(t, main); got != cut {
		t.Errorf("AC-7 trunk moved to %s on a story that changes the configuration", got)
	}
}

// AC-3 through the binary: the story's own copy of the configuration, emptied
// in its commits, does not take away the pause trunk's configuration calls for.
func TestAC3TheStorysOwnCopyOfTheConfigurationDoesNotTakeThePauseAwayThroughTheBinary(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := cpeRepository(t, binary, "low", []string{"high"}, []string{cpeHookGlob})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	cpeToTheCommit(s, func(s *ontoStory) {
		s.writes("implementer", cpeHookFile, "package hook\n\n// Guard is US-001's.\n")
		setConfig(t, s.tree, "human_gates", "pre_commit_pause_tiers", []string{})
		setConfig(t, s.tree, "human_gates", "pre_commit_pause_paths", []string{})
	})
	cpeCommitted(s)

	out, code := cpeGate(s)
	refusedWith(t, out, code != 0, "SDLC-E0037")
	if !strings.Contains(strings.Join(strings.Fields(out), " "), cpeHookFile) {
		t.Errorf("AC-3 the refusal does not name %s:\n%s", cpeHookFile, out)
	}
	if cpeHas(cpeEvents(t, main), cpeLanded) {
		t.Error("AC-3 the person was told no sign-off was needed: " + cpeLanded + " is on the record")
	}
}
