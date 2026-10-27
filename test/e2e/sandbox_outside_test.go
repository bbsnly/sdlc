// US-019 in the sandbox: a path outside the repository, put to the built hook
// through the plugin launcher as the Bash or file-tool call Claude Code would
// send, in a story's worktree with a frozen test in it. The loop governs the
// repository and nothing else: the shell rules let a placed outside path go
// whatever its name, the Write tool still refuses it for being outside, and
// what reaches the repository by a link, a `..`, a variable, a glob or another
// checkout is still the repository's. internal/shellpolicy and internal/hook
// hold the tables; these are the binary's own answers to a few of the rows.

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// outsideAnswer puts a Bash command of the session under test, made by agent
// ("" is the main conversation) from dir, to the hook, and returns the reason
// it was refused with, or "" where it was let through.
func (sb sandbox) outsideAnswer(t *testing.T, dir, agent, command string) string {
	t.Helper()
	input := map[string]any{"command": command}
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, dir, agent, "Bash", input))
	if code != 0 {
		t.Fatalf("the hook exited %d for %s, so its reply is never read:\n%s", code, describeInput("Bash", input), stderr)
	}
	if stdout == carryOn {
		return ""
	}
	var r refusal
	oneLine(t, stdout, &r)
	if r.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("the hook's reply to %s is neither a refusal nor a pass: %q", describeInput("Bash", input), stdout)
	}
	return r.HookSpecificOutput.PermissionDecisionReason
}

func TestUS019APathOutsideTheRepositoryIsNotTheLoopsInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GatePlan); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}
	if t.Failed() {
		t.Fatal("the story's tests were not frozen, so nothing after this can be run")
	}
	// The sandbox's own directory holds the repository and is not part of it:
	// what is made here is outside, a flat copy of the frozen test among it.
	out := sb.own(t, "scratch")
	for _, name := range []string{"invoice_test.go", "other_test.go", "notes.txt"} {
		writeFile(t, filepath.Join(out, name), sandboxTest)
	}
	writeFile(t, filepath.Join(out, "copy", "invoice_test.go"), sandboxTest)
	writeFile(t, filepath.Join(out, "copy", ".sdlc", "state", "active"), "US-001\n")
	writeFile(t, filepath.Join(out, "copy", "CLAUDE.md"), "# contract\n")
	slash := filepath.ToSlash
	frozen := slash(filepath.Join(tree, "invoice_test.go"))
	outs := slash(out)

	t.Run("AC-1 and AC-2 a frozen-looking or test-looking name outside the repository is let through to both agents", func(t *testing.T) {
		for _, agent := range []string{"", "implementer"} {
			for _, row := range []struct{ dir, command string }{
				{tree, "rm -f " + outs + "/invoice_test.go"},
				{tree, "rm -f " + outs + "/*_test.go"},
				{tree, "rm -f " + outs + "/copy/invoice_test.go"},
				{tree, "echo x > " + outs + "/invoice_test.go"},
				{tree, "echo x > " + outs + "/brand_new_test.go"},
				{tree, "cp invoice_test.go " + outs + "/"},
				{tree, "find " + outs + " -name '*_test*' -delete"},
				{tree, "cd " + outs + " && find . -name '*_test*' -delete"},
				{out, "rm -f invoice_test.go"},
				{out, "echo x > brand_new_test.go"},
				{out, "find . -name '*_test*' -delete"},
				{filepath.Join(out, "copy"), "find . -name '*_test*' -delete"},
				{filepath.Join(out, "copy"), "printf x > .sdlc/state/active"},
				{filepath.Join(out, "copy"), "echo x > CLAUDE.md"},
			} {
				if reason := sb.outsideAnswer(t, row.dir, agent, row.command); reason != "" {
					t.Errorf("agent %q, from %s: %s was refused:\n%s", agent, row.dir, row.command, reason)
				}
			}
		}
	})

	t.Run("AC-1 the Write tool still refuses the same outside path, as outside", func(t *testing.T) {
		for _, agent := range []string{"", "implementer", "sdet"} {
			for _, path := range []string{
				filepath.Join(out, "notes.txt"),
				filepath.Join(out, "invoice_test.go"),
				filepath.Join(out, "copy", ".sdlc", "state", "active"),
			} {
				input := map[string]any{"file_path": path, "content": "x"}
				stdout, _, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, agent, "Write", input))
				var r refusal
				if code != 0 || stdout == carryOn {
					t.Errorf("agent %q wrote %s outside the repository: exit %d, %q", agent, path, code, stdout)
					continue
				}
				oneLine(t, stdout, &r)
				reason := r.HookSpecificOutput.PermissionDecisionReason
				if !strings.Contains(reason, "[write-outside-repository]") ||
					!strings.Contains(reason, "this path is outside the repository, and the loop only governs what it can review") {
					t.Errorf("agent %q, %s: refused otherwise than as outside:\n%s", agent, path, reason)
				}
			}
		}
	})

	t.Run("AC-3 a find from the repository, or a root the hook cannot place, is refused naming the root and the repository", func(t *testing.T) {
		for _, c := range []struct{ command, word string }{
			{`find "$S" -name '*_test*' -delete`, "$S"},
			{`rm -f "$S/invoice_test.go"`, "$S/invoice_test.go"},
			{"find . -name '*_test*' -delete", ""},
		} {
			reason := sb.outsideAnswer(t, tree, "", c.command)
			if reason == "" || !strings.Contains(reason, "[frozen-test-through-the-tool]") {
				t.Errorf("%s was not refused under the freeze: %q", c.command, reason)
				continue
			}
			if c.word != "" && !strings.Contains(reason, c.word) {
				t.Errorf("%s: the refusal does not name %q:\n%s", c.command, c.word, reason)
			}
			// The launcher gives the hook the sandbox's repository as the
			// session's project and the story's worktree, inside it, as where
			// the shell is; the worktree's path holds the repository's, so
			// either is the checkout the refusal may name.
			if !strings.Contains(reason, slash(sb.repo)) && !strings.Contains(reason, sb.repo) {
				t.Errorf("%s: the refusal does not name the repository %s it measured against:\n%s", c.command, sb.repo, reason)
			}
			if !strings.Contains(reason, "sdlc unfreeze") {
				t.Errorf("%s: the way on is no longer the unfreeze:\n%s", c.command, reason)
			}
		}
	})

	t.Run("AC-1 a word the hook cannot place keeps the loop-state refusal it has always had", func(t *testing.T) {
		reason := sb.outsideAnswer(t, tree, "", `printf x > "$S/copy/.sdlc/state/active"`)
		want := ".sdlc/state is the loop's own record, and a shell command is the one way around every rule that protects it. " +
			"Instead: the sdlc command owns this: `sdlc artifact write` for a gate's documents, `sdlc review add` for a review, " +
			"`sdlc gate` for an outcome; the freeze is lifted by the person running the session [loop-state-through-the-tool]"
		if reason != want {
			t.Errorf("loop state through $S changed:\n%s\nwant\n%s", reason, want)
		}
	})

	t.Run("AC-1 what reaches the repository from outside is still refused", func(t *testing.T) {
		in := filepath.Join(out, "in")
		if err := os.Symlink(tree, in); err != nil {
			t.Skipf("symbolic links cannot be made here: %v", err)
		}
		viaOut := slash(filepath.Join("..", "..", filepath.Base(sb.repo), ".sdlc", "worktrees", "US-001", "invoice_test.go"))
		for _, agent := range []string{"", "implementer"} {
			for _, row := range []struct{ dir, command string }{
				{tree, "rm -f " + frozen},
				{out, "rm -f " + frozen},
				{tree, "rm -f " + slash(in) + "/invoice_test.go"},
				{tree, "find " + slash(in) + " -name '*_test*' -delete"},
				{out, "rm -f in/invoice_test.go"},
				{in, "rm -f invoice_test.go"},
				{in, "find . -name '*_test*' -delete"},
				{filepath.Join(out, "copy"), "rm -f " + viaOut},
				{tree, `R=$PWD; rm -f "$R/invoice_test.go"`},
				{tree, `rm -f "$(pwd)/invoice_test.go"`},
				{out, `D=` + slash(tree) + `; rm -f "$D/invoice_test.go"`},
				{tree, "rm -f " + slash(filepath.Dir(sb.repo)) + "/*/.sdlc/worktrees/US-001/invoice_test.go"},
				{tree, "find " + outs + " . -name '*_test*' -delete"},
				{tree, "find " + slash(sb.dir) + " -name '*_test*' -delete"},
				{out, "find " + slash(tree) + " -name '*_test*' -delete"},
				{tree, "ln -s " + slash(tree) + " " + outs + "/invoice-link && rm -f " + outs + "/invoice-link/invoice_test.go"},
				// Another checkout of the same repository: the main checkout.
				{tree, "echo cheat >> " + slash(filepath.Join(sb.repo, "invoice_test.go"))},
			} {
				if reason := sb.outsideAnswer(t, row.dir, agent, row.command); reason == "" {
					t.Errorf("agent %q, from %s: %s went through", agent, row.dir, row.command)
				}
			}
		}
	})
}
