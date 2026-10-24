package e2e

// US-023 AC-10: a project that keeps its instructions in AGENTS.md runs the
// loop as a CLAUDE.md project does. init writes the section to AGENTS.md and no
// CLAUDE.md, doctor finds the contract there, a story starts, and AGENTS.md,
// a nested CLAUDE.md, the project's .agents and the person's ~/.agents are held
// from the session working it. AC-9: what the guides quote of init and doctor
// about AGENTS.md is what they print.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agFolded is a line with its white space folded, for comparing output a page
// quotes with output a command printed.
func agFolded(line string) string { return strings.Join(strings.Fields(line), " ") }

// agProject is a repository beside the sandbox's own that keeps its
// instructions in AGENTS.md, with sdlc init run in it and its work committed,
// and a sandbox value that works it. It returns what init and doctor printed.
func agProject(t *testing.T) (a sandbox, printed []string) {
	t.Helper()
	sb := newSandbox(t)
	proj := sb.own(t, "agents-project")
	sb.mustGit(t, proj, "init", "--quiet", "--initial-branch=main")
	writeFile(t, filepath.Join(proj, "AGENTS.md"), "# The project\n\nOur own instructions.\n")

	r := sb.person(t, proj, "init")
	if r.code != 0 {
		t.Fatalf("sdlc init in a project with only an AGENTS.md exited %d:\n%s%s", r.code, r.stdout, r.stderr)
	}
	printed = append(printed, r.stdout)
	if !strings.Contains(r.stdout, "AGENTS.md") || strings.Contains(r.stdout, "CLAUDE.md") {
		t.Errorf("AC-10 init does not say it wrote AGENTS.md, or names CLAUDE.md:\n%s", r.stdout)
	}
	if _, err := os.Lstat(filepath.Join(proj, "CLAUDE.md")); err == nil {
		t.Fatal("AC-10 init created a CLAUDE.md, which hides AGENTS.md from Claude Code")
	}
	body, err := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
	if err != nil || !strings.HasPrefix(string(body), "# The project\n\nOur own instructions.\n") ||
		!strings.Contains(string(body), "## SDLC Contract") {
		t.Fatalf("AC-10 AGENTS.md is not the project's words with the section appended (%v):\n%s", err, body)
	}
	sb.mustGit(t, proj, "add", "--all")
	sb.mustGit(t, proj, "commit", "--quiet", "--message", "chore: sdlc init in a project that keeps AGENTS.md")

	doctor := sb.person(t, proj, "doctor")
	printed = append(printed, doctor.stdout)
	contract := false
	for line := range strings.SplitSeq(doctor.stdout, "\n") {
		if strings.Contains(line, "project contract") {
			contract = strings.HasPrefix(strings.TrimSpace(line), "ok") && strings.Contains(line, "AGENTS.md")
		}
	}
	if !contract {
		t.Errorf("AC-10 doctor does not find the contract in AGENTS.md:\n%s", doctor.stdout)
	}

	// What doctor says of the two layouts a guide shows beside this one: a
	// CLAUDE.md that does not load AGENTS.md, and one that imports it.
	for _, claude := range []string{"# Ours\n", "# Ours\n\n@AGENTS.md\n"} {
		writeFile(t, filepath.Join(proj, "CLAUDE.md"), claude)
		printed = append(printed, sb.person(t, proj, "doctor").stdout)
	}
	if err := os.Remove(filepath.Join(proj, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	a = sb
	a.repo = proj
	return a, printed
}

// AC-10: the loop in a project with AGENTS.md and no CLAUDE.md.
func TestAC10AProjectThatKeepsAGENTSmdRunsTheLoopAndHoldsIt(t *testing.T) {
	a, _ := agProject(t)
	a.works(t, a.repo, "", "", "start")
	tree := a.tree()
	if _, err := os.Stat(filepath.Join(tree, "AGENTS.md")); err != nil {
		t.Fatalf("the story's worktree has no AGENTS.md: %v", err)
	}

	// The rule is in force at this point for what it held before the story.
	a.refusesAWrite(t, tree, filepath.Join(tree, ".sdlc", "config.json"), "write-protected-path")

	for _, rel := range []string{"AGENTS.md", "docs/AGENTS.md", "sub/CLAUDE.md", ".agents/skills/x/SKILL.md"} {
		t.Run("AC-10 a Write of "+rel, func(t *testing.T) {
			a.refusesAWrite(t, tree, filepath.Join(tree, filepath.FromSlash(rel)), "write-protected-path")
		})
	}
	for _, cmd := range []string{"echo x > AGENTS.md", "echo x >> docs/agents.md", "mkdir -p .agents && echo x > .agents/s"} {
		t.Run("AC-10 the shell: "+cmd, func(t *testing.T) {
			a.refuses(t, sandboxSession, tree, map[string]any{"command": cmd}, "protected-path-through-the-tool")
		})
	}
	home := a.own(t, "home")
	t.Run("AC-10 the person's ~/.agents", func(t *testing.T) {
		a.refusesAWrite(t, tree, filepath.Join(home, ".agents", "skills", "x", "SKILL.md"), "agents-config-is-the-persons")
	})
	// The main conversation writes nothing during an iteration, so a page that
	// only names the agents is refused under that rule, not as protected.
	t.Run("AC-10 a page that names the agents is another file", func(t *testing.T) {
		a.refusesAWrite(t, tree, filepath.Join(tree, "docs", "the-agents.md"), "orchestrator-delegates")
	})
	if _, err := os.Lstat(filepath.Join(tree, "CLAUDE.md")); err == nil {
		t.Error("AC-10 a CLAUDE.md appeared in the story's worktree")
	}
}

// agReason is what the hook said of a Write of path from the main
// conversation, and whether it refused it.
func (sb sandbox) agReason(t *testing.T, dir, path string) (string, bool) {
	t.Helper()
	input := map[string]any{"file_path": path, "content": "x\n"}
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, dir, "", "Write", input))
	if code != 0 {
		t.Fatalf("the hook exited %d for %s, so its reply is never read:\n%s", code, describeInput("Write", input), stderr)
	}
	if stdout == carryOn {
		return "", false
	}
	var r refusal
	oneLine(t, stdout, &r)
	return r.HookSpecificOutput.PermissionDecisionReason, r.HookSpecificOutput.PermissionDecision == "deny"
}

// AC-12 and AC-13 in the sandbox: during a story, CLAUDE.local.md is held as
// CLAUDE.md is, and a CLAUDE.md, CLAUDE.local.md or AGENTS.md in a directory
// above the project is refused with a fix that says Claude Code loads
// instruction files from every directory above the one it starts in. The
// sandbox's own directory is above the repository and its worktree; a
// directory beside the repository is not.
func TestAC12AndAC13TheSandboxHoldsCLAUDElocalmdAndTheInstructionFilesAboveIt(t *testing.T) {
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	tree := sb.tree()
	for _, rel := range []string{"CLAUDE.local.md", "sub/CLAUDE.local.md"} {
		t.Run("AC-12 a Write of "+rel, func(t *testing.T) {
			sb.refusesAWrite(t, tree, filepath.Join(tree, filepath.FromSlash(rel)), "write-protected-path")
		})
	}
	t.Run("AC-12 the shell: echo x > CLAUDE.local.md", func(t *testing.T) {
		sb.refuses(t, sandboxSession, tree, map[string]any{"command": "echo x > CLAUDE.local.md"}, "protected-path-through-the-tool")
	})
	const above = "instructions-above-the-project"
	for _, name := range []string{"CLAUDE.md", "CLAUDE.local.md", "AGENTS.md"} {
		path := filepath.Join(sb.dir, name)
		t.Run("AC-13 a Write of "+name+" above the project", func(t *testing.T) {
			reason, refused := sb.agReason(t, tree, path)
			lower := strings.ToLower(reason)
			if !refused || !strings.Contains(reason, "["+above+"]") ||
				!strings.Contains(lower, "every directory above") || !strings.Contains(lower, "claude code") {
				t.Errorf("AC-13 a Write of %s was not refused as %s with a fix that says Claude Code loads instruction "+
					"files from every directory above the one it starts in: %q", path, above, reason)
			}
		})
		t.Run("AC-13 the shell writes "+name+" above the project", func(t *testing.T) {
			sb.refuses(t, sandboxSession, tree, map[string]any{"command": "echo x > " + filepath.ToSlash(path)}, above)
		})
	}
	t.Run("AC-13 a directory beside the project is not above it", func(t *testing.T) {
		beside := sb.own(t, "beside")
		if reason, _ := sb.agReason(t, tree, filepath.Join(beside, "CLAUDE.md")); strings.Contains(reason, "["+above+"]") {
			t.Errorf("AC-13 a CLAUDE.md beside the project was refused as above it: %s", reason)
		}
	})
}

// agQuoted is every line in a fenced block of a page that names AGENTS.md and
// reads as a line of init or doctor: what the page says the commands print.
func agQuoted(page string) []string {
	var quoted []string
	fenced := false
	for line := range strings.SplitSeq(strings.ReplaceAll(page, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced || !strings.Contains(line, "AGENTS.md") {
			continue
		}
		for _, lead := range []string{"created", "kept", "ok", "problem", "skipped", "fix:"} {
			if strings.HasPrefix(trimmed, lead+" ") {
				quoted = append(quoted, agFolded(trimmed))
				break
			}
		}
	}
	return quoted
}

// AC-9: the guides quote init and doctor on AGENTS.md, and each line they
// quote is one the commands printed in the AC-10 project.
func TestAC9WhatTheGuidesQuoteOfInitAndDoctorOnAGENTSmdIsWhatTheyPrint(t *testing.T) {
	_, printed := agProject(t)
	seen := map[string]bool{}
	for _, out := range printed {
		for line := range strings.SplitSeq(out, "\n") {
			seen[agFolded(line)] = true
		}
	}
	total := 0
	for _, name := range []string{
		"getting-started.md", "guides/adding-to-an-existing-project.md", "commands.md", "troubleshooting.md",
	} {
		body, err := os.ReadFile(filepath.Join(moduleRoot(t), "docs", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range agQuoted(string(body)) {
			total++
			if !seen[line] {
				t.Errorf("AC-9 docs/%s quotes a line init and doctor do not print:\n%s", name, line)
			}
		}
	}
	if total == 0 {
		t.Error("AC-9 none of the four pages quotes what init or doctor prints about AGENTS.md")
	}
}

// AC-14, AC-15 and AC-16 in the sandbox (design review round 3): during a
// story, a glob in an absolute directory does not reach the loop's
// configuration, ln and install do not replace the root's CLAUDE.md or land
// one above the project, and a program that writes the file it is given by an
// option does not write CLAUDE.md or the configuration. Each was let through
// by the released hook when the reviewers tried it.
func TestAC14AC15AndAC16TheSandboxRefusesAGlobALinkAndAWritingProgram(t *testing.T) {
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	tree := sb.tree()
	payload := sb.own(t, "payload")
	writeFile(t, filepath.Join(payload, "CLAUDE.md"), "planted\n")
	p := filepath.ToSlash(payload)
	// The worktree's configuration, with a glob in the name of the sandbox's
	// own directory, which is above every checkout in it.
	rel, err := filepath.Rel(sb.dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	globbed := filepath.ToSlash(filepath.Join(filepath.Dir(sb.dir), filepath.Base(sb.dir)[:3]+"*", rel))
	for cmd, rule := range map[string]string{
		"echo x > " + globbed + "/.sdlc/config.json":            "loop-state-through-the-tool",
		"ln -sf " + p + "/CLAUDE.md .":                          "protected-path-through-the-tool",
		"install " + p + "/CLAUDE.md .":                         "protected-path-through-the-tool",
		"ln -s " + p + "/CLAUDE.md " + filepath.ToSlash(sb.dir): "instructions-above-the-project",
		"sort -o CLAUDE.md " + p + "/CLAUDE.md":                 "protected-path-through-the-tool",
		"sort -o .sdlc/config.json " + p + "/CLAUDE.md":         "loop-state-through-the-tool",
	} {
		t.Run("US-023 "+cmd, func(t *testing.T) {
			sb.refuses(t, sandboxSession, tree, map[string]any{"command": cmd}, rule)
		})
	}
}
