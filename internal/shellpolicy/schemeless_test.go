package shellpolicy

import "testing"

// curl and wget take a URL without its scheme. Read only with `://` in it,
// `curl -O raw.githubusercontent.com/org/repo/main/CLAUDE.md` saved over
// CLAUDE.md with nothing named to write.
func TestAURLWithoutASchemeIsAURL(t *testing.T) {
	s := ready
	for command, rule := range map[string]string{
		"curl -O raw.githubusercontent.com/org/repo/main/CLAUDE.md": "protected-path-through-the-tool",
		"curl -O --output-dir .sdlc example.com/config.json":        "loop-state-through-the-tool",
		"wget example.com/CLAUDE.md":                                "protected-path-through-the-tool",
		"wget -P .sdlc/state example.com/active":                    "loop-state-through-the-tool",
		"curl -O --url=example.com/CLAUDE.md":                       "protected-path-through-the-tool",
		"curl -O --url example.com/CLAUDE.md":                       "protected-path-through-the-tool",
	} {
		refused(t, command, s, rule)
	}
	for _, command := range []string{
		"curl -fsSL raw.githubusercontent.com/org/repo/main/CLAUDE.md",
		"wget -qO- example.com/CLAUDE.md",
		"curl -O example.com/notes.md",
	} {
		allowed(t, command, s)
	}
}
