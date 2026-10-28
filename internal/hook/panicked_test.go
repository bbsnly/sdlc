package hook

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// US-035 AC-2 and THREATS I1: the debug log says a panic happened, and by
// what type, but the value of a panic the code raised can quote a word of the
// command, and the log is a file anyone on the machine may read. Only the
// runtime's own message, which holds no word of the command, is given.
func TestAC2APanicIsLoggedByItsTypeAndNeverByAValueThatCouldQuoteTheCommand(t *testing.T) {
	t.Parallel()
	// Split so that the secrets scan does not take the fixture for a token.
	const secret = "ghp_" + "zq7SecretShapedToken0123456789abcdefXYZ"
	for _, r := range []any{"touch " + secret, errors.New(secret), fmt.Errorf("wrapped: %w", errors.New(secret)), []string{secret}} {
		attrs := panicked(r)
		if !slices.Contains(attrs, any("panic_type")) || !slices.Contains(attrs, any(fmt.Sprintf("%T", r))) {
			t.Errorf("panicked(%T) = %v, want its type under panic_type", r, attrs)
		}
		if strings.Contains(fmt.Sprint(attrs...), "zq7") {
			t.Errorf("panicked(%T) = %v quotes the value", r, attrs)
		}
	}

	var r any
	func() {
		defer func() { r = recover() }()
		var words []string
		_ = words[len(t.Name())]
	}()
	attrs := panicked(r)
	i := slices.Index(attrs, any("panic"))
	if i < 0 || i+1 >= len(attrs) || !strings.Contains(fmt.Sprint(attrs[i+1]), "index out of range") {
		t.Errorf("panicked(a runtime error) = %v, want the runtime's own message under panic", attrs)
	}
}

// US-035 AC-5: a word whose place only the shell knows is over no bound: the
// home of nobody this machine knows, and on Windows a path relative to a
// drive's own working directory. The command alone, Bound(""), is the
// resolver's answer.
func TestAC5AWordWhosePlaceOnlyTheShellKnowsIsOverNoBound(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res := pathrules.NewResolver(root).Memoised().Budgeted(pathrules.CommandLinks)
	for _, w := range []string{"~zq7nobodyknown/x", `C:x`, "notes.txt"} {
		if err := boundOnDisk(res, w); err != nil {
			t.Errorf("boundOnDisk(%q) = %v, want nil", w, err)
		}
	}
	if err := boundOnDisk(res, ""); err != nil {
		t.Errorf("boundOnDisk(\"\") before any bound was passed = %v, want nil", err)
	}
}
