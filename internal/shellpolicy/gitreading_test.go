package shellpolicy

// US-029.2, decisions 4 and 6 and T1/T4: git runs more than its words show.
// These hold the reader in gitcalls.go to what git itself would run: the
// settings that move a checkout, what it cannot read and says so, the
// configuration channels an alias arrives through, and the variables each
// command runs with. worktree_test.go holds the evasions; these hold the
// readings each answer rests on.

import (
	"slices"
	"strings"
	"testing"
)

// grRefusedSaying holds a command to a refusal by rule whose reason says part.
func grRefusedSaying(t *testing.T, command string, s State, rule, part string) {
	t.Helper()
	f, ok := inspect(t, command, s)
	switch {
	case !ok:
		t.Errorf("allowed: %s", command)
	case f.Rule != rule:
		t.Errorf("%s was refused by %s, want %s (%s)", command, f.Rule, rule, f.Reason)
	case !strings.Contains(f.Reason, part):
		t.Errorf("%s: the reason does not say %q: %s", command, part, f.Reason)
	case f.Route == "":
		t.Errorf("%s: finding = %+v", command, f)
	}
}

// AC-6 and T4: core.worktree and core.bare decide which directory a checkout
// works in, so changing either, in any scope and by any of git config's
// spellings old and new, is refused, and reading them is not.
func TestAC6ChangingTheSettingsThatMoveACheckoutIsRefusedAndReadingThemIsNot(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, tc := range []struct{ command, key string }{
		{"git config core.worktree /work/main", "`core.worktree`"},
		{"git config --local core.WorkTree /work/main", "`core.WorkTree`"},
		{"git config --file .git/config core.worktree /work/main", "`core.worktree`"},
		{"git config --file=x core.worktree /work/main", "`core.worktree`"},
		{"git config --type=bool core.bare true", "`core.bare`"},
		{"git config --type bool core.bare true", "`core.bare`"},
		{"git config -- core.worktree /work/main", "`core.worktree`"},
		{"git config core.bare true", "`core.bare`"},
		{"git config set core.bare true", "`core.bare`"},
		{"git config --add core.worktree /work/main", "`core.worktree`"},
		{"git config --unset core.worktree", "`core.worktree`"},
		{"git config --unset-all core.worktree", "`core.worktree`"},
		{"git config unset core.worktree", "`core.worktree`"},
		{"git config --remove-section core", "`core`"},
		{"git config remove-section core", "`core`"},
		{"git config --rename-section core other", "`core`"},
		{"git config rename-section old core", "`core`"},
		{"git -C /work/main config core.bare true", "`core.bare`"},
	} {
		f, ok := inspect(t, tc.command, s)
		switch {
		case !ok:
			t.Errorf("allowed: %s", tc.command)
		case f.Rule != "worktree-setting-through-the-tool":
			t.Errorf("%s was refused by %s, want worktree-setting-through-the-tool (%s)", tc.command, f.Rule, f.Reason)
		case !strings.Contains(f.Reason, "changing "+tc.key):
			t.Errorf("%s: the reason does not name %s: %s", tc.command, tc.key, f.Reason)
		case !strings.Contains(f.Route, "a person changes core.worktree and core.bare"):
			t.Errorf("%s: route = %s", tc.command, f.Route)
		}
	}
	for _, command := range []string{
		"git config core.worktree",
		"git config --get core.worktree",
		"git config get core.bare",
		"git config --list",
		"git config list --show-origin",
		"git config user.name me",
		"git config --unset user.email",
		"git config --rename-section old new",
		"git config --remove-section alias",
		"git config --rename-section core",
		"git config unset",
		"git config --file",
		`git config user.name "$USER"`,
	} {
		allowed(t, command, s)
	}
}

// AC-6 and decision 4: what the hook cannot read it cannot judge, and every
// other git rule would let it pass unread. Each refusal says which part it
// could not read.
func TestAC6AGitCommandTheHookCannotReadIsRefusedSayingWhichPart(t *testing.T) {
	s := withShared(ready, sharedLandings)
	const unread = "git-command-cannot-be-read"
	for _, tc := range []struct{ command, part string }{
		// A subcommand, or a setting's name, the shell has yet to expand.
		{"git $CMD prune", "the subcommand `$CMD`, which the shell has yet to expand"},
		{`git "$(echo worktree)" prune`, "the subcommand `$(...)`"},
		{`git -c "$K=x" status`, "the setting `$K`, whose name the shell has yet to expand"},
		{`git config "$K" x`, "the setting `$K`, whose name the shell has yet to expand"},
		// An alias whose value cannot be read, wherever it is set.
		{`git -c alias.x="$V" x`, "the alias `x`, whose value the shell has yet to expand"},
		{`git -c alias.x="$(cat f)" x`, "the alias `x`, whose value the shell has yet to expand"},
		{"git --config-env alias.x=V x", "the alias `x`, whose value the shell has yet to expand"},
		{"git --config-env=alias.x='$V' x", "the alias `x`, whose value the shell has yet to expand"},
		{`git config alias.x "$V"`, "the alias `alias.x`, whose value the shell has yet to expand"},
		{`git config alias.x "$V" || true`, "the alias `alias.x`"},
		{`GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=alias.x GIT_CONFIG_VALUE_0="$V" git x`, "the alias `x`, whose value"},
		// Aliases that lead back to themselves, or deeper than the rules read.
		{"git -c alias.x=x x", "the alias `x`, which leads back to itself"},
		{"git -c alias.x=y -c alias.y=x x", "which leads back to itself"},
		{"git -c alias.a=b -c alias.b=c -c alias.c=d -c alias.d=e -c alias.e=status a",
			"one of aliases nested deeper than the rules read"},
		{`git config alias.a '!git config alias.b "!git config alias.c \"!git config alias.d x\""'`,
			"an alias nested deeper than the rules read"},
		// Includes, and settings files the hook does not read.
		{"git -c include.path=/tmp/x status", "`include.path`, an include"},
		{"git -c includeIf.onbranch:x.path=/tmp/x status", "an include"},
		{"git config include.path /tmp/x", "`include.path`, an include"},
		{"git config set include.path x", "an include"},
		{"git config --global include.path ~/.gitconfig.local", "an include"},
		{"git config includeIf.gitdir:/x/.path /tmp/x", "an include"},
		{"git config --rename-section old include", "`include`, an include"},
		{"git config --rename-section includeIf.x includeIf.y", "`includeIf.y`, an include"},
		{"GIT_CONFIG_GLOBAL=/tmp/g git status", "GIT_CONFIG_GLOBAL, which points git at a settings file"},
		{"GIT_CONFIG_SYSTEM=/etc/gitconfig git status", "GIT_CONFIG_SYSTEM"},
		{"GIT_CONFIG=/tmp/c git status", "GIT_CONFIG, which points"},
		// GIT_CONFIG_PARAMETERS that the shell has yet to expand, or git would
		// not read.
		{`GIT_CONFIG_PARAMETERS="$P" git status`, "GIT_CONFIG_PARAMETERS"},
		{`GIT_CONFIG_PARAMETERS="garbage" git status`, "GIT_CONFIG_PARAMETERS"},
		{`GIT_CONFIG_PARAMETERS="'alias.x'=bad" git status`, "GIT_CONFIG_PARAMETERS"},
		{`GIT_CONFIG_PARAMETERS="'unterminated" git status`, "GIT_CONFIG_PARAMETERS"},
	} {
		grRefusedSaying(t, tc.command, s, unread, tc.part)
	}
	// A brace list is expanded and each word read; one longer than the rules
	// read is not a subcommand they can name.
	long := "git {" + strings.Repeat("a,", 70) + "b} prune"
	grRefusedSaying(t, long, s, unread, "a brace list longer than the rules read")
	// A program the shell has yet to expand is read as git, and the word after
	// it as git's subcommand.
	f, ok := inspect(t, "$X $Y", s)
	if !ok || f.Rule != unread || !strings.Contains(f.Reason, "`$X` is not expanded yet") ||
		!strings.Contains(f.Route, "name the program itself") {
		t.Errorf("$X $Y = %+v, %v; want it refused as a program read as git", f, ok)
	}
	// The decoded reading of a command still refuses what the undecoded one
	// leaves to it.
	for _, command := range []string{
		`git config alias.x "$V"; echo $'a'`,
		`git config "$K" x; echo $'a'`,
		`git $CMD prune; echo $'a'`,
	} {
		grRefusedSaying(t, command, s, unread, "shell has yet to expand")
	}

	// What reads cleanly is not refused for being unusual.
	for _, command := range []string{
		"GIT_CONFIG_GLOBAL=/dev/null git status",
		"GIT_CONFIG_SYSTEM=NUL git status",
		"git -c alias.a=b -c alias.b=c -c alias.c=d -c alias.d=status a",
		"git {status,log}",
		"$G status",
		"git config --remove-section includeIf.onbranch:main",
		"git -c alias.x status",
		"git --config-env=alias.x x",
		"git -c alias.wl='worktree list' wl",
		"git -c alias.x='!git worktree list' x",
		"git -c alias.x='!git log' x --grep='a b'",
		"git -c alias.x='!git status' x ''",
	} {
		allowed(t, command, s)
	}
}

// AC-6 and T1: an alias is read wherever git would find it -- -c,
// --config-env with the variable it names, GIT_CONFIG_PARAMETERS as git
// quotes it, and GIT_CONFIG_COUNT with its keys and values -- and judged as
// the command it runs.
func TestAC6AnAliasIsReadThroughEveryConfigurationChannelGitReads(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		`GIT_CONFIG_PARAMETERS="'alias.x'='worktree prune'" git x`,
		`GIT_CONFIG_PARAMETERS="'alias.x=worktree prune'" git x`,
		`GIT_CONFIG_PARAMETERS="  'user.name'='me'   'alias.x'='worktree prune'" git x`,
		`GIT_CONFIG_PARAMETERS="'alias.x'='worktree '\''prune'\'''" git x`,
		"GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=alias.x GIT_CONFIG_VALUE_0='worktree prune' git x",
		// A count the shell has yet to expand is every key there is, and one
		// above the keys set is read as far as they go.
		"GIT_CONFIG_COUNT=$N GIT_CONFIG_KEY_0=alias.x GIT_CONFIG_VALUE_0='worktree prune' git x",
		"GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=alias.x GIT_CONFIG_VALUE_0='worktree prune' git x",
		"V='worktree prune' git --config-env=alias.x=V x",
		"export V='worktree prune'; git --config-env alias.x=V x",
		// An alias's value split as git splits it: quotes taken out, and a
		// backslash escaping the character after it outside single quotes.
		`git -c alias.x='worktree "pr"une' x`,
		`git -c alias.x='worktree pr\une' x`,
		// A shell alias's command line, with the words after the alias.
		"git -c alias.x='!f() { git worktree prune; }; f' x",
		`git -c alias.x='!git -c alias.y="!git worktree prune" y' x`,
		"git -c alias.x='!git worktree' x \"it's here\"",
		// Defined now, judged as it would run.
		"git config --add alias.x 'worktree prune'",
		"git config --replace-all alias.x 'worktree prune'",
		"git config --type bool alias.x 'worktree prune'",
	} {
		wtRefusedAs(t, command, s, "worktree-through-the-tool")
	}
	for _, command := range []string{
		// git refuses a count that is not a number, and runs nothing.
		"GIT_CONFIG_COUNT=two GIT_CONFIG_KEY_0=alias.x GIT_CONFIG_VALUE_0='worktree prune' git x",
		`GIT_CONFIG_PARAMETERS="'alias.x'='it'\''s'" git status`,
		`GIT_CONFIG_PARAMETERS="'alias.x'='worktree prune'" git status`,
		"git config alias.st status",
	} {
		allowed(t, command, s)
	}
	// An alias defined to commit meets the commit gate, and one that commits
	// in another checkout meets that checkout's rule.
	wtRefusedAs(t, "git config alias.ci 'commit -m x'", withShared(notReady, sharedLandings), "commit-gate")
	wtRefusedAs(t, "git config alias.ci '!git -C /work/main commit -m x'", s, "commit-in-another-checkout")
	wtRefusedAs(t, "git -c alias.ci='!git commit' ci -m x", withShared(notReady, sharedLandings), "commit-gate")
	allowed(t, "git -c alias.ci='!git commit' ci -m x", s)
}

// AC-6 and T4: a variable points git where the shell that runs it has it set:
// an unset takes it away, a subshell's export stays in the subshell, and what
// is set in front of a shell, or of eval, is set for the commands in it.
func TestAC6GitsPlaceIsWhereTheShellRunningItHasItsVariablesSet(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		"GIT_WORK_TREE=/work/main sh -c 'git commit -m x'",
		"GIT_DIR=/work/main/.git eval 'git commit -m x'",
		"export GIT_DIR=/work/main/.git; unset GIT_WORK_TREE; git commit -m x",
		"GIT_WORK_TREE=/work/main; (git commit -m x)",
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	wtRefusedAs(t, `GIT_WORK_TREE=/work/main bash -c "git $(echo commit) -m x"`, s, "git-command-cannot-be-read")
	for _, command := range []string{
		"export GIT_DIR=/work/main/.git; unset GIT_DIR; git commit -m x",
		"export GIT_WORK_TREE=/work/main; unset -v GIT_WORK_TREE; git commit -m x",
		"(export GIT_DIR=/work/main/.git); git commit -m x",
		"GIT_DIR=/work/main/.git eval 'git status'",
		"unset GIT_DIR; git commit -m x",
	} {
		allowed(t, command, s)
	}
}

// AC-6 and T4: git reads its place and its settings from the environment the
// session started with too. A place there counts as one in the command does;
// a setting there is read for its aliases, and never refused for being
// unreadable, because an agent cannot spell out what a person's environment
// set.
func TestAC6TheEnvironmentTheSessionStartedWithPointsGitAsTheCommandWould(t *testing.T) {
	carrying := func(vars map[string]string) State {
		s := withShared(ready, sharedLandings)
		s.Carried = func(name string) string { return vars[name] }
		return s
	}
	wtRefusedAs(t, "git commit -m x", carrying(map[string]string{"GIT_WORK_TREE": "/work/main"}), "commit-in-another-checkout")
	wtRefusedAs(t, "git x", carrying(map[string]string{"GIT_CONFIG_PARAMETERS": "'alias.x'='worktree prune'"}), "worktree-through-the-tool")
	for _, tc := range []struct {
		command string
		vars    map[string]string
	}{
		{"git commit -m x", map[string]string{"GIT_DIR": "/work/own/gitdir"}},
		{"git commit -m x", map[string]string{"HOME": "/home/me"}},
		{"git status", map[string]string{"GIT_CONFIG_PARAMETERS": "garbage"}},
		{"git status", map[string]string{"GIT_CONFIG_GLOBAL": "/home/me/.gitconfig"}},
		// The command line's own value is the one git runs with.
		{"GIT_WORK_TREE=/work/project git commit -m x", map[string]string{"GIT_WORK_TREE": "/work/main"}},
	} {
		allowed(t, tc.command, carrying(tc.vars))
	}
}

// AC-6 and T4: another checkout's work staged into this index, by a work tree
// that is given relative to where env -C runs git, is a commit in that
// checkout later in the same command.
func TestAC6AWorkTreeRelativeToEnvDashCIsPlacedFromThere(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		"env -C /work git --work-tree=main add -A && git commit -m x",
		"env -C /work/main git --work-tree=. add -A && git commit -m x",
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	allowed(t, "env -C sub git --work-tree=. add -A && git commit -m x", s)
	allowed(t, "env -C /work git --work-tree=elsewhere add -A && git commit -m x", s)
}

// AC-6 and T1: `git worktree` reads only with list, or when asked for help or
// for nothing at all, which prints its usage. Every other action, and one the
// hook cannot read, changes a checkout.
func TestAC6WorktreeActionsAreReadAsGitReadsThem(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		action string
		reads  bool
	}{
		{nil, "", true},
		{[]string{"list"}, "list", true},
		{[]string{"-v", "list"}, "list", true},
		{[]string{"--porcelain", "list"}, "list", true},
		{[]string{"-h"}, "-h", true},
		{[]string{"--help"}, "--help", true},
		{[]string{"prune"}, "prune", false},
		{[]string{"-q", "prune"}, "prune", false},
		{[]string{"$SUB", "--force"}, "$SUB", false},
	} {
		action, reads := worktreeAction(tc.args)
		if action != tc.action || reads != tc.reads {
			t.Errorf("worktreeAction(%q) = %q, %v; want %q, %v", tc.args, action, reads, tc.action, tc.reads)
		}
	}
	s := withShared(ready, sharedLandings)
	for _, tc := range []struct{ command, shown string }{
		{"git worktree lock x", "`git worktree lock`"},
		{"git worktree unlock x", "`git worktree unlock`"},
		{"git worktree repair", "`git worktree repair`"},
		{"git worktree add ../new", "`git worktree add`"},
		{"git worktree -q prune", "`git worktree prune`"},
		{"git worktree $(echo remove) --force x", "`git worktree $(...)`"},
		{`git worktree "$A"`, "`git worktree $A`"},
	} {
		grRefusedSaying(t, tc.command, s, "worktree-through-the-tool", tc.shown)
	}
	for _, command := range []string{"git worktree", "git worktree -h", "git worktree --porcelain list"} {
		allowed(t, command, s)
	}
}

// AC-6: git config's changes are read in the syntax before git 2.46 and after,
// with the options that take a value stepped over, and a read changes nothing.
func TestAC6GitConfigsChangesAreReadInBothSyntaxes(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []configChange
	}{
		{[]string{"core.bare", "true"}, []configChange{{key: "core.bare", value: "true", valued: true}}},
		{[]string{"set", "core.bare", "true"}, []configChange{{key: "core.bare", value: "true", valued: true}}},
		{[]string{"--file", "f", "a.b", "c"}, []configChange{{key: "a.b", value: "c", valued: true}}},
		{[]string{"--file=f", "a.b", "c"}, []configChange{{key: "a.b", value: "c", valued: true}}},
		{[]string{"--", "a.b", "c"}, []configChange{{key: "a.b", value: "c", valued: true}}},
		{[]string{"--unset", "a.b"}, []configChange{{key: "a.b"}}},
		{[]string{"unset", "a.b"}, []configChange{{key: "a.b"}}},
		{[]string{"--remove-section", "a"}, []configChange{{key: "a", section: true}}},
		{[]string{"--rename-section", "a", "b"}, []configChange{{key: "a", section: true}, {key: "b", section: true, valued: true}}},
		{[]string{"a.b"}, nil},
		{[]string{"--get", "a.b"}, nil},
		{[]string{"get", "a.b"}, nil},
		{[]string{"--list"}, nil},
		{[]string{"--unset"}, nil},
		{[]string{"--remove-section"}, nil},
		{[]string{"--rename-section", "a"}, nil},
	} {
		if got := configChanges(tc.args); !slices.Equal(got, tc.want) {
			t.Errorf("configChanges(%q) = %+v, want %+v", tc.args, got, tc.want)
		}
	}
}

// AC-6: GIT_CONFIG_PARAMETERS is read as git writes it, each setting quoted
// as sq_quote quotes it, and anything else is not read at all.
func TestAC6GitConfigParametersAreReadAsGitQuotesThem(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  []setting
		ok    bool
	}{
		{"", nil, true},
		{"   ", nil, true},
		{"'a.b'='c'", []setting{{key: "a.b", value: "c", known: true}}, true},
		{"'a.b=c'", []setting{{key: "a.b", value: "c", known: true}}, true},
		{"'a.b'='c' 'd.e'='f'", []setting{{key: "a.b", value: "c", known: true}, {key: "d.e", value: "f", known: true}}, true},
		{`'a.b'='it'\''s'`, []setting{{key: "a.b", value: "it's", known: true}}, true},
		{"'a.b'='$X'", []setting{{key: "a.b", value: "$X"}}, true},
		{"a.b=c", nil, false},
		{"'a.b'=c", nil, false},
		{"'a.b", nil, false},
		{"'a.b'='c", nil, false},
	} {
		got, ok := configParameters(tc.value)
		if ok != tc.ok || !slices.Equal(got, tc.want) {
			t.Errorf("configParameters(%q) = %+v, %v; want %+v, %v", tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

// AC-6: an alias's value is split into words as git splits it, and the words
// after a shell alias are handed to the shell quoted as the same words.
func TestAC6AnAliasIsSplitAndItsArgumentsQuotedAsGitAndTheShellReadThem(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  []string
	}{
		{"worktree prune", []string{"worktree", "prune"}},
		{"  log\t--oneline  ", []string{"log", "--oneline"}},
		{`commit -m "two words"`, []string{"commit", "-m", "two words"}},
		{`commit -m 'it\s'`, []string{"commit", "-m", `it\s`}},
		{`pr\une`, []string{"prune"}},
		{`""`, []string{""}},
		{"", nil},
	} {
		if got := splitAlias(tc.value); !slices.Equal(got, tc.want) {
			t.Errorf("splitAlias(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
	for _, tc := range []struct {
		words []string
		want  string
	}{
		{[]string{"-m", "x"}, "-m x"},
		{[]string{"it's here"}, `'it'\''s here'`},
		{[]string{""}, "''"},
		{[]string{"a/b.c=d:e,f+g@h%"}, "a/b.c=d:e,f+g@h%"},
		{[]string{"$HOME"}, "'$HOME'"},
	} {
		if got := shellQuoted(tc.words); got != tc.want {
			t.Errorf("shellQuoted(%q) = %s, want %s", tc.words, got, tc.want)
		}
	}
}
