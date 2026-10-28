package shellpolicy

import (
	"cmp"
	"path"
	"slices"
	"strings"
)

// Listing is what the hook read of a directory's names or an archive's
// members. Complete is whether it read to the end cleanly: a listing cut at a
// bound, or one that failed part way, could have left out the one name that
// matters, so it is never read as though nothing more were there. Missing is
// whether the path does not exist. Cause is why a read stopped short, in words
// a refusal quotes, or "" where an archive is in no format the reader reads.
type Listing struct {
	Names             []string
	Complete, Missing bool
	Cause             string
	// Format is the archive format the hook read the file as ("zip", "tar",
	// "tar.gz", "tar.bz2"), "" where it read it as none, or for a directory
	// listing. An extractor that reads by name (unzip) needs it: a file read
	// as another format is the extractor's fall-back, not the archive it was
	// given. The hook's reader sets it on every complete archive exit.
	Format string
}

// written is what a command writes, as writtenTo reads it from the words, and
// the landings only the state can place: a copy into a directory whose name
// has a dot, one fed its sources by xargs, the entries a copy's source puts
// under its destination in place of the spelt landing, and the members an
// extraction lands.
func (q pathQuestion) written() []string {
	words := writtenTo(q.run.words)
	if landed, spelt, _ := q.copyFills(); len(spelt) > 0 {
		words = slices.DeleteFunc(words, func(w string) bool { return slices.Contains(spelt, w) })
		words = append(words, landed...)
	}
	members, _ := q.extracted()
	return slices.Concat(words, q.directoryLandings(), q.pipedLandings(), members)
}

// copyNames are the programs whose sources land under a destination each
// under its own name.
var copyNames = map[string]bool{
	"cp": true, "copy": true, "copy-item": true, "cpi": true, "mv": true, "move": true, "move-item": true,
	"mi": true, "ln": true, "install": true,
}

// directoryLandings are the sources of a copy (copyArguments) whose
// destination's name has a dot, landed under it where the state shows it is
// a directory. intoADirectory reads such a name as a file, which a name
// alone cannot tell apart: `cp payload/x_test.go pkg.v2` wrote the frozen
// pkg.v2/x_test.go, and a project kept in me.github.io was a file to `cp
// CLAUDE.md ../me.github.io`, and, once cp's was read, to rsync's and
// scp's. A ln or an install is landed whatever the name.
func (q pathQuestion) directoryLandings() []string {
	words := q.run.words
	if len(words) == 0 {
		return nil
	}
	from, to, target, ok := copyArguments(words)
	if !ok || target || len(from) == 0 || remote(to) || intoADirectory(to, len(from)) || !q.isDirectory(to, from) {
		return nil
	}
	return landedIn(to, from)
}

// isDirectory reports whether the state shows a destination is a directory:
// the project's root, which holds .sdlc, or one above it; one that holds a
// frozen test; the
// root of another checkout; or, asked of the disk only where a source would
// land on a held name in it, one the disk says is a directory. The frozen
// tests are asked only where Resolve is set, since without it a relative
// name is not known to be the project's (cpinto_test.go holds `cp x
// internal/calc.v2` as a file in such a state).
func (q pathQuestion) isDirectory(to string, from []string) bool {
	s, m := q.s, q.m
	if s.Resolve == nil || q.lost {
		return false
	}
	c := clean(to)
	if !isAbsolute(c) && !strings.HasPrefix(c, "~") {
		c = path.Join(q.dir, c)
	}
	// The root, and every directory above it, are directories.
	if m.resolve(s, c+"/.sdlc") == ".sdlc" || m.levelsAbove(s, c) > 0 {
		return true
	}
	if r := m.resolve(s, c); r != "" {
		if _, ok := holding(r, s.Frozen); ok {
			return true
		}
	}
	for _, l := range m.shared(s, c) {
		if l.Rel == "." {
			return true
		}
	}
	return s.disk.IsDir != nil && q.landsHeld(c, from) && m.isDir(s, c)
}

// landsHeld reports whether a source, under its own name in dir, would be a
// name a rule holds: an instruction file or directory, the loop's own, or
// the backlog. A frozen test is not asked after: the directory it would land
// in holds it, and isDirectory has already answered yes through holding.
func (q pathQuestion) landsHeld(dir string, from []string) bool {
	for _, f := range from {
		landed := path.Join(dir, path.Base(clean(f)))
		if _, ok := protectedPath(landed, q.s.Backlog); ok {
			return true
		}
		if _, ok := loopState(landed); ok {
			return true
		}
	}
	return false
}

// isDir is what Disk.IsDir says of a word, asked once per command.
func (m *memo) isDir(s State, word string) bool {
	is, ok := m.dirs[word]
	if !ok {
		is = s.disk.IsDir(word)
		if m.dirs == nil {
			m.dirs = map[string]bool{}
		}
		m.dirs[word] = is
	}
	return is
}

// pipedLandings are the path words of the line landed under the destination
// of a cp, mv, ln or install that xargs feeds: `echo /tmp/p/CLAUDE.md | xargs
// cp -t .` writes ./CLAUDE.md, and the line's words were read only where they
// stand. The destination is known where it is given as an option, or where
// xargs puts each name in place of a word (-I) and the destination is the
// last word.
func (q pathQuestion) pipedLandings() []string {
	to, sources, ok := q.pipedCopy()
	if !ok {
		return nil
	}
	landed := landedIn(to, sources)
	if listed, spelt, _ := q.sourceLandings(to, sources, false); len(spelt) > 0 {
		landed = append(slices.DeleteFunc(landed, func(w string) bool { return slices.Contains(spelt, w) }), listed...)
	}
	return landed
}

// inProject reports whether a directory, as the command spells it, is the
// project's root or in it, by Resolve: the root by the probe levelsAbove
// asks, since Resolve answers "" for the root as for outside. Nothing is
// cleaned first, so a `..` after a link is read where the link leads.
func (q pathQuestion) inProject(dir string) bool {
	if q.s.Resolve == nil {
		return false
	}
	c := strings.TrimSuffix(clean(dir), "/")
	return q.m.resolve(q.s, c+"/.sdlc") == ".sdlc" || c != "" && q.m.resolve(q.s, c) != ""
}

// namedDirectories are the directories a line names for zsh's `~name`: a
// variable given an absolute path, or `hash -d name=path`. Read as user c's
// home, which there is not, `c=<root>; rm -rf ~c/.sdlc/state` was placed
// outside the project.
func namedDirectories(line string) map[string]string {
	if !strings.Contains(line, "~") || !strings.Contains(line, "=") {
		return nil
	}
	var named map[string]string
	for _, f := range strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune(" \t\n;&|()", r) }) {
		if !isAssignment(f) {
			continue
		}
		name, value, _ := strings.Cut(f, "=")
		if v := clean(value); isAbsolute(v) {
			if named == nil {
				named = map[string]string{}
			}
			named[name] = v
		}
	}
	return named
}

// untilded is words with, after each that starts at a named directory, the
// word with the directory spelt out.
func (m *memo) untilded(words []string) []string {
	if len(m.tildes) == 0 {
		return words
	}
	for _, w := range slices.Clip(words) {
		rest, ok := strings.CutPrefix(clean(w), "~")
		if !ok {
			continue
		}
		name, tail, _ := strings.Cut(rest, "/")
		if dir, ok := m.tildes[name]; ok {
			words = append(words, path.Join(dir, tail))
		}
	}
	return words
}

// placedDir is a directory a word names, where the walk puts it: a relative
// one is joined to where the command is.
func (q pathQuestion) placedDir(dir string) string {
	c := clean(dir)
	// clean takes the root's slash off with every other trailing one: placed
	// as "", `rm -f /*` listed the command's own directory in place of /.
	if c == "" && strings.HasPrefix(strings.ReplaceAll(unquote(dir), "\\", "/"), "/") {
		return "/"
	}
	c = cmp.Or(c, ".")
	if isAbsolute(c) || strings.HasPrefix(c, "~") {
		return c
	}
	return path.Join(cmp.Or(q.dir, "."), c)
}

// reachable reports whether a placed directory is the project, in it, or
// above it: where a name in it can be one a rule holds.
func (q pathQuestion) reachable(dir string) bool {
	return q.inProject(dir) || q.m.levelsAbove(q.s, dir) > 0
}

// unlisted is a listing that came back short, with the word of the command
// that asked for it and whether a copy's source asked.
type unlisted struct {
	word    string
	listing Listing
	copied  bool
}

// notListed are the programs whose operands are not what they write: a
// copy's sources are read, and are listed for where they land
// (sourceLandings); an archiver's are members; find's are where it starts.
var notListed = map[string]bool{
	"cp": true, "copy": true, "copy-item": true, "cpi": true, "ln": true, "install": true,
	"rsync": true, "scp": true, "tar": true, "find": true, "curl": true, "wget": true,
}

// listedNames are the entries on disk that a glob in a word the command
// writes or takes away fills in, each under the directory as the word spells
// it, and the listings in or above the project that came back short. A glob's names
// are otherwise matched against the names the rules hold, and a bare `*`
// names none of them: `rm -f *` in the project's root removed its CLAUDE.md,
// and zsh's `echo x > *` wrote it. Only the words as written are listed,
// never a copy's landing: listed, `cp {side}/* .` was the root's `./*`.
func (q pathQuestion) listedNames() (names []string, short []unlisted) {
	run := q.run
	if q.s.disk.Entries == nil || q.lost || len(run.words) == 0 {
		return nil, nil
	}
	// Four rules ask, of the same segment from the same directory.
	key := strings.Join(slices.Concat([]string{q.dir}, q.redirects, run.words), "\x00")
	if l, ok := q.m.listed[key]; ok {
		return l.names, l.short
	}
	defer func() {
		if q.m.listed == nil {
			q.m.listed = map[string]listedGlobs{}
		}
		q.m.listed[key] = listedGlobs{names, short}
	}()
	words := slices.Concat(q.redirects, takenAway(run.words))
	if !notListed[base(run.words[0])] && changesFiles(run.words) {
		words = append(words, writtenTo(run.words)...)
	}
	for _, w := range words {
		found, l, dir, asked := q.globEntries(w)
		names = append(names, found...)
		// Above the project too: kept only for the project, a cut listing
		// of the directory above let `rm -f ../*` past with its
		// CLAUDE.md unnamed.
		if asked && !l.Complete && q.reachable(dir) {
			short = append(short, unlisted{word: w, listing: l})
		}
	}
	return names, short
}

// listedGlobs is what listedNames found for one segment.
type listedGlobs struct {
	names []string
	short []unlisted
}

// globEntries are the names on disk a word with a glob fills in, with the
// listing and the placed directory it was read from, and whether the disk
// was asked: for a glob in the last name, in a directory spelt with none and
// no variable in the project or above it, each entry it matches; and for
// zsh's `***`, which follows links, each entry of the directory before it,
// wherever that is, with the rest of the word after it, so that
// `<outside>/***/.sdlc/state/active` reaches the project through a link in
// <outside>. One level is listed, as the bound on what is read allows.
func (q pathQuestion) globEntries(word string) (names []string, l Listing, placed string, asked bool) {
	dir, glob, rest, ok := globParts(word)
	links := rest != ""
	if !ok || !links && !q.reachable(q.placedDir(dir)) {
		return nil, Listing{}, "", false
	}
	placed = q.placedDir(dir)
	l = q.s.disk.Entries(placed)
	matches := segmentMatcher(glob)
	for _, n := range l.Names {
		switch {
		case n == "." || n == "..":
		case !links && matches(n):
			names = append(names, underDir(dir, graphic(n)))
		case links && !strings.HasPrefix(n, "."):
			names = append(names, underDir(dir, graphic(n)+"/"+rest))
		}
	}
	return names, l, placed, true
}

// globParts is a word cut at its first glob, where the shell's matches of it
// can be listed: the directory before it, spelt with no glob or variable,
// and either a glob that is the last name, or a `***` with rest after it.
func globParts(word string) (dir, glob, rest string, ok bool) {
	c := clean(word)
	segs := strings.Split(c, "/")
	g := slices.IndexFunc(segs, func(seg string) bool { return strings.ContainsAny(seg, "*?[") })
	if g < 0 {
		return "", "", "", false
	}
	dir = strings.Join(segs[:g], "/")
	if dir == "" && strings.HasPrefix(c, "/") {
		dir = "/"
	}
	if strings.ContainsAny(dir, "{$`%") || strings.HasPrefix(dir, "~") && dir != "~" && !strings.HasPrefix(dir, "~/") {
		return "", "", "", false
	}
	switch {
	case g == len(segs)-1:
		return dir, segs[g], "", true
	case segs[g] == "***":
		return dir, segs[g], strings.Join(segs[g+1:], "/"), true
	}
	return "", "", "", false
}

// underDir is name under a directory as a word spells it, where "" is the
// directory the command runs in.
func underDir(dir, name string) string {
	switch dir {
	case "":
		return name
	case "/":
		return "/" + name
	}
	return dir + "/" + name
}

// sourceLandings are where a copy's sources land that put what their
// directory holds under the destination rather than themselves, each entry
// under its own name, in place of the spelt landing (`./*` for `cp p/* .`),
// which names none of them; with the listings that came back short, and the
// spelt landings they stand for. A source fills so where its last name is
// a glob, where it is spelt for its contents (`p/.`, `p/`), or under -T.
// Nothing is listed for a destination outside the project and not above it,
// or with the walk lost, and the spelt landing then stands: copied into the
// root, `cp -r p/. .` put p's CLAUDE.md over the root's with nothing asked.
func (q pathQuestion) sourceLandings(to string, from []string, contents bool) (landed, spelt []string, short []unlisted) {
	if q.s.disk.Entries == nil || q.lost || remote(to) || !q.reachable(q.placedDir(to)) {
		return nil, nil, nil
	}
	for _, f := range from {
		dir, glob, ok := fillOf(f, contents)
		if !ok {
			continue
		}
		l := q.s.disk.Entries(q.placedDir(dir))
		if !l.Complete && q.inProject(q.placedDir(to)) {
			short = append(short, unlisted{word: f, listing: l, copied: true})
		}
		spelt = append(spelt, landedIn(to, []string{f})...)
		matches := segmentMatcher(glob)
		for _, n := range l.Names {
			if n != "." && n != ".." && (glob == "" || matches(n)) {
				landed = append(landed, landedIn(to, []string{graphic(n)})...)
			}
		}
	}
	return landed, spelt, short
}

// fillOf is the directory a copy's source fills its destination from, and
// the glob its entries are matched by, "" for every entry; false for a
// source that lands under its own name.
func fillOf(source string, contents bool) (dir, glob string, ok bool) {
	c := clean(source)
	switch {
	case contents || fillsWithContents(source):
		return c, "", true
	case strings.ContainsAny(path.Base(c), "*?[") && !strings.ContainsAny(path.Dir(c), "*?[{$`%"):
		return path.Dir(c), path.Base(c), true
	}
	return "", "", false
}

// copyFills is sourceLandings for the copy the segment runs, into the
// project: one into a directory above it is checkFillsAbove's, which names
// the fill as the release did.
func (q pathQuestion) copyFills() (landed, spelt []string, short []unlisted) {
	words := q.run.words
	if len(words) == 0 || !copyNames[base(words[0])] {
		return nil, nil, nil
	}
	// Both readings (Step 17): a glob or contents source glued to a bundle
	// ending in t fills its destination in the project too, not only above it.
	for _, r := range copyReadings(base(words[0]), words[1:]) {
		if !q.inProject(q.placedDir(r.to)) {
			continue
		}
		l, s, u := q.sourceLandings(r.to, r.from, noTargetDirectory(words[1:]))
		landed, spelt, short = append(landed, l...), append(spelt, s...), append(short, u...)
	}
	return landed, spelt, short
}

// pipedCopy is the destination of a cp, mv, ln or install that xargs feeds,
// and the path words of the line it can be fed.
func (q pathQuestion) pipedCopy() (to string, sources []string, ok bool) {
	run := q.run
	if !run.piped || len(run.words) == 0 || !copyNames[base(run.words[0])] {
		return "", nil, false
	}
	// The first reading (linkArguments' or bundleTarget's, Step 17) that names
	// a target or takes a `{}` is the one xargs feeds.
	for _, r := range copyReadings(base(run.words[0]), run.words[1:]) {
		if r.target || slices.ContainsFunc(r.from, func(f string) bool { return strings.Contains(f, "{}") }) {
			to, ok = r.to, true
			break
		}
	}
	if !ok {
		return "", nil, false
	}
	for _, w := range q.m.handedWords(q.line) {
		if strings.Contains(w, "/") && !strings.HasPrefix(w, "-") {
			sources = append(sources, w)
		}
	}
	return to, sources, true
}

// extraction is an extracting command as its words have it: the archive it
// reads, "" for standard input; the directories it puts the members in;
// whether -P keeps a member's leading `/` and `..`; and, where the members
// cannot be known without running it, the program or the option that hides
// them.
type extraction struct {
	archive  string
	archives []string // the -f archives of a tar, to count more than one (Step 19)
	into     []string
	absolute bool
	program  string
	renaming string
	// format is "zip" where the members must be read from a zip (unzip), so a
	// listing read as another format is the extractor's fall-back, not the
	// archive; "" where any format the reader reads is the archive (tar).
	format string
	// envVar is an option variable set for the extractor (UNZIP, UNZIPOPT or
	// TAR_OPTIONS), which the hook cannot see the options of (Step 19, B2).
	envVar string
	// noArchive is set where the extractor is given no archive the scan can
	// pick out of its options (unzip, Step 19).
	noArchive bool
}

// unreadable reports whether an extraction's members cannot be known without
// the archive's magic bytes being wrong for it: another program reads it, an
// option renames members, it comes on standard input or a `-`, an option
// variable adds options the hook cannot see, the archive word is one the shell
// or the extractor fills in, the extractor names no archive, or a tar is given
// more than one archive, which differ in which they extract.
func (e extraction) unreadable() bool {
	return e.program != "" || e.renaming != "" || e.archive == "" || e.archive == "-" ||
		e.envVar != "" || e.noArchive || e.manyArchives() || expandedArchive(e.archive)
}

// manyArchives reports whether a tar is given more than one distinct archive.
func (e extraction) manyArchives() bool {
	seen := map[string]bool{}
	for _, a := range e.archives {
		seen[a] = true
	}
	return len(seen) > 1
}

// expandedArchive reports whether an archive word is one the shell or the
// extractor expands to another file: a variable, a substitution, a backtick, a
// `%name%`, a glob, or a brace list, none of which the hook reads as the file
// that is extracted (B4).
func expandedArchive(archive string) bool {
	return strings.ContainsAny(archive, "$`%*?[{")
}

// extractionOf is the extraction a command makes, and false for a command
// that extracts nothing. line and carried are where the extractor's option
// variables are read, as withEnvironmentOptions reads the compressors' (B2).
func extractionOf(words []string, line string, carried func(string) string) (extraction, bool) {
	name, args := base(words[0]), words[1:]
	switch name {
	case "tar":
		e, ok := tarExtraction(args)
		return withExtractorEnvironment(e, name, line, carried), ok
	case "unzip":
		if into := unzipInto(args); into != nil {
			e := extraction{into: into, format: "zip"}
			e.archive, e.noArchive = unzipArchive(args)
			if unzipRenames(args) {
				e.renaming = "-j"
			}
			return withExtractorEnvironment(e, name, line, carried), true
		}
	case "jar":
		if len(args) > 0 && strings.Contains(strings.TrimPrefix(args[0], "-"), "x") {
			e := extraction{into: orHere(valueOf(args, "-C"))}
			if strings.Contains(args[0], "f") && len(args) > 1 {
				e.archive = args[1]
			}
			return e, true
		}
	case "expand-archive":
		e := extraction{into: orHere(parameterValue(args, "-destinationpath"))}
		if archive := slices.Concat(parameterValue(args, "-literalpath"), parameterValue(args, "-path"),
			nonOptions(args)); len(archive) > 0 {
			e.archive = archive[0]
		}
		return e, true
	}
	return otherExtraction(name, args)
}

// tarExtraction is extractionOf for a tar. A snapshot file (-g) is not counted
// among the archives, so `tar -xg snap -f a.tar -C .` reads one archive.
func tarExtraction(args []string) (extraction, bool) {
	archives, _, into, _, extract := tarArguments(args)
	if !extract {
		return extraction{}, false
	}
	e := extraction{into: orHere(into), renaming: renamingOption(args), archive: first(archives), archives: archives,
		absolute: hasWord(args, "--absolute-names") || shortFlag(args, 'P') ||
			len(args) > 0 && !strings.HasPrefix(args[0], "-") && strings.Contains(args[0], "P")}
	return e, true
}

// withExtractorEnvironment marks an extraction unreadable where an option
// variable the extractor reads is set, as withEnvironmentOptions (writers.go)
// folds the compressors' variables: the hook cannot see the options the
// variable adds, so where the members land is unknown. The cwd is added to
// where it extracts, since the variable can carry a -d or -C that redirects
// it into the project (B2).
func withExtractorEnvironment(e extraction, name, line string, carried func(string) string) extraction {
	for _, v := range extractorEnvironment[name] {
		if environmentSet(v, line, carried) {
			e.envVar = v
			e.into = append(e.into, ".")
			return e
		}
	}
	return e
}

// extractorEnvironment are the variables each extractor reads options from,
// first among its words, as compressorEnvironment (writers.go) is for the
// compressors.
var extractorEnvironment = map[string][]string{
	"unzip": {"UNZIP", "UNZIPOPT"},
	"tar":   {"TAR_OPTIONS"},
}

// environmentSet reports whether a variable is set for the command, on the
// line or carried from the environment the session started in, with a value
// that is not empty.
func environmentSet(name, line string, carried func(string) string) bool {
	if carried != nil && strings.Trim(carried(name), `"'`) != "" {
		return true
	}
	for f := range strings.FieldsSeq(line) {
		if v, ok := strings.CutPrefix(f, name+"="); ok && strings.Trim(v, `"'`) != "" {
			return true
		}
	}
	return false
}

// unzipArchive is the file unzip extracts, picked from its options as unzip
// reads them: -d, -P and the charset options -O and -I take a value, the rest
// of a bundle where they are not last, or the next word. The archive is the
// first word that is neither an option nor such a value. noArchive is set
// where a bundle holds a letter the scan does not know, so a valued option the
// scan cannot see would move the archive word onto the next (B4); the refusing
// reading is that no archive is named, which refuses as unreadable.
func unzipArchive(args []string) (archive string, noArchive bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" || !strings.HasPrefix(a, "-") {
			return a, false
		}
		takesNext, known := unzipBundle(strings.TrimPrefix(a, "-"))
		if !known {
			return "", true
		}
		if takesNext {
			i++
		}
	}
	return "", true
}

// unzipBundle reads one bundle of unzip's option letters: whether its last
// valued letter takes the next word as its value, and whether every letter is
// one the scan knows. An unknown letter could be a valued option the scan
// cannot see, which would move the archive onto the next word, so the refusing
// reading is that no archive is named (B4).
func unzipBundle(letters string) (takesNext, known bool) {
	const valued, flags = "dPOI", "onqufjabCLUXKsyMVBhWDR2"
	for j := range len(letters) {
		switch c := letters[j]; {
		case strings.IndexByte(valued, c) >= 0:
			// A valued letter before other letters takes the rest of the
			// bundle; last in the bundle it takes the next word.
			return j == len(letters)-1, true
		case strings.IndexByte(flags, c) < 0:
			return false, false
		}
	}
	return false, true
}

// unzipRenames reports whether unzip is told to junk the members' paths (-j),
// alone or in a bundle, so a member does not land where its name says (B3).
func unzipRenames(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsRune(a, 'j') {
			return true
		}
	}
	return false
}

// otherExtraction is extractionOf for the extractors whose members the hook
// does not read: what pax, cpio, unar, 7z and ditto read is not a format the
// reader knows, or comes on standard input, so where they put it is all that
// is known.
func otherExtraction(name string, args []string) (extraction, bool) {
	e := extraction{program: name}
	switch name {
	case "pax":
		if !shortFlag(args, 'w') {
			e.into, e.archive = paxInto(args), first(valueOf(args, "-f"))
		}
	case "cpio":
		if !shortFlag(args, 'p') {
			e.into, e.archive = cpioInto(args), first(append(valueOf(args, "-F"), valueOf(args, "--file")...))
		}
	case "unar":
		e.into = orHere(append(valueOf(args, "-o"), valueOf(args, "-output-directory")...))
		e.archive = operandAfter(args, "-o")
	case "7z", "7za", "7zr", "7zz":
		if e.into = sevenZipInto(args); len(e.into) > 0 {
			e.archive = first(nonOptions(args)[1:])
		}
	case "ditto":
		if hasWord(args, "-x") {
			e.into, e.archive = destination(nonOptions(args)), first(nonOptions(args))
		}
	}
	return e, len(e.into) > 0
}

// first is the first of words, or "" for none.
func first(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

// renamingOption is the option of a tar that changes a member's name as it
// is extracted, so that where it lands is not its name: GNU's
// --strip-components and --transform, and bsdtar's -s.
func renamingOption(args []string) string {
	for _, a := range args {
		option, _, _ := strings.Cut(a, "=")
		switch option {
		case "--strip-components", "--strip", "--transform", "--xform", "-s", "--substitute":
			return option
		}
	}
	return ""
}

// operandAfter is the first word of args that is neither an option nor the
// value of the option given.
func operandAfter(args []string, valued string) string {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == valued:
			i++
		case !strings.HasPrefix(a, "-"):
			return a
		}
	}
	return ""
}

// unextracted is an extraction into the project whose members could not be
// read, with where it puts them as the command spells it.
type unextracted struct {
	e       extraction
	into    string
	listing Listing
}

// extracted are where the members of the archive an extraction into the
// project reads land, each under the directory it is extracted into, and
// what could not be read where its members could not all be. A member is
// landed as tar and unzip land it, with its leading `/` and every `..` taken
// off, but under -P as it is written. Neither is asked for an extraction
// above the project, or with the walk lost: checkFillsAbove and the unplaced
// walk refuse those. Without it, `tar -xf p.tar -C .` with a member
// CLAUDE.md overwrote the root's.
func (q pathQuestion) extracted() (landed []string, short *unextracted) {
	if q.s.disk.Archive == nil || q.lost || len(q.run.words) == 0 {
		return nil, nil
	}
	e, ok := extractionOf(q.run.words, q.line, q.s.Carried)
	if !ok {
		return nil, nil
	}
	var into []string
	for _, d := range e.into {
		if q.inProject(q.placedDir(d)) {
			into = append(into, d)
		}
	}
	if len(into) == 0 {
		return nil, nil
	}
	if e.unreadable() {
		return nil, &unextracted{e: e, into: into[0]}
	}
	l := q.s.disk.Archive(q.placedDir(e.archive))
	// An unzip listing counts as the archive's members only where the hook
	// read the file as a zip: unzip falls back to `<name>.zip` and matches a
	// wildcard in the zipfile name, so a file read as another format is not
	// what unzip lays down. An empty format counts as a zip (the fake disk of
	// the frozen rows leaves it empty); the hook's reader sets a format on
	// every complete exit, so a real non-zip is caught (B4).
	if e.format == "zip" && l.Format != "" && l.Format != "zip" {
		u := unextracted{e: e, into: into[0], listing: l}
		return nil, &u
	}
	for _, n := range l.Names {
		landed = append(landed, landedMember(graphic(n), e.absolute, into)...)
	}
	if !l.Complete {
		short = &unextracted{e: e, into: into[0], listing: l}
	}
	return landed, short
}

// landedMember is where an archive's member lands in each directory: with
// its leading `/` and every `..` taken off, or under -P (absolute) as it is
// written, where a leading `/` puts it there whatever the directory.
func landedMember(member string, absolute bool, into []string) []string {
	if !absolute {
		member = strings.Join(slices.DeleteFunc(strings.Split(member, "/"), func(seg string) bool {
			return seg == "" || seg == "." || seg == ".."
		}), "/")
	}
	switch {
	case member == "":
		return nil
	case strings.HasPrefix(member, "/"):
		return []string{member}
	}
	landed := make([]string, 0, len(into))
	for _, d := range into {
		landed = append(landed, underDir(strings.TrimSuffix(clean(d), "/"), member))
	}
	return landed
}

// checkFromDisk refuses a write into the project that turns on what the disk
// holds, where the disk could not be read in full: a glob whose directory, or
// a copy whose source, could not be listed, and an extraction whose archive's
// members could not be read. It is asked after the rules that judge by name,
// so a name the partial read did find keeps its rule, and before
// checkFillsAbove, which keeps an extraction above the project.
func checkFromDisk(q pathQuestion) (Finding, bool) {
	_, short := q.listedNames()
	_, _, copied := q.copyFills()
	short = append(short, copied...)
	if to, sources, ok := q.pipedCopy(); ok {
		_, _, piped := q.sourceLandings(to, sources, false)
		short = append(short, piped...)
	}
	if len(short) > 0 {
		return q.unlistedFinding(short[0]), true
	}
	if _, u := q.extracted(); u != nil {
		return q.unextractedFinding(*u), true
	}
	return Finding{}, false
}

// unlistedFinding is the refusal of a write whose glob or copy's source
// could not be listed in full.
func (q pathQuestion) unlistedFinding(u unlisted) Finding {
	route := "name the files you mean or use a narrower glob; to empty a large directory, remove the " +
		"directory itself (`rm -rf dist`) and make it again"
	if u.copied {
		route = "copy the files by name, or copy the directory under its own name"
	}
	return Finding{
		Rule: ruleDirectoryUnknown,
		Reason: q.quoted(u.word) + " could not be listed in full (" + cmp.Or(graphic(u.listing.Cause), "the listing failed") +
			"), so the hook cannot tell whether it matches an instruction file or loop state",
		Route: route,
	}
}

// unextractedFinding is the refusal of an extraction into the project whose
// archive's members could not be read, saying why.
func (q pathQuestion) unextractedFinding(u unextracted) Finding {
	e, l := u.e, u.listing
	extractor := shown(base(q.run.words[0]))
	route := "extract it outside the project (an absolute path under the temp directory) and copy by name the " +
		"files you need; or, if an earlier part of this command makes the archive, run that part first as its " +
		"own command so the hook can read it; otherwise say what you would unpack and why, and stop"
	var why string
	switch {
	case e.envVar != "":
		// The variable is known, but the options it adds are not, so the way
		// through is to take it out of the environment.
		why = "`" + e.envVar + "` is set, and the hook cannot see the options it adds to " + extractor +
			", so it cannot tell where the members land"
		route = "unset `" + e.envVar + "`, or put its options on the command line; or " + route
	case e.manyArchives():
		why = extractor + " is given more than one archive (" + q.quoted(strings.Join(e.archives, "`, `")) +
			"), and tars differ in which one they extract, so the hook cannot tell whose members land"
		route = "give each archive its own extraction command, so the hook can read its members; or " + route
	case e.noArchive:
		why = extractor + " names no archive the hook can pick out of its options, so it cannot tell which file " +
			"it extracts"
	case e.renaming != "":
		why = shown(e.renaming) + " renames members as they are extracted, so the hook cannot tell where they land"
	case e.archive == "" || e.archive == "-":
		why = extractor + " reads its archive from standard input, whose member names the hook cannot read"
	case expandedArchive(e.archive):
		why = q.quoted(e.archive) + " is expanded by the shell or " + extractor + " to a file the hook cannot see, " +
			"so it cannot read its members"
	case e.program != "":
		why = q.quoted(e.archive) + " is extracted by " + shown(e.program) + ", whose archives the hook cannot " +
			"read the member names of" + q.formats()
	case e.format == "zip" && l.Format != "" && l.Format != "zip":
		why = extractor + " reads " + q.quoted(e.archive) + " as a zip, and it is not one (the hook reads it as " +
			l.Format + "), so it cannot tell which members " + extractor + " extracts"
	case l.Missing:
		why = q.quoted(e.archive) + " does not exist yet, so the hook cannot read its members"
	case l.Cause == "":
		why = q.quoted(e.archive) + " is an archive the hook cannot read the member names of" + q.formats()
	default:
		why = q.quoted(e.archive) + " could not be read to its end (" + bounded(graphic(l.Cause)) + ")"
	}
	return Finding{
		Rule: ruleArchiveUnknown,
		Reason: why + ", and it is extracted into " + q.quoted(u.into) + ", which may hold an instruction file " +
			"or loop state",
		Route: route,
	}
}

// formats is the clause naming the archive formats the hook reads.
func (q pathQuestion) formats() string {
	f := q.s.disk.Formats
	switch len(f) {
	case 0:
		return ""
	case 1:
		return " (it reads " + f[0] + ")"
	}
	return " (it reads " + strings.Join(f[:len(f)-1], ", ") + " and " + f[len(f)-1] + ")"
}

const (
	ruleDirectoryUnknown = "directory-entries-unknown"
	ruleArchiveUnknown   = "archive-members-unknown"
)
