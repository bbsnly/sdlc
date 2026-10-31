package policy

// US-112 AC-17: the file tools may not write, edit or replace any story's
// trace.jsonl, whoever asks, under the trace's own rule; reading it is never
// refused. The spellings are the gate record's rows in policy_test.go
// (TestAFinishedStorysLoopStateIsNotWritable) with the trace's name.

import (
	"strings"
	"testing"
)

func TestAC17NoAgentAndNotTheMainConversationWritesTheTraceWithAFileTool(t *testing.T) {
	agents := []string{
		"", "sdlc:researcher", "sdlc-implementer", "sdlc:sdet", "sdlc:verifier", "sdlc:bookkeeper",
		"sdlc:code-reviewer", "sdlc:architect", "sdlc:project-manager", "Explore",
	}
	for _, tool := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
		for _, agent := range agents {
			for _, story := range []string{"A-1", "OLD-9"} {
				v := Evaluate(Request{Tool: tool, Agent: agent, Path: ".sdlc/stories/" + story + "/trace.jsonl", Story: "A-1"})
				if v.Allowed {
					t.Errorf("AC-17 %s by %q wrote %s's trace", tool, agent, story)
					continue
				}
				if v.Rule != "trace-is-written-by-the-tool" {
					t.Errorf("AC-17 %s by %q of %s's trace was refused by %q, want trace-is-written-by-the-tool", tool, agent, story, v.Rule)
				}
			}
		}
	}
}

// AC-17: the refusal says that sdlc writes the trace and that `sdlc trace
// <ID>` reads it.
func TestAC17TheTraceRefusalSaysWhoWritesItAndHowToReadIt(t *testing.T) {
	v := Evaluate(Request{Tool: "Write", Agent: "sdlc:verifier", Path: ".sdlc/stories/A-1/trace.jsonl", Story: "A-1"})
	text := v.Reason + " " + v.Route
	if !strings.Contains(text, "sdlc trace") {
		t.Errorf("AC-17 the refusal does not name `sdlc trace <ID>`: %q", text)
	}
	if !strings.Contains(strings.ToLower(text), "written by sdlc") && !strings.Contains(strings.ToLower(text), "sdlc writes") {
		t.Errorf("AC-17 the refusal does not say that sdlc writes the trace: %q", text)
	}
}

// AC-17: spelled the ways macOS and Windows find the same file.
func TestAC17TheTraceIsRefusedBySpellingsThatFoldToIt(t *testing.T) {
	for _, path := range []string{
		".sdlc/stories/OLD-9/TRACE.JSONL",
		".sdlc/stories/OLD-9/Trace.Jsonl",
		".ſdlc/stories/OLD-9/trace.jsonl",
		".sdlc/ſtories/OLD-9/trace.jsonl",
		".sdlc/ﬆories/OLD-9/trace.jsonl",
		".sdlc/stories/OLD-9/trace.jsonl::$DATA",
	} {
		v := Evaluate(Request{Tool: "Write", Agent: "sdlc:implementer", Path: path, Story: "A-1"})
		if v.Rule != "trace-is-written-by-the-tool" {
			t.Errorf("AC-17 %s: refused by %q, want trace-is-written-by-the-tool", path, v.Rule)
		}
	}
}

// AC-17: reading stays allowed to every agent, and a file of the same name
// elsewhere is a different file.
func TestAC17ReadingTheTraceAndAFileOfItsNameElsewhereStayOpen(t *testing.T) {
	for _, tool := range []string{"Read", "Grep", "Glob"} {
		if v := Evaluate(Request{Tool: tool, Agent: "sdlc:verifier", Path: ".sdlc/stories/A-1/trace.jsonl", Story: "A-1"}); !v.Allowed {
			t.Errorf("AC-17 %s of the trace was refused by %s", tool, v.Rule)
		}
	}
	for _, path := range []string{".sdlc/stories/A-1/scratch/trace.jsonl", "testdata/trace.jsonl"} {
		if v := Evaluate(Request{Tool: "Write", Agent: "sdlc:sdet", Path: path, Story: "A-1", Tests: Tests{IsTest: true}}); v.Rule == "trace-is-written-by-the-tool" {
			t.Errorf("AC-17 %s is not a story's trace and was refused as one", path)
		}
	}
}
