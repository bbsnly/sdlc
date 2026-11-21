package config

import (
	"reflect"
	"testing"
)

// stepAbove is Team.StepAbove(model, effort), what an arbiter is asked for given
// what its reviewer was asked for (US-131 AC-6, the settled IQ-3), asked through
// the method set so that this package compiles before the method is written.
// The test plan names it.
func stepAbove(t *testing.T, team Team, model, effort string) (gotModel, gotEffort string) {
	t.Helper()
	m := reflect.ValueOf(team).MethodByName("StepAbove")
	if !m.IsValid() {
		t.Fatal("AC-6 Team has no StepAbove method: an arbiter is staffed one step above its reviewer, " +
			"func (t Team) StepAbove(model, effort string) (string, string)")
	}
	f, ok := reflect.TypeAssert[func(string, string) (string, string)](m)
	if !ok {
		t.Fatalf("AC-6 StepAbove is %s, want func(string, string) (string, string)", m.Type())
	}
	return f(model, effort)
}

// One step up team.efforts; at its ceiling, the next entry of team.models at the
// same effort; at both ceilings, the reviewer's own. With no row to step from,
// which is a project with the project manager disabled, the highest effort and
// the session's model, which is no model named.
func TestAC6AnArbiterIsAskedForOneStepAboveItsReviewer(t *testing.T) {
	t.Parallel()
	custom := Team{Models: []string{"small", "large"}, Efforts: []string{"low", "high"}}
	for _, c := range []struct {
		name          string
		team          Team
		model, effort string
		wantM, wantE  string
	}{
		{"AC-6 the next effort", Team{}, "sonnet", "medium", "sonnet", "high"},
		{"AC-6 the lowest effort", Team{}, "haiku", "low", "haiku", "medium"},
		{"AC-6 the next model at the effort ceiling", Team{}, "sonnet", "max", "opus", "max"},
		{"AC-6 the reviewer's own at both ceilings", Team{}, "opus", "max", "opus", "max"},
		{"AC-6 the project's own efforts", custom, "small", "low", "small", "high"},
		{"AC-6 the project's own models", custom, "small", "high", "large", "high"},
		{"AC-6 the project's own ceilings", custom, "large", "high", "large", "high"},
		{"AC-6 no row to step from", Team{}, "", "", "", "max"},
		{"AC-6 no row, with the project's efforts", custom, "", "", "", "high"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m, e := stepAbove(t, c.team, c.model, c.effort)
			if m != c.wantM || e != c.wantE {
				t.Errorf("AC-6 above %q at %q the arbiter is asked for %q at %q, want %q at %q",
					c.model, c.effort, m, e, c.wantM, c.wantE)
			}
		})
	}
}
