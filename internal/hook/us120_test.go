package hook

// US-120: the shell spellings US-023's reviews showed getting past a rule,
// asked of the hook on a real disk. These are the rows whose answer depends on
// what is there: a link the shell follows, a compressed twin, the name a gzip
// header holds, a directory a bare `*` fills in, a named directory, the
// environment's CDPATH. shellpolicy's us120_test.go holds the rows the words
// alone decide.

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	u120LoopState = "loop-state-through-the-tool"
	u120Protected = "protected-path-through-the-tool"
	u120Write     = "write-protected-path"
)

// u120World is agAboveWorld with the files the rows below reach for: an
// instruction file in the project and in the directory above it, a payload
// outside holding a CLAUDE.md, a compressed twin of CLAUDE.local.md and of
// sub/CLAUDE.md, and notes.gz, whose gzip header names CLAUDE.md, so that
// `gunzip -N` writes CLAUDE.md. side is a directory outside the project that
// the rows cd into, so that nothing they read is outside the test's own temp
// directory.
func u120World(t *testing.T) (root, home, base, payload, side string) {
	t.Helper()
	root, home, base = agAboveWorld(t)
	write(t, root, "CLAUDE.md", "contract\n")
	write(t, root, "sub/README.md", "x\n")
	write(t, root, "build/out.log", "x\n")
	write(t, root, "docs/guide.md", "x\n")
	write(t, home, "code/CLAUDE.md", "the person's\n")
	write(t, base, "p/CLAUDE.md", "planted\n")
	write(t, base, "side/x.txt", "x\n")
	write(t, root, "payload/CLAUDE.md", "planted\n")
	write(t, root, "CLAUDE.local.md.gz", "x")
	write(t, root, "sub/CLAUDE.md.gz", "x")
	f, err := os.Create(filepath.Join(root, "notes.gz"))
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	zw.Name = "CLAUDE.md"
	if _, err := zw.Write([]byte("planted\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return root, home, base, filepath.Join(base, "p"), filepath.Join(base, "side")
}

// u120Link makes a symbolic link at link to target, or skips: Windows makes
// one only with developer mode or an elevated token.
func u120Link(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this platform would not make a symbolic link, which the row is about: %v", err)
	}
}

// u120Expand fills a row's placeholders with the world's paths, slash-separated
// as the shell is given them.
func u120Expand(row, root, home, base, payload, side string) string {
	return strings.NewReplacer("{root}", slash(root), "{home}", slash(home), "{base}", slash(base),
		"{p}", slash(payload), "{side}", slash(side)).Replace(row)
}

// u120Refused checks the hook refuses cmd, run by the implementer at root,
// under one of rules, and fails naming what it said instead.
func u120Refused(t *testing.T, criterion, root, cmd string, getenv func(string) string, rules ...string) {
	t.Helper()
	r := call(t, command(root, "sdlc-implementer", cmd), getenv)
	got := agRuleOf(r)
	switch {
	case slices.Contains(rules, got):
		if !strings.Contains(reasonOf(r), "Instead:") {
			t.Errorf("%s the refusal of %q offers no route:\n%s", criterion, cmd, reasonOf(r))
		}
	case !denied(r):
		t.Errorf("%s %q is allowed; want it refused as %s", criterion, cmd, strings.Join(rules, " or "))
	default:
		t.Errorf("%s %q is refused as %q; want %s:\n%s", criterion, cmd, got, strings.Join(rules, " or "), reasonOf(r))
	}
}

func u120Allowed(t *testing.T, criterion, root, cmd string) {
	t.Helper()
	if r := call(t, command(root, "sdlc-implementer", cmd), noEnv); denied(r) {
		t.Errorf("%s %q is refused, and should be left alone:\n%s", criterion, cmd, reasonOf(r))
	}
}

// AC-1 (ANALYSIS F1): through the hook, a compressor that removes the file it
// packs is refused as `rm` of the file is. Both files exist on the disk.
func TestAC1ThroughTheHookACompressorThatRemovesAHeldFileIsRefused(t *testing.T) {
	root, _, _, _, _ := u120World(t)
	for cmd, rule := range map[string]string{
		"gzip .sdlc/state/active":          u120LoopState,
		"gzip CLAUDE.md":                   u120Protected,
		"xz .sdlc/config.json":             u120LoopState,
		"zstd --rm CLAUDE.md":              u120Protected,
		"zip -m x.zip .sdlc/state/session": u120LoopState,
	} {
		t.Run("AC-1 "+cmd, func(t *testing.T) { u120Refused(t, "AC-1", root, cmd, noEnv, rule) })
	}
	t.Run("AC-1 keep", func(t *testing.T) { u120Allowed(t, "AC-1", root, "gzip -k CLAUDE.md") })
}

// AC-2 (ANALYSIS F4): a `..` after a symbolic link is judged where the shell
// reaches. {base}/s is a link to the project's sub, so {base}/s/.. is the
// project's root, not {base}; a textual clean puts each of these outside the
// project, and the hook lets the loop-state rows through and names the
// CLAUDE.md row as above the project. {base}/r links to the root itself, so
// {base}/r/.. is the directory above. The `**/..` rows are AC-2's examples.
func TestAC2ThroughTheHookADotDotAfterALinkOrAGlobstarIsJudgedWhereTheShellLands(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	u120Link(t, filepath.Join(root, "sub"), filepath.Join(base, "s"))
	u120Link(t, root, filepath.Join(base, "r"))
	for row, rule := range map[string]string{
		"echo x > {base}/s/../.sdlc/config.json":     u120LoopState,
		"rm {base}/s/../.sdlc/state/active":          u120LoopState,
		"echo x > {base}/s/../CLAUDE.md":             u120Protected,
		"rm .sdlc/stories/**/../state/active":        u120LoopState,
		"rm {root}/.sdlc/stories/**/../state/active": u120LoopState,
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-2 "+row, func(t *testing.T) { u120Refused(t, "AC-2", root, cmd, noEnv, rule) })
	}
	// Already refused today (AC-10): {base}/r/.. is the directory above, which
	// a textual clean also puts above the project.
	held := u120Expand("echo x > {base}/r/../AGENTS.md", root, home, base, payload, side)
	t.Run("AC-2 held", func(t *testing.T) { u120Refused(t, "AC-2", root, held, noEnv, agAboveRule) })
	// The shell lands on an ordinary file in the project: nothing to refuse.
	cmd := u120Expand("echo x > {base}/s/../notes.txt", root, home, base, payload, side)
	t.Run("AC-2 ordinary", func(t *testing.T) { u120Allowed(t, "AC-2", root, cmd) })
}

// AC-3 (ANALYSIS F5): after a `cd` the walk cannot place, a relative write or
// extraction is judged as if the directory were any it could be. CDPATH given
// on the line and CDPATH in the environment the session started in each
// redirect `cd .sdlc` into the project; `cd -` returns to a directory the walk
// lost; a `**/..` split over two `cd`s, and a `cd` into what git prints, land
// where the words do not say.
func TestAC3ThroughTheHookAWriteAfterACdThatCannotBePlacedIsRefused(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	for row, rule := range map[string]string{
		"cd {side} && CDPATH={root} cd .sdlc && rm -rf state":                    u120LoopState,
		"cd {side} && CDPATH={root} pushd .sdlc && rm -rf state":                 u120LoopState,
		"cd {root}* && cd {side} && cd - && tar -xf {side}/p.tar -C ..":          agAboveRule,
		"cd {root}/internal/**/.. && cd .. && tar -xf {side}/p.tar":              agAboveRule,
		`cd "$(git rev-parse --show-toplevel)" && cd .. && tar -xf {side}/p.tar`: agAboveRule,
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-3 "+row, func(t *testing.T) { u120Refused(t, "AC-3", root, cmd, noEnv, rule) })
	}
	// CDPATH carried in from the environment the session started in.
	carried := env(map[string]string{"CDPATH": slash(root)})
	cmd := u120Expand("cd {side} && cd .sdlc && rm -rf state", root, home, base, payload, side)
	t.Run("AC-3 CDPATH from the environment", func(t *testing.T) { u120Refused(t, "AC-3", root, cmd, carried, u120LoopState) })
	// A cd into a directory spelled out leaves the write where it says.
	plain := u120Expand("cd {side} && tar -xf {side}/p.tar", root, home, base, payload, side)
	t.Run("AC-3 placed", func(t *testing.T) { u120Allowed(t, "AC-3", root, plain) })
}

// AC-4 (ANALYSIS F2): a copy is judged where it lands however its target is
// given. xargs hands the payload's names to cp, whose `-t .` puts them in the
// project's root; a target glued to an option bundle names the root or the
// directory above it. The payload directory holds a CLAUDE.md.
func TestAC4ThroughTheHookACopyIsRefusedWhereItLandsHoweverTheTargetIsGiven(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	for row, rule := range map[string]string{
		"ls {p}/* | xargs cp -t .":                   u120Protected,
		"printf '%s\\n' {p}/* | xargs -I{} cp {} ..": agAboveRule,
		"ls {p}/* | xargs ln -sf -t .":               u120Protected,
		"cp -pt.. {p}/CLAUDE.md":                     agAboveRule,
		"cp -pt. {p}/CLAUDE.md":                      u120Protected,
		// A glob or a directory's contents copied into the root, where the
		// disk shows the source holds a CLAUDE.md: US-023's PLAN.md (Not done)
		// and its architect's round 3 note 3 recorded these as overwriting the
		// root CLAUDE.md, so AC-10 wants them held.
		"cp {p}/* .":        u120Protected,
		"cp -t . {p}/*":     u120Protected,
		"cp -r {p}/. .":     u120Protected,
		"cp payload/* .":    u120Protected,
		"cp -r payload/. .": u120Protected,
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-4 "+row, func(t *testing.T) { u120Refused(t, "AC-4", root, cmd, noEnv, rule) })
	}
	cmd := u120Expand("ls {side}/* | xargs cp -t build", root, home, base, payload, side)
	t.Run("AC-4 ordinary", func(t *testing.T) { u120Allowed(t, "AC-4", root, cmd) })
	// A directory that holds nothing held, copied into the root, stays allowed.
	fill := u120Expand("cp {side}/* .", root, home, base, payload, side)
	t.Run("AC-4 ordinary fill", func(t *testing.T) { u120Allowed(t, "AC-4", root, fill) })
}

// AC-5 (ANALYSIS F1): a decompressor is refused by what it writes. `gunzip
// CLAUDE.local.md` with CLAUDE.local.md.gz on the disk writes CLAUDE.local.md;
// `gunzip -N` writes the name the header holds, CLAUDE.md here; `-r` writes
// into every directory below, and sub holds CLAUDE.md.gz; gtar, pax and cpio
// after `cd ..` fill the directory above.
func TestAC5ThroughTheHookADecompressorIsRefusedByWhatItWrites(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	for row, rules := range map[string][]string{
		"gunzip CLAUDE.local.md":           {u120Protected},
		"gunzip -Nf notes.gz":              {u120Protected},
		"gzip -dN notes.gz":                {u120Protected},
		"gunzip -rf sub":                   {u120Protected},
		"gunzip -rf .":                     {u120Protected, u120LoopState},
		"GZIP=-d gzip -f sub/CLAUDE.md.gz": {u120Protected},
		"cd .. && gtar -xf {side}/p.tar":   {agAboveRule},
		"cd .. && pax -r < {side}/p.pax":   {agAboveRule},
		"cd .. && cpio -i < {side}/p.cpio": {agAboveRule},
		"xz -d --files={side}/list.txt":    {u120Protected, u120LoopState, agAboveRule},
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-5 "+row, func(t *testing.T) { u120Refused(t, "AC-5", root, cmd, noEnv, rules...) })
	}
	// To standard output, or a listing, writes nothing.
	for _, cmd := range []string{"gunzip -c notes.gz", "gunzip -Nc notes.gz", "gunzip -l CLAUDE.local.md.gz"} {
		t.Run("AC-5 "+cmd, func(t *testing.T) { u120Allowed(t, "AC-5", root, cmd) })
	}
}

// AC-6 (ANALYSIS F6): a glob is read as the shell reads it. A bare `*` in the
// project's root fills in CLAUDE.md, which is there; in the directory above it
// fills in the person's CLAUDE.md; zsh writes a redirect to every match
// (MULTIOS), so `echo x > *` writes each. zsh's `***/` follows the link
// {side}/pl into the project; `~c` is the named directory c, which zsh takes
// from a variable holding an absolute path. Today a bare `*` names nothing,
// `***` is read as `**` from a directory below nothing, and `~c` is read as
// user c's home, which there is not.
func TestAC6ThroughTheHookAGlobIsReadAsTheShellReadsIt(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	u120Link(t, root, filepath.Join(side, "pl"))
	for row, rule := range map[string]string{
		"rm -f *": u120Protected,
		// EXTENDED_GLOB's `^build` names everything in the root but build,
		// CLAUDE.md among it. Moved here from shellpolicy, which reads no
		// disk and where TestAGlobNamesTheFilesItMatches holds `rm -rf *` allowed.
		"setopt EXTENDED_GLOB; rm -f ^build": u120Protected,
		"truncate -s 0 *":                    u120Protected,
		"echo x > *":                         u120Protected,
		"echo x > **":                        u120Protected,
		"cd .. && echo x > *":                agAboveRule,
		"cd .. && rm -f *":                   agAboveRule,
		"rm -f ../*":                         agAboveRule,
		"rm -f {home}/code/*":                agAboveRule,
		"rm {side}/***/.sdlc/state/active":   u120LoopState,
		"c={root}; echo x > ~c/CLAUDE.md":    u120Protected,
		"c={root}; rm -rf ~c/.sdlc/state":    u120LoopState,
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-6 "+row, func(t *testing.T) { u120Refused(t, "AC-6", root, cmd, noEnv, rule) })
	}
	// glob_test.go:57 and globdir_test.go:27 hold `rm -rf build/*` allowed: a
	// bare `*` in a directory that holds nothing held fills in nothing held.
	for _, cmd := range []string{"rm -rf build/*", "rm -f build/*", "echo x > build/*"} {
		t.Run("AC-6 "+cmd, func(t *testing.T) { u120Allowed(t, "AC-6", root, cmd) })
	}
}

// AC-7 (ANALYSIS F3): a project whose root's name has a dot, me.github.io,
// holds every refusal one without a dot holds. The released reading takes a
// destination whose last name has a dot for a file, so a copy into the root
// by its own name is let through today.
func TestAC7ThroughTheHookARefusalHoldsWhereTheProjectRootHasADot(t *testing.T) {
	root, home := agAboveWorldAt(t, "dev", "code", "me.github.io")
	write(t, root, "CLAUDE.md", "contract\n")
	base := filepath.Dir(home)
	write(t, base, "p/CLAUDE.md", "planted\n")
	write(t, base, "p/.sdlc/state/active", "B-2\n")
	write(t, base, "p/notes.md", "x\n")
	payload := filepath.Join(base, "p")
	for row, rule := range map[string]string{
		"cp {p}/CLAUDE.md ../me.github.io": u120Protected,
		"mv {p}/CLAUDE.md ../me.github.io": u120Protected,
		"cp -r {p}/.sdlc ../me.github.io":  u120LoopState,
		"cp -r {p}/.sdlc {root}":           u120LoopState,
	} {
		cmd := u120Expand(row, root, home, base, payload, base)
		t.Run("AC-7 "+row, func(t *testing.T) { u120Refused(t, "AC-7", root, cmd, noEnv, rule) })
	}
	// Already refused today (AC-10): the root by its absolute name.
	held := u120Expand("cp {p}/CLAUDE.md {root}", root, home, base, payload, base)
	t.Run("AC-7 held", func(t *testing.T) { u120Refused(t, "AC-7", root, held, noEnv, u120Protected) })
	cmd := u120Expand("cp {p}/notes.md ../me.github.io", root, home, base, payload, base)
	t.Run("AC-7 ordinary", func(t *testing.T) { u120Allowed(t, "AC-7", root, cmd) })
}

// AC-8 (ANALYSIS F7): a word that climbs out of .agents is refused under the
// rule for where it lands, with that rule's route, through the hook as through
// shellpolicy.
func TestAC8ThroughTheHookAWordThatClimbsOutOfDotAgentsNamesWhereItLands(t *testing.T) {
	root, _, _, _, _ := u120World(t)
	for cmd, rule := range map[string]string{
		"rm -rf $D/.agents/../.sdlc/state": u120LoopState,
		"echo x > $D/.agents/../CLAUDE.md": u120Protected,
	} {
		t.Run("AC-8 "+cmd, func(t *testing.T) { u120Refused(t, "AC-8", root, cmd, noEnv, rule) })
	}
}

// AC-11 (ANALYSIS F1): shuf -o and split's prefix write the file they name.
func TestAC11ThroughTheHookAProgramThatWritesAFileItNamesIsRefused(t *testing.T) {
	root, _, _, _, _ := u120World(t)
	for cmd, rule := range map[string]string{
		"shuf -o CLAUDE.md build/out.log":          u120Protected,
		"split -l 1 build/out.log CLAUDE.md":       u120Protected,
		"shuf -o .sdlc/state/active build/out.log": u120LoopState,
	} {
		t.Run("AC-11 "+cmd, func(t *testing.T) { u120Refused(t, "AC-11", root, cmd, noEnv, rule) })
	}
	t.Run("AC-11 ordinary", func(t *testing.T) { u120Allowed(t, "AC-11", root, "shuf -o build/shuffled.log build/out.log") })
}

// AC-12 (ANALYSIS F7): a refusal names the place the command can reach. `mv
// build/out.log ..` and `cp -r docs ..` put no instruction file in a directory
// above the project; `rm sub/**/../CLAUDE.md` names only paths in it; and a
// lowercase docs/agents.md or sub/Claude.md, refused by the file tools and the
// shell alike, is said to be matched without regard to case.
func TestAC12ThroughTheHookARefusalNamesThePlaceTheCommandCanReach(t *testing.T) {
	root, _, _, _, _ := u120World(t)
	for _, cmd := range []string{"mv build/out.log ..", "cp -r docs .."} {
		t.Run("AC-12 "+cmd, func(t *testing.T) {
			if r := agShell(t, root, cmd); agRuleOf(r) == agAboveRule {
				t.Errorf("AC-12 %q puts no instruction file above the project and was refused as %s:\n%s", cmd, agAboveRule, reasonOf(r))
			}
		})
	}
	t.Run("AC-12 rm sub/**/../CLAUDE.md", func(t *testing.T) {
		u120Refused(t, "AC-12", root, "rm sub/**/../CLAUDE.md", noEnv, u120Protected)
	})
	for _, rel := range []string{"docs/agents.md", "sub/Claude.md"} {
		t.Run("AC-12 Write "+rel, func(t *testing.T) {
			r := call(t, event(root, "Write", "sdlc-implementer", filepath.Join(root, filepath.FromSlash(rel))), noEnv)
			if agRuleOf(r) != u120Write {
				t.Fatalf("AC-12 Write %s is answered %q; want %s:\n%s", rel, agRuleOf(r), u120Write, reasonOf(r))
			}
			if said := strings.ToLower(reasonOf(r)); !strings.Contains(said, "case") {
				t.Errorf("AC-12 the refusal of Write %s does not say the name is matched without regard to case:\n%s", rel, reasonOf(r))
			}
		})
		t.Run("AC-12 shell "+rel, func(t *testing.T) {
			r := agShell(t, root, "echo x > "+rel)
			if agRuleOf(r) != u120Protected {
				t.Fatalf("AC-12 echo x > %s is answered %q; want %s:\n%s", rel, agRuleOf(r), u120Protected, reasonOf(r))
			}
			if said := strings.ToLower(reasonOf(r)); !strings.Contains(said, "case") {
				t.Errorf("AC-12 the refusal of echo x > %s does not say the name is matched without regard to case:\n%s", rel, reasonOf(r))
			}
		})
	}
	// The real case stays named as it is.
	t.Run("AC-12 control", func(t *testing.T) {
		u120Refused(t, "AC-12", root, "mv build/out.log ../CLAUDE.md", noEnv, agAboveRule)
	})
}

// u120Tar writes a tar archive at path holding each member, so that an
// extraction can be judged by the names its archive holds.
func u120Tar(t *testing.T, path string, members ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for _, name := range members {
		body := []byte("planted\n")
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// AC-10 (US-023 PLAN.md Not done, architect round 3 note 3): an extraction is
// judged by the members its archive holds. `tar -xf p.tar -C .` with a member
// CLAUDE.md overwrites the root's CLAUDE.md, and a member named ../CLAUDE.md
// extracted in sub lands on it too (GNU and BSD tar strip the `../` unless -P
// is given, and then it lands on sub/CLAUDE.md, which is held as well). Today
// the hook reads no archive, so both are let through. An archive holding only
// an ordinary file stays allowed, so the fix is not to refuse every
// extraction; one the hook cannot read is the plan's to decide.
func TestAC10ThroughTheHookAnExtractionIsJudgedByTheMembersItsArchiveHolds(t *testing.T) {
	root, _, _, _, side := u120World(t)
	u120Tar(t, filepath.Join(side, "p.tar"), "CLAUDE.md")
	u120Tar(t, filepath.Join(side, "q.tar"), "../CLAUDE.md")
	u120Tar(t, filepath.Join(side, "n.tar"), "notes/a.txt")
	for name, cmd := range map[string]string{
		"AC-10 a member CLAUDE.md into the root":  "tar -xf " + slash(filepath.Join(side, "p.tar")) + " -C .",
		"AC-10 a member ../CLAUDE.md, run in sub": "cd sub && tar -xf " + slash(filepath.Join(side, "q.tar")),
	} {
		t.Run(name, func(t *testing.T) { u120Refused(t, "AC-10", root, cmd, noEnv, u120Protected) })
	}
	t.Run("AC-10 an archive of an ordinary file", func(t *testing.T) {
		u120Allowed(t, "AC-10", root, "tar -xf "+slash(filepath.Join(side, "n.tar"))+" -C .")
	})
}
