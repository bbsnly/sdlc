package shellpolicy

import (
	"slices"
	"strconv"
	"strings"
)

// The two checks here refuse by class, after every older rule. Rounds of
// review each found one more way a shell gives a name a new meaning, or ends a
// quote where the readings do not, and a list of spellings was always one
// short: so a line that changes what a name runs refuses a later review or
// person's command, and a line whose quoting no reading follows refuses where
// sdlc or git is on it. Neither keeps package state.

// commandTable reports whether name is a list that maps a name to what runs:
// zsh's from its parameter module and bash's two.
func commandTable(name string) bool {
	switch name {
	case "commands", "functions", "dis_functions", "functions_source", "dis_functions_source",
		"aliases", "dis_aliases", "galiases", "dis_galiases", "saliases", "dis_saliases",
		"BASH_CMDS", "BASH_ALIASES":
		return true
	}
	return false
}

// codeTable reports whether name is a list whose values are code: every one
// commandTable names but the two whose values are paths.
func codeTable(name string) bool {
	return commandTable(name) && name != "commands" && name != "BASH_CMDS" &&
		name != "functions_source" && name != "dis_functions_source"
}

// anyWordTable reports whether a list's names expand outside the first word,
// so that a write to it can rename a word anywhere on a later line.
func anyWordTable(name string) bool {
	switch name {
	case "galiases", "dis_galiases", "saliases", "dis_saliases":
		return true
	}
	return false
}

// declareFamily is the builtins whose operands are assignments or names.
func declareFamily(program string) bool {
	switch program {
	case "declare", "typeset", "local", "readonly", "export", "integer", "float", "private":
		return true
	}
	return false
}

// changeWalk is one reading of a line walked in order: what the changes so far
// gave a new meaning to.
type changeWalk struct {
	powerShell bool
	change     string          // the first change, as written, for the reason
	trap       bool            // the first change is a trap
	names      map[string]bool // the names the changes write out
	every      bool            // a change reads as every name
	aliases    map[string]string
	nameref    bool
	values     []string
}

// tableChanged refuses a later review or person's command on a line that
// changes what a name runs, on any of the line's readings. ask reads a
// value as a command under the entry point the line came in by, and read
// makes the line's readings, once for every rule that walks them.
func tableChanged(command string, powerShell bool, reading assignments, read func(string, bool) []pair,
	ask func(string) (Finding, bool)) (Finding, bool) {
	for _, p := range read(command, powerShell) {
		if f, ok := changedOn(p.text, powerShell, reading, ask); ok {
			return f, true
		}
	}
	return Finding{}, false
}

func changedOn(text string, powerShell bool, reading assignments, ask func(string) (Finding, bool)) (Finding, bool) {
	runs := withAliasLines(text, powerShell, reading)
	w := changeWalk{powerShell: powerShell, names: map[string]bool{}, aliases: map[string]string{}}
	for _, r := range runs {
		// A value a change runs, and a script read again on its marks, are
		// read where they stand by every rule; they are no later run.
		if r.extra || r.value {
			continue
		}
		if w.changed() {
			if f, ok := w.later(r); ok {
				return f, true
			}
		}
		if f, ok := w.record(r, reading); ok {
			return f, true
		}
	}
	return w.askValues(ask)
}

// withAliasLines are a line's runs with each git alias's command line walked
// after the line, so a change and its use inside one alias line are read
// together.
func withAliasLines(text string, powerShell bool, reading assignments) []ran {
	runs, t := programsAt(text, powerShell, reading)
	lines, _ := gitAliasLines(text, runs, t, powerShell)
	for _, line := range lines {
		more, _ := programsAt(line, false, reading)
		runs = append(runs, more...)
	}
	return runs
}

// askValues asks the rules of each value a nameref on the line may have
// written, where one did.
func (w *changeWalk) askValues(ask func(string) (Finding, bool)) (Finding, bool) {
	if !w.nameref {
		return Finding{}, false
	}
	for _, v := range w.values {
		if f, ok := ask(v); ok {
			return namerefFinding(v, f), true
		}
	}
	return Finding{}, false
}

func (w *changeWalk) changed() bool { return w.change != "" }

// note records a change that names names, or every name where every is set.
func (w *changeWalk) note(r ran, every bool, names ...string) {
	if w.change == "" {
		w.change = shortened(strings.Join(r.words, " "))
		if len(r.words) == 0 {
			w.change = shortened(strings.Join(r.sets, " "))
		}
		w.trap = len(r.words) > 0 && base(r.words[0]) == "trap"
	}
	w.every = w.every || every
	for _, n := range names {
		w.names[n] = true
	}
}

// record reads one run for a change to what a name runs.
func (w *changeWalk) record(r ran, reading assignments) (Finding, bool) {
	for _, set := range append(append([]string{}, r.env...), r.sets...) {
		if a, ok := readAssignment(set); ok {
			w.assigned(r, a)
		}
	}
	words, marks := linedUp(r.words, r.marked)
	for _, m := range slices.Concat(marks, r.env, r.sets) {
		if f, ok := w.expansions(r, m); ok {
			return f, true
		}
	}
	if len(words) == 0 {
		return Finding{}, false
	}
	if r.defines {
		names := words
		if words[0] == "function" {
			names = words[1:]
		}
		every := false
		for _, n := range names {
			every = every || actsOnAnyName(n)
		}
		w.note(r, every, names...)
		return Finding{}, false
	}
	w.byProgram(r, words, marks, reading)
	return Finding{}, false
}

// assigned reads one assignment a run makes: a write to a list, or a value
// that names one, which a nameref or a later made name writes through.
func (w *changeWalk) assigned(r ran, a assignment) {
	w.values = append(w.values, a.value)
	if commandTable(tableName(a.value)) {
		w.note(r, true)
	}
	w.tableWrite(r, a.name, a.subscript, a.indexed, a.value)
}

// actsOnAnyName reports whether a function of this name runs with words of
// commands it does not name: the handler bash and zsh call with a command they
// cannot find, and a zsh trap function.
func actsOnAnyName(function string) bool {
	return function == "command_not_found_handle" || function == "command_not_found_handler" ||
		strings.HasPrefix(function, "TRAP")
}

// tableName is a value read as a list's name: up to its first `[`, and
// without a trailing `+`.
func tableName(v string) string {
	v, _, _ = strings.Cut(v, "[")
	return strings.TrimSuffix(v, "+")
}

// tableWrite records a write to a list, if name is one: the key it writes,
// or every name where no key is written out or the key is made.
func (w *changeWalk) tableWrite(r ran, name, subscript string, indexed bool, value string) {
	if !commandTable(name) {
		return
	}
	key := strings.ReplaceAll(subscript, `\`, "")
	if !indexed || anyWordTable(name) || key == "" || strings.ContainsAny(key, "$`*?[") || actsOnAnyName(key) {
		w.note(r, true)
		return
	}
	w.note(r, false, key)
	if name == "aliases" || name == "BASH_ALIASES" {
		w.aliases[key] = value
	}
}

// byProgram reads the builtins that change what a name runs.
func (w *changeWalk) byProgram(r ran, words, marks []string, reading assignments) {
	switch program := base(words[0]); {
	case program == "alias":
		w.alias(r, words, marks)
	case program == "unalias":
		w.unalias(r, words, marks)
	case program == "hash":
		w.hash(r, words, marks)
	case program == "trap":
		if action := trapAction(words[1:]); action != "" && action != "-" {
			w.note(r, true)
		}
	case program == "enable", program == "disable", program == "unfunction", program == "unhash",
		program == "autoload", program == "functions", program == "zmodload":
		w.note(r, true)
	case program == "unset":
		w.unset(r, words, marks)
	case declareFamily(program):
		w.declared(r, words, reading)
	case program == "set":
		w.setA(r, words, marks)
	case program == "print", program == "printf":
		for i := 1; i+1 < len(words); i++ {
			if words[i] == "-v" {
				name, rest, indexed := strings.Cut(words[i+1], "[")
				w.tableWrite(r, name, strings.TrimSuffix(rest, "]"), indexed, "")
			}
		}
	case program == "mapfile", program == "readarray":
		if commandTable(words[len(words)-1]) {
			w.note(r, true)
		}
	}
}

// alias reads `alias name=value …`: each name, made or written, and every name
// for an alias that expands outside the first word.
func (w *changeWalk) alias(r ran, words, marks []string) {
	for i := 1; i < len(words); i++ {
		word := words[i]
		if strings.HasPrefix(word, "-") || strings.HasPrefix(word, "+") {
			if word != "--" && strings.ContainsAny(word[1:], "gs") {
				w.note(r, true)
			}
			continue
		}
		name, value, ok := strings.Cut(word, "=")
		if !ok {
			continue
		}
		if madeWord(marks[i], w.powerShell) && madeName(marks[i]) || r.substituted {
			w.note(r, true)
			continue
		}
		w.note(r, false, name)
		w.aliases[name] = value
	}
}

// madeName reports whether the name part of an operand's mark, before its =,
// is made.
func madeName(mark string) bool {
	name, _, _ := strings.Cut(mark, "=")
	return !writtenOut(name)
}

func (w *changeWalk) unalias(r ran, words, marks []string) {
	for i := 1; i < len(words); i++ {
		switch {
		case words[i] == "-a":
			w.note(r, true)
		case strings.HasPrefix(words[i], "-"):
		case !writtenOut(marks[i]):
			w.note(r, true)
		default:
			w.note(r, false, words[i])
		}
	}
}

// hash reads bash's `hash -p path name`, the path attached or not, and zsh's
// `hash name=path`.
func (w *changeWalk) hash(r ran, words, marks []string) {
	path := false
	for i := 1; i < len(words); i++ {
		word := words[i]
		if strings.HasPrefix(word, "-") && word != "--" {
			gives, next := hashPath(word)
			path = path || gives
			if next {
				i++
			}
			continue
		}
		name, _, assigns := strings.Cut(word, "=")
		if !assigns && !path {
			continue
		}
		if !writtenOut(marks[i]) && (!assigns || madeName(marks[i])) {
			w.note(r, true)
			continue
		}
		w.note(r, false, name)
	}
}

// hashPath reads an option cluster of hash: whether it gives a path with -p,
// and whether that path is the next word rather than attached.
func hashPath(option string) (gives, next bool) {
	at := strings.IndexByte(option, 'p')
	return at >= 0, at == len(option)-1
}

func (w *changeWalk) unset(r ran, words, marks []string) {
	function := false
	for i := 1; i < len(words); i++ {
		word := words[i]
		if strings.HasPrefix(word, "-") {
			function = function || strings.ContainsRune(word, 'f')
			continue
		}
		if function {
			w.note(r, !writtenOut(marks[i]), word)
			continue
		}
		name, rest, indexed := strings.Cut(word, "[")
		w.tableWrite(r, name, strings.TrimSuffix(rest, "]"), indexed, "")
	}
}

// declared reads the declare family's operands: a list declared whole, and
// on a line with a nameref every value it assigns is read as a command.
func (w *changeWalk) declared(r ran, words []string, reading assignments) {
	for _, word := range words[1:] {
		if strings.HasPrefix(word, "-") {
			w.nameref = w.nameref || base(words[0]) != "export" && strings.ContainsRune(word[1:], 'n')
			continue
		}
		if !reading.is(word) && commandTable(tableName(word)) {
			w.note(r, true)
		}
	}
}

// setA reads zsh's `set -A name …` and `set +A name …`, a list assigned whole.
// A name the shell makes may be any list, so it is a change to every name, as
// a made name is for the other writers: read as the name written,
// `set -A ${n} s 'sdlc approve'; s US-001` changed nothing.
func (w *changeWalk) setA(r ran, words, marks []string) {
	at, ok := setAOperands(words)
	if !ok {
		return
	}
	if commandTable(words[at]) || !writtenOut(marks[at]) {
		w.note(r, true)
	}
	w.values = append(w.values, words[at+1:]...)
}

// expansions reads the `${…}` in a word that assign: a write to a list, read
// as any write is, and one with zsh's flags or bash's `!` before the name,
// which is refused wherever it stands.
func (w *changeWalk) expansions(r ran, word string) (Finding, bool) {
	for i := strings.Index(word, "${"); i >= 0; {
		head := word[i+2:]
		name, rest, flagged := expansionHead(head)
		if op := assigningOperator(rest); op != "" {
			if flagged {
				return flaggedExpansion(), true
			}
			n, sub, indexed := strings.Cut(name, "[")
			w.tableWrite(r, n, strings.TrimSuffix(sub, "]"), indexed, "")
		}
		next := strings.Index(head, "${")
		if next < 0 {
			break
		}
		i += 2 + next
	}
	return Finding{}, false
}

// expansionHead reads the start of a `${…}`: whether flags, a modifier or `!`
// come before the name, the name with its subscript, and what follows.
func expansionHead(head string) (name, rest string, flagged bool) {
	for head != "" && strings.ContainsRune("(=~^#+!", rune(head[0])) {
		flagged = true
		if head[0] == '(' {
			end := strings.IndexByte(head, ')')
			if end < 0 {
				return "", "", true
			}
			head = head[end+1:]
			continue
		}
		head = head[1:]
	}
	j := 0
	for j < len(head) && nameRune(rune(head[j])) {
		j++
	}
	if j < len(head) && head[j] == '[' {
		if end, ok := subscriptEnd(head, j); ok {
			j = end + 1
		}
	}
	return head[:j], head[j:], flagged
}

// assigningOperator is the assignment operator rest starts with: `=`, `:=`
// or zsh's `::=`, or "".
func assigningOperator(rest string) string {
	for _, op := range []string{"::=", ":=", "="} {
		if strings.HasPrefix(rest, op) {
			return op
		}
	}
	return ""
}

// later reads a run after a change: one whose program word is sdlc, or a name
// a change gave a new meaning, or any once a change reads as every name, is
// read from every word on for a review or a person's command.
func (w *changeWalk) later(r ran) (Finding, bool) {
	words, _ := linedUp(r.words, r.marked)
	if len(words) == 0 {
		return Finding{}, false
	}
	program := words[0]
	if !w.every && !isSdlc(program) && !w.names[program] {
		return Finding{}, false
	}
	// A help request to sdlc written out, where no change named it.
	if program == "sdlc" && !w.every && !w.names["sdlc"] && asksForHelp(words[1:]) {
		return Finding{}, false
	}
	read := w.expanded(words)
	for j := range read {
		args := read[j:]
		if readReview(args, args, w.powerShell).addWord {
			return w.finding(words, args, true, j == 1), true
		}
		if personOnly(args) {
			return w.finding(words, args, false, j == 1), true
		}
	}
	return Finding{}, false
}

// expanded is words as bash expands them after a program word whose alias
// value ends in a blank: each next word that is an alias the line defined is
// replaced by its value while the value before it ends in a blank. The
// program word itself is not replaced: a value that holds a whole command is
// read where it is defined.
func (w *changeWalk) expanded(words []string) []string {
	out := []string{words[0]}
	value := w.aliases[words[0]]
	for i := 1; i < len(words); i++ {
		next, ok := w.aliases[words[i]]
		if !ok || !strings.HasSuffix(value, " ") && !strings.HasSuffix(value, "\t") {
			return append(out, words[i:]...)
		}
		out = append(out, strings.Fields(next)...)
		value = next
	}
	return out
}

// finding is the class's refusal of a later run, words, read from args.
// subcommand is whether args start right after the program word, the one
// place the words can be meant as sdlc's: `git tag release` read from
// `release` was never meant as `sdlc release`, and asking whether it was read
// as a false alarm.
func (w *changeWalk) finding(words, args []string, review, subcommand bool) Finding {
	line, read := shortened(strings.Join(words, " ")), shortened("sdlc "+strings.Join(args, " "))
	f := w.changeSaid(line, read, words[0], review)
	switch {
	case review:
		f.Rule = reviewerRuleName
	case subcommand:
		sub := shortened(strings.Join(args[:min(len(args), 2)], " "))
		f.Route += "; if you meant `sdlc " + sub + "`, it is a person's to run, and a session that meant it hands " +
			"the story over"
	}
	return f
}

// changeSaid is the reason and the route for line, read as read after the
// line's first change, program being line's program word and review whether
// it reads as a review of the agent's own. A trap's route is
// not an alias's: a trap set in a command of its own fires as that command
// ends, so "run the change in its own command" ran an EXIT trap's cleanup
// before the command it was for.
func (w *changeWalk) changeSaid(line, read, program string, review bool) Finding {
	f := Finding{Rule: assignmentRule}
	if w.trap {
		how := "this line sets a trap (`" + w.change + "`), whose action can read a later command's words " +
			"(`$BASH_COMMAND`), and the rules do not read what the trap does"
		f.Reason = how + ", so `" + line + "` after it is read as `" + read + "`, a command the trap could run"
		if review && program == "sdlc" {
			f.Reason = "Your review is yours to record, but " + how
		}
		f.Route = "drop the trap and do the cleanup after the command on the same line (`" + line +
			"; <cleanup>`), or run the command on a line of its own"
		return f
	}
	how := "this line changes what a name runs (`" + w.change + "`)"
	f.Reason = how + ", and after it `" + line + "` is read as `" + read + "`, because the rules cannot tell which " +
		"program `" + shortened(program) + "` is"
	if review && program == "sdlc" {
		f.Reason = "Your review is yours to record, but " + how + ", and the rules cannot tell whether `sdlc` " +
			"still means sdlc"
	}
	f.Route = "run the alias, `hash`, function or other change in its own command, then this command in the next " +
		"one, with any `sdlc` written out"
	return f
}

// namerefFinding is the refusal of a value on a line that declares a nameref.
func namerefFinding(value string, f Finding) Finding {
	return Finding{
		Rule: assignmentRule,
		Reason: "this line declares a nameref (`declare -n`), which can make any name on the line write to a list of " +
			"aliases or functions, so every value it assigns is read as a command, and " + strconv.Quote(shortened(value)) +
			" would be refused as a command (" + f.Rule + ")",
		Route: "write to the variable by its own name rather than through a nameref, or put the nameref and the value " +
			"in separate commands",
	}
}

func flaggedExpansion() Finding {
	return Finding{
		Rule: assignmentRule,
		Reason: "this line assigns through a `${…}` with flags or `!`, which can change which name it writes and what " +
			"it stores, so the rules cannot read it",
		Route: "assign in a command of its own, by the name itself: `x=y`, or `n[k]=v`; to keep `:=`'s meaning, test " +
			"first, `[ -n \"$x\" ] || x=y`, and apply a flag where the value is read, `echo ${(L)x}`",
	}
}

// unclosedLine refuses a line whose quoting the rules cannot follow, where
// sdlc or git is on it: every reading ends inside a quote, or the lexer of a
// reading recorded that it read the line otherwise than the shells. On such a
// line the word boundaries are what is wrong, so it is read as text, which
// takes no position on where a quote ends. It lexes only a line holding a
// character a misreading needs.
func unclosedLine(command string, powerShell bool, reading assignments, read func(string, bool) []pair) (Finding, bool) {
	if !mayMisread(command, powerShell) {
		return Finding{}, false
	}
	pairs := read(command, powerShell)
	open, misread := len(pairs) > 0, false
	texts := []string{command, dequoted(command)}
	for _, p := range pairs {
		_, t := programsAt(p.text, powerShell, reading)
		l := lexUnder(p.text, powerShell, reading)
		open = open && l.unclosed
		misread = misread || t.misread || l.misread
		texts = append(texts, p.text)
	}
	if !open && !misread {
		return Finding{}, false
	}
	for _, text := range texts {
		if strings.Contains(text, "sdlc") || strings.Contains(text, "git") {
			return unclosedFinding(), true
		}
	}
	if readsAsDecision(dequotedWords(dequoted(command))) {
		return unclosedFinding(), true
	}
	return Finding{}, false
}

// mayMisread reports whether a line holds a character a misreading needs.
func mayMisread(command string, powerShell bool) bool {
	return !powerShell && (strings.ContainsAny(command, "'\"`") || strings.Contains(command, "$(") ||
		strings.Contains(command, "${") || strings.Contains(command, "=("))
}

// readsAsDecision reports whether words, from any one of them on, read as a
// review or a command only a person runs.
func readsAsDecision(words []string) bool {
	for j := range words {
		if readReview(words[j:], words[j:], false).addWord || personOnly(words[j:]) {
			return true
		}
	}
	return false
}

// dequoted is a line with each dollar sign followed by an empty pair of
// quotes taken out, then each backslash and newline, then every quote and
// backslash: `s""dlc`, `g\it` and `"g"it` read as sdlc and git however the
// shells close the quotes.
func dequoted(line string) string {
	line = strings.NewReplacer(`$''`, "", `$""`, "").Replace(line)
	line = strings.ReplaceAll(line, "\\\n", "")
	return strings.NewReplacer(`'`, "", `"`, "", `\`, "").Replace(line)
}

// dequotedWords splits de-quoted text at blanks and at the characters that
// end a word or a command, so a program word the shell makes, `$(printf
// sd)lc`, leaves the words after it to be read.
func dequotedWords(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || strings.ContainsRune(";&|()<>`", r)
	})
}

func unclosedFinding() Finding {
	return Finding{
		Rule: assignmentRule,
		Reason: "the quoting on this line ends where the hook's reading of it does not, so the rules cannot tell " +
			"which words run as commands, and sdlc or git is on it, or its words read as a review or a command only " +
			"a person runs",
		Route: "close each quote where you mean it to end, and keep a quote out of a `${…}` default (set it in a " +
			"variable first); or run the sdlc or git command in a command of its own",
	}
}

// renamed is what a name the line renamed runs: an alias's value or a hashed
// path, as words with their marks.
type renamed struct {
	words, marks []string
	blank        bool // an alias value ending in a blank, which expands the next word too
}

// renamedRuns are a line's later runs whose program word the line made an
// alias of, or gave a path, with the value or the path spliced in front of
// the use's words as bash and zsh run them, so a line meets the rule its
// command meets: read as written, `alias s=sdlc; s approve US-001` was `s`.
// The class (tableChanged) is the guarantee; the splice names the rule.
func renamedRuns(runs []ran) []ran {
	names := map[string]renamed{}
	var out []ran
	for _, r := range runs {
		if r.extra || r.value {
			continue
		}
		words, marks := linedUp(r.words, r.marked)
		if len(words) > 0 {
			if spliced, ok := splice(words, marks, names); ok {
				out = append(out, ran{words: spliced.words, marked: spliced.marks, group: r.group, piped: r.piped,
					substituted: r.substituted, env: r.env, script: true, renamedFrom: strings.Join(words, " ")})
			}
		}
		renames(r, words, names)
	}
	return out
}

// splice puts what the program word was renamed to in its place, looking the
// new first word up again while it names a rename this chain has not used,
// and expanding the next word while an alias value ends in a blank.
func splice(words, marks []string, names map[string]renamed) (renamed, bool) {
	used := map[string]bool{}
	out := renamed{words: words, marks: marks}
	for {
		v, ok := names[out.words[0]]
		if !ok || used[out.words[0]] || len(v.words) == 0 {
			break
		}
		used[out.words[0]] = true
		rest, restMarks := out.words[1:], out.marks[1:]
		if v.blank && len(rest) > 0 {
			if next, ok := names[rest[0]]; ok {
				rest = slices.Concat(next.words, rest[1:])
				restMarks = slices.Concat(next.marks, restMarks[1:])
			}
		}
		out = renamed{words: slices.Concat(v.words, rest), marks: slices.Concat(v.marks, restMarks)}
	}
	return out, len(used) > 0
}

// renames records what a run renames: `alias name=value`, bash's `hash -p
// path name`, zsh's `hash name=path`, and a write with a written key to
// aliases, BASH_ALIASES, commands or BASH_CMDS.
func renames(r ran, words []string, names map[string]renamed) {
	for _, set := range slices.Concat(r.env, r.sets) {
		a, ok := readAssignment(set)
		if !ok || !a.indexed {
			continue
		}
		switch a.name {
		case "aliases", "BASH_ALIASES", "commands", "BASH_CMDS":
			names[a.subscript] = renamedTo(a.value, r.substituted)
		}
	}
	if len(words) == 0 {
		return
	}
	switch base(words[0]) {
	case "alias":
		for _, w := range words[1:] {
			if name, value, ok := strings.Cut(w, "="); ok && !strings.HasPrefix(w, "-") {
				names[name] = renamedTo(value, r.substituted)
			}
		}
	case "hash":
		hashRenames(words, r.substituted, names)
	}
}

// hashRenames records bash's `hash -p path name` and zsh's `hash name=path`.
func hashRenames(words []string, cut bool, names map[string]renamed) {
	path := ""
	for i := 1; i < len(words); i++ {
		w := words[i]
		switch {
		case strings.HasPrefix(w, "-p") && len(w) > 2:
			path = w[2:]
		case w == "-p" && i+1 < len(words):
			i++
			path = words[i]
		case strings.Contains(w, "="):
			name, value, _ := strings.Cut(w, "=")
			names[name] = renamedTo(value, cut)
		case path != "":
			names[w] = renamedTo(path, cut)
		}
	}
}

// renamedTo is a value as the words it splices in. One the outer shell cut,
// or holding a made word, splices as a made program word, so what follows
// it is read as after any made program word.
func renamedTo(value string, cut bool) renamed {
	words := strings.Fields(value)
	if len(words) == 0 && cut {
		words = []string{""}
	}
	marks := slices.Clone(words)
	if len(marks) > 0 && (cut || strings.ContainsAny(value, "$`")) {
		marks[0] = cutMark + marks[0]
	}
	return renamed{words: words, marks: marks, blank: strings.HasSuffix(value, " ") || strings.HasSuffix(value, "\t")}
}

// checkRenamed asks the sdlc rules of each run a line's renames splice.
func checkRenamed(command string, s State, reading assignments) (Finding, bool) {
	for _, p := range s.seen.readingsOf(command, s.PowerShell, reading) {
		runs, _ := programsAt(p.text, s.PowerShell, reading)
		for _, r := range renamedRuns(runs) {
			if f, ok := checkSdlcCommands(r.words, r.marked, r.piped, r.substituted, s); ok {
				return renamedFinding(f, r), true
			}
		}
	}
	return Finding{}, false
}

// renamedFinding says the run was read through a rename.
func renamedFinding(f Finding, r ran) Finding {
	f.Reason += "; `" + shortened(r.renamedFrom) + "` is `" + shortened(strings.Join(r.words, " ")) +
		"`, because a name in it is an alias, or a `hash` path, defined earlier on this line"
	return f
}
