# sdlc

A story-driven delivery loop for [Claude Code](https://claude.com/claude-code).
One story per session, nine gates, enforced by hooks rather than by good
intentions: the acceptance tests are frozen before any implementation code is
written, the agent that implements cannot edit them, and nothing reaches trunk
until an independent verifier and a code reviewer have each signed off in a
fresh context.

It is for people who let an agent write real code and want the discipline they
would ask of a colleague — a plan reviewed before implementation, tests written
against the specification rather than against the diff, and a trunk that is
always green.

[![ci](https://github.com/bbsnly/sdlc/actions/workflows/ci.yml/badge.svg)](https://github.com/bbsnly/sdlc/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## Status

All nine gates are built and enforced, and a story can go from the backlog to a
commit without leaving the loop. **The first release is not tagged yet**, so the
`curl` line below has nothing to download; the source route does.
[Watch the repository](https://github.com/bbsnly/sdlc/subscription) to hear when
it lands.

## Install

**1. The binary.**

```console
$ curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh
```

Windows PowerShell: `irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex`.
There is also `npx @bbsnly/sdlc install` and `go install`. Every route downloads
the same native binary and checks it against the release's checksums first — see
[Installation](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md).

Until the first release is tagged, build it from source instead:

```console
$ git clone https://github.com/bbsnly/sdlc && cd sdlc
$ ./task build
$ export PATH="$PWD/dist:$PATH"
```

**2. The plugin**, in Claude Code:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

The repository is its own marketplace, so there is nothing else to add. Working
from a clone instead? Start the session with
`claude --plugin-dir /path/to/sdlc/plugin`.

`npx skills add bbsnly/sdlc` installs the runbook as a plain
[Agent Skill](https://agentskills.io), for any agent that reads them. It is the
runbook only — no binary, no agents, no hook, so nothing is enforced. The plugin
is what makes the loop a loop.

**3. Your project**, from its root:

```console
$ sdlc init
$ sdlc doctor
```

`init` reads the repository, writes a configuration with commands that match it,
and adds the contract section to your `CLAUDE.md`. It never overwrites your own
files, and it writes no `.gitignore`: what a project commits is the project's
decision. `doctor` checks the result and names the command that fixes anything
it does not like.

Then, in Claude Code:

```text
/sdlc:next
```

That is the whole interface.

## What the loop actually does

A story moves through nine gates. Each gate has an owner, and the ones that can
say no are the point:

| Gate | What happens | Can it block? |
| --- | --- | --- |
| 1 · Select | A story is chosen from the backlog and its acceptance criteria agreed | — |
| 2 · Analyse | The story is read against the codebase; threats and security sensitivity decided | — |
| 3 · Test | Acceptance tests are written from the criteria, and **frozen** — hash-locked | — |
| 4 · Plan | The plan is reviewed by an architect, a red team and a security reviewer | **Yes** |
| 5 · Implement | Code is written until the frozen tests pass. Tests cannot be touched | — |
| 6 · Verify | An independent agent re-derives the tests from the spec, hunting test-gaming | **Yes** |
| 7 · Review | The diff is reviewed in a context that never saw the reasoning behind it | **Yes** |
| 8 · Commit | The commit gate checks the tests are still the frozen ones | **Yes** |
| 9 · Retro | Lessons are recorded where the next story will read them | — |

The freeze is what makes the rest mean anything. An agent that can edit its own
acceptance tests will eventually edit them, and every gate after that is
theatre.

Four things are checked rather than asked for, and they are the difference
between a loop and a checklist:

- **A gate cannot pass without what it produces.** No analysis document, no
  analysis gate. The summary that says the document exists is not the document.
- **A gate cannot be recorded out of order.** Each one is done by somebody who
  could only do it because the one before it happened.
- **An approval goes stale.** A review is stamped with what was in front of it —
  the plan's content at the design gate, the whole tree at the code gate — so
  revising the plan or touching the code sends it back to the reviewers who
  approved the old one.
- **The commit waits.** `git commit` is refused until every gate before it has
  passed, and the refusal names the one that has not.

## How enforcement works

Enforcement uses Claude Code's own surfaces and nothing else: hooks, the
permission block, and the plugin's agents. No git hooks are installed, and
`sdlc` never writes to `.git/hooks`.

One rule is worth knowing before you first see it fire. A gate's documents — the
analysis, the plan, the reviews — are stored with `sdlc artifact write` and
`sdlc review add`, not written as files by the agent that produced them. They are
loop state, the same as the gate record, and treating them that way is what stops
the conversation quietly doing a gate's work and then recording a pass on it.

Shell commands are covered too, narrowly: a command that would write the loop's
own record, turn enforcement off, or commit before the gates are done is refused
and told what to run instead. Everything else — your tests, your build, your
tooling — is untouched.

That is a deliberate limit worth stating plainly. This is a discipline tool, not
a sandbox: it constrains an agent that is trying to do the right thing, and it
does not defend against one that is trying to escape.

## Who does the work

Each gate is run by an agent that starts from a fresh context and can only write
what its gate produces. None of them sees the others' reasoning, which is the
whole point: a reviewer that inherited the argument for a change is not a
reviewer.

| Agent | Gate | Can it block? |
| --- | --- | --- |
| `sdlc:researcher` | 2 · analysis and threats | — |
| `sdlc:sdet` | 3 · acceptance tests | — |
| `sdlc:implementer` | 4 · the plan, 5 · the code | — |
| `sdlc:architect` | 4 · design review | **Yes** |
| `sdlc:security` | 4 and 7 | **Yes**, when the story is security-sensitive |
| `sdlc:red-team` | 4 · attacks the plan | — |
| `sdlc:perf` | 4 and 7 | Only against a stated budget |
| `sdlc:human-advocate` | 4 and 7 | — |
| `sdlc:verifier` | 6 · independent verification | **Yes** |
| `sdlc:code-reviewer` | 7 · the diff | **Yes** |
| `sdlc:bookkeeper` | 9 · retro | — |

Advisory does not mean optional. A gate will not pass until every reviewer it
expects has reported, because a reviewer you can skip by not running it is not a
reviewer.

## Prerequisites

- **Claude Code** 2.1.263 or newer
- **git**, and on Windows [Git for Windows](https://git-scm.com/download/win)
  (WSL is not required)
- **Go** 1.26 or newer, to build from source while there is no release

## Documentation

| Page | What is in it |
| --- | --- |
| [Installation](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md) | every way to install it, and how to verify a download |
| [Getting started](https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md) | from nothing to a story going through the loop |
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

MIT — see [LICENSE](LICENSE).
