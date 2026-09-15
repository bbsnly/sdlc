package shellpolicy

import (
	"path"
	"strings"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// takenAway are the words a command takes away whole, a directory as much as a
// file: what rm -r, rmdir and del remove, what mv moves from, what tar
// --remove-files and rsync --remove-source-files copy and delete, what git rm,
// checkout, restore and clean put back or clear out, and find's roots when it
// deletes everything it finds. Not where anything goes: `cp new.go
// internal/calc/` and `mv new.go internal/calc` leave the directory where it
// was.
func takenAway(words []string) []string {
	if len(words) == 0 {
		return nil
	}
	args := words[1:]
	switch base(words[0]) {
	case "rm", "remove-item", "ri":
		// Without -r, rm leaves a directory alone and says so.
		if recursive(args) {
			return args
		}
	case "rmdir", "rd", "del", "erase":
		return args
	case "mv", "move", "move-item", "mi", "ren", "rename", "rename-item", "rni":
		return sources(args)
	case "tar":
		// Each file goes once it is in the archive.
		if hasWord(args, "--remove-files") {
			_, _, operands, _ := tarArguments(args)
			return operands
		}
	case "rsync":
		// Each file goes once it is copied, which empties a directory of them.
		if hasWord(args, "--remove-source-files") {
			if operands := copyOperands(args, rsyncValued); len(operands) > 1 {
				return operands[:len(operands)-1]
			}
		}
	case "find":
		// With a name to look for, find deletes what has it, which foundBy reads.
		if roots, names, patterns := findArguments(args); hasWord(args, "-delete") && len(names)+len(patterns) == 0 {
			return roots
		}
	case "git":
		sub, moved, rest := gitCall(args)
		var named []string
		switch sub {
		case "rm", "checkout", "restore":
			if gitChangesFiles(args) {
				named = rest
			}
		case "mv":
			named = sources(rest)
		case "stash":
			named = stashPaths(rest)
		case "clean":
			// Without -f, git clean cleans nothing.
			if gitOption(rest, "--force") || shortFlag(rest, 'f') {
				named = cleaned(rest)
			}
		}
		if moved == "" {
			return named
		}
		// A new slice: named shares the command's own words, and joined in
		// place, the next rule to read them read internal/internal/calc.
		joined := make([]string, len(named))
		for j, w := range named {
			joined[j] = path.Join(moved, clean(w))
		}
		return joined
	}
	return nil
}

// discardsTheWorkTree reports whether a git command throws away what the work
// tree holds that is not committed, all of it, wherever in the tree the
// command runs: `git stash`, `git reset --hard`, `git switch -f`, `git
// read-tree -u`, and a clean, restore or checkout of the whole tree, as `:/` names
// it. The loop's record is never committed, and a frozen test is not
// until the story is.
func discardsTheWorkTree(words []string) bool {
	if !isGit(words) {
		return false
	}
	sub, _, rest := gitCall(words[1:])
	switch sub {
	case "stash":
		return len(rest) == 0 || !stashKeeps[rest[0]] && wholeTree(stashPaths(rest))
	case "clean":
		return (gitOption(rest, "--force") || shortFlag(rest, 'f')) && wholeTree(cleaned(rest))
	case "restore":
		return gitChangesFiles(words[1:]) && wholeTree(cleaned(rest))
	case "reset":
		return gitOption(rest, "--hard") || gitOption(rest, "--merge") || gitOption(rest, "--keep")
	case "checkout", "switch":
		return gitOption(rest, "--force") || gitOption(rest, "--discard-changes") || shortFlag(rest, 'f') ||
			wholeTree(cleaned(rest))
	case "read-tree":
		return shortFlag(rest, 'u')
	}
	return false
}

// stashPaths are the paths a stash is limited to, and none when it takes the
// whole work tree. Only push, or a stash with no command named, takes paths, and
// only those written after `--` are read: a quoted message is split into words
// here, and `git stash push -m "wip all"` read as limited to a path called all"
// stashed everything.
func stashPaths(rest []string) []string {
	if len(rest) > 0 && rest[0] == "push" {
		rest = rest[1:]
	} else if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		return nil
	}
	for i, a := range rest {
		if strings.HasPrefix(a, "--pathspec-from-file") {
			return nil
		}
		if a == "--" {
			return rest[i+1:]
		}
	}
	return nil
}

// wholeTree reports whether pathspecs name the whole work tree: none at all, its
// top, which `:/` names wherever git runs, or only what to leave
// out, as `:!build` does.
func wholeTree(pathspecs []string) bool {
	positive := 0
	for _, p := range pathspecs {
		p = clean(p)
		// clean took the slash off `:/`.
		rest, top := strings.CutPrefix(p+"/", ":/")
		switch {
		case strings.HasPrefix(p, ":!") || strings.HasPrefix(p, ":^"):
		case top && path.Clean("./"+rest) == ".":
			return true
		default:
			positive++
		}
	}
	return positive == 0
}

// stashKeeps are the stash commands that leave the work tree as it is.
var stashKeeps = map[string]bool{
	"list": true, "show": true, "drop": true, "clear": true, "create": true, "store": true,
}

// recursive reports whether rm was asked to go into directories: -r, -R, a
// bundle with either in it, --recursive, or PowerShell's -Recurse.
func recursive(args []string) bool {
	return hasWord(args, "--recursive") || shortFlag(args, 'r') || shortFlag(args, 'R')
}

// gitOption reports whether a git long option is among args, spelled out or cut
// short: git takes any start of a long option that names only one, and
// `git restore --staged --work` puts back the work tree as well.
func gitOption(args []string, name string) bool {
	for _, a := range args {
		if len(a) > 2 && strings.HasPrefix(name, a) {
			return true
		}
	}
	return false
}

// shortFlag reports whether a one-letter option is among args, alone or in a
// bundle.
func shortFlag(args []string, flag byte) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.IndexByte(a, flag) >= 0 {
			return true
		}
	}
	return false
}

// cleaned are the paths git clean, restore or checkout is given, or where it runs
// when it is given none.
func cleaned(args []string) []string {
	var named []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-e" || a == "--exclude":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			named = append(named, a)
		}
	}
	if len(named) == 0 {
		return []string{"."}
	}
	return named
}

// sources are what a move takes from: every operand but the last, which is
// where they go, unless where they go was given as an option.
func sources(args []string) []string {
	var named []string
	target := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-t" || a == "--target-directory" || strings.EqualFold(a, "-Destination"):
			target = true
			i++
		case strings.HasPrefix(a, "-t") || strings.HasPrefix(a, "--target-directory=") ||
			len(a) > len("-Destination:") && strings.EqualFold(a[:len("-Destination:")], "-Destination:"):
			target = true
		case strings.HasPrefix(a, "-"):
		default:
			named = append(named, a)
		}
	}
	if !target && len(named) > 0 {
		named = named[:len(named)-1]
	}
	return named
}

// holding is a frozen path inside the directory word names. The project's own
// directory is the loop state's to refuse, which it holds as well.
func holding(word string, frozen []string) (string, bool) {
	c := strings.TrimPrefix(clean(word), "./")
	w := pathrules.Fold(c)
	for _, f := range frozen {
		folded := pathrules.Fold(f)
		for i := 0; i < len(folded); i++ {
			if folded[i] != '/' {
				continue
			}
			// An absolute path is matched by its end, as a file's is, where
			// the project's own path is not known. A relative one starts at
			// the project: build/internal/calc is not internal/calc.
			if d := folded[:i]; w == d || isAbsolute(c) && strings.HasSuffix(w, "/"+d) {
				return f, true
			}
		}
	}
	return "", false
}

// globHolding is a frozen path inside a directory a glob names, the glob
// matched segment by segment from the project's root, as the shell expands it
// there: `*` is internal, and so internal/calc/add_test.go goes with it.
func globHolding(pattern string, frozen []string) (string, bool) {
	p := strings.Split(pattern, "/")
	for _, f := range frozen {
		names := strings.Split(f, "/")
		if len(p) >= len(names) {
			// The file itself, or below it, which the freeze's own globs read.
			continue
		}
		matched := true
		for i := range p {
			if !segmentMatches(p[i], names[i]) {
				matched = false
				break
			}
		}
		if matched {
			return f, true
		}
	}
	return "", false
}
