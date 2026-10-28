package shellpolicy

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// What bash and zsh hand over for the spellings of handedwords_test.go is
// asked of them. A row there says a command takes the person's configuration
// or the loop's record away through xargs, from a reading of the grammar; the
// classifier's tests pass whether or not that reading is right, and a row
// that refuses a spelling no shell expands to the place is a false refusal,
// as one that lets through a spelling every shell does is a hole. Each row is
// run with the real programs in a home and a project that are the test's own
// temporary directories, and the row says whether the shell changed anything
// in either.

// handedShellRow is a command and whether a shell that runs it changes the
// home or the project. EVIL in a command is a directory of the test's own to
// copy from.
type handedShellRow struct {
	command string
	acts    bool
	// ambiguous is whether the classifier refuses the row though the shell
	// does not act, as it refuses the same word written out.
	ambiguous bool
	// needs is a program the row runs beyond the shell's own and coreutils'.
	needs string
}

var handedShellRows = []handedShellRow{
	{command: "echo ~/.claude/settings.json | xargs rm", acts: true},
	{command: "echo $HOME/.claude/settings.json | xargs rm -f", acts: true},
	{command: "echo ${HOME}/.claude/settings.json | xargs rm", acts: true},
	{command: `echo "$HOME"/.claude/settings.json | xargs rm`, acts: true},
	{command: `echo "$HOME/.claude/settings.json" | xargs rm`, acts: true},
	{command: `echo "${HOME}/.claude/settings.json" | xargs rm`, acts: true},
	{command: `echo ~/".claude"/settings.json | xargs rm`, acts: true},
	{command: "echo ~/.cl''aude/settings.json | xargs rm", acts: true},
	{command: `echo ~/.cl\aude/settings.json | xargs rm`, acts: true},
	// The tilde is handed over as it is, and rm is given a directory called
	// ~ where the command runs, which is not there: the classifier refuses
	// these for what cannot be told from the line, not for what they do.
	{command: `echo \~/.claude/settings.json | xargs rm`, ambiguous: true},
	{command: "echo '~/.claude/settings.json' | xargs rm", ambiguous: true},
	{command: `printf '%s\n' ~/.claude/settings.json | xargs -I{} rm {}`, acts: true},
	{command: `printf '%s\0' ~/.claude/settings.json | xargs -0 rm`, acts: true},
	{command: "echo ~/.claude/settings.json | xargs -n1 rm", acts: true},
	{command: "echo ~/.claude/hooks.json | xargs touch", acts: true},
	{command: `echo ~/.claude | xargs -I{} cp -R "$EVIL"/. {}`, acts: true},
	{command: "xargs rm <<< ~/.claude/settings.json", acts: true},
	{command: "echo ~/.claude/settings.json | sort | xargs rm", acts: true},
	{command: "{ echo ~/.claude/settings.json; } | xargs rm", acts: true},
	{command: "echo $(echo ~/.claude/settings.json) | xargs rm", acts: true},
	{command: "find ~/.claude -type f | xargs rm", acts: true},
	{command: "X=~/.claude; echo $X/settings.json | xargs rm", acts: true},
	{command: "echo ~/.claude/settings.json | parallel rm", acts: true, needs: "parallel"},
	{command: "echo ~/.agents | xargs rm -rf", acts: true},
	{command: "echo $HOME/.agents/x | xargs rm", acts: true},
	{command: `echo "$HOME"/.agents | xargs rm -rf`, acts: true},
	{command: "echo ~/.ag''ents/x | xargs rm", acts: true},
	{command: "echo .sd''lc/state/active | xargs rm", acts: true},
	{command: `echo .sd"lc"/state/active | xargs rm`, acts: true},
	{command: "echo CLAU''DE.md | xargs rm", acts: true},
	{command: "echo .sd''lc/state/active | python3 -c 'import os; os.remove(input())'", acts: true, needs: "python3"},
	{command: "python3 -c 'import os; os.remove(input())' <<< .sd''lc/state/active", acts: true, needs: "python3"},
	// Nothing held is reached.
	{command: "echo ~/notes.txt | xargs rm -f"},
	{command: "echo ~/.claude/settings.json | xargs cat"},
}

// handedTree is every file and directory under root, with what each file
// holds.
func handedTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			tree[path] = "/"
			return nil
		}
		body, err := os.ReadFile(path)
		tree[path] = string(body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// handedActs runs a row in shell, with a home and a project of the test's
// own, and reports whether either is not as it was.
func handedActs(t *testing.T, shell []string, command string) bool {
	t.Helper()
	home, project, evil := t.TempDir(), t.TempDir(), t.TempDir()
	for _, f := range []string{
		filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".agents", "x"),
		filepath.Join(project, ".sdlc", "state", "active"), filepath.Join(project, "CLAUDE.md"),
		filepath.Join(evil, "hooks.json"),
	} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("kept\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := []map[string]string{handedTree(t, home), handedTree(t, project)}
	cmd := exec.CommandContext(t.Context(), shell[0], slices.Concat(shell[1:], []string{command})...)
	cmd.Dir = project
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "EVIL=" + evil, "LC_ALL=C"}
	_ = cmd.Run()
	for i, after := range []map[string]string{handedTree(t, home), handedTree(t, project)} {
		if len(after) != len(before[i]) {
			return true
		}
		for path, body := range before[i] {
			if after[path] != body {
				return true
			}
		}
	}
	return false
}

func TestBashAndZshHandOverTheSpellingsTheHandedRowsSayTheyDo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rows are bash's and zsh's grammar, which does not change with the platform; Linux and macOS ask them")
	}
	asked := 0
	for _, shell := range [][]string{{"bash", "--norc", "--noprofile", "-c"}, {"zsh", "-f", "-c"}} {
		path, err := exec.LookPath(shell[0])
		if err != nil {
			t.Logf("%s is not on PATH, so it was not asked", shell[0])
			continue
		}
		asked++
		shell[0] = path
		for _, row := range handedShellRows {
			if row.needs != "" {
				if _, err := exec.LookPath(row.needs); err != nil {
					t.Logf("%s is not on PATH, so %s was not run", row.needs, row.command)
					continue
				}
			}
			if got := handedActs(t, shell, row.command); got != row.acts {
				t.Errorf("%s: %s changed the home or the project: %v; the row says %v",
					filepath.Base(shell[0]), row.command, got, row.acts)
			}
		}
	}
	if asked == 0 {
		t.Skip("neither bash nor zsh is on PATH, so neither was asked")
	}
	// And the classifier refuses every row that acts, and those it cannot
	// tell from one that does; handedwords_test.go names the rule of each.
	for _, row := range handedShellRows {
		if _, refused := Inspect(row.command, handedState()); refused != (row.acts || row.ambiguous) {
			t.Errorf("the classifier refused %s: %v; the shells say it acts: %v", row.command, refused, row.acts)
		}
	}
}
