# Troubleshooting

- [Introduction](#introduction)
- [Find your problem](#find-your-problem)
- [Where to run a command](#where-to-run-a-command)
  - [The story's worktree](#the-storys-worktree)
  - [Your own terminal](#your-own-terminal)
- [Common problems](#common-problems)
  - [Setting up](#setting-up)
  - [Starting a story](#starting-a-story)
  - [A command that finds no story](#a-command-that-finds-no-story)
  - [When the loop waits for you](#when-the-loop-waits-for-you)
  - [Tests and the freeze](#tests-and-the-freeze)
  - [Gates and reviews](#gates-and-reviews)
  - [Landing a finished story](#landing-a-finished-story)
  - [Your settings](#your-settings)
  - [Nothing is being refused](#nothing-is-being-refused)
- [Error codes](#error-codes): `SDLC-E0001` to `SDLC-E0095`, in order
- [Warnings the hook prints](#warnings-the-hook-prints)
- [Where to go next](#where-to-go-next)

## Introduction

`sdlc` has stopped and told you something, and you want to get moving again.
Don't worry. It stops when a rule you are relying on was about to be broken, or
when something it needs is missing, and every stop comes with a way through.

This page is where a refusal's link lands. It has an entry for every error code
`sdlc` prints, and each entry says the same four things in the same order: what
happened, why `sdlc` stopped, what to do, and where to read more. Each entry
stands alone, so read only the one you came for.

It also covers the messages that are not errors: the warnings the hook prints
in a session when it cannot do its job, and what a slash command says when it
does not run.

## Find your problem

A refusal from the `sdlc` command ends with a code and a link. This is what you
see when you answer the loop from your project's directory while the story
waits in its worktree:

```console
$ sdlc approve AUTH-3
sdlc: "AUTH-3" is held by another checkout of this repository

  why  the checkout at "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3" holds it, and a person's answer is about the work in that checkout, not this one
  fix  a person runs the command in the checkout that holds the story, named above, or moves the story here with `/sdlc:release <ID> --reason "..."`, or `sdlc release <ID> --reason "..."` in their own terminal, then `sdlc start <ID>` in this checkout; an assistant stops and tells the person which checkout holds the story, and runs neither "sdlc start" nor "sdlc release"

  SDLC-E0050  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0050
```

There are three ways to find your entry:

1. **Follow the link.** The last line of the refusal goes straight to the
   code's entry.
2. **Search this page for the code**, such as `SDLC-E0050`. A code keeps its
   meaning for good, so one you find in an old log or an old issue still
   describes the same problem.
3. **Browse [Common problems](#common-problems)** when you have a situation and
   no code.

Read the `fix` line first. It names the way through, and here that is one
`cd`. An entry on this page says more: which of several causes you have met,
which directory to run the command in, and the few cases where the `fix` line
is not the whole answer.

> [!NOTE]
> A refusal that names a rule in square brackets, such as `[commit-gate]`,
> comes from the hook and not from the command. Most of those carry no code.
> [Why was I refused?](guides/why-was-i-refused.md) shows how to read one.

## Where to run a command

Two things decide whether a command works: which directory you run it in, and
who runs it. Most entries on this page come down to one or the other.

### The story's worktree

Each story is worked in a worktree of its own: a second checkout of your
repository at `.sdlc/worktrees/<ID>`, on a branch named `story/<ID>`. Your
project's own directory is the main checkout. Nearly every command that acts on
a story runs in that story's worktree.

To find it, run `sdlc status` anywhere in the repository. The `held` line names
the worktree:

```text
  held     AUTH-3  in "/home/<user>/src/billing/.sdlc/worktrees/AUTH-3"
```

Change to that directory and run the command there:

```console
$ cd .sdlc/worktrees/AUTH-3
```

Three things are done from the main checkout instead: starting a story,
changing your settings or your backlog, and anything an entry says to do "on
trunk". Trunk is the branch your finished work lands on, whatever your project
calls it.

> [!NOTE]
> In Claude Code, `/sdlc:next` typed in your project begins the next story, and
> `/sdlc:next <ID>` picks up a story that is already under way, wherever you
> type it. Every other slash command acts on the directory its session was
> opened in.

### Your own terminal

Six commands are a person's decision, and no agent can run them:
`sdlc approve`, `sdlc unfreeze`, `sdlc ack`, `sdlc remap`, `sdlc release` and
`sdlc staffing assign`. You can give each one in two ways:

- **In Claude Code or Claude Desktop**, type its slash command, such as
  `/sdlc:approve AUTH-3`.
- **In your own terminal**, run the command itself. Your own terminal is one
  outside Claude Code: a window you opened yourself. A terminal that Claude
  Code opened does not count, because there `sdlc` cannot tell you from the
  agent.

Where an entry says "the session", it means the Claude Code session working the
story and its agents. The session meets most refusals before you do. Where an
entry says a fix is yours, the session does not attempt it: it hands the story
to you and stops.

## Common problems

Find the situation you are in, then follow the code to its entry.

### Setting up

| What you see | What to do | Entry |
| --- | --- | --- |
| `this is not a Git repository` | Change into your repository, or run `git init` in a new project. | [SDLC-E0001](#sdlc-e0001) |
| `this project has not been set up for the loop` | Run `sdlc init` in the repository's root. | [SDLC-E0002](#sdlc-e0002) |
| `this project is already set up for the loop` | Edit `.sdlc/config.json` yourself. `init` will not overwrite it. | [SDLC-E0003](#sdlc-e0003) |
| `the story's worktree could not be made` | Most often, commit the files `sdlc init` wrote, then start again. | [SDLC-E0053](#sdlc-e0053) |
| A session says `the sdlc binary was not found` | Install the `sdlc` command, or let `/sdlc:next` install it. | [The binary was not found](#the-sdlc-binary-was-not-found-so-nothing-is-being-enforced) |

### Starting a story

| What you see | What to do | Entry |
| --- | --- | --- |
| `there is no story to start` | Run `sdlc story list` to see what holds each story back. | [SDLC-E0010](#sdlc-e0010) |
| `no story with the id` | Check the id against `sdlc story list`. | [SDLC-E0009](#sdlc-e0009) |
| `commands.smoke failed in the story's new worktree` | Fix trunk, or have the smoke command install what a new checkout needs. | [SDLC-E0041](#sdlc-e0041) |
| `is waiting for a person` | Answer the loop's question first. | [SDLC-E0036](#sdlc-e0036) |
| `is finished`, and it waits to land | Run `sdlc stop` in the story's worktree. | [SDLC-E0033](#sdlc-e0033) |
| `an iteration is already running` | Start the second story from the main checkout. | [SDLC-E0012](#sdlc-e0012) |

### A command that finds no story

| What you see | What to do | Entry |
| --- | --- | --- |
| `there is no story being worked on` | Run the command in the story's worktree, which `sdlc status` names. | [SDLC-E0011](#sdlc-e0011) |
| `is held by another checkout of this repository` | Run the command in the checkout the refusal names. | [SDLC-E0050](#sdlc-e0050) |
| The checkout that holds the story is gone | Release the story, then start it again. | [SDLC-E0051](#sdlc-e0051) |
| The story is being worked on in another session | Type `/sdlc:next` in the session that should have it. | [SDLC-E0048](#sdlc-e0048) |

### When the loop waits for you

| What you see | What to do | Entry |
| --- | --- | --- |
| `is waiting for a person` | Read the question in `sdlc status`, then approve or reject. | [SDLC-E0036](#sdlc-e0036) |
| `has not been approved by a person for committing` | Read the work, then approve it. | [SDLC-E0037](#sdlc-e0037) |
| `nothing on "AUTH-3" is waiting for a decision` | Nothing: there is no question to answer. | [SDLC-E0035](#sdlc-e0035) |
| `was run in a process Claude Code started` | Type the slash command, or use your own terminal. | [SDLC-E0079](#sdlc-e0079) |
| A slash command answers that it `did not run` | Do what it says about the variable it names, or use your own terminal. | [A slash command did not run](#a-slash-command-did-not-run) |

### Tests and the freeze

| What you see | What to do | Entry |
| --- | --- | --- |
| A frozen test changed | Put it back, or lift the freeze on the record. | [SDLC-E0025](#sdlc-e0025) |
| `there are test files the freeze does not hold` | Remove them, or lift the freeze and freeze again. | [SDLC-E0043](#sdlc-e0043) |
| `the tests are already frozen` | Lift the freeze first if the tests really have to change. | [SDLC-E0022](#sdlc-e0022) |
| `there are no test files to freeze` | Write the tests, or correct `paths.tests`. | [SDLC-E0024](#sdlc-e0024) |
| `the freeze waits for the test reviewer's review of the tests as they are` | Have the test reviewer look at the tests, then freeze again. | [SDLC-E0029](#sdlc-e0029) |
| The freeze check refused the freeze, or the new tests passed | Correct the test files it names. | [SDLC-E0085](#sdlc-e0085) |

### Gates and reviews

| What you see | What to do | Entry |
| --- | --- | --- |
| `cannot pass until its reviews are in` | Wait for the reviewers, or send a change back to them. | [SDLC-E0029](#sdlc-e0029) |
| `is blocked by a reviewer` | Usually nothing: the session reworks and the reviewer looks again. | [SDLC-E0030](#sdlc-e0030) |
| A gate `has gone to` a reviewer's arbiter | Usually nothing: the session has the arbiter review. | [SDLC-E0030](#sdlc-e0030) |
| A command `did not pass, so the implementation gate is recorded as failed` | Read the output file the message names. | [SDLC-E0095](#sdlc-e0095) |
| `the configured commands of AUTH-3 are already running` | Wait for the run to finish. | [SDLC-E0006](#sdlc-e0006) |
| `sdlc decide` recorded nothing | Give the part the message names. | [SDLC-E0094](#sdlc-e0094) |
| A gate `comes after` another | Record the earlier gate first. | [SDLC-E0031](#sdlc-e0031) |
| `cannot pass until its documents are stored` | Store the gate's documents. | [SDLC-E0020](#sdlc-e0020) |
| `cannot pass while a finding ... is open` | Close each finding the message names. | [SDLC-E0092](#sdlc-e0092) |
| `the commit gate cannot pass while there is uncommitted work` | Commit the work, or stash what is not the story's. | [SDLC-E0032](#sdlc-e0032) |
| Trunk moved under the story | Decide whether the new commits stay. | [SDLC-E0046](#sdlc-e0046) |

### Landing a finished story

A story lands when `sdlc stop` runs in its worktree after the last gate. A
refused landing changes nothing: trunk is where it was, and the story waits in
its worktree.

| What you see | What to do | Entry |
| --- | --- | --- |
| The story's commits `do not apply onto` trunk | Rebase the story onto trunk and resolve the conflict. | [SDLC-E0075](#sdlc-e0075) |
| Your project's directory `has changes where the landing writes` | Stash or commit them there. | [SDLC-E0056](#sdlc-e0056) |
| A command or a check failed at landing | Read which one, then follow its fix. | [SDLC-E0055](#sdlc-e0055) |
| Trunk was rewritten, or moved while the landing ran | Rebase by hand, or run the stop again. | [SDLC-E0054](#sdlc-e0054) |
| Another story is still landing | Run `sdlc stop` again when it has finished. | [SDLC-E0006](#sdlc-e0006) |
| The story landed, and its change to `CODEMAP.md` did not | Choose what happens to the kept change. | [SDLC-E0076](#sdlc-e0076) |

### Your settings

| What you see | What to do | Entry |
| --- | --- | --- |
| `.sdlc/config.json` is not valid JSON | Correct the line the message names. | [SDLC-E0004](#sdlc-e0004) |
| A setting holds a value `sdlc` cannot use | Change each setting the message names. | [SDLC-E0045](#sdlc-e0045) |
| The file still has a `staffing` section | Move each setting to its key under `team`. | [SDLC-E0087](#sdlc-e0087) |
| The `team` section cannot be used | Correct each problem the message lists. | [SDLC-E0088](#sdlc-e0088) |
| The backlog cannot be read | Correct the story the message names. | [SDLC-E0008](#sdlc-e0008) |

### Nothing is being refused

If you expected a refusal and the call went through, run `sdlc doctor` in the
story's worktree. It changes nothing, and it says what is wrong.
[When nothing was refused](guides/why-was-i-refused.md#when-nothing-was-refused)
walks through the causes, and
[Warnings the hook prints](#warnings-the-hook-prints) covers the messages a
session shows when a rule is switched off.

## Error codes

Every code `sdlc` prints is here, in order. In the commands below, `<ID>` is
your story's id, such as `AUTH-3`, and `<trunk>` is the name of your trunk
branch.

### SDLC-E0001

**What happened.** You ran `sdlc` in a directory that is not inside a Git
repository.

**Why.** The loop keeps its records beside your code and measures every change
with git, so it only works in a repository.

**What to do.** Change into the repository you meant to use. For a new project,
make one first:

```console
$ git init
```

If the message names `GIT_CEILING_DIRECTORIES`, don't run `git init`: you may
already be inside a repository that the variable hides. Take the directory the
message names out of that variable, or unset it.

**Read more.** [Set up your project](getting-started.md#set-up-your-project).

### SDLC-E0002

**What happened.** You ran `sdlc` in a repository that has not been set up for
the loop.

**Why.** The loop reads its settings from `.sdlc/config.json`, and this
repository has none.

**What to do.** If you want the loop here, run this in the repository's root,
or type `/sdlc:init` in Claude Code:

```console
$ sdlc init
```

It writes the settings, a backlog with one example story, and the contract
section of your `CLAUDE.md`, or of your `AGENTS.md` where your project keeps
its instructions there. Commit what it wrote before you start a story.

**Read more.**
[Adding sdlc to an existing project](guides/adding-to-an-existing-project.md).

### SDLC-E0003

**What happened.** You ran `sdlc init` in a repository that is already set up.

**Why.** `init` found your `.sdlc/config.json` and stopped rather than write
the defaults over your settings.

**What to do.** To change a setting, edit `.sdlc/config.json` yourself. To
start again from the defaults on purpose, between stories:

```console
$ sdlc init --force
```

**Read more.**
[Starting again from the defaults](guides/adding-to-an-existing-project.md#starting-again-from-the-defaults).

### SDLC-E0004

**What happened.** `.sdlc/config.json` could not be read as it is written. It
is not valid JSON, or a setting holds the wrong kind of value, such as `"3"`
where a number belongs.

**Why.** A file read wrongly would run your project with settings you did not
write, so `sdlc` reads all of it or none.

**What to do.** The message names the line, or the setting and what it takes.
Correct it in your project, on trunk, and check the result:

```console
$ sdlc doctor
```

The usual causes are a trailing comma, an unquoted key, or a `//` comment
copied from the documentation. JSON takes no comments. If the file is past
saving, delete it and run `sdlc init` again.

**Read more.**
[When a setting is wrong](configuration.md#when-a-setting-is-wrong).

### SDLC-E0005

**What happened.** A file the loop depends on could not be read. The message
names it: one of the loop's own records, a story's plan or review, or a test
file the freeze covers.

**Why.** `sdlc` does not guess at a record it cannot read. It writes its own
files whole or not at all, so one that will not read was usually edited or
merged by hand. A merge conflict left in a committed record is the common
cause.

**What to do.** List every file that cannot be read:

```console
$ sdlc doctor
```

Then put each one right:

- Restore a committed file from git, or fix the permissions on one that cannot
  be opened.
- If `doctor` names the file that says which story is under way, `sdlc stop`
  removes it and ends the iteration.
- If the message says `.sdlc/stories` is not a directory, move what is there
  aside and restore the directory.
- If `sdlc trace` printed this, something stands where the story's trace
  should be. Move it aside.

If `sdlc` itself left a file it cannot read, that is a bug. Please
[open an issue](https://github.com/bbsnly/sdlc/issues).

**Read more.**
[When something is broken](guides/stopping-and-resuming-a-story.md#when-something-is-broken).

### SDLC-E0006

**What happened.** `sdlc` could not write its records, or could not get its
turn to. Nothing was recorded.

**Why.** A record is written whole or not at all, and only one `sdlc` command
writes at a time. A second one waits its turn, and is refused if the wait runs
out.

**What to do.** The message says which of these it met:

- **It cannot write.** Check that you can write to `.sdlc/` and that the disk
  is not full. If the message names a path that is in the way, move that path
  aside.
- **`.sdlc/`, or a directory in it, is a link to somewhere outside the
  repository.** A clone can bring one. Replace the link with a real directory
  of the same name.
- **Another `sdlc` command is still running.** The message names it. Wait for
  it to finish, then run your command again. If that command was killed, the
  message says so and says when its place is given up, which is about two
  minutes later. Run your command again then. There is nothing to remove by
  hand, with one exception: if the process the message names is not an `sdlc`
  command at all, remove the directory the message names.
- **The story's commands are already running.** `sdlc gate implementation pass`
  and `sdlc freeze` run your configured commands, one run to a story at a
  time. A second one is refused at once, because the first can take many
  minutes. The message names the run that is going: its process, when it
  started, and which of the two commands it is. Wait for it to finish, then
  run yours again. Other stories and `sdlc status` carry on meanwhile.
- **Another story is landing.** Stories land on trunk one at a time. Run
  `sdlc stop` in your story's worktree again once the other landing has finished.
  `sdlc status` names the story that is landing. If that landing hangs, usually
  on a command that never ends, press Ctrl+C in its terminal. The session runs
  the stop again by itself, up to three times, and then hands the story to a
  person.
- **Another `sdlc stop` is landing this same story.** Wait for it to finish. If
  the story has not landed by then, run `sdlc stop` in its worktree again.

Two messages need more care:

- If the reason ends by saying the gate failure, or the block, **was not
  recorded** because the story could not be handed over, put right what the
  message names and run the same command again. That is safe: the failure is
  counted once.
- If it says the story **is only partly handed over to a person**, don't run
  the command again yet. Run `sdlc status` to see where the story stands. If a
  question is open, type `/sdlc:approve <ID>` in Claude Code, or run
  `sdlc approve <ID>` in your own terminal. Then check that `.sdlc/` and the
  backlog's directory can be written.

**Read more.** [Done when it lands](the-loop.md#done-when-it-lands).

### SDLC-E0007

**What happened.** The backlog file is not where your settings say it is.

**Why.** Every story comes from the backlog, so there is nothing to start
without it.

**What to do.** Run `sdlc init` to create one. If you already keep your stories
in a file, point `backlog.path` in `.sdlc/config.json` at it.

**Read more.** [Writing stories](guides/writing-stories.md).

### SDLC-E0008

**What happened.** The backlog could not be read: it is not valid JSON, or a
story in it is missing something.

**Why.** A story with a status the loop does not know would never be started,
and nobody would notice. So the whole backlog is refused instead.

**What to do.** The message names the story that stopped it. Every story needs
an `id`, a `title` and a `status`, and the status is one of these, spelled
exactly: `todo`, `ready`, `in_progress`, `awaiting_human`, `blocked`, `done`,
`dropped`. Correct the story in your backlog file and try again.

**Read more.** [Statuses](guides/writing-stories.md#statuses).

### SDLC-E0009

**What happened.** No story in the backlog has the id you gave.

**Why.** Ids are compared exactly, including upper and lower case.

**What to do.** List the ids you can use:

```console
$ sdlc story list
```

`sdlc trace <ID>` prints this code too, for a story that is in the backlog and
has not been started. A story has nothing to trace until it has.

**Read more.**
[Start a story by name](guides/writing-stories.md#start-a-story-by-name).

### SDLC-E0010

**What happened.** There is no story to start. The message says why: every
story is done, the ones left are blocked or wait for another story, each one is
already under way, or the stories left are not marked ready.

**Why.** A bare `sdlc start`, or `/sdlc:next`, begins only a story whose
`status` is `ready`. It passes over a `todo` story, because `todo` means the
criteria are not settled yet.

**What to do.** See what holds each story back, from the main checkout:

```console
$ sdlc story list
```

Then do the one that fits:

- **Every ready story is under way.** Pick one up where it is, with
  `/sdlc:next <ID>`.
- **Only `todo` stories are left.** The message names up to five. Mark one
  `ready` in the backlog once its criteria are settled, or name it with
  `sdlc start <ID>`.
- **A story is blocked, or waits for one that is not done.** Finish the story
  it depends on, or correct the backlog.
- **Everything is done.** The loop has finished its work. Write the next story.

You see the same code when you name a story that cannot start: one that is
`done`, `blocked` or `dropped`, or one that waits for a story that is not done.
Naming a story puts it first in line. It does not start work the backlog says
cannot start.

**Read more.**
[Choose what runs next](guides/writing-stories.md#choose-what-runs-next).

### SDLC-E0011

**What happened.** You ran a command that acts on the story being worked on, in
a checkout where no story is under way.

**Why.** Commands such as `sdlc gate`, `sdlc approve` and `sdlc freeze` act on
the story of the checkout they run in. Each story is worked in its own
worktree, so your project's directory usually has none.

**What to do.** Find the story's worktree, change to it, and run the command
again:

```console
$ sdlc status
$ cd .sdlc/worktrees/<ID>
```

The `held` line of `sdlc status` names the worktree.

> [!NOTE]
> The refusal's `fix` line says to run `sdlc start` or type `/sdlc:next`. That
> is right only when no story has been started. For a story that is already
> under way, change to its worktree instead.

Three cases have their own way through:

- **The loop handed the story to you.** A hand-over ends the iteration, so a
  bare `sdlc approve` finds no story. Name the story, in its worktree: type
  `/sdlc:approve <ID>` in Claude Code, or run `sdlc approve <ID>` in your own
  terminal.
- **You are lifting the freeze after a hand-over.** Answer first, as above.
  Then pick the story up with `/sdlc:next <ID>`, and lift the freeze once it is
  running.
- **You are recording a cost.** `sdlc cost add` takes `--story <ID>` for a
  story that is not under way.

**Read more.** [Where to run a command](#where-to-run-a-command).

### SDLC-E0012

**What happened.** You tried to start a story in a checkout that is already
working another one.

**Why.** A checkout works one story at a time, and so does a Claude Code
session. The gates measure the checkout's files, so two stories in one place
could not be told apart.

**What to do.** To work a second story at the same time, start it from the main
checkout, in a second Claude Code session. It gets a worktree of its own:

```console
$ sdlc start <ID>
```

In a story's worktree that is the whole fix. Don't run `sdlc stop` there to
make room: it would put down the story the worktree was made for. With
`git.worktrees` off there is one checkout, and so one story at a time: finish
the current story, or put it down with `sdlc stop`.

Two other commands print this code while a story is running:

- **`sdlc init --force`.** Run it between stories.
- **`sdlc release`.** Run `sdlc stop` in each checkout the message names, then
  release the story.

**Read more.**
[Working several stories at once](the-loop.md#working-several-stories-at-once).

### SDLC-E0013

**What happened.** You named a gate `sdlc` does not know.

**Why.** A gate is recorded under a fixed name, and the message lists the names
this version knows.

**What to do.** Run the command again with one of them:

```console
$ sdlc gate --help
```

**Read more.** [`sdlc gate`](commands.md#sdlc-gate-gate-status).

### SDLC-E0014

**What happened.** You gave `sdlc gate` an outcome it does not take.

**Why.** A gate passed, failed, or has not been reached yet.

**What to do.** Run the command again with `pass`, `fail` or `pending`, as in
`sdlc gate <gate> pass`.

**Read more.** [`sdlc gate`](commands.md#sdlc-gate-gate-status).

### SDLC-E0015

**What happened.** A story's id cannot be used as a name on disk. It holds a
path separator, a `..`, or a character that is not safe in a file name.

**Why.** Each story keeps its records in a directory named after its id.

**What to do.** Rename the story in your backlog. Letters, digits, dots, dashes
and underscores always work, as in `AUTH-3`.

If the message says `.sdlc/state/active does not name a story`, no story is at
fault: the loop's note of which story is under way is damaged. Run `sdlc stop`,
which clears it, and start the story again.

**Read more.** [Every field](guides/writing-stories.md#every-field).

### SDLC-E0016

**What happened.** You asked `sdlc artifact` for a document it does not know.

**Why.** Each gate has its own documents, with fixed names.

**What to do.** List the names this version stores, and use one of them:

```console
$ sdlc artifact list
```

`sdlc artifact diff` takes only `plan`.

**Read more.** [`sdlc artifact write`](commands.md#sdlc-artifact-write-name).

### SDLC-E0017

**What happened.** The document you asked `sdlc` to store was empty.

**Why.** A gate's document is what the later gates read, and an empty one says
nothing.

**What to do.** Pipe the document in, or pass `--file` with a path that has
something in it. If you piped it from another command, check that the command
printed anything.

**Read more.** [`sdlc artifact write`](commands.md#sdlc-artifact-write-name).

### SDLC-E0018

**What happened.** The document you asked `sdlc` to store could not be read.

**Why.** The path given to `--file` does not exist or cannot be opened, or the
command piping the document in stopped part-way.

**What to do.** Check the path, or pipe the document in instead.

**Read more.** [`sdlc artifact write`](commands.md#sdlc-artifact-write-name).

### SDLC-E0019

**What happened.** The document is larger than `sdlc` stores, which is 1 MiB.

**Why.** A gate's document is prose that a person and the later gates read. A
truncated one would be worse than none.

**What to do.** Keep the document itself short, and link from it to the bulk: a
log, a dataset or a capture.

**Read more.** [`sdlc artifact write`](commands.md#sdlc-artifact-write-name).

### SDLC-E0020

**What happened.** A gate was recorded as passed before its documents were
stored.

**Why.** The gates that follow read those documents. A pass with nothing behind
it gives them nothing to work from.

**What to do.** The message names what is missing. In the story's worktree,
store each document, then record the gate again:

```console
$ sdlc artifact write <name> --file <path>
$ sdlc gate <gate> pass
```

Only a pass is held to this. A gate can still be recorded as failed.

**Read more.** [The gates, one by one](the-loop.md#the-gates-one-by-one).

### SDLC-E0021

**What happened.** A git command that `sdlc` ran did not finish. The message
names the command and quotes what git said.

**Why.** The loop asks git what changed, which files belong to the project and
where each branch is. It cannot go on without an answer.

**What to do.** Check that git is installed and that this directory is a
repository git can read:

```console
$ sdlc doctor
```

If the reason ends by saying a gate failure, or a block, was not recorded
because the story could not be handed over, run the same command again once git
works. That is safe: the failure is counted once.

**Read more.** [`sdlc doctor`](commands.md#sdlc-doctor).

### SDLC-E0022

**What happened.** `sdlc freeze` was run on tests that are already frozen.

**Why.** Freezing again over the top would quietly accept whatever changed in
between. A change to frozen tests has to be a decision on the record.

**What to do.** Usually nothing: the freeze is there. If the tests really have
to change, lift the freeze first, in the story's worktree. Type
`/sdlc:unfreeze` in Claude Code, or run this in your own terminal:

```console
$ sdlc unfreeze --reason "AC-2 asks for a 401, and the test expects a 403"
```

The test author then corrects the tests and freezes them again.

If your project sets `freeze.allow_new_test_files`, `sdlc freeze` adds the test
files written since the freeze, and refuses with this code only when there are
none to add. Where the story has a test reviewer, the freeze first waits for
it to approve the tests with the new files among them, and refuses with
[SDLC-E0029](#sdlc-e0029) or [SDLC-E0030](#sdlc-e0030) until then.

**Read more.**
[When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md).

### SDLC-E0023

**What happened.** A command needed the tests to be frozen, and they are not.

**Why.** Until the tests are frozen, a later edit to one leaves no trace, so
the test gate cannot pass.

**What to do.** Once the acceptance tests are written and failing, freeze them
in the story's worktree:

```console
$ sdlc freeze
```

`sdlc unfreeze` prints this code too, when there is no freeze to lift.

**Read more.** [Gate 3: freeze the tests](the-loop.md#gate-3-freeze-the-tests).

### SDLC-E0024

**What happened.** `sdlc freeze` found no test files.

**Why.** Either the acceptance tests are not written yet, or `paths.tests` in
`.sdlc/config.json` does not describe where your project keeps them.

**What to do.** If the tests are missing, the test author writes them first. If
they are there and `sdlc` does not see them, widen `paths.tests` yourself, in
your project on trunk. The session cannot edit that file, so it tells you which
directory or name is missing and stops.

**Read more.**
[Tell sdlc which files are tests](guides/tuning-commands-for-your-stack.md#tell-sdlc-which-files-are-tests).

### SDLC-E0025

**What happened.** A frozen test is not what was frozen. The message names each
file that was changed, or is gone, since the freeze.

**Why.** The freeze is what stops an agent editing its tests until they pass.
The frozen tests are checked again when the test gate, the plan, the
implementation, the verification and the commit are recorded, and once more
when the story lands.

**What to do.** This one is yours: the session hands the story to you and
stops. Find out what changed, then take one of two ways on, in the story's
worktree:

- **The change should not be there.** Put the file back as it was frozen.
- **The test did have to change.** Lift the freeze, so that the change is a
  decision on the record: type `/sdlc:unfreeze` in Claude Code, or run
  `sdlc unfreeze --reason "<why>"` in your own terminal. The test author then
  freezes the tests again.

If the loop handed the story to you, answer its question first with
`/sdlc:approve <ID>`, or `sdlc approve <ID>`, and pick the story up with
`/sdlc:next <ID>`. `sdlc unfreeze` needs the story to be running, and is
[refused](#sdlc-e0011) until it is.

A few cases differ:

- **`sdlc freeze` printed this code.** The story's tests were frozen, and the
  record of that freeze has gone missing without the freeze being lifted.
  Freezing again would accept the tests as they are now, whatever happened to
  them. Lift the freeze on the record first, as above.
- **It came at landing.** A frozen test that only trunk changed since the story
  began is accepted as trunk has it. The files named were changed by the story,
  or by the story and trunk both. Put them back on the story's branch and run
  `sdlc stop` in the worktree again. If the change is to stay, reopen the
  story: answer the hand-over, record a gate as failed with
  `sdlc gate code_review fail --story <ID> --note "<why>"`, type
  `/sdlc:next <ID>`, and lift the freeze once the story is running.
- **The reason says the story's record has no list of its own commits, or
  names a commit of the story's that trunk holds.** `sdlc` then cannot tell
  trunk's test files from the story's. The story was started with an older
  version of `sdlc`, or trunk came to hold a commit the story made. Check that
  each file named is trunk's own and is to stay, then lift the freeze and have
  the tests frozen again. One unfreeze and one freeze are enough, however many
  files are named.

**Read more.**
[When a frozen test was changed](guides/when-a-frozen-test-is-wrong.md#when-a-frozen-test-was-changed).

### SDLC-E0026

**What happened.** A command that needs `--reason` was run without one.

**Why.** Lifting a freeze, moving a story or changing who works on it is a
decision, and the reason is what goes on the record beside it.

**What to do.** The message says which command it was. Run that same command
again with one line of reason:

- **`sdlc unfreeze`.** Say which criterion the test got wrong. Type
  `/sdlc:unfreeze` in Claude Code, or run
  `sdlc unfreeze --reason "AC-2 asks for a 401, and the test expects a 403"`
  in your own terminal.
- **`sdlc release`.** Say why the story is moving to another checkout.
- **`sdlc staffing amend` and `sdlc staffing assign`.** A reason that is blank,
  or longer than 1,000 characters, is refused. The project manager runs `amend`
  again. `assign` is yours.

**Read more.** [Lift the freeze](guides/when-a-frozen-test-is-wrong.md#lift-the-freeze).

### SDLC-E0027

**What happened.** A review was recorded for a role the gate does not take a
review from now.

**Why.** Each gate has its own reviewers: the architect reviews the plan, the
code reviewer reviews the change, and so on. And a gate takes a reviewer's
review or its arbiter's, never both at once.

**What to do.** See which roles the gate expects, and where each review has got
to:

```console
$ sdlc review list --gate <gate>
```

The message says which of six reasons it is:

- **The gate does not expect that role.** Record the review under one of the
  roles the list shows.
- **The agent is turned off**, in `team.agents.<agent>.enabled`. Its review is
  not waited for. Turn it back on in `.sdlc/config.json`, on trunk.
- **The staffing left it out of this story.** The project manager brings it
  back with `sdlc staffing amend <agent> --needed --reason "<why>"`.
- **The name is past the agent's `instances`.** A second reviewer of a role is
  named with a number, as in `security-2`, and `team.agents.<agent>.instances`
  says how many there are. Use a name within it, or raise the number.
- **The arbiter is not due.** `<role>-arbiter` reviews a gate only once its
  reviewer has blocked as many times as its limit allows:
  `loop.max_review_rounds`, or the agent's own `max_rounds`. Before that, the
  reviewer reviews under its own name. A reviewer whose block does not stop
  the gate, such as an advisory one, has no arbiter, and nor has an agent that
  is turned off.
- **The gate has gone to the arbiter.** The reviewer has blocked as many times
  as its limit allows, so the gate takes the arbiter's review instead. The
  session delegates to a fresh agent of the reviewer's kind, which records as
  `<role>-arbiter`. Once the arbiter has reviewed, the reviewer has its turn
  again only when you answer a hand-over that the two of them raised at that
  gate.

`sdlc artifact diff plan --since <role>` prints this code too, when the role is
not one of the design review's reviewers.

**Read more.** [Who can block](guides/when-a-reviewer-blocks.md#who-can-block)
and [the arbiter](the-loop.md#the-arbiter).

### SDLC-E0028

**What happened.** `sdlc review add` was given a verdict it does not take.

**Why.** A reviewer approves, blocks, or leaves a note.

**What to do.** Run the command again with `approve`, `block` or `note`,
spelled exactly. `approve` lets the gate pass, `block` stops it, and `note`
puts a finding on the record without standing in the way.

**Read more.**
[What each verdict does](guides/when-a-reviewer-blocks.md#what-each-verdict-does).

### SDLC-E0029

**What happened.** A gate cannot pass because its reviews are not in. The
message names each reviewer: one that has not reviewed yet, or one whose review
is of something that has since changed.

**Why.** A review is of what the reviewer saw. Revising the plan, or touching
the code, makes every earlier approval of it stale. An approval of a plan that
has since changed is not an approval.

**What to do.** Usually nothing: the session sends the work to the reviewers
that are outstanding. To see who they are, in the story's worktree:

```console
$ sdlc review list
```

If you changed the story by hand, type `/sdlc:next <ID>` so that the reviewers
see your change before it is committed.

**From `sdlc freeze`**, the review is the test reviewer's, of the test files as
they are. It has not reviewed them, has not approved them, or reviewed them
before a test was changed, added or renamed:

```console
$ sdlc freeze
sdlc: the freeze waits for the test reviewer's review of the tests as they are

  why  test-reviewer has not reviewed
  fix  delegate to the reviewers this gate expects -- "sdlc review list" shows who is outstanding

  SDLC-E0029  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0029
```

The session delegates to the test reviewer, or to `test-reviewer-arbiter` where
the refusal names it, and runs `sdlc freeze` again once it approves.

**At landing**, this code means the story's branch is not the change its
reviewers saw. A commit came after the commit gate, or a rebase by hand changed
what the story's commits do. A commit that changes only the root `CODEMAP.md`
never causes it. The `fix` line says which of two cases you have:

- **The reviews can be recorded again.** The session has the verifier and the
  code reviewers look at the branch as it now is, then runs `sdlc stop` again.
- **It is yours.** Trunk no longer has the commit the reviewed change began
  from, or already holds the story in a form nobody reviewed. Read the branch
  against trunk and decide. If the story's own commits are still the change
  that was reviewed, replay only those onto trunk in the worktree and run
  `sdlc stop` there again. Otherwise reopen the story, as
  [Reopen a finished story](guides/stopping-and-resuming-a-story.md#reopen-a-finished-story)
  shows.

**Read more.** [Why approvals go stale](the-loop.md#why-approvals-go-stale).

### SDLC-E0030

**What happened.** A gate cannot pass because a reviewer blocked it.

**Why.** This is the loop working. A way past a reviewer would make every
review optional.

**What to do.** Usually nothing. The session fixes what the reviewer found and
has the same reviewer look again. Its latest verdict is the one that counts.
Recording the gate as failed starts the rework, but it does not clear the
block: only the same reviewer's approval does.

If the reviewer blocks as many times as its limit allows, which is
`loop.max_review_rounds` or the agent's own `max_rounds`, the gate goes to its
[arbiter](the-loop.md#the-arbiter): a fresh agent of the reviewer's kind,
which reviews in its place. The refusal then says that the
gate has gone to `<role>-arbiter`, and its `fix` line names the arbiter:

```text
  why  test-reviewer blocked: AC-2 still has no test, and tests_frozen has gone to test-reviewer-arbiter
  fix  delegate test-reviewer's open findings to a fresh sdlc:test-reviewer as test-reviewer-arbiter, asked for what sdlc staffing show --gate tests_frozen gives it
```

The session does that by itself. If the arbiter blocks to its own limit as
well, the loop stops and hands the story to you.

**From `sdlc freeze`**, the block is the test reviewer's, or its arbiter's. The
test author corrects the tests and the test plan from its findings, and the
reviewer looks again.

**Read more.** [When a reviewer blocks](guides/when-a-reviewer-blocks.md).

### SDLC-E0031

**What happened.** A gate was recorded before the gates that come before it.

**Why.** Each gate builds on the one before: the plan is written against frozen
tests, the code against a reviewed plan, the review against verified work.

**What to do.** See where the story stands, in its worktree, and record the
earlier gate first:

```console
$ sdlc status
```

There is no flag to skip a gate. If an earlier gate failed, it is recorded
again once it really passes.

**Read more.** [The gates, one by one](the-loop.md#the-gates-one-by-one).

### SDLC-E0032

**What happened.** The commit gate was recorded while the story's worktree
still has uncommitted changes.

**Why.** The gate records that the story's work is committed on its branch. A
working tree with changes in it is not.

**What to do.** In the story's worktree, commit the work. If a file there does
not belong to the story, stash that file by path, then record the gate:

```console
$ git stash push -- <path>
```

A stash of the whole working tree is refused while a story is being worked on,
because it would take the loop's own records with it.

If `.sdlc/.gitignore` is among the uncommitted files, `sdlc start` wrote it, to
keep each story's trace out of git. Commit it with the story's work.

**At landing**, this code means the worktree has changes nobody committed, and
your commands would run with them in place. Commit them on the story's branch,
after which the reviewers look again, or stash them by path. Then run
`sdlc stop` there again.

**Read more.** [Gate 8: commit](the-loop.md#gate-8-commit).

### SDLC-E0033

**What happened.** You tried to start a story whose gates have all passed.

**Why.** Starting it would put finished work back in progress with nothing on
the record to say why.

**What to do.** The `why` line says which of two states the story is in:

- **It waits to land.** Every gate has passed, and trunk does not have the
  story yet. Land it from its worktree:

  ```console
  $ cd .sdlc/worktrees/<ID>
  $ sdlc stop
  ```

- **It is done.** If the work has to come back, because a bug turned up after
  the commit, record the gate it goes back to as failed. That reopens the
  story, with the reason on its record:

  ```console
  $ sdlc gate code_review fail --story <ID> --note "AC-2 turned out to be untested"
  ```

`sdlc stop` in the worktree prints this code too, when your latest answer to
the story was a rejection. Reopen the story in the same way, then type
`/sdlc:next <ID>`.

**Read more.**
[When the story is finished](guides/stopping-and-resuming-a-story.md#when-the-story-is-finished).

### SDLC-E0034

**What happened.** A command was given a flag or an argument it cannot use, or
its command line could not be read at all. Nothing was recorded.

**Why.** `sdlc` refuses a value it cannot use rather than guess what you meant.

**What to do.** The message says what was wrong. See what the command takes,
then run it again:

```console
$ sdlc <command> --help
```

These are the ones you are likely to meet:

| Command | What it refuses |
| --- | --- |
| `sdlc cost add --usd` | An amount that is empty, negative or not a number. An empty one usually came from a shell substitution that printed nothing. |
| `sdlc log --since` | Something git cannot read as a commit, or `--since` together with `--all`. |
| `sdlc ack` | A missing `--through`, or a commit that is not on this branch. |
| `sdlc escalate` | A type or a `--message` that is blank or missing. |
| `sdlc approve --reject` | A rejection with no reason. |
| `sdlc trace` | No story id and no `--all`, more than one id, or an id together with `--all`. |
| `sdlc review add --important`, `--nits` | A count that is negative, above 1,000,000 or not a number. |
| Any command | An unknown command, an unknown flag, or the wrong number of arguments. |

If the message is `unknown flag: --important`, your `sdlc` command is older
than the plugin. Update it.

**Read more.** [Commands](commands.md).

### SDLC-E0035

**What happened.** An approval was given for a story that is not waiting for
one.

**Why.** An approval answers a question. A story waits for you only after the
loop has handed it over, and this one has no question open.

**What to do.** Nothing needs answering. To see which stories do wait for you:

```console
$ sdlc status
```

**Read more.** [Human approval gates](guides/human-approval-gates.md).

### SDLC-E0036

**What happened.** The story was handed to you with a question, and it has not
been answered yet.

**Why.** Carrying on would go straight past the question the loop stopped to
ask.

**What to do.** Read the question: `sdlc status` shows it on the `waiting`
line. Then answer in the story's worktree. Type `/sdlc:approve <ID>` in a
Claude Code session opened there, or use your own terminal:

```console
$ cd .sdlc/worktrees/<ID>
$ sdlc approve <ID>
```

To send the work back instead, give your reason:

```console
$ sdlc approve <ID> --reject "the migration has no way back"
```

Either answer goes on the story's record. Then type `/sdlc:next <ID>`, and the
story carries on.

**At landing**, `sdlc stop` prints this code while a question waits, and lands
nothing. Answer in the same way, then run `sdlc stop` in the worktree again.

**Read more.** [Answer](guides/human-approval-gates.md#answer).

### SDLC-E0037

**What happened.** The story is ready to commit, or to land, and your project
holds it for your approval first. Nobody has approved the work as it now
stands.

**Why.** The reason says which of three things holds the story:

- **Its risk tier.** `human_gates.pre_commit_pause_tiers` in
  `.sdlc/config.json` names the tiers that wait: `high`, unless you changed
  it. A story with no `risk_tier` is `low`. A project that keeps
  `pre_commit_pause_tiers` at `["high"]` has every high-tier story wait here,
  whatever paths it lists.
- **A path its change touches.** `human_gates.pre_commit_pause_paths` lists
  globs, and the reason names each file the story changed with the glob it
  matched. A file the story left alone pauses nothing. The story's own copy of
  your settings cannot take the pause away, because the lists in your main
  checkout count too.
- **Something that could not be read**: your settings, or the story's change.
  That pauses, and is never read as no pause. The reason quotes what failed,
  and `sdlc doctor` names the file where your settings are at fault. Your
  approval answers it as it answers any pause. A backlog that cannot be read
  is different: the commit gate refuses with
  [the backlog's own error](#sdlc-e0008), which no approval answers.

An approval is for the work as it stands, so a change made after it needs
approving again. A story that passes the commit gate with nothing holding it
is marked too: its record gains a `landed_without_pause` event, and `sdlc log`
shows it as a `no pause` line.

**What to do.** The `fix` line says what you do, and what the session does
instead of answering for you:

```text
a person reads the work and answers with "/sdlc:approve <ID>", or "sdlc approve <ID>", typing the first in Claude Code or running the second in their own terminal; once they have, the session commits the work they approved. An assistant does not answer for them: it hands the story to a person with "sdlc escalate pre_commit_approval --message 'ready for a person to approve before it is committed'" and stops
```

So read the story's changes in its worktree, then answer there. Type
`/sdlc:approve <ID>` in Claude Code, or use your own terminal:

```console
$ cd .sdlc/worktrees/<ID>
$ sdlc approve <ID>
```

Then type `/sdlc:next <ID>`, and the session commits the work you approved.
Work you sent back stays sent back until it changes.

**At landing**, this code means the work changed after you approved it: the
story was rebased by hand, its reviews were recorded again after the commit
gate, or its tier or a path came to wait for you after it was committed. Read
the work as
it now is, approve it in the worktree, and run `sdlc stop` there. Where the
landing itself put the story onto a newer trunk, and the result is line for
line the change you approved, your approval stands and nobody asks again.

**When the refusal speaks of the story's commits.** A story that waits for
you, and is worked in its own worktree, is also held to its commits. The
refusal then begins
`"<ID>" has not been approved by a person as its commits now stand`, or, at the
commit gate, `"<ID>" would no longer be held to its commits`. A commit was
amended, reworded, squashed, reordered, rebased or added after the commit gate
passed. Read the commits as they would land, in the worktree:

```console
$ git --no-replace-objects log --no-mailmap --format=fuller <trunk>..story/<ID>
```

That form shows each commit's own author and message. A plain `git log` can
show replaced words, or an author that a `.mailmap` renames. Then approve, and
run `sdlc stop`. An answer has to come from the version of `sdlc` that refused:
an older one clears none of these.

If the commits were changed by mistake, and git still has the commit the
refusal names, you can put the story back on it instead:

```console
$ git reset --hard <commit>
```

The reset drops every commit above that one, and any uncommitted change. After
it, `sdlc stop` lands the story with nobody asked.

If the story is finished and no session is running it, the session cannot hand
it over. Record the commit gate as failed in the worktree, then type
`/sdlc:next <ID>`, which takes the story up again at that gate:

```console
$ sdlc gate commit fail --story <ID> --note "the story changed after the commit gate"
```

**What your approval covers.** It covers the files you were shown. You give it
before the story is committed, so the messages and authors of the story's
commits are fixed afterwards, when the commit gate passes. Nobody is asked to
read them, but they cannot change unseen: a commit changed or added after the
commit gate is approved again before the story lands.

An approval does not reach everything. It does not cover:

- the first message and author a session gives a commit;
- commits a session adds on top of the commit your answer names, before the
  commit gate passes again;
- commits after the commit gate that change the root `CODEMAP.md` alone;
- a story without a worktree, with `git.worktrees` set to false;
- a story whose commit gate printed `no range recorded`: read that story's
  commits yourself before it lands;
- a tier or a path that waits only in the settings trunk has committed, and
  not in your main checkout's or the worktree's own copy: the landing holds
  the story to its commits, and a commit gate run again does not;
- a commit gate recorded by an older `sdlc`.

**Read more.**
[The pause before the commit](guides/human-approval-gates.md#the-pause-before-the-commit).

### SDLC-E0038

**What happened.** You started a new story while this checkout is not on trunk:
it is on another branch, or on no branch.

**Why.** This check is made only with `git.worktrees` set to false. In that
mode every story is committed straight to trunk, so a story begun anywhere else
would not land where its record says.

**What to do.** Switch back to trunk and start again:

```console
$ git switch <trunk>
$ sdlc start
```

If the name `sdlc` uses is not your trunk's, correct `git.trunk_branch` in
`.sdlc/config.json` first. A story that is already under way is not held to
this, and is picked up as it is.

With `git.worktrees` on, which is the default, the branch this checkout is on
does not matter: each new story gets a worktree cut from trunk.

**Read more.**
[With worktrees turned off](the-loop.md#with-worktrees-turned-off).

### SDLC-E0039

**What happened.** You started a new story while the checkout holds uncommitted
work that belongs to no story. The refusal names what it found.

**Why.** This check is made only with `git.worktrees` set to false. In that
mode a story is committed with everything in the working tree, so work left
lying around would be reviewed and committed as part of whatever starts next.

**What to do.** Commit it, stash it, or discard it, then start again:

```console
$ git stash --include-untracked
$ sdlc start
```

The loop's own files do not count: everything under `.sdlc/`, and the backlog.
Straight after `sdlc init`, what is left is usually the contract section it
added to `CLAUDE.md`, or to `AGENTS.md`, so commit that.

If `sdlc start` first said it passed over a story whose checkout no longer
exists, don't discard anything: the work may be that story's.
[SDLC-E0051](#sdlc-e0051) says how to pick it up again.

> [!WARNING]
> Leave `-x` out when you clear untracked files with `git clean`. The story
> worktrees under `.sdlc/worktrees/` are ignored by git, and `git clean -x`
> with `-f` given twice removes every one of them, uncommitted work included.

**Read more.**
[With worktrees turned off](the-loop.md#with-worktrees-turned-off).

### SDLC-E0040

**What happened.** You started a story while your trunk is behind `origin`.

**Why.** This check is made only with `git.remote` on. A story started on an
old trunk is planned, tested and reviewed against code that has already
changed.

**What to do.** Bring trunk up to date, then start again. With trunk checked
out:

```console
$ git pull --rebase
$ sdlc start
```

If this checkout is on another branch, update trunk without checking it out:

```console
$ git fetch origin <trunk>:<trunk>
```

When `origin` cannot be reached at all, `sdlc start` says so and goes ahead.
Working offline is not the same as being behind.

**Read more.** [`git`](configuration.md#git).

### SDLC-E0041

**What happened.** Trunk failed its smoke check, `commands.smoke`, before the
story had changed anything. The refusal carries the end of what the check
printed.

**Why.** A story started on a broken trunk cannot tell its own failures from
the ones that were already there.

**What to do.** Fix trunk, or revert the commit that broke it, and start again.

Often trunk is fine and the check fails for another reason. It runs in the
story's new worktree, and a new worktree has only what trunk commits: no
installed dependencies, no `node_modules`, no virtual environment and no
generated files. Have the smoke command install what it needs, and commit that
to trunk:

```json
"smoke": "npm ci && npm run build --silent"
```

Commit the change before you start again. The new worktree reads the
`.sdlc/config.json` that trunk commits, not the copy in your directory. To turn
the check off, leave `commands.smoke` empty.

A failed check removes the worktree and the branch it made. If it could not,
the message says what is left, and [SDLC-E0053](#sdlc-e0053) says how to take
it away.

**Read more.**
[Give a new worktree what it needs](guides/tuning-commands-for-your-stack.md#give-a-new-worktree-what-it-needs).

### SDLC-E0042

**Retired.** Nothing prints this code any more. It refused a change that was
over the loop's size limit, and that limit is gone. The number is not given to
any other problem.

### SDLC-E0043

**What happened.** The story has test files that the freeze does not cover. The
message names them.

**Why.** A test that arrives after the freeze was not written before the code,
and nothing says it was not written to pass. The gates that check the freeze
refuse while one is there.

**What to do.** In the story's worktree, do one of these:

- **Remove the files**, if the story does not need them.
- **Freeze them with the rest.** Lift the freeze and have the tests frozen
  again: type `/sdlc:unfreeze` in Claude Code, or run
  `sdlc unfreeze --reason "<why>"` in your own terminal. If the loop handed the
  story to you, answer with `/sdlc:approve <ID>`, or `sdlc approve <ID>`, and
  type `/sdlc:next <ID>` first: `sdlc unfreeze` needs the story to be running.
- **Run `sdlc freeze` again**, if your project sets
  `freeze.allow_new_test_files`. It adds the new files to the freeze. Where
  the story has a test reviewer, have it review the tests with the new files
  among them first: until it approves, the freeze refuses with
  [SDLC-E0029](#sdlc-e0029) or [SDLC-E0030](#sdlc-e0030).

A test file that only trunk changed while the story was under way is not held
against the story. When this code names trunk's files, one of these is true:

- **The story changed, or took away, one of trunk's test files.** Put it back
  as trunk has it, and commit:

  ```console
  $ git checkout <trunk> -- <path>
  ```

- **The reason says the story's record has no list of its own commits, or
  names a commit of the story's that trunk holds.** `sdlc` then cannot tell
  trunk's test files from the story's. The story was started with an older
  version of `sdlc`, or trunk came to hold a commit the story made. Check that
  each file named is trunk's own and is to stay, then lift the freeze and have
  the tests frozen again. One unfreeze and one freeze are enough.

At landing the choices are the same. To leave the files out, take them off the
story's branch and run `sdlc stop` again. To keep them, reopen the story first,
as [SDLC-E0025](#sdlc-e0025) shows.

**Read more.**
[Adding a test file after the freeze](guides/when-a-frozen-test-is-wrong.md#adding-a-test-file-after-the-freeze).

### SDLC-E0044

**What happened.** The first gate cannot pass, because the story has no
acceptance criteria.

**Why.** The tests are written from the story's criteria, and every later gate
checks the work against them. With none, there is nothing to test.

**What to do.** Write the story's `acceptance_criteria` in your backlog, each
one a behaviour that can be observed. A criterion with an empty `text` does not
count. When it is not clear what the criteria should be, the session records
the gate as failed and hands the question to you.

**Read more.**
[Write criteria that can be tested](guides/writing-stories.md#write-criteria-that-can-be-tested).

### SDLC-E0045

**What happened.** `.sdlc/config.json` is valid JSON, and a setting in it holds
a value `sdlc` cannot use. The message names every such setting at once.

**Why.** A setting `sdlc` cannot use is one it cannot enforce, so the file is
refused rather than read without it.

**What to do.** Change each setting the message names, in your project on
trunk, then check the file:

```console
$ sdlc doctor
```

These are the usual ones:

| The message names | What to do |
| --- | --- |
| A `version` higher than this `sdlc` reads | A newer release wrote the file. Update `sdlc`, and leave the number alone. |
| A `version` of `0` or less | Set it to `1`, or leave it out. |
| A `backlog.path` that leaves the repository | Give the path relative to the repository's root, with no `..`. |
| A negative limit, threshold or budget | Use `0` to turn one off. |
| An entry in `budget.gate_hours` | Use a gate's name as the key, spelled exactly, and a number of hours, `0` or more. |
| An entry in a list of paths | `sdlc doctor` names the entry and what it takes. |
| An entry of `human_gates.pre_commit_pause_paths` | Give a path or a glob from the repository's root, with forward slashes, as in `"internal/hook/**"`. An entry that is empty, starts with `/`, or holds a backslash or `..` would match no file, so the pause you listed would be no pause at all. |

**At landing**, the code has two more causes, and both are yours:

- **Trunk is not a branch here.** There is nothing to land the story on. Make
  the branch again, or set `git.trunk_branch` in your project to the branch
  you land on.
- **The settings trunk commits could not be read.** The landing takes its
  commands from trunk. Commit a `.sdlc/config.json` on trunk that `sdlc` can
  read.

Then run `sdlc stop` in the story's worktree again.

**Read more.** [When a setting is wrong](configuration.md#when-a-setting-is-wrong).

### SDLC-E0046

**What happened.** A commit was made in the story's checkout before the story
reached its commit gate. The message reads
`HEAD has moved since the story started`.

**Why.** A story's work is committed at the commit gate. A commit before it
reached the branch with no gate passed, and the reviewers would not see that
work in the change they read.

**What to do.** The session records the gate as failed and hands the story to
you. Decide whether the new commits stay: a fix of your own, or a rebase onto
trunk, usually does. Keep them or revert them, then answer in the story's
worktree with `/sdlc:approve <ID>`, or `sdlc approve <ID>`, and type
`/sdlc:next <ID>`. The story is picked up from where its branch now is.

If you moved the branch yourself and nobody handed the story over, put the
story down and pick it up again, in your own terminal:

```console
$ sdlc stop
$ sdlc start <ID>
```

The story's own commit, made once its code review has passed, is not a move.

**Read more.**
[Take over from the loop](guides/stopping-and-resuming-a-story.md#take-over-from-the-loop).

### SDLC-E0047

**What happened.** `sdlc log` could not tell where you had read up to. The
commit you last acknowledged is one git does not have.

**Why.** History was rewritten, or this clone never had that commit. The log
stops rather than list every story again and bury the new ones.

**What to do.** Acknowledge a commit git does have. Type `/sdlc:ack` in Claude
Code, or run this in your own terminal:

```console
$ sdlc ack --through <commit>
```

Until then, `sdlc log --since <commit>` starts after a commit you name, and
`sdlc log --all` lists every story. Removing `.sdlc/state/acknowledged` also
makes the log start from the beginning.

**Read more.**
[When the log will not start](guides/reviewing-what-landed-on-trunk.md#when-the-log-will-not-start).

### SDLC-E0048

**What happened.** An `sdlc` command that changes a story ran in a Claude Code
session other than the one working that story. Nothing was changed.

**Why.** One session works one story. This keeps one session's commands out of
a story another session is working. It guards against accidents; it is not a
barrier against a session set on getting round it.

**What to do.** To move the story to this session, type `/sdlc:next` in it. The
session takes the story over, and from then on the other session is refused
instead.

To answer for the story yourself without moving it, type `/sdlc:approve` or
`/sdlc:unfreeze` in Claude Code, or run `sdlc approve`, `sdlc unfreeze` or
`sdlc stop` in your own terminal.

**Read more.**
[Which checkout holds a story](the-loop.md#which-checkout-holds-a-story).

### SDLC-E0049

**What happened.** The note given to the commit gate names a commit that is not
among the commits the gate records. Nothing was recorded.

**Why.** The note and the record would then say two different things about
which commits are the story's.

**What to do.** Usually nothing: the session runs the gate again with the hash
corrected or left out. The note needs no hash at all, because the gate records
the commits itself:

```console
$ sdlc gate commit pass --note "on trunk"
```

**Read more.** [`sdlc gate`](commands.md#sdlc-gate-gate-status).

### SDLC-E0050

**What happened.** You named a story in a checkout that does not hold it. The
`why` line names the checkout that does. Nothing was changed.

**Why.** A story is held by one checkout at a time: the one that started it,
which is its own worktree. What a command records is about the work in that
checkout, so it has to run there.

**What to do.** Run the command in the checkout the refusal names:

```console
$ cd .sdlc/worktrees/<ID>
```

In Claude Code, type the slash command in a session opened in that directory.
`/sdlc:next <ID>` is the exception, and works from anywhere.

Less common cases:

- **You want the story in this checkout instead.** Put it down where it is,
  release it, and start it here.
  [Move a story to another checkout](guides/stopping-and-resuming-a-story.md#move-a-story-to-another-checkout)
  shows how. Releasing leaves behind anything the other checkout has not
  committed.
- **This session already holds a story in another checkout.** One session works
  one story. Open a new Claude Code session here for the second one.
- **The message says this checkout names a story that another checkout
  holds.** This checkout has a stale note of the story. Run `sdlc stop` here to
  clear it. The story carries on where it is held.
- **The message lists several checkouts that each name the story.** None of
  them holds it until you decide. Keep the one whose session you recognise, and
  run `sdlc stop` in each of the others.

**Read more.**
[Which checkout holds a story](the-loop.md#which-checkout-holds-a-story).

### SDLC-E0051

**What happened.** The checkout that holds the story is gone. Its directory no
longer exists, or is no longer a checkout of this repository. Nothing was
changed.

**Why.** A story stays with its checkout until it is done, dropped or released,
because its uncommitted work is there. `sdlc` cannot tell a deleted worktree
from one that is only missing for now, so no checkout can start the story, or
record anything for it, until you let it go.

**What to do.** Release the story. Type `/sdlc:release <ID> --reason "<why>"`
in Claude Code, or run this in your own terminal, in any checkout:

```console
$ sdlc release <ID> --reason "the worktree was deleted with its disk"
```

Then run the command that was refused: `/sdlc:next <ID>` to carry on, or your
answer if the story waits for one.

Two things to know:

- **A checkout you moved or copied while its story was stopped** is in the same
  position. Release the story, then start it again in the moved checkout. Any
  uncommitted work you find there is the story's, so don't discard it.
- **If the story's tests were frozen** and you start it in a fresh checkout,
  every test file is treated as frozen until the freeze is lifted and taken
  again there.

**Read more.**
[Move a story to another checkout](guides/stopping-and-resuming-a-story.md#move-a-story-to-another-checkout).

### SDLC-E0052

**What happened.** This repository has no main checkout for `sdlc` to keep the
backlog in. The message says which check failed.

**Why.** The backlog and every story's records live in the repository's main
checkout. Some repositories have none that `sdlc` can find:

- a bare repository;
- a repository made with `git init --separate-git-dir`, when you are in one of
  its linked worktrees;
- a worktree of a submodule.

**What to do.** Work in a clone made without `--bare`, or in the main checkout
of a repository made with `--separate-git-dir`. Until then every command that
reads the backlog is refused. `sdlc stop` still works, to end an iteration.

**Read more.** [More than one checkout](the-loop.md#more-than-one-checkout).

### SDLC-E0053

**What happened.** `sdlc start` could not make the new story's worktree.
Nothing was started, and what it had made was removed again.

**Why.** Each new story begins in a worktree of its own, at
`.sdlc/worktrees/<ID>`, on a new branch `story/<ID>` cut from trunk. Something
was in the way, and the first line of the message says what.

**What to do.** Follow the fix for the cause the message names, in your
project, then start again:

| The message says | What to do |
| --- | --- |
| Trunk has no `.sdlc/config.json` committed | Commit the files `sdlc init` wrote. This is the usual first run. The `fix` line names the files. |
| The `.sdlc/config.json` trunk commits cannot be read | Correct it, and commit it to trunk. |
| Trunk is not a branch with a commit here | Make the branch `git.trunk_branch` names, with a commit on it, or set `git.trunk_branch` to your trunk. |
| Git will not take `story/<ID>` as a branch name | Rename the story. An id such as `A..B`, or one that ends in `.lock`, cannot be a branch. |
| `.sdlc/worktrees`, or the story's directory in it, is a file or a link | Move it out of the way. |
| `.sdlc/worktrees/.gitignore` does not hide the worktrees | Put a line `*` in it, and nothing that starts with `!`, or remove the file. |
| The story's directory already has files in it | Move them somewhere else. `start` never writes over what it did not make. |
| The backlog kept changing while the worktree was being made | Run `sdlc start` again. |
| The story's branch or worktree is already there | Remove the leftover, as below. |

The first row is the one most people meet:

```console
$ git add .sdlc user_stories.json CLAUDE.md REVIEW.md
$ git commit -m "Set up the sdlc loop"
```

The `fix` line names the file that holds your contract: `CLAUDE.md`,
`AGENTS.md`, or both. Commit the one it names.

Setting `git.worktrees` to false in `.sdlc/config.json` gets past every cause,
by working stories in your own checkout, one at a time.

**A leftover worktree or branch.** A start that was interrupted leaves a
worktree or a branch behind, and so does a story that was dropped part-way.
Look before you remove either, because a commit there may be the story's work.
In your project, with trunk checked out:

```console
$ git log --oneline <trunk>..story/<ID>
```

If that lists commits, bring them to trunk first, or keep them under another
name with `git branch -m story/<ID> <new name>`:

```console
$ git merge --ff-only story/<ID>
```

If git says it cannot fast-forward, trunk has moved since the story was cut. In
the story's worktree, run `git rebase <trunk>` and your project's tests, then
merge again.

When nothing is listed, or the work is on trunk, remove both:

```console
$ git worktree remove .sdlc/worktrees/<ID>
$ git branch -d story/<ID>
```

Both commands refuse rather than lose work, so read what they say before you
add `--force` or reach for `git branch -D`. If the message says the worktree is
on no branch, look at its commits with
`git -C .sdlc/worktrees/<ID> log --oneline <trunk>..HEAD` and keep what you
want on a branch first: a commit on no branch is lost with its worktree.

On Windows, a refused start sometimes cannot remove what it made, because a
program still has a file open there. Close it, remove the worktree and the
branch as above, and then follow the fix.

**Read more.** [Removing a worktree](the-loop.md#removing-a-worktree).

### SDLC-E0054

**What happened.** `sdlc stop` did not land the story, because it could not put
the story onto trunk as trunk now is. Nothing landed: trunk is unchanged, and
the story waits in its worktree.

**Why.** A landing only ever moves trunk forward to the story's commits. When
another story landed first, the stop puts this story's commits onto the new
trunk by itself. The states below are the ones it does not handle alone. Where
the commits simply do not apply, you get a conflict,
[SDLC-E0075](#sdlc-e0075), instead.

**What to do.** The message says which state it met:

- **Trunk was rewritten, or went back.** A plain rebase would carry commits
  that are not the story's. The `fix` line gives the rebase to run in the
  story's worktree, one that replays only the story's own commits. This is
  yours: the session hands the story to you.
- **The story's branch has a merge commit.** A landing does not carry a merge
  onto trunk. Rebase the branch in the worktree so that the story's commits sit
  on trunk in a line. This is yours too.
- **Trunk moved while the landing ran.** Somebody committed to it, or a command
  moved it. Run `sdlc stop` in the story's worktree again. The session does
  this by itself, up to three times, and hands the story to a person if the
  last of those stops is refused as well.

After a rebase by hand, `sdlc stop` runs your configured commands on the
result.

**Read more.** [Done when it lands](the-loop.md#done-when-it-lands).

### SDLC-E0055

**What happened.** `sdlc stop` did not land the story, because one of the
landing's checks failed. Nothing landed: trunk is unchanged, and the story
waits in its worktree.

**Why.** Before trunk moves, the stop checks that the story is still what the
gates passed, and that your commands pass on the files about to become trunk.

**What to do.** The message says which check it was, and each has its own fix.

**A configured command failed.** The message names the command, how it ended,
and the last of what it printed. This one stays with the session. It runs the
same command on trunk as it is, in a checkout that has trunk checked out:

- **It fails on trunk too.** The failure is trunk's own. Put trunk right, and
  `sdlc stop` then lands the story as it is.
- **It passes on trunk.** The story is at fault, and the session takes it back
  for rework: it records the implementation gate as failed, takes the story
  through its gates again, and stops again.

For `coverage` and `mutation`, the last number the command prints has to reach
your threshold.

**Any other check is yours.** Put right what the message names, then run
`sdlc stop` in the story's worktree again:

| The message says | What to do |
| --- | --- |
| The story's commits could not be put onto trunk, and git names no file | Most often git has nobody to make the commits as. Set `user.name` and `user.email`. |
| The worktree is part-way through a rebase, a merge, a cherry-pick, a revert or a bisect, or on no branch | Finish it or abort it in the worktree. If git names a lock file and no git command is running, remove that file first. |
| The worktree could not be put back | Run `git rebase --abort`, then `git status`. If the branch is not at the commit the message gives, run `git reset --hard <commit>`. |
| The worktree is on another branch | Keep what that branch holds, then run `git switch story/<ID>` in the worktree. |
| `story/<ID>` is gone | If the worktree still has the story's work, run `git branch story/<ID>` there and switch to it. Otherwise reopen the story. |
| A command moved a branch, or changed a tracked file | Change the command so that it only checks. A formatter that writes belongs in `fmt`, not in `fmt_check`. |
| The story's commits change something under `.sdlc/`, or the backlog | Take those changes out of the story's commits, and make them on trunk yourself. No story lands the loop's own files. |
| The story's commits change the review policy | Revert the file on the story's branch. Change the review policy on trunk, between stories. |
| The story's commits change the root `CODEMAP.md` into something other than a plain file | Take that change out of the story's commits. |
| Trunk already holds the story's commits, with a merge among them | The story was brought in by hand. Decide what trunk should hold. |
| The story `changed while this stop waited its turn to land` | Nothing needs putting right. `sdlc status` in the worktree says where the story stands now. |
| The worktree `is gone`, and the story has not landed | Run `sdlc status` and `sdlc doctor` in your project to see where the story and its branch stand. Remove nothing yet. |

To reopen a story instead of fixing it in place, answer the hand-over, record
the gate as failed in the worktree, and type `/sdlc:next <ID>`:

```console
$ sdlc gate implementation fail --story <ID> --note "the tests fail on trunk's new API"
```

> [!WARNING]
> When you run `sdlc stop` yourself, it runs the story's code and your
> repository's git settings with your own privileges. Read the story's changes
> first.

**Merge and diff drivers with unusual names.** One refusal under this code is
about your git configuration and not about the story. It reads, with the names
from your own repository in place of the angle brackets:

```text
the story cannot land while <merge or diff> drivers are configured under names that hold an
equals sign, "{sdlc-private}" or a byte that is not UTF-8: "<key>"

sdlc stops a repository's merge and diff drivers from deciding what lands by overriding their
settings on git's command line. That command line ends a setting's name at its first equals
sign, sdlc replaces "{sdlc-private}" there with a path of its own, and a byte that is not
UTF-8 is not sure to reach git unchanged on every system. For a name that holds any of them
sdlc cannot be sure to override the setting it means to, so the driver's own could stay in
force and change what lands, and sdlc refuses such a name on every system alike. Git accepts
such a name; the limit is sdlc's rule, not something git reported. Nothing landed: <trunk> is
unchanged, and the iteration on <ID> is still running

a person, in the story's worktree, finds the file that holds each such driver with "git
config --show-origin --list" (it may be one that other repositories read too), renames the
driver to a name with no equals sign, no "{sdlc-private}" and no byte that is not UTF-8, or
removes it, then runs "sdlc stop" again; the attributes line that names the driver can wait
until the story has landed; an assistant hands the story to a person with "sdlc escalate" and
stops
```

Find the driver from the story's worktree, because the stop reads git's
configuration as the worktree sees it. The file may be your own `~/.gitconfig`:

```console
$ git config --show-origin --list
```

Rename the driver, or remove it, and run `sdlc stop` again. A few things help:

- The message shows a byte that is not UTF-8 as an escape, such as `\xff`. Git
  prints the byte itself, so search for the part of the name before it. A name
  saved in an older encoding such as Latin-1 is caught as well: save the file
  as UTF-8, or rename the driver.
- Leave the `merge=<name>` or `diff=<name>` line in `.gitattributes` alone
  until the story has landed. Without the driver, git uses its built-in merge
  and diff for those paths, which is all the landing needs. Changing a tracked
  `.gitattributes` in the worktree would be a change no reviewer saw.
- A diff driver with such a name is refused by every stop. A merge driver is
  refused only when the stop has to put the story onto a trunk that moved. A
  repository with both can meet the refusal once for each.

**Read more.** [Done when it lands](the-loop.md#done-when-it-lands).

### SDLC-E0056

**What happened.** `sdlc stop` did not land the story, because a checkout that
has trunk checked out holds something the landing would write over. That is
usually your project's own directory. Nothing landed, and nothing was written
anywhere.

**Why.** When trunk moves, the files of every checkout that has trunk checked
out move with it. `sdlc` will not write over work you have not committed.

**What to do.** The message names the checkout, and what is in the way:

- **Uncommitted changes, or untracked files, where the story's commits
  write.** In that checkout, stash them or commit them. A commit moves trunk,
  and the next stop puts the story onto it before it lands.
- **A merge, a cherry-pick, a revert, a rebase or a bisect left part-way.**
  Finish it or abort it there.
- **A locked index.** If no git command is running in that checkout, remove the
  lock file the message names.

Then run `sdlc stop` in the story's worktree again. These changes are yours, so
the session does not touch them: it hands the story to you.

**Read more.**
[When the story is finished](guides/stopping-and-resuming-a-story.md#when-the-story-is-finished).

### SDLC-E0057

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a story id the backlog does not have. The number is not given to any
other problem.

### SDLC-E0058

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a story that could not be started. The number is not given to any other
problem.

### SDLC-E0059

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a story that another checkout was working. The number is not given to
any other problem.

### SDLC-E0060

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a story id named twice. The number is not given to any other problem.

### SDLC-E0061

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused to run from inside a Claude Code session. The number is not given to
any other problem.

### SDLC-E0062

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused to run with `git.worktrees` off. The number is not given to any other
problem.

### SDLC-E0063

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a `--max` that was not a whole number of 1 or more. The number is not
given to any other problem.

### SDLC-E0064

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a story that was waiting for a person's answer. The number is not given
to any other problem.

### SDLC-E0065

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a story whose gates had all passed. The number is not given to any
other problem.

### SDLC-E0066

**Retired.** The `sdlc parallel` command, which printed this code, is gone. It
refused a list of tools wider than the built-in one. The number is not given to
any other problem.

### SDLC-E0067

**What happened.** A command that records or changes a story's staffing was run
in a project where the project manager is turned off. Nothing was changed.

**Why.** The staffing is the project manager's list of which agents a story
needs. With the project manager off there is none: every agent that is turned
on takes part in every story.

**What to do.** Nothing, unless you want stories staffed. If you do, delete
`team.agents.project-manager.enabled` in `.sdlc/config.json`, or set it to
`true`, in your project on trunk.

**Read more.** [`team`](configuration.md#team).

### SDLC-E0068

**What happened.** The story has no staffing recorded yet, and a command needed
one. The analysis gate does not pass until there is one.

**Why.** With the project manager on, each story is staffed before its work
begins: which agents it needs, and the model and effort each is asked for.

**What to do.** Usually nothing: the session asks the project manager, which
records the staffing. Until it has, every agent that is turned on is needed.

**Read more.** [Agents](the-agents.md).

### SDLC-E0069

**What happened.** The project manager recorded a staffing that is not whole,
or recorded one for a story that already has one. Nothing was recorded.

**Why.** A staffing names every agent that is turned on, once, says whether the
story needs it and why, and gives a model and an effort for each one it needs.
The message says which part was missing.

**What to do.** Usually nothing: the project manager gives the whole staffing
again. A staffing that is already recorded is changed one agent at a time, with
`sdlc staffing amend <agent> --reason "<why>"`.

**Read more.** [`sdlc staffing`](commands.md#sdlc-staffing).

### SDLC-E0070

**What happened.** A staffing left out an agent that is never left out. Nothing
was recorded.

**Why.** Some agents take part in every story: the ones that do a gate's work,
the verifier, the code reviewer, and security on a story that is
security-sensitive. Security also stays in until the analysis has said the
story is not security-sensitive.

**What to do.** Usually nothing: the project manager keeps the agent. To spend
less on it, it asks for a lower model or effort instead.

**Read more.**
[What the project manager cannot leave out](configuration.md#what-the-project-manager-cannot-leave-out).

### SDLC-E0071

**What happened.** A staffing asked for a model or an effort that your settings
do not allow. Nothing was recorded.

**Why.** `team.models` and `team.efforts` list what may be asked for, and an
agent's `lowest` sets a floor under it. The names are compared exactly, and the
message lists what is allowed for that agent.

**What to do.** Usually nothing: the project manager asks for one of those. To
allow something else, change the lists in the `team` section of
`.sdlc/config.json`, in your project on trunk.

**Read more.**
[Models, efforts and floors](configuration.md#models-efforts-and-floors).

### SDLC-E0072

**What happened.** A staffing would change something you assigned to an agent
yourself. Nothing was recorded.

**Why.** What a person chose for an agent stays as it is. The project manager
works around it.

**What to do.** Usually nothing: the project manager leaves that agent out of
its change. Only you can change it: type `/sdlc:assign` in Claude Code, or run
`sdlc staffing assign` in your own terminal.

**Read more.** [`sdlc staffing assign`](commands.md#sdlc-staffing-assign-agent).

### SDLC-E0073

**What happened.** A staffing would leave out a reviewer whose latest review is
a block, at a gate that has not passed. Nothing was recorded.

**Why.** Leaving the reviewer out would let the gate pass over its block.

**What to do.** Usually nothing: the session takes the reviewer's findings
through rework and has it review again. Where the gate has gone to the
reviewer's [arbiter](the-loop.md#the-arbiter), it is the arbiter that reviews
again, and the `fix` line says so. If the block itself is wrong, the session
hands the story to you. You then answer the reviewer, or record the gate as
failed.

**Read more.** [When a reviewer blocks](guides/when-a-reviewer-blocks.md).

### SDLC-E0074

**What happened.** `sdlc staffing assign` was run from inside a Claude Code
session. Nothing was recorded.

**Why.** Choosing an agent's model or effort by hand is a person's decision.

**What to do.** Type `/sdlc:assign` in Claude Code, or run
`sdlc staffing assign` in your own terminal. A session that meets this stops
and says what it would have asked for.

**Read more.** [Your own terminal](#your-own-terminal).

### SDLC-E0075

**What happened.** `sdlc stop` did not land the story, because trunk has moved
on and the story's commits no longer apply onto it. Git stopped on a conflict
in the files the message names. Nothing landed: trunk is unchanged, and the
story waits in its worktree.

**Why.** The stop tried to put the story onto the new trunk by itself, met the
conflict, and put the branch back exactly where it was. Resolving a conflict is
a judgement about two changes, so it is yours.

**What to do.** In your own terminal, rebase the story onto trunk:

```console
$ cd .sdlc/worktrees/<ID>
$ git rebase <trunk>
```

Resolve each file git stops on, `git add` it, and carry the rebase on as git
says until it finishes. Then answer the hand-over, if the session handed the
story to you, and land the story:

```console
$ sdlc approve <ID>
$ sdlc stop
```

Answer after you resolve, not before, so that your approval is of the work as
it now is. If you ran the stop yourself there is no question to answer, and you
leave that line out. `sdlc stop` runs your configured commands on the result.

> [!WARNING]
> That rebase is git's own. It runs the hooks, merge drivers, editor and
> filters your repository's configuration names, with your privileges. Read
> `git config --show-origin --list` and `.gitattributes` in the worktree first.

Two things can follow. Where the story waits for you, by its tier or a path
its change touches, the rebased work is approved again, and a stop with no
question open says so with [SDLC-E0037](#sdlc-e0037). And where the rebase
changed what the story's commits do, the reviewers look again, as
[SDLC-E0029](#sdlc-e0029) describes.

The session does not resolve a conflict, and it does not abandon a rebase it
finds part-way, which may be yours.

**Read more.** [Done when it lands](the-loop.md#done-when-it-lands).

### SDLC-E0076

**What happened.** The story landed, and its change to the root `CODEMAP.md`
did not reach trunk. `sdlc stop` prints this as a warning and still succeeds:
the story's commits are on trunk and the story is done. Nothing about the
landing needs doing again.

**Why.** A story's sections of `CODEMAP.md` are put on trunk in a commit of
their own, after the landing. Here that could not be done, so the sections are
kept for you to deal with. Until you have, every `sdlc` command in the
repository begins with a line that names the story.

**What to do.** The message prints the path of the kept file, under
`.git/sdlc/codemap/` in your project. Read it first: the commit is made from
it, and no reviewer has read it. Then choose, in your project. Each of these is
your command, so type the slash command in Claude Code or run the other in your
own terminal:

- `/sdlc:remap <ID>`, or `sdlc remap <ID>`, makes the change, once what was in
  the way is put right.
- `/sdlc:remap <ID> --replace`, or `sdlc remap <ID> --replace`, puts the kept
  sections over what trunk's map has under the same headings.
- `/sdlc:remap <ID> --drop`, or `sdlc remap <ID> --drop`, discards the kept
  change and commits nothing.

Which one fits depends on what the message says was in the way:

| The message says | What to do |
| --- | --- |
| A section is no longer what the bookkeeper read | Another story, or you, changed that section first. Choose `--replace` to put the kept text over it, or edit `CODEMAP.md` by hand and `--drop`. |
| Trunk's map already holds every kept section | There is nothing to commit. `--drop` removes the kept copy and loses nothing. |
| Trunk's map already holds one section, among others it names | Only that one is on trunk. `--drop` would throw the others away. |
| A section of trunk's map cannot be told where it ends | `--replace` is refused as well. Edit `CODEMAP.md` by hand, commit it, and `--drop`. |
| Uncommitted changes to `CODEMAP.md`, no committer configured, or something that is not a plain file where `CODEMAP.md` goes | Put that right, then run the plain form. `--replace` does not help, and `--drop` would throw the change away. |
| The kept file is no longer a set of sections | It was changed after it was stored. Put it right and run the plain form, or `--drop`. |

The session cannot make this choice for you. It tells you what the stop
printed, and does not stop again or hand the story over: the story is done.

**Read more.**
[Parallel stories and `CODEMAP.md`](the-loop.md#parallel-stories-and-codemapmd).

### SDLC-E0077

**What happened.** `sdlc remap` was asked to repair a story that has nothing to
repair. Nothing was changed.

**Why.** The repair is only for a story that has landed while its change to
`CODEMAP.md` is still waiting. This story has not landed, has no change kept,
or already has its change on trunk.

**What to do.** Nothing. `sdlc status` begins with a line for each landed story
whose change is waiting, and names the command to run for it. Where it prints
none, `--replace` and `--drop` have nothing to act on either.

If the message says a kept file is left over from a commit that was already
made, `--drop` removes the file and commits nothing.

**Read more.** [`sdlc remap`](commands.md#sdlc-remap-story).

### SDLC-E0078

**What happened.** The story's sections for the root `CODEMAP.md` were refused.
Nothing was stored, and whatever was stored before is still there.

**Why.** The sections reach trunk with no reviewer, so they are held to one
strict form, and `sdlc` never repairs them. The message names the section and
what is wrong with it.

**What to do.** Usually nothing: the bookkeeper sends the whole set again. Each
section is:

1. a line that is exactly `<!-- sdlc:add -->` or `<!-- sdlc:replace -->`;
2. its heading, as `##`, a space and the text;
3. at least one line under it, with no heading at that level or above, no
   fenced block left open, no comment left open and no conflict marker.

No heading may appear twice. The text is sent from a Bash heredoc with a quoted
delimiter, because a PowerShell here-string adds carriage returns, which are
refused. A story worked without a worktree of its own is refused whatever it
sends: its bookkeeper edits `CODEMAP.md` in place.

`sdlc remap` prints this code too, when the file kept for a landed story was
changed after it was stored. Put the file right and run the repair again, or
discard it with `--drop`. `--replace` reads the same file, and is refused in
the same way.

**Read more.** [`sdlc codemap write`](commands.md#sdlc-codemap-write).

### SDLC-E0079

**What happened.** One of your six commands ran in a process that Claude Code
started. Nothing was read or changed.

**Why.** These commands are a person's decision, and inside a process Claude
Code started `sdlc` cannot tell you from the agent. That covers the agent's own
shell and anything it runs, a `!` line typed in a session that was started from
a terminal, and a terminal app or a `tmux` server that Claude Code opened. The
`why` line names the environment variable that gave it away.

**What to do.** Give the command in one of these ways instead:

- Type its slash command, in Claude Code or in Claude Desktop, with the same
  arguments.
- In Claude Desktop, you may also type it as a `!` line at the prompt, as in
  `!sdlc approve <ID>`.
- Run it in a terminal outside Claude Code: a window you opened yourself.

| In Claude Code or Claude Desktop | In your own terminal |
| --- | --- |
| `/sdlc:approve <ID>` | `sdlc approve <ID>` |
| `/sdlc:unfreeze` | `sdlc unfreeze --reason "<why>"` |
| `/sdlc:ack` | `sdlc ack --through <commit>` |
| `/sdlc:remap <ID>` | `sdlc remap <ID>`, for a landed story whose change to `CODEMAP.md` is waiting, with `--replace` or `--drop` where its message says so |
| `/sdlc:release <ID> --reason "<why>"` | `sdlc release <ID> --reason "<why>"` |
| `/sdlc:assign` | `sdlc staffing assign <agent> --model <model> --reason "<why>"` |

A few things can still be in the way:

- **Your own terminal has no `sdlc`.** The plugin's copy is for Claude Code
  only. Install the command with `npx @bbsnly/sdlc install`. The slash commands
  need nothing installed.
- **You met this in a window you opened yourself.** The terminal app was
  started from inside Claude Code, so every window it opens is marked. Quit the
  app fully and open it again from the Dock, Finder or the Start menu, or end
  the `tmux` server. Then check that `printenv` no longer lists the variable
  the `why` line named.
- **A slash command answers that it did not run.** That is not this code: the
  hook could not tell that you typed it.
  [A slash command did not run](#a-slash-command-did-not-run) says what to
  check. In Claude Desktop, the `!` line still works.

Setting `SDLC_ENFORCE=0` does not turn this check off.

If you are an assistant, stop here and say what you would have run. Do not try
another way of running it.

**Read more.**
[The commands that are yours](claude-desktop.md#the-commands-that-are-yours).

### SDLC-E0080

**What happened.** The story's commits add the markers of an unresolved
conflict to a file. The commit gate, or `sdlc stop`, refused. Nothing was
recorded or landed.

**Why.** Conflict markers in a file mean a conflict that nobody resolved, and
landing it would put a broken file on trunk.

**What to do.** This one is yours: the session hands the story to you. The
message names each file and the lines. Do one of two things:

- **The conflict is real.** In the story's worktree, resolve it in each file,
  or take out the lines, and commit. The reviewers look at the change again.
  Then the commit gate, or `sdlc stop`, is run again.
- **The lines are meant to be there**, as in a page that shows what a conflict
  looks like, or a test fixture. List the file in
  [`paths.conflict_markers_intended`](configuration.md#pathsconflict_markers_intended)
  in `.sdlc/config.json`, in a commit on trunk, from your project. A change
  under `.sdlc/` cannot ride the story.

After the commit to trunk, what comes next depends on where you were refused:

- **At `sdlc stop`**, run it in the story's worktree again. The landing reads
  trunk's copy of the list.
- **At the commit gate**, bring the story onto that trunk commit first, with
  `git rebase <trunk>` in the worktree. The gate reads the worktree's copy. A
  gate that is then refused because the branch moved is answered as
  [SDLC-E0046](#sdlc-e0046) says.

The check looks for a conflict as git writes it: the three markers, seven
characters wide, among the lines the story added to one file. A conflict that a
later commit of the story takes out again is not counted, and nor is one the
file already had.

A pass is not a promise that no file holds conflict text. The check does not
find markers of another width, indented markers, a conflict with one of its
markers missing, or a wrong resolution that leaves no marker at all.

**Read more.**
[`paths.conflict_markers_intended`](configuration.md#pathsconflict_markers_intended).

### SDLC-E0081

**What happened.** The hook refused a shell command, because a path in it goes
through too many symbolic links for the hook to say where it leads. The command
did not run. The refusal quotes the path.

**Why.** Before a command writes anything, the hook works out where each path
really lands, following links as the filesystem does. Past 64 links, or past
the system's own limit, which is about 32 on macOS and 40 on Linux, it cannot
be sure, so it refuses rather than guess. A link that leads back to itself is
refused in the same way.

**What to do.** Name the place the path leads to directly, without the links.
To find that place, follow the links one at a time, running this again on what
it prints:

```console
$ readlink <link>
```

In PowerShell, use `(Get-Item <link>).Target`. Tools that resolve a whole path
in one go, such as `realpath`, fail on these paths.

If the refusal names your working directory, every command is refused there:
`cd` to a directory with a shorter path and run the command again. If a chain
of links that long is not one you made, find out who made it before you write
through it.

**Read more.** [`too-many-links`](enforcement.md#too-many-links).

### SDLC-E0082

**What happened.** The hook refused a shell command, because its paths together
go through more symbolic links than the hook follows for one command, which is
512. The command did not run. The refusal quotes the path that took the count
past the limit.

**Why.** The hook follows the links of every path a command writes. A command
with very many linked paths would hold the hook until it ran out of time, so
the links are counted instead. It is a count of links, not a time.

**What to do.** Split the command into several, each naming fewer paths that go
through links, or name each place directly.

**Read more.** [`too-many-links`](enforcement.md#too-many-links).

### SDLC-E0083

**What happened.** The hook refused a shell command, because a path in it is
longer, in full, than the system looks up in one go: about 1,024 bytes on macOS
and 4,096 on Linux. The command did not run.

**Why.** The hook works out where a path leads by looking it up from the root
of the disk. A path too long to look up could lead anywhere, so the hook
refuses rather than guess.

**What to do.** Make the full path shorter, by moving or renaming the
directories on the way to it, and name it again. Running the command from a
directory nearer the file does not help, because the hook reads the full path
however the command names it.

If your working directory's own path is that long, every command is refused
there: `cd` to a directory with a shorter path first.

**Read more.** [`path-too-long`](enforcement.md#path-too-long).

### SDLC-E0084

**What happened.** A reviewer asked what changed in the plan since its last
review, and `sdlc artifact diff plan` could not show it.

**Why.** The diff starts from the plan as that reviewer last saw it, and that
copy is not there to compare with. A diff from anything else could hide a
change from the reviewer, so none is given.

**What to do.** Nothing is wrong with the story. The reviewer reads the whole
plan this round, and its verdict counts the same. You will see one round of
this for a story that was under way when you updated `sdlc`.

One reason deserves a look. If the `why` line says
`the file no longer holds the bytes its name is the hash of`, the kept copy of
the plan was changed after `sdlc` stored it. Find out what changed it before
you trust the round.

**Read more.** [`sdlc artifact diff plan`](commands.md#sdlc-artifact-diff-plan).

### SDLC-E0085

**What happened.** `sdlc freeze` did not freeze the tests, because of one of
the two commands it runs first. One is the check your project sets,
`commands.freeze_check`. The other is a run of the new and changed tests alone,
with `commands.test_files`, or with `commands.test` where that is empty.
Nothing was frozen, and the story is where it was.

**Why.** The freeze check judges the new test files before they are frozen, so
that a test that does not compile or lint is corrected while it still can be.
The run of the new tests asks one thing more: a test written before its code
has to fail.

**What to do.** The message says which of these happened:

- **The check failed.** The message shows the last 40 lines it printed. The
  test author corrects the files the check names and runs `sdlc freeze` again.
  Nobody needs a person for that. If the output says a program was not found,
  or the check fails whatever the tests hold, no correction to a test clears
  it: install the program, or change `commands.freeze_check` yourself.
- **The check changed a test file.** A freeze holds what the check judged, so
  the files must be the same afterwards. A formatter that rewrites files
  belongs in the test author's own run, before the freeze. The author looks at
  what changed and runs `sdlc freeze` again. A check that rewrites a file on
  every run never settles: change the check.
- **The new tests passed.** A test written before its code fails until the
  code exists, so tests that pass test nothing yet. Everything the command
  printed is in `.sdlc/stories/<ID>/freeze-run.log`, and its last 20 lines
  follow the message. The test author makes each new test fail for the reason
  it is there, and runs `sdlc freeze` again. If the command runs tests other
  than the ones `SDLC_TEST_FILES` lists, other tests decide how it ends:
  correct `commands.test_files` yourself.
- **The run of the new tests could not run.** It could not be started, or it
  ended with the shell's status 126 or 127, or by a signal. That says nothing
  about the tests, so it is not taken for tests that fail. Put the program,
  the shell or the command right, and freeze again.
- **A test file's name holds a line break**, or another character that splits
  a line. Each command is given one name to a line, so rename the file with
  `git mv` and freeze again.
- **There is no shell to run a command in.** Both commands run under `sh` or
  `bash`, and neither is on your `PATH`. On Windows, Git Bash provides them.
- **The run could not be set up in the temporary directory.** Each run has a
  directory of its own there, and the list of test files is written in it.
  Make the temporary directory writable and free some room in it. `TMPDIR`
  names it, or `TMP` or `TEMP` on Windows.
- **A command ran `sdlc freeze` itself**, or `SDLC_FREEZE_FILES` or
  `SDLC_RUN_DIR` was already set. Take `sdlc freeze` out of the command, or
  unset the variable.
- **The story, or its freeze, changed while the commands ran.** Run
  `sdlc status` to see which story is being worked on, and freeze again only
  if it is the one you meant.

**Read more.** [The freeze check](configuration.md#the-freeze-check) and
[the narrow test command](configuration.md#the-narrow-test-command).

### SDLC-E0086

**What happened.** A reviewer recorded a block without counting an important
finding. Nothing was recorded.

**Why.** A block costs a whole round of rework. Under the
[review policy](the-loop.md#the-review-policy), it has to name at least one
important finding. A review with only nits is an approve, with the nits listed.

**What to do.** Usually nothing: the reviewer records its review again. For a
block, it passes `--important` with the number of important findings. For a
review with only nits, it records `approve` with `--nits`.

If the message is `unknown flag: --important`, that is
[SDLC-E0034](#sdlc-e0034): your `sdlc` command is older than the plugin.

**Read more.** [`sdlc review add`](commands.md#sdlc-review-add-gate-role-verdict).

### SDLC-E0087

**What happened.** `.sdlc/config.json` still has a setting that the `team`
section replaced: something under `staffing`, or `reviews.gate7_advisory` set
to `true`. Every command that reads the file refuses.

**Why.** Reading the file without the old setting would run your agents
differently from what the file says.

**What to do.** Move each setting the message names to its key under `team`,
and delete the old one. Do this in your project, on trunk:

- Delete the whole `staffing` section once its keys have moved. An empty
  `"staffing": {}` is still refused.
- Don't run `sdlc init --force` for this. It writes the defaults over your own
  settings.
- With a story under way, edit the file on trunk and not in the story's
  worktree. An edit there only moves the refusal to the landing.
- A long message stops after about 2,000 characters and counts the rest, as in
  `; and 12 more`. Fix the ones it names and run the command again to see the
  others.

**Read more.**
[Upgrading from the staffing section](configuration.md#upgrading-from-the-staffing-section).

### SDLC-E0088

**What happened.** The `team` section of `.sdlc/config.json` cannot be used.
The message lists every problem at once.

**Why.** The section names an agent that does not exist, names one twice, or
holds a value outside what the setting takes: a `lowest` that is not in your
lists, `retries` outside 0 to 20, `instances` outside 1 to 16, or `max_rounds`
below 1.

**What to do.** Correct each problem in `.sdlc/config.json`, in your project on
trunk, and check the file:

```console
$ sdlc doctor
```

A long message stops after about 2,000 characters and counts the rest. Those
are listed once the first ones are fixed.

**Read more.** [`team`](configuration.md#team).

### SDLC-E0089

**What happened.** A review was recorded with findings that do not match its
counts. Nothing was recorded.

**Why.** At the design review and the code review, each finding a review counts
is listed with `--finding`, so that it can be closed later. A finding nobody
listed is one nobody can close.

**What to do.** Usually nothing: the reviewer records its review again, with
one `--finding "<important|nit> [path]: <text>"` for each finding. There are as
many important ones as `--important` says, and no more nits than `--nits`.

**Read more.** [`sdlc review add`](commands.md#sdlc-review-add-gate-role-verdict).

### SDLC-E0090

**What happened.** A finding was closed with `--declined` or `--referred` and
no reason. Nothing was recorded.

**Why.** Declining or referring a finding is a decision. The reviewer who
raised it and the verifier judge the reason.

**What to do.** Usually nothing: the session gives the reason in one line, with
`--reason "<why>"`, or fixes the finding and closes it with `--fixed`.

**Read more.** [The scout rule](the-loop.md#the-scout-rule).

### SDLC-E0091

**What happened.** A finding was closed with `--referred`, and it is about a
file the story changes. Nothing was recorded.

**Why.** Under the scout rule, a defect in a file the story changes is fixed in
the story, or declined with a reason. `--referred` is for something outside the
story's files that cannot wait.

**What to do.** Usually nothing: the session fixes the finding and closes it
with `--fixed`, or closes it with `--declined --reason "<why>"`.

The verification gate and the commit gate ask the same question again. A
finding that was referred earlier is refused there if the story has changed its
file since. Close each finding the gate names in one of those two ways, then
pass the gate again.

**Read more.** [The scout rule](the-loop.md#the-scout-rule).

### SDLC-E0092

**What happened.** A gate cannot pass while a review's finding is still open.
The verification gate waits for the design review's findings, and the commit
gate waits for those and the code review's.

**Why.** A finding nobody has closed is a decision nobody has made.

**What to do.** Usually nothing: the session closes each finding the message
names. `sdlc status` shows each one with its text. A finding is closed in one
of three ways, after fixing it where it is to be fixed:

```console
$ sdlc finding close <gate> <role> <F-n> --fixed
$ sdlc finding close <gate> <role> <F-n> --declined --reason "<why>"
$ sdlc finding close <gate> <role> <F-n> --referred --reason "<why>"
```

Then the gate is passed again.

**Read more.** [The scout rule](the-loop.md#the-scout-rule).

### SDLC-E0093

**What happened.** The verification gate, or the commit gate, found fewer
findings on the story's record than its reviews listed.

**Why.** An older `sdlc` saved the record and dropped the findings. Read as
none, they would let the gate pass. This happens when the plugin's `sdlc` and
the one on your `PATH` are different releases.

**What to do.** Each reviewer the message names records its review again, with
`sdlc review add` and one `--finding` for each finding it listed. The text of
each is in the review documents under `.sdlc/stories/<ID>/reviews/`. The
findings are then closed with `sdlc finding close`.

So that it does not happen again, bring the command and the plugin to the same
release. `sdlc version` names the one on your `PATH`.

**Read more.** [Updating](installation.md#updating).

### SDLC-E0094

**What happened.** `sdlc decide` refused a decision. Nothing was recorded: the
story's record gained no decision, and its `DECISIONS.md` gained no line.

**Why.** A decision the loop makes for itself is read by you afterwards, so it
is recorded whole or not at all. The message names the one part it refused:

```console
$ sdlc decide AUTH-3 --question "Which total does AC-2 mean?" --option "the gross total" --chosen "the net total" --reason "AC-2 says what is charged" --by researcher
sdlc: a decision weighs at least two options, and this gives 1

  why  nothing was recorded
  fix  give --option "<an option>" once for each option weighed, at least twice, none of them blank, then run the command again

  SDLC-E0094  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0094
```

**What to do.** Usually nothing: the session gives the missing part and runs
the command again. A decision has five parts, each on one line:

| Flag | What it takes |
| --- | --- |
| `--question` | What was decided. It is not blank. |
| `--option` | An option that was weighed, once for each. At least two and at most ten, none blank, and no two the same. |
| `--chosen` | The option chosen, word for word as one of the `--option` values. |
| `--reason` | Why it was chosen. It is not blank. |
| `--by` | The agent whose recommendation or ruling was applied: an agent such as `researcher`, an instance such as `security-2`, or an arbiter such as `architect-arbiter`. Never a person, so that no decision reads as your answer. |

Each value is at most 1,000 bytes, and holds no line break and no control or
invisible character.

One refusal is about the story and not a flag: the story's commit is one your
`sdlc ack` already covers. A decision recorded on it now would be neither
counted by `sdlc status` nor listed by `sdlc log`. Record it on the story under
way, which `sdlc status` names, and name the other story in `--question`.

**Read more.** [Two kinds of decision](the-loop.md#two-kinds-of-decision) and
[`sdlc decide`](commands.md#sdlc-decide-id).

### SDLC-E0095

**What happened.** `sdlc gate implementation pass` did not record a pass,
because of the commands it ran. The gate runs your configured commands in the
story's worktree (`build`, `test`, `lint`, `fmt_check`, `coverage` and
`mutation`, those that are set, in that order) and records the pass only when
all of them pass. The first line of the message names the command, as
`commands.<key>`, and how it ended.

**Why.** The result of this run is what the verifier and the landing rely on
afterwards, so a pass is recorded only for a run that passed, on the work as
it stood when the run began.

**What to do.** Read the output first. Everything the commands printed is in
`.sdlc/stories/<ID>/implementation-run.log`, up to 16 MiB, and the last 20
lines of the command that stopped the run follow the message. If the output
could not be put there, the message names the temporary file beside it that
holds it instead. Remove whatever stands at the name before the next run.

Two cases count as a failed gate. The gate is recorded as failed, which counts
towards `loop.max_rework_rounds`, and at that limit the story is handed to you:

- **A command failed.** It ended with a status other than 0. The implementer
  puts the work right in the story's worktree and records the gate again,
  which runs the commands again.
- **A threshold was not met**, or the coverage command printed no figure. The
  same as a failure.

The others record nothing, and use up no rework round:

- **A command could not be run.** There is no shell, the program is not there
  (status 127), it cannot be run (126), or a signal ended it. This is yours to
  put right: the program, the shell, or `commands.<key>` in
  `.sdlc/config.json`.
- **A command wrote a file of the work it ran on.** The message names the
  path. A command writes what it makes under `$SDLC_RUN_DIR`, or to a path git
  ignores. Then the gate is recorded again.
- **The story changed while the commands ran.** The message says what moved: a
  gate was recorded, the freeze was taken again or lifted, a frozen test
  changed, or the branch moved. Another session in the same checkout, or a
  command of your own, is the usual cause. Record the gate again once that has
  finished.
- **`SDLC_RUN_DIR` was already set.** The gate was run from inside one of your
  configured commands. Unset the variable.

A session that meets the same refusal twice hands the story to you.

`SDLC_RUN_DIR` is a directory each run is given for itself, made empty in the
system's temporary directory and removed when the run ends. Your coverage
command reads there what your test command wrote. One that finds nothing there
was probably run by hand, or its test command writes its profile somewhere
else.

A second run for the same story while one is going is refused with
[SDLC-E0006](#sdlc-e0006), which names the run that is going.

**Read more.** [The run directory](configuration.md#the-run-directory) and
[`sdlc gate`](commands.md#sdlc-gate-gate-status).

## Warnings the hook prints

These are not errors. A warning starts with `sdlc:` and appears in the session
working a story. The call it is about goes through, because the hook never
blocks your session over a fault of its own.

Take each one seriously all the same. A warning means a rule you are relying on
is off, or running in a reduced form, until you fix what it names. Only the
session working the story is told. Every other session hears nothing from the
plugin.

The gates do not depend on the hook. A frozen test that was changed while a
warning stood is still refused at the gates that check the freeze, with
[SDLC-E0025](#sdlc-e0025).

### `the sdlc binary was not found, so nothing is being enforced`

**What happened.** The plugin is installed, and it cannot find the `sdlc`
command it hands every tool call to.

**Why.** The plugin looks in four places: at `SDLC_BIN`, in its own directory,
on the `PATH` the app started with, and where the installers put the command.
It found none there. Until it does, nothing is refused.

**What to do.** Install the command by any route in
[Installation](installation.md), or type `/sdlc:next`, which offers to install
it. Then check with `/sdlc:doctor`.

A desktop app such as Claude Desktop keeps the `PATH` it was started with. If
`sdlc doctor` passes in a new terminal and the app still shows the warning,
quit the app fully, from the tray or the menu bar too, and open it again. Or
set `SDLC_BIN` to the full path of the command, in the
[local environment editor](claude-desktop.md#set-variables-in-the-local-environment-editor).

The plugin also tries to download its own copy when a session starts. When that
fails and no other copy is found, the session says why:

| The session says | What to do |
| --- | --- |
| `could not fetch sdlc <version> (<archive>) from the release` | The machine is offline, or the release has nothing for this platform. Check the connection and open a new session, which tries again. |
| `the plugin's checksums.txt holds no checksum for sdlc <version> on <platform>`, `the plugin carries no checksums.txt`, or `the version of the plugin could not be read from its plugin.json` | The plugin is not one a release published. Update it from the plugin browser and open a new session. Don't point `SDLC_BIN` at a binary you did not install yourself. |
| `the download does not match the checksum the plugin pins for it, so it was neither installed nor run` | Open a new session to try once more. If it fails in the same way, don't run the download, and report it. |
| `no release of sdlc is built for this operating system and processor` | Build `sdlc` from its source, and set `SDLC_BIN` to what you built. |
| `nothing here can check a download against its pinned checksum`, `the download could not be unpacked`, or `the bin directory of the plugin could not be written to` | Install the command yourself, then set `SDLC_BIN` to it, or put it on `PATH` and reopen the app. |

Offline, the download can take about two minutes to give up, each time a
session starts. Setting `SDLC_BIN` turns the download off.

While no command can be found, a slash command such as `/sdlc:approve` does not
run either. It says so first:
`the command you typed did not run, because the sdlc binary was not found`.

**Read more.** [How the plugin finds sdlc](installation.md#how-the-plugin-finds-sdlc).

### `.sdlc/state/active could not be read`, or `does not name a story`

**What happened.** The loop's note of which story is under way cannot be used.
It cannot be opened, it is empty, or it holds something that is not a story's
id, such as a merge conflict left in place.

**Why.** With no story to hold the session to, the hook enforces nothing in
this session.

**What to do.** In the story's worktree, see what is wrong:

```console
$ sdlc doctor
```

It names the file under `loop state`. Then run `sdlc stop`, which removes the
note and ends the iteration, and pick the story up again with
`/sdlc:next <ID>`.

**Read more.** [`sdlc doctor`](commands.md#sdlc-doctor).

### `.sdlc/config.json could not be read, so tests are being recognised by the default patterns`

**What happened.** Your settings file cannot be read, so the hook does not know
which files your project calls tests.

**Why.** The hook carries on with the default patterns, plus every file the
freeze covers. Protected paths and the freeze still hold. A test that only your
own `paths.tests` names is not recognised as one until the file reads.

**What to do.** Correct the file, as [SDLC-E0004](#sdlc-e0004) describes.
`sdlc doctor` names the problem under `configuration`.

**Read more.** [`paths.tests`](configuration.md#pathstests).

### `every test file is being treated as frozen`

**What happened.** The hook could not read the story's freeze, so it treats
every test file as frozen. The warning says which of these it is:

| The warning begins | What it means |
| --- | --- |
| `.sdlc/state/tests.lock could not be read` | The freeze is damaged. |
| `.sdlc/state/tests.lock does not hold the freeze <ID>'s record says its tests are under` | The story's tests were frozen, and the freeze has gone missing without being lifted. |
| `the story's record is kept in the repository's main checkout, which could not be found` | The hook cannot reach your project's directory. |
| `.sdlc/state/tests.lock could not be read, and nor could .sdlc/config.json` | Neither the freeze nor your settings can be read, so every file the default patterns call a test is treated as frozen. |

**Why.** A freeze that cannot be read is not treated as no freeze. Until it
reads again, any write to something that looks like a test is refused.

**What to do.** Run `sdlc doctor` in the story's worktree. It names the problem
under `loop state`. Restore the file if you keep a copy. Otherwise lift the
freeze on the record and have the tests frozen again: type `/sdlc:unfreeze` in
Claude Code, or run `sdlc unfreeze --reason "<why>"` in your own terminal.
Deleting the file is not enough, because `sdlc freeze` refuses a freeze that
went missing without being lifted.

**Read more.**
[Lift the freeze](guides/when-a-frozen-test-is-wrong.md#lift-the-freeze).

### `the working tree could not be measured, so this commit was not checked`

**What happened.** The session committed, and the hook could not compare the
commit with what was reviewed and approved. The commit went through.

**Why.** The hook could not read your settings, or could not ask git about the
working tree in time.

**What to do.** Nothing is lost: the commit gate makes the same check
afterwards, and refuses a commit that is not the reviewed work. Run
`sdlc doctor` to find out what the hook could not read.

A story that waits for your approval before the commit gets no such warning.
There the hook refuses the commit, because no approval can be matched to work
nobody measured. If you have approved the work, make that commit in your own
terminal, and the commit gate then checks it against your approval.

**Read more.** [`commit-gate`](enforcement.md#commit-gate).

### Nothing is enforced, and nothing is said

**What happened.** You expected the hook to refuse something, it did not, and
no warning appeared.

**Why.** The hook holds one session to one story. When no Claude Code session
is recorded as working the story, there is no session to hold and none to tell.
`sdlc status` and `sdlc doctor` both say so.

**What to do.** Type `/sdlc:next <ID>` in the session that should work the
story. That session is then the one the hook holds.

**Read more.**
[When nothing was refused](guides/why-was-i-refused.md#when-nothing-was-refused).

### The hook ran out of time

**What happened.** A call was refused under the rule name
`not-checked-in-time`. This is not a warning, and the call did not go through.

**Why.** A command the hook cannot finish reading in time, or a change it
cannot check in time, is refused rather than let through unchecked. No rule was
broken.

**What to do.** The refusal says what to try: a long command again in shorter
parts, or a file change again as it is. If that is refused as well, start
Claude Code with `SDLC_DEBUG_FILE` set to a file, repeat the call, and read the
`hook decision` line for it. With `SDLC_TRACE=1` set as well, the log gives the
time each call that finishes took, a refused one included. A deadline far below
25 seconds in that log means
[`SDLC_HOOK_DEADLINE_MS`](configuration.md#sdlc_hook_deadline_ms) is set where
the session started.

Two gaps are left, and it is as well to know them. A hook that Claude Code
kills before it has learnt whether the call is one it checks still lets the
call through, and says nothing. So does one that is killed just after it has
allowed a commit.

**Read more.** [`not-checked-in-time`](enforcement.md#not-checked-in-time).

### A slash command did not run

**What happened.** You typed the slash command for one of your six decisions,
such as `/sdlc:approve`, and it answered that it did not run. Nothing was read
or changed. This is not a warning either, and it is not
[SDLC-E0079](#sdlc-e0079), which comes from the command itself.

**Why.** The hook runs one of these commands only where it can tell that
Claude Code ran the hook itself, for a session a person attends. Otherwise an
agent could hand the hook a message of its own and approve its own work. The
hook tells by four environment variables, and the answer names each one that
failed, by its name and never its value.

**What to do.** Do what the answer says for the variable it names:

| The answer names | What it found | What to do |
| --- | --- | --- |
| `AI_AGENT` | It does not hold the label Claude Code gives a hook it runs itself: `claude-code_`, then its version, then `_harness`. An older Claude Code may give its hooks another label. | Update Claude Desktop or Claude Code. If you set `AI_AGENT` yourself, take it out and start the session again. |
| `CLAUDE_CODE_EXECPATH` | It is set, as Claude Code sets it for an agent's tools. Most often the session was started from a terminal that Claude Code opened. | Quit that terminal, and start the session from one you opened yourself. |
| `GIT_EDITOR` | It is exactly `true`, as Claude Code sets it for an agent's shell. Any other editor is fine. | If you set it so yourself, take it out and start the session again. |
| `CLAUDE_CODE_SESSION_ATTENDED` | It is not `1`. A `claude -p` started from another session has it otherwise, and an older Claude Code does not set it. | Type the command in an interactive session you opened yourself. |

A variable you set yourself is in the shell you started Claude Code from, or in
Claude Desktop's
[local environment editor](claude-desktop.md#set-variables-in-the-local-environment-editor).

To go on at once, there are two other ways in. In Claude Desktop, type the
command as a `!` line, such as `!sdlc approve <ID>`. Or run it in a terminal
outside Claude Code, which needs the `sdlc` command installed:
`npx @bbsnly/sdlc install`.

The check rests on what Claude Code 2.1.321 sets, as read in Claude Desktop. A
later release that keeps the four variables is expected to work, and an
earlier one was not tried.

**Read more.**
[A person's decisions, refused in every session](enforcement.md#a-persons-decisions-refused-in-every-session).

### `sdlc trace` shows nothing was recorded

**What happened.** `sdlc trace` says `no trace.jsonl for this story`, or the
trace stops short, or its agent runs have no tokens.

**Why.** Recording never refuses anything and never speaks in the session, so a
gap in the trace is silent. These are the usual causes:

| The cause | What to do |
| --- | --- |
| The plugin was not reloaded after an update | Restart Claude Code, so that it loads the plugin's new hooks. |
| The session does not hold the story | Type `/sdlc:next <ID>` in the session that should work it. Only that session, and its agents, are recorded. |
| Claude Code is older than 2.1.309 | Run `claude --version`, and update Claude Code. |
| The trace is full | A trace stops at 4 MiB, and `sdlc trace` says that the later records are missing. |
| Something other than a plain file stands where the trace should be | Move it aside. The next record starts a new trace. |
| The command and the plugin are different releases | Bring them to the same release, as [Updating](installation.md#updating) shows. |

**What to do.** To see which cause it is, start Claude Code with
`SDLC_DEBUG_FILE` set to a file, run a turn, and read the lines that start with
`trace:`.

**Read more.**
[See where the time and tokens went](guides/tracking-what-a-story-costs.md#see-where-the-time-and-tokens-went).

## Where to go next

- [Why was I refused?](guides/why-was-i-refused.md): reading a refusal, from
  the command or from the hook
- [Enforcement](enforcement.md): every rule the hook holds a session to
- [Commands](commands.md): every command and flag
- [Human approval gates](guides/human-approval-gates.md): answering when the
  loop stops for you
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md):
  putting a story down, and landing it
