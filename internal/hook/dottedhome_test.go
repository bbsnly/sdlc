package hook

// US-108 AC-7, security B2 from design review round 2, which no frozen row
// held until red-team's round-4 mutation found it: `cp x/.claude.json` into
// the person's home spelled out in full. A home whose last name has a dot,
// first.last, reads as a file to the copy's destination, so the configuration
// file the copy makes there, <home>/.claude.json, was never asked about. The
// rows put HOME at such a home under t.TempDir() and copy, install and link
// .claude.json into it, from both sessions.

import (
	"os"
	"path/filepath"
	"testing"
)

// dottedHome points HOME (and USERPROFILE, for Windows) at a home named
// first.last inside a temp dir, and returns it slash-separated, as a command
// spells it.
func dottedHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	home := filepath.Join(dir, "first.last")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.ToSlash(home)
}

func TestAC7ACopyOrALinkOfClaudeJSONIntoAHomeWhoseNameHasADotIsRefused(t *testing.T) {
	const rule = "[claude-config-is-the-persons]"
	home := dottedHome(t)
	root := loopProject(t)
	into := ` "` + home + `"`
	for _, cmd := range []string{
		"cp /tmp/x/.claude.json" + into,
		"install /tmp/x/.claude.json" + into,
		"ln -s /tmp/x/.claude.json" + into,
	} {
		for _, session := range []string{working, "session-b"} {
			r := call(t, inSession(command(root, "", cmd), session), noEnv)
			if !denied(r) || !containsRule(reasonOf(r), rule) {
				t.Errorf("AC-7 from session %q, %s was not refused as %s: %q", session, cmd, rule, reasonOf(r))
			}
		}
	}
	// A file of another name copied into that home is not the configuration.
	for _, session := range []string{working, "session-b"} {
		if r := call(t, inSession(command(root, "", "cp /tmp/x/notes.txt"+into), session), noEnv); !allows(r) {
			t.Errorf("AC-7 from session %q, a copy of notes.txt into the home was refused: %q", session, reasonOf(r))
		}
	}
}

// containsRule reports whether a refusal's reason ends in the rule's name.
func containsRule(reason, rule string) bool {
	return len(reason) >= len(rule) && reason[len(reason)-len(rule):] == rule
}
