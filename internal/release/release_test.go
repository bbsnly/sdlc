package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a repository root whose release metadata is coherent, so a
// test can break exactly one thing and see only that break reported.
func fixture(t *testing.T, version, changelog string) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, Source, `{"name":"sdlc","version":"`+version+`"}`)
	for _, carrier := range carriers {
		write(t, root, carrier, `{"name":"@bbsnly/sdlc","version":"`+version+`"}`)
	}
	write(t, root, ChangelogFile, changelog)
	return root
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const oneRelease = `# Changelog

## [Unreleased]

## [1.2.3] - 2026-01-01

### Added

- A thing.

## [1.2.2] - 2025-12-01

- An older thing.
`

func TestACoherentRepositoryHasNothingToReport(t *testing.T) {
	problems, err := Check(fixture(t, "1.2.3", oneRelease), "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected nothing wrong, got %v", problems)
	}
}

// The whole point of the check: one file bumped, another forgotten.
func TestAForgottenCarrierIsReported(t *testing.T) {
	root := fixture(t, "1.2.3", oneRelease)
	write(t, root, carriers[0], `{"version":"1.2.2"}`)

	problems, err := Check(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].File != carriers[0] {
		t.Fatalf("expected %s to be reported, got %v", carriers[0], problems)
	}
	if !strings.Contains(problems[0].Why, "1.2.2") || !strings.Contains(problems[0].Why, "1.2.3") {
		t.Errorf("the message should name both versions, got %q", problems[0].Why)
	}
}

func TestAVersionWithNoChangelogSectionIsReported(t *testing.T) {
	problems, err := Check(fixture(t, "9.9.9", oneRelease), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].File != ChangelogFile {
		t.Fatalf("expected the changelog to be reported, got %v", problems)
	}
}

func TestAHeadingWithNothingUnderItIsNotARelease(t *testing.T) {
	problems, err := Check(fixture(t, "1.2.3", "# Changelog\n\n## [1.2.3]\n\n## [1.2.2]\n\n- old\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Why, "empty") {
		t.Fatalf("expected the empty section to be reported, got %v", problems)
	}
}

func TestATagThatDoesNotMatchTheVersionIsReported(t *testing.T) {
	problems, err := Check(fixture(t, "1.2.3", oneRelease), "v1.2.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Why, "v1.2.4") {
		t.Fatalf("expected the tag to be reported, got %v", problems)
	}
}

// A tag is checked only when one is being cut. `task check` runs on every
// commit, where there is no tag and nothing to compare.
func TestNoTagMeansNoTagCheck(t *testing.T) {
	problems, err := Check(fixture(t, "1.2.3", oneRelease), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected nothing wrong, got %v", problems)
	}
}

func TestNotesAreTheSectionAndNothingAround(t *testing.T) {
	notes, err := Notes(fixture(t, "1.2.3", oneRelease), "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if notes != "### Added\n\n- A thing." {
		t.Fatalf("got %q", notes)
	}
}

// "## [1.2.3]" must not match the heading of 1.2.30, which is what a prefix
// match without the closing bracket would do.
func TestAVersionIsNotAPrefixOfAnother(t *testing.T) {
	changelog := "# Changelog\n\n## [1.2.30]\n\n- thirty\n\n## [1.2.3]\n\n- three\n"
	notes, err := Notes(fixture(t, "1.2.3", changelog), "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if notes != "- three" {
		t.Fatalf("got %q", notes)
	}
}

func TestAVersionThatIsNotSemverIsRefused(t *testing.T) {
	for _, bad := range []string{"v1.2.3", "1.2", "1.2.3+build", "latest", "01.2.3"} {
		root := fixture(t, bad, oneRelease)
		if _, err := Version(root); err == nil {
			t.Errorf("%q was accepted as a version", bad)
		}
	}
}

func TestAMissingCarrierIsReportedRatherThanIgnored(t *testing.T) {
	root := fixture(t, "1.2.3", oneRelease)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(carriers[0]))); err != nil {
		t.Fatal(err)
	}
	problems, err := Check(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Why, "does not exist") {
		t.Fatalf("expected the missing carrier to be reported, got %v", problems)
	}
}

// The check that matters: this repository, as it stands right now.
func TestThisRepositoryIsCoherent(t *testing.T) {
	problems, err := Check("../..", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("%s", p)
	}
}
