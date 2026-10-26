package shellpolicy

// The names of the readings, as the debug log gives them (US-035, D1): the
// command as it arrived, as the shell reads it, with `$'...'` decoded, and
// with its brace lists written out.
const (
	viaReleased = "released"
	viaShell    = "shell"
	viaDecoded  = "decoded"
	viaBraces   = "braces"
)

// Candidates are the names State.Considered is told a program by: the
// programs the rules key on. A program the command runs is told by its name
// here, never as the command spells it, and one outside these is not told at
// all, so that nothing a command says reaches the log (US-035, D1).
var Candidates = vocabulary()

// vocabulary is the verbs that change a file, the interpreters, and the
// programs the rules name otherwise.
func vocabulary() map[string]bool {
	out := map[string]bool{}
	for name := range mutating {
		out[name] = true
	}
	for name := range interpreters {
		out[name] = true
	}
	for _, name := range []string{"git", "sdlc", "claude", "sed", "perl", "awk", "gawk", "find", "mkdir"} {
		out[name] = true
	}
	return out
}

// inspection is one call of Inspect or ClaudeConfigWrites, shared by every
// copy of its State: the pairs Considered has been told, the reading being
// walked, and what Bound said of each word (linkbound.go).
type inspection struct {
	told   map[[2]string]bool
	via    string
	bounds map[string]error
	// keys is each word as it was asked, cleaned: a long command asks about
	// the same few words thousands of times, and cleaning each again was a
	// fifth of its time.
	keys map[string]string
	hits []linkHit
}

func newInspection() *inspection {
	return &inspection{told: map[[2]string]bool{}, bounds: map[string]error{}, keys: map[string]string{}}
}

// reading notes the reading the rules are about to walk.
func (in *inspection) reading(via string) {
	if in != nil {
		in.via = via
	}
}

// tell tells s.Considered the program words run, once for each reading that
// brings it in, where it is one of Candidates.
func (in *inspection) tell(s State, words []string) {
	if in == nil || s.Considered == nil || len(words) == 0 {
		return
	}
	name := base(words[0])
	key := [2]string{name, s.via}
	if !Candidates[name] || s.via == "" || in.told[key] {
		return
	}
	in.told[key] = true
	s.Considered(name, s.via)
}
