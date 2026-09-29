package policy

// US-029.2, T7: a story's worktree sits inside the main checkout, so a path
// there is inside the repository the file tools were given and is still not
// the held checkout's to write. The refusal says whose it is and where to work
// instead, rather than "work inside the repository", which it already is.

import (
	"strings"
	"testing"
)

// AC-6 and T7: a file in another checkout nested in the held one is refused
// to every writing tool and every role, before any other rule, with a reason
// and a route of its own.
func TestAC6AndT7AFileInANestedCheckoutIsRefusedAndSentBackToTheHeldCheckout(t *testing.T) {
	for _, r := range []Request{
		{Tool: "Write", Path: ".sdlc/worktrees/B-2/src.go", OtherCheckout: true, Story: "A-1"},
		{Tool: "Edit", Agent: "sdlc-implementer", Path: ".sdlc/worktrees/B-2/internal/x.go", OtherCheckout: true, Story: "A-1"},
		{Tool: "Write", Agent: "sdlc-sdet", Path: ".sdlc/worktrees/B-2/x_test.go", OtherCheckout: true, Story: "A-1",
			Tests: Tests{IsTest: true}},
		{Tool: "MultiEdit", Path: ".sdlc/worktrees/B-2/.sdlc/state/active", OtherCheckout: true, Story: "A-1"},
		// Where the caller could not name the path inside that checkout.
		{Tool: "Write", OtherCheckout: true, Story: "A-1"},
	} {
		v := Evaluate(r)
		switch {
		case v.Allowed || v.Rule != "write-outside-repository":
			t.Errorf("%s %s = %+v, want write-outside-repository", r.Tool, r.Path, v)
		case !strings.Contains(v.Reason, "another checkout of this repository"):
			t.Errorf("%s: reason = %s", r.Path, v.Reason)
		case !strings.Contains(v.Route, "another story's checkout") || strings.Contains(v.Route, "work inside the repository"):
			t.Errorf("%s: route = %s, want the other checkout's own", r.Path, v.Route)
		}
	}
	// A path outside the repository keeps the rule's own route, and the same
	// path in the held checkout is not refused for being there.
	if v := Evaluate(Request{Tool: "Write", Path: "/etc/hosts", Outside: true}); v.Route != fixtureRule(t, "write-outside-repository").Route {
		t.Errorf("outside the repository the route is %q", v.Route)
	}
	if v := Evaluate(Request{Tool: "Write", Path: ".sdlc/worktrees/B-2/src.go", Story: "A-1"}); !v.Allowed {
		t.Errorf("a path the caller did not place in another checkout = %+v", v)
	}
	if v := Evaluate(Request{Tool: "Read", Path: ".sdlc/worktrees/B-2/src.go", OtherCheckout: true}); !v.Allowed {
		t.Errorf("reading another checkout's file = %+v", v)
	}
}

// A path that is a story's directory itself, or its stories directory, names
// no story and no file in one.
func TestAStoriesDirectoryItselfNamesNoStoryFile(t *testing.T) {
	for _, p := range []string{".sdlc/stories", ".sdlc/stories/A-1", ".sdlc/stories/A-1/", "internal/x.go"} {
		if story, rest := storySplit(p); story != "" || rest != "" {
			t.Errorf("storySplit(%q) = %q, %q; want nothing", p, story, rest)
		}
	}
	if story, rest := storySplit(".sdlc/stories/A-1/reviews/x.md"); story != "A-1" || rest != "reviews/x.md" {
		t.Errorf("storySplit of a review = %q, %q", story, rest)
	}
}

func fixtureRule(t *testing.T, id string) Rule {
	t.Helper()
	for _, r := range Rules {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no rule %s", id)
	return Rule{}
}
