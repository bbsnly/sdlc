# Reviewing what landed on trunk

The loop commits a story once every gate has passed, and nobody has to be
watching when it does. This guide covers looking back afterwards: which stories
landed since you last looked, what each one went through, what to make of a
range of commits, and how a follow-up gets into the backlog.

Look back after a run of stories nobody watched, before you cut a release, or
when you pick a project up again.

## The log, and marking it read

`sdlc log` lists the stories committed to trunk, oldest first, with the commits
each one landed in and what it went through: review rounds, blocks, a freeze
lifted, gates reopened, questions handed to a person, and spend. It changes
nothing.

```console
$ sdlc log
$ sdlc log --since v0.3.0
$ sdlc log --all
```

It starts after the last commit you acknowledged. When you have read up to a
commit, say so in your own terminal:

```console
$ sdlc ack --through HEAD
```

From then on the log starts after that commit. The hook refuses `sdlc ack` from
a tool call, because an assistant that could run it would be deciding which
stories nobody needs to look at. Acknowledging an earlier commit is allowed, and
the log lists the stories after it again.

The acknowledgement lives in `.sdlc/state/acknowledged` and says how far the
person using this clone has read. Whether it is committed is your call.
[`sdlc log`](../commands.md#sdlc-log) and [`sdlc ack`](../commands.md#sdlc-ack)
have every flag.

## With the assistant

In Claude Code:

```text
/sdlc:trunk-review
```

It reads the same log and goes through it with you, story by story. It lists
the commits in each range and reads the changes against the story's acceptance
criteria and its retro. It looks hardest where the log shows a freeze lifted, a
gate reopened or a question answered. For each story it reports what looks
right, what is worth a second look, and what to follow up. To start somewhere
else, say so after the command: `/sdlc:trunk-review since v0.3.0`, or
`/sdlc:trunk-review everything`.

Only you start it. The skill carries `disable-model-invocation`, so the
assistant does not run it on its own initiative, and the plugin is tested so
that no agent loads it either.

It does not mark anything read. When it is done it names the commit to pass to
`sdlc ack`, and leaves running it to you.

## Ranges to read with care

A range runs from HEAD when the story's code review first passed to HEAD when
its commit gate last passed. That holds every commit the story made, and can
hold commits somebody else made in the same window. A story reopened after it
was committed spans everything that landed in between. A range is a list to
read, not a list to revert, and the skill never reverts, resets or rewrites a
commit.

Some entries need more care still:

- **Inferred.** A story committed by an `sdlc` that did not record its range is
  found by the commit messages that name it, and the entry says so. A commit
  that forgot the id is missed, and a later one that names it, such as
  `Revert US-001`, moves the end of the range.
- **Unknown, missing, or off this branch.** When no message names the story, or
  its commit is not in this repository or not in HEAD's history, nothing is
  guessed. These entries are listed every time, whatever you acknowledge, so
  that they are seen. The skill reviews what the record says and tells you the
  change itself was not reviewed.

## Follow-ups go into the backlog between stories

The skill proposes each follow-up as a story in the backlog's own shape, with
`status` set to `blocked`, so `sdlc start` does not pick it up before you have
read it. A `todo` or `ready` story is picked next; see
[Statuses](writing-stories.md#statuses). It gives the follow-up the risk tier of
the story it follows up, for you to confirm: a story with no tier is `low`, and
would be committed without waiting for you.

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
