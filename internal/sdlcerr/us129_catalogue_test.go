package sdlcerr

// US-129 AC-2: every SDLC-E code, its fix and the page it links to are what
// they were at the story's base, in the order they were registered. The
// catalogue is built by package variables, which is init work, and the story
// cuts init: the golden holds that whatever builds it builds the same one.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// us129Golden was written from 37e3ac8 with US129_WRITE_GOLDEN=1, before the
// freeze.
var us129Golden = filepath.Join("testdata", "us129_catalogue.golden")

func us129Catalogue() string {
	var b strings.Builder
	for _, e := range catalogue {
		fmt.Fprintf(&b, "%s\n  url %s\n  fix %s\n", e.code, e.code.URL(), e.fix)
	}
	return b.String()
}

func TestAC2EveryCodeItsFixAndItsPageAreWhatTheyWereAtTheBase(t *testing.T) {
	got := us129Catalogue()
	if os.Getenv("US129_WRITE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(us129Golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(us129Golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(us129Golden)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.SplitAfter(string(raw), "\n")
	have := strings.SplitAfter(got, "\n")
	for i := range max(len(want), len(have)) {
		var w, h string
		if i < len(want) {
			w = want[i]
		}
		if i < len(have) {
			h = have[i]
		}
		if w != h {
			t.Fatalf("AC-2 the catalogue differs from the base's at line %d of %s:\n got %q\nwant %q", i+1, us129Golden, h, w)
		}
	}
}
