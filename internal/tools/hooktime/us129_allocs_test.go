package main

// US-129 AC-7, the half that cuts: each adversarial shape of this leg that the
// criterion names makes at most half the allocations Inspect made for it at
// the story's base, counted with testing.AllocsPerRun. The State is built for
// every call, as the hook builds one, and what building it costs is counted
// on its own and taken off, so that the figure is Inspect's alone. And every
// shape of the leg is answered as it was at the base, which the corpus golden
// in internal/shellpolicy does not hold, since these commands are built here.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// us129BaseAllocs is what Inspect allocated for each shape at 37e3ac8, as
// us129Allocs counts it, by the operating system it was counted on (Gate 3):
// the most of twenty runs and five under the race detector, which spread by
// 0.7% at most. Linux allocates about 5,160 fewer for each shape than macOS,
// in the path calls the State makes, so each has its own.
var us129BaseAllocs = map[string]map[string]float64{
	"darwin": {
		"perl": 14664, "here-string": 3169674,
		"single-dash-touch": 31708, "single-dash-cp": 30479, "single-dash-sed": 30409, "single-dash-tee": 31708,
		"readings-single-dash": 42945,
	},
	"linux": {
		"perl": 9504, "here-string": 3169673,
		"single-dash-touch": 26548, "single-dash-cp": 25299, "single-dash-sed": 25229, "single-dash-tee": 26548,
		"readings-single-dash": 37785,
	},
}

// us129Sink keeps a State built only to be counted from being built on the
// stack, which the one handed to Inspect is not.
var us129Sink shellpolicy.State

func us129Allocs(s shape, dir string) float64 {
	both := testing.AllocsPerRun(2, func() { shellpolicy.Inspect(s.command, state(dir, s.powerShell)) })
	alone := testing.AllocsPerRun(2, func() { us129Sink = state(dir, s.powerShell) })
	return both - alone
}

func us129Resolved(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return dir
}

func TestAC7TheAdversarialShapesMakeAtMostHalfTheAllocationsTheyMadeAtTheBase(t *testing.T) {
	bases, ok := us129BaseAllocs[runtime.GOOS]
	if !ok {
		t.Skipf("AC-7's base was counted on macOS and Linux, whose counts differ; %s has none", runtime.GOOS)
	}
	dir := us129Resolved(t.TempDir())
	named := map[string]bool{}
	for _, s := range shapes() {
		if !strings.HasPrefix(s.name, "single-dash-") && s.name != "here-string" && s.name != "readings-single-dash" && s.name != "perl" {
			continue
		}
		named[s.name] = true
		t.Run("AC-7 "+s.name, func(t *testing.T) {
			got := us129Allocs(s, dir)
			base, ok := bases[s.name]
			if os.Getenv("US129_PRINT") != "" {
				t.Logf("%q: %.0f,", s.name, got)
			}
			switch {
			case !ok:
				t.Fatalf("AC-7 no base figure for %s", s.name)
			case got > base/2:
				t.Errorf("AC-7 Inspect made %.0f allocations for the %s shape, want at most half the %.0f it made at the base, %.0f",
					got, s.name, base, base/2)
			}
		})
	}
	for _, want := range []string{"here-string", "single-dash-touch", "single-dash-cp", "single-dash-sed", "single-dash-tee", "readings-single-dash", "perl"} {
		if !named[want] {
			t.Errorf("AC-7 the leg has no %s shape to count", want)
		}
	}
}

// us129BaseAnswers is the sum of what Inspect said of each shape at
// 37e3ac8, the directory it was read in spelt <dir>.
var us129BaseAnswers = map[string]string{
	"perl": "fb46734f25341cdd", "here-string": "fb46734f25341cdd", "dot-glob": "90e6be95ed853729",
	"single-dash-touch": "fb46734f25341cdd", "single-dash-cp": "fb46734f25341cdd",
	"single-dash-sed": "fb46734f25341cdd", "single-dash-tee": "fb46734f25341cdd",
	"links": "9e4c2e9d552db8f6", "cdpath": "fb46734f25341cdd", "readings-script": "a3a3d467a90924b2",
	"readings-single-dash": "fb46734f25341cdd",
}

func TestAC7InspectAnswersEveryShapeOfTheLegAsItDidAtTheBase(t *testing.T) {
	dir, linked := us129Resolved(t.TempDir()), us129Resolved(t.TempDir())
	links := makeLinks(linked)
	for _, s := range shapes() {
		t.Run("AC-7 "+s.name, func(t *testing.T) {
			at := dir
			if s.linked {
				if links != nil {
					t.Skip("no link can be made here:", links)
				}
				at = linked
			}
			f, refused := shellpolicy.Inspect(s.command, state(at, s.powerShell))
			answer := strings.ReplaceAll(fmt.Sprintf("refused=%v rule=%q via=%q code=%q route=%q reason=%q",
				refused, f.Rule, f.Via, f.Code, f.Route, f.Reason), filepath.ToSlash(at), "<dir>")
			sum := sha256.Sum256([]byte(answer))
			got := hex.EncodeToString(sum[:8])
			if os.Getenv("US129_PRINT") != "" {
				t.Logf("%q: %q, // %s", s.name, got, answer)
			}
			if want := us129BaseAnswers[s.name]; got != want {
				t.Errorf("AC-7 Inspect answers the %s shape otherwise than at the base: %s", s.name, answer)
			}
		})
	}
}
