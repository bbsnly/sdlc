# sdlc

A story-driven delivery loop for [Claude Code](https://claude.com/claude-code).
`sdlc` takes one story per session through nine gates, and hooks hold your
agent to each of them. The acceptance tests are frozen before any
implementation code is written, the agent that implements cannot edit them, and
nothing reaches trunk until an independent verifier and a code reviewer have
each signed off in a fresh context.

It is for people who let an agent write real code and want the discipline they
would ask of a colleague: a plan reviewed before implementation, tests written
against the specification rather than against the diff, and a trunk that is
always green.

[![ci](https://github.com/bbsnly/sdlc/actions/workflows/ci.yml/badge.svg)](https://github.com/bbsnly/sdlc/actions/workflows/ci.yml)
[![coverage](https://codecov.io/gh/bbsnly/sdlc/branch/main/graph/badge.svg)](https://codecov.io/gh/bbsnly/sdlc)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![npm downloads](https://img.shields.io/npm/dm/@bbsnly/sdlc?label=npm%20downloads)](https://www.npmjs.com/package/@bbsnly/sdlc)
[![release downloads](https://img.shields.io/github/downloads/bbsnly/sdlc/total?label=release%20downloads)](https://github.com/bbsnly/sdlc/releases)

## Install

There are three parts: the binary that does the enforcing, the plugin that runs
the loop, and one command in your project. Before you begin, you will need:

- **Claude Code** 2.1.265 or newer
- **git** 2.17 or newer, and on Windows
  [Git for Windows](https://git-scm.com/download/win) (WSL is not required)
- **Node** 18 or newer, for the `npx` command below
- **Go** 1.26 or newer, only to build from source or use `go install`

### Give your agent this prompt

Paste it into Claude Code, in the project you want to work on. It installs
`sdlc` and takes you through the setup.

```text
Set up sdlc: run `npx @bbsnly/sdlc install`, then read
https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md and follow it
all the way through.
```

### Install it yourself

If you would rather type the commands yourself, it takes about two minutes.
First, install the binary. One command does it on macOS, Linux and Windows:

```console
$ npx @bbsnly/sdlc install
```

It downloads the native binary built for your machine and checks it before
installing it. If it prints lines to add a directory to your `PATH`, add them.

Then, in Claude Code, add the plugin. The repository is its own marketplace, so
there is nothing else to add:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

Finally, from the root of your project:

```console
$ sdlc init
```

`init` reads the repository, writes a configuration with commands that match it,
and adds the contract section to your `CLAUDE.md`. It never overwrites your own
files.

> [!WARNING]
> Commit what `init` wrote before your first story. Each new story starts in a
> worktree cut from trunk's commits, so only what is committed is there.

Once that is committed, `sdlc doctor` checks the result and says what to do
about anything it does not like.

### Other ways to install

No Node? Any one of these gives you the same binary:

| Route | Command |
| --- | --- |
| macOS, Linux | `curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh \| sh` |
| Windows PowerShell | `irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 \| iex` |
| Go 1.26 or newer | `go install github.com/bbsnly/sdlc/cmd/sdlc@latest` |
| By hand | the prebuilt archive for your platform from [Releases](https://github.com/bbsnly/sdlc/releases), checked against `checksums.txt`, [step by step](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md#by-hand-from-a-release) |
| From source | `git clone https://github.com/bbsnly/sdlc && cd sdlc && ./task build` |

The `npx` command and the two scripts download the same native binary and check
it against the release's checksums before it goes anywhere near your `PATH`;
`go install` and the source build compile it on your machine. You can also skip
the binary step: install the plugin, type `/sdlc:next`, and it offers to
install the binary for you.

[Installation](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md)
covers every route in full, how to verify a download yourself, the other ways
to load the plugin, and
[what the plugin does without the binary](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md#the-plugin-without-the-binary).

## Using it

In Claude Code, you type one command:

```text
/sdlc:next
```

It works the current story through its gates, one at a time, until the story is
committed and its retro is written, or until a gate needs a person. Run it again
after that, or after a session that ended part-way, and it picks up at the first
gate that has not passed. That is the only command you need.

Each new story is worked in a worktree of its own, `.sdlc/worktrees/<ID>`, on a
branch `story/<ID>` cut from trunk, and committed there, so your checkout is
left as it was and a second session can work a second story at the same time.
When the retro is written, `sdlc stop` in the story's worktree checks the story,
moves trunk to it by fast-forward and takes the worktree away.
[The loop](https://github.com/bbsnly/sdlc/blob/main/docs/the-loop.md#done-when-it-lands)
says what it checks, and `git.worktrees` in the configuration turns it off.

Underneath, everything the skill does is the `sdlc` command, and you can run any
of it yourself: `sdlc status` shows where a story is, and `sdlc story list`
shows the backlog. Every command takes `--json`, so a skill reads exactly what a
person reads. [Commands](https://github.com/bbsnly/sdlc/blob/main/docs/commands.md)
has all of them.

## What the loop actually does

A story moves through nine gates. Each gate has an owner, and the ones that can
say no are the point:

| Gate | What happens | Can it block? |
| --- | --- | --- |
| 1 · Select | A story is chosen from the backlog and its acceptance criteria agreed | none |
| 2 · Analyse | The story is read against the codebase; threats and security sensitivity decided | none |
| 3 · Test | Acceptance tests are written from the criteria, and **frozen** (hash-locked) | none |
| 4 · Plan | The plan is reviewed by five agents, among them an architect, a red team and a security reviewer | **Yes** |
| 5 · Implement | Code is written until the frozen tests pass. Tests cannot be touched | none |
| 6 · Verify | An independent agent re-derives the tests from the spec, hunting test-gaming | **Yes** |
| 7 · Review | The diff is reviewed in a context that never saw the reasoning behind it | **Yes** |
| 8 · Commit | The commit gate checks the tests are still the frozen ones | **Yes** |
| 9 · Retro | Lessons are recorded where the next story will read them | none |

The freeze is what makes the rest mean anything. An agent that can edit its own
acceptance tests will eventually edit them, and every gate after that stops
meaning anything.

Four things are checked by the command itself:

- **A gate cannot pass without what it produces.** No analysis document, no
  analysis gate. The summary that says the document exists is not the document.
- **A gate cannot be recorded out of order.** Each one is done by somebody who
  could only do it because the one before it happened.
- **An approval goes stale.** A review is stamped with what was in front of it
  (the plan's content at the design gate, the whole tree at the code gate), so
  revising the plan or touching the code sends it back to the reviewers who
  approved the old one.
- **The commit waits.** `git commit` is refused until every gate before it has
  passed, and the refusal names the one that has not.

## How enforcement works

The plugin gives you three skills that only you start (`/sdlc:next`,
`/sdlc:trunk-review` and `/sdlc:consolidate`), twelve agents, and hooks on
Claude Code's tool calls. Each gate is run by an agent that starts from a fresh
context and sees none of the others' reasoning, because a reviewer that
inherited the argument for a change is not a reviewer. The hook and the `sdlc`
command refuse what would bypass a gate, and every refusal says what to run
instead. No git hooks are installed, and your tests, your build and your tooling
are untouched.

Two limits are worth knowing from the start:

- **This is a discipline tool, not a sandbox.** It constrains an agent that is
  trying to do the right thing, and it does not defend against one that is
  trying to escape.
- **The agents are steered, not blocked.** Each is told to work only a gate of
  a story you started and to stop on anything else, but nothing enforces that.
  The plugin does nothing until a person types `/sdlc:next`, and without the
  binary nothing is enforced at all.

[Enforcement](https://github.com/bbsnly/sdlc/blob/main/docs/enforcement.md)
lists every rule and what to do instead, and
[Agents](https://github.com/bbsnly/sdlc/blob/main/docs/agents.md) says who does
the work at each gate and which of them can block.

## Documentation

If you are new, start with Getting started. The other pages are there when you
need them.

| Page | What is in it |
| --- | --- |
| [Installation](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md) | every way to install it, and how to verify a download |
| [Getting started](https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md) | from nothing to a story going through the loop |
| [Guides](https://github.com/bbsnly/sdlc/blob/main/docs/README.md) | how to do one job: set up an existing project, write stories, resume a story, act on a blocked review, and more |
| [The loop](https://github.com/bbsnly/sdlc/blob/main/docs/the-loop.md) | what each gate is for, what it produces, what is refused without it |
| [Commands](https://github.com/bbsnly/sdlc/blob/main/docs/commands.md) | every command and flag |
| [Configuration](https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md) | every setting in `.sdlc/config.json` |
| [Enforcement](https://github.com/bbsnly/sdlc/blob/main/docs/enforcement.md) | every rule, what it refuses, and what to do instead |
| [Agents](https://github.com/bbsnly/sdlc/blob/main/docs/agents.md) | who does the work |
| [Troubleshooting](https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md) | every error code, and what to do about it |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). A fresh clone needs only Go:

```console
$ ./task check
```

Every other tool is pinned and fetched on demand.

## License

MIT, see [LICENSE](LICENSE).
