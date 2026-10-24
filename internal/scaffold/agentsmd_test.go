package scaffold

// US-023 AC-1, AC-2 and the second decision's answer 4: which file init writes
// the contract to. Claude Code reads AGENTS.md only where no CLAUDE.md exists,
// so a project that keeps its instructions in AGENTS.md gets the section there
// and no CLAUDE.md; every other project is written as before; and a CLAUDE.md
// that is a link to AGENTS.md is the section in AGENTS.md, with the link kept.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const agentsMDPath = "AGENTS.md"

// agNamed reports whether a list init returns names the file, as itself or
// with a note after it.
func agNamed(list []string, file string) bool {
	return slices.ContainsFunc(list, func(p string) bool { return p == file || strings.HasPrefix(p, file+" ") })
}

func agExists(t *testing.T, root, rel string) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(root, rel))
	return err == nil
}

// AC-1: with an AGENTS.md and no CLAUDE.md, init appends the section to
// AGENTS.md, keeps what was there, creates no CLAUDE.md, and says it wrote
// AGENTS.md.
func TestAC1InitAppendsTheContractToAGENTSmdWhereThereIsNoCLAUDEmd(t *testing.T) {
	root := t.TempDir()
	touch(t, root, agentsMDPath, "# My project\n\nOur own instructions.")
	res, err := Init(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if agExists(t, root, claudeMDPath) {
		t.Error("AC-1 init created a CLAUDE.md, which hides the project's AGENTS.md from Claude Code")
	}
	got := read(t, root, agentsMDPath)
	if !strings.HasPrefix(got, "# My project\n\nOur own instructions.\n") || !HasContract(got) {
		t.Errorf("AC-1 AGENTS.md is not the project's words with the section appended:\n%s", got)
	}
	if !agNamed(res.Created, agentsMDPath) {
		t.Errorf("AC-1 Created = %v, want AGENTS.md named as the file written", res.Created)
	}
	if agNamed(res.Created, claudeMDPath) || agNamed(res.Kept, claudeMDPath) {
		t.Errorf("AC-1 init names CLAUDE.md though it wrote none: created %v, kept %v", res.Created, res.Kept)
	}
}

// AC-2: every other project is written as before, and where either file
// already carries the section on its own line, neither changes.
func TestAC2InitWritesEveryOtherProjectAsBefore(t *testing.T) {
	const own = "# Mine\n\nOwn words.\n"
	const filled = "# Mine\n\n" + ContractHeading + "\n\n- filled in\n"
	for _, c := range []struct {
		name          string
		claude, agent string // "" for no such file
		wantClaude    string // "same", "appended", "created" or "none"
		wantAgents    string // "same" or "none"
	}{
		{"AC-2 neither file", "", "", "created", "none"},
		{"AC-2 CLAUDE.md alone", own, "", "appended", "none"},
		{"AC-2 CLAUDE.md beside AGENTS.md", own, own, "appended", "same"},
		{"AC-2 CLAUDE.md with the section, beside AGENTS.md", filled, own, "same", "same"},
		{"AC-2 AGENTS.md with the section, beside CLAUDE.md", own, filled, "same", "same"},
		{"AC-2 AGENTS.md with the section and no CLAUDE.md", "", filled, "none", "same"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.claude != "" {
				touch(t, root, claudeMDPath, c.claude)
			}
			if c.agent != "" {
				touch(t, root, agentsMDPath, c.agent)
			}
			if _, err := Init(root, false); err != nil {
				t.Fatal(err)
			}
			switch c.wantClaude {
			case "none":
				if agExists(t, root, claudeMDPath) {
					t.Error("init created a CLAUDE.md")
				}
			case "created":
				if !HasContract(read(t, root, claudeMDPath)) {
					t.Error("init did not create CLAUDE.md with the section")
				}
			case "appended":
				if got := read(t, root, claudeMDPath); !strings.HasPrefix(got, c.claude) || !HasContract(got) {
					t.Errorf("CLAUDE.md is not the project's words with the section appended:\n%s", got)
				}
			case "same":
				if got := read(t, root, claudeMDPath); got != c.claude {
					t.Errorf("CLAUDE.md changed:\n%s", got)
				}
			}
			switch c.wantAgents {
			case "none":
				if agExists(t, root, agentsMDPath) {
					t.Error("init created an AGENTS.md, which the story leaves to the project")
				}
			case "same":
				if got := read(t, root, agentsMDPath); got != c.agent {
					t.Errorf("AGENTS.md changed:\n%s", got)
				}
			}
		})
	}
}

// Decision 2, answer 4: a CLAUDE.md that is a link to AGENTS.md is the
// section in AGENTS.md. init appends to AGENTS.md, keeps the link, and names
// AGENTS.md. A link to anything else is not written through: a file outside
// the project stays as it was.
func TestAC1InitAppendsToAGENTSmdThroughACLAUDEmdThatLinksToIt(t *testing.T) {
	root := t.TempDir()
	touch(t, root, agentsMDPath, "# Mine\n\nOwn words.\n")
	if err := os.Symlink(agentsMDPath, filepath.Join(root, claudeMDPath)); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	res, err := Init(root, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, claudeMDPath))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("AC-1 the CLAUDE.md link to AGENTS.md is no longer a link (%v)", err)
	}
	if got := read(t, root, agentsMDPath); !strings.HasPrefix(got, "# Mine\n\nOwn words.\n") || !HasContract(got) {
		t.Errorf("AC-1 AGENTS.md did not get the section through the link:\n%s", got)
	}
	if !agNamed(res.Created, agentsMDPath) {
		t.Errorf("AC-1 Created = %v, want AGENTS.md", res.Created)
	}

	elsewhere := t.TempDir()
	touch(t, elsewhere, "notes.md", "someone else's\n")
	other := t.TempDir()
	if err := os.Symlink(filepath.Join(elsewhere, "notes.md"), filepath.Join(other, claudeMDPath)); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	if _, err := Init(other, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, elsewhere, "notes.md"); got != "someone else's\n" {
		t.Errorf("init wrote through a CLAUDE.md link to a file outside the project:\n%s", got)
	}
}

// Decision 2, answer 4, for the spellings of a link design review round 1
// found no row for: a CLAUDE.md link to ./AGENTS.md, or to AGENTS.md by its
// absolute path, is a link to AGENTS.md as much as one to the bare name, so
// init appends to AGENTS.md and keeps the link. A reader that compared the
// link's text with "AGENTS.md" passed the row above and fails these.
func TestAC1InitKeepsALinkToAGENTSmdHoweverItIsSpelt(t *testing.T) {
	for name, target := range map[string]func(root string) string{
		"AC-1 a link spelt ./AGENTS.md":          func(string) string { return "./" + agentsMDPath },
		"AC-1 a link to AGENTS.md's whole path":  func(root string) string { return filepath.Join(root, agentsMDPath) },
		"AC-1 a link through the root directory": func(root string) string { return filepath.Join("..", filepath.Base(root), agentsMDPath) },
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if resolved, err := filepath.EvalSymlinks(root); err == nil {
				root = resolved
			}
			touch(t, root, agentsMDPath, "# Mine\n\nOwn words.\n")
			if err := os.Symlink(target(root), filepath.Join(root, claudeMDPath)); err != nil {
				t.Skipf("symbolic links cannot be made here: %v", err)
			}
			res, err := Init(root, false)
			if err != nil {
				t.Fatal(err)
			}
			if info, err := os.Lstat(filepath.Join(root, claudeMDPath)); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("the CLAUDE.md link to AGENTS.md is no longer a link (%v)", err)
			}
			if got := read(t, root, agentsMDPath); !strings.HasPrefix(got, "# Mine\n\nOwn words.\n") || !HasContract(got) {
				t.Errorf("AGENTS.md did not get the section through the link:\n%s", got)
			}
			if !agNamed(res.Created, agentsMDPath) {
				t.Errorf("Created = %v, want AGENTS.md", res.Created)
			}
		})
	}
}

// AC-1 where the project's AGENTS.md is itself a link, as monorepos and
// dotfiles keep a shared one (design review round 1, red team): init still
// writes no CLAUDE.md, and the AGENTS.md Claude Code reads from the project
// carries the section. Whether the link is kept, and the shared file written,
// is left open: the reviews disagree, and no decision on the story says.
func TestAC1InitGivesAnAGENTSmdThatIsALinkTheSectionAndNoCLAUDEmd(t *testing.T) {
	shared := t.TempDir()
	touch(t, shared, agentsMDPath, "# Shared\n\nEvery project's words.\n")
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(shared, agentsMDPath), filepath.Join(root, agentsMDPath)); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	if agExists(t, root, claudeMDPath) {
		t.Error("AC-1 init created a CLAUDE.md beside an AGENTS.md that is a link, which hides it from Claude Code")
	}
	if got := read(t, root, agentsMDPath); !strings.HasPrefix(got, "# Shared\n\nEvery project's words.\n") || !HasContract(got) {
		t.Errorf("AC-1 the project's AGENTS.md, read through its link, is not its words with the section appended:\n%s", got)
	}
}
