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

- If the command is not found at all, you have this runbook without the tool it drives. Offer to
  install it, and wait for an answer. On macOS or Linux that is
  `curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh`; on Windows,
  `irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex`. Both check the
  download against the release's checksums before anything is put on a PATH. If they say yes,
  run it and then run `sdlc status --json` again. If it is still not found, the binary landed in
  a directory this session's PATH does not have — say which directory the installer reported and
  that a new session will see it, and stop. Do not work the gates by hand: a loop with no record
  behind it is the failure this exists to prevent.
- If it fails with `SDLC-E0002`, this project has not been set up. Tell the user, and offer to
  run `sdlc init`. Stop until they answer.
- If it fails with `SDLC-E0001`, you are not in a Git repository. Say so and stop.
- Otherwise read `active`, `gates` and `next_gate` from the output. `next_gate` is the gate
  to work now: the loop resumes at the first gate that has not passed, and the tool works
  that out so you do not have to. Go straight to that section below.
- With no story active there is no `next_gate`; `next` names the story that would be picked
  up. Start at Gate 1.

Each gate below is delegated to an agent that ships with the plugin. If those agents are not
available in this session, the plugin is not installed — this runbook is here on its own. Tell
the user to run `/plugin marketplace add bbsnly/sdlc` and `/plugin install sdlc@sdlc`, and stop.
Without the agents there is no fresh context per gate and no hook refusing anything, and a loop
you run yourself from end to end is a checklist you are marking off about your own work.

## Gate 1 — Select the story

Run:

```bash
sdlc start --json
```

This picks up a story already under way, or takes the next runnable one. If it fails with
`SDLC-E0010` there is nothing to work on: show the user the `why` field from the error, run
`sdlc story list` so they can see the backlog, and stop.

If it fails with `SDLC-E0038`, `SDLC-E0039`, `SDLC-E0040` or `SDLC-E0041`, trunk is not somewhere
a new story can start from: HEAD is on another branch, there is uncommitted work that belongs to
no story, trunk is behind `origin`, or it fails its smoke check. Show the user the error as it
is, and stop. Do not commit, stash, discard or fix anything on trunk to get past it — that work
is not a story's, and nothing the loop records would account for it.

Read the story from the backlog file named by `backlog.path` in `.sdlc/config.json`. Check its
acceptance criteria are testable: each one names an observable behaviour, not an implementation.
If a criterion cannot be turned into a failing test, that is a Definition-of-Ready problem —
record it, and hand the question to a person rather than inventing an interpretation:

```bash
sdlc gate dor fail --note "AC-2 describes an implementation, not a behaviour"
sdlc escalate spec_unclear --message "AC-2 describes an implementation: what should a user see?"
```

A project can ask for the human advocate at this gate too, and then the gate waits for it. Check:

```bash
sdlc review list --gate dor --json
```

If it lists `human-advocate`, delegate to `sdlc:human-advocate` with the story id, and read what
it found before deciding. Its review is advisory, but a story nobody would want built is not
ready either.

When the story is ready:

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
after this one read the document. Hand it to a person with what the researcher reported —
`sdlc escalate analysis_incomplete --message "..."` — and stop.

If the agent reports `open_questions` above zero, or `split_recommended`, do not pass the gate.
Record it as a failure with the reason, and hand the open questions to a person. Guessing an
answer here is the single most expensive mistake in the loop: everything downstream is built on it.

```bash
sdlc gate analysis fail --note "<n> open questions the specification does not settle"
sdlc escalate spec_unclear --message "<the open questions>"
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

If the agent reports anything in `untestable`, hand it to a person. A criterion nobody can test
is a Definition-of-Ready problem that reached Gate 3, and writing something adjacent to it is
worse than saying so:

```bash
sdlc gate tests_frozen fail --note "AC-3 cannot be observed from outside"
sdlc escalate untestable --message "AC-3 cannot be observed from outside: <why>"
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
by anyone, including you and including the agent that wrote them. Lifting the freeze is sometimes
right and is also exactly the shortcut that makes the rest of the loop meaningless, so it is not
yours to do: the hook refuses `sdlc unfreeze` from you and from every agent. If a frozen test is
wrong, hand it to a person with
`sdlc escalate frozen_test_wrong --message "<which test, and the criterion it gets wrong>"` and
stop. They run `sdlc unfreeze --reason "..."` and `sdlc approve` in their own terminal, and the
next session carries on.

`sdlc status --json` reports the freeze and whether it is still intact.

A test file added after the freeze holds every gate from here to the commit until the freeze
holds it too (`SDLC-E0043`). Only a project with `freeze.allow_new_test_files` on lets the test
author add one; run `sdlc freeze` again after it does, and the new file is added to the freeze.

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
| `sdlc:perf` | only by blocking, on a performance budget the contract states |
| `sdlc:human-advocate` | no, but it must report |

They can run at the same time; they do not read each other's work, and running them in sequence
only invites the later ones to agree with the earlier ones. Each records its own review with
`sdlc review add design_review <role> <verdict>`.

```bash
sdlc review list --gate design_review
sdlc gate design_review pass --note "<what the architect said, in one clause>"
```

The gate refuses until every reviewer has reported and the blocking ones have approved, and a
block from perf refuses it too — perf never has to approve, but it blocks when the change breaks
a stated performance budget. It also
refuses an approval of a plan that has since changed: if the plan is revised, the reviewers who
approved the old one have to look again. Do not edit the plan yourself to satisfy a reviewer —
send it back to the implementer with the findings.

If a blocking reviewer blocks, or perf does, that is not a failure to route around:

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
hand it to a person with `sdlc escalate freeze_broken --message "..."`, rather than freezing
again over the top.

If it fails with `SDLC-E0042`, the change is bigger than `thresholds.diff_size_cap` lets one
story be. Do not have the implementer squeeze it under the cap: that trades a story too big to
review for one cramped to fit. Record the gate as failed and hand the split to a person:

```bash
sdlc gate implementation fail --note "<n> lines against a cap of <cap>"
sdlc escalate story_too_large --message "<n> lines against a cap of <cap>: how should it be split?"
```

The same refusal can come at Gate 6 or Gate 7, when rework has grown the change. Handle it the
same way there.

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
parallel, with the story id. The code reviewer blocks, unless the project turned on
`reviews.gate7_advisory`; security blocks when the story is security-sensitive; perf blocks only
when the change breaks a stated performance budget; the human advocate reports. `sdlc review list`
says which is which for this story.

```bash
sdlc review list --gate code_review
sdlc gate code_review pass --note "<what the reviewer said, in one clause>"
```

Rework goes back to `sdlc:implementer`, and then the same reviewers look again — their
approvals are stamped with the tree, so anything that changed makes them stale.

## Gate 8 — Commit

Only now. Until every gate above has passed, `git commit` is refused by the hook, which will
tell you which gate is missing.

First, a story whose `risk_tier` is one the project holds for a person — `high`, unless
`human_gates.pre_commit_pause_tiers` says otherwise — waits for their approval. If nobody has
approved the work as it stands, hand it over and stop:

```bash
sdlc escalate pre_commit_approval --message "<what changed, what to look at, and why it is ready>"
```

The commit is refused until a person has run `sdlc approve`, and an approval holds only for the
work it was given for: change anything after it, and it needs approving again. Once approved,
the next `/sdlc:next` resumes here.

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
wrong, say which acceptance criterion it contradicts, and hand that to a person.

Do not change the contract to get past a gate either. `CLAUDE.md` is refused while a story is
running for the same reason a frozen test is: a rule you can edit is a rule that stopped
applying to you. If a rule in the `## SDLC Contract` section is genuinely wrong, say which rule
blocks which gate, and hand it to a person, who can change it.

A gate that keeps failing, and a reviewer that keeps blocking, go to a person without you
deciding it. After `loop.max_rework_rounds` failures of the same gate, or `loop.max_review_rounds`
blocks from the same reviewer, `sdlc gate` or `sdlc review add` hands the story over, ends the
iteration, and says so — `handed_over` in its `--json`. Stop there: something upstream is wrong,
and another attempt would meet the same wall.

## Handing a decision to a person

Some questions are not yours to answer: acceptance criteria that contradict each other, a frozen
test that is genuinely wrong, a gate that keeps failing for the same reason. Hand them over:

```bash
sdlc escalate <type> --message "<the question, and what you found>"
```

Then stop, and tell the user what you asked. The iteration has ended, and the story waits until
a person answers with `sdlc approve` in their own terminal. Do not run `sdlc approve` yourself
— the hook refuses it — and do not start the story again before they have answered.

Do not end your turn mid-story any other way. While a story is being worked on, the plugin's
Stop hook sends a stop back until it is one of three things: the gate finished, the story handed
to a person, or the iteration ended with `sdlc stop`. After `loop.max_stop_blocks` stops in a row
with nothing recorded, it hands the story to a person itself.

## If a command fails

Every failure carries a code, a reason and a fix. Show the user the `error`, `why` and `fix`
fields as they are. Do not paraphrase them, and do not retry a command that failed for a reason
that has not changed.
