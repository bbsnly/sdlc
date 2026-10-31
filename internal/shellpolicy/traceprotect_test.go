package shellpolicy

// US-112 AC-17: a story's trace.jsonl is written by sdlc alone, from the shell
// as with the file tools, under a rule of its own. The evasions are the ones
// the gate record's rows already try (shellpolicy_test.go, glob_test.go,
// find_test.go, shared_test.go), with the file's name in place of
// gate-record.json, and THREATS' spellings of the name; reading the trace
// stays open to every agent.

import (
	"strings"
	"testing"
)

// traceRule is the trace's own rule, beside gate-record-is-written-by-the-tool
// (ANALYSIS R6).
const traceRule = "trace-is-written-by-the-tool"

// traceWrites is every row that writes, moves, truncates or deletes the trace
// named name, in the story A-1 unless the row says otherwise.
func traceWrites(name string) []string {
	upper := strings.ToUpper(name)
	stem, ext, _ := strings.Cut(name, ".")
	return []string{
		// The gate record's rows.
		"git rm .sdlc/stories/A-1/" + name,
		"cd .sdlc/stories/A-1 && echo {} > " + name,
		"awk '{print > FILENAME}' .sdlc/stories/A-1/" + name,
		"python3 -m json.tool /tmp/g.json .sdlc/stories/A-1/" + name,
		"echo pass >> .sdlc/stories/A-1/" + name,
		"rm .sdlc/Stories/A-1/" + name,
		"Set-Content -Path .sdlc/stories/A-1/" + name + " -Value '{}'",
		"rm /home/dev/repo/.sdlc/stories/B-2/" + name,
		"find . -name " + name + " -delete",
		"rm .sdlc/stories/A-1/" + name[:len(name)-1] + "?",
		// Quoting.
		"echo x > '.sdlc/stories/A-1/" + name + "'",
		`echo x > ".sdlc/stories/A-1/` + name + `"`,
		`echo x > .sdlc/stories/A-1/"` + stem + `".` + ext,
		`echo x > .sdlc/stories/A-1/` + stem[:2] + `\` + stem[2:] + "." + ext,
		// A case change, which macOS and Windows find the same file by.
		"echo x > .sdlc/stories/A-1/" + upper,
		"rm .SDLC/stories/A-1/" + name,
		// Globs and a brace list.
		"rm .sdlc/stories/*/" + stem + ".*",
		"rm .sdlc/stories/A-1/" + stem[:2] + "?" + stem[3:] + "." + ext,
		"rm .sdlc/stories/A-1/{" + stem + ",nothing}." + ext,
		// ./, a doubled separator, /./ and Windows separators. A `..` inside
		// the word is placed on the disk, which this package leaves to its
		// caller; the hook's tests try it (internal/hook/trace_test.go), and
		// TestAC17TheTraceIsFoundFromWhereTheCommandRuns the `..` that leaves
		// where the shell stands.
		"rm ./.sdlc/stories/A-1/" + name,
		"rm .sdlc//stories//A-1//" + name,
		"rm .sdlc/./stories/A-1/" + name,
		`del .sdlc\stories\A-1\` + name,
		// Redirection and tee.
		"echo x >| .sdlc/stories/A-1/" + name,
		"echo x &> .sdlc/stories/A-1/" + name,
		"echo x 1> .sdlc/stories/A-1/" + name,
		"tee .sdlc/stories/A-1/" + name + " < /dev/null",
		"echo x | tee -a .sdlc/stories/A-1/" + name,
		// Every other way to write it.
		"cp /tmp/x .sdlc/stories/A-1/" + name,
		"mv /tmp/x .sdlc/stories/A-1/" + name,
		"ln -sf /tmp/x .sdlc/stories/A-1/" + name,
		"install /tmp/x .sdlc/stories/A-1/" + name,
		"dd if=/dev/null of=.sdlc/stories/A-1/" + name,
		"truncate -s 0 .sdlc/stories/A-1/" + name,
		": > .sdlc/stories/A-1/" + name,
		"rm -f .sdlc/stories/A-1/" + name,
		"sed -i 's/a/b/' .sdlc/stories/A-1/" + name,
		"perl -pi -e 's/a/b/' .sdlc/stories/A-1/" + name,
		"Out-File -FilePath .sdlc/stories/A-1/" + name,
		"Add-Content -Path .sdlc/stories/A-1/" + name + " -Value x",
		"Remove-Item .sdlc/stories/A-1/" + name,
	}
}

// traceReads is every row that only reads it.
func traceReads(name string) []string {
	return []string{
		"cat .sdlc/stories/A-1/" + name,
		"tail -n 20 .sdlc/stories/A-1/" + name,
		"grep shell .sdlc/stories/A-1/" + name,
		"wc -l .sdlc/stories/A-1/" + name,
		"head -c 100 .sdlc/stories/B-2/" + name,
		"sdlc trace A-1",
		"sdlc trace --all --json",
	}
}

func TestAC17TheTraceCannotBeWrittenThroughTheShell(t *testing.T) {
	for _, command := range traceWrites("trace.jsonl") {
		f := refused(t, command, ready, traceRule)
		if !strings.Contains(f.Reason+" "+f.Route, "sdlc trace") {
			t.Errorf("AC-17 the refusal of %q does not say that `sdlc trace <ID>` reads the trace: %+v", command, f)
		}
	}
}

// AC-17: a relative path from where the Bash tool last stood.
func TestAC17TheTraceIsFoundFromWhereTheCommandRuns(t *testing.T) {
	inState := State{CommitReady: true, Dir: ".sdlc/state"}
	refused(t, "echo {} > ../stories/A-1/trace.jsonl", inState, traceRule)
	inStory := State{CommitReady: true, Dir: ".sdlc/stories/A-1"}
	refused(t, "rm trace.jsonl", inStory, traceRule)
	allowed(t, "cat trace.jsonl", inStory)
}

// AC-17 and THREATS T1: another checkout's copy, by its absolute path.
func TestAC17AnotherCheckoutsTraceIsRefusedToo(t *testing.T) {
	s := withShared(ready, sharedLandings)
	refused(t, "echo '{}' > /work/main/.sdlc/stories/A-1/trace.jsonl", s, traceRule)
	allowed(t, "cat /work/main/.sdlc/stories/A-1/trace.jsonl", s)
}

func TestAC17ReadingTheTraceStaysOpen(t *testing.T) {
	for _, command := range traceReads("trace.jsonl") {
		allowed(t, command, ready)
	}
	// A file of another name beside it is the story's, as before.
	allowed(t, "echo x > .sdlc/stories/A-1/notes.jsonl", ready)
	allowed(t, "echo x > .sdlc/stories/A-1/scratch/trace.jsonl.bak", ready)
}

// AC-17: the gate record keeps its own rule; the new one is the trace's alone.
func TestAC17TheGateRecordIsStillRefusedUnderItsOwnRule(t *testing.T) {
	refused(t, "echo pass >> .sdlc/stories/A-1/gate-record.json", ready, "loop-state-through-the-tool")
}
