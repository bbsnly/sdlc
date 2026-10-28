package cli

// The order the stop checks a story in: the freeze and the conflict markers
// are looked at before the reviews are asked whether they are of this change
// (requireIntactFreeze and markerCheck before changeChecks, landing.go). Many
// tests of those refusals leave their reviews stale on purpose, because
// recording them again is about twenty-five git processes the refusal never
// reads. What keeps that honest is here: the same story, with its reviews
// stale and nothing else wrong, is refused for the reviews (SDLC-E0029), and
// the moment it also has the earlier problem the stop says that one instead.
// A check moved after the reviews fails the row for its code, and every test
// that leaves its reviews stale with it.

import (
	"testing"
)

func TestAnEarlierRefusalWinsOverReviewsThatAreStale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code string
		commit     func(t *testing.T, wt string)
	}{
		{"E0025 a frozen test the story changed", ldFrozenMoved, func(t *testing.T, wt string) {
			t.Helper()
			swCommitFile(t, wt, otTaxTest, "package internal\n\n// changed by the story\n", "the story's own change to a frozen test")
		}},
		{"E0043 a test file the story added after the freeze", ldUnfrozenTest, func(t *testing.T, wt string) {
			t.Helper()
			swCommitFile(t, wt, otLaterTest, otOwn, "a test written after the freeze")
		}},
		{"conflict markers the story added", cmkCode, func(t *testing.T, wt string) {
			t.Helper()
			swCommitFile(t, wt, "notes.txt", cmkText(cmkJoin(cmkFiller(1, 5), cmkSet())...), "late: a file with a conflict in it")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true, taxTest: true})
			lxChangeAfterTheReviews(t, wt)
			swRefused(t, ldStale, wt, ldSession, "stop")

			tc.commit(t, wt)
			trunk := ldTrunk(t, main)
			swRefused(t, tc.code, wt, ldSession, "stop")
			if got := ldTrunk(t, main); got != trunk {
				t.Errorf("trunk moved to %s though the stop was refused", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}
