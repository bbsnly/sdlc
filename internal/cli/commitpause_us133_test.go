package cli

// US-133 in process: the commit gate pauses for a person's approval because
// of what the change touches, as well as because of the story's tier.
//
// A story is worked in its own worktree, as `sdlc init` sets a project up,
// from a trunk whose configuration lists human_gates.pre_commit_pause_paths
// and holds two files under internal/hook/ before the story starts. What the
// story changes is measured from where its branch left trunk, with what git
// does not track yet (AC-2): a file added, changed, deleted, renamed away or
// left untracked, and a name that differs from a glob only in case, which on
// macOS and Windows is the same file (THREATS T3, T4). The pause is the
// tiers' and the paths' together, and nothing the story does takes one away:
// not its tier, not a change to its own copy of .sdlc/config.json, not a
// flag of the gate (AC-3). A pass without a pause leaves landed_without_pause
// on the record, which `sdlc log` shows and `sdlc ack` marks read (AC-4), and
// `sdlc status` says beforehand whether the commit will pause and why (AC-5).
//
// Everything is read as a caller sees it: the gate's exit and what it printed,
// the record on disk, status, log and doctor. Refusals are matched by their
// code, and a file and its glob are matched by being named, each glob nearest
// its own file, so that the wording is the plan's. Helpers carry a cp prefix,
// for commit pause.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// cpLandedWithoutPause is the event AC-4 names.
const cpLandedWithoutPause = "landed_without_pause"

// cpHookGlob and cpPluginGlob are the globs a project in these tests lists:
// the hook's directory, and the plugin's hook configuration.
const (
	cpHookGlob   = "internal/hook/**"
	cpPluginGlob = "plugin/hooks/*.json"
	cpBody       = "package hook\n\n// Written by the story.\n"
)

// cpTrunkFiles are on trunk before the story, under a listed glob, so that a
// story can change, delete or rename one.
var cpTrunkFiles = map[string]string{
	"internal/hook/guard.go": "package hook\n\n// Guard is on trunk before the story.\n",
	"internal/hook/old.go":   "package hook\n\n// Old is on trunk before the story.\n",
}

// cpWalk is the project a test starts from: US-001's risk tier ("" leaves
// it out of the backlog), and the two human_gates lists trunk commits.
type cpWalk struct {
	tier  string
	tiers []string
	globs []string
}

var (
	// cpLow is a low story in a project that pauses the high tier and the
	// hook's paths: only a file can pause it.
	cpLow = cpWalk{tier: "low", tiers: []string{"high"}, globs: []string{cpHookGlob, cpPluginGlob}}
	// cpHighUnpaused is a high story in a project that pauses no tier.
	cpHighUnpaused = cpWalk{tier: "high", tiers: []string{}, globs: []string{cpHookGlob}}
	// cpHighPaused is a high story in a project that pauses the high tier.
	cpHighPaused = cpWalk{tier: "high", tiers: []string{"high"}, globs: []string{cpHookGlob}}
)

// cpWalked is the main checkout and US-001's worktree, the story taken to the
// implementation by the real commands, from a copy of a walked template. The
// test's commands run in the worktree from ldSession.
func cpWalked(t *testing.T, w cpWalk) (main, wt string) {
	t.Helper()
	main, _ = walkedCopyNoted(t, fmt.Sprintf("us133 %+v", w), func(t *testing.T, root string) map[string]string {
		t.Helper()
		ldRepositoryAt(t, root, func(root string) {
			if w.tier != "" {
				ldTier(t, root, w.tier)
			}
			setConfigOnDisk(t, root, "human_gates", map[string]any{
				"pre_commit_pause_tiers": w.tiers,
				"pre_commit_pause_paths": w.globs,
				"dor_advocate_check":     false,
			})
			for rel, body := range cpTrunkFiles {
				writeFile(t, root, rel, body)
			}
		})
		cut := ldTrunk(t, root)
		ldStarted(t, root)
		return map[string]string{"cut": cut}
	})
	wt = swWorktreeOf(main, "US-001")
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	return main, wt
}

// cpReviewed takes the story from the implementation through the code review,
// with the work as it stands in the worktree.
func cpReviewed(t *testing.T, wt string) {
	t.Helper()
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
}

// cpGate is the commit gate asked to pass, in the worktree.
func cpGate(t *testing.T, wt string) result {
	t.Helper()
	return wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "the story's work")
}

// cpPaused holds a gate's answer to a pause: refused with SDLC-E0037, and the
// commit gate not passed on the record.
func cpPaused(t *testing.T, main string, r result) string {
	t.Helper()
	out := r.stdout + r.stderr
	if r.code == 0 {
		t.Fatalf("the commit gate passed where it should have paused for a person:\n%s", out)
	}
	if !strings.Contains(out, ldNotApproved) {
		t.Fatalf("the commit gate was refused with something other than %s:\n%s", ldNotApproved, out)
	}
	if swRecord(t, main, "US-001").Pass(model.GateCommit) {
		t.Error("the refused commit gate is recorded as passed")
	}
	return out
}

// cpFlat is text with its line wrapping collapsed.
func cpFlat(text string) string { return strings.Join(strings.Fields(text), " ") }

// cpClauses is text cut into its clauses: at line breaks, semicolons,
// commas and full stops, so that a file and a glob in one clause are said
// together.
var cpClauses = regexp.MustCompile(`\n|; |, |\. `)

// cpNames holds text to AC-2's message: each file is named, and so is the glob
// it matched, and where there is more than one, each file is named in a
// clause with its own glob and no other file of the list.
func cpNames(t *testing.T, what, text string, pairs ...[2]string) {
	t.Helper()
	flat := cpFlat(text)
	for _, p := range pairs {
		file, glob := p[0], p[1]
		if !strings.Contains(flat, file) || !strings.Contains(flat, glob) {
			t.Errorf("AC-2 %s does not name %s and the glob it matched, %s:\n%s", what, file, glob, text)
			continue
		}
		if len(pairs) < 2 {
			continue
		}
		together := false
		for _, clause := range cpClauses.Split(text, -1) {
			others := false
			for _, q := range pairs {
				others = others || (q[0] != file && strings.Contains(clause, q[0]))
			}
			together = together || (strings.Contains(clause, file) && strings.Contains(clause, glob) && !others)
		}
		if !together {
			t.Errorf("AC-2 %s does not name %s in a clause with its own glob, %s:\n%s", what, file, glob, text)
		}
	}
}

// cpWord reports whether text holds word as a word of its own.
func cpWord(text, word string) bool {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

// cpGives reports whether text names a path, or a tier as a word of its own:
// "low" is in "allow" and "below".
func cpGives(text, name string) bool {
	if strings.Contains(name, "/") {
		return strings.Contains(text, name)
	}
	return cpWord(text, name)
}

// cpLandings is every landed_without_pause event on US-001's record.
func cpLandings(t *testing.T, main string) []model.Event {
	t.Helper()
	var out []model.Event
	for _, e := range swRecord(t, main, "US-001").Events {
		if e.Type == cpLandedWithoutPause {
			out = append(out, e)
		}
	}
	return out
}

// cpPause is what `sdlc status --json` says under pre_commit_pause: whether
// the commit will pause, and the object as text, for the reasons it gives.
func cpPause(t *testing.T, wt string) (willPause bool, text string) {
	t.Helper()
	var doc map[string]json.RawMessage
	r := wtMust(t, wt, ldSession, "", "status", "--json")
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatalf("status --json is not one object: %v\n%s", err, r.stdout)
	}
	raw, ok := doc["pre_commit_pause"]
	if !ok {
		t.Fatalf("AC-5 status --json has no pre_commit_pause while the story stands before the commit gate:\n%s", r.stdout)
	}
	var pause map[string]any
	if err := json.Unmarshal(raw, &pause); err != nil {
		t.Fatalf("AC-5 pre_commit_pause is not an object: %s", raw)
	}
	will, ok := pause["will_pause"].(bool)
	if !ok {
		t.Fatalf("AC-5 pre_commit_pause has no will_pause of true or false: %s", raw)
	}
	return will, string(raw)
}

// ---------------------------------------------------------------- AC-2

// AC-2: a low story, in a tier no setting pauses, whose change touches a
// listed path, pauses at the commit gate and names the file and the glob.
// Each way a change can touch a path is a row of its own.
func TestAC2ALowStoryWhoseChangeTouchesAListedPathPausesAtTheCommitGate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file string
		change     func(t *testing.T, wt string)
	}{
		{"AC-2 a file added under the glob", "internal/hook/new.go", func(t *testing.T, wt string) {
			t.Helper()
			writeFile(t, wt, "internal/hook/new.go", cpBody)
		}},
		{"AC-2 a file trunk holds, changed", "internal/hook/guard.go", func(t *testing.T, wt string) {
			t.Helper()
			writeFile(t, wt, "internal/hook/guard.go", cpTrunkFiles["internal/hook/guard.go"]+"\n// Changed.\n")
		}},
		{"AC-2 a file trunk holds, deleted", "internal/hook/old.go", func(t *testing.T, wt string) {
			t.Helper()
			if err := os.Remove(filepath.Join(wt, "internal", "hook", "old.go")); err != nil {
				t.Fatal(err)
			}
		}},
		{"AC-2 a file trunk holds, renamed out from under the glob", "internal/hook/old.go", func(t *testing.T, wt string) {
			t.Helper()
			wtGit(t, wt, "mv", "internal/hook/old.go", "internal/old.go")
		}},
		{"AC-2 a file in a directory the glob reaches only at depth", "internal/hook/claudecode/deep/new.go", func(t *testing.T, wt string) {
			t.Helper()
			writeFile(t, wt, "internal/hook/claudecode/deep/new.go", cpBody)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := cpWalked(t, cpLow)
			ldWork(t, wt)
			tc.change(t, wt)
			cpReviewed(t, wt)
			commitEverything(t, wt)

			out := cpPaused(t, main, cpGate(t, wt))
			cpNames(t, "the commit gate's refusal", out, [2]string{tc.file, cpHookGlob})
			if got := cpLandings(t, main); len(got) != 0 {
				t.Errorf("AC-4 a commit gate that paused left %s on the record: %+v", cpLandedWithoutPause, got)
			}
		})
	}
}

// AC-2: a new file git does not track yet is part of what the story changes.
// The pause is asked before the tree is held to being committed, so it is the
// pause that is said, not the uncommitted work.
func TestAC2AnUntrackedFileUnderAListedPathPausesTheCommitGate(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpLow)
	ldWork(t, wt)
	writeFile(t, wt, "internal/hook/fresh/untracked.go", cpBody)
	cpReviewed(t, wt)
	wtGit(t, wt, "add", "--", "billing.go", "refund.go", "price list.txt", "internal/invoice_test.go")
	wtGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "the story")

	out := cpPaused(t, main, cpGate(t, wt))
	cpNames(t, "the commit gate's refusal", out, [2]string{"internal/hook/fresh/untracked.go", cpHookGlob})
}

// AC-2: every matching file is named, each with the glob it matched.
func TestAC2ThePauseNamesEachMatchingFileAndTheGlobItMatched(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpLow)
	writeFile(t, wt, "internal/hook/new.go", cpBody)
	writeFile(t, wt, "plugin/hooks/hooks.json", "{}\n")
	writeFile(t, wt, "plugin/hooks/README.md", "not matched by *.json\n")
	cpReviewed(t, wt)
	commitEverything(t, wt)

	out := cpPaused(t, main, cpGate(t, wt))
	cpNames(t, "the commit gate's refusal", out,
		[2]string{"internal/hook/new.go", cpHookGlob}, [2]string{"plugin/hooks/hooks.json", cpPluginGlob})
	if strings.Contains(out, "plugin/hooks/README.md") {
		t.Errorf("AC-2 the refusal names a file no glob matches:\n%s", out)
	}
}

// AC-2: where both the tier and a file pause the story, the message says both.
func TestAC2APausedTierAndAMatchingFileAreBothNamed(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpHighPaused)
	writeFile(t, wt, "internal/hook/new.go", cpBody)
	cpReviewed(t, wt)
	commitEverything(t, wt)

	out := cpPaused(t, main, cpGate(t, wt))
	cpNames(t, "the commit gate's refusal", out, [2]string{"internal/hook/new.go", cpHookGlob})
	if !cpWord(out, "high") {
		t.Errorf("AC-2 the refusal does not name the tier high:\n%s", out)
	}
}

// AC-2 and THREATS T4: on macOS and Windows a name that differs only in case
// is the same file, so a glob matches without regard to case; erring this way
// pauses, the other way lets a change past a person.
func TestAC2AGlobMatchesAPathThatDiffersFromItOnlyInCase(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpLow)
	// No plugin/ directory is on trunk, so the name is the one git records on
	// every platform, and only a match that folds case finds it.
	writeFile(t, wt, "Plugin/Hooks/Hooks.JSON", "{}\n")
	cpReviewed(t, wt)
	commitEverything(t, wt)
	if staged := wtGit(t, wt, "log", "-1", "--name-only", "--format="); !strings.Contains(staged, "Plugin/Hooks/Hooks.JSON") {
		t.Fatalf("the story's commit does not hold the file as it was written:\n%s", staged)
	}

	out := cpPaused(t, main, cpGate(t, wt))
	cpNames(t, "the commit gate's refusal", out, [2]string{"Plugin/Hooks/Hooks.JSON", cpPluginGlob})
}

// AC-2: the change is measured from where the story's branch left trunk, so a
// listed file trunk already held, and the story did not touch, pauses nothing.
func TestAC2AListedFileTrunkAlreadyHeldAndTheStoryLeftAlonePausesNothing(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpLow)
	ldWork(t, wt)
	cpReviewed(t, wt)
	commitEverything(t, wt)

	if r := cpGate(t, wt); r.code != 0 {
		t.Fatalf("AC-2 a story that touched no listed file was held at the commit gate:\n%s%s", r.stdout, r.stderr)
	}
	if got := cpLandings(t, main); len(got) != 1 {
		t.Errorf("AC-4 the commit gate passed without a pause and left %d %s events, want 1", len(got), cpLandedWithoutPause)
	}
}

// AC-2 as pre_commit_pause_tiers pauses today: the pause is answered by a
// person's approval of the work as it stands, and then the gate passes, with
// no landed_without_pause, since it did not land without a person.
func TestAC2APausedStoryIsCommittedOnceAPersonApprovesIt(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpLow)
	writeFile(t, wt, "internal/hook/new.go", cpBody)
	cpReviewed(t, wt)
	commitEverything(t, wt)
	cpPaused(t, main, cpGate(t, wt))

	ldApproved(t, wt)
	if r := cpGate(t, wt); r.code != 0 {
		t.Fatalf("AC-2 the work a person approved was refused:\n%s%s", r.stdout, r.stderr)
	}
	if got := cpLandings(t, main); len(got) != 0 {
		t.Errorf("AC-4 a story a person approved is recorded as %s: %+v", cpLandedWithoutPause, got)
	}
}

// AC-2 in one checkout, with story worktrees off: the story's change is what
// it did since it started, and a listed file in it pauses the gate there too.
func TestAC2InOneCheckoutAListedFilePausesTheCommitGate(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "human_gates", map[string]any{
		"pre_commit_pause_tiers": []string{"high"}, "pre_commit_pause_paths": []string{cpHookGlob}, "dor_advocate_check": false,
	})
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"Refunds","status":"ready","risk_tier":"low","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)
	commitEverything(t, root)
	mustRun(t, "start")
	reach(t, root, model.GateImplementation)
	writeFile(t, root, "internal/hook/new.go", cpBody)
	swPass(t, root, "", model.GateImplementation, model.GateCommit)
	commitEverything(t, root)

	r := run(t, "gate", "commit", "pass", "--note", "on trunk")
	if r.code == 0 || !strings.Contains(r.stderr, ldNotApproved) {
		t.Fatalf("AC-2 a low story that changed a listed file was committed with nobody asked:\n%s", r.stderr)
	}
	cpNames(t, "the commit gate's refusal", r.stderr, [2]string{"internal/hook/new.go", cpHookGlob})
}

// ---------------------------------------------------------------- AC-3

// AC-3 and THREATS T1: the story's own copy of .sdlc/config.json, changed in
// its commits, does not take away a pause trunk's configuration calls for:
// neither the paths emptied nor the tier taken out of the list.
func TestAC3AStorysOwnCopyOfTheConfigurationCannotTakeAPauseAway(t *testing.T) {
	t.Parallel()
	narrowed := map[string]any{"pre_commit_pause_tiers": []string{}, "pre_commit_pause_paths": []string{}, "dor_advocate_check": false}
	t.Run("AC-3 the paths emptied in the story's copy", func(t *testing.T) {
		t.Parallel()
		main, wt := cpWalked(t, cpLow)
		writeFile(t, wt, "internal/hook/new.go", cpBody)
		setConfigOnDisk(t, wt, "human_gates", narrowed)
		cpReviewed(t, wt)
		commitEverything(t, wt)

		out := cpPaused(t, main, cpGate(t, wt))
		cpNames(t, "the commit gate's refusal", out, [2]string{"internal/hook/new.go", cpHookGlob})
		if got := cpLandings(t, main); len(got) != 0 {
			t.Errorf("AC-3 the person was told no sign-off was needed: %+v", got)
		}
	})
	t.Run("AC-3 the tier taken out in the story's copy", func(t *testing.T) {
		t.Parallel()
		main, wt := cpWalked(t, cpHighPaused)
		ldWork(t, wt)
		setConfigOnDisk(t, wt, "human_gates", narrowed)
		cpReviewed(t, wt)
		commitEverything(t, wt)

		out := cpPaused(t, main, cpGate(t, wt))
		if !cpWord(out, "high") {
			t.Errorf("AC-3 the refusal does not name the tier high:\n%s", out)
		}
	})
	t.Run("AC-3 the paths emptied in the story's copy and not committed", func(t *testing.T) {
		t.Parallel()
		main, wt := cpWalked(t, cpLow)
		writeFile(t, wt, "internal/hook/new.go", cpBody)
		cpReviewed(t, wt)
		commitEverything(t, wt)
		setConfigOnDisk(t, wt, "human_gates", narrowed)

		// The pause is asked before the tree is held to being committed, so it
		// is the pause that is said, not the uncommitted copy.
		out := cpPaused(t, main, cpGate(t, wt))
		cpNames(t, "the commit gate's refusal", out, [2]string{"internal/hook/new.go", cpHookGlob})
		if got := cpLandings(t, main); len(got) != 0 {
			t.Errorf("AC-3 the person was told no sign-off was needed: %+v", got)
		}
	})
}

// AC-3: no tier turns a pause a file calls for off, whatever it is spelt as,
// and a story with none in the backlog is no exception.
func TestAC3NoRiskTierTurnsOffAPauseAFileCallsFor(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"", "low", "LOW", "none", "trivial"} {
		t.Run("AC-3 tier "+fmt.Sprintf("%q", tier), func(t *testing.T) {
			t.Parallel()
			w := cpLow
			w.tier = tier
			main, wt := cpWalked(t, w)
			writeFile(t, wt, "internal/hook/new.go", cpBody)
			cpReviewed(t, wt)
			commitEverything(t, wt)

			cpPaused(t, main, cpGate(t, wt))
		})
	}
}

// AC-3 and THREATS T7: no flag of the commit gate turns a pause off. Every
// flag `sdlc gate` takes, its own and the ones it inherits, is tried in turn,
// so a flag added later is tried too.
func TestAC3NoFlagOfTheCommitGateTurnsOffAPauseAFileCallsFor(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpLow)
	writeFile(t, wt, "internal/hook/new.go", cpBody)
	cpReviewed(t, wt)
	commitEverything(t, wt)

	gate, _, err := New(strings.NewReader(""), io.Discard, io.Discard).Find([]string{"gate"})
	if err != nil || gate == nil || gate.Name() != "gate" {
		t.Fatalf("no gate command to read the flags of: %v", err)
	}
	var tried []string
	try := func(f *pflag.Flag) {
		if f.Name == "help" || slices.Contains(tried, f.Name) {
			return
		}
		tried = append(tried, f.Name)
		args := []string{"gate", "commit", "pass"}
		switch {
		case f.Name == "note":
			args = append(args, "--note=a person said it was fine")
		case f.Name == "story":
			args = append(args, "--note", "x", "--story=US-001")
		case f.Value.Type() == "bool":
			args = append(args, "--note", "x", "--"+f.Name)
		default:
			args = append(args, "--note", "x", "--"+f.Name+"=1")
		}
		r := wtRun(t, wt, ldSession, "", args...)
		if r.code == 0 || swRecord(t, main, "US-001").Pass(model.GateCommit) {
			t.Fatalf("AC-3 sdlc %s passed the commit gate on a change a glob pauses:\n%s", strings.Join(args, " "), r.stdout)
		}
	}
	gate.Flags().VisitAll(try)
	gate.InheritedFlags().VisitAll(try)
	if !slices.Contains(tried, "story") || !slices.Contains(tried, "json") {
		t.Errorf("the flags tried, %q, are not the gate's", tried)
	}
	if got := cpLandings(t, main); len(got) != 0 {
		t.Errorf("AC-3 a flag left %s on the record: %+v", cpLandedWithoutPause, got)
	}
}

// AC-3 and THREATS T2: the landing asks again of a story that was rebased,
// as it asks of a tier that pauses, with trunk's configuration: a glob the
// person listed on trunk after the commit gate passed pauses the landing.
func TestAC3TheLandingAsksAgainWithTrunksPathsAfterARebase(t *testing.T) {
	t.Parallel()
	main, wt := cpWalked(t, cpLow)
	ldWork(t, wt)
	cpReviewed(t, wt)
	commitEverything(t, wt)
	if r := cpGate(t, wt); r.code != 0 {
		t.Fatalf("the commit gate refused a story that touched no listed file:\n%s", r.stderr)
	}
	swPass(t, wt, ldSession, model.GateRetro, "")
	ldBookkeeping(t, wt)

	setConfigOnDisk(t, main, "human_gates", map[string]any{
		"pre_commit_pause_tiers": []string{"high"}, "pre_commit_pause_paths": []string{cpHookGlob, "refund*.go"}, "dor_advocate_check": false,
	})
	swCommitFile(t, main, "CHANGES.md", "another story's work\n", "another story lands")
	wtGit(t, main, "add", "--", ".sdlc/config.json")
	wtGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "a person lists refund*.go")
	ldRebase(t, wt)
	ldReviewAgain(t, wt)
	before := ldTrunk(t, main)

	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	cpNames(t, "the landing's refusal", out, [2]string{"refund.go", "refund*.go"})
	if got := ldTrunk(t, main); got != before {
		t.Errorf("AC-3 trunk moved to %s on a change a person's glob pauses", got)
	}
}

// ---------------------------------------------------------------- AC-4

// AC-4: a pass without a pause leaves landed_without_pause, naming the tier and
// saying that no file matched pre_commit_pause_paths.
func TestAC4APassWithoutAPauseRecordsLandedWithoutPauseNamingTheTier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		w    cpWalk
	}{
		{"AC-4 a low story in a project that pauses high", cpLow},
		{"AC-4 a high story in a project that pauses no tier", cpHighUnpaused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := cpWalked(t, tc.w)
			ldWork(t, wt)
			cpReviewed(t, wt)
			commitEverything(t, wt)
			if r := cpGate(t, wt); r.code != 0 {
				t.Fatalf("the commit gate refused a story no setting pauses:\n%s", r.stderr)
			}
			got := cpLandings(t, main)
			if len(got) != 1 {
				t.Fatalf("AC-4 the record has %d %s events, want 1: %+v", len(got), cpLandedWithoutPause, swRecord(t, main, "US-001").Events)
			}
			said := got[0].Message + " " + got[0].Reason
			if !cpWord(said, tc.w.tier) {
				t.Errorf("AC-4 the event does not name the tier %s: %q", tc.w.tier, said)
			}
			if !strings.Contains(said, "pre_commit_pause_paths") {
				t.Errorf("AC-4 the event does not say that no file matched pre_commit_pause_paths: %q", said)
			}
		})
	}
}

// AC-4: `sdlc log` shows the person what landed without their sign-off, and
// `sdlc ack` marks it read.
func TestAC4SdlcLogShowsALandingWithoutAPauseAndAckMarksItRead(t *testing.T) {
	t.Parallel()
	_, wt := cpWalked(t, cpHighUnpaused)
	ldWork(t, wt)
	cpReviewed(t, wt)
	commitEverything(t, wt)
	if r := cpGate(t, wt); r.code != 0 {
		t.Fatalf("the commit gate refused a story no setting pauses:\n%s", r.stderr)
	}

	prose := wtMust(t, wt, "", "", "log").stdout
	if !strings.Contains(prose, "US-001") || !strings.Contains(prose, "pre_commit_pause_paths") || !cpWord(prose, "high") {
		t.Errorf("AC-4 sdlc log does not show that US-001, tier high, landed with no file matching pre_commit_pause_paths:\n%s", prose)
	}
	machine := wtMust(t, wt, "", "", "log", "--json").stdout
	if !strings.Contains(machine, cpLandedWithoutPause) {
		t.Errorf("AC-4 sdlc log --json does not carry %s:\n%s", cpLandedWithoutPause, machine)
	}

	wtMust(t, wt, "", "", "ack", "--through", "HEAD")
	if after := wtMust(t, wt, "", "", "log").stdout; strings.Contains(after, "US-001") {
		t.Errorf("AC-4 after sdlc ack, sdlc log still lists the landing as unread:\n%s", after)
	}
	if all := wtMust(t, wt, "", "", "log", "--all").stdout; !strings.Contains(all, "pre_commit_pause_paths") {
		t.Errorf("AC-4 sdlc log --all lost the landing without a pause once it was read:\n%s", all)
	}
}

// ---------------------------------------------------------------- AC-5

// AC-5: before the commit gate, status says whether the commit will pause, and
// why, from the work as it stands in the worktree: committed or not, tracked
// or not.
func TestAC5StatusSaysBeforeTheCommitGateWhetherTheCommitWillPauseAndWhy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		w      cpWalk
		change func(t *testing.T, wt string)
		will   bool
		names  []string
	}{
		{"AC-5 an untracked file under a glob", cpLow, func(t *testing.T, wt string) {
			t.Helper()
			writeFile(t, wt, "internal/hook/new.go", cpBody)
		}, true, []string{"internal/hook/new.go", cpHookGlob}},
		{"AC-5 a tracked file under a glob, changed and not staged", cpLow, func(t *testing.T, wt string) {
			t.Helper()
			writeFile(t, wt, "internal/hook/guard.go", "package hook\n")
		}, true, []string{"internal/hook/guard.go", cpHookGlob}},
		{"AC-5 a tracked file under a glob, deleted and not staged", cpLow, func(t *testing.T, wt string) {
			t.Helper()
			if err := os.Remove(filepath.Join(wt, "internal", "hook", "old.go")); err != nil {
				t.Fatal(err)
			}
		}, true, []string{"internal/hook/old.go", cpHookGlob}},
		{"AC-5 a tracked file renamed away from a glob, staged", cpLow, func(t *testing.T, wt string) {
			t.Helper()
			wtGit(t, wt, "mv", "internal/hook/old.go", "internal/old.go")
		}, true, []string{"internal/hook/old.go", cpHookGlob}},
		{"AC-5 a tier that pauses", cpHighPaused, func(t *testing.T, wt string) {
			t.Helper()
			ldWork(t, wt)
		}, true, []string{"high"}},
		{"AC-5 nothing that pauses", cpLow, func(t *testing.T, wt string) {
			t.Helper()
			ldWork(t, wt)
		}, false, []string{"low"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, wt := cpWalked(t, tc.w)
			tc.change(t, wt)

			will, text := cpPause(t, wt)
			if will != tc.will {
				t.Errorf("AC-5 will_pause = %t, want %t: %s", will, tc.will, text)
			}
			prose := wtMust(t, wt, ldSession, "", "status").stdout
			if !strings.Contains(strings.ToLower(prose), "pause") {
				t.Errorf("AC-5 sdlc status does not say whether the commit will pause:\n%s", prose)
			}
			for _, name := range tc.names {
				if !cpGives(text, name) {
					t.Errorf("AC-5 pre_commit_pause does not give the reason %q: %s", name, text)
				}
				if !cpGives(prose, name) {
					t.Errorf("AC-5 sdlc status does not give the reason %q:\n%s", name, prose)
				}
			}
		})
	}
}

// AC-5 "at or before": status says it from the first gate, and at the commit
// gate itself.
func TestAC5StatusSaysWhetherTheCommitWillPauseFromTheFirstGateToTheCommitGate(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"Refunds","status":"ready","risk_tier":"high","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)
	commitEverything(t, root)
	mustRun(t, "start")
	for _, at := range []model.Gate{model.GateDoR, model.GateCommit} {
		reach(t, root, at)
		var doc map[string]any
		if err := json.Unmarshal([]byte(mustRun(t, "status", "--json").stdout), &doc); err != nil {
			t.Fatal(err)
		}
		pause, ok := doc["pre_commit_pause"].(map[string]any)
		if !ok {
			t.Fatalf("AC-5 status --json at %s has no pre_commit_pause object: %v", at, doc["pre_commit_pause"])
		}
		if pause["will_pause"] != true {
			t.Errorf("AC-5 at %s a high story in a project that pauses high has will_pause %v", at, pause["will_pause"])
		}
	}
}

// ---------------------------------------------------------------- AC-1

// AC-1: `sdlc init` writes pre_commit_pause_paths, empty.
func TestAC1InitWritesPreCommitPausePathsEmpty(t *testing.T) {
	t.Parallel()
	root := gitProject(t)
	mustRun(t, "init")
	var cfg struct {
		HumanGates map[string]json.RawMessage `json:"human_gates"`
	}
	wtReadJSON(t, filepath.Join(root, ".sdlc", "config.json"), &cfg)
	raw, ok := cfg.HumanGates["pre_commit_pause_paths"]
	if !ok {
		t.Fatalf("AC-1 init wrote no human_gates.pre_commit_pause_paths: %v", cfg.HumanGates)
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil || list == nil || len(list) != 0 {
		t.Errorf("AC-1 init wrote human_gates.pre_commit_pause_paths as %s, want []", raw)
	}
}

// AC-1: doctor refuses a glob that does not parse, naming the entry, and takes
// a list that parses as a setting it knows.
func TestAC1DoctorRefusesAPausePathThatDoesNotParse(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "human_gates", map[string]any{
		"pre_commit_pause_tiers": []string{"high"}, "pre_commit_pause_paths": []string{"internal/[hook/**"},
	})
	r := run(t, "doctor")
	out := r.stdout + r.stderr
	if r.code == 0 {
		t.Errorf("AC-1 doctor passed a glob that does not parse:\n%s", out)
	}
	if !strings.Contains(out, "human_gates.pre_commit_pause_paths[0]") || !strings.Contains(out, "SDLC-E0045") {
		t.Errorf("AC-1 doctor does not name human_gates.pre_commit_pause_paths[0] under SDLC-E0045:\n%s", out)
	}

	setConfigOnDisk(t, root, "human_gates", map[string]any{
		"pre_commit_pause_tiers": []string{"high"}, "pre_commit_pause_paths": []string{cpHookGlob},
	})
	if out := run(t, "doctor").stdout; strings.Contains(out, "settings sdlc does not know") {
		t.Errorf("AC-1 doctor reads human_gates.pre_commit_pause_paths as a setting it does not know:\n%s", out)
	}
}
