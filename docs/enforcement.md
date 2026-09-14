# Enforcement

The loop is a set of rules, and rules that are only asked for are advice. This
page lists every rule that is actually enforced, what it refuses, and what to do
instead.

## What this is, and what it is not

Enforcement uses Claude Code's own surfaces and nothing else: a `PreToolUse`
hook, the plugin's agents, and the `sdlc` command. No git hooks are installed,
and `sdlc` never writes to `.git/hooks`.

**This is a discipline tool, not a sandbox.** It constrains an assistant that is
trying to do the right thing and takes the shortest path to it. It does not
defend against one that is trying to escape, and it would be dishonest to imply
otherwise. The rules below are narrow on purpose, and every refusal names the
sanctioned route — a refusal that only says "no" makes an assistant retry, work
around the hook, or give up, all worse than the thing being prevented.

## When any of it applies

Nothing is enforced unless **all** of these are true:

1. The project has `.sdlc/config.json`.
2. A story is being worked on — `.sdlc/state/active` names one.
3. `SDLC_ENFORCE` is not `0` in the environment the session started with.

Outside those, the hook allows everything and says nothing. It also **fails
open**: an unreadable payload, a missing configuration, a path it cannot
resolve — an error ends in "carry on", with a message saying what is off. A hook
that blocks a session because of its own bug is worse than the mistake it was
trying to prevent.

The exceptions are the files a rule exists to protect. A test freeze or a gate
record that cannot be read is not treated as absent, because then breaking one
would be the way round it: tests stay frozen and the commit waits until the file
reads. `sdlc doctor` names it under "loop state".

## Rules on writing files

These apply to `Write`, `Edit`, `MultiEdit` and `NotebookEdit`. Reading,
searching and running things are nobody's business here.

### `write-outside-repository`

Refuses a write that resolves outside the repository, symlinks followed.

*Instead:* work inside the repository; if you need a file elsewhere, ask.

### `write-protected-path`

Refuses any write to `.git`, `.claude`, `CLAUDE.md`, `.sdlc/config.json`,
`.sdlc/state` or `.sdlc/claude-progress.json`.

Configuration is the human's, and loop state is the tool's. An assistant editing
either can make the record say something that did not happen.

*Instead:* change configuration by hand outside a running iteration; change loop
state through the `sdlc` command.

`CLAUDE.md` is in that list for the same reason the test freeze exists. A rule
the agent can edit is a rule that stopped applying to it, and a contract rewritten
to get past a gate takes every gate after it with it. If a rule is genuinely
wrong, the loop's answer is to stop and say so, not to edit it and carry on.

### `gate-record-is-written-by-the-tool`

Refuses any in-place edit of `gate-record.json`, in any story's directory and
not only the one being worked on. A finished story's record is the evidence
that it finished, and the same is true of its reviews and its documents below.

*Instead:* `sdlc gate <name> pass|fail --note "..."`.

### `review-is-written-by-the-tool`

Refuses any write under a story's `reviews/` directory.

*Instead:* `sdlc review add <gate> <role> <verdict>`, which stamps the review
with what was in front of the reviewer.

### `gate-artifact-is-written-by-the-tool`

Refuses any in-place write of `ANALYSIS.md`, `THREATS.md`, `TEST-PLAN.md`,
`PLAN.md`, `VERIFICATION.md` or `RETRO.md`.

This one refuses **everyone**, including the agent whose gate it is. Two reasons,
and they point the same way. A gate's document is loop state, and loop state
goes through the tool. And Claude Code refuses a subagent's file write when the
filename reads like a report, so "only the researcher may write `ANALYSIS.md`"
is a rule nobody could keep.

*Instead:* `sdlc artifact write <name>`.

### `frozen-test-is-not-edited`

Refuses any edit of a file in the freeze, from any role.

*Instead:* change the code until the test passes. If the test itself is wrong,
say which acceptance criterion it got wrong and stop. The person running the
session lifts the freeze with `sdlc unfreeze --reason "..."`, which puts the
change on the record.

### `no-new-test-after-the-freeze`

Refuses a new test file once the tests are frozen, unless
`freeze.allow_new_test_files` is on.

*Instead:* put the case in one of the frozen files. If it needs a file of its
own, say so and stop: the person running the session can unfreeze and freeze
again so the new file is covered.

### `implementer-does-not-write-tests`

Refuses the implementer writing any test file, frozen or not, before the freeze
or after it. This is not a setting.

*Instead:* make the existing tests pass; if they are wrong, say so rather than
changing them.

### `sdet-writes-tests-only`

Refuses the test author writing anything that is not a test, its own story
directory, or `CODEMAP.md`.

*Instead:* write the tests. If this project keeps fixtures somewhere the loop
does not recognise, add that directory to `paths.tests`.

### `researcher-writes-analysis-only`

Refuses the analysis agent writing outside its own story directory and
`CODEMAP.md`.

### `reviewer-reviews`

Refuses any of the seven reviewing agents — architect, security, red-team, perf,
human-advocate, verifier and code-reviewer — writing outside the story's own
directory. The retro agent has a rule of its own, below.

None of the seven is given a file-writing tool, so this is a backstop, not the
thing keeping them out: it is what still holds if an agent's tool list grows.
Like every rule on this page, it governs the file-writing tools. A reviewer's
shell commands meet the shell rules instead, which protect loop state, the
freeze and the commit, and not your source.

A reviewer that changes the work is reviewing its own, and the independence that
made the review worth having is gone. The story directory stays open so a
reviewer can work something out on paper; the verdict itself still goes through
`sdlc review add`, which the rule above holds it to.

*Instead:* record the verdict with `sdlc review add <gate> <role> approve|block`
and say what is wrong rather than fixing it.

### `bookkeeper-writes-the-retro-and-the-map`

Refuses the retro agent writing anything but its own story directory and
`CODEMAP.md`.

The retro records what happened. An agent that can change the code while writing
up the lessons can also write up lessons it has just made untrue.

*Instead:* `sdlc artifact write retro`; `CODEMAP.md` is the only other thing this
gate produces.

### `orchestrator-delegates`

Refuses the main conversation writing code or tests during an iteration.

The gates exist so that each is done by an agent that cannot see the others'
reasoning. The conversation doing the work itself is the failure the loop exists
to prevent, and it is reached without breaking a single stated rule unless
something refuses it.

*Instead:* delegate to the agent whose gate it is, or `sdlc stop` to end the
iteration and take over yourself.

## Rules on shell commands

Every rule above governs the file-writing tools. A shell command is not one of
them, which would leave `cat > .sdlc/stories/A-1/ANALYSIS.md` walking past all
of them. These six close that, and nothing else about your shell is touched:
your tests, your build and your tooling run exactly as before.

This is pattern matching, not a shell. It is deliberately narrow.

### `loop-state-through-the-tool`

Refuses a command that would write or delete the loop's own files: `.sdlc/state`,
`.sdlc/config.json`, any story's `gate-record.json`, any `reviews/` directory,
or any gate document — by redirection, or through `rm`, `mv`, `cp`, `tee`,
`sed -i` and their like.

A scratch file inside a story's directory is not loop state, and reading any of
it is never refused.

*Instead:* `sdlc artifact write`, `sdlc review add`, `sdlc gate`. The freeze is
lifted by the person running the session.

### `protected-path-through-the-tool`

Refuses a command that would write or delete `CLAUDE.md`, anything under
`.claude` or anything under `.git` — the paths `write-protected-path` protects
from the file tools that are not the loop's own state. It matches the way the
filesystem does, so `claude.md` is `CLAUDE.md` on macOS and Windows.

A settings file under `.claude` is where hooks are configured, and `CLAUDE.md`
is the contract every gate reads, so a shell command that rewrote either would
take the rules with it. Reading them is never refused, and neither is
`.gitignore` or `.github/`.

*Instead:* change them by hand outside a running iteration. If a rule is wrong,
stop and say which one.

### `frozen-test-through-the-tool`

Refuses a command that would write, rewrite or delete a frozen acceptance test:
by redirection, through `rm`, `mv`, `cp`, `tee`, `sed -i` and their like, or
from inside a program handed to an interpreter with `-c` or `-e`.

Reading one is never refused — `cat`, `grep`, `sed -n` and the test runner all
work as they always did, and reading the tests is how the implementer knows
what to implement. Before the freeze this rule does nothing at all.

*Instead:* leave it alone. If it genuinely has to change, say which test and
why; the person running the session lifts the freeze with
`sdlc unfreeze --reason "..."`.

### `unfreeze-is-a-human-decision`

Refuses `sdlc unfreeze` run from a tool call, however `sdlc` is reached — on
`PATH`, by path, through `npx` or `go run`.

Lifting the freeze is sometimes right, and it is also exactly the move an agent
would make to reach green. So it is a person's decision: they run it in their
own terminal, where the hook is not asked. A note that only mentions the word,
such as `--note "no need to unfreeze"`, is not refused.

*Instead:* say which frozen test is wrong and which acceptance criterion it gets
wrong, and stop there.

### `commit-gate`

Refuses `git commit` until every gate before the commit gate has passed. The
refusal names the first one that has not.

Once they have, it also refuses a commit of work that has changed since the
verifier and the code reviewers approved it. Their reviews are stamped with the
working tree as it was, outside `.sdlc/`, and a change anywhere in it makes them
stale; the refusal names whose. Measuring the tree takes a moment, so it is done
only for `git commit`.

A gate record that is missing or will not parse counts as no gate passed, and
the refusal names the file. `sdlc start` always writes one, so either means
something damaged it. `sdlc stop` still ends the iteration when the record
cannot be read.

*Instead:* finish the gates — `sdlc status` shows where the story stands — or
`sdlc stop` to end the iteration and commit as yourself.

### `enforcement-stays-on`

Refuses a command that sets `SDLC_ENFORCE`, `SDLC_BIN` or `CLAUDE_PROJECT_DIR`.

The switch that turns enforcement off belongs to the person who started the
session. A switch an assistant can reach is not a control.

*Instead:* if a rule is wrong, say which one and why.

## Rules the command enforces

The hook is only half of it. `sdlc gate <name> pass` refuses when the gate did
not actually happen:

| Refused | Code |
| --- | --- |
| an earlier gate has not passed | `SDLC-E0031` |
| the gate's documents are not on disk | `SDLC-E0020` |
| a reviewer has not reported, or reviewed something that has changed since | `SDLC-E0029` |
| a blocking reviewer blocked | `SDLC-E0030` |
| the tests are not frozen, or the freeze belongs to another story | `SDLC-E0023` |
| a frozen test changed or vanished | `SDLC-E0025` |
| the commit gate, with work still uncommitted | `SDLC-E0032` |

A `fail` is always recordable. A gate can fail precisely because its work could
not be done, and refusing to record that would leave the loop with nowhere to
put the truth.

## Turning it off

`SDLC_ENFORCE=0` in the environment you start the session from disables every
hook rule. It cannot be set from inside a session.

Use it when you are debugging the loop itself. If you find yourself reaching for
it during ordinary work, the rule that is in your way is probably wrong — please
open an issue and say which one.

## Seeing what happened

A hook that allows a call says nothing unless something is wrong, and its stderr
goes to Claude Code's debug log, which makes "why did nothing happen" the
hardest question to answer from outside. Both of these together turn the hook's
decisions into a log you can read:

```console
$ export SDLC_DEBUG=1
$ export SDLC_DEBUG_FILE=/tmp/sdlc-hook.log
```

`SDLC_DEBUG_FILE` on its own produces nothing.
