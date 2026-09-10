package sdlcerr

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// troubleshootingPath is the page the catalogue is keyed on, relative to this
// package.
const troubleshootingPath = "../../docs/troubleshooting.md"

var codePattern = regexp.MustCompile(`^SDLC-E[0-9]{4}$`)

func TestCatalogueCodesAreWellFormedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range catalogue {
		if !codePattern.MatchString(e.code.id) {
			t.Errorf("code %q does not match SDLC-Ennnn", e.code.id)
		}
		if seen[e.code.id] {
			t.Errorf("code %q is registered for two conditions; codes are API and are never reused", e.code.id)
		}
		seen[e.code.id] = true
	}
	if len(seen) != len(catalogue) {
		t.Fatalf("catalogue has %d entries but %d distinct codes", len(catalogue), len(seen))
	}
}

func TestEveryCodeHasAFix(t *testing.T) {
	for _, e := range catalogue {
		if strings.TrimSpace(e.fix) == "" {
			t.Errorf("%s has no fix; an error the user cannot act on is not finished", e.code.id)
		}
	}
}

func TestEveryCodeHasATroubleshootingHeading(t *testing.T) {
	documented := documentedCodes(t)
	for _, e := range catalogue {
		if _, ok := documented[e.code.id]; !ok {
			t.Errorf("%s has no `### %s` heading in %s", e.code.id, e.code.id, troubleshootingPath)
		}
	}
}

// A heading with no live code behind it is either a code retired by deleting its
// section — which loses the meaning a reader in an old log still needs — or a
// typo. A retired code keeps its section and says so.
func TestEveryTroubleshootingHeadingIsLiveOrRetired(t *testing.T) {
	for id, body := range documentedCodes(t) {
		_, live := byID[id]
		switch {
		case live && isRetired(body):
			t.Errorf("%s is in the catalogue but its section in %s is marked **Retired.**",
				id, troubleshootingPath)
		case !live && !isRetired(body):
			t.Errorf("%s has a section in %s but is not in the catalogue; "+
				"if it was retired, mark the section with a leading **Retired.**",
				id, troubleshootingPath)
		}
	}
}

func isRetired(body string) bool {
	return strings.HasPrefix(strings.TrimSpace(body), "**Retired.**")
}

// documentedCodes maps each `### SDLC-Ennnn` heading in the real page to the
// prose under it.
func documentedCodes(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(troubleshootingPath))
	if err != nil {
		t.Fatalf("read %s: %v", troubleshootingPath, err)
	}
	found := parseCodeSections(string(raw))
	if len(found) == 0 {
		t.Fatalf("%s has no `### SDLC-Ennnn` headings at all", troubleshootingPath)
	}
	return found
}

// parseCodeSections splits a Markdown page into its `### ` sections. A `## `
// heading closes the current section, so prose after the code list is not
// attributed to the last code.
func parseCodeSections(md string) map[string]string {
	found := map[string]string{}
	heading := ""
	var body strings.Builder
	flush := func() {
		if heading != "" {
			found[heading] = body.String()
		}
		body.Reset()
	}
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if h, ok := strings.CutPrefix(trimmed, "### "); ok {
			flush()
			heading = strings.TrimSpace(h)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			heading = ""
			continue
		}
		body.WriteString(line + "\n")
	}
	flush()
	return found
}

func TestParseCodeSectionsSeparatesLiveFromRetired(t *testing.T) {
	sections := parseCodeSections(`# Troubleshooting

## Error codes

### SDLC-E0001

Live prose.

### SDLC-E0002

**Retired.** Superseded by SDLC-E0001 in v2.0.0.

## Something else

Trailing prose that belongs to no code.
`)

	if len(sections) != 2 {
		t.Fatalf("parsed %d sections, want 2: %v", len(sections), sections)
	}
	if isRetired(sections["SDLC-E0001"]) {
		t.Error("a live section was read as retired")
	}
	if !isRetired(sections["SDLC-E0002"]) {
		t.Error("a **Retired.** section was not recognised")
	}
	if strings.Contains(sections["SDLC-E0002"], "Trailing prose") {
		t.Error("a `## ` heading did not close the preceding section")
	}
}

func TestNewTakesTheFixFromTheCatalogue(t *testing.T) {
	err := New(NotInitialised, "this project has not been initialised", "there is no .sdlc/config.json here")
	if err.Fix != fixFor(NotInitialised) {
		t.Errorf("Fix = %q, want the catalogue entry %q", err.Fix, fixFor(NotInitialised))
	}
	if err.Fix == "" {
		t.Error("Fix is empty; New must never produce an error without one")
	}
}

func TestWithFixAndWithCauseDoNotMutateTheReceiver(t *testing.T) {
	base := New(BacklogMissing, "no backlog", "backlog.path names a file that is not there")
	specific := base.WithFix(`create user_stories.json`).WithCause(fs.ErrNotExist)

	if base.Fix == specific.Fix {
		t.Error("WithFix did not change the copy's fix")
	}
	if base.Unwrap() != nil {
		t.Error("WithCause mutated the receiver; it must return a copy")
	}
	if !errors.Is(specific, fs.ErrNotExist) {
		t.Error("errors.Is cannot see through the error to its cause")
	}
}

func TestErrorIsOneLineAndCarriesTheCode(t *testing.T) {
	err := New(StoryNotFound, `no story with id "AUTH-9"`, "the backlog has 3 stories")
	got := err.Error()
	if strings.Contains(got, "\n") {
		t.Errorf("Error() spans lines, which breaks log wrapping: %q", got)
	}
	if !strings.Contains(got, "SDLC-E0009") {
		t.Errorf("Error() = %q, want it to name the code", got)
	}
}

func TestRenderShowsWhatWhyFixAndAResolvableLink(t *testing.T) {
	err := New(NotAGitRepo, "this is not a Git repository", "sdlc keeps its state beside your code")
	got := err.Render()

	for _, want := range []string{
		"sdlc: this is not a Git repository",
		"why  sdlc keeps its state beside your code",
		"fix  " + fixFor(NotAGitRepo),
		"SDLC-E0001",
		"troubleshooting.md#sdlc-e0001",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() is missing %q:\n%s", want, got)
		}
	}
}

// The cause is for errors.Is, not for the reader: a wrapped syscall message
// under "why" is the stack trace this package exists to avoid.
func TestRenderDoesNotLeakTheCause(t *testing.T) {
	err := New(StateUnreadable, "the loop's state could not be read", "a file under .sdlc/state is unreadable").
		WithCause(errors.New("open /x/.sdlc/state/active: permission denied"))
	if strings.Contains(err.Render(), "permission denied") {
		t.Errorf("Render() leaked the underlying cause:\n%s", err.Render())
	}
}

func TestRenderFallsBackForOrdinaryErrors(t *testing.T) {
	got := Render(errors.New("something plain"))
	if got != "sdlc: something plain\n" {
		t.Errorf("Render(plain) = %q", got)
	}
}

func TestRenderFindsAWrappedError(t *testing.T) {
	inner := New(NoRunnableStory, "nothing to work on", "every story is done or blocked")
	got := Render(fmt.Errorf("selecting a story: %w", inner))
	if !strings.Contains(got, "SDLC-E0010") {
		t.Errorf("Render() did not unwrap to the rich form:\n%s", got)
	}
}

func TestRegisterRejectsADuplicateCode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a duplicate code did not panic")
		}
	}()
	register(catalogue[0].code.id, "some fix")
}

func TestRegisterRejectsAnEmptyFix(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a code with no fix did not panic")
		}
	}()
	register("SDLC-E9999", "")
}
