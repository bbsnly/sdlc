package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/store"
)

// storyUnderWay is a project with an iteration running, started in session, or
// from a terminal when session is empty.
func storyUnderWay(t *testing.T, session string) string {
	t.Helper()
	root := gitProject(t)
	initialised(t)
	t.Setenv(store.SessionEnv, session)
	mustRun(t, "start")
	return root
}

// projectFiles is every file and directory in the project outside .git, with
// what each file holds.
func projectFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		switch {
		case d.IsDir() && rel == ".git":
			return filepath.SkipDir
		case d.IsDir():
			files[rel] = "directory"
			return nil
		}
		raw, err := os.ReadFile(path)
		files[rel] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sessionRefused(r result) bool {
	return strings.Contains(r.stdout+r.stderr, "SDLC-E0048")
}

// A story is changed only from the Claude Code session working it, and from a
// terminal. The hook held that session and its agents to the story and nothing
// held any other: a model in another session could run `sdlc artifact write`
// into a story it did not start. Refused, a command changes nothing at all.
func TestAStoryIsChangedOnlyFromTheSessionWorkingIt(t *testing.T) {
	const owner, other = "session-owner", "session-other"
	for _, c := range []struct {
		name, stdin string
		args        []string
	}{
		{"artifact write", "# Analysis\n", []string{"artifact", "write", "analysis"}},
		{"review add", "reads well\n", []string{"review", "add", "design_review", "red-team", "note"}},
		{"gate", "", []string{"gate", "analysis", "fail", "--note", "not yet"}},
		{"escalate", "", []string{"escalate", "question", "--message", "which way?"}},
		{"cost add", "", []string{"cost", "add", "--usd", "1.00"}},
		{"freeze", "", []string{"freeze"}},
		{"unfreeze", "", []string{"unfreeze", "--reason", "the test was wrong"}},
		{"approve", "", []string{"approve"}},
		{"stop", "", []string{"stop"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := storyUnderWay(t, owner)
			t.Setenv(store.SessionEnv, other)
			before := projectFiles(t, root)
			r := runWith(t, c.stdin, c.args...)
			if r.code == 0 || !sessionRefused(r) {
				t.Errorf("from another session: exit %d\n%s", r.code, r.stderr)
			}
			if !strings.Contains(r.stderr, "/sdlc:next") {
				t.Errorf("the refusal does not say how a person takes the story over:\n%s", r.stderr)
			}
			// A model cannot type /sdlc:next, and the nearest thing it can run
			// takes the story over.
			if !strings.Contains(r.stderr, `does not run "sdlc start"`) {
				t.Errorf("the refusal does not keep an assistant from taking the story over:\n%s", r.stderr)
			}
			after := projectFiles(t, root)
			for path, was := range before {
				if now, ok := after[path]; !ok || now != was {
					t.Errorf("the refused command changed %s", path)
				}
			}
			for path := range after {
				if _, ok := before[path]; !ok {
					t.Errorf("the refused command created %s", path)
				}
			}

			for _, allowed := range []struct {
				name, session string
				unrecorded    bool
			}{
				{"the session working it", owner, false},
				{"a terminal", "", false},
				{"another session, with no session recorded", other, true},
				{"a session id that cannot be one, as a terminal", "not a session!", false},
			} {
				root := storyUnderWay(t, owner)
				if allowed.unrecorded {
					if err := os.Remove(filepath.Join(root, ".sdlc", "state", "session")); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv(store.SessionEnv, allowed.session)
				r := runWith(t, c.stdin, c.args...)
				if sessionRefused(r) {
					t.Errorf("from %s: refused\n%s", allowed.name, r.stderr)
				}
				// An agent of the session working the story stores its document.
				if c.name == "artifact write" && !allowed.unrecorded && r.code != 0 {
					t.Errorf("from %s: exit %d\n%s", allowed.name, r.code, r.stderr)
				}
			}
		})
	}
}

// `sdlc start` is how a session takes a story over, so another session's start
// is not refused: it moves the story there, and from then on the session it
// came from is the one refused.
func TestAnotherSessionTakesAStoryOverWithStart(t *testing.T) {
	root := storyUnderWay(t, "session-owner")
	t.Setenv(store.SessionEnv, "session-other")
	if r := runWith(t, "# Analysis\n", "artifact", "write", "analysis"); !sessionRefused(r) {
		t.Fatalf("another session wrote into the story before taking it over:\n%s", r.stderr)
	}
	mustRun(t, "start")
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "state", "session"))
	if err != nil || strings.TrimSpace(string(raw)) != "session-other" {
		t.Fatalf("start in another session left session %q (%v)", raw, err)
	}
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	t.Setenv(store.SessionEnv, "session-owner")
	if r := runWith(t, "# Analysis\n", "artifact", "write", "analysis"); !sessionRefused(r) {
		t.Errorf("the session the story was taken from still wrote into it:\n%s", r.stderr)
	}
}

// Nothing is refused for the session where there is no story it would write
// into: a session record left with no story under way, one that names no
// session, or a story that cannot be read, which the command itself reports.
// `sdlc ack` changes no story.
func TestTheSessionCheckKeepsOnlyAStoryUnderWay(t *testing.T) {
	const owner, other = "session-owner", "session-other"
	t.Run("a session record left with no story under way", func(t *testing.T) {
		root := storyUnderWay(t, owner)
		mustRun(t, "stop")
		writeFile(t, root, ".sdlc/state/session", owner+"\n")
		t.Setenv(store.SessionEnv, other)
		if r := run(t, "gate", "analysis", "fail", "--note", "x"); sessionRefused(r) {
			t.Errorf("refused with no story under way:\n%s", r.stderr)
		}
	})
	t.Run("sdlc ack while a story is under way", func(t *testing.T) {
		storyUnderWay(t, owner)
		t.Setenv(store.SessionEnv, other)
		if r := run(t, "ack", "--through", "HEAD"); r.code != 0 {
			t.Errorf("sdlc ack from another session: exit %d\n%s", r.code, r.stderr)
		}
	})
	t.Run("a session record that names no session", func(t *testing.T) {
		root := storyUnderWay(t, owner)
		writeFile(t, root, ".sdlc/state/session", "not a session!\n")
		t.Setenv(store.SessionEnv, other)
		mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	})
	t.Run("an active story that cannot be read", func(t *testing.T) {
		root := storyUnderWay(t, owner)
		active := filepath.Join(root, ".sdlc", "state", "active")
		if err := os.Remove(active); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(active, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(store.SessionEnv, other)
		if r := runWith(t, "# Analysis\n", "artifact", "write", "analysis"); sessionRefused(r) {
			t.Errorf("refused over a story that cannot be read:\n%s", r.stderr)
		}
	})
}
