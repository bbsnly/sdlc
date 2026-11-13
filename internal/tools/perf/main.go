// Command perf times every hook event and every command of the loop as a
// session pays for them: the built binary, run as a process, in a sandbox
// whose configured commands do nothing, and reports the wall time, the CPU
// time and the peak resident memory of each. `./task perf-report` prints the
// figures, with a base binary's beside them where one is given.
//
//	perf report -runs N [-base <binary>]
//
// It is a program and not a benchmark because what it measures is the
// process: `./task bench` times the hook's and the commands' work in one
// process and leaves out the start-up, the exec and the launcher's shell, and
// a session pays for all four on every tool call (US-129, AC-11).
// The sandbox is one fixed path for a run of the tool, put back from a
// snapshot before every measured run, so that each run reads the disk a fresh
// call reads and none reads what the one before it wrote: the backlog of 130
// stories, the 190 KB gate record and the 2,000-line trace are the AC-10
// benchmarks' sizes.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// The three measures of a run, as the columns name them.
const (
	wall = "wall"
	cpu  = "cpu"
	rss  = "rss"
)

var allMeasures = []string{wall, cpu, rss}

// usage is what the program says to arguments it does not take: the task,
// because it is what a contributor types.
const usage = "sdlc: perf was given something it does not take, so nothing was measured: %s\n" +
	"      Run it as ./task perf-report (RUNS=N, BASE=<binary>), which hands it its flags:\n" +
	"      report -runs N [-base <binary>].\n"

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, usage, "no argument at all, where the first word is report")
		return 2
	}
	if args[0] != "report" {
		fmt.Fprintf(stderr, usage, strconv.Quote(args[0])+", where the first word is report")
		return 2
	}
	flags := flag.NewFlagSet("perf", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runs := flags.Int("runs", 0, "")
	base := flags.String("base", "", "")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintf(stderr, usage, err)
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, usage, "an argument that is not a flag: "+flags.Arg(0))
		return 2
	}
	if *runs < 1 {
		fmt.Fprintf(stderr, usage, fmt.Sprintf("-runs=%d; it is a whole number from 1", *runs))
		return 2
	}
	// An interrupt ends the child that is running; the sandbox goes with the
	// temporary directory either way.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sb, err := newSandbox(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "sdlc: perf measured nothing: %v\n", err)
		return 1
	}
	defer sb.close()
	results, err := sb.measure(ctx, *runs, *base)
	if err != nil {
		fmt.Fprintf(stderr, "sdlc: perf measured nothing more: %v\n", err)
		return 1
	}
	report(stdout, results, *runs, *base, sb)
	return 0
}

// measures is what one run of a name came to: the process's wall time, its CPU
// time (user and system, with its children's where the platform counts them)
// and its peak resident set in kilobytes, which Windows does not report.
type measures struct {
	wall, cpu time.Duration
	rss       int64
	rssKnown  bool
}

// result is every run of one name, the result binary's and the base's.
type result struct {
	name name
	runs []measures
	base []measures
}

// percentile is the nearest-rank p-th percentile of a sorted slice: p50 is
// the median, p95 the figure 95% of runs were at or under.
func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := (p*len(sorted) + 99) / 100
	return sorted[max(i-1, 0)]
}

// column is one measure of one name over its runs, sorted.
func column(runs []measures, measure string) []float64 {
	out := make([]float64, 0, len(runs))
	for _, m := range runs {
		switch measure {
		case wall:
			out = append(out, ms(m.wall))
		case cpu:
			out = append(out, ms(m.cpu))
		case rss:
			out = append(out, float64(m.rss))
		}
	}
	sort.Float64s(out)
	return out
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// report prints a table: for each name, p50 and p95 of each measure, the base's
// p50 and the change where a base was run, then the start-up split of one
// PreToolUse call.
func report(w io.Writer, results []result, runs int, base string, sb *sandbox) {
	fmt.Fprintf(w, "perf-report: %d runs of each name, the result built from %s", runs, sb.checkout)
	if base != "" {
		fmt.Fprintf(w, ", the base %s", base)
	} else {
		fmt.Fprint(w, ", no base binary (BASE=<binary> gives one)")
	}
	fmt.Fprintf(w, "\n%s\n", sandboxSizes)
	fmt.Fprintf(w, "CPU is the process's user and system time%s; peak memory is the peak resident set%s.\n",
		cpuNote, rssNote)
	fmt.Fprintf(w, "%-38s %-9s %22s %22s %22s", "name", "class", "wall ms (p50, p95)", "cpu ms (p50, p95)", "peak rss KB (p50, p95)")
	if base != "" {
		fmt.Fprintf(w, " %34s", "base p50 wall, cpu, rss (change)")
	}
	fmt.Fprintln(w)
	for _, r := range results {
		fmt.Fprintf(w, "%-38s %-9s", r.name.name, r.name.class)
		for _, measure := range allMeasures {
			fmt.Fprintf(w, " %22s", pair(r.runs, measure))
		}
		if base != "" {
			fmt.Fprintf(w, " %34s", changes(r.runs, r.base))
		}
		fmt.Fprintln(w)
	}
	// The split of one call: what the binary costs to start, what the hook's
	// decision adds, and what the launcher's shell adds around both.
	version, direct, launcher := sb.split(results)
	fmt.Fprintf(w, "start-up split of one PreToolUse call, p50 wall: sdlc version %s ms (the binary's start-up); "+
		"PreToolUse Bash %s ms (%+.1f ms against it)", one(version), one(direct), direct-version)
	if launcher > 0 {
		fmt.Fprintf(w, "; through plugin/bin/sdlc-hook %s ms (the launcher's shell adds %+.1f ms)", one(launcher), launcher-direct)
	} else {
		fmt.Fprint(w, "; the launcher's shell is not timed on this platform")
	}
	fmt.Fprintln(w)
}

// pair is "p50, p95" of one measure, or that the measure is not reported.
func pair(runs []measures, measure string) string {
	if measure == rss && (len(runs) == 0 || !runs[0].rssKnown) {
		return "not reported"
	}
	c := column(runs, measure)
	if measure == rss {
		return fmt.Sprintf("%d, %d", int64(percentile(c, 50)), int64(percentile(c, 95)))
	}
	return one(percentile(c, 50)) + ", " + one(percentile(c, 95))
}

// changes is the base's p50 of each measure and the result's change from it.
func changes(runs, base []measures) string {
	var parts []string
	for _, measure := range allMeasures {
		if measure == rss && (len(base) == 0 || !base[0].rssKnown) {
			parts = append(parts, "n/a")
			continue
		}
		was, now := percentile(column(base, measure), 50), percentile(column(runs, measure), 50)
		shown := one(was)
		if measure == rss {
			shown = strconv.FormatInt(int64(was), 10)
		}
		if was > 0 {
			shown += fmt.Sprintf(" (%+.0f%%)", (now-was)/was*100)
		}
		parts = append(parts, shown)
	}
	return strings.Join(parts, "; ")
}

// one is a figure in milliseconds to one decimal, with a full stop whatever
// the locale.
func one(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

// sandboxSizes is what the sandbox holds, stated with every report so that a
// figure is read against the sizes it was measured on.
const sandboxSizes = "sandbox: a backlog of 130 stories (about 10,000 lines), the story's gate record at 190 KB, its trace at 2,000 lines; " +
	"the configured commands do nothing, so nothing here times an agent, a model or a project's configured command"

// kinds orders names by the sandbox kind each needs, so that each kind is
// built once.
func kinds(names []name) []string {
	var out []string
	for _, n := range names {
		if !slices.Contains(out, n.on) {
			out = append(out, n.on)
		}
	}
	return out
}
