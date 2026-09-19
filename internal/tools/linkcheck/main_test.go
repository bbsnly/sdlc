package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// finding.String() is what a person reading stderr actually sees; a hygiene
// finding carries no line, and its message must not claim one it doesn't have.
func TestFindingStringOmitsTheLineWhenThereIsNone(t *testing.T) {
	if got := (finding{"README.md", 3, "boom"}).String(); got != "README.md:3: boom" {
		t.Errorf("String() = %q, want a line number", got)
	}
	if got := (finding{"README.md", 0, "boom"}).String(); got != "README.md: boom" {
		t.Errorf("String() = %q, want no line number", got)
	}
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

// A reference-style definition ("[id]: target") is a link like any other, not
// only the inline "[text](target)" form the other fixtures use.
func TestReferenceStyleLinksResolve(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "See [the docs][ref].\n\n[ref]: ./missing.md\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].msg, "does not resolve") {
		t.Fatalf("want the reference target reported missing, got %v", got)
	}
}

// A fragment after the file part must be stripped before the file is
// resolved, not just when the whole target is a bare "#anchor".
func TestALinkWithAFragmentResolvesTheFilePart(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/a.md", "[b](b.md#section)\n")
	write(t, root, "docs/b.md", "## section\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a fragment on a file that exists should resolve, got %v", got)
	}
}

// The walk tolerates a root that is not there yet -- this tool runs from the
// first commit onwards, before anything has been written.
func TestALinkCheckOfARootThatDoesNotExistYetFindsNothing(t *testing.T) {
	got, err := checkLinks(filepath.Join(t.TempDir(), "not-created-yet"))
	if err != nil {
		t.Fatalf("a tree that is not there yet is not a broken link, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// A directory that cannot even be listed is a real failure to report, unlike
// one that simply is not there.
func TestALinkCheckFailsWhenADirectoryCannotBeListed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not restrict directory listing on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) }) // let TempDir clean up

	if _, err := checkLinks(root); err == nil {
		t.Fatal("want an error when a directory cannot be listed")
	}
}

// A package's README links to files the package does not ship. The skip was
// keyed by path, so it held for node_modules at the root and not for the one
// `./task markdownlint` installs under .github/tools.
func TestInstalledPackagesAreNotChecked(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "ok\n")
	write(t, root, "node_modules/a/README.md", "[x](./missing.md)\n")
	write(t, root, ".github/tools/markdownlint/node_modules/b/README.md", "[x](./missing.md)\n")

	got, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("installed packages were link-checked: %v", got)
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

// markdownFiles finds a name; checkLinks then has to read it. A broken
// symlink is found and named just like any other file, and reading it fails
// for a reason that has nothing to do with the links inside it.
func TestLinksFailsWhenAMarkdownFileCannotBeRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink needs elevated privileges on Windows")
	}
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "broken.md")); err != nil {
		t.Fatal(err)
	}

	if _, err := checkLinks(root); err == nil {
		t.Fatal("want an error reading a file that does not exist")
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

// A planning document is one wherever it is tracked, not only under plan/.
func TestHygieneRejectsAPlanningDocumentAnywhere(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"test/fixtures/PLAN.md":       "a plan beside the tests\n",
		"docs/release-plan.md":        "what ships when\n",
		"internal/scaffold/README.md": "fine\n",
	})
	got, err := checkHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 findings, got %d: %v", len(got), got)
	}
}

// This exact mistake reached trunk once: `go build ./internal/tools/release`
// leaves an executable called `release` in the working directory, and
// `git add -A` committed 3.5 MB of it. The check that would have caught it
// keys on what the file is, not what it is called.
func TestHygieneRejectsACompiledBinary(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"release":      "\x7fELF\x02\x01\x01 and whatever follows\n",
		"tool.exe":     "MZ\x90\x00 a windows executable\n",
		"mach":         "\xcf\xfa\xed\xfe a mach-o\n",
		"task":         "#!/bin/sh\necho a shell script is not a binary\n",
		"docs/page.md": "# a document\n",
	})
	got, err := checkHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 findings, got %d: %v", len(got), got)
	}
	for _, f := range got {
		if !strings.Contains(f.msg, "compiled binary") {
			t.Errorf("unexpected finding: %v", f)
		}
	}
}

// compiled() reads the first 4 bytes; a tracked file shorter than that must
// not be mistaken for one of the binary formats it opens with a magic number.
func TestHygieneDoesNotMistakeAShortFileForABinary(t *testing.T) {
	root := gitRepo(t, map[string]string{"short": "hi"})
	got, err := checkHygiene(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a 2-byte file was reported: %v", got)
	}
}

// A file git still lists but the working tree no longer has must be skipped,
// not crash the check that exists to catch mistakes in exactly this tree.
func TestHygieneSkipsATrackedFileThatIsNoLongerOnDisk(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"gone.md":   "tracked, then deleted\n",
		"README.md": "fine\n",
	})
	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatal(err)
	}

	findings, err := checkHygiene(root)
	if err != nil {
		t.Fatalf("a file git ls-files names but the disk no longer has should be skipped, not fail: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("a deleted file was reported: %v", findings)
	}
}

// A tracked file this process cannot read must be skipped by the account-name
// scan, not fail the whole check over one file.
func TestHygieneSkipsATrackedFileItCannotOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not remove read permission on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	root := gitRepo(t, map[string]string{
		"locked.md": "C:\\Users\\alice\\file\n",
		"README.md": "fine\n",
	})
	if err := os.Chmod(filepath.Join(root, "locked.md"), 0o000); err != nil {
		t.Fatal(err)
	}

	got, err := checkHygiene(root)
	if err != nil {
		t.Fatalf("a file this process cannot read should be skipped, not fail the check: %v", err)
	}
	for _, f := range got {
		if strings.Contains(f.msg, "alice") {
			t.Errorf("a file that could not be opened should not have been scanned: %v", f)
		}
	}
}
