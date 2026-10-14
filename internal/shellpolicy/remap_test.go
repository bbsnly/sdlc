package shellpolicy

import (
	"strings"
	"testing"
)

// US-089: `sdlc remap` puts a landed story's map change on trunk, replaces a
// section there or discards the change. Each is a choice between two stories'
// words, so it is a person's command as `approve`, `unfreeze` and `ack` are:
// always, whatever its flags and whatever story it names, and not only while
// a story waits. The rule is remap-is-a-human-decision, in both entry points:
// Inspect, for the session working a story, and HumanDecisions, for any other
// session of the project.

const remapRule = "remap-is-a-human-decision"

// remapForms is every form of the command: the rule keys on the subcommand,
// and no flag or id makes it a session's.
var remapForms = []string{
	"remap US-001",
	"remap US-001 --replace",
	"remap US-001 --drop",
	"remap --replace US-001",
	"remap --drop US-001",
	"remap US-001 --replace=true",
	"remap US-001 --drop=true",
	"remap US-001 --replace --drop",
	"remap US-404",
	"remap",
	"remap --help",
	"remap ../US-001 --drop",
	"remap US-001 --json",
	"--json remap US-001 --drop",
}

// remapWrappers are the spellings the rule was tried against. Each holds one
// %s where the sdlc arguments go, so that the same wrapper is tried with
// `ack --through HEAD`, which is refused already, and with each remap form: a
// wrapper that hides remap and not ack is the bug.
var remapWrappers = []string{
	"sdlc %s",
	"/usr/local/bin/sdlc %s",
	`C:\Users\dev\AppData\Local\sdlc\bin\sdlc.exe %s`,
	"SDLC %s",
	"npx @bbsnly/sdlc %s",
	"go run ./cmd/sdlc %s",
	"go run -mod=mod ./cmd/sdlc %s",
	"env sdlc %s",
	"env FOO=1 sdlc %s",
	"env -S 'sdlc %s'",
	"flock /tmp/x sdlc %s",
	"script -qc 'sdlc %s' /dev/null",
	"command sdlc %s",
	"$'sdlc' %s",
	"bash -c 'sdlc %s'",
	`bash -c "sdlc %s"`,
	"sh -c 'cd x && sdlc %s'",
	"true && sdlc %s",
	"go test ./... ; sdlc %s",
	"go test ./... || sdlc %s",
	"(sdlc %s)",
	"cd /somewhere && (sdlc %s)",
	"bash <<EOF\nsdlc %s\nEOF",
	"nohup sdlc %s &",
	"time sdlc %s",
	"echo x | sdlc %s",
}

func remapSpelt(wrapper, args string) string {
	return strings.Replace(wrapper, "%s", args, 1)
}

// The wrappers are ones the reading already sees through: each refuses `ack`.
// Were one not, the rows below would prove nothing about remap.
func remapWrappersHold(t *testing.T) {
	t.Helper()
	for _, wrapper := range remapWrappers {
		command := remapSpelt(wrapper, "ack --through HEAD")
		if _, ok := Inspect(command, ready); !ok {
			t.Errorf("Inspect lets through %q: the wrapper is not one the reading sees through", command)
		}
		if _, ok := HumanDecisions(command, false, func(string) bool { return false }); !ok {
			t.Errorf("HumanDecisions lets through %q: the wrapper is not one the reading sees through", command)
		}
	}
}

func TestAC6AModelSessionWorkingAStoryCannotRunRemapInAnyFormOrSpelling(t *testing.T) {
	remapWrappersHold(t)
	for _, wrapper := range remapWrappers {
		for _, form := range remapForms {
			command := remapSpelt(wrapper, form)
			refused(t, command, ready, remapRule)
			refused(t, command, notReady, remapRule)
		}
	}
	refused(t, "sdlc remap\tUS-001", ready, remapRule)
	refused(t, "xargs sdlc remap", ready, remapRule)
}

// With no story being worked on, only HumanDecisions asks. Remap is refused
// there whatever the project holds, as an ack is, and the answer to "does a
// story wait" is never asked for it: a landed story is not one that waits.
func TestAC6AModelSessionWorkingNoStoryCannotRunRemapEither(t *testing.T) {
	for _, wrapper := range remapWrappers {
		for _, form := range remapForms {
			command := remapSpelt(wrapper, form)
			for _, waits := range []bool{true, false} {
				asked := false
				f, ok := HumanDecisions(command, false, func(string) bool { asked = true; return waits })
				if !ok || f.Rule != remapRule {
					t.Errorf("HumanDecisions(%q, a story waits = %v) = %q, %v; want %s", command, waits, f.Rule, ok, remapRule)
				}
				if asked {
					t.Errorf("HumanDecisions(%q) asked whether a story waits: remap is refused either way", command)
				}
			}
		}
	}
}

func TestAC6RemapIsRefusedInPowerShellToo(t *testing.T) {
	for _, command := range []string{
		"sdlc remap US-001",
		"sdlc remap US-001 --replace",
		"sdlc remap US-001 --drop",
		"s`dlc remap US-001 --drop",
		"& sdlc remap US-001 --replace",
		"sdlc remap US-001 --drop; Write-Output done",
	} {
		if f, ok := HumanDecisions(command, true, aStoryWaits); !ok || f.Rule != remapRule {
			t.Errorf("HumanDecisions(%q, powershell) = %q, %v; want %s", command, f.Rule, ok, remapRule)
		}
		if f, ok := Inspect(command, State{CommitReady: true, PowerShell: true}); !ok || f.Rule != remapRule {
			t.Errorf("Inspect(%q, powershell) = %q, %v; want %s", command, f.Rule, ok, remapRule)
		}
	}
}

func TestAC6TheRemapRefusalSaysWhatToDoNext(t *testing.T) {
	f := refused(t, "sdlc remap US-001 --replace", ready, remapRule)
	for _, word := range []string{"person", "terminal"} {
		if !strings.Contains(strings.ToLower(f.Route), word) {
			t.Errorf("the route does not say %q: %s", word, f.Route)
		}
	}
	if f.Message() == "" {
		t.Error("no message for the assistant")
	}
}

// Naming the command is not running it, and the bookkeeper's own command,
// which stores the sections and decides nothing, is a session's to run.
func TestAC1OnlyRunningRemapIsRefusedAndStoringSectionsIsNot(t *testing.T) {
	if f, ok := Inspect("sdlc remap US-001", ready); !ok || f.Rule != remapRule {
		t.Errorf("Inspect(sdlc remap US-001) = %q, %v; want %s", f.Rule, ok, remapRule)
	}
	for _, command := range []string{
		"sdlc codemap write",
		"sdlc codemap write --json",
		"sdlc codemap write <<'SDLC_DOCUMENT'\n<!-- sdlc:add -->\n## refund.go\n\nKeeps refunds.\nSDLC_DOCUMENT",
		"sdlc codemap",
		"echo remap",
		`git commit -m "sdlc remap US-001 puts the map change on trunk"`,
		`sdlc escalate spec_unclear --message "a person runs sdlc remap US-001 --drop"`,
		"sdlc status --json",
		"cat docs/troubleshooting.md",
		"remap US-001",
		"go test ./... # then sdlc remap US-001",
	} {
		allowed(t, command, ready)
		for _, waits := range []bool{true, false} {
			if f, ok := HumanDecisions(command, false, func(string) bool { return waits }); ok {
				t.Errorf("HumanDecisions(%q, a story waits = %v) refused it under %s", command, waits, f.Rule)
			}
		}
	}
}
