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
	// disk, where it is set, is what the filesystem said of each path, asked
	// once. Every word of a command was walked down from the root on its own,
	// and 2,500 words from 50 directories took 28 s of the hook's 30.
	disk *diskAnswers
}

// diskAnswers is whether each path asked about is there, and where each
// existing path leads with its links followed.
type diskAnswers struct {
	there    map[string]bool
	resolved map[string]string
}

// NewResolver resolves the project's own path, once.
func NewResolver(project string) Resolver {
	return Resolver{project: project, root: resolve(project)}
}

// Memoised is res asking the filesystem about each path once, for as long as
// it is used. It is for one decision, over which the disk is taken not to
// change: a Resolver kept while files come and go would answer from before.
func (res Resolver) Memoised() Resolver {
	res.disk = &diskAnswers{there: map[string]bool{}, resolved: map[string]string{}}
	return res
}

// there reports whether p is on disk, its last element not followed.
func (res Resolver) there(p string) bool {
	if res.disk == nil {
		_, err := os.Lstat(p)
		return err == nil
	}
	ok, asked := res.disk.there[p]
	if !asked {
		_, err := os.Lstat(p)
		ok = err == nil
		res.disk.there[p] = ok
	}
	return ok
}

// resolve is resolve(p), asked once where res is memoised.
func (res Resolver) resolve(p string) string {
	if res.disk == nil {
		return resolve(p)
	}
	out, asked := res.disk.resolved[p]
	if !asked {
		out = resolve(p)
		res.disk.resolved[p] = out
	}
	return out
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
	resolved := res.resolveExisting(abs)

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

// Place is where p lands on disk, absolute: the file a program that writes p
// writes, with every link on the way followed as the filesystem follows it. ok
// is false for a path whose place cannot be known from here.
//
// Nothing is cleaned before the links are read. Rel cleans first, and so reads
// `link/../x` as x beside the link, where the filesystem goes to x beside what
// the link leads to: through a link into another checkout, a record there was
// written under a name that was nowhere near it. So the longest part of p that
// exists is left to the filesystem, `..` and all, and only what is not there
// yet, where there is no link left to follow, is taken apart by name.
func (res Resolver) Place(p string) (string, bool) {
	var abs string
	switch {
	case p == "":
		return "", false
	case filepath.IsAbs(p):
		abs = p
	case filepath.VolumeName(p) != "":
		return "", false
	case strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/"):
		abs = filepath.VolumeName(res.project) + p
	default:
		abs = res.project + string(filepath.Separator) + p
	}
	volume := filepath.VolumeName(abs)
	parts := strings.FieldsFunc(abs[len(volume):], func(r rune) bool { return r == '/' || r == filepath.Separator })
	sep := string(filepath.Separator)

	// Down from the root, as resolveExisting goes, and for the same reason.
	existing, i := volume+sep, 0
	for ; i < len(parts); i++ {
		next := existing + parts[i]
		if !res.there(next) {
			break
		}
		existing = next + sep
	}
	resolved := volume + sep
	if i > 0 {
		resolved = res.resolve(strings.TrimSuffix(existing, sep))
	}
	base := filepath.VolumeName(resolved)
	kept := strings.FieldsFunc(resolved[len(base):], func(r rune) bool { return r == '/' || r == filepath.Separator })
	for _, part := range parts[i:] {
		switch part {
		case ".":
		case "..":
			if len(kept) > 0 {
				kept = kept[:len(kept)-1]
			}
		default:
			kept = append(kept, part)
		}
	}
	return base + sep + strings.Join(kept, sep), true
}

// Within is p relative to root, slash-separated, when p is root or inside it,
// comparing as the filesystem compares names. Both are absolute and resolved.
func Within(root, p string) (string, bool) {
	r, err := filepath.Rel(root, p)
	if err == nil {
		r = filepath.ToSlash(r)
		if r != ".." && !strings.HasPrefix(r, "../") {
			return r, true
		}
	}
	rel, inside := foldedRel(root, p)
	if inside && rel == "" {
		rel = "."
	}
	return rel, inside
}

// Root is the project, resolved.
func (res Resolver) Root() string { return res.root }

// foldedRel is p relative to root where the filesystem folds case, as macOS's
// and Windows' do and filepath.Rel does not: `/PRIVATE/VAR/.../.sdlc/state`,
// or `\\?\C:\project\.sdlc\state`, is the project's loop state, and read as
// outside the project it was nobody's to protect. What it returns is folded,
// which is how every rule compares a path anyway.
//
// The folded text only says where to look. A volume can keep case, and APFS
// keeps the trailing dot Fold drops, so `proj` beside a project called `Proj`
// or `proj.` is another directory, and a write there was read as the
// project's. The part of p that spells the project has to be the project.
func foldedRel(root, p string) (string, bool) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return "", false
	}
	r := strings.TrimSuffix(Fold(filepath.ToSlash(root)), "/")
	spelled := strings.TrimPrefix(filepath.ToSlash(p), "//?/")
	q := Fold(spelled)
	rel, inside := "", q == r
	if !inside {
		rel, inside = strings.CutPrefix(q, r+"/")
	}
	if !inside {
		return "", false
	}
	// Folding keeps every separator, so the project is as many segments of p
	// as it is of root. What is not there is not the same file either.
	n := strings.Count(r, "/") + 1
	ancestor := strings.Join(strings.SplitN(spelled, "/", n+1)[:n], "/")
	here, _ := os.Stat(filepath.FromSlash(ancestor))
	project, _ := os.Stat(root)
	if !os.SameFile(here, project) {
		return "", false
	}
	return rel, true
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

// resolve follows symlinks, and on Windows junctions, falling back to the path
// itself when it cannot.
func resolve(p string) string {
	if out, ok := finalPath(p); ok {
		return out
	}
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
func (res Resolver) resolveExisting(p string) string {
	// Memoised, the walk below asks the disk only about what no earlier word
	// passed through, and a path that is not there costs nothing more.
	if res.disk == nil && res.there(p) {
		return res.resolve(p)
	}
	sep := string(filepath.Separator)
	volume := filepath.VolumeName(p)
	parts := strings.Split(strings.TrimPrefix(p[len(volume):], sep), sep)
	existing := volume + sep
	i := 0
	for ; i < len(parts); i++ {
		next := filepath.Join(existing, parts[i])
		if !res.there(next) {
			break
		}
		existing = next
	}
	return filepath.Join(append([]string{res.resolve(existing)}, parts[i:]...)...)
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
