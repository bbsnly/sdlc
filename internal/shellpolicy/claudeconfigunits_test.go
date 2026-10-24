package shellpolicy

// US-108 AC-6 and AC-7, the readings claudeconfig.go is built from, one at a
// time. claudeconfig_test.go holds the rows ANALYSIS.md section 2.8
// reproduced; these hold what each reading decides, so that a change to one
// shows where it is. The person's home is the caller's to place, through
// State.ClaudeConfig; tempHomeState places it at t.TempDir() and points HOME
// there too, so that a reading which came to ask the environment would still
// never reach a real ~/.claude.

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tempHomeState is claudeConfigState with the person's home at a temp dir,
// which HOME and USERPROFILE name, and that home named as a directory that
// holds the configuration. It returns the home, slash-separated, as a command
// would spell it.
func tempHomeState(t *testing.T) (State, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	home := filepath.ToSlash(dir)
	config := home + "/.claude"
	s := claudeConfigState()
	tilde := s.ClaudeConfig
	s.ClaudeConfig = func(word string) bool {
		c := clean(word)
		return tilde(word) || c == config || strings.HasPrefix(c, config+"/")
	}
	s.ClaudeConfigHeld = func(word string) bool {
		c := clean(word)
		return c == "~" || c == home
	}
	return s, home
}

// claudeConfigWritten are commands that write the person's Claude Code
// configuration, with the home at home. US-023 asks the same rows of ~/.agents
// (agentsmd_test.go), which is why they are a table of their own.
func claudeConfigWritten(home string) []string {
	return []string{
		"echo x > ~/.claude/settings.json",
		`echo x > "` + home + `/.claude/settings.json"`,
		`rm -rf "` + home + `/.claude/hooks"`,
		`cp -r dotfiles/. "` + home + `"`,
		"rm -rf ~/.claude/skills",
		"tar -xf skills.tar -C ~/.claude",
		"unzip skills.zip -d ~/.claude/skills",
		"make -C ~/.claude",
		"git -C ~/.claude/plugins/p pull",
		"git clone https://example.com/p.git ~/.claude/plugins/p",
		"cp -r dotfiles/. ~",
		"claude config set -g theme dark",
		"claude plugin marketplace add example/market",
		`python3 -c 'import os; open(os.path.expanduser("~/.claude/settings.json"), "w")'`,
		"X=~/.claude; echo x > $X/settings.json",
		"echo x > ~bob/.claude/settings.json",
	}
}

// claudeConfigRead are commands that only read the configuration, or write
// somewhere else, with the home at home.
func claudeConfigRead(home string) []string {
	return []string{
		"cat ~/.claude/settings.json",
		"cp ~/.claude/settings.json backup.json",
		"claude mcp list",
		"claude config get theme",
		"claude plugin install --help",
		"echo x > $OUT/notes.txt",
		"cp notes.txt ~/notes.txt",
		`cat "` + home + `/.claude/settings.json"`,
		`cp notes.txt "` + home + `/notes.txt"`,
		"echo x > internal/app.go",
	}
}

func TestAC7ClaudeConfigWritesRefusesWritesToTheConfigurationAndLetsReadsThrough(t *testing.T) {
	if _, ok := ClaudeConfigWrites("echo x > ~/.claude/settings.json", ready); ok {
		t.Error("with no way to place the configuration, a command was refused for writing it")
	}
	s, home := tempHomeState(t)
	for _, command := range claudeConfigWritten(home) {
		f, ok := ClaudeConfigWrites(command, s)
		if !ok || f.Rule != ruleClaudeConfig {
			t.Errorf("AC-7 %q = %+v, %v; want it refused as %s", command, f, ok, ruleClaudeConfig)
			continue
		}
		if f.Reason == "" || f.Route == "" {
			t.Errorf("AC-7 the refusal of %q does not say why and what to do: %+v", command, f)
		}
	}
	for _, command := range claudeConfigRead(home) {
		if f, ok := ClaudeConfigWrites(command, s); ok {
			t.Errorf("AC-7 %q was refused: %+v", command, f)
		}
	}
}

func TestAC7ValueOfReadsAnOptionGluedOnOrInTheNextWord(t *testing.T) {
	args := []string{"-d", "one", "-dtwo", "--directory=three", "-d"}
	if got := valueOf(args, "-d"); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("valueOf(-d) = %q, want [one two]: a -d with nothing after it gives nothing", got)
	}
	if got := valueOf(args, "--directory"); !slices.Equal(got, []string{"three"}) {
		t.Errorf("valueOf(--directory) = %q, want [three]", got)
	}
	if got := valueOf([]string{"x"}, "-C"); got != nil {
		t.Errorf("valueOf of an option not given = %q, want none", got)
	}
}

func TestAC7NonOptionsAreTheWordsThatAreNotOptions(t *testing.T) {
	if got := nonOptions([]string{"-r", "src", "--force", "dest"}); !slices.Equal(got, []string{"src", "dest"}) {
		t.Errorf("nonOptions = %q, want [src dest]", got)
	}
	if got := nonOptions([]string{"-q"}); got != nil {
		t.Errorf("nonOptions of options alone = %q, want none", got)
	}
}

func TestAC7IntoDirectoriesAreTheDirectoriesAnOptionWritesInto(t *testing.T) {
	for _, c := range []struct {
		command string
		want    []string
	}{
		{"tar -xf a.tar -C out", []string{"out"}},
		{"tar -cf a.tar -C src .", nil},
		{"unzip a.zip -d out", []string{"out"}},
		{"make -C out", []string{"out"}},
		{"gmake --directory=out", []string{"out"}},
		{"git -C out pull", []string{"out"}},
		{"git clone https://example.com/p.git dest", []string{"dest"}},
		{"git -C out clone https://example.com/p.git dest", []string{"out", "out/dest"}},
		{"git -C out log", nil},
		{"cat out", nil},
	} {
		if got := intoDirectories(strings.Fields(c.command)); !slices.Equal(got, c.want) {
			t.Errorf("intoDirectories(%q) = %q, want %q", c.command, got, c.want)
		}
	}
}

func TestAC7CopiesWholeIsACopyThatTakesDirectoriesWithIt(t *testing.T) {
	for command, want := range map[string]bool{
		"cp -r a b":              true,
		"cp -a a b":              true,
		"cp --archive a b":       true,
		"cp a b":                 false,
		"copy-item a b -Recurse": true,
		"copy-item a b":          false,
		"copy a b /s":            true,
		"rsync -v a b":           true,
		"mv a b":                 true,
		"move-item a b":          true,
		"cat a":                  false,
	} {
		if got := copiesWhole(strings.Fields(command)); got != want {
			t.Errorf("copiesWhole(%q) = %v, want %v", command, got, want)
		}
	}
}

func TestAC7NamesClaudeConfigSeesTheConfigurationInAnySpellingTheShellCouldPlace(t *testing.T) {
	for line, want := range map[string]bool{
		"cat ~/.claude/settings.json":        true,
		"ls /home/alice/.claude.json":        true,
		"echo $CLAUDE_CONFIG_DIR":            true,
		"echo $claude_code_plugin_cache_dir": true,
		"ls ~/.cla*":                         true,
		"ls ~/[.]claude":                     true,
		"ls ~/.claude.":                      true,
		`cat "C:\Users\a\.claude\x"`:         true,
		"cat CLAUDE.md":                      false,
		"cat ~/.clauderc":                    false,
		"ls src":                             false,
		"ls ~/.c*x":                          false,
	} {
		if got := namesClaudeConfig(line); got != want {
			t.Errorf("namesClaudeConfig(%q) = %v, want %v", line, got, want)
		}
		m := &memo{}
		if first, again := m.namesClaudeConfig(line), m.namesClaudeConfig(line); first != want || again != want {
			t.Errorf("the memo's namesClaudeConfig(%q) = %v then %v, want %v both times", line, first, again, want)
		}
	}
}

func TestAC7ClaudeChangesItsConfigNamesTheSubcommandThatChangesIt(t *testing.T) {
	for _, c := range []struct {
		command string
		sub     string
		changes bool
	}{
		{"claude config set -g theme dark", "config set", true},
		{"claude config remove allowedTools x", "config remove", true},
		{"claude config get theme", "config get", false},
		{"claude mcp add s ./server", "mcp add", true},
		{"claude mcp add-json s {}", "mcp add-json", true},
		{"claude mcp list", "mcp list", false},
		{"claude plugin install p", "plugin install", true},
		{"claude plugins disable p", "plugins disable", true},
		{"claude plugin marketplace add example/m", "plugin marketplace add", true},
		{"claude plugin marketplace list", "plugin marketplace list", false},
		{"npx @anthropic-ai/claude-code config set k v", "config set", true},
		{"npm exec @anthropic-ai/claude-code -- mcp remove s", "mcp remove", true},
	} {
		sub, changes := claudeChangesItsConfig(strings.Fields(c.command))
		if changes != c.changes || sub != c.sub {
			t.Errorf("claudeChangesItsConfig(%q) = %q, %v; want %q, %v", c.command, sub, changes, c.sub, c.changes)
		}
	}
	for _, command := range []string{
		"claude plugin install --help",
		"claude config",
		"claude -p hello",
		"git config set x y",
		"npm install @anthropic-ai/claude-code",
	} {
		if sub, changes := claudeChangesItsConfig(strings.Fields(command)); changes {
			t.Errorf("claudeChangesItsConfig(%q) = %q, true; it changes nothing", command, sub)
		}
	}
}

func TestAC6ClaudeArgsAreTheWordsClaudeCodeIsGiven(t *testing.T) {
	for _, c := range []struct {
		command string
		want    []string
	}{
		{"claude -p hi", []string{"-p", "hi"}},
		{"npx -y @anthropic-ai/claude-code -p hi", []string{"-p", "hi"}},
		{"pnpm dlx @anthropic-ai/claude-code mcp list", []string{"mcp", "list"}},
		{"npm exec", nil},
		{"npx -y", nil},
	} {
		if got := claudeArgs(strings.Fields(c.command)); !slices.Equal(got, c.want) {
			t.Errorf("claudeArgs(%q) = %q, want %q", c.command, got, c.want)
		}
	}
}

func TestAC6RunsClaudeProgramAndStartsClaudeCodeReadEverySpellingThatStartsASession(t *testing.T) {
	for command, want := range map[string]bool{
		"claude -p hi":                               true,
		"/usr/local/bin/claude -p hi":                true,
		"claude.cmd -p hi":                           true,
		"~/.local/share/claude/versions/2.1.0 -p hi": true,
		"cla*de -p hi":                               true,
		"npx @anthropic-ai/claude-code":              true,
		"bun x @anthropic-ai/claude-code":            true,
		"npm install @anthropic-ai/claude-code":      false,
		"npm":                                        false,
		"claudette":                                  false,
	} {
		if got := runsClaudeProgram(strings.Fields(command)); got != want {
			t.Errorf("runsClaudeProgram(%q) = %v, want %v", command, got, want)
		}
	}
	if runsClaudeProgram(nil) || startsClaudeCode(nil) {
		t.Error("no words start nothing")
	}
	for command, want := range map[string]bool{
		"Start-Process claude": true,
		"start claude":         true,
		"node node_modules/@anthropic-ai/claude-code/cli.js -p hi": true,
		"python3 agent.py --sdk claude-agent-sdk":                  true,
		"npm install @anthropic-ai/claude-code":                    false,
		"pip install claude-agent-sdk":                             false,
		"start notepad":                                            false,
		"echo hello":                                               false,
	} {
		if got := startsClaudeCode(strings.Fields(command)); got != want {
			t.Errorf("startsClaudeCode(%q) = %v, want %v", command, got, want)
		}
	}
}

func TestAC6MentionsClaudeIsTheProgramOrThePackageAndNotTheConfiguration(t *testing.T) {
	for text, want := range map[string]bool{
		`subprocess.run(["claude", "-p", "x"])`: true,
		"import claude_agent_sdk":               true,
		"cat ~/.claude/settings.json":           false,
		"echo hello":                            false,
	} {
		if got := mentionsClaude(text); got != want {
			t.Errorf("mentionsClaude(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestAC6ReadablePromptIsOneWithNothingTheShellHasYetToExpand(t *testing.T) {
	for _, c := range []struct {
		args, marked []string
		want         bool
	}{
		{[]string{"-p", "/sdlc:next"}, nil, true},
		{[]string{"-p", "$PROMPT"}, nil, false},
		{[]string{"-p", "%PROMPT%"}, nil, false},
		{[]string{"-p", "`cat p`"}, nil, false},
		{[]string{"-p", "x"}, []string{"claude", "$SUB"}, false},
	} {
		if got := readablePrompt(c.args, c.marked); got != c.want {
			t.Errorf("readablePrompt(%q, %q) = %v, want %v", c.args, c.marked, got, c.want)
		}
	}
}

// AC-6: a second session is refused only where the session it starts could
// expand a person's command; one that plainly cannot is let through.
func TestAC6CheckSecondSessionRefusesOnlyASessionThatCouldExpandAPersonsCommand(t *testing.T) {
	tempHomeState(t)
	for _, command := range []string{
		`claude -p "/sdlc:approve US-001"`,
		`claude -p "$PROMPT"`,
		`tmux new-session -d claude`,
		`c=claude; $c -p "/sdlc:ack --through HEAD"`,
		`Start-Process claude -ArgumentList "/approve"`,
	} {
		f, ok := checkSecondSession(command, false)
		if !ok || f.Rule != "story-stays-in-its-session" || f.Route == "" {
			t.Errorf("AC-6 %q = %+v, %v; want it refused as story-stays-in-its-session, with a route", command, f, ok)
		}
	}
	for _, command := range []string{
		"claude --version",
		"claude mcp list",
		`claude -p "summarise the diff"`,
		"echo /sdlc:approve",
		"go test ./...",
	} {
		if f, ok := checkSecondSession(command, false); ok {
			t.Errorf("AC-6 %q was refused: %+v", command, f)
		}
	}
}
