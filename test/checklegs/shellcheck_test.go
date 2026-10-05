package checklegs

import (
	"regexp"
	"strings"
	"testing"
)

// trackedWord matches "tracked" as its own word, which "untracked" never
// contains (there is no boundary between "un" and "tracked"), but which "not
// tracked" does -- callers check for the negated phrasing first for that
// reason.
var trackedWord = regexp.MustCompile(`(?i)\btracked\b`)

// notTrackedPhrase matches every negated phrasing this test is willing to
// accept: "untracked" as one word, or "not [yet] tracked" as a phrase.
var notTrackedPhrase = regexp.MustCompile(`(?i)(untracked|not\s+(yet\s+)?tracked)`)

// annotatesTrackedness reports whether out says, within a few lines of
// filename's own mention, whether that file is tracked. It does not assume
// which side of the filename the annotation lands on, how far away, or its
// exact wording -- only that some nearby text says "tracked" without being
// negated (wantTracked) or says it is not (!wantTracked). A window, not the
// whole output, so a positive match from one file's own annotation cannot
// be read as having answered for the other file in the same fixture.
func annotatesTrackedness(out, filename string, wantTracked bool) bool {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, filename) {
			continue
		}
		start, end := max(0, i-2), min(len(lines), i+3)
		window := strings.Join(lines[start:end], "\n")
		switch {
		case wantTracked:
			if trackedWord.MatchString(window) && !notTrackedPhrase.MatchString(window) {
				return true
			}
		default:
			if notTrackedPhrase.MatchString(window) {
				return true
			}
		}
	}
	return false
}

// TestAC3AFailingShellcheckLegSaysWhetherEachFileItObjectsToIsTracked covers
// AC-3 ("any leg ... shall say, when it fails, whether the files it objected
// to are tracked") on the one leg ANALYSIS.md names as already scanning both
// a tracked and an untracked-but-not-ignored file by design
// (Taskfile.yml:241-245's own comment on --others): staged.sh is added,
// pending.sh deliberately is not. Both have the same unquoted-variable
// issue, so both still fail the leg -- this story does not change who
// shellcheck objects to, only whether the failure says which kind each file
// is.
//
// It fails today: shellcheck's own stdout never mentions trackedness at
// all, for either file.
func TestAC3AFailingShellcheckLegSaysWhetherEachFileItObjectsToIsTracked(t *testing.T) {
	requireShellcheck(t)
	root := t.TempDir()
	gitInit(t, root)
	write(t, root, "staged.sh", "#!/bin/sh\nx=$1\necho $x\n")
	write(t, root, "pending.sh", "#!/bin/sh\ny=$1\necho $y\n")
	gitAdd(t, root, "staged.sh")

	out, err := runTask(t, root, "shellcheck")

	// Unaffected by this story: shellcheck's own file list is unchanged.
	if err == nil {
		t.Fatalf("shellcheck should still object to both files' unquoted variables:\n%s", out)
	}
	if !strings.Contains(out, "staged.sh") || !strings.Contains(out, "pending.sh") {
		t.Fatalf("shellcheck's output did not name both files:\n%s", out)
	}

	if !annotatesTrackedness(out, "staged.sh", true) {
		t.Errorf("shellcheck's output never said staged.sh is tracked:\n%s", out)
	}
	if !annotatesTrackedness(out, "pending.sh", false) {
		t.Errorf("shellcheck's output never said pending.sh is not tracked:\n%s", out)
	}
}
