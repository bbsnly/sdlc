package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rpHeld puts one write through each of the hook's two doors, the file tools and
// the shell, as an agent working the story.
func rpHeld(t *testing.T, root, agent, rel, spelled string) {
	t.Helper()
	r := call(t, event(root, "Write", agent, filepath.Join(root, filepath.FromSlash(spelled))), noEnv)
	if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "review-policy-is-not-edited") {
		t.Errorf("%s writing %s (the policy at %s): the file rule did not refuse it: %+v", agent, spelled, rel, r.HookSpecificOutput)
	}
	r = call(t, command(root, agent, "echo weakened >> "+spelled), noEnv)
	if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "protected-path-through-the-tool") {
		t.Errorf("%s writing %s (the policy at %s): the shell rule did not refuse it: %+v", agent, spelled, rel, r.HookSpecificOutput)
	}
}

// The person's Q1, end to end through the hook: the path comes from the
// configuration, and one that will not read leaves the policy where the defaults
// put it, which is where it most likely is, as the backlog's does.
func TestQ1TheHookHoldsTheReviewPolicyWhereverTheConfigurationSaysItIs(t *testing.T) {
	t.Parallel()
	const implementer = "sdlc:implementer"
	for _, c := range []struct {
		name, config, held string
		spelled            []string
	}{
		{"nothing said", `{"version":1}`, "REVIEW.md", []string{"REVIEW.md", "review.md"}},
		{"an empty path", `{"version":1,"reviews":{"policy":""}}`, "REVIEW.md", []string{"REVIEW.md"}},
		{"another file", `{"version":1,"reviews":{"policy":"docs/policy.md"}}`, "docs/policy.md", []string{"docs/policy.md", "Docs/POLICY.md"}},
		{"another file, spelt with ./", `{"version":1,"reviews":{"policy":"./docs/policy.md"}}`, "docs/policy.md", []string{"docs/policy.md"}},
		{"a configuration that is not JSON", `{not json`, "REVIEW.md", []string{"REVIEW.md"}},
		{"a configuration with a policy it refuses", `{"version":1,"reviews":{"policy":"../outside.md"}}`, "REVIEW.md", []string{"REVIEW.md"}},
		{"a policy with a newline in it", `{"version":1,"reviews":{"policy":"a\nb.md"}}`, "REVIEW.md", []string{"REVIEW.md"}},
	} {
		t.Run("Q1 "+c.name, func(t *testing.T) {
			t.Parallel()
			root := loopProject(t)
			write(t, root, ".sdlc/config.json", c.config)
			for _, spelled := range c.spelled {
				rpHeld(t, root, implementer, c.held, spelled)
			}
		})
	}

	t.Run("Q1 every agent, and the main conversation", func(t *testing.T) {
		t.Parallel()
		root := loopProject(t)
		for _, agent := range []string{"", "sdlc:sdet", "sdlc:researcher", "sdlc:code-reviewer", "sdlc:bookkeeper"} {
			if !denied(call(t, event(root, "Edit", agent, "REVIEW.md"), noEnv)) {
				t.Errorf("%q edited the policy", agent)
			}
			if !denied(call(t, command(root, agent, "rm REVIEW.md"), noEnv)) {
				t.Errorf("%q removed the policy", agent)
			}
		}
	})
	t.Run("Q1 moved, the old name is an ordinary file", func(t *testing.T) {
		t.Parallel()
		root := loopProject(t)
		write(t, root, ".sdlc/config.json", `{"version":1,"reviews":{"policy":"docs/policy.md"}}`)
		if r := call(t, event(root, "Write", implementer, "REVIEW.md"), noEnv); denied(r) {
			t.Errorf("a file the configuration moved the policy from was refused: %+v", r.HookSpecificOutput)
		}
		if r := call(t, command(root, implementer, "echo mine > REVIEW.md"), noEnv); denied(r) {
			t.Errorf("a file the configuration moved the policy from was refused: %+v", r.HookSpecificOutput)
		}
	})
	t.Run("Q1 a reviewer reads it", func(t *testing.T) {
		t.Parallel()
		root := loopProject(t)
		for _, cmd := range []string{"cat REVIEW.md", "grep nit REVIEW.md", "sdlc review list"} {
			if r := call(t, command(root, "sdlc:code-reviewer", cmd), noEnv); denied(r) {
				t.Errorf("%s was refused: %+v", cmd, r.HookSpecificOutput)
			}
		}
	})
	t.Run("Q1 a person changes it by hand outside a story", func(t *testing.T) {
		t.Parallel()
		root := loopProject(t)
		if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
			t.Fatal(err)
		}
		if r := call(t, event(root, "Edit", "", "REVIEW.md"), noEnv); denied(r) {
			t.Errorf("the policy was held with no story running: %+v", r.HookSpecificOutput)
		}
		if r := call(t, command(root, "", "echo mine >> REVIEW.md"), noEnv); denied(r) {
			t.Errorf("the policy was held with no story running: %+v", r.HookSpecificOutput)
		}
	})
}

// The reproduced bypass, through the hook's own reading of a session's command.
func TestAC3TheHookKeepsAReviewWithCountsItsReviewersOwn(t *testing.T) {
	t.Parallel()
	const rule = "review-is-recorded-by-its-reviewer"
	root := loopProject(t)
	for _, cmd := range []string{
		"sdlc review add code_review --important 3 code-reviewer block <<'SDLC_DOCUMENT'\nx\nSDLC_DOCUMENT",
		"sdlc review add code_review --nits 3 code-reviewer approve <<'SDLC_DOCUMENT'\nx\nSDLC_DOCUMENT",
	} {
		for _, agent := range []string{"sdlc:implementer", "", "general-purpose", "sdlc:architect"} {
			r := call(t, command(root, agent, cmd), noEnv)
			if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, rule) {
				t.Errorf("%q ran %q: %+v", agent, cmd, r.HookSpecificOutput)
			}
		}
		if r := call(t, command(root, "sdlc:code-reviewer", cmd), noEnv); denied(r) {
			t.Errorf("the code reviewer could not record its own review: %s", r.HookSpecificOutput.PermissionDecisionReason)
		}
	}
}

// Security, Gate 7 round 2, F1, through the hook's own reading of a command: the
// line after a comment that ends in a backslash is one the shell runs, so it is
// judged, or refused where it cannot be read.
func TestQ1TheHookJudgesTheLineAfterACommentThatEndsInABackslash(t *testing.T) {
	t.Parallel()
	const implementer = "sdlc:implementer"
	root := loopProject(t)
	for _, cmd := range []string{"true # \\\nrm REVIEW.md", "T=REVIEW.md # \\\nrm \"$T\""} {
		r := call(t, command(root, implementer, cmd), noEnv)
		if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "protected-path-through-the-tool") {
			t.Errorf("%q was not refused as a write to the policy: %+v", cmd, r.HookSpecificOutput)
		}
	}
	// The verifier, Gate 7 round 5 (V3): three backslashes end a comment as one
	// does, and the next line runs.
	for _, cmd := range []string{"true # \\\\\\\nrm REVIEW.md", "true # \\\\\\\r\nrm REVIEW.md", "sh -c 'true # \\\\\\\nrm REVIEW.md'"} {
		r := call(t, command(root, implementer, cmd), noEnv)
		why := r.HookSpecificOutput.PermissionDecisionReason
		if !denied(r) || !strings.Contains(why, "protected-path-through-the-tool") && !strings.Contains(why, "command-too-deep-to-read") {
			t.Errorf("%q was not refused: %+v", cmd, r.HookSpecificOutput)
		}
	}
	// The person's option A, Gate 7 round 5: an ordinary line after a comment
	// that ends in a backslash is refused as too deep to read, and one after a
	// comment with no backslash at its end is not.
	if r := call(t, command(root, implementer, "echo before # a comment \\\necho next line ran"), noEnv); !denied(r) ||
		!strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "command-too-deep-to-read") {
		t.Errorf("an ordinary line after a comment that ends in a backslash was not refused as too deep to read: %+v", r.HookSpecificOutput)
	}
	if r := call(t, command(root, implementer, "echo before # a comment\necho next line ran"), noEnv); denied(r) {
		t.Errorf("an ordinary line after a comment was refused: %+v", r.HookSpecificOutput)
	}
}
