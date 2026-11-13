package main

// US-129 AC-7, beside R3: a write to a frozen test at the end of a shape
// survives a lexer that skips the middle of a long command, and a here-string
// comparison that pairs a terminator with the wrong opener and drops what is
// between two blocks. At the start, or between two blocks, it does not. So
// each of the seven shapes, cut to leave room, with the write at its start
// and in its middle (between two here-string blocks, or inside the padding of
// the others), is refused as it was at the base (US-129 red team review 2,
// F-24).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// us129Leading is the write that opens a command, and us129Between the one
// set inside it, as Bash and as PowerShell spell them.
var (
	us129Leading = map[bool]string{
		false: "echo x > internal/alpha/alpha_test.go; ",
		true:  "Set-Content -Path internal/alpha/alpha_test.go -Value x\n",
	}
	us129Between = map[bool]string{
		false: "; echo x > internal/alpha/alpha_test.go; ",
		true:  "Set-Content -Path internal/alpha/alpha_test.go -Value x\n",
	}
)

// us129Placed is s, cut to leave room, with the write at its start, or in its
// middle: at the block boundary nearest the half for a here-string shape, and
// halfway through the run of a's that pads every other.
func us129Placed(t *testing.T, s shape, where string) string {
	t.Helper()
	if where == "start" {
		write := us129Leading[s.powerShell]
		return write + us129Cut(t, s, len(write))
	}
	write := us129Between[s.powerShell]
	cut := us129Cut(t, s, len(write))
	if s.powerShell {
		const block = "$s = @'\nx\n'@\n"
		at := len(cut) / len(block) / 2 * len(block)
		return cut[:at] + write + cut[at:]
	}
	from := strings.Index(cut, strings.Repeat("a", 128))
	if from < 0 {
		t.Fatalf("the %s shape has no padding to write into", s.name)
	}
	to := from
	for to < len(cut) && cut[to] == 'a' {
		to++
	}
	at := (from + to) / 2
	return cut[:at] + write + cut[at:]
}

// us129PlacedRefusals is the sum of what Inspect refused each shape with, its
// write at the start or in the middle, at 37e3ac8, the directory it was read
// in spelt <dir>.
var us129PlacedRefusals = map[string]string{
	"perl start": "99873ec44958", "perl middle": "99873ec44958",
	"here-string start": "99873ec44958", "here-string middle": "99873ec44958",
	"single-dash-touch start": "99873ec44958", "single-dash-touch middle": "99873ec44958",
	"single-dash-cp start": "99873ec44958", "single-dash-cp middle": "99873ec44958",
	"single-dash-sed start": "99873ec44958", "single-dash-sed middle": "99873ec44958",
	"single-dash-tee start": "99873ec44958", "single-dash-tee middle": "99873ec44958",
	"readings-single-dash start": "99873ec44958", "readings-single-dash middle": "99873ec44958",
}

func TestAC7EachAdversarialShapeWithAWriteToAFrozenTestAtItsStartOrInItsMiddleIsRefusedAsAtTheBase(t *testing.T) {
	dir := us129Resolved(t.TempDir())
	for _, s := range shapes() {
		if !us129Named(s) {
			continue
		}
		for _, where := range []string{"start", "middle"} {
			name := s.name + " " + where
			t.Run("AC-7 "+name, func(t *testing.T) {
				command := us129Placed(t, s, where)
				if len(command) > len(s.command) {
					t.Fatalf("the %s shape with its write is %d bytes, longer than the shape's %d", name, len(command), len(s.command))
				}
				f, refused := shellpolicy.Inspect(command, state(dir, s.powerShell))
				text := strings.ReplaceAll(fmt.Sprintf("rule=%q via=%q code=%q route=%q reason=%q", f.Rule, f.Via, f.Code, f.Route, f.Reason), dir, "<dir>")
				sum := sha256.Sum256([]byte(text))
				got := hex.EncodeToString(sum[:6])
				if os.Getenv("US129_PRINT") != "" {
					t.Logf("%q: %q, // %s", name, got, text)
				}
				if !refused {
					t.Fatalf("AC-7 the %s shape with a write to a frozen test at its %s is let through", s.name, where)
				}
				if want := us129PlacedRefusals[name]; got != want {
					t.Errorf("AC-7 the %s shape with a write to a frozen test at its %s is refused otherwise than at the base: %s", s.name, where, text)
				}
			})
		}
	}
}
