// Command testtime reads `go test -json` on stdin and writes what go test would
// have printed without -json, then each package's time and the slowest tests.
// `./task test` runs it.
//
// go test gives a package a limit, -timeout, and kills it there. The time a
// package takes is printed on its ok line and nowhere else, so a suite that
// grew towards its limit said nothing until the day a loaded runner tipped it
// over, and then said only which test was running when the limit came: the
// limit had been raised from ten minutes to twenty that way. Given the limit,
// this names every package that took more than half of it, while there is
// still room to find out why.
//
// The output is go test's own without -json, rebuilt from the events: a
// passing test says nothing, a failing one says what it logged under its
// --- FAIL line, nested as go nests it, and the ok and FAIL lines are kept as
// they are, so the annotation in `./task test` reads the same lines. What a
// passing test in a failing package printed to stdout itself is not kept:
// -json gives it to the test, as it gives a log line, and the two cannot be
// told apart. A line that
// is not JSON, such as an error from go itself, is passed through as it came.
package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// event is the part of a test2json event this reads.
type event struct {
	Action     string  `json:"Action"`
	Package    string  `json:"Package"`
	Test       string  `json:"Test"`
	Elapsed    float64 `json:"Elapsed"`
	Output     string  `json:"Output"`
	OutputType string  `json:"OutputType"`
}

// The actions this reads, as test2json names them.
const (
	passed  = "pass"
	failed  = "fail"
	skipped = "skip"
	output  = "output"
)

// entry is one line a test or a package printed, or the place where one of
// its subtests (or, for a package, a test) ended.
type entry struct {
	text  string
	child *node
}

// node is a test, or with name "" the package itself.
type node struct {
	name     string
	entries  []entry
	children []*node // in the order they started
	result   string  // "pass", "fail" or "skip"; "" while it runs
	placed   bool    // it has an entry in its parent
	frame    string  // its own --- line
}

// pkg is what has been read of one package.
type pkg struct {
	root   node
	tests  map[string]*node
	done   bool
	result string
}

// timed is a package or a test, with its time.
type timed struct {
	pkg, test string
	elapsed   time.Duration
}

// report is everything read.
type report struct {
	out      io.Writer
	pkgs     map[string]*pkg
	order    []string
	packages []timed
	tests    []timed
}

// run exits 0 once it has read to the end, 1 when stdin cannot be read, and 2
// for words it does not take. Whether the tests passed is go test's exit, not
// this one's.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("testtime", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	top := flags.Int("n", 20, "how many of the slowest tests to name")
	limit := flags.Duration("limit", 0, "the -timeout go test was given")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 || *top < 0 || *limit < 0 {
		fmt.Fprintln(stderr, "testtime: takes only -n <count> and -limit <duration>, and reads go test -json on stdin")
		return 2
	}
	r := &report{out: stdout, pkgs: map[string]*pkg{}}
	if err := r.read(stdin); err != nil {
		fmt.Fprintf(stderr, "testtime: reading go test -json: %v\n", err)
		return 1
	}
	r.summary(*top, *limit)
	return 0
}

// read takes events until the end, printing each package's output when the
// package ends. A package that never ended (go test was killed, or the input
// stopped) is printed at the end.
func (r *report) read(in io.Reader) error {
	br := bufio.NewReader(in)
	for {
		raw, err := br.ReadString('\n')
		if raw != "" {
			r.take(raw)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	for _, name := range r.order {
		if p := r.pkgs[name]; !p.done {
			r.flush(p)
		}
	}
	return nil
}

// take reads one line of input.
func (r *report) take(raw string) {
	var e event
	if !strings.HasPrefix(raw, "{") || json.Unmarshal([]byte(raw), &e) != nil {
		fmt.Fprint(r.out, withNewline(raw))
		return
	}
	switch {
	case e.Action == "build-output":
		fmt.Fprint(r.out, e.Output)
	case e.Package == "":
	case e.Test == "":
		r.packageEvent(e)
	default:
		r.testEvent(e)
	}
}

// packageEvent is an event of the package's own.
func (r *report) packageEvent(e event) {
	p := r.pkg(e.Package)
	switch e.Action {
	case output:
		// A test still running when the package speaks again is one go test
		// will not finish: it timed out or panicked. Its lines go here, before
		// the FAIL line, as go prints them.
		p.root.placeUnfinished()
		p.root.entries = append(p.root.entries, entry{text: e.Output})
	case passed, failed, skipped:
		p.done, p.result = true, e.Action
		if e.Action != skipped {
			r.packages = append(r.packages, timed{pkg: e.Package, elapsed: seconds(e.Elapsed)})
		}
		r.flush(p)
	}
}

// testEvent is an event of one test's.
func (r *report) testEvent(e event) {
	p := r.pkg(e.Package)
	n := p.test(e.Test)
	switch e.Action {
	case output:
		if e.OutputType == "frame" && strings.HasPrefix(e.Output, "--- ") {
			n.frame = e.Output
		} else if !strings.HasPrefix(e.Output, "=== ") {
			n.entries = append(n.entries, entry{text: e.Output})
		}
	case passed, failed, skipped:
		n.result = e.Action
		p.parent(e.Test).place(n)
		if !strings.Contains(e.Test, "/") {
			r.tests = append(r.tests, timed{pkg: e.Package, test: e.Test, elapsed: seconds(e.Elapsed)})
		}
	}
}

func (r *report) pkg(name string) *pkg {
	p, ok := r.pkgs[name]
	if !ok {
		p = &pkg{tests: map[string]*node{}}
		r.pkgs[name] = p
		r.order = append(r.order, name)
	}
	return p
}

// test is the node of the test named, made the first time it is named.
func (p *pkg) test(name string) *node {
	n, ok := p.tests[name]
	if !ok {
		n = &node{name: name}
		p.tests[name] = n
		parent := p.parent(name)
		parent.children = append(parent.children, n)
	}
	return n
}

// parent is the node of the nearest test whose name, and a slash, start
// name's, or the package's. A subtest's own name can hold a slash, so the
// nearest known one is taken rather than the text before the last slash.
func (p *pkg) parent(name string) *node {
	for i := strings.LastIndexByte(name, '/'); i > 0; i = strings.LastIndexByte(name[:i], '/') {
		if n, ok := p.tests[name[:i]]; ok {
			return n
		}
	}
	return &p.root
}

// place puts child's report at this point in n's, as go does when a test ends.
func (n *node) place(child *node) {
	if !child.placed {
		child.placed = true
		n.entries = append(n.entries, entry{child: child})
	}
}

// placeUnfinished places every child of n that started and has not ended.
func (n *node) placeUnfinished() {
	for _, c := range n.children {
		if c.result == "" {
			n.place(c)
		}
	}
}

// flush prints the package as go test without -json would. A package that
// passed or had no tests prints its last line, ok or ?, and nothing it said
// before it. One that did not prints its own lines and the report of every
// test that did not pass or skip, but not the PASS line, which only -v prints.
func (r *report) flush(p *pkg) {
	p.root.placeUnfinished()
	quiet := p.result == passed || p.result == skipped
	for i, e := range p.root.entries {
		switch {
		case quiet && i < len(p.root.entries)-1:
		case e.child != nil:
			r.block(e.child, 0)
		case e.text == "PASS\n":
		default:
			fmt.Fprint(r.out, e.text)
		}
	}
	p.root = node{}
	p.tests = map[string]*node{}
}

// block prints a test that did not pass, its --- line first and what it
// logged under it, its subtests a level further in. -json indents every log
// line by four spaces whatever the depth; go test without it indents four
// more for each level.
func (r *report) block(n *node, depth int) {
	if n.result == passed || n.result == skipped {
		return
	}
	indent := strings.Repeat("    ", depth)
	if n.frame != "" {
		fmt.Fprint(r.out, indent+n.frame)
	}
	n.placeUnfinished()
	for _, e := range n.entries {
		if e.child != nil {
			r.block(e.child, depth+1)
		} else {
			fmt.Fprint(r.out, indent+e.text)
		}
	}
}

// summary prints the packages slowest first, the slowest top tests, and every
// package over half of limit.
func (r *report) summary(top int, limit time.Duration) {
	if len(r.packages) == 0 {
		return
	}
	slowest := func(a, b timed) int { return cmp.Compare(b.elapsed, a.elapsed) }
	slices.SortStableFunc(r.packages, slowest)
	slices.SortStableFunc(r.tests, slowest)

	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "time per package, slowest first:")
	for _, p := range r.packages {
		fmt.Fprintf(r.out, "  %7.2fs  %s\n", p.elapsed.Seconds(), p.pkg)
	}
	if n := min(top, len(r.tests)); n > 0 {
		fmt.Fprintf(r.out, "the %d slowest tests:\n", n)
		for _, t := range r.tests[:n] {
			fmt.Fprintf(r.out, "  %7.2fs  %s %s\n", t.elapsed.Seconds(), t.pkg, t.test)
		}
	}
	if limit == 0 {
		return
	}
	for _, p := range r.packages {
		if p.elapsed > limit/2 {
			fmt.Fprintf(r.out, "testtime: %s took %.2fs, more than half of go test's limit of %s. "+
				"It passed, but a loaded machine can take twice as long: profile it with "+
				"`./task profile PKG=<its directory>` and make it faster, as CONTRIBUTING.md says, before it reaches the limit.\n",
				p.pkg, p.elapsed.Seconds(), limit)
		}
	}
}

// seconds is a test2json Elapsed as a duration.
func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

func withNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
