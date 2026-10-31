package shellpolicy

// US-112, THREATS S1 and design review round 1 (security, B1): the hook
// records what Claude Code tells it on SubagentStart, SubagentStop,
// Notification, StopFailure, PostCompact, PostToolUseFailure, PostToolUse and
// Stop, and two of those write agent_start and agent_end to the gate record,
// which US-118 will refuse gates on. A model's own shell that runs the hook on
// one of those events with a payload it wrote forges that record. The hook's
// own check is AI_AGENT, which Claude Code sets for a model's tools; the model
// can take it out of its own command. So two rules, each covering the other's
// gaps, in the session holding the story and in any other:
//
//   - the hook run by hand on an event it records is refused, as the
//     UserPromptExpansion is (forgedhook.go), under a rule of its own;
//     PreToolUse and SessionStart stay open, which is how reviewers here drive
//     the hook;
//   - a command that removes, blanks or overrides AI_AGENT is refused under
//     enforcement-stays-on, beside SDLC_ENFORCE and CLAUDE_CODE_SESSION_ID.
//
// The Bash rows for the first rule are forgedhook_test.go's spellings, each
// tried on every recorded event. The Bash rows for the second were each run
// under bash and zsh with AI_AGENT set, and each leaves a child process with
// AI_AGENT unset, empty or another value in at least one of the two
// (`export -n` is bash's, and `local AI_AGENT` with no value unsets it only in
// zsh). The PowerShell and cmd rows are the forms setsSession reads for
// CLAUDE_CODE_SESSION_ID, and Remove-Item and Clear-Item on the env: drive;
// they were not run, as no PowerShell was at hand.
//
// Since the person's decision of 2026-10-29 the hook records whatever
// AI_AGENT holds, so these two rules are all that hold S1
// (hiddenbehind_test.go adds interpreter code that runs the hook by hand).
// Not refused by either rule: interpreter code that builds the variable's name, as
// `os.environ.pop("AI_"+"AGENT")`, and a script file that unsets it. Round 2
// left `env -i` here too, for leaving no PATH; PATH can be passed back, so
// round 3 refuses it with the other one-line spellings (oneline_test.go).

import (
	"strings"
	"testing"
)

// ruleRecordedEvent is the rule a hand-run hook on a recorded event is refused
// under.
const ruleRecordedEvent = "recorded-event-comes-from-the-host"

// recordedEvents is every event the hook records as this story has it, the
// two it recorded nothing on before included.
var recordedEvents = []string{
	"SubagentStart", "SubagentStop", "Notification", "StopFailure", "PostCompact",
	"PostToolUseFailure", "PostToolUse", "Stop",
}

// forgedEventPayload is a payload a model could write for event.
func forgedEventPayload(event string) string {
	return `{"hook_event_name":"` + event + `","session_id":"session-a","agent_type":"sdlc:verifier","agent_id":"a1"}`
}

// handRunsOf is every spelling that runs the hook on event by hand, for Bash
// and for PowerShell.
func handRunsOf(event string) (bash, powerShell []string) {
	hook := "sdlc hook " + event
	return []string{
		"printf '%s' '" + forgedEventPayload(event) + "' | " + hook,
		hook + " < /tmp/p.json",
		hook + " 0</tmp/p.json",
		hook + " <<< '" + forgedEventPayload(event) + "'",
		hook + " <<'EOF'\n" + forgedEventPayload(event) + "\nEOF",
		"exec 3</tmp/p.json; " + hook + " <&3",
		hook,
		"sdlc-hook " + event + " < /tmp/p.json",
		`"$CLAUDE_PLUGIN_ROOT/bin/sdlc-hook" ` + event + " < /tmp/p.json",
		"/opt/sdlc/plugin/bin/sdlc-hook " + event + " < /tmp/p.json",
		"cat /tmp/p.json | /opt/sdlc/plugin/bin/sdlc-hook " + event,
		"sh plugin/bin/sdlc-hook " + event + " < p.json",
		"bash ./plugin/bin/sdlc-hook " + event + " < p.json",
		"sh -c '" + hook + " < /tmp/p.json'",
		"nohup " + hook + " < /tmp/p.json",
		"env " + hook + " < /tmp/p.json",
		"command " + hook + " < /tmp/p.json",
		"/usr/local/bin/sdlc hook " + event + " < /tmp/p.json",
		"npx @bbsnly/sdlc hook " + event + " < /tmp/p.json",
		"go run ./cmd/sdlc hook " + event + " < /tmp/p.json",
		"cd /tmp && " + hook + " < p.json",
		// The event in any case, as the expansion is read.
		"sdlc hook " + strings.ToLower(event) + " < /tmp/p.json",
		"sdlc hook " + strings.ToUpper(event) + " < /tmp/p.json",
		// A copy of sdlc under another name, handed `hook` and the event.
		"/tmp/x hook " + event + " < /tmp/p.json",
		// cmd's forms, from the Bash tool.
		`cmd /c "` + hook + ` < p.json"`,
		"sdlc-hook.cmd " + event + " < p.json",
	}, []string{
		"Get-Content p.json | " + hook,
		`Get-Content p.json | & "$env:CLAUDE_PLUGIN_ROOT\bin\sdlc-hook.cmd" ` + event,
		"Start-Process sdlc -ArgumentList 'hook','" + event + "' -RedirectStandardInput p.json",
		"Start-Process sdlc -ArgumentList 'hook " + event + "' -RedirectStandardInput p.json",
		"Start-Process -FilePath:sdlc -ArgumentList:'hook','" + event + "' -RedirectStandardInput p.json",
		`cmd /c "` + hook + ` < p.json"`,
	}
}

// refusedInEverySession holds one command to rule, with a reason and a route,
// from the session holding the story and from any other, and returns the
// first session's finding.
func refusedInEverySession(t *testing.T, command string, powerShell bool, rule string) Finding {
	t.Helper()
	none := func(string) bool { return false }
	held, heldOK := Inspect(command, State{CommitReady: true, PowerShell: powerShell})
	other, otherOK := HumanDecisions(command, powerShell, none)
	for _, c := range []struct {
		session string
		f       Finding
		ok      bool
	}{{"the session holding the story", held, heldOK}, {"a second session", other, otherOK}} {
		switch {
		case !c.ok:
			t.Errorf("%s may run %q", c.session, command)
		case c.f.Rule != rule:
			t.Errorf("%s: %q was refused as %s, want %s (%s)", c.session, command, c.f.Rule, rule, c.f.Reason)
		case c.f.Reason == "" || c.f.Route == "":
			t.Errorf("%s: the refusal of %q does not say why and what to do: %+v", c.session, command, c.f)
		}
	}
	return held
}

// B1, THREATS S1: every hand-run of the hook on an event it records is
// refused in every session, and the refusal says the event that stays open.
func TestAC2AC15AModelsShellThatRunsTheHookOnAnEventItRecordsIsRefusedInEverySession(t *testing.T) {
	for _, event := range recordedEvents {
		bash, powerShell := handRunsOf(event)
		for _, command := range bash {
			f := refusedInEverySession(t, command, false, ruleRecordedEvent)
			if f.Rule == ruleRecordedEvent && !strings.Contains(f.Route, "PreToolUse") {
				t.Errorf("the refusal of %q does not say that the hook may be tried on PreToolUse: %s", command, f.Route)
			}
		}
		for _, command := range powerShell {
			refusedInEverySession(t, command, true, ruleRecordedEvent)
		}
	}
}

// B1: the rule is the recorded events'. PreToolUse and SessionStart, which
// the hook records nothing on by hand, stay open, and so does every line that
// only names an event or the launcher; the expansion and an event that cannot
// be read keep their own rule.
func TestAC2AC15TheHookRunByHandOnAnEventItDoesNotRecordIsLeftAlone(t *testing.T) {
	for _, c := range []struct {
		command    string
		powerShell bool
	}{
		{"sdlc hook PreToolUse < /tmp/p.json", false},
		{"sdlc-hook PreToolUse < /tmp/p.json", false},
		{"cat /tmp/p.json | /opt/sdlc/plugin/bin/sdlc-hook PreToolUse", false},
		{"sdlc hook SessionStart < /tmp/p.json", false},
		{"sdlc status", false},
		{"docker stop web", false},
		{"echo Stop", false},
		{"grep -n SubagentStop plugin/hooks/hooks.json", false},
		{"grep -rn PostToolUseFailure docs", false},
		{`git commit -m "record SubagentStop and Stop"`, false},
		{"git add plugin/bin/sdlc-hook", false},
		{"chmod +x plugin/bin/sdlc-hook", false},
		{"sdlc trace A-1", false},
		{"Get-Content p.json | sdlc hook PreToolUse", true},
		{"Start-Process sdlc -ArgumentList 'hook PreToolUse'", true},
	} {
		leftAloneInEverySession(t, c.command, c.powerShell)
	}
	refusedInEverySession(t, "sdlc hook UserPromptExpansion < /tmp/p.json", false, ruleSlashCommand)
	refusedInEverySession(t, `sdlc hook "$E" < /tmp/p.json`, false, ruleSlashCommand)
}

// aiAgentRemovals is every row that removes, blanks or overrides AI_AGENT for
// what runs after it, for Bash and for PowerShell.
func aiAgentRemovals() (bash, powerShell []string) {
	return []string{
		// Blanked or overridden for one command.
		"AI_AGENT= sdlc hook PreToolUse < /tmp/p.json",
		"AI_AGENT=x sdlc hook PreToolUse < /tmp/p.json",
		`AI_AGENT="" sdlc status`,
		"AI_AGENT+=x sdlc status",
		"env AI_AGENT= sdlc hook PreToolUse < /tmp/p.json",
		"true && AI_AGENT= sdlc status",
		"echo '{}' | AI_AGENT= sdlc hook PreToolUse",
		// Blanked for the rest of the shell, which persists between calls.
		"export AI_AGENT=",
		`export AI_AGENT=""; sdlc hook PreToolUse < /tmp/p.json`,
		"declare -x AI_AGENT=",
		"typeset -x AI_AGENT=",
		"readonly AI_AGENT=",
		// Removed.
		"unset AI_AGENT",
		"unset AI_AGENT; sdlc hook PreToolUse < /tmp/p.json",
		"unset -v AI_AGENT",
		"unset FOO AI_AGENT",
		"unset 'AI_AGENT'",
		`unset "AI_AGENT"`,
		`unset AI_\AGENT`,
		"env -u AI_AGENT sdlc hook PreToolUse < /tmp/p.json",
		"env -uAI_AGENT sdlc hook PreToolUse < /tmp/p.json",
		"env --unset=AI_AGENT sdlc hook PreToolUse < /tmp/p.json",
		"env --unset AI_AGENT sdlc status",
		"export -n AI_AGENT",
		"typeset +x AI_AGENT",
		"declare +x AI_AGENT",
		"f() { local AI_AGENT; sdlc hook PreToolUse < /tmp/p.json; }; f",
		"f() { local AI_AGENT=; sdlc hook PreToolUse < /tmp/p.json; }; f",
		// Behind a wrapper, a subshell, another shell or eval.
		"(unset AI_AGENT; sdlc hook PreToolUse < /tmp/p.json)",
		"bash -c 'unset AI_AGENT; sdlc hook PreToolUse < /tmp/p.json'",
		"sh -c 'AI_AGENT= sdlc hook PreToolUse < /tmp/p.json'",
		"cd /tmp && unset AI_AGENT && sdlc hook PreToolUse < p.json",
		"nohup env -u AI_AGENT sdlc hook PreToolUse < /tmp/p.json",
		"eval 'unset AI_AGENT'",
		`eval "AI_AGENT= sdlc hook PreToolUse < /tmp/p.json"`,
		// A name the shell makes as the command runs could be this one,
		// as an event that cannot be read could be the expansion.
		`unset "$V"`,
		`env -u "$V" sdlc hook PreToolUse < /tmp/p.json`,
		// Code an interpreter runs, naming the variable.
		`python3 -c 'import os,subprocess; os.environ.pop("AI_AGENT"); subprocess.run(["sdlc","hook","PreToolUse"])'`,
		"python3 - <<'EOF'\nimport os\ndel os.environ['AI_AGENT']\nos.execvp('sdlc', ['sdlc', 'hook', 'PreToolUse'])\nEOF",
		`node -e 'delete process.env.AI_AGENT; require("child_process").execSync("sdlc hook PreToolUse")'`,
		// cmd's form, from the Bash tool.
		`cmd /c "set AI_AGENT= && sdlc hook PreToolUse < p.json"`,
	}, []string{
		"$env:AI_AGENT = ''; sdlc hook PreToolUse",
		"$env:AI_AGENT=$null",
		"${env:AI_AGENT} = $null",
		// The environment's names are folded on Windows.
		"$env:ai_agent = ''",
		"Remove-Item Env:AI_AGENT",
		`Remove-Item -Path Env:\AI_AGENT`,
		"ri env:AI_AGENT",
		"del Env:/AI_AGENT",
		"rm env:ai_agent",
		"Clear-Item Env:AI_AGENT",
		"Set-Item Env:AI_AGENT ''",
		"New-Item -Path Env:AI_AGENT -Value x -Force",
		"[Environment]::SetEnvironmentVariable('AI_AGENT', $null)",
		`[System.Environment]::SetEnvironmentVariable("AI_AGENT", "", "Process")`,
		`cmd /c "set AI_AGENT= && sdlc hook PreToolUse < p.json"`,
	}
}

// B1, THREATS S1, and G9's other half: the hook tells a model's tool by
// AI_AGENT, so the model may not take it away, blank it or set it to anything
// else, in the session holding the story or in another. The refusal names
// the variable and does not send the agent round the rule.
func TestAC2AC15ACommandThatRemovesOrOverridesAIAgentIsRefusedInEverySession(t *testing.T) {
	bash, powerShell := aiAgentRemovals()
	for _, command := range bash {
		f := refusedInEverySession(t, command, false, "enforcement-stays-on")
		if f.Rule == "enforcement-stays-on" && (!strings.Contains(f.Reason, "AI_AGENT") || strings.Contains(f.Route, "sdlc stop")) {
			t.Errorf("the refusal of %q does not name AI_AGENT, or its route ends the iteration: %+v", command, f)
		}
	}
	for _, command := range powerShell {
		refusedInEverySession(t, command, true, "enforcement-stays-on")
	}
}

// B1: reading AI_AGENT, a longer name that holds it, re-exporting it as it is,
// and taking another variable away are not refused.
func TestAC2AC15ReadingAIAgentOrChangingAnotherVariableIsLeftAlone(t *testing.T) {
	for _, c := range []struct {
		command    string
		powerShell bool
	}{
		{"echo $AI_AGENT", false},
		{"printenv AI_AGENT", false},
		{"env | grep AI_AGENT", false},
		{`test -n "$AI_AGENT" && echo a model`, false},
		{"grep -rn AI_AGENT docs", false},
		{`git commit -m "unset AI_AGENT in the hook"`, false},
		{"export AI_AGENT", false},
		{"MY_AI_AGENT= ls", false},
		{"AI_AGENTS=1 ls", false},
		{"unset CDPATH", false},
		{"env -u CDPATH ls", false},
		{"sdlc hook PreToolUse < /tmp/p.json", false},
		{"echo $env:AI_AGENT", true},
		{"Get-Item Env:AI_AGENT", true},
		{"Get-ChildItem env: | Select-String AI_AGENT", true},
	} {
		leftAloneInEverySession(t, c.command, c.powerShell)
	}
}
