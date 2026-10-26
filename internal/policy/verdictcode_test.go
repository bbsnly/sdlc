package policy

import "testing"

// US-035 AC-12: a refusal with a code renders it between the route and the
// rule, as the shell rules' refusals do, so that one guide reads both.
func TestAC12AVerdictWithACodeSaysItBeforeTheRule(t *testing.T) {
	v := Verdict{Rule: "too-many-links", Reason: "lnk/x goes through more than 64 links", Route: "name the place directly", Code: "SDLC-E0081"}
	want := "lnk/x goes through more than 64 links. Instead: name the place directly (SDLC-E0081) [too-many-links]"
	if got := v.Message(); got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
	v.Code = ""
	if got := v.Message(); got != "lnk/x goes through more than 64 links. Instead: name the place directly [too-many-links]" {
		t.Errorf("without a code, Message() = %q", got)
	}
}
