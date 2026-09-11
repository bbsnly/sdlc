// Package policy decides who may write where while a story is being worked on.
//
// This is the loop's separation of duties, and it is enforced here rather than
// asked for in a prompt. An instruction is advice; a rule is a rule.
//
// Every denial names the rule in one clause and then names the sanctioned route
// to the same end. "Denied" on its own makes an assistant retry, work around
// the hook, or give up -- all worse outcomes than the thing being prevented.
package policy

import (
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// Request is what is about to happen.
type Request struct {
	Tool    string // Write, Edit, MultiEdit, NotebookEdit
	Agent   string // the role asking; empty is the main conversation
	Path    string // repository-relative, slash-separated
	Outside bool   // the path resolves outside the repository
	Story   string // the story the iteration is on
	Tests   Tests  // what the freeze says about this path
}

// Tests is the freeze, as it applies to one path. The caller works these out --
// from the project's configuration and the lock on disk -- so that the rules
// stay a decision table that can be read and tested on its own.
type Tests struct {
	IsTest   bool // the project's conventions say this path is a test
	Frozen   bool // this story's acceptance tests have been frozen
	Locked   bool // this exact file is part of the freeze
	AllowNew bool // configuration permits new test files after the freeze
}

// NormalizeAgent reduces an agent name to the bare role the rules are written
// against.
//
// The same agent arrives under three names depending on how it was installed:
// "sdlc:researcher" from the plugin, "sdlc-researcher" from a loose agent file,
// and "researcher" if someone renamed it. A rule that matched only one of them
// would silently stop enforcing when the install method changed, which is the
// worst way for a security control to fail.
func NormalizeAgent(name string) string {
	name = strings.TrimSpace(name)
	for _, prefix := range []string{"sdlc:", "sdlc-"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			return rest
		}
	}
	return name
}

// Verdict is the answer. An allowed verdict carries nothing else: there is
// nothing to say about a write that is fine.
type Verdict struct {
	Allowed bool
	Rule    string // the rule that refused, for the docs and the tests
	Reason  string // what the rule is, in one clause
	Route   string // the sanctioned way to achieve the same thing
}

// Allowed is the verdict for the overwhelmingly common case.
var Allowed = Verdict{Allowed: true}

// Rule is one enforceable statement. Route is not optional: a rule that cannot
// say what to do instead is a rule that will be worked around.
type Rule struct {
	ID    string
	Route string
	// check returns the reason when the rule refuses, and "" when it does not.
	check func(Request) string
}

// Protected paths. Configuration is the human's, and loop state is the CLI's:
// an assistant editing either can make the record say something that did not
// happen.
var protectedPaths = []string{
	".git",
	".claude",
	"CLAUDE.md",
	".sdlc/config.json",
	".sdlc/state",
	".sdlc/claude-progress.json",
}

// Rules run in order, and the first refusal wins. The order is from the most
// general to the most specific, so that a message about who may write where
// never appears for a path nobody may write at all.
var Rules = []Rule{
	{
		ID:    "write-outside-repository",
		Route: "work inside the repository; if you need a file elsewhere, ask the user to create it",
		check: func(r Request) string {
			if !r.Outside {
				return ""
			}
			return "this path is outside the repository, and the loop only governs " +
				"what it can review"
		},
	},
	{
		ID: "write-protected-path",
		Route: "configuration is yours to change by hand outside a running iteration, " +
			"and loop state changes through the sdlc command",
		check: func(r Request) string {
			if !pathrules.UnderAny(r.Path, protectedPaths...) {
				return ""
			}
			return r.Path + " is protected while a story is being worked on: it is either " +
				"human-owned configuration or the loop's own record of what happened"
		},
	},
	{
		ID: "frozen-test-is-not-edited",
		Route: "change the code until the test passes; if the test itself is wrong, " +
			"say which acceptance criterion it got wrong and run " +
			"`sdlc unfreeze --reason \"...\"` so the change is on the record",
		check: func(r Request) string {
			if !r.Tests.Locked {
				return ""
			}
			return r.Path + " is a frozen acceptance test. It was locked by content when " +
				"the test gate passed, and an agent that can edit its own tests will " +
				"eventually edit them -- which makes every gate after this one theatre"
		},
	},
	{
		ID: "no-new-test-after-the-freeze",
		Route: "put the case in one of the frozen files, or run " +
			"`sdlc unfreeze --reason \"...\"` and freeze again so the new file is covered",
		check: func(r Request) string {
			if !r.Tests.Frozen || !r.Tests.IsTest || r.Tests.Locked || r.Tests.AllowNew {
				return ""
			}
			return r.Path + " would be a new test file added after the freeze, which is " +
				"the freeze with extra steps: a test written now can be written to pass"
		},
	},
	{
		ID: "implementer-does-not-write-tests",
		Route: "make the existing tests pass; if they are wrong, say so rather than " +
			"changing them",
		check: func(r Request) string {
			if NormalizeAgent(r.Agent) != "implementer" || !r.Tests.IsTest {
				return ""
			}
			return "the agent that implements a story does not write its tests -- that " +
				"is the separation the loop is made of"
		},
	},
	{
		ID: "sdet-writes-tests-only",
		Route: "write the acceptance tests; the code that makes them pass comes next, " +
			"from sdlc:implementer. If this project keeps fixtures somewhere the " +
			"loop does not recognise, add that directory to paths.tests in " +
			".sdlc/config.json",
		check: func(r Request) string {
			if NormalizeAgent(r.Agent) != "sdet" || r.Tests.IsTest {
				return ""
			}
			if pathrules.UnderAny(r.Path, storyDir(r.Story), "CODEMAP.md") {
				return ""
			}
			return "the agent that writes the acceptance tests does not write the code " +
				"they are meant to fail against -- a test written beside its " +
				"implementation tests the implementation, not the criterion"
		},
	},
	{
		ID: "researcher-writes-analysis-only",
		Route: "keep to the story's own directory and CODEMAP.md, and persist the " +
			"analysis with `sdlc artifact write`; the tests come next, from " +
			"sdlc-sdet, and the code after that",
		check: func(r Request) string {
			if NormalizeAgent(r.Agent) != "researcher" {
				return ""
			}
			if pathrules.UnderAny(r.Path, storyDir(r.Story), "CODEMAP.md") {
				return ""
			}
			return "the analysis agent writes analysis, not code or tests"
		},
	},
	{
		ID: "reviewer-reviews",
		Route: "record the verdict with `sdlc review add <gate> <role> approve|block`, " +
			"and say what is wrong rather than fixing it -- a reviewer that " +
			"changes the work is reviewing its own",
		check: func(r Request) string {
			role := NormalizeAgent(r.Agent)
			if !model.IsReviewRole(role) {
				return ""
			}
			// The story's own directory stays open, the way it is for every
			// other agent: a reviewer working something out on paper is not a
			// reviewer changing the work. The review itself still goes through
			// `sdlc review add`, which the rule below holds it to.
			if pathrules.UnderAny(r.Path, storyDir(r.Story)) {
				return ""
			}
			return "a reviewer does not write the work it is reviewing: " + role +
				" says what is wrong and somebody else changes it, which is the " +
				"separation that makes a review worth having"
		},
	},
	{
		ID: "bookkeeper-writes-the-retro-and-the-map",
		Route: "store the retro with `sdlc artifact write retro`; CODEMAP.md is the " +
			"only other thing this gate produces",
		check: func(r Request) string {
			if NormalizeAgent(r.Agent) != "bookkeeper" {
				return ""
			}
			if pathrules.UnderAny(r.Path, storyDir(r.Story), "CODEMAP.md") {
				return ""
			}
			return "the retro records what happened; it does not change it"
		},
	},
	{
		ID: "gate-record-is-written-by-the-tool",
		Route: "record outcomes with `sdlc gate <name> pass|fail --note \"...\"`; " +
			"the record is the loop's memory and every later gate reads it",
		check: func(r Request) string {
			if !strings.EqualFold(fileAt(r.Story, r.Path), model.RecordFile) {
				return ""
			}
			return model.RecordFile + " is the loop's record of what happened, and a " +
				"record the assistant can edit is not a record"
		},
	},
	{
		ID: "review-is-written-by-the-tool",
		Route: "run `sdlc review add <gate> <role> <verdict>` and give it the review; " +
			"it records who reviewed what, and what they were looking at",
		check: func(r Request) string {
			if !pathrules.Under(r.Path, storyDir(r.Story)+"/reviews") {
				return ""
			}
			return "a review is part of a gate's record: it is written by sdlc, which " +
				"stamps it with the thing that was reviewed so that a later change " +
				"makes the approval stale instead of silently standing"
		},
	},
	{
		ID: "gate-artifact-is-written-by-the-tool",
		Route: "run `sdlc artifact write <name>` and give it the document -- " +
			"delegate to the agent whose gate it is rather than writing it yourself",
		check: func(r Request) string {
			artifact, ok := artifactAt(r.Story, r.Path)
			if !ok {
				return ""
			}
			return artifact.File + " is a gate's own record and is written by sdlc, " +
				"not edited in place -- the same rule as every other piece of loop state"
		},
	},
	{
		ID: "orchestrator-delegates",
		Route: "delegate the work to the agent whose gate it is, " +
			"or run `sdlc stop` to end the iteration and take over yourself",
		check: func(r Request) string {
			if NormalizeAgent(r.Agent) != "" {
				return ""
			}
			if pathrules.UnderAny(r.Path, ".sdlc", "CODEMAP.md") {
				return ""
			}
			return "the main conversation does not write code or tests during an " +
				"iteration; the gates exist so that each is done by an agent that " +
				"cannot see the others' reasoning"
		},
	},
}

// governedTools are the ones that write. Anything else is not this package's
// business, and saying so here keeps the rule from depending on how the hook
// happens to be configured.
var governedTools = map[string]bool{
	"Write":        true,
	"Edit":         true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

// Evaluate applies the rules in order.
func Evaluate(r Request) Verdict {
	if !governedTools[r.Tool] {
		return Allowed
	}
	if r.Path == "" && !r.Outside {
		return Allowed
	}
	for _, rule := range Rules {
		if reason := rule.check(r); reason != "" {
			return Verdict{Rule: rule.ID, Reason: reason, Route: rule.Route}
		}
	}
	return Allowed
}

// Message is what the assistant reads when a write is refused: the rule, then
// the way to do what it was trying to do.
func (v Verdict) Message() string {
	if v.Allowed {
		return ""
	}
	return v.Reason + ". Instead: " + v.Route + " [" + v.Rule + "]"
}

// artifactAt reports the gate artifact at this path, if there is one.
//
// Without this rule the loop has a hole exactly where it matters: the main
// conversation may write under .sdlc/ for its own bookkeeping, and the analysis
// lives under .sdlc/, so it could do Gate 2 itself and record a pass on its own
// work. Every gate after that would be reviewing something shaped by the
// reasoning it was supposed to be independent of.
//
// The rule refuses everyone, including the agent whose gate it is. That is not
// a compromise: Claude Code refuses a subagent's Write when the filename reads
// like a report, so "only the researcher may write ANALYSIS.md" is a rule that
// nobody can satisfy. Going through the tool works for every writer and matches
// what the loop already does with the rest of its state.
func artifactAt(story, path string) (model.Artifact, bool) {
	return model.ArtifactByFile(fileAt(story, path))
}

// fileAt is the name of the file this path names directly inside the story's
// own directory, or "" if it is anywhere else. Anywhere else includes a
// subdirectory of it: the gates' own files sit at the top level, and a
// reviewer's notes underneath are nobody's business but theirs.
func fileAt(story, path string) string {
	rest, inStory := strings.CutPrefix(path, storyDir(story)+"/")
	if !inStory || rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

func storyDir(story string) string {
	if story == "" {
		return ".sdlc/stories"
	}
	return ".sdlc/stories/" + strings.TrimSpace(story)
}
