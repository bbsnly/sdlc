package cli

// US-026 AC-10: where the configuration does not turn the staffing on, every
// gate waits for exactly the reviewers the released tool requires, and no
// command's output changes.
//
// "No output changes" is held against the output itself, not against a
// description of it. The files under testdata/staffing-off were written by the
// tree as it stood before the story (c7bb9d8), from the walk below: one story
// taken from `sdlc init` to `sdlc ack`, with a refusal at each reviewed gate
// and a hand-over to a person, every command in prose in one project and with
// --json in another, and the story's record as it is on disk at the end. They are compared byte for byte
// after what differs between two runs is replaced: where the project is, the
// time, and the names of commits and trees.
//
// US-002: the configuration `sdlc init` writes, which the walk prints, now has
// a comment block above each setting, and the files still hold it as the tree
// before US-026 wrote it. Their wording is not what AC-10 is about, so both the
// golden and the walk are compared with the top-level `_` keys of that one file
// cut out (stfWithoutConfigComments, in configcomments_test.go). The files are
// untouched, and every other byte of both transcripts is still compared as it is.
//
// test/e2e's TestAC2EveryCommandThatNeedsNoPersonRunsToTheEndInTheSandbox holds
// the prose of the built binary the same way, with story worktrees on, and was
// written before this story too.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var (
	stfTimePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
	stfShaPattern  = regexp.MustCompile(`\b[0-9a-f]{40,64}\b|\b[0-9a-f]{12}\b`)
)

// stfTranscript is what a run of commands printed, in the order they ran.
type stfTranscript struct {
	t     *testing.T
	root  string
	json  bool
	paths []struct{ path, name string }
	out   strings.Builder
}

// normalise replaces what differs between two runs. Each path is replaced as
// it is, slash-separated, and as %q prints either, so that a Windows path a
// message quotes is found too.
func (tr *stfTranscript) normalise(text string) string {
	for _, p := range tr.paths {
		for _, spelling := range []string{p.path, filepath.ToSlash(p.path)} {
			quoted := strconv.Quote(spelling)
			text = strings.ReplaceAll(text, quoted[1:len(quoted)-1], p.name)
			text = strings.ReplaceAll(text, spelling, p.name)
		}
	}
	text = stfTimePattern.ReplaceAllString(text, "<time>")
	return stfShaPattern.ReplaceAllString(text, "<sha>")
}

// run is one command, with --json after it in the project that asks for JSON.
func (tr *stfTranscript) run(stdin string, args ...string) {
	tr.t.Helper()
	if tr.json {
		args = append(append([]string{}, args...), "--json")
	}
	r := runWith(tr.t, stdin, args...)
	tr.out.WriteString("$ sdlc " + strings.Join(args, " ") + "\n")
	tr.out.WriteString("exit " + strconv.Itoa(r.code) + "\n")
	tr.out.WriteString("stdout:\n" + tr.normalise(r.stdout))
	tr.out.WriteString("stderr:\n" + tr.normalise(r.stderr))
	tr.out.WriteString("\n")
}

// reads is every command that only reads, at one point of the walk.
func (tr *stfTranscript) reads() {
	tr.t.Helper()
	for _, args := range [][]string{
		{"status"},
		{"story", "list"},
		{"artifact", "list"},
		{"review", "list"},
		{"review", "list", "--gate", "design_review"},
		{"review", "list", "--gate", "code_review", "--story", "US-001"},
		{"log"},
		{"log", "--since", "HEAD"},
		{"cost"},
		{"cost", "--story", "US-001"},
		{"doctor"},
	} {
		tr.run("", args...)
	}
}

// file is a file of the loop's own, as it is on disk.
func (tr *stfTranscript) file(rel string) {
	tr.t.Helper()
	raw, err := os.ReadFile(filepath.Join(tr.root, filepath.FromSlash(rel)))
	if err != nil {
		tr.t.Fatalf("%s: %v", rel, err)
	}
	tr.out.WriteString("== " + rel + "\n" + tr.normalise(string(raw)) + "\n")
}

// stfOffWalk takes one story from `sdlc init` to `sdlc ack` in a project that
// says nothing about the staffing, and returns everything the commands printed.
func stfOffWalk(t *testing.T, asJSON bool) string {
	t.Helper()
	root := gitProject(t)
	// doctor reports the binary the hook would run: one of the test's own,
	// named outright, so that what is installed on this machine is not read.
	name := "sdlc"
	if runtime.GOOS == "windows" {
		name = "sdlc.exe"
	}
	bin := filepath.Join(wtTempDir(t), name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := wtTempDir(t)
	setenv(t, "SDLC_BIN", bin)
	setenv(t, "HOME", home)
	setenv(t, "LOCALAPPDATA", home)

	tr := &stfTranscript{t: t, root: root, json: asJSON}
	tr.paths = []struct{ path, name string }{{bin, "<bin>"}, {home, "<home>"}, {root, "<root>"}}

	// No stack, so no commands: a configured program doctor would look for on
	// this machine's PATH is one the output would then depend on.
	tr.run("", "init")
	tr.file(".sdlc/config.json")
	worktreesOff(t, root)
	commitEverything(t, root)

	tr.reads()
	tr.run("", "start")
	tr.reads()

	tr.run("", "gate", "dor", "pass", "--note", "the criteria are testable")
	tr.run("", "gate", "analysis", "pass")
	tr.run("# Analysis\n", "artifact", "write", "analysis")
	tr.run("# Threats\n", "artifact", "write", "threats")
	tr.run("", "gate", "analysis", "pass", "--security-sensitive=false")

	writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// AC-1\n")
	tr.run("# Test plan\n", "artifact", "write", "test_plan")
	tr.run("", "freeze")
	tr.run("", "gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test")
	tr.run("# Plan\n", "artifact", "write", "plan")
	tr.run("", "gate", "plan", "pass", "--note", "one step")

	// Gate 4: nobody has reviewed, then the architect blocks, then everybody
	// reports and the gate passes.
	tr.run("", "gate", "design_review", "pass", "--note", "nobody objected")
	tr.run("# architect\n", "review", "add", "design_review", "architect", "block", "--note", "AC-1 has no step")
	tr.run("", "gate", "design_review", "pass")
	tr.run("# nobody\n", "review", "add", "design_review", "code-reviewer", "approve")
	tr.run("# architect\n", "review", "add", "design_review", "architect", "approve")
	tr.run("# red-team\n", "review", "add", "design_review", "red-team", "note", "--note", "nothing to add")
	tr.run("# security\n", "review", "add", "design_review", "security", "approve")
	tr.run("# perf\n", "review", "add", "design_review", "perf", "approve")
	tr.run("", "review", "list")
	tr.run("", "gate", "design_review", "pass")
	tr.run("# human-advocate\n", "review", "add", "design_review", "human-advocate", "approve")
	tr.run("", "gate", "design_review", "pass", "--note", "approved")

	writeFile(t, root, "internal/invoice.go", "package internal\n\nfunc Invoice() {}\n")
	tr.run("", "gate", "implementation", "pass", "--note", "frozen tests green")
	tr.run("# Verification\n", "artifact", "write", "verification")
	tr.run("", "gate", "verification", "pass", "--note", "all commands green")
	tr.run("", "gate", "verifier_review", "pass")
	tr.run("# verifier\n", "review", "add", "verifier_review", "verifier", "approve")
	tr.run("", "gate", "verifier_review", "pass", "--note", "approved")

	// Gate 7: every listed reviewer is waited for, the advisory ones too.
	tr.run("# code-reviewer\n", "review", "add", "code_review", "code-reviewer", "approve")
	tr.run("", "gate", "code_review", "pass")
	tr.run("# security\n", "review", "add", "code_review", "security", "note")
	tr.run("# perf\n", "review", "add", "code_review", "perf", "approve")
	tr.run("", "gate", "code_review", "pass")
	tr.run("# human-advocate\n", "review", "add", "code_review", "human-advocate", "approve")
	tr.reads()
	tr.run("", "gate", "code_review", "pass", "--note", "approved")
	tr.run("", "cost", "add", "--usd", "1.25")

	// A hand-over to a person, and their answer.
	tr.run("", "escalate", "spec_unclear", "--message", "is a zero total refused too?")
	tr.reads()
	tr.run("", "approve", "US-001")
	tr.run("", "start")

	tr.run("", "gate", "commit", "pass", "--note", "not yet")
	commitEverything(t, root)
	tr.run("", "gate", "commit", "pass", "--note", "committed")
	tr.run("# Retro\n", "artifact", "write", "retro")
	tr.run("", "gate", "retro", "pass", "--note", "no deviations")
	tr.reads()
	tr.file(".sdlc/stories/US-001/gate-record.json")
	tr.run("", "stop")
	tr.reads()
	tr.run("", "ack", "--through", "HEAD")
	tr.run("", "log")
	return tr.out.String()
}

// stfGolden is one file under testdata/staffing-off.
func stfGolden(name string) string {
	return filepath.Join("testdata", "staffing-off", name)
}

func TestAC10WithNoStaffingSectionEveryCommandPrintsWhatItPrintedBeforeTheStory(t *testing.T) {
	t.Parallel()
	for _, form := range []struct {
		name, file string
		json       bool
	}{
		{"AC_10 in prose", "prose.golden", false},
		{"AC_10 with --json", "json.golden", true},
	} {
		t.Run(form.name, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(stfGolden(form.file))
			if err != nil {
				t.Fatalf("the output the tool printed before the story is not there to compare with: %v", err)
			}
			got := stfWithoutConfigComments(t, stfOffWalk(t, form.json))
			wanted := stfWithoutConfigComments(t, string(want))
			if got == wanted {
				return
			}
			gotLines, wantLines := strings.Split(got, "\n"), strings.Split(wanted, "\n")
			for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
				var g, w string
				if i < len(gotLines) {
					g = gotLines[i]
				}
				if i < len(wantLines) {
					w = wantLines[i]
				}
				if g != w {
					from := max(0, i-12)
					t.Fatalf("with no staffing section the output changed at line %d of %s:\n got  %q\n want %q\n\nthe lines before it:\n%s",
						i+1, form.file, g, w, strings.Join(gotLines[from:i], "\n"))
				}
			}
		})
	}
}

// A section that is there and off is the same as no section: nothing is asked
// of the story and nothing more is printed.
func TestAC10AStaffingSectionThatIsOffIsTheSameAsNone(t *testing.T) {
	t.Parallel()
	for _, section := range []map[string]any{
		{"enabled": false},
		{"enabled": false, "models": []string{"haiku", "opus"}, "efforts": []string{"low", "high"},
			"lowest":     map[string]any{"verifier": map[string]string{"model": "opus", "effort": "high"}},
			"bring_back": map[string]any{"security": []string{"internal/"}}},
	} {
		root := gitProjectInitialised(t)
		before := run(t, "doctor", "--json").stdout
		setConfigOnDisk(t, root, "staffing", section)
		commitEverything(t, root)
		if after := run(t, "doctor", "--json").stdout; after != before {
			t.Errorf("doctor reads a staffing section that is off as something to report:\n%s\nbefore the section:\n%s", after, before)
		}
		mustRun(t, "start")
		// Every gate passes with nothing recorded about the staffing, on the
		// reviews the released tool asks for and no others.
		reach(t, root, "")
		for _, args := range [][]string{{"status", "--json"}, {"review", "list", "--json"}, {"log", "--json"}} {
			var doc map[string]any
			out := mustRun(t, args...).stdout
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "staffing") || strings.Contains(out, "left_out") || strings.Contains(out, "asked_") {
				t.Errorf("sdlc %s says something about the staffing in a project that has it off:\n%s", strings.Join(args, " "), out)
			}
		}
		raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "staffing") {
			t.Errorf("the record of a story worked with the staffing off holds one:\n%s", raw)
		}
	}
}
