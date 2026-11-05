package hook

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// refusedOnce writes the line that a refused file's own values are in use once
// a process: the hook loads the file at several sites in one call, and a file
// left refused would otherwise grow a person's debug log by a line a site.
var refusedOnce sync.Once

// loadConfig is the configuration the hook's rules read. A file refused only
// for a setting the team replaced, or for its team section, was read, and its
// values are the project's own: a project that upgraded with a staffing
// section reaches this with no mistake of its own, possibly mid-story, and the
// defaults would drop its test paths, backlog, trunk, pause tiers and stop
// limit while a model works. Every command in a person's hands says what to
// fix, so the hook says nothing more to the session.
func loadConfig(root string) (config.Config, error) {
	cfg, setAside, err := config.ForRules(root)
	if err == nil && setAside != nil {
		refusedOnce.Do(func() {
			code := "unknown"
			if e, ok := errors.AsType[*sdlcerr.Error](setAside); ok {
				code = e.Code.String()
			}
			slog.Debug("hook: config refused (" + code + "); using the file's own values")
		})
	}
	return cfg, err
}
