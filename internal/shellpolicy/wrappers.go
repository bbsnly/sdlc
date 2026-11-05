package shellpolicy

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"
)

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
func (run *invocation) env(args []string, reading assignments) []string {
	// A bound, since each -S can hand over another.
	for range 8 {
		var split string
		var found bool
		args, split, found = run.envOptions(args)
		for len(args) > 0 && reading.is(args[0]) {
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
			run.cleared = true
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
	if abbreviates(name, "--ignore-environment") {
		// GNU's getopt takes a long option by any prefix that names one, and
		// `--ignore-env` empties the environment as the whole name does. A
		// prefix that could name --ignore-signal too is refused by env itself.
		run.cleared = true
		return "", false
	}
	switch name = longName(name, envLongs); name {
	case "--split-string", "--chdir", "--unset", "--argv0":
		if !glued {
			v = value("")
		}
		switch name {
		case "--split-string":
			return v, true
		case "--chdir":
			run.chdir = v
		case "--unset":
			run.unsets = append(run.unsets, v)
		}
	}
	return "", false
}

// envLongs are GNU env's long options.
var envLongs = []string{"--ignore-environment", "--null", "--unset", "--chdir", "--split-string",
	"--block-signal", "--default-signal", "--ignore-signal", "--list-signal-handling", "--debug", "--help",
	"--version", "--argv0"}

// longName is the one of options that name, before any `=`, names: the
// option itself, or the one option it abbreviates, as GNU's getopt reads it.
// It is "" where it names none, or more than one, which getopt refuses.
func longName(name string, options []string) string {
	found := ""
	for _, o := range options {
		switch {
		case o == name:
			return o
		case abbreviates(name, o):
			if found != "" {
				return ""
			}
			found = o
		}
	}
	return found
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
		case 'u':
			run.unsets = append(run.unsets, value(a[j+1:]))
		case 'P', 'a', 'L', 'U':
			value(a[j+1:])
		case 'i', '-':
			// An environment emptied: env runs the program with only what is
			// set after it, so AI_AGENT is gone however PATH is passed back.
			run.cleared = true
			continue
		default:
			continue
		}
		break
	}
	return "", false
}

// xargs reads xargs' options, GNU's and BSD's, and returns the command it
// runs, keeping the string -I, -J, -i and --replace give, which xargs fills in
// with each argument it reads. Read as flags, the value of `xargs -I {} rm {}`
// was the program; and with `-I{}` read as a group's brace, the program after
// it was lost, so that the word after that was read as the program (US-112,
// security R2-2).
func (run *invocation) xargs(args []string) []string {
	run.piped = true
	for len(args) > 0 {
		switch a := args[0]; {
		case a == "--":
			return args[1:]
		case a == "-" || !strings.HasPrefix(a, "-"):
			return args
		case strings.HasPrefix(a, "--"):
			args = run.xargsLong(args)
		default:
			args = run.xargsBundle(args)
		}
	}
	return args
}

// xargsBundle reads one bundle of xargs' short options, and returns the words
// after it and its value. The letters in valued take the rest of the bundle or
// the next word; -e, -i and -l are GNU's, whose value is glued on or absent.
func (run *invocation) xargsBundle(args []string) []string {
	const valued = "adEIJLnPRsS"
	a := args[0]
	for j := 1; j < len(a); j++ {
		switch c := a[j]; {
		case strings.IndexByte(valued, c) >= 0:
			v, rest := a[j+1:], args[1:]
			if v == "" && len(rest) > 0 {
				v, rest = rest[0], rest[1:]
			}
			if c == 'I' || c == 'J' {
				run.replace = v
			}
			return rest
		case c == 'i':
			run.replace = cmp.Or(a[j+1:], "{}")
			return args[1:]
		case c == 'e' || c == 'l':
			return args[1:]
		}
	}
	return args[1:]
}

// xargsLong reads one of GNU xargs' long options, by any prefix that names
// one, and returns the words after it and its value.
func (run *invocation) xargsLong(args []string) []string {
	name, v, glued := strings.Cut(args[0], "=")
	rest := args[1:]
	switch name = longName(name, xargsLongs); name {
	case "--arg-file", "--delimiter", "--max-args", "--max-procs", "--max-chars", "--process-slot-var":
		if !glued && len(rest) > 0 {
			v, rest = rest[0], rest[1:]
		}
		if name == "--process-slot-var" {
			// xargs sets the variable it names for each command it runs.
			run.assigns = append(run.assigns, v+"=")
		}
	case "--replace":
		run.replace = cmp.Or(v, "{}")
	}
	return rest
}

// xargsLongs are GNU xargs' long options. --eof, --replace and --max-lines
// take a value only after `=`.
var xargsLongs = []string{"--arg-file", "--delimiter", "--eof", "--replace", "--max-lines", "--max-args",
	"--max-procs", "--interactive", "--process-slot-var", "--null", "--no-run-if-empty", "--max-chars",
	"--show-limits", "--verbose", "--exit", "--open-tty", "--help", "--version"}

// exec reads the exec builtin's options: -c starts the command with an empty
// environment, -l as a login shell, and -a gives it another name. `-` is
// zsh's precommand modifier, which program takes off next.
func (run *invocation) exec(args []string) []string {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
		a := args[0]
		args = args[1:]
		if a == "--" {
			break
		}
		args = run.execBundle(a, args)
	}
	return args
}

// execBundle reads one bundle of exec's flags, and returns the words after it
// and after the name an -a that ends it takes.
func (run *invocation) execBundle(a string, args []string) []string {
	for j := 1; j < len(a); j++ {
		switch a[j] {
		case 'c':
			run.cleared = true
		case 'a':
			if j+1 == len(a) && len(args) > 0 {
				return args[1:]
			}
			return args
		}
	}
	return args
}

// arch reads macOS arch's options, which it matches whole: -arch takes an
// architecture, -d a variable it unsets and -e one it sets, -c empties the
// environment, and any other word that starts with a dash names an
// architecture. A glued -dNAME is read as an unset too.
func (run *invocation) arch(args []string) []string {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		a := args[0]
		args = args[1:]
		var v string
		if (a == "-arch" || a == "-d" || a == "-e") && len(args) > 0 {
			v, args = args[0], args[1:]
		}
		switch {
		case a == "--":
			return args
		case a == "-c":
			run.cleared = true
		case strings.HasPrefix(a, "-d"):
			run.unsets = append(run.unsets, cmp.Or(v, a[2:]))
		case a == "-e":
			run.assigns = append(run.assigns, v)
		}
	}
	return args
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

// optionTable is how a wrapper that runs the command after its options reads
// them, from the tool's manual or its --help: the short letters that take a
// value, glued or the next word, and those that take none; the long options
// that take a value, `=VALUE` or the next word, and those that take none.
// Read as flags alone, an option's value was the program: in `time -p rm …`
// the program was `-p`, and every rule let it through (US-112, code review
// B1). The tables are static data, with no init work.
type optionTable struct {
	// names are the programs it serves, matched on base.
	names         []string
	valued, flags string
	long, known   []string
	// env, dir and target are options as the table writes them, "-E" or
	// "--env": the one whose value is NAME=VALUE for the command's
	// environment; the one whose value is the directory it runs in, as env
	// -C's is; and the one whose value is the program to debug.
	env, dir, target []string
	// operands are the words taken after the options and before the command,
	// whatever they are: chroot's new root, timeout's duration, ssh's host.
	// optionsAfterOperands is set where options are read after them too.
	operands             int
	optionsAfterOperands bool
	// assigns is set where NAME=VALUE words after the options set the
	// command's environment, as sudo's do. Read as the program, the first of
	// them hid the command: `sudo A=1 git commit -m x` committed unchecked
	// (code review, US-112 Gate 7).
	assigns bool
	// shell is set where the words after the options are a command line for
	// a shell, and piped where the command is handed more words on standard
	// input, as GNU parallel hands them.
	shell, piped bool
	// plus is set where an option may start with `+`, as a shell's `+o` does.
	plus bool
	// interleaved is set where options and positionals mix: the program is
	// target's value, or else the first positional, and the other
	// positionals and what follows `--` are its words.
	interleaved bool
	// words is PowerShell's way: a parameter is a whole word, named in any
	// case by its name or the one name it is the start of, and the table
	// writes each folded, aliases included.
	words bool
}

// tableOf is the table of the program name, or nil for one with none.
func tableOf(name string) *optionTable {
	for i := range optionTables {
		if slices.Contains(optionTables[i].names, name) {
			return &optionTables[i]
		}
	}
	return nil
}

// options reads a wrapper's options by its table, from the place at, and
// returns the words from its program on. It reads as getopt does with `+`:
// `--` ends the options, and the first word that is no option is the first
// operand, or the program. An option the table does not know may or may not
// take a value. This reading takes it as a flag, and a resume of this reader
// on the words after its value takes it the other way (worklist.push).
// Either reading's refusal refuses: `strace -J 1 git commit -m x` runs git,
// or 1.
func (w *worklist) options(run *invocation, t *optionTable, at place, words []string, reading assignments) []string {
	for len(words) > 0 {
		a := words[0]
		switch {
		case at.ended || !t.option(a):
			switch {
			case t.interleaved && at.ended:
				return t.debugged(run, at, words)
			case t.interleaved:
				run.words = append(run.words, a)
			case at.owed > 0:
				at.owed--
				at.ended = at.ended || !t.optionsAfterOperands
			default:
				for t.assigns && len(words) > 0 && reading.is(words[0]) {
					run.assigns = append(run.assigns, words[0])
					words = words[1:]
				}
				return words
			}
			words = words[1:]
		case a == "--":
			at.ended = true
			words = words[1:]
		case t.words:
			name, _ := t.parameter(a)
			words = w.long(run, t, &at, name, "", false, words[1:])
		case strings.HasPrefix(a, "--"):
			name, v, glued := strings.Cut(a, "=")
			words = w.long(run, t, &at, name, v, glued, words[1:])
		default:
			words = w.bundle(run, t, &at, words)
		}
	}
	if t.interleaved {
		return t.debugged(run, at, nil)
	}
	return nil
}

// option reports a word that is one of the table's options, or one it does
// not know: a dash and more, or a plus and more where the table takes one.
func (t *optionTable) option(a string) bool {
	if t.words {
		_, ok := t.parameter(a)
		return ok
	}
	return len(a) > 1 && (a[0] == '-' || t.plus && a[0] == '+')
}

// parameter is the name, folded, that a word gives one of PowerShell's
// parameters: what follows its dash, or the en dash, em dash or horizontal
// bar PowerShell's parser takes as one; or what follows a slash, where that
// names a parameter, so that `/tmp/x.ps1` stays a file. Neither the dashes
// nor the slash could be run here, where there is no PowerShell; read as a
// dash where PowerShell does not take it, a word reads more, never less
// (security, round 7, N2).
func (t *optionTable) parameter(a string) (string, bool) {
	r, n := utf8.DecodeRuneInString(a)
	name := strings.ToLower(a[n:])
	switch r {
	case '-', '–', '—', '―':
		return name, name != ""
	case '/':
		_, _, ok := t.longOption(name)
		return name, ok
	}
	return "", false
}

// longOption is the option a long name gives: the option itself, or the one
// option it is the start of, as GNU's getopt_long and PowerShell take it
// (`--cla` is `--class`). The start of two options, or of none, is not ok,
// and is read both ways; a tool that takes no start of a name fails on one,
// so reading it as the option only reads more.
func (t *optionTable) longOption(name string) (full string, valued, ok bool) {
	n := 0
	for i, options := range [][]string{t.long, t.known} {
		for _, o := range options {
			switch {
			case o == name:
				return o, i == 0, true
			case strings.HasPrefix(o, name):
				full, valued = o, i == 0
				n++
			}
		}
	}
	return full, valued, n == 1
}

// long reads the long option name, with v its value where it was glued on
// after `=`, and returns the words after it.
func (w *worklist) long(run *invocation, t *optionTable, at *place, name, v string, glued bool,
	rest []string) []string {
	full, valued, ok := t.longOption(name)
	switch {
	case ok && valued:
		if !glued && len(rest) > 0 {
			v, rest = rest[0], rest[1:]
		}
		t.took(run, at, full, v)
	case !ok && !glued:
		w.push(resume{table: t, at: *at, rest: afterFirst(rest), run: *run})
	}
	return rest
}

// bundle reads the short options of words[0] letter by letter, and returns
// the words after them: a flag goes on, and a letter that takes a value takes
// the rest of the bundle, or else the next word. A letter the table does not
// know goes on as a flag, and a resume takes what follows it as its value.
func (w *worklist) bundle(run *invocation, t *optionTable, at *place, words []string) []string {
	a, rest := words[0], words[1:]
	for j := 1; j < len(a); j++ {
		switch c := a[j]; {
		case strings.IndexByte(t.flags, c) >= 0:
		case strings.IndexByte(t.valued, c) >= 0:
			v := a[j+1:]
			if v == "" && len(rest) > 0 {
				v, rest = rest[0], rest[1:]
			}
			t.took(run, at, "-"+a[j:j+1], v)
			return rest
		case j+1 == len(a):
			w.push(resume{table: t, at: *at, rest: afterFirst(rest), run: *run})
		default:
			w.push(resume{table: t, at: *at, rest: rest, run: *run})
		}
	}
	return rest
}

// took keeps what an option's value says of the command: the variable strace
// -E sets, whose name is taken out of the environment as well, so that `-E
// AI_AGENT` and `-E AI_AGENT=` read as env -u and an assignment do; the
// directory sudo -D runs it in; and lldb's program.
func (t *optionTable) took(run *invocation, at *place, name, v string) {
	switch {
	case slices.Contains(t.env, name):
		n, _, set := strings.Cut(v, "=")
		run.unsets = append(run.unsets, n)
		if set {
			run.assigns = append(run.assigns, v)
		}
	case slices.Contains(t.dir, name):
		run.chdir = v
	case slices.Contains(t.target, name):
		at.target = v
	}
}

// debugged is what lldb runs once its options end: the program its last -f
// names, wherever the -f stands, or else its first positional, with the
// other positionals and then what follows `--` as its words. lldb made
// `/usr/bin/git` the target of `lldb commit -f /usr/bin/git -- -m x`, with
// the arguments `commit -m x` (security, round 7, N1).
func (t *optionTable) debugged(run *invocation, at place, rest []string) []string {
	var target []string
	if at.target != "" {
		target = []string{at.target}
	}
	words := slices.Concat(target, run.words, rest)
	run.words = nil
	return words
}

// shellKnown are the long options bash takes with no value, which sh, dash,
// ksh and zsh are read with too.
var shellKnown = []string{"--norc", "--noprofile", "--login", "--posix", "--restricted", "--verbose",
	"--version", "--help", "--debugger", "--dump-strings", "--dump-po-strings", "--noediting",
	"--pretty-print"}

// optionTables are the wrappers read by their options, each from its manual,
// and lldb's from `lldb --help` on macOS, the one of them at hand to run. A
// letter or a long option a table lacks is read both ways, which reads more,
// never less.
var optionTables = []optionTable{
	{
		// bash's time keyword takes only -p and --; reading the programs'
		// options for it as well only reads more.
		names: []string{"time", "gtime"}, valued: "fo", flags: "ahlpqvV",
		long:  []string{"--format", "--output"},
		known: []string{"--append", "--portability", "--quiet", "--verbose", "--help", "--version"},
	},
	{
		names: []string{"strace"}, valued: "abeEIoOpPsSuUX", flags: "AcCdDfFhiknqrtTvVwxyYzZ",
		long: []string{"--output", "--trace", "--signal", "--status", "--abbrev", "--verbose", "--raw", "--read",
			"--write", "--fault", "--inject", "--string-limit", "--summary-sort-by", "--columns", "--user",
			"--env", "--attach", "--trace-path", "--detach-on"},
		known: []string{"--output-append-mode", "--summary-only", "--summary", "--debug", "--daemonize",
			"--follow-forks", "--output-separately", "--help", "--instruction-pointer", "--stack-trace",
			"--syscall-number", "--quiet", "--relative-timestamps", "--absolute-timestamps", "--timestamps",
			"--syscall-times", "--no-abbrev", "--version", "--summary-wall-clock", "--strings-in-hex",
			"--decode-fds", "--decode-pids", "--successful-only", "--failed-only", "--seccomp-bpf"},
		env: []string{"-E", "--env"},
	},
	{
		names: []string{"ltrace"}, valued: "aADeFlnopsuwx", flags: "bcCfhiLrStTV",
		long:  []string{"--output", "--library", "--indent", "--align", "--config"},
		known: []string{"--demangle", "--no-signals", "--help", "--version"},
	},
	{
		names: []string{"lldb"}, valued: "aOocfKkSsRlnp", flags: "bQxdehvXrPw",
		long: []string{"--arch", "--one-line-before-file", "--one-line", "--core", "--file",
			"--source-on-crash", "--one-line-on-crash", "--source-before-file", "--source",
			"--repl-language", "--script-language", "--attach-name", "--attach-pid"},
		known: []string{"--batch", "--source-quietly", "--no-lldbinit", "--debug", "--editor", "--help",
			"--version", "--no-use-colors", "--repl", "--python-path", "--wait-for", "--local-lldbinit",
			"--print-script-interpreter-info"},
		target: []string{"-f", "--file"}, interleaved: true,
	},
	{
		names: []string{"chroot"}, valued: "gGu", long: []string{"--groups", "--userspec"},
		known: []string{"--skip-chdir", "--help", "--version"}, operands: 1,
	},
	{
		// gtimeout is GNU timeout under Homebrew's name, and the operand is
		// the duration: skipOptions took it for a number an option was given,
		// and then the command for the duration, so `timeout 60 git commit`
		// was a command called commit.
		names: []string{"timeout", "gtimeout"}, valued: "ks", flags: "fpv",
		long:  []string{"--kill-after", "--signal"},
		known: []string{"--foreground", "--preserve-status", "--verbose", "--help", "--version"}, operands: 1,
	},
	{
		// `nice -5` reads 5 as a letter it does not know, both ways.
		names: []string{"nice"}, valued: "n", long: []string{"--adjustment"},
		known: []string{"--help", "--version"},
	},
	{names: []string{"caffeinate"}, valued: "tw", flags: "dimsu"},
	{
		names: []string{"stdbuf"}, valued: "ioe", long: []string{"--input", "--output", "--error"},
		known: []string{"--help", "--version"},
	},
	{
		// sudo's -h is both its help and --host's short form, so it is in
		// neither list and is read both ways. doas's -u, -C, -n and -s read
		// the same as sudo's. doas takes no NAME=VALUE and fails to run one
		// as a program, so reading it as sudo does refuses nothing that runs.
		names: []string{"sudo", "doas"}, valued: "CDgpRrTUu", flags: "AbBEHiKklnPSsVv", assigns: true,
		long: []string{"--close-from", "--chdir", "--group", "--host", "--prompt", "--chroot", "--role",
			"--type", "--command-timeout", "--other-user", "--user"},
		known: []string{"--askpass", "--background", "--bell", "--preserve-env", "--edit", "--set-home",
			"--help", "--login", "--remove-timestamp", "--reset-timestamp", "--list", "--non-interactive",
			"--preserve-groups", "--stdin", "--shell", "--validate", "--version"},
		dir: []string{"-D", "--chdir"},
	},
	{
		names: []string{"ionice"}, valued: "cnpPu", flags: "tVh",
		long:  []string{"--class", "--classdata", "--pid", "--pgid", "--uid"},
		known: []string{"--ignore", "--help", "--version"},
	},
	{
		// The namespace letters take a file only glued, so the rest of such
		// a bundle reads as letters, and those the table lacks both ways.
		names: []string{"nsenter"}, valued: "tSG", flags: "mupinUCTrwFZa",
		long: []string{"--target", "--setuid", "--setgid"},
		known: []string{"--mount", "--uts", "--ipc", "--net", "--pid", "--user", "--cgroup", "--time",
			"--root", "--wd", "--preserve-credentials", "--no-fork", "--follow-context", "--all", "--help",
			"--version"},
	},
	{
		names: []string{"unshare"}, valued: "SGRw", flags: "mupinUCTfrc",
		long: []string{"--setuid", "--setgid", "--root", "--wd", "--propagation", "--setgroups",
			"--map-user", "--map-group", "--map-users", "--map-groups"},
		known: []string{"--mount", "--uts", "--ipc", "--net", "--pid", "--user", "--cgroup", "--time",
			"--fork", "--mount-proc", "--kill-child", "--map-root-user", "--map-current-user", "--map-auto",
			"--keep-caps", "--help", "--version"},
		dir: []string{"-w", "--wd"},
	},
	{
		names: []string{"systemd-run"}, valued: "upEMH", flags: "rqtPGSd",
		long: []string{"--unit", "--property", "--description", "--slice", "--setenv", "--uid", "--gid",
			"--nice", "--machine", "--host", "--service-type", "--on-active", "--on-boot", "--on-startup",
			"--on-unit-active", "--on-unit-inactive", "--on-calendar", "--path-property",
			"--socket-property", "--timer-property", "--working-directory"},
		known: []string{"--user", "--system", "--scope", "--remain-after-exit", "--send-sighup", "--pty",
			"--pipe", "--quiet", "--wait", "--collect", "--same-dir", "--shell", "--no-block",
			"--no-ask-password", "--help", "--version"},
		env: []string{"-E", "--setenv"}, dir: []string{"--working-directory"},
	},
	{
		// watch hands its words to sh -c, as one command line.
		names: []string{"watch"}, valued: "nqs", flags: "bcdegprtwxhvC",
		long: []string{"--interval", "--equexit", "--shotsdir"},
		known: []string{"--beep", "--color", "--no-color", "--differences", "--errexit", "--chgexit",
			"--precise", "--no-rerun", "--no-title", "--no-wrap", "--exec", "--help", "--version"},
		shell: true,
	},
	{
		// GNU parallel runs its command through a shell, once for each
		// argument after :::, which are left among its words, and takes them
		// from standard input where there are none. Its many flags are read
		// both ways where the table lacks them. --sshdelay is left out on
		// purpose: in the table, it would let the frozen row that reaches
		// `eval --` through its fork pass without the reader's turn through
		// eval (red team, round 8, V38).
		names: []string{"parallel"}, valued: "jPnNLsadEICS",
		long: []string{"--jobs", "--max-procs", "--max-args", "--max-replace-args", "--max-lines",
			"--max-chars", "--arg-file", "--delimiter", "--colsep", "--sshlogin", "--sshloginfile", "--slf",
			"--results", "--res", "--joblog", "--timeout", "--delay", "--memfree", "--load", "--tmpdir",
			"--workdir", "--wd", "--tagstring", "--env", "--header", "--retries", "--halt", "--basefile",
			"--bf", "--return", "--transferfile", "--tf", "--nice", "--rpl"},
		shell: true, piped: true,
	},
	{
		// What follows the host is a command line for the shell at the other
		// end, and localhost is this machine. OpenSSH reads options after the
		// host too, up to the first word that is none: `ssh -G localhost -p
		// 2222` printed port 2222 (security, round 6, N2).
		names: []string{"ssh"}, valued: "BbcDEeFIiJLlmOoPpQRSWw", flags: "46AaCfGgKkMNnqsTtVvXxYy",
		operands: 1, optionsAfterOperands: true, shell: true,
	},
	{
		// chrt fails without a priority, so reading a word that is none as
		// the priority moves the program onto a command that does not run.
		names: []string{"chrt"}, valued: "TPD", flags: "abdefiormRpv",
		long: []string{"--sched-runtime", "--sched-period", "--sched-deadline"},
		known: []string{"--all-tasks", "--batch", "--deadline", "--ext", "--fifo", "--idle", "--other",
			"--reset-on-fork", "--max", "--rr", "--pid", "--verbose", "--help", "--version"},
		operands: 1,
	},
	{
		// The mask, or with -c the list of CPUs, comes before the command.
		names: []string{"taskset"}, flags: "acp",
		known:    []string{"--all-tasks", "--cpu-list", "--pid", "--help", "--version"},
		operands: 1,
	},
	// task-spooler queues the command after its options.
	{names: []string{"ts"}, valued: "LDNW"},
	{
		// The limits' options take a value only glued on (`-n10`,
		// `--nofile=10`), so they are left out, and read both ways.
		names: []string{"prlimit"}, valued: "op", flags: "hV", long: []string{"--output", "--pid"},
		known: []string{"--noheadings", "--raw", "--verbose", "--help", "--version"},
	},
	{
		names: []string{"numactl"}, valued: "iCNmp", flags: "lsHhV",
		long:  []string{"--interleave", "--physcpubind", "--cpunodebind", "--membind", "--preferred"},
		known: []string{"--localalloc", "--show", "--hardware", "--help", "--version"},
	},
	// macOS's: taskpolicy and sandbox-exec read by getopt, as run here.
	{names: []string{"taskpolicy"}, valued: "dgctlpmjSP", flags: "bBasxX"},
	{names: []string{"sandbox-exec"}, valued: "fnpD"},
	{
		// xcrun runs the developer tool it names, git among them. It takes
		// its long options with one dash or two and no bundle, so no letter
		// is listed, and `-sdk macosx` is read both ways.
		names: []string{"xcrun"}, long: []string{"--sdk", "--toolchain"},
		known: []string{"--verbose", "--no-cache", "--kill-cache", "--log", "--find", "--run",
			"--show-sdk-path", "--show-sdk-version", "--show-sdk-build-version", "--show-sdk-platform-path",
			"--show-sdk-platform-version", "--show-toolchain-path"},
	},
	// Their options are not listed: each is read both ways.
	{names: []string{"rlwrap", "eatmydata"}},
	{
		// bash's -O takes a shopt name, and dash and ksh93 refuse -O and +O,
		// so the reading that gives them a value reads a command that does
		// not run.
		names: []string{"sh", "bash", "dash", "ksh"}, valued: "oO", flags: "abcefhiklmnprstuvxBCDEHPT",
		long:  []string{"--rcfile", "--init-file", "--emulate"},
		known: shellKnown, plus: true, shell: true,
	},
	{
		// zsh's -O and +O are CORRECT_ALL, a flag: read as taking a value,
		// `zsh -c -O 'rm …'` handed the script to -O, and no script was read
		// (security, round 5, B1). Its only option with a value is -o.
		names: []string{"zsh"}, valued: "o", flags: "abcefhiklmnprstuvxBCDEHOPT",
		long: []string{"--emulate"}, known: shellKnown, plus: true, shell: true,
	},
	{
		// Version is in neither list: Windows PowerShell's takes a value and
		// pwsh's does not. Command, File, EncodedCommand and CommandWithArgs
		// are flags here, so that the script after them is the program.
		names: []string{"pwsh", "powershell"},
		long: []string{"executionpolicy", "ex", "ep", "workingdirectory", "wd", "outputformat", "o", "of",
			"inputformat", "inp", "if", "configurationname", "config", "configurationfile", "custompipename",
			"settingsfile", "settings", "windowstyle", "w", "psconsolefile", "encodedarguments", "ea"},
		known: []string{"noprofile", "nop", "nologo", "nol", "noninteractive", "noni", "noexit", "noe",
			"interactive", "i", "login", "l", "mta", "sta", "help", "h", "?", "noprofileloadtime", "command",
			"c", "file", "f", "encodedcommand", "e", "ec", "commandwithargs", "cwa"},
		dir:   []string{"workingdirectory", "wd"},
		words: true, shell: true,
	},
}
