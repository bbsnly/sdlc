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
