package shellpolicy

// US-120: close every shell spelling US-023's reviews showed getting past a
// rule, so v0.5.0 ships with none known. Each row is a spelling a reviewer or
// a criterion named; the comment on each test says the family it belongs to
// (ANALYSIS.md F1 to F7) and why it is let through today.
//
// These rows do not go through the US-023 mirror harness (refused/allowed):
// the spellings here are new readings, not respellings of a held name, so the
// helpers below assert the rule directly without respelling to AGENTS.md.

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// u120Refused checks command is refused under rule, with a route and a reason,
// without a panic (the hook fails open on a panic).
func u120Refused(t *testing.T, criterion, command string, s State, rule string) {
	t.Helper()
	f, ok := inspectNoPanic(t, command, s)
	switch {
	case !ok:
		t.Errorf("%s %q is allowed; want it refused as %s", criterion, command, rule)
	case f.Rule != rule:
		t.Errorf("%s %q is refused as %s; want %s (%s)", criterion, command, f.Rule, rule, f.Reason)
	case f.Route == "" || f.Reason == "":
		t.Errorf("%s %q: finding = %+v", criterion, command, f)
	}
}

// u120RefusedUnder is u120Refused for a case that may rightly carry one of
// several rules.
func u120RefusedUnder(t *testing.T, criterion, command string, s State, rules ...string) {
	t.Helper()
	f, ok := inspectNoPanic(t, command, s)
	switch {
	case !ok:
		t.Errorf("%s %q is allowed; want it refused as %s", criterion, command, strings.Join(rules, " or "))
	case !slices.Contains(rules, f.Rule):
		t.Errorf("%s %q is refused as %s; want %s (%s)", criterion, command, f.Rule, strings.Join(rules, " or "), f.Reason)
	case f.Route == "" || f.Reason == "":
		t.Errorf("%s %q: finding = %+v", criterion, command, f)
	}
}

func u120Allowed(t *testing.T, criterion, command string, s State) {
	t.Helper()
	if f, ok := inspectNoPanic(t, command, s); ok {
		t.Errorf("%s %q is refused as %s, and should be left alone: %s", criterion, command, f.Rule, f.Reason)
	}
}

// u120HeldTargets maps a held path in agHeldState to the rule a removal or a
// redirect of it is refused under today, so a new spelling can be checked
// against the same rule its plain spelling gets.
var u120HeldTargets = map[string]string{
	"CLAUDE.md":              agProtected,
	"sub/AGENTS.md":          agProtected,
	"user_stories.json":      agProtected,
	".sdlc/config.json":      ruleLoopState,
	".sdlc/state/active":     ruleLoopState,
	".sdlc/state/tests.lock": ruleLoopState,
	agFrozenTest:             ruleFrozenTest,
}

// AC-1 (ANALYSIS F1): a compressor or archiver that removes its operand while
// packing it is a removal of that operand. Where the operand is held, it is
// refused as `rm` of it is. gzip, bzip2, xz and compress remove their input;
// zstd does so with --rm; zip with -m; tar with --remove-files; rsync with
// --remove-source-files. Today `decompressed` returns no writes for a
// compressor, and zip/zstd are in no list, so each of the gzip-family and
// zip/zstd rows is allowed. tar --remove-files and rsync --remove-source-files
// are already refused, by `takenAway` reading what they remove (AC-10); they
// keep a row so a regression shows.
func TestAC1ACompressorThatRemovesItsOperandIsRefusedAsRemovingItIs(t *testing.T) {
	s := agHeldState()
	// Programs let through today: the fix has to refuse each.
	open := []string{"gzip %s", "bzip2 %s", "xz %s", "lzma %s", "compress %s", "pigz %s", "zstd --rm %s", "zip -m x.zip %s"}
	// Programs already refused today, kept so a regression shows (AC-10).
	held := []string{"tar --remove-files -cf x.tar %s", "rsync --remove-source-files %s /tmp/s/"}
	for target, rule := range u120HeldTargets {
		for _, p := range append(append([]string{}, open...), held...) {
			command := fmt.Sprintf(p, target)
			t.Run("AC-1 "+command, func(t *testing.T) { u120Refused(t, "AC-1", command, s, rule) })
		}
	}
	// A keep option, writing to stdout, or an ordinary operand leaves the file
	// in place, so these stay allowed.
	for _, command := range []string{
		"gzip -k CLAUDE.md", "gzip --keep CLAUDE.md", "gzip -c CLAUDE.md", "gzip -c .sdlc/state/active",
		"bzip2 -k CLAUDE.md", "xz -k .sdlc/state/active", "xz -c CLAUDE.md",
		"zstd CLAUDE.md", "zstd -k CLAUDE.md", "zip x.zip CLAUDE.md", "tar -cf x.tar CLAUDE.md",
		"rsync CLAUDE.md /tmp/s/", "gzip notes.txt", "gzip build/out.log", "zip -m x.zip notes.txt",
	} {
		t.Run("AC-1 "+command, func(t *testing.T) { u120Allowed(t, "AC-1", command, s) })
	}
}

// AC-2 (ANALYSIS F4): a `..` after a `**` segment is read where the shell
// lands, not where a textual clean lands. `.sdlc/stories/**/../state/active`
// cleans, with `**` matching nothing, to `.sdlc/state/active`, which removing
// turns every rule off; `internal/sub/**/../x_test.go` cleans to a frozen
// test. `pathrules.Rel` cleans `**/..` to one name before the rules see it, so
// each is let through today. The plain `..` spellings, which clean the same
// way the shell lands, are refused, and pin the comparison.
func TestAC2ADotDotAfterAGlobstarIsJudgedWhereTheShellLands(t *testing.T) {
	s := agHeldState()
	for _, command := range []string{
		"rm .sdlc/stories/**/../state/active",
		"rm /work/project/.sdlc/stories/**/../state/active",
		"echo x > .sdlc/stories/**/../state/active",
	} {
		t.Run("AC-2 "+command, func(t *testing.T) { u120Refused(t, "AC-2", command, s, ruleLoopState) })
	}
	for _, command := range []string{
		"rm internal/sub/**/../invoice_test.go",
		"rm /work/project/internal/sub/**/../invoice_test.go",
	} {
		t.Run("AC-2 "+command, func(t *testing.T) { u120Refused(t, "AC-2", command, s, ruleFrozenTest) })
	}
	// The plain `..`, which lands where the shell lands, is refused today: the
	// `**/..` rows must reach the same rule.
	for _, command := range []string{"rm .sdlc/stories/../state/active", "rm internal/sub/../invoice_test.go"} {
		t.Run("AC-2 control "+command, func(t *testing.T) {
			if _, ok := inspectNoPanic(t, command, s); !ok {
				t.Errorf("AC-2 the plain spelling %q is allowed, so the glob rows compare with nothing", command)
			}
		})
	}
	// A `**/..` that lands on nothing held stays allowed.
	for _, command := range []string{"rm docs/**/../notes.md", "rm internal/sub/**/../README.md"} {
		t.Run("AC-2 "+command, func(t *testing.T) { u120Allowed(t, "AC-2", command, s) })
	}
}

// AC-3 (ANALYSIS F5): after a `cd` or `pushd` the walk cannot place, a later
// relative write or extraction is judged as if the directory were any it could
// be, so it is refused during a story. The walk cannot place a `cd` that CDPATH
// can redirect, a two-word zsh `cd old new`, a `**/..` climb split over two
// `cd`s, or a `cd` whose target starts with a variable; today `changedDir`
// reads none of these, so the fill or removal after them is let through. A
// plain `cd` into an absolute directory stays placed, so ordinary scratch work
// is untouched.
func TestAC3AWriteAfterACdThatCannotBePlacedIsRefused(t *testing.T) {
	s := agHeldState()
	// Reaching the loop's own state after an unplaceable cd.
	for _, command := range []string{
		"cd /tmp && CDPATH=/work/project cd .sdlc && rm -rf state",
		"cd /tmp && CDPATH=/work/project pushd .sdlc && rm -rf state",
	} {
		t.Run("AC-3 "+command, func(t *testing.T) { u120Refused(t, "AC-3", command, s, ruleLoopState) })
	}
	// CDPATH carried in from the environment the session started in: the hook
	// hands it over as Carried, and a relative cd is then not placed.
	carried := agHeldState()
	carried.Carried = func(name string) string {
		if name == "CDPATH" {
			return "/work/project"
		}
		return ""
	}
	for _, command := range []string{"cd /tmp && cd .sdlc && rm -rf state", "cd /tmp && pushd .sdlc && rm -rf state"} {
		t.Run("AC-3 carried CDPATH "+command, func(t *testing.T) { u120Refused(t, "AC-3", command, carried, ruleLoopState) })
	}
	t.Run("AC-3 carried CDPATH, absolute cd", func(t *testing.T) {
		u120Allowed(t, "AC-3", "cd /tmp/x && tar -xf /tmp/p.tar", carried)
	})
	// Already refused today, by the walk losing its place (AC-10): `cd -` back
	// to the root, zsh's two-word cd into it, a cd into what git prints or what
	// a variable holds.
	for _, command := range []string{
		"cd /tmp && cd - && rm -rf .sdlc/state",
		"cd /tmp/x && cd tmp/x work/project && rm -rf .sdlc/state",
		`cd /tmp && cd "$(git rev-parse --show-toplevel)" && rm -rf .sdlc/state`,
		`cd /tmp && cd "$D" && rm -rf .sdlc/state`,
	} {
		t.Run("AC-3 held "+command, func(t *testing.T) { u120Refused(t, "AC-3", command, s, ruleLoopState) })
	}
	t.Run("AC-3 unplaced, nothing held", func(t *testing.T) { u120Allowed(t, "AC-3", `cd "$D" && rm -rf build`, s) })
	// Filling a directory above the project after an unplaceable cd.
	for _, command := range []string{
		"cd /tmp && CDPATH=/work cd project && tar -xf /tmp/p.tar -C ..",
		"cd /work/proj* && cd /tmp && cd - && tar -xf /tmp/p.tar -C ..",
		"cd /work/{project,} && tar -xf /tmp/p.tar",
		"cd /work/project/internal/**/.. && cd .. && tar -xf /tmp/p.tar",
		"cd /tmp/**/.. && cd work && tar -xf /tmp/p.tar",
		`cd "$X"/wor? && tar -xf /tmp/p.tar`,
	} {
		t.Run("AC-3 "+command, func(t *testing.T) { u120Refused(t, "AC-3", command, s, agAboveRule) })
	}
	// A cd the walk can place leaves the write where the walk puts it, so these
	// stay allowed: the route for scratch work is to spell the directory out.
	for _, command := range []string{
		"cd /tmp && cd .sdlc && rm -rf state",
		"cd /tmp/x && tar -xf /tmp/p.tar",
		"cd build && tar -xf /tmp/p.tar",
	} {
		t.Run("AC-3 "+command, func(t *testing.T) { u120Allowed(t, "AC-3", command, s) })
	}
}

// AC-4 (ANALYSIS F2, F3): a copy, move, link or install is judged where it
// lands, however the target is spelt. A target glued to an option bundle
// (`cp -pt.`), an option value after the operands (`ln ... -S`), `rsync -R`
// keeping the source's path, and a destination directory whose name has a dot
// are each misread today, so the landing on a held path is let through. Where
// the destination is resolved to a directory in the project, a dotted name is
// still a directory: cpinto_test.go:39, whose state has no Resolve, keeps its
// released answer.
func TestAC4ACopyLinkOrInstallIsRefusedHoweverTheTargetIsGiven(t *testing.T) {
	s := agHeldState()
	for command, rule := range map[string]string{
		// A target glued to an option bundle (red team round 4, hostile 2).
		"cp -pt. /tmp/p/CLAUDE.md":              agProtected,
		"cp -pt.. /tmp/p/CLAUDE.md":             agAboveRule,
		"install -Dt. /tmp/p/CLAUDE.md":         agProtected,
		"install -Dt.. /tmp/p/CLAUDE.md":        agAboveRule,
		"cp -ptinternal /tmp/p/invoice_test.go": ruleFrozenTest,
		// An option value after the operands moves the destination (hostile 3).
		"ln -sf /tmp/p/CLAUDE.md .. -S /tmp/x": agAboveRule,
		// rsync -R keeps the source's own path under the destination.
		"rsync -R /tmp/p/./.sdlc/config.json /work/project/":  ruleLoopState,
		"rsync -R /tmp/p/./.sdlc/state/active /work/project/": ruleLoopState,
		// A destination directory whose name has a dot (hostile 2, round 7).
		"cp payload/invoice_test.go pkg.v2":           ruleFrozenTest,
		"cp /tmp/x/invoice_test.go ../project/pkg.v2": ruleFrozenTest,
		"mv payload/invoice_test.go pkg.v2":           ruleFrozenTest,
		// xargs hands the names to cp, whose target is the root or above it
		// (US-023 PLAN Not done; red team round 8).
		"echo /tmp/p/CLAUDE.md | xargs cp -t .":             agProtected,
		"echo /tmp/p/CLAUDE.md | xargs -I{} cp {} .":        agProtected,
		"echo /tmp/p/CLAUDE.md | xargs -I{} cp {} ..":       agAboveRule,
		"printf '%s\\n' /tmp/p/AGENTS.md | xargs cp -t sub": agProtected,
	} {
		t.Run("AC-4 "+command, func(t *testing.T) { u120Refused(t, "AC-4", command, s, rule) })
	}
	// Already refused today (AC-10): an option value after the operands, and
	// cp --parents, which keeps the source's path under the destination.
	for command, rule := range map[string]string{
		"install /tmp/p/CLAUDE.md . -m 644":                         agProtected,
		"cd /tmp/p && cp --parents .sdlc/config.json /work/project": ruleLoopState,
		"cd /tmp/p && cp --parents sub/AGENTS.md /work/project":     agProtected,
		"echo CLAUDE.md | xargs -I{} cp /tmp/p/{} .":                agProtected,
	} {
		t.Run("AC-4 held "+command, func(t *testing.T) { u120Refused(t, "AC-4", command, s, rule) })
	}
	t.Run("AC-4 xargs, ordinary", func(t *testing.T) {
		u120Allowed(t, "AC-4", "echo /tmp/p/notes.md | xargs cp -t build", s)
	})
	// A copy or install of an ordinary file, or into a dotted directory that
	// holds nothing, stays allowed.
	for _, command := range []string{
		"cp -pt. /tmp/p/notes.md",
		"cp -pt build /tmp/p/invoice_test.go",
		"cp payload/notes.md pkg.v2",
		"rsync -R /tmp/p/./notes.md /work/project/",
	} {
		t.Run("AC-4 "+command, func(t *testing.T) { u120Allowed(t, "AC-4", command, s) })
	}
	// PowerShell's abbreviated -Destination lands above the project.
	ps := agHeldState()
	ps.PowerShell = true
	t.Run("AC-4 Copy-Item -Dest ..", func(t *testing.T) {
		u120Refused(t, "AC-4", "Copy-Item -Dest .. /tmp/p/CLAUDE.md", ps, agAboveRule)
	})
}

// AC-5 (ANALYSIS F1): a decompressor or extractor is judged by what it writes.
// A bare operand whose compressed twin exists is decompressed to the operand
// itself (`gunzip CLAUDE.md` with CLAUDE.md.gz present); options from the
// environment (`GZIP=-d gzip -f CLAUDE.md.gz`) decompress a file the reader
// sees no `-d` for; and gtar, pax or cpio after a `cd` above fill the directory
// above. `unsuffixed` answers false for an operand with no suffix, `parse`
// drops the assignment, and 11b knows none of gtar/pax/cpio, so each is let
// through today.
func TestAC5ADecompressorIsRefusedByWhatItWrites(t *testing.T) {
	s := agHeldState()
	// A bare operand (the name the program writes) that is held.
	for command, rule := range map[string]string{
		"gunzip CLAUDE.md":                agProtected,
		"gunzip CLAUDE.local.md":          agProtected,
		"gzip -d sub/AGENTS.md":           agProtected,
		"gunzip .sdlc/state/active":       ruleLoopState,
		"gunzip internal/invoice_test.go": ruleFrozenTest,
		"uncompress CLAUDE.md":            agProtected,
		"compress -d CLAUDE.md":           agProtected,
		"gunzip ../CLAUDE.md":             agAboveRule,
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { u120Refused(t, "AC-5", command, s, rule) })
	}
	// Options taken from the environment decompress a held file.
	for command, rule := range map[string]string{
		"GZIP=-d gzip -f CLAUDE.md.gz":          agProtected,
		"export GZIP=-d; gzip -f CLAUDE.md.gz":  agProtected,
		"BZIP2=-d bzip2 -f sub/AGENTS.md.bz2":   agProtected,
		"XZ_OPT=-d xz -f sub/AGENTS.md.xz":      agProtected,
		"GZIP=-d gzip -f .sdlc/state/active.gz": ruleLoopState,
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { u120Refused(t, "AC-5", command, s, rule) })
	}
	// gtar, pax and cpio after a cd above the project fill the directory above.
	for _, command := range []string{
		"cd .. && gtar -xf p.tar",
		"cd .. && pax -r -f p.pax",
		"cd .. && cpio -id -F p.cpio",
		"unar -o .. p.zip",
		"cd .. && jar -xf p.jar",
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { u120Refused(t, "AC-5", command, s, agAboveRule) })
	}
	ps := agHeldState()
	ps.PowerShell = true
	t.Run("AC-5 Expand-Archive", func(t *testing.T) {
		u120Refused(t, "AC-5", "Expand-Archive p.zip -DestinationPath ..", ps, agAboveRule)
	})
	// gunzip -N writes the name the header holds, in the directory of the
	// file; -r decompresses every file below the directory it is given. The
	// words do not say which file is written, so where the directory can hold
	// a held name the command is refused.
	for command, rule := range map[string]string{
		"gunzip -N notes.gz":    agProtected,
		"gunzip -Nf build/x.gz": agProtected,
		"gunzip -r .sdlc/state": ruleLoopState,
		"gunzip -rf sub":        agProtected,
		"gzip -drf .sdlc/state": ruleLoopState,
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { u120Refused(t, "AC-5", command, s, rule) })
	}
	// xz --files reads the names it works on from a file the hook does not
	// read, so what it writes could be any held file in the project.
	for _, command := range []string{"xz -d --files=list.txt", "xz -d --files0=list.txt", "unxz --files=list.txt"} {
		t.Run("AC-5 "+command, func(t *testing.T) {
			u120RefusedUnder(t, "AC-5", command, s, agProtected, ruleLoopState, ruleFrozenTest, agAboveRule)
		})
	}
	// Already refused today (AC-10).
	for command, rule := range map[string]string{"gunzip -N .sdlc/state/x.gz": ruleLoopState, "unzip p.zip -d ..": agAboveRule} {
		t.Run("AC-5 held "+command, func(t *testing.T) { u120Refused(t, "AC-5", command, s, rule) })
	}
	for _, command := range []string{"gunzip -N /tmp/x/notes.gz", "gunzip -Nc notes.gz", "gunzip -r /tmp/x"} {
		t.Run("AC-5 "+command, func(t *testing.T) { u120Allowed(t, "AC-5", command, s) })
	}
	// Writing to stdout, a keep, a list or test option, or an ordinary file is
	// a read of the held file, or writes nothing held, so these stay allowed.
	for _, command := range []string{
		"gzip -dc CLAUDE.md.gz", "gzip --stdout -d CLAUDE.md.gz", "gunzip -l CLAUDE.md.gz",
		"gzip -t CLAUDE.md.gz", "gunzip notes.md.gz", "cd build && gtar -xf /tmp/p.tar",
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { u120Allowed(t, "AC-5", command, s) })
	}
}

// AC-6 (ANALYSIS F6): a glob is read as the shell that runs it reads it. zsh's
// GLOB_DOTS and bash's dotglob let `?` and `*` match a leading dot, so
// `?sdlc/state/active` names the loop's state; bash 3.2's `.?` matches `..`, so
// `.sdlc/stories/.?/state/active` climbs to the state directory; and zsh's
// `(a|b)` names each alternative, so `CLAUDE.(md|x)` names CLAUDE.md. The hook
// does not know the shell or its options, so by the person's standing rule an
// ambiguous reading refuses where it writes. `segmentMatches` never lets a
// glob match a leading dot, and the alternation holds none of `*?[`, so each is
// let through today. (A bare `*`, which needs the disk to say the directory
// holds an instruction file, is in the hook's rows.)
func TestAC6AGlobIsReadAsTheShellThatRunsItReadsIt(t *testing.T) {
	s := agHeldState()
	// GLOB_DOTS and dotglob, set on the line or in the person's start-up files.
	for _, command := range []string{
		"setopt GLOB_DOTS; rm -f ?sdlc/state/active",
		"shopt -s dotglob; rm -f ?sdlc/state/active",
		"rm -f ?sdlc/state/active",
		"setopt GLOB_DOTS; rm -f /work/?roject/?sdlc/config.json",
	} {
		t.Run("AC-6 "+command, func(t *testing.T) { u120Refused(t, "AC-6", command, s, ruleLoopState) })
	}
	// bash 3.2's `.?` standing for `..`.
	t.Run("AC-6 .?-state", func(t *testing.T) {
		u120Refused(t, "AC-6", "bash -c 'rm .sdlc/stories/.?/state/active'", s, ruleLoopState)
	})
	t.Run("AC-6 .?-frozen", func(t *testing.T) {
		u120Refused(t, "AC-6", "rm internal/sub/.?/invoice_test.go", s, ruleFrozenTest)
	})
	// zsh's alternation names each branch; the redirect writes CLAUDE.md.
	t.Run("AC-6 alternation", func(t *testing.T) {
		u120Refused(t, "AC-6", "echo x > CLAUDE.(md|x)", s, agProtected)
	})
	// EXTENDED_GLOB: `^x` names everything but x, and `(*/)#` any number of
	// directories, as `**/` does. `(a|b)` is read by zsh with no option at all;
	// today the line splitter cuts the word at its `|`, and the refusal it gets
	// is story-stays-in-its-session, the wrong rule (ANALYSIS 6.5, 6.7).
	for command, rule := range map[string]string{
		"setopt EXTENDED_GLOB; rm -f internal/^README.md":   ruleFrozenTest,
		"rm -f (CLAUDE|README).md":                          agProtected,
		"setopt EXTENDED_GLOB; rm -f sub/(*/)#../CLAUDE.md": agProtected,
	} {
		t.Run("AC-6 "+command, func(t *testing.T) { u120Refused(t, "AC-6", command, s, rule) })
	}
	// Already refused here today (AC-10): a named directory, made with hash -d
	// or taken from a variable holding an absolute path, which Inspect's state
	// places at the root (the hook, which reads `~c` as user c's home, lets
	// the last two through: its us120_test.go holds them); and `^` after a
	// held directory's own name.
	for command, rule := range map[string]string{
		"hash -d c=/work/project; rm -rf ~c/.sdlc/state":   ruleLoopState,
		"c=/work/project; rm -rf ~c/.sdlc/state":           ruleLoopState,
		"c=/work/project; echo x > ~c/CLAUDE.md":           agProtected,
		"setopt EXTENDED_GLOB; rm -f .sdlc/state/^nothing": ruleLoopState,
	} {
		t.Run("AC-6 held "+command, func(t *testing.T) { u120Refused(t, "AC-6", command, s, rule) })
	}
	// A glob that cannot name a held file, in a directory that holds none,
	// stays allowed.
	for _, command := range []string{
		"rm -rf build/*",
		"setopt GLOB_DOTS; rm -rf build/*",
		"echo x > notes.(md|txt)",
		"rm -f ?uild/out.txt",
		"setopt EXTENDED_GLOB; rm -f build/^x",
	} {
		t.Run("AC-6 "+command, func(t *testing.T) { u120Allowed(t, "AC-6", command, s) })
	}
}

// AC-7 (ANALYSIS F3): where the project's own root directory has a dot in its
// name (me.github.io), every refusal that holds without one holds. A relative
// `cp ... ../me.github.io` or an absolute `cp ... /src/me.github.io` has a
// destination the released reading takes for a file, because its last name has
// a dot, so the copy that lands the root's held files is let through today.
func TestAC7ARefusalHoldsWhereTheProjectRootHasADot(t *testing.T) {
	s := agAboveStateAt("/src/me.github.io")
	s.Frozen = []string{agFrozenTest}
	s.Backlog = "user_stories.json"
	for command, rule := range map[string]string{
		"cp /tmp/p/CLAUDE.md ../me.github.io":  agProtected,
		"mv /tmp/p/CLAUDE.md ../me.github.io":  agProtected,
		"cp -r /tmp/p/.sdlc ../me.github.io":   ruleLoopState,
		"cp -r /tmp/p/.sdlc /src/me.github.io": ruleLoopState,
		"cp -r /tmp/p/.claude ../me.github.io": agProtected,
	} {
		t.Run("AC-7 "+command, func(t *testing.T) { u120Refused(t, "AC-7", command, s, rule) })
	}
	// An ordinary file into the dotted root stays allowed.
	for _, command := range []string{"cp /tmp/x/notes.md ../me.github.io", "cp /tmp/x/notes.md /src/me.github.io"} {
		t.Run("AC-7 "+command, func(t *testing.T) { u120Allowed(t, "AC-7", command, s) })
	}
}

// u120HomeHeld is agHomeState with a frozen test, the backlog and loop state,
// so that a word that climbs out of .agents can land on each kind of held
// file. The project is under the home, so ~/.agents is placed.
func u120HomeHeld(t *testing.T) State {
	t.Helper()
	s, _ := agHomeState(t)
	s.Frozen = []string{"internal/calc/add_test.go"}
	s.Backlog = "user_stories.json"
	return s
}

// AC-8 (ANALYSIS F7): a word that names `.agents` and then climbs out of it
// with `..` is refused under the rule for where it lands, not as the person's
// ~/.agents. Today `checkAgentsNamedWord` fires on the `.agents` in the word
// and answers agents-config-is-the-persons, losing the loop-state or
// protected-path route and the "told" clause, though the word cannot be in
// ~/.agents once it climbs out.
func TestAC8AWordThatClimbsOutOfDotAgentsNamesWhereItLands(t *testing.T) {
	s := u120HomeHeld(t)
	for command, rule := range map[string]string{
		"echo x > $D/.agents/../CLAUDE.md":               agProtected,
		"echo x > $D/.agents/../.sdlc/state/active":      ruleLoopState,
		"rm -rf $D/.agents/../.sdlc/state":               ruleLoopState,
		`cd "$D" && rm -rf .agents/../.sdlc`:             ruleLoopState,
		"rm -rf $D/.agents/../internal/calc/add_test.go": ruleFrozenTest,
	} {
		t.Run("AC-8 "+command, func(t *testing.T) { u120Refused(t, "AC-8", command, s, rule) })
	}
	// A word that stays inside .agents is the person's ~/.agents, and keeps
	// that rule.
	for _, command := range []string{
		"echo x > $D/.agents/skills/x/SKILL.md",
		"echo x > ~/.agents/skills/x/SKILL.md",
	} {
		t.Run("AC-8 "+command, func(t *testing.T) { u120Refused(t, "AC-8", command, s, agAgentsRule) })
	}
}

// AC-11 (ANALYSIS F1): shuf with -o, split with an output prefix, and any
// other program found to write a file named by an option or an operand are
// refused where that file is held, as AC-16 of US-023 refuses sort -o. None of
// these is in `givenWrites` or `mutating`, so each is let through today.
func TestAC11AProgramThatWritesAFileItNamesIsRefused(t *testing.T) {
	s := agHeldState()
	for command, rule := range map[string]string{
		"shuf -o CLAUDE.md x":          agProtected,
		"shuf -oCLAUDE.md x":           agProtected,
		"shuf --output=CLAUDE.md x":    agProtected,
		"shuf -o .sdlc/state/active x": ruleLoopState,
		"shuf -o sub/AGENTS.md x":      agProtected,
		"split -l 1 x CLAUDE.md":       agProtected,
		"split -l 1 x .sdlc/state/act": ruleLoopState,
		// Other programs that write a file they are given (the open-ended clause).
		"zstd -d -o CLAUDE.md x.zst":  agProtected,
		"zstd -o CLAUDE.md x":         agProtected,
		"csplit -f .sdlc/state/x x 1": ruleLoopState,
		"lz4 -d x.lz4 CLAUDE.md":      agProtected,
		"brotli -d -o CLAUDE.md x.br": agProtected,
	} {
		t.Run("AC-11 "+command, func(t *testing.T) { u120Refused(t, "AC-11", command, s, rule) })
	}
	// Reading, or writing an ordinary file, stays allowed.
	for _, command := range []string{
		"shuf x", "shuf -o out.txt x", "split -l 1 x out/part", "sort -o out.txt x",
	} {
		t.Run("AC-11 "+command, func(t *testing.T) { u120Allowed(t, "AC-11", command, s) })
	}
}

// AC-12 (ANALYSIS F7): a refusal names the place a command can actually reach.
// `mv build.log ..` and `cp -r docs ..` put a named file or directory above the
// project, not an instruction file in a directory above it, so they are not
// instructions-above-the-project; today `filledDirs` counts every mv and
// recursive cp, so both are refused under the wrong rule. `rm sub/**/../CLAUDE.md`
// names only paths in the project, so it is a protected path, not above the
// project.
func TestAC12ARefusalNamesThePlaceTheCommandCanReach(t *testing.T) {
	s := agHeldState()
	// Not instructions-above-the-project: they put no instruction file in a
	// directory above the project.
	for _, command := range []string{"mv build.log ..", "cp -r docs .."} {
		t.Run("AC-12 "+command, func(t *testing.T) {
			if f, ok := inspectNoPanic(t, command, s); ok && f.Rule == agAboveRule {
				t.Errorf("AC-12 %q puts no instruction file above the project and was refused as %s: %s", command, agAboveRule, f.Reason)
			}
		})
	}
	// Every path is in the project, so it is a protected path, not above it.
	for _, command := range []string{"rm sub/**/../CLAUDE.md", "echo x > /work/project/sub/**/../AGENTS.md"} {
		t.Run("AC-12 "+command, func(t *testing.T) { u120Refused(t, "AC-12", command, s, agProtected) })
	}
	// A lowercase name is described as matched without regard to case, not as
	// an instruction file on every disk.
	t.Run("AC-12 case", func(t *testing.T) {
		f, ok := inspectNoPanic(t, "echo x > docs/agents.md", s)
		if !ok || f.Rule != agProtected {
			t.Fatalf("AC-12 echo x > docs/agents.md = %+v, %v; want %s", f, ok, agProtected)
		}
		said := strings.ToLower(f.Reason + " " + f.Route)
		if !strings.Contains(said, "case") {
			t.Errorf("AC-12 the refusal of a lowercase agents.md does not say the name is matched without regard to case:\n%s\n%s", f.Reason, f.Route)
		}
	})
	// A command that really can put an instruction file above the project is
	// still refused as such: the fix must not stop refusing the real case.
	for _, command := range []string{"mv /tmp/p/CLAUDE.md ..", "cp /tmp/p/CLAUDE.md .."} {
		t.Run("AC-12 control "+command, func(t *testing.T) { u120Refused(t, "AC-12", command, s, agAboveRule) })
	}
}

// AC-10 (ANALYSIS, VERIFICATION gap 4): US-023's readings that probes found
// right but no frozen row holds. Each is refused today; the test pins it so a
// regression shows, and the test plan gives the mutation that fails each. `**`
// read as an exact segment reaches the state; the long and bundled decompressor
// options write a held file; and climbsPastGlobstar holds a `**/..` landing
// above.
func TestAC10TheUnpinnedUS023ReadingsAreHeld(t *testing.T) {
	s := agHeldState()
	for command, rule := range map[string]string{
		// `**` read as an exact segment (mutation: read `**` only where it is
		// the whole word).
		"rm /work/**/.sdlc/state/active":    ruleLoopState,
		"echo x > /wor**/.sdlc/config.json": ruleLoopState,
		// A `**/..` climb that lands above (mutation: drop climbsPastGlobstar
		// from joinAbove).
		"echo x > /tmp/**/../work/CLAUDE.md":            agAboveRule,
		"rm /tmp/**/../work/project/.sdlc/state/active": ruleLoopState,
		// The long and bundled decompressor options (mutation: read only the
		// short `-d`/`-f` spellings).
		"gunzip --dec sub/AGENTS.md.gz": agProtected,
		"gzip -fdS.q CLAUDE.md.q":       agProtected,
		"unxz -S .q -f sub/AGENTS.md.q": agProtected,
		"gunzip -fSc CLAUDE.mdc":        agProtected,
		// A cd into an absolute glob, followed by a fill (mutation: drop the
		// glob start from checkFillsAbove).
		"cd /wor? && tar -xf /tmp/p.tar": agAboveRule,
	} {
		t.Run("AC-10 "+command, func(t *testing.T) { u120Refused(t, "AC-10", command, s, rule) })
	}
	// The controls these readings must not over-refuse.
	for _, command := range []string{
		"rm /work/zzz/**/.sdlc/state/active",
		"gzip -dc CLAUDE.md.gz",
		"cd /work/project && tar -xf /tmp/p.tar",
	} {
		t.Run("AC-10 "+command, func(t *testing.T) { u120Allowed(t, "AC-10", command, s) })
	}
	// The narrowed ~/.agents ask (claudeconfig.go configWord): a word the
	// policy cannot place is asked as the person's ~/.agents only where the
	// line names ~/.agents. Mutation: drop the namesConfig condition, and the
	// first row is refused; drop the ask, and the second is allowed.
	home := u120HomeHeld(t)
	t.Run("AC-10 narrowed ask, line names no .agents", func(t *testing.T) {
		u120Allowed(t, "AC-10", "echo x > $D/skills/x/SKILL.md", home)
	})
	t.Run("AC-10 narrowed ask, line names ~/.agents", func(t *testing.T) {
		u120Refused(t, "AC-10", "echo x > $D/skills/x/SKILL.md; ls ~/.agents", home, agAgentsRule)
	})
}
