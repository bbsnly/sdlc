# Stopping and resuming a story

- [Introduction](#introduction)
- [Pick up where you left off](#pick-up-where-you-left-off)
  - [See where a story got to](#see-where-a-story-got-to)
- [Put a story down](#put-a-story-down)
- [Take over from the loop](#take-over-from-the-loop)
- [What a stopped story keeps](#what-a-stopped-story-keeps)
- [When the story is finished](#when-the-story-is-finished)
  - [Keep a finished story off trunk](#keep-a-finished-story-off-trunk)
  - [Reopen a finished story](#reopen-a-finished-story)
- [Move a story to another checkout](#move-a-story-to-another-checkout)
- [When something is broken](#when-something-is-broken)
- [With worktrees turned off](#with-worktrees-turned-off)
- [Where to go next](#where-to-go-next)

## Introduction

A story does not have to be finished in one sitting. Sessions end, laptops
close, and sometimes you want to take the work over from the loop for a while.
Don't worry: nothing is lost when that happens.

Everything the loop knows about a story is on disk, not in the conversation.
You can leave a story at any gate and pick it up tomorrow, in a new session, at
the gate where it stopped.

This page shows how to pick a story up again, how to put one down on purpose
and work on it yourself, and what happens when a story is finished and lands on
trunk.

## Pick up where you left off

If a session ended part-way through a story, open Claude Code in your project
and type `/sdlc:next` with the story's id:

```text
/sdlc:next AUTH-3
```

The loop finds the first gate that has not passed and carries on from there.
The gates the story has already passed are not worked again.

The id matters. Each story is worked in a worktree of its own, a second
checkout at `.sdlc/worktrees/AUTH-3`, and a plain `/sdlc:next` typed in a new
session in your project begins the next `ready` story instead of going back to
this one. `/sdlc:next` alone is enough in two places: the session that is still
working the story, and a session opened in the story's worktree.

If you don't remember which story was under way, ask in your project:

```console
$ sdlc status
No iteration running.

  backlog  1 ready · 1 in progress
  held     AUTH-3  in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3"
  next up  AUTH-4  Log every rejected session

A person runs `sdlc start`, or types /sdlc:next in Claude Code, to begin.
```

The `held` line names the story under way and its worktree. `next up` is what a
plain `/sdlc:next` would begin.

> [!NOTE]
> If `status` lists the story under `waiting`, the loop handed it to you with a
> question, and it will not start until you have answered.
> [Human approval gates](human-approval-gates.md) shows how.

### See where a story got to

You may look before you set the loop going. Run `sdlc start` with the story's
id, in your project:

```console
$ sdlc start AUTH-3
Resumed AUTH-3  Reject expired sessions

Worktree: "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3"
Branch: story/AUTH-3

  dor              pass  criteria are testable
  analysis         pass  no trust boundary
  tests_frozen     pass  2 criteria, 1 failing test
  plan             pass  1 step
  design_review    fail  architect blocked: AC-2 has no step

Next: a person opens Claude Code in the worktree above and types /sdlc:next there to work the story; /sdlc:next typed in a new session in this checkout begins the next story instead.
```

Each gate the story has reached is listed with its outcome and the note that
was recorded with it. The loop carries on at the first one that has not passed,
here `design_review`.

You don't have to run this first, because `/sdlc:next AUTH-3` does it for you.
Its last line describes the other way to carry on, a session opened in the
worktree, where no id is needed.

## Put a story down

You don't need to do anything before you close a session or shut the laptop.
The story stays as it is, and `/sdlc:next AUTH-3` picks it up.

To put a story down on purpose, run `sdlc stop` in a terminal of your own, from
the story's worktree:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc stop
Stopped the iteration on AUTH-3. Nothing was recorded; `sdlc start` picks it up again.
```

Stopping ends the iteration and nothing else. No gate changes, and the story
stays in progress in your backlog.

Run it in the worktree, not in your project. A stop acts on the checkout it
runs in, and in your project it answers `No iteration was running.` and leaves
the story as it was.

> [!WARNING]
> Check that the story is not finished before you stop it. Once every gate has
> passed, `sdlc stop` in the story's worktree does not put the story down: it
> lands it on trunk. [When the story is finished](#when-the-story-is-finished)
> shows how to hold one back.

## Take over from the loop

Stopping also stands the hook down. Its rules apply only while a story is being
worked, and only to the session working it, so once you have stopped you may
edit code, tests or anything else in the worktree as yourself. This is the way
to take over when the loop refuses something you want done.

Let's change a line the reviewers have already approved. Stop the story, make
your change in the worktree, and from there ask where the reviews stand now:

```console
$ sdlc review list --story AUTH-3 --gate code_review
Review policy: REVIEW.md
  code_review     code-reviewer   blocking  approve (round 1, stale -- what was reviewed has changed since)
  code_review     security        advisory  approve (round 1, stale -- what was reviewed has changed since)
  code_review     perf            on budget approve (round 1, stale -- what was reviewed has changed since)
  code_review     human-advocate  advisory  approve (round 1, stale -- what was reviewed has changed since)
```

Stopping and resuming make no review stale. Your change did: the verifier and
the code reviewers approved the files as they were, and an approval of files
that have changed is not counted. The design reviewers approved the plan, so
only a changed plan makes their reviews stale.

Send your change back through the gates that check it, by recording the first
of them as failed:

```console
$ sdlc gate verification fail --story AUTH-3 --note "changed the expiry comparison after review"
AUTH-3  verification  fail
  changed the expiry comparison after review
  reopened: verifier_review, code_review
```

Then type `/sdlc:next AUTH-3`. The verifier and the code reviewers see your
change before the loop commits it. If you skip this step nothing slips through:
the commit is [refused](../troubleshooting.md#sdlc-e0029), naming the reviews
your change made stale.

Two things do not stand down with the hook. The tests stay frozen, and
[When a frozen test is wrong](when-a-frozen-test-is-wrong.md) shows the way to
change one. And the hook holds only the session that is working a story: any
other session open in your repository was never held to it.

## What a stopped story keeps

A stopped story keeps everything the loop has recorded about it:

| Kept | Where it is |
| --- | --- |
| every gate's outcome and its note | on the story's record; `sdlc start <ID>` prints them |
| the documents the gates wrote | `.sdlc/stories/<ID>/` in your project |
| every review, every round | `.sdlc/stories/<ID>/reviews/` in your project |
| the code | the story's worktree, `.sdlc/worktrees/<ID>` |
| the test freeze | in force in the story's worktree |
| what the story has cost | on the story's record; see [Tracking what a story costs](tracking-what-a-story-costs.md) |

The freeze is the one worth understanding. It belongs to the story, not to the
session, so an unfinished story keeps its tests frozen while it is down.
Stopping and starting again is not a way round it.

## When the story is finished

Once its last gate has passed, a story is finished, but it is not done yet:
trunk does not have its commits. The stop in its worktree is what lands it.
`/sdlc:next` runs that stop for you at the end of Gate 9, and this is what it
prints:

```console
$ sdlc stop
main moved from a3ff947e9908 to 1e2985e3a9bc by fast-forward; the files in "/home/<user>/src/billing", which has main checked out, moved with it.
The backlog and AUTH-3's record are changed in the main checkout and not committed.
Ended the iteration on AUTH-3. Every gate passed, so the story is done and `sdlc start` moves on to the next one.
Removed .sdlc/worktrees/AUTH-3, with the files git ignored in it, and story/AUTH-3.
```

Trunk, which this project calls `main`, now has the story's commits, and the
worktree and its branch are gone. Nothing is pushed. Two things are left in
your project for you to commit: the backlog, where the story is now `done`, and
the story's directory under `.sdlc/stories/`.

A landing that is refused leaves trunk where it was, and the story finished and
waiting in its worktree. The refusal says what to do. Where it says to stop
again, as it does when another story is still landing, when trunk moved while
the landing ran, or after a stop that was cut short, nothing is lost. The
session stops again, up to three times, and if the third stop does not land the
story either, it hands the story to a person.
[Done when it lands](../the-loop.md#done-when-it-lands) says what a landing
checks.

### Keep a finished story off trunk

A stop in the story's worktree always lands a finished story. To hold one back,
hand it to a person before that stop, from the worktree:

```console
$ sdlc escalate hold_landing --message "AUTH-3 is finished; it lands once the release branch is cut"
```

The first word is the kind of question, and `hold_landing` is a word of your
own choosing. The story now waits for your answer, and nothing reaches trunk
until it has one. When you are ready, answer with `sdlc approve AUTH-3` in the
worktree, and the next `sdlc stop` there lands the story.
[Human approval gates](human-approval-gates.md#answer) covers answering.

### Reopen a finished story

`sdlc` [refuses to start](../troubleshooting.md#sdlc-e0033) a story whose gates
have all passed. If a finished story that has not landed needs more work,
record the gate it goes back to as failed, in its worktree:

```console
$ sdlc gate code_review fail --story AUTH-3 --note "the 401 body is not empty"
AUTH-3  code_review  fail
  the 401 body is not empty
  reopened: commit, retro
```

The story is in progress again, and `/sdlc:next AUTH-3` takes it up at that
gate.

## Move a story to another checkout

A story stays with the checkout that started it, which is its own worktree.
You will rarely need to move one. The usual reason is that the worktree was
deleted by hand, after which every command on the story
[says so](../troubleshooting.md#sdlc-e0051) until you let the story go.

Releasing is a person's decision, so run it in a terminal of your own, or type
`/sdlc:release` with the same arguments in Claude Code:

```console
$ sdlc release AUTH-3 --reason "the worktree it was started in was deleted"
```

After that, `sdlc start AUTH-3` picks the story up in any checkout. Only the
hold moves. Work the old checkout had not committed stays where it was, so
commit it there or bring it across yourself before you carry on.

`release` refuses while the story is running. Stop it first, in the checkout
that holds it. [`sdlc release`](../commands.md#sdlc-release) has the details,
including what happens to the test freeze.

## When something is broken

`sdlc stop` is built to work when other commands will not. If the story's
record has been damaged, by a hand edit or a bad merge, `sdlc status` refuses
to read it, and the stop still gets you out:

```console
$ sdlc stop
sdlc: .sdlc/stories/AUTH-3/gate-record.json could not be read, so this stop is not written to it. Run `sdlc doctor` to see why.
Stopped the iteration on AUTH-3. Nothing was recorded; `sdlc start` picks it up again.
```

The iteration has ended and the hook has stood down, so the repository is yours
again. Stopping does not repair the file. `sdlc start` keeps
[refusing the story](../troubleshooting.md#sdlc-e0005) until the record reads,
and `sdlc doctor` names every file it cannot read. If you committed the story's
directory, restore the file from git.

The same holds when a story's id has gone from the backlog, renamed or deleted
while the story was running. `sdlc status` says the story cannot be committed
and offers two ways out: put the story back under its old id, or run
`sdlc stop`.

## With worktrees turned off

Everything above describes the default, where each story has a worktree of its
own. With [`git.worktrees`](../configuration.md#turning-worktrees-off) set to
false, stories are worked in your project itself, one at a time, and a few
things are simpler:

- Every command on this page is run in your project.
- A plain `/sdlc:next` picks up the story under way before anything new, so you
  don't need to name it.
- A story that is waiting for your answer holds the checkout. Nothing else
  starts there until you have answered.
- A story is `done` as soon as its last gate passes. `sdlc stop` then puts it
  away and lifts its test freeze, so that the next story can freeze its own.

## Where to go next

- [Human approval gates](human-approval-gates.md): when the story stopped
  because the loop wants an answer from you
- [When a reviewer blocks](when-a-reviewer-blocks.md) and
  [When a frozen test is wrong](when-a-frozen-test-is-wrong.md): the two stops
  you are most likely to meet
- [`sdlc stop`](../commands.md#sdlc-stop) and
  [`sdlc start`](../commands.md#sdlc-start): the reference for both commands
- [Enforcement](../enforcement.md): the rules that stand down when you stop
