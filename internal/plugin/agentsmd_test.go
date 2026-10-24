package plugin

// US-023 AC-8: an agent or skill that reads the contract, or that is told not
// to write the file holding it, names AGENTS.md as well as CLAUDE.md, so that
// in a project that keeps its instructions in AGENTS.md it reads the section
// from there and leaves that file alone.

import (
	"strings"
	"testing"
)

// agReaders are the agents and skills that read the SDLC Contract section.
var agReaders = []string{
	"architect", "code-reviewer", "human-advocate", "implementer", "perf", "researcher", "sdet", "security",
	"next", "consolidate",
}

// agBlocks is a body cut into paragraphs and list items, each with its white
// space folded: the unit a sentence naming a file lives in.
func agBlocks(body string) []string {
	var blocks []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, strings.Join(strings.Fields(strings.Join(cur, " ")), " "))
			cur = nil
		}
	}
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "|"):
			flush()
			cur = append(cur, trimmed)
		default:
			cur = append(cur, trimmed)
		}
	}
	flush()
	return blocks
}

func agComponents(t *testing.T) map[string]Component {
	t.Helper()
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]Component{}
	for _, c := range append(agents, skills...) {
		all[strings.TrimPrefix(c.Name, "sdlc:")] = c
	}
	return all
}

// AC-8: every block that points a reader at the SDLC Contract in CLAUDE.md
// also names AGENTS.md, and each reader has at least one such block, so that
// dropping CLAUDE.md from the text does not pass.
func TestAC8EveryAgentAndSkillThatReadsTheContractNamesAGENTSmd(t *testing.T) {
	all := agComponents(t)
	for _, name := range agReaders {
		c, ok := all[name]
		if !ok {
			t.Errorf("AC-8 the plugin ships no %s", name)
			continue
		}
		named := false
		for _, b := range agBlocks(c.Body) {
			if !strings.Contains(b, "SDLC Contract") {
				continue
			}
			if strings.Contains(b, "CLAUDE.md") && !strings.Contains(b, "AGENTS.md") {
				t.Errorf("AC-8 %s sends a reader to the contract in CLAUDE.md alone:\n%s", name, b)
			}
			named = named || strings.Contains(b, "AGENTS.md")
		}
		if !named {
			t.Errorf("AC-8 %s never says the contract may be in AGENTS.md", name)
		}
	}
}

// AC-8: the implementer's list of what it must not write names AGENTS.md.
func TestAC8TheImplementerIsToldNotToWriteAGENTSmd(t *testing.T) {
	c, ok := agComponents(t)["implementer"]
	if !ok {
		t.Fatal("the plugin ships no implementer")
	}
	found := false
	for _, b := range agBlocks(c.Body) {
		if strings.Contains(b, "Do not write, edit or delete") && strings.Contains(b, "CLAUDE.md") {
			found = true
			if !strings.Contains(b, "AGENTS.md") {
				t.Errorf("AC-8 the implementer's do-not-write list names CLAUDE.md and not AGENTS.md:\n%s", b)
			}
		}
	}
	if !found {
		t.Error("AC-8 the implementer has no do-not-write list that names the contract file")
	}
}
