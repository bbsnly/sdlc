# The loop

One story at a time, through eleven recorded gates. This page says what each one
is for, who does it, what it produces, and what the tool refuses if you try to
pass it without doing it.

The nine gates on the front page map to eleven record keys, because two of them
are split into the work and the review of it: the plan and its design review,
and the verification and the verdict on it.

## The shape of it

Every gate follows the same shape, and the shape is the point:

1. Work is delegated to an agent that starts from a fresh context.
2. The agent stores what it produced **through the tool**, never as a file it
   writes itself.
3. The outcome is recorded, and the tool refuses to record a pass unless the
   work is actually there.

Nothing is taken on trust, including the assistant running the loop. A summary
saying a document exists is not the document.

## Where everything lives

```text
.sdlc/
  config.json                    the settings
  state/
    active                       the story being worked on
    tests.lock                   the freeze: every test file, by content
  stories/
    AUTH-3/
      gate-record.json           every gate, event and review, in order
      ANALYSIS.md                gate 2
      THREATS.md                 gate 2
      TEST-PLAN.md               gate 3
      PLAN.md                    gate 4
      VERIFICATION.md            gate 6
      RETRO.md                   gate 9
      reviews/
        design_review-architect-1.md
        code_review-code-reviewer-1.md
```

All of it is text in your repository, which is what makes it reviewable. Whether
you commit it is your decision; the loop does not write a `.gitignore`.

## Gate 1 — `dor`, definition of ready

**Who:** the conversation. **Produces:** nothing.

The story is selected and its acceptance criteria are read as a test author
would read them. A criterion that describes an implementation rather than an
observable behaviour fails the gate here, which is the cheapest place in the
loop to find out.

## Gate 2 — `analysis`

**Who:** `sdlc:researcher`. **Produces:** `ANALYSIS.md`, `THREATS.md`.

The story is read against the codebase before anything is written: what exists,
what it touches, what the current behaviour actually is with `file:line`, and
what could go wrong at each trust boundary it crosses. It also decides whether
the story is security-sensitive, which is what makes the security reviewer
blocking at Gates 4 and 7.

Open questions stop the loop rather than being guessed at. Everything downstream
is built on this.

**Refused without it:** a pass with either document missing.

## Gate 3 — `tests_frozen`

**Who:** `sdlc:sdet`. **Produces:** `TEST-PLAN.md`, the tests, and the freeze.

Each acceptance criterion becomes a test that fails now, for the reason the
criterion names. The test plan maps every criterion to the test that covers it
and says why each fails today.

Then `sdlc freeze` records the sha256 of every test file. From this moment
nobody edits them — not the implementer, not the agent that wrote them, not the
conversation. `sdlc unfreeze --reason "..."` lifts it and puts the reason on the
record.

**Refused without it:** a pass with no test plan, no freeze, a freeze belonging
to another story, or a frozen file that has changed or vanished.

This is the gate the rest of the loop rests on. An agent that can edit its own
acceptance tests will eventually edit them — not out of malice, just by taking
the shortest path to green — and every gate after that is theatre.

## Gate 4 — `plan`, then `design_review`

**Who:** `sdlc:implementer` writes it; `sdlc:architect`, `sdlc:security`,
`sdlc:red-team`, `sdlc:perf` and `sdlc:human-advocate` review it.
**Produces:** `PLAN.md` and five reviews.

The plan is the smallest change that makes the frozen tests pass, in steps
somebody else could follow, with the alternative that was rejected and why.

The reviewers run in parallel and do not read each other's work. The architect
blocks; security blocks when the story is security-sensitive; the other three
report but cannot stop the gate — and cannot be skipped either.

**Refused without it:** a pass with the plan missing, a reviewer that has not
reported, a blocking reviewer that has not approved, or an approval of a plan
that has since changed.

## Gate 5 — `implementation`

**Who:** `sdlc:implementer`. **Produces:** the code.

The plan is carried out one step at a time, running the tests after each. The
implementer cannot touch any test file, and cannot add a new one.

**Refused without it:** a pass when the freeze is gone or broken.

## Gate 6 — `verification`, then `verifier_review`

**Who:** `sdlc:verifier`. **Produces:** `VERIFICATION.md` and a blocking verdict.

A verifier that has not seen the implementation re-derives what each acceptance
criterion should mean, compares that with the frozen tests, runs every command
the project configures, and hunts by hand for the thing tests cannot catch: an
implementation that passes them without the behaviour being there. It checks
every gaming vector the red team named at Gate 4.

**Refused without it:** a pass with the verification document missing, a broken
freeze, or a verifier that blocked or has not approved the current tree.

## Gate 7 — `code_review`

**Who:** `sdlc:code-reviewer`, plus `sdlc:security`, `sdlc:perf` and
`sdlc:human-advocate`. **Produces:** four reviews.

The diff is reviewed in a context that never saw the reasoning that produced it.
That is the whole value: a reviewer that inherited the justification cannot
notice what only makes sense if you already knew why.

**Refused without it:** the same conditions as the design review, against the
tree rather than the plan.

## Gate 8 — `commit`

**Who:** the conversation. **Produces:** the commit.

`git commit` is refused by the hook until every gate above has passed, and the
refusal names the one that has not.

**Refused without it:** a pass while anything is uncommitted, or with a broken
freeze.

## Gate 9 — `retro`

**Who:** `sdlc:bookkeeper`. **Produces:** `RETRO.md`.

What deviated from the plan, what sent the story back and where it could have
been caught one gate earlier, what this story actually demonstrated, and what
was deliberately left undone.

**Refused without it:** a pass with the retro missing.

Passing it is also what finishes the story. With no gate left unpassed, the
story's status becomes `done` and it leaves the backlog. End the iteration with
`sdlc stop`, and the next `sdlc start` takes the next story rather than
reopening this one — it refuses to start a finished story at all. Commit the
backlog change along with the retro.

## Rework

A gate that fails is recorded as failed, and the loop goes back rather than
around:

```console
$ sdlc gate design_review fail --note "architect blocked: AC-3 has no step"
```

There is no flag that skips a gate, and no way to record one out of order. When
a reviewer blocks, the way past is the same reviewer looking again — its new
verdict replaces the old one, and the gate reads the latest.

This holds after the story is finished, too. Recording a gate as failed on a
story already marked `done` puts it back to `in_progress`: being done is a
reading of the gate record, not a door that locks behind you. It is also the
only way back in, which is what keeps reopening a decision somebody made rather
than a side effect of running a command.

## Why approvals go stale

A review is stamped with what was in front of it: the plan's content at Gate 4,
the whole working tree at Gates 6 and 7. Change the plan, or touch a line of
code, and every approval of the old one becomes stale and the gate says so.

The loop's own directory is left out of that tree hash. Recording a review
writes a file under `.sdlc/`, so counting it would make every review stale the
instant it was filed.
