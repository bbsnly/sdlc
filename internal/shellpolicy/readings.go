package shellpolicy

import (
	"iter"
	"reflect"
	"slices"
	"strings"
)

// A command is read more than one way where the shell's grammar leaves open
// which program runs. An option a wrapper's table does not know may or may not
// take the word after it, so `strace -J 1 git commit -m x` runs 1 or git; and
// zsh's `]]`, a `}` before always, else or elif, a `{` after a function's name,
// and coproc each start another command. Every reading is made from one
// worklist, deduplicated and capped (US-112, design review round 5): round 4
// copied a reading and read on, the copies forked again inside every fork, and
// security's 34 unknown options in 124 bytes made 14.9 million readings and
// 12.3 GB, which Claude Code answers by letting the call through.

// maxReadings is how many other readings one command may have, and how many
// the commands of a line may have between them in the program walk, counted
// through the scripts it hands a shell. Nothing a person writes comes near
// it; docs/enforcement.md gives the same figure.
const maxReadings = 16

// place is where a table's reader is in a wrapper's options: the operands it
// still takes before the command, whether `--` has been read, and lldb's
// program to debug, which the last -f or --file names.
type place struct {
	owed   int
	ended  bool
	target string
}

// resume is a reading still to make: the reader that takes it up, a table or
// nil for the program reader, with that reader's place; the words left; and
// the reading so far, its slices clipped so that an append to one reading
// never writes another's. Its others and its bound are never set.
type resume struct {
	table *optionTable
	at    place
	rest  []string
	run   invocation
}

// worklist makes one command's readings. throughEval is set for the whole of
// a call of programUnderEval; every resume in one worklist is read the same
// way, so it is no part of a resume.
type worklist struct {
	throughEval bool
	queue       []resume
	pushed      []resume
	unread      bool
}

// program takes off the front of a segment whatever only runs the command
// after it: `(`, `then`, `env`, `sudo`, `sh -c`, `xargs` and their like. Read
// as the first word, `(git commit)`, `env git commit` and `sh -c 'git
// commit'` were commands called `(git`, `env` and `sh`, and no rule knew any
// of them. Its first reading takes every option a table does not know as a
// flag; the others are read through everyReading.
//
// The rules that turn on which program runs read program: eval, trap and
// their like are the program to them, and they read the string such a builtin
// is given as a script of its own, whose readings the program walk counts
// toward the line's (programScan.command).
func program(words []string) invocation {
	return readEvery(words, false)
}

// programUnderEval is program with the builtins that hand the shell a string
// to parse taken off the front of every reading, as many times as they stand
// there, each counted in the reading's evals: eval, trap's action, zsh's
// emulate -c and the callback mapfile and readarray run with -C (evaluated).
// eval runs its words in the shell that calls it, so to the path rules the
// command after it is the command. What the reading set stays set, and what
// wraps the command under the builtin is taken off in turn.
//
// The path rules, named.go's two readers and the find reader read through the
// builtins this way. Only the first reading did, as underEval, until round 7:
// in `eval parallel --sshdelay 1 eval "rm …" ::: 1` the reading that took
// `--sshdelay` with its value was judged as a program called eval, and its rm
// met no path rule (security, round 6, B1). Each turn is a turn of the one
// reader, so the readings under any number of builtins share the command's
// one cap.
func programUnderEval(words []string) invocation {
	return readEvery(words, true)
}

// readEvery is the first reading of words, with the others in its others.
func readEvery(words []string, throughEval bool) invocation {
	w := &worklist{throughEval: throughEval}
	run := w.read(resume{rest: words})
	for len(w.queue) > 0 && !w.unread {
		next := w.queue[0]
		w.queue = w.queue[1:]
		run.others = append(run.others, w.read(next))
	}
	run.unread = w.unread
	return run
}

// readings yields the command's first reading, and then each of the others.
//
// The list is cut short where the command passed maxReadings, and nothing
// past the cut was made. The two walks refuse such a command themselves: the
// program walk, which Inspect and HumanDecisions ask first, counts every
// command it lexes, find's -exec commands among them, and refuses the line
// past the count or past four nested strings (programScan.nests); the path
// walk refuses a command whose own readings passed it, and so does judgeExecs.
// The other readers, findRemoves, named.go's rereadsText and runsCode, and
// runsAScript, read what is there and refuse nothing themselves; that is
// safe because the program walk has read the same line through first, or
// refused it. A new reader reads everyReading, never .words alone. It is a
// function and not a method, since invocation's readers take a pointer and a
// command's readings are asked of the value program returns.
func everyReading(run invocation) iter.Seq2[int, invocation] {
	return func(yield func(int, invocation) bool) {
		if !yield(0, run) {
			return
		}
		for i, o := range run.others {
			if !yield(i+1, o) {
				return
			}
		}
	}
}

// read makes the reading r resumes: the table's reader, where it stopped in a
// wrapper's options, and then the program reader, which takes off one wrapper
// at a time until the program stands first.
func (w *worklist) read(r resume) invocation {
	run, words := r.run, r.rest
	if r.table != nil {
		words = w.options(&run, r.table, r.at, words)
	}
	for len(words) > 0 {
		first := opened(words[0])
		if first == "" {
			words = words[1:]
			continue
		}
		if w.throughEval && len(words) > 1 {
			if handed, from, ok := evaluated(first, words); ok {
				// A turn: the reading goes on in what the builtin hands the
				// shell, with its fields as they stand. Asked before unwrap,
				// whose default case copies the words, so that a chain of
				// builtins is not copied at each (perf, round 7). The words
				// the builtin keeps for itself are scanned here, since no
				// other scan reads them: emulate takes its -c, and mapfile its
				// -C, from anywhere among its words, and zsh ran `{ emulate
				// sh } always { rm … -c x }`, whose `}` stood in no reading
				// (security, round 7, B3).
				w.scan(run, words, 1, from)
				run.evals++
				words = handed
				continue
			}
		}
		var done bool
		if words, done = w.unwrap(&run, first, words); done {
			break
		}
	}
	w.scan(run, run.words, 0, len(run.words))
	return run
}

// scan pushes a reading of the words after each of words[i:j] that starts
// another command (startsAReading). Each is run with what wraps the command,
// and its words are a command of their own. The scan reads, and refuses
// nothing itself: a reading is refused only where a rule refuses what it runs.
func (w *worklist) scan(run invocation, words []string, i, j int) {
	run.words = nil
	for ; i < j; i++ {
		if startsAReading(words, i) {
			w.push(resume{rest: words[i+1:], run: run})
		}
	}
}

// opened is a word with what opens a group or negates a command taken off its
// front: `(`, `{` and `!`, each written plain or after a backslash, which the
// shell takes out of a string handed to eval or to a shell before it reads
// the string. Read with its backslash, `eval \{\ rm\ -f
// .sdlc/stories/US-1/trace.jsonl\;\ \}` was a program called `\{`, and the rm
// eval runs met no rule (verifier, US-112 Gate 7). `{}` is a word, the one
// xargs and find fill in; trimmed as a group's brace, it was `}`, and `xargs
// -I{} sh {} …` ran a script the reading cut at its first character.
func opened(word string) string {
	for {
		w := strings.TrimPrefix(word, `\`)
		if w == "" || w == "{}" || w == `{\}` || !strings.ContainsRune("({!", rune(w[0])) {
			return word
		}
		word = w[1:]
	}
}

// startsAReading reports a word of a reading at which zsh, or bash's coproc,
// starts another command: a `]]` after the first word, the last of a run of
// them, since with SHORT_LOOPS `if [[ -n a && -n b ]] cmd` runs cmd and the
// program walk's lexer ends the command before the `]]`; a `}` before always,
// else or elif, the only words zsh takes after a group's close; a `{` as the
// second word, which `function f {` and `coproc N {` leave; and coproc as the
// first.
func startsAReading(words []string, i int) bool {
	next := ""
	if i+1 < len(words) {
		next = words[i+1]
	}
	switch words[i] {
	case "]]":
		return i > 0 && next != "]]"
	case "}":
		return next == "always" || next == "else" || next == "elif"
	case "{":
		return i == 1
	case "coproc":
		return i == 0
	}
	return false
}

// push queues a reading still to make, unless it would read no word, the
// command is past its cap, or an equal one is queued already. Where
// maxReadings are stored, it sets the bound in place of storing.
//
// A resume reads no word where nothing is left and the reading so far holds
// none either: the `]]` that ends `[[ -f a ]]`, the `{` that ends `function f
// {`, and an unknown option as the last word start nothing and count for
// nothing. lldb's positionals are words of the reading so far, and its -f
// value is in the reader's place (security, round 6, N3).
//
// The cap is asked before the compare: the reading that pushed goes on
// reading past the bound, and 8 KB of letters a table does not know is 4,096
// pushes, each of which has to stop here without a reflect.DeepEqual.
func (w *worklist) push(r resume) {
	if len(r.rest) == 0 && len(r.run.words) == 0 && r.at.target == "" || w.unread || w.made(r) {
		return
	}
	if len(w.pushed) == maxReadings {
		w.unread = true
		return
	}
	r.run = clipped(r.run)
	w.pushed = append(w.pushed, r)
	w.queue = append(w.queue, r)
}

// made reports a resume already pushed that is equal to r in every part: the
// reader and its place, the words left, and every field of the reading so
// far. Round 5 keyed a resume by four of its fields and lost `shell`, so in
// `strace --argv0 -o watch strace --argv0 X 'git commit -m x'` the reading
// through watch, which is the one that runs, was dropped as one already made
// (security, round 5, B2). Compared whole, a field added to invocation later
// is in the key with no edit here.
//
// reflect.DeepEqual tells a nil slice from an empty one. The readers leave a
// slice nil until they append to it, and clipping keeps it nil, so two resumes
// equal in meaning compare equal; a reader that made an empty slice that is
// not nil would make them differ, read more, and fail the frozen 16-`-J` rows
// of readings_test.go on the cap. evals and lldb's -f value are an integer and
// a string, with no such case.
func (w *worklist) made(r resume) bool {
	for _, p := range w.pushed {
		if p.table == r.table && len(p.rest) == len(r.rest) && reflect.DeepEqual(p, r) {
			return true
		}
	}
	return false
}

// clipped is the reading with each of its slices clipped to its length.
func clipped(run invocation) invocation {
	run.words = slices.Clip(run.words)
	run.assigns = slices.Clip(run.assigns)
	run.cut = slices.Clip(run.cut)
	run.unsets = slices.Clip(run.unsets)
	return run
}
