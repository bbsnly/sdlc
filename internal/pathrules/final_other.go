//go:build !windows

package pathrules

// finalPath is followed by filepath.EvalSymlinks everywhere but Windows, where
// a junction is a link it does not follow.
func finalPath(string) (string, bool) { return "", false }
