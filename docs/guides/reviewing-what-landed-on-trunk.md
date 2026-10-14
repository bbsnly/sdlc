# Reviewing what landed on trunk

- [Introduction](#introduction)
- [The log, and marking it read](#the-log-and-marking-it-read)
- [With the assistant](#with-the-assistant)
- [Follow-ups go into the backlog between stories](#follow-ups-go-into-the-backlog-between-stories)
- [When the log will not start](#when-the-log-will-not-start)
- [Ranges to read with care](#ranges-to-read-with-care)
  - [What a range is](#what-a-range-is)
  - [What the check cannot catch](#what-the-check-cannot-catch)
  - [What is kept after a story stops](#what-is-kept-after-a-story-stops)
  - [When the hook watched nothing](#when-the-hook-watched-nothing)
  - [Stories worked in their own worktree](#stories-worked-in-their-own-worktree)
  - [Entries the log could not place](#entries-the-log-could-not-place)

## Introduction

The loop commits a story once every gate has passed, and nobody has to be
watching when it does. That is the point of it, but it also means that stories
can land on trunk while you are somewhere else.

This guide shows how to see what landed since you last looked, read it with or
without the assistant, and add follow-ups to the backlog. You may want to look
back after a run of stories nobody watched, before you cut a release, or when
you pick a project up again. The everyday case takes two commands, and the rest
of the page is there for when you need it.

## The log, and marking it read

`sdlc log` lists the stories committed to trunk, oldest first, with the commits
each one landed in and what it went through: review rounds, blocks, a freeze
lifted, gates reopened, questions handed to a person, and spend. It changes
nothing, so you may run it as often as you like.

```console
$ sdlc log
```

By default it starts after the last commit you acknowledged. When you have read
up to a commit, say so in your own terminal:

```console
$ sdlc ack --through HEAD
```

From then on the log starts after that commit. Acknowledging an earlier commit
is allowed, and the log lists the stories after it again.

> [!WARNING]
> `sdlc ack` is for a person alone. The hook refuses it from a tool call,
> because an assistant that could run it would be deciding which stories nobody
> needs to look at. Run it in your own terminal.

If you would rather start somewhere else, give the log a starting point, or ask
for everything:

```console
$ sdlc log --since v0.3.0
$ sdlc log --all
```

The acknowledgement lives in `.sdlc/state/acknowledged` and says how far the
person using this clone has read. Whether it is committed is your call.
[`sdlc log`](../commands.md#sdlc-log) and [`sdlc ack`](../commands.md#sdlc-ack)
have every flag.

## With the assistant

If you would like company while you read, Claude Code has a skill for it:

```text
/sdlc:trunk-review
```

It reads the same log and goes through it with you, story by story. It lists
the commits in each range and reads the changes against the story's acceptance
criteria and its retro. It looks hardest where the log shows a freeze lifted, a
gate reopened or a question answered. For each story it reports what looks
right, what is worth a second look, and what to follow up.

To start somewhere else, say so after the command:
`/sdlc:trunk-review since v0.3.0`, or `/sdlc:trunk-review everything`.

The skill does not mark anything read. When it is done it names the commit to
pass to `sdlc ack`, and leaves running it to you.

> [!NOTE]
> Only you start the skill. It carries `disable-model-invocation`, so the
> assistant does not run it on its own initiative, and the plugin is tested so
> that no agent loads it either.

## Follow-ups go into the backlog between stories

When the skill finds something worth following up, it proposes each follow-up
as a story in the backlog's own shape, with `status` set to `blocked`, so
`sdlc start` does not pick it up before you have read it. A `todo` or `ready`
story is picked next; see [Statuses](writing-stories.md#statuses). It gives the
follow-up the risk tier of the story it follows up, for you to confirm: a story
with no tier is `low`, and would be committed without waiting for you.

Nothing is added until you say which ones to add. Even then, a follow-up goes
into the backlog only when no story is being worked on. While one runs, the
session working it cannot edit the backlog
([`backlog-is-not-edited`](../enforcement.md#backlog-is-not-edited)), so the
criteria that story is held to stay the ones it started with. Other sessions are
not held to that, so the skill leaves the follow-ups as text instead, and you add
them once the story is finished.

## When the log will not start

If `.sdlc/state/acknowledged` names a commit git does not have, `sdlc log`
stops with [SDLC-E0047](../troubleshooting.md#sdlc-e0047). That happens after a
history rewrite, or when the file came from another clone. Acknowledge a commit
this repository has, or use `sdlc log --all` to read everything without it.

## Ranges to read with care

Most of the time you can read a story's range as it is listed. This section is
for the times you want to know how far to trust one. It explains how a range is
worked out, the three things its check cannot catch, and which entries deserve a
closer look. Don't worry if you do not need it yet: the log and the skill both
work without it.

### What a range is

A range is the commits the loop watched the story's session land, as far as the
hook could tell a command was a commit. The hook writes down the HEAD each such
command starts from, in the repository's git directory at
`<git-dir>/sdlc/commits/<story>/`, one small file per HEAD.
`git rev-parse --absolute-git-dir` names the git directory. The commit gate takes the commits
whose first parent is one of those, checks that exactly they and nothing else
lie between the pair it picks, and records that pair. So a person's commit made
after the story's is left out, and a story whose commits somebody else's sits
between records no range at all, rather than one that would hold theirs. It says
the session **landed** those commits, not that it wrote them: a fast-forward
merge the session ran lands commits somebody else made.

### What the check cannot catch

The check is that the pair holds exactly the commits the loop watched, never
that it holds everything the story did. Three things it cannot catch, each with
what to do about it.

#### A commit the hook did not watch

It is missing from the range, and leaves no sign. A command the hook could not
tell was a commit, one whose repository its words did not identify as this one
(after `cd "$DIR"`, with `git -C "$DIR"`, with `GIT_DIR` set), or the second
commit of one Bash call that made two, is not in the range. At the start, the
story's first commits then read as somebody else's, and at the end the range
stops short.

When the range stops before HEAD the commit gate says `head is <head>, past
<end>: nothing that landed after <end> was watched`, which is usually a person's
commit and is sometimes this. When the range ends *at* HEAD there is no such
line and no signal at all: a commit nobody watched, made straight on a HEAD
the hook had written down (after a command that went through and committed
nothing, say) is taken into the range, and nothing says so.

Read the last commit of every range as one that may not be the story's.

#### History rewritten under the loop

It can hand a person's commit to the story. A story in its own worktree that is
put onto a later trunk commit, by a rebase or by the stop's own replay, is the
exception: its range starts at the trunk commit it sits on, and the commits made
again are taken as its own. Elsewhere, a rebase that replays the story's
commit on top of one a person made on trunk leaves the person's commit in the
range and the story's own rewritten commit out of it. The same follows from
anybody rewriting trunk while a story is under way, such as a force-push, a
squash or a tidy of history, and not only from the session. The `head is` line
fires in that case, because HEAD is the rewritten commit and the range ends
before it.

Read a range whose story saw a rebase or a force-push against its commits by
hand. And read the first commits of a range that starts at the trunk commit a
story sits on: nothing watched them, so a person's commit made right on that
trunk commit, or on the rebased story before the session's next commit, reads as
the story's.

#### What the hook writes down is believed

Anyone who can write into the git directory can plant a HEAD, and the commit
gate cannot tell. What that buys is not a fake commit, which git would not
resolve, but the parent of a real, unrelated commit already on trunk: the next
commit gate takes that commit into the story's range, the check passes, and an
unreviewed commit reads as the story's.

The git directory is protected from the session's tools and shell commands by
the same rules that protect `.git/hooks`, and those rules have known gaps.

### What is kept after a story stops

What the hook wrote down is removed when a finished story is stopped, which the
loop does straight after its last gate, so it is not there to look at when you
review later. What survives is on the story's record, and `sdlc log` reads it
back: the range, `range      unknown` where the gate recorded none, and, where
the gate saw HEAD past the range's end, the same `head is <head>, past <end>`
line beneath it, with the full HEAD as `head_past_commit` in `sdlc log --json`.
An entry with no such line had a range ending at HEAD when it was recorded.

### When the hook watched nothing

Where the hook watched nothing — the plugin was not installed, enforcement was
switched off, or the story was already under way when the version that watches
arrived — the range is the older, wider one: from HEAD when code review first
passed to HEAD when the commit gate last passed, which can hold commits somebody
else made in that window. A story reopened after it was committed spans
everything that landed in between, unless it was put onto a later trunk commit.
A range is a list to read, not a list to revert, and the skill never reverts,
resets or rewrites a commit.

### Stories worked in their own worktree

A story worked in its own worktree reaches trunk only when `sdlc stop` lands it,
and the landing replaces the commit gate's range with the commits that reached
trunk, as they are after any rebase onto it. Everything in that range was
checked before trunk moved: no merge commit, nothing under `.sdlc/` and no
backlog change, the reviewed change or reviews recorded again, and the
configured commands passing where it was put onto a trunk that moved
([the loop](../the-loop.md#done-when-it-lands) lists the checks). The record's
`replayed` event names the commit the story was at before, the trunk commit it
was put onto and the commit it became. Two things
are still worth a look:

- **`CODEMAP.md`.** The story's sections of it reach trunk in a commit no
  reviewer saw, made by `sdlc stop` straight after the story's own commits and
  outside the range the log prints. It is the commit `docs: map <ID>`, and the
  story's record names it as `codemap_commit`: read it with `git show`. A
  commit of `CODEMAP.md` inside the range, from a story that made one on its
  own branch, was not reviewed either.
- **Where the reviewed change began.** Where some of the story's commits reached
  trunk before the landing, the landed event on its record names the commit its
  reviewed change began at, and the range starts later. The commits between are
  the story's too, among whatever else trunk gained meanwhile, and the log does
  not tell them apart.

### Entries the log could not place

Three kinds of entry deserve a closer look, because the log could not place
their range with confidence:

- **Inferred.** A story committed by an `sdlc` that did not record its range is
  found by the commit messages that name it, and the entry says so. A commit
  that forgot the id is missed, and a later one that names it, such as
  `Revert US-001`, moves the end of the range.
- **No single range.** The commit gate watched the story's commits and no one
  range holds only them, so it recorded none, and the log says
  `range      unknown: the loop watched this story's commits, but no single
  range holds only them`. The commit messages are not read for it: the story's
  commits are somewhere since its code review, with somebody else's among them.
- **Unknown, missing, or off this branch.** When no message names the story, or
  its commit is not in this repository or not in HEAD's history, nothing is
  guessed. These entries are listed every time, whatever you acknowledge, so
  that they are seen. The skill reviews what the record says and tells you the
  change itself was not reviewed.
