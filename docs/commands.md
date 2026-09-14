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
| `--force` | restore the default settings over an existing setup, keeping your stories, where they are (`backlog.path`) and your trunk branch (`git.trunk_branch`) |

Reads the repository, detects the stack, and writes `.sdlc/config.json` with
commands that already match it, a backlog with one example story, the story
schema, and a `## SDLC Contract` section appended to `CLAUDE.md`. It never
overwrites a file you wrote, and it writes no `.gitignore`.

It recognises Go (`go.mod`), Rust (`Cargo.toml`), Node (`package.json`) and
Python (`pyproject.toml`, `setup.py` or `requirements.txt`), tried in that order
at the root of the repository, so a Go service with a `package.json` for its
front-end tooling is a Go project. Anything else gets no commands and the
common test directories, for you to fill in.

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

An active story that has gone from the backlog reads as `(not in the backlog)`,
with `"not_in_backlog": true` in `--json`. The commit gate refuses such a story,
so put it back, or run `sdlc stop` to end the iteration.

Three fields in `--json` are worth knowing by name, because a skill reads them to
decide what to do:

| Field | What it says |
| --- | --- |
| `next_gate` | the gate to work now — the first one that has not passed. Absent when every gate is behind you, or when no story is active |
| `next` | the story `sdlc start` would pick up. Only present when nothing is active, and absent while a story waits for a person |
| `waiting` | every story handed to a person with `sdlc escalate` and not yet answered, with the question it asks |

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

Naming a story chooses it over the priority order, and nothing else: a story
that is `dropped`, `blocked` or marked `done`, or that waits on a `depends_on`
story that is not done, is refused as it would be passed over —
[SDLC-E0010](troubleshooting.md#sdlc-e0010).

It also refuses a story whose gates have all passed, rather than putting
finished work back in progress. The way back into a finished story is to record
the gate that failed — see [SDLC-E0033](troubleshooting.md#sdlc-e0033).

Before it begins a new story — not one it is picking up again — it checks that
trunk is somewhere to start from: HEAD is on `git.trunk_branch`, nothing is
uncommitted outside `.sdlc/` and the backlog, trunk is not behind `origin` when
`git.remote` is on, and `commands.smoke` passes. Each has its own refusal, from
[SDLC-E0038](troubleshooting.md#sdlc-e0038) to
[SDLC-E0041](troubleshooting.md#sdlc-e0041).

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

A gate recorded as failed `loop.max_rework_rounds` times since a person last
answered for the story hands the story to that person, as
[`sdlc escalate`](#sdlc-escalate) `gate_failing` would, and says so —
`handed_over` in `--json`.

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
has changed since. A `blocking` reviewer has to approve; an `advisory` one only
has to report; perf is `on budget`: it never has to approve, and its block
stops the gate, because it blocks only on a performance budget the contract
states.

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
story's entry in the backlog at `dor`, the plan's content at `design_review`,
the working tree at `verifier_review` and `code_review`. That is what makes an
approval go stale when the thing it approved changes. The story and the tree
both leave out the `status` and `updated` fields `sdlc` writes into the backlog
itself, and the tree leaves out `.sdlc/`; see
[Why approvals go stale](the-loop.md#why-approvals-go-stale).

A block that stops the gate, recorded `loop.max_review_rounds` times by the same
reviewer since a person last answered for the story, hands the story to that
person as `review_not_converging`, and says so — `handed_over` in `--json`.

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

The one exception is `freeze.allow_new_test_files`. With it on, running
`sdlc freeze` again on the story's own freeze adds the test files written since,
provided none of the frozen ones has changed, and lists what it added; `--json`
names them in `added`.

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

## `sdlc escalate`

Hand the story to a person, and end the iteration.

```console
$ sdlc escalate pre_commit_approval --message "the migration has no down step"
```

| Flag | What it does |
| --- | --- |
| `--message` | what the person is being asked to decide. Required |

The type is a word for the kind of question — `pre_commit_approval`,
`spec_unclear`, `loop_stalled` — and the message is the question. It goes on the
story's record, bound to the work as it stands. The story becomes
`awaiting_human`, and the iteration ends, so the session stops rather than
carrying on past the question. `sdlc start` refuses the story until somebody
answers, and `sdlc status` lists it as waiting.

Nothing else starts in the meantime either: `sdlc start` picks up a story
already under way before a new one, so it picks the waiting story and refuses
it. That is deliberate. The work waiting for an answer is still in the tree, and
a second story started on top of it would end up in the same commit.

## `sdlc approve`

A person's answer to an escalation.

```console
$ sdlc approve US-001
$ sdlc approve US-001 --reject "the migration has no way back"
```

| Flag | What it does |
| --- | --- |
| `--reject` | send the work back instead, saying why |

Run it in your own terminal: the hook refuses it from a tool call, because an
agent that could answer would be approving its own work. Without a story id it
answers for the story being worked on. The answer goes on the record with the
tree it was given for, the story goes back to `in_progress`, and `sdlc start`
picks it up again.

## `sdlc cost`

What the story has cost so far, against what it was expected to.

```console
$ sdlc cost
US-001  $12.40 spent of $60.00 (20.7%)

$ sdlc cost add --usd 1.42 --note "gate 4, five reviewers"
US-001  $13.82 spent of $60.00 (23%)
```

### `sdlc cost add`

Records one amount against the story being worked on, or the one `--story` names.

| Flag | What it does |
| --- | --- |
| `--usd` | the amount, in US dollars. Required |
| `--note` | what it was spent on |
| `--story` | the story it was spent on, when no iteration is running on it. `sdlc cost` takes it too |

`budget.per_story_usd` in `.sdlc/config.json` says what one story is expected to cost, and
`alert_fractions` says where along the way to say so. An alert goes to standard error, once,
on the entry that crosses it, with `--json` as well as without — so a runner reading standard
output for the number still sees it, and the same warning does not repeat on every entry
afterwards.

**Nothing here blocks.** A budget that stops a story halfway leaves the work stranded between
gates, which costs more than the overspend it prevents. The entry that uses up the budget says
so, whatever `alert_fractions` holds, and names `sdlc stop` if that is the call.

Where the number comes from is the runner's business. A headless session reports its cost only
once it is over, and by then the iteration it ran may have ended, so the runner notes the story
first and names it:

```console
$ story=$(sdlc status --json | jq -r '.active // .next.story')
$ cost=$(claude -p "Run /sdlc:next" --output-format json | jq .total_cost_usd)
$ sdlc cost add --story "$story" --usd "$cost"
```

An interactive session has `/cost`. An empty or unparseable amount is refused rather than
recorded as zero, because a story that silently cost nothing is the one wrong answer nobody
questions.

## `sdlc doctor`

Check this project and say how to fix what is wrong.

```console
$ sdlc doctor
$ sdlc doctor --json
```

Looks at the git repository, the git command itself, the configuration, the
backlog, the loop's state files the hook reads on every tool call, the contract
in `CLAUDE.md`, the programs your configured commands would run, and whether
`sdlc` is on your `PATH`. Every problem comes with the
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
