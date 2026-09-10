// Package pathrules turns the path a tool is about to touch into something a
// rule can be written against.
//
// Rules are written in repository-relative, slash-separated form -- ".git/",
// ".sdlc/config.json" -- because that is how a person says them and how they
// read in a denial. Everything else here exists to get an arbitrary path the
// assistant supplied into that form without letting it escape on the way.
package pathrules

import (
	"os"
	"path/filepath"
	"strings"
)

// Rel puts p into repository-relative, slash-separated form.
//
// outside is true when p resolves somewhere that is not under project, which
// includes the obvious "../.." and the less obvious case of a symlink inside
// the repository pointing out of it. Symlinks are resolved on the deepest part
// of the path that exists, because the file being written usually does not
// exist yet -- a check that only resolved whole paths would see nothing.
func Rel(project, p string) (rel string, outside bool) {
	if p == "" {
		return "", false
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(project, abs)
	}
	abs = filepath.Clean(abs)

	root := resolve(project)
	resolved := resolveExisting(abs)

	r, err := filepath.Rel(root, resolved)
	if err != nil {
		return filepath.ToSlash(abs), true
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") {
		return filepath.ToSlash(resolved), true
	}
	if r == "." {
		return "", false
	}
	return r, false
}

// resolve follows symlinks, falling back to the path itself when it cannot.
func resolve(p string) string {
	if out, err := filepath.EvalSymlinks(p); err == nil {
		return out
	}
	return filepath.Clean(p)
}

// resolveExisting follows symlinks on the longest existing prefix of p and
// rejoins the rest. Writing to "link/config" where link points at .git must
// resolve to .git/config even though .git/config was never named.
func resolveExisting(p string) string {
	rest := []string{}
	cur := p
	for {
		if _, err := os.Lstat(cur); err == nil {
			return filepath.Join(append([]string{resolve(cur)}, rest...)...)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

// Under reports whether rel is dir itself or something inside it. Both are
// slash-separated and relative; "a/b" is under "a" but "ab" is not.
func Under(rel, dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return true
	}
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// UnderAny reports whether rel is under any of dirs.
func UnderAny(rel string, dirs ...string) bool {
	for _, d := range dirs {
		if Under(rel, d) {
			return true
		}
	}
	return false
}
