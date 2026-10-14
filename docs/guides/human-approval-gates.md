# Human approval gates

- [Introduction](#introduction)
- [How a hand-over works](#how-a-hand-over-works)
- [Answer](#answer)
- [Resume](#resume)
  - [Reviews after an answer](#reviews-after-an-answer)
- [The pause before the commit](#the-pause-before-the-commit)
  - [Turning the pause on or off](#turning-the-pause-on-or-off)
  - [What your approval covers](#what-your-approval-covers)
  - [Checking the story at Gate 1](#checking-the-story-at-gate-1)
- [Record your decision](#record-your-decision)
- [Where the loop hands a story over](#where-the-loop-hands-a-story-over)
- [Looking before a commit that is not paused](#looking-before-a-commit-that-is-not-paused)
- [Where to go next](#where-to-go-next)

## Introduction

Sooner or later the loop reaches a question it should not answer for itself.
Perhaps the specification does not settle something, or the story is risky
enough that a person should see it before trunk does. When that happens, the
loop stops rather than guessing: it hands the story to a person and ends the
iteration.

This page shows what you see when the loop stops for you, what to type to
answer, and how the story carries on afterwards. It then shows how to turn the
optional stops on or off, and lists every place the loop can hand a story over.

## How a hand-over works

A hand-over is one command, run by the session or by `sdlc` itself. You will
usually meet it as the last thing a session prints:

```console
$ sdlc escalate spec_unclear --message "AC-2 contradicts AC-3: which one holds?"
US-001  waiting for a person: spec_unclear
  AC-2 contradicts AC-3: which one holds?

The iteration has ended. A person reads the work and answers in their own terminal:
  sdlc approve US-001
  sdlc approve US-001 --reject "why"
```

The question goes on the story's gate record, bound to the work as it stands.
The story's status becomes `awaiting_human`, and the iteration ends, so the
session stops instead of carrying on past the question.

If you come back later and want to know what is waiting, `sdlc status` says
what it is and who has to act:

```console
$ sdlc status
No iteration running.

  backlog  1 awaiting human
  waiting  US-001  spec_unclear: AC-2 contradicts AC-3: which one holds?

A person answers first, in their own terminal: `sdlc approve US-001`, or `sdlc approve US-001 --reject "why"`.
```

Until somebody answers, `sdlc start` refuses the story with
[SDLC-E0036](../troubleshooting.md#sdlc-e0036). It refuses to start anything
else in that checkout as well, because it picks up a story already under way
before a new one. The waiting work is still in the tree, and a second story
started on top of it would end up in the same commit.

With [`git.worktrees`](../configuration.md#git) on, the default, the story waits
in its own worktree, `.sdlc/worktrees/<ID>`, and `sdlc start` in the main
checkout passes it over and begins the next story in a worktree of its own.

> [!NOTE]
> The assistant cannot answer for you. The hook refuses `sdlc approve` from a
> tool call, however `sdlc` is reached, because an agent that could answer would
> be approving its own work. A reviewer's `approve` verdict, given with
> `sdlc review add`, is a different command and is not refused. See
> [`approval-is-a-human-decision`](../enforcement.md#approval-is-a-human-decision).

## Answer

When you are ready to reply to a hand-over, read what the story is waiting on.
`sdlc status` shows the question, and the work is in the tree for you to look
at. Then answer in your own terminal, in the
checkout that holds the story: with `git.worktrees` on, its worktree,
`.sdlc/worktrees/US-001`. In any other checkout `sdlc approve` is refused with
[SDLC-E0050](../troubleshooting.md#sdlc-e0050), which names the one to use.

```console
$ sdlc approve US-001
US-001  approved (spec_unclear)

A person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.
```

To send the work back instead, say why. The reason is required, because
whoever picks the story up next needs to know what to change:

```console
$ sdlc approve US-001 --reject "refunds skip the audit log"
```

Either answer goes on the record with the work it was given for, and moves the
story back to `in_progress`. Name the story: with no id, `approve` answers for
the story being worked on, and a hand-over has already ended that iteration. An
answer for a story that is not waiting is refused with
[SDLC-E0035](../troubleshooting.md#sdlc-e0035).

## Resume

To carry on, run `/sdlc:next`. With no story active, it runs `sdlc start`, which
picks up the story already under way and goes to the first gate that has not
passed. (With `--json` it reports `"resume": true` and names that gate as
`next_gate`.) You can also run `sdlc start US-001` yourself first, as
`sdlc approve` suggests.

### Reviews after an answer

In addition, you may wish to know whether an answer makes any review stale. The
hand-over and the answer do not. `sdlc` writes only
the story's `status` and `updated` fields in the backlog, and those are left out
of what a review is stamped with. Anything else you change is not. The human
advocate's Gate 1 review is stamped with the story's entry, and the verifier's
and the code reviewer's with the whole working tree, backlog included. Edit a
criterion or add a decision after those reviews, and they are stale:

```console
$ sdlc review list --gate code_review
  code_review     code-reviewer   blocking  approve (round 3, stale -- what was reviewed has changed since)
  code_review     security        advisory  approve (round 2, stale -- what was reviewed has changed since)
  code_review     perf            on budget approve (round 2, stale -- what was reviewed has changed since)
  code_review     human-advocate  advisory  approve (round 2, stale -- what was reviewed has changed since)
```

Those reviewers look again before the gate, or the commit, will pass.

## The pause before the commit

Most stops come from the loop running into a question. One is different: you
can ask the loop to stop before it commits a risky story, so that a person sees
the work before it lands on trunk. That stop is optional, and it is controlled
by one setting.

### Turning the pause on or off

`sdlc init` writes this block into `.sdlc/config.json`:

```json
"human_gates": { "pre_commit_pause_tiers": ["high"], "dor_advocate_check": false }
```

`pre_commit_pause_tiers` names the risk tiers that wait for a person before they
are committed. A story's tier is its `risk_tier` in the backlog, and a story
without one is `low`. Tiers are compared without regard to case, so a story
marked `High` is held by `["high"]`. `[]` turns the pause off.

To look at a story that would not otherwise be held, give it `risk_tier: high`
when you write it, or add its tier to `pre_commit_pause_tiers`.

A story in a paused tier reaches Gate 8 and stops. The runbook hands it over
with `sdlc escalate pre_commit_approval`, and until a person has approved the
work, both `git commit` (refused by the
[`commit-gate`](../enforcement.md#commit-gate) rule) and
`sdlc gate commit pass` ([SDLC-E0037](../troubleshooting.md#sdlc-e0037)) refuse.
An approval stands when `sdlc stop` itself puts the story onto a trunk that
moved and the result is the approved change line for line. A story rebased by
hand is approved again. So is one whose reviews were recorded again after its
commit: there the approval is of the tree as it stood when you gave it, so run
`sdlc stop` after approving and before trunk moves again, or the stop puts the
story onto the new trunk and asks for the approval once more.

An approval is bound to the work it was given for. Change anything after it and
it needs approving again. Work you sent back stays sent back until it changes,
and once it has changed it has to be handed over again. That is what makes the
approval worth something: it answers for the files that are committed, not for
a version of the story that no longer exists.

### What your approval covers

Your approval covers the files you were shown. You give it before the story is
committed, when there is no commit to read, so the messages and authors of the
story's commits are fixed afterwards, when the commit gate passes. Nobody asks
you to read the commits' messages and authors: they land as the session wrote
them unless they change.

For a story in its own worktree, in a tier that pauses, they cannot change
unseen. A commit amended, reworded, squashed, reordered or added after the
commit gate is approved again before the story lands: `sdlc stop` refuses it,
and so does `sdlc gate commit pass` run again
([SDLC-E0037](../troubleshooting.md#sdlc-e0037), which says how to read the
commits and what each refusal means). When `sdlc stop` itself puts the story
onto a trunk that moved, it keeps every commit's author and message, and asks
you nothing.

Once the story has been committed, your answer also names a commit: the one the
story's branch is at when you give it. `sdlc approve` prints it:

```text
This answer is also for the story's commits as they stand: story/US-001 is at 3f2a9c1d7e5b.
```

Where a second sentence follows, saying they are not the commits the story was
held to, a commit was changed or added since the commit gate and your answer
has just approved it. Read the commits with the command it prints. If that is
not what you meant, and git still has the commit the gate recorded, the
`git reset --hard` it prints puts the story back on that commit before anyone
runs `sdlc stop`; the reset drops every commit above it and any uncommitted
change. Answer in the story's worktree with its branch checked out: with
another branch there, or none, the answer names no commit and says so.

What is not covered:

- the first message and author a session gives a commit;
- commits a session puts on top of the commit your answer names: the one commit
  gate pass after your answer fixes them with their words unread, as a rework
  round does;
- commits above the recorded one that change the root `CODEMAP.md` alone, any
  number of them, with any author and message;
- a story without a worktree ([`git.worktrees`](../configuration.md#git) set
  to false);
- a story whose first commit gate pass recorded no range, which a session can
  bring about. The pass then prints `no range recorded`: read that story's
  commits yourself before the stop;
- a tier that pauses only in the main checkout's configuration or trunk's, and
  not in the worktree's own: the stop holds the story to its commits, and the
  commit gate run again does not;
- a commit gate recorded by an older `sdlc`.

### Checking the story at Gate 1

`dor_advocate_check` is a different kind of check. With it on, the
`sdlc:human-advocate` agent reads the story at Gate 1 as the person it is for
would, and `sdlc gate dor pass` waits until that review is in.
`sdlc review list --gate dor` lists it. The review is advisory, so a block from
it does not stop the gate, and it goes stale if the story changes after it.

## Record your decision

Put the decision where the next agent will read it, not only in the chat. Every
gate is done by an agent starting from a fresh context, and that agent never
sees the conversation. Where the decision goes depends on what the loop asked.

**A question about the story.** Edit the story in the backlog: tighten the
criterion, add a non-goal, split it. The story schema has a `decisions` list
for the reasoning, and it travels with the story:

```json
"decisions": [
  { "at": "2026-09-14", "by": "sam", "text": "Zero is invalid; negative totals are credit notes, out of scope." }
]
```

Keep the story's `id` as it is. `sdlc approve` finds the story by it, and a story
that has gone from the backlog cannot be committed: `sdlc status` reports it as
`not in the backlog`.

**A frozen test that is wrong.** Run `sdlc approve <ID>`, then `sdlc start <ID>`
and `sdlc unfreeze --reason "..."`, all three in the story's worktree with
`git.worktrees` on: the escalation ended the iteration, and the freeze is lifted
on the story being worked on in the checkout the command runs in. The reason is
your decision on the record. Like `sdlc approve`, `sdlc unfreeze` is refused to
the assistant: only a person runs it, in their own terminal
([`unfreeze-is-a-human-decision`](../enforcement.md#unfreeze-is-a-human-decision)).
See
[When a frozen test is wrong](when-a-frozen-test-is-wrong.md).

**A contract rule or a setting.** While a story is being worked on, the hook
refuses the assistant any write to `CLAUDE.md` or `.sdlc/config.json`: a rule
the assistant can edit is a rule that stopped applying to it. The hand-over has
ended the iteration, so make the change yourself, then answer with
`sdlc approve`. A story in its own worktree reads that worktree's copies, so
make the change there for it to reach the story, and commit it to trunk for the
stories after it.

**The gate itself.** The note on a gate outcome is a one-line record of why.
When you record an outcome yourself, say what was decided:

```console
$ sdlc gate analysis fail --note "AC-2 split into US-004; zero totals invalid"
```

## Where the loop hands a story over

If you would like to know every place the loop can stop for you, this table
lists them. Each row says where it happens, what happened, and what is recorded
on the story.

| Where | What happened | What is recorded |
| --- | --- | --- |
| Gate 1 | a criterion describes an implementation, not a behaviour | `dor fail`, then `spec_unclear` |
| Gate 2 | the researcher has open questions | `analysis fail`, then `spec_unclear` |
| Gate 2 | a document the gate needs was not stored | `analysis_incomplete` |
| Gate 3 | a criterion cannot be observed from outside | `tests_frozen fail`, then `untestable` |
| Gate 5 | an acceptance test changed after the freeze | `freeze_broken` |
| any gate | a frozen test contradicts a criterion | `frozen_test_wrong` |
| any gate | a rule in the `## SDLC Contract` blocks a gate | a hand-over that names the rule |
| Gate 8 | the story's risk tier waits for a person | `pre_commit_approval` |
| any gate | the same gate failed `loop.max_rework_rounds` times | `gate_failing`, by `sdlc gate` |
| a review | the same reviewer blocked `loop.max_review_rounds` times | `review_not_converging`, by `sdlc review add` |
| any time | the `Stop` hook sent back `loop.max_stop_blocks` stops in a row with nothing recorded, and the session stopped again | `loop_stalled`, by the `Stop` hook |

The rows down to Gate 8 are written into the `/sdlc:next` runbook. The last
three are done by `sdlc` itself, so no session can talk its way past them:
another attempt would meet the same wall, and a person deciding to carry on is
what starts the count again. The limits are in
[`loop`](../configuration.md#loop).

## Looking before a commit that is not paused

The pause is the way to see a story before it lands. Without it, you can still
put a story down at the right moment. Once `sdlc status` shows `commit` as the
next gate, every gate before it has passed. In the story's worktree, with
`git.worktrees` on, where `git diff` shows the story's work:

```console
$ sdlc stop
$ git diff
$ sdlc start
```

Then run `/sdlc:next` to let the loop commit. If you change the code while the
story is down, the commit is refused, by the hook before `git commit` and by
`sdlc gate commit pass` after it, naming the reviews your change made stale.
Send the change back through the gates that check it by recording the first of
them as failed:

```console
$ sdlc gate verification fail --note "changed the refund rounding after review"
```

Recording a gate reopens every gate after it, so the verifier and the code
reviewers see your change before the loop commits it. The command names the
gates it reopened.

## Where to go next

- [Configuration: `human_gates`](../configuration.md#human_gates)
- [`sdlc escalate`](../commands.md#sdlc-escalate) and
  [`sdlc approve`](../commands.md#sdlc-approve)
- [Stopping and resuming a story](stopping-and-resuming-a-story.md)
- [When a reviewer blocks](when-a-reviewer-blocks.md)
- [Getting started](../getting-started.md#write-a-story): the story format
