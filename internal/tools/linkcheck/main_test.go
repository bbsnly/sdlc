package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitRepo makes a throwaway repository with the given files staged, so the
// hygiene checks have a real `git ls-files` to read.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		write(t, root, rel, body)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"add", "-A"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

func TestLinksResolve(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "See [the site](https://example.com) and [nothing](./missing.md).\n")
	write(t, root, "docs/a.md", "[b](b.md)\n")
	write(t, root, "docs/b.md", "ok\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].msg, "does not resolve") {
		t.Errorf("unexpected finding: %v", got[0])
	}
}

func TestReadmeMayNotLinkIntoDocs(t *testing.T) {
	root := t.TempDir()
	// The link resolves perfectly in the repository. That is exactly why an
	// ordinary link checker cannot catch it.
	write(t, root, "README.md", "Full guide: [install](docs/install.md)\n")
	write(t, root, "docs/install.md", "ok\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].msg, "ships inside the npm package") {
		t.Errorf("finding should explain why: %v", got[0])
	}
}

func TestDocsMayLinkIntoDocs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/a.md", "[install](install.md)\n")
	write(t, root, "docs/install.md", "ok\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("docs may link to docs, got: %v", got)
	}
}

func TestFencedCodeIsNotScanned(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "```\n[not a link](./nope.md)\n```\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("fenced blocks are examples, not links: %v", got)
	}
}

func TestAnchorsAndExternalsAreSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "[a](#section) [b](https://x.test) [c](mailto:a@b.test)\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want none, got: %v", got)
	}
}

func TestHygieneRejectsTrackedInternalDocs(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"plan/checklist.md":          "internal\n",
		"docs/adr/ADR-0001.md":       "why we did it\n",
		"docs/parity/legacy-spec.md": "the old kit\n",
		"README.md":                  "fine\n",
	})
	got, err := checkHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 {
		t.Fatalf("want at least 3 findings, got %d: %v", len(got), got)
	}
}

func TestHygieneRejectsRealAccountNames(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"docs/windows.md": "PATH included C:\\Users\\alice\\AppData\\Local\\sdlc\n",
	})
	got, err := checkHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].msg, "alice") {
		t.Errorf("finding should name the account it found: %v", got[0])
	}
}

func TestHygieneAllowsTheSyntheticAccount(t *testing.T) {
	// Windows path fixtures must carry absolute paths to be meaningful. The
	// rule is one agreed fake name, not an exemption for whole files.
	root := gitRepo(t, map[string]string{
		"internal/shellpolicy/fixtures_test.go": "const p = `C:\\Users\\dev\\AppData\\Local\\sdlc\\bin\\sdlc.exe`\n",
	})
	got, err := checkHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("the synthetic account is allowed, got: %v", got)
	}
}

func TestHygieneAllowsVendoredLegacyKit(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"test/parity/legacy/PLAN.md": "the legacy kit's own template, executed as a fixture\n",
	})
	got, err := checkHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("the vendored kit is a fixture, got: %v", got)
	}
}
