package hook

// US-129 AC-8, for the call the binary makes: each hook event's first call in
// a process that has made none, as every hook process is, allocates no more
// than it did at the story's base, and all of them together at most 60% of
// that. The AC-8 row measures after a warm-up, which a cache kept by the
// package and filled by the warm-up would pass while the binary, which makes
// one call a process, gained nothing (US-129 red team, F-14 and R2). Each
// event runs in a child process of this test binary (TestUS129FirstCallChild)
// with the collector off, its fixture built before the measure.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// us129FirstBase is what each event's first call allocated at 37e3ac8, by the
// operating system it was measured on (Gate 3): the most of the runs taken.
var us129FirstBase = map[string]map[string]uint64{
	"darwin": {
		"PreToolUse Bash": 1115040, "PreToolUse Write": 119352, "PreToolUse Edit": 119432, "PostToolUse": 82336,
		"SubagentStart": 970456, "SubagentStop": 989672, "Stop": 726640, "SessionStart": 4824,
	},
	"linux": {
		"PreToolUse Bash": 1062320, "PreToolUse Write": 73280, "PreToolUse Edit": 73296, "PostToolUse": 58952,
		"SubagentStart": 992016, "SubagentStop": 961784, "Stop": 658576, "SessionStart": 4664,
	},
}

// us129FirstNoise is how far a first call may sit above its pin: Linux's
// SubagentStart moved by 5% over three runs of one tree, the rest by less
// than 1%, and SessionStart's 4.8 KB by 192 bytes. The bound that cuts is the
// total's, and AC-8's row holds each event to 1%.
const us129FirstNoise = 10 // one part in ten

// us129FirstFloor is the least a first call may sit above its pin, in bytes.
// The small events move with the host more than with the code: on one tree
// ubuntu-latest measured the PostToolUse call at 66,224, 70,592 and 75,984
// bytes (CI runs 45256250859 and 45259902598), where a Linux container
// measures 62,000 against a pin of 58,952, and a longer TMPDIR alone adds
// 8 KB to it in path lookups. 16 KiB was not enough. 32 KiB is still a
// fraction of the hundreds of KB a cost put back would show, and the total
// still holds the 60%.
const us129FirstFloor = 32 << 10

// TestUS129FirstCallChild is the process a case of the row runs in, and
// nothing on its own.
func TestUS129FirstCallChild(t *testing.T) {
	name := os.Getenv("U129_FIRST_EVENT")
	if name == "" {
		t.Skip("the process TestAC8EachEventsFirstCallInAFreshProcessAllocatesNoMoreThanAtTheBase runs each event in")
	}
	for _, e := range us129Events() {
		if e.name != name {
			continue
		}
		l := us129MidWalk(t)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		us129Call(t, e, l)
		runtime.ReadMemStats(&after)
		if err := os.WriteFile(os.Getenv("U129_FIRST_SIDE"), []byte(strconv.FormatUint(after.TotalAlloc-before.TotalAlloc, 10)), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no hook event %q", name)
}

// us129FirstIn is what the first call of the event named allocated in a new
// process.
func us129FirstIn(t *testing.T, name string) uint64 {
	t.Helper()
	side := filepath.Join(t.TempDir(), "bytes")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUS129FirstCallChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "U129_FIRST_EVENT="+name, "U129_FIRST_SIDE="+side, "GOGC=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the first %s call: %v\n%s", name, err, out)
	}
	raw, err := os.ReadFile(side)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAC8EachEventsFirstCallInAFreshProcessAllocatesNoMoreThanAtTheBase(t *testing.T) {
	if us129Race {
		// As AC-8's row: under the race detector sync.Pool drops what it is
		// handed at random, and no figure measured there is the code's.
		cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-v",
			"-run", "^TestAC8EachEventsFirstCallInAFreshProcessAllocatesNoMoreThanAtTheBase$", ".")
		cmd.Env = append(os.Environ(), "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("AC-8 without the race detector:\n%s", out)
		}
		t.Logf("AC-8 without the race detector:\n%s", out)
		return
	}
	bases, ok := us129FirstBase[runtime.GOOS]
	if !ok {
		t.Skipf("AC-8's first calls were measured on macOS and Linux; %s allocates otherwise in its file and process calls, and has no base", runtime.GOOS)
	}
	var got, base uint64
	for _, e := range us129Events() {
		n := us129FirstIn(t, e.name)
		got += n
		want, ok := bases[e.name]
		base += want
		t.Logf("%q: %d (base %d)", e.name, n, want)
		switch {
		case !ok:
			t.Errorf("AC-8 no base figure for the first %s call", e.name)
		case n > want+max(want/us129FirstNoise, us129FirstFloor):
			t.Errorf("AC-8 the first %s call in a new process allocated %d bytes, more than the %d it allocated at the base", e.name, n, want)
		}
	}
	if got*10 > base*6 {
		t.Errorf("AC-8 the first calls of the hook events allocated %d bytes in all, want at most 60%% of the %d they allocated at the base, %d",
			got, base, base*6/10)
	}
}
