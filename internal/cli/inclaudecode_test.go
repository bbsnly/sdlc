package cli

// US-108 AC-15: a person's commands refuse in a process whose environment
// carries Claude Code's markers. A model's Bash or PowerShell has them, so has
// any script, shell or interpreter it starts, and so has a `!` command a person
// types in a session; a terminal outside Claude Code has none. The shell rules
// read the command line and miss what they cannot read (`arch`, `vim -c`, a
// script on standard input); the markers reach every one of those.
//
// The refusal is SDLC-E0079. It comes before anything is read, as SDLC-E0061
// and SDLC-E0074 do, so it answers no question about the record. Those two
// keep refusing parallel and staffing assign first where the session id is
// set (decision 2 of 2026-10-20), and the new code covers every other case.
// The slash commands record as before: internal/hook's tests hold that.

import (
	"bytes"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/store"
)

const icInClaudeCode = "SDLC-E0079"

// icMarker is one of the variables Claude Code sets for the processes it
// starts, with a value of the kind it sets (ANALYSIS 7.1).
type icMarker struct{ name, value string }

var icMarkers = []icMarker{
	{"CLAUDECODE", "1"},
	{store.SessionEnv, "session-of-the-model"},
	{"CLAUDE_CODE_CHILD_SESSION", "1"},
	{"CLAUDE_PID", "4242"},
	{"AI_AGENT", "claude-code_2-1-302_agent"},
}

func icIsMarker(name string) bool {
	return slices.ContainsFunc(icMarkers, func(m icMarker) bool { return m.name == name })
}

// icRun runs one command in dir with set in its environment over what the
// test's commands have, and every marker that set does not name read as
// unset: a terminal outside Claude Code, given set.
func icRun(t *testing.T, dir string, set map[string]string, args ...string) result {
	t.Helper()
	inv := invocationOf(t)
	getenv := func(name string) string {
		if v, ok := set[name]; ok {
			return v
		}
		if icIsMarker(name) {
			return ""
		}
		return inv.getenv(name)
	}
	var out, errb bytes.Buffer
	code := ExecuteIn(dir, getenv, args, strings.NewReader(""), &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

// icRecord is everything a command could change: the project's files and the
// claims under git's directory.
func icRecord(t *testing.T, root string) map[string]string {
	t.Helper()
	out := projectFiles(t, root)
	for k, v := range wtFiles(t, filepath.Join(root, ".git", "sdlc")) {
		out[".git/sdlc/"+k] = v
	}
	return out
}

// icCommand is a person's command, with the slash command that stands for it
// in Claude Code, or none.
type icCommand struct {
	terminal, slash string
	args            []string
}

var icCommands = []icCommand{
	{"sdlc approve", "/sdlc:approve", []string{"approve", "US-001"}},
	{"sdlc unfreeze", "/sdlc:unfreeze", []string{"unfreeze", "--reason", "the test was wrong"}},
	{"sdlc ack", "/sdlc:ack", []string{"ack", "--through", "HEAD"}},
	{"sdlc remap", "/sdlc:remap", []string{"remap", "US-001"}},
	{"sdlc release", "/sdlc:release", []string{"release", "US-001", "--reason", "moved to a new session"}},
	{"sdlc staffing assign", "/sdlc:assign", []string{"staffing", "assign", "security", "--model", "opus", "--reason", "mine"}},
}

// icRefused holds r to AC-15's refusal: code given, one code only (nothing
// was read to find another), nothing on standard output, and a fix that names
// the slash command first and a terminal outside Claude Code second.
func icRefused(t *testing.T, r result, code string, c icCommand) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("%s went ahead inside Claude Code's environment:\n%s%s", c.terminal, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, code) {
		t.Errorf("%s was refused with something other than %s:\n%s", c.terminal, code, r.stderr)
	}
	if n := strings.Count(r.stderr, "SDLC-E"); n != 1 {
		t.Errorf("%s's refusal carries %d codes, so something was read before it was refused:\n%s", c.terminal, n, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("%s printed %q on standard output as well as refusing", c.terminal, r.stdout)
	}
	if code != icInClaudeCode {
		return
	}
	fix := rlFixLine(r.stderr)
	outside := strings.Index(fix, "outside Claude Code")
	if outside < 0 {
		t.Errorf("%s's refusal does not send a person to a terminal outside Claude Code:\n%s", c.terminal, r.stderr)
	}
	if c.slash == "" {
		if strings.Contains(fix, "/sdlc:") {
			t.Errorf("%s has no slash command, and its refusal names one:\n%s", c.terminal, fix)
		}
		return
	}
	slash := strings.Index(fix, c.slash)
	if slash < 0 {
		t.Fatalf("%s's refusal does not name %s:\n%s", c.terminal, c.slash, r.stderr)
	}
	if outside >= 0 && outside < slash {
		t.Errorf("%s's refusal names the terminal before %s (AC-12):\n%s", c.terminal, c.slash, fix)
	}
	// The terminal command, where the fix names it, comes after the slash
	// command: "/sdlc:ack" holds "sdlc:ack", never "sdlc ack".
	if i := strings.Index(fix, c.terminal); i >= 0 && i < slash {
		t.Errorf("%s's refusal names %s before %s (AC-12):\n%s", c.terminal, c.terminal, c.slash, fix)
	}
}

func TestAC15EachPersonsCommandRunWithAnyOfClaudeCodesMarkersChangesNothingAndIsRefused(t *testing.T) {
	t.Parallel()
	for _, c := range icCommands {
		t.Run("AC-15 "+c.terminal, func(t *testing.T) {
			root := storyUnderway(t)
			before := icRecord(t, root)
			rows := map[string]map[string]string{}
			for _, m := range icMarkers {
				rows[m.name+" alone"] = map[string]string{m.name: m.value}
			}
			// A model's Bash tool has every one of them.
			all := map[string]string{}
			for _, m := range icMarkers {
				all[m.name] = m.value
			}
			rows["every marker, as in a model's Bash"] = all

			for _, name := range slices.Sorted(maps.Keys(rows)) {
				set := rows[name]
				r := icRun(t, root, set, c.args...)
				// A session id reaches staffing assign's own check first,
				// unchanged (AC-8).
				want := icInClaudeCode
				if c.slash == "/sdlc:assign" && set[store.SessionEnv] != "" {
					want = "SDLC-E0074"
				}
				t.Logf("AC-15 %s with %s", c.terminal, name)
				icRefused(t, r, want, c)
				if after := icRecord(t, root); !maps.Equal(before, after) {
					t.Errorf("%s with %s was refused and changed the record all the same", c.terminal, name)
				}
			}
		})
	}
}

// The refusal reads nothing first: in a directory that is no project at all a
// person's command run inside Claude Code is refused for that, and not for
// the missing repository.
func TestAC15APersonsCommandInsideClaudeCodeIsRefusedBeforeTheRepositoryIsRead(t *testing.T) {
	t.Parallel()
	dir := wtTempDir(t)
	for _, c := range icCommands {
		r := icRun(t, dir, map[string]string{"CLAUDECODE": "1"}, c.args...)
		icRefused(t, r, icInClaudeCode, c)
	}
}

// AC-8 and AC-15's other half: a terminal outside Claude Code carries no
// marker, and its person's commands work as they did. A marker that is set to
// nothing marks nothing, and a variable of the person's own that only starts
// as Claude Code's do is not a marker.
func TestAC15ATerminalOutsideClaudeCodeStillRecordsAPersonsCommand(t *testing.T) {
	t.Parallel()
	for name, set := range map[string]map[string]string{
		"AC-15 a terminal with no marker":                 {},
		"AC-15 markers set to nothing":                    {"CLAUDECODE": "", "AI_AGENT": "", "CLAUDE_PID": ""},
		"AC-15 the person's own CLAUDE_CONFIG_DIR is set": {"CLAUDE_CONFIG_DIR": "/home/dev/.claude-work"},
	} {
		t.Run(name, func(t *testing.T) {
			root := storyUnderway(t)
			before := icRecord(t, root)
			r := icRun(t, root, set, "ack", "--through", "HEAD")
			if r.code != 0 || strings.Contains(r.stderr, icInClaudeCode) {
				t.Fatalf("sdlc ack from a terminal outside Claude Code: exit %d\n%s%s", r.code, r.stdout, r.stderr)
			}
			if maps.Equal(before, icRecord(t, root)) {
				t.Error("sdlc ack from a terminal outside Claude Code recorded nothing")
			}
		})
	}
}

// parallel has no slash command, and starts model sessions: inside Claude
// Code's environment it starts none. A session id is still SDLC-E0061's
// (decision 2); any other marker is AC-15's. Not parallel: the fake claude it
// would start is put on PATH.
func TestAC15ParallelRunWithAMarkerOtherThanTheSessionIdStartsNothingAndIsRefused(t *testing.T) {
	main := plRepository(t, 2)
	rig := plInstall(t, nil, false)
	before := icRecord(t, main)
	c := icCommand{terminal: "sdlc parallel", args: []string{"parallel", "US-001", "US-002"}}
	for _, m := range icMarkers {
		want := icInClaudeCode
		if m.name == store.SessionEnv {
			want = plFromASession
		}
		t.Logf("AC-15 sdlc parallel with %s", m.name)
		icRefused(t, icRun(t, main, map[string]string{m.name: m.value}, c.args...), want, c)
	}
	if got := rig.started(); len(got) != 0 {
		t.Errorf("sdlc parallel inside Claude Code's environment started sessions for %q", got)
	}
	if !maps.Equal(before, icRecord(t, main)) {
		t.Error("a refused sdlc parallel changed the record")
	}
}
