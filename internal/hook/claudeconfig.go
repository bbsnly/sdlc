package hook

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// claudeConfig is where the person's Claude Code configuration is, for one
// call: ~/.claude and ~/.claude.json, and the directories Claude Code is told
// to keep it in instead. It decides the hooks, plugins, skills and permissions
// every session runs under, this plugin's own launcher and binary among them,
// so no session writes it (AC-7 of US-108).
//
// It is worked out only when a write is asked about, and once. A home that
// cannot be found is no configuration at all: the hook fails open on its own
// errors, and Claude Code always sets one.
type claudeConfig struct {
	getenv func(string) string
	// project is what a relative word of a command is relative to, absolute.
	project string
	roots   func() []claudeRoot
	// said is what word answered for each word asked, and held what heldWord
	// did: a command names the same words in segment after segment.
	said, held map[string]bool
}

// claudeRoot is one place the configuration is kept, as written and with the
// links on the way to it followed.
type claudeRoot struct {
	path     string
	resolver pathrules.Resolver
}

func newClaudeConfig(getenv func(string) string, project string) *claudeConfig {
	c := &claudeConfig{getenv: getenv, project: project, said: map[string]bool{}, held: map[string]bool{}}
	c.roots = sync.OnceValue(c.find)
	return c
}

// find is every place the configuration is kept. A ~/.claude linked into a
// dotfiles directory is common, so each is compared as it is on disk too.
func (c *claudeConfig) find() []claudeRoot {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json"))
	}
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "CLAUDE_CODE_PLUGIN_CACHE_DIR"} {
		if dir := c.getenv(name); dir != "" && filepath.IsAbs(dir) {
			paths = append(paths, filepath.Clean(dir))
		}
	}
	roots := make([]claudeRoot, 0, len(paths))
	for _, p := range paths {
		// The path asked has the firmlink taken off, so a HOME spelled
		// through it has to lose it too, or neither spelling meets the other.
		p = withoutFirmlink(p)
		roots = append(roots, claudeRoot{path: p, resolver: pathrules.NewResolver(p).Memoised()})
	}
	return roots
}

// holds reports whether an absolute path is the configuration or inside it:
// as written, folded as a filesystem that folds case reads it, and as the
// disk has it, through links and on Windows short names.
func (c *claudeConfig) holds(abs string) bool {
	if abs == "" || !filepath.IsAbs(abs) {
		return false
	}
	abs = withoutFirmlink(filepath.Clean(abs))
	for _, r := range c.roots() {
		// Folded on every platform: a refusal on a disk that keeps case is the
		// safe way to be wrong.
		if pathrules.Under(filepath.ToSlash(abs), filepath.ToSlash(r.path)) {
			return true
		}
		if _, outside := r.resolver.Rel(abs); !outside {
			return true
		}
	}
	return false
}

// above reports whether an absolute path is a directory that holds the
// configuration: the home, or a directory above it.
func (c *claudeConfig) above(abs string) bool {
	if abs == "" || !filepath.IsAbs(abs) {
		return false
	}
	abs = withoutFirmlink(filepath.Clean(abs))
	for _, r := range c.roots() {
		if pathrules.Under(filepath.ToSlash(r.path), filepath.ToSlash(abs)) {
			return true
		}
		if _, outside := pathrules.NewResolver(abs).Rel(r.resolver.Root()); !outside {
			return true
		}
	}
	return false
}

// word reports whether a word of a command, as the shell rules spell it, is
// the configuration or inside it.
func (c *claudeConfig) word(word string) bool {
	if said, ok := c.said[word]; ok {
		return said
	}
	abs, ok := c.place(word)
	said := ok && c.holds(abs)
	c.said[word] = said
	return said
}

// heldWord reports whether a word of a command is a directory that holds the
// configuration.
func (c *claudeConfig) heldWord(word string) bool {
	if held, ok := c.held[word]; ok {
		return held
	}
	abs, ok := c.place(word)
	held := ok && c.above(abs)
	c.held[word] = held
	return held
}

// place is the absolute path a word names, with the spellings of the home and
// of the configuration's own variables read as the shell would read them. ok
// is false for a word whose place only the shell can settle, which the shell
// rules judge by what the command names.
func (c *claudeConfig) place(word string) (string, bool) {
	w := strings.NewReplacer(`"`, "", "'", "").Replace(word)
	w, ok := c.expanded(w)
	if !ok {
		return "", false
	}
	w, ok = expandHome(strings.ReplaceAll(w, `\`, "/"))
	if !ok || strings.ContainsAny(w, "$%`*?[") {
		return "", false
	}
	p := filepath.FromSlash(w)
	if filepath.IsAbs(p) {
		return p, true
	}
	abs, ok := pathrules.Abs(c.project, p)
	return abs, ok && c.project != ""
}

// homeSpellings are the ways a shell names the home at the start of a word:
// POSIX's, PowerShell's and cmd's, and their Windows parts.
var homeSpellings = []string{
	"${home}", "$home", "$env:home", "${env:home}", "$env:userprofile", "${env:userprofile}",
	"%userprofile%", "%homedrive%%homepath%",
}

// configSpellings are the ways a shell names CLAUDE_CONFIG_DIR.
var configSpellings = []string{
	"${claude_config_dir}", "$claude_config_dir", "$env:claude_config_dir", "${env:claude_config_dir}",
	"%claude_config_dir%",
}

// expanded is word with a home or CLAUDE_CONFIG_DIR at its start put where it
// is. Case is folded, as PowerShell and cmd fold it; a POSIX shell does not,
// and reading `$Home` as the home there refuses more, which is the safe side.
func (c *claudeConfig) expanded(word string) (string, bool) {
	lower := strings.ToLower(word)
	for _, s := range homeSpellings {
		if strings.HasPrefix(lower, s) && atBoundary(word, len(s)) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", false
			}
			return filepath.ToSlash(home) + word[len(s):], true
		}
	}
	for _, s := range configSpellings {
		if strings.HasPrefix(lower, s) && atBoundary(word, len(s)) {
			dir := c.getenv("CLAUDE_CONFIG_DIR")
			if dir == "" {
				return "", false
			}
			return filepath.ToSlash(dir) + word[len(s):], true
		}
	}
	return word, true
}

// atBoundary reports whether a variable's name ends at i: `$HOMEDIR` is not
// `$HOME` followed by DIR.
func atBoundary(word string, i int) bool {
	return i == len(word) || word[i] == '/' || word[i] == '\\'
}

// withoutFirmlink is p with macOS's /System/Volumes/Data taken off the front,
// which names the same directory as the path without it: not a symlink, so
// nothing on the way resolves it, and ~/.claude spelled through it was
// another directory. Taken off whether or not the rest is firmlinked, which
// can only add refusals. internal/pathrules has a withoutFirmlink of its own,
// which takes it off only where both spellings are the same file, since there
// a path read as the project's is one more path the rules let through; a fix
// to how the prefix is read belongs in both.
func withoutFirmlink(p string) string {
	const data = "/System/Volumes/Data"
	if runtime.GOOS != "darwin" {
		return p
	}
	if rest, ok := cutPrefixFold(p, data); ok && strings.HasPrefix(rest, "/") {
		return rest
	}
	return p
}

// cutPrefixFold is strings.CutPrefix, comparing as a disk that folds case does.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}
