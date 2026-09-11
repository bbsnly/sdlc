// Command release reports and checks this repository's release metadata.
//
//	release check          every file that carries the version agrees, and the
//	                       changelog has a non-empty section for it
//	release check -tag v1  the same, and the tag being released matches
//	release version        print the version, for a workflow to read
//	release notes          print the changelog section for that version
//
// `task check` runs `release check` on every commit, so a version bump that
// misses a file fails then, rather than halfway through a release.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/bbsnly/sdlc/internal/release"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "release: %v\n", err)
		os.Exit(1)
	}
}

// run parses its own arguments so that flags work on either side of the
// subcommand. The plain flag package stops parsing at the first non-flag
// argument, so `release check -tag v1` -- the form this tool documents, and the
// form the release workflow uses -- left -tag unset and checked nothing at all.
// Parsing twice around the subcommand is what makes the documented spelling
// mean what it says.
//
// A leftover argument is an error rather than something to ignore, because a
// mistyped flag that is silently dropped is how a check ends up checking
// nothing while reporting success.
func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("C", ".", "repository root")
	tag := fs.String("tag", "", "tag being released, checked against the version")
	out := fs.String("o", "", "write to this file instead of standard output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := ""
	if fs.NArg() > 0 {
		command = fs.Arg(0)
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return dispatch(command, *root, *tag, *out, stdout)
}

func dispatch(command, root, tag, out string, stdout io.Writer) error {
	switch command {
	case "", "check":
		return check(root, tag, stdout)
	case "version":
		version, err := release.Version(root)
		if err != nil {
			return err
		}
		return emit(out, version+"\n", stdout)
	case "notes":
		version, err := release.Version(root)
		if err != nil {
			return err
		}
		notes, err := release.Notes(root, version)
		if err != nil {
			return err
		}
		return emit(out, notes+"\n", stdout)
	default:
		return fmt.Errorf("unknown command %q: expected check, version or notes", command)
	}
}

func check(root, tag string, stdout io.Writer) error {
	problems, err := release.Check(root, tag)
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		version, err := release.Version(root)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "release: %s, and every file that carries it agrees\n", version)
		return nil
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	return fmt.Errorf("%d problem(s).\nBump every file listed above to the same version, "+
		"and give it a section in %s", len(problems), release.ChangelogFile)
}

func emit(path, text string, stdout io.Writer) error {
	if path == "" {
		_, err := io.WriteString(stdout, text)
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}
