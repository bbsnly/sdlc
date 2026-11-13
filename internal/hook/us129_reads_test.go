package hook

// US-129 AC-5 for the hook: one hook call, run as cmd/sdlc runs it
// (RunHosted) against AC-8's story held mid-walk, reads each of
// .sdlc/config.json, the backlog, a story's gate record and
// .sdlc/state/tests.lock, of any checkout, at most once, unless it wrote that
// file in between.
//
// How the reads are counted. The standard library notes every file a test
// process opens, and every directory it moves to, when the test binary runs
// with -test.testlogfile: os.OpenFile, and so os.Open, os.ReadFile and
// os.Root.Open, call internal/testlog, which go test uses to cache results.
// Each case runs in a process of its own, this test binary run again for
// TestUS129ReadsChild alone, which marks where the measured call starts and
// ends by asking for two variables, which the log notes too. Only what the
// call opens is counted, not what building the project opened. A file the
// process writes is written through fsx.WriteFileAtomic, which opens
// .<name>.tmp* beside it and renames that over it: an open of one is a write
// of the file, after which one more read is allowed.

import (
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// us129Watched is whether path is one of the four files AC-5 names, in any
// checkout.
func us129Watched(path string) bool {
	slash := filepath.ToSlash(path)
	if strings.HasSuffix(slash, "/.sdlc/config.json") || strings.HasSuffix(slash, "/user_stories.json") ||
		strings.HasSuffix(slash, "/.sdlc/state/tests.lock") {
		return true
	}
	return pathpkg.Base(slash) == "gate-record.json" && pathpkg.Base(pathpkg.Dir(pathpkg.Dir(slash))) == "stories"
}

// us129ReadsIn runs case name in a process of its own and returns, for each
// watched file, the most times the call read it with no write of it between.
func us129ReadsIn(t *testing.T, name string) map[string]int {
	t.Helper()
	dir := t.TempDir()
	logPath, sidePath := filepath.Join(dir, "testlog"), filepath.Join(dir, "side")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUS129ReadsChild$", "-test.count=1",
		"-test.timeout=5m", "-test.testlogfile="+logPath)
	cmd.Env = append(os.Environ(), "U129_READS_CASE="+name, "U129_READS_SIDE="+sidePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the process for %s failed: %v\n%s", name, err, out)
	}
	side, err := os.ReadFile(sidePath)
	if err != nil {
		t.Fatalf("the process for %s made no call: %v", name, err)
	}
	cwd := strings.TrimSpace(string(side))
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	since, most := map[string]int{}, map[string]int{}
	in := false
	for line := range strings.Lines(string(raw)) {
		op, arg, _ := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		if (op == "open" || op == "chdir") && !filepath.IsAbs(arg) {
			arg = filepath.Join(cwd, arg)
		}
		switch {
		case op == "getenv" && arg == "U129_READS_BEGIN":
			in = true
		case op == "getenv" && arg == "U129_READS_END":
			in = false
		case op == "chdir":
			cwd = filepath.Clean(arg)
		case !in || op != "open":
		default:
			p := filepath.Clean(arg)
			if base := filepath.Base(p); strings.HasPrefix(base, ".") && strings.Contains(base, ".tmp") {
				since[filepath.Join(filepath.Dir(p), base[1:strings.Index(base, ".tmp")])] = 0
				continue
			}
			if us129Watched(p) {
				since[p]++
				most[p] = max(most[p], since[p])
			}
		}
	}
	return most
}

// us129Mark is the child's side of the count: where it is, then the call
// between the two marks.
func us129Mark(t *testing.T, call func()) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("U129_READS_SIDE"), []byte(cwd+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Getenv("U129_READS_BEGIN")
	call()
	os.Getenv("U129_READS_END")
}

// TestUS129ReadsChild is the process a case of TestAC5 runs in, and nothing
// on its own: it is how a row gets a process with the open log on.
func TestUS129ReadsChild(t *testing.T) {
	name := os.Getenv("U129_READS_CASE")
	if name == "" {
		t.Skip("the process TestAC5EachHookCallReadsEachFileOfTheLoopAtMostOnce runs each of its cases in")
	}
	for _, e := range us129Events() {
		if e.name == name {
			l := us129MidWalk(t)
			us129Mark(t, func() { us129Call(t, e, l) })
			return
		}
	}
	for _, c := range us129MoreReads() {
		if c.event.name == name {
			l := c.layout(t)
			us129Mark(t, func() { us129Call(t, c.event, l) })
			return
		}
	}
	t.Fatalf("no hook event %q", name)
}

// us129MoreReads are the Bash calls that read the most, each on the story it
// is made on in the loop: the session's commit once every review is fresh,
// and `sdlc stop` once the retro has passed (US-129 red team, F-10).
func us129MoreReads() []struct {
	layout func(t *testing.T) swLayout
	event  us129Event
} {
	bash := func(command string) func(swLayout) map[string]any {
		return func(swLayout) map[string]any {
			return map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command}}
		}
	}
	return []struct {
		layout func(t *testing.T) swLayout
		event  us129Event
	}{
		{func(t *testing.T) swLayout {
			t.Helper()
			l := swCheckouts(t)
			swReady(t, l)
			return l
		}, us129Event{name: "PreToolUse Bash git commit with every review fresh", event: "PreToolUse", payload: bash("git add -A && git commit -m x")}},
		{func(t *testing.T) swLayout {
			t.Helper()
			l := swCheckouts(t)
			wtRecord(t, l.main, "A-1", model.Gates...)
			return l
		}, us129Event{name: "PreToolUse Bash sdlc stop after the retro", event: "PreToolUse", payload: bash("sdlc stop")}},
	}
}

func TestAC5EachHookCallReadsEachFileOfTheLoopAtMostOnce(t *testing.T) {
	events := us129Events()
	for _, c := range us129MoreReads() {
		events = append(events, c.event)
	}
	for _, e := range events {
		t.Run("AC-5 "+e.name, func(t *testing.T) {
			t.Parallel()
			for p, n := range us129ReadsIn(t, e.name) {
				if n > 1 {
					t.Errorf("AC-5 %s read %s %d times, want at most once between its own writes of it", e.name, p, n)
				}
			}
		})
	}
}
