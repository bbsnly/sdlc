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

## Status: early, and honest about it

There is no release, no npm package and no published plugin yet. What exists is
the first vertical slice: a story can be selected and analysed, with both gates
recorded and a hook enforcing who may write where while it happens. Gates 3 to 9
are not built.

[Watch the repository](https://github.com/bbsnly/sdlc/subscription) to hear
about the first release.

### Trying it from source

```console
$ git clone https://github.com/bbsnly/sdlc && cd sdlc
$ ./task build
$ export PATH="$PWD/dist:$PATH"
```

Then, in a project of your own:

```console
$ sdlc init
$ sdlc status
```

`init` reads the repository, writes a configuration with commands that match it,
and adds the contract section to your `CLAUDE.md`. It never overwrites your own
files, and it writes no `.gitignore`: what a project commits is the project's
decision.

To use the two gates that exist, start Claude Code with the plugin loaded and
run `/sdlc:next`:

```console
$ claude --plugin-dir /path/to/sdlc/plugin
```

## What the loop actually does

A story moves through nine gates. Each gate has an owner, and the ones that can
say no are the point:

| Gate | What happens | Can it block? | Built |
| --- | --- | --- | --- |
| 1 · Select | A story is chosen from the backlog and its acceptance criteria agreed | — | Yes |
| 2 · Analyse | The story is read against the codebase; threats and security sensitivity decided | — | Yes |
| 3 · Test | Acceptance tests are written from the criteria, and **frozen** — hash-locked | — | Not yet |
| 4 · Plan | The plan is reviewed by an architect, a red team and a security reviewer | **Yes** | Not yet |
| 5 · Implement | Code is written until the frozen tests pass. Tests cannot be touched | — | Not yet |
| 6 · Verify | An independent agent re-derives the tests from the spec, hunting test-gaming | **Yes** | Not yet |
| 7 · Review | The diff is reviewed in a context that never saw the reasoning behind it | **Yes** | Not yet |
| 8 · Commit | The commit gate checks the tests are still the frozen ones | **Yes** | Not yet |
| 9 · Retro | Lessons are recorded where the next story will read them | — | Not yet |

The freeze is what makes the rest mean anything. An agent that can edit its own
acceptance tests will eventually edit them, and every gate after that is
theatre.

## How enforcement works

Enforcement uses Claude Code's own surfaces and nothing else: hooks, the
permission block, and the plugin's agents. No git hooks are installed, and
`sdlc` never writes to `.git/hooks`.

One rule is worth knowing before you first see it fire. A gate's documents — the
analysis, the threat assessment — are stored with `sdlc artifact write`, not
written as files by the agent that produced them. They are loop state, the same
as the gate record, and treating them that way is what stops the conversation
quietly doing a gate's work and then recording a pass on it.

That is a deliberate limit worth stating plainly. This is a discipline tool, not
a sandbox: it constrains an agent that is trying to do the right thing, and it
does not defend against one that is trying to escape.

## Prerequisites

- **Claude Code** 2.1.263 or newer
- **git**, and on Windows [Git for Windows](https://git-scm.com/download/win)
  (WSL is not required)
- **Go** 1.26 or newer, to build from source while there is no release

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). A fresh clone needs only Go:

```console
$ ./task check
```

Every other tool is pinned and fetched on demand.

## License

MIT — see [LICENSE](LICENSE).
