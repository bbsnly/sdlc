package e2e

// US-129 AC-3 and AC-6 count the git processes each process of the walk
// starts by the parent the counting git logs. A git started through an
// intermediary, `sh -c 'git a; git b'`, an alias that runs git, xargs, has a
// parent that is no process of the walk, and would be counted nowhere: the
// count would fall with nothing removed (US-129 red team, V1 and R7). So every
// git the counting git saw has to have been started by a process of the walk,
// or by a git it saw. It passes at the base.

import (
	"slices"
	"strings"
	"testing"
)

func TestAC6EveryGitTheCountingGitSawWasStartedByAProcessOfTheWalkOrByGit(t *testing.T) {
	parallel(t)
	w, byPID := u129Walked(t)
	walk := map[int]bool{}
	for _, c := range w.calls {
		walk[c.pid] = true
	}
	gits := map[int]bool{}
	for _, qs := range byPID {
		for _, q := range qs {
			gits[q.pid] = true
		}
	}
	if len(gits) == 0 {
		t.Fatal("the counting git saw no git at all")
	}
	var strays []string
	for ppid, qs := range byPID {
		if walk[ppid] || gits[ppid] {
			continue
		}
		for _, q := range qs {
			strays = append(strays, w.spelt(strings.Join(u129Sealed(q.args), " ")))
		}
	}
	slices.Sort(strays)
	for _, s := range strays {
		t.Errorf("AC-6 git %q was started by a process that is not the walk's and not a git", s)
	}
}
