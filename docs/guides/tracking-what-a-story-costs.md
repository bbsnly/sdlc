# Tracking what a story costs

A loop that runs on its own spends money on its own. `sdlc` keeps what a story
cost in the story's gate record, beside everything else it did, and warns you
as the total passes the points you chose. This page shows how to record the
number, where to read it back, and what a warning does.

`sdlc` does not measure anything itself. It records the amounts you give it.

## Find the number

Claude Code knows what a session cost, so that is where the amount comes from:

- a headless run ends with `total_cost_usd` in the output of
  `claude -p ... --output-format json`;
- an interactive session shows it with `/cost`.

When the session knows its cost, the `/sdlc:next` skill records it at Gate 9,
before it stops the iteration. You only need to record it yourself for spend
the session could not see, or when you run the loop from a script.

## Record an amount

```console
$ sdlc cost add --usd 1.42 --note "gate 2, researcher"
US-001  $1.42 spent of $60.00 (2.4%)
```

`--usd` is required; `--note` is optional and stays on the record beside the
amount. Each call adds one entry, and the line printed is the running total
against the budget.

The amount goes on the story being worked on, or on the one `--story` names.
With no iteration running and no story named, there is nothing to put it on:

```console
$ sdlc cost add --usd 1
sdlc: there is no story being worked on

  why  cost is recorded against the story being worked on, and no iteration is running
  fix  name the story with --story -- a runner that records the cost once a session is over has to, because the iteration has ended by then

  SDLC-E0011  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0011
$ sdlc cost add --story US-001 --usd 1
US-001  $2.42 spent of $60.00 (4%)
```

The story has to be in the backlog; an id that is not is refused rather than
starting a record nothing reads.

An amount that is empty, negative or not a number is refused with
[SDLC-E0034](../troubleshooting.md#sdlc-e0034). An empty value usually means a
shell substitution produced nothing, and recording that as `$0.00` would make
the story look free. A leading `$` is accepted. To correct a mistake, add
another entry with a note that says so; the record keeps both.

## From a script

A headless session reports its cost only once it is over. By then the iteration
it ran may have ended: Gate 9 stops it, and so does handing the story to a
person. So note the story before the run, and name it afterwards:

```console
$ story=$(sdlc status --json | jq -r '.active // .next.story')
$ cost=$(claude -p "/sdlc:next" --output-format json | jq .total_cost_usd)
$ sdlc cost add --story "$story" --usd "$cost"
```

`active` is the story being worked on, and `next` the one `sdlc start` would
pick up when nothing is. When neither is there, because the backlog has nothing
to start or the next story is waiting for a person, the first line yields
`null` and the last line is refused instead of putting the cost on the wrong
story.

## Set the budget

The budget lives in `.sdlc/config.json`:

```json
"budget": { "per_story_usd": 60, "alert_fractions": [0.5, 0.8, 1.0] }
```

Those are the defaults, and a key you leave out keeps its default.
`per_story_usd` is what one story is expected to cost. `alert_fractions` are the
points, as fractions of that budget, where `sdlc` says so. Set
`per_story_usd` to `0` to turn the budget off. The total is still kept:

```console
$ sdlc cost add --usd 1
US-001  $3.42 spent; no budget is set for this project
```

## Read the running total

`sdlc cost` prints the total without changing it. It takes `--story` too:

```console
$ sdlc cost
US-001  $0.00 spent of $60.00 (0%)
$ sdlc cost --story US-002
US-002  $41.10 spent of $60.00 (68.5%)
```

`sdlc status` shows it for the story being worked on, once something has been
spent or a budget is set:

```console
$ sdlc status
US-001  in progress  Example: reject invoices with a non-positive total

  no gates recorded yet

  tests  not frozen
  cost   $65.42 of $60.00 (109%)

  next   dor

  backlog  1 in progress

A person types /sdlc:next in Claude Code to carry on, or runs `sdlc stop` to put it down.
```

A script reads the `cost` object from `sdlc status --json`, which has
`spent_usd`, `budget_usd` and `fraction`; the last two are left out when no
budget is set. Each entry itself is kept under `spend` in
`.sdlc/stories/<ID>/gate-record.json`, with its time (`at`), amount (`usd`) and
`note`.

## What an alert does

An alert fires on the entry that carries the total past a fraction, and only on
that entry. It goes to standard error, so in a terminal it appears above the
total:

```console
$ sdlc cost add --usd 29 --note "gates 3-4"
sdlc: 50% of the $60.00 budget for this story is spent ($30.42)
US-001  $30.42 spent of $60.00 (50.7%)
$ sdlc cost add --usd 20 --note "gate 5"
sdlc: 80% of the $60.00 budget for this story is spent ($50.42)
US-001  $50.42 spent of $60.00 (84%)
$ sdlc cost add --usd 15 --note "rework"
sdlc: 100% of the $60.00 budget for this story is spent ($65.42)
sdlc: this story has spent its whole $60.00 budget, and nothing is blocked -- stopping a story between gates costs more than the overspend. `sdlc stop` ends the iteration if that is the call.
US-001  $65.42 spent of $60.00 (109%)
```

Three things are worth knowing:

- **The total goes to standard output and the alert to standard error**, so a
  script that reads the number still gets a clean line. That holds with
  `--json` too: the alert still goes to standard error, so a runner reading
  standard output cannot miss it, and it is also in the `alerts` array.
- **Each alert is also written to the gate record** as an event of type
  `budget`, so the retro can see when the story went over.
- **The entry that uses up the budget always says so**, whatever
  `alert_fractions` holds, and names `sdlc stop`. It says it once, on the entry
  that reaches `per_story_usd`. With `[0.5]` alone you hear at half the budget
  and again when it is used up. A fraction of `0` never fires; one above `1`,
  such as `1.5`, fires once the story is that far over.

## What an alert does not do

Nothing blocks. No gate refuses, the hook does not change, and the story carries
on. A budget that stopped a story halfway would leave the work stranded between
gates, and that costs more than the overspend. If a story should stop, that is
your call, and `sdlc stop` makes it.

The budget is per story. There is no total across stories.

## Where to go next

- [`sdlc cost`](../commands.md#sdlc-cost): the command and its flags
- [`budget`](../configuration.md#budget): the settings
- [Stopping and resuming a story](stopping-and-resuming-a-story.md): putting a
  story down when it has cost enough
- [Gate 9](../the-loop.md#gate-9--retro): where the skill records the cost
