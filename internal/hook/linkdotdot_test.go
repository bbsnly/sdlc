package hook

// US-050, AC-1: a word of a shell command that reaches another checkout's
// loop state through a link and then `..` is refused on every platform. On
// Windows both of the hook's readings of such a word came back name-first,
// while Git Bash, which runs the command, follows the link before the `..`:
// the held session wrote the main checkout's record from its worktree, and
// the hook read the write as one beside the link (CWE-59, improper link
// resolution before file access). A symlink is asked everywhere one can be
// made, and a junction, which needs no privilege, on Windows.
//
// The probe at the end runs the write through shellx.Command, the shell sdlc
// itself runs commands with, and holds the hook to where the file really
// landed, so that CI checks the shell's reading rather than a reading of its
// source.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/shellx"
)

// acLink makes at a link of the kind named to the directory target, or skips
// where that kind cannot be made.
func acLink(t *testing.T, kind, target, at string) {
	t.Helper()
	if kind == "junction" {
		if runtime.GOOS != "windows" {
			t.Skip("junctions exist only on Windows")
		}
		out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", at, target).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J %s %s: %v\n%s", at, target, err, out)
		}
		return
	}
	if err := os.Symlink(target, at); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
}

// acReaches reports whether any of readings, slash-separated as the
// take-away rule is given them, is file, which is on disk.
func acReaches(t *testing.T, readings []string, file string) bool {
	t.Helper()
	want, err := os.Stat(file)
	if err != nil {
		t.Fatalf("%s is not there to compare with: %v", file, err)
	}
	for _, r := range readings {
		if got, err := os.Stat(filepath.FromSlash(r)); err == nil && os.SameFile(got, want) {
			return true
		}
	}
	return false
}

// acLinkedWorktree is wtCheckouts with a link, r, in the worktree, leading to
// the reviews of A-1 in the main checkout, beside A-1's record.
func acLinkedWorktree(t *testing.T, kind string) (main, wt string) {
	t.Helper()
	if kind == "junction" && runtime.GOOS != "windows" {
		t.Skip("junctions exist only on Windows")
	}
	main, wt = wtCheckouts(t)
	reviews := filepath.Join(main, ".sdlc", "stories", "A-1", "reviews")
	if err := os.MkdirAll(reviews, 0o755); err != nil {
		t.Fatal(err)
	}
	acLink(t, kind, reviews, filepath.Join(wt, "r"))
	return main, wt
}

// Read the way the shell reads it, `r/../gate-record.json` from the worktree
// is the main checkout's record of A-1: it lands there for the rule that
// guards another checkout's loop state, the take-away rule is given that
// place, and a write to it is refused. Read by name first, it is a file
// beside r in the worktree, which nothing protects.
func TestAC1AWriteThroughALinkAndThenDotDotIntoAnotherCheckoutsRecordIsRefused(t *testing.T) {
	for _, kind := range []string{"symlink", "junction"} {
		t.Run(kind, func(t *testing.T) {
			main, wt := acLinkedWorktree(t, kind)
			record := ckRecord(main)
			words := []string{
				"r/../gate-record.json",
				filepath.ToSlash(wt) + "/r/../gate-record.json",
				"r/./../gate-record.json",
			}

			l := ckLandings(t, wt)
			set := checkoutSet(placeOf(wt), pathrules.NewResolver(wt))
			for _, word := range words {
				if !ckLands(l, word, ".sdlc/stories/A-1/gate-record.json", false) {
					t.Errorf("%s lands at %+v, want the record in the main checkout", word, l.land(word))
				}
				if got := set.Place(word); !acReaches(t, got, record) {
					t.Errorf("the take-away rule places %s at %q, and none of those is %s", word, got, record)
				}
			}

			for _, command := range []string{
				"echo '{}' > r/../gate-record.json",
				"cp invoice.go r/../gate-record.json",
			} {
				if got := wtAsk(t, wtHeld, wt, "Bash", map[string]any{"command": command}, noEnv); !denied(got) {
					t.Errorf("the held session ran %q from its worktree, writing the main checkout's record through a link and ..", command)
				}
			}
		})
	}
}

// The probe: the shell sdlc runs commands with writes `r/../<name>` from the
// worktree, and where the file appears is the shell's own reading, not ours of
// its source. Spelt relative, it is where r leads, as on Linux and macOS; and
// whatever the spelling, the hook had a reading of the word, before it ran,
// at the file the shell made. On Windows this is Git Bash's reading; the
// spelling with a drive is held only to the second, since Cygwin is said to
// read that one by name first.
func TestAC1TheHookReadsAWordThroughALinkAndThenDotDotWhereTheShellWritesIt(t *testing.T) {
	for _, kind := range []string{"symlink", "junction"} {
		t.Run(kind, func(t *testing.T) {
			main, wt := acLinkedWorktree(t, kind)
			if runtime.GOOS == "windows" {
				acNeedsGitBash(t, wt)
			}
			const name = "us050-probe.txt"
			linkFirst := filepath.Join(main, ".sdlc", "stories", "A-1", name)
			nameFirst := filepath.Join(wt, name)

			for _, tc := range []struct {
				word      string
				linkFirst bool
			}{
				{"r/../" + name, true},
				{filepath.ToSlash(wt) + "/r/../" + name, runtime.GOOS != "windows"},
			} {
				landings := ckLandings(t, wt).land(tc.word)
				readings := checkoutSet(placeOf(wt), pathrules.NewResolver(wt)).Place(tc.word)

				acShell(t, wt, "printf probe > '"+tc.word+"'")
				landed := ""
				for _, at := range []string{linkFirst, nameFirst} {
					if _, err := os.Stat(at); err == nil {
						landed = at
					}
				}
				if landed == "" {
					t.Fatalf("the shell wrote %s, and it is neither at %s nor at %s", tc.word, linkFirst, nameFirst)
				}

				if tc.linkFirst && landed != linkFirst {
					t.Errorf("the shell wrote %s at %s, want %s: it follows the link before the ..", tc.word, landed, linkFirst)
				}
				if !acReaches(t, readings, landed) {
					t.Errorf("the shell wrote %s at %s, and the hook read it only as %q", tc.word, landed, readings)
				}
				if landed == linkFirst && !slices.Contains(landings, shellpolicy.Landing{Rel: ".sdlc/stories/A-1/" + name}) {
					t.Errorf("the shell wrote %s in the main checkout, at %s, and the hook had it landing at %+v", tc.word, landed, landings)
				}
				if err := os.Remove(landed); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// acShell runs command through shellx, from dir, to its end.
func acShell(t *testing.T, dir, command string) string {
	t.Helper()
	cmd, err := shellx.Command(t.Context(), dir, command)
	if err != nil {
		t.Skipf("no POSIX shell to probe: %v", err)
	}
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := shellx.Run(cmd); err != nil {
		t.Fatalf("%s: %v\n%s", command, err, out.String())
	}
	return out.String()
}

// acNeedsGitBash skips unless the shell shellx finds is Git Bash, MSYS2's or
// Cygwin's, whose reading of a link and `..` is the one this probe pins. A
// bash from WSL reads paths in a Linux of its own.
func acNeedsGitBash(t *testing.T, dir string) {
	t.Helper()
	system := strings.TrimSpace(acShell(t, dir, "uname -s"))
	for _, prefix := range []string{"MINGW", "MSYS", "CYGWIN"} {
		if strings.HasPrefix(system, prefix) {
			return
		}
	}
	t.Skipf("the shell shellx found says it is %q, not Git Bash, MSYS2 or Cygwin", system)
}
