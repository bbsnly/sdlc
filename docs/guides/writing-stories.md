# Writing stories

- [Introduction](#introduction)
- [Write your first story](#write-your-first-story)
- [Write criteria that can be tested](#write-criteria-that-can-be-tested)
  - [The sentence patterns](#the-sentence-patterns)
  - [Say what is out of scope](#say-what-is-out-of-scope)
- [Say more about a story](#say-more-about-a-story)
  - [Every field](#every-field)
- [Choose what runs next](#choose-what-runs-next)
  - [Hold a story back](#hold-a-story-back)
  - [Make one story wait for another](#make-one-story-wait-for-another)
  - [Start a story by name](#start-a-story-by-name)
- [Statuses](#statuses)
- [When the loop turns a story away](#when-the-loop-turns-a-story-away)
- [Editing the backlog](#editing-the-backlog)
- [Where to go next](#where-to-go-next)

## Introduction

A story tells the loop what you want built and how you will know it is done.
Everything the loop does afterwards is measured against it. The tests are
written from your acceptance criteria, the plan is reviewed against them, and
the verifier checks the finished code against them once more.

So the time you spend on a story is the best-spent time in the whole loop. A
vague criterion gives the test author nothing to pin down, and the first gate
turns the story away before any work starts.

This guide shows you how to write a story, and how to write acceptance criteria
that can become tests. Then it covers the optional fields, how the loop chooses
which story to run next, and what to do when a story is turned away. Don't
worry about the optional fields to begin with: a title and one good criterion
are enough for your first story.

## Write your first story

Your stories live in the backlog, `user_stories.json`, at the root of your
repository. `sdlc init` put one example story in it. Open the file and replace
the example with a story of your own:

```json
{
  "_schema": ".sdlc/templates/story.schema.json",
  "stories": [
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
  ]
}
```

That is the smallest story the loop can run. It has four parts:

- **`id`** names the story everywhere: in commands, in the commit, and in the
  directory that keeps its record. Use a prefix and a number, such as `AUTH-3`.
- **`title`** is what the commit and the retro call the story.
- **`status`** says where the story stands. `ready` tells the loop it may be
  started.
- **`acceptance_criteria`** is the behaviour you are asking for. Each criterion
  has an `id`, numbered `AC-1`, `AC-2` and so on, and a `text`.

When you have saved the file, ask `sdlc` what it makes of your backlog:

```console
$ sdlc story list
> AUTH-3  ready   -  Reject expired sessions

> is what `sdlc start` would pick.
```

The `>` marks the story the loop will take next, and the `-` says it has no
priority yet. You don't need to commit the backlog before you run the story:
the loop reads it from your checkout.

## Write criteria that can be tested

The loop turns every criterion into at least one test that fails, before any
code is written. Those tests are then frozen: nobody edits them for the rest of
the story. So a criterion has to say something a test can observe from outside,
before the code exists.

Let's take a look at the difference:

| Criterion | Can it be tested? |
| --- | --- |
| The handler shall use `TokenValidator`. | No. It names a class, so a test could only check how the code is built. |
| The API shall handle bad tokens gracefully. | No. It gives a test nothing to assert. |
| IF the token cannot be parsed, THEN the API shall respond 401 with no detail about why. | Yes. A caller sees the response, and a test can check it. |

A few habits keep a criterion testable:

- **Name the exact outcome.** Give the status code, the error value or the
  stored state. A response, a returned value and a saved record are all things
  a test can see.
- **Keep to one behaviour.** Two outcomes of the same event belong together,
  as in `AC-1` above. Two separate behaviours do not: put them in one criterion
  and you get a test that can pass for one of them.
- **Write the whole criterion in `text`.** It is the only part of a criterion
  the loop reads. A criterion split across fields of your own, such as `given`,
  `when` and `then`, is empty as far as the loop can tell.
- **Keep the ids stable.** The contract `init` wrote asks for a criterion's id
  in the name of the test that covers it, so every test can be traced back to
  its criterion. Renumbering breaks the trail.

You don't need a criterion for every edge. The test author adds the boundary
cases itself: the empty input, the zero, the second call.

### The sentence patterns

The capitals in the examples are deliberate. Each criterion follows one of five
sentence patterns, known as EARS, and each pattern suits a different kind of
behaviour:

| Pattern | Use it for | Example |
| --- | --- | --- |
| `WHEN` | what happens in response to an event | WHEN a request carries a session token whose expiry is in the past, the API shall respond 401. |
| `WHILE` | what holds for as long as a state lasts | WHILE two signing keys are active, the API shall accept a token signed by either. |
| `IF ... THEN` | what happens when something goes wrong | IF the token cannot be parsed, THEN the API shall respond 401 with no detail about why. |
| `WHERE` | what applies only when an option is turned on | WHERE single sign-on is turned on, the API shall refuse a password sign-in with 403. |
| "The system shall" | what is always true | The API shall include a `Request-Id` header in every response. |

`sdlc` does not check the pattern, so nothing refuses a criterion written
another way. The patterns earn their place because each one forces you to name
a trigger and an outcome, which is exactly what a test needs.

### Say what is out of scope

When you leave something out on purpose, say so in `non_goals`. Every agent
reads the whole story, so this tells the test author and the reviewers what not
to go looking for:

```json
"non_goals": ["refresh tokens", "rate limiting"]
```

## Say more about a story

A story may also say who it is for, how urgent it is, what it depends on and
how much a mistake in it would cost. Here is the same story with those filled
in:

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
      "text": "IF the token cannot be parsed, THEN the API shall respond 401 with no detail about why."
    }
  ],
  "non_goals": ["refresh tokens", "rate limiting"]
}
```

The one to think about is `risk_tier`. A story marked `high` stops before its
commit and waits for you to read the work and approve it. A story with no tier
counts as `low`. A story of any tier also waits where its change touches a path
your project lists in `pre_commit_pause_paths`, so a tier set low does not take
that away. You may change which tiers and which paths wait with
[`human_gates`](../configuration.md#human_gates).

### Every field

| Field | Required | What it is for |
| --- | --- | --- |
| `id` | yes | the story's name, such as `AUTH-3` |
| `title` | yes | what the commit and the retro call the story |
| `status` | yes | where the story stands; see [Statuses](#statuses) |
| `acceptance_criteria` | yes | the behaviour the tests will hold the code to, each with an `id` and a `text` |
| `priority` | no | a whole number, and the lowest runs first; `0` is for urgent fixes |
| `depends_on` | no | the ids of the stories that must be `done` before this one starts |
| `as_a`, `i_want`, `so_that` | no | the user-story sentence: who it is for, what they want, and why |
| `risk_tier` | no | `low`, `medium` or `high`; a `high` story waits for you before its commit, and a story of any tier waits where its change touches a path `pre_commit_pause_paths` lists |
| `non_goals` | no | what is out of scope on purpose |
| `notes` | no | anything else worth saying |
| `decisions` | no | choices a person has made about the story, each with `at`, `by` and `text` |
| `created`, `updated` | no | timestamps; `sdlc` sets `updated` whenever it changes the status |

The story schema that `init` wrote, `.sdlc/templates/story.schema.json`,
describes all of these, and you may point your editor at it for completion.
`sdlc` itself checks less than the schema does:
[Editing the backlog](#editing-the-backlog) says what it insists on.

## Choose what runs next

When you type `/sdlc:next`, the loop picks a story for you. It considers only
the stories that are `ready` and are not waiting on another story, and it takes
the one with the lowest `priority`. A story with no priority goes last: it
counts as 999. Where two stories tie, the lower id goes first, with its numbers
read as numbers, so `AUTH-2` comes before `AUTH-10`.

To see what it would pick, and why the others are not in the running, run
`sdlc story list`:

```console
$ sdlc story list
  AUTH-1  done    1  Issue session tokens
> AUTH-3  ready   1  Reject expired sessions
  AUTH-4  ready   2  Revoke sessions on logout  (waiting on AUTH-9 (not in the backlog))
  AUTH-5  todo    3  Rotate signing keys

> is what `sdlc start` would pick.
```

Each line shows a story's id, status, priority and title. `sdlc start` is the
command `/sdlc:next` runs to begin a story.

### Hold a story back

A story you have written but are not ready to run stays `todo`. The loop passes
over a `todo` story until you mark it `ready`, so you can keep half-formed
stories in the backlog without any of them starting by surprise.

### Make one story wait for another

List the ids a story needs in its `depends_on`. The story is held back until
every one of them is `done`.

> [!WARNING]
> Check the ids in `depends_on`. A story waits for ever on an id that is not in
> the backlog, and on a story that was `dropped`. `sdlc story list` says so
> beside the story, as it does for `AUTH-4` above.

### Start a story by name

To run a particular story ahead of the others, name it:

```text
/sdlc:next AUTH-5
```

Naming a story puts it ahead of the priority order, and it is the one way to
start a story that is still `todo`. It changes nothing else. A story that is
`done`, `blocked` or `dropped`, or that waits on a story that is not done, is
[refused](../troubleshooting.md#sdlc-e0010), and the message says which.

> [!NOTE]
> A story that is already under way has a worktree of its own, a second
> checkout under `.sdlc/worktrees/`. Other checkouts pass over it, and
> `sdlc story list` says where it is held.
> [Stopping and resuming a story](stopping-and-resuming-a-story.md) shows how
> to pick one up again.

## Statuses

Every story has one of seven statuses. You set four of them, and `sdlc` sets
the other three as the story moves along.

| Status | What it means | Who sets it |
| --- | --- | --- |
| `todo` | written, and not yet ready to run; started only by name | you |
| `ready` | may be started | you |
| `in_progress` | being worked on | `sdlc` |
| `awaiting_human` | handed to you, and waiting for your answer | `sdlc` |
| `blocked` | cannot go on, and is never started | you |
| `done` | finished, and landed on trunk | `sdlc` |
| `dropped` | abandoned, and never started | you |

Spell a status exactly as it is here. `sdlc` refuses a backlog with a status it
does not know, because a story it cannot place would never run.

## When the loop turns a story away

The first gate reads each criterion the way a test author would. It turns the
story away when a criterion names an implementation instead of a behaviour, or
cannot be turned into a failing test. The loop does not guess what you meant.
It escalates: it hands the story to you and stops.

Suppose `AC-2` had read "The handler shall use `TokenValidator`". The last
thing the session prints would be something like this:

```text
AUTH-3  waiting for a person: spec_unclear
  AC-2 names a class, not something a caller can see: what should the API do with a token it cannot parse?

The iteration has ended. A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve AUTH-3, or sdlc approve AUTH-3
  sdlc approve AUTH-3 --reject "why"
```

Nothing has gone wrong. This is the cheapest place in the whole loop to find
out that a story is not clear, and putting it right takes three steps.

First, fix the criterion. Edit `user_stories.json` at the root of your
repository, the same file you wrote the story in.

Next, tell the loop you have answered. Only a person can do this, so run it in
a terminal of your own, from the story's worktree:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc approve AUTH-3
```

Then type `/sdlc:next` again. The story carries on from the first gate it has
not passed.

> [!NOTE]
> A story with no criterion `text` at all
> [cannot pass the first gate](../troubleshooting.md#sdlc-e0044), whatever
> anyone decides. Write at least one.

A story can be handed back to you a little later for the same kind of reason.
At Gate 2 the researcher may find a question your story does not settle, and at
Gate 3 the test author may meet a criterion it cannot test. You answer both the
same way: fix the story, approve, and type `/sdlc:next`.
[Human approval gates](human-approval-gates.md) covers every place the loop
stops for you.

## Editing the backlog

The backlog is yours to edit, by hand, between stories. Add a story, reword
one, or change the order whenever nothing is running.

`sdlc` writes to the file too, and only a little. As a story moves along, it
changes that story's `status` and sets its `updated` time. It leaves everything
else exactly as you wrote it: the order of the keys, the indentation, and any
field of your own.

While a story is running, leave its entry alone. The hook
[refuses an agent](../enforcement.md#backlog-is-not-edited) that tries to write
the backlog, so the criteria the tests are held to, and the tier that decides
(with the paths your project lists) who approves the commit, stay the ones the
story started with. Nothing stops you editing the entry by hand, so change a running
story only when the loop has handed it back to you with a question.

Every command reads the backlog, so a mistake in it stops them all until you
fix it. `sdlc` insists on three things: the file is valid JSON, every story has
an `id`, a `title` and a `status` it knows, and each id is made of letters,
digits, dots, dashes and underscores, starting with a letter or a digit. When
one of those is broken, the refusal names the story:

```text
sdlc: the story "AUTH-5" has the status "to-do"

  why  a status is one of todo, ready, in_progress, awaiting_human, blocked, done, dropped, and a story with any other is one the loop would never pick
  fix  fix the JSON in your backlog file — every story needs an "id", a "title" and a "status" from the schema

  SDLC-E0008  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0008
```

> [!NOTE]
> To keep the backlog somewhere else, or under another name, set
> [`backlog.path`](../configuration.md#backlogpath) in `.sdlc/config.json`.

## Where to go next

- [The loop](../the-loop.md): what each gate does with your story
- [Human approval gates](human-approval-gates.md): every place the loop stops
  and waits for you
- [Tuning commands for your stack](tuning-commands-for-your-stack.md): making
  sure the loop runs, and freezes, the right tests
- [`sdlc story list`](../commands.md#sdlc-story-list) and
  [`sdlc start`](../commands.md#sdlc-start): the two commands on this page, in
  full
