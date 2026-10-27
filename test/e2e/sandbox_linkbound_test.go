package e2e

// US-035, through the built hook and the plugin's launcher, in a story's
// worktree: a word through more than 64 links is refused under SDLC-E0081
// (AC-5, AC-12), a command whose words follow more links than its budget under
// SDLC-E0082 (AC-6, AC-12), and the debug log a person asks for with
// SDLC_DEBUG_FILE names the tool and the reading (AC-1) and holds no word of
// the command (AC-2). internal/hook and internal/pathrules hold the cases; this
// is the hook's own answer to one of each.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// lbChain makes name in dir the head of a chain of n links to target, and
// returns the word name/notes.txt; or skips where no link can be made.
func lbChain(t *testing.T, dir, name string, n int, target string) string {
	t.Helper()
	for i := range n {
		at, to := name, fmt.Sprintf("%s.%d", name, i+1)
		if i > 0 {
			at = fmt.Sprintf("%s.%d", name, i)
		}
		if i == n-1 {
			to = target
		}
		if err := os.Symlink(to, filepath.Join(dir, at)); err != nil {
			t.Skipf("this platform will not make a link here: %v", err)
		}
	}
	return name + "/notes.txt"
}

func TestTheSandboxRefusesAtTheLinkBoundsUnderTheirCodesAndLogsNoWordOfTheCommand(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	target := filepath.Join(tree, "elsewhere")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Split so that the secrets scan does not take the fixture for a token.
	const secret = "ghp_" + "zq7SecretShapedToken0123456789abcdefXYZ"
	logFile := filepath.Join(sb.own(t, "logs"), "hook.log")
	ruleID := regexp.MustCompile(`\[[a-z0-9]+(?:-[a-z0-9]+)*\]`)

	ask := func(t *testing.T, cmd string) string {
		t.Helper()
		call := toolCall(sandboxSession, tree, "implementer", bashTool, map[string]any{"command": cmd})
		stdout, stderr, code := sb.lateHook(t, tree, call, true, "SDLC_DEBUG_FILE="+logFile)
		if code != 0 {
			t.Fatalf("the hook exited %d:\n%s", code, stderr)
		}
		if stdout == carryOn {
			t.Fatalf("the hook let %.80s... through", cmd)
		}
		var r refusal
		oneLine(t, stdout, &r)
		if r.HookSpecificOutput.PermissionDecision != "deny" {
			t.Fatalf("the hook's reply to %.80s... is not a refusal: %q", cmd, stdout)
		}
		return r.HookSpecificOutput.PermissionDecisionReason
	}

	t.Run("AC-5 a word through 65 links", func(t *testing.T) {
		word := lbChain(t, tree, "lnkzq7", 65, target)
		reason := ask(t, "touch "+word+" # "+secret)
		for _, want := range []string{
			word + " goes through more than 64 links, more than sdlc follows in one path",
			"Instead: name the place it leads to directly, without the links",
			"SDLC-E0081",
		} {
			if !strings.Contains(reason, want) {
				t.Errorf("the refusal does not say %q:\n%s", want, reason)
			}
		}
		if !ruleID.MatchString(reason) {
			t.Errorf("the refusal names no rule id:\n%s", reason)
		}
	})

	t.Run("AC-6 a command over its budget", func(t *testing.T) {
		var words []string
		for i := range pathrules.CommandLinks/30 + 2 {
			words = append(words, lbChain(t, tree, fmt.Sprintf("bzq7%d", i), 30, target))
		}
		reason := ask(t, "touch "+strings.Join(words, " "))
		if !strings.Contains(reason, "SDLC-E0082") || !ruleID.MatchString(reason) {
			t.Errorf("the refusal of a command over its budget does not name SDLC-E0082 and a rule id:\n%s", reason)
		}
	})

	t.Run("AC-1 AC-2 the log names the tool and the reading and holds no word", func(t *testing.T) {
		raw, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("SDLC_DEBUG_FILE was set and the hook wrote no log there: %v", err)
		}
		log := string(raw)
		if !regexp.MustCompile(`(?m)\bcandidate=touch\b.*\bvia=released\b|\bvia=released\b.*\bcandidate=touch\b`).MatchString(log) {
			t.Errorf("the log has no line naming candidate=touch and via=released:\n%s", log)
		}
		for _, word := range []string{"zq7", secret, "notes.txt"} {
			if strings.Contains(log, word) {
				t.Errorf("the log holds %q, a word of a command:\n%s", word, log)
			}
		}
	})
}
