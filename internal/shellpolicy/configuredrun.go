package shellpolicy

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// US-134 has `sdlc gate implementation pass` and `sdlc freeze` run the
// project's configured commands themselves and record the result, which the
// landing and the verifier then reuse. The commands inherit the environment
// sdlc starts with, and the session chooses it: `GOFLAGS='-run=^$'`, a PATH
// whose `go` exits 0, or NODE_OPTIONS=--require in front of the call records a
// pass of a run that tested nothing. So this rule refuses either call where
// the same line changes that environment, or runs the call behind a word that
// can change it. It refuses any change and names no tool's variable: the core
// is language-neutral (AC-1, AC-13; security F-1).
//
// It asks agentenv.go's whole reading of the line with anyName, a set that
// takes any name for a protected one, so that every spelling read there for
// the six names is read here for every name (the person's option A, security
// F-22 and F-23), and adds readings of its own for what agentenv.go has no
// need to read: the limits, the keyword and allexport options, an assigning
// `${…}` head with flags or a nested name, PowerShell's compound assignments
// and arithmetic. On PowerShell it reads what is allowed rather than what
// refuses: a line that may be the call must be the call alone, after at most
// one Set-Location or cd to an absolute path (E below). Each round of review
// from F-23 on found another way PowerShell reaches the environment, so E
// stands in for agentenv.go's quotesProtected, which answers no for anyName.
//
// The evasions tried against it (2026-11-21 and 2026-11-22, bash 5.3.20 and
// zsh 5.9, a stand-in sdlc first on PATH), all refused: every row of
// us134_test.go, us134_wrappers_test.go and us134_agentenv_test.go, as their
// headers record; among them a setting, env, an export, a declaration, `set
// -a`, an unset, an alias or a function in front of the call or earlier on the
// line; every builtin agentenv.go reads as writing a name; the keyword option
// through `set`, `setopt` and `shopt`, and through an option word the shell
// makes; assigning heads with flags or a nested name; the limits; a builtin
// reached through a made program word; made words standing for sdlc, for the
// whole call or for a tail of it; a wrapper other than time, !, exec, command,
// nice and nohup, with sdlc written out or made; the call handed to a shell
// or an interpreter as text; and, on PowerShell, every statement beside the
// call, at any depth, a line naming the call, Invoke-Expression, pwsh given an
// encoded command or anything but literal words, and a typographic quote.
// The code review's round (2026-11-23, zsh 5.9 -f and bash 5.3, a stand-in
// sdlc by path and on PATH, and Inspect in a scratch copy, as no test file
// could be added after the freeze): zsh's glob qualifiers on the program word,
// `(N)`, `(:A)`, `(.)`, `(#q.)` and chains of them, before both calls, with a
// setting in front and with an export earlier; qualifiers that run code or add
// words, `(e:…:)`, `(+name)` and `(P:…:)`, on the program word or anywhere on
// a line with the call; the same inside `$(…)`, `${…}` and backticks; and the
// call handed to rg --pre, man -P, MANPAGER, PAGER and LESSOPEN, all refused,
// while the qualified call alone runs as before. Its second round (the same
// day and shells): an increment after a nested `((`, `(( ((1)), CI++ ))`;
// arithmetic assigning a name the shell makes, `${x:-HOME}`, `${:-HOME}`,
// `$x`, `$(printf HOME)` or a backtick, with each of the thirteen assigning
// operators, in `((…))`, `$((…))`, `$[…]`, a subscript quoted or not, an
// offset, zsh's padding flags, `[[ … -eq … ]]`, `for ((…))`, let, a quoted
// string `${1@P}` or `${(e)…}` expands, and zsh's shift, repeat, return,
// break, continue, printf and print -f; a descriptor stored in a name,
// `{HOME}>/dev/null`; zsh's module builtins given a made name; and hash -p in
// each spelling, behind builtin or command, and zsh's `hash name=path`, all
// refused, while `hash`, `hash -r`, `hash -t` and comparisons in arithmetic
// and tests stay allowed. Its third: a here-document whose delimiter is not
// quoted, with the call and a setting in a substitution in its body, with
// arithmetic or an assigning `${…}` in a body a builtin or a group reads
// before the call, and on zsh in the call's own document, through `<<` and
// `<<-` and in a backtick, all refused, while a quoted delimiter and a body
// with nothing the shell runs stay allowed. Its fourth (zsh 5.9 -f and bash
// 5.3, a stand-in sdlc first on PATH and in the directory): zsh's subscript in
// such a body, `$path[HOME=41]`, `$argv[…]`, `$#path[…]`, `$HOME[…]`,
// `$path[1,TMPDIR=44]`, each flag before the name and `$@[…]`, before the call
// written out or made and in its own document; and a made call with a setting
// in a substitution whose end a reader finds early, after a `)` in double
// quotes, a case pattern, a comment, a `}` in double quotes in `${…}` or an
// escaped backtick, with `$(printf sd%s lc)` and `sd[l]c`, and arithmetic
// behind a substitution whose end it finds late, `$(echo "(") don't $((
// HOME=41 ))`, all refused, while the same bodies under a quoted delimiter,
// and `$(date)`, a backtick's `git rev-parse HEAD` and `$name` beside a made
// call, stay allowed.
// What it does not hold is in docs/enforcement.md's Not held.

// configuredRunRule is the rule's name, which docs/enforcement.md heads its
// section with.
const configuredRunRule = "commands-run-as-configured"

// configuredRunSetting is how every route under the rule ends.
const configuredRunSetting = "a setting the project's commands need goes in the configured command itself, in " +
	loopConfigFile + ", which a person edits"

// The causes of a refusal, each with its own reason and route. Bash's are all
// the names text; on PowerShell the first that holds names the cause.
const (
	causeNames = iota
	causeTypographicQuote
	causeShellGivenMore
	causeBesideTheCall
	causeExpandingDocument
)

// configuredRunFinding is the refusal for a cause. docs/enforcement.md quotes
// each text, and the US-129 golden pins the names text, which a line naming
// the call on PowerShell gets.
func configuredRunFinding(cause int) Finding {
	f := Finding{Rule: configuredRunRule}
	switch cause {
	case causeTypographicQuote:
		f.Reason = "this line holds a typographic quote, one of ‘ ’ ‚ ‛ “ ” „, at which " +
			"PowerShell ends a string as at an ASCII quote, so what it runs cannot be read, and it may run sdlc freeze " +
			"or sdlc gate implementation pass with an environment it changed"
		f.Route = "write ASCII quotes in place of typographic ones, or hand the text over in a file (git commit -F, " +
			"or --file on sdlc review add and sdlc artifact write); " + configuredRunSetting
	case causeShellGivenMore:
		f.Reason = "pwsh or powershell is given a word that is not literal, or an encoded command, so the script it " +
			"runs cannot be read, and it may run sdlc freeze or sdlc gate implementation pass with an environment " +
			"it changed"
		f.Route = "give pwsh only plain words and single-quoted strings, as in pwsh -File ./build.ps1, and no " +
			"encoded command; " + configuredRunSetting
	case causeBesideTheCall:
		f.Reason = "on PowerShell, sdlc freeze and sdlc gate implementation pass run alone on their line, because " +
			"PowerShell can change the environment they start with in more ways than a reading of the line can " +
			"list, and this line holds something beside the call"
		f.Route = "run the call alone on its line, written out as sdlc, with a plain or single-quoted note, after " +
			"at most one Set-Location or cd to an absolute path then ; or &&; " + configuredRunSetting
	case causeExpandingDocument:
		f.Reason = "this line holds a here-document whose delimiter is not quoted, so the shell runs the " +
			"substitutions in its body and evaluates its arithmetic and its other expansions, which can run sdlc " +
			"freeze or sdlc gate implementation pass, or change the environment either starts with"
		f.Route = "quote the delimiter, as in <<'EOF', so that the body is text, or hand the text over in a file " +
			"(git commit -F, or --file on sdlc review add and sdlc artifact write); " + configuredRunSetting
	default:
		f.Reason = "sdlc gate implementation pass and sdlc freeze run the configured commands with the environment " +
			"they start with, and record the result; a line that changes it could make them pass or fail for a " +
			"reason that is not the code"
		f.Route = "run the call with nothing in front of it; " + configuredRunSetting
	}
	return f
}

// checkConfiguredRun is the rule, asked at the end of checkPrograms over the
// runs it built from text, the reading. It runs after every earlier rule has
// found nothing, so a line one of them refuses keeps that rule's name.
func checkConfiguredRun(runs []ran, text string, s State, reading assignments) (Finding, bool) {
	if s.PowerShell {
		line := text
		if s.seen != nil && s.seen.line != "" {
			line = s.seen.line
		}
		return powerShellConfiguredRun(runs, text, line)
	}
	if s.seen != nil && expandingDocument(s.seen.line, text, s, reading) {
		return configuredRunFinding(causeExpandingDocument), true
	}
	if f, ok := bashConfiguredRun(runs, text); ok {
		return f, true
	}
	return qualifiedConfiguredRun(runs, text, reading)
}

// qualifiedConfiguredRun is the rule asked again of the line as zsh reads it,
// where a parenthesis stuck to a word is the word's: glob qualifiers, or a
// group of a pattern. The reader reads it as bash does, which ends the word
// there and reads `(N)` as a subshell and `(#q.)` as the start of a comment,
// so that `GOFLAGS=-run=nothing ~/.local/bin/sdlc(N) freeze` was the call
// alone in one run and a setting on sdlc in another; zsh, the default shell
// on macOS, ran sdlc with the setting through `(N)`, `(:A)`, `(.)` and
// `(#q.)`, where bash refuses the line as a syntax error (code review,
// security F-1). zshQualified writes each such parenthesis as `?`, so that
// the word is one the shell makes, which B.2 and C read; this reading is
// asked as well as the first, never in place of it, so it only refuses more.
//
// A qualifier that runs code or adds words is refused where the call may
// run: zsh runs `(e:…:)` and `(+name)` in the shell itself, and
// `sdlc(e:'export GOFLAGS=-run=nothing':) freeze` ran the call with the
// setting and nothing in front of it. A line that nests parentheses past
// maxNesting is not read this way, and is refused where it may run the call.
func qualifiedConfiguredRun(runs []ran, text string, reading assignments) (Finding, bool) {
	q := zshQualified(text)
	if q.deep && mayRunTheCall(runs, text) {
		return configuredRunFinding(causeNames), true
	}
	if !q.found {
		return Finding{}, false
	}
	runs, _ = programsAt(q.text, false, assignments{released: reading.released, met: new(metWords)})
	if q.runsCode != "" {
		if _, found, _, front := bashCallRuns(runs); found || front || namesTheCall(q.runsCode) {
			return configuredRunFinding(causeNames), true
		}
	}
	return bashConfiguredRun(runs, q.text)
}

// qualified is a line with each parenthesis zsh reads as part of the word in
// front of it written as `?`, whether there was one, the text of those that
// run code or add words, and whether the line nests past maxNesting.
type qualified struct {
	text     string
	found    bool
	runsCode string
	deep     bool
}

// zshQualified finds, outside quotes and comments, a parenthesis stuck to the
// end of a word, whose body is not empty and which is not `$(`, `<(`, `>(`,
// `=(`, `((` or a negation's `!(`, as zsh reads them all as something else,
// and writes it, to the `)` that closes it or to the end, as `?`. A
// function's `()` is empty, and one with a space before it is a subshell. It
// follows quotes, backslashes, `$(…)`, `${…}` and backticks, reading inside
// them as it reads the line, since a qualifier there runs as much as one
// outside, and reads a qualifier's body in the same pass, so that every byte
// is read once.
func zshQualified(text string) qualified {
	if !strings.Contains(text, "(") {
		return qualified{text: text}
	}
	z := qualifierScan{text: text}
	z.commands(0, 0)
	if !z.found || z.deep {
		return qualified{text: text, deep: z.deep}
	}
	z.out = append(z.out, text[z.from:]...)
	return qualified{text: string(z.out), found: true, runsCode: z.code.String()}
}

// qualifierScan is zshQualified's state: the line, the text written so far
// and where the rest of it starts, what it found, how deep it is, and whether
// it is in a qualifier's body, which is part of a word and holds no comment:
// read as one, the `#` of `(#q.)` hid the call after it.
type qualifierScan struct {
	text  string
	out   []byte
	from  int
	found bool
	code  strings.Builder
	depth int
	deep  bool
	word  bool
}

// commands reads commands from i to the byte closer, `)`, `}` or a backtick,
// or to the end where closer is 0, and gives the index of the closer, or
// len(text).
func (z *qualifierScan) commands(i int, closer byte) int {
	text := z.text
	for i < len(text) {
		c := text[i]
		switch {
		case closer != 0 && c == closer:
			return i
		case c == '\\':
			i += 2
		case c == '\'':
			i = singleQuoteEnd(text, i) + 1
		case c == '"':
			i = z.doubleQuoted(i+1) + 1
		case c == '#' && !z.word && (i == 0 || strings.IndexByte(" \t\n;&|()", text[i-1]) >= 0):
			if j := strings.IndexByte(text[i:], '\n'); j >= 0 {
				i += j
			} else {
				i = len(text)
			}
		case c == '(' && stuckToAWord(text, i):
			i = z.qualifier(i)
		case c == '(':
			i = z.nested(i+1, ')', z.word) + 1
		default:
			i = z.expansion(i)
		}
	}
	return len(text)
}

// doubleQuoted reads a double-quoted body from i, following backslashes,
// `$(…)`, `${…}` and backticks, and gives the index of its closing quote.
func (z *qualifierScan) doubleQuoted(i int) int {
	text := z.text
	for i < len(text) {
		switch text[i] {
		case '"':
			return i
		case '\\':
			i += 2
		default:
			i = z.expansion(i)
		}
	}
	return len(text)
}

// expansion reads the backtick, `$(` or `${` at i, inside which the shell
// runs what it reads, and gives where the scan goes on: past its end, or past
// i where none opens there.
func (z *qualifierScan) expansion(i int) int {
	text := z.text
	switch {
	case text[i] == '`':
		return z.nested(i+1, '`', false) + 1
	case text[i] == '$' && i+1 < len(text) && text[i+1] == '(':
		return z.nested(i+2, ')', false) + 1
	case text[i] == '$' && i+1 < len(text) && text[i+1] == '{':
		return z.nested(i+2, '}', z.word) + 1
	}
	return i + 1
}

// nested reads from i to closer one level down, as commands or, where word is
// set, as part of a word, and gives the index of the closer. Past maxNesting
// it reads no further and marks the line deep, so that the stack stays
// bounded however deep a line nests.
func (z *qualifierScan) nested(i int, closer byte, word bool) int {
	if z.depth >= maxNesting {
		z.deep = true
		return len(z.text)
	}
	was := z.word
	z.depth, z.word = z.depth+1, word
	end := z.commands(i, closer)
	z.depth, z.word = z.depth-1, was
	return end
}

// qualifier reads the parenthesis that opens at i, and each stuck to the end
// of the one before, and writes each whose body is not empty as `?`, and gives
// where the scan goes on. Its body is read as commands, so a qualifier inside
// it is found too, and then written over with it.
func (z *qualifierScan) qualifier(i int) int {
	for {
		z.out = append(z.out, z.text[z.from:i]...)
		z.from = i
		mark := len(z.out)
		end := z.nested(i+1, ')', true)
		if z.deep {
			return len(z.text)
		}
		body := z.text[i+1 : end]
		if strings.TrimSpace(body) == "" {
			return end + 1
		}
		z.out = append(z.out[:mark], '?')
		z.from, z.found = min(end+1, len(z.text)), true
		if qualifierRunsCode(body) {
			z.code.WriteString(body)
			z.code.WriteByte('\n')
		}
		if i = end + 1; i >= len(z.text) || z.text[i] != '(' {
			return i
		}
	}
}

// singleQuoteEnd is the index of the `'` that closes the quote opening at i,
// a backslash followed inside `$'…'`, or len(text).
func singleQuoteEnd(text string, i int) int {
	ansi := i > 0 && text[i-1] == '$'
	for j := i + 1; j < len(text); j++ {
		switch {
		case ansi && text[j] == '\\':
			j++
		case text[j] == '\'':
			return j
		}
	}
	return len(text)
}

// stuckToAWord reports a `(` at i that zsh reads as part of the word before
// it: straight after a byte of a word, which a blank, an operator, `$`, `=`,
// `<`, `>` and `(` are not, nor a `!` that starts a word.
func stuckToAWord(text string, i int) bool {
	if i == 0 {
		return false
	}
	switch prev := text[i-1]; {
	case strings.IndexByte(" \t\n;&|()<>$={", prev) >= 0:
		return false
	case prev == '!':
		return i >= 2 && strings.IndexByte(" \t\n;&|()<>$={!", text[i-2]) < 0
	}
	return true
}

// qualifierRunsCode reports glob qualifiers that run code or add words, read
// up to the first `:`, where the modifiers start: e and + run code, and P puts
// a word before each match, so `(P:env:)` makes env the program. A `+` that
// compares a size or a time, `(L+10)`, and the end flag `(#e)` are read as
// one too, which only refuses more on a line that may run the call.
func qualifierRunsCode(body string) bool {
	flags, _, _ := strings.Cut(body, ":")
	return strings.ContainsAny(flags, "e+P")
}

// bashConfiguredRun is the rule on a Bash line: B finds the call, C reads the
// words in front of it, A reads what the line changes, and D what it hands a
// shell or an interpreter as text.
func bashConfiguredRun(runs []ran, text string) (Finding, bool) {
	if !mayRunTheCall(runs, text) {
		return Finding{}, false
	}
	callRun, found, ownChange, front := bashCallRuns(runs)
	if front || found && (ownChange || lineChanges(runs, callRun, blankedCalls(text, runs, callRun))) {
		return configuredRunFinding(causeNames), true
	}
	// B.3: a made command the reader lists no run for.
	if shaped, envSet := madeWholeCall(text); shaped && (envSet || lineChanges(runs, callRun, text)) {
		return configuredRunFinding(causeNames), true
	}
	if handedAsText(runs, text) {
		return configuredRunFinding(causeNames), true
	}
	return Finding{}, false
}

// bashCallRuns marks the runs B or C finds the call in, and says whether any
// was, whether a call's own run changes what it starts with (A.1), and whether
// C refuses a word in front of one.
func bashCallRuns(runs []ran) (callRun []bool, found, ownChange, front bool) {
	callRun = make([]bool, len(runs))
	for i, r := range runs {
		if r.extra {
			continue
		}
		at, ok := callIn(r, false)
		switch {
		case ok:
			// C: every word in front of the call is one the rule allows.
			words, _ := linedUp(r.words, r.marked)
			front = front || !allowedInFront(slices.Concat(r.wrappers, words[:at]))
		case madeProgramCall(r, false):
		default:
			continue
		}
		callRun[i], found = true, true
		ownChange = ownChange || changesItsOwnRun(r)
	}
	return callRun, found, ownChange, front
}

// mayRunTheCall is the first test, which keeps the rule cheap without
// skipping a line for holding no literal sdlc (security F-9): the text, with
// quotes and backslashes out and folded, holds sdlc; or a mark of any run is
// made by the shell (security F-18); or the text holds `$`, a backtick, or a
// `{` with a `,` before its `}`, the shapes madeWholeCall reads.
func mayRunTheCall(runs []ran, text string) bool {
	if strings.Contains(strings.ToLower(unescaped.Replace(text)), "sdlc") ||
		strings.ContainsAny(text, "$`") || braceList(text) {
		return true
	}
	return slices.ContainsFunc(runs, func(r ran) bool {
		return slices.ContainsFunc(r.marked, func(m string) bool { return madeWord(m, false) })
	})
}

// expandingDocument reports a here-document whose delimiter is not quoted
// and whose body changes what the call starts with, or runs the call. The
// readings take every body out before the rules read the line, since a
// document is text; but where the delimiter is not quoted the shell runs the
// body's substitutions and evaluates its arithmetic and assigning expansions,
// in the shell itself where a builtin or a group reads it, and on zsh in the
// call's own process for the call's own document. bash and zsh ran a
// stand-in sdlc with GOFLAGS=-v from `$(GOFLAGS=-v sdlc freeze)` in the body
// of `: <<E`, with HOME=21 after `$(( HOME=21 ))` in such a body before the
// call, and zsh with HOME=32 for `sdlc freeze <<E` over `$(( HOME=32 ))`
// (code review, security F-6). A quoted delimiter, `<<'EOF'`, keeps its body
// as written.
//
// On a line that names the call, such a body refuses where it holds an
// expansion (bodyOpener), since neither call takes a document. On every line,
// each expansion is read from where it opens to the end of its body, after
// text, the line as this reading has it, and refuses where this rule refuses
// that: so `$(GOFLAGS=x sdl? freeze)` in a body is read as it is anywhere
// else, and a review handed to sdlc review add with `git commit` in backticks
// stays text. Read to its end and not to its closer, an expansion's end found
// early only makes the reading longer: a `)` in double quotes, a case pattern
// or a comment ended `$(echo ")"; GOFLAGS=-v $(printf sd%s lc) freeze)` before
// its call, and bash and zsh ran it (security F-8). Read one by one, a quote
// in the text between two cannot hide the second, and with text first it
// cannot hide the call: the rule refuses a change after the call as before it.
// Each reading reads the rest of the body again, so past maxDocumentReading
// bytes between them the line refuses.
func expandingDocument(line, text string, s State, reading assignments) bool {
	if !strings.Contains(line, "<<") {
		return false
	}
	tails, refused := documentTails(line, text)
	if refused {
		return true
	}
	// In each reading a line is given, as `s\dlc` is sdlc only once decoded;
	// and without the line it came from, so that no document is read again.
	inner := s
	inner.seen = nil
	return slices.ContainsFunc(tails, func(tail string) bool {
		return slices.ContainsFunc(readings(tail, false, reading), func(p pair) bool {
			runs, _ := programsAt(p.text, false, reading)
			_, refused := checkConfiguredRun(runs, p.text, inner, reading)
			return refused
		})
	})
}

// documentTails is each expansion in the bodies on line whose delimiter is not
// quoted, from its opener to the end of its body, after text; or refused,
// where line names the call and holds one, or they pass maxDocumentReading.
func documentTails(line, text string) (tails []string, refused bool) {
	named := namesTheCall(line)
	var scan documentScan
	read := 0
	lines := strings.Split(line, "\n")
	for i := 0; i < len(lines); i++ {
		_, i = scan.opening(lines, i, false)
		for _, d := range scan.delimiters() {
			var body string
			i, body = pastBody(lines, i, d)
			for _, m := range bodyOpener().FindAllStringIndex(body, -1) {
				if opensNothing(body[m[0]:m[1]]) {
					continue
				}
				if read += len(text) + len(body) - m[0]; named || read > maxDocumentReading {
					return nil, true
				}
				tails = append(tails, text+"\n: "+body[m[0]:])
			}
		}
	}
	return tails, false
}

// opensNothing reports a match of bodyOpener that is an escaped character or
// a plain `${name}`.
func opensNothing(m string) bool { return m[0] == '\\' || m[len(m)-1] == '}' }

// maxDocumentReading is how many bytes expandingDocument reads again between
// the expansions of a line's bodies. Such a reading costs more than its
// length: one over 16 KB of substitutions cost the whole line 75 ms, one over
// 64 KB 800 ms, and sixteen over 240 KB nearly three minutes.
const maxDocumentReading = 1 << 15

// pastBody is the index of the line that closes the body d opens after
// lines[i], as pastDocuments reads it, and the body as the shell expands it,
// or "" where the delimiter is quoted and the shell expands nothing.
func pastBody(lines []string, i int, d delimiter) (int, string) {
	var body []string
	for closed := false; !closed && i+1 < len(lines); {
		var l string
		l, i = joined(lines, i+1, !d.quoted)
		if closed = l == d.word || d.dash && strings.TrimLeft(l, "\t") == d.word; !closed && !d.quoted {
			body = append(body, l)
		}
	}
	return i, strings.Join(body, "\n")
}

// bodyOpener is where the shell expands something in a body whose delimiter
// is not quoted: `$(`, a backtick, `$[`, `${`, and zsh's subscript on a bare
// parameter, `$name[`, with or without flags before the name, whose index zsh
// evaluates as arithmetic: zsh 5.9 set HOME to 41 from `$path[HOME=41]` in
// the body of `: <<E`, and from `$#path[…]`, `$HOME[…]`, `$@[…]` and `$$[…]`
// likewise, where bash reads none of them (security F-7). It matches an
// escaped character and a plain `${name}` too, which expand nothing that can
// change or run anything, so that the caller passes over them. Every backtick
// opens one, the closing one too, since a backtick quoted inside a
// substitution would pair the rest the wrong way.
var bodyOpener = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile("(?s)\\\\.|\\$\\{\\w+\\}|\\$[(\\[{]|`|\\$[#+=~^]*" + parameterName + "\\[")
})

// braceList reports a `{` with a `,` before the `}` after it.
func braceList(text string) bool {
	for i := strings.IndexByte(text, '{'); i >= 0; {
		rest := text[i+1:]
		if comma, closes := strings.IndexByte(rest, ','), strings.IndexByte(rest, '}'); comma >= 0 && (closes < 0 || comma < closes) {
			return true
		}
		j := strings.IndexByte(rest, '{')
		if j < 0 {
			return false
		}
		i += 1 + j
	}
	return false
}

// callIn finds the call in a run's words and marks, and the index of its
// program word: sdlc written out, by name, by path or with a backslash,
// followed by words that read as the call (B.1); or a made word followed by
// the call or a tail of it written out (C, security F-18). Behind a program
// that only prints or reads its words, sdlc is only mentioned. A word after a
// flag may be the flag's value, as sdlcArgs reads it, and is the call there
// only with the call's words written out after it: a wrapper's option may take
// no value (`bwrap --setenv GOFLAGS -v sdlc freeze`), and `hash -p
// /usr/local/bin/sdlc "$X"` names a path for a later word to run.
//
// A run whose words had a substitution cut out of them, in its own words or
// in the command that handed it to a shell, may have lost its program word
// that way: `ssh host $(command -v sdlc) freeze` reaches the rule as a script
// run of `freeze`. Where its words are the call or a tail of it, the cut word
// is read as the made word in front of them, at 0.
func callIn(r ran, powerShell bool) (int, bool) {
	words, marks := linedUp(r.words, r.marked)
	if len(words) == 0 {
		return 0, false
	}
	if i := slices.Index(marks, "$SUB"); r.substituted && i < 0 && couldBeTheCall(words, marks, powerShell, false) {
		return 0, true
	}
	mentions := mentionsOnly(base(words[0]))
	for i := range words {
		if i > 0 && mentions {
			break
		}
		if callAt(words, marks, i, powerShell) {
			return i, true
		}
	}
	return 0, false
}

// callAt reports the call with its program word at words[i]: sdlc followed by
// words that read as the call, or by the call written out where sdlc may be a
// flag's value; or a made word followed by the call or a tail of it.
func callAt(words, marks []string, i int, powerShell bool) bool {
	if isSdlc(words[i]) {
		valued := i > 0 && strings.HasPrefix(words[i-1], "-") && words[i-1] != "--"
		if valued && couldBeTheCall(words[i+1:], words[i+1:], powerShell, false) ||
			!valued && readsAsTheCall(words[i+1:], marks[i+1:], powerShell) {
			return true
		}
	}
	return madeWord(marks[i], powerShell) && couldBeTheCall(words[i+1:], marks[i+1:], powerShell, false)
}

// positional is the words after sdlc that are no flag and no flag's value,
// each with whether the shell makes it.
func positional(words, marks []string, powerShell bool) (pos []string, made []bool) {
	for i := 0; i < len(words); i++ {
		if strings.HasPrefix(words[i], "-") {
			if ValueFlags[words[i]] {
				i++
			}
			continue
		}
		pos = append(pos, words[i])
		made = append(made, i < len(marks) && madeWord(marks[i], powerShell))
	}
	return pos, made
}

// firstPositionalMark is the mark of the first word positional reads.
func firstPositionalMark(words, marks []string) string {
	for i := 0; i < len(words) && i < len(marks); i++ {
		if strings.HasPrefix(words[i], "-") {
			if ValueFlags[words[i]] {
				i++
			}
			continue
		}
		return marks[i]
	}
	return ""
}

// readsAsTheCall reports words after sdlc that read as `freeze` or as `gate
// implementation pass`, an operand the shell makes counting as a match (B.1).
func readsAsTheCall(words, marks []string, powerShell bool) bool {
	pos, made := positional(words, marks, powerShell)
	is := func(i int, w string) bool { return i < len(pos) && (pos[i] == w || made[i]) }
	return is(0, "freeze") || is(0, "gate") && is(1, "implementation") && is(2, "pass")
}

// couldBeTheCall reports words after a made program word that may be the
// call's: `freeze`, `gate implementation pass`, or a tail of it, `pass` or
// `implementation pass` (security F-15). Where firstMade is set, a first word
// that expands a variable or a substitution counts too, since it may be any of
// them (B.2): `$SDLC $(echo freeze)`. A made word further on is not read, so a
// made builtin given a name and then a made value stays a change of its own,
// as agentenv.go reads it: `$(printf zfor%s mat) -f PATH "$PWD/fake:$PATH"`;
// and a `%` or a glob is not read, so that `reachedFirst(%q, %q)` in prose is
// no call.
func couldBeTheCall(words, marks []string, powerShell, firstMade bool) bool {
	pos, made := positional(words, marks, powerShell)
	if firstMade && len(made) > 0 && made[0] && strings.ContainsAny(firstPositionalMark(words, marks), "$`") {
		return true
	}
	at := func(i int, w string) bool { return i < len(pos) && pos[i] == w }
	return at(0, "freeze") || at(0, "gate") && at(1, "implementation") && at(2, "pass") ||
		at(0, "implementation") && at(1, "pass") || at(0, "pass")
}

// madeProgramCall reports a run whose program word the shell makes, past its
// leading assignments, followed by words that could be the call (B.2,
// security F-9): it cannot be ruled out as sdlc. An element of an array
// literal, a first mark with `=` before its first `$`, a PowerShell variable
// assigned, and a made word with nothing after it are not read;
// madeWholeCall reads the last from the text.
func madeProgramCall(r ran, powerShell bool) bool {
	words, marks := linedUp(r.words, r.marked)
	if r.literal != "" || len(marks) == 0 {
		return false
	}
	first := 0
	for first < len(marks) && assignmentShaped(marks[first]) {
		first++
	}
	if first+1 >= len(marks) || !madeWord(marks[first], powerShell) {
		return false
	}
	// [ and [[ are the test builtin and keyword, never a pattern: a bracket
	// that closes nothing matches only itself, and init's own
	// `if [ -n "${SDLC_RUN_DIR-}" ]` was refused as a made program.
	if m := marks[first]; !powerShell && (m == "[" || m == "[[") {
		return false
	}
	if head, _, _ := strings.Cut(marks[first], "$"); strings.Contains(head, "=") || assignsTo(marks[first+1]) {
		return false
	}
	// On PowerShell only the call or its tail is read here: E reads the call
	// operator given a made program before a made word itself, and the US-129
	// corpus pins `"$V" "$W"` to its answer there.
	return couldBeTheCall(words[first+1:], marks[first+1:], powerShell, !powerShell)
}

// assignmentShape is a word that assigns a variable, NAME=value, with a
// subscript or `+=`.
var assignmentShape = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\[[^\]]*\])?\+?=`)
})

// assignsTo reports PowerShell's assignment operators, which make the made
// word before them a variable assigned, not a program: `$x = …`, `$x ??= …`.
func assignsTo(w string) bool {
	return strings.HasSuffix(w, "=") && strings.Trim(w, "-+*/%?=") == "" && w != "=="
}

func assignmentShaped(w string) bool {
	return strings.Contains(w, "=") && assignmentShape().MatchString(w)
}

// allowedInFront reports words that may stand in front of the call: time, !,
// exec, command, nice and nohup, each bare, and -p straight after time or
// command (C). Anything else may hand the commands an environment no word on
// the line shows: firejail --env, direnv exec, npx's node_modules/.bin, a
// limit (security F-11).
//
// An empty word is a substitution the reader cut out and a wrapper's reader
// took for an option's value; the made word is read on its mark.
func allowedInFront(words []string) bool {
	for i, w := range words {
		switch w {
		case "", "time", "!", "exec", "command", "nice", "nohup":
			continue
		case "-p":
			if i > 0 && (words[i-1] == "time" || words[i-1] == "command") {
				continue
			}
		}
		return false
	}
	return true
}

// changesItsOwnRun reports a run of the call started with something set,
// emptied or unset, or handed to a shell, eval, xargs or find (A.1).
func changesItsOwnRun(r ran) bool {
	return len(r.env) > 0 || r.cleared || len(r.unsets) > 0 || r.script || r.piped || r.replace != ""
}

// lineChanges reports a line that changes the environment the call starts
// with, read from every run that is not a run of the call (A.2) and from the
// text (A.3).
func lineChanges(runs []ran, callRun []bool, text string) bool {
	for i, r := range runs {
		if !callRun[i] && !r.extra && changesTheEnvironment(r, text) {
			return true
		}
	}
	return textChanges(text) || limitsInText(text)
}

// limitsInText reports a simple command, read from the text, whose program
// sets a limit. The reader takes prlimit off as a wrapper with its options,
// so `prlimit --pid $$ --nofile=10:10`, which sets a limit of the shell that
// then runs the call, left no run at all, and the freeze's commands inherited
// the limit (security F-11).
func limitsInText(text string) bool {
	for _, words := range simpleCommands(text) {
		if words, _ = afterFront(withoutRedirects(words)); len(words) > 0 && limits(base(unescaped.Replace(words[0]))) {
			return true
		}
	}
	return false
}

// changesTheEnvironment reports a run, before the call or after it, that sets,
// defines or aliases; whose first word assigns; that agentenv.go's reading,
// asked for any name, takes as a change; or that is one of the builtins this
// rule reads for itself: those that run text or change what the shell runs
// with, the limits (security F-11), the keyword and allexport options
// (security F-17, F-24), and env, exec or command given an option other than
// -p (A.2).
func changesTheEnvironment(r ran, text string) bool {
	if len(r.sets) > 0 || r.defines || r.alias.name != "" {
		return true
	}
	if slices.ContainsFunc(r.wrappers, func(w string) bool { return limits(base(w)) }) {
		return true
	}
	words, marks := linedUp(r.words, r.marked)
	if len(words) == 0 {
		return false
	}
	if assignmentShaped(marks[0]) || anyName.takesProtected(r, text) {
		return true
	}
	switch name := base(words[0]); name {
	case "alias", "source", ".", "eval", "let", "builtin", "enable", "env":
		return true
	case "exec", "command":
		return len(words) > 1 && strings.HasPrefix(words[1], "-") && words[1] != "-p"
	case "set", "setopt", "shopt":
		return turnsOnAnOption(name, words, marks)
	case "hash":
		return hashSetsAPath(words, marks)
	default:
		return limits(name)
	}
}

// hashSetsAPath reports hash told which file a name runs: bash's -p, in any
// cluster of options before `--`, and zsh's name=path, -d's included. After
// `hash -p /tmp/w sdlc` the line's later sdlc runs /tmp/w, which can set what
// it likes and then run the real binary (code review, security F-5). A word
// the shell makes may be either, and an option after a name is read as one.
// `hash`, `hash -r`, `hash -t sdlc` and `hash sdlc` only read or forget.
func hashSetsAPath(words, marks []string) bool {
	options := true
	for i := 1; i < len(words); i++ {
		w := words[i]
		if madeWord(marks[i], false) || strings.Contains(w, "=") ||
			options && len(w) > 1 && w[0] == '-' && strings.Contains(w[1:], "p") {
			return true
		}
		options = options && w != "--"
	}
	return false
}

// limits reports a program that sets a limit the commands inherit, which can
// fail the freeze's run with tests that pass.
func limits(name string) bool {
	switch name {
	case "ulimit", "umask", "limit", "unlimit", "prlimit":
		return true
	}
	return false
}

// turnsOnAnOption reports set, setopt or shopt turning on bash's keyword
// option, under which a NAME=value word anywhere among a command's words goes
// into its environment, or allexport (security F-17, F-24). An option word
// the shell makes may be either once it runs: for set and shopt before `--`,
// after which a word is a positional parameter, and for setopt anywhere.
// `set -o pipefail`, `set -eux` and `shopt -s extglob` change nothing.
func turnsOnAnOption(name string, words, marks []string) bool {
	named, shoptO := false, false
	for i := 1; i < len(words); i++ {
		w := words[i]
		if w == "--" && name != "setopt" {
			return false
		}
		if madeWord(marks[i], false) {
			return true
		}
		cluster := len(w) > 1 && (w[0] == '-' || w[0] == '+') && w[1] != '-'
		if name == "shopt" {
			shoptO = shoptO || cluster && strings.Contains(w[1:], "o")
		}
		if optionWordTurnsOn(name, w, cluster, named, shoptO) {
			return true
		}
		named = cluster && strings.Contains(w[1:], "o")
	}
	return false
}

// optionWordTurnsOn reports one word of set, setopt or shopt that turns on
// the keyword or allexport option: cluster is an option cluster, named a word
// after -o or +o, and shoptO a shopt that has been given -o.
func optionWordTurnsOn(name, w string, cluster, named, shoptO bool) bool {
	if name == "shopt" {
		return shoptO && namesAnExportOption(w)
	}
	return (name == "setopt" || named) && namesAnExportOption(w) ||
		cluster && w[0] == '-' && strings.ContainsAny(w[1:], "ak")
}

// namesAnExportOption reports an option word that, folded and with `_` out,
// holds allexport or keyword.
func namesAnExportOption(w string) bool {
	f := strings.ReplaceAll(strings.ToLower(w), "_", "")
	return strings.Contains(f, "allexport") || strings.Contains(f, "keyword")
}

// textChanges reports text that assigns a variable in a way no run shows:
// agentenv.go's text form asked for any name, and the rule's own readers for
// an assigning `${…}` head (security F-16), an assignment to `$env:NAME` or
// `${env:NAME}` (security F-20), arithmetic that assigns or increments
// (arithmetic.go), a descriptor stored in a name, and .NET's
// SetEnvironmentVariable (A.3).
func textChanges(text string) bool {
	return anyName.setsProtectedText(text) || assigningHead(text) || setsByText().MatchString(text) ||
		arithmeticChanges(text) || namesADescriptor().MatchString(text)
}

// namesADescriptor is a redirection that opens a descriptor and stores its
// number in the name before it: `: {HOME}>/dev/null` sets HOME to 10 in bash
// and 11 in zsh, which also takes a blank before the operator. The name has
// to be written out; `{${x}}>` is an argument.
var namesADescriptor = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[\s;&|()])\{[A-Za-z_]\w*(?:\[[^\]]*\])?\}[ \t]*(?:[<>]|&>)`)
})

// setsByText is `$env:NAME` or `${env:NAME}` followed by an assignment
// operator, spaced or not, and SetEnvironmentVariable( read on any line.
var setsByText = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`(?i)\$\{?env:[a-z_]\w*\}?\s*(?:[-+*/%]|\?\?)?=(?:[^=]|$)|setenvironmentvariable\(`)
})

// assigningHead reports a `${` whose head an assigning operator closes: `=`,
// `:=` or `::=`, after any flags in `(…)`, a name, digits or a nested `${…}`
// balanced to any depth, and any subscript. `${GOFLAGS:-none=set}`,
// `${(P)${:-PATH}}` and `${#PATH}` are reads.
//
// Where each head's bracket and brace close is looked up in tables built in
// one pass: searched for from each `${`, the line was read again for every
// one, and 100,000 of them nested took a third of a second, growing with the
// square of the line.
func assigningHead(text string) bool {
	if !strings.Contains(text, "${") {
		return false
	}
	h := newHeads(text)
	for at := 0; ; {
		i := strings.Index(text[at:], "${")
		if i < 0 {
			return false
		}
		at += i + 2
		if h.assigns(at) {
			return true
		}
	}
}

// heads is what assigningHead looks up: for each index, the first `)` and
// `]` at or after it, and for each `{`, the `}` that closes it, braces
// balanced; len(text) where there is none.
type heads struct {
	text                 string
	paren, bracket, pair []int
}

func newHeads(text string) heads {
	n := len(text)
	h := heads{text: text, paren: make([]int, n+1), bracket: make([]int, n+1), pair: make([]int, n)}
	h.paren[n], h.bracket[n] = n, n
	for i := n - 1; i >= 0; i-- {
		h.paren[i], h.bracket[i] = h.paren[i+1], h.bracket[i+1]
		switch text[i] {
		case ')':
			h.paren[i] = i
		case ']':
			h.bracket[i] = i
		}
	}
	var open []int
	for i := range n {
		h.pair[i] = n
		switch text[i] {
		case '{':
			open = append(open, i)
		case '}':
			if len(open) > 0 {
				h.pair[open[len(open)-1]] = i
				open = open[:len(open)-1]
			}
		}
	}
	return h
}

// assigns reports the head of a `${` whose body starts at j closed by an
// assigning operator, as assigningHead reads it.
func (h heads) assigns(j int) bool {
	text := h.text
	j, ok := h.past(j, '(', h.paren)
	if !ok {
		return false
	}
	switch {
	case strings.HasPrefix(text[j:], "${"):
		if j = h.pair[j+1]; j >= len(text) {
			return false
		}
		j++
	case j < len(text) && nameByte(text[j]):
		for j < len(text) && nameByte(text[j]) {
			j++
		}
	default:
		return false
	}
	if j, ok = h.past(j, '[', h.bracket); !ok {
		return false
	}
	rest := text[j:]
	return strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":=") || strings.HasPrefix(rest, "::=")
}

// past is j past the text that open starts at j and the first closer after
// it ends, or j where open does not stand there; not ok where no closer
// comes.
func (h heads) past(j int, open byte, closer []int) (int, bool) {
	if j >= len(h.text) || h.text[j] != open {
		return j, true
	}
	k := closer[j]
	return k + 1, k < len(h.text)
}

// blankedCalls is text with the literal words of each run of the call blanked
// by where they stand, so that a --note quoting a setting is not read as one.
// The call's command is found again by scanLine, and nothing is blanked where
// the scan stopped before the end of that command, or where its words are not
// the run's words in number and in text: the whole text is then read, which
// only refuses more (security F-28). A word that holds an expansion stays,
// since the shell runs it before the call starts (red-team F-13).
func blankedCalls(text string, runs []ran, callRun []bool) string {
	scan := scanLine(text, false)
	stopped := scan.stop != "" || scan.runs
	used := make([]bool, len(scan.cmds))
	var b []byte
	for i, r := range runs {
		if !callRun[i] {
			continue
		}
		k := matchingCommand(scan.cmds, used, stopped, r.words)
		if k < 0 {
			continue
		}
		used[k] = true
		if b == nil {
			b = []byte(text)
		}
		blankLiterals(b, scan.cmds[k])
	}
	if b == nil {
		return text
	}
	return string(b)
}

// matchingCommand is the first command of cmds not used yet whose words are
// words, in number and in text, or -1. Where the scan stopped, a command it
// may not have read to its end matches nothing.
func matchingCommand(cmds []namedCommand, used []bool, stopped bool, words []string) int {
	for k, c := range cmds {
		if !used[k] && (!stopped || c.next != "") && slices.Equal(c.texts(), words) {
			return k
		}
	}
	return -1
}

// blankLiterals blanks each literal word of c in b, where it stands.
func blankLiterals(b []byte, c namedCommand) {
	for _, w := range c.words {
		if !w.literal() {
			continue
		}
		for j := w.pieces[0].start; j < w.end(); j++ {
			b[j] = ' '
		}
	}
}

// madeWholeCall reports a simple command, read from the text, whose words
// after what may stand in front are made words that may be the whole call:
// two substitutions in a row, or one word the shell splits into several
// (B.3, security F-9, F-15, F-25). The reader lists no run for these:
// `GOFLAGS=-v $(printf 'sd%s freeze' lc)` reads as an assignment alone. What
// may stand in front is leading assignments, the words C allows, env with its
// options and assignments, and if, elif, while and until; a redirection and
// its target are set aside. envSet is an env set aside with a setting or an
// option, which is a change itself.
func madeWholeCall(text string) (shaped, envSet bool) {
	for _, words := range simpleCommands(text) {
		words, env := afterFront(withoutRedirects(words))
		if len(words) >= 2 && substitution(words[0]) && substitution(words[1]) ||
			len(words) == 1 && splitsIntoWords(words[0]) {
			return true, env
		}
	}
	return false, false
}

// afterFront is words after what may stand in front of a made whole call, as
// madeWholeCall reads it, and whether an env among them was given a setting or
// an option.
func afterFront(words []string) ([]string, bool) {
	env := false
	for len(words) > 0 {
		switch w := words[0]; {
		case assignmentShaped(w), w == "!", w == "exec", w == "nice", w == "nohup", w == "if", w == "elif",
			w == "while", w == "until":
			words = words[1:]
		case w == "time" || w == "command":
			words = words[1:]
			if len(words) > 0 && words[0] == "-p" {
				words = words[1:]
			}
		case base(w) == "env":
			var set bool
			words, set = afterEnv(words[1:])
			env = env || set
		default:
			return words, env
		}
	}
	return words, env
}

// afterEnv is env's words after its options and assignments, and whether it
// was given either.
func afterEnv(words []string) ([]string, bool) {
	set := false
	for len(words) > 0 {
		w := words[0]
		switch {
		case w == "--":
			set = true
			words = words[1:]
		case w == "-u" || w == "-C" || w == "-S" || w == "--unset" || w == "--chdir" || w == "--split-string":
			set = true
			words = words[min(2, len(words)):]
		case strings.HasPrefix(w, "-") || assignmentShaped(w):
			set = true
			words = words[1:]
		default:
			return words, set
		}
	}
	return words, set
}

// redirection is a word that is a redirection's operator, with any descriptor
// in front of it and any it moves to after it.
var redirection = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[0-9]*(?:&>>?|>>?|<<<|<<-?|<>|<|>\|)(&[0-9-]*)?`)
})

// withoutRedirects is words with each redirection and its target taken out.
func withoutRedirects(words []string) []string {
	var out []string
	for i := 0; i < len(words); i++ {
		m := redirection().FindStringSubmatch(words[i])
		if m == nil {
			out = append(out, words[i])
			continue
		}
		if len(m[0]) == len(words[i]) && m[1] == "" {
			i++
		}
	}
	return out
}

// substitution reports a word that is one unquoted `$(…)` or backtick
// substitution, and nothing else.
func substitution(w string) bool {
	if strings.HasPrefix(w, "$((") {
		return false
	}
	if strings.HasPrefix(w, "$(") {
		return closingParen(w, 2) == len(w)-1
	}
	return len(w) > 1 && w[0] == '`' && strings.IndexByte(w[1:], '`') == len(w)-2
}

// closingParen is the index of the `)` that closes a `(` whose body starts at
// i, quotes followed and parentheses balanced, or len(w).
func closingParen(w string, i int) int { return closingOf(w, i, '(', ')') }

// closingOf is the index of the closer that ends what open started just
// before i, opens and closers balanced and single quotes skipped, or len(w)
// where none does.
func closingOf(w string, i int, open, closer byte) int {
	depth := 1
	for ; i < len(w); i++ {
		switch w[i] {
		case '\\':
			i++
		case '\'':
			if j := strings.IndexByte(w[i+1:], '\''); j >= 0 {
				i += j + 1
			}
		case open:
			depth++
		case closer:
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(w)
}

// quotedSplit is a double-quoted expansion that still splits into words:
// `"$@"`, any `"${@…}"`, `"${NAME[@]…}"`, and zsh's flags holding `@`.
var quotedSplit = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^"(?:\$@|\$\{@|\$\{[A-Za-z_]\w*\[@\]|\$\{\([^)]*@)`)
})

// splitsIntoWords reports one word the shell may split into several: an
// unquoted substitution or expansion, `$*`, zsh's `${=NAME}`, an unquoted
// brace list, or a quoted expansion that splits (security F-25 (b)). A quoted
// `"$X"` or `"$(…)"` is one word, and the call is more than one.
func splitsIntoWords(w string) bool {
	switch {
	case substitution(w):
		return true
	case strings.HasPrefix(w, "$") && !strings.HasPrefix(w, "$'") && !strings.HasPrefix(w, `$"`) &&
		!strings.ContainsAny(w, `'"`):
		return len(w) > 1
	case strings.HasPrefix(w, `"`):
		return quotedSplit().MatchString(w)
	}
	return !strings.ContainsAny(w, `'"`) && braceList(w)
}

// simpleCommands splits text into simple commands, each as its words with
// their quotes, at unquoted `;`, `&&`, `||`, `|`, `&` (not in a redirection),
// a newline, `(`, `)`, a `{` or `}` standing alone, and after then, do and
// else. It follows quotes, backslashes and `$(`/backtick nesting, and lists no
// commands for any other rule: it is madeWholeCall's, and reads one shape. A
// parenthesis glued to a word is the word's.
func simpleCommands(text string) [][]string {
	sp := commandSplitter{text: text, start: -1}
	for i := 0; i < len(text); i++ {
		if next, ended := sp.boundary(i); ended {
			i = next
			continue
		}
		if sp.start < 0 {
			sp.start = i
		}
		i = sp.skip(i)
	}
	sp.endCmd(len(text))
	return sp.out
}

// commandSplitter is simpleCommands' state: the commands so far, the one
// being read, and where the word being read started, or -1.
type commandSplitter struct {
	text  string
	out   [][]string
	cmd   []string
	start int
}

func (sp *commandSplitter) endWord(at int) {
	if sp.start < 0 {
		return
	}
	w := sp.text[sp.start:at]
	sp.start = -1
	if w == "{" || w == "}" || (w == "then" || w == "do" || w == "else") && len(sp.cmd) == 0 {
		if len(sp.cmd) > 0 {
			sp.out = append(sp.out, sp.cmd)
		}
		sp.cmd = nil
		return
	}
	sp.cmd = append(sp.cmd, w)
}

func (sp *commandSplitter) endCmd(at int) {
	sp.endWord(at)
	if len(sp.cmd) > 0 {
		sp.out = append(sp.out, sp.cmd)
	}
	sp.cmd = nil
}

// boundary ends a word or a command where text[i] is a blank or an operator,
// and gives where the scan goes on from.
func (sp *commandSplitter) boundary(i int) (int, bool) {
	text, c := sp.text, sp.text[i]
	switch {
	case c == ' ' || c == '\t':
		sp.endWord(i)
	case c == '(' && sp.start >= 0:
		// Glued to a word, a parenthesis is the word's: a zsh glob
		// qualifier, `f()`, or prose such as ``decoded(`…`)``, whose
		// backticks stand alone in no command.
		return closingParen(text, i+1), true
	case c == ';' || c == '\n' || c == '|' || c == '(' || c == ')':
		sp.endCmd(i)
	case c == '&' && (sp.start < 0 || i <= sp.start || strings.IndexByte("<>", text[i-1]) < 0):
		sp.endCmd(i)
	default:
		return i, false
	}
	return i, true
}

// skip steps over the quote, escape or substitution that starts at i, and
// gives where it ends.
func (sp *commandSplitter) skip(i int) int {
	text := sp.text
	switch text[i] {
	case '\\':
		i++
	case '\'':
		if j := strings.IndexByte(text[i+1:], '\''); j >= 0 {
			i += j + 1
		} else {
			i = len(text)
		}
	case '"':
		i = closingQuote(text, i+1)
	case '`':
		if j := strings.IndexByte(text[i+1:], '`'); j >= 0 {
			i += j + 1
		} else {
			i = len(text)
		}
	case '$':
		if i+1 < len(text) && text[i+1] == '(' {
			i = closingParen(text, i+2)
		}
	}
	return i
}

// closingQuote is the index of the `"` that closes a double-quoted string
// whose body starts at i, escapes and `$(…)` followed, or len(text)-1.
func closingQuote(text string, i int) int {
	for ; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			return i
		case '$':
			if i+1 < len(text) && text[i+1] == '(' {
				i = closingParen(text, i+2)
			}
		}
	}
	return len(text) - 1
}

// handedAsText reports text that names the call and hands it to something
// that runs text: an interpreter, xargs, find given a command, GNU parallel,
// a shell given input or on the right of a pipe, a made program word whose
// words name the call, su and runuser given a command, or a reader that runs
// a command of its choosing (D). The rule cannot read the call there, and
// refuses it as the project refuses an ambiguous reading.
//
// The readers are man, whose pager -P, MANPAGER or PAGER names; less and
// more, which run LESSOPEN; and rg given --pre. callIn reads sdlc behind them
// as only mentioned, so `GOFLAGS=-run=nothing rg --pre sdlc --regexp=x
// freeze` and `man -P 'sdlc freeze #' ls` ran the call with the setting
// (code review, security F-2).
func handedAsText(runs []ran, text string) bool {
	if !namesTheCall(strings.ReplaceAll(text, `\`, "")) {
		return false
	}
	if shellReadsText().MatchString(text) {
		return true
	}
	return slices.ContainsFunc(runs, runsText)
}

// runsText reports a run that runs text it is handed, as handedAsText reads it.
func runsText(r ran) bool {
	words, marks := linedUp(r.words, r.marked)
	if len(words) == 0 {
		return false
	}
	name := base(words[0])
	switch {
	case interpretsCode(name), name == "xargs", name == "parallel",
		name == "find" && slices.ContainsFunc(words, findRuns),
		len(r.wrappers) > 0 && (base(r.wrappers[0]) == "xargs" || base(r.wrappers[0]) == "parallel"),
		r.input && (namesAShell(words) || namesAShell(append([]string{""}, words[0]))),
		(name == "su" || name == "runuser") && slices.ContainsFunc(words, func(w string) bool {
			return w == "-c" || strings.HasPrefix(w, "--command")
		}),
		name == "man" || name == "less" || name == "more",
		name == "rg" && slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, "--pre") }),
		madeWord(marks[0], false) && namesTheCall(strings.Join(words[1:], " ")):
		return true
	}
	return false
}

// shellReadsText is a shell on the right of a pipe, or one given its input by
// a redirection, which the reader does not list as a run.
var shellReadsText = sync.OnceValue(func() *regexp.Regexp {
	const shells = `(?:\S*/)?(?:sh|bash|zsh|dash|ksh|mksh|fish|pwsh|powershell)\b`
	return regexp.MustCompile(`\|\s*` + shells + `|(?:^|[;&|(\s])` + shells + `[^;&|\n]*<`)
})

// powerShellConfiguredRun is E: on PowerShell, a line that may run the call
// must be the call alone, after at most one Set-Location or cd to an absolute
// path (security F-30, F-35 to F-37, F-39 to F-41, F-43, F-44; architect
// F-13). Where it applies it is the rule's whole answer, and A to D are not
// asked. It reads line, the command as written, as well as text, the reading,
// because the readings drop a double-quoted here-string's subexpressions.
func powerShellConfiguredRun(runs []ran, text, line string) (Finding, bool) {
	if !powerShellMayRunTheCall(runs, text, line) {
		return Finding{}, false
	}
	may, written := powerShellCallRuns(runs)
	quote, shell, named := powerShellReads(text, line)
	may = may || named
	if !may && !quote && !shell || callAloneAfterPrefix(line) {
		return Finding{}, false
	}
	switch {
	case quote:
		return configuredRunFinding(causeTypographicQuote), true
	case shell:
		return configuredRunFinding(causeShellGivenMore), true
	case written:
		return configuredRunFinding(causeBesideTheCall), true
	}
	return configuredRunFinding(causeNames), true
}

// powerShellCallRuns reports a run in which B or C finds the call, and
// whether sdlc is written out in one.
func powerShellCallRuns(runs []ran) (may, written bool) {
	for _, r := range runs {
		if r.extra {
			continue
		}
		if at, ok := callIn(r, true); ok {
			may = true
			words, _ := linedUp(r.words, r.marked)
			written = written || isSdlc(words[at])
		} else if madeProgramCall(r, true) {
			may = true
		}
	}
	return may, written
}

// powerShellReads reads the reading and the line as written: whether either
// holds a typographic quote, gives pwsh more than literal words in a
// statement at any depth, or may otherwise be the call.
func powerShellReads(text, line string) (quote, shell, may bool) {
	for _, t := range []string{text, line} {
		quote = quote || typographicQuote(t)
		may = may || namesTheCall(t)
		sp := splitPowerShell(t)
		for _, st := range sp.stmts {
			given, maybe := statementMayBeTheCall(sp.words(st))
			shell, may = shell || given, may || maybe
		}
		if line == text {
			break
		}
	}
	return quote, shell, may
}

// powerShellMayRunTheCall is E's first test: Bash's, or the text holds `&`,
// `(`, iex, Invoke-Expression, pwsh or powershell, folded, or a typographic
// quote, or names sdlc once `+`, quotes and backticks are out. `pwsh -ec …`
// holds neither sdlc nor `$`, and `('sd'+'lc')` gives sdlc only once `+` is
// out.
func powerShellMayRunTheCall(runs []ran, text, line string) bool {
	if mayRunTheCall(runs, text) {
		return true
	}
	for _, t := range []string{text, line} {
		lower := strings.ToLower(t)
		if strings.ContainsAny(t, "&(") || strings.Contains(lower, "iex") || strings.Contains(lower, "invoke-expression") ||
			strings.Contains(lower, "pwsh") || strings.Contains(lower, "powershell") || typographicQuote(t) ||
			strings.Contains(powerShellFolded(t), "sdlc") {
			return true
		}
	}
	return false
}

// typographicQuotes are U+2018 to U+201E, at which PowerShell ends a string
// as at an ASCII quote: ‘ ’ ‚ ‛ close a single-quoted one and “ ” „ a double-
// quoted one. The reader and E's split pair ASCII quotes only.
const typographicQuotes = "‘’‚‛“”„"

// typographicQuote reports text that holds one, anywhere (security F-44).
func typographicQuote(text string) bool { return strings.ContainsAny(text, typographicQuotes) }

// powerShellFolded is text with `+` and the blanks beside it taken out, then
// the ASCII and typographic quotes and backticks, and folded.
func powerShellFolded(text string) string {
	text = plusJoins().ReplaceAllString(text, "")
	return strings.ToLower(strings.Map(func(r rune) rune {
		if r == '\'' || r == '"' || r == '`' || strings.ContainsRune(typographicQuotes, r) {
			return -1
		}
		return r
	}, text))
}

var plusJoins = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`\s*\+\s*`) })

// namesSdlc is sdlc, or sdlc.exe, as a name of its own: after no letter,
// digit, `.` or `-`, and before no letter, digit, `/`, `\`, `.` or `-`. So
// `.sdlc/state` and `sdlc-hook.cmd` are not sdlc.
var namesSdlc = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[^\p{L}\p{N}.\-])sdlc(?:\.exe)?(?:$|[^\p{L}\p{N}/\\.\-])`)
})

var callWords = sync.OnceValue(func() [4]*regexp.Regexp {
	return [4]*regexp.Regexp{
		regexp.MustCompile(`\bfreeze\b`), regexp.MustCompile(`\bgate\b`),
		regexp.MustCompile(`\bimplementation\b`), regexp.MustCompile(`\bpass\b`),
	}
})

// namesTheCall reports text that, folded as powerShellFolded folds it, holds
// sdlc as a name of its own and, anywhere and in any order, freeze, or each of
// gate, implementation and pass, as whole words (security F-36, F-39). A
// process started any way, and text run as code by any invoker, then names
// it; so does a commit message, whose route is git commit -F.
func namesTheCall(text string) bool {
	f := powerShellFolded(text)
	if !strings.Contains(f, "sdlc") || !namesSdlc().MatchString(f) {
		return false
	}
	w := callWords()
	return w[0].MatchString(f) || w[1].MatchString(f) && w[2].MatchString(f) && w[3].MatchString(f)
}

// statementMayBeTheCall reads one statement's words, at any depth: whether
// pwsh or powershell is given the encoded switch or a word that is not
// literalShape (security F-37, F-41, F-43), and whether it may otherwise be the
// call: the call operator given a made program before words that could be the
// call's (security F-35, red-team F-20, F-25), Invoke-Expression given a
// literalShape or an expression, or sdlc given a parenthesised operand where the
// call's words stand (security F-35).
func statementMayBeTheCall(words []string) (shellGivenMore, may bool) {
	if len(words) == 0 {
		return false, false
	}
	prog, args, callOp := words[0], words[1:], false
	switch {
	case prog == "&" || prog == ".":
		if len(args) == 0 {
			return false, false
		}
		prog, args, callOp = args[0], args[1:], true
	case len(prog) > 1 && prog[0] == '&':
		prog, callOp = prog[1:], true
	}
	if powerShellProgram(prog) {
		return slices.ContainsFunc(args, func(a string) bool { return encodedSwitch(a) || !literalWord(a) }), false
	}
	if callOp && operatorMade(prog) && couldBeTheCallsWords(args) {
		return false, true
	}
	if name := strings.ToLower(prog); !callOp && (name == "iex" || name == "invoke-expression") {
		return false, len(args) > 0 && !variable().MatchString(args[0])
	}
	return false, parenthesisedOperand(words)
}

// powerShellProgram reports pwsh or powershell, in any case, with or without
// .exe, a directory in front or single quotes around.
func powerShellProgram(w string) bool {
	if len(w) > 1 && w[0] == '\'' && w[len(w)-1] == '\'' {
		w = w[1 : len(w)-1]
	}
	if i := strings.LastIndexAny(w, `/\`); i >= 0 {
		w = w[i+1:]
	}
	w = strings.TrimSuffix(strings.ToLower(w), ".exe")
	return w == "pwsh" || w == "powershell"
}

// encodedSwitch reports pwsh's encoded-command switch as pwsh reads its
// command line: one or two of `-` and U+2013 to U+2015, or one `/`, then ec
// or any start of encodedcommand, in any case (`-e`, `--ec`, `–ec`, `/enc`,
// `-EncodedCommand`).
func encodedSwitch(a string) bool {
	a = strings.ToLower(a)
	if rest, ok := strings.CutPrefix(a, "/"); ok {
		a = rest
	} else {
		n := 0
		for n < 2 {
			r, size := utf8.DecodeRuneInString(a)
			if r != '-' && (r < '–' || r > '―') {
				break
			}
			a, n = a[size:], n+1
		}
		if n == 0 {
			return false
		}
	}
	const whole = "encodedcommand"
	return a == "ec" || a != "" && len(a) <= len(whole) && whole[:len(a)] == a
}

// literalWord reports a word that is what it says: a plain word with none of
// $ @ ( ) " ' { } ; or a backtick, and no typographic quote; or one
// single-quoted string that is the whole word, two quotes inside standing
// for one, to the end of the line where none closes it, and with no U+2018 to
// U+201B inside (security F-43, F-50). A nested statement is one word of its
// parent, and not literal (security F-51).
func literalWord(w string) bool {
	return literalShape().MatchString(w)
}

var literalShape = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile("^(?:[^$@()\"'`{};‘-„]+|'(?:[^'‘-‛]|'')*'?)$")
})

// variable is a word that is a variable alone.
var variable = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^\$(?:\{[^}]*\}|[\w:]+)$`)
})

// operatorMade reports a word the call operator is given that names no
// program as written: a variable or a made word, a parenthesised expression,
// or a double-quoted string holding `$` or a backtick, which PowerShell 7
// reads as an escape such as `u{73}`.
func operatorMade(w string) bool {
	return strings.HasPrefix(w, "$") || strings.HasPrefix(w, "(") || strings.HasPrefix(w, "@") ||
		strings.HasPrefix(w, `"`) && strings.ContainsAny(w, "$`")
}

// couldBeTheCallsWords reports words after a made program that could be the
// call's: a made word, freeze, or gate and what may follow it.
func couldBeTheCallsWords(words []string) bool {
	if len(words) == 0 {
		return false
	}
	if slices.ContainsFunc(words, func(w string) bool {
		return operatorMade(w) && (w[0] != '"' || strings.Contains(w, "$"))
	}) {
		return true
	}
	word := func(i int) string { return strings.ToLower(strings.Trim(words[i], "'")) }
	return word(0) == "freeze" || word(0) == "gate" && (len(words) == 1 || word(1) == "implementation")
}

// parenthesisedOperand reports a statement that runs sdlc, by sdlcArgs and in
// any case, with a non-empty `(` operand where the call's words stand: first,
// after gate, or after gate implementation. `sdlc status (…)` is not this.
func parenthesisedOperand(words []string) bool {
	lower := make([]string, len(words))
	for i, w := range words {
		lower[i] = strings.ToLower(w)
	}
	args, ok := sdlcArgs(lower)
	if !ok {
		return false
	}
	pos, _ := positional(args, nil, true)
	paren := func(i int) bool {
		return i < len(pos) && strings.HasPrefix(pos[i], "(") && !strings.HasPrefix(strings.TrimLeft(pos[i][1:], " \t"), ")") &&
			len(strings.TrimLeft(pos[i][1:], " \t")) > 0
	}
	at := func(i int, w string) bool { return i < len(pos) && pos[i] == w }
	return paren(0) || at(0, "gate") && (paren(1) || at(1, "implementation") && paren(2))
}

// callAloneAfterPrefix reports a line that is the call alone, spaces and tabs
// trimmed from both ends, or one prefix and then the call alone. The prefix is
// Set-Location or cd, in any case, given one absolute file-system path as a
// plain word or a whole single-quoted string, then `;` or `&&` (architect
// F-13). sl, chdir, Push-Location, pushd, a provider path, `~`, a relative or
// built path and -Path refuse.
func callAloneAfterPrefix(line string) bool {
	t := strings.Trim(line, " \t")
	if m := locationPrefix().FindStringIndex(t); m != nil {
		t = t[m[1]:]
	}
	return callAlone(t)
}

var locationPrefix = sync.OnceValue(func() *regexp.Regexp {
	const absolute = `(?:/|[A-Za-z]:[\\/])`
	return regexp.MustCompile(`^(?i:set-location|cd)[ \t]+(?:` + absolute + `[A-Za-z0-9_./:=+\\-]*|'` + absolute +
		`(?:[^'\x{2018}-\x{201b}]|'')*')[ \t]*(?:;|&&)[ \t]*`)
})

// callAlone reports text that is sdlc written out followed by words that read
// as the call, every word a plain word of ASCII letters, digits and `_./:=+-`,
// or a whole single-quoted string with two quotes standing for one inside and no
// U+2018 to U+201B, the words apart by spaces or tabs. It is a lexer of its
// own, not a toggle on `'`: a quote inside a plain word is no word, so the
// line refuses there (red-team F-19, security F-33).
func callAlone(text string) bool {
	var words []string
	for text != "" {
		word, end, ok := callWord(text)
		if !ok {
			return false
		}
		words = append(words, word)
		text = text[end:]
		if text != "" && text[0] != ' ' && text[0] != '\t' {
			return false
		}
		text = strings.TrimLeft(text, " \t")
	}
	return len(words) > 1 && words[0] == "sdlc" && readsAsTheCall(words[1:], nil, true)
}

// callWord is the word text starts with, as callAlone reads it, and where it
// ends; not ok where text starts with no word.
func callWord(text string) (word string, end int, ok bool) {
	if text[0] != '\'' {
		for end < len(text) && callByte(text[end]) {
			end++
		}
		return text[:end], end, end > 0
	}
	end = 1
	for end < len(text) && (text[end] != '\'' || end+1 < len(text) && text[end+1] == '\'') {
		if text[end] == '\'' {
			end++
		}
		end++
	}
	if end >= len(text) || strings.ContainsAny(text[1:end], "‘’‚‛") {
		return "", 0, false
	}
	return strings.ReplaceAll(text[1:end], "''", "'"), end + 1, true
}

// callByte reports a byte of a plain word: an ASCII letter or digit, or one
// of `_./:=+-`.
func callByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_./:=+-", c) >= 0
}

// psSplit is a PowerShell line split into statements at every depth, by one
// pass over it (security F-40). A statement ends at `;`, `&&`, `||`, `|` or a
// newline outside quotes and here-strings, and the inside of every block
// `{…}`, `(…)`, `$(…)` and `@(…)`, a double-quoted string's or here-string's
// `$(…)` included, is statements of its own. Each statement is a span of the
// one buffer, and each quote or bracket that opens is recorded with where it
// closes, so that a statement's words step over a nested one without lexing it
// again: E is linear in the line however deep it nests (perf's round-11 note).
// A closer that closes nothing is text, and a quote or bracket left open runs
// to the end of the line, so no input makes the split fail (red-team F-28).
type psSplit struct {
	line  string
	stmts [][2]int
	// closes[i] is one more than where the quote or bracket opened at i
	// closes, or 0 where none opens there.
	closes []int
}

// psFrame is a bracket or a string the split is inside.
type psFrame struct {
	kind  byte // 'T' the line, '{' or '(' a block, '"' a string, 'h' a here-string
	open  int  // where it opened
	start int  // where the statement being read in it started
}

func splitPowerShell(line string) psSplit {
	p := psSplitter{sp: psSplit{line: line, closes: make([]int, len(line))}, stack: []psFrame{{kind: 'T', open: -1}}}
	for i := 0; i < len(line); {
		if top := p.top(); top.kind == '"' || top.kind == 'h' {
			i = p.inString(i, top)
		} else {
			i = p.inCode(i, top)
		}
	}
	for len(p.stack) > 0 {
		top := p.top()
		if top.kind != '"' && top.kind != 'h' {
			p.end(len(line))
		}
		p.closeAt(top.open, len(line)-1)
		p.pop()
	}
	return p.sp
}

// psSplitter is splitPowerShell's state: the split so far and the brackets
// and strings it is inside.
type psSplitter struct {
	sp    psSplit
	stack []psFrame
}

func (p *psSplitter) top() psFrame { return p.stack[len(p.stack)-1] }

func (p *psSplitter) pop() { p.stack = p.stack[:len(p.stack)-1] }

func (p *psSplitter) push(kind byte, open, start int) {
	p.stack = append(p.stack, psFrame{kind: kind, open: open, start: start})
}

// end ends the statement being read in the innermost frame at at.
func (p *psSplitter) end(at int) {
	f, line := &p.stack[len(p.stack)-1], p.sp.line
	// From the left only: TrimSpace would read the blanks at the end of the
	// line once for every frame still open there.
	if strings.IndexFunc(line[f.start:at], func(r rune) bool { return !unicode.IsSpace(r) }) >= 0 {
		p.sp.stmts = append(p.sp.stmts, [2]int{f.start, at})
	}
	f.start = at + 1
}

func (p *psSplitter) closeAt(open, at int) {
	if open >= 0 {
		p.sp.closes[open] = at + 1
	}
}

func (p *psSplitter) next(i int) byte {
	if i+1 < len(p.sp.line) {
		return p.sp.line[i+1]
	}
	return 0
}

// inString reads the byte at i inside a double-quoted string or here-string,
// and gives where the split goes on from.
func (p *psSplitter) inString(i int, top psFrame) int {
	line, c := p.sp.line, p.sp.line[i]
	switch {
	case c == '`':
		return i + 2
	case c == '$' && p.next(i) == '(':
		p.push('(', i, i+2)
		return i + 2
	case top.kind == '"' && c == '"' && p.next(i) == '"':
		return i + 2
	case top.kind == '"' && c == '"':
		p.closeAt(top.open, i)
		p.pop()
	case top.kind == 'h' && c == '\n' && strings.HasPrefix(line[i+1:], `"@`):
		p.closeAt(top.open, i+2)
		p.pop()
		return i + 3
	}
	return i + 1
}

// inCode reads the byte at i outside a string: a quote or a string opens,
// or inStatement reads it.
func (p *psSplitter) inCode(i int, top psFrame) int {
	line, c := p.sp.line, p.sp.line[i]
	switch {
	case c == '`':
		return i + 2
	case c == '\'':
		j := closingSingle(line, i)
		p.closeAt(i, j)
		return j + 1
	case c == '@' && (p.next(i) == '\'' || p.next(i) == '"') && hereStringOpens(line, i+2):
		if p.next(i) == '"' {
			p.push('h', i, i)
			return i + 2
		}
		j := strings.Index(line[i+2:], "\n'@")
		j = cmpOr(j < 0, len(line)-1, i+2+j+2)
		p.closeAt(i, j)
		return j + 1
	case c == '"':
		p.push('"', i, i)
		return i + 1
	case c == '$' && p.next(i) == '{':
		j := strings.IndexByte(line[i:], '}')
		j = cmpOr(j < 0, len(line)-1, i+j)
		p.closeAt(i, j)
		return j + 1
	}
	return p.inStatement(i, top)
}

// inStatement reads a bracket that opens or closes a block at i, or an end
// of a statement.
func (p *psSplitter) inStatement(i int, top psFrame) int {
	c := p.sp.line[i]
	switch {
	case (c == '$' || c == '@') && (p.next(i) == '(' || p.next(i) == '{'):
		p.push(p.next(i), i, i+2)
		return i + 2
	case c == '{' || c == '(':
		p.push(c, i, i+1)
	case c == '}' && top.kind == '{' || c == ')' && top.kind == '(':
		p.end(i)
		p.closeAt(top.open, i)
		p.pop()
	case c == ';' || c == '\n' || c == '|' && p.next(i) != '|':
		p.end(i)
	case c == '|' || c == '&' && p.next(i) == '&':
		p.end(i)
		p.stack[len(p.stack)-1].start = i + 2
		return i + 2
	}
	return i + 1
}

// cmpOr is a when cond holds, else b.
func cmpOr(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}

// closingSingle is where the single-quoted string opened at i closes, two
// quotes inside standing for one, or the end of the line where none closes it.
func closingSingle(line string, i int) int {
	for j := i + 1; j < len(line); j++ {
		if line[j] != '\'' {
			continue
		}
		if j+1 < len(line) && line[j+1] == '\'' {
			j++
			continue
		}
		return j
	}
	return len(line) - 1
}

// hereStringOpens reports that only blanks stand between a here-string's
// opening quote and the end of its line, as PowerShell requires. It reads the
// blanks only, so that `@'@'@'…` on one line is not read to its end each time.
func hereStringOpens(line string, i int) bool {
	for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
		i++
	}
	return i < len(line) && line[i] == '\n'
}

// words is a statement's words, split at blanks outside quotes and brackets.
// A string or a bracket is part of the word it stands in, stepped over by where
// it closes. A nested statement in a word is folded to its brackets around x,
// or around nothing where only blanks stand inside: its own statement reads
// what is in it, and a parent that read it again made 50,000 unclosed `{`
// quadratic. It still makes the word not literalShape (security F-51).
func (sp psSplit) words(st [2]int) []string {
	var out []string
	line, i := sp.line, st[0]
	for i < st[1] {
		for i < st[1] && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
			i++
		}
		start := i
		var nests bool
		i, nests = sp.wordEnd(i, st[1])
		if end := min(i, st[1]); end > start && !nests {
			out = append(out, line[start:end])
		} else if end > start {
			out = append(out, sp.fold(start, end))
		}
	}
	return out
}

// wordEnd is where the word that starts at i ends, before stop or past it
// where a string or bracket in it runs on, and whether a nested statement or
// a double-quoted string stands in it.
func (sp psSplit) wordEnd(i, stop int) (int, bool) {
	line, nests := sp.line, false
	for i < stop && line[i] != ' ' && line[i] != '\t' && line[i] != '\r' {
		switch {
		case sp.closes[i] > 0:
			nests = nests || sp.nested(i) >= 0 || line[i] == '"' || line[i] == '@' && i+1 < len(line) && line[i+1] == '"'
			i = sp.closes[i]
		case line[i] == '`':
			i += 2
		default:
			i++
		}
	}
	return i, nests
}

// nested is how many bytes open the nested statement at i, `{`, `(`, `$(`, `@(`
// or `@{`, or -1 where none opens there. `${…}` is a name, not a statement.
func (sp psSplit) nested(i int) int {
	switch c, line := sp.line[i], sp.line; {
	case c == '{' || c == '(':
		return 1
	case (c == '$' || c == '@') && i+1 < len(line) && line[i+1] == '(', c == '@' && i+1 < len(line) && line[i+1] == '{':
		return 2
	}
	return -1
}

// fold is the line from start to end with every nested statement in it,
// a double-quoted string's included, folded as words says.
func (sp psSplit) fold(start, end int) string {
	var b strings.Builder
	line := sp.line
	for i := start; i < end; {
		n := -1
		if sp.closes[i] > 0 {
			n = sp.nested(i)
		}
		if n < 0 {
			b.WriteByte(line[i])
			i++
			continue
		}
		closer, stop := byte(')'), sp.closes[i]
		if line[i+n-1] == '{' {
			closer = '}'
		}
		b.WriteString(line[i : i+n])
		for j := i + n; j < stop; j++ {
			if c := line[j]; c != ' ' && c != '\t' && c != '\r' && c != '\n' && (j != stop-1 || c != closer) {
				b.WriteByte('x')
				break
			}
		}
		b.WriteByte(closer)
		i = stop
	}
	return b.String()
}
