// A path that is only named, in the sandbox: the built hook is put the calls
// Claude Code would send, in a story's worktree with its tests frozen and the
// loop's record in it. internal/shellpolicy holds the tables of forms and
// internal/hook the state each is asked in; these are the hook's own answers,
// through the plugin's launcher, to a mention and to the act nearest to it.

package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// refusesAWrite puts a file tool's call from the main conversation to the
// hook, which has to refuse it under rule, and returns what it said to do
// instead.
func (sb sandbox) refusesAWrite(t *testing.T, dir, path, rule string) string {
	t.Helper()
	input := map[string]any{"file_path": path, "content": "x\n"}
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, dir, "", "Write", input))
	if code != 0 {
		t.Fatalf("the hook exited %d for %s, so its reply is never read:\n%s", code, describeInput("Write", input), stderr)
	}
	if stdout == carryOn {
		t.Fatalf("the hook let the main conversation run %s", describeInput("Write", input))
	}
	var r refusal
	oneLine(t, stdout, &r)
	reason := r.HookSpecificOutput.PermissionDecisionReason
	if r.HookSpecificOutput.PermissionDecision != "deny" || !strings.Contains(reason, "["+rule+"]") {
		t.Fatalf("%s was not refused under %s: %q", describeInput("Write", input), rule, stdout)
	}
	_, route, found := strings.Cut(filepath.ToSlash(reason), "Instead: ")
	if !found {
		t.Fatalf("the refusal of %s offers no route:\n%s", describeInput("Write", input), reason)
	}
	return route
}

func TestTheSandboxLetsThroughAPathThatIsOnlyNamedAndRefusesTheActBesideIt(t *testing.T) {
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GatePlan); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}
	if t.Failed() {
		t.Fatal("the story's tests were not frozen, so nothing after this can be run")
	}
	const (
		state, protected = "loop-state-through-the-tool", "protected-path-through-the-tool"
		frozen, approval = "frozen-test-through-the-tool", "approval-is-a-human-decision"
		delegates        = "orchestrator-delegates"
	)
	bash := func(command string) map[string]any { return map[string]any{"command": command} }

	for name, command := range map[string]string{
		"AC-1 an escalation whose message says what was refused":   `sdlc escalate spec_unclear --message "the hook refused; rm .sdlc/state/active was what I ran"`,
		"AC-1 an escalation with an arrow at the configuration":    `sdlc escalate pre_commit_approval --message 'see -> .sdlc/config.json'`,
		"AC-1 a gate note that names the contract":                 `sdlc gate plan fail --note="see -> CLAUDE.md"`,
		"AC-2 the loop's files left out of a diff a program reads": `git diff --numstat -- . ':(exclude).sdlc' | python3 -c 'import sys; print(sys.stdin.read())'`,
		"AC-2 the short form beside perl":                          `git log --oneline -- . ':!.sdlc' ':!CLAUDE.md' | perl -ne 'print'`,
		"AC-3 the help of approve":                                 `sdlc approve --help`,
		"AC-3 the help of unfreeze after its arguments":            `sdlc unfreeze --reason x -h`,
		"AC-3 the help of ack read through head":                   `sdlc ack --help 2>&1 | head -20`,
	} {
		t.Run(name, func(t *testing.T) { sb.allows(t, tree, "", "Bash", bash(command)) })
	}

	t.Run("AC-5 the act beside the text of a flag is refused", func(t *testing.T) {
		sb.refusesUnderOneOf(t, tree, `sdlc escalate spec_unclear --message "x" > .sdlc/config.json`, state)
		sb.refusesUnderOneOf(t, tree, `sdlc escalate spec_unclear --message "the hook refused"; rm .sdlc/state/active`, state)
		sb.refusesUnderOneOf(t, tree, `sdlc escalate spec_unclear --message refused; rm .sdlc/state/active`, state)
		sb.refusesUnderOneOf(t, tree, `sdlc gate plan fail --note "$(echo y > CLAUDE.md)"`, protected)
		sb.refusesUnderOneOf(t, tree, `echo "a; rm CLAUDE.md"`, protected)
	})
	t.Run("AC-5 a pathspec that does not exclude the path is refused", func(t *testing.T) {
		sb.refusesUnderOneOf(t, tree, `git diff -- .sdlc ':(exclude)docs' | perl -ne 'print'`, state)
		sb.refusesUnderOneOf(t, tree, `git diff -- . ':(exclude).sdlc' | python3 -c 'open(".sdlc/config.json","w")'`, state)
		sb.refusesUnderOneOf(t, tree, `git diff --numstat -- . ':(exclude).sdlc' > .sdlc/state/active`, state)
		sb.refusesUnderOneOf(t, tree, `git checkout -- . ':(exclude)docs'`, state, frozen, protected)
	})
	t.Run("AC-5 a help flag that does not stop the command is refused", func(t *testing.T) {
		for _, command := range []string{
			`sdlc approve US-001`,
			`sdlc approve --help=false US-001`,
			`sdlc approve --help --help=false US-001`,
			`sdlc approve --reject --help`,
			`sdlc approve -- --help`,
			`sdlc approve --help; sdlc approve US-001`,
			`sdlc approve --help | sh`,
		} {
			sb.refusesUnderOneOf(t, tree, command, approval)
		}
	})
	t.Run("AC-6 an argument the hook cannot place is refused and told so", func(t *testing.T) {
		for _, command := range []string{
			`sdlc escalate spec_unclear --messag "see -> .sdlc/config.json"`,
			`sdlc escalate spec_unclear -- --message "see -> .sdlc/config.json"`,
			`./sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`,
			`git --literal-pathspecs diff -- . ':(exclude).sdlc' | perl -ne 'print'`,
		} {
			reason := strings.ToLower(sb.refusesUnderOneOf(t, tree, command, state))
			if reason != "" && !strings.Contains(reason, "cannot tell") && !strings.Contains(reason, "could not tell") {
				t.Errorf("the refusal of %s does not say the hook could not tell:\n%s", command, reason)
			}
		}
	})

	t.Run("AC-4 the main conversation is told where a file that is not the story's work goes", func(t *testing.T) {
		route := sb.refusesAWrite(t, tree, filepath.Join(tree, "commit-msg.txt"), delegates)
		if !strings.Contains(route, ".sdlc/stories/US-001") {
			t.Errorf("the route does not name the story's own directory: %s", route)
		}
		if strings.Contains(route, filepath.ToSlash(filepath.Join(sb.repo, ".sdlc", "stories"))) {
			t.Errorf("the route names the main checkout's copy, which this session may not write: %s", route)
		}
		for _, word := range []string{"shell", "bash", "redirect", "printf", "heredoc", "cat >", "echo ", "tee "} {
			if strings.Contains(strings.ToLower(route), word) {
				t.Errorf("the route offers the shell (%q) as the way to write the file: %s", word, route)
			}
		}
	})
	t.Run("AC-4 the story's own directory is where the main conversation may write", func(t *testing.T) {
		there := filepath.Join(tree, ".sdlc", "stories", "US-001", "commit-msg.txt")
		sb.allows(t, tree, "", "Write", map[string]any{"file_path": there, "content": "fix: x\n"})
	})
}
