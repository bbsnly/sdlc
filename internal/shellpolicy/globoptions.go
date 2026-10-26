package shellpolicy

import (
	"slices"
	"strings"
)

// The shell that runs a line reads some globs in ways the rest of this
// package does not: zsh's `(a|b)` names each alternative, and with no option
// at all; with GLOB_DOTS (zsh) or dotglob (bash) a `?` or `*` matches a
// leading dot; with EXTENDED_GLOB `(x)#` is any number of x; and bash 3.2's
// `.?` matches `..`. Each is written here as a spelling the rules already
// read, braces and `**`, so that every rule reads the shell's names. Read as
// the line was written, `rm -f (CLAUDE|README).md` was cut at its `|` into a
// command called `(CLAUDE`, and `CLAUDE.(md|x)` named nothing.

// namesOption reports whether a line names one of a shell's options, as zsh
// reads their names: without regard to case or underscores. Any mention
// counts, the refusing direction.
func namesOption(command string, names ...string) bool {
	folded := strings.ReplaceAll(strings.ToLower(command), "_", "")
	for _, n := range names {
		if strings.Contains(folded, n) {
			return true
		}
	}
	return false
}

// The readings shellGlobReading can write, each one a cause a refusal can
// name.
const (
	readGroups  = 1 << iota // `(a|b)`, and EXTENDED_GLOB's `^x` and `(x)#`
	readDots                // GLOB_DOTS and dotglob
	readDotDots             // bash 3.2's `.?`
	readAll     = readGroups | readDots | readDotDots
)

// shellGlobReading is the line with those of the globs in reading written as
// braces and `**`, and whether that changed it. A leading dot is matched
// whether or not the line names GLOB_DOTS or dotglob, since a person's
// start-up files can set either where the hook never sees them; but not in
// quoted text, which the shell does not expand, unless the text is a script
// given to a shell's -c: read so, find's `-name '*.orig'` became two names
// and every find that deletes was refused.
func shellGlobReading(command string, s State, reading int) (string, bool) {
	if s.PowerShell || !strings.ContainsAny(command, "(?*^") {
		return command, false
	}
	g := globReader{command: command, reading: reading, extended: namesOption(command, "extendedglob")}
	if reading&readDots != 0 {
		g.dotNames = heldDotNames(s)
	}
	var b strings.Builder
	var q globQuotes
	for i := 0; i < len(command); i++ {
		if command[i] == '\\' && q.quote != '\'' && i+1 < len(command) {
			b.WriteString(command[i : i+2])
			i++
			continue
		}
		q.step(command, i)
		here := reading
		if !q.expands() {
			here &^= readDots
		}
		written, end := g.at(i, here)
		if end < i {
			b.WriteByte(command[i])
			continue
		}
		b.WriteString(written)
		i = end
	}
	out := b.String()
	return out, out != command
}

// globQuotes is where a line is in its quotes: quote is the quote it is in,
// and inner the one a script in it is in, where the quoted text is a script.
type globQuotes struct {
	quote, inner byte
	script       bool
}

// step reads the byte at i.
func (q *globQuotes) step(command string, i int) {
	switch c := command[i]; {
	case q.quote == 0 && (c == '\'' || c == '"'):
		q.quote, q.script = c, givenToShell(command[:i])
	case c == q.quote:
		q.quote, q.inner = 0, 0
	case q.script && (c == '\'' || c == '"') && (q.inner == 0 || q.inner == c):
		q.inner ^= c
	}
}

// expands reports whether the shell that reads the text there expands a glob
// in it.
func (q *globQuotes) expands() bool {
	return q.quote == 0 || q.script && q.inner == 0
}

// givenToShell reports whether the text before a quote ends in a shell's -c,
// alone or at the end of a bundle (`bash -lc`), so that what is quoted is a
// script the shell reads.
func givenToShell(before string) bool {
	fields := strings.Fields(before)
	if len(fields) == 0 || !strings.HasSuffix(before, " ") && !strings.HasSuffix(before, "\t") {
		return false
	}
	last := fields[len(fields)-1]
	return len(last) > 1 && last[0] == '-' && last[1] != '-' && strings.HasSuffix(last, "c")
}

// globReader is shellGlobReading's line and what it reads in it: the
// readings asked for, whether the line sets EXTENDED_GLOB, and the held
// names that start with a dot.
type globReader struct {
	command  string
	reading  int
	extended bool
	dotNames []string
}

// heldDotNames are the names a rule holds that start with a dot: the loop's
// directory, git's, the instruction directories, and every such name in the
// path of a frozen test or the backlog.
func heldDotNames(s State) []string {
	names := []string{".sdlc"}
	for _, p := range slices.Concat(protectedShellPaths, s.Frozen, []string{s.Backlog}) {
		for seg := range strings.SplitSeq(p, "/") {
			if strings.HasPrefix(seg, ".") && seg != "." && seg != ".." && !slices.Contains(names, seg) {
				names = append(names, seg)
			}
		}
	}
	return names
}

// at is how the glob that starts at i is written in the readings asked for
// there, and the index of its last byte, or an end before i where nothing
// starting at i is rewritten.
func (g globReader) at(i, reading int) (string, int) {
	command, c := g.command, g.command[i]
	switch {
	case reading&readGroups != 0 && c == '(':
		if group, end, ok := globGroup(command, i, g.extended); ok {
			return group, end
		}
	case reading&readDotDots != 0 && c == '.' && i+1 < len(command) && command[i+1] == '?' &&
		segmentStart(command, i) && segmentEnd(command, i+2):
		// bash 3.2 lets `.?` match `..`.
		return "{.?,..}", i + 1
	case reading&readGroups != 0 && g.extended && c == '^' && segmentStart(command, i) && !segmentEnd(command, i+1):
		// EXTENDED_GLOB's `^x` is every name but x, which `*` covers.
		return "*", nameEnd(command, i)
	case reading&readDots != 0 && (c == '?' || c == '*') && segmentStart(command, i) && !segmentEnd(command, i+1) &&
		g.matchesDotName(i):
		// GLOB_DOTS and dotglob let a name's first character be a dot.
		// Neither matches `.` or `..`, which a glob that is the whole name
		// could, so such a glob is left as it is.
		if c == '?' {
			return "{?,.}", i
		}
		return "{*,.*}", i
	}
	return "", -1
}

// matchesDotName reports whether the name whose glob starts at i matches a
// held name once its first character can be a dot. Only such a name is
// written in braces, which no plain command holds: written so wherever it
// stood, `rm -f *_test.go` in a directory outside the repository could no
// longer be placed there, and was refused as the repository's.
func (g globReader) matchesDotName(i int) bool {
	name := g.command[i : nameEnd(g.command, i)+1]
	if strings.HasPrefix(name, "**") {
		// A `**` is read as directories already, and braced it was not:
		// `/tmp/**/../work` was no longer read as climbing.
		return false
	}
	dotted := "." + name[1:]
	if name[0] == '*' {
		dotted = "." + name
	}
	return slices.ContainsFunc(g.dotNames, func(held string) bool { return segmentMatches(dotted, held) })
}

// nameEnd is the index of the last byte of the name that i is in.
func nameEnd(command string, i int) int {
	for i+1 < len(command) && !segmentEnd(command, i+1) {
		i++
	}
	return i
}

// globGroup is a `(...)` at open written as the shell reads it in a word: an
// alternation `(a|b)` as `{a,b}`, and with EXTENDED_GLOB `(x)#` as `{,x}`, or
// `**/` for `(*/)#`. ok is false for anything else, which includes a
// subshell: a group at a command's start, or one holding white space, is
// left as it is, so `(echo|tee CLAUDE.md)` is still read as a pipe into tee.
func globGroup(command string, open int, extended bool) (string, int, bool) {
	if open > 0 && command[open-1] == '$' || atCommandStart(command, open) {
		return "", 0, false
	}
	end := strings.IndexByte(command[open+1:], ')')
	if end < 0 {
		return "", 0, false
	}
	end += open + 1
	inner := command[open+1 : end]
	if inner == "" || strings.ContainsAny(inner, " \t\n;&<>()'\"") {
		return "", 0, false
	}
	if extended && end+1 < len(command) && command[end+1] == '#' {
		last := end + 1
		if last+1 < len(command) && command[last+1] == '#' {
			last++
		}
		if inner == "*/" {
			return "**/", last, true
		}
		return "{," + inner + "}", last, true
	}
	if !strings.Contains(inner, "|") {
		return "", 0, false
	}
	return "{" + strings.ReplaceAll(inner, "|", ",") + "}", end, true
}

// atCommandStart reports whether nothing but white space stands between i
// and the start of the command it is in.
func atCommandStart(command string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch command[j] {
		case ' ', '\t':
			continue
		case ';', '&', '|', '(', '{', '\n', '!':
			return true
		}
		return false
	}
	return true
}

// segmentStart reports whether i starts a name: the start of a word, or just
// after a `/`.
func segmentStart(command string, i int) bool {
	return i == 0 || strings.IndexByte(" \t\n/='\"><", command[i-1]) >= 0
}

// segmentEnd reports whether i ends a name.
func segmentEnd(command string, i int) bool {
	return i == len(command) || strings.IndexByte(" \t\n/;&|'\")", command[i]) >= 0
}

// inspectShellGlobs refuses a line that the shell's reading of its globs
// refuses, judged first where that reading differs from the words: refused
// under the split reading as well, `rm -f (CLAUDE|README).md` was told it
// started a session of its own. A reading that is let go records nothing:
// State.Commits is called only for the answer that stands.
func inspectShellGlobs(command string, s State) (Finding, bool) {
	read, changed := shellGlobReading(command, s, readAll)
	if !changed {
		return Finding{}, false
	}
	committed, record := false, s.Commits
	s.Commits = func() { committed = true }
	f, refused := inspectWritten(read, s)
	if !refused {
		return Finding{}, false
	}
	if committed && record != nil {
		record()
	}
	return withDotClause(f, command, s), true
}

// dotClauses are what a refusal says where only a reading of a leading dot
// names what it refuses: without it, `rm -f ?sdlc/state/active` was refused
// for a file the person's own shell might never have matched, and nothing
// said why.
var dotClauses = []struct {
	reading int
	clause  string
}{
	{readDots, "; `?sdlc` matches `.sdlc` if the shell has GLOB_DOTS or dotglob set, which the hook " +
		"cannot see in your start-up files"},
	{readDotDots, "; `.?` matches `..` in bash 3.2"},
}

// withDotClause is f, refused under a held-name rule, with the clause for
// the dot reading that alone refuses the command. It is read again only for
// a refusal, which is rare, and not at all where another reading refuses.
func withDotClause(f Finding, command string, s State) Finding {
	if f.Rule != ruleLoopState && f.Rule != "protected-path-through-the-tool" && f.Rule != ruleFrozenTest {
		return f
	}
	s.Commits = nil
	without := func(reading int) bool {
		read, _ := shellGlobReading(command, s, reading)
		_, refused := inspectWritten(read, s)
		return refused
	}
	if without(readGroups) {
		return f
	}
	for _, d := range dotClauses {
		if without(readGroups | d.reading) {
			f.Reason += d.clause
			return f
		}
	}
	return f
}
