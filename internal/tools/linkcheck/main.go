// Command linkcheck enforces two repository rules that no off-the-shelf tool
// checks together, and that this project needs checked on every commit.
//
// Link mode (the default) resolves every relative Markdown link in README.md
// and docs/ against the working tree, and asserts that README.md links into no
// docs/ page. That second rule is the point of the tool. README.md ships inside
// the npm package and every release archive, where docs/ does not exist, so a
// relative link from README.md into docs/ resolves perfectly in the repository
// and is a dead link everywhere the file actually travels. An ordinary link
// checker passes exactly the links this rule forbids.
//
// Hygiene mode asserts that nothing internal is tracked: no planning document,
// no design record, and no path carrying a real person's account name. The
// repository is public from its first commit, so anything committed by mistake
// is public before it can be un-committed, and git log keeps it after it is.
//
// Neither mode touches the network.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// syntheticAccount is the single fake account name every fixture uses. Windows
// path fixtures must carry absolute paths to be meaningful, so the rule is one
// agreed fake name rather than an exemption for whole files: a fixture that
// reaches for a real username fails this check.
const syntheticAccount = "dev"

// markdownLink matches inline links and images: [text](target) and ![alt](target).
var markdownLink = regexp.MustCompile(`!?\[[^\]]*\]\(([^)]+)\)`)

// referenceDef matches link reference definitions: [id]: target
var referenceDef = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]]+\]:\s*(\S+)`)

// homePath matches a user-profile path in any of the three spellings this
// project can produce, capturing the account name.
var homePath = regexp.MustCompile(`(?i)(?:[A-Z]:\\Users\\|/c/Users/|/Users/|/home/)([A-Za-z0-9._-]+)`)

type finding struct {
	file string
	line int
	msg  string
}

func (f finding) String() string {
	if f.line > 0 {
		return fmt.Sprintf("%s:%d: %s", f.file, f.line, f.msg)
	}
	return fmt.Sprintf("%s: %s", f.file, f.msg)
}

func main() {
	mode := flag.String("mode", "links", "which checks to run: links or hygiene")
	root := flag.String("C", ".", "repository root to check")
	flag.Parse()

	var (
		findings []finding
		err      error
		fix      string
	)
	switch *mode {
	case "links":
		findings, err = checkLinks(*root)
		fix = "Fix the link, or move the target into the file that owns it."
	case "hygiene":
		findings, err = checkHygiene(*root)
		fix = "Move the file under plan/ (gitignored), or redact the path to <user>."
	default:
		fmt.Fprintf(os.Stderr, "linkcheck: unknown mode %q\nlinkcheck: expected links or hygiene\n", *mode)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "linkcheck: %v\n", err)
		os.Exit(2)
	}
	if len(findings) > 0 {
		sort.Slice(findings, func(i, j int) bool {
			if findings[i].file != findings[j].file {
				return findings[i].file < findings[j].file
			}
			return findings[i].line < findings[j].line
		})
		for _, f := range findings {
			fmt.Fprintln(os.Stderr, f)
		}
		fmt.Fprintf(os.Stderr, "\nlinkcheck: %d problem(s) in %s mode.\n%s\n", len(findings), *mode, fix)
		os.Exit(1)
	}
}

// checkLinks resolves relative links and enforces the README-into-docs rule.
func checkLinks(root string) ([]finding, error) {
	files, err := markdownFiles(root)
	if err != nil {
		return nil, err
	}
	var findings []finding
	for _, rel := range files {
		abs := filepath.Join(root, rel)
		body, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		for _, lt := range linkTargets(string(body)) {
			target := lt.target
			if isExternal(target) {
				continue
			}
			// Strip any fragment; an anchor is not a file.
			if i := strings.IndexByte(target, '#'); i >= 0 {
				if i == 0 {
					continue // pure in-page anchor
				}
				target = target[:i]
			}
			if target == "" {
				continue
			}
			resolved := filepath.Join(root, filepath.FromSlash(path.Join(path.Dir(rel), target)))
			if _, err := os.Stat(resolved); err != nil {
				findings = append(findings, finding{rel, lt.line,
					fmt.Sprintf("relative link %q does not resolve", lt.target)})
				continue
			}
			if rel == "README.md" && strings.HasPrefix(path.Clean(path.Join(path.Dir(rel), target)), "docs/") {
				findings = append(findings, finding{rel, lt.line,
					fmt.Sprintf("README.md links into docs/ (%q); README.md ships inside the npm package "+
						"and every release archive, where docs/ does not exist, so this is a dead link there. "+
						"Either inline what the reader needs, or link to the canonical URL on the docs site", lt.target)})
			}
		}
	}
	return findings, nil
}

// checkHygiene asserts nothing internal or personally identifying is tracked.
func checkHygiene(root string) ([]finding, error) {
	tracked, err := gitLsFiles(root)
	if err != nil {
		return nil, err
	}
	var findings []finding
	for _, rel := range tracked {
		slash := filepath.ToSlash(rel)
		switch {
		case strings.HasPrefix(slash, "plan/"):
			findings = append(findings, finding{slash, 0, "internal working document is tracked"})
		case strings.HasPrefix(slash, "docs/adr/"), strings.HasPrefix(slash, "docs/parity/"):
			findings = append(findings, finding{slash, 0,
				"design record is tracked; this repository carries only what a user needs (see plan/adr/)"})
		}
		base := strings.ToLower(path.Base(slash))
		if strings.Contains(base, "plan") && strings.HasSuffix(base, ".md") &&
			!strings.HasPrefix(slash, "test/parity/legacy/") {
			findings = append(findings, finding{slash, 0,
				"looks like a planning document; only test/parity/legacy/ may carry one, as a fixture"})
		}
		if compiled(filepath.Join(root, rel)) {
			findings = append(findings, finding{slash, 0,
				"is a compiled binary; `go build ./internal/tools/x` leaves one in the working " +
					"directory and `git add -A` sweeps it in. Delete it and add the name to .gitignore"})
		}
	}

	for _, rel := range tracked {
		slash := filepath.ToSlash(rel)
		if strings.HasPrefix(slash, "test/parity/legacy/") {
			continue // vendored kit, verified once at import
		}
		abs := filepath.Join(root, rel)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() > 4<<20 {
			continue
		}
		f, err := os.Open(abs)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for n := 1; scanner.Scan(); n++ {
			for _, m := range homePath.FindAllStringSubmatch(scanner.Text(), -1) {
				if account := m[1]; account != syntheticAccount {
					findings = append(findings, finding{slash, n,
						fmt.Sprintf("path carries an account name (%q); fixtures use %q and prose writes <user>",
							account, syntheticAccount)})
				}
			}
		}
		_ = f.Close()
	}
	return findings, nil
}

// compiled reports whether a file starts with an executable's magic number.
// This repository ships source and text; a tracked Mach-O, ELF or PE file is
// always a build artefact somebody did not mean to commit.
func compiled(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	var head [4]byte
	if n, err := io.ReadFull(file, head[:]); err != nil || n < 4 {
		return false
	}
	switch {
	case head == [4]byte{0x7f, 'E', 'L', 'F'}: // ELF
		return true
	case head[0] == 'M' && head[1] == 'Z': // PE, and a DOS executable
		return true
	case head == [4]byte{0xfe, 0xed, 0xfa, 0xce}, head == [4]byte{0xfe, 0xed, 0xfa, 0xcf},
		head == [4]byte{0xce, 0xfa, 0xed, 0xfe}, head == [4]byte{0xcf, 0xfa, 0xed, 0xfe},
		head == [4]byte{0xca, 0xfe, 0xba, 0xbe}: // Mach-O, both byte orders, and a fat binary
		return true
	}
	return false
}

type linkTarget struct {
	target string
	line   int
}

func linkTargets(body string) []linkTarget {
	var out []linkTarget
	inFence := false
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range markdownLink.FindAllStringSubmatch(line, -1) {
			out = append(out, linkTarget{strings.Fields(m[1])[0], i + 1})
		}
		for _, m := range referenceDef.FindAllStringSubmatch(line, -1) {
			out = append(out, linkTarget{m[1], i + 1})
		}
	}
	return out
}

func isExternal(target string) bool {
	lower := strings.ToLower(target)
	for _, p := range []string{"http://", "https://", "mailto:", "tel:", "ftp://", "//"} {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// skipped is what a link check has no business walking into. The legacy kit is
// a byte-for-byte fixture for the differential tests, the scaffold templates
// are written into somebody else's repository where their links resolve, and
// the rest is not ours.
var skipped = map[string]bool{
	".git":                        true,
	"node_modules":                true,
	"dist":                        true,
	"plan":                        true,
	"test/parity/legacy":          true,
	"internal/scaffold/templates": true,
}

// markdownFiles is every Markdown file in the tree, not only README.md and
// docs/. CONTRIBUTING.md, SECURITY.md, the skill and the agents all carry
// links, and until this walked the whole tree a broken one in any of them was
// nobody's job to notice.
func markdownFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			// A tree that is being written while this walks is not a broken
			// link, and this tool runs from the first commit onwards.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (skipped[rel] || strings.HasPrefix(d.Name(), ".") && rel != ".github") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func gitLsFiles(root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed (is %s a git repository?): %w", root, err)
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}
