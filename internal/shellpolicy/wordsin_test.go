package shellpolicy

import (
	"slices"
	"testing"
)

func TestWordsInKeepsTheColonOfADriveOnItsWord(t *testing.T) {
	for _, row := range []struct {
		text string
		want []string
	}{
		{"ls C:/Users/p/* | xargs cp -t .", []string{"ls", "C:/Users/p/", "xargs", "cp", "-t", "."}},
		{"d:/x", []string{"d:/x"}},
		// Only one letter then `:/` is a drive; every other colon still ends a word.
		{"xC:/z", []string{"xC", "/z"}},
		{"C:x", []string{"C", "x"}},
		{"a:.sdlc/state", []string{"a", ".sdlc/state"}},
		{"PATH=/a:/b", []string{"PATH", "/a", "/b"}},
		{"C:", []string{"C"}},
	} {
		if got := wordsIn(row.text); !slices.Equal(got, row.want) {
			t.Errorf("wordsIn(%q) = %q, want %q", row.text, got, row.want)
		}
	}
}
