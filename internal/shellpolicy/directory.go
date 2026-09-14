package shellpolicy

import (
	"path"
	"strings"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// takenAway are the words a command takes away whole, a directory as much as a
// file: what rm -r, rmdir and del remove, what mv moves from, what git rm,
// checkout and restore put back, and find's roots when it deletes everything
// it finds. Not where anything goes: `cp new.go internal/calc/` and `mv new.go
// internal/calc` leave the directory where it was.
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
			named = rest
		case "mv":
			named = sources(rest)
		}
		if moved != "" {
			for j, w := range named {
				named[j] = path.Join(moved, clean(w))
			}
		}
		return named
	}
	return nil
}

// recursive reports whether rm was asked to go into directories: -r, -R, a
// bundle with either in it, --recursive, or PowerShell's -Recurse.
func recursive(args []string) bool {
	for _, a := range args {
		if a == "--recursive" || strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a, "rR") {
			return true
		}
	}
	return false
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

// holding is a frozen path inside the directory word names, or any of them when
// the word is the project's own directory, which it is only where the command
// runs at the project's root.
func holding(word string, frozen []string, atRoot bool) (string, bool) {
	c := strings.TrimPrefix(clean(word), "./")
	if c == "." {
		if atRoot && len(frozen) > 0 {
			return frozen[0], true
		}
		return "", false
	}
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
