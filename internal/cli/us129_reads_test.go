package cli

// US-129 AC-5 for the commands: one command, run in this process as cmd/sdlc
// runs it (ExecuteIn), in a project whose story has reached the
// implementation, reads each of .sdlc/config.json, the backlog, a story's
// gate record and .sdlc/state/tests.lock at most once, unless it wrote that
// file in between. internal/hook/us129_reads_test.go holds the same for each
// hook event.
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
	"bytes"
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

// us129Commands are the commands of AC-3 a story at the implementation can
// run here, each as its own case.
var us129Commands = [][]string{
	{"start"}, {"status", "--json"}, {"story", "list"}, {"staffing", "show"}, {"review", "list"},
	{"cost"}, {"trace"}, {"doctor"},
	{"artifact", "write", "plan"}, {"gate", "implementation", "pass", "--note", "counted"}, {"freeze"}, {"stop"},
}

// TestUS129ReadsChild is the process a case of TestAC5 runs in, and nothing
// on its own: it is how a row gets a process with the open log on.
func TestUS129ReadsChild(t *testing.T) {
	name := os.Getenv("U129_READS_CASE")
	if name == "" {
		t.Skip("the process TestAC5EachCommandReadsEachFileOfTheLoopAtMostOnce runs each of its cases in")
	}
	for _, args := range us129Commands {
		if strings.Join(args, " ") == name {
			root := startedAt(t, model.GateImplementation)
			us129Mark(t, func() {
				var out, errb bytes.Buffer
				ExecuteIn(root, os.Getenv, args, strings.NewReader("# Plan\n\nOne step.\n"), &out, &errb)
			})
			return
		}
	}
	for _, c := range us129MoreCommands {
		if c.name == name {
			root := c.at(t)
			us129Mark(t, func() {
				var out, errb bytes.Buffer
				ExecuteIn(root, c.getenv, c.args, strings.NewReader(us129Review), &out, &errb)
			})
			return
		}
	}
	t.Fatalf("no command %q", name)
}

// us129MoreCommands are the commands that read the most, each on the project
// it is run on in the loop, which a story at the implementation is not: a
// review that stamps the tree, the review gate and the commit gate, the stop
// that lands, and the release of a stopped story by a person (US-129 red
// team, F-10).
var us129MoreCommands = []struct {
	name   string
	args   []string
	at     func(t *testing.T) string
	getenv func(string) string
}{
	{"review add code_review security approve", []string{"review", "add", "code_review", "security", "approve", "--note", "counted"},
		func(t *testing.T) string {
			t.Helper()
			return startedAt(t, model.GateCodeReview)
		}, os.Getenv},
	{"gate verifier_review pass", []string{"gate", "verifier_review", "pass", "--note", "counted"},
		func(t *testing.T) string {
			t.Helper()
			root := startedAt(t, model.GateVerifierReview)
			mustRunWith(t, us129Review, "review", "add", "verifier_review", "verifier", "approve", "--note", "the verifier's")
			return root
		}, os.Getenv},
	{"gate commit pass", []string{"gate", "commit", "pass", "--note", "counted"},
		func(t *testing.T) string {
			t.Helper()
			root := startedAt(t, model.GateCommit)
			commitEverything(t, root)
			return root
		}, os.Getenv},
	{"stop with its landing", []string{"stop"}, finishedStory, os.Getenv},
	{"release of a stopped story", []string{"release", "US-001", "--reason", "counted"},
		func(t *testing.T) string {
			t.Helper()
			root := startedAt(t, model.GateImplementation)
			mustRun(t, "stop")
			return root
		}, us129AsAPerson},
}

func TestAC5EachCommandReadsEachFileOfTheLoopAtMostOnce(t *testing.T) {
	names := make([]string, 0, len(us129Commands)+len(us129MoreCommands))
	for _, args := range us129Commands {
		names = append(names, strings.Join(args, " "))
	}
	for _, c := range us129MoreCommands {
		names = append(names, c.name)
	}
	for _, name := range names {
		t.Run("AC-5 "+name, func(t *testing.T) {
			t.Parallel()
			for p, n := range us129ReadsIn(t, name) {
				if n > 1 {
					t.Errorf("AC-5 sdlc %s read %s %d times, want at most once between its own writes of it", name, p, n)
				}
			}
		})
	}
}
