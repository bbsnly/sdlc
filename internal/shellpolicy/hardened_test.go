package shellpolicy

// US-029.3 pins the readers US-029.2's Gate 7 rework added with no frozen
// test, where this story's AC-9 rows lean on them: `printf -v` with `--`,
// PowerShell's Env: item commands, `${NAME:=word}`, and the bound on the text
// `!` aliases hand the shell. Each is held by a commit pointed at the main
// checkout from A-1's worktree, which is what they were added to read, and a
// control that is not one. They pass today.
//
// Helpers carry an hd prefix, for hardened.

import (
	"strings"
	"testing"
)

// `printf -v` with an end of options before the format sets the variable as
// plainly as without one, in either spelling of -v.
func TestPrintfDashVAfterAnEndOfOptionsStillSetsGitsPlace(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"printf -v GIT_DIR -- %s /work/main/.git; export GIT_DIR; git commit -m x",
		"printf -v GIT_WORK_TREE -- '%s' /work/main; export GIT_WORK_TREE; git commit -m x",
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	allowed(t, "printf -v stamp -- '%s' now; git commit -m \"$stamp\"", s)
	allowed(t, "printf -v GIT_DIR -- %s /work/main/.git/worktrees/A-1; export GIT_DIR; git commit -m x", s)
}

// PowerShell sets a variable through the Env: drive by every item command and
// parameter spelling, and each reads as the assignment it is.
func TestPowerShellEnvItemCommandsSetGitsPlaceHoweverTheyAreSpelled(t *testing.T) {
	s := wtPowerShell(heldInA1(ready))
	for _, command := range []string{
		`Set-Item -Value /work/main/.git -Path Env:\GIT_DIR; git commit -m x`,
		`Set-Item -LiteralPath Env:GIT_DIR -Value /work/main/.git; git commit -m x`,
		`New-Item -Path Env: -Name GIT_DIR -Value /work/main/.git; git commit -m x`,
		`ni Env:\GIT_WORK_TREE -Value /work/main; git commit -m x`,
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	for _, command := range []string{
		`Set-Item -Path Env:\GIT_AUTHOR_NAME -Value dev; git commit -m x`,
		`New-Item -Path Env: -Name GREETING -Value dev; git commit -m x`,
	} {
		allowed(t, command, s)
	}
}

// ${NAME:=word} assigns NAME where it is unset, which is an assignment the
// commit's place is read from.
func TestADefaultingExpansionSetsGitsPlaceAsAnAssignmentDoes(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		": ${GIT_DIR:=/work/main/.git}; export GIT_DIR; git commit -m x",
		": \"${GIT_WORK_TREE:=/work/main}\"; export GIT_WORK_TREE; git commit -m x",
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	allowed(t, ": ${STAMP:=now}; git commit -m x", s)
}

// The text `!` aliases hand the shell is read up to a bound, and past it the
// command is refused as one the rules cannot read, with the route that spells
// the aliases out.
func TestAliasTextPastTheBoundIsRefusedAsUnreadable(t *testing.T) {
	alias := func(size int) string {
		return "git -c 'alias.big=!echo " + strings.Repeat("a", size) + "' big"
	}
	f, ok := inspect(t, alias(maxAliasText+1), ready)
	if !ok {
		t.Fatal("an alias past the bound was let through")
	}
	if f.Rule != "git-command-cannot-be-read" ||
		!strings.Contains(f.Reason, "aliases that hand the shell more command lines than the rules read") ||
		!strings.Contains(f.Route, "commands of their own") {
		t.Errorf("an alias past the bound was refused as %+v", f)
	}
	allowed(t, alias(maxAliasText-(4<<10)), ready)
}
