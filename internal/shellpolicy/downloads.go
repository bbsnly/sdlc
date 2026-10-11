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
		valued: "eiBtTwQDARIXlU",
		longValued: map[string]bool{
			"--accept": true, "--reject": true, "--accept-regex": true, "--reject-regex": true,
			"--include-directories": true, "--exclude-directories": true, "--domains": true,
			"--exclude-domains": true, "--tries": true, "--timeout": true, "--wait": true,
			"--waitretry": true, "--quota": true, "--level": true, "--user-agent": true,
			"--header": true, "--user": true, "--password": true, "--http-user": true,
			"--http-password": true, "--post-data": true, "--post-file": true, "--body-data": true,
			"--body-file": true, "--method": true, "--referer": true, "--load-cookies": true,
			"--input-file": true, "--base": true, "--config": true, "--execute": true,
			"--limit-rate": true, "--bind-address": true, "--dns-timeout": true,
			"--connect-timeout": true, "--read-timeout": true, "--cut-dirs": true,
			"--default-page": true, "--restrict-file-names": true,
		},
	})
}

type download struct {
	writes, dir, remote map[string]bool
	// longValued are the long options that take the next word as their value.
	longValued map[string]bool
	// valued are the one-letter options that take a value, which ends a bundle:
	// `curl -XPOST` asks for no -O.
	valued string
	// remoteOnly is curl's way: a URL is saved under its own name only when
	// asked to be. wget saves it so unless an output document is named.
	remoteOnly bool
}

func downloadWrites(args []string, d download) []string {
	sc := downloadScan{d: d}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--url="):
			// Only its last part is read.
			sc.urls = append(sc.urls, a)
		case strings.HasPrefix(a, "--"):
			i = sc.long(args, i)
		case strings.HasPrefix(a, "-"):
			i = sc.bundle(args, i)
		default:
			// Without its scheme, a word is a URL all the same.
			sc.urls = append(sc.urls, a)
		}
	}
	if sc.remote || !d.remoteOnly && !sc.named {
		for _, u := range sc.urls {
			u, _, _ = strings.Cut(u, "?")
			sc.written = append(sc.written, path.Join(sc.dir, path.Base(u)))
		}
	}
	return sc.written
}

// downloadScan is downloadWrites part way through a download's words: the
// files it has been told to write, the URLs it fetches, the directory they
// are saved in, whether an output document was named, and whether it was
// asked to save a URL under its own name.
type downloadScan struct {
	d             download
	written, urls []string
	dir           string
	named, remote bool
}

// take records the value of an option that names a directory or a file.
func (sc *downloadScan) take(option, value string) {
	switch {
	case sc.d.dir[option]:
		sc.dir = value
	case sc.d.writes[option]:
		sc.named = sc.named || option == "O" || option == "--output-document"
		sc.written = append(sc.written, value)
	}
}

// long reads the long option at args[i], with its value where it takes one,
// and returns the index of the last word it read.
func (sc *downloadScan) long(args []string, i int) int {
	d := sc.d
	name, value, glued := strings.Cut(args[i], "=")
	sc.remote = sc.remote || d.remote[name]
	if (d.writes[name] || d.dir[name] || d.longValued[name]) && !glued && i+1 < len(args) {
		i++
		value = args[i]
	}
	sc.take(name, value)
	return i
}

// bundle reads the bundle of one-letter options at args[i], up to the first
// that takes a value, and returns the index of the last word it read.
func (sc *downloadScan) bundle(args []string, i int) int {
	d, a := sc.d, args[i]
	for j := 1; j < len(a); j++ {
		letter := a[j : j+1]
		sc.remote = sc.remote || d.remote[letter]
		if !d.writes[letter] && !d.dir[letter] && !strings.Contains(d.valued, letter) {
			continue
		}
		value := a[j+1:]
		if value == "" && i+1 < len(args) {
			i++
			value = args[i]
		}
		sc.take(letter, value)
		break
	}
	return i
}
