package shellpolicy

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// lostPlace is what is known of a walk that a `cd` lost: the absolute
// directories it could be in (dirs), the part of where it is that the `cd`
// spelt after what it could not place (tail), and, where neither is known,
// the `cd` as written and why it cannot be placed (why), for the refusal.
//
// The walk read every `cd` it could not place as somewhere unknown, and a
// relative write after one by its name alone, as the project's: so CDPATH,
// which sends `cd .sdlc` into the project from anywhere, let `rm -rf state`
// through, and after `cd -` a fill of `..` was nowhere at all.
type lostPlace struct {
	dirs      []string
	tail      string
	cd, why   string
	unplacing bool
}

// after is what is known of the walk once a `cd` moves it from dir, and
// whether the walk is lost where changedDir places it: given CDPATH, a
// relative `cd` is lost wherever it starts, and a two-word `cd` (zsh's
// `cd old new`) is somewhere its first word does not say.
func (l lostPlace) after(dir string, lost bool, words []string, cdpath string) (lostPlace, bool) {
	to, next, unplaced, lostHere := unplacedCd(words)
	if unplaced {
		return next, lostHere
	}
	switch {
	case strings.HasPrefix(to, "~"):
		return lostPlace{}, false
	case cdpath != "" && !isAbsolute(to) && to != "." && to != ".." &&
		!strings.HasPrefix(to, "./") && !strings.HasPrefix(to, "../"):
		return underCdpath(dir, lost, to, cdpath), true
	case climbsPastGlobstar(to):
		// A `**` can be no directories, so the `..` after it climbs from
		// the part before it; the walk is lost, as for any glob, and could
		// be there.
		next := lostPlace{}
		switch {
		case isAbsolute(to):
			next.dirs = []string{climbedGlobstar(to)}
		case !lost && dir != "":
			next.dirs = []string{climbedGlobstar(path.Join(dir, to))}
		}
		return next, false
	case lost && !isAbsolute(to):
		return l.below(to), false
	}
	return lostPlace{}, false
}

// unplacedCd is a `cd` whose words alone leave the walk where nothing is
// known of it, with why, and whether the walk is lost where changedDir
// places it; or, where unplaced is false, the directory it goes to, cleaned.
func unplacedCd(words []string) (to string, l lostPlace, unplaced, lost bool) {
	cd := strings.Join(words, " ")
	switch base(words[0]) {
	case "popd", "pop-location":
		return "", lostPlace{cd: cd, why: "it goes back to a directory the command does not show", unplacing: true}, true, false
	}
	// Not skipOptions, which drops a lone `-` as a wrapper's (`env -`): `cd
	// -` was said to go to the home directory.
	args := words[1:]
	for len(args) > 0 && args[0] != "-" && strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	if len(args) > 1 && strings.EqualFold(args[0], "/d") {
		args = args[1:]
	}
	switch {
	case len(args) == 0:
		return "", lostPlace{cd: cd, why: "it goes to the home directory, which the hook does not place", unplacing: true}, true, false
	case len(args) > 1:
		return "", lostPlace{cd: cd, why: "zsh reads a `cd` of two words as a substitution in the directory it is in", unplacing: true}, true, true
	}
	to = clean(args[0])
	switch {
	case to == "-":
		return "", lostPlace{cd: cd, why: "it goes back to the directory before, which the command does not show", unplacing: true}, true, false
	case strings.HasPrefix(to, "$"):
		// What comes after the variable is still where the walk is below
		// it: after `cd $D/.sdlc`, `rm -rf state` is .sdlc/state.
		next := lostPlace{cd: cd, why: "it starts with a variable, which the hook does not read", unplacing: true}
		if _, rest, ok := strings.Cut(to, "/"); ok && !strings.ContainsAny(rest, "$*?[`") {
			next.tail = rest
		}
		return "", next, true, false
	}
	return to, lostPlace{}, false, false
}

// underCdpath is where a relative `cd` given CDPATH could be: the shell
// looks for the directory under each entry first, and only then where it is.
func underCdpath(dir string, lost bool, to, cdpath string) lostPlace {
	next := lostPlace{tail: to}
	for entry := range strings.SplitSeq(cdpath, ":") {
		switch {
		case isAbsolute(entry):
			next.dirs = append(next.dirs, path.Join(entry, to))
		case !lost && dir != "":
			next.dirs = append(next.dirs, path.Join(dir, entry, to))
		}
	}
	if !lost && dir != "" {
		next.dirs = append(next.dirs, path.Join(dir, to))
	}
	return next
}

// below is a lost walk after a relative `cd`: below each place it could be.
func (l lostPlace) below(to string) lostPlace {
	next := l
	for i, d := range l.dirs {
		if i == 0 {
			next.dirs = nil
		}
		next.dirs = append(next.dirs, path.Join(d, to))
	}
	if l.tail != "" {
		next.tail = path.Join(l.tail, to)
	}
	return next
}

// cdpath is the CDPATH a `cd` in segment searches: set in front of it, set
// or exported earlier on the line, or in the environment the session
// started in. Read anywhere on the line, the refusing direction.
func (w *pathWalk) cdpath(segment string) string {
	for _, text := range []string{segment, w.command} {
		for f := range strings.FieldsSeq(text) {
			if v, ok := strings.CutPrefix(f, "CDPATH="); ok && strings.Trim(v, `"'`) != "" {
				return strings.Trim(v, `"'`)
			}
		}
	}
	if w.s.Carried != nil {
		return w.s.Carried("CDPATH")
	}
	return ""
}

// judge asks the path rules of a segment once more from each place a lost
// walk could be, and with the tail before each relative word, after the
// reading from nowhere found nothing: a word refused by its own name keeps
// its rule. Where nothing places the walk, a command that fills a directory
// it names relatively is refused, since that directory could be any, one
// above the project among them.
func (l lostPlace) judge(j segmentRules, segment string, run invocation, dir string) (Finding, bool) {
	for _, d := range l.dirs {
		k := j
		k.dir, k.lost, k.from, k.certain = d, false, "", false
		k.away.dir, k.away.lost = d, false
		if f, ok := k.judge(segment, run); ok {
			return f, true
		}
	}
	if l.tail != "" {
		k := j
		k.dir, k.certain = l.tail, false
		if f, ok := k.judge(segment, run); ok {
			return f, true
		}
	}
	// A walk lost in an absolute glob is placed from the glob, and one spelt
	// from ~ from the home, by checkFillsAbove.
	if !l.unplacing || len(l.dirs) > 0 || isAbsolute(dir) || j.from != "" || j.s.Root == "" || len(run.words) == 0 {
		return Finding{}, false
	}
	for _, d := range filledDirs(run, segment) {
		if c := clean(d.dir); !isAbsolute(c) && !strings.HasPrefix(c, "~") {
			return Finding{
				Rule:   ruleAbove,
				Reason: "after `" + bounded(l.cd) + "`, which the hook cannot place because " + l.why + unplacedFillReason,
				Route:  unplacedFillRoute,
			}, true
		}
	}
	if f, ok := l.fromHeld(j, segment, run); ok {
		return f, true
	}
	return l.globRefusal(j, run)
}

// fromHeld asks the path rules from each directory of the project a relative
// word could be a held path from: the walk could be in any directory, so a
// word is judged from each. By its name alone, `cd - && rm -rf state` took
// .sdlc/state away.
func (l lostPlace) fromHeld(j segmentRules, segment string, run invocation) (Finding, bool) {
	for _, hd := range j.heldFrom(run, l.tail) {
		k := j
		k.dir, k.lost, k.from, k.certain = path.Join(j.s.Root, hd.dir), false, "", false
		k.away.dir, k.away.lost = k.dir, false
		f, ok := k.judge(segment, run)
		if !ok {
			continue
		}
		// Name the operand the command acts on, under the held directory as a
		// project-relative path, not an option word nor the absolute path
		// path.Join left (architect r5 #2-3, red team r5 #2). Only the shown
		// word changes, in this branch; the decision stands.
		if operand, named := operandUnder(hd, run, j.redirects); named {
			f.Reason = namedOperand(f.Reason, operand)
		}
		f.Reason = "after `" + bounded(l.cd) + "`, which the hook cannot place because " + l.why +
			", the command could be in " + hd.dir + ", where " + f.Reason
		if hd.dir == loopStoriesDir {
			f.Reason += j.storiesShortClause()
		}
		f.Route = unplacedCdRoute(f.Route)
		return f, true
	}
	return Finding{}, false
}

// globRefusal refuses a write or removal whose first name is a glob, after a
// `cd` the hook cannot place: the glob could match an instruction file or the
// loop's own state in whatever directory the shell is in, which the hook
// cannot tell apart, so the ambiguous write refuses (red team r4 finding 1). A
// word with an explicit directory, even a variable one (`$D/*`), is not this:
// its ambiguity is the directory, which a placed command has too and the story
// does not change.
func (l lostPlace) globRefusal(j segmentRules, run invocation) (Finding, bool) {
	for _, w := range writtenOrRemoved(run, j.redirects) {
		c := clean(w)
		if isAbsolute(c) || strings.HasPrefix(c, "~") || strings.HasPrefix(c, "-") {
			continue
		}
		if first, _, _ := strings.Cut(c, "/"); strings.ContainsAny(first, "*?[") {
			return Finding{
				Rule: ruleDirectoryUnknown,
				Reason: "after `" + bounded(l.cd) + "`, which the hook cannot place because " + l.why +
					", `" + bounded(graphic(first)) + "` could match an instruction file or the loop's own " +
					"state in whatever directory the shell is in, which the hook cannot tell apart",
				Route: unplacedCdRoute("name the files you mean"),
			}, true
		}
	}
	return Finding{}, false
}

// loopStoriesDir is the directory the story records are kept in.
const loopStoriesDir = ".sdlc/stories"

// unplacedCdRoute adds to a rule's route how to place the shell, so a model
// does not read "there" as the held directory the reason just named (human
// advocate r4 finding 1, r5 finding 1).
func unplacedCdRoute(route string) string {
	return route + "; or, if the command is meant for another directory, `cd` to it by an absolute path with no " +
		"variable or glob, so the hook can place the shell"
}

// storiesShortClause says, where the listing of .sdlc/stories could not be
// read in full, that any name there could be a story's record, so the person
// is not told a name they did not write is one (verifier gap 2, human advocate
// r2 note 1). The cause is the listing's own, which the hook words so it does
// not blame .sdlc/stories for entries another directory listed (red team r5,
// verifier r4 gap 2).
func (j segmentRules) storiesShortClause() string {
	l := j.storiesListing()
	if l.Complete {
		return ""
	}
	return "; the list of stories in .sdlc/stories could not be read in full (" +
		cmp.Or(graphic(l.Cause), "the listing failed") + "), so any name there could be a story's record"
}

// storiesListing is the disk's listing of the story records, or an empty one
// where there is no disk to ask.
func (j segmentRules) storiesListing() Listing {
	if j.s.disk.Stories == nil {
		return Listing{Complete: true}
	}
	return j.s.disk.Stories()
}

// writtenOrRemoved are the words a command writes or takes away, with its
// redirect targets: what a glob after an unplaced `cd` would match. find's
// own filters (`-name '*.o'`) are left out, since they are not words it
// writes.
func writtenOrRemoved(run invocation, redirects []string) []string {
	return slices.Concat(redirects, writtenTo(run.words), takenAway(run.words))
}

// operandUnder is the first word a command acts on whose first name selected
// the held directory, as a project-relative path under it: the operand a
// refusal from that directory should name rather than an option word.
func operandUnder(hd heldDir, run invocation, redirects []string) (string, bool) {
	for _, w := range slices.Concat(run.words[1:], redirects) {
		c := strings.TrimPrefix(clean(w), "./")
		if c == "" || isAbsolute(c) || strings.HasPrefix(c, "~") || strings.HasPrefix(c, "-") {
			continue
		}
		if first, _, _ := strings.Cut(c, "/"); pathrules.Fold(first) == hd.first {
			return hd.dir + "/" + c, true
		}
	}
	return "", false
}

// namedOperand is a reason with the path it names at its start replaced by
// operand, up to the " is " that follows it. Every held-file reason begins
// "<path> is ...", so only the word shown changes.
func namedOperand(reason, operand string) string {
	if i := strings.Index(reason, " is "); i >= 0 {
		return operand + reason[i:]
	}
	return reason
}

// heldFrom are the directories of the project from which a relative word of
// the segment is, or is inside, a held path (the loop's own, the backlog, a
// frozen test or a story's record): each directory a held path names whose
// next name is the word's first, and that ends with tail, what
// the `cd` spelt after its variable. A held directory itself is not one, or
// every name written after a `cd` the hook cannot place would be refused as
// inside .sdlc/state.
func (j segmentRules) heldFrom(run invocation, tail string) []heldDir {
	firsts := map[string]bool{}
	for _, w := range slices.Concat(run.words[1:], j.redirects) {
		if c := strings.TrimPrefix(clean(w), "./"); c != "" && !isAbsolute(c) && !strings.HasPrefix(c, "~") && !strings.HasPrefix(c, "-") {
			first, _, _ := strings.Cut(c, "/")
			firsts[pathrules.Fold(first)] = true
		}
	}
	var dirs []heldDir
	held := slices.Concat([]string{loopStateDir, loopConfigFile, ".sdlc/claude-progress.json", j.s.Backlog},
		j.s.Frozen, j.storyRecords(firsts))
	for _, h := range held {
		segs := strings.Split(h, "/")
		for i := 1; i < len(segs); i++ {
			d, first := strings.Join(segs[:i], "/"), pathrules.Fold(segs[i])
			here := heldDir{dir: d, first: first}
			if firsts[first] && (tail == "" || d == tail || strings.HasSuffix(d, "/"+tail)) && !slices.Contains(dirs, here) {
				dirs = append(dirs, here)
			}
		}
	}
	return dirs
}

// A heldDir is a directory of the project a relative word could be a held
// path from, with the folded first name that selected it, so a refusal can
// name the operand rather than an option word.
type heldDir struct {
	dir   string
	first string
}

// storyRecords are the story directories, as held paths, that the disk
// lists in .sdlc/stories. Any single name there is a story's, so a story is
// told from any other name by what is on disk: held by the name alone,
// `cd "$D" && rm -rf build` would be refused as a story called build, and
// left out, `cd - && rm -rf A-1` took a story's record. Where the listing
// came back short, each of the words' first names could be one it left
// out. Without a disk to ask, none is.
func (j segmentRules) storyRecords(firsts map[string]bool) []string {
	if j.s.disk.Stories == nil {
		return nil
	}
	l := j.s.disk.Stories()
	names := l.Names
	if !l.Complete {
		names = slices.Collect(maps.Keys(firsts))
	}
	records := make([]string, 0, len(names))
	for _, n := range names {
		records = append(records, ".sdlc/stories/"+n)
	}
	return records
}
