package shellpolicy

import "strconv"

// assignment is a word read as the shells read an assignment in front of a
// program: a name, an optional subscript, an optional +, then = and the value.
type assignment struct {
	name, subscript, value string
	indexed, appends       bool
}

// readAssignment reads w as bash and zsh read an assignment, finding the ]
// that closes a subscript by counting brackets and skipping quotes. Read as
// the name characters before = alone, `a[1]=x git push --force` and
// `SDLC_ENFORCE+=0 sdlc status` had the assignment as their program, though
// the shells run the git and the sdlc after it.
func readAssignment(w string) (assignment, bool) {
	i := 0
	for i < len(w) && nameRune(rune(w[i])) {
		i++
	}
	if i == 0 {
		return assignment{}, false
	}
	a := assignment{name: w[:i]}
	if i < len(w) && w[i] == '[' {
		end, ok := subscriptEnd(w, i)
		if !ok {
			return assignment{}, false
		}
		a.indexed, a.subscript = true, w[i+1:end]
		i = end + 1
	}
	if i < len(w) && w[i] == '+' {
		a.appends = true
		i++
	}
	if i >= len(w) || w[i] != '=' {
		return assignment{}, false
	}
	a.value = w[i+1:]
	return a, true
}

// subscriptEnd is where the ] that closes the subscript opening at w[open]
// is, if one does.
func subscriptEnd(w string, open int) (int, bool) {
	depth := 0
	for j := open; j < len(w); j++ {
		switch c := w[j]; c {
		case '\\':
			j++
		case '\'', '"':
			k := indexByteFrom(w, j+1, c)
			if k < 0 {
				return 0, false
			}
			j = k
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return j, true
			}
		}
	}
	return 0, false
}

func indexByteFrom(s string, from int, c byte) int {
	for k := from; k < len(s); k++ {
		if s[k] == c {
			return k
		}
	}
	return -1
}

// assignments is how one reading of a command takes the words in front of a
// program, and a brace at the start of a word. Some shells read a subscripted
// or appending word, `a[$i]=x` or `a+=x/git`, as an assignment and others
// (dash) as the program, and a word whose subscript the shell has yet to work
// out may be read either way, so each entry point reads a line twice where it
// holds such a word: widened, then as a word that is not an assignment, and
// refuses if either reading does. Read one way alone, lines that one shell
// runs as git or sdlc were let through. A brace at a word's start is read
// twice the same way: widened, as a character of the word, as the shells read
// it, and then as the released lexer read it, opening a group (lexer.brace).
type assignments struct {
	// released reads a subscripted or appending word as no assignment, and a
	// brace at a word's start as opening a group.
	released bool
	// met notes the first such words the reading met. It is shared by every
	// copy, so a word met deep in a walk is not lost on the way back up.
	met *metWords
}

// metWords is the first word of each kind the widened reading met.
type metWords struct {
	// assignment is a subscripted or appending word in front of a program.
	assignment string
	// brace is what follows a brace at the start of a word: the word the
	// released reading reads as a group's first.
	brace string
}

// widened is the first reading of a line, which notes what it meets.
func widened() assignments { return assignments{met: new(metWords)} }

// of reads w as an assignment, under this reading.
func (r assignments) of(w string) (assignment, bool) {
	a, ok := readAssignment(w)
	if ok && (a.indexed || a.appends) {
		if r.met != nil && r.met.assignment == "" {
			r.met.assignment = w
		}
		if r.released {
			return assignment{}, false
		}
	}
	return a, ok
}

// is reports whether w is an assignment, under this reading.
func (r assignments) is(w string) bool {
	_, ok := r.of(w)
	return ok
}

// metWidened reports whether the reading met a word the two readings take
// apart, so that the line has to be read the second way too.
func (r assignments) metWidened() bool {
	return r.met != nil && (r.met.assignment != "" || r.met.brace != "")
}

// secondReading is the reading a line that met such a word is read again under.
func secondReading() assignments { return assignments{released: true} }

// Classify reads the command as the released reading takes the words in front
// of a program: it names a run for the trace and refuses nothing, so a second
// reading has nothing to add.
func Classify(command string, powerShell bool, configured map[string]string) (class, program string) {
	return classifyOnce(command, powerShell, configured, secondReading())
}

// bothReadings asks check under the widened reading and, where that allows the
// line and met a word the readings take apart, under the second. A refusal only
// the second reading makes says why, since the line reads as a setting to the
// person who wrote it.
func bothReadings(check func(assignments) (Finding, bool)) (Finding, bool) {
	first := widened()
	if f, refused := check(first); refused || !first.metWidened() {
		return f, refused
	}
	f, refused := check(secondReading())
	if !refused {
		return f, false
	}
	if w := first.met.assignment; w != "" {
		f.Reason += "; read as some shells (dash among them) read it, " + strconv.Quote(w) +
			" is the program, though bash and zsh read it as a setting"
		f.Route += "; or put the setting on its own line, before the command"
	}
	if w := first.met.brace; w != "" {
		f.Reason += "; that is the line read as this hook read it before, where a { at the start of a word " +
			"opens a group: there " + strconv.Quote(w) + " is the program name, and a pattern in it can make " +
			"a program the rules refuse"
		f.Route += "; or write the brace list out (a/* b/*), or quote a brace that is no list ('{p}'/*)"
	}
	return f, true
}

// ClaudeConfigWrites is claudeConfigWritesOnce under both readings.
func ClaudeConfigWrites(command string, s State) (Finding, bool) {
	s.Considered = onceEach(s.Considered)
	return bothReadings(func(r assignments) (Finding, bool) { return claudeConfigWritesOnce(command, s, r) })
}

// Inspect is inspectOnce under both readings.
func Inspect(command string, s State) (Finding, bool) {
	s.Considered = onceEach(s.Considered)
	return bothReadings(func(r assignments) (Finding, bool) { return inspectOnce(command, s, r) })
}

// onceEach is tell, told each pair once for the whole call: each reading's
// walk keeps its own count, so `touch {a,b}.txt`, read again for its brace,
// told the log of touch twice through each reading.
func onceEach(tell func(candidate, via string)) func(candidate, via string) {
	if tell == nil {
		return nil
	}
	told := map[[2]string]bool{}
	return func(candidate, via string) {
		if key := [2]string{candidate, via}; !told[key] {
			told[key] = true
			tell(candidate, via)
		}
	}
}

// HumanDecisions is humanDecisionsOnce under both readings.
func HumanDecisions(command string, powerShell bool, storyWaits func(dir string) bool) (Finding, bool) {
	return bothReadings(func(r assignments) (Finding, bool) {
		return humanDecisionsOnce(command, powerShell, storyWaits, r)
	})
}

// The forms below read under the first reading alone, and only the package's
// own tests may call them: production code must call the …As form with the
// reading it is under. A production call here would read a line the second way
// with this one piece read the first way, and that lets through what the second
// reading refuses. They belong in a _test.go file, but the freeze on test files
// kept them out of one, so nothing but review stops a production caller.

func (q pathQuestion) expandsAGlob(raw string) bool { return q.expandsAGlobAs(raw, widened()) }

func checkSecondSession(command string, powerShell bool) (Finding, bool) {
	return checkSecondSessionAs(command, powerShell, widened())
}

func movesInAScript(text string, powerShell bool) bool {
	return movesInAScriptAs(text, powerShell, widened())
}
