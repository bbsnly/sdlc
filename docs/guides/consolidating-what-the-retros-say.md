# Consolidating what the retros say

- [Introduction](#introduction)
- [Run a consolidation](#run-a-consolidation)
  - [What it reads](#what-it-reads)
  - [What it proposes](#what-it-proposes)
  - [When nothing repeats](#when-nothing-repeats)
- [Decide on each proposal](#decide-on-each-proposal)
  - [Proposals that loosen a check](#proposals-that-loosen-a-check)
  - [Keeping the contract short](#keeping-the-contract-short)
  - [What you declined](#what-you-declined)
- [Commit what changed](#commit-what-changed)
  - [The proposals file](#the-proposals-file)
- [The reminder](#the-reminder)
  - [What counts as finished since](#what-counts-as-finished-since)
  - [When the line is wrong or missing](#when-the-line-is-wrong-or-missing)
- [While a story is being worked on](#while-a-story-is-being-worked-on)
- [What it never does](#what-it-never-does)
- [Where to go next](#where-to-go-next)

## Introduction

Every story ends with a retro: what deviated from the plan, what had to be
reworked, what was learned. One retro is an account of one story. Read a few
together and some things come up again: a reviewer blocking on the same kind
of finding, a gate that always needs a second round, a lesson written down
three times and never acted on.

Consolidating turns what repeats into changes to how your project runs the
loop. The `/sdlc:consolidate` skill reads your finished stories and proposes
changes to two things: the `## SDLC Contract` section of your `CLAUDE.md`,
where the reviewers find your project's rules, and `.sdlc/config.json`. It
proposes, and you decide. Nothing is edited until you say so.

You may want to run it after a handful of stories, when the same trouble has
come up twice, or before you start a new phase of the project. `sdlc` also
[reminds you](#the-reminder) when a few stories have finished.

## Run a consolidation

Wait until no story is being worked on, then type this in Claude Code, in your
project:

```text
/sdlc:consolidate
```

The assistant reads your finished stories, looks for what repeats, and shows
you what it proposes. Only you can start it: the assistant does not run this
skill on its own initiative, and none of the loop's agents can load it.

### What it reads

It reads every finished story from three sides:

- **The retro**, `.sdlc/stories/<ID>/RETRO.md`.
- **What the story went through**, as
  [`sdlc log --all`](../commands.md#sdlc-log) tells it: review rounds, blocks,
  gates reopened, freezes lifted, questions handed to you, and spend.
- **The reviews**, every file in `.sdlc/stories/<ID>/reviews/`. The log keeps
  only the findings a reviewer counted, and the review files hold the rest.

Then it reads the contract and the configuration those stories ran under.

Only what more than one story shows becomes a proposal. The same lesson in two
retros counts. So does the same gate reopened in two stories, even when their
retros are empty. A finding that the review files of two or more stories raise
on the same kind of matter counts too, whether it blocked or not, and the
proposal names the stories and the files. Something a single story shows is
listed as seen once, with nothing proposed.

### What it proposes

Each proposal quotes its evidence and has one target:

| Target | What would change |
| --- | --- |
| contract | a sentence in the `## SDLC Contract` section, under the subsection it belongs in |
| config | one key in `.sdlc/config.json`, with its value now and the value proposed |
| plugin | nothing in your project: you get the text of an issue to file against the plugin |
| you | nothing in your project: something you do differently, such as how you write stories |

The assistant shows the proposals in the session and writes them to a file in
your main checkout, `.sdlc/consolidation-<date>.md`, dated by your own clock.
Nothing else is written until you say which ones to apply.

### When nothing repeats

Don't worry if you run it too early. When nothing shows up in two stories, the
skill says so rather than make a pattern up. It still writes the file
`.sdlc/consolidation-<date>.md`, saying how many stories it read and that it
found nothing, and that file is what ends the reminder in `sdlc status`: the
run has happened.

## Decide on each proposal

The assistant takes the contract and configuration proposals one at a time. It
shows you the exact change and applies it only when you say yes. Each decision
is written back into the proposals file: applied, declined, or left as text.

For the contract, it changes lines inside the `## SDLC Contract` section and
nothing else in the file. For the configuration, it changes one key at a time
and runs `sdlc doctor` before the first edit and after each one, so that a
problem an edit caused is caught at once and can be put back.

When you weigh a configuration proposal,
[Configuration](../configuration.md) says what each key does.

### Proposals that loosen a check

A proposal that loosens a check says so first, and names what would stop being
checked: a risk tier or a path that no longer waits for your approval, a
reviewer whose block no longer stops the gate, a limit turned off, a lower
coverage threshold.

A change to who waits for a person before a commit is never folded into
another proposal. You always see it on its own.

### Keeping the contract short

Every line of the `## SDLC Contract` section is read in every session, so a
long contract costs you on every story. A proposal for the contract says how
many lines the section has now and how many it would have after.

Over 150 lines, the proposal says the section would be too long to be read in
full each session, and proposes in the same breath the lines to cut or merge,
each with the reason it is the least costly to lose. The assistant does not
apply an addition that takes the section past 150 lines unless you also confirm
the cut, or tell it to go over.

### What you declined

When you decline a proposal, the reason you give is written into the
proposals file. The next run reads the earlier files, so a change you declined
is not proposed again in the same form unless a story has finished since. Then
the new proposal names the earlier decline and its date, and carries your
reason forward.

Only a decision recorded in one of those files counts. A retro or a review
that merely says something was declined does not: the assistant treats what it
reads there as evidence, not as instructions.

## Commit what changed

Everything the skill writes is left uncommitted, so that you can read it
first. Commit the contract and configuration edits to trunk before you start
the next story:

```console
$ git add CLAUDE.md .sdlc/config.json
$ git commit -m "Tighten the contract after consolidating"
```

> [!WARNING]
> Don't skip this commit. A story is worked in a worktree cut from what trunk
> commits, so an edit you have not committed does not reach the next story,
> and the rule you agreed to is not applied. With
> [`git.worktrees`](../configuration.md#git) off, an uncommitted edit to the
> contract [stops the next start](../troubleshooting.md#sdlc-e0039) instead.

### The proposals file

Leave the proposals file where it is, in the `.sdlc/` directory of your main
checkout, and keep its name. The reminder counts from the date in
`consolidation-<date>.md`, so moving or deleting the file loses that date.

It does not need to be committed. If you do commit the proposals file, do it
on trunk, in a commit of its own, and while no story is started. A story's
landing refuses a change under `.sdlc/`, and a commit on trunk while a story
is under way moves trunk underneath it.

Read the file before you commit it. A commit publishes everything it quotes,
including the reasons you gave for declining, which can quote text from a
review.

> [!NOTE]
> With `git.worktrees` off, an untracked file in your checkout keeps the next
> story's commit gate [from passing](../troubleshooting.md#sdlc-e0032). There,
> commit the proposals file before the next story starts.

## The reminder

You do not have to remember to consolidate. When enough stories have finished
since the last time, `sdlc status` ends with one more line, and so does an
`sdlc stop` that ends an iteration:

```text
3 stories have finished and none has been consolidated: a person can run /sdlc:consolidate to see what they have in common.
```

"Enough" is [`consolidate.nudge_after`](../configuration.md#consolidate), which
is 3 unless you set it. Below that number of stories, `sdlc status` and
`sdlc stop` add nothing about `/sdlc:consolidate`, and `0` turns the line off
for good:

```json
"consolidate": { "nudge_after": 5 }
```

The line is for you. Nothing else in the loop waits on it, and no agent can
act on it.

### What counts as finished since

The count starts from your last consolidation, and `sdlc` finds that by the
date in the name of the newest `.sdlc/consolidation-<date>.md` in your main
checkout. It reads only the names of those files, never what is in them. With
no such file, every finished story counts.

A story counts when the backlog says it is done and it finished on a later
day than that date, by your computer's own clock. A story that finishes on
the day of a consolidation does not count, whether it landed before the run or
after it. That is what lets a run end the reminder straight away. The cost is
that a story landing later the same day never counts towards the next
reminder, though the skill reads it whenever it is next run.

### When the line is wrong or missing

If the line never comes, check these first:

- `nudge_after` is `0`, or fewer stories have finished than it asks for.
- A file in `.sdlc/` is named for a day that has not come yet, such as
  `consolidation-2099-01-01.md`. `sdlc` does not guess which dates are meant,
  so that name puts the reminder off until then. Rename the file to the day it
  was run, or remove it.
- A story's record cannot be read. The count is then left out rather than
  made short, and `sdlc doctor` says which record is damaged.

## While a story is being worked on

The skill writes nothing while a story is being worked on, not even the
proposals file. That story is held to the contract and configuration it
started with, so the proposals stay in the session as text. Because no file
was written, the reminder stays too, until you run the skill again with no
story under way.

It checks again before every edit. If a story is started part-way through,
the editing ends there, and the decisions still to make are given to you in
the session.

## What it never does

The skill writes three things: the proposals file, the `## SDLC Contract`
section, and `.sdlc/config.json`. Three things are off the table, whatever the
retros say:

- **Edit the plugin.** A change the agents, skills or hooks need comes out as
  issue text for you to file. An edit to your installed copy of the plugin
  would change every project that uses it, and the next update would undo it.
- **Pin a model.** It never proposes a model or a reasoning effort, for an
  agent or in the configuration. Which model to pay for is your choice:
  [Models](../the-agents.md#models) explains.
- **Guess from one story.** A single retro is an anecdote, and a contract rule
  written from one tends to be the wrong rule.

> [!NOTE]
> Does your project keep its contract in `AGENTS.md`? Then that is the file the
> skill edits, and wherever this page says `CLAUDE.md`, read `AGENTS.md`. Where
> both files carry a contract section, it changes neither and sends you to
> `sdlc doctor`, which says which one to remove.

## Where to go next

- [Reviewing what landed on trunk](reviewing-what-landed-on-trunk.md): looks
  at one story's change at a time, where this page looks across stories
- [The other half of the contract](../configuration.md#the-other-half-of-the-contract):
  what the contract section is for
- [Fill in the contract](adding-to-an-existing-project.md#fill-in-the-contract):
  which agent reads each part of it
- [Configuration](../configuration.md): every setting a proposal can change
