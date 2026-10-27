package shellpolicy

import (
	"path"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// ruleClaudeConfig is the rule a write to the person's Claude Code
// configuration is refused under, from the file tools and from the shell.
const ruleClaudeConfig = "claude-config-is-the-persons"

// claudeConfigRoute is what to do instead. internal/policy says the same of a
// file tool's write.
const claudeConfigRoute = "say what you would change there and why, and stop: the person changes their own " +
	"Claude Code configuration, in Claude Code's settings or by hand"

// claudeConfigReason is why: what the configuration decides.
const claudeConfigReason = "the person's Claude Code configuration, which decides the hooks, plugins, skills, " +
	"agents and permissions every session runs under, so a session that wrote it could change the rules it is held to"

// ruleAgentsConfig is the rule a write to the person's ~/.agents is refused
// under, from the file tools and from the shell: the twin of
// ruleClaudeConfig, for the folder other agent tools load skills from.
const ruleAgentsConfig = "agents-config-is-the-persons"

// agentsConfigRoute and agentsConfigReason are what to do instead, and why.
// internal/policy says the same of a file tool's write.
const agentsConfigRoute = "say what you would change there and why, and stop: the person adds or changes a " +
	"skill in their own ~/.agents, by hand or from their own terminal, outside the session"

const agentsConfigReason = "in the person's ~/.agents, the folder Codex, Cursor, Copilot, Gemini CLI and other " +
	"agent tools load skills from (it has nothing to do with sdlc's own agents), so a skill written there runs " +
	"in the person's next session with one of them"

// ClaudeConfigWrites reports a command that writes the person's Claude Code
// configuration or their ~/.agents: the one shell rule the hook asks in a
// session that holds no story, where none of the others applies. It reads the
// command as Inspect does, with the same readings and the same walk of where
// it runs, and asks nothing but this; only Dir, Root, PowerShell and the
// ClaudeConfig and AgentsConfig pairs of s are read.
func ClaudeConfigWrites(command string, s State) (Finding, bool) {
	if s.ClaudeConfig == nil && s.ClaudeConfigHeld == nil && s.AgentsConfig == nil && s.AgentsConfigHeld == nil {
		// Allowed without the bound on links, which is sound: with nothing to
		// ask, no word is placed on disk and no link is followed. The hook
		// always sets all four.
		return Finding{}, false
	}
	s.configOnly = true
	s.seen = newInspection()
	s.seen.line = command
	return inspectAs(command, command, s)
}

// configKind is one of the person's places the shell holds in every session:
// ~/.claude or ~/.agents, with the rule's words and the caller's callbacks.
type configKind struct {
	rule, route string
	// what is what the place is, after "<word> is "; named is the place as
	// a refusal names it.
	what, named string
	in, held    func(word string) bool
	// agents is whether this is ~/.agents, whose names make an unplaced
	// word suspect in place of .claude's.
	agents bool
}

// checkPersonsConfig refuses a command that writes either of the person's
// places. ~/.agents is asked first: a command that fills or empties a whole
// home (`cp -r dotfiles/. ~`, `rm -rf ~`) writes both, and its refusal names
// the folder that is less known to be there; any other write into ~/.claude
// is not under ~/.agents, so it keeps its own rule.
func checkPersonsConfig(q pathQuestion) (Finding, bool) {
	s := q.s
	if f, ok := checkConfig(q, agentsKind(s)); ok {
		return f, true
	}
	claude := configKind{
		rule: ruleClaudeConfig, route: claudeConfigRoute, what: claudeConfigReason,
		named: "the person's Claude Code configuration", in: s.ClaudeConfig, held: s.ClaudeConfigHeld,
	}
	return checkConfig(q, claude)
}

// agentsKind is the person's ~/.agents, as checkPersonsConfig and
// checkAgentsNamedWord ask it.
func agentsKind(s State) configKind {
	return configKind{
		rule: ruleAgentsConfig, route: agentsConfigRoute, what: agentsConfigReason,
		named: "the person's ~/.agents", in: s.AgentsConfig, held: s.AgentsConfigHeld, agents: true,
	}
}

// checkAgentsNamedWord refuses, ahead of the loop-state rule, a word the
// command writes whose own spelling names .agents and that is or could be in
// the person's ~/.agents. .agents is protected at any depth by name, so the
// loop-state rule refused `echo x > ~bob/.agents/settings.json` and `echo x >
// $HOME/.agents/x` as the project's own. Only such a word is asked: asked of
// the whole line, as checkPersonsConfig asks it, `cd "$D" && rm -rf
// .sdlc/state && ls .agents` and `python3 -c 'open(".agents/x","w")'` lost
// the released rules that refuse them.
func checkAgentsNamedWord(q pathQuestion) (Finding, bool) {
	k := agentsKind(q.s)
	if k.in == nil || len(q.run.words) == 0 {
		return Finding{}, false
	}
	return q.writtenConfig(k, true)
}

// checkConfig refuses a command of one segment that writes one of the
// person's places: a word it writes or takes away that the caller places
// there, one that cannot be placed in a command that names it, and, for
// ~/.claude, Claude Code's own commands that change it.
//
// The words asked are the ones the loop-state rule asks, read for what they
// write rather than what they name: a copy's destination and not its source,
// and sed or perl only when they edit in place, so that reading the
// configuration stays free.
func checkConfig(q pathQuestion, k configKind) (Finding, bool) {
	run := q.run
	if k.in == nil || len(run.words) == 0 {
		return Finding{}, false
	}
	if sub, ok := claudeChangesItsConfig(run.words); ok && !k.agents {
		return Finding{
			Rule:   ruleClaudeConfig,
			Reason: "`claude " + sub + "` changes " + claudeConfigReason,
			Route:  claudeConfigRoute,
		}, true
	}
	// A program places its own paths: `python3 -c` with expanduser("~/.claude")
	// or HOME+"/.claude.json" names a place the shell never sees whole.
	if q.runsCode() && q.m.namesConfig(q.line, k.agents, false) {
		return Finding{
			Rule: k.rule,
			Reason: "the inline code names " + k.named + ", and where it writes " +
				"is the program's to work out, so it could write " + k.what,
			Route: "if the code only reads the configuration, read it with a plain command such as `cat`; " +
				"otherwise " + k.route,
		}, true
	}
	return q.writtenConfig(k, false)
}

// writtenConfig is the refusal of the first word the command writes that is
// the place, or a directory that holds it. With spelt set, only a word whose
// own spelling names the place is asked.
func (q pathQuestion) writtenConfig(k configKind, spelt bool) (Finding, bool) {
	inside, held := q.configWords()
	// After a cd into the home, as `cd ~`, a bare `cd` or `cd $HOME/sub`, a
	// relative word is asked from there as well as by its name: read by its
	// name alone, `cd ~ && touch l/settings.json`, with l a link to
	// ~/.claude, wrote the person's configuration (US-035, from US-120).
	dirs := []string{q.dir}
	if q.lost && (q.from == "~" || strings.HasPrefix(q.from, "~/")) {
		dirs = append(dirs, q.from)
	}
	for _, group := range []struct {
		words []string
		held  bool
	}{{inside, false}, {held, true}} {
		// Operands first, so the refusal names the word the command acts on:
		// asked in order, `cd ~/lc && rm -f x` named ~/lc/-f as the
		// configuration (US-035, verifier round 12). Options are still asked.
		for _, raw := range optionsLast(group.words) {
			if spelt && !q.spells(k, raw) {
				continue
			}
			if f, ok := q.configWordFrom(k, dirs, raw, group.held); ok {
				return f, true
			}
		}
	}
	return Finding{}, false
}

// configWordFrom is configWord of raw spelled from each of dirs in turn.
//
// A word it found nothing in is not asked again with the same answer to give:
// a line fed on standard input hands every word of it to each segment that
// writes, so `echo <20 paths> | xargs rm;` a hundred times asked 2000 words
// from each of 100 segments, twice over for ~/.agents and ~/.claude, and
// took three quarters of the time Inspect spent on that 45 KB command. Only
// what found nothing is kept, so the first word refused is the one it always
// was.
func (q pathQuestion) configWordFrom(k configKind, dirs []string, raw string, held bool) (Finding, bool) {
	ask := q.configAskOf(k, raw, held)
	if q.m.configAsked(q.line)[ask] {
		return Finding{}, false
	}
	for _, dir := range dirs {
		for _, w := range q.m.spell(q.s, dir, []string{raw}) {
			if f, ok := q.configWord(k, w, raw, held); ok {
				return f, true
			}
		}
	}
	q.m.configNothing[ask] = true
	return Finding{}, false
}

// configAsk is everything configWordFrom's answer for one word turns on but
// the State and the line, which are one for a memo: where the walk is, lost
// or not and spelt from ~ (writtenConfig's dirs, and configUnplaced), and,
// for a word that names the place, whether the shell expands a glob in it in
// this segment (expandsAGlob), the one question configWord asks of the
// segment.
type configAsk struct {
	rule, dir, from, raw string
	lost, held           bool
	glob                 int8
}

// configAskOf is the configAsk of raw, asked of k from where q is.
func (q pathQuestion) configAskOf(k configKind, raw string, held bool) configAsk {
	ask := configAsk{rule: k.rule, dir: q.dir, from: q.from, raw: raw, lost: q.lost, held: held}
	if q.m.wordNamesConfig(raw, k.agents) {
		ask.glob = 1
		if q.expandsAGlob(raw) {
			ask.glob = 2
		}
	}
	return ask
}

// configAsked is the words configWordFrom found nothing in, for line: the
// memo is one reading's, and its line is one command, but a set kept for
// another line is not trusted.
func (m *memo) configAsked(line string) map[configAsk]bool {
	if m.configNothing == nil || m.configFor != line {
		m.configNothing, m.configFor = map[configAsk]bool{}, line
	}
	return m.configNothing
}

// spells reports whether a word spells the configuration out by its name,
// and does not climb back out of ~/.agents to land beside it.
func (q pathQuestion) spells(k configKind, raw string) bool {
	return q.m.wordNamesConfig(raw, k.agents) && (!k.agents || !climbsOutOf(raw, ".agents"))
}

// wordNamesConfig is namesConfigAs of one word, with globs read as names,
// worked out once a word: the words of a whole line are asked it again for
// each segment that writes, and lowering and splitting each one again was
// most of what configWordFrom had left to do on a 45 KB command.
func (m *memo) wordNamesConfig(raw string, agents bool) bool {
	key := configName{raw: raw, agents: agents}
	named, ok := m.namesWord[key]
	if !ok {
		named = namesConfigAs(raw, true, agents)
		if m.namesWord == nil {
			m.namesWord = map[configName]bool{}
		}
		m.namesWord[key] = named
	}
	return named
}

// configName is a word wordNamesConfig is asked, and of which place.
type configName struct {
	raw    string
	agents bool
}

// climbsOutOf reports whether a word that names a directory called name
// leaves it again with `..`, counting levels, so that where it lands is not
// in any such directory: `$D/.agents/../CLAUDE.md` is beside ~/.agents, not
// in it, and was told it was the person's ~/.agents, losing the rule its
// landing has. `.agents/x/..` stays inside.
func climbsOutOf(word, name string) bool {
	named, depth := false, -1
	for seg := range strings.SplitSeq(clean(word), "/") {
		switch {
		case pathrules.SameName(seg, name):
			named, depth = true, 0
		case seg == "..":
			if depth >= 0 {
				depth--
			}
		case seg != "" && seg != "." && depth >= 0:
			depth++
		}
	}
	return named && depth < 0
}

// configWord is the refusal of one word the command writes, where it is the
// place, or with held set, a directory that holds it; or where it cannot be
// placed and the command names the place. raw is the word as the command
// gave it, before it was placed.
func (q pathQuestion) configWord(k configKind, w, raw string, held bool) (Finding, bool) {
	c := clean(w)
	if c == "" || strings.HasPrefix(c, "-") {
		return Finding{}, false
	}
	if k.in(w) || held && k.held != nil && k.held(w) {
		return Finding{Rule: k.rule, Reason: shortened(c) + " is " + k.what, Route: k.route}, true
	}
	// A glob is placed by the directory the words spell before it: with l a
	// link in the home to ~/.claude, `rm -f ~/l/*` named nothing the hook
	// could place, nor the configuration, and was let through (US-035,
	// security review round 4).
	if dir, ok := globDir(c); ok && k.in(dir) {
		return Finding{Rule: k.rule, Reason: shortened(c) + " is " + k.what, Route: k.route}, true
	}
	// A glob names the configuration only in the word it is written in, and
	// only where the shell expands it; elsewhere on the line only a name
	// spelled out does. The whole line's reading with globs is the cheap
	// question first: the word is on the line, so where that finds nothing
	// neither can the others.
	if q.configUnplaced(c) && q.m.namesConfig(q.line, k.agents, true) &&
		(q.m.namesConfig(q.line, k.agents, false) || q.m.wordNamesConfig(raw, k.agents) && q.expandsAGlob(raw)) {
		return Finding{
			Rule: k.rule,
			Reason: shortened(c) + " is not known until the shell runs the command, which names " +
				k.named + ", so it could be " + k.what,
			Route: "write it with the path spelled out, if it is not " + k.named + "; " +
				"otherwise " + k.route,
		}, true
	}
	return Finding{}, false
}

// globDir is the directory a cleaned word spells before the first of its
// names that holds a glob, where it spells one with no variable in it.
func globDir(c string) (string, bool) {
	segs := strings.Split(c, "/")
	i := slices.IndexFunc(segs, func(seg string) bool { return strings.ContainsAny(seg, "*?[") })
	if i <= 0 {
		return "", false
	}
	dir := strings.Join(segs[:i], "/")
	if dir == "" || strings.ContainsAny(dir, "$%`") {
		return "", false
	}
	return dir, true
}

// runsCode reports whether the segment's command runs a program it is handed
// rather than one in a file: inline code, or an interpreter reading its
// program from standard input. The walk splits a line at its pipes, so a pipe
// anywhere on the line counts as feeding it.
func (q pathQuestion) runsCode() bool {
	return runsInlineCode(q.run.words, q.segment) ||
		programOnStdin(q.run.words, strings.Contains(q.segment, "<") || q.m.piped(q.line))
}

// piped reports whether a line holds a pipe, worked out once for the line.
func (m *memo) piped(line string) bool {
	if m.pipe == 0 {
		m.pipe = -1
		if strings.Contains(line, "|") {
			m.pipe = 1
		}
	}
	return m.pipe > 0
}

// stdinValued are the options of an interpreter that take their value as the
// next word, so that the value is not read as the script it runs, and whether
// the name is an interpreter's at all. A switch and not a map, which would be
// built in the package's init.
func stdinValued(name string) ([]string, bool) {
	switch name {
	case "python":
		return []string{"-W", "-X"}, true
	case "node":
		return []string{"-r", "--require", "--import", "-C", "--conditions"}, true
	case "bun":
		return []string{"-r", "--require", "--preload"}, true
	case "ruby":
		return []string{"-r", "-I", "-C", "-E"}, true
	case "php":
		return []string{"-c", "-d", "-z"}, true
	case "perl":
		// Read as a script, the directory of `perl -I lib` hid the program
		// perl was fed on standard input.
		return []string{"-I"}, true
	}
	return nil, false
}

// programOnStdin reports whether an interpreter fed on standard input reads
// its program from there: it names no script and no module. `printf '%s'
// "$code" | python3` and `node <<< "$code"` run what they are given as surely
// as `python3 -c` does, with no `-` to say so.
func programOnStdin(words []string, fed bool) bool {
	if !fed || len(words) == 0 {
		return false
	}
	name := base(words[0])
	if strings.HasPrefix(name, "python") || name == "py" {
		name = "python"
	}
	valued, ok := stdinValued(name)
	if !ok {
		return false
	}
	for i := 1; i < len(words); i++ {
		switch a := words[i]; {
		case a == "-":
			return true
		case a == "--":
			return i+1 == len(words) || words[i+1] == "-"
		case name == "python" && strings.HasPrefix(a, "-m"):
			return false
		case strings.HasPrefix(a, "-"):
			if slices.Contains(valued, a) {
				i++
			}
		default:
			return false
		}
	}
	return true
}

// expandsAGlob reports whether the shell would expand a glob in a word the
// segment gives: a `*`, `?` or `[` outside every quote and not escaped, as the
// command spells it. `"$HOME"/.cla*""` is expanded and `'s/.*//'` is a sed
// script. Every word that unquotes to raw is read, and one expanded is
// enough: read by the first alone, the quoted twin in `rm -rf "$HOME/.cla*"
// $HOME/.cla*` hid the bare one the shell expands. A word the segment does not
// spell as a word of its own, a redirect's target or a name the walk made, is
// read as expanded.
func (q pathQuestion) expandsAGlob(raw string) bool {
	m := q.m
	if m.spelledFor != q.segment || m.spelled == nil {
		m.spelledFor = q.segment
		m.unspelled, m.spelled = spelledFields(q.segment)
		m.globbed = nil
	}
	// The same for every word that unquotes to raw, so worked out once: asked
	// for each of k twins, each reading all k, the walk was quadratic in them.
	if expanded, ok := m.globbed[raw]; ok {
		return expanded
	}
	expanded, found := false, false
	for i, w := range m.unspelled {
		if w == raw {
			found = true
			expanded = expanded || globOutsideQuotes(m.spelled[i])
		}
	}
	expanded = expanded || !found
	if m.globbed == nil {
		m.globbed = map[string]bool{}
	}
	m.globbed[raw] = expanded
	return expanded
}

// globOutsideQuotes reports whether a word, as the command spells it, holds a
// `*`, `?` or `[` outside every quote and not escaped.
func globOutsideQuotes(spelled string) bool {
	var quote byte
	for j := 0; j < len(spelled); j++ {
		switch c := spelled[j]; {
		case c == quote:
			quote = 0
		case c == '\\' && quote != '\'':
			j++
		case quote != 0:
		case c == '\'' || c == '"':
			quote = c
		case c == '*' || c == '?' || c == '[':
			return true
		}
	}
	return false
}

// configUnplaced reports whether a written word is one whose place the shell
// settles and the caller cannot: an expansion or a glob, another user's home,
// or a relative path from somewhere the walk has lost. `X=~/.claude; cd "$X";
// echo > settings.json` writes there in words none of which says so.
func (q pathQuestion) configUnplaced(c string) bool {
	switch {
	case strings.ContainsAny(c, "$%`*?["):
		return true
	case strings.HasPrefix(c, "~"):
		return c != "~" && !strings.HasPrefix(c, "~/")
	}
	return q.lost && !isAbsolute(c)
}

// configWords are the words of the segment's command that could write the
// configuration: inside are asked whether they are in it, and held whether
// they are in it or hold it, which only a command that writes a whole
// directory's worth or takes one away can do to a home.
func (q pathQuestion) configWords() (inside, held []string) {
	run, m := q.run, q.m
	words := run.words
	inside = append(inside, q.redirects...)
	if changesAFile(words) {
		if copiesWhole(words) {
			held = append(held, configWritten(words)...)
		} else {
			inside = append(inside, configWritten(words)...)
		}
	}
	// The file a program opens is inside a string, and a command fed on
	// standard input is given words from elsewhere on the line.
	if q.runsCode() || run.piped && changesFiles(words) {
		inside = append(inside, m.words(q.line)...)
	}
	inside = append(inside, q.pipedLandings()...)
	inside = append(inside, q.directoryLandings()...)
	listed, _ := q.listedNames()
	inside = append(inside, listed...)
	held = append(held, takenAway(words)...)
	held = append(held, intoDirectories(words)...)
	if findChanges(words) {
		roots, _, _ := findArguments(words[1:])
		held = append(held, roots...)
	}
	return inside, held
}

// configWritten is what a command writes, as writtenTo has it, except that a
// copy writes only where it copies to: `cp ~/.claude/settings.json backup`
// reads the configuration. A link and a move write what writtenTo says,
// their landings among it; a move removes its source, which is written too.
//
// Each source's name under the destination is asked too, whatever the
// destination's own name looks like (everyLanding): read as a file, a home
// whose name holds a dot let `cp x/.claude.json` into that home spelled out
// in full write the person's configuration with nothing asked.
func configWritten(words []string) []string {
	switch name := base(words[0]); name {
	case "cp", "copy", "copy-item", "cpi", "install":
		_, to, _ := moveArguments(words[1:])
		out := []string{to}
		if _, into, ok := bundleTarget(name, words[1:]); ok {
			out = append(out, into)
		}
		return append(out, everyLanding(words, true)...)
	}
	return writtenTo(words)
}

// copiesWhole reports whether a copy takes directories with it, so that its
// destination can be a home it fills: `cp -r dotfiles/. ~`.
func copiesWhole(words []string) bool {
	args := words[1:]
	switch base(words[0]) {
	case "cp":
		return recursive(args) || hasWord(args, "--archive") || shortFlag(args, 'a')
	case "copy-item", "cpi", "copy":
		for _, a := range args {
			if strings.EqualFold(a, "-Recurse") || strings.EqualFold(a, "/s") || strings.EqualFold(a, "/e") {
				return true
			}
		}
	case "rsync", "scp", "mv", "move", "move-item", "mi":
		return true
	}
	return false
}

// intoDirectories are the directories a command is told to write into by an
// option: tar's and make's -C, unzip's -d, and git's -C for a git that writes
// files there, with the directory a clone makes.
func intoDirectories(words []string) []string {
	args := words[1:]
	switch base(words[0]) {
	case "tar":
		_, _, into, _, extract := tarArguments(args)
		if extract {
			return into
		}
	case "unzip":
		return valueOf(args, "-d")
	case "make", "gmake":
		return append(valueOf(args, "-C"), valueOf(args, "--directory")...)
	case "git":
		sub, dir, rest, _ := gitCall(args)
		var out []string
		if dir != "" && (gitChangesFiles(args) || sub == "clone" || sub == "init" || sub == "pull" || sub == "reset") {
			out = append(out, dir)
		}
		if sub == "clone" || sub == "init" {
			if operands := nonOptions(rest); len(operands) > 0 {
				out = append(out, path.Join(dir, operands[len(operands)-1]))
			}
		}
		return out
	}
	return nil
}

// valueOf is the value of option name wherever it is among args, glued on or
// in the word after.
func valueOf(args []string, name string) []string {
	var out []string
	for i, a := range args {
		switch {
		case a == name && i+1 < len(args):
			out = append(out, args[i+1])
		case strings.HasPrefix(a, name) && len(a) > len(name):
			out = append(out, strings.TrimPrefix(strings.TrimPrefix(a, name), "="))
		}
	}
	return out
}

// nonOptions are the words among args that are not options.
func nonOptions(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// namesClaudeConfig reports whether a command names the person's Claude Code
// configuration in any way the shell could make a path of: a name that is
// .claude or .claude.json, or a glob that matches one, or the variables that
// move it. Worked out once for each reading's line.
func (m *memo) namesClaudeConfig(line string) bool {
	return m.namesConfig(line, false, true)
}

func namesClaudeConfig(line string) bool {
	return namesConfigAs(line, true, false)
}

// namesConfig is namesConfigAs, worked out once for each reading's line,
// with a cache for each place and each reading of globs: a shared one would
// answer one place with the other's answer.
//
// With globs unset it is what a whole line is asked. A regular expression's
// `.*`, in a sed script or a program, matched .claude as a glob, and `perl
// -pe 's/.*//' notes.txt` was refused for writing the person's configuration.
func (m *memo) namesConfig(line string, agents, globs bool) bool {
	cache := &m.claudeNamed
	switch {
	case agents && globs:
		cache = &m.agentsNamed
	case agents:
		cache = &m.agentsLiteral
	case !globs:
		cache = &m.claudeLiteral
	}
	if *cache == 0 {
		*cache = -1
		if namesConfigAs(line, globs, agents) {
			*cache = 1
		}
	}
	return *cache > 0
}

// namesConfigAs reports whether a line names the person's Claude Code
// configuration, or with agents set their ~/.agents, reading globs as names
// or not.
func namesConfigAs(line string, globs, agents bool) bool {
	lower := strings.ToLower(line)
	if !agents && (strings.Contains(lower, "claude_config_dir") || strings.Contains(lower, "claude_code_plugin_cache_dir")) {
		return true
	}
	if !strings.Contains(lower, ".") {
		return false
	}
	files := []string{".claude", ".claude.json"}
	if agents {
		files = []string{".agents"}
	}
	names := strings.FieldsFunc(lower, func(r rune) bool {
		return strings.ContainsRune(" \t\r\n/\\'\"=;:&|()<>$`{},", r)
	})
	return slices.ContainsFunc(names, func(name string) bool { return namesConfigFile(name, globs, files) })
}

// namesConfigFile reports whether one name, a segment of a path, is one of
// files, or with globs set, a glob that could match one.
func namesConfigFile(name string, globs bool, files []string) bool {
	// Windows drops a trailing dot or space: `.claude.` is .claude there.
	name = strings.TrimRight(name, ". ")
	if name == "" || name[0] != '.' && name[0] != '[' {
		return false
	}
	if slices.Contains(files, name) {
		return true
	}
	if !globs || !strings.ContainsAny(name, "*?[") {
		return false
	}
	for _, file := range files {
		if ok, _ := path.Match(name, file); ok {
			return true
		}
	}
	return false
}

// claudeChangesItsConfig reports a run of Claude Code's own command line that
// changes the configuration, and names the subcommand: settings, MCP servers
// and plugins. What only reads it -- list, get, --help -- is not.
func claudeChangesItsConfig(words []string) (string, bool) {
	if !runsClaudeProgram(words) {
		return "", false
	}
	args := nonOptions(claudeArgs(words))
	if len(args) < 2 || hasWord(words, "--help") || hasWord(words, "-h") {
		return "", false
	}
	sub, verb := strings.ToLower(args[0]), strings.ToLower(args[1])
	changes := false
	switch sub {
	case "config":
		changes = verb == "set" || verb == "add" || verb == "remove" || verb == "rm"
	case "mcp":
		changes = verb == "add" || verb == "add-json" || verb == "add-from-claude-desktop" || verb == "remove"
	case "plugin", "plugins":
		switch verb {
		case "install", "i", "uninstall", "remove", "enable", "disable", "update":
			changes = true
		case "marketplace":
			if len(args) > 2 {
				third := strings.ToLower(args[2])
				changes = third == "add" || third == "remove" || third == "rm" || third == "update"
				verb += " " + third
			}
		}
	}
	return sub + " " + verb, changes
}

// claudeArgs are the words Claude Code is given, after the program and, for a
// package runner, after the package it names.
func claudeArgs(words []string) []string {
	switch base(words[0]) {
	case "npx", "bunx", "pnpx":
		return afterPackage(words[1:])
	case "npm", "bun", "pnpm", "yarn":
		if len(words) > 2 {
			return afterPackage(words[2:])
		}
		return nil
	}
	return words[1:]
}

// afterPackage is a package runner's words after the package it runs.
func afterPackage(args []string) []string {
	for i, w := range args {
		if !strings.HasPrefix(w, "-") {
			return args[i+1:]
		}
	}
	return nil
}

// claudeProgram reports whether a name, folded and without the .exe base
// takes off, is one Claude Code's command line is run by.
func claudeProgram(name string) bool {
	switch name {
	case "claude", "claude.cmd", "claude.ps1":
		return true
	}
	return false
}

// runsClaudeProgram reports whether a command runs Claude Code's command line:
// by its name, by any path, by a glob that matches the name, the native
// installer's binary named by its version, or through a package runner.
func runsClaudeProgram(words []string) bool {
	if len(words) == 0 {
		return false
	}
	name := base(words[0])
	switch {
	case claudeProgram(name):
		return true
	case strings.Contains("/"+strings.ToLower(clean(words[0])), "/claude/versions/"):
		// ~/.local/bin/claude is a link to ~/.local/share/claude/versions/<v>.
		return true
	case strings.ContainsAny(name, "*?["):
		for _, n := range []string{"claude", "claude.cmd", "claude.ps1"} {
			if ok, _ := path.Match(name, n); ok {
				return true
			}
		}
	case name == "npx" || name == "bunx" || name == "pnpx":
		return runsClaude(words[1:])
	case name == "npm" || name == "bun" || name == "pnpm" || name == "yarn":
		// A package runner's own spelling: `npm exec`, `bun x`, `pnpm dlx`.
		// Installing the package starts nothing.
		if len(words) > 1 {
			switch base(words[1]) {
			case "exec", "x", "dlx":
				return runsClaude(words[2:])
			}
		}
	}
	return false
}

// startsClaudeCode reports whether a command starts a Claude Code session of
// its own: its command line run as runsClaudeProgram reads it, PowerShell's
// Start-Process or cmd's start given it, or any word naming its package, its
// cli.js or the Agent SDK, which `node .../cli.js` and a script's import do.
func startsClaudeCode(words []string) bool {
	if runsClaudeProgram(words) {
		return true
	}
	if len(words) == 0 {
		return false
	}
	switch base(words[0]) {
	case "start-process", "saps", "start":
		if slices.ContainsFunc(words[1:], func(w string) bool { return claudeProgram(base(w)) }) {
			return true
		}
	case "npm", "pnpm", "yarn", "bun", "pip", "pip3", "pipx", "uv", "poetry", "brew", "conda":
		// Installing the package starts nothing; runsClaudeProgram has read
		// the spellings of these that run it.
		return false
	}
	return slices.ContainsFunc(words, namesClaudePackage)
}

// namesClaudePackage reports whether a word names Claude Code's package or
// the Agent SDK's, in npm's spelling or Python's.
func namesClaudePackage(word string) bool {
	w := strings.ToLower(word)
	return strings.Contains(w, "@anthropic-ai/claude-code") || strings.Contains(w, "claude-agent-sdk") ||
		strings.Contains(w, "claude_agent_sdk")
}

// personsCommands are the person's decisions, as their slash commands are
// named.
var personsCommands = []string{"approve", "unfreeze", "ack", "remap", "release", "assign"}

// namesAPersonsCommand reports whether text holds one of a person's slash
// commands: a `/` that starts a word, any plugin's name and a colon or none,
// and the command's name, whole, in any case. `/sdlc:approve`, `/approve` and
// `/SDLC:Approve` are all expanded as a person's approval.
func namesAPersonsCommand(text string) bool {
	lower := strings.ToLower(text)
	for i := strings.IndexByte(lower, '/'); i >= 0; {
		if personsCommandAt(lower, i) {
			return true
		}
		next := strings.IndexByte(lower[i+1:], '/')
		if next < 0 {
			break
		}
		i += next + 1
	}
	return false
}

// personsCommandAt reports whether the `/` at i in lower text starts one of a
// person's slash commands.
func personsCommandAt(lower string, i int) bool {
	if i > 0 && strings.IndexByte(" \t\r\n'\"`=(;&|<>{,", lower[i-1]) < 0 {
		return false
	}
	name := lower[i+1:]
	if colon := strings.IndexAny(name, ": \t\r\n/'\""); colon >= 0 && name[colon] == ':' {
		name = name[colon+1:]
	}
	for _, c := range personsCommands {
		if rest, ok := strings.CutPrefix(name, c); ok && (rest == "" || !wordByte(rest[0])) {
			return true
		}
	}
	return false
}

// wordByte is a byte that carries a command's name on: `/approve-all` is
// another command, and erring that way refuses it too, which is the safe side.
func wordByte(b byte) bool {
	return 'a' <= b && b <= 'z' || '0' <= b && b <= '9' || b == '_'
}

// terminalGivers are the programs that run a command in a terminal of its
// own, which something can type into after the command has started.
var terminalGivers = map[string]bool{
	"tmux": true, "screen": true, "zellij": true, "expect": true, "unbuffer": true, "script": true,
}

// checkSecondSession refuses a command that starts a Claude Code session that
// would expand one of a person's slash commands: a session started from here
// that reads /sdlc:approve as its prompt makes the decision as if a person had
// typed it. It is the reading for a session that holds no story, which
// HumanDecisions asks, and for the session holding one, Inspect asks it last,
// for the spellings checkNestedSession does not read as Claude Code.
//
// The prompt is read where it can be. A session whose prompt cannot be read --
// held in a variable, substituted, fed on standard input or by xargs, or
// typed later into a terminal it was given -- is refused as one that could;
// one with a literal prompt that names no person's command, `claude
// --version` and `claude mcp list` are not.
func checkSecondSession(command string, powerShell bool) (Finding, bool) {
	return checkSecondSessionRead(command, powerShell, readings)
}

// checkSecondSessionRead is checkSecondSession with the readings of the
// command made by read.
func checkSecondSessionRead(command string, powerShell bool, read func(string, bool) []pair) (Finding, bool) {
	lower := strings.ToLower(command)
	if !strings.ContainsAny(command, "$%`*?[") && !mayNameClaude(lower) {
		return Finding{}, false
	}
	runs, named := everyRun(read(command, powerShell), powerShell)
	// What can type into a session later: `tmux new 'claude'`, then send-keys.
	given := slices.ContainsFunc(wordsIn(lower), func(w string) bool { return terminalGivers[base(w)] })
	for _, run := range runs {
		if len(run.words) == 0 || run.extra {
			continue
		}
		if expandsAPersonsCommand(run, named, given, command) {
			return secondSessionFinding, true
		}
	}
	// `ln -s "$(which claude)" /tmp/x && /tmp/x -p "/sdlc:approve"` starts it
	// under a name no reading knows, in the same call. The `command -v` form
	// never gets here: the loop above reads `command -v claude` as a run of
	// claude, and refuses it there.
	if named && slices.ContainsFunc(runs, copiesAProgram) &&
		(mentionsClaude(command) || slices.ContainsFunc(runs, func(r ran) bool { return mentionsClaude(strings.Join(r.words, " ")) })) {
		return copiedSessionFinding, true
	}
	return Finding{}, false
}

// mayNameClaude reports whether a command, folded, could name claude once the
// shell has taken its quotes and backslashes out and expanded its brace
// lists. Asked of the raw text alone, `cl""aude -p '/sdlc:approve'`, `c\laude`
// and `cl{a,}ude` held no claude, and a second session started each with
// nothing read.
func mayNameClaude(lower string) bool {
	if strings.Contains(lower, "claude") {
		return true
	}
	bare := unescaped.Replace(lower)
	if strings.Contains(bare, "claude") {
		return true
	}
	return strings.Contains(lower, "{") && strings.Contains(unescaped.Replace(bracesExpanded(lower)), "claude")
}

// unescaped takes out what the shell removes from a word once it has read it.
var unescaped = strings.NewReplacer(`'`, "", `"`, "", `\`, "")

// copiesAProgram reports whether a command copies, moves or links files, as
// the configuration's own rule reads a copy: what it makes can be run under a
// name of its own.
func copiesAProgram(r ran) bool {
	if len(r.words) == 0 {
		return false
	}
	switch base(r.words[0]) {
	case "cp", "copy", "copy-item", "cpi", "mv", "move", "move-item", "mi", "rsync", "scp", "install", "ln":
		return true
	case "new-item", "ni":
		return slices.ContainsFunc(r.words[1:], func(w string) bool {
			w = strings.ToLower(w)
			return strings.Contains(w, "symboliclink") || strings.Contains(w, "hardlink")
		})
	}
	return false
}

// everyRun is every command each of a line's readings runs, and whether a
// person's slash command is named anywhere in it, in a reading or in the words
// of a command one runs.
func everyRun(read []pair, powerShell bool) (all []ran, named bool) {
	for _, r := range read {
		runs, _ := programsAt(r.text, powerShell)
		all = append(all, runs...)
		named = named || namesAPersonsCommand(r.text)
		for _, run := range runs {
			named = named || namesAPersonsCommand(strings.Join(run.words, " "))
		}
	}
	return all, named
}

// expandsAPersonsCommand reports whether one command starts a Claude Code
// session that would, or could, expand a person's command. named is whether
// the command names one anywhere, and given whether it runs something that
// gives a program a terminal of its own.
func expandsAPersonsCommand(r ran, named, given bool, command string) bool {
	words := r.words
	starts := startsClaudeCode(words)
	// A program the shell has yet to work out could be claude: `c=claude; $c
	// -p ...`, `"$(command -v claude)" -p ...`.
	program := words[0]
	if len(r.marked) > 0 {
		program = r.marked[0]
	}
	unsure := program == "" || strings.ContainsAny(program, "$%`")
	switch {
	case starts && named, unsure && named:
		return true
	case starts:
		return r.substituted || r.piped || r.input || given || !readablePrompt(words[1:], r.marked)
	case terminalGivers[base(words[0])]:
		// `tmux new-session -d 'claude'`: the session is given a terminal
		// something can type into later.
		return mentionsClaude(strings.Join(words, " "))
	case runsInlineCode(words, "") || programOnStdin(words, r.input):
		// Inline code that starts it, or the SDK, whose prompt is not read:
		// in its words, or, for a program read from standard input, anywhere
		// in the command. A bare `python3` fed on standard input reads its
		// program there as surely as `python3 -` does.
		return mentionsClaude(strings.Join(words, " ")) ||
			r.input && (hasWord(words[1:], "-") || programOnStdin(words, true)) && mentionsClaude(command)
	}
	return false
}

// mentionsClaude reports whether text names Claude Code as a program or a
// package: `"claude"` in a list of arguments does, `.claude` does not.
func mentionsClaude(text string) bool {
	for _, w := range wordsIn(strings.ToLower(text)) {
		if claudeProgram(base(w)) || namesClaudePackage(w) {
			return true
		}
	}
	return false
}

// readablePrompt reports whether each of a session's words is one the rules
// read as written, with nothing the shell has yet to expand.
func readablePrompt(args, marked []string) bool {
	for _, w := range args {
		if strings.ContainsAny(w, "$%`") {
			return false
		}
	}
	for _, w := range marked {
		if strings.Contains(w, "$SUB") {
			return false
		}
	}
	return true
}

// copiedSessionFinding is the refusal of a line that copies or links a program
// and names both Claude Code and a person's command. Its reason is its own:
// the line starts no session under a name the hook knows, and a reason that
// said it did read as a false refusal worth a retry.
var copiedSessionFinding = Finding{
	Rule: secondSessionFinding.Rule,
	Reason: "this line copies or links a program and names both Claude Code and a person's slash command, " +
		"such as /sdlc:approve, so it could start a session under another name that expands it, " +
		"and a decision typed by a model is not a person's",
	Route: secondSessionFinding.Route,
}

// secondSessionFinding is the refusal of a session started from here that
// would expand a person's command. It keeps the name of the rule that refuses
// every nested session in the session holding a story.
var secondSessionFinding = Finding{
	Rule: "story-stays-in-its-session",
	Reason: "a Claude Code session started from here could expand a person's slash command, such as " +
		"/sdlc:approve, and a decision typed by a model is not a person's",
	Route: "say what you wanted that session to do and stop; a person types their own commands, " +
		"in Claude Code or in their own terminal",
}

// secondSession is checkSecondSession, whose refusal is read from the command
// as it arrived.
func secondSession(command string, powerShell bool, read func(string, bool) []pair) (Finding, bool) {
	f, ok := checkSecondSessionRead(command, powerShell, read)
	if ok {
		f.Via = viaReleased
	}
	return f, ok
}
