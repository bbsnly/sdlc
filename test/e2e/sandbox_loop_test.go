// US-067, the loop run in a sandbox with nobody at the keyboard: every command
// that needs no person, with what it prints and how it exits (AC-2); the steps
// that do need one, performed by this process from outside the session under
// test, and still refused to that session (AC-3); and the hook, given the JSON
// Claude Code sends and held to what it prints (AC-4). sandbox_test.go has the
// helpers; cmd/sdlc has AC-4's forced panic, which no built binary has a way
// to cause.

package e2e

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// ran is one command of the session under test, and what it has to print and
// exit with, as normalise leaves it.
type ran struct {
	dir, agent, stdin string
	args              []string
	stdout, stderr    string
	code              int
}

// holds runs each command, the hook asked first, and reports every one that
// printed or exited otherwise.
func (sb sandbox) holds(t *testing.T, steps ...ran) {
	t.Helper()
	for _, s := range steps {
		r := sb.does(t, s.dir, s.agent, s.stdin, s.args...)
		stdout, stderr := sb.normalise(r.stdout), sb.normalise(r.stderr)
		if stdout != s.stdout || stderr != s.stderr || r.code != s.code {
			t.Errorf("sdlc %s:\ngot  exit %d\nstdout %q\nstderr %q\nwant exit %d\nstdout %q\nstderr %q",
				strings.Join(s.args, " "), r.code, stdout, stderr, s.code, s.stdout, s.stderr)
		}
	}
}

// works is a command of the session under test that has to exit 0.
func (sb sandbox) works(t *testing.T, dir, agent, stdin string, args ...string) string {
	t.Helper()
	r := sb.does(t, dir, agent, stdin, args...)
	if r.code != 0 {
		t.Fatalf("sdlc %s exited %d:\n%s%s", strings.Join(args, " "), r.code, r.stdout, r.stderr)
	}
	return r.stdout
}

// nextGate is the gate `sdlc status --json` says comes next in the story's
// worktree, and "" once every gate has passed.
func (sb sandbox) nextGate(t *testing.T) string {
	t.Helper()
	var status struct {
		Active   string `json:"active"`
		NextGate string `json:"next_gate"`
	}
	stdout, stderr, code := sb.outcome(t, sandboxSession, sb.tree(), "", "status", "--json")
	if code != 0 {
		t.Fatalf("sdlc status --json exited %d:\n%s%s", code, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("sdlc status --json printed something that is not JSON: %v\n%s", err, stdout)
	}
	if status.Active == "" {
		return ""
	}
	return status.NextGate
}

// The documents the session under test stores, and the two files it writes.
const (
	sandboxAnalysis     = "# Analysis\n\nAC-1 is testable.\n"
	sandboxThreats      = "# Threats\n\nNothing crosses a trust boundary.\n"
	sandboxTestPlan     = "# Test plan\n\nAC-1 -> TestRejectsZero\n"
	sandboxPlan         = "# Plan\n\nOne step.\n"
	sandboxVerification = "# Verification\n\nEvery command green.\n"
	sandboxRetro        = "# Retro\n\nNo deviations.\n"
	sandboxTest         = "package main\n\n// AC-1\n"
	sandboxCode         = "package main\n\nfunc Invoice() {}\n"
)

// worksGate does what the runbook says at one gate, in the story's worktree,
// as whoever it delegates the gate to, and records the outcome. Every command
// and every write is put to the hook first.
func (sb sandbox) worksGate(t *testing.T, gate model.Gate) {
	t.Helper()
	tree := sb.tree()
	switch gate {
	case model.GateAnalysis:
		sb.works(t, tree, "researcher", sandboxAnalysis, "artifact", "write", "analysis")
		sb.works(t, tree, "researcher", sandboxThreats, "artifact", "write", "threats")
	case model.GateTestsFrozen:
		sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)
		sb.works(t, tree, "sdet", sandboxTestPlan, "artifact", "write", "test_plan")
		sb.works(t, tree, "sdet", "", "freeze")
	case model.GatePlan:
		sb.works(t, tree, "implementer", sandboxPlan, "artifact", "write", "plan")
	case model.GateImplementation:
		sb.writesFile(t, "implementer", "invoice.go", sandboxCode)
	case model.GateVerification:
		sb.works(t, tree, "verifier", sandboxVerification, "artifact", "write", "verification")
	case model.GateCommit:
		sb.commits(t)
	case model.GateRetro:
		sb.works(t, tree, "bookkeeper", sandboxRetro, "artifact", "write", "retro")
	}
	for _, r := range model.ReviewersFor(gate) {
		if r.WhenDoRAdvocate {
			continue
		}
		sb.works(t, tree, r.Role, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
	}
	if gate == model.GateAnalysis {
		sb.works(t, tree, "", "", "gate", string(gate), "pass", "--security-sensitive=false")
		return
	}
	sb.works(t, tree, "", "", "gate", string(gate), "pass", "--note", "worked by the session under test")
}

// commits is the story's commit: staged, the `git commit` put to the hook,
// which lets it through only once the gates before it have passed, and then
// made.
func (sb sandbox) commits(t *testing.T) {
	t.Helper()
	sb.mustGit(t, sb.tree(), "add", "-A")
	sb.allows(t, sb.tree(), "", "Bash", map[string]any{"command": "git commit -m 'feat: US-001'"})
	sb.mustGit(t, sb.tree(), "commit", "--quiet", "-m", "feat: US-001")
}

// worksEveryGate works the story from wherever it stands to its last gate.
func (sb sandbox) worksEveryGate(t *testing.T) {
	t.Helper()
	for gate := sb.nextGate(t); gate != ""; gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}
}

// refusedTo is the refusal sdlc prints: what is wrong, why, the fix, and the
// code with the page that explains it.
func refusedTo(what, why, fix, code string) string {
	return "sdlc: " + what + "\n\n  why  " + why + "\n  fix  " + fix + "\n\n  " + code +
		"  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#" + strings.ToLower(code) + "\n"
}

func stored(name, file, body string) string {
	return fmt.Sprintf("US-001  %s  .sdlc/stories/US-001/%s (%d bytes)\n", name, file, len(body))
}

func passed(gate, note string) string {
	return "US-001  " + gate + "  pass\n  " + note + "\n"
}

func reviewedBy(gate, role string) string {
	return "US-001  " + gate + "  " + role + "  approve (round 1)\n  .sdlc/stories/US-001/reviews/" + gate + "-" + role + "-1.md\n"
}

// ---------------------------------------------------------------- AC-2

// What each command prints is the released tool's, taken from a run of it in a
// directory `git init` and `sdlc init` made and nothing else: no stack, so no
// commands, which is the one problem `sdlc doctor` reports there and why it
// exits 1. The worktree is where the story is worked because a project
// `sdlc init` set up has story worktrees on.
//
// S3 of the analysis: this is every subcommand that needs no person, not only
// the nine AC-2 names. `commit` in AC-2 is the gate, not a command: the
// `git commit` the hook lets through once the gates before it have passed,
// and `sdlc gate commit pass`. `parallel` starts model sessions and is never
// run; AC-3's test holds it refused.
func TestAC2EveryCommandThatNeedsNoPersonRunsToTheEndInTheSandbox(t *testing.T) {
	sb := newSandbox(t)
	main, tree, title := sb.repo, sb.tree(), titleOf(t, sb.repo)

	noStory := func(why, fix string) string {
		return refusedTo("there is no story being worked on", why, fix, "SDLC-E0011")
	}
	doctor := func(runnable string) string {
		return "sdlc doctor\n\n" +
			"  ok       git repository       <repository>\n" +
			"  ok       git command          found on PATH\n" +
			"  ok       configuration        .sdlc/config.json\n" +
			"  ok       backlog              user_stories.json  (1 story, " + runnable + " runnable)\n" +
			"  ok       loop state           the iteration, every gate record, the test freeze and the commit acknowledged all read\n" +
			"  ok       project contract     CLAUDE.md carries the \"## SDLC Contract\" section\n" +
			"  problem  commands             no commands are configured, so every gate that runs one is skipped\n" +
			"                                fix: fill in commands.test and commands.lint in .sdlc/config.json\n" +
			"  ok       sdlc on PATH         SDLC_BIN: <sdlc>\n" +
			"\n1 problem. The fix is above it.\n"
	}
	reviews := func(design, code, state string) string {
		return "  design_review   architect       blocking  " + state + "\n" +
			"  design_review   red-team        advisory  " + state + "\n" +
			"  design_review   security        " + design + "  " + state + "\n" +
			"  design_review   perf            on budget " + state + "\n" +
			"  design_review   human-advocate  advisory  " + state + "\n" +
			"  verifier_review verifier        blocking  " + state + "\n" +
			"  code_review     code-reviewer   blocking  " + state + "\n" +
			"  code_review     security        " + code + "  " + state + "\n" +
			"  code_review     perf            on budget " + state + "\n" +
			"  code_review     human-advocate  advisory  " + state + "\n"
	}
	held := "  held     US-001  in \"<worktree>\", this checkout\n"

	t.Run("AC-2 version", func(t *testing.T) {
		// What it prints is the commit the binary was built from, which no test
		// can know: one line, and the same version --json gives.
		r := sb.does(t, main, "", "", "version")
		if r.code != 0 || r.stderr != "" || strings.Count(r.stdout, "\n") != 1 || strings.TrimSpace(r.stdout) == "" {
			t.Errorf("sdlc version: exit %d, stdout %q, stderr %q; want one line and exit 0", r.code, r.stdout, r.stderr)
		}
		var v struct {
			Version string `json:"version"`
		}
		j := sb.does(t, main, "", "", "version", "--json")
		if err := json.Unmarshal([]byte(j.stdout), &v); err != nil || j.code != 0 || v.Version == "" ||
			!strings.HasPrefix(r.stdout, v.Version) {
			t.Errorf("sdlc version --json: exit %d, %q (%v); want the version `sdlc version` begins with, %q",
				j.code, j.stdout, err, r.stdout)
		}
	})

	t.Run("AC-2 before a story is started: status, story list, doctor, artifact list, review list, log, cost, init", func(t *testing.T) {
		sb.holds(t,
			ran{dir: main, args: []string{"status"},
				stdout: "No iteration running.\n\n  backlog  1 ready\n  next up  US-001  " + title +
					"\n\nA person runs `sdlc start`, or types /sdlc:next in Claude Code, to begin.\n"},
			ran{dir: main, args: []string{"story", "list"},
				stdout: "> US-001  ready   1  " + title + "\n\n> is what `sdlc start` would pick.\n"},
			ran{dir: main, args: []string{"doctor"}, stdout: doctor("one"), code: 1},
			ran{dir: main, args: []string{"artifact", "list"},
				stdout: "  analysis   ANALYSIS.md    gate analysis   written by the researcher agent\n" +
					"  threats    THREATS.md     gate analysis   written by the researcher agent\n" +
					"  test_plan  TEST-PLAN.md   gate tests_frozen written by the sdet agent\n" +
					"  plan       PLAN.md        gate plan       written by the implementer agent\n" +
					"  verification VERIFICATION.md gate verification written by the verifier agent\n" +
					"  retro      RETRO.md       gate retro      written by the bookkeeper agent\n"},
			ran{dir: main, args: []string{"review", "list"}, code: 1,
				stderr: noStory("reviews belong to the story being worked on, and no iteration is running",
					`a person runs "sdlc start", or types /sdlc:next in Claude Code, to begin an iteration on the next story`)},
			ran{dir: main, args: []string{"log"}, stdout: "No story has been committed yet.\n"},
			ran{dir: main, args: []string{"cost"}, code: 1,
				stderr: noStory("cost is kept per story, and no iteration is running",
					"name the story with --story -- a runner that records the cost once a session is over has to, because the iteration has ended by then")},
			// The sandbox is a project `sdlc init` has already set up, which is
			// what a second one says.
			ran{dir: main, args: []string{"init"}, code: 1,
				stderr: refusedTo("this project is already set up for the loop",
					".sdlc/config.json is already there, and overwriting it would lose your settings",
					`edit .sdlc/config.json directly, or run "sdlc init --force" to overwrite it`, "SDLC-E0003")},
		)
	})

	t.Run("AC-2 start", func(t *testing.T) {
		sb.holds(t,
			ran{dir: main, args: []string{"start"},
				stdout: "Started US-001  " + title + "\n\nWorktree: \"<worktree>\"\nBranch: story/US-001\n\n  no gates recorded yet\n\n" +
					"Next: a person opens Claude Code in the worktree above and types /sdlc:next there to work the story; " +
					"/sdlc:next typed in a new session in this checkout begins the next story instead.\n"},
			ran{dir: tree, args: []string{"status"},
				stdout: "US-001  in progress  " + title + "\n\n  no gates recorded yet\n\n  tests  not frozen\n  cost   $0.00 of $60.00 (0%)\n\n" +
					"  next   dor\n\n  backlog  1 in progress\n" + held +
					"\nA person types /sdlc:next in Claude Code to carry on, or runs `sdlc stop` to put it down.\n"},
			ran{dir: tree, args: []string{"review", "list"}, stdout: reviews("blocking", "blocking", "not reviewed")},
		)
	})
	if t.Failed() {
		t.Fatal("the story was not started, so nothing after this can be run")
	}

	t.Run("AC-2 the commit gate refuses a commit before the gates in front of it", func(t *testing.T) {
		sb.refuses(t, sandboxSession, tree, "Bash", map[string]any{"command": "git commit -m early"}, "commit-gate")
	})

	t.Run("AC-2 gate, artifact write and freeze, up to the frozen tests", func(t *testing.T) {
		sb.holds(t,
			ran{dir: tree, args: []string{"gate", "dor", "pass", "--note", "the criteria are testable"},
				stdout: passed("dor", "the criteria are testable")},
			ran{dir: tree, agent: "researcher", stdin: sandboxAnalysis, args: []string{"artifact", "write", "analysis"},
				stdout: stored("analysis", "ANALYSIS.md", sandboxAnalysis)},
			ran{dir: tree, agent: "researcher", stdin: sandboxThreats, args: []string{"artifact", "write", "threats"},
				stdout: stored("threats", "THREATS.md", sandboxThreats)},
			ran{dir: tree, args: []string{"gate", "analysis", "pass", "--security-sensitive=false"},
				stdout: "US-001  analysis  pass\n"},
		)
		sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)
		sb.holds(t,
			ran{dir: tree, agent: "sdet", stdin: sandboxTestPlan, args: []string{"artifact", "write", "test_plan"},
				stdout: stored("test_plan", "TEST-PLAN.md", sandboxTestPlan)},
			ran{dir: tree, agent: "sdet", args: []string{"freeze"}, stdout: "US-001  1 test file frozen\n  invoice_test.go\n"},
			ran{dir: tree, args: []string{"gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test"},
				stdout: passed("tests_frozen", "1 criterion, 1 failing test")},
		)
	})

	t.Run("AC-2 review add and gate, from the plan to the code review", func(t *testing.T) {
		sb.holds(t,
			ran{dir: tree, agent: "implementer", stdin: sandboxPlan, args: []string{"artifact", "write", "plan"},
				stdout: stored("plan", "PLAN.md", sandboxPlan)},
			ran{dir: tree, args: []string{"gate", "plan", "pass", "--note", "one step"}, stdout: passed("plan", "one step")},
		)
		review := func(gate string) {
			t.Helper()
			for _, r := range model.ReviewersFor(model.Gate(gate)) {
				if r.WhenDoRAdvocate {
					continue
				}
				sb.holds(t, ran{dir: tree, agent: r.Role, stdin: "# " + r.Role + "\n",
					args: []string{"review", "add", gate, r.Role, "approve"}, stdout: reviewedBy(gate, r.Role)})
			}
			sb.holds(t, ran{dir: tree, args: []string{"gate", gate, "pass", "--note", "approved"}, stdout: passed(gate, "approved")})
		}
		review("design_review")
		sb.writesFile(t, "implementer", "invoice.go", sandboxCode)
		sb.holds(t,
			ran{dir: tree, args: []string{"gate", "implementation", "pass", "--note", "frozen tests green"},
				stdout: passed("implementation", "frozen tests green")},
			ran{dir: tree, agent: "verifier", stdin: sandboxVerification, args: []string{"artifact", "write", "verification"},
				stdout: stored("verification", "VERIFICATION.md", sandboxVerification)},
			ran{dir: tree, args: []string{"gate", "verification", "pass", "--note", "all commands green"},
				stdout: passed("verification", "all commands green")},
		)
		review("verifier_review")
		review("code_review")
		// The analysis found nothing security-sensitive, so security advises.
		sb.holds(t, ran{dir: tree, args: []string{"review", "list"}, stdout: reviews("advisory", "advisory", "approve (round 1)")})
	})

	t.Run("AC-2 the commit gate lets the commit through, git makes it, and the gate records its range", func(t *testing.T) {
		sb.commits(t)
		if subject := sb.mustGit(t, tree, "log", "-1", "--format=%s"); subject != "feat: US-001" {
			t.Errorf("the story's branch ends in %q, want the commit just made", subject)
		}
		sb.holds(t, ran{dir: tree, args: []string{"gate", "commit", "pass", "--note", "committed"},
			stdout: passed("commit", "committed") + "  committed on story/US-001 in <sha>..<sha>\n"})
	})

	t.Run("AC-2 the retro, cost add, status and stop", func(t *testing.T) {
		sb.holds(t,
			ran{dir: tree, agent: "bookkeeper", stdin: sandboxRetro, args: []string{"artifact", "write", "retro"},
				stdout: stored("retro", "RETRO.md", sandboxRetro)},
			ran{dir: tree, args: []string{"gate", "retro", "pass", "--note", "no deviations"},
				stdout: passed("retro", "no deviations") +
					"\nEvery gate on US-001 has passed. `sdlc stop` in .sdlc/worktrees/US-001 lands it on main.\n"},
			ran{dir: tree, args: []string{"cost", "add", "--usd", "2.40"}, stdout: "US-001  $2.40 spent of $60.00 (4%)\n"},
			ran{dir: tree, args: []string{"cost"}, stdout: "US-001  $2.40 spent of $60.00 (4%)\n"},
			ran{dir: tree, args: []string{"status"},
				stdout: "US-001  finished     " + title + "\n\n" +
					"  dor              pass  the criteria are testable\n" +
					"  analysis         pass\n" +
					"  tests_frozen     pass  1 criterion, 1 failing test\n" +
					"  plan             pass  one step\n" +
					"  design_review    pass  approved\n" +
					"  implementation   pass  frozen tests green\n" +
					"  verification     pass  all commands green\n" +
					"  verifier_review  pass  approved\n" +
					"  code_review      pass  approved\n" +
					"  commit           pass  committed\n" +
					"  retro            pass  no deviations\n\n" +
					"  tests  1 test file frozen at <time>\n  cost   $2.40 of $60.00 (4%)\n\n" +
					"  backlog  1 in progress\n" + held +
					"\nEvery gate has passed. Run `sdlc stop` here to land it on main.\n"},
			// Every gate has passed, so this stop is the session's own to run:
			// it lands the story on trunk and takes the worktree away.
			ran{dir: tree, args: []string{"stop"},
				stdout: "main moved from <sha> to <sha> by fast-forward; the files in \"<repository>\", which has main checked out, moved with it.\n" +
					"The backlog and US-001's record are changed in the main checkout and not committed.\n" +
					"Ended the iteration on US-001. Every gate passed, so the story is done and `sdlc start` moves on to the next one.\n" +
					"Removed .sdlc/worktrees/US-001, with the files git ignored in it, and story/US-001.\n"},
		)
	})

	t.Run("AC-2 once the story is on trunk: status, log, story list, doctor", func(t *testing.T) {
		sb.holds(t,
			ran{dir: main, args: []string{"status"},
				stdout: "No iteration running.\n\n  backlog  1 done\n\nNothing to start: the one story in the backlog is finished.\n" +
					"Add a story to user_stories.json (schema: .sdlc/templates/story.schema.json).\n"},
			ran{dir: main, args: []string{"log"},
				stdout: "<sha>..<sha>  US-001  " + title + "\n" +
					"  rounds     design_review 1 · verifier_review 1 · code_review 1\n  spent      $2.40\n"},
			ran{dir: main, args: []string{"story", "list"},
				stdout: "  US-001  done   1  " + title + "\n\nNothing to start: the one story in the backlog is finished.\n"},
			ran{dir: main, args: []string{"doctor"}, stdout: doctor("none"), code: 1},
		)
		// And the work is where the loop says it is.
		if subject := sb.mustGit(t, main, "log", "-1", "--format=%s", "main"); subject != "feat: US-001" {
			t.Errorf("trunk ends in %q, want the story's commit", subject)
		}
		if exists(tree) {
			t.Errorf("the story's worktree %s is still there after the story landed", tree)
		}
	})
}

// ---------------------------------------------------------------- AC-3

// personsDecisions is each command only a person runs, as a session types it,
// and the rule the hook refuses it under.
var personsDecisions = []struct{ command, rule string }{
	{"sdlc approve US-001", "approval-is-a-human-decision"},
	{"sdlc approve", "approval-is-a-human-decision"},
	{`sdlc approve US-001 --reject "no"`, "approval-is-a-human-decision"},
	{`sdlc unfreeze --reason "the test is wrong"`, "unfreeze-is-a-human-decision"},
	{"sdlc ack --through HEAD", "acknowledgement-is-a-human-decision"},
	{`sdlc release US-001 --reason "moved"`, "release-is-a-human-decision"},
	{"sdlc stop", "stop-is-a-human-decision"},
	{"sdlc parallel US-001", "parallel-is-a-human-decision"},
}

// A whole story with every step that needs a person in it, and nobody there:
// the freeze lifted and put back, an escalation answered, the iteration put
// down part-way, the story released, the log acknowledged. Each is run by this
// process with no session in its environment, in the sandbox this process made,
// and each is refused to the session under test by the hook before and after.
//
// What this does not close, and did not before: a session can write a program
// that runs `sdlc approve` in its own project, because the hook reads the words
// of a tool call and not what a program it allows goes on to run. Nothing here
// makes that shorter: the steps are test code, they take no directory from an
// argument, the environment or a file, and no command, flag, variable or task
// performs one.
func TestAC3TheHarnessPerformsThePersonOnlyStepsFromOutsideTheSessionAndTheRunStillCompletes(t *testing.T) {
	sb := newSandbox(t)
	main, tree, title := sb.repo, sb.tree(), titleOf(t, sb.repo)

	sb.works(t, main, "", "", "start")
	everyDecisionIsRefused := func(t *testing.T, session, when string) {
		t.Helper()
		for _, d := range personsDecisions {
			if d.rule == "stop-is-a-human-decision" && when != "while the story is under way" {
				continue // an iteration that has ended has nothing to stop
			}
			sb.refuses(t, session, tree, "Bash", map[string]any{"command": d.command}, d.rule)
		}
	}
	t.Run("AC-3 the hook refuses each to the session working the story", func(t *testing.T) {
		everyDecisionIsRefused(t, sandboxSession, "while the story is under way")
	})

	for gate := sb.nextGate(t); gate != string(model.GatePlan); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}

	t.Run("AC-3 unfreeze", func(t *testing.T) {
		sb.refuses(t, sandboxSession, tree, "Bash", map[string]any{"command": `sdlc unfreeze --reason "AC-1's test is wrong"`},
			"unfreeze-is-a-human-decision")
		r := sb.person(t, tree, "unfreeze", "--reason", "AC-1's test asserted the wrong total")
		want := "US-001  freeze lifted on 1 test file\n  AC-1's test asserted the wrong total\n  reopened: tests_frozen\n"
		if r.stdout != want || r.stderr != "" || r.code != 0 {
			t.Fatalf("unfreeze, by the harness: exit %d\nstdout %q\nstderr %q\nwant stdout %q", r.code, r.stdout, r.stderr, want)
		}
		// The session carries on from the gate the person reopened.
		if gate := sb.nextGate(t); gate != string(model.GateTestsFrozen) {
			t.Fatalf("the next gate after the freeze was lifted is %q, want tests_frozen", gate)
		}
		sb.worksGate(t, model.GateTestsFrozen)
	})

	t.Run("AC-3 approve", func(t *testing.T) {
		sb.holds(t, ran{dir: tree, args: []string{"escalate", "spec_unclear", "--message", "is a zero total refused too?"},
			stdout: "US-001  waiting for a person: spec_unclear\n  is a zero total refused too?\n\n" +
				"The iteration has ended. A person reads the work and answers in their own terminal:\n" +
				"  sdlc approve US-001\n  sdlc approve US-001 --reject \"why\"\n"})
		if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
			t.Fatalf("the story is %q after the escalation, want awaiting_human", got["US-001"])
		}
		// The story waits, so no session is held to it: the refusal is the one
		// any session gets, the one that escalated and one that never saw it.
		everyDecisionIsRefused(t, sandboxSession, "while the story waits for a person")
		everyDecisionIsRefused(t, sandboxOther, "while the story waits for a person")

		r := sb.person(t, tree, "approve", "US-001")
		want := "US-001  approved (spec_unclear)\n\nA person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.\n"
		if r.stdout != want || r.stderr != "" || r.code != 0 {
			t.Fatalf("approve, by the harness: exit %d\nstdout %q\nstderr %q\nwant stdout %q", r.code, r.stdout, r.stderr, want)
		}
		if got := storiesOf(t, main); got["US-001"] != "in_progress" {
			t.Errorf("the story is %q after the approval, want in_progress", got["US-001"])
		}
		sb.works(t, tree, "", "", "start")
	})

	t.Run("AC-3 a stop before the gates have passed", func(t *testing.T) {
		sb.refuses(t, sandboxSession, tree, "Bash", map[string]any{"command": "sdlc stop"}, "stop-is-a-human-decision")
		r := sb.person(t, tree, "stop")
		want := "Stopped the iteration on US-001. Nothing was recorded; `sdlc start` picks it up again.\n"
		if r.stdout != want || r.stderr != "" || r.code != 0 {
			t.Fatalf("stop, by the harness: exit %d\nstdout %q\nstderr %q\nwant stdout %q", r.code, r.stdout, r.stderr, want)
		}
	})

	t.Run("AC-3 release", func(t *testing.T) {
		sb.refuses(t, sandboxSession, tree, "Bash", map[string]any{"command": `sdlc release US-001 --reason "x"`},
			"release-is-a-human-decision")
		r := sb.person(t, tree, "release", "US-001", "--reason", "the story is to be worked from a new session")
		want := "Released US-001: the checkout at \"<worktree>\" no longer holds it. \"sdlc start US-001\" picks it up in any checkout.\n" +
			"Anything the checkout at \"<worktree>\" has not committed stays there, and is not moved.\n"
		if got := sb.normalise(r.stdout); got != want || r.stderr != "" || r.code != 0 {
			t.Fatalf("release, by the harness: exit %d\nstdout %q\nstderr %q\nwant stdout %q", r.code, got, r.stderr, want)
		}
	})

	// The run still completes: the session picks the story up where the person
	// left it and takes it to trunk.
	sb.works(t, tree, "", "", "start", "US-001")
	sb.worksEveryGate(t)
	sb.works(t, tree, "", "", "stop")
	if got := storiesOf(t, main); got["US-001"] != "done" {
		t.Fatalf("the story is %q at the end of the run, want done", got["US-001"])
	}

	t.Run("AC-3 ack", func(t *testing.T) {
		sb.holds(t, ran{dir: main, args: []string{"log"},
			stdout: "<sha>..<sha>  US-001  " + title + "\n" +
				"  rounds     design_review 1 · verifier_review 1 · code_review 1\n" +
				"  reopened   tests_frozen reopened: the freeze was lifted: AC-1's test asserted the wrong total\n" +
				"  unfrozen   AC-1's test asserted the wrong total\n" +
				"  escalated  spec_unclear: is a zero total refused too? (answered)\n"})
		// Acknowledging is refused always, to any session, story or none.
		for _, session := range []string{sandboxSession, sandboxOther} {
			sb.refuses(t, session, main, "Bash", map[string]any{"command": "sdlc ack --through HEAD"},
				"acknowledgement-is-a-human-decision")
		}
		r := sb.person(t, main, "ack", "--through", "HEAD")
		want := "Acknowledged through <sha>. `sdlc log` starts after it.\n"
		if got := sb.normalise(r.stdout); got != want || r.stderr != "" || r.code != 0 {
			t.Fatalf("ack, by the harness: exit %d\nstdout %q\nstderr %q\nwant stdout %q", r.code, got, r.stderr, want)
		}
		sb.holds(t, ran{dir: main, args: []string{"log"},
			stdout: "No story has been committed since <sha>, the last commit acknowledged. `sdlc log --all` lists every one.\n"})
	})

	// S3: parallel starts real model sessions, which is no part of this story,
	// so it is never run to the end. The hook refuses it above; and past the
	// hook, the binary refuses a process that says it is a session.
	t.Run("AC-3 parallel is refused by the binary too, and starts nothing", func(t *testing.T) {
		stdout, stderr, code := sb.outcome(t, sandboxSession, main, "", "parallel", "US-001")
		want := refusedTo("sdlc parallel was run from inside a Claude Code session",
			"starting model sessions is a person's decision",
			`a person runs "sdlc parallel" in their own terminal, outside Claude Code; an assistant stops and tells the person`,
			"SDLC-E0061")
		if stdout != "" || stderr != want || code != 1 {
			t.Errorf("sdlc parallel in a session: exit %d\nstdout %q\nstderr %q\nwant stderr %q", code, stdout, stderr, want)
		}
		if exists(filepath.Join(main, ".sdlc", "worktrees", "US-001")) {
			t.Error("the refused run left a worktree")
		}
	})
}

// T-4: a person-only step that carried a session id would pass for the wrong
// reason, because the binary lets the session working a story through. The
// harness's steps carry none, and this is what shows the difference is real:
// the same command from a session the story is not held in is refused by the
// binary, and from no session it is taken.
func TestAC3APersonOnlyStepCarriesNoSessionAndASessionThatDoesNotHoldTheStoryIsRefused(t *testing.T) {
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GatePlan); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}

	stdout, stderr, code := sb.outcome(t, sandboxOther, sb.tree(), "", "unfreeze", "--reason", "from another session")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "is being worked on in another Claude Code session") {
		t.Errorf("unfreeze from a session that does not hold the story: exit %d\nstdout %q\nstderr %q\n"+
			"want it refused as another session's", code, stdout, stderr)
	}
	for _, kv := range sb.env(t, "") {
		if strings.HasPrefix(kv, "CLAUDE_") && !strings.HasPrefix(kv, "CLAUDE_PROJECT_DIR=") {
			t.Errorf("a person's environment carries %s", kv)
		}
		if strings.HasPrefix(kv, "SDLC_ENFORCE=") {
			t.Errorf("the harness sets %s: a run with enforcement off shows nothing", kv)
		}
	}
	if r := sb.person(t, sb.tree(), "unfreeze", "--reason", "from the harness"); r.code != 0 {
		t.Errorf("unfreeze with no session exited %d:\n%s%s", r.code, r.stdout, r.stderr)
	}
}

// AC-3's second half, where it matters most: a real project, not a sandbox,
// set up as every other test here sets one up. It holds today and has to go on
// holding: nothing in this story touches the hook, the shell policy, or
// approve, unfreeze and ack. internal/shellpolicy has every spelling; this is
// the shipped launcher and the built binary agreeing on the three.
func TestAC3TheHookStillRefusesAPersonsDecisionsToAModelDrivenSessionInARealProject(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1})
	held := main.as(claudeSession)
	held.run(t, "start")

	refused := func(t *testing.T, session, command, rule string) {
		t.Helper()
		out := launch(t, binary, main.dir, "PreToolUse", toolCall(session, main.dir, "", "Bash", map[string]any{"command": command}))
		var r refusal
		oneLine(t, out, &r)
		got := r.HookSpecificOutput
		if got.HookEventName != "PreToolUse" || got.PermissionDecision != "deny" ||
			!strings.Contains(got.PermissionDecisionReason, "["+rule+"]") || !strings.Contains(got.PermissionDecisionReason, "Instead:") {
			t.Errorf("session %s running %q was not refused under %s with a route: %q", session, command, rule, out)
		}
	}

	t.Run("AC-3 to the session working a story", func(t *testing.T) {
		for _, d := range personsDecisions {
			refused(t, claudeSession, d.command, d.rule)
		}
	})

	held.analysed(t)
	held.run(t, "escalate", "spec_unclear", "--message", "is a zero total refused too?")

	t.Run("AC-3 to any session while a story waits for a person", func(t *testing.T) {
		for _, session := range []string{claudeSession, sessionUnheld} {
			for _, d := range personsDecisions {
				if d.rule == "stop-is-a-human-decision" {
					continue // the escalation ended the iteration
				}
				refused(t, session, d.command, d.rule)
			}
		}
	})

	// And the story still waits: nothing a session was refused happened.
	if got := statuses(t, main.backlog()); got["US-001"] != "awaiting_human" {
		t.Errorf("the story is %q, want awaiting_human", got["US-001"])
	}
}

// ---------------------------------------------------------------- AC-4

// stopCall is the JSON Claude Code sends when a session ends its turn.
func stopCall(session, dir string, active bool) map[string]any {
	return map[string]any{
		"hook_event_name":  "Stop",
		"session_id":       session,
		"cwd":              dir,
		"stop_hook_active": active,
	}
}

// turn is what the hook prints for a Stop, read strictly.
type turn struct {
	Continue bool   `json:"continue"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

var stopCount = regexp.MustCompile(`\(this was (\d+)\)`)

// The hook's stdout is the protocol: Claude Code parses it, and what it cannot
// parse it drops. So each reply is compared whole where it is fixed, and read
// with every field accounted for where it carries a reason.
func TestAC4TheHookIsGivenTheJSONClaudeCodeSendsAndItsStdoutIsTheProtocol(t *testing.T) {
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()

	quiet := func(t *testing.T, what, stdout, stderr string, code int) {
		t.Helper()
		if stdout != carryOn || stderr != "" || code != 0 {
			t.Errorf("%s: exit %d\nstdout %q\nstderr %q\nwant exactly %q, nothing on stderr and exit 0", what, code, stdout, stderr, carryOn)
		}
	}

	t.Run("AC-4 with no story under way a tool call and a stop both go through", func(t *testing.T) {
		stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, main, "", "Write",
			map[string]any{"file_path": filepath.Join(main, "invoice.go"), "content": sandboxCode}))
		quiet(t, "PreToolUse for a write", stdout, stderr, code)
		stdout, stderr, code = sb.hook(t, "Stop", stopCall(sandboxSession, main, false))
		quiet(t, "Stop", stdout, stderr, code)
	})

	sb.works(t, main, "", "", "start")

	t.Run("AC-4 PreToolUse: a call the loop allows", func(t *testing.T) {
		stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, "researcher", "Bash",
			map[string]any{"command": "sdlc artifact write analysis"}))
		quiet(t, "PreToolUse for `sdlc artifact write analysis`", stdout, stderr, code)
	})

	t.Run("AC-4 PreToolUse: a call the loop refuses", func(t *testing.T) {
		for _, c := range []struct {
			name, agent, tool string
			input             map[string]any
			rule              string
		}{
			{"the loop's own state, written by hand", "", "Write",
				map[string]any{"file_path": filepath.Join(tree, ".sdlc", "state", "active"), "content": "US-002\n"}, "write-protected-path"},
			{"enforcement turned off from inside", "implementer", "Bash",
				map[string]any{"command": "SDLC_ENFORCE=0 sdlc gate dor pass"}, "enforcement-stays-on"},
			{"a commit before the gates", "", "Bash", map[string]any{"command": "git commit -m early"}, "commit-gate"},
		} {
			stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, c.agent, c.tool, c.input))
			if stderr != "" || code != 0 {
				t.Errorf("%s: exit %d and stderr %q; a refusal is a reply on stdout, read only when the hook exits 0", c.name, code, stderr)
			}
			var r refusal
			oneLine(t, stdout, &r)
			got := r.HookSpecificOutput
			if got.HookEventName != "PreToolUse" || got.PermissionDecision != "deny" ||
				!strings.HasSuffix(got.PermissionDecisionReason, " ["+c.rule+"]") || !strings.Contains(got.PermissionDecisionReason, "Instead:") {
				t.Errorf("%s: the reply is not a refusal under %s with a route: %q", c.name, c.rule, stdout)
			}
		}
	})

	t.Run("AC-4 Stop: the session working the story is sent back, and no other", func(t *testing.T) {
		stdout, stderr, code := sb.hook(t, "Stop", stopCall(sandboxOther, tree, false))
		quiet(t, "Stop from a session that holds no story", stdout, stderr, code)
		stdout, stderr, code = sb.hook(t, "Stop", stopCall(sandboxSession, tree, true))
		quiet(t, "Stop from a session already sent back", stdout, stderr, code)

		for want := 1; want <= 2; want++ {
			stdout, stderr, code = sb.hook(t, "Stop", stopCall(sandboxSession, tree, false))
			if stderr != "" || code != 0 {
				t.Errorf("Stop: exit %d and stderr %q", code, stderr)
			}
			var r turn
			oneLine(t, stdout, &r)
			count := stopCount.FindStringSubmatch(r.Reason)
			if !r.Continue || r.Decision != "block" || !strings.HasPrefix(r.Reason, "US-001 is still being worked on. Work dor and record it") ||
				count == nil || count[1] != fmt.Sprint(want) {
				t.Errorf("stop %d of the session working the story was not sent back to the dor gate with its count: %q", want, stdout)
			}
		}
	})

	t.Run("AC-4 a payload the hook cannot read, and an event it does not know, go through", func(t *testing.T) {
		for _, event := range []string{"PreToolUse", "Stop", "SomethingNew"} {
			for _, payload := range []string{"", "not json", "{}", `{"tool_name":"Bash","tool_input":"a string"}`} {
				stdout, stderr, code := sb.hookRaw(t, event, payload, runtime.GOOS != "windows")
				quiet(t, event+" given "+fmt.Sprintf("%q", payload), stdout, stderr, code)
			}
		}
	})

	// What Claude Code runs is the launcher; what Windows runs here is the
	// binary. They have to be the same answer, byte for byte.
	t.Run("AC-4 the launcher and the binary give the same reply", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the POSIX launcher does not run here; internal/plugin holds sdlc-hook.cmd to the binary's replies")
		}
		for _, call := range []map[string]any{
			toolCall(sandboxSession, tree, "", "Bash", map[string]any{"command": "sdlc status"}),
			toolCall(sandboxSession, tree, "", "Bash", map[string]any{"command": "sdlc approve US-001"}),
			toolCall(sandboxSession, tree, "", "Write", map[string]any{"file_path": filepath.Join(tree, ".sdlc", "state", "active")}),
		} {
			raw, err := json.Marshal(call)
			if err != nil {
				t.Fatal(err)
			}
			viaLauncher, _, _ := sb.hookRaw(t, "PreToolUse", string(raw), true)
			direct, _, _ := sb.hookRaw(t, "PreToolUse", string(raw), false)
			if viaLauncher != direct || direct == "" {
				t.Errorf("for %s the launcher printed %q and the binary %q", raw, viaLauncher, direct)
			}
		}
	})
}
