# When a frozen test is wrong

- [Introduction](#introduction)
- [When the loop tells you a test is wrong](#when-the-loop-tells-you-a-test-is-wrong)
- [Decide whether the test is wrong](#decide-whether-the-test-is-wrong)
- [Lift the freeze](#lift-the-freeze)
- [Have the test corrected](#have-the-test-corrected)
  - [Correcting the test yourself](#correcting-the-test-yourself)
- [What lifting the freeze costs](#what-lifting-the-freeze-costs)
- [When a frozen test was changed](#when-a-frozen-test-was-changed)
- [Adding a test file after the freeze](#adding-a-test-file-after-the-freeze)
- [What an agent may still do with a frozen test](#what-an-agent-may-still-do-with-a-frozen-test)
- [Where to go next](#where-to-go-next)

## Introduction

At Gate 3 the loop turns each acceptance criterion into a failing test, and
then freezes the tests. From that moment no agent can edit them: not the
implementer, not the test author, not the session. An agent that can edit its
own tests will sooner or later edit one to pass, and the freeze is what makes a
green result at the end worth something.

Sometimes, though, a frozen test is plainly wrong. It asserts something the
criterion never said. Don't worry: there is a way to change it, and it is yours
alone. You lift the freeze with a reason, the test is corrected, and the tests
are frozen again.

This page takes you through those steps and says what they cost the gates that
come after. It also covers two neighbours of the same problem: a frozen test
that was changed without the freeze being lifted, and a test file that has to
be added once the tests are frozen.

## When the loop tells you a test is wrong

An agent that believes a frozen test is wrong can do nothing about it. It says
which criterion the test contradicts, hands the story to you, and stops:

```text
AUTH-3  waiting for a person: frozen_test_wrong
  TestAC1 expects 401 for a token that expires in an hour; AC-1 rejects only an expiry in the past

The iteration has ended. A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve AUTH-3, or sdlc approve AUTH-3
  sdlc approve AUTH-3 --reject "why"
```

The story now waits in its worktree, `.sdlc/worktrees/AUTH-3`, with its tests
still frozen. You may also arrive here on your own, having read a test and
found it wrong. The steps are the same.

## Decide whether the test is wrong

Changing the code is the default, so be strict before you agree. Open the test
in the story's worktree and the criterion in your backlog, then name the
criterion the test contradicts and say how. "The test is too strict" is not
that. "AC-1 rejects only an expiry in the past, and the test expects 401 for a
token that expires in an hour" is.

**If the test is right**, the code is what has to change. Send the work back
with your reason, from the story's worktree, in a terminal of your own:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc approve AUTH-3 --reject "the test is right: the token in it expired an hour ago"
```

Then type `/sdlc:next AUTH-3` in Claude Code, in your project. The freeze
stands, and the loop carries on against the same tests.

**If the criterion is wrong**, and the test only repeats its mistake, correct
the criterion in your backlog first. Then lift the freeze, so that the test can
follow it.

**If the test is wrong**, carry on with this page.

## Lift the freeze

Lifting the freeze takes three commands, run in a terminal of your own from the
story's worktree. The first two put the story back in your hands:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc approve AUTH-3
$ sdlc start AUTH-3
```

`approve` answers the hand-over, which `sdlc` insists on before the story will
start again. `start` is needed because the hand-over ended the iteration, and
the freeze is lifted on the story being worked on, in the checkout the command
runs in.

The third lifts the freeze, and it needs your reason:

```console
$ sdlc unfreeze --reason "TestAC1 expects 401 for a token that expires in an hour; AC-1 rejects only an expiry in the past"
AUTH-3  freeze lifted on 1 test file
  TestAC1 expects 401 for a token that expires in an hour; AC-1 rejects only an expiry in the past
  reopened: tests_frozen, plan, design_review
```

The reason is the point of the command. It goes on the story's record beside
the freeze it undid, and it is what the retro, and anybody reading
[`sdlc log`](reviewing-what-landed-on-trunk.md) later, will see. Lifting a
freeze is sometimes right, and it is also the move an agent would make to
reach green. A recorded reason is what tells the two apart.

If nothing was handed over, because you found the test yourself while the
story was running, skip the first two commands. If you have no terminal,
[Human approval gates](human-approval-gates.md#where-to-type-your-answer) says
where a person's commands are typed in Claude Code and in Claude Desktop.

> [!NOTE]
> Only a person lifts a freeze. The hook refuses `sdlc unfreeze` from an agent,
> and the command refuses to run anywhere Claude Code started it.
> [Enforcement](../enforcement.md#unfreeze-is-a-human-decision) says why.

## Have the test corrected

Lifting the freeze sends the story back to Gate 3, so the simplest way on is to
let the loop do the work. In Claude Code, in your project, type:

```text
/sdlc:next AUTH-3
```

The loop starts at Gate 3 again. It gives the test author your reason and the
job of correcting the test it names, freezes the tests once that is done, and
goes on through the gates that follow.

### Correcting the test yourself

You may edit the test yourself instead. The freeze is lifted, so nothing
refuses the edit. Where the test reviewer is staffed, though, `sdlc freeze`
refuses until the reviewer has approved the corrected tests
([SDLC-E0029](../troubleshooting.md#sdlc-e0029)), and only the loop asks it. In
that case let `/sdlc:next AUTH-3` finish the job. Otherwise, when the test says
what the criterion says, freeze again and pass the gate, in the worktree:

```console
$ sdlc freeze
AUTH-3  1 test file frozen
  internal/session/session_test.go
```

```console
$ sdlc gate tests_frozen pass --note "AC-1's test corrected"
```

The new freeze covers every test file in the worktree at that moment, any you
added included. Then type `/sdlc:next AUTH-3`, and the loop carries on from the
gate after.

## What lifting the freeze costs

The tests are about to change, so the gate that froze them no longer stands,
and nor does any gate passed on top of it. `sdlc unfreeze` lists them after
`reopened`. Each has to pass again, in order, but not all of the work behind
them is done again:

- **Design review approvals stand**, so long as the plan does not change. They
  are approvals of the plan, not of the tests, and the gate passes again on the
  reviews it already has.
- **The test reviewer's approval does not**, where it is staffed. It approved
  the tests as they were, so the corrected ones are reviewed before the freeze
  is taken again. Where the tests had gone to its
  [arbiter](when-a-reviewer-blocks.md#when-a-reviewer-keeps-blocking) before the first freeze, the arbiter
  reviews them. Tests put back byte for byte as they were keep their approval.
- **The verifier's and the code reviewers' approvals do not.** They approved
  the files as they were, and a corrected test is a changed file. Both gates
  need their reviewers again.
- **A corrected test that changes what has to be built needs a new plan.** The
  implementer revises it at Gate 4, and the design reviewers look at the new
  one.

## When a frozen test was changed

A hand-over of the kind `freeze_broken` is the other way round: the loop found
that a frozen test is no longer what was frozen. Something edited it, or
removed it, without the freeze being lifted. The loop does not freeze again to
cover that over. It stops and asks you.

Find out what changed. The refusal the loop met names every file that is not
as it was frozen, and [Troubleshooting](../troubleshooting.md#sdlc-e0025)
describes it. Then there are two ways on:

- **The change should not be there.** Put the file back as it was frozen,
  answer with `sdlc approve AUTH-3` in the story's worktree, and type
  `/sdlc:next AUTH-3`.
- **The test did have to change.** [Lift the freeze](#lift-the-freeze), so
  that the change is a decision on the record and not something that happened
  quietly. The tests are then frozen again as they now are.

## Adding a test file after the freeze

No test file can be added once the tests are frozen. The hook refuses one
written by an agent, and a test file that reaches the worktree some other way
[holds the gates](../troubleshooting.md#sdlc-e0043) until it is gone or frozen.
The way to add one is the way to change one: lift the freeze, add the file, and
freeze again, so that the new file is covered.

If your project needs it often, `freeze.allow_new_test_files` in
`.sdlc/config.json` loosens the rule for the test author alone. With it on, the
test author may write a new test file after the freeze, and running
`sdlc freeze` again adds the file to the freeze that is already there, once the
test reviewer, where it is staffed, has approved the tests with it. The
implementer is still refused, because that rule is not a setting.

> [!WARNING]
> Know what you are giving up before you turn it on. A test written after the
> code can be written to pass, which is the very thing the freeze is there to
> prevent.
> [`freeze.allow_new_test_files`](../configuration.md#freezeallow_new_test_files)
> describes the setting.

## What an agent may still do with a frozen test

The freeze stops edits, not work. Running the tests is never refused, and nor
is reading one, with the file tools or with `cat`, `grep` or `head`.

What the hook refuses is any way of changing a frozen file: an edit with the
file tools, or a shell command that writes, moves or deletes it. Commands that
put a frozen test beside `sed`, `awk`, `perl` or another interpreter are held
to a narrower rule than plain reading, because each of those can write through
a program of its own that the hook cannot read.
[Enforcement](../enforcement.md#frozen-test-through-the-tool) lists what is
let through.

When a refusal of this kind turns up in your session, nothing needs fixing. It
is the agent being told to change the code and not the test.

## Where to go next

- [Human approval gates](human-approval-gates.md): answering a hand-over, and
  where to type the answer when you are not at a terminal
- [Stopping and resuming a story](stopping-and-resuming-a-story.md): the freeze
  stays with a story that is stopped
- [`sdlc freeze`](../commands.md#sdlc-freeze) and
  [`sdlc unfreeze`](../commands.md#sdlc-unfreeze): the reference for both
  commands
- [Enforcement](../enforcement.md#frozen-test-is-not-edited): the rule that
  keeps agents out of frozen tests
