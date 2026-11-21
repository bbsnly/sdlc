package config

import (
	"slices"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// PathMatch is a file the story changes and the glob of
// human_gates.pre_commit_pause_paths that it matched.
type PathMatch struct {
	File string
	Glob string
}

// CommitPause is whether the commit gate waits for a person, and why: the
// story's tier, the files its change touches that a listed glob matches, and
// anything that could not be read to tell. The gate, the hook, the landing and
// status all decide from one of these, so that none of them can say a pause
// is off that another holds.
type CommitPause struct {
	Tier       string
	TierPauses bool
	// Globs is how many globs the configurations list between them.
	Globs   int
	Matches []PathMatch
	// Unread says what could not be read, where the decision rests on
	// something nobody could read: a configuration, the backlog, or git. It
	// pauses, so that a failure is never read as no pause.
	Unread string
	// more is how many matches Capped left out of Matches.
	more int
}

// Pauses reports whether the commit waits for a person's approval.
func (p CommitPause) Pauses() bool {
	return p.TierPauses || len(p.Matches) > 0 || p.more > 0 || p.Unread != ""
}

// With is h with other's lists added: a configuration taken together with
// another can only add to what pauses, never take a pause away.
func (h HumanGates) With(other HumanGates) HumanGates {
	out := h
	out.PreCommitPauseTiers = union(h.PreCommitPauseTiers, other.PreCommitPauseTiers)
	out.PreCommitPausePaths = union(h.PreCommitPausePaths, other.PreCommitPausePaths)
	out.DoRAdvocateCheck = h.DoRAdvocateCheck || other.DoRAdvocateCheck
	return out
}

// union is a's entries, then each of b's that a does not hold.
func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// PauseFor decides the pause for a story of tier whose change touches the
// slash paths changed. Each path, in sorted order, is named with the first
// glob that matches it.
func (h HumanGates) PauseFor(tier string, changed []string) CommitPause {
	tier = strings.TrimSpace(tier)
	if tier == "" {
		tier = DefaultRiskTier
	}
	p := CommitPause{Tier: tier, TierPauses: h.PausesBeforeCommit(tier), Globs: len(h.PreCommitPausePaths)}
	if p.Globs == 0 {
		return p
	}
	paths := slices.Clone(changed)
	slices.Sort(paths)
	for _, path := range slices.Compact(paths) {
		for _, glob := range h.PreCommitPausePaths {
			if PauseGlobMatches(glob, path) {
				p.Matches = append(p.Matches, PathMatch{File: path, Glob: glob})
				break
			}
		}
	}
	return p
}

// PauseGlobMatches reports whether a glob of pre_commit_pause_paths matches
// the slash path rel. Each form only adds matches, since a pause matched too
// widely asks a person once more and one matched too narrowly lets a change
// past them: exactly, then folded, as macOS and Windows read a name
// (pathrules.Fold: case, Unicode and the NTFS spellings, THREATS T4), and
// with a bare directory taken to mean what is under it too, so that
// "internal/hook" pauses on internal/hook/hook.go as a person meant.
func PauseGlobMatches(glob, rel string) bool {
	folded, foldedRel := pathrules.Fold(glob), pathrules.Fold(rel)
	for _, g := range []string{glob, glob + "/**"} {
		if MatchGlob(g, rel) {
			return true
		}
	}
	for _, g := range []string{folded, folded + "/**"} {
		if MatchGlob(g, foldedRel) {
			return true
		}
	}
	return false
}

// Capped is p with at most n of its matches kept, and the rest counted, for a
// refusal whose text has to stay bounded. It pauses where p does.
func (p CommitPause) Capped(n int) CommitPause {
	if len(p.Matches) <= n {
		return p
	}
	out := p
	out.more += len(p.Matches) - n
	out.Matches = slices.Clone(p.Matches[:n])
	return out
}

// Why is why the commit waits, for a refusal, or "" where it does not.
func (p CommitPause) Why() string {
	var clauses []string
	if p.TierPauses {
		clauses = append(clauses, model.TierWaits(p.Tier))
	}
	if path := p.pathClause(); path != "" {
		clauses = append(clauses, path)
	}
	if p.Unread != "" {
		clauses = append(clauses, p.unreadClause())
	}
	return strings.Join(clauses, "; and ")
}

// pathClause names each matching file with its glob. Every name is quoted, on
// one line: a file name is the session's to choose, and it is read back to the
// session inside sdlc's own refusal.
func (p CommitPause) pathClause() string {
	if len(p.Matches) == 0 && p.more == 0 {
		return ""
	}
	pairs := make([]string, 0, len(p.Matches)+1)
	for _, m := range p.Matches {
		pairs = append(pairs, strconv.Quote(m.File)+" matches "+strconv.Quote(m.Glob))
	}
	if p.more > 0 {
		pairs = append(pairs, "and "+strconv.Itoa(p.more)+" more, which `sdlc status` names")
	}
	return "the change touches paths that wait for a person's approval before it is committed: " +
		strings.Join(pairs, "; ")
}

func (p CommitPause) unreadClause() string {
	return "what pauses the story could not all be read (" + strconv.Quote(p.Unread) +
		"), so whether its change waits for a person cannot be told, and it waits: " +
		"`sdlc doctor` names a configuration at fault, and a person's approval of the work answers it"
}

// Reasons is each reason the commit does or does not pause, one clause each,
// for `sdlc status`.
func (p CommitPause) Reasons() []string {
	var out []string
	if p.TierPauses {
		out = append(out, "the story's risk tier is "+p.Tier+", which pre_commit_pause_tiers pauses")
	} else {
		out = append(out, "the story's risk tier is "+p.Tier+", which pre_commit_pause_tiers does not pause")
	}
	switch {
	case len(p.Matches) > 0:
		for _, m := range p.Matches {
			out = append(out, strconv.Quote(m.File)+" matches "+strconv.Quote(m.Glob))
		}
	case p.Globs == 0:
		out = append(out, "pre_commit_pause_paths lists no path")
	case p.Unread == "":
		out = append(out, "no file the story changes matches pre_commit_pause_paths")
	}
	if p.Unread != "" {
		out = append(out, p.unreadClause())
	}
	return out
}

// NoPause is the message of the landed_without_pause event: what was asked,
// and that nothing paused.
func (p CommitPause) NoPause() string {
	return "the commit gate passed with no pause for a person: " + strings.Join(p.Reasons(), "; ")
}

// WaitsIn is why the work as tree has it waits for a person in r, or "" where
// a person has approved it. Where the answer is that the work changed since a
// person decided, or was sent back, the files that pause it are named too, so
// that every refusal of a path's pause says which.
func (p CommitPause) WaitsIn(r *model.Record, tree string) string {
	why := p.Why()
	got := r.WaitsForApprovalBecause(why, tree)
	if got == "" || got == why {
		return got
	}
	if path := p.pathClause(); path != "" {
		got += "; " + path
	}
	return got
}
