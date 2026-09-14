package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// formatTimeout bounds one run of commands.fmt_file. It is inside the hook's own
// timeout in hooks.json, so a formatter that hangs is reported rather than
// killed along with the hook.
const formatTimeout = 25 * time.Second

// formatOutputLimit is how much of a failing formatter's output goes back to
// the session: enough to see the error, not a page of it.
const formatOutputLimit = 2000

// formatWritten runs commands.fmt_file on a file a tool has just written.
//
// The file is already written, so nothing here refuses anything. A formatter
// that fails is reported to the session as the reason it is sent back to work:
// the file is not in the shape the project keeps, and the formatter's own words
// say why. Everything else fails open, like the rest of the hook.
func formatWritten(raw []byte, getenv func(string) string, warn func(string)) turnReply {
	reply := turnReply{Continue: true}
	if getenv("SDLC_ENFORCE") == "0" {
		return reply
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return reply
	}
	project := getenv("CLAUDE_PROJECT_DIR")
	if project == "" {
		project = p.CWD
	}
	if project == "" || p.ToolInput.FilePath == "" {
		return reply
	}
	project, ok := projectRoot(project)
	if !ok || activeStory(project, warn) == "" {
		return reply
	}
	rel, outside := pathrules.Rel(project, p.ToolInput.FilePath)
	if outside || rel == "" || strings.HasPrefix(strings.ToLower(rel), config.Dir+"/") {
		return reply
	}
	cfg, err := config.Load(project)
	if err != nil {
		return reply
	}
	command := strings.TrimSpace(cfg.Commands["fmt_file"])
	if command == "" {
		return reply
	}

	shell, err := findShell()
	if err != nil {
		reply.SystemMessage = "sdlc: commands.fmt_file was not run on " + rel +
			", because neither sh nor bash is on PATH."
		return reply
	}
	ctx, cancel := context.WithTimeout(context.Background(), formatTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-c", command)
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "FILE="+p.ToolInput.FilePath)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	slog.Debug("hook formatted a file", "file", rel, "err", err)
	if err == nil {
		return reply
	}

	said := strings.TrimSpace(out.String())
	if len(said) > formatOutputLimit {
		said = "..." + said[len(said)-formatOutputLimit:]
	}
	why := "it exited with " + err.Error()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		why = "it did not finish in " + formatTimeout.String()
	}
	reason := "commands.fmt_file failed on " + rel + ": " + why + ", so the file is as it was " +
		"written and not formatted. Fix what the formatter reports; if the command itself is " +
		"wrong, say so rather than editing .sdlc/config.json."
	if said != "" {
		reason += "\n\n" + said
	}
	reply.Decision, reply.Reason = "block", reason
	return reply
}

// findShell is what runs a configured command. The commands are written for a
// POSIX shell, and Claude Code on Windows runs under Git Bash, which has both.
func findShell() (string, error) {
	for _, name := range []string{"sh", "bash"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}
