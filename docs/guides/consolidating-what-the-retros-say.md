# Consolidating what the retros say

Every story ends with a retro: what deviated from the plan, what had to be
reworked, what was learned. One retro is a story about one story. Read a few
together and some things come up again: a reviewer blocking on the same kind of
finding, a gate that always needs a second round, a lesson written down three
times and never acted on.

This guide covers turning that into changes to how your project runs the loop:
the `## SDLC Contract` section of `CLAUDE.md`, and `.sdlc/config.json`.

Do it after a handful of stories, when the same trouble has come up twice, or
before you start a new phase of the project.

## With the assistant

In Claude Code:

```text
/sdlc:consolidate
```

It reads every finished story: the retro in `.sdlc/stories/<ID>/RETRO.md` and
what [`sdlc log --all`](../commands.md#sdlc-log) says each one went through —
review rounds, blocks, gates reopened, freezes lifted, escalations and spend.
Then it reads the contract and the configuration those stories ran under.

Only what more than one story shows becomes a proposal, from the retros or from
the log alone: the same gate reopened, or the same reviewer blocking on the same
kind of finding as the blocks' notes show, in two stories counts even when their
retros are empty. Something a single story shows
is listed as seen once, with nothing proposed. When nothing shows up in two
stories, it tells you so and stops rather than make a pattern up.

Only you start it. The skill carries `disable-model-invocation`, so the
assistant does not run it on its own initiative, and the plugin is tested so
that no agent loads it either.

## What it proposes

Each proposal quotes its evidence and has one target:

| Target | What changes |
| --- | --- |
| contract | a sentence in the `## SDLC Contract` section, under the subsection it belongs in |
| config | one key in `.sdlc/config.json`, with its value now and the value proposed |
| plugin | nothing here: issue text for the plugin's issue tracker |
| you | nothing here: something you do differently, such as how stories are written |

It writes them to `.sdlc/consolidation-<date>.md` and shows them in the session.
Nothing else is written until you say which to apply.

A proposal that loosens a check says so first and names what stops being
checked: a tier taken out of `pre_commit_pause_tiers`, a higher
`diff_size_cap`, the code reviewer made advisory, a loop limit turned off. A
change to who waits for a person before a commit is never folded into another
proposal. [Configuration](../configuration.md) says what each key does.

## What it applies

It goes through the contract and configuration proposals one at a time, shows
you the exact change, and applies only the ones you confirm. It changes lines
inside the `## SDLC Contract` section and nothing else in `CLAUDE.md`, and one
key at a time in `.sdlc/config.json`, running `sdlc doctor` before the first and
after each, so that only a problem the edit caused is put down to it. Each
decision is written back into the proposals file.

It writes nothing while a story is being worked on, not even the proposals
file. That story is held to the contract and configuration it started with, so
the proposals stay in the session as text, and you can run it again once the
story is finished. It checks again before every edit: a story started part-way
through ends the editing, and the decisions left go in the session instead.

Everything it writes is left uncommitted. An edit to `CLAUDE.md` stops the next
`sdlc start` until it is committed. The proposals file and an edited
`.sdlc/config.json` do not, and the next story's commit takes them in with its
own work, so commit them, or set them aside, before the next story starts.

## What it never does

- **Edit the plugin.** A change the agents, skills or hooks need comes out as
  issue text. The plugin's files under `~/.claude/plugins/cache` are never
  written: an edit there would change every project that uses the plugin, and
  the next update would put it back.
- **Pin a model.** It never proposes a model or a reasoning effort for an agent
  or in the configuration. The agents inherit your session's model on purpose;
  see [Models](../agents.md#models).
- **Guess from one story.** A single retro is an anecdote, and a contract rule
  written from one tends to be the wrong rule.

## Where it fits

[Reviewing what landed on trunk](reviewing-what-landed-on-trunk.md) looks at
each story's change and turns what it finds into follow-up stories.
Consolidating looks across stories and turns what repeats into the rules they
run under. [The other half of the
contract](../configuration.md#the-other-half-of-the-contract) says what the
contract section is for.
