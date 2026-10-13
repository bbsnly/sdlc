package hook

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// maxBelow bounds the directories and files looked at below where a find
// starts. Past it the hook does not know what the find could come to, and the
// find is judged by the names it is given, as it always was.
const maxBelow = 10000

// outsider answers, from the disk and never from the text of a path, whether
// a path is outside every checkout of the repository. It is the ground the
// shell rules let a word go on, so every doubt and every failure answers no:
// the word is then judged by its name, as it was before there was this.
//
// The text of a path is no ground: looked up as written, "outside" is also
// what the lookup says of a path it could not read. On macOS
// /System/Volumes/Data/<repository> is the repository by another name,
// /.vol/<device>/<inode> is any directory in it, and a hard link is one of its
// files under a name elsewhere; each was placed outside, and written.
type outsider struct {
	// checkouts is every checkout of the repository, the session's first.
	checkouts func() []string
	roots     []os.FileInfo
	asked     bool
	unknown   bool
	// decided is what cleared answered for each path it was asked about, and
	// below what holdsNoCheckout answered for each place a find starts: the
	// rules ask about the same find more than once.
	decided, below map[string]bool
}

// newOutsider is an outsider for one command. Made any other way it has no
// maps to remember in, and writing to one is a panic, which in the hook lets
// the command through.
func newOutsider(checkouts func() []string) *outsider {
	return &outsider{checkouts: checkouts, decided: map[string]bool{}, below: map[string]bool{}}
}

// outside reports whether path, absolute and slash-separated, is outside the
// repository as the disk stands, and with below, whether everything under it
// is too.
func (o *outsider) outside(path string, below bool) bool {
	p := filepath.FromSlash(path)
	// A share or a device path is somewhere the disk's answers cannot be
	// compared with the repository's: only a path on a lettered drive is placed.
	// Nor is a path with `..` in it, which Clean takes off the name where the
	// disk takes it off the directory, and a link makes those two places.
	if !filepath.IsAbs(p) || len(filepath.VolumeName(p)) > 2 ||
		slices.Contains(strings.Split(filepath.ToSlash(p), "/"), "..") {
		return false
	}
	// What Clean does change is the same path to the disk: a doubled
	// separator, a `.` between two, and a separator at the end, without which
	// a link there is asked about as the link it is.
	p = filepath.Clean(p)
	return namedAsLooked(p) && o.cleared(p) && (!below || o.holdsNoCheckout(p))
}

// cleared reports whether nothing on the way to p is the repository's: asked
// once for each directory, because the words of a command share them.
func (o *outsider) cleared(p string) bool {
	if answer, ok := o.decided[p]; ok {
		return answer
	}
	answer := o.decide(p)
	o.decided[p] = answer
	return answer
}

// decide is cleared for one path, given that its parent is asked in turn.
func (o *outsider) decide(p string) bool {
	parent := filepath.Dir(p)
	info, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		// Not there yet: where it would be made decides.
		return parent != p && o.cleared(parent)
	case err != nil || info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
		// A link, or on Windows a junction, leads wherever it was pointed.
		return false
	case !info.IsDir():
		// A file with a second name may have it in the repository. And the
		// directory its name puts it in has to be there: on macOS
		// /.vol/<device>/<inode> is a file by its number, in a directory
		// that is not one, and a copy onto it was let go as a path outside.
		_, err = os.Lstat(parent)
		return err == nil && oneName(p, info) && o.cleared(parent)
	case o.isCheckout(info):
		return false
	case parent == p:
		return true
	}
	// The directory the disk says is above this one has to be the one the
	// name says: /.vol/<device>/<inode> on macOS is a directory by its number,
	// and its `..` is where it really is.
	up, err := os.Lstat(p + string(filepath.Separator) + "..")
	if err != nil {
		return false
	}
	named, err := os.Lstat(parent)
	return err == nil && os.SameFile(up, named) && o.cleared(parent)
}

// isCheckout reports whether a directory is a checkout of the repository,
// under whatever name it was reached, or cannot be told from one.
func (o *outsider) isCheckout(info os.FileInfo) bool {
	if !o.asked {
		o.asked = true
		for i, c := range o.checkouts() {
			root, err := os.Stat(filepath.FromSlash(c))
			switch {
			case err == nil:
				o.roots = append(o.roots, root)
			case i == 0 || !errors.Is(err, fs.ErrNotExist):
				// A worktree git still lists and nobody has is no checkout;
				// one that cannot be read might be this directory.
				o.unknown = true
			}
		}
	}
	for _, root := range o.roots {
		if os.SameFile(info, root) {
			return true
		}
	}
	return o.unknown || len(o.roots) == 0
}

// holdsNoCheckout reports whether no checkout of the repository is below
// root: asked once for each, because the walk is of everything there and more
// than one rule asks about a find.
func (o *outsider) holdsNoCheckout(root string) bool {
	if answer, ok := o.below[root]; ok {
		return answer
	}
	answer := o.walk(root)
	o.below[root] = answer
	return answer
}

// walk is holdsNoCheckout for one root, looking at no more than maxBelow of
// what is there and following no link, as a find that follows none does.
func (o *outsider) walk(root string) bool {
	seen, held := 0, false
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if seen++; seen > maxBelow {
			return fs.SkipAll
		}
		if !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if o.isCheckout(info) {
			held = true
			return fs.SkipAll
		}
		return nil
	})
	return err == nil && !held && seen <= maxBelow
}
