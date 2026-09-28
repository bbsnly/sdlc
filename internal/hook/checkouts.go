package hook

import (
	"encoding/json"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// place is where a loop project sits among its repository's checkouts, found
// once per call from git's own files and never from git's variables, which
// name a directory anybody can make.
type place struct {
	project string
	co      config.Checkout
	// err is why the main checkout, where the backlog and the story records
	// are kept, could not be found.
	err error
}

func placeOf(project string) place {
	co, err := config.FindCheckout(project)
	return place{project: project, co: co, err: err}
}

// records is the directory the backlog and the story records are kept under:
// the main checkout, and false where it could not be found. A linked
// worktree's own copy of them, checked out by git, is not the story's record,
// and reading it there was failing open onto whatever the copy said.
func (pl place) records() (string, bool) {
	switch {
	case pl.err != nil:
		return "", false
	case pl.co.Linked():
		return pl.co.Main, true
	}
	return pl.project, true
}

// record reads the story's record from where it is kept. found is false where
// the main checkout could not be found; err is the record's own failure.
func (pl place) record(story string) (record *model.Record, found bool, err error) {
	dir, found := pl.records()
	if !found {
		return nil, false, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".sdlc", "stories", story, model.RecordFile))
	if err != nil {
		return nil, true, err
	}
	record = new(model.Record)
	if err := json.Unmarshal(raw, record); err != nil {
		return nil, true, err
	}
	return record, true, nil
}

// noMainCheckout is what the rules that read the story's record say where the
// main checkout could not be found.
const noMainCheckout = "the repository's main checkout, where the story's record is kept, could not be " +
	"found, so no gate can be shown to have passed (`sdlc doctor` says why)"

// landings says where a word of a shell command lands among the project's
// repository's other checkouts, for shellpolicy.State.Shared. It is nil where
// the project is its repository's only checkout, which is then held exactly as
// it always was.
//
// A checkout is found by walking up from where the word lands to a directory
// whose .git belongs to this repository, not by listing the worktrees: a list
// has an end a session could add worktrees to get past, and costs a read per
// entry on every call. Every such directory the walk passes counts, and a .git
// that does not belong -- a repository nested in a checkout, vendored or made
// with `git init` over the main checkout's .sdlc as a decoy -- is walked past.
type landings struct {
	resolver pathrules.Resolver
	project  string
	// own is the project's own git directory, where it is outside the project.
	own        string
	candidates []string
	// unplaced is a project whose .git leads to nothing that could be read,
	// where every word outside it might be the main checkout's.
	unplaced bool
	belong   map[string]bool
}

func newLandings(pl place, resolver pathrules.Resolver) *landings {
	candidates, readable := pl.co.Candidates()
	if !pl.co.Pointer() && !pl.co.HasWorktrees() {
		return nil
	}
	l := &landings{resolver: resolver, project: resolver.Root(), candidates: candidates,
		unplaced: !readable, belong: map[string]bool{}}
	if pl.co.GitDir != "" {
		if _, inside := pathrules.Within(l.project, pl.co.GitDir); !inside {
			l.own = pl.co.GitDir
		}
	}
	return l
}

// land is where word lands, relative to the project, where it does not land in
// the project itself. It is asked two ways, and either answer being another
// checkout's is enough to refuse: links followed before `..` is taken apart,
// as a POSIX filesystem does, and `..` taken apart by name before links are
// followed, as most of Windows does. The second is asked everywhere, because
// Place takes apart by name whatever follows a directory that is not there
// yet: with `mkdir -p d` earlier in the command, `d/../lnk/x` reached another
// checkout through lnk, and the first reading never followed it.
func (l *landings) land(word string) []shellpolicy.Landing {
	word, ok := expandHome(word)
	if !ok || word == "" {
		return nil
	}
	word = filepath.FromSlash(word)
	abs, ok := l.resolver.Place(word)
	if !ok {
		return nil
	}
	out := l.at(abs)
	if lexical, ok := pathrules.Abs(l.resolver.Root(), word); ok {
		if cleaned, ok := l.resolver.Place(filepath.Clean(lexical)); ok && cleaned != abs {
			out = append(out, l.at(cleaned)...)
		}
	}
	return out
}

func (l *landings) at(abs string) []shellpolicy.Landing {
	var out []shellpolicy.Landing
	if _, inside := pathrules.Within(l.project, abs); inside {
		// The project's own words are Resolve's; only a checkout nested
		// inside it, as a worktree under .sdlc can be, is looked for.
		return l.walk(abs, l.project, out)
	}
	if l.unplaced {
		return append(out, shellpolicy.Landing{Rel: filepath.ToSlash(abs)})
	}
	if l.own != "" {
		if rel, inside := pathrules.Within(l.own, abs); inside {
			return append(out, shellpolicy.Landing{Rel: path.Join(".git", rel), Own: true})
		}
	}
	for _, c := range l.candidates {
		if rel, inside := pathrules.Within(c, abs); inside {
			out = append(out, shellpolicy.Landing{Rel: path.Join(".git", rel)})
		}
	}
	return l.walk(abs, "", out)
}

// walk adds a landing for every directory from abs up to stop, not including
// it, that is a checkout of this repository.
func (l *landings) walk(abs, stop string, out []shellpolicy.Landing) []shellpolicy.Landing {
	for dir := abs; ; {
		if stop != "" {
			if rel, inside := pathrules.Within(stop, dir); inside && rel == "." {
				break
			}
		}
		if l.belongs(dir) {
			rel, _ := pathrules.Within(dir, abs)
			out = append(out, shellpolicy.Landing{Rel: rel})
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

// belongs reports whether dir is a checkout of this repository: its .git is a
// candidate common git directory, leads to one, or leads to a worktree's
// directory inside one that leads back to it. The way back is asked for
// because a .git file anybody can write says where it leads, and only git
// writes the worktree's own entry.
func (l *landings) belongs(dir string) bool {
	if b, seen := l.belong[dir]; seen {
		return b
	}
	b := l.decide(dir)
	l.belong[dir] = b
	return b
}

func (l *landings) decide(dir string) bool {
	dotgit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotgit)
	switch {
	case err != nil:
		return false
	case info.IsDir():
		return l.candidate(dotgit)
	}
	gitdir, ok := config.GitdirOf(dotgit)
	if !ok {
		return false
	}
	if l.candidate(gitdir) {
		return true
	}
	if parent := filepath.Dir(gitdir); filepath.Base(parent) != "worktrees" || !l.candidate(filepath.Dir(parent)) {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(gitdir, "gitdir"))
	if err != nil {
		return false
	}
	back := filepath.FromSlash(strings.TrimSpace(string(raw)))
	if !filepath.IsAbs(back) {
		back = filepath.Join(gitdir, back)
	}
	return config.SamePath(back, dotgit)
}

func (l *landings) candidate(p string) bool {
	for _, c := range l.candidates {
		if config.SamePath(c, p) {
			return true
		}
	}
	return false
}

// expandHome is word with a leading ~ read as the shell reads it, and false
// where the home it names cannot be found.
func expandHome(word string) (string, bool) {
	// The shell expands ~ before the command sees it. Read as a directory
	// called ~ in the repository, the user's own ~/.claude was the project's.
	rest, ok := strings.CutPrefix(word, "~")
	if !ok {
		return word, true
	}
	if rest == "" || rest[0] == '/' {
		home, err := os.UserHomeDir()
		if err != nil {
			// Somebody's home, even when HOME is not set to say where.
			return "", false
		}
		return filepath.ToSlash(home) + rest, true
	}
	name, after, named := strings.Cut(rest, "/")
	if !named {
		return word, true
	}
	// `~someone/.claude` is someone's home, not the project's; but the
	// someone can be the user running the session, with the project in it.
	u, err := user.Lookup(name)
	if err != nil || u.HomeDir == "" {
		return "", false
	}
	return filepath.ToSlash(u.HomeDir) + "/" + after, true
}
