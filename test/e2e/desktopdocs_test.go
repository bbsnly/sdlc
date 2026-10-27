// US-108 AC-13: the Claude Desktop page.
//
// A person who runs the loop from Claude Desktop reads one page, and it has to
// cover what is different there: the plugin installed from the plugin browser,
// the slash commands that stand for a person's terminal commands, the two
// variables set in Desktop's local environment editor, the one command that
// still needs a terminal, and why Cowork is not supported. What it quotes the
// app showing is checked against what the hook shows, as CONTRIBUTING.md's
// quoted sandbox output is.

package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// desktopPage is the page's place under docs/.
const desktopPage = "claude-desktop.md"

func readDesktopPage(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "docs", desktopPage))
	if err != nil {
		t.Fatalf("docs/ has no Claude Desktop page: %v", err)
	}
	// A checkout with CRLF line endings reads the same.
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func TestAC13TheClaudeDesktopPageCoversWhatAPersonThereNeeds(t *testing.T) {
	parallel(t)
	page := readDesktopPage(t)

	t.Run("AC-13 install from the plugin browser", func(t *testing.T) {
		if !someParagraph(page, "plugin browser", "install") {
			t.Error("no paragraph says how to install the plugin from Claude Desktop's plugin browser")
		}
	})
	t.Run("AC-13 the person-only slash commands", func(t *testing.T) {
		for _, name := range []string{"approve", "unfreeze", "ack", "remap", "release", "assign"} {
			if !strings.Contains(page, "/sdlc:"+name) {
				t.Errorf("the page does not name /sdlc:%s", name)
			}
		}
	})
	t.Run("AC-13 SDLC_ENFORCE and SDLC_DEBUG_FILE in the local environment editor", func(t *testing.T) {
		for _, variable := range []string{"SDLC_ENFORCE", "SDLC_DEBUG_FILE"} {
			if !someParagraph(page, variable, "local environment editor") {
				t.Errorf("no paragraph says to set %s in Desktop's local environment editor", variable)
			}
		}
	})
	t.Run("AC-13 sdlc parallel still needs a terminal", func(t *testing.T) {
		if !someParagraph(page, "sdlc parallel", "terminal") {
			t.Error("no paragraph says that sdlc parallel still needs a terminal")
		}
	})
	t.Run("AC-13 why Cowork is not supported", func(t *testing.T) {
		// The story's own reason: Cowork refuses a plugin with a bin/
		// directory, and this one's launcher lives there.
		if !someParagraph(page, "Cowork", "bin/") {
			t.Error("no paragraph says why Cowork is not supported: it refuses a plugin with a bin/ directory")
		}
	})
}

// The slash commands the page names are commands a person can type: each is a
// skill the plugin ships.
func TestAC13TheClaudeDesktopPageNamesOnlySlashCommandsThePluginHas(t *testing.T) {
	parallel(t)
	page := readDesktopPage(t)
	named := regexp.MustCompile(`/sdlc:([a-z][a-z-]*)`).FindAllStringSubmatch(page, -1)
	if len(named) == 0 {
		t.Fatal("the page names no slash command")
	}
	for _, m := range named {
		if _, err := os.Stat(filepath.Join(moduleRoot(t), "plugin", "skills", m[1], "SKILL.md")); err != nil {
			t.Errorf("the page names /sdlc:%s, and the plugin has no such skill", m[1])
		}
	}
}

// AC-13's last clause: what the page quotes the app showing is what it shows.
// An approval typed as /sdlc:approve is the page's example; the sandbox's story
// is US-001, handed to a person as spec_unclear.
func TestAC13TheOutputTheClaudeDesktopPageQuotesIsWhatTheAppShows(t *testing.T) {
	parallel(t)
	page := readDesktopPage(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GateAnalysis); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}
	sb.works(t, tree, "", "", "escalate", "spec_unclear", "--message", "is a zero total refused too?")

	e := sb.types(t, sandboxOther, tree, "sdlc:approve", "US-001")
	shown := strings.TrimSpace(sb.normalise(strings.ReplaceAll(e.shown(), "\r\n", "\n")))
	if !e.blocked() || !strings.Contains(shown, "US-001  approved (spec_unclear)") {
		t.Fatalf("/sdlc:approve did not show the approval, so there is nothing to compare the page with: %q", e.raw)
	}
	for _, block := range fencedBlocks(page) {
		if strings.Contains(block, shown) {
			return
		}
	}
	t.Errorf("no fenced block of docs/%s holds what /sdlc:approve shows, which is:\n%s", desktopPage, shown)
}
