package shellpolicy

// US-026: who may run which `sdlc staffing` command, and what the project
// manager may run at all.
//
//	assignment-is-a-human-decision               `sdlc staffing assign`, from any
//	                                             session (AC-8, decision 8). Asked
//	                                             in both entry points: Inspect,
//	                                             for the session working a story,
//	                                             and HumanDecisions, for any other
//	                                             session of the project.
//	staffing-is-recorded-by-the-project-manager  `sdlc staffing set` and `amend`
//	                                             from anyone in a session but the
//	                                             project manager (AC-6, decision
//	                                             10).
//	project-manager-only-staffs                  any sdlc command from the project
//	                                             manager but `staffing set`,
//	                                             `amend`, `show` and the commands
//	                                             that only read (AC-12, decision
//	                                             10).
//
// The word after `staffing` is read as a list of what is known: `show` is
// anyone's, `set` and `amend` the project manager's, and anything else, a word
// the reading cannot make out included, is refused. So is what the project
// manager may run: a list of what it may, not of what it may not.
//
// The contract asks that a change here comes with the evasions it was tried
// against. They are the lists below, taken from T4 and T12 of the story's
// threats and from the spellings the other person-only commands are held to.

import (
	"slices"
	"strings"
	"testing"
)

const (
	assignRule   = "assignment-is-a-human-decision"
	recorderRule = "staffing-is-recorded-by-the-project-manager"
	managerRule  = "project-manager-only-staffs"
)

// staffingRules is every rule a `sdlc staffing` command can be refused under.
// A spelling the reading cannot make out is refused under whichever of them
// the implementation reaches first; what matters is that it is refused.
var staffingRules = []string{assignRule, recorderRule, managerRule}

// assignSpellings read as `sdlc staffing assign` through the readings the
// other person-only commands already have.
var assignSpellings = []string{
	// As a person would type it.
	`sdlc staffing assign security --model opus --effort high --reason "mine"`,
	"sdlc staffing assign security --reason x",
	"sdlc staffing assign",
	"sdlc staffing assign security --story US-001 --reason x",
	// However sdlc is reached.
	"/usr/local/bin/sdlc staffing assign security --reason x",
	"./bin/sdlc staffing assign security --reason x",
	`C:\Users\dev\AppData\Local\sdlc\bin\sdlc.exe staffing assign security --reason x`,
	"SDLC staffing assign security --reason x",
	"npx @bbsnly/sdlc staffing assign security --reason x",
	"npx sdlc@0.3.0 staffing assign security --reason x",
	"go run ./cmd/sdlc staffing assign security --reason x",
	"go run -mod=mod ./cmd/sdlc staffing assign security --reason x",
	// A flag before the verb, or between `staffing` and `assign`. --model and
	// --effort take a value, and a reading that did not know it would take
	// the value for the subcommand.
	"sdlc --json staffing assign security --reason x",
	"sdlc --model opus staffing assign security --reason x",
	"sdlc --effort high staffing assign security --reason x",
	"sdlc --reason x staffing assign security",
	"sdlc --story US-001 staffing assign security --reason x",
	"sdlc staffing --model opus assign security --reason x",
	"sdlc staffing --effort high assign security --reason x",
	"sdlc staffing --reason x assign security",
	"sdlc staffing --story US-001 assign security --reason x",
	"sdlc staffing --json assign security --reason x",
	"sdlc --model opus --effort high --reason x staffing --story US-001 assign security",
	// After the word that ends the flags.
	"sdlc -- staffing assign security",
	"sdlc staffing -- assign security",
	// --flag=value.
	"sdlc --model=opus staffing assign security --reason=x",
	"sdlc staffing --model=opus assign security --reason=x",
	"sdlc staffing --effort=high --reason=x assign security",
	// Behind a wrapper.
	"env sdlc staffing assign security --reason x",
	"env FOO=1 sdlc staffing assign security --reason x",
	"env -S 'sdlc staffing assign security --reason x'",
	"env -u CLAUDE_CODE_SESSION_ID sdlc staffing assign security --reason x",
	"env --unset=CLAUDE_CODE_SESSION_ID sdlc staffing assign security --reason x",
	"env -i PATH=/usr/bin sdlc staffing assign security --reason x",
	"flock /tmp/x sdlc staffing assign security --reason x",
	"script -qc 'sdlc staffing assign security --reason x' /dev/null",
	"command sdlc staffing assign security --reason x",
	"nohup sdlc staffing assign security --reason x &",
	"time sdlc staffing assign security --reason x",
	"xargs sdlc staffing assign",
	"echo security | xargs sdlc staffing assign --reason x",
	// Through another shell, or evaluated.
	"bash -c 'sdlc staffing assign security --reason x'",
	`bash -c "sdlc staffing assign security --reason x"`,
	"sh -c 'cd x && sdlc staffing assign security --reason x'",
	"eval 'sdlc staffing assign security --reason x'",
	`eval "sdlc staffing assign security --reason x"`,
	"bash <<EOF\nsdlc staffing assign security --reason x\nEOF",
	// In a list, a pipeline, or a subshell.
	"true && sdlc staffing assign security --reason x",
	"go test ./... ; sdlc staffing assign security --reason x",
	"go test ./... || sdlc staffing assign security --reason x",
	"(sdlc staffing assign security --reason x)",
	"cd /somewhere && (sdlc staffing assign security --reason x)",
	"echo x | sdlc staffing assign security --reason x",
	"sdlc staffing show; sdlc staffing assign security --reason x",
	"sdlc staffing show && sdlc staffing assign security --reason x",
	// Quoted.
	"sdlc staffing 'assign' security --reason x",
	`sdlc staffing "assign" security --reason x`,
	`sdlc "staffing" "assign" security --reason x`,
	"$'sdlc' staffing assign security --reason x",
	"sdlc staffing $'assign' security --reason x",
	"sdlc staffing\tassign\tsecurity --reason x",
	// Aimed at another checkout of the repository.
	"cd ../other-checkout && sdlc staffing assign security --reason x",
	"cd .sdlc/worktrees/US-001 && sdlc staffing assign security --reason x",
	"env -C ../other-checkout sdlc staffing assign security --reason x",
}

// unreadSpellings are commands whose word after `staffing` is not one of the
// three the reading knows, because it is another word or because the shell
// makes it at run time. Each could be an assignment, so each is refused.
var unreadSpellings = []string{
	"sdlc staffing $verb security --reason x",
	"sdlc staffing ${VERB} security --reason x",
	`sdlc staffing "$(printf assign)" security --reason x`,
	"sdlc staffing `echo assign` security --reason x",
	"sdlc staffing ass''ign security --reason x",
	`sdlc staffing as"sig"n security --reason x`,
	`sdlc staffing as\sign security --reason x`,
	"sdlc staffing {assign,show} security --reason x",
	"sdlc staffing a{ssign,} security --reason x",
	"sdlc staffing ass* security --reason x",
	"sdlc staffing ?ssign security --reason x",
	"sdlc staffing Assign security --reason x",
	"sdlc staffing ASSIGN security --reason x",
	"sdlc staffing assign.exe security --reason x",
	"sdlc staffing assig security --reason x",
	"sdlc staffing give security --model opus --reason x",
	"sdlc staffing remove security",
}

func TestAC8AModelSessionWorkingAStoryCannotRunStaffingAssign(t *testing.T) {
	for _, command := range assignSpellings {
		refused(t, command, ready, assignRule)
		refused(t, command, notReady, assignRule)
	}
}

// Whoever the hook says is running it: the person's command is no agent's,
// the project manager's least of all.
func TestAC8NoAgentRunsStaffingAssign(t *testing.T) {
	for _, agent := range []string{
		"", "project-manager", "researcher", "sdet", "implementer", "bookkeeper", "verifier",
		"code-reviewer", "architect", "security", "general-purpose",
	} {
		for _, command := range assignSpellings[:6] {
			f, ok := Inspect(command, State{CommitReady: true, Agent: agent})
			if !ok {
				t.Errorf("%q ran %q", agent, command)
				continue
			}
			if f.Rule != assignRule {
				t.Errorf("%q running %q was refused under %s, want %s", agent, command, f.Rule, assignRule)
			}
		}
	}
}

// With no story being worked on, only HumanDecisions asks. An assignment is
// refused there whatever the project holds, as an acknowledgement is, and the
// answer to "does a story wait" is never asked for it: a person can assign to
// a story at any point after it is started.
func TestAC8AModelSessionWorkingNoStoryCannotRunStaffingAssignEither(t *testing.T) {
	for _, command := range assignSpellings {
		for _, waits := range []bool{true, false} {
			asked := false
			f, ok := HumanDecisions(command, false, func(string) bool { asked = true; return waits })
			if !ok || f.Rule != assignRule {
				t.Errorf("HumanDecisions(%q, a story waits = %v) = %q, %v; want %s", command, waits, f.Rule, ok, assignRule)
			}
			if asked {
				t.Errorf("HumanDecisions(%q) asked whether a story waits: an assignment is refused either way", command)
			}
		}
	}
}

func TestAC8StaffingAssignIsRefusedInPowerShellToo(t *testing.T) {
	for _, command := range []string{
		"sdlc staffing assign security --reason x",
		"s`dlc staffing assign security --reason x",
		"& sdlc staffing assign security --reason x",
		"& 'sdlc' staffing assign security --reason x",
		"sdlc.exe staffing assign security --reason x",
		"sdlc staffing assign security --reason x; Write-Output done",
		"sdlc staffing --model opus assign security --reason x",
		"$env:FOO='1'; sdlc staffing assign security --reason x",
	} {
		if f, ok := HumanDecisions(command, true, aStoryWaits); !ok || f.Rule != assignRule {
			t.Errorf("HumanDecisions(%q, powershell) = %q, %v; want %s", command, f.Rule, ok, assignRule)
		}
		if f, ok := Inspect(command, State{CommitReady: true, PowerShell: true}); !ok || f.Rule != assignRule {
			t.Errorf("Inspect(%q, powershell) = %q, %v; want %s", command, f.Rule, ok, assignRule)
		}
	}
	// PowerShell's own ways of making the word at run time are words the
	// reading does not know.
	for _, command := range []string{
		"sdlc staffing ass`ign security --reason x",
		"sdlc staffing $verb security --reason x",
	} {
		if f, ok := HumanDecisions(command, true, aStoryWaits); !ok {
			t.Errorf("HumanDecisions(%q, powershell) let it through (%q)", command, f.Rule)
		}
		if f, ok := Inspect(command, State{CommitReady: true, PowerShell: true}); !ok {
			t.Errorf("Inspect(%q, powershell) let it through (%q)", command, f.Rule)
		}
	}
}

// A word after `staffing` the reading does not know is refused to everyone,
// the project manager included, in both entry points.
func TestAC8AStaffingCommandTheReadingCannotMakeOutIsRefused(t *testing.T) {
	for _, command := range unreadSpellings {
		for _, agent := range []string{"", "project-manager", "implementer"} {
			f, ok := Inspect(command, State{CommitReady: true, Agent: agent})
			if !ok {
				t.Errorf("Inspect(%q) from %q let it through", command, agent)
				continue
			}
			if !slices.Contains(staffingRules, f.Rule) {
				t.Errorf("Inspect(%q) from %q refused it under %s, which is none of the staffing's rules", command, agent, f.Rule)
			}
			if f.Route == "" || f.Reason == "" {
				t.Errorf("Inspect(%q): the refusal says nothing to do next: %+v", command, f)
			}
		}
		if _, ok := HumanDecisions(command, false, func(string) bool { return false }); !ok {
			t.Errorf("HumanDecisions(%q) let it through with no story being worked on", command)
		}
	}
}

func TestAC8TheRefusalOfAnAssignmentSaysWhatToDoNext(t *testing.T) {
	f := refused(t, "sdlc staffing assign security --model opus --reason x", ready, assignRule)
	for _, word := range []string{"person", "terminal"} {
		if !strings.Contains(strings.ToLower(f.Route), word) {
			t.Errorf("the route does not say %q: %s", word, f.Route)
		}
	}
	if f.Message() == "" {
		t.Error("no message for the assistant")
	}
}

// T4: sdlc is also run from its npm package, and each package manager has its
// own word for running one. A reading that knew only `npx` would let the
// person's command through behind the others.
func TestAC8StaffingAssignIsRefusedBehindEveryPackageRunner(t *testing.T) {
	for _, command := range []string{
		"npm exec sdlc staffing assign security --reason x",
		"npm exec -- sdlc staffing assign security --reason x",
		"npm x sdlc staffing assign security --reason x",
		"pnpm dlx sdlc staffing assign security --reason x",
		"pnpm exec sdlc staffing assign security --reason x",
		"yarn dlx @bbsnly/sdlc staffing assign security --reason x",
		"bunx sdlc staffing assign security --reason x",
		"pnpx sdlc staffing assign security --reason x",
		"npx --yes sdlc staffing assign security --reason x",
	} {
		refused(t, command, ready, assignRule)
		refused(t, command, notReady, assignRule)
		if f, ok := Inspect(command, State{CommitReady: true, Agent: "project-manager"}); !ok || f.Rule != assignRule {
			t.Errorf("Inspect(%q) from the project manager = %q, %v; want %s", command, f.Rule, ok, assignRule)
		}
		if f, ok := HumanDecisions(command, false, func(string) bool { return false }); !ok || f.Rule != assignRule {
			t.Errorf("HumanDecisions(%q) = %q, %v; want %s", command, f.Rule, ok, assignRule)
		}
	}
}

// The same programs do other things with the name: building sdlc, testing its
// package, running something else. None is a run of sdlc, and the staffing's
// rules refuse none of them to the session, or to the project manager, which
// may run only what reads (AC-12).
func TestAC12BuildingOrTestingSdlcIsNotRunningIt(t *testing.T) {
	for _, command := range []string{
		"go build ./cmd/sdlc",
		"go build -o bin/sdlc ./cmd/sdlc",
		"go test ./cmd/sdlc -run Staffing",
		"go vet ./cmd/sdlc",
		"go list ./cmd/sdlc",
		"npx prettier --check docs",
		"npm test",
		"find . -name sdlc",
		"ls bin/sdlc",
	} {
		allowed(t, command, ready)
		allowed(t, command, State{CommitWhy: "analysis has not passed", Agent: "project-manager"})
		if f, ok := HumanDecisions(command, false, aStoryWaits); ok {
			t.Errorf("HumanDecisions(%q) refused it under %s", command, f.Rule)
		}
	}
}

// Naming the command is not running it, and reading the staffing is anyone's.
func TestAC8OnlyRunningStaffingAssignIsRefused(t *testing.T) {
	for _, command := range []string{
		"echo sdlc staffing assign",
		`git commit -m "docs: a person runs sdlc staffing assign security"`,
		`sdlc escalate spec_unclear --message "a person may want sdlc staffing assign security --model opus"`,
		`sdlc gate analysis pass --note "the person ran sdlc staffing assign first"`,
		"sdlc staffing show",
		"sdlc staffing show --json",
		"sdlc staffing show --gate code_review --story US-001",
		"sdlc --json staffing show",
		"sdlc staffing --gate code_review show",
		"sdlc status --json",
		"sdlc review list",
		"cat docs/commands.md",
		"grep -rn 'staffing assign' docs/",
		"go test ./internal/cli -run Staffing",
		"go test ./... # then sdlc staffing assign security",
	} {
		allowed(t, command, ready)
		if f, ok := HumanDecisions(command, false, aStoryWaits); ok {
			t.Errorf("HumanDecisions(%q) refused it under %s", command, f.Rule)
		}
	}
}

// Recording and amending the staffing are the project manager's, not a
// person's: where no story is held they are no decision HumanDecisions keeps,
// which is also what lets the unattended allowlist name them.
func TestAC6RecordingTheStaffingIsNotAHumanDecision(t *testing.T) {
	for _, command := range []string{
		"sdlc staffing set",
		"sdlc staffing set --file staffing.json",
		`sdlc staffing amend perf --left-out --reason "nothing to measure"`,
		"sdlc staffing amend perf --needed --model sonnet --effort low --reason x",
	} {
		for _, waits := range []bool{true, false} {
			if f, ok := HumanDecisions(command, false, func(string) bool { return waits }); ok {
				t.Errorf("HumanDecisions(%q) refused it under %s", command, f.Rule)
			}
		}
	}
}

// The flags the staffing commands take with a value are ones the subcommand
// search knows, or `sdlc --model opus staffing assign` reads as the command
// `opus`.
func TestAC8TheModelAndEffortFlagsAreOnesThatTakeAValue(t *testing.T) {
	for _, flag := range []string{"--model", "--effort", "--reason", "--file", "--story", "--gate"} {
		if !ValueFlags[flag] {
			t.Errorf("ValueFlags has no %s, so a subcommand after it is read as its value", flag)
		}
	}
	for _, flag := range []string{"--needed", "--left-out", "--json"} {
		if ValueFlags[flag] {
			t.Errorf("ValueFlags has %s, which takes no value: the word after it would be skipped", flag)
		}
	}
}

// ------------------------------------------------- who records the staffing

// recordSpellings write the staffing: `set` and `amend`, however they are
// reached.
var recordSpellings = []string{
	"sdlc staffing set",
	"sdlc staffing set --file /tmp/staffing.json",
	"sdlc staffing set --story US-001",
	"sdlc staffing set <<'SDLC_DOCUMENT'\n{\"agents\":[]}\nSDLC_DOCUMENT",
	"cat staffing.json | sdlc staffing set",
	"sdlc staffing set < staffing.json",
	`sdlc staffing amend perf --left-out --reason "nothing to measure"`,
	"sdlc staffing amend researcher --model opus --reason x",
	"sdlc staffing amend perf --needed --model sonnet --effort low --reason x",
	"sdlc --json staffing set",
	"sdlc --story US-001 staffing amend perf --left-out --reason x",
	"sdlc staffing --reason x amend perf --left-out",
	"sdlc staffing --model opus amend researcher --reason x",
	"sdlc staffing --file staffing.json set",
	"/usr/local/bin/sdlc staffing set",
	"npx @bbsnly/sdlc staffing set",
	"go run ./cmd/sdlc staffing amend perf --left-out --reason x",
	"env sdlc staffing set",
	"bash -c 'sdlc staffing set --file s.json'",
	"eval 'sdlc staffing amend perf --left-out --reason x'",
	"(sdlc staffing set)",
	"sdlc staffing show && sdlc staffing set",
	"sdlc status; sdlc staffing amend perf --left-out --reason x",
	"sdlc staffing 'set'",
	`sdlc staffing "amend" perf --left-out --reason x`,
}

func TestAC6OnlyTheProjectManagerRecordsOrAmendsTheStaffingInASession(t *testing.T) {
	for _, command := range recordSpellings {
		for _, agent := range []string{
			"", "researcher", "sdet", "implementer", "bookkeeper", "verifier", "code-reviewer",
			"architect", "security", "red-team", "perf", "human-advocate", "general-purpose", "Explore",
		} {
			for _, s := range []State{{CommitReady: true, Agent: agent}, {CommitWhy: "code_review has not passed", Agent: agent}} {
				f, ok := Inspect(command, s)
				if !ok {
					t.Errorf("%q ran %q", agent, command)
					continue
				}
				if f.Rule != recorderRule {
					t.Errorf("%q running %q was refused under %s, want %s", agent, command, f.Rule, recorderRule)
				}
				if !strings.Contains(f.Route, "sdlc:project-manager") {
					t.Errorf("the route does not send %q to the project manager: %s", agent, f.Route)
				}
			}
		}
		// The project manager's own.
		allowed(t, command, State{CommitReady: true, Agent: "project-manager"})
		allowed(t, command, State{CommitWhy: "analysis has not passed", Agent: "project-manager"})
	}
}

func TestAC6TheRefusalNamesWhoTriedToRecordTheStaffing(t *testing.T) {
	f := refused(t, "sdlc staffing set", State{Agent: "implementer"}, recorderRule)
	if !strings.Contains(f.Reason, "implementer") {
		t.Errorf("the reason does not say who tried: %s", f.Reason)
	}
	f = refused(t, "sdlc staffing set", State{}, recorderRule)
	if !strings.Contains(f.Reason, "main conversation") {
		t.Errorf("the reason does not say the main conversation tried: %s", f.Reason)
	}
}

func TestAC6SettingAndAmendingInPowerShellIsTheProjectManagersToo(t *testing.T) {
	for _, command := range []string{
		"sdlc staffing set --file staffing.json",
		"Get-Content staffing.json | sdlc staffing set",
		"& sdlc staffing amend perf --left-out --reason x",
	} {
		if f, ok := Inspect(command, State{CommitReady: true, PowerShell: true}); !ok || f.Rule != recorderRule {
			t.Errorf("Inspect(%q, powershell, main conversation) = %q, %v; want %s", command, f.Rule, ok, recorderRule)
		}
		if f, ok := Inspect(command, State{CommitReady: true, PowerShell: true, Agent: "project-manager"}); ok {
			t.Errorf("Inspect(%q, powershell, project manager) refused it under %s", command, f.Rule)
		}
	}
}

// ------------------------------------------- what the project manager may run

// managerRefused is every sdlc command that writes, other than the two that
// write the staffing. personRules are the ones refused to every session
// already, which keep the rule they were refused under.
var managerRefused = []string{
	"sdlc gate analysis pass",
	"sdlc gate analysis pass --security-sensitive=false",
	"sdlc gate code_review pass --note approved",
	"sdlc gate design_review fail --note rework",
	"sdlc review add code_review code-reviewer approve",
	"sdlc review add design_review security approve --note fine",
	"sdlc artifact write analysis",
	"sdlc artifact write plan --file PLAN.md",
	"sdlc escalate spec_unclear --message x",
	"sdlc freeze",
	"sdlc start",
	"sdlc start US-002",
	"sdlc cost add --usd 1",
	"sdlc init",
	"sdlc init --force",
	// A command this version does not have is not one it may run either.
	"sdlc frobnicate",
	"sdlc story add US-009",
	// However it is reached.
	"/usr/local/bin/sdlc gate analysis pass",
	"npx @bbsnly/sdlc gate analysis pass",
	"go run ./cmd/sdlc gate analysis pass",
	"env sdlc gate analysis pass",
	"env -u CLAUDE_CODE_SESSION_ID sdlc gate analysis pass",
	"bash -c 'sdlc gate analysis pass'",
	"eval 'sdlc review add code_review code-reviewer approve'",
	"(sdlc gate analysis pass)",
	"sdlc staffing show && sdlc gate analysis pass",
	"sdlc status; sdlc escalate spec_unclear --message x",
	"sdlc --note x gate analysis pass",
	"sdlc --json gate analysis pass",
	"sdlc --story US-001 gate analysis pass",
	"sdlc 'gate' analysis pass",
	"$'sdlc' gate analysis pass",
	"cd .. && sdlc gate analysis pass",
	"sdlc $verb analysis pass",
}

var personRules = map[string]string{
	"sdlc approve US-001":            "approval-is-a-human-decision",
	"sdlc unfreeze --reason x":       "unfreeze-is-a-human-decision",
	"sdlc ack --through HEAD":        "acknowledgement-is-a-human-decision",
	"sdlc release US-001 --reason x": "release-is-a-human-decision",
	"sdlc parallel US-001":           "parallel-is-a-human-decision",
	"sdlc stop":                      "stop-is-a-human-decision",
}

func TestAC12TheProjectManagerRunsNoSdlcCommandThatWritesButTheStaffing(t *testing.T) {
	manager := State{CommitWhy: "analysis has not passed", Agent: "project-manager"}
	for _, command := range managerRefused {
		f, ok := Inspect(command, manager)
		if !ok {
			t.Errorf("the project manager ran %q", command)
			continue
		}
		// A review it does not own is refused as anyone's is; everything else
		// under its own rule.
		if f.Rule != managerRule && f.Rule != "review-is-recorded-by-its-reviewer" {
			t.Errorf("the project manager running %q was refused under %s, want %s", command, f.Rule, managerRule)
		}
		if f.Route == "" || f.Reason == "" {
			t.Errorf("the refusal of %q says nothing to do next: %+v", command, f)
		}
	}
	// What is a person's is refused to it as it is to every session.
	for command := range personRules {
		if f, ok := Inspect(command, manager); !ok {
			t.Errorf("the project manager ran %q", command)
		} else if f.Rule != personRules[command] && f.Rule != managerRule {
			t.Errorf("the project manager running %q was refused under %s", command, f.Rule)
		}
	}
	// And the rule is the project manager's alone: the same commands from the
	// main conversation are not refused under it.
	for _, command := range managerRefused {
		if f, ok := Inspect(command, State{CommitWhy: "analysis has not passed"}); ok && f.Rule == managerRule {
			t.Errorf("the main conversation running %q was refused under the project manager's rule", command)
		}
	}
}

func TestAC12TheRefusalTellsTheProjectManagerWhatItMayRun(t *testing.T) {
	f := refused(t, "sdlc gate analysis pass", State{Agent: "project-manager"}, managerRule)
	for _, want := range []string{"sdlc staffing set", "sdlc staffing amend"} {
		if !strings.Contains(f.Route, want) {
			t.Errorf("the route does not name `%s`: %s", want, f.Route)
		}
	}
	// T11: it cannot hand the story to a person, so it is told who can.
	f = refused(t, "sdlc escalate spec_unclear --message x", State{Agent: "project-manager"}, managerRule)
	if !strings.Contains(f.Route, "runbook") && !strings.Contains(f.Route, "main conversation") {
		t.Errorf("the route does not say who hands the story to a person: %s", f.Route)
	}
}

// What only reads, and everything that is not sdlc, is the project manager's
// as it is any read-only agent's.
func TestAC12TheProjectManagerReadsWhatAnyoneReads(t *testing.T) {
	manager := State{CommitWhy: "analysis has not passed", Agent: "project-manager"}
	for _, command := range []string{
		"sdlc staffing show",
		"sdlc staffing show --json",
		"sdlc staffing show --gate code_review",
		"sdlc status",
		"sdlc status --json",
		"sdlc review list",
		"sdlc review list --gate code_review --json",
		"sdlc story list",
		"sdlc artifact list",
		"sdlc log",
		"sdlc cost",
		"sdlc doctor",
		"sdlc version",
		"sdlc --help",
		"sdlc staffing --help",
		"git log --oneline -5",
		"git diff --stat",
		"grep -rn staffing internal/",
		"cat plugin/agents/security.md",
		"ls internal",
		`echo "sdlc gate analysis pass is the main conversation's"`,
	} {
		allowed(t, command, manager)
	}
}

// The agent's name is the one Claude Code sends, reduced as the hook reduces
// it. A name that only looks like it is somebody else.
func TestAC12OnlyTheProjectManagerItselfHasItsAllowance(t *testing.T) {
	for _, agent := range []string{"project-manager-2", "manager", "project_manager", "pm"} {
		if _, ok := Inspect("sdlc staffing set", State{CommitReady: true, Agent: agent}); !ok {
			t.Errorf("%q recorded the staffing as though it were the project manager", agent)
		}
	}
}
