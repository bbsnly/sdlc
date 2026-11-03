package shellpolicy

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The reproduced bypass (ANALYSIS.md 3a, T1). reviewRole skips a flag's value only
// when ValueFlags lists the flag; otherwise the value sits between the gate and
// the role, the pair is not found, and the implementer records the code reviewer's
// verdict. Cobra reads the same words as gate code_review, role code-reviewer.
func TestAC3ACountFlagCannotMoveARoleOutOfTheRuleThatKeepsAReviewItsReviewersOwn(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--important", "--nits"} {
		if !ValueFlags[flag] {
			t.Errorf("ValueFlags does not list %s", flag)
		}
	}
	const rule = "review-is-recorded-by-its-reviewer"
	implementer := State{CommitReady: true, Agent: "implementer"}
	reviewer := State{CommitReady: true, Agent: "code-reviewer"}
	for _, command := range []string{
		// Between the gate and the role: the two the released rule did not see.
		"sdlc review add code_review --important 3 code-reviewer block",
		"sdlc review add code_review --nits 3 code-reviewer block",
		"sdlc review add code_review --nits 3 code-reviewer approve",
		// With = the value is part of the flag's word.
		"sdlc review add code_review --important=3 code-reviewer block",
		"sdlc review add code_review --nits=3 code-reviewer block",
		// Before the gate.
		"sdlc review add --important 3 code_review code-reviewer block",
		"sdlc review add --nits 3 code_review code-reviewer block",
		"sdlc review add --important=3 --nits=1 code_review code-reviewer approve",
		// Repeated, and both.
		"sdlc review add code_review --important 3 --important 4 code-reviewer block",
		"sdlc review add code_review --nits 1 --nits 2 code-reviewer block",
		"sdlc review add code_review --important 3 --nits 2 code-reviewer block",
		"sdlc review add --important 3 code_review --nits 2 code-reviewer block",
		// After the verdict, and a value the flag's parser takes as its own.
		"sdlc review add code_review code-reviewer block --important 3",
		"sdlc review add code_review --important -1 code-reviewer block",
		"sdlc review add code_review --nits -1 code-reviewer approve",
	} {
		t.Run("AC-3 "+command, func(t *testing.T) {
			t.Parallel()
			refused(t, command, implementer, rule)
			// The reviewer the command names records its own, in every spelling.
			allowed(t, command, reviewer)
		})
	}
}

// The person's Q1: every agent's write to the policy is refused through the shell
// as through the file tools, keyed on the configured path as the backlog is, and
// folded as the disk folds it. Reading it is what every reviewer is told to do.
func TestQ1TheShellDoesNotRewriteTheReviewPolicy(t *testing.T) {
	t.Parallel()
	const rule = "protected-path-through-the-tool"
	writes := func(p string) []string {
		return []string{
			"echo weakened > " + p,
			"echo weakened >> " + p,
			"echo weakened | tee " + p,
			"cp /tmp/mine.md " + p,
			"mv /tmp/mine.md " + p,
			"mv " + p + " /tmp/gone.md",
			"sed -i 's/5 per review/500 per review/' " + p,
			"rm " + p,
		}
	}
	for _, c := range []struct {
		name, policy string
		spellings    []string
	}{
		{"the default", "REVIEW.md", []string{"REVIEW.md", "./REVIEW.md"}},
		{"folded", "REVIEW.md", []string{"review.md", "Review.MD"}},
		{"configured", "docs/policy.md", []string{"docs/policy.md", "./docs/policy.md"}},
		{"configured and folded", "Docs/Policy.md", []string{"docs/policy.md", "DOCS/POLICY.MD"}},
	} {
		for _, agent := range []string{"implementer", "", "code-reviewer", "sdet"} {
			t.Run("Q1 "+c.name+" as "+agent, func(t *testing.T) {
				t.Parallel()
				s := State{CommitReady: true, Agent: agent, ReviewPolicy: c.policy}
				for _, p := range c.spellings {
					for _, command := range writes(p) {
						refused(t, command, s, rule)
					}
				}
			})
		}
	}

	t.Run("Q1 reading it is not writing it", func(t *testing.T) {
		t.Parallel()
		s := State{CommitReady: true, Agent: "code-reviewer", ReviewPolicy: "REVIEW.md"}
		for _, command := range []string{"cat REVIEW.md", "grep nit REVIEW.md", "head -40 review.md", "sdlc review list"} {
			allowed(t, command, s)
		}
	})
	t.Run("Q1 moved, the old name is an ordinary file", func(t *testing.T) {
		t.Parallel()
		s := State{CommitReady: true, Agent: "implementer", ReviewPolicy: "docs/policy.md"}
		for _, command := range writes("REVIEW.md") {
			allowed(t, command, s)
		}
	})
	t.Run("Q1 no path named is no rule", func(t *testing.T) {
		t.Parallel()
		s := State{CommitReady: true, Agent: "implementer"}
		for _, command := range writes("REVIEW.md") {
			allowed(t, command, s)
		}
	})
	// A file of the same name in another directory is left unpinned: the shell
	// rules read a word without knowing for sure where it lands, and hold the
	// backlog's name at any depth for that reason.
	t.Run("Q1 a file beside it is not it", func(t *testing.T) {
		t.Parallel()
		s := State{CommitReady: true, Agent: "implementer", ReviewPolicy: "REVIEW.md"}
		for _, p := range []string{"REVIEW.md.txt", "REVIEWS.md"} {
			for _, command := range writes(p) {
				allowed(t, command, s)
			}
		}
	})
}

const rpProtected = "protected-path-through-the-tool"

// rpHeld is each kind of path a story holds, with the rule it already has.
var rpHeld = []struct{ name, policy, path, rule string }{
	{"the policy", "REVIEW.md", "REVIEW.md", rpProtected},
	{"a configured policy", "docs/policy.md", "docs/policy.md", rpProtected},
	{"a frozen test", "REVIEW.md", "internal/invoice_test.go", ruleFrozenTest},
	{"tests.lock", "REVIEW.md", ".sdlc/state/tests.lock", ruleLoopState},
	{"the loop's configuration", "REVIEW.md", ".sdlc/config.json", ruleLoopState},
	{"the contract", "REVIEW.md", "CLAUDE.md", rpProtected},
	{"the backlog", "REVIEW.md", "user_stories.json", rpProtected},
	{"the hold file", "REVIEW.md", ".sdlc/state/active", ruleLoopState},
	{"the session file", "REVIEW.md", ".sdlc/state/session", ruleLoopState},
}

func rpStory(policy string) State {
	return State{
		CommitReady: true, Agent: "implementer", ReviewPolicy: policy,
		Backlog: "user_stories.json", Frozen: []string{"internal/invoice_test.go"},
	}
}

// Security B1. A segment with no words still has its redirects, and they are
// judged as any command's are.
func TestQ1ARedirectWithNoCommandIsJudgedLikeAWrite(t *testing.T) {
	t.Parallel()
	for _, h := range rpHeld {
		t.Run("Q1 "+h.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range []string{"> " + h.path, ">> " + h.path, "true; > " + h.path} {
				refused(t, command, rpStory(h.policy), h.rule)
			}
		})
	}
	t.Run("Q1 what writes nothing held stays allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range []string{
			"< REVIEW.md", "cat < REVIEW.md", "2>/dev/null", "2>&1", "> /dev/null", "> /tmp/out", "> notes.md", "true; > notes.md",
		} {
			allowed(t, command, rpStory("REVIEW.md"))
		}
	})
}

// Security B2 and the architect's B1. The table of editors that rewrite a file
// given on the command line held ed and none of these. It is asked only of held
// paths, so an ordinary file stays free. nano has no batch mode, so no row.
func TestQ1AnEditorThatRewritesAFileIsAWriterOfAHeldPath(t *testing.T) {
	t.Parallel()
	editors := func(p string) []string {
		return []string{
			"ex -sc %d -c wq " + p,
			"vim -es -c %d -c wq " + p,
			"vi -es -c %d -c wq " + p,
			"nvim --headless -c %d -c wq " + p,
			"emacs --batch " + p + " -f save-buffer",
		}
	}
	for _, h := range rpHeld {
		t.Run("Q1 "+h.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range editors(h.path) {
				refused(t, command, rpStory(h.policy), h.rule)
			}
		})
	}
	t.Run("Q1 an ordinary file stays allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range editors("notes.md") {
			allowed(t, command, rpStory("REVIEW.md"))
		}
	})
}

// The architect's B2: one assignment was a way past the freeze. The value is on
// the line, so the line is read with it. Any assignment anywhere on the line
// counts, a branch or a subshell's too and one a later assignment replaces, since
// an ambiguous reading refuses. A value the line does not hold is declined: a
// program's output, or an unassigned `$OUT`, which everyday commands write to.
func TestQ1ATargetAssignedOnTheSameLineIsReadAsItsValue(t *testing.T) {
	t.Parallel()
	shapes := func(p string) []string {
		return []string{
			`T=` + p + `; echo w > "$T"`,
			`T=` + p + `; echo w >> "$T"`,
			`T=` + p + `; rm "$T"`,
			`T=` + p + `; cp a "$T"`,
			`T=` + p + `; mv a "$T"`,
			`T=` + p + `; echo w | tee "$T"`,
			`T=` + p + `; truncate -s 0 "$T"`,
			`export T=` + p + `; echo w > "$T"`,
			`export T=` + p + `; rm "$T"`,
			`T=` + p + `; echo w > "${T}"`,
			`T=` + p + `; rm "${T}"`,
			`T=` + p + `; echo w > $T`,
			`T=` + p + ` && rm "$T"`,
			`T="` + p + `"; rm "$T"`,
			// In a branch, in a subshell, and replaced before or after.
			`if true; then T=` + p + `; fi; echo w > "$T"`,
			`true && T=` + p + `; rm "$T"`,
			`(T=` + p + `); echo w > "$T"`,
			`T=notes.md; T=` + p + `; rm "$T"`,
			`T=` + p + `; T=notes.md; rm "$T"`,
		}
	}
	for _, h := range rpHeld {
		switch h.name {
		case "the policy", "a configured policy", "a frozen test", "tests.lock", "the hold file":
		default:
			continue
		}
		t.Run("Q1 "+h.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range shapes(h.path) {
				refused(t, command, rpStory(h.policy), h.rule)
			}
		})
	}
	t.Run("Q1 an assignment to an ordinary path stays allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range shapes("notes.md") {
			allowed(t, command, rpStory("REVIEW.md"))
		}
	})
	t.Run("Q1 what the assignment does not reach stays allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range []string{
			`echo x > "$OUT"`,
			`T=REVIEW.md; cat "$T"`,
			`T=REVIEW.md; echo w > notes.md`,
			`T=REVIEW.md; echo w > "$OUT"`,
		} {
			allowed(t, command, rpStory("REVIEW.md"))
		}
	})
}

// Red team's first gaming vector: the rule added at one protectedPath call, in
// loopFileFinding, passed every literal row, and `rm REV*` was then allowed. The
// backlog is asked after in several places; each has its row here, at the default
// path and at a configured one, so the policy has to be in all of them.
func TestQ1EveryPlaceThatAsksAfterAHeldPathAsksAfterThePolicy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, policy string
		s            func(State) State
		commands     []string
	}{
		{"a glob, at the default", "REVIEW.md", nil, []string{"rm REV*", "rm REVIEW.m?", "rm {REVIEW,x}.md"}},
		{"a glob, at a configured path", "docs/policy.md", nil, []string{"rm docs/pol*", "rm docs/policy.m?", "rm do*/policy.md"}},
		{"find by name, at the default", "REVIEW.md", nil, []string{"find . -name REVIEW.md -delete", "find . -name 'REVIEW.m?' -delete"}},
		{"find by name, at a configured path", "docs/policy.md", nil, []string{"find . -name policy.md -delete", "find . -path '*docs/pol*' -delete"}},
		{"find with an operator, at the default", "REVIEW.md", nil, []string{`find . -name 'REVIEW.m?' ! -name x -delete`}},
		{"find with an operator, at a configured path", "docs/policy.md", nil, []string{`find . -name 'pol*' ! -name x -delete`}},
		{"another checkout, at the default", "REVIEW.md", rpInAnotherCheckout, []string{"echo x > /work/main/REVIEW.md", "rm /work/main/review.md"}},
		{"another checkout, at a configured path", "docs/policy.md", rpInAnotherCheckout, []string{"echo x > /work/main/docs/policy.md"}},
		{"above the project, at the default", "REVIEW.md", rpAboveTheProject, []string{"rm ../project/REV*"}},
		{"above the project, at a configured path", "docs/policy.md", rpAboveTheProject, []string{"rm ../project/docs/pol*"}},
		{"a copy that lands on it, at the default", "REVIEW.md", rpAtADottedRoot, []string{"cp /tmp/p/REVIEW.md ../me.github.io"}},
		{"a copy that lands on it, at a configured path", "policy.md", rpAtADottedRoot, []string{"cp /tmp/p/policy.md ../me.github.io"}},
		{"a dotted directory and a glob that reaches it", ".github/REVIEW.md", nil, []string{"shopt -s dotglob; rm -f ?github/REVIEW.md"}},
	} {
		t.Run("Q1 "+c.name, func(t *testing.T) {
			t.Parallel()
			s := rpStory(c.policy)
			if c.s != nil {
				s = c.s(s)
			}
			for _, command := range c.commands {
				refused(t, command, s, rpProtected)
			}
		})
	}
	t.Run("Q1 after a cd the hook cannot place", func(t *testing.T) {
		t.Parallel()
		s := agHeldState()
		s.ReviewPolicy = "docs/policy.md"
		for _, command := range []string{`cd "$D" && rm -f policy.md`, "cd - && echo x > policy.md"} {
			u120Refused(t, "Q1", command, s, rpProtected)
		}
		s.ReviewPolicy = "REVIEW.md"
		u120Allowed(t, "Q1", `cd "$D" && rm -f policy.md`, s)
	})
}

func rpInAnotherCheckout(s State) State {
	policy := s.ReviewPolicy
	s = withShared(s, sharedLandings)
	s.ReviewPolicy = policy
	return s
}

func rpAboveTheProject(s State) State {
	above := agAboveStateAt("/src/project")
	above.ReviewPolicy, above.Agent = s.ReviewPolicy, s.Agent
	return above
}

func rpAtADottedRoot(s State) State {
	above := agAboveStateAt("/src/me.github.io")
	above.ReviewPolicy, above.Agent = s.ReviewPolicy, s.Agent
	return above
}

// rpValues is one name given n ordinary values on one line, then rest.
func rpValues(n int, rest string) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "T=notes%d.md; ", i)
	}
	return b.String() + rest
}

// Both design reviewers, round 2 (architect, "The statements with no row";
// security N5): the reader's bounds, which no other row reaches. A name is read
// with each of its values, one reading apiece, so past maxReadings values the
// line is refused rather than read in part. A line read longer than the bound on
// its growth is refused the same way. Each bound refuses; nothing past one is
// allowed unread.
func TestQ1TheAssignmentReaderRefusesALineItCannotReadWithinItsBounds(t *testing.T) {
	t.Parallel()
	const tooDeep = "command-too-deep-to-read"
	s := rpStory("REVIEW.md")
	t.Run("Q1 as many values of one name as the readings bound, and a use", func(t *testing.T) {
		t.Parallel()
		allowed(t, rpValues(maxReadings, `echo w > "$T"`), s)
	})
	t.Run("Q1 one value more", func(t *testing.T) {
		t.Parallel()
		refused(t, rpValues(maxReadings+1, `echo w > "$T"`), s, tooDeep)
	})
	long := "T=" + strings.Repeat("a", 1000) + "; echo"
	t.Run("Q1 a line read within the bound on its growth", func(t *testing.T) {
		t.Parallel()
		allowed(t, long+strings.Repeat(` "$T"`, 60), s)
	})
	t.Run("Q1 a line read past the bound on its growth", func(t *testing.T) {
		t.Parallel()
		refused(t, long+strings.Repeat(` "$T"`, maxAssignedGrowth/1000+2), s, tooDeep)
	})
	// Security N1: built first and counted after, 16,000 values of one name cost
	// 5.9 GB, and with no use no bound fired. Capped at maxReadings values, the
	// line is read in a few readings, and with nothing used it is allowed.
	t.Run("Q1 many values of one name and no use of it", func(t *testing.T) {
		t.Parallel()
		allowed(t, rpValues(4000, `echo w > "$OUT"`), s)
	})
}

// The architect's third bound row: a backslash inside a value is gone once the
// shell assigns it, so `REV\IEW.md` is the policy.
func TestQ1AValueWithABackslashInsideIsReadAsTheShellAssignsIt(t *testing.T) {
	t.Parallel()
	refused(t, `T=REV\IEW.md; rm "$T"`, rpStory("REVIEW.md"), rpProtected)
	allowed(t, `T=no\tes.md; rm "$T"`, rpStory("REVIEW.md"))
}

// Security, Gate 7 round 1 and 2 (N2): each bound has its own reason, and a
// reason given for the other bound would send a person to the wrong change.
func TestQ1EachBoundOfTheAssignmentReaderGivesItsOwnReason(t *testing.T) {
	t.Parallel()
	const tooDeep = "command-too-deep-to-read"
	s := rpStory("REVIEW.md")
	t.Run("Q1 past the readings bound", func(t *testing.T) {
		t.Parallel()
		f := refused(t, rpValues(maxReadings+1, `echo w > "$T"`), s, tooDeep)
		if !strings.Contains(f.Reason, "more than "+strconv.Itoa(maxReadings)+" readings") || strings.Contains(f.Reason, "KB") {
			t.Errorf("the reason does not name the readings bound: %s", f.Reason)
		}
		if !strings.Contains(f.Route, "--file") {
			t.Errorf("the route does not say how to hand over a document: %s", f.Route)
		}
	})
	t.Run("Q1 past the bound on its growth", func(t *testing.T) {
		t.Parallel()
		long := "T=" + strings.Repeat("a", 1000) + "; echo" + strings.Repeat(` "$T"`, maxAssignedGrowth/1000+2)
		f := refused(t, long, s, tooDeep)
		if !strings.Contains(f.Reason, "more than "+strconv.Itoa(maxAssignedGrowth>>10)+" KB longer") || strings.Contains(f.Reason, "readings") {
			t.Errorf("the reason does not name the bound on growth: %s", f.Reason)
		}
		if !strings.Contains(f.Route, "--file") {
			t.Errorf("the route does not say how to hand over a document: %s", f.Route)
		}
	})
}

// Security, Gate 7 round 1, I1: the shell removes a line continuation before it
// reads a use, so one between the `$` and the name is still the use.
func TestQ1AUseSplitByALineContinuationIsReadAsTheUse(t *testing.T) {
	t.Parallel()
	shapes := func(p string) []string {
		return []string{
			"T=" + p + "; rm \"$\\\nT\"",
			"T=" + p + "; rm $\\\nT",
			"T=" + p + "; rm \"$\\\n{T}\"",
			"T=" + p + "; rm \"${T\\\n}\"",
			"T=" + p + "; echo w > \"$\\\nT\"",
		}
	}
	for _, h := range rpHeld {
		switch h.name {
		case "the policy", "a frozen test", "the session file":
		default:
			continue
		}
		t.Run("Q1 "+h.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range shapes(h.path) {
				refused(t, command, rpStory(h.policy), h.rule)
			}
		})
	}
	t.Run("Q1 an ordinary path stays allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range shapes("notes.md") {
			allowed(t, command, rpStory("REVIEW.md"))
		}
	})
}

// Security, Gate 7 round 1, I2: in straight-line code the shell uses the last
// value of each name, and a value given again is the last.
func TestQ1TheLastValueOfEachNameIsReadTogether(t *testing.T) {
	t.Parallel()
	s := rpStory("REVIEW.md")
	for _, command := range []string{
		`T=a; T=REVIEW; U=.md; U=x; U=.md; rm "$T$U"`,
		`T=a; T=REVIEW; U=.md; U=x; U=.md; echo w > "$T$U"`,
	} {
		refused(t, command, s, rpProtected)
	}
}

// Security, Gate 7 round 1, I3, and the names the plan added beyond the frozen
// five: each is the same program under another name, so each writes a held path.
func TestQ1EveryEditorNameInTheWritersTableIsAWriterOfAHeldPath(t *testing.T) {
	t.Parallel()
	editors := func(p string) []string {
		return []string{
			"view " + p,
			"rvim " + p,
			"rview " + p,
			"vimdiff " + p,
			"gvim " + p,
			"vim.tiny -es -c %d -c wq " + p,
			"vim.basic -es -c %d -c wq " + p,
			"vim.nox -es -c %d -c wq " + p,
			"vim.gtk3 -es -c %d -c wq " + p,
			"nvi " + p,
			"red -s " + p,
			"emacs-nox --batch " + p + " -f save-buffer",
			"kak -n -f ggd " + p,
		}
	}
	for _, h := range rpHeld {
		t.Run("Q1 "+h.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range editors(h.path) {
				refused(t, command, rpStory(h.policy), h.rule)
			}
		})
	}
	t.Run("Q1 an ordinary file stays allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range editors("notes.md") {
			allowed(t, command, rpStory("REVIEW.md"))
		}
	})
}

// Security, Gate 7 round 2, F1: the shell runs the line after a comment that
// ends in a backslash, so that line is judged as any line is.
func TestQ1TheLineAfterACommentThatEndsInABackslashIsJudged(t *testing.T) {
	t.Parallel()
	s := rpStory("REVIEW.md")
	for _, c := range []struct{ name, line, rule string }{
		{"the contract", "rm CLAUDE.md", rpProtected},
		{"the policy", "rm REVIEW.md", rpProtected},
		{"a frozen test", "rm internal/invoice_test.go", ruleFrozenTest},
		{"a frozen test, copied over", "cp x internal/invoice_test.go", ruleFrozenTest},
		{"the loop's directory", "rm -rf .sdlc", ruleLoopState},
		{"the session file", "cp x .sdlc/state/session", ruleLoopState},
		{"the session file, emptied", "truncate -s 0 .sdlc/state/session", ruleLoopState},
	} {
		t.Run("Q1 "+c.name, func(t *testing.T) {
			t.Parallel()
			refused(t, c.line, s, c.rule)
			for _, before := range []string{"true # \\\n", "echo before # a comment \\\n"} {
				refused(t, before+c.line, s, c.rule)
			}
		})
	}
	t.Run("Q1 a target assigned before the comment", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct{ value, use, rule string }{
			{"REVIEW.md", `rm "$T"`, rpProtected},
			{"REVIEW.md", `cp x "$T"`, rpProtected},
			{"REVIEW.md", `mv x "$T"`, rpProtected},
			{"REVIEW.md", `sed -i d "$T"`, rpProtected},
			{"REVIEW.md", `vim -es -c %d -c wq "$T"`, rpProtected},
			{".sdlc/state/session", `truncate -s 0 "$T"`, ruleLoopState},
			{"internal/invoice_test.go", `cp x "$T"`, ruleFrozenTest},
		} {
			refused(t, "T="+c.value+" # \\\n"+c.use, s, c.rule)
		}
	})
	// The person's option A, Gate 7 round 5: the ordinary lines of this shape
	// that stood here are refused now, in rpAfterAContinuedComment.
	t.Run("Q1 a continued line with no comment stays allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range []string{
			"go test \\\n  ./...",
			"echo w \\\n  > notes.md",
		} {
			allowed(t, command, s)
		}
	})
}

// rpCommentAndContinuation needs a comment's backslash-newline kept as a line
// end and every other one joined, as the shell reads it, in a line and in a
// script handed to a shell.
var rpCommentAndContinuation = []struct{ name, command, rule string }{
	{"the path split", "true # \\\nrm CLAUDE.\\\nmd", rpProtected},
	{"the command word split", "true # \\\nr\\\nm CLAUDE.md", rpProtected},
	{"the path split midway", "true # \\\nrm CLA\\\nUDE.md", rpProtected},
	{"a frozen test split", "true # \\\nrm internal/invoice_\\\ntest.go", ruleFrozenTest},
	{"a quoted # before the path", "true # \\\nrm '#' CLAUDE.\\\nmd", rpProtected},
	{"a use split after the $", "T=REVIEW.md # \\\nrm \"$\\\nT\"", rpProtected},
	{"a braced use split", "T=REVIEW.md # \\\nrm \"${\\\nT}\"", rpProtected},
	{"a value split before the comment", "T=REV\\\nIEW.md # \\\nrm \"$T\"", rpProtected},
	{"handed to bash", "bash -c 'true # \\\nrm CLAUDE.\\\nmd'", rpProtected},
	{"handed to sh", "sh -c 'true # \\\nrm CLAUDE.\\\nmd'", rpProtected},
}

// Security, Gate 7 round 3: a comment ends at its line end, and a
// backslash-newline elsewhere on the line still joins.
func TestQ1ACommentEndsAtItsLineWhileEveryOtherContinuationJoins(t *testing.T) {
	t.Parallel()
	for _, c := range rpCommentAndContinuation {
		t.Run("Q1 "+c.name, func(t *testing.T) {
			t.Parallel()
			refused(t, c.command, rpStory("REVIEW.md"), c.rule)
		})
	}
}

// rpCommentShapes are other ways to write a comment that ends in a backslash.
var rpCommentShapes = []struct{ name, command, rule string }{
	{"a bare #", "#\\\nrm CLAUDE.md", rpProtected},
	{"a line that is only a comment", "# only a comment \\\nrm CLAUDE.md", rpProtected},
	{"several #", "ls ### heading \\\nrm CLAUDE.md", rpProtected},
	{"no space before the backslash", "echo hi #comment\\\nrm CLAUDE.md", rpProtected},
	{"CRLF, the policy", "true # \\\r\nrm REVIEW.md", rpProtected},
	{"CRLF, the contract", "true # \\\r\nrm CLAUDE.md", rpProtected},
	{"handed to sh", "sh -c 'true # \\\nrm CLAUDE.md'", rpProtected},
	{"handed to bash", "bash -c 'true # \\\nrm CLAUDE.md'", rpProtected},
	{"two comments in a row", "true # a \\\ntrue # b \\\nrm CLAUDE.md", rpProtected},
	{"inside if", "if true; then true # \\\nrm CLAUDE.md\nfi", rpProtected},
	{"inside a subshell", "( true # \\\nrm CLAUDE.md\n)", rpProtected},
	{"an assignment after the comment", "true # \\\nT=REVIEW.md; rm \"$T\"", rpProtected},
	{"a person's command after the comment", "true # \\\nsdlc approve US-1", "approval-is-a-human-decision"},
}

// The verifier, Gate 7 round 3: each shape of a comment ends at its line end.
func TestQ1EveryShapeOfACommentThatEndsInABackslashEndsAtItsLine(t *testing.T) {
	t.Parallel()
	for _, c := range rpCommentShapes {
		t.Run("Q1 "+c.name, func(t *testing.T) {
			t.Parallel()
			refused(t, c.command, rpStory("REVIEW.md"), c.rule)
		})
	}
}

// rpOrdinaryContinued are everyday lines with a comment, a # that is no
// comment, and a continuation, which the shell runs as one ordinary command.
// Three that stood here are refused now under the person's option A, in
// rpAfterAContinuedComment: each has a # that may open a comment on a line
// that ends in a backslash, with more after it.
var rpOrdinaryContinued = []string{
	"go test ./... \\\n-count=1 # all of it",
	"# build first\ngo build ./... \\\n&& go vet ./...",
	"echo \"# heading\" \\\n> notes.md",
	"curl -s http://example.com/#a \\\n-o out.html",
	"docker run --rm \\\n-e A=1 \\\nimg # run it",
	"rm -rf build \\\ndist # clean",
	"X=1; echo \"$\\\nX\"",
}

// The verifier, Gate 7 round 3: reading a comment to its line end leaves an
// ordinary continued line allowed.
func TestQ1AnOrdinaryLineWithACommentAndAContinuationStaysAllowed(t *testing.T) {
	t.Parallel()
	for _, command := range rpOrdinaryContinued {
		allowed(t, command, rpStory("REVIEW.md"))
	}
}

// rpSplitWrites are the held paths of a story removed with the name split by a
// continuation, so the write is only seen when the line is joined.
var rpSplitWrites = []struct{ name, rm string }{
	{"the contract", "rm CLAUDE.\\\nmd"},
	{"a frozen test", "rm internal/invoice_\\\ntest.go"},
	{"the policy", "rm REV\\\nIEW.md"},
}

// rpRefusedByAnyRule holds a row to a refusal that says why and what to do,
// whichever rule gives it.
func rpRefusedByAnyRule(t *testing.T, command string, s State) {
	t.Helper()
	f, ok := inspectNoPanic(t, command, s)
	switch {
	case !ok:
		t.Errorf("allowed: %q", command)
	case f.Reason == "" || f.Route == "":
		t.Errorf("%q: finding = %+v", command, f)
	}
}

// The verifier (V1, V2), the code reviewer (B1) and security (R4-1), Gate 7
// round 4: a comment that ends in a backslash, then a write split by a second
// continuation, is refused however the comment is reached: after an escaped
// quote or `$`, after `$$` or `)`, inside a string handed to a shell with `-c`
// among other flags or as a here-string, or to an eval spelled with a backslash
// or quotes.
func TestQ1AContinuedWriteAfterAContinuedCommentIsRefusedHoweverTheCommentIsReached(t *testing.T) {
	t.Parallel()
	afterComment := func(lead string) func(string) string {
		return func(rm string) string { return lead + " # \\\n" + rm }
	}
	handed := func(lead string) func(string) string {
		return func(rm string) string { return lead + " 'true # \\\n" + rm + "'" }
	}
	for _, c := range []struct {
		name string
		line func(string) string
	}{
		// V1 and R4-1 (c): -c with another flag in the same word.
		{"handed to sh -ec", handed("sh -ec")},
		{"handed to bash -lc", handed("bash -lc")},
		{"handed to bash -xc", handed("bash -xc")},
		{"handed to zsh -fc", handed("zsh -fc")},
		// R4-1 (c): eval spelled so the word holds no `eval`.
		{"handed to eval with a backslash in", handed(`e\val`)},
		{"handed to eval with quotes in", handed(`ev''al`)},
		{"handed to bash as a here-string", handed("bash <<<")},
		// V2: the escape and the ANSI-C string, which no row reached.
		{"after an escaped single quote", afterComment(`echo \'`)},
		{"after an escaped double quote", afterComment(`echo \"`)},
		{"after an ANSI-C string of a quote", afterComment(`echo $'\''`)},
		{"after an ANSI-C string with a quote in", afterComment(`echo $'a\'b'`)},
		// B1 and R4-1 (a): a `$` before a quote that opens no ANSI-C string.
		{"after an escaped $ before a quote", afterComment(`echo \$'a\'`)},
		{"after $$ before a quote", afterComment(`echo $$'a\'`)},
		// R4-1 (b): a comment that opens straight after `)`.
		{"after a parenthesis", func(rm string) string { return "(true)# \\\n" + rm }},
	} {
		for _, w := range rpSplitWrites {
			t.Run("Q1 "+c.name+", "+w.name, func(t *testing.T) {
				t.Parallel()
				rpRefusedByAnyRule(t, c.line(w.rm), rpStory("REVIEW.md"))
			})
		}
	}
}

// rpAfterAContinuedComment are lines where a physical line holding a # that
// may open a comment ends in an odd number of backslashes and more follows it,
// each writing only what an ordinary command writes. The rows marked moved
// were allowed before the person's option A, Gate 7 round 5.
var rpAfterAContinuedComment = []struct{ name, command string }{
	{"the name split", "true # \\\nrm notes.\\\nmd"},
	{"a redirect continued", "echo before # a comment \\\necho w \\\n> notes.md"},
	{"flags continued after the comment", "go test ./... # all of it \\\n-count=1 \\\n-v"},
	{"a line that is only a comment", "# a note \\\necho a \\\nb"},
	{"the second continuation lines later", "true # \\\necho next\necho w \\\n> notes.md"},
	{"ls after it", "true # \\\nls"},
	{"go test after three backslashes", "true # \\\\\\\ngo test ./..."},
	{"ls after it, CRLF", "true # \\\r\nls"},
	{"a # at the line's start", "# a note \\\nls"},
	{"moved, a line after a comment", "echo before # a comment \\\necho next line ran"},
	{"moved, an ordinary file removed", "true # \\\nrm notes.md"},
	{"moved, the policy read", "true # \\\ncat REVIEW.md"},
	{"moved, an ordinary file assigned", "T=notes.md # \\\nrm \"$T\""},
	{"moved, one continuation, the comment's own", "echo before # a comment \\\necho next"},
	{"moved, a continuation before the comment", "go test \\\n./... # all of it \\\necho done"},
	{"moved, a use after the comment", "X=1; echo \"$X\" # note \\\necho done"},
	{"moved, handed with -ec", "bash -ec 'echo hi # note \\\necho there'"},
	{"moved, a # in a quote after a space", "git log --format='%h #%s' \\\n-n 5"},
	{"moved, a # after a brace", "echo ${#PATH} \\\n&& echo ok"},
	// A known over-refusal, held on the record: a # in quotes after a space.
	{"a quoted issue number before a continued flag", "git commit -m \"fix #12\" \\\n--amend"},
}

// The person's option A, Gate 7 round 5: where a line holding a # that may open
// a comment ends in an odd number of backslashes, whether the next line is a
// command of its own turns on quotes the rules do not follow there, so anything
// after it is refused as too deep to read, whatever it writes. The route tells
// a person to write that part of the command on one line.
func TestQ1AnythingAfterALineThatMayEndAContinuedCommentIsRefusedAsTooDeepToRead(t *testing.T) {
	t.Parallel()
	s := rpStory("REVIEW.md")
	rows := slices.Clone(rpAfterAContinuedComment)
	// Each place a # may open a comment: after a blank, a tab or an operator,
	// and after `{`, `}`, `=`, `,`, `<` and `>`, which the readings take for one.
	for _, before := range []string{" ", "\t", ";", "&", "|", "(", ")", "{", "}", "=", ",", "<", ">"} {
		rows = append(rows, struct{ name, command string }{"a # after " + strconv.Quote(before), "echo a" + before + "# \\\nls"})
	}
	for _, c := range rows {
		t.Run("Q1 "+c.name, func(t *testing.T) {
			t.Parallel()
			f := refused(t, c.command, s, tooDeepRule)
			if !strings.Contains(strings.ToLower(f.Route), "on one line") {
				t.Errorf("the route does not tell a person to write that part of the command on one line: %+v", f)
			}
		})
	}
	t.Run("Q1 lines just outside the rule stay allowed", func(t *testing.T) {
		t.Parallel()
		for _, command := range []string{
			// A # only on the last line.
			"go test \\\n./... \\\n-count=1 # all of it",
			// Two and four backslashes.
			"true # \\\\\nls",
			"true # \\\\\\\\\nls",
			// No backslash at the comment's end.
			"true # note\nls",
			// A continued line with no #.
			"go test \\\n./...",
			// A # inside a word.
			"echo a#b \\\nls",
			// Nothing but blanks after it.
			"echo hi # note \\\n",
			"echo hi # note \\\n  \n",
			// A document whose body holds the shape, quoted and not.
			"cat > notes.md <<'EOF'\n# heading \\\nmore text \\\nand more\nEOF",
			"cat > notes.md <<EOF\n# heading \\\nmore text \\\nand more\nEOF",
		} {
			allowed(t, command, s)
		}
	})
}

// The verifier, Gate 7 round 5 (V3): in a comment a backslash is a character
// like any other, so a comment that ends in any number of them ends at its
// line, and the next line runs whether anything after it is continued or not.
func TestQ1ACommentThatEndsInAnOddNumberOfBackslashesHidesNoWriteOnTheNextLine(t *testing.T) {
	t.Parallel()
	forms := []struct {
		name string
		line func(end, next string) string
	}{
		{"LF", func(end, next string) string { return "true # " + end + "\n" + next }},
		{"CRLF", func(end, next string) string { return "true # " + end + "\r\n" + next }},
		{"after a command", func(end, next string) string { return "echo hi # note " + end + "\n" + next }},
		{"handed to sh -c", func(end, next string) string { return "sh -c 'true # " + end + "\n" + next + "'" }},
		{"handed to bash -c", func(end, next string) string { return "bash -c 'true # " + end + "\n" + next + "'" }},
	}
	held := []struct{ name, path string }{
		{"the policy", "REVIEW.md"},
		{"the contract", "CLAUDE.md"},
		{"a frozen test", "internal/invoice_test.go"},
		{"the session file", ".sdlc/state/session"},
	}
	for _, n := range []int{1, 3, 5} {
		for _, f := range forms {
			for _, h := range held {
				t.Run(fmt.Sprintf("Q1 %d backslashes, %s, %s", n, f.name, h.name), func(t *testing.T) {
					t.Parallel()
					for _, next := range []string{"rm " + h.path, "cp x " + h.path} {
						rpRefusedByAnyRule(t, f.line(strings.Repeat("\\", n), next), rpStory("REVIEW.md"))
					}
				})
			}
		}
	}
}

// The verifier (V4) and security (R5-2), Gate 7 round 5: a `$'...'` string is
// read for its comments as the shell decodes it, a # and line ends written as
// escapes included.
func TestQ1ADecodedStringIsReadForACommentThatEndsInABackslash(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, lead, hash, nl string }{
		{"a # as written, handed to bash -c", "bash -c", "#", `\n`},
		{"a # in hex, handed to bash -c", "bash -c", `\x23`, `\x0a`},
		{"a # in octal, handed to bash -c", "bash -c", `\043`, `\x0a`},
		{"a # in hex, handed to sh -c", "sh -c", `\x23`, `\x0a`},
		{"a # in hex, handed to zsh -c", "zsh -c", `\x23`, `\x0a`},
		{"a # in hex, handed to eval", "eval", `\x23`, `\x0a`},
	} {
		for _, w := range rpSplitWrites {
			t.Run("Q1 "+c.name+", "+w.name, func(t *testing.T) {
				t.Parallel()
				rm := strings.ReplaceAll(w.rm, "\\\n", `\\`+c.nl)
				rpRefusedByAnyRule(t, c.lead+" $'true "+c.hash+` \\`+c.nl+rm+"'", rpStory("REVIEW.md"))
			})
		}
	}
}

// Security (R5-1), Gate 7 round 5: a # after `=`, `,`, `{`, `<` or `>` is one
// that may open a comment, so the backslash that ends its line is not read as a
// join the shell may not make.
func TestQ1AHashAfterAnAssignmentACommaABraceOrARedirectMayOpenAComment(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, lead string }{
		{"after =", "A=#"},
		{"after ,", "A=x,#"},
		{"after {", "A={#"},
		{"after <", "A=x <#"},
		{"after >", "A=x >#"},
	} {
		t.Run("Q1 "+c.name, func(t *testing.T) {
			t.Parallel()
			rpRefusedByAnyRule(t, c.lead+" T=REV\\\nIEW.md # \\\nrm \"$T\"", rpStory("REVIEW.md"))
		})
	}
}

// Security (R5-4), Gate 7 round 5: outside a comment a backslash before a
// carriage return and newline escapes the carriage return and joins nothing. An
// ordinary line of that shape with a comment on it stays allowed, and one that
// names the contract stays refused, joined or not, the safe direction.
func TestQ1ABackslashBeforeACarriageReturnOutsideACommentIsRead(t *testing.T) {
	t.Parallel()
	s := rpStory("REVIEW.md")
	allowed(t, "rm notes.\\\r\nmd # note", s)
	rpRefusedByAnyRule(t, "rm CLAUDE.\\\r\nmd # note", s)
}
