package plugin

// US-108 AC-2: the six slash commands a person makes their decisions with.
//
// The hook makes the decision on UserPromptExpansion, before the skill's body
// reaches the model (AC-1). The body is read only when the hook let the
// expansion through: an older binary that does not know the event, a hook that
// crashed and failed open, or a Claude Code that never raised it. In every one
// of those the action did not run, and the model must say so and do nothing:
// a body that told it how to approve would have the model make the person's
// decision, which is the one thing the loop exists to prevent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// personsSkill is one of a person's slash commands and the terminal command it
// stands for.
type personsSkill struct {
	name     string // the skill, typed as /sdlc:<name>
	terminal string // the command a person types in a terminal
}

var personsSkills = []personsSkill{
	{"approve", "sdlc approve"},
	{"unfreeze", "sdlc unfreeze"},
	{"ack", "sdlc ack"},
	{"remap", "sdlc remap"},
	{"release", "sdlc release"},
	{"assign", "sdlc staffing assign"},
}

// personsSkillPromises are the sentences each body has to say, with the line
// breaks folded away, as TestAHumanOnlySkillKeepsWhatItPromises pins every
// other human-only skill's.
func personsSkillPromises(s personsSkill) []string {
	return []string{
		"`/sdlc:" + s.name + "` did not run",
		"Tell the person that",
		"Do not run `" + s.terminal + "` yourself",
		"Do nothing else",
	}
}

// maxPersonsSkillBody bounds a body that only reports: room for the sentences
// above and a line on what to try, and none for instructions.
const maxPersonsSkillBody = 1200

func TestAC2EachPersonsSlashCommandIsKeptFromTheModelAndOnlyReportsThatTheActionDidNotRun(t *testing.T) {
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Component{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	for _, want := range personsSkills {
		t.Run("AC-2 "+want.name, func(t *testing.T) {
			s, ok := byName[want.name]
			if !ok {
				t.Fatalf("there is no %s skill, so /sdlc:%s is not a command a person can type", want.name, want.name)
			}
			if dir := filepath.Base(filepath.Dir(s.Path)); dir != want.name {
				t.Errorf("%s: the skill sits in %q; Claude Code names it /sdlc:<directory>", s.Path, dir)
			}
			if flag := s.Front["disable-model-invocation"]; flag != "true" {
				t.Errorf("%s: disable-model-invocation is %q, so the model can start the person's decision", s.Path, flag)
			}
			if !humanOnlySkills[want.name] {
				t.Errorf("%s is not in humanOnlySkills, so nothing else holds its flag", want.name)
			}
			// A skill that grants tools hands the model the means to act on it.
			if v, ok := s.Front["allowed-tools"]; ok {
				t.Errorf("%s: allowed-tools: %s -- a body that only reports needs no tool", s.Path, v)
			}
			raw, err := os.ReadFile(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Join(strings.Fields(s.Body), " ")
			for _, phrase := range personsSkillPromises(want) {
				if !strings.Contains(body, phrase) {
					t.Errorf("%s: does not say %q", s.Path, phrase)
				}
			}
			// What would have the model act rather than report: a shell line
			// Claude Code runs as it expands the skill, a block of commands to
			// copy, or the command a person runs given as something to do.
			for _, bad := range []string{"!`", "```", "$(", "sdlc " + want.name + " $ARGUMENTS", want.terminal + " $ARGUMENTS"} {
				if strings.Contains(string(raw), bad) {
					t.Errorf("%s: holds %q, which tells the model to do something rather than report", s.Path, bad)
				}
			}
			if n := len(body); n > maxPersonsSkillBody {
				t.Errorf("%s: the body is %d bytes; one that only reports that the action did not run fits in %d",
					s.Path, n, maxPersonsSkillBody)
			}
		})
	}
}

// AC-10: /sdlc:init and /sdlc:doctor are commands a person can type. They are
// not a person's decisions, so the model may start them as it may run the
// terminal commands; nothing here pins their flags.
func TestAC10InitAndDoctorAreSlashCommandsAPersonCanType(t *testing.T) {
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, s := range skills {
		have[s.Name] = true
	}
	for _, name := range []string{"init", "doctor"} {
		if !have[name] {
			t.Errorf("AC-10: there is no %s skill, so /sdlc:%s is not a command a person can type", name, name)
		}
	}
}
