package shellpolicy

import (
	"slices"
	"strings"
)

// While a story is held, its session moves no branch -- trunk, the story's own
// or another story's -- other than by the story's own gated commit. The
// landing fast-forwards trunk to the reviewed work, and a branch moved under it
// by hand was either work nobody reviewed or another story's lost: `git branch
// -f main HEAD` from a story's worktree put trunk wherever the session liked,
// and `git branch -D story/B-2` took another session's work away with its
// branch. What a git command does to a branch is read here from its words, as
// git will run them; which branch is trunk is not asked, except of the one
// rebase the session may run.

// branchRule is the rule's name.
const branchRule = "branch-through-the-tool"

// branchHold is the branch rule's finding for one call of Inspect, held back
// until every other rule has passed the whole command, and the work done
// reading the command since. A command another rule refuses keeps that rule's
// name: `git branch -f main HEAD; sdlc approve A-1` is an approval refused.
type branchHold struct {
	finding Finding
	found   bool
	work    int
}

// maxHeldWork bounds the reading done after a branch move is found, in units
// charged by run, segment and reading. The rest of a command is read only to
// find a rule that answers before this one; made long enough, it would wear
// the reading out past what Claude Code gives the hook, and the command would
// run unchecked. Past the bound the held finding is the answer.
const maxHeldWork = 1 << 20

// keep holds f, the first branch move found, unless one is held already.
func (h *branchHold) keep(f Finding) {
	if h != nil && !h.found {
		h.finding, h.found = f, true
	}
}

// charge counts n units of work, once a finding is held, and reports whether
// the reading is past the bound.
func (h *branchHold) charge(n int) bool {
	if h == nil || !h.found {
		return false
	}
	h.work += n
	return h.work > maxHeldWork
}

// branch applies the branch rule to one reading of a git command run from dir,
// and holds what it finds. Once a finding is held the rule is not asked again,
// and the run is charged to the bound instead; past it, the held finding is
// returned.
func (j *gitRules) branch(g gitRead, r ran, dir string, e *environment, asGit bool, depth int, defining bool) (Finding, bool) {
	h := j.s.hold
	if h != nil && h.found {
		if h.charge(len(g.args) + len(g.settings)) {
			return h.finding, true
		}
		return Finding{}, false
	}
	if f, ok := j.branchMoves(g, r, dir, e, asGit, depth, defining); ok {
		if h == nil {
			return f, true
		}
		f.Via = j.s.via
		h.keep(f)
	}
	return Finding{}, false
}

// branchMoves reports whether a git command moves a branch of this repository,
// by the class it belongs to. `git worktree add -B` is one, and the worktree
// rule refuses every `worktree add` before this is asked.
func (j *gitRules) branchMoves(g gitRead, r ran, dir string, e *environment, asGit bool, depth int, defining bool) (Finding, bool) {
	if g.sub == "" || g.at >= len(g.args) {
		return Finding{}, false
	}
	rest := g.args[g.at+1:]
	var what string
	switch g.sub {
	case "update-ref", "filter-branch", "fast-import", "send-pack", "receive-pack", "http-push":
		what = "`git " + g.sub + "` writes refs directly"
	case "branch":
		what = branchMovedByBranch(rest)
	case "checkout", "switch":
		what = branchMovedByCheckout(g.sub, rest)
	case "symbolic-ref":
		if symbolicRefWrites(rest) {
			what = "`git symbolic-ref` writing HEAD puts a checkout on another branch, whose next commit " +
				"then moves it"
		}
	case "push", "fetch", "pull":
		what = "`git " + g.sub + "` writes refs, into this repository or from it, and a held story moves none"
	case "remote":
		if firstWord(rest) == "update" {
			what = "`git remote update` fetches, and writes refs into this repository"
		}
	case "reset":
		what = j.branchMovedByReset(g, r, dir, e, rest)
	case "rebase":
		if defining {
			// An alias that rebases is refused where it runs.
			return Finding{}, false
		}
		return j.rebase(g, r, dir, e, asGit, depth)
	}
	if what == "" || !j.ofThisRepository(g, r, dir, e) {
		return Finding{}, false
	}
	return branchFinding(what), true
}

// branchMovedByBranch is how the words of `git branch` move a branch, or
// nothing where they move none.
func branchMovedByBranch(rest []string) string {
	if branchOption(rest, []string{"force", "delete"}, nil, "fdD", "u") {
		return "`git branch` forcing or deleting a branch moves or takes away one that exists"
	}
	if branchOption(rest, []string{"move", "copy"}, nil, "mMcC", "u") {
		return "`git branch` renaming or copying a branch can write over one that exists"
	}
	return ""
}

// branchMovedByCheckout is how the words of `git checkout` or `git switch`,
// whichever sub is, move a branch, or nothing where they move none.
func branchMovedByCheckout(sub string, rest []string) string {
	switch {
	case branchOption(rest, []string{"ignore-other-worktrees"}, nil, "", ""):
		return "`git " + sub + " --ignore-other-worktrees` checks out a branch another worktree has, " +
			"whose next commit then moves it"
	case sub == "checkout" && branchOption(rest, nil, nil, "B", "b"),
		sub == "switch" && branchOption(rest, []string{"force-create"}, []string{"force"}, "C", "c"):
		return "`git " + sub + "` resetting a branch moves one that exists to where it starts it"
	}
	return ""
}

// branchMovedByReset is how a `git reset` moves a branch, or nothing where it
// moves none: one that names a commit, in the checkout a story has committed
// in or in another checkout.
func (j *gitRules) branchMovedByReset(g gitRead, r ran, dir string, e *environment, rest []string) string {
	if !resetMoves(rest) || !j.s.Committed && !j.inAnotherCheckout(g, r, dir, e) {
		return ""
	}
	what := "`git reset` naming a commit moves the branch its checkout has checked out"
	if !j.s.Committed {
		what += ", and this one is another checkout's"
	}
	return what
}

// branchFinding is the rule's refusal of a command that moves a branch.
func branchFinding(what string) Finding {
	return Finding{
		Rule: branchRule,
		Reason: what + "; while a story is held, nothing its session runs moves trunk or a story " +
			"branch, the story's own or another's, other than the story's own gated commit",
		Route: "leave branches to the loop: `sdlc stop` in a finished story's worktree lands it on " +
			"trunk, and a person moves, deletes, pushes or fetches a branch in their own terminal; " +
			"to read one, use `git log`, `git show` or `git diff`",
	}
}

// rebase judges a rebase once the story's gates have passed. Before them the
// commit gate refuses it, and a bare `--abort` or `--quit`, which commit
// nothing, go through as they always have. Before the commit gate there is
// nothing of the story's on its branch to rebase. After it, only the one
// rebase rebaseAllowed describes goes through: `--continue` and `--skip` are
// refused like every other option, because the runbook only ever abandons a
// rebase that stopped.
func (j *gitRules) rebase(g gitRead, r ran, dir string, e *environment, asGit bool, depth int) (Finding, bool) {
	switch {
	case !j.s.CommitReady:
		return Finding{}, false
	case !j.ofThisRepository(g, r, dir, e):
		return Finding{}, false
	case !j.s.Committed:
		if !makesACommit(append([]string{"git"}, g.args...), "rebase") {
			return Finding{}, false
		}
		return Finding{
			Rule: branchRule,
			Reason: "a rebase before the commit gate has passed has nothing of the story's to put on " +
				"trunk, and it moves the story's branch other than by its own gated commit",
			Route: "commit the story's work through the gates first; once the commit gate has passed, " +
				"`git rebase <trunk>` in the story's own worktree is the one rebase its session may run",
		}, true
	case j.rebaseAllowed(g, r, dir, e, asGit, depth):
		return Finding{}, false
	}
	return Finding{
		Rule: branchRule,
		Reason: "this is not the one rebase a held story's session may run: its own branch, in its own " +
			"worktree, onto trunk's branch, with no option but --quiet and no setting or alias, or a bare " +
			"`git rebase --abort` there",
		Route: "run `git rebase <trunk>` in the story's worktree, naming trunk by its branch or as " +
			"refs/heads/<trunk>; abandon a rebase that stops with `git rebase --abort` and hand the story " +
			"to a person with `sdlc escalate`",
	}, true
}

// rebaseAllowed reports whether a rebase is the one a held session may run
// after the commit gate: its own checkout's branch onto trunk's, named by
// trunk's full branch ref or by a short name IsTrunk reads as that ref,
// optionally quiet, with no other option, no setting, no alias and no place
// but a -C into its own checkout; or a bare `git rebase --abort` there. Asked
// of trunk last, because that one is a question for git.
func (j *gitRules) rebaseAllowed(g gitRead, r ran, dir string, e *environment, asGit bool, depth int) bool {
	if asGit || depth > 0 || j.lost || r.chdir != "" || g.aliased || len(g.settings) > 0 ||
		len(pointedAt(g, r, e)) > 0 {
		return false
	}
	for i := 0; i < g.at; i += 2 {
		if g.args[i] != "-C" {
			return false
		}
	}
	words := append([]string{"git"}, g.args...)
	if !ownRepository(words, dir, j.s) || inAnotherCheckout(words, dir, j.s, j.m) {
		return false
	}
	rest := g.args[g.at+1:]
	if len(rest) == 1 && rest[0] == "--abort" {
		return true
	}
	upstream := ""
	for _, a := range rest {
		switch {
		case a == "-q" || a == "--quiet":
		case strings.HasPrefix(a, "-") || upstream != "":
			return false
		default:
			upstream = a
		}
	}
	return upstream != "" && j.isTrunk(upstream)
}

// isTrunk reports whether a name is trunk's branch: its full ref, or its short
// name where git reads that as the ref. A tag or another ref with trunk's short
// name is what git rebases onto first, and a rebase onto it lands something
// else on trunk.
func (j *gitRules) isTrunk(name string) bool {
	if j.s.Trunk == nil {
		return false
	}
	switch trunk := j.s.Trunk(); {
	case trunk == "":
		return false
	case name == "refs/heads/"+trunk:
		return true
	case name != trunk:
		return false
	}
	return j.s.IsTrunk != nil && j.s.IsTrunk(name)
}

// ofThisRepository reports whether a git command works on this repository's
// refs: in any of its checkouts, through its git directory, or somewhere the
// words do not place. Only a repository they place outside it is another's,
// such as a fixture a test made.
func (j *gitRules) ofThisRepository(g gitRead, r ran, dir string, e *environment) bool {
	at, placed := ranIn(r, dir)
	if !placed || j.lost || !placedGit(g.args) {
		return true
	}
	if j.elsewhere(pointedAt(g, r, e), placeDir(r, dir, g), "dt") {
		return true
	}
	words := append([]string{"git"}, g.args...)
	return inThisRepository(words, at, j.s) || inAnotherCheckout(words, at, j.s, j.m)
}

// inAnotherCheckout reports whether a git command works in another checkout of
// this repository, or, where there are others, somewhere the words do not
// place. With one checkout, that somewhere is this one or another repository.
func (j *gitRules) inAnotherCheckout(g gitRead, r ran, dir string, e *environment) bool {
	if j.s.Shared == nil {
		return false
	}
	at, placed := ranIn(r, dir)
	if !placed || j.lost || !placedGit(g.args) {
		return true
	}
	if j.elsewhere(pointedAt(g, r, e), placeDir(r, dir, g), "dt") {
		return true
	}
	return inAnotherCheckout(append([]string{"git"}, g.args...), at, j.s, j.m)
}

// branchOption reports whether a subcommand's words hold one of its options
// that moves a branch: a long option named in long, or abbreviated as git
// takes an abbreviation, unless it is one of own, the subcommand's option by
// that whole name; or a letter in short, read through a bundle of letters up to
// one in valued, which takes the rest of the word as its value. Words after
// `--` are names.
func branchOption(args, long, own []string, short, valued string) bool {
	for _, a := range args {
		switch {
		case a == "--":
			return false
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a[2:], "=")
			if name != "" && prefixOfAny(name, long) && !slices.Contains(own, name) {
				return true
			}
		case strings.HasPrefix(a, "-"):
			if bundleHolds(a[1:], short, valued) {
				return true
			}
		}
	}
	return false
}

// bundleHolds reports whether a bundle of option letters holds one in short,
// read up to the first in valued, which takes the rest of the word as its
// value.
func bundleHolds(letters, short, valued string) bool {
	for _, c := range letters {
		if strings.ContainsRune(short, c) {
			return true
		}
		if strings.ContainsRune(valued, c) {
			return false
		}
	}
	return false
}

// symbolicRefWrites reports whether `git symbolic-ref` is asked to write or
// delete a ref, rather than read one: two names, -m, or -d.
func symbolicRefWrites(args []string) bool {
	names := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			names += len(args) - i - 1
			i = len(args)
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a[2:], "=")
			if name != "" && prefixOfAny(name, []string{"delete"}) {
				return true
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if strings.ContainsAny(a[1:], "dm") {
				return true
			}
		default:
			names++
		}
	}
	return names >= 2
}

// resetMoves reports whether a reset can move the branch its checkout has
// checked out: one naming a commit other than HEAD, with no paths. A reset of
// paths moves no branch, nor does one to HEAD, whatever its mode. One name with
// no `--` is a commit or a path as git finds it, and is read as a commit.
func resetMoves(args []string) bool {
	var names []string
	paths := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			paths = i+1 < len(args)
			i = len(args)
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a[2:], "=")
			if name != "" && prefixOfAny(name, []string{"patch", "pathspec-from-file"}) {
				paths = true
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if strings.ContainsRune(a[1:], 'p') {
				paths = true
			}
		default:
			names = append(names, a)
		}
	}
	if paths || len(names) != 1 {
		return false
	}
	return names[0] != "HEAD" && names[0] != "@"
}

// firstWord is the first word of args that is not an option.
func firstWord(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}
