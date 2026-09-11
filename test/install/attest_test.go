package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Everything a release serves has to carry provenance, and the install scripts
// are served: goreleaser ships them as release assets and the notes link them
// at the tag, where the bytes cannot change. The attestation step listed only
// what goreleaser builds, so the one file people pipe into a shell was the one
// file with nothing behind it -- while the documentation said otherwise.
func TestEveryReleaseAssetIsAttested(t *testing.T) {
	root := repoRoot(t)
	attested := subjectPaths(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	for _, asset := range extraFiles(t, filepath.Join(root, ".goreleaser.yml")) {
		if !attested[asset] {
			t.Errorf("%s goes out with the release and is not in the attestation's "+
				"subject-path: an unattested installer is the weakest link in a "+
				"chain that attests everything it installs", asset)
		}
	}
}

// subjectPaths reads the block under "subject-path: |". A YAML dependency to
// read two lists would be the largest thing in go.mod, and the shape being
// read here is fixed by the file it lives in.
func subjectPaths(t *testing.T, path string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	inBlock := false
	for _, line := range lines(t, path) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "subject-path:"):
			inBlock = true
		case !inBlock:
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		case strings.HasSuffix(trimmed, ":"), strings.HasPrefix(trimmed, "- "):
			inBlock = false
		default:
			found[trimmed] = true
		}
	}
	if len(found) == 0 {
		t.Fatalf("no subject-path entries found in %s", path)
	}
	return found
}

// extraFiles reads every "- glob: X" in .goreleaser.yml: the files the release
// carries beyond what it builds.
func extraFiles(t *testing.T, path string) []string {
	t.Helper()
	var globs []string
	seen := map[string]bool{}
	for _, line := range lines(t, path) {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "- glob:")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			globs = append(globs, name)
		}
	}
	if len(globs) == 0 {
		t.Fatalf("no extra_files globs found in %s", path)
	}
	return globs
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(raw), "\n")
}
