# Enforcement

- [Introduction](#introduction)
- [What sdlc stops, and what it leaves to you](#what-sdlc-stops-and-what-it-leaves-to-you)
  - [When any of it applies](#when-any-of-it-applies)
  - [A person's decisions, refused in every session](#a-persons-decisions-refused-in-every-session)
  - [How a command is read](#how-a-command-is-read)
  - [A path outside the repository](#a-path-outside-the-repository)
- [Decisions that are yours](#decisions-that-are-yours)
  - [`approval-is-a-human-decision`](#approval-is-a-human-decision)
  - [`unfreeze-is-a-human-decision`](#unfreeze-is-a-human-decision)
  - [`acknowledgement-is-a-human-decision`](#acknowledgement-is-a-human-decision)
  - [`remap-is-a-human-decision`](#remap-is-a-human-decision)
  - [`release-is-a-human-decision`](#release-is-a-human-decision)
  - [`assignment-is-a-human-decision`](#assignment-is-a-human-decision)
  - [`stop-is-a-human-decision`](#stop-is-a-human-decision)
  - [`slash-command-is-a-human-decision`](#slash-command-is-a-human-decision)
- [The tests](#the-tests)
  - [`frozen-test-is-not-edited`](#frozen-test-is-not-edited)
  - [`frozen-test-through-the-tool`](#frozen-test-through-the-tool)
  - [`no-new-test-after-the-freeze`](#no-new-test-after-the-freeze)
  - [`implementer-does-not-write-tests`](#implementer-does-not-write-tests)
- [Each agent's job](#each-agents-job)
  - [`orchestrator-delegates`](#orchestrator-delegates)
  - [`sdet-writes-tests-only`](#sdet-writes-tests-only)
  - [`researcher-writes-analysis-only`](#researcher-writes-analysis-only)
  - [`reviewer-reviews`](#reviewer-reviews)
  - [`bookkeeper-writes-the-retro-and-the-map`](#bookkeeper-writes-the-retro-and-the-map)
  - [`review-is-recorded-by-its-reviewer`](#review-is-recorded-by-its-reviewer)
  - [`staffing-is-recorded-by-the-project-manager`](#staffing-is-recorded-by-the-project-manager)
  - [`project-manager-only-staffs`](#project-manager-only-staffs)
- [The loop's records](#the-loops-records)
  - [`loop-state-through-the-tool`](#loop-state-through-the-tool)
  - [`gate-record-is-written-by-the-tool`](#gate-record-is-written-by-the-tool)
  - [`review-is-written-by-the-tool`](#review-is-written-by-the-tool)
  - [`gate-artifact-is-written-by-the-tool`](#gate-artifact-is-written-by-the-tool)
  - [`trace-is-written-by-the-tool`](#trace-is-written-by-the-tool)
  - [`decisions-are-written-by-the-tool`](#decisions-are-written-by-the-tool)
  - [`recorded-event-comes-from-the-host`](#recorded-event-comes-from-the-host)
  - [`commands-run-as-configured`](#commands-run-as-configured)
- [Your configuration and instructions](#your-configuration-and-instructions)
  - [`write-protected-path`](#write-protected-path)
  - [`protected-path-through-the-tool`](#protected-path-through-the-tool)
  - [`backlog-is-not-edited`](#backlog-is-not-edited)
  - [`review-policy-is-not-edited`](#review-policy-is-not-edited)
  - [`claude-config-is-the-persons`](#claude-config-is-the-persons)
  - [`agents-config-is-the-persons`](#agents-config-is-the-persons)
  - [`instructions-above-the-project`](#instructions-above-the-project)
  - [`write-outside-repository`](#write-outside-repository)
- [Commits, branches and checkouts](#commits-branches-and-checkouts)
  - [`commit-gate`](#commit-gate)
  - [`commit-in-another-checkout`](#commit-in-another-checkout)
  - [`branch-through-the-tool`](#branch-through-the-tool)
  - [`worktree-through-the-tool`](#worktree-through-the-tool)
  - [`worktree-setting-through-the-tool`](#worktree-setting-through-the-tool)
- [Keeping the rules on](#keeping-the-rules-on)
  - [`enforcement-stays-on`](#enforcement-stays-on)
  - [`story-stays-in-its-session`](#story-stays-in-its-session)
- [When a command cannot be checked](#when-a-command-cannot-be-checked)
  - [`not-checked-in-time`](#not-checked-in-time)
  - [`command-too-deep-to-read`](#command-too-deep-to-read)
  - [`git-command-cannot-be-read`](#git-command-cannot-be-read)
  - [`archive-members-unknown`](#archive-members-unknown)
  - [`directory-entries-unknown`](#directory-entries-unknown)
  - [`too-many-links`](#too-many-links)
  - [`path-too-long`](#path-too-long)
- [Stopping mid-story](#stopping-mid-story)
- [Rules the command enforces](#rules-the-command-enforces)
- [What sdlc does not stop](#what-sdlc-does-not-stop)
  - [When the hook cannot do its job](#when-the-hook-cannot-do-its-job)
  - [Not held](#not-held)
  - [Report a hole](#report-a-hole)
- [Turning it off](#turning-it-off)
- [Seeing what happened](#seeing-what-happened)
- [Where to go next](#where-to-go-next)

## Introduction

The loop is a set of rules, and a rule an agent is only asked to keep is
advice. So `sdlc` enforces them. While a story is being worked on, a hook looks
at every file the agent is about to write and every shell command it is about
to run, and refuses the few that would get round a gate: committing early,
editing a frozen test, approving its own work.

A refusal says what the rule is, what to do instead, and the rule's name in
square brackets:

```text
this story has not been through the gates that come before committing: dor has not passed. Instead: finish the gates -- `sdlc status` shows where this story stands; committing without them is for the person running the session to decide [commit-gate]
```

Most of the time the agent reads that and carries on by itself. This page is
where you look the rule up. It says what `sdlc` stops and what it leaves to
you, lists every rule under the name a refusal prints, says plainly what is not
stopped, and ends with how to turn enforcement off and how to see what the hook
decided.

If you have a refusal in front of you and want to be walked through it, start
with [Why was I refused?](guides/why-was-i-refused.md).

## What sdlc stops, and what it leaves to you

Everything `sdlc` enforces protects one of seven things. Each row links to its
rules:

| What is protected | An agent cannot | Left to you |
| --- | --- | --- |
| [Your decisions](#decisions-that-are-yours) | approve its own work, lift the freeze, mark the log as read, or end a story part-way | every one of them, with a slash command or in your terminal |
| [The tests](#the-tests) | change a frozen test, add one after the freeze, or write tests as the implementer | lifting the freeze when a test is wrong |
| [Each agent's job](#each-agents-job) | do another agent's work, or record another's review | doing any of the work yourself, once the story is put down |
| [The loop's records](#the-loops-records) | edit a gate record, a review, a decision, a gate's document or the story's measurements by hand, or choose the environment your commands run in when their result is recorded | reading them |
| [Your configuration and instructions](#your-configuration-and-instructions) | edit `.sdlc/config.json`, `CLAUDE.md`, the backlog, the review policy or your Claude Code configuration | editing them by hand, between stories |
| [Commits, branches and checkouts](#commits-branches-and-checkouts) | commit before the gates have passed, push, fetch, move a branch, or add and remove worktrees | pushing, and anything else you do to a branch |
| [The rules themselves](#keeping-the-rules-on) | turn enforcement off, or start a second session nothing holds | turning it off when you start a session |

Nothing else about a session is touched. Your tests, your build and your own
tooling run exactly as they did before.

The hook is asked only about what an agent does. It is never asked about what
you type in your own terminal, so everything in the last column stays open to
you while a story runs. Where the decision is yours, the rule's entry says how
you make it.

Beside the rules that protect something, a few refuse a command only because
the hook [could not check it](#when-a-command-cannot-be-checked), and one check
has no rule name at all: a session that
[stops in the middle of a story](#stopping-mid-story) is sent back to work.

### When any of it applies

The hook does not watch every session. It holds one session to one story, and
nothing is enforced unless all of these are true:

1. The project is set up for the loop: it has a `.sdlc/config.json`.
2. A story is being worked on.
3. The call comes from the session working that story, or from one of its
   agents. That is the session where `/sdlc:next` was typed. A story started
   with `sdlc start` in a terminal is held by no session until somebody types
   `/sdlc:next`.
4. `SDLC_ENFORCE` was not `0` when the session started.

`sdlc status` and `sdlc doctor` both say when a story is under way and no
session is working it.

Every other session in the repository is left alone. The plugin is silent
there: it refuses nothing but
[a person's decisions](#a-persons-decisions-refused-in-every-session), sends no
session back to work and runs no formatter. Left alone means every rule, so
another session can edit code and tests, and commit. Work it leaves
uncommitted in the story's checkout goes into the story's commit, and a commit
it makes there is caught at the story's next gate, which
[refuses](troubleshooting.md#sdlc-e0046) and hands the story to you. Its `sdlc`
commands cannot write into the story either: each one that records something
[refuses](troubleshooting.md#sdlc-e0048) from a Claude Code session other than
the one working the story.

The rules follow the session, not the directory. A session held to a story is
held to the same rules in every checkout of the repository: your main checkout,
the story's own worktree, and any other story's.

### A person's decisions, refused in every session

A few commands are a person's decision. An agent that could run one would be
approving its own work, lifting the freeze on its own tests, or choosing which
stories nobody reads. So the hook refuses them from an agent's tool call in
every session of a project set up for the loop, whether or not that session is
working a story:

| Command | Refused | Rule | You type |
| --- | --- | --- | --- |
| `sdlc approve` | while a story is under way where the call runs, or one is waiting for your answer | [`approval-is-a-human-decision`](#approval-is-a-human-decision) | `/sdlc:approve` |
| `sdlc unfreeze` | while a story is under way where the call runs, or one is waiting for your answer | [`unfreeze-is-a-human-decision`](#unfreeze-is-a-human-decision) | `/sdlc:unfreeze` |
| `sdlc ack` | always | [`acknowledgement-is-a-human-decision`](#acknowledgement-is-a-human-decision) | `/sdlc:ack` |
| `sdlc remap` | always | [`remap-is-a-human-decision`](#remap-is-a-human-decision) | `/sdlc:remap` |
| `sdlc release` | always | [`release-is-a-human-decision`](#release-is-a-human-decision) | `/sdlc:release` |
| `sdlc staffing assign` | always | [`assignment-is-a-human-decision`](#assignment-is-a-human-decision) | `/sdlc:assign` |

You make each one by typing its slash command, in Claude Code or in Claude
Desktop, or by running the command in your own terminal, where the hook is not
asked.
[Running the loop from Claude Desktop](claude-desktop.md#the-commands-that-are-yours)
has the table. A seventh decision, ending a story before its gates have
passed, is refused only in the session working that story, under
[`stop-is-a-human-decision`](#stop-is-a-human-decision).

The hook runs a slash command only where it can tell that Claude Code ran the
hook itself, for a session a person attends. It tells by four environment
variables that Claude Code sets one way for your session and another for the
tools an agent runs: `AI_AGENT`, `CLAUDE_CODE_EXECPATH`, `GIT_EDITOR` and
`CLAUDE_CODE_SESSION_ATTENDED`. Where one of them says otherwise, as in a
session an agent started, the command answers that it did not run and names
the variable.
[A slash command did not run](troubleshooting.md#a-slash-command-did-not-run)
says what to do then. In Claude Desktop a `!` line, such as
`!sdlc approve AUTH-3`, is the other way in.

Asking a command for its help decides nothing and is not refused:
`sdlc approve --help` goes through. The exception is `sdlc remap`, whose help
is `sdlc help remap`.

The hook is the first line, and the commands themselves are the second. Each
of the six [refuses to run](troubleshooting.md#sdlc-e0079) in any process that
Claude Code started, however the command was reached, and it does so with
enforcement turned off too.

In the same sessions the hook also refuses what would let an agent reach one
of these decisions by another route:

- writing your Claude Code configuration, or your `~/.agents`
  ([`claude-config-is-the-persons`](#claude-config-is-the-persons),
  [`agents-config-is-the-persons`](#agents-config-is-the-persons));
- starting a Claude Code session that could type one of your slash commands,
  or one in a terminal of its own, which would look attended
  ([`story-stays-in-its-session`](#story-stays-in-its-session));
- running the hook by hand as if you had typed one, or as if Claude Code had
  reported something
  ([`slash-command-is-a-human-decision`](#slash-command-is-a-human-decision),
  [`recorded-event-comes-from-the-host`](#recorded-event-comes-from-the-host));
- changing one of the variables that tell an agent's tools from your session
  ([`enforcement-stays-on`](#enforcement-stays-on));
- a command too deeply nested for the hook to read, or one it could not read
  in time, since any of the above could be inside it
  ([`command-too-deep-to-read`](#command-too-deep-to-read),
  [`not-checked-in-time`](#not-checked-in-time)).

### How a command is read

The hook does not run your shell. It reads each command an agent is about to
run, in the `Bash`, `PowerShell` and `Monitor` tools, for what the command
would do to the files and commands the rules protect, however it is spelled.
Each of these is refused as a write to the loop's records, under
[`loop-state-through-the-tool`](#loop-state-through-the-tool):

```text
rm -rf .sdlc/state
sh -c 'cd .sdlc && rm -rf state'
D=.sdlc/state; rm -rf "$D"
find . -name tests.lock -delete
python3 -c 'import shutil; shutil.rmtree(".sdlc/state")'
```

Where the hook cannot tell what a command would do to a protected file, it
refuses, and the refusal says how to write the command so that it can. Such a
refusal does not mean the command was harmful.

Reading is left alone: `cat`, `grep`, `head` and the file tools read anything.
A command with `sed`, `awk`, `perl` or another interpreter in it is held more
tightly near a protected file, because each of those can write through a
program of its own. What the reading misses is listed under
[Not held](#not-held).

> [!NOTE]
> If `sdlc doctor` sent you here, it asked the same reader about a command in
> your `.sdlc/config.json`. It judges without a story or an agent, so it can
> name a rule for a command the hook allows mid-story, as it does for one that
> commits. Edit the command by hand so that it no longer does what the rule
> refuses.

### A path outside the repository

Outside the repository means outside the checkout the session is held in and
every other checkout of the same repository. The file tools refuse a path
there, under [`write-outside-repository`](#write-outside-repository). A shell
command is different: a path the hook can place outside the repository is not
governed by the shell rules, whatever its name. A copy of a frozen test in a
scratch directory is not a frozen test.

The hook places a path outside only when it is sure, and that takes a plain
command: one made of the commands below and no others, alone or joined by
`&&`, with every path written out in full.

| Command | Plain as |
| --- | --- |
| `rm` | `rm FILE...` or `rm -f FILE...` |
| `cp` | `cp FILE... TO`, `cp -p FILE... TO` or `cp -t DIRECTORY FILE...` |
| `mv` | `mv FILE... TO`, as the whole command |
| `touch`, `tee` | `touch FILE...`, `tee FILE...` |
| `sed` | `sed -i s/x/y/ FILE...`, with one substitution |
| `echo`, `printf` | any words |
| `find` | `find DIRECTORY... -name GLOB -delete`, or with `-path` for `-name` |
| `cd` | `cd DIRECTORY` |

Output may go to a file with `>` or `>>`. A plain command holds nothing the
shell would fill in or act on: no variable, `$( )`, `~`, glob in a directory,
brace list, `..`, pipe, `;` or backslash, and no space inside quotes. A
`PowerShell` command is never plain.

A word the hook cannot place is judged by its name, as that file in the
repository: `rm "$S/internal/x_test.go"` is refused as the test
`internal/x_test.go` wherever `$S` leads, because the hook does not run the
shell. The way on is to write the path out in full, in a plain command.

> [!NOTE]
> A path through a link is not placed. On macOS `/tmp` and `/var` are links, so
> write `/private/tmp/...` and `/private/var/...`.

## Decisions that are yours

These rules keep the loop's decisions with a person.
[A person's decisions, refused in every session](#a-persons-decisions-refused-in-every-session)
says where each one holds and how you make it yourself.

### `approval-is-a-human-decision`

Refuses `sdlc approve` from an agent's tool call. A reviewer's `approve`
verdict, recorded with `sdlc review add`, is a different command and is not
refused.

An approval is your answer to a question the loop stopped to ask you. An agent
that could give it would be approving its own work.

*Instead:* the agent hands the question over with `sdlc escalate` and stops.
You read the work and type `/sdlc:approve`, or run `sdlc approve` in your own
terminal. [Human approval gates](guides/human-approval-gates.md) shows how.

### `unfreeze-is-a-human-decision`

Refuses `sdlc unfreeze` from an agent's tool call. A command that only
mentions the word, as in `--note "no need to unfreeze"`, is not refused.

Lifting the freeze is sometimes right. It is also exactly the move an agent
would make to get a failing test to pass.

*Instead:* the agent says which frozen test is wrong and which acceptance
criterion it gets wrong, and stops. You lift the freeze with `/sdlc:unfreeze`,
or `sdlc unfreeze --reason "..."` in your own terminal.
[When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md) walks
through it.

### `acknowledgement-is-a-human-decision`

Refuses `sdlc ack` from an agent's tool call, in every session, whether or not
a story is being worked on. `sdlc log` itself is not refused.

`sdlc log` lists what has landed on trunk since the commit you last
acknowledged. An agent that could move that mark would be deciding which
stories nobody needs to look at, its own among them.

*Instead:* the agent says what `sdlc log` lists, and stops. You read it and
type `/sdlc:ack`, or run `sdlc ack --through <commit>` in your own terminal.
[Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md) is
the guide.

### `remap-is-a-human-decision`

Refuses `sdlc remap` from an agent's tool call, in every session, whatever
follows the word. Its `--help` is refused with it; `sdlc help remap` prints the
same text and is not.

`sdlc remap` puts a landed story's change to `CODEMAP.md` on trunk, over what
trunk holds, or throws it away
([the loop](the-loop.md#parallel-stories-and-codemapmd)). Either is a choice
between two texts that no reviewer read, and an agent that made it would be
deciding what trunk's map says.

*Instead:* the agent says what `sdlc stop` or `sdlc status` printed, and
stops. You type `/sdlc:remap <ID>`, or run `sdlc remap <ID>` in your own
terminal.

### `release-is-a-human-decision`

Refuses `sdlc release` from an agent's tool call, in every session.

Releasing a story takes it from the checkout that holds it, after which any
checkout can start it. An agent that could do that could take a story another
session is working, or let go of the one it is held to and of every rule with
it.

*Instead:* the agent says which checkout holds the story, and stops. If the
story is to move, you type `/sdlc:release <ID> --reason "..."`, or run
`sdlc release <ID> --reason "..."` in your own terminal.
[Stopping and resuming a story](guides/stopping-and-resuming-a-story.md#move-a-story-to-another-checkout)
covers the move.

### `assignment-is-a-human-decision`

Refuses `sdlc staffing assign` from an agent's tool call, in every session. A
run of `sdlc` whose subcommand the hook cannot read, such as one built from a
variable, is refused under this rule too, because it could be this command.

What `sdlc staffing assign` records says that a person chose a model or an
effort for an agent, and the project manager cannot change it afterwards. A
session that could run it could pin every agent where it liked, in your name.

*Instead:* the agent says what it would have asked for, and stops. You type
`/sdlc:assign`, or run `sdlc staffing assign` in your own terminal, outside
Claude Code.

### `stop-is-a-human-decision`

Refuses `sdlc stop` from an agent's tool call while the story still has a gate
to pass. Ending a story whose gates have all passed is the loop's last step,
and is not refused.

Nearly every rule on this page holds only while a story is being worked on, so
ending one part-way would turn them all off at once. A commit no gate passed
could go through straight after.

*Instead:* the agent works the next gate, or hands the story to you with
`sdlc escalate` and stops. You end a story part-way with `sdlc stop` in your
own terminal, or `!sdlc stop` at the prompt in Claude Desktop.
[Stopping and resuming a story](guides/stopping-and-resuming-a-story.md#put-a-story-down)
says what that keeps.

### `slash-command-is-a-human-decision`

Refuses running the `sdlc` hook by hand, from an agent's tool call, on
`UserPromptExpansion`, the event Claude Code raises when you type a slash
command. It refuses the same on an event it cannot read until the command
runs, such as one given as a variable. It holds in every session.

When you type `/sdlc:approve`, the hook runs the command for you, as your
decision. An agent that ran the hook on a message of its own making would be
approving its own work in your name. The hook's check of its own environment
answers such a run that the command did not run, and this rule refuses it
before it starts.

*Instead:* the agent says what it wanted decided, and stops, and you type the
slash command. To see what the hook answers to a call, it runs
[`sdlc hook PreToolUse`](commands.md#sdlc-hook-event), which is not refused.

## The tests

The tests are written from your acceptance criteria and frozen before any code
exists ([Gate 3](the-loop.md#gate-3-freeze-the-tests)). These rules keep them
as they were frozen.

### `frozen-test-is-not-edited`

Refuses any edit of a frozen test with the file tools, from every agent.

A test is frozen by its content when the test gate passes. An agent that can
edit its own tests will, sooner or later, edit them until they pass, and every
gate after that one means nothing.

*Instead:* the agent changes the code until the test passes. If the test
itself is wrong, it says which acceptance criterion the test gets wrong, and
stops. Only you lift the freeze:
[When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md) is the way
through.

### `frozen-test-through-the-tool`

Refuses a shell command that would write, move or delete a frozen test: a
redirect, `rm`, `mv`, `cp`, `tee`, `sed -i`, `find -delete`, `git checkout`, or
code handed to an interpreter. Before the freeze it does nothing.

Reading a frozen test is how the implementer learns what to build, and `cat`,
`grep`, `head` and the file tools are never refused. A command with `sed`,
`awk`, `perl` or another interpreter in it is let through only where it names
the test plainly as the file to read: `sed -n 1,5p internal/x_test.go` is
allowed, and `cat internal/x_test.go | sed -n 1,5p` is refused.

A copy of a frozen test outside the repository is not a frozen test:
[A path outside the repository](#a-path-outside-the-repository) says when the
hook can tell. A patch applied with `git apply` or `patch` names its files
where this rule cannot see them, and `sdlc gate` still
[refuses](troubleshooting.md#sdlc-e0025) once a frozen test has changed.

*Instead:* the agent leaves the test alone. If it has to change, the agent
says which test and why, and you lift the freeze. For a copy outside the
repository, it writes the path out in full in a plain command.

### `no-new-test-after-the-freeze`

Refuses a new test file once the tests are frozen, from the file tools and
from a shell command that would create one, unless
[`freeze.allow_new_test_files`](configuration.md#freezeallow_new_test_files)
is on. New means any test file the freeze does not hold, however long it has
been there. Removing one is not refused. Nor is a path the hook places
outside the repository, which is no test of this project's.

A test added after the code exists can be written to pass. That is the freeze
with extra steps.

*Instead:* the agent says which case is missing, and stops. You may lift the
freeze so that the test author adds the case and freezes again. With the
setting on, the test author adds the file and runs `sdlc freeze`, which waits
for the test reviewer, where the story has one, to approve the tests with the
new file among them. Until then the later gates
[refuse](troubleshooting.md#sdlc-e0043).

### `implementer-does-not-write-tests`

Refuses the implementer writing any test file, frozen or not, before the
freeze or after it, through the file tools or the shell. Reading one is not
refused. Through the shell, a path the hook places outside the repository is
no test of this project's, so a scratch copy may be changed with a plain
command.

The agent that writes the code does not write the tests it is judged by. That
separation is what the loop is made of, and it is not a setting:
`freeze.allow_new_test_files` lets the test author add a file, never the
implementer.

*Instead:* the implementer makes the existing tests pass. If they are wrong,
it says so and stops.

After the freeze the two rules above answer first, so this is the name you see
before the freeze, or for a new file the setting lets past.

## Each agent's job

Each gate is done by an [agent](the-agents.md) that never saw the others'
reasoning. These rules keep each one to its own part.

All but the last three govern the file tools: `Write`, `Edit`, `MultiEdit` and
`NotebookEdit`. A shell command is held to the rules that protect the tests,
the loop's records, your configuration and the commit, and not to an agent's
role.

### `orchestrator-delegates`

Refuses the main conversation, the one you type into, writing any file outside
`.sdlc/` and `CODEMAP.md` while a story is being worked on: code, tests and
documentation alike. It refuses the same of a subagent that is not one of the
loop's own, such as Claude Code's general-purpose agent or one of yours.

The conversation doing the work itself is the failure the loop exists to
prevent, and it can be reached without breaking any other rule. Handing the
work to an agent the loop does not know is the same failure one step removed.

*Instead:* the session delegates the work to the agent whose gate it is. A
file that is not the story's work, such as a note for you or a commit message,
goes under `.sdlc/stories/<ID>/` in the checkout the session works in. It is
removed before the commit gate is recorded, because a file left there is
committed with the story unless your project ignores that directory. If you
asked for a quick change mid-story, let the story's own agents make it, or
[put the story down](guides/stopping-and-resuming-a-story.md#put-a-story-down)
first.

### `sdet-writes-tests-only`

Refuses the test author writing anything but tests, the story's own directory
and `CODEMAP.md`.

A test written beside its implementation tests the implementation, not the
criterion.

*Instead:* the test author writes the tests, and the code comes next, from the
implementer. If your project keeps fixtures somewhere the loop does not
recognise as tests, the agent names the directory and stops, and you add it to
[`paths.tests`](configuration.md#pathstests) between stories.

### `researcher-writes-analysis-only`

Refuses the researcher writing outside the story's own directory and
`CODEMAP.md`.

The researcher says what the story needs before anybody writes a test or a
line of code. It does not go on to write either.

*Instead:* it keeps to the story's directory and `CODEMAP.md`, and stores its
documents with `sdlc artifact write`.

### `reviewer-reviews`

Refuses any of the eight reviewing agents (architect, security, red-team,
perf, human-advocate, verifier, code-reviewer and test-reviewer) writing
outside the story's own directory.

A reviewer that changes the work is reviewing its own. None of the eight is
given a file-writing tool, so this rule is a backstop: it is what still holds
if an agent's tool list grows.

*Instead:* the reviewer says what is wrong and records its verdict with
`sdlc review add`. Somebody else makes the change.

### `bookkeeper-writes-the-retro-and-the-map`

Refuses the bookkeeper writing anything but the story's own directory and
`CODEMAP.md`.

The retro records what happened. An agent that could change the code while
writing up the lessons could write up lessons it had just made untrue.

*Instead:* it stores the retro with `sdlc artifact write retro`. `CODEMAP.md`
is the only other thing its gate produces.

### `review-is-recorded-by-its-reviewer`

Refuses `sdlc review add` run by anyone but the reviewer it names: the main
conversation, an agent outside the loop, another reviewer, or the implementer
recording an approval of its own work. A review you record in your own
terminal is not asked about.

A gate passes on its reviewers' verdicts, so a review recorded by somebody
else is a review nobody did.

*Instead:* the session delegates to the reviewer, which reads the work and
records its own verdict. Where a reviewer runs as several
[instances](configuration.md#an-agents-entry), each records under its own
name, such as `security-2`. A reviewer's
[arbiter](the-loop.md#the-arbiter) is a fresh agent of the reviewer's kind,
and records as `<role>-arbiter`.

### `staffing-is-recorded-by-the-project-manager`

Refuses `sdlc staffing set` and `sdlc staffing amend` in the session working a
story, from anyone but the `sdlc:project-manager` agent.

The staffing says which agents take part in a story and what model and effort
each is asked for. The session running the loop is the one a smaller staffing
saves work for, and the agents are the ones it leaves out, so none of them
writes it.

*Instead:* the session delegates to `sdlc:project-manager`, with the story and
what happened.

### `project-manager-only-staffs`

Refuses the `sdlc:project-manager` agent every `sdlc` command but the ones it
is for: `sdlc staffing show`, `sdlc staffing set`, `sdlc staffing amend`, the
commands that only read (`sdlc status`, `sdlc review list`, `sdlc story list`,
`sdlc artifact list`, `sdlc log`, `sdlc cost`, `sdlc doctor` and
`sdlc version`), and any command asked for its help.

The project manager decides who takes part. An agent that could also record a
gate or a review, or hand the story to you, would be deciding the story.

*Instead:* it records or amends the staffing, and says anything else in its
result. Reporting an agent's failed run with
[`sdlc team failure`](commands.md#sdlc-team-failure-agent) is for the session
running the story, the project manager's own failed run included.

## The loop's records

The record of a story is what every later gate relies on, and what you approve
from. `sdlc` writes it, and nobody edits it. These rules cover every story's
records, not only the one being worked on: a finished story's record is the
evidence that it finished.

### `loop-state-through-the-tool`

Refuses a shell command that would write or delete the loop's own files:
`.sdlc/config.json`, `.sdlc/state`, any story's gate record, `DECISIONS.md`,
reviews and gate documents, and `.sdlc` or a story's directory taken whole. How
the command does it makes no difference: a redirect, `rm`, `mv`, `cp`, `tee`,
`sed -i`, `find -delete`, a hard link, or code handed to an interpreter.

It also refuses two things that would take those files along with them:

- removing or moving a checkout of this repository, or a directory that holds
  one, such as `rm -rf ..` from a story's worktree;
- throwing the working tree away wholesale, with `git stash`,
  `git reset --hard`, `git checkout -- .` or `git clean`.

A scratch file inside a story's directory is not loop state, and reading the
loop's files with `cat`, `grep` or `head` is never refused. `sed`, `awk`,
`perl` and other interpreters are refused on them whatever they are asked to
do.

A `find` that deletes is read with its operators. `!` and `-not` are taken to
leave every file in, and each side of `-o` or `-or` is read on its own, so
`find . ! -name '*.go' -delete` is refused for the loop's files it would
reach, and the refusal names them.

Without this rule, a shell command would be the one way round every rule that
protects the record.

*Instead:* the agent uses `sdlc artifact write` for a gate's document,
`sdlc review add` for a review, `sdlc gate` for an outcome and `sdlc decide`
for a decision. To clear up, it removes only what the story made, by name, as
in `rm -rf build` or `find build -type d -empty -delete`, and runs one `find`
for each name it filters on. `.sdlc/config.json` has no command: you change it
by hand, between stories. Removing a checkout is yours too.

### `gate-record-is-written-by-the-tool`

Refuses any edit of a story's `gate-record.json` with the file tools.

The gate record is the loop's memory of which gates passed, and every later
gate reads it. A record an agent can edit is not a record.

*Instead:* the agent records an outcome with
[`sdlc gate <name> pass|fail --note "..."`](commands.md#sdlc-gate-gate-status).

### `review-is-written-by-the-tool`

Refuses any write under a story's `reviews/` directory with the file tools.

`sdlc` stamps each review with what the reviewer was looking at, so that a
later change makes the approval
[stale](the-loop.md#why-approvals-go-stale) instead of letting it stand. A
review written by hand carries no such stamp.

*Instead:* the reviewer runs
[`sdlc review add`](commands.md#sdlc-review-add-gate-role-verdict) with its
gate, role and verdict, its counts, and one `--finding` for each finding it
counts.

### `gate-artifact-is-written-by-the-tool`

Refuses any in-place write of a gate's document: `ANALYSIS.md`, `THREATS.md`,
`TEST-PLAN.md`, `PLAN.md`, `VERIFICATION.md` or `RETRO.md`. It refuses
everyone, the agent whose gate it is included.

A gate's document is part of the record. Were it an ordinary file, the main
conversation could write the analysis itself and then record a pass on its own
work.

*Instead:* the agent whose gate it is runs `sdlc artifact write <name>` and
gives it the document.

### `trace-is-written-by-the-tool`

Refuses any write to a story's `trace.jsonl`, by a file tool or a shell
command, from every agent and the main conversation alike. Reading it is never
refused.

[The trace](the-loop.md#the-trace) is the loop's measurements of the story:
what each agent's run took, and what was refused. A measurement an agent can
write is not a measurement.

*Instead:* there is nothing to record by hand. Read it with
[`sdlc trace <ID>`](commands.md#sdlc-trace).

### `decisions-are-written-by-the-tool`

Refuses any write to a story's `DECISIONS.md` with the file tools, from every
agent and the main conversation alike. Reading it is never refused, and a
`DECISIONS.md` outside a story's directory is an ordinary file.

The file lists the
[implementation decisions](the-loop.md#two-kinds-of-decision) the loop made on
the story without stopping for you, and it is where you read them afterwards.
`sdlc` writes it again from the story's record each time a decision is
recorded. A decision an agent can rewrite is not a record of one.

*Instead:* the agent records a decision with
[`sdlc decide`](commands.md#sdlc-decide-id), giving the question, each option
it weighed, the one chosen, the reason, and whose recommendation it was. A
shell command that writes the file is refused under
[`loop-state-through-the-tool`](#loop-state-through-the-tool).

### `recorded-event-comes-from-the-host`

Refuses running the `sdlc` hook by hand, from an agent's tool call, on one of
the eight events it records: `PostToolUse`, `PostToolUseFailure`,
`SubagentStart`, `SubagentStop`, `Stop`, `Notification`, `StopFailure` and
`PostCompact`. It holds in every session.

On those events the hook writes down each agent's run and the story's
measurements as Claude Code reported them. A report an agent wrote is not
Claude Code's, and the story's figures would be whatever the agent chose.

*Instead:* the agent leaves the recording to Claude Code. To see what the hook
answers, it runs [`sdlc hook PreToolUse`](commands.md#sdlc-hook-event) with a
`session_id` of its own, because a refusal that carries the session's own id
is counted in the story's trace.

### `commands-run-as-configured`

Refuses `sdlc gate implementation pass` and `sdlc freeze` where the same
command line changes the environment `sdlc` starts with, or runs the call
behind another program that could. In `PowerShell`, a line that may run either
call has to be the call alone.

Both calls run [your configured commands](configuration.md#commands) and
record the result, and the verifier and the landing reuse that result instead
of running the commands again. The commands inherit the environment `sdlc`
starts with. A setting in front of the call, such as `GOFLAGS='-run=^$'` or a
`PATH` that finds another `go`, would record a pass of a run that tested
nothing. So the rule refuses any change, whatever variable it names.

The refusal says which of these it met:

| The line holds | Write it as |
| --- | --- |
| A setting, an `export`, `env`, a second command that changes the environment, or a wrapper such as `sudo` or `timeout`, in front of the call or earlier on the line | The call with nothing in front of it. A `cd` before it is fine, and so are `time`, `nice` and `nohup`. |
| In `Bash`, a here-document whose delimiter is not quoted, with `$( )`, a backtick or `${ }` in its body | The delimiter quoted, as in `<<'EOF'`, so that the body is text. |
| In `PowerShell`, anything beside the call: a pipe, a comment, a variable or a double-quoted note | The call alone, with a plain or single-quoted `--note`, after at most one `Set-Location` or `cd` to an absolute path. |
| In `PowerShell`, text that names `sdlc` and the call's words, such as a commit message or a review's finding | The text in a file, handed over with `git commit -F <file>` or `--file <file>`. |
| In `PowerShell`, a typographic quote anywhere on the line, an editor's apostrophe in `it’s` among them | ASCII quotes, or the text in a file. |
| In `PowerShell`, `pwsh` or `powershell` given a variable, an expression or an encoded command | Literal words, as in `pwsh -File ./build.ps1`. |

*Instead:* the agent runs the call alone. A setting your commands need goes in
the command itself, in `.sdlc/config.json`, which you edit by hand, between
stories.

The rule narrows who chooses that environment, and does not settle it. A
result the verifier marks `run by sdlc` was run as configured from a line the
rule could read, which is not proof that nobody chose the environment. The
rule does not read a script the line runs, such as `bash ./x.sh` or
`make gate`. Nor does it read `sdlc` run under another name: a copy, a hard
link, or `go build -o` in a project that builds `sdlc`. Inline interpreter
code that builds the name gets past it, and so does a substitution that is the
whole call behind another program, as in `sudo $(printf 'sd%s freeze' lc)`.
The verifier's check for tests that pass without the behaviour stays the net,
and [Not held](#not-held) lists the rest.

## Your configuration and instructions

The contract in `CLAUDE.md`, your settings, the backlog and the review policy
are what a story is judged against. A rule the agent can edit is a rule that
has stopped applying to it. Everything here is yours to change by hand,
between stories.

### `write-protected-path`

Refuses a file-tool write to `.git`, `.sdlc/config.json` and `.sdlc/state` at
the root of the repository, and to `CLAUDE.md`, `CLAUDE.local.md`,
`AGENTS.md`, `.claude` and `.agents` at any depth. Names are matched without
regard to case, so `docs/agents.md` is held as `AGENTS.md` is, and the refusal
says so.

Configuration is yours, and the loop's state is `sdlc`'s. An agent editing
either can make the record say something that did not happen. The instruction
files are held at any depth because Claude Code and other agent tools read
them wherever they sit in the project. A contract rewritten to get past a gate
takes every later gate with it.

*Instead:* you change configuration by hand, between stories. If a rule in
your contract is wrong, the agent stops and says so.

### `protected-path-through-the-tool`

Refuses a shell command that would write or delete `CLAUDE.md`,
`CLAUDE.local.md`, `AGENTS.md`, anything under `.claude`, `.agents` or `.git`,
the backlog, or the review policy. It is the shell's side of the rule above
and the two below. An editor such as `vim` or `ed` opened on one of them is
refused as well, because it can rewrite the file with nobody at the keyboard;
`cat` reads it.

The shell matches these names more widely than the file tools do: without
regard to case, and in any directory. While a story runs, a command that
writes `docs/review.md` is refused where your policy is `REVIEW.md`.

*Instead:* you change these files by hand, between stories. If what one says
is wrong, the agent stops and says what.

### `backlog-is-not-edited`

Refuses a file-tool write to the backlog, wherever
[`backlog.path`](configuration.md#backlogpath) puts it. `sdlc` itself still
records each story's status there as the loop moves it.

The backlog holds the acceptance criteria the tests are held to, and each
story's risk tier, which decides, with the paths your settings list, whether
you approve the commit. An agent that lowered a story's tier mid-story would
take that pause away.

*Instead:* if a story's criteria or risk tier are wrong, the agent says which,
and stops. You change the backlog by hand;
[Writing stories](guides/writing-stories.md) says when it is safe to.

### `review-policy-is-not-edited`

Refuses a file-tool write to the review policy, wherever
[`reviews.policy`](configuration.md#reviewspolicy) puts it, from every agent.

Every reviewer reads [the policy](the-loop.md#the-review-policy) first, and it
tells them what to report. An agent that could edit it could edit what its own
reviewers obey.

*Instead:* if the policy is wrong, the agent says what, and stops. You change
it on trunk, between stories. A running story keeps the copy it started with,
so to apply a change to it you end the story, commit the change on trunk and
start again. A change made in the story's worktree does not reach trunk:
[the landing refuses it](troubleshooting.md#sdlc-e0055).

### `claude-config-is-the-persons`

Refuses writing your Claude Code configuration, with a file tool or a shell
command, in every session of a project set up for the loop. That is `~/.claude`
and everything under it, `~/.claude.json`, and the directories
`CLAUDE_CONFIG_DIR` and `CLAUDE_CODE_PLUGIN_CACHE_DIR` name. Claude Code's own
commands that change it are refused too, such as `claude config set`,
`claude mcp add` and `claude plugin install`.

That configuration decides the hooks, plugins, skills, agents and permissions
every session runs under, this plugin among them. A session that could write
it could take the hook away, or give itself the permissions you withhold.

*Instead:* the agent says what it would change there and why, and stops. You
change your own configuration, in Claude Code's settings or by hand.

The rule holds only in a project set up for the loop. A session in any other
project is held to nothing, so it can write all of this, and every project on
the machine then runs what it wrote. That limit is accepted, and
[SECURITY.md](../SECURITY.md) says more about it.

Claude Code's commands are read by the word `claude`. One run through the path
in `CLAUDE_CODE_EXECPATH`, as in `"$CLAUDE_CODE_EXECPATH" mcp add`, is refused
only in the session working a story, under
[`story-stays-in-its-session`](#story-stays-in-its-session).

### `agents-config-is-the-persons`

Refuses writing your `~/.agents`, and everything under it, in every session of
a project set up for the loop. A command that fills or empties your whole home
directory, such as `rm -rf ~`, is refused under this rule too.

`~/.agents` is the folder Codex, Cursor, Copilot, Gemini CLI and other agent
tools load skills from. It has nothing to do with `sdlc`'s own agents. A skill
written there runs in your next session with one of those tools, outside every
rule on this page.

*Instead:* the agent says what it would change there and why, and stops. You
add or change a skill in your own `~/.agents`, by hand.

### `instructions-above-the-project`

Refuses a write, while a story is being worked on, to `CLAUDE.md`,
`CLAUDE.local.md` or `AGENTS.md` in a directory above the project, or in that
directory's `.claude`. The project is the checkout the session works in, so a
story's worktree has your main checkout above it. A copy or an extraction that
fills such a directory is refused as well, as with `cp -r src/. ..` or a
`tar -x` there, because it may put one of those files in it.

Claude Code loads those files from every directory above the one it starts in.
What a session writes there is read as instructions by every later session
started below it, in this project and in others.

*Instead:* the agent says what it would change there and why, and stops, or
writes the files inside the project. You change the instruction files above
the project by hand.

### `write-outside-repository`

Refuses a file-tool write that lands outside the repository, links followed.
It also refuses a write into another checkout of the repository nested inside
this one, such as a story's worktree written from a session held in your main
checkout.

The loop vouches only for what it can review, and a file elsewhere is in no
story's diff. A file in another checkout is that checkout's story's work.

*Instead:* the agent works inside the repository, in the checkout its session
holds, and asks you to create a file it needs elsewhere. A shell command may
still write a scratch file outside the repository, as
[A path outside the repository](#a-path-outside-the-repository) sets out.

## Commits, branches and checkouts

The commit is where a story's work becomes history, and the landing is where
it reaches trunk. These rules keep both for work that has been through the
gates.

### `commit-gate`

Refuses `git commit`, and the git commands that make a commit of their own
(`merge`, `cherry-pick`, `revert`, `am`, `rebase` and `commit-tree`), until
the story is ready to commit. Three things have to be true, and the refusal
names the first that is not:

1. Every gate before the commit has passed.
2. The work has not changed since the verifier and the code reviewers approved
   it.
3. For a story that waits for you, you have approved that same work with
   `sdlc approve`. A story waits when its risk tier is in
   [`human_gates.pre_commit_pause_tiers`](configuration.md#human_gates), or
   when its change touches a path that `human_gates.pre_commit_pause_paths`
   matches. For a path, the refusal names each file with the glob it matched.

This is the rule that makes the gates count. Nothing reaches your history
until the tests, the plan and the diff have each been checked, and what is
committed is what was checked.

The story's own copy of your settings cannot take the pause away: the lists in
your main checkout count beside it.

*Instead:* the agent finishes the gates, and `sdlc status` shows which is
next. Committing without them is your decision: you end the story with
`sdlc stop` in your own terminal first.

Where a story waits for you and the hook cannot measure the working tree in
time, it refuses the commit and says so, because no approval can be matched to
work nobody measured. If you have approved the work, make that commit in your
own terminal. `sdlc gate commit pass` then checks it against your approval.

A commit made in a way the hook cannot read is caught one step later: the next
gate [refuses](troubleshooting.md#sdlc-e0046) once the branch has moved under
the story. A repository nested inside your project, such as vendored code, is
treated as part of the checkout it sits in, so a commit there is held to the
same gate.

### `commit-in-another-checkout`

Refuses a commit in another checkout of the repository, however git is pointed
there: with `git -C`, a `cd`, `--git-dir` or `--work-tree`. It refuses whatever
the story's gates say.

The story's gates cover only the checkout it is held in. What they reviewed is
not what would be committed in another.

*Instead:* the agent commits from the checkout the story is held in.

### `branch-through-the-tool`

Refuses a command that moves trunk or a story branch other than by the story's
own gated commit:

- `git push`, `git fetch`, `git pull` and `git remote update`, whatever they
  name;
- forcing, deleting, renaming or copying a branch with `git branch`, and
  resetting one with `git checkout -B` or `git switch -C`;
- `git update-ref`, and the other commands that write refs directly;
- `git reset` to another commit, in another checkout, or anywhere once the
  story is committed;
- a rebase, once the gates before the commit have passed.

`sdlc stop` lands a finished story on trunk itself, after checking that it is
the reviewed change and nothing else. A branch moved any other way can put
work on trunk that no gate passed. Refusing every push and fetch is a choice:
the loop never pushes, and the hook cannot always tell another repository from
one of this repository's own checkouts.

*Instead:* the agent leaves branches to the loop, and reads them with
`git log`, `git show` or `git diff`. You push, fetch, move or delete a branch
in your own terminal.

A git command the hook can place in another repository altogether, such as a
test fixture in a temporary directory, is not refused.

### `worktree-through-the-tool`

Refuses every `git worktree` action but `list`: `add`, `remove`, `move`,
`prune`, `lock`, `unlock` and `repair`.

Each story is held in the checkout git lists for it. Moving or removing a
worktree ends the hold on the story in it, pruning forgets one, and adding one
can put a checkout where a story's worktree is about to go.

*Instead:* `sdlc start` makes a story's worktree, and you
[remove one](the-loop.md#removing-a-worktree) in your own terminal. To read
another branch, the agent uses `git show <branch>:<path>` or
`git diff <branch>`.

### `worktree-setting-through-the-tool`

Refuses `git config` changing `core.worktree` or `core.bare`, in any scope,
and removing or renaming the `core` section. Reading them is not refused.

Either setting changes which directory a checkout works in. A change can move
your main checkout's work tree out from under it, and every story it holds
with it.

*Instead:* you change them in your own terminal. If git says this is not a
work tree, the agent stops and tells you.

## Keeping the rules on

### `enforcement-stays-on`

Refuses a command that sets `SDLC_ENFORCE`, `SDLC_BIN`, `CLAUDE_PROJECT_DIR`
or `CLAUDE_CODE_SESSION_ID`. In every session of a project set up for the
loop, it also refuses a command that removes, blanks or sets one of six
protected names, or that starts a program with an emptied environment, such as
`env -i`. The six are `AI_AGENT`, `CLAUDE_CODE_EXECPATH`, `GIT_EDITOR`,
`CLAUDE_CODE_SESSION_ATTENDED`, `CLAUDE_CODE_CHILD_SESSION` and
`CLAUDE_CODE_ENTRYPOINT`.

The switch that turns enforcement off belongs to whoever started the session,
and a switch an agent can reach is not a control. The other three names decide
which `sdlc` the hook runs, which project it looks at and which session holds
a story.

The six protected names are how the hook tells a slash command you typed from
one an agent made up. It reads the first four, and Claude Code works out
`CLAUDE_CODE_SESSION_ATTENDED` from the last two
([a person's decisions](#a-persons-decisions-refused-in-every-session)).
Changed by an agent, any of them could make a message it wrote, or a session
it started, pass for your typed `/sdlc:approve`.

The hook cannot always tell a change from a mention, so a command that names
one of the six where the shell could change it is refused even when it changes
nothing, as `declare -p AI_AGENT` is. Reading one with `echo "$AI_AGENT"` or
`printenv GIT_EDITOR` is not, and nor is a longer name that holds one, such as
`GIT_SEQUENCE_EDITOR`. The refusal names the variable the command changes, or
all six where it cannot tell which.

`GIT_EDITOR` is the one an agent meets most:
`GIT_EDITOR=true git rebase --continue` is refused. Claude Code already gives
an agent's shell `GIT_EDITOR=true`, so git needs no setting of its own.

*Instead:* if a rule is wrong, the agent says which one and why, and stops. It
runs a git command without a `GIT_EDITOR` in front. To drop some other
variable for one program, it uses `env -u NAME`, not `env -i`. You turn
enforcement off yourself, as [Turning it off](#turning-it-off) shows.

### `story-stays-in-its-session`

Refuses starting another Claude Code session from the session working the
story: `claude`, `@anthropic-ai/claude-code` through a runner such as `npx`,
or the path in `CLAUDE_CODE_EXECPATH` run as a program. Installing the package
is not refused.

In every session of a project set up for the loop, it also refuses starting a
Claude Code session that could type one of your slash commands:

- one whose prompt names `/sdlc:approve` or another of them, or whose prompt
  the hook cannot read;
- one given a terminal of its own: through `tmux`, `screen`, `expect`,
  `script` or Python's `pty` module, through `osascript` telling Terminal or
  iTerm to run `claude`, or through a `socat` address that runs it. The path
  in `CLAUDE_CODE_EXECPATH` counts as `claude` here, and so does a shell on
  such a terminal that is fed its commands on standard input;
- an `osascript` or a `socat` whose text the hook cannot read to the end,
  whatever it names.

A story holds only the session working it. One started from its shell is a
new session, and nothing here would hold what it did. And a slash command
typed by an agent in a session of its own is not yours. A session with a
terminal of its own looks to the hook like one you attend, so this rule is
all that stands in front of it.

Refusing what cannot be read costs some honest commands. An `osascript` has to
hand a terminal its command as one plain string in double quotes, and a
`socat` address may hold no variable, glob or bracket. So
`do script "cd /repo && make"` and `socat - TCP:[::1]:8080` are refused, and
the refusal names the character it stopped at.

*Instead:* the agent hands the work to this session's agents. It writes an
`osascript` or `socat` line as plain text, with one command to a call, or asks
you to run it. If you want another session, open one in your own terminal.

## When a command cannot be checked

These rules name no file and no decision. Each refuses a command the hook
could not read to the end, because a command it cannot read could hide
anything the other rules refuse. None of them says the command was harmful,
and each refusal says how to write the command so that the hook can read it.

### `not-checked-in-time`

Refuses a call the hook could not check in time: a command it had not finished
reading, or a change to a file it had not checked. The refusal says that the
hook ran out of time. No rule was broken, and none was applied.

Claude Code gives a hook 30 seconds and then kills it, and a killed hook lets
the call through unchecked. So the hook keeps a deadline of its own, 25
seconds, and a call it has not decided by then is refused. Without it, a
command long enough to be slow to read would get round every rule on this page
with nobody told.

It is given for any call from the session working a story, and for a command
from any session in a project set up for the loop. So a session that holds no
story can meet it too, for a command and never for a file.

*Instead:* the agent runs a long command again in shorter parts, one call for
each, and tries a change to a file again as it is. If a short command, a
commit or the same change is refused this way twice, the call is not what is
slow: the agent stops and tells you, and
[Troubleshooting](troubleshooting.md#warnings-the-hook-prints) says where to
look. [`SDLC_HOOK_DEADLINE_MS`](configuration.md#sdlc_hook_deadline_ms) can
shorten the deadline, and nothing lengthens it.

One limit is left, and it is the first listed under
[Commands the rules do not read](#commands-the-rules-do-not-read): a hook that
is killed before it knows whether the call is one it checks.

### `command-too-deep-to-read`

Refuses a command that nests `$( )` or backticks more than 32 deep, that hands
a shell or `eval` a string holding another such string more than four deep, or
that leaves more than 16 ways to read which programs it runs. It holds in
every session of a project set up for the loop.

Every level of a command is read for what it runs. Past those limits the hook
stops reading, and it refuses the command rather than let it run unread.
Nothing a person writes by hand comes near them.

*Instead:* the agent writes it as separate commands, in separate calls where
a line holds many, and runs the inner command on its own instead of inside
another `eval` or `bash -c`.

### `git-command-cannot-be-read`

Refuses a git command whose subcommand or alias the hook cannot read: one the
shell has yet to fill in, such as `git $CMD prune`, an alias the command
defines that the hook cannot follow, or configuration pulled in from a file
with an include or `GIT_CONFIG_GLOBAL`. A line whose program and next word are
both variables, such as `$EDITOR "$FILE"`, is refused as well, because it
could be git with any subcommand.

Several rules turn on which git command runs: a commit, a push, the removal of
a worktree. One the hook cannot read could be any of them.

*Instead:* the agent spells git's subcommand and any alias out in the command
itself, and names a program that is not git, such as `python3`, instead of a
variable.

### `archive-members-unknown`

Refuses extracting an archive into the project when the hook cannot read what
is in it: the archive does not exist yet, comes on standard input, is in a
format other than tar, tar.gz, tar.bz2 and zip, or is extracted with an option
that renames its members, such as `--strip-components`.

The hook reads an archive's member names and judges each where it would land,
so an archive holding a `CLAUDE.md` is refused as a write to it would be.
Where it cannot read them, any member could be an instruction file or one of
the loop's records.

*Instead:* the agent extracts the archive outside the project, under the temp
directory, and copies in by name the files it needs. If an earlier part of the
same command makes the archive, it runs that part first, as a command of its
own.

### `directory-entries-unknown`

Refuses a write or removal by glob, or a copy of a directory's contents into
the project, when the directory could not be listed in full: it holds more
than 20,000 entries, or the listing failed. A glob after a `cd` the hook
cannot follow, as in `cd - && rm -f *.o`, is refused the same way.

To judge a glob, the hook lists what it matches. A listing it could not finish
may hide an instruction file or one of the loop's records.

*Instead:* the agent names the files it means, or uses a narrower glob. To
empty a large directory, it removes the directory itself with `rm -rf dist`
and makes it again.

### `too-many-links`

Refuses a command one of whose paths goes through more symbolic links than
the hook follows: more than 64 on the way to one path, or more than 512 across
the whole command. A link that leads back to itself is refused the same way.
The refusal carries [SDLC-E0081](troubleshooting.md#sdlc-e0081) for one path
and [SDLC-E0082](troubleshooting.md#sdlc-e0082) for the command.

The hook judges a path by where it lands, so it follows each link on the way.
A path through more links than it follows has no place the hook can be sure
of.

*Instead:* the agent names the place the path leads to directly, without the
links, and splits a command over the limit into several.

### `path-too-long`

Refuses a command one of whose paths is longer, in full, than the system looks
up in one go: about 1,024 bytes on macOS and 4,096 on Linux. From a working
directory whose own path is that long, every command is refused. The refusal
carries [SDLC-E0083](troubleshooting.md#sdlc-e0083).

The hook places a path by looking it up from the root of the disk. One it
cannot look up, it cannot place.

*Instead:* make the full path shorter, by moving or renaming a directory on
the way to it, or run the command yourself. Running it from a nearer directory
does not help, because the hook reads the full path either way.

## Stopping mid-story

A session that ends its turn while a story is being worked on, without handing
the story to you or finishing it, leaves the story where nobody is looking. So
there is one more check, which has no rule name. When the session working a
story stops, the hook sends it back to work and says what comes next: work the
next gate, hand the story to you with `sdlc escalate`, or, once every gate has
passed, end it with `sdlc stop`.

A session is never held in a loop. After
[`loop.max_stop_blocks`](configuration.md#loop) stops in a row with nothing
recorded on the story (three, by default), the next one hands the story to you
and ends the iteration. `sdlc status` lists the story as waiting, and
`sdlc approve` lets it start again. Setting `max_stop_blocks` to `0` turns
the check off.

A session waiting for a subagent it started is not giving up: it has nothing
to record until the subagent reports. While Claude Code reports a subagent of
the session still running, the stop is let through and not counted. That wait
lasts 30 minutes from the first stop let through, and the 30 minutes is not a
setting. After it a stop counts as any other, so a subagent that never ends is
still handed to you.

Other background work, such as a shell command left running, does not hold the
count. Nor does anything where Claude Code does not report what a session has
left running, as an older version does not: every stop is counted there.

## Rules the command enforces

The hook is half of it. The other half is `sdlc gate`, which refuses to record
a pass for a gate that did not happen:

| `sdlc gate <name> pass` is refused when | Code |
| --- | --- |
| an earlier gate has not passed | [SDLC-E0031](troubleshooting.md#sdlc-e0031) |
| the story has no acceptance criteria, at the first gate | [SDLC-E0044](troubleshooting.md#sdlc-e0044) |
| the gate's documents have not been stored | [SDLC-E0020](troubleshooting.md#sdlc-e0020) |
| a reviewer has not reported, or reviewed something that has changed since | [SDLC-E0029](troubleshooting.md#sdlc-e0029) |
| a blocking reviewer blocked, or perf found a stated performance budget broken | [SDLC-E0030](troubleshooting.md#sdlc-e0030) |
| the tests are not frozen, or the freeze is another story's | [SDLC-E0023](troubleshooting.md#sdlc-e0023) |
| a frozen test changed or vanished | [SDLC-E0025](troubleshooting.md#sdlc-e0025) |
| there is a test file the freeze does not hold | [SDLC-E0043](troubleshooting.md#sdlc-e0043) |
| one of your configured commands fails when the gate runs them, at the implementation gate | [SDLC-E0095](troubleshooting.md#sdlc-e0095) |
| work is still uncommitted, at the commit gate | [SDLC-E0032](troubleshooting.md#sdlc-e0032) |
| the note names a commit the story's range does not hold, at the commit gate | [SDLC-E0049](troubleshooting.md#sdlc-e0049) |
| the branch has moved under the story since it started, at any gate before the commit | [SDLC-E0046](troubleshooting.md#sdlc-e0046) |

A `fail` can always be recorded. A gate can fail because its work could not be
done, and refusing to record that would leave the loop nowhere to put the
truth.

`sdlc freeze` is held the same way. Where the story has a test reviewer, the
freeze [waits for it](troubleshooting.md#sdlc-e0029) to approve the tests as
they are. And it [refuses](troubleshooting.md#sdlc-e0085) new tests that
already pass, since a test written before its code has nothing to pass on.

[`sdlc gate`](commands.md#sdlc-gate-gate-status) has the rest, and
`sdlc stop` checks a finished story once more
[before it lands](the-loop.md#done-when-it-lands).

## What sdlc does not stop

**This is a discipline tool, not a sandbox.** It holds an agent that is trying
to do the right thing and takes the shortest route to it. It does not defend
against one that is set on getting out, and it would be dishonest to imply
otherwise.

Three things are worth knowing about how it is built:

- **The hook reads, and does not confine.** It runs before a tool call and
  refuses what it reads as breaking a rule. A command it cannot read that way
  runs with your privileges, like any other.
- **What matters most has a second line.** Your six decisions refuse
  themselves in any process Claude Code started. A commit made behind the
  hook's back, a frozen test that changed and work that changed after its
  reviews are each refused again by `sdlc gate`, and once more when the story
  lands.
- **It uses Claude Code's own hooks and nothing else.** `sdlc` installs no git
  hooks and never writes to `.git/hooks`.

### When the hook cannot do its job

The hook never blocks your session because of a fault of its own. A call it
cannot read, a configuration that is missing or a path it cannot resolve ends
in the call going through. In the session working a story it says so, in a
message that starts `sdlc:` and names what is off.
[Warnings the hook prints](troubleshooting.md#warnings-the-hook-prints) lists
each message with what it switches off.

Four things are not let through that way, because breaking them would
otherwise be the way round them:

- **A freeze or a gate record that cannot be read** is not treated as absent.
  The tests stay frozen and the commit waits until the file reads, and
  `sdlc doctor` names it.
- **A main checkout the hook cannot find** from a story's worktree: the commit
  is refused, and every test counts as frozen.
- **A working tree the hook cannot measure**, for a story whose commit waits
  for your approval: the commit is refused under
  [`commit-gate`](#commit-gate). A commit nothing pauses goes through with a
  warning.
- **Running out of time**, which is refused under
  [`not-checked-in-time`](#not-checked-in-time).

### Not held

These gaps are known and not closed. Each says what can happen and, where
there is a way, how you would notice.

#### Commands the rules do not read

- **A call the hook is killed on before it knows whether it checks it.**
  Claude Code kills a hook after 30 seconds, and a hook it kills before the
  hook has learnt whether the call is one it checks still lets the call
  through, with nothing said in the session. A person's decision run in such a
  call is refused by the command itself, with
  [SDLC-E0079](troubleshooting.md#sdlc-e0079).
- **Words or a script from standard input, or from a file.** The rules read a
  command as it is written, and not what a file or a pipe hands a program:
  `sh tidy.sh`, `echo 'git worktree prune' | sh`,
  `echo prune | xargs git worktree`. A command run that way can commit in
  another checkout or remove a worktree. A person's decision run that way is
  refused by the command itself, with SDLC-E0079. Ending a story part-way with
  `sdlc stop` is not one of the six, and gets through.
- **A program the rules do not read through.** The rules read past the common
  wrappers, such as `env`, `sudo`, `timeout` and `xargs`, and no others. A
  commit or the removal of a worktree behind any other program is unread, as
  in `docker run img git commit -m x` with the checkout mounted. A person's
  decision run by such a program is refused by the command itself, with
  SDLC-E0079.
- **A command line for another kind of shell.** A string handed to a shell of
  another kind than the tool's, such as `pwsh -Command` in a `Bash` command or
  `bash -c` in a `PowerShell` one, is read by the rules of the tool it came
  from. A commit written that way can land in another checkout as a plain
  commit, which only [`commit-gate`](#commit-gate) holds. A person's decision
  in such a string is refused by the command itself, with SDLC-E0079.
- **What the persistent shell keeps.** The `Bash` tool keeps one shell from
  call to call, and the hook sees only the environment the session started
  with. A variable exported in an earlier call, such as `GIT_DIR`, is unseen,
  so a later `git commit` can land on the branch another checkout has checked
  out, trunk included. A call that stages another checkout's work, followed by
  a plain commit in the next call, puts that work in the story's commit.
- **A place the command does not spell.** A path given only as a default, as
  in `rm -rf "${D:-..}"`, and code that works out its own path, as in
  `python3 -c 'import os, shutil; shutil.rmtree(os.getcwd())'`, can remove the
  checkout itself. `git worktree list` then marks it prunable, and every
  `sdlc` command on its story says the checkout
  [no longer exists](troubleshooting.md#sdlc-e0051).
- **Your own git configuration.** Aliases and includes in `~/.gitconfig` and
  the system configuration are unread. An alias of yours that commits or
  removes a worktree is allowed as `git st` is.
  `git config --show-origin --get-regexp '^alias\.'` lists every alias git
  would run.
- **`sdlc` reached under another name.** Through a variable, a substitution, a
  copy of the binary, or code that runs it, `sdlc` is not always found by the
  hook. The six commands that are a person's still refuse themselves, with
  SDLC-E0079. Nothing behind the hook stops `sdlc review add` reached that
  way: you would notice a review in `sdlc review list` with no run of its
  reviewer behind it in `sdlc trace`.
- **A line built to be misread.** The rules follow the common forms of shell
  grammar, in `Bash` and `PowerShell`, and not all of it. A line built on a
  corner of it can run a command the rules took for something else: a `case`
  inside `$( )`, a program name made from substitutions, an alias, a function
  or a trap defined in an earlier call, or GNU `parallel` given command lines
  as its arguments. A push that gets out that way is stopped only by
  protecting the branch on your server.
- **A program that starts clean, or removes Claude Code's markers.** A
  person's decision run by a program that starts it with a clean environment
  is not caught: `launchctl submit`, `open`, `at`, `cron`, Terminal told by
  AppleEvents to run it, a `tmux` server you started that is already running,
  `su -`, `sudo -i`, `ssh`, `schtasks`, WMI and
  `Start-Process -UseNewEnvironment` each start it with none of the markers. A
  person's decision run after a command has removed Claude Code's markers from
  its environment is not caught either. Nor is a slash command fed to the hook
  on a message an agent wrote, once a command the rules do not read has
  overridden `AI_AGENT` with a hook's label and removed the other markers the
  hook reads. Each story's `gate-record.json` lists every approval and
  unfreeze, with when it was made.
- **A protected name changed where the rules cannot see it.**
  [`enforcement-stays-on`](#enforcement-stays-on) refuses the commands it
  reads that change one of its six names, `CLAUDE_CODE_EXECPATH` and
  `GIT_EDITOR` among them. It does not read a script run from a file,
  interpreter code that builds a name from parts, or the commands `launchctl`
  and `tmux` have for the environment they hand on, such as
  `launchctl setenv` and `tmux set-environment`. Through one of those, a
  message an agent wrote can reach the hook as your typed command, and a
  session an agent starts can look attended.
- **A session an agent starts in a terminal of its own.** Claude Code counts a
  session with a terminal of its own as one you attend, so a slash command
  typed there may run as yours.
  [`story-stays-in-its-session`](#story-stays-in-its-session) refuses the
  starts it reads. It does not read a file that starts one, run by `osascript`
  or opened in a terminal app. Nor does it read a terminal program it does not
  know, such as `xterm`, `dtach` or `asciinema`, or the path to Claude Code
  reached through a substitution and then handed to `tmux`. Whether the hook
  of such a session still carries the agent's `CLAUDE_CODE_EXECPATH` and
  `GIT_EDITOR` has not been checked. Where it does not, that rule is all that
  stands in front of it.
- **A project MCP server, or a project hook.** Any session can write
  `.mcp.json`, which no rule protects, and a session that holds no story can
  write the project's `.claude/settings.json`. Claude Code starts a hook named
  there with an environment of its own, which passes the hook's check of a
  typed slash command. A server named in `.mcp.json` has not been checked, and
  is taken to be alike. You would notice Claude Code asking you to approve a
  server you did not add, or `git status` showing one of those files changed.
  A `.claude/settings.local.json` that git ignores does not show there.
- **A slash command a program types into your own prompt.** Where you run
  Claude Code inside `tmux` or `screen`, or on macOS, a command an agent runs
  can type into your session's prompt: with `tmux send-keys`,
  `screen -X stuff`, or a keystroke sent by `osascript`. Claude Code takes it
  as your typing, in Claude Desktop too, and no rule reads a keystroke as one.
  An `osascript` that tells Terminal or iTerm to type into a window already
  open goes through the same way. You would notice a command in your
  transcript that you did not type.
- **A later Claude Code.** The hook's check of a typed slash command rests on
  what Claude Code 2.1.321 sets, as read in Claude Desktop, whose entrypoint
  is `claude-desktop`: a label in `AI_AGENT` that ends `_harness` for a hook
  Claude Code runs itself, and a `CLAUDE_CODE_SESSION_ATTENDED` it writes
  afresh for each hook from `CLAUDE_CODE_CHILD_SESSION` and
  `CLAUDE_CODE_ENTRYPOINT`. A release that changes those so that the hook can
  no longer read them fails closed: your slash command answers that it did not
  run. A release that makes the hook of a session an agent started look like
  yours does not fail closed.
- **A path no rule can tell is the same file.** A hard link, or on macOS a
  `/.vol/<device>/<inode>` path, is a second name for a protected file, and a
  write through it is placed where it is spelled. `git status` shows the
  protected file changed.
- **A session in a project that is not set up for the loop.** Its calls are
  unread, and it is held to nothing. So a command there can give `AI_AGENT` a
  hook's label, remove the other markers, and run the hook on a message of its
  own for a project that is set up: a person's decision recorded that way is
  not caught.
- **The story's figures.** An agent's run or a refusal recorded by a route the
  rules do not see, such as a script file that runs the hook, goes into the
  story's record and its trace. That can change what
  [`sdlc trace`](commands.md#sdlc-trace) reports about cost, and never whether
  a gate passed.

#### Branches and the landing

- **A branch moved by a spelling the rules do not read.** Every gap above gets
  a branch move past [`branch-through-the-tool`](#branch-through-the-tool) as
  well. Trunk moved that way holds work no gate passed: in your main checkout,
  `git status` shows changes nobody made there, and `git reflog` shows when. A
  story's own branch moved that way does not land, and another story's is
  reported by that story's
  [next gate](troubleshooting.md#sdlc-e0046).
- **A reset of the story's own branch before the commit gate.** A soft or
  mixed `git reset` there is the session's own work and is not refused. The
  next gate names it.
- **The hold ending before the landing.** Escalating ends the iteration. Until
  you answer, the session holds nothing and none of this binds it. The
  escalation is on the record and in `sdlc status`, and the landing still
  makes every check.
- **Git settings that name a program.** A session can write `core.hooksPath`,
  `core.fsmonitor` or a filter in `.gitattributes`, and git then runs the
  program during commands the rules allow, the landing among them. `sdlc` does
  not report it: read `git config --show-origin --list` and `.gitattributes`.
- **A stop or a rebase you run.** `sdlc stop` run by you, and a rebase the
  loop [sends you to do by hand](troubleshooting.md#sdlc-e0075), run the
  story's code and the repository's hooks, filters and merge drivers with your
  privileges. Read the configuration first.
- **`CODEMAP.md` reaches trunk with no reviewer.** `sdlc stop` commits the
  story's sections of it to trunk after the story lands, in a commit named
  `docs: map <ID>`. Read it with `git show`.
- **Ignored files.** Files the story's worktree ignores are in no tree the
  loop measures, yet they are there when the landing's commands run, and can
  make a command pass that would fail on trunk. List them with
  `git -C .sdlc/worktrees/<ID> status --ignored`. They are removed with the
  worktree when the story lands.
- **A change moved between identical lines.** The landing compares a change
  with the one its reviewers saw by its lines, so a change moved to an
  identical stretch elsewhere in the same file compares equal.
- **A frozen test put back by hand over trunk's change.** A commit made by
  hand after a rebase, restoring a frozen test that trunk changed, reads as an
  intact freeze and takes trunk's change to that test away. The reviewers see
  it in the diff when the change is reviewed again.
- **A test file committed by hand, with trunk fast-forwarded to it.** A test
  file committed by hand on the story's branch, where trunk's branch then
  comes to point at that commit before any command of `sdlc` has run, reads
  as trunk's test and is accepted as one. The accepting command prints
  `accepted as <trunk>'s` and names the file.
- **A shallow repository** can hide ancestry. The landing then
  [refuses](troubleshooting.md#sdlc-e0054).

#### The review policy

- **What the shell rules do not read reaches the story's copy.** A change left
  in the story's commits is in its diff, and
  [`sdlc stop` refuses it](troubleshooting.md#sdlc-e0055). A copy changed and
  restored before the commit leaves no trace, so reviewers may have read a
  changed policy. With [`git.worktrees`](configuration.md#git) off there is no
  landing to refuse.
- **A link's target.** Where the configured name is a link to another file in
  the repository, a file tool can write through the name, the shell rule
  leaves the target alone, and a change to the target lands. The backlog
  behaves the same way.

#### The findings ledger

- **A record two releases share.** An `sdlc` older than the findings drops
  them when it saves a story's record. The verification and commit gates
  [count them and refuse](troubleshooting.md#sdlc-e0093), but an older `sdlc`
  that records those gates itself checks no findings at all. Keep the plugin's
  `sdlc` and the one on your `PATH` on one release.
- **A path the match does not read.** A glob, or a path written only inside a
  finding's text, is not matched against the story's changes. The verifier
  reads every such finding against the diff.

#### The run of your commands

- **A run from a line the rule cannot read.**
  [`commands-run-as-configured`](#commands-run-as-configured) narrows who
  chooses the environment of `sdlc gate implementation pass` and
  `sdlc freeze`, and does not settle it. It does not read a script or a file
  the line runs, as in `bash ./x.sh`, `make gate` or `npm run`. Nor does it
  read `sdlc` run under another name: a copy, a hard link, or `go build -o` in
  a project that builds `sdlc`. Inline interpreter code that builds the name,
  an `eval` given text the line builds, and a substitution that is the whole
  call behind another program, as in `sudo $(printf 'sd%s freeze' lc)`, get
  past it too. None of these gives more than `bash ./x.sh` already does. You
  would notice a recorded pass whose tests did not run, or ran fewer than the
  story's, in the output file the gate names.
- **A setting made somewhere other than the line.**
  [`commands-run-as-configured`](#commands-run-as-configured) reads one
  command line. It does not see a tool's own saved settings, such as
  `go env -w GOFLAGS=...`, your shell's startup files, a variable set in
  Claude Code's settings, or a variable exported in an earlier call of the
  persistent shell.
- **In `PowerShell`, a line that runs the call without naming it.**
  [`commands-run-as-configured`](#commands-run-as-configured) reads a
  `PowerShell` line for the call's own words. A line that builds them, with
  `-f`, `-join` or a script block, or that hands them to `pwsh -Command` in a
  single-quoted script, can set a variable and run the call unread.

#### A repository with more than one checkout

- **Ordinary files in another checkout.** The shell can write a file no rule
  protects in another checkout: your main checkout's source from a story's
  worktree, or another story's. `git status` there shows the change, and a
  frozen test changed that way is reported by that story's
  [next gate](troubleshooting.md#sdlc-e0025).
- **`GIT_WORK_TREE` around `sdlc` itself.** An `sdlc` command run with it set
  treats that directory as the checkout, `sdlc start` included.
- **A finished story with worktrees turned off.** Between its retro and
  `sdlc stop`, the session working it is held to nothing in the repository's
  other checkouts. The loop runs `sdlc stop` straight after the retro, which
  closes it.
- **Two checkouts naming one story.** A session that holds no story can write
  the files that say which checkout holds one. Each checkout is then
  [refused the story](troubleshooting.md#sdlc-e0050) until you run `sdlc stop`
  in all but one.

### Report a hole

A shell spelling that gets past a rule, and is not on the list above, is the
most useful report this project gets. Please send it privately, through
[GitHub's private vulnerability reporting](https://github.com/bbsnly/sdlc/security/advisories/new),
and not as a public issue. [SECURITY.md](../SECURITY.md) says what is in scope
and what to expect.

A rule that is wrong for your project is not a hole. Open an issue and say
which rule, and why.

## Turning it off

If you are debugging the loop itself, you may switch enforcement off for one
session. Start Claude Code with `SDLC_ENFORCE` set to `0`:

```console
$ SDLC_ENFORCE=0 claude
```

It must be exactly `0`, and it is read from the environment the session starts
with. An agent that tries to set it from inside a session is refused, under
[`enforcement-stays-on`](#enforcement-stays-on).

With it set, every part of the hook stands down for that session, and says
nothing about it:

- no file write and no command is refused, a person's decisions included;
- a session that [stops mid-story](#stopping-mid-story) is not sent back;
- your formatter, [`commands.fmt_file`](configuration.md#commands), is no
  longer run on the files an agent writes.

Three things carry on. Your slash commands still run. The six commands that
are yours still [refuse themselves](troubleshooting.md#sdlc-e0079) when an
agent runs one. And the story's trace is still recorded, with no refusals in
it, because none were made.

If you find yourself reaching for this during ordinary work, the rule in your
way is probably wrong for your project. Please open an issue and say which
one.

## Seeing what happened

A hook that allows a call says nothing about it, which makes "why did nothing
happen?" the hardest question to answer from outside. To see every decision,
start Claude Code with `SDLC_DEBUG_FILE` set to a file of your choosing:

```console
$ SDLC_DEBUG_FILE=/tmp/sdlc-hook.log claude
```

Each tool call then adds a few lines to that file. The last of them is the
decision:

```text
time=2026-11-24T10:50:42.155+01:00 level=DEBUG msg="hook decision" event=PreToolUse considered=true allowed=false rule=commit-gate via=released
```

`allowed` says whether the call went through, and `rule` names the rule that
refused it. `considered` is `false` when the hook never got as far as the
rules, for instance because the session is not the one working the story.
[See every decision](guides/why-was-i-refused.md#see-every-decision) reads the
other lines with you.

Two more variables change what is logged:

| Variable | What it does |
| --- | --- |
| `SDLC_DEBUG=1` | writes the same log to the hook's standard error, which Claude Code keeps in its own debug log |
| `SDLC_TRACE=1` | adds a line saying how long the hook took, which is how to tell whether a slow session is the hook |

With `SDLC_TRACE` on, each call ends with a line like this one, in
milliseconds:

```text
time=2026-11-24T10:50:42.243+01:00 level=DEBUG msg=stage name=hook ms=2
```

To look back at a story without a log, run `sdlc trace <ID>`. Among other
things, it counts the story's refusals by rule and by agent.

## Where to go next

- [Why was I refused?](guides/why-was-i-refused.md): reading one refusal, and
  what to check when nothing was refused
- [Troubleshooting](troubleshooting.md): every error code, and the hook's
  warnings
- [Human approval gates](guides/human-approval-gates.md): the decisions the
  loop keeps for you
- [When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md):
  lifting the freeze, on the record
- [The loop](the-loop.md): the gates these rules protect
- [SECURITY.md](../SECURITY.md): what counts as a security report, and how to
  send one
