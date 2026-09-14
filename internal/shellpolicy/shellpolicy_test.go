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

// What only wraps a command is not the command. Every one of these went
// through, because the rules read the first word and the first word was `env`,
// `(git` or `sh`.
func TestAWrappedCommandIsStillTheCommand(t *testing.T) {
	for _, command := range []string{
		"env git commit -m x",
		"(git commit -m x)",
		"sh -c 'git commit -m x'",
		`bash -lc "git commit -m x"`,
		"if true; then git commit -m x; fi",
		"(cd sub && git commit -m x)",
		"sudo git commit -m x",
		"time git commit -m x",
		"nice -n 10 git commit -m x",
	} {
		refused(t, command, notReady, "commit-gate")
	}
	for _, command := range []string{
		"(rm .sdlc/state/tests.lock)",
		"env rm .sdlc/state/tests.lock",
		"sh -c 'rm .sdlc/state/tests.lock'",
		"echo .sdlc/state/tests.lock | xargs rm",
		"find .sdlc/state -name tests.lock -delete",
		"dd if=/dev/null of=.sdlc/state/tests.lock",
		"git rm .sdlc/stories/A-1/gate-record.json",
		"git checkout -- .sdlc/state/tests.lock",
		`python3 -c "open('.sdlc/state/tests.lock','w').write('{}')"`,
		`python3 -c "import os; os.remove('.sdlc/state/tests.lock')"`,
		"for f in x; do rm .sdlc/state/tests.lock; done",
		// A relative path, after a cd earlier in the same command.
		"cd .sdlc/stories/A-1 && echo {} > gate-record.json",
		"cd .sdlc/state && rm tests.lock",
		"cd .sdlc && cd state && rm tests.lock",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	refused(t, "cd .claude && echo {} > settings.local.json", ready, "protected-path-through-the-tool")
	refused(t, "(sdlc unfreeze --reason x)", ready, "unfreeze-is-a-human-decision")
}

// The Bash tool keeps its directory between calls, so the cd can be in an
// earlier one. The session's directory is where the relative path starts.
func TestARelativePathIsReadFromWhereTheCommandRuns(t *testing.T) {
	inState := State{CommitReady: true, Dir: ".sdlc/state"}
	refused(t, "rm tests.lock", inState, "loop-state-through-the-tool")
	refused(t, "echo {} > ../stories/A-1/gate-record.json", inState, "loop-state-through-the-tool")
	allowed(t, "rm scratch.txt", State{CommitReady: true, Dir: "internal"})

	frozen := State{CommitReady: true, Frozen: []string{"internal/x_test.go"}, Dir: "internal"}
	refused(t, "echo cheat > x_test.go", frozen, "frozen-test-through-the-tool")
}

// Reading through the wrappers must not make ordinary work look like writing.
func TestWrappedOrdinaryWorkIsStillOrdinary(t *testing.T) {
	frozen := State{CommitReady: false, CommitWhy: "code_review has not passed",
		Frozen: []string{"internal/x_test.go"}}
	for _, command := range []string{
		"cd internal && go test ./...",
		"(cd web && npm test)",
		"env GOFLAGS=-mod=mod go build ./...",
		"sh -c 'go test ./...'",
		"find . -name '*.go'",
		"find internal -name x_test.go",
		"git checkout main",
		"git diff internal/x_test.go",
		"sed -n 1,20p internal/x_test.go",
		"perl -ne 'print' internal/x_test.go",
		"python3 -m pytest -c setup.cfg internal/x_test.go",
		"python3 -m pytest internal/x_test.go",
		"ruby -Itest internal/x_test.go",
		"cat .sdlc/stories/A-1/PLAN.md",
		"xargs -n1 echo < files.txt",
	} {
		allowed(t, command, frozen)
	}
}

// A review from anyone but the reviewer it names is the implementer approving
// its own work, and the gate passed on it.
func TestAReviewIsTheReviewersOwn(t *testing.T) {
	as := func(agent string) State { return State{CommitReady: true, Agent: agent} }
	for _, c := range []struct{ agent, command string }{
		{"implementer", "sdlc review add code_review code-reviewer approve --note ok"},
		{"", "sdlc review add design_review architect approve"},
		{"general-purpose", "sdlc review add verifier_review verifier approve"},
		{"architect", "sdlc review add design_review security approve"},
		{"implementer", `sdlc review add --note "all good" code_review code-reviewer approve`},
		{"implementer", "npx @bbsnly/sdlc --json review add code_review code-reviewer approve"},
		{"implementer", "sdlc review add --story A-1 verifier_review verifier approve"},
		{"implementer", "sdlc review add code_review Code-Reviewer approve"},
	} {
		refused(t, c.command, as(c.agent), "review-is-recorded-by-its-reviewer")
	}
	allowed(t, "sdlc review add code_review code-reviewer approve --note ok", as("code-reviewer"))
	allowed(t, "sdlc review add design_review human-advocate note --note 'the architect missed AC-2'",
		as("human-advocate"))
	allowed(t, "sdlc review list --gate code_review", as("implementer"))
	allowed(t, `sdlc gate code_review pass --note "code-reviewer approved"`, as(""))
}

// Every agent hands its review or its plan to sdlc as a here-document, and a
// document talks about commands. Read as commands, these were refused.
func TestADocumentIsTextNotCommands(t *testing.T) {
	plan := "sdlc artifact write plan <<'SDLC_DOCUMENT'\n# Plan\n\n" +
		"3. Commit with `git commit -m \"...\"` at Gate 8.\n" +
		"Never run `sdlc unfreeze` or `sdlc approve`.\n" +
		"rm .sdlc/state/tests.lock would break the freeze.\n" +
		"SDLC_DOCUMENT"
	allowed(t, plan, notReady)
	redTeam := State{CommitReady: false, CommitWhy: notReady.CommitWhy, Agent: "red-team"}
	allowed(t, "sdlc review add design_review red-team note --note x <<-EOF\n\t`git commit` too early\n\tEOF", redTeam)
	allowed(t, "sdlc review add design_review red-team note <<< 'fine'", redTeam)

	// Text that goes to something that runs it is commands, and the command
	// that opens the document, and anything after it, is still read.
	refused(t, "bash <<'EOF'\nrm .sdlc/state/tests.lock\nEOF", ready, "loop-state-through-the-tool")
	refused(t, "cat <<'EOF' | sh\ngit commit -m x\nEOF", notReady, "commit-gate")
	refused(t, "cat > .sdlc/stories/A-1/PLAN.md <<'EOF'\nplan\nEOF", ready, "loop-state-through-the-tool")
	refused(t, "sdlc artifact write plan <<'EOF'\ntext\nEOF\ngit commit -m x", notReady, "commit-gate")
}

// Ways to write a frozen test that the shell rules read past: an overwriting
// redirect, a bundled -i, a program on standard input, and git.
func TestAFrozenTestCannotBeWrittenAnyOtherWay(t *testing.T) {
	frozen := State{CommitReady: true, Frozen: []string{"internal/x_test.go"}}
	for _, command := range []string{
		"echo x >| internal/x_test.go",
		"sed -Ei 's/a/b/' internal/x_test.go",
		"perl -pi.bak -e 's/a/b/' internal/x_test.go",
		"python3 - <<'EOF'\nopen('internal/x_test.go','w').write('')\nEOF",
		"python3 <<'EOF'\nopen('internal/x_test.go','w').write('')\nEOF",
		`python3 -c "import os; os.remove('internal/x_test.go')"`,
		"git checkout -- internal/x_test.go",
		"find internal -name x_test.go -delete",
		"ls internal/x_test.go | xargs rm",
	} {
		refused(t, command, frozen, "frozen-test-through-the-tool")
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
		// PowerShell, which is the shell on Windows.
		"Remove-Item .sdlc/state/tests.lock",
		"Set-Content -Path .sdlc/stories/A-1/gate-record.json -Value '{}'",
		`ri -Force .sdlc\state\active`,
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
		// As the code reviewer, whose review it is to record.
		allowed(t, command, State{CommitReady: true, Agent: "code-reviewer"})
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

// The implementer does not write tests, and that rule was only ever put to the
// file tools: before the freeze, or with new test files allowed after it, a
// redirect wrote one.
func TestTheImplementerCannotWriteATestThroughTheShell(t *testing.T) {
	state := State{
		CommitReady:     true,
		ImplementerTest: func(p string) bool { return strings.HasSuffix(p, "_test.go") },
	}
	for _, command := range []string{
		"echo 'package x' > y_test.go",
		"touch internal/new_test.go",
		"cp /tmp/passing.go ./z_test.go",
	} {
		refused(t, command, state, "implementer-does-not-write-tests")
	}
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
