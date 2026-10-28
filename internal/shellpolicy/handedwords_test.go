package shellpolicy

// The words a segment is handed from elsewhere on its line, through xargs or
// GNU parallel, or on standard input to a program written on the line, are
// read as the shell hands them over (handedWords). Read by wordsIn alone they
// lost their `~` and their `$` and were split at a quote, and the refused
// rows below were let through: the person's ~/.claude and ~/.agents taken
// away or written from any session, and the loop's record from the session
// holding the story, in spellings that were refused written out.
// handedwords_shell_test.go asks bash and zsh what each spelling hands over.

import (
	"slices"
	"strings"
	"testing"
)

// handedState is the hook's State as far as these rows need it: the person's
// home at /home/dev, holding ~/.claude and ~/.agents and placed from ~, $HOME
// and ${HOME} as the hook's personsConfig places it; the project's own paths
// resolved; and one frozen test.
func handedState() State {
	const home = "/home/dev"
	place := func(word string) string {
		c := clean(word)
		for _, h := range []string{"~", "$HOME", "${HOME}"} {
			if c == h {
				return home
			}
			if rest, ok := strings.CutPrefix(c, h+"/"); ok {
				return home + "/" + rest
			}
		}
		return c
	}
	under := func(dir string) func(string) bool {
		return func(word string) bool {
			p := place(word)
			return p == dir || hasDir(p, dir)
		}
	}
	holds := func(word string) bool {
		p := place(word)
		return p == home || p == "/home" || p == "/"
	}
	s := claudeConfigState()
	s.Frozen = []string{"internal/invoice_test.go"}
	s.ClaudeConfig, s.ClaudeConfigHeld = under(home+"/.claude"), holds
	s.AgentsConfig, s.AgentsConfigHeld = under(home+"/.agents"), holds
	return s
}

// handedRow is a command, the rule the session holding the story refuses it
// under (Inspect), and the rule any other session does (ClaudeConfigWrites),
// "" where it is let through.
type handedRow struct {
	command, story, other string
}

func checkHanded(t *testing.T, rows []handedRow) {
	t.Helper()
	for _, row := range rows {
		s := handedState()
		f, ok := Inspect(row.command, s)
		if got := map[bool]string{true: f.Rule}[ok]; got != row.story {
			t.Errorf("in the story's session, %s = %q (%s); want %q", row.command, got, f.Reason, row.story)
		}
		if ok && f.Route == "" {
			t.Errorf("the refusal of %s says nothing to do next", row.command)
		}
		f, ok = ClaudeConfigWrites(row.command, s)
		if got := map[bool]string{true: f.Rule}[ok]; got != row.other {
			t.Errorf("in another session, %s = %q (%s); want %q", row.command, got, f.Reason, row.other)
		}
	}
}

func TestAWordHandedOnStandardInputIsReadAsTheShellHandsItOver(t *testing.T) {
	const (
		claude = ruleClaudeConfig
		agents = ruleAgentsConfig
		loop   = "loop-state-through-the-tool"
		named  = "protected-path-through-the-tool"
	)
	checkHanded(t, []handedRow{
		// The spellings of the home a shell expands, before a writer xargs feeds.
		{"echo ~/.claude/settings.json | xargs rm", claude, claude},
		{"echo ~/.claude/settings.json | xargs rm -f", claude, claude},
		{"echo $HOME/.claude/settings.json | xargs rm -f", named, claude},
		{"echo ${HOME}/.claude/settings.json | xargs rm", named, claude},
		{`echo "$HOME"/.claude/settings.json | xargs rm`, named, claude},
		{`echo "$HOME/.claude/settings.json" | xargs rm`, named, claude},
		{`echo "${HOME}/.claude/settings.json" | xargs rm`, named, claude},
		{`echo ~/".claude"/settings.json | xargs rm`, named, claude},
		{"echo ~/.cl''aude/settings.json | xargs rm", claude, claude},
		{`echo ~/.cl\aude/settings.json | xargs rm`, claude, claude},
		// Another user's home is somewhere the hook cannot place.
		{"echo ~bob/.claude/settings.json | xargs rm", named, claude},
		// Quoted or escaped, the shell hands rm a directory called ~ where the
		// command runs; refused as `rm '~/.claude/settings.json'` is, since
		// which of the two was meant cannot be told from the line.
		{`echo \~/.claude/settings.json | xargs rm`, claude, claude},
		{"echo '~/.claude/settings.json' | xargs rm", claude, claude},
		// What xargs is told to do with the words.
		{`printf '%s\n' ~/.claude/settings.json | xargs -I{} rm {}`, claude, claude},
		{`printf '%s\0' ~/.claude/settings.json | xargs -0 rm`, claude, claude},
		{"echo ~/.claude/settings.json | xargs -n1 rm", claude, claude},
		{"echo ~/.claude/settings.json | xargs touch", claude, claude},
		{"echo ~/.claude/settings.json | xargs sed -i s/a/b/", claude, claude},
		{"echo ~/.claude/settings.json | xargs mv -t /tmp", claude, claude},
		{"echo ~/.claude/settings.json | xargs -I{} cp /tmp/evil {}", claude, claude},
		{"echo ~/.claude/settings.json | xargs -I{} ln -sf /tmp/evil {}", claude, claude},
		{"echo ~/.claude | xargs -I{} cp -r /tmp/evil/. {}", claude, claude},
		// GNU parallel, fed the same way and given the words after :::.
		{"echo ~/.claude/settings.json | parallel rm", claude, claude},
		{"echo ~/.claude/settings.json | parallel -X rm", claude, claude},
		{"parallel rm ::: ~/.claude/settings.json", claude, claude},
		// Ways for the words to reach xargs.
		{"xargs rm <<< ~/.claude/settings.json", claude, claude},
		{"echo ~/.claude/settings.json | sort | xargs rm", claude, claude},
		{"{ echo ~/.claude/settings.json; } | xargs rm", claude, claude},
		{"echo $(echo ~/.claude/settings.json) | xargs rm", claude, claude},
		{"find ~/.claude | xargs rm", claude, claude},
		{"find ~/.claude -print0 | xargs -0 rm -rf", claude, claude},
		{"ls ~/.claude | xargs -I{} rm ~/.claude/{}", claude, claude},
		{"X=~/.claude; echo $X/settings.json | xargs rm", named, claude},
		// The file xargs -a reads, where the line writes it.
		{"echo ~/.claude/settings.json > list; xargs -a list rm", claude, claude},
		// The person's ~/.agents.
		{"echo ~/.agents | xargs rm -rf", agents, agents},
		{"echo ~/.agents/x | xargs rm", agents, agents},
		{"echo $HOME/.agents/x | xargs rm", agents, agents},
		{`echo "$HOME"/.agents | xargs rm -rf`, agents, agents},
		{"echo ~/.ag''ents/x | xargs rm", agents, agents},
		// The project's own files, held while a story is worked.
		{"echo .sd''lc/state/active | xargs rm", loop, ""},
		{`echo .sd"lc"/state/active | xargs rm`, loop, ""},
		{"echo .cl''aude/settings.json | xargs rm", named, ""},
		{"echo CLAU''DE.md | xargs rm", named, ""},
		{"echo internal/invoice''_test.go | xargs rm", "frozen-test-through-the-tool", ""},
		// A program written on the line that reads its standard input.
		{"echo .sd''lc/state/active | python3 -c 'import os; os.remove(input())'", loop, ""},
		{"python3 -c 'import os; os.remove(input())' <<< .sd''lc/state/active", loop, ""},
	})
}

// What reaches no held place is let through as it was, and a list the hook
// cannot read, such as a file the line does not write, is the producer's to
// say, as it was.
func TestAWordHandedOnStandardInputThatNamesNothingHeldIsLetThrough(t *testing.T) {
	checkHanded(t, []handedRow{
		{"echo ~/notes.txt | xargs rm", "", ""},
		{"echo $HOME/notes | xargs rm", "", ""},
		{"echo ~/.cache/go | xargs rm -rf", "", ""},
		{`echo "$TMPDIR/x" | xargs rm`, "", ""},
		{"cat files.txt | xargs rm", "", ""},
		{"xargs -a files.txt rm", "", ""},
		{"find . -name '*.orig' | xargs rm", "", ""},
		{"git ls-files -z | xargs -0 rm -f", "", ""},
		{"echo ~/.claude/settings.json | xargs cat", "", ""},
		{"echo ~/.claude/settings.json | xargs grep x", "", ""},
		{"parallel rm ::: build/a build/b", "", ""},
		{"python3 -c 'print(1)' | xargs echo", "", ""},
	})
}

// A program's own text is read as written, and not cut at its blanks: cut,
// `open(os.path.expanduser("~/.agents/x"))` named a .agents in the project.
func TestAProgramsTextIsReadAsWrittenWhereNothingFeedsIt(t *testing.T) {
	checkHanded(t, []handedRow{
		{`python3 -c 'import os; open(os.path.expanduser("~/.agents/settings.json"), "w")'`, ruleAgentsConfig, ruleAgentsConfig},
	})
}

func TestHandedWordsAreTheLinesWordsAndThenEachShellWordOnce(t *testing.T) {
	line := `echo "$HOME"/.claude ~/x a/b | xargs rm; echo ~/x .sd''lc a/b > out`
	got := handedWords(wordsIn(line), line)
	want := []string{
		"echo", "HOME", "/.claude", "/x", "a/b", "xargs", "rm", ".sd", "lc", "out",
		"$HOME/.claude", "~/x", ".sdlc",
	}
	if !slices.Equal(got, want) {
		t.Errorf("handedWords(%s) = %q; want %q", line, got, want)
	}
}
