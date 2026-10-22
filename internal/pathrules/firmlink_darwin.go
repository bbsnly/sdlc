package pathrules

import (
	"os"
	"path/filepath"
	"strings"
)

// firmlinkRoot is where macOS mounts the data volume. Most of what is on it is
// also reached without the prefix, through a firmlink, which is not a symlink:
// nothing that resolves a path takes the prefix off.
const firmlinkRoot = "/System/Volumes/Data"

// withoutFirmlink is p without firmlinkRoot in front, where the two spellings
// are the same file, and p as it is anywhere else. A file tool's write to the
// project spelled through the firmlink was read as outside it, and refused as
// write-outside-repository (US-108).
//
// It is taken off only where the deepest part of the path that exists is the
// same file with the prefix and without it: the data volume's own root, and a
// directory of the system volume spelled through it, are other directories,
// and a path read as the project's that is not would be one no rule protects.
// The configuration check in internal/hook has a withoutFirmlink of its own,
// which takes the prefix off whatever follows, since there that can only add
// refusals; a fix to how the prefix is read belongs in both.
func withoutFirmlink(p string) string {
	// Compared on the prefix's bytes alone, before anything is folded or asked
	// of the disk: nearly every path asked does not start with it.
	n := len(firmlinkRoot)
	if len(p) <= n || p[n] != '/' || !strings.EqualFold(p[:n], firmlinkRoot) {
		return p
	}
	rest := p[n:]
	// Up to "/" at the latest, which is never the data volume's root.
	for at := rest; ; at = filepath.Dir(at) {
		if plain, err := os.Stat(at); err == nil {
			through, err := os.Stat(p[:n] + at)
			if err != nil || !os.SameFile(plain, through) {
				return p
			}
			return rest
		}
		if at == "/" {
			return p
		}
	}
}
