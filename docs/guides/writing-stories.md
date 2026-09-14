# Writing stories the loop can run

The loop works on one story at a time: one iteration, one commit. This guide
covers where stories live and what one may contain. It shows how to write
acceptance criteria that can become frozen tests, how the next story is
chosen, and what stops a story before any work starts.

## The backlog file

Stories live in the file named by `backlog.path` in `.sdlc/config.json`.
`sdlc init` sets it to `user_stories.json` at the repository root and puts one
example story in it.

```json
{
  "_schema": ".sdlc/templates/story.schema.json",
  "stories": []
}
```

`_schema` records which schema the file follows. The schema itself is the JSON
Schema file `init` wrote, and you can point your editor at it.

`sdlc` itself checks less than the schema does. When it reads the backlog it
requires valid JSON and, on every story, an `id`, a `title` and a `status` from
the list under [Statuses](#statuses), spelled exactly. The id must be letters,
digits, dots, dashes and underscores, starting with a letter or a digit. Break
any of those and every command that reads the backlog stops with
[SDLC-E0008](../troubleshooting.md#sdlc-e0008) or
[SDLC-E0015](../troubleshooting.md#sdlc-e0015), naming the story. A misspelt
status is refused rather than ignored, because a story whose status the loop
does not know would never be picked. The rest of the schema, such as the
`PREFIX-123` shape of an id, is not checked.

The tool also writes this file. `sdlc start`, `sdlc escalate`, `sdlc approve`
and the gates change a story's `status` and set its `updated` time. They edit
those two values in place and leave every other byte as you wrote it: the key
order, the indentation, and any key the schema does not name. Reviews are
stamped without those two fields, so the loop moving a story along does not
make an approval stale. Changing anything else in the story does, the
acceptance criteria above all.

## A story

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

| Field | Required | What it is for |
| --- | --- | --- |
| `id` | yes | the schema asks for `PREFIX-123`; it also becomes `.sdlc/stories/<id>/` |
| `title` | yes | what the commit and the retro refer to |
| `status` | yes | where the story stands; see [Statuses](#statuses) |
| `acceptance_criteria` | to pass Gate 1 | the behaviour the tests will pin down, as `{id, text}` objects |
| `priority` | no | an integer; lower runs first |
| `depends_on` | no | ids of stories that must be `done` first |
| `as_a`, `i_want`, `so_that` | no | the user-story sentence |
| `risk_tier` | no | `low`, `medium` or `high`; none is `low`, and by default `high` waits for a person before it is committed ([`human_gates`](../configuration.md#human_gates)) |
| `non_goals`, `notes` | no | what is deliberately out of scope, and anything else |
| `decisions` | no | human choices made about the story, as `{at, by, text}` |
| `created`, `updated` | no | timestamps; the tool sets `updated` when it changes the status |

Edit the backlog between stories. While a story is running, the hook refuses
any attempt by the assistant to write it
([`backlog-is-not-edited`](../enforcement.md#backlog-is-not-edited)), so the
criteria the tests are held to and the tier that decides who approves the commit
stay the ones the story started with.

## Acceptance criteria that become tests

At Gate 3 the test author turns every criterion into at least one test that
fails now. `sdlc freeze` then records those tests by content, and nobody edits
them for the rest of the story. So a criterion has to say something a test can
observe before the code exists.

- **Describe behaviour, not implementation.** "The handler shall use
  `TokenValidator`" names a class. "The API shall respond 401" names something
  a caller sees.
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

## Priority and dependencies

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

`sdlc start AUTH-4` names a story directly. That puts it ahead of the priority
order and changes nothing else. A story that is `dropped`, `blocked` or `done`,
or that waits on a dependency that is not done, is refused with
[SDLC-E0010](../troubleshooting.md#sdlc-e0010), and the message says which.

## Statuses

| Status | Picked by `sdlc start` | Set by |
| --- | --- | --- |
| `todo` | yes, exactly like `ready` | you |
| `ready` | yes | you |
| `in_progress` | resumed first | `sdlc start` and `sdlc approve`; also a gate recorded as failed on a story that was done |
| `awaiting_human` | resumed first, once a person has answered | `sdlc escalate`, and the loop when it hands a story to a person |
| `blocked` | no | you |
| `done` | no | the tool, when the last gate passes |
| `dropped` | no | you |

Because `todo` is picked like `ready`, mark a story you are not ready to run
as `blocked` until you are.

## What the definition-of-ready gate refuses

Gate 1, `dor`, is mostly a judgement. The one thing `sdlc gate dor pass` checks
for itself is that the story has at least one acceptance criterion with `text`.
Without one it refuses with [SDLC-E0044](../troubleshooting.md#sdlc-e0044). With
[`human_gates.dor_advocate_check`](../configuration.md#human_gates) on, it also
waits for the human advocate's review.

The rest is the runbook reading each criterion the way a test author would. It
records a failure when one names an implementation rather than an observable
behaviour, or cannot be turned into a failing test. A failure is always
recordable:

```console
$ sdlc gate dor fail --note "AC-2 describes an implementation, not a behaviour"
```

Then it hands the question to a person with `sdlc escalate spec_unclear`,
rather than inventing an interpretation.

Problems with a story can still surface after Gate 1:

- **Gate 2, `analysis`,** fails when the researcher finds questions the
  specification does not settle, or recommends a split because the story will
  not fit `thresholds.diff_size_cap`.
- **Gate 3, `tests_frozen`,** fails when the test author reports a criterion it
  cannot test.
- **Gates 5 to 7** refuse a pass once the change grows past
  `thresholds.diff_size_cap` ([SDLC-E0042](../troubleshooting.md#sdlc-e0042)),
  and the runbook asks a person how to split the story.

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
