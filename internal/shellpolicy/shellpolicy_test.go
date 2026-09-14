package shellpolicy

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/policy"
)

var ready = State{CommitReady: true}
var notReady = State{CommitReady: false, CommitWhy: "code_review has not passed"}

func inspect(t *testing.T, command string, s State) (Finding, bool) {
	t.Helper()
	return Inspect(command, s)
}

func refused(t *testing.T, command string, s State, wantRule string) Finding {
	t.Helper()
	got, ok := inspect(t, command, s)
	if !ok {
		t.Fatalf("allowed: %s", command)
	}
	if got.Rule != wantRule {
		t.Errorf("%s was refused by %s, want %s (%s)", command, got.Rule, wantRule, got.Reason)
	}
	if got.Route == "" || got.Reason == "" {
		t.Errorf("finding = %+v", got)
	}
	return got
}

func allowed(t *testing.T, command string, s State) {
	t.Helper()
	if got, ok := inspect(t, command, s); ok {
		t.Errorf("%s was refused by %s: %s", command, got.Rule, got.Reason)
	}
}

// The one way around every rule that protects the loop's record is a shell
// command, so these are the commands that matter most.
func TestLoopStateCannotBeWrittenThroughTheShell(t *testing.T) {
	for _, command := range []string{
		"cat notes.md > .sdlc/stories/A-1/ANALYSIS.md",
		"echo pass >> .sdlc/stories/A-1/gate-record.json",
		"rm .sdlc/state/tests.lock",
		"rm -rf .sdlc/state",
		"mv /tmp/x.md .sdlc/stories/A-1/reviews/design_review-architect-1.md",
		"cp /tmp/plan.md ./.sdlc/stories/A-1/PLAN.md",
		"tee .sdlc/state/active < /dev/null",
		"true && printf pass > .sdlc/state/active",
		"touch .sdlc/config.json",
		`echo x > ".sdlc/state/active"`,
		// The same files, spelled the way macOS and Windows also find them.
		// The file-writing rules folded case and these did not.
		"echo x > .SDLC/state/active",
		"rm .sdlc/Stories/A-1/gate-record.json",
		"echo {} > .sdlc/Config.json",
		"rm -rf .SDLC",
		"echo x > .sdlc/ﬆate/active",
		"RM .sdlc/state/tests.lock",
		"rm.exe .sdlc/state/tests.lock",
		"echo x > .sdlc/config.json::$DATA",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
}

// The file-writing rules protected CLAUDE.md, .claude and .git, and the shell
// rules did not: `echo {} > .claude/settings.local.json` went through during an
// iteration, and a settings file is where hooks are turned off.
func TestHumanOwnedConfigurationCannotBeWrittenThroughTheShell(t *testing.T) {
	for _, command := range []string{
		`echo "the rules are gone" > CLAUDE.md`,
		"echo x > claude.md",
		"sed -i '' s/TBD/done/ CLAUDE.md",
		"echo {} > .claude/settings.local.json",
		"rm -rf .claude",
		"cp /tmp/hook .git/hooks/pre-commit",
		"rm -rf .git",
	} {
		refused(t, command, ready, "protected-path-through-the-tool")
	}
	for _, command := range []string{
		"cat CLAUDE.md",
		"grep -n Contract CLAUDE.md",
		"echo node_modules >> .gitignore",
		"echo x > .github/workflows/extra.yml",
		"git add -A",
	} {
		allowed(t, command, ready)
	}
}

// Every path the file-writing rules protect is refused through the shell too,
// by one rule or the other. A path added to one list and not the other is the
// gap this closes, reopened.
func TestTheShellProtectsEveryPathTheFileRulesDo(t *testing.T) {
	for _, protected := range policy.ProtectedPaths {
		command := "touch " + protected + "/x"
		if strings.HasSuffix(protected, ".md") || strings.HasSuffix(protected, ".json") {
			command = "touch " + protected
		}
		if _, ok := Inspect(command, ready); !ok {
			t.Errorf("%q is protected from Write and not from %q", protected, command)
		}
	}
}

// Reading is nobody's business here, and neither is anything outside the
// loop's own files. A rule that refused too much would be turned off.
func TestReadingAndOrdinaryWorkAreUntouched(t *testing.T) {
	for _, command := range []string{
		"cat .sdlc/stories/A-1/ANALYSIS.md",
		"grep -r TODO .sdlc/state/active",
		"ls -la .sdlc/stories/A-1",
		"go test ./... -count=1",
		"echo done > /tmp/scratch.md",
		"sdlc artifact write analysis < /tmp/analysis.md",
		"sdlc gate analysis pass --note ok",
		// Not `sdlc unfreeze`: that is a person's decision now, and refused
		// from a tool call (TestUnfreezeIsAHumanDecision).
		"echo hi > .sdlc/stories/A-1/notes.md",
		"npm run build && npm test",
		"rm -rf node_modules",
		"git status --porcelain",
		"git log --oneline -5",
	} {
		allowed(t, command, ready)
	}
}

// The rule is about the loop's own files, not about the directory they sit in.
// A scratch file inside a story's directory is the story's business; every
// story's gate documents are the loop's, including the ones nobody is working
// on, and an absolute path or a Windows separator is the same file.
func TestTheRuleFollowsTheFileNotTheDirectory(t *testing.T) {
	for _, command := range []string{
		"echo x > .sdlc/stories/B-2/ANALYSIS.md",
		"rm /home/dev/repo/.sdlc/stories/B-2/gate-record.json",
		`del .sdlc\stories\A-1\PLAN.md`,
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"echo hi > .sdlc/stories/A-1/notes.md",
		"echo hi > .sdlc/stories/A-1/scratch/draft.md",
		"rm .sdlc/stories/A-1/notes.md",
	} {
		allowed(t, command, ready)
	}
}

func TestTheCommitGateStandsInFrontOfTheCommit(t *testing.T) {
	got := refused(t, `git commit -m "done"`, notReady, "commit-gate")
	if !strings.Contains(got.Reason, "code_review") {
		t.Errorf("the refusal does not say what is missing: %q", got.Reason)
	}
	if !strings.Contains(got.Route, "sdlc stop") {
		t.Errorf("the refusal offers no way out: %q", got.Route)
	}

	for _, command := range []string{
		"git commit",
		"git -c user.email=a@b commit --amend",
		"/usr/bin/git commit -am wip",
		"go build ./... && git commit -m done",
	} {
		refused(t, command, notReady, "commit-gate")
	}
}

// Once the gates are done, committing is the point. A gate that never opens is
// not a gate.
func TestOnceTheGatesArePassedTheCommitGoesThrough(t *testing.T) {
	allowed(t, `git commit -m "done"`, ready)
}

// Reading git, and committing somewhere that is not this repository's history,
// are not the commit gate's business.
func TestOnlyCommittingIsTheCommitGatesBusiness(t *testing.T) {
	for _, command := range []string{
		"git add -A",
		"git diff HEAD",
		"git show --stat",
		"echo commit",
		"grep commit README.md",
	} {
		allowed(t, command, notReady)
	}
}

// The switch that turns enforcement off belongs to the person who started the
// session. A switch an assistant can reach is not a control.
func TestEnforcementCannotBeTurnedOffFromInside(t *testing.T) {
	for _, command := range []string{
		"SDLC_ENFORCE=0 git commit -m done",
		"export SDLC_ENFORCE=0",
		"SDLC_BIN=/tmp/fake sdlc gate commit pass",
		"CLAUDE_PROJECT_DIR=/tmp sdlc status",
	} {
		refused(t, command, ready, "enforcement-stays-on")
	}
}

// A separator hidden in quotes joins two segments into one, which can only make
// this notice more than it should, never less. Worth stating as a test so the
// direction of the imprecision stays deliberate.
func TestASeparatorIsFoundWhereverItIs(t *testing.T) {
	for _, command := range []string{
		"cd /tmp; rm /repo/.sdlc/state/active",
		"x=1 || echo y > .sdlc/state/active",
		"echo $(printf pass > .sdlc/state/active)",
		"echo `rm .sdlc/state/tests.lock`",
		"echo one\nrm .sdlc/state/tests.lock",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
}

func TestNothingIsNotACommand(t *testing.T) {
	for _, command := range []string{"", "   ", "\n\n", "&&", "|"} {
		allowed(t, command, notReady)
	}
}

func TestAnAllowedFindingHasNoMessage(t *testing.T) {
	if (Finding{}).Message() != "" {
		t.Error("an empty finding produced a message")
	}
	f := refused(t, "rm .sdlc/state/active", ready, "loop-state-through-the-tool")
	for _, want := range []string{"Instead:", "[loop-state-through-the-tool]"} {
		if !strings.Contains(f.Message(), want) {
			t.Errorf("message is missing %q: %q", want, f.Message())
		}
	}
}

// The freeze is the hinge the whole loop turns on, and it was enforced against
// the file tools only. `Write` to `x_test.go` was refused as a frozen
// acceptance test; `echo cheat > x_test.go` was allowed. One redirect went
// round all of it.
func TestAFrozenTestCannotBeWrittenThroughTheShell(t *testing.T) {
	state := State{CommitReady: true, Frozen: []string{"internal/invoice_test.go", "x_test.go"}}
	for _, command := range []string{
		"echo cheat > x_test.go",
		"echo cheat >> x_test.go",
		"echo cheat > ./x_test.go",
		"sed -i '' s/want/got/ internal/invoice_test.go",
		"rm internal/invoice_test.go",
		"mv other.go x_test.go",
		"cp /dev/null internal/invoice_test.go",
		"tee x_test.go < /dev/null",
		"python3 -c 'open(\"x_test.go\",\"w\")' && echo done",
		"true; echo cheat > x_test.go",
		"echo cheat > X_TEST.GO",
		"echo cheat > x_teﬆ.go",
		"RM internal/invoice_test.go",
		"python3 -c 'open(\"x_teﬆ.go\",\"w\")'",
	} {
		t.Run(command, func(t *testing.T) {
			f, refused := Inspect(command, state)
			if !refused {
				t.Fatal("a frozen acceptance test was writable through the shell")
			}
			if f.Rule == "" {
				t.Error("the refusal names no rule")
			}
		})
	}
}

// And everything else stays out of the way. A loop that refuses the test
// command is a loop nobody runs.
func TestOrdinaryCommandsAreStillFineAfterTheFreeze(t *testing.T) {
	state := State{CommitReady: true, Frozen: []string{"internal/invoice_test.go", "x_test.go"}}
	for _, command := range []string{
		"go test ./...",
		"go test ./internal/... -run TestInvoice",
		"echo hi > internal/invoice.go",
		"cat internal/invoice_test.go",
		"grep -n want internal/invoice_test.go",
		"sed -n 1,20p internal/invoice_test.go",
		"gofmt -l .",
	} {
		t.Run(command, func(t *testing.T) {
			if f, refused := Inspect(command, state); refused {
				t.Errorf("refused an ordinary command: %s", f.Message())
			}
		})
	}
}

// A freeze that exists and cannot be read hands over a test matcher instead of
// a list, and everything the matcher calls a test is frozen. Treating it as no
// freeze made corrupting tests.lock the way to `echo` into a frozen test.
func TestAnUnreadableFreezeFreezesEveryTest(t *testing.T) {
	state := State{CommitReady: true, IsTest: func(p string) bool { return strings.HasSuffix(p, "_test.go") }}
	if _, refused := Inspect("echo cheat > ./x_test.go", state); !refused {
		t.Error("a test file was writable through the shell while the freeze could not be read")
	}
	for _, command := range []string{"echo hi > x.go", "go test ./...", "cat x_test.go"} {
		if f, refused := Inspect(command, state); refused {
			t.Errorf("refused %q: %s", command, f.Message())
		}
	}
}

// The gates having passed is not the same as the commit being what passed them.
// Code edited after the code review was committed without a word, and only the
// commit gate, recorded after the commit, noticed.
func TestACommitWaitsForTheReviewsToBeOfWhatIsCommitted(t *testing.T) {
	asked := 0
	stale := State{CommitReady: true, Fresh: func() (bool, string) {
		asked++
		return false, "the work has changed since code-reviewer (code_review) reviewed it"
	}}
	if f := refused(t, "git commit -m done", stale, "commit-gate"); !strings.Contains(f.Reason, "changed since") {
		t.Errorf("the refusal does not say what changed: %q", f.Reason)
	}
	// Measuring the tree is for a commit, not for every command.
	allowed(t, "go test ./...", stale)
	allowed(t, "git status", stale)
	if asked != 1 {
		t.Errorf("the tree was measured %d times; only the commit should ask", asked)
	}

	allowed(t, "git commit -m done", State{CommitReady: true, Fresh: func() (bool, string) { return true, "" }})

	// Gates that have not passed answer first, without measuring anything.
	asked = 0
	notYet := State{CommitWhy: "code_review has not passed", Fresh: func() (bool, string) {
		asked++
		return true, ""
	}}
	refused(t, "git commit -m done", notYet, "commit-gate")
	if asked != 0 {
		t.Error("the tree was measured for a story whose gates have not passed")
	}
}

// Lifting the freeze was an instruction in the runbook and nothing more, so an
// agent failing a test could lift the freeze on it from its own shell.
func TestUnfreezeIsAHumanDecision(t *testing.T) {
	for _, command := range []string{
		`sdlc unfreeze --reason "the test is wrong"`,
		"/usr/local/bin/sdlc unfreeze --reason x",
		`C:\Users\dev\AppData\Local\sdlc\bin\sdlc.exe unfreeze --reason x`,
		"SDLC unfreeze --reason x",
		"npx @bbsnly/sdlc unfreeze --reason x",
		"go run ./cmd/sdlc unfreeze --reason x",
		"sdlc --json unfreeze --reason x",
		"go test ./... || sdlc unfreeze --reason x",
	} {
		refused(t, command, ready, "unfreeze-is-a-human-decision")
	}
	for _, command := range []string{
		"sdlc freeze",
		"sdlc status --json",
		`sdlc gate tests_frozen pass --note "no need to unfreeze"`,
		"echo unfreeze",
	} {
		allowed(t, command, ready)
	}
}

// An approval answers a question the loop stopped to ask a person, so an agent
// that could run it would be answering its own question.
func TestApprovingIsAHumanDecision(t *testing.T) {
	for _, command := range []string{
		"sdlc approve US-001",
		"/usr/local/bin/sdlc approve",
		`C:\Users\dev\AppData\Local\sdlc\bin\sdlc.exe approve US-001`,
		"SDLC approve US-001",
		"npx @bbsnly/sdlc approve US-001",
		"go run ./cmd/sdlc approve US-001",
		"sdlc --json approve US-001",
		`sdlc approve US-001 --reject "not like this"`,
		"go test ./... && sdlc approve US-001",
	} {
		refused(t, command, ready, "approval-is-a-human-decision")
	}
	for _, command := range []string{
		`sdlc review add code_review code-reviewer approve --note "reads well"`,
		`sdlc escalate pre_commit_approval --message "approve the commit?"`,
		"sdlc status --json",
		"echo approve",
	} {
		allowed(t, command, ready)
	}
}

// The file tools refused a test file added after the freeze. A shell command
// was checked only against the files the freeze holds, so `echo > new_test.go`
// added one, written to pass, and nothing noticed.
func TestANewTestCannotBeAddedThroughTheShellAfterTheFreeze(t *testing.T) {
	state := State{
		CommitReady: true,
		Frozen:      []string{"x_test.go"},
		NewTest:     func(p string) bool { return strings.HasSuffix(p, "_test.go") && p != "x_test.go" },
	}
	for _, command := range []string{
		"echo 'package x' > y_test.go",
		"touch internal/new_test.go",
		"cp /tmp/passing.go ./z_test.go",
	} {
		refused(t, command, state, "no-new-test-after-the-freeze")
	}
	refused(t, "echo cheat > x_test.go", state, "frozen-test-through-the-tool")
	for _, command := range []string{"echo x > y.go", "go test ./...", "cat y_test.go"} {
		allowed(t, command, state)
	}
}

// Before the freeze there is nothing to protect, and the sdet writes these
// files for a living.
func TestBeforeTheFreezeTheShellIsAsFreeAsItWas(t *testing.T) {
	if _, refused := Inspect("echo written > x_test.go", State{CommitReady: true}); refused {
		t.Error("a test file was refused before anything was frozen")
	}
}
