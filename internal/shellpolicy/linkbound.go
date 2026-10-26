package shellpolicy

import (
	"errors"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// A word is placed on disk by following the links on the way, and the hook
// bounds how many it follows: pathrules.WordLinks for one word, and
// pathrules.CommandLinks for the whole command. Past the first, the rest of a
// word was read by name, so a chain of 65 links into .sdlc/state was a path
// no rule knew; and a command of 60 words through 255 links each held the
// hook for 22.8 s of its 25 (US-035, from US-052). State.Bound says where a
// word is past either, and the command is then refused here.

// ruleTooManyLinks is the rule a command past a bound on links is refused
// under, with SDLC-E0081 for a word and SDLC-E0082 for the whole command.
const ruleTooManyLinks = "too-many-links"

// rulePathTooLong is the rule, with SDLC-E0083, for a word whose path is
// longer than the system looks up in one go. The walk stopped there as it did
// at a chain of links past the system's limit, and read the rest by name.
const rulePathTooLong = "path-too-long"

// workingDirectory is how a refusal names the directory the command starts in,
// which no word of it spells.
const workingDirectory = "the working directory"

// pastWordBound reports whether err, from Bound, is a bound of one word: the
// word is then answered as one that cannot be placed.
func pastWordBound(err error) bool {
	return errors.Is(err, pathrules.ErrTooManyLinks) || errors.Is(err, pathrules.ErrPathTooLong)
}

// linkHit is the first word Bound said was past a bound: what it said, the
// spelling it was asked, and the reading that asked.
type linkHit struct {
	err       error
	word, via string
	// quote, where set, is the word to quote in place of word as typed.
	quote string
}

// bounded is s with every callback that places a word on disk asking Bound
// first, so that no rule, and no rule written later, can place a word without
// it: Resolve, Shared, Outside, the checkouts' Place and the four that place
// a word against the person's own configuration. A word past the bound of one
// word is answered as one that cannot be placed, so that nothing reads the
// rest of it by name or walks it again. A word asked once the command's
// budget is spent is still handed on: the hook's resolver then places it by
// name without following a link, which costs a lookup a name, so a rule that
// refuses a word by its name anywhere in the command still does. That can
// only add a refusal, never an allow, because the command is refused at the
// end of inspectAs for the spent budget whatever the rules said.
func (s State) bounded() State {
	if s.seen == nil {
		s.seen = newInspection()
	}
	s.seen.hits = nil
	if s.Bound == nil {
		return s
	}
	in, bound := s.seen, s.Bound
	ask := func(word string) bool { return in.within(bound, word) }
	if resolve := s.Resolve; resolve != nil {
		s.Resolve = func(word string) string {
			if !ask(word) {
				return ""
			}
			return resolve(word)
		}
	}
	if shared := s.Shared; shared != nil {
		s.Shared = func(word string) []Landing {
			if !ask(word) {
				return nil
			}
			return shared(word)
		}
	}
	if outside := s.Outside; outside != nil {
		s.Outside = func(p string, below bool) bool { return ask(p) && outside(p, below) }
	}
	for _, f := range []*func(string) bool{&s.ClaudeConfig, &s.ClaudeConfigHeld, &s.AgentsConfig, &s.AgentsConfigHeld} {
		*f = askedFirst(*f, ask)
	}
	s.Checkouts = placedAfter(s.Checkouts, ask)
	return s
}

// askedFirst is check with ask said first, nil where check is.
func askedFirst(check, ask func(string) bool) func(string) bool {
	if check == nil {
		return nil
	}
	return func(word string) bool { return ask(word) && check(word) }
}

// placedAfter is checkouts with each word ask refuses placed nowhere, nil
// where checkouts is.
func placedAfter(checkouts func() CheckoutSet, ask func(string) bool) func() CheckoutSet {
	if checkouts == nil {
		return nil
	}
	return func() CheckoutSet {
		set := checkouts()
		if place := set.Place; place != nil {
			set.Place = func(word string) []string {
				if !ask(word) {
					return nil
				}
				return place(word)
			}
		}
		return set
	}
}

// within asks bound about word, once a command, and reports whether the word
// may be placed. Every spelling is asked, a glob's or a variable's included:
// bash leaves `%`, an unmatched `?` or `[`, and `{x}` without a comma as they
// are, and a glob's literal start is a path like any other. A word that begins
// with a dash is asked as well, as spell asks it: `./-x` cleans to `-x`, and
// after `--` or a redirect it is a file. An error that is neither bound is no
// answer, and the word is placed as before.
func (in *inspection) within(bound func(string) error, word string) bool {
	key, cleaned := in.keys[word]
	if !cleaned {
		key = strings.TrimPrefix(clean(word), "./")
		in.keys[word] = key
	}
	if key == "" {
		return true
	}
	err, asked := in.bounds[key]
	if !asked {
		err = bound(key)
		in.bounds[key] = err
	}
	switch {
	case pastWordBound(err):
		in.keep(err, key)
		return false
	case errors.Is(err, pathrules.ErrLinkBudget):
		in.keep(err, key)
	}
	return true
}

// keep holds a word past a bound, with the reading being walked.
func (in *inspection) keep(err error, word string) {
	in.hits = append(in.hits, linkHit{err: err, word: word, via: in.via})
}

// parents asks the bound of each directory `mkdir -p` makes, from dir. mkdir
// changes no file and no rule classifies it; with -p it walks the path one
// directory at a time, as no lookup does, and so goes through a chain of
// links past what the system follows in one lookup (AC-7). Plain mkdir looks
// its path up in one go, and is left alone (D7).
func (in *inspection) parents(s State, dir string, words []string) {
	if in == nil || s.Bound == nil || len(words) == 0 || base(words[0]) != "mkdir" {
		return
	}
	var operands []string
	parents := false
	for i := 1; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "--":
			operands = append(operands, words[i+1:]...)
			i = len(words)
		case w == "--parents":
			parents = true
		case w == "-m", w == "--mode":
			i++
		case strings.HasPrefix(w, "--"):
		case strings.HasPrefix(w, "-") && len(w) > 1:
			parents = parents || bundles(w, 'p')
		default:
			operands = append(operands, w)
		}
	}
	if !parents {
		return
	}
	for _, op := range operands {
		c := clean(op)
		if dir != "" && dir != "." && !isAbsolute(c) {
			c = path.Join(dir, c)
		}
		in.within(s.Bound, c)
	}
}

// bundles reports whether a word of short options holds the flag f, before
// any letter that takes the rest of the word as its value: `-vp` does, and
// `-m0p` is a mode of 0p.
func bundles(w string, f byte) bool {
	for i := 1; i < len(w); i++ {
		switch w[i] {
		case f:
			return true
		case 'm':
			return false
		}
	}
	return false
}

// linkFinding is the refusal for a word past a bound, or for a command the
// resolver passed one in, where no rule refused it first. Bound("") asks the
// resolver about the command alone: a walk no callback asked about, such as
// the one to where the command runs, charges the same budget.
func (in *inspection) linkFinding(s State, line string) (Finding, bool) {
	if in == nil || s.Bound == nil {
		return Finding{}, false
	}
	if len(in.hits) == 0 {
		return in.commandFinding(s)
	}
	hit := in.quotedHit(s, line)
	// A word joined to the working directory, or to the directory a cd moved
	// to, is quoted as typed after it, unless that directory is where the links
	// are. Asking costs a walk only on the way to a refusal. Below a working
	// directory past a bound, every relative word is past it for the directory,
	// and quoted, `python3 -c` there named `-c` as the path too long (US-035,
	// verifier round 7).
	switch {
	case errors.Is(hit.err, pathrules.ErrLinkBudget):
	case pastWordBound(s.DirBound) && strings.HasPrefix(hit.word, s.Dir+"/"):
		hit.err, hit.quote = s.DirBound, workingDirectory
	default:
		hit = in.dirQuote(s, hit, line)
	}
	if hit.quote == "" && hit.word != "" {
		hit.quote, _ = spelled(hit.word, line, s.Dir)
	}
	return hit.finding(), true
}

// commandFinding is the refusal for a command the resolver passed a bound in
// where no word asked about did. A working directory past the bound refuses
// every command run in it, echo included, and the refusal named no path a
// person could act on (US-035, security review); it names the working
// directory where the walk to it is where the bound was passed.
func (in *inspection) commandFinding(s State) (Finding, bool) {
	err := s.Bound("")
	if !pastWordBound(err) && !errors.Is(err, pathrules.ErrLinkBudget) {
		return Finding{}, false
	}
	hit := linkHit{err: err, via: in.via}
	if pastWordBound(s.DirBound) {
		hit.err, hit.quote = s.DirBound, workingDirectory
	}
	return hit.finding(), true
}

// quotedHit is the hit the refusal is for. The first decides the code, and
// its word is the one quoted: for a spent budget, the word that spent it. Only
// where the command spells no word it is, nor one it is below, is the first of
// its kind the command spells taken instead, reason and all: a rule asked first
// about a path it made itself. Lending the word alone gave a lookup-limit
// reason to a word over 64 (US-035, security review).
func (in *inspection) quotedHit(s State, line string) linkHit {
	hit := in.hits[0]
	if _, found := spelled(hit.word, line, s.Dir); found {
		return hit
	}
	budget := errors.Is(hit.err, pathrules.ErrLinkBudget)
	for _, h := range in.hits {
		if _, found := spelled(h.word, line, s.Dir); found && errors.Is(h.err, pathrules.ErrLinkBudget) == budget {
			return h
		}
	}
	return hit
}

// finding is the refusal for the hit, quoting the word it is to quote.
func (h *linkHit) finding() Finding {
	word := "a path this command reaches"
	if h.quote != "" {
		word = shortened(h.quote)
	}
	links := strconv.Itoa(pathrules.WordLinks)
	f := Finding{
		Rule:   ruleTooManyLinks,
		Reason: word + " goes through more than " + links + " links, more than sdlc follows in one path",
		Route:  "name the place it leads to directly, without the links, or ask the person where it leads",
		Via:    h.via,
		Code:   sdlcerr.TooManyLinks.String(),
	}
	switch {
	case errors.Is(h.err, pathrules.ErrLinkBudget):
		budget := strconv.Itoa(pathrules.CommandLinks)
		f.Reason = "the words of this command go through more than the " + budget +
			" links sdlc follows for one command, counted across all its words"
		if h.word != "" {
			f.Reason += ", and " + word + " is the word that took it past " + budget
		}
		f.Route = "split the command into several, each naming fewer paths through links, " +
			"or name each place directly, without the links"
		f.Code = sdlcerr.LinkBudgetSpent.String()
	case errors.Is(h.err, pathrules.ErrLookupLimit):
		f.Reason = word + " goes through more links than this system follows in one lookup, " +
			"so where it leads cannot be read"
	case errors.Is(h.err, pathrules.ErrPathTooLong):
		// The hook reads the path in full, from the root, whichever directory
		// the command runs in; so the route is a shorter path, never a nearer
		// directory, which names the same path in fewer words.
		f.Rule = rulePathTooLong
		f.Reason = "the full path of " + word + " is longer than this system looks up in one go, " +
			"so sdlc cannot tell where it leads"
		f.Route = "make the full path shorter, by moving or renaming the directories on the way to it, " +
			"or ask the person to run the command"
		f.Code = sdlcerr.PathTooLong.String()
	}
	return f
}

// typed is the word of line that is key, as the command spells it, quotes
// and all, for a command run at the project's root. A key a rule joined to
// the directory a `cd` left the command in is read without that directory, so
// the word quoted is the one typed after the cd and never the cd's own. Where
// no word is key, it is the longest word key is a path below: a rule asked
// about a path it made of one, as install -D's file in the directory it names.
// Otherwise it is key itself, as where the word is the value of an option.
func typed(key, line string) string {
	w, _ := spelled(key, line, "")
	return w
}

// spelled is typed for a command run in dir, and whether the command spells
// key or a word it is below. The spelling alone could not tell a word typed
// exactly as it cleans from one never typed, and E0082 quoted a later word for
// the one that spent the budget (US-035, code review). The rules join every
// relative word to dir, and read with it, a key from a session in a
// subdirectory was never found, and E0082 quoted `./a` for the word that
// spent the budget (US-035, code review round 2); so dir is read off the front
// of a key as a cd's directory is. The directory of a cd is a word every key
// joined to it is below, and quoted for it, it sent the person to a directory
// with no links in it (US-035, verifier round 4); so it is quoted only where
// nothing else is found.
func spelled(key, line, dir string) (string, bool) {
	words, moves := typedWords(line, dir)
	keys := []string{key}
	if dir != "" {
		moves = append([]typedWord{{clean: dir, move: true}}, moves...)
	}
	for _, m := range moves {
		if rest, ok := strings.CutPrefix(key, m.clean+"/"); ok && rest != "" {
			keys = append(keys, rest)
		}
	}
	for _, k := range keys {
		if w, ok := typedAs(words, k, false); ok {
			return w, true
		}
	}
	if w, ok := typedAs(words, key, true); ok {
		return w, true
	}
	return key, false
}

// typedWord is a word of the line as typed, what it cleans to, and whether it
// is the directory a cd moves to.
type typedWord struct {
	text, clean string
	move        bool
}

// typedAs is the word of words that is key, or else the longest one key is a
// path below, leaving out the directories a cd moves to unless moves is set.
func typedAs(words []typedWord, key string, moves bool) (string, bool) {
	below := ""
	for _, w := range words {
		switch {
		case w.move && !moves:
		case w.clean == key:
			return w.text, true
		case strings.HasPrefix(key, w.clean+"/") && len(w.text) > len(below):
			below = w.text
		}
	}
	return below, below != ""
}

// typedWords are the words of line, each as typed and cleaned, with a
// redirect's operator taken off the front and a separator off the end, and
// the cleaned directories each cd moves to, in order, for a command run in
// dir.
func typedWords(line, dir string) ([]typedWord, []typedWord) {
	r := wordReader{at: dir}
	for _, f := range lineWords(line) {
		r.field(f)
	}
	return r.out, r.moves
}

// wordReader gathers typedWords, following the cds of the line as it goes:
// whether the next word is a cd's operand, whether `--` has ended its
// options, and the directory the shell is in after the cds so far.
type wordReader struct {
	out, moves  []typedWord
	inCd, ended bool
	at          string
}

// field reads one blank-separated field of the line.
func (r *wordReader) field(f string) {
	for i, w := range []string{f, redirectTarget(f)} {
		w = strings.TrimRight(w, ";&|)")
		c := strings.TrimPrefix(clean(w), "./")
		if w == "" || c == "" || (i == 1 && w == strings.TrimRight(f, ";&|)")) {
			continue
		}
		skip, move := false, false
		if i == 0 {
			skip, move = r.cd(w, c)
		}
		if skip {
			continue
		}
		if move {
			r.moves = append(r.moves, typedWord{text: w, clean: r.at, move: true})
		}
		r.out = append(r.out, typedWord{text: w, clean: c, move: move})
	}
	if strings.ContainsAny(f, ";&|") {
		r.inCd = false
	}
}

// redirectTarget is the file a field that is a redirect names, `x` of `2>x` or
// `&>x`, and "" for any other field. Digits are taken off the front only
// before a redirect's operator: `123abc` is a word, not a redirect of fd 123
// (US-035, code review round 2). A copy of a descriptor, as `2>&1`, names no
// file.
func redirectTarget(f string) string {
	rest := strings.TrimLeft(f, "0123456789")
	target := strings.TrimLeft(rest, "&<>")
	if target == rest {
		return ""
	}
	if op := rest[:len(rest)-len(target)]; strings.HasSuffix(op, "&") && strings.Trim(target, "0123456789-") == "" {
		return ""
	}
	return target
}

// cd reads one word, as typed and cleaned: skip where it is a cd or one of its
// options, move where it is the directory a cd moves to.
func (r *wordReader) cd(w, c string) (skip, move bool) {
	switch {
	case cdPrograms[strings.ToLower(base(c))]:
		r.inCd, r.ended = true, false
		return true, false
	case !r.inCd:
		return false, false
	case !r.ended && w == "--":
		r.ended = true
		return true, false
	case !r.ended && strings.HasPrefix(w, "-") && w != "-":
		return true, false // an option of the cd
	}
	r.inCd = false
	// Where the shell is after it, as the rules join a word to it.
	if isAbsolute(c) || r.at == "" {
		r.at = c
	} else {
		r.at = path.Join(r.at, c)
	}
	return false, true
}

// dirQuote is h quoting, in place of its word, the first directory a cd in
// line moves to, as typed, that its word is below and that is itself past a
// bound. The reason is then the directory's own. A word joined to such a directory goes through its links too, and
// quoted alone, `y` after `cd lnk65` named a word with no link in it.
func (in *inspection) dirQuote(s State, h linkHit, line string) linkHit {
	_, moves := typedWords(line, s.Dir)
	for _, m := range moves {
		rest, below := strings.CutPrefix(h.word, m.clean+"/")
		if !below || rest == "" {
			continue
		}
		if err := in.dirBound(s.Bound, m.clean, rest, h.err); err != nil {
			h.err, h.quote = err, m.text
			return h
		}
	}
	return h
}

// dirBound is the bound the directory dir passes itself, asked of bound: nil,
// or a bound of one word (pastWordBound). Where a word below it, with rest after
// dir, passed the system's lookup limit (werr), dir's first name below is asked
// as well: Bound(dir) does not ask the system to follow dir's own last link,
// and a cd into a directory behind 33 to 64 links quoted the word after it
// (US-035, verifier round 5). A lookup of that name that stops for the
// system's limit has stopped in dir, since nothing looks up a name's own link.
func (in *inspection) dirBound(bound func(string) error, dir, rest string, werr error) error {
	if err := in.bound(bound, dir); pastWordBound(err) {
		return err
	}
	if !errors.Is(werr, pathrules.ErrLookupLimit) {
		return nil
	}
	name, _, _ := strings.Cut(rest, "/")
	if err := in.bound(bound, dir+"/"+name); errors.Is(err, pathrules.ErrLookupLimit) {
		return err
	}
	return nil
}

// bound is bound(key), asked once a command.
func (in *inspection) bound(bound func(string) error, key string) error {
	err, asked := in.bounds[key]
	if !asked {
		err = bound(key)
		in.bounds[key] = err
	}
	return err
}

// cdPrograms are the commands that move the shell to the directory they name.
var cdPrograms = map[string]bool{"cd": true, "pushd": true, "chdir": true, "set-location": true, "sl": true,
	"push-location": true}

// lineWords splits line at the blanks outside quotes, keeping the quotes, so
// that a quoted word with a space in it is one word.
func lineWords(line string) []string {
	var out []string
	var word strings.Builder
	quote := byte(0)
	for i := range len(line) {
		ch := line[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == ' ' || ch == '\t' || ch == '\n':
			if word.Len() > 0 {
				out = append(out, word.String())
				word.Reset()
			}
			continue
		}
		word.WriteByte(ch)
	}
	if word.Len() > 0 {
		out = append(out, word.String())
	}
	return out
}

// shortened is a word cut to its first and last 30 bytes where it is longer
// than 64, at the edges of runes: the refusal goes back to the model, and a
// word can be kilobytes long (THREATS P1).
func shortened(w string) string {
	const edge = 30
	if len(w) <= 2*edge+4 {
		return w
	}
	head, tail := edge, len(w)-edge
	for head > 0 && !utf8.RuneStart(w[head]) {
		head--
	}
	for tail < len(w) && !utf8.RuneStart(w[tail]) {
		tail++
	}
	return w[:head] + "..." + w[tail:]
}
