package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/shellx"
)

// The one implementation of "run the configured commands and say what
// happened", which the implementation gate, the freeze's narrow run and the
// landing share, so that a result one of them records is one the others can
// read as theirs.

// ranCommand is how one configured command ended.
type ranCommand struct {
	key      string
	exit     int
	duration time.Duration
	// printed is the tail of both streams, in the order they were written;
	// stdout the tail of standard output alone, where the figure a threshold
	// reads is looked for.
	printed, stdout string
	// startErr is why the command could not be started at all: no shell, or
	// a start that failed. It is nil for one that ran, however it ended.
	startErr error
	// err is what the run returned, nil for an exit of 0.
	err error
}

// couldNotRun reports a command that says nothing about the work: it never
// started, a signal ended it, or the shell answered 126 or 127, cannot
// execute and not found. The split is the shell's, not any tool's, so the
// core stays language-neutral.
func (r ranCommand) couldNotRun() bool {
	return r.startErr != nil || r.exit < 0 || r.exit == 126 || r.exit == 127
}

// result is the command as the record keeps it.
func (r ranCommand) result(cfg config.Config) model.CommandRun {
	return model.CommandRun{Name: commandsPrefix + r.key, Exit: r.exit, DurationMS: r.duration.Milliseconds(),
		Digest: commandDigest(cfg, r.key)}
}

// oneCommand is a configured command made and not yet run.
type oneCommand struct {
	key             string
	c               *exec.Cmd
	printed, stdout *shellx.TailWriter
}

// prepareOne makes command, to run from root through the shell in a process
// group of its own. run, where it is not nil, gives it the run's directory,
// and out, where it is not nil, everything it prints; env is added to what
// it is handed. keep is how much of what it prints is handed back. It is
// made apart from the run so that a caller can make the first command while
// it still holds the project's lock: with no shell, nothing has been let go.
func prepareOne(ctx context.Context, root, key, command string, run *shellx.RunDir, out io.Writer, keep int,
	env ...string) (*oneCommand, error) {
	c, err := shellx.Command(ctx, root, command)
	if err != nil {
		return nil, err
	}
	shellx.InGroup(c)
	c.Env = gitx.Environ(env...)
	if run != nil {
		c.Env = run.Env(c.Env)
	}
	o := &oneCommand{key: key, c: c, printed: shellx.NewTail(keep), stdout: shellx.NewTail(numberLimit)}
	both := io.Writer(o.printed)
	if out != nil {
		both = io.MultiWriter(o.printed, out)
	}
	c.Stdout, c.Stderr = io.MultiWriter(o.stdout, both), both
	return o, nil
}

// inOrder makes both streams one writer, so that what the command printed
// keeps the order it was written in: with two writers exec reads each
// stream on its own, and the lines of one interleave with the other's as the
// reads happen to fall. It is for a command whose standard output no
// threshold reads, which then has none of its own.
func (o *oneCommand) inOrder() *oneCommand {
	o.c.Stdout, o.c.Stderr = o.printed, o.printed
	return o
}

// run runs the command to its end and says how it ended.
func (o *oneCommand) run(ctx context.Context) ranCommand {
	ran := ranCommand{key: o.key}
	started := time.Now()
	ran.err = shellx.Run(o.c)
	ran.duration = time.Since(started)
	// The command may have done anything to the repository.
	gitx.Forget(ctx)
	var exited *exec.ExitError
	switch {
	case errors.As(ran.err, &exited):
		ran.exit = exited.ExitCode()
	case errors.Is(ran.err, exec.ErrWaitDelay):
		// Something it started held its output open after it had finished:
		// what counts is how the command itself ended.
		ran.exit = o.c.ProcessState.ExitCode()
	case ran.err != nil:
		ran.startErr = ran.err
	}
	ran.printed, ran.stdout = o.printed.String(), o.stdout.String()
	return ran
}

// runOne makes command and runs it, as prepareOne and run do.
func runOne(ctx context.Context, root, key, command string, run *shellx.RunDir, out io.Writer, keep int,
	env ...string) ranCommand {
	o, err := prepareOne(ctx, root, key, command, run, out, keep, env...)
	if err != nil {
		return ranCommand{key: key, startErr: err, err: err}
	}
	return o.run(ctx)
}

// setRun is one run of the command set: what passed, and the command that
// did not, with why in passes' words, where one did not.
type setRun struct {
	passed []ranCommand
	failed *ranCommand
	why    string
}

// results is what the run passed, as the record keeps it.
func (r setRun) results(cfg config.Config) []model.CommandRun {
	out := make([]model.CommandRun, 0, len(r.passed))
	for _, c := range r.passed {
		out = append(out, c.result(cfg))
	}
	return out
}

// setCommands is each key of landingCommands that cfg sets, in that order,
// with its command trimmed.
func setCommands(cfg config.Config) [][2]string {
	var set [][2]string
	for _, key := range landingCommands {
		if command := strings.TrimSpace(cfg.Commands[key]); command != "" {
			set = append(set, [2]string{key, command})
		}
	}
	return set
}

// commandSet is what runSet is given: where to run, what with, and what to
// do around each command.
type commandSet struct {
	root string
	cfg  config.Config
	run  *shellx.RunDir
	out  io.Writer
	keep int
	// first, where it is set, is the first command, made already by
	// prepareOne while the caller held the project's lock.
	first *oneCommand
	// before is called before each command, and after after each that
	// passed; an error from after ends the run with it.
	before func(key string)
	after  func(key string) error
	// written, where it is set, is called after each command with how it
	// ended, so that a caller can put a line of its own in out.
	written func(ranCommand)
}

// runSet runs every command cfg sets of landingCommands, in that order, each
// held to passes, and stops at the first that does not pass.
func runSet(ctx context.Context, set commandSet) (setRun, error) {
	var r setRun
	for _, kc := range setCommands(set.cfg) {
		key := kc[0]
		ran := set.one(ctx, key, kc[1])
		if why, ok := set.judged(key, ran); !ok {
			return setRun{passed: r.passed, failed: &ran, why: why}, nil
		}
		r.passed = append(r.passed, ran)
		if set.after != nil {
			if err := set.after(key); err != nil {
				return r, err
			}
		}
	}
	return r, nil
}

// one runs key's command, the one prepared first where it is key's, with the
// set's lines before and after it.
func (set *commandSet) one(ctx context.Context, key, command string) ranCommand {
	if set.before != nil {
		set.before(key)
	}
	var ran ranCommand
	if set.first != nil && set.first.key == key {
		ran, set.first = set.first.run(ctx), nil
	} else {
		ran = runOne(ctx, set.root, key, command, set.run, set.out, set.keep)
	}
	if set.written != nil {
		set.written(ran)
	}
	return ran
}

// judged is why ran did not pass, and whether it did.
func (set *commandSet) judged(key string, ran ranCommand) (why string, ok bool) {
	if ran.startErr != nil {
		return "it could not be run: " + ran.startErr.Error(), false
	}
	ok, why = passes(key, ran.exit, ran.stdout, thresholdOf(set.cfg, key))
	return why, ok
}

// commandDigest is the hex SHA-256 of key, its command as trimmed, and the
// threshold in force for it, or nothing where it has none. The record keeps
// it and never the text, because a command can hold a token and a record is
// committed. Each field goes in as its length, a colon and itself: run
// together, `echo 9` held to 50 and `echo 95` held to 0 are one input
// (security F-6).
func commandDigest(cfg config.Config, key string) string {
	threshold := ""
	if t := thresholdOf(cfg, key); t > 0 {
		threshold = strconv.FormatFloat(t, 'g', -1, 64)
	}
	sum := sha256.New()
	for _, field := range []string{key, strings.TrimSpace(cfg.Commands[key]), threshold} {
		writeField(sum, field)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func writeField(h hash.Hash, field string) {
	fmt.Fprintf(h, "%d:%s", len(field), field)
}

// resultHolds reports whether the implementation gate's result counts for
// tree under cfg: the gate is passed, its tree is tree, and it ran exactly
// the commands cfg sets, in order, as cfg sets them. A result with no tree or
// no commands is no result, never a pass (THREATS B1). sdlc status asks it
// with the checkout's configuration and the landing with trunk's, so the two
// read a result the same way.
func resultHolds(g model.GateResult, cfg config.Config, tree string) bool {
	if g.Status != model.GatePass || g.TreeHash == "" || g.TreeHash != tree || len(g.Commands) == 0 {
		return false
	}
	set := setCommands(cfg)
	return slices.EqualFunc(g.Commands, set, func(c model.CommandRun, kc [2]string) bool {
		return c.Name == commandsPrefix+kc[0] && c.Digest == commandDigest(cfg, kc[0])
	})
}

// thresholdOf is the threshold a command's last number is held to, where it
// has one.
func thresholdOf(cfg config.Config, key string) float64 {
	switch key {
	case commandCoverage:
		return cfg.Thresholds.CoverageMin
	case commandMutation:
		return cfg.Thresholds.MutationMin
	}
	return 0
}

// passes is the pass rule: the command exits 0, and coverage and mutation, held
// to a threshold above zero, print on standard output a last number at least
// that. Standard error is shown but never searched. It says how the command
// failed where it did.
func passes(key string, exit int, stdout string, threshold float64) (bool, string) {
	switch {
	case exit < 0:
		return false, "it was stopped before it finished"
	case exit != 0:
		return false, fmt.Sprintf("it exited with status %d", exit)
	case key != commandCoverage && key != commandMutation || threshold <= 0:
		return true, ""
	}
	name := "thresholds." + key + "_min"
	n, ok := lastNumber(stdout)
	if !ok {
		return false, fmt.Sprintf("it printed no number on standard output, and %s is %s", name, number(threshold))
	}
	if n < threshold {
		return false, fmt.Sprintf("the last number it printed on standard output is %s, below %s of %s",
			number(n), name, number(threshold))
	}
	return true, ""
}

var numberPattern = regexp.MustCompile(`[0-9]+(\.[0-9]+)?`)

// lastNumber is the last number in text.
func lastNumber(text string) (float64, bool) {
	found := numberPattern.FindAllString(text, -1)
	if len(found) == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(found[len(found)-1], 64)
	return n, err == nil
}
