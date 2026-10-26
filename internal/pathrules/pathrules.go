// Package pathrules turns the path a tool is about to touch into something a
// rule can be written against.
//
// Rules are written in repository-relative, slash-separated form -- ".git/",
// ".sdlc/config.json" -- because that is how a person says them and how they
// read in a denial. Everything else here exists to get an arbitrary path the
// assistant supplied into that form without letting it escape on the way.
package pathrules

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"

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
	// budget, where it is set, is the links one command may still follow,
	// shared by every copy of the Resolver and by every one At makes.
	budget *budget
}

// budget is what is left of the links a command may follow. Every walk that
// follows a link charges it here, once for each path it walks, so that a
// command of many words through long chains is refused before it holds the
// hook to its deadline. project and root are the resolver's own, cleaned and
// resolved, and rootLinks the links on the way to the project: a word spelled
// through the project is counted from root, so that /tmp or /var on macOS, or
// a linked home holding the project, is followed once, when the resolver is
// made, and never charged for every word.
type budget struct {
	left          int
	first         error
	project, root string
	rootLinks     int
}

// spent reports whether the command has followed more links than it may.
func (b *budget) spent() bool { return b != nil && b.left < 0 }

// met keeps the first bound any walk passed, which the end of a command asks
// for with Bound("").
func (b *budget) met(err error) {
	if b != nil && b.first == nil {
		b.first = err
	}
}

// charge takes n links off what is left.
func (b *budget) charge(n int) {
	if b == nil || n <= 0 {
		return
	}
	b.left -= n
	if b.left < 0 {
		b.met(ErrLinkBudget)
	}
}

// CommandLinks is the budget of links one command may follow, counted across
// all its words. Derived, not measured: the slowest link seen on macOS took
// 1.18 ms to follow through the hook, Linux is taken to be twenty times
// slower, and half of the hook's 25 s deadline is given to following links;
// 12.5 s / 23.5 ms is 531, rounded down to 512 (US-035, D4). A command of 60
// words through 255 links each held the hook for 22.8 s before this.
// docs/troubleshooting.md and docs/enforcement.md give the figure.
const CommandLinks = 512

var (
	// ErrTooManyLinks is a word that goes through more than maxLinks links,
	// or whose placing stopped at a link the system would not follow.
	ErrTooManyLinks = errors.New("a path goes through more symbolic links than sdlc follows for one path")
	// ErrLookupLimit is ErrTooManyLinks where the word is within maxLinks but
	// past what this system follows in one lookup (macOS 32, Linux 40): the
	// OS cannot say where it leads, and a program that walks one directory at
	// a time still writes through it.
	ErrLookupLimit = fmt.Errorf("%w: more than this system follows in one lookup", ErrTooManyLinks)
	// ErrLinkBudget is a command whose words have followed more than its
	// budget of links between them.
	ErrLinkBudget = errors.New("the paths of one command go through more symbolic links than sdlc follows for one command")
	// ErrPathTooLong is a word whose path from the root is longer than this
	// system looks up in one go (1,024 bytes on macOS, 4,096 on Linux). The OS
	// cannot say where it leads, and the walk read the rest of it by name,
	// while a shell part of the way down writes to it by a shorter, relative
	// name, through any link on the way (US-035, security review).
	ErrPathTooLong = errors.New("a path is longer than this system looks up in one go")
)

// diskAnswers is what Lstat said of each path asked about, and where each
// existing path leads with its links followed: whole, for resolve, and from a
// place known to hold no link, with the links counted, for Place's walk.
type diskAnswers struct {
	there    map[string]entry
	resolved map[string]string
	followed map[[2]string]followed
	counted  map[[2]string]followed
}

// entry is what Lstat said of a path: whether it is there, and whether it is a
// link or a directory itself. symlink is a symlink alone, the only link
// EvalSymlinks follows. unreadable is a path Lstat could not look up because
// the lookup passed the system's own limit on links, or, where tooLong, on
// the length of a path.
type entry struct{ there, link, symlink, dir, unreadable, tooLong bool }

// NewResolver resolves the project's own path, once.
func NewResolver(project string) Resolver {
	return Resolver{project: project, root: resolve(project)}
}

// Memoised is res asking the filesystem about each path once, for as long as
// it is used. It is for one decision, over which the disk is taken not to
// change: a Resolver kept while files come and go would answer from before.
func (res Resolver) Memoised() Resolver {
	res.disk = &diskAnswers{there: map[string]entry{}, resolved: map[string]string{},
		followed: map[[2]string]followed{}, counted: map[[2]string]followed{}}
	return res
}

// Budgeted is res with every link it follows from then on, by Rel, Place and
// Bound, charged to a budget of links shared by every copy of it. It is for
// one command, as Memoised is.
func (res Resolver) Budgeted(links int) Resolver {
	b := &budget{left: links}
	if p := filepath.Clean(res.project); filepath.IsAbs(p) {
		b.project, b.root = p, res.root
		b.rootLinks = res.walkLinks(p[:rootLen(p)], p, maxLinkWalk).links
	}
	res.budget = b
	return res
}

// At is a resolver for another project, asking the same memo and charging
// the same budget as res. The configuration checks place each word a command
// writes against the person's own roots, and a resolver of their own for each
// walked every word again from the root, four times in all (US-035, D8).
func (res Resolver) At(project string) Resolver {
	res.project = project
	res.root = res.resolve(project)
	return res
}

// nameMax is the longest name a system the hook runs on holds: 255 bytes on
// Linux, and 255 UTF-16 units on macOS and Windows.
const nameMax = 255

// nameOverLimit reports whether name is past nameMax in bytes and in UTF-16
// units both, and so a name no system holds. Counted in bytes alone, a name of
// 130 "é", which macOS holds, read a path past the system's length below it as
// not there, and a relative write through a link on that path into
// .sdlc/state was allowed (US-035, verifier round 7). A name past the limit
// in one count only is left a path too long: refusing it is the safe side.
func nameOverLimit(name string) bool {
	if len(name) <= nameMax {
		return false
	}
	units := 0
	for _, r := range name {
		units += utf16.RuneLen(r)
	}
	return units > nameMax
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
	info, err := os.Lstat(p)
	switch {
	case err == nil:
		e = entry{there: true, link: info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0,
			symlink: info.Mode()&fs.ModeSymlink != 0, dir: info.IsDir()}
	case lookupLimit(err):
		e.unreadable = true
	case pathTooLong(err) && !nameOverLimit(filepath.Base(p)):
		// The system gives the same answer for one name longer than it can
		// hold, and taken for a path too long, a word that was no path, as a
		// long token in `python3 -c` code, refused the command (US-035,
		// verifier round 6). Such a name cannot be there, nor anything below
		// it, so it is not there, as before; but only the name looked up is
		// asked, never an earlier one, which the system held to get this far.
		e.unreadable, e.tooLong = true, true
	}
	if res.disk != nil {
		res.disk.there[p] = e
	}
	return e
}

// resolve is resolve(p), asked once where res is memoised. With a budget,
// the links on the way are counted first, by a walk of their own, and
// charged; resolve(p) is still the answer. Where p is over the bound of one
// word, or the budget is spent, the answer is p by name, without following a
// link: the command is refused for it by then, and EvalSymlinks would follow
// up to 255.
func (res Resolver) resolve(p string) string {
	if res.disk != nil {
		if out, asked := res.disk.resolved[p]; asked {
			return out
		}
	}
	out := filepath.Clean(p)
	if res.followable(p) {
		out = resolve(p)
	}
	if res.disk != nil {
		res.disk.resolved[p] = out
	}
	return out
}

// followable reports whether resolve may follow the links on the way to p:
// always without a budget, and with one where p is within the bound of one
// word and the budget is not spent. The links are counted, and charged, on
// the way.
func (res Resolver) followable(p string) bool {
	b := res.budget
	if b == nil {
		return true
	}
	if b.spent() {
		return false
	}
	if res.countPath(p) > maxLinks {
		b.met(ErrTooManyLinks)
		return false
	}
	return !b.spent()
}

// span is where a count of the links on the way to p starts: known, a place
// that holds no link, the path at to walk from it, and base, the links on the
// way to known. A path spelled through the budget's project starts at its
// resolved root. The comparison is by bytes, so on a disk that folds case a
// word spelled in other letters is counted from the volume's root, with the
// project's own links: that can only charge more, and refuse sooner.
func (res Resolver) span(p string) (known, at string, base int) {
	if b := res.budget; b != nil && b.project != "" {
		if rest, ok := strings.CutPrefix(p, b.project); ok && (rest == "" || os.IsPathSeparator(rest[0])) {
			return b.root, b.root + rest, b.rootLinks
		}
	}
	return p[:rootLen(p)], p, 0
}

// countPath is how many links following p goes through, up to one more than
// maxLinks, charged to the budget where they are new.
func (res Resolver) countPath(p string) int {
	known, at, base := res.span(p)
	return base + res.count(known, at, max(maxLinks-base, 0)).links
}

// count is walkLinks(known, at, limit), asked once where res is memoised, with
// what it followed charged to the budget. Only a walk that finished within
// its limit is kept: one cut short says nothing of a longer limit. A known
// that at does not start with is a path past the bound, never a slice past
// the end of at, since a panic in the hook is an allow.
func (res Resolver) count(known, at string, limit int) followed {
	if !startsAt(known, at) {
		res.budget.met(ErrTooManyLinks)
		return followed{links: limit + 1}
	}
	key := [2]string{known, at}
	if res.disk != nil {
		if f, asked := res.disk.counted[key]; asked {
			return f
		}
	}
	f := res.walkLinks(known, at, limit)
	res.budget.charge(f.links)
	if res.disk != nil && f.links <= limit {
		res.disk.counted[key] = f
	}
	return f
}

// startsAt reports whether at is known or a path below it, by its names: the
// bytes of known followed by a separator, or ending in one.
func startsAt(known, at string) bool {
	rest, ok := strings.CutPrefix(at, known)
	switch {
	case !ok || known == "":
		return false
	case rest == "", os.IsPathSeparator(known[len(known)-1]):
		return true
	}
	return os.IsPathSeparator(rest[0])
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
		if folded, inside := respelledRel(root, resolved); inside {
			return folded, false
		}
		return filepath.ToSlash(abs), true
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") {
		if folded, inside := respelledRel(root, resolved); inside {
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
	w, parts := res.walk(abs)
	sep := string(filepath.Separator)
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

// walk is Place's walk down abs as far as it goes, before the place it got to
// is settled, and the names of abs it walked.
func (res Resolver) walk(abs string) (placeWalk, []string) {
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
	return w, parts
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
// ended because a link could not be followed. over says that was because the
// word needed more links than it had left, and unreadable, where set, is the
// path the walk stopped at because the system would not look it up.
type placeWalk struct {
	at, known       string
	settled, isDir  bool
	i, crossed      int
	left            int
	dotted, stopped bool
	over            bool
	unreadable      string
}

// stepParent takes a `..` and says whether the walk is over.
func (res Resolver) stepParent(w *placeWalk) bool {
	w.dotted = true
	if filepath.Dir(w.at) == w.at {
		return false // `..` at the top of a volume stays there, as every platform reads it
	}
	if !w.settled {
		to, ok, over := res.followCounting(w.known, w.at, w.crossed, &w.left)
		if !ok {
			w.stopped, w.over = true, over // past the budget, or a link to nothing: the rest by name
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
		if e.unreadable {
			w.unreadable = next
			res.metLookup(next)
		}
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
		to, ok, over := res.followCounting(w.known, w.at, w.crossed, &w.left)
		if ok {
			return to
		}
		w.over = over
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
// at needs more links than *left allows, leads nowhere, fails the guard that
// it start with known at a name, or the command's budget is spent, it says
// false and leaves *left alone; followCounting says whether it was the first.
func (res Resolver) follow(known, at string, crossed int, left *int) (string, bool) {
	to, ok, _ := res.followCounting(known, at, crossed, left)
	return to, ok
}

// followCounting is follow, saying as well whether at was over: whether it
// needed more links than *left, as against leading nowhere. A known that at
// does not start with at a name is neither: walkLinks slices at by the length
// of known, and a panic in the hook is an allow (US-035, AC-8). It is kept
// as a bound met all the same, as count keeps it: the rest of the word is read
// by name, and Bound("") then refuses the command for it. With a budget, the
// links followed are charged to it, and once it is spent nothing is followed,
// so the rest of the word is read by name: the command is refused by then.
func (res Resolver) followCounting(known, at string, crossed int, left *int) (string, bool, bool) {
	if !startsAt(known, at) {
		res.budget.met(ErrTooManyLinks)
		return at, false, false
	}
	if res.budget.spent() {
		return at, false, false
	}
	f, asked := followed{}, false
	if res.disk != nil {
		f, asked = res.disk.followed[[2]string{known, at}]
	}
	if !asked {
		f = res.followFrom(known, at, crossed, *left)
		res.budget.charge(f.links)
		if res.disk != nil && f.links <= *left {
			res.disk.followed[[2]string{known, at}] = f
		}
	}
	if f.links > *left {
		res.budget.met(ErrTooManyLinks)
		return at, false, true
	}
	if !f.ok {
		return at, false, false
	}
	*left -= f.links
	return f.to, true, false
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

// WordLinks is maxLinks, for a refusal at it to say.
const WordLinks = maxLinks

// maxLinkWalk is how many links filepath.EvalSymlinks follows before it gives
// up, the most a count of the links on the way to a project needs.
const maxLinkWalk = 255

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
	rel, inside := respelledRel(root, p)
	if inside && rel == "" {
		rel = "."
	}
	return rel, inside
}

// Root is the project, resolved.
func (res Resolver) Root() string { return res.root }

// respelledRel is p relative to root where filepath.Rel has read it as outside:
// as written, where one of them is spelled through macOS's data-volume
// firmlink and the two are related without it, and otherwise as foldedRel
// reads it. Asked only once filepath.Rel says outside, so nothing it places
// already is placed differently, and Root is as it was.
func respelledRel(root, p string) (string, bool) {
	r, q := withoutFirmlink(root), withoutFirmlink(p)
	if r == root && q == p {
		return foldedRel(root, p)
	}
	rel, err := filepath.Rel(r, q)
	rel = filepath.ToSlash(rel)
	switch {
	case err != nil || rel == ".." || strings.HasPrefix(rel, "../"):
		return foldedRel(root, p)
	case rel == ".":
		return "", true
	}
	return rel, true
}

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
//
// On macOS either may be spelled through the data volume's firmlink, which
// comes off first (AC-18 of US-108).
func foldedRel(root, p string) (string, bool) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return "", false
	}
	root, p = withoutFirmlink(root), withoutFirmlink(p)
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
	existing, rest, _ := res.existingPrefix(p)
	return filepath.Join(append([]string{res.resolve(existing)}, rest...)...)
}

// existingPrefix is the longest part of p that is there, the names after it,
// and, where the walk stopped at a path the system would not look up rather
// than one that is not there, that path.
func (res Resolver) existingPrefix(p string) (string, []string, string) {
	// Memoised, the walk below asks the disk only about what no earlier word
	// passed through, and a path that is not there costs nothing more.
	if res.disk == nil && res.there(p) {
		return p, nil, ""
	}
	sep := string(filepath.Separator)
	volume := filepath.VolumeName(p)
	parts := strings.Split(strings.TrimPrefix(p[len(volume):], sep), sep)
	existing := volume + sep
	for i := range parts {
		next := filepath.Join(existing, parts[i])
		if e := res.lstat(next); !e.there {
			if !e.unreadable {
				return existing, parts[i:], ""
			}
			res.metLookup(next)
			return existing, parts[i:], next
		}
		existing = next
	}
	return existing, nil, ""
}

// Bound says whether p can be placed within the bounds on the links a word
// and a command may follow: nil where it can, ErrTooManyLinks (or
// ErrLookupLimit) where p goes through more than maxLinks or its placing
// stops at a link the system would not follow, ErrPathTooLong where its
// placing stops at a path longer than the system looks up, and ErrLinkBudget
// where the command's budget is spent. Bound("") asks about the command alone: the
// first bound any walk on the resolver passed, word or not. A word is read
// both ways a program can read it, as Rel reads it and as Place does, and the
// walks are the ones they do, memoised, so asking first costs them nothing.
//
// Past the bound, Rel read the rest of a word by name, and on macOS a chain
// of 33 to 64 links, one in each directory, into .sdlc/state was allowed for
// a program that walks a path one directory at a time (US-035, AC-7).
func (res Resolver) Bound(p string) error {
	b := res.budget
	if p == "" {
		if b == nil {
			return nil
		}
		return b.first
	}
	if b.spent() {
		return ErrLinkBudget
	}
	abs, ok := res.absolute(p)
	if !ok {
		return nil
	}
	if err := res.wordBound(abs); err != nil {
		b.met(err)
		return err
	}
	if b.spent() {
		return ErrLinkBudget
	}
	return nil
}

// wordBound is Bound for abs alone, without the command's budget: nil, or the
// bound of one word it passes.
func (res Resolver) wordBound(abs string) error {
	over, stop := res.readBoth(abs)
	switch {
	case over:
		return ErrTooManyLinks
	case stop == "":
		return nil
	}
	return res.lookupBound(stop, abs)
}

// lookupBound is the bound p passes where a walk along it stopped at stop,
// which the system would not look up: ErrPathTooLong where stop is longer than
// it looks up in one go, and otherwise, for the links on the way to p,
// ErrTooManyLinks where a count link by link from the root, in which no lookup
// crosses more than one link and the system's own limit cannot stop it, passes
// maxLinks, and ErrLookupLimit otherwise. The length is asked of stop, the path
// the system refused, and not of p: below a working directory past the length,
// `touch l/<a name past the system's>` was given the lookup limit's reason,
// because the name it ends in cannot be there and so was no path too long
// (US-035, verifier round 7).
func (res Resolver) lookupBound(stop, p string) error {
	if res.lstat(stop).tooLong {
		return ErrPathTooLong
	}
	if res.count(p[:rootLen(p)], p, maxLinks).links > maxLinks {
		return ErrTooManyLinks
	}
	return ErrLookupLimit
}

// metLookup keeps, on a budgeted resolver, the bound a walk passed where it
// stopped at p because the system would not look p up. A walk that stops there
// reads the rest of its word by name, and recorded nowhere, a word in a
// spelling Bound was not asked about was placed by name past the bound (US-035,
// code review); kept, Bound("") refuses the command for it whoever walked it.
func (res Resolver) metLookup(p string) {
	if b := res.budget; b != nil && b.first == nil {
		b.met(res.lookupBound(p, p))
	}
}

// readBoth walks abs as Rel reads it and as Place does, and reports whether
// either went through more than maxLinks, and the first path either met that
// the system would not look up, "" where neither met one.
func (res Resolver) readBoth(abs string) (over bool, stop string) {
	existing, _, stop := res.existingPrefix(filepath.Clean(abs))
	over = res.countPath(existing) > maxLinks
	w, _ := res.walk(abs)
	switch {
	case w.dotted && !w.stopped:
		res.settle(&w)
	case !w.dotted:
		over = over || res.countPath(w.at) > maxLinks
	}
	if stop == "" {
		stop = w.unreadable
	}
	return over || w.over, stop
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
		// And `name::$INDEX_ALLOCATION`, or `name:$I30:$INDEX_ALLOCATION`,
		// is the directory itself: the spelling of Git's CVE-2019-1352.
		part = strings.TrimSuffix(part, ":$i30:$index_allocation")
		part = strings.TrimSuffix(part, "::$index_allocation")
		if trimmed := strings.TrimRight(part, ". "); trimmed != "" {
			parts[i] = trimmed
		}
	}
	return strings.Join(parts, "/")
}

// SameName reports whether a and b are one path to a filesystem that folds
// case.
func SameName(a, b string) bool { return Fold(a) == Fold(b) }

// CaseClause is the clause a refusal of path ends with where path holds name
// only as the disk folds case: `docs/agents.md` is held as AGENTS.md is, and
// a reason that called it an instruction file wherever it is said what no
// agent tool on a disk that keeps case does. It is "" where name is spelt as
// written, and for no name. The file tools and the shell end their refusals
// with the same words.
func CaseClause(path, name string) string {
	if name == "" {
		return ""
	}
	for seg := range strings.SplitSeq(path, "/") {
		if seg == name {
			return ""
		}
		if SameName(seg, name) {
			return "; the name is matched without regard to case, so " + path + " is held as " + name +
				" is, because the disk may treat them as one file"
		}
	}
	return ""
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
