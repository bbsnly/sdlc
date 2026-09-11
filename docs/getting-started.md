# Getting started

This page takes you from nothing to a story going through the loop. It should
take about ten minutes, most of which is writing one good acceptance criterion.

## Install

```console
$ curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh
```

On Windows, `irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex`.
There is also `npx @bbsnly/sdlc install`, `go install`, and a source build —
[Installation](installation.md) has all of them, and how to verify a download.

Then, in Claude Code:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

The repository is its own marketplace, so there is nothing else to add.

## Set a project up

From inside the repository you want to work on:

```console
$ sdlc init
```

`init` reads your repository, works out what it is built with, and writes:

| File | What it is |
| --- | --- |
| `.sdlc/config.json` | the loop's settings, with commands that already match your project |
| `user_stories.json` | your backlog, with one example story in it |
| `.sdlc/story.schema.json` | what a story may contain |
| `CLAUDE.md` | a `## SDLC Contract` section appended, if it is not already there |

It never overwrites your own files, and it writes no `.gitignore`: what a
project commits is the project's decision. Run it again with `--force` to
restore the default settings while keeping your stories.

Then check it is happy:

```console
$ sdlc doctor
```

`doctor` looks at the handful of things that actually stop the loop working —
git, the configuration, the backlog, the contract, the commands your config
names, and whether `sdlc` is on your `PATH`. Every problem it reports comes with
the command that fixes it.

## Fill in the contract

`init` appends a `## SDLC Contract` section to your `CLAUDE.md` with every field
marked `TBD`. Fill it in before you run a story, because it is what the design
reviewer and the code reviewer check against — a contract full of `TBD` gives
them nothing to hold the work to.

You do not need all of it. The three that earn their keep first are the
architecture rules, the test conventions, and the glossary.

## Write a story

Stories live in the file named by `backlog.path` in `.sdlc/config.json`, which
`init` sets to `user_stories.json` at the repository root.

```json
{
  "schema": "sdlc/story/1",
  "stories": [
    {
      "id": "AUTH-3",
      "title": "Reject expired sessions",
      "status": "ready",
      "priority": 1,
      "acceptance_criteria": [
        {
          "id": "AC-1",
          "type": "event",
          "text": "When a request carries a session token whose expiry is in the past, the API shall respond 401 and shall not touch the session store."
        },
        {
          "id": "AC-2",
          "type": "unwanted",
          "text": "If the token cannot be parsed, then the API shall respond 401 with no detail about why."
        }
      ]
    }
  ]
}
```

The criteria are the important part. Each one has to describe something you
could observe from outside — a response, a returned value, a stored record — and
not how the code does it. If a criterion cannot be turned into a failing test,
the first gate will say so and stop, which is the cheapest place in the whole
loop to find out.

`status` is `todo`, `ready`, `in_progress`, `blocked`, `awaiting_human` or
`done`. `priority` is lowest-first, and a story with no priority goes last.

```console
$ sdlc story list
```

shows the backlog and says what is holding each story back.

## Run it

In Claude Code, from the project:

```text
/sdlc:next
```

That is all, once the plugin is installed. If you are working from a clone
rather than an installed plugin, start the session with
`claude --plugin-dir /path/to/sdlc/plugin` instead.

That is the whole interface. The skill picks up the next runnable story, or
resumes one already under way, and works it through the gates one at a time —
delegating each to the agent whose gate it is, and recording every outcome
before the next begins.

You will be asked to decide things. That is deliberate: the loop stops rather
than guessing when the specification does not settle something.

## What you will see happen

1. **Gate 1** checks your acceptance criteria are testable and picks the story.
2. **Gate 2** sends a researcher into a fresh context to read the story against
   your codebase and write down what it touches and what could go wrong.
3. **Gate 3** turns each criterion into a test that fails, then freezes every
   test file by content. From here, nobody edits them.
4. **Gate 4** writes a plan and puts it in front of five reviewers.
5. **Gate 5** implements the plan until the frozen tests pass.
6. **Gate 6** sends a verifier that has not seen any of it to re-derive the
   criteria and hunt for tests that pass without the behaviour being there.
7. **Gate 7** reviews the diff in a context that never saw the reasoning.
8. **Gate 8** commits — and not before.
9. **Gate 9** writes the retro.

At any point:

```console
$ sdlc status
$ sdlc review list
```

## When something goes wrong

Every failure carries a code, what happened, why, and what to do about it. Look
the code up in [troubleshooting](troubleshooting.md).

If the loop refuses something, read the refusal before working around it. It
names the sanctioned route, and the route is usually one command.

## Where to go next

- [The loop](the-loop.md) — what each gate is for and what it produces
- [Commands](commands.md) — every command and flag
- [Configuration](configuration.md) — every setting
- [Enforcement](enforcement.md) — every rule, and why it exists
- [Agents](agents.md) — who does the work
