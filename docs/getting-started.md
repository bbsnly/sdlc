# Getting started

- [Introduction](#introduction)
  - [Before you begin](#before-you-begin)
  - [Letting your agent do the setup](#letting-your-agent-do-the-setup)
- [Install sdlc](#install-sdlc)
- [Set up your project](#set-up-your-project)
  - [Fill in the contract](#fill-in-the-contract)
  - [Commit the setup](#commit-the-setup)
  - [Check the setup](#check-the-setup)
- [Write a story](#write-a-story)
- [Run your first story](#run-your-first-story)
  - [What you will see](#what-you-will-see)
  - [When the loop asks you something](#when-the-loop-asks-you-something)
  - [Checking on a story](#checking-on-a-story)
  - [When the story lands](#when-the-story-lands)
- [When something goes wrong](#when-something-goes-wrong)
- [Next steps](#next-steps)

## Introduction

A coding agent will write code for any story you hand it. Trusting what comes
back is the hard part. Were the tests written against what you asked for, or
against what got built? Did anyone but the author read the plan? Was anything
checked before it was committed?

`sdlc` settles those questions the same way for every story. It is a command
and a Claude Code plugin that take one story at a time through nine gates.
Failing tests are written from your acceptance criteria and frozen before any
code exists. The plan and the diff are each reviewed by agents that never saw
the author's reasoning. The commit comes last, and a hook holds the agent to
every gate along the way.

On this page you will install `sdlc`, set up a project, write one story and
send it through the loop. Allow about ten minutes before the story starts, most
of it spent on one good acceptance criterion. You don't need to understand the
gates first. Watching a story go through them is the quickest way to learn.

### Before you begin

You will need:

- Claude Code 2.1.309 or newer, in a terminal or in the Code tab of Claude
  Desktop
- git 2.17 or newer, and a project that is a git repository
- Node 18 or newer, for the one-line install

If you have no Node, or you want the details,
[Installation](installation.md#what-it-needs) has the full list and every other
way to install.

### Letting your agent do the setup

You may hand most of this page to your agent. Paste this prompt into Claude
Code, in the project you want to work on:

```text
Set up sdlc: run `npx @bbsnly/sdlc install`, then read
https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md and follow it
all the way through.
```

The agent installs the command and works through the rest of the page with
you. Four things on it are yours alone, and the agent stops and hands each one
back to you: adding the plugin, deciding what goes in the contract, writing
your story's acceptance criteria, and typing `/sdlc:next`, which is where the
agent's part ends.

The rest of this page takes the same steps by hand.

## Install sdlc

There are two things to install: the `sdlc` command, which keeps the loop's
records and enforces its rules, and the Claude Code plugin, which brings the
agents and the `/sdlc:next` skill that drives them.

First, install the command. One line does it on macOS, Linux and Windows:

```console
$ npx @bbsnly/sdlc install
```

The installer downloads the native `sdlc` binary built for your machine and
checks it before installing it. Open a new terminal and make sure it is there:

```console
$ sdlc version
```

If that prints a version, the command is installed. If your terminal cannot
find `sdlc`, look back at what the installer printed. It does not change your
`PATH`, and where its directory is not on it, it prints the lines to add.

Next, add the plugin. Only a person can do this, so type these two commands
into Claude Code yourself:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

The repository is its own marketplace, so there is nothing else to add.

> [!NOTE]
> In the Code tab of Claude Desktop, typing `/plugin` does not add the plugin.
> Open Claude Desktop's plugin browser, add the marketplace `bbsnly/sdlc` and
> install the `sdlc` plugin from it. A plugin installed that way fetches its
> own `sdlc`, so the `npx` step is optional there.
> [Running the loop from Claude Desktop](claude-desktop.md#install-the-plugin-from-the-plugin-browser)
> covers the rest, including what to type where you have no terminal.

## Set up your project

With both installed, you can set up the project you want to work on. From
anywhere inside its repository, run `sdlc init`, or type `/sdlc:init` in Claude
Code:

```console
$ sdlc init
Set up sdlc in /home/<user>/src/billing

  created  .sdlc/config.json
  created  .sdlc/templates/story.schema.json
  created  user_stories.json
  created  CLAUDE.md
  created  REVIEW.md

Guessed the commands from a Go project.

Next:
  1. sdlc doctor              check the guesses
  2. edit user_stories.json   replace the example with a story of your own
  3. /sdlc:next               type it in Claude Code to work the first story
```

`init` works out what your project is built with (it recognises Go, Rust, Node
and Python) and fills in the commands that build, test and lint it. Here is
what each file it wrote is for:

| File | What it is for |
| --- | --- |
| `.sdlc/config.json` | the loop's settings, with commands that already match your project |
| `user_stories.json` | your backlog, with one example story in it |
| `.sdlc/templates/story.schema.json` | what a story may contain |
| `CLAUDE.md` | your project's instructions, now with a `## SDLC Contract` section |
| `REVIEW.md` | the [review policy](configuration.md#reviewspolicy) every reviewer reads first |

`init` never overwrites a file of yours. If you already have a `CLAUDE.md`, it
appends the contract section and leaves the rest as it was. For a repository
that already has code, tests and conventions of its own,
[Adding sdlc to an existing project](guides/adding-to-an-existing-project.md)
goes through what `init` touches and what it leaves alone.

If `init` says no stack was recognised, the commands in `.sdlc/config.json`
are empty, and you will need to fill in at least `test` and `lint` yourself.
[Tuning commands for your stack](guides/tuning-commands-for-your-stack.md)
shows how.

> [!NOTE]
> Does your project keep its instructions in `AGENTS.md`, with no `CLAUDE.md`?
> Then `sdlc init` appends the contract to `AGENTS.md` and creates no
> `CLAUDE.md`. Wherever this page says `CLAUDE.md`, read `AGENTS.md`.
> [Adding sdlc to an existing project](guides/adding-to-an-existing-project.md#a-project-that-keeps-agentsmd)
> explains why.

The rest of this page follows the three steps `init` printed, and adds two
before them: filling in the contract, and committing what `init` wrote.

### Fill in the contract

Open `CLAUDE.md` and find the `## SDLC Contract` section. This is where you
tell the reviewers what good looks like in your project. Most of its fields
start as `TBD`, and the design reviewer and the code reviewer check the work
against what you write there. A contract full of `TBD` gives them nothing to
hold the work to.

You don't need to fill in all of it today. Start with the architecture rules,
the test conventions and the glossary, and write each rule as a sentence
somebody could check against a diff:

```markdown
### Architecture rules (the design reviewer blocks on these)

- Dependency direction: `internal/billing` imports nothing under `internal/http`.
- Errors: billing returns typed errors such as `ErrNonPositiveTotal`; no panics.
```

[Fill in the contract](guides/adding-to-an-existing-project.md#fill-in-the-contract)
says which agent reads each part of it.

### Commit the setup

Next, commit the contract together with everything `init` wrote:

```console
$ git add .sdlc user_stories.json CLAUDE.md REVIEW.md
$ git commit -m "Set up the sdlc loop"
```

> [!WARNING]
> Don't skip this commit. Each story is worked in a worktree of its own, a
> second checkout that git cuts from the commits on trunk, the branch your
> finished work lands on. What you have not committed is not there, so `sdlc`
> [refuses to start a story](troubleshooting.md#sdlc-e0053) until you have.

### Check the setup

Now make sure everything is in order. Run `sdlc doctor`, or type `/sdlc:doctor`
in Claude Code:

```console
$ sdlc doctor
```

`doctor` checks the handful of things that stop the loop working: git, the
configuration, the backlog, the contract, the commands `init` guessed, and
whether `sdlc` is on your `PATH`. When all is well, its last line reads:

```text
Everything checks out.
```

If it finds a problem, the fix is printed directly beneath it.

## Write a story

A story says what you want built and how you will know it is done. Your stories
live in the backlog, `user_stories.json`, at the root of the repository. Open
it and replace the example `init` left there with a story of your own. Let's
take a look at a small one:

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
          "text": "WHEN a request carries a session token whose expiry is in the past, the API shall respond 401 and shall not read the session store."
        },
        {
          "id": "AC-2",
          "text": "IF the token cannot be parsed, THEN the API shall respond 401 with no detail about why."
        }
      ]
    }
  ]
}
```

The `status` of `ready` is what tells the loop this story may be started, and
where several are ready, the lowest `priority` goes first.

The acceptance criteria are the part to spend your time on. The loop turns
every one of them into a failing test before any code is written, so each
should describe something you could observe from outside: a response, a
returned value, a stored record. "The API shall respond 401" can be tested.
"The handler shall use `TokenValidator`" only says how the code is built.

The capitals are deliberate. `WHEN` and `IF ... THEN` are two of a handful of
sentence patterns, and [Writing stories](guides/writing-stories.md) lists them
all.

Don't worry about getting this perfect the first time. If a criterion cannot
be turned into a test, the first gate says so and stops, which is the cheapest
place in the whole loop to find out.

When you have saved the file, ask `sdlc` what it makes of your backlog:

```console
$ sdlc story list
> AUTH-3  ready   1  Reject expired sessions

> is what `sdlc start` would pick.
```

The `>` marks the story the loop will take next. Where a story is waiting on
another, its line says so. You don't need to commit the backlog before you
carry on: the loop reads it from your checkout.

## Run your first story

You are ready to run the story. Open Claude Code in your project and type:

```text
/sdlc:next
```

Nothing happens until you do: only a person can start the loop. The skill
takes the next `ready` story and works it through the gates one at a time. It
hands each gate to the agent whose job it is, and records the outcome before
the next gate begins.

### What you will see

Here is what happens to your story, gate by gate:

1. **Gate 1** checks that your acceptance criteria can be tested.
2. **Gate 2** sends a researcher to read the story against your codebase and
   write down what it touches and what could go wrong.
3. **Gate 3** turns each criterion into a test that fails, then freezes the
   tests. From here on, nobody edits them.
4. **Gate 4** writes a plan and puts it in front of up to five reviewers.
5. **Gate 5** writes the code, until the frozen tests pass.
6. **Gate 6** sends a verifier that has seen none of it to hunt for tests that
   pass without the behaviour being there.
7. **Gate 7** reviews the diff in a context that never saw the reasoning
   behind it.
8. **Gate 8** makes the commit, and only after everything before it has
   passed.
9. **Gate 9** writes the retro, an account of what was learned, and lands the
   story on trunk.

None of this happens in your own checkout. Each story gets a worktree at
`.sdlc/worktrees/<ID>`, on a branch named `story/<ID>`, and trunk does not move
until the finished story lands.

[The loop](the-loop.md) describes each gate in full: what it is for, who does
the work and what it produces.

### When the loop asks you something

The loop does not guess. When your story leaves a question open, or a reviewer
keeps blocking, it escalates: it hands the story to you and stops. You will see
something like this as the last thing the session prints:

```text
AUTH-3  waiting for a person: spec_unclear
  AC-2 says no detail: is an empty body right, or a fixed message?

The iteration has ended. A person reads the work and answers in Claude Code or in their own terminal:
  /sdlc:approve AUTH-3, or sdlc approve AUTH-3
  sdlc approve AUTH-3 --reject "why"
```

Nothing has gone wrong. The work so far is in the story's worktree for you to
read, and that is also where you answer, in a terminal of your own:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc approve AUTH-3
```

To send the work back instead, give the reason with `--reject`. Either way,
open Claude Code in your project and type `/sdlc:next AUTH-3` afterwards, and
the story carries on from the gate it stopped at. Name the story: a plain
`/sdlc:next` in your project begins the next `ready` story instead. In Claude
Code opened in the story's own worktree, a plain `/sdlc:next` is enough.

> [!NOTE]
> The answer is yours alone. The hook refuses `sdlc approve` from an agent,
> because an agent that could answer would be approving its own work.
> [Human approval gates](guides/human-approval-gates.md) shows every place the
> loop can stop for you, and how to answer each.

### Checking on a story

You may ask where a story has got to at any point. In your project,
`sdlc status` names the story under way and the worktree that holds it. For the
detail, ask from inside that worktree:

```console
$ cd .sdlc/worktrees/AUTH-3
$ sdlc status
$ sdlc review list
```

There, `status` shows each gate the story has passed and the one it is on, and
`review list` shows what every reviewer said. Neither changes anything, so both
are safe to run at any time. From your own checkout, where nothing is running,
`sdlc review list` needs the story named: `sdlc review list --story AUTH-3`.

### When the story lands

Gate 9 ends with the landing. The skill runs `sdlc stop` for you, trunk moves
forward to the story's commits, and the story's worktree and branch are
removed. Nothing is pushed: that part is yours.

Two things are left in your checkout for you to commit. One is your backlog,
where the story is now `done`. The other is the story's own directory under
`.sdlc/stories/`, which holds its record and the documents the gates produced:
the analysis, the plan, every review and the retro, all as text you can read.

To see what a story went through on its way to trunk, run `sdlc log`.
[Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md)
shows how to read it, and
[Done when it lands](the-loop.md#done-when-it-lands) says what the landing
checks. Then write your next story, and type `/sdlc:next` again.

## When something goes wrong

Every refusal from `sdlc` has the same shape: what happened, why, and what to
do about it. This is the one you would see if you had skipped the setup
commit:

```text
sdlc: the story's worktree could not be made: trunk, "main", has no .sdlc/config.json committed

  why  a new worktree has only what trunk commits, and the loop in it reads its configuration from there
  fix  commit .sdlc/config.json and CLAUDE.md to trunk, then start again, or set git.worktrees to false in this checkout's .sdlc/config.json; an assistant stops and tells the person

  SDLC-E0053  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0053
```

Read the `fix` line before you try anything else. It names the way through,
and that is usually one command. The last line is the error's code, with a
link to its entry in [Troubleshooting](troubleshooting.md), which says more.

The hook refuses things as well, when an agent tries something the loop's
rules do not allow. [Why was I refused?](guides/why-was-i-refused.md) walks
through reading one of those.

## Next steps

Now that your first story is under way, you may be wondering what to read
next. To understand how the loop works, start here:

- [The loop](the-loop.md): what each gate is for and what it produces
- [Agents](the-agents.md): who does the work at each gate
- [Enforcement](enforcement.md): what the hook holds the agent to, and why

To get on with your own project, these guides cover the jobs you are most
likely to meet first:

- [Writing stories](guides/writing-stories.md)
- [Tuning commands for your stack](guides/tuning-commands-for-your-stack.md)
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md)
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md)

And when you need to look something up, [Commands](commands.md) lists every
command and flag, and [Configuration](configuration.md) lists every setting.
