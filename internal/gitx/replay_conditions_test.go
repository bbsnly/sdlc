package gitx

// US-090: two things the replay does that US-088's design reviews asked for
// after its tests were frozen, each held here. An attribute nobody committed,
// read through core.attributesFile or from a tree attr.tree names, does not
// change how the replay merges a file (AC-4). And a merge driver the
// repository configures never runs in it, whatever its name: one whose name
// the command line cannot carry, because it holds an equals sign or the text
// this package puts its private directory in place of, is refused before git
// runs, and the one with an empty name is replaced like any other (AC-11).
//
// The AC numbers in this file are US-090's. Each case ends by running the
// rebase git would run with nothing sealed, in the same checkout: what the
// replay kept out is shown to have been live there, so a case cannot pass
// because git never read the setting. Helpers carry an rc prefix, and the rp
// ones are replay_test.go's.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// rcUnsealed is the rebase a person would run by hand, with whatever the
// repository configures left to run, and whether it finished.
func rcUnsealed(t *testing.T, wt, onto, upstream string) bool {
	t.Helper()
	cmd := wtExec(t, wt, "rebase", "--merge", "--no-fork-point", "--onto", onto, upstream)
	return cmd.Run() == nil
}

// ---------------------------------------------------------------- AC-4

// rcUnionTree is a tree in root's objects, on no branch, whose .gitattributes
// gives every path git's union merge.
func rcUnionTree(t *testing.T, root string) string {
	t.Helper()
	file := filepath.Join(lgTempDir(t), "gitattributes")
	if err := os.WriteFile(file, []byte("* merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob := wtGitOut(t, root, "hash-object", "-w", "--", file)
	cmd := exec.CommandContext(t.Context(), "git", "mktree")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader("100644 blob " + blob + "\t.gitattributes\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git mktree: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// AC-4: the story and trunk changed the same lines of billing.go, which is a
// conflict. An attribute nobody committed gives the file git's union merge,
// which joins the two sides with no conflict: read through
// core.attributesFile, and from a tree attr.tree names. The replay reads
// neither: it stops, names billing.go, and is abandoned without a trace. The
// same rebase run by hand in the same checkout does join them, which is what
// the replay kept out.
func TestAC4AnAttributeNobodyCommittedDoesNotChangeHowTheReplayMergesAFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(t *testing.T, root string)
	}{
		{"AC-4 an attributes file named by core.attributesFile", func(t *testing.T, root string) {
			t.Helper()
			file := filepath.Join(lgTempDir(t), "attributes")
			if err := os.WriteFile(file, []byte("* merge=union\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			run(t, root, "config", "core.attributesFile", filepath.ToSlash(file))
		}},
		{"AC-4 a tree named by attr.tree", func(t *testing.T, root string) {
			t.Helper()
			run(t, root, "config", "attr.tree", rcUnionTree(t, root))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, fork := rpRepo(t)
			wt := lgStory(t, root)
			tip := rpApply(t, wt, "US-001", rpConflicts[0].story)
			trunk := rpApply(t, root, "another story lands", rpConflicts[0].trunk)
			tc.set(t, root)
			if got := wtGitOut(t, wt, "check-attr", "merge", "--", "billing.go"); !strings.HasSuffix(got, ": union") {
				if strings.Contains(tc.name, "attr.tree") {
					t.Skipf("this git does not read attr.tree, which came with 2.44: billing.go's merge attribute is %q", got)
				}
				t.Fatalf("precondition: billing.go's merge attribute is %q in the story's checkout, want union", got)
			}
			before := rpSnap(t, wt)

			if err := Replay(t.Context(), wt, trunk, fork); err == nil {
				t.Fatalf("Replay reported no error for a conflict that only an attribute nobody committed would join; billing.go is\n%s",
					lgRead(t, wt, "billing.go"))
			}
			if got := rpUnmerged(t, wt); !slices.Equal(got, []string{"billing.go"}) {
				t.Errorf("Unmerged names %q, want billing.go", got)
			}
			if got := lgRev(t, root, "refs/heads/main"); got != trunk {
				t.Errorf("trunk moved from %s to %s", trunk, got)
			}
			rpAbandonIfPartWay(t, wt)
			if got := rpSnap(t, wt); got != before {
				t.Errorf("the abandoned replay left the checkout changed:\nbefore\n%s\nafter\n%s", before, got)
			}
			if got := lgRev(t, wt, "HEAD"); got != tip {
				t.Errorf("the story's tip moved from %s to %s", tip, got)
			}

			// What was kept out: by hand, the attribute joins the two sides.
			if !rcUnsealed(t, wt, trunk, fork) {
				t.Fatal("precondition: the same rebase run by hand did not finish, so the attribute joins nothing here and the case shows nothing")
			}
			joined := lgRead(t, wt, "billing.go")
			for _, line := range []string{"// story line A", "// trunk's own line"} {
				if !strings.Contains(joined, line) {
					t.Errorf("precondition: the rebase by hand left billing.go without %q, so the union merge did not join the sides:\n%s", line, joined)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- AC-11

// rcSteered is what a driver here does when it runs: it writes the story's
// lines somewhere else and reports success.
const rcSteered = "printf 'package billing\\n\\n// story line A\\n// story line B\\n' > \"$2\"\nexit 0"

// rcDriver configures a merge driver by the name given, names it for
// billing.go by an attribute nobody committed, and returns its configuration
// key.
func rcDriver(t *testing.T, root, bin, marks, name string) string {
	t.Helper()
	key := "merge." + name + ".driver"
	run(t, root, "config", key, "'"+rpScript(t, bin, marks, "driver", rcSteered)+"' %O %A %B")
	write(t, root, ".git/info/attributes", "billing.go merge="+name+"\n")
	return key
}

// rcRan holds the driver to having run in the rebase by hand that ends a case:
// git does read the attribute as naming it.
func rcRan(t *testing.T, marks string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(marks, "driver")); err != nil {
		t.Errorf("precondition: the driver did not run in the same rebase run by hand, so nothing shows git reads the attribute as naming it: %v", err)
	}
}

// AC-11: a merge driver whose name holds an equals sign, or the text this
// package puts its private directory in place of, cannot be replaced on git's
// command line: the setting would land on another key and the driver would
// run. The replay refuses before git runs: SDLC-E0021 naming the driver's
// key, no rebase begun, the checkout exactly as it was, and the driver not
// run. Run by hand, the same rebase does run it.
func TestAC11AMergeDriverWhoseNameTheCommandLineCannotCarryIsRefusedBeforeGitRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver here is a shell script marked executable, which Windows does not run as such")
	}
	for _, tc := range []struct{ name, driver string }{
		{"AC-11 a name that holds an equals sign", "st=eer"},
		{"AC-11 a name that holds {sdlc-private}", "st{sdlc-private}eer"},
		{"AC-11 a name that is {sdlc-private} and nothing else", "{sdlc-private}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, fork := rpRepo(t)
			wt, tip := rpStory(t, root)
			trunk := rpTrunkMoves(t, root)
			bin, marks := lgTempDir(t), lgTempDir(t)
			key := rcDriver(t, root, bin, marks, tc.driver)
			before := rpSnap(t, wt)

			err := Replay(t.Context(), wt, trunk, fork)
			rpNoneRan(t, marks)
			var refusal *sdlcerr.Error
			if !errors.As(err, &refusal) {
				t.Fatalf("Replay returned %v, want a refusal a person can read, naming the driver", err)
			}
			// US-093: the stop says this refusal under SDLC-E0055 and in words of its own, so what
			// Replay returns for it may carry either code, and the key may sit in the what or in the
			// why. What is held is that it is a refusal sdlc made, before git ran, that names the key.
			if refusal.Code != sdlcerr.RepositoryUnreadable && refusal.Code != sdlcerr.LandingCheckFailed {
				t.Errorf("the refusal's code is %s, want SDLC-E0021 or SDLC-E0055: %s / %s", refusal.Code, refusal.What, refusal.Why)
			}
			if !strings.Contains(refusal.What+" "+refusal.Why, strconv.Quote(key)) {
				t.Errorf("the refusal says %q / %q, and does not name the driver's key %s", refusal.What, refusal.Why, strconv.Quote(key))
			}
			if got := rpPartWay(t, wt); got != "" {
				t.Errorf("%s is part-way after a replay that was refused before git ran", got)
			}
			if got := rpSnap(t, wt); got != before {
				t.Errorf("the refused replay changed the checkout:\nbefore\n%s\nafter\n%s", before, got)
			}
			if got := lgRev(t, wt, "HEAD"); got != tip {
				t.Errorf("the story's tip moved from %s to %s", tip, got)
			}
			if got := lgRev(t, root, "refs/heads/main"); got != trunk {
				t.Errorf("trunk moved from %s to %s", trunk, got)
			}

			rcUnsealed(t, wt, trunk, fork)
			rcRan(t, marks)
		})
	}
}

// AC-11: the merge driver with an empty name, merge..driver, is one a path
// names with an empty merge attribute. It is replaced like every other driver
// by one that fails, so the path stops as a conflict: the replay returns an
// error, Unmerged names billing.go, the driver does not run, and abandoning
// the replay leaves the checkout as it was. Run by hand, the same rebase does
// run it.
func TestAC11TheMergeDriverWithAnEmptyNameDoesNotRunAndItsPathStopsAsAConflict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver here is a shell script marked executable, which Windows does not run as such")
	}
	root, fork := rpRepo(t)
	wt, tip := rpStory(t, root)
	trunk := rpTrunkMoves(t, root)
	bin, marks := lgTempDir(t), lgTempDir(t)
	if key := rcDriver(t, root, bin, marks, ""); key != "merge..driver" {
		t.Fatalf("precondition: the driver's key is %q, want merge..driver", key)
	}
	if got := wtGitOut(t, wt, "config", "--get", "merge..driver"); got == "" {
		t.Fatal("precondition: git reads no merge..driver from the configuration")
	}
	before := rpSnap(t, wt)

	err := Replay(t.Context(), wt, trunk, fork)
	rpNoneRan(t, marks)
	if err == nil {
		t.Fatalf("Replay reported no error for a path whose merge driver was to be one that fails; billing.go is\n%s", lgRead(t, wt, "billing.go"))
	}
	if got := rpPartWay(t, wt); got == "" {
		t.Error("nothing is part-way after a replay that stopped on the path the driver was named for")
	}
	if got := rpUnmerged(t, wt); !slices.Equal(got, []string{"billing.go"}) {
		t.Errorf("Unmerged names %q, want billing.go", got)
	}
	if err := AbandonReplay(t.Context(), wt); err != nil {
		t.Fatalf("AbandonReplay: %v", err)
	}
	rpNoneRan(t, marks)
	if got := rpSnap(t, wt); got != before {
		t.Errorf("the abandoned replay left the checkout changed:\nbefore\n%s\nafter\n%s", before, got)
	}
	if got := lgRev(t, wt, "HEAD"); got != tip {
		t.Errorf("the story's tip moved from %s to %s", tip, got)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != trunk {
		t.Errorf("trunk moved from %s to %s", trunk, got)
	}

	rcUnsealed(t, wt, trunk, fork)
	rcRan(t, marks)
}
