package cli

// US-058, `sdlc parallel <ID>...`: several stories worked at once, one Claude
// Code session and one worktree each, started by a person from the main
// checkout. The sessions are a fake claude (parallel_fake_test.go) that does
// what the plan says for its story, through the loop's real commands.
//
// What a caller sees is read from what the command prints and what the fake
// sessions were handed. Refusals are matched by the code they print. The
// scheduler, the allowlist and the classification of a story are unit-tested in
// internal/parallel; interrupting a run is in test/e2e, because a test in this
// package cannot signal its own process.
//
// The tests are not parallel: the launcher starts its sessions from the
// process's environment, which a test changes with t.Setenv.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/parallel"
	"github.com/bbsnly/sdlc/internal/store"
)

// The codes the command's refusals are matched by, one for each, in the order
// the story's acceptance criteria name them.
const (
	plUnknownStory     = "SDLC-E0057"
	plNotRunnable      = "SDLC-E0058"
	plHeldElsewhere    = "SDLC-E0059"
	plListedTwice      = "SDLC-E0060"
	plFromASession     = "SDLC-E0061"
	plNeedsWorktrees   = "SDLC-E0062"
	plBadMax           = "SDLC-E0063"
	plAwaitingPerson   = "SDLC-E0064"
	plFinished         = "SDLC-E0065"
	plAllowlistWidened = "SDLC-E0066"
)

// plStories is n stories, US-001 up, in priority order.
func plStories(n int) []wtStory {
	var out []wtStory
	for i := 1; i <= n; i++ {
		out = append(out, wtStory{id: "US-00" + strconv.Itoa(i)})
	}
	return out
}

// plRepository is a main checkout with n stories, story worktrees on as in a
// project that never heard of the switch, everything committed.
func plRepository(t *testing.T, n int) string {
	t.Helper()
	return swRepository(t, plStories(n)...)
}

// plRun is `sdlc parallel args...` run from the main checkout by session, or
// by a person at a terminal when session is "".
func plRun(t *testing.T, main, session string, args ...string) result {
	t.Helper()
	return wtRun(t, main, session, "", append([]string{"parallel"}, args...)...)
}

// plRefused runs the command, which has to be refused with code, and checks
// what AC-3 asks of every refusal: it names the story, says what to do, started
// nothing and changed nothing.
func plRefused(t *testing.T, rig *plRig, main, code, story string, args ...string) string {
	t.Helper()
	before := wtLoopState(t, main)
	claims := wtFiles(t, filepath.Join(main, ".git", "sdlc"))
	r := plRun(t, main, "", args...)
	out := r.stdout + r.stderr
	if r.code == 0 {
		t.Fatalf("sdlc parallel %s went ahead where it should have been refused with %s:\n%s", strings.Join(args, " "), code, out)
	}
	if !strings.Contains(r.stderr, code) {
		t.Errorf("sdlc parallel %s was refused with something other than %s:\n%s", strings.Join(args, " "), code, out)
	}
	if story != "" && !strings.Contains(out, story) {
		t.Errorf("the refusal does not name %s:\n%s", story, out)
	}
	if rlFixLine(out) == "" {
		t.Errorf("the refusal says nothing to do:\n%s", out)
	}
	if started := rig.started(); len(started) != 0 {
		t.Errorf("a refused run started sessions for %q", started)
	}
	if !maps.Equal(before, wtLoopState(t, main)) {
		t.Error("a refused run changed the main checkout's loop state")
	}
	if !maps.Equal(claims, wtFiles(t, filepath.Join(main, ".git", "sdlc"))) {
		t.Error("a refused run changed the claims")
	}
	if entries, _ := os.ReadDir(filepath.Join(main, ".sdlc", "worktrees")); len(entries) != 0 {
		t.Errorf("a refused run made worktrees: %d", len(entries))
	}
	return out
}

// plWorktreeOf is where a session's story is held.
func plWorktreeOf(main, id string) string { return swWorktreeOf(main, id) }

// ---------------------------------------------------------------------- AC-1

func TestAC1ParallelStartsOneSessionPerStoryEachRunningTheNextSkillFromTheMainCheckout(t *testing.T) {
	main := plRepository(t, 3)
	rig := plInstall(t, map[string]plBehaviour{
		"US-001": {Actions: []string{"start"}},
		"US-002": {Actions: []string{"start"}},
	}, false)

	r := plRun(t, main, "", "US-001", "US-002")
	if r.code != 0 {
		t.Logf("exit %d:\n%s%s", r.code, r.stdout, r.stderr)
	}

	if got := rig.started(); !slices.Equal(got, []string{"US-001", "US-002"}) {
		t.Fatalf("sessions were started for %q, want one each for US-001 and US-002 and none for US-003", got)
	}
	sessions := map[string]string{}
	for id, call := range rig.calls() {
		prompts := 0
		for _, a := range call.Args {
			if a == "/sdlc:next "+id {
				prompts++
			}
		}
		if prompts != 1 {
			t.Errorf("%s's session was given %q, want the one prompt /sdlc:next %s", id, call.Args, id)
		}
		if !slices.Contains(call.Args, "-p") && !slices.Contains(call.Args, "--print") {
			t.Errorf("%s's session is not headless: %q", id, call.Args)
		}
		if !config.SamePath(call.Cwd, main) {
			t.Errorf("%s's session ran in %q, want the main checkout %q", id, call.Cwd, main)
		}
		if call.Failed != "" {
			t.Errorf("%s's session could not start its story:\n%s", id, call.Failed)
		}

		// The environment is the person's, as it is: nothing added that says
		// who the session is or turns the loop's rules off, and not hidden.
		if call.Env["PLFAKE_SENTINEL"] != "carried" {
			t.Errorf("%s's session lost the environment it was started from", id)
		}
		for _, name := range []string{store.SessionEnv, "SDLC_ENFORCE", "SDLC_BIN", "CLAUDE_PROJECT_DIR"} {
			if v, set := call.Env[name]; set {
				t.Errorf("%s's session was started with %s=%q: the launcher sets none of the variables that say who a session is", id, name, v)
			}
		}

		// Each holds its own story in the worktree of its own.
		wt := plWorktreeOf(main, id)
		if !swListed(t, main, wt) {
			t.Errorf("%s has no worktree at %s", id, wt)
		}
		if got := swClaimed(t, main, id); !config.SamePath(got, wt) {
			t.Errorf("%s is held by %q, want its own worktree %q", id, got, wt)
		}
		if active := strings.TrimSpace(wtRead(t, filepath.Join(wt, ".sdlc", "state", "active"))); active != id {
			t.Errorf("the worktree of %s is working %q", id, active)
		}
		sessions[id] = strings.TrimSpace(wtRead(t, filepath.Join(wt, ".sdlc", "state", "session")))
	}
	if sessions["US-001"] == "" || sessions["US-001"] == sessions["US-002"] {
		t.Errorf("the sessions bound to the two stories are %q: no session works two", sessions)
	}
	if wtExists(plWorktreeOf(main, "US-003")) || swClaimed(t, main, "US-003") != "" {
		t.Error("US-003 was not named, and something started it")
	}
	if wtExists(filepath.Join(main, ".sdlc", "state", "active")) {
		t.Error("the main checkout is working a story: the launcher started one in it instead of in a worktree")
	}
}

// A story already in its own worktree is not refused: a person re-runs the
// command after an approval or an interrupt, and the session resumes it.
func TestAC1AStoryAlreadyInItsOwnWorktreeIsResumedByANewSession(t *testing.T) {
	main := plRepository(t, 2)
	swStarted(t, main, swSessionA, "US-001")
	rig := plInstall(t, map[string]plBehaviour{"US-001": {Actions: []string{"start"}}}, false)
	before := swClaimed(t, main, "US-001")

	plRun(t, main, "", "US-001")

	call, ok := rig.calls()["US-001"]
	if !ok {
		t.Fatal("no session was started for a story held by its own worktree")
	}
	if call.Failed != "" {
		t.Errorf("the session could not resume the story:\n%s", call.Failed)
	}
	if got := swClaimed(t, main, "US-001"); got != before || !config.SamePath(got, plWorktreeOf(main, "US-001")) {
		t.Errorf("the claim moved from %q to %q", before, got)
	}
}

// ---------------------------------------------------------------------- AC-2

func TestAC2TheCommandSaysHowManyRunAndHowManyWait(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		peak int
	}{
		{[]string{"US-001", "US-002", "US-003"}, "running 2, waiting 1", 2},
		{[]string{"--max", "1", "US-001", "US-002", "US-003"}, "running 1, waiting 2", 1},
		{[]string{"US-001", "US-002", "US-003", "--max", "3"}, "running 3, waiting 0", 3},
		{[]string{"--max", "7", "US-001", "US-002", "US-003"}, "running 3, waiting 0", 3},
	} {
		name := strings.Join(tc.args, " ")
		t.Run(name, func(t *testing.T) {
			main := plRepository(t, 3)
			rig := plInstall(t, nil, false)
			r := plRun(t, main, "", tc.args...)
			if !strings.Contains(r.stdout+r.stderr, tc.want) {
				t.Errorf("sdlc parallel %s did not say %q:\n%s%s", name, tc.want, r.stdout, r.stderr)
			}
			if got := rig.started(); !slices.Equal(got, []string{"US-001", "US-002", "US-003"}) {
				t.Errorf("sessions were started for %q: the ones that waited start when a slot frees", got)
			}
			if got := rig.peak(); got > tc.peak {
				t.Errorf("%d sessions ran at once, the cap is %d", got, tc.peak)
			}
		})
	}
}

// The count is for a person at a terminal, and with --json standard output is
// one document a script parses.
func TestAC2WithJSONStandardOutputIsOneDocumentAndTheCountIsStillSaid(t *testing.T) {
	main := plRepository(t, 3)
	plInstall(t, nil, false)
	r := plRun(t, main, "", "--json", "US-001", "US-002", "US-003")
	var doc map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatalf("standard output is not one JSON document: %v\n%s", err, r.stdout)
	}
	if !strings.Contains(r.stdout+r.stderr, "running 2, waiting 1") {
		t.Errorf("the count was not said:\n%s%s", r.stdout, r.stderr)
	}
}

func TestAC2AMaxThatIsNotAPositiveNumberIsRefusedAndStartsNothing(t *testing.T) {
	for _, maxArg := range []string{"0", "-1", "two", "1.5", ""} {
		main := plRepository(t, 2)
		rig := plInstall(t, nil, false)
		r := plRun(t, main, "", "--max", maxArg, "US-001", "US-002")
		out := r.stdout + r.stderr
		if r.code == 0 {
			t.Errorf("--max %q was accepted:\n%s", maxArg, out)
		}
		// A flag cobra cannot parse as a number is a usage error, with its
		// own code; a number that is not positive is this command's.
		if !strings.Contains(out, plBadMax) && !strings.Contains(out, "SDLC-E0034") {
			t.Errorf("--max %q was refused with neither %s nor SDLC-E0034:\n%s", maxArg, plBadMax, out)
		}
		if _, err := strconv.Atoi(maxArg); err == nil && !strings.Contains(out, plBadMax) {
			t.Errorf("--max %q is a number, and its refusal is %s:\n%s", maxArg, plBadMax, out)
		}
		if !strings.Contains(out, "--max") {
			t.Errorf("--max %q: the refusal does not name the flag:\n%s", maxArg, out)
		}
		if got := rig.started(); len(got) != 0 {
			t.Errorf("--max %q started sessions for %q", maxArg, got)
		}
	}
}

// ---------------------------------------------------------------------- AC-3

func TestAC3AnUnknownStoryIsRefusedByNameAndNothingIsStarted(t *testing.T) {
	main := plRepository(t, 2)
	rig := plInstall(t, nil, false)
	plRefused(t, rig, main, plUnknownStory, "US-999", "US-001", "US-999")
	plRefused(t, rig, main, plUnknownStory, "US-999", "US-999", "US-001")
}

func TestAC3AnIdThatCannotBeAStoryIsRefusedBeforeAnythingIsLaunched(t *testing.T) {
	main := plRepository(t, 2)
	rig := plInstall(t, nil, false)
	for _, id := range []string{"../etc", "US-001; touch PWNED", "$(touch PWNED)", "a b", "US-001'"} {
		r := plRun(t, main, "", "US-001", id)
		if r.code == 0 {
			t.Errorf("%q was accepted as a story:\n%s", id, r.stdout)
		}
		if got := rig.started(); len(got) != 0 {
			t.Errorf("%q: sessions were started for %q", id, got)
		}
		if wtExists(filepath.Join(main, "PWNED")) {
			t.Errorf("%q ran as a command", id)
		}
	}
}

func TestAC3AStoryThatIsNotRunnableIsRefusedWhateverMakesItSo(t *testing.T) {
	for _, status := range []model.Status{model.StatusBlocked, model.StatusDropped, model.StatusDone} {
		main := plRepository(t, 2)
		rig := plInstall(t, nil, false)
		wtSetStatus(t, main, "US-002", status)
		out := plRefused(t, rig, main, plNotRunnable, "US-002", "US-001", "US-002")
		if !strings.Contains(out, string(status)) {
			t.Errorf("the refusal for a %s story does not say so:\n%s", status, out)
		}
	}
}

func TestAC3AStoryWaitingOnAnUnfinishedOneIsNotRunnable(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002", dependsOn: []string{"US-003"}}, wtStory{id: "US-003"})
	rig := plInstall(t, nil, false)
	out := plRefused(t, rig, main, plNotRunnable, "US-002", "US-001", "US-002")
	if !strings.Contains(out, "US-003") {
		t.Errorf("the refusal does not name what US-002 waits on:\n%s", out)
	}
}

// plHeldByAnotherCheckout is a repository where a checkout of a person's own, with no
// worktrees of its own, holds US-001.
func plHeldByAnotherCheckout(t *testing.T) (main, other string) {
	t.Helper()
	main = plRepository(t, 2)
	other = wtWorktree(t, main, "other")
	worktreesOff(t, other)
	wtCommit(t, other, "this checkout works its stories in place")
	if got := wtStarted(t, other, wtSessionWorktree, "US-001"); got.Story != "US-001" {
		t.Fatalf("start in the other checkout gave %q", got.Story)
	}
	return main, other
}

func TestAC3AStoryAnotherCheckoutHoldsIsRefusedNamingWhereAndNothingIsStarted(t *testing.T) {
	main, other := plHeldByAnotherCheckout(t)
	rig := plInstall(t, nil, false)
	out := plRefused(t, rig, main, plHeldElsewhere, "US-001", "US-002", "US-001")
	if !wtNames(out, other) {
		t.Errorf("the refusal does not say which checkout holds US-001 (%s):\n%s", other, out)
	}
}

func TestAC3AStoryListedTwiceIsRefusedBeforeAnythingTouchesTheBacklog(t *testing.T) {
	main := plRepository(t, 3)
	rig := plInstall(t, nil, false)
	plRefused(t, rig, main, plListedTwice, "US-002", "US-001", "US-002", "US-003", "US-002")
	// Found from the arguments alone: a story the backlog does not have, listed
	// twice, is listed twice before it is unknown.
	plRefused(t, rig, main, plListedTwice, "US-999", "US-999", "US-999")
}

// The pre-flight is all or nothing: one story that cannot be run keeps every
// other from being started.
func TestAC3OneRefusedStoryStartsNoneOfTheOthers(t *testing.T) {
	main := plRepository(t, 3)
	rig := plInstall(t, map[string]plBehaviour{
		"US-001": {Actions: []string{"start"}}, "US-003": {Actions: []string{"start"}},
	}, false)
	wtSetStatus(t, main, "US-002", model.StatusBlocked)
	plRefused(t, rig, main, plNotRunnable, "US-002", "US-001", "US-002", "US-003")
	if wtExists(plWorktreeOf(main, "US-001")) || wtExists(plWorktreeOf(main, "US-003")) {
		t.Error("a story was started in a run that was refused")
	}
}

func TestAC3ARunNeedsStoryWorktreesAndSaysHowToTurnThemOn(t *testing.T) {
	main := wtRepository(t, plStories(2)...)
	rig := plInstall(t, nil, false)
	out := plRefused(t, rig, main, plNeedsWorktrees, "", "US-001", "US-002")
	if !strings.Contains(out, "git.worktrees") {
		t.Errorf("the refusal does not name the setting:\n%s", out)
	}
}

// ------------------------------------------- AC-3, the re-run the person allowed

// plRecordWith writes story id's record in main as it stands after setup says,
// the way a run that had got that far left it.
func plRecordWith(t *testing.T, main, id string, setup func(r *model.Record)) {
	t.Helper()
	r := model.NewRecord(id, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	setup(r)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := wtRecordPath(main, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A pending escalation is a decision only a person may make; a session started
// on it would go on past it, which is an approval by another name.
func TestAC3AStoryWithAnEscalationNobodyHasAnsweredIsRefusedNotResumed(t *testing.T) {
	main := plRepository(t, 2)
	wt := swStarted(t, main, swSessionA, "US-001").Worktree
	wtMust(t, wt, swSessionA, "", "escalate", "spec_unclear", "--message", "AC-1 and AC-2 contradict each other")
	rig := plInstall(t, map[string]plBehaviour{"US-001": {Actions: []string{"start"}}}, false)

	before := wtLoopState(t, main)
	r := plRun(t, main, "", "US-001", "US-002")
	out := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(r.stderr, plAwaitingPerson) {
		t.Fatalf("a story waiting for a person was not refused with %s:\n%s", plAwaitingPerson, out)
	}
	if !strings.Contains(out, "US-001") || !strings.Contains(out, "sdlc approve US-001") {
		t.Errorf("the refusal does not name the story and the approval a person runs:\n%s", out)
	}
	if got := rig.started(); len(got) != 0 {
		t.Errorf("sessions were started for %q beside a story that waits for a person", got)
	}
	if !maps.Equal(before, wtLoopState(t, main)) {
		t.Error("the refused run changed the loop's state")
	}
}

func TestAC3AFinishedStoryIsRefusedNotWorkedAgain(t *testing.T) {
	main := plRepository(t, 2)
	rig := plInstall(t, nil, false)
	plRecordWith(t, main, "US-002", func(r *model.Record) {
		for _, g := range model.Gates {
			r.SetGate(g, model.GatePass, "", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		}
	})
	out := plRefused(t, rig, main, plFinished, "US-002", "US-001", "US-002")
	if !strings.Contains(out, "finished") {
		t.Errorf("the refusal does not say the story is finished:\n%s", out)
	}
}

// ---------------------------------------------------------------------- AC-4

func TestAC4ARunFromAModelSessionIsRefusedBeforeAnythingIsReadAndStartsNothing(t *testing.T) {
	main := plRepository(t, 2)
	rig := plInstall(t, nil, false)
	before := wtLoopState(t, main)

	// An unknown story as well: the backlog is not looked at for a caller who
	// may not run the command, so its answer cannot be had through the error.
	for _, args := range [][]string{
		{"US-001", "US-002"},
		{"US-001", "US-999"},
		{"--unattended", "US-001"},
		{"--max", "0", "US-001"},
		{"US-001", "US-001"},
	} {
		r := plRun(t, main, wtSessionThird, args...)
		out := r.stdout + r.stderr
		if r.code == 0 {
			t.Errorf("sdlc parallel %s from a model session went ahead:\n%s", strings.Join(args, " "), out)
		}
		if !strings.Contains(r.stderr, plFromASession) {
			t.Errorf("sdlc parallel %s from a model session was refused with something other than %s:\n%s",
				strings.Join(args, " "), plFromASession, out)
		}
		for _, other := range []string{plUnknownStory, plListedTwice, plBadMax, plNotRunnable} {
			if strings.Contains(out, other) {
				t.Errorf("sdlc parallel %s: the refusal for a session carries %s, so the arguments were read first", strings.Join(args, " "), other)
			}
		}
		if rlFixLine(out) == "" {
			t.Errorf("the refusal says nothing to do:\n%s", out)
		}
	}
	if got := rig.started(); len(got) != 0 {
		t.Errorf("a model session started sessions for %q", got)
	}
	if !maps.Equal(before, wtLoopState(t, main)) {
		t.Error("a refused run changed the main checkout")
	}
}

// A person has no session id, and is not asked for anything else.
func TestAC4APersonAtATerminalIsNotRefused(t *testing.T) {
	main := plRepository(t, 1)
	rig := plInstall(t, nil, false)
	r := plRun(t, main, "", "US-001")
	if strings.Contains(r.stderr, plFromASession) {
		t.Errorf("a person was refused as a model session:\n%s", r.stderr)
	}
	if got := rig.started(); !slices.Equal(got, []string{"US-001"}) {
		t.Errorf("sessions started for %q", got)
	}
}

// ---------------------------------------------------------------------- AC-5

func TestAC5HelpSaysTheSessionsAreNotInteractiveAndQuotesWhatTheFlagAllows(t *testing.T) {
	r := wtMust(t, wtTempDir(t), "", "", "parallel", "--help")
	help := strings.Join(strings.Fields(r.stdout), " ")
	lower := strings.ToLower(help)
	for _, want := range []string{"--unattended", "--max", "parallel.allow", "not interactive"} {
		if !strings.Contains(lower, want) {
			t.Errorf("--help does not say %q:\n%s", want, help)
		}
	}
	for _, entry := range parallel.Allowlist() {
		if !strings.Contains(help, entry) {
			t.Errorf("--help does not quote the allowlist entry %q:\n%s", entry, help)
		}
	}
}

// plAllowed is what the session of the story was pre-approved to use.
func plAllowed(call plCall) []string {
	var out []string
	collecting := false
	for _, a := range call.Args {
		switch {
		case a == "--allowedTools" || a == "--allowed-tools":
			collecting = true
		case strings.HasPrefix(a, "--allowedTools=") || strings.HasPrefix(a, "--allowed-tools="):
			_, v, _ := strings.Cut(a, "=")
			out = append(out, strings.Split(v, ",")...)
			collecting = false
		case strings.HasPrefix(a, "-"):
			collecting = false
		case collecting:
			out = append(out, strings.Split(a, ",")...)
		}
	}
	return out
}

// plSkips is whether any argument turns the permission prompts off wholesale.
func plSkips(call plCall) bool {
	for _, a := range call.Args {
		l := strings.ToLower(a)
		if strings.Contains(l, "skip-permissions") || strings.Contains(l, "bypass") {
			return true
		}
	}
	return false
}

func TestAC5ByDefaultASessionIsGivenNoToolsAndNoSkip(t *testing.T) {
	main := plRepository(t, 2)
	rig := plInstall(t, nil, false)
	plRun(t, main, "", "US-001", "US-002")
	if len(rig.calls()) != 2 {
		t.Fatalf("sessions: %q", rig.started())
	}
	for id, call := range rig.calls() {
		if got := plAllowed(call); len(got) != 0 {
			t.Errorf("%s's session was pre-approved %q without the flag", id, got)
		}
		if plSkips(call) {
			t.Errorf("%s's session was started with a skip: %q", id, call.Args)
		}
		for _, a := range call.Args {
			if strings.Contains(a, "unattended") {
				t.Errorf("%s's session was handed the launcher's own flag: %q", id, a)
			}
		}
	}
}

func TestAC5TheUnattendedFlagGrantsTheBuiltInListAndNeverASkip(t *testing.T) {
	for _, args := range [][]string{{"--unattended", "US-001", "US-002"}, {"US-001", "US-002", "--unattended"}} {
		main := plRepository(t, 2)
		rig := plInstall(t, nil, false)
		plRun(t, main, "", args...)
		if len(rig.calls()) != 2 {
			t.Fatalf("sdlc parallel %s: sessions %q", strings.Join(args, " "), rig.started())
		}
		want := slices.Sorted(slices.Values(parallel.Allowlist()))
		for id, call := range rig.calls() {
			got := slices.Sorted(slices.Values(plAllowed(call)))
			if !slices.Equal(got, want) {
				t.Errorf("sdlc parallel %s: %s's session was pre-approved\n  %q\nwant the built-in list\n  %q", strings.Join(args, " "), id, got, want)
			}
			if plSkips(call) {
				t.Errorf("an unattended session was started with a skip: %q", call.Args)
			}
		}
	}
}

func TestAC5ConfigurationCanNarrowTheListAndNothingElse(t *testing.T) {
	list := parallel.Allowlist()
	for name, tc := range map[string]struct {
		allow []string
		want  []string
	}{
		"one entry":   {[]string{list[0]}, []string{list[0]}},
		"two entries": {[]string{list[len(list)-1], list[0]}, []string{list[0], list[len(list)-1]}},
		"none":        {[]string{}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			main := plRepository(t, 1)
			setConfigOnDisk(t, main, "parallel", map[string]any{"allow": tc.allow})
			rig := plInstall(t, nil, false)
			plRun(t, main, "", "--unattended", "US-001")
			call, ok := rig.calls()["US-001"]
			if !ok {
				t.Fatal("no session was started")
			}
			got := slices.Sorted(slices.Values(plAllowed(call)))
			if !slices.Equal(got, slices.Sorted(slices.Values(tc.want))) {
				t.Errorf("the session was pre-approved %q, want %q", got, tc.want)
			}
		})
	}
}

// config.json is a file a session on the project can write, so a person's
// next unattended run must not hand it a wider list than the built-in one.
func TestAC5AConfiguredEntryThatIsNotBuiltInIsRefusedNotGranted(t *testing.T) {
	for _, widened := range []string{"Bash(*)", "Bash", "Write", "WebFetch", "Bash(git push:*)"} {
		main := plRepository(t, 1)
		setConfigOnDisk(t, main, "parallel", map[string]any{"allow": []string{parallel.Allowlist()[0], widened}})
		rig := plInstall(t, nil, false)
		r := plRun(t, main, "", "--unattended", "US-001")
		out := r.stdout + r.stderr
		if r.code == 0 || !strings.Contains(r.stderr, plAllowlistWidened) {
			t.Errorf("%q in parallel.allow was not refused with %s:\n%s", widened, plAllowlistWidened, out)
		}
		if !strings.Contains(out, widened) || !strings.Contains(out, "parallel.allow") {
			t.Errorf("the refusal does not name the entry and the setting:\n%s", out)
		}
		if got := rig.started(); len(got) != 0 {
			t.Errorf("%q: sessions were started for %q", widened, got)
		}
	}
}

// ---------------------------------------------------------------------- AC-6

type plRow struct {
	Story   string `json:"story"`
	Gate    string `json:"gate"`
	State   string `json:"state"`
	Command string `json:"command"`
	Exit    int    `json:"exit"`
}

type plReport struct {
	Stories []plRow `json:"stories"`
}

// plMixed is a run whose four sessions end four different ways: one is left
// with its story unfinished and nothing pending, two escalate (one then exits
// with a failing status), and one exits with a failing status having done
// nothing.
func plMixed(t *testing.T) (main string, rig *plRig) {
	t.Helper()
	main = plRepository(t, 4)
	rig = plInstall(t, map[string]plBehaviour{
		"US-001": {Actions: []string{"start"}},
		"US-002": {Actions: []string{"start", "escalate"}, Message: "\x1b[31mred\x1b[0m \x1b]0;pwned\x07 the migration has no down step"},
		"US-003": {Exit: 3},
		"US-004": {Actions: []string{"start", "escalate"}, Exit: 1},
	}, false)
	return main, rig
}

func TestAC6TheStatusViewGivesEveryStoryItsGateAndItsState(t *testing.T) {
	main, _ := plMixed(t)
	r := plRun(t, main, "", "--json", "--max", "2", "US-001", "US-002", "US-003", "US-004")
	report := decode[plReport](t, r)
	if len(report.Stories) != 4 {
		t.Fatalf("the view lists %d stories, want 4:\n%s", len(report.Stories), r.stdout)
	}
	byID := map[string]plRow{}
	var order []string
	for _, row := range report.Stories {
		byID[row.Story] = row
		order = append(order, row.Story)
	}
	if !slices.Equal(order, []string{"US-001", "US-002", "US-003", "US-004"}) {
		t.Errorf("the stories are listed as %q, want the order they were named in", order)
	}
	first := string(model.Gates[0])

	// Left unfinished with nothing pending: not a failure, not a person's turn.
	if row := byID["US-001"]; row.State != "stopped" || row.Gate != first || row.Command != "" {
		t.Errorf("US-001 = %+v, want stopped at %s with nothing to run", row, first)
	}
	// Ended by a person's decision: waiting, with the command to run, whatever
	// the exit status of the session that escalated.
	for _, id := range []string{"US-002", "US-004"} {
		if row := byID[id]; row.State != "waiting" || !strings.Contains(row.Command, "sdlc approve "+id) {
			t.Errorf("%s = %+v, want waiting with sdlc approve %s", id, row, id)
		}
	}
	// An exit with none of those is a failure, with its status.
	if row := byID["US-003"]; row.State != "failed" || row.Exit != 3 || row.Command != "" {
		t.Errorf("US-003 = %+v, want failed with exit 3", row)
	}
}

func TestAC6TheProseViewGathersWhatWaitsOnAPersonLastWithTheCommandToRun(t *testing.T) {
	main, _ := plMixed(t)
	r := plRun(t, main, "", "--max", "2", "US-001", "US-002", "US-003", "US-004")
	out := r.stdout + r.stderr

	for _, id := range []string{"US-001", "US-002", "US-003", "US-004"} {
		if !strings.Contains(out, id) {
			t.Errorf("the view never mentions %s:\n%s", id, out)
		}
	}
	// A story's own line says what became of it, in the words the view uses.
	for id, wrong := range map[string][]string{
		"US-002": {"failed", "stopped"},
		"US-004": {"failed", "stopped"},
		"US-003": {"waiting"},
		"US-001": {"failed", "waiting"},
	} {
		for line := range strings.Lines(out) {
			if !strings.Contains(line, id) || strings.Contains(line, "sdlc approve") {
				continue
			}
			for _, w := range wrong {
				if strings.Contains(line, w) {
					t.Errorf("%s is shown as %q: %s", id, w, strings.TrimSpace(line))
				}
			}
		}
	}
	// The commands come after every story's own state, in one place.
	approveAt := strings.Index(out, "sdlc approve US-002")
	if approveAt < 0 || !strings.Contains(out, "sdlc approve US-004") {
		t.Fatalf("the view does not give the commands a person runs:\n%s", out)
	}
	for _, word := range []string{"failed", "stopped"} {
		if at := strings.LastIndex(out, word); at > approveAt {
			t.Errorf("%q appears after the gathered commands: they are not last\n%s", word, out)
		}
	}
	if first, last := strings.Index(out, "sdlc approve US-002"), strings.Index(out, "sdlc approve US-004"); abs(first-last) > 400 {
		t.Errorf("the commands are not in one place (%d bytes apart):\n%s", abs(first-last), out)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// What a story's escalation message says came from a file the clone brought
// along, and could retitle or overwrite the person's terminal.
func TestAC6NothingAStoryCarriesCanControlThePersonsTerminal(t *testing.T) {
	main, _ := plMixed(t)
	var backlog map[string]any
	wtReadJSON(t, filepath.Join(main, "user_stories.json"), &backlog)
	for _, s := range backlog["stories"].([]any) {
		if story := s.(map[string]any); story["id"] == "US-001" {
			story["title"] = "Story \x1b]0;pwned\x07\x1b[2J US-001"
		}
	}
	wtWriteJSON(t, filepath.Join(main, "user_stories.json"), backlog)
	wtCommit(t, main, "a title with escapes")

	r := plRun(t, main, "", "--max", "2", "US-001", "US-002", "US-003", "US-004")
	for _, stream := range []string{r.stdout, r.stderr} {
		for _, c := range stream {
			if c == '\x1b' || c == '\x07' || c == '\r' || c == 0x7f || (c < 0x20 && c != '\n' && c != '\t') {
				t.Fatalf("the output carries the control character %q:\n%q", c, stream)
			}
		}
	}
}

// ---------------------------------------------------------------------- AC-7

func TestAC7TheRunPushesNothingAndAsksNoRemote(t *testing.T) {
	main := plRepository(t, 2)
	rig := plInstall(t, map[string]plBehaviour{
		"US-001": {Actions: []string{"start", "escalate"}}, "US-002": {Actions: []string{"start"}},
	}, true)
	plRun(t, main, "", "--unattended", "US-001", "US-002")
	if len(rig.started()) != 2 {
		t.Fatalf("sessions: %q", rig.started())
	}
	for _, line := range rig.gitCommands() {
		words := strings.Fields(line)
		for len(words) > 0 && strings.HasPrefix(words[0], "-") {
			words = words[1:] // git's own options, and -C <dir> or -c <k=v> below
		}
		for _, w := range words {
			switch w {
			case "push", "fetch", "pull", "ls-remote", "clone", "send-pack", "request-pull":
				t.Errorf("a git command touched a remote: git %s", line)
			}
		}
	}
}

func TestAC7TheCommandBuildsNoNetworkCallAndNoProcessOfItsOwn(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "parallel.go", nil, 0)
	if err != nil {
		t.Fatalf("the command is in parallel.go of this package: %v", err)
	}
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		switch path {
		case "net", "net/http", "net/rpc", "net/smtp":
			t.Errorf("parallel.go imports %s", path)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := n.X.(*ast.Ident); ok && pkg.Name == "exec" && (n.Sel.Name == "Command" || n.Sel.Name == "CommandContext") {
				t.Errorf("parallel.go builds a process with exec.%s: sessions go through shellx.Command", n.Sel.Name)
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING && strings.Contains(strings.ToLower(n.Value), "git push") {
				t.Errorf("parallel.go has %s in it", n.Value)
			}
		}
		return true
	})
}

// ---------------------------------------------------------------------- AC-8

// The launcher reads the records and writes none. Whatever a session did to a
// story is the session's, and a run never says a story is done.
func TestAC8TheLauncherWritesNoRecordClaimActiveSessionOrStatusOfItsOwn(t *testing.T) {
	for name, exit := range map[string]int{"sessions that end cleanly": 0, "sessions that fail": 5} {
		t.Run(name, func(t *testing.T) {
			main := plRepository(t, 3)
			rig := plInstall(t, map[string]plBehaviour{
				"US-001": {Exit: exit}, "US-002": {Exit: exit}, "US-003": {Exit: exit},
			}, false)
			state := wtLoopState(t, main)
			claims := wtFiles(t, filepath.Join(main, ".git", "sdlc", "claims"))

			plRun(t, main, "", "US-001", "US-002", "US-003")

			if len(rig.started()) != 3 {
				t.Fatalf("sessions: %q", rig.started())
			}
			if after := wtLoopState(t, main); !maps.Equal(state, after) {
				t.Errorf("the launcher changed the loop's state: %s", wtDifference(state, after))
			}
			if after := wtFiles(t, filepath.Join(main, ".git", "sdlc", "claims")); !maps.Equal(claims, after) {
				t.Error("the launcher changed the claims")
			}
		})
	}
}

func TestAC8NoStoryIsEverRecordedDoneByARun(t *testing.T) {
	main := plRepository(t, 2)
	plInstall(t, map[string]plBehaviour{
		"US-001": {Actions: []string{"start"}}, "US-002": {Actions: []string{"start", "escalate"}},
	}, false)
	plRun(t, main, "", "US-001", "US-002")
	for id, status := range wtStatuses(t, main) {
		if status == string(model.StatusDone) {
			t.Errorf("%s is %s after a run", id, status)
		}
	}
}

// wtDifference names the files that differ between two states, for a message.
func wtDifference(a, b map[string]string) string {
	var names []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			names = append(names, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
