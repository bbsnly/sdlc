// US-093 end to end: the stop's refusal for a merge or diff driver whose name
// git's command line cannot carry. A session works a story in its worktree
// through every gate as the runbook directs, with a diff driver of such a name
// already in the repository's configuration: the commit gate passes, as it
// must, and it is the stop that refuses, on trunk where it was and on trunk
// moved. The refusal names the key, says the rule is sdlc's and not git's, and
// gives a fix that is to rename or remove the driver; a merge driver of such a
// name gets the same words once trunk has moved, for two names. Each refused
// stop leaves trunk, the story's branch and its worktree as they were, and
// removing the driver is all the way back: the stop then lands the story
// (AC-1, AC-2). docs/troubleshooting.md quotes what the binary printed under
// SDLC-E0055 (AC-3).
//
// The words are held as the criteria require them and as the cli's
// landing_driverkeys_test.go holds them: the key, whose rule it is, the fix,
// what it does not talk about, and the page quoting each of what, why and fix
// with a placeholder where the run's own key, path, commit or story stands.
// Helpers carry a dke prefix, for driver keys end to end, and use
// landing_ontrunk_test.go's and landing_conditions_test.go's.

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// dkeRefusal is a refusal as the binary printed it at the end of its output.
type dkeRefusal struct{ out, what, why, fix string }

// dkeParse splits the refusal at the end of out, after any progress line the
// stop wrote before it, into what, why and fix, and holds its code to
// SDLC-E0055.
func dkeParse(t *testing.T, out string) dkeRefusal {
	t.Helper()
	whyAt := strings.LastIndex(out, "\n\n  why  ")
	if whyAt < 0 {
		t.Fatalf("no refusal in what the stop printed:\n%s", out)
	}
	start := strings.LastIndex(out[:whyAt], "sdlc: ")
	rest := out[whyAt+len("\n\n  why  "):]
	why, afterWhy, ok := strings.Cut(rest, "\n  fix  ")
	if start < 0 || !ok {
		t.Fatalf("the refusal is not in the form sdlc prints:\n%s", out)
	}
	fix, tail, _ := strings.Cut(afterWhy, "\n\n")
	if !strings.HasPrefix(strings.TrimSpace(tail), "SDLC-E0055 ") {
		t.Fatalf("the refusal does not end with SDLC-E0055:\n%s", out)
	}
	return dkeRefusal{
		out:  out,
		what: strings.TrimSpace(out[start+len("sdlc: ") : whyAt]),
		why:  strings.TrimSpace(why),
		fix:  strings.TrimSpace(fix),
	}
}

func dkeFlat(text string) string { return strings.Join(strings.Fields(text), " ") }

var (
	dkeSdlc     = regexp.MustCompile(`\bsdlc\b`)
	dkeGit      = regexp.MustCompile(`\bgit\b`)
	dkeNot      = regexp.MustCompile(`\b(not|no|never|nor|isn't|aren't)\b`)
	dkeGitFrame = regexp.MustCompile(`\bgit (refus|reject|would not|will not|won't|cannot|can't|could not|did not|said|reported|reports|says|stopped|failed|complain|printed|told)`)
	dkeDenial   = regexp.MustCompile(`\b(not|nor|isn't|never)\b( something| what| anything)? git (said|reported|printed|told|refused|rejected)`)
	dkeEdits    = regexp.MustCompile(`rename|remove|delete|unset|edit|change|config`)
	dkeClauses  = regexp.MustCompile(`[.;] `)
)

const dkePrivate = "{sdlc-private}"

// dkeHolds holds a refusal to what AC-1 and AC-2 require of it for the key:
// it names the key, quoted; says the rule is sdlc's own and not git's, in
// whatever words, with the key and the placeholder's text (which holds the word
// sdlc) taken out of what is read; its fix is for a person to rename or remove
// the driver and for an assistant to hand the story to a person; and nothing in
// it is about a committer or a file in a directory's place. Every failure is
// reported once, with the refusal.
func dkeHolds(t *testing.T, r dkeRefusal, key string) {
	t.Helper()
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	body := dkeFlat(r.what + " " + r.why)
	if !strings.Contains(body, strconv.Quote(key)) {
		bad("it does not name the key %s, quoted", key)
	}
	if strings.Contains(r.fix, key) {
		bad("its fix has the key in it")
	}
	lower := strings.ToLower(body)
	for _, own := range []string{strconv.Quote(key), key, strconv.Quote(dkePrivate), dkePrivate} {
		lower = strings.ReplaceAll(lower, strings.ToLower(own), " ")
	}
	var denies bool
	for _, sentence := range dkeClauses.Split(lower, -1) {
		denies = denies || (dkeSdlc.MatchString(sentence) && dkeGit.MatchString(sentence) && dkeNot.MatchString(sentence))
	}
	if !dkeSdlc.MatchString(lower) || !denies {
		bad("it does not say the rule is sdlc's own and not git's (a sentence with sdlc, git and a denial, outside the placeholder and the key)")
	}
	if frame := dkeGitFrame.FindString(dkeDenial.ReplaceAllString(lower, "")); frame != "" {
		bad("it puts sdlc's own rule in git's mouth with %q", frame)
	}
	fix := strings.ToLower(dkeFlat(r.fix))
	if !strings.Contains(fix, `"sdlc escalate"`) {
		bad("its fix does not say \"sdlc escalate\"")
	}
	var persons []string
	for _, clause := range dkeClauses.Split(fix, -1) {
		if !strings.Contains(clause, "assistant") {
			persons = append(persons, clause)
			continue
		}
		if dkeEdits.MatchString(clause) || !strings.Contains(clause, "sdlc escalate") {
			bad("the assistant's clause of the fix is more than escalation: %q", clause)
		}
	}
	if said := strings.Join(persons, "; "); !strings.Contains(said, "person") ||
		!strings.Contains(said, "rename") || !strings.Contains(said, "remove") {
		bad("its fix does not say that a person renames or removes the driver, apart from the assistant's clause")
	}
	// A path is left out of what is read: a temporary directory is named for the test that made it.
	var prose []string
	for w := range strings.FieldsSeq(strings.ToLower(r.what + "\n" + r.why + "\n" + r.fix)) {
		if !strings.Contains(w, "/") {
			prose = append(prose, w)
		}
	}
	all := strings.Join(prose, " ")
	for _, not := range []string{"committer", "user.name", "user.email", "directory against", "file in a directory", "sdlc approve", "sdlc ack", "unfreeze"} {
		if strings.Contains(all, not) {
			bad("it talks of %q, which has nothing to do with a driver's name", not)
		}
	}
	if strings.Count(r.out, "\n  fix  ") != 1 || strings.Count(r.out, "\n  why  ") != 1 {
		bad("the output has more than the refusal's one why and one fix line: the key forged one")
	}
	if strings.ContainsRune(r.out, rune(0x202e)) {
		bad("the output holds a right-to-left override as it stands, and does not show it escaped")
	}
	if len(problems) > 0 {
		t.Errorf("the refusal for %s is not what AC-1 and AC-2 require:\n- %s\nit reads:\n%s", key, strings.Join(problems, "\n- "), r.out)
	}
}

// dkeLeft holds the story to being where it was after a refused stop: trunk at
// trunk, the branch at tip, nothing part-way or changed in its worktree, and
// the story its session's still.
func dkeLeft(t *testing.T, s *ontoStory, main checkout, trunk, tip, when string) {
	t.Helper()
	if got := landTrunk(t, main); got != trunk {
		t.Errorf("%s trunk moved from %s to %s", when, trunk, got)
	}
	if got := headOf(t, s.tree); got != tip {
		t.Errorf("%s the story's tip moved from %s to %s", when, tip, got)
	}
	if got := gitOut(t, s.tree, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Errorf("%s the story's worktree is not clean:\n%s", when, got)
	}
	if got := gitOut(t, s.tree, "symbolic-ref", "--short", "HEAD"); got != "story/US-001" {
		t.Errorf("%s the worktree is on %q, want story/US-001", when, got)
	}
	heldWithItsSession(t, s, main, when)
}

// dkePattern is one part of a refusal as the page has to quote it: a word that
// belongs to the run that printed it -- the key, a path, a commit, the story,
// the trunk branch, which kind of driver -- may be any short stretch of words,
// and every other word is as the binary prints it.
var dkeOwn = regexp.MustCompile("(?i)^[`'\"(]*(/|[a-z]:\\\\)|us-001|[0-9a-f]{12,}")

func dkePattern(part string, keys ...string) *regexp.Regexp {
	const placeholder = `\S+(?: \S+){0,3}?`
	var words []string
	for w := range strings.FieldsSeq(part) {
		bare := strings.ToLower(strings.Trim(w, "\"'`.,;:()"))
		own := bare == "main" || bare == "merge" || bare == "diff" || dkeOwn.MatchString(w)
		for _, k := range keys {
			own = own || strings.Contains(w, k)
		}
		if own {
			words = append(words, placeholder)
			continue
		}
		words = append(words, regexp.QuoteMeta(w))
	}
	return regexp.MustCompile(strings.Join(words, " "))
}

// dkeSection is the part of page under heading, up to the next heading of its
// level or above, with a block quote's marks gone and white space made one
// space.
func dkeSection(page, heading string) string {
	lines := strings.Split(page, "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return l == heading })
	if start < 0 {
		return ""
	}
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	var kept []string
	for _, line := range lines[start+1:] {
		hashes := len(line) - len(strings.TrimLeft(line, "#"))
		if hashes > 0 && hashes <= level && strings.HasPrefix(line[hashes:], " ") {
			break
		}
		kept = append(kept, strings.TrimPrefix(strings.TrimSpace(line), ">"))
	}
	return dkeFlat(strings.Join(kept, "\n"))
}

// dkeStory is a story worked by a session in its worktree, the stop that is to
// refuse it, and what the stop has to leave as it was.
type dkeStory struct {
	t     *testing.T
	main  checkout
	s     *ontoStory
	trunk string
	tip   string
}

// dkeWorked is US-001 worked through every gate and its bookkeeper's commit
// made, in a repository to which beforeTheGates may give a driver that the
// commit gate then meets.
func dkeWorked(t *testing.T, beforeTheGates func(main checkout)) *dkeStory {
	t.Helper()
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	// A driver put in the configuration before the gates is met by the commit gate: it has to
	// pass, and s.worked fails the test where it does not.
	beforeTheGates(main)
	s.worked()
	landBookkeeping(t, s.treeSession, true)
	return &dkeStory{t: t, main: main, s: s, trunk: landTrunk(t, main), tip: headOf(t, s.tree)}
}

// moveTrunk is another story landing: trunk moves on by a commit to a file of
// its own.
func (d *dkeStory) moveTrunk() {
	d.t.Helper()
	writeFile(d.t, filepath.Join(d.main.dir, "CHANGES.md"), "another story's work\n")
	d.trunk = commitPaths(d.t, d.main.dir, "another story lands", "CHANGES.md")
}

// stop runs the stop as the session does, asking the hook first, and returns
// what it printed and its exit code.
func (d *dkeStory) stop() (string, int) {
	d.t.Helper()
	d.s.do("", "Bash", running(d.s.at.in(d.s.tree, "sdlc stop")))
	return d.s.here().try(d.t, "stop")
}

// refused is a stop that has to be refused for key, and to leave everything
// as it was. It ends the test where the stop landed the story, since there is
// then no worktree left to go on with.
func (d *dkeStory) refused(key, when string) dkeRefusal {
	d.t.Helper()
	out, code := d.stop()
	if code == 0 || !strings.Contains(out, "SDLC-E0055") {
		d.t.Fatalf("%s the stop exited %d, want SDLC-E0055 naming %s:\n%s", when, code, key, out)
	}
	r := dkeParse(d.t, out)
	dkeHolds(d.t, r, key)
	dkeLeft(d.t, d.s, d.main, d.trunk, d.tip, when)
	return r
}

// lands is a stop that has to land the story onto the trunk as it then is.
func (d *dkeStory) lands(when string) {
	d.t.Helper()
	out, code := d.stop()
	if code != 0 {
		d.t.Fatalf("%s the stop exited %d, want the story landed:\n%s", when, code, out)
	}
	for _, f := range []string{d.s.source, d.s.test} {
		if !ontoHolds(d.t, d.main, f) {
			d.t.Errorf("%s trunk does not hold %s after the stop", when, f)
		}
	}
	if got := landTrunk(d.t, d.main); got == d.trunk {
		d.t.Errorf("%s trunk is still at %s: nothing landed", when, got)
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: the story is worked with `diff.st=eer.algorithm` already in the
// repository's configuration, and every gate passes, the commit gate among
// them: it is the stop that refuses. It refuses with trunk where it was, which
// is every stop and not only one that puts the story onto a trunk that moved,
// and again with trunk moved, in the same words. Taking the driver away is all
// the way back: the stop then lands the story.
func TestAC2ADiffDriverNamedBeforeTheGatesLetsTheCommitGatePassAndTheStopRefusesOnEveryStop(t *testing.T) {
	parallel(t)
	const key = "diff.st=eer.algorithm"
	d := dkeWorked(t, func(main checkout) { git(t, main.dir, "config", "--", key, "patience") })

	first := d.refused(key, "with trunk where it was,")
	d.moveTrunk()
	second := d.refused(key, "with trunk moved,")
	if dkeFlat(first.what+first.why+first.fix) != dkeFlat(second.what+second.why+second.fix) {
		t.Errorf("the same refusal twice reads two ways:\n%s\n%s", first.out, second.out)
	}

	git(t, d.main.dir, "config", "--unset-all", "--", key)
	d.lands("with the diff driver taken away,")
	if got := statuses(t, d.main.backlog())["US-001"]; got != "done" {
		t.Errorf("US-001 is %s after the stop, want done", got)
	}
}

// ---------------------------------------------------------------- AC-1, AC-2 and AC-3

// AC-1, AC-2 and AC-3: with trunk moved, a merge driver named `st=eer`, and one
// named `{sdlc-private}`, each make the stop refuse naming its key and saying
// the rule is sdlc's, and so does a diff driver named `st=eer`, in the same
// words. docs/troubleshooting.md quotes the binary's text for all three under
// SDLC-E0055. With every driver taken away the stop lands the story, so
// nothing but the drivers' names was wrong.
func TestAC1AndAC2AndAC3TheStopRefusesADriverNameGitsCommandLineCannotCarryInSdlcsOwnWordsAndTheDocsQuoteThem(t *testing.T) {
	parallel(t)
	d := dkeWorked(t, func(checkout) {})
	d.moveTrunk()

	const mergeEqualsKey, mergePrivateKey, diffKey = "merge.st=eer.driver", "merge.{sdlc-private}.driver", "diff.st=eer.algorithm"
	git(t, d.main.dir, "config", "--", mergeEqualsKey, "true %O %A %B")
	mergeEquals := d.refused(mergeEqualsKey, "with a merge driver named st=eer,")
	git(t, d.main.dir, "config", "--unset-all", "--", mergeEqualsKey)
	git(t, d.main.dir, "config", "--", mergePrivateKey, "true %O %A %B")
	mergePrivate := d.refused(mergePrivateKey, "with a merge driver named {sdlc-private},")
	git(t, d.main.dir, "config", "--unset-all", "--", mergePrivateKey)

	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "troubleshooting.md"))
	if err != nil {
		t.Fatal(err)
	}
	page := dkeSection(string(raw), "### SDLC-E0055")
	if page == "" {
		t.Fatal("docs/troubleshooting.md has no section for SDLC-E0055")
	}
	quoted := func(what string, r dkeRefusal, key, name string) {
		t.Helper()
		for _, part := range []struct{ what, text string }{{"what", r.what}, {"why", r.why}, {"fix", r.fix}} {
			if strings.TrimSpace(part.text) == "" {
				t.Fatalf("the refusal for %s has no %s", what, part.what)
			}
			if !dkePattern(part.text, key, name).MatchString(page) {
				t.Errorf("the page's section for SDLC-E0055 does not quote the %s of the refusal for %s as the binary printed it "+
					"(a placeholder may stand for the key, a path, a commit, the story or the trunk branch):\n%s",
					part.what, what, part.text)
			}
		}
		// The three parts stand together, in this order, as one block, and are not three quotations from
		// three places on the page.
		var parts []string
		for _, text := range []string{r.what, r.why, r.fix} {
			parts = append(parts, dkePattern(text, key, name).String())
		}
		if !regexp.MustCompile(strings.Join(parts, `(?: \S+){0,3} `)).MatchString(page) {
			t.Errorf("the page's section for SDLC-E0055 does not hold the what, the why and the fix of the refusal for %s "+
				"together, in that order, in one place:\n%s\n%s\n%s", what, r.what, r.why, r.fix)
		}
	}
	quoted("the merge driver named st=eer", mergeEquals, mergeEqualsKey, "st=eer")
	quoted("the merge driver named {sdlc-private}", mergePrivate, mergePrivateKey, "{sdlc-private}")

	git(t, d.main.dir, "config", "--", diffKey, "patience")
	diff := d.refused(diffKey, "with a diff driver named st=eer,")
	git(t, d.main.dir, "config", "--unset-all", "--", diffKey)
	quoted("the diff driver named st=eer", diff, diffKey, "st=eer")

	// The same words for the diff key as for the merge key, apart from the key and which kind it is.
	words := func(r dkeRefusal, key string) string {
		text := strings.ReplaceAll(r.what+" "+r.why+" "+r.fix, strconv.Quote(key), "KEY")
		return regexp.MustCompile(`(?i)\b(merge|diff)\b`).ReplaceAllString(dkeFlat(text), "X")
	}
	if got, want := words(diff, diffKey), words(mergeEquals, mergeEqualsKey); got != want {
		t.Errorf("the diff driver's refusal is not in the merge driver's words:\nmerge: %s\ndiff:  %s", want, got)
	}

	d.lands("with every driver taken away,")
}

// ---------------------------------------------------------------- AC-1, locale

// dkeUTF8Locale is a UTF-8 locale of this machine in which git leaves a merge
// driver with a byte that is not UTF-8 in its name out of the keys it lists,
// which is where a listing that git matches in the caller's locale loses the
// key; it is empty where there is none.
func dkeUTF8Locale(t *testing.T) string {
	t.Helper()
	for _, loc := range []string{"en_US.UTF-8", "C.UTF-8", "en_GB.UTF-8", "en_US.utf8", "C.utf8"} {
		dir := t.TempDir()
		git(t, dir, "init", "--quiet")
		dkeAppendConfig(t, dir, "[merge \"st\xffeer\"]\n\tdriver = true\n")
		cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "config", "-z", "--get-regexp", `^merge\..*\.driver$`)
		cmd.Env = append(os.Environ(), "LC_ALL="+loc, "LANG="+loc)
		if out, _ := cmd.Output(); !bytes.Contains(out, []byte("st\xffeer")) {
			return loc
		}
	}
	return ""
}

// dkeAppendConfig adds text, bytes as they are, to the repository configuration
// in dir.
func dkeAppendConfig(t *testing.T, dir, text string) {
	t.Helper()
	path := filepath.Join(dir, ".git", "config")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, text...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// AC-1: with a merge driver named with a byte that is not UTF-8 in the
// configuration, and the process in a UTF-8 locale, the stop does not run the
// driver, and where the name also holds an equals sign it refuses with SDLC-E0055
// in sdlc's words. A listing that git matches in the caller's locale left such a
// key out, so that the repository's driver ran in the stop. Trunk adds the
// story's source file with other text, so that the rebase has a file to merge by
// the driver the attribute names.
func TestAC1AMergeDriverNamedWithBytesThatAreNotUTF8NeverRunsInTheStopWhateverTheLocale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver here is a shell script, and a key with a byte that is not UTF-8 cannot survive Windows' argument conversion")
	}
	loc := dkeUTF8Locale(t)
	if loc == "" {
		t.Skip("this machine has no UTF-8 locale in which git leaves a key with a byte that is not UTF-8 out of its listing, so there is nothing to reproduce")
	}
	for _, tc := range []struct {
		label, name string
		refused     bool
	}{
		{"a byte that is not UTF-8", "st\xffeer", false},
		{"a byte that is not UTF-8 and an equals sign", "st\xff=eer", true},
	} {
		t.Run("AC-1 in "+loc+", "+tc.label, func(t *testing.T) {
			t.Setenv("LC_ALL", loc)
			t.Setenv("LANG", loc)
			d := dkeWorked(t, func(checkout) {})
			mark := filepath.Join(t.TempDir(), "driver-ran")
			script := filepath.Join(t.TempDir(), "driver.sh")
			writeFile(t, script, "#!/bin/sh\n: > '"+mark+"'\ncat \"$3\" > \"$2\"\nexit 0\n")
			if err := os.Chmod(script, 0o755); err != nil {
				t.Fatal(err)
			}
			dkeAppendConfig(t, d.main.dir, "[merge \""+tc.name+"\"]\n\tdriver = '"+script+"' %O %A %B\n")
			writeFile(t, filepath.Join(d.main.dir, ".git", "info", "attributes"), d.s.source+" merge="+tc.name+"\n")
			writeFile(t, filepath.Join(d.main.dir, d.s.source), "package main\n\n// another story's text, in the same file\n")
			d.trunk = commitPaths(t, d.main.dir, "another story lands in the same file", d.s.source)

			if tc.refused {
				d.refused("merge."+tc.name+".driver", "in a UTF-8 locale,")
			} else {
				d.stop()
			}
			if _, err := os.Stat(mark); err == nil {
				t.Errorf("the repository's merge driver for %q ran in the stop", tc.name)
			}
		})
	}
}
