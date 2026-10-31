// Command migrationguide writes the skeleton of a migration guide for a release.
//
//	migrationguide <from-tag> <to-tag>
//
// It lists every commit in from..to that is marked breaking and writes
// docs/migrations/<from>-to-<to>.md with a section for each, for a person to
// complete. It never overwrites a file: a guide somebody has started is theirs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/bbsnly/sdlc/internal/gitx"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "migration-guide: %v\n", err)
		os.Exit(1)
	}
}

const usage = "usage: ./task migration-guide -- <from-tag> <to-tag>"

// commit is one breaking commit: the whole hash and the subject line.
type commit struct{ hash, subject string }

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("migration-guide", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("C", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("%s", usage)
	}
	from, to := flags.Arg(0), flags.Arg(1)
	for _, word := range []string{from, to} {
		if err := checkTag(*root, word); err != nil {
			return err
		}
	}
	commits, err := breaking(*root, from, to)
	if err != nil {
		return err
	}
	rel := filepath.Join("docs", "migrations", from+"-to-"+to+".md")
	if err := create(*root, rel, render(from, to, commits)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "wrote %s\n", rel)
	return err
}

// gitOutput runs git in root with a fixed argument list and no shell. The
// environment is gitx's, so a GIT_DIR left by a hook cannot point it at
// another repository.
func gitOutput(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	cmd.Env = gitx.Environ()
	out, err := cmd.Output()
	return string(out), err
}

// checkTag accepts only a tag. show-ref --verify takes the whole ref, so a
// revision such as v1.1.0~1, a branch, HEAD or a hash is not one, and the word
// is only the tail of a ref name, never an option.
func checkTag(root, word string) error {
	if _, err := gitOutput(root, "show-ref", "--verify", "--quiet", "refs/tags/"+word); err != nil {
		return fmt.Errorf("%q is not a git tag in this repository; list tags with \"git tag\", "+
			"then run ./task migration-guide -- <from> <to>", word)
	}
	if strings.Contains(word, "/") {
		return fmt.Errorf("%q is a tag, but its name cannot go in a file name; "+
			"tag the release without a \"/\" or write the guide by hand", word)
	}
	return nil
}

var (
	bangSubject = regexp.MustCompile(`^\w+(\([^)]*\))?!:`)
	footer      = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE:`)
)

// breaking lists the breaking commits in from..to, oldest first so the guide
// is the same each run. A mention of the words in the middle of a sentence is
// not a footer, which is why the footer is anchored at the start of a line.
func breaking(root, from, to string) ([]commit, error) {
	out, err := gitOutput(root, "log", "-z", "--reverse", "--no-show-signature", "--format=%H%n%B",
		"refs/tags/"+from+"..refs/tags/"+to, "--")
	if err != nil {
		return nil, fmt.Errorf("git log %s..%s failed: %w", from, to, err)
	}
	var found []commit
	for record := range strings.SplitSeq(out, "\x00") {
		hash, message, _ := strings.Cut(strings.TrimSpace(record), "\n")
		subject, _, _ := strings.Cut(message, "\n")
		if bangSubject.MatchString(subject) || footer.MatchString(message) {
			found = append(found, commit{hash, subject})
		}
	}
	return found, nil
}

func render(from, to string, commits []commit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Migrating from %s to %s\n\n", codeSpan(from), codeSpan(to))
	if len(commits) == 0 {
		b.WriteString("No commit in this range is marked breaking.\n")
	}
	for i, c := range commits {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## %s (%s)\n\nCommit %s.\n", codeSpan(c.subject), codeSpan(c.hash[:7]), codeSpan(c.hash))
		for _, h := range []string{"What changed", "Steps", "Verify"} {
			fmt.Fprintf(&b, "\n### %s\n\nTo complete.\n", h)
		}
	}
	return b.String()
}

// codeSpan puts text from git in a code span, so no subject is read as
// markdown or HTML. A control character such as a carriage return becomes a
// space, since it would break the line. The fence is longer than any backtick
// run in the text, and CommonMark strips one space either side, which is
// what lets text that starts or ends with a backtick sit inside it.
func codeSpan(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	fence := "`"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		text = " " + text + " "
	}
	return fence + text + fence
}

// create is the last thing done, so a refused tag leaves nothing behind, and
// O_EXCL means a guide that is there is never truncated.
func create(root, rel, text string) error {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: a committed document for everyone who clones the repository
		return fmt.Errorf("cannot create the directory for %s: %w", rel, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // G302: a committed document for everyone who clones the repository
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists; move or delete it and run again", rel)
	}
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", rel, err)
	}
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", rel, err)
	}
	return nil
}
