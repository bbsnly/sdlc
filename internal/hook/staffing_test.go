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

const staffingOn = `{"version":1,"staffing":{"enabled":true,` +
	`"models":["haiku","sonnet","opus"],"efforts":["low","medium","high","xhigh","max"],` +
	`"lowest":{"verifier":{"model":"opus","effort":"high"}},"bring_back":{"security":["internal/hook/"]}}}`

// staffedProject is loopProject in a project that turns the staffing on.
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
	for name, root := range map[string]string{"the staffing on": staffedProject(t), "the staffing off": loopProject(t)} {
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
		"sdlc parallel A-1":                                  {"project-manager-only-staffs", "parallel-is-a-human-decision"},
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
// story recorded as sensitive (AC-4), and a project with the staffing off
// (AC-10), where a staffing in the record says nothing.
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
		{"AC_10 a staffing in the record of a project with the staffing off", `{"version":1}`, "perf", nil, true},
		{"AC_10 no staffing recorded", staffingOn, "perf", func(r *model.Record) { r.Staffing = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := loopProject(t)
			write(t, root, ".sdlc/config.json", tc.config)
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
			if tc.change != nil {
				tc.change(record)
			}
			for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
				for _, r := range model.ReviewersFor(gate) {
					subject := tree
					if r.Role == tc.stale {
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
			case !tc.refused && denied(r):
				t.Errorf("the commit was held to the stale review of %s, which the staffing leaves out: %s", tc.stale, reason)
			case tc.refused && !denied(r):
				t.Errorf("the commit went through with a stale review from %s, which the story still needs", tc.stale)
			case tc.refused && !strings.Contains(reason, tc.stale):
				t.Errorf("the refusal does not say whose review is stale (%s): %s", tc.stale, reason)
			}
		})
	}
}

// T6: a staffing section that is on and cannot be used does not load. The hook
// then works from the defaults, in which the staffing is off, and answers as
// it does everywhere: it does not stop answering, and it does not let a
// person's command through.
func TestAC5TheHookStillAnswersWhenTheStaffingSectionCannotBeUsed(t *testing.T) {
	for name, section := range map[string]string{
		"no models":            `{"enabled":true,"models":[],"efforts":["low"]}`,
		"a lowest not allowed": `{"enabled":true,"models":["opus"],"efforts":["low"],"lowest":{"verifier":{"model":"fable"}}}`,
		"the wrong type":       `{"enabled":"yes"}`,
		"an agent not shipped": `{"enabled":true,"models":["opus"],"efforts":["low"],"lowest":{"designer":{"model":"opus"}}}`,
	} {
		root := loopProject(t)
		write(t, root, ".sdlc/config.json", `{"version":1,"staffing":`+section+`}`)
		refusedUnder(t, call(t, command(root, "", "sdlc staffing assign security --reason x"), noEnv),
			"assignment-is-a-human-decision", "with "+name+", the main conversation", "sdlc staffing assign")
		if r := call(t, command(root, "", "go test ./... -count=1"), noEnv); denied(r) {
			t.Errorf("with %s, an ordinary command was not let through: %+v", name, r)
		}
		if !denied(call(t, command(root, "", "git commit -m early"), noEnv)) {
			t.Errorf("with %s, the commit gate stopped standing in front of a commit", name)
		}
	}
}
