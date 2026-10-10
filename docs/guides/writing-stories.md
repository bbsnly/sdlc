# Writing stories the loop can run

- [Introduction](#introduction)
- [A story](#a-story)
  - [A fuller story](#a-fuller-story)
  - [Fields](#fields)
- [Statuses](#statuses)
- [Acceptance criteria that become tests](#acceptance-criteria-that-become-tests)
- [The backlog file](#the-backlog-file)
  - [What `sdlc` checks](#what-sdlc-checks)
  - [What the tool writes](#what-the-tool-writes)
  - [Editing while a story runs](#editing-while-a-story-runs)
- [Priority and dependencies](#priority-and-dependencies)
  - [How the next story is chosen](#how-the-next-story-is-chosen)
  - [Dependencies that never finish](#dependencies-that-never-finish)
  - [More than one checkout](#more-than-one-checkout)
  - [Naming a story](#naming-a-story)
- [What the definition-of-ready gate refuses](#what-the-definition-of-ready-gate-refuses)
  - [Problems that surface later](#problems-that-surface-later)
- [Where to go next](#where-to-go-next)

## Introduction

The loop takes one story at a time, and the quality of what comes out depends a great
deal on what goes in. A story whose criteria are vague gives the test author nothing to
pin down, and the first gate will turn it away.

Each checkout, and each Claude Code session, works on one story at a time: one
iteration, one commit. This guide shows you how to write a story, and how to write
acceptance criteria that can become frozen tests. It then explains where stories live,
how the next story is chosen, and what stops a story before any work starts.

Don't worry about the whole schema to begin with. A title and one good criterion are
enough for your first story.

## A story

The smallest story the loop can run has an `id`, a `title`, a `status` and one
acceptance criterion:

```json
{
  "id": "AUTH-3",
  "title": "Reject expired sessions",
  "status": "ready",
  "acceptance_criteria": [
    {
      "id": "AC-1",
      "text": "WHEN a request carries a session token whose expiry is in the past, the API shall respond 401 and shall not read the session store."
    }
  ]
}
```

Everything else is optional. The `ready` status tells `sdlc start` that the story may
be picked, and the criterion is what the tests will be held to. The next section shows
how to write criteria like this one.

### A fuller story

In addition, a story can say who it is for, how urgent it is, what it depends on and
what it leaves out:

```json
{
  "id": "AUTH-3",
  "title": "Reject expired sessions",
  "as_a": "API client",
  "i_want": "requests with an expired session to be refused",
  "so_that": "a leaked old token cannot be replayed",
  "priority": 1,
  "status": "ready",
  "risk_tier": "high",
  "depends_on": ["AUTH-1"],
  "acceptance_criteria": [
    {
      "id": "AC-1",
      "text": "WHEN a request carries a session token whose expiry is in the past, the API shall respond 401 and shall not read the session store."
    },
    {
      "id": "AC-2",
      "text": "IF the token cannot be parsed, THEN the API shall respond 401 with a body that does not say why."
    }
  ],
  "non_goals": ["refresh tokens", "rate limiting"]
}
```

### Fields

| Field | Required | What it is for |
| --- | --- | --- |
| `id` | yes | the schema asks for `PREFIX-123`; it also becomes `.sdlc/stories/<id>/` |
| `title` | yes | what the commit and the retro refer to |
| `status` | yes | where the story stands; see [Statuses](#statuses) |
| `acceptance_criteria` | the schema requires it; the loop needs one with `text` to pass Gate 1 | the behaviour the tests will pin down, as `{id, text}` objects |
| `priority` | no | an integer; lower runs first |
| `depends_on` | no | ids of stories that must be `done` first |
| `as_a`, `i_want`, `so_that` | no | the user-story sentence |
| `risk_tier` | no | `low`, `medium` or `high`; none is `low`, and by default `high` waits for a person before it is committed ([`human_gates`](../configuration.md#human_gates)) |
| `non_goals`, `notes` | no | what is deliberately out of scope, and anything else |
| `decisions` | no | human choices made about the story, as `{at, by, text}` |
| `created`, `updated` | no | timestamps; the tool sets `updated` when it changes the status |

## Statuses

Every story has one of seven statuses. You set some of them yourself, and the tool sets
the rest as the story moves along.

| Status | Picked by `sdlc start` | Set by |
| --- | --- | --- |
| `todo` | yes, exactly like `ready` | you |
| `ready` | yes | you |
| `in_progress` | resumed first, in the checkout that holds it | `sdlc start` and `sdlc approve`; also a gate recorded as failed on a story that was done |
| `awaiting_human` | resumed first, once a person has answered, in the checkout that holds it | `sdlc escalate`, and the loop when it hands a story to a person |
| `blocked` | no | you |
| `done` | no | the tool, when the last gate passes; for a story in its own worktree, when `sdlc stop` lands it on trunk |
| `dropped` | no | you |

Because `todo` is picked like `ready`, mark a story you are not ready to run
as `blocked` until you are.

## Acceptance criteria that become tests

At Gate 3 the test author turns every criterion into at least one test that fails now.
`sdlc freeze` then records those tests by content, and nobody edits them for the rest
of the story. So a criterion has to say something a test can observe before the code
exists.

Let's take a look at the difference. "The handler shall use `TokenValidator`" names a
class, and a test can only check how the code is built. "The API shall respond 401"
names something a caller sees, and a test can check it directly. The second is the kind
of criterion to aim for.

Each criterion is one object with an `id` and a `text`:

```json
{
  "id": "AC-1",
  "text": "WHEN a request carries a session token whose expiry is in the past, the API shall respond 401 and shall not read the session store."
}
```

A few habits make criteria easier to test:

- **Name the exact outcome.** Give the status code, the error value, the stored
  state. "Shall handle bad input gracefully" gives a test nothing to assert.
- **One behaviour per criterion.** A criterion that fails for two reasons
  becomes a test that passes for one.
- **Write the whole criterion in `text`.** It is the only field of a criterion
  the loop reads. A criterion written as separate `given`, `when` and `then`
  fields is empty to it, and a story with no criterion text cannot pass Gate 1.
- **Use EARS form.** The schema lists the patterns `WHEN`, `WHILE`,
  `IF ... THEN`, `WHERE` and plain "the system shall". An optional `type`
  field can record which one you used; nothing in the loop reads it.
- **Keep `AC-n` ids stable.** The contract template's test conventions ask
  for the id in the name of the test that covers it, so every test can be
  traced back to its criterion.

You do not need a criterion for every edge. The test author adds boundary cases
itself. Write what is deliberately excluded in `non_goals`, so the reviewers do
not flag its absence.

## The backlog file

Stories live in the file named by `backlog.path` in `.sdlc/config.json`.
`sdlc init` sets it to `user_stories.json` at the repository root and puts one
example story in it. An empty backlog looks like this:

```json
{
  "_schema": ".sdlc/templates/story.schema.json",
  "stories": []
}
```

`_schema` records which schema the file follows. The schema itself is the JSON
Schema file `init` wrote, and you can point your editor at it.

### What `sdlc` checks

`sdlc` itself checks less than the schema does. When it reads the backlog it
requires valid JSON and, on every story, an `id`, a `title` and a `status` from
the list under [Statuses](#statuses), spelled exactly. The id must be letters,
digits, dots, dashes and underscores, starting with a letter or a digit. Break
any of those and every command that reads the backlog stops with
[SDLC-E0008](../troubleshooting.md#sdlc-e0008) or
[SDLC-E0015](../troubleshooting.md#sdlc-e0015), naming the story.

A misspelt status is refused rather than ignored, because a story whose status
the loop does not know would never be picked. The rest of the schema, such as
the `PREFIX-123` shape of an id, is not checked.

### What the tool writes

The tool also writes this file. `sdlc start`, `sdlc escalate`, `sdlc approve`
and the gates change a story's `status` and set its `updated` time. They edit
those two values in place and leave every other byte as you wrote it: the key
order, the indentation, and any key the schema does not name.

Reviews are stamped without those two fields, so the loop moving a story along
does not make an approval stale. Changing anything else in the story does, the
acceptance criteria above all.

### Editing while a story runs

Edit the backlog between stories. While a story is running, the hook refuses
any attempt by the assistant to write it
([`backlog-is-not-edited`](../enforcement.md#backlog-is-not-edited)), so the
criteria the tests are held to and the tier that decides who approves the commit
stay the ones the story started with.

## Priority and dependencies

Most of the time you only need to set `priority` and, where one story needs another,
`depends_on`. Then run `sdlc start` and the loop picks for you. The rest of this
section explains exactly how it chooses, for when the order surprises you.

### How the next story is chosen

`sdlc start` with no argument picks a story like this:

1. A story that is `in_progress` or `awaiting_human` is resumed. This comes
   before everything else. One that is waiting for a person is refused with
   [SDLC-E0036](../troubleshooting.md#sdlc-e0036) until someone answers with
   `sdlc approve`.
2. Otherwise, it considers stories that are `ready` or `todo` and whose every
   `depends_on` entry is `done`.
3. The lowest `priority` wins. The schema gives `0` to fix-forward work. A
   story with no priority counts as 999, so it sorts after every story with a
   lower number.
4. Ties go to the id, with the numbers in it read as numbers, so `AUTH-2` comes
   before `AUTH-10`.

### Dependencies that never finish

A dependency that never reaches `done` holds the story back forever. That
includes an id that is not in the backlog and a story that was `dropped`.
`sdlc story list` names every unfinished dependency, and says when one will
never be done:

```console
$ sdlc story list
  AUTH-1  done      1  Issue session tokens
> AUTH-3  ready     1  Reject expired sessions
  AUTH-4  ready     2  Revoke sessions on logout  (waiting on AUTH-9 (not in the backlog))
  AUTH-5  blocked   3  Rotate signing keys

> is what `sdlc start` would pick.
```

### More than one checkout

In a repository with linked worktrees, every checkout picks from the one
backlog, kept in the main checkout, and passes over a story another checkout
holds: one it started and has not finished, whether running, stopped or waiting
for a person. A story that depends on a held story waits, as it does on any story
that is not done. So two checkouts that start at once never pick the same story.

`sdlc story list` says which checkout holds each story, and marks what start
would pick in the checkout you run it in. With
[`git.worktrees`](../configuration.md#git) on, the default, every new story gets
a worktree of its own, so this is every repository once a story has started. A
story that depends on another is cut from trunk, so it starts only once that
story is done, and sees that story's work only once a person has brought it to
trunk ([more than one checkout](../the-loop.md#more-than-one-checkout)).

### Naming a story

`sdlc start AUTH-4` names a story directly. That puts it ahead of the priority
order and changes nothing else. A story that is `dropped`, `blocked` or `done`,
or that waits on a dependency that is not done, is refused with
[SDLC-E0010](../troubleshooting.md#sdlc-e0010), and the message says which.

## What the definition-of-ready gate refuses

`sdlc gate dor pass` checks one thing for itself: that the story has at least one
acceptance criterion with `text`. Without one it refuses with
[SDLC-E0044](../troubleshooting.md#sdlc-e0044). With
[`human_gates.dor_advocate_check`](../configuration.md#human_gates) on, it also
waits for the human advocate's review.

Beyond that, Gate 1, `dor`, is mostly a judgement. The runbook reads each
criterion the way a test author would. It records a failure when one names an
implementation rather than an observable behaviour, or cannot be turned into a
failing test. A failure is always recordable:

```console
$ sdlc gate dor fail --note "AC-2 describes an implementation, not a behaviour"
```

Then it hands the question to a person with `sdlc escalate spec_unclear`,
rather than inventing an interpretation.

### Problems that surface later

Problems with a story can still surface after Gate 1:

- **Gate 2, `analysis`,** fails when the researcher finds questions the
  specification does not settle.
- **Gate 3, `tests_frozen`,** fails when the test author reports a criterion it
  cannot test.

In each case the runbook records the failure and hands the question to a person
with `sdlc escalate`. That ends the iteration and marks the story
`awaiting_human`. Fix the story in the backlog, answer with `sdlc approve` in
your own terminal, and run `/sdlc:next` again. It resumes at the first gate that
has not passed.

## Where to go next

- [The loop](../the-loop.md#gate-1--dor-definition-of-ready): what each gate
  does with the story
- [`sdlc story list`](../commands.md#sdlc-story-list) and
  [`sdlc start`](../commands.md#sdlc-start)
- [Tuning commands for your stack](tuning-commands-for-your-stack.md): making
  sure the frozen tests are the right files
- [Troubleshooting](../troubleshooting.md#sdlc-e0010): when nothing can start
