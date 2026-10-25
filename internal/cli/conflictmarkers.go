package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// markerPlace is where the check for the markers of an unresolved conflict ran,
// which decides what a person is told to do: the commit gate and the stop read
// the list of paths where the markers are meant from different copies of the
// configuration, and each is run again by a different command.
type markerPlace struct {
	// again is the command a person runs once the refusal is dealt with.
	again string
	// listing is how an entry in paths.conflict_markers_intended reaches the
	// copy of the configuration that this place reads.
	listing string
}

// atTheStop is the check in `sdlc stop`, which reads trunk's committed
// configuration: a change to .sdlc/ cannot ride a story, so a person's entry
// goes in a commit to trunk, which the stop puts the story onto.
func atTheStop(trunk, in string) markerPlace {
	return markerPlace{
		again: `"sdlc stop" in ` + in,
		listing: `lists the file in "paths.conflict_markers_intended" in .sdlc/config.json in a commit to ` + trunk +
			`, because "sdlc stop" reads ` + trunk + `'s copy of it and not the story's, and then runs "sdlc stop" in ` +
			in + ` again`,
	}
}

// atTheGate is the check in the commit gate of a story in its own worktree,
// which reads this checkout's configuration, the stop's being the authority:
// the entry reaches this checkout when the story is brought onto the commit.
func atTheGate(trunk string) markerPlace {
	return markerPlace{
		again: `"sdlc gate commit pass"`,
		listing: `lists the file in "paths.conflict_markers_intended" in .sdlc/config.json in a commit to ` + trunk +
			`, because "sdlc stop" reads ` + trunk + `'s copy of it and not the story's, brings the story onto that ` +
			`commit with "git rebase ` + shellWord(trunk) + `" in this checkout, which this gate reads, and runs ` +
			`"sdlc gate commit pass" again`,
	}
}

// inPlace is the check in the commit gate of a story that has no worktree of
// its own: nothing lands it, and the configuration this checkout holds is the
// one that is read.
func inPlace() markerPlace {
	return markerPlace{
		again: `"sdlc gate commit pass"`,
		listing: `lists the file in "paths.conflict_markers_intended" in this checkout's .sdlc/config.json, ` +
			`commits that, and runs "sdlc gate commit pass" again`,
	}
}

const (
	// markerFilesShown and markerSetsShown bound a refusal, as quoteSome and
	// driverKeysShown bound theirs: the files it names, and the conflicts it
	// gives the lines of for each.
	markerFilesShown = 5
	markerSetsShown  = 3
)

// requireNoConflictMarkers refuses a change from base to tip that adds the
// markers of an unresolved conflict to a file the project does not say is meant
// to hold them. base and tip are full commit hashes.
//
// Nothing resolves such a file once it is on trunk: whatever builds from it or
// reads it gets both sides at once. The commit gate and the stop both ask, and
// the stop is the authority: it reads trunk's configuration for the exempt
// paths, where the commit gate reads the story's own. The commit gate does not
// refuse a diff driver whose key it cannot override, as the stop does before it
// asks, so with git.worktrees off, where nothing lands through the stop, such a
// key can still change which lines git calls added.
func requireNoConflictMarkers(ctx context.Context, root, base, tip string, intended config.Paths, at markerPlace) error {
	scan, err := gitx.ConflictMarkersOutside(ctx, root, base, tip, intended.IntendsConflictMarkers)
	if err != nil {
		return err
	}
	// A scan that stopped short is not one that found nothing: a file of
	// pairs that might be conflicts can use up what it reads, and a real one in
	// another file be left unseen.
	if len(scan.Sets) == 0 && scan.Beyond == 0 {
		return nil
	}
	slog.Debug("conflict markers found", "sets", len(scan.Sets), "beyond", scan.Beyond)

	return sdlcerr.New(sdlcerr.ConflictMarkersAdded,
		"the story's commits add the markers of an unresolved conflict: "+describeMarkerSets(scan),
		"an unresolved conflict is what git leaves in a file it could not merge: a line that begins with "+
			markerRun('<')+" and a space, optionally a line that begins with "+markerRun('|')+" and a space, a line "+
			"that is "+markerRun('=')+" and nothing else, and a line that begins with "+markerRun('>')+" and a space, "+
			"in that order among the lines the story added to one file. Nothing resolves the file once it is on trunk, "+
			"and whatever builds from it or reads it gets both sides of the conflict at once").
		WithFix(`a person puts each file named right -- resolves the unresolved conflict in it, or takes out the lines ` +
			`that are not one -- commits, and runs ` + at.again + ` again; where the lines are meant to be there, a ` +
			`person ` + at.listing + `; ` + assistantEscalates + `: it does not resolve the conflict and does not ` +
			`edit that list`)
}

// markerRun is a marker's seven characters, built here so that this file holds
// no line that is one.
func markerRun(c byte) string { return strings.Repeat(string(c), 7) }

// describeMarkerSets names the files, and for each the lines of its first few
// conflicts, with a count of what is left out. A line is never echoed: the file
// is the story's text, and a marker's label can say anything.
func describeMarkerSets(scan gitx.MarkerScan) string {
	var files []string
	for i := 0; i < len(scan.Sets); {
		j := i + 1
		for j < len(scan.Sets) && scan.Sets[j].Path == scan.Sets[i].Path {
			j++
		}
		files = append(files, describeFile(scan.Sets[i:j]))
		i = j
	}
	shown := files[:min(len(files), markerFilesShown)]
	out := strings.Join(shown, ", ")
	if left := len(files) - len(shown); left > 0 {
		out += "; and " + strconv.Itoa(left) + " more files"
	}
	switch {
	case scan.Beyond > 0 && out == "":
		// The scan keeps a bounded number: what it read past is counted, not named.
		out = "at least " + strconv.Itoa(scan.Beyond) + " lines that may be the start of one, too many to check, " +
			"so no file is named"
	default:
		// What the scan could not check is not a conflict it found: it is said apart.
		if counted := scan.Beyond - scan.Unchecked; counted > 0 {
			out += "; and at least " + strconv.Itoa(counted) + " more conflicts that were not counted one by one"
		}
		if scan.Unchecked > 0 {
			out += "; and " + strconv.Itoa(scan.Unchecked) + " more places that may be conflicts and could not be checked"
		}
	}
	return out
}

// describeFile is one file's conflicts: `"notes.txt" (conflict 1: lines 3, 5,
// 7; conflict 2: lines 12, 14, 16; and 297 more)`.
func describeFile(sets []gitx.MarkerSet) string {
	var parts []string
	for i, set := range sets[:min(len(sets), markerSetsShown)] {
		lines := []string{strconv.Itoa(set.Open)}
		if set.Base > 0 {
			lines = append(lines, strconv.Itoa(set.Base))
		}
		lines = append(lines, strconv.Itoa(set.Separator), strconv.Itoa(set.Close))
		parts = append(parts, fmt.Sprintf("conflict %d: lines %s", i+1, strings.Join(lines, ", ")))
	}
	if left := len(sets) - markerSetsShown; left > 0 {
		parts = append(parts, "and "+strconv.Itoa(left)+" more")
	}
	return sdlcerr.QuotePath(sets[0].Path) + " (" + strings.Join(parts, "; ") + ")"
}

// requireNoMarkersFrom is the commit gate's look for a story with no worktree
// of its own, where nothing lands through the stop and this is the only one.
// started is the record's CommitBase as it stood before this pass placed the
// commits: HEAD when code review first passed, or the start of the range an
// earlier pass recorded, so the oldest the record holds. began is the record's
// Base as it stood then, before this pass moved it to HEAD.
//
// A record that names none, because the first code review had no commit to
// name or an older sdlc wrote it, or one that names a commit git cannot read,
// which the commit gate has never refused over (windowOf), is measured from
// began instead: HEAD where the iteration started, or where the last gate
// recorded after code review measured from (moveBase). It can be before commits
// that others made on trunk in between, so the story answers for markers they
// committed, and it can be after the story's own commits, whose markers are
// then not seen; the docs name both as limits. Only where neither can be read
// is the check skipped, and the log says so. Git failing to run, or the context
// ending, is not a commit it cannot read: that is an error, and the gate does
// not pass on it.
func (g *gateRun) requireNoMarkersFrom(ctx context.Context, started, began, head string) error {
	for _, from := range []string{started, began} {
		if from == "" {
			continue
		}
		base, err := gitx.Resolve(ctx, g.s.Root(), from)
		if err == nil {
			return requireNoConflictMarkers(ctx, g.s.Root(), base, head, g.s.Config().Paths, inPlace())
		}
		// Resolve reads a git killed by the context as a name it does not know,
		// so the context is asked first.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !gitx.NamesNoCommit(err) {
			return err
		}
	}
	slog.Debug("conflict markers not looked for: the record names no commit the story started from that git can read",
		"story", g.id)
	return nil
}
