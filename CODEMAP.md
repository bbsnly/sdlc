# CODEMAP

Facts about this codebase's modules, boundaries and entry points that are not
obvious from a directory listing. Added to by the analysis and retro agents.
Nothing here is an opinion, and nothing restates what a file's own comments
already say.

## internal/config/checkout.go

Package `config`. Defines `Checkout`, one working tree of a git repository, and
`FindCheckout(root) (Checkout, error)`, which locates the repository's main
checkout — where the backlog and `.sdlc/stories/` are kept — from git's own
on-disk files (`.git`, `commondir`, the common `config`), with no subprocess and
no `GIT_*` variable. Also defines `(Checkout) Worktrees()`, `(Checkout)
Candidates()` (the common-git-directory candidates `internal/hook` protects,
chosen without reading `core.bare`), and `SamePath(a, b)`. Imports `pathrules`
and `sdlcerr`.

## internal/store/claim.go

Package `store`. Defines the claim file format `sdlc/claim/1`, one file per
held story at `<git common dir>/sdlc/claims/<ID>.json` — outside `.sdlc/`, so
git never shows, commits or pushes it. A claim holds only the story id and the
holding checkout's absolute path; no session id. Defines `Holders()`, which
resolves who holds each story from claims plus `.sdlc/state/active` adoption,
and `SessionHolder`, which finds which checkout a Claude Code session is bound
to across a repository's checkouts. Imports `config`, `fsx`, `model`,
`sdlcerr`.

## internal/hook/checkouts.go

Package `hook`. Defines `place`, where a project sits among its repository's
checkouts, derived once per hook call from `config.FindCheckout`, and the
`State.Shared` mechanism: for a word a shell command or a file tool would
touch, finds which checkout of the repository — other than the session's own —
it lands in, by walking up from the word's resolved location to the nearest
ancestor directory whose `.git` leads into a candidate common git directory.
Backs the hook's refusal of a held session's writes to another checkout's
backlog, records, `.git`, `.claude` and `CLAUDE.md`, and the
`commit-in-another-checkout` rule. Imports `config`, `model`, `pathrules`,
`shellpolicy`.

## internal/cli/held.go

Package `cli`. Defines `decideStart`, the one function both `sdlc start` and
`checkTrunkForNewWork` call to decide what a checkout's next `sdlc start`
does, given `store.Holders()`: resume, begin a story, or refuse one held
elsewhere (SDLC-E0050) or whose holder is gone (SDLC-E0051). Defines
`refuseIfHeldElsewhere`, called by every writer (`gate`, `artifact write`,
`review add`, `cost add`, `approve`, `escalate`, `freeze`, `unfreeze`) before
its own write.

## internal/cli/release.go

Package `cli`. Adds the `sdlc release STORY --reason "..."` command: a
person-only command, refused to a Claude Code session by
`internal/shellpolicy`'s `release-is-a-human-decision` rule, that ends a
checkout's claim on a story so any checkout can start it. Refuses while any
checkout is still running an iteration on the story (SDLC-E0012). Writes a
`release` event to the story's record, carrying the person's reason and no
path.

## internal/gitx/worktree.go

Package `gitx`. Holds the binary's only git commands that change a
repository. `AddWorktree(ctx, root, dir, branch, trunk)` cuts `branch` from the
commit `refs/heads/<trunk>` is on with a create-only `update-ref`, then runs
`git worktree add` without `-b`. When that fails, it takes away only what the
call made. It refuses with SDLC-E0053 (`WorktreeNotMade`), wrapping
`ErrInTheWay`, `ErrNotADirectory`, `ErrBranchName` or `ErrNoTrunk` for
`errors.Is`. `RemoveWorktree` removes a linked worktree git lists on the given
branch, and deletes that branch only at the commit git lists for it. It refuses
the main checkout and any directory git does not list. Also defines `Ahead`,
`HeadAhead`, `Holds`, `Worktrees`, and `Version` with `MinimumVersion` 2.17.
Every git call goes through `gitInput` in `gitx.go`, which passes
`--work-tree=<root>` and removes git's location variables from the environment
(`Environ`). Imports `sdlcerr`.

## internal/store/storyworktree.go

Package `store`. The layout of story worktrees. `StoryWorktree(id)` is
`.sdlc/worktrees/<ID>` in the main checkout, whichever checkout asks, and the
worktree is on `StoryBranch(id)`, which is `story/<ID>`. `StoryWorktreeOf` and
`IsStoryWorktree` tell a story worktree from one made by hand. `At(root)` opens
another checkout of the same repository, with that checkout's configuration.
`LockStory(id)` is the project lock keyed on the story's worktree path.
`sdlc start` holds it from making the worktree until the start is recorded, and
it exists in the user's cache only while held. `EnsureWorktreesIgnored` writes
`.sdlc/worktrees/.gitignore` holding `*`, but only where none exists and only
into a real directory. `CheckWorktreesIgnored` reports one that does not hide
everything. Imports `config`, `sdlcerr`.

## internal/cli/storyworktree.go

Package `cli`. Defines `startInWorktree`, `sdlc start` with `git.worktrees` on,
which the start command in `loop.go` calls. A new story's worktree is made and
smoke-checked before the project lock is taken, under `store.LockStory`. Start
then decides again under the lock, at most `startRounds` (2) times. Under the
lock it writes `active` first, then `session`, and claims the story for the
worktree through a store opened at the worktree. The backlog status and the
record go to the main checkout. Also builds:

- SDLC-E0053's fix for each cause (`notMadeFor`, `inTheWayFix`);
- the worktree texts of SDLC-E0040 and SDLC-E0041;
- what a failed start leaves (`rolledBack`, `leftBegun`).

`lookAt`, `keepOn` and `shellWord` build the commands that `doctor.go` and
E0053 tell a person to run. They use only the checked story id and the
configured trunk. A branch name git lists is only displayed, through
`shellWord`. Imports `config`, `gitx`, `model`, `sdlcerr`, `store`.

## internal/hook/checkoutset.go

Package `hook`. Builds the `shellpolicy.CheckoutSet` for a command that could
take something away. The set lists the project first, then the main checkout
and every linked worktree git has an entry for, each with its links resolved.
Its `Place` reads a word through links and by name, as `land` does. Where the
main checkout cannot be found, the set is what could be read, the project at
least. `hook.go` hands the set to shellpolicy as `State.Checkouts`, built at
most once per call. Beside it goes `State.Carried`, the environment the session
started with. This story added both callbacks to the hook-to-shellpolicy seam,
and shellpolicy still reads nothing from the disk or the environment itself.
Imports `config`, `pathrules`, `shellpolicy`.

## internal/shellpolicy/decode.go

Package `shellpolicy`. Defines two decoders:

- `decoded`, a POSIX command as the shell reads `$'...'` escapes and a
  backslash before an ordinary character outside quotes;
- `bracesExpanded`, a command with its brace lists expanded.

`readings` in `shellpolicy.go` adds each as a reading of its own, beside the
released reading. It does so only where the text differs, and never for
PowerShell. Standard library only.

## internal/shellpolicy/gitcalls.go

Package `shellpolicy`. Reads a git command as git will run it.

- **Aliases** come from `-c`, `--config-env`, `GIT_CONFIG_COUNT` with
  `GIT_CONFIG_KEY_n` and `GIT_CONFIG_VALUE_n`, and `GIT_CONFIG_PARAMETERS`.
  They also come from the session's starting environment through
  `State.Carried`, which resolves aliases and never refuses on its own.
- **`!` aliases:** their command lines are read to `maxGitDepth` (4), and up to
  `maxAliasText` (64 KB) per reading. Going over is refused.
- **Git's place:** tracks where `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`
  and `core.worktree` point git, from anywhere earlier in the command
  (`environment`, `pointedAt`).

Emits `worktree-through-the-tool`, `worktree-setting-through-the-tool` and
`git-command-cannot-be-read`. Decides whether a commit meets `commit-gate` or
`commit-in-another-checkout`, whose finding `commands.go` builds. Standard
library only.

## internal/shellpolicy/takeaway.go

Package `shellpolicy`. Defines `CheckoutSet`: the checkouts the hook finds, and
a function that places a word on disk. The package reads nothing from the disk
itself. Refuses, under `loop-state-through-the-tool`, a command that takes away
a checkout of the repository or a directory holding one. The checkout can be
reached:

- by name;
- by the shell's names for where it is;
- through `..`;
- by glob and brace words, matched against `checkoutTree`, a tree of the
  checkouts and every directory above them. At most `maxStates` (256) states
  are followed, and going over counts as a match.

The words come from removers and movers, `rsync`'s delete options, `find`'s
removers, and removers in inline code. Imports `pathrules`.

## internal/shellpolicy/wrappers.go

Package `shellpolicy`. Works out what `env`, `flock` and `script` run, so that
`program` in `shellpolicy.go` reads through to the command behind them. For
`env`, that covers its options that take a value, and `-S` or
`--split-string`, whose string is read as a command line. Standard library
only.

## internal/cli/landing.go

Package `cli`. Runs the landing a finished worktree story's `sdlc stop`
performs: `(*lander).land()` works through checks L0 to L12 — `check` (trunk,
range and change), `runLandingCommands` (trunk's committed `build`, `test`,
`lint`, `fmt_check`, `coverage` and `mutation` commands, each through
`shellx.InGroup`), `move` (the fast-forward and the record write) and
`cleanUp` (worktree and branch removal). `landIfFinished`, called from `stop`,
is the entry point; it applies only where `store.StoryWorktreeOf` names the
story's own worktree. `mainSettingsOf` reads the trunk's name and the
backlog's path from the main checkout's own configuration, never the story
branch's. `codemapCheck`, backed by `gitx.ModeAt`, refuses any change at or
under the root `CODEMAP.md` other than a plain or executable file there, so
the fingerprint's exclusion of that path (`leftOut`) cannot carry anything
else to trunk unreviewed. Builds the landing-specific `WithFix` text for
`SDLC-E0054`, `SDLC-E0055` and `SDLC-E0056`, and for reused codes reopened at
landing. Imports `config`, `gitx`, `model`, `sdlcerr`, `shellx`, `store`. The
`landingCommands` list (`coverage`, `mutation` among them) is
the only caller of `commands.coverage`/`commands.mutation` from
`.sdlc/config.json` (`sdlc doctor` reads each command's text and runs none);
`./task check` in `Taskfile.yml` does not run either.

## internal/cli/trunktests.go

Package `cli`. Decides which of a story's test files are trunk's own, so that
trunk's tests a story has picked up by landing on a moved trunk do not count
against its freeze. `sittingOn` is the merge base of trunk's tip and the story's
head, with trunk named by the main checkout's configuration; it is `""` where
that cannot be told. `heldTests` returns the frozen files still broken, the test
files the freeze does not hold, trunk's files the story changed or took away, and
the `model.TrunkTests` it accepted. A path is accepted only where trunk changed it
between the cut and `sittingOn`, the story's commits did not, trunk's mode for it
is `100644` or `100755`, and the SHA-256 of the file on disk equals that of trunk's
blob (`judge.asTrunkHasIt`); `git status` is not asked. A frozen file is accepted
only where the lock's hash for it is the cut's blob. Outside a story's own
worktree, and wherever there is no cut or `sittingOn` is the cut or the tip, the
result is the same two lists as before. `acceptedLine` builds the line a gate
prints; `recordAccepted` sets the record's field. Imports `gitx`, `model`,
`store`, `testset`.

## Tests of the landing on trunk

Added by US-088, in the packages above. `internal/gitx/replay_test.go` runs the
sealed replay, its abandon, `Unmerged` and `LinesFingerprint` against real
repositories, including the git behaviour the landing rests on (conflicts listed,
an abandoned rebase restored, a missing committer). `internal/store/landings_test.go`
covers `LockLandings`: one holder, the wait, the refusal naming the holder, and
release on a signal. `internal/cli/landing_ontrunk_test.go` drives the stop and
its commands through AC-1 to AC-13 in sandbox repositories.
`internal/cli/landing_ontrunk_review_test.go` holds the cases the first design
review found no test held. `internal/cli/landing_ontrunk_wait_test.go` covers a
stop waiting for another landing, and a replay that is not the reviewed change.
`test/e2e/landing_ontrunk_test.go` runs the built binary through the plugin
launcher: three stories landing one after another, two stops at once, the hook
still refusing a write to a test file, and the pages a person reads.

## internal/cli/doctor.go: configured commands

Package `cli`. `commandChecks(root, cfg)` reads every non-empty command in
`commands` twice and runs none. `programsIn` is the best-effort PATH reader: the
first word of each part, skipping builtins (`trap` among them) and any part that
starts with `VAR=value`, which hides the program after it (US-003). Then each
command goes through `shellpolicy.Inspect` with `State{Backlog}` only, and a
refusal is a `commands and hook` problem naming the key and `Finding.Rule`, never
the command or the reason. That state leaves out the agent's role, the directory,
the freeze, `Resolve`, `Shared`, `Checkouts` and `CommitReady`, so a rule that
turns on them can be reported for a command the hook allows (`commit-gate` on a
`git commit`) and missed for one it refuses. `fmt_file` is skipped there: the
hook runs it itself, outside the classifier. The profile that init's coverage
commands write goes to `mktemp` and an `EXIT` trap removes it; a run killed by a
signal leaves the file in the OS temp directory.

## internal/gitx/landing.go and internal/gitx/fastforward.go

Package `gitx`. `landing.go` defines every sealed git call the landing uses to
read the repository: `sealed`/`sealedCall` run each one in its own
`os.MkdirTemp` directory, with no hooks, no replacement objects, the
commit-graph and filesystem monitor off, and the variables in `unsealed`
stripped from the environment. `BranchTip`, `IsAncestor`, `MergeBase`,
`HasMerges`, `ChangedUnder`, `ModeAt`, `FileAt`, `Uncommitted`, `InProgress`,
`ResolvesTo` and `CheckedOut` are sealed reads; `Fingerprint` hashes the patch
between two commits with `leaveOut` turned into literal exclude pathspecs
(`.sdlc`, the root `CODEMAP.md`) and every diff-shaping git option fixed on
the command line. `fastforward.go` defines `FastForward(ctx, root, trunk,
from, to)`, the package's only function that moves a branch: `checkCheckout`
inspects every checkout that has trunk checked out for anything in the way
before any write; the write phase takes in paths already holding the target
content, then moves each checkout's index and working tree from `from` to
`to` by a two-tree read-tree merge; the ref itself moves by one
compare-and-swap, and `putBack` reverses every checkout already written if a
later one refuses. `RemoveLandedWorktree` removes a landed worktree and
deletes its branch only where that branch is exactly at the landed commit.
Neither file runs a hook, a merge commit or a push.

## internal/gitx/replay.go

Package `gitx`. The one rebase the tool runs itself: `Replay(ctx, checkout, onto,
upstream)`, called by `ontoTrunk` in `internal/cli/landing.go` when `sdlc stop`
puts a finished story's commits onto a trunk that moved. It runs `rebase --merge
--no-fork-point --onto` through `sealedCall` with `replaySettings` as `-c`
settings: the rebase's and rerere's own options off, `merge.default=text`,
`commit.gpgSign=false`, both editors `true`, `attr.tree` empty, and
`core.attributesFile` pointed at a file the call's private directory does not
create. Every `merge.<name>.driver` the repository configures is overridden to
`exit 1`, so a configured driver ends as a conflict and runs nothing; a name
holding `=` or the private directory's placeholder is refused before git runs.
The repository's filter drivers and committer are left to it. `AbandonReplay` is
`rebase --abort`. `Unmerged` lists the conflicted paths with `diff --name-only -z
--no-renames --diff-filter=U`. `nulSeparated` is the NUL-splitting helper both
it and `ChangedBetween` use. `LinesFingerprint`, the context-free measure the
landing compares from both sides, is in `landing.go` beside `Fingerprint`.

## internal/shellpolicy/refs.go

Package `shellpolicy`. Defines the rule `branch-through-the-tool`
(`branchRule`), asked from `gitRules.judge` for every git command a held
session runs: refuses forcing, deleting, renaming or copying a branch, a
checkout or switch that creates one over an existing one or ignores another
worktree, symbolic-ref's writing forms, a reset naming a commit elsewhere, and
`worktree add` with a forced branch, in every placement `gitcalls.go` already
reads. Its finding is held in a `branchHold` value made fresh for each
`Inspect` call, not package state, until every earlier rule has passed the
whole command; `maxHeldWork` (1<<20 work units, charged by run, segment and
reading) bounds how much more of the command is read once a finding is held,
so past that bound `Inspect` answers the held finding without reading
further. `rebaseAllowed`, called from `checkCommit`, admits exactly two forms
once the commit gate has passed: the plain rebase of the session's own
checkout onto trunk's branch ref (`isTrunk`), and a bare `git rebase
--abort`.

## internal/shellx/group_other.go and internal/shellx/group_windows.go

Package `shellx`. Each defines `InGroup(cmd *exec.Cmd)` under its own build
tag (`!windows` / `windows`); `Run` calls it before starting a command so the
whole tree the command spawns stops when the command does. On `!windows`,
`InGroup` sets `cmd.SysProcAttr.Setpgid` and a `cmd.Cancel` that sends
`SIGKILL` to the process group. On Windows, `InGroup` makes a kill-on-close
job object (`newJob`), with the `kernel32` procedures it needs looked up
lazily inside the function rather than held as package-level values, and the
process is assigned to the job only once `Run` has started it (`joinGroup`);
a child the command starts in the gap between `Start` and that assignment is
not in the job and outlives a stop. `shellx.StopAll()` cancels every
registered group; `cmd/sdlc/main.go`'s signal handler calls it before
`store.ReleaseHeld`, so a second stop cannot take the landing lock while the
first one's commands still run.

## internal/pathrules/pathrules.go

Package `pathrules`. `Resolver.Place(p)` is where `p` lands on disk,
absolute, with every link on the way followed the way the filesystem follows
it: `.` and `..` are read by the walk itself, never handed to the OS, so a
`..` straight after a link takes the parent of where the link leads, not the
parent of the link's own name. `follow`/`followFrom`/`walkLinks`
(`final_windows.go`'s `finalPath` first, Go's `filepath.EvalSymlinks`-style
walk otherwise) bound the links one word may follow at `maxLinks` (64,
`pathrules.go`), counting every link crossed including ones inside a
resolve; past the bound the rest of the word is read by name, so `Place`
still answers rather than refuse. A `Resolver.Memoised()` resolver caches
`Lstat`, resolve and follow answers per path so a repeated prefix in one
command costs one syscall. `final_windows.go` and `final_other.go` each
define `normalise(p string) string`, which spells a `p` already known to
hold no symlink the way `resolve` would spell it (case and long names on
Windows; unchanged elsewhere).

## internal/cli/reading.go

Package `cli`. Defines `type reading`, one command's lazily-filled, memoised
answer to "what tree is this review stamped against, and what is HEAD": a
`*store.Store`, the subject and HEAD each with a "known" flag. `readingOf(s)`
makes one; `(*reading).subject(ctx)` and `.headOf(ctx)` call
`store.ReviewSubject`/`gitx.Head` at most once each, and never keep a failure,
so a caller that asks again after an error makes git run again. It is a local
built inside one `RunE`, never package state, never given a `context.Context`
to hold, and never read again once the command has written the tree or moved
HEAD. `readingOf` is called at exactly four sites: `gate` (loop.go), `review
add`, `review list` and the landing's `changeChecks`; the landing's
deliberate re-reads (`requireApprovalAgain`, `unmoved`) and the other direct
callers of `ReviewSubject`/`gitx.Head` (`approve`, `log`, `ack`, `doctor`,
`start`) keep their own calls (US-055). Imports `gitx`, `store`.

## internal/cli/leave_windows.go and internal/cli/leave_other.go

Package `cli`. Each defines `leaveWorktree(main, worktree string)` under its
own build tag. On Windows, a directory cannot be removed while it is a
process's current one, so `leaveWorktree` reads the process's working
directory (`os.Getwd`) and, only when it is `worktree` or under it
(`config.PathKey`), moves the process into `main` with `os.Chdir`, logging at
debug level if that fails. Elsewhere it does nothing, because a directory
that is a process's current one can still be removed. Called from the
landing path around worktree removal, after `ExecuteIn` let a command share
the process across invocations. Imports `config`.

## internal/cli/docs_test.go

Package `cli`. Doc-text tests: every drift between a shipped page and the code
it describes that this suite has caught so far. `page(t, name)` reads
`docs/<name>` relative to the module root. Each test's "what should be
true" side is derived from running code rather than from a second copy of the
same fact kept in the test: `model.Gates`, `model.Artifacts`, `policy.Rules`
and the rules `shellpolicy.Inspect` actually produces (`shellRules`),
`config.Default()` reflected into JSON field names, `scaffold.StackNames()`.
`project` and `mustRun` (`internal/cli/commands_test.go`) drive the command
tree in-process through `New(...)`, never forking the compiled binary, so
this package cannot observe a process being signalled or killed.

## test/e2e package

Package `e2e`. `build(t)` compiles the real `cmd/sdlc` binary with `go build`
into the calling test's own temporary directory, so each test that calls it
builds again (its comment says once per package; it is not); every other
helper (`runTool`, `tool`,
`runToolWithInput`, `launcher`) drives that binary with `exec.CommandContext`,
so this is the only package here that forks the real binary and can send it
an OS signal, or that exercises `plugin/bin/sdlc-hook`, the launcher script
Claude Code actually calls. `analysed`, `walk` and `review` take a fresh
story through Gate 1/2, up to but not including the commit gate, and record
a review verdict from each named role, for a test that needs a story already
at a particular gate.

The three `sandbox_*_test.go` files work differently. `sandboxProgram` builds
`internal/tools/sandbox` and `newSandbox` runs its `create` with a home and a
temp directory of the test's own (`programEnv`), so the sdlc under test is the
one that program built, not `build(t)`'s. `sandbox.env` points the home, the
cache and the temp directory of every command at the sandbox's `home` and
`tmp`. `sandbox.does` puts a command to the hook and then runs it as the
session under test; `sandbox.person` runs one with no session and never puts
it to the hook, and refuses a directory outside the sandbox the same process
made. The steps the loop keeps for a person exist nowhere else: `person` is
test code, and no task or program performs them. Before these files no test
here ran `sdlc ack`, or held the hook's refusal of `approve`, `unfreeze` or
`ack` end to end.

## test/e2e/zcommands_test.go: the command guard

Package `e2e`. `TestAC1EveryCommandAndTheHookHaveACaseThatRanItToTheResultItExpected`
fails, naming each command and the hook, that no case in the package ran to the
result the case expected. The file's name is part of how it works: Go runs a
package's tests in file-name order, so the guard runs after every test that can
record a run. Runs are recorded by `noteRun` and `noteHook` at the launch sites.
A command counts when it exited 0 where it was started, or when a case expected
another exit and compared its streams (`sandbox.holds`, which covers `doctor`'s
exit 1). The hook counts when it answered. A refusal does not count, and nor
does a command spelled inside a payload handed to the hook. A command only a
person may run counts when a case runs it through `sandbox.person`, which the
single `approve --reject` case in `sandbox_reject_test.go` does.

`knownGaps()` lists the commands no case runs, each with its reason:
`story`, `artifact`, `review` and `staffing` print their help only, and
`parallel` has no run on Windows. The guard skips with a reason under `-run`,
`-skip`, `-shuffle` and `-list`, because the package has then not run whole.

## internal/testscan

Package `testscan`. Development tooling that reads this repository's command
tree and Go sources without running either. Only `test/e2e` and
`internal/tools/testreport` import it; `go list -deps ./cmd/sdlc` does not name
it. It imports `internal/cli` (for the tree `cli.New` builds), `internal/model`
(for `Gates`) and cobra.

- `Commands()` is the commands as typed after `sdlc`, plus `hook`. It walks the
  tree and skips hidden commands, `help` and `completion`; nothing is listed by
  hand.
- `Resolve(args)` is the command an argument list runs, by cobra's `Find` on a
  fresh tree, or `hook` when the first word is `hook`. It is false for a single
  word holding a whole command line, an unknown word, `help` and `completion`.
  Flags after the command do not change the answer, so `freeze --help` resolves
  to `freeze`.
- `Unrun(ran, gaps)` gives the guard's failure lines: first the commands in
  neither set, then each gap that names no such command, has no reason, or was
  run by a case after all.
- `Report(root, w)` writes what `testreport` prints (below).
- `Sleeps(root)` and `CheckSleeps(root, allowed)` find every `time.Sleep` call in
  a test function under `root`, and match them against a list by file and
  function. Only a call through the file's own import of `time` counts.

Files are parsed with `go/parser`, not searched. The walk (`files`) skips
`.git`, `.sdlc`, `node_modules` and `dist` only at the module root, and
`testdata` at any depth. A directory that cannot be read is an error.

The sleep list is `allowedSleeps` in `internal/testscan/sleeps_test.go`: six
entries, one per function, each with its reason. Four are in
`internal/store/guard_test.go`, one in `internal/hook/stop_test.go` and one in
`internal/fsx/fsx_test.go`. The key is the function, so a new sleep inside a
listed function is not reported. `time.After`, `time.NewTimer`, `time.Tick` and
a dot import are not seen.

## internal/tools/testreport

Package `main`, run by `./task test-report` as `go run ./internal/tools/testreport`.
The task is silent, takes no words, and is not a leg of `check`. The tool's one
flag is `-C <dir>`, the module to read (default `.`); any other word
exits 2. Exit 1 means the directory cannot be read. Exit 0 means the report was
written, however long it is. Stdout holds one line per fact:

- `command <name>` for each of `Commands()` that no test in `test/e2e` runs;
- `gate <key>` for each key in `model.Gates` that no test in `test/e2e` passes;
- `<class> <count> <dir>` for each directory holding tests. The class is `e2e`
  for `test/e2e`, `other` for any other directory under `test/` and for a
  directory with a `.test.js` file, and `unit` for the rest. The count is the
  top-level `Test` functions, `TestMain` excluded; a JavaScript file counts its
  `test(` lines.

It reads sources only and runs nothing, so it can differ from the guard. It
credits a literal in an expected-text list as a run (`staffing`, in
`staffing_test.go`), and skips refusal helpers by callee name, any name
beginning `refus`.

## Taskfile.yml's `check` legs: what each reads, and whether `.gitignore` applies

`check` runs seventeen tasks in sequence. What a leg reads decides whether it can
fail on a file a clean clone would not have, so each is listed by how it finds
its files.

- `markdownlint` runs the markdownlint-cli2 installed by its own `npm ci` under
  `.github/tools/markdownlint`. `.markdownlint-cli2.jsonc` sets `"gitignore":
  true`, so the tool drops what `.gitignore` names, including this
  repository's own gate documents, even though its glob walks into
  dot-directories.
- `secrets` runs two gitleaks scans. `gitleaks git` reads history. `gitleaks
  dir` has no flag that reads `.gitignore`, so it is pointed at a snapshot: git
  builds a tree from its own file set (tracked plus untracked and not ignored)
  using a throwaway index and a throwaway object directory, with the real
  object store only an alternate, and `git archive` extracts that tree into a
  temporary directory. The leg fails closed, with an instruction, if the real
  store's loose-object count changed while the snapshot was built or if the
  extracted file count is below what the snapshot's index names, so an empty
  copy never reads as a clean scan.
- `shellcheck` builds its file list from `git ls-files --cached --others
  --exclude-standard`: `--exclude-standard` drops anything `.gitignore` names,
  while `--others` still lints a script that is written but not yet `git add`ed,
  by design (the task's own comment).
- `internal/tools/linkcheck`'s link mode walks the whole tree (`markdownFiles`)
  but skips every dot-directory other than `.github`, so it never walks into
  the loop's own records; its hygiene mode (`checkHygiene`, also run as
  `no-internal-docs`) reads only `git ls-files` output, so every file it can
  object to is already tracked by construction.
- `release-check` (`internal/release.Check`) reads a fixed short list of named
  files plus a glob of `docs/*.md`; none of those paths are gitignored.
- `fmt-check` walks the filesystem directly with `find` (not `git`), pruning
  only the worktrees directory; no `.go` file is gitignored today, so it is not
  currently exposed to the same failure.

A shared helper in Taskfile.yml's `vars` (`ANNOTATE_AWK`) is run over the
failing output of `fmt-check`, `vet`, `lint`, `test`, `npm-test`, `actionlint`,
`shellcheck`, `markdownlint`, `linkcheck` and `secrets`. After each line that
names a file it adds a line saying whether git tracks it, has it untracked (CI
would report it once it is added), or ignores it (a clean clone does not have
it and CI would not report it). It skips markdownlint's `Finding:` line, which
echoes an ignored glob and not a file. `test` instead says it per failing
package, because `go test` names a test file only by its bare name.
`no-internal-docs` says every file it names is tracked, as it reads nothing
else.

`test/checklegs` holds the tests for `markdownlint`, `secrets` and `shellcheck`.
Each runs the leg's task through the `task` CLI in a disposable fixture git
repository, with the pinned tool resolved from the local module or npm cache;
the secrets test builds its AWS-shaped key at run time so that gitleaks does
not report the test source itself.

`commands.coverage` and `commands.mutation` in `.sdlc/config.json` are read
only by `internal/cli/landing.go`'s `landingCommands` (landing.go:222), during
a story's own landing (Gate 9); no leg of `./task check` runs either.

## internal/parallel

Package `parallel`. What `sdlc parallel` needs that is not about a project's
files, with no import of `cli`. `launch.go` holds `Allowlist()` (the built-in
tools an unattended session gets: `sdlc` subcommands, `git status` and `git
add`, build and test commands, scoped `Edit` and `Write`, unscoped `Read`; no
interpreter, runner, network tool, push, `claude` or decision command),
`Narrow` (the allowlist cut down by `parallel.allow`; an entry that is not
built in is SDLC-E0066, and nil means all, empty means none), `Quote`, and
`Command`, which builds the one `claude -p` line through `shellx.Command`.
`schedule.go` defines `Schedule`, which runs at most `limit` sessions and calls
its progress function from one goroutine only. `status.go` defines `Classify`,
which reads a finished session as `waiting`, `stopped` or `failed` from the
story's record first and the exit status second. Imports `model`, `sdlcerr`,
`shellx`.

## internal/cli/parallel.go

Package `cli`. The `sdlc parallel <ID>...` command. `runParallel` checks that
the process is a person's (SDLC-E0061) before it reads anything, then the flags
and the ids, then `parallelPreflight` for each story in order, first refusal
winning and nothing started until all pass; it reuses `decideStart`'s helpers
and `recode` copies their wording under the SDLC-E0057 to E0066 codes. A story
already in its own worktree may be re-run. Sessions run from the main checkout.
Records are read only after `parallel.Schedule` returns, and `printParallel`
prints the status view with the commands a person must run gathered at the
end. Imports `parallel`, `store`, `model`, `sdlcerr`, `shellx`.

## internal/shellpolicy `checkParallel`

Package `shellpolicy`. The rule `parallel-is-a-human-decision`, in
`shellpolicy.go`, refuses `sdlc parallel` from a model session. It is asked
from both `checkPrograms` (`Inspect`) and `humanDecisionsOn`, beside
`checkRelease`. `--max` was added to `ValueFlags`, so its value is not read as
a subcommand.

## config `parallel` section

Package `config`. `Config.Parallel.Allow` is a `*[]string`, so a missing key
(the whole built-in list) and an empty list (none) differ. It can only narrow
the allowlist; `parallel.Narrow` refuses anything wider.

## cmd/sdlc/main.go signal handler

`exitOnInterrupt` handles `os.Interrupt`, `SIGTERM` and `SIGHUP`. `SIGHUP` is
there so that closing the terminal an `sdlc parallel` runs in calls
`shellx.StopAll()`, which ends the sessions, each in a process group of its own.

## internal/shellpolicy/find.go

Package `shellpolicy`. What `find` picks by a glob, for three rules.
`findArguments` splits find's arguments into its roots (`findRoots`, `.` when
none) and the globs given to the six options `globOptions` lists, `-name` and
`-iname` by name, `-path`, `-ipath`, `-wholename` and `-iwholename` by the
whole path, each unquoted. `findPattern` reads a glob the way find reads its
classes (a leading `]`, negation, ranges, POSIX named classes, escapes) into
two readings, as written and folded, and is nil when it cannot: nil means
cannot be read, not no match. A range between two ASCII lower-case letters,
upper-case letters or digits (`plainRange`) holds what is between its ends by
code point or by letter, in either case, and any character beyond ASCII; any
other range, like an equivalence class, holds any one character. A negated part
leaves out only what every reading holds. `matchParts` matches without
backtracking, folding the name for the folded reading. `nameMatches` and
`segmentMatches` (glob.go) share the reader. `foundBy` reads each glob once and
skips a nil read; `takeAway.find` in `takeaway.go` counts one as a match.
`literals` keeps a glob the bracket scan in `hasLiteral` keeps or in which the
reader finds a character of its own (`ownRune`). The four callers of the flat
reading are `checkLoopState` (roots filtered by `reaches`, shapes
`protectedShapes` plus the five `stateFiles`), `checkFrozenTests` (every root,
`literals` of both lists, shapes `frozenShapes`), `takeAway.find`, and the
no-name `find` case of `takenAway`. `unreadGlob` finds the first glob word the
hook cannot read, by its place in the segment (`spelledFields`, shared with the
expression reader), and why (`unreadCause`); both find blocks refuse it with
`unreadFinding` when the command changes files from a root that `couldHold` says
could hold loop state or a frozen test, and `placedRoot` says whether the
refusal can name where the root is. `findOperator` is the words `findRoots`
stops at, so an operator is never read as a directory. `unfollowedFinding`
refuses an expression the reader could not follow, under the same roots as
`unreadFinding`, with a reason and a route chosen by the `findCause`; `leftIn`
adds to a refusal that a find's operators leave the file in, and only to one
the flat reading does not make; `ruleWayOut` is the sanctioned route the find
refusals end with. `printedTo` lists the targets of `-fprint`, `-fprint0`,
`-fprintf` and `-fls` for `writtenTo`, `changesAFile` and `changesFiles`.
"find changes files" is two predicates: the narrow `-delete`/`runsAWriter`,
which gates what the expression picks, and the wide `changesFiles` (it adds
`printedTo`), which gates the unread-glob and cannot-follow refusals. Imports
`pathrules`.

## internal/shellpolicy/findexpression.go

Package `shellpolicy`. The expression find parses, read beside the flat reading
of find.go so a negated or alternated name test is judged by what it leaves in.
`inspectReading` (shellpolicy.go) makes one `findScan` for a reading, the first
time a segment runs find, and asks it to `read` each find once; the three path
rules and `takeAway` are handed the `findExpression`.

- `newFindScan` cuts `findSpelling`'s text into `findPiece`s where
  `splitSegments` cuts `pathText`'s (`separatorAt` is its table, in its order),
  following quotes across the whole text once with `quoting`: two `quoteLevel`s,
  the shell the command is given to and a shell handed one of its quoted words
  as a script. `lineUp` proves the pieces against the plain segments, one for
  one and word for word (`sameWords`); where they do not line up, no find in the
  reading is followed.
- `read` finds the find's program word, how deep in quotes it stands (outside
  them, or one quote deep in a script; an `eval` only with its script as one
  quoted word), and its `end`: the first piece end outside every quote, so a
  separator inside a quoted word does not end it. Where the find runs past its
  own segment, `foundFind` carries its whole words and `inspectReading` asks the
  rules a second time with them, from the same directory. Where `read` gives
  up, its one `fail` exit hands over the segment and every word after it to the
  end of the command instead (`toEnd`), charged to the scan's budget; past the
  budget it makes no words (`spent`) and `inspectReading` refuses the find with
  `spentFinding` (find.go), asking no rule. `laterAction` (find.go) says so
  where a find that only reads is refused for an action later on the line.
- `tokens` makes the reader's words from that stretch and accounts for every
  parsed word of the find with exactly one token (`fieldWords`, built on
  `spacedFields` as `parse` is); a marker byte (`markClose`, `markSemi`) is
  accepted only alone or in quotes of its own at the find's level. `asSplit` is
  the same text with markers as spaces, at the same offsets, for `parse`.
- `readFind`, `readExpression`, `readPrimary` and `readExec` read the roots and
  the expression into a postfix list with an operator stack and no recursion.
  `followed` is set by the last statement of `readExpression` and nowhere else.
  `findTakes` is the table of how many values each option is given and whether
  the option is known at all; an expression holding an unknown one is not
  followed. `shellFill` reports whether the shell fills in or could split a
  word. `maxFindWords` (512) bounds the expression.
- `picks` evaluates the postfix for one file: a negation always answers "could
  pick" (never the complement of the wide glob reader), narrowing comes only
  from a positive glob beside it under AND, and an action in a branch counts.
  `pickedShapes` is the shapes `checkLoopState` and `checkFrozenTests` judge
  after the flat reading's own; `takeAway.find` ORs `picks` into its `passes`,
  with `-empty` and a non-`d` `-type` answering false.

`findSpelling` and `pathText` (shellpolicy.go) change together, and so do
`separatorAt` and `splitSegments`: a change to one not made to the other shows
as every find in a command being refused. `spacedFields` (shellpolicy.go) is
the field splitter `parse` and the reader share: a descriptor number written
against a redirect is no word. `underEval` (shellpolicy.go) takes the `eval`
builtin off an invocation for the path rules alone, which read and judge the
command under it; `program` and the rules that turn on which program runs are
handed the builtin as before.

## internal/shellpolicy/glob.go

Package `shellpolicy`. The shell's own globs and brace lists, read without
listing a directory. `withGlobs` adds every protected shape that a word with
`*?[{` in it can name (`globbed`, `matchShape`). `segmentMatches` uses the same
reader as find's `-name`, matches a leading dot only to a pattern that has one,
and gives `pathMatches` (`path.Match`) for a segment the reader cannot read. In
`matchShape`, a first segment names a shape through the reader only when the
reader finds a character of its own in it (`ownRune`, `namedFirst`); any other
keeps the bracket scan in `hasLiteral` and `pathMatches`, as before the reader.
`globHolding` in `directory.go` and the checkout tree in `takeaway.go` use
`segmentMatches` as well. `braces` expands at most `maxBraces` (64) words.

## internal/tools/sandbox

Package `main`, run by `go run` from `Taskfile.yml` like `linkcheck` and
`release`; goreleaser does not build it and nothing `cmd/sdlc` links imports
it (a test in `test/e2e` fails if the binary's dependencies name it). Two
words, `create` and `remove <dir>`, with `-C <checkout>` before the word;
anything else is exit 2. `create` refuses a temp directory that is the
checkout or inside it, before it makes anything, then makes `sdlc-sandbox-*`
directly under `os.TempDir()` with `os.MkdirTemp`'s mode, and in it `.sdlc-sandbox` (the
marker), `home`, `tmp`, `bin/sdlc` (`go build` in the checkout) and `repo`
(`git init` on `config.Default().Git.TrunkBranch`, `protocol.allow=never`, the
built `sdlc init`, one commit). Its children get an environment built from
nothing: home, cache and temp inside the sandbox, `GOFLAGS` empty,
`GOWORK=off`, `GOPROXY=off`, and from the caller only the names in `passed`.
It prints three lines, which `CONTRIBUTING.md` quotes and a test compares. A
create that fails deletes the directory `MkdirTemp` returned. `remove` decides
every refusal before it deletes: the path resolves, its parent is the resolved
temp directory and its name has the prefix, neither the working directory nor
the `-C` checkout is inside it (the working-directory check holds for the
program run directly; through `./task sandbox-remove` the working directory is
always the checkout, so it never fires there), and what `os.OpenRoot` opened is the same file
as the path and holds the marker as a regular file by `Lstat` (the marker is
never read). Deletion is by name through that root, the marker after every
other entry, then `os.Remove` of the empty directory. A failure while the
marker is still there says to run the same remove again, which finishes it; a
failure of that last `os.Remove` has a message of its own, because the marker
is gone and a second remove would refuse: it names the empty directory for the
person to delete. The place and the marker guard against a
slip; the temp directory is the caller's variable and anyone can write the
marker. Imports `config`. No `SDLC-E` code: `internal/tools/` is outside the
packages the linter holds to `sdlcerr`.

## Taskfile.yml `sandbox` and `sandbox-remove`

Neither is a leg of `check`. `sandbox` sets `GOCACHE`, `GOMODCACHE` and
`GOPATH` from `go env` in the task's `env:`, because the program gives its
children a home inside the sandbox and any `go` command, `go env` included,
writes telemetry under the home it is given; the program cannot ask from
inside. Both tasks pass the words after `--` as
`{{range .CLI_ARGS_LIST}}{{shellQuote .}} {{end}}` and empty `GOFLAGS` for
their own `go run`. The `task` wrapper is itself a `go run`, so a caller's
`GOFLAGS` still reaches that one. Through `go run` and Task the program's exit
codes 1 and 2 both reach the caller as 1. `task` and `task.cmd` start Task with
`--dir <checkout>`, so the program's working directory is the checkout and the
task cannot tell whether the caller's shell is inside the sandbox.

## cmd/sdlc test binary

`cmd/sdlc/testenv_test.go`'s `TestMain` re-executes the test binary two ways
under `SDLC_TEST_HELPER_MAIN`: `1` runs `main()`, and `panic:<NAME>` (or
`panic:*`) runs `run` with an environment reader that panics when that
variable is read. The second is how a hook's reply after a panic is checked in
a real process; the released binary has no way to cause one.

## Where commands lock, and which command reads the session

Every command that writes takes its lock under
`os.UserCacheDir()/sdlc/locks/` (`internal/store/guard.go`), outside the
project, and under `os.TempDir()` where there is no cache directory.
`testenv.Run` (`internal/testenv`) gives the tests a home and a cache
directory of their own: `HOME` and `USERPROFILE` point at `home` and
`XDG_CACHE_HOME` and `LocalAppData` at `cache`, both in the directory it makes
and removes. `GOCACHE`, `GOMODCACHE`, `GOPATH` and `GOENV` keep their values,
taken from `go env` once when one is unset, before the redirection; a go that
cannot say stops the run. `npm_config_cache` keeps its value too, or becomes
the `.npm` under the home the machine had (`npm-cache` under `LocalAppData` on
Windows), named and not made; if either spelling is set, or no home can be
found, nothing is exported and the run carries on. `~/.npmrc` is not read
while the tests run. It writes `off` to go's telemetry mode file under
that home where go's configuration directory is there, since a go process
writing into the directory as it is removed left a `sdlc-testenv-*` behind.
After the tests it names everything in `cache` but the empty `sdlc/locks` and
fails the run, keeping a failing run's own exit code; a lock left held is a
directory in `sdlc/locks`. On Windows the `Microsoft` directory at the top of
`cache` is left out (`systemOwned`), since the system's own programs keep it
under `LocalAppData`; `leftoversOn` takes the system's name so the tests reach
that on any machine. `run` is `Run` with the tests handed in, which is
how the tests reach what happens after `m.Run`. In `internal/cli` only
`parallel.go` reads the session variable to refuse. `approve`, `unfreeze` and
`ack` are refused to a session by the hook alone.

The landing takes one more, `LockLandings` (`internal/store/guard.go`), at the
project lock's path with `-landing` added. A stop takes it before it reads trunk
and holds it through the replay, the commands and the fast-forward. Where it is
held, the stop lets the project's lock go before it waits, up to `landingWait`
(eight minutes, `internal/cli/landing.go`).

## internal/hook/deadline.go

Package `hook`. Holds the hook's own deadline for a PreToolUse call: `hookDeadline`
(25 s from the moment `run` starts, five short of the PreToolUse timeout in
`plugin/hooks/hooks.json`), the rule name `deadlineRule` (`not-checked-in-time`) and
the variable `deadlineEnv` (`SDLC_HOOK_DEADLINE_MS`). `deadlineIn(getenv)` returns the
deadline in force: a whole number of milliseconds above 0 and below the fixed figure,
otherwise the fixed figure. `deadlineVerdict(shell)` returns the refusal for a shell
command or for a file change; both texts are constants and take nothing from the call.
`clock` is the seam (`now`, `after`, `spawn`): `Run` passes `time.Now`, `time.After` and
a goroutine, and `run` takes the same struct, which is how a test moves time. `warnings`
is the locked list `run` answers with, closed by `taken()`. `outcome` is what the rest of
a decision returns to `run`: a verdict, an ok flag, the `record` closure for an allowed
commit's head, and a recovered panic. Imports `policy`. Nothing runs in `init`.

`internal/hook/deadline_test.go` drives `run` through a fake `clock`.
`test/e2e/hookdeadline_test.go` drives the built binary through the plugin launcher.

## The Stop guard and its count file

`internal/hook/stop.go` holds the whole Stop guard: `decideStop`,
`subagentListed`, `waitFrom`, `stopReason` and `handOver`, and the reply type
`turnReply`. The panic answer for a hook is in `cmd/sdlc/main.go`'s `crash`,
not here. `decideStop` takes the clock as an argument; `hook.Run` passes
`time.Now`.

The Stop event's `background_tasks` is read only in `subagentListed`, by a
second unmarshal of the raw event. The `payload` type in `hook.go` does not
hold it, on purpose: a typed field would let a malformed list fail the one
unmarshal that every other check shares. An entry is a `map[string]json.RawMessage`
and only its `type` is looked up.

`internal/store/stop.go` holds the count file `.sdlc/state/stop-blocks.json`
(`story`, `blocks`, `progress`, and the optional `waiting_since`), with
`RecordProgress`, and `Escalate` with `HandOver`, the unit it is a call of: a
story handed to a person with what the command was recording, or nothing
written. `waiting_since` is kept as the text it was written as; the hook decides
what an unreadable one means.

Four test files send Stop events to the built binary:
`test/e2e/sandbox_loop_test.go`, `test/e2e/worktree_test.go`,
`test/e2e/storyworktree_test.go` and `cmd/sdlc/sandboxpanic_test.go`.

## Who a reviewed gate waits for, and the lists an agent or a command is on

Three places each decide which reviews count, from `model.Reviewers`:
`requireReviews` in `internal/cli/review.go` (every `sdlc gate ... pass`, and
again for Gates 6 and 7 at the commit gate through `requireFreshReviews` in
`loop.go`), the loop inside `sdlc review list` in the same file, and
`reviewsFresh` in `internal/hook/hook.go` before a `git commit`. The hook's
takes every review on the record at `verifier_review` and `code_review`,
whether or not the reviewer is expected, and refuses on any stamped with
another tree. `cli` and `hook` do not import each other, so a rule all three
share lives in `internal/model`.

`sdlc gate analysis pass --security-sensitive` is the only writer of
`Record.Security`. It is set whenever the flag is given, on any recording of
that gate, and no hook rule asks which agent runs `sdlc gate`.

An agent or an `sdlc` command is named in each of these, and no test ties them
all together:

- `model.Reviewers` and the `Role` of each entry in `model.Artifacts`
  (`internal/model/model.go`);
- `policy.IsLoopRole` (`internal/policy/policy.go`): an agent not listed there
  is treated as the main conversation by `orchestrator-delegates`;
- `shellpolicy.ValueFlags`: every `sdlc` flag that takes a value, held to the
  command tree by `internal/cli/cli_test.go`;
- the checks `checkPrograms` and `humanDecisionsOn` ask in
  `internal/shellpolicy/shellpolicy.go`, each keyed on the first subcommand
  word; `shellRules` in `internal/cli/docs_test.go` is the sample of commands
  that makes a rule need a heading in `docs/enforcement.md`;
- `parallel.Allowlist` (`internal/parallel/launch.go`), which names the `sdlc`
  subcommands an unattended session may run;
- `personsDecisions` in `test/e2e/sandbox_loop_test.go`;
- the agent count in `TestEveryAgentWorksOnlyAStoryAPersonStarted`
  (`internal/plugin/plugin_test.go`), and in prose in `README.md`,
  `docs/agents.md`, `docs/the-loop.md` and `docs/installation.md`.

`plugin/skills/next/SKILL.md` is capped at 17,500 bytes by
`TestTheRunbookFitsWhatCompactionKeeps`, and was 17,492 at c7bb9d8.

Inside a story's directory the shell rules protect `gate-record.json`,
`reviews/` and the files `model.Artifacts` names, and nothing else
(`loopState` in `shellpolicy.go`); the file rules let every loop agent and the
main conversation write there.

## The staffing

Off unless `staffing.enabled`. `config.Staffing` (`internal/config/config.go`)
holds the lists, `lowest` and `bring_back`, and checks what it can without
knowing the agents; `staffingRoster` in `internal/cli/staffing.go` checks the
names against `model.Agents`, from `openStore` and from `sdlc doctor`.

The record keeps it as `Record.Staffing` (`internal/model/model.go`): one
`Staffed` row for each agent, what a person `Assigned`, every `Amendment`, and
`BroughtBack`, written when `code_review` or `commit` passes. `model.Agents` is
the roster, `model.Doers` the agent that does each gate's work,
`NeverLeftOut` the floor, and `Record.LeftOut` and `Record.StoodDown` what the
gates and the hook ask.

`internal/cli/staffing.go` has `sdlc staffing show`, `set`, `amend` and
`assign`, and `waited`, the one rule for whether a gate waits for a reviewer:
`requireReviews`, `sdlc review list` and `sdlc staffing show` all ask it with
one `reading`, whose `changedPaths` (`internal/cli/reading.go`) is every path
the story changed, `gitx.ChangedBetween` included. `staffTheGate` holds
`gate analysis pass` to a recorded staffing and writes `BroughtBack`. The hook
reads `StoodDown` in `reviewsFresh` and makes no git call for it.

`internal/shellpolicy/staffing.go` has the three rules that say who may run
what: `checkAssign` (nobody in a session), `checkRecorder` (`set` and `amend`
are the project manager's), `checkManager` (the project manager runs nothing
else of `sdlc`), and `checkUnread` for a run of `sdlc` whose subcommand cannot
be read. `sdlcUses` finds each use of `sdlc` among a command's words.
`staffing assign` also refuses wherever `CLAUDE_CODE_SESSION_ID` is set.

`plugin/agents/project-manager.md` is the twelfth agent. It is not a reviewer
and not a loop role to the file rules. `parallel.Allowlist` names `set`,
`amend` and `show` one by one, never `sdlc staffing` as a whole.
