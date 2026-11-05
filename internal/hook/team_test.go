package hook

// US-109 at the hook. It makes no git call and imports no cli, so it reads the team from
// the file and who stood down from the record. What it says of a stale review is the
// same rule cli's gates ask (THREATS.md, T10): a second copy of that rule is how a commit
// is held to a reviewer the gate never waited for, or let past one it did.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

func teamFile(agents string) string {
	return `{"version":1,"team":{"agents":{` + agents + `}}}`
}

// needed turns a row of the record's staffing on, whatever the row said.
func needed(agent string) func(r *model.Record) {
	return func(r *model.Record) {
		for i := range r.Staffing.Agents {
			if r.Staffing.Agents[i].Agent == agent {
				r.Staffing.Agents[i].Needed = true
			}
		}
	}
}

// aCopy puts a copy's row on the record, with its review of an older tree.
func aCopy(name string, gate model.Gate, isNeeded bool) func(r *model.Record) {
	return func(r *model.Record) {
		r.Staffing.Agents = append(r.Staffing.Agents, model.Staffed{Agent: name, Needed: isNeeded, Reason: "for the test"})
		r.AddReview(model.Review{Gate: gate, Role: name, Verdict: model.VerdictApprove, Subject: "the tree before the rework"}, time.Now())
	}
}

func TestAC4AC8ACommitIsHeldOnlyToTheReviewsTheGateWaitsFor(t *testing.T) {
	for _, tc := range []struct {
		name, config, stale string
		change              func(r *model.Record)
		refused             bool
	}{
		// AC-4: a disabled agent's row is not read, and nor is its old review.
		{"AC_4 a disabled reviewer whose row says needed", teamFile(`"human-advocate":{"enabled":false}`),
			"human-advocate", needed("human-advocate"), false},
		{"AC_4 a disabled reviewer whose row leaves it out", teamFile(`"human-advocate":{"enabled":false}`),
			"human-advocate", nil, false},
		{"AC_4 a disabled reviewer a path brought back", teamFile(`"security":{"enabled":false}`),
			"security", func(r *model.Record) {
				r.Staffing.BroughtBack = map[string]string{"security": "internal/hook/extra.go"}
			}, false},
		{"AC_10 disabled security on a story recorded as sensitive", teamFile(`"security":{"enabled":false}`),
			"security", func(r *model.Record) { r.SetSecuritySensitive(true) }, false},
		{"AC_4 a disabled reviewer with no staffing recorded", teamFile(`"perf":{"enabled":false}`),
			"perf", func(r *model.Record) { r.Staffing = nil }, false},
		{"AC_4 a disabled reviewer with the project manager disabled too",
			teamFile(`"project-manager":{"enabled":false},"perf":{"enabled":false}`), "perf", nil, false},
		// One agent's setting does nothing for another.
		{"AC_4 another reviewer disabled, this one needed", teamFile(`"perf":{"enabled":false}`),
			"human-advocate", needed("human-advocate"), true},
		// AC-5: these two stay required although disabled. A person recorded the review, and
		// the tree has moved since.
		{"AC_5 a disabled verifier's review a person recorded", teamFile(`"verifier":{"enabled":false}`),
			"verifier", nil, true},
		{"AC_5 a disabled code reviewer's review a person recorded", teamFile(`"code-reviewer":{"enabled":false}`),
			"code-reviewer", nil, true},
		// AC-6: advisory is whether a block stops the gate. It is still required.
		{"AC_6 an advisory code reviewer", teamFile(`"code-reviewer":{"can_block":false}`),
			"code-reviewer", nil, true},
		// AC-8: the hook enumerates the roster today, so a copy's stale review goes unnoticed.
		{"AC_8 a copy's stale review", teamFile(`"code-reviewer":{"instances":2}`),
			"code-reviewer-2", aCopy("code-reviewer-2", model.GateCodeReview, true), true},
		{"AC_8 a copy of the verifier", teamFile(`"verifier":{"instances":2}`),
			"verifier-2", aCopy("verifier-2", model.GateVerifierReview, true), true},
		// With the project manager disabled no row says anything, and every copy waits.
		{"AC_8 a copy with the project manager disabled", teamFile(`"project-manager":{"enabled":false},"perf":{"instances":2}`),
			"perf-2", aCopy("perf-2", model.GateCodeReview, false), true},
		// Lowered mid-story: the review stands on the record, and the gate no longer waits.
		{"AC_8 a copy beyond instances", teamFile(`"code-reviewer":{"instances":2}`),
			"code-reviewer-3", aCopy("code-reviewer-3", model.GateCodeReview, true), false},
		{"AC_8 a copy with no setting", `{"version":1}`,
			"code-reviewer-2", aCopy("code-reviewer-2", model.GateCodeReview, true), false},
		{"AC_8 a copy the staffing left out", teamFile(`"perf":{"instances":2}`),
			"perf-2", aCopy("perf-2", model.GateCodeReview, false), false},
		{"AC_8 a copy whose role is disabled", teamFile(`"perf":{"instances":2,"enabled":false}`),
			"perf-2", aCopy("perf-2", model.GateCodeReview, true), false},
		// The person's decision at Gate 4: a disabled agent's instances are not waited for, the
		// verifier's and the code reviewer's included. Only the base role's review is held.
		{"AC_4 an instance of the disabled verifier", teamFile(`"verifier":{"instances":2,"enabled":false}`),
			"verifier-2", aCopy("verifier-2", model.GateVerifierReview, true), false},
		{"AC_4 an instance of the disabled code reviewer", teamFile(`"code-reviewer":{"instances":2,"enabled":false}`),
			"code-reviewer-2", aCopy("code-reviewer-2", model.GateCodeReview, true), false},
		{"AC_5 the base role's review of the disabled verifier with instances set", teamFile(`"verifier":{"instances":2,"enabled":false}`),
			"verifier", nil, true},
		{"AC_5 the base role's review of the disabled code reviewer with instances set", teamFile(`"code-reviewer":{"instances":2,"enabled":false}`),
			"code-reviewer", nil, true},
		// Red team R3 (V14): a path brings back every instance of the role it is listed on, and
		// the record says so under the instance's own name, as it records the review. A reading
		// keyed by the base role alone would let perf-2's stale review through.
		{"AC_7 an instance a path brought back", teamFile(`"perf":{"instances":2}`),
			"perf-2", func(r *model.Record) {
				aCopy("perf-2", model.GateCodeReview, false)(r)
				r.Staffing.BroughtBack = map[string]string{"perf-2": "internal/hot/loop.go"}
			}, true},
		// Red team R10 (V21): with the project manager disabled as well, the disabled verifier's
		// gate is still a person's, and the review a person recorded is still held.
		{"AC_5 a disabled verifier with the project manager disabled too",
			teamFile(`"project-manager":{"enabled":false},"verifier":{"enabled":false}`), "verifier", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			commitHeldTo(t, tc.config, tc.stale, tc.change, tc.refused)
		})
	}
}

// customSettings are the settings the defaults would get wrong for a project: where its
// tests and its backlog are, what its trunk is called, and which tiers wait for a person.
const customSettings = `"backlog":{"path":"planning/backlog.json"},"git":{"trunk_branch":"trunk"},` +
	`"paths":{"tests":{"dirs":["checks/"],"file_globs":["*.check.js"]}},` +
	`"human_gates":{"pre_commit_pause_tiers":["medium"]}`

// commitUnder asks the hook about a commit on a record that passed every gate before it,
// with the file given as the project's configuration. stale's review is of another tree,
// where there is one, and the story's risk tier is the one given. The tree is measured under a
// file that loads, since the file under test may not.
func commitUnder(t *testing.T, file, tier, stale string) reply {
	t.Helper()
	root := loopProject(t)
	story := `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress","risk_tier":"` + tier + `"}]}` + "\n"
	write(t, root, "user_stories.json", story)
	write(t, root, "planning/backlog.json", story)
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	initRepo := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	initRepo.Dir = root
	if out, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	write(t, root, "invoice.go", "package invoice\n")
	cfg, err := config.Parse([]byte(`{"version":1,` + customSettings + `}`))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.GateCommit.Before() {
		record.SetGate(g, model.GatePass, "", at)
	}
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			subject := tree
			if r.Role == stale {
				subject = "the tree before the rework"
			}
			record.AddReview(model.Review{Gate: gate, Role: r.Role, Verdict: model.VerdictApprove, Subject: subject}, at)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
	write(t, root, ".sdlc/config.json", file)
	return call(t, command(root, "", "git commit -m done"), noEnv)
}

// T7 and red team V2. A file refused as an old key or an unusable team was read: its values
// are good JSON, and every command in a person's hands says what to fix. A project reaches this
// with no mistake of its own, in the middle of a story, and the defaults would drop its test
// paths, its backlog, its trunk and its pause while a model is working. A row that held only
// that the hook answers passed with `config.Load` at every site.
//
// A file the decoder could not finish, or that holds a problem from outside the team, answers
// as it always has: a value from a newer format is never misread (red team C4).
func TestAC13TheHookReadsTheProjectsOwnSettingsFromARefusedFile(t *testing.T) {
	held := func(root, path string) bool {
		return denied(call(t, event(root, "Write", "sdlc-implementer", path), noEnv))
	}
	for name, rest := range map[string]string{
		"AC_12 a staffing section":         `"staffing":{"enabled":true,"lowest":{"verifier":{"model":"opus"}}}`,
		"AC_12 a staffing section of null": `"staffing":null`,
		"AC_12 a staffing of no shape":     `"staffing":3`,
		"AC_12 gate7_advisory true":        `"reviews":{"gate7_advisory":true}`,
		"AC_13 a negative retries":         `"team":{"retries":-1}`,
		"AC_13 a key that is not an agent": `"team":{"agents":{"designer":{"enabled":false}}}`,
		"AC_8 instances past the ceiling":  `"team":{"agents":{"perf":{"instances":17}}}`,
		"AC_13 an old key beside a team":   `"staffing":{},"team":{"retries":-1}`,
		// Red team R4 (V15): the clamp is both ways. Read upward only, zero seats would let a
		// stale verifier review through on this path.
		"AC_8 instances of zero":      `"team":{"agents":{"verifier":{"instances":0}}}`,
		"AC_8 instances of minus one": `"team":{"agents":{"verifier":{"instances":-1}}}`,
	} {
		file := `{"version":1,` + customSettings + `,` + rest + `}`
		t.Run(name, func(t *testing.T) {
			root := loopProject(t)
			write(t, root, ".sdlc/config.json", file)
			for _, path := range []string{"checks/total.txt", "billing/total.check.js", "planning/backlog.json"} {
				if !held(root, path) {
					t.Errorf("T7 a write to %s went through: the file's own settings were dropped", path)
				}
			}
			// Red team R6 (V17): the shell's write is read at another site from the Write tool's.
			// Swapping some of the sites passed the rows above.
			if !denied(call(t, command(root, "sdlc:implementer", "echo x > checks/total.txt"), noEnv)) {
				t.Error("T7 a shell write to checks/total.txt went through: the file's own test paths were dropped")
			}
			if held(root, "internal/invoice_test.go") {
				t.Error("T7 a default test path was held in a project that names its own")
			}
			if got := trunkOf(placeOf(root)); got != "trunk" {
				t.Errorf("T7 trunk is %q, want the project's own", got)
			}
			if r := commitUnder(t, file, "low", "verifier"); !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "verifier") {
				t.Errorf("T7 a commit over a stale review went through: %+v", r.HookSpecificOutput)
			}
			if r := commitUnder(t, file, "medium", ""); !denied(r) {
				t.Error("T7 the project's own tier did not wait for a person")
			}
			if r := commitUnder(t, file, "high", ""); denied(r) {
				t.Errorf("T7 the default tiers were read in place of the project's own: %s", r.HookSpecificOutput.PermissionDecisionReason)
			}
		})
	}
	// Red team R6 (V17), the freeze: a new test file under the project's own test directory,
	// written through the shell once the tests are frozen, is read at a site of its own. The
	// file is refused for an old key, which the released tool refuses as well.
	t.Run("AC_13 a new test file under the freeze", func(t *testing.T) {
		root := loopProject(t)
		write(t, root, ".sdlc/config.json", `{"version":1,`+customSettings+`,"staffing":3}`)
		freeze(t, root, "A-1", "checks/frozen.txt", "frozen\n")
		if !denied(call(t, command(root, "sdlc:implementer", "echo x > checks/new.txt"), noEnv)) {
			t.Error("T7 a new test file under checks/ went through under the freeze: the file's own test paths were dropped")
		}
	})
	// Security note 5 and red team H3, here since round 2, because on their own they passed
	// before the code existed: a refused file keeps its values on the hook's path, so an
	// `instances` far past the ceiling reaches every reader there. Out of memory in Go is fatal
	// and a recover answers nothing, so each reader bounds it from above as well. The first
	// value fails by its size and the second by not returning. The old key beside it is one the
	// released tool refuses too, so the file is read for its values only once ForRules is there.
	for _, n := range []string{"2000000", "2000000000"} {
		t.Run("AC_8 instances "+n, func(t *testing.T) {
			file := `{"version":1,` + customSettings + `,"staffing":3,"team":{"agents":{"perf":{"instances":` + n + `},"verifier":{"instances":` + n + `}}}}`
			root := loopProject(t)
			write(t, root, ".sdlc/config.json", file)
			if !held(root, "checks/total.txt") {
				t.Error("T7 a write to checks/total.txt went through: the file's own settings were dropped")
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			r := commitUnder(t, file, "low", "verifier")
			runtime.ReadMemStats(&after)
			if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "verifier") {
				t.Errorf("a commit over a stale review went through: %+v", r.HookSpecificOutput)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<20 {
				t.Errorf("one hook call allocated %d bytes on an instances the configuration refuses", grew)
			}
		})
	}
	for name, file := range map[string]string{
		"AC_13 a version too high beside a team problem": `{"version":2,` + customSettings + `,"team":{"retries":-1}}`,
		"AC_12 a version too high beside an old key":     `{"version":2,` + customSettings + `,"staffing":{}}`,
		"AC_13 a negative limit beside a team problem":   `{"version":1,` + customSettings + `,"team":{"retries":-1},"loop":{"max_review_rounds":-1}}`,
		"AC_13 a wrong type inside the team":             `{"version":1,` + customSettings + `,"team":{"agents":{"security":{"enabled":"false"}}}}`,
		"AC_13 the team of the wrong type":               `{"version":1,` + customSettings + `,"team":"yes"}`,
		"AC_13 a file that is not JSON":                  `{not json`,
	} {
		t.Run(name, func(t *testing.T) {
			root := loopProject(t)
			write(t, root, ".sdlc/config.json", file)
			if !held(root, "internal/invoice_test.go") {
				t.Error("T7 the hook read a value from a file it should not read: a default test path was not held")
			}
			if held(root, "checks/total.txt") {
				t.Error("T7 the hook read a value from a file it should not read: a path the file names was held")
			}
			if got := trunkOf(placeOf(root)); got != "" {
				t.Errorf("T7 trunk is %q, read from a file it should not read", got)
			}
			if r := commitUnder(t, file, "low", "verifier"); denied(r) {
				t.Errorf("T7 a commit was refused for what the file says, in a file that does not read: %s", r.HookSpecificOutput.PermissionDecisionReason)
			}
		})
	}
}

// Red team R6 (V17): the stop guard reads its limit at its own site. A refused file read as
// the defaults takes max_stop_blocks 0 for 3, and hands a story over against the project's
// own setting.
func TestAC13TheStopGuardReadsTheProjectsOwnLimitFromARefusedFile(t *testing.T) {
	for name, rest := range map[string]string{
		"AC_12 a staffing section":     `"staffing":{"enabled":true,"lowest":{"verifier":{"model":"opus"}}}`,
		"AC_12 a staffing of no shape": `"staffing":3`,
		"AC_13 a team problem":         `"team":{"retries":-1}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := storyUnderWay(t)
			write(t, root, ".sdlc/config.json", `{"version":1,"loop":{"max_stop_blocks":0},`+rest+`}`)
			if r := stop(t, root, false, noEnv); r.Decision != "" || r.SystemMessage != "" {
				t.Errorf("T7 max_stop_blocks 0 in a refused file was read as the default: %+v", r)
			}
			if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
				t.Error("T7 with the guard off in the file, a stop still handed the story to a person")
			}
		})
	}
}

// T5 at the hook, which asks WaitsForApproval before `git commit`. A by-hand or failure
// hand-over approved on a high-tier story with the tree as it stands does not answer the
// commit (red team V3), and the control does. The commit is refused here before the gate is
// reached, so this is the reader a closure at the gate alone would leave open.
func TestAC9AnApprovedFailureDoesNotLetACommitThroughTheHook(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		answers bool
	}{
		{"agent_failed", false},
		{"work_by_hand", false},
		{"pre_commit_approval", true},
		{"review_not_converging", true},
	} {
		t.Run("AC_9 "+tc.kind, func(t *testing.T) {
			root := loopProject(t)
			write(t, root, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress","risk_tier":"high"}]}`+"\n")
			t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
			t.Setenv("GIT_WORK_TREE", root)
			initRepo := exec.CommandContext(t.Context(), "git", "init", "--quiet")
			initRepo.Dir = root
			if out, err := initRepo.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v\n%s", err, out)
			}
			write(t, root, "invoice.go", "package invoice\n")
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			at := time.Now()
			record := model.NewRecord("A-1", at)
			for _, g := range model.GateCommit.Before() {
				record.SetGate(g, model.GatePass, "", at)
			}
			record.Approvals = []model.Approval{{At: model.Timestamp(at), Decision: model.DecisionApproved, Type: tc.kind, TreeHash: tree}}
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))

			r := call(t, command(root, "", "git commit -m done"), noEnv)
			reason := r.HookSpecificOutput.PermissionDecisionReason
			switch {
			case tc.answers && denied(r):
				t.Errorf("the approved answer did not let the commit through: %s", reason)
			case !tc.answers && !denied(r):
				t.Error("T5 the commit went through on an answer that does not answer it")
			case !tc.answers && !strings.Contains(reason, "pre_commit_approval"):
				t.Errorf("the refusal does not say how to ask the question: %s", reason)
			}
		})
	}
}

// AC-9 through the hook: reporting a failure is the session's, and not the project
// manager's. The spellings are internal/shellpolicy's.
func TestAC9TheFailureCommandGoesThroughTheHookForTheSessionAndNotForTheProjectManager(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	const reported = `sdlc team failure researcher --reason "it returned nothing"`
	if r := call(t, command(root, "", reported), noEnv); denied(r) {
		t.Errorf("AC-9 the session was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
	refusedUnder(t, call(t, command(root, "sdlc:project-manager", reported), noEnv),
		"project-manager-only-staffs", "the project manager", reported)
}

// T1 through the hook: the event Claude Code sends, with the name a copy records under.
func TestAC8TheHookKeepsACopysReviewToItsReviewer(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	const recorded = "sdlc review add code_review code-reviewer-2 approve"
	for _, agent := range []string{"", "sdlc:implementer", "sdlc:security", "sdlc:code-reviewer-2", "general-purpose"} {
		refusedUnder(t, call(t, command(root, agent, recorded), noEnv), "review-is-recorded-by-its-reviewer", who(agent), recorded)
	}
	if r := call(t, command(root, "sdlc:code-reviewer", recorded), noEnv); denied(r) {
		t.Errorf("AC-8 the code reviewer was refused its own copy's review: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
}

// T3: the hook sees the role's agent type for every copy, and a review records no run. So
// where a role has copies with models of their own, the trace does not say which it asked
// for, as though it knew which copy this run was.
func TestAC8TheTraceDoesNotClaimWhichCopyARunWas(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	write(t, root, ".sdlc/config.json", teamFile(`"security":{"instances":2}`))
	trStaffed(t, root, &model.Staffing{At: "2026-10-28T10:00:00Z", Agents: []model.Staffed{
		{Agent: "security", Needed: true, AskedModel: "opus", AskedEffort: "high"},
		{Agent: "security-2", Needed: true, AskedModel: "haiku", AskedEffort: "low"},
		{Agent: "verifier", Needed: true, AskedModel: "sonnet", AskedEffort: "medium"},
	}})
	trSubagent(t, root, "sdlc:security", "a1", nil, noEnv, nil)
	trAbsent(t, trOne(t, root, "agent_run"), "asked_model", "asked_effort")

	// A role with no copy still says what it was asked for.
	other := trProject(t)
	write(t, other, ".sdlc/config.json", teamFile(`"security":{"instances":2}`))
	trStaffed(t, other, &model.Staffing{At: "2026-10-28T10:00:00Z", Agents: []model.Staffed{
		{Agent: "security-2", Needed: true, AskedModel: "haiku", AskedEffort: "low"},
		{Agent: "verifier", Needed: true, AskedModel: "sonnet", AskedEffort: "medium"},
	}})
	trSubagent(t, other, "sdlc:verifier", "a1", nil, noEnv, nil)
	if got := trOne(t, other, "agent_run"); got["asked_model"] != "sonnet" {
		t.Errorf("AC-8 a role with no copy lost what it was asked for: %v", got)
	}
}

// The count is read from the file, whatever it is: the last copy holds the commit, and
// the one past it does not.
func TestAC8TheHookCountsCopiesFromTheFileAndOneWithoutIt(t *testing.T) {
	for n, stale := range map[int]string{2: "perf-3", 3: "perf-4", 9: "perf-10"} {
		t.Run("AC_8 instances "+strconv.Itoa(n), func(t *testing.T) {
			commitHeldTo(t, teamFile(`"perf":{"instances":`+strconv.Itoa(n)+`}`), stale,
				aCopy(stale, model.GateCodeReview, true), false)
		})
	}
	t.Run("AC_8 the last copy of nine", func(t *testing.T) {
		commitHeldTo(t, teamFile(`"perf":{"instances":10}`), "perf-10", func(r *model.Record) {
			needed("perf")(r)
			for i := 2; i <= 9; i++ {
				r.Staffing.Agents = append(r.Staffing.Agents, model.Staffed{Agent: "perf-" + strconv.Itoa(i), Needed: true, Reason: "for the test"})
			}
			aCopy("perf-10", model.GateCodeReview, true)(r)
		}, true)
	})
}
