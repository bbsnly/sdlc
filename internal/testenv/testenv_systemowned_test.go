package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

// The directory Windows' own programs keep under LocalAppData is not what a
// test left, on Windows and nowhere else, and only as a directory at the top
// of the cache directory.
func TestWhatWindowsKeepsUnderLocalAppDataIsNotALeftover(t *testing.T) {
	cases := []struct {
		name  string
		goos  string
		leave func(t *testing.T, cache string)
		left  []string // relative to the cache directory
	}{
		{name: "Microsoft on Windows, with what is in it", goos: "windows",
			leave: func(t *testing.T, c string) { put(t, filepath.Join(c, "Microsoft", "Windows", "a-cache"), false) }},
		{name: "microsoft in another case on Windows", goos: "windows",
			leave: func(t *testing.T, c string) { put(t, filepath.Join(c, "microsoft"), true) }},
		{name: "Microsoft on Linux", goos: "linux", left: []string{"Microsoft"},
			leave: func(t *testing.T, c string) { put(t, filepath.Join(c, "Microsoft"), true) }},
		{name: "Microsoft on macOS", goos: "darwin", left: []string{"Microsoft"},
			leave: func(t *testing.T, c string) { put(t, filepath.Join(c, "Microsoft"), true) }},
		{name: "a file named Microsoft on Windows", goos: "windows", left: []string{"Microsoft"},
			leave: func(t *testing.T, c string) { put(t, filepath.Join(c, "Microsoft"), false) }},
		{name: "Microsoft inside sdlc on Windows", goos: "windows", left: []string{filepath.Join("sdlc", "Microsoft")},
			leave: func(t *testing.T, c string) { put(t, filepath.Join(c, "sdlc", "Microsoft"), true) }},
		{name: "another directory on Windows", goos: "windows", left: []string{"Vendor"},
			leave: func(t *testing.T, c string) { put(t, filepath.Join(c, "Vendor"), true) }},
		{name: "a stray file beside Microsoft on Windows", goos: "windows", left: []string{"stray"},
			leave: func(t *testing.T, c string) {
				put(t, filepath.Join(c, "Microsoft", "a-cache"), false)
				put(t, filepath.Join(c, "stray"), false)
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "cache")
			c.leave(t, cache)
			got := leftoversOn(c.goos, cache)
			if len(got) != len(c.left) {
				t.Fatalf("leftovers = %q, want %q under %s", got, c.left, cache)
			}
			for i, want := range c.left {
				if got[i] != filepath.Join(cache, want) {
					t.Errorf("leftovers[%d] = %q, want %q", i, got[i], filepath.Join(cache, want))
				}
			}
		})
	}
}

// put makes path, a directory or an empty file, and what it sits in.
func put(t *testing.T, path string, dir bool) {
	t.Helper()
	if dir {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
