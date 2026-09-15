---
name: consolidate
description: Turn what the retros and the log keep saying across finished stories into proposed changes to this project's SDLC Contract and .sdlc/config.json, and apply the ones you confirm. Run it yourself as /sdlc:consolidate; the model does not start it.
disable-model-invocation: true
---

# Consolidate what the retros say

You are helping a person find what keeps happening across the stories the loop has finished, and
turn it into changes to how this project runs the loop. You read, count and propose. What
changes is theirs to decide.

In the project you may write three files, and nothing else:

- `.sdlc/consolidation-<YYYY-MM-DD>.md`, the proposals;
- the `## SDLC Contract` section of the project's `CLAUDE.md`, and only that section;
- `.sdlc/config.json`.

Everything else is read only. Never write under `~/.claude`, and never write under the plugin
cache, `~/.claude/plugins/cache`: an edit there changes every project that uses the plugin, and
is lost the next time it updates. What the plugin's agents, skills or hooks should do differently
goes to the person as issue text for the plugin's issue tracker, never as an edit.

Never propose pinning a model or a reasoning effort, in an agent or in the configuration. The
agents inherit the session's model on purpose: which model to pay for is the person's choice.

## Before anything else

Run:

```bash
sdlc status --json
```

- If the command is not found at all, the tool this reads is not installed. Offer to install it,
  and wait for an answer. On macOS or Linux that is
  `curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh`; on Windows,
  `irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex`. If they say yes,
  run it and then run `sdlc status --json` again. If it is still not found, say which directory
  the installer reported and that a new session will see it, and stop. Do not consolidate from
  the retros alone: the log is what says which stories finished and what each one went through.
- If it fails with `SDLC-E0002`, this project has not been set up, so there is nothing to
  consolidate. Say so and stop.
- If it fails with `SDLC-E0001`, you are not in a Git repository. Say so and stop.
- Otherwise note whether `active` names a story. While one is being worked on it is held to the
  contract and configuration it started with. The session working it is refused edits to both;
  this one may not be, so nothing stops you but this: write nothing at all, not even the
  proposals file, and give the proposals as text.

## Gather the evidence

```bash
sdlc log --all --json
```

For every entry, read `.sdlc/stories/<ID>/RETRO.md` when it is there — its deviations, rework,
lessons and follow-ups — and from the entry `rounds`, `blocks`, `reopened`, `unfrozen`,
`escalations` and `spent_usd`. When the entry has `not_in_backlog`, the story has gone from the
backlog; its retro and record still count.

Then read what the proposals would change: the `## SDLC Contract` section of `CLAUDE.md`, from
that heading to the next heading of the same level or the end of the file, and
`.sdlc/config.json`.

A retro that is only its headings, or a placeholder such as `# Retro`, says nothing. The log
still counts without one: the same gate reopened, escalations of the same type, or blocks from the
same reviewer on the same kind of finding, as their `note`s show, in two or more stories is
evidence. A block with no `note` shows no kind of finding, and reviewers block routinely, so two
blocks alone are not a pattern. If neither the retros nor the log show anything in two stories,
tell the person what you found and stop: two stories are the least a pattern can stand on, and a
proposal from one is a guess.

## Find what repeats

Look for what more than one story shows:

- the same lesson, deviation or follow-up in different retros;
- the same gate needing more than one round, or the same reviewer blocking on the same kind of
  finding;
- gates reopened, freezes lifted or escalations of the same type;
- stories costing well over `budget.per_story_usd`, or tests the freeze kept missing.

Something one story shows goes under **Seen once**, with no proposal.

## Propose

Each proposal names its evidence — story ids, a short quote from each retro, counts from the
log — and one target:

- **contract**: text to add to, change in or remove from the `## SDLC Contract` section, under
  the `###` subsection it belongs in, written as the sentence that would appear there. Every line
  of the section is loaded into every session, and the template asks to keep it under about 150
  lines, so prefer changing a line to adding one;
- **config**: one key in `.sdlc/config.json`, its value now, the value proposed and why. Propose
  only a key the file already has or the configuration reference documents
  (`https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md`). A key this `sdlc` does not
  know — invented, or documented for a release newer than the one installed — changes nothing the
  loop does, and `sdlc doctor` reports it;
- **plugin**: issue text for the plugin's issue tracker — a title, what happened in which
  stories, and what the agent, skill or hook should do instead;
- **you**: something a person does differently, such as how stories are written. Nothing is
  edited for it.

A proposal that loosens a check says so first, as a trade-off, and names what stops being
checked. Among them, and not only these: a tier taken out of
`human_gates.pre_commit_pause_tiers`, `dor_advocate_check` turned off, a higher
`thresholds.diff_size_cap` or a lower `coverage_min` or `mutation_min`, `reviews.gate7_advisory`
or `freeze.allow_new_test_files` turned on, `paths.tests` narrowed so the freeze covers less, a
`commands` entry cleared so the gates that run it skip it, `git.remote` turned off, a `loop` limit
raised or set to `0`, or a contract rule dropped, including one that makes a kind of story
`risk_tier: high`. A person pausing before a commit is the check most easily lost, so never fold
a change to it into another proposal.

## Write the proposals

Unless a story is active, write them to `.sdlc/consolidation-<YYYY-MM-DD>.md`, with today's date.
If that file is already there, add `-2`, `-3` and so on rather than overwrite it.

```markdown
# Consolidation — <YYYY-MM-DD>

Read <n> stories and <m> retros, committed <first commit>..<last commit>.

## P1 — <what changes>

- Target: contract (### <subsection>) | config (`<key>`) | plugin | you
- Evidence: <ids, quotes, counts>
- Change: <the exact sentence, the value, or the issue text>
- Trade-off: <what stops being checked, or "none">
- Decision: proposed

## Seen once

- <id>: <what it showed>
```

Show the person the proposals in the session as well.

## Apply the ones confirmed

Take the contract and config proposals one at a time. For each, show the exact change and ask.
Apply it only on a yes, and only that one. Before each edit run `sdlc status --json` again. If
`active` now names a story, stop: make no more edits and write nothing more to the proposals
file, and give the decisions still to record, and the proposals still to apply, in the session.

- **contract**: change only lines inside the `## SDLC Contract` section, and nothing else in
  `CLAUDE.md`. If the file has no such section, do not write one anywhere: say so, and leave the
  proposal as text for the person to add.
- **config**: before the first config edit, run `sdlc doctor` once and keep what it reports: it
  can report problems the edit has nothing to do with, and ends with a failing exit when it
  finds any. In each config edit, change only that key, keeping the file's formatting and its `_`
  note keys, then run `sdlc doctor` again. Show its report as it is, and treat only a problem
  that was not there before as caused by the edit; if there is one, say so and offer to put the
  value back.

While no story is active, record each decision in the proposals file: `applied`, `declined`, or
`left as text`. Plugin proposals stay issue text, for the person to file.

## Finish

Say what changed — which files, and which proposals — and what did not. The proposals file and
every edit are uncommitted. An edit to `CLAUDE.md` stops the next `sdlc start` until it is
committed. The proposals file and an edited `.sdlc/config.json` do not, and the next story's
commit would take them in with its own work. Tell the person to commit them, or set them aside,
before the next story starts.

## If a command fails

Every failure carries a code, a reason and a fix. Show the user the `error`, `why` and `fix`
fields as they are. Do not paraphrase them, and do not retry a command that failed for a reason
that has not changed. `sdlc doctor` is the exception: it fails when it finds a problem, and its
report is what says why, so show that report as it is.
