package shellpolicy

import "testing"

// A download writes where it is told to, and a URL is not a path on this
// machine: fetching another project's CLAUDE.md or configuration to compare
// was a write to this one's.
func TestADownloadWritesWhereItIsTold(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"curl -fsSL https://raw.githubusercontent.com/org/repo/main/CLAUDE.md",
		"curl -XPOST https://example.com/CLAUDE.md",
		"curl -uOli https://example.com/CLAUDE.md",
		"curl -H X-Other https://example.com/.sdlc/config.json",
		"curl https://example.com/CLAUDE.md --output",
		"curl https://example.com/CLAUDE.md -o",
		"wget -qO- https://example.com/.sdlc/config.json",
		"wget -qO- https://example.com/CLAUDE.md",
		"wget https://example.com/",
		"wget https://example.com/.sdlc/config.json",
		"wget -q https://example.com/y.tgz",
	} {
		allowed(t, command, s)
	}
	// Saved outside the project, under any name, is nothing of the project's.
	placed := s
	placed.Resolve = func(word string) string {
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	for _, command := range []string{
		"curl -o /tmp/claude.md https://example.com/CLAUDE.md",
		"curl -O --output-dir /tmp https://example.com/CLAUDE.md",
		"wget -O /tmp/x https://example.com/CLAUDE.md",
		"wget --output-document /tmp/x https://example.com/CLAUDE.md",
		"wget -P /tmp https://example.com/CLAUDE.md",
	} {
		allowed(t, command, placed)
	}

	for command, rule := range map[string]string{
		"curl -O https://example.com/CLAUDE.md":                         "protected-path-through-the-tool",
		"curl -sSLO https://example.com/CLAUDE.md":                      "protected-path-through-the-tool",
		"curl --remote-name https://example.com/CLAUDE.md?raw=1":        "protected-path-through-the-tool",
		"curl --remote-name-all https://example.com/CLAUDE.md":          "protected-path-through-the-tool",
		"curl -O --output-dir .sdlc https://example.com/config.json":    "loop-state-through-the-tool",
		"curl --output-dir=.sdlc -O https://example.com/config.json":    "loop-state-through-the-tool",
		"curl -o CLAUDE.md https://example.com/x":                       "protected-path-through-the-tool",
		"curl --output CLAUDE.md https://example.com/x":                 "protected-path-through-the-tool",
		"curl --output=CLAUDE.md https://example.com/x":                 "protected-path-through-the-tool",
		"curl -D .sdlc/state/active https://example.com/x":              "loop-state-through-the-tool",
		"curl --dump-header .sdlc/state/active https://example.com":     "loop-state-through-the-tool",
		"curl -c .sdlc/state/active https://example.com/x":              "loop-state-through-the-tool",
		"curl --cookie-jar=.sdlc/state/active https://example.com":      "loop-state-through-the-tool",
		"curl -o internal/calc/add_test.go https://example.com/x":       "frozen-test-through-the-tool",
		"wget https://example.com/CLAUDE.md":                            "protected-path-through-the-tool",
		"wget -P .sdlc https://example.com/config.json":                 "loop-state-through-the-tool",
		"wget --directory-prefix=.sdlc https://example.com/config.json": "loop-state-through-the-tool",
		"wget -O CLAUDE.md https://example.com/x":                       "protected-path-through-the-tool",
		"wget --output-document CLAUDE.md https://example.com/x":        "protected-path-through-the-tool",
		"wget -o .sdlc/state/active https://example.com/x":              "loop-state-through-the-tool",
		"wget -a .sdlc/state/active https://example.com/x":              "loop-state-through-the-tool",
		"wget --output-file=.sdlc/state/active https://example.com":     "loop-state-through-the-tool",
		"wget --append-output .sdlc/state/active https://example.com":   "loop-state-through-the-tool",
	} {
		refused(t, command, s, rule)
	}
}
