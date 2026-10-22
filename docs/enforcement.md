# Enforcement

- [Introduction](#introduction)
- [What this is, and what it is not](#what-this-is-and-what-it-is-not)
- [When any of it applies](#when-any-of-it-applies)
  - [`not-checked-in-time`](#not-checked-in-time)
- [Rules on writing files](#rules-on-writing-files)
  - [`claude-config-is-the-persons`](#claude-config-is-the-persons)
  - [`write-outside-repository`](#write-outside-repository)
  - [`write-protected-path`](#write-protected-path)
  - [`backlog-is-not-edited`](#backlog-is-not-edited)
  - [`gate-record-is-written-by-the-tool`](#gate-record-is-written-by-the-tool)
  - [`review-is-written-by-the-tool`](#review-is-written-by-the-tool)
  - [`gate-artifact-is-written-by-the-tool`](#gate-artifact-is-written-by-the-tool)
  - [`frozen-test-is-not-edited`](#frozen-test-is-not-edited)
  - [`no-new-test-after-the-freeze`](#no-new-test-after-the-freeze)
  - [`implementer-does-not-write-tests`](#implementer-does-not-write-tests)
  - [`sdet-writes-tests-only`](#sdet-writes-tests-only)
  - [`researcher-writes-analysis-only`](#researcher-writes-analysis-only)
  - [`reviewer-reviews`](#reviewer-reviews)
  - [`bookkeeper-writes-the-retro-and-the-map`](#bookkeeper-writes-the-retro-and-the-map)
  - [`orchestrator-delegates`](#orchestrator-delegates)
- [Rules on shell commands](#rules-on-shell-commands)
  - [How a command is read](#how-a-command-is-read)
  - [A path outside the repository](#a-path-outside-the-repository)
  - [`loop-state-through-the-tool`](#loop-state-through-the-tool)
  - [`protected-path-through-the-tool`](#protected-path-through-the-tool)
  - [`frozen-test-through-the-tool`](#frozen-test-through-the-tool)
  - [`unfreeze-is-a-human-decision`](#unfreeze-is-a-human-decision)
  - [`approval-is-a-human-decision`](#approval-is-a-human-decision)
  - [`acknowledgement-is-a-human-decision`](#acknowledgement-is-a-human-decision)
  - [`remap-is-a-human-decision`](#remap-is-a-human-decision)
  - [`release-is-a-human-decision`](#release-is-a-human-decision)
  - [`parallel-is-a-human-decision`](#parallel-is-a-human-decision)
  - [`stop-is-a-human-decision`](#stop-is-a-human-decision)
  - [`review-is-recorded-by-its-reviewer`](#review-is-recorded-by-its-reviewer)
  - [`assignment-is-a-human-decision`](#assignment-is-a-human-decision)
  - [`slash-command-is-a-human-decision`](#slash-command-is-a-human-decision)
  - [`staffing-is-recorded-by-the-project-manager`](#staffing-is-recorded-by-the-project-manager)
  - [`project-manager-only-staffs`](#project-manager-only-staffs)
  - [`commit-gate`](#commit-gate)
  - [`commit-in-another-checkout`](#commit-in-another-checkout)
  - [`worktree-through-the-tool`](#worktree-through-the-tool)
  - [`worktree-setting-through-the-tool`](#worktree-setting-through-the-tool)
  - [`branch-through-the-tool`](#branch-through-the-tool)
  - [`git-command-cannot-be-read`](#git-command-cannot-be-read)
  - [`enforcement-stays-on`](#enforcement-stays-on)
  - [`story-stays-in-its-session`](#story-stays-in-its-session)
  - [Claude config through the shell](#claude-config-through-the-shell)
  - [`command-too-deep-to-read`](#command-too-deep-to-read)
- [A repository with more than one checkout](#a-repository-with-more-than-one-checkout)
  - [Not held: the known gaps](#not-held)
- [Unattended sessions](#unattended-sessions)
- [Stopping mid-story](#stopping-mid-story)
- [Rules the command enforces](#rules-the-command-enforces)
- [Turning it off](#turning-it-off)
- [Seeing what happened](#seeing-what-happened)

## Introduction

The loop is a set of rules, and rules that are only asked for are advice. So
that the rules hold, a hook watches what the assistant working your story tries
to do, and refuses the few things that would get round a gate.

Most of the time you will meet this as a single refusal. It names the rule that
refused the call and the command to run instead, and the assistant carries on
from there. If you want to read one, start with
[Why was I refused?](guides/why-was-i-refused.md).

This page lists every rule that is actually enforced, what it refuses, and what
to do instead. There are two groups of them: the
[rules on writing files](#rules-on-writing-files) and the
[rules on shell commands](#rules-on-shell-commands). You do not need to read
them in order. Look up the rule named in a refusal, and come back to the
sections on [when any of it applies](#when-any-of-it-applies) and on
[turning it off](#turning-it-off) when you need them. The gaps that are known
and not closed are listed under [Not held](#not-held).

## What this is, and what it is not

Enforcement uses Claude Code's own surfaces and nothing else: a `PreToolUse`
hook, a `Stop` hook, the plugin's agents, and the `sdlc` command. A
`PostToolUse` hook runs your [`commands.fmt_file`](configuration.md#commands),
and refuses nothing. No git hooks are installed, and `sdlc` never writes to
`.git/hooks`.

**This is a discipline tool, not a sandbox.** It constrains an assistant that is
trying to do the right thing and takes the shortest path to it. It does not
defend against one that is trying to escape, and it would be dishonest to imply
otherwise.

The rules below are narrow on purpose, and every refusal names the sanctioned
route. A refusal that only says "no" makes an assistant retry, work around the
hook, or give up, all worse than the thing being prevented.

## When any of it applies

The hook does not watch every session. It holds one session to one story, and
leaves every other session alone. Nothing is enforced unless
**all** of these are true:

1. The project has `.sdlc/config.json`, found by looking up from where the session and the
   tool call are, as far as the repository root and not into a directory
   `GIT_CEILING_DIRECTORIES` names.
2. A story is being worked on — `.sdlc/state/active` names one.
3. The call comes from the Claude Code session working the story, or from one
   of its agents. `sdlc start` records the session it runs in, in
   `.sdlc/state/session`, so the session that starts or resumes a story is the
   one held to it, and every other session in the repository is left alone.
   Run outside Claude Code, `sdlc start` records no session: a new story holds
   no session until one picks it up with `/sdlc:next`, and a story resumed that
   way keeps the session it had. `/sdlc:next` stops rather than work a story
   when `sdlc start` could not record the session it runs in. A call that names
   no session, or a `.sdlc/state/session` that is empty, unreadable or names
   something that cannot be a session id, holds no session either.
4. `SDLC_ENFORCE` is not `0` in the environment the session started with.

### Other sessions in the same repository

In every other session the plugin is silent: it never sends a session back at
the end of its turn, runs no formatter, writes nothing and prints nothing, with
the binary installed or not. It refuses nothing there but a person's decisions,
below. `sdlc status` and `sdlc doctor` say when a story is under way
and no session is working it.

Left alone means every rule. Another session can edit code and tests and
commit, and the loop commits a story with `git add -A`, so work it has left
uncommitted goes into the story's commit. A commit it makes moves HEAD
under the story, and from then on every gate before the story's own commit is
refused ([SDLC-E0046](troubleshooting.md#sdlc-e0046)), so the story goes to a
person.

Another session's `sdlc` commands do not write into the story.
`sdlc artifact write`, `review add`, `gate`, `escalate`, `cost add`, `freeze`,
`unfreeze`, `approve`, `release`, `staffing set`, `staffing amend` and `stop`
refuse to run, and change nothing, with
[SDLC-E0048](troubleshooting.md#sdlc-e0048), when `CLAUDE_CODE_SESSION_ID`
names a Claude Code session other than the one `.sdlc/state/session` records.
This keeps one session's `sdlc` commands out of a story another is working. It
is not a barrier against an assistant set on getting round it: `sdlc start` is
not refused, because it is how `/sdlc:next` takes a story over; a command with
no `CLAUDE_CODE_SESSION_ID` reads as a terminal, and a person; and another
session can still edit `.sdlc/` without `sdlc`. Nothing is refused while no
session is recorded, and `sdlc ack` changes no story.

### A person's decisions, refused in every session

Where no story is being worked on, or the call comes from another session, the
hook allows everything and says nothing, with one exception: a person's
decisions, refused from a tool call in any session, in any project with
`.sdlc/config.json` the call reaches (the session's, the working directory's,
or one a path in the command names). With `SDLC_ENFORCE` set to `0`, the hook
refuses nothing at all, a person's decisions included; the commands' own
refusals below stay, [SDLC-E0079](troubleshooting.md#sdlc-e0079) among them.

- `sdlc ack` is refused there always. The log of what landed on trunk is always
  there for a person to read.
- `sdlc remap` is refused there always, its help included
  ([`remap-is-a-human-decision`](#remap-is-a-human-decision)). A landed story
  is not one that waits, so whether a story waits is never asked for it.
- `sdlc release` is refused there always. It takes a story from the checkout
  holding it ([`release-is-a-human-decision`](#release-is-a-human-decision)).
- `sdlc parallel` is refused there always
  ([`parallel-is-a-human-decision`](#parallel-is-a-human-decision)).
- `sdlc staffing assign`, and a run of `sdlc` whose subcommand cannot be read,
  are refused there always
  ([`assignment-is-a-human-decision`](#assignment-is-a-human-decision)).
- `sdlc approve` and `sdlc unfreeze` are refused while a project the call
  reaches, or the one the command moves to with `cd`, has a story a person could
  decide on: one under way, whichever session holds it or none, or one handed to
  a person with `sdlc escalate` that nobody has answered. With no story under
  way and none waiting, they are not. After a move the hook cannot follow, such
  as `cd "$OLDPWD"`, `cd ../$name` or `popd`, they are refused as if a story
  waited where it led.
- A command nested too deep to read
  ([`command-too-deep-to-read`](#command-too-deep-to-read)) is refused whatever
  it runs, since any of these could be inside it.
- A write to the person's Claude Code configuration, by a file tool or a
  command, is refused there always
  ([`claude-config-is-the-persons`](#claude-config-is-the-persons)).
- Starting a Claude Code session that could expand one of the person's slash
  commands is refused there always
  ([`story-stays-in-its-session`](#story-stays-in-its-session)).
- Running the sdlc hook by hand on `UserPromptExpansion`, the event on which it
  runs a person's slash command, is refused there always
  ([`slash-command-is-a-human-decision`](#slash-command-is-a-human-decision)).

A command is read to its end, so `sdlc approve A-1; sdlc ack --through HEAD` is
refused for the `sdlc ack` in it. `sdlc escalate` ends the iteration, so the
answer to it always comes while no session is held, and no session's assistant
approves another's story. A session that is not using sdlc never meets the
refusals of `sdlc ack`, `sdlc release`, `sdlc parallel`,
`sdlc staffing assign`, `sdlc approve` and `sdlc unfreeze`: each is refused only
when an assistant runs that command itself. A command nested too deep to read
is refused in any of those projects, whatever else it does.

Behind those rules, the commands refuse themselves. `sdlc approve`,
`sdlc unfreeze`, `sdlc ack`, `sdlc remap`, `sdlc release`, `sdlc parallel` and
`sdlc staffing assign` each refuse to run, before anything is read, with
[SDLC-E0079](troubleshooting.md#sdlc-e0079), in a process whose environment has
any of `CLAUDECODE`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_CODE_CHILD_SESSION`,
`CLAUDE_PID` or `AI_AGENT`, the environment variables Claude Code sets in the
processes it starts. [SDLC-E0061](troubleshooting.md#sdlc-e0061) and
[SDLC-E0074](troubleshooting.md#sdlc-e0074) answer first for `sdlc parallel`
and `sdlc staffing assign` where the session id is set. A model's Bash or
PowerShell has those variables, and so does every program it starts that keeps
its environment, so the seven commands are refused even where the shell rules
cannot read them: behind `arch`, `osascript -e`, `su -c`, `vim -c`, awk's
`system()`, `git -c core.pager` or `GIT_EDITOR`, through `Invoke-Expression`
or a PowerShell argument in parentheses, behind a wrapper the rules do not read
through that keeps the environment, and in a shell or an interpreter fed its
program on standard input. `sudo`, `doas`, `env -i` and `env -u` start a
command without them; written out, the shell rules read the command after them
and refuse it there. A `!` line typed in a session has the variables too, so
there a person types the slash command instead.

A slash command a person types reaches the hook, which hands the command none
of those variables, so it records. A hook run from a model's tool call has
`AI_AGENT`, and so has the hook of a session started from one: there a typed
person's command answers that it did not run, and records nothing. What neither
catches is listed in [Not held](#not-held): a program that starts with a clean
environment or removes those variables; a part-way `sdlc stop`, which is not
one of the seven; a program Claude Code starts itself from a project's
configuration, such as an MCP server named in `.mcp.json` or a hook in
`.claude/settings.json`, which may not have `AI_AGENT`; and a slash command a
program types into the person's own prompt, as `tmux send-keys` can, which
reaches Claude Code's own hook as the person's typing would.
`SDLC_ENFORCE=0` turns none of this off.

Asking one of these commands for its help decides nothing and is not refused:
`sdlc approve --help`, on a line of its own or after a `cd`, alone or piped to
`head`, `tail`, `cat` or `grep`. The flags are read as `sdlc` reads them, so
the last of several help flags counts, `--help=false` is not help, and neither
is `--help` after `--` or as the value of another flag. Any other form is
refused as the command would be: after another command, with a trailing
comment, with a file redirect, piped to a pager or any other program, in the
background, through `env` or another program, or with a variable or a
substitution among its words, since the hook cannot be sure the command would
not run. `sdlc help approve` prints the same help and is not refused in any of
those forms, since it names no command that decides anything.

### When the hook fails open

The hook also **fails open**: an unreadable payload, a missing configuration, a path it cannot
resolve — an error ends in "carry on", and in the session working a story, with
a message saying what is off. A call that cannot be read names no session, so
it goes through with nothing said. A hook that blocks a session because of its
own bug is worse than the mistake it was trying to prevent.

The exceptions are the files a rule exists to protect. A test freeze or a gate
record that cannot be read is not treated as absent, because then breaking one
would be the way round it: tests stay frozen and the commit waits until the file
reads. `sdlc doctor` names it under "loop state".

Running out of time is not one of those errors either. A call the hook checks
and cannot decide in time is refused, with the rule name
[`not-checked-in-time`](#not-checked-in-time). That section also says what is
still left open.

Nor is a slash command a person typed, such as `/sdlc:approve`. The hook runs
the command it stands for and shows the person what it printed, and the
command's text never reaches the model. Carrying on there would hand that text
to the model, which is told only to say the command did not run. So a command
the hook could not run, because its arguments did not read, because the
session has no project folder, or because the command itself gave way, is
answered with a block that says it did not run and what to do. Nothing is
decided either way: the block stops only the model reading the command.

A slash command acts on the directory the session is in, as a terminal there
would, so that a person can type one in a story's worktree. The model can move
a session's directory between calls, into another checkout that takes part, and
the person's next command then acts there. What it printed, which the person is
shown, names the story and the checkout, and a story that checkout does not
have is refused.

### `not-checked-in-time`

Refuses a call the hook checks and could not check in time: a command it could
not finish reading in time, or a change to a file it could not check in time.
The refusal says that the hook ran out of time. It does not say the call broke
a rule, and no rule was applied to it.

Claude Code gives the hook 30 seconds for a tool call and then kills it. A
killed hook says nothing, and the call runs with no rule applied. So the hook
keeps a deadline of its own, 25 seconds from when it starts, and refuses a call
it checks and has not decided by then. Reading the call, finding the project
and the story, and reading the command all count against it. A command long
enough to take that much reading could get round every rule on this page with
nobody told. Once the hook knows it checks the call, it no longer can; see
[What is still left open](#what-is-still-left-open).

Measuring the working tree for a commit counts against the deadline too, but
that check has a limit of its own, 20 seconds from the first `git` call the
hook makes. Where that limit ends first, the commit goes through with a
warning that the tree could not be measured, as it always has. Only a commit
still being measured when the hook's 25 seconds end is refused.

This refusal is given for the calls the hook checks:

- any call from the session working a story;
- a command from any session, in any project with `.sdlc/config.json` the call
  reaches (the session's, the working directory's, or one a path in the command
  names), which is where
  [a person's decisions](#a-persons-decisions-refused-in-every-session) are
  refused.

So a session that holds no story can meet this refusal too, for a command in
such a project that takes the hook too long to read. A file written by that
session is never refused for time. Nor is any call that reaches no such
project, nor anything while `SDLC_ENFORCE` is `0`. A call decided in time is
answered exactly as it was before there was a deadline.

*Instead:* run a command in shorter parts, one call for each. Try a change to a
file again as it is. If a short command, a commit, or the same change is
refused this way again, it is not the call that is slow: stop and tell the
person running the session.

If you are that person, [Troubleshooting](troubleshooting.md#warnings-the-hook-prints)
says which lines of the hook's log to read. [Turning it off](#turning-it-off)
is the last resort, because it disables every rule on this page and not only
this refusal.

[`SDLC_HOOK_DEADLINE_MS`](configuration.md#sdlc_hook_deadline_ms) can shorten
the deadline. Nothing lengthens it.

#### What is still left open

The hook refuses only once it knows the call is one it checks, so that a call
it does not check, such as a file written by a session holding no story, never
hears from the plugin because the hook was slow. Until it knows, a deadline
that passes changes nothing. The hook carries on finding out,
refuses the call there and then if it is one it checks, and otherwise answers
as it always does.

So a hook that Claude Code kills before it has learnt whether the call is one it
checks still lets the call through and says nothing. Finding the project means
walking up from every path a command names, and that walk is not bounded: a
command can make it outlast the 30 seconds, and so can a call that never
finishes arriving. Nothing appears in the session. With `SDLC_DEBUG_FILE` set,
the log may hold a line saying the deadline passed before the hook knew whether
it checks the call, if the hook lived to write it.

There is a second case. Once the hook has decided to allow a commit, it writes
down the commit the work is made on, and that step is not under the deadline. A
hook killed there has already allowed the commit, which goes through, and the
note is lost; [SECURITY.md](../SECURITY.md) lists it among the limits of what
the commit gate knows.

This refusal makes no reading faster. It answers for the calls that run out of
time once the hook knows what it is looking at.

## Rules on writing files

These apply to `Write`, `Edit`, `MultiEdit` and `NotebookEdit`. Reading,
searching and running things are nobody's business here.

### `claude-config-is-the-persons`

Refuses writing the person's Claude Code configuration, from any session, in a
project that takes part in the loop: `~/.claude` and everything under it,
`~/.claude.json`, and the directories `CLAUDE_CONFIG_DIR` and
`CLAUDE_CODE_PLUGIN_CACHE_DIR` name. Links are followed both ways, so a link in
the repository that leads there is refused, and so is a write through
`~/.claude` to wherever it leads. The file tools are refused by this rule, and
so is a shell command that writes there, as
[the shell's reading of it](#claude-config-through-the-shell) says.

That configuration decides the hooks, plugins, skills, agents and permissions
every session runs under, this plugin's own launcher, binary and checksums
included. A session that could write it could take the hook away, give itself
the permissions a person withholds, or change what a person's slash command
runs, and then type it. `~/.claude.json` is refused whole, on purpose: it holds
the person's MCP servers and the settings of every project, and no part of it
is the model's to change. A project kept under `~/.claude` is refused with it.

*Instead:* say what you would change there and why, and stop. The person
changes their own Claude Code configuration, in Claude Code's settings or by
hand.

It holds only in a project that takes part. A session in a project that does
not is held to nothing, so it can still write all of this, and every project on
the machine then runs what it wrote. That is the residual a person accepted
for this rule; [SECURITY.md](../SECURITY.md) says more.

### `write-outside-repository`

Refuses a write that resolves outside the repository, symlinks followed.

It also refuses a write into another checkout of this repository nested inside
the one the session is held in: a story's worktree under `.sdlc/worktrees/`,
written from a session held in the main checkout, by its path or through a
link. A file there is that checkout's work, not this story's, and its freeze and
loop state are that checkout's too. The refusal names the checkout the file is
in.

*Instead:* work inside the repository; if you need a file elsewhere, ask. A
file in another checkout of this repository belongs to that checkout's story:
work in the checkout this session holds, and leave that one to the session
holding its story.

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

### `backlog-is-not-edited`

Refuses any write to the backlog file, wherever
[`backlog.path`](configuration.md#backlogpath) puts it. A backlog linked in from
elsewhere is protected under the name the configuration gives it.

The backlog holds the acceptance criteria the tests are held to and each story's
`risk_tier`, which decides whether a person approves the commit. An assistant
that lowered a story to `low` mid-iteration would take that pause away. The
`sdlc` command still records a story's status there as the loop moves it.

*Instead:* if a story's criteria or risk tier are wrong, say which and stop.
Change the backlog by hand outside a running iteration.

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
session lifts the freeze with `/sdlc:unfreeze`, or `sdlc unfreeze --reason "..."`, which puts the
change on the record.

### `no-new-test-after-the-freeze`

Refuses a new test file once the tests are frozen, unless
`freeze.allow_new_test_files` is on.

*Instead:* say which case is missing and stop. The frozen files are frozen too,
so the case cannot go into one of them either: the person running the session
can unfreeze and freeze again so the new test is covered. With the setting on,
run `sdlc freeze` after adding the file, or `tests_frozen`, `plan`,
`implementation`, `verification` and `commit` refuse with `SDLC-E0043`. If the
file is a scratch file outside the repository, write its path out in full in a
plain command.

A shell command that would create a new test file — a redirect, `touch`, `cp` —
is refused
by the same rule, and so is naming one in a command that runs `sed`, `awk`,
`perl` or another interpreter other than plainly as the file it is given to
read. Removing a test file the freeze does not hold is not: that is what
`SDLC-E0043` asks for. Nor is a path the hook places outside the repository,
which is no test of this project's, whatever its name: a scratch copy is
changed with a plain command, as
[A path outside the repository](#a-path-outside-the-repository) sets out. Where
the rule refuses a path written out in full outside the repository that the
hook did not place there, the refusal says so: it names the word, the
repository it was measured against, and what stopped the hook placing it. Where
it refuses a word the hook could not read as a path, such as one that starts
with a variable, the refusal says that the hook could not tell where the word
leads. A relative word read in the repository gets neither, and nor does a glob
that is the pattern a `find` looks for.

"New" is what the freeze does not hold, not what was written recently, so a
test file that has been there for years reports here too. A frozen test can be
reported here rather than as a frozen test, depending on how it was named; what
to do is the same either way.

### `implementer-does-not-write-tests`

Refuses the implementer writing any test file, frozen or not, before the freeze
or after it, through the file tools or through the shell, and, where `sed`,
`awk`, `perl` or another interpreter runs in the command, naming one other than
plainly as the file it is given to read.

Reading one is not: `sed -n 1,5p internal/x_test.go` names it plainly as the
file `sed` reads. `cat internal/x_test.go | sed -n 1,5p` names it somewhere
else, and is refused.

Through the shell, a path the hook places outside the repository is no test of
this project's and is not refused: the implementer changes a scratch copy with
a plain command, as
[A path outside the repository](#a-path-outside-the-repository) sets out. The
file tools refuse the same path, for being outside. Where the rule refuses,
through the shell, a path written out in full outside the repository that the
hook did not place there, the refusal says so: it names the word, the
repository it was measured against, and what stopped the hook placing it. Where
it refuses a word the hook could not read as a path, such as one that starts
with a variable, the refusal says that the hook could not tell where the word
leads. A relative word read in the repository gets neither, and nor does a glob
that is the pattern a `find` looks for.

This is not a setting: `freeze.allow_new_test_files` lets the test author add
one, never the implementer.

*Instead:* make the existing tests pass; if they are wrong, say so rather than
changing them. If the file is a scratch file outside the repository, write its
path out in full in a plain command.

After the freeze, the rules above answer first: a frozen file is refused as
`frozen-test-is-not-edited` and a new one as `no-new-test-after-the-freeze`, so
this is the rule named only before the freeze, or once the setting lets a new
file past.

### `sdet-writes-tests-only`

Refuses the test author writing anything that is not a test, its own story
directory, or `CODEMAP.md`.

*Instead:* write the tests. If this project keeps fixtures somewhere the loop
does not recognise, say which directory and stop: the person running the session
adds it to `paths.tests`, outside the iteration.

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

Refuses the main conversation writing code or tests during an iteration, and
any subagent that is not one of the loop's own — Claude Code's general-purpose
agent, or one of yours. Code or tests here is every file outside `.sdlc/` and
`CODEMAP.md`, so a page of documentation is refused as well.

The gates exist so that each is done by an agent that cannot see the others'
reasoning. The conversation doing the work itself is the failure the loop exists
to prevent, and it is reached without breaking a single stated rule unless
something refuses it. Handing the work to a subagent the loop does not know is
the same failure one step removed.

*Instead:* delegate the story's work to the agent whose gate it is. A file that
is not the story's work, such as a note for the person or a commit message,
goes under the story's own directory, `.sdlc/stories/<ID>/`, where the file tools
may write it; the loop's own records there are still written only by `sdlc`.
If neither fits, say what the work is and stop: ending the iteration to do the
work outside the loop is the person's decision.

The directory is the one in the checkout the session works in. A story with a
worktree has two: the one in the main checkout holds the gate documents, and a
session in the worktree may not write there
([`write-outside-repository`](#write-outside-repository)); the one in the
worktree is the one the route means.

Keep such a file out of the commit and remove it before the commit gate is
recorded. `sdlc init` does not ignore `.sdlc/stories/`, so in a project that has
not chosen to, a file left there is committed with the story, and one left
uncommitted counts as uncommitted work at the commit gate
([SDLC-E0032](troubleshooting.md#sdlc-e0032)). A commit message is read by the
commit itself: leave it out of what is staged, commit with it, and remove it
before the gate is recorded.

## Rules on shell commands

Every rule above governs the file-writing tools, and two of them —
`no-new-test-after-the-freeze` and `implementer-does-not-write-tests` — hold
against a shell command too. The rest would leave
`cat > .sdlc/stories/A-1/ANALYSIS.md` walking past them. The rules below close
that, and nothing else about your shell is touched: your tests, your build and
your tooling run exactly as before.

They apply to every tool that runs a command: `Bash`, `PowerShell` and
`Monitor`.

This is pattern matching, not a shell. It is deliberately narrow.

If a refusal sent you here, go straight to the rule it named. The next section
says how a command is read before any rule judges it, and you may skip it until
you need to know why a particular spelling was, or was not, read.

If [`sdlc doctor`](commands.md#sdlc-doctor) sent you here for a command in
`.sdlc/config.json`, it asked the same classifier about that command as the main
conversation would run it from the project root, with no story under way. It
does not know the agent's role or a story's state, so a rule that turns on them
can be named for a command the hook allows (a `git commit` under
[`commit-gate`](#commit-gate) is allowed once the gates have passed) and missed
for one it refuses. Change the command so that it does not do what the rule
refuses, by hand, in your own terminal.

### How a command is read

#### Wrappers the rules read through

The rules read past what only wraps a command:

- `( … )`, `{ … }` and a leading `!`, and the shell's own `if`, `then`, `else`,
  `elif`, `do`, `while`, `until` and `time`;
- `env` with the values its `-u`, `-C` and `-P` take;
- `sudo`, `doas`, `command`, `builtin`, `exec`, `timeout`, `stdbuf`,
  `caffeinate`, `nice`, `nohup`, `setsid`, `ionice`, `chrt`, `unbuffer`,
  `taskset`, `chronic`, `ts`, `firejail`, `systemd-run`, `unshare`, `nsenter`,
  `direnv exec`, `busybox` and `toybox`;
- `sh -c`, and `bash`, `zsh`, `dash`, `ksh`, `pwsh` and `powershell` the same
  way, `cmd /c`, `ssh` with the command after the host, and `xargs`;
- `env -S`, `flock` and `script` with the command line each is given;
- `watch` and `parallel`, which hand their words to a shell, their words read
  as one command line, with `parallel`'s `:::` and the arguments after it among
  them ([not held](#not-held));
- as well as a command sent to the background with `&` or run with
  PowerShell's `&`.

A program that runs another and is not on this list is read as itself, not for
what it runs ([not held](#not-held)), with two exceptions:

- The command `find` runs with `-exec`, `-execdir`, `-ok` or `-okdir` is read
  by the rules on commits, on git and on `sdlc`, so
  `find . -name x -exec git commit -m x ';'` meets `commit-gate`.
- `sdlc` is found where it is a later word behind a program not on the list, so
  `strace sdlc approve A-1` and `docker run img sdlc unfreeze --reason "..."`
  are refused as `sdlc approve` and `sdlc unfreeze` are. `git` is not found
  that way: `strace git commit -m x` is not read as a commit.

#### PowerShell spellings

The rules know PowerShell's cmdlets and their aliases, a parameter given as
`-Path:file`, and a backtick used as an escape.

#### Where a path points

The rules read a relative path from where the command runs: after a `cd` earlier in
the same command, where an earlier call left the session, or where `env -C`,
`sudo -D`, `systemd-run --working-directory` or `unshare -w` runs it, as a `cd`
in front of it would.

They also read a path as the file it is on disk, so a link to `.sdlc`, or a
Windows short name such as `SDLC~1`, is the file it reaches.

#### Here-documents

The body of a here-document is text, not commands, so a plan or a review that
mentions `git commit` is not refused for it — unless the text goes to a shell
or an interpreter, which runs it.

PowerShell's here-string, `@'` to `'@` or `@"` to `"@`, is a document too, but
only in a `PowerShell` command and only when it is closed; the lines after one
that is never closed are read as commands. In a `Bash` or `Monitor` command
`@'` is an `@` and an ordinary quote, and the quotes are read as quotes: what
is inside them is text, and what is outside them is commands.

#### Quotes and escapes

Which program a command runs is read with its quoting, so a commit message, a
`--note` or a `--message` that mentions `sdlc approve` or `git commit` is text,
not a command.

Quotes and escapes are read as the tool's shell reads them: `$'…'` with its
escapes decoded, a backslash before an ordinary character dropped, so
`g\it com\mit` and `git $'commit'` are `git commit`, and an escaped quote, as
in `'it'\''s'`, a quote inside the word rather than the start of a string.

#### A path that is only named

The rules that protect a path look for it as a word of the command, in quotes
or out of them, so that a path hidden in a string is still found. Three things
are read as naming a path and not acting on it, each only where the hook is
sure of it:

- **The text of an sdlc flag.** The value of `--message`, `--note`, `--reason`
  or `--reject`, when all of this holds: `sdlc` is run as `sdlc`, as a command
  of its own; nothing but `cd` and `sdlc` runs before it on the line; the flag
  is written in full, before any `--`; the text is one pair of quotes straight
  after it, with no variable, substitution or backslash inside; and what the
  command prints stays on the terminal or is piped only to programs that only
  read (`cat`, `head`, `tail`, `grep`, `rg`, `wc`, `ls`, `stat`, `file`, `du`,
  `diff`, `cmp`, `basename`, `dirname`, `realpath`, `cksum`, `md5sum`,
  `sha1sum`, `sha256sum` and `shasum`), up to eight of them, none writing to a
  file. `sdlc`
  prints the text it is given, and nothing tells a program that prints what it
  reads from one that runs it, so a text piped to anything else, `jq`, `tee`,
  `awk` and `python3` included, is not read this way.
- **A path a git pathspec leaves out.** The path in `':(exclude)PATH'`,
  `':!PATH'` or `':^PATH'`, with `top`, `icase` or `glob` beside `exclude` if
  they are there, when all of this holds: `git` is run as `git` with its
  subcommand as the next word; the subcommand is `diff`, `log`, `show`,
  `status`, `ls-files`, `grep`, `diff-tree`, `diff-index` or `diff-files`,
  without `--no-index`; nothing but `cd` and `sdlc` runs before it; the
  pathspec comes after `--`; the path is made of letters, digits and
  `._/*?-` and holds no `..`; and `GIT_LITERAL_PATHSPECS` is not set for the
  session. Those nine are the git commands the hook knows to take what
  follows `--` as pathspecs, to do nothing with them but read, and to print
  what they find and not the words they were given. Piped to a program that
  is not one of the readers above, the pipe must be a plain `|` with no
  redirect, so that an error that quotes the pathspec does not reach that
  program. Under every other git command the word is read as it always was:
  `git mv` and `git rm` take it as a file's name, and
  `git mv -f -- other ':!a/../PATH'` moves `other` over `PATH`.
- **A request for help.** `sdlc <command> --help` or `-h`, read as `sdlc`
  itself reads it, when `sdlc` would print help and do nothing else; see
  [a person's decisions](#a-persons-decisions-refused-in-every-session). The
  one exception is `sdlc remap --help`, which is refused as the command is;
  `sdlc help remap` prints the same text and is not.

No mention is read on a line that also hands text to something that runs it
(a shell, `eval`, `xargs`, `source`), or that holds a loop, a condition or a
program named by a variable. Nor on a line written with a backslash, `$'…'`, a
comment, a here-document, a brace or a parenthesis outside quotes: the hook
reads those lines as it always did. So it does in PowerShell a line that holds
a backtick, a character outside ASCII, a doubled quote inside a string, or a
`%`, `@` or `,` outside quotes, which covers the stop-parsing token `--%`,
after which PowerShell no longer reads a quote as a quote. What PowerShell
makes of a line was taken from its documentation and not checked against a
running PowerShell.

Nor is one read where a later command on the line could be given it. The text
stays on the line, and a shell hands the words of one command to the next: in
`sdlc escalate x --message 'rm PATH'; perl -e 'system(@ARGV)' "$_"` the `$_`
is that text. So a mention is read only when no command after it has a word
the shell fills in when the command runs (a variable, a history word such as
`!$`, a pattern, `~`), none runs an earlier line again (`fc`, `history`), and
none runs a program written on the line (`python3 -c`, `perl -e`, `node -e`,
`awk`, `sed` and their like). For a pathspec, the programs git's own listing
is piped to may run a program of their own, since reading the listing is what
the pathspec is for; they may not hold a word the shell fills in, and no other
command on the line may do either. The words of a text that is read as a
mention stay where a rule that reads the whole line finds them; what goes is
only the `;`, `>` and `|` inside the quotes, which the shell never saw.

Reading a path as only named can let a command through and never refuses one.
A command that is still refused is refused under the rule that always named
it. Where the refusal is for a path, and the path may only have been named in
one of the ways above, the reason ends by saying that the hook cannot tell
whether the command acts on it or only names it, and why: for example

```text
... is the loop's own record, and a shell command is the one way around every
rule that protects it; the hook cannot tell whether the command acts on it or
only names it, because what the command prints is piped to a program the hook
does not know to be one that only reads, which could run it
```

and the way on starts with how to write the command so that the hook can tell,
before the rule's own.

One thing this leaves open is for you to weigh: `git diff -- . ':(exclude)PATH'`
piped to a program that runs what it reads is allowed, as the same line is with
any other path left out or with none. What reaches that program is the diff, so
the act would have to be in a file's changes already.

#### A command inside a string

A string handed to `bash -c`, `cmd /c`, PowerShell's `-Command` or `eval`, and
a command inside `$(…)` or backticks, is read for the commands it runs, with
the rules of the tool the command came from: a string for a shell of another
kind, such as `pwsh -Command` in a `Bash` command or `bash -c` in a
`PowerShell` one, is read as the tool's own shell would read it, not as that
shell does ([not held](#not-held)).

#### A commit in another repository

A commit in another repository, after a `cd` or with `git -C` or `--git-dir`,
is not held to the story's gates. A commit with `GIT_DIR` set is, and so is one
in a repository inside the project, such as a submodule; where `GIT_DIR` names
another checkout of this repository, it is refused as a commit there
([`commit-in-another-checkout`](#commit-in-another-checkout)).

### A path outside the repository

The loop governs the repository and nothing else. Outside the repository means
outside the checkout the session is held in and outside every other checkout of
the same repository. The file tools refuse a path there, under
[`write-outside-repository`](#write-outside-repository), because a file the
loop cannot review is not one it lets a tool write. A shell command is yours to
run, so a path the hook can place outside the repository is not governed by
the shell rules, whatever its name: a copy of a frozen test in a scratch
directory is not a frozen test.

Placing a path is the exception, and the hook does it only on grounds a
spelling cannot fake. All three have to hold: the command is plain, the
directory it runs in is known, and the disk agrees.

#### A plain command

A command is plain, on this page and in a refusal, when the hook reads the
whole of it and knows what every word is for. It is made of the commands in
this table and no others, each with the options shown and no others, alone or
joined by `&&` and nothing else:

| Command | Plain as |
| --- | --- |
| `rm` | `rm FILE...` or `rm -f FILE...` |
| `cp` | `cp FILE... TO`, `cp -p FILE... TO` or `cp -t DIRECTORY FILE...` |
| `mv` | `mv FILE... TO`, and only as the whole command, joined to no other |
| `touch` | `touch FILE...` |
| `tee` | `tee FILE...` |
| `sed` | `sed -i s/x/y/ FILE...`: one substitution ending in `/` or `/g`, and no `w` or `W` in the first file's path |
| `echo`, `printf` | any words |
| `find` | `find DIRECTORY... -name GLOB -delete`, or with `-path` for `-name` |
| `cd` | `cd DIRECTORY`: one directory, with no option, glob or `..` |

The output of any of them may go to a file with `>` or `>>`, set apart by a
space from the word before it. A word may be in single or double quotes. The
command holds nothing the shell would fill in or act on: no variable, `$(...)`,
backtick, `~`, brace list or `!`; no `;`, pipe, `||`, single `&`, `<`,
here-document or subshell; and no character outside printable ASCII. It holds
no backslash, in quotes or out of them: the rules that judge by name read one
as a separator, and the shell reads one in single quotes as a letter of the
name. Inside quotes it holds no space, none of `;`, `&`, `|`, `<`, `>`, `)` or
a backtick, and no quote of the other kind: the rules that judge by name read
each of those as if it were not quoted, and where the two readings of a command
differ the hook places nothing. A `PowerShell` command is never plain.

Anything else in the command — another program, another option, `rm -r`,
`cp -a` — could make a link or move the shell, and the hook does not guess
which. One such word anywhere in a command stops the hook placing any path in
it, and the whole command is judged by name, as every command was before the
hook placed any path.

#### A known directory

The directory is the one Claude Code says the session is in, moved only by a
plain `cd`. A relative `cd` is followed only where the session carries no
`CDPATH`.

#### The disk agrees

There is no link on the way to the path, no directory on the way that is a
checkout of the repository under another name, and no second name for the file,
which a hard link gives it. For `find`, no checkout is below where it starts
either.

#### A word the hook cannot place

A word the hook cannot place is judged by its name, as that file in the
repository: `rm "$S/internal/x_test.go"` is refused as the frozen test
`internal/x_test.go` wherever `$S` leads, because the hook does not run the
shell. That is what "judged by name" means on this page. The way on is to write
the path out in full, with no variable, glob, brace list or `..` in it, in a
plain command.

Judged by name is not the same as
[a path that is only named](#a-path-that-is-only-named), which is a path a
command mentions and does not act on: in the text of an sdlc flag, in a
pathspec that leaves it out, or beside a request for help. Neither `sdlc` nor
`git` is in the table above, so a command with such a mention in it is not
plain, and every other path in it is judged by name. Where the hook is not
sure of the mention, a refusal can say both things of one path: what stopped
the hook placing it, and then that the hook cannot tell whether the command
acts on it or only names it.

For the three rules that protect tests, a glob in the last name of what `rm`
removes is placed by the directory before it, as in
`rm -f /private/tmp/scratch/*_test.go`. A glob anywhere else, or with a `/`
after it, is judged by name.

A plain `cp` or `mv` is read by where each file lands. A file copied or moved
into the repository from outside is the file it becomes there, whatever the
destination is called, for the rules on tests, on loop state and on protected
paths. For the three rules that protect tests, a `cp` of a test to a path
placed outside the repository only reads the test, and is not refused. Where
the destination is not written out in full, as in `cp x_test.go ../scratch/`,
the hook cannot tell where a file lands, and each file is judged by name.

#### Known limits

Each of these errs towards refusing:

- A path through a link is not placed, even where the link stays outside the
  repository. On macOS `/tmp` and `/var` are links, so `/tmp/...` and anything
  under `$TMPDIR` is not placed: write `/private/tmp/...`.
- A file with a hard link is not placed.
- A `find` over more than 10,000 files and directories is not placed, and
  neither is one that starts at a directory that is not there.
- On Windows only a path on a lettered drive is placed, and none with a name
  that ends in a dot or a space; no `PowerShell` command is plain.
- A command with a space inside quotes is not plain, so a path with a space in
  it is never placed, and `echo "two words" > /private/tmp/scratch/x_test.go`
  is judged by name: `printf two-words > /private/tmp/scratch/x_test.go` is
  plain.
- A command with a backslash in it is not plain, so
  `printf 'a\nb\n' > /private/tmp/scratch/x_test.go` is judged by name: write
  each line with an `echo` of its own, joined by `&&`.
- On macOS, `sed -i` wants a suffix after it, `sed -i '' s/x/y/ FILE`, and that
  spelling is not plain.

### `loop-state-through-the-tool`

Refuses a command that would write or delete the loop's own files: `.sdlc/state`,
`.sdlc/config.json`, `.sdlc/claude-progress.json`, any story's
`gate-record.json`, any `reviews/` directory, or any gate document, and `.sdlc`,
`.sdlc/stories` or a story's directory taken whole — by redirection; through
`rm`, `mv`, `cp`, `tee`, `dd`, `sed -i`, `find -delete`, `git rm`,
`git checkout` and their like; or from
inside a program handed to an interpreter.

A relative name is read from the directory the session is in. Where the hook
places that directory outside the repository, `.sdlc/state/active` there is
that directory's file and is not refused. Neither is a `find` that starts only
in directories placed outside the repository, whatever name it looks for:
`find /private/tmp/scratch -name active -delete` removes no file of the loop's.
Written through a variable, a glob or a link, or in a command that is not
plain, the path is judged by name; see
[A path outside the repository](#a-path-outside-the-repository).

It also refuses taking away a checkout of this repository, or a directory that
holds one: the session's own, the main checkout, or another story's worktree,
each with the story and the loop state it holds.

*Instead:* `sdlc artifact write`, `sdlc review add`, `sdlc gate`. The freeze is
lifted by the person running the session. For `.sdlc/config.json` there is no
command: the person running the session changes it by hand, outside a running
iteration. Remove only what the story made, by naming it inside the checkout,
such as `rm -rf build`; a checkout is removed by a person, in their own
terminal.

A refused `find` has a way on of its own:

- For a `find` with a glob the hook cannot read, spell the names out, or use a
  glob the hook can read, in single quotes, with no variable and no backslash
  the shell would act on.
- For a `find` whose operators leave a protected file in, run one `find` for
  each filter.
- For one whose expression the hook cannot follow, the refusal names the cause
  and the way on for it: write the word out in single quotes, correct or
  replace the option, run the `find` as a command of its own and not inside a
  script, put the directories first and take the comment off, or run one `find`
  for each filter.

The rest of this entry is the fine detail: what is left alone, how a `find` is
read, and what counts as taking a checkout away.

#### What is left alone

A scratch file inside a story's directory is not loop state. Reading the loop's
files with `cat`, `grep`, `head` or the file tools is never refused; `sed`,
`awk`, `perl` and interpreters are refused on them whatever they are asked to
do, because each can write through its own program.

A path that is only named, in the text of an sdlc flag, in a pathspec that
leaves it out, or beside a request for help, is not refused where the hook is
sure of it; [a path that is only named](#a-path-that-is-only-named) says when
that is.

#### How `find` globs are read

The globs `find` is given with `-name`, `-iname`, `-path`, `-ipath`,
`-wholename` and `-iwholename` are read as `find` reads them:

- a `]` first in a bracket expression is one of its members, as in `[]x]`;
- `[!x]` and `[^x]` exclude;
- ranges and POSIX names such as `[[:digit:]]` work;
- and a backslash escapes the next character.

A character in a bracket expression matches in either case. A range between two
lower-case letters, two upper-case letters or two digits holds what lies
between them by code point or by letter, in either case, and any character
beyond ASCII; a range between any other two characters, such as `[{-g]`, holds
any one character, because locales put those in different orders. `[!...]` and
`[^...]` leave out only what all of these readings hold.

#### A glob the hook cannot read

A glob the hook cannot read is refused when the `find` goes on to delete, run a
writer, or write its list to a file, from a directory that is, or is above, the
one holding the loop's files: `.`, `..` or a directory above them, or one the
hook cannot place, such as `"$ROOT"`, a glob, a path outside the project, or a
directory reached by a `cd` it could not follow.

A glob the hook cannot read is:

- one the shell changes before find sees it: it fills in a variable, such as
  `"$G"`, or removes a backslash;
- one that a quote or white space splits;
- or one that find reads in a way the hook does not, such as a bracket
  expression the hook does not know, or a glob longer than 512 bytes.

The refusal names the glob and the directory. A `find` that only reads what it
finds, such as with `-exec cat`, is not refused, whatever its glob.

#### How `find` operators are read

Between its tests `find` has operators, and the hook reads them: `!` and
`-not` negate, `-o` and `-or` give alternatives, `-a` and `-and` join, and
parentheses group — each in the quoted and escaped spellings a shell leaves
for find, such as `\!`, `'('` and `-not`.

A negated test is read as leaving every file in, never as the files its name
does not pick, so a `find` that negates a name and then deletes is read as
deleting every file the name does not pick, the loop's own among them. Each
alternative of an `-o` is read on its own, and one that picks a protected file
refuses the whole command.

The refusal then says that the operators leave the file in, and the way on is
one `find` for each filter: `find . -name '*.orig' -delete`, and then another
for the next name.

This reading is asked of a `find` that deletes or runs a writer; a `find` that
only writes its list to a file, with `-fprint` and its like, is judged by its
glob alone, not by what its operators pick.

#### Which words belong to the `find`

The `find` is read as find is handed it. A redirect is no word of it, with a
descriptor number or without, wherever on the line it stands. A separator
inside a quoted word, such as the `;` of `-name 'a;b'`, is a character of the
word and does not end the `find`.

A `find` given to a shell as a script, with `sh -c` or `bash -c`, or to `eval`,
is the same `find`, read through the one pair of quotes the script is written
in. Each command of a string given to `eval` is read as the command it is.

#### An expression the hook cannot follow

Where the expression is one the hook cannot follow, the `find` is refused from
the same directories as an unreadable glob, when it deletes, runs a writer, or
writes its list to a file. The refusal names the word and the directory, and
its way on depends on the cause:

- A word the shell fills in or unescapes where an operator could be, such as
  `"$OP"` between two tests. Write the word out in single quotes, or run one
  `find` for each filter. A value the shell fills in, such as the reference of
  `-newer "$REF"`, is a value and not a place an operator could be, and is not
  refused.
- An option the hook does not know: it cannot tell how many of the words after
  it are the option's own. If it is a slip, correct it; if your `find` has such
  an option, pick the files with options the hook knows, or list them with a
  `find` that only prints and name the ones to change.
- A `find` inside something the hook cannot take apart: a script inside a
  script; a script given to `eval` in more than one word, which `eval` parses a
  second time; a script whose quote closes before the end of its command; a
  script in double quotes that holds a backslash before a quote or before
  another backslash; an escaped separator written against another word; a
  process substitution among the `find`'s words; or a quote that is never
  closed, or is still open where the `find` starts, which is what an
  apostrophe in a comment on the line leaves. Run the
  `find` as a command of its own, or write the script as one word in single
  quotes; where a quote is never closed, close it, or take the comment that
  holds it off the command.
- A word that is neither a test nor an operator where only one of those can
  stand. That is a directory after the first test, a comment after the `find`,
  or a separator handed to `find` as a word of its own. Put every directory
  before the first test, and take the comment off the command.
- The two ways the hook reads the command do not agree, or it cannot tell
  where the `find` starts. Run the `find` as a command of its own, with each
  word written out plainly.
- The shape of the expression itself: a comma, parentheses that do not
  balance, an operator with nothing to govern, or more than 512 words. Run one
  `find` for each filter.

Some ordinary commands are over-refused in this way and have a plain way on,
named with the refusal:

- a `-depth` given a number (use `-maxdepth` or `-mindepth`),
- a leading `-s` or `-x` before the directories (write the directories out),
- a leading `-E` (list the files with a `find` that only prints, and name the
  ones to change),
- an option after a variable in an `-exec` command (put the option first),
- and a `find` that deletes with a comment after it on the same line (put the
  comment on a line of its own, without an apostrophe).

#### When the hook cannot tell where a `find` ends

Where the hook cannot take the `find` apart, or its two readings do not agree,
it does not know where the `find` ends either. Such a `find` is judged with
every word from its start to the end of the command, as if they were all its
own. That refuses more than the `find` alone would be refused for, and in each
case the way on is to run the `find` as a command of its own, on a line with
nothing after it:

- Such a `find` that only reads is refused, from a directory that holds or
  could hold a protected file, where a later word on the line is an action of
  `find` that changes files: `-delete`, an `-exec` of anything but a reader, or
  `-fprint` and its like. The refusal says that the later word is the cause,
  and that the hook cannot tell where the `find` ends.
- Such a `find` that changes files is judged as if every later word on the
  line were a file it writes. It is refused, wherever it starts, where a later
  command on the line so much as names a protected file, one of the loop's own
  files or a frozen test, and the refusal is then about that file.
- Where the two readings do not agree, the same holds for a word anywhere on
  the line, before the `find` as well as after it.
- A command may hold only so many such finds. Past that bound each one is
  refused, wherever it starts and whatever it does, a `find` that only reads
  included. Split the line into separate commands and run each `find` as a
  command of its own.

#### Taking away a checkout

Taking a checkout away is:

- removing or moving it with `rm`, `rmdir`, `mv` and their PowerShell and `cmd`
  twins,
- moving it to the trash with `trash`, `trash-put` or `gio trash`, or away with
  `gio move` or `gio rename`,
- the destination of `rsync` with `--delete`,
- what `find` could remove with `-delete` or `-exec rm`,
- a remover at the end of a pipe such as `xargs rm`,
- and a remover in code handed to an interpreter.

The checkout can be named any way the shell has for it:

- by its path, `.`, `$PWD`, `~+` or `$(pwd)`, and `$OLDPWD` or `~-` for where
  the shell was before its last `cd`;
- in PowerShell, `(Get-Location)`;
- `..` and the directories above;
- and a glob or a brace list that could match it, such as `rm -rf ../*` from a
  story's worktree.

A glob is refused for what it could match, which is a guess, so the refusal
names the checkout it could reach. A `-name` or `-path` glob the hook cannot
read counts as matching here, so a `find` that deletes with a mistyped bracket
expression can be refused for the checkout it could reach. The operators are
read here too, so a `find` that starts at a checkout, or above one, and whose
negation or alternation leaves the checkout in, is refused for it.

`rsync --delete` into a checkout, or a directory holding one, is refused
because it removes there whatever the source lacks.

#### Removals that take no checkout away, and three refused all the same

`rm -rf ./build/*`, `rm -rf node_modules`, `find . -name '*.pyc' -delete` and,
from a story's worktree, `find .. -empty -delete`, which removes nothing that
holds a checkout, take away no checkout and are not refused.

Three that take away none are refused all the same:

- `rm -rf build/.*`, because `.*` is read as matching `..`, as the refusal
  says;
- PowerShell's `[IO.Directory]::Delete((Join-Path $PWD 'build'), $true)`,
  because `$PWD` names the checkout;
- and a `find` that deletes all it finds, with no name to look for, from `.` at
  a checkout's root or from `..` just below it, as
  `find . -type d -empty -delete` there does, whatever else it tests, because
  it is read as taking that checkout away with the loop's record in it. For
  this `find`, at your own checkout's root the refusal names `.sdlc/state` and
  gives the loop-state reason; where the checkout is another it names that
  checkout and gives the take-away reason.

Either way, and for all three, the way round is to name the directory itself,
as in `rm -rf build` or `find build -type d -empty -delete`.

### `protected-path-through-the-tool`

Refuses a command that would write or delete `CLAUDE.md`, anything under
`.claude`, anything under `.git`, or the backlog — the paths
`write-protected-path` and `backlog-is-not-edited` protect from the file tools
that are not the loop's own state. It matches the way the filesystem does, so
`claude.md` is `CLAUDE.md` on macOS and Windows. A `CLAUDE.md` in a directory
below the root, such as `sub/CLAUDE.md`, is refused by this rule too.

A settings file under `.claude` is where hooks are configured, `CLAUDE.md` is
the contract every gate reads, and the backlog is the story itself, so a shell
command that rewrote any of them would take the rules with it. Reading them with
`cat`, `grep`, `head` or the file tools is never refused, and neither is
`.gitignore` or `.github/`. As with loop state, `sed`, `awk`, `perl` and
interpreters are refused on them, though not for naming a whole directory, as
`python3 -m pytest --ignore .sdlc` does. A path outside the project, such as
your own `~/.claude`, is not the project's and is not protected.

*Instead:* change them by hand outside a running iteration. If what one says is
wrong, stop and say what.

### `frozen-test-through-the-tool`

Refuses a command that would write, rewrite or delete a frozen acceptance test:
by redirection; through `rm`, `mv`, `cp`, `tee`, `sed -i`, `find -delete`,
`git checkout` and their like; or from inside a program handed to `sed`, `awk`,
`perl` or another interpreter, on the command line or on standard input.

Before the freeze this rule does nothing at all.

A path the hook places outside the repository is not a frozen test, whatever
its name, and neither is what a `find` comes to that starts only in directories
placed outside it;
[A path outside the repository](#a-path-outside-the-repository) says when a
path is placed. Where the rule refuses a path it did not place, the refusal
names the word, or where the `find` starts, the repository it was measured
against, and what stopped the hook placing it.

*Instead:* leave it alone. If it genuinely has to change, say which test and
why; the person running the session lifts the freeze with
`/sdlc:unfreeze`, or `sdlc unfreeze --reason "..."`. If the file is a copy outside the repository,
write its path out in full in a plain command.

A refused `find` has a way on of its own:

- For a `find` with a glob the hook cannot read, spell the names out, or use a
  glob the hook can read, in single quotes, with no variable and no backslash
  the shell would act on.
- For one whose operators leave a frozen test in, run one `find` for each
  filter.
- For one whose expression the hook cannot follow, take the way on the refusal
  gives for its cause, as listed under
  [`loop-state-through-the-tool`](#loop-state-through-the-tool).

#### A program given somewhere other than the first argument

A `sed`, `awk` or `gawk` command given its program somewhere other than its
first argument — with `-f`, `--file`, or, for `awk` as for `gawk`, gawk's
`--source`/`-e` and `--exec`/`-E` — leaves nothing to say
which of its files is data, so every file it names is treated as one the
program could write.

#### Reading a frozen test

Reading one with `cat`, `grep`, `head` or the file tools is never refused where
`sed`, `awk`, `gawk`, `perl` or another interpreter is not in the command too,
and reading the tests is how the implementer knows what to implement. Those are
narrower, because each writes through a program of its own that this cannot
read.

A frozen test named plainly as the file one of them is given to read —
`sed -n 1,5p internal/x_test.go` — is allowed, unless that command was given its
program somewhere other than its first argument, when every file it names is
treated as one the program could write.

Otherwise the name is looked for as a word of the command, not as the program
would work it out, anywhere on a command line one of these tools runs on, and
refused there: inside the program text, in quotes, or in another part of a
pipeline or list. A `;`, `&&`, `||` or `|` inside the program counts as another
part of the list, so `sed -n '1,5p;10,20p' internal/x_test.go` is refused too.

The flags above (`-f`, `--file`, `--source`/`-e` and `--exec`/`-E`) are looked
for in any word starting with `-`, wherever it stands and program text
included, so
`awk '$1 == "-f" {print $2}' internal/x_test.go` is refused as well.

A frozen test named in the quoted text of `--message`, `--note`, `--reason` or
`--reject`, or left out by a git pathspec, is not refused where the hook is
sure that is all it is: see
[a path that is only named](#a-path-that-is-only-named).

#### How `find` globs and operators are read

The globs `find` is given are read as under
[`loop-state-through-the-tool`](#loop-state-through-the-tool). A glob the hook
cannot read is refused when the `find` goes on to delete, run a writer, or write
its list to a file, from a directory that holds a frozen acceptance test or
could: one with a frozen test below it, `.`, `..` or a directory above them, or
one the hook cannot place. The refusal names the glob, the directory, and one
frozen test the directory holds or could hold.

A `find` that only reads what it finds is not refused, whatever its glob.

The operators of a `find` that deletes or runs a writer are read as under
[`loop-state-through-the-tool`](#loop-state-through-the-tool), and an
expression the hook cannot follow is refused the same way, naming the word and
the directory.

#### A patch names its files where the rule cannot see them

A patch applied with `git apply` or `patch` names its files inside the patch,
where this cannot see them. `sdlc gate` still refuses a frozen test that
changed, with `SDLC-E0025`.

### `unfreeze-is-a-human-decision`

Refuses `sdlc unfreeze` run from a tool call, where `sdlc` is written out as
the program: by name on `PATH`, by path, or through `npx` or `go run`. Reached
through a variable, a command substitution or a copy under another name, it is
[not held](#not-held). In a session the story is not held in, it is refused
while a story is under way or waiting for a person.

Lifting the freeze is sometimes right, and it is also exactly the move an agent
would make to reach green. So it is a person's decision: they run it in their
own terminal, where the hook is not asked. A note that only mentions the word,
such as `--note "no need to unfreeze"`, is not refused.

*Instead:* say which frozen test is wrong and which acceptance criterion it gets
wrong, and stop there. The person running the session lifts the freeze with `/sdlc:unfreeze`, or
`sdlc unfreeze --reason "..."` in their own terminal.

### `approval-is-a-human-decision`

Refuses `sdlc approve` run from a tool call, where `sdlc` is written out as
the program: by name on `PATH`, by path, or through `npx` or `go run`. Reached
through a variable, a command substitution or a copy under another name, it is
[not held](#not-held). In a session the story is not held in, it is refused
while a story is under way or waiting for a person, and not in a project with
neither.

An approval is a person's answer to a question the loop stopped to ask them:
`sdlc escalate` hands the story over and ends the iteration. An agent that could
give the answer would be approving its own work. A reviewer's `approve` verdict,
given with `sdlc review add`, is a different command and is not refused.

*Instead:* `sdlc escalate <type> --message "..."`, and stop. The person reads the
work and runs `/sdlc:approve`, or `sdlc approve` in their own terminal.

### `acknowledgement-is-a-human-decision`

Refuses `sdlc ack` run from a tool call, where `sdlc` is written out as the
program (by name on `PATH`, by path, or through `npx` or `go run`), in any
session, in any project with `.sdlc/config.json` the call reaches (the
session's, the working directory's, or one a path in the command names),
whether or not a story is being worked on.

`sdlc log` starts after the commit acknowledged, so acknowledging one takes every
story before it off the list a person reads. An agent that could run it would be
deciding which stories nobody needs to look at. `sdlc log` itself is not refused.

The log of what landed on trunk is always there for a person to read, so there
is always a decision here to keep, between stories and in a session that never
started one as much as during a story.

*Instead:* say what `sdlc log` lists, and stop. The person reads it and runs
`/sdlc:ack`, or `sdlc ack --through <commit>` in their own terminal.

### `remap-is-a-human-decision`

Refuses `sdlc remap` run from a tool call, where `sdlc` is written out as the
program (by name on `PATH`, by path, or through `npx` or `go run`), in any
session, in any project with `.sdlc/config.json` the call reaches, whether or
not a story is being worked on. The rule reads the subcommand and nothing
after it: no story id, no flag and no state of the project makes the command a
session's.

`sdlc remap` puts a landed story's kept change into trunk's `CODEMAP.md`
([the loop](the-loop.md#parallel-stories-and-codemapmd)). The text is a
bookkeeper's and no reviewer read it; `--replace` puts it over what trunk
holds, which may be another story's or a person's, and `--drop` throws it
away. An agent that could run it would be deciding what trunk's map says.

Its help is refused with it. For every other command a person keeps,
`sdlc <command> --help` is let through because a test holds that spelling to
printing help and doing nothing; none holds it for this one, so the spelling is
read as the command it names. `sdlc help remap` is not refused, and
[the commands page](commands.md#sdlc-remap-story) has the same text.

The hook refuses a command only where it can read it, and [Not held](#not-held)
lists the spellings it cannot: each is as open for this command as for
`sdlc ack`. The change itself
is kept in the repository's git directory, at `.git/sdlc/codemap/<ID>.md`,
which the file and shell rules protect only from a session that holds a story,
and then only where they can place the path: one built by a substitution, such
as `$(git rev-parse --git-common-dir)/sdlc/codemap/<ID>.md`, is not placed, so
a session that holds a story can remove or rewrite another story's kept file
that way. It is the limit every protected path has.
A session that holds none can remove or rewrite that file, as it can commit to
trunk; the story's `gate-record.json` has a `codemap` event for every change
`sdlc` itself stored, applied or dropped, and none for one made that way.

*Instead:* say what the stop or `sdlc status` printed, and stop. The person runs
`/sdlc:remap <story>`, or `sdlc remap <story>` in their own terminal.

### `release-is-a-human-decision`

Refuses `sdlc release` run from a tool call, where `sdlc` is written out as the
program (by name on `PATH`, by path, or through `npx` or `go run`), in any
session, in any project with `.sdlc/config.json` the call reaches, whether or
not a story is being worked on.

Releasing a story takes it from the checkout of the repository that holds it,
after which any checkout can start it. An agent that could run it could take a
story another checkout is working, or let go of the one it is held to and with
it every rule holding it.

*Instead:* say which checkout holds the story and stop. A person runs
`/sdlc:release <ID> --reason "..."`, or `sdlc release <ID> --reason "..."` in their own terminal
if it is to move.

### `parallel-is-a-human-decision`

Refuses `sdlc parallel` run from a tool call, where `sdlc` is written out as
the program (by name on `PATH`, by path, or through `npx` or `go run`), in any
session, in any project with `.sdlc/config.json` the call reaches, whether or
not a story is being worked on.

`sdlc parallel` starts a Claude Code session for each story named, and with
`--unattended` it hands each of them a list of tools to use without asking. An
agent that could run it would be starting model sessions, and choosing what they
may do, that nobody asked for. See [unattended sessions](#unattended-sessions).

*Instead:* say which stories you meant, and stop. A person runs
`sdlc parallel <ID>...` in their own terminal, outside Claude Code.

### `stop-is-a-human-decision`

Refuses `sdlc stop` run from a tool call while the story still has a gate to
pass, where `sdlc` is written out as the program: by name on `PATH`, by path,
or through `npx` or `go run`.

Nearly every rule on this page holds only while a story is being worked on, so ending
the iteration part-way is the way round all of them at once: a commit no gate
passed goes through straight after. Ending a story whose gates have all passed
is the runbook's last step, and is not refused.

*Instead:* work the next gate, or hand the story to a person with
`sdlc escalate <type> --message "..."` and stop. The person ends the iteration
with `sdlc stop` in their own terminal.

### `review-is-recorded-by-its-reviewer`

Refuses `sdlc review add <gate> <role> <verdict>` run by anyone but the agent
it names: the main conversation, a subagent outside the loop, another reviewer,
or the implementer recording an approval of its own work.

Every reviewer records its own verdict, and the gate passes on those verdicts.
A review recorded by someone else is a review nobody did. A person recording one
in their own terminal is not asked.

*Instead:* delegate to the reviewer, which reads the work and records its own
verdict.

### `assignment-is-a-human-decision`

Refuses `sdlc staffing assign` run from a tool call, in any session, in any
project with `.sdlc/config.json` the call reaches, whether or not a story is
being worked on and whether the staffing is on or off.

What `sdlc staffing assign` records says a person chose a model or an effort for
an agent, and the project manager cannot change it afterwards. A session that
could run it could pin every agent where it liked, in a person's name.

*Instead:* say what you would have asked for, and stop. A person types
`/sdlc:assign` in Claude Code, or runs `sdlc staffing assign` in their own terminal, outside
Claude Code.
`sdlc staffing --help` lists the commands.

#### How the word after `sdlc staffing` is read

The word after `sdlc staffing` is read against the three the hook knows: `show`,
`set` and `amend`. Any other word is refused, and so is one the shell makes when
the command runs, since it could be `assign`. `sdlc staffing` with no word after
it is refused unless it asks for help.

#### A subcommand that cannot be read

The same rule refuses a run of `sdlc` whose subcommand cannot be read, because
it could be this command or another only a person runs. That is a run where the
word after `sdlc` comes from a variable, a substitution or a pattern, and a run
that is handed its words on standard input by `xargs` or GNU `parallel`. It
applies to every `sdlc` command, not only the staffing's, and the refusal says
which of the two it met.

*Instead:* write the subcommand out as a literal word, `sdlc status` and not a
variable that holds `status`; and pass the words on the command line.

Where `sdlc` is not the program a command runs but a later word of it, as in a
copy of a built binary or a `cd` into a directory of that name, none of this
applies: only a written-out `staffing` followed by a verb other than the three
is refused there, and so is a `staffing` command handed its words on standard
input.

### `slash-command-is-a-human-decision`

Refuses running the sdlc hook by hand on Claude Code's `UserPromptExpansion`
event, or on an event the hook cannot read, from a tool call: in any session,
in any project with `.sdlc/config.json` the call reaches, whether or not a
story is being worked on.

On that event the hook is being run as if a person had typed a slash command.
It runs the command the payload on its standard input names, such as
`/sdlc:approve`, as that person's decision. The hook cannot tell a run Claude
Code made from one a model made with a payload it wrote, so a session that
could run it could approve its own work, lift the freeze or acknowledge the log
in a person's name.

It refuses four kinds of command:

- `sdlc` itself running `hook`, by name, by path, through `npx` or `go run`,
  or behind a wrapper the rules read through, on the event written out or on
  one it cannot read: `sdlc hook UserPromptExpansion < p.json`. Through
  `Start-Process`, its argument list is read as PowerShell passes it, a list
  or one string alike, so
  `Start-Process sdlc -ArgumentList 'hook UserPromptExpansion'` is refused.
- The plugin's launcher, `sdlc-hook` or `sdlc-hook.cmd`, by name or by any
  path, run as the program, as a script under `sh` or `bash`, or through
  `cmd /c` or `Start-Process`, on the same events:
  `"$CLAUDE_PLUGIN_ROOT/bin/sdlc-hook" UserPromptExpansion < p.json`.
- A program whose name cannot be read, because it comes from a variable, a
  substitution or a pattern, given `hook` and then one of those events:
  `"$(which sdlc)" hook "$E" < p.json`.
- Any program handed the event written out, as its first word or after a word
  `hook`, which is how a copy of `sdlc` under another name is run:
  `/tmp/x hook UserPromptExpansion < p.json`.

The event's name is read in any case. An event cannot be read when the shell
makes it as the command runs, from a variable, a substitution (PowerShell's
`$( )` included, and one a script run under `sh` is given, as in
`sh plugin/bin/sdlc-hook "$(echo UserPromptExpansion)"`), a pattern or a `%`,
or when `xargs` or GNU `parallel` hands it
over on standard input. Every event Claude Code has is named in letters alone, so
any other word is read that way. The refusal says which of the two it met: the
event written out, or one it cannot read.

Where `sdlc` or the launcher is only a later word of a command, behind a
program the hook does not know, only the event written out is refused. So
`git add plugin/bin/sdlc-hook`, `chmod +x plugin/bin/sdlc-hook` and
`cp plugin/bin/sdlc-hook "$D/"` are not refused.

A command that an older rule refuses first keeps that rule's name. So
`echo UserPromptExpansion | xargs sdlc hook` is refused under
[`assignment-is-a-human-decision`](#assignment-is-a-human-decision), and a
project manager's or a reviewer's run of the hook may be refused under
[`project-manager-only-staffs`](#project-manager-only-staffs) or
[`review-is-recorded-by-its-reviewer`](#review-is-recorded-by-its-reviewer).

*Instead:* say what you wanted decided and stop; a person types
`/sdlc:approve <ID>`, or `sdlc approve <ID>` in their own terminal, and likewise
`/sdlc:unfreeze`, `/sdlc:ack`, `/sdlc:remap`, `/sdlc:release` and
`/sdlc:assign`; to try the hook yourself, run it on another event written out
as a literal word, such as `sdlc hook PreToolUse`.

Not refused: the hook run by hand on any other event, as in
`sdlc hook PreToolUse < p.json`, which is how a reviewer drives it; and a line
that only names the event, such as `grep -n UserPromptExpansion
plugin/hooks/hooks.json`, a commit message or a document's text.

Not caught by this rule, and caught after it, each in [Not held](#not-held).
Code handed to an interpreter that runs the hook itself, as `python3 -c` with
`subprocess.run` does, or a script file that runs it, reaches the hook, which
answers that the command did not run and records nothing, since the process
has `AI_AGENT`. PowerShell's `Invoke-Expression` or `iex`, given the hook's
command line as a string, reaches the hook with `AI_AGENT` too, and the hook
answers that the command did not run. A copy of `sdlc` or of the launcher
under another name, given an event this rule cannot read, as in
`/tmp/x hook "$E"`, reaches the hook with `AI_AGENT`, and the hook answers that
the command did not run. An event given in PowerShell as an expression in
parentheses, as in `sdlc hook ('User'+'PromptExpansion')`, which is read as a
command of its own, reaches the hook with `AI_AGENT`, and the hook answers that
the command did not run. A session in a project that does not take part,
running the hook on a payload whose `cwd` names one that does, runs it with
`AI_AGENT`, and the hook answers that the command did not run. The launcher
sourced with `.` or `source`, given an event this rule cannot read, gets past
this rule too. Run that way, as in `. plugin/bin/sdlc-hook "$E" < p.json`, it
reaches the hook with `AI_AGENT`, and the hook's check of that variable is
what stops it: the hook answers that the command did not run. A program Claude
Code starts itself, such as an MCP server or a hook named in the project's
settings, has an environment of its own that may not have `AI_AGENT`, and a
payload it feeds the hook is not caught (the project MCP server or hook in Not
held). A slash command another program types into the person's own prompt
reaches Claude Code's own hook as the person's typing would (in Not held too).

### `staffing-is-recorded-by-the-project-manager`

Refuses `sdlc staffing set` and `sdlc staffing amend` in the session working a
story, from anyone but the `sdlc:project-manager` agent: the main conversation,
a subagent outside the loop, and every agent the staffing is about.

The staffing says who takes part in a story and what each is asked for. The
session that runs the loop is the one a smaller staffing saves work for, and the
agents are the ones it leaves out, so none of them writes it. Where no story is
being worked on the hook does not ask; the commands themselves refuse, because
they write only the story being worked on.

*Instead:* delegate to `sdlc:project-manager` with the story and what happened.

### `project-manager-only-staffs`

Refuses the `sdlc:project-manager` agent every run of `sdlc` but the ones it is
for: `sdlc staffing show`, `sdlc staffing set`, `sdlc staffing amend`, the
commands that only read (`sdlc status`, `sdlc review list`, `sdlc story list`,
`sdlc artifact list`, `sdlc log`, `sdlc cost`, `sdlc doctor`, `sdlc version`),
and any command asked for its help. It is a list of what it may run, so a
command this version does not have, and a subcommand that cannot be read, are
refused too. Help is read as `sdlc` reads it: a help flag turned off by a later
`--help=false`, or beside a variable or a substitution that could turn it off,
is not a request for help.

The project manager decides who takes part. An agent that could also record a
gate or a review, or hand the story to a person, would be deciding the story.

*Instead:* record or amend the staffing, and say anything else in your result.
Handing a story to a person is for the main conversation running the runbook.

### `commit-gate`

Refuses `git commit` until every gate before the commit gate has passed. The
refusal names the first one that has not.

Once they have, it also refuses a commit of work that has changed since the
verifier and the code reviewers approved it. Their reviews are stamped with the
working tree as it was, outside `.sdlc/` and without the `status` and `updated`
fields `sdlc` writes into the backlog, and a change anywhere else makes them
stale; the refusal names whose. Measuring the tree takes a moment, so it is done
only for `git commit`.

A story whose risk tier is in `human_gates.pre_commit_pause_tiers` is also
refused until a person has approved that same tree with `sdlc approve`. A story
missing from the backlog is refused too, because its tier cannot be known.

The hook still lets through a commit amended once the commit gate has passed:
the tree is the approved one. For a story in its own worktree in such a tier
the landing is what holds it to the commits the gate recorded, and `sdlc stop`
refuses another ([`SDLC-E0037`](troubleshooting.md#sdlc-e0037)).

*Instead:* finish the gates — `sdlc status` shows where the story stands.
Committing without them is for the person running the session to decide:
`sdlc stop`, which turns this rule off along with the iteration, is refused from
a tool call until they have passed
([`stop-is-a-human-decision`](#stop-is-a-human-decision)).

#### Commands that make commits of their own

The git commands that make commits of their own are refused the same way:
`merge`, `cherry-pick`, `revert`, `am`, `rebase` and `commit-tree`, apart from
`--abort` or `--quit` on its own, and a `--no-commit`, `merge --squash` or
`cherry-pick -n` that leaves the change in the working tree for `git commit`.

So is an alias to one of them, set with `git -c`, `git config`, `--config-env`
or the `GIT_CONFIG_*` variables, in the command or in the environment the
session started with; an alias in your own git configuration file is not read,
and one the hook cannot read at all is refused
([`git-command-cannot-be-read`](#git-command-cannot-be-read)).

A commit made in a way the hook cannot read is refused at the next gate
instead, which will not pass once HEAD has moved since the story started
([`SDLC-E0046`](troubleshooting.md#sdlc-e0046)). Once the gates before the
commit have passed, a rebase is
[`branch-through-the-tool`](#branch-through-the-tool)'s to judge instead.

#### A gate record that cannot be read

A gate record that is missing or will not parse counts as no gate passed, and
the refusal names the file. `sdlc start` always writes one, so either means
something damaged it. `sdlc stop`, run by a person, still ends the iteration
when the record cannot be read.

#### The note of a commit let through

A commit this rule lets through is also written down: the HEAD it is made on,
in the repository's git directory at `<git-dir>/sdlc/commits/<story>/`, so that
the commit gate can tell the story's commits from anybody else's.

Only a command that commits now is written down: not one that sets an alias,
rehearses with `--dry-run` or leaves the change uncommitted. Nor is one whose repository the
command's words do not identify as this one, such as after `cd "$HOME/other"`,
with `git -C "$DIR"` or with `GIT_DIR` set, or in a repository nested inside
the project: this rule holds such a commit back in case it is this
repository's, and writing it down on the same guess would record this
repository's HEAD for a commit made elsewhere.

The note is written only for the session working the story, never refuses anything
itself, and is never committed or pushed. The commit gate believes it, so it is
a claim the hook saw a commit coming rather than proof;
[Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md#ranges-to-read-with-care)
says what that leaves open.

### `commit-in-another-checkout`

Refuses a commit, or a command that makes one as `commit-gate` counts them, in
[another checkout](#a-repository-with-more-than-one-checkout) of the
repository: with `git -C`, `cd`, `env -C` or
`sudo -D` into the main checkout or another linked worktree, with `--git-dir`
naming the main checkout's `.git` or another worktree's git directory, or with
`--work-tree` naming another checkout. It refuses whatever the story's gates
say.

The story's gates cover only the checkout it is held in: what they reviewed is
not what would be committed in another, and that the story may commit says
nothing about it. `--git-dir` naming the session's own worktree's git
directory is its own checkout, and meets `commit-gate` as a plain `git commit`
does, and so does `GIT_DIR`, `GIT_WORK_TREE` or `GIT_INDEX_FILE` naming its own.

*Instead:* commit from the checkout the story is held in.

#### Other ways git is told where to commit

The rule reads the same places when git is given them in the ways listed here,
and only in those ways. Each is read with the rules of the tool the command
came from, so a string handed to a shell of another kind is not read for that
shell's own ways
([not held](#not-held)). `GIT_DIR` names the
repository, `GIT_WORK_TREE` the work tree and `GIT_INDEX_FILE` the index, set
anywhere earlier in the command:

- as an assignment, in front of the command or on its own, read as handed on
  to git whether or not it is exported; or with `export`, `declare`,
  `typeset`, `local`, `readonly` or `env`;
- with `printf -v NAME`, with or without `--` after the name;
- with `read` from a here-string (`<<<`); what `read` takes from anywhere else
  is not in the command, so cannot be placed;
- in a `Bash` or `Monitor` command, with `${NAME:=…}` or `${NAME=…}`, read as
  setting it whether or not it was set before, and inside single quotes too;
- in a `PowerShell` command, with `$env:NAME =` or `${env:NAME} =`, with `+=`
  for either, with `[Environment]::SetEnvironmentVariable`, or with
  `Set-Item`, `New-Item`, `Set-Content` or `Add-Content`, or their aliases
  `si`, `ni`, `sc` and `ac`, on a path that starts with `Env:`.

  Their parameters are read in any order, each named after a `-` in full, or
  cut short to a part that names it alone, with its value after a space, a `:`
  or both; a switch the cmdlet takes, by that name, given bare or with its
  value joined to the `:`; one path, quoted or not, given with `-Path`, with
  `-LiteralPath` or its aliases `-PSPath` and `-LP`, or first; `New-Item`'s
  `-Name` under `Env:` itself; and the value given with `-Value`, or next.

  `Add-Content` and `+=` are read as setting what they add. A value that comes
  down the pipeline is not in the command, so cannot be placed.

`core.worktree` names the work tree, given with `git -c`, `--config-env` or the
`GIT_CONFIG_*` variables. The same variables in the environment the session
started with count too. An index in another checkout's git directory, such as
the main checkout's `.git/index`, is that checkout's.

#### When the hook cannot tell where git is pointed

A place that cannot be placed, given through a variable the shell has yet to
expand or in one of the ways above, is refused as though it were another
checkout. So is a commit later in the same command than a `git add`, `rm`,
`mv`, `restore`, `reset` or other index write run with another checkout's work
tree or index.

### `worktree-through-the-tool`

Refuses `git worktree` for every action but `list`: `add`, `remove`, `move`,
`prune`, `lock`, `unlock`, `repair`, and an action the hook cannot read,
however git is reached, an alias included, in any checkout. `git worktree list`,
`git worktree -h`, `git help worktree` and `man git-worktree` are not refused.

`git worktree` makes, moves or takes away a checkout of this repository, and
each story is held in
[the checkout git lists for it](#a-repository-with-more-than-one-checkout): its
claim names that
checkout's path. Moving or removing a worktree ends the hold on the story in it,
pruning forgets one, and adding one can put a checkout where a story's worktree
is about to go.

*Instead:* `sdlc start` makes a story's worktree; a person adds, removes, prunes
or repairs one in their own terminal. To read another branch, use
`git show <branch>:<path>` or `git diff <branch>`.

### `worktree-setting-through-the-tool`

Refuses `git config` changing `core.worktree` or `core.bare`, in any scope,
whether it sets, adds, replaces or unsets them, and removing or renaming the
`core` section. Reading them is not refused.

Changing either changes which directory a checkout of this repository works in,
and it can move the main checkout's work tree out from under it, and with it
every story that checkout holds.

*Instead:* a person changes `core.worktree` and `core.bare` in their own
terminal. If git says this is not a work tree, stop and tell them.

### `branch-through-the-tool`

Refuses a command that moves trunk or a story branch other than by the story's
own gated commit, by what the command does to a branch rather than by which
branch it names, in the story's worktree and in any other checkout:

- `git update-ref` in every form, `-d` and `--stdin` included, and the plumbing
  that writes refs: `filter-branch`, `fast-import`, `send-pack`, `receive-pack`
  and `http-push`;
- `git branch` with `-f`, `--force`, `-d`, `-D`, `--delete`, `-m`, `-M`,
  `--move`, `-c`, `-C` or `--copy`, or an abbreviation git takes of one;
- `git checkout -B`, `git switch -C` or `--force-create`, and either with
  `--ignore-other-worktrees`;
- `git symbolic-ref` given a ref to write, `-m` or `-d`;
- `git push`, `git fetch`, `git pull` and `git remote update`;
- `git reset` naming a commit other than HEAD with no paths, in another
  checkout, or anywhere once the story's commit gate has passed;
- a rebase once the gates before the commit have passed: before the commit gate
  every one but `git rebase --abort` and `git rebase --quit`, which make no
  commit and move no branch wherever they are pointed, though an abort in
  another checkout throws away the rebase in progress there; after it every one
  but the plain rebase of the session's own worktree onto trunk and a bare
  `git rebase --abort` there.

*Instead:* leave branches to the loop. `sdlc stop` in a finished story's
worktree lands it on trunk, and a person moves, deletes, pushes or fetches a
branch in their own terminal. To read one, use `git log`, `git show` or
`git diff`.

#### The plain rebase onto trunk

The plain rebase names trunk by its full branch ref, `refs/heads/<trunk>`, or by
its short name where git reads that name as trunk's branch in the session's
checkout; a tag or another ref with trunk's short name makes it refused. It
takes `-q` or `--quiet` and nothing else: no other option, no `git -c`, alias,
`--git-dir` or `GIT_DIR`, and no `-C` but one into the session's own worktree.

`--continue`, `--skip` and `--quit` are refused after the commit gate:
resolving a conflict is a person's. The runbook no longer rebases at all:
`sdlc stop` puts a finished story onto a trunk that moved itself, and abandons
its own rebase where the commits do not apply. Trunk is the one
the main checkout's configuration names.

#### A git command in another repository

A git command the words place in another repository altogether, such as a
fixture in a temporary directory, is not this repository's branches, and is not
refused. One they cannot place is.

#### When another rule refuses the same command

This rule answers last: a command another rule refuses keeps that rule, and once a
branch move is found, the rest of the command is read only within a bound, past
which the refusal is this rule's.

### `git-command-cannot-be-read`

Refuses a git command whose subcommand, or an alias it defines or runs, the
hook cannot read, and one whose configuration comes from somewhere the hook
cannot read:

- a subcommand the shell has yet to expand, such as `git $CMD prune` or
  `git "$(echo worktree)" prune`; a brace list such as `git {worktree,} prune`
  is expanded first, and each word it makes is read, and one that makes more
  words than the rules read is refused;
- a program the shell has yet to expand is read as git, so `$G worktree prune`
  and `"$(command -v git)" commit` are judged as git commands; where the word
  after it has yet to be expanded too, as in `$X $Y`, that word would be git's
  subcommand, and cannot be read. That refuses commands that never run git:
  `$EDITOR "$FILE"`, `$CC $CFLAGS -o x x.c`, and
  `for f in ./t/*; do "$f" "$ARG"; done`, which runs each script in `t`, are
  refused, and the refusal says to name the program itself;
- aliases nested deeper than the rules read, or an alias that leads back to
  itself;
- `!` aliases whose shell command lines, counted each time one runs, come to
  more than the rules read in one command, 64 KB: an alias that runs another
  many times, a few levels deep, is refused rather than read for longer than the
  hook is given, and the refusal says to run what the aliases would run as
  commands of their own, a few at a time;
- an alias whose value cannot be read, wherever it is set: `-c alias.x=…`,
  `git config alias.x …`, `--config-env=alias.x=VAR` with no assignment to
  `VAR` in the command, or `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_<n>` and
  `GIT_CONFIG_VALUE_<n>`, or `GIT_CONFIG_PARAMETERS`, whose value the shell has
  yet to expand or that will not parse;
- an include, `include.path` or `includeIf.<condition>.path`, given in the
  command, or `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM` or `GIT_CONFIG` set in
  it to a file other than `/dev/null`; `git config` writing an include is
  refused too.

What the hook cannot read, it cannot judge, and every other git rule would let
it pass unread.

*Instead:* spell git's subcommand and any alias out in the command itself, with
no include or configuration file. Where the program is not git, name it, such
as `python3`, rather than a variable. Where aliases hand the shell more than the
rules read, run what they would run as commands of their own, a few at a time.

#### How an alias is read

The rules for git read what git would run. An alias set in the command is read
as its value, so `git -c alias.x='worktree remove --force' x .` is judged as
`git worktree remove`, an alias defined with `git config` is judged by what it
would run, and one whose value starts with `!` by the shell command it runs.

A git in that shell command runs with the settings of the git command around
it, which git hands on, so in
`git -c alias.a='!git b' -c alias.b='worktree prune' a` the `git b` is
`git worktree prune`.

An alias in your own git configuration, such as `git st`, is not read and not
refused ([not held](#not-held)).

### `enforcement-stays-on`

Refuses a command that sets `SDLC_ENFORCE`, `SDLC_BIN`, `CLAUDE_PROJECT_DIR` or
`CLAUDE_CODE_SESSION_ID`.

The switch that turns enforcement off belongs to the person who started the
session. A switch an assistant can reach is not a control.
`CLAUDE_CODE_SESSION_ID` is one: `sdlc start` records the session it finds
there as the one the story holds, so setting it hands the story to a session
that does not exist.

*Instead:* if a rule is wrong, say which one and why.

### `story-stays-in-its-session`

Refuses running `claude`, or running `@anthropic-ai/claude-code` through a
package runner such as `npx`, `npm exec` or `pnpm dlx`, from the session
working the story. Installing the package is not refused.

A Claude Code session started from a shell is a session of its own, and the
story holds only the session working it, so none of these rules would hold
what the new one did. The shell rules do not read a script file, so they
cannot tell what one runs. A script file that runs `claude` starts a session
whose hook has `AI_AGENT`, so a person's slash command expanded there answers
that it did not run, and records nothing. One that sets
`CLAUDE_CODE_SESSION_ID` and runs a person's command is refused by the command
itself, with SDLC-E0079.

*Instead:* hand the work to this session's agents. A person who wants another
session opens one in their own terminal.

In every session in a project that takes part, the rule also refuses starting a
Claude Code session that could expand one of the person's slash commands, such
as `/sdlc:approve`: a slash command typed by a model in a session of its own is
not a person's. It refuses:

- `claude`, or the package through a runner, given a prompt that names
  `/sdlc:approve`, `/sdlc:unfreeze`, `/sdlc:ack`, `/sdlc:remap`,
  `/sdlc:release` or `/sdlc:assign`, with or without the plugin's name in front;
- `claude` given a prompt the hook cannot read: one substituted with `$( )`,
  piped or redirected in, or written with a variable;
- `claude` run through a program that gives it a terminal to type into, such as
  `tmux`, `screen`, `zellij`, `expect`, `unbuffer` or `script`;
- a program that could be `claude` (a variable, a glob, the versioned binary
  under `~/.local/share/claude/versions/`, `Start-Process`) given a prompt that
  names one of those commands;
- inline code, such as `python3 -c` or `node -e`, whose words run `claude`, or
  that reads its program from standard input in a command that does: with
  `python3 -`, or with a bare `python3` or `node` given its program through a
  pipe, a here-document or a here-string;
- a line that copies or links a program, names `claude`, or its package, by
  name, and names one of those commands, such as
  `ln -s "$(which claude)" /tmp/x && /tmp/x -p "/sdlc:approve US-001"`: the
  copy starts under a name no rule knows, in the same call. That line is
  refused with the copy's own reason: "this line copies or links a program and
  names both Claude Code and a person's slash command, ...". The same line with
  `"$(command -v claude)"` is refused too, under the same rule, earlier and with
  the session's reason, "a Claude Code session started from here could expand a
  person's slash command, ...", because `command -v claude` is read as a run of
  `claude`.

The rule's name says "story", and in another session there may be none: it is
the same rule, about a session started from this one, and a later rename is the
clean fix. A session started with a prompt the hook can read and that names
none of those commands is not refused there.

Not caught by this rule, and caught after it. Each of these starts a Claude Code
session that keeps the environment of the model's tool that started it, and the
hook of a session started that way has `AI_AGENT`, so a person's slash command
expanded there answers that it did not run, and records nothing:

- A script file that starts `claude` starts a session whose hook has
  `AI_AGENT`.
- A prompt built inside a program by a means the hook does not read, such as
  one decoded from base64 by a program it does not recognise as inline code, or
  a program name built inside code (`"cl" + "aude"`), reaches a session whose
  hook has `AI_AGENT`.
- An alias, whether the person's shell profile defines it or the same line
  does, is not something this rule reads, and a shell run by a tool expands
  none unless `expand_aliases` is set. A session an alias starts has `AI_AGENT`
  in its hook all the same.
- An interpreter is read as taking its program from standard input only where
  no word after its options could be its script: an option that takes a value
  in the next word, other than the few the hook knows (`-W` and `-X` for
  Python, `-r`, `--require`, `--import` and `-C` for Node, `-I` for Perl, and
  the like), makes its value look like the script, as with
  `python3 --check-hash-based-pycs default`, `ruby --disable gems` or
  `node --title x`. A session such a program starts with `claude` has
  `AI_AGENT` in its hook.
- A link the copy above does not read as `claude`, one made from the versioned
  binary's path or from a glob such as `cla*`, and a copy made in one call and
  run in the next, each start a session whose hook has `AI_AGENT`.
- `arch -arm64 claude -p /sdlc:approve` and `su me -c` start a session whose
  hook has `AI_AGENT`.
- An editor or a pager that runs a shell, as in
  `vim -c '!claude -p /sdlc:approve' -c q`, starts a session whose hook has
  `AI_AGENT`.
- An interpreter's own call to the system, as in
  `awk 'BEGIN{system("claude -p /sdlc:approve")}'`, and AppleScript's
  `do shell script`, as in
  `osascript -e 'do shell script "claude -p /sdlc:approve"'`, start a session
  whose hook has `AI_AGENT`.
- A program a git setting runs, as in
  `git -c core.pager='claude -p /sdlc:approve' log`, and `GIT_EDITOR` set in
  front of `git commit` in a session that holds no story, start a session whose
  hook has `AI_AGENT`.
- Code handed to an interpreter that runs `sdlc` itself, as `python3 -c` does
  with `subprocess.run(["sdlc", "approve", ...])`, is refused by the command
  itself, with SDLC-E0079, and the hook run that way answers that the command
  did not run, since the process has `AI_AGENT`.

`launchctl submit -l x -- claude -p /sdlc:approve` starts `claude` with a
clean environment, with no `AI_AGENT`, and is not caught (in
[Not held](#not-held), with the other programs that start clean).

### Claude config through the shell

A command is refused under
[`claude-config-is-the-persons`](#claude-config-is-the-persons) when it writes
the person's Claude Code configuration:

- a redirect into it, or a command that changes a file there, such as `cp`,
  `mv`, `tee`, `sed -i`, `touch` or PowerShell's `Set-Content`;
- a recursive copy or move, or a removal, of a directory that holds it, such as
  `rm -rf ~` or `cp -r dotfiles ~`;
- a command that writes into a directory it is given, such as `tar -x -C`,
  `unzip -d`, `make -C` or a `git -C` that writes, and a `find` rooted there
  that changes what it finds;
- inline code, such as `python3 -c` or `node -e`, or a program an interpreter
  reads from standard input, that names it by name, since where a program
  writes is the program's to work out: `expanduser("~/.claude")` and
  `HOME + "/.claude.json"` are never a path the shell sees. Code that only
  reads it is refused too; `cat` reads it. A regular expression in the code,
  such as `.*`, is not a name;
- a copy, link or install into a directory that holds it, by the name it lands
  under: `cp x/.claude.json ~`, and an `ln -s` of it into the home spelled out
  in full, both write `~/.claude.json`, even where the home's name holds a dot;
- `xargs` fed from a pipe whose words name it;
- Claude Code's own commands that change it: `claude config set`, `add` and
  `remove` (or `rm`); `claude mcp add`, `add-json`, `add-from-claude-desktop`
  and `remove`; `claude plugin` (or `plugins`) `install` (or `i`), `uninstall`,
  `remove`, `enable`, `disable` and `update`; and `claude plugin marketplace
  add`, `remove` (or `rm`) and `update`. Their `--help` is not refused, nor is
  any other `claude` command: `claude install`, `update`, `setup-token` and
  `migrate-installer` change the program, not the configuration.

A path is placed as the shell would place it: `~`, `$HOME`, `${HOME}`,
`$env:HOME`, `$env:USERPROFILE`, `%USERPROFILE%` and `%HOMEDRIVE%%HOMEPATH%`,
`CLAUDE_CONFIG_DIR` in each of those forms, and a relative path from the
directory the command runs in. A word written to that cannot be placed, such as
a variable or a glob, is refused where the command names the configuration by
name, `.claude` or `.claude.json`, or by one of the two variables, or where the
word is itself a glob the shell expands, such as `.*`, that could match them. So
`cd ~ && rm -rf .*` and `rm -rf "$HOME"/.cla*` are refused, and `sed -i
's/.*//' "$FILE"` is not: its `.*` is in quotes, a script for sed and not a
name. A glob elsewhere on the line, such as one kept in a variable and used
later, is not read as a name. A script file that writes there is not read, like
any other.

A project that is itself inside `~/.claude` has every write refused, since every
file in it is part of the configuration.

### `command-too-deep-to-read`

Refuses a command that nests `$( )` or backticks more than 32 deep.

Every level is read for the commands it runs, and a line nested thousands deep
took longer to read than Claude Code gives the hook, which then lets the command
run unchecked. Nothing a person writes nests that deep. It applies to any
command: it is refused in any session, in any project with `.sdlc/config.json`
the call reaches (the session's, the working directory's, or one a path in the
command names), whether or not a story is being worked on, because `sdlc ack`
or an approval could be buried in it.

*Instead:* write it as separate commands.

## A repository with more than one checkout

Every checkout of a repository, the main checkout and each linked worktree made
with `git worktree add`, works one backlog and one record of each story, kept in
the main checkout. Its `.sdlc/state` is its own. A session is held in the
checkout it started its story in, and the rules follow it into the others.

The parts below say how a session is found from another checkout, what it is
refused there, what happens to branches, how a path is placed in a checkout,
what happens when the main checkout cannot be found, and, last, the gaps that
are known and [not held](#not-held).

### Finding the session

A call is judged as the session's wherever it runs. When no checkout the call
reaches holds the session, the hook asks the repository: first every checkout a
claim names, whatever their number, and then the first 64 linked worktrees, for
an iteration that was running when this release was installed and has no claim
yet.

The first `sdlc` command that writes there turns that into a claim, and
`sdlc doctor` says when a repository has more than 64 worktree entries, since a
session past the 64th is not found until then. Adding worktrees does not move a
checkout a claim names out of reach, and the claims are refused to a held
session wherever it names them.

### The other checkouts

From any checkout, a session held in one is refused, in every other checkout of
the repository, what it is refused in its own:

- writing the backlog, `.sdlc/` (the story records, the configuration and that
  checkout's own `.sdlc/state`), `.git/` (the claims included), `.claude/` and
  `CLAUDE.md`, by an absolute path, a link, `..` through a link, `~`, `cd`,
  `find`, `xargs` or code handed to an interpreter;
- discarding what that checkout holds, with `git stash`, `git reset --hard`,
  `--merge` or `--keep`, a forced `checkout` or `switch`, `git read-tree -u`,
  or a clean, restore or checkout of the whole tree, however git is pointed
  there;
- taking the checkout away, or a directory that holds it
  ([`loop-state-through-the-tool`](#loop-state-through-the-tool));
- and committing there, however git is pointed at it
  ([`commit-in-another-checkout`](#commit-in-another-checkout)).

Every `git worktree` action but `list` is refused in any checkout
([`worktree-through-the-tool`](#worktree-through-the-tool)), and so is changing
`core.worktree` or `core.bare`
([`worktree-setting-through-the-tool`](#worktree-setting-through-the-tool)).

Reading anything, and writing with the shell files no rule protects, are not
refused. The file tools are refused everything outside the checkout the session
is held in, as they always were, and every other checkout nested inside it,
such as a story's worktree under `.sdlc/worktrees/` seen from the main checkout
([`write-outside-repository`](#write-outside-repository)).

### Branches

While a session holds a story, the hook refuses it any command
that moves trunk or a story branch, the story's own or another story's, other
than by the story's own gated commit, from the story's worktree and from the
main checkout alike ([`branch-through-the-tool`](#branch-through-the-tool)). By
class, it refuses:

- a ref written directly, with `git update-ref` (`--stdin` included) or the
  other plumbing that writes refs;
- a branch forced, deleted, renamed or copied with `git branch`;
- a checkout or switch that resets a branch that exists (`-B`, `-C`,
  `--force-create`), or checks out one another worktree has
  (`--ignore-other-worktrees`);
- `git symbolic-ref` writing HEAD;
- `git push`, `git fetch`, `git pull` and `git remote update`, whatever they
  name;
- a `git reset` naming a commit in another checkout, and in the session's own
  once the story is committed;
- a rebase, but for the one below;
- a merge or any other commit in another checkout
  ([`commit-in-another-checkout`](#commit-in-another-checkout)), and
  `git worktree add` with a forced branch
  ([`worktree-through-the-tool`](#worktree-through-the-tool)).

Each is refused however git is spelled, aliased or pointed at the checkout, as
far as the rules read those, and nothing that only reads branches is. Refusing
every push and fetch while a story is held, one to another repository included,
is a choice: the words cannot always tell another repository from a path to one
of this repository's own checkouts, and the loop never pushes.

#### The two moves that are the session's

Two moves of the story's own branch are the session's.

- After the commit gate, the plain rebase of its worktree's branch onto trunk,
  `git rebase <trunk>`, named by trunk's branch or as `refs/heads/<trunk>` and
  with nothing but `--quiet`; and a bare `git rebase --abort` there. Nothing
  asks for that rebase any longer, since `sdlc stop` puts the story onto a trunk
  that moved itself. The hook still admits it for a session that runs one
  before it stops, and the abort for a rebase the session started; a story
  rebased that way is judged at the landing as one rebased by hand
  ([SDLC-E0029](troubleshooting.md#sdlc-e0029),
  [SDLC-E0037](troubleshooting.md#sdlc-e0037)).
- After the retro, one commit of the root `CODEMAP.md` alone. Nothing asks for
  it any longer, since the bookkeeper stores its sections with
  `sdlc codemap write` and `sdlc stop` puts them on trunk; the hook still admits
  it, and a story that makes one lands with it.

The landing checks what either did before trunk moves, and it holds the purpose
whatever the spelling: nothing lands that is not the story's reviewed change on
top of trunk, with no merge in it, nothing under `.sdlc/` or in the backlog,
and the root `CODEMAP.md` only as a plain file by exactly that name.

Those last checks are the landing's, so they hold only for stories worked in
their own worktree: with `git.worktrees` off, a story's commit can carry any of
them, as it always could.

### Which checkout a path is in

Which checkout a path is in is worked out from where it lands on disk, links
followed, by walking up to the nearest directory whose `.git` belongs to this
repository. A `.git` that leads elsewhere is walked past rather than stopped
at, so a repository made with `git init` inside a checkout cannot hide that
checkout's files.

The price is that an unrelated repository inside a checkout, such as vendored
code or a submodule never initialised, is under the `.git` name rule like any
other path in that checkout, and a commit made in it is refused as a commit in
that checkout.

A worktree nested inside the project, as a story's worktree under
`.sdlc/worktrees/` is inside the main checkout, counts as another checkout for
what it holds, to the shell and to the file tools. A linked worktree moved by
hand to somewhere outside the project is still a checkout of this repository,
even where git's own record of it names its old place.

On macOS a path spelled through `/System/Volumes/Data`, in any case, is the
path it names, where both spellings are the same file: a file tool's write to
the project spelled that way is a write to the project. Where they are not the
same file, as with the data volume's own root or a directory of the system
volume spelled through it, the path is read as written.

### Failing closed

When the main checkout cannot be found — a bare repository, `core.bare` set, a
rewritten `commondir` — the hook does not read the worktree's own copy of the
records. The commit is refused, every test counts as frozen, no story counts as
finished, and a person's decision counts as waiting.

Where not even git's directory can be read, every path outside the checkout that
might be the main checkout's evidence is refused by name, and the refusal says
to run `sdlc doctor`.

### Not held

These are known and not closed. Each says what could happen, and
how you would notice.

#### Branches and the landing

- **A branch moved by a spelling the rules do not read.** Every way below of
  getting a command past the rules unread gets a branch move past
  [`branch-through-the-tool`](#branch-through-the-tool) as well. Trunk moved
  that way holds work no gate passed: in the main checkout, `git status` shows
  changes nobody made there, the difference reversed, because the branch moved
  and its files did not, and `git reflog <trunk>` shows when. Another story's
  branch moved or deleted that way is reported by that story's next gate
  ([SDLC-E0046](troubleshooting.md#sdlc-e0046)). A story's own branch moved
  that way does not land: the landing refuses what is not the reviewed change
  on top of trunk ([SDLC-E0054](troubleshooting.md#sdlc-e0054),
  [SDLC-E0029](troubleshooting.md#sdlc-e0029)).
- **A reset of the session's own branch before the commit gate.** A soft or
  mixed reset, such as `git reset --soft HEAD~1` or `git reset HEAD~1`, is not
  refused. It is the session's own work, and the next gate names it
  ([SDLC-E0046](troubleshooting.md#sdlc-e0046)). A reset with `--hard`,
  `--merge` or `--keep` is refused, by
  [`loop-state-through-the-tool`](#loop-state-through-the-tool).
- **A commit after the commit gate that the commit rule allows.** The landing
  lands only the change that was reviewed, or one whose reviews are recorded
  again and whose commands pass, so a person sees
  [SDLC-E0029](troubleshooting.md#sdlc-e0029) at `sdlc stop`.
- **The root `CODEMAP.md` reaches trunk with no reviewer.** The sections the
  bookkeeper stores are put on trunk by `sdlc stop`, after the story's own
  commits and outside the range `sdlc log` prints, in a commit named
  `docs: map <ID>` that the story's record names as `codemap_commit`. `sdlc`
  makes that commit itself: it is not signed, no hook of the repository runs
  for it, and it changes that one path as a plain file or is not made
  ([SDLC-E0076](troubleshooting.md#sdlc-e0076)). Any agent of the story can
  store sections, at any gate, and the last set stored is the one applied; the
  record has a `codemap` event for each storing. A commit of `CODEMAP.md` on
  the story's own branch after the retro is still taken too, only as a plain
  file by exactly that name ([SDLC-E0055](troubleshooting.md#sdlc-e0055)); the
  hook's check of that commit answers not fresh for files beneath it, and
  wherever the tree cannot be measured with `CODEMAP.md` as that commit has it.
  Read both with `git show`.
- **Git settings that name a program.** The session can write settings such as
  `core.hooksPath`, `core.fsmonitor` or a filter in `.gitattributes`, and git
  runs the program they name during commands the rules allow, the bookkeeper's
  commit and the allowed rebase among them, and while the landing's
  fast-forward checks and writes the files of a checkout with trunk checked
  out: a clean filter runs as it hashes them, before anything is written. Such a
  program can do anything a branch move can. The landing refuses what it did to
  trunk's history, the loop's files and the change, and a person notices the
  program only by reading the repository's configuration with
  `git config --show-origin --list` and its `.gitattributes`; `sdlc` does not
  report it. While `sdlc stop` puts a story's commits onto trunk, every merge
  driver the configuration named when the stop read it, one with an empty name
  included, is replaced by one that stops as a conflict, and a merge driver whose
  name holds an equals sign, the text `{sdlc-private}` or a byte that is not
  UTF-8 makes a stop that has to put the story onto a trunk that moved refuse
  before git runs (`SDLC-E0055`, naming the key), commits are not signed, no
  editor runs, and no
  attributes file outside the repository is read; a filter the configuration
  names does run, as it does on any checkout, and an attribute in the
  repository's own `info/attributes` counts as one committed in
  `.gitattributes` does, `merge=union` among them. So does an attributes file
  in the worktree that `git status` does not show.
  Wherever the landing measures a story's change it sets every
  `diff.<name>.algorithm` the configuration holds to `myers`, one with an empty
  name included, and a diff driver whose name holds any of those three makes
  every stop refuse before the change is first measured, in the same words,
  whether or not trunk moved. A stop with trunk where the story left it replays
  nothing, so it does not refuse a merge driver. `sdlc` asks git for every key
  and chooses the drivers' itself, so the locale git runs in does not decide
  which are found. The commit gate measures the change the same way and does
  not apply the rule: it passes where git takes the setting, and fails with
  `SDLC-E0021` in git's words for a name git's own command line will not take.
- **Repository configuration that changes what the allowed rebase does to other
  refs.** The landing refuses a range that is not the story's alone. A person
  sees other refs move in `git reflog` for them.
- **A ref of another kind with trunk's short name, made earlier in the same
  command as the allowed rebase.** A tag named as trunk, made before the
  command, makes the rebase by trunk's short name refused; made in the same
  command, it is not seen. The landing refuses the result
  ([SDLC-E0054](troubleshooting.md#sdlc-e0054) or
  [SDLC-E0029](troubleshooting.md#sdlc-e0029)), and a person sees the ref in
  `git show-ref`.
- **The hold ending before the landing.** Escalating ends the iteration, and so
  does `sdlc stop`'s way out of a broken state. Until a person answers, the
  session holds nothing and none of this binds it. The escalation is on the
  record and in `sdlc status`, the way out prints its own line on standard
  error, and the landing still asks every check.
- **Ignored files in the story's worktree take part in the commands.**
  Workspace files, local environment files and caches the story's worktree
  ignores can make a command pass there that would fail on trunk, and nothing
  in the stop's output shows them. List them with
  `git -C .sdlc/worktrees/<ID> status --ignored` before trusting a pass.
- **A person's stop runs what the session planted.** A stop a person runs,
  after the 600 seconds the runbook gives it or after answering an escalation,
  runs the story's code and the repository's program-naming settings with that
  person's privileges and credentials. [SDLC-E0055](troubleshooting.md#sdlc-e0055),
  the runbook and [configuration](configuration.md#at-landing) say so before
  sending a person there.
- **Two identical stretches of context give one fingerprint.** The change the
  reviewers saw is compared with its hunk headers cut down to their marker, so a
  change that moved to an identical stretch elsewhere in a file compares equal.
  A person reading the landed range sees where the lines went. Where `sdlc stop`
  itself puts a story onto a trunk that moved, the result is compared with the
  reviewed change without the lines around it, from both sides: the story's
  change on the new trunk, and trunk's change on top of the story. A line moved
  among lines identical to it is the same file and is not seen. Nothing else is
  compared this way: a commit after the reviews and a rebase by hand are
  measured with their context, as before.
- **An ignored file is not part of any tree the loop measures.** One left in the
  worktree is there when the landing's commands run, and one at a path trunk has
  come to track is overwritten when the story is put onto trunk.
- **A frozen test put back by hand over trunk's change.** In a story's own
  worktree a test file that only trunk changed is trunk's, and one the story and
  trunk both changed is refused. A commit made by hand after a rebase that puts
  a frozen test trunk changed back to its frozen copy reads as an intact freeze,
  and takes trunk's change to that test away. The reviewers see the test in the
  diff when the change is reviewed again; the stop's own replay cannot make such
  a commit.
- **A commit of the story's own that no `sdlc gate` run has noted, once trunk's
  branch points at it.** A test file that only trunk changed is trunk's, and
  the story's commits are told from trunk's by a list in the story's record
  (`forks`). `sdlc start` begins the list; every `sdlc gate` run in the story's
  worktree adds to it, passed or refused, and so do a stop that puts the story
  onto trunk and a freeze that follows a person's unfreeze. A commit made on
  the story's branch since the last of those is on no list: one a person made by
  hand, one the session made, or one made again by an amend or a rebase. If
  trunk's branch then comes to point at it, by a fast-forward or any other
  move, before any `sdlc gate` run, the test files that commit added or changed
  read as trunk's and are accepted as trunk's at the next gate and at the stop.
  The plainest case leaves nothing to tell it by at all: a test file committed
  by hand directly on the trunk commit the story sits on, and trunk
  fast-forwarded to it before any sdlc command ran, is the same commits and
  branches as a story put onto a commit trunk gained. Three more things are
  not on the list: a commit that reaches the branch only through a merge's
  other parent, because an entry covers the commits that descend from the
  lowest one on the branch's own line; an entry a person edits out of the
  record; and an entry whose commit git no longer has, which is passed over.
  You would notice by the line the accepting command prints, `accepted as
  <trunk>'s`, and by `trunk_tests` in the story's record, which names the files
  and the trunk commit: a file there that the story wrote is this. The record
  names what was accepted by the latest gate that checked the freeze and was
  recorded as passed (`tests_frozen`, `plan`, `implementation`, `verification`
  or `commit`), or by the stop that landed the story. It is the tree as that
  gate saw it: the record keeps it through later gates that do not check the
  freeze, a gate that is refused changes nothing, and it is not checked again
  until the next gate that does. `trunk_tests` is empty if no such gate has accepted
  anything, if the latest one accepted nothing, if the commit gate was reopened
  by a command that accepted nothing itself, or if a person ran `sdlc unfreeze`.
  Emptying it writes no event, so an empty field with earlier `trunk_tests`
  events means one of these happened, not that nothing was ever accepted. Take
  the file out of the branch, or have a person unfreeze so that it is frozen in
  their sight.
  The list also refuses in three states where the files really are trunk's,
  each with [SDLC-E0043](troubleshooting.md#sdlc-e0043) or SDLC-E0025 naming
  the commit: a gate ran while the local trunk was behind a commit the branch
  already held, trunk was moved back and then forward again, or commits the
  branch held reached trunk by another story's landing. One unfreeze by a
  person and a freeze is the way out of each. A freeze taken after the stop
  itself lifted the last one, on a landed story reopened in a worktree that was
  kept, leaves the list as it was: a session can take that freeze alone, and it
  neither resets a list nor gives one to a record that had none.
- **A rebase a person runs is not sealed.** [SDLC-E0075](troubleshooting.md#sdlc-e0075)
  sends a person to rebase a story by hand. That rebase runs the hooks, merge
  drivers, editor and filters the configuration names, with that person's
  privileges; read the configuration first, as above.
- **A shallow repository** can only hide ancestry, and the landing then refuses
  with [SDLC-E0054](troubleshooting.md#sdlc-e0054). A commit it hides is one git
  does not have, so an entry of a story's list of its own commits that names it
  is passed over; the freeze still takes nothing as trunk's there while the
  commit the story was cut from is hidden too, as it is in a history that is
  one line.
- **Ignored files go with a landed story's worktree.** They are removed with it,
  and the stop's last line says so.

#### Commands the rules do not read

- **A call the hook is killed on before it knows whether it checks it.** The
  hook refuses a call it could not check in time only once it knows the call is
  one it checks
  ([`not-checked-in-time`](#not-checked-in-time)). A command that keeps the
  hook finding the project for the whole of Claude Code's 30 seconds runs with
  no rule applied and nothing said in the session. A person's decision run in
  such a call is refused by the command itself, with SDLC-E0079. With
  `SDLC_DEBUG_FILE` set, the log has no `hook decision` line for that call.
- **A commit in the next call.** Each call is read on its own. A call that
  stages another checkout's work into the session's index, with
  `git --work-tree=<main> add` or `GIT_INDEX_FILE`, followed by a plain
  `git commit` in a later call, puts that work in the story's commit. The commit
  gate then does not pass: the reviews no longer match the tree
  ([SDLC-E0029](troubleshooting.md#sdlc-e0029)), or the tree no longer matches
  the commit ([SDLC-E0032](troubleshooting.md#sdlc-e0032)), and
  `git show --stat HEAD` in the story's worktree lists files the story never
  touched.
- **What the persistent shell keeps.** The Bash tool keeps one shell from call
  to call, and the hook sees only the environment the session started with. A
  variable exported in an earlier call, such as `export GIT_DIR=<main>/.git`,
  or set by a file run with `source` or `.`, is not seen, so a later
  `git commit` can land on the branch another checkout has checked out, trunk
  included. `git log` there shows a commit no gate passed, and `git reflog`
  when it came. A person's decision run from that shell is refused by the
  command itself, with SDLC-E0079, whatever an earlier call exported, while
  Claude Code's markers are still set; a person's decision run after an
  earlier call unset or blanked those markers is not caught (the last entry
  of this list). The story's `gate-record.json` lists every approval and
  unfreeze with when it was made.
- **A program the rules do not read through.** The rules read past the
  wrappers [the shell rules](#wrappers-the-rules-read-through) list, and no others. A
  command that any other program runs, or that code handed to an interpreter
  runs as a command of its own, is not read for what it runs. A commit and the
  removal of a worktree or a checkout get past the rules that way, as in
  `strace git commit -m x`. A person's decision inside a string, code or a
  script that such a program runs, as in `fish -c 'sdlc approve A-1'` or
  `make approve`, is refused by the command itself, with SDLC-E0079.
  `strace sdlc approve A-1`, with `sdlc` written out as a word of the command,
  is refused by the shell rules before it runs, and by SDLC-E0079 had it run.
  `tmux new-session 'sdlc approve A-1'` is refused with SDLC-E0079 where it
  starts the `tmux` server, which then has the session's environment; where a
  server a person started is already running, it runs the command with an
  environment of its own, and a person's decision run that way is not caught
  (the last entry of this list). `git log` and `git reflog` show a commit no gate passed,
  `git worktree list` a worktree gone or prunable, and the story's
  `gate-record.json` every approval and unfreeze with when it was made.
- **A command line for another kind of shell.** A string handed to a shell of
  another kind than the tool's, such as `pwsh -Command` or `powershell -c` in
  a `Bash` command, or `bash -c` or `sh -c` in a `PowerShell` one, is read
  with the rules of the tool the command came from, not as that shell reads
  it. So PowerShell's ways of setting a variable are not read in a string
  handed to PowerShell from `Bash`, nor `${NAME:=…}` and `${NAME=…}` in one
  handed to a POSIX shell from `PowerShell`, and a `git commit` after them can
  land in another checkout as a plain commit, which only
  [`commit-gate`](#commit-gate) holds. Where that shell's quoting and escapes
  make a word into something other than the tool's would, the word is read as
  the tool's would make it, by every rule that turns on which program a
  command runs. A person's decision in such a string, as in
  `pwsh -Command 'sdlc approve A-1'` from `Bash`, is refused by the command
  itself, with SDLC-E0079. `git log` and `git reflog` in the other checkout show a commit
  no gate passed, and when it came; `git worktree list` shows a worktree gone,
  or marks it prunable; and the story's `gate-record.json` lists every
  approval and unfreeze with when it was made.
- **`parallel`'s own command lines.** GNU `parallel` runs its command once
  for each argument after `:::`, with the argument put after it, and runs
  each argument as a command line of its own where no command comes before
  `:::`. The rules read the command, `:::` and its arguments as one command
  line instead, so an argument run as a command line of its own is not read
  for what it runs. A commit, the removal of a worktree or a checkout, and a
  write to the loop's own record with a program such as `rm`, `cp` or `tee`
  can each get past that way. Two things in such an argument are refused all
  the same: a redirect onto a file a rule protects, as in
  `parallel ::: 'echo x > .sdlc/state/active'`, and `sdlc` written out, as in
  `parallel ::: 'sdlc approve A-1'`. `git log` and
  `git reflog` show a commit no gate passed, `git worktree list` a worktree
  gone or prunable, and `sdlc doctor` a loop record it can no longer read.
- **Other ways of setting git's place.** `GIT_DIR`, `GIT_WORK_TREE` and
  `GIT_INDEX_FILE` are read only where they are set in the ways
  [`commit-in-another-checkout`](#commit-in-another-checkout) lists. Set
  another way, as a `for` or `select` loop's variable, through a name
  reference (`declare -n`), with bash's `+=`, `mapfile`, `getopts` or
  arithmetic, with PowerShell's `Copy-Item` or `Rename-Item` on `Env:`, with
  a parameter PowerShell binds in a way the rules do not follow, or with a
  name held in a variable, as in `Set-Item "Env:$n"`, a later `git commit`
  can land in another checkout as a plain commit, which only
  [`commit-gate`](#commit-gate) holds. `git log` and `git reflog` in the other
  checkout show a commit no gate passed, and when it came.
- **Your own git configuration.** Aliases and includes in `~/.gitconfig`, the
  system configuration, a file `GIT_CONFIG_GLOBAL` or `GIT_CONFIG_SYSTEM` named
  when the session started, and anything those include, are not read. A bare
  alias of yours, such as `git st`, is allowed; so would be one whose value
  removes a worktree or makes a commit. `git config --show-origin
  --get-regexp '^alias\.'` lists every alias git would run, and where it came
  from.
- **A path given as a default.** `${X:-…}`, `${X:=…}` and `${X=…}` are read
  without the default, so `rm -rf "${D:-..}"` or `rm -rf "${D:=..}"`, run from
  a story's worktree, is not taken for removing the directory that holds the
  checkout.
- **Code that works out its own path.** A program handed to an interpreter is
  read for the paths it names outright, not for a path relative to where it
  runs or one it works out, such as its own current directory. So
  `node -e 'require("fs").rmSync("..", {recursive: true})'` and
  `python3 -c 'import os, shutil; shutil.rmtree(os.getcwd())'` are not
  refused. For this and the entry above, you would notice the checkout gone:
  `git worktree list` marks it prunable, and every `sdlc` command on its story
  says the checkout holding it no longer exists
  ([SDLC-E0051](troubleshooting.md#sdlc-e0051)).
- **PowerShell spellings of git.** `& (Get-Command git) worktree prune`,
  dot-sourcing a variable, as in `. $G worktree prune`, and a git command whose
  output is assigned, as in `$x = git worktree prune`, are not read as git, so
  no git rule refuses them. A commit made this way is refused at the next gate
  instead ([SDLC-E0046](troubleshooting.md#sdlc-e0046)); anything else shows
  only in what it did, such as a worktree `git worktree list` no longer lists,
  or lists as prunable.
- **Words or a script from standard input, or from a file.** The rules read a
  command as it is written. What reaches a program on its standard input, or
  in a file it is told to read, is not read for what it runs:
  `echo prune | xargs git worktree`, `xargs -a args.txt git worktree`,
  `echo 'git worktree prune' | sh`, `echo commit -m x | xargs git -C <main>`,
  and a script run from a file, such as `sh tidy.sh`. Such a command can remove
  or prune a worktree, or commit in another checkout, and the same holds for
  every rule that turns on which program a command runs. A person's decision
  run this way, as in `echo 'sdlc approve US-001' | sh`,
  `bash <<< 'sdlc approve US-001'`, `echo 'sdlc unfreeze --reason x' | sh` or
  a script run from a file, is refused by the command itself, with
  SDLC-E0079, in every session, the one holding the story included. Run after
  a command has removed Claude Code's markers, a person's decision is not
  caught this way, and a model working a story could lift its own freeze
  (the last entry of this list). Ending the iteration part-way with
  `sdlc stop`, which turns every rule off, still gets through this way:
  [`stop-is-a-human-decision`](#stop-is-a-human-decision) reads it only
  written out, and `sdlc stop` does not refuse itself inside Claude Code,
  since the runbook's last step runs it there. `git worktree list`
  shows a worktree gone, or marks it prunable; `git log` and `git reflog` in
  the other checkout show a commit no gate passed, and when it came; and the
  story's `gate-record.json` lists every approval and unfreeze with when it was
  made.
- **Deleting through the Finder.** AppleScript run with `osascript`, telling
  the Finder to delete a checkout or move it to the Trash, is not read. The
  checkout is then in the Trash, and can be put back from there. Until it is,
  `git worktree list` marks it prunable, and every `sdlc` command on its story
  says the checkout holding it no longer exists
  ([SDLC-E0051](troubleshooting.md#sdlc-e0051)).
- **The shell command a `!` alias runs.** An alias whose value starts with `!`
  runs a shell command. The rules on git and on commits read that command, so
  `git -c alias.x='!git worktree prune' x` is refused. A redirect in it onto a
  file a rule protects is refused too, as in
  `git -c alias.x='!echo y > CLAUDE.md' x`. No other rule reads it: a file it
  writes or deletes with a program, as in
  `git -c alias.x='!rm -rf .sdlc/state' x`, is not refused, nor is anything
  else only the other rules would refuse. A person's decision run this way, as
  in `git -c alias.x='!sdlc approve A-1' x`, is refused by the command itself,
  with SDLC-E0079. `git status` shows a file it changed, and the
  story's `gate-record.json` lists every approval and unfreeze with when it
  was made, so one nobody remembers making shows there.
- **`sdlc` reached under another name.** The shell rules find `sdlc` only
  where the word is written out. Reached through a variable, as in
  `"$SDLC_BIN" approve A-1` or `s=sdlc; $s approve A-1`, through a shell alias,
  as in `alias s=sdlc; s approve A-1`, through a command substitution, as in
  `$(command -v sdlc) approve A-1`, or as a copy under another name, as in
  `cp bin/sdlc /tmp/s && /tmp/s approve A-1`, a person's command is refused by
  the command itself, with SDLC-E0079. The story's `gate-record.json` lists
  every approval and unfreeze with when it was made.
- **Code or a string that runs `sdlc`.** Code handed to an interpreter that
  runs `sdlc` itself, as `python3 -c` does with
  `subprocess.run(["sdlc", "approve", "A-1"])`, runs it where the shell rules
  cannot read it, and the command itself refuses it with SDLC-E0079. Given
  `["sdlc", "hook", "UserPromptExpansion"]` and a payload it wrote, the hook
  answers that the command did not run, and records nothing, since the
  process has `AI_AGENT`. PowerShell's `Invoke-Expression` or `iex` runs a
  command it is given as a string, which the shell rules do not read;
  `Invoke-Expression 'sdlc approve A-1'` is refused by the command itself,
  with SDLC-E0079. The story's `gate-record.json` lists every approval and
  unfreeze with when it was made.
- **The hook under another name, on an event it cannot read.** A copy of
  `sdlc` or of the plugin's launcher under another name, given an event the
  shell rules cannot read, as in `/tmp/x hook "$E"`, reaches the hook, which
  answers that the command did not run and records nothing, since the process
  has `AI_AGENT`
  ([`slash-command-is-a-human-decision`](#slash-command-is-a-human-decision)
  refuses that copy before it runs only with the event written out). The
  story's `gate-record.json` lists every approval and unfreeze with when it
  was made.
- **A PowerShell expression in parentheses.** In a `PowerShell` command, a word
  given as an expression in parentheses is read as a command of its own, not
  as a word of the command it is given to, so the shell rules read neither the
  subcommand nor the event given that way. `sdlc ('ap'+'prove') US-1` is
  refused by the command itself, with SDLC-E0079, and
  `sdlc hook ('User'+'PromptExpansion')` reaches the hook, which answers that
  the command did not run, since the process has `AI_AGENT`. The story's
  `gate-record.json` lists every approval and unfreeze with when it was made.
- **A session in a project that does not take part.** Its calls are not read.
  A plain `sdlc approve` run there that reaches a project that does take part,
  through a directory the command does not spell, such as
  `cd "$(cat /tmp/d)"`, is refused by the command itself, with SDLC-E0079.
  Running the hook there on a payload whose `cwd` names a project that does
  take part answers that the command did not run, and records nothing, since
  the process has `AI_AGENT`. That project's `gate-record.json` lists every
  approval and unfreeze with when it was made.
- **A project MCP server, or a project hook.** Any session can write
  `.mcp.json`, which no rule protects, and Claude Code starts a server named
  there once the person approves it, or with no question where
  `enableAllProjectMcpServers` is set. A session that holds no story can write
  the project's `.claude/settings.json` or `.claude/settings.local.json`,
  which are protected only in the session that holds the story, and Claude
  Code runs a hook named there when its event comes. Claude Code starts either
  with an environment of its own, which has `CLAUDECODE`, so a person's
  command it runs is refused with SDLC-E0079. That environment of its own may
  not have `AI_AGENT`, and a person's slash command it feeds the hook on a
  payload it wrote, with `sdlc hook UserPromptExpansion`, is then not caught.
  You would notice Claude Code asking you to approve a server you did not add,
  `git status` showing `.mcp.json` or a file under `.claude` changed, and the
  story's `gate-record.json` listing a decision nobody remembers making.
- **A slash command a program types into your own prompt.** Where you run
  Claude Code inside `tmux` or `screen`, or on macOS, a command the model runs
  can type into your session's prompt: `tmux send-keys` into the session's
  pane, `screen -X stuff`, or a keystroke sent with `osascript` through System
  Events. Claude Code takes what was typed as your typing, and its own hook,
  which starts with an environment of its own and no `AI_AGENT`, records a
  person's decision typed that way: it is not caught. This is not new: in
  0.4.0 the same keystrokes typed a `!sdlc approve` line, which SDLC-E0079 now
  refuses. You would notice a command in your session's prompt or transcript
  that you did not type, and the story's `gate-record.json` listing a decision
  nobody remembers making.
- **A path no rule can tell is the same file.** On macOS,
  `/.vol/<device>/<inode>` names a file by its volume and inode, and a hard
  link is a second name for the same file. Neither resolves to the path a rule
  protects, so a write through one is placed where it is spelled. `ls -i`
  shows the same inode for both names, and `git status` in the checkout shows
  the protected file changed.
- **A program that starts clean, or removes Claude Code's markers.** The
  routes above are caught only while the process keeps the environment of the
  model's tool that started it. A person's decision run by a program that
  starts it with a clean environment is not caught: `launchctl submit`, a
  launchd agent, `cron`, Claude Code's own daemon, Terminal told by
  AppleEvents to run it, a `tmux` server a person started that is already
  running, `su -`, `sudo -i`, plain `sudo` and `doas` as they are set up by
  default, `ssh`, `schtasks`, WMI's `Win32_Process` and `Start-Process
  -UseNewEnvironment` each start it with none of the markers. `at` keeps the
  environment it is given, and so does an app `open` starts. A person's
  decision in an `at` job submitted after a command removed Claude Code's
  markers is not caught, and neither is one `open` hands to an app that was
  already running, which has an environment of its own. A person's decision
  run after a command has removed or blanked Claude Code's markers, in the
  same call or an earlier one of the persistent shell, or by a program that
  clears its own environment, is not caught either. Removing `AI_AGENT` alone
  is enough for the hook: a person's slash command fed to it on a payload a
  model wrote is not caught once that is unset, since the hook hands the
  command none of the other markers. Run after removing Claude Code's markers,
  `sdlc unfreeze --reason x` lifts a story's freeze, and so a model working a
  story could lift its own. The story's `gate-record.json` lists every
  approval and unfreeze with when it was made.

#### Other checkouts and claims

- **Ordinary files in another checkout.** The shell can write a file no rule
  protects in another checkout: a story worktree's source from the main
  checkout, or the main checkout's from a story's worktree. Another story's
  frozen tests are among them, except one at a path this story's freeze also
  holds: a write to that path in any checkout is refused by
  [`frozen-test-through-the-tool`](#frozen-test-through-the-tool). `git status`
  in that checkout shows the change, and a frozen test changed that way is
  reported by that story's next gate as a broken freeze
  ([SDLC-E0025](troubleshooting.md#sdlc-e0025)).
- **`GIT_WORK_TREE` around `sdlc` itself.** An `sdlc` command run with
  `GIT_WORK_TREE` set treats that directory as the checkout, `sdlc start`
  included. The paths `sdlc start` prints say where it made and recorded the
  story.
- **A finished story between its retro and `sdlc stop`, outside a worktree.**
  Its claim goes when the story is done, and a checkout's `active` holds only a
  story in progress or waiting for a person, so from a call that reaches only
  other checkouts the session working it is not found, and is held there to
  nothing until `sdlc stop` ends the iteration. In its own checkout it is held
  as before. Nothing says so; the runbook runs `sdlc stop` straight after the
  retro, which closes it. A story worked in its own worktree is not done until
  it lands, and keeps its claim until then.
- **Claims written by a session that holds nothing.** Such a session can write
  or remove a claim or another checkout's `.sdlc/state/active`, and so make two
  checkouts name one story. Each is then refused it
  ([SDLC-E0050](troubleshooting.md#sdlc-e0050), which lists them) until a person
  runs `sdlc stop` in all but one. What the refusal says about each checkout is
  read from files anybody can write: it helps a person choose, and proves
  nothing.

## Unattended sessions

[`sdlc parallel`](commands.md#sdlc-parallel-id) starts one headless Claude Code
session for each story named. Those sessions are **not interactive**: no
permission prompt reaches anyone, so a tool that default permissions deny ends
that story's turn. Without `--unattended` nothing is pre-approved, and the
sessions go as far as default permissions allow. The launcher never passes a
blanket skip of permissions in either mode.

### What `--unattended` pre-approves

With `--unattended` the launcher gives each session exactly this list to use
without asking, and adds nothing else. What your own Claude Code settings allow
still applies on top of it, which is [how you add a command](#adding-a-command):

```text
Read
Edit(.sdlc/worktrees/**)
Write(.sdlc/worktrees/**)
Bash(cd:*)
Bash(sdlc start:*)
Bash(sdlc status:*)
Bash(sdlc gate:*)
Bash(sdlc artifact:*)
Bash(sdlc codemap:*)
Bash(sdlc review:*)
Bash(sdlc staffing set:*)
Bash(sdlc staffing amend:*)
Bash(sdlc staffing show:*)
Bash(sdlc escalate:*)
Bash(sdlc freeze:*)
Bash(sdlc story:*)
Bash(sdlc cost:*)
Bash(git status:*)
Bash(git add:*)
Bash(go build:*)
Bash(go test:*)
Bash(go vet:*)
Bash(cargo build:*)
Bash(cargo test:*)
Bash(npm test:*)
Bash(pytest:*)
```

### What the list leaves out

The list is written for the loop's own commands, by name, and not `sdlc` as a
whole. It has:

- no network tool,
- no `git push`,
- no `claude`,
- no `sdlc approve`, `unfreeze`, `ack`, `remap`, `release`, `parallel` or `stop`,
- no `git commit`,
- no interpreter or runner (`go run`, `sh`, `python3`, `env`, `xargs`,
  `npm run`),
- and no `git diff`, `log` or `show`, whose `--output` writes a file where it
  is told.

The staffing's commands are named one by one for the same reason:
`sdlc staffing` as a whole would pre-approve `sdlc staffing assign`, which is a
person's. `sdlc codemap` is on the list as a whole, for the bookkeeper's
`sdlc codemap write`, which is its only subcommand.

### What the list does hand over

What it does have is worth reading as it is:

- **The toolchain entries run code the session wrote, with your privileges.**
  `go test`, `go build`, `cargo test`, `npm test` and `pytest` are arbitrary
  code from the repository. That is the work, and also the reason to read what
  a story's branch holds before you run anything from it yourself.
- **`Edit` and `Write` are scoped to `.sdlc/worktrees/**`**, the worktrees of
  the stories under the main checkout, where each session works. The main
  checkout's own files and the rest of the machine are outside them.
- **Text in the repository is input to a session that holds pre-approved
  tools.** A file, an issue or a test that says something to the model is
  prompt injection to a session nobody is watching.

### Narrowing the list

`parallel.allow` in the project's configuration can only narrow the list: a
list names the built-in entries to keep, an empty one keeps none, and an entry
that is not built in refuses the run
([SDLC-E0066](troubleshooting.md#sdlc-e0066)). The file is writable by a session
on the project, which is why it is never allowed to widen anything.

### Adding a command

Nothing in `sdlc` adds to the list: there is no flag for it, and `parallel.allow`
only takes entries away. A session is an ordinary Claude Code session, though,
started in the main checkout, so it reads your Claude Code settings as any
session there does. An allow rule in those settings is a tool the session uses
without asking, with `--unattended` or without it.

To let the sessions run one more command, add a rule for it to
`.claude/settings.local.json` in the main checkout:

```json
{
  "permissions": {
    "allow": [
      "Bash(make lint:*)",
      "Bash(git diff:*)"
    ]
  }
}
```

`Bash(make lint:*)` lets through `make lint` and `make lint` followed by
anything. The session's last words, printed beneath an `ended early` story,
usually name the command that was denied, which is the one to add. Run
`sdlc parallel` for that story again and it resumes where it stopped.

Which file you choose decides who gets the rule:

| File | Who gets the rule |
| --- | --- |
| `.claude/settings.local.json` in the main checkout | your sessions in this project. Claude Code keeps the file out of git |
| `.claude/settings.json` in the main checkout | everyone who works on the project, once it is committed |
| `~/.claude/settings.json` | every session you start, in every project |

Before you add one:

- **The rule is not only for unattended sessions.** Every Claude Code session
  that reads the file stops asking about that command, the ones you sit in
  front of included.
- **None of what [the list leaves out](#what-the-list-leaves-out) is left out
  for you any longer.** A rule for a runner (`./task`, `make`, `npm run`, an
  interpreter) pre-approves whatever that runner can be made to run, and a rule
  for `git diff` pre-approves its `--output`.
- **A deny rule in the same settings applies too**, and beats every allow,
  the built-in list included.
- **The hook still decides first.** A rule for `sdlc approve` or for
  `git commit` does not get a session past what
  [a person's decisions](#a-persons-decisions-refused-in-every-session) and the
  gates refuse.
- **Some commands are asked about whatever the rules say.** Claude Code asks
  about a `cd` joined to a `git` command (`cd dir && git status`) even when
  both halves are allowed, and no rule changes that.

### What the hook still holds, and what nothing stops

The hook still decides in these sessions, and a deny beats an allow. But a
session is bound to its story only once it has run `sdlc start`, and every rule
that needs a story holds from then on. A project whose build is none of the
toolchain entries in [the list](#what---unattended-pre-approves) runs headless
and is stopped at the first tool it was not given.

What nothing here can stop is a script file a pre-approved command runs, and a
spelling the hook cannot read. Nothing records who started a run or with which
list: the evidence is the session transcripts and the hook's debug log.

## Stopping mid-story

A session that ends its turn while a story is being worked on — without handing
the story to a person or ending the iteration — leaves the story where nobody
is looking. Finishing a gate is not a place to stop: the next one is waiting.
The `Stop` hook sends that stop back, with what to do instead: work the next
gate or `sdlc escalate`, or, once every gate has passed, `sdlc stop`.

A stop it has already sent back goes through, so a session is never held in a
loop. The count is kept per story and starts again whenever anything is recorded
on it. Once [`loop.max_stop_blocks`](configuration.md#loop) stops in a row have
been sent back with nothing recorded — three, by default — the next one hands
the story to a person with a `loop_stalled` escalation and ends the iteration.
`sdlc status` lists it as waiting, and `sdlc approve` lets it start again.

In an interactive session this is felt: a reply that ends mid-story is sent back.
`"max_stop_blocks": 0` turns the guard off, and so does `SDLC_ENFORCE=0`.

### Waiting for a subagent

A session that stops while a subagent it started is still running is waiting,
not giving up: it has nothing to record until the subagent reports, and Claude
Code wakes it then. Claude Code lists what a session has left running in
`background_tasks` on the `Stop` event, and while an entry there has the type
`subagent` the stop is let through and not counted. Other background work — a
shell command, a monitor, a workflow, a teammate — does not hold the count.

The hook does not watch the subagent; it looks only when the session stops.
When the list is missing or cannot be read, the stop is counted as before and
the [debug log](#seeing-what-happened) says the hook could not tell. A Claude
Code version that sends no list is counted that way too. A stop that was
counted with no line about a subagent had a list the hook read, with no
subagent in it.

### How long the wait lasts

The wait has an end. It lasts 30 minutes from the first stop let through, and
`waiting_since` in `.sdlc/state/stop-blocks.json` is when that was, in UTC. The
30 minutes is fixed, not a setting.

The wait belongs to what is recorded on the story, not to the session or to the
subagent, so only recording something on the story starts a new one: another
stop does not, and neither does a second or later subagent started with nothing
recorded since the first.

After the 30 minutes a stop is counted even with a subagent listed, so a
subagent that never ends is still handed to a person. The stops let through
during the wait are not in the number the hand-over reports.

## Rules the command enforces

The hook is only half of it. `sdlc gate <name> pass` refuses when the gate did
not actually happen:

| Refused | Code |
| --- | --- |
| an earlier gate has not passed | `SDLC-E0031` |
| definition of ready, for a story with no acceptance criteria | `SDLC-E0044` |
| the gate's documents are not on disk | `SDLC-E0020` |
| a reviewer has not reported, or reviewed something that has changed since | `SDLC-E0029` |
| a blocking reviewer blocked, or perf found a stated performance budget broken | `SDLC-E0030` |
| the tests are not frozen, or the freeze belongs to another story | `SDLC-E0023` |
| a frozen test changed or vanished | `SDLC-E0025` |
| a test file the freeze does not hold, at `tests_frozen`, `plan`, `implementation`, `verification` and `commit` | `SDLC-E0043` |
| the commit gate, with work still uncommitted | `SDLC-E0032` |
| the commit gate, with a note naming a commit its range does not hold | `SDLC-E0049` |
| any gate before the commit, once HEAD has moved since `sdlc start` | `SDLC-E0046` |

A `fail` is always recordable. A gate can fail precisely because its work could
not be done, and refusing to record that would leave the loop with nowhere to
put the truth.

`sdlc start` holds new work to the same standard before it begins. A story
picked up again is not held to it, because its own work is in the tree:

| Refused | Code |
| --- | --- |
| HEAD is not on `git.trunk_branch`, with `git.worktrees` false | `SDLC-E0038` |
| uncommitted work that belongs to no story, with `git.worktrees` false | `SDLC-E0039` |
| trunk is behind `origin`, with `git.remote` on | `SDLC-E0040` |
| trunk fails `commands.smoke`, in the story's new worktree with `git.worktrees` on | `SDLC-E0041` |
| the story's worktree or branch cannot be made, with `git.worktrees` on | `SDLC-E0053` |

## Turning it off

`SDLC_ENFORCE=0` in the environment you start the session from disables every
hook rule. It cannot be set from inside a session.

Use it when you are debugging the loop itself. If you find yourself reaching for
it during ordinary work, the rule that is in your way is probably wrong — please
open an issue and say which one.

## Seeing what happened

A hook that allows a call says nothing unless something is wrong, and its stderr
goes to Claude Code's debug log, which makes "why did nothing happen" the
hardest question to answer from outside. This turns the hook's decisions into a
log you can read:

```console
$ export SDLC_DEBUG_FILE=/tmp/sdlc-hook.log
```

`SDLC_DEBUG=1` without a file writes the same log to stderr. `SDLC_TRACE=1` adds
how long each call to `sdlc` took, which is how to tell whether a slow session is the hook.

To read a single refusal, see [Why was I refused?](guides/why-was-i-refused.md).
For an `SDLC-E` code, see [Troubleshooting](troubleshooting.md).
