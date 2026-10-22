package hook

// US-108 AC-18 through the hook: a file tool's path spelled through macOS's
// /System/Volumes/Data, or in another letter case where the filesystem folds
// it, is the path it names, to the rules on the project's files and to the
// rule on the person's ~/.claude. The researcher fed a Write to
// /System/Volumes/Data<worktree>/internal/hook/hook.go to the built hook: it
// was refused as write-outside-repository where the plain path is refused as
// orchestrator-delegates, and an implementer's ordinary write there would be
// refused as outside (ANALYSIS.md 7.8). internal/pathrules holds the unit.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// firmlink is the prefix under which macOS's data volume shows every path of
// the system volume's firmlinked directories: /Users, /private and the rest.
const firmlink = "/System/Volumes/Data"

// throughFirmlink is p spelled through the firmlink, and skips the test where
// that spelling is not the same directory on this machine.
func throughFirmlink(t *testing.T, p string) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("/System/Volumes/Data is a macOS firmlink; no other platform has it")
	}
	spelled := firmlink + p
	there, err := os.Stat(spelled)
	if err != nil {
		t.Skipf("%s is not reachable through the firmlink on this machine: %v", p, err)
	}
	if here, err := os.Stat(p); err != nil || !os.SameFile(here, there) {
		t.Skipf("%s is not the same directory as %s on this machine", spelled, p)
	}
	return spelled
}

// foldsCase skips the test on a filesystem that keeps case, where the path in
// capitals is another path and the rules are right to read it so.
func foldsCase(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("this filesystem keeps case, so the path in capitals is another one")
	}
}

// ruleTag is the rule a refusal names, "[rule-name]" at its end.
var ruleTag = regexp.MustCompile(`\[([a-z0-9-]+)\]\s*$`)

// verdictOf is what a reply decided, in a form two replies compare on: the
// rule a refusal names, or "allowed".
func verdictOf(r reply) string {
	if !denied(r) {
		return "allowed"
	}
	if m := ruleTag.FindStringSubmatch(reasonOf(r)); m != nil {
		return "refused under " + m[1]
	}
	return "refused: " + reasonOf(r)
}

// fileCall is a file tool's PreToolUse call from session, run in cwd, writing
// path.
func fileCall(session, cwd, tool, agent, path string) string {
	input := map[string]string{"file_path": path}
	if tool == "Edit" {
		input["old_string"], input["new_string"] = "a", "b"
	}
	e := map[string]any{
		"hook_event_name": "PreToolUse", "session_id": session,
		"tool_name": tool, "cwd": cwd, "tool_input": input,
	}
	if agent != "" {
		e["agent_type"] = agent
	}
	return mustMarshal(e)
}

// ac18Rows are writes inside the project that the rules tell apart: the
// orchestrator's write it delegates, an implementer's ordinary write, the
// project's contract, and the loop's own configuration and backlog.
var ac18Rows = []struct{ agent, rel string }{
	{"", "internal/invoice.go"},
	{"sdlc-implementer", "internal/invoice.go"},
	{"sdlc-implementer", "CLAUDE.md"},
	{"sdlc-implementer", ".sdlc/config.json"},
	{"sdlc-implementer", "user_stories.json"},
}

// ac18ProjectSpellingsAgree asks each row with the plain path and with the
// path respelled, from the session holding the story, with and without
// CLAUDE_PROJECT_DIR, and with the call run from the plain project and from
// the respelled one, and wants one verdict for both.
func ac18ProjectSpellingsAgree(t *testing.T, root, respelledRoot, spelling string) {
	t.Helper()
	plainVerdicts := map[string]bool{}
	for _, getenv := range []struct {
		name string
		get  func(string) string
	}{
		{"no CLAUDE_PROJECT_DIR", noEnv},
		{"CLAUDE_PROJECT_DIR set", env(map[string]string{"CLAUDE_PROJECT_DIR": root})},
	} {
		for _, row := range ac18Rows {
			for _, tool := range []string{"Write", "Edit"} {
				plainPath := filepath.Join(root, filepath.FromSlash(row.rel))
				respelled := filepath.Join(respelledRoot, filepath.FromSlash(row.rel))
				want := verdictOf(call(t, fileCall(working, root, tool, row.agent, plainPath), getenv.get))
				plainVerdicts[want] = true
				for _, c := range []struct{ cwd, path, how string }{
					{root, respelled, "the path " + spelling},
					{respelledRoot, plainPath, "the call run from the project " + spelling},
					{respelledRoot, respelled, "both " + spelling},
				} {
					got := verdictOf(call(t, fileCall(working, c.cwd, tool, row.agent, c.path), getenv.get))
					if got != want {
						t.Errorf("AC-18 (%s): %s by %q of %s, with %s, was %s; the plain path was %s",
							getenv.name, tool, row.agent, row.rel, c.how, got, want)
					}
				}
			}
		}
	}
	// The comparison means something only if the plain paths were told apart.
	for _, need := range []string{"allowed", "refused under orchestrator-delegates", "refused under write-protected-path"} {
		if !plainVerdicts[need] {
			t.Errorf("AC-18: no plain row was %s, so the rows no longer tell the rules apart: %v", need, plainVerdicts)
		}
	}
}

func TestAC18AFileToolPathThroughTheDataVolumeFirmlinkIsTheProjectPathItNames(t *testing.T) {
	root := loopProject(t)
	ac18ProjectSpellingsAgree(t, root, throughFirmlink(t, root), "through /System/Volumes/Data")
}

func TestAC18AFileToolPathInAnotherCaseIsTheProjectPathItNames(t *testing.T) {
	foldsCase(t)
	root := loopProject(t)
	ac18ProjectSpellingsAgree(t, root, strings.ToUpper(root), "in capitals")
}

func TestAC18AFileToolPathThroughTheFirmlinkInAnotherCaseIsTheProjectPathItNames(t *testing.T) {
	root := loopProject(t)
	through := throughFirmlink(t, root)
	ac18ProjectSpellingsAgree(t, root, strings.ToUpper(through), "through /SYSTEM/VOLUMES/DATA in capitals")
}

// ac18ConfigRefused wants each write into the person's ~/.claude, spelled with
// home respelled, refused under claude-config-is-the-persons from the session
// holding the story and from a second session of the project.
func ac18ConfigRefused(t *testing.T, config, respelledHome, spelling string) {
	t.Helper()
	root := loopProject(t)
	const rule = "refused under claude-config-is-the-persons"
	for kind, path := range claudeConfigTargets(config) {
		rel, err := filepath.Rel(filepath.Dir(config), path)
		if err != nil {
			t.Fatal(err)
		}
		// Every caller runs only where the filesystem folds case, so the
		// configuration's own names in capitals are the same files too.
		for _, p := range []string{filepath.Join(respelledHome, rel), filepath.Join(respelledHome, strings.ToUpper(rel))} {
			for _, tool := range []string{"Write", "Edit"} {
				for _, session := range []string{working, "session-b"} {
					if got := verdictOf(call(t, fileCall(session, root, tool, "", p), noEnv)); got != rule {
						t.Errorf("AC-18: %s to the %s file of ~/.claude %s (%s), from session %q, was %s, not %s",
							tool, kind, spelling, p, session, got, rule)
					}
				}
			}
		}
	}
}

func TestAC18AWriteToClaudeConfigThroughTheDataVolumeFirmlinkIsRefusedAsTheConfig(t *testing.T) {
	config := claudeHome(t)
	home := filepath.Dir(config)
	ac18ConfigRefused(t, config, throughFirmlink(t, home), "through /System/Volumes/Data")
}

func TestAC18AWriteToClaudeConfigInAnotherCaseIsRefusedAsTheConfig(t *testing.T) {
	foldsCase(t)
	config := claudeHome(t)
	ac18ConfigRefused(t, config, strings.ToUpper(filepath.Dir(config)), "in capitals")
}

func TestAC18AWriteToClaudeConfigThroughTheFirmlinkInAnotherCaseIsRefusedAsTheConfig(t *testing.T) {
	config := claudeHome(t)
	through := throughFirmlink(t, filepath.Dir(config))
	ac18ConfigRefused(t, config, strings.ToUpper(through), "through /SYSTEM/VOLUMES/DATA in capitals")
}
