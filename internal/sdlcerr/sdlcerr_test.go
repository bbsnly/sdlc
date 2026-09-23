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
// parseCodeSections reads the `### SDLC-Ennnn` sections under "## Error
// codes", and only those. The page carries other sections -- the warnings the
// hook prints have no code, because nothing failed -- and a parser that took
// every `###` on the page would insist those were codes too.
func parseCodeSections(md string) map[string]string {
	found := map[string]string{}
	heading := ""
	inCodes := false
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
			heading = ""
			if inCodes {
				heading = strings.TrimSpace(h)
			}
			continue
		}
		if section, ok := strings.CutPrefix(trimmed, "## "); ok {
			flush()
			heading = ""
			inCodes = strings.TrimSpace(section) == "Error codes"
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

// Error() is for logs, not for a person to read at the command line, so unlike
// Render it puts the cause in: a log with the code and nothing else is not
// enough to debug from.
func TestErrorAppendsTheCauseWhenThereIsOne(t *testing.T) {
	err := New(StoryNotFound, `no story with id "AUTH-9"`, "the backlog has 3 stories").
		WithCause(errors.New("open user_stories.json: permission denied"))
	got := err.Error()
	if !strings.HasSuffix(got, ": open user_stories.json: permission denied") {
		t.Errorf("Error() = %q, want the cause after the code, separated by a colon", got)
	}
}

// A code is API: scripts match on it and docs/troubleshooting.md is linked by
// it, so the number behind each name is never changed or handed to another
// error. The catalogue refuses a duplicate on its own; this is what refuses a
// renumbering, by making it a second, deliberate edit. A new code adds a row.
func TestEveryCodeKeepsTheNumberItWasReleasedWith(t *testing.T) {
	released := []struct {
		code Code
		id   string
	}{
		{NotAGitRepo, "SDLC-E0001"},
		{NotInitialised, "SDLC-E0002"},
		{AlreadyInitialised, "SDLC-E0003"},
		{ConfigUnreadable, "SDLC-E0004"},
		{StateUnreadable, "SDLC-E0005"},
		{StateUnwritable, "SDLC-E0006"},
		{BacklogMissing, "SDLC-E0007"},
		{BacklogUnreadable, "SDLC-E0008"},
		{StoryNotFound, "SDLC-E0009"},
		{NoRunnableStory, "SDLC-E0010"},
		{NoActiveIteration, "SDLC-E0011"},
		{IterationAlreadyActive, "SDLC-E0012"},
		{StoryAlreadyFinished, "SDLC-E0033"},
		{BadArgument, "SDLC-E0034"},
		{NothingToApprove, "SDLC-E0035"},
		{AwaitingPerson, "SDLC-E0036"},
		{ApprovalRequired, "SDLC-E0037"},
		{NotOnTrunk, "SDLC-E0038"},
		{UncommittedWork, "SDLC-E0039"},
		{TrunkBehind, "SDLC-E0040"},
		{TrunkBroken, "SDLC-E0041"},
		{UnfrozenTests, "SDLC-E0043"},
		{NoAcceptanceCriteria, "SDLC-E0044"},
		{UnknownGate, "SDLC-E0013"},
		{UnknownGateStatus, "SDLC-E0014"},
		{UnknownArtifact, "SDLC-E0016"},
		{EmptyArtifact, "SDLC-E0017"},
		{ArtifactUnreadable, "SDLC-E0018"},
		{AlreadyFrozen, "SDLC-E0022"},
		{NotFrozen, "SDLC-E0023"},
		{NoTestsFound, "SDLC-E0024"},
		{FreezeBroken, "SDLC-E0025"},
		{ReasonRequired, "SDLC-E0026"},
		{UnknownReviewer, "SDLC-E0027"},
		{UnknownVerdict, "SDLC-E0028"},
		{ReviewsMissing, "SDLC-E0029"},
		{ReviewBlocks, "SDLC-E0030"},
		{GateOutOfOrder, "SDLC-E0031"},
		{TreeNotCommitted, "SDLC-E0032"},
		{RepositoryUnreadable, "SDLC-E0021"},
		{GateDocumentsMissing, "SDLC-E0020"},
		{ArtifactTooLarge, "SDLC-E0019"},
		{UnsafeStoryID, "SDLC-E0015"},
		{ConfigInvalid, "SDLC-E0045"},
		{TrunkMoved, "SDLC-E0046"},
		{AcknowledgedUnknown, "SDLC-E0047"},
		{SessionElsewhere, "SDLC-E0048"},
		{NoteOutsideRange, "SDLC-E0049"},
	}
	for _, r := range released {
		if got := r.code.String(); got != r.id {
			t.Errorf("%s now renders as %s: a released code was renumbered", r.id, got)
		}
	}
	if len(released) != len(catalogue) {
		t.Errorf("the catalogue has %d codes and %d are pinned here; add the new one", len(catalogue), len(released))
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
