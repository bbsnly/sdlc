// Package pathrules turns the path a tool is about to touch into something a
// rule can be written against.
//
// Rules are written in repository-relative, slash-separated form -- ".git/",
// ".sdlc/config.json" -- because that is how a person says them and how they
// read in a denial. Everything else here exists to get an arbitrary path the
// assistant supplied into that form without letting it escape on the way.
package pathrules

import (
	"io/fs"
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

// diskAnswers is what Lstat said of each path asked about, and where each
// existing path leads with its links followed: whole, for resolve, and from a
// place known to hold no link, with the links counted, for Place's walk.
type diskAnswers struct {
	there    map[string]entry
	resolved map[string]string
	followed map[[2]string]followed
}

// entry is what Lstat said of a path: whether it is there, and whether it is a
// link or a directory itself. symlink is a symlink alone, the only link
// EvalSymlinks follows.
type entry struct{ there, link, symlink, dir bool }

// NewResolver resolves the project's own path, once.
func NewResolver(project string) Resolver {
	return Resolver{project: project, root: resolve(project)}
}

// Memoised is res asking the filesystem about each path once, for as long as
// it is used. It is for one decision, over which the disk is taken not to
// change: a Resolver kept while files come and go would answer from before.
func (res Resolver) Memoised() Resolver {
	res.disk = &diskAnswers{there: map[string]entry{}, resolved: map[string]string{}, followed: map[[2]string]followed{}}
	return res
}

// there reports whether p is on disk, its last element not followed.
func (res Resolver) there(p string) bool { return res.lstat(p).there }

// lstat is what Lstat says of p, asked once where res is memoised. A link is a
// symlink, or on Windows a junction, which Go reports as irregular.
func (res Resolver) lstat(p string) entry {
	if res.disk != nil {
		if e, asked := res.disk.there[p]; asked {
			return e
		}
	}
	var e entry
	if info, err := os.Lstat(p); err == nil {
		e = entry{there: true, link: info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0,
			symlink: info.Mode()&fs.ModeSymlink != 0, dir: info.IsDir()}
	}
	if res.disk != nil {
		res.disk.there[p] = e
	}
	return e
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
// written under a name that was nowhere near it. So the walk down the longest
// part of p that exists reads a `..` after a link as the parent of where the
// link leads, and only what is not there yet, where there is no link left to
// follow, is taken apart by name.
//
// The walk reads `.` and `..` itself and hands neither to the OS. Win32 removes
// `lnk\..` by name before NTFS sees the path, so on Windows both of the hook's
// readings came back name-first, while Git Bash, which runs the command,
// follows the link first (R2-5, from US-029.2).
func (res Resolver) Place(p string) (string, bool) {
	abs, ok := res.absolute(p)
	if !ok {
		return "", false
	}
	volume := filepath.VolumeName(abs)
	parts := strings.FieldsFunc(abs[len(volume):], func(r rune) bool { return r == '/' || r == filepath.Separator })
	sep := string(filepath.Separator)

	// Down from the root, as resolveExisting goes, and for the same reason.
	w := placeWalk{at: volume + sep, known: volume + sep, settled: true, isDir: true, left: maxLinks}
	for ; w.i < len(parts); w.i++ {
		var stop bool
		switch parts[w.i] {
		case ".":
			w.dotted = true
		case "..":
			stop = res.stepParent(&w)
		default:
			stop = res.stepName(&w, parts[w.i])
		}
		if stop {
			break
		}
	}
	resolved := res.settle(&w)
	base := filepath.VolumeName(resolved)
	kept := strings.FieldsFunc(resolved[len(base):], func(r rune) bool { return r == '/' || r == filepath.Separator })
	for _, part := range parts[w.i:] {
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

// absolute is p made absolute against the project, and false for a path whose
// place cannot be known from here.
func (res Resolver) absolute(p string) (string, bool) {
	switch {
	case p == "":
		return "", false
	case filepath.IsAbs(p):
		return p, true
	case filepath.VolumeName(p) != "":
		return "", false
	case strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/"):
		return filepath.VolumeName(res.project) + p, true
	default:
		return res.project + string(filepath.Separator) + p, true
	}
}

// placeWalk is where Place's walk has got to. at is settled while no link has
// been crossed since known, the last place on the way known to be there and to
// hold no link, which at starts with. Only an unsettled at is followed, at a
// `..`, so a `..` after plain directories costs nothing. crossed is how many
// links lie between known and at, and left how many more links the word may
// follow. i is how many parts have been taken, and stopped says the walk
// ended because a link could not be followed.
type placeWalk struct {
	at, known       string
	settled, isDir  bool
	i, crossed      int
	left            int
	dotted, stopped bool
}

// stepParent takes a `..` and says whether the walk is over.
func (res Resolver) stepParent(w *placeWalk) bool {
	w.dotted = true
	if filepath.Dir(w.at) == w.at {
		return false // `..` at the top of a volume stays there, as every platform reads it
	}
	if !w.settled {
		to, ok := res.follow(w.known, w.at, w.crossed, &w.left)
		if !ok {
			w.stopped = true // past the budget, or a link to nothing: the rest by name
			return true
		}
		w.at, w.known, w.settled, w.crossed, w.isDir = to, to, true, 0, res.lstat(to).dir
	}
	if !w.isDir {
		return true // the OS cannot go up from here either: the rest by name, as before
	}
	w.at, w.isDir = filepath.Dir(w.at), true
	w.known = w.at
	return false
}

// stepName takes one name and says whether the walk is over, which it is where
// the name is not there.
func (res Resolver) stepName(w *placeWalk, name string) bool {
	next := joinName(w.at, name)
	e := res.lstat(next)
	if !e.there {
		return true
	}
	if e.link {
		w.crossed++
	}
	w.at, w.settled, w.isDir = next, w.settled && !e.link, e.dir
	if w.settled {
		w.known = w.at
	}
	return false
}

// settle is where the walk's at leads, with the links on the way followed.
func (res Resolver) settle(w *placeWalk) string {
	switch {
	case filepath.Dir(w.at) == w.at, w.stopped:
	case w.dotted:
		if to, ok := res.follow(w.known, w.at, w.crossed, &w.left); ok {
			return to
		}
	default:
		return res.resolve(w.at)
	}
	return w.at
}

// joinName is dir/name, with a separator between them unless dir ends in one
// or is a volume alone, as EvalSymlinks joins them.
func joinName(dir, name string) string {
	if len(dir) > len(filepath.VolumeName(dir)) && !os.IsPathSeparator(dir[len(dir)-1]) {
		dir += string(filepath.Separator)
	}
	return dir + name
}

// followed is where a path leads with its links followed, how many links that
// took, and whether they could all be followed.
type followed struct {
	to    string
	links int
	ok    bool
}

// follow is resolve for Place's walk: where at leads with its links followed,
// each counted against *left, which it lowers. known is the start of at that
// is there and holds no link, and crossed the links in the rest of it. Where
// at needs more links than *left allows, or leads nowhere, it says false and
// leaves *left alone.
func (res Resolver) follow(known, at string, crossed int, left *int) (string, bool) {
	f, asked := followed{}, false
	if res.disk != nil {
		f, asked = res.disk.followed[[2]string{known, at}]
	}
	if !asked {
		f = res.followFrom(known, at, crossed, *left)
		if res.disk != nil && f.links <= *left {
			res.disk.followed[[2]string{known, at}] = f
		}
	}
	if !f.ok || f.links > *left {
		return at, false
	}
	*left -= f.links
	return f.to, true
}

// followFrom is follow, asked of the disk. Windows follows every link on the
// way, symlink or junction, in one call that it bounds itself, and the walk
// counts the links it crossed; elsewhere, and where that call fails, the links
// are followed here and every one is counted, the ones their targets lead
// through too. limit+1 links is a path that needs more than limit.
func (res Resolver) followFrom(known, at string, crossed, limit int) followed {
	out, ok := finalPath(at)
	f := followed{to: out, links: crossed, ok: true}
	if !ok {
		f = res.walkLinks(known, at, limit)
	}
	return f
}

// walkLinks is filepath.EvalSymlinks's own walk of at, started after known,
// counting the symlinks it follows: within limit it gives EvalSymlinks's
// answer, or not ok where EvalSymlinks fails, and past it, limit+1 without
// following one more. EvalSymlinks follows 255, and an absolute target sends
// it back to the root, so one call on a word built for it cost half a second,
// and one at every `..` of the word held the hook past its timeout.
func (res Resolver) walkLinks(known, at string, limit int) followed {
	w := linkWalk{dest: known, rest: at[len(known):]}
	for w.rest != "" {
		if done, f := w.step(res, limit); done {
			return f
		}
	}
	return followed{to: normalise(filepath.Clean(w.dest)), links: w.links, ok: true}
}

// linkWalk is where walkLinks has got to: dest, which holds no link, the path
// still to walk after it, and the symlinks followed so far.
type linkWalk struct {
	dest, rest string
	links      int
}

// isSeparator reports whether r separates names in a path, on any platform.
func isSeparator(r rune) bool { return r == '/' || r == filepath.Separator }

// step walks one name of rest. It reports done with the answer where the walk
// ends there, and otherwise leaves dest and rest for the next name.
func (w *linkWalk) step(res Resolver, limit int) (bool, followed) {
	w.rest = strings.TrimLeftFunc(w.rest, isSeparator)
	name, after := w.rest, ""
	if j := strings.IndexFunc(w.rest, isSeparator); j >= 0 {
		name, after = w.rest[:j], w.rest[j:]
	}
	w.rest = after
	switch name {
	case "", ".", "..":
		if name == ".." {
			w.dest = filepath.Dir(w.dest) // dest holds no link, so its parent by name is its parent on disk
		}
		return false, followed{}
	}
	w.dest = joinName(w.dest, name)
	e := res.lstat(w.dest)
	switch {
	case !e.there, !e.symlink && !e.dir && w.rest != "":
		return true, followed{links: w.links}
	case !e.symlink:
		return false, followed{}
	case w.links == limit:
		return true, followed{links: limit + 1}
	}
	w.links++
	target, err := os.Readlink(w.dest)
	if err != nil {
		return true, followed{links: w.links}
	}
	// A relative target replaces the link's own name; an absolute one
	// starts again at the root it names.
	v := rootLen(target)
	w.dest, w.rest = filepath.Dir(w.dest), target[v:]+w.rest
	if v > 0 {
		w.dest = target[:v]
	}
	return false, followed{}
}

// rootLen is how much of p is the volume and the separator after it that an
// absolute path starts with, as EvalSymlinks reads a link's target: 0 for a
// relative one.
func rootLen(p string) int {
	v := len(filepath.VolumeName(p))
	if v < len(p) && os.IsPathSeparator(p[v]) {
		v++
	}
	return v
}

// maxLinks is how many links Place may follow for one word, every one
// counted: the ones the word crosses and the ones their targets lead through.
// Counting only the ones it crossed, a word with a link at each `..` leading
// through 255 more took 44.5 s, past the hook's timeout. It has to stay above
// every platform's own limit on the links one lookup follows -- Linux 40,
// macOS 32, Windows 63 reparse points -- so that the bound never cuts short a
// word that one call can write through. That holds on Linux and macOS, where
// each link counted is one the OS follows in the same lookup. On Windows Go
// marks every reparse point that is not a symlink irregular, and the walk
// counts those too, though Windows passes through them without following
// anything, so a word through them can reach the bound first: US-053.
const maxLinks = 64

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
