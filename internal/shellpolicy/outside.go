package shellpolicy

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The loop governs the repository and nothing else, so a word the hook places
// outside it is let go whatever its name. Placing is the exception and judging
// by name the rule: a word is let go only in a command the hook reads whole,
// from a directory the command is certainly in, and where the disk itself says
// the path is not the repository's. Anything else is judged as it always was.
//
// Reading whole is a list of what is known, not of what is feared. With every
// word placed, and the commands that could make the placing wrong looked for
// by name, `ln` and a `cd -P`, the names were never all of them: `\ln`, `gln`,
// a variable holding `ln`, `cp -a` of a link and `python -c 'os.rename(...)'`
// each led a placed word into the repository.

// plainRunes are what a word of a plain command is written in outside quotes,
// beside letters and digits. Nothing the shell expands but a glob, which
// literal answers for: no `$`, backtick, backslash, `~`, brace or `!`, and no
// `;`, `|`, single `&`, parenthesis or newline, which would make the next
// command run whether or not the one before it did.
const plainRunes = "/._-+,:@=*?[]"

// plainWord is a word of a plain command, without its quotes.
type plainWord struct {
	text string
	// quoted is whether any of it was in quotes, and redirect whether it is
	// where `>` or `>>` sends the output.
	quoted, redirect bool
}

// plainRun is one simple command of a command the hook reads whole, as the
// plain reader read it: its words without their quotes, where its output is
// sent, and the directory it is read from.
type plainRun struct {
	words, redirects []string
	dir              string
}

// plainCommand is a command the hook reads whole, as its simple commands, or
// nil where it is not one: simple commands joined by `&&`, each a program
// plainProgram knows, in words spelled with nothing the shell would change.
// Only in such a command is a word placed.
//
// Each is read from where the plain `cd`s before it leave the shell, which
// `&&` makes certain: the command after one runs only if it succeeded.
func plainCommand(command string, s State) []plainRun {
	if s.PowerShell {
		return nil
	}
	commands, ok := plainWords(command)
	if !ok {
		return nil
	}
	runs := make([]plainRun, 0, len(commands))
	dir := s.Dir
	for _, words := range commands {
		if !plainProgram(words, len(commands) == 1, s) {
			return nil
		}
		run := plainRun{dir: dir}
		for _, w := range words {
			if w.redirect {
				run.redirects = append(run.redirects, w.text)
			} else {
				run.words = append(run.words, w.text)
			}
		}
		runs = append(runs, run)
		if next, moved := changedDir(dir, run.words); moved {
			dir = next
		}
	}
	return runs
}

// readAlike reports whether the plain reader read the command as the walk
// reads it: the same segments, each with the same words sent to the same
// places. The walk splits a command at every separator, in quotes or out of
// them, and its words at every space, so the two can differ; and where they
// do, anywhere in the command, the walk's words are not what the shell will be
// given, and no path of the command is placed.
func (m *memo) readAlike(segments []string) bool {
	if len(m.whole) == 0 || len(m.whole) != len(segments) {
		return false
	}
	ticked := false
	for idx, marked := range segments {
		segment, _, _ := subshellMarks(marked, &ticked)
		words, redirects, _ := parse(segment)
		if !slices.Equal(m.whole[idx].words, words) || !slices.Equal(m.whole[idx].redirects, redirects) {
			return false
		}
	}
	return true
}

// readFrom reports whether the plain reader read segment idx of the command
// from dir, which is where the `cd`s it read itself leave the shell.
func (m *memo) readFrom(idx int, dir string) bool {
	return idx < len(m.whole) && m.whole[idx].dir == dir
}

// plainScan is plainWords part way through a command: the simple commands and
// the words read so far, and the word being read.
type plainScan struct {
	commands [][]plainWord
	words    []plainWord
	word     plainWord
	// text is the word being read, so far. It is made a string once, where the
	// word ends: joined piece by piece, a word of many quoted pieces is copied
	// again for each of them.
	text []byte
	// in is whether a word is being read, and sent whether a `>` is waiting
	// for the word it sends the output to.
	in, sent bool
}

// plainWords is a command as its simple commands, or false where it holds
// anything but words, quotes that only group, `&&`, and `>` or `>>` to a word.
//
// The command is whatever a model wrote, and a panic here would let it through
// unread: every step takes at least one byte and none past the end.
func plainWords(command string) ([][]plainWord, bool) {
	var p plainScan
	for i := 0; i < len(command); {
		n := p.read(command[i:])
		if n == 0 {
			return nil, false
		}
		i += n
	}
	p.end()
	if p.sent || len(p.words) == 0 {
		return nil, false
	}
	return append(p.commands, p.words), true
}

// read takes what rest, which is not empty, starts with, and answers how many
// bytes of it that was: 0 where it is nothing a plain command holds.
func (p *plainScan) read(rest string) int {
	switch c := rest[0]; {
	case c == ' ' || c == '\t':
		p.end()
		return 1
	case c == '\'' || c == '"':
		return p.quote(rest)
	case c == '&':
		return p.join(rest)
	case c == '>':
		return p.send(rest)
	case plainByte(c):
		n := 1
		for n < len(rest) && plainByte(rest[n]) {
			n++
		}
		p.add(rest[:n], false)
		return n
	}
	return 0
}

func plainByte(c byte) bool {
	return isLetter(c) || '0' <= c && c <= '9' || strings.IndexByte(plainRunes, c) >= 0
}

// end closes the word being read, where there is one.
func (p *plainScan) end() {
	if p.in {
		p.word.text = string(p.text)
		p.words = append(p.words, p.word)
	}
	p.word, p.text, p.in = plainWord{}, p.text[:0], false
}

// add puts text on the end of the word being read, which is where a `>`
// before it sends the output.
func (p *plainScan) add(text string, quoted bool) {
	p.text = append(p.text, text...)
	p.word.quoted = p.word.quoted || quoted
	p.word.redirect = p.word.redirect || p.sent
	p.in, p.sent = true, false
}

// quote takes a quoted stretch that only groups: closed, printable, with no
// backslash and no quote of the other kind in it, and in double quotes with
// nothing the shell fills in or takes out.
//
// In single quotes a backslash is a letter of the name to the shell, and to
// the rules that judge by name it is a separator. Taken here, `echo x >
// '/tmp/out/h\x_test.go'` had the disk asked about `h/x_test.go` in a
// directory `h` that was not there, and the file the shell writes is
// `h\x_test.go`, which can be a link to a frozen test. A quote of the other
// kind those rules take out of the word, where the shell leaves it in the name.
//
// What else they read differently inside quotes is not refused here but by
// readAlike, which finds the two readings to differ: a space, `;`, `&`, `|`,
// `<`, `>`, `)` and a backtick. An opening parenthesis they read as this does.
func (p *plainScan) quote(rest string) int {
	n := strings.IndexByte(rest[1:], rest[0])
	if n < 0 {
		return 0
	}
	body := rest[1 : 1+n]
	if !printable(body) || strings.ContainsAny(body, "\\'\"") || rest[0] == '"' && strings.ContainsAny(body, "$`!") {
		return 0
	}
	p.add(body, true)
	return n + 2
}

// join takes the `&&` between two simple commands. A single `&` runs what
// follows whether or not the command before it succeeded.
func (p *plainScan) join(rest string) int {
	p.end()
	if p.sent || len(p.words) == 0 || len(rest) < 2 || rest[1] != '&' {
		return 0
	}
	p.commands, p.words = append(p.commands, p.words), nil
	return 2
}

// send takes a `>` or `>>` that stands apart from the word before it. Against
// a word it may be a descriptor's, as in `2>`, and `>&` and `>|` are not read.
func (p *plainScan) send(rest string) int {
	if p.in || p.sent {
		return 0
	}
	p.sent = true
	if len(rest) > 1 && rest[1] == '>' {
		return 2
	}
	return 1
}

func printable(text string) bool {
	for i := range len(text) {
		if text[i] < ' ' || text[i] > '~' {
			return false
		}
	}
	return true
}

// plainProgram reports whether a simple command is a program whose every word
// the hook knows the use of: what it writes, and that it makes no link, moves
// none, and leaves the shell where it was. alone is whether it is the whole
// command.
//
// The list is closed, and so are the options: a program not on it, or an option
// not named here, is not known to be harmless, and the command is judged by name.
func plainProgram(words []plainWord, alone bool, s State) bool {
	if len(words) == 0 || words[0].quoted || words[0].redirect {
		return false
	}
	args, ok := plainOperands(words[1:])
	if !ok {
		return false
	}
	switch words[0].text {
	case "echo", "printf":
		return true
	case "rm":
		// Without -r, it removes names and follows nothing.
		return onlyOptions(args, "-f")
	case "touch", "tee":
		return onlyOptions(args)
	case "cp":
		return plainCopy(args)
	case "mv":
		// A move can put a link where a later word is read: alone, there is
		// no later word.
		return alone && onlyOptions(args)
	case "sed":
		return plainSed(args)
	case "find":
		return plainFind(args)
	case "cd":
		return len(args) == 1 && certainMove(args[0], s)
	}
	return false
}

// plainOperands are the words after the program that are not where its output
// is sent, or false where any of them starts with an unquoted `=`: zsh reads
// `=name` as the path of the program called name.
func plainOperands(words []plainWord) ([]string, bool) {
	var args []string
	for _, w := range words {
		if !w.quoted && strings.HasPrefix(w.text, "=") {
			return nil, false
		}
		if !w.redirect {
			args = append(args, w.text)
		}
	}
	return args, true
}

// plainCopy reports whether cp's words are ones it is read with. Without -a,
// -P, -R, -l or -s it copies what a link leads to, and makes none. `-t` is read
// only in the one spelling every cp that has it shares.
func plainCopy(args []string) bool {
	if len(args) > 2 && args[0] == "-t" {
		return onlyOptions(args[1:])
	}
	return onlyOptions(args, "-p")
}

// plainSed reports whether sed's words are `-i`, a substitution and files. BSD
// sed reads the word after `-i` as a backup suffix and the next as its script,
// where `w` writes the file the rest of the word names.
func plainSed(args []string) bool {
	return len(args) > 2 && args[0] == "-i" && sedSubstitution(args[1]) &&
		onlyOptions(args[2:]) && !strings.ContainsAny(args[2], "wW")
}

// sedSubstitution reports whether a script is the one `sed -i` is read with:
// `s/x/y/` and nothing after it but `g`. `w` after it writes a file, and `e`
// runs a command. Not a pattern compiled when the package loads: that is paid
// on every tool call.
func sedSubstitution(script string) bool {
	parts := strings.Split(script, "/")
	return len(parts) == 4 && parts[0] == "s" && (parts[3] == "" || parts[3] == "g")
}

// plainFind reports whether find's words are where it starts, one -name or
// -path, and -delete. Without -L, -H or -follow it stays below where it starts,
// and -delete removes names.
func plainFind(args []string) bool {
	n := len(args)
	return n > 3 && onlyOptions(args[:n-3]) && (args[n-3] == "-name" || args[n-3] == "-path") &&
		args[n-1] == "-delete"
}

// onlyOptions reports whether every word that starts with a dash is one of known.
func onlyOptions(args []string, known ...string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !slices.Contains(known, a) {
			return false
		}
	}
	return true
}

// certainMove reports whether `cd to` leaves the shell in the directory its
// word names, once it has succeeded, which `&&` sees to. Not `-` or a stack
// entry; not through `..`, which the shell takes off the name and the disk off
// the directory, and a link makes those two places; not to a glob, which names
// whichever directory it matches; and not a relative name where CDPATH is
// carried, which looks for it somewhere else first.
func certainMove(to string, s State) bool {
	if to == "" || to[0] == '-' || to[0] == '+' || !literal(to, false) {
		return false
	}
	return isAbsolute(to) || s.Carried == nil || s.Carried("CDPATH") == ""
}

func hasDotDot(p string) bool {
	return slices.Contains(strings.Split(p, "/"), "..")
}

// literal reports whether the text of a path is where it leads: nothing the
// shell fills in, no `..`, no glob or brace list, and no backslash, which is a
// letter of a name to the shell and a separator to clean. lastGlob lets a glob
// stand in the last name, for what a command only removes: whatever it
// matches is a name in the directory before it.
func literal(p string, lastGlob bool) bool {
	if strings.ContainsAny(p, "$%`~{\\") || hasDotDot(p) {
		return false
	}
	if lastGlob {
		p = p[:strings.LastIndexByte(p, '/')+1]
	}
	return !strings.ContainsAny(p, "*?[")
}

// placed reports whether the hook places a path, absolute and literal, outside
// the repository: the lookup puts it there, it is in no other checkout, and
// the disk, where the caller has one to ask, agrees. below asks it of
// everything under the path as well, which only the disk can answer.
func (m *memo) placed(s State, p string, below bool) bool {
	if s.Resolve == nil || m.resolve(s, p) != "" || len(m.shared(s, p)) > 0 {
		return false
	}
	if s.Outside == nil {
		return !below
	}
	return s.Outside(p, below)
}

// outside reports whether a word of the command is a path placed outside the
// repository, read from where the command is. removal is whether the command
// only removes it.
func (q pathQuestion) outside(word string, removal, below bool) bool {
	// literal is asked of the word as the command spells it. Asked of what
	// clean makes of it, a backslash was a separator by then, and the path the
	// disk was asked about was not the one the shell writes.
	c := clean(word)
	if !q.certain || c == "" || !literal(word, removal) {
		return false
	}
	if !isAbsolute(c) {
		// In the repository, a relative name is the repository's. And joined
		// to a directory with `..` in it, the name would lose the `..` where
		// the disk takes it off the directory.
		if !isAbsolute(q.dir) || hasDotDot(q.dir) {
			return false
		}
		c = path.Join(q.dir, c)
	}
	if removal && strings.ContainsAny(c, "*?[") {
		c = path.Dir(c)
	}
	return q.m.placed(q.s, c, below)
}

// copyRead is how kept reads what a plain cp or mv is given to copy.
type copyRead int

const (
	// eachWord asks about every word by itself.
	eachWord copyRead = iota
	// byLanding asks about a source placed outside by where its copy lands.
	byLanding
	// byLandingOrRead does as well, and lets go what a cp that lands outside
	// copies from: the freeze does not refuse reading a frozen test.
	byLandingOrRead
)

// kept is words without those placed outside the repository.
//
// What a cp or mv copies from outside is judged by where it lands, whether or
// not the name it is given looks like a directory's. Dropped without that
// asked, `cp CLAUDE.md /work/my.app` from a directory outside the repository
// was a word outside it and a destination that named no protected path, and
// the contract was overwritten.
//
// Where the destination is not a literal path, where a copy lands is not in
// the text, and what is copied is judged by the name it is given, as the
// destination is. Worked out from the text, `..` came off the name where the
// disk takes it off the directory: `cp x_test.go /tmp/link/..`, with the link
// leading into the repository, was a copy that landed in /tmp, and it lands in
// the repository.
func (q pathQuestion) kept(words []string, removal bool, read copyRead) []string {
	// The walk can leave a segment no program and still a place its output is
	// sent, as it does the end of `echo '$(' > f`. Read for its first word,
	// that was a panic, and a panic in the hook lets the command through.
	if !q.certain || len(words) == 0 || len(q.run.words) == 0 {
		return words
	}
	from, to, reads := q.copies(read)
	// Where an outside source lands is asked last, after the words the command
	// was given: the destination, where it is a frozen test, is what is refused.
	var out, landed []string
	for _, w := range words {
		source := slices.Contains(from, w)
		switch {
		case reads && source:
		case !q.outside(w, removal, false) || source && !literal(to, false):
			out = append(out, w)
		case source && !q.landsOutside(w, to):
			landed = append(landed, lands(w, to))
		}
	}
	return append(out, landed...)
}

// copies is what a cp or mv takes from and where it puts it, where read asks
// for either, and whether it is a cp that only reads what it takes from: one
// that lands outside the repository, asked for the freeze.
func (q pathQuestion) copies(read copyRead) (from []string, to string, reads bool) {
	if read == eachWord || len(q.run.words) == 0 || q.run.words[0] != "cp" && q.run.words[0] != "mv" {
		return nil, "", false
	}
	from, to, _ = moveArguments(q.run.words[1:])
	reads = read == byLandingOrRead && q.run.words[0] == "cp" && q.outside(to, false, false)
	for _, f := range from {
		reads = reads && q.landsOutside(f, to)
	}
	return from, to, reads
}

// landsOutside reports whether the copy a cp or mv makes of source in to is
// placed outside the repository. The text says where it lands only where to is
// a literal path, and what it is called there only where source has no
// backslash, which clean reads as the start of its last name.
func (q pathQuestion) landsOutside(source, to string) bool {
	return !strings.Contains(source, `\`) && literal(to, false) && q.outside(lands(source, to), false, false)
}

// lands is the name a copy of source has in the directory to, a literal path.
func lands(source, to string) string {
	return path.Join(clean(to), path.Base(clean(source)))
}

// blindCopy is where a cp or mv puts word, where that is not a literal path:
// kept then leaves the word to be judged by its name.
func (q pathQuestion) blindCopy(word string) (string, bool) {
	from, to, _ := q.copies(byLanding)
	return to, slices.Contains(from, word) && !literal(to, false)
}

// findsOutside reports whether the command is a find that starts only in
// directories placed outside the repository with nothing of it below them.
// find does not leave where it starts.
func (q pathQuestion) findsOutside() bool {
	if !q.finds() {
		return false
	}
	roots, _ := findRoots(q.run.words[1:])
	return len(roots) > 0 && !slices.ContainsFunc(roots, func(r string) bool { return !q.outside(r, false, true) })
}

func (q pathQuestion) finds() bool {
	return len(q.run.words) > 0 && base(q.run.words[0]) == "find"
}

// maxShown bounds a word of the command a refusal repeats.
const maxShown = 1024

// seePage ends a route that turns on where a path was placed with where that
// is set out: what makes a command plain is more than a refusal has room for.
const seePage = "; see docs/enforcement.md, under A path outside the repository"

// macLinks goes with the link a refusal gives as its cause. On macOS a path
// under /tmp is the commonest one outside the repository and is through a
// link nobody made, so the refusal read as a fault in the hook.
const macLinks = " (on macOS `/tmp` and `/var` are links: write `/private/tmp/...` or `/private/var/...`)"

// quoted is a word of the command as it is written, bounded. A substitution in
// it is only a marker by now, so the command it is in is quoted beside it.
func (q pathQuestion) quoted(word string) string {
	if !strings.Contains(word, "$SUB") {
		return shown(bounded(word))
	}
	return shown(bounded(word)) + " in `" + bounded(q.line) + "`"
}

func bounded(word string) string {
	if len(word) > maxShown {
		word = strings.ToValidUTF8(word[:maxShown], "") + "..."
	}
	return word
}

// graphic is a name read from the disk with every rune that is not
// graphic escaped, as strconv.QuoteToGraphic escapes it: a directory entry or
// an archive member is named by whoever made it, and one holding a newline
// or a right-to-left mark would have written what it liked into a refusal
// the model and the person read. It runs before bounded, so that an escape is
// never cut in half.
func graphic(name string) string {
	if !strings.ContainsFunc(name, func(r rune) bool { return !unicode.IsGraphic(r) || r == utf8.RuneError }) {
		return name
	}
	q := strconv.QuoteToGraphic(name)
	return q[1 : len(q)-1]
}

// explained is a frozen-test refusal with it said, where the refusal does not
// rest on a path plainly in the repository, which word or which find's start
// the hook read as the repository's, and what it measured it against.
func (q pathQuestion) explained(f Finding, word, frozen string) Finding {
	at := q.at()
	if slices.Contains(q.reached, frozen) {
		roots, _ := findRoots(q.run.words[1:])
		root := roots[0]
		for _, r := range roots {
			if !q.outside(r, false, true) {
				root = r
				break
			}
		}
		f.Reason += ". The find that would change it starts at " + q.quoted(root)
		switch c := clean(root); {
		case !literal(c, false) || q.lost && !isAbsolute(c):
			f.Reason += ", and the hook could not tell where that leads, so it treated it as the repository" + at
		case q.inside(c):
			f.Reason += ", in the repository" + at
		default:
			f.Reason += ", which the hook could not place outside the repository" + at + q.unplaced(root, true)
		}
		f.Route += "; if the files to change are outside the repository, start the find at their directory, " +
			"written out in full, with no variable, glob, brace list or `..` in it, and keep the command one " +
			"the hook reads whole: `find`, the directory, one `-name` or `-path` with its pattern, and " +
			"`-delete`, on its own or after `cd` and `&&`" + seePage
		return f
	}
	if !literal(clean(word), false) {
		return q.untold(f, word)
	}
	return q.unplacedWord(f, word)
}

// untold is a refusal of a word the hook cannot read as a path with it said
// that the hook could not tell where the word leads, and how to write it so
// that it can.
func (q pathQuestion) untold(f Finding, word string) Finding {
	f.Reason += ". The hook could not tell where " + q.quoted(word) + " leads, so it treated it as that file " +
		"in the repository" + q.at()
	f.Route += "; if the file is outside the repository, write its path out in full, with no variable, " +
		"glob, brace list or `..` in it" + seePage
	return f
}

// unplacedWord is a refusal of a word with it said, where the word is not
// plainly the repository's, that the hook did not place it outside, why, and
// how to write the command so that it does.
//
// The freeze's refusal shares it with the two on a test file that is not
// frozen. Those two kept the words they always had: a scratch file outside the
// repository, named in a command with a `;` or a here-document in it, was a
// new test of the project's to stop and ask an unfreeze for, when the Write
// tool refuses the same path as outside.
//
// A relative word read in the repository keeps those words whatever is in it.
// One that starts with a variable or a tilde is not relative: where it leads
// is the shell's to say. And the pattern a find looks for is no path to write
// out in full.
func (q pathQuestion) unplacedWord(f Finding, word string) Finding {
	c := clean(word)
	if filled := strings.IndexAny(c, "$%~`") == 0; !filled && q.inside(c) {
		return f
	}
	if !literal(c, false) {
		if q.finds() {
			return f
		}
		return q.untold(f, word)
	}
	f.Reason += ". The hook could not place " + q.quoted(word) + " outside the repository" + q.at() +
		", so it treated it as that file" + q.unplaced(word, false)
	f.Route += "; if the file is outside the repository, name it in a command the hook reads whole: a plain " +
		"`rm`, `cp`, `mv`, `touch`, `tee`, `sed -i`, `echo`, `printf` or `find ... -delete`, on its own or " +
		"after `cd` and `&&`, with the path written out in full and no link on the way to it" + seePage
	return f
}

// at is the checkout a word was measured against, for a refusal to name, or
// nothing where the caller did not say which it is.
func (q pathQuestion) at() string {
	if q.s.Root == "" {
		return ""
	}
	return " at " + q.s.Root
}

// unplaced is why a path written out in full was not placed outside the
// repository, for the refusal to say. "Could not place" alone reads as a fault
// in the hook where the path is plainly outside, and what stopped it is as
// often somewhere else in the command as in the path. below is whether it was
// asked of everything under the path, as it is of where a find starts.
func (q pathQuestion) unplaced(word string, below bool) string {
	switch to, blind := q.blindCopy(word); {
	case !q.certain:
		return ": it places a path only in a command it reads whole, and a `;`, a pipe, a variable, a " +
			"backslash, a here-document, `2>&1`, a space inside quotes, or a program or an option not on its " +
			"list, anywhere in the command, stops it placing a path in it"
	case below:
		return ": on disk it is not there, or there is a link on the way to it" + macLinks + ", a checkout of " +
			"the repository in it, or more in it than the hook looks through"
	case blind && q.outside(word, false, false):
		// The word itself is placed, and the disk has nothing against it.
		return ": the command copies or moves it to " + q.quoted(to) + ", and the hook could not tell where " +
			"that leads"
	}
	return ": on disk there is a link on the way to it" + macLinks + ", a second name for the file, or another " +
		"checkout of the repository around it"
}

// inside reports whether a path is plainly the repository's: a relative name
// read in it, or a path the lookup puts in it.
func (q pathQuestion) inside(c string) bool {
	if !isAbsolute(c) {
		if q.lost || !isAbsolute(q.dir) {
			return !q.lost
		}
		c = path.Join(q.dir, c)
	}
	return q.s.Resolve != nil && q.m.resolve(q.s, c) != ""
}
