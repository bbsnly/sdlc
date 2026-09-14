# Stopping and resuming a story

A story does not have to be finished in one sitting. You can put it down at any
gate, close the session, and pick it up tomorrow where it stopped. This page
shows how, what is kept while it is down, and how `sdlc stop` gets you out when
the loop's own state is broken.

## Stop the iteration

```console
$ sdlc stop
Stopped the iteration on US-001. Nothing was recorded; `sdlc start` picks it up again.
```

Stopping ends the iteration and nothing else. No gate changes, the story stays
`in_progress` in the backlog, and the gate record gets one `loop_end` event.

It also turns the hook off. Every rule in [enforcement](../enforcement.md) only
applies while `.sdlc/state/active` names a story, so once you have stopped you
can edit code, tests or configuration as yourself. That is the sanctioned route
when the loop refuses something and you want to take over.

## Start again

With nothing running, `sdlc status` names the story that `sdlc start` would pick
up. A story already under way comes before anything new:

```console
$ sdlc status
No iteration running.

  backlog  1 in progress
  next up  US-001  Example: reject invoices with a non-positive total

Run `sdlc start` to begin.

$ sdlc start
Resumed US-001  Example: reject invoices with a non-positive total

  dor              pass  criteria are testable
  analysis         pass  no trust boundary
  tests_frozen     pass  2 criteria, 1 failing test
  plan             pass  2 steps
  design_review    pass  architect approved round 2
  implementation   pass
  verification     pass
  verifier_review  pass

Next: run /sdlc:next in Claude Code to work the story.
```

The loop picks up at the first gate that has not passed, here `code_review`.
You do not have to work that out. `sdlc start --json` reports it:

```console
$ sdlc start --json
{"ok":true,"story":"US-001","title":"Example: reject invoices with a non-positive total","resume":true,"next_gate":"code_review"}
```

`/sdlc:next` reads those two fields. Whether you run `sdlc start` yourself or
leave it to the skill, a story with `"resume": true` goes straight to
`next_gate`, and the gates it already passed are not worked again. Once the story
is active, `sdlc status` shows the same gate on its `next` line, and
`sdlc status --json` as `next_gate`.

A resumed story is not held to the trunk checks a new one is. `sdlc start`
refuses to begin a new story from a trunk that is on another branch, has
uncommitted work, is behind `origin` or fails its smoke check
([SDLC-E0038](../troubleshooting.md#sdlc-e0038) to
[SDLC-E0041](../troubleshooting.md#sdlc-e0041)). A story already in progress has
its own work in the tree, so those checks are skipped when it is picked up.

A story handed to a person with `sdlc escalate` is not stopped, it is waiting.
`sdlc status` lists it under `waiting`, and `sdlc start` refuses it
([SDLC-E0036](../troubleshooting.md#sdlc-e0036)) until a person answers with
`sdlc approve` in their own terminal.

## What a stop keeps

| Kept | Where |
| --- | --- |
| every gate outcome and its note | `.sdlc/stories/<ID>/gate-record.json` |
| every review, every round | the record, and `reviews/` beside it |
| the gate documents | `ANALYSIS.md`, `PLAN.md` and the rest, beside the record |
| the test freeze | `.sdlc/state/tests.lock` |
| spend recorded with `sdlc cost add` | the record |

The freeze is the one worth understanding. It belongs to the story, not the
session, so an unfinished story keeps its tests locked while it is down.
Stopping and starting again is not a way round it.

That also means a second story cannot freeze its own tests while the first one's
freeze is on disk. You can `sdlc start OTHER-1` while US-001 is still in
progress, provided trunk passes the checks above, but OTHER-1's `sdlc freeze`
then fails with [SDLC-E0022](../troubleshooting.md#sdlc-e0022), naming US-001.
`sdlc unfreeze` will not help: it refuses to lift a freeze that belongs to
another story. Finish US-001 (`sdlc stop`, then `sdlc start US-001`), or mark it
`dropped` in the backlog, which releases its freeze.

## When reviews go stale

Stopping and resuming does not make any review stale. `sdlc stop` on an
unfinished story writes only under `.sdlc/`, which is left out of what reviews
are stamped with. `sdlc start` on a story that is already `in_progress` writes
nothing to the backlog.

What you do while the story is down is another matter. A review at
`verifier_review` or `code_review` is stamped with the whole working tree, minus
`.sdlc/`. Touch any file git can see and those approvals no longer count. That
includes the backlog file: `sdlc` leaves out the `status` and `updated` fields it
writes itself, but changing a story's title or its criteria by hand changes the
tree. A design review is stamped with `PLAN.md`, so only a changed plan makes it
stale.

```console
$ sdlc review list --gate verifier_review
  verifier_review verifier        blocking  approve (round 1, stale -- what was reviewed has changed since)
```

A stale review is simply not counted. The gate refuses to pass until that
reviewer looks again. See
[why approvals go stale](../the-loop.md#why-approvals-go-stale).

## In a new Claude Code session

Nothing the loop needs lives in the conversation. A fresh session running
`/sdlc:next` reads `sdlc status --json`, and if a story is active it goes
straight to `next_gate`. The hook reads the same files on every tool call, so
the freeze and the write rules apply from the session's first edit.

If you closed the old session without stopping, the iteration is still active
and there is nothing to do but run `/sdlc:next`. The exception is a session that
kept ending its turn mid-story: after `loop.max_stop_blocks` stops in a row with
nothing recorded, the `Stop` hook hands the story to a person. `sdlc status`
then lists it as waiting, and it needs `sdlc approve` before it can start again.

## When the story is finished

Passing the retro marks the story `done` in the backlog. `sdlc status` then
reads `finished` instead of `in progress`, and stopping is what puts the story
away:

```console
$ sdlc stop
Ended the iteration on US-001. Every gate passed, so the story is done and `sdlc start` moves on to the next one.
```

This is the one stop that changes something: the story's freeze is lifted, so
the next story can take its own. A finished story cannot be started again
([SDLC-E0033](../troubleshooting.md#sdlc-e0033)). The way back in is to record a
gate as failed, which puts the story back to `in_progress`.

## Getting out of a broken state

`sdlc stop` is built to work when other commands will not.

### The gate record will not read

A damaged `gate-record.json` stops `sdlc status`, and the hook refuses
`git commit` because no gate can be shown to have passed:

```console
$ sdlc status
sdlc: the record for "US-001" is not valid JSON

  why  .sdlc/stories/US-001/gate-record.json does not parse, and sdlc writes it whole or not at all, so it was edited or merged by hand; git has the last committed copy
  fix  run "sdlc doctor", which names every file it cannot read; restore a committed one from git, or fix its permissions — and if sdlc left it that way, open an issue at https://github.com/bbsnly/sdlc/issues

  SDLC-E0005  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0005

$ sdlc stop
sdlc: .sdlc/stories/US-001/gate-record.json could not be read, so this stop is not written to it. Run `sdlc doctor` to see why.
Stopped the iteration on US-001. Nothing was recorded; `sdlc start` picks it up again.
```

The iteration ends and the hook stands down, so you can commit as yourself.
Stopping does not repair the record: `sdlc start` refuses with the same
SDLC-E0005 until the file reads again. If you committed the story's directory,
restore the file from git.

`sdlc doctor` names the unreadable record under "loop state" whether or not the
story is active, because it reads every story's record on disk. Before the stop
its fix is about the commit; after it, that `sdlc start US-001` fails until the
file reads.

### The story is not in the backlog

If the active story's id disappears from the backlog, whether renamed or
deleted, `sdlc status` says so, and `sdlc gate` refuses to record against it:

```console
$ sdlc status
US-001  in progress  (not in the backlog)
...
US-001 is not in user_stories.json any more, so it cannot be committed. Put it back, or run `sdlc stop` to end the iteration.

$ sdlc gate code_review pass
sdlc: no story with the id "US-001"

  why  the backlog has US-002
  fix  run "sdlc story list" to see the ids you can use

  SDLC-E0009  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0009

$ sdlc stop
Stopped the iteration on US-001. Nothing was recorded; `sdlc start` picks it up again.
```

`sdlc status --json` reports it as `"not_in_backlog": true`. The commit gate
refuses such a story too, because its risk tier cannot be read. Stop still ends
the iteration. Put the story back under its old id to carry on with its record,
or `sdlc start` the next one.

## Where to go next

- [Commands: `sdlc stop`](../commands.md#sdlc-stop) and
  [`sdlc start`](../commands.md#sdlc-start)
- [When a reviewer blocks](when-a-reviewer-blocks.md)
- [When a frozen test is wrong](when-a-frozen-test-is-wrong.md)
- [Enforcement](../enforcement.md): which rules turn off when you stop
