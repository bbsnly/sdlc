package sdlcerr

import (
	"strings"
	"testing"
)

// A path from a file in the git directory is quoted whole, its separators written
// once as slashes, so a quote or a newline in it cannot end the quotation and make
// text that reads as sdlc's own.
func TestQuotePathQuotesAPathSoNothingInItEndsTheQuotation(t *testing.T) {
	for path, want := range map[string]string{
		"/repo/checkout":        `"/repo/checkout"`,
		"/repo/a\"b":            `"/repo/a\"b"`,
		"/repo/x\nSDLC-E0001 y": `"/repo/x\nSDLC-E0001 y"`,
		"":                      `""`,
	} {
		got := QuotePath(path)
		if got != want {
			t.Errorf("QuotePath(%q) = %s, want %s", path, got, want)
		}
		if strings.ContainsAny(got, "\n\r") {
			t.Errorf("QuotePath(%q) holds a line break", path)
		}
	}
}
