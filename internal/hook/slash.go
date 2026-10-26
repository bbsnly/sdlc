package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/store"
)

// Runner runs one sdlc command, as a person at a terminal in dir would. It has
// the signature of cli.ExecuteIn, which cmd/sdlc hands over: hook may not
// import cli.
type Runner func(dir string, getenv func(string) string, args []string,
	stdin io.Reader, stdout, stderr io.Writer) int

// RunWith is Run with a way to run the person's own commands, which a
// UserPromptExpansion answers by running.
func RunWith(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string,
	commands Runner) int {
	// Routed on the event word Claude Code passes, and kept so: the shell
	// rule that refuses a model running this by hand
	// (internal/shellpolicy/forgedhook.go) reads that word alone, and a route
	// taken from the payload's hook_event_name would get past it.
	if len(args) > 0 && args[0] == "UserPromptExpansion" {
		return answerExpansion(stdin, stdout, getenv, commands)
	}
	return runOnTheClock(args, stdin, stdout, stderr, getenv)
}

// typedCommand is the sdlc command a slash command stands for, by the name the
// skill has. Each of a person's decisions is here, and init and doctor, which
// a person in Claude Desktop has no terminal to run either.
func typedCommand(name string) []string {
	switch name {
	case "approve", "unfreeze", "ack", "remap", "release", "init", "doctor":
		return []string{name}
	case "assign":
		return []string{"staffing", "assign"}
	}
	return nil
}

// terminalForm is how the slash command is run in a terminal, for the words
// that send a person there.
func terminalForm(name string) string {
	return "sdlc " + strings.Join(typedCommand(name), " ")
}

// expansion is the part of a UserPromptExpansion payload this needs.
type expansion struct {
	HookEventName string `json:"hook_event_name"`
	ExpansionType string `json:"expansion_type"`
	CommandName   string `json:"command_name"`
	CommandArgs   string `json:"command_args"`
	CommandSource string `json:"command_source"`
	AgentType     string `json:"agent_type"`
	Prompt        string `json:"prompt"`
	CWD           string `json:"cwd"`
}

// typed is the skill name the person typed, where the expansion is one of this
// plugin's slash commands typed by a person, and "" for anything else.
//
// Claude Code raises the event for a command a person typed and never for one
// the model starts with its Skill tool. Everything else that raises it -- an
// MCP prompt, a project's or a user's command of the same name, another
// plugin's, a subagent's -- is let through untouched: running the command for
// one of those would be recording a decision nobody made.
func (e expansion) typed() string {
	if e.HookEventName != "UserPromptExpansion" || e.ExpansionType != "slash_command" ||
		e.CommandSource != "plugin" || e.AgentType != "" {
		return ""
	}
	if name, ok := strings.CutPrefix(e.CommandName, "sdlc:"); ok {
		if typedCommand(name) != nil {
			return name
		}
		return ""
	}
	// Claude Code may name a plugin's command without its plugin where no other
	// command has the name. The line typed then says whose it was: without that,
	// another plugin's approve would run this one's.
	if typedCommand(e.CommandName) == nil {
		return ""
	}
	if first, _, _ := strings.Cut(strings.TrimSpace(e.Prompt), " "); first != "/sdlc:"+e.CommandName {
		return ""
	}
	return e.CommandName
}

// expansionAnswer is the answer to a slash command the hook handled. A block
// stops the expansion, so the model never reads the skill, whose body says the
// command did not run; continue stays true, because the session goes on.
type expansionAnswer struct {
	Continue bool   `json:"continue"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func shown(reason string) expansionAnswer {
	return expansionAnswer{Continue: true, Decision: blocked, Reason: reason}
}

// answerExpansion answers UserPromptExpansion. A slash command of this plugin's,
// typed by a person, is run as the terminal command it stands for, and the
// person is shown what that printed; anything else is let through.
//
// SDLC_ENFORCE=0 does not stop it: these are the person's own commands, which a
// terminal runs with enforcement off too.
//
// This is the one place the hook answers its own failure with a block. A
// command that gave way, or arguments that could not be read, is a decision
// that was not made, and the person is told so and what to do. Let through,
// the expansion would reach the model, which a person typing a decision has
// no reason to wait on.
func answerExpansion(stdin io.Reader, stdout io.Writer, getenv func(string) string, commands Runner) int {
	raw, _ := io.ReadAll(io.LimitReader(stdin, maxPayload+1))
	var e expansion
	if len(raw) > maxPayload || json.Unmarshal(raw, &e) != nil {
		// There is no telling whose it was. Let through, the skill body says
		// the command did not run.
		slog.Debug("hook: the expansion could not be read, so nothing was run", "why", unreadableBecause(raw))
		return emit(stdout, Allow())
	}
	name := e.typed()
	if name == "" {
		slog.Debug("hook: the expansion is not one of the plugin's commands typed by a person",
			"command", e.CommandName, "type", e.ExpansionType, "source", e.CommandSource, "agent", e.AgentType)
		return emit(stdout, Allow())
	}
	return emit(stdout, runTyped(e, name, getenv, commands))
}

// runTyped runs the command the person typed and says what came of it.
func runTyped(e expansion, name string, getenv func(string) string, commands Runner) expansionAnswer {
	slash := "/sdlc:" + name
	// The hook's own environment, before anything typed is read. Claude Code
	// sets AI_AGENT for a model's tools and never for a hook it runs itself, so
	// a hook that has it is a session started from a model's tool call, or a
	// model's shell running the hook on a payload it wrote: no person typed
	// this. init and doctor are no decision, and a model may run both.
	if getenv(store.AgentEnv) != "" && name != "init" && name != "doctor" {
		// By name, never its value, as SDLC-E0079's why does.
		slog.Info("hook: a person's slash command was not run, since the hook's environment has a model's tool variable",
			"command", slash, "variable", store.AgentEnv)
		return shown(slash + " did not run: the environment variable " + store.AgentEnv + " is set where this " +
			"hook runs, which Claude Code does for a session started from another Claude Code session's tool " +
			"call, and a command typed there is not a person's decision. Type " + slash + " again, with the " +
			"same arguments, in a Claude Code session you opened yourself, or run `" + terminalForm(name) +
			"` with the same arguments in a terminal outside Claude Code. If you did open this session " +
			"yourself, the terminal or app you started it from was started inside Claude Code: quit that app " +
			"fully, open it again, and start Claude Code from it.")
	}
	words, unread := splitTyped(e.CommandArgs)
	if unread != "" {
		slog.Debug("hook: the slash command's arguments could not be read", "command", slash, "why", unread)
		return shown(unread + ", so " + slash + " did not run. Type it again with the quote closed.")
	}
	dir := e.CWD
	if dir == "" {
		dir = getenv("CLAUDE_PROJECT_DIR")
	}
	if dir == "" {
		slog.Debug("hook: the slash command names no directory to run in", "command", slash)
		return shown(slash + " did not run: the session has no project folder for it to act on. " +
			"Open the project's folder in a new session and type it there.")
	}
	if commands == nil {
		slog.Debug("hook: no way to run the slash command was handed over", "command", slash)
		return shown(notRun(name, "this sdlc cannot run a slash command"))
	}
	// A terminal outside Claude Code has none of Claude Code's variables, and a
	// person's command is a terminal's: with the hook's own the CLI would
	// refuse every person's command with SDLC-E0079, `staffing assign` with
	// SDLC-E0074, and a decision from a session the story is not held in.
	markers := store.ClaudeCodeMarkers()
	env := func(key string) string {
		if slices.Contains(markers[:], key) {
			return ""
		}
		return getenv(key)
	}
	var out, errOut bytes.Buffer
	// Written before the run as well as after: a command killed at the hook's
	// timeout leaves this line alone, which says it started.
	slog.Debug("hook: running the slash command", "command", slash, "dir", dir)
	code, ran := runGuarded(commands, dir, env, append(typedCommand(name), words...), &out, &errOut)
	if !ran {
		return shown(notRun(name, "sdlc gave way while running it, and the fault is sdlc's, not what you typed"))
	}
	slog.Debug("hook: ran the slash command", "command", slash, "exit", code)
	return shown(printed(slash, out.String(), errOut.String(), code))
}

// runGuarded runs the command, and ran is false where it gave way. The recover
// covers the command alone: a command that returned ran, and nothing after it
// may say otherwise.
func runGuarded(commands Runner, dir string, getenv func(string) string, args []string,
	stdout, stderr io.Writer) (code int, ran bool) {
	defer func() {
		if r := recover(); r != nil {
			// At error level, so that it reaches a log without debugging on.
			// The panic is never shown: it can quote anything the command read.
			slog.Error("hook: the slash command gave way", append([]any{"command", args[0]}, append(panicked(r), "stack", string(debug.Stack()))...)...)
			code, ran = 0, false
		}
	}()
	return commands(dir, getenv, args, strings.NewReader(""), stdout, stderr), true
}

// notRun is what a person is told when the command they typed did not run:
// that, why, and what to do, in Claude Code first and in a terminal second.
func notRun(name, why string) string {
	slash := "/sdlc:" + name
	return slash + " did not run: " + why + ". Type it again. If it fails the same way, " +
		"type /sdlc:doctor, or run `sdlc doctor` in a terminal, to see what is wrong, or run `" +
		terminalForm(name) + " ...` in your own terminal. To report it, set SDLC_DEBUG_FILE to a " +
		"file's path in Claude Desktop's local environment editor, or in the terminal Claude Code " +
		"starts from, type the command again, and send that file."
}

// printed is what the command printed, as a terminal shows it: its output,
// then its errors.
func printed(slash, stdout, stderr string, code int) string {
	var parts []string
	for _, s := range []string{stdout, stderr} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s finished and printed nothing (exit code %d).", slash, code)
	}
	if code != 0 {
		// A refusal reads as its SDLC-E code says, but a long one can be read
		// as done.
		parts = append(parts, fmt.Sprintf("%s ended with exit code %d.", slash, code))
	}
	return strings.Join(parts, "\n")
}

// splitTyped splits the arguments typed after a slash command into words as a
// POSIX shell would, and does nothing else a shell does: nothing in them is
// run, expanded or redirected, so `;`, `|`, `$(...)`, `~` and `*` are only
// letters. Single quotes are literal; in double quotes a backslash escapes only
// `"` and itself; outside them it escapes the next character. A quote that is
// never closed is a line that cannot be read, and why says so.
func splitTyped(s string) (words []string, why string) {
	var word strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '\t', '\n', '\r':
			if inWord {
				words, inWord = append(words, word.String()), false
				word.Reset()
			}
			continue
		case '\'', '"':
			end, why := quoted(s, i, &word)
			if why != "" {
				return nil, why
			}
			i = end
		case '\\':
			if i+1 < len(s) {
				i++
			}
			word.WriteByte(s[i])
		default:
			word.WriteByte(c)
		}
		inWord = true
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, ""
}

// quoted writes the text of the quoted part of a word that opens at i to word,
// and returns where its closing quote is, or why it cannot be read.
func quoted(s string, i int, word *strings.Builder) (end int, why string) {
	if s[i] == '\'' {
		end := strings.IndexByte(s[i+1:], '\'')
		if end < 0 {
			return 0, "the single quote opened in what you typed is never closed"
		}
		word.WriteString(s[i+1 : i+1+end])
		return i + 1 + end, ""
	}
	end, closed := doubleQuoted(s, i+1, word)
	if !closed {
		return 0, "the double quote opened in what you typed is never closed"
	}
	return end, ""
}

// doubleQuoted writes the text of a double-quoted part of a word, from just
// after its opening quote at from, to word, and returns where its closing quote
// is. A backslash there escapes only `"` and itself.
func doubleQuoted(s string, from int, word *strings.Builder) (end int, closed bool) {
	for i := from; i < len(s); i++ {
		switch {
		case s[i] == '"':
			return i, true
		case s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\'):
			i++
		}
		word.WriteByte(s[i])
	}
	return len(s), false
}
