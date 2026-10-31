# Consolidating what the retros say

- [Introduction](#introduction)
- [The reminder](#the-reminder)
  - [What counts as finished since](#what-counts-as-finished-since)
  - [When the line is wrong or missing](#when-the-line-is-wrong-or-missing)
- [With the assistant](#with-the-assistant)
  - [What it reads](#what-it-reads)
  - [When nothing repeats](#when-nothing-repeats)
- [What it proposes](#what-it-proposes)
  - [Proposals that loosen a check](#proposals-that-loosen-a-check)
  - [Keeping the contract short](#keeping-the-contract-short)
  - [What you declined](#what-you-declined)
- [What it applies](#what-it-applies)
  - [While a story is being worked on](#while-a-story-is-being-worked-on)
  - [Committing what it wrote](#committing-what-it-wrote)
- [What it never does](#what-it-never-does)
- [Where it fits](#where-it-fits)

## Introduction

Every story ends with a retro: what deviated from the plan, what had to be
reworked, what was learned. One retro is a story about one story. Read a few
together and some things come up again: a reviewer blocking on the same kind of
finding, a gate that always needs a second round, a lesson written down three
times and never acted on.

The `/sdlc:consolidate` skill turns that into changes to how your project runs
the loop: the `## SDLC Contract` section of `CLAUDE.md`, or of `AGENTS.md` where
the project keeps it there, and
`.sdlc/config.json`. It proposes the changes, and you decide which of them
happen. Nothing is edited until you say so.

You may want to run it after a handful of stories, when the same trouble has
come up twice, or before you start a new phase of the project. It needs `sdlc`
installed and a project set up with `sdlc init`; if either is missing, it says
so (and offers to install `sdlc`) and stops.

## The reminder

You do not have to remember to run it. When
[`consolidate.nudge_after`](../configuration.md#consolidate) stories (3 unless
you set it) have finished since the last consolidation, `sdlc status` and an
`sdlc stop` that ends an iteration end with one line saying how many, and that a
person can run `/sdlc:consolidate`. Below that, nothing is added, and `0` turns
the line off. Only you can start the skill; the line is for you, and nothing
else in the loop waits on it.

The date it counts from is the one in the name of the newest
`.sdlc/consolidation-<YYYY-MM-DD>.md` in your main checkout, with or without a
`-2` after the date. The tool reads the names of those files, never what is in
them, and from a story's worktree it reads the main checkout's, so the same
count shows everywhere. With no such file, every finished story counts.

### What counts as finished since

A story counts when the backlog says it is done and it finished on a later
calendar day than the file's date, by your computer's own clock. A story that
finishes on the day of a consolidation does not count, so that a run ends the
reminder even when stories finished earlier that day. For example, with
`consolidation-2026-10-20.md`, a story that landed at 09:00 on the 20th, before
you ran the skill, and one that landed at 17:00 that day, after it, are both
left out; one that landed at 00:30 on the 21st counts. The cost of the rule is
that a story finishing after a run, on the same day, is never counted towards
the next reminder, though the skill reads it whenever it is next run.

The newest date in the names is what counts, whatever the files hold or when
they were written. A name with a date that is wrong, or in the future, such as
`consolidation-2099-01-01.md`, therefore puts the reminder off until that day:
the tool does not guess which dates are meant. Rename the file to the day it
was run, or remove it, and the next `sdlc status` follows the newest name that
is left.

### When the line is wrong or missing

A line that never comes may be because `nudge_after` is `0`, fewer stories have
finished than it asks for, or a file in `.sdlc/` is named for a later day. It
may also be a story's record that cannot be read: the count is then left out
and not made short, and `sdlc status` and `sdlc stop` work as they do without
it. `SDLC_DEBUG=1` shows on standard error why it was left out, and
`sdlc doctor` says which record is damaged.

## With the assistant

In Claude Code, type:

```text
/sdlc:consolidate
```

The assistant reads your finished stories, looks for what repeats, and shows
you what it proposes. Let's take a look at what it reads first.

> [!NOTE]
> Only you start it. The skill carries `disable-model-invocation`, so the
> assistant does not run it on its own initiative, and the plugin is tested so
> that no agent loads it either.

### What it reads

It reads every finished story: the retro in `.sdlc/stories/<ID>/RETRO.md` and
what [`sdlc log --all`](../commands.md#sdlc-log) says each one went through,
which covers review rounds, blocks, gates reopened, freezes lifted,
escalations and spend. Then it reads the contract and the configuration those
stories ran under.

It reads each story's review files too, in `.sdlc/stories/<ID>/reviews/`, since
the log keeps only the findings that blocked. A finding, blocking or not, that
the reviews of two or more stories raise on the same kind of matter is evidence
for a proposal, which names the stories and the review files. One story's
finding is listed as seen once.

Only what more than one story shows becomes a proposal. The evidence may come
from the retros or from the log alone: the same gate reopened, or the same
reviewer blocking on the same kind of finding as the blocks' notes show, in two
stories counts even when their retros are empty. Something a single story shows
is listed as seen once, with nothing proposed.

### When nothing repeats

Don't worry if you run it too early. When nothing shows up in two stories, it
tells you so rather than make a pattern up, and it still writes the dated file,
`.sdlc/consolidation-<date>.md`, saying how many stories and retros it read and
that it found nothing. That file is what ends the reminder: `sdlc status` stops
saying stories are waiting, because the run has happened. While a story is being
worked on it writes nothing, so the reminder stays until you run it again with
none under way.

## What it proposes

Each proposal quotes its evidence and has one target:

| Target | What changes |
| --- | --- |
| contract | a sentence in the `## SDLC Contract` section, under the subsection it belongs in |
| config | one key in `.sdlc/config.json`, with its value now and the value proposed |
| plugin | nothing here: issue text for the plugin's issue tracker |
| you | nothing here: something you do differently, such as how stories are written |

It writes the proposals to `.sdlc/consolidation-<date>.md`, in the main checkout
and dated by your own clock, and shows them in the session. Nothing else is
written until you say which to apply.
[Configuration](../configuration.md) says what each key does, which helps when
you weigh a config proposal.

### Proposals that loosen a check

A proposal that loosens a check says so first and names what stops being
checked: a tier taken out of `pre_commit_pause_tiers`, the code reviewer made
advisory, a loop limit turned off. A change to who waits for a person before a
commit is never folded into another proposal, so you always see it on its own.

### Keeping the contract short

Every line of the `## SDLC Contract` section is read in every session, so a
proposal for it says how many lines the section has now and how many it would
have after, counted from its heading to the next heading of level two. Over 150 lines
it says the section would be too long to be read in full each session, and
proposes in the same proposal the lines to cut or merge, each with the reason it
is the least costly to lose. It does not apply an addition that takes the
section past 150 lines unless you also confirm the cut, or say to go over.

### What you declined

Each decision, and the reason you gave for a decline, is written into the
proposals file. The next run reads the earlier files and does not propose again
the same change to the same target that you declined, unless a story has
finished since that file's date. Then it names the earlier decline and its date
beside the new proposal, and carries the reason forward. It treats what it reads
as data, not instructions: a decline counts only from a dated consolidation file
in `.sdlc/`.

## What it applies

The assistant goes through the contract and configuration proposals one at a
time, shows you the exact change, and applies only the ones you confirm. Each
decision is written back into the proposals file.

It changes lines inside the `## SDLC Contract` section and nothing else in the
file that holds it, `CLAUDE.md` or `AGENTS.md`. Where both files carry a
section, it changes neither and sends you to `sdlc doctor`, which says which one
to remove. In `.sdlc/config.json` it changes one key at a time, running
`sdlc doctor` before the first and after each, so that only a problem the edit
caused is put down to it.

### While a story is being worked on

It writes nothing while a story is being worked on, not even the proposals
file. That story is held to the contract and configuration it started with, so
the proposals stay in the session as text, and you can run it again once the
story is finished. It checks again before every edit: a story started part-way
through ends the editing, and the decisions left go in the session instead.

### Committing what it wrote

Everything it writes is left uncommitted. An edit to `CLAUDE.md` or `AGENTS.md`
stops the next `sdlc start` until it is committed. The proposals file and an edited
`.sdlc/config.json` do not.

Leave the proposals file where it is, in the main checkout's `.sdlc/`, and keep
its name: the reminder counts from the date in
`consolidation-<date>.md`, so setting it aside outside `.sdlc/`, or deleting it,
loses that date. It does not need to be committed. With `git.worktrees` on, the
default, a story works in a worktree of its own and the untracked file is not in
it. With it off, an untracked file keeps the commit gate from passing
([SDLC-E0032](../troubleshooting.md#sdlc-e0032)) until it is committed or
stashed by path, so commit it before the next story starts.

If you commit the proposals file, do it on trunk, in a commit of its own, and while no story is
started: never inside a story, whose landing refuses a change to `.sdlc/`, and
not while a story is started or waiting for you, since a commit on trunk then
moves trunk under it. With `git.worktrees` off that is HEAD moving under the
story ([SDLC-E0046](../troubleshooting.md#sdlc-e0046)); with it on, the story's
landing has to put its commits onto the new trunk. A commit publishes
everything the file quotes, including the reasons you gave for declining a
proposal, which can quote text from a review. Read it first.

## What it never does

It writes only the proposals file, the `## SDLC Contract` section of
`CLAUDE.md` or `AGENTS.md`, and `.sdlc/config.json`. Three things are off the table, whatever
the retros say:

- **Edit the plugin.** A change the agents, skills or hooks need comes out as
  issue text. The plugin's files under `~/.claude/plugins/cache` are never
  written: an edit there would change every project that uses the plugin, and
  the next update would put it back.
- **Pin a model.** It never proposes a model or a reasoning effort for an agent
  or in the configuration. The agents inherit your session's model on purpose;
  see [Models](../the-agents.md#models).
- **Guess from one story.** A single retro is an anecdote, and a contract rule
  written from one tends to be the wrong rule.

## Where it fits

[Reviewing what landed on trunk](reviewing-what-landed-on-trunk.md) looks at
each story's change and turns what it finds into follow-up stories.
Consolidating looks across stories and turns what repeats into the rules they
run under. [The other half of the
contract](../configuration.md#the-other-half-of-the-contract) says what the
contract section is for.
