---
name: next
description: Work the next story through the loop's gates, one gate at a time. Use when the user asks to start a story, continue the loop, pick up the next piece of work, or says /sdlc:next.
---

# Work the next story

You are running one iteration of a story-driven delivery loop. One story, one gate at a time,
each gate recorded before the next begins.

Everything you know about the loop's state comes from the `sdlc` command. Do not infer it from
the conversation, and do not write to `.sdlc/state/` yourself — a hook will refuse it, because a
record the assistant can edit is not a record.

## Before anything else

Run:

```bash
sdlc status --json
```

- If it fails with `SDLC-E0002`, this project has not been set up. Tell the user, and offer to
  run `sdlc init`. Stop until they answer.
- If it fails with `SDLC-E0001`, you are not in a Git repository. Say so and stop.
- Otherwise read `active`, `gates` and `next` from the output. They tell you where to resume.

## Gate 1 — Select the story

Run:

```bash
sdlc start --json
```

This picks up a story already under way, or takes the next runnable one. If it fails with
`SDLC-E0010` there is nothing to work on: show the user the `why` field from the error, run
`sdlc story list` so they can see the backlog, and stop.

Read the story from the backlog file named by `backlog.path` in `.sdlc/config.json`. Check its
acceptance criteria are testable: each one names an observable behaviour, not an implementation.
If a criterion cannot be turned into a failing test, that is a Definition-of-Ready problem —
record it and ask the user rather than inventing an interpretation:

```bash
sdlc gate dor fail --note "AC-2 describes an implementation, not a behaviour"
```

Otherwise:

```bash
sdlc gate dor pass --note "<what made the story ready, in one clause>"
```

## Gate 2 — Analysis and threats

Delegate to the `sdlc:researcher` agent. Give it a brief containing:

- the story id and its full text, including every acceptance criterion
- the paths it should read: `CODEMAP.md`, `CLAUDE.md`'s `## SDLC Contract` section, and the
  `spec.paths` from `.sdlc/config.json`
- that it stores its two documents with `sdlc artifact write analysis` and
  `sdlc artifact write threats`

Do not do this analysis yourself. The agent starts from a fresh context on purpose: it has not
seen the conversation that led here, so it cannot inherit an assumption from it.

This is enforced, not requested. Nobody edits a gate's documents in place — not you, not the
agent whose gate it is. They go through `sdlc artifact write`, the same as every other piece of
loop state, and the hook refuses anything else.

When it returns, read its JSON result and record the gate:

```bash
sdlc gate analysis pass --note "<security_sensitive and open_questions, in one clause>"
```

If either document was not stored the gate will refuse to pass, and say which one is missing.
That is not something to work around: the agent's summary is not the document, and the gates
after this one read the document. Show the user what the researcher reported and stop.

If the agent reports `open_questions` above zero, or `split_recommended`, do not pass the gate.
Record it as a failure with the reason, show the user the open questions, and stop. Guessing an
answer here is the single most expensive mistake in the loop: everything downstream is built on it.

```bash
sdlc gate analysis fail --note "<n> open questions the specification does not settle"
```

## What is not built yet

Gates 3 to 9 — acceptance tests, the plan, implementation, verification, review, commit and
retro — are not in this version. When gate 2 passes, say so plainly and stop:

> Analysis is done and recorded. The rest of the loop is not built yet; `sdlc status` shows
> where this story stands.

Do not carry on into the later gates by hand. The point of the loop is that each gate is done by
someone who cannot see the others' reasoning, and doing it in this conversation is exactly what
it exists to prevent.

## If a command fails

Every failure carries a code, a reason and a fix. Show the user the `error`, `why` and `fix`
fields as they are. Do not paraphrase them, and do not retry a command that failed for a reason
that has not changed.
