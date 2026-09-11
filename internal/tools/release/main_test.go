package main

import (
	"io"
	"strings"
	"testing"
)

// The library is tested next door. What can only go wrong here is argument
// parsing, and it did: `release check -tag v1` -- the spelling this tool
// documents and the release workflow uses -- left -tag unset, so the preflight
// that exists to stop a mismatched tag being released checked nothing and
// reported success.
func TestTheTagIsCheckedWhicheverSideOfTheSubcommandItIsOn(t *testing.T) {
	for _, args := range [][]string{
		{"check", "-C", "../../..", "-tag", "v9.9.9"},
		{"-C", "../../..", "check", "-tag", "v9.9.9"},
		{"-C", "../../..", "-tag", "v9.9.9", "check"},
	} {
		err := run(args, io.Discard)
		if err == nil {
			t.Errorf("release %s passed on a tag that does not match the version",
				strings.Join(args, " "))
		}
	}
}

// The repository's own version is the one every file agrees on, so the matching
// tag has to pass -- otherwise the check above would be satisfied by a tool
// that always failed.
func TestTheMatchingTagPasses(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"check", "-C", "../../..", "-tag", "v" + thisVersion(t)}, &out); err != nil {
		t.Fatalf("the repository's own tag was refused: %v", err)
	}
	if !strings.Contains(out.String(), "every file that carries it agrees") {
		t.Errorf("stdout = %q", out.String())
	}
}

// A mistyped flag lands in the argument list. Ignoring it is how a check goes
// on reporting success while checking something other than what was asked.
func TestAStrayArgumentIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"check", "-C", "../../..", "v9.9.9"},
		{"check", "-C", "../../..", "-tag", "v9.9.9", "extra"},
	} {
		if err := run(args, io.Discard); err == nil {
			t.Errorf("release %s was accepted", strings.Join(args, " "))
		}
	}
}

func TestAnUnknownCommandSaysWhatThereIs(t *testing.T) {
	err := run([]string{"publish", "-C", "../../.."}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "check, version or notes") {
		t.Errorf("err = %v", err)
	}
}

func thisVersion(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	if err := run([]string{"version", "-C", "../../.."}, &out); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out.String())
}
