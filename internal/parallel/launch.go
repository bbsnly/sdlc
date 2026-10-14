// Package parallel is the part of `sdlc parallel` that is not about a
// project's files: the tools an unattended session is given, the one command
// line that starts a session, the scheduler that bounds how many run at once,
// and what a finished session reads as. The command itself, which reads the
// project, is in internal/cli.
package parallel

import (
	"context"
	"os/exec"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellx"
)

// Allowlist is what `sdlc parallel --unattended` pre-approves for a session.
// It is a fresh slice on every call, and built here rather than held in a
// package variable so that nothing is built on the latency path of every tool
// call in a user's session.
//
// Short and scoped, with no network tool, no push, no `claude`, and none of
// the commands that are a person's decision (approve, unfreeze, ack, release,
// parallel) or that end a story (stop). The hook still decides first, since a
// deny beats an allow, but not before the session has bound itself to a story,
// so nothing on the list is destructive on its own.
//
// Two groups are more than they look. The build and test commands run code the
// session has just written, with the person's privileges: that is the work, and
// it is why the list names the toolchains `sdlc init` recognises and no runner
// (`go run`, `npm run`, an interpreter, `env`, `xargs`) that would run
// anything at all. Writes are scoped to the worktrees under the main checkout,
// which is where each story is worked, so the checkout's own files and the rest
// of the machine are outside them. `git diff`, `log` and `show` are left out:
// each takes an --output that writes a file wherever it is told.
//
// A project whose build is none of these runs headless and stops at the first
// tool it was not given. Widening the list is out of scope.
func Allowlist() []string {
	return []string{
		"Read",
		"Edit(.sdlc/worktrees/**)",
		"Write(.sdlc/worktrees/**)",
		"Bash(cd:*)",
		"Bash(sdlc start:*)",
		"Bash(sdlc status:*)",
		"Bash(sdlc gate:*)",
		"Bash(sdlc artifact:*)",
		// The bookkeeper stores the story's sections of CODEMAP.md, or every
		// unattended story would stall at its retro. The group has that one
		// subcommand: another added to it is pre-approved by this entry.
		"Bash(sdlc codemap:*)",
		"Bash(sdlc review:*)",
		// One by one: `sdlc staffing:*` would pre-approve assign, a person's.
		"Bash(sdlc staffing set:*)",
		"Bash(sdlc staffing amend:*)",
		"Bash(sdlc staffing show:*)",
		"Bash(sdlc escalate:*)",
		"Bash(sdlc freeze:*)",
		"Bash(sdlc story:*)",
		"Bash(sdlc cost:*)",
		"Bash(git status:*)",
		"Bash(git add:*)",
		"Bash(go build:*)",
		"Bash(go test:*)",
		"Bash(go vet:*)",
		"Bash(cargo build:*)",
		"Bash(cargo test:*)",
		"Bash(npm test:*)",
		"Bash(pytest:*)",
	}
}

// Narrow is the list a session is given when the configuration names one:
// the built-in entries it names, in the built-in order, each once. A key left
// out gives the whole list and an empty one gives none. An entry that is not
// built in is refused, exactly: the configuration is writable by a session on
// the project, so it may take tools away but never hand one out.
func Narrow(configured *[]string) ([]string, error) {
	built := Allowlist()
	if configured == nil {
		return built, nil
	}
	for _, e := range *configured {
		if !slices.Contains(built, e) {
			return nil, sdlcerr.New(sdlcerr.ParallelAllowlistWidened,
				"parallel.allow names "+Quote(e)+", which is not in the built-in list",
				"the setting can only take tools away from the list a session is given, never add one")
		}
	}
	return slices.DeleteFunc(built, func(e string) bool { return !slices.Contains(*configured, e) }), nil
}

// Prompt is what the session is asked to do: the loop's skill, for one story.
func Prompt(id string) string { return "/sdlc:next " + id }

// Quote is word as one shell word, whatever it holds. It is not the cli's
// shellWord, which turns what it cannot print into "?" and so cannot round-trip.
func Quote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// Command is the headless session for one story, run from dir through the
// shell like every command the tool starts, and in a group so that an
// interrupt ends everything the session started. Its input is the null device
// and its environment is the person's as it is. With an empty allow nothing is
// pre-approved, and in no case is the permission prompt turned off wholesale.
//
// The prompt goes last: --allowedTools takes every word that follows it, so a
// prompt after the list would be read as one more tool.
func Command(ctx context.Context, dir, id string, allow []string) (*exec.Cmd, error) {
	words := []string{"claude"}
	if len(allow) > 0 {
		words = append(words, "--allowedTools")
		words = append(words, quoteAll(allow)...)
	}
	words = append(words, "-p", Quote(Prompt(id)))
	cmd, err := shellx.Command(ctx, dir, strings.Join(words, " "))
	if err != nil {
		return nil, err
	}
	shellx.InGroup(cmd)
	return cmd, nil
}

func quoteAll(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = Quote(w)
	}
	return out
}
