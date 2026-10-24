package cli

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// checkProjectInstructions is doctor's check of the person's Project
// instructions setting, made only where Claude Code reaches the contract
// through it. It is not in checkOrder, as the story worktrees check is not, so
// a CLAUDE.md project's report keeps the checks it had.
const checkProjectInstructions = "project instructions"

// maxSettings is as much of settings.json as doctor reads.
const maxSettings = 1 << 20

// The words every line of the check ends with: doctor reads the person's user
// settings and nothing an organisation manages.
const (
	managedOK      = "; managed settings were not checked"
	managedSkipped = "; managed settings were not checked either"
)

// instructionsCheck reads Claude Code's Project instructions setting from the
// person's user settings, and never writes them. Claude Code reads AGENTS.md
// with no CLAUDE.md beside it only where the setting lets it. Every line is
// fixed text, and the only value it names is one of Claude Code's own: a
// value read from the file could carry a newline or a terminal escape that
// forges a line of the report a model reads.
func instructionsCheck(getenv func(string) string) check {
	dir, unplaced := settingsDir(getenv)
	if dir == "" {
		return instructionsSkipped("not checked: " + unplaced + ", so sdlc cannot tell which settings.json Claude Code reads")
	}
	value, unread := instructionFiles(filepath.Join(dir, "settings.json"))
	if unread != "" {
		return instructionsSkipped("not checked: settings.json in your Claude Code configuration directory " + unread)
	}
	const is = "Project instructions is "
	const in = " in your Claude Code user settings"
	switch value {
	case "":
		return check{Name: checkProjectInstructions, State: stateOK,
			Detail: "Project instructions is not set" + in + ", so Claude Code's default, claude-md-or-agents-md, reads AGENTS.md" + managedOK}
	case "claude-md-or-agents-md", "claude-md-and-agents-md":
		return check{Name: checkProjectInstructions, State: stateOK,
			Detail: is + value + in + ", so Claude Code reads AGENTS.md" + managedOK}
	case "claude-md", "managed-only":
		fix := "in Claude Code, run /config and set Project instructions to claude-md-or-agents-md"
		if value == "claude-md" {
			fix += ", or add a CLAUDE.md to this project with the line @AGENTS.md in it"
		}
		return check{Name: checkProjectInstructions, State: stateProblem,
			Detail: is + value + in + ", so Claude Code does not read AGENTS.md" + managedOK, Fix: fix}
	}
	return instructionsSkipped("not checked: Project instructions" + in + " has a value sdlc does not know")
}

func instructionsSkipped(detail string) check {
	return check{Name: checkProjectInstructions, State: stateSkipped, Detail: detail + managedSkipped}
}

// settingsDir is where Claude Code keeps the person's user settings:
// CLAUDE_CONFIG_DIR where it is set, or .claude in the home. "" with the
// reason where neither can be placed.
func settingsDir(getenv func(string) string) (dir, unplaced string) {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", "CLAUDE_CONFIG_DIR is not an absolute path"
		}
		return dir, ""
	}
	home := getenv("HOME")
	if runtime.GOOS == goosWindows && getenv("USERPROFILE") != "" {
		home = getenv("USERPROFILE")
	}
	if home == "" {
		return "", "there is no home directory"
	}
	return filepath.Join(home, ".claude"), ""
}

// instructionFiles is the setting's value, "" where it is not set, or why the
// file could not be read for it. Each layer is decoded alone, so nothing else
// of the file is kept, and no error text from it is.
func instructionFiles(path string) (value, unread string) {
	const unreadable, shape = "could not be read", "does not have the shape sdlc expects for Project instructions"
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ""
	}
	if err != nil || !info.Mode().IsRegular() {
		return "", unreadable
	}
	f, err := os.Open(path)
	if err != nil {
		return "", unreadable
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSettings+1))
	if err != nil || len(raw) > maxSettings {
		return "", unreadable
	}
	if !json.Valid(raw) {
		return "", "is not valid JSON"
	}
	// The newer key first, and the older where it is not there.
	plugins, okPlugins := member(raw, "pluginConfigs")
	plugin, okPlugin := member(plugins, "cc-plugin-agents-md@builtin")
	if plugin == nil {
		plugin, okPlugin = member(plugins, "agents-md@builtin")
	}
	options, okOptions := member(plugin, "options")
	setting, okSetting := member(options, "instructionFiles")
	if !okPlugins || !okPlugin || !okOptions || !okSetting {
		return "", shape
	}
	if setting == nil {
		return "", ""
	}
	if json.Unmarshal(setting, &value) != nil {
		return "", shape
	}
	return value, ""
}

// member is one key of a JSON object, nil where the object or the key is not
// there; ok is false where raw is something other than an object.
func member(raw json.RawMessage, key string) (json.RawMessage, bool) {
	if raw == nil {
		return nil, true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil, false
	}
	return object[key], true
}
