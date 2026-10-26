package shellpolicy

// US-120, the third unfreeze: rows that hold the Gate 6 rework, so that
// undoing it fails a frozen test. Round 2 of the verification ran each of
// these in a scratch copy and found none of them in the suite. Each test names
// the code it pins; the spellings are the shapes the frozen us120_test.go rows
// and VERIFICATION.md already use.

import "testing"

// AC-7: rsync and scp land a CLAUDE.md, or a .sdlc, in a project root whose
// name has a dot as they do in a plain root. intoADirectory reads
// `../me.github.io` as a file because its last name has a dot; cp's copy was
// read by directoryLandings first, and rsync's and scp's only once
// copyArguments read their operands. Each dotted row is checked against its
// plain-root twin, so the two cannot drift apart.
func TestAC7RsyncAndScpIntoADottedProjectRootAreRefusedAsIntoAPlainOne(t *testing.T) {
	dotted := agAboveStateAt("/src/me.github.io")
	dotted.Frozen = []string{agFrozenTest}
	dotted.Backlog = "user_stories.json"
	plain := agAboveStateAt("/src/project")
	plain.Frozen = []string{agFrozenTest}
	plain.Backlog = "user_stories.json"
	for _, row := range []struct{ dotted, plain, rule string }{
		{"rsync /tmp/p/CLAUDE.md ../me.github.io", "rsync /tmp/p/CLAUDE.md ../project", agProtected},
		{"rsync -a /tmp/p/CLAUDE.md /src/me.github.io", "rsync -a /tmp/p/CLAUDE.md /src/project", agProtected},
		{"scp /tmp/p/CLAUDE.md ../me.github.io", "scp /tmp/p/CLAUDE.md ../project", agProtected},
		{"scp /tmp/p/CLAUDE.md /src/me.github.io", "scp /tmp/p/CLAUDE.md /src/project", agProtected},
		{"rsync -a /tmp/p/.sdlc ../me.github.io", "rsync -a /tmp/p/.sdlc ../project", ruleLoopState},
		{"scp -r /tmp/p/.sdlc /src/me.github.io", "scp -r /tmp/p/.sdlc /src/project", ruleLoopState},
	} {
		t.Run("AC-7 plain root "+row.plain, func(t *testing.T) { u120Refused(t, "AC-7", row.plain, plain, row.rule) })
		t.Run("AC-7 "+row.dotted, func(t *testing.T) { u120Refused(t, "AC-7", row.dotted, dotted, row.rule) })
	}
	// An ordinary file into the dotted root, and a copy into a dotted name the
	// state does not show to be a directory, stay allowed: the rework reads the
	// root as a directory, not every dotted name.
	for _, command := range []string{
		"rsync /tmp/x/notes.md ../me.github.io",
		"scp /tmp/x/notes.md /src/me.github.io",
		"rsync /tmp/p/CLAUDE.md docs.v2",
		"scp /tmp/p/CLAUDE.md docs.v2",
	} {
		t.Run("AC-7 "+command, func(t *testing.T) { u120Allowed(t, "AC-7", command, dotted) })
	}
}

// AC-11: split and csplit add letters, digits or a format's to the prefix
// they are given, and split adds --additional-suffix after that, so a prefix
// that only starts a held name writes it. `split -l 1 x CLAUDE.` spelt neither
// the prefix nor any name it wrote, and was let through where it wrote
// CLAUDE.md, until suffixed read the names a prefix can write.
func TestAC11ASplitPrefixThatStartsAHeldNameIsRefusedAsWritingIt(t *testing.T) {
	s := agHeldState()
	for command, rule := range map[string]string{
		"split -l 1 x CLAUDE.":                                    agProtected,
		"split -l 1 x CLAUDE":                                     agProtected,
		"split -l 1 x user_stories":                               agProtected,
		"split -l 1 x internal/invoice_":                          ruleFrozenTest,
		"csplit -f CLAUDE. x 1":                                   agProtected,
		"csplit --prefix=CLAUDE x 1":                              agProtected,
		"split -l 1 --additional-suffix=.md x CLAUD":              agProtected,
		"split -l 1 --additional-suffix .md x sub/AGENT":          agProtected,
		"split -l 1 --additional-suffix=.json x user_storie":      agProtected,
		"split -l 1 --additional-suffix=_test.go x internal/invo": ruleFrozenTest,
	} {
		t.Run("AC-11 "+command, func(t *testing.T) { u120Refused(t, "AC-11", command, s, rule) })
	}
	// A prefix whose names are all ordinary stays allowed, with a suffix or
	// without, and so does the default prefix.
	for _, command := range []string{
		"split -l 1 x out/part",
		"split -l 1 --additional-suffix=.md x out/part",
		"csplit -f out/part x 1",
		"split -l 1 x notes",
		"split -l 1 x",
		"csplit x 1",
	} {
		t.Run("AC-11 "+command, func(t *testing.T) { u120Allowed(t, "AC-11", command, s) })
	}
}

// AC-3: after a `cd` the walk cannot place, a relative word is judged from
// each directory of the project that makes it a held path. Judged by its name
// alone, `cd - && rm -rf state` took .sdlc/state away, until heldFrom named
// the directories a held path runs through: .sdlc for state and config.json,
// internal for the frozen test.
func TestAC3ARelativeRemovalAfterAnUnplacedCdIsRefusedWhereAHeldPathEndsWithIt(t *testing.T) {
	s := agHeldState()
	for command, rule := range map[string]string{
		"cd /tmp && cd - && rm -rf state":           ruleLoopState,
		"cd - && rm -rf state":                      ruleLoopState,
		"cd - && rm -f config.json":                 ruleLoopState,
		"cd - && echo x > config.json":              ruleLoopState,
		`cd "$D" && rm -rf state`:                   ruleLoopState,
		`cd "$D" && rm -f config.json`:              ruleLoopState,
		"cd - && rm -f invoice_test.go":             ruleFrozenTest,
		`cd "$D" && rm -f ./invoice_test.go`:        ruleFrozenTest,
		`cd "$X"/internal && rm -f invoice_test.go`: ruleFrozenTest,
	} {
		t.Run("AC-3 "+command, func(t *testing.T) { u120Refused(t, "AC-3", command, s, rule) })
	}
	// A name no held path ends with stays allowed after the same cd: the
	// released control.
	for _, command := range []string{`cd "$D" && rm -rf build`, "cd - && rm -rf build", "cd - && rm -f notes.json"} {
		t.Run("AC-3 "+command, func(t *testing.T) { u120Allowed(t, "AC-3", command, s) })
	}
}

// US-120, the fourth unfreeze: rows for the Gate 7 findings, so that each is
// held by a frozen test. Each test names the review that found it.

// AC-4 (code review round 1, blocking 2): a copy whose target is glued into
// an option bundle ending in t fills that target, as the plain spelling does.
// filledDirs reads its arguments through moveArguments, which reads `-rt` and
// `-pt` as options that take no value, so `..` was read as a source and the
// glob as where the copy lands; the release hid it by refusing every `cp -r`,
// and `cp -pt .. /tmp/p/*` was let through by both. Each row is checked
// against its plain twin, so the two cannot drift apart.
func TestAC4ACopyWhoseTargetEndsAnOptionBundleFillsItAsThePlainSpellingDoes(t *testing.T) {
	d := &u120Disk{entries: map[string]Listing{"/tmp/p": u120Listed(".", "..", "CLAUDE.md", "x.txt")}}
	s := agHeldState().WithDisk(d.disk(u120Formats...))
	for _, row := range []struct{ bundled, plain string }{
		{"cp -rt .. /tmp/p/*", "cp -r /tmp/p/* .."},
		{"cp -rt.. /tmp/p/*", "cp -r /tmp/p/* .."},
		{"cp -pt .. /tmp/p/*", "cp -p /tmp/p/* .."},
		{"cp -pt.. /tmp/p/*", "cp -p /tmp/p/* .."},
	} {
		t.Run("AC-4 plain "+row.plain+" for "+row.bundled, func(t *testing.T) { u120Refused(t, "AC-4", row.plain, s, agAboveRule) })
		t.Run("AC-4 "+row.bundled, func(t *testing.T) { u120Refused(t, "AC-4", row.bundled, s, agAboveRule) })
	}
	// A named source that is no instruction file lands under its own name,
	// and stays allowed spelt with the bundle as without it.
	for _, command := range []string{"cp -rt .. docs", "cp -pt .. /tmp/p/x.txt"} {
		t.Run("AC-4 "+command, func(t *testing.T) { u120Allowed(t, "AC-4", command, s) })
	}
}

// AC-12 (code review round 1, blocking 1): a recursive copy or a move above
// the project whose source the hook cannot read is refused, as the release
// refused every one. Narrowed to sources that fill the directory with their
// contents, filledDirs read `$D` as a name that lands under itself, so `D=/tmp/p/.;
// cp -r $D ..` and `F=/tmp/p/CLAUDE.md; mv $F ..` put an instruction file
// above the project. The sources spelt out as plain names stay allowed: that
// is the half of AC-12 the narrowing was for.
func TestAC12ACopyOrMoveAboveTheProjectFromASourceTheHookCannotReadIsRefused(t *testing.T) {
	s := agHeldState()
	for _, command := range []string{"cp -r $D ..", "mv $F ..", "cp -r $(echo /tmp/p/.) .."} {
		t.Run("AC-12 "+command, func(t *testing.T) { u120Refused(t, "AC-12", command, s, agAboveRule) })
	}
	for _, command := range []string{"cp -r docs ..", "mv build.log .."} {
		t.Run("AC-12 "+command, func(t *testing.T) { u120Allowed(t, "AC-12", command, s) })
	}
}

// AC-3 (verification round 3, gap 1): where the listing of .sdlc/stories came
// back short, any first name of a relative word could be a story it left
// out, so after a `cd` the walk cannot place, `rm -rf build` is refused as
// loop state. storyRecords reads every first name as a story's then; read by
// the names listed alone, the short listing would let a story's record be
// taken away. The same removal placed, or with a complete listing that
// holds no build, stays allowed.
func TestAC3AfterAnUnplacedCdARemovalIsRefusedWhereTheStoriesListingCameBackShort(t *testing.T) {
	stories := func(l Listing) State {
		return agHeldState().WithDisk(Disk{Stories: func() Listing { return l }})
	}
	short := map[string]State{
		"cut short":      stories(Listing{Names: []string{"A-1"}, Cause: "more than 20000 entries"}),
		"listing failed": stories(Listing{Cause: "the listing failed"}),
	}
	for name, s := range short {
		for _, command := range []string{"cd - && rm -rf build", `cd "$D" && rm -rf build`, "cd - && rm -rf A-1"} {
			t.Run("AC-3 "+name+" "+command, func(t *testing.T) { u120Refused(t, "AC-3", command, s, ruleLoopState) })
		}
		for _, command := range []string{"rm -rf build", "cd sub && rm -rf build", "cd /tmp && rm -rf build"} {
			t.Run("AC-3 "+name+" placed "+command, func(t *testing.T) { u120Allowed(t, "AC-3", command, s) })
		}
	}
	full := stories(u120Listed("A-1"))
	t.Run("AC-3 complete cd - && rm -rf build", func(t *testing.T) { u120Allowed(t, "AC-3", "cd - && rm -rf build", full) })
	t.Run("AC-3 complete cd - && rm -rf A-1", func(t *testing.T) {
		u120Refused(t, "AC-3", "cd - && rm -rf A-1", full, ruleLoopState)
	})
}

// AC-10 (security review, note 1): an extraction is judged by the archive the
// extractor reads. unzip's -P takes the password as its value, so `unzip -P
// <x> <archive> -d .` extracts <archive>; operandAfter skipped only -d's
// value, and judged <x>. tar given more than one archive extracts one the
// hook cannot tell (bsdtar the last), where tarExtraction read the first;
// more than one is refused as archive-members-unknown, the person's reading
// at the unfreeze. The archive holding CLAUDE.md is p, the others hold
// ordinary names.
func TestAC10AnExtractionIsJudgedByTheArchiveTheExtractorReads(t *testing.T) {
	d := &u120Disk{archives: map[string]Listing{
		"/tmp/p.zip": u120Listed("CLAUDE.md"),
		"/tmp/n.zip": u120Listed("notes/a.txt"),
		"/tmp/p.tar": u120Listed("CLAUDE.md"),
		"/tmp/n.tar": u120Listed("notes/a.txt"),
		"/tmp/m.tar": u120Listed("notes/b.txt"),
	}}
	s := agHeldState().WithDisk(d.disk(u120Formats...))
	for _, command := range []string{"unzip -P /tmp/n.zip /tmp/p.zip -d .", "unzip -P /tmp/n.zip /tmp/p.zip"} {
		t.Run("AC-10 "+command, func(t *testing.T) { u120Refused(t, "AC-10", command, s, agProtected) })
	}
	// The password is no archive the other way round either: an ordinary
	// archive after a password that names one holding CLAUDE.md is read for
	// its own members, and allowed.
	for _, command := range []string{"unzip -P /tmp/p.zip /tmp/n.zip -d .", "unzip /tmp/n.zip -d ."} {
		t.Run("AC-10 "+command, func(t *testing.T) {
			d.asked = nil
			u120Allowed(t, "AC-10", command, s)
			u120Asked(t, "AC-10", command, d, "archive /tmp/n.zip")
		})
	}
	for _, command := range []string{
		"tar -xf /tmp/n.tar -f /tmp/m.tar -C .",
		"tar -x --file=/tmp/n.tar --file /tmp/m.tar -C .",
		"bsdtar -xf /tmp/n.tar -f /tmp/m.tar -C .",
	} {
		t.Run("AC-10 "+command, func(t *testing.T) { u120Refused(t, "AC-10", command, s, ruleArchiveUnknown) })
	}
	// The reviewer's own shape: the archive read first holds ordinary names,
	// the one extracted last holds CLAUDE.md. Refused, by either rule.
	t.Run("AC-10 tar -xf /tmp/n.tar -f /tmp/p.tar -C .", func(t *testing.T) {
		u120RefusedUnder(t, "AC-10", "tar -xf /tmp/n.tar -f /tmp/p.tar -C .", s, ruleArchiveUnknown, agProtected)
	})
	// One archive is still judged by its members, and two extracted outside
	// the project write nothing the rules hold.
	for _, command := range []string{"tar -xf /tmp/n.tar -C .", "tar -xf /tmp/n.tar -f /tmp/m.tar -C /tmp/x"} {
		t.Run("AC-10 "+command, func(t *testing.T) { u120Allowed(t, "AC-10", command, s) })
	}
}
