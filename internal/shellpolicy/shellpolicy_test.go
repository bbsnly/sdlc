package shellpolicy

import (
	"strings"
	"testing"
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
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
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
		"sdlc unfreeze --reason \"AC-2 was wrong\"",
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
		"rm /home/me/repo/.sdlc/stories/B-2/gate-record.json",
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
