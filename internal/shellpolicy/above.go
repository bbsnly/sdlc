package shellpolicy

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// rootNames are the names of an absolute path after its drive and leading
// slash: /work/project is work and project.
func rootNames(p string) []string {
	p = p[strings.IndexByte(p, '/')+1:]
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
}

// levelsAbove is how many names above the project dir is, as the command
// spells it, or 0 where it is not above. dir is k names above where dir,
// followed by the last k names of Root and then .sdlc, is the project's own
// .sdlc: Resolve answers exactly `.sdlc` only for a path that leads there,
// and the model cannot replace the project's .sdlc while a story is worked
// on. Nothing is asked of <dir>/.sdlc itself, so a link planted there
// (`ln -s <root>/docs ~/code/.sdlc`) changes nothing; asked of it, the probe
// was turned off by one. Every k is tried, because the macOS firmlink spells
// a directory up to all of Root's names above it. The root itself is not
// probed: Resolve answers "" for it, as it does for outside.
func (m *memo) levelsAbove(s State, dir string) int {
	names := rootNames(s.Root)
	for k := 1; s.Resolve != nil && k <= len(names); k++ {
		if m.resolve(s, path.Join(dir, path.Join(names[len(names)-k:]...), ".sdlc")) == ".sdlc" {
			return k
		}
	}
	return 0
}

// reachesProject reports whether a glob in an absolute or ~ word, spelt as
// clean leaves it, could name a path in the project. Resolve reads a glob
// as a literal name, which is nowhere, so `echo x >
// ~/code/proj*/.sdlc/state/tests.lock` was outside and let through. The glob
// is matched against Root's own names rather than read as never outside:
// `/work/other*/.sdlc/config.json` and `/tmp/*/tests.lock` are another
// project's files.
//
// A segment that holds `**` stands for any run of those names, none
// included, as zsh and bash's globstar read it, once the part before it is
// the project's directory or above it; every such segment, since zsh's
// GLOB_STAR_SHORT makes `**x` recurse. Where one has a `..` after it, the
// answer is yes whatever comes before: `**` can stand for no directories,
// so the `..` climbs out of the part before it. It asks the disk nothing but
// levelsAbove.
func (m *memo) reachesProject(s State, c string) bool {
	// pathrules.Under(P, "") holds for any P.
	if s.Root == "" {
		return false
	}
	segs := strings.Split(c, "/")
	g := slices.IndexFunc(segs, func(seg string) bool { return strings.ContainsAny(seg, "*?[") })
	prefix := cmp.Or(strings.Join(segs[:g], "/"), "/")
	if climbsPastGlobstar(c) || pathrules.Under(prefix, s.Root) {
		return true
	}
	// By text where the prefix is an ancestor of Root's own spelling, and
	// otherwise by asking the disk.
	var k int
	if isAbsolute(prefix) && pathrules.Under(s.Root, prefix) {
		k = len(rootNames(s.Root)) - len(rootNames(prefix))
	} else {
		k = m.levelsAbove(s, prefix)
	}
	names := rootNames(s.Root)
	names, rest := names[len(names)-k:], segs[g:]
	// Each segment is matched against the name it stands for, until one
	// holds `**`, which stands for the rest of them; a word that ends before
	// the names do is above the project, not in it.
	i := 0
	for i < len(names) && i < len(rest) && !strings.Contains(rest[i], "**") && segmentMatches(rest[i], names[i]) {
		i++
	}
	return k > 0 && (i == len(names) || i < len(rest) && strings.Contains(rest[i], "**"))
}

// globRoute is what a refusal's route gains where the word is held only
// because its glob can reach the project: `/work/proj*`, which
// /work/project2 also matches.
const globRoute = "; if the directory is not this project, spell it out without a glob"

// byGlobRoute is f with the glob's clause on its route, where the glob arm
// of memo.outside placed the word.
func (m *memo) byGlobRoute(f Finding, c string) Finding {
	if m.byGlob[clean(c)] {
		f.Route += globRoute
	}
	return f
}

// made is a word a command writes that it does not spell, with the clause a
// refusal of it ends with.
type made struct{ word, clause string }

// madeWords are the words a ln, install or decompressor writes, as
// writtenTo gives them, with what puts each there: the reason that names
// docs/guide.md/CLAUDE.md for `ln -s /tmp/x/CLAUDE.md docs/guide.md` says
// why. Where the destination may be a file the clause says so, and where a
// bundle ending in t is read both ways, the release's reading does too.
func madeWords(words []string) []made {
	name := base(words[0])
	if written, ok := decompressed(name, words[1:]); ok {
		return clauses(written, name+" writes it")
	}
	if name != "ln" && name != "install" {
		return nil
	}
	what := "it"
	if name == "ln" {
		what = "the link"
	}
	from, to, target := linkArguments(name, words[1:])
	sure := name + " puts " + what + " there"
	clause := "if " + clean(to) + " is a directory, " + sure
	bundled, into, both := bundleTarget(name, words[1:])
	switch c := clean(to); {
	case both:
		clause = "if " + name + " treats the last operand as the destination, it puts " + what + " there"
	case target || c == "." || c == ".." || strings.HasSuffix(to, "/"):
		clause = sure
	}
	out := clauses(landedIn(to, from), clause)
	if both {
		out = append(out, clauses(landedIn(into, bundled), sure)...)
	}
	return out
}

func clauses(words []string, clause string) []made {
	out := make([]made, 0, len(words))
	for _, w := range words {
		out = append(out, made{w, clause})
	}
	return out
}

// noteMade notes, once for the segment, each word its command writes and
// does not spell, under each spelling the rules read it in. Keyed by the
// segment, so a word one segment lands and a later one spells carries no
// clause in the later one.
func (q pathQuestion) noteMade() {
	m := q.m
	key := q.segment + "\x00" + q.dir
	if m.madeFor == key {
		return
	}
	m.madeFor = key
	for _, w := range madeWords(q.run.words) {
		if slices.Contains(q.run.words, w.word) {
			continue
		}
		for _, spelt := range m.spell(q.s, q.dir, []string{w.word}) {
			if m.madeBy == nil {
				m.madeBy = map[string]string{}
			}
			m.madeBy[key+"\x00"+spelt] = w.clause
		}
	}
}

// madeReason is f with the clause of the word c, where the segment's command
// wrote it without spelling it.
func (q pathQuestion) madeReason(f Finding, c string) Finding {
	if clause, ok := q.m.madeBy[q.segment+"\x00"+q.dir+"\x00"+c]; ok {
		f.Reason += " (" + clause + ")"
	}
	return f
}

// ruleAbove is the rule an instruction file above the project is refused
// under, from the file tools and from the shell.
const ruleAbove = "instructions-above-the-project"

// aboveReason and aboveRoute are the rule's words for a directory spelt out.
// internal/policy says the same of a file tool's write.
const aboveReason = " is an instruction file above the project, which is the checkout this session works in " +
	"(so a story's worktree has the main checkout above it): Claude Code loads CLAUDE.md, CLAUDE.local.md and " +
	"AGENTS.md from every directory above the one it starts in, so what a session writes there is read as " +
	"instructions by every later session started below it, in this project and in others"

const aboveRoute = "say what you would change there and why, and stop: the person changes the instruction " +
	"files above the project by hand, outside a running iteration"

// globAboveReason and globAboveRoute are its words for a directory spelt
// with a glob, which can name one above the project or not.
const globAboveReason = " can name an instruction file above the project, because the shell fills in the glob " +
	"in its directory and that can match one above it; the project is the checkout this session works in (so a " +
	"story's worktree has the main checkout above it), and Claude Code loads CLAUDE.md, CLAUDE.local.md and " +
	"AGENTS.md from every directory above the one it starts in, so what a session writes there is read as " +
	"instructions by every later session started below it"

const globAboveRoute = "if the directory is not above the project, spell it out without a glob; otherwise " +
	aboveRoute

// climbsReason is what a glob reason ends with where `**` has a `..` after
// it, which no name above the project need match.
const climbsReason = " (`**` can stand for no directories, so the `..` after it climbs out of the part before it)"

// fillsAboveReason and fillsAboveRoute are the rule's words for a command
// that fills a whole directory above the project.
const fillsAboveReason = " is a directory above the project, which is the checkout this session works in (so a " +
	"story's worktree has the main checkout above it), and this command puts files into it that may include an " +
	"instruction file; Claude Code loads CLAUDE.md, CLAUDE.local.md and AGENTS.md from every directory above " +
	"the one it starts in, so an instruction file that lands there is read by every later session started below it"

const fillsAboveRoute = "say what you would copy or move there and why, and stop: the person puts files into a " +
	"directory above the project by hand, outside a running iteration, or you can write them inside the project instead"

// globFillsReason and globFillsRoute are its words for a directory spelt with
// a glob, which can be one above the project or not: `cd /work/proj* && tar
// -xf p.tar` fills the project or one beside it.
const globFillsReason = " can name a directory above the project, because the shell fills in the glob in its " +
	"directory and that can match one above it; the project is the checkout this session works in (so a story's " +
	"worktree has the main checkout above it), and this command puts files into it that may include an instruction " +
	"file; Claude Code loads CLAUDE.md, CLAUDE.local.md and AGENTS.md from every directory above the one it starts " +
	"in, so an instruction file that lands there is read by every later session started below it"

const globFillsRoute = "if the directory is not above the project, spell it out without a glob; otherwise " +
	fillsAboveRoute

// How aboveDir placed a directory: by its text or the disk, through a glob,
// or through a `**` with a `..` after it.
const (
	aboveSpelt = iota
	aboveGlob
	aboveClimbs
)

// checkAboveProject refuses a command that writes an instruction file in a
// directory above the project (AC-13). The words are the ones the person's
// configuration rules ask, read for what a command writes rather than what
// it names, so that `sed -n p ~/code/CLAUDE.md` reads freely; each is read
// through the protected shapes, so a glob, a brace list or a backslash in
// the name is the name. It runs before the loop-state rule, whose name match
// refuses `../CLAUDE.md` today, and only refuses, so it needs no certainty
// about the command.
func checkAboveProject(q pathQuestion) (Finding, bool) {
	s := q.s
	if s.Root == "" || len(q.run.words) == 0 {
		return Finding{}, false
	}
	q.noteMade()
	inside, held := q.configWords()
	shapes := func() []string { return protectedShapes(s.Backlog) }
	for _, w := range slices.Concat(inside, held) {
		named := spellings("", []string{w})
		// withGlobs leaves a last name with none of these as it is, and the
		// last name is all that is asked of it here; a brace in the directory
		// is read in the brace-expanded reading of the command. A glob that
		// can match no instruction file is not expanded either: expanded, the
		// `.*` of `rm a/a/.../.*` doubled the hook's time on that line. It is
		// asked of each spelling, since dd's `of=CLAUDE.m?` names one only in
		// the spelling without its `of=`.
		last := w[strings.LastIndexByte(w, '/')+1:]
		if strings.ContainsAny(last, `{\`) ||
			strings.ContainsAny(last, "*?[") && slices.ContainsFunc(named, globsAnInstructionFile) {
			named = withGlobs(named, shapes)
		}
		for _, spelt := range named {
			if f, ok := q.aboveWord(spelt); ok {
				return f, true
			}
		}
	}
	return Finding{}, false
}

// instructionName reports whether a last name is one of the instruction
// files, folded as the disk folds it. Each name is folded once a command:
// folded for every word of every segment, a long line of xargs writers took
// six seconds under the race detector, against one before.
func (m *memo) instructionName(name string) bool {
	is, ok := m.instruction[name]
	if !ok {
		folded := pathrules.Fold(name)
		is = slices.ContainsFunc(model.InstructionFiles, func(f string) bool { return folded == pathrules.Fold(f) })
		if m.instruction == nil {
			m.instruction = map[string]bool{}
		}
		m.instruction[name] = is
	}
	return is
}

// aboveWord is the refusal of one spelling of a written word, where its last
// name is an instruction file, folded as the disk folds it, and its directory
// is above the project. ~/.claude and ~/.agents keep their own rules.
func (q pathQuestion) aboveWord(spelt string) (Finding, bool) {
	s := q.s
	c := clean(spelt)
	name := c[strings.LastIndexByte(c, '/')+1:]
	if !q.m.instructionName(name) ||
		s.ClaudeConfig != nil && s.ClaudeConfig(spelt) || s.AgentsConfig != nil && s.AgentsConfig(spelt) {
		return Finding{}, false
	}
	shown, how, ok := q.aboveDir(c[:strings.LastIndexByte(c, '/')+1])
	if !ok {
		return Finding{}, false
	}
	word := strings.TrimSuffix(shown, "/") + "/" + name
	f := Finding{Rule: ruleAbove, Reason: word + aboveReason, Route: aboveRoute}
	if how != aboveSpelt {
		f = Finding{Rule: ruleAbove, Reason: word + globAboveReason, Route: globAboveRoute}
	}
	if how == aboveClimbs {
		f.Reason += climbsReason
	}
	return q.madeReason(f, spelt), true
}

// checkFillsAbove refuses a command that fills a whole directory above the
// project with files that may include an instruction file: a recursive copy
// or a move, a copy whose source is a glob that can match one, ditto, or an
// extraction. It is asked last, so every rule before it keeps the name of
// what it refuses, and the home's rules refuse `cp -r dotfiles/. ~` first.
//
// Where a cd into an absolute glob lost the walk, the fill is placed from
// the glob, which 11a does not do: placed so there, `cd {parent}/* && echo x
// > CLAUDE.md` lost the protected-path rule it is refused under, while here
// every named word was refused by loop state already. Not placed, `cd /wor?
// && tar -xf /tmp/p.tar` filled /work.
func checkFillsAbove(q pathQuestion) (Finding, bool) {
	if q.s.Root == "" || len(q.run.words) == 0 {
		return Finding{}, false
	}
	if q.lost && q.from == "" && isAbsolute(q.dir) && strings.ContainsAny(q.dir, "*?[") {
		q.from = q.dir
	}
	for _, d := range filledDirs(q.run.words) {
		shown, how, ok := q.aboveDir(clean(d))
		if !ok {
			continue
		}
		if how == aboveSpelt {
			return Finding{Rule: ruleAbove, Reason: shown + fillsAboveReason, Route: fillsAboveRoute}, true
		}
		f := Finding{Rule: ruleAbove, Reason: shown + globFillsReason, Route: globFillsRoute}
		if how == aboveClimbs {
			f.Reason += climbsReason
		}
		return f, true
	}
	return Finding{}, false
}

// filledDirs are the directories a command puts files into whole, where
// "." is the one it runs in. make -C and git -C are left out: they run a
// program there rather than copy files into it.
func filledDirs(words []string) []string {
	name, args := base(words[0]), words[1:]
	switch name {
	case "cp", "copy", "copy-item", "cpi", "mv", "move", "move-item", "mi", "ln", "install":
		from, to, _ := linkArguments(name, args)
		if copiesWhole(words) || slices.ContainsFunc(from, globsAnInstructionFile) {
			return []string{to}
		}
	case "rsync":
		return destination(copyOperands(args, rsyncValued))
	case "scp":
		return destination(copyOperands(args, scpValued))
	case "ditto":
		return destination(nonOptions(args))
	case "tar", "bsdtar":
		if _, into, _, extract := tarArguments(args); extract {
			return orHere(into)
		}
	case "unzip":
		return unzipInto(args)
	case "7z", "7za", "7zr", "7zz":
		return sevenZipInto(args)
	}
	return nil
}

// unzipInto is where unzip extracts: -d's directory, or the one it runs in,
// and nowhere where -l, -t, -v, -p, -c, -z or -Z has it only list, test or
// print.
func unzipInto(args []string) []string {
	if slices.ContainsFunc(args, func(a string) bool { return slices.Contains(unzipReads, a) }) {
		return nil
	}
	return orHere(valueOf(args, "-d"))
}

var unzipReads = []string{"-l", "-t", "-v", "-p", "-c", "-z", "-Z"}

// sevenZipInto is where 7z x or 7z e extracts: the directory glued to -o, or
// the one it runs in.
func sevenZipInto(args []string) []string {
	operands := nonOptions(args)
	if len(operands) == 0 || operands[0] != "x" && operands[0] != "e" {
		return nil
	}
	into := "."
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "-o"); ok {
			into = v
		}
	}
	return []string{into}
}

// destination is the last of a copy's operands, where it has a source too:
// none of one operand, or of none.
func destination(operands []string) []string {
	return operands[min(max(len(operands)-1, 1), len(operands)):]
}

// orHere is dirs, or the directory the command runs in where there are none.
func orHere(dirs []string) []string {
	if len(dirs) == 0 {
		return []string{"."}
	}
	return dirs
}

// globsAnInstructionFile reports whether a source's last name holds a glob
// that can match an instruction file: `cp /tmp/p/* ..` copies the CLAUDE.md
// there, if there is one, and `*.txt` cannot.
func globsAnInstructionFile(source string) bool {
	c := clean(source)
	name := c[strings.LastIndexByte(c, '/')+1:]
	return strings.ContainsAny(name, "*?[") &&
		slices.ContainsFunc(model.InstructionFiles, func(f string) bool { return segmentMatches(name, f) })
}

// aboveDir reports whether a directory, as the command spells it, is above
// the project, with the directory as placed from where the command is, for
// the refusal to name, and how it was placed. Its callers ask it only where
// Root is set.
//
// Nothing is cut with path.Dir or joined with path.Join where a `**` has a
// `..` after it: each reads `**/..` as one name and cleans it away, and
// `/tmp/**/../work` was read as /tmp/work.
func (q pathQuestion) aboveDir(dir string) (shown string, how int, ok bool) {
	// A file in an ancestor's .claude is held as in the ancestor.
	lifted, c := "", strings.TrimSuffix(dir, "/")
	if i := strings.LastIndexByte(c, '/'); pathrules.SameName(c[i+1:], ".claude") {
		lifted, dir = c[i+1:], c[:i+1]
	}
	shown, how, ok = q.placedAbove(dir)
	if lifted != "" {
		shown = strings.TrimSuffix(shown, "/") + "/" + lifted
	}
	return shown, how, ok
}

// placedAbove is aboveDir for a directory that is not a .claude.
func (q pathQuestion) placedAbove(dir string) (string, int, bool) {
	s, m := q.s, q.m
	if !climbsPastGlobstar(dir) {
		dir = path.Clean(dir)
	}
	if shellDirectory(dir) || strings.ContainsAny(dir, "$%") {
		// Resolve reads no variables: the home, spelt with one, is above
		// where the hook's reading of $HOME says it is and the home is above
		// the project. Every such word is refused by name already. ~+ and ~-
		// are where the shell is, which the lookup cannot place.
		return dir, aboveSpelt, !shellDirectory(dir) && s.ClaudeConfigHeld != nil && s.ClaudeConfigHeld(dir) &&
			m.levelsAbove(s, "~") > 0
	}
	shown := dir
	if !isAbsolute(dir) && !strings.HasPrefix(dir, "~") {
		var ok bool
		if shown, dir, ok = q.fromStart(dir); !ok {
			return "", 0, false
		}
	}
	if strings.ContainsAny(dir, "*?[") {
		how, ok := q.globAbove(dir)
		return shown, how, ok
	}
	if isAbsolute(dir) {
		if c := path.Clean(dir); pathrules.Under(s.Root, c) {
			return shown, aboveSpelt, !pathrules.SameName(c, s.Root)
		}
	}
	return shown, aboveSpelt, m.levelsAbove(s, dir) > 0
}

// fromStart is a relative directory joined to where it starts, for the
// refusal to show, and that joined to the root as well where it is still
// relative. It starts where the command is, or where a cd to a ~ path left
// it. Where a cd into a glob left it is not placed: the glob can match the
// project itself, and the project's rules refuse the word there by its name.
func (q pathQuestion) fromStart(dir string) (shown, joined string, ok bool) {
	start := q.dir
	switch {
	case !q.lost:
	case q.from != "":
		start = q.from
	default:
		return "", "", false
	}
	shown = joinAbove(start, dir)
	if isAbsolute(shown) || strings.HasPrefix(shown, "~") {
		return shown, shown, true
	}
	return shown, joinAbove(q.s.Root, shown), true
}

// globAbove is whether an absolute or ~ directory with a glob in it can be
// above the project: always where a `**` has a `..` after it, and otherwise
// unless the part before the glob is the project or inside it, by its text or
// on disk.
func (q pathQuestion) globAbove(dir string) (int, bool) {
	if climbsPastGlobstar(dir) {
		return aboveClimbs, true
	}
	s, m := q.s, q.m
	segs := strings.Split(dir, "/")
	g := slices.IndexFunc(segs, func(seg string) bool { return strings.ContainsAny(seg, "*?[") })
	prefix := cmp.Or(strings.Join(segs[:g], "/"), "/")
	inside := pathrules.Under(prefix, s.Root) || m.resolve(s, prefix) != "" || m.resolve(s, prefix+"/.sdlc") == ".sdlc"
	return aboveGlob, !inside
}

// joinAbove is dir joined to start, cleaned, but where either has `**`
// before a `..`, which path.Join would clean away: `cd /tmp/**/../work &&
// tar -xf p.tar` was read as filling /tmp/work.
func joinAbove(start, dir string) string {
	if climbsPastGlobstar(start) || climbsPastGlobstar(dir) {
		if dir == "." {
			return start
		}
		return start + "/" + dir
	}
	return path.Join(start, dir)
}

// movedFrom is where a cd leaves a lost walk, as the command spells it, where
// that spelling starts at ~: the target itself, or a relative one joined to
// where the walk was, while that still starts at ~. Only aboveDir reads it:
// a rule that joined relative words to it would read `cd ~ && rm
// .sdlc/state/active`, refused by name today, as a file in the home.
func movedFrom(from string, lost bool, words []string) string {
	args := skipOptions(words[1:])
	if len(args) == 0 {
		return ""
	}
	to := clean(args[0])
	switch {
	case strings.HasPrefix(to, "~") && !shellDirectory(to):
		return to
	case lost && from != "" && !isAbsolute(to):
		if joined := path.Join(from, to); strings.HasPrefix(joined, "~") {
			return joined
		}
	}
	return ""
}
