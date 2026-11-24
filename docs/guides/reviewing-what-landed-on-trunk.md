# Reviewing what landed on trunk

- [Introduction](#introduction)
- [See what landed](#see-what-landed)
  - [Read an entry](#read-an-entry)
  - [Start from somewhere else](#start-from-somewhere-else)
- [Read a story's change](#read-a-storys-change)
- [Review it with the assistant](#review-it-with-the-assistant)
- [Mark it read](#mark-it-read)
- [Add follow-ups to the backlog](#add-follow-ups-to-the-backlog)
- [When the log will not start](#when-the-log-will-not-start)
- [Ranges to read with care](#ranges-to-read-with-care)
  - [What the log says about a range](#what-the-log-says-about-a-range)
  - [The map commit](#the-map-commit)
  - [Stories worked in your own checkout](#stories-worked-in-your-own-checkout)
- [Where to go next](#where-to-go-next)

## Introduction

The loop lands a story once every gate has passed, and nobody has to be
watching when it does. That is the point of it. It also means stories reach
trunk while you are somewhere else.

`sdlc log` is how you catch up. It lists the stories that landed since you
last looked, with the commits each one landed in and what it went through on
the way: how many rounds of review, who blocked, what was found, what it cost.

This guide shows how to read the log, look at a story's change with or without
the assistant, mark what you have read, and turn what you find into follow-up
stories. You may want it after a run of stories nobody watched, before you cut
a release, or when you pick a project up again.

## See what landed

From your project, run `sdlc log`:

```console
$ sdlc log
```

It lists every story that was committed since the last commit you marked as
read, oldest first. The first time, that is every story. It changes nothing,
so you may run it as often as you like.

### Read an entry

Each story's entry starts with a line like this one:

```text
b9a9f4e96c93..75ef254ded0b  AUTH-3  Reject expired sessions
```

That is the range of commits the story landed in, its id and its title.
Beneath it comes a row for each thing worth knowing about how the story got
there. Here are the rows from the same entry:

| Row | What it tells you |
| --- | --- |
| `rounds     design_review 2 · verifier_review 1 · code_review 1` | How many rounds of review each gate took. More than one means something was sent back. |
| `blocks     1: design_review architect (round 1)` | Which reviewer blocked, at which gate, in which round. |
| `counts     design_review architect (round 1): 1 important` | How many findings a review counted. |
| `finding    design_review architect F-1 fixed, important: the plan: AC-2 has no step` | Each finding, and whether it was fixed, declined or referred to you. A declined or referred finding gives its reason. |
| `staffing   2026-11-23T19:51:47Z  staffing_set: 13 agents needed, 0 left out; staffing changes on the record before this: 0` | Which agents the story used. A line for each agent sits above this one, with what the agent was asked for, or that it was left out, and why. |
| `spent      $12.40` | What was [recorded as spent](tracking-what-a-story-costs.md) on the story. |

Three more rows appear only when something unusual happened, and they are
where a second look pays most:

- **`unfrozen`**: the freeze on the story's tests was lifted, with the reason
  that was given. Check that the tests changed the way the reason says.
- **`reopened`**: a gate that had passed was opened again.
- **`escalated`**: the story was handed to a person, with the question, and
  whether it was `answered` or is still `waiting`.

An entry may also have a `range` row. That is the log telling you how far to
trust the range on its first line, and
[Ranges to read with care](#ranges-to-read-with-care) says what each one
means.

### Start from somewhere else

To look further back than the last commit you marked as read, name the commit
to start after, or ask for everything:

```console
$ sdlc log --since v0.3.0
$ sdlc log --all
```

`--since` takes a tag, a hash, or a name such as `HEAD~20`.
[`sdlc log`](../commands.md#sdlc-log) has the rest, including `--json` for a
script.

## Read a story's change

The range on an entry's first line is one git understands, so you can hand it
straight to git. Let's take a look at what AUTH-3 changed:

```console
$ git log --stat b9a9f4e96c93..75ef254ded0b
$ git diff b9a9f4e96c93..75ef254ded0b
```

Read the change against the story's acceptance criteria in your backlog. Does
each criterion have something in the change behind it? Is there a change no
criterion asked for?

The story's own documents are beside your code, in `.sdlc/stories/AUTH-3/`,
all as text you can read. The two worth opening first are `RETRO.md`, which
says what deviated from the plan and what was learned, and the files under
`reviews/`, which hold everything each reviewer said. The log only shows the
findings they counted.

## Review it with the assistant

If you would like company while you read, there is a skill for it. Type this
in Claude Code:

```text
/sdlc:trunk-review
```

The assistant reads the same log and goes through it with you, story by story.
It lists the commits in each range and reads the change against the story's
acceptance criteria and its retro. It looks hardest where the log shows a
freeze lifted, a gate reopened or a question answered. For each story it
reports what looks right, what is worth a second look, and what to follow up.

To start from somewhere else, say so after the command:

```text
/sdlc:trunk-review since v0.3.0
/sdlc:trunk-review everything
```

The skill only reads. It never reverts, resets or rewrites a commit, and it
marks nothing as read. When it is done, it names the commit to acknowledge and
leaves that to you.

> [!NOTE]
> Only you can start this skill. The assistant does not run it on its own
> initiative, and none of the loop's agents can load it.

## Mark it read

When you have read up to a commit, tell `sdlc`. Run this in your own terminal:

```console
$ sdlc ack --through HEAD
Acknowledged through 75ef254ded0b. `sdlc log` starts after it.
```

In Claude Code, you may type `/sdlc:ack --through HEAD` instead.

From then on the log starts after that commit. `--through` takes HEAD or any
commit in its history, and there is no default: you always say how far you
read. If you acknowledge a commit earlier than the last one, the log lists the
stories in between again.

A story the log [could not place](#what-the-log-says-about-a-range) stays in
the log whatever you acknowledge, so that it is seen.

> [!WARNING]
> Acknowledging is yours alone. The hook refuses `sdlc ack` from an agent,
> because an agent that could run it would be deciding which stories nobody
> needs to look at.

The acknowledgement says how far the person using this clone has read. It is
kept in this clone and is not something to commit.

## Add follow-ups to the backlog

A review usually ends with a short list: a criterion that deserves another
test, a finding that was declined and should not have been. Each of those is a
new story.

The `/sdlc:trunk-review` skill drafts them for you. It proposes each follow-up
as a story in your backlog's own shape, with two choices made on the safe
side for you to confirm:

- **`status` is `blocked`**, so the loop does not start the story before you
  have read it.
- **`risk_tier` is the tier of the story it follows up.** A story with no tier
  is `low`, and a low-risk story is committed without waiting for you, unless
  its change touches a path `pre_commit_pause_paths` lists.

Nothing is added until you say which ones to add. To write a follow-up
yourself, add it to the backlog like any other story:
[Writing stories](writing-stories.md) shows how.

> [!NOTE]
> Add follow-ups between stories. While a story is being worked on, the skill
> leaves the backlog alone and gives you the follow-ups as text, so that the
> criteria that story is held to stay the ones it started with.

## When the log will not start

If the commit you last acknowledged is one git no longer has, `sdlc log`
[stops and says so](../troubleshooting.md#sdlc-e0047) rather than guess where
you got to. That happens after history was rewritten, or when the
acknowledgement was copied from another clone.

Acknowledge a commit this repository does have, and the log starts from there.
To read everything in the meantime, use `sdlc log --all`.

## Ranges to read with care

Most of the time you can read a range as it is listed. This section is for
the times you want to know how far to trust one. Don't worry if you do not
need it yet: the log and the skill both work without it.

A range is the commits the loop saw the story land. For a story worked in a
worktree of its own, which is how `sdlc` sets a project up, it is exactly the
commits that reached trunk when the story landed. Everything in it was checked
before trunk moved: it is the change the reviewers saw, or it was reviewed
again. [Done when it lands](../the-loop.md#done-when-it-lands) lists the
checks.

A range is still a record and not proof. It is a list to read, never a list
to revert.

### What the log says about a range

When the log is not sure of a range, it says so on a `range` row beneath the
entry's first line:

| The row says | What it means | What to do |
| --- | --- | --- |
| `head is <head>, past <end>: nothing that landed after <end> was watched` | When the story's commit was recorded, HEAD had moved past the end of the range, and the loop watched nothing after it. Usually a person committed after the session. | Look at what `<head>` changed, and whether it belongs to the story. |
| `inferred from commit messages` | The story was committed by an older `sdlc` that recorded no range, so the log found it by the commit messages that name its id. | Check both ends. A commit that forgot the id is missing, and a later one that names it, such as a revert, moves the end. |
| `unknown: the loop watched this story's commits, but no single range holds only them` | Somebody else's commits sit between the story's. | Find the story's commits yourself, among those since its code review. |
| `unknown: the record names no commit, and no commit message names AUTH-3` | Nothing says where the story landed. | Review what the story's documents say, and treat the change itself as not reviewed. |
| `not on this branch`, or `not in this repository` | The story's commit is not in the history of the branch you are on, or git does not have it at all. | The same. Nothing is guessed for it. |

Entries with an `unknown` range, or one that is not on this branch or not in
this repository, are listed every time, whatever you have acknowledged.

### The map commit

One commit can reach trunk with a story and sit outside its range. Where the
story wrote sections for `CODEMAP.md`, the project's map of its code,
`sdlc stop` puts them on trunk in a commit of its own, straight after the
story's commits. Its message begins `docs: map` and the story's id:

```console
$ git log --oneline --grep "docs: map AUTH-3"
```

No reviewer saw that commit, because the map is written after the last
review. Give it a glance with `git show` when you review the story.

### Stories worked in your own checkout

With [`git.worktrees`](../configuration.md#git) set to `false`, a story's
commits are made straight onto the branch you have checked out, among
whatever else is committed in the meantime. A range is then only what the
hook watched the story's session commit, and three things can make it wrong:

- **A commit the hook did not watch.** It is missing from the range, with no
  sign. A commit nobody watched can also be taken into the end of a range that
  stops at HEAD. Read the last commit of every range as one that may not be
  the story's.
- **History rewritten while the story was under way.** After a rebase, a
  squash or a force-push, a person's commit can end up inside the range and
  the story's own rewritten commit outside it. Read such a story's commits by
  hand.
- **A session the hook was not watching.** Where the plugin was not installed
  or enforcement was off, the range is everything from the story's code review
  to its commit, which can hold commits somebody else made in that window.

A story that was reopened after it was committed spans everything that landed
in between.

## Where to go next

- [`sdlc log`](../commands.md#sdlc-log) and
  [`sdlc ack`](../commands.md#sdlc-ack): every flag, and the `--json` fields
- [Consolidating what the retros say](consolidating-what-the-retros-say.md):
  looking across stories for what repeats, where this page looks at one story
  at a time
- [Tracking what a story costs](tracking-what-a-story-costs.md): where the
  `spent` row comes from
- [Writing stories](writing-stories.md): the shape of a follow-up
