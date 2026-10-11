# Commands

- [Introduction](#introduction)

Commands you type:

- [`sdlc init`](#sdlc-init)
- [`sdlc doctor`](#sdlc-doctor)
  - [The story worktrees check](#the-story-worktrees-check)
- [`sdlc status`](#sdlc-status)
  - [Linked worktrees](#linked-worktrees)
- [`sdlc story list`](#sdlc-story-list)
- [`sdlc start`](#sdlc-start)
  - [Worktrees](#worktrees)
  - [Starting with more than one checkout](#starting-with-more-than-one-checkout)
  - [What start checks first](#what-start-checks-first)
- [`sdlc stop`](#sdlc-stop)
  - [Landing a story from its worktree](#landing-a-story-from-its-worktree)
- [`sdlc approve`](#sdlc-approve)
- [`sdlc unfreeze`](#sdlc-unfreeze)
- [`sdlc release`](#sdlc-release)
- [`sdlc parallel ID...`](#sdlc-parallel-id)
- [`sdlc log`](#sdlc-log)
  - [How a story's range is found](#how-a-storys-range-is-found)
- [`sdlc ack`](#sdlc-ack)
- [`sdlc cost`](#sdlc-cost)
  - [`sdlc cost add`](#sdlc-cost-add)
- [`sdlc version`](#sdlc-version)

Commands the loop's agents run:

- [`sdlc gate GATE STATUS`](#sdlc-gate-gate-status)
  - [How the commit range is recorded](#how-the-commit-range-is-recorded)
- [`sdlc artifact list`](#sdlc-artifact-list)
- [`sdlc artifact write NAME`](#sdlc-artifact-write-name)
- [`sdlc review list`](#sdlc-review-list)
- [`sdlc review add GATE ROLE VERDICT`](#sdlc-review-add-gate-role-verdict)
- [`sdlc staffing`](#sdlc-staffing)
- [`sdlc staffing show`](#sdlc-staffing-show)
- [`sdlc staffing set`](#sdlc-staffing-set)
- [`sdlc staffing amend AGENT`](#sdlc-staffing-amend-agent)
- [`sdlc staffing assign AGENT`](#sdlc-staffing-assign-agent), a person's
  command, refused to every session
- [`sdlc freeze`](#sdlc-freeze)
- [`sdlc escalate`](#sdlc-escalate)
- [`sdlc hook EVENT`](#sdlc-hook-event)

- [Where to go next](#where-to-go-next)

## Introduction

Most of the time you will not type an `sdlc` command at all. The `/sdlc:next`
command in Claude Code runs them for you, one gate at a time. This page is for
the moments when you want to see what a command does, run one yourself, or
understand a line it printed. Running `sdlc <command> --help` in your terminal
says the same thing as this page.

The commands you type most are [`sdlc init`](#sdlc-init) once per project,
[`sdlc doctor`](#sdlc-doctor) when something is not working,
[`sdlc status`](#sdlc-status) and [`sdlc story list`](#sdlc-story-list) to see
where things are, and [`sdlc approve`](#sdlc-approve) when the loop hands a story
to you.

A few more are yours alone, because the hook refuses them to an agent:
[`sdlc unfreeze`](#sdlc-unfreeze) when a frozen test is wrong,
[`sdlc release`](#sdlc-release), [`sdlc parallel`](#sdlc-parallel-id),
[`sdlc ack`](#sdlc-ack) to mark what you have read of what landed (its
companion [`sdlc log`](#sdlc-log) is open to everyone), and
[`sdlc staffing assign`](#sdlc-staffing-assign-agent), which the hook refuses
to every session. In the session working a story, the hook also refuses
[`sdlc stop`](#sdlc-stop) to the main conversation and to every agent while the
story still has a gate to pass; once every gate has passed it does not. In any
other Claude Code session the hook does not refuse `sdlc stop`, and the command
itself refuses ([SDLC-E0048](troubleshooting.md#sdlc-e0048)).

The commands you type come first on this page, up to
[`sdlc version`](#sdlc-version). After them come the ones the agents run, which
are here for reference whenever you need them. `sdlc staffing assign` is kept
there beside the other staffing commands, and is a person's all the same.

Each entry below says what the command is for, shows the usual way of running
it, lists its flags, and then covers the less common cases and the ways it
refuses. Two rules run through all of them:

- **`--json` works everywhere.** One line of JSON on standard output, for a
  script or a skill to read. Failures come back as JSON too, with the same
  fields as the prose.
- **A refusal carries four things**: what happened, why, what to do about it,
  and a code you can look up in [troubleshooting](troubleshooting.md). A
  required flag left off is the exception: `sdlc cost add` with no `--usd`
  prints `required flag(s) "usd" not set`, with no code.

## `sdlc init`

Run `init` once, at the start, to set the repository up for the loop.

```console
$ sdlc init
```

`init` reads the repository, detects the stack, and writes `.sdlc/config.json`
with commands that already match it, a backlog with one example story, the story
schema, and a `## SDLC Contract` section appended to `CLAUDE.md`. It never
overwrites a file you wrote, and it writes no `.gitignore`.

Then commit what `init` wrote, because each story's worktree is cut from trunk's
commits. After that, run `sdlc doctor` to check the setup, and open Claude Code
and type `/sdlc:next` to begin. [Getting started](getting-started.md) walks
through the first story.

If you later want the default settings back, `--force` restores them over an
existing setup:

```console
$ sdlc init --force
```

| Flag | What it does |
| --- | --- |
| `--force` | restore the default settings over an existing setup, keeping your stories, where they are (`backlog.path`) and your trunk branch (`git.trunk_branch`); refused while a story is being worked on ([SDLC-E0012](troubleshooting.md#sdlc-e0012)) |

`init` recognises Go (`go.mod`), Rust (`Cargo.toml`), Node (`package.json`) and
Python (`pyproject.toml`, `setup.py` or `requirements.txt`), tried in that order
at the root of the repository, so a Go service with a `package.json` for its
front-end tooling is a Go project. Anything else gets no commands and the
common test directories, for you to fill in.

## `sdlc doctor`

Check this project and say how to fix what is wrong. Run it first whenever
something is not working; it is the fast check.

```console
$ sdlc doctor
$ sdlc doctor --json
```

`doctor` looks at the git repository, the git command itself, the configuration —
including a setting it does not know, such as a misspelled one — the backlog,
the loop's state files the hook reads on every tool call and the commit
[`sdlc ack`](#sdlc-ack) recorded, the contract in `CLAUDE.md`, the programs your
configured commands would run, whether the hook would refuse any of them, and
whether `sdlc` is on your `PATH`. Every problem comes with what to do about it.
Exits non-zero when something is wrong, so it works in a script.

A quiet `doctor` is not a promise that the hook will allow every command: it
judges each command as the main conversation would run it from the project root,
with no story under way, so it does not judge the agent's role, the freeze, the
other checkouts or whether the story's gates have passed, and it does not judge
`fmt_file`, which the hook runs itself. It names a refused command by its key
and the rule, never prints the command, and never runs it.

In a repository with linked worktrees it names the checkout holding each story
under "loop state", in whichever checkout you run it. In a linked worktree it
reads the backlog and the records from the main checkout, and reports
[SDLC-E0052](troubleshooting.md#sdlc-e0052) when the repository has none.

### The story worktrees check

With [`git.worktrees`](configuration.md#git) on, the git command check is a
problem below git 2.17, the oldest that can remove a worktree again: install a
newer git, or set `git.worktrees` to false to begin each story in the checkout
`sdlc start` runs in. A `story worktrees` check appears only when there is
something to do:

- trunk has no `.sdlc/config.json` committed, so `sdlc start` would refuse to
  begin a story ([SDLC-E0053](troubleshooting.md#sdlc-e0053)): commit it and
  `CLAUDE.md` to trunk, or set `git.worktrees` to false in this checkout's
  `.sdlc/config.json`;
- trunk tracks files under `.sdlc/state`, the loop's own state, which every
  story worktree writes to, so no story worked in a worktree could land
  ([SDLC-E0055](troubleshooting.md#sdlc-e0055)): a person stops tracking them on
  trunk with `git rm -r --cached .sdlc/state` in the main checkout, adds
  `/.sdlc/state/` to `.gitignore`, and commits both;
- git lists a worktree under `.sdlc/worktrees/` that no checkout holds, such as
  a finished story's or one an interrupted start left. It says what is true of
  each, and what a person does about it, in the order a person takes it, and
  never counts the loop's own files under `.sdlc/state` as somebody's changes:
  - its directory is gone: `git worktree prune` in the main checkout makes git
    forget it;
  - it has changes nobody committed, which may be somebody's work: keep what is
    wanted, committed on `story/<ID>` or copied elsewhere, before removing
    anything;
  - `story/<ID>` holds commits trunk does not have, which are the story's work,
    and come after the changes where there are both: bring them to trunk, with
    trunk checked out, with `git merge --ff-only story/<ID>` in the main
    checkout, before removing either, and if git cannot fast-forward, first
    rebase `story/<ID>` on trunk in its worktree and run the tests;
  - it is on a branch other than `story/<ID>`, which start did not make: the
    branch is named as a shell reads it back without expanding anything, in
    single quotes where it needs them, and no command is built from it. A
    person looks at what it holds, with
    `git -C .sdlc/worktrees/<ID> log --oneline <trunk>..HEAD` in the main
    checkout, before merging or removing anything;
  - it is on no branch, and its HEAD has commits trunk does not have, as a
    rebase stopped part-way leaves it: "a commit on no branch is lost when its
    worktree is removed", so no command removes it. A person looks at them
    with that `git log`, and keeps what is wanted on a branch, with
    `git -C .sdlc/worktrees/<ID> branch <new name>`, before merging or removing
    anything;
  - git could not read what it holds: a person looks with that `git log`
    before removing anything;
  - its HEAD, on `story/<ID>` or on no branch, has nothing trunk does not: if
    no `sdlc start` is running for the story, remove it with
    `git worktree remove .sdlc/worktrees/<ID>`, and `git branch -d story/<ID>`
    where it is on that branch, in the main checkout. Where the loop's own
    files are all it has, it says they are safe to lose if git refuses over
    them.

Where there is more than one thing to say, each is a line of its own, followed
by its own `fix:` line, and in `--json` the detail and the fix hold the lines in
the same order, one to a line.

It never suggests `git branch -D` or `--force`. See
[more than one checkout](the-loop.md#more-than-one-checkout).

`doctor` does not run your test suite. It is meant to be the fast answer to "why
is this not working".

## `sdlc status`

When you want to know what the loop is working on and where it has got to, ask
`status`. It changes nothing, so it is safe to run at any point.

```console
$ sdlc status
$ sdlc status --json
```

It reports the active story, every gate's outcome, the state of the test freeze
and whether it is still intact, the backlog counts, and what `sdlc start` would
pick up next.

An active story with no gate left reads as `finished` rather than `in progress`,
because there is nothing left to work on it: what it wants is `sdlc stop`.

An active story that has gone from the backlog reads as `(not in the backlog)`,
with `"not_in_backlog": true` in `--json`. The commit gate refuses such a story,
so put it back, or run `sdlc stop` to end the iteration.

In addition, three fields in `--json` are worth knowing by name, because a skill
reads them to decide what to do:

| Field | What it says |
| --- | --- |
| `next_gate` | the gate to work now — the first one that has not passed. Absent when every gate is behind you, or when no story is active |
| `next` | the story `sdlc start` would pick up. Only present when nothing is active, and absent while a story waits for a person |
| `waiting` | every story handed to a person with `sdlc escalate` and not yet answered, with the question it asks |

Where the project has the [staffing](#sdlc-staffing) on, a `staffing` line says
whether one is recorded for the active story and names who it leaves out, and
`staffing` in `--json` is `none` or `recorded`.

### Linked worktrees

In a repository with linked worktrees, status also lists every story a checkout
holds, on a `held` line naming the checkout, and `held` in `--json` has one
entry per story, with its `checkout`, `here` for the one you ran it in, `gone`
for a checkout that is no longer there, and `contenders` when more than one
checkout names the story and none holds it. `next up` passes over what another
checkout holds. A story this checkout names as its own that another holds, or
that nobody holds, says so on its line, because `sdlc start` here will not carry
it on, and status then ends with what a person does about it instead of sending
them to carry on ([more than one checkout](the-loop.md#more-than-one-checkout)).

## `sdlc story list`

See the whole backlog at a glance, and what is holding each story back.

```console
$ sdlc story list
$ sdlc story list --json
```

The list marks the story `sdlc start` would choose and shows every story's
status. A story waiting on a `depends_on` story that is not done names it, and
says when that one is dropped or not in the backlog, because then it never will
be.

In a repository with linked worktrees, a story a checkout holds ends with where
it is held, and `held_by` in `--json` names that checkout:

| Ends with | What it means |
| --- | --- |
| `(held in "<checkout>")` | another checkout holds it |
| `(held in "<checkout>", this checkout)` | the checkout you ran it in holds it |
| `(held in "<checkout>", gone: see sdlc release <ID> --reason "...")` | the checkout that holds it no longer exists ([SDLC-E0051](troubleshooting.md#sdlc-e0051)) |
| `(held by none: more than one checkout names it ("<checkout>", "<checkout>"))` | several checkouts name it and none holds it until a person runs `sdlc stop` in all but one |

The mark is what `sdlc start` would pick in the checkout you run it in, which is
never a story another checkout holds.

## `sdlc start`

Begin an iteration on the next story, or on the one you name. `/sdlc:next` runs
this for you, so you will rarely type it yourself.

```console
$ sdlc start
$ sdlc start AUTH-3
```

With no story named, `start` picks up a story already under way, or takes the
next runnable one: resume first, then priority, then id. It refuses if an
iteration is already running on another story; on the same story it resumes and
changes nothing about the story. With `--json` it reports `resume` and
`next_gate`, the first gate that has not passed, so a session picking a story
back up carries on from there, and `session`, the Claude Code session it
recorded, empty when it ran in none.

Run in Claude Code, it records the session it runs in, in
`.sdlc/state/session`. That session and its agents are the ones the hook holds
to the story; every other session in the repository is left alone, except that
the `sdlc` commands that change the story refuse to run there
([SDLC-E0048](troubleshooting.md#sdlc-e0048)). Starting or resuming the story
in another session moves it there. Run outside Claude Code, it records no
session. A new story then has nothing enforced in any session until one picks
it up with `/sdlc:next`, and `sdlc status` says so meanwhile. A
story resumed that way keeps the session already recorded, since the command
cannot tell that the story has moved.

Naming a story picks it over the priority order, and nothing else. A story that
is `dropped`, `blocked` or marked `done`, or that waits on a `depends_on` story
that is not done, is refused as it would be passed over
([SDLC-E0010](troubleshooting.md#sdlc-e0010)).

`start` also refuses a story whose gates have all passed, rather than putting
finished work back in progress. The way back into a finished story is to record
the gate that failed — see [SDLC-E0033](troubleshooting.md#sdlc-e0033).

### Worktrees

With [`git.worktrees`](configuration.md#git) on, the default, a new story begins
in a worktree of its own. `start` makes `.sdlc/worktrees/<ID>` in the main
checkout, on a new branch `story/<ID>` cut from the local trunk branch, runs
`commands.smoke` there, and records the story as held by that worktree, whose
`.sdlc/state` it writes rather than this checkout's. It says where, under the
story's title:

```text
Worktree: "/home/dev/project/.sdlc/worktrees/US-001"
Branch: story/US-001
```

and ends `Next: a person opens Claude Code in the worktree above and types
/sdlc:next there to work the story; /sdlc:next typed in a new session in this
checkout begins the next story instead.` A new session opened in the checkout
start ran in holds no story, so `/sdlc:next` there starts again, and passes
this story over as held. A Claude Code session that ran start itself holds the
story, and `/sdlc:next` there carries it on, in this checkout or in the
worktree.
Run in the story's worktree, start ends `Next: a person types /sdlc:next in
Claude Code, opened in this checkout or in the worktree above, to work the
story there.` With `--json` it adds `worktree`, the worktree's absolute path,
`branch`, and `main`, the main checkout's absolute path, where the backlog and
the story's record are kept.
Picking up a story its worktree holds prints the same path and branch, from the
main checkout or from the worktree, and makes nothing. A story the main
checkout holds, or a worktree made by hand, prints neither, and the output is
as it was with `git.worktrees` false.

### Starting with more than one checkout

In a repository with linked worktrees, the checkout that starts a story holds
it until it is finished or a person runs [`sdlc release`](#sdlc-release). With
no story named, start passes over what another checkout holds, and says on
standard error which story it passed over because the checkout holding it is
gone, as it is for a single checkout moved while its story was stopped: if this
checkout was moved from there, what it has not committed is that story's own.
Naming a story another checkout holds is refused with
[SDLC-E0050](troubleshooting.md#sdlc-e0050), or
[SDLC-E0051](troubleshooting.md#sdlc-e0051) when the checkout holding it is
gone, except a story held in its own worktree under `.sdlc/worktrees/`: named
in any other checkout, the main checkout included, it is picked up there, in
its worktree. Picked up in
another Claude Code session, the story is taken over, and the session that was
working it is refused the story's `sdlc` commands from then on
([SDLC-E0048](troubleshooting.md#sdlc-e0048)). A Claude Code session already
working a story in one checkout is refused another in the next
([SDLC-E0050](troubleshooting.md#sdlc-e0050)). See
[more than one checkout](the-loop.md#more-than-one-checkout).

### What start checks first

Before it begins a new story — not one it is picking up again — it checks that
trunk is somewhere to start from. With `git.worktrees` on: trunk is not behind
`origin` when `git.remote` is on
([SDLC-E0040](troubleshooting.md#sdlc-e0040)), and `commands.smoke` passes in
the new worktree ([SDLC-E0041](troubleshooting.md#sdlc-e0041)); a worktree that
cannot be made is [SDLC-E0053](troubleshooting.md#sdlc-e0053), and a refused
start takes its worktree and branch away again, or says what it could not. With
it off: HEAD is on
`git.trunk_branch`, nothing is uncommitted outside `.sdlc/` and the backlog,
trunk is not behind `origin` when `git.remote` is on, and `commands.smoke`
passes. Each has its own refusal, from
[SDLC-E0038](troubleshooting.md#sdlc-e0038) to
[SDLC-E0041](troubleshooting.md#sdlc-e0041).

## `sdlc stop`

End the current iteration without recording a result. Don't worry about losing
work: stopping puts a story down for now, and it is also the last step of a
finished story, which `stop` then tidies away (or lands, from its worktree).

```console
$ sdlc stop
```

Stopping does not undo anything. The gates already recorded stay recorded, the
story stays in progress, and starting again resumes it.

A story whose gates have all passed is a different case, and `stop` says so: it
is done, the test freeze it was holding is lifted, what the hook wrote down about
its commits is removed from the repository's git directory, and the next
`sdlc start` moves on to the next story instead of reopening it. An unfinished
story keeps its freeze and what was written down, so stopping and starting again
cannot be a way round either.

In a checkout whose `.sdlc/state/active` names a story another checkout holds,
or that more than one checkout names, stop puts this checkout's copy down and
writes nothing to the story's record, and its last line says where the story
carries on rather than that `sdlc start` picks it up here.

From a Claude Code session other than the one working the story, `stop` refuses
with [SDLC-E0048](troubleshooting.md#sdlc-e0048) and changes nothing.

### Landing a story from its worktree

In a story's own worktree, `.sdlc/worktrees/<ID>`, with every gate passed, stop
**lands** the story instead: it moves trunk to the story's commits by
fast-forward, and the story is done
([the loop](the-loop.md#done-when-it-lands) lists what it checks first). The
same holds with no iteration running, for a finished story the worktree holds,
such as one a person answered for after an escalation.

```console
$ sdlc stop
sdlc: running commands.test on 8a0e5d6c1f2b in .sdlc/worktrees/US-001
main moved from 3f1c2a9d4b7e to 8a0e5d6c1f2b by fast-forward; the files in "/home/dev/project", which has main checked out, moved with it.
The backlog and US-001's record are changed in the main checkout and not committed.
Ended the iteration on US-001. Every gate passed, so the story is done and `sdlc start` moves on to the next one.
Removed .sdlc/worktrees/US-001, with the files git ignored in it, and story/US-001.
```

The `running` lines go to standard error, one for each configured command the
landing runs, and only where the story was rebased or its change is not the one
reviewed ([configuration](configuration.md#at-landing)). Where no checkout has
trunk checked out, the first line ends `no checkout has main checked out, so
only the branch moved.` instead. Where trunk already held the story, it says
`US-001 was already on main at 8a0e5d6c1f2b; recorded it as landed.` Where
some of the reviewed change was on trunk before, a line follows that names the
commit it began at and the range the record keeps as what landed. With no
iteration running, the third line starts `Every gate on US-001 passed` instead
of `Ended the iteration`. A worktree holding changes nobody committed, or one a
person locked, is kept, and the last line says so, names the files, and sends
you to `sdlc doctor`:

```text
Kept .sdlc/worktrees/US-001 and story/US-001: it holds changes nobody committed, "notes.txt". `sdlc doctor` in the main checkout says what to do with it.
```

With `--json` these lines go to standard error, and standard output has `ok`,
`story`, `was_active` and `done`, which is `true`.

Every refusal leaves trunk where it was, and ends with `Nothing landed: main is
unchanged, and the iteration on US-001 is still running`, or, with no iteration
running, `... and US-001 waits in .sdlc/worktrees/US-001`. Trunk has moved past
the story: [SDLC-E0054](troubleshooting.md#sdlc-e0054). The worktree, its
commits or a command are not fit to land:
[SDLC-E0055](troubleshooting.md#sdlc-e0055). A checkout with trunk checked out
has changes nobody committed that the fast-forward would write over:
[SDLC-E0056](troubleshooting.md#sdlc-e0056). The change is not the one the
reviewers saw: [SDLC-E0029](troubleshooting.md#sdlc-e0029). Another stop is
landing it already: [SDLC-E0006](troubleshooting.md#sdlc-e0006). The others each
have an "At landing" paragraph in [troubleshooting](troubleshooting.md).

Stop in a story's worktree always lands a finished story. To put one down
without landing it, hand it to a person with
[`sdlc escalate`](#sdlc-escalate).

## `sdlc approve`

A person's answer to an escalation. When `sdlc status` says a story is waiting
for you, this is the command that lets it carry on.

```console
$ sdlc approve US-001
$ sdlc approve US-001 --reject "the migration has no way back"
```

| Flag | What it does |
| --- | --- |
| `--reject` | send the work back instead, saying why |

Name the story. Without an id, `approve` answers for the story being worked on,
and `sdlc escalate` ends the iteration, so after an escalation there is none
and it is refused with [SDLC-E0011](troubleshooting.md#sdlc-e0011). The answer
goes on the record with the tree it was given for, the story goes back to
`in_progress`, and `sdlc start` picks it up again.

> [!WARNING]
> Run it in your own terminal. The hook refuses it from a tool call in any
> session while a story waits for an answer or is under way, because an agent
> that could answer would be approving its own work.

If nothing on the story is waiting for a decision, `approve` is refused with
[SDLC-E0035](troubleshooting.md#sdlc-e0035). A story held by another checkout is
refused with [SDLC-E0050](troubleshooting.md#sdlc-e0050), and `--reject` with no
reason is [SDLC-E0034](troubleshooting.md#sdlc-e0034).

## `sdlc unfreeze`

Lift the test freeze, on the record. Only a person does this, and only when a
frozen test is wrong.

```console
$ sdlc unfreeze --reason "AC-2's test asserted the old error message"
```

| Flag | What it does |
| --- | --- |
| `--reason` | why the frozen tests have to change. Required |

> [!WARNING]
> Run it in your own terminal. The hook refuses it from a tool call while a
> story is under way or waiting for an answer, because lifting the freeze is the
> move an agent would make to reach green.

Without `--reason` it is refused with
[SDLC-E0026](troubleshooting.md#sdlc-e0026). With no freeze to lift, or a freeze
that belongs to another story, it is refused with
[SDLC-E0023](troubleshooting.md#sdlc-e0023). It lifts the freeze of the story
being worked on, so with no iteration running it is refused with
[SDLC-E0011](troubleshooting.md#sdlc-e0011). After an escalation, which ends
the iteration, answer with `sdlc approve <ID>` and run `sdlc start <ID>` first.

> [!NOTE]
> The reason is required because that is the whole point. Lifting the freeze is
> sometimes right, when a test encoded the wrong behaviour, and it is also exactly
> the move an agent would make to reach green. Recording why is what tells the
> two apart.

Unfreezing also sends the story back to Gate 3. The tests are about to change, so
`tests_frozen`, and every gate passed after it, go back to `pending` with the
reason as their note. They are listed under `reopened`, in `--json` as well.

## `sdlc release`

Let a story go from the checkout holding it, so that any checkout can start it.
You will want this when a worktree was deleted or a story should move somewhere
else.

```console
$ sdlc release US-001 --reason "the worktree it was started in was deleted"
Released US-001: the checkout at "/home/dev/project-wt" no longer holds it. "sdlc start US-001" picks it up in any checkout.
```

| Flag | What it does |
| --- | --- |
| `--reason` | why the story is moving to another checkout. Required |

Every checkout of a repository, the main checkout and each linked worktree, works
the one backlog. A story is held by the checkout that started it until it is
finished or dropped, and no other checkout can start it, record a gate for it, or
answer for it ([SDLC-E0050](troubleshooting.md#sdlc-e0050)). Release it when that
checkout is gone ([SDLC-E0051](troubleshooting.md#sdlc-e0051)), or when you want
the story worked somewhere else.

> [!WARNING]
> Run it in your own terminal. The hook refuses it from a tool call in any
> session, because an agent that could run it could take another checkout's
> story.

It refuses while an
iteration is running on the story ([SDLC-E0012](troubleshooting.md#sdlc-e0012)):
`sdlc stop` in the checkout named ends it first. A story nothing holds is not an
error: it says `Nothing to release` and changes nothing.

Nothing is moved. Anything the checkout that held the story has not committed
stays there, and the output says so while that checkout exists. The reason goes
on the story's record; the checkout's path does not. `sdlc start` and
`sdlc approve` then say that work the old checkout had not committed stayed
with it, and that if this checkout is that one, moved here, the work is here. A
freeze does not move either: in a checkout that picks the story up without it,
`sdlc unfreeze --reason ...` lifts it and `sdlc freeze` takes it again. Release
says so only where the checkout it runs in does not hold the story's freeze. A
checkout that was moved took its freeze with it, and start and approve then say
nothing about where the work is. In `--json` the output is `ok`, `story` and
`checkout`, the checkout that held it, absent when none did.

## `sdlc parallel ID...`

Work several stories at once: one headless Claude Code session and one worktree
for each story named. Reach for it when the stories are independent and you are
happy to come back to find them waiting on you.

```console
$ sdlc parallel US-001 US-002 US-003
sdlc: the sessions are headless and cannot ask, so each stops at its first tool that default permissions deny
sdlc: running 2, waiting 1
sdlc: the session for US-002 has ended
sdlc: running 2, waiting 0
sdlc: the session for US-001 has ended
sdlc: the session for US-003 has ended
US-001  waiting on you, every gate passed  Add the export command
US-002  ended early, at analysis  Rate-limit the importer
US-003  waiting on you, at plan  Drop the legacy parser

Ended early with nothing waiting on you. The usual cause is a tool that default
permissions deny, which a session with nobody to ask cannot get past. Run it again to
resume where it stopped, or look at where each story is:
  sdlc parallel US-002
  sdlc status

Waiting on you:
  cd /path/to/project/.sdlc/worktrees/US-001 && sdlc stop
  cd /path/to/project/.sdlc/worktrees/US-003 && sdlc approve US-003
```

| Flag | What it does |
| --- | --- |
| `--max` | the most sessions to run at once. A whole number, 1 or more; the default is 2. The rest start, in the order named, as a slot frees |
| `--unattended` | give each session the built-in list of tools to use without asking, which [the enforcement page](enforcement.md#unattended-sessions) states in full. It is never a skip of permissions, and `parallel.allow` can only narrow the list |

Run it in your own terminal, from any checkout of the project. Each session runs
`/sdlc:next` for its story, so the story is started in a worktree of its own at
`.sdlc/worktrees/<ID>` and worked there. Run it again after an answer or an
interrupt and a story under way resumes where it was left.

The sessions are **not interactive**: no permission prompt reaches anyone. A
session is stopped by the first tool that default permissions deny, and the
story is shown as `ended early`. Each session costs what a full loop costs, and
several run at once. If `claude` is not on your `PATH` the shell says so beneath
the story, and the story shows as failed.

Every story is checked before any session starts, with the answers `sdlc start`
would give, and a refusal starts none: not in the backlog
([SDLC-E0057](troubleshooting.md#sdlc-e0057)), not runnable
([SDLC-E0058](troubleshooting.md#sdlc-e0058)), held by another checkout
([SDLC-E0059](troubleshooting.md#sdlc-e0059)), named twice
([SDLC-E0060](troubleshooting.md#sdlc-e0060)), waiting on a person
([SDLC-E0064](troubleshooting.md#sdlc-e0064)) or finished
([SDLC-E0065](troubleshooting.md#sdlc-e0065)). `--max` that is not a whole number
of 1 or more is [SDLC-E0063](troubleshooting.md#sdlc-e0063), a project with
`git.worktrees` off is [SDLC-E0062](troubleshooting.md#sdlc-e0062), and a
`parallel.allow` that names a tool the built-in list does not have, with
`--unattended`, is [SDLC-E0066](troubleshooting.md#sdlc-e0066).

> [!WARNING]
> It is a person's command: the hook refuses it from a tool call in any session,
> and the command itself refuses to run when the session variable is set
> ([SDLC-E0061](troubleshooting.md#sdlc-e0061)), because starting model sessions
> with tools pre-approved is work nobody asked for.

When the sessions have ended the output says what became of each story, read from
its record and not from what the session said, in the order named:

- `waiting on you`: an escalation nobody has answered, or every gate has passed and
  the story waits to land. The commands are gathered at the end, each with the
  `cd` to run it from.
- `ended early`: the session ended normally and nothing is waiting on you. The
  story is where the session left it. The usual cause is a tool that default
  permissions deny: pass `--unattended`, or
  [add the command it names](enforcement.md#adding-a-command) to your Claude
  Code settings, and run it again.
- `failed`: the session exited with a failing status, which is shown.

The last of each stopped or failed session's output is printed beneath its story,
with control characters spelled out. Nothing is written to disk. `sdlc parallel`
writes no record, claim or status of its own and never records a story done; the
exit status is 1 when any story failed, and 0 otherwise. In `--json`, standard
output is one document, `{"ok": ..., "stories": [...]}`, each story with `story`,
`gate`, `state` (`waiting`, `stopped` or `failed`), `command` and `exit`, and
the counts are still printed to standard error.

Interrupting with Ctrl+C ends every session and everything it started. If a
flag's spelling is not accepted by the installed `claude`, the sessions fail and
say so beneath their stories.

## `sdlc log`

Look back over the stories committed to trunk, and what each one went through.
Reach for it when you want to review what the loop landed while you were away.

```console
$ sdlc log
$ sdlc log --since HEAD~20
$ sdlc log --all --json
```

| Flag | What it does |
| --- | --- |
| `--since` | list the stories committed after this commit — a hash, a tag, or a name like `HEAD~20` — instead of after the last one acknowledged |
| `--all` | list every committed story, whatever has been acknowledged. Not together with `--since` |

The log has one entry per story whose commit gate has passed, oldest first: the
commits it landed in, how many rounds each gate's reviews took, every block, and
what is worth a second look. That means a freeze lifted with `sdlc unfreeze`,
gates reopened, a question handed to a person and whether it was answered, and
what the story cost. It changes nothing.

The log starts after the last commit a person acknowledged with
[`sdlc ack`](#sdlc-ack), the one `.sdlc/state/acknowledged` names: a story whose
commits end in that commit's history has been read. With nothing acknowledged,
it lists every story. A file that names no commit git has is refused —
[SDLC-E0047](troubleshooting.md#sdlc-e0047) — rather than read as nothing
acknowledged.

### How a story's range is found

The range is the one the commit gate recorded; see
[`sdlc gate`](#sdlc-gate-gate-status). For a story that landed from its
worktree, it is the commits that reached trunk, as they are after any rebase,
and it prints the same in every checkout. The record's `landed` event says how
trunk moved, and, where some of the reviewed change was on trunk before the
landing, the commit it began at. A story committed by an `sdlc` that did
not record one is found by its commit messages instead: the newest commit in
HEAD's history whose message names the story's id as a whole word ends the
range, the commit before the oldest one starts it, and the entry says the range
was inferred. A later commit that names the id, such as `Revert US-001`, moves
that end, and can bring a story already acknowledged back into the log. When no
message names the story either, its range is `unknown`, and nothing is guessed.
A story whose commit gate recorded no range on purpose, because no single range
holds only the commits the loop watched it land, is `unknown` too, and says so:
`range      unknown: the loop watched this story's commits, but no single range
holds only them`, with `range_unknown` in `--json`. Its commit messages are
not read for it. A recorded range whose commit gate saw HEAD past its end
says so beneath it, in the gate's own words — `range      head is <head>, past
<end>: nothing that landed after <end> was watched` — with the full HEAD as
`head_past_commit` in `--json`. A story that cannot be placed — its range is
unknown, or ends in a commit that is not on this branch (`off_branch`) or not in
this repository (`missing`) — is always listed.

In `--json`, `range_source` is `record`, `message` or `unknown`, and
`since_source` says where the log started: `acknowledged`, `flag` for
`--since`, `all` for `--all`, or `none` when nothing is acknowledged. `since` is
the full commit it started after, and `spent_usd` is always there, `0` for a
story with no spend recorded.

An entry for a story with a [staffing](#sdlc-staffing) has a `staffing` line
for each agent — what it was asked for, or that it was left out, and why — and
one for each change made to it. After them comes the record's own line for each
time the staffing was recorded, amended or assigned, with when: those lines stay
when a staffing is lost or recorded a second time, and a second recording says
how many changes came before it. In `--json` that is `staffing`, with `agents`
and `amendments` as [`sdlc staffing show`](#sdlc-staffing-show) gives them, and
`staffing_events`.

An entry for a story that is not finished, in a repository with linked
worktrees, names the checkout that holds it on a `held` line, and as `held_by`
in `--json`.

> [!WARNING]
> A range is what the loop saw the story's session land, as far as the hook could
> tell a command was a commit: a person's commit made in the same window is left
> out of it, and a story whose commits somebody else's sits between has no range at
> all. It is not proof, and it is not complete: a commit the hook did not watch is
> missing from it, and a range recorded where the hook watched nothing is the
> whole window, which can hold anybody's commits.
> [Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md#ranges-to-read-with-care)
> says what to read with care. A story reopened after it was committed spans
> everything that landed in between. It is a list to read, not a list to revert.

To go through the log with the assistant, run `/sdlc:trunk-review`: see
[Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md).

## `sdlc ack`

Once you have read the log, tell `sdlc` how far you got, and the next
`sdlc log` starts after that point.

```console
$ sdlc ack --through HEAD
$ sdlc ack --through 3f2a9c1e0b7d
```

`ack` writes the full commit to `.sdlc/state/acknowledged`, and `sdlc log` starts
after it from then on. Nothing else changes.

| Flag | What it does |
| --- | --- |
| `--through` | the commit you have read the log up to: HEAD, or one in its history. Required |

> [!WARNING]
> Run it in your own terminal. The hook refuses it from a tool call, because an
> agent that could run it would be deciding which stories nobody needs to look at.

There is no default. A commit git cannot read, or one that is not on this branch,
is refused with [SDLC-E0034](troubleshooting.md#sdlc-e0034). The output names the
commit acknowledged before, and says when the new one comes before it, because
the log then lists the stories in between again; in `--json`, `back` is `true`.
`through` is a full commit. `previous` is too, or the file's text when git does
not have that commit, and it is absent when nothing was acknowledged. Running
`sdlc ack` is how a file that names no commit
([SDLC-E0047](troubleshooting.md#sdlc-e0047)) is put right.

The file says how far the person using this clone has read, so whether it is
committed is your call.

## `sdlc cost`

See what the story has cost so far, against what it was expected to.

```console
$ sdlc cost
US-001  $12.40 spent of $60.00 (20.7%)

$ sdlc cost add --usd 1.42 --note "gate 4, five reviewers"
US-001  $13.82 spent of $60.00 (23%)
```

### `sdlc cost add`

Records one amount against the story being worked on, or the one `--story` names.

| Flag | What it does |
| --- | --- |
| `--usd` | the amount, in US dollars. Required |
| `--note` | what it was spent on |
| `--story` | the story it was spent on, when no iteration is running on it. `sdlc cost` takes it too |

With no story being worked on and none named, `cost add` is refused with
[SDLC-E0011](troubleshooting.md#sdlc-e0011). An empty, negative or non-numeric
`--usd` is [SDLC-E0034](troubleshooting.md#sdlc-e0034), and a story another
checkout holds is [SDLC-E0050](troubleshooting.md#sdlc-e0050).

`budget.per_story_usd` in `.sdlc/config.json` says what one story is expected to cost, and
`alert_fractions` says where along the way to say so. An alert goes to standard error, once,
on the entry that crosses it, with `--json` as well as without — so a runner reading standard
output for the number still sees it, and the same warning does not repeat on every entry
afterwards.

> [!NOTE]
> **Nothing here blocks.** A budget that stops a story halfway leaves the work
> stranded between gates, which costs more than the overspend it prevents. The
> entry that uses up the budget says so, whatever `alert_fractions` holds, and
> names `sdlc stop` if that is the call.

Where the number comes from is the runner's business. A headless session reports its cost only
once it is over, and by then the iteration it ran may have ended, so the runner notes the story
first and names it:

```console
$ story=$(sdlc status --json | jq -r '.active // .next.story')
$ cost=$(claude -p "/sdlc:next" --output-format json | jq .total_cost_usd)
$ sdlc cost add --story "$story" --usd "$cost"
```

An interactive session has `/cost`. An empty or unparseable amount is refused rather than
recorded as zero, because a story that silently cost nothing is the one wrong answer nobody
questions. [Tracking what a story costs](guides/tracking-what-a-story-costs.md) walks
through recording, reading back and budgeting.

## `sdlc version`

Print the version of `sdlc` you are running, which is the first thing to include
when you report a problem.

```console
$ sdlc version
$ sdlc version --short
$ sdlc version --json
```

| Flag | What it does |
| --- | --- |
| `--short` | print the version alone, with nothing around it |

With `--json` it prints `version`, `commit`, `date` and `dirty`, and with
`--short` as well, `version` alone.

## `sdlc gate GATE STATUS`

Record the outcome of one gate. The agents running a story call this as each
gate comes to an end; you would reach for it yourself only to reopen a gate or
to record one by hand.

```console
$ sdlc gate dor pass --note "criteria are testable"
$ sdlc gate analysis pass --security-sensitive --note "token crosses a boundary"
$ sdlc gate code_review fail --note "AC-2 is untested"
```

| Flag | What it does |
| --- | --- |
| `--note` | why the gate came out this way, in one line |
| `--story` | record against this story instead of the one being worked on. The id has to be in the backlog; a mistyped one is refused rather than starting a record nothing reads |
| `--security-sensitive` | the story touches a trust boundary, so the security review blocks. Set it at the analysis gate; unset is assumed to mean yes |

The gate record keys are `dor`, `analysis`, `tests_frozen`, `plan`, `design_review`,
`implementation`, `verification`, `verifier_review`, `code_review`, `commit` and
`retro`. The outcomes are `pass`, `fail` and `pending`.

`gate` records a decision; it does not make one. It does refuse to record a
**pass** that is not true: see [the loop](the-loop.md) for what each gate
requires. A `fail` is always recordable, because a gate can fail precisely
because its work could not be done, and the loop has to have somewhere to put
that.

Recording the last gate is also what finishes the story: when no gate is left
unpassed, the story's status in the backlog becomes `done`, except for a story in
its own worktree, which is done when
[`sdlc stop`](#landing-a-story-from-its-worktree) lands it. That is
read from the record rather than from the gate's name, so a gate recorded as
failed afterwards — a code review reopened on work already committed — puts the
story back to `in_progress`, where the rework belongs.

A gate recorded as failed `loop.max_rework_rounds` times since a person last
answered for the story hands the story to that person, as
[`sdlc escalate`](#sdlc-escalate) `gate_failing` would, and says so —
`handed_over` in `--json`.

That failure and the hand-over are recorded together or not at all. Where the
story cannot be handed over — git cannot measure the tree, or the backlog, the
claim or `.sdlc/state` cannot be written — the command is refused with the code
of whatever stopped it, exits non-zero, and leaves the record, the backlog and
the iteration as they were. The refusal says so, after its own reason and fix.
With git missing:

```text
sdlc: git add did not finish

  why  git add gave no error message; git may not be installed or on PATH, this may not be a repository git can read, or git may have stopped without saying why. The gate failure was not recorded, because recording it would reach the limit at which the story is handed to a person, and the story could not be handed over
  fix  check that git is installed and that this directory is a repository git can read; then run the same command again, which is safe: the gate failure is counted once

  SDLC-E0021  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0021
```

Put the cause right and run the same command again: the failure is counted
once, and the story is handed over. A failure below the limit asks for none of
this and is recorded as it always was. The promise is for a command that is
refused; one that is killed part-way can leave the failure and the question on
the record with the backlog not yet showing it, which
[`sdlc approve`](#sdlc-approve) answers.

When recording a gate reopens the gates after it, the command names them in gate
order on a `reopened:` line beneath the note, and `--json` lists them under
`reopened`. Each of those gates is back to `pending` and has to be passed again
before the story can move on. Neither the line nor the key appears when nothing
was reopened, and [`sdlc log`](#sdlc-log) shows the same gates.

```console
$ sdlc gate plan fail --note "the plan misses the migration"
US-001  plan  fail
  the plan misses the migration
  reopened: design_review, implementation, verification
```

### How the commit range is recorded

The rest of this section explains the range that the `code_review` and `commit`
passes record. You do not need it to use the command, but it is what to read
when a `landed in` line surprises you.

The first `code_review` pass records HEAD at that moment, where the story's
commits can start. Each `commit` pass records the range the story landed in and
says so: `landed in <start>..<end>`, with each hash cut to 12 characters, or
`commit_base` and `commit` in full with `--json`. In a story's own worktree it
says `committed on story/<ID> in <start>..<end>` instead, because trunk has none
of it until the story lands, and the pass that leaves no gate unpassed ends
`` Every gate on <ID> has passed. `sdlc stop` in .sdlc/worktrees/<ID> lands it on <trunk>. ``
When the story lands, the record's range is replaced by the commits that reached
trunk.

The range is worked out from what the hook watched. Each time it lets the
session's command through and can tell the command commits, it writes down the
HEAD the command starts from, in the repository's git directory at
`<git-dir>/sdlc/commits/<story>/` — never in the working tree, never committed,
and never pushed. The story's commits are the ones since the code review whose
first parent is a HEAD the hook wrote down, and the range is the one pair of
commits that holds exactly those: a person's commit made after the story's is
left out of it. The gate puts the pair back to git before it records it; where
no single range holds only the watched commits — somebody else's commit sits
between two of the story's, or a merge brought in a side branch — it records no
range, sets `range_unknown` in `--json`, and says so in place of `landed in`:

```text
  no range recorded: the loop watched this story's commits, but no single range holds only them
```

When the range it records ends before HEAD, it says which commit HEAD is:

```text
  landed in 3f1c2a9d4b7e..8a0e5d6c1f2b
  head is c4d9e2f7a130, past 8a0e5d6c1f2b: nothing that landed after 8a0e5d6c1f2b was watched
```

It keeps that HEAD on the story's record, so the fact outlasts the output:
`head_past_commit` in `--json` holds it in full, and [`sdlc log`](#sdlc-log)
repeats the line. Each pass replaces what the last one kept, and a range whose
end is HEAD keeps nothing.

That is usually a person's commit made after the session's, and is then as it
should be. It is also the only sign of the two cases where the range stops short
of the story's work: a command that committed twice, of which the hook watched
only the start, and history rewritten under the loop. When the range's end *is*
HEAD there is no signal at all that anything unwatched landed: a commit the hook
did not see, made straight on a HEAD it did, joins the range and nothing says
so. What the hook wrote down is removed when the finished story is stopped, so
look at it before `sdlc stop` if the line gives you a reason to.

Where the hook watched nothing — the plugin is not installed, enforcement is
switched off for the session, or the story was already under way when this
version arrived — the range is what it always was: from HEAD when code review
first passed to HEAD when the commit gate passes, which can hold commits
somebody else made in that window. The start is left out, and the output says
`landed in <end>`, when it is not known: the repository had no commits yet when
code review first passed, or an older `sdlc` passed it.

A hash in the note has to be inside the range this pass records, or the pass is
refused with [SDLC-E0049](troubleshooting.md#sdlc-e0049): seven to forty
hexadecimal characters that git resolves to a commit count as naming it, and
anything else is prose. Where the start is not known, the one commit the record
names is the range the note is held to. The note needs no hash. The check is
skipped when no range is recorded, so it is a consistency check between the
note and the record, not evidence of which commits are the story's.

The end is forgotten when the commit gate stops standing, and every pass records
its answer over the last one's. A pass that records a watched range moves the
start to that range's start as well, so `commit_base` is then no longer where
code review began. When the hook watched nothing the start stays, even when a
finished story is reopened later, so that story's range can also span
everything that landed on trunk in between.

## `sdlc artifact list`

See which documents this version of `sdlc` can store. The names it prints are
the ones `sdlc artifact write` accepts.

```console
$ sdlc artifact list
```

## `sdlc artifact write NAME`

Store one of a gate's documents, such as the analysis or the plan, for the story
being worked on. The agent whose gate it is runs this; the document arrives on
standard input, or from a file.

```console
$ sdlc artifact write analysis < analysis.md
$ sdlc artifact write threats --file /tmp/threats.md
$ sdlc artifact write plan <<'SDLC_DOCUMENT'
# Plan
...
SDLC_DOCUMENT
```

| Flag | What it does |
| --- | --- |
| `--file` | read the document from this path instead of standard input |
| `--story` | store against this story instead of the one being worked on. The id has to be in the backlog |

The names are `analysis`, `threats`, `test_plan`, `plan`, `verification` and
`retro`.

Writing a document again replaces it, so a gate that ran twice has one record,
not two. An empty document is refused, and so is one larger than a megabyte,
because storing a truncated document would leave every later gate reviewing
something that stops mid-sentence.

> [!NOTE]
> This is the only way these files are written. The hook refuses anyone who
> edits them in place, including the agent whose gate it is. See
> [enforcement](enforcement.md).

## `sdlc review list`

Check which reviews each gate expects and where they have got to. It is the
quickest way to see why a gate is still waiting.

```console
$ sdlc review list
$ sdlc review list --gate design_review --json
```

| Flag | What it does |
| --- | --- |
| `--gate` | only this gate's reviews |
| `--story` | for this story instead of the one being worked on |

For every expected review, the list says whether it can block the gate, what the
latest verdict was, and whether it is **stale**, which means the reviewer looked
at something that has changed since. A `blocking` reviewer has to approve; an
`advisory` one only has to report; perf is `on budget`: it never has to approve,
and its block stops the gate, because it blocks only on a performance budget the
contract states.

Where the story has a [staffing](#sdlc-staffing), a reviewer it leaves out
reads `left out` with the staffing's reason, and the gate does not wait for it:
`left_out` and `reason` in `--json`. A reviewer the staffing leaves out that a
changed path brings back to the code review is listed as any other, with the
path: `brought_back_by` in `--json`.

## `sdlc review add GATE ROLE VERDICT`

Record one reviewer's conclusion. Each reviewing agent runs this when it has
finished, with the review itself on standard input.

```console
$ sdlc review add design_review architect block --note "AC-3 has no step" < review.md
$ sdlc review add code_review perf note --file /tmp/perf.md
```

| Flag | What it does |
| --- | --- |
| `--file` | read the review from this path instead of standard input |
| `--note` | the headline finding, in one line |
| `--story` | record against this story instead of the one being worked on. The id has to be in the backlog |

The verdicts are `approve`, `block` and `note`.

The command stamps the review with what was in front of the reviewer — the
story's entry in the backlog at `dor`, the plan's content at `design_review`,
the working tree at `verifier_review` and `code_review`. That is what makes an
approval go stale when the thing it approved changes. The story and the tree
both leave out the `status` and `updated` fields `sdlc` writes into the backlog
itself, and the tree leaves out `.sdlc/`; see
[Why approvals go stale](the-loop.md#why-approvals-go-stale).

A block that stops the gate, recorded `loop.max_review_rounds` times by the same
reviewer since a person last answered for the story, hands the story to that
person as `review_not_converging`, and says so — `handed_over` in `--json`.

That block and the hand-over are recorded together or not at all, as a
[gate failure at its limit](#sdlc-gate-gate-status) is. Where the story cannot
be handed over the command is refused, nothing is recorded and no review
document is left, and the refusal says that the block was not recorded and that
running the same command again is safe. The round that was refused is not a
round: run again, the block is the round it would have been.

> [!NOTE]
> Rounds are kept rather than overwritten, so "what did the architect say last
> time" stays answerable.

## `sdlc staffing`

You will not need these commands unless your project has turned the staffing on.
The staffing is, for one story, which of the plugin's agents the story needs and
the model and effort each is asked for. It is off unless the project sets
[`staffing.enabled`](configuration.md#staffing); while it is off, each of the
four commands below is refused with
[SDLC-E0067](troubleshooting.md#sdlc-e0067) and every agent takes part in every
story. [Agents](agents.md#the-staffing) says how it is chosen.

`sdlc staffing` on its own prints what the four commands are.

> [!NOTE]
> What is recorded and shown is what was asked for. The environment can override
> both a model and an effort, and `sdlc` does not observe what a delegation is
> given, so no output says more than that.

## `sdlc staffing show`

Read the story's staffing, and see who to delegate to at a gate. This is the
command to run when you want to know why an agent is, or is not, taking part.

```console
$ sdlc staffing show
$ sdlc staffing show --gate code_review --json
```

| Flag | What it does |
| --- | --- |
| `--gate` | who to delegate to at this gate, instead of the one the story is at |
| `--story` | for this story instead of the one being worked on |

Prints the models and efforts the project allows, lowest first, the paths that
bring a reviewer back to the code review, then one line for each agent: what it
is asked for, or that it is left out, with the reason, and whether a person
chose it. Every change made since follows, with when and why. A reason is
printed quoted, because a model or a person wrote it.

In `--json`, `agents` has a row for each agent: `needed`, `reason`,
`asked_model`, `asked_effort` and `asked_reason`. `set_by` is `person` on a row
[`sdlc staffing assign`](#sdlc-staffing-assign-agent) recorded, and absent
otherwise; it says which command wrote the row, and nothing about who typed it.
`brought_back_by` is the changed path that has the code review wait for a
reviewer the staffing leaves out. `at_gate` is who to delegate to at the gate:
the agent that does the gate's work and each reviewer the gate waits for, with
what each is asked for. `amendments` is every change, each with `at`, `agent`,
`change`, `reason` and `by`.

With the staffing on and none recorded it fails with
[SDLC-E0068](troubleshooting.md#sdlc-e0068).

## `sdlc staffing set`

Record the story's staffing. The `sdlc:project-manager` agent runs it, once,
when the story has been found ready.

```console
$ sdlc staffing set < staffing.json
$ sdlc staffing set --file /tmp/staffing.json
```

| Flag | What it does |
| --- | --- |
| `--file` | read the staffing from this path instead of standard input |
| `--story` | the story being worked on, which is the only one a staffing is recorded for |

The staffing is a JSON document with one row for each agent the plugin ships:

```json
{"agents": [
  {"agent": "perf", "needed": false, "reason": "nothing here is on a hot path"},
  {"agent": "verifier", "needed": true, "reason": "every story is verified",
   "asked_model": "opus", "asked_effort": "high",
   "asked_reason": "the criteria turn on an ordering that is easy to misread"}
]}
```

An agent the story needs comes with the model and effort asked for and the
reason for them. Nothing else in the document is read. A document that leaves an
agent out, names one twice or gives no reason is refused with
[SDLC-E0069](troubleshooting.md#sdlc-e0069), and so is a second `set`: a
recorded staffing is changed with `sdlc staffing amend`.

Some agents are never left out — the ones that do a gate's work, the verifier,
the code reviewer, and security on a story that is security-sensitive or not yet
said not to be ([SDLC-E0070](troubleshooting.md#sdlc-e0070)). A model or effort
outside the project's lists, or below the agent's lowest, is refused with
[SDLC-E0071](troubleshooting.md#sdlc-e0071), which names what is allowed. What a
person assigned stands: a staffing that disagrees with it is refused with
[SDLC-E0072](troubleshooting.md#sdlc-e0072).

While the staffing is on, `sdlc gate analysis pass` waits for one to be
recorded.

## `sdlc staffing amend AGENT`

Change one agent of the staffing, with the reason. The `sdlc:project-manager`
agent runs it: when the analysis is recorded, and whenever an agent's work fell
short or the work turned out other than expected.

```console
$ sdlc staffing amend perf --left-out --reason "nothing here is on a hot path"
$ sdlc staffing amend researcher --model opus --reason "its first pass missed the callers"
```

| Flag | What it does |
| --- | --- |
| `--needed` | the story needs the agent after all; give `--model` and `--effort` with it |
| `--left-out` | the story does not need the agent |
| `--model` | the model to ask for, one of `staffing.models` |
| `--effort` | the effort to ask for, one of `staffing.efforts` |
| `--reason` | why, from what happened or what the work is. Required |
| `--story` | the story being worked on, which is the only one a staffing is amended for |

What changed, when and why is kept, and `sdlc staffing show` and `sdlc log`
print it.

A gate that has passed stays passed when a reviewer is brought in: the reviewer
is waited for from then on, at the next gate it reviews, and at the commit gate
when it was brought in after the code review passed.

It is held to everything `sdlc staffing set` is. A reviewer whose latest review
is a block at a gate that has not passed is not left out
([SDLC-E0073](troubleshooting.md#sdlc-e0073)): the block is answered by rework,
or by a person.

## `sdlc staffing assign AGENT`

Pin the model or the effort an agent is asked for on a story. This is how a
person says so: they run it in their own terminal, outside Claude Code.
Reach for it when you know better than the project manager which model a story
deserves.

```console
$ sdlc staffing assign security --model opus --effort high --reason "this one touches the installer"
```

| Flag | What it does |
| --- | --- |
| `--model` | the model to ask for, one of `staffing.models` |
| `--effort` | the effort to ask for, one of `staffing.efforts` |
| `--reason` | why. Required |
| `--story` | a story that has been started, instead of the one being worked on |

The agent is then needed at every gate it has a part in, and no staffing or
amendment changes what was assigned. Running it again changes it. Before a
staffing is recorded, or for an agent the staffing leaves out, it takes both
`--model` and `--effort`. It is held to the project's limits like any staffing,
and it cannot take an agent off a story.

> [!WARNING]
> It is refused wherever a Claude Code session id is set
> ([SDLC-E0074](troubleshooting.md#sdlc-e0074)), and the hook refuses it to every
> session ([enforcement](enforcement.md#assignment-is-a-human-decision)). What
> `set_by: person` records is that this command wrote the row.

## `sdlc freeze`

Lock the acceptance tests by content, so the implementer cannot change them to
make them pass. The loop does this for you at the `tests_frozen` gate.

```console
$ sdlc freeze
$ sdlc freeze --json
```

The freeze records the sha256 of every file that `paths.tests` in your configuration calls
a test, asking git which files are in the working tree so that build output and
vendored code cannot be mistaken for one.

It refuses if a freeze already exists
([SDLC-E0022](troubleshooting.md#sdlc-e0022)), because taking a second one over
the top would quietly bless whatever changed in between. It is also refused when
`paths.tests` names nothing to freeze
([SDLC-E0024](troubleshooting.md#sdlc-e0024)), and when a freeze was taken and
has since gone missing without being lifted
([SDLC-E0025](troubleshooting.md#sdlc-e0025)): a person lifts that one with
`sdlc unfreeze --reason`, and then freezes again.

The one exception is `freeze.allow_new_test_files`. With it on, running
`sdlc freeze` again on the story's own freeze adds the test files written since,
provided none of the frozen ones has changed, and lists what it added; `--json`
names them in `added`.

## `sdlc escalate`

Hand the story to a person, and end the iteration. An agent reaches for this
when it needs a decision it should not make itself.

```console
$ sdlc escalate pre_commit_approval --message "the migration has no down step"
```

| Flag | What it does |
| --- | --- |
| `--message` | what the person is being asked to decide. Required |

The type is a word for the kind of question — `pre_commit_approval`,
`spec_unclear`, `loop_stalled` — and the message is the question. It goes on the
story's record, bound to the work as it stands. The story becomes
`awaiting_human`, and the iteration ends, so the session stops rather than
carrying on past the question. `sdlc start` refuses the story until somebody
answers, and `sdlc status` lists it as waiting.

An escalation that is refused — git cannot measure the tree, or something it
writes cannot be written — leaves the record, the backlog and the iteration as
they were: no question is asked, and the command can be run again.

Nothing else starts in that checkout in the meantime either: `sdlc start` picks
up a story already under way before a new one, so it picks the waiting story and
refuses it. That is deliberate. The work waiting for an answer is still in the
tree, and a second story started on top of it would end up in the same commit.
With [`git.worktrees`](configuration.md#git) on, a story waiting in its own
worktree is held there, and `sdlc start` in the main checkout passes it over
and begins the next story in a worktree of its own.

## `sdlc hook EVENT`

You will not run this one. It is what the plugin's hook script calls, reading
the event on standard input. It is documented so that a line in a log makes
sense when you meet it.

## Where to go next

- [Human approval gates](guides/human-approval-gates.md): answering an
  escalation with `sdlc approve`
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md):
  `sdlc stop` and `sdlc start`
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md): `sdlc review add`
  and `sdlc review list` in a rework round
- [When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md): lifting
  the freeze with `sdlc unfreeze`
- [Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md):
  `sdlc log` and `sdlc ack`
- [Tracking what a story costs](guides/tracking-what-a-story-costs.md):
  `sdlc cost`
- [Troubleshooting](troubleshooting.md): every `SDLC-E` code and what to do
  about it
