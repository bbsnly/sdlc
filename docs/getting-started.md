# Getting started

- [Introduction](#introduction)
- [Install sdlc](#install-sdlc)
- [Set up your project](#set-up-your-project)
  - [Fill in the contract](#fill-in-the-contract)
  - [Commit the setup](#commit-the-setup)
  - [Check the setup](#check-the-setup)
- [Write a story](#write-a-story)
- [Run the loop](#run-the-loop)
  - [What happens at each gate](#what-happens-at-each-gate)
  - [Checking on a story](#checking-on-a-story)
- [When something goes wrong](#when-something-goes-wrong)
- [Next steps](#next-steps)

## Introduction

When you hand a story to a coding agent, getting code written is the easy
part. The hard part is trusting it: knowing that the tests were written against
what you asked for, that somebody other than the author read the plan, and that
nothing was committed before it was checked.

`sdlc` gives your agent that discipline. It takes one story at a time through
nine gates, from checking the story is ready to writing up what was learned,
and a hook holds the agent to each of them.

This page takes you from nothing to your first story going through the loop.
It takes about ten minutes, and most of that is writing one good acceptance
criterion. You do not need to understand every gate before you begin. Watching
your first story go through is the quickest way to learn them.

## Install sdlc

There are two things to install: the `sdlc` command, and the Claude Code plugin
that drives it. You will need Claude Code 2.1.265 or newer, git 2.17 or newer
and, for the one-line install below, Node 18 or newer;
[Installation](installation.md#what-it-needs) has the full list. The easiest way
is to give your agent this prompt. Paste it into Claude Code, in the project you
want to work on:

```text
Set up sdlc: run `npx @bbsnly/sdlc install`, then read
https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md and follow it
all the way through.
```

The agent installs the command and works through this page with you. A few
steps are yours alone, and it will hand those back to you when it reaches them.

> [!NOTE]
> If you are an agent following this page for a person: only they can type the
> `/plugin` commands and `/sdlc:next` in Claude Code. Hand back to them adding
> the plugin, what goes in the contract, the story's acceptance criteria and
> `/sdlc:next`, which is where you stop.

If you would rather type the commands yourself, first install the command. If
`sdlc version` already works in your terminal, the command is installed and you
can skip to adding the plugin. Otherwise, with Node 18 or newer, one line does
it on macOS, Linux and Windows alike:

```console
$ npx @bbsnly/sdlc install
```

This downloads the native `sdlc` binary built for your machine and checks it
before installing it. If the installer prints lines to add a directory to your
`PATH`, add them now, so that your terminal can find `sdlc` in the next step.
Until you open a new terminal, you can run the binary by its full path:
`~/.local/bin/sdlc` on macOS and Linux, or
`%LOCALAPPDATA%\Programs\sdlc\bin\sdlc.exe` on Windows.

Then, in Claude Code, add the plugin. Only a person can type these commands:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

The repository is its own marketplace, so there is nothing else to add.

> [!NOTE]
> No Node? You may also install with a one-line script for your platform, with
> `go install`, from a prebuilt archive, or from source.
> [Installation](installation.md) covers each of them, and shows how to verify
> a download yourself.

## Set up your project

Next, tell `sdlc` about the project you want to work on. It needs to be a git
repository. From inside it, run:

```console
$ sdlc init
```

`init` reads your repository, works out what it is built with, and writes the
few files the loop needs:

| File | What it is |
| --- | --- |
| `.sdlc/config.json` | the loop's settings, with commands that already match your project |
| `user_stories.json` | your backlog, with one example story in it |
| `.sdlc/templates/story.schema.json` | what a story may contain |
| `CLAUDE.md` | created with a `## SDLC Contract` section, or the section appended if the file exists |
| `AGENTS.md` | the section appended instead, where the project keeps its instructions there and has no `CLAUDE.md` |

`init` never overwrites your own files, so it is safe to run in a repository
that already has a `CLAUDE.md` or an `AGENTS.md`. Where there is an `AGENTS.md`
and no `CLAUDE.md`, `sdlc init` appends the section to `AGENTS.md` and creates
no `CLAUDE.md`, because Claude Code reads `AGENTS.md` only where there is no
`CLAUDE.md`. [Adding sdlc to an existing
project](guides/adding-to-an-existing-project.md#a-project-that-keeps-agentsmd)
says more.

If `init` says no stack was recognised, the commands in `.sdlc/config.json` are
empty, and you will need to fill in at least `test` and `lint` yourself.
[Tuning commands for your stack](guides/tuning-commands-for-your-stack.md) shows
how.

> [!NOTE]
> `init` writes no `.gitignore`, because what a project commits is the
> project's decision. To restore the default settings later while keeping your
> stories, see [`sdlc init`](commands.md#sdlc-init). Setting up a repository
> that already has code and tests? Read
> [Adding sdlc to an existing project](guides/adding-to-an-existing-project.md).

### Fill in the contract

The `## SDLC Contract` section that `init` added to your `CLAUDE.md` (or your
`AGENTS.md`) is where
you tell the reviewers what good looks like in your project. Most fields start
as `TBD`, and the design reviewer and the code reviewer check the work against
what you write there. A contract full of `TBD` gives them nothing to hold the
work to.

You do not need to fill in all of it today. Three fields earn their keep first:
the architecture rules, the test conventions and the glossary. [Adding sdlc to
an existing project](guides/adding-to-an-existing-project.md#fill-in-the-contract)
says what to write in each field.

### Commit the setup

Next, commit the contract together with everything `init` wrote:

```console
$ git add .sdlc user_stories.json CLAUDE.md
$ git commit -m "Set up the sdlc loop"
```

Where `init` wrote the section to `AGENTS.md`, add `AGENTS.md` in place of
`CLAUDE.md`.

> [!WARNING]
> Do not skip this commit. Each story is worked on in a worktree of its own (a
> second checkout that git makes), cut from the commits on trunk, your main
> branch, so only what is committed is there. A story started
> before the commit is refused with
> [SDLC-E0053](troubleshooting.md#sdlc-e0053).

### Check the setup

Now check that everything is in order:

```console
$ sdlc doctor
```

`doctor` looks at the handful of things that stop the loop working, such as
git, the configuration, the backlog and whether `sdlc` is on your `PATH`. Every
problem it reports comes with what to do about it.

## Write a story

A story says what you want built and how you will know it is done. Stories live
in your backlog file, which `init` created as `user_stories.json` at the root
of the repository. Let's take a look at an example:

```json
{
  "_schema": ".sdlc/templates/story.schema.json",
  "stories": [
    {
      "id": "AUTH-3",
      "title": "Reject expired sessions",
      "status": "ready",
      "priority": 1,
      "acceptance_criteria": [
        {
          "id": "AC-1",
          "type": "WHEN",
          "text": "WHEN a request carries a session token whose expiry is in the past, the API shall respond 401 and shall not read the session store."
        },
        {
          "id": "AC-2",
          "type": "IF-THEN",
          "text": "IF the token cannot be parsed, THEN the API shall respond 401 with no detail about why."
        }
      ]
    }
  ]
}
```

The acceptance criteria are the important part. Each one should describe
something you could observe from outside, such as a response, a returned value
or a stored record, and not how the code produces it. The loop turns every
criterion into a failing test before any code is written, so a criterion that
cannot be tested has nowhere to go. The capitals are deliberate: `WHEN` and
`IF ... THEN` are two of a handful of sentence patterns, and
[Writing stories](guides/writing-stories.md) lists them all.

Don't worry about getting this perfect the first time. If a criterion cannot be
turned into a test, the first gate will say so and stop, which is the cheapest
place in the whole loop to find out.

To see your backlog, and what is holding each story back, run:

```console
$ sdlc story list
```

> [!NOTE]
> A story is picked up when its `status` is `todo` or `ready` and the stories
> it depends on are done. [Writing stories](guides/writing-stories.md) covers
> every status, how the next story is chosen, and how to write criteria the
> first gate accepts.

## Run the loop

You are ready to run your first story. In Claude Code, from your project, type:

```text
/sdlc:next
```

The skill picks up the next runnable story, or
resumes one already under way, and works it through the gates one at a time. It
hands each gate to the agent whose job it is, and records every outcome before
the next gate begins.

Nothing happens until you type the command: in Claude Code, only a person can
start the skill. Along the way you will be asked to decide things. That is
deliberate. The loop stops and asks when the story does not settle something,
and never guesses. [Human approval gates](guides/human-approval-gates.md) shows
what you will see and how to answer.

> [!NOTE]
> The agents are told to work only on a gate of a story you started and to stop
> on anything else, but nothing enforces that. If you are working from a clone
> of this repository and not an installed plugin, start the session with
> `claude --plugin-dir /path/to/sdlc/plugin`.

### What happens at each gate

Here is what you will see as your story goes through:

1. **Gate 1** checks that your acceptance criteria are testable, and picks the
   story.
2. **Gate 2** sends a researcher to read the story against your codebase and
   write down what it touches and what could go wrong.
3. **Gate 3** turns each criterion into a test that fails, then freezes every
   test file. From here on, nobody edits them.
4. **Gate 4** writes a plan and puts it in front of five reviewers.
5. **Gate 5** implements the plan until the frozen tests pass.
6. **Gate 6** sends a verifier that has seen none of it to hunt for tests that
   pass without the behaviour being there.
7. **Gate 7** reviews the diff in a context that never saw the reasoning.
8. **Gate 8** makes the commit, and only after everything before it has
   passed.
9. **Gate 9** writes the retro and lands the story on trunk (the skill runs
   `sdlc stop` for you).

The work happens away from your own checkout. Each story gets a worktree at
`.sdlc/worktrees/<ID>` on a branch named `story/<ID>`, and trunk only moves
when the finished story lands at Gate 9.

> [!NOTE]
> [The loop](the-loop.md) describes each gate in full: what it is for, who
> does it and what it produces. To learn what the landing checks, and what to
> do when trunk has moved on, see
> [Done when it lands](the-loop.md#done-when-it-lands).

### Checking on a story

Once a story is under way, you may ask where it has got to at any point:

```console
$ sdlc status
$ sdlc review list
```

`status` shows the story in progress and the gate it has reached. `review list`
shows what each reviewer said. Before a story is started, `status` names the
story that is next up, and `review list` has nothing to show and says so with
[SDLC-E0011](troubleshooting.md#sdlc-e0011).

## When something goes wrong

Every failure carries a code, what happened, why, and what to do about it. To
read more about any code, look it up in
[Troubleshooting](troubleshooting.md).

If the loop refuses something, read the refusal before working around it. It
names the sanctioned route, and that route is usually one command.
[Why was I refused?](guides/why-was-i-refused.md) walks through reading one.

## Next steps

Now that your first story is under way, you may be wondering what to read
next. If you would like to understand how the loop works, start here:

- [The loop](the-loop.md): what each gate is for and what it produces
- [Agents](the-agents.md): who does the work at each gate
- [Enforcement](enforcement.md): what the hook holds the agent to, and why

If you would rather get on with your own project, these guides cover the jobs
you are most likely to meet first:

- [Writing stories](guides/writing-stories.md)
- [Tuning commands for your stack](guides/tuning-commands-for-your-stack.md)
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md)
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md)

And when you need to look something up, [Commands](commands.md) lists every
command and flag, and [Configuration](configuration.md) lists every setting.
