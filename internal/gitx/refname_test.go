package gitx

import (
	"errors"
	"math/rand/v2"
	"os/exec"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// refnameCases are names chosen at each edge of check-ref-format's rules,
// read as the part after refs/heads/.
var refnameCases = []string{
	"main", "master", "story/US-001", "sdlc/US-001", "a/b/c", "UPPER", "with-dash", "with_underscore",
	"dots.in.it", "trailing-", "ünïcödé", "\xc3", "\xff\xfe", "a\x80b",
	"", "/", "a/", "/a", "a//b", "a/b/",
	".", "..", ".a", "a/.b", "a.", "a/b.", "a..b", "a/..", "a/.", "a.b.",
	"a.lock", "a/b.lock", "a.lock/b", ".lock", "lock", "a.lockb", "a.LOCK", "a.loc",
	"@", "@/a", "a/@", "a@b", "@{", "a@{b", "a@{-1}", "@{-1}", "a{b", "a@", "a}b", "{@",
	"HEAD", "head", "-a", "a-", "-",
	"a b", "a\tb", "a\x01b", "a\x1fb", "a\x7fb", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a]b", "a\\b",
	"a!b", `a"b`, "a#b", "a$b", "a%b", "a&b", "a'b", "a(b", "a+b", "a,b", "a;b", "a<b", "a=b", "a>b", "a|b",
	"a`b", "~", "^", ":", "*", "[", "\\", "refs/heads/x",
}

// refnameRandom is names drawn from the characters the rules turn on, with a
// fixed seed, for the combinations nobody thought to write down.
func refnameRandom(n int) []string {
	r := rand.New(rand.NewPCG(1, 2))
	parts := []string{"a", "b", ".", "/", "@", "{", "}", "-", ".lock", "~", " ", "\x01", "\xc3", "*", "HEAD"}
	names := make([]string, 0, n)
	for range n {
		var b strings.Builder
		for range 1 + r.IntN(5) {
			b.WriteString(parts[r.IntN(len(parts))])
		}
		names = append(names, b.String())
	}
	return names
}

// gitTakes is what the git on PATH says of args, run in dir, which is no
// repository: true for exit status 0, false for any other.
func gitTakes(t *testing.T, dir string, args ...string) bool {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"check-ref-format"}, args...)...)
	cmd.Dir = dir
	cmd.Env = Environ("GIT_CEILING_DIRECTORIES=" + dir)
	err := cmd.Run()
	if _, exited := errors.AsType[*exec.ExitError](err); err != nil && !exited {
		t.Fatalf("git check-ref-format %q could not be run: %v", args, err)
	}
	return err == nil
}

func TestTheRefNameRuleAgreesWithGitsCheckRefFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range append(refnameCases, refnameRandom(150)...) {
		full := headsPrefix + name
		if got, want := refnameRule(full) == "", gitTakes(t, dir, full); got != want {
			t.Errorf("refnameRule(%q) takes it: %v; git check-ref-format: %v (rule %q)", full, got, want, refnameRule(full))
		}
		// Asked with no refs/heads/ in front, the one-component rule and "@"
		// alone are reached too.
		if got, want := refnameRule(name) == "", name != "" && gitTakes(t, dir, name); got != want {
			t.Errorf("refnameRule(%q) takes it: %v; git check-ref-format: %v (rule %q)", name, got, want, refnameRule(name))
		}
	}
}

func TestTheBranchRuleAgreesWithGitsCheckRefFormatBranchWhereItAnswers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range append(refnameCases, refnameRandom(150)...) {
		rule, ok := branchRule(name)
		if !ok {
			if !strings.Contains(name, "@") {
				t.Errorf("branchRule(%q) left to git a name with no @ in it", name)
			}
			continue
		}
		// git is never handed an empty argument here: AddWorktree refuses one
		// before, and some versions of git read none as no argument at all.
		if name == "" {
			if rule == "" {
				t.Error(`branchRule("") takes it`)
			}
			continue
		}
		if got, want := rule == "", gitTakes(t, dir, "--branch", name); got != want {
			t.Errorf("branchRule(%q) takes it: %v; git check-ref-format --branch: %v (rule %q)", name, got, want, rule)
		}
	}
}

// A NUL cannot be handed to git on its command line, so git was never able to
// take a name that holds one; the rule refuses it as well.
func TestTheRefNameRuleRefusesANul(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"refs/heads/a\x00b", "refs/heads/\x00", "refs/heads/a\x00"} {
		if refnameRule(name) == "" {
			t.Errorf("refnameRule(%q) takes it", name)
		}
	}
	if rule, ok := branchRule("a\x00b"); !ok || rule == "" {
		t.Errorf("branchRule takes a name with a NUL in it: %q, %v", rule, ok)
	}
}

func TestBranchTipRefusesANameGitDoesNotTakeWithTheRuleItBreaks(t *testing.T) {
	t.Parallel()
	_, err := BranchTip(t.Context(), t.TempDir(), "a..b")
	refusal, ok := errors.AsType[*sdlcerr.Error](err)
	if !ok || !strings.Contains(refusal.Why, `"refs/heads/a..b" holds ".."`) {
		t.Fatalf("BranchTip of a..b: %#v", err)
	}
}
