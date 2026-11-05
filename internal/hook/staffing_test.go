package hook

// US-026 through the hook, which is the first barrier: who Claude Code says is
// running a command decides whether it may write the staffing.
//
//   - `sdlc staffing assign` is a person's (AC-8, decision 8): refused to every
//     agent and to the main conversation, in the session working the story and
//     in any other, whether or not a story is being worked on.
//   - `sdlc staffing set` and `amend` are the project manager's (AC-6, decision
//     10): refused to everyone else in a session.
//   - the project manager runs no other sdlc command that writes (AC-12,
//     decision 10).
//
// internal/shellpolicy has every spelling. This is the event Claude Code sends,
// with its agent_type, answered by the hook.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// staffingOn is a project with a staffing to read, in the form US-109 gives it: the
// project manager is on because nothing disables it.
const staffingOn = `{"version":1,"team":{` +
	`"models":["haiku","sonnet","opus"],"efforts":["low","medium","high","xhigh","max"],` +
	`"agents":{"verifier":{"lowest":{"model":"opus","effort":"high"}},"security":{"bring_back":["internal/hook/"]}}}}`

// staffingOff is a project whose person disabled the project manager, which is the only
// way a staffing on the record stops deciding who waits (US-109 AC-3).
const staffingOff = `{"version":1,"team":{"agents":{"project-manager":{"enabled":false}}}}`

// staffedProject is loopProject in a project with a staffing to read.
func staffedProject(t *testing.T) string {
	t.Helper()
	root := loopProject(t)
	write(t, root, ".sdlc/config.json", staffingOn)
	return root
}

// refusedUnder reports a call the hook did not refuse under the rule.
func refusedUnder(t *testing.T, r reply, rule, who, cmd string) {
	t.Helper()
	if !denied(r) {
		t.Errorf("%s ran %q", who, cmd)
		return
	}
	reason := r.HookSpecificOutput.PermissionDecisionReason
	if !strings.Contains(reason, "["+rule+"]") {
		t.Errorf("%s running %q was refused by something other than %s: %s", who, cmd, rule, reason)
	}
	if !strings.Contains(reason, "Instead:") {
		t.Errorf("the refusal of %q offers no route: %s", cmd, reason)
	}
}

var assignCommands = []string{
	`sdlc staffing assign security --model opus --effort high --reason "mine"`,
	"sdlc staffing assign architect --reason x",
	"sdlc --model opus staffing assign security --reason x",
	"sdlc staffing --effort high assign security --reason x",
	"env -u CLAUDE_CODE_SESSION_ID sdlc staffing assign security --reason x",
	"bash -c 'sdlc staffing assign security --reason x'",
	"go test ./... && sdlc staffing assign security --reason x",
}

// everyone is who Claude Code can say is running a command: the main
// conversation, each of the plugin's agents under the names it is given, and
// agents that are not the plugin's.
var everyone = []string{
	"", "sdlc:project-manager", "sdlc-project-manager", "project-manager",
	"sdlc:researcher", "sdlc:sdet", "sdlc:implementer", "sdlc:bookkeeper", "sdlc:verifier",
	"sdlc:code-reviewer", "sdlc:architect", "sdlc:security", "sdlc:red-team", "sdlc:perf",
	"sdlc:human-advocate", "general-purpose", "Explore",
}

func TestAC8TheSessionWorkingAStoryIsRefusedStaffingAssignWhoeverRunsIt(t *testing.T) {
	t.Parallel()
	off := loopProject(t)
	write(t, off, ".sdlc/config.json", staffingOff)
	for name, root := range map[string]string{"the project manager on": staffedProject(t), "the project manager disabled": off} {
		for _, cmd := range assignCommands {
			for _, agent := range everyone {
				refusedUnder(t, call(t, command(root, agent, cmd), noEnv), "assignment-is-a-human-decision",
					"with "+name+", "+who(agent), cmd)
			}
		}
	}
}

func who(agent string) string {
	if agent == "" {
		return "the main conversation"
	}
	return agent
}

func TestAC8AnotherSessionOfTheProjectIsRefusedStaffingAssignWhileAStoryIsUnderWay(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	for _, cmd := range assignCommands {
		refusedUnder(t, call(t, inSession(command(root, "", cmd), "session-b"), noEnv),
			"assignment-is-a-human-decision", "another session", cmd)
	}
}

// With nothing being worked on a person can still assign to a story that has
// been started, so a session is still refused: an assignment is not a decision
// on a story that waits, and nothing is asked about one.
func TestAC8ASessionIsRefusedStaffingAssignWithNoStoryBeingWorkedOn(t *testing.T) {
	t.Parallel()
	for _, root := range []string{staffedProject(t), loopProject(t)} {
		if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
			t.Fatal(err)
		}
		for _, cmd := range assignCommands {
			for _, agent := range []string{"", "sdlc:project-manager", "general-purpose"} {
				refusedUnder(t, call(t, inSession(command(root, agent, cmd), "session-b"), noEnv),
					"assignment-is-a-human-decision", who(agent)+", with no story being worked on", cmd)
			}
		}
		// Nor with an escalation answered, when nothing waits for anybody.
		escalated(t, root, true)
		for _, cmd := range assignCommands {
			if !denied(call(t, inSession(command(root, "", cmd), "session-b"), noEnv)) {
				t.Errorf("a session ran %q with every escalation answered", cmd)
			}
		}
	}
}

// What only names the command goes through, and reading the staffing is
// anyone's.
func TestAC8OrdinaryCommandsAboutTheStaffingAreNotRefused(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	for _, cmd := range []string{
		"sdlc staffing show",
		"sdlc staffing show --json --gate code_review",
		"sdlc status --json",
		"echo sdlc staffing assign",
		`sdlc escalate spec_unclear --message "a person may want sdlc staffing assign security --model opus"`,
		"grep -rn 'staffing assign' docs/",
	} {
		for _, agent := range []string{"", "sdlc:researcher", "sdlc:implementer"} {
			if r := call(t, command(root, agent, cmd), noEnv); denied(r) {
				t.Errorf("%s was refused %q: %s", who(agent), cmd, r.HookSpecificOutput.PermissionDecisionReason)
			}
		}
	}
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"sdlc staffing show --story A-1", "sdlc status", "echo sdlc staffing assign"} {
		if r := call(t, inSession(command(root, "", cmd), "session-b"), noEnv); denied(r) {
			t.Errorf("with no story being worked on, %q was refused: %s", cmd, r.HookSpecificOutput.PermissionDecisionReason)
		}
	}
}

var recordCommands = []string{
	"sdlc staffing set <<'SDLC_DOCUMENT'\n{\"agents\":[]}\nSDLC_DOCUMENT",
	"sdlc staffing set --file /tmp/staffing.json",
	`sdlc staffing amend perf --left-out --reason "nothing to measure"`,
	"sdlc staffing amend researcher --model opus --reason x",
	"sdlc staffing --reason x amend perf --left-out",
	"bash -c 'sdlc staffing set --file s.json'",
	"sdlc staffing show && sdlc staffing amend perf --left-out --reason x",
}

// AC-6: the session running the runbook does not write or amend the staffing
// itself, and neither does any agent the staffing is about.
func TestAC6OnlyTheProjectManagerRecordsOrAmendsTheStaffing(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	for _, cmd := range recordCommands {
		for _, agent := range everyone {
			r := call(t, command(root, agent, cmd), noEnv)
			if strings.Contains(agent, "project-manager") {
				if denied(r) {
					t.Errorf("%s could not run %q: %s", agent, cmd, r.HookSpecificOutput.PermissionDecisionReason)
				}
				continue
			}
			refusedUnder(t, r, "staffing-is-recorded-by-the-project-manager", who(agent), cmd)
			if reason := r.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "sdlc:project-manager") {
				t.Errorf("the refusal does not send %s to the project manager: %s", who(agent), reason)
			}
		}
	}
}

// AC-12: the project manager never approves, unfreezes, acknowledges, or
// records a gate or a review, and decision 10 has the hook refuse it every
// other sdlc command that writes.
func TestAC12TheProjectManagerRunsNoOtherSdlcCommandThatWrites(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	for cmd, rules := range map[string][]string{
		"sdlc gate analysis pass --security-sensitive=false": {"project-manager-only-staffs"},
		"sdlc gate code_review pass --note approved":         {"project-manager-only-staffs"},
		"sdlc review add code_review code-reviewer approve":  {"project-manager-only-staffs", "review-is-recorded-by-its-reviewer"},
		"sdlc artifact write analysis":                       {"project-manager-only-staffs"},
		"sdlc escalate spec_unclear --message x":             {"project-manager-only-staffs"},
		"sdlc freeze":                                        {"project-manager-only-staffs"},
		"sdlc start":                                         {"project-manager-only-staffs"},
		"sdlc cost add --usd 1":                              {"project-manager-only-staffs"},
		"sdlc init --force":                                  {"project-manager-only-staffs"},
		"env sdlc gate analysis pass":                        {"project-manager-only-staffs"},
		"bash -c 'sdlc gate analysis pass'":                  {"project-manager-only-staffs"},
		"sdlc staffing show && sdlc gate analysis pass":      {"project-manager-only-staffs"},
		"sdlc approve A-1":                                   {"project-manager-only-staffs", "approval-is-a-human-decision"},
		"sdlc unfreeze --reason x":                           {"project-manager-only-staffs", "unfreeze-is-a-human-decision"},
		"sdlc ack --through HEAD":                            {"project-manager-only-staffs", "acknowledgement-is-a-human-decision"},
		"sdlc release A-1 --reason x":                        {"project-manager-only-staffs", "release-is-a-human-decision"},
		"sdlc stop":                                          {"project-manager-only-staffs", "stop-is-a-human-decision"},
	} {
		for _, agent := range []string{"sdlc:project-manager", "sdlc-project-manager", "project-manager"} {
			r := call(t, command(root, agent, cmd), noEnv)
			if !denied(r) {
				t.Errorf("%s ran %q", agent, cmd)
				continue
			}
			reason := r.HookSpecificOutput.PermissionDecisionReason
			under := false
			for _, rule := range rules {
				under = under || strings.Contains(reason, "["+rule+"]")
			}
			if !under {
				t.Errorf("%s running %q was refused under none of %q: %s", agent, cmd, rules, reason)
			}
		}
	}
	// "Mark a story ready": the backlog is nobody's to edit while a story is
	// worked, and the project manager has no allowance there.
	for _, cmd := range []string{
		`sed -i '' 's/"todo"/"ready"/' user_stories.json`,
		"echo '{}' > user_stories.json",
		"echo x > .sdlc/stories/A-1/gate-record.json",
	} {
		if !denied(call(t, command(root, "sdlc:project-manager", cmd), noEnv)) {
			t.Errorf("the project manager ran %q", cmd)
		}
	}
	for _, path := range []string{"user_stories.json", "internal/invoice.go", "internal/invoice_test.go", ".sdlc/stories/A-1/gate-record.json"} {
		for _, tool := range []string{"Write", "Edit"} {
			if !denied(call(t, event(root, tool, "sdlc:project-manager", filepath.Join(root, filepath.FromSlash(path))), noEnv)) {
				t.Errorf("the project manager's %s of %s went through", tool, path)
			}
		}
	}
}

func TestAC12TheProjectManagerReadsAndStaffsAndNothingStopsIt(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	for _, cmd := range []string{
		"sdlc staffing show --json",
		"sdlc status --json",
		"sdlc review list",
		"sdlc story list",
		"git log --oneline -5",
		"git diff --stat",
		"grep -rn security internal/",
	} {
		if r := call(t, command(root, "sdlc:project-manager", cmd), noEnv); denied(r) {
			t.Errorf("the project manager was refused %q: %s", cmd, r.HookSpecificOutput.PermissionDecisionReason)
		}
	}
	for _, tool := range []string{"Read", "Grep", "Glob"} {
		if denied(call(t, event(root, tool, "sdlc:project-manager", filepath.Join(root, "user_stories.json")), noEnv)) {
			t.Errorf("the project manager's %s was refused", tool)
		}
	}
}

// AC-3 at the hook: `git commit` is not refused for want of a review from a
// reviewer the staffing leaves out. Such a reviewer may have reported before it
// was left out, and its review of a tree since changed is then on the record
// with nobody to bring it up to date. The rows beside it are every way that
// must not become "no review is held to": a reviewer that is kept, one a
// changed path brought back when code review passed (AC-7), security on a
// story recorded as sensitive (AC-4), and a project with the project manager
// disabled (US-109 AC-3), where a staffing in the record says nothing.
func TestAC3ACommitIsNotHeldToTheStaleReviewOfAReviewerTheStaffingLeavesOut(t *testing.T) {
	for _, tc := range []struct {
		name string
		// config is the project's; stale is whose review is of another tree;
		// change is what the row does to a record that leaves perf, security
		// and the human advocate out of a story that is not sensitive.
		config string
		stale  string
		change func(r *model.Record)
		// refused is whether the commit is, and then names stale.
		refused bool
	}{
		{"AC_3 a reviewer left out", staffingOn, "perf", nil, false},
		{"AC_3 another reviewer left out", staffingOn, "human-advocate", nil, false},
		{"AC_3 a reviewer the staffing keeps", staffingOn, "code-reviewer", nil, true},
		{"AC_3 the verifier, which is never left out", staffingOn, "verifier", nil, true},
		{"AC_7 a reviewer left out that a changed path brought back", staffingOn, "security",
			func(r *model.Record) {
				r.Staffing.BroughtBack = map[string]string{"security": "internal/hook/extra.go"}
			}, true},
		{"AC_4 security left out of a story recorded as sensitive", staffingOn, "security",
			func(r *model.Record) { r.SetSecuritySensitive(true) }, true},
		{"AC_3 security left out of a story that is not sensitive", staffingOn, "security", nil, false},
		{"AC_10 a staffing in the record of a project with the project manager disabled", staffingOff, "perf", nil, true},
		{"AC_3 no team section, where the project manager is on by default", `{"version":1}`, "perf", nil, false},
		{"AC_10 no staffing recorded", staffingOn, "perf", func(r *model.Record) { r.Staffing = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			commitHeldTo(t, tc.config, tc.stale, tc.change, tc.refused)
		})
	}
}

// commitHeldTo asks the hook about a commit in a project with the configuration given,
// on a record that passed every gate before it. The staffing on the record leaves perf,
// security and the human advocate out of a story that is not sensitive, every reviewer of
// Gates 6 and 7 approved this tree, and stale's review is of another. refused is whether
// the commit is, and then it names stale.
func commitHeldTo(t *testing.T, cfgText, stale string, change func(r *model.Record), refused bool) {
	t.Helper()
	root := loopProject(t)
	write(t, root, ".sdlc/config.json", cfgText)
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
	record.SetSecuritySensitive(false)
	record.Staffing = &model.Staffing{At: model.Timestamp(at)}
	for _, agent := range model.Agents {
		record.Staffing.Agents = append(record.Staffing.Agents, model.Staffed{
			Agent: agent, Needed: agent != "perf" && agent != "security" && agent != "human-advocate", Reason: "for the test",
		})
	}
	if change != nil {
		change(record)
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

	r := call(t, command(root, "", "git commit -m done"), noEnv)
	reason := r.HookSpecificOutput.PermissionDecisionReason
	switch {
	case !refused && denied(r):
		t.Errorf("the commit was held to the stale review of %s, which nothing waits for: %s", stale, reason)
	case refused && !denied(r):
		t.Errorf("the commit went through with a stale review from %s, which the story still needs", stale)
	case refused && !strings.Contains(reason, stale):
		t.Errorf("the refusal does not say whose review is stale (%s): %s", stale, reason)
	}
}

// T7: every value here is refused to a command, and the hook goes on answering: it does not
// stop, it does not turn the refusal into a blanket denial, and it does not let a person's
// command through. Each value goes through the hook, because the validation that cannot
// panic is what stands between a value in the file and a crash on every tool call of a
// session. What the hook reads from a file it was able to read, and from one it was not, is
// TestAC13TheHookReadsTheProjectsOwnSettingsFromARefusedFile in team_test.go: this row holds
// with `config.Load` at every site, and that one does not (red team V2).
func TestAC13TheHookStillAnswersWhenTheFileIsRefused(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat(`{"x":`, 5000) + `1` + strings.Repeat(`}`, 5000)
	huge := `["` + strings.Repeat(`a","`, 200000) + `a"]`
	for name, rest := range map[string]string{
		"AC_12 a staffing section":              `"staffing":{"enabled":true,"models":[],"efforts":["low"]}`,
		"AC_12 a staffing section that is off":  `"staffing":{"enabled":false}`,
		"AC_12 a staffing of null":              `"staffing":null`,
		"AC_12 gate7_advisory true":             `"reviews":{"gate7_advisory":true}`,
		"AC_12 gate7_advisory true then null":   `"reviews":{"gate7_advisory":true,"gate7_advisory":null}`,
		"AC_13 team of the wrong type":          `"team":"yes"`,
		"AC_13 team null":                       `"team":null`,
		"AC_13 retries of the wrong type":       `"team":{"retries":"two"}`,
		"AC_13 no models":                       `"team":{"models":[]}`,
		"AC_13 models null":                     `"team":{"models":null}`,
		"AC_13 agents of the wrong type":        `"team":{"agents":[1,2]}`,
		"AC_13 an agent of null":                `"team":{"agents":{"security":null}}`,
		"AC_13 an agent of the wrong type":      `"team":{"agents":{"security":"off"}}`,
		"AC_13 a key that is not an agent":      `"team":{"agents":{"designer":{"enabled":false}}}`,
		"AC_13 a lowest of the wrong type":      `"team":{"agents":{"security":{"lowest":7}}}`,
		"AC_13 instances of a huge number":      `"team":{"agents":{"security":{"instances":1e300}}}`,
		"AC_8 instances past the ceiling":       `"team":{"agents":{"security":{"instances":17}}}`,
		"AC_8 instances far past the ceiling":   `"team":{"agents":{"security":{"instances":2000000000}}}`,
		"AC_9 retries past the ceiling":         `"team":{"retries":21}`,
		"AC_9 retries at the top of an int":     `"team":{"retries":9223372036854775807}`,
		"AC_13 a huge list":                     `"team":{"models":` + huge + `}`,
		"AC_13 a huge bring_back":               `"team":{"agents":{"code-reviewer":{"bring_back":` + huge + `}}}`,
		"AC_13 deep nesting in a map value":     `"team":{"agents":{"security":{"lowest":` + deep + `}}}`,
		"AC_13 deep nesting under a key":        `"team":{"agents":` + deep + `}`,
		"AC_13 a misspelt key with deep values": `"team":{"agents":{"security":{"lowset":` + deep + `}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := loopProject(t)
			write(t, root, ".sdlc/config.json", `{"version":1,`+rest+`}`)
			refusedUnder(t, call(t, command(root, "", "sdlc staffing assign security --reason x"), noEnv),
				"assignment-is-a-human-decision", "the main conversation", "sdlc staffing assign")
			if r := call(t, command(root, "", "go test ./... -count=1"), noEnv); denied(r) {
				t.Errorf("an ordinary command was not let through: %+v", r)
			}
			if r := call(t, command(root, "", "sdlc doctor"), noEnv); denied(r) {
				t.Errorf("sdlc doctor, which names the fix, was refused: %+v", r)
			}
			if !denied(call(t, command(root, "", "git commit -m early"), noEnv)) {
				t.Error("the commit gate stopped standing in front of a commit")
			}
		})
	}
}
