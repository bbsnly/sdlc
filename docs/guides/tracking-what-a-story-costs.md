# Tracking what a story costs

- [Introduction](#introduction)
- [See what a story has cost](#see-what-a-story-has-cost)
- [Where the number comes from](#where-the-number-comes-from)
- [Record an amount yourself](#record-an-amount-yourself)
  - [Which story it goes on](#which-story-it-goes-on)
  - [Amounts that are refused](#amounts-that-are-refused)
- [Set the budget](#set-the-budget)
- [When a story nears its budget](#when-a-story-nears-its-budget)
  - [Nothing blocks](#nothing-blocks)
- [Record the cost from a script](#record-the-cost-from-a-script)
- [See where the time and tokens went](#see-where-the-time-and-tokens-went)
- [Where to go next](#where-to-go-next)

## Introduction

A loop that runs on its own spends money on its own. You don't want to find
out a week later that one story cost far more than you expected.

`sdlc` does not measure what a session costs. Claude Code knows that, and
`sdlc` keeps the figure: it records each amount it is given against the story,
adds them up, and warns you as the total passes the points you chose. It never
stops a story over money.

This page shows where to read a story's cost, how to record an amount
yourself, how to set the budget, and what happens as a story nears it.

## See what a story has cost

To see the running total for the story being worked on, run `sdlc cost` in the
story's worktree:

```console
$ sdlc cost
AUTH-3  $1.42 spent of $60.00 (2.4%)
```

That is the amount recorded so far, the budget for one story, and how much of
the budget is used. `sdlc status` shows the same on its `cost` line, beside
the gates the story has passed.

For any other story, finished or not, name it:

```console
$ sdlc cost --story AUTH-3
```

And when you look back over what landed, each story's entry in `sdlc log` has
a `spent` row. [Reviewing what landed on trunk](reviewing-what-landed-on-trunk.md)
shows how to read the rest of the entry.

## Where the number comes from

You rarely have to record anything yourself. When a story reaches Gate 9, the
`/sdlc:next` skill records what the session cost, if the session knows it,
just before the story lands.

You only need to step in for spend that session could not see, or when you
run the loop from a script. Claude Code tells you the figure in two places:

- In an interactive session, type `/cost`.
- A headless run, `claude -p ... --output-format json`, ends with
  `total_cost_usd` in its output.

## Record an amount yourself

To add an amount to the story being worked on, run `sdlc cost add` in the
story's worktree:

```console
$ sdlc cost add --usd 1.42 --note "gate 2, researcher"
AUTH-3  $1.42 spent of $60.00 (2.4%)
```

`--usd` is the amount in US dollars. `--note` is optional, and stays on the
story's record beside the amount, so that the retro can say what the money
went on. Each call adds one entry, and the line it prints is the new total.

Made a mistake? Entries are never edited. Add another entry that puts it
right, with a note that says so, and the record keeps both.

### Which story it goes on

The amount goes on the story being worked on in the checkout you are in. With
no story under way there, `sdlc` has nothing to put it on, and
[tells you to name one](../troubleshooting.md#sdlc-e0011). Name the story with
`--story`, and the amount goes on that one:

```console
$ sdlc cost add --story AUTH-3 --usd 0.50 --note "after the session"
```

That works for a story that has landed. A story that is still under way is
held by its worktree, and an amount for it is added from there. From anywhere
else, `sdlc` [refuses and names the checkout](../troubleshooting.md#sdlc-e0050)
to run the command in.

### Amounts that are refused

An amount that is empty, negative or not a number is
[refused](../troubleshooting.md#sdlc-e0034) rather than recorded as zero:

```console
$ sdlc cost add --usd ""
sdlc: "" is not an amount

  why  --usd takes a number of US dollars, like 1.42
  fix  check the flag's value — "sdlc <command> --help" says what it takes

  SDLC-E0034  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0034
```

An empty value usually means a variable in a script came out empty. Recording
that as `$0.00` would make the story look free, which is the one wrong answer
nobody questions. A leading `$` is fine: `--usd '$1.42'` is read as 1.42.

## Set the budget

The budget lives in `.sdlc/config.json`, and `sdlc init` writes it like this:

```json
"budget": { "per_story_usd": 60, "alert_fractions": [0.5, 0.8, 1.0] }
```

`per_story_usd` is what you expect one story to cost. `alert_fractions` are
the points along the way, as shares of that budget, at which `sdlc` warns you.
Change either by hand, between stories, and commit the file to trunk so that
the next story picks it up.

If you would rather have no budget, set `per_story_usd` to `0`. The total is
still kept, and it is printed without a percentage:

```console
$ sdlc cost add --usd 1
AUTH-3  $1.00 spent; no budget is set for this project
```

> [!NOTE]
> The budget is for one story. `sdlc` keeps no total across stories. To add
> up a run of them, read the `spent` rows in `sdlc log`.

## When a story nears its budget

An alert is printed by the entry that carries the total past one of your
fractions, and only by that entry, so the same warning does not repeat. With
the defaults, this is what you see as a story spends its way through a $60
budget:

```console
$ sdlc cost add --usd 29 --note "gates 3-4"
sdlc: 50% of the $60.00 budget for this story is spent ($30.42)
AUTH-3  $30.42 spent of $60.00 (50.7%)
$ sdlc cost add --usd 20 --note "gate 5"
sdlc: 80% of the $60.00 budget for this story is spent ($50.42)
AUTH-3  $50.42 spent of $60.00 (84%)
$ sdlc cost add --usd 15 --note "rework"
sdlc: 100% of the $60.00 budget for this story is spent ($65.42)
sdlc: this story has spent its whole $60.00 budget, and nothing is blocked -- stopping a story between gates costs more than the overspend. `sdlc stop` ends the iteration if that is the call.
AUTH-3  $65.42 spent of $60.00 (109%)
```

Each alert also goes on the story's record, so the retro can see when the
story went over.

The entry that uses up the whole budget always says so, whatever
`alert_fractions` holds. With `[0.5]` alone, you hear at half the budget and
again when it is gone. A fraction above `1`, such as `1.5`, warns once the
story is that far over, and a fraction of `0` never fires.

### Nothing blocks

An alert is only a message. No gate refuses, the hook does not change, and
the story carries on. That is deliberate: a budget that stopped a story
halfway would leave the work stranded between gates, and that costs more than
the overspend.

If a story should stop, that is your call. Run `sdlc stop` in your own
terminal, and [Stopping and resuming a story](stopping-and-resuming-a-story.md)
shows what it keeps for later.

## Record the cost from a script

A headless session reports its cost only once it is over. By then the story
it worked may no longer be under way: it landed, or it was handed to you. So
note the story before the run, and name it afterwards:

```console
$ story=$(sdlc status --json | jq -r '.active // .next.story')
$ cost=$(claude -p "/sdlc:next" --output-format json | jq .total_cost_usd)
$ sdlc cost add --story "$story" --usd "$cost"
```

`active` is the story being worked on in this checkout, and `next` is the one
`sdlc start` would pick when there is none. If neither is there, the first
line yields `null`, and the last line is refused instead of putting the cost
on the wrong story.

If the session ended with the story waiting for you, the story is still held
by its worktree, and the last line is refused from your main checkout. Run it
again from `.sdlc/worktrees/<ID>`.

Alerts go to standard error and the total to standard output, so a script
that reads the total still gets one clean line. With `--json`, that line is
JSON:

```console
$ sdlc cost --json
{"ok":true,"story":"AUTH-3","spent_usd":65.42,"budget_usd":60,"fraction":1.0903333333333334}
```

## See where the time and tokens went

The cost is one number. To see what is behind it, run `sdlc trace` with the
story's id:

```console
$ sdlc trace AUTH-3
```

It shows how long each gate took, and how much of that was the agents at work
or the story waiting for you. It also shows the tokens each kind of agent
used, which is where to look when one story costs twice what the others do.
`sdlc trace --all` puts every story side by side.
[`sdlc trace`](../commands.md#sdlc-trace) describes the whole report.

> [!NOTE]
> You may also give a gate a budget in hours, with `budget.gate_hours`. Like
> the money, it warns and never blocks. [`budget`](../configuration.md#budget)
> shows how to set it.

## Where to go next

- [`sdlc cost`](../commands.md#sdlc-cost): the command and its flags
- [`budget`](../configuration.md#budget): the settings, in full
- [Stopping and resuming a story](stopping-and-resuming-a-story.md): putting a
  story down when it has cost enough
- [Consolidating what the retros say](consolidating-what-the-retros-say.md):
  what to do when stories keep going over
