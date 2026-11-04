# When a reviewer blocks

- [Introduction](#introduction)
- [What a block looks like](#what-a-block-looks-like)
- [Rework](#rework)
  - [The reviewer looks again](#the-reviewer-looks-again)
- [Who can block](#who-can-block)
- [What each verdict does](#what-each-verdict-does)
- [When the rework does not converge](#when-the-rework-does-not-converge)
- [Why the other approvals go stale too](#why-the-other-approvals-go-stale-too)
- [`reviews.gate7_advisory`](#reviewsgate7_advisory)
- [Where to go next](#where-to-go-next)

## Introduction

Three gates are reviewed: `design_review` at Gate 4, `verifier_review` at Gate 6
and `code_review` at Gate 7. Sooner or later one of the reviewers will find
something wrong and record a `block`. Don't worry if that happens to your first
story: it is the loop doing what it is for.

A block that can stop the gate does stop it, and the only way past it is the
same reviewer looking again at the reworked plan or code. This page shows you
what a block looks like, how a rework round goes, and which reviewers are able
to block in the first place.

A project with `human_gates.dor_advocate_check` on also has `dor` wait for the
human advocate. That review is advisory, and nothing below changes for it.

## What a block looks like

When a reviewer has blocked, `sdlc gate` tells you so and names the reviewer
and the headline of its finding:

```console
$ sdlc gate design_review pass
sdlc: design_review is blocked by a reviewer

  why  architect blocked: AC-2 has no step
  fix  fix what the reviewer found, then have the same reviewer look again

  SDLC-E0030  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0030
```

The headline is only the start. The full review is stored as a file in the
story's `reviews` directory (`.sdlc/stories/<ID>/reviews/`), and
`sdlc review add` prints its path when the reviewer records it (see
[The reviewer looks again](#the-reviewer-looks-again)). Open that file to read
everything the reviewer found.

There is no flag that overrides this. A gate that could pass over its blocking
reviewer would make every review optional.

## Rework

The usual way through is to revise the work and ask the reviewer to look
again. Start by recording the gate as failed, with the finding in the note:

```console
$ sdlc gate design_review fail --note "architect blocked: AC-2 has no step"
US-001  design_review  fail
  architect blocked: AC-2 has no step
```

Then send the findings back to whoever did the work. At Gate 4 that is
`sdlc:implementer`, revising the plan and storing it again with
`sdlc artifact write plan`. At Gates 6 and 7 it is also `sdlc:implementer`, this
time changing the code.

> [!WARNING]
> Do not fix the plan or the code in the main conversation. The hook refuses
> code there, and a plan revised by the conversation that argued for it is not
> what the reviewer asked for.

### The reviewer looks again

A reviewer records its conclusion with `sdlc review add GATE ROLE VERDICT`. The review document
comes from standard input or `--file`, and `--note` carries the headline. `--important` and `--nits`
carry the reviewer's own counts: a block with no important finding is refused
([SDLC-E0086](../troubleshooting.md#sdlc-e0086)), and a review with only nits is an approve with the
nits listed. At `design_review` and `code_review`, `--finding` lists each finding counted, and
each is numbered F-1, F-2 and so on. The numbers below are examples. When the architect first
blocked, the review was recorded like this:

```console
$ sdlc review add design_review architect block --important 1 --finding "important the plan: AC-2 has no step" --note "AC-2 has no step" < review.md
US-001  design_review  architect  block (round 1)
  .sdlc/stories/US-001/reviews/design_review-architect-1.md
```

The implementer closes the finding once the plan has the step, with
`sdlc finding close design_review architect F-1 --fixed`. When the rework is in, the **same**
reviewer looks again. It reads the finding's closing first, and its new verdict is a new round,
which the gate reads:

```console
$ sdlc review add design_review architect approve --important 0 --nits 2 --finding "nit the plan: step 4 is long" --finding "nit the plan: AC-1 and AC-2 share a step" < review.md
US-001  design_review  architect  approve (round 2)
  .sdlc/stories/US-001/reviews/design_review-architect-2.md
```

Earlier rounds are kept, both in the record and as files, so the second round
can see what the first one said.

What a reviewer reports is set by the [review policy](../configuration.md#reviewspolicy). If the
reviewers are reporting the wrong things, a person commits a change to it on trunk between stories,
and a running story keeps the copy it was cut from. To apply it to this story, end the story, commit
on trunk and start again.

## Who can block

Not every reviewer can stop a gate. Every reviewer a gate expects has to
report, whether or not it can block, and what its verdict does depends on the
kind of reviewer it is. `sdlc review list` shows the split for the story in
front of you:

```console
$ sdlc review list --gate design_review
Review policy: REVIEW.md
  design_review   architect       blocking  block (round 1)
  design_review   red-team        advisory  not reviewed
  design_review   security        advisory  not reviewed
  design_review   perf            on budget not reviewed
  design_review   human-advocate  advisory  not reviewed
```

Here the architect is blocking, so its `block` holds the gate. The same split,
gate by gate:

| Gate | Blocking | On budget | Advisory |
| --- | --- | --- | --- |
| `design_review` | `architect`; `security` when the story is security-sensitive | `perf` | `red-team`, `human-advocate`; `security` otherwise |
| `verifier_review` | `verifier` | none | none |
| `code_review` | `code-reviewer`; `security` when the story is security-sensitive | `perf` | `human-advocate`; `security` otherwise |

Whether a story is security-sensitive is recorded at Gate 2 with
`sdlc gate analysis pass --security-sensitive` or `--security-sensitive=false`.
If it was never set, the story is treated as sensitive and security blocks.

`perf` never has to approve, but its block stops the gate. Its agent is told to
block only when the change breaks a performance budget stated in `CLAUDE.md`, so
a block from it means a stated budget, not a preference.

`reviews.gate7_advisory` moves the code reviewer to advisory. See the
[last section](#reviewsgate7_advisory).

## What each verdict does

A reviewer's verdict is `approve`, `block` or `note`, and the table shows what
each does for each kind of reviewer:

| Verdict | From a blocking reviewer | From perf | From an advisory reviewer |
| --- | --- | --- | --- |
| `approve` | lets the gate pass | counts as reported | counts as reported |
| `block` | stops the gate | stops the gate | counts as reported |
| `note` | not enough: the gate waits for an approval | counts as reported | counts as reported |

A role the gate does not expect is refused with
[SDLC-E0027](../troubleshooting.md#sdlc-e0027).

## When the rework does not converge

Rounds are not unlimited. A block that stops the gate, recorded
`loop.max_review_rounds` times by the same reviewer, hands the story to a
person. The configuration `sdlc init` writes sets it to 2, so had the architect
blocked again in round 2:

```console
$ sdlc review add design_review architect block --important 1 --finding "important the plan: AC-2 still has no step" --note "AC-2 still has no step" < review.md
US-001  design_review  architect  block (round 2)
  .sdlc/stories/US-001/reviews/design_review-architect-2.md

US-001 is handed to a person: architect has blocked design_review 2 times, and loop.max_review_rounds is 2, so the rework is not converging on what it asks for. The last note: AC-2 still has no step

A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve US-001, or sdlc approve US-001
  sdlc approve US-001 --reject "why"
```

The iteration ends there, and `sdlc status` lists the story as waiting. A block
from an advisory reviewer is a finding, not a lost round, so it is not counted.
Recording the same gate as failed `loop.max_rework_rounds` times, 3 by default,
hands the story over in the same way. Both counts start again once a person has
answered with `sdlc approve`. See [`loop`](../configuration.md#loop).

## Why the other approvals go stale too

You may notice that after a rework, reviewers who never blocked are asked to
look again. Every review is stamped with what the reviewer was looking at: the
content of `PLAN.md` at `design_review`, and the working tree, minus `.sdlc/`,
at `verifier_review` and `code_review`. Rework changes that, so every earlier
review of the gate stops counting, advisory ones included:

```console
$ sdlc review list --gate design_review
Review policy: REVIEW.md
  design_review   architect       blocking  approve (round 2)
  design_review   red-team        advisory  note (round 1, stale -- what was reviewed has changed since; reviewed revision 40c9f6da7ea56d0ae851a7114f073945adaf8903e44383452f4111abcdb86a5d; see sdlc artifact diff plan --since red-team)
  design_review   security        advisory  note (round 1, stale -- what was reviewed has changed since; reviewed revision 40c9f6da7ea56d0ae851a7114f073945adaf8903e44383452f4111abcdb86a5d; see sdlc artifact diff plan --since security)
  design_review   perf            on budget note (round 1, stale -- what was reviewed has changed since; reviewed revision 40c9f6da7ea56d0ae851a7114f073945adaf8903e44383452f4111abcdb86a5d; see sdlc artifact diff plan --since perf)
  design_review   human-advocate  advisory  note (round 1, stale -- what was reviewed has changed since; reviewed revision 40c9f6da7ea56d0ae851a7114f073945adaf8903e44383452f4111abcdb86a5d; see sdlc artifact diff plan --since human-advocate)

$ sdlc gate design_review pass
sdlc: design_review cannot pass until its reviews are in

  why  red-team reviewed something that has changed since; security reviewed something that has changed since; perf reviewed something that has changed since; human-advocate reviewed something that has changed since
  fix  delegate to the reviewers this gate expects -- "sdlc review list" shows who is outstanding

  SDLC-E0029  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0029
```

This is deliberate. The red team read a plan that no longer exists, and its
findings about that plan say nothing about this one. Send the whole gate back
out, not only the reviewer that blocked.

Those reviewers are resumed, not started afresh, and each is given what changed,
from the command its line names. `sdlc artifact diff` reads a diff and records
nothing, so every reviewer still records a verdict on the plan as it now stands
before the gate can pass.

The same holds after a gate has passed. If the code changes after
`code_review`, the hook refuses `git commit` of that tree and names the
reviewers whose approvals it no longer matches. `sdlc gate commit pass` asks
again too, and refuses with SDLC-E0029, sending the story back to
`verifier_review` and `code_review`. See
[why approvals go stale](../the-loop.md#why-approvals-go-stale).

## `reviews.gate7_advisory`

If you would rather the code reviewer could not stop Gate 7, there is a setting
for it. [Configuration](../configuration.md#reviewsgate7_advisory) describes
it: it makes the code reviewer advisory, and it is off by default. With it on,
`sdlc review list` shows the change:

```console
$ sdlc review list --gate code_review
Review policy: REVIEW.md
  code_review     code-reviewer   advisory  not reviewed
  code_review     security        blocking  not reviewed
  code_review     perf            on budget not reviewed
  code_review     human-advocate  advisory  not reviewed
```

Advisory does not mean skipped: `code_review` still waits for the code reviewer's report
on the tree as it stands, and a missing or stale one holds the gate, but its
`block` no longer stops it. Nothing else moves. The verifier stays blocking,
perf still blocks on a budget, and security still blocks on a security-sensitive
story. It blocks here only because this story's sensitivity was never recorded.

> [!WARNING]
> Turning it on is a real loosening: Gate 7 is the last review between a change
> and trunk.

## Where to go next

- [Commands: `sdlc review add`](../commands.md#sdlc-review-add-gate-role-verdict)
  and [`sdlc review list`](../commands.md#sdlc-review-list)
- [Agents](../the-agents.md#who-does-what): which agent reviews at each gate
- [The loop: rework](../the-loop.md#rework)
- [When a frozen test is wrong](when-a-frozen-test-is-wrong.md)
