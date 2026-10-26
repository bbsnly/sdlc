package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// US-120 in the sandbox: the built binary, through the plugin's launcher, in a
// real git repository with a story started, refuses the shell spellings US-023's
// reviews showed getting past a rule, each under the rule its plain spelling
// gets; and a move of an ordinary file above the project is not named as
// putting an instruction file there. The unit rows are in internal/shellpolicy
// and internal/hook; these are one of each family, end to end.
func TestUS120TheSandboxRefusesTheShellSpellingsThatGotPastTheRules(t *testing.T) {
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	tree := sb.tree()
	side := filepath.ToSlash(sb.own(t, "side"))
	const (
		loopState = "loop-state-through-the-tool"
		protected = "protected-path-through-the-tool"
	)
	for _, row := range []struct{ criterion, command, rule string }{
		{"AC-1", "gzip .sdlc/config.json", loopState},
		{"AC-1", "zstd --rm CLAUDE.md", protected},
		{"AC-2", "rm .sdlc/stories/**/../state/active", loopState},
		{"AC-3", "cd " + side + " && CDPATH=" + filepath.ToSlash(tree) + " cd .sdlc && rm -f config.json", loopState},
		{"AC-5", "gunzip CLAUDE.md", protected},
		{"AC-6", "rm -f (CLAUDE|README).md", protected},
		{"AC-11", "shuf -o CLAUDE.md README.md", protected},
		{"AC-11", "split -l 1 README.md CLAUDE.md", protected},
	} {
		t.Run(row.criterion+" "+row.command, func(t *testing.T) {
			sb.refuses(t, sandboxSession, tree, map[string]any{"command": row.command}, row.rule)
		})
	}
	t.Run("AC-12 mv README.md .. is not an instruction file above the project", func(t *testing.T) {
		stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, "", bashTool, map[string]any{"command": "mv README.md .."}))
		if code != 0 {
			t.Fatalf("the hook exited %d:\n%s", code, stderr)
		}
		if strings.Contains(stdout, "[instructions-above-the-project]") {
			t.Errorf("AC-12 mv README.md .. puts no instruction file above the project, and was refused as if it did:\n%s", stdout)
		}
	})
}
