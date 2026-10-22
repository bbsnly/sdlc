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
	for line := range strings.SplitSeq(md, "\n") {
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
	type pin struct {
		code Code
		id   string
	}
	released := []pin{
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
		{StoryHeldElsewhere, "SDLC-E0050"},
		{HoldingCheckoutMissing, "SDLC-E0051"},
		{NoMainCheckout, "SDLC-E0052"},
		{WorktreeNotMade, "SDLC-E0053"},
		{BranchBehindTrunk, "SDLC-E0054"},
		{LandingCheckFailed, "SDLC-E0055"},
		{UncommittedOnTrunk, "SDLC-E0056"},
		{ParallelUnknownStory, "SDLC-E0057"},
		{ParallelNotRunnable, "SDLC-E0058"},
		{ParallelHeldElsewhere, "SDLC-E0059"},
		{ParallelListedTwice, "SDLC-E0060"},
		{ParallelFromASession, "SDLC-E0061"},
		{ParallelNeedsWorktrees, "SDLC-E0062"},
		{ParallelBadMax, "SDLC-E0063"},
		{ParallelAwaitingPerson, "SDLC-E0064"},
		{ParallelFinished, "SDLC-E0065"},
		{ParallelAllowlistWidened, "SDLC-E0066"},
		// US-026: the staffing's eight. They are found by number, since the
		// number is what is released and the name in the source is not;
		// TestAC4AC5AC8TheStaffingCodesAreDocumentedAndSayWhatToDo says what
		// each is for.
		{registered("SDLC-E0067"), "SDLC-E0067"},
		{registered("SDLC-E0068"), "SDLC-E0068"},
		{registered("SDLC-E0069"), "SDLC-E0069"},
		{registered("SDLC-E0070"), "SDLC-E0070"},
		{registered("SDLC-E0071"), "SDLC-E0071"},
		{registered("SDLC-E0072"), "SDLC-E0072"},
		{registered("SDLC-E0073"), "SDLC-E0073"},
		{registered("SDLC-E0074"), "SDLC-E0074"},
	}
	// US-088: the landing may give a refusal of its own a code of its own --
	// the story's commits not applying onto trunk, which goes to a person --
	// or say it under a code it has; that is the plan's to choose, after this
	// file is frozen with the story's tests. The next number is therefore
	// pinned here ahead of the code: counted once it is registered, and until
	// then standing for nothing. A new code under any other number still
	// fails the count below.
	if c := registered("SDLC-E0075"); c != (Code{}) {
		released = append(released, pin{c, "SDLC-E0075"})
	}
	// US-089: a story's map change that did not reach trunk has a code of its
	// own, and the plan may give the repair's refusals theirs. Other stories
	// are under way beside it and may land first with the next number, so the
	// next five are pinned ahead of their codes, as SDLC-E0075 was: each is
	// counted once it is registered and stands for nothing until then.
	// TestAC6TheMapChangeThatDidNotReachTrunkHasACodeThatSaysWhatAPersonRuns
	// holds that the code is there, under whichever of them it has.
	for _, id := range []string{"SDLC-E0076", "SDLC-E0077", "SDLC-E0078", "SDLC-E0080"} {
		if c := registered(id); c != (Code{}) {
			released = append(released, pin{c, id})
		}
	}
	// US-108 AC-15: a person's command run inside Claude Code's environment
	// is refused under a code of its own, the next after SDLC-E0078. It is
	// released with v0.5.0, so it is pinned outright, not ahead of its code.
	released = append(released, pin{registered("SDLC-E0079"), "SDLC-E0079"})
	for _, r := range released {
		if got := r.code.String(); got != r.id {
			t.Errorf("%s now renders as %s: a released code was renumbered", r.id, got)
		}
	}
	if len(released) != len(catalogue) {
		t.Errorf("the catalogue has %d codes and %d are pinned here; add the new one", len(catalogue), len(released))
	}
}

// US-029.3 lands a finished story on trunk, and adds a code for each way that
// landing is refused on its own account: trunk moved since the story's branch
// was cut (AC-3), a configured command failing on the tree about to land
// (AC-4), and the checkout that has trunk checked out holding changes the
// fast-forward would write over (AC-6). Each is a heading a person is sent to
// from the refusal, and each fix says what to do next.
func TestAC3AC4AndAC6TheLandingCodesAreDocumentedAndSayWhatToDo(t *testing.T) {
	documented := documentedCodes(t)
	for _, c := range []struct {
		code Code
		id   string
		// fix is a word the fix has to carry for a person to act on it.
		fix string
	}{
		{BranchBehindTrunk, "SDLC-E0054", "rebase"},
		{LandingCheckFailed, "SDLC-E0055", "commands"},
		{UncommittedOnTrunk, "SDLC-E0056", "commit"},
	} {
		if body, ok := documented[c.id]; !ok || isRetired(body) {
			t.Errorf("%s has no live `### %s` section in %s", c.id, c.id, troubleshootingPath)
		}
		if fix := fixFor(c.code); !strings.Contains(fix, c.fix) {
			t.Errorf("%s's fix does not say %q: %s", c.id, c.fix, fix)
		}
	}
}

// US-058 AC-9: `sdlc parallel` adds a code for each way it refuses, and each is
// a heading a person is sent to from the refusal, with a fix that says what to
// do next. AC-3's four are the first four; the rest are the person-only rule
// (AC-4), the worktrees switch, an invalid --max (AC-2), the two re-run
// refusals the person's answer left standing (a pending escalation and a
// finished story), and a configured allowlist that widens the built-in one
// (AC-5).
func TestAC9TheParallelCodesAreDocumentedAndSayWhatToDo(t *testing.T) {
	documented := documentedCodes(t)
	for _, c := range []struct {
		code Code
		id   string
		// fix is a word the fix has to carry for a person to act on it.
		fix string
	}{
		{ParallelUnknownStory, "SDLC-E0057", "backlog"},
		{ParallelNotRunnable, "SDLC-E0058", "ready"},
		{ParallelHeldElsewhere, "SDLC-E0059", "release"},
		{ParallelListedTwice, "SDLC-E0060", "once"},
		{ParallelFromASession, "SDLC-E0061", "terminal"},
		{ParallelNeedsWorktrees, "SDLC-E0062", "git.worktrees"},
		{ParallelBadMax, "SDLC-E0063", "--max"},
		{ParallelAwaitingPerson, "SDLC-E0064", "approve"},
		{ParallelFinished, "SDLC-E0065", "reopen"},
		{ParallelAllowlistWidened, "SDLC-E0066", "parallel.allow"},
	} {
		if body, ok := documented[c.id]; !ok || isRetired(body) {
			t.Errorf("%s has no live `### %s` section in %s", c.id, c.id, troubleshootingPath)
		}
		if fix := fixFor(c.code); !strings.Contains(fix, c.fix) {
			t.Errorf("%s's fix does not say %q: %s", c.id, c.fix, fix)
		}
	}
}

// A fix is printed as it is written, and a person copies the command out of
// it. SDLC-E0049, SDLC-E0050 and SDLC-E0051 were held as raw strings with the
// quotes escaped, so their fixes printed --reason \"...\" and --note \"...\",
// which no shell needs and a reader takes for part of the command (US-029.2,
// carried from US-029.1's Gate 7).
func TestAFixPrintsItsCommandWithoutBackslashes(t *testing.T) {
	for _, code := range []Code{NoteOutsideRange, StoryHeldElsewhere, HoldingCheckoutMissing} {
		if fix := fixFor(code); strings.Contains(fix, `\`) {
			t.Errorf("%s's fix prints a backslash: %s", code, fix)
		}
	}
	for _, e := range catalogue {
		if strings.Contains(e.fix, `\"`) {
			t.Errorf("%s's fix prints an escaped quote: %s", e.code.id, e.fix)
		}
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
