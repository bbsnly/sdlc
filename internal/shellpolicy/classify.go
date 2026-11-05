package shellpolicy

import "strings"

// The classes of command a shell trace record keeps (US-112, AC-8).
const (
	ClassTest  = "test"
	ClassBuild = "build"
	ClassLint  = "lint"
	ClassGit   = "git"
	ClassSdlc  = "sdlc"
	ClassOther = "other"
)

// maxProgram is the longest program name a trace record keeps.
const maxProgram = 64

// Classify is the class of a command and the name of the program it starts
// with, for the trace: the one word of a command the trace keeps. It decides
// nothing, and no rule reads it. configured is the project's own commands by
// the class each stands for ("test", "build" and "lint").
//
// The command read is the first one after any leading directory changes
// joined by && or ;, with its leading assignments and the shell's `time`
// (with `-p`) skipped: the runbook starts
// every command in a story's worktree with `cd -- '<tree>' &&`, and read
// literally every test run there was the program cd (a person's decision of
// 2026-10-28). A command the lexer cannot read whole is other, with no
// program. The program is kept only where it is a plain file name, so nothing
// the shell would expand, and no text a model wrote, reaches the trace.
func classifyOnce(command string, powerShell bool, configured map[string]string, reading assignments) (class, program string) {
	words, marks, ok := leadingCommand(command, powerShell, reading)
	if !ok {
		return ClassOther, ""
	}
	program = programName(marks[0])
	for _, c := range []string{ClassTest, ClassBuild, ClassLint} {
		if runsConfigured(words, configured[c], powerShell, reading) {
			return c, program
		}
	}
	switch base(words[0]) {
	case "git":
		return ClassGit, program
	case "sdlc":
		return ClassSdlc, program
	case "pytest":
		return ClassTest, program
	case "go", "cargo", "make":
		return byVerb(words[1:], false), program
	case "npm", "pnpm", "yarn":
		return byVerb(words[1:], true), program
	}
	return ClassOther, program
}

// leadingCommand is the words, and the words as marked, of the command
// Classify reads, or not ok where there is none or the line cannot be read.
func leadingCommand(command string, powerShell bool, reading assignments) (words, marks []string, ok bool) {
	l := lexUnder(command, powerShell, reading)
	if l.unclosed || l.subshells.unread != nil {
		return nil, nil, false
	}
	for i, w := range l.commands {
		if l.groups[i] != 0 {
			continue
		}
		m := l.marks[i]
		for len(w) > 0 && reading.is(w[0]) {
			w, m = w[1:], m[1:]
		}
		w, m = afterTime(w, m)
		if len(w) == 0 {
			continue
		}
		if cdPrograms[base(w[0])] && (l.joins[i] == "&&" || l.joins[i] == ";" || l.joins[i] == "\n") {
			continue
		}
		return w, m, true
	}
	return nil, nil, false
}

// afterTime is a command's words, and its marks, past the shell's time and
// its -p, where a command follows them. Read as the program, `time go test
// ./...` was other, and the trace counted no test run (code review, Gate 7).
func afterTime(words, marks []string) ([]string, []string) {
	if len(words) < 2 || words[0] != "time" {
		return words, marks
	}
	n := 1
	for n < len(words)-1 && (words[n] == "-p" || words[n] == "--") {
		n++
	}
	return words[n:], marks[n:]
}

// programName is the name a program word gives, or "" where it is not a plain
// file name: the shell makes it, or it holds anything but [a-z0-9._+-] once
// its directory and an .exe are taken off and it is folded.
func programName(mark string) string {
	if strings.ContainsAny(mark, "$`") {
		return ""
	}
	name := clean(mark)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	if name == "" || len(name) > maxProgram {
		return ""
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && !strings.ContainsRune("._+-", r) {
			return ""
		}
	}
	return name
}

// runsConfigured reports whether words start with the project's own command
// for a class, read the same way: the same program, then the same words.
func runsConfigured(words []string, configured string, powerShell bool, reading assignments) bool {
	if strings.TrimSpace(configured) == "" {
		return false
	}
	want, _, ok := leadingCommand(configured, powerShell, reading)
	if !ok || len(want) > len(words) || base(want[0]) != base(words[0]) {
		return false
	}
	for i := 1; i < len(want); i++ {
		if want[i] != words[i] {
			return false
		}
	}
	return true
}

// byVerb is the class a build tool's verb gives: the word after the program,
// with a Node tool's `run` taken off.
func byVerb(args []string, node bool) string {
	if node && len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	if len(args) == 0 {
		return ClassOther
	}
	switch args[0] {
	case "test":
		return ClassTest
	case "build":
		return ClassBuild
	}
	return ClassOther
}
