package main

import (
	"path/filepath"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// BenchmarkInspect is the leg's nine shapes, through the real
// shellpolicy.Inspect and the State the leg builds, as a benchmark: the leg
// says whether a shape is inside its budget, and this, through `./task bench`
// and benchstat, says by how much a change moved it and what it allocates.
// A State is built for every call and off the clock, as the leg builds one.
func BenchmarkInspect(b *testing.B) {
	resolved := func(dir string) string {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return r
		}
		return dir
	}
	dir, linked := resolved(b.TempDir()), resolved(b.TempDir())
	links := makeLinks(linked)
	for _, s := range shapes() {
		b.Run(s.name, func(b *testing.B) {
			at := dir
			if s.linked {
				if links != nil {
					b.Skip("no link can be made here:", links)
				}
				at = linked
			}
			if _, refused := shellpolicy.Inspect(s.command, state(at, s.powerShell)); refused != s.refused {
				b.Fatalf("Inspect answered the %s shape as %s; the leg expects it %s", s.name, answer(!s.refused), answer(s.refused))
			}
			b.ReportAllocs()
			for range b.N {
				b.StopTimer()
				st := state(at, s.powerShell)
				b.StartTimer()
				shellpolicy.Inspect(s.command, st)
			}
		})
	}
}
