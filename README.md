# sdlc

[![ci](https://github.com/bbsnly/sdlc/actions/workflows/ci.yml/badge.svg)](https://github.com/bbsnly/sdlc/actions/workflows/ci.yml)
[![coverage](https://codecov.io/gh/bbsnly/sdlc/branch/main/graph/badge.svg)](https://codecov.io/gh/bbsnly/sdlc)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![npm downloads](https://img.shields.io/npm/dm/@bbsnly/sdlc?label=npm%20downloads)](https://www.npmjs.com/package/@bbsnly/sdlc)
[![release downloads](https://img.shields.io/github/downloads/bbsnly/sdlc/total?label=release%20downloads)](https://github.com/bbsnly/sdlc/releases)

## About sdlc

`sdlc` is a delivery loop for [Claude Code](https://claude.com/claude-code). It
takes one user story at a time through nine gates, and a hook holds your agent
to every one of them.

Failing tests are written from your acceptance criteria and frozen before any
code exists, and the agent that writes the code cannot edit them. The plan and
the diff are each reviewed by agents that never saw the author's reasoning. The
commit comes last.

It is for people who let an agent write real code, and want the discipline they
would ask of a colleague: a plan reviewed before the work starts, tests written
against what you asked for and not against what got built, and a trunk that is
always green.

## A story, from start to trunk

You say what you want as a story in your backlog, with acceptance criteria
that say how you will know it is done:

```json
{
  "id": "AUTH-3",
  "title": "Reject expired sessions",
  "status": "ready",
  "priority": 1,
  "acceptance_criteria": [
    {
      "id": "AC-1",
      "text": "WHEN a request carries a session token whose expiry is in the past, the API shall respond 401 and shall not read the session store."
    }
  ]
}
```

Then you open Claude Code in your project and type one command:

```text
/sdlc:next
```

From there the story moves through the gates, one at a time. Each gate is
handed to an agent that starts from a fresh context, and its outcome is
recorded before the next one begins:

| Gate | What happens |
| --- | --- |
| 1 | Your acceptance criteria are checked: can each one be tested? |
| 2 | A researcher reads the story against your codebase, and writes down what it touches and what could go wrong |
| 3 | A failing test is written for each criterion, and the tests are **frozen** |
| 4 | A plan is written, and up to five reviewers read it |
| 5 | The code is written, until the frozen tests pass |
| 6 | A verifier that has seen none of it hunts for tests that pass without the behaviour being there |
| 7 | The diff is reviewed in a context that never saw the reasoning behind it |
| 8 | The commit is made, and only now |
| 9 | A retro records what was learned, and the story lands on trunk |

The work happens in a worktree of the story's own, so your checkout is left as
it was until the finished story lands. When a gate needs a decision that is
yours, the loop stops and asks you. Nothing is pushed: that part stays with
you.

## What the agent cannot do

The gates are not a checklist the agent ticks off about its own work. A hook
looks at every file the agent writes and every command it runs, before it
happens, and refuses what would get round a gate. Let's say the agent writing
the code finds a frozen test in its way and tries to edit it. This is the
answer it gets:

```text
the agent that implements a story does not write its tests -- that is the separation the loop is made of. Instead: make the existing tests pass; if they are wrong, say so rather than changing them [implementer-does-not-write-tests]
```

Every refusal says what to do instead. The gates themselves are held just as
firmly:

- **A gate cannot pass without what it produces.** No analysis document, no
  analysis gate.
- **A gate cannot be recorded out of order.**
- **An approval goes stale.** Change the plan or the code after a review, and
  it goes back to the reviewers who approved the old one.
- **The commit waits.** `git commit` is refused until every gate before it has
  passed, and the refusal names the one that has not.

No git hooks are installed, and your tests, your build and your tooling are
untouched.

Two limits are worth knowing from the start:

- **This is a discipline tool, not a sandbox.** It constrains an agent that is
  trying to do the right thing. It does not defend against one that is trying
  to escape.
- **The agents are steered, not blocked.** Each is told to work only a gate of
  a story you started, and to stop on anything else, but nothing enforces
  that. The plugin does nothing until a person types `/sdlc:next`, and without
  the `sdlc` command nothing is enforced at all.

[Enforcement](https://github.com/bbsnly/sdlc/blob/main/docs/enforcement.md)
lists every rule, and what to do instead.

## Install

There are two things to install: the `sdlc` command, which keeps the loop's
records and enforces its rules, and the Claude Code plugin, which brings the
`/sdlc:next` skill, thirteen agents and the hooks. You will need:

- **Claude Code** 2.1.309 or newer, in a terminal or in the Code tab of Claude
  Desktop
- **git** 2.17 or newer
- **Node** 18 or newer, for the one-line install

### Let your agent do it

Paste this prompt into Claude Code, in the project you want to work on:

```text
Set up sdlc: run `npx @bbsnly/sdlc install`, then read
https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md and follow it
all the way through.
```

The agent installs the command and takes you through the setup. It hands back
to you what only a person can do, such as adding the plugin and typing
`/sdlc:next`.

### Do it yourself

First, install the command. One line does it on macOS, Linux and Windows:

```console
$ npx @bbsnly/sdlc install
```

Next, add the plugin. Type these two commands into Claude Code:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

Then set up your project, from anywhere inside its repository:

```console
$ sdlc init
```

`init` works out what your project is built with, writes a configuration to
match, and never overwrites a file of yours. From here,
[Getting started](https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md)
takes you to your first story in about ten minutes. It has two steps that are
easy to miss: filling in your project's contract, and committing what `init`
wrote before you start a story.

> [!NOTE]
> No Node? In the Code tab of Claude Desktop? Want to check a download
> yourself? [Installation](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md)
> covers every way to install. You may also skip the first step: add the
> plugin, type `/sdlc:next`, and it offers to install the command for you.

## Update

Update the command and the plugin together, so that neither asks something of
the other that it does not know. First the command:

```console
$ npx @bbsnly/sdlc@latest install
```

Then the plugin, in Claude Code:

```text
/plugin update sdlc@sdlc
```

Restart Claude Code afterwards, so that it loads the plugin's new hooks.
`sdlc version` tells you which release you now have. A release with breaking
changes comes with a migration guide, and
[Updating](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md#updating)
lists them all, with how to update a command you installed another way.

## Documentation

New here? Start with
[Getting started](https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md).
The [documentation](https://github.com/bbsnly/sdlc/blob/main/docs/README.md)
lists every page, and these are the ones you are most likely to want:

- [Writing stories](https://github.com/bbsnly/sdlc/blob/main/docs/guides/writing-stories.md):
  acceptance criteria the loop can turn into tests
- [Adding sdlc to an existing project](https://github.com/bbsnly/sdlc/blob/main/docs/guides/adding-to-an-existing-project.md):
  what `init` touches, and what it leaves alone
- [The loop](https://github.com/bbsnly/sdlc/blob/main/docs/the-loop.md): what
  each gate is for, and what it produces
- [Agents](https://github.com/bbsnly/sdlc/blob/main/docs/the-agents.md): who
  does the work at each gate, and which of them can block
- [Commands](https://github.com/bbsnly/sdlc/blob/main/docs/commands.md) and
  [Configuration](https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md):
  every command, and every setting in `.sdlc/config.json`. Its
  [`team` section](https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md#team)
  sets which agents run and which can block
- [Troubleshooting](https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md):
  every error code, and what to do about it

## Contributing

Thank you for considering a contribution. [CONTRIBUTING.md](CONTRIBUTING.md)
says how the project is built and tested. A fresh clone needs only Go, and one
command runs everything CI runs:

```console
$ ./task check
```

To report a vulnerability, please follow [SECURITY.md](SECURITY.md).

## Licence

`sdlc` is open-source software under the [MIT licence](LICENSE).
