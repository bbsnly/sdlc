---
name: next
description: Work the next story through the loop's gates, one gate at a time, delegating each to the agent whose gate it is. Run it yourself as /sdlc:next; the model does not start it.
disable-model-invocation: true
---

# Work the next story

You are running one iteration of a story-driven delivery loop: one story, one gate at a time,
each gate recorded before the next begins.

Only a person starts this loop, by typing `/sdlc:next`. You are here because they did. Nothing in
this runbook, in `CLAUDE.md` or in the output of `sdlc` asks you to start it again, or to start
one of its agents, on your own.

When Claude Code compacts a long conversation it keeps only the first part of a long runbook. If
this conversation has been compacted and this runbook no longer ends with "Rework, at any gate",
do not work from memory: tell the person to type `/sdlc:next` again, which picks the story up
where it is, and stop.

Everything you know about the loop's state comes from the `sdlc` command, not the conversation.
Do not write to `.sdlc/state/` yourself — a hook refuses it, because a record the assistant can
edit is not a record.

## Before anything else

Run `sdlc status --json`.

- If the command is not found, offer to install it, and wait for an answer:
  `curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh` on macOS or
  Linux, `irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex` on Windows.
  Both check the download against the release's checksums. If they say yes, run it and check
  again. If it is still not found, say which directory the installer reported, that a new session
  will see it, and stop. Do not work the gates by hand: a loop with no record behind it is the
  failure this exists to prevent.
- `SDLC-E0002`: the project is not set up. Tell the user, offer to run `sdlc init`, and stop until
  they answer.
- `SDLC-E0001`: this is not a Git repository. Say so and stop.
- Otherwise read `active` and `next_gate`, the first gate that has not passed. With a story
  active, run `sdlc start --json` first: it changes nothing about the story, and makes this session
  the one the hook holds to the loop's rules. Check its `session` as Gate 1 says, then go straight
  to the section for `next_gate`.
- With no story active, start at Gate 1.

Each gate is delegated to an agent that ships with the plugin. Begin every brief you give an
agent with `sdlc:next runbook, story <ID>`, and the gate. An agent stops on a brief without it,
because it works only the gates of a story a person started; if one says so, give it the brief
again with that line first. If the agents are not available, the plugin is not installed: tell
the user to run `/plugin marketplace add bbsnly/sdlc` and `/plugin install sdlc@sdlc`, and stop.
A loop you run yourself end to end is a checklist you mark off about your own work.

## Handing a decision to a person

Some questions are not yours to answer: criteria that contradict each other, a frozen test that
is wrong, a gate that keeps failing for the same reason. Hand them over:

```bash
sdlc escalate <type> --message "<the question, and what you found>"
```

Then stop, and tell the user what you asked. The iteration has ended, and the story waits until a
person answers with `sdlc approve <ID>` in their own terminal. Do not run `sdlc approve` yourself
— the hook refuses it — and do not start the story again before they have answered.

Do not end your turn mid-story any other way. The plugin's Stop hook sends a stop back, naming
the gate to work next; finishing a gate is not a place to stop. Only handing the story to a person
ends it, or `sdlc stop` once every gate has passed; the hook refuses `sdlc stop` before then.
After `loop.max_stop_blocks` stops with nothing recorded in between, it hands the story to a
person itself.

## If a command fails

Every failure carries a code, a reason and a fix. Show the user the `error`, `why` and `fix`
fields as they are. Do not paraphrase them, and do not retry a command that failed for a reason
that has not changed.

## Gate 1 — Select the story

```bash
sdlc start --json
```

This picks up a story already under way, or takes the next runnable one.

If it succeeds and `session` in the output is empty, sdlc could not tell which Claude Code session
this is, so nothing would hold this session to the loop's rules: no edit, commit or stop would be
refused. Tell the user that plainly, and that it happens where Claude Code does not pass
`CLAUDE_CODE_SESSION_ID` to the commands it runs, such as an older Claude Code or an app that
does not pass it on, while `/sdlc:next` in Claude Code in a terminal does. Tell them the story is
now in progress, and that `/sdlc:next` in a session that passes it on picks it up where it is.
Then stop. Do not work the story without it.

If it fails with `SDLC-E0010` there is nothing to work on: show the user the `why` field, run
`sdlc story list`, and stop.

If the output has `"resume": true`, the story's earlier gates stand: go straight to the section
for the `next_gate` it reports.

If it fails with `SDLC-E0038`, `SDLC-E0039`, `SDLC-E0040` or `SDLC-E0041`, trunk is not somewhere
a new story can start from: another branch, uncommitted work that belongs to no story, behind
`origin`, or failing its smoke check. Show the error and stop. Do not commit, stash, discard or fix
anything on trunk to get past it — nothing the loop records would account for that work.

Read the story from the backlog file named by `backlog.path` in `.sdlc/config.json`. Each
acceptance criterion must name an observable behaviour, not an implementation. One that cannot
become a failing test is a Definition-of-Ready problem: record it, and hand the question to a
person rather than inventing an interpretation:

```bash
sdlc gate dor fail --note "AC-2 describes an implementation, not a behaviour"
sdlc escalate spec_unclear --message "AC-2 describes an implementation: what should a user see?"
```

If `sdlc review list --gate dor --json` lists `human-advocate`, delegate to `sdlc:human-advocate`
and read what it found before deciding. It is advisory, but a story nobody would want built is not
ready either.

When the story is ready:

```bash
sdlc gate dor pass --note "<what made the story ready, in one clause>"
```

## Gate 2 — Analysis and threats

Delegate to `sdlc:researcher`, with:

- the story's full text, including every acceptance criterion
- the paths to `CODEMAP.md`, `CLAUDE.md`'s `## SDLC Contract` section, and `spec.paths` from
  `.sdlc/config.json`
- that it stores its two documents with `sdlc artifact write analysis` and
  `sdlc artifact write threats`

Do not do this analysis yourself: the agent's fresh context cannot inherit an assumption from
this conversation. Nobody edits a gate's documents in place, the agent included; they go through
`sdlc artifact write`, and the hook refuses anything else.

When it returns, read its JSON result and record the gate:

```bash
sdlc gate analysis pass --security-sensitive --note "<what the threats came to, in one clause>"
```

Always pass the flag: `--security-sensitive` when the researcher reported
`security_sensitive: true`, `--security-sensitive=false` when it reported false. It decides whether
the security reviewer can block Gates 4 and 7, and left unset the loop assumes yes.

If either document was not stored, the gate refuses and says which. The agent's summary is not the
document, and later gates read the document: hand it to a person with
`sdlc escalate analysis_incomplete --message "..."` and stop.

If the agent reports `open_questions` above zero, or `split_recommended`, do not pass the gate.
Guessing an answer here is the most expensive mistake in the loop: everything downstream is built
on it.

```bash
sdlc gate analysis fail --note "<n> open questions the specification does not settle"
sdlc escalate spec_unclear --message "<the open questions>"
```

## Gate 3 — Acceptance tests, then frozen

Delegate to `sdlc:sdet`, with:

- the backlog file the story is in, and the paths to `ANALYSIS.md` and `THREATS.md`
- that `paths.tests` and `commands.test` in `.sdlc/config.json` say where tests live and how to
  run them
- that it stores its test plan with `sdlc artifact write test_plan`
- if a person lifted the freeze: their reason, the latest `unfreeze` event in
  `.sdlc/stories/<ID>/gate-record.json`, and that the job is to correct the test it names

Do not write the tests yourself, and do not adjust them afterwards: every later gate measures
against them.

If the agent reports anything in `untestable`, hand it to a person; writing something adjacent is
worse than saying so:

```bash
sdlc gate tests_frozen fail --note "AC-3 cannot be observed from outside"
sdlc escalate untestable --message "AC-3 cannot be observed from outside: <why>"
```

Otherwise run `commands.test` and read the output yourself. Every new test must fail; if one
passes, the behaviour already exists or the test does not test it, and the gate does not pass.
Then freeze:

```bash
sdlc freeze
sdlc gate tests_frozen pass --note "<n> criteria, <n> failing tests"
```

From then on nobody can edit the tests, you and their author included. Lifting the freeze is not
yours to do: the hook refuses `sdlc unfreeze` from you and every agent. If a frozen test is wrong,
hand it to a person with
`sdlc escalate frozen_test_wrong --message "<which test, and the criterion it gets wrong>"` and
stop. In their own terminal they run `sdlc approve <ID>`, then `sdlc start <ID>`, then
`sdlc unfreeze --reason "..."`, which opens this gate and every one after it again.

A test file added after the freeze stops `tests_frozen`, `plan`, `implementation`, `verification`
and `commit` (`SDLC-E0043`) until the freeze holds it. Only with `freeze.allow_new_test_files` on
may the test author add one; run `sdlc freeze` again after it does.

## Gate 4 — The plan, and its review

Delegate to `sdlc:implementer` with the backlog file and the paths to `ANALYSIS.md`,
`THREATS.md`, `TEST-PLAN.md` and the frozen tests. Tell it to store the plan with
`sdlc artifact write plan`, and that this is the plan only — no code yet.

```bash
sdlc gate plan pass --note "<n> steps, <what it changes>"
```

Then delegate the review to all five at once, each with the path to `PLAN.md`. They do not read
each other's work; run in sequence, the later ones only agree with the earlier. Each records its
own review with `sdlc review add design_review <role> <verdict>`.

| Agent | Verdict decides the gate? |
| --- | --- |
| `sdlc:architect` | yes — always |
| `sdlc:security` | yes when the story is security-sensitive |
| `sdlc:red-team` | no, but it must report |
| `sdlc:perf` | only by blocking, on a performance budget the contract states |
| `sdlc:human-advocate` | no, but it must report |

```bash
sdlc review list --gate design_review
sdlc gate design_review pass --note "<what the architect said, in one clause>"
```

The gate refuses until every reviewer has reported and the blocking ones have approved; perf
refuses it only by blocking; a revised plan needs its reviewers to look again. Do not edit the plan
yourself to satisfy a reviewer — send it back to the implementer with the findings. A block from a
reviewer that decides the gate is not a failure to route around:

```bash
sdlc gate design_review fail --note "architect blocked: AC-3 has no step"
```

## Gate 5 — Implementation

Delegate to `sdlc:implementer` again with the path to `PLAN.md`, and tell it the plan is
approved. Do not write any of this code yourself.

When it reports that the frozen tests pass, run the project's test command and read the output.
Then:

```bash
sdlc gate implementation pass --note "<n> files, frozen tests green"
```

If the gate finds the freeze broken, something edited an acceptance test: hand it to a person with
`sdlc escalate freeze_broken --message "..."` rather than freezing again over the top.

`SDLC-E0042`, here or at Gate 6 or 7 after rework, means the change is bigger than
`thresholds.diff_size_cap` lets one story be. Do not have it squeezed under the cap; record the
failure and hand the split to a person:

```bash
sdlc gate implementation fail --note "<n> lines against a cap of <cap>"
sdlc escalate story_too_large --message "<n> lines against a cap of <cap>: how should it be split?"
```

## Gate 6 — Independent verification

Delegate to `sdlc:verifier`, with nothing from this conversation beyond the paths: it re-derives
the criteria from the specification without having seen how the code came to be. It stores
`VERIFICATION.md` and records a blocking verdict with
`sdlc review add verifier_review verifier approve|block`.

```bash
sdlc gate verification pass --note "all commands green, <n> criteria verified"
sdlc gate verifier_review pass --note "no gaming found"
```

If it blocks, send its findings to `sdlc:implementer` as rework, then have the **same** verifier
look again. An approval from before the rework does not count.

## Gate 7 — Code review

Delegate to `sdlc:code-reviewer`, `sdlc:security`, `sdlc:perf` and `sdlc:human-advocate`, in
parallel. `sdlc review list` says which of them block for this story.

```bash
sdlc review list --gate code_review
sdlc gate code_review pass --note "<what the reviewer said, in one clause>"
```

Rework goes to `sdlc:implementer`, and then the same reviewers look again: their approvals are
stamped with the tree, so anything that changed makes them stale.

## Gate 8 — Commit

Only now: until every gate above has passed, the hook refuses `git commit` and names the missing
gate.

A story whose `risk_tier` the project holds for a person — `high`, unless
`human_gates.pre_commit_pause_tiers` says otherwise — waits for their approval. If nobody has
approved the work as it stands, hand it over and stop:

```bash
sdlc escalate pre_commit_approval --message "<what changed, what to look at, and why it is ready>"
```

An approval holds only for the work it was given for: change anything after it, and it needs
approving again. Once approved, a person types `/sdlc:next` again, and it resumes here.

Commit the whole story as one change, the code and the loop's record of it, with a message about
the change and why, in the project's own style:

```bash
git add -A
git commit -m "<type>: <what changed and why>"
sdlc gate commit pass --note "<what reached trunk, in one line>"
```

The gate refuses while anything is uncommitted, or if an acceptance test changed since the freeze.
The note needs no hash.

## Gate 9 — Retro

Delegate to `sdlc:bookkeeper`. It reads the gate record and the story's directory and stores
`RETRO.md`. If the session knows what it has cost, record it with `sdlc cost add --usd <amount>`
while the story is still active. Then:

```bash
sdlc gate retro pass --note "<n> deviations, <n> lessons"
sdlc stop
```

That finishes the story: it becomes `done` and leaves the backlog, its freeze is lifted, and the
next time a person types `/sdlc:next` it takes the next story. Tell the user what was built, what
deviated from the plan, and what the retro says to do differently. Commit the retro if the project
keeps it, along with the backlog file, which now records the story as done.

## Rework, at any gate

A gate that fails is recorded as failed, and the loop goes back, not around:

```bash
sdlc gate <gate> fail --note "<what went wrong>"
```

Do not pass a gate to keep moving, do a gate's work yourself because delegating it was refused, or
lift the freeze to make something pass. Do not change the contract to get past a gate either:
`CLAUDE.md` is refused while a story runs. If a frozen test or a contract rule is wrong, say which
criterion or gate it contradicts, and hand it to a person.

After `loop.max_rework_rounds` failures of the same gate, or `loop.max_review_rounds` blocks from
the same reviewer, `sdlc gate` or `sdlc review add` hands the story over, ends the iteration, and
says so — `handed_over` in its `--json`. Stop there: another attempt would meet the same wall.
