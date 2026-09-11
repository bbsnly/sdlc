# Commands

Every command, every flag. `sdlc <command> --help` says the same thing in your
terminal.

Two rules run through all of them:

- **`--json` works everywhere.** One line of JSON on standard output, for a
  script or a skill to read. Failures come back as JSON too, with the same
  fields as the prose.
- **Failures carry four things**: what happened, why, what to do about it, and a
  code you can look up in [troubleshooting](troubleshooting.md).

## `sdlc init`

Set this repository up to run the loop.

```console
$ sdlc init
$ sdlc init --force
```

| Flag | What it does |
| --- | --- |
| `--force` | restore the default settings over an existing setup, keeping your stories |

Reads the repository, detects the stack, and writes `.sdlc/config.json` with
commands that already match it, a backlog with one example story, the story
schema, and a `## SDLC Contract` section appended to `CLAUDE.md`. It never
overwrites a file you wrote, and it writes no `.gitignore`.

## `sdlc status`

Show what the loop is working on and where it has got to.

```console
$ sdlc status
$ sdlc status --json
```

Reports the active story, every gate's outcome, the state of the test freeze and
whether it is still intact, the backlog counts, and what `sdlc start` would pick
up next. It changes nothing, so it is safe to run at any point.

An active story with no gate left reads as `finished` rather than `in progress`,
because there is nothing left to work on it: what it wants is `sdlc stop`.

Two fields in `--json` are worth knowing by name, because a skill reads them to
decide what to do:

| Field | What it says |
| --- | --- |
| `next_gate` | the gate to work now — the first one that has not passed. Absent when every gate is behind you, or when no story is active |
| `next` | the story `sdlc start` would pick up. Only present when nothing is active |

## `sdlc story list`

List the backlog, and say what is holding each story back.

```console
$ sdlc story list
$ sdlc story list --json
```

Marks the story `sdlc start` would choose, and for every other one says why it
is waiting: blocked by another story, not ready, or already done.

## `sdlc start`

Begin an iteration on the next story, or on the one you name.

```console
$ sdlc start
$ sdlc start AUTH-3
```

Picks up a story already under way, or takes the next runnable one: resume
first, then priority, then id. Refuses if an iteration is already running.

It also refuses a story whose gates have all passed, rather than putting
finished work back in progress. The way back into a finished story is to record
the gate that failed — see [SDLC-E0033](troubleshooting.md#sdlc-e0033).

## `sdlc stop`

End the current iteration without recording a result.

```console
$ sdlc stop
```

Stopping does not undo anything. The gates already recorded stay recorded, the
story stays in progress, and starting again resumes it.

A story whose gates have all passed is a different case, and `stop` says so: it
is done, the test freeze it was holding is lifted, and the next `sdlc start`
moves on to the next story instead of reopening it. An unfinished story keeps
its freeze, so stopping and starting again cannot be a way round it.

## `sdlc gate GATE STATUS`

Record the outcome of one gate.

```console
$ sdlc gate dor pass --note "criteria are testable"
$ sdlc gate analysis pass --security-sensitive --note "token crosses a boundary"
$ sdlc gate code_review fail --note "AC-2 is untested"
```

| Flag | What it does |
| --- | --- |
| `--note` | why the gate came out this way, in one line |
| `--story` | record against this story instead of the one being worked on. The id has to be in the backlog; a mistyped one is refused rather than starting a record nothing reads |
| `--security-sensitive` | the story touches a trust boundary, so the security review blocks. Set it at the analysis gate; unset is assumed to mean yes |

Gates: `dor`, `analysis`, `tests_frozen`, `plan`, `design_review`,
`implementation`, `verification`, `verifier_review`, `code_review`, `commit`,
`retro`. Outcomes: `pass`, `fail`, `pending`.

`gate` records a decision; it does not make one. But it does refuse to record a
**pass** that is not true: see [the loop](the-loop.md) for what each gate
requires. A `fail` is always recordable — a gate can fail precisely because its
work could not be done, and the loop has to have somewhere to put that.

Recording the last gate is also what finishes the story: when no gate is left
unpassed, the story's status becomes `done` and it leaves the backlog. That is
read from the record rather than from the gate's name, so a gate recorded as
failed afterwards — a code review reopened on work already committed — puts the
story back to `in_progress`, where the rework belongs.

## `sdlc artifact list`

List the documents this version can store.

```console
$ sdlc artifact list
```

## `sdlc artifact write NAME`

Store one of a gate's documents for the story being worked on.

```console
$ sdlc artifact write analysis < analysis.md
$ sdlc artifact write threats --file /tmp/threats.md
$ sdlc artifact write plan <<'SDLC_DOCUMENT'
# Plan
...
SDLC_DOCUMENT
```

| Flag | What it does |
| --- | --- |
| `--file` | read the document from this path instead of standard input |
| `--story` | store against this story instead of the one being worked on |

Names: `analysis`, `threats`, `test_plan`, `plan`, `verification`, `retro`.

Writing a document again replaces it: a gate that ran twice has one record, not
two. An empty document is refused, and so is one larger than a megabyte —
storing a truncated document would leave every later gate reviewing something
that stops mid-sentence.

This is the only way these files are written. The hook refuses anyone who edits
them in place, including the agent whose gate it is. See
[enforcement](enforcement.md).

## `sdlc review list`

Show which reviews each gate expects and where they have got to.

```console
$ sdlc review list
$ sdlc review list --gate design_review --json
```

| Flag | What it does |
| --- | --- |
| `--gate` | only this gate's reviews |
| `--story` | for this story instead of the one being worked on |

Says, for every expected review, whether it can block the gate, what the latest
verdict was, and whether it is **stale** — the reviewer looked at something that
has changed since.

## `sdlc review add GATE ROLE VERDICT`

Record one reviewer's conclusion.

```console
$ sdlc review add design_review architect block --note "AC-3 has no step" < review.md
$ sdlc review add code_review perf note --file /tmp/perf.md
```

| Flag | What it does |
| --- | --- |
| `--file` | read the review from this path instead of standard input |
| `--note` | the headline finding, in one line |
| `--story` | record against this story instead of the one being worked on |

Verdicts: `approve`, `block`, `note`.

The command stamps the review with what was in front of the reviewer — the
plan's content at `design_review`, the working tree at `verifier_review` and
`code_review`. That is what makes an approval go stale when the thing it
approved changes.

Rounds are kept rather than overwritten, so "what did the architect say last
time" stays answerable.

## `sdlc freeze`

Lock the acceptance tests by content.

```console
$ sdlc freeze
$ sdlc freeze --json
```

Records the sha256 of every file that `paths.tests` in your configuration calls
a test, asking git which files are in the working tree so that build output and
vendored code cannot be mistaken for one.

Refuses if a freeze already exists. Taking a second one over the top would
quietly bless whatever changed in between.

## `sdlc unfreeze`

Lift the test freeze, on the record.

```console
$ sdlc unfreeze --reason "AC-2's test asserted the old error message"
```

| Flag | What it does |
| --- | --- |
| `--reason` | why the frozen tests have to change. Required |

The reason is required because that is the whole point. Lifting the freeze is
sometimes right — a test encoded the wrong behaviour — and it is also exactly
the move an agent would make to reach green. Recording why is what tells the two
apart.

## `sdlc doctor`

Check this project and say how to fix what is wrong.

```console
$ sdlc doctor
$ sdlc doctor --json
```

Looks at the git repository, the git command itself, the configuration, the
backlog, the contract in `CLAUDE.md`, the programs your configured commands
would run, and whether `sdlc` is on your `PATH`. Every problem comes with the
command that fixes it. Exits non-zero when something is wrong, so it works in a
script.

It does not run your test suite. This is meant to be the fast answer to "why is
this not working".

## `sdlc version`

Print the version of sdlc you are running.

```console
$ sdlc version
$ sdlc version --short
```

| Flag | What it does |
| --- | --- |
| `--short` | print the version alone, with nothing around it |

## `sdlc hook EVENT`

Not for you to run. It is what the plugin's hook script calls, reading the event
on standard input. It is documented here only so that a line in a log makes
sense.
