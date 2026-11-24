# When a reviewer blocks

- [Introduction](#introduction)
- [What happens when a reviewer blocks](#what-happens-when-a-reviewer-blocks)
- [Read what the reviewer found](#read-what-the-reviewer-found)
- [When a reviewer keeps blocking](#when-a-reviewer-keeps-blocking)
- [When the loop hands the story to you](#when-the-loop-hands-the-story-to-you)
  - [Decide what to do](#decide-what-to-do)
  - [Answer, and carry on](#answer-and-carry-on)
- [Who can block](#who-can-block)
- [What each verdict does](#what-each-verdict-does)
- [Why the other reviewers look again](#why-the-other-reviewers-look-again)
- [Making a reviewer advisory with `can_block`](#making-a-reviewer-advisory-with-can_block)
- [Where to go next](#where-to-go-next)

## Introduction

Four gates end in a review. The tests are reviewed at Gate 3, before they are
frozen, and the plan at Gate 4. The verifier gives its verdict on the
implementation at Gate 6, and the diff is reviewed at Gate 7. Sooner or later a
reviewer finds something wrong and blocks. Don't worry if that happens to your
first story: it is the loop doing what it is for.

Most blocks need nothing from you. The loop sends the finding back to the
author, the work is revised, and the same reviewer looks again. When that does
not settle it, a second reviewer of the same kind, its arbiter, rules on what
is still open. A story comes to you only when the arbiter has blocked twice as
well.

This page shows what a block looks like, how to read what the reviewer found,
and what your options are when the story is handed to you. After that it says
which reviewers can block at all, and how to change that.

## What happens when a reviewer blocks

A block from a reviewer that can stop its gate does stop it. When the session
tries to pass the gate, `sdlc` refuses:

```text
sdlc: design_review is blocked by a reviewer

  why  architect blocked: AC-2 has no step
  fix  fix what the reviewer found, then have the same reviewer look again

  SDLC-E0030  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0030
```

There is no flag that overrides this. A gate that could pass over its blocking
reviewer would make every review optional. At Gate 3 it is `sdlc freeze` that
refuses, with the same code, until the test reviewer approves the tests as they
are.

What follows is a rework round, and the loop runs it without you:

1. The gate is recorded as failed, with the reviewer's finding as its note.
2. The findings go back to the author: the test author, who corrects the tests
   at Gate 3, or the implementer, who revises the plan at Gate 4, or the code
   at Gates 6 and 7.
3. The same reviewer reads the revised work and gives a new verdict. Earlier
   rounds are kept, so the second round can see what the first one said.
4. The other reviewers of that gate look again as well, because
   [what they reviewed has changed](#why-the-other-reviewers-look-again).

> [!NOTE]
> If you are watching, resist asking the session to patch the plan or the code
> itself. The hook refuses code written in the main conversation, and a plan
> revised by the conversation that argued for it is not what the reviewer asked
> for.

## Read what the reviewer found

To see where every reviewer of a gate stands, ask from the story's worktree:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc review list --gate design_review
Review policy: REVIEW.md
  design_review   architect       blocking  block (round 1)
  design_review   red-team        advisory  note (round 1)
  design_review   security        advisory  note (round 1)
  design_review   perf            on budget note (round 1)
  design_review   human-advocate  advisory  note (round 1)
```

Each line is a reviewer, whether it can stop the gate, and its latest verdict.
Once the story has been handed to you it is no longer running, so name it:
`sdlc review list --story AUTH-3`.

The reason in a refusal is only the headline. Every review is kept in full in
your project, one file for each reviewer and round, such as
`.sdlc/stories/AUTH-3/reviews/design_review-architect-1.md`. Open that file to
read everything the reviewer found.

Each thing a reviewer wants changed is a numbered finding, `F-1`, `F-2` and so
on. Every finding is closed before the story is committed: fixed, or declined
with a reason the reviewer then reads.

## When a reviewer keeps blocking

Rounds are not unlimited. When the same reviewer has blocked twice, which is the
limit `sdlc init` sets as `loop.max_review_rounds`, the loop does not hand the
story to you yet. It sends the gate to the reviewer's **arbiter**, and goes on
without you. This is what `sdlc` prints at the second block:

```text
AUTH-3  design_review  architect  block (round 2)
  .sdlc/stories/AUTH-3/reviews/design_review-architect-2.md
  architect has blocked design_review 2 times, and loop.max_review_rounds is 2: the gate goes to architect-arbiter, a fresh sdlc:architect that rules on the open findings.
  Its review then stands for architect at design_review, and architect is refused there.
  Until architect-arbiter first reviews, any answer a person gives the story starts architect's count again and gives it its turn.
  From then on, only a person's answer to a review_not_converging hand-over raised by architect or architect-arbiter at design_review gives architect its turn again.
```

The arbiter is a fresh agent of the same kind as the reviewer, recorded as
`architect-arbiter`. It reads the open findings, how each was closed and the
reviewer's documents, and rules on each: fixed, or declined with a reason. It is
asked for one step above the reviewer, in effort, or in model where the effort
is already at its ceiling, and `sdlc staffing show --gate design_review` lists
it in the architect's place.

From then on the arbiter's review stands for the architect's at that gate, and
the gate reads it. The architect itself is refused there. A reviewer that cannot
block never reaches an arbiter, because a block that cannot stop the gate is not
counted. A reviewer your project has turned off has none, so its own limit hands
the story over, as the next section describes.

## When the loop hands the story to you

Only the arbiter's own limit hands the story to you. When the arbiter has
blocked twice too, `sdlc` ends the iteration:

```text
AUTH-3  design_review  architect-arbiter  block (round 2)
  .sdlc/stories/AUTH-3/reviews/design_review-architect-arbiter-2.md

AUTH-3 is handed to a person: architect-arbiter has blocked design_review 2 times, and loop.max_review_rounds is 2, so the rework is not converging on what it asks for. The last note: AC-2 still has no step

A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve AUTH-3, or sdlc approve AUTH-3
  sdlc approve AUTH-3 --reject "why"
```

An agent may have a limit of its own, `max_rounds` in its entry under
`team.agents`, and it then counts for that agent in place of
`loop.max_review_rounds`. A gate that is recorded as failed three times,
whatever the reason, is handed over in the same way, and that limit is
`loop.max_rework_rounds`. A block from an advisory reviewer is a finding, not a
lost round, so it is not counted. [`loop`](../configuration.md#loop) describes
both settings.

Nothing has gone wrong, and nothing is lost. The work is in the story's
worktree, `.sdlc/worktrees/AUTH-3`, and every round of the reviews, the
arbiter's included, is in `.sdlc/stories/AUTH-3/reviews/` in your project.

### Decide what to do

Read the rounds of both reviewers and the work they are about. A reviewer and an author who
cannot agree are usually in one of three positions, and each has its own way
on.

**The reviewer is right, and the author has not got there yet.** Let the loop
try again. Your approval gives the reviewer its turn again and starts the counts
afresh, so the author gets two more rounds with it. If you can see what the
author keeps missing, say so in the story first.

**They disagree because the story leaves it open.** Settle it yourself. Edit
the story in your backlog, `user_stories.json` in your project: tighten the
criterion, or add a line to its `decisions`. Every agent reads the story from
there, the reviewer included.
[Write your decision down](human-approval-gates.md#write-your-decision-down)
has an example.

**The reviewer is holding the work to the wrong thing.** A blocking reviewer
cannot be overruled for one story, and that is deliberate. What you can change
is what reviewers are asked to report, in the
[review policy](../configuration.md#reviewspolicy), and which of them can stop
a gate, with [`can_block`](#making-a-reviewer-advisory-with-can_block). Both
are changed on trunk, and both reach the stories you start afterwards. The
story that is waiting keeps the copies its worktree was cut from, so for this
one, write your ruling into the story's `decisions`, where the reviewer reads
it on the next round.

### Answer, and carry on

Answer in a terminal of your own, from the story's worktree:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc approve AUTH-3
AUTH-3  approved (review_not_converging)

A person runs `sdlc start AUTH-3`, or types /sdlc:next in Claude Code, to carry on.
```

To send the work back with a reason of your own, use
`sdlc approve AUTH-3 --reject "why"` in its place.
[Human approval gates](human-approval-gates.md#where-to-type-your-answer) says
where to type the answer if you are not at a terminal.

Then set the story going again, in Claude Code, in your project:

```text
/sdlc:next AUTH-3
```

The loop picks up at the gate that was blocked and starts another rework
round.

## Who can block

Every reviewer a gate expects has to report, but not every reviewer can stop
the gate. `sdlc review list` shows which is which for the story in front of
you, in its third column. The same split, gate by gate:

| Gate | Blocking | On budget | Advisory |
| --- | --- | --- | --- |
| `tests_frozen` | `test-reviewer`, unless its `can_block` is false | none | none |
| `design_review` | `architect`; `security` when the story is security-sensitive | `perf` | `red-team`, `human-advocate`; `security` otherwise |
| `verifier_review` | `verifier` | none | none |
| `code_review` | `code-reviewer`; `security` when the story is security-sensitive | `perf` | `human-advocate`; `security` otherwise |

The test reviewer is the one whose block refuses a command other than
`sdlc gate`: it refuses `sdlc freeze` until the review that stands for the test
reviewer approves the tests as they are. That is its own, or its arbiter's once
its limit has sent the tests there. Where its `can_block` is false it is
advisory, and the freeze waits only until it has reviewed.

Whether a story is security-sensitive is decided at Gate 2, from the
researcher's threat assessment. Until that gate has passed, the story is
treated as sensitive, and security blocks.

`perf` is the one "on budget" reviewer. It never has to approve, but its block
stops the gate. Its agent is told to block only when the change breaks a
performance budget stated in your `CLAUDE.md`, so a block from it means a
stated budget and not a preference.

## What each verdict does

A reviewer's verdict is `approve`, `block` or `note`. What it does depends on
the kind of reviewer that gave it:

| Verdict | From a blocking reviewer | From perf | From an advisory reviewer |
| --- | --- | --- | --- |
| `approve` | lets the gate pass | counts as reported | counts as reported |
| `block` | stops the gate | stops the gate | counts as reported |
| `note` | not enough: the gate waits for an approval | counts as reported | counts as reported |

## Why the other reviewers look again

After a rework round you may notice that reviewers who never blocked are asked
to look again. Every review is of something in particular: the plan at Gate 4,
and the files in the story's worktree at Gates 6 and 7. Rework changes that, so
every earlier review of the gate stops counting, advisory ones included:

```console
$ sdlc review list --gate design_review
Review policy: REVIEW.md
  design_review   architect       blocking  approve (round 2)
  design_review   red-team        advisory  note (round 1, stale -- what was reviewed has changed since; reviewed revision d5e8e535085d883692e839a7faaba7b232fcf7a45be95221c23fd1199f1531eb; see sdlc artifact diff plan --since red-team)
  design_review   security        advisory  note (round 1, stale -- what was reviewed has changed since; reviewed revision d5e8e535085d883692e839a7faaba7b232fcf7a45be95221c23fd1199f1531eb; see sdlc artifact diff plan --since security)
  design_review   perf            on budget note (round 1, stale -- what was reviewed has changed since; reviewed revision d5e8e535085d883692e839a7faaba7b232fcf7a45be95221c23fd1199f1531eb; see sdlc artifact diff plan --since perf)
  design_review   human-advocate  advisory  note (round 1, stale -- what was reviewed has changed since; reviewed revision d5e8e535085d883692e839a7faaba7b232fcf7a45be95221c23fd1199f1531eb; see sdlc artifact diff plan --since human-advocate)
```

This is deliberate. The red team read a plan that no longer exists, and what it
found there says nothing about this one. The gate
[will not pass](../troubleshooting.md#sdlc-e0029) until each of them has given
a verdict on the plan as it now stands.

It costs less than it sounds. Those reviewers are resumed, not started afresh,
and each is given only what changed since it last looked, which is the diff
its line names.

The same holds after a gate has passed. If the code changes after the code
review, the commit is refused, naming the reviewers whose approvals it no
longer matches.
[Why approvals go stale](../the-loop.md#why-approvals-go-stale) has the whole
rule.

## Making a reviewer advisory with `can_block`

If you would rather a reviewer could not stop its gate, there is a setting for
it. Each reviewer's entry under `team.agents` in `.sdlc/config.json` has a
`can_block`, and for the code reviewer it looks like this once it is set to
false:

```json
"code-reviewer":   { "enabled": true, "can_block": false, "instances": 1 }
```

Commit the change to trunk, and the stories you start afterwards read it.
`sdlc review list` shows the difference:

```console
$ sdlc review list --gate code_review
Review policy: REVIEW.md
  code_review     code-reviewer   advisory  not reviewed
  code_review     security        advisory  not reviewed
  code_review     perf            on budget not reviewed
  code_review     human-advocate  advisory  not reviewed
```

Advisory does not mean skipped. The gate still waits for the code reviewer's
report on the files as they stand, and a missing or stale one holds it. Only
its `block` no longer stops the gate. Its findings are still closed before the
commit, as every reviewer's are.

Nothing else moves. The verifier stays blocking, perf still blocks on a budget,
and security still blocks on a security-sensitive story, which this one is not.

> [!WARNING]
> Think twice before you turn this off for the code reviewer. Gate 7 is the
> last review between a change and trunk, and for a story like this one only a
> broken performance budget can then stop it.
> [An agent's entry](../configuration.md#an-agents-entry) describes the
> setting.

## Where to go next

- [Human approval gates](human-approval-gates.md): answering any hand-over,
  and where to type the answer
- [Agents](../the-agents.md#who-does-what): which agent reviews at each gate,
  and what each looks for
- [Rework](../the-loop.md#rework): how the loop goes back, and never around
- [`sdlc review list`](../commands.md#sdlc-review-list): the reference for the
  command
