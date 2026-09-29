package shellpolicy

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// git runs more than its words show. An alias given with -c, set in git's
// configuration variables or in the environment the session started with,
// runs another subcommand, or a command line of its own; a subcommand the
// shell has yet to work out could be any of them; and GIT_DIR, GIT_WORK_TREE,
// GIT_INDEX_FILE and core.worktree point it at another checkout from anywhere
// earlier in the command. `git -c alias.gone='worktree remove --force' gone .`
// was a command called gone, and `export GIT_WORK_TREE=/elsewhere; git
// commit` a commit in this checkout. What follows reads a git command as git
// will run it, as far as the command says, and refuses one it cannot read.

// maxGitDepth is how deep aliases, and the command lines they hand the shell,
// are read, as programsWithin reads scripts.
const maxGitDepth = 4

// maxAliasText bounds the command lines `!` aliases hand the shell that one
// reading of a command reads, counted each time one runs. Each is read
// wherever it runs, so an alias that runs another k times, four deep, was read
// k to the fourth times: at 50 a level, 1.6 KB took 30 s, past what Claude
// Code gives the hook, and the command ran unchecked. Counted in lines, two
// levels of 4,000 still took 4.5 s: the time goes on the text. Going over is
// refused, as a glob that could reach too many places is.
const maxAliasText = 64 << 10

// environment is the variables each command of a line runs with, as far as the
// line and the environment the session started with say. Names are compared
// in upper case: Windows' are not case-sensitive, and nothing is lost by
// reading POSIX's the same way.
type environment struct {
	t *subshells
	// sets is what each subshell has set so far, by name.
	sets map[int]map[string]string
	// outer is where a name the line does not set is looked up. here is
	// whether the command line set it, rather than the session's environment.
	outer func(name string) (value string, here, ok bool)
	// handed is what a `!` alias's command line is handed by the git command
	// running it: its settings, which git passes on in GIT_CONFIG_PARAMETERS.
	handed []setting
}

// unset marks a variable `unset` took away.
const unset = "\x00"

// unknownValue is a value the command gives that the hook cannot work out,
// such as what read takes from standard input. Like a variable the shell has
// yet to expand, it names no place that can be found.
const unknownValue = "$?"

func newEnvironment(text string, s State, t *subshells) *environment {
	// PowerShell sets a variable with a statement the lexer does not read as an
	// assignment. Each is read from the whole command, and counts for every
	// command in it.
	global := map[string]string{}
	if s.PowerShell {
		for _, m := range powerShellSets.FindAllStringSubmatch(text, -1) {
			name := strings.ToUpper(m[1] + m[2] + m[3] + m[4])
			global[name] = strings.Trim(m[5], `'"`)
		}
	}
	carried := s.Carried
	return &environment{t: t, sets: map[int]map[string]string{}, outer: func(name string) (string, bool, bool) {
		if v, ok := global[name]; ok {
			return v, true, true
		}
		if carried != nil {
			if v := carried(name); v != "" {
				return v, false, true
			}
		}
		return "", false, false
	}}
}

// powerShellSets matches PowerShell's ways of setting a variable for the
// commands after it: `$env:NAME = value` and `${env:NAME} = value`, or `+=`,
// Set-Item, New-Item, Set-Content and Add-Content on the env: drive, with or
// without -Path and -Value, and .NET's
// `[Environment]::SetEnvironmentVariable('NAME', value)`, with or without a
// third argument. A quoted value is one word, and anything else runs to a
// space or the end of the statement. Add-Content and `+=` are read as setting
// what they add, which is what they do to a variable nothing set before them.
// The same cmdlets with their parameters in another order are read from their
// words (powerShellItemSets).
var powerShellSets = regexp.MustCompile(`(?i)(?:\$env:([a-z_][a-z0-9_]*)\s*\+?=\s*|` +
	`\$\{env:([a-z_][a-z0-9_]*)\}\s*\+?=\s*|` +
	`\b(?:set-item|new-item|set-content|add-content|si|ni|sc|ac)\s+` +
	`(?:-path\s+)?env:[\\/]?([a-z_][a-z0-9_]*)\s+(?:-value\s+)?|` +
	`setenvironmentvariable\(\s*['"]([a-z_][a-z0-9_]*)['"]\s*,\s*)` +
	`('[^']*'|"[^"]*"|[^\s;),|]+)`)

// within is the environment of a command line an alias hands the shell, which
// starts with what the git command handing it over runs with, and is handed
// that command's settings. Read without them, `git b` inside
// `git -c alias.a='!git b' -c alias.b='worktree prune' a` was a subcommand
// called b, and the prune met no rule.
func (e *environment) within(r ran, t *subshells, handed []setting) *environment {
	return &environment{t: t, sets: map[int]map[string]string{}, outer: func(name string) (string, bool, bool) {
		return e.get(r, name)
	}, handed: handed}
}

// record keeps what r sets, or unsets, for the commands after it in its
// subshell.
func (e *environment) record(r ran) {
	keep := func(name, value string) {
		m := e.sets[r.group]
		if m == nil {
			m = map[string]string{}
			e.sets[r.group] = m
		}
		m[strings.ToUpper(name)] = value
	}
	for _, a := range r.sets {
		name, value, _ := strings.Cut(a, "=")
		keep(name, value)
	}
	if len(r.words) > 1 && base(r.words[0]) == "unset" {
		for _, name := range r.words[1:] {
			if !strings.HasPrefix(name, "-") {
				keep(name, unset)
			}
		}
	}
}

// get is a variable as r runs with it, whether the command line set it, and
// whether it is set at all.
func (e *environment) get(r ran, name string) (value string, here, ok bool) {
	name = strings.ToUpper(name)
	if v, ok := assigned(r.env, name); ok {
		return v, true, true
	}
	for g := r.group; ; g = e.t.parent[g] {
		if v, ok := e.sets[g][name]; ok {
			if v == unset {
				return "", false, false
			}
			return v, true, true
		}
		if v, ok := assigned(e.t.env[g], name); ok {
			return v, true, true
		}
		if g == 0 {
			break
		}
	}
	return e.outer(name)
}

// assigned is the last value assigns, NAME=value, gives name.
func assigned(assigns []string, name string) (string, bool) {
	for i := len(assigns) - 1; i >= 0; i-- {
		if n, v, _ := strings.Cut(assigns[i], "="); strings.ToUpper(n) == name {
			return v, true
		}
	}
	return "", false
}

// setting is one of git's configuration settings a command runs with.
type setting struct {
	key, value string
	// here is whether the command line gives it, rather than the environment
	// the session started with, which a person set before it did.
	here bool
	// known is whether its value can be read: not where the shell has yet to
	// expand it, or --config-env names a variable nothing sets.
	known bool
}

// settings are what git's configuration variables give r, and the part of
// them that cannot be read, where the command line set one that way. The
// session's own are read for their aliases, and never refused: an agent
// cannot spell out what a person's environment set.
func (e *environment) settings(r ran) (out []setting, unread string) {
	if v, here, ok := e.get(r, "GIT_CONFIG_PARAMETERS"); ok {
		pairs, parsed := configParameters(v)
		switch {
		case here && (unexpanded(v) || !parsed):
			unread = "GIT_CONFIG_PARAMETERS, whose value the shell has yet to expand or git would not read"
		case parsed:
			for _, p := range pairs {
				p.here = here
				out = append(out, p)
			}
		}
	}
	count, countHere, ok := e.get(r, "GIT_CONFIG_COUNT")
	if !ok {
		return out, unread
	}
	n, err := strconv.Atoi(strings.TrimSpace(count))
	switch {
	case err == nil:
	case unexpanded(count):
		// Every key there is, read as git reads them: from 0 to the first
		// one missing.
		n = -1
	default:
		// git refuses a count that is not a number, and runs nothing.
		return out, unread
	}
	for i := 0; n < 0 || i < n; i++ {
		key, keyHere, ok := e.get(r, "GIT_CONFIG_KEY_"+strconv.Itoa(i))
		if !ok {
			break
		}
		value, valueHere, _ := e.get(r, "GIT_CONFIG_VALUE_"+strconv.Itoa(i))
		out = append(out, setting{key: key, value: value, here: countHere || keyHere || valueHere, known: !unexpanded(value)})
	}
	return out, unread
}

// configFiles is a settings file the command points git at, which the hook
// does not read, or "" where it points it at none. /dev/null hides nothing.
func configFiles(r ran, e *environment) string {
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG"} {
		v, here, ok := e.get(r, name)
		if !ok || !here {
			continue
		}
		switch strings.ToLower(clean(v)) {
		case "", "/dev/null", "nul":
			continue
		}
		return name + ", which points git at a settings file the hook does not read"
	}
	return ""
}

// configParameters reads GIT_CONFIG_PARAMETERS as git writes it: each setting
// quoted as the shell would quote it, `'key'='value'`, or `'key=value'` as git
// wrote it before 2.31, separated by spaces.
func configParameters(v string) ([]setting, bool) {
	var out []setting
	for i := 0; ; {
		for i < len(v) && strings.IndexByte(" \t\r\n", v[i]) >= 0 {
			i++
		}
		if i == len(v) {
			return out, true
		}
		key, n, ok := sqDequote(v[i:])
		if !ok {
			return nil, false
		}
		i += n
		var value string
		if i < len(v) && v[i] == '=' {
			if value, n, ok = sqDequote(v[i+1:]); !ok {
				return nil, false
			}
			i += 1 + n
		} else {
			key, value, _ = strings.Cut(key, "=")
		}
		out = append(out, setting{key: key, value: value, known: !unexpanded(value)})
	}
}

// sqDequote reads one string quoted as git's sq_quote quotes it from the front
// of s, where a quote or a ! inside closes the quotes, is escaped with a
// backslash and opens them again, and returns it and how much of s it took.
func sqDequote(s string) (string, int, bool) {
	if !strings.HasPrefix(s, "'") {
		return "", 0, false
	}
	var b strings.Builder
	for i := 1; ; {
		j := strings.IndexByte(s[i:], '\'')
		if j < 0 {
			return "", 0, false
		}
		b.WriteString(s[i : i+j])
		i += j + 1
		if i+2 < len(s) && s[i] == '\\' && s[i+2] == '\'' {
			b.WriteByte(s[i+1])
			i += 3
			continue
		}
		return b.String(), i, true
	}
}

// gitRead is a git command as git will run it.
type gitRead struct {
	// args are git's words after the program, with each alias it runs put in
	// place of its name.
	args []string
	sub  string
	at   int // where sub is in args, or len(args) where there is none
	// script is the command line a `!` alias hands the shell, with the words
	// after the alias's name.
	script string
	// settings are those git runs with: its configuration variables', then its
	// -c and --config-env, in the order it reads them.
	settings []setting
	// unread is the part that cannot be read, or "".
	unread  string
	aliased bool
}

// gitReadings are git's words read as git runs them, once for each word a
// brace list in the subcommand makes: `git {worktree,} prune` is `git worktree
// prune`, and bash drops the empty word. undecoded is a reading another
// decodes, which is left a subcommand the shell has yet to expand (see pair).
func gitReadings(args []string, env []setting, lookup func(string) (string, bool, bool), undecoded bool) []gitRead {
	_, at := gitOptions(args, env, lookup)
	if at == len(args) || !strings.Contains(args[at], "{") {
		return []gitRead{readGit(args, env, lookup, undecoded)}
	}
	words := braces(args[at])
	for _, w := range words {
		if open, _, _ := braceGroup(w); open >= 0 {
			return []gitRead{{args: args, at: at, sub: args[at],
				unread: "the subcommand " + shown(args[at]) + ", a brace list longer than the rules read"}}
		}
	}
	var out []gitRead
	for _, w := range words {
		variant := slices.Concat(args[:at], args[at+1:])
		if w != "" {
			variant = slices.Concat(args[:at], []string{w}, args[at+1:])
		}
		out = append(out, readGit(variant, env, lookup, undecoded))
	}
	return out
}

// readGit reads git's words, putting each alias git runs in place of its name
// until the subcommand is one of git's own.
func readGit(args []string, env []setting, lookup func(string) (string, bool, bool), undecoded bool) gitRead {
	g := gitRead{args: args}
	seen := map[string]bool{}
	for {
		g.settings, g.at = gitOptions(g.args, env, lookup)
		if g.at == len(g.args) {
			return g
		}
		g.sub = g.args[g.at]
		if unexpanded(g.sub) {
			if !undecoded {
				g.unread = "the subcommand " + shown(g.sub) + ", which the shell has yet to expand"
			}
			return g
		}
		name := strings.ToLower(g.sub)
		value, known, found := g.alias(name)
		switch {
		case !found:
			return g
		case seen[name]:
			g.unread = "the alias " + shown(g.sub) + ", which leads back to itself"
			return g
		case !known:
			g.unread = "the alias " + shown(g.sub) + ", whose value the shell has yet to expand"
			return g
		case len(seen) == maxGitDepth:
			g.unread = "the alias " + shown(g.sub) + ", one of aliases nested deeper than the rules read"
			return g
		}
		seen[name] = true
		g.aliased = true
		if script, ok := strings.CutPrefix(value, "!"); ok {
			g.script = strings.TrimSpace(script + " " + shellQuoted(g.args[g.at+1:]))
			g.sub = ""
			return g
		}
		g.args = slices.Concat(g.args[:g.at], splitAlias(value), g.args[g.at+1:])
	}
}

// alias is the value git gives the alias name: the last one among the
// settings.
func (g gitRead) alias(name string) (value string, known, found bool) {
	for i := len(g.settings) - 1; i >= 0; i-- {
		s := g.settings[i]
		if k, ok := strings.CutPrefix(strings.ToLower(s.key), "alias."); ok && k == name {
			return s.value, s.known, true
		}
	}
	return "", false, false
}

// gitOptions reads git's own options, in front of its subcommand: the settings
// they give it after env's, and where the subcommand is.
func gitOptions(args []string, env []setting, lookup func(string) (string, bool, bool)) ([]setting, int) {
	settings := append([]setting{}, env...)
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c" && i+1 < len(args):
			i++
			key, value, _ := strings.Cut(args[i], "=")
			settings = append(settings, setting{key: key, value: value, here: true, known: !unexpanded(value)})
		case a == "--config-env" && i+1 < len(args):
			i++
			settings = append(settings, configEnv(args[i], lookup))
		case strings.HasPrefix(a, "--config-env="):
			settings = append(settings, configEnv(strings.TrimPrefix(a, "--config-env="), lookup))
		case a == "-C" || a == "--git-dir" || a == "--work-tree" || a == "--namespace" || a == "--attr-source":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return settings, i
		}
	}
	return settings, len(args)
}

// configEnv is the setting `--config-env=<name>=<variable>` gives git: the
// variable's value, which is unknown where nothing the hook can see sets it.
func configEnv(spec string, lookup func(string) (string, bool, bool)) setting {
	at := strings.LastIndexByte(spec, '=')
	if at < 0 {
		return setting{key: spec, here: true, known: true}
	}
	key, name := spec[:at], spec[at+1:]
	if unexpanded(name) {
		return setting{key: key, here: true}
	}
	value, _, ok := lookup(name)
	return setting{key: key, value: value, here: true, known: ok && !unexpanded(value)}
}

// splitAlias is an alias's value as git splits it into words: at white space
// outside quotes, with the quotes taken out and a backslash escaping the
// character after it, except inside single quotes.
func splitAlias(value string) []string {
	var out []string
	var b strings.Builder
	var quote byte
	started := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case quote == 0 && strings.IndexByte(" \t\r\n", c) >= 0:
			if started {
				out = append(out, b.String())
				b.Reset()
				started = false
			}
		case quote == 0 && (c == '\'' || c == '"'):
			quote, started = c, true
		case c == quote:
			quote = 0
		case c == '\\' && quote != '\'' && i+1 < len(value):
			i++
			b.WriteByte(value[i])
			started = true
		default:
			b.WriteByte(c)
			started = true
		}
	}
	if started {
		out = append(out, b.String())
	}
	return out
}

// shellQuoted is words written for a shell to read back as the same words.
func shellQuoted(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		if w != "" && strings.Trim(w, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@%") == "" {
			quoted[i] = w
			continue
		}
		quoted[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// unexpanded reports whether a word, as the lexer marked it, holds something
// the shell has yet to work out: a variable, or a substitution, which it marks
// $SUB.
func unexpanded(word string) bool {
	return strings.ContainsAny(word, "$`")
}

// unexpandedProgram reports whether the part of a program word that names the
// program is one the shell has yet to work out: `$G`, `${GIT}`, `"$(command -v
// git)"`. That could be git, and is read as git. `"$VENV/bin/python"` ends in
// a name of its own, and is python.
func unexpandedProgram(word string) bool {
	name := clean(word)
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return unexpanded(name)
}

// shown is a word as a refusal quotes it, with the lexer's mark for a
// substitution written as the shell's.
func shown(word string) string {
	return "`" + strings.ReplaceAll(word, "$SUB", "$(...)") + "`"
}

// gitRules applies the rules that turn on how git will run a command to one
// line's commands, in the order they run.
type gitRules struct {
	s State
	m *memo
	// staged is whether a command earlier in the line wrote the index from
	// another checkout's work tree, or into another checkout's index.
	staged bool
	// aliasText is how much command line aliases have handed the shell so far.
	aliasText int
}

// gitCommit is what the commit rules need of a git command read as git runs
// it.
type gitCommit struct {
	// words are git's words with the aliases it runs expanded, where that finds
	// a commit the command's own words do not show, or where the program was
	// read as git; nil otherwise.
	words []string
	// asGit is whether the program is a word the shell has yet to expand.
	asGit bool
	// away is whether git is pointed at another checkout, or somewhere that
	// cannot be placed, by something other than its words: its variables,
	// core.worktree, or an earlier command that staged another checkout's
	// files.
	away bool
}

// indexWriters are the git subcommands that write the index.
var indexWriters = map[string]bool{
	"add": true, "stage": true, "rm": true, "mv": true, "update-index": true, "read-tree": true,
	"apply": true, "restore": true, "reset": true, "checkout": true,
}

// check applies the git rules to r, run from dir with e. An alias being
// defined is judged as it would run: the commit gate is left to the command
// that runs it, and only another checkout's commit is refused.
func (j *gitRules) check(r ran, dir string, e *environment, depth int, defining bool) (gitCommit, Finding, bool) {
	words := r.marked
	if len(words) == 0 {
		words = r.words
	}
	if len(words) == 0 {
		return gitCommit{}, Finding{}, false
	}
	asGit := !isGit(words) && unexpandedProgram(words[0]) && !r.expression && !j.s.undecoded
	if !isGit(words) && !asGit {
		return gitCommit{}, Finding{}, false
	}
	env, unread := e.settings(r)
	env = append(env, e.handed...)
	if unread == "" {
		unread = configFiles(r, e)
	}
	if unread != "" && !asGit {
		return gitCommit{}, unreadable(unread), true
	}
	lookup := func(name string) (string, bool, bool) { return e.get(r, name) }
	var first gitCommit
	for n, g := range gitReadings(words[1:], env, lookup, j.s.undecoded) {
		c, f, ok := j.judge(g, r, dir, e, words[0], asGit, depth, defining)
		if ok {
			return c, f, true
		}
		if n == 0 {
			first = c
		}
	}
	return first, Finding{}, false
}

// judge applies the git rules to one reading of a git command.
func (j *gitRules) judge(g gitRead, r ran, dir string, e *environment, program string, asGit bool, depth int, defining bool) (gitCommit, Finding, bool) {
	c := gitCommit{asGit: asGit}
	if g.unread != "" {
		if asGit && g.at < len(g.args) && unexpanded(g.args[g.at]) {
			return c, Finding{
				Rule: "git-command-cannot-be-read",
				Reason: shown(program) + " is not expanded yet, so the hook reads it as git, and " +
					shown(g.args[g.at]) + " would then be git's subcommand",
				Route: "name the program itself, such as `python3`, rather than a variable",
			}, true
		}
		return c, unreadable(g.unread), true
	}
	if !asGit {
		if part := unreadSetting(g.settings); part != "" {
			return c, unreadable(part), true
		}
	}
	here := placeDir(r, dir, g)
	switch g.sub {
	case "worktree":
		if action, reads := worktreeAction(g.args[g.at+1:]); !reads {
			return c, worktreeFinding(action), true
		}
	case "config":
		if f, ok := j.config(g, r, here, e, depth); ok {
			return c, f, true
		}
	}
	if g.script != "" {
		if f, ok := j.script(g.script, r, here, e, g.settings, depth, defining); ok {
			return c, f, true
		}
	}
	places := pointedAt(g, r, e)
	if !defining && indexWriters[g.sub] && j.elsewhere(places, here, "ti") {
		j.staged = true
	}
	c.away = j.staged || j.elsewhere(places, here, "dti")
	expanded := append([]string{"git"}, g.args...)
	switch {
	case g.sub == "":
	case asGit:
		c.words = expanded
	case g.aliased && len(r.words) > 1:
		if sub, _, _, _ := gitCall(r.words[1:]); !makesACommit(r.words, sub) && makesACommit(expanded, g.sub) {
			c.words = expanded
		}
	}
	return c, Finding{}, false
}

// commit applies the commit rules to a command, read as j.check read it: the
// commit gate and another checkout's, or, for an alias defined now, another
// checkout's alone.
func (j *gitRules) commit(r ran, dir string, c gitCommit, placed, defining bool) (Finding, bool) {
	words := r.words
	if c.words != nil {
		words, placed = c.words, placed && !c.asGit
	}
	if !isGit(words) {
		return Finding{}, false
	}
	if defining {
		sub, _, _, _ := gitCall(words[1:])
		if !makesACommit(words, sub) {
			return Finding{}, false
		}
		return commitElsewhere(words, dir, j.s, c.away)
	}
	return checkCommit(words, dir, placed, j.s, c.away)
}

// script applies the git rules, and the commit rules, to a command line an
// alias hands the shell, with the settings of the git command running it. git
// hands it to sh, on Windows as well.
func (j *gitRules) script(line string, r ran, dir string, e *environment, handed []setting, depth int,
	defining bool) (Finding, bool) {
	if depth >= maxGitDepth {
		return unreadable("an alias's command line, nested deeper than the rules read"), true
	}
	if j.aliasText += len(line) + 1; j.aliasText > maxAliasText {
		// The aliases are spelled out already, so the route is not unreadable's.
		f := unreadable("aliases that hand the shell more command lines than the rules read")
		f.Route = "run what the aliases would run as commands of their own, a few at a time, rather than " +
			"through aliases that run one another"
		return f, true
	}
	runs, t := programsAt(line, false)
	if t.tooDeep {
		return tooDeep, true
	}
	inner := e.within(r, t, handed)
	for _, sr := range runs {
		inner.record(sr)
		c, f, ok := j.check(sr, dir, inner, depth+1, defining)
		if !sr.extra && len(sr.words) > 0 {
			// Not written down as a commit: the command that runs the alias is
			// the one the recorder reads.
			if f, ok := j.commit(sr, dir, c, false, defining); ok {
				return f, true
			}
		}
		if ok {
			return f, true
		}
	}
	return Finding{}, false
}

// config applies the git rules to what `git config` writes: core.worktree and
// core.bare, an include, and an alias, judged as it would run.
func (j *gitRules) config(g gitRead, r ran, dir string, e *environment, depth int) (Finding, bool) {
	for _, change := range configChanges(g.args[g.at+1:]) {
		key := strings.ToLower(change.key)
		switch {
		case unexpanded(change.key):
			if j.s.undecoded {
				continue
			}
			return unreadable("the setting " + shown(change.key) + ", whose name the shell has yet to expand"), true
		case key == "core.worktree", key == "core.bare", change.section && key == "core":
			return Finding{
				Rule: "worktree-setting-through-the-tool",
				Reason: "changing " + shown(change.key) + " changes which directory a checkout of this " +
					"repository works in, and it can move the main checkout's work tree",
				Route: "a person changes core.worktree and core.bare in their own terminal; if git says " +
					"this is not a work tree, stop and tell them",
			}, true
		case !change.valued:
		case isInclude(key, change.section):
			return unreadable(shown(change.key) + ", an include, which reads settings from a file the hook does not read"), true
		case !change.section && strings.HasPrefix(key, "alias."):
			if unexpanded(change.value) {
				if j.s.undecoded {
					continue
				}
				return unreadable("the alias " + shown(change.key) + ", whose value the shell has yet to expand"), true
			}
			if f, ok := j.alias(change.value, r, dir, e, depth); ok {
				return f, true
			}
		}
	}
	return Finding{}, false
}

// alias judges an alias being defined as it would run: `git config alias.gone
// 'worktree prune'` is refused as `git worktree prune` is.
func (j *gitRules) alias(value string, r ran, dir string, e *environment, depth int) (Finding, bool) {
	if depth >= maxGitDepth {
		return unreadable("an alias nested deeper than the rules read"), true
	}
	if script, ok := strings.CutPrefix(value, "!"); ok {
		return j.script(script, r, dir, e, nil, depth+1, true)
	}
	words := append([]string{"git"}, splitAlias(value)...)
	run := ran{words: words, marked: words, group: r.group, env: r.env}
	c, f, ok := j.check(run, dir, e, depth+1, true)
	if ok {
		return f, true
	}
	return j.commit(run, dir, c, false, true)
}

// isInclude reports whether a setting, or with section a section, is an
// include: `include.path`, or `includeIf.<condition>.path`.
func isInclude(key string, section bool) bool {
	if section {
		return key == "include" || strings.HasPrefix(key, "includeif")
	}
	return strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.")
}

// unreadSetting is a setting the command line gives git that cannot be read,
// or "": one whose name the shell has yet to expand, an include, or an alias
// whose value cannot be read, which is refused where it is given whether or
// not it is run.
func unreadSetting(settings []setting) string {
	for _, s := range settings {
		if !s.here {
			continue
		}
		key := strings.ToLower(s.key)
		switch {
		case unexpanded(s.key):
			return "the setting " + shown(s.key) + ", whose name the shell has yet to expand"
		case isInclude(key, false):
			return shown(s.key) + ", an include, which reads settings from a file the hook does not read"
		case strings.HasPrefix(key, "alias.") && !s.known:
			return "the alias " + shown(s.key) + ", whose value cannot be read"
		}
	}
	return ""
}

// unreadable is the refusal of a git command the hook cannot read.
func unreadable(part string) Finding {
	return Finding{
		Rule:   "git-command-cannot-be-read",
		Reason: "git would run something the hook cannot read: " + part,
		Route:  "spell git's subcommand and any alias out in the command itself, with no include or configuration file",
	}
}

// worktreeAction is what `git worktree` is asked to do, and whether that only
// reads: `list`, or asking for help. With no action git prints its usage.
func worktreeAction(rest []string) (string, bool) {
	for _, a := range rest {
		switch {
		case a == "-h" || a == "--help":
			return a, true
		case strings.HasPrefix(a, "-"):
		default:
			return a, a == "list"
		}
	}
	return "", true
}

func worktreeFinding(action string) Finding {
	return Finding{
		Rule: "worktree-through-the-tool",
		Reason: "`git worktree " + strings.ReplaceAll(action, "$SUB", "$(...)") + "` makes, moves or takes away a " +
			"checkout of this repository, and each story is held in the checkout git lists for it",
		Route: "`sdlc start` makes a story's worktree; a person adds, removes, prunes or repairs one in their " +
			"own terminal; to read another branch, use `git show <branch>:<path>` or `git diff <branch>`",
	}
}

// configChange is one change `git config` makes.
type configChange struct {
	key, value string
	// valued is whether it gives the setting a value, rather than taking one
	// away.
	valued bool
	// section is whether key is a section, removed or renamed.
	section bool
}

// configChanges are the settings a `git config` command writes or takes away,
// and the sections it removes or renames, in the syntax before git 2.46 and
// after. A read changes nothing.
func configChanges(args []string) []configChange {
	var words []string
	action := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, glued := strings.Cut(a, "=")
		switch {
		case a == "--":
			words = append(words, args[i+1:]...)
			i = len(args)
		case configValued[name]:
			if !glued {
				i++
			}
		case configActions[name]:
			action = name
		case strings.HasPrefix(a, "-"):
		default:
			words = append(words, a)
		}
	}
	if action == "" && len(words) > 0 && configSubcommands[words[0]] {
		action, words = words[0], words[1:]
	}
	switch action {
	case "", "--add", "--replace-all", "set":
		if len(words) >= 2 {
			return []configChange{{key: words[0], value: words[1], valued: true}}
		}
	case "--unset", "--unset-all", "unset":
		if len(words) >= 1 {
			return []configChange{{key: words[0]}}
		}
	case "--remove-section", "remove-section":
		if len(words) >= 1 {
			return []configChange{{key: words[0], section: true}}
		}
	case "--rename-section", "rename-section":
		if len(words) >= 2 {
			return []configChange{{key: words[0], section: true}, {key: words[1], section: true, valued: true}}
		}
	}
	return nil
}

// configValued are git config's options that take a value.
var configValued = map[string]bool{
	"-f": true, "--file": true, "--blob": true, "--type": true, "--default": true, "--comment": true,
	"--value": true, "--url": true,
}

// configActions are git config's actions spelled as options.
var configActions = map[string]bool{
	"--get": true, "--get-all": true, "--get-regexp": true, "--get-urlmatch": true, "--get-color": true,
	"--get-colorbool": true, "-l": true, "--list": true, "-e": true, "--edit": true, "--add": true,
	"--replace-all": true, "--unset": true, "--unset-all": true, "--rename-section": true, "--remove-section": true,
}

// configSubcommands are the actions git 2.46 spells as subcommands. A setting
// always has a dot in it, so none of these is one.
var configSubcommands = map[string]bool{
	"list": true, "get": true, "set": true, "unset": true, "rename-section": true, "remove-section": true,
	"edit": true,
}

// placeRef is a place a git command is pointed at: 'd' its repository, 't' its
// work tree, 'i' its index.
type placeRef struct {
	value string
	kind  byte
}

// pointedAt is every place git is pointed at other than by the directory it
// runs in: GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE, core.worktree however
// it is given, and --git-dir and --work-tree as written.
func pointedAt(g gitRead, r ran, e *environment) []placeRef {
	var out []placeRef
	for _, v := range []struct {
		name string
		kind byte
	}{{"GIT_DIR", 'd'}, {"GIT_WORK_TREE", 't'}, {"GIT_INDEX_FILE", 'i'}} {
		if value, _, ok := e.get(r, v.name); ok {
			out = append(out, placeRef{value, v.kind})
		}
	}
	for _, s := range g.settings {
		if strings.EqualFold(s.key, "core.worktree") {
			value := s.value
			if !s.known {
				value = unknownValue
			}
			out = append(out, placeRef{value, 't'})
		}
	}
	args := g.args[:g.at]
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--git-dir" && i+1 < len(args):
			i++
			out = append(out, placeRef{args[i], 'd'})
		case a == "--work-tree" && i+1 < len(args):
			i++
			out = append(out, placeRef{args[i], 't'})
		case strings.HasPrefix(a, "--git-dir="):
			out = append(out, placeRef{strings.TrimPrefix(a, "--git-dir="), 'd'})
		case strings.HasPrefix(a, "--work-tree="):
			out = append(out, placeRef{strings.TrimPrefix(a, "--work-tree="), 't'})
		case a == "-C" || a == "-c" || a == "--namespace" || a == "--config-env" || a == "--attr-source":
			i++
		}
	}
	return out
}

// elsewhere reports whether a place of one of kinds lands in another checkout
// of the repository, or cannot be placed at all, where there are other
// checkouts. The project's own git directory, outside it in a linked
// worktree, is its own.
func (j *gitRules) elsewhere(places []placeRef, dir, kinds string) bool {
	if j.s.Shared == nil {
		return false
	}
	for _, p := range places {
		if strings.IndexByte(kinds, p.kind) < 0 {
			continue
		}
		c := clean(p.value)
		if c == "" || strings.ContainsAny(c, "$`%") || shellDirectory(c) {
			return true
		}
		if !isAbsolute(c) && !strings.HasPrefix(c, "~") {
			c = path.Join(dir, c)
		}
		for _, l := range j.m.shared(j.s, c) {
			if !l.Own {
				return true
			}
		}
	}
	return false
}

// placeDir is the directory a git command run from dir works in: where env -C
// sends it, and then its own -C.
func placeDir(r ran, dir string, g gitRead) string {
	dir, _ = ranIn(r, dir)
	return gitPlace(append([]string{"git"}, g.args...), dir)
}

// ranIn is the directory a command run from dir works in, once env -C, or a
// wrapper's option like it, has sent it on, and false where the words do not
// show where that is. Such a directory is "", the project, as gitPlace reads
// `git -C "$D"`: somewhere this cannot know is not another checkout.
func ranIn(r ran, dir string) (string, bool) {
	if r.chdir == "" {
		return dir, true
	}
	switch c := clean(r.chdir); {
	case strings.ContainsAny(c, "$`%*?[") || shellDirectory(c):
		return "", false
	case isAbsolute(c) || strings.HasPrefix(c, "~"):
		return c, true
	default:
		return path.Join(dir, c), true
	}
}
