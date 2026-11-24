# Commands

- [Introduction](#introduction)
  - [Who runs which command](#who-runs-which-command)
  - [What every command shares](#what-every-command-shares)

Commands you run:

- [`sdlc init`](#sdlc-init)
- [`sdlc doctor`](#sdlc-doctor)
  - [The story worktrees check](#the-story-worktrees-check)
- [`sdlc status`](#sdlc-status)
- [`sdlc story list`](#sdlc-story-list)
- [`sdlc start`](#sdlc-start)
  - [What start checks first](#what-start-checks-first)
- [`sdlc stop`](#sdlc-stop)
  - [Landing a story from its worktree](#landing-a-story-from-its-worktree)
- [`sdlc log`](#sdlc-log)
- [`sdlc cost`](#sdlc-cost)
- [`sdlc cost add`](#sdlc-cost-add)
- [`sdlc trace`](#sdlc-trace)
  - [`sdlc trace --all`](#sdlc-trace---all)
  - [Reading the trace file](#reading-the-trace-file)
- [`sdlc version`](#sdlc-version)
- [`sdlc completion`](#sdlc-completion)

Commands only a person runs:

- [`sdlc approve`](#sdlc-approve)
- [`sdlc unfreeze`](#sdlc-unfreeze)
- [`sdlc ack`](#sdlc-ack)
- [`sdlc release`](#sdlc-release)
- [`sdlc remap STORY`](#sdlc-remap-story)
- [`sdlc staffing assign AGENT`](#sdlc-staffing-assign-agent)

Commands the loop runs:

- [`sdlc gate GATE STATUS`](#sdlc-gate-gate-status)
- [`sdlc artifact list`](#sdlc-artifact-list)
- [`sdlc artifact write NAME`](#sdlc-artifact-write-name)
- [`sdlc artifact diff plan`](#sdlc-artifact-diff-plan)
- [`sdlc codemap write`](#sdlc-codemap-write)
- [`sdlc review list`](#sdlc-review-list)
- [`sdlc review add GATE ROLE VERDICT`](#sdlc-review-add-gate-role-verdict)
- [`sdlc finding close GATE ROLE F-N`](#sdlc-finding-close-gate-role-f-n)
- [`sdlc decide ID`](#sdlc-decide-id)
- [`sdlc staffing`](#sdlc-staffing)
- [`sdlc staffing show`](#sdlc-staffing-show)
- [`sdlc staffing set`](#sdlc-staffing-set)
- [`sdlc staffing amend AGENT`](#sdlc-staffing-amend-agent)
- [`sdlc team`](#sdlc-team)
- [`sdlc team failure AGENT`](#sdlc-team-failure-agent)
- [`sdlc freeze`](#sdlc-freeze)
- [`sdlc escalate`](#sdlc-escalate)
- [`sdlc hook EVENT`](#sdlc-hook-event)

- [Where to go next](#where-to-go-next)

## Introduction

Most of the time you will not type an `sdlc` command at all. `/sdlc:next` in
Claude Code runs them for you, one gate at a time. This page is for the moments
in between: you want to see where a story stands, the loop has handed you a
decision, or a command printed a line you want explained.

Every command has one entry, and every entry has the same shape: what the
command is for, how to run it, its flags, what it prints, and when it refuses.
`sdlc <command> --help` gives the short form of the same in your terminal.

The examples come from one project, `billing`. Its backlog holds three
stories: `AUTH-3`, then `AUTH-4`, which waits for it, and `AUTH-5`, which is
not ready yet.

### Who runs which command

The commands fall into three groups, and the page takes them in that order.

**Commands you run.** They set a project up, show where things stand, begin a
story or put it down, and look back at what landed. You will type
[`sdlc init`](#sdlc-init) once per project, [`sdlc doctor`](#sdlc-doctor) when
something is not working, and [`sdlc status`](#sdlc-status) whenever you want
to know where a story has got to.

**Commands only a person runs.** Each of these is a decision the loop must not
make for itself, so an agent is refused all six:

| Command | In Claude Code | You run it when |
| --- | --- | --- |
| [`sdlc approve`](#sdlc-approve) | `/sdlc:approve` | the loop has handed you a story and waits for your answer |
| [`sdlc unfreeze`](#sdlc-unfreeze) | `/sdlc:unfreeze` | a frozen test is wrong |
| [`sdlc ack`](#sdlc-ack) | `/sdlc:ack` | you have read what landed on trunk |
| [`sdlc release`](#sdlc-release) | `/sdlc:release` | a story has to move to another checkout |
| [`sdlc remap`](#sdlc-remap-story) | `/sdlc:remap` | a landed story's change to `CODEMAP.md` did not reach trunk |
| [`sdlc staffing assign`](#sdlc-staffing-assign-agent) | `/sdlc:assign` | you want to choose the model an agent is asked for |

You type the slash command in Claude Code or in Claude Desktop, with the same
arguments, or run the command in a terminal you opened yourself. Where a
slash command answers that it did not run, type the command as a `!` line in
Claude Desktop, such as `!sdlc approve AUTH-3`, or use your own terminal. Run
anywhere else, in a process Claude Code started, each one refuses and changes
nothing ([SDLC-E0079](troubleshooting.md#sdlc-e0079)).
[Where to type your answer](guides/human-approval-gates.md#where-to-type-your-answer)
shows each way.

**Commands the loop runs.** The agents run these as a story moves through its
gates: they record a gate, store a document, add a review. You will read them
in a session far more often than you type them. They are here so that a line
in a transcript makes sense, and for the day you
[do a gate's work by hand](the-agents.md#doing-an-agents-gate-by-hand).

### What every command shares

**`--json` works everywhere.** It prints one line of JSON on standard output
instead of prose, for a script to read. A refusal comes back as JSON too, with
`ok` set to `false` and the fields `error`, `why`, `fix`, `code` and `docs`.

**A refusal says what to do next.** It names what happened, why, the fix, and
a code that has its own entry in [Troubleshooting](troubleshooting.md):

```console
$ sdlc start AUTH-9
sdlc: no story with the id "AUTH-9"

  why  the backlog has AUTH-3, AUTH-4, AUTH-5
  fix  run "sdlc story list" to see the ids you can use

  SDLC-E0009  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0009
```

A refused command exits non-zero and prints the refusal on standard error. A
required flag left off is the one exception to the shape: `sdlc cost add` with
no `--usd` prints `required flag(s) "usd" not set`, with no code.

**A command acts on the story in the checkout you run it in.** By default each
story is worked in a worktree of its own, `.sdlc/worktrees/<ID>`, so a command
that changes a story is run there. Where a command takes `--story`, the flag
names a story instead of using the one being worked on.

## `sdlc init`

Set a repository up for the loop. You run it once per project, from anywhere
inside the repository.

In Claude Code, `/sdlc:init` runs it and shows you what it printed.

```text
sdlc init [flags]
```

| Flag | What it does |
| --- | --- |
| `--force` | restore the default settings over an existing setup, keeping your stories |

```console
$ sdlc init
Set up sdlc in /home/<user>/src/billing

  created  .sdlc/config.json
  created  .sdlc/templates/story.schema.json
  created  user_stories.json
  created  CLAUDE.md
  created  REVIEW.md

Guessed the commands from a Go project.

Next:
  1. sdlc doctor              check the guesses
  2. edit user_stories.json   replace the example with a story of your own
  3. /sdlc:next               type it in Claude Code to work the first story
```

`init` works out what the project is built with and writes five files: the
configuration, with commands that match the stack; the story schema; a backlog
with one example story; the contract, a `## SDLC Contract` section in
`CLAUDE.md`; and `REVIEW.md`, the
[review policy](the-loop.md#the-review-policy) every reviewer reads. It also
writes `.sdlc/.gitignore`, which keeps each story's trace out of git and is not
in the list it prints.

It never overwrites a file you wrote, and it never touches your own
`.gitignore`. A `CLAUDE.md` that is already there has the section appended. A
`REVIEW.md` that is already there is kept, and the line reads
`kept     REVIEW.md`.

Commit what `init` wrote before you start a story, because each story's
worktree is cut from trunk's commits.
[Getting started](getting-started.md#set-up-your-project) walks through the
first run, and
[Adding sdlc to an existing project](guides/adding-to-an-existing-project.md)
covers a repository that already has conventions of its own.

**The stack.** `init` recognises Go (`go.mod`), Rust (`Cargo.toml`),
Node (`package.json`) and Python (`pyproject.toml`, `setup.py` or
`requirements.txt`). It tries them in that order at the root of the
repository, so a Go service with a `package.json` for its front-end tooling is
a Go project. A `package.json` that does not parse still counts as Node, with
no commands filled in. Anything else gets no commands and the common test
directories, for you to fill in.

**The contract.** `init` finds an existing contract only by a
`## SDLC Contract` heading on a line of its own. A `###` heading, or a sentence
that mentions the contract, does not count, and `init` then appends a section
of its own.

**A project with `AGENTS.md`.** Where the project has an `AGENTS.md` and no
`CLAUDE.md`, or its `CLAUDE.md` is a link to `AGENTS.md`, `sdlc init` puts the
section in `AGENTS.md` and makes no `CLAUDE.md`. The line then reads
`created  AGENTS.md (appended to)`.
[A project that keeps AGENTS.md](guides/adding-to-an-existing-project.md#a-project-that-keeps-agentsmd)
has the details.

**Starting again.** Run a second time, `init` refuses rather than lose your
settings ([SDLC-E0003](troubleshooting.md#sdlc-e0003)). `--force` restores the
defaults:

```console
$ sdlc init --force
Set up sdlc in /home/<user>/src/billing

  created  .sdlc/config.json
  created  .sdlc/templates/story.schema.json
  kept     user_stories.json
  kept     CLAUDE.md
  kept     REVIEW.md

Guessed the commands from a Go project.

Next:
  1. sdlc doctor              check the guesses
  2. edit user_stories.json   replace the example with a story of your own
  3. /sdlc:next               type it in Claude Code to work the first story
```

The configuration and the schema are rewritten. Your stories are kept, and so
are three settings: where the backlog is (`backlog.path`), your trunk branch
(`git.trunk_branch`) and a review policy you moved (`reviews.policy`).
`.sdlc/.gitignore` is kept as well, and the contract is appended only if it is
missing. `--force` is refused while a story is being worked on
([SDLC-E0012](troubleshooting.md#sdlc-e0012)).

## `sdlc doctor`

Check the project and say how to fix what is wrong. Run it first whenever
something is not working: it takes a moment, and it runs none of your commands.

In Claude Code, `/sdlc:doctor` runs it and shows you what it printed.

```text
sdlc doctor [flags]
```

`doctor` has no flags of its own.

```console
$ sdlc doctor
sdlc doctor

  ok       git repository       /home/<user>/src/billing
  ok       git command          found on PATH
  ok       configuration        .sdlc/config.json  (Go project)
                                paths.conflict_markers_intended (this checkout's copy; "sdlc stop" reads trunk's): none
  ok       backlog              user_stories.json  (3 stories, one runnable)
  ok       loop state           the iteration, every gate record, the test freeze and the commit acknowledged all read
  ok       review policy        REVIEW.md
  ok       project contract     CLAUDE.md carries the "## SDLC Contract" section
  ok       commands             build, coverage, fmt, fmt_check, fmt_file, lint, smoke, test, test_files
  ok       freeze check         commands.freeze_check is not set, so sdlc freeze runs no check
  ok       sdlc on PATH         /home/<user>/.local/bin/sdlc

Everything checks out.
```

Each line is one check. A `problem` is followed by a `fix:` line that says what
to do, the last line counts the problems, and `doctor` exits non-zero when
there is one, so it works in a script.

| Check | What it looks at |
| --- | --- |
| `git repository` | that you are inside one |
| `git command` | that git is installed, and recent enough for worktrees (2.17 or later) |
| `configuration` | that `.sdlc/config.json` reads, with every setting it does not know, such as a misspelled one, and every value it refuses ([SDLC-E0045](troubleshooting.md#sdlc-e0045)) |
| `backlog` | that the backlog reads, and how many stories could start |
| `loop state` | that every story's record, the freeze and the acknowledged commit read, and which checkout holds each story |
| `review policy` | which file the reviewers read |
| `story worktrees` | worktrees that need your attention; shown only when one does |
| `project contract` | which file carries the `## SDLC Contract` section |
| `project instructions` | whether Claude Code will read `AGENTS.md`; shown only where the contract is there |
| `commands` | that the programs your configured commands start are installed, and that the hook would let each command run |
| `freeze check` | whether `commands.freeze_check` is set |
| `sdlc on PATH` | where your shell finds `sdlc` |

The second line of `configuration` lists the paths in
[`paths.conflict_markers_intended`](configuration.md#pathsconflict_markers_intended),
or says `none`. It is this checkout's copy, and `sdlc stop` reads trunk's.

**What the `commands` check can miss.** `doctor` looks up the first word of
each command, and the first word after an operator such as `&&`, `|` or `;`.
It cannot see the programs a script calls, and it can miss one inside a
compound command. It judges each command as the main conversation would run it
from the project root with no story under way, so a quiet `doctor` is not a
promise that every agent may run every command. It names a refused command by
its key and the rule, and never prints or runs it. The `freeze check` line says
only whether a freeze check is set, never what it holds.

**The review policy.** This is the one check that can say `warning`. A warning
is not a problem: it changes neither the last line nor the exit status.

```text
  warning  review policy        reviews.policy names docs/polcy.md, and there is no file there
                                fix: create docs/polcy.md by hand, on trunk between stories, or take reviews.policy out of .sdlc/config.json
```

It warns about a file [`reviews.policy`](configuration.md#reviewspolicy) names
that is not there, or that is a directory or a link out of the repository. With
worktrees on it also warns about a policy trunk does not hold, and about one
that reached trunk after the story's worktree was cut. With `reviews.policy`
not set and no `REVIEW.md`, the check is `ok` and says `no review policy`.

**The contract.** `doctor` finds the contract the way `init` does: only by a
`## SDLC Contract` heading on a line of its own. With the section in
`AGENTS.md`, the check says so and a `project instructions` check follows it:

```text
  ok       project contract     AGENTS.md carries the "## SDLC Contract" section and there is no CLAUDE.md, so Claude Code 2.1.277 or later reads it
  ok       project instructions Project instructions is not set in your Claude Code user settings, so Claude Code's default, claude-md-or-agents-md, reads AGENTS.md; managed settings were not checked
```

It is a problem where a `CLAUDE.md`, or a `CLAUDE.local.md` at the root, is
read in its place and does not import it with an `@AGENTS.md` line, and where
both files carry the section, since the two can disagree. The
`project instructions` check reads the Project instructions setting from your
Claude Code user settings and never writes it. It is a problem where the
setting is `claude-md` or `managed-only`, which keep Claude Code from reading
`AGENTS.md`.

In a story's worktree, `doctor` reads the backlog and the records from the main
checkout, and says so when the repository has none
([SDLC-E0052](troubleshooting.md#sdlc-e0052)).
[Check with `sdlc doctor`](guides/why-was-i-refused.md#check-with-sdlc-doctor)
shows how to use it when a command was refused.

### The story worktrees check

A `story worktrees` line appears only when there is something for you to do.
Here a story was released, and its worktree is still there:

```text
  problem  story worktrees      AUTH-4's worktree, .sdlc/worktrees/AUTH-4, has changes nobody committed, and no checkout holds AUTH-4
                                fix: the changes nobody committed may be somebody's work: a person keeps what is wanted, committed on story/AUTH-4 or copied elsewhere, before removing anything
```

The check says what is true of each worktree and what to do about it, in the
order you do it:

| What it found | What you do |
| --- | --- |
| Trunk has no `.sdlc/config.json` committed, so no story can start ([SDLC-E0053](troubleshooting.md#sdlc-e0053)) | Commit it, and the file that holds the contract, to trunk. |
| Trunk tracks files under `.sdlc/state`, so no story can land ([SDLC-E0055](troubleshooting.md#sdlc-e0055)) | Run `git rm -r --cached .sdlc/state` in the main checkout, add `/.sdlc/state/` to `.gitignore`, and commit both. |
| A worktree's directory is gone | Run `git worktree prune` in the main checkout. |
| A worktree no checkout holds has changes nobody committed | Keep what is wanted, committed on `story/<ID>` or copied elsewhere, before you remove anything. |
| `story/<ID>` holds commits trunk does not have | With trunk checked out, run `git merge --ff-only story/<ID>` in the main checkout. If git cannot fast-forward, rebase `story/<ID>` on trunk in its worktree and run the tests first. |
| A worktree is on another branch, or on no branch, with commits trunk does not have | Look at them with `git -C .sdlc/worktrees/<ID> log --oneline <trunk>..HEAD`. Keep what is wanted on a branch before you merge or remove anything. |
| A worktree has nothing trunk does not | Run `git worktree remove .sdlc/worktrees/<ID>`, then `git branch -d story/<ID>`, in the main checkout. |

`doctor` never suggests `git branch -D` or `--force`, and it never counts the
loop's own files as somebody's changes.
[Removing a worktree](the-loop.md#removing-a-worktree) explains what is safe to
delete.

## `sdlc status`

See what the loop is working on and where it has got to. `status` changes
nothing, so it is safe to run at any point.

```text
sdlc status [flags]
```

`status` has no flags of its own.

With no story under way, it shows the backlog and what would start next:

```console
$ sdlc status
No iteration running.

  backlog  1 todo · 2 ready
  next up  AUTH-3  Reject expired sessions

A person runs `sdlc start`, or types /sdlc:next in Claude Code, to begin.
```

In the worktree of a story being worked on, it shows the story:

```console
$ sdlc status
AUTH-3  in progress  Reject expired sessions

  dor              pass  criteria are testable
  analysis         pass  a session token crosses a trust boundary
  tests_frozen     pass  2 criteria, 1 failing test
  plan             pass  2 steps

  tests  1 test file frozen at 2026-11-24T09:47:22Z
  cost   $0.00 of $60.00 (0%)

  staffing  recorded: left out perf

  next   design_review
  pause  the commit will not pause: the story's risk tier is low, which pre_commit_pause_tiers does not pause; pre_commit_pause_paths lists no path
  finding    design_review architect F-1 open, important: the plan: AC-2 has no step
  finding    design_review architect F-2 open, nit, internal/session/session.go:4: the name says nothing
  to close   sdlc finding close design_review architect F-1 --fixed once it is fixed, or --declined --reason "<why>"

  backlog  1 todo · 1 ready · 1 in progress
  held     AUTH-3  in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3", this checkout

Decisions: 2 not yet acknowledged (2 in AUTH-3 (.sdlc/stories/AUTH-3/DECISIONS.md), under way)

A person types /sdlc:next in Claude Code to carry on, or runs `sdlc stop` to put it down.
```

| Line | What it says |
| --- | --- |
| the first line | the story, and `in progress` or `finished`. A finished story has every gate behind it and wants `sdlc stop`. |
| the gates | each gate recorded so far, its outcome and its note. A gate that was reopened reads `pending`, with why. |
| `tests` | whether the tests are frozen, how many files, and since when |
| `cost` | what the story has spent of its budget |
| `staffing` | whether a staffing is recorded, and which agents it leaves out |
| `next` | the gate to work now |
| `pause` | whether the commit will wait for your approval, and why |
| `finding`, `to close` | each open finding of the design and code reviews, and how to close the first |
| `declined`, `referred` | each finding that was declined, and each one referred to you, with its reason |
| `backlog` | how many stories are in each status |
| `waiting` | a story handed to a person, with the question it asks |
| `held` | which checkout holds each story |
| `landing`, `to land` | the story landing now, and each stop waiting to land after it |
| `next up` | the story `sdlc start` would pick |
| `Decisions:` | how many decisions the loop made that you have not yet acknowledged, and where to read them |
| the last line | what to do next |

A story handed to you shows on a `waiting` line, and the last line then tells
you how to answer:

```text
  waiting  AUTH-3  pre_commit_approval: the migration has no down step
```

While a story lands, `status` in every checkout names it, and every stop that
waits to land after it:

```console
$ sdlc status
No iteration running.

  backlog  1 ready · 2 in progress
  held     AUTH-3  in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3"
  held     AUTH-4  in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-4"
  landing  AUTH-3  sdlc stop, pid 13615, since 2026-11-24T09:56:19Z
  to land  AUTH-4  sdlc stop, pid 13615, since 2026-11-24T09:56:19Z, waits for AUTH-3
  next up  AUTH-5  Log failed sign-ins

A person runs `sdlc start`, or types /sdlc:next in Claude Code, to begin.
```

A `landing` line that begins `none:` means no story is landing: either a stop
ended without finishing, and the next one takes over by itself, or
[`sdlc remap`](#sdlc-remap-story) is at work.

**The `pause` line.** Until the commit gate has passed, `status` in the
story's own checkout says whether the commit will wait for your approval. It
gives the story's risk tier, and each file the change touches with the
pattern in
[`pre_commit_pause_paths`](configuration.md#human_gates) it matched, or says
that none does. Here a low-tier story has touched a path the project pauses
on:

```text
  pause  the commit will pause for a person's approval: the story's risk tier is low, which pre_commit_pause_tiers does not pause; "internal/session/rotate.go" matches "internal/session/**"
```

Once you have approved the work as it stands, the line says the commit will
not pause again. Where `status` cannot read something it needs to decide, the
line says what, and that the commit will pause.

**The `Decisions:` line.** The loop records each
[implementation decision](the-loop.md#two-kinds-of-decision) it makes with
[`sdlc decide`](#sdlc-decide-id). `status` counts the ones you have not yet
acknowledged: every decision of a story under way, and those of a committed
story until your [`sdlc ack`](#sdlc-ack) covers its commit. The line names up
to three stories under way, each with its file, and points you to `sdlc log`
for the committed ones:

```text
Decisions: 2 not yet acknowledged (2 in stories committed since your last ack, listed by sdlc log)
```

With nothing to acknowledge, there is no such line. A dropped story's
decisions are not counted, since no `ack` could ever cover them. They stay in
its `DECISIONS.md`. Where a story's record cannot be read, the line says
`at least`, and a line on standard error names the story.

**Other lines you may see.** Where the [`team`](configuration.md#team) section
turns an agent off, or stops security, the verifier or the code reviewer from
blocking, `status` says so under `next`. An active story that has gone from the
backlog reads `(not in the backlog)`: put it back, or run `sdlc stop`. Where
[`budget.gate_hours`](configuration.md#budget) gives a gate a number of hours
and the agents have spent longer at it, `status` prints one line about it on
standard error.

**The consolidation reminder.** Once `consolidate.nudge_after` stories (3
unless you set it) have finished since the last `/sdlc:consolidate`, `status`
ends with one more line, which says how many and that you can run the skill:

```text
3 stories have finished since the last consolidation on 2026-10-20: a person can run /sdlc:consolidate to see what they have in common.
```

Below `nudge_after` stories, and with the setting at `0`, `status` prints no
such line and `--json` is without the field.
[The reminder](guides/consolidating-what-the-retros-say.md#the-reminder) says
what counts as finished since.

**With `--json`.** These are the fields a script reads:

| Field | What it holds |
| --- | --- |
| `active`, `title` | the story being worked on. Absent when none is. |
| `gates` | each recorded gate's outcome |
| `next_gate` | the gate to work now. Absent when every gate has passed, or no story is active. |
| `next` | the story `sdlc start` would pick. Present only when nothing is active. |
| `backlog` | the count of stories in each status |
| `freeze`, `cost`, `staffing` | the freeze and whether it is `intact`, the spend and the budget, and `none` or `recorded` |
| `findings`, `referred` | every finding of the active story with its closings, and the referred ones again |
| `waiting` | every story handed to a person and not yet answered, with its question |
| `held` | every story a checkout holds, with the `checkout`, and `here` for the one you ran it in |
| `disabled` | every agent the `team` section turns off |
| `pre_commit_pause` | whether the active story's commit will wait for you: `will_pause`; `approved`, which is `true` once you have approved the work as it stands; the story's `tier`, and whether `tier_pauses`; `paths`, each `file` the change touches with the `glob` it matched; and `reasons`, the clauses the `pause` line prints. Absent once the commit gate has passed, and in a checkout that does not work the story. |
| `commands` | the result the implementation gate recorded: `current`, which is `true` while it is for the files and the commands as they are now, the `tree_hash` it ran on, and `results`, each command's `name`, `exit` and `duration_ms`. Absent until the gate has recorded one. |
| `decisions_unread` | how many decisions you have not yet acknowledged. Always present, `0` included. |
| `decisions_unknown` | every story whose record could not be read, so that its decisions are not counted. Absent where every record reads. |
| `landing` | the stop that is landing a story: its `story`, `command`, `since` and `pid`, and `gone` where that process is no longer running |
| `waiting_to_land` | every stop waiting to land, with the story it waits `for` |
| `consolidate` | `finished`, the count of stories since the last consolidation, and `last`, its date, which is `null` where there has been none |
| `not_in_backlog` | `true` when the active story is no longer in the backlog |

[Checking on a story](getting-started.md#checking-on-a-story) shows `status` in
a first run.

## `sdlc story list`

See the whole backlog at a glance, and what is holding each story back.

```text
sdlc story list [flags]
```

`story list` has no flags of its own.

```console
$ sdlc story list
> AUTH-3  ready   1  Reject expired sessions
  AUTH-4  ready   2  Rotate session tokens  (waiting on AUTH-3)
  AUTH-5  todo    3  Log failed sign-ins

> is what `sdlc start` would pick.
```

Each line is a story's id, status, priority and title. A story that waits for
another names it, and says so when that one is dropped or missing, because
then it never will be done.

A story that a checkout holds ends with where:

| Ends with | What it means |
| --- | --- |
| `(held in "<checkout>")` | another checkout holds it |
| `(held in "<checkout>", this checkout)` | the checkout you ran it in holds it |
| `(held in "<checkout>", gone: see /sdlc:release ...)` | the checkout that holds it no longer exists ([SDLC-E0051](troubleshooting.md#sdlc-e0051)) |
| `(held by none: more than one checkout names it ...)` | several checkouts name it, and none holds it until you run `sdlc stop` in all but one |

When nothing can start, the mark is gone and the last lines say why, and what
to do about it:

```console
$ sdlc story list
  AUTH-3  in_progress   1  Reject expired sessions  (held in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3")
  AUTH-4  ready         2  Rotate session tokens  (waiting on AUTH-3)
  AUTH-5  todo          3  Log failed sign-ins

Nothing to start: of 3 stories, 0 done and 0 blocked, and 1 waiting on a dependency that is not done; AUTH-3 is held by another checkout of this repository; a bare start takes only a story marked ready, and it passed over 1 todo story: "AUTH-5".
To go on, mark a todo story ready once its criteria are settled, or name one with "sdlc start <ID>".
```

With `--json`, each story in `stories` has `next` where it is the one that
would start, `blocked_by` where it waits for others, and `held_by` where a
checkout holds it. [Choose what runs next](guides/writing-stories.md#choose-what-runs-next)
explains statuses, priorities and dependencies.

## `sdlc start`

Begin an iteration on the next story, or on the one you name. `/sdlc:next`
runs this for you, so you will rarely type it yourself.

```text
sdlc start [STORY] [flags]
```

`start` has no flags of its own.

```console
$ sdlc start
Started AUTH-3  Reject expired sessions

Worktree: "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3"
Branch: story/AUTH-3

  no gates recorded yet

Next: a person opens Claude Code in the worktree above and types /sdlc:next there to work the story; /sdlc:next typed in a new session in this checkout begins the next story instead.
```

**Which story it picks.** With no story named, `start` first picks up a story
already under way. Otherwise it takes the next `ready` story whose
`depends_on` stories are done, by priority and then by id. It takes only
`ready` stories and never a `todo` one: with only `todo` stories left it
refuses, says how many it passed over, and names up to five. Mark one `ready`,
or name it.

Naming a story chooses it over the priority order, and nothing else. A story
that is dropped, blocked or done, or that waits for a story that is not done,
is refused as it would have been passed over:

```console
$ sdlc start AUTH-4
sdlc: "AUTH-4" waits on AUTH-3

  why  a story starts once every story in its depends_on is done
  fix  finish AUTH-3 first, or take it out of AUTH-4's depends_on

  SDLC-E0010  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0010
```

**Where the story is worked.** A new story begins in a worktree of its own,
`.sdlc/worktrees/<ID>`, on a new branch `story/<ID>` cut from trunk. `start`
prints both, and your own checkout is left as it was. Open Claude Code in the
worktree and type `/sdlc:next` there.
[`git.worktrees`](configuration.md#git) turns this off, and the story is then
worked in the checkout you ran `start` in.

**Picking a story up again.** Run on a story that is already under way,
`start` prints `Resumed` in place of `Started`, lists the gates recorded so
far, and changes nothing about the story. From the story's worktree its last
line reads:

```text
Next: a person types /sdlc:next in Claude Code, opened in this checkout or in the worktree above, to work the story there.
```

**Sessions.** Run in Claude Code, `start` notes the session it ran in. That
session and its agents are the ones held to the story, and any other session
is refused the commands that change it
([SDLC-E0048](troubleshooting.md#sdlc-e0048)). Starting the story again in
another session moves it there.

**More than one checkout.** The checkout that starts a story holds it until
the story is finished or you run [`sdlc release`](#sdlc-release). A bare
`start` passes over what another checkout holds. Naming a story that another
checkout holds is refused, except a story in its own worktree under
`.sdlc/worktrees/`: named from anywhere, it is picked up there.
[More than one checkout](the-loop.md#more-than-one-checkout) explains who
holds what.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| no story has that id | [SDLC-E0009](troubleshooting.md#sdlc-e0009) |
| nothing can start, or the story you named cannot | [SDLC-E0010](troubleshooting.md#sdlc-e0010) |
| an iteration is already running on another story in this checkout | [SDLC-E0012](troubleshooting.md#sdlc-e0012) |
| every gate on the story has passed, and it waits to land | [SDLC-E0033](troubleshooting.md#sdlc-e0033) |
| the story waits for a person's answer | [SDLC-E0036](troubleshooting.md#sdlc-e0036) |
| another checkout holds the story, or the checkout that held it is gone | [SDLC-E0050](troubleshooting.md#sdlc-e0050), [SDLC-E0051](troubleshooting.md#sdlc-e0051) |

With `--json`, `start` prints `story`, `title`, `resume`, `next_gate` (the
first gate that has not passed) and `session`, and for a story in a worktree
`worktree`, `branch` and `main`, the main checkout's path.
[Stopping and resuming a story](guides/stopping-and-resuming-a-story.md) covers
picking work back up.

### What start checks first

Before it begins a new story, and not one it is picking up again, `start`
checks that trunk is somewhere to start from:

| It checks that | Otherwise |
| --- | --- |
| trunk is not behind `origin`, when `git.remote` is on | [SDLC-E0040](troubleshooting.md#sdlc-e0040) |
| the worktree can be made | [SDLC-E0053](troubleshooting.md#sdlc-e0053) |
| `commands.smoke` passes in the new worktree | [SDLC-E0041](troubleshooting.md#sdlc-e0041) |

A start that is refused takes its worktree and branch away again, or says what
it could not remove.

With worktrees turned off, it checks instead that HEAD is on trunk
([SDLC-E0038](troubleshooting.md#sdlc-e0038)), that nothing is uncommitted
outside `.sdlc/` and the backlog
([SDLC-E0039](troubleshooting.md#sdlc-e0039)), that trunk is not behind
`origin`, and that `commands.smoke` passes.

## `sdlc stop`

End the current iteration. For a story that is not finished, this puts it down
for now and loses nothing. For a finished story it is the last step: run in the
story's worktree, `stop` lands it on trunk.

```text
sdlc stop [flags]
```

`stop` has no flags of its own.

```console
$ sdlc stop
Stopped the iteration on AUTH-3. Nothing was recorded; `sdlc start` picks it up again.
```

Stopping does not undo anything. The gates already recorded stay recorded, the
story stays in progress, its tests stay frozen, and starting again resumes it.
With no iteration running, `stop` says `No iteration was running.` and exits 0.

**Who may stop.** You may, at any time, in your own terminal. The session
working a story is refused `sdlc stop` while the story still has a gate to
pass, so that an agent cannot walk away from one. Any other Claude Code session
is refused too, and changes nothing
([SDLC-E0048](troubleshooting.md#sdlc-e0048)).

**The consolidation reminder.** Where the stop ended an iteration, and enough
stories have finished since the last `/sdlc:consolidate`, `stop` ends with the
same line `sdlc status` does, at the same threshold, counting the story it has
just finished.

That line is text only: `stop --json` is unchanged, with no field for it, and
the line is never on standard error.

With `--json`, `stop` prints `ok`, `story`, `was_active` and `done`, which is
`true` for a finished story.
[Put a story down](guides/stopping-and-resuming-a-story.md#put-a-story-down)
explains when to stop and what a stopped story keeps.

### Landing a story from its worktree

In a story's own worktree, with every gate passed, `stop` lands the story: it
moves trunk to the story's commits by fast-forward, and the story is done.

```console
$ sdlc stop
main moved from 27367e1d65c8 to 8edf4f0e2a43 by fast-forward; the files in "/home/<user>/src/billing", which has main checked out, moved with it.
Landed on the result sdlc recorded at Gate 5 for tree 571ae232889d: commands.build, commands.test, commands.lint, commands.fmt_check, commands.coverage passed on it, so none ran again.
The backlog and AUTH-3's record are changed in the main checkout and not committed.
Ended the iteration on AUTH-3. Every gate passed, so the story is done and `sdlc start` moves on to the next one.
CODEMAP.md on main is up to date at 646584db7109, in a commit of its own after the story's (adds "internal/session", replaces "internal/billing").
Removed .sdlc/worktrees/AUTH-3, with the files git ignored in it, and story/AUTH-3.
```

| Line | What it tells you |
| --- | --- |
| `main moved from ...` | Trunk now holds the story's commits. Where no checkout has trunk checked out, it ends `so only the branch moved`. |
| `Landed on the result ...` | Your commands were not run again, because the implementation gate already ran them on exactly these files. Where the landing ran them itself, there is no such line. |
| `The backlog and ...` | The story's status and record changed in the main checkout. They are yours to commit. |
| `Ended the iteration ...` | The story is done. |
| `CODEMAP.md on main ...` | The sections the bookkeeper stored are on trunk, in a commit of their own named `docs: map <ID>`. A story that stored none has no such line. |
| `Removed ...` | The worktree and the branch are gone. A worktree with changes nobody committed is kept, and the line says so and names the files. |

The same holds with no iteration running, for a finished story the worktree
holds, such as one you answered for after an escalation.
[Done when it lands](the-loop.md#done-when-it-lands) lists what `stop` checks
before it moves trunk.

**When the commands run again.** The landing runs your configured commands
itself where the files that would land are not the ones the implementation
gate ran them on, or where trunk's commands or thresholds differ from the
ones it ran ([At landing](configuration.md#at-landing)). It first says why,
on a line of standard error that begins
`sdlc: the configured commands run again because`, and then prints a
`running` line for each command.

**When trunk has moved on.** `stop` puts the story's commits onto the newer
trunk and runs your configured commands on the result. A line that begins
`Replayed` then names the commit the story was moved from and the trunk
commit it now sits on.

> [!WARNING]
> Putting a story onto a newer trunk makes its commits again. That runs the
> repository's git filters with the privileges of whoever runs the stop, and
> the new commits are not signed.

**When another story is landing.** `stop` waits its turn, says on standard
error which story it waits for, and `sdlc status` in any checkout shows both.
After eight minutes it gives up
([SDLC-E0006](troubleshooting.md#sdlc-e0006)). A stop that died part-way is
taken over after two minutes, and you remove nothing by hand.

**When the map does not apply.** Where the story's sections cannot be put into
trunk's `CODEMAP.md`, the story has still landed and `stop` still exits 0. It
prints [SDLC-E0076](troubleshooting.md#sdlc-e0076) on standard error as a
warning, and every `sdlc` command reminds you until you have run
[`sdlc remap`](#sdlc-remap-story).

**When it refuses.** Every refusal leaves trunk where it was, and ends
`Nothing landed`.

| It refuses when | Code |
| --- | --- |
| the story's commits do not apply onto a trunk that moved | [SDLC-E0075](troubleshooting.md#sdlc-e0075) |
| trunk was rewritten, or moved while the landing ran | [SDLC-E0054](troubleshooting.md#sdlc-e0054) |
| another stop is landing this story, or another story was still landing after eight minutes | [SDLC-E0006](troubleshooting.md#sdlc-e0006) |
| the worktree, its commits or a configured command is not fit to land | [SDLC-E0055](troubleshooting.md#sdlc-e0055) |
| the checkout that has trunk checked out holds changes the fast-forward would write over | [SDLC-E0056](troubleshooting.md#sdlc-e0056) |
| the change is not the one the reviewers saw | [SDLC-E0029](troubleshooting.md#sdlc-e0029) |
| the story's commits add the markers of an unresolved conflict | [SDLC-E0080](troubleshooting.md#sdlc-e0080) |

`stop` in a story's worktree always lands a finished story.
[Keep a finished story off trunk](guides/stopping-and-resuming-a-story.md#keep-a-finished-story-off-trunk)
says what to do when you are not ready for that.

With `--json` the lines above go to standard error. Standard output also has
`codemap_commit` where the map was committed, and `codemap_kept` set to `true`
where it could not be.

## `sdlc log`

Look back over the stories committed to trunk, and what each one went through.
Reach for it when you want to review what the loop landed while you were away.

```text
sdlc log [flags]
```

| Flag | What it does |
| --- | --- |
| `--since` | list the stories committed after this commit (a hash, a tag, or a name like `HEAD~20`) instead of after the last one acknowledged |
| `--all` | list every committed story, whatever has been acknowledged. Not together with `--since`. |

The log has one entry for each story whose commit gate has passed, oldest
first. It changes nothing.

```console
$ sdlc log
27367e1d65c8..8edf4f0e2a43  AUTH-3  Reject expired sessions
  rounds     tests_frozen 1 · design_review 2 · verifier_review 1 · code_review 2
  blocks     1: design_review architect (round 1)
  counts     design_review architect (round 1): 1 important, 1 nits
  counts     code_review code-reviewer (round 1): 2 nits
  finding    design_review architect F-1 fixed, important: the plan: AC-2 has no step
  finding    design_review architect F-2 declined, nit, internal/session/session.go:4: the name says nothing; reason: the name is the domain's word
  finding    code_review code-reviewer F-1 referred, nit, docs/elsewhere.md: a stale sentence on another page; reason: another page, and the person should see it
  finding    code_review code-reviewer F-2 fixed, nit, internal/session/session.go:4: the comment restates the code
  reopened   design_review, implementation, verification reopened: plan was recorded fail
  reopened   commit, retro reopened: code_review was recorded fail
  failed analysis researcher: "it returned nothing"
  failed analysis researcher: "it returned nothing"
  no pause   the commit gate passed with no pause for a person: the story's risk tier is low, which pre_commit_pause_tiers does not pause; pre_commit_pause_paths lists no path
  decided    analysis by researcher: "Is a token that expires this very second expired?" -- chose "expired", because "the check runs after the instant it names"
  decided    plan by implementer: "Where does the expiry check live?" -- chose "internal/session", because "the session store is never read there"
  escalated  pre_commit_approval: the migration has no down step (answered)
  staffing   project-manager: asked for sonnet at medium -- "the staffing is looked at again after the analysis"; "a short story to read"
  staffing   researcher: asked for opus at medium -- "every story is analysed"; "its first pass missed the callers"
  staffing   sdet: asked for sonnet at medium -- "every story has frozen tests"; "two criteria, one handler"
  staffing   implementer: asked for sonnet at medium -- "every story is implemented"; "a small change in one package"
  staffing   verifier: asked for opus at high -- "every story is verified"; "the criteria turn on an ordering that is easy to misread"
  staffing   bookkeeper: asked for haiku at low -- "every story has a retro"; "a short retro"
  staffing   architect: asked for sonnet at medium -- "the plan touches the session middleware"; "one boundary to check"
  staffing   security: asked for opus at high, a person chose -- "the story is about authentication"; "this one touches the installer"
  staffing   red-team: asked for sonnet at medium -- "the story is about authentication"; "a short plan"
  staffing   perf: left out -- "nothing here is on a hot path"
  staffing   human-advocate: asked for sonnet at low -- "the 401 response is what a user sees"; "one response to read"
  staffing   code-reviewer: asked for sonnet at medium -- "every story's diff is reviewed"; "a small diff"
  staffing   test-reviewer: asked for sonnet at medium -- "the tests are read before they are frozen"; "one test file"
  staffing   amended 2026-11-24T09:47:21Z  perf: needed -> left out -- "nothing here is on a hot path" (staffing amend)
  staffing   amended 2026-11-24T09:47:21Z  researcher: sonnet at medium -> opus at medium -- "its first pass missed the callers" (staffing amend)
  staffing   amended 2026-11-24T09:47:21Z  security: sonnet at high -> opus at high -- "this one touches the installer" (person)
  staffing   2026-11-24T09:47:21Z  staffing_set: 13 agents needed, 0 left out; staffing changes on the record before this: 0
  staffing   2026-11-24T09:47:21Z  staffing_amend: perf: needed -> left out
  staffing   2026-11-24T09:47:21Z  staffing_amend: researcher: sonnet at medium -> opus at medium
  staffing   2026-11-24T09:47:21Z  staffing_assign: security: sonnet at high -> opus at high
  spent      $32.84
```

| Line | What it says |
| --- | --- |
| the first line | the commits the story landed in, its id and its title |
| `rounds`, `blocks` | how many rounds each gate's reviews took, and every block |
| `counts` | the important findings and nits each review counted, one line a round |
| `finding` | each finding the reviews listed, how it was closed, and the reason where it was declined or referred |
| `reopened` | gates that went back to pending, and why |
| `failed` | each run of an agent that failed |
| `no pause` | that the story was committed without waiting for your approval, and why: its tier, and that no file matched `pre_commit_pause_paths`. Where you approved the work after that, as a landing can ask, the line says so and when. |
| `decided` | each decision the loop made with [`sdlc decide`](#sdlc-decide-id): the gate it was made at, who made it, the question, what was chosen and why. The story's `DECISIONS.md` has the options that were weighed. |
| `escalated` | each question handed to a person, and whether it was answered |
| `staffing` | what each agent was asked for or why it was left out, then each change to the staffing, with when |
| `spent` | what the story cost |

A freeze you lifted with `sdlc unfreeze` has a line of its own as well. In a
repository with more than one checkout, a story that is not finished names the
checkout that holds it on a `held` line.

**Where the log starts.** It starts after the last commit you acknowledged
with [`sdlc ack`](#sdlc-ack). With nothing acknowledged, it lists every story.
Once you are up to date, it says so:

```console
$ sdlc log
No story has been committed since 646584db7109, the last commit acknowledged. `sdlc log --all` lists every one.
```

**The range.** The first line's range is what the loop saw the story land.
For a story that landed from its worktree, it is the commits that reached
trunk. A range found from commit messages says it was inferred, and one that
cannot be found reads `unknown`: nothing is guessed.

> [!WARNING]
> A range is a list to read, not a list to revert. It is what the loop saw,
> not proof of which commits are the story's.
> [Ranges to read with care](guides/reviewing-what-landed-on-trunk.md#ranges-to-read-with-care)
> says when to look closer.

**When it refuses.** `--all` together with `--since`, or a `--since` that git
cannot read, is refused ([SDLC-E0034](troubleshooting.md#sdlc-e0034)). So is an
acknowledgement that names no commit git has
([SDLC-E0047](troubleshooting.md#sdlc-e0047)), which running `sdlc ack` again
puts right.

With `--json`, `since_source` says where the log started (`acknowledged`,
`flag`, `all` or `none`), and each entry in `entries` has `story`, `title`,
`committed_at`, `commit_base`, `commit`, `range_source` (`record`, `message` or
`unknown`), `rounds`, `blocks`, `review_counts`, `findings`, `reopened`,
`escalations`, `failures`, `landed_without_pause`, `decisions`, `staffing`,
`staffing_events` and `spent_usd`.

[Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md)
walks through reading the log, and `/sdlc:trunk-review` goes through it with
the assistant.

## `sdlc cost`

See what a story has cost so far, against what it was expected to.

```text
sdlc cost [flags]
```

| Flag | What it does |
| --- | --- |
| `--story` | the story to report on, when it is not the one being worked on |

```console
$ sdlc cost
AUTH-3  $32.84 spent of $60.00 (54.7%)
```

The budget is [`budget.per_story_usd`](configuration.md#budget) in your
configuration. With `--json`, `cost` prints `story`, `spent_usd`, `budget_usd`
and `fraction`. With no story being worked on and none named, it is refused
([SDLC-E0011](troubleshooting.md#sdlc-e0011)).

[Tracking what a story costs](guides/tracking-what-a-story-costs.md) covers
recording, reading back and budgeting.

## `sdlc cost add`

Record one amount against a story. `sdlc` does not measure money itself:
whoever runs the session reports what it cost.

```text
sdlc cost add [flags]
```

| Flag | What it does |
| --- | --- |
| `--usd` | the amount spent, in US dollars. Required. |
| `--note` | what it was spent on |
| `--story` | the story it was spent on, when no iteration is running on it |

```console
$ sdlc cost add --usd 1.42 --note "gate 4, five reviewers"
AUTH-3  $1.42 spent of $60.00 (2.4%)
```

Run it in the checkout that holds the story. For a story in its own worktree
that is the worktree: from the main checkout it is refused
([SDLC-E0050](troubleshooting.md#sdlc-e0050)).

**Alerts.** As the spend crosses each fraction in `budget.alert_fractions`,
`cost add` says so once, on standard error, on the entry that crosses it:

```console
$ sdlc cost add --usd 30 --note "gate 5, the implementer"
sdlc: 50% of the $60.00 budget for this story is spent ($32.84)
AUTH-3  $32.84 spent of $60.00 (54.7%)
```

Nothing here blocks. The entry that uses up the budget says so and names
`sdlc stop`, and the story carries on unless you stop it.
[Nothing blocks](guides/tracking-what-a-story-costs.md#nothing-blocks) says
why.

With `--json`, it prints `story`, `added_usd`, `spent_usd`, `budget_usd` and
`fraction`, and an `alerts` array holding each alert this entry set off. The
alerts still go to standard error as well.

**When it refuses.** An empty, negative or non-numeric `--usd` is refused
rather than recorded as zero ([SDLC-E0034](troubleshooting.md#sdlc-e0034)).
With no story being worked on and none named, there is nothing to add to
([SDLC-E0011](troubleshooting.md#sdlc-e0011)).
[Record the cost from a script](guides/tracking-what-a-story-costs.md#record-the-cost-from-a-script)
shows how a headless run reports its cost.

## `sdlc trace`

See where a story's time and tokens went: how long each gate took, what each
kind of agent used, and which commands ran. It reads what was measured while
the story ran, and writes nothing.

```text
sdlc trace [<ID>] [--all] [flags]
```

| Flag | What it does |
| --- | --- |
| `--all` | every story with a record, side by side, with the median of each column |

You run it as `sdlc trace <ID>` for one story or `sdlc trace --all` for every
story, and never both.

The report for one story has three parts. The first is the story's time, by
the gate it was working towards:

```console
$ sdlc trace AUTH-3
AUTH-3  Reject expired sessions

time, by the gate the story was working towards:
  gate             wall         agents       waiting on a person  between iterations  no agent and no person accounts for
  dor              4m           0s           0s                   0s                  4m
  analysis         24m          20m          0s                   0s                  4m
  tests_frozen     33m          28m          0s                   0s                  5m
  plan             20m          15m          0s                   0s                  5m
  design_review    32m          23m          0s                   0s                  9m
  implementation   48m          44m          0s                   0s                  4m
  verification     25m          22m          0s                   0s                  3m
  verifier_review  1m           0s           0s                   0s                  1m
  code_review      18m          14m          0s                   0s                  4m
  commit           49m          0s           42m                  4m                  3m
  retro            10m          6m           0s                   0s                  4m
  in all           4h25m        2h52m        42m                  4m                  47m
```

| Column | What it counts |
| --- | --- |
| `wall` | the time the story spent working towards that gate. A gate that was reopened takes the time back. |
| `agents` | the time agents were running. Two agents at once count once. |
| `waiting on a person` | the time a question to you stood open |
| `between iterations` | the time between one iteration ending and the next beginning |
| the last column | what is left of the wall time once the other three are taken off |

The second part has a block for each kind of agent that ran. This is the
implementer's, the fifth of ten in this report:

```text
  sdlc:implementer: 3 runs, 1h06m, 68 responses, the largest context 88600 tokens, read from the cache 87%
    tokens: m-sonnet 16800 in, 549000 read from the cache, 64000 written to it, 22600 out
    tools: Bash 25; Edit 12; Grep 4; Read 30; Write 1
    asked for: sonnet at medium; ran on m-sonnet at medium
    handed back: 3 done, 0 blocked
```

`asked for` is what the story's staffing asked for, and what follows it is the
model and effort your host reported. A model's name is printed as the host
gives it, and `m-sonnet` stands for one here.

The third part is everything else, and ends the report:

```text
the main conversation, by gate:
  dor: m-opus 1800 in, 21000 read from the cache, 9000 written to it, 700 out
  analysis: m-opus 2400 in, 64000 read from the cache, 11000 written to it, 1500 out
  tests_frozen: m-opus 2100 in, 71000 read from the cache, 8000 written to it, 1200 out
  plan: m-opus 1900 in, 76000 read from the cache, 6000 written to it, 900 out
  design_review: m-opus 3200 in, 142000 read from the cache, 13000 written to it, 2300 out
  implementation: m-opus 1700 in, 88000 read from the cache, 5000 written to it, 800 out
  verifier_review: m-opus 1600 in, 93000 read from the cache, 4000 written to it, 900 out
  commit: m-opus 2000 in, 101000 read from the cache, 6000 written to it, 1100 out
  retro: m-opus 1500 in, 58000 read from the cache, 7000 written to it, 700 out
  after the last gate: m-opus 1200 in, 61000 read from the cache, 3000 written to it, 500 out
shell commands, by class:
  build: 1, 2.6s in all, the longest 2.6s
  git: 2, 0.3s in all, the longest 0.24s
  lint: 2, 19.4s in all, the longest 9.8s
  sdlc: 1, 0.31s in all, the longest 0.31s
  test: 6, 42.7s in all, the longest 21.8s
test runs: 6, 42.7s in all, 3 passed, 3 failed
refusals, by rule, agent and code:
  frozen-tests-are-read-only, sdlc:implementer: 1
the change at the commit gate: 4 files, 96 lines added and 7 removed; of them the frozen tests 1 files, 58 added and 0 removed

reviews, by gate:
  design_review: 2 rounds, 1 block
  verifier_review: 1 round, 0 blocks
  code_review: 1 round, 0 blocks
```

**What else it tells you.** The commands `sdlc` ran itself when the
implementation gate was recorded are no agent's shell commands, so they have
a line of their own after `test runs`, which gives how many ran and how long
they took:

```text
Gate 5 commands, run by sdlc at the implementation gate: 5, 0.59s in all
```

A story whose tests were reviewed before the freeze has a `tests_frozen` row
under `reviews, by gate` as well. A gate whose agents ran past its
[`budget.gate_hours`](configuration.md#budget) ends its row
`<- past budget.gate_hours`. An agent that started and never ended is listed as
`unfinished` and left out of the times. The report also says when lines of the
trace could not be read, and when the trace reached its limit and the later
records are missing.

**A story with no trace.** A story worked before `sdlc` measured anything has
only its gates to go on. `sdlc trace` then says once that the story predates
tracing, and prints `not recorded` for what it cannot know, never 0. Its
second line reads:

```text
no trace.jsonl for this story: it predates tracing, or nothing was recorded (see "sdlc trace shows nothing was recorded" in the troubleshooting guide)
```

[`sdlc trace` shows nothing was recorded](troubleshooting.md#sdlc-trace-shows-nothing-was-recorded)
says what to check when a new story has none.

**When it refuses.** With neither an id nor `--all`, with more than one id, or
with both, it is refused and says which mistake it was
([SDLC-E0034](troubleshooting.md#sdlc-e0034)). A story the backlog does not
hold, or one that was never started, is
[SDLC-E0009](troubleshooting.md#sdlc-e0009), and a trace that cannot be read is
[SDLC-E0005](troubleshooting.md#sdlc-e0005).

**With `--json`.** It prints one JSON document and nothing else, with every
time in seconds, and `null` for a figure that was not recorded:

| Part of the report | Key |
| --- | --- |
| the story | `story`, with `traced`, `skipped_lines`, `truncated` and `truncated_at` |
| time, by gate | `gates`, each with `gate`, `wall`, `agents`, `person`, `between` and `unaccounted`, and `past_gate_hours` where it ran past its hours; `total` for the last row |
| unfinished agents | `unfinished`, each with `agent_type`, `agent_id` and `start` |
| by kind of agent | `agent_types`, each with `agent_type`, `runs`, `time`, `models`, `responses`, `peak_context`, `cache_read_ratio`, `tools`, `asked`, `observed_models`, `observed_efforts`, `done` and `blocked` |
| the main conversation | `main`, each with `gate` and `models` |
| shell commands | `shell`, each with `class`, `count`, `total` and `longest` |
| test runs | `tests`: `count`, `time`, `passes` and `failures` |
| Gate 5 commands | `implementation_commands`: `count` and `time`. Absent where the gate recorded none. |
| refusals | `refusals`, each with `rule`, `agent_type`, `code` and `count` |
| the change at the commit gate | `diff`: `files`, `added`, `removed`, `test_files`, `test_added` and `test_removed` |
| reviews | `reviews`, each with `gate`, `rounds` and `blocks` |

[See where the time and tokens went](guides/tracking-what-a-story-costs.md#see-where-the-time-and-tokens-went)
shows when the report is worth reading.

### `sdlc trace --all`

Every story that has a record, one row each, with the median of each column
under them. A second line under each row gives what the gates recorded: the
escalations, the decisions recorded with [`sdlc decide`](#sdlc-decide-id),
the freezes lifted, the review rounds, and whether a gate failed after the
commit gate had passed.

```console
$ sdlc trace --all
story      start                     end                       wall         agents       waiting on a person  design review to end  tokens         largest context  test time    refusals     diff files   diff lines   first pass
AUTH-1     2026-11-17T09:00:00.000Z  2026-11-17T14:30:00.000Z  5h30m        not recorded 0s                   3h50m                 not recorded   not recorded     not recorded not recorded not recorded not recorded no
           escalations: none; decisions: 0; unfreezes: 0; review rounds: code_review 1; a gate failed after the commit gate: no
AUTH-2     2026-11-19T13:10:00.000Z  2026-11-19T16:50:00.000Z  3h40m        40m          0s                   2h00m                 360000         70200            12s          0            3            65           yes
           escalations: none; decisions: 0; unfreezes: 0; review rounds: none; a gate failed after the commit gate: no
AUTH-3     2026-11-23T09:20:00.000Z  2026-11-23T13:45:00.000Z  4h25m        2h52m        42m                  2h32m                 2771640        88600            42.7s        1            4            103          yes
           escalations: pre_commit_approval 1; decisions: 0; unfreezes: 0; review rounds: design_review 2, verifier_review 1, code_review 1; a gate failed after the commit gate: no
AUTH-4     2026-11-24T10:05:00.000Z  in progress               40m          0s           0s                   not yet               30100          29500            0s           0            not recorded not recorded not yet
           escalations: none; decisions: 0; unfreezes: 0; review rounds: none; a gate failed after the commit gate: no
median                                                         4h25m        1h46m        0s                   2h32m                 1565820        79400            27.35s       0            4            84
first-pass success, of the stories that ended: 67%
a story still in progress is left out of the medians; not recorded is a figure its story did not record
```

Here `AUTH-1` predates tracing and `AUTH-4` is under way. `first pass` means no
block at the verifier's review or the code review, and no failure at Gates 6
and 7. A story in progress is left out of the medians, and reads `not yet`
until it ends or has one of those on its record.

With `--json`, each row in `stories` has `story`, `start`, `end`, `wall`,
`agents`, `person`, `escalations`, `decisions`, `unfreezes`, `review_rounds`,
`first_pass`,
`design_to_end`, `failed_after_commit`, `tokens`, `peak_context`, `test_time`,
`refusals`, `diff_files` and `diff_lines`. After them come `medians` and
`first_pass_share`.

Where no story has a record yet, it says so and exits 0:

```console
$ sdlc trace --all
no story has a record yet: a story gets one when it is started ("sdlc start <ID>")
```

### Reading the trace file

You rarely need to: `sdlc trace` reads it for you. The trace is `trace.jsonl`
in the story's directory under `.sdlc/stories/`, one JSON object to a line.
It holds times, counts, exit codes and short names, and no prompt, response,
command argument, file path or error message.
[The trace](the-loop.md#the-trace) says why it is kept out of git.

Every line has `at`, the time, and `kind`, which is one of these:

| Kind | Written when | What it holds |
| --- | --- | --- |
| `agent_run` | an agent stops | the agent's type, its start and end, the models and tokens it used, its tool calls, the effort the host reported, and `asked_model` and `asked_effort` |
| `turn` | the main conversation's turn ends | the models and tokens, the responses and the tool calls of that turn |
| `handback` | an agent hands its work back | the agent, and `done` or `blocked` |
| `refusal` | the hook refuses a tool call | the rule, the agent, the tool, and the `SDLC-E` code where there is one |
| `shell` | a shell command ends | how long it took, its class (`test`, `build`, `lint`, `git`, `sdlc` or `other`), the program, and a test run's outcome |
| `diff` | the commit gate passes | files and lines changed, and the same for the frozen tests alone |
| `notification` | the host asks you for permission, or says it is idle | which of the two |
| `api_error` | a turn ends on an error from the model provider | the kind of error, such as `rate_limit` |
| `compaction` | the host compacts a conversation | whether it was `auto` or `manual` |
| `truncated` | the trace reaches its 4 MiB limit | the size. Nothing is written after it. |

Two more records, `agent_start` and `agent_end`, sit in the story's gate
record and not in the trace. They mark when each agent started and stopped,
which is how a story's time is split between its agents.

`asked_model` and `asked_effort` are what the story's staffing asked for that
agent, after any [`sdlc staffing assign`](#sdlc-staffing-assign-agent) of
yours. They are not what ran: that is in the record's `models` and `effort`.
Both are left out for an agent outside the loop, such as `Explore`.

## `sdlc version`

Print the version of `sdlc` you are running. It is the first thing to include
when you report a problem.

```text
sdlc version [flags]
```

| Flag | What it does |
| --- | --- |
| `--short` | print only the semantic version |

```console
$ sdlc version
$ sdlc version --short
```

With `--json` it prints `version`, `commit`, `date` and `dirty`, and with
`--short` as well, `version` alone. [Updating](installation.md#updating) says
how to move to a newer one.

## `sdlc completion`

Print a completion script for your shell, so that Tab completes commands and
flags.

```text
sdlc completion [command]
```

The command is your shell: `bash`, `zsh`, `fish` or `powershell`.

| Flag | What it does |
| --- | --- |
| `--no-descriptions` | leave the descriptions out of the completions |

```console
$ sdlc completion zsh --help
```

Each shell loads its script differently, and the help for each one says how.

## `sdlc approve`

Answer when the loop has handed you a story. When `sdlc status` says a story
is waiting for you, this is the command that lets it carry on.

Only a person runs it. In Claude Code or Claude Desktop, type `/sdlc:approve`
with the same arguments.

```text
sdlc approve [story] [flags]
```

| Flag | What it does |
| --- | --- |
| `--reject` | send the work back instead, saying why |

```console
$ sdlc approve AUTH-3
AUTH-3  approved (pre_commit_approval)

A person runs `sdlc start AUTH-3`, or types /sdlc:next in Claude Code, to carry on.
```

Name the story. Without an id, `approve` answers for the story being worked
on, and an escalation ends the iteration, so afterwards there is none
([SDLC-E0011](troubleshooting.md#sdlc-e0011)). Your answer goes on the record,
the story goes back to in progress, and `sdlc start` picks it up again.

To send the work back, give the reason. Whoever does the work next reads it:

```console
$ sdlc approve AUTH-3 --reject "the migration has no way back"
```

**What your answer covers.** Once a story has been committed in its worktree,
your answer also records the commit the story's branch is at. Where the story
pauses before the commit, for its tier or for a path its change touches, an
approving answer prints that commit, and says so when it is not the one the
story was held to before.
[What your approval covers](guides/human-approval-gates.md#what-your-approval-covers)
has the rest.

**After a `work_by_hand` hand-over.** Where the gate's work belongs to an
agent your project turned off, approving does not do that work. `approve` says
which gate is open, and you record it with
`sdlc gate <gate> pass --story <ID>` after doing the work by hand. Neither
that answer nor one given after an agent failed stands in for the pause before
the commit. A story that pauses, for its tier or for a path its change
touches, is still asked then, and `approve` says so. Where it cannot tell
from where it runs whether the story will pause, it says the question may
still be asked.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| nothing on the story is waiting for a decision | [SDLC-E0035](troubleshooting.md#sdlc-e0035) |
| another checkout holds the story; run it in that one | [SDLC-E0050](troubleshooting.md#sdlc-e0050) |
| `--reject` has no reason | [SDLC-E0034](troubleshooting.md#sdlc-e0034) |
| it was run in a process Claude Code started | [SDLC-E0079](troubleshooting.md#sdlc-e0079) |

[Human approval gates](guides/human-approval-gates.md) walks through reading
the work and answering.

## `sdlc unfreeze`

Lift the freeze on a story's tests, on the record. You do this only when a
frozen test is wrong.

Only a person runs it. In Claude Code, type `/sdlc:unfreeze` with the same
arguments.

```text
sdlc unfreeze [flags]
```

| Flag | What it does |
| --- | --- |
| `--reason` | why the frozen tests have to change. Required. |

Run it in the checkout that holds the story:

```console
$ sdlc unfreeze --reason "AC-1's test asserted the old error message"
AUTH-4  freeze lifted on 2 test files
  AC-1's test asserted the old error message
  reopened: tests_frozen, plan
```

Lifting the freeze sends the story back to Gate 3. The tests are about to
change, so `tests_frozen` and every gate passed after it go back to pending,
with your reason as their note. They are listed under `reopened`, and in
`--json` as well.

> [!NOTE]
> The reason is the point. Lifting the freeze is sometimes right, and it is
> also the move an agent would make to reach green. Writing down why is what
> tells the two apart.

`unfreeze` works on the story being worked on. After an escalation, which ends
the iteration, answer with `sdlc approve <ID>` and run `sdlc start <ID>`
first.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| there is no `--reason` | [SDLC-E0026](troubleshooting.md#sdlc-e0026) |
| there is no freeze to lift, or it belongs to another story | [SDLC-E0023](troubleshooting.md#sdlc-e0023) |
| no iteration is running | [SDLC-E0011](troubleshooting.md#sdlc-e0011) |
| it was run in a process Claude Code started | [SDLC-E0079](troubleshooting.md#sdlc-e0079) |

[When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md) covers
deciding, lifting the freeze and having the test corrected.

## `sdlc ack`

Mark how far you have read the log. The next `sdlc log` starts after that
commit, and the decisions of the stories up to it are no longer counted by
`sdlc status` as waiting to be read.

Only a person runs it. In Claude Code or Claude Desktop, type `/sdlc:ack` with
the same arguments.

```text
sdlc ack [flags]
```

| Flag | What it does |
| --- | --- |
| `--through` | the commit you have read the log up to: `HEAD`, or one in its history. Required. |

```console
$ sdlc ack --through HEAD
Acknowledged through 646584db7109. `sdlc log` starts after it.
```

There is no default: you say which commit. The output names the commit
acknowledged before, and says when the new one comes before it, because the
log then lists the stories in between again:

```console
$ sdlc ack --through HEAD~1
Acknowledged through 8edf4f0e2a43. `sdlc log` starts after it.
That is before 646584db7109, the commit acknowledged until now, so the log lists the stories committed in between again.
```

The acknowledgement is a note of how far you have read in this clone, and it
stays there, uncommitted.

> [!WARNING]
> Leave the acknowledgement out of git. With worktrees on, committing it makes
> `sdlc doctor` report a [`story worktrees`](#the-story-worktrees-check)
> problem, and no story can land until it is taken out again.

With `--json`, it prints `through` and `previous` as full commits, and `back`,
which is `true` when you moved the mark backwards.

**When it refuses.** A missing `--through`, a commit git cannot read, or one
that is not on this branch is refused
([SDLC-E0034](troubleshooting.md#sdlc-e0034)), and so is a run in a process
Claude Code started ([SDLC-E0079](troubleshooting.md#sdlc-e0079)).
[Mark it read](guides/reviewing-what-landed-on-trunk.md#mark-it-read) shows
where `ack` fits in a review.

## `sdlc release`

Let a story go from the checkout that holds it, so that any checkout can start
it. You want this when a worktree was deleted, or when a story should be
worked somewhere else.

Only a person runs it. In Claude Code, type `/sdlc:release` with the same
arguments.

```text
sdlc release STORY [flags]
```

| Flag | What it does |
| --- | --- |
| `--reason` | why the story is moving to another checkout. Required. |

```console
$ sdlc release AUTH-4 --reason "it will be worked in another clone"
Released AUTH-4: the checkout at "/home/<user>/src/billing/.sdlc/worktrees/AUTH-4" no longer holds it. "sdlc start AUTH-4" picks it up in any checkout.
Anything the checkout at "/home/<user>/src/billing/.sdlc/worktrees/AUTH-4" has not committed stays there, and is not moved.
```

Nothing is moved. Whatever the old checkout had not committed stays in it,
and so does a freeze: in a checkout that picks the story up without one,
`sdlc unfreeze --reason ...` lifts it and `sdlc freeze` takes it again. One
thing is dropped: the sections of `CODEMAP.md` stored for the story, which
belonged to the attempt that is over. Your reason goes on the story's record.

A story that nothing holds is not an error. `release` says
`Nothing to release` and changes nothing.

With `--json`, it prints `story`, and `checkout`, the checkout that held it,
where one did.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| there is no `--reason` | [SDLC-E0026](troubleshooting.md#sdlc-e0026) |
| an iteration is running on the story; run `sdlc stop` in that checkout first | [SDLC-E0012](troubleshooting.md#sdlc-e0012) |
| it was run in a process Claude Code started | [SDLC-E0079](troubleshooting.md#sdlc-e0079) |

[Move a story to another checkout](guides/stopping-and-resuming-a-story.md#move-a-story-to-another-checkout)
walks through it.

## `sdlc remap STORY`

Put a landed story's change to `CODEMAP.md` on trunk, or drop it. You need it
only after a stop has warned you with
[SDLC-E0076](troubleshooting.md#sdlc-e0076): the story landed, and the
sections its bookkeeper stored did not go into the map.

Only a person runs it. In Claude Code, type `/sdlc:remap` with the same
arguments.

```text
sdlc remap STORY [flags]
```

| Flag | What it does |
| --- | --- |
| `--replace` | put the kept sections over whatever trunk's map holds under their headings |
| `--drop` | discard the kept change and commit nothing |

Read the kept change before you run it. The warning prints the file's full
path, the commit is made from it, and nobody reviewed it.

```console
$ sdlc remap AUTH-3
CODEMAP.md on main is up to date at 646584db7109, in a commit of its own after the story's (adds "internal/session", replaces "internal/billing").
```

You have three choices:

| Run | When |
| --- | --- |
| `sdlc remap <ID>` | Whatever was in the way is gone. It makes the change `stop` would have made: one commit of `CODEMAP.md` alone on trunk, and only where every section still applies. |
| `sdlc remap <ID> --replace` | Trunk's map has changed under those headings and you want the story's version. What trunk held stays in history. |
| `sdlc remap <ID> --drop` | You edited `CODEMAP.md` yourself, or the change is no longer wanted. Nothing is committed. |

The plain form refuses with SDLC-E0076 again and names each section that does
not apply: an added one whose heading trunk's map now has, or a replaced one
whose text on trunk has changed since the bookkeeper read it. Nothing is
committed, and the change stays kept however often you run it.

Where something else was in the way, such as uncommitted changes to
`CODEMAP.md` in the checkout that has trunk checked out, put that right and
run the plain form. `--replace` does not help there, and `--drop` would throw
the change away.

The commit is made by `sdlc`. It is not signed, no hook of the repository runs
for it, and its message says no reviewer read it. It moves trunk, as any
commit does.

> [!NOTE]
> `/sdlc:remap` first waits for another story's landing to finish, and Claude
> Code gives a slash command 60 seconds. If the landing takes longer, nothing
> shows for a minute and the model says the command did not run, which is
> true. Type it again once the landing has finished.

With `--json`, it prints `story`, and `codemap_commit` or `dropped`.
`sdlc remap --help` is refused in a session along with the command;
`sdlc help remap` prints the help.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| a section does not apply to trunk's map | [SDLC-E0076](troubleshooting.md#sdlc-e0076) |
| the story has not landed, has nothing kept, or its change is already on trunk | [SDLC-E0077](troubleshooting.md#sdlc-e0077) |
| the kept file is no longer a set of sections; `--drop` still removes it | [SDLC-E0078](troubleshooting.md#sdlc-e0078) |
| it was run in a process Claude Code started | [SDLC-E0079](troubleshooting.md#sdlc-e0079) |

[Parallel stories and `CODEMAP.md`](the-loop.md#parallel-stories-and-codemapmd)
explains why the map is handled apart from the story's commits.

## `sdlc staffing assign AGENT`

Choose the model or the effort an agent is asked for on one story. Reach for
it when you know better than the project manager what a story deserves.

Only a person runs it. In Claude Code, type `/sdlc:assign` with the same
arguments, or run it in your own terminal.

```text
sdlc staffing assign AGENT [flags]
```

| Flag | What it does |
| --- | --- |
| `--model` | the model to ask for, one of `team.models` |
| `--effort` | the effort to ask for, one of `team.efforts` |
| `--reason` | why. Required. |
| `--story` | a story that has been started, instead of the one being worked on |

```console
$ sdlc staffing assign security --model opus --effort high --reason "this one touches the installer"
AUTH-3  assigned by a person: security: sonnet at high -> opus at high
```

The agent is then needed at every gate it has a part in, and nothing the
project manager records later changes what you assigned. Running it again
changes it. Before a staffing is recorded, or for an agent the staffing leaves
out, give both `--model` and `--effort`. It is held to the project's limits as
any staffing is, and it cannot take an agent off a story.

**When it refuses.** From inside a Claude Code session it is refused
([SDLC-E0074](troubleshooting.md#sdlc-e0074)), and anywhere else Claude Code
started the process as well ([SDLC-E0079](troubleshooting.md#sdlc-e0079)). A
model or effort outside the project's lists is
[SDLC-E0071](troubleshooting.md#sdlc-e0071), and with the project manager
turned off there is no staffing to assign to
([SDLC-E0067](troubleshooting.md#sdlc-e0067)).
[Choose the model for one agent](the-agents.md#choose-the-model-for-one-agent)
says when it is worth doing.

## `sdlc gate GATE STATUS`

Record the outcome of one gate. The loop runs this as each gate comes to an
end. You would reach for it yourself only to reopen a gate, or to record one
whose work you did by hand.

```text
sdlc gate GATE STATUS [flags]
```

| Flag | What it does |
| --- | --- |
| `--note` | why the gate came out this way, in one line |
| `--story` | record against this story instead of the one being worked on. The id has to be in the backlog. |
| `--security-sensitive` | the story touches a trust boundary, so the security review blocks. Set at the analysis gate, and assumed true until it is set. |

```console
$ sdlc gate analysis pass --security-sensitive --note "a session token crosses a trust boundary"
AUTH-3  analysis  pass
  a session token crosses a trust boundary
```

STATUS is `pass`, `fail` or `pending`. GATE is one of eleven names, because
Gates 4 and 6 each record the work and its review:

| Gate | Name |
| --- | --- |
| 1 | `dor` |
| 2 | `analysis` |
| 3 | `tests_frozen` |
| 4 | `plan`, then `design_review` |
| 5 | `implementation` |
| 6 | `verification`, then `verifier_review` |
| 7 | `code_review` |
| 8 | `commit` |
| 9 | `retro` |

`gate` records a decision; it does not make one. It does refuse to record a
pass that is not true, and [The gates, one by one](the-loop.md#the-gates-one-by-one)
says what each pass requires. A `fail` can always be recorded, because the
loop needs somewhere to put a gate whose work could not be done.

**Reopening.** Recording a gate as failed sends the gates after it back to
pending, and the command names them:

```console
$ sdlc gate plan fail --note "the plan misses the migration"
AUTH-3  plan  fail
  the plan misses the migration
  reopened: design_review, implementation, verification
```

Each has to pass again before the story moves on. `--json` lists them under
`reopened`, and [`sdlc log`](#sdlc-log) shows the same gates.
[Rework](the-loop.md#rework) explains what goes back and why.

A gate that fails `loop.max_rework_rounds` times since you last answered for
the story hands the story to you, as [`sdlc escalate`](#sdlc-escalate) would,
and says so: `handed_over` in `--json`.

**The implementation gate.** `sdlc gate implementation pass` does more than
record. Before it records the pass, it runs your configured commands itself:
`build`, `test`, `lint`, `fmt_check`, `coverage` and `mutation`, those that
are set, in that order, at the root of the story's worktree. Each has to exit
0, and `coverage` and `mutation` have to reach their
[thresholds](configuration.md#thresholds). Then it reports what it ran:

```console
$ sdlc gate implementation pass --note "frozen tests pass"
AUTH-3  implementation  pass
  frozen tests pass
  ran commands.build (0.032s), commands.test (0.447s), commands.lint (0.842s), commands.fmt_check (0.009s), commands.coverage (0.035s) on tree 571ae232889d
```

Before each command, one line on standard error names it and the file its
output is kept in:

```text
sdlc: running commands.build for AUTH-3's implementation gate in /home/<user>/src/billing/.sdlc/worktrees/AUTH-3; its output is put in /home/<user>/src/billing/.sdlc/stories/AUTH-3/implementation-run.log when the run ends
```

A gate with none of those commands set runs nothing, and neither does `fail`
or `pending`. This is what the run gives you:

- **A result nobody has to repeat.** The pass records each command's name,
  exit status and time against the tree hash, which is the same measure of
  the story's files that a review is stamped with. While
  `sdlc status --json` reports that result as `current`, the verifier works
  from it and [the landing](#landing-a-story-from-its-worktree) lands on it,
  and neither runs the commands again. Recording the gate again clears it.
  With `--json`, the pass gives the run as `commands` and `tree_hash`.
- **One directory for the whole run.** The commands share a new, empty
  directory named in `SDLC_RUN_DIR`, so that `coverage` can read what `test`
  left there. [The run directory](configuration.md#the-run-directory) shows
  how.
- **A failing command fails the gate.** A command that exits other than 0,
  misses its threshold or prints no figure is refused with
  [SDLC-E0095](troubleshooting.md#sdlc-e0095), and the gate is recorded as
  failed, so it counts towards
  [`loop.max_rework_rounds`](configuration.md#loop). The refusal names the
  first command that did not pass, shows the last 20 lines it printed, and
  names the file that holds everything,
  `.sdlc/stories/<ID>/implementation-run.log` in the main checkout. It begins:

  ```text
  sdlc: commands.test ended with status 1 and did not pass, so the implementation gate is recorded as failed
  ```

- **A run that proves nothing records nothing.** Three things are refused
  with the same code and spend no rework round: a command that could not be
  started, a command that wrote to a file of the tree it ran on, and a story
  that changed while the commands ran.
- **One run at a time.** Other stories, and `sdlc status`, carry on while the
  commands run. A second `sdlc gate implementation pass` or `sdlc freeze` on
  the same story is refused at once
  ([SDLC-E0006](troubleshooting.md#sdlc-e0006)), and so is one run from
  inside one of the commands.

> [!WARNING]
> A result run by `sdlc` was run as you configured it,
> from a line the rule could read: the hook refuses a command line that sets
> the environment before the call
> ([`commands-run-as-configured`](enforcement.md#commands-run-as-configured)).
> It is not proof that the environment was not chosen some other way, and
> [Not held](enforcement.md#not-held) lists what can still change it.

**The commit gate.** A pass of `commit` records the commits the story is made
of and prints them:

```console
$ sdlc gate commit pass --note "committed"
AUTH-3  commit  pass
  committed
  committed on story/AUTH-3 in 27367e1d65c8..8edf4f0e2a43
```

With worktrees turned off the line reads `landed in <start>..<end>`, because
the commits are on trunk already. Where somebody else's commit sits among the
story's, no single range holds only the story's commits, and the line says
`no range recorded`. The work is fine; the record only has no range. A commit
hash you put in the note has to be one of those commits
([SDLC-E0049](troubleshooting.md#sdlc-e0049)).

**The last gate.** Recording the last gate finishes the story:

```console
$ sdlc gate retro pass --note "retro written"
AUTH-3  retro  pass
  retro written

Every gate on AUTH-3 has passed. `sdlc stop` in .sdlc/worktrees/AUTH-3 lands it on main.
```

A story in its own worktree is done when
[`sdlc stop`](#landing-a-story-from-its-worktree) lands it. A gate recorded as
failed after that puts the story back in progress, where the rework belongs.

**Findings hold two gates.** `verification pass` reads the design review's
findings, and `commit pass` the design and code reviews'. Each is refused while
a finding is open ([SDLC-E0092](troubleshooting.md#sdlc-e0092)), naming every
one, until it is closed with
[`sdlc finding close`](#sdlc-finding-close-gate-role-f-n). A finding referred
to a person holds the gate too, where the story has since changed the
finding's file ([SDLC-E0091](troubleshooting.md#sdlc-e0091)).

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| the gate or the status is not one it knows | [SDLC-E0013](troubleshooting.md#sdlc-e0013), [SDLC-E0014](troubleshooting.md#sdlc-e0014) |
| an earlier gate has not passed | [SDLC-E0031](troubleshooting.md#sdlc-e0031) |
| a reviewer blocks the gate. Where the gate has gone to the reviewer's arbiter, the refusal names the arbiter | [SDLC-E0030](troubleshooting.md#sdlc-e0030) |
| a command the implementation gate ran did not pass, or its run proved nothing | [SDLC-E0095](troubleshooting.md#sdlc-e0095) |
| the story's commands are already running | [SDLC-E0006](troubleshooting.md#sdlc-e0006) |
| `analysis` would pass with no staffing recorded | [SDLC-E0068](troubleshooting.md#sdlc-e0068) |
| a finding is open, or stands referred in a file the story changes | [SDLC-E0092](troubleshooting.md#sdlc-e0092), [SDLC-E0091](troubleshooting.md#sdlc-e0091) |
| the story's commits add the markers of an unresolved conflict | [SDLC-E0080](troubleshooting.md#sdlc-e0080) |
| another checkout or another session holds the story | [SDLC-E0050](troubleshooting.md#sdlc-e0050), [SDLC-E0048](troubleshooting.md#sdlc-e0048) |

Where [`budget.gate_hours`](configuration.md#budget) gives a gate a number of
hours and the agents have spent longer at it, `gate` prints one line about it
on standard error and decides exactly as it would have.

## `sdlc artifact list`

See which documents `sdlc` stores for a story. The names it prints are the
ones [`sdlc artifact write`](#sdlc-artifact-write-name) accepts.

```text
sdlc artifact list [flags]
```

`artifact list` has no flags of its own.

```console
$ sdlc artifact list
  analysis   ANALYSIS.md    gate analysis   written by the researcher agent
  threats    THREATS.md     gate analysis   written by the researcher agent
  test_plan  TEST-PLAN.md   gate tests_frozen written by the sdet agent
  plan       PLAN.md        gate plan       written by the implementer agent
  verification VERIFICATION.md gate verification written by the verifier agent
  retro      RETRO.md       gate retro      written by the bookkeeper agent
```

Each line is a document's name, its file in the story's directory, the gate
that needs it, and the agent that writes it. With `--json`, each entry in
`artifacts` has `name`, `file`, `gate` and `role`.

## `sdlc artifact write NAME`

Store one of a gate's documents for the story being worked on. The agent whose
gate it is runs this, with the document on standard input or in a file.

```text
sdlc artifact write NAME [flags]
```

| Flag | What it does |
| --- | --- |
| `--file` | read the document from this path instead of standard input |
| `--story` | store against this story instead of the one being worked on. The id has to be in the backlog. |

```console
$ sdlc artifact write analysis < analysis.md
AUTH-3  analysis  .sdlc/stories/AUTH-3/ANALYSIS.md (57 bytes)
```

NAME is `analysis`, `threats`, `test_plan`, `plan`, `verification` or `retro`.
Writing a document again replaces it, so a gate that ran twice has one
document, not two.

This is the only way these files are written. An agent that edits one in place
is refused, including the agent whose gate it is, so that what a gate read is
what was stored.

Storing the plan also keeps that revision of it, so that a reviewer asked to
look again can see what changed with
[`sdlc artifact diff plan`](#sdlc-artifact-diff-plan).

**When it refuses.** A name it does not know is
[SDLC-E0016](troubleshooting.md#sdlc-e0016), and an empty document is
[SDLC-E0017](troubleshooting.md#sdlc-e0017). One larger than a megabyte is
[SDLC-E0019](troubleshooting.md#sdlc-e0019): a document cut short would leave
every later gate reviewing something that stops mid-sentence. With no story
being worked on and none named, it is
[SDLC-E0011](troubleshooting.md#sdlc-e0011).

## `sdlc artifact diff plan`

Print what changed in the plan since a reviewer's latest design review. A
reviewer asked to look again after a rework reads this, and not the whole plan
a second time.

```text
sdlc artifact diff plan [flags]
```

| Flag | What it does |
| --- | --- |
| `--since` | the design reviewer whose latest review the diff starts from. Required. |

```console
$ sdlc artifact diff plan --since architect
--- PLAN.md 40c9f6da7ea56d0ae851a7114f073945adaf8903e44383452f4111abcdb86a5d
+++ PLAN.md ad70ac83778a442067fc5e0f4420419aafe37666cc890dc70787299d138f4dba
@@ -1,3 +1,3 @@
 # Plan
 
-One.
+One, and a correction to it.
```

It prints a unified diff with three lines of context, from the plan that
review read to the plan as it stands now. Each side is named by the hash of
its content, which is also the name its revision is kept under. The command
records nothing, so the reviewer still records a verdict on the plan as it now
stands.

It prints nothing and exits 0 when the plan has not changed since that
review: the review is current, and the gate accepts it as it is.

Every revision is kept, one for each distinct plan, and `sdlc` never removes
one. Correcting a plan therefore does not remove the earlier text, and a
project that commits `.sdlc/stories` commits it too.

With `--json`, it prints `story`, `role`, `reviewed_subject` (the hash of the
plan the review read), `subject` (the hash of the plan now) and `diff`.

**When it refuses.** Where the plan that review read cannot be given, because
its revision was not kept or the reviewer has no design review yet, it refuses
and that reviewer reads the whole plan this round
([SDLC-E0084](troubleshooting.md#sdlc-e0084)). A diff from anything else could
hide a change. A missing `PLAN.md` is
[SDLC-E0005](troubleshooting.md#sdlc-e0005), any name other than `plan` is
[SDLC-E0016](troubleshooting.md#sdlc-e0016), and a `--since` that names no
design reviewer is [SDLC-E0027](troubleshooting.md#sdlc-e0027).
[Why approvals go stale](the-loop.md#why-approvals-go-stale) explains when a
reviewer is asked to look again.

## `sdlc codemap write`

Store the sections a story adds to `CODEMAP.md`, or replaces in it. The
bookkeeper runs this at the retro, in the story's worktree, and never edits
the file there. [`sdlc stop`](#landing-a-story-from-its-worktree) puts the
sections on trunk once the story has landed.

```text
sdlc codemap write [flags]
```

`codemap write` has no flags of its own. The sections arrive on standard
input:

```console
$ sdlc codemap write <<'MD'
<!-- sdlc:add -->
## internal/session

Package `session`. Says whether a token may be used.

<!-- sdlc:replace -->
## internal/billing

Package `billing`. Keeps the accounts and, now, invoices.
MD
Stored 2 sections for AUTH-3: adds "internal/session", replaces "internal/billing". `sdlc stop` puts them into CODEMAP.md on main once the story has landed.
```

Each section begins with a line that is exactly `<!-- sdlc:add -->` or
`<!-- sdlc:replace -->`, then its heading as `##`, a space and the text, then
at least one line under it. A replaced section takes the place of the section
with that heading, deeper headings and all, and an added one goes at the end of
the map. Headings are matched without regard to case.

Writing again replaces what was stored. A stored set lasts as long as the
attempt it was stored in: [`sdlc release`](#sdlc-release) drops it, and so
does a start that begins the story afresh.

**When it refuses.** The set reaches trunk with no reviewer, so it is held to
a narrow form and refused, never repaired
([SDLC-E0078](troubleshooting.md#sdlc-e0078)). A set that does not begin with
a directive, a body with a heading at the section's level or above, a fenced
block left open, a conflict marker, an empty section, a heading given twice,
or text that is not plain UTF-8 with line feeds are all refused. So is a story
that has no worktree of its own: no stop lands such a story, and the
bookkeeper edits `CODEMAP.md` in place there. With no story being worked on it
is [SDLC-E0011](troubleshooting.md#sdlc-e0011).

With `--json`, it prints `story`, and `sections`, a count.

## `sdlc review list`

See which reviews each gate expects and where they have got to. It is the
quickest way to find out why a gate is still waiting.

```text
sdlc review list [flags]
```

| Flag | What it does |
| --- | --- |
| `--gate` | only this gate's reviews |
| `--story` | for this story instead of the one being worked on |

```console
$ sdlc review list --gate design_review
Review policy: REVIEW.md
  design_review   architect       blocking  block (round 1)
  design_review   red-team        advisory  note (round 1)
  design_review   security        blocking  approve (round 1)
  design_review   perf            on budget left out by the staffing -- "nothing here is on a hot path"; delegate to sdlc:project-manager to amend the staffing if the story needs it
  design_review   human-advocate  advisory  approve (round 1)
```

The first line names the review policy every reviewer reads. Where there is no
file at that path, it says the project has `no review policy`.

Each line after it is one expected review: the gate, the reviewer, whether it
can stop the gate, and its latest verdict.

| It reads | What it means |
| --- | --- |
| `blocking` | the reviewer has to approve before the gate passes |
| `advisory` | the reviewer only has to report |
| `on budget` | perf: it blocks only when a performance budget in the contract is breached |
| `not reviewed` | the gate is still waiting for this review |
| `left out by the staffing` | the story's staffing leaves this reviewer out, with its reason, and the gate does not wait for it |
| `stale` | the reviewer looked at something that has changed since, and has to look again |

A stale design review also names the revision of the plan it read, and the
command that shows what changed since:

```text
  design_review   architect       blocking  block (round 1, stale -- what was reviewed has changed since; reviewed revision 40c9f6da7ea56d0ae851a7114f073945adaf8903e44383452f4111abcdb86a5d; see sdlc artifact diff plan --since architect)
```

**A gate that has gone to an arbiter.** Once a reviewer has blocked as often
as it may, the gate goes to the reviewer's
[arbiter](the-loop.md#the-arbiter). Until the arbiter has reviewed, the
reviewer's row still gives its own block, and says where the gate has gone:

```text
  design_review   architect       blocking  block (round 2; the gate has gone to architect-arbiter, which reviews it next)
```

From then on the row gives the arbiter's review, which stands in the
reviewer's place, and names the arbiter:

```text
  design_review   architect       blocking  approve (by architect-arbiter, round 1)
```

With `--json`, `policy` is the path of the review policy, empty where there is
none, and each entry in `reviews` has `gate`, `role`, `blocking`, `verdict` and
`round`. It has `left_out` and `reason` for a reviewer the staffing leaves out,
`brought_back_by` for one a changed path brought back to the code review,
`reviewed_subject`, the plan's hash, for a stale design review, `gone_to` for
a gate that waits for an arbiter, and `reviewed_by` where the review is the
arbiter's.

[When a reviewer blocks](guides/when-a-reviewer-blocks.md) shows how to read a
block and what happens next.

## `sdlc review add GATE ROLE VERDICT`

Record one reviewer's conclusion. Each reviewing agent runs this when it has
finished, with the review itself on standard input or in a file.

```text
sdlc review add GATE ROLE VERDICT [flags]
```

| Flag | What it does |
| --- | --- |
| `--file` | read the review from this path instead of standard input |
| `--note` | the headline finding, in one line |
| `--important` | how many important findings this review names |
| `--nits` | how many nits this review found |
| `--finding` | one finding; once for each finding the review counts, and left out at 0 |
| `--story` | record against this story instead of the one being worked on. The id has to be in the backlog. |

```console
$ sdlc review add design_review architect block --important 1 --nits 1 \
    --finding "important the plan: AC-2 has no step" \
    --finding "nit internal/session/session.go:4: the name says nothing" < review.md
AUTH-3  design_review  architect  block (round 1)
  .sdlc/stories/AUTH-3/reviews/design_review-architect-1.md
```

VERDICT is `approve`, `block` or `note`. ROLE is the reviewer's name: its
role, or an instance's name such as `security-2` where the `team` section
gives a reviewer more than one. Once a gate has
[gone to an arbiter](the-loop.md#the-arbiter), ROLE is the arbiter's name,
which is the reviewer's with `-arbiter` after it: `architect-arbiter`, or
`security-2-arbiter` for an instance. Every round is kept, so what the
architect said last time stays answerable.

**A block names something important.** `block` with no `--important`, or with
0, is refused and records nothing
([SDLC-E0086](troubleshooting.md#sdlc-e0086)), because a review with only nits
is an approve with the nits listed. The counts are the reviewer's own, and
`sdlc` does not judge them. A count that is negative, above 1,000,000, or not
a number is refused ([SDLC-E0034](troubleshooting.md#sdlc-e0034)).

**Findings.** Each `--finding` is one finding: `<important|nit>`, then a path
from the repository's root where it is about one file, then a colon, a space
and the text. A line number after the path is fine. Put a value that holds
backticks, `$` or `"` in single quotes, because the shell expands them in
double quotes before `sdlc` reads it.

Findings are numbered F-1, F-2 and so on for each gate and reviewer, across
every round. At `design_review` and `code_review` the findings listed have to
match the counts, or nothing is recorded:

```console
$ sdlc review add design_review architect block --important 2 --nits 1 \
    --finding "important the plan: AC-2 has no step" < review.md
sdlc: the findings listed with --finding do not agree with --important and --nits

  why  --finding lists 1 important and 0 nits, and the review counts 2 important and 1 nit
  fix  list each finding the review counts with --finding "<important|nit> [path]: <text>", one flag for each: as many important ones as --important says, and no more nits than --nits; or give --important and --nits the numbers the review lists. Nothing was recorded, so run the command again

  SDLC-E0089  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0089
```

**Why an approval goes stale.** The review is tied to what was in front of the
reviewer: the story at `dor`, the plan at `design_review`, and the working
tree at `verifier_review` and `code_review`. When that changes, the review
reads `stale` and the reviewer looks again.

**Too many rounds.** A block that stops the gate, recorded
`loop.max_review_rounds` times by the same reviewer since you last answered
for the story, sends the gate to that reviewer's
[arbiter](the-loop.md#the-arbiter). An agent's own
[`max_rounds`](configuration.md#an-agents-entry) takes the place of
`loop.max_review_rounds` where it has one. The command says so in four lines
of its own, and `--json` has `arbiter`:

```console
$ sdlc review add design_review architect block --important 1 \
    --finding "important the plan: AC-2 still has no step" < review.md
AUTH-3  design_review  architect  block (round 2)
  .sdlc/stories/AUTH-3/reviews/design_review-architect-2.md
  architect has blocked design_review 2 times, and loop.max_review_rounds is 2: the gate goes to architect-arbiter, a fresh sdlc:architect that rules on the open findings.
  Its review then stands for architect at design_review, and architect is refused there.
  Until architect-arbiter first reviews, any answer a person gives the story starts architect's count again and gives it its turn.
  From then on, only a person's answer to a review_not_converging hand-over raised by architect or architect-arbiter at design_review gives architect its turn again.
```

From that block on, `review add` takes the arbiter at that gate and refuses
the reviewer ([SDLC-E0027](troubleshooting.md#sdlc-e0027)). Before it, the
arbiter is the one refused. The arbiter's review is recorded like any other:

```console
$ sdlc review add design_review architect-arbiter approve < review.md
AUTH-3  design_review  architect-arbiter  approve (round 1)
  .sdlc/stories/AUTH-3/reviews/design_review-architect-arbiter-1.md
```

The arbiter has the same limit. Its own block, recorded that many times,
hands the story to you as `review_not_converging`, and says so: `handed_over`
in `--json`. So does the reviewer's block at its limit where the agent is
disabled, since a disabled agent has no arbiter.

What gives the reviewer its turn again depends on whether the arbiter has
reviewed yet:

- **Before the arbiter's first review**, any answer you give the story starts
  the reviewer's count again, whatever the answer was about. The reviewer is
  then taken, and the arbiter is refused until the limit comes round again.
- **From the arbiter's first review**, only your answer to a
  `review_not_converging` hand-over that the reviewer or its arbiter raised
  at that gate gives the reviewer its turn again. An answer to another
  reviewer's hand-over, or to another gate's, does not. Neither does an
  `sdlc unfreeze`, so after one the arbiter of the test reviewer reviews the
  corrected tests.

**When it refuses.** Beyond the two above: a finding longer than 1,000 bytes,
one with a line break or a character that does not show, or more than 100
findings ([SDLC-E0034](troubleshooting.md#sdlc-e0034)); a reviewer whose
earlier findings an older `sdlc` dropped from the record, which runs the
command again with its `--finding` flags
([SDLC-E0093](troubleshooting.md#sdlc-e0093)).

[What each verdict does](guides/when-a-reviewer-blocks.md#what-each-verdict-does)
says how the three verdicts affect a gate.

## `sdlc finding close GATE ROLE F-N`

Close one finding a review listed, on the story being worked on. The loop
closes each finding once it is dealt with, because an open one holds the
verification and commit gates.

```text
sdlc finding close GATE ROLE F-N [flags]
```

| Flag | What it does |
| --- | --- |
| `--fixed` | the finding is fixed in the story |
| `--declined` | the finding is declined, with `--reason` |
| `--referred` | the finding is outside the story's files, cannot wait, and goes to a person, with `--reason` |
| `--reason` | why it was declined or referred, in one line |

```console
$ sdlc finding close design_review architect F-1 --fixed
AUTH-3  design_review architect F-1  fixed; 1 finding of design_review or code_review is still open: design_review architect F-2
```

It takes exactly one of `--fixed`, `--declined` and `--referred`. ROLE is the
name the review was recorded under. The line it prints says how many findings
are still open and names the first.

A referral is for you to read when the story ends, and `sdlc status` and
`sdlc log` list it:

```console
$ sdlc finding close code_review code-reviewer F-1 --referred --reason "another page, and the person should see it"
AUTH-3  code_review code-reviewer F-1  referred for a person to read when the story ends; 1 finding of design_review or code_review is still open: code_review code-reviewer F-2
```

A later closing replaces an earlier one as the finding's state, and both stay
on the record. With `--json`, `closing` holds `at`, `as`, `reason`, and
`session`, the Claude Code session the command ran in. The record is a tracked
file, so that id goes into your project's history with it.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| `--declined` or `--referred` has no reason | [SDLC-E0090](troubleshooting.md#sdlc-e0090) |
| `--referred` is used on a finding in a file the story changes: fix it, or decline it with a reason | [SDLC-E0091](troubleshooting.md#sdlc-e0091) |
| the reason is longer than 1,000 bytes or more than one line | [SDLC-E0034](troubleshooting.md#sdlc-e0034) |

[The scout rule](the-loop.md#the-scout-rule) says why a finding is fixed or
declined in the story, and not put off.

## `sdlc decide ID`

Record a decision the loop made for itself, and why. The loop runs this for
every [implementation decision](the-loop.md#two-kinds-of-decision), so that
you can read each one afterwards. A decision about the product is yours, and
goes to you with [`sdlc escalate`](#sdlc-escalate) instead.

```text
sdlc decide <ID> [flags]
```

| Flag | What it does |
| --- | --- |
| `--question` | what was decided, as a question |
| `--option` | one option that was weighed. Give it once for each: at least two, at most ten, and no two the same. |
| `--chosen` | the option chosen, word for word as one of the `--option` values |
| `--reason` | why it was chosen |
| `--by` | the agent whose recommendation or ruling was applied |

```console
$ sdlc decide AUTH-3 --question "Is a token that expires this very second expired?" \
    --option "expired" --option "still valid" --chosen "expired" \
    --reason "the check runs after the instant it names" --by researcher
AUTH-3  analysis  D-1 by researcher recorded in .sdlc/stories/AUTH-3/DECISIONS.md
```

The line it prints names the story, the gate the story is at, the decision's
number, who decided, and the file. Every flag is required, and each value is
one line of at most 1,000 bytes.

**Where a decision goes.** It is added to the story's record as a `decision`
event, and the story's `DECISIONS.md` is written again from the record, with
every decision under its number, in the order they were made. From there:

- [`sdlc log`](#sdlc-log) lists it on a `decided` line, with its story.
- [`sdlc status`](#sdlc-status) counts it, on its `Decisions:` line and as
  `decisions_unread`, until your [`sdlc ack`](#sdlc-ack) covers the story's
  commit.
- [`sdlc trace --all`](#sdlc-trace---all) counts it for its story.

A decision changes no gate, review, freeze, staffing or approval, and it
answers nothing you were asked. It can be recorded while a story waits for
you, and the story still waits.

**Who decided.** `--by` is an agent's name, such as `researcher`, an
instance's, such as `security-2`, or an arbiter's, such as
`architect-arbiter`. A person is never one. It names whose recommendation was
applied, and not the session that ran the command.

> [!NOTE]
> `sdlc` takes `--by` on trust. Any session may give any name the loop knows,
> and no gate reads it. [`sdlc trace`](#sdlc-trace) shows which session ran
> the command.

**Exit status.** It exits 0 once the decision is recorded, and 1 when it is
refused. A refusal records nothing: neither the record nor `DECISIONS.md`
changes. Running the same command twice records the decision twice, as the
next number, because the record is a history.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| a flag is missing or blank, a value is over 1,000 bytes or holds a line break or a character that does not show, there are fewer than two options or more than ten, or an option is given twice | [SDLC-E0094](troubleshooting.md#sdlc-e0094) |
| `--chosen` is not one of the options, word for word | [SDLC-E0094](troubleshooting.md#sdlc-e0094) |
| `--by` is no agent the loop knows | [SDLC-E0094](troubleshooting.md#sdlc-e0094) |
| your `sdlc ack` already covers the story's commit, so the decision would never be shown to you. Record it on the story under way, and name the other story in `--question` | [SDLC-E0094](troubleshooting.md#sdlc-e0094) |
| the backlog has no such story, or the story was never started | [SDLC-E0009](troubleshooting.md#sdlc-e0009), [SDLC-E0011](troubleshooting.md#sdlc-e0011) |
| another checkout holds the story | [SDLC-E0050](troubleshooting.md#sdlc-e0050) |

The refusal says which part to put right:

```console
$ sdlc decide AUTH-3 --question "Is a token that expires this very second expired?" \
    --option "expired" --option "still valid" --chosen "refused" \
    --reason "AC-1 says the expiry is in the past" --by researcher
sdlc: --chosen "refused" is not one of the options

  why  nothing was recorded
  fix  give --chosen as one of the --option values, word for word, then run the command again

  SDLC-E0094  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0094
```

With `--json`, it prints `story`, `gate`, `by`, `number` and `path`.
[Two kinds of decision](the-loop.md#two-kinds-of-decision) says which
decisions the loop makes, and which it brings to you.

## `sdlc staffing`

The staffing is, for one story, which agents the story needs and the model and
effort each is asked for. The project manager records it once the story is
found ready, and changes it as the work shows what it needs.
[The staffing](the-agents.md#the-staffing) explains how it is chosen.

`sdlc staffing` on its own prints its four commands:
[`show`](#sdlc-staffing-show), [`set`](#sdlc-staffing-set),
[`amend`](#sdlc-staffing-amend-agent) and
[`assign`](#sdlc-staffing-assign-agent), which is yours alone.

Where your project turns the project manager off in the
[`team`](configuration.md#team) section, there is no staffing: `set`, `amend`
and `assign` are refused ([SDLC-E0067](troubleshooting.md#sdlc-e0067)), and
every enabled agent takes part in every story.

> [!NOTE]
> What is recorded and shown is what was asked for. Your environment can
> override both a model and an effort, and `sdlc` does not see what an agent
> was given.

## `sdlc staffing show`

Read a story's staffing. This is the command to run when you want to know why
an agent is, or is not, taking part.

```text
sdlc staffing show [flags]
```

| Flag | What it does |
| --- | --- |
| `--gate` | who to delegate to at this gate, instead of the one the story is at |
| `--story` | for this story instead of the one being worked on |

```console
$ sdlc staffing show
AUTH-3  staffing, at analysis

  may be asked for, lowest first:  models haiku, sonnet, opus;  efforts low, medium, high, xhigh, max

  project-manager asked for sonnet at medium -- "the staffing is looked at again after the analysis"; "a short story to read"
  researcher      asked for opus at medium -- "every story is analysed"; "its first pass missed the callers"
  sdet            asked for sonnet at medium -- "every story has frozen tests"; "two criteria, one handler"
  implementer     asked for sonnet at medium -- "every story is implemented"; "a small change in one package"
  verifier        asked for opus at high -- "every story is verified"; "the criteria turn on an ordering that is easy to misread"
  bookkeeper      asked for haiku at low -- "every story has a retro"; "a short retro"
  architect       asked for sonnet at medium -- "the plan touches the session middleware"; "one boundary to check"
  security        asked for opus at high, a person chose -- "the story is about authentication"; "this one touches the installer"
  red-team        asked for sonnet at medium -- "the story is about authentication"; "a short plan"
  perf            left out -- "nothing here is on a hot path"
  human-advocate  asked for sonnet at low -- "the 401 response is what a user sees"; "one response to read"
  code-reviewer   asked for sonnet at medium -- "every story's diff is reviewed"; "a small diff"
  test-reviewer   asked for sonnet at medium -- "the tests are read before they are frozen"; "one test file"

  amended
    2026-11-24T09:47:21Z  perf: needed -> left out -- "nothing here is on a hot path" (staffing amend)
    2026-11-24T09:47:21Z  researcher: sonnet at medium -> opus at medium -- "its first pass missed the callers" (staffing amend)
    2026-11-24T09:47:21Z  security: sonnet at high -> opus at high -- "this one touches the installer" (person)
```

It prints the models and efforts your project allows, lowest first, then one
line for each agent: what it is asked for, or that it is left out, and whether
a person chose it. The two quoted reasons are why the story needs the agent,
and why that model and effort. Every change made since follows, with when and
why.

With `--json`, `agents` has a row for each agent: `needed`, `reason`,
`asked_model`, `asked_effort` and `asked_reason`, and `set_by` where
[`sdlc staffing assign`](#sdlc-staffing-assign-agent) wrote the row. `at_gate`
is who to delegate to at the gate, with what each is asked for, and
`amendments` is every change, each with `at`, `agent`, `change`, `reason` and
`by`.

**An arbiter.** Where a gate has
[gone to a reviewer's arbiter](the-loop.md#the-arbiter),
`sdlc staffing show --gate` lists the arbiter in the reviewer's place. It is
asked for one step above the reviewer: the next effort in `team.efforts`, or
the next model in `team.models` where the effort is already the highest.

**When it refuses.** Before a staffing is recorded there is nothing to show
([SDLC-E0068](troubleshooting.md#sdlc-e0068)). With the project manager turned
off it exits 0 and says there is no staffing.
[See who is working a story](the-agents.md#see-who-is-working-a-story) reads
the output with you.

## `sdlc staffing set`

Record a story's staffing. The `sdlc:project-manager` agent runs it, once,
when the story has been found ready.

```text
sdlc staffing set [flags]
```

| Flag | What it does |
| --- | --- |
| `--file` | read the staffing from this path instead of standard input |
| `--story` | the story being worked on, which is the only one a staffing is recorded for |

```console
$ sdlc staffing set < staffing.json
AUTH-3  staffing recorded: 13 agents needed, 0 left out
```

The staffing is a JSON document with one row for every enabled agent, the
project manager included, and for each instance of a reviewer the `team`
section gives more than one:

```json
{"agents": [
  {"agent": "perf", "needed": false, "reason": "nothing here is on a hot path"},
  {"agent": "verifier", "needed": true, "reason": "every story is verified",
   "asked_model": "opus", "asked_effort": "high",
   "asked_reason": "the criteria turn on an ordering that is easy to misread"}
]}
```

These are two of its rows. An agent the story needs comes with the model and
effort asked for, and the reason for them. An agent your project turned off
needs no row.

While the project manager is enabled, `sdlc gate analysis pass` waits for a
staffing to be recorded.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| the document leaves an agent out, names one twice or gives no reason, or a staffing is already recorded | [SDLC-E0069](troubleshooting.md#sdlc-e0069) |
| it leaves out an agent that is never left out: one that does a gate's work, the verifier, the code reviewer, or security on a security-sensitive story | [SDLC-E0070](troubleshooting.md#sdlc-e0070) |
| a model or effort is outside the project's lists, or below the agent's lowest | [SDLC-E0071](troubleshooting.md#sdlc-e0071) |
| it disagrees with what a person assigned | [SDLC-E0072](troubleshooting.md#sdlc-e0072) |

[What the project manager cannot leave out](configuration.md#what-the-project-manager-cannot-leave-out)
lists the limits.

## `sdlc staffing amend AGENT`

Change one agent of a recorded staffing, with the reason. The
`sdlc:project-manager` agent runs it when the analysis is recorded, and
whenever an agent's work fell short or the story turned out other than
expected.

```text
sdlc staffing amend AGENT [flags]
```

| Flag | What it does |
| --- | --- |
| `--needed` | the story needs the agent after all; give `--model` and `--effort` with it |
| `--left-out` | the story does not need the agent |
| `--model` | the model to ask for, one of `team.models` |
| `--effort` | the effort to ask for, one of `team.efforts` |
| `--reason` | why, from what happened or what the work is. Required. |
| `--story` | the story being worked on, which is the only one a staffing is amended for |

```console
$ sdlc staffing amend perf --left-out --reason "nothing here is on a hot path"
AUTH-3  staffing amended: perf: needed -> left out

$ sdlc staffing amend researcher --model opus --reason "its first pass missed the callers"
AUTH-3  staffing amended: researcher: sonnet at medium -> opus at medium
```

What changed, when and why is kept, and `sdlc staffing show` and `sdlc log`
print it. AGENT may be an instance, such as `security-2`.

A gate that has passed stays passed when a reviewer is brought in. The
reviewer is waited for from then on: at the next gate it reviews, or at the
commit gate when it was brought in after the code review passed.

**When it refuses.** It is held to everything
[`sdlc staffing set`](#sdlc-staffing-set) is. A reviewer whose latest review
is a block, at a gate that has not passed, cannot be left out
([SDLC-E0073](troubleshooting.md#sdlc-e0073)): the block is answered by
rework, or by you.

## `sdlc team`

Report what happened to an agent of the team. It has one command,
[`failure`](#sdlc-team-failure-agent). The team itself is set in the
[`team`](configuration.md#team) section of your configuration.

## `sdlc team failure AGENT`

Record that a run of an agent failed at the gate the story is at: it returned
nothing usable, or broke off. The loop runs this, and then runs the agent
again while [`team.retries`](configuration.md#team) allows.

```text
sdlc team failure AGENT [flags]
```

| Flag | What it does |
| --- | --- |
| `--reason` | what went wrong with the run, in a sentence. Required. |

```console
$ sdlc team failure researcher --reason "it returned nothing"
AUTH-3  analysis  researcher failed: 1 failure at this gate, and a person is asked at 3; run it again
  "it returned nothing"
```

The count is the agent's at that gate, since you last decided on the story.
With `team.retries` at 2, the third failure hands the story to you. You read
the reasons and decide with [`sdlc approve`](#sdlc-approve).

It records nothing else, and nothing passes because of it: no gate, review,
staffing or freeze changes. Nobody takes the agent's place, and no other agent
is a substitute for it.

With `--json`, it prints `story`, `agent`, `gate` and `failures`, and
`handed_over` where it handed the story to you.
[When an agent failed or is turned off](guides/human-approval-gates.md#when-an-agent-failed-or-is-turned-off)
says what your choices are.

## `sdlc freeze`

Freeze the story's acceptance tests, so that the implementer cannot change
them to make them pass. The loop does this at Gate 3, once the tests are
written and the test reviewer has approved them.

```text
sdlc freeze [flags]
```

`freeze` has no flags of its own.

```console
$ sdlc freeze
AUTH-3  1 test file frozen
  internal/session/session_test.go
  commands.test_files ran over the new and changed test files and failed with status 1, as new tests do; its output is in /home/<user>/src/billing/.sdlc/stories/AUTH-3/freeze-run.log
```

The freeze covers every file that [`paths.tests`](configuration.md#pathstests)
calls a test. From then on an agent is refused any change to them, and only
you can lift the freeze, with [`sdlc unfreeze`](#sdlc-unfreeze).

Before it freezes anything, `freeze` does up to three things.

**It waits for the test reviewer.** Where the story is staffed with a test
reviewer, the freeze waits for that reviewer's approval of the test files as
they are. Until then it refuses:

```console
$ sdlc freeze
sdlc: the freeze waits for the test reviewer's review of the tests as they are

  why  test-reviewer has not reviewed
  fix  delegate to the reviewers this gate expects -- "sdlc review list" shows who is outstanding

  SDLC-E0029  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0029
```

`sdlc review list --gate tests_frozen` shows where the review has got to. A
test changed, added or renamed after the approval makes it stale, and the
reviewer reads the tests again. Whether the freeze waits depends on your team
and the story's staffing:

| The test reviewer is | The freeze |
| --- | --- |
| staffed for the story, or the staffing does not mention it, or the project manager is turned off | waits for its approval |
| left out by the story's staffing, or disabled in [`team`](configuration.md#an-agents-entry) | waits for no review |
| advisory, with `can_block` set to `false` | waits only until it has reviewed |

Where the tests have gone to `test-reviewer-arbiter`, the freeze reads the
[arbiter's](the-loop.md#the-arbiter) review in the test reviewer's place, and
the refusal names it.

**It runs the freeze check.** Where `commands.freeze_check` is set, `sdlc
freeze` runs it from the repository root, with the new or changed test files
in `SDLC_FREEZE_FILES`, one to a line. It takes the freeze only if the check
exits 0 and leaves every test file as it found it. Otherwise the freeze will
refuse, showing the last 40 lines the check printed
([SDLC-E0085](troubleshooting.md#sdlc-e0085)). The test author corrects the
files it names and runs the command again, and no person is needed.
[The freeze check](configuration.md#the-freeze-check) has an example.

**It runs the new tests, and expects them to fail.** A story's first freeze
runs `commands.test_files` over the new and changed test files, from the
repository root, with `SDLC_TEST_FILES` naming a file that lists them. Where
`commands.test_files` is empty it runs the whole of `commands.test`, and where
neither is set it runs nothing.
[The narrow test command](configuration.md#the-narrow-test-command) says what
the command is handed. A test written before its code fails until the code
exists, so the outcome is read the other way round:

| The run | The freeze |
| --- | --- |
| fails | is taken. The last line of the output says so, and `--json` gives the run as `run`. |
| passes | is refused: those tests test nothing yet ([SDLC-E0085](troubleshooting.md#sdlc-e0085)) |
| could not run at all, as when the shell cannot find the program | is refused, and is yours to put right |
| changes a test file | is refused |

Before the run, one line on standard error names the command and the file its
output goes to. Everything the run printed is in
`.sdlc/stories/<ID>/freeze-run.log`, whether the freeze was taken or refused.

> [!WARNING]
> A freeze that was taken proves that the command exited non-zero, and not
> why. A runner that found no tests exits non-zero too, and so does a wrong
> `commands.test_files` that fails whatever the tests hold. The test author's
> own run and the test reviewer are what show each test failing for the
> right reason.

**Adding a test file later.** With
[`freeze.allow_new_test_files`](configuration.md#freezeallow_new_test_files)
on, running `sdlc freeze` again adds the test files written since, provided
none of the frozen ones has changed, and lists what it added. `--json` names
them in `added`. The test reviewer reads the added files with the rest first,
and the freeze check runs again. The new tests are not run a second time.

**When it refuses.**

| It refuses when | Code |
| --- | --- |
| the tests are already frozen | [SDLC-E0022](troubleshooting.md#sdlc-e0022) |
| `paths.tests` names nothing to freeze | [SDLC-E0024](troubleshooting.md#sdlc-e0024) |
| a freeze was taken and has gone missing without being lifted; lift it with `sdlc unfreeze --reason`, then freeze again | [SDLC-E0025](troubleshooting.md#sdlc-e0025) |
| the test reviewer has not approved the tests as they are | [SDLC-E0029](troubleshooting.md#sdlc-e0029) |
| the test reviewer, or its arbiter, blocked | [SDLC-E0030](troubleshooting.md#sdlc-e0030) |
| the freeze check fails, or changes a test file | [SDLC-E0085](troubleshooting.md#sdlc-e0085) |
| the new tests pass, could not be run, or the run changed a test file | [SDLC-E0085](troubleshooting.md#sdlc-e0085) |
| the story's commands are already running | [SDLC-E0006](troubleshooting.md#sdlc-e0006) |

[Gate 3: freeze the tests](the-loop.md#gate-3-freeze-the-tests) says what the
freeze protects.

## `sdlc escalate`

Hand the story to a person, and end the iteration. An agent reaches for this
when it needs a decision it should not make itself.

```text
sdlc escalate <type> [flags]
```

| Flag | What it does |
| --- | --- |
| `--message` | what the person is being asked to decide. Required. |

```console
$ sdlc escalate pre_commit_approval --message "the migration has no down step"
AUTH-3  waiting for a person: pre_commit_approval
  the migration has no down step

The iteration has ended. A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve AUTH-3, or sdlc approve AUTH-3
  sdlc approve AUTH-3 --reject "why"
```

The type is one word for the kind of question, such as `pre_commit_approval`,
`product_decision` or `frozen_test_wrong`, and the message is the question.
The story then waits for you: the session stops rather than carrying on past
the question, `sdlc status` lists the story as waiting, and `sdlc start`
refuses it until you answer with [`sdlc approve`](#sdlc-approve)
([SDLC-E0036](troubleshooting.md#sdlc-e0036)).

**Product decisions.** `product_decision` is the one escalation Gate 2 makes
for a story's [product decisions](the-loop.md#two-kinds-of-decision). It
carries every product question of the story, each with two or three options
and a recommendation, so that you answer them all at once. `spec_unclear` and
`design_decision` are for a product decision too. An implementation decision
is never escalated: the agent makes it and records it with
[`sdlc decide`](#sdlc-decide-id).

A story waiting in its own worktree is held there, so `sdlc start` in the main
checkout passes it over and begins the next story. With worktrees turned off,
nothing else starts in that checkout until you answer, because the waiting
work is still in the tree.

[Every reason the loop stops](guides/human-approval-gates.md#every-reason-the-loop-stops)
lists the kinds of question and what each asks of you.

## `sdlc hook EVENT`

You will not run this one. It is what the plugin calls on every tool call in a
session, and it is listed here so that its name makes sense when you meet it
in a log. [Enforcement](enforcement.md) describes what it holds an agent to.

## Where to go next

- [The loop](the-loop.md): the nine gates these commands record
- [Human approval gates](guides/human-approval-gates.md): answering with
  `sdlc approve`
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md):
  `sdlc stop` and `sdlc start`
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md): reviews and
  findings in a rework round
- [When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md):
  `sdlc unfreeze`
- [Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md):
  `sdlc log` and `sdlc ack`
- [Tracking what a story costs](guides/tracking-what-a-story-costs.md):
  `sdlc cost` and `sdlc trace`
- [Configuration](configuration.md): every setting these commands read
- [Troubleshooting](troubleshooting.md): every `SDLC-E` code and what to do
  about it
