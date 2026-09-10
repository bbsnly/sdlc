// Package testset answers one question: is this file one of the project's
// tests?
//
// The loop freezes tests and refuses to let the agent that implements a story
// edit them, so the answer decides what is protected. It comes from the
// project's own configuration rather than from a guess about the language,
// because every ecosystem names its tests differently and a wrong guess here
// either protects nothing or protects everything.
package testset

import (
	"path"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// Matcher decides whether a repository-relative, slash-separated path is a test.
type Matcher struct {
	dirs  []string
	globs []string
}

// New reads the project's conventions.
func New(p config.TestPaths) Matcher {
	m := Matcher{}
	for _, d := range p.Dirs {
		if d = strings.Trim(strings.TrimSpace(d), "/"); d != "" {
			m.dirs = append(m.dirs, d)
		}
	}
	for _, g := range p.FileGlobs {
		if g = strings.TrimSpace(g); g != "" {
			m.globs = append(m.globs, g)
		}
	}
	return m
}

// Configured reports whether the project said anything at all. A project that
// named no test directories and no patterns cannot have its tests frozen, and
// saying so is better than freezing nothing and calling it done.
func (m Matcher) Configured() bool { return len(m.dirs) > 0 || len(m.globs) > 0 }

// Match reports whether this path is a test file.
//
// A directory entry matches everything beneath it. A pattern matches either the
// whole path or the file's own name, so "*_test.go" finds internal/x_test.go
// without every project having to write "**/*_test.go".
func (m Matcher) Match(rel string) bool {
	rel = strings.TrimPrefix(path.Clean(strings.TrimSpace(rel)), "./")
	if rel == "" || rel == "." {
		return false
	}
	if pathrules.UnderAny(rel, m.dirs...) {
		return true
	}
	base := path.Base(rel)
	for _, g := range m.globs {
		if ok, err := path.Match(g, rel); err == nil && ok {
			return true
		}
		if ok, err := path.Match(g, base); err == nil && ok {
			return true
		}
	}
	return false
}

// Filter keeps only the test files, in the order it was given them.
func (m Matcher) Filter(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if m.Match(p) {
			out = append(out, p)
		}
	}
	return out
}
