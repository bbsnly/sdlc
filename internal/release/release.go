// Package release answers the three questions a release asks of the
// repository: what version is this, do the files that repeat that number
// agree, and what does the changelog say about it.
//
// The number lives in more than one file because more than one packaging
// format needs it -- the plugin manifest Claude Code reads, the npm package
// that ships the binary. Nothing generates one from another, because a
// generated manifest is a file nobody reads until it is wrong. Instead every
// copy is checked against the one source of truth on every run of `task
// check`, so a forgotten bump fails on the commit that forgot it rather than
// during a release.
//
// This package is developer-facing. Its errors are read by whoever is cutting
// the release, not by a user of the product, so they are ordinary errors.
package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Source is the file that decides what version this repository is at.
// Claude Code refuses to load a plugin whose manifest is wrong, so this
// number is already load-bearing; making it the source of truth means the
// file that must be right is the file everything else is checked against.
const Source = "plugin/.claude-plugin/plugin.json"

// ChangelogFile is where a release's notes come from. Release notes are
// written by a person, in the repository, reviewed like anything else --
// not generated from commit subjects at tag time.
const ChangelogFile = "CHANGELOG.md"

// carriers are the other files that repeat the version. Each is read as JSON
// and asked for its "version" key.
//
// A carrier is listed here from the commit that introduces it. A carrier that
// is "optional until it exists" is a check that passes by being incomplete.
var carriers = []string{
	"npm/package.json",
}

// semver is deliberately strict: three numbers, an optional pre-release, no
// build metadata. A build tag would have to survive a filename, an npm
// version and a git tag, and the first of those would mangle it.
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?$`)

// Problem is one thing wrong with the release metadata. It names the file so
// the person reading it knows where to go.
type Problem struct {
	File string
	Why  string
}

func (p Problem) String() string {
	if p.File == "" {
		return p.Why
	}
	return p.File + ": " + p.Why
}

// Version reports the version this repository is at, read from Source.
func Version(root string) (string, error) {
	v, err := versionIn(filepath.Join(root, filepath.FromSlash(Source)))
	if err != nil {
		return "", err
	}
	if !semver.MatchString(v) {
		return "", fmt.Errorf("%s: version %q is not a semantic version", Source, v)
	}
	return v, nil
}

// Check reports everything wrong with this repository's release metadata. An
// empty tag means "not releasing, just checking the repository is coherent";
// a non-empty one is checked against the version as well, which is what the
// release workflow does before it builds anything.
func Check(root, tag string) ([]Problem, error) {
	version, err := Version(root)
	if err != nil {
		return nil, err
	}

	var problems []Problem
	for _, carrier := range carriers {
		got, err := versionIn(filepath.Join(root, filepath.FromSlash(carrier)))
		switch {
		case errors.Is(err, os.ErrNotExist):
			problems = append(problems, Problem{carrier, "does not exist, but it is listed as carrying the version"})
			continue
		case err != nil:
			return nil, err
		case got != version:
			problems = append(problems, Problem{carrier,
				fmt.Sprintf("says %q, and %s says %q", got, Source, version)})
		}
	}

	notes, err := Notes(root, version)
	switch {
	case errors.Is(err, errNoSection):
		problems = append(problems, Problem{ChangelogFile,
			fmt.Sprintf("has no section for %s; a release is what the changelog says it is", version)})
	case err != nil:
		return nil, err
	case strings.TrimSpace(notes) == "":
		problems = append(problems, Problem{ChangelogFile,
			fmt.Sprintf("the section for %s is empty", version)})
	}

	if tag != "" && tag != "v"+version {
		problems = append(problems, Problem{"",
			fmt.Sprintf("tag %s does not match version %s from %s; tags are v-prefixed", tag, version, Source)})
	}
	return problems, nil
}

// errNoSection distinguishes "the changelog does not mention this version"
// from "the changelog could not be read", because the first is a problem to
// report and the second is a problem to stop on.
var errNoSection = errors.New("no such section")

// Notes returns the changelog's section for one version, without its heading.
//
// The heading may carry a date and a link, as Keep a Changelog suggests; only
// the bracketed version is matched.
func Notes(root, version string) (string, error) {
	body, err := os.ReadFile(filepath.Join(root, ChangelogFile))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")

	// The closing bracket is what makes this exact: "## [0.1.0]" cannot
	// prefix-match the heading for 0.1.10.
	want := "## [" + strings.TrimPrefix(version, "v") + "]"
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, want) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("%s for %s: %w", ChangelogFile, version, errNoSection)
	}

	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), nil
}

// versionIn reads the "version" key out of a JSON file.
func versionIn(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", filepath.ToSlash(path), err)
	}
	if doc.Version == "" {
		return "", fmt.Errorf("%s: has no \"version\"", filepath.ToSlash(path))
	}
	return doc.Version, nil
}
