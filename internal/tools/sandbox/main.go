// Command sandbox makes a throwaway directory to try a change to sdlc in, and
// removes one it made.
//
//	sandbox [-C <checkout>] create         make one, and print where it is
//	sandbox [-C <checkout>] remove <dir>   remove one that create made
//
// `./task sandbox` and `./task sandbox-remove -- <dir>` run it. A sandbox is a
// new directory under the temp directory holding a git repository with sdlc
// initialised and one ready story (repo), the sdlc built from the checkout
// (bin), and a home and a temp directory for what the two write (home, tmp).
// It confines nothing: the word is not the one docs/enforcement.md uses when it
// says sdlc is not a sandbox.
//
// It has those two words and no other. The steps the loop keeps for a person
// are no part of it, and it is not built into the released binary.
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
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
)

const (
	// prefix begins the name of every sandbox, and marker is the file create
	// writes in one. Remove takes a directory that has both and is still
	// directly under the temp directory. That guards against a slip, not
	// against somebody who sets out to make a directory look like a sandbox.
	prefix = "sdlc-sandbox-"
	marker = ".sdlc-sandbox"
	mark   = "./task sandbox made this directory in an sdlc checkout. ./task sandbox-remove -- <this directory> removes it.\n"

	module = "github.com/bbsnly/sdlc"

	// usage names the tasks, because they are what a contributor types, and the
	// two words for whoever runs the program without them.
	usage = "sandbox: run ./task sandbox to create a sandbox, or ./task sandbox-remove -- <dir> to remove one, " +
		"where <dir> is the directory on the \"sandbox\" line that ./task sandbox printed " +
		"(without the tasks: sandbox [-C <checkout>] create | remove <dir>)"

	// whichDirectory ends a refusal to remove: where a directory that can be
	// removed comes from.
	whichDirectory = "Run ./task sandbox-remove -- <dir> with the directory from the \"sandbox\" line that ./task sandbox printed"
)

// passed is every variable of the caller's that reaches a child: where go and
// git are, where go's caches are, and what Windows needs to start a process at
// all. Nothing else crosses, so a session's own SDLC_, CLAUDE_ and GIT_
// variables and its GOFLAGS cannot move the sandbox or change what is built.
var passed = []string{"PATH", "GOCACHE", "GOMODCACHE", "GOPATH", "SystemRoot", "SystemDrive", "ComSpec", "PATHEXT", "WINDIR"}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run reads -C and then one of the two words. The flag package stops at the
// first word that is not a flag, so nothing after create or remove is read as
// one: a directory that begins with a dash is a directory, or too many words.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("sandbox", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	checkout := flags.String("C", ".", "")
	err := flags.Parse(args)
	words := flags.Args()
	switch {
	case err == nil && len(words) == 1 && words[0] == "create":
		// An interrupt ends the child that is running, and the directory that
		// was begun goes the way it does after any other failure.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		err = create(ctx, *checkout, stdout)
	case err == nil && len(words) == 2 && words[0] == "remove":
		err = remove(*checkout, words[1])
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "sandbox: %v.\n", err)
		return 1
	}
	return 0
}

// resolved is each path made absolute with its links followed, which is the
// one spelling two paths can be compared in. A path with nothing at it is an
// error.
func resolved(paths ...string) ([]string, error) {
	out := make([]string, len(paths))
	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err == nil {
			abs, err = filepath.EvalSymlinks(abs)
		}
		if err != nil {
			return nil, err
		}
		out[i] = abs
	}
	return out, nil
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// step is one thing create does, with what to say when it fails: what was being
// done, and what a person can do about it.
type step struct {
	doing string
	run   func() error
	next  string
}

// sandboxPlaces is the checkout at from and the temp directory, resolved, once
// they are fit for a sandbox: the first is the root of an sdlc checkout and the
// second is outside it.
func sandboxPlaces(from string) (checkout, temp string, err error) {
	places, err := resolved(from, os.TempDir())
	if err != nil {
		return "", "", fmt.Errorf("no sandbox was made: the checkout %q or the temp directory %q is not there (%w).\n"+
			"Run ./task sandbox at the root of an sdlc checkout, with TMPDIR (TMP on Windows) naming a directory that exists",
			from, os.TempDir(), err)
	}
	checkout, temp = places[0], places[1]
	// The whole line, not a part of it: a module whose path only begins with
	// this one is somebody else's code, and it would be built and run.
	gomod, err := os.ReadFile(filepath.Join(checkout, "go.mod"))
	if err != nil || !slices.Contains(strings.Split(strings.ReplaceAll(string(gomod), "\r\n", "\n"), "\n"), "module "+module) {
		return "", "", fmt.Errorf("no sandbox was made: %s is not the root of an sdlc checkout.\nRun ./task sandbox at the root of one", checkout)
	}
	// A sandbox goes outside the checkout: made in it, it would be the
	// checkout's untracked files, and removed with whatever cleans those.
	if within(temp, checkout) {
		return "", "", fmt.Errorf("no sandbox was made: the temp directory %s is not outside the checkout %s, and a sandbox goes outside it.\n"+
			"Point TMPDIR (TMP on Windows) at a directory outside the checkout, then run ./task sandbox again", temp, checkout)
	}
	return checkout, temp, nil
}

// sandboxEnv is the whole environment of what a sandbox runs: its own home and
// temp directories, and from the caller's only what passed names.
func sandboxEnv(home, tmp string) []string {
	env := []string{
		"HOME=" + home, "USERPROFILE=" + home,
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"), "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"), "APPDATA=" + filepath.Join(home, "AppData", "Roaming"),
		"TMPDIR=" + tmp, "TMP=" + tmp, "TEMP=" + tmp,
		"GOFLAGS=", "GOWORK=off", "GOPROXY=off",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		"GIT_AUTHOR_NAME=Sandbox", "GIT_AUTHOR_EMAIL=sandbox@example.com",
		"GIT_COMMITTER_NAME=Sandbox", "GIT_COMMITTER_EMAIL=sandbox@example.com",
	}
	for _, name := range passed {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// create makes a sandbox with the sdlc built from the checkout at from, and
// prints where it is. It ends with a whole sandbox or with nothing.
func create(ctx context.Context, from string, stdout io.Writer) (err error) {
	checkout, temp, err := sandboxPlaces(from)
	if err != nil {
		return err
	}
	// MkdirTemp makes it for this user alone, and it stays that way: on a
	// shared temp directory that is what keeps another user's files out of it.
	dir, err := os.MkdirTemp(temp, prefix)
	if err != nil {
		return fmt.Errorf("no sandbox was made: %w.\n"+
			"Point TMPDIR (TMP on Windows) at a directory you can write to, then run ./task sandbox again", err)
	}
	defer func() {
		if err != nil {
			// The only delete create makes, and only of the path MkdirTemp
			// returned a moment ago, which nothing but this process has named:
			// never a path from an argument or one put together again from
			// what was printed. A failure to delete names what is left.
			err = errors.Join(fmt.Errorf("%w\nNo sandbox was made, and nothing is left at %s unless a line after this one names what could not be deleted",
				err, dir), os.RemoveAll(dir))
		}
	}()

	repo, sdlc := filepath.Join(dir, "repo"), filepath.Join(dir, "bin", "sdlc"+exeSuffix)
	home, tmp := filepath.Join(dir, "home"), filepath.Join(dir, "tmp")
	// Built from nothing. Whatever go, git and sdlc keep under a home, a cache
	// or a temp directory goes inside the sandbox: any go command writes
	// telemetry under the home it is given. Go's flags are emptied and its
	// proxy is off, so what is built is what is in the checkout and a module
	// that is missing is an error, not a download. Git reads no configuration
	// but the repository's own and does not refresh the checkout's index when
	// go asks it for the revision to stamp.
	env := sandboxEnv(home, tmp)
	// A fixed list of words and no shell: nothing here is a string a project
	// configured, which is what internal/shellx is for.
	child := func(in, name string, args ...string) func() error {
		return func() error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir, cmd.Env = in, env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s %s: %s", filepath.Base(name), strings.Join(args, " "), strings.TrimSpace(err.Error()+"\n"+string(out)))
			}
			return nil
		}
	}
	const (
		needsGit  = "Unless you interrupted it, check that git is on PATH and runs (git --version), then run ./task sandbox again."
		needsRoom = "Check that the temp directory can be written to and has room, then run ./task sandbox again."
	)
	for _, s := range []step{
		// The marker first, so that a sandbox left half-made by a process that
		// was killed is one remove still takes.
		{"mark the directory as a sandbox", func() error { return os.WriteFile(filepath.Join(dir, marker), []byte(mark), 0o600) }, needsRoom},
		{"make the sandbox's home", func() error { return os.MkdirAll(home, 0o700) }, needsRoom},
		{"make the sandbox's temp directory", func() error { return os.MkdirAll(tmp, 0o700) }, needsRoom},
		{"build sdlc from " + checkout, child(checkout, "go", "build", "-o", sdlc, "./cmd/sdlc"),
			"Unless you interrupted it, check that go is on PATH. The build uses no network: if go could not find a module, run ./task check once, " +
				"which fills the module cache, then run ./task sandbox again."},
		// On the branch a new project's configuration names as trunk, which is
		// where `sdlc init` then finds HEAD.
		{"make the sandbox's repository", child(dir, "git", "init", "--quiet", "--initial-branch="+config.Default().Git.TrunkBranch, repo), needsGit},
		{"set protocol.allow to never in the sandbox's repository", child(repo, "git", "config", "--local", "protocol.allow", "never"), needsGit},
		{"initialise sdlc in the sandbox's repository", child(repo, sdlc, "init"),
			"That was the sdlc built from " + checkout + ": run ./task check there to see what is wrong with it."},
		{"stage what sdlc init wrote", child(repo, "git", "add", "--all"), needsGit},
		{"commit what sdlc init wrote", child(repo, "git", "commit", "--quiet", "--message", "chore: a sandbox, as sdlc init leaves a new repository"), needsGit},
	} {
		if err = s.run(); err != nil {
			// The cause says so when what stopped it was an interrupt.
			return fmt.Errorf("could not %s: %w\n%s", s.doing, errors.Join(err, context.Cause(ctx)), s.next)
		}
	}
	fmt.Fprintf(stdout, "sandbox     %s\nrepository  %s\nsdlc        %s\n", dir, repo, sdlc)
	return nil
}

// refuseHeld refuses a target that holds the working directory or the checkout.
func refuseHeld(arg, target, cwd, checkout string) error {
	// Windows will not delete the directory a process stands in, and elsewhere
	// a shell left standing in a deleted directory fails in ways nobody
	// connects to this.
	for _, held := range []struct{ what, path, next string }{
		{"your working directory", cwd, "Change to a directory outside it"},
		{"the checkout this was run from", checkout, "Move the checkout out of it, or run this from another checkout"},
	} {
		if within(held.path, target) {
			return fmt.Errorf("%q was not removed: %s is inside it.\n%s, then run ./task sandbox-remove -- %s again",
				arg, held.what, held.next, target)
		}
	}
	return nil
}

// remove takes away a sandbox create made, and nothing else.
//
// Whoever names the directory may be a model with nobody watching, so every
// refusal is decided before anything is deleted, and in this order: the path
// resolves; it is where create puts a sandbox and named as one; neither the
// working directory nor the checkout is inside it; and what was opened there
// is that directory and holds the marker.
func remove(from, arg string) error {
	places, err := resolved(os.TempDir(), from, ".")
	if err != nil {
		return fmt.Errorf("nothing was removed: the temp directory %q, the checkout %q or the working directory is not there (%w).\n"+
			"A sandbox is removed from the temp directory it was made in: run ./task sandbox-remove with TMPDIR (TMP on Windows) as ./task sandbox had it",
			os.TempDir(), from, err)
	}
	temp, checkout, cwd := places[0], places[1], places[2]
	// No path at all would resolve to the working directory.
	places, err = resolved(arg)
	if arg == "" || err != nil {
		return fmt.Errorf("%w\n%s", errors.Join(
			fmt.Errorf("there is nothing to remove at %q: it is already gone, or ./task sandbox never made it", arg), err),
			"If you removed it already, there is nothing left to do. To remove another, give ./task sandbox-remove -- <dir> "+
				"the directory from the \"sandbox\" line that ./task sandbox printed")
	}
	target := places[0]
	if filepath.Dir(target) != temp || !strings.HasPrefix(filepath.Base(target), prefix) {
		return fmt.Errorf("%q was not removed: it is not a sandbox where ./task sandbox made it, which is a directory named %s* directly under the temp directory %s.\n%s",
			arg, prefix, temp, whichDirectory)
	}
	if err := refuseHeld(arg, target, cwd, checkout); err != nil {
		return err
	}
	// Opened once, and everything after this is asked of what was opened, so a
	// directory swapped in afterwards is neither judged nor deleted from.
	root, err := os.OpenRoot(target)
	if err != nil {
		return fmt.Errorf("%q was not removed: it cannot be opened as a directory (%w).\n%s", arg, err, whichDirectory)
	}
	// The marker is looked at and never opened or read: a link in its place is
	// not a regular file, so there is nothing to follow.
	here, errHere := os.Lstat(target)
	opened, errOpened := root.Stat(".")
	made, errMade := root.Lstat(marker)
	if err = errors.Join(errHere, errOpened, errMade); err != nil || !os.SameFile(here, opened) || !made.Mode().IsRegular() {
		return fmt.Errorf("%w\n%s", errors.Join(
			fmt.Errorf("%q was not removed: it does not hold the plain file %s that ./task sandbox writes in every sandbox it makes, "+
				"or it was replaced while this ran. Nothing in it was touched", arg, marker), err, root.Close()), whichDirectory)
	}
	// Only now is anything deleted, and only by name through what was opened,
	// never by the path that was given. The marker goes after everything else
	// in the directory: while it is there the directory is still a sandbox, and
	// the same remove finishes once what stopped it is put right.
	entries, err := fs.ReadDir(root.FS(), ".")
	for _, e := range entries {
		if e.Name() != marker {
			err = errors.Join(err, root.RemoveAll(e.Name()))
		}
	}
	if err == nil {
		err = root.Remove(marker)
	}
	if err != nil {
		return fmt.Errorf("%q was only partly removed: what could be deleted is gone, and this could not be:\n%w\n"+
			"Put right what those lines name, then run ./task sandbox-remove -- %s again, which finishes it", arg, errors.Join(err, root.Close()), target)
	}
	// What is left is an empty directory, and os.Remove deletes nothing else.
	// With the marker gone a second remove would refuse it, so a failure here
	// does not say to run this again: a read-only temp directory left an empty
	// sdlc-sandbox-* behind that the message sent the person back to in vain.
	if err = errors.Join(root.Close(), os.Remove(target)); err != nil {
		return fmt.Errorf("everything in %q was removed, and the directory itself, now empty, could not be:\n%w\n"+
			"Nothing in it marks it as a sandbox any more, so ./task sandbox-remove will not take it again: "+
			"delete the empty directory %s yourself", arg, err, target)
	}
	return nil
}
