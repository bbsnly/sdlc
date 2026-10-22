package plugin

// US-108: what the plugin has to carry for Claude Desktop, where a person
// installs it from the plugin browser and runs no installer.
//
// test/install runs the launcher's fetch against a mirror. These hold the
// files it rests on: the hook entries that call it, and the pins it checks.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// launcherCommand is the hook command for event, as every entry spells it.
func launcherCommand(event string) string {
	return `"${CLAUDE_PLUGIN_ROOT}/bin/sdlc-hook" ` + event
}

// matchesAll reports whether a hook matcher fires for every one of names: an
// empty matcher fires for all, and any other is a pattern the whole name has to
// match.
func matchesAll(t *testing.T, matcher string, names []string) []string {
	t.Helper()
	if matcher == "" || matcher == "*" {
		return nil
	}
	re, err := regexp.Compile(`^(?:` + matcher + `)$`)
	if err != nil {
		return []string{"the matcher " + matcher + " is not a pattern: " + err.Error()}
	}
	var missed []string
	for _, name := range names {
		if !re.MatchString(name) {
			missed = append(missed, name)
		}
	}
	return missed
}

// eventEntry checks that hooks.json runs the launcher on event, with a timeout,
// and returns the matchers it runs under.
func eventEntry(t *testing.T, event string) []string {
	t.Helper()
	hooks, err := Hooks(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	var matchers []string
	for _, entry := range hooks[event] {
		for _, h := range entry.Hooks {
			if h.Type != "command" || h.Command != launcherCommand(event) {
				continue
			}
			if h.Timeout <= 0 {
				t.Errorf("%s: the hook has no timeout; a hook that hangs hangs the session", event)
			}
			matchers = append(matchers, entry.Matcher)
		}
	}
	if len(matchers) == 0 {
		t.Fatalf("hooks.json does not run %s on %s", launcherCommand(event), event)
	}
	return matchers
}

// AC-9: the launcher fetches on the first SessionStart, so it has to be run
// then -- on a new session at least, which is what "startup" is.
func TestAC9TheLauncherRunsWhenASessionStarts(t *testing.T) {
	for _, matcher := range eventEntry(t, "SessionStart") {
		if missed := matchesAll(t, matcher, []string{"startup"}); len(missed) == 0 {
			return
		}
	}
	t.Error("no SessionStart entry fires when a new session starts up")
}

// AC-1: the hook makes a person's decision on UserPromptExpansion, so it has to
// be run for each of the plugin's commands. Claude Code's documentation does
// not say whether a plugin skill's command_name carries the plugin's name, so
// both spellings must be matched.
func TestAC1TheHookRunsOnTheExpansionOfEveryOneOfThePluginsCommands(t *testing.T) {
	var names []string
	for _, s := range personsSkills {
		names = append(names, s.name, "sdlc:"+s.name)
	}
	names = append(names, "init", "sdlc:init", "doctor", "sdlc:doctor")
	for _, matcher := range eventEntry(t, "UserPromptExpansion") {
		if missed := matchesAll(t, matcher, names); len(missed) > 0 {
			t.Errorf("the UserPromptExpansion matcher %q misses %v", matcher, missed)
		}
	}
}

// AC-9: the pins the launcher checks a download against. They are a release's
// checksums.txt, copied in by a person's commit after the release is built (the
// story's release-order decision), so they name one release and every archive
// it publishes.
//
// Nothing here asks that the release be the one plugin.json names. The tagged
// commit carries the previous release's pins, and release.yml runs this suite
// on that commit; a test that asked for a match would fail every release. The
// launcher fails closed on a mismatch instead (test/install).
func TestAC9ThePluginPinsTheChecksumsOfOneReleaseForEveryPlatformItShips(t *testing.T) {
	file, err := os.Open(filepath.Join(pluginDir, "checksums.txt"))
	if err != nil {
		t.Fatalf("the plugin carries no pins for the launcher to check a download against: %v", err)
	}
	defer file.Close()
	line := regexp.MustCompile(`^([0-9a-f]{64})  (\S+)$`)
	archive := regexp.MustCompile(`^sdlc_([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)_([a-z]+_[a-z0-9]+)\.(tar\.gz|zip)$`)
	releases := map[string]bool{}
	platforms := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for n := 1; scanner.Scan(); n++ {
		text := scanner.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		m := line.FindStringSubmatch(text)
		if m == nil {
			t.Errorf("checksums.txt:%d is not in the form a release's checksums.txt has: %q", n, text)
			continue
		}
		if a := archive.FindStringSubmatch(m[2]); a != nil {
			releases[a[1]] = true
			platforms[a[2]+"."+a[3]] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 {
		t.Fatalf("the pins name %d releases, want one: %v", len(releases), releases)
	}
	// What .goreleaser.yml publishes.
	for _, want := range []string{
		"darwin_arm64.tar.gz", "linux_amd64.tar.gz", "linux_arm64.tar.gz", "windows_amd64.zip", "windows_arm64.zip",
	} {
		if !platforms[want] {
			t.Errorf("the pins hold no checksum for the %s archive, so the launcher there can fetch nothing", want)
		}
	}
}
