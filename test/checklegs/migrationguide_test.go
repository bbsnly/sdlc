package checklegs

// US-119, the task: `./task migration-guide -- <from> <to>`. The program it
// runs is tested beside it, in internal/tools/migrationguide. What can only be
// seen here is the wiring, which is read from task's own dry run: the
// variables resolved as a real run resolves them, and nothing run.

import (
	"path/filepath"
	"strings"
	"testing"
)

const migrationGuideProgram = "./internal/tools/migrationguide"

// A word after the dashes reaches a shell in the task's command line. A tag
// is a git ref name and may hold a ";" or a "$(...)", so the task has to quote
// each word again before the shell reads it, as the sandbox task does; with the
// words pasted in raw, the second one of these would run `touch` and the first
// would end the command at the semicolon.
func TestAC1TheTaskHandsEachWordAfterTheDashesToTheProgramQuotedAsOne(t *testing.T) {
	// --verbose because a silent task's commands are otherwise left out of a
	// dry run.
	dry, err := taskWith(t, filepath.Join(repoRoot(t), "Taskfile.yml"),
		"--dry", "--verbose", "migration-guide", "--", "v1;touch-x", "$(touch x)")
	if err != nil {
		t.Fatalf("task --dry migration-guide: %v\n%s", err, dry)
	}
	var commands []string
	for line := range strings.SplitSeq(dry, "\n") {
		if strings.Contains(line, migrationGuideProgram) {
			commands = append(commands, line)
		}
	}
	if len(commands) != 1 {
		t.Fatalf("the task runs the program on %d lines, want one:\n%s", len(commands), dry)
	}
	_, words, _ := strings.Cut(commands[0], migrationGuideProgram)
	words = strings.TrimSpace(words)
	if !strings.HasSuffix(words, "'v1;touch-x' '$(touch x)'") {
		t.Errorf("the program is handed %q, want the two words, each in single quotes and in order", words)
	}
}
