package shellpolicy

// US-112 AC-17, design review round 2 (security, N1): a named pipe made at the
// trace's name before its first record leaves nothing recorded for the story,
// and a reader that opens it waits for a writer that never comes: `sdlc trace`
// hangs, and so does each SubagentStop's read of the hand-back. mkfifo and
// mknod make a file at the name they are given, as touch does, so they are
// writes to it. The gate record is under the same rule, and has been open to
// both. Each row with a relative path was run under bash and zsh on macOS and
// made the pipe; Windows has neither program.

import "testing"

// pipesAt is every row that makes a named pipe at the story file named name.
func pipesAt(name string) []string {
	at := ".sdlc/stories/A-1/" + name
	return []string{
		"mkfifo " + at,
		"mkfifo -m 600 " + at,
		"/usr/bin/mkfifo " + at,
		"command mkfifo " + at,
		"cd .sdlc/stories/A-1 && mkfifo " + name,
		"mknod " + at + " p",
		"mknod -m 600 " + at + " p",
		"mkfifo /home/dev/repo/" + at,
	}
}

func TestAC17ANamedPipeAtTheTracesNameIsRefused(t *testing.T) {
	for _, command := range pipesAt("trace.jsonl") {
		t.Run("AC-17 "+command, func(t *testing.T) {
			refused(t, command, ready, traceRule)
		})
	}
}

func TestAC17ANamedPipeAtTheGateRecordsNameIsRefused(t *testing.T) {
	for _, command := range pipesAt("gate-record.json") {
		t.Run("AC-17 "+command, func(t *testing.T) {
			refused(t, command, ready, "loop-state-through-the-tool")
		})
	}
}

// A pipe elsewhere is a pipe, and is not refused.
func TestAC17ANamedPipeElsewhereIsLeftAlone(t *testing.T) {
	for _, command := range []string{
		"mkfifo /tmp/fifo",
		"mknod /tmp/fifo p",
		"mkfifo build/progress",
	} {
		allowed(t, command, ready)
	}
}
