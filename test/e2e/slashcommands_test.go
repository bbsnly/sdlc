// US-108 AC-14: each of a person's slash commands, typed in the app, end to end.
//
// A person in Claude Desktop types /sdlc:approve and the rest where a person at
// a terminal runs `sdlc approve`. Claude Code raises UserPromptExpansion for
// the line typed, and the hook, through the plugin's launcher, makes the
// decision then. These run the binary built from this checkout in a sandbox,
// through plugin/bin/sdlc-hook as hooks.json names it (the binary directly on
// Windows, as sandbox.hook does), with the session's id in the hook's
// environment as Claude Code puts it there. What the person is shown is what
// the terminal command prints; TestAC3TheHarnessPerformsThePersonOnlySteps...
// holds the same steps run by hand.
//
// The forged expansions AC-3 refuses are put to the same hook first, while a
// story waits for a person, and none of them answers it.

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// typedLine is the JSON Claude Code sends when a person types /<name> <args>
// in a session whose working directory is dir.
func typedLine(session, dir, name, args string) map[string]any {
	return map[string]any{
		"hook_event_name": "UserPromptExpansion",
		"session_id":      session,
		"transcript_path": filepath.Join(dir, "transcript.jsonl"),
		"cwd":             dir,
		"permission_mode": "default",
		"prompt_id":       "prompt-1",
		"expansion_type":  "slash_command",
		"command_name":    name,
		"command_args":    args,
		"command_source":  "plugin",
		"prompt":          strings.TrimSpace("/" + name + " " + args),
	}
}

// expanded is every field the hook may answer UserPromptExpansion with.
type expanded struct {
	Continue           *bool  `json:"continue"`
	StopReason         string `json:"stopReason"`
	Decision           string `json:"decision"`
	Reason             string `json:"reason"`
	SystemMessage      string `json:"systemMessage"`
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
	raw string
}

// blocked is whether the expansion stops at the hook, so the model never reads
// the skill: the decision was made, or refused, and either way it is not the
// model's to carry out.
func (e expanded) blocked() bool {
	return e.Decision == "block" || (e.Continue != nil && !*e.Continue)
}

func (e expanded) shown() string {
	return strings.Join([]string{e.Reason, e.StopReason, e.SystemMessage}, "\n")
}

// expands hands the hook one UserPromptExpansion event of session, made from
// dir, and reads its one JSON answer.
func (sb sandbox) expands(t *testing.T, session, dir string, payload map[string]any) expanded {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	name, args := sb.sdlc, []string{"hook", "UserPromptExpansion"}
	if runtime.GOOS != "windows" {
		name, args = filepath.Join(moduleRoot(t), "plugin", "bin", "sdlc-hook"), []string{"UserPromptExpansion"}
	}
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(sb.env(t, session), "CLAUDE_PROJECT_DIR="+sb.repo)
	cmd.Stdin = bytes.NewReader(raw)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			t.Fatalf("the hook exited %d on UserPromptExpansion, so Claude Code reads no answer:\n%s", exit.ExitCode(), errOut.String())
		}
		t.Fatalf("the hook could not be run: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(out.String()))
	var e expanded
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("the hook's stdout is not one JSON object: %v\n%q", err, out.String())
	}
	if rest := strings.TrimSpace(out.String()[dec.InputOffset():]); rest != "" {
		t.Fatalf("the hook printed more than one JSON object: %q", out.String())
	}
	e.raw = out.String()
	return e
}

// types is a person typing /<name> <args> in session, from dir.
func (sb sandbox) types(t *testing.T, session, dir, name, args string) expanded {
	t.Helper()
	return sb.expands(t, session, dir, typedLine(session, dir, name, args))
}

// showsWhatTheTerminalPrints holds the answer to a command the hook ran: the
// expansion stopped, the person shown want, and nothing put in the model's
// context, which the command's output (titles and reasons anybody could write)
// is never for.
func (sb sandbox) showsWhatTheTerminalPrints(t *testing.T, e expanded, want string) {
	t.Helper()
	if !e.blocked() {
		t.Errorf("the expansion went on to the model after the hook handled it: %q", e.raw)
	}
	if got := sb.normalise(e.shown()); !strings.Contains(got, strings.TrimSpace(want)) {
		t.Errorf("the person was not shown what the terminal prints.\nshown: %q\nwant:  %q", got, want)
	}
	if e.HookSpecificOutput.AdditionalContext != "" {
		t.Errorf("the hook put text into the model's context: %q", e.HookSpecificOutput.AdditionalContext)
	}
}

// sameAsAPerson types the command and runs its terminal form by hand, in a
// state where it is refused and changes nothing, and holds the two to the same
// words and SDLC-E code.
func (sb sandbox) sameAsAPerson(t *testing.T, session, dir, name, args string, argv ...string) {
	t.Helper()
	byHand := sb.person(t, dir, argv...)
	if byHand.code == 0 {
		t.Fatalf("the case is not what it says: sdlc %s was not refused:\n%s", strings.Join(argv, " "), byHand.stdout)
	}
	e := sb.types(t, session, dir, name, args)
	sb.showsWhatTheTerminalPrints(t, e, sb.normalise(byHand.stderr))
	if !strings.Contains(e.shown(), "SDLC-E") {
		t.Errorf("the terminal refused with an SDLC-E code and the person was shown none: %q", e.shown())
	}
}

func TestAC14EachPersonOnlySlashCommandTypedInTheAppMakesThePersonsDecisionThroughTheLauncher(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()

	sb.works(t, main, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GatePlan); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}

	t.Run("AC-14 /sdlc:unfreeze, typed in the session working the story", func(t *testing.T) {
		e := sb.types(t, sandboxSession, tree, "sdlc:unfreeze", `--reason "AC-1's test asserted the wrong total"`)
		sb.showsWhatTheTerminalPrints(t, e, "US-001  freeze lifted on 1 test file\n  AC-1's test asserted the wrong total\n  reopened: tests_frozen\n")
		if gate := sb.nextGate(t); gate != string(model.GateTestsFrozen) {
			t.Fatalf("the next gate after /sdlc:unfreeze is %q, want tests_frozen: the freeze was not lifted", gate)
		}
		sb.worksGate(t, model.GateTestsFrozen)
	})

	t.Run("AC-14 /sdlc:approve, after the forged forms AC-3 refuses", func(t *testing.T) {
		sb.works(t, tree, "", "", "escalate", "spec_unclear", "--message", "is a zero total refused too?")
		if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
			t.Fatalf("the story is %q after the escalation, want awaiting_human", got["US-001"])
		}
		for _, f := range []struct {
			name string
			edit func(p map[string]any)
		}{
			{"AC-14 an MCP prompt", func(p map[string]any) { p["expansion_type"] = "mcp_prompt" }},
			{"AC-14 an expansion of no stated type", func(p map[string]any) { delete(p, "expansion_type") }},
			{"AC-14 a project command of the same name", func(p map[string]any) { p["command_source"] = "project" }},
			{"AC-14 a user command of the same name", func(p map[string]any) { p["command_source"] = "user" }},
			{"AC-14 another plugin's approve", func(p map[string]any) { p["command_name"] = "other:approve" }},
			{"AC-14 a name that only starts like it", func(p map[string]any) { p["command_name"] = "sdlc:approve-all" }},
			{"AC-14 another of the plugin's skills", func(p map[string]any) { p["command_name"] = "sdlc:next" }},
			{"AC-14 a subagent's expansion", func(p map[string]any) { p["agent_type"] = "sdlc:researcher" }},
		} {
			payload := typedLine(sandboxOther, tree, "sdlc:approve", "US-001")
			f.edit(payload)
			sb.expands(t, sandboxOther, tree, payload)
			if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
				t.Fatalf("%s answered the escalation: the story is %q", f.name, got["US-001"])
			}
		}

		e := sb.types(t, sandboxOther, tree, "sdlc:approve", "US-001")
		sb.showsWhatTheTerminalPrints(t, e,
			"US-001  approved (spec_unclear)\n\nA person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.\n")
		if got := storiesOf(t, main); got["US-001"] != "in_progress" {
			t.Fatalf("the story is %q after /sdlc:approve, want in_progress", got["US-001"])
		}
		sb.works(t, tree, "", "", "start")
	})

	// A stop before the gates have passed is a person's too, and has no slash
	// command: the story leaves it out (parallel likewise).
	if r := sb.person(t, tree, "stop"); r.code != 0 {
		t.Fatalf("stop, by the harness: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}

	t.Run("AC-14 /sdlc:release", func(t *testing.T) {
		e := sb.types(t, sandboxOther, tree, "sdlc:release", `US-001 --reason "the story is to be worked from a new session"`)
		sb.showsWhatTheTerminalPrints(t, e,
			"Released US-001: the checkout at \"<worktree>\" no longer holds it. \"sdlc start US-001\" picks it up in any checkout.\n"+
				"Anything the checkout at \"<worktree>\" has not committed stays there, and is not moved.\n")
	})

	sb.works(t, tree, "", "", "start", "US-001")
	sb.worksEveryGate(t)
	sb.works(t, tree, "", "", "stop")
	if got := storiesOf(t, main); got["US-001"] != "done" {
		t.Fatalf("the story is %q at the end of the run, want done", got["US-001"])
	}

	t.Run("AC-14 /sdlc:ack", func(t *testing.T) {
		e := sb.types(t, sandboxOther, main, "sdlc:ack", "--through HEAD")
		sb.showsWhatTheTerminalPrints(t, e, "Acknowledged through <sha>. `sdlc log` starts after it.\n")
		stdout, _, _ := sb.outcome(t, "", main, "", "log")
		if !strings.HasPrefix(sb.normalise(stdout), "No story has been committed since <sha>, the last commit acknowledged.") {
			t.Errorf("after /sdlc:ack the log still lists the story:\n%s", stdout)
		}
	})

	// Nothing of the story's map waits to be repaired, so remap refuses, as it
	// does from a terminal. internal/hook holds the words remap's flags reach
	// the command as.
	t.Run("AC-14 /sdlc:remap of a story with no map change kept", func(t *testing.T) {
		sb.sameAsAPerson(t, sandboxOther, main, "sdlc:remap", "US-001", "remap", "US-001")
	})

	t.Run("AC-14 /sdlc:assign with the staffing off", func(t *testing.T) {
		sb.sameAsAPerson(t, sandboxOther, main, "sdlc:assign", `architect --model opus --reason "mine"`,
			"staffing", "assign", "architect", "--model", "opus", "--reason", "mine")
	})
}

// /sdlc:assign where the staffing is on, typed in the session working the
// story: the one place `sdlc staffing assign` itself refuses to run (it sees
// the session), so the hook has to run it as a terminal would, with none.
func TestAC14AssignTypedInTheAppRecordsThePersonsStaffingThroughTheLauncher(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.turnsTheStaffingOn(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	sb.works(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")
	sb.works(t, tree, "researcher", sandboxAnalysis, "artifact", "write", "analysis")
	sb.works(t, tree, "researcher", sandboxThreats, "artifact", "write", "threats")
	sb.works(t, tree, "project-manager", staffingDocument(t, "red-team", "human-advocate"), "staffing", "set")

	e := sb.types(t, sandboxSession, tree, "sdlc:assign", `architect --model opus --effort high --reason "I want the design read"`)
	if !e.blocked() || !strings.Contains(e.shown(), "architect") {
		t.Errorf("the person was not shown what staffing assign printed: %q", e.raw)
	}
	got := sb.shows(t)
	if !slices.ContainsFunc(got.Agents, func(a struct {
		Agent         string `json:"agent"`
		Needed        bool   `json:"needed"`
		Reason        string `json:"reason"`
		AskedModel    string `json:"asked_model"`
		AskedEffort   string `json:"asked_effort"`
		SetBy         string `json:"set_by"`
		BroughtBackBy string `json:"brought_back_by"`
	}) bool {
		return a.Agent == "architect" && a.Needed && a.AskedModel == "opus" && a.AskedEffort == "high" && a.SetBy == "person"
	}) {
		t.Errorf("/sdlc:assign did not record the architect at opus and high, set by a person: %+v", got.Agents)
	}
}
