package shellpolicy

// US-108 AC-6 and AC-7, the readings step 12 added to claudeconfig.go: which
// words the shell would expand as a glob, an interpreter that reads its
// program from standard input, the link `ln` makes where it runs, and a line
// that copies a program and names both Claude Code and a person's command.
// The whole-command rows are the ones reproduced against the built binary;
// the rest hold one reading each, so that a change to one shows where it is.

import (
	"slices"
	"strings"
	"testing"
)

// The whole-command rows, each through the entry point the hook asks.
func TestAC7ClaudeConfigWritesReadsAGlobOnlyWhereTheShellExpandsIt(t *testing.T) {
	s, _ := tempHomeState(t)
	// A sed script's `.*` is not a glob: it is quoted, and the file it edits
	// is named by a variable that does not name the configuration.
	if f, ok := ClaudeConfigWrites(`sed -i 's/.*//' "$FILE"`, s); ok {
		t.Errorf(`AC-7 sed -i 's/.*//' "$FILE" was refused: %+v`, f)
	}
	for _, command := range []string{
		// The shell expands .cla* in the home: it can be .claude.
		`rm -rf "$HOME"/.cla*""`,
		// `ln -s` with one operand makes the link where it runs, the home.
		`cd ~ && ln -s /x/.claude.json`,
	} {
		f, ok := ClaudeConfigWrites(command, s)
		if !ok || f.Rule != ruleClaudeConfig || f.Reason == "" || f.Route == "" {
			t.Errorf("AC-7 %q = %+v, %v; want it refused as %s, with a reason and a route", command, f, ok, ruleClaudeConfig)
		}
	}
}

func TestAC7ClaudeConfigWritesRefusesABarePythonFedCodeThatNamesTheConfiguration(t *testing.T) {
	s, _ := tempHomeState(t)
	const code = `printf '%s' 'open(__import__("os").path.expanduser("~/.claude/settings.json"), "w")' | `
	for _, python := range []string{"python3", "python3 -W ignore"} {
		command := code + python
		f, ok := ClaudeConfigWrites(command, s)
		if !ok || f.Rule != ruleClaudeConfig || f.Route == "" {
			t.Errorf("AC-7 %q = %+v, %v; want it refused as %s, with a route", command, f, ok, ruleClaudeConfig)
		}
	}
	// The same program from a file of its own is not handed its code here.
	if f, ok := ClaudeConfigWrites(`printf '%s' '~/.claude' | python3 tool.py`, s); ok {
		t.Errorf("AC-7 a python3 that runs a script was refused for the code on its standard input: %+v", f)
	}
}

func TestAC6ALineThatLinksClaudeUnderAnotherNameAndNamesAPersonsCommandIsRefused(t *testing.T) {
	tempHomeState(t)
	none := func(string) bool { return false }
	for _, c := range []struct {
		command, reason string
	}{
		// `command -v claude` is read as running claude, so the session finding.
		{`ln -s "$(command -v claude)" /tmp/x && /tmp/x -p "/sdlc:approve"`, secondSessionFinding.Reason},
		// `which` runs nothing: only the copy and the two names refuse it.
		{`ln -s "$(which claude)" /tmp/x && /tmp/x -p "/sdlc:approve"`, copiedSessionFinding.Reason},
	} {
		f, ok := HumanDecisions(c.command, false, none)
		if !ok || f.Rule != "story-stays-in-its-session" || f.Reason != c.reason || f.Route == "" {
			t.Errorf("AC-6 %q = %+v, %v; want it refused as story-stays-in-its-session, reason %q", c.command, f, ok, c.reason)
		}
	}
	// The link alone names no person's command.
	if f, ok := HumanDecisions(`ln -s "$(which claude)" /tmp/x`, false, none); ok {
		t.Errorf("AC-6 a link to claude that names no person's command was refused: %+v", f)
	}
}

// One reading at a time.
func TestAC7ExpandsAGlobIsAGlobOutsideEveryQuoteAndNotEscaped(t *testing.T) {
	for _, c := range []struct {
		segment, raw string
		want         bool
	}{
		{`rm -rf "$HOME"/.cla*""`, "$HOME/.cla*", true},
		{`ls ~/.cla?`, "~/.cla?", true},
		{`ls ~/[.]claude`, "~/[.]claude", true},
		{`sed -i 's/.*//' "$FILE"`, "s/.*//", false},
		{`ls "~/.cla*"`, "~/.cla*", false},
		// A quoted twin first on the line does not hide the word the shell
		// expands after it (red-team, round 4).
		{`rm -rf "$HOME/.cla*" $HOME/.cla*`, "$HOME/.cla*", true},
		{`rm -rf ".cla*" .cla*`, ".cla*", true},
		// raw is the word with its quotes taken off and its backslashes kept,
		// as the walk hands it over.
		{`ls ~/.cla\*`, `~/.cla\*`, false},
		{`ls "a\"*"`, `a\*`, false},
		// A word the segment does not spell, such as one the walk made, is
		// read as expanded.
		{`echo x`, "~/.cla*", true},
	} {
		q := pathQuestion{segment: c.segment, m: &memo{}}
		if got := q.expandsAGlob(c.raw); got != c.want {
			t.Errorf("expandsAGlob(%q in %q) = %v, want %v", c.raw, c.segment, got, c.want)
		}
		if got := q.expandsAGlob(c.raw); got != c.want {
			t.Errorf("expandsAGlob(%q in %q), asked again, = %v, want %v", c.raw, c.segment, got, c.want)
		}
	}
	// The memo's words are the segment's it was last asked of.
	m := &memo{}
	(pathQuestion{segment: `ls ~/.cla*`, m: m}).expandsAGlob("~/.cla*")
	if got := (pathQuestion{segment: `ls "~/.cla*"`, m: m}).expandsAGlob("~/.cla*"); got {
		t.Error("expandsAGlob read a quoted word by the words of the segment asked before it")
	}
}

func TestAC7ProgramOnStdinIsAnInterpreterThatNamesNoScriptAndNoModule(t *testing.T) {
	for _, c := range []struct {
		command string
		want    bool
	}{
		{"python3", true},
		{"python3 -", true},
		{"python3 -u", true},
		{"python3 -W ignore", true},
		{"python3 -X dev", true},
		{"python3 --", true},
		{"python3 -- -", true},
		{"python3 -- tool.py", false},
		{"python3 -m http.server", false},
		{"python3 -mhttp.server", false},
		{"python3 tool.py", false},
		{"python3 -W ignore tool.py", false},
		{"py", true},
		{"node", true},
		{"node -r ./hook.js", true},
		{"node app.js", false},
		{"bun --preload ./p.ts", true},
		{"ruby -I lib", true},
		{"php -d x=1", true},
		{"perl -w", true},
		// -I takes the next word as a directory, so lib is not the script:
		// `perl -I lib` reads its program from standard input (red-team, round 4).
		{"perl -I lib", true},
		{"perl -I lib script.pl", false},
		{"perl script.pl", false},
		{"cat", false},
	} {
		if got := programOnStdin(strings.Fields(c.command), true); got != c.want {
			t.Errorf("programOnStdin(%q, fed) = %v, want %v", c.command, got, c.want)
		}
	}
	if programOnStdin(strings.Fields("python3"), false) {
		t.Error("an interpreter fed nothing on standard input was read as reading its program there")
	}
	if programOnStdin(nil, true) {
		t.Error("no words were read as an interpreter")
	}
}

func TestAC7StdinValuedNamesTheOptionsThatTakeTheNextWordForEachInterpreter(t *testing.T) {
	for _, c := range []struct {
		name   string
		has    string
		interp bool
	}{
		{"python", "-W", true},
		{"node", "--require", true},
		{"bun", "--preload", true},
		{"ruby", "-I", true},
		{"php", "-d", true},
		// This row once wanted none, which pinned `perl -I lib` fed its
		// program on standard input as running a script called lib.
		{"perl", "-I", true},
		{"cat", "", false},
	} {
		valued, ok := stdinValued(c.name)
		if ok != c.interp {
			t.Errorf("stdinValued(%q) says interpreter %v, want %v", c.name, ok, c.interp)
		}
		if c.has == "" && valued != nil {
			t.Errorf("stdinValued(%q) = %q, want none", c.name, valued)
		}
		if c.has != "" && !slices.Contains(valued, c.has) {
			t.Errorf("stdinValued(%q) = %q, want it to hold %s", c.name, valued, c.has)
		}
	}
}

func TestAC7PipedIsWorkedOutOnceForALine(t *testing.T) {
	for line, want := range map[string]bool{"printf x | python3": true, "python3 < code.py": false} {
		m := &memo{}
		if first, again := m.piped(line), m.piped(line); first != want || again != want {
			t.Errorf("piped(%q) = %v then %v, want %v both times", line, first, again, want)
		}
	}
}

func TestAC7ConfigWrittenIsWhereACopyOrALinkWrites(t *testing.T) {
	for _, c := range []struct {
		command string
		want    []string
	}{
		// The link is made where the command runs, under the target's name.
		{"ln -s /x/.claude.json", []string{".claude.json"}},
		{"ln -s /x/a.json dir", []string{"dir", "dir/a.json"}},
		{"cp a b dir", []string{"dir", "dir/a", "dir/b"}},
		{"install -m 644 a dir", []string{"dir", "dir/a"}},
	} {
		got := configWritten(strings.Fields(c.command))
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("configWritten(%q) = %q, want it to hold %q", c.command, got, w)
			}
		}
	}
	// A copy's sources are read, not written.
	if got := configWritten(strings.Fields("cp ~/.claude/settings.json backup")); slices.Contains(got, "~/.claude/settings.json") {
		t.Errorf("configWritten read a copy's source as written: %q", got)
	}
}

func TestAC6CopiesAProgramIsACopyAMoveOrALink(t *testing.T) {
	for command, want := range map[string]bool{
		"cp /usr/local/bin/claude /tmp/x":    true,
		"ln -s /usr/local/bin/claude /tmp/x": true,
		"mv a b":                             true,
		"rsync a b":                          true,
		"Copy-Item a b":                      true,
		"New-Item -ItemType SymbolicLink -Path x -Target y": true,
		"ni -ItemType HardLink -Path x -Target y":           true,
		"New-Item -ItemType File -Path x":                   false,
		"cat /usr/local/bin/claude":                         false,
	} {
		if got := copiesAProgram(ran{words: strings.Fields(command)}); got != want {
			t.Errorf("copiesAProgram(%q) = %v, want %v", command, got, want)
		}
	}
	if copiesAProgram(ran{}) {
		t.Error("no words were read as a copy")
	}
}

// red-team, round 4: perl given a directory for its includes still reads its
// program from standard input, as a bare perl does, so the program it is fed
// is read for a Claude Code session that would expand a person's command.
func TestAC6APerlGivenAnIncludeDirectoryIsReadAsABarePerlForTheProgramItIsFed(t *testing.T) {
	tempHomeState(t)
	none := func(string) bool { return false }
	const code = `printf '%s' 'system("claude","-p","/sdlc:approve")' | `
	for _, perl := range []string{"perl", "perl -I lib"} {
		command := code + perl
		f, ok := HumanDecisions(command, false, none)
		if !ok || f.Rule != "story-stays-in-its-session" || f.Route == "" {
			t.Errorf("AC-6 %q = %+v, %v; want it refused as story-stays-in-its-session, with a route", command, f, ok)
		}
	}
}

// red-team, round 4: the shell expands the unquoted word whatever a quoted
// twin of it earlier on the line says, so a session that holds no story is
// refused the removal. The quoted word alone names a file called .cla*, which
// is nobody's configuration.
func TestAC7ClaudeConfigWritesReadsEveryWordThatSpellsTheGlobNotOnlyTheFirst(t *testing.T) {
	s, _ := tempHomeState(t)
	for _, command := range []string{
		`rm -rf "$HOME/.cla*" $HOME/.cla*`,
		`cd ~ && rm -rf ".cla*" .cla*`,
		`cd ~ && rm -rf '.cla*' .cla*`,
	} {
		f, ok := ClaudeConfigWrites(command, s)
		if !ok || f.Rule != ruleClaudeConfig || f.Reason == "" || f.Route == "" {
			t.Errorf("AC-7 %q = %+v, %v; want it refused as %s, with a reason and a route", command, f, ok, ruleClaudeConfig)
		}
	}
	if f, ok := ClaudeConfigWrites(`rm -rf "$HOME/.cla*"`, s); ok {
		t.Errorf("AC-7 a removal of the file named .cla* in quotes was refused: %+v", f)
	}
}
