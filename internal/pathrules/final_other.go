//go:build !windows

package pathrules

// finalPath is followed by filepath.EvalSymlinks everywhere but Windows, where
// a junction is a link it does not follow.
func finalPath(string) (string, bool) { return "", false }

// normalise is p, which has no symlink on it, spelled as resolve would spell
// it: as it is, here.
func normalise(p string) string { return p }
