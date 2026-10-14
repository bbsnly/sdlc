package cli

// US-092 in process, the cases the first set leaves to a person reading the
// plan: what the stop does when it replays more than once in a tier that
// waits (AC-2), what the commit gate does with a second pass that would leave
// the story with fewer commits held than before it (AC-1), the commits above
// the recorded range that are not the bookkeeper's (AC-1), what `sdlc approve`
// records and says (AC-1), that no refusal prints anything a session chose
// (AC-1), and the way out of a refusal when no iteration is running, followed
// as it is printed (AC-1).
//
// Each test is a story walked with the real commands. The one place a record
// is written by hand says so, and says which half of the rule it reaches.
//
// Helpers carry a cm prefix, for commit cover, more.

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// cmFlat is text with every run of white space made one space, so that a
// phrase is found whatever the line it was wrapped on.
func cmFlat(text string) string { return strings.Join(strings.Fields(text), " ") }

// cmMarker puts the file the story's configured command waits for in place:
// from then on the command passes.
func cmMarker(t *testing.T, marker string) {
	t.Helper()
	if err := os.WriteFile(marker, []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cmEvents are the events of one type in the story's record, oldest first.
func cmEvents(t *testing.T, main, kind string) []map[string]any {
	t.Helper()
	var out []map[string]any
	list, _ := ccRecordJSON(t, main)["events"].([]any)
	for _, e := range list {
		if m, ok := e.(map[string]any); ok && m["type"] == kind {
			out = append(out, m)
		}
	}
	return out
}

// cmTrunkMoves is another story landing on trunk: a file of its own.
func cmTrunkMoves(t *testing.T, main string) string {
	t.Helper()
	return swCommitFile(t, main, "MORE.md", "yet another story's work\n", "yet another story lands")
}

// cmNothingLanded holds a stop that was refused after the story had been put
// onto a trunk that moved: trunk is where the person's commit left it, the
// story is not done, and its worktree and claim are kept. The branch has
// been replayed by then, so unlike ccRefusedAndNothingLanded it is not held
// to where it was.
func cmNothingLanded(t *testing.T, s ccStory, trunk string) {
	t.Helper()
	if got := ldTrunk(t, s.main); got != trunk {
		t.Errorf("trunk is at %s after the refused stop, want %s", got, trunk)
	}
	lxNotDone(t, s.main)
	if !swListed(t, s.main, s.wt) {
		t.Error("the refused stop took the story's worktree away")
	}
}

// cmLastApprovalCommit is the commit the person's latest answer names, or "".
func cmLastApprovalCommit(t *testing.T, main string) string {
	t.Helper()
	list := ccApprovals(t, main)
	if len(list) == 0 {
		t.Fatal("the record holds no approval")
	}
	got, _ := list[len(list)-1]["commit"].(string)
	return got
}

// ---------------------------------------------------------------- AC-2

// AC-2: a stop cut short after its own replay, and then run again once trunk
// has moved a second time, replays again. Each commit keeps its message and its
// author through both replays, the record holds both replays, and nobody is
// asked: the approval count is what it was. Fails if the stop's own second
// replay is judged as a tip somebody changed.
func TestAC2ATrunkThatMovesAgainAfterACutShortReplayLandsWithMessagesAndAuthorsKept(t *testing.T) {
	t.Parallel()
	s, _, marker, before, approvals := ccCutShort(t, "Carol <carol@example.org>")
	again := cmTrunkMoves(t, s.main)
	cmMarker(t, marker)

	landed := otLands(t, s.main, s.wt, again)
	after := ccLog(t, s.main, again, landed.tip)
	if len(after) != len(before) {
		t.Fatalf("the story had %d commits and trunk has %d of them on top of %s", len(before), len(after), again[:12])
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("commit %d changed through two replays:\nwas  %q\nnow  %q", i+1, before[i], after[i])
		}
	}
	if got := len(cmEvents(t, s.main, eventReplayed)); got != 2 {
		t.Errorf("the record holds %d replays, want the stop's two", got)
	}
	if got := len(ccApprovals(t, s.main)); got != approvals {
		t.Errorf("the record has %d approvals after the landing, want the %d it had", got, approvals)
	}
}

// AC-2: the tip amended between the two replays is not the commit the first
// replay made, and the second stop is refused before it replays anything. Trunk
// has moved again, so Gate 8's own check of the approval may refuse first; what
// is held is that either way the story is refused with SDLC-E0037, nothing
// lands and the branch is left as the session left it.
func TestAC2ATipAmendedBetweenTwoReplaysIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		tamper func(t *testing.T, wt string)
	}{
		{"AC-2 the tip's message amended", func(t *testing.T, wt string) {
			t.Helper()
			ccAmend(t, wt, "-m", "feat: something else")
		}},
		{"AC-2 the tip's author amended", func(t *testing.T, wt string) {
			t.Helper()
			ccAmend(t, wt, "--no-edit", "--author", "Mallory <mallory@example.com>")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, marker, _, _ := ccCutShort(t, "")
			tc.tamper(t, s.wt)
			again := cmTrunkMoves(t, s.main)
			cmMarker(t, marker)

			ccRefusedAndNothingLanded(t, s)
			if got := ldTrunk(t, s.main); got != again {
				t.Errorf("trunk is at %s, want the commit the person made, %s", got, again)
			}
		})
	}
}

// AC-2: the record of an earlier replay is what the second replay is judged by,
// so an earlier record that was forged, cut short or taken out is a refusal and
// never a step the walk skips. Each is refused after the second replay, with
// trunk where the person's commit left it and nothing landed.
func TestAC2AForgedOrShortenedEarlierReplayedEventIsRefusedNeverSkipped(t *testing.T) {
	t.Parallel()
	// edit changes the first "replayed" event of doc, and says what it did to
	// the event's "from", the commit the first replay found the story at.
	for _, tc := range []struct {
		name string
		want string
		edit func(t *testing.T, doc map[string]any, cut string)
	}{
		{"AC-2 the first event names another commit as the tip it replaced", "one for one", func(t *testing.T, doc map[string]any, cut string) {
			t.Helper()
			cmEditFirstReplayed(t, doc, func(m map[string]any, from string) {
				msg, _ := m["message"].(string)
				m["message"] = strings.Replace(msg, from, cut, 1)
			})
		}},
		{"AC-2 the first event's tip is cut short to twelve characters", "no longer reads", func(t *testing.T, doc map[string]any, _ string) {
			t.Helper()
			cmEditFirstReplayed(t, doc, func(m map[string]any, from string) {
				msg, _ := m["message"].(string)
				m["message"] = strings.Replace(msg, from, from[:12], 1)
			})
		}},
		{"AC-2 the first event is not a line the stop wrote", "no longer reads", func(t *testing.T, doc map[string]any, _ string) {
			t.Helper()
			cmEditFirstReplayed(t, doc, func(m map[string]any, _ string) { m["message"] = "put onto trunk, somehow" })
		}},
		{"AC-2 the first event names a commit that git does not have", "no longer reads", func(t *testing.T, doc map[string]any, _ string) {
			t.Helper()
			// Well formed, and as good as a commit git has since removed: the
			// line reads, the commits it names do not.
			cmEditFirstReplayed(t, doc, func(m map[string]any, from string) {
				msg, _ := m["message"].(string)
				m["message"] = strings.Replace(msg, from, strings.Repeat("a", len(from)), 1)
			})
		}},
		{"AC-2 the first event is taken out of the record", "was before sdlc stop put it onto", func(t *testing.T, doc map[string]any, _ string) {
			t.Helper()
			events, _ := doc["events"].([]any)
			var kept []any
			dropped := false
			for _, e := range events {
				if m, ok := e.(map[string]any); ok && m["type"] == eventReplayed && !dropped {
					dropped = true
					continue
				}
				kept = append(kept, e)
			}
			if !dropped {
				t.Fatal("the record holds no replayed event to take out")
			}
			doc["events"] = kept
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, marker, _, _ := ccCutShort(t, "")
			doc := ccRecordJSON(t, s.main)
			tc.edit(t, doc, s.cut)
			ccSaveRecord(t, s.main, doc)
			again := cmTrunkMoves(t, s.main)
			cmMarker(t, marker)

			out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
			suSays(t, cmFlat(out), notApprovedAsCommitted("US-001"), tc.want)
			cmNothingLanded(t, s, again)
		})
	}
}

// cmEditFirstReplayed runs edit on the first "replayed" event of doc with the
// commit its message says the story was at, and fails where there is none.
func cmEditFirstReplayed(t *testing.T, doc map[string]any, edit func(m map[string]any, from string)) {
	t.Helper()
	events, _ := doc["events"].([]any)
	for _, e := range events {
		m, ok := e.(map[string]any)
		if !ok || m["type"] != eventReplayed {
			continue
		}
		msg, _ := m["message"].(string)
		rest := strings.TrimPrefix(msg, "Replayed US-001 from ")
		from, _, _ := strings.Cut(rest, " ")
		if len(from) < 40 {
			t.Fatalf("the replayed event says %q, which does not name the commit the story was at", msg)
		}
		edit(m, from)
		return
	}
	t.Fatal("the record holds no replayed event")
}

// AC-2 and the approval: a person who answers for the story after a replay cut
// short is answering for the commits the replay made, and the stop that
// replays again still lands them. The answer names the tip it was given
// for, and that approval is what the next replay's commits are held to.
func TestAC2AnAnswerGivenAfterACutShortReplayStillLandsAfterTheNextReplay(t *testing.T) {
	t.Parallel()
	s, _, marker, before, _ := ccCutShort(t, "Carol <carol@example.org>")
	replayed := swRev(t, s.wt, "HEAD")
	wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
	out := wtMust(t, s.wt, "", "", "approve", "US-001")
	said := cmFlat(out.stdout + out.stderr)
	suSays(t, said, "story/US-001 is at "+shortCommit(replayed))
	if strings.Contains(said, "They are not the commits") {
		t.Errorf("approve warns of commits that are the replay's own:\n%s", said)
	}
	if got := cmLastApprovalCommit(t, s.main); got != replayed {
		t.Errorf("the answer names %q, want the replayed tip %s", got, replayed)
	}
	again := cmTrunkMoves(t, s.main)
	cmMarker(t, marker)

	// The iteration ended with the escalation: the stop does not need one.
	landed := otLands(t, s.main, s.wt, again)
	after := ccLog(t, s.main, again, landed.tip)
	for i := range before {
		if i >= len(after) || before[i] != after[i] {
			t.Fatalf("commit %d changed through the replays: %q", i+1, after)
		}
	}
}

// ---------------------------------------------------------------- AC-1 at the commit gate

// AC-1, the rework round: the commit gate passes at C1, the work is reopened,
// a person answers for it and the answer names C1, the session commits C2 and
// the gate passes on that answer. An amend of C2 after that pass is a change
// of a commit nobody approved: the gate run again is refused and records
// nothing, and the stop is refused with it. Three commands must not land it.
func TestAC1AGateRunAgainAfterAReworkRoundRefusesAnAmendMadeAfterThePass(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	c1 := s.second
	wtMust(t, s.wt, ldSession, "", "gate", "implementation", "fail", "--note", "reopened for a rework")
	body := "package billing\n\n// Refund is what US-001 adds, reworked.\ntype Refund struct{}\n"
	writeFile(t, s.wt, "refund.go", body)
	swPass(t, s.wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, s.wt)
	if got := cmLastApprovalCommit(t, s.main); got != c1 {
		t.Fatalf("precondition: the answer given after the first pass names %q, want C1, %s", got, c1)
	}
	// The helper commits what the rework left uncommitted, and passes the gate.
	swPass(t, s.wt, ldSession, model.GateCommit, "")
	c2 := swRev(t, s.wt, "HEAD")
	if c2 == c1 {
		t.Fatal("precondition: the rework made no commit")
	}
	if got := swRecord(t, s.main, "US-001").Commit; got != c2 {
		t.Fatalf("precondition: the gate passed on the answer and records %q, want C2, %s", got, c2)
	}

	ccAmend(t, s.wt, "-m", "feat: refunds, reworded after the pass")
	out := swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again after the amend")
	suSays(t, cmFlat(out), notApprovedAsCommitted("US-001"), "nothing was recorded")
	if got := swRecord(t, s.main, "US-001").Commit; got != c2 {
		t.Errorf("the refused gate changed the commit the record holds from %s to %s", c2, got)
	}
	stop := ccRefusedAndNothingLanded(t, s)
	suSays(t, cmFlat(stop), "is not in the history of")
}

// AC-1: a person's answer covers one commit-gate pass. After a pass that stood
// on it, a second pass with other commits needs another answer, whatever the
// gate went through in between: here the gate is recorded as failed, which
// empties the commit the record holds, and the amend after it is still a
// change. Passing it again with nothing amended needs nobody.
func TestAC1AnAnswerCoversOneCommitGatePassHoweverTheGateWasReopened(t *testing.T) {
	t.Parallel()
	// The answer names the commit it was given for, which a commit added on
	// top of it still holds: only that the answer is older than the pass that
	// stood on it keeps it from covering this pass too.
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, wt string)
	}{
		{"AC-1 the pass that stood on the answer is recorded as failed and the tip amended", func(t *testing.T, wt string) {
			t.Helper()
			ccAmend(t, wt, "-m", "feat: something else again")
		}},
		{"AC-1 the pass that stood on the answer is recorded as failed and a commit added on top", func(t *testing.T, wt string) {
			t.Helper()
			ccGit(t, wt, "commit", "--quiet", "--allow-empty", "-m", "chore: nothing")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ccFinished(t)
			ccAmend(t, s.wt, "-m", "feat: something else")
			amended := swRev(t, s.wt, "HEAD")
			swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again")
			ccHandedOverAndApproved(t, s.wt)
			wtMust(t, s.wt, ldSession, "", "gate", "commit", "pass", "--story", "US-001", "--note", "on the answer")
			if got := swRecord(t, s.main, "US-001").Commit; got != amended {
				t.Fatalf("precondition: the pass on the answer records %q, want %s", got, amended)
			}
			recorded := len(cmEvents(t, s.main, eventCommitRecorded))

			wtMust(t, s.wt, ldSession, "", "gate", "commit", "fail", "--story", "US-001", "--note", "reopened")
			tc.change(t, s.wt)
			swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--story", "US-001", "--note", "on the old answer")

			r := swRecord(t, s.main, "US-001")
			if r.Commit != "" {
				t.Errorf("the refused gate recorded the commit %s", r.Commit)
			}
			if got := len(cmEvents(t, s.main, eventCommitRecorded)); got != recorded {
				t.Errorf("the refused gate left %d commit_recorded events, was %d", got, recorded)
			}
		})
	}
	t.Run("AC-1 the gate recorded as failed and passed again with nothing amended passes without a person", func(t *testing.T) {
		s := ccFinished(t)
		wtMust(t, s.wt, ldSession, "", "gate", "commit", "fail", "--note", "reopened")
		wtMust(t, s.wt, ldSession, "", "gate", "commit", "pass", "--note", "nothing changed")
		if got := swRecord(t, s.main, "US-001").Commit; got != s.second {
			t.Errorf("the gate passed again records %q, want the same commit, %s", got, s.second)
		}
	})
}

// AC-1 (V4): a rejection is the latest answer, and an answer that did not
// approve clears nothing. The stop is refused for a story a person sent back,
// as it was before this story (SDLC-E0033); the commit gate run again is
// refused with SDLC-E0037; and a commit named by an approval before the
// rejection is not read: approved and then sent back is sent back.
func TestAC1ARejectionAfterTheRefusalLeavesTheStopAndTheGateRefused(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 a rejection and nothing before it", func(t *testing.T) {
		s := ccFinished(t)
		ccAmend(t, s.wt, "-m", "feat: something else")
		ccRefusedAndNothingLanded(t, s)
		wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
		wtMust(t, s.wt, "", "", "approve", "US-001", "--reject", "the message is wrong")

		trunk := ldTrunk(t, s.main)
		swRefused(t, "SDLC-E0033", s.wt, ldSession, "stop")
		if got := ldTrunk(t, s.main); got != trunk {
			t.Errorf("trunk moved from %s to %s on a story a person sent back", trunk, got)
		}
		lxNotDone(t, s.main)
		swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--story", "US-001", "--note", "after a rejection")
	})
	t.Run("AC-1 an approval and then a rejection", func(t *testing.T) {
		s := ccFinished(t)
		ccAmend(t, s.wt, "-m", "feat: something else")
		ccRefusedAndNothingLanded(t, s)
		ccHandedOverAndApproved(t, s.wt)
		// The commit gate is recorded as failed so that the session can take
		// the story up again and hand it over once more.
		wtMust(t, s.wt, ldSession, "", "gate", "commit", "fail", "--story", "US-001", "--note", "asking again")
		swStarted(t, s.wt, ldSession)
		wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
		wtMust(t, s.wt, "", "", "approve", "US-001", "--reject", "on second thoughts, no")

		swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--story", "US-001", "--note", "after a rejection")
		if r := swRecord(t, s.main, "US-001"); r.Commit != "" {
			t.Errorf("the refused gate recorded the commit %s", r.Commit)
		}
	})
}

// AC-1: an answer names the commit it was given for, and one more amend after
// it is a change of a commit that answer did not see. The stop and the gate
// are both refused.
func TestAC1AnAmendAfterThePersonsAnswerIsRefusedAtTheStopAndAtTheGate(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	ccAmend(t, s.wt, "-m", "feat: something else")
	ccRefusedAndNothingLanded(t, s)
	ccHandedOverAndApproved(t, s.wt)
	ccAmend(t, s.wt, "-m", "feat: something else, once more")

	ccRefusedWhileHandedOver(t, s)
	swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--story", "US-001", "--note", "after the answer")
}

// AC-1: a commit that git cannot read is not evidence that the story still
// holds it. Forty zeros as the commit the gate recorded, and as the commit a
// person's answer names, are each a refusal at the stop and never a pass.
func TestAC1AUnreadableRecordedCommitIsRefusedNotWavedThrough(t *testing.T) {
	t.Parallel()
	zeros := strings.Repeat("0", 40)
	t.Run("AC-1 forty zeros as the recorded commit", func(t *testing.T) {
		s := ccFinished(t)
		doc := ccRecordJSON(t, s.main)
		doc["commit"] = zeros
		ccSaveRecord(t, s.main, doc)

		out := ccRefusedAndNothingLanded(t, s)
		suSays(t, cmFlat(out), notApprovedAsCommitted("US-001"))
	})
	t.Run("AC-1 forty zeros as the commit a person's answer names", func(t *testing.T) {
		s := ccFinished(t)
		ccAmend(t, s.wt, "-m", "feat: something else")
		ccRefusedAndNothingLanded(t, s)
		ccHandedOverAndApproved(t, s.wt)
		doc := ccRecordJSON(t, s.main)
		list, _ := doc["approvals"].([]any)
		last, _ := list[len(list)-1].(map[string]any)
		last["commit"] = zeros
		ccSaveRecord(t, s.main, doc)

		ccRefusedWhileHandedOver(t, s)
		swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--story", "US-001", "--note", "on a commit that is no commit")
	})
}

// AC-1 (architect B2, red-team H2): a commit added on top of the recorded range
// after the gate passed, and the gate run again, is not a way to have it held
// as the story's commits. The gate is refused, as the stop would be, and
// nothing is recorded. An empty commit, one by another author, and a merge
// of a side branch are each a commit nobody read.
func TestAC1ACommitAddedOnTopAfterTheGatePassedIsRefusedWhenTheGateIsRunAgain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		add  func(t *testing.T, wt string)
	}{
		{"AC-1 an empty commit", func(t *testing.T, wt string) {
			t.Helper()
			ccGit(t, wt, "commit", "--quiet", "--allow-empty", "-m", "chore: nothing")
		}},
		{"AC-1 an empty commit by another author", func(t *testing.T, wt string) {
			t.Helper()
			ccGit(t, wt, "commit", "--quiet", "--allow-empty", "--author", "Mallory <mallory@example.com>", "-m", "chore: nothing")
		}},
		{"AC-1 a merge of a side branch with no change", func(t *testing.T, wt string) {
			t.Helper()
			ccGit(t, wt, "switch", "--quiet", "-c", "side")
			ccGit(t, wt, "commit", "--quiet", "--allow-empty", "-m", "chore: aside")
			ccGit(t, wt, "switch", "--quiet", "story/US-001")
			ccGit(t, wt, "merge", "--quiet", "--no-ff", "-m", "chore: merge aside", "side")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ccFinished(t)
			tc.add(t, s.wt)

			out := swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again, with a commit on top")
			suSays(t, cmFlat(out), notApprovedAsCommitted("US-001"), "nothing was recorded")
			if got := swRecord(t, s.main, "US-001").Commit; got != s.second {
				t.Errorf("the refused gate changed the commit the record holds from %s to %s", s.second, got)
			}
		})
	}
}

// cmNoRange leaves the watched story with no single range: the hook is
// recorded as having seen the story's session commit on a head that is the
// parent of none of the story's commits, so that the second pass finds
// nothing to place. It is what an unwatched commit beside a watched one comes
// to, in the one way a test can put it without the hook running.
func cmNoRange(t *testing.T, s ccStory) {
	t.Helper()
	watched(t, s.wt, s.second)
}

// AC-1: a commit-gate pass that would leave the story with no recorded commit,
// where it had one, takes the hold off it, so it is refused and records
// nothing. The gate had recorded the range ending at the story's tip; the
// second pass finds no range, and no answer names a commit for the stop to
// hold the tip to instead.
func TestAC1APassThatLeavesNoRecordedCommitWhereOneWasKnownIsRefusedAndRecordsNothing(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	cmNoRange(t, s)
	events := len(cmEvents(t, s.main, eventCommitRecorded))

	out := swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again, with no range to find")
	suSays(t, cmFlat(out), "would no longer be held to its commits", "nothing was recorded")
	r := swRecord(t, s.main, "US-001")
	if r.Commit != s.second || r.RangeUnknown {
		t.Errorf("the refused pass changed the record: commit %q, range unknown %v; want %s and a range", r.Commit, r.RangeUnknown, s.second)
	}
	if got := len(cmEvents(t, s.main, eventCommitRecorded)); got != events {
		t.Errorf("the refused pass left %d commit_recorded events, was %d", got, events)
	}
	ldLanded(t, s.main, s.wt, s.second)
}

// AC-1: the same pass, with a person's answer that names the story's tip,
// stands: the record then names no commit, and the stop holds the tip to the
// answer's. The tip as the answer found it lands; the tip amended after it is
// refused, and the refusal says the gate recorded no range.
func TestAC1APassThatLeavesNoRangeStandsOnAPersonsAnswerAndTheStopHoldsTheTipToIt(t *testing.T) {
	t.Parallel()
	pass := func(t *testing.T) ccStory {
		t.Helper()
		s := ccFinished(t)
		cmNoRange(t, s)
		swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again, with no range to find")
		ccHandedOverAndApproved(t, s.wt)
		wtMust(t, s.wt, ldSession, "", "gate", "commit", "pass", "--story", "US-001", "--note", "on the answer")
		wtMust(t, s.wt, ldSession, "", "gate", "retro", "pass", "--story", "US-001", "--note", "the retro again")
		r := swRecord(t, s.main, "US-001")
		if r.Commit != "" || !r.RangeUnknown {
			t.Fatalf("precondition: the pass on the answer records commit %q, range unknown %v; want no commit and an unknown range", r.Commit, r.RangeUnknown)
		}
		return s
	}
	t.Run("AC-1 the tip the answer was given for lands", func(t *testing.T) {
		s := pass(t)
		ldLanded(t, s.main, s.wt, s.second)
	})
	t.Run("AC-1 the tip amended after the answer is refused", func(t *testing.T) {
		s := pass(t)
		ccAmend(t, s.wt, "-m", "feat: something else")
		out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
		suSays(t, cmFlat(out), notApprovedAsCommitted("US-001"), "recorded no range")
		if got := ldTrunk(t, s.main); got == swRev(t, s.wt, "HEAD") {
			t.Error("the amended tip landed")
		}
		lxNotDone(t, s.main)
	})
}

// AC-1, the stop's half only: a record that says the commit gate's last pass
// placed no range, whose history shows an earlier pass did record one, and
// whose latest answer names no commit, has lost the hold the story was under.
// No command of this binary leaves a record so, since the gate refuses the
// pass that would, so the record is written by hand: the commit removed and
// the range marked unknown. The stop refuses it and says what is missing.
func TestAC1ARecordThatLostItsRangeAfterOneWasRecordedIsRefusedAtTheStop(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	doc := ccRecordJSON(t, s.main)
	delete(doc, "commit")
	doc["range_unknown"] = true
	ccSaveRecord(t, s.main, doc)

	out := ccRefusedAndNothingLanded(t, s)
	suSays(t, cmFlat(out), notApprovedAsCommitted("US-001"), "recorded no range")
}

// ---------------------------------------------------------------- AC-1 above the range

// AC-1: a commit above the recorded range is the bookkeeper's only if it
// changes the root CODEMAP.md alone and leaves it a plain file. Each of these
// is a commit of the story that nobody saw, though the net change is the one
// reviewed or there is none: it is refused at the stop, with nothing landed,
// and the refusal says what is wrong with the commit.
func TestAC1ACommitAboveTheRangeThatIsNotTheBookkeepersIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		want  string
		above func(t *testing.T, wt string)
	}{
		{"AC-1 an empty commit", "changes no path", func(t *testing.T, wt string) {
			t.Helper()
			ccGit(t, wt, "commit", "--quiet", "--allow-empty", "-m", "chore: nothing")
		}},
		{"AC-1 a CODEMAP.md in a directory, added and taken away", "changes one path that is not the root CODEMAP.md", func(t *testing.T, wt string) {
			t.Helper()
			swCommitFile(t, wt, "docs/CODEMAP.md", "# map\n", "docs: a map")
			ccGit(t, wt, "rm", "--quiet", "--", "docs/CODEMAP.md")
			ccGit(t, wt, "commit", "--quiet", "-m", "docs: no map")
		}},
		{"AC-1 CODEMAP.md made a link in one commit and a file again in the next", "something other than a plain file", func(t *testing.T, wt string) {
			t.Helper()
			// The link is put into the index by hand: a link in the working
			// tree is a file where the platform has none.
			blob := strings.TrimSpace(cmHashObject(t, wt, "refund.go"))
			ccGit(t, wt, "update-index", "--add", "--cacheinfo", "120000,"+blob+",CODEMAP.md")
			ccGit(t, wt, "commit", "--quiet", "-m", "docs: a link")
			swCommitFile(t, wt, "CODEMAP.md", "# map\n", "docs: a map again")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ccFinished(t)
			tc.above(t, s.wt)

			out := ccRefusedAndNothingLanded(t, s)
			suSays(t, cmFlat(out), notApprovedAsCommitted("US-001"), "which is above", tc.want)
		})
	}
}

// AC-1: a commit above the recorded range that adds a codemap.md, the bookkeeper's
// file in the wrong case, is refused too. The landing's own check of the root
// CODEMAP.md refuses it first, with SDLC-E0055, so the story is refused either
// way and nothing lands; which rule says so is not what is held.
func TestAC1ACommitAboveTheRangeThatAddsACodemapInTheOtherCaseIsRefused(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	// Put into the index by hand with the case kept: on a disk that folds case
	// the file would be the CODEMAP.md already there.
	blob := strings.TrimSpace(cmHashObject(t, s.wt, "refund.go"))
	ccGit(t, s.wt, "-c", "core.ignorecase=false", "update-index", "--add", "--cacheinfo", "100644,"+blob+",codemap.md")
	ccGit(t, s.wt, "-c", "core.ignorecase=false", "commit", "--quiet", "-m", "docs: a map")
	trunk := ldTrunk(t, s.main)

	r := wtRun(t, s.wt, ldSession, "", "stop")
	said := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(said, ldNotApproved) && !strings.Contains(said, ldCheckFailed) {
		t.Fatalf("sdlc stop exited %d where it should have been refused with %s or %s:\n%s", r.code, ldNotApproved, ldCheckFailed, said)
	}
	if got := ldTrunk(t, s.main); got != trunk {
		t.Errorf("trunk moved from %s to %s", trunk, got)
	}
	ldStillHeld(t, s.main, s.wt)
}

// cmHashObject writes the file into the object store and returns its name.
func cmHashObject(t *testing.T, dir, file string) string {
	t.Helper()
	return swGit(t, dir, "hash-object", "-w", "--", file)
}

// ---------------------------------------------------------------- AC-1 sdlc approve

// AC-1: an approving answer in a tier that waits is for the story's commits
// as they stand: it records the tip of the story's branch and prints it. Where
// the tip is not the commits the story was held to, it says so and says how
// to go back; where it is, it says nothing of the kind.
func TestAC1ApproveNamesTheTipOfTheStorysBranchAndPrintsItInATierThatWaits(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 the commits as the gate recorded them", func(t *testing.T) {
		s := ccFinished(t)
		wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
		r := wtMust(t, s.wt, "", "", "approve", "US-001")
		said := cmFlat(r.stdout + r.stderr)
		suSays(t, said, "story/US-001 is at "+shortCommit(s.second))
		if strings.Contains(said, "They are not the commits") {
			t.Errorf("approve warns of commits that are the recorded ones:\n%s", said)
		}
		if got := cmLastApprovalCommit(t, s.main); got != s.second {
			t.Errorf("the answer names %q, want the tip, %s", got, s.second)
		}
	})
	t.Run("AC-1 commits changed since the gate passed", func(t *testing.T) {
		s := ccFinished(t)
		ccAmend(t, s.wt, "-m", "feat: something else")
		tip := swRev(t, s.wt, "HEAD")
		wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
		r := wtMust(t, s.wt, "", "", "approve", "US-001")
		said := cmFlat(r.stdout + r.stderr)
		suSays(t, said, "story/US-001 is at "+shortCommit(tip), "They are not the commits",
			"git --no-replace-objects log --no-mailmap --format=fuller main..story/US-001",
			"git reset --hard "+shortCommit(s.second))
		if got := cmLastApprovalCommit(t, s.main); got != tip {
			t.Errorf("the answer names %q, want the tip it was shown, %s", got, tip)
		}
	})
	t.Run("AC-1 with --json the answer carries the commit", func(t *testing.T) {
		s := ccFinished(t)
		wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
		r := wtMust(t, s.wt, "", "", "approve", "US-001", "--json")
		var got map[string]any
		if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
			t.Fatalf("approve --json printed %q: %v", r.stdout, err)
		}
		if got["commit"] != s.second {
			t.Errorf("approve --json says commit %v, want the tip, %s", got["commit"], s.second)
		}
	})
}

// AC-1: the answer names the story's commits only where the worktree is on the
// story's branch. With HEAD detached below them it is not what the stop lands,
// and an answer that named it would hold the story to commits nobody was
// shown. The answer then records no commit and says so; a session that moved
// HEAD, amended the tip after switching back and ran the gate again is refused
// all the same.
func TestAC1ApproveWithTheWorktreeDetachedBelowTheStorysCommitsRecordsNoCommit(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	ccGit(t, s.wt, "switch", "--quiet", "--detach", s.first)
	wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
	r := wtMust(t, s.wt, "", "", "approve", "US-001")
	said := cmFlat(r.stdout + r.stderr)
	suSays(t, said, "names no commit of the story", "is not on story/US-001", "files only")
	if got := cmLastApprovalCommit(t, s.main); got != "" {
		t.Errorf("an answer given with HEAD detached names the commit %q", got)
	}

	ccGit(t, s.wt, "switch", "--quiet", "story/US-001")
	ccAmend(t, s.wt, "-m", "feat: something else")
	swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--story", "US-001", "--note", "on an answer given elsewhere")
}

// AC-1 non-goal: in a tier that does not wait for a person nothing holds the
// story's commits, and approve says nothing about them: the lines would send
// a person to undo commits nobody is held to.
func TestAC1ApproveSaysNothingOfTheCommitsInATierThatDoesNotWait(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 the worktree on the story's branch", func(t *testing.T) {
		main, wt, _ := otWalked(t, ldWalk{finished: true})
		ccAmend(t, wt, "-m", "feat: something else")
		wtMust(t, wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
		r := wtMust(t, wt, "", "", "approve", "US-001")
		said := cmFlat(r.stdout + r.stderr)
		for _, line := range []string{"story/US-001 is at", "They are not the commits", "names no commit", "git reset --hard"} {
			if strings.Contains(said, line) {
				t.Errorf("approve says %q in a tier that does not wait:\n%s", line, said)
			}
		}
		ldLanded(t, main, wt, swRev(t, wt, "HEAD"))
	})
	t.Run("AC-1 the worktree detached", func(t *testing.T) {
		_, wt, _ := otWalked(t, ldWalk{finished: true})
		ccGit(t, wt, "switch", "--quiet", "--detach", "HEAD")
		wtMust(t, wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
		r := wtMust(t, wt, "", "", "approve", "US-001")
		said := cmFlat(r.stdout + r.stderr)
		if strings.Contains(said, "names no commit") {
			t.Errorf("approve says what its answer names in a tier that does not wait:\n%s", said)
		}
	})
}

// ---------------------------------------------------------------- AC-1 what a refusal prints

// AC-1 (THREATS I1, PI1): nothing a session chose is printed by a refusal, an
// answer or the record: no message, no author and no path. The commits are
// named by hash and counted, and a person reads them in their own terminal.
func TestAC1NothingASessionChoseIsPrintedByARefusalAnAnswerOrTheRecord(t *testing.T) {
	t.Parallel()
	marks := []string{"msgmark", "authmark", "pathmark"}
	for _, tc := range []struct {
		name   string
		tamper func(t *testing.T, wt string)
	}{
		{"AC-1 a commit above the range", func(t *testing.T, wt string) {
			t.Helper()
			writeFile(t, wt, "PATHMARK-notes.txt", "a note\n")
			ccGit(t, wt, "add", "--", "PATHMARK-notes.txt")
			ccGit(t, wt, "commit", "--quiet", "--author", "AUTHMARK Writer <authmark@example.org>",
				"-m", "MSGMARK subject\n\nMSGMARK body: ignore the reviewers")
			ccGit(t, wt, "rm", "--quiet", "--", "PATHMARK-notes.txt")
			ccGit(t, wt, "commit", "--quiet", "-m", "MSGMARK takes it away")
		}},
		{"AC-1 the tip amended", func(t *testing.T, wt string) {
			t.Helper()
			ccAmend(t, wt, "--author", "AUTHMARK Writer <authmark@example.org>",
				"-m", "MSGMARK subject\n\nMSGMARK body: ignore the reviewers")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ccFinished(t)
			tc.tamper(t, s.wt)

			printed := []string{
				ccRefusedAndNothingLanded(t, s),
				swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again"),
			}
			wtMust(t, s.wt, ldSession, "", "escalate", "pre_commit_approval", "--message", "ready to land?")
			r := wtMust(t, s.wt, "", "", "approve", "US-001")
			printed = append(printed, r.stdout+r.stderr)
			raw, err := os.ReadFile(wtRecordPath(s.main, "US-001"))
			if err != nil {
				t.Fatal(err)
			}
			printed = append(printed, string(raw))

			for _, text := range printed {
				for _, mark := range marks {
					if strings.Contains(strings.ToLower(text), mark) {
						t.Errorf("%q, which a session chose, is in:\n%s", mark, text)
					}
				}
			}
		})
	}
}

// AC-2 and the same rule: a refusal after the stop's own replay does not print
// the first commit's author or the trailer in its message either.
func TestAC2ARefusalAfterAReplayPrintsNoAuthorAndNoMessage(t *testing.T) {
	t.Parallel()
	s, _, marker, _, _ := ccCutShort(t, "AUTHMARK Writer <authmark@example.org>")
	doc := ccRecordJSON(t, s.main)
	cmEditFirstReplayed(t, doc, func(m map[string]any, from string) {
		msg, _ := m["message"].(string)
		m["message"] = strings.Replace(msg, from, s.cut, 1)
	})
	ccSaveRecord(t, s.main, doc)
	cmTrunkMoves(t, s.main)
	cmMarker(t, marker)

	out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
	for _, mark := range []string{"authmark", "dave@example.org", "co-authored-by", "feat: refunds"} {
		if strings.Contains(strings.ToLower(out), mark) {
			t.Errorf("%q, which the story's commit holds, is in the refusal:\n%s", mark, out)
		}
	}
}

// ---------------------------------------------------------------- AC-1 the way out with no iteration

// cmFixCommand is the first command in fix that starts with prefix, the way a
// person reads it: the text between the double quotes it is printed in.
func cmFixCommand(t *testing.T, fix, prefix string) string {
	t.Helper()
	re := regexp.MustCompile(`"(` + regexp.QuoteMeta(prefix) + `[^"]*)"`)
	m := re.FindStringSubmatch(fix)
	if m == nil {
		t.Fatalf("the fix does not print a command that starts %q:\n%s", prefix, fix)
	}
	return m[1]
}

// cmHandedOverThenChanged is a finished story that a person has answered, with
// the iteration ended by the escalation, and whose tip is then amended: the
// state in which the stop's refusal has to say how to get out without an
// iteration running.
func cmHandedOverThenChanged(t *testing.T) ccStory {
	t.Helper()
	s := ccFinished(t)
	ccHandedOverAndApproved(t, s.wt)
	ccAmend(t, s.wt, "-m", "feat: something else")
	return s
}

// AC-1: the refusal with no iteration running prints a way out that works as
// printed. The first is the reset: the printed `git reset --hard` puts the story
// back on the commit the gate recorded, and the stop lands it.
func TestAC1TheResetThatARefusalPrintsWithNoIterationRunningLandsTheStory(t *testing.T) {
	t.Parallel()
	s := cmHandedOverThenChanged(t)

	out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	fix = cmFlat(fix)
	suSays(t, fix, "sdlc gate commit fail --story US-001", "/sdlc:next", `"sdlc stop"`, worktreeIn("US-001"))
	reset := cmFixCommand(t, fix, "git reset --hard ")
	if want := "git reset --hard " + shortCommit(s.second); reset != want {
		t.Fatalf("the fix prints %q, want the commit the gate recorded: %q", reset, want)
	}
	ccGit(t, s.wt, "reset", "--quiet", "--hard", strings.TrimPrefix(reset, "git reset --hard "))
	ldLanded(t, s.main, s.wt, s.second)
}

// AC-1: the other way out. The fix names `sdlc gate commit fail --story`, which
// the test runs as printed, and /sdlc:next, which takes the story up again at
// the commit gate. There the gate refuses, the story is handed over, a person
// answers, the gate passes on the answer, the retro is recorded again and the
// stop lands the story as it now is.
func TestAC1TheCommitGateFailThatARefusalPrintsWithNoIterationRunningGetsToALanding(t *testing.T) {
	t.Parallel()
	s := cmHandedOverThenChanged(t)
	amended := swRev(t, s.wt, "HEAD")

	out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	fix = cmFlat(fix)
	suSays(t, fix, "/sdlc:next", `"sdlc escalate pre_commit_approval --message ..."`, worktreeIn("US-001"))
	printed := cmFixCommand(t, fix, "sdlc gate commit fail --story US-001 --note ")
	// The note is printed in single quotes: one argument.
	note := strings.Trim(strings.TrimPrefix(printed, "sdlc gate commit fail --story US-001 --note "), "'")
	if note == "" {
		t.Fatalf("the fix prints a gate command with no note: %q", printed)
	}
	wtMust(t, s.wt, ldSession, "", "gate", "commit", "fail", "--story", "US-001", "--note", note)

	if got := swStarted(t, s.wt, ldSession); got.Story != "US-001" || !got.Resume || got.NextGate != string(model.GateCommit) {
		t.Fatalf("/sdlc:next gave %+v, want US-001 taken up again at the commit gate", got)
	}
	gate := swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again")
	_, gateFix := ldRefusal(t, gate, ldNotApproved)
	suSays(t, cmFlat(gateFix), `"sdlc escalate pre_commit_approval --message ..."`)
	ccHandedOverAndApproved(t, s.wt)
	wtMust(t, s.wt, ldSession, "", "gate", "commit", "pass", "--story", "US-001", "--note", "approved again")
	wtMust(t, s.wt, ldSession, "", "gate", "retro", "pass", "--story", "US-001", "--note", "the retro again")
	ldLanded(t, s.main, s.wt, amended)
}

// cmReviewAgain is ldReviewAgain for a story with no iteration running: the
// reviews are recorded against the story by name.
func cmReviewAgain(t *testing.T, wt string) {
	t.Helper()
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if r.WhenDoRAdvocate {
				continue
			}
			wtMust(t, wt, ldSession, "# "+r.Role+", again\n", "review", "add", string(gate), r.Role, "approve", "--story", "US-001")
		}
	}
}

// AC-1 and the way out of Gate 8's own refusal: where the work changed after a
// person's answer and no iteration is running, the fix says how a finished story
// gets back to a person. `sdlc escalate` has no story to hand over, so the
// fix names the commit gate recorded as failed, and /sdlc:next; followed as
// printed, the story is handed over, answered and lands as it now is.
func TestAC1TheApprovalRefusalWithNoIterationRunningSaysHowTheStoryGetsToAPerson(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	ccHandedOverAndApproved(t, s.wt)
	lxChangeAfterTheReviews(t, s.wt)
	ldBookkeeping(t, s.wt)
	moved := ldMoveTrunk(t, s.main)
	cmReviewAgain(t, s.wt)

	out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	fix = cmFlat(fix)
	suSays(t, fix, "no iteration is running on US-001", "/sdlc:next", worktreeIn("US-001"))
	printed := cmFixCommand(t, fix, "sdlc gate commit fail --story US-001 --note ")
	note := strings.Trim(strings.TrimPrefix(printed, "sdlc gate commit fail --story US-001 --note "), "'")
	wtMust(t, s.wt, ldSession, "", "gate", "commit", "fail", "--story", "US-001", "--note", note)
	if got := swStarted(t, s.wt, ldSession); got.Story != "US-001" || !got.Resume || got.NextGate != string(model.GateCommit) {
		t.Fatalf("/sdlc:next gave %+v, want US-001 taken up again at the commit gate", got)
	}
	swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "the work as it now is")
	ccHandedOverAndApproved(t, s.wt)
	wtMust(t, s.wt, ldSession, "", "gate", "commit", "pass", "--story", "US-001", "--note", "approved as it now is")
	wtMust(t, s.wt, ldSession, "", "gate", "retro", "pass", "--story", "US-001", "--note", "the retro again")
	otLands(t, s.main, s.wt, moved)
}

// AC-1 (V4): the commit a story is held to by a person's answer is the commit
// of the latest answer, and only where that answer approved. A rejection names
// no commit to hold anything to, however many an earlier approval named: no
// command reaches the difference, since the stop and the gate both refuse a
// rejected story for their own reasons first, so the rule is held on a record.
func TestAC1AnAnswerThatDidNotApproveNamesNoCommitToHoldTheStoryTo(t *testing.T) {
	t.Parallel()
	named := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name      string
		approvals []model.Approval
		want      string
	}{
		{"AC-1 no answer", nil, ""},
		{"AC-1 an approval that names a commit", []model.Approval{
			{Decision: model.DecisionApproved, Commit: named},
		}, named},
		{"AC-1 an approval that names none", []model.Approval{
			{Decision: model.DecisionApproved},
		}, ""},
		{"AC-1 an approval and then a rejection", []model.Approval{
			{Decision: model.DecisionApproved, Commit: named},
			{Decision: model.DecisionRejected, Commit: strings.Repeat("b", 40)},
		}, ""},
		{"AC-1 a rejection and then an approval that names none", []model.Approval{
			{Decision: model.DecisionRejected, Commit: named},
			{Decision: model.DecisionApproved},
		}, ""},
		{"AC-1 an approval that names a commit after one that names another", []model.Approval{
			{Decision: model.DecisionApproved, Commit: named},
			{Decision: model.DecisionApproved, Commit: strings.Repeat("b", 40)},
		}, strings.Repeat("b", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &model.Record{Approvals: tc.approvals}
			if got := approvedCommit(record); got != tc.want {
				t.Errorf("the commit the latest answer holds the story to is %q, want %q", got, tc.want)
			}
		})
	}
}
