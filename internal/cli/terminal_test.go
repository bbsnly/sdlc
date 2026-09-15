package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode"
)

// A title, a message or a reason comes from a file a clone brings along. None
// of it may reach the terminal as a control the terminal acts on.
func TestTheTerminalIsNotHandedAControlFromTheRepository(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	csi := string(rune(0x9b))
	title := "Refunds\x1b]0;owned\x07" + csi + "2J\r"
	escaped := `Refunds` + spelled("001b") + `]0;owned` + spelled("0007") + spelled("009b") + `2J`
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"`+escaped+`\r","status":"ready","risk_tier":"low","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)

	out := mustRun(t, "status").stdout
	if strings.ContainsFunc(out, isControl) {
		t.Errorf("status printed a control character:\n%q", out)
	}
	if !strings.Contains(out, escaped+spelled("000d")) {
		t.Errorf("status does not show the title's controls as text:\n%s", out)
	}

	payload := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if payload.Next == nil || payload.Next.Title != title {
		t.Errorf("status --json changed the title: %+v", payload.Next)
	}

	r := run(t, "artifact", "write", "\x1b[2J")
	if r.code == 0 || strings.ContainsFunc(r.stderr, isControl) || !strings.Contains(r.stderr, spelled("001b")+"[2J") {
		t.Errorf("an unknown document name reached the terminal as a control:\n%q", r.stderr)
	}
}

// fmt writes whole strings, but a control cut in two by a writer that did not
// is a control again once the terminal joins it.
func TestAControlSplitAcrossTwoWritesIsStillSpelledOut(t *testing.T) {
	var buf bytes.Buffer
	w := &controls{w: &buf}
	for _, part := range [][]byte{{'a', 0xc2}, {0x9b, '2', 'J', 0x9b, 0xe2, 0x80}} {
		if n, err := w.Write(part); err != nil || n != len(part) {
			t.Fatalf("Write(%q) = %d, %v", part, n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), "a"+spelled("009b")+`2J\x9b`+"\xe2"+`\x80`; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
}

// spelled is a control as sdlc prints it: a backslash, u, and its code.
func spelled(code string) string { return `\u` + code }

func isControl(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }
