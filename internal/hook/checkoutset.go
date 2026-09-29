package hook

import (
	"path/filepath"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// checkoutSet is where the repository's checkouts are, for the shell rule that
// refuses taking one away: the project, which is the checkout the story is
// held in, then the main checkout and every linked worktree git has an entry
// for. It is built only for a command that could take something away.
//
// Where the main checkout could not be found, the set is what could be read,
// the project itself at least, and the rule stands for that much.
func checkoutSet(pl place, resolver pathrules.Resolver) shellpolicy.CheckoutSet {
	seen := map[string]bool{}
	var checkouts []string
	add := func(p string) {
		if p == "" {
			return
		}
		key := config.PathKey(p)
		if seen[key] {
			return
		}
		seen[key] = true
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		checkouts = append(checkouts, filepath.ToSlash(filepath.Clean(p)))
	}
	add(resolver.Root())
	add(pl.co.Main)
	for _, w := range pl.co.Worktrees() {
		add(w)
	}
	return shellpolicy.CheckoutSet{
		Checkouts: checkouts,
		Place:     func(word string) []string { return placeWord(resolver, word) },
	}
}

// placeWord is where a word of a shell command lands on disk, read the two
// ways land reads it: through links before `..` is taken apart, as a POSIX
// filesystem does, and by name first, as most of Windows does.
func placeWord(resolver pathrules.Resolver, word string) []string {
	word, ok := expandHome(word)
	if !ok || word == "" {
		return nil
	}
	word = filepath.FromSlash(word)
	var out []string
	if abs, ok := resolver.Place(word); ok {
		out = append(out, filepath.ToSlash(abs))
	}
	if lexical, ok := pathrules.Abs(resolver.Root(), word); ok {
		if cleaned, ok := resolver.Place(filepath.Clean(lexical)); ok {
			out = append(out, filepath.ToSlash(cleaned))
		}
	}
	return out
}
