package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/trace"
)

// session is the Claude Code session the sandbox's story is started in, and
// the one every hook payload names: the hook holds a story to the session
// that started it, and a call from any other is let through with less work.
const session = "perf-session-0001"

// exeSuffix is what a Windows binary's name ends in.
var exeSuffix = map[string]string{windows: ".exe"}[runtime.GOOS]

// passed is every variable of the caller's that reaches a child: where git is,
// and what Windows needs to start a process at all. Nothing else crosses, so
// a session's own SDLC_, CLAUDE_ and GIT_ variables cannot move the sandbox or
// change what is measured, and git reads no configuration but the repository's
// own (architect F-20: GIT_CONFIG_GLOBAL at the null device).
var passed = []string{"PATH", "SystemRoot", "SystemDrive", "ComSpec", "PATHEXT", "WINDIR"}

// The sandbox's files are the tool's own and a model never names them, so the
// modes are the narrow ones gosec asks for.
const (
	dirMode  = 0o750
	fileMode = 0o600
)

// sandbox is the directory the tool measures in: the binary built from the
// checkout, a home and a temp directory for what the children write, and the
// repository at one fixed path, rebuilt for each kind and put back from its
// snapshot before every run.
type sandbox struct {
	checkout, dir, bin, launcher, home, tmp, repo string
	snapshot                                      map[string]snap
	// version is what `sdlc version` cost, for the start-up split.
	version []measures
}

// snap is one file of the repository as the kind's build left it.
type snap struct {
	data    []byte
	mode    fs.FileMode
	modTime time.Time
}

// newSandbox builds the checkout's binary into a new temporary directory.
func newSandbox(ctx context.Context) (*sandbox, error) {
	checkout, err := filepath.Abs(".")
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(checkout, "go.mod")); err != nil {
		return nil, fmt.Errorf("%s is not the root of the checkout (no go.mod); run ./task perf-report there", checkout)
	}
	dir, err := os.MkdirTemp("", "sdlc-perf-")
	if err != nil {
		return nil, fmt.Errorf("the temporary directory could not be made (%w); make sure TMPDIR, or TMP and TEMP on Windows, names a directory you can write to", err)
	}
	// On macOS the temporary directory is behind a link, and a project
	// ordinarily is not: paths are resolved once so that what the children see
	// is one spelling.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	sb := &sandbox{checkout: checkout, dir: dir, bin: filepath.Join(dir, "bin", "sdlc"+exeSuffix),
		launcher: filepath.Join(checkout, "plugin", "bin", "sdlc-hook"),
		home:     filepath.Join(dir, "home"), tmp: filepath.Join(dir, "tmp"), repo: filepath.Join(dir, "repo")}
	for _, d := range []string{sb.home, sb.tmp} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			sb.close()
			return nil, err
		}
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", sb.bin, "./cmd/sdlc")
	build.Dir, build.Env = checkout, append(os.Environ(), "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		sb.close()
		return nil, fmt.Errorf("go build ./cmd/sdlc: %w\n%s", err, out)
	}
	return sb, nil
}

func (sb *sandbox) close() { _ = os.RemoveAll(sb.dir) }

// env is the whole environment of a child: built from nothing, with the
// session's mark only where asked, and never for a command a person runs.
func (sb *sandbox) env(inSession bool, extra ...string) []string {
	env := []string{
		"HOME=" + sb.home, "USERPROFILE=" + sb.home,
		"XDG_CACHE_HOME=" + filepath.Join(sb.home, ".cache"), "XDG_CONFIG_HOME=" + filepath.Join(sb.home, ".config"),
		"LOCALAPPDATA=" + filepath.Join(sb.home, "AppData", "Local"), "APPDATA=" + filepath.Join(sb.home, "AppData", "Roaming"),
		"TMPDIR=" + sb.tmp, "TMP=" + sb.tmp, "TEMP=" + sb.tmp,
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		"GIT_AUTHOR_NAME=Perf", "GIT_AUTHOR_EMAIL=perf@example.com",
		"GIT_COMMITTER_NAME=Perf", "GIT_COMMITTER_EMAIL=perf@example.com",
	}
	if inSession {
		env = append(env, store.SessionEnv+"="+session)
	}
	for _, name := range passed {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return append(env, extra...)
}

// child runs one process in the repository; a non-zero exit is an error with
// what it printed.
func (sb *sandbox) child(ctx context.Context, inSession bool, stdin, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = sb.repo, sb.env(inSession), strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", filepath.Base(program), strings.Join(args, " "), err, out)
	}
	return nil
}

func (sb *sandbox) git(ctx context.Context, args ...string) error {
	return sb.child(ctx, false, "", "git", args...)
}

func (sb *sandbox) sdlc(ctx context.Context, stdin string, args ...string) error {
	return sb.child(ctx, false, stdin, sb.bin, args...)
}

// write puts body at rel in the repository.
func (sb *sandbox) write(rel, body string) error {
	path := filepath.Join(sb.repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), fileMode)
}

// build makes the repository anew for kind at the fixed path and takes its
// snapshot. The real commands make it, as the AC-10 benchmarks' fixtures are
// made, so a gate that grows a requirement fails the build rather than
// measuring a project no session could reach.
func (sb *sandbox) build(ctx context.Context, kind string) error {
	if err := os.RemoveAll(sb.repo); err != nil {
		return err
	}
	if err := os.MkdirAll(sb.repo, dirMode); err != nil {
		return err
	}
	steps := []func() error{
		func() error {
			return sb.git(ctx, "init", "--quiet", "--initial-branch="+config.Default().Git.TrunkBranch)
		},
		// In the repository's own configuration, which the snapshot carries
		// into every run: a commit otherwise starts `git maintenance run
		// --auto` detached, and its lock under .git/objects came and went
		// while the snapshot walked the tree (US-129 verification 2, F-12).
		func() error { return sb.git(ctx, "config", "maintenance.auto", "false") },
		func() error { return sb.git(ctx, "config", "gc.auto", "0") },
		func() error { return sb.sdlc(ctx, "", "init") },
		sb.configure,
		func() error { return sb.write("invoice.go", "package invoice\n") },
		func() error { return sb.commit(ctx, "chore: a project, as sdlc init leaves it") },
		func() error { return sb.padBacklog(ctx) },
	}
	if kind != kindInitialised {
		upTo := model.Gate(strings.TrimPrefix(kind, kindStarted))
		if kind == kindStopped {
			upTo = model.GateImplementation
		}
		steps = append(steps,
			// Started from the session, so the hook holds the story to it.
			func() error { return sb.child(ctx, true, "", sb.bin, "start") },
			func() error { return sb.reach(ctx, upTo) },
			sb.padStory,
		)
		switch {
		case upTo == model.GateCommit:
			// The commit gate records that the work reached trunk.
			steps = append(steps, func() error { return sb.commit(ctx, "feat: the story") })
		case upTo == model.GateTestsFrozen:
			// A test file for the measured freeze to hold, and the test
			// reviewer's approve of it, which that freeze waits for.
			steps = append(steps, func() error { return sb.write("invoice_test.go", "package invoice\n") })
			steps = append(steps, sb.approvals(ctx, model.GateTestsFrozen)...)
		case kind == kindStopped:
			steps = append(steps, func() error { return sb.sdlc(ctx, "", "stop") })
		}
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return fmt.Errorf("building the %s sandbox: %w", kind, err)
		}
	}
	return sb.take()
}

// configure turns worktrees and the project manager off in the new project's
// configuration, keeping the rest: a story in one checkout, as the AC-10
// command benchmarks have it, and a gate that waits for no staffing.
func (sb *sandbox) configure() error {
	path := filepath.Join(sb.repo, ".sdlc", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	section := func(parent map[string]any, key string) map[string]any {
		m, _ := parent[key].(map[string]any)
		if m == nil {
			m = map[string]any{}
			parent[key] = m
		}
		return m
	}
	section(cfg, "git")["worktrees"] = false
	section(section(cfg, "team"), "agents")["project-manager"] = map[string]any{"enabled": false}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), fileMode)
}

func (sb *sandbox) commit(ctx context.Context, message string) error {
	if err := sb.git(ctx, "add", "-A"); err != nil {
		return err
	}
	return sb.git(ctx, "commit", "--quiet", "-m", message)
}

// padBacklog adds 129 finished stories to the backlog, each with the criteria,
// non-goals and notes a real one carries, and commits it: about 10,000 lines,
// the size of this repository's.
func (sb *sandbox) padBacklog(ctx context.Context) error {
	cfg, err := config.Load(sb.repo)
	if err != nil {
		return err
	}
	path := cfg.BacklogPath(sb.repo)
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var b model.Backlog
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	words := strings.Repeat("the hook answers every call it is asked without reading a file twice ", 3)
	for i := range 129 {
		s := model.Story{
			ID: fmt.Sprintf("P-%03d", i+1), Title: fmt.Sprintf("Finished story %d", i+1), Status: model.StatusDone,
			AsA: "person working a story", IWant: words, SoThat: words, Notes: words,
		}
		for j := range 14 {
			s.AcceptanceCriteria = append(s.AcceptanceCriteria, model.AcceptanceCriterion{ID: fmt.Sprintf("AC-%d", j+1), Text: words})
		}
		for range 6 {
			s.NonGoals = append(s.NonGoals, words)
		}
		b.Stories = append(b.Stories, s)
	}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), fileMode); err != nil {
		return err
	}
	return sb.commit(ctx, "chore: the backlog as long as a real one")
}

// reach takes the started story through every gate before upTo with the real
// commands, as the cli package's test helper does.
func (sb *sandbox) reach(ctx context.Context, upTo model.Gate) error {
	for _, gate := range model.Gates {
		if gate == upTo {
			return nil
		}
		for _, step := range sb.gateSteps(ctx, gate) {
			if err := step(); err != nil {
				return err
			}
		}
	}
	return nil
}

// gateSteps is what one gate needs before it is recorded, its reviews, and
// recording it.
func (sb *sandbox) gateSteps(ctx context.Context, gate model.Gate) []func() error {
	artifact := func(kind string) func() error {
		return func() error { return sb.sdlc(ctx, "# "+kind+"\n", "artifact", "write", kind) }
	}
	var steps []func() error
	switch gate {
	case model.GateAnalysis:
		steps = append(steps, artifact("analysis"), artifact("threats"))
	case model.GateTestsFrozen:
		steps = append(steps, func() error { return sb.write("internal/invoice_test.go", "package internal\n\n// AC-1\n") },
			artifact("test_plan"))
	case model.GatePlan:
		steps = append(steps, artifact("plan"))
	case model.GateVerification:
		steps = append(steps, artifact("verification"))
	case model.GateRetro:
		steps = append(steps, artifact("retro"))
	case model.GateCommit:
		steps = append(steps, func() error { return sb.commit(ctx, "feat: the story") })
	case model.GateDoR, model.GateDesignReview, model.GateImplementation, model.GateVerifierReview, model.GateCodeReview:
		// Nothing of its own: only its reviews, below.
	}
	steps = append(steps, sb.approvals(ctx, gate)...)
	// The freeze waits for the tests gate's reviews, so it is taken after them.
	if gate == model.GateTestsFrozen {
		steps = append(steps, func() error { return sb.sdlc(ctx, "", "freeze") })
	}
	return append(steps, func() error {
		return sb.sdlc(ctx, "", "gate", string(gate), "pass", "--note", "reached by the perf tool")
	})
}

// approvals is an approve from each reviewer the gate expects, the Gate 1
// advocate aside.
func (sb *sandbox) approvals(ctx context.Context, gate model.Gate) []func() error {
	var steps []func() error
	for _, r := range model.ReviewersFor(gate) {
		if r.WhenDoRAdvocate {
			continue
		}
		steps = append(steps, func() error {
			return sb.sdlc(ctx, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
		})
	}
	return steps
}

// padStory pads the story's gate record to 190 KB with the agent runs a long
// story records, and its trace with 2,000 lines of shell commands.
func (sb *sandbox) padStory() error {
	cfg, err := config.Load(sb.repo)
	if err != nil {
		return err
	}
	s := store.New(&config.Project{Root: sb.repo, Config: cfg})
	record, err := s.Record("US-001")
	if err != nil {
		return err
	}
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	for i := 0; ; i++ {
		when := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		agent := fmt.Sprintf("agent-%04d", i)
		record.Events = append(record.Events,
			model.Event{At: when, Type: model.EventAgentStart, Message: "sdlc:researcher started", AgentType: "sdlc:researcher", AgentID: agent},
			model.Event{At: when, Type: model.EventAgentEnd, Message: "sdlc:researcher finished", AgentType: "sdlc:researcher", AgentID: agent})
		if i%16 != 0 {
			continue
		}
		raw, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return err
		}
		if len(raw) >= 190<<10 {
			break
		}
	}
	if err := s.SaveRecord(record); err != nil {
		return err
	}
	dir := filepath.Join(sb.repo, ".sdlc", "stories", "US-001")
	for range 2000 {
		if err := trace.Append(dir, trace.Record{"kind": "shell", trace.KeyAgentType: implementer,
			keyDuration: 2000, "class": "test", "program": "go", "outcome": "ok", "exit_code": 0}); err != nil {
			return err
		}
	}
	return nil
}

// take is the snapshot of every file of the repository, .git included.
func (sb *sandbox) take() error {
	sb.snapshot = map[string]snap{}
	fsys := os.DirFS(sb.repo)
	return fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		// A file that is gone by the time the walk reaches it was never
		// part of the repository's state, only of a process that had not
		// finished with it: passed over, not an error.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err == nil {
			var data []byte
			if data, err = fs.ReadFile(fsys, rel); err == nil {
				sb.snapshot[filepath.FromSlash(rel)] = snap{data, info.Mode().Perm(), info.ModTime()}
				return nil
			}
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
}

// restore puts the repository back as the snapshot has it, in place, so that
// the paths the claim and the configuration record stay true. Every file's
// time is put back too: the hook treats a record younger than a session's
// last call otherwise.
func (sb *sandbox) restore() error {
	if err := os.RemoveAll(sb.repo); err != nil {
		return err
	}
	for rel, s := range sb.snapshot {
		path := filepath.Join(sb.repo, rel)
		if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
			return err
		}
		if err := os.WriteFile(path, s.data, s.mode); err != nil {
			return err
		}
		if err := os.Chtimes(path, s.modTime, s.modTime); err != nil {
			return err
		}
	}
	return nil
}

// command is the process one run of n is, with binary.
func (sb *sandbox) command(ctx context.Context, binary string, n name) (*exec.Cmd, error) {
	// The host's own variable, spelt here: this tool builds the environment
	// a host gives the launcher, so it has to name what that host sets, and
	// the host's profile is only cmd/sdlc's to import. It is build tooling
	// outside the binary, where the rule against naming a host does not
	// reach.
	env := sb.env(false, "CLAUDE_PROJECT_DIR="+sb.repo)
	if n.event == "" {
		cmd := exec.CommandContext(ctx, binary, n.args...)
		cmd.Stdin = strings.NewReader(n.stdin)
		if !n.person {
			env = sb.env(true)
		}
		cmd.Dir, cmd.Env = sb.repo, env
		return cmd, nil
	}
	payload := maps.Clone(n.payload)
	payload["hook_event_name"], payload["session_id"], payload["cwd"] = n.event, session, sb.repo
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	if n.launcher {
		cmd = exec.CommandContext(ctx, sb.launcher, n.event)
		env = append(env, "SDLC_BIN="+binary)
	} else {
		cmd = exec.CommandContext(ctx, binary, "hook", n.event)
	}
	cmd.Dir, cmd.Env, cmd.Stdin = sb.repo, env, bytes.NewReader(raw)
	return cmd, nil
}

// once runs n with binary on the repository put back from its snapshot, and
// returns its measures. A run that does not end as expected is an error with
// what it printed: a refusal timed is not the work timed.
func (sb *sandbox) once(ctx context.Context, binary string, n name) (measures, error) {
	if err := sb.restore(); err != nil {
		return measures{}, err
	}
	cmd, err := sb.command(ctx, binary, n)
	if err != nil {
		return measures{}, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err = cmd.Run()
	m := measures{wall: time.Since(start)}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return m, fmt.Errorf("%s did not run: %w", n.name, err)
	}
	ps := cmd.ProcessState
	m.cpu = ps.UserTime() + ps.SystemTime()
	m.rss, m.rssKnown = peakRSS(ps)
	if !n.ok(ps.ExitCode(), stdout.String()) {
		return m, fmt.Errorf("%s %s with %s exited %d:\n%s%s", n.name, n.kind(), binary, ps.ExitCode(), stdout.String(), stderr.String())
	}
	return m, nil
}

// measure runs every name runs times on the result binary, and on the base
// where one is given, each kind of sandbox built once.
func (sb *sandbox) measure(ctx context.Context, runs int, base string) ([]result, error) {
	list := names(sb.repo)
	var out []result
	for _, kind := range kinds(list) {
		results, err := sb.measureKind(ctx, runs, base, kind, list)
		if err != nil {
			return nil, err
		}
		out = append(out, results...)
	}
	return out, nil
}

// measureKind builds one kind of sandbox and runs the names that need it; on
// the started story it times `sdlc version` too, for the start-up split.
func (sb *sandbox) measureKind(ctx context.Context, runs int, base, kind string, list []name) ([]result, error) {
	if err := sb.build(ctx, kind); err != nil {
		return nil, err
	}
	var out []result
	for _, n := range list {
		if n.on != kind {
			continue
		}
		r, err := sb.measureName(ctx, runs, base, n)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if kind == startedAt(model.GateImplementation) {
		r, err := sb.measureName(ctx, runs, "", name{name: "sdlc version", args: []string{"version"}, person: true, ok: exits0})
		if err != nil {
			return nil, err
		}
		sb.version = r.runs
	}
	return out, nil
}

// measureName is n's runs, the result's and the base's taken turn about so
// that a burst of load falls on both alike.
func (sb *sandbox) measureName(ctx context.Context, runs int, base string, n name) (result, error) {
	r := result{name: n}
	for range runs {
		m, err := sb.once(ctx, sb.bin, n)
		if err != nil {
			return r, err
		}
		r.runs = append(r.runs, m)
		if base == "" {
			continue
		}
		if m, err = sb.once(ctx, base, n); err != nil {
			return r, err
		}
		r.base = append(r.base, m)
	}
	return r, nil
}

// split is the p50 wall time of `sdlc version`, of the PreToolUse Bash event
// run directly, and of the same through the launcher (0 where it is not run).
func (sb *sandbox) split(results []result) (version, direct, launcher float64) {
	version = percentile(column(sb.version, wall), 50)
	for _, r := range results {
		switch r.name.name {
		case preToolUseBash:
			direct = percentile(column(r.runs, wall), 50)
		case launcherName:
			launcher = percentile(column(r.runs, wall), 50)
		}
	}
	return version, direct, launcher
}
