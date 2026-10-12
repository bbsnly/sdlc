package shellpolicy

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// What bash does with the rows of namedonly_test.go is asked of bash. A row
// there says a text is only text, or that the shell acts on it, from a reading
// of the grammar; the tests of the classifier pass whether or not that reading
// is right, and a row that calls an act a mention is a hole the table would
// hold open. No program is run: sdlc and every program a text names are shell
// functions that write their own name to a log, and PATH is an empty
// directory.

// corpusStandIns stand in for the programs. Each also prints, so that a
// redirect the shell makes leaves a mark on the file it opens, and each ends as
// the row asks, so that what follows `&&` is run in one pass and what follows
// `||` in the other.
const corpusStandIns = `
ran() { printf '%s\n' "$1" >>"$ROW_LOG"; echo out; echo err >&2; return "$ROW_STATUS"; }
sdlc() { ran sdlc; }
rm() { ran rm; }
tee() { ran tee; }
touch() { ran touch; }
cp() { ran cp; }
`

// corpusSeeded are the files a text names, as they are before a row is run.
var corpusSeeded = []string{
	".sdlc/config.json", ".sdlc/state/active", ".sdlc/state/tests.lock", ".sdlc/stories/A-1/gate-record.json",
	"CLAUDE.md", "user_stories.json", ".claude/settings.json", ".git/config", "internal/x_test.go",
}

const (
	bashRanNothing = iota
	bashTookItForText
	bashActedOnIt
)

func corpusTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
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

// askBash runs a row twice, with the stand-ins succeeding and failing, and
// says what bash made of it: it acted if anything but sdlc ran or any file is
// not as it was, it took the text for text if only sdlc ran, and otherwise it
// ran nothing, which is what bash does with a line it cannot parse.
func askBash(t *testing.T, bash, command string) int {
	t.Helper()
	saw := bashRanNothing
	for _, status := range []string{"0", "1"} {
		root, log := t.TempDir(), filepath.Join(t.TempDir(), "log")
		for _, rel := range corpusSeeded {
			file := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("kept\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		before := corpusTree(t, root)

		// The wait is for a text with `&` in it: nothing is left running
		// when the directory is removed.
		cmd := exec.CommandContext(t.Context(), bash, "-c", corpusStandIns+command+"\nwait\n")
		cmd.Dir = root
		cmd.Env = []string{"PATH=" + t.TempDir(), "ROW_LOG=" + log, "ROW_STATUS=" + status}
		_ = cmd.Run()

		ran, _ := os.ReadFile(log)
		after := corpusTree(t, root)
		changed := len(after) != len(before)
		for path, body := range before {
			changed = changed || after[path] != body
		}
		for name := range strings.FieldsSeq(string(ran)) {
			changed = changed || name != "sdlc"
		}
		switch {
		case changed:
			return bashActedOnIt
		case len(ran) > 0:
			saw = bashTookItForText
		}
	}
	return saw
}

func theBashToAsk(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the rows are bash's grammar, which does not change with the platform; Linux and macOS ask it")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH, so it was not asked")
	}
	return bash
}

func TestAC1BashRunsNothingInTheTextOfARowTheTableCallsAMention(t *testing.T) {
	bash := theBashToAsk(t)
	rows := append(corpusTextRows(false), corpusHardQuoting...)
	for _, r := range rows {
		t.Run(r.name("AC-1"), func(t *testing.T) {
			t.Parallel()
			if got := askBash(t, bash, r.command); got != bashTookItForText {
				t.Errorf("bash made %d of %q, and the table calls it text (0 ran nothing, 1 text, 2 acted on it)", got, r.command)
			}
		})
	}
}

func TestAC5BashActsOnTheTextOfARowTheTableCallsAnAct(t *testing.T) {
	bash := theBashToAsk(t)
	for _, r := range corpusTextRows(true) {
		t.Run(r.name("AC-5"), func(t *testing.T) {
			t.Parallel()
			if got := askBash(t, bash, r.command); got != bashActedOnIt {
				t.Errorf("bash made %d of %q, and the table calls it an act (0 ran nothing, 1 text, 2 acted on it)", got, r.command)
			}
		})
	}
}

func TestAC5BashDoesNotTakeARowWhoseQuoteClosesEarlyForText(t *testing.T) {
	bash := theBashToAsk(t)
	for _, r := range corpusQuoteClosers {
		t.Run(r.name("AC-5"), func(t *testing.T) {
			t.Parallel()
			if got := askBash(t, bash, r.command); got == bashTookItForText {
				t.Errorf("bash took %q for text, and the table says the quote closes before the operator", r.command)
			}
		})
	}
}
