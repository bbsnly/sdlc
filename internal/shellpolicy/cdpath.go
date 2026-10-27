package shellpolicy

import (
	"path"
	"slices"
	"strings"
)

// This file reads the CDPATH a relative `cd` searches: what the line, the
// command and the session's environment set it to, and whether the line sets
// it in a way the hook cannot read, which leaves the cd unplaced (lostPlace).

// cdpathUnread is why a relative cd is not placed where the line names
// CDPATH in a way the hook does not read as a value. It says the line may set
// it, not that it does: said of `grep -rn cdpath internal` or a comment, "the
// line sets CDPATH" was false (US-035, verifier round 12).
const cdpathUnread = "the line names CDPATH, which a relative `cd` searches first, and may set it in a way " +
	"the hook does not read"

// cdpathFix is how to let the hook place a cd the line's CDPATH left
// unplaced: an absolute cd is not it, since the cd may be one the person
// meant to search CDPATH (human advocate, design review round 3).
const cdpathFix = "set CDPATH as one plain assignment (`CDPATH=/a:/b`), or leave the name CDPATH out of the command"

// searchesCdpath reports whether a cd to to looks for it under each CDPATH
// entry first: a relative directory that does not start at . or ..
func searchesCdpath(to string) bool {
	return !isAbsolute(to) && to != "." && to != ".." && !strings.HasPrefix(to, "./") && !strings.HasPrefix(to, "../")
}

// cdpathRelative is why a relative cd is not placed where CDPATH has a
// relative entry and the walk is lost, and cdpathRelativeFix how to let the
// hook place it.
const (
	cdpathRelative    = "it searches a relative CDPATH entry from a directory the hook cannot place"
	cdpathRelativeFix = "spell each CDPATH entry as an absolute path, or leave CDPATH out"
	// A relative entry the session's own environment gave: the line sets no
	// CDPATH, so a reason about the line's sent the person looking for one
	// that is not there (US-035, red team, design review round 5).
	cdpathCarriedRelative = "it searches a relative entry of the CDPATH the session started with, from a " +
		"directory the hook cannot place"
	cdpathCarriedFix = "spell each CDPATH entry as an absolute path, or leave CDPATH out, in the environment " +
		"the session starts in"
	cdpathMany = "through CDPATH it could leave the shell in more places than the hook follows"
	// `unset CDPATH;` is not offered: with CDPATH read-only, bash fails the
	// unset and runs the cd after it with CDPATH still set (US-035, human
	// advocate, design review round 6).
	cdpathManyFix = "spell the `cd`s by absolute paths, which search no CDPATH; or leave CDPATH out of the " +
		"command, and ask the person to leave it out of the environment the session starts in, such as their " +
		"shell's startup file"
)

// maxCdpathPlaces is how many places, each counted once, a cd that searches
// CDPATH may leave the walk in before it is read as one that could be
// anywhere. Each relative cd on a lost walk added every entry beside the
// places carried from before, and every segment was judged from all of them:
// `D=x; cd $D; ` and 600 of `CDPATH=/a:/b cd x; ` ran to the hook's deadline
// (US-035, verifier round 12). Past it, a relative write refuses, as after any
// cd that could leave the shell anywhere, and the work each segment does
// stays bounded. 16, not 8: with two absolute entries, 8 was met by the
// fourth nested relative cd (US-035, the person's decision, unfreeze #14).
const maxCdpathPlaces = 16

// searchPath is the CDPATH a `cd` searches: the entries the command sets
// (set), those of the environment the session started in (carried), each
// list with no entry twice, and whether the command sets it in a way the hook
// does not read. values are the whole values the cd could be searching, in
// their own order, for reachedFirst, where the command or the line assigns
// CDPATH; cleared is whether the line unsets CDPATH or assigns it empty.
type searchPath struct {
	set, carried []string
	unread       bool
	values       [][]string
	cleared      bool
}

// reachedFirst reports whether a shell standing in q, a place a lost walk
// could be in, certainly goes elsewhere on a cd to to that searches p, so
// that q/to is no place it can reach and q need not move below. It holds
// where q is E/to for an absolute entry E, and where the search certainly
// reaches E first:
//   - E is in every value the cd could be searching (values): where the cd's
//     own segment assigns CDPATH in front of it, that value alone, since an
//     assignment in front of a builtin replaces the exported value for it;
//     otherwise every value the command and the line assign, and the
//     session's, an unset one holding no entry;
//   - in each, every entry before E is absolute: bash searches in order, and
//     an empty or `.` entry is the shell's own directory, q;
//   - in each, an empty or `.` entry comes after E: zsh tries its own
//     directory before CDPATH unless one is there;
//   - the line neither unsets CDPATH nor assigns it empty.
//
// Then the shell, in E/to, finds E/to and goes there or to an earlier entry's
// directory, each a place the cd adds, and never to q/to. Checked against the
// union of every value, a place was dropped that `CDPATH=/b cd x`, an empty
// or `.` entry first, an `unset CDPATH` between, or zsh, could still reach
// (US-035, architect and security B1, design review round 6). Never on the
// session's value alone: the hook reads its own environment, which may not be
// the shell's (US-035, security review, design review round 7).
func (p searchPath) reachedFirst(q, to string) bool {
	if p.unread || p.cleared || len(p.values) == 0 {
		return false
	}
	for _, e := range p.values[0] {
		if !isAbsolute(e) || path.Join(e, to) != q {
			continue
		}
		every := true
		for _, v := range p.values {
			every = every && searchedInOrderTo(v, e)
		}
		if every {
			return true
		}
	}
	return false
}

// searchedInOrderTo reports whether a search of the entries v certainly
// reaches e before the shell's own directory, in bash and in zsh alike.
func searchedInOrderTo(v []string, e string) bool {
	i := slices.Index(v, e)
	if i < 0 {
		return false
	}
	for _, before := range v[:i] {
		if !isAbsolute(before) {
			return false
		}
	}
	for _, after := range v[i+1:] {
		if after == "" || after == "." {
			return true
		}
	}
	return false
}

// valueEntries is each distinct value's entries, in order; an empty value,
// a CDPATH that is not set, holds none.
func valueEntries(values ...string) [][]string {
	var out [][]string
	var seen []string
	for _, v := range values {
		if slices.Contains(seen, v) {
			continue
		}
		seen = append(seen, v)
		if v == "" {
			out = append(out, nil)
			continue
		}
		out = append(out, strings.Split(v, ":"))
	}
	return out
}

// searched reports whether the cd searches any entry at all.
func (p searchPath) searched() bool {
	return len(p.set) > 0 || len(p.carried) > 0
}

// cdpathEntries is the entries of the CDPATH values given, in order, each
// once. Joined and split again for every cd, the 600 values of a line of
// `CDPATH=/a:/b cd x` were each read 600 times, and every copy counted as a
// place (US-035, verifier round 12).
func cdpathEntries(values ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		for entry := range strings.SplitSeq(v, ":") {
			if !seen[entry] {
				seen[entry] = true
				out = append(out, entry)
			}
		}
	}
	return out
}

// underCdpath is where a relative `cd` words make given CDPATH p could be:
// the shell looks for the directory under each entry first, and only then
// where it is. A relative entry is searched from where the shell is. Placed
// from there only where that was not the project's root, whose dir is "",
// `CDPATH=.sdlc cd state && rm -f active` was read as a plain relative cd and
// took the loop's state away; from where a lost walk could be anywhere, the
// cd is unplaced (US-035, code review round 5).
//
// A lost walk keeps what was known of it, moved by the cd as though no CDPATH
// were set, which is where the shell goes where no entry holds the directory;
// the places an absolute entry could send it are searched beside it. Started
// afresh, a CDPATH of `:` or of a directory that is not there left the walk
// placed nowhere and asked nothing: `D=x; cd $D && CDPATH=: cd state && rm -f
// active` was allowed (US-035, unfreeze #12).
func (l lostPlace) underCdpath(words []string, dir string, lost bool, to string, p searchPath) lostPlace {
	if lost {
		// A place the shell certainly leaves for an entry's directory does
		// not move below: `CDPATH=/a:/b:. cd x`, repeated, otherwise added
		// /a/x/x, /a/x/x/x and on, and passed the bound (reachedFirst).
		l.dirs, l.searched = l.notReachedFirst(l.dirs, to, p), l.notReachedFirst(l.searched, to, p)
	}
	next := l.cdpathStart(lost, to)
	var asked []string
	for _, list := range []struct {
		entries []string
		carried bool
	}{{p.set, false}, {p.carried, true}} {
		for _, entry := range list.entries {
			if slices.Contains(asked, entry) {
				continue
			}
			asked = append(asked, entry)
			if why, fix, ok := searchEntry(&next, entry, dir, to, lost, list.carried); !ok {
				return anywhereAfter(words, to, why, fix)
			}
		}
	}
	if !lost {
		next.dirs = addPlace(next.dirs, path.Join(dir, to))
	}
	if next.placeCount() > maxCdpathPlaces {
		return anywhereAfter(words, to, cdpathMany, cdpathManyFix)
	}
	return next
}

// notReachedFirst is places without those a cd to to that searches p
// certainly takes the shell from to an entry's directory (reachedFirst).
func (lostPlace) notReachedFirst(places []string, to string, p searchPath) []string {
	var out []string
	for _, q := range places {
		if !p.reachedFirst(q, to) {
			out = append(out, q)
		}
	}
	return out
}

// cdpathStart is what is known of a walk after a cd to to that searches
// CDPATH, before any entry is: on a lost walk, where it goes where no entry
// holds the directory. Where nothing was known after what the walk could not
// place, the cd's own words are: below, which only lengthens a tail, left
// none, and a cd that searched CDPATH into the project's Claude Code
// directory, then a program run there, was no longer read as inside it
// (US-035, verifier round 12).
func (l lostPlace) cdpathStart(lost bool, to string) lostPlace {
	if !lost {
		return lostPlace{tail: to}
	}
	next := l.below(to)
	if next.tail == "" && !strings.ContainsAny(to, "$*?[`") {
		next.tail = to
	}
	return next
}

// searchEntry adds to next the place CDPATH entry could send a cd to to from
// dir; or, where it leaves the shell anywhere, says why, with the fix.
// carried is whether the entry is the session's environment's.
func searchEntry(next *lostPlace, entry, dir, to string, lost, carried bool) (why, fix string, ok bool) {
	switch {
	case isAbsolute(entry) && lost:
		next.searched = addPlace(next.searched, path.Join(entry, to))
	case isAbsolute(entry):
		next.dirs = addPlace(next.dirs, path.Join(entry, to))
	case !lost:
		next.dirs = addPlace(next.dirs, path.Join(dir, entry, to))
	case entry == "" || entry == ".":
		// The directory the shell is in, where it goes anyway.
	case carried:
		return cdpathCarriedRelative, cdpathCarriedFix, false
	default:
		return cdpathRelative, cdpathRelativeFix, false
	}
	if next.placeCount() > maxCdpathPlaces {
		return cdpathMany, cdpathManyFix, false
	}
	return "", "", true
}

// anywhereAfter is a lost walk after a `cd` words make to to, which could
// have left the shell in any directory at all, for why, with fix.
func anywhereAfter(words []string, to, why, fix string) lostPlace {
	next := lostPlace{cd: strings.Join(words, " "), why: why, fix: fix, unplacing: true, anywhere: true}
	if !strings.ContainsAny(to, "$*?[`") {
		next.tail = to
	}
	return next
}

// cdpath is the CDPATH a `cd` in segment searches: every value set in front
// of it, set or exported anywhere on the line, and the one in the environment
// the session started in; more places is the refusing direction. unread is
// whether the line sets CDPATH in a way no value is read from, as
// `CDPATH+=x`, `CDPATH=$D`, `read CDPATH` or zsh's `cdpath=(x)`: a relative
// cd could then be anywhere. Split on spaces alone, `CDPATH=<dir>;cd state`
// read the value with the `;` in it, a directory that is not there, and `rm
// -f active` after it took the loop's state away (US-035, security review
// round 4). The command and the line are read once: read again for every cd,
// a line of 500 assignments and 700 cds took three and a half seconds (US-035,
// performance review, design review round 3).
func (w *pathWalk) cdpath(segment string) searchPath {
	if w.cdpathOf == nil {
		p := w.startingCdpath()
		w.cdpathOf = &p
	}
	p := *w.cdpathOf
	if here := cdpathAssigned(segment); here.unread || here.cleared || len(here.values) > 0 {
		p.set = slices.Concat(cdpathEntries(here.values...), p.set)
		p.unread = p.unread || here.unread
		p.cleared = p.cleared || here.cleared
		if len(here.values) > 0 {
			p.values = valueEntries(here.values...)
		}
	}
	return p
}

// startingCdpath is the CDPATH every cd of the command could search before
// its own segment is read: what the command and the line assign, and the
// session's. The session's value joins values only beside one the line
// assigns, an unset one as a value with no entries, since a cd before the
// assignment searches it (reachedFirst).
func (w *pathWalk) startingCdpath() searchPath {
	command, line := cdpathAssigned(w.command), w.s.seen.lineCdpath()
	assigned := slices.Concat(command.values, line.values)
	p := searchPath{
		set:     cdpathEntries(assigned...),
		unread:  command.unread || line.unread,
		cleared: command.cleared || line.cleared,
	}
	carried := ""
	if w.s.Carried != nil {
		carried = w.s.Carried("CDPATH")
		if carried != "" {
			p.carried = cdpathEntries(carried)
		}
	}
	if len(assigned) > 0 {
		p.values = valueEntries(slices.Concat(assigned, []string{carried})...)
	}
	return p
}

// cdpathRead is what a text says of CDPATH: each value it assigns, whether
// it sets it in a way no value is read from, and whether it unsets it or
// assigns it empty (cleared).
type cdpathRead struct {
	values  []string
	unread  bool
	cleared bool
}

// cdpathAssigned is what text says of CDPATH: each value it assigns, and
// unread where it may set CDPATH in a way that gives no value the words show:
// appended to, set from a variable, a substitution or a glob, quoted in part,
// set as an element or an array, named to a command, or spelt in another
// case. zsh ties the array cdpath to the same search path, and read by its
// capitals alone, `cdpath=(<dir>); cd state && rm -f active` took the loop's
// state away (US-035, security review, design review round 3). A whole word
// is read as one that may set it unless it is read (`$CDPATH`, `${CDPATH}`)
// or unset, so a command the hook does not know sets one fails closed; part
// of a longer word is no mention: any mention at all made `unset CDPATH; cd
// build && rm -rf out` and a file called cdpath.go.orig unplace every cd on
// the line (US-035, red team, design review round 3, and security review,
// code review round 5).
func cdpathAssigned(text string) cdpathRead {
	const name = "CDPATH"
	var r cdpathRead
	folded := upperASCII(text)
	fields := assignmentScan{text: text, start: -1, assigns: true}
	for i := 0; ; {
		j := strings.Index(folded[i:], name)
		if j < 0 {
			return r
		}
		at := i + j
		i = at + len(name)
		r.mention(text, at, i, &fields)
	}
}

// mention reads the name text spells in text[at:end], a mention of CDPATH in
// any case, read forward by fields.
func (r *cdpathRead) mention(text string, at, end int, fields *assignmentScan) {
	rest := text[end:]
	switch {
	case at > 1 && text[at-2:at] == "${":
		// Read, unless the expansion assigns it, as `${CDPATH:=<dir>}`.
		r.unread = r.unread || strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":=")
		return
	case at > 0 && !strings.ContainsRune(cdpathBefore, rune(text[at-1])):
		// Part of another word, or `$CDPATH`, which reads it.
		return
	}
	setting := strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, "+=") || strings.HasPrefix(rest, "[")
	if !setting && rest != "" && !strings.ContainsRune(wordEnds+"'\"\r", rune(rest[0])) {
		// Part of another word, as cdpath.go.
		return
	}
	assigns := fields.assignsAt(at)
	switch {
	case setting && assigns && text[at:end] == "CDPATH" && rest[0] == '=':
		r.assigned(rest[1:])
	case !setting && fields.command == "unset":
		// unset clears it: a cd after it searches no CDPATH, and one before
		// it may search one, so no place is certain (reachedFirst).
		r.cleared = true
	default:
		// Any other whole word may set it, by a command the hook does not
		// know sets one. A comment is read as words too: read by the quote
		// tracker, a misread `#` would hide what comes after it.
		r.unread = true
	}
}

// assigned reads the value at the start of s, what follows a `CDPATH=`.
func (r *cdpathRead) assigned(s string) {
	v, ok := assignedValue(s)
	switch {
	case !ok:
		r.unread = true
	case v != "":
		r.values = append(r.values, v)
	default:
		// An empty CDPATH is searched by no cd, which goes to its own
		// directory's child, so no place is certain (reachedFirst).
		r.cleared = true
	}
}

// cdpathBefore are the characters a whole word CDPATH can follow.
const cdpathBefore = " \t\n\r;&|(='\""

// upperASCII is s with its ASCII letters in capitals and every other byte
// left as it is, so that an index into it is one into s.
func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// assignmentScan reads the fields of a text forward, once, and says at each
// place it is asked about whether a name starting there is where the shell
// assigns it: at the start of a command, after other assignments, or as an
// operand of one of the builtins that set a variable, after its options.
// Asked of all that came before each name, a line of many assignments was
// read again for each one.
type assignmentScan struct {
	text string
	// pos is the next byte to read, start where the field being read began,
	// or -1 between fields, and n how many fields of the command were read.
	pos, start, n int
	// assigns is whether the fields read so far leave a name after them
	// where the shell assigns it, and command the command's first field that
	// is no assignment.
	assigns bool
	command string
}

// assignsAt reports whether a name at at is where the shell assigns it, a
// quote opened just before it included. at is never before a place asked
// about already.
func (a *assignmentScan) assignsAt(at int) bool {
	for ; a.pos < at; a.pos++ {
		switch c := a.text[a.pos]; {
		case strings.IndexByte(";&|(\n", c) >= 0:
			a.start, a.n, a.assigns, a.command = -1, 0, true, ""
		case strings.IndexByte(" \t\r\v\f", c) >= 0:
			a.field(a.pos)
		case a.start < 0:
			a.start = a.pos
		}
	}
	return a.assigns && (a.start < 0 || strings.Trim(a.text[a.start:at], `'"`) == "")
}

// field ends the field being read at end, where one is.
func (a *assignmentScan) field(end int) {
	if a.start < 0 {
		return
	}
	f := a.text[a.start:end]
	assignment := strings.IndexByte(f, '=') > 0
	switch {
	case assignment || strings.HasPrefix(f, "-"):
	case a.n == 0 && slices.Contains(assigningBuiltins, f):
	default:
		a.assigns = false
	}
	if a.command == "" && !assignment {
		a.command = f
	}
	a.start = -1
	a.n++
}

// assigningBuiltins are the builtins whose operands assign a variable, and
// env, whose operands set one for the program it runs.
var assigningBuiltins = []string{"export", "declare", "typeset", "local", "readonly", "env"}

// assignedValue is the value at the start of s, what follows a `CDPATH=`,
// where the words alone say what it is: not where the shell would expand
// part of it, nor where it runs on into more of the same word.
func assignedValue(s string) (string, bool) {
	var v string
	switch {
	case s != "" && (s[0] == '\'' || s[0] == '"'):
		end := strings.IndexByte(s[1:], s[0])
		if end < 0 {
			return "", false
		}
		v, s = s[1:1+end], s[2+end:]
	default:
		end := strings.IndexAny(s, wordEnds)
		if end < 0 {
			end = len(s)
		}
		v, s = s[:end], s[end:]
	}
	if s != "" && !strings.ContainsRune(wordEnds, rune(s[0])) {
		return "", false
	}
	// A tilde is expanded at the start of a value and after each colon.
	if strings.ContainsAny(v, "$`\\'\"(") || strings.HasPrefix(v, "~") || strings.Contains(v, ":~") {
		return "", false
	}
	return v, true
}

// wordEnds are the characters that end an unquoted word.
const wordEnds = " \t\n;&|()<>"
