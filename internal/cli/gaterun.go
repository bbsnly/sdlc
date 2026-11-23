package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellx"
	"github.com/bbsnly/sdlc/internal/store"
)

// `sdlc gate implementation pass` runs the commands the landing runs, on the
// tree the gate measures, and records what passed against that tree (US-134).
// The landing then lands on that result where the tree and the commands are
// the ones recorded, instead of running them again.

const (
	// implementationLog and freezeLog are the files in the story's directory
	// that hold all a run printed: the gate's and the freeze's narrow run's.
	implementationLog = "implementation-run.log"
	freezeLog         = "freeze-run.log"
	// runLogPattern is the story's .gitignore line for the temporary files a
	// run streams into: a run killed outright leaves its own, holding
	// whatever a test printed, and a project that commits its story
	// directories would commit it.
	runLogPattern = ".*-run.log.tmp*"
	// runLogLimit bounds an output file; a closing line says where it was cut.
	runLogLimit = 16 << 20
	// gateTailBytes and gateTailLines are how much of what a failed command
	// printed the gate's refusal shows: the file holds the rest.
	gateTailBytes = 16 << 10
	gateTailLines = 20
)

// gateResult is what a passed run of the gate puts on the record.
type gateResult struct {
	tree     string
	commands []model.CommandRun
}

// runsCommands reports whether this gate runs the configured commands: the
// implementation gate's pass, where the checkout's configuration sets at
// least one of the landing's. fail and pending run nothing.
func (g *gateRun) runsCommands() bool {
	return g.gate == model.GateImplementation && g.status == model.GatePass && len(setCommands(g.s.Config())) > 0
}

// isRunDirVar is whether an entry of the environment sets SDLC_RUN_DIR,
// compared as Windows compares names.
func isRunDirVar(entry string) bool {
	name, _, _ := strings.Cut(entry, "=")
	return strings.EqualFold(name, shellx.RunDirVar)
}

// gateRefused is SDLC-E0095 with what and why.
func gateRefused(what, why string) *sdlcerr.Error {
	return sdlcerr.New(sdlcerr.GateRunRefused, what, why)
}

// gateBefore is what the gate takes before its run, to hold the run to: the
// tree the result is for, HEAD, every file of that tree as it was, each
// gate's status and time, and the freeze.
type gateBefore struct {
	tree, head string
	files      treeState
	began      map[model.Gate]model.GateResult
	frozen     *model.Lock
}

// runCommands is the implementation gate's run (PLAN D5). unlock is the
// project's lock, let go while the commands run and taken back after them.
func (g *gateRun) runCommands(unlock *func(), late gateOvers) error {
	ctx, s, cfg, id := g.cmd.Context(), g.s, g.s.Config(), g.id
	// A command that runs `sdlc gate` would start the set again, with the
	// lock let go, to any depth.
	if slices.ContainsFunc(os.Environ(), isRunDirVar) {
		return gateRefused("the implementation gate would run its commands inside a run of them, so nothing was recorded",
			shellx.RunDirVar+" is already set, which is what a command sdlc runs is given, so this gate is running "+
				"inside one of the configured commands and would start them again with the project's lock let go")
	}
	runLock, err := s.LockRun(id, store.RunGate)
	if err != nil {
		return err
	}
	defer runLock.Release()
	before, err := g.takeBefore(ctx)
	if err != nil {
		return err
	}
	run, err := shellx.NewRunDir()
	if err != nil {
		return gateRefused("the implementation gate's commands could not be run, so nothing was recorded",
			"the run's directory could not be made: "+err.Error()).WithCause(err)
	}
	defer func() { _ = run.Close() }()
	log, first, err := g.prepareRun(ctx, run)
	if err != nil {
		return err
	}
	defer log.discard()

	// The commands are the project's own and can run for longer than any
	// other command waits for the lock, so it is let go for them; the story's
	// run lock stays held.
	(*unlock)()
	*unlock = noUnlock
	stderr := g.cmd.ErrOrStderr()
	r, err := runSet(ctx, commandSet{
		root: s.Root(), cfg: cfg, run: run, out: log, keep: gateTailBytes, first: first,
		before: func(key string) {
			fmt.Fprintf(stderr, "sdlc: running commands.%s for %s's implementation gate in %s; its output is put in %s "+
				"when the run ends\n", key, id, s.Root(), log.final)
			log.line("sdlc: commands." + key)
		},
		written: func(ran ranCommand) {
			log.line(fmt.Sprintf("sdlc: commands.%s, exit %d, %s", ran.key, ran.exit, humanTime(ran.duration)))
		},
	})
	if err != nil {
		return err
	}
	if f := r.failed; f != nil && f.couldNotRun() {
		// Nothing is recorded and no rework round is spent: the command says
		// nothing of the work. The lock is not taken back, so that an
		// interrupt here leaves nothing held.
		shown, said := log.place()
		return gateRefused(fmt.Sprintf("%s%s could not be run (status %d), so nothing was recorded", commandsPrefix, f.key, f.exit),
			r.why+"; a person puts the program, the shell or "+commandsPrefix+f.key+" right; everything it printed is in "+
				shown+said+printedTail(f))
	}
	return g.afterRun(unlock, late, before, r, log)
}

// takeBefore takes what the run is held to, under the project's lock.
func (g *gateRun) takeBefore(ctx context.Context) (gateBefore, error) {
	var b gateBefore
	var err error
	if b.tree, err = g.rd.subject(ctx); err != nil {
		return b, err
	}
	if b.head, err = g.rd.headOf(ctx); err != nil {
		return b, err
	}
	files := g.rd.files
	if files == nil {
		if files, err = gitx.Files(ctx, g.s.Root()); err != nil {
			return b, err
		}
	}
	b.files, b.began = takeTree(g.s, files), gateTimes(g.record)
	b.frozen, err = g.s.Lock()
	return b, err
}

// prepareRun opens the output file and makes the first command, while the
// project's lock is still held: with no shell nothing has been let go.
func (g *gateRun) prepareRun(ctx context.Context, run *shellx.RunDir) (*outputFile, *oneCommand, error) {
	dir, err := g.s.StoryDir(g.id)
	if err != nil {
		return nil, nil, err
	}
	log, err := openOutputFile(dir, implementationLog)
	if err != nil {
		return nil, nil, err
	}
	set := setCommands(g.s.Config())
	first, err := prepareOne(ctx, g.s.Root(), set[0][0], set[0][1], run, log, gateTailBytes)
	if err != nil {
		log.discard()
		return nil, nil, gateRefused(commandsPrefix+set[0][0]+" could not be run, so nothing was recorded", err.Error()).WithCause(err)
	}
	return log, first, nil
}

// afterRun takes the project's lock back, holds the run to what was taken
// before it, and records the gate: failed where a command failed, passed with
// its result where every command passed and the tree is as it was.
func (g *gateRun) afterRun(unlock *func(), late gateOvers, before gateBefore, r setRun, log *outputFile) error {
	ctx, s := g.cmd.Context(), g.s
	held, err := lockHeld(ctx, func() (*store.Guard, error) { return store.LockOn(s.LockRoot(), "gate", envOf(g.cmd).lockClock) })
	if err != nil {
		return err
	}
	*unlock = held.Release
	shown, said := log.place()
	record, err := g.unmovedSince(before.head, before.began, before.frozen)
	if err != nil {
		return err
	}
	g.record = record

	if f := r.failed; f != nil {
		return g.recordFailed(late, f, r.why, shown, said)
	}
	if path := changedSince(s, before.files); path != "" {
		return gateRefused("a command wrote "+path+", a file of the tree it ran on, so nothing was recorded",
			"the result is for the tree hashed before the run, and with "+path+" changed it would stand for a tree nobody "+
				"ran the commands on; a command writes what it makes under $"+shellx.RunDirVar+" or a path git ignores, "+
				"and the gate is recorded again; everything the commands printed is in "+shown+said)
	}

	// The gate's ordinary write, on the record as it is now.
	if record.Pass(model.GateCodeReview) {
		g.moveBase(ctx)
	}
	g.ran = &gateResult{tree: before.tree, commands: r.results(s.Config())}
	w, err := g.write(late)
	if err != nil {
		return err
	}
	if err := g.report(w.reopened, w.done, w.handedOver, w.message); err != nil {
		return err
	}
	if said != "" {
		fmt.Fprintln(g.cmd.ErrOrStderr(), "sdlc:"+said)
	}
	return nil
}

// recordFailed is AC-11: a command failed, missed its threshold or printed no
// figure. The gate is recorded as failed on the record as it is now, which
// counts towards loop.max_rework_rounds, and refused with nothing on stdout.
func (g *gateRun) recordFailed(late gateOvers, f *ranCommand, why, shown, said string) error {
	g.status = model.GateFail
	g.note = fmt.Sprintf("%s%s ended with status %d and did not pass; its output is in %s", commandsPrefix, f.key, f.exit,
		storyRelative(g.id, shown))
	w, err := g.write(late)
	if err != nil {
		return err
	}
	if w.handedOver != "" {
		fmt.Fprintln(g.cmd.ErrOrStderr(), "sdlc: "+w.message)
	}
	return gateRefused(fmt.Sprintf("%s%s ended with status %d and did not pass, so the implementation gate is recorded as failed",
		commandsPrefix, f.key, f.exit),
		why+"; everything the commands printed is in "+shown+said+printedTail(f))
}

// printedTail is the last lines a command printed, set apart under a line of
// its own after everything sdlc says, so that none of it can pass for the
// refusal's own words.
func printedTail(f *ranCommand) string {
	printed := strings.TrimRight(f.printed, "\n")
	if printed == "" {
		return "\n\n  " + commandsPrefix + f.key + " printed nothing"
	}
	return fmt.Sprintf("\n\n  %s%s printed (last %d lines):\n%s", commandsPrefix, f.key, gateTailLines,
		setApart(lastLines(printed, gateTailLines)))
}

// storyRelative is a file in the story's directory by its slash path from
// the project's root, as a note on the record names it.
func storyRelative(id, path string) string {
	return ".sdlc/stories/" + id + "/" + filepath.Base(path)
}

// gateTimes is each gate's status and time, as the record holds them.
func gateTimes(record *model.Record) map[model.Gate]model.GateResult {
	times := map[model.Gate]model.GateResult{}
	for g, r := range record.Gates {
		times[g] = model.GateResult{Status: r.Status, At: r.At}
	}
	return times
}

// unmovedSince is the record as it is now, once nothing the gate stood on
// moved while the commands ran: the story is still the one being worked on,
// every gate has the status and time it had, the freeze is the one there
// was, its files hold what was frozen, and HEAD is where it was. The record,
// the freeze and the backlog are read again only where the file differs from
// the one read before the run.
func (g *gateRun) unmovedSince(head string, began map[model.Gate]model.GateResult, frozen *model.Lock) (*model.Record, error) {
	s, id := g.s, g.id
	moved := func(what string) error {
		return gateRefused("the story changed while the commands ran, so nothing was recorded",
			what+"; a person's command or another session in this checkout is the usual cause, and the gate is "+
				"recorded again once that has finished")
	}
	if active, err := s.Active(); err != nil {
		return nil, err
	} else if active != id {
		return nil, moved("the story being worked on is now " + cmpOrNone(active) + ", not " + id)
	}
	record, err := s.Record(id)
	if err != nil {
		return nil, err
	}
	for _, gate := range model.Gates {
		now := record.Gates[gate]
		if was := began[gate]; now.Status != was.Status || now.At != was.At {
			return nil, moved("gate " + string(gate) + " was recorded as " + cmpOrNone(string(now.Status)))
		}
	}
	lock, err := s.Lock()
	if err != nil {
		return nil, err
	}
	if !sameFreeze(frozen, lock) {
		return nil, moved("the freeze was taken again or lifted")
	}
	if lock != nil {
		if changed := verifyFreeze(s, lock); len(changed) > 0 {
			return nil, brokenFreeze(changed)
		}
	}
	gitx.Forget(g.cmd.Context())
	if now, err := gitx.Head(g.cmd.Context(), s.Root()); err != nil {
		return nil, err
	} else if now != head {
		return nil, moved("HEAD moved from " + short(head) + " to " + short(now))
	}
	return record, nil
}

// saidNone stands for a value that is empty in a refusal.
const saidNone = "none"

func cmpOrNone(s string) string {
	if s == "" {
		return saidNone
	}
	return s
}

func short(hash string) string { return hash[:min(len(hash), 12)] }

// sameFreeze reports two freezes that name the same story, time and files.
func sameFreeze(a, b *model.Lock) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Story == b.Story && a.At == b.At && maps.Equal(a.Files, b.Files)
}

// ---------------------------------------------------------------- the tree

// treeState is each path of the tree as the gate's check takes it, in Go and
// with no git: a link by its own text, never followed; a regular file by the
// hash of its bytes, opened so that it cannot block; anything else by its
// type; an absent path as absent. The backlog is taken as ReviewSubject
// measures it, so that a story's status changed by another session meanwhile
// is not a change of the tree, and only where it is a regular file, which
// BacklogSubject opens as pathState does: any other type is taken by
// pathState (code review, security F-3).
type treeState struct {
	paths      map[string]string
	backlogRel string
	backlog    []byte
	hasBacklog bool
}

// takeTree is the state of every path of files outside .sdlc/.
func takeTree(s *store.Store, files []string) treeState {
	t := treeState{paths: make(map[string]string, len(files))}
	t.backlogRel, t.backlog, t.hasBacklog = s.BacklogSubject()
	for _, p := range files {
		if p == ".sdlc" || strings.HasPrefix(p, ".sdlc/") || t.hasBacklog && p == t.backlogRel {
			continue
		}
		t.paths[p] = pathState(filepath.Join(s.Root(), filepath.FromSlash(p)))
	}
	return t
}

// changedSince is the first path, in order, whose state differs from before,
// or "" where none does. A file a command added is not looked at.
func changedSince(s *store.Store, before treeState) string {
	for _, p := range slices.Sorted(maps.Keys(before.paths)) {
		if pathState(filepath.Join(s.Root(), filepath.FromSlash(p))) != before.paths[p] {
			return p
		}
	}
	if before.hasBacklog {
		if rel, now, ok := s.BacklogSubject(); !ok || rel != before.backlogRel || !bytes.Equal(now, before.backlog) {
			return before.backlogRel
		}
	}
	return ""
}

// pathState is one path's state, as treeState says.
func pathState(path string) string {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "absent"
	case err != nil:
		return "unreadable"
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "link?"
		}
		return "link:" + target
	case !info.Mode().IsRegular():
		return "type:" + info.Mode().Type().String()
	}
	f, _, err := fsx.OpenRegular(path)
	if err != nil {
		return "swapped"
	}
	defer func() { _ = f.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "unreadable"
	}
	return "file:" + hex.EncodeToString(sum.Sum(nil))
}

// ---------------------------------------------------------------- the output file

// outputFile is an output file being written: a temporary file of its own in the
// story's directory, put at its name only when the run ends, by a rename that
// replaces whatever stands there and never writes through it (security F-10).
type outputFile struct {
	final, tmp string
	f          *os.File
	mu         sync.Mutex
	written    int64
	cut        bool
	placed     bool
}

// openOutputFile writes the story's .gitignore and then creates the temporary
// file, in that order, so that the file is ignored by git from the moment it
// exists, a run killed outright included.
//
// The name ends in .out, after the random digits: an interrupted write leaves
// a file named .name.tmp followed by digits and nothing after them, and every
// writing command sweeps those under the project lock, which is let go while
// the commands run. Named with the digits last, the log was taken for one of
// them, an `sdlc artifact write` made meanwhile removed it, and the refusal
// named a file that was not there (verifier F-1).
func openOutputFile(dir, name string) (*outputFile, error) {
	if err := ignoreRunLogs(dir); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, "."+name+".tmp*.out")
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.StateUnwritable, "the run's output file could not be made in "+sdlcerr.QuotePath(dir),
			err.Error()).WithCause(err)
	}
	shellx.Register(f.Name())
	return &outputFile{final: filepath.Join(dir, name), tmp: f.Name(), f: f}, nil
}

// Write keeps what a command printed, up to runLogLimit. It never fails: a
// write it could not make must not stop the command's own output.
func (l *outputFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cut || l.f == nil {
		return len(p), nil
	}
	room := runLogLimit - l.written
	keep := p
	if int64(len(keep)) > room {
		keep, l.cut = keep[:room], true
	}
	n, _ := l.f.Write(keep)
	l.written += int64(n)
	if l.cut {
		_, _ = fmt.Fprintf(l.f, "\nsdlc: the output was cut here, at %d MiB\n", runLogLimit>>20)
	}
	return len(p), nil
}

// line is one line of sdlc's own, between the commands' output.
func (l *outputFile) line(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil && !l.cut {
		_, _ = fmt.Fprintln(l.f, text)
	}
}

// place puts the file at its name, and returns the path every line names it
// by and, where it could not be put there, the sentence that says so. The
// temporary file is then kept for a person, ignored by the story's
// .gitignore; the outcome is the same either way, because the file is data.
func (l *outputFile) place() (shown, said string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.placed {
		return l.final, ""
	}
	l.placed = true
	err := l.f.Sync()
	err = errors.Join(err, l.f.Close())
	l.f = nil
	if err == nil {
		err = fsx.Replace(l.tmp, l.final)
	}
	if err != nil {
		shellx.Keep(l.tmp)
		return l.tmp, fmt.Sprintf(" (the output could not be put at %s: %v; it is in %s. Remove what stands at %s before "+
			"the next run.)", l.final, err, l.tmp, l.final)
	}
	shellx.Keep(l.tmp)
	return l.final, ""
}

// discard removes the temporary file where it was never put in place.
func (l *outputFile) discard() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.placed {
		return
	}
	l.placed = true
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	_ = shellx.Remove(l.tmp)
}

// ignoreRunLogs makes the story's .gitignore hold the two output names and
// the pattern of their temporary files. It is read only where it is one
// regular file with one name; anything else at the name, a link, a second
// name of another file or a pipe, is taken as absent, and the file is written
// by a rename that replaces it, never through it. A line already there stays,
// in its order, and counts with or without a leading /.
func ignoreRunLogs(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	var raw []byte
	if f, fi, err := fsx.OpenRegular(path); err == nil {
		if fsx.OneName(f, fi) {
			raw, err = io.ReadAll(f)
			if err != nil {
				raw = nil
			}
		}
		_ = f.Close()
	}
	have := map[string]bool{}
	for line := range strings.Lines(string(raw)) {
		have[strings.TrimPrefix(strings.TrimRight(line, " \t\r\n"), "/")] = true
	}
	out := raw
	added := false
	for _, want := range []string{implementationLog, freezeLog, runLogPattern} {
		if have[want] {
			continue
		}
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		out, added = append(out, want+"\n"...), true
	}
	if !added {
		return nil
	}
	if err := fsx.WriteFileAtomic(path, out, 0o644); err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable, "the story's .gitignore could not be written at "+sdlcerr.QuotePath(path),
			err.Error()).WithCause(err)
	}
	return nil
}
