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

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// Request is what is about to happen.
type Request struct {
	Tool    string // Write, Edit, MultiEdit, NotebookEdit
	Agent   string // the role asking; empty is the main conversation
	Path    string // repository-relative, slash-separated
	Outside bool   // the path resolves outside the repository
	Story   string // the story the iteration is on
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
		ID: "researcher-writes-analysis-only",
		Route: "write the analysis under the story's own directory; " +
			"the tests come next, from sdlc-sdet, and the code after that",
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
		ID: "story-artifact-belongs-to-its-gate",
		Route: "delegate it to the agent whose gate it is -- that agent starts from a " +
			"fresh context, which is the whole reason its answer is worth more than yours",
		check: func(r Request) string {
			owner, name, ok := artifactOwner(r.Story, r.Path)
			if !ok || NormalizeAgent(r.Agent) == owner {
				return ""
			}
			return name + " is written by the " + owner + " agent, not by whoever happens " +
				"to be holding the conversation"
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

// storyArtifacts names the file each gate produces and the role that owns it.
//
// Without this the loop has a hole exactly where it matters: the main
// conversation may write under .sdlc/ for its own bookkeeping, and the analysis
// lives under .sdlc/, so it could simply do Gate 2 itself and record a pass.
// Every gate after that would then be reviewing work shaped by the reasoning it
// was supposed to be independent of.
var storyArtifacts = map[string]string{
	"ANALYSIS.md": "researcher",
	"THREATS.md":  "researcher",
}

// artifactOwner reports which role owns the path, if it is one of a story's
// gate artifacts.
func artifactOwner(story, path string) (owner, name string, ok bool) {
	dir := storyDir(story) + "/"
	rest, inStory := strings.CutPrefix(path, dir)
	if !inStory || strings.Contains(rest, "/") {
		return "", "", false
	}
	owner, ok = storyArtifacts[rest]
	return owner, rest, ok
}

func storyDir(story string) string {
	if story == "" {
		return ".sdlc/stories"
	}
	return ".sdlc/stories/" + strings.TrimSpace(story)
}
