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
- Otherwise read `active`, `gates` and `next_gate` from the output. `next_gate` is the gate
  to work now: the loop resumes at the first gate that has not passed, and the tool works
  that out so you do not have to. Go straight to that section below.
- With no story active there is no `next_gate`; `next` names the story that would be picked
  up. Start at Gate 1.

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
sdlc gate analysis pass --security-sensitive --note "<what the threats came to, in one clause>"
```

Always pass the flag, one way or the other — `--security-sensitive` when the researcher reported
`security_sensitive: true`, and `--security-sensitive=false` when it reported false. It is not a
label: it decides whether the security reviewer can block Gates 4 and 7. Leaving it unset is not
the same as false; the loop then assumes the answer is yes, which is the safe direction to be
wrong in but will make you wonder later why security is blocking.

If either document was not stored the gate will refuse to pass, and say which one is missing.
That is not something to work around: the agent's summary is not the document, and the gates
after this one read the document. Show the user what the researcher reported and stop.

If the agent reports `open_questions` above zero, or `split_recommended`, do not pass the gate.
Record it as a failure with the reason, show the user the open questions, and stop. Guessing an
answer here is the single most expensive mistake in the loop: everything downstream is built on it.

```bash
sdlc gate analysis fail --note "<n> open questions the specification does not settle"
```

## Gate 3 — Acceptance tests, then frozen

Delegate to the `sdlc:sdet` agent. Give it a brief containing:

- the story id, and the paths to `story.json`, `ANALYSIS.md` and `THREATS.md`
- that `paths.tests` and `commands.test` in `.sdlc/config.json` say where tests live and how to
  run them
- that it stores its test plan with `sdlc artifact write test_plan`

Do not write the tests yourself, and do not adjust them afterwards. The tests are what every
later gate measures against, and a test shaped by the conversation that will also shape the
implementation measures nothing.

If the agent reports anything in `untestable`, stop. A criterion nobody can test is a
Definition-of-Ready problem that reached Gate 3, and writing something adjacent to it is worse
than saying so:

```bash
sdlc gate tests_frozen fail --note "AC-3 cannot be observed from outside"
```

Otherwise run the project's own test command — `commands.test` from `.sdlc/config.json` — and
read the output yourself. Every new test must fail. If any of them passes, the behaviour already
exists or the test does not test it; either way the gate does not pass.

Then freeze:

```bash
sdlc freeze
sdlc gate tests_frozen pass --note "<n> criteria, <n> failing tests"
```

`sdlc freeze` records what every test file contains. From that point the tests cannot be edited
by anyone, including you and including the agent that wrote them — the hook refuses it and points
at `sdlc unfreeze`, which asks for a reason and puts it on the record. Lifting the freeze is
sometimes right and is also exactly the shortcut that makes the rest of the loop meaningless, so
never do it to make something pass.

`sdlc status --json` reports the freeze and whether it is still intact.

## Gate 4 — The plan, and its review

Two gates, recorded separately: the plan, then the review of it.

Delegate to `sdlc:implementer` with the story id and the paths to `story.json`, `ANALYSIS.md`,
`THREATS.md`, `TEST-PLAN.md` and the frozen tests. Tell it to store the plan with
`sdlc artifact write plan`, and that this is the plan only — no code yet.

```bash
sdlc gate plan pass --note "<n> steps, <what it changes>"
```

Then the review. Delegate to all five, and give each one the story id and the path to `PLAN.md`:

| Agent | Verdict decides the gate? |
| --- | --- |
| `sdlc:architect` | yes — always |
| `sdlc:security` | yes when the story is security-sensitive |
| `sdlc:red-team` | no, but it must report |
| `sdlc:perf` | no, but it must report |
| `sdlc:human-advocate` | no, but it must report |

They can run at the same time; they do not read each other's work, and running them in sequence
only invites the later ones to agree with the earlier ones. Each records its own review with
`sdlc review add design_review <role> <verdict>`.

```bash
sdlc review list --gate design_review
sdlc gate design_review pass --note "<what the architect said, in one clause>"
```

The gate refuses until every reviewer has reported and the blocking ones have approved. It also
refuses an approval of a plan that has since changed: if the plan is revised, the reviewers who
approved the old one have to look again. Do not edit the plan yourself to satisfy a reviewer —
send it back to the implementer with the findings.

If a blocking reviewer blocks, that is not a failure to route around:

```bash
sdlc gate design_review fail --note "architect blocked: AC-3 has no step"
```

## Gate 5 — Implementation

Delegate to `sdlc:implementer` again, this time to carry out the approved plan. Give it the
path to `PLAN.md` and tell it the plan is approved.

Do not write any of this code yourself. The frozen tests are what the change is measured
against, and an implementation shaped by the conversation that will also review it is not
measured by anything.

When it reports that the frozen tests pass, run the project's own test command and read the
output. Then:

```bash
sdlc gate implementation pass --note "<n> files, frozen tests green"
```

The gate checks the freeze is still intact. If it is not, something edited an acceptance test:
stop and show the user, rather than freezing again over the top.

## Gate 6 — Independent verification

Delegate to `sdlc:verifier`. Give it the story id and nothing from this conversation beyond the
paths — the point of it is that it re-derives the criteria from the specification without
having seen how the code came to be.

It stores `VERIFICATION.md` and records a blocking verdict with
`sdlc review add verifier_review verifier approve|block`.

```bash
sdlc gate verification pass --note "all commands green, <n> criteria verified"
sdlc gate verifier_review pass --note "no gaming found"
```

If the verifier blocks, send its findings back to `sdlc:implementer` as rework, then have the
**same** verifier look again. Its verdict is stamped with the tree it reviewed, so an approval
from before the rework does not count and the gate will say so.

## Gate 7 — Code review

Delegate to `sdlc:code-reviewer`, `sdlc:security`, `sdlc:perf` and `sdlc:human-advocate`, in
parallel, with the story id. The code reviewer blocks; security blocks when the story is
security-sensitive; the other two report.

```bash
sdlc review list --gate code_review
sdlc gate code_review pass --note "<what the reviewer said, in one clause>"
```

Rework goes back to `sdlc:implementer`, and then the same reviewers look again — their
approvals are stamped with the tree, so anything that changed makes them stale.

## Gate 8 — Commit

Only now. Until every gate above has passed, `git commit` is refused by the hook, which will
tell you which gate is missing.

Commit the whole story as one change: the code, and the loop's own record of how it got there.
Write the message about the change and why, in the project's own style.

```bash
git add -A
git commit -m "<type>: <what changed and why>"
sdlc gate commit pass --note "<short sha>"
```

The gate refuses while anything is uncommitted, and refuses if an acceptance test changed since
the freeze.

## Gate 9 — Retro

Delegate to `sdlc:bookkeeper` with the story id. It reads the gate record and everything under
the story's directory and stores `RETRO.md`.

```bash
sdlc gate retro pass --note "<n> deviations, <n> lessons"
sdlc stop
```

Passing this gate finishes the story: no gate is left, so it becomes `done` and leaves the
backlog, the test freeze it was holding is lifted, and the next `/sdlc:next` takes the next
story.

If the session knows what it has cost — `/cost` in an interactive session, `total_cost_usd`
in the output of a headless one — record it before stopping, so the story's record says what
it cost as well as what it did:

```bash
sdlc cost add --usd <amount>
```

Then tell the user what was built, what deviated from the plan, and what the retro recorded as
worth doing differently. Commit the retro if the project keeps it, along with the backlog file,
which now records the story as done.

## Rework, at any gate

A gate that fails is recorded as failed and the loop goes back, it does not go around:

```bash
sdlc gate <gate> fail --note "<what went wrong>"
```

Do not pass a gate to keep moving. Do not do a gate's work yourself because delegating it was
refused. Do not lift the test freeze to make something pass — if a frozen test is genuinely
wrong, say which acceptance criterion it contradicts and ask the user.

Do not change the contract to get past a gate either. `CLAUDE.md` is refused while a story is
running for the same reason a frozen test is: a rule you can edit is a rule that stopped
applying to you. If a rule in the `## SDLC Contract` section is genuinely wrong, say which rule
blocks which gate, and ask the user to change it.

If the same gate fails three times, stop and show the user. Something upstream is wrong, and a
fourth attempt will find the same wall.

## If a command fails

Every failure carries a code, a reason and a fix. Show the user the `error`, `why` and `fix`
fields as they are. Do not paraphrase them, and do not retry a command that failed for a reason
that has not changed.
