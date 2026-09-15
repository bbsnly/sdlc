# Human approval gates

The loop stops rather than guessing. When the specification does not settle
something, or a story is risky enough that a person should see it before trunk
does, the loop hands the story to a person and ends the iteration. This page
shows where that happens, how to answer, and how the story carries on.

## How a hand-over works

A hand-over is one command, run by the session or by `sdlc` itself:

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
session stops instead of carrying on past the question. `sdlc status` says what
is waiting and who has to act:

```console
$ sdlc status
No iteration running.

  backlog  1 awaiting human
  waiting  US-001  spec_unclear: AC-2 contradicts AC-3: which one holds?

A person answers first, in their own terminal: `sdlc approve US-001`, or `sdlc approve US-001 --reject "why"`.
```

Until somebody answers, `sdlc start` refuses the story with
[SDLC-E0036](../troubleshooting.md#sdlc-e0036). It refuses to start anything
else as well, because it picks up a story already under way before a new one.
The waiting work is still in the tree, and a second story started on top of it
would end up in the same commit.

The assistant cannot answer for you. The hook refuses `sdlc approve` from a tool
call, however `sdlc` is reached, because an agent that could answer would be
approving its own work. See
[`approval-is-a-human-decision`](../enforcement.md#approval-is-a-human-decision).

## Where the loop hands a story over

| Where | What happened | What is recorded |
| --- | --- | --- |
| Gate 1 | a criterion describes an implementation, not a behaviour | `dor fail`, then `spec_unclear` |
| Gate 2 | the researcher has open questions, or recommends a split | `analysis fail`, then `spec_unclear` |
| Gate 2 | a document the gate needs was not stored | `analysis_incomplete` |
| Gate 3 | a criterion cannot be observed from outside | `tests_frozen fail`, then `untestable` |
| Gate 5 | an acceptance test changed after the freeze | `freeze_broken` |
| Gates 5 to 7 | the change is bigger than `thresholds.diff_size_cap` | the gate fails, then `story_too_large` |
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

## The pause before the commit

`sdlc init` writes this block into `.sdlc/config.json`:

```json
"human_gates": { "pre_commit_pause_tiers": ["high"], "dor_advocate_check": false }
```

`pre_commit_pause_tiers` names the risk tiers that wait for a person before they
are committed. A story's tier is its `risk_tier` in the backlog, and a story
without one is `low`. Tiers are compared without regard to case, so a story
marked `High` is held by `["high"]`. `[]` turns the pause off.

A story in a paused tier reaches Gate 8 and stops. The runbook hands it over
with `sdlc escalate pre_commit_approval`, and until a person has approved the
work, both `git commit` (refused by the
[`commit-gate`](../enforcement.md#commit-gate) rule) and
`sdlc gate commit pass` ([SDLC-E0037](../troubleshooting.md#sdlc-e0037)) refuse.

An approval is bound to the work it was given for. Change anything after it and
it needs approving again. Work you sent back stays sent back until it changes,
and once it has changed it has to be handed over again. That is what makes the
approval worth something: it answers for the commit, not for a version of the
story that no longer exists.

To look at a story that would not otherwise be held, give it `risk_tier: high`
when you write it, or add its tier to `pre_commit_pause_tiers`.

`dor_advocate_check` is a different kind of check. With it on, the
`sdlc:human-advocate` agent reads the story at Gate 1 as the person it is for
would, and `sdlc gate dor pass` waits until that review is in.
`sdlc review list --gate dor` lists it. The review is advisory, so a block from
it does not stop the gate, and it goes stale if the story changes after it.

## Answer

Read what the story is waiting on. `sdlc status` shows the question, and the
work is in the tree for you to look at. Then answer in your own terminal:

```console
$ sdlc approve US-001
US-001  approved (spec_unclear)

Run `sdlc start US-001`, or /sdlc:next, to carry on.
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

## Record your decision

Put the decision where the next agent will read it, not only in the chat. Every
gate is done by an agent starting from a fresh context, and that agent never
sees the conversation.

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
and `sdlc unfreeze --reason "..."`: the escalation ended the iteration, and the
freeze is lifted on the story being worked on. The reason is your decision on
the record. See
[When a frozen test is wrong](when-a-frozen-test-is-wrong.md).

**A contract rule or a setting.** While a story is being worked on, the hook
refuses the assistant any write to `CLAUDE.md` or `.sdlc/config.json`: a rule
the assistant can edit is a rule that stopped applying to it. The hand-over has
ended the iteration, so make the change yourself, then answer with
`sdlc approve`.

**The gate itself.** The note on a gate outcome is a one-line record of why.
When you record an outcome yourself, say what was decided:

```console
$ sdlc gate analysis fail --note "AC-2 split into US-004; zero totals invalid"
```

## Resume

Run `/sdlc:next`. With no story active, it runs `sdlc start`, which picks up the
story already under way, reports `"resume": true`, and goes to `next_gate`: the
first gate that has not passed. You can also run `sdlc start US-001` yourself
first, as `sdlc approve` suggests.

The hand-over and the answer do not make any review stale. `sdlc` writes only
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

## Looking before a commit that is not paused

The pause is the way to see a story before it lands. Without it, you can still
put a story down at the right moment. Once `sdlc status` shows `commit` as the
next gate, every gate before it has passed:

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
reviewers see your change before the loop commits it.

## Where to go next

- [Configuration: `human_gates`](../configuration.md#human_gates)
- [`sdlc escalate`](../commands.md#sdlc-escalate) and
  [`sdlc approve`](../commands.md#sdlc-approve)
- [Stopping and resuming a story](stopping-and-resuming-a-story.md)
- [When a reviewer blocks](when-a-reviewer-blocks.md)
- [Getting started](../getting-started.md#write-a-story): the story format
