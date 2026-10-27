package pathrules

import (
	"strings"
	"testing"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// foldEveryWay is Fold as it was before its ASCII path: every string through
// golang.org/x/text, and every segment split and joined again.
func foldEveryWay(p string) string {
	folded := norm.NFC.String(cases.Fold().String(norm.NFD.String(p)))
	parts := strings.Split(folded, "/")
	for i, part := range parts {
		part = strings.TrimSuffix(part, "::$data")
		part = strings.TrimSuffix(part, ":$i30:$index_allocation")
		part = strings.TrimSuffix(part, "::$index_allocation")
		if trimmed := strings.TrimRight(part, ". "); trimmed != "" {
			parts[i] = trimmed
		}
	}
	return strings.Join(parts, "/")
}

// Fold takes an ASCII path, and a path with no segment to trim, the short way.
// Every rule matches a path through Fold, so the short way has to give what the
// long way gives for every string: a path it folded differently would be a
// protected file one spelling of which no rule matches.
func TestFoldGivesWhatTheUnicodeFoldGivesForEveryPath(t *testing.T) {
	var paths []string
	// Every pair of ASCII bytes, alone, as a segment and before a separator.
	for a := range 128 {
		for b := range 128 {
			pair := string([]byte{byte(a), byte(b)})
			paths = append(paths, pair, "x/"+pair, pair+"/x", "."+pair+"/"+pair)
		}
	}
	paths = append(paths,
		"", "/", "//", ".", "..", "./", "a/./b", ". .", ".sdlc.", ".SDLC. /State /Active.",
		".sdlc/state/active::$DATA", "::$DATA", ".sdlc::$INDEX_ALLOCATION/state",
		".sdlc:$I30:$INDEX_ALLOCATION/state", "a:b", "C:/Users/X/Project/.Sdlc",
		"CLAUDE.md", "Claude.MD", "internal/X_TEST.go", "rm", "RM.EXE", "./Git",
		// And what is not ASCII, which keeps the long way.
		".ſdlc/state/active", "invoice_teﬆ.go", ".sdlc/ﬆate/active", "\u212aey", "e\u0301", "é.",
		"\xff", "a\x80b", "ÉTAT. /x",
	)
	for _, p := range paths {
		if got, want := Fold(p), foldEveryWay(p); got != want {
			t.Errorf("Fold(%q) = %q, the Unicode fold gives %q", p, got, want)
		}
	}
}
