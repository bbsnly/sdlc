package shellpolicy

import "strings"

// The wrappers program takes off the front whose options need more than
// skipOptions gives them: a value that is not a number, or a command line of
// their own. Read as flags, the value was the program, and the program behind
// it -- a commit, or a person's approval -- met no rule.

// env reads env's options and the variables it sets, and returns the command
// it runs. -S hands env a string that it splits into words, options and
// variables among them, and reads as though they had been written in its
// place: `env -S 'git commit -m x'` and `env -S 'sdlc approve A-1'` were a
// program called `git commit -m x`. An option with a value takes it: `env -u
// HOME git commit` was a command called HOME.
func (run *invocation) env(args []string) []string {
	// A bound, since each -S can hand over another.
	for range 8 {
		var split string
		var found bool
		args, split, found = run.envOptions(args)
		for len(args) > 0 && isAssignment(args[0]) {
			run.assigns = append(run.assigns, args[0])
			args = args[1:]
		}
		if !found {
			return args
		}
		args = append(strings.Fields(split), args...)
	}
	return args
}

// envOptions skips env's options, keeping where -C sends the command, and
// returns what follows them and the string an -S gave, if one did.
func (run *invocation) envOptions(args []string) (rest []string, split string, found bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func(glued string) string {
			if glued == "" && i+1 < len(args) {
				i++
				return args[i]
			}
			return glued
		}
		switch {
		case a == "--":
			return args[i+1:], split, found
		case a == "-":
			// -i, spelled the old way.
		case !strings.HasPrefix(a, "-"):
			return args[i:], split, found
		case strings.HasPrefix(a, "--"):
			if s, ok := run.envLong(a, value); ok {
				split, found = s, true
			}
		default:
			if s, ok := run.envBundle(a, value); ok {
				split, found = s, true
			}
		}
	}
	return nil, split, found
}

// envLong reads one of env's long options, taking the next word through
// value where the option takes one and has none glued on, and returns the
// string a --split-string gave.
func (run *invocation) envLong(a string, value func(string) string) (string, bool) {
	name, v, glued := strings.Cut(a, "=")
	switch name {
	case "--split-string", "--chdir", "--unset", "--argv0":
		if !glued {
			v = value("")
		}
		switch name {
		case "--split-string":
			return v, true
		case "--chdir":
			run.chdir = v
		}
	}
	return "", false
}

// envBundle reads one bundle of env's short options, `-iu NAME`, where a
// letter that takes a value takes the rest of the bundle or the next word, and
// returns the string an S gave.
func (run *invocation) envBundle(a string, value func(string) string) (string, bool) {
	for j := 1; j < len(a); j++ {
		switch a[j] {
		case 'S':
			return value(a[j+1:]), true
		case 'C':
			run.chdir = value(a[j+1:])
		case 'u', 'P', 'a', 'L', 'U':
			value(a[j+1:])
		default:
			continue
		}
		break
	}
	return "", false
}

// flocked is what flock runs: after its options, the lock file or descriptor,
// and then the command, or with -c a command line, which line reports.
func flocked(args []string) (words []string, line bool) {
	i := 0
	for ; i < len(args) && strings.HasPrefix(args[i], "-"); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		switch {
		case a == "--wait" || a == "--timeout" || a == "--conflict-exit-code":
			i++
		case strings.HasPrefix(a, "--"):
		case strings.ContainsAny(a[len(a)-1:], "wE"):
			// -w and -E take the next word when nothing is glued after them.
			i++
		}
	}
	if i >= len(args) {
		return nil, false
	}
	rest := args[i+1:]
	if len(rest) > 1 && (rest[0] == "-c" || rest[0] == "--command") {
		return rest[1:2], true
	}
	return rest, false
}

// scripted is what script runs: util-linux's -c or --command gives it a
// command line, which line reports, and BSD's and macOS's take the command
// after the file the session is written to. macOS's -F takes no value, and
// read as taking one, `script -F f git commit` ran a command called commit.
func scripted(args []string) (words []string, line bool) {
	i := 0
	for ; i < len(args) && strings.HasPrefix(args[i], "-"); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if strings.HasPrefix(a, "--") {
			if words, ok := scriptLong(a, args, &i); ok {
				return words, true
			}
			continue
		}
		if words, ok := scriptBundle(a, args, &i); ok {
			return words, true
		}
	}
	// The file, then the command.
	if i+1 >= len(args) {
		return nil, false
	}
	return args[i+1:], false
}

// scriptLong reads one of script's long options, moving i past a value given
// in the next word, and returns the command line a --command gives.
func scriptLong(a string, args []string, i *int) ([]string, bool) {
	name, v, glued := strings.Cut(a, "=")
	switch name {
	case "--command":
		if glued {
			return []string{v}, true
		}
		return commandLine(args[*i+1:]), true
	case "--echo", "--log-in", "--log-out", "--log-io", "--log-timing", "--logging-format", "--output-limit":
		if !glued {
			*i++
		}
	}
	return nil, false
}

// scriptBundle reads one bundle of script's short options, moving i past a
// value given in the next word, and returns the command line a c gives.
func scriptBundle(a string, args []string, i *int) ([]string, bool) {
	for j := 1; j < len(a); j++ {
		switch c := a[j]; {
		case c == 'c':
			if j+1 < len(a) {
				return []string{a[j+1:]}, true
			}
			return commandLine(args[*i+1:]), true
		case strings.IndexByte("EIOBTmo", c) >= 0:
			if j+1 == len(a) {
				*i++
			}
			return nil, false
		case c == 't':
			// util-linux takes a file glued on, and BSD the next word as a
			// number of seconds.
			if j+1 == len(a) && *i+1 < len(args) && strings.Trim(args[*i+1], "0123456789.") == "" {
				*i++
			}
			return nil, false
		}
	}
	return nil, false
}

// commandLine is the one word a -c gives, the command line, without the file
// that follows it.
func commandLine(words []string) []string {
	if len(words) == 0 {
		return nil
	}
	return words[:1]
}
