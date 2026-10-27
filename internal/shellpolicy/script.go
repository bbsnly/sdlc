package shellpolicy

import "slices"

// This file follows a script handed to a shell, whose cds move the rest of
// the script and not the command around it, by reading the command's quotes;
// and the unquoted walk, which keeps a misread quote from allowing a command.

// A scriptScope is a script handed to a shell that the walk is inside, as
// `sh -c 'cd x && rm y'`: where the walk was when the script began, and how
// many subshells were open, which the walk goes back to where the script's
// quote closes; and whether a shell inside it was handed a script again,
// whose quoting is the inner shell's and is not followed. Not followed at
// all, `bash -c 'cd <the loop's state> && rm -f active'` was read from where
// the command started, and allowed (US-035, code review round 4).
type scriptScope struct {
	at        walkPlace
	outer     int
	untracked bool
}

// quoting reads the quotes of a segment, notes whether it closes the script
// the walk is in, and reports whether it starts inside a quote. Where the
// command's quotes do not close, no shell runs it as written and the walk
// reads no script.
func (w *pathWalk) quoting(segment string) bool {
	if w.unbalanced {
		return false
	}
	quoted := w.quote != 0
	w.quote = quoteAfter(segment, w.quote, w.s.PowerShell)
	w.leaving = w.script != nil && w.quote == 0
	return quoted
}

// enters begins the script a segment hands to a shell, and notes whether the
// segment closes its quote as well, so that the script ends with it. A shell
// handed a script inside a script is not followed: its cds are ones the hook
// cannot place. A shell in a quoted word of a command that is no shell runs
// nothing.
func (w *pathWalk) enters(run invocation, quoted bool) {
	switch {
	case !run.shell || w.unbalanced || quoted && w.script == nil:
		return
	case w.script != nil:
		w.script.untracked = true
		return
	}
	w.script = &scriptScope{at: w.place(), outer: len(w.outer)}
	w.leaving = w.quote == 0
}

// scriptStart asks the path rules of a segment of a script handed to a shell
// once more from where the script began: the refusing side, since the
// script's words were read from there before its cds were followed.
func (w *pathWalk) scriptStart(j segmentRules, segment string, run invocation) (Finding, bool) {
	if w.script == nil {
		return Finding{}, false
	}
	at := w.script.at
	if at.dir == j.dir && at.lost == j.lost {
		return Finding{}, false
	}
	k := j
	k.dir, k.lost, k.from, k.certain = at.dir, at.lost, at.from, false
	k.away.dir, k.away.lost = at.dir, at.lost
	return k.judge(segment, run)
}

// place is where the walk is, as a subshell keeps it.
func (w *pathWalk) place() walkPlace {
	return walkPlace{w.dir, w.lost, w.old, w.from, w.near, w.kept}
}

// setPlace puts the walk at p.
func (w *pathWalk) setPlace(p walkPlace) {
	w.dir, w.lost, w.old, w.from, w.near, w.kept = p.dir, p.lost, p.old, p.from, p.near, p.kept
}

// leaveScript takes the walk back to where it was when the script it is in
// began. The unquoted walk stays where the script's cds left it.
func (w *pathWalk) leaveScript() {
	if w.script == nil {
		return
	}
	if w.unquoted == nil {
		w.unquoted = &unquotedWalk{at: w.place(), outer: slices.Clone(w.outer)}
	}
	w.setPlace(w.script.at)
	w.outer = w.outer[:min(len(w.outer), w.script.outer)]
	w.script = nil
	w.dropUnquoted()
}

// An unquotedWalk is where the walk would be had it not read the quotes:
// every cd outside eval and a shell's own words moving it, wherever a quote
// put the cd, and no script's end taking it back, with the subshells open
// around it. The quote tracker is a reader of its own, which can be wrong
// where a misread quote balances again further on; where its answer moves the
// walk less than that, each segment is judged from the unquoted walk as well,
// so that a misread costs a refusal and not an allow (US-035, security review
// note 1 and architect note 5, design review round 3).
type unquotedWalk struct {
	at    walkPlace
	outer []walkPlace
}

// keepUnquoted moves the unquoted walk by the cd words make, where moves says
// it moves, after a segment that took the walk from before; diverged is
// whether the walk itself went somewhere the unquoted walk does not, which
// starts one where there is none.
func (w *pathWalk) keepUnquoted(before walkPlace, words []string, segment string, moves, diverged bool) {
	u := w.unquoted
	if u == nil {
		if !diverged {
			return
		}
		u = &unquotedWalk{at: before, outer: slices.Clone(w.outer)}
		w.unquoted = u
	}
	if moves {
		u.at = w.moved(u.at, words, segment)
	}
	w.dropUnquoted()
}

// dropUnquoted forgets the unquoted walk where it is where the walk is.
func (w *pathWalk) dropUnquoted() {
	if u := w.unquoted; u != nil && samePlace(u.at, w.place()) && slices.EqualFunc(u.outer, w.outer, samePlace) {
		w.unquoted = nil
	}
}

// subshells is pathWalk.subshells for the unquoted walk, where there is one.
func (u *unquotedWalk) subshells(opens, closes int) {
	if u == nil {
		return
	}
	for ; closes > 0 && len(u.outer) > 0; closes-- {
		u.at = u.outer[len(u.outer)-1]
		u.outer = u.outer[:len(u.outer)-1]
	}
	for ; opens > 0; opens-- {
		u.outer = append(u.outer, u.at)
	}
}

// fromUnquoted asks the path rules of a segment once more from where the
// unquoted walk is, where that is somewhere else.
func (w *pathWalk) fromUnquoted(j segmentRules, segment string, run invocation) (Finding, bool) {
	u := w.unquoted
	if u == nil {
		return Finding{}, false
	}
	at := u.at
	if run.chdir != "" {
		cd := []string{"cd", "--", run.chdir}
		at.dir, _ = changedDir(u.at.dir, cd)
		at.lost = lostAt(u.at.lost, at.dir)
		at.from = movedFrom(u.at.from, u.at.lost, cd)
	}
	k := j
	k.dir, k.lost, k.from, k.certain = at.dir, at.lost, at.from, false
	k.away.dir, k.away.lost, k.away.pwd, k.away.pwdLost, k.away.old = at.dir, at.lost, u.at.dir, u.at.lost, u.at.old
	if f, ok := k.judge(segment, run); ok {
		return f, true
	}
	if run.chdir != "" || !u.at.lost || w.s.configOnly {
		return Finding{}, false
	}
	return u.at.near.judge(k, segment, run, u.at.dir)
}

// samePlace reports whether a and b are the same place.
func samePlace(a, b walkPlace) bool {
	return a.dir == b.dir && a.lost == b.lost && a.old == b.old && a.from == b.from && a.kept == b.kept &&
		a.near.same(b.near)
}

// same reports whether l and m know the same of a lost walk.
func (l lostPlace) same(m lostPlace) bool {
	return l.tail == m.tail && l.cd == m.cd && l.why == m.why && l.fix == m.fix && l.unplacing == m.unplacing &&
		l.anywhere == m.anywhere && slices.Equal(l.dirs, m.dirs) && slices.Equal(l.searched, m.searched)
}

// quotesLeftOpen reports whether marked segments, read in turn, end inside a
// quote.
func quotesLeftOpen(marked []string, powerShell bool) bool {
	var q byte
	ticked := false
	for _, m := range marked {
		segment, _, _ := subshellMarks(m, &ticked)
		q = quoteAfter(segment, q, powerShell)
	}
	return q != 0
}

// ansiQuote is quoteAfter's mark for a `$'...'` string.
const ansiQuote = 1

// quoteAfter is the quote a shell is inside at the end of text, read from
// inside open: a single or double quote, ansiQuote, or 0. A backslash escapes
// the character after it outside single quotes, and in PowerShell not at
// all, so an escaped `$` starts no `$'...'`; a `#` that starts a word outside
// quotes starts a comment.
func quoteAfter(text string, open byte, powerShell bool) byte {
	q := open
	for i := 0; i < len(text); i++ {
		var comment bool
		if q, i, comment = quoteStep(text, i, q, powerShell); comment {
			break
		}
	}
	return q
}

// quoteStep is quoteAfter at text[i] inside q: the quote after it, the index
// of the last character it read, and whether a comment starts there.
func quoteStep(text string, i int, q byte, powerShell bool) (byte, int, bool) {
	c := text[i]
	switch {
	case q == ansiQuote && c == '\\':
		return q, i + 1, false
	case q == '\'' || q == ansiQuote:
		if c == '\'' {
			return 0, i, false
		}
	case c == '\\' && !powerShell:
		return q, i + 1, false
	case q == '"':
		if c == '"' {
			return 0, i, false
		}
	case c == '#' && (i == 0 || text[i-1] == ' ' || text[i-1] == '\t'):
		return q, i, true
	case c == '$' && i+1 < len(text) && text[i+1] == '\'' && !powerShell:
		// Read at the `$`, so that an escaped one, which the backslash case
		// steps over, leaves a plain quote after it: read at the quote,
		// `\$'x'` was taken for `$'x'` (US-035, code review round 5).
		return ansiQuote, i + 1, false
	case c == '\'' || c == '"':
		return c, i, false
	}
	return q, i, false
}
