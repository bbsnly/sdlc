package shellpolicy

// US-019, the evasions the design reviews found, in the spellings that need no
// disk. Letting a path placed outside the repository go is only safe if
// everything that reaches the repository from outside stays the repository's.
// Each row is refused today and has to be refused after the story; the lookup is
// the stub of outsiderepository_test.go, which places a path outside wherever it
// is not under /work/project, so a row passes only if the policy refuses it for
// a reason that is not "the stub says outside". The same rows, with real
// directories, links and hard links, are put to the hook in internal/hook.
//
// Not here: what is allowed today and not this story's to change (a reassigned
// HOME or an unknown user read for loop state, a glob in the middle of a path
// read for loop state, a link made and used in one command under another name).

import (
	"slices"
	"testing"
)

const (
	orFrozenTest = "internal/shellpolicy/abbreviated_test.go"
	orFrozenRule = "frozen-test-through-the-tool"
)

// orEvasionRow is a command, the directory it runs from, and the rule it
// belongs to: "" is any of the rules that read a word as a file.
type orEvasionRow struct{ dir, command, rule string }

// orEvasionTargets are what a relative name is trying to mean: a frozen test,
// the loop's own record, and a protected path.
var orEvasionTargets = []struct{ command, rule string }{
	{"rm -f " + orFrozenTest, orFrozenRule},
	{"printf x > .sdlc/state/active", "loop-state-through-the-tool"},
	{"echo x > CLAUDE.md", "protected-path-through-the-tool"},
}

// orEvasionsRefused puts each row to the policy for both agents. The main
// conversation is held to the row's rule where it names one; the implementer is
// refused by whichever of the rules that read a file speaks first.
func orEvasionsRefused(t *testing.T, links map[string]string, rows []orEvasionRow) {
	t.Helper()
	for _, agent := range orWho {
		for _, row := range rows {
			t.Run(agent+" from "+row.dir+" "+row.command, func(t *testing.T) {
				f, ok := Inspect(row.command, orState(agent, row.dir, links))
				if !ok {
					t.Fatalf("allowed: %s", row.command)
				}
				if row.rule != "" && agent == "" {
					if f.Rule != row.rule {
						t.Errorf("refused by %s, want %s: %s", f.Rule, row.rule, f.Reason)
					}
					return
				}
				if !slices.Contains(fileRules, f.Rule) {
					t.Errorf("refused by %s, want one of %v: %s", f.Rule, fileRules, f.Reason)
				}
			})
		}
	}
}

// A `cd` to a directory outside the repository that the shell does not make, or
// does not make for the command that follows, leaves the shell where it was, and
// the relative name after it is the repository's.
func TestAC1AndAC2ACdTheShellDoesNotMakeThenARelativeNameStaysRefused(t *testing.T) {
	var rows []orEvasionRow
	for _, prefix := range []string{
		"cd /tmp/out/gone; ",
		"cd /nonexistent; ",
		"rm -rf /tmp/out/copy; cd /tmp/out/copy; ",
		"rmdir /tmp/out/empty; cd /tmp/out/empty; ",
		"mv /tmp/out/copy /tmp/out/copy2; cd /tmp/out/copy; ",
		"cd /tmp/out | cat; ",
		"cd /tmp/out & ",
		"f() { cd /tmp/out; }; ",
		"if false; then cd /tmp/out; fi; ",
		"while false; do cd /tmp/out; done; ",
		"false && cd /tmp/out; ",
		"true || cd /tmp/out; ",
		"test -d /tmp/out/gone && cd /tmp/out/gone; ",
		"! cd /tmp/out; ",
		"(cd /tmp/out); ",
		"sh -c 'cd /tmp/out'; ",
		"pushd -n /tmp/out; ",
		"cd /tmp/out extra; ",
		"cd /tmp/out/notes.txt; ",
		"cd /tmp/out 2>/dev/null; ",
		"cd /tmp/out\n",
	} {
		for _, target := range orEvasionTargets {
			rows = append(rows, orEvasionRow{"", prefix + target.command, target.rule})
		}
	}
	orEvasionsRefused(t, nil, rows)
}

// A find rooted at a glob is rooted at everything the glob expands to, which
// can be the repository. Only a root written out is outside or not.
func TestAC1AndAC2AGlobAsTheLastSegmentOfAFindRootStaysRefused(t *testing.T) {
	var rows []orEvasionRow
	for _, action := range []string{"-name '*_test*' -delete", "-name active -delete"} {
		rows = append(rows,
			orEvasionRow{"", "find /work/* " + action, ""},
			orEvasionRow{"", "find /work/[a-z]* " + action, ""},
			orEvasionRow{"", "find /work/project* " + action, ""},
			orEvasionRow{"/work", "find * " + action, ""},
			orEvasionRow{orOutside, "find /work/* " + action, ""},
		)
	}
	rows = append(rows,
		orEvasionRow{"", "find /work/{project,x} -name '*_test*' -delete", orFrozenRule},
		orEvasionRow{"", "echo /work/project | xargs -I{} find {} -name '*_test*' -delete", orFrozenRule},
		orEvasionRow{"", "find /tmp/out -follow -name '*_test*' -delete", ""},
		orEvasionRow{"", "find -L /tmp/out -name '*_test*' -delete", ""},
		orEvasionRow{"", "find /tmp/out -L -name '*_test*' -delete", ""},
	)
	orEvasionsRefused(t, nil, rows)
}

// A copy, move or install names the directory it puts files in with an option
// that GNU tools take spelled out, abbreviated, bundled with others and with
// its operand attached. Each of these lands an outside source on a frozen test.
func TestAC2ATargetDirectorySpellingCopyingAnOutsideSourceOverAFrozenTestStaysRefused(t *testing.T) {
	var rows []orEvasionRow
	for _, command := range []string{
		"cp --target=internal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp --t=internal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp --target internal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp --target-directory=internal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp -rtinternal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp -tinternal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp -Zt internal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp -vt internal/shellpolicy /tmp/out/abbreviated_test.go",
		"cp -S .bak -t internal/shellpolicy /tmp/out/abbreviated_test.go",
		"mv --target=internal/shellpolicy /tmp/out/abbreviated_test.go",
		"mv --target-directory=internal/shellpolicy /tmp/out/abbreviated_test.go",
		"mv -Zt internal/shellpolicy /tmp/out/abbreviated_test.go",
		"mv -ft internal/shellpolicy /tmp/out/abbreviated_test.go",
		"mv -ftinternal/shellpolicy /tmp/out/abbreviated_test.go",
		"install --target=internal/shellpolicy /tmp/out/abbreviated_test.go",
		"install -Ct internal/shellpolicy /tmp/out/abbreviated_test.go",
		"install -m644 -t internal/shellpolicy /tmp/out/abbreviated_test.go",
	} {
		rows = append(rows, orEvasionRow{"", command, ""})
	}
	orEvasionsRefused(t, nil, rows)
}

// A link made in the command does not exist when the policy reads it, so a word
// through it reads as outside. Nothing may be taken for outside because of a
// command that could have made the link, however it is spelled.
func TestAC1AnAlternateSpellingOfALinkMakerThenAWriteThroughTheOutsideNameStaysRefused(t *testing.T) {
	const through = " && echo x > /tmp/out/l/abbreviated_test.go"
	var rows []orEvasionRow
	for _, command := range []string{
		`\ln -s /work/project/internal /tmp/out/l` + through,
		`gln -s /work/project/internal /tmp/out/l` + through,
		`l''n -s /work/project/internal /tmp/out/l` + through,
		`L=ln; $L -s /work/project/internal /tmp/out/l; echo x > /tmp/out/l/abbreviated_test.go`,
		`cp -a /tmp/out/in /tmp/out/l` + through,
		`cp -P /tmp/out/in /tmp/out/l` + through,
		`cp -rs /work/project/internal /tmp/out/l` + through,
		`cp --symbolic-link -r /work/project/internal /tmp/out/l` + through,
		`rsync -a /tmp/out/in /tmp/out/l` + through,
		`tar -xf /tmp/out/links.tar -C /tmp/out` + through,
		`python3 -c 'import os; os.rename("/tmp/out/in", "/tmp/out/l")'; echo x > /tmp/out/l/abbreviated_test.go`,
		`mv /tmp/out/in /tmp/out/l` + through,
		`link /work/project/` + orFrozenTest + ` /tmp/out/hl2_test.go && echo x > /tmp/out/hl2_test.go`,
	} {
		rows = append(rows, orEvasionRow{"", command, ""})
	}
	orEvasionsRefused(t, nil, rows)
}

// A tilde word is a path the policy cannot place: HOME is the shell's to change
// in the command, a named user is looked up by the shell, and `~PWD` is a user's
// directory and not a variable.
func TestAC1AAReassignedHomeOrAUserThePolicyCannotPlaceStaysRefused(t *testing.T) {
	orEvasionsRefused(t, nil, []orEvasionRow{
		{"", "export HOME=/work/project && rm -f ~/" + orFrozenTest, orFrozenRule},
		{"", "export USERPROFILE=/work/project && rm -f ~/" + orFrozenTest, orFrozenRule},
		{"", "HOME=/work/project; rm -f ~/" + orFrozenTest, orFrozenRule},
		{"", "export HOME=/work/project && echo x > ~/internal/shellpolicy/brand_new_test.go", ""},
		{"", "rm -f ~nosuchuserzz/" + orFrozenTest, orFrozenRule},
		{"", "rm -f ~PWD/" + orFrozenTest, orFrozenRule},
		{"", "rm -f ~OLDPWD/" + orFrozenTest, orFrozenRule},
		{"", "R=/work/project; rm -f ~R/" + orFrozenTest, orFrozenRule},
	})
}

// `..` after a directory change through a link is the link's target's parent in
// a shell that resolves links first, and the link's parent in one that does not.
// A program that changes directory itself does the first.
func TestAC1ARelativeDotDotAfterALinkStaysRefused(t *testing.T) {
	links := map[string]string{"/tmp/out/in": "internal"}
	var rows []orEvasionRow
	for _, command := range []string{
		"cd /tmp/out/in && cd .. && rm -f " + orFrozenTest,
		"cd /tmp/out/in && env -C .. rm -f " + orFrozenTest,
		"cd /tmp/out/in && sudo -D .. rm -f " + orFrozenTest,
		"env -C /tmp/out/in rm -f ../" + orFrozenTest,
		"set -P; cd /tmp/out/in; cd ..; rm -f " + orFrozenTest,
		"set -o physical; cd /tmp/out/in; cd ..; rm -f " + orFrozenTest,
		"set -w; cd /tmp/out/in; cd ..; rm -f " + orFrozenTest,
		"cd -P /tmp/out/in && cd .. && rm -f " + orFrozenTest,
		"cd -Pe /tmp/out/in && cd .. && rm -f " + orFrozenTest,
		"cd -LP /tmp/out/in && cd .. && rm -f " + orFrozenTest,
		"cd -L -P /tmp/out/in && cd .. && rm -f " + orFrozenTest,
		"builtin cd -P /tmp/out/in && cd .. && rm -f " + orFrozenTest,
		"env -C /tmp/out/gone rm -f " + orFrozenTest,
	} {
		rows = append(rows, orEvasionRow{"", command, orFrozenRule})
	}
	orEvasionsRefused(t, links, rows)
}

// BSD sed (macOS) reads the word after -i as a backup suffix and the next word
// as its script, and a script that ends in `w` followed by a file name writes
// that file: `sed -i s/a/b/ /tmp/out/w/work/project/...` is a file outside the
// repository to GNU sed and a frozen test to BSD sed.
func TestAC1AndAC2ABSDSedWriteCommandInTheFirstFileWordStaysRefused(t *testing.T) {
	orEvasionsRefused(t, nil, []orEvasionRow{
		{"", "sed -i s/a/b/ /tmp/out/w/work/project/" + orFrozenTest, orFrozenRule},
		{orOutside, "sed -i s/a/b/ w/work/project/" + orFrozenTest, orFrozenRule},
		{"", "sed -i s/a/b/ /tmp/out/W/work/project/" + orFrozenTest, orFrozenRule},
	})
}
