package hook

// US-023 through the hook. AC-5: the hook's tables that refuse a spelling of
// CLAUDE.md (orRun's and namedRun's) ask each row again with AGENTS.md, in a
// world where every CLAUDE.md the row could reach has an AGENTS.md beside it,
// made the same way: a copy, a link or a second name. The tests here that
// stand alone in hook_test.go (a link, a PowerShell backtick, a path rooted
// without a drive) are asked again by hand through the same helpers. AC-6 and
// AC-11 are the file tools and the shell below the root and under .agents, and
// ~/.agents with HOME at a temp dir, from the session holding the story and
// from a second session, as US-108 holds ~/.claude. AC-12 asks each of those
// table rows once more with CLAUDE.local.md beside every CLAUDE.md, and AC-13
// is a project held at <temp>/<home>/code/project, with the home and the
// directories between above it.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const agAgentsRule = "agents-config-is-the-persons"

// agRespell is s with each spelling of CLAUDE.md written for AGENTS.md, letter
// for letter and case for case, where `.m` or `~1` follows the name after at
// most one quote; changed is false where there is none, or where s already
// names agents.
func agRespell(s string) (out string, changed bool) {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "agents") {
		return s, false
	}
	b := []byte(s)
	for i := 1; i+5 <= len(lower); i++ {
		if lower[i:i+5] != "laude" {
			continue
		}
		rest := lower[i+5:]
		if rest != "" && strings.ContainsRune("'\"`", rune(rest[0])) {
			rest = rest[1:]
		}
		if !strings.HasPrefix(rest, ".m") && !strings.HasPrefix(rest, ".{m") && !strings.HasPrefix(rest, "~1") {
			continue
		}
		if lower[i-1] != 'c' {
			continue
		}
		for j, c := range []byte("agents") {
			if b[i-1+j] >= 'A' && b[i-1+j] <= 'Z' {
				c = c - 'a' + 'A'
			}
			b[i-1+j] = c
		}
		changed = true
		i += 4
	}
	return string(b), changed
}

// agRespellLocal is s with each spelling of CLAUDE.md written for
// CLAUDE.local.md (AC-12): ".local" after the name and at most one quote,
// where `.m` or `.{m` follows. changed is false where there is none, or where
// s already names a .local.md.
func agRespellLocal(s string) (out string, changed bool) {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "local.m") || strings.Contains(lower, "local.{m") {
		return s, false
	}
	var b strings.Builder
	last := 0
	for i := 1; i+5 <= len(lower); i++ {
		if lower[i:i+5] != "laude" || lower[i-1] != 'c' {
			continue
		}
		at := i + 5
		if at < len(s) && strings.ContainsRune("'\"`", rune(s[at])) {
			at++
		}
		if rest := lower[at:]; !strings.HasPrefix(rest, ".m") && !strings.HasPrefix(rest, ".{m") {
			continue
		}
		b.WriteString(s[last:at])
		b.WriteString(".local")
		last, changed = at, true
		i = at
	}
	b.WriteString(s[last:])
	return b.String(), changed
}

// agMirror gives every CLAUDE.md under the directories an AGENTS.md beside
// it, made as that one was made: a link to the same place respelt, a second
// name of the AGENTS.md beside the file it is a second name of, or a copy.
// Where there is one already it is left alone.
func agMirror(t *testing.T, dirs ...string) {
	t.Helper()
	agMirrorAs(t, "AGENTS.md", agRespell, dirs...)
}

// agMirrorAs is agMirror for the twin called name, with a link's target
// respelt by respell.
func agMirrorAs(t *testing.T, name string, respell func(string) (string, bool), dirs ...string) {
	t.Helper()
	type seen struct {
		info fs.FileInfo
		twin string
	}
	var files []seen
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !strings.EqualFold(d.Name(), "CLAUDE.md") {
				return nil
			}
			twin := filepath.Join(filepath.Dir(path), name)
			if _, err := os.Lstat(twin); err == nil {
				return nil
			}
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				respelt, _ := respell(target)
				return os.Symlink(respelt, twin)
			}
			for _, s := range files {
				if os.SameFile(s.info, info) {
					return os.Link(s.twin, twin)
				}
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files = append(files, seen{info: info, twin: twin})
			return os.WriteFile(twin, raw, 0o644)
		})
		if err != nil {
			t.Fatalf("US-023 the %s beside each CLAUDE.md under %s could not be made: %v", name, dir, err)
		}
	}
}

// agAgentsMD is the AC-5 respelling of a hook table's row, asked as a
// subtest, with the world given its AGENTS.md first; and the AC-12 respelling,
// with the world given its CLAUDE.local.md.
func agAgentsMD(t *testing.T, command string, world []string, ask func(t *testing.T, command string)) {
	t.Helper()
	if respelt, ok := agRespell(command); ok {
		agMirror(t, world...)
		t.Run("US-023 AC-5 "+respelt, func(t *testing.T) { ask(t, respelt) })
	}
	if respelt, ok := agRespellLocal(command); ok {
		agMirrorAs(t, "CLAUDE.local.md", agRespellLocal, world...)
		t.Run("US-023 AC-12 "+respelt, func(t *testing.T) { ask(t, respelt) })
	}
}

// AC-5, AC-6: through the hook, a file tool's write to AGENTS.md at any depth
// and in any case, and to a CLAUDE.md below the root, is refused as protected,
// with the fix CLAUDE.md at the root gets.
func TestAC5AndAC6AFileToolsWriteToAGENTSmdOrANestedCLAUDEmdIsRefusedAsProtected(t *testing.T) {
	root := loopProject(t)
	want := reasonOf(call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, "CLAUDE.md")), noEnv))
	_, wantRoute, _ := strings.Cut(want, "Instead: ")
	for _, rel := range []string{"AGENTS.md", "agents.md", "sub/AGENTS.md", "a/b/Agents.MD", "sub/CLAUDE.md", "docs/claude.md"} {
		for _, tool := range []string{"Write", "Edit", "NotebookEdit"} {
			r := call(t, event(root, tool, "sdlc-implementer", filepath.Join(root, filepath.FromSlash(rel))), noEnv)
			got := reasonOf(r)
			_, route, _ := strings.Cut(got, "Instead: ")
			if !denied(r) || !strings.Contains(got, "[write-protected-path]") || route != wantRoute {
				t.Errorf("AC-5/AC-6 %s to %s: %q; want write-protected-path with CLAUDE.md's fix", tool, rel, got)
			}
		}
	}
	if r := call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, "docs", "the-agents.md")), noEnv); denied(r) {
		t.Errorf("AC-5 docs/the-agents.md was refused: %s", reasonOf(r))
	}
}

// AC-11: the project's .agents, at any depth and in any case, refused to the
// file tools and the shell as .claude is.
func TestAC11AWriteUnderTheProjectsDotAgentsIsRefusedThroughTheHook(t *testing.T) {
	root := loopProject(t)
	for _, rel := range []string{".agents/skills/x/SKILL.md", "sub/.agents/skills/x/SKILL.md", ".Agents/x.md", "sub/.claude/skills/x/SKILL.md"} {
		for _, tool := range []string{"Write", "Edit", "NotebookEdit"} {
			r := call(t, event(root, tool, "sdlc-implementer", filepath.Join(root, filepath.FromSlash(rel))), noEnv)
			if !denied(r) || !strings.Contains(reasonOf(r), "[write-protected-path]") {
				t.Errorf("AC-11 %s to %s: %q; want write-protected-path", tool, rel, reasonOf(r))
			}
		}
	}
	for _, cmd := range []string{"echo x > .agents/skills/x/SKILL.md", "rm -rf .agents", "echo x > sub/.agents/s", "echo x > AGENTS.md", "rm sub/AGENTS.md"} {
		r := call(t, command(root, "sdlc-implementer", cmd), noEnv)
		if !denied(r) || !strings.Contains(reasonOf(r), "[protected-path-through-the-tool]") {
			t.Errorf("AC-11 %q: %q; want protected-path-through-the-tool", cmd, reasonOf(r))
		}
	}
}

// AC-5: TestAShellCommandThroughALinkIsReadAsTheFileItReaches, for AGENTS.md.
func TestAC5AShellCommandThroughALinkToAGENTSmdIsReadAsAGENTSmd(t *testing.T) {
	root := loopProject(t)
	write(t, root, "AGENTS.md", "# contract\n")
	if err := os.Symlink(filepath.Join(root, "AGENTS.md"), filepath.Join(root, "brief.md")); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	if !denied(call(t, command(root, "sdlc-implementer", "echo x > brief.md"), noEnv)) {
		t.Error("AC-5 echo x > brief.md went through a link to AGENTS.md")
	}
}

// AC-5: TestAPowerShellCommandIsReadAsPowerShell, for AGENTS.md.
func TestAC5AGENTSmdEscapedWithABacktickIsNotRemovableFromPowerShell(t *testing.T) {
	root := loopProject(t)
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": working, "tool_name": "PowerShell", "cwd": root,
		"agent_type": "sdlc-implementer", "tool_input": map[string]string{"command": "Remove-Item AGENTS`.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := call(t, string(raw), noEnv); !denied(r) {
		t.Error("AC-5 AGENTS.md, escaped with a backtick, was removable from PowerShell")
	}
}

// AC-5: TestAPathRootedWithoutADriveIsGovernedOnWindows, for AGENTS.md.
func TestAC5AGENTSmdRootedWithoutADriveIsGovernedOnWindows(t *testing.T) {
	root := loopProject(t)
	volume := filepath.VolumeName(root)
	if volume == "" {
		t.Skip("only Windows has a path that is rooted and not absolute")
	}
	r := call(t, event(root, "Write", "sdlc-implementer", root[len(volume):]+`\AGENTS.md`), noEnv)
	if !denied(r) || !strings.Contains(reasonOf(r), "write-protected-path") {
		t.Errorf("AC-5 AGENTS.md, named without its drive, was not refused as protected: %+v", r.HookSpecificOutput)
	}
}

// agentsHome points HOME and USERPROFILE at a temp dir and returns the
// person's ~/.agents inside it.
func agentsHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".agents")
}

func agHeldAsThePersons(t *testing.T, what string, r reply) {
	t.Helper()
	said := strings.ToLower(reasonOf(r))
	if !denied(r) || !strings.Contains(said, "["+agAgentsRule+"]") {
		t.Errorf("AC-11 %s: %q; want it refused as %s", what, reasonOf(r), agAgentsRule)
		return
	}
	if !strings.Contains(said, "skills") || !strings.Contains(said, "agent tools") {
		t.Errorf("AC-11 %s: the refusal does not say other agent tools load skills from there: %s", what, reasonOf(r))
	}
}

// AC-11 and decision 2: a write under the person's ~/.agents, from the file
// tools and the shell, is refused in every session of a participating project,
// a linked ~/.agents through where it leads too; a project that does not take
// part keeps the write.
func TestAC11AWriteUnderThePersonsDotAgentsIsRefusedInEverySession(t *testing.T) {
	agents := agentsHome(t)
	root := loopProject(t)
	skill := filepath.Join(agents, "skills", "x", "SKILL.md")
	for _, tool := range []string{"Write", "Edit", "NotebookEdit"} {
		for _, session := range []string{working, "session-b"} {
			agHeldAsThePersons(t, tool+" from "+session, call(t, inSession(event(root, tool, "", skill), session), noEnv))
		}
	}
	for _, cmd := range []string{
		"echo x > ~/.agents/skills/x/SKILL.md",
		"echo x > " + filepath.ToSlash(skill),
		"cp /etc/hosts ~/.agents/skills/x/SKILL.md",
		"mkdir -p ~/.agents/skills/x && tee ~/.agents/skills/x/SKILL.md < /etc/hosts",
		"rm -rf ~/.agents/skills",
	} {
		for _, session := range []string{working, "session-b"} {
			agHeldAsThePersons(t, cmd+" from "+session, call(t, inSession(command(root, "", cmd), session), noEnv))
		}
	}
	plain := t.TempDir()
	if !allows(call(t, event(plain, "Write", "", skill), noEnv)) {
		t.Error("AC-11 a write to ~/.agents was refused in a project that does not take part")
	}
	if !allows(call(t, command(plain, "", "echo x > ~/.agents/skills/x/SKILL.md"), noEnv)) {
		t.Error("AC-11 a shell write to ~/.agents was refused in a project that does not take part")
	}
}

// AC-11: ~/.agents linked into a dotfiles directory, written where it leads.
func TestAC11ALinkedDotAgentsIsHeldWhereItLeads(t *testing.T) {
	agents := agentsHome(t)
	root := loopProject(t)
	dotfiles := filepath.Join(filepath.Dir(agents), "dotfiles", "agents")
	if err := os.MkdirAll(dotfiles, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, agents); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	target := filepath.Join(dotfiles, "skills", "x", "SKILL.md")
	for _, session := range []string{working, "session-b"} {
		agHeldAsThePersons(t, "Write through the link from "+session, call(t, inSession(event(root, "Write", "", target), session), noEnv))
		agHeldAsThePersons(t, "echo through the link from "+session,
			call(t, inSession(command(root, "", "echo x > "+filepath.ToSlash(target)), session), noEnv))
	}
}

// AC-12: through the hook, CLAUDE.local.md at any depth and in any case is
// refused to the file tools and the shell as CLAUDE.md is, with CLAUDE.md's
// fix; a link to it is read as it.
func TestAC12AWriteToCLAUDElocalmdIsRefusedAsTheSameWriteToCLAUDEmdIs(t *testing.T) {
	root := loopProject(t)
	want := reasonOf(call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, "CLAUDE.md")), noEnv))
	_, wantRoute, _ := strings.Cut(want, "Instead: ")
	for _, rel := range []string{"CLAUDE.local.md", "claude.local.md", "sub/CLAUDE.local.md", "a/b/Claude.Local.MD"} {
		for _, tool := range []string{"Write", "Edit", "NotebookEdit"} {
			r := call(t, event(root, tool, "sdlc-implementer", filepath.Join(root, filepath.FromSlash(rel))), noEnv)
			got := reasonOf(r)
			_, route, _ := strings.Cut(got, "Instead: ")
			if !denied(r) || !strings.Contains(got, "[write-protected-path]") || route != wantRoute {
				t.Errorf("AC-12 %s to %s: %q; want write-protected-path with CLAUDE.md's fix", tool, rel, got)
			}
		}
	}
	for _, cmd := range []string{
		"echo x > CLAUDE.local.md", "cp notes.md CLAUDE.local.md", "rm sub/CLAUDE.local.md", "echo x >> a/claude.local.md",
	} {
		r := call(t, command(root, "sdlc-implementer", cmd), noEnv)
		if !denied(r) || !strings.Contains(reasonOf(r), "[protected-path-through-the-tool]") {
			t.Errorf("AC-12 %q: %q; want protected-path-through-the-tool", cmd, reasonOf(r))
		}
	}
	if r := call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, "CLAUDE.local.md.bak")), noEnv); denied(r) {
		t.Errorf("AC-12 CLAUDE.local.md.bak was refused: %s", reasonOf(r))
	}
	write(t, root, "CLAUDE.local.md", "# mine\n")
	if err := os.Symlink(filepath.Join(root, "CLAUDE.local.md"), filepath.Join(root, "brief.md")); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	if !denied(call(t, command(root, "sdlc-implementer", "echo x > brief.md"), noEnv)) {
		t.Error("AC-12 echo x > brief.md went through a link to CLAUDE.local.md")
	}
}

// agAboveRule is the rule a write to an instruction file above the project is
// refused under (AC-13).
const agAboveRule = "instructions-above-the-project"

// agAboveWorld is a project held in a story at <base>/<home>/code/project, with
// HOME and USERPROFILE at <base>/<home>: so the home, <base>/<home>/code and
// <base> are all above the project, and <base>/<home>/code/other is beside it.
func agAboveWorld(t *testing.T) (root, home, base string) {
	t.Helper()
	base = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	home = filepath.Join(base, "home")
	root = filepath.Join(home, "code", "project")
	write(t, root, ".sdlc/config.json", `{"version":1}`)
	write(t, root, ".sdlc/state/active", "A-1\n")
	write(t, root, ".sdlc/state/session", working+"\n")
	write(t, root, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	write(t, filepath.Join(home, "code", "other"), "README.md", "beside\n")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return root, home, base
}

// agHeldAsAbove checks that a refusal is the AC-13 rule's, and that it says
// why: Claude Code loads instruction files from every directory above the one
// it starts in.
func agHeldAsAbove(t *testing.T, what string, r reply) {
	t.Helper()
	said := reasonOf(r)
	if !denied(r) || !strings.Contains(said, "["+agAboveRule+"]") {
		t.Errorf("AC-13 %s: %q; want it refused as %s", what, said, agAboveRule)
		return
	}
	lower := strings.ToLower(said)
	if !strings.Contains(lower, "every directory above") || !strings.Contains(lower, "claude code") {
		t.Errorf("AC-13 %s: the refusal does not say Claude Code loads instruction files from every directory above "+
			"the one it starts in: %s", what, said)
	}
}

// AC-13: during a story, a write to a CLAUDE.md, CLAUDE.local.md or AGENTS.md
// in a directory above the project, the person's home among them, is refused
// to the file tools and the shell, with a fix that says why. Today the file
// tools refuse it only as outside the repository, and the shell lets an
// absolute or ~ spelling through, since a word placed outside the project
// skips every rule about the project's files. The same names beside the
// project, or below the home but not above the project, are not this rule's.
func TestAC13AWriteToAnInstructionFileAboveTheProjectIsRefused(t *testing.T) {
	root, home, base := agAboveWorld(t)
	code := filepath.Join(home, "code")
	for _, path := range []string{
		filepath.Join(home, "CLAUDE.md"), filepath.Join(home, "AGENTS.md"), filepath.Join(home, "CLAUDE.local.md"),
		filepath.Join(code, "CLAUDE.md"), filepath.Join(code, "AGENTS.md"), filepath.Join(base, "CLAUDE.local.md"),
	} {
		for _, tool := range []string{"Write", "Edit", "NotebookEdit"} {
			agHeldAsAbove(t, tool+" to "+path, call(t, event(root, tool, "sdlc-implementer", path), noEnv))
		}
	}
	for _, cmd := range []string{
		"echo x > ~/CLAUDE.md",
		"printf x >> ~/CLAUDE.local.md",
		"echo x > $HOME/AGENTS.md",
		"echo x > ../CLAUDE.md",
		"echo x > ../../AGENTS.md",
		"cd .. && echo x > CLAUDE.local.md",
		"cp /etc/hosts " + slash(code) + "/CLAUDE.md",
		"tee " + slash(base) + "/AGENTS.md < /etc/hosts",
		"rm ~/AGENTS.md",
	} {
		agHeldAsAbove(t, cmd, call(t, command(root, "sdlc-implementer", cmd), noEnv))
	}
	// Beside the project, or under the home and not above the project: not
	// this rule's, whatever else refuses them.
	for _, path := range []string{
		filepath.Join(code, "other", "CLAUDE.md"), filepath.Join(home, "notes", "AGENTS.md"),
		filepath.Join(code, "project2", "CLAUDE.local.md"), filepath.Join(code, "proj", "CLAUDE.md"),
		filepath.Join(home, "notes.md"),
	} {
		if r := call(t, event(root, "Write", "sdlc-implementer", path), noEnv); strings.Contains(reasonOf(r), "["+agAboveRule+"]") {
			t.Errorf("AC-13 %s is not above the project and was refused as if it were: %s", path, reasonOf(r))
		}
	}
	for _, cmd := range []string{
		"echo x > " + slash(filepath.Join(code, "other")) + "/CLAUDE.md",
		"echo x > ~/notes/AGENTS.md",
		"echo x > ../other/CLAUDE.local.md",
		"echo x > ~/notes.md",
	} {
		if r := call(t, command(root, "sdlc-implementer", cmd), noEnv); strings.Contains(reasonOf(r), "["+agAboveRule+"]") {
			t.Errorf("AC-13 %q writes nothing above the project and was refused as if it did: %s", cmd, reasonOf(r))
		}
	}
	// The project's own CLAUDE.md stays the project's rule's.
	if r := call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, "CLAUDE.md")), noEnv); !strings.Contains(reasonOf(r), "[write-protected-path]") {
		t.Errorf("AC-13 the project's own CLAUDE.md: %q; want write-protected-path", reasonOf(r))
	}
}

// AC-11, the spellings of the home the red team found untried (design review
// round 1): $HOME in a command, ~/.agents in another case on a disk that
// folds case, and on macOS the home through the /System/Volumes/Data firmlink.
// Each is the person's ~/.agents and is refused under its own rule.
func TestAC11ThePersonsDotAgentsIsPlacedByEverySpellingOfTheHome(t *testing.T) {
	agents := agentsHome(t)
	root := loopProject(t)
	home := filepath.Dir(agents)
	for _, cmd := range []string{
		"echo x > $HOME/.agents/skills/x/SKILL.md",
		`echo x > "$HOME/.agents/skills/x/SKILL.md"`,
		"echo x > ${HOME}/.agents/skills/x/SKILL.md",
	} {
		agHeldAsThePersons(t, cmd, call(t, command(root, "", cmd), noEnv))
	}
	write(t, home, ".case-probe", "x\n")
	if _, err := os.Stat(filepath.Join(home, ".CASE-PROBE")); err == nil {
		upper := filepath.Join(home, ".AGENTS", "skills", "x", "SKILL.md")
		agHeldAsThePersons(t, "Write to "+upper, call(t, event(root, "Write", "", upper), noEnv))
		agHeldAsThePersons(t, "echo to ~/.AGENTS", call(t, command(root, "", "echo x > ~/.AGENTS/skills/x/SKILL.md"), noEnv))
	}
	const firmlink = "/System/Volumes/Data"
	if _, err := os.Stat(firmlink + home); err != nil {
		t.Skipf("only macOS reaches the home through %s (%v)", firmlink, err)
	}
	through := firmlink + filepath.Join(home, ".agents", "skills", "x", "SKILL.md")
	agHeldAsThePersons(t, "Write to "+through, call(t, event(root, "Write", "", through), noEnv))
}

// AC-11 and AC-5: on NTFS, `name::$INDEX_ALLOCATION` is the directory name
// itself (the spelling of Git's CVE-2019-1352 against .git), so
// `.agents::$INDEX_ALLOCATION\skills\x\SKILL.md` is a file in .agents.
// pathrules.Fold trims `::$DATA` and not this. Windows only: elsewhere the
// spelling names another file.
func TestAC11ADirectoryNamedThroughItsIndexStreamIsTheDirectoryOnWindows(t *testing.T) {
	root := loopProject(t)
	if filepath.VolumeName(root) == "" {
		t.Skip("only NTFS reads name::$INDEX_ALLOCATION as the directory name; this is not Windows")
	}
	for _, rel := range []string{
		`.agents::$INDEX_ALLOCATION\skills\x\SKILL.md`, `sub\.agents::$INDEX_ALLOCATION\skills\x\SKILL.md`,
		`.claude::$INDEX_ALLOCATION\settings.json`, `.AGENTS::$index_allocation\skills\x\SKILL.md`,
	} {
		r := call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, rel)), noEnv)
		if !denied(r) || !strings.Contains(reasonOf(r), "[write-protected-path]") {
			t.Errorf("AC-11 %s: %q; want write-protected-path", rel, reasonOf(r))
		}
	}
}

// agRuleOf is the rule id a refusal names at its end, in brackets, or "" for
// a call that went through.
func agRuleOf(r reply) string {
	if !denied(r) {
		return ""
	}
	said := reasonOf(r)
	open := strings.LastIndex(said, "[")
	if open < 0 || !strings.HasSuffix(said, "]") {
		return ""
	}
	return said[open+1 : len(said)-1]
}

// agShell is what the hook says of a shell command the implementer runs in
// the project at root.
func agShell(t *testing.T, root, cmd string) reply {
	t.Helper()
	return call(t, command(root, "sdlc-implementer", cmd), noEnv)
}

// agAboveWorldAt is agAboveWorld with the home called homeName and the
// project at <home>/<under...>, so that a directory above it can have a dot
// in its name: a GOPATH layout, or a home named first.last.
func agAboveWorldAt(t *testing.T, homeName string, under ...string) (root, home string) {
	t.Helper()
	base := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	home = filepath.Join(base, homeName)
	root = filepath.Join(append([]string{home}, under...)...)
	write(t, root, ".sdlc/config.json", `{"version":1}`)
	write(t, root, ".sdlc/state/active", "A-1\n")
	write(t, root, ".sdlc/state/session", working+"\n")
	write(t, root, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return root, home
}

// agFirmlink is the macOS spelling of every path on the data volume.
const agFirmlink = "/System/Volumes/Data"

// AC-13, design review round 3 (security B1, red team's gaming vectors): with
// a real disk and HOME at a temp dir, an instruction file above the project is
// held through every spelling of its name the shell has (a glob, a brace list,
// a backslash, another case), through a glob in the directory, through a link
// to the directory, through the macOS firmlink however many names above the
// project it is, and in the .claude of a directory above. Security ran these
// through today's hook: each is allowed.
func TestAC13AnInstructionFileAboveTheProjectIsHeldThroughEverySpellingTheHookSees(t *testing.T) {
	root, home, base := agAboveWorld(t)
	code := filepath.Join(home, "code")
	write(t, code, "CLAUDE.md", "the person's own\n")
	write(t, code, "AGENTS.md", "the person's own\n")
	cmds := []string{
		"echo x >> ~/code/CLAUDE.m?",
		"echo x | tee ~/code/CLAUDE.{md,x}",
		"echo x > " + slash(code) + "/[C]LAUDE.md",
		`echo x > ~/code/CLAUDE.m\d`,
		"sed -i s/a/b/ ~/code/CLAUDE.m*",
		"rm ~/code/AGENTS.m?",
		"echo x | tee ~/{code,x}/AGENTS.md",
		"echo x > ~/co*/CLAUDE.md",
		"echo x > " + slash(home) + "/co?e/AGENTS.md",
		"echo x > ~/code/CLAUDE.md",
		"echo x > ~/code/AGENTS.md",
		"cp /etc/hosts ~/code/CLAUDE.md",
		"echo x > " + slash(code) + "/claude.md",
		"echo x > ~/Agents.MD",
		"echo x > ~/code/Claude.Local.MD",
		"echo x > " + slash(code) + "/.claude/AGENTS.md",
		"echo x > ~/code/.clau*/CLAUDE.md",
		"cd $D && echo x > " + slash(code) + "/CLAUDE.md",
	}
	paths := []string{
		filepath.Join(home, "Agents.MD"), filepath.Join(code, "claude.md"), filepath.Join(code, ".claude", "AGENTS.md"),
	}
	// A directory above the project reached through a link made in an earlier
	// call.
	linked := filepath.Join(base, "linked")
	if err := os.Symlink(code, linked); err == nil {
		cmds = append(cmds, "echo x > "+slash(linked)+"/CLAUDE.md")
		paths = append(paths, filepath.Join(linked, "CLAUDE.md"))
	} else {
		t.Logf("AC-13 the linked rows are not asked: symbolic links cannot be made here: %v", err)
	}
	// The firmlink, one name above the project and as many as there are.
	if _, err := os.Stat(agFirmlink + code); err == nil {
		first, _, _ := strings.Cut(strings.TrimPrefix(slash(base), "/"), "/")
		cmds = append(cmds,
			"echo x > "+agFirmlink+slash(code)+"/CLAUDE.md",
			"echo x > "+agFirmlink+"/"+first+"/CLAUDE.md")
		paths = append(paths, agFirmlink+filepath.Join(code, "CLAUDE.md"))
	} else {
		t.Logf("AC-13 the firmlink rows are not asked: only macOS reaches a path through %s (%v)", agFirmlink, err)
	}
	for _, cmd := range cmds {
		t.Run("AC-13 "+cmd, func(t *testing.T) { agHeldAsAbove(t, cmd, agShell(t, root, cmd)) })
	}
	for _, path := range paths {
		for _, tool := range []string{"Write", "Edit"} {
			t.Run("AC-13 "+tool+" "+path, func(t *testing.T) {
				agHeldAsAbove(t, tool+" to "+path, call(t, event(root, tool, "sdlc-implementer", path), noEnv))
			})
		}
	}
	// A glob that reaches only what is beside the project, or no instruction
	// file, is not this rule's.
	for _, cmd := range []string{
		"echo x > ~/code/other/CLAUDE.m?",
		"echo x > ~/code/CLAUDE.md.ba?",
		"echo x > ~/co*/notes.md",
	} {
		t.Run("AC-13 "+cmd, func(t *testing.T) {
			if got := agRuleOf(agShell(t, root, cmd)); got == agAboveRule {
				t.Errorf("AC-13 %q writes no instruction file above the project and was refused as if it did", cmd)
			}
		})
	}
}

// AC-13 and AC-15, design review round 3 (security B2, red team hostile 2
// and 3): ln, install, cp and mv that land an instruction file in a directory
// above the project, in a real home, including a directory whose name has a
// dot. Security and red team ran these through today's hook: each is allowed.
func TestAC13AndAC15ALinkOrCopyThatLandsAnInstructionFileAboveTheProjectIsRefused(t *testing.T) {
	outside := t.TempDir()
	for _, name := range []string{"CLAUDE.md", "AGENTS.md", "CLAUDE.local.md", "notes.md"} {
		write(t, outside, name, "planted\n")
	}
	p := slash(outside)
	root, home, _ := agAboveWorld(t)
	code := filepath.Join(home, "code")
	for _, cmd := range []string{
		"ln -s " + p + "/CLAUDE.md ~/code",
		"ln " + p + "/CLAUDE.md ~/code",
		"install -m 644 " + p + "/CLAUDE.md ~/code",
		"ln -s " + p + "/CLAUDE.md ..",
		"ln -sf " + p + "/AGENTS.md " + slash(code),
		"install " + p + "/AGENTS.md ~",
		"ln -t .. " + p + "/CLAUDE.local.md",
		"install -t ~/code " + p + "/AGENTS.md",
		"cp " + p + "/CLAUDE.md ~/code",
		"cd .. && ln -s " + p + "/CLAUDE.md",
	} {
		t.Run("AC-13 "+cmd, func(t *testing.T) { agHeldAsAbove(t, cmd, agShell(t, root, cmd)) })
	}
	for _, cmd := range []string{"ln -s " + p + "/CLAUDE.md ~/code/other", "cp " + p + "/notes.md ~/code"} {
		t.Run("AC-13 "+cmd, func(t *testing.T) {
			if got := agRuleOf(agShell(t, root, cmd)); got == agAboveRule {
				t.Errorf("AC-13 %q lands no instruction file above the project and was refused as if it did", cmd)
			}
		})
	}
	// A home named first.last, with the project under ~/src/github.com.
	root, home = agAboveWorldAt(t, "first.last", "src", "github.com", "project")
	gh := slash(filepath.Join(home, "src", "github.com"))
	for _, cmd := range []string{
		"cp " + p + "/CLAUDE.md ~/src/github.com",
		"ln -s " + p + "/CLAUDE.md ~/src/github.com",
		"ln -s " + p + "/CLAUDE.md ~/src/github.com/",
		"install " + p + "/CLAUDE.md ~/src/github.com",
		"cp " + p + "/AGENTS.md " + gh,
		"install -m 644 " + p + "/CLAUDE.local.md " + gh,
		"mv " + p + "/CLAUDE.md " + gh,
		"ln " + p + "/CLAUDE.md " + slash(filepath.Join(home, "src")),
		"cp " + p + "/CLAUDE.md " + slash(home),
		"cp " + p + "/CLAUDE.md ..",
	} {
		t.Run("AC-13 "+cmd, func(t *testing.T) { agHeldAsAbove(t, cmd, agShell(t, root, cmd)) })
	}
}

// AC-13, design review round 3 (security note 2, red team hostile 1): a
// .sdlc link planted in a directory above the project, in an earlier call,
// must not make that directory read as inside the project. Today the plant is
// allowed, and the plan's precondition then reads ~/code as the project's.
func TestAC13APlantedDotSdlcAboveTheProjectDoesNotHideTheInstructionFilesThere(t *testing.T) {
	root, home, _ := agAboveWorld(t)
	code := filepath.Join(home, "code")
	write(t, root, "docs/README.md", "docs\n")
	write(t, code, "CLAUDE.md", "the person's own\n")
	if err := os.Symlink(filepath.Join(root, "docs"), filepath.Join(code, ".sdlc")); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "user_stories.json"), filepath.Join(home, ".sdlc")); err != nil {
		t.Fatal(err)
	}
	cmds := []string{
		"echo x > ~/code/CLAUDE.md",
		"echo x > ~/CLAUDE.md",
		"cp /etc/hosts ~/code/AGENTS.md",
		"echo x > " + slash(code) + "/CLAUDE.local.md",
	}
	paths := []string{filepath.Join(code, "CLAUDE.md"), filepath.Join(home, "AGENTS.md")}
	if _, err := os.Stat(agFirmlink + code); err == nil {
		cmds = append(cmds, "echo x > "+agFirmlink+slash(code)+"/CLAUDE.md")
		paths = append(paths, agFirmlink+filepath.Join(code, "CLAUDE.md"))
	}
	for _, cmd := range cmds {
		t.Run("AC-13 "+cmd, func(t *testing.T) { agHeldAsAbove(t, cmd, agShell(t, root, cmd)) })
	}
	for _, path := range paths {
		t.Run("AC-13 Write "+path, func(t *testing.T) {
			agHeldAsAbove(t, "Write to "+path, call(t, event(root, "Write", "sdlc-implementer", path), noEnv))
		})
	}
	// The project's own CLAUDE.md stays the project's rule's.
	if got := agRuleOf(agShell(t, root, "echo x > CLAUDE.md")); got != "protected-path-through-the-tool" {
		t.Errorf("AC-13 with the links planted, echo x > CLAUDE.md is refused as %q; want protected-path-through-the-tool", got)
	}
}

// AC-13, design review round 3 (red team hostile 4): a copy or an extraction
// that fills a directory above the project, in shapes the hook can see. Red
// team ran these against a State built like the hook's: each is allowed,
// but for git, which is refused today under the loop-state rule.
func TestAC13ACopyOrExtractionThatFillsADirectoryAboveTheProjectIsRefusedThroughTheHook(t *testing.T) {
	root, _, base := agAboveWorld(t)
	write(t, base, "p/CLAUDE.md", "planted\n")
	p := slash(filepath.Join(base, "p"))
	for _, cmd := range []string{
		"cd .. && tar -xf " + p + ".tar",
		"cd ~/code && unzip " + p + ".zip",
		"cp " + p + "/* ..",
		"ditto " + p + " ..",
		"ditto " + p + " ~/code",
		"bsdtar -xf " + p + ".tar -C ..",
		"7z x " + p + ".7z -o..",
		"cp -r " + p + "/. ~/code",
		"tar -xf " + p + ".tar -C ~/code",
	} {
		t.Run("AC-13 "+cmd, func(t *testing.T) { agHeldAsAbove(t, cmd, agShell(t, root, cmd)) })
	}
	for _, cmd := range []string{"git --work-tree=.. checkout HEAD -- .", "git --work-tree=" + slash(filepath.Dir(root)) + " checkout HEAD -- ."} {
		t.Run("AC-13 "+cmd, func(t *testing.T) {
			if got := agRuleOf(agShell(t, root, cmd)); got != agAboveRule && got != "loop-state-through-the-tool" {
				t.Errorf("AC-13 %q is refused as %q; want %s", cmd, got, agAboveRule)
			}
		})
	}
	for _, cmd := range []string{"tar -xf " + p + ".tar -C build", "cp " + p + "/* build/", "ditto " + p + " build"} {
		t.Run("AC-13 "+cmd, func(t *testing.T) {
			if got := agRuleOf(agShell(t, root, cmd)); got == agAboveRule {
				t.Errorf("AC-13 %q fills a directory in the project and was refused as above it", cmd)
			}
		})
	}
}

// Decision 7 and AC-13, through the hook: a path with two whole matches of one
// name is refused. The hook fails open on a panic, so a matcher that ignores
// yield's false return lets each of these through.
func TestAC5AndAC13APathWithTwoWholeMatchesOfOneNameIsRefusedThroughTheHook(t *testing.T) {
	root := loopProject(t)
	for _, rel := range []string{"a/.claude/b/.claude/c", ".agents/a/.agents/b", "a/CLAUDE.md/b/CLAUDE.md", "a/AGENTS.md/b/AGENTS.md"} {
		t.Run("AC-5 "+rel, func(t *testing.T) {
			if got := agRuleOf(agShell(t, root, "echo x > "+rel)); got != "protected-path-through-the-tool" {
				t.Errorf("AC-5 echo x > %s is refused as %q; want protected-path-through-the-tool", rel, got)
			}
			r := call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, filepath.FromSlash(rel))), noEnv)
			if got := agRuleOf(r); got != "write-protected-path" {
				t.Errorf("AC-5 Write %s is refused as %q; want write-protected-path", rel, got)
			}
		})
	}
}

// agFreeze writes a lock holding each file, with the hash of what is on disk.
func agFreeze(t *testing.T, root string, files ...string) {
	t.Helper()
	held := map[string]string{}
	for _, rel := range files {
		body := "package x\n"
		write(t, root, rel, body)
		sum := sha256.Sum256([]byte(body))
		held[rel] = hex.EncodeToString(sum[:])
	}
	raw, err := json.Marshal(map[string]any{"schema": "sdlc/tests-lock/1", "story": "A-1", "at": "2026-09-10T08:30:00Z", "files": held})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/state/tests.lock", string(raw))
}

// agHeldUnder checks the hook refuses cmd under one of rules.
func agHeldUnder(t *testing.T, criterion, root, cmd string, rules ...string) {
	t.Helper()
	r := agShell(t, root, cmd)
	if slices.Contains(rules, agRuleOf(r)) {
		return
	}
	t.Errorf("%s %q: %q; want it refused as %s", criterion, cmd, reasonOf(r), strings.Join(rules, " or "))
}

// agTwinRule is the rule the hook refuses twin under, the same command spelt
// from the project root. AGENTS.md is this story's own name, held under
// CLAUDE.md's rule (AC-5), so its twin is not asked.
func agTwinRule(t *testing.T, root, twin string) string {
	t.Helper()
	if strings.Contains(twin, "AGENTS") {
		return "protected-path-through-the-tool"
	}
	got := agRuleOf(agShell(t, root, twin))
	if got == "" {
		t.Fatalf("the twin %q goes through the hook, so the row compares with nothing", twin)
	}
	return got
}

// AC-14 (design review round 3, security note 3): through the hook, with a
// real home, a glob or a brace list in a home-spelt or absolute directory that
// could name the project's loop state, configuration, frozen test or a
// protected path is refused as the same command spelt from the project root.
// Security ran these through today's hook: each is allowed but for the frozen
// test, which is held by name.
func TestAC14AGlobInAHomeOrAbsoluteDirectoryReachesTheProjectsHeldFiles(t *testing.T) {
	root, home, _ := agAboveWorld(t)
	freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")
	code := slash(filepath.Join(home, "code"))
	for _, row := range []struct{ cmd, twin string }{
		{"echo x > ~/code/proj*/.sdlc/state/tests.lock", "echo x > .sdlc/state/tests.lock"},
		{"echo '{}' > ~/co*/project/.sdlc/state/tests.lock", "echo '{}' > .sdlc/state/tests.lock"},
		{"rm ~/co*/project/.sdlc/state/tests.lock", "rm .sdlc/state/tests.lock"},
		{"rm ~/co*/project/.sdlc/config.json", "rm .sdlc/config.json"},
		{"echo x > ~/code/proj*/.sdlc/config.json", "echo x > .sdlc/config.json"},
		{"rm ~/co*/project/.sdlc/state/active", "rm .sdlc/state/active"},
		{"echo x > " + slash(home) + "/co?e/project/.sdlc/state/active", "echo x > .sdlc/state/active"},
		{"rm " + code + "/proj?ct/.sdlc/config.json", "rm .sdlc/config.json"},
		{"echo x > " + code + "/[p]roject/.sdlc/state/tests.lock", "echo x > .sdlc/state/tests.lock"},
		{"echo x > ~/code/{project,x}/.sdlc/config.json", "echo x > .sdlc/config.json"},
		{"echo x > ~/code/proj*/user_stories.json", "echo x > user_stories.json"},
		{"echo x > ~/co*/project/internal/invoice_test.go", "echo x > internal/invoice_test.go"},
		{"rm " + code + "/proj*/internal/invoice_test.go", "rm internal/invoice_test.go"},
		{"echo x > ~/code/proj*/CLAUDE.md", "echo x > CLAUDE.md"},
		{"echo x > " + code + "/proj*/CLAUDE.md", "echo x > CLAUDE.md"},
		{"echo x > ~/code/proj*/AGENTS.md", "echo x > AGENTS.md"},
	} {
		t.Run("AC-14 "+row.cmd, func(t *testing.T) {
			rules := []string{agTwinRule(t, root, row.twin)}
			if strings.HasSuffix(row.twin, ".md") {
				rules = append(rules, agAboveRule)
			}
			agHeldUnder(t, "AC-14", root, row.cmd, rules...)
		})
	}
	for _, cmd := range []string{
		"echo x > ~/code/proj*/notes.md",
		"echo x > ~/code/other*/.sdlc/config.json",
		"echo x > ~/code/proj*/CLAUDE.md.bak",
		"rm ~/co*/other/README.md",
	} {
		t.Run("AC-14 "+cmd, func(t *testing.T) {
			switch got := agRuleOf(agShell(t, root, cmd)); got {
			case "loop-state-through-the-tool", "frozen-test-through-the-tool", "protected-path-through-the-tool":
				t.Errorf("AC-14 %q can name no held file in the project and was refused as %s", cmd, got)
			}
		})
	}
}

// AC-15 (design review round 3, security note 3 and red team hostile 2):
// through the hook, ln and install are refused where what they create or
// replace is held, as cp to the same place is, into a directory whose name has
// a dot too. Security and red team ran `ln -sf <outside>/CLAUDE.md .` and
// `install <outside>/CLAUDE.md .` through today's hook: both are allowed.
func TestAC15LnAndInstallAreRefusedThroughTheHookWhereCpToTheSamePlaceIs(t *testing.T) {
	root := loopProject(t)
	agFreeze(t, root, "internal/invoice_test.go", "pkg.v2/invoice_test.go")
	for _, dir := range []string{"sub", "docs", "docs.v2", "bin"} {
		write(t, root, dir+"/README.md", "x\n")
	}
	outside := t.TempDir()
	for _, name := range []string{"CLAUDE.md", "AGENTS.md", "tests.lock", "config.json", "invoice_test.go", "notes.md", "tool", "guide.md"} {
		write(t, outside, name, "x\n")
	}
	x := slash(outside)
	// Each row's twin is cp to the same place, which the hook refuses today;
	// the row is refused under the rule the twin is.
	for _, row := range []struct{ cmd, twin string }{
		{"ln -sf " + x + "/CLAUDE.md .", "cp " + x + "/CLAUDE.md ."},
		{"ln -s " + x + "/CLAUDE.md .", "cp " + x + "/CLAUDE.md ."},
		{"ln " + x + "/CLAUDE.md .", "cp " + x + "/CLAUDE.md ."},
		{"ln -t . " + x + "/CLAUDE.md", "cp " + x + "/CLAUDE.md ."},
		{"ln -sf " + x + "/CLAUDE.md", "cp " + x + "/CLAUDE.md ."},
		{"ln -s " + x + "/AGENTS.md sub/", "cp " + x + "/AGENTS.md sub/"},
		{"install " + x + "/CLAUDE.md .", "cp " + x + "/CLAUDE.md ."},
		{"install -t . " + x + "/AGENTS.md", "cp " + x + "/AGENTS.md ."},
		{"ln -s " + x + "/CLAUDE.md docs.v2", "cp " + x + "/CLAUDE.md docs.v2/"},
		{"install " + x + "/CLAUDE.md docs.v2", "cp " + x + "/CLAUDE.md docs.v2/"},
		{"ln -sf " + x + "/tests.lock .sdlc/state", "cp " + x + "/tests.lock .sdlc/state/"},
		{"ln -t .sdlc/state " + x + "/tests.lock", "cp " + x + "/tests.lock .sdlc/state/"},
		{"install " + x + "/config.json .sdlc", "cp " + x + "/config.json .sdlc/"},
		{"ln -sf " + x + "/invoice_test.go internal", "cp " + x + "/invoice_test.go internal/"},
		{"install " + x + "/invoice_test.go internal/", "cp " + x + "/invoice_test.go internal/"},
		{"ln -s " + x + "/invoice_test.go pkg.v2", "cp " + x + "/invoice_test.go pkg.v2/"},
		{"install " + x + "/invoice_test.go pkg.v2", "cp " + x + "/invoice_test.go pkg.v2/"},
	} {
		t.Run("AC-15 "+row.cmd, func(t *testing.T) {
			rules := []string{agTwinRule(t, root, row.twin)}
			// ln and install are never read whole (shellpolicy/outside.go), so
			// their source is not placed outside, and is read by its name before
			// the place it lands: a test name the freeze does not hold is a new
			// test. cp is read whole, and with no link on the way its source is
			// placed outside and let go, so the frozen test it replaces is what
			// refuses it. Under /tmp on Linux the twin was refused as the frozen
			// test and the row as a new one; on macOS the temporary directory is
			// reached through a link and both were new (CI on 2492472).
			if strings.Contains(row.cmd, "_test.go") {
				rules = append(rules, "no-new-test-after-the-freeze")
			}
			agHeldUnder(t, "AC-15", root, row.cmd, rules...)
		})
	}
	for _, cmd := range []string{
		"ln -s " + x + "/notes.md docs/notes.md",
		"ln -s README.md docs/readme.md",
		"ln -sf " + x + "/tool bin",
		"install -m 755 " + x + "/tool bin/",
		"ln -t docs " + x + "/guide.md",
	} {
		t.Run("AC-15 "+cmd, func(t *testing.T) {
			if r := agShell(t, root, cmd); !allows(r) {
				t.Errorf("AC-15 %q links or installs an ordinary file and was refused: %s", cmd, reasonOf(r))
			}
		})
	}
}

// agWriters are the programs AC-16 names, each with %s where the file it
// writes goes.
var agWriters = []string{
	"sort -o %s /etc/hosts",
	"sort --output=%s /etc/hosts",
	"uniq /etc/hosts %s",
	"gzip -df %s.gz",
	"gunzip -f %s.gz",
	"xz -df %s.xz",
	"unxz -f %s.xz",
	"base64 -d -o %s /etc/hosts",
	"iconv -f utf-8 -t utf-8 -o %s /etc/hosts",
}

// AC-16 (design review round 3, security note 3 and red team hostile 5):
// through the hook, a program that writes a file it is given by an option or
// an operand is refused where the file is held, as a redirect to it is; and
// where it is an instruction file above the project. Red team ran
// `sort -o CLAUDE.md /tmp/x` through the live hook: it is allowed.
func TestAC16AProgramThatWritesAFileItIsGivenIsRefusedThroughTheHookAsARedirectToItIs(t *testing.T) {
	root := loopProject(t)
	freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")
	for _, target := range []string{"CLAUDE.md", "sub/AGENTS.md", ".sdlc/config.json", ".sdlc/state/tests.lock", "internal/invoice_test.go"} {
		rule := agTwinRule(t, root, "echo x > "+target)
		for _, program := range agWriters {
			cmd := strings.ReplaceAll(program, "%s", target)
			t.Run("AC-16 "+cmd, func(t *testing.T) { agHeldUnder(t, "AC-16", root, cmd, rule) })
		}
	}
	for _, program := range agWriters {
		cmd := strings.ReplaceAll(program, "%s", "out/notes.txt")
		t.Run("AC-16 "+cmd, func(t *testing.T) {
			if r := agShell(t, root, cmd); !allows(r) {
				t.Errorf("AC-16 %q writes an ordinary file and was refused: %s", cmd, reasonOf(r))
			}
		})
	}
	above, home, _ := agAboveWorld(t)
	for _, target := range []string{"~/code/CLAUDE.md", "../AGENTS.md", slash(home) + "/CLAUDE.local.md"} {
		for _, program := range agWriters {
			cmd := strings.ReplaceAll(program, "%s", target)
			t.Run("AC-16 "+cmd, func(t *testing.T) { agHeldAsAbove(t, cmd, agShell(t, above, cmd)) })
		}
	}
}
