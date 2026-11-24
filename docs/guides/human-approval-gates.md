# Human approval gates

- [Introduction](#introduction)
- [When the loop hands you a story](#when-the-loop-hands-you-a-story)
- [What the loop asks you, and what it decides](#what-the-loop-asks-you-and-what-it-decides)
- [Read the work](#read-the-work)
- [Answer](#answer)
  - [Send the work back](#send-the-work-back)
  - [Where to type your answer](#where-to-type-your-answer)
- [Carry on](#carry-on)
- [Write your decision down](#write-your-decision-down)
- [The pause before the commit](#the-pause-before-the-commit)
  - [Turning the pause on or off](#turning-the-pause-on-or-off)
  - [What your approval covers](#what-your-approval-covers)
- [Every reason the loop stops](#every-reason-the-loop-stops)
  - [When an agent failed or is turned off](#when-an-agent-failed-or-is-turned-off)
- [Where to go next](#where-to-go-next)

## Introduction

The loop does not guess at what you want. When a story leaves a product question
open, when a reviewer and the work cannot agree, or when a risky change is ready
to be committed, the loop escalates: it hands the story to you and stops.

Nothing has gone wrong when that happens. Everything done so far is kept, and
the story waits for your answer for as long as you need.

This page takes you from that stop back to a running story: what the loop
printed, where the work is, how to answer and where to type the answer, and how
the story carries on. After that come the pause before the commit, which is the
one stop you choose to have, and a table of every reason the loop can stop.

## When the loop hands you a story

A hand-over is usually the last thing a session prints:

```text
AUTH-3  waiting for a person: spec_unclear
  AC-2 says no detail: is an empty body right, or a fixed message?

The iteration has ended. A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve AUTH-3, or sdlc approve AUTH-3
  sdlc approve AUTH-3 --reject "why"
```

The first line names the story and the kind of question, here `spec_unclear`.
The line under it is the question itself.
[Every reason the loop stops](#every-reason-the-loop-stops) lists the kinds you
can meet.

If the session is long gone, run `sdlc status` in your project to see what is
waiting:

```console
$ sdlc status
No iteration running.

  backlog  1 awaiting human
  waiting  AUTH-3  spec_unclear: AC-2 says no detail: is an empty body right, or a fixed message?
  held     AUTH-3  in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3"

A person answers first, in Claude Code or in their own terminal: `/sdlc:approve AUTH-3`, or `sdlc approve AUTH-3`, or `sdlc approve AUTH-3 --reject "why"`.
```

Take note of the `held` line. It says where the story is: a worktree of its
own under `.sdlc/worktrees/`, a second checkout that holds this story's work
and nothing else. You read the work there, and you answer there.

Don't worry about the story while it waits. `sdlc`
[refuses to start it](../troubleshooting.md#sdlc-e0036) until you have
answered, and a `/sdlc:next` typed in your project passes it over and begins
the next `ready` story in a worktree of its own.

## What the loop asks you, and what it decides

The loop asks you about a **product decision** and about nothing else. That is a
question about what the story's user sees or can do, about which of two readings
of a criterion is meant, about what is in or out of the story, or about anything
your `## SDLC Contract` reserves to a person. The loop asks once per story:
every product question of the story, each with two or three options and a
recommendation, so that you answer them all in one sitting. The hand-over names
the kind as `product_decision`, or `spec_unclear` and `design_decision` where one
question stopped the work.

Every other question is an **implementation decision**: how an edge case
behaves, whether a suspected bug is real, which of two designs meets the
criteria, what kind of test to write, an internal name. The loop makes it
itself, with the recommendation of the agent that raised it, or an arbiter's
ruling where a reviewer and the work disagree, and it goes on. It never stops
you for one.

You read these afterwards. Each is written down with its options, the one
chosen and why, in `.sdlc/stories/AUTH-3/DECISIONS.md`, in the order made.
`sdlc log` lists each with its story, and they count as unread until your
`sdlc ack` covers the story's commit. If you disagree with one, open the story
and change it, as [Write your decision down](#write-your-decision-down) says.

## Read the work

Before you answer, look at what the story has done so far. Its code is in the
worktree:

```console
$ cd .sdlc/worktrees/AUTH-3
$ git status
$ git diff
```

Nothing is committed before Gate 8, so `git status` lists every file the story
has changed or added.

The documents the gates produced are kept in your project, not in the
worktree. `.sdlc/stories/AUTH-3/` holds the analysis, the plan and every
review, all as text you can read. To see what each reviewer concluded, ask from
inside the worktree:

```console
$ sdlc review list --story AUTH-3
```

## Answer

When you have decided, answer in a terminal of your own, from the story's
worktree:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc approve AUTH-3
AUTH-3  approved (spec_unclear)

A person runs `sdlc start AUTH-3`, or types /sdlc:next in Claude Code, to carry on.
```

Approving means "carry on". For a question, approve once you have settled it
and [written your decision down](#write-your-decision-down). For a story paused
before its commit, approving says that this work may be committed.

Always name the story. With no id, `approve` answers for the story being worked
on, and after a hand-over no story is.

Your answer goes on the story's record, together with the work it was given
for.

### Send the work back

To refuse the work as it stands, answer with `--reject` and say why. The reason
is required:

```console
$ sdlc approve AUTH-3 --reject "the 401 response still carries a message"
```

A rejection puts your reason on the record. It does not move the story back to
an earlier gate, so the loop would pick up exactly where it stopped. When work
has to be done again, say from which gate by recording that gate as failed, in
the same worktree:

```console
$ sdlc gate implementation fail --story AUTH-3 --note "the 401 response still carries a message"
AUTH-3  implementation  fail
  the 401 response still carries a message
  reopened: verification, verifier_review, code_review
```

The loop starts again from that gate with your note on it, and every gate after
it has to pass again. [`sdlc gate`](../commands.md#sdlc-gate-gate-status) lists
the names the gates go by.

### Where to type your answer

A terminal of your own is one outside Claude Code: a window you opened
yourself. `sdlc approve` [refuses to run](../troubleshooting.md#sdlc-e0079)
anywhere Claude Code started it, a `!` line in a session opened from a terminal
included, because there it cannot tell you from the agent.

The hand-over also offers `/sdlc:approve AUTH-3`, without saying where to type
it. A slash command acts on the directory its session was opened in, so type it
in a Claude Code or Claude Desktop session opened in the story's worktree. Typed
in a session opened in your project, it is
[refused](../troubleshooting.md#sdlc-e0050), and the refusal names the worktree
to use.

| Where you are | What to type |
| --- | --- |
| A terminal of your own | `cd .sdlc/worktrees/AUTH-3`, then `sdlc approve AUTH-3` |
| Claude Code or Claude Desktop, in a session opened in `.sdlc/worktrees/AUTH-3` | `/sdlc:approve AUTH-3` |
| Claude Desktop, where the slash command [does not run](../claude-desktop.md#when-a-slash-command-does-not-run) | `!cd .sdlc/worktrees/AUTH-3 && sdlc approve AUTH-3` |

The slash command takes the same arguments as the terminal command, `--reject`
included. The other commands that are yours alone, such as `sdlc unfreeze` and
`sdlc release`, are typed in the same three ways.
[Running the loop from Claude Desktop](../claude-desktop.md#the-commands-that-are-yours)
has more on typing these commands there.

> [!NOTE]
> The answer is yours alone. The hook refuses `sdlc approve` from an agent,
> however it reaches for it, because an agent that could answer would be
> approving its own work.
> [Enforcement](../enforcement.md#approval-is-a-human-decision) says how.

If your project has
[worktrees turned off](../configuration.md#turning-worktrees-off), the story is
in your project itself, and every command on this page is typed there.

## Carry on

To set the story going again, type this in Claude Code, in your project:

```text
/sdlc:next AUTH-3
```

Name the story here too. It is held in its worktree, so a plain `/sdlc:next`
typed in your project passes it over: it begins the next `ready` story, or
tells you there is no story to start. In a session opened in the worktree,
`/sdlc:next` alone is enough.

The loop picks the story up at the first gate that has not passed. Gates
already passed are not worked again, and your answer makes no review stale.

## Write your decision down

Every gate is worked by an agent that never sees your conversation. A decision
you only type into the chat is gone by the next gate, so put it where the next
agent will read it. Where that is depends on what you were asked.

**A question about the story.** Edit the story in your backlog,
`user_stories.json` in your project: tighten the criterion, add a non-goal, or
split the story. The `decisions` list is there for your reasoning, and it
travels with the story:

```json
"decisions": [
  { "at": "2026-09-14", "by": "sam", "text": "An unparseable token gets 401 with an empty body." }
]
```

Keep the story's `id` as it is. The story is found by it, and one that has gone
from the backlog cannot be committed.

**A frozen test that is wrong.** Approving is only the first of three commands
here. [When a frozen test is wrong](when-a-frozen-test-is-wrong.md) has all of
them.

**A rule in your contract, or a setting.** These are yours to change: while a
story is worked, the hook refuses an agent any write to `CLAUDE.md` or
`.sdlc/config.json`, because a rule the agent can edit has stopped applying to
it. Make the change in your project and commit it to trunk. It reaches the
stories you start after that commit, and the waiting story keeps the copy its
worktree was cut from.

> [!WARNING]
> Don't commit a change to anything under `.sdlc/`, to the backlog or to your
> review policy on a story's branch. `sdlc`
> [refuses to land](../troubleshooting.md#sdlc-e0055) a story whose commits
> touch any of them. `CLAUDE.md` is not on that list, but the worktree keeps
> the copy it was cut from, so change the contract on trunk too.

**Work that has to be done again.** Record the gate it goes back to as failed,
with a note that says what to change, as in
[Send the work back](#send-the-work-back).

## The pause before the commit

Most stops are the loop running into a question. One is a stop you ask for: a
story in a risk tier you chose, or whose change touches a path you listed, waits
at Gate 8, so that you see the work before it is committed and can reach trunk.

You will see `pre_commit_approval` as the kind, with the session's own summary
of what to look at:

```text
AUTH-3  waiting for a person: pre_commit_approval
  Expired tokens are rejected before the session store is read. Look at internal/session/session.go.

The iteration has ended. A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve AUTH-3, or sdlc approve AUTH-3
  sdlc approve AUTH-3 --reject "why"
```

By now every gate before the commit has passed: the tests are frozen and green,
and the plan and the diff have been reviewed. [Read the work](#read-the-work)
and [answer](#answer) as you would any other hand-over. Until you approve,
nothing can commit the story. The hook
[refuses the session's `git commit`](../enforcement.md#commit-gate), and `sdlc`
[refuses to pass the commit gate](../troubleshooting.md#sdlc-e0037).

Once you have approved, `/sdlc:next AUTH-3` commits the work, writes the retro
and lands the story.

Your approval is for the work as it stood when you gave it. If anything changes
afterwards, the commit is refused until you approve again. Work you sent back
stays sent back until it changes, and is then handed to you once more.

### Turning the pause on or off

`sdlc init` writes this block into `.sdlc/config.json`:

```json
"human_gates": { "pre_commit_pause_tiers": ["high"], "pre_commit_pause_paths": [], "dor_advocate_check": false }
```

`pre_commit_pause_tiers` lists the risk tiers that wait for you before they are
committed. A story's tier is its `risk_tier` in the backlog, which is `low`,
`medium` or `high`, and a story without one is `low`. An empty list, `[]`,
pauses no story for its tier.

`pre_commit_pause_paths` lists files, as globs from the repository root, such as
`"src/auth/**"`. A story whose change touches one waits for you whatever its
tier. A tier is a word somebody chose when the story was written, and the files
a change touches are what it does, so list the ones that decide who may approve,
commit or enforce in your project. The refusal names each file with the glob it
matched. With both lists empty, no story waits.

To see one particular story before it is committed, give it
`"risk_tier": "high"` when you write it. To see more of them, add their tier to
the list, or list the paths they would touch. Before Gate 8, `sdlc status` says
whether the commit will pause, and why. The other setting in the block has
nothing to do with approvals, and
[Configuration](../configuration.md#human_gates) describes it.

A story that matches neither list is committed without waiting, and `sdlc log`
shows a `no pause` line for it until your `sdlc ack` marks it read. You may
still read the story at any moment, because its worktree is an ordinary
checkout, and
[Stopping and resuming a story](stopping-and-resuming-a-story.md#take-over-from-the-loop)
shows how to change it yourself before the loop commits.

### What your approval covers

Your approval covers the files you were shown. You give it before the story is
committed, when there is no commit to read, so the messages and authors of the
story's commits are fixed afterwards, when the commit gate passes. Nobody asks
you to read them.

Once fixed, they cannot change unseen. For a story in its own worktree that
waits, by its tier or a path, a commit that is amended, reworded, squashed,
reordered or added after the commit gate has to be approved again before the
story lands.
[Troubleshooting](../troubleshooting.md#sdlc-e0037) shows how to read the
commits when that happens.

If trunk moved while the story waited, the landing puts the story onto the new
trunk itself and keeps every commit's author and message. Your approval stands,
so long as the change is line for line the one you approved. A story rebased by
hand is approved again.

When you answer for a story that is already committed, `sdlc approve` also
names the commit your answer is for:

```text
This answer is also for the story's commits as they stand: story/AUTH-3 is at 3f2a9c1d7e5b.
```

If a second sentence follows, saying these are not the commits the story was
held to, then a commit was changed or added since the commit gate, and your
answer has approved it. The same output says how to read those commits, and how
to put the story back if that is not what you meant to approve.

An approval does not cover everything, and it is as well to know where it
stops:

- the first message and author a session gives a commit, which nobody is asked
  to read;
- a story without a worktree, with `git.worktrees` set to false, whose commits
  are not held at all;
- a story whose commit gate printed `no range recorded`. Read that story's
  commits yourself before it lands.

The same Troubleshooting entry has the complete list.

## Every reason the loop stops

The kind of question is the word after "waiting for a person". These are the
ones the loop uses:

| You see | What happened | What it needs from you |
| --- | --- | --- |
| `product_decision` | at Gate 2, the researcher found product questions the specification does not settle, and the loop puts all of them to you at once, each with options and a recommendation | answer each in the story, then approve |
| `spec_unclear`, `design_decision` | an agent met one product question that stopped its work, such as which of two readings of a criterion is meant. At Gate 1, a criterion that describes how the code is built and not a behaviour is `spec_unclear` too | settle it in the story, then approve |
| `analysis_incomplete` | Gate 2 ended without one of its documents stored | read the message, then approve to run the gate again |
| `untestable` | At Gate 3, a criterion cannot be observed from outside | rewrite the criterion, then approve |
| `frozen_test_wrong` | a frozen test contradicts a criterion | follow [When a frozen test is wrong](when-a-frozen-test-is-wrong.md) |
| `freeze_broken` | at Gate 5, an acceptance test was found changed after the freeze | follow [the same guide](when-a-frozen-test-is-wrong.md#when-a-frozen-test-was-changed) |
| `pre_commit_approval` | the story waits for you at Gate 8, by its risk tier or a path its change touches | read the work, then approve or send it back |
| `work_by_hand` | the gate's work belongs to an agent your project turned off | do that gate's work yourself, as described below |
| `review_not_converging` | a reviewer's [arbiter](when-a-reviewer-blocks.md#when-a-reviewer-keeps-blocking) blocked as many times as its reviewer's limit, `loop.max_review_rounds`, 2 unless you change it (a reviewer your project turned off has no arbiter, so its own limit hands over) | follow [When a reviewer blocks](when-a-reviewer-blocks.md#when-the-loop-hands-the-story-to-you) |
| `gate_failing` | the same gate failed `loop.max_rework_rounds` times, 3 unless you change it | read the last note, decide what has to change, then approve |
| `loop_stalled` | the session kept ending its turn in the middle of the story with nothing recorded, more than `loop.max_stop_blocks` times in a row, 3 unless you change it | approve to let the loop carry on |
| `agent_failed` | an agent's run failed more than `team.retries` times at one gate, each reported with `sdlc team failure` | read the reasons, then approve to run the agent again, or reject |

A session may also hand a story over under a word of its own, as it does when a
rule in your contract blocks a gate. The message says what it is asking.

The last four are counted by `sdlc` itself, not by the session, so no session
can talk its way past a limit. Another attempt would meet the same wall, and
your answer is what starts the count again. The limits are in
[`loop`](../configuration.md#loop) and [`team`](../configuration.md#team).

### When an agent failed or is turned off

After an `agent_failed` hand-over, the message lists why each run failed.
Approve to let the loop run the agent again, or reject.

After a `work_by_hand` hand-over, the gate's work is yours, because the agent
that does it is turned off in your project:

1. Run `sdlc approve AUTH-3` in the story's worktree. It names the gate that is
   open.
2. Do that gate's work by hand.
3. Record it there, with `sdlc gate <gate> pass --story AUTH-3`.
4. Type `/sdlc:next AUTH-3` to carry on.

Carrying on before the gate is recorded hands the story to you again at the
same gate.

Approving either of these lets the loop carry on, and it does not answer the
pre-commit question: a story that pauses, by its tier or a path its change
touches, still waits for you before its commit. That holds only for
`agent_failed` and `work_by_hand`. Every other hand-over you approve, at Gate 8
or before it, is also read as your answer to the pre-commit question for the
work as it stands.

## Where to go next

- [When a reviewer blocks](when-a-reviewer-blocks.md): the story came to you
  because a reviewer and the author could not agree
- [When a frozen test is wrong](when-a-frozen-test-is-wrong.md): lifting the
  freeze, which only you can do
- [Stopping and resuming a story](stopping-and-resuming-a-story.md): putting a
  story down and picking it up again
- [`sdlc approve`](../commands.md#sdlc-approve) and
  [`sdlc escalate`](../commands.md#sdlc-escalate): the reference for both
  commands
