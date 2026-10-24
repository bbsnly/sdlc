package hook

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A path that is only named, through the hook: the state the rules are asked
// with is the state the hook works out from the project's own files, and the
// answer is the one Claude Code reads. internal/shellpolicy holds the tables;
// these are a mention and the act nearest to it for each thing let through,
// from every tool that runs a command.

// namedAsk is a command from tool, as the session working the story sends it.
func namedAsk(root, tool, agent, cmd string) string {
	e := map[string]any{
		"hook_event_name": "PreToolUse", "session_id": working,
		"tool_name":  tool,
		"cwd":        root,
		"tool_input": map[string]string{"command": cmd},
	}
	if agent != "" {
		e["agent_type"] = agent
	}
	return mustMarshal(e)
}

// namedRow is one command and the rule the hook refuses it under: none where
// it is let through.
type namedRow struct {
	name, tool, agent, command, rule string
}

func namedReason(r reply) string { return r.HookSpecificOutput.PermissionDecisionReason }

func (c namedRow) check(t *testing.T, root string) reply {
	t.Helper()
	tool := c.tool
	if tool == "" {
		tool = "Bash"
	}
	got := call(t, namedAsk(root, tool, c.agent, c.command), noEnv)
	switch {
	case c.rule == "" && denied(got):
		t.Errorf("refused, and the path is only named: %s\n%s", c.command, namedReason(got))
	case c.rule != "" && !denied(got):
		t.Errorf("allowed: %s", c.command)
	case c.rule != "" && !strings.Contains(namedReason(got), "["+c.rule+"]"):
		t.Errorf("%s was refused, but not as %s: %s", c.command, c.rule, namedReason(got))
	case c.rule != "" && !strings.Contains(namedReason(got), "Instead: "):
		t.Errorf("the refusal of %s does not say what to do instead: %s", c.command, namedReason(got))
	}
	return got
}

// namedProject is a story being worked on with one test frozen.
func namedProject(t *testing.T) string {
	t.Helper()
	root := loopProject(t)
	freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")
	return root
}

const (
	namedLoopState = "loop-state-through-the-tool"
	namedProtected = "protected-path-through-the-tool"
	namedFrozen    = "frozen-test-through-the-tool"
	namedApproval  = "approval-is-a-human-decision"
	namedUnfreeze  = "unfreeze-is-a-human-decision"
)

func namedRun(t *testing.T, rows []namedRow) {
	t.Helper()
	root := namedProject(t)
	for _, c := range rows {
		t.Run(c.name, func(t *testing.T) { c.check(t, root) })
		// US-023 AC-5: the row again, for AGENTS.md.
		agAgentsMD(t, c.command, []string{root}, func(t *testing.T, respelt string) {
			t.Helper()
			r := c
			r.command = respelt
			r.check(t, root)
		})
	}
}

func TestAC1TheHookLetsThroughAPathInTheQuotedTextOfAnSdlcFlag(t *testing.T) {
	namedRun(t, []namedRow{
		{name: "AC-1 an escalation that says what was refused", command: `sdlc escalate spec_unclear --message "the hook refused; rm .sdlc/state/active was what I ran"`},
		{name: "AC-1 an escalation with an arrow at the configuration", command: `sdlc escalate pre_commit_approval --message 'see -> .sdlc/config.json'`},
		{name: "AC-1 a gate note that names the contract", command: `sdlc gate analysis pass --note="see -> CLAUDE.md"`},
		{name: "AC-1 a note that names the backlog", command: `sdlc cost add --usd 1 --note "see -> user_stories.json"`},
		{name: "AC-1 an escalation that names the frozen test", command: `sdlc escalate frozen_test_wrong --message "wrong; rm internal/invoice_test.go was refused"`},
		{name: "AC-1 the same from an agent", agent: "sdlc:implementer", command: `sdlc escalate frozen_test_wrong --message 'see -> internal/invoice_test.go'`},
		{name: "AC-1 from Monitor", tool: "Monitor", command: `sdlc escalate spec_unclear --message "see -> .sdlc/config.json"`},
		{name: "AC-1 from PowerShell", tool: "PowerShell", command: `sdlc escalate spec_unclear --message 'refused; Remove-Item .sdlc/state/active'`},
	})
}

func TestAC2TheHookLetsThroughAPathAPathspecExcludes(t *testing.T) {
	namedRun(t, []namedRow{
		{name: "AC-2 the loop's files left out of a diff a program reads", command: `git diff --numstat -- . ':(exclude).sdlc' | python3 -c 'import sys; print(sys.stdin.read())'`},
		{name: "AC-2 the short form", command: `git log --oneline -- . ':!.sdlc' ':!CLAUDE.md' | perl -ne 'print'`},
		{name: "AC-2 the frozen test left out of a diff awk sums", command: `git diff --numstat -- . ':(exclude)internal/invoice_test.go' | awk '{a+=$1} END {print a}'`},
		{name: "AC-2 every test left out of a diff sed reads", command: `git diff --stat -- . ':(exclude)*_test.go' | sed -n 1,3p`},
		{name: "AC-2 from an agent", agent: "sdlc:implementer", command: `git diff --numstat -- . ':^internal/invoice_test.go' | awk '{print}'`},
		{name: "AC-2 from Monitor", tool: "Monitor", command: `git diff --numstat -- . ':(exclude).sdlc' | perl -ne 'print'`},
		{name: "AC-2 from PowerShell", tool: "PowerShell", command: `git diff --numstat -- . ':(exclude).sdlc' | perl -ne 'print'`},
	})
}

func TestAC3TheHookLetsThroughARequestForHelp(t *testing.T) {
	namedRun(t, []namedRow{
		{name: "AC-3 the help of approve", command: `sdlc approve --help`},
		{name: "AC-3 the short flag after the story", command: `sdlc approve A-1 -h`},
		{name: "AC-3 the help of unfreeze, read through a pager", command: `sdlc unfreeze --help 2>&1 | head -20`},
		{name: "AC-3 the help of ack", command: `sdlc ack --help`},
		{name: "AC-3 the help of release", command: `sdlc release --help`},
		{name: "AC-3 the help of stop", command: `sdlc stop -h`},
		{name: "AC-3 the help of an assignment", command: `sdlc staffing assign --help`},
		{name: "AC-3 from an agent", agent: "sdlc:implementer", command: `sdlc approve --help`},
		{name: "AC-3 from Monitor", tool: "Monitor", command: `sdlc approve --help`},
		{name: "AC-3 from PowerShell", tool: "PowerShell", command: `sdlc approve --help`},
	})
}

// A session that does not hold the story is asked only about a person's
// decisions, and so is every session while the story waits for one.
func TestAC3TheHookLetsAnotherSessionAskForHelpWhileAStoryWaitsForAPerson(t *testing.T) {
	root := loopProject(t)
	escalated(t, root, false)
	for _, command := range []string{`sdlc approve --help`, `sdlc approve A-1 -h`, `sdlc unfreeze --help`, `sdlc ack -h`, `sdlc release A-1 --help`} {
		t.Run("AC-3 "+command, func(t *testing.T) {
			for _, session := range []string{working, "session-b"} {
				if got := call(t, inSession(namedAsk(root, "Bash", "", command), session), noEnv); denied(got) {
					t.Errorf("refused for %s: %s", session, namedReason(got))
				}
			}
		})
	}
}

func TestAC5TheHookStillRefusesTheActNearestToEachMention(t *testing.T) {
	namedRun(t, []namedRow{
		{name: "AC-5 a redirect after the text", command: `sdlc escalate spec_unclear --message "x" > .sdlc/config.json`, rule: namedLoopState},
		{name: "AC-5 a command after the text", command: `sdlc escalate spec_unclear --message "the hook refused"; rm .sdlc/state/active`, rule: namedLoopState},
		{name: "AC-5 the text outside quotes", command: `sdlc escalate spec_unclear --message refused; rm .sdlc/state/active`, rule: namedLoopState},
		{name: "AC-5 a substitution in the text", command: `sdlc gate analysis pass --note "$(echo y > CLAUDE.md)"`, rule: namedProtected},
		{name: "AC-5 a quote that closes before the operator", command: `sdlc escalate spec_unclear --message "a\\"; rm .sdlc/state/active; echo "b"`, rule: namedLoopState},
		{name: "AC-5 the frozen test written after the text", command: `sdlc escalate frozen_test_wrong --message "wrong" > internal/invoice_test.go`, rule: namedFrozen},
		{name: "AC-5 the same text to another program", command: `echo "a; rm CLAUDE.md"`, rule: namedProtected},
		{name: "AC-5 an approval run inside the text", command: `sdlc escalate spec_unclear --message "$(sdlc approve A-1)"`, rule: namedApproval},
		{name: "AC-5 from Monitor", tool: "Monitor", command: `sdlc escalate spec_unclear --message "x"; rm .sdlc/state/active`, rule: namedLoopState},
		{name: "AC-5 from PowerShell", tool: "PowerShell", command: `sdlc escalate spec_unclear --message 'x'; Remove-Item .sdlc/state/active`, rule: namedLoopState},

		{name: "AC-5 a pathspec that includes the path", command: `git diff -- .sdlc ':(exclude)docs' | perl -ne 'print'`, rule: namedLoopState},
		{name: "AC-5 magic that is not exclude", command: `git diff -- ':(top).sdlc' | perl -ne 'print'`, rule: namedLoopState},
		{name: "AC-5 the excluded path written by the program beside it", command: `git diff -- . ':(exclude).sdlc' | python3 -c 'open(".sdlc/config.json","w")'`, rule: namedLoopState},
		{name: "AC-5 the excluded path written by a redirect", command: `git diff --numstat -- . ':(exclude).sdlc' > .sdlc/state/active`, rule: namedLoopState},
		{name: "AC-5 a checkout of everything but the path", command: `git checkout -- . ':(exclude)docs'`, rule: namedLoopState},
		{name: "AC-5 the frozen test written by awk beside its exclude", command: `git diff -- . ':(exclude)internal/invoice_test.go' | awk '{print > "internal/invoice_test.go"}'`, rule: namedFrozen},
		{name: "AC-5 an exclude that is not git's", command: `echo ':(exclude).sdlc' | perl -ne 'print'`, rule: namedLoopState},

		{name: "AC-5 an approval", command: `sdlc approve A-1`, rule: namedApproval},
		{name: "AC-5 a help flag turned off", command: `sdlc approve --help=false A-1`, rule: namedApproval},
		{name: "AC-5 a help flag turned off by a later one", command: `sdlc approve --help --help=false A-1`, rule: namedApproval},
		{name: "AC-5 a help flag taken as a value", command: `sdlc approve --reject --help`, rule: namedApproval},
		{name: "AC-5 a help flag after the double dash", command: `sdlc approve -- --help`, rule: namedApproval},
		{name: "AC-5 help in one command and the approval in the next", command: `sdlc approve --help; sdlc approve A-1`, rule: namedApproval},
		{name: "AC-5 help piped to a shell", command: `sdlc approve --help | sh`, rule: namedApproval},
		{name: "AC-5 an unfreeze with its help turned off", command: `sdlc unfreeze -h=false --reason x`, rule: namedUnfreeze},
		{name: "AC-5 from PowerShell, a help flag turned off", tool: "PowerShell", command: `sdlc approve --help --help=false A-1`, rule: namedApproval},
	})
}

func TestAC6TheHookSaysItCouldNotTellWhereAnArgumentCannotBePlaced(t *testing.T) {
	root := namedProject(t)
	for _, c := range []namedRow{
		{name: "AC-6 an abbreviated flag", command: `sdlc escalate spec_unclear --messag "see -> .sdlc/config.json"`, rule: namedLoopState},
		{name: "AC-6 a double dash before the flag", command: `sdlc escalate spec_unclear -- --message "see -> .sdlc/config.json"`, rule: namedLoopState},
		{name: "AC-6 sdlc by another spelling", command: `./sdlc gate analysis pass --note 'see -> CLAUDE.md'`, rule: namedProtected},
		{name: "AC-6 an exclude with pathspecs read literally", command: `git --literal-pathspecs diff -- . ':(exclude).sdlc' | perl -ne 'print'`, rule: namedLoopState},
		{name: "AC-6 the same by the environment", command: `GIT_LITERAL_PATHSPECS=1 git diff -- . ':!internal/invoice_test.go' | awk '{print}'`, rule: namedFrozen},
	} {
		t.Run(c.name, func(t *testing.T) {
			reason := strings.ToLower(namedReason(c.check(t, root)))
			if !strings.Contains(reason, "cannot tell") && !strings.Contains(reason, "could not tell") {
				t.Errorf("the refusal of %s does not say the hook could not tell:\n%s", c.command, reason)
			}
		})
	}
}

// --- AC-2: git is asked which pathspecs exclude ------------------------------

// namedPathspecs are words after `--` and whether git reads each as leaving
// .sdlc out of everything else. Git is asked below; the hook is held to git's
// answer, and not to a reading of git's documentation.
var namedPathspecs = []struct {
	word     string
	excludes bool
}{
	{":(exclude).sdlc", true},
	{":!.sdlc", true},
	{":^.sdlc", true},
	{":(exclude,top).sdlc", true},
	{":(top,exclude).sdlc", true},
	{":(exclude,icase).SDLC", true},
	{":(exclude,glob).sdlc/**", true},
	{":/!.sdlc", true},

	{".sdlc", false},
	{":(top).sdlc", false},
	{":(glob).sdlc/**", false},
	{":(icase).SDLC", false},
	{":(literal).sdlc", false},
	{":.sdlc", false},
	{":(excluded).sdlc", false},
	{":(exclud).sdlc", false},
	{":(Exclude).sdlc", false},
	{"::!.sdlc", false},
	{"!.sdlc", false},
	{"^.sdlc", false},
	{":(exclude) .sdlc", false},
	{":(exclude)docs:.sdlc", false},
}

func TestAC2GitLeavesThePathOutForEachPathspecTheTableCallsAnExclude(t *testing.T) {
	root := wtTempDir(t)
	wtGit(t, root, "init", "--quiet", "--initial-branch=main")
	write(t, root, ".sdlc/config.json", `{"version":1}`)
	write(t, root, "docs/a.md", "a\n")
	wtGit(t, root, "add", "-A")

	for _, p := range namedPathspecs {
		t.Run("AC-2 "+p.word, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "git", "ls-files", "--", ".", p.word)
			cmd.Dir = root
			var listed bytes.Buffer
			cmd.Stdout = &listed
			failed := cmd.Run()
			// An exclude leaves the rest in; a word git will not read lists nothing.
			excludes := failed == nil && strings.Contains(listed.String(), "docs/a.md") && !strings.Contains(listed.String(), ".sdlc/config.json")
			if excludes != p.excludes {
				t.Errorf("git ls-files -- . %q leaves .sdlc out: %v, and the table says %v; it listed:\n%s", p.word, excludes, p.excludes, listed.String())
			}
		})
	}
}

func namedReadBesideAProgram(word string) string {
	return "git diff --numstat -- . '" + word + "' | perl -ne 'print'"
}

func TestAC2TheHookLetsThroughEveryPathspecGitReadsAsAnExclude(t *testing.T) {
	root := loopProject(t)
	for _, p := range namedPathspecs {
		if p.excludes {
			t.Run("AC-2 "+p.word, func(t *testing.T) { namedRow{command: namedReadBesideAProgram(p.word)}.check(t, root) })
		}
	}
}

func TestAC5TheHookStillRefusesEveryPathspecGitDoesNotReadAsAnExclude(t *testing.T) {
	root := loopProject(t)
	for _, p := range namedPathspecs {
		if !p.excludes {
			t.Run("AC-5 "+p.word, func(t *testing.T) {
				namedRow{command: namedReadBesideAProgram(p.word), rule: namedLoopState}.check(t, root)
			})
		}
	}
}

// --- AC-4: a file that is neither code nor a test ---------------------------

const namedDelegates = "orchestrator-delegates"

// namedRoute is what a refusal says to do instead.
func namedRoute(t *testing.T, r reply) string {
	t.Helper()
	_, route, found := strings.Cut(filepath.ToSlash(namedReason(r)), "Instead: ")
	if !found {
		t.Fatalf("the refusal does not say what to do instead: %s", namedReason(r))
	}
	return route
}

// The main conversation is refused a file of its own outside the story's
// directory, as it always was, and is now told where such a file goes: the
// story's directory in the checkout the session is in. The shell would write
// the same file anywhere, code included, so the route may not offer it.
func TestAC4TheMainConversationIsToldWhereAFileThatIsNotTheStorysWorkGoes(t *testing.T) {
	root := loopProject(t)
	for _, file := range []string{"commit-msg.txt", "notes/handoff.md", "internal/billing/invoice.go"} {
		for _, tool := range []string{"Write", "Edit"} {
			t.Run("AC-4 "+tool+" "+file, func(t *testing.T) {
				got := call(t, event(root, tool, "", file), noEnv)
				if !denied(got) || !strings.Contains(namedReason(got), "["+namedDelegates+"]") {
					t.Fatalf("the main conversation was not refused under %s: %s", namedDelegates, namedReason(got))
				}
				route := namedRoute(t, got)
				if !strings.Contains(route, ".sdlc/stories/A-1") {
					t.Errorf("the route does not name the story's own directory, where such a file is allowed: %s", route)
				}
				if !strings.Contains(route, "delegate") {
					t.Errorf("the route no longer says to delegate the story's work: %s", route)
				}
				namedOffersNoShell(t, route)
			})
		}
	}
}

// namedOffersNoShell fails a route that points at the shell: a redirect from
// the main conversation writes any unprotected file, and a route that says so
// teaches the way round the rule it belongs to.
func namedOffersNoShell(t *testing.T, route string) {
	t.Helper()
	lower := strings.ToLower(route)
	for _, word := range []string{"shell", "bash", "redirect", "printf", "heredoc", "here-doc", "cat >", "echo ", "tee "} {
		if strings.Contains(lower, word) {
			t.Errorf("the route offers the shell (%q) as the way to write the file: %s", word, route)
		}
	}
}

func TestAC4TheStorysOwnDirectoryIsWhereTheMainConversationMayWrite(t *testing.T) {
	root := loopProject(t)
	for _, file := range []string{".sdlc/stories/A-1/commit-msg.txt", ".sdlc/stories/A-1/notes/handoff.md"} {
		for _, tool := range []string{"Write", "Edit"} {
			t.Run("AC-4 "+tool+" "+file, func(t *testing.T) {
				if got := call(t, event(root, tool, "", file), noEnv); denied(got) {
					t.Errorf("the route names the story's directory and the write there is refused: %s", namedReason(got))
				}
			})
		}
	}
	// What the loop keeps in the same directory is still the tool's to write.
	for file, rule := range map[string]string{
		".sdlc/stories/A-1/gate-record.json": "gate-record-is-written-by-the-tool",
		".sdlc/stories/A-1/ANALYSIS.md":      "gate-artifact-is-written-by-the-tool",
	} {
		t.Run("AC-4 AC-5 "+file, func(t *testing.T) {
			got := call(t, event(root, "Write", "", file), noEnv)
			if !denied(got) || !strings.Contains(namedReason(got), "["+rule+"]") {
				t.Errorf("writing %s was not refused under %s: %s", file, rule, namedReason(got))
			}
		})
	}
}

// With the story in a worktree there are two directories of that name, and
// only the one in the session's checkout is the session's to write: the main
// checkout's is outside the repository to a session in the worktree.
func TestAC4TheRouteNamesTheStorysDirectoryInTheSessionsCheckout(t *testing.T) {
	l := swCheckouts(t)
	inMain := filepath.ToSlash(filepath.Join(l.main, ".sdlc", "stories"))
	for _, o := range swOpenings(l) {
		t.Run("AC-4 "+o.name, func(t *testing.T) {
			got := swCall(t, swHeld, swFile("", o, "Write", "", filepath.Join(l.wt, "commit-msg.txt")), nil)
			if !denied(got) || !strings.Contains(swReason(got), "["+namedDelegates+"]") {
				t.Fatalf("the main conversation was not refused under %s: %s", namedDelegates, swReason(got))
			}
			route := namedRoute(t, got)
			if !strings.Contains(route, ".sdlc/stories/A-1") {
				t.Errorf("the route does not name the story's own directory: %s", route)
			}
			if strings.Contains(route, inMain) {
				t.Errorf("the route names the main checkout's copy, which this session may not write: %s", route)
			}
			namedOffersNoShell(t, route)

			there := swFile("", o, "Write", "", filepath.Join(l.wt, ".sdlc", "stories", "A-1", "commit-msg.txt"))
			if got := swCall(t, swHeld, there, nil); denied(got) {
				t.Errorf("the write to the story's directory in the worktree is refused: %s", swReason(got))
			}
		})
	}
}
