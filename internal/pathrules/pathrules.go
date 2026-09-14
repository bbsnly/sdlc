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
	"runtime"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Rel puts p into repository-relative, slash-separated form.
//
// outside is true when p resolves somewhere that is not under project, which
// includes the obvious "../.." and the less obvious case of a symlink inside
// the repository pointing out of it. Symlinks are resolved on the deepest part
// of the path that exists, because the file being written usually does not
// exist yet -- a check that only resolved whole paths would see nothing.
func Rel(project, p string) (rel string, outside bool) {
	return NewResolver(project).Rel(p)
}

// A Resolver is Rel for one project, with the project itself resolved once.
// The shell rules ask it about every word of a command, and resolving the
// project again for each was most of the cost of a long one.
type Resolver struct {
	project, root string
}

// NewResolver resolves the project's own path, once.
func NewResolver(project string) Resolver {
	return Resolver{project: project, root: resolve(project)}
}

// Rel is Rel(project, p).
func (res Resolver) Rel(p string) (rel string, outside bool) {
	if p == "" {
		return "", false
	}
	abs, ok := Abs(res.project, p)
	if !ok {
		return filepath.ToSlash(p), true
	}
	abs = filepath.Clean(abs)

	root := res.root
	resolved := resolveExisting(abs)

	r, err := filepath.Rel(root, resolved)
	if err != nil {
		if folded, inside := foldedRel(root, resolved); inside {
			return folded, false
		}
		return filepath.ToSlash(abs), true
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") {
		if folded, inside := foldedRel(root, resolved); inside {
			return folded, false
		}
		return filepath.ToSlash(resolved), true
	}
	if r == "." {
		return "", false
	}
	return r, false
}

// foldedRel is p relative to root where the filesystem folds case, as macOS's
// and Windows' do and filepath.Rel does not: `/PRIVATE/VAR/.../.sdlc/state`,
// or `\\?\C:\project\.sdlc\state`, is the project's loop state, and read as
// outside the project it was nobody's to protect. What it returns is folded,
// which is how every rule compares a path anyway.
func foldedRel(root, p string) (string, bool) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return "", false
	}
	r := strings.TrimSuffix(Fold(filepath.ToSlash(root)), "/")
	q := strings.TrimPrefix(Fold(filepath.ToSlash(p)), "//?/")
	if q == r {
		return "", true
	}
	return strings.CutPrefix(q, r+"/")
}

// Abs makes p absolute against base, as the program that writes it would. ok
// is false for a path whose place cannot be known from here.
//
// On Windows two kinds of path are neither absolute nor relative to base, and
// filepath.IsAbs calls both relative. `\repo\CLAUDE.md` is rooted on the
// current drive: joined onto the project, it became `repo/CLAUDE.md`, a file
// no rule protects, while the tool wrote the project's own CLAUDE.md. And
// `C:repo\CLAUDE.md` is relative to a working directory on that drive, which
// only the writing process knows.
func Abs(base, p string) (abs string, ok bool) {
	switch {
	case filepath.IsAbs(p):
		return p, true
	case filepath.VolumeName(p) != "":
		return p, false
	case strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/"):
		return filepath.VolumeName(base) + p, true
	}
	return filepath.Join(base, p), true
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
//
// p is absolute and clean. The walk goes down from the root and stops at the
// first part that is not there, since nothing can exist below it. Walking up
// from the far end cost a lookup per level, and a path tens of thousands of
// levels deep, which fits in one tool call, took longer than the hook is given.
func resolveExisting(p string) string {
	if _, err := os.Lstat(p); err == nil {
		return resolve(p)
	}
	sep := string(filepath.Separator)
	volume := filepath.VolumeName(p)
	parts := strings.Split(strings.TrimPrefix(p[len(volume):], sep), sep)
	existing := volume + sep
	i := 0
	for ; i < len(parts); i++ {
		next := filepath.Join(existing, parts[i])
		if _, err := os.Lstat(next); err != nil {
			break
		}
		existing = next
	}
	return filepath.Join(append([]string{resolve(existing)}, parts[i:]...)...)
}

// Under reports whether rel is dir itself or something inside it. Both are
// slash-separated and relative; "a/b" is under "a" but "ab" is not.
func Under(rel, dir string) bool {
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return true
	}
	// Case-insensitively, because macOS and Windows are, and on those two the
	// guard was one capital letter from being off: `.SDLC/state/active`,
	// `.sdlc/Config.json`, `claude.md` and `.GIT/config` are the same files as
	// the ones this refuses, and all four were allowed.
	//
	// On a case-sensitive filesystem this refuses a genuinely different file
	// -- a lowercase `claude.md` beside `CLAUDE.md`. That is the right way for
	// this to be wrong: a refusal that names its rule and can be argued with,
	// rather than a protection that silently is not there.
	//
	// Segment by segment, never by byte offset. A letter can fold to an ASCII
	// one while taking a different number of bytes -- `ſ` (long s) is two
	// bytes and folds to `s`, the Kelvin sign is three and folds to `k` -- so
	// cutting rel at len(dir) landed mid-character and `.ſdlc/state/active`
	// matched nothing. APFS folds it, and the write reached the real file.
	relParts := strings.Split(Fold(rel), "/")
	dirParts := strings.Split(Fold(dir), "/")
	if len(relParts) < len(dirParts) {
		return false
	}
	for i, d := range dirParts {
		if relParts[i] != d {
			return false
		}
	}
	return true
}

// Fold is a path as a case-insensitive filesystem reads it, for comparing two
// spellings of one. Every rule that matches a path matches it through this.
//
// strings.EqualFold is not enough. It folds one letter to one letter, and APFS
// folds `ﬆ` to `st` and `ﬁ` to `fi`, so `.sdlc/ﬆate/active` and
// `invoice_teﬆ.go` reached the protected files while matching nothing. This is
// full Unicode case folding, over a canonically decomposed string so that `é`
// written either way is one letter, which is how APFS also compares.
//
// A trailing dot or space is dropped from each segment, because Windows drops
// it: `.sdlc.` is `.sdlc` there. So is a trailing `::$DATA`, NTFS's name for a
// file's main stream. On a filesystem that keeps them, this refuses
// a name that is genuinely different, which is the safe way to be wrong.
func Fold(p string) string {
	folded := norm.NFC.String(cases.Fold().String(norm.NFD.String(p)))
	parts := strings.Split(folded, "/")
	for i, part := range parts {
		// On NTFS `name::$DATA` is the file's own content under another
		// spelling, so it is that file.
		part = strings.TrimSuffix(part, "::$data")
		if trimmed := strings.TrimRight(part, ". "); trimmed != "" {
			parts[i] = trimmed
		}
	}
	return strings.Join(parts, "/")
}

// SameName reports whether a and b are one path to a filesystem that folds
// case.
func SameName(a, b string) bool { return Fold(a) == Fold(b) }

// UnderAny reports whether rel is under any of dirs.
func UnderAny(rel string, dirs ...string) bool {
	for _, d := range dirs {
		if Under(rel, d) {
			return true
		}
	}
	return false
}
