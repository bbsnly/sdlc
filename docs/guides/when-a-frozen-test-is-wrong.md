# When a frozen test is wrong

Sometimes an acceptance test asserts something the criterion never said. The
freeze exists so that nobody quietly edits a test to reach green, which also
means nobody edits one quietly when it genuinely is wrong. This page shows how
to change a frozen test on the record, and what that costs the gates after it.

## What the freeze is

At Gate 3, after the tests are written and failing, `sdlc freeze` records the
sha256 of every file that `paths.tests` in `.sdlc/config.json` calls a test. It
asks git which files are in the working tree, so an untracked test counts and an
ignored one does not:

```console
$ sdlc freeze
US-001  1 test file frozen
  internal/invoice/invoice_test.go
```

The freeze is written to `.sdlc/state/tests.lock`, with the story it belongs to.
It holds across `sdlc stop` and across sessions, and it is lifted when a
finished story is stopped. See
[Gate 3](../the-loop.md#gate-3--tests_frozen).

## What gets refused

Any edit to a frozen file through Claude Code's file tools is refused, whoever
the agent is. The refusal names the way out:

```text
internal/invoice/invoice_test.go is a frozen acceptance test. It was locked by
content when the test gate passed, and an agent that can edit its own tests will
eventually edit them -- which makes every gate after this one theatre. Instead:
change the code until the test passes; if the test itself is wrong, say which
acceptance criterion it got wrong and stop: the person running the session lifts
the freeze with `sdlc unfreeze --reason "..."`, which puts the change on the
record [frozen-test-is-not-edited]
```

The shell is covered too. Once the tests are frozen, the hook refuses:

| A tool call that | Rule |
| --- | --- |
| writes, moves or deletes a frozen file from the shell | `frozen-test-through-the-tool` |
| creates a new test file, with the file tools or the shell, unless the project allows it | `no-new-test-after-the-freeze` |
| writes a test file as the implementer before the freeze, or with `freeze.allow_new_test_files` on | `implementer-does-not-write-tests` |
| runs `sdlc unfreeze` | `unfreeze-is-a-human-decision` |

Reading tests and running them is never refused.

The command checks too, for whatever reached the tree some other way. A pass at
`tests_frozen`, `plan`, `implementation`, `verification` or `commit` refuses if
a frozen file changed or vanished
([SDLC-E0025](../troubleshooting.md#sdlc-e0025)), if a test file is there that
the freeze does not hold ([SDLC-E0043](../troubleshooting.md#sdlc-e0043)), or if
the freeze is gone or belongs to another story
([SDLC-E0023](../troubleshooting.md#sdlc-e0023)).

## Decide whether the test is wrong

Changing the code is the default. Before touching the test, name the acceptance
criterion it contradicts and say how. "The test is too strict" is not that.
"AC-1 says the invoice is returned in state Draft, and the test asserts a total
of 42" is.

The `/sdlc:next` runbook tells the assistant to do exactly this, hand it to you
with `sdlc escalate frozen_test_wrong --message "..."`, and stop. It cannot lift
the freeze itself: the hook refuses `sdlc unfreeze` from any tool call. The
decision is yours, and you make it in your own terminal.

## Lift the freeze, on the record

`sdlc unfreeze` acts on the story being worked on. An escalation ended the
iteration, and `sdlc start` refuses a story that is waiting for a person, so
answer it first and pick the story up again:

```console
$ sdlc approve US-001
US-001  approved (frozen_test_wrong)

A person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.

$ sdlc start US-001
```

If you are driving the story yourself and nothing was escalated, it is still
active and you can go straight on.

`sdlc unfreeze` refuses without a reason:

```console
$ sdlc unfreeze
sdlc: lifting the freeze needs a reason

  why  the freeze is what stops an agent editing its way to green, so lifting it belongs on the record
  fix  pass --reason with the one line that explains why the tests have to change

  SDLC-E0026  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0026

$ sdlc unfreeze --reason "AC-1's test expects 42; the criterion says Draft state"
US-001  freeze lifted on 1 test file
  AC-1's test expects 42; the criterion says Draft state
  reopened: tests_frozen, plan, design_review, implementation
```

The reason becomes an `unfreeze` event in the story's gate record, next to the
`freeze` it undid. That is what the retro and any later reader see.

## Change the test, then freeze again

With the freeze lifted, the separation of duties still holds. The implementer
may never write a test file, and the main conversation writes no code or tests
during an iteration. Either delegate the change to `sdlc:sdet`, or run
`sdlc stop` in your own terminal, edit the test yourself, and `sdlc start` again.

Then freeze again, with the story active, and pass the gate the unfreeze
reopened:

```console
$ sdlc freeze
US-001  1 test file frozen
  internal/invoice/invoice_test.go

$ sdlc gate tests_frozen pass --note "AC-1's test corrected"
```

The new freeze covers every file `paths.tests` matches at that moment, including
any file added while the old one was lifted. `sdlc freeze` refuses while this
story's freeze is still on disk
([SDLC-E0022](../troubleshooting.md#sdlc-e0022)), so unfreeze always comes
first.

## What it costs later gates

Lifting the freeze sends the story back to Gate 3. The tests are about to
change, so `tests_frozen` no longer stands, and nor does any gate passed on top
of it: each goes back to `pending` with your reason as its note, and
`sdlc unfreeze` lists them under `reopened`. `sdlc status` shows `tests_frozen`
as the next gate, so the next `/sdlc:next` starts there, gives the test author
your reason, and freezes again once the test is corrected.

The gates after it have to pass again, in order. Not all of their work has to
be done again:

- **Design review approvals stand**, as long as `PLAN.md` does not change. They
  are stamped with the plan, not with the tests, so `design_review` passes again
  on the reviews it already has.
- **Verifier and code review approvals go stale.** They are stamped with the
  working tree, and a changed test file changes it. Both gates need their
  reviewers again, and both the hook and the commit gate check that before a
  commit goes through.
- **A corrected test that changes what the plan has to do** needs a new plan.
  Change `PLAN.md` before `plan` passes again, and the design reviewers look at
  the new one.

## Allowing new test files after the freeze

By default no new test file can be added once the tests are frozen. The hook
refuses one written by an agent, and a test file that reaches the tree some
other way holds every gate from `tests_frozen` to `commit` with SDLC-E0043. The
route is to unfreeze, add it, and freeze again, so the new file is covered.

`freeze.allow_new_test_files` in `.sdlc/config.json` loosens that for the test
author. With it on, `sdlc:sdet` may write a new test file after the freeze. The
implementer is still refused, because that rule is not a setting.

A new file is not frozen by being written. Until you run `sdlc freeze` again,
the same gates refuse with SDLC-E0043. With the setting on, that second
`sdlc freeze` adds the new files to the existing freeze and lists them, instead
of refusing as already frozen. It adds nothing if a frozen file has changed, and
refuses with SDLC-E0025.

Know what you are getting. A test written after the implementation can be
written to pass. See
[`freeze.allow_new_test_files`](../configuration.md#freezeallow_new_test_files).

## Where to go next

- [Commands: `sdlc freeze`](../commands.md#sdlc-freeze) and
  [`sdlc unfreeze`](../commands.md#sdlc-unfreeze)
- [Enforcement: `frozen-test-is-not-edited`](../enforcement.md#frozen-test-is-not-edited)
- [When a reviewer blocks](when-a-reviewer-blocks.md)
- [Stopping and resuming a story](stopping-and-resuming-a-story.md)
