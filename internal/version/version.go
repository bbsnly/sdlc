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

// Info is what this build knows about itself.
type Info struct {
	Version string // semver, or "0.0.0-dev" for an untagged build
	Commit  string // full commit sha, or "" when unknown
	Date    string // RFC3339 build time, or "" when unknown
	Dirty   bool   // built from a tree with uncommitted changes
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
	info := Info{Version: version, Commit: commit, Date: date}

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
