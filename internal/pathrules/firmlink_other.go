//go:build !darwin

package pathrules

// withoutFirmlink is p: only macOS spells its paths through a data-volume
// firmlink.
func withoutFirmlink(p string) string { return p }
