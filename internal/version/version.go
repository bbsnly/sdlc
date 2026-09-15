// Package version reports what build this is.
//
// Values are injected at release time with -ldflags. When they are absent --
// a `go install`, a `go run`, a local build -- they are recovered from the
// module's build info, so the binary can always say something true about
// itself rather than nothing.
package version

import (
	"runtime/debug"
	"sync"
)

// Injected by the release build. Do not set these anywhere else.
var (
	version = ""
	commit  = ""
	date    = ""
)

// Protocol is what the plugin's runbook can count on from this build: the
// commands, flags and JSON fields it reads, and the rules the hook enforces
// under it. It goes up when the runbook starts relying on something an older
// build does not have, so that a runbook newer than the sdlc on PATH can tell,
// and stop, rather than work gates that sdlc does not enforce.
const Protocol = 1

// Info is what this build knows about itself.
type Info struct {
	Version string `json:"version"` // semver, or "0.0.0-dev" for an untagged build
	Commit  string `json:"commit"`  // full commit sha, or "" when unknown
	// RFC3339, or "" when unknown. A released build carries the *commit*
	// date, not the time it was built: a build time would differ on every
	// rebuild of the same tag and make the binary unreproducible.
	Date     string `json:"date"`
	Dirty    bool   `json:"dirty"`    // built from a tree with uncommitted changes
	Protocol int    `json:"protocol"` // [Protocol]
}

var (
	once   sync.Once
	cached Info
)

// Get returns this build's identity. It is safe for concurrent use and reads
// build info at most once.
func Get() Info {
	once.Do(func() { cached = compute() })
	return cached
}

func compute() Info {
	info := Info{Version: version, Commit: commit, Date: date, Protocol: Protocol}

	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			case "vcs.modified":
				info.Dirty = s.Value == "true"
			}
		}
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
	}
	if info.Version == "" {
		info.Version = "0.0.0-dev"
	}
	return info
}

// String renders the one line `sdlc version` prints.
func (i Info) String() string {
	s := i.Version
	if i.Commit != "" {
		short := i.Commit
		if len(short) > 12 {
			short = short[:12]
		}
		s += " (" + short
		if i.Dirty {
			s += ", dirty"
		}
		s += ")"
	}
	return s
}
