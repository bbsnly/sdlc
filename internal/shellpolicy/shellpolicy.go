// Package shellpolicy reads a shell command well enough to notice the two ways
// around everything else in this tool.
//
// Every other rule governs the file-writing tools. A shell command is not one
// of them, so `cat > .sdlc/stories/A-1/ANALYSIS.md` would walk straight past a
// rule that says only sdlc writes that file, and `git commit` would walk past
// the gate that exists to stand in front of it.
//
// This is pattern matching, not a shell. It is a discipline control, not a
// sandbox: it stops an assistant taking a shortcut, and it does not pretend to
// stop one that is determined to get out. Saying that plainly is better than
// implying a guarantee nobody can keep -- so the rules here are deliberately
// narrow, and each one names the sanctioned route rather than just refusing.
package shellpolicy

import (
	"path/filepath"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
)

// State is what the loop knows, worked out by the caller. Keeping it out here
// leaves this package a decision table that can be read and tested on its own.
type State struct {
	// CommitReady is whether the story has been through the gates that come
	// before committing. CommitWhy says what is missing when it has not.
	CommitReady bool
	CommitWhy   string

	// Frozen is every acceptance test the freeze holds, repository-relative
	// and slash-separated. Empty before the freeze, and before then there is
	// nothing here to protect.
	Frozen []string
}

// Finding is a refusal. An empty Rule means nothing objected.
type Finding struct {
	Rule   string
	Reason string
	Route  string
}

// Message is what the assistant reads.
func (f Finding) Message() string {
	if f.Rule == "" {
		return ""
	}
	return f.Reason + ". Instead: " + f.Route + " [" + f.Rule + "]"
}

// mutating commands: the ones that exist to change a file. A command that only
// reads is nobody's business here, which is why this is a list of verbs rather
// than a list of everything.
var mutating = map[string]bool{
	"rm": true, "mv": true, "cp": true, "tee": true, "dd": true,
	"truncate": true, "install": true, "ln": true, "chmod": true,
	"chown": true, "touch": true, "shred": true, "unlink": true,
	"rmdir": true, "sponge": true,
	// Editors that rewrite a file given on the command line. `sed -i` is the
	// most common way an agent changes a file from a shell, and the record it
	// would be changing is the loop's own evidence -- unlike a test, nothing
	// downstream notices afterwards. Named here whether or not the invocation
	// actually asks for in-place editing: a `sed` that only reads names its
	// input too, and refusing a read of the gate record costs nothing.
	"sed": true, "perl": true, "awk": true, "ed": true, "patch": true,
	"python": true, "python3": true, "ruby": true, "node": true,
	// Windows spellings, because Bash on Windows is not always a POSIX shell.
	"del": true, "erase": true, "move": true, "copy": true,
}

// Inspect reports the first rule that refuses this command.
//
// The rules do not depend on who is asking. A shell command that rewrites the
// loop's record is wrong from every role, including the one whose record it is,
// for the same reason the file-writing rules refuse it from everyone.
func Inspect(command string, s State) (Finding, bool) {
	for _, segment := range segments(command) {
		words, redirects, assigns := parse(segment)
		if len(words) == 0 && len(assigns) == 0 {
			continue
		}
		if f, ok := checkEnforcement(assigns); ok {
			return f, true
		}
		if f, ok := checkCommit(words, s); ok {
			return f, true
		}
		if f, ok := checkLoopState(words, redirects); ok {
			return f, true
		}
		if f, ok := checkFrozenTests(segment, words, redirects, s); ok {
			return f, true
		}
	}
	return Finding{}, false
}

// checkEnforcement stops a session turning the loop off from the inside. The
// switch exists for the person who started the session, and a switch an
// assistant can reach is not a control.
func checkEnforcement(assigns []string) (Finding, bool) {
	for _, name := range assigns {
		switch name {
		case "SDLC_ENFORCE", "SDLC_BIN", "CLAUDE_PROJECT_DIR":
			return Finding{
				Rule: "enforcement-stays-on",
				Reason: name + " decides whether the loop is enforced at all, and it is " +
					"set by the person who started the session, not from inside it",
				Route: "if a rule is wrong, say which one and why; `sdlc stop` ends the " +
					"iteration and hands the work back",
			}, true
		}
	}
	return Finding{}, false
}

// checkCommit puts the commit gate in front of the commit.
func checkCommit(words []string, s State) (Finding, bool) {
	if s.CommitReady || !isGit(words) || !hasWord(words[1:], "commit") {
		return Finding{}, false
	}
	return Finding{
		Rule:   "commit-gate",
		Reason: "this story has not been through the gates that come before committing: " + s.CommitWhy,
		Route: "finish the gates -- `sdlc status` shows where this story stands -- " +
			"or `sdlc stop` to end the iteration and commit as yourself",
	}, true
}

// checkLoopState stops the shell being the way around every other rule.
func checkLoopState(words, redirects []string) (Finding, bool) {
	writes := len(redirects) > 0 || (len(words) > 0 && mutating[base(words[0])])
	if !writes {
		return Finding{}, false
	}
	candidates := redirects
	if len(words) > 0 && mutating[base(words[0])] {
		candidates = append(candidates, words[1:]...)
	}
	for _, c := range candidates {
		if hit, ok := loopState(c); ok {
			return Finding{
				Rule: "loop-state-through-the-tool",
				Reason: hit + " is the loop's own record, and a shell command is the one " +
					"way around every rule that protects it",
				Route: "the sdlc command owns this: `sdlc artifact write` for a gate's " +
					"documents, `sdlc review add` for a review, `sdlc gate` for an " +
					"outcome, `sdlc unfreeze --reason ...` for the freeze",
			}, true
		}
	}
	return Finding{}, false
}

// checkFrozenTests is the freeze, applied to the shell.
//
// Every other rule the freeze has is enforced against the file tools, and a
// shell command went straight past them: `Write` to `x_test.go` was refused as
// a frozen acceptance test, and `echo cheat > x_test.go` was allowed. One
// redirect was the whole way round the hinge the loop turns on.
func checkFrozenTests(segment string, words, redirects []string, s State) (Finding, bool) {
	if len(s.Frozen) == 0 {
		return Finding{}, false
	}
	candidates := redirects
	switch {
	case runsInlineCode(words):
		// `python3 -c '...'` is a program, not a list of arguments, and the
		// file it opens is inside a string. There is no parsing this without
		// being an interpreter, so the whole of it is searched instead: a
		// frozen path appearing anywhere in code that runs is enough.
		candidates = append(candidates, wordsIn(segment)...)
	case changesAFile(words):
		candidates = append(candidates, words[1:]...)
	}
	for _, c := range candidates {
		if frozen, ok := isFrozen(c, s.Frozen); ok {
			return Finding{
				Rule: "frozen-test-through-the-tool",
				Reason: frozen + " is a frozen acceptance test, and a shell command is " +
					"the one way around every rule that protects it",
				Route: "leave it alone -- it was locked by content when the test gate " +
					"passed, and every gate after that is measured against it. If it " +
					"genuinely has to change, `sdlc unfreeze --reason ...` puts the " +
					"reason on the record",
			}, true
		}
	}
	return Finding{}, false
}

// isFrozen matches a word from a command line against the frozen set, allowing
// for the spellings the same file arrives under: as written, with a ./ in
// front, or as an absolute path.
func isFrozen(word string, frozen []string) (string, bool) {
	word = strings.TrimPrefix(filepath.ToSlash(clean(word)), "./")
	if word == "" {
		return "", false
	}
	for _, f := range frozen {
		switch {
		case strings.EqualFold(word, f),
			strings.HasSuffix(strings.ToLower(word), "/"+strings.ToLower(f)),
			strings.HasSuffix(strings.ToLower(f), "/"+strings.ToLower(word)):
			return f, true
		}
	}
	return "", false
}

// changesAFile reports whether this command, as it is written, exists to
// change a file.
//
// Narrower than the mutating list, and deliberately so. That list names `sed`
// whether or not the invocation edits in place, because refusing a read of the
// loop's record costs nothing. A frozen test is different: reading one is how
// the implementer knows what to implement, and `sed -n 1,20p x_test.go` has to
// go through.
func changesAFile(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch name := base(words[0]); name {
	case "sed", "perl":
		return hasFlagPrefix(words[1:], "-i", "--in-place")
	case "awk", "grep":
		// Neither writes where it is pointed; both need a redirect, which is
		// already counted.
		return false
	default:
		return mutating[name]
	}
}

// runsInlineCode reports whether this is an interpreter being handed a program
// on the command line, where the files it touches are not arguments at all.
func runsInlineCode(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch base(words[0]) {
	case "python", "python3", "perl", "ruby", "node", "deno", "bun", "php":
		return hasFlagPrefix(words[1:], "-c", "-e", "--eval", "--print")
	}
	return false
}

func hasFlagPrefix(words []string, prefixes ...string) bool {
	for _, w := range words {
		for _, p := range prefixes {
			if strings.HasPrefix(w, p) {
				return true
			}
		}
	}
	return false
}

// wordsIn pulls every path-shaped run of characters out of a piece of text, so
// that a filename quoted inside a program is still a filename.
func wordsIn(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !inAPath(r) })
}

// inAPath reports whether a rune can appear in a path this cares about.
func inAPath(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '/', r == '_', r == '-':
		return true
	}
	return false
}

// segments splits a command line into the pieces that run on their own. It does
// not understand quoting well enough to be a shell, and does not need to: a
// separator hidden inside quotes makes one segment out of two, which can only
// make this notice more, never less.
func segments(command string) []string {
	replacer := strings.NewReplacer(
		"&&", "\n", "||", "\n", ";", "\n", "|", "\n", "`", "\n", "$(", "\n", ")", "\n",
	)
	var out []string
	for _, line := range strings.Split(replacer.Replace(command), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// parse splits one segment into its words, the targets of its redirections, and
// the names of any environment assignments in front of the command.
func parse(segment string) (words, redirects, assigns []string) {
	fields := strings.Fields(strings.NewReplacer(">>", " > ", ">", " > ", "<", " < ").Replace(segment))
	leading := true
	for i := 0; i < len(fields); i++ {
		f := unquote(fields[i])
		switch {
		case f == ">":
			if i+1 < len(fields) {
				redirects = append(redirects, unquote(fields[i+1]))
				i++
			}
		case f == "<":
			i++
		case isAssignment(f) && (leading || exporting(words)):
			name, _, _ := strings.Cut(f, "=")
			assigns = append(assigns, name)
		default:
			leading = false
			words = append(words, f)
		}
	}
	return words, redirects, assigns
}

func isAssignment(f string) bool {
	name, _, ok := strings.Cut(f, "=")
	if !ok || name == "" {
		return false
	}
	for _, r := range name {
		if !nameRune(r) {
			return false
		}
	}
	return true
}

// nameRune is what a shell allows in an environment variable's name.
func nameRune(r rune) bool {
	return r == '_' ||
		r >= 'A' && r <= 'Z' ||
		r >= 'a' && r <= 'z' ||
		r >= '0' && r <= '9'
}

// exporting reports whether the words so far are a command that takes
// NAME=VALUE arguments, so that `export SDLC_ENFORCE=0` is read the same way as
// `SDLC_ENFORCE=0 something`.
func exporting(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch base(words[0]) {
	case "export", "set", "declare", "typeset", "env", "setenv":
		return true
	}
	return false
}

func isGit(words []string) bool { return len(words) > 0 && base(words[0]) == "git" }

func hasWord(words []string, want string) bool {
	for _, w := range words {
		if w == want {
			return true
		}
	}
	return false
}

// loopState reports whether a word names something the loop owns.
//
// It is matched by shape rather than by a list of exact paths, so that it
// covers every story rather than only the one being worked on, and so that an
// absolute path, a "./" prefix or a Windows separator lands in the same place.
// A scratch file inside a story's directory is not loop state: the file-writing
// rules already allow those, and a shell rule that disagreed with them would be
// a rule nobody could follow.
func loopState(word string) (string, bool) {
	p := strings.TrimPrefix(clean(word), "./")
	if p == "" {
		return "", false
	}
	for _, own := range []string{".sdlc/state", ".sdlc/config.json", ".sdlc/claude-progress.json", ".git/hooks"} {
		if at(p, own) {
			return own, true
		}
	}
	// The directory itself, taken wholesale. Every path inside it was covered
	// and this was not, so `rm -rf .sdlc/state` was refused while `rm -rf
	// .sdlc` -- which destroys the same thing and more -- went through.
	if p == ".sdlc" || strings.HasSuffix(p, "/.sdlc") {
		return ".sdlc", true
	}
	rest, inStories := cutAt(p, ".sdlc/stories")
	if !inStories {
		return "", false
	}
	// .sdlc/stories itself, or a whole story directory, taken wholesale.
	if rest == "" || !strings.Contains(rest, "/") {
		return p, true
	}
	_, tail, _ := strings.Cut(rest, "/")
	if strings.HasPrefix(tail, "reviews/") || tail == "reviews" {
		return p, true
	}
	if tail == model.RecordFile {
		return p, true
	}
	if _, ok := model.ArtifactByFile(tail); ok {
		return p, true
	}
	return "", false
}

// at reports whether p is the named path or something inside it, wherever the
// named path sits in p.
func at(p, want string) bool {
	_, ok := cutAt(p, want)
	return ok
}

// cutAt finds want as a whole path segment run inside p and returns what
// follows it, without a leading slash.
func cutAt(p, want string) (string, bool) {
	switch {
	case p == want:
		return "", true
	case strings.HasPrefix(p, want+"/"):
		return strings.TrimPrefix(p, want+"/"), true
	}
	if i := strings.Index(p, "/"+want); i >= 0 {
		rest := p[i+1+len(want):]
		if rest == "" {
			return "", true
		}
		if strings.HasPrefix(rest, "/") {
			return strings.TrimPrefix(rest, "/"), true
		}
	}
	return "", false
}

func clean(word string) string {
	return strings.TrimSuffix(strings.ReplaceAll(unquote(word), "\\", "/"), "/")
}

func unquote(f string) string {
	for _, q := range []string{`"`, "'"} {
		if len(f) >= 2 && strings.HasPrefix(f, q) && strings.HasSuffix(f, q) {
			return f[1 : len(f)-1]
		}
	}
	return strings.Trim(f, `"'`)
}

func base(word string) string {
	word = clean(word)
	if i := strings.LastIndex(word, "/"); i >= 0 {
		return word[i+1:]
	}
	return word
}
