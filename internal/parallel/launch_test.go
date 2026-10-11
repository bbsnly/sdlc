package parallel

// US-058 AC-1, AC-5 and AC-7 at the level of the one session the launcher
// starts: what it runs, where, with what, and what it grants. The package is
// new, and these name the functions the plan has to give it:
//
//	Allowlist() []string
//	Narrow(configured *[]string) ([]string, error)
//	Prompt(id string) string
//	Quote(word string) string
//	Command(ctx context.Context, dir, id string, allow []string) (*exec.Cmd, error)
//
// A fake `claude` first on PATH records what it was started with, so that the
// command is judged by what a shell makes of it and not by how it is spelt.

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/shellx"
)

// fakeClaude puts a `claude` first on PATH that writes the arguments it got,
// NUL-separated, and the directory it ran in, and returns where it wrote them.
func fakeClaude(t *testing.T) (argsFile, pwdFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a POSIX shell script; the launcher's Windows path runs under Git Bash and is not run here")
	}
	bin, out := t.TempDir(), t.TempDir()
	argsFile, pwdFile = filepath.Join(out, "args"), filepath.Join(out, "pwd")
	script := "#!/bin/sh\n" +
		"pwd -P > \"$PARALLEL_FAKE_PWD\"\n" +
		"printf '%s\\0' \"$@\" > \"$PARALLEL_FAKE_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PARALLEL_FAKE_ARGS", argsFile)
	t.Setenv("PARALLEL_FAKE_PWD", pwdFile)
	return argsFile, pwdFile
}

// launched runs Command's command to its end and returns the arguments the
// fake claude was given.
func launched(t *testing.T, dir, id string, allow []string) []string {
	t.Helper()
	argsFile, pwdFile := fakeClaude(t)
	cmd, err := Command(t.Context(), dir, id, allow)
	if err != nil {
		t.Fatalf("Command(%q, %q): %v", dir, id, err)
	}
	if err := shellx.Run(cmd); err != nil {
		t.Fatalf("running the session's command: %v", err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the command never reached claude: %v", err)
	}
	got, _ := os.ReadFile(pwdFile)
	if !sameDir(strings.TrimSpace(string(got)), dir) {
		t.Errorf("claude ran in %q, want %q", strings.TrimSpace(string(got)), dir)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
}

func sameDir(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	return err == nil && ra == rb
}

// allowedIn is every tool the arguments pre-approve: what follows an
// --allowedTools (or --allowed-tools) flag up to the next flag, with a
// comma-joined value split, and --flag=value read as well.
func allowedIn(args []string) []string {
	var out []string
	collecting := false
	for _, a := range args {
		switch {
		case a == "--allowedTools" || a == "--allowed-tools":
			collecting = true
		case strings.HasPrefix(a, "--allowedTools=") || strings.HasPrefix(a, "--allowed-tools="):
			_, v, _ := strings.Cut(a, "=")
			out = append(out, splitTools(v)...)
			collecting = false
		case strings.HasPrefix(a, "-"):
			collecting = false
		case collecting:
			out = append(out, splitTools(a)...)
		}
	}
	return out
}

func splitTools(v string) []string {
	var out []string
	for e := range strings.SplitSeq(v, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// blanketSkip is whether an argument turns permission prompts off wholesale.
func blanketSkip(a string) bool {
	l := strings.ToLower(a)
	return strings.Contains(l, "skip-permissions") || strings.Contains(l, "bypasspermissions") ||
		strings.Contains(l, "bypass-permissions")
}

func TestAC1TheSessionRunsTheNextSkillForOneStoryInTheDirectoryItIsGiven(t *testing.T) {
	dir := t.TempDir()
	args := launched(t, dir, "US-042", nil)
	prompts := 0
	for _, a := range args {
		if a == "/sdlc:next US-042" {
			prompts++
		} else if strings.Contains(a, "US-") {
			t.Errorf("an argument names a story other than the one launched: %q", a)
		}
	}
	if prompts != 1 {
		t.Errorf("claude was given %d prompts of /sdlc:next US-042, want exactly one: %q", prompts, args)
	}
	if !slices.Contains(args, "-p") && !slices.Contains(args, "--print") {
		t.Errorf("the session is not headless (no -p or --print): %q", args)
	}
	if got := Prompt("US-042"); got != "/sdlc:next US-042" {
		t.Errorf("Prompt = %q, want the skill and the one id", got)
	}
	if other := Prompt("US-7"); other != "/sdlc:next US-7" {
		t.Errorf("Prompt(US-7) = %q: the id is the story's and no other", other)
	}
}

// A session with no flag has default permissions: nothing is pre-approved, and
// nothing turns the prompts off. AC-5, read as the analysis agreed: "default
// permissions of the headless session, no blanket skip".
func TestAC5ADefaultSessionPreApprovesNothingAndSkipsNothing(t *testing.T) {
	args := launched(t, t.TempDir(), "US-001", nil)
	if got := allowedIn(args); len(got) != 0 {
		t.Errorf("a default session pre-approves %q", got)
	}
	for _, a := range args {
		if blanketSkip(a) || strings.HasPrefix(a, "--allowed") || strings.HasPrefix(a, "--permission-mode") {
			t.Errorf("a default session was given %q", a)
		}
	}
}

// With the flag, the session is given exactly the list it is handed, however
// awkward each entry is to put through `sh -c`, and nothing else; still no
// blanket skip, whatever the list.
func TestAC5AnUnattendedSessionIsGivenExactlyTheListAndNoBlanketSkip(t *testing.T) {
	for name, allow := range map[string][]string{
		"built in": Allowlist(),
		"awkward": {
			"Bash(go test:*)", `Bash(echo it's "quoted" $(not run) ` + "`not run`" + `:*)`,
			"Read", "Edit(/a b/*)", "Bash(x);touch PWNED",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			args := launched(t, dir, "US-001", allow)
			got, want := slices.Clone(allowedIn(args)), slices.Clone(allow)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("the session pre-approves\n  %q\nwant\n  %q", got, want)
			}
			for _, a := range args {
				if blanketSkip(a) {
					t.Errorf("an unattended session was given %q: the flag grants a list, never a skip", a)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
				t.Error("an allowlist entry ran as a command")
			}
		})
	}
}

// AC-7: the session is started through shellx, so an interrupt and StopAll end
// it and everything it started, and the launcher builds no process of its own.
// The environment and the input are the person's: nothing is added, hidden or
// held open for the session to wait on.
func TestAC7TheSessionIsStartedThroughShellxWithTheEnvironmentAsItIs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell on PATH; Windows runs it under Git Bash")
	}
	dir := t.TempDir()
	cmd, err := Command(t.Context(), dir, "US-001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != dir {
		t.Errorf("Dir = %q, want %q", cmd.Dir, dir)
	}
	if len(cmd.Args) != 3 || cmd.Args[1] != "-c" || !strings.Contains(cmd.Args[2], "claude") {
		t.Errorf("Args = %q: not what shellx.Command builds (sh -c <command>)", cmd.Args)
	}
	if cmd.Stdin != nil {
		t.Errorf("Stdin = %v: a headless session reads nothing, and a nil one is the null device", cmd.Stdin)
	}
	if cmd.Env != nil {
		for _, kv := range cmd.Env {
			name, _, _ := strings.Cut(kv, "=")
			if !slices.Contains(os.Environ(), kv) {
				t.Errorf("the session's environment has %s, which the process's does not", name)
			}
		}
		if len(cmd.Env) != len(os.Environ()) {
			t.Errorf("the session's environment has %d variables, the process's %d: nothing is hidden",
				len(cmd.Env), len(os.Environ()))
		}
	}
	// InGroup is what has an interrupt end the tree, not just the shell.
	if cmd.Cancel == nil || cmd.WaitDelay <= 0 {
		t.Error("the command was not put in a group (shellx.InGroup)")
	}
}

// Every word crosses `sh -c`, so a word that is the person's, or a repository
// file's, must come out the other end as it went in.
func TestAC7QuoteGivesTheShellTheWordAsItWasWrittenWhateverItHolds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell on PATH; Windows runs it under Git Bash")
	}
	for _, word := range []string{
		"plain", "", "two words", "it's", `say "hi"`, "$(touch PWNED)", "`touch PWNED`", "a;touch PWNED",
		"Bash(git commit:*)", "*", "back\\slash", "line\nbreak", "-n", "~", "$HOME", "!x", "a&b|c>d<e",
	} {
		dir := t.TempDir()
		cmd, err := shellx.Command(t.Context(), dir, "printf %s "+Quote(word))
		if err != nil {
			t.Skipf("no shell: %v", err)
		}
		out, err := cmd.Output()
		if err != nil || string(out) != word {
			t.Errorf("Quote(%q) reached the shell as %q (%v)", word, out, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "PWNED")); err == nil {
			t.Errorf("Quote(%q) ran something", word)
		}
	}
}

// ------------------------------------------------------------- the allowlist

var toolEntry = regexp.MustCompile(`^[A-Z][A-Za-z]*(\(.+\))?$`)

func TestAC5TheBuiltInAllowlistIsShortScopedAndHasNothingTheLoopRefuses(t *testing.T) {
	list := Allowlist()
	if len(list) == 0 {
		t.Fatal("the unattended flag grants nothing")
	}
	seen := map[string]bool{}
	for _, e := range list {
		if seen[e] {
			t.Errorf("%q is listed twice", e)
		}
		seen[e] = true
		if !toolEntry.MatchString(e) {
			t.Errorf("%q is not a tool name with an optional scope", e)
		}
		if strings.Contains(e, ",") {
			t.Errorf("%q holds a comma, which a comma-joined flag value could not carry", e)
		}
		lower := strings.ToLower(e)
		for _, banned := range []string{
			"curl", "wget", "webfetch", "websearch", "push", "claude", "sdlc approve", "sdlc unfreeze",
			"sdlc ack", "sdlc release", "sdlc parallel", "sudo", "ssh", "nc ", "skip", "bypass",
		} {
			if strings.Contains(lower, banned) {
				t.Errorf("%q has %q in it: nothing that reaches the network, pushes, starts a session or decides for a person", e, banned)
			}
		}
		name, scope, _ := strings.Cut(e, "(")
		switch name {
		case "Bash":
			inner := strings.TrimSuffix(scope, ")")
			if inner == "" || inner == "*" || inner == "*:*" || inner == ":*" || strings.HasPrefix(inner, "*") {
				t.Errorf("%q lets every command through", e)
			}
		case "Edit", "Write", "MultiEdit", "NotebookEdit":
			if scope == "" {
				t.Errorf("%q is not scoped to a path: writes are scoped, not the machine's", e)
			}
		}
		// The loop's own refusals must not be in it: the hook would refuse the
		// call, and the entry would promise what is not there.
		if name == "Bash" {
			command := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(scope, ")"), ":*"), "*")
			if f, refused := shellpolicy.HumanDecisions(command, false, func(string) bool { return true }); refused {
				t.Errorf("%q is a human decision (%s)", e, f.Rule)
			}
		}
	}
	for _, need := range []string{"Bash", "Read"} {
		if !slices.ContainsFunc(list, func(e string) bool { return e == need || strings.HasPrefix(e, need+"(") }) {
			t.Errorf("the list has no %s entry, so no story could be worked unattended", need)
		}
	}
	if !slices.ContainsFunc(list, func(e string) bool { return strings.HasPrefix(e, "Edit") || strings.HasPrefix(e, "Write") }) {
		t.Error("the list lets a session write nothing")
	}
}

func TestAC5AllowlistHandsOutACopyEachTime(t *testing.T) {
	first := Allowlist()
	want := slices.Clone(first)
	for i := range first {
		first[i] = "Bash(*)"
	}
	if got := Allowlist(); !slices.Equal(got, want) {
		t.Errorf("changing one result changed the next: %q", got)
	}
}

func TestAC5NarrowingNothingGivesTheWholeList(t *testing.T) {
	got, err := Narrow(nil)
	if err != nil || !slices.Equal(got, Allowlist()) {
		t.Errorf("Narrow(nil) = %q, %v; want the built-in list", got, err)
	}
}

func TestAC5NarrowingKeepsOnlyWhatIsNamedInTheBuiltInOrder(t *testing.T) {
	list := Allowlist()
	if len(list) < 3 {
		t.Skip("needs three entries to show the order")
	}
	named := []string{list[2], list[0], list[2]}
	got, err := Narrow(&named)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{list[0], list[2]}; !slices.Equal(got, want) {
		t.Errorf("Narrow(%q) = %q, want %q: the built-in order, each once", named, got, want)
	}
}

func TestAC5NarrowingToNothingIsTheDefaultRun(t *testing.T) {
	none := []string{}
	got, err := Narrow(&none)
	if err != nil || len(got) != 0 {
		t.Errorf("Narrow([]) = %q, %v; want an empty list and no error", got, err)
	}
}

// config.json is writable by a session on the project, so a configured entry
// that is not in the built-in list is refused, exactly: a near miss, another
// case, a trailing space and a wider scope are all different strings.
func TestAC5NarrowingCanNeverWidenTheList(t *testing.T) {
	list := Allowlist()
	for _, widened := range []string{
		"Bash(*)", "Bash", "Write", "Edit", "WebFetch", "Bash(curl:*)", "Bash(git push:*)",
		strings.ToLower(list[0]), list[0] + " ", " " + list[0], list[0] + "x", "",
	} {
		named := []string{list[0], widened}
		got, err := Narrow(&named)
		if err == nil {
			t.Errorf("Narrow(%q) = %q: an entry that is not built in was granted", named, got)
			continue
		}
		if got != nil {
			t.Errorf("Narrow(%q) returned %q with its error: nothing is granted on a refusal", named, got)
		}
		var refusal *sdlcerr.Error
		if !errors.As(err, &refusal) || refusal.Code != sdlcerr.ParallelAllowlistWidened {
			t.Errorf("Narrow(%q): %v, want SDLC-E0066", named, err)
		}
		if widened != "" && !strings.Contains(refusal.What+refusal.Why, widened) {
			t.Errorf("the refusal does not name %q: %s", widened, refusal.Render())
		}
	}
}
