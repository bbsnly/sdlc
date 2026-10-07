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
`.sdlc/config.json`; `./task check` in `Taskfile.yml` does not run either.

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

## Taskfile.yml's `check` legs: what each reads, and whether `.gitignore` applies

`check` runs sixteen tasks in sequence. What a leg reads decides whether it can
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
reader finds a character of its own (`ownRune`). The callers are
`checkLoopState` (roots filtered by `reaches`, shapes `protectedShapes` plus the
five `stateFiles`), `checkFrozenTests` (every root, `literals` of both lists,
shapes `frozenShapes`) and `takeAway.find`. `unreadGlob` finds the first glob
word the hook cannot read, by its place in the segment, and why
(`unreadCause`); both find blocks refuse it with `unreadFinding` when the
command changes files from a root that `couldHold` says could hold loop state
or a frozen test, and `placedRoot` says whether the refusal can name where the root
is. `printedTo` lists the targets of `-fprint`, `-fprint0`, `-fprintf` and
`-fls` for `writtenTo`, `changesAFile` and `changesFiles`. Imports `pathrules`.

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
