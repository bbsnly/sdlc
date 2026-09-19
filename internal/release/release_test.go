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
// The README goes into every archive. Cutting a release without rewriting the
// sentence that says there is no release ships a README that denies it exists.
func TestAReleaseCannotShipPagesThatSayThereIsNoRelease(t *testing.T) {
	root := fixture(t, "1.2.3", oneRelease)
	write(t, root, "README.md", "**The first release is not\ntagged yet**, so build from source.\n")
	write(t, root, "docs/installation.md", "Use Go while there is no release.\n")

	problems, err := Check(root, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, p := range problems {
		files = append(files, p.File)
	}
	if got := strings.Join(files, ","); got != "README.md,docs/installation.md" {
		t.Errorf("want a problem for each page, got %v", problems)
	}

	// Before a tag the same sentences are true, and nothing complains.
	if problems, err := Check(root, ""); err != nil || len(problems) != 0 {
		t.Errorf("an ordinary check objected to pre-release wording: %v %v", problems, err)
	}
}

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

// A Problem without a file is a tag mismatch, which names no file; one with a
// file always leads with it, so the person fixing it knows where to look.
func TestProblemStringLeadsWithTheFileOnlyWhenThereIsOne(t *testing.T) {
	if got := (Problem{File: "npm/package.json", Why: "says the wrong thing"}).String(); got != "npm/package.json: says the wrong thing" {
		t.Errorf("String() = %q", got)
	}
	if got := (Problem{Why: "the tag does not match"}).String(); got != "the tag does not match" {
		t.Errorf("String() = %q", got)
	}
}

// Version is read straight from Source; a repository missing that file has
// nothing to check against and must fail rather than report an empty version.
func TestVersionFailsWhenTheSourceFileIsMissing(t *testing.T) {
	if _, err := Version(t.TempDir()); err == nil {
		t.Fatal("want an error when plugin.json does not exist")
	} else if !strings.Contains(err.Error(), "plugin.json") {
		t.Errorf("err should name the file it could not read, got %v", err)
	}
}

func TestVersionFailsOnMalformedJSON(t *testing.T) {
	root := t.TempDir()
	write(t, root, Source, "{not json")
	if _, err := Version(root); err == nil {
		t.Fatal("want an error on malformed JSON")
	} else if !strings.Contains(err.Error(), filepath.ToSlash(Source)) {
		t.Errorf("err should name the file, got %v", err)
	}
}

func TestVersionFailsWhenTheVersionKeyIsMissing(t *testing.T) {
	root := t.TempDir()
	write(t, root, Source, `{"name":"sdlc"}`)
	if _, err := Version(root); err == nil || !strings.Contains(err.Error(), `has no "version"`) {
		t.Errorf("err = %v, want it to say there is no version key", err)
	}
}

// Check's own preflight is Version; a repository that fails that has nothing
// for Check to report a Problem about, so Check must fail too, not report an
// empty problem list as if the repository were coherent.
func TestCheckFailsRatherThanReportingNothingWhenTheSourceIsMissing(t *testing.T) {
	problems, err := Check(t.TempDir(), "")
	if err == nil {
		t.Fatal("want an error, not a silently empty problem list")
	}
	if problems != nil {
		t.Errorf("problems = %v, want none alongside an error", problems)
	}
}

// A carrier that fails to parse is a different failure from one that is merely
// out of date, and Check must stop rather than silently skip it.
func TestCheckFailsWhenACarrierFailsToParse(t *testing.T) {
	root := fixture(t, "1.2.3", oneRelease)
	write(t, root, carriers[0], "{not json")

	problems, err := Check(root, "")
	if err == nil {
		t.Fatal("want an error, not a reported Problem, for a carrier that will not parse")
	}
	if problems != nil {
		t.Errorf("problems = %v, want none alongside an error", problems)
	}
}

// A changelog Check cannot even read is a different failure from one with no
// section for this version; only the second is a Problem to report.
func TestCheckFailsWhenTheChangelogCannotBeRead(t *testing.T) {
	root := fixture(t, "1.2.3", oneRelease)
	if err := os.Remove(filepath.Join(root, ChangelogFile)); err != nil {
		t.Fatal(err)
	}

	problems, err := Check(root, "")
	if err == nil {
		t.Fatal("want an error, not a reported Problem, for a changelog that cannot be read at all")
	}
	if problems != nil {
		t.Errorf("problems = %v, want none alongside an error", problems)
	}
}

// prereleaseClaims reads the README to catch it saying there is no release;
// when it cannot even read the page, Check must say so rather than reporting
// a clean repository.
func TestCheckFailsWhenThePrereleaseCheckCannotReadAPage(t *testing.T) {
	root := fixture(t, "1.2.3", oneRelease)
	if err := os.RemoveAll(filepath.Join(root, "README.md")); err != nil {
		t.Fatal(err)
	}
	// A directory named README.md fails to read for a different reason than
	// "does not exist", which is the one prereleaseClaims tolerates.
	if err := os.MkdirAll(filepath.Join(root, "README.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	problems, err := Check(root, "v1.2.3") // only checked when a tag is being cut
	if err == nil {
		t.Fatal("want an error, not a reported Problem, when the page cannot be read")
	}
	if problems != nil {
		t.Errorf("problems = %v, want none alongside an error", problems)
	}
}
