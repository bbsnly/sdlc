# When a reviewer blocks

Three gates are reviewed: `design_review` at Gate 4, `verifier_review` at Gate 6
and `code_review` at Gate 7. A block that can stop the gate does, and the only
way past it is the same reviewer looking again at the reworked plan or code.
This page shows who can block, what a block looks like, and how a rework round
goes.

A project with `human_gates.dor_advocate_check` on also has `dor` wait for the
human advocate. That review is advisory, and nothing below changes for it.

## Who can block

Every reviewer a gate expects has to report, whether or not it can block. What
its verdict does depends on the kind of reviewer it is:

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

`sdlc review list` shows the split for the story in front of you:

```console
$ sdlc review list --gate design_review
  design_review   architect       blocking  block (round 1)
  design_review   red-team        advisory  not reviewed
  design_review   security        advisory  not reviewed
  design_review   perf            on budget not reviewed
  design_review   human-advocate  advisory  not reviewed
```

## What each verdict does

A reviewer records its conclusion with `sdlc review add GATE ROLE VERDICT`. The
review document comes from standard input or `--file`, and `--note` carries the
headline:

```console
$ sdlc review add design_review architect block --note "AC-2 has no step" < review.md
US-001  design_review  architect  block (round 1)
  .sdlc/stories/US-001/reviews/design_review-architect-1.md
```

| Verdict | From a blocking reviewer | From perf | From an advisory reviewer |
| --- | --- | --- | --- |
| `approve` | lets the gate pass | counts as reported | counts as reported |
| `block` | stops the gate | stops the gate | counts as reported |
| `note` | not enough: the gate waits for an approval | counts as reported | counts as reported |

A role the gate does not expect is refused with
[SDLC-E0027](../troubleshooting.md#sdlc-e0027).

## What a block looks like

```console
$ sdlc gate design_review pass
sdlc: design_review is blocked by a reviewer

  why  architect blocked: AC-2 has no step
  fix  fix what the reviewer found, then have the same reviewer look again

  SDLC-E0030  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0030
```

There is no flag that overrides this. A gate that could pass over its blocking
reviewer would make every review optional.

## Rework

Record the gate as failed, with the finding in the note:

```console
$ sdlc gate design_review fail --note "architect blocked: AC-2 has no step"
US-001  design_review  fail
  architect blocked: AC-2 has no step
```

Then send the findings back to whoever did the work. At Gate 4 that is
`sdlc:implementer`, revising the plan and storing it again with
`sdlc artifact write plan`. At Gates 6 and 7 it is `sdlc:implementer`, changing
the code. Do not fix the plan or the code in the main conversation. The hook
refuses code there, and a plan revised by the conversation that argued for it is
not what the reviewer asked for.

When the rework is in, the **same** reviewer looks again. Its new verdict is a
new round, and the gate reads the latest one:

```console
$ sdlc review add design_review architect approve < review.md
US-001  design_review  architect  approve (round 2)
  .sdlc/stories/US-001/reviews/design_review-architect-2.md
```

Earlier rounds are kept, both in the record and as files, so the second round
can see what the first one said.

## When the rework does not converge

Rounds are not unlimited. A block that stops the gate, recorded
`loop.max_review_rounds` times by the same reviewer, hands the story to a
person. The configuration `sdlc init` writes sets it to 2, so had the architect
blocked again in round 2:

```console
$ sdlc review add design_review architect block --note "AC-2 still has no step" < review.md
US-001  design_review  architect  block (round 2)
  .sdlc/stories/US-001/reviews/design_review-architect-2.md

US-001 is handed to a person: architect has blocked design_review 2 times, and loop.max_review_rounds is 2, so the rework is not converging on what it asks for. The last note: AC-2 still has no step

A person reads the work and answers in their own terminal:
  sdlc approve US-001
  sdlc approve US-001 --reject "why"
```

The iteration ends there, and `sdlc status` lists the story as waiting. A block
from an advisory reviewer is a finding, not a lost round, so it is not counted.
Recording the same gate as failed `loop.max_rework_rounds` times, 3 by default,
hands the story over in the same way. Both counts start again once a person has
answered with `sdlc approve`. See [`loop`](../configuration.md#loop).

## Why the other approvals go stale too

Every review is stamped with what the reviewer was looking at: the content of
`PLAN.md` at `design_review`, and the working tree, minus `.sdlc/`, at
`verifier_review` and `code_review`. Rework changes that, so every earlier
review of the gate stops counting, advisory ones included:

```console
$ sdlc review list --gate design_review
  design_review   architect       blocking  approve (round 2)
  design_review   red-team        advisory  note (round 1, stale -- what was reviewed has changed since)
  design_review   security        advisory  note (round 1, stale -- what was reviewed has changed since)
  design_review   perf            on budget note (round 1, stale -- what was reviewed has changed since)
  design_review   human-advocate  advisory  note (round 1, stale -- what was reviewed has changed since)

$ sdlc gate design_review pass
sdlc: design_review cannot pass until its reviews are in

  why  red-team reviewed something that has changed since; security reviewed something that has changed since; perf reviewed something that has changed since; human-advocate reviewed something that has changed since
  fix  delegate to the reviewers this gate expects -- "sdlc review list" shows who is outstanding

  SDLC-E0029  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0029
```

This is deliberate. The red team read a plan that no longer exists, and its
findings about that plan say nothing about this one. Send the whole gate back
out, not only the reviewer that blocked.

The same holds after a gate has passed. If the code changes after
`code_review`, the hook refuses `git commit` of that tree and names the
reviewers whose approvals it no longer matches. `sdlc gate commit pass` asks
again too, and refuses with SDLC-E0029, sending the story back to
`verifier_review` and `code_review`. See
[why approvals go stale](../the-loop.md#why-approvals-go-stale).

## `reviews.gate7_advisory`

[Configuration](../configuration.md#reviewsgate7_advisory) describes this
setting: it makes the code reviewer advisory, and it is off by default. With it
on, `sdlc review list` shows the change:

```console
$ sdlc review list --gate code_review
  code_review     code-reviewer   advisory  not reviewed
  code_review     security        blocking  not reviewed
  code_review     perf            on budget not reviewed
  code_review     human-advocate  advisory  not reviewed
```

Advisory is not skipped. `code_review` still waits for the code reviewer's report
on the tree as it stands, and a missing or stale one holds the gate, but its
`block` no longer stops it. Nothing else moves. The verifier stays blocking,
perf still blocks on a budget, and security still blocks on a security-sensitive
story. It blocks here only because this story's sensitivity was never recorded.

Turning it on is a real loosening: Gate 7 is the last review between a change
and trunk.

## Where to go next

- [Commands: `sdlc review add`](../commands.md#sdlc-review-add-gate-role-verdict)
  and [`sdlc review list`](../commands.md#sdlc-review-list)
- [Agents](../agents.md#who-does-what): what each reviewer looks for
- [The loop: rework](../the-loop.md#rework)
- [When a frozen test is wrong](when-a-frozen-test-is-wrong.md)
