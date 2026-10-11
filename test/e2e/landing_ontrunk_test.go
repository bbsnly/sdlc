// US-088 end to end: stories worked in worktrees land on trunk one after the
// other with nobody in between. Three sessions each begin a story before any
// of them has landed, work it through every gate as the runbook directs, and
// stop: each stop puts its story onto the trunk the ones before it moved,
// takes the test files they landed for trunk's, runs the commands and lands
// (AC-7, with AC-1 and AC-8 as a session meets them). Two stops at the same
// moment both land (AC-13). The hook goes on refusing an agent's write to a
// test file that came from trunk, and `sdlc unfreeze` from a session (AC-6).
// The pages a person reads describe the landing as it then is (AC-7).
//
// The project ignores the loop's own state, has commands that pass on it, and
// has a committer in its git configuration, as a project a person commits in
// has: the binary's own replay of a story's commits needs one. Helpers carry
// an onto prefix.

package e2e

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// ontoRepository is storyRepository with the loop's state ignored, commands
// that pass on this project, and a committer.
func ontoRepository(t *testing.T, binary string, stories ...spec) checkout {
	t.Helper()
	main := storyRepository(t, binary, stories...)
	git(t, main.dir, "rm", "-r", "--cached", "--quiet", "--ignore-unmatch", "--", ".sdlc/state")
	writeFile(t, filepath.Join(main.dir, ".gitignore"), "/.sdlc/state/\n")
	// sdlc init's defaults for this project's Go files fail on it -- it has no
	// main function, and no coverage -- and the landing runs them.
	for key, command := range map[string]string{
		"build": "exit 0", "test": "exit 0", "lint": "exit 0", "fmt_check": "exit 0",
		"coverage": "echo 100", "mutation": "echo 100",
	} {
		setConfig(t, main.dir, "commands", key, command)
	}
	git(t, main.dir, "config", "user.email", "t@example.com")
	git(t, main.dir, "config", "user.name", "Test")
	commitEverything(t, main.dir, "ignore the loop's state, and commands that pass")
	return main
}

// ontoStory is one story worked by one session in the worktree start made for
// it: a test file and a source file of its own, which no other story touches.
type ontoStory struct {
	*treeSession
	story, test, source string
}

// ontoBegun is a session opened in the main checkout beginning the next story,
// which has to be id.
func ontoBegun(t *testing.T, binary string, main checkout, session, id string) *ontoStory {
	t.Helper()
	s := &treeSession{t: t, binary: binary, id: session, at: opening{"opened in the main checkout", main.dir, main.dir}}
	s.do("", "Bash", running("sdlc start --json"))
	begun := startIn(t, main, main.as(session))
	if begun.story != id {
		t.Fatalf("the start in %s gave %s, want %s", session, begun.story, id)
	}
	s.tree = begun.dir
	slug := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	return &ontoStory{treeSession: s, story: id, test: slug + "_test.go", source: slug + ".go"}
}

// worked takes the story through every gate, as treeSession.workGate does, with
// the story's own two files.
func (s *ontoStory) worked() {
	s.t.Helper()
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
		case model.GateVerification:
			s.stores("verifier", "verification", "# Verification\n\nEvery command green.\n")
		case model.GateCommit:
			git(s.t, s.tree, "add", "-A")
			s.do("", "Bash", running(s.at.in(s.tree, "git commit -m "+s.story)))
			git(s.t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", s.story)
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
		s.run("", "gate", gate, "pass", "--note", "worked by the session")
	}
}

// ontoHolds reports whether trunk commits a file at rel.
func ontoHolds(t *testing.T, main checkout, rel string) bool {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "cat-file", "-e", "refs/heads/main:"+rel)
	cmd.Dir = main.dir
	return cmd.Run() == nil
}

// ---------------------------------------------------------------- AC-7

// AC-7: three stories from three worktrees, each begun before the first
// landed, land one after the other. The first adds a test file and a source
// file, and so do the others. Between the three stops there is no command from
// a person: every command here is one a session runs, asked of the hook first,
// and each stop has to work as it is, on a trunk the stop before it moved.
func TestAC7ThreeStoriesFromThreeWorktreesLandOneAfterTheOtherWithNobodyBetween(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2}, spec{id: "US-003", priority: 3})
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
	}
	// The first story's bookkeeper maps what it added, as the runbook directs.
	landBookkeeping(t, stories[0].treeSession, true)
	if got := landTrunk(t, main); got != cut {
		t.Fatalf("precondition: trunk moved to %s before any story was stopped", got)
	}

	before := cut
	for _, s := range stories {
		s.run("", "stop")
		tip := landTrunk(t, main)
		if tip == before {
			t.Fatalf("the stop of %s exited 0 and trunk is still at %s", s.story, before)
		}
		if gitOut(t, main.dir, "merge-base", before, tip) != before {
			t.Fatalf("after %s landed trunk at %s does not descend from %s, where the story before left it", s.story, tip, before)
		}
		for _, f := range []string{s.test, s.source} {
			if !ontoHolds(t, main, f) {
				t.Errorf("trunk does not hold %s after %s landed", f, s.story)
			}
		}
		if exists(s.tree) {
			t.Errorf("the landed %s's worktree is still there", s.story)
		}
		before = tip
	}

	if merges := gitOut(t, main.dir, "rev-list", "--merges", cut+"..main"); merges != "" {
		t.Errorf("trunk has merge commits: %s", merges)
	}
	for _, s := range stories {
		for _, f := range []string{s.test, s.source} {
			if !ontoHolds(t, main, f) {
				t.Errorf("trunk lost %s, which %s landed, to a story that landed after it", f, s.story)
			}
		}
		if _, ok := branchesOf(t, main.dir)["story/"+s.story]; ok {
			t.Errorf("story/%s is still there", s.story)
		}
	}
	if !ontoHolds(t, main, "CODEMAP.md") {
		t.Error("the first story's CODEMAP.md did not reach trunk")
	}
	for id, status := range statuses(t, main.backlog()) {
		if status != "done" {
			t.Errorf("%s is %s after all three stops, want done", id, status)
		}
	}
	for _, s := range stories[1:] {
		var record struct {
			Events []struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"events"`
			Approvals []any `json:"approvals"`
		}
		readJSON(t, main.record(s.story), &record)
		for _, e := range record.Events {
			// The loop lifts a finished story's freeze itself as it closes it,
			// and writes that as an event of the same type; `sdlc log` tells
			// the two apart by this message, and so does this.
			if e.Type == "unfreeze" && e.Message == "the story is finished, so its freeze is lifted" {
				continue
			}
			if e.Type == "unfreeze" || e.Type == "release" {
				t.Errorf("%s's record has a %s on it: a person's command came between the stops", s.story, e.Type)
			}
		}
		if len(record.Approvals) != 0 {
			t.Errorf("%s's record has %d approvals on it: a person's command came between the stops", s.story, len(record.Approvals))
		}
	}
}

// AC-8 with AC-11 as a session meets them: the runbook's one `sdlc stop` lands
// a story on a trunk that a person's commit moved, with no refusal to act on
// and no rebase for the session to run.
func TestAC8ASessionsStopLandsItsStoryOnATrunkThatMovedWithNoRebaseByHand(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	s.worked()
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "another story's work\n")
	moved := commitPaths(t, main.dir, "another story lands", "CHANGES.md")

	out, code := s.here().try(t, "stop")
	if code != 0 {
		t.Fatalf("the session's stop exited %d on a trunk that moved, want the story landed:\n%s", code, out)
	}
	tip := landTrunk(t, main)
	if tip == moved || gitOut(t, main.dir, "merge-base", moved, tip) != moved {
		t.Fatalf("trunk is at %s after the stop, want the story's commits on top of %s", tip, moved)
	}
	for _, f := range []string{s.test, s.source, "CHANGES.md"} {
		if !ontoHolds(t, main, f) {
			t.Errorf("trunk does not hold %s after the stop", f)
		}
	}
	if got := statuses(t, main.backlog())["US-001"]; got != "done" {
		t.Errorf("US-001 is %s after the stop, want done", got)
	}
}

// ---------------------------------------------------------------- AC-13

// AC-13 between two processes: two sessions stop their finished stories at the
// same moment, on a trunk a person's commit had moved, so that each has its
// commits to put onto trunk and the commands to run. Both exit 0 and trunk
// holds the commits of both. One channel lets both go; nothing waits on a
// clock, and the criterion holds however the two then fall.
func TestAC13TwoSessionsStoppingAtTheSameMomentBothLand(t *testing.T) {
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	stories := []*ontoStory{
		ontoBegun(t, binary, main, sessionFirst, "US-001"),
		ontoBegun(t, binary, main, sessionSecond, "US-002"),
	}
	for _, s := range stories {
		s.worked()
	}
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "a person's work\n")
	moved := commitPaths(t, main.dir, "a person commits", "CHANGES.md")

	type result struct {
		out bytes.Buffer
		err error
	}
	results := make([]*result, len(stories))
	gun := make(chan struct{})
	var wg sync.WaitGroup
	for i, s := range stories {
		results[i] = &result{}
		cmd := s.here().command(t, "", "stop")
		cmd.Stdout, cmd.Stderr = &results[i].out, &results[i].out
		wg.Go(func() {
			<-gun
			results[i].err = cmd.Run()
		})
	}
	close(gun)
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Errorf("the stop of %s failed (%v), want both stories landed:\n%s", stories[i].story, r.err, r.out.String())
		}
	}
	tip := landTrunk(t, main)
	if gitOut(t, main.dir, "merge-base", moved, tip) != moved {
		t.Fatalf("trunk at %s does not descend from %s, where it was before the stops", tip, moved)
	}
	if merges := gitOut(t, main.dir, "rev-list", "--merges", moved+"..main"); merges != "" {
		t.Errorf("trunk has merge commits: %s", merges)
	}
	for _, s := range stories {
		for _, f := range []string{s.test, s.source} {
			if !ontoHolds(t, main, f) {
				t.Errorf("trunk does not hold %s, which %s adds, after both stops", f, s.story)
			}
		}
	}
	for id, status := range statuses(t, main.backlog()) {
		if status != "done" {
			t.Errorf("%s is %s after both stops, want done", id, status)
		}
	}
}

// ontoPatience bounds how long the test waits for something that has to
// happen; a deadline, not a pause.
const ontoPatience = 3 * time.Minute

// AC-13, the wait: a stop that finds another story landing says whom it waits
// for, and takes nothing while it waits. US-001's stop is held open inside the
// project's test command, on the tree it has put onto trunk; US-002's stop,
// begun then, names that stop on standard error and waits. Killed outright
// there, as a session's time limit kills it, it leaves nothing behind: once
// US-001 has landed, US-002's stop is run again and lands at once, with no
// lock of its own left to wait out.
//
// Nothing here waits on a clock. The first landing is held and let go through
// named pipes, and the second stop is killed on the line it prints.
func TestAC13AStopThatWaitsForAnotherLandingSaysWhomItWaitsForAndCanBeCutShort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the first landing is held open on a named pipe made with mkfifo, which Windows has not")
	}
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	pipes := t.TempDir()
	started, release, once := filepath.Join(pipes, "started"), filepath.Join(pipes, "release"), filepath.Join(pipes, "once")
	for _, p := range []string{started, release} {
		if out, err := exec.CommandContext(t.Context(), "mkfifo", p).CombinedOutput(); err != nil {
			t.Skipf("no named pipe could be made here: %v\n%s", err, out)
		}
	}
	// The test command stops once, and only on a tree that has both US-001's
	// work and what trunk gained since: the tree US-001's stop makes.
	setConfig(t, main.dir, "commands", "test", "if test -f CHANGES.md && test -f us001.go && test ! -f '"+once+"'; then : > '"+once+
		"'; echo started > '"+started+"'; head -n 1 '"+release+"' > /dev/null; fi")
	commitEverything(t, main.dir, "a test command that can be held open")
	stories := []*ontoStory{
		ontoBegun(t, binary, main, sessionFirst, "US-001"),
		ontoBegun(t, binary, main, sessionSecond, "US-002"),
	}
	for _, s := range stories {
		s.worked()
	}
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "a person's work\n")
	commitPaths(t, main.dir, "a person commits", "CHANGES.md")
	// Both pipes are opened for reading and writing, so that neither the test
	// nor the command waits on the other to open one.
	startedPipe, err := os.OpenFile(started, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = startedPipe.Close() }()
	releasePipe, err := os.OpenFile(release, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = releasePipe.Close() }()
	var letGo sync.Once
	letFirstGo := func() { letGo.Do(func() { _, _ = releasePipe.WriteString("go\n") }) }

	var firstOut bytes.Buffer
	first := stories[0].here().command(t, "", "stop")
	first.Stdout, first.Stderr = &firstOut, &firstOut
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Wait() }()
	t.Cleanup(func() {
		letFirstGo()
		_ = first.Process.Kill()
	})
	inCommands := make(chan struct{})
	go func() {
		if _, err := bufio.NewReader(startedPipe).ReadString('\n'); err == nil {
			close(inCommands)
		}
	}()
	select {
	case <-inCommands:
	case err := <-firstDone:
		t.Fatalf("US-001's stop ended (%v) before the test command ran on the tree it put onto trunk:\n%s", err, firstOut.String())
	case <-time.After(ontoPatience):
		t.Fatalf("US-001's stop never reached the test command:\n%s", firstOut.String())
	}

	second := stories[1].here().command(t, "", "stop")
	var secondOut bytes.Buffer
	second.Stdout = &secondOut
	waiting, err := second.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	said := make(chan string)
	var all strings.Builder
	go func() {
		defer close(said)
		lines := bufio.NewScanner(waiting)
		for lines.Scan() {
			said <- lines.Text()
		}
	}()
	named := false
	deadline := time.After(ontoPatience)
	for !named {
		select {
		case line, open := <-said:
			if !open {
				_ = second.Wait()
				t.Fatalf("US-002's stop ended while US-001 was landing, without saying it waits for the stop of US-001:\n%s%s", secondOut.String(), all.String())
			}
			all.WriteString(line + "\n")
			named = strings.Contains(line, "stop US-001") && strings.Contains(line, "landing")
		case <-deadline:
			_ = second.Process.Kill()
			t.Fatalf("US-002's stop said nothing of the landing it waits for:\n%s", all.String())
		}
	}
	if err := second.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	// Whatever else it wrote before it died is read to the end, so that Wait
	// finds the pipe drained.
	for line := range said {
		all.WriteString(line + "\n")
	}
	_ = second.Wait()
	for _, f := range []string{stories[1].test, stories[1].source} {
		if ontoHolds(t, main, f) {
			t.Fatalf("trunk holds %s, which US-002 adds, though its stop was only waiting", f)
		}
	}

	letFirstGo()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("US-001's stop failed (%v) once its test command was let go:\n%s", err, firstOut.String())
		}
	case <-time.After(ontoPatience):
		t.Fatalf("US-001's stop never finished:\n%s", firstOut.String())
	}
	if !ontoHolds(t, main, stories[0].source) {
		t.Fatalf("trunk does not hold %s after US-001's stop:\n%s", stories[0].source, firstOut.String())
	}

	// Run again at once: the stop that was cut short left no lock to wait out.
	stories[1].run("", "stop")
	for _, f := range []string{stories[1].test, stories[1].source} {
		if !ontoHolds(t, main, f) {
			t.Errorf("trunk does not hold %s after US-002's stop was run again", f)
		}
	}
	for id, status := range statuses(t, main.backlog()) {
		if status != "done" {
			t.Errorf("%s is %s after both stops, want done", id, status)
		}
	}
}

// ---------------------------------------------------------------- AC-6

// AC-6: after the freeze the hook refuses an agent's write to any test file,
// one that came from trunk included: where trunk has it and the worktree does
// not yet, and where a rebase has brought it into the worktree, though the
// freeze does not hold it. `sdlc unfreeze` stays a person's.
//
// This holds without the story changing the hook: it is here so that the test
// files the landing now takes for trunk's are never taken for the session's to
// write.
func TestAC6TheHookStillRefusesAWriteToATestFileThatCameFromTrunk(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	for gate := s.nextGate(); gate != string(model.GateTestsFrozen); gate = s.nextGate() {
		s.workGate(model.Gate(gate))
	}
	s.workGate(model.GateTestsFrozen)
	writeFile(t, filepath.Join(main.dir, "trunk_test.go"), "package main\n\n// another story's test\n")
	commitPaths(t, main.dir, "another story's test lands", "trunk_test.go")
	theirs, held := filepath.Join(s.tree, "trunk_test.go"), filepath.Join(s.tree, "invoice_test.go")

	refusedEveryWay := func(t *testing.T, path string) {
		t.Helper()
		for _, agent := range []string{"", "implementer", "sdet", "verifier"} {
			s.refused(agent, "Write", writing(path))
			s.refused(agent, "Edit", editing(path))
			s.refused(agent, "Bash", running(s.at.in(s.tree, "echo '// more' >> "+filepath.Base(path))))
			s.refused(agent, "Bash", running(s.at.in(s.tree, "git checkout main -- "+filepath.Base(path))))
		}
	}
	t.Run("AC-6 before the worktree has it", func(t *testing.T) {
		if exists(theirs) {
			t.Fatal("precondition: the worktree already has trunk's new test file")
		}
		refusedEveryWay(t, theirs)
	})
	t.Run("AC-6 once a rebase has brought it in", func(t *testing.T) {
		git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--quiet", "main")
		if !exists(theirs) {
			t.Fatal("precondition: the rebase did not bring trunk's test file into the worktree")
		}
		refusedEveryWay(t, theirs)
		refusedEveryWay(t, held)
	})
	t.Run("AC-6 unfreeze stays a person's", func(_ *testing.T) {
		for _, agent := range []string{"", "implementer", "sdet"} {
			s.refused(agent, "Bash", running(s.at.in(s.tree, `sdlc unfreeze --reason "trunk's tests came in"`)))
		}
	})
}

// ---------------------------------------------------------------- AC-7, the pages

// AC-7: docs/the-loop.md, docs/troubleshooting.md and the /sdlc:next runbook
// describe landing as it then is. Each is held to no longer saying what the
// story makes untrue, and to naming what it adds: the stop puts the story onto
// trunk itself, a conflict goes to a person, a command that fails is reworked
// by the session, and trunk's test files are not held against the story.
func TestAC7TheLoopPageTroubleshootingAndTheRunbookDescribeLandingAsItIs(t *testing.T) {
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

	t.Run("AC-7 the runbook's Gate 9", func(t *testing.T) {
		gate9 := flat(section(read(t, "plugin", "skills", "next", "SKILL.md"), "## Gate 9"))
		// The session no longer rebases: no rebase is named for it to run or
		// to abandon. A command that failed on the tree about to land is its
		// own to rework, and a conflict is still a person's.
		hold(t, "the runbook's Gate 9", gate9,
			[]string{"sdlc stop", "SDLC-E0055", "conflict", "escalate"},
			[]string{"git rebase"})
	})
	t.Run("AC-7 the-loop.md, done when it lands", func(t *testing.T) {
		landing := flat(section(read(t, "docs", "the-loop.md"), "### Done when it lands"))
		hold(t, "docs/the-loop.md, Done when it lands", landing,
			[]string{"sdlc stop", "conflict", "test file"},
			[]string{"gives the rebase to run"})
	})
	t.Run("AC-7 troubleshooting.md", func(t *testing.T) {
		page := read(t, "docs", "troubleshooting.md")
		hold(t, "docs/troubleshooting.md, SDLC-E0054", flat(section(page, "### SDLC-E0054")),
			[]string{"conflict"},
			[]string{"which the session may run itself"})
		hold(t, "docs/troubleshooting.md, SDLC-E0055", flat(section(page, "### SDLC-E0055")),
			[]string{"rework"},
			[]string{"found something only a person can put right"})
		hold(t, "docs/troubleshooting.md, SDLC-E0043", flat(section(page, "### SDLC-E0043")),
			[]string{"trunk"},
			[]string{"usually ones a rebase brought in from trunk"})
	})
}
