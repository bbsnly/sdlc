package shellpolicy

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// The claim on a story names the checkout it is held in, so a session that
// takes that checkout away, or moves it, holds nothing afterwards and is held
// to nothing. `mv .` was the one spelling of it that was refused. The shell has
// other names for where it is -- $PWD, ~+, `pwd`, and $OLDPWD or ~- for where
// it was -- and `..`, a glob such as `../*`, `rsync --delete`, find and a
// program handed to an interpreter reach the checkout, or a directory holding
// it, without naming it at all. Another story's worktree, and the main
// checkout, are someone else's work and hold the story records.

// CheckoutSet is where the repository's checkouts are on disk, as the hook
// finds them. The package reads nothing from the disk itself.
type CheckoutSet struct {
	// Checkouts are every checkout of the repository, absolute and
	// slash-separated, with their links resolved: the one the story is held
	// in first.
	Checkouts []string
	// Place is where a word of a command lands on disk: absolute,
	// slash-separated, with the links on the way followed and `~` read as the
	// home it names, and relative to the project where it is not absolute.
	// There can be two answers, where following a link first and taking `..`
	// apart by name first disagree; nil is somewhere that cannot be known.
	Place func(word string) []string
}

// The checkouts a word can name, in the order a refusal prefers them.
const (
	holdingOne = iota
	anotherCheckout
	thisCheckout
)

// checkoutTree is a CheckoutSet as a tree of the checkouts and every directory
// above one, each by its path folded as the filesystem folds it.
type checkoutTree struct {
	nodes map[string]*checkoutNode
	held  string
	place func(word string) []string
}

type checkoutNode struct {
	key, path string
	parent    string
	children  map[string]string // a child's name, folded, to its key
	kind      int
}

func newCheckoutTree(set CheckoutSet) *checkoutTree {
	t := &checkoutTree{nodes: map[string]*checkoutNode{}, place: set.Place}
	for i, c := range set.Checkouts {
		p := path.Clean(c)
		n := t.node(p)
		if i == 0 {
			n.kind, t.held = thisCheckout, n.key
		} else {
			n.kind = max(n.kind, anotherCheckout)
		}
		for {
			up := path.Dir(p)
			if up == p || up == "." {
				break
			}
			parent := t.node(up)
			n.parent = parent.key
			parent.children[pathrules.Fold(path.Base(p))] = n.key
			p, n = up, parent
		}
	}
	return t
}

// node is the directory p of the tree, made where it is not there yet.
func (t *checkoutTree) node(p string) *checkoutNode {
	key := pathrules.Fold(p)
	n, ok := t.nodes[key]
	if !ok {
		n = &checkoutNode{key: key, path: p, parent: key, children: map[string]string{}, kind: holdingOne}
		t.nodes[key] = n
	}
	return n
}

// maxStates bounds the places a glob is followed to. `.*` is `.`, `..` and
// every dotted name at once, and a word of them is thousands of places; past
// the bound the word is taken to reach a checkout.
const maxStates = 256

// globState is where a glob has come to: a directory of the tree, or off
// levels below one, in a directory the tree does not have.
type globState struct {
	node string
	off  int
}

// reach follows a glob's segments from start through the tree, and returns the
// directory of the tree it can end at that a refusal would name first, or
// false where it can end at none.
func (t *checkoutTree) reach(start string, segments []string) (*checkoutNode, bool) {
	states := map[globState]bool{t.at(start): true}
	for _, seg := range segments {
		next := map[globState]bool{}
		for st := range states {
			t.step(st, seg, next)
		}
		if len(next) > maxStates {
			return t.nodes[t.held], true
		}
		states = next
	}
	var best *checkoutNode
	for st := range states {
		if n := t.nodes[st.node]; st.off == 0 && n != nil && prefers(n, best) {
			best = n
		}
	}
	return best, best != nil
}

// prefers reports whether a refusal names n rather than best: the kind it
// prefers, and between two of a kind the first by path, so that a refusal read
// from a map names the same checkout every time.
func prefers(n, best *checkoutNode) bool {
	return best == nil || n.kind > best.kind || n.kind == best.kind && n.path < best.path
}

// at is where a directory on disk is in the tree.
func (t *checkoutTree) at(dir string) globState {
	off := 0
	// Fold once and walk up the folded string. Fold works on each path
	// component and keeps every separator, so the folded parents are the
	// parents of the folded path; folding the whole path again at each level
	// had cost time growing as the square of a deep path's depth.
	for p := pathrules.Fold(path.Clean(dir)); ; off++ {
		if _, ok := t.nodes[p]; ok {
			return globState{p, off}
		}
		up := path.Dir(p)
		if up == p || up == "." {
			// Not under any directory the tree has, which a tree with a root
			// has none of.
			return globState{"", off + 1}
		}
		p = up
	}
}

// step adds to next where one segment of a glob can take st.
func (t *checkoutTree) step(st globState, seg string, next map[globState]bool) {
	glob := strings.ContainsAny(seg, "*?[")
	switch {
	case seg == "" || seg == ".":
		next[st] = true
		return
	case seg == "..":
		t.up(st, next)
		return
	case !glob:
		if n := t.nodes[st.node]; st.off == 0 && n != nil {
			if child, ok := n.children[pathrules.Fold(seg)]; ok {
				next[globState{child, 0}] = true
				return
			}
		}
		next[globState{st.node, st.off + 1}] = true
		return
	}
	// A glob can name any entry the tree does not know of, as well as those it
	// does. `.*` is also `.` and `..`, as bash before 5.2 and some other
	// shells expand it.
	next[globState{st.node, st.off + 1}] = true
	if segmentMatches(seg, ".") {
		next[st] = true
	}
	if segmentMatches(seg, "..") {
		t.up(st, next)
	}
	if n := t.nodes[st.node]; st.off == 0 && n != nil {
		for name, child := range n.children {
			if segmentMatches(seg, name) {
				next[globState{child, 0}] = true
			}
		}
	}
}

func (t *checkoutTree) up(st globState, next map[globState]bool) {
	if st.off > 0 {
		next[globState{st.node, st.off - 1}] = true
		return
	}
	if n := t.nodes[st.node]; n != nil {
		next[globState{n.parent, 0}] = true
	}
}

// lexically is what a word names relative to the checkout the command started
// in, read by its name alone: the checkout itself where it cleans to `.`, a
// directory holding it where it cleans to `..` or further up, and a glob that
// can come back to either, as `../*` can. levels is how many directories above
// the checkout it can name, the checkout itself being 0.
func lexically(word string) (levels int, ok bool) {
	// Where the word can be: k levels above the checkout on the way to it,
	// and off levels down from there in a directory off that way.
	type at struct{ k, off int }
	states := map[at]bool{{0, 0}: true}
	for _, seg := range strings.Split(word, "/") {
		next := map[at]bool{}
		for st := range states {
			up := func() {
				if st.off > 0 {
					next[at{st.k, st.off - 1}] = true
				} else {
					next[at{st.k + 1, 0}] = true
				}
			}
			switch {
			case seg == "" || seg == ".":
				next[st] = true
			case seg == "..":
				up()
			case !strings.ContainsAny(seg, "*?["):
				next[at{st.k, st.off + 1}] = true
			default:
				next[at{st.k, st.off + 1}] = true
				if segmentMatches(seg, ".") {
					next[st] = true
				}
				if segmentMatches(seg, "..") {
					up()
				}
				// A segment that matches every name matches the one on the
				// way back down.
				if st.off == 0 && st.k > 0 && everyName(seg) {
					next[at{st.k - 1, 0}] = true
				}
			}
		}
		if len(next) > maxStates {
			return 0, true
		}
		states = next
	}
	levels, ok = -1, false
	for st := range states {
		if st.off == 0 && (!ok || st.k < levels) {
			levels, ok = st.k, true
		}
	}
	return levels, ok
}

// throughDotDot is a glob with each segment that can match `..` written as
// `..`, and whether it had one: `build/.*` can match `build/..`.
func throughDotDot(word string) (string, bool) {
	segments := strings.Split(word, "/")
	found := false
	for i, seg := range segments {
		if strings.ContainsAny(seg, "*?[") && segmentMatches(seg, "..") {
			segments[i], found = "..", true
		}
	}
	return strings.Join(segments, "/"), found
}

// backThroughDotDot reports whether a command takes away a glob that can match
// `..`.
func backThroughDotDot(words []string) bool {
	for _, w := range takenAway(words) {
		if _, ok := throughDotDot(unquote(w)); ok {
			return true
		}
	}
	return false
}

// backUp is how a glob comes back up to what it could take away, where that is
// through `..` and it is levels above the checkout from dir, or "". Said
// without it, `rm -rf build/.*` was taking away the checkout, and nothing said
// how.
func backUp(word, dir string, levels int) string {
	dotted, ok := throughDotDot(word)
	if !ok || strings.ContainsAny(dotted, "*?[{") {
		return ""
	}
	if back, ok := lexically(path.Join(dir, dotted)); !ok || back != levels {
		return ""
	}
	return dotted
}

// everyName reports whether a glob segment matches every name that does not
// start with a dot: `*`, `?*`, `**`.
func everyName(seg string) bool {
	return strings.Trim(seg, "*?") == "" && strings.Contains(seg, "*")
}

// takeAway is the checkouts a command could take away, followed from where the
// command is.
type takeAway struct {
	s    State
	m    *memo
	dir  string
	lost bool
	// pwd is where the shell is, which $PWD names: dir, unless env -C runs
	// the command somewhere else, where the shell has already put its $PWD
	// in the words.
	pwd     string
	pwdLost bool
	// old is where the shell was before its last `cd`, or unknownDir.
	old string
	// from is all of the above, as the memo keys what has been looked at
	// from there; checkTakesACheckout sets it.
	from string
}

// unknownDir is a directory the command does not show.
const unknownDir = "\x00"

// checkTakesACheckout refuses a command that takes away a checkout of the
// repository, or a directory holding one.
func checkTakesACheckout(line, segment string, run invocation, t takeAway) (Finding, bool) {
	words := run.words
	if len(words) == 0 {
		return Finding{}, false
	}
	t.from = t.dir + "\x00" + t.pwd + "\x00" + t.old + "\x00" + strconv.FormatBool(t.lost) + strconv.FormatBool(t.pwdLost)
	removed := takenAway(words)
	// find's own roots are find's to judge, below, with its filters: taken
	// away whole, `find .. -empty -delete` was the directory holding the
	// checkout, which -empty never removes.
	if base(words[0]) != "find" {
		for _, w := range removed {
			if f, ok := t.word(w, false, ""); ok {
				return f, true
			}
		}
	}
	if dest, ok := rsyncDeletes(words); ok {
		if f, ok := t.word(dest, false, "rsync"); ok {
			return f, true
		}
	}
	if base(words[0]) == "find" && (hasWord(words[1:], "-delete") || findRemoves(words[1:])) {
		if f, ok := t.find(words[1:]); ok {
			return f, true
		}
	}
	// A path handed to a remover in another segment, `echo <path> | xargs rm
	// -rf`, is found among the words of the whole line, relative ones as much
	// as absolute: read as a program's are, `echo ../B-2 | xargs rm -rf` took
	// another story's worktree away. A word ending in `.` or `..` is one rm and
	// mv refuse, and `find . -name '*.o' | xargs rm -rf` is an ordinary clean.
	// The line is read once from each directory, and each word in it once: a
	// hundred `echo ... | xargs rm -rf` in one command read it a hundred times,
	// and took 3.3 s.
	if run.piped && len(removed) > 0 && t.m.first("piped remover", t.from) {
		seen := map[string]bool{}
		for _, w := range loosePaths(line) {
			if seen[w] {
				continue
			}
			seen[w] = true
			if end := path.Base(clean(unquote(w))); end == "." || end == ".." {
				continue
			}
			if f, ok := t.word(w, false, ""); ok {
				return f, true
			}
		}
	}
	// Inside a program, only what names a place wherever it stands counts: a
	// bare `.` in a program is not the checkout, and `os.listdir(".")` is not a
	// removal.
	inline := runsInlineCode(words, segment) || t.s.PowerShell && ioCall.MatchString(segment)
	if inline && removesInCode(line) {
		for _, w := range loosePaths(line) {
			if f, ok := t.word(w, true, ""); ok {
				return f, true
			}
		}
	}
	return Finding{}, false
}

// word refuses a word that names a checkout or a directory holding one, as
// the command gives it. loose is a word from a line or a program, which counts
// only where it names a place wherever it is: an absolute path, `~`, or one of
// the shell's names for where it is. how is "rsync" for the destination of
// `rsync --delete`.
func (t takeAway) word(w string, loose bool, how string) (Finding, bool) {
	w = unquote(w)
	if strings.HasPrefix(w, "-") {
		return Finding{}, false
	}
	// Each word is looked at once from where the shell is: the same fifty
	// words in a hundred `rm -rf` were placed and folded a hundred times, and
	// the allocations alone made the command half as slow again as released.
	if !t.m.first("taken away "+how+" "+strconv.FormatBool(loose), t.from+"\x00"+w) {
		return Finding{}, false
	}
	named, known, shellName := t.shellNames(w)
	if !known {
		return takenFinding(w, "", thisCheckout, true, how), true
	}
	c := clean(named)
	if loose && !shellName && !isAbsolute(c) && !strings.HasPrefix(c, "~") {
		return Finding{}, false
	}
	if loose && !shellName && (path.Base(c) == "." || path.Base(c) == "..") {
		// rm and mv refuse those. The shell's names for where it is are the
		// directory itself: `[IO.Directory]::Delete($PWD, $true)` removes it.
		return Finding{}, false
	}
	if c == "" || strings.ContainsAny(c, "$`%") {
		return Finding{}, false
	}
	joined := c
	if !isAbsolute(c) && !strings.HasPrefix(c, "~") && !shellName {
		if t.lost {
			return Finding{}, false
		}
		joined = path.Join(t.dir, c)
	}
	words, done := expandedBraces(joined)
	if !done {
		return takenFinding(w, "", thisCheckout, true, how), true
	}
	for _, e := range words {
		glob := strings.ContainsAny(e, "*?[")
		if !isAbsolute(e) && !strings.HasPrefix(e, "~") && !t.lost {
			if levels, ok := lexically(e); ok {
				kind := thisCheckout
				if levels > 0 {
					kind = holdingOne
				}
				return takenFinding(w, backUp(c, t.dir, levels), kind, glob, how), true
			}
		}
		tree := t.m.checkouts(t.s)
		if tree == nil {
			continue
		}
		if n, ok := tree.reaches(e, glob, t.m); ok {
			return takenFinding(w, n.path, n.kind, glob, how), true
		}
	}
	return Finding{}, false
}

// shellNames is a word with the shell's names for where it is, and where it
// was, put in: `$PWD`, `${PWD}` and `~+` are the directory the shell is in,
// and `$OLDPWD`, `${OLDPWD}` and `~-` the one before its last `cd`. known is
// false where that is somewhere the command does not show.
func (t takeAway) shellNames(w string) (named string, known, shellName bool) {
	for _, n := range []struct {
		names []string
		dir   string
		lost  bool
	}{
		{[]string{"$PWD", "${PWD}", "~+"}, t.pwd, t.pwdLost},
		{[]string{"$OLDPWD", "${OLDPWD}", "~-"}, t.old, false},
	} {
		for _, name := range n.names {
			rest, ok := cutName(w, name, t.s.PowerShell)
			if !ok {
				continue
			}
			if n.dir == unknownDir || n.lost {
				return w, false, true
			}
			dir := n.dir
			if dir == "" {
				dir = "."
			}
			return dir + rest, true, true
		}
	}
	return w, true, false
}

// cutName cuts a shell name for a directory off the front of a word, where the
// word is that directory or something in it. PowerShell's $PWD is any case.
func cutName(w, name string, powerShell bool) (string, bool) {
	head := w
	if len(head) > len(name) {
		head = head[:len(name)]
	}
	folds := powerShell && strings.HasPrefix(name, "$") && strings.EqualFold(head, name)
	if head != name && !folds {
		return "", false
	}
	rest := w[len(head):]
	if rest != "" && rest[0] != '/' && rest[0] != '\\' {
		return "", false
	}
	return rest, true
}

// expandedBraces is a word's brace lists expanded, and false where there were
// more than the rules expand: such a word is taken to reach a checkout.
func expandedBraces(word string) ([]string, bool) {
	words := braces(word)
	for _, w := range words {
		if open, _, _ := braceGroup(w); open >= 0 {
			return nil, false
		}
	}
	return words, true
}

// reaches is the directory of the tree a word, literal or a glob, can name.
func (t *checkoutTree) reaches(word string, glob bool, m *memo) (*checkoutNode, bool) {
	if !glob {
		var best *checkoutNode
		for _, p := range m.place(t, word) {
			if n, ok := t.nodes[pathrules.Fold(path.Clean(p))]; ok && (best == nil || n.kind > best.kind) {
				best = n
			}
		}
		return best, best != nil
	}
	// A glob is placed up to its first segment with a pattern in it, which is
	// a directory the filesystem can say where it is, and followed through the
	// tree from there.
	segments := strings.Split(word, "/")
	first := 0
	for first < len(segments) && !strings.ContainsAny(segments[first], "*?[") {
		first++
	}
	prefix := strings.Join(segments[:first], "/")
	switch {
	case prefix == "" && isAbsolute(word):
		prefix = "/"
	case prefix == "":
		prefix = "."
	}
	var best *checkoutNode
	for _, p := range m.place(t, prefix) {
		if n, ok := t.reach(p, segments[first:]); ok && (best == nil || n.kind > best.kind) {
			best = n
		}
	}
	return best, best != nil
}

// find refuses find removing a checkout: a directory it starts from, other than
// `.`, which find never removes, where the directory passes its filters; or,
// where the tree is known, a checkout or a directory holding one below it
// whose name passes them. -empty passes nothing that holds a checkout, and a
// -type without d nothing that is a directory.
//
// A glob that cannot be read passes: it says nothing of what it picks. Read as
// picking nothing, a glob longer than maxGlob in `find .sdlc/worktrees -name
// GLOB -exec rm -rf {} +` would take the checkouts below, which the finds
// before it do not look at.
func (t takeAway) find(args []string) (Finding, bool) {
	roots, names, patterns := findArguments(args)
	nameGlobs, pathGlobs := readGlobs(names), readGlobs(patterns)
	passes := func(name, full string) bool {
		if hasWord(args, "-empty") {
			return false
		}
		for i, a := range args {
			if a == "-type" && i+1 < len(args) && !strings.Contains(args[i+1], "d") {
				return false
			}
		}
		if len(names)+len(patterns) == 0 {
			return true
		}
		for _, g := range nameGlobs {
			if g == nil || g.MatchString(name) {
				return true
			}
		}
		for _, g := range pathGlobs {
			if g == nil || g.MatchString(full) {
				return true
			}
		}
		return false
	}
	least, most := findDepth(args, "-mindepth", 0), findDepth(args, "-maxdepth", -1)
	for _, root := range roots {
		c := clean(root)
		if least == 0 && c != "." && passes(path.Base(c), c) {
			if f, ok := t.word(root, false, ""); ok {
				return f, true
			}
		}
		tree := t.m.checkouts(t.s)
		if tree == nil || strings.ContainsAny(c, "$`%*?[{") {
			continue
		}
		named, known, shellName := t.shellNames(c)
		if !known {
			continue
		}
		if !shellName && !isAbsolute(named) && !strings.HasPrefix(named, "~") {
			if t.lost {
				continue
			}
			named = path.Join(t.dir, named)
		}
		var best *checkoutNode
		for _, p := range t.m.place(tree, named) {
			start := pathrules.Fold(path.Clean(p))
			for key, n := range tree.nodes {
				rel, below := strings.CutPrefix(key, strings.TrimSuffix(start, "/")+"/")
				if !below {
					continue
				}
				depth := strings.Count(rel, "/") + 1
				if depth < least || most >= 0 && depth > most {
					continue
				}
				if passes(path.Base(n.path), c+"/"+rel) && prefers(n, best) {
					best = n
				}
			}
		}
		if best != nil {
			return takenFinding(root, best.path, best.kind, true, ""), true
		}
	}
	return Finding{}, false
}

// findDepth is the value of find's -mindepth or -maxdepth, or def.
func findDepth(args []string, option string, def int) int {
	for i, a := range args {
		if a == option && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				return n
			}
		}
	}
	return def
}

// findRemoves reports whether find runs a remover on what it finds: `-exec rm
// -rf {} +`, `-execdir mv {} ...`.
func findRemoves(args []string) bool {
	for i, a := range args {
		if findRuns(a) {
			if exec := program(args[i+1:]); len(exec.words) > 0 && hasWord(takenAway(exec.words), "{}") {
				return true
			}
		}
	}
	return false
}

// rsyncDeletes is where rsync copies to when it deletes there what the source
// does not have: with --delete, any --delete-* option, or --del.
func rsyncDeletes(words []string) (string, bool) {
	if base(words[0]) != "rsync" {
		return "", false
	}
	deletes := false
	for _, a := range words[1:] {
		if a == "--del" || a == "--delete" || strings.HasPrefix(a, "--delete-") {
			deletes = true
		}
	}
	if !deletes {
		return "", false
	}
	operands := copyOperands(words[1:], rsyncValued)
	if len(operands) < 2 {
		return "", false
	}
	return operands[len(operands)-1], true
}

// removesInCode reports whether a program, or a PowerShell command, calls
// something that removes or moves a directory.
func removesInCode(line string) bool {
	return removers.MatchString(line)
}

// ioCall is PowerShell calling .NET's file system classes, which remove and
// move directories as Remove-Item and Move-Item do: `[IO.Directory]::Delete(`.
var ioCall = regexp.MustCompile(`(?i)\[(system\.)?io\.(directory|file|directoryinfo|fileinfo)\]::`)

var removers = regexp.MustCompile(`(?i)rmtree|remove|rmdir|unlink|rename|\bmove|delete|\brm(_rf?|sync)?\b|rimraf|\breplace\(`)

// loosePaths are the words of a line that could name a place wherever they
// stand, with PowerShell's name for where it is written as the shell's.
func loosePaths(line string) []string {
	line = currentDirectory.ReplaceAllString(line, " $$PWD ")
	return strings.FieldsFunc(line, func(r rune) bool {
		return !inAPath(r) && !strings.ContainsRune("~$+{}*?[]:\\", r)
	})
}

// currentDirectory is PowerShell's call for the directory it runs in. A
// program's own, `os.getcwd()` and its kin, is not read: it is as often the
// start of a path below the directory, `os.path.join(os.getcwd(), "x")`, as the
// directory, and the words around it do not say which.
var currentDirectory = regexp.MustCompile(`(?i)\$?\(Get-Location\)`)

// takenFinding is the refusal of taking away a checkout: named by word, at
// place where the tree says where that is, of a kind; glob where the word is a
// guess at what it matches.
func takenFinding(word, place string, kind int, glob bool, how string) Finding {
	what := map[int]string{
		thisCheckout:    "the checkout this story is held in",
		anotherCheckout: "another checkout of this repository",
		holdingOne:      "a directory holding a checkout of this repository",
	}[kind]
	takes := map[int]string{
		thisCheckout:    "the story's work and loop state",
		anotherCheckout: "its work and loop state",
		holdingOne:      "the checkout's work and loop state",
	}[kind]
	if place != "" {
		what = "`" + place + "`, " + what
	}
	verb := " names "
	if glob {
		verb = " could match "
	}
	reason := "`" + word + "`" + verb + what + ", and taking it away takes " + takes + " with it"
	if how == "rsync" {
		is := " is "
		if kind == holdingOne {
			is = " holds "
		}
		reason = "rsync --delete into `" + word + "` removes what the source lacks, and `" + word + "`" + is +
			what + ", with " + takes
	}
	return Finding{
		Rule:   "loop-state-through-the-tool",
		Reason: reason,
		Route: "remove only what the story made, by naming it inside the checkout, such as `rm -rf build`; " +
			"a checkout is removed by a person, in their own terminal",
	}
}
