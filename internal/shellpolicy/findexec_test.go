package shellpolicy

import "testing"

// find running a program that only reads writes nothing. It is the ordinary
// way to search files of a kind: `find . -name '*.md' -exec wc -l {} +` was a
// write to CLAUDE.md, and `-exec grep` over `*.json` one to the configuration.
func TestFindRunningAReaderWritesNothing(t *testing.T) {
	for _, command := range []string{
		"find . -name '*.md' -exec wc -l {} +",
		"find . -name '*.json' -exec grep -l version {} +",
		`find . -name '*.md' -execdir cat {} \;`,
		`find . -name CLAUDE.md -exec head -5 {} \;`,
		`find .sdlc -name active -exec ls -l {} \;`,
		"find . -name '*.json' -ok cat {} +",
		"find . -name '*.json' -okdir cat {} +",
		"find . -name '*.md' -exec /usr/bin/wc -l {} +",
	} {
		allowed(t, command, ready)
	}
	for _, reader := range []string{
		"cat", "head", "tail", "wc", "grep", "rg", "ls", "stat", "file", "du", "cksum",
		"md5sum", "sha1sum", "sha256sum", "shasum", "diff", "cmp", "basename", "dirname", "realpath",
	} {
		allowed(t, "find . -name '*.md' -exec "+reader+" {} +", ready)
	}

	// What it runs changes files, or cannot be seen to leave them alone.
	for _, command := range []string{
		"find . -name '*.md' -exec rm {} +",
		`find . -name active -exec sh -c 'rm $1' _ {} \;`,
		"find . -name config.json -exec sed -i s/a/b/ {} +",
		`find . -name active -exec wc -l {} \; -exec rm {} \;`,
		`find . -name active -execdir rm {} \;`,
		`find . -name active -ok rm {} \;`,
		`find . -name active -okdir rm {} \;`,
		"find . -name active -exec ./scripts/tidy {} +",
		"find . -name active -exec",
		"find . -name active -delete",
	} {
		if _, ok := Inspect(command, ready); !ok {
			t.Errorf("allowed: %s", command)
		}
	}
}
