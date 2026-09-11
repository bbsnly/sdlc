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
	"os"

	"github.com/bbsnly/sdlc/internal/release"
)

func main() {
	root := flag.String("C", ".", "repository root")
	tag := flag.String("tag", "", "tag being released, checked against the version")
	out := flag.String("o", "", "write to this file instead of standard output")
	flag.Parse()

	if err := run(flag.Arg(0), *root, *tag, *out); err != nil {
		fmt.Fprintf(os.Stderr, "release: %v\n", err)
		os.Exit(1)
	}
}

func run(command, root, tag, out string) error {
	switch command {
	case "", "check":
		return check(root, tag)
	case "version":
		version, err := release.Version(root)
		if err != nil {
			return err
		}
		return emit(out, version+"\n")
	case "notes":
		version, err := release.Version(root)
		if err != nil {
			return err
		}
		notes, err := release.Notes(root, version)
		if err != nil {
			return err
		}
		return emit(out, notes+"\n")
	default:
		return fmt.Errorf("unknown command %q: expected check, version or notes", command)
	}
}

func check(root, tag string) error {
	problems, err := release.Check(root, tag)
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		version, err := release.Version(root)
		if err != nil {
			return err
		}
		fmt.Printf("release: %s, and every file that carries it agrees\n", version)
		return nil
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	return fmt.Errorf("%d problem(s).\nBump every file listed above to the same version, "+
		"and give it a section in %s", len(problems), release.ChangelogFile)
}

func emit(path, text string) error {
	if path == "" {
		fmt.Print(text)
		return nil
	}
	return os.WriteFile(path, []byte(text), 0o644)
}
