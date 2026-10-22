package cli

import (
	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// refuseInsideClaudeCode is SDLC-E0079 where the command runs in a process
// that carries any of Claude Code's markers, and nil elsewhere. slash is the
// slash command that stands for it, or "" for one that has none.
//
// The shell rules read a command line and miss what they cannot read: `arch`,
// `vim -c`, a script fed to a shell on standard input. Every one of those
// keeps the environment of the model's Bash that started it, so this is asked
// of the process itself. It reads through the command's getenv, never
// os.Getenv: a slash command runs in the hook's own process, which carries the
// markers too, and the hook hands it an environment without them.
func refuseInsideClaudeCode(cmd *cobra.Command, slash string) error {
	getenv := envOf(cmd).getenv
	found := ""
	for _, name := range store.ClaudeCodeMarkers() {
		if getenv(name) != "" {
			found = name
			break
		}
	}
	if found == "" {
		return nil
	}
	path := cmd.CommandPath()
	err := sdlcerr.New(sdlcerr.PersonsCommandInClaudeCode,
		quote(path)+" was run in a process Claude Code started, so nothing was read or changed",
		// By name only: the session id is a value of one of them.
		"the environment variable "+found+" is set here, and Claude Code sets it for the processes it starts: "+
			"a model's Bash or PowerShell and whatever they run, a `!` line typed in a session, and a terminal "+
			"or tmux it started; a person's decision comes from a slash command they type in Claude Code, or "+
			"from a terminal outside it")
	if slash == "" {
		return err.WithFix("a person runs " + quote(path) + withArguments(cmd, false) + " in a terminal outside " +
			"Claude Code, in a window they opened themselves; an assistant stops here and tells the person")
	}
	// The first argument is put in where it has the shape of an id: the story
	// for approve, remap and release, since after an escalation no story is
	// active and a bare slash command is refused with SDLC-E0011, and the
	// agent's name for staffing assign. Nothing else typed is, since a reason
	// is free text.
	typed, run, named := slash, path, false
	if words := cmd.Flags().Args(); len(words) > 0 && store.CheckID(words[0]) == nil {
		typed, run, named = slash+" "+words[0], path+" "+words[0], true
	}
	more, them := withArguments(cmd, named), ""
	if more != "" {
		them = " with them"
	}
	return err.WithFix("a person types " + quote(typed) + " in Claude Code" + more + ", or runs " + quote(run) + them +
		" in a terminal outside Claude Code, in a window they opened themselves; an assistant stops here and says " +
		"what it would have run")
}

// withArguments is how a fix says to give the command the rest of what was
// typed, or "" where nothing else was.
func withArguments(cmd *cobra.Command, named bool) string {
	rest := len(cmd.Flags().Args())
	if named {
		rest--
	}
	switch {
	case rest == 0 && cmd.Flags().NFlag() == 0:
		return ""
	case named:
		return " with the rest of the arguments"
	}
	return " with the same arguments"
}
