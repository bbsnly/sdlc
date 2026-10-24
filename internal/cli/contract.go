package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/scaffold"
)

// The files Claude Code reads a project's instructions from, at its root.
const (
	claudeFile = "CLAUDE.md"
	agentsFile = "AGENTS.md"
	localFile  = "CLAUDE.local.md"
)

// contractFiles is what a project's root holds of them. Each text is read only
// where Stat, following links, says it is a regular file; a nil info is a file
// that is not there, or not one that can be read.
type contractFiles struct {
	claude, agents, local  os.FileInfo
	claudeText, agentsText string
	localText              string
	claudePresent          bool // by Lstat: a dangling link or a directory is still a CLAUDE.md Claude Code finds
	same, localSame        bool // CLAUDE.md, or CLAUDE.local.md, and AGENTS.md are one file
}

func readContract(root string) contractFiles {
	var c contractFiles
	c.claude, c.claudeText = scaffold.RegularFile(filepath.Join(root, claudeFile))
	c.agents, c.agentsText = scaffold.RegularFile(filepath.Join(root, agentsFile))
	c.local, c.localText = scaffold.RegularFile(filepath.Join(root, localFile))
	_, err := os.Lstat(filepath.Join(root, claudeFile))
	c.claudePresent = err == nil
	// A link by any spelling and a hard link are both one file.
	c.same = c.claude != nil && c.agents != nil && os.SameFile(c.claude, c.agents)
	c.localSame = c.local != nil && c.agents != nil && os.SameFile(c.local, c.agents)
	return c
}

// claudeCarries is whether CLAUDE.md, read as itself, has the section: one
// that is AGENTS.md under a second name is AGENTS.md's.
func (c contractFiles) claudeCarries() bool { return !c.same && scaffold.HasContract(c.claudeText) }

// bySetting is whether Claude Code reaches the section only through its
// Project instructions setting: in AGENTS.md, with no CLAUDE.md and no
// CLAUDE.local.md at the root to read in its place.
func (c contractFiles) bySetting() bool {
	return scaffold.HasContract(c.agentsText) && !c.claudePresent && c.local == nil
}

// importsAgents reports whether a file's text imports AGENTS.md as Claude Code
// reads an import (decision 6 of US-023): a line that, trimmed, is @AGENTS.md
// or @./AGENTS.md, outside a fenced code block. A fence opens at three or more
// backticks or tildes and closes at a line of only that character, at least as
// many; one left open runs to the end.
func importsAgents(text string) bool {
	fence := ""
	for line := range strings.Lines(text) {
		t := strings.TrimSpace(line)
		switch {
		case fence != "":
			if len(t) >= len(fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			fence = t[:len(t)-len(strings.TrimLeft(t, t[:1]))]
		case t == "@"+agentsFile || t == "@./"+agentsFile:
			return true
		}
	}
	return false
}

// contractCheck says whether Claude Code will read the contract section, and
// from which file. Every fix names a file and the line to add or remove.
func contractCheck(c contractFiles) check {
	section := `"` + scaffold.ContractHeading + `" section`
	agents := scaffold.HasContract(c.agentsText)
	ok := func(detail string) check { return check{Name: checkProjectContract, State: stateOK, Detail: detail} }
	problem := func(detail, fix string) check {
		return check{Name: checkProjectContract, State: stateProblem, Detail: detail, Fix: fix}
	}
	switch {
	case c.same && agents:
		return ok("AGENTS.md carries the " + section + ", and CLAUDE.md is a link to the same file, so Claude Code reads it")
	case c.claudeCarries() && agents:
		return problem("CLAUDE.md and AGENTS.md both carry the "+section+", and the two can disagree",
			`remove the `+section+` from AGENTS.md, or remove it from CLAUDE.md and put a line @AGENTS.md in its place`)
	case c.claudeCarries():
		detail := "CLAUDE.md carries the " + section
		if c.agents != nil && !importsAgents(c.claudeText) {
			detail += "; the AGENTS.md beside it is not imported, so by default Claude Code does not read it"
		}
		return ok(detail)
	case agents && !c.claudePresent && c.localSame:
		return ok("AGENTS.md carries the " + section + ", and CLAUDE.local.md is a link to the same file, so Claude Code reads it")
	case agents && !c.claudePresent && c.local != nil && !importsAgents(c.localText):
		return problem("AGENTS.md carries the "+section+", but a CLAUDE.local.md in the project root stops Claude Code reading AGENTS.md",
			"add a line @AGENTS.md to CLAUDE.local.md, outside any code block (or, if you no longer use it, remove CLAUDE.local.md)")
	case agents && !c.claudePresent && c.local != nil:
		return ok("AGENTS.md carries the " + section + ", and CLAUDE.local.md imports it")
	case agents && !c.claudePresent:
		return ok("AGENTS.md carries the " + section + " and there is no CLAUDE.md, so Claude Code 2.1.277 or later reads it")
	case agents && importsAgents(c.claudeText):
		return ok("AGENTS.md carries the " + section + ", and CLAUDE.md imports it with an @AGENTS.md line")
	case agents:
		return problem("AGENTS.md carries the "+section+", but Claude Code reads CLAUDE.md in its place, and CLAUDE.md does not import it",
			"add a line @AGENTS.md to CLAUDE.md, outside any code block")
	case !c.claudePresent && c.agents == nil:
		return problem("there is no CLAUDE.md or AGENTS.md",
			`run "sdlc init" -- it writes CLAUDE.md with the `+section+` and touches nothing else`)
	case c.agents != nil && (!c.claudePresent || c.same):
		return problem("AGENTS.md has no "+section,
			`run "sdlc init" -- it appends the `+section+` to AGENTS.md and leaves the rest alone`)
	}
	return problem("CLAUDE.md has no "+section,
		`run "sdlc init" -- it appends the `+section+` to CLAUDE.md and leaves the rest alone`)
}

// contractToCommit is the files a new worktree needs from trunk for the loop
// in it to read the contract (decisions 1, 5 and 8 of US-023): what trunk's
// tree holds it in, since the worktree is made from that; where trunk holds
// it nowhere, what the main checkout's disk holds it in; and CLAUDE.md where
// neither does. Each name is one of the constants, never text from a file.
func contractToCommit(ctx context.Context, main, trunk string) []string {
	if names := inTrunk(ctx, main, trunk); names != nil {
		return names
	}
	c := readContract(main)
	if names := holding(c.claudeCarries(), scaffold.HasContract(c.agentsText), c.claudePresent); names != nil {
		return names
	}
	return []string{claudeFile}
}

// inTrunk is what trunk's tree holds the contract in, or nil. A CLAUDE.md that
// is a link or a submodule has no text of its own, and an error is nothing
// there.
func inTrunk(ctx context.Context, main, trunk string) []string {
	commit, err := gitx.Resolve(ctx, main, "refs/heads/"+trunk)
	if err != nil {
		return nil
	}
	mode, _ := gitx.ModeAt(ctx, main, commit, claudeFile)
	var claude []byte
	if mode == "100644" || mode == "100755" {
		claude, _ = gitx.FileAt(ctx, main, commit, claudeFile)
	}
	agents, _ := gitx.FileAt(ctx, main, commit, agentsFile)
	return holding(scaffold.HasContract(string(claude)), scaffold.HasContract(string(agents)), mode != "")
}

// holding is the files to commit for a tree that holds the section in
// CLAUDE.md, in AGENTS.md, and has a CLAUDE.md: an import or a link is
// committed with its target. nil where neither holds it.
func holding(claude, agents, hasClaude bool) []string {
	switch {
	case claude:
		return []string{claudeFile}
	case agents && hasClaude:
		return []string{claudeFile, agentsFile}
	case agents:
		return []string{agentsFile}
	}
	return nil
}

// commitList is the fix's opening words for the files to commit with the
// configuration.
func commitList(names []string) string {
	switch len(names) {
	case 0:
		return "commit " + config.File + " to trunk"
	case 1:
		return "commit " + config.File + " and " + names[0] + " to trunk"
	}
	return "commit " + config.File + ", " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " to trunk"
}
