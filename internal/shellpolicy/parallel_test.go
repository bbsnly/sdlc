package shellpolicy

import (
	"strings"
	"testing"
)

// US-058 AC-4: `sdlc parallel` starts model sessions, optionally with tools
// pre-approved, so it is a person's command as `release` and `ack` are, always
// and not only while a story waits. A model that could run it would be
// starting work nobody asked for. The rule is parallel-is-a-human-decision, in
// both entry points: Inspect, for the session working a story, and
// HumanDecisions, for any other session of the project.

const parallelRule = "parallel-is-a-human-decision"

// The spellings are the evasions the rule was tried against: every one reads
// as the same command through the readings the other human decisions already
// have, and --max, which takes a value, is one the reading has to know, or
// `sdlc --max 2 parallel ...` hides the subcommand behind the number.
var parallelSpellings = []string{
	"sdlc parallel US-001 US-002",
	"/usr/local/bin/sdlc parallel US-001",
	`C:\Users\dev\AppData\Local\sdlc\bin\sdlc.exe parallel US-001`,
	"SDLC parallel US-001",
	"npx @bbsnly/sdlc parallel US-001",
	"go run ./cmd/sdlc parallel US-001",
	"go run -mod=mod ./cmd/sdlc parallel US-001",
	"sdlc --json parallel US-001",
	"sdlc --max 2 parallel US-001",
	"sdlc --unattended parallel US-001",
	"sdlc parallel --unattended US-001 US-002",
	"sdlc parallel US-001 US-002 --unattended",
	"sdlc parallel US-001 --max 3",
	"sdlc parallel --max 3 --unattended US-001",
	"env sdlc parallel US-001",
	"env FOO=1 sdlc parallel US-001",
	"env -S 'sdlc parallel US-001'",
	"flock /tmp/x sdlc parallel US-001",
	"script -qc 'sdlc parallel US-001' /dev/null",
	"command sdlc parallel US-001",
	"$'sdlc' parallel US-001",
	"bash -c 'sdlc parallel US-001'",
	`bash -c "sdlc parallel US-001"`,
	"sh -c 'cd x && sdlc parallel US-001'",
	"true && sdlc parallel US-001",
	"go test ./... ; sdlc parallel US-001",
	"go test ./... || sdlc parallel US-001",
	"(sdlc parallel US-001)",
	"cd /somewhere && (sdlc parallel US-001)",
	"bash <<EOF\nsdlc parallel US-001\nEOF",
	"nohup sdlc parallel US-001 &",
	"time sdlc parallel US-001",
	"sdlc parallel\tUS-001",
	"echo x | sdlc parallel US-001",
	"xargs sdlc parallel",
}

func TestAC4AModelSessionWorkingAStoryCannotRunParallel(t *testing.T) {
	for _, command := range parallelSpellings {
		refused(t, command, ready, parallelRule)
		refused(t, command, notReady, parallelRule)
	}
}

// With no story being worked on, only HumanDecisions asks. Parallel is refused
// there whatever the project holds, as a release or an ack is, and the answer
// to "does a story wait" is never asked for it.
func TestAC4AModelSessionWorkingNoStoryCannotRunParallelEither(t *testing.T) {
	for _, command := range parallelSpellings {
		for _, waits := range []bool{true, false} {
			asked := false
			f, ok := HumanDecisions(command, false, func(string) bool { asked = true; return waits })
			if !ok || f.Rule != parallelRule {
				t.Errorf("HumanDecisions(%q, a story waits = %v) = %q, %v; want %s", command, waits, f.Rule, ok, parallelRule)
			}
			if asked {
				t.Errorf("HumanDecisions(%q) asked whether a story waits: parallel is refused either way", command)
			}
		}
	}
}

func TestAC4ParallelIsRefusedInPowerShellToo(t *testing.T) {
	for _, command := range []string{
		"sdlc parallel US-001 US-002",
		"s`dlc parallel US-001",
		"& sdlc parallel US-001",
		"sdlc parallel --unattended US-001; Write-Output done",
	} {
		if f, ok := HumanDecisions(command, true, aStoryWaits); !ok || f.Rule != parallelRule {
			t.Errorf("HumanDecisions(%q, powershell) = %q, %v; want %s", command, f.Rule, ok, parallelRule)
		}
		if f, ok := Inspect(command, State{CommitReady: true, PowerShell: true}); !ok || f.Rule != parallelRule {
			t.Errorf("Inspect(%q, powershell) = %q, %v; want %s", command, f.Rule, ok, parallelRule)
		}
	}
}

func TestAC4TheRefusalSaysWhatToDoNext(t *testing.T) {
	f := refused(t, "sdlc parallel US-001", ready, parallelRule)
	for _, word := range []string{"person", "terminal"} {
		if !strings.Contains(strings.ToLower(f.Route), word) {
			t.Errorf("the route does not say %q: %s", word, f.Route)
		}
	}
	if f.Message() == "" {
		t.Error("no message for the assistant")
	}
}

// Naming the command is not running it, and a program with the word in its
// name is another program.
func TestAC4OnlyRunningParallelIsRefused(t *testing.T) {
	for _, command := range []string{
		"echo parallel",
		`git commit -m "sdlc parallel US-001 runs the stories in worktrees"`,
		`sdlc escalate spec_unclear --message "a person runs sdlc parallel US-001 US-002"`,
		"sdlc status --json",
		"sdlc log --since HEAD~2",
		"go test -parallel 4 ./...",
		"go test ./internal/parallel/...",
		"cat docs/parallel.md",
		"xargs -P 4 -n 1 echo",
		"parallel echo ::: a b",
		"go test ./... # then sdlc parallel US-001",
	} {
		allowed(t, command, ready)
		if f, ok := HumanDecisions(command, false, aStoryWaits); ok {
			t.Errorf("HumanDecisions(%q) refused it under %s", command, f.Rule)
		}
	}
}

// The flag the command takes with a value is one the subcommand search knows,
// or `sdlc --max 2 parallel` reads as the command `2`.
func TestAC4TheMaxFlagIsOneThatTakesAValue(t *testing.T) {
	if !ValueFlags["--max"] {
		t.Error("ValueFlags has no --max, so a subcommand after it is read as the number")
	}
}
