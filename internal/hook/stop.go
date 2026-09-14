package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// turnReply is what Claude Code reads from a Stop or PostToolUse hook. Decision
// "block" sends the session back to work, and Reason is what it is told.
type turnReply struct {
	Continue      bool   `json:"continue"`
	Decision      string `json:"decision,omitempty"`
	Reason        string `json:"reason,omitempty"`
	SystemMessage string `json:"systemMessage,omitempty"`
}

// decideStop is the stop guard.
//
// A session that ends its turn mid-story -- without finishing the gate, handing
// the story to a person, or ending the iteration -- leaves the story where
// nobody is looking: the next session may not come, and nothing says this one
// gave up. So the stop is sent back once, with what to do instead.
//
// A stop already sent back goes through, so the session is never held in a
// loop. The count is per story and starts again whenever anything is recorded
// on it, and when it reaches loop.max_stop_blocks the story goes to a person.
// Like every other rule here it fails open: anything it cannot read lets the
// stop through.
func decideStop(raw []byte, getenv func(string) string, warn func(string)) turnReply {
	allow := turnReply{Continue: true}
	if getenv("SDLC_ENFORCE") == "0" {
		return allow
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return allow
	}
	project := getenv("CLAUDE_PROJECT_DIR")
	if project == "" {
		project = p.CWD
	}
	if project == "" {
		return allow
	}
	project, ok := projectRoot(project)
	if !ok {
		return allow
	}
	story := activeStory(project, warn)
	if story == "" || p.StopHookActive {
		return allow
	}

	cfg, err := config.Load(project)
	if err != nil {
		cfg = config.Default()
	}
	limit := cfg.Loop.MaxStopBlocks
	if limit <= 0 {
		return allow
	}
	s := store.New(&config.Project{Root: project, Config: cfg})
	progress, err := s.RecordProgress(story)
	if err != nil {
		slog.Debug("stop guard could not read the record", "err", err)
		return allow
	}
	count := s.StopCount()
	if count.Story != story || count.Progress != progress {
		count = store.StopCount{Story: story, Progress: progress}
	}
	if count.Blocks >= limit {
		return handOver(s, story, count.Blocks)
	}
	count.Blocks++
	if err := s.SaveStopCount(count); err != nil {
		slog.Debug("stop guard could not keep its count", "err", err)
		return allow
	}
	return turnReply{Continue: true, Decision: "block", Reason: stopReason(project, story, count.Blocks, limit)}
}

// stopReason says what to do instead of stopping, naming the gate to work.
func stopReason(project, story string, blocks, limit int) string {
	next := "the next gate"
	if raw, err := os.ReadFile(filepath.Join(project, ".sdlc", "stories", story, model.RecordFile)); err == nil {
		var record model.Record
		if json.Unmarshal(raw, &record) == nil {
			gate, remaining := record.NextGate()
			if !remaining {
				return fmt.Sprintf("every gate on %s has passed and the iteration is still open: "+
					"end it with `sdlc stop`.", story)
			}
			next = string(gate)
		}
	}
	return fmt.Sprintf("%s is still being worked on. Work %s and record it; hand the story to a "+
		"person with `sdlc escalate <type> --message \"...\"` if it needs one; or end the iteration "+
		"with `sdlc stop`. A story left mid-gate is one nobody is looking at, so after %d stops in a "+
		"row with nothing recorded the loop hands it to a person itself (this was %d).",
		story, next, limit, blocks)
}

// handOver gives a story that keeps stopping to a person, and lets the stop
// through. Holding the session any longer would not change what it does.
func handOver(s *store.Store, story string, blocks int) turnReply {
	reply := turnReply{Continue: true}
	failed := "sdlc: " + story + " kept stopping with nothing recorded, and could not be handed " +
		"to a person. Run `sdlc doctor` to see why."
	g, err := store.Lock(s.Root(), "hook Stop")
	if err != nil {
		slog.Debug("stop guard could not take the lock", "err", err)
		reply.SystemMessage = failed
		return reply
	}
	defer g.Release()

	ctx, cancel := context.WithTimeout(context.Background(), treeTimeout)
	defer cancel()
	question := fmt.Sprintf("the session stopped %d times in a row without recording anything on "+
		"the story. Read .sdlc/stories/%s/gate-record.json for where it got to, and answer with "+
		"`sdlc approve %s` to let the loop carry on.", blocks+1, story, story)
	if _, err := s.Escalate(ctx, story, "loop_stalled", question); err != nil {
		slog.Debug("stop guard could not escalate", "err", err)
		reply.SystemMessage = failed
		return reply
	}
	if err := s.ClearStopCount(); err != nil {
		slog.Debug("stop guard could not clear its count", "err", err)
	}
	reply.SystemMessage = fmt.Sprintf("sdlc: %s stopped %d times in a row with nothing recorded, so "+
		"it has been handed to a person and the iteration has ended. `sdlc status` shows the question; "+
		"answer it with `sdlc approve %s`.", story, blocks+1, story)
	return reply
}
