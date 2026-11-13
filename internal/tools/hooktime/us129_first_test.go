package main

// US-129 AC-7, for the call the hook makes, and for what the cut may not
// change. The AC-7 allocation row counts with AllocsPerRun, which warms up
// first: a cache kept by the package and keyed by the command would pass it
// and gain nothing in the hook, which classifies one command a process. Here
// each shape's first Inspect is counted in a process of its own (US-129 red
// team, F-14 and R1). And every shape the cut is for is let through, so an
// early answer for a command past some size would halve its allocations and
// pass both goldens: each shape, cut to leave room, then a write to a frozen
// test, is refused as it was at the base (red team, F-15 and R3).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// us129Named is whether s is one of the shapes AC-7 names.
func us129Named(s shape) bool {
	return strings.HasPrefix(s.name, "single-dash-") || s.name == "here-string" || s.name == "readings-single-dash" || s.name == perlBudget
}

// us129FirstBase is what each shape's first Inspect allocated at 37e3ac8, by
// the operating system it was counted on (Gate 3): runtime.MemStats.Mallocs
// around the one call, the most of the runs taken.
var us129FirstBase = map[string]map[string]uint64{
	"darwin": {
		"perl": 14691, "here-string": 3169627,
		"single-dash-touch": 31724, "single-dash-cp": 30502, "single-dash-sed": 30434, "single-dash-tee": 31723,
		"readings-single-dash": 42718,
	},
	"linux": {
		"perl": 9531, "here-string": 3169627,
		"single-dash-touch": 26562, "single-dash-cp": 25322, "single-dash-sed": 25252, "single-dash-tee": 26562,
		"readings-single-dash": 37558,
	},
}

// TestUS129FirstInspectChild is the process a case of the first row runs in,
// and nothing on its own.
func TestUS129FirstInspectChild(t *testing.T) {
	name := os.Getenv("U129_FIRST_SHAPE")
	if name == "" {
		t.Skip("the process TestAC7EachShapesFirstInspectInAFreshProcessMakesAtMostHalfItsBase runs each shape in")
	}
	for _, s := range shapes() {
		if s.name != name {
			continue
		}
		st := state(us129Resolved(t.TempDir()), s.powerShell)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		shellpolicy.Inspect(s.command, st)
		runtime.ReadMemStats(&after)
		if err := os.WriteFile(os.Getenv("U129_FIRST_SIDE"), []byte(strconv.FormatUint(after.Mallocs-before.Mallocs, 10)), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no shape %q", name)
}

func us129FirstIn(t *testing.T, name string) uint64 {
	t.Helper()
	side := filepath.Join(t.TempDir(), "mallocs")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUS129FirstInspectChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "U129_FIRST_SHAPE="+name, "U129_FIRST_SIDE="+side, "GOGC=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the first Inspect of %s: %v\n%s", name, err, out)
	}
	raw, err := os.ReadFile(side)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAC7EachShapesFirstInspectInAFreshProcessMakesAtMostHalfItsBase(t *testing.T) {
	if us129Race {
		// Under the race detector sync.Pool drops what it is handed at
		// random, so the row counts in a build without it, as AC-8's does.
		cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-v",
			"-run", "^TestAC7EachShapesFirstInspectInAFreshProcessMakesAtMostHalfItsBase$", ".")
		cmd.Env = append(os.Environ(), "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("AC-7 without the race detector:\n%s", out)
		}
		t.Logf("AC-7 without the race detector:\n%s", out)
		return
	}
	bases, ok := us129FirstBase[runtime.GOOS]
	if !ok {
		t.Skipf("AC-7's first calls were counted on macOS and Linux, whose counts differ; %s has none", runtime.GOOS)
	}
	for _, s := range shapes() {
		if !us129Named(s) {
			continue
		}
		t.Run("AC-7 "+s.name, func(t *testing.T) {
			t.Parallel()
			got := us129FirstIn(t, s.name)
			base, ok := bases[s.name]
			t.Logf("%q: %d (base %d)", s.name, got, base)
			switch {
			case !ok:
				t.Fatalf("AC-7 no base figure for the first Inspect of %s", s.name)
			case got > base/2:
				t.Errorf("AC-7 the first Inspect of the %s shape in a new process made %d allocations, want at most half the %d it made at the base, %d",
					s.name, got, base, base/2)
			}
		})
	}
}

// us129Write is the write to a frozen test that ends each shape, as Bash and
// as PowerShell spell it.
var us129Write = map[bool]string{
	false: "; echo x > internal/alpha/alpha_test.go",
	true:  "Set-Content -Path internal/alpha/alpha_test.go -Value x\n",
}

// us129Cut is s cut by room bytes where it is padded, so that the shape and
// the write together are no longer than the shape: whole here-string blocks
// from its end, or the run of a's that pads every other shape.
func us129Cut(t *testing.T, s shape, room int) string {
	t.Helper()
	if s.powerShell {
		const block = "$s = @'\nx\n'@\n"
		n := (room + len(block) - 1) / len(block)
		return s.command[:len(s.command)-n*len(block)]
	}
	at := strings.Index(s.command, strings.Repeat("a", room+64))
	if at < 0 {
		t.Fatalf("the %s shape has no padding to cut", s.name)
	}
	return s.command[:at] + s.command[at+room:]
}

// us129BaseRefusals is the sum of what Inspect refused each shape and its
// write with at 37e3ac8, the directory it was read in spelt <dir>.
var us129BaseRefusals = map[string]string{
	"perl": "99873ec44958", "here-string": "99873ec44958",
	"single-dash-touch": "99873ec44958", "single-dash-cp": "99873ec44958", "single-dash-sed": "99873ec44958",
	"single-dash-tee": "99873ec44958", "readings-single-dash": "99873ec44958",
}

func TestAC7EachAdversarialShapeWithAWriteToAFrozenTestAtItsEndIsRefusedAsAtTheBase(t *testing.T) {
	dir := us129Resolved(t.TempDir())
	for _, s := range shapes() {
		if !us129Named(s) {
			continue
		}
		t.Run("AC-7 "+s.name, func(t *testing.T) {
			write := us129Write[s.powerShell]
			command := us129Cut(t, s, len(write)) + write
			if len(command) > len(s.command) {
				t.Fatalf("the %s shape with its write is %d bytes, longer than the shape's %d", s.name, len(command), len(s.command))
			}
			f, refused := shellpolicy.Inspect(command, state(dir, s.powerShell))
			text := strings.ReplaceAll(fmt.Sprintf("rule=%q via=%q code=%q route=%q reason=%q", f.Rule, f.Via, f.Code, f.Route, f.Reason), dir, "<dir>")
			sum := sha256.Sum256([]byte(text))
			got := hex.EncodeToString(sum[:6])
			if os.Getenv("US129_PRINT") != "" {
				t.Logf("%q: %q, // %s", s.name, got, text)
			}
			if !refused {
				t.Fatalf("AC-7 the %s shape with a write to a frozen test at its end is let through", s.name)
			}
			if want := us129BaseRefusals[s.name]; got != want {
				t.Errorf("AC-7 the %s shape with a write to a frozen test at its end is refused otherwise than at the base: %s", s.name, text)
			}
		})
	}
}
