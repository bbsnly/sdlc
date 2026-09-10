// Package plugin reads the Claude Code plugin this repository ships.
//
// It exists so that the parts of the plugin that must agree -- the manifest,
// the agents, the skills, the hooks and the commands they call -- can be
// checked against each other by a test rather than by remembering. `claude
// plugin validate` checks the manifest against its schema; this checks the
// manifest against the rest of the repository, which is where the drift is.
//
// The frontmatter parser here is deliberately small. Agent and skill
// frontmatter is a handful of scalar keys, and a YAML dependency to read them
// would be the largest thing in go.mod.
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manifest is a plugin's own description of itself.
type Manifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Homepage    string   `json:"homepage"`
	License     string   `json:"license"`
	Keywords    []string `json:"keywords"`
}

// Marketplace is the catalogue a repository publishes.
type Marketplace struct {
	Name    string `json:"name"`
	Owner   Owner  `json:"owner"`
	Plugins []struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Description string `json:"description"`
	} `json:"plugins"`
}

// Owner is who to contact about the marketplace.
type Owner struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Component is an agent or a skill: a Markdown file with frontmatter.
type Component struct {
	Name        string
	Description string
	Path        string // repository-relative, slash-separated
	Body        string
	Front       map[string]string
}

// LoadManifest reads plugin/.claude-plugin/plugin.json.
func LoadManifest(pluginDir string) (*Manifest, error) {
	var m Manifest
	if err := readJSON(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadMarketplace reads .claude-plugin/marketplace.json from a repository root.
func LoadMarketplace(root string) (*Marketplace, error) {
	var m Marketplace
	if err := readJSON(filepath.Join(root, ".claude-plugin", "marketplace.json"), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Agents reads plugin/agents/*.md.
func Agents(pluginDir string) ([]Component, error) {
	return components(filepath.Join(pluginDir, "agents"), "*.md")
}

// Skills reads plugin/skills/*/SKILL.md.
func Skills(pluginDir string) ([]Component, error) {
	return components(filepath.Join(pluginDir, "skills"), filepath.Join("*", "SKILL.md"))
}

// Hooks reads plugin/hooks/hooks.json as the raw structure Claude Code sees.
func Hooks(pluginDir string) (map[string][]HookMatcher, error) {
	var file struct {
		Hooks map[string][]HookMatcher `json:"hooks"`
	}
	if err := readJSON(filepath.Join(pluginDir, "hooks", "hooks.json"), &file); err != nil {
		return nil, err
	}
	return file.Hooks, nil
}

// HookMatcher is one "when this tool, run this" entry.
type HookMatcher struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	} `json:"hooks"`
}

func components(dir, pattern string) ([]Component, error) {
	paths, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	out := make([]Component, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		front, body, err := splitFrontmatter(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, Component{
			Name:        front["name"],
			Description: front["description"],
			Path:        filepath.ToSlash(path),
			Body:        body,
			Front:       front,
		})
	}
	return out, nil
}

// splitFrontmatter reads the leading --- block as scalar key/value pairs. Keys
// whose value is a list or a nested block are recorded with an empty value:
// nothing here needs to read one, and pretending to parse YAML would be worse
// than not parsing it.
func splitFrontmatter(text string) (map[string]string, string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, "", fmt.Errorf("no frontmatter: a component starts with a --- block")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return nil, "", fmt.Errorf("the frontmatter block is never closed")
	}
	block := text[4 : 4+end]
	body := text[4+end+5:]

	front := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, " ") ||
			strings.HasPrefix(line, "-") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		front[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return front, body, nil
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
