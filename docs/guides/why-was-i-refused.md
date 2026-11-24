# Why was I refused?

- [Introduction](#introduction)
- [Read the refusal](#read-the-refusal)
  - [In your terminal](#in-your-terminal)
  - [In the session](#in-the-session)
- [Refusals you are likely to meet](#refusals-you-are-likely-to-meet)
  - [From the command](#from-the-command)
  - [From the hook](#from-the-hook)
  - [Decisions that are yours](#decisions-that-are-yours)
  - [When a rule is in your way](#when-a-rule-is-in-your-way)
- [When nothing was refused](#when-nothing-was-refused)
  - [Check with `sdlc doctor`](#check-with-sdlc-doctor)
  - [Warnings that begin `sdlc:`](#warnings-that-begin-sdlc)
- [See every decision](#see-every-decision)
- [Turn enforcement off](#turn-enforcement-off)
- [Where to go next](#where-to-go-next)

## Introduction

Sooner or later something is refused: a command you typed, or something the
agent tried while it worked a story. Don't worry. A refusal is `sdlc` doing its
job, and the message that comes with it nearly always names the way through.

Refusals come from two places. The `sdlc` command refuses in your terminal
when a step is out of order or in the wrong place. And while a story is being
worked on, a hook looks at every file the agent writes and every shell command
it runs, and refuses the ones the loop's rules do not allow.

This page shows how to read both kinds, and lists the refusals you are most
likely to meet with what to do about each. It ends with what to check when you
expected a refusal and nothing happened.

## Read the refusal

Both kinds tell you the same three things: what happened, why, and what to do
instead. They only look different.

### In your terminal

A refusal from the command has a `why` line, a `fix` line and a code. This is
what `sdlc start` prints when the one story in the backlog is already being
worked on in another checkout:

```console
$ sdlc start
sdlc: there is no story to start

  why  of 1 story, 0 done and 0 blocked; AUTH-3 is held by another checkout of this repository
  fix  add the next story, or unblock one — "sdlc story list" shows each story's status and what any blocked one is waiting on

  SDLC-E0010  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0010
```

Read the `fix` line before you try anything else. It names the way through,
and that is usually one command. The last line is the error's code, an `SDLC-E`
number, with a link to its entry in [Troubleshooting](../troubleshooting.md).
Every code has an entry there, which says what causes it and how to get past
it.

### In the session

When the hook refuses something an agent tried, the agent is handed a reason,
and you see it in the session. Here the agent that implements a story has tried
to write a test file:

```text
the agent that implements a story does not write its tests -- that is the separation the loop is made of. Instead: make the existing tests pass; if they are wrong, say so rather than changing them [implementer-does-not-write-tests]
```

A reason always has three parts:

1. **What the rule is**, in one clause: the agent that implements a story does
   not write its tests.
2. **`Instead:`**, the way to get the same thing done inside the rules.
3. **The rule's name**, in square brackets at the end.

The agent reads the `Instead:` part too, and most of the time it carries on by
itself. When you want to know more, the rule's name is a heading in
[Enforcement](../enforcement.md):
[`implementer-does-not-write-tests`](../enforcement.md#implementer-does-not-write-tests)
says what the rule refuses and why it exists.

A few reasons also carry an `SDLC-E` code in round brackets, just before the
rule's name. Look that up in Troubleshooting like any other code.

> [!NOTE]
> Only one rule is named, however many a call would break. The hook stops at
> the first rule that refuses, so getting past one refusal can uncover the
> next.

## Refusals you are likely to meet

A handful of refusals account for nearly all the ones you will see. Find yours
by its opening words. For any other, follow the code to
[Troubleshooting](../troubleshooting.md) or the rule's name to
[Enforcement](../enforcement.md).

### From the command

These are printed in your own terminal, or shown to the agent when it runs
`sdlc`:

| It says | What happened | What to do |
| --- | --- | --- |
| `there is no story being worked on` | You ran the command in a checkout where no story is under way. Each story is worked in a worktree of its own, a second checkout under `.sdlc/worktrees/`. ([SDLC-E0011](../troubleshooting.md#sdlc-e0011)) | Run `sdlc status`, which names the worktree that holds the story. `cd` there and run the command again. |
| `"AUTH-3" is held by another checkout of this repository` | You named a story that is under way somewhere else. ([SDLC-E0050](../troubleshooting.md#sdlc-e0050)) | The `why` line names the checkout that holds it. Run the command there. |
| `there is no story to start` | Nothing in the backlog is `ready`, or every ready story is already under way. ([SDLC-E0010](../troubleshooting.md#sdlc-e0010)) | Run `sdlc story list` to see what holds each story back. |
| `the story's worktree could not be made` | Most often, the setup `sdlc init` wrote was never committed. ([SDLC-E0053](../troubleshooting.md#sdlc-e0053)) | Commit `.sdlc/config.json` and `CLAUDE.md` to trunk, then start again. |
| `commands.smoke failed in the story's new worktree` | Trunk does not build in a fresh checkout, often because its dependencies are not installed there. ([SDLC-E0041](../troubleshooting.md#sdlc-e0041)) | Fix trunk, or have the smoke command install what it needs. [Tuning commands for your stack](tuning-commands-for-your-stack.md#give-a-new-worktree-what-it-needs) shows how. |
| `design_review is blocked by a reviewer` | The agent tried to pass a gate while a reviewer's block stands. ([SDLC-E0030](../troubleshooting.md#sdlc-e0030)) | Usually nothing: the agent reworks and the reviewer looks again. [When a reviewer blocks](when-a-reviewer-blocks.md) covers the rest. |

### From the hook

These appear in the session while a story is being worked on. Most of them
need nothing from you: the agent reads the reason and takes the other route.

| The reason begins | What happened | What to do |
| --- | --- | --- |
| `this story has not been through the gates that come before committing` | The agent tried to commit before the gates that come before the commit had passed. ([`commit-gate`](../enforcement.md#commit-gate)) | Nothing. `sdlc status` shows which gate is next. |
| `the agent that implements a story does not write its tests` | The implementer tried to write or change a test. ([`implementer-does-not-write-tests`](../enforcement.md#implementer-does-not-write-tests)) | Nothing, unless the test itself is wrong. |
| `… is a frozen acceptance test` | An agent tried to change a test after the freeze. ([`frozen-test-is-not-edited`](../enforcement.md#frozen-test-is-not-edited)) | Nothing, unless the test itself is wrong. Then [When a frozen test is wrong](when-a-frozen-test-is-wrong.md) is the way through. |
| `… would be a new test file added after the freeze` | An agent tried to add a test file once the tests were frozen. ([`no-new-test-after-the-freeze`](../enforcement.md#no-new-test-after-the-freeze)) | If a case really is missing, the same guide shows how to lift the freeze and take it again. |
| `… is protected while a story is being worked on` | An agent tried to edit a file that is yours, such as `.sdlc/config.json` or `CLAUDE.md`. ([`write-protected-path`](../enforcement.md#write-protected-path)) | Edit it yourself, by hand, between stories. |
| `… is the backlog` | An agent tried to edit the backlog. ([`backlog-is-not-edited`](../enforcement.md#backlog-is-not-edited)) | Change a story's criteria or risk tier yourself, between stories. |
| `neither the main conversation nor an agent outside the loop writes code or tests during an iteration` | You asked the session for a quick change mid-story, and it tried to make the change itself. ([`orchestrator-delegates`](../enforcement.md#orchestrator-delegates)) | Let the story's own agents make it, or [put the story down](stopping-and-resuming-a-story.md) first. |
| `` `git push` writes refs `` | The agent tried to push, or to move a branch. ([`branch-through-the-tool`](../enforcement.md#branch-through-the-tool)) | Push from your own terminal. The loop never pushes. |
| `the sdlc hook ran out of time` | The hook could not finish checking the call in time, so the call was not run. No rule was broken. ([`not-checked-in-time`](../enforcement.md#not-checked-in-time)) | The reason says what to try: a long command again in shorter parts, a file change again as it is. |

### Decisions that are yours

A few commands are a person's decision, and the hook refuses them from any
agent. You will meet these when you ask the assistant to run one for you:

| The reason begins | The command | Type it yourself |
| --- | --- | --- |
| `an approval is a person's answer` | `sdlc approve` | `/sdlc:approve` in Claude Code, or `sdlc approve` in your terminal |
| `lifting the test freeze is the move an agent would make` | `sdlc unfreeze` | `/sdlc:unfreeze`, or `sdlc unfreeze --reason "..."` |
| `acknowledging says a person has read what landed on trunk` | `sdlc ack` | `/sdlc:ack`, or `sdlc ack --through <commit>` |
| `ending the iteration before the story's gates have passed` | `sdlc stop`, before the story is finished | `sdlc stop` in your terminal |

The reason is the same each time. An agent that could approve its own work,
lift the freeze on its own tests or end the story it is held to would not be
held to anything. Most of these are refused in every session of the project,
not only the one working a story.
[A person's decisions](../enforcement.md#a-persons-decisions-refused-in-every-session)
lists them all, and [Human approval gates](human-approval-gates.md) shows how
to answer when the loop stops for you.

### When a rule is in your way

If a rule is wrong for your project, say which one and why. Some rules have a
setting behind them, which you change between stories. For the others, open an
issue. Don't route around a rule, and don't ask the agent to: a rule that is
quietly worked around stops protecting the next story as well.

## When nothing was refused

Sometimes the opposite happens: you expected the hook to refuse something, and
the call went through. The hook does not watch every session. It holds one
session to one story, and it refuses nothing unless all of these are true:

1. The project is set up for the loop, with a `.sdlc/config.json`.
2. A story is being worked on.
3. The call comes from the session working that story, the one where
   `/sdlc:next` was typed, or from one of its agents.
4. Enforcement has not been [turned off](#turn-enforcement-off).

If one of those is missing, the hook is working as designed. A second Claude
Code session open in the same repository, for example, is left alone.
[When any of it applies](../enforcement.md#when-any-of-it-applies) has the
detail.

### Check with `sdlc doctor`

When all four should hold and a call still went through, run `sdlc doctor` in
the story's worktree. It changes nothing, and it is the fastest way to find
what is wrong:

```console
$ sdlc doctor
sdlc doctor

  ok       git repository       /home/<user>/src/billing/.sdlc/worktrees/AUTH-3
  ok       git command          found on PATH
  ok       configuration        .sdlc/config.json  (Go project)
                                paths.conflict_markers_intended (this checkout's copy; "sdlc stop" reads trunk's): none
  ok       backlog              /home/<user>/src/billing/user_stories.json  (1 story, one runnable)
  ok       loop state           the iteration, every gate record, the test freeze and the commit acknowledged all read; AUTH-3 held in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3", this checkout; no Claude Code session is working AUTH-3, so nothing is enforced until a person picks it up by typing /sdlc:next in Claude Code
  ok       review policy        REVIEW.md
  ok       project contract     CLAUDE.md carries the "## SDLC Contract" section
  ok       commands             build, coverage, fmt, fmt_check, fmt_file, lint, smoke, test, test_files
  ok       freeze check         commands.freeze_check is not set, so sdlc freeze runs no check
  ok       sdlc on PATH         /home/<user>/.local/bin/sdlc

Everything checks out.
```

Here the answer is on the `loop state` line. The story was started from a
terminal, so no Claude Code session holds it yet, and nothing is enforced until
somebody types `/sdlc:next`. `sdlc status` says the same.

Where `doctor` finds a problem, the line starts with `problem`, the fix is
printed beneath it, and the command exits non-zero. Look at the last line as
well: it names the `sdlc` your terminal finds, which should be the one you
meant to install.

### Warnings that begin `sdlc:`

The hook never blocks your session because of a fault of its own. When it
cannot do its job, it lets the call through, and tells the session working the
story with a message that starts with `sdlc:`, such as this one:

```text
sdlc: .sdlc/config.json could not be read, so tests are being recognised by the default patterns until it can be. Run `sdlc doctor` to see why.
```

Take these seriously. Each one means a rule you are relying on is off, or
running in a reduced form, until you fix what it names.
[Warnings the hook prints](../troubleshooting.md#warnings-the-hook-prints)
lists every message with what it switches off.

One of them means nothing is enforced at all. It begins:

```text
sdlc: the sdlc binary was not found, so nothing is being enforced.
```

The rest of the message says where the plugin looked and how to put it right.
The plugin is installed but cannot find the `sdlc` command, so every call goes
through. Install the command by any route in
[Installation](../installation.md), or type `/sdlc:next`, which offers to
install it.

> [!WARNING]
> A desktop app keeps the `PATH` it was started with. If `sdlc doctor` passes
> in a new terminal and the app still says the binary was not found, quit the
> app fully and open it again.
> [Troubleshooting](../troubleshooting.md#the-sdlc-binary-was-not-found-so-nothing-is-being-enforced)
> covers the other causes.

## See every decision

A hook that allows a call says nothing about it, which makes "why did nothing
happen?" hard to answer. To see every decision the hook makes, start Claude
Code with `SDLC_DEBUG_FILE` set to a file of your choosing:

```console
$ SDLC_DEBUG_FILE=/tmp/sdlc-hook.log claude
```

Each tool call then adds a few lines to that file. Let's take a look at the
lines for one shell command that was refused:

```text
time=2026-11-23T20:46:14.710+01:00 level=DEBUG msg="hook considering a command" agent="" story=AUTH-3 commit_ready=false why="dor has not passed"
time=2026-11-23T20:46:14.822+01:00 level=DEBUG msg="hook considered a tool" candidate=touch via=released
time=2026-11-23T20:46:14.826+01:00 level=DEBUG msg="hook considered a tool" candidate=touch via=decoded
time=2026-11-23T20:46:14.829+01:00 level=DEBUG msg="hook considered a tool" candidate=cp via=decoded
time=2026-11-23T20:46:14.862+01:00 level=DEBUG msg="hook decision" event=PreToolUse considered=true allowed=false rule=loop-state-through-the-tool via=decoded
```

The last line is the decision, and it is the one to read first:

- **`allowed`** says whether the call went through.
- **`rule`** names the rule that refused it, and is empty for a call that was
  allowed. Where the refusal carries an `SDLC-E` code, a `code` follows it.
- **`considered`** is `false` when the hook never got as far as the rules, for
  instance because the session is not the one working the story.
- **`agent`**, on the first line, is who made the call. An empty one is the
  main conversation.

For a shell command, a `hook considered a tool` line names each program in it
that a rule looked at, as `candidate`. Only the program's plain name is
written, never the rest of the command, so the log holds nothing the command
said.

`via` is the reading of the command the program was found in. The hook reads a
command more than one way, because a shell does:

| `via` | The command as |
| --- | --- |
| `released` | it is written, which is the hook's first reading |
| `shell` | the shell that runs it reads it, where that differs, as it does around a PowerShell here-string or backtick |
| `decoded` | a POSIX shell reads it once each `$'...'` is decoded and each backslash taken out |
| `braces` | a POSIX shell reads it once each brace list, such as `{a,b}`, is written out |

A program found under `decoded` or `braces` and nowhere else was hidden in the
command as typed. That is often the answer to why a command that looks harmless
was refused: in the example, `cp` was written with a backslash in the middle of
its name. On the decision line, `via` is the reading that refused the call.

> [!NOTE]
> To look back at a story without a log, run `sdlc trace <ID>`. Among other
> things, it counts the story's refusals by rule and by agent.
> [`sdlc trace`](../commands.md#sdlc-trace) describes the whole report.

## Turn enforcement off

If you are debugging the loop itself, you may switch enforcement off for one
session. Start Claude Code with `SDLC_ENFORCE` set to `0`:

```console
$ SDLC_ENFORCE=0 claude
```

It must be exactly `0`. With it set, the hook allows every call and says
nothing about it, so remember that you set it.

Only the person who starts the session can do this. An agent that tries to set
it from inside a session is refused, under
[`enforcement-stays-on`](../enforcement.md#enforcement-stays-on). If you find
yourself reaching for it during ordinary work, the rule in your way is probably
wrong for your project, and [worth raising](#when-a-rule-is-in-your-way).

## Where to go next

- [Enforcement](../enforcement.md): every rule, and why it exists
- [Troubleshooting](../troubleshooting.md): every error code, and the hook's
  warnings
- [When a frozen test is wrong](when-a-frozen-test-is-wrong.md): lifting the
  freeze, on the record
- [Human approval gates](human-approval-gates.md): the decisions the loop
  keeps for you
- [Stopping and resuming a story](stopping-and-resuming-a-story.md): putting a
  story down and picking it up again
