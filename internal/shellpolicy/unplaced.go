package shellpolicy

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// This file is what the walk knows after a `cd` it cannot place, and how the
// rules are asked then: from each place the shell could be, and from each
// directory that would make a word a held path. The CDPATH such a `cd`
// searches is read in cdpath.go.

// lostPlace is what is known of a walk that a `cd` lost: the absolute
// directories it could be in (dirs), the part of where it is that the `cd`
// spelt after what it could not place (tail), and, where neither is known,
// the `cd` as written and why it cannot be placed (why), for the refusal,
// with how to let the hook place it where an absolute `cd` is not that
// (fix). anywhere is whether the shell could be in any directory at all, as
// after a `cd` that searches a CDPATH the hook does not read.
//
// The walk read every `cd` it could not place as somewhere unknown, and a
// relative write after one by its name alone, as the project's: so CDPATH,
// which sends `cd .sdlc` into the project from anywhere, let `rm -rf state`
// through, and after `cd -` a fill of `..` was nowhere at all.
type lostPlace struct {
	dirs      []string
	tail      string
	cd, why   string
	fix       string
	unplacing bool
	anywhere  bool
	// searched are the places a CDPATH entry could have sent a lost walk,
	// beside the places it falls back to where no entry holds the directory;
	// asked as dirs are, they never count as knowing where the walk is.
	searched []string
}

// after is what is known of the walk once a `cd` moves it from dir, and
// whether the walk is lost where changedDir places it: given CDPATH, a
// relative `cd` is lost wherever it starts, and a two-word `cd` (zsh's
// `cd old new`) is somewhere its first word does not say.
func (l lostPlace) after(dir string, lost bool, words []string, cdpath searchPath) (lostPlace, bool) {
	to, next, unplaced, lostHere := unplacedCd(words)
	if unplaced {
		return next, lostHere
	}
	switch {
	case to == "~" || strings.HasPrefix(to, "~/"):
		return homeCd(words, to), false
	case strings.HasPrefix(to, "~"):
		return lostPlace{}, false
	case cdpath.unread && searchesCdpath(to):
		return anywhereAfter(words, to, cdpathUnread, cdpathFix), true
	case cdpath.searched() && searchesCdpath(to):
		return l.underCdpath(words, dir, lost, to, cdpath), true
	case climbsPastGlobstar(to):
		// A `**` can be no directories, so the `..` after it climbs from
		// the part before it; the walk is lost, as for any glob, and could
		// be there.
		next := lostPlace{}
		switch {
		case isAbsolute(to):
			next.dirs = []string{climbedGlobstar(to)}
		case !lost:
			// dir is "" at the project's root, which is a place too
			// (underCdpath).
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
		return "", lostPlace{cd: cd, why: "it goes to the home directory, which the command can change", unplacing: true}, true, false
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

// homeCd is what is known of a walk after a `cd` to the home, `~` or a path
// below it: nothing of where the home is, since the command can set HOME
// before it, and what the cd spelt after ~ is still where the walk is below
// it, as after a variable. Taken as the person's home, `cd ~ && rm -rf state`
// was let through with HOME set to .sdlc earlier on the line (US-035,
// verifier round 9).
func homeCd(words []string, to string) lostPlace {
	next := lostPlace{cd: strings.Join(words, " "), why: "it goes to the home directory, which the command can change",
		unplacing: true}
	if rest, ok := strings.CutPrefix(to, "~/"); ok && rest != "" && !strings.ContainsAny(rest, "$*?[`") {
		next.why = "it starts at the home directory, which the command can change"
		next.tail = rest
	}
	return next
}

// homeRooted reports whether from, where a cd left a lost walk, is spelt
// from the person's home: ~ or a path below it, not another user's ~name.
func homeRooted(from string) bool {
	return from == "~" || strings.HasPrefix(from, "~/")
}

// below is a lost walk after a relative `cd`: below each place it could be.
func (l lostPlace) below(to string) lostPlace {
	next := l
	next.dirs, next.searched = joinedAll(l.dirs, to), joinedAll(l.searched, to)
	if l.tail != "" {
		next.tail = path.Join(l.tail, to)
	}
	return next
}

// joinedAll is each of dirs with to joined below it, in a slice of its own.
func joinedAll(dirs []string, to string) []string {
	var out []string
	for _, d := range dirs {
		out = addPlace(out, path.Join(d, to))
	}
	return out
}

// placeKey is how a place is compared: `.`, which path.Join leaves for `a/..`,
// is the project's root, "".
func placeKey(d string) string {
	if d == "." {
		return ""
	}
	return d
}

// addPlace is list with d added, unless the place is in it already. Counted
// with repeats, eight round trips of `cd a && make; cd ..` under one CDPATH
// were refused for more places than the hook follows, where the shell can be
// in two (US-035, verifier round 13). The bound keeps list short, so the scan
// is too.
func addPlace(list []string, d string) []string {
	for _, e := range list {
		if placeKey(e) == placeKey(d) {
			return list
		}
	}
	return append(list, d)
}

// placeCount is how many places a lost walk could be in: its dirs, and each
// searched place that is not one of them.
func (l lostPlace) placeCount() int {
	n := len(l.dirs)
	for _, d := range l.searched {
		if len(addPlace(l.dirs, d)) > len(l.dirs) {
			n++
		}
	}
	return n
}

// judge asks the path rules of a segment once more from each place a lost
// walk could be, and with the tail before each relative word, after the
// reading from nowhere found nothing: a word refused by its own name keeps
// its rule. Where nothing places the walk, a command that fills a directory
// it names relatively is refused, since that directory could be any, one
// above the project among them.
func (l lostPlace) judge(j segmentRules, segment string, run invocation, dir string) (Finding, bool) {
	if f, ok := judgedFrom(j, segment, run, distinctPlaces(l.dirs, l.searched)); ok {
		return f, true
	}
	if l.tail != "" {
		k := j
		k.dir, k.certain = l.tail, false
		if f, ok := k.judge(segment, run); ok {
			return f, true
		}
	}
	if f, ok := judgedFrom(j, segment, run, homeDirs(j)); ok {
		return f, true
	}
	// A walk lost in an absolute glob is placed from the glob, and one spelt
	// from ~ from the home, by checkFillsAbove. One spelt from the person's
	// own home is not placed by it alone: the same command can set HOME first,
	// and `export HOME=$PWD/.sdlc && cd && rm -rf state` took the loop's
	// state away once a bare cd was spelt ~ (US-035, verifier round 9).
	if !l.unplacing || len(l.dirs) > 0 || isAbsolute(dir) || j.from != "" && !homeRooted(j.from) ||
		j.s.Root == "" || len(run.words) == 0 {
		return Finding{}, false
	}
	for _, d := range filledDirs(run, segment) {
		if c := clean(d.dir); !isAbsolute(c) && !strings.HasPrefix(c, "~") {
			return Finding{
				Rule:   ruleAbove,
				Reason: "after `" + bounded(l.cd) + "`, which the hook cannot place because " + l.why + unplacedFillReason,
				Route:  l.fillRoute(),
			}, true
		}
	}
	if f, ok := l.fromHeld(j, segment, run); ok {
		return f, true
	}
	if f, ok := l.globRefusal(j, run); ok {
		return f, true
	}
	return l.anywhereRefusal(j, run)
}

// distinctPlaces is the places of lists, in order, each once: a CDPATH that
// names one directory many times, or an empty entry beside the place a cd
// falls back to, judged every segment as often (US-035, performance review,
// design review round 5).
func distinctPlaces(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range lists {
		for _, d := range l {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	return out
}

// judgedFrom asks the path rules of a segment from each of dirs, as a walk
// placed there.
func judgedFrom(j segmentRules, segment string, run invocation, dirs []string) (Finding, bool) {
	for _, d := range dirs {
		k := j
		k.dir, k.lost, k.from, k.certain = d, false, "", false
		k.away.dir, k.away.lost = d, false
		if f, ok := k.judge(segment, run); ok {
			return f, true
		}
	}
	return Finding{}, false
}

// homeDirs are the places a lost walk spelt from ~ is, as the session's home
// has them: the spelling itself, and the directory of the project it leads to
// on disk, where a `..` after a link climbs from (`cd -P ~/l/.. && rm -rf
// state`). Read only from somewhere unknown, `cd ~/l && rm -f active`, with l
// a link in the home to the loop's state, took it away (US-035, security
// review round 4). The command can set HOME first, so the rules for a cd the
// hook cannot place are asked as well.
func homeDirs(j segmentRules) []string {
	if !homeRooted(j.from) {
		return nil
	}
	dirs := []string{j.from}
	if j.s.Resolve != nil {
		if at := j.m.resolve(j.s, j.from); at != "" && at != "." {
			dirs = append(dirs, at)
		}
	}
	return dirs
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
		f.Route = l.route(f.Route)
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
				Route: l.route("name the files you mean"),
			}, true
		}
	}
	return Finding{}, false
}

// anywhereRefusal refuses a relative write or removal after a `cd` that could
// have left the shell in any directory at all. Held paths are not all a list
// a word can be matched with: judged as after any other unplaced cd,
// `CDPATH+=<the git directory>; cd objects && rm -rf 12` took objects from
// the repository (US-035, security review, design review round 3).
func (l lostPlace) anywhereRefusal(j segmentRules, run invocation) (Finding, bool) {
	if !l.anywhere {
		return Finding{}, false
	}
	// writtenTo reads every operand of a program it does not know as written,
	// so a program that changes no file is asked of its redirects alone:
	// `cd x` and `ls x` after the cd write nothing.
	words := j.redirects
	if changesFiles(run.words) {
		words = writtenOrRemoved(run, j.redirects)
	}
	for _, w := range words {
		c := clean(w)
		if c == "" || isAbsolute(c) || strings.HasPrefix(c, "~") || strings.HasPrefix(c, "-") {
			continue
		}
		return Finding{
			Rule: ruleDirectoryUnknown,
			Reason: "after `" + bounded(l.cd) + "`, which the hook cannot place because " + l.why +
				", the shell could be in any directory, so `" + bounded(graphic(c)) + "` could be an instruction " +
				"file, or in the git directory or the loop's own state, which the hook cannot tell apart",
			Route: l.route("name the files by an absolute path"),
		}, true
	}
	return Finding{}, false
}

// loopStoriesDir is the directory the story records are kept in.
const loopStoriesDir = ".sdlc/stories"

// route adds to a rule's route how to let the hook place the shell, so a
// model does not read "there" as the held directory the reason just named
// (human advocate r4 finding 1, r5 finding 1): `cd` by an absolute path, or
// for a `cd` that is no help after, l's own fix. An absolute path does not
// take a `cd` out of eval (human advocate, design review round 3).
func (l lostPlace) route(route string) string {
	return route + "; or, if the command is meant for another directory, " + l.placing() +
		", so the hook can place the shell"
}

// fillRoute is the route of a fill after l, which could be above the project.
func (l lostPlace) fillRoute() string {
	if l.fix == "" {
		return unplacedFillRoute
	}
	return "spell the directory out with an absolute path, or " + l.fix +
		"; otherwise say what you would copy there and why, and stop"
}

// placing is how to let the hook place the shell after l.
func (l lostPlace) placing() string {
	return cmp.Or(l.fix, "`cd` to it by an absolute path with no variable or glob")
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
		j.s.Frozen, j.storyRecords(firsts), j.stateEntries(firsts))
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

// stateEntries are the names in the loop's state directory, as held paths, so
// that a word could be one of them after a `cd` into the directory itself:
// only its parent was asked, and `cd "$D" && rm -f active`, with D the state
// directory, took it away (US-035, code review round 4). They are the files
// the store keeps there, which a write can make before they exist (US-035,
// red team, design review round 3), and what the disk lists. Where the
// listing came back short, each of the words' first names could be one it
// left out.
func (j segmentRules) stateEntries(firsts map[string]bool) []string {
	if len(firsts) == 0 {
		return nil
	}
	names := model.StateFiles()
	if j.s.disk.Entries != nil {
		l := j.s.disk.Entries(loopStateDir)
		names = append(names, l.Names...)
		if !l.Complete {
			names = append(names, slices.Collect(maps.Keys(firsts))...)
		}
	}
	return statePaths(names)
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
