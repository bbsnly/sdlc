package sdlcerr

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// US-115 AC-3 and AC-8. A block with no important finding is refused under a code
// of its own, the next after SDLC-E0085. Found by number, so that this package
// keeps compiling until it is registered.
func TestAC3TheBlockCodeIsNamedBlockWithoutImportantIsDocumentedAndSaysWhatToDo(t *testing.T) {
	const id = "SDLC-E0086"
	raw, err := os.ReadFile("catalogue.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s*BlockWithoutImportant\s*=\s*register\("` + id + `"`).Match(raw) {
		t.Errorf("catalogue.go does not register %s as BlockWithoutImportant", id)
	}
	code := registered(id)
	if code.String() != id {
		t.Fatalf("the catalogue has no %s", id)
	}
	fix := strings.ToLower(fixFor(code))
	for _, word := range []string{"--important", "approve", "nits"} {
		if !strings.Contains(fix, word) {
			t.Errorf("the fix for %s does not say %q: %s", id, word, fix)
		}
	}
	body, ok := documentedCodes(t)[id]
	if !ok || isRetired(body) {
		t.Fatalf("%s has no live `### %s` section in %s", id, id, troubleshootingPath)
	}
	flat := strings.ToLower(strings.Join(strings.Fields(body), " "))
	for _, word := range []string{"--important", "important finding", "review policy", "only nits", "approve"} {
		if !strings.Contains(flat, word) {
			t.Errorf("the section for %s does not say %q:\n%s", id, word, body)
		}
	}
}
