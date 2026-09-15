package shellpolicy

import (
	"path"
	"strings"
)

// A download writes where it is told to, and a URL is not a path on this
// machine: `curl -fsSL https://example.com/CLAUDE.md` prints another project's
// CLAUDE.md, and writes nothing.

// curlWrites are the files curl writes: its output, dumped headers and cookie
// jar, and with -O the last part of each URL, in --output-dir when given.
func curlWrites(args []string) []string {
	return downloadWrites(args, download{
		writes:     map[string]bool{"o": true, "D": true, "c": true, "--output": true, "--dump-header": true, "--cookie-jar": true},
		dir:        map[string]bool{"--output-dir": true},
		remote:     map[string]bool{"O": true, "--remote-name": true, "--remote-name-all": true},
		valued:     "dHuXAemwxFbKErTzYyCUQt",
		remoteOnly: true,
	})
}

// wgetWrites are the files wget writes: its output document and log, and
// unless the output document is named, the last part of each URL, under -P
// when given.
func wgetWrites(args []string) []string {
	return downloadWrites(args, download{
		writes: map[string]bool{"O": true, "o": true, "a": true, "--output-document": true, "--output-file": true, "--append-output": true},
		dir:    map[string]bool{"P": true, "--directory-prefix": true},
	})
}

type download struct {
	writes, dir, remote map[string]bool
	// valued are the one-letter options that take a value, which ends a bundle:
	// `curl -XPOST` asks for no -O.
	valued string
	// remoteOnly is curl's way: a URL is saved under its own name only when
	// asked to be. wget saves it so unless an output document is named.
	remoteOnly bool
}

func downloadWrites(args []string, d download) []string {
	var written, urls []string
	dir, named, remote := "", false, false
	take := func(option, value string) {
		switch {
		case d.dir[option]:
			dir = value
		case d.writes[option]:
			named = named || option == "O" || option == "--output-document"
			written = append(written, value)
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.Contains(a, "://"):
			urls = append(urls, a)
		case strings.HasPrefix(a, "--"):
			name, value, glued := strings.Cut(a, "=")
			remote = remote || d.remote[name]
			if (d.writes[name] || d.dir[name]) && !glued && i+1 < len(args) {
				i++
				value = args[i]
			}
			take(name, value)
		case strings.HasPrefix(a, "-"):
			for j := 1; j < len(a); j++ {
				letter := a[j : j+1]
				remote = remote || d.remote[letter]
				if !d.writes[letter] && !d.dir[letter] && !strings.Contains(d.valued, letter) {
					continue
				}
				value := a[j+1:]
				if value == "" && i+1 < len(args) {
					i++
					value = args[i]
				}
				take(letter, value)
				break
			}
		}
	}
	if remote || !d.remoteOnly && !named {
		for _, u := range urls {
			u, _, _ = strings.Cut(u, "?")
			written = append(written, path.Join(dir, path.Base(u)))
		}
	}
	return written
}
