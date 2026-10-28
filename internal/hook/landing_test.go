package hook

// US-029.3 AC-9 through the hook: while a session holds a story, no command of
// its moves trunk or a story branch -- its own or another story's -- other than
// by its own gated commit, from the story's worktree or the main checkout,
// however the session was opened. The one move of its own branch that is its
// own (the person's answer to OQ-3) is a rebase onto trunk's current tip after
// the commit gate; before it, and onto anything else, it is refused. Route R1
// adds one more commit of its own: CODEMAP.md alone, after the retro.
//
// The layout is swCheckouts': A-1 held by swHeld in .sdlc/worktrees/A-1 on
// story/A-1, B-2 held by swOther on story/B-2, trunk "main" checked out in the
// main checkout. Each row is judged from each of the three openings, and a
// refusal is held to saying what to do instead; the rule is left to the plan.
// The tables below are the evasions the criterion was tried against, by the
// class THREATS T1 and T2 name them.
//
// Helpers carry an lh prefix, for landing in the hook.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// lhCommitted commits A-1's work on story/A-1: its frozen test, a change to
// invoice.go and the project's CODEMAP.md.
func lhCommitted(t *testing.T, l swLayout) {
	t.Helper()
	write(t, l.wt, "invoice.go", "package invoice\n\n// Invoice is A-1's.\ntype Invoice struct{}\n")
	write(t, l.wt, "CODEMAP.md", "# CODEMAP\n")
	wtGit(t, l.wt, "add", "--", "invoice.go", "invoice_test.go", "CODEMAP.md")
	wtGit(t, l.wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "A-1")
}

// lhRecordThrough records A-1's gates as passed up to and including last, with
// the reviews stamped against A-1's worktree as it stands.
func lhRecordThrough(t *testing.T, l swLayout, last model.Gate) {
	t.Helper()
	cfg, err := config.Load(l.wt)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: l.wt, Config: cfg}).ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.Gates {
		record.SetGate(g, model.GatePass, "", at)
		if g == last {
			break
		}
	}
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			record.AddReview(model.Review{Gate: gate, Role: r.Role, Verdict: model.VerdictApprove, Subject: tree}, at)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, l.main, ".sdlc/stories/A-1/gate-record.json", string(raw))
}

// lhFinished is A-1 with every gate passed, the retro included, and its work
// committed on story/A-1: the window before landing, where AC-9 matters most.
func lhFinished(t *testing.T) swLayout {
	t.Helper()
	l := swCheckouts(t)
	lhCommitted(t, l)
	lhRecordThrough(t, l, model.GateRetro)
	return l
}

// lhRows is each command from each opening, with {main}, {wt} and {other}
// spelled as the checkouts' paths.
func lhRows(l swLayout, powerShell bool, commands ...string) []swRow {
	spell := strings.NewReplacer("{main}", l.main, "{wt}", l.wt, "{other}", l.other)
	var rows []swRow
	for _, o := range swOpenings(l) {
		for _, c := range commands {
			command := spell.Replace(c)
			if powerShell {
				rows = append(rows, swPowerShell(o.name+": "+c, o, command))
			} else {
				rows = append(rows, swBash(o.name+": "+c, o, command))
			}
		}
	}
	return rows
}

// The evasions, by class. Each moves trunk or a story branch, or would.
var (
	lhRefWrites = []string{
		"git update-ref refs/heads/main HEAD",
		"git update-ref refs/heads/main story/A-1",
		"git update-ref refs/heads/story/A-1 main",
		"git update-ref refs/heads/story/B-2 HEAD",
		"git update-ref -d refs/heads/story/B-2",
		"git update-ref --no-deref HEAD main",
		"echo 'update refs/heads/main HEAD' | git update-ref --stdin",
		"git update-ref --stdin <<< 'delete refs/heads/story/B-2'",
		"git -C '{main}' update-ref refs/heads/main story/A-1",
		"git --git-dir='{main}/.git' update-ref refs/heads/main story/A-1",
		"GIT_DIR='{main}/.git' git update-ref refs/heads/main story/A-1",
		"export GIT_DIR='{main}/.git'; git update-ref refs/heads/main story/A-1",
		"cd '{main}' && git update-ref refs/heads/main story/A-1",
		"sh -c 'git update-ref refs/heads/main HEAD'",
		"env git update-ref refs/heads/main HEAD",
		"command git update-ref refs/heads/main HEAD",
		"git -c alias.up=update-ref up refs/heads/main HEAD",
		"git -c 'alias.up=!git update-ref refs/heads/main HEAD' up",
	}
	lhBranchForces = []string{
		"git branch -f main HEAD",
		"git branch --force main story/A-1",
		"git branch -f story/A-1 main",
		"git branch -f story/B-2 HEAD",
		"git branch -D story/B-2",
		"git branch -d story/B-2",
		"git branch --delete --force story/B-2",
		"git branch -M story/A-1 main",
		"git branch -m story/B-2 gone",
		"git branch -C story/A-1 main",
		"git -C '{main}' branch -f main story/A-1",
	}
	lhCheckoutResets = []string{
		"git checkout -B main",
		"git checkout -B story/B-2",
		"git checkout -B story/A-1 main",
		"git switch -C main",
		"git switch --force-create main story/A-1",
		"git -C '{main}' checkout -B main story/A-1",
	}
	lhOtherWorktrees = []string{
		"git checkout --ignore-other-worktrees main",
		"git switch --ignore-other-worktrees main",
		"git switch --ignore-other-worktrees story/B-2",
	}
	lhSymbolicRefs = []string{
		"git symbolic-ref HEAD refs/heads/main",
		"git symbolic-ref HEAD refs/heads/story/B-2",
	}
	lhIntoItself = []string{
		"git push . HEAD:main",
		"git push . HEAD:refs/heads/main",
		"git push . +HEAD:story/B-2",
		"git push . :story/B-2",
		"git push --force . main:story/A-1",
		"git push '{main}' HEAD:main",
		"git push '{main}/.git' HEAD:main",
		"git fetch . HEAD:main",
		"git fetch . story/A-1:main",
		"git fetch --force . main:story/A-1",
		"git fetch '{main}' story/A-1:main",
	}
	lhFetchThenMerge = []string{
		"git pull . main",
		"git -C '{main}' pull . story/A-1",
		"git -C '{main}' pull --ff-only . story/A-1",
	}
	lhResets = []string{
		"git -C '{main}' reset --hard story/A-1",
		"git -C '{main}' reset --soft story/A-1",
		"git -C '{main}' reset story/A-1",
		"cd '{main}' && git reset --hard story/A-1",
		"git reset --hard HEAD~1",
		"git reset --soft main",
		"git reset --keep main",
	}
	lhRebases = []string{
		"git rebase HEAD~1",
		"git rebase story/B-2",
		"git rebase --onto story/B-2 main",
		"git -C '{main}' rebase story/A-1",
	}
	lhMergesInMain = []string{
		"git -C '{main}' merge --ff-only story/A-1",
		"cd '{main}' && git merge story/A-1",
	}
	lhForcedWorktrees = []string{
		"git worktree add -B main '{main}-x'",
		"git worktree add -B story/B-2 '{main}-y' HEAD",
	}
	lhPowerShell = []string{
		"git update-ref refs/heads/main HEAD",
		"git branch -f main HEAD",
		"git push . HEAD:main",
		"& git update-ref -d refs/heads/story/B-2",
		"git -C '{main}' reset --hard story/A-1",
		"$env:GIT_DIR = '{main}/.git'; git update-ref refs/heads/main story/A-1",
	}
)

// AC-9, every class, in the window where it matters most: the story finished
// and not yet landed.
func TestAC9AFinishedStorysSessionMovesNoTrunkOrStoryBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		commands []string
	}{
		{"AC-9 a ref written directly", lhRefWrites},
		{"AC-9 a branch forced, deleted, renamed or copied", lhBranchForces},
		{"AC-9 a checkout or switch that resets a branch", lhCheckoutResets},
		{"AC-9 a branch checked out in another worktree", lhOtherWorktrees},
		{"AC-9 HEAD pointed at another branch", lhSymbolicRefs},
		{"AC-9 a push or fetch into the repository's own refs", lhIntoItself},
		{"AC-9 a fetch that then merges", lhFetchThenMerge},
		{"AC-9 a reset that moves a branch", lhResets},
		{"AC-9 a rebase onto anything but trunk's tip", lhRebases},
		{"AC-9 trunk merged into by the session", lhMergesInMain},
		{"AC-9 a worktree added with a forced branch", lhForcedWorktrees},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := lhFinished(t)
			swRefused(t, swHeld, lhRows(l, false, tc.commands...), "", nil)
		})
	}
	t.Run("AC-9 in PowerShell", func(t *testing.T) {
		l := lhFinished(t)
		swRefused(t, swHeld, lhRows(l, true, lhPowerShell...), "", nil)
	})
}

// AC-9 holds for as long as the session holds the story: with gates 1 to 7
// passed and the commit to make, and with the commit made and the retro to do.
func TestAC9TheSessionMovesNoTrunkOrStoryBranchBeforeTheStoryIsFinishedEither(t *testing.T) {
	t.Parallel()
	sample := []string{
		"git update-ref refs/heads/main HEAD",
		"git branch -f main story/A-1",
		"git branch -D story/B-2",
		"git push . HEAD:main",
		"git checkout --ignore-other-worktrees main",
		"git -C '{main}' reset --hard story/A-1",
	}
	t.Run("AC-9 ready to commit", func(t *testing.T) {
		l := swCheckouts(t)
		swReady(t, l)
		swRefused(t, swHeld, lhRows(l, false, sample...), "", nil)
	})
	t.Run("AC-9 committed, the retro to do", func(t *testing.T) {
		l := swCheckouts(t)
		lhCommitted(t, l)
		lhRecordThrough(t, l, model.GateCommit)
		swRefused(t, swHeld, lhRows(l, false, sample...), "", nil)
	})
}

// OQ-3: the rebase of its own branch onto trunk is the session's only after
// the commit gate. Before it the branch has nothing of the story's to rebase.
func TestAC9AndQ3TheSessionDoesNotRebaseItsBranchBeforeTheCommitGate(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	swReady(t, l)
	swRefused(t, swHeld, []swRow{
		swBash("Q3 in the worktree", swOpenings(l)[0], "git rebase main"),
		swBash("Q3 from the main checkout", swOpenings(l)[1], "git -C '"+l.wt+"' rebase main"),
	}, "", nil)
}

// What reads refs, and the rebase onto trunk's tip the person's answer to OQ-3
// leaves the session, go through: none of them moves a branch the session does
// not own by its own gated work.
func TestAC9AndQ3WhatReadsBranchesAndTheRebaseOntoTrunkGoThrough(t *testing.T) {
	t.Parallel()
	l := lhFinished(t)
	swAllowed(t, swHeld, lhRows(l, false,
		"git branch",
		"git branch --list",
		"git branch -a",
		"git branch -v",
		"git branch --show-current",
		"git branch --contains HEAD",
		"git rev-parse HEAD",
		"git show-ref",
		"git for-each-ref refs/heads",
		"git log --oneline main..HEAD",
		"git merge-base main HEAD",
		"git diff main...HEAD",
		"git symbolic-ref --short HEAD",
		"git status",
		"git worktree list",
	), nil)
	swAllowed(t, swHeld, []swRow{
		swBash("Q3 rebase in the worktree", swOpenings(l)[0], "git rebase main"),
		swBash("Q3 rebase abandoned in the worktree", swOpenings(l)[0], "git rebase --abort"),
		swBash("Q3 rebase from the main checkout, in the worktree", swOpenings(l)[1], "git -C '"+l.wt+"' rebase main"),
		swBash("Q3 rebase with the shell in the worktree", swOpenings(l)[2], "git rebase main"),
	}, nil)
}

// THREATS T14 and route R1: after the retro, the bookkeeper's CODEMAP.md at
// the root is committed on the story's branch while the session still holds
// it -- that file alone, and only once the retro has passed.
func TestT14TheBookkeepersCodemapIsCommittedAloneAfterTheRetroAndNotBefore(t *testing.T) {
	t.Parallel()
	commit := "git add CODEMAP.md && git commit -m 'docs: map A-1'"
	t.Run("R1 after the retro", func(t *testing.T) {
		l := lhFinished(t)
		write(t, l.wt, "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
		swAllowed(t, swHeld, []swRow{
			swBash("R1 in the worktree", swOpenings(l)[0], commit),
			swBash("R1 with the shell in the worktree", swOpenings(l)[2], commit),
		}, nil)
	})
	t.Run("R1 before the retro", func(t *testing.T) {
		l := swCheckouts(t)
		lhCommitted(t, l)
		lhRecordThrough(t, l, model.GateCommit)
		write(t, l.wt, "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
		swRefused(t, swHeld, []swRow{swBash("R1 in the worktree", swOpenings(l)[0], commit)}, "", nil)
	})
	t.Run("R1 with another file", func(t *testing.T) {
		l := lhFinished(t)
		write(t, l.wt, "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
		write(t, l.wt, "invoice.go", "package invoice\n\n// changed after the reviews\n")
		swRefused(t, swHeld, []swRow{
			swBash("R1 in the worktree", swOpenings(l)[0], "git commit -am 'docs: map A-1'"),
		}, "", nil)
	})
}
