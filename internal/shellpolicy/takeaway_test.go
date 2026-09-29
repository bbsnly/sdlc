package shellpolicy

// US-029.2, decision 4 and T1/T2, with the checkouts on disk as the hook gives
// them through State.Checkouts: the session is held in a story worktree at
// /work/main/.sdlc/worktrees/A-1, inside the main checkout /work/main, beside
// another story's worktree B-2. worktree_test.go holds the names the words
// alone decide; these are the ones only the tree of checkouts can: a glob, a
// brace list, a `..` through the tree, find's filters and depths, rsync's
// destination, a path handed to xargs rm, and a path in a program.

import (
	"path"
	"strings"
	"testing"
)

// The layout every test here works in. a1Home is where `~` leads, and holds
// no checkout.
const (
	a1Root   = "/work/main/.sdlc/worktrees/A-1"
	a1Main   = "/work/main"
	a1Other  = "/work/main/.sdlc/worktrees/B-2"
	a1GitDir = "/work/main/.git/worktrees/A-1"
	a1Home   = "/home/me"
)

// a1Abs is where a word of a command lands on disk, as the hook's Place puts
// it: absolute, `~` read as a1Home, and relative to the story worktree
// otherwise. A home the fixture does not know is nowhere.
func a1Abs(word string) string {
	switch {
	case word == "~" || strings.HasPrefix(word, "~/"):
		return path.Join(a1Home, word[1:])
	case strings.HasPrefix(word, "~"):
		return ""
	case isAbsolute(word):
		return path.Clean(word)
	}
	return path.Join(a1Root, word)
}

// a1Under is p relative to dir, "." for dir itself, and false where p is not
// in it.
func a1Under(p, dir string) (string, bool) {
	if p == dir {
		return ".", true
	}
	rest, ok := strings.CutPrefix(p, dir+"/")
	return rest, ok
}

// heldInA1 is s for a session held in story worktree A-1. Resolve answers for
// the worktree's own files, Shared for the main checkout, for B-2 and for the
// worktree's own git directory under the main checkout's .git, and Checkouts
// gives the three checkouts with the worktree the story is held in first.
func heldInA1(s State) State {
	s.Backlog = "user_stories.json"
	s.Resolve = func(word string) string {
		if rest, ok := a1Under(a1Abs(word), a1Root); ok && rest != "." && isAbsolute(word) {
			return rest
		}
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	s.Shared = func(word string) []Landing {
		p := a1Abs(word)
		if p == "" {
			return nil
		}
		if rest, ok := a1Under(p, a1GitDir); ok {
			return []Landing{{Rel: strings.TrimSuffix(".git/"+rest, "/."), Own: true}}
		}
		if _, ok := a1Under(p, a1Root); ok {
			return nil
		}
		if rest, ok := a1Under(p, a1Other); ok {
			return []Landing{{Rel: rest}}
		}
		if rest, ok := a1Under(p, a1Main); ok {
			return []Landing{{Rel: rest}}
		}
		return nil
	}
	s.Checkouts = func() CheckoutSet {
		return CheckoutSet{
			Checkouts: []string{a1Root, a1Main, a1Other},
			Place: func(word string) []string {
				if p := a1Abs(word); p != "" {
					return []string{p}
				}
				return nil
			},
		}
	}
	return s
}

// taRefusedNaming holds a command to a refusal for taking a checkout away that
// says which one, and that it is a guess where the word is a pattern.
func taRefusedNaming(t *testing.T, command string, s State, names ...string) {
	t.Helper()
	f, ok := inspect(t, command, s)
	switch {
	case !ok:
		t.Errorf("allowed: %s", command)
		return
	case f.Rule != "loop-state-through-the-tool":
		t.Errorf("%s was refused by %s, want loop-state-through-the-tool (%s)", command, f.Rule, f.Reason)
		return
	case !strings.Contains(f.Route, "in their own terminal"):
		t.Errorf("%s: the route does not send the removal to a person: %s", command, f.Route)
	}
	for _, name := range names {
		if !strings.Contains(f.Reason, name) {
			t.Errorf("%s: the reason does not say %q: %s", command, name, f.Reason)
		}
	}
}

// AC-6, T1 and T2: a glob, or a brace list, that could match a checkout, or a
// directory holding one, takes it away as surely as its name does. The refusal
// names what it could reach, the held checkout first where it could reach
// several, and says it is a guess.
func TestAC6AndT1AGlobThatCouldMatchACheckoutIsRefusedNamingWhatItCouldReach(t *testing.T) {
	s := heldInA1(ready)
	for _, tc := range []struct {
		command string
		names   []string
	}{
		{"rm -rf ../B-*", []string{"could match", a1Other, "another checkout"}},
		{"rm -rf ../?-2", []string{"could match", a1Other}},
		{"rm -rf ../[AB]-2", []string{"could match", a1Other}},
		{"rm -rf ../../worktrees/B-*", []string{"could match", a1Other}},
		{"rm -rf /work/main/.sdlc/worktrees/*", []string{"could match", a1Root, "the checkout this story is held in"}},
		{"rm -rf /work/*", []string{"could match", a1Main}},
		{"rm -rf /work/ma*", []string{"could match", a1Main}},
		{"rm -rf /*/main", []string{"could match", a1Main}},
		{"rm -rf /wor?/main/.sdlc", []string{"could match", "a directory holding a checkout"}},
		{"rm -rf /work/main/.sdlc/worktrees/*/..", []string{"could match", "a directory holding a checkout"}},
		// A name the tree knows, spelled as the filesystem folds it, and a
		// brace list of names.
		{"rm -rf /WORK/main", []string{"names", a1Main}},
		{"rm -rf ../{B-2,C-3}", []string{"names", a1Other}},
		{"mv ../{C-3,B-2} /tmp/gone", []string{a1Other}},
		// Back down into the held checkout by a pattern that matches every name.
		{"rm -rf ../*", []string{"could match", "the checkout this story is held in"}},
		// More brace lists than the rules expand is taken to reach one.
		{"rm -rf ../{a,b}{c,d}{e,f}{g,h}{i,j}{k,l}{m,n}{o,p}", []string{"could match"}},
	} {
		taRefusedNaming(t, tc.command, s, tc.names...)
	}

	// What matches no checkout, and nothing holding one, is the story's own to
	// clear away.
	for _, command := range []string{
		"rm -rf ../C-*",
		"rm -rf ./build/*",
		"rm -rf build/*.o",
		"rm -rf /tmp/*",
		"rm -rf ../B-2/build/*",
		"rm -rf ~/*",
		"rm -rf /work/main/.sdlc/worktrees/B-2/build",
		"rm -rf ../{C-3,D-4}",
		"mv ../C-* /tmp",
	} {
		allowed(t, command, s)
	}
}

// AC-6, T1 and T2: find removes what passes its filters below where it starts,
// so a start above a checkout is refused when the checkout could pass them, and
// allowed when a depth, a type or -empty rules it out.
func TestAC6AndT1FindIsRefusedWhereACheckoutBelowItCouldPassItsFilters(t *testing.T) {
	s := heldInA1(ready)
	for _, tc := range []struct {
		command string
		names   []string
	}{
		{"find .. -maxdepth 1 -name 'B-*' -exec rm -rf {} +", []string{"could match", a1Other}},
		{"find .. -name B-2 -delete", []string{"could match", a1Other}},
		{"find .. -iname 'b-2' -delete", []string{"could match", a1Other}},
		{"find /work/main -path '*/worktrees/B-2' -exec rm -rf {} +", []string{"could match", a1Other}},
		// Both story worktrees are three levels down, and which of them the
		// refusal names is not held here: only that it names one.
		{"find /work/main -mindepth 3 -maxdepth 3 -type d -exec rm -rf {} +", []string{"could match", "/work/main/.sdlc/worktrees/"}},
		// A depth that is not a number is no depth at all.
		{"find .. -maxdepth deep -name B-2 -delete", []string{"could match", a1Other}},
		// The start itself, where it passes the filters.
		{"find ../B-2 -delete", []string{"B-2"}},
		{`find "$PWD" -delete`, []string{"the checkout this story is held in"}},
	} {
		taRefusedNaming(t, tc.command, s, tc.names...)
	}
	for _, command := range []string{
		"find . -name '*.pyc' -delete",
		"find .. -maxdepth 1 -name 'C-*' -exec rm -rf {} +",
		"find /work/main -mindepth 4 -type d -exec rm -rf {} +",
		"find .. -maxdepth 1 -name B-2 -type f -delete",
		"find /tmp -name 'B-*' -delete",
		"find .. -name B-2",
		"find $X -name B-2 -delete",
		"find '../B-*' -name x -delete",
	} {
		allowed(t, command, s)
	}
}

// AC-6 and T2: rsync --delete empties its destination of what the source
// lacks, and a destination that is a checkout, or holds one, loses it.
func TestAC6AndT2RsyncDeleteIntoACheckoutIsRefusedAndTheReasonSaysWhy(t *testing.T) {
	s := heldInA1(ready)
	for _, tc := range []struct {
		command string
		names   []string
	}{
		{"rsync -a --delete dist/ ../B-2/", []string{"rsync --delete", "removes what the source lacks", a1Other, " is "}},
		{"rsync -a --delete-after dist/ /work/main", []string{"rsync --delete", a1Main}},
		{"rsync -a --del dist/ /work/main/.sdlc/worktrees", []string{"rsync --delete", " holds "}},
	} {
		taRefusedNaming(t, tc.command, s, tc.names...)
	}
	for _, command := range []string{
		"rsync -a --delete dist/ /tmp/out/",
		"rsync -a dist/ ../B-2/",
		"rsync --delete dist",
		"rsync -a --delete dist/ ./public/",
	} {
		allowed(t, command, s)
	}
}

// AC-6, T1 and T2: a path handed over in another segment, or in a program, is
// placed as the words of a command are.
func TestAC6AndT2APathHandedToARemoverByAPipeOrInAProgramIsPlaced(t *testing.T) {
	s := heldInA1(ready)
	for _, tc := range []struct {
		command string
		names   []string
	}{
		{"echo ../B-2 | xargs rm -rf", []string{"names", a1Other}},
		{"echo /work/main | xargs rm -rf", []string{"names", a1Main}},
		{"printf '%s\\n' build ../B-2 | xargs rm -rf", []string{a1Other}},
		{`python3 -c 'import shutil; shutil.rmtree("/work/main")'`, []string{a1Main}},
		{`node -e 'require("fs").rmSync("/work/main/.sdlc/worktrees/B-2", {recursive: true})'`, []string{a1Other}},
	} {
		taRefusedNaming(t, tc.command, s, tc.names...)
	}
	ps := wtPowerShell(heldInA1(ready))
	taRefusedNaming(t, "[IO.Directory]::Delete('/work/main', $true)", ps, a1Main)
	taRefusedNaming(t, "[IO.Directory]::Delete((Get-Location), $true)", ps, "the checkout this story is held in")
	for _, command := range []string{
		"echo build | xargs rm -rf",
		"find . -name '*.o' | xargs rm -rf",
		"echo .. | xargs rm -rf",
		"echo ../B-2 | xargs ls",
		`python3 -c 'import shutil; shutil.rmtree("/tmp/x")'`,
		`python3 -c 'print(open("/work/main/go.mod").read())'`,
	} {
		allowed(t, command, s)
	}
	allowed(t, "[IO.File]::ReadAllText('/work/main/go.mod')", ps)
}

// AC-6 and T1: $OLDPWD and ~- are where the shell was before its last cd.
// Before any cd that is somewhere the command does not show, which could be
// the checkout; after one, it is the directory the command left.
func TestAC6AndT1WhereTheShellWasIsTheDirectoryItLeft(t *testing.T) {
	s := heldInA1(ready)
	taRefusedNaming(t, `rm -rf "$OLDPWD"`, s, "could match", "the checkout this story is held in")
	taRefusedNaming(t, `cd /tmp && rm -rf "$OLDPWD"`, s, "names", "the checkout this story is held in")
	taRefusedNaming(t, "cd /tmp && rm -rf ~-/..", s, "names", "a directory holding")
	allowed(t, `cd build && cd /tmp && rm -rf "$OLDPWD"`, s)
	allowed(t, `cd build && rm -rf "$OLDPWD/out"`, s)
}

// AC-6: lexically is what a word names relative to the checkout by its name
// alone, as levels above it; a word that cannot come back to the checkout or
// above it names neither.
func TestAC6AWordNamesTheCheckoutOrADirectoryAboveItByItsNameAlone(t *testing.T) {
	for _, tc := range []struct {
		word   string
		levels int
		ok     bool
	}{
		{".", 0, true},
		{"./", 0, true},
		{"build/..", 0, true},
		{"..", 1, true},
		{"../..", 2, true},
		{"../*", 0, true},
		{"*/..", 0, true},
		{"../.*", 1, true},
		{".*", 0, true},
		{"build", 0, false},
		{"../B-2", 0, false},
		{"../?", 0, false},
		{"build/*", 0, false},
	} {
		levels, ok := lexically(tc.word)
		if ok != tc.ok || ok && levels != tc.levels {
			t.Errorf("lexically(%q) = %d, %v; want %d, %v", tc.word, levels, ok, tc.levels, tc.ok)
		}
	}
	// Past the bound on places a glob is followed to, the word is taken to
	// reach the checkout.
	if levels, ok := lexically(strings.Repeat(".*/", 64)); !ok || levels != 0 {
		t.Errorf("lexically of 64 `.*` = %d, %v; want the checkout itself", levels, ok)
	}
}

// AC-6: the tree of checkouts follows a glob from where it is placed, one
// segment at a time, and answers with the directory a refusal would name
// first: the held checkout, then another, then one holding a checkout.
func TestAC6TheTreeOfCheckoutsFollowsAGlobToTheCheckoutItCouldReach(t *testing.T) {
	tree := newCheckoutTree(CheckoutSet{Checkouts: []string{a1Root, a1Main, a1Other}})
	for _, tc := range []struct {
		start    string
		segments []string
		want     string // "" for none
	}{
		{a1Main, []string{".sdlc", "worktrees", "*"}, a1Root},
		{a1Main, []string{".sdlc", "worktrees", "B-*"}, a1Other},
		{a1Main, []string{".sdlc", "*"}, "/work/main/.sdlc/worktrees"},
		{a1Main, []string{"."}, a1Main},
		{a1Root, []string{"..", "B-?"}, a1Other},
		{a1Root, []string{"..", ".."}, "/work/main/.sdlc"},
		{a1Root, []string{".*"}, a1Root},
		{a1Root, []string{"build", "..", ".."}, "/work/main/.sdlc/worktrees"},
		{a1Root, []string{"build", "*"}, ""},
		{a1Main, []string{"src", "*"}, ""},
		{"/elsewhere/x", []string{"*"}, ""},
		{"/elsewhere/x", []string{"..", "..", "work"}, "/work"},
	} {
		n, ok := tree.reach(tc.start, tc.segments)
		switch {
		case tc.want == "" && ok:
			t.Errorf("reach(%s, %v) = %s, want none", tc.start, tc.segments, n.path)
		case tc.want != "" && !ok:
			t.Errorf("reach(%s, %v) = none, want %s", tc.start, tc.segments, tc.want)
		case ok && n.path != tc.want:
			t.Errorf("reach(%s, %v) = %s, want %s", tc.start, tc.segments, n.path, tc.want)
		}
	}
	// More places than the bound follows are taken to reach the held checkout.
	many := make([]string, 64)
	for i := range many {
		many[i] = ".*"
	}
	if n, ok := tree.reach(a1Root, many); !ok || n.path != a1Root {
		t.Errorf("reach through 64 `.*` = %v, %v; want the held checkout", n, ok)
	}
}

// AC-6: with no Place to ask, a word is where its absolute path says, and a
// relative one is somewhere the tree cannot know.
func TestAC6AWordTheTreeCannotPlaceNamesNoCheckout(t *testing.T) {
	s := ready
	s.Checkouts = func() CheckoutSet { return CheckoutSet{Checkouts: []string{"/work/project", "/work/main"}} }
	taRefusedNaming(t, "rm -rf /work/main", s, "names", "/work/main")
	allowed(t, "rm -rf ../main", s)
	allowed(t, "rm -rf build", s)
}
