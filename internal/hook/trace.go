package hook

// What the hook records about the story a session holds (US-112): the two
// agent events on the gate record, and everything else as a line of the
// story's trace.jsonl. Recording never changes an answer. It runs after the
// answer is on stdout, under a recover of its own, and anything it cannot do
// is a line in the debug log.
//
// Every name of the host's that recording needs sits in this file: the events
// it records, the payload's fields, the hand-back tool, the `Exit code N` text,
// the notification types, the StopFailure errors and the compaction triggers.
// The frozen tests drive hook.Run, which has no host, and expect each of them
// recorded, so they cannot move to the host's edge without a seam those tests
// do not have (a person's decision of 2026-10-29). Kept here, they can be
// lifted out as one file.

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/policy"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/trace"
)

// Transcripts reads a host's transcript at path from the byte offset from: the
// counts of what it holds, and the offset after the last whole line read. A
// read that gives no counts gives an error whose class says why (see
// claudecode.ReadTranscript).
type Transcripts func(ctx context.Context, path string, from int64) (trace.Usage, int64, error)

// Host is what cmd/sdlc hands the hook from the host's edge: a way to run a
// person's own commands, and a reader of the host's transcripts. Either may be
// nil, and the hook then does without.
type Host struct {
	Commands    Runner
	Transcripts Transcripts
}

const (
	// transcriptTime is the most a transcript read is given. A hook that
	// Claude Code kills at its timeout records nothing more, and the reader is
	// at about 3 ms a MiB, so five seconds is far past any span it reads.
	transcriptTime = 5 * time.Second
	// idMax is the longest agent type or id kept; labelMax the longest effort,
	// permission mode or background task type; promptMax the longest prompt id.
	idMax     = 128
	labelMax  = 32
	promptMax = 64
	// The most entries a record keeps of each count, `other` among them. At
	// these an agent_run whose every name is as long as may be kept is still
	// under the trace's 4,096 bytes a line.
	toolCap  = 16
	modelCap = 4
	taskCap  = 8
	// markCap is the most transcripts the turn mark remembers.
	markCap = 16
	// handBackTool is the tool a subagent hands its report back through.
	handBackTool = "SubagentHandback"
)

// errReaderPanicked is a transcript reader that panicked.
var errReaderPanicked = errors.New("the transcript reader panicked")

// The tool events recording reads, and a test run's outcome where its exit
// cannot be read.
const (
	eventStop       = "Stop"
	eventToolUsed   = "PostToolUse"
	eventToolFailed = "PostToolUseFailure"
	outcomeUnknown  = "unknown"
	// keyKind is the key every trace record names its kind by.
	keyKind = "kind"
)

// RunHosted is Run with the host's edge: cmd/sdlc's way to run a person's
// commands, and its transcript reader.
func RunHosted(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, h Host) int {
	// Routed on the event word Claude Code passes, and kept so: the shell
	// rule that refuses a model running this by hand
	// (internal/shellpolicy/forgedhook.go) reads that word alone, and a route
	// taken from the payload's hook_event_name would get past it.
	if len(args) > 0 && args[0] == "UserPromptExpansion" {
		return answerExpansion(stdin, stdout, getenv, h.Commands)
	}
	if len(args) == 0 || !recordsEvent(args[0]) {
		return runOnTheClock(args, stdin, stdout, stderr, getenv)
	}
	// run reads the payload before anything else, so a copy taken as it reads
	// costs the deadline nothing.
	var kept bytes.Buffer
	code := runOnTheClock(args, io.TeeReader(stdin, &kept), stdout, stderr, getenv)
	noteEvent(args[0], kept.Bytes(), getenv, h.Transcripts)
	return code
}

// recordsOnly reports whether the hook only records an event, and answers it
// with carry-on and nothing more. A PostToolUseFailure was judged as a
// PreToolUse before it was named here, so a failed command the rules refuse
// came back as a deny (THREATS E2).
func recordsOnly(event string) bool {
	switch event {
	case "SubagentStart", "SubagentStop", "Notification", "StopFailure", "PostCompact", eventToolFailed:
		return true
	}
	return false
}

// recordsEvent reports whether anything is recorded on the event, after the
// answer it gives.
func recordsEvent(event string) bool {
	return recordsOnly(event) || event == eventStop || event == eventToolUsed
}

// notePayload is the part of an event recording reads. effort, the hand-back's
// message and the background tasks are read apart, so that one of a shape
// nobody expected costs that field and not the record.
type notePayload struct {
	ToolName             string          `json:"tool_name"`
	AgentType            string          `json:"agent_type"`
	AgentID              string          `json:"agent_id"`
	SessionID            string          `json:"session_id"`
	CWD                  string          `json:"cwd"`
	AgentTranscriptPath  string          `json:"agent_transcript_path"`
	TranscriptPath       string          `json:"transcript_path"`
	PermissionMode       string          `json:"permission_mode"`
	PromptID             string          `json:"prompt_id"`
	Effort               json.RawMessage `json:"effort"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	// DurationMS is a pointer, because a payload that gives none is not one
	// that took no time.
	DurationMS       *float64        `json:"duration_ms"`
	Error            string          `json:"error"`
	IsInterrupt      bool            `json:"is_interrupt"`
	NotificationType string          `json:"notification_type"`
	Trigger          string          `json:"trigger"`
	BackgroundTasks  json.RawMessage `json:"background_tasks"`
	ToolInput        struct {
		Command         string          `json:"command"`
		RunInBackground bool            `json:"run_in_background"`
		Message         json.RawMessage `json:"message"`
	} `json:"tool_input"`
}

// held is the story a session holds, where its trace is written.
type held struct {
	project, story, dir string
	s                   *store.Store
	cfg                 config.Config
}

// noteEvent records one event the hook has answered. Nothing it does can
// reach the answer: it runs after it, and a panic here is recovered and
// logged rather than reaching cmd/sdlc's recover, which would write a second.
func noteEvent(event string, raw []byte, getenv func(string) string, read Transcripts) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("trace: recording gave way, and nothing more is recorded for this event",
				append([]any{"event", event}, panicked(r)...)...)
		}
	}()
	// Whatever AI_AGENT holds: Claude Code 2.1.309 in Claude Desktop
	// (entrypoint claude-desktop, seen on 2026-10-29) sets it for its own
	// hooks too, and a check here recorded nothing there (the person's
	// decision of 2026-10-29). A model's own run of the hook on a payload it
	// wrote is the shell rules' to refuse (THREATS S1).
	var p notePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		slog.Debug("trace: not recorded: the event could not be read", "event", event)
		return
	}
	if (event == eventToolUsed || event == eventToolFailed) && !shellpolicy.Tools[p.ToolName] &&
		p.ToolName != handBackTool {
		// Write, Edit and the rest record nothing, and are most of a
		// session's calls: asked after findLoop, config.Load and the store,
		// each one paid for all three.
		return
	}
	h, ok := heldFor(getenv, p)
	if !ok {
		slog.Debug("trace: not recorded: no story is held by this session here", "event", event)
		return
	}
	switch event {
	case "SubagentStart":
		h.agentStarted(p)
	case "SubagentStop":
		h.agentStopped(p, read)
	case eventToolUsed, eventToolFailed:
		h.toolUsed(event, p)
	case eventStop:
		h.turn(p, read)
	case "Notification":
		h.notified(p)
	case "StopFailure":
		h.add(trace.Record{keyKind: "api_error", trace.KeyAgentType: label(p.AgentType, idMax), "error": apiError(p.Error)})
	case "PostCompact":
		h.add(trace.Record{keyKind: "compaction", trace.KeyAgentType: label(p.AgentType, idMax), "trigger": compactTrigger(p.Trigger)})
	}
}

// heldFor is the story the session holds where the event was made, found as
// the rules find it. The command of a shell event is left out: where a command
// ran is not where it is recorded.
func heldFor(getenv func(string) string, p notePayload) (*held, bool) {
	project, story := findLoop(getenv, payload{SessionID: p.SessionID, CWD: p.CWD}, "", func(string) {})
	if story == "" {
		return nil, false
	}
	cfg, err := config.Load(project)
	if err != nil {
		cfg = config.Default()
	}
	pl := placeOf(project)
	s := store.NewIn(&config.Project{Root: project, Config: cfg}, pl.co, pl.err)
	dir, err := s.StoryDir(story)
	if err != nil {
		return nil, false
	}
	return &held{project: project, story: story, dir: dir, s: s, cfg: cfg}, true
}

// refusalNote is how decide's caller records a refusal of the call p, in the
// story held in project, or nil where nothing is to be recorded. Everything it
// needs is settled here, on the decision's own path: after a refusal the only
// work left is one append, since a hook that Claude Code kills at its timeout
// lets the call through even with a deny on stdout.
func refusalNote(pl place, story string, p payload) func(policy.Verdict) {
	dir, err := store.NewIn(&config.Project{Root: pl.project}, pl.co, pl.err).StoryDir(story)
	if err != nil {
		return nil
	}
	h := &held{project: pl.project, story: story, dir: dir}
	return func(v policy.Verdict) {
		// Its own recover: run has answered by now, and a panic that reached
		// cmd/sdlc's would write a second answer.
		defer func() {
			if r := recover(); r != nil {
				slog.Debug("trace: recording a refusal gave way", panicked(r)...)
			}
		}()
		h.add(trace.Record{
			keyKind: "refusal", "rule": v.Rule, "code": v.Code,
			"tool": boundedName(p.ToolName), trace.KeyAgentType: label(p.AgentType, idMax),
		})
	}
}

// add appends r to the story's trace, leaving out every key whose value is
// the empty string, and reports whether the line was written. It is the one
// way a record is written.
func (h *held) add(r trace.Record) bool {
	for k, v := range r {
		if s, ok := v.(string); ok && s == "" {
			delete(r, k)
		}
	}
	err := trace.Append(h.dir, r)
	switch {
	case err == nil:
		return true
	case errors.Is(err, trace.ErrFull):
		// Said once, by the record that filled it.
	case errors.Is(err, trace.ErrTruncated):
		slog.Debug("trace: the trace reached 4 MiB; nothing more is recorded for this story")
	default:
		// The writer's errors carry no path (THREATS I4).
		slog.Debug("trace: a record was not written", "kind", r[keyKind], "why", err.Error())
	}
	return false
}

// ------------------------------------------------------------------ agents

// agentStarted puts agent_start on the gate record.
func (h *held) agentStarted(p notePayload) {
	if id := label(p.AgentID, idMax); id != "" {
		h.agentEvent(model.EventAgentStart, label(p.AgentType, idMax), id)
		return
	}
	slog.Debug("trace: an agent's start was not recorded: its id is missing or is not a name")
}

// agentStopped puts agent_end on the gate record, and then, with the store's
// lock let go, records the run: its counts from the transcript, and then its
// status, read after the transcript so that a hand-back delivered while the
// transcript was read is seen (red team, V7).
func (h *held) agentStopped(p notePayload, read Transcripts) {
	id := label(p.AgentID, idMax)
	if id == "" {
		slog.Debug("trace: an agent's end was not recorded: its id is missing or is not a name")
		return
	}
	agentType := label(p.AgentType, idMax)
	record := h.agentEvent(model.EventAgentEnd, agentType, id)
	r := trace.Record{
		keyKind: trace.KindAgentRun, trace.KeyAgentType: agentType, "agent_id": id, "end": trace.Stamp(time.Now()),
		"effort": label(effortOf(p.Effort), labelMax), "permission_mode": label(p.PermissionMode, labelMax),
	}
	if record != nil {
		r["start"] = startOf(record, id)
		r["asked_model"], r["asked_effort"] = askedFor(record, p.AgentType)
	}
	if read != nil && p.AgentTranscriptPath != "" {
		if u, _, err := countsFrom(read, "agent", p.AgentTranscriptPath, 0); err == nil {
			putCounts(r, u)
		}
	}
	r["status"] = cmp.Or(h.handedBack(id), handBackStatus(p.LastAssistantMessage))
	h.add(r)
}

// agentEvent puts one agent event on the gate record under the store's lock,
// the one decideStop takes, and returns the record as it was saved, or nil
// where it was not. Nothing else is done while the lock is held: five
// reviewers stopping at once wait for each other's few milliseconds, never for
// a transcript (O4).
//
// A subagent starting or stopping is not progress to the stop guard. Its
// count is carried over the save, so that a session that kept starting
// subagents is still handed to a person (THREATS T5).
func (h *held) agentEvent(typ, agentType, id string) *model.Record {
	g, err := store.Lock(h.s.LockRoot(), "hook "+typ)
	if err != nil {
		// The lock's own error names its path.
		slog.Debug("trace: an agent event was not recorded: the store's lock was not got", "type", typ, "agent_id", id)
		return nil
	}
	defer g.Release()
	before, beforeErr := h.s.RecordProgress(h.story)
	record, err := h.s.Record(h.story)
	if err == nil {
		record.Events = append(record.Events, model.Event{
			At: model.Timestamp(time.Now()), Type: typ, AgentType: agentType, AgentID: id,
		})
		err = h.s.SaveRecord(record)
	}
	if err != nil {
		slog.Debug("trace: an agent event was not recorded: the gate record could not be read or written", "type", typ, "agent_id", id)
		return nil
	}
	after, afterErr := h.s.RecordProgress(h.story)
	if beforeErr != nil || afterErr != nil {
		return record
	}
	if count := h.s.StopCount(); count.Story == h.story && count.Progress == before {
		count.Progress = after
		if err := h.s.SaveStopCount(count); err != nil {
			slog.Debug("trace: the stop guard's count was not carried over an agent event", "type", typ)
		}
	}
	return record
}

// startOf is when the newest agent_start of the agent was recorded, in the
// trace's form, or "" where the record holds none: a guessed start would
// read as a real one.
func startOf(record *model.Record, id string) string {
	for _, e := range slices.Backward(record.Events) {
		if e.Type != model.EventAgentStart || e.AgentID != id {
			continue
		}
		if at, err := time.Parse(time.RFC3339, e.At); err == nil {
			return trace.Stamp(at)
		}
		return ""
	}
	return ""
}

// askedFor is the model and effort the staffing asked for the agent, with what
// a person assigned over it, and nothing for an agent the loop does not staff.
// It is cli's rule (staffingRow and assigned in internal/cli/staffing.go):
// a change to one is made to both, since hook may not import cli.
func askedFor(record *model.Record, agentType string) (askedModel, askedEffort string) {
	if record.Staffing == nil || !policy.IsLoopRole(agentType) {
		return "", ""
	}
	agent := policy.NormalizeAgent(agentType)
	for _, a := range record.Staffing.Agents {
		if a.Agent == agent {
			askedModel, askedEffort = a.AskedModel, a.AskedEffort
		}
	}
	for _, a := range record.Staffing.Assigned {
		if a.Agent == agent {
			askedModel, askedEffort = cmp.Or(a.Model, askedModel), cmp.Or(a.Effort, askedEffort)
		}
	}
	return label(askedModel, idMax), label(askedEffort, labelMax)
}

// handedBack is the status of the agent's newest hand-back in the trace, or ""
// where there is none. A trace that will not read says why by its class: the
// status then comes from the agent's last message.
func (h *held) handedBack(id string) string {
	reading, err := trace.Read(h.dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Debug("trace: the hand-back status was not read", "why", unreadTrace(err))
		}
		return ""
	}
	for _, r := range slices.Backward(reading.Records) {
		if r[keyKind] == "handback" && r["agent_id"] == id {
			status, _ := r["status"].(string)
			return knownStatus(status)
		}
	}
	return ""
}

// unreadTrace is why a trace was not read, as a fixed word.
func unreadTrace(err error) string {
	switch {
	case errors.Is(err, trace.ErrNotOurs):
		return "not one regular file"
	case errors.Is(err, trace.ErrOutside):
		return "outside the checkout"
	}
	return "unreadable"
}

// handBackStatus is the status of a hand-back that is one JSON object, done or
// blocked, and "" for anything else: text around the object, a list, another
// status, or none.
func handBackStatus(message string) string {
	// A map, not a struct: encoding/json gives a struct's field the value of
	// a key in any case, so `{"status":"blocked","Status":"done"}` read as
	// done (verifier, AC-5). A missing key is no value, and so no status.
	var m map[string]json.RawMessage
	var status string
	if json.Unmarshal([]byte(message), &m) != nil || json.Unmarshal(m["status"], &status) != nil {
		return ""
	}
	return knownStatus(status)
}

// knownStatus is a hand-back's status where it is done or blocked, and ""
// for anything else.
func knownStatus(status string) string {
	if status == "done" || status == "blocked" {
		return status
	}
	return ""
}

// ------------------------------------------------------------------ tools

// toolUsed records a shell command that finished or failed, and a hand-back
// made through its tool. Nothing of the command but its class and program is
// kept, and nothing of a failure but its exit code.
func (h *held) toolUsed(event string, p notePayload) {
	if p.ToolName == handBackTool && event == eventToolUsed {
		h.handBack(p)
		return
	}
	if !shellpolicy.Tools[p.ToolName] {
		return
	}
	class, program := shellpolicy.Classify(p.ToolInput.Command, p.ToolName == toolPowerShell, h.cfg.Commands)
	r := trace.Record{
		keyKind: "shell", trace.KeyAgentType: label(p.AgentType, idMax), "background": p.ToolInput.RunInBackground,
		"class": class, "program": program,
	}
	if d := p.DurationMS; d != nil && *d >= 0 && *d < 1<<53 {
		r["duration_ms"] = int64(*d)
	}
	if class == shellpolicy.ClassTest {
		r["outcome"] = "pass"
		if event == eventToolFailed {
			outcome, code, ok := failed(p)
			r["outcome"] = outcome
			if ok {
				r["exit_code"] = code
			}
		}
	}
	h.add(r)
}

// failed is a failed command's outcome, and its exit code where the error
// starts by giving one, as Claude Code writes it: "Exit code N".
func failed(p notePayload) (outcome string, code int, ok bool) {
	if p.IsInterrupt {
		return "interrupted", 0, false
	}
	rest, found := strings.CutPrefix(p.Error, "Exit code ")
	if !found {
		return outcomeUnknown, 0, false
	}
	end := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(rest[:end])
	if end == 0 || end > 10 || err != nil {
		return outcomeUnknown, 0, false
	}
	return "fail", n, true
}

// handBack records the status a subagent handed back through its tool, which
// its SubagentStop, another process, reads. Nothing else of the message is
// kept.
func (h *held) handBack(p notePayload) {
	id := label(p.AgentID, idMax)
	var message string
	if json.Unmarshal(p.ToolInput.Message, &message) != nil {
		message = string(p.ToolInput.Message)
	}
	status := handBackStatus(message)
	if id == "" || status == "" {
		return
	}
	h.add(trace.Record{keyKind: "handback", trace.KeyAgentType: label(p.AgentType, idMax), "agent_id": id, "status": status})
}

// ------------------------------------------------------------------ other events

// notified records a prompt for a person's permission and an idle prompt, the
// two notifications that are time a session waited, by their type alone.
func (h *held) notified(p notePayload) {
	switch p.NotificationType {
	case "permission_prompt", "idle_prompt":
		h.add(trace.Record{keyKind: "notification", trace.KeyAgentType: label(p.AgentType, idMax), "notification_type": p.NotificationType})
	}
}

// apiError is a StopFailure's error where it is one of the set Claude Code
// gives, and "" for anything else, so free text never reaches the trace. The
// set is the enum in the Claude Code 2.1.311 binary, since the hooks reference
// could not be read when it was written; a value outside it is an api_error
// with no kind, so the count of errors stays right.
func apiError(e string) string {
	switch e {
	case "authentication_failed", "oauth_org_not_allowed", "account_on_hold", "verification_required",
		"billing_error", "rate_limit", "overloaded", "invalid_request", "model_not_found",
		"server_error", "unknown", "max_output_tokens", "cloud_credential_error":
		return e
	}
	return ""
}

// compactTrigger is a compaction's trigger, auto or manual, and "" for
// anything else.
func compactTrigger(t string) string {
	if t == "auto" || t == "manual" {
		return t
	}
	return ""
}

// ------------------------------------------------------------------ turns

// turnMarks is how far the hook has read each transcript a session stopped
// in, the one read last first. A transcript is kept by its path's hash: the
// path may hold anything, and nothing of it reaches the loop's files.
type turnMarks struct {
	Marks []turnMark `json:"marks"`
}

type turnMark struct {
	Transcript string `json:"transcript"`
	Read       int64  `json:"read"`
}

// turn records a turn of the main conversation, counted from where the last
// turn in the same transcript stopped. It runs after the stop is answered, and
// so after decideStop let its lock go.
func (h *held) turn(p notePayload, read Transcripts) {
	r := trace.Record{
		keyKind: trace.KindTurn, "prompt_id": label(p.PromptID, promptMax), "effort": label(effortOf(p.Effort), labelMax),
	}
	if tasks := taskTypes(p.BackgroundTasks); len(tasks) > 0 {
		r["background_tasks"] = tasks
	}
	if read == nil || p.TranscriptPath == "" {
		h.add(r)
		return
	}
	path := filepath.Join(h.project, filepath.FromSlash(config.Dir), "state", model.TraceMarkFile)
	marks := readMarks(path)
	sum := sha256.Sum256([]byte(p.TranscriptPath))
	key := hex.EncodeToString(sum[:])
	var from int64
	if i := slices.IndexFunc(marks.Marks, func(m turnMark) bool { return m.Transcript == key }); i >= 0 {
		from = marks.Marks[i].Read
	}
	u, next, err := countsFrom(read, "main", p.TranscriptPath, from)
	moves := err == nil
	if err == nil {
		putCounts(r, u)
	}
	// A span too large to read, or a line that is not in the shape the reader
	// reads, will be the same at the next stop: the mark moves past it, or one
	// long session loses every later turn's counts. Any other failure, a read
	// out of time among them, is tried again from the same mark, so nothing is
	// counted twice and nothing skipped (O3a, red team N2).
	if errors.Is(err, trace.ErrTranscriptTooLarge) || errors.Is(err, trace.ErrTranscriptShape) {
		moves = next > from
	}
	if !h.add(r) || !moves {
		return
	}
	marks.Marks = slices.DeleteFunc(marks.Marks, func(m turnMark) bool { return m.Transcript == key })
	marks.Marks = append([]turnMark{{Transcript: key, Read: next}}, marks.Marks...)
	marks.Marks = marks.Marks[:min(len(marks.Marks), markCap)]
	writeMarks(h.project, path, marks)
}

// readMarks reads the turn mark, and holds no marks where it is not one
// regular file that reads: a named pipe at its name would hold the hook.
func readMarks(path string) turnMarks {
	var marks turnMarks
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		return marks
	}
	f, err := os.Open(path)
	var raw []byte
	if err == nil {
		defer func() { _ = f.Close() }()
		raw, err = io.ReadAll(io.LimitReader(f, 64<<10))
	}
	if err != nil || json.Unmarshal(raw, &marks) != nil {
		return turnMarks{}
	}
	return marks
}

// writeMarks writes the turn mark, and not through a link out of the project,
// as the store writes its state (store.leaves).
func writeMarks(project, path string, marks turnMarks) {
	if _, outside := pathrules.Rel(project, path); outside {
		slog.Debug("trace: the turn mark was not written: its directory leads out of the project")
		return
	}
	raw, err := json.Marshal(marks)
	if err == nil {
		err = fsx.WriteFileAtomic(path, append(raw, '\n'), 0o644)
	}
	if err != nil {
		// fsx's errors name the path.
		slog.Debug("trace: the turn mark was not written, so the next turn counts from where this one started")
	}
}

// taskTypes counts by type the background tasks Claude Code listed when the
// turn ended, every entry whatever its status: whether it lists finished tasks
// there is not known, so no status is read. A list that will not read is no
// count at all.
func taskTypes(raw json.RawMessage) map[string]int {
	var tasks []struct {
		Type json.RawMessage `json:"type"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &tasks) != nil {
		return nil
	}
	counts := map[string]int{}
	for _, t := range tasks {
		var typ string
		_ = json.Unmarshal(t.Type, &typ)
		counts[typ]++
	}
	return fold(counts, taskCap, func(s string) bool { return label(s, labelMax) != "" },
		func(n int) int64 { return int64(n) }, func(a, b int) int { return a + b })
}

// ------------------------------------------------------------------ counts

// countsFrom reads a transcript for the counts of a run or a turn, in five
// seconds at most and under a recover, since the reader is the host's. A read
// that gives no counts is one debug line, which names the transcript as agent
// or main and why in a fixed word for the error's class. It carries nothing
// else, now or later: no path, time, size or counter, so that the cases differ
// in the word alone and the word is the same on every run of one (red team,
// V1). The error's own text is never logged, since a reader may put the path
// in it.
func countsFrom(read Transcripts, which, path string, from int64) (u trace.Usage, next int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			u, next, err = trace.Usage{}, 0, errReaderPanicked
		}
		if err != nil {
			slog.Debug("trace: a transcript's counts were not recorded", "transcript", which, "why", whyNotCounted(err))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), transcriptTime)
	defer cancel()
	u, next, err = read(ctx, path, from)
	if err == nil && u.LongLines > 0 {
		slog.Debug("trace: transcript lines too long to read were passed over", "transcript", which, "lines", u.LongLines)
	}
	return u, next, err
}

// whyNotCounted is a failed read's class as a fixed word.
func whyNotCounted(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "missing"
	case errors.Is(err, trace.ErrTranscriptUnreadable):
		return "unreadable"
	case errors.Is(err, trace.ErrTranscriptShape):
		return "not in the shape the hook reads"
	case errors.Is(err, trace.ErrTranscriptTooLarge):
		return "larger than 64 MB"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "out of time"
	case errors.Is(err, errReaderPanicked):
		return "the reader panicked"
	}
	return "failed"
}

// putCounts puts a read's counts on a record, its names bounded and the rest
// folded into `other`.
func putCounts(r trace.Record, u trace.Usage) {
	r["models"] = fold(u.Models, modelCap, func(s string) bool { return boundedName(s) != "" },
		func(t trace.Tokens) int64 { return t.Input + t.CacheRead + t.CacheWritten + t.Output },
		func(a, b trace.Tokens) trace.Tokens {
			return trace.Tokens{
				Input: a.Input + b.Input, CacheRead: a.CacheRead + b.CacheRead,
				CacheWritten: a.CacheWritten + b.CacheWritten, Output: a.Output + b.Output,
			}
		})
	r["tools"] = fold(u.Tools, toolCap, func(s string) bool { return boundedName(s) != "" },
		func(n int) int64 { return int64(n) }, func(a, b int) int { return a + b })
	r["responses"], r["peak_context"] = u.Responses, u.PeakContext
}

// fold keeps at most limit entries of counts, `other` among them. A name that
// is not one goes into `other`; of the rest, those with the most weight keep
// their names, ties by name, and everything past them is summed into `other`
// too. Nothing is dropped, so the sums are what the counts were (THREATS I3).
func fold[V any](counts map[string]V, limit int, named func(string) bool, weight func(V) int64, plus func(V, V) V) map[string]V {
	var names []string
	var other V
	folded := false
	for name, v := range counts {
		if name != "other" && named(name) {
			names = append(names, name)
			continue
		}
		other, folded = plus(other, v), true
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(weight(counts[b]), weight(counts[a])), cmp.Compare(a, b))
	})
	keep := len(names)
	if folded || keep > limit {
		keep = min(keep, limit-1)
	}
	out := make(map[string]V, keep+1)
	for i, name := range names {
		if i < keep {
			out[name] = counts[name]
			continue
		}
		other, folded = plus(other, counts[name]), true
	}
	if folded {
		out["other"] = other
	}
	return out
}

// effortOf is the level of a payload's effort, `{"level": "high"}`.
func effortOf(raw json.RawMessage) string {
	var e struct {
		Level string `json:"level"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &e) != nil {
		return ""
	}
	return e.Level
}

// label is s where it is 1 to most characters of [A-Za-z0-9:._-], and "" for
// anything else: what a payload or the staffing names is kept only where it is
// a short name, since a model can write any of it (THREATS I3; red team, V6).
func label(s string, most int) string {
	return bounded(s, most, ":._-")
}

// boundedName is a model's or a tool's name: as label, at 128, with `/`, `@`
// and `+` as well, since a Vertex or OpenRouter model is named with them.
func boundedName(s string) string {
	return bounded(s, idMax, ":._@/+-")
}

func bounded(s string, most int, extra string) string {
	if s == "" || len(s) > most {
		return ""
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune(extra, r) {
			return ""
		}
	}
	return s
}
