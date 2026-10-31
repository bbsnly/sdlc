package scaffold

// US-112.2 AC-6: `sdlc init` writes commands.freeze_check empty, whatever stack
// it found, since choosing a project's check is a person's edit, and says what
// the key is in the `_commands` block (decision 2) and not in a key of its own
// under `commands`, where every key is read as a command.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
)

func TestAC6InitWritesTheFreezeCheckEmptyWithALineInTheCommandsBlockForEveryStackAndNone(t *testing.T) {
	for _, stack := range ccStacks {
		t.Run(stack.name, func(t *testing.T) {
			_, raw := ccInit(t, stack.files)

			var doc struct {
				Commands map[string]*string `json:"commands"`
			}
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			got, written := doc.Commands["freeze_check"]
			if !written {
				t.Fatalf("init does not write commands.freeze_check:\n%s", raw)
			}
			if got == nil || *got != "" {
				t.Errorf("init writes commands.freeze_check as %v; it stays empty until a person chooses it", got)
			}

			members, err := ccMembers(raw)
			if err != nil {
				t.Fatal(err)
			}
			lines, ok := ccBlockOf(members, "commands")
			if !ok {
				t.Fatal(`there is no "_commands" block`)
			}
			if !strings.Contains(strings.Join(lines, " "), "freeze_check") {
				t.Errorf("the _commands block does not say what freeze_check is:\n%s", strings.Join(lines, "\n"))
			}

			if _, err := config.Parse(raw); err != nil {
				t.Errorf("the configuration init writes does not read back: %v", err)
			}
		})
	}
}
