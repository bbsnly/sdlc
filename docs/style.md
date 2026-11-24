# Writing the documentation

- [Introduction](#introduction)
- [Start from the reader](#start-from-the-reader)
- [The shape of a page](#the-shape-of-a-page)
- [Voice](#voice)
- [Words](#words)
- [Headings](#headings)
- [Commands and code](#commands-and-code)
  - [When to show output](#when-to-show-output)
- [Notes and warnings](#notes-and-warnings)
- [Links](#links)
- [Error codes and internal names](#error-codes-and-internal-names)
- [Edge cases](#edge-cases)
- [Before and after](#before-and-after)
- [Before you send a page](#before-you-send-a-page)

## Introduction

These pages are for a developer reading in a browser who wants to get
something done. They are not a specification, and they are not written for an
agent to parse. This guide says how to write one, and every page, and every
story that changes a page, follows it.

The model is [Laravel's documentation](https://laravel.com/docs). Its pages
open by saying what a thing is and why you would use it. Their headings are
tasks. A command sits exactly where you need it, with a sentence or two after
it. Where this guide does not settle a question, read a Laravel page and do
what it does.

## Start from the reader

A page is designed around its reader's task, not around the code's structure.
Before you write anything, answer three questions:

1. Who is reading, and what have they just done?
2. What did they come here to do? Say it as one task, in their words.
3. What do they need first, in what order, and what can wait?

Then write the page for that path, starting from an empty file. The code has
commands, flags, settings and rules; the reader has a job. A page shaped like
the code, with a section for every flag and a sentence for every branch,
answers questions nobody asked and buries the one they did.

When you rewrite a page, the old one is a source of facts and nothing else: not
its order, not its paragraphs. Keep a sentence only where it is already the
best way to say the thing. A true fact that another user will look for is not
deleted. It moves to the page where they will look, and you check it is there
before you cut it here.

These pages are for people who use sdlc, not for people who work on it. What
only shows how sdlc works inside, such as package names, the files it keeps
for itself, how it hashes or locks, what the hook passes to Claude Code, and
why the code was built the way it was, is deleted, not moved. Keep what a user
needs to act, to decide, or to trust what the tool did.

## The shape of a page

Each kind of page has its own reader:

| Kind | Its reader | Its shape |
| --- | --- | --- |
| [Getting started](getting-started.md) | new, and wants a first result | one path from nothing to working |
| A guide, under `guides/` | has one job to do, or has hit one situation | that job, in the order it is done |
| How it works, such as [The loop](the-loop.md) | wants to understand | each idea in plain words, then its details |
| Reference, such as [Commands](commands.md) | is looking one thing up | one entry for each thing, every entry alike |

Whatever its kind, a page runs in this order:

1. **The title, and a list of its sections**, each linked.
2. **An Introduction.** What this is, why you would want it, and what the page
   covers, in two or three short paragraphs.
3. **The main task.** The common case, in the order the reader does it.
4. **The details.** Variations and the less common cases, each under a heading
   of its own.
5. **Where to go next.** A short list of links.

The reference pages are where completeness lives. Every other page may leave
things out, and should.

## Voice

- **Speak to the reader as "you".** The tool is `sdlc`, or the command by name:
  "`init` writes five files". Use "let's" to walk through an example. There is
  no "we" and no "the user".
- **Present tense, active verbs.** "`init` writes five files", not "five files
  will be written".
- **Warm, direct and confident.** Say what to do, then why in a clause. "Don't
  worry" is welcome where a reader might, and "you may" where there is a
  choice. Leave out "simply", "just", "easy" and "obviously".
- **Short paragraphs.** One idea each, in two to four sentences. A sentence that
  needs three commas is two sentences.
- **Honest about limits.** Where the tool does not enforce something, say so
  once, plainly. Everywhere else, do not hedge.
- **Plain British English.** Behaviour, artefact, recognise, licence.
  Contractions are fine where they sound natural.

## Words

[The loop](the-loop.md#introduction) defines the words that carry weight here:
gate, freeze, escalate, blocking and advisory, a person. Use each the same way
every time, and never swap in a synonym for variety. A gate is not a stage or a
step, and a freeze is not a lock.

- **Say a term in plain words the first time a page uses it.** "It escalates:
  it hands the story to you and stops."
- **The trunk branch is trunk**, never the name a project happens to give it.
  That name appears only in output and configuration you reproduce.
- **"You" or "a person"?** Write "you" when you are talking to the reader.
  Write "a person" when the point is the line between people and agents: "only
  a person can start the loop".
- **No path from anyone's machine.** Write `/home/<user>/src/billing`.
- **Keep to the examples the other pages use**, such as the story `AUTH-3`,
  so that a reader meets the same project on every page.

## Headings

Name a heading after what the reader is doing or asking: "Write a story",
"Commit the setup", "When a reviewer blocks". Use sentence case, and keep it
short enough to scan in the list at the top of the page.

Do not name a heading after the code. "Gate 3 — `tests_frozen`" tells a new
reader nothing; "Gate 3: freeze the tests" does. The reference pages are the
exception. There an entry is headed by the thing being looked up, such as
`sdlc init` or `SDLC-E0053`, because that is what the reader searches for.

Others depend on an anchor: other pages, `README.md`, the links `sdlc` prints
with an error, and some tests. Before you rename a heading, search for the
links to it (`grep -rn 'getting-started.md#' .`), then keep it or change them
all.

## Commands and code

Put a command right where the reader needs it. Lead in with one sentence that
says what it does and where to run it. Follow with a sentence or two on what
happened, or what to look for.

- Use `console`, with a `$` before each line the reader types in a terminal.
- Use `text` for what is typed into Claude Code, such as `/sdlc:next`, and for
  output shown on its own.
- Use `json` or `markdown` for a file's contents.
- One step to a block. Where a value is the reader's own, write it as `<ID>`.

Every command and every example is real. Run it against a binary built from
the tree you are documenting. Tests check some of them too: the story on the
Getting started page is decoded as a backlog.

### When to show output

Show output when the reader needs it to recognise success, or has to read it
to decide what to do next. Leave it out when it only repeats the command, or
when it differs on every machine, as a version does.

- **Exact.** Copy it from the binary, never from memory or from another page.
- **Whole, or one line.** Show all of it, or quote a single line and say which
  it is ("its last line reads"). Never trim it silently.
- **One substitution.** The machine's own path becomes a placeholder.

A message that changes in the code changes in the same commit on every page
that quotes it.

## Notes and warnings

A **note** (`> [!NOTE]`) is worth knowing, but the main path survives without
it: a shortcut, or a pointer for a reader whose situation differs. A
**warning** (`> [!WARNING]`) is for what bites: lost work, a wasted hour, or
something that cannot be undone. Say what to do first, then what happens
otherwise.

Keep each to three sentences and a link. One to a section is plenty, and two
never sit together. The next step is never inside one: a reader who skips
every note and warning still finishes the task.

## Links

- **Link the words that name the destination.**
  "[Writing stories](guides/writing-stories.md) lists every pattern", never
  "click here" and never a bare address.
- **Link for depth, never for a missing step.** The reader finishes the task
  without leaving the page.
- **Link a destination once in a section**, the first time it comes up, and
  link to the section when it is a section you mean.
- **Use relative links between these pages.** `README.md` links by full address,
  because it is shown in places these pages are not.
- **Give every new page a line in [the index](README.md).** A page nothing
  links to fails the build.

## Error codes and internal names

A narrative page says what the reader sees and what to do about it. Codes and
internal names belong where they are looked up.

An `SDLC-E` code belongs in [Troubleshooting](troubleshooting.md), which has a
heading for each, and in the reference entry of the command that prints it. In
the narrative, say what happens and link those words to the code's entry. A
code does appear in output you reproduce, because that is where the reader
meets it.

The same goes for internal names: a gate's record key, a rule's name, an
agent's identifier, a file under `.sdlc/state/`, an environment variable. They
belong in [Commands](commands.md), [Configuration](configuration.md),
[Enforcement](enforcement.md) and [Agents](the-agents.md). A name the reader
types or sees while doing the task is part of the task, and stays:
`/sdlc:next`, `"status": "ready"`, `.sdlc/config.json`.

| Instead of | Write |
| --- | --- |
| A story started before the commit is refused with SDLC-E0053. | Until you commit, `sdlc` [refuses to start a story](troubleshooting.md#sdlc-e0053). |
| `sdlc:sdet` records `tests_frozen`. | The test author writes a failing test for each criterion, then freezes them. |
| The story becomes `awaiting_human`. | The story waits for you. |

## Edge cases

The main path is the common case. For anything else, ask whether a reader with
an ordinary project would meet it today, doing this page's task.

- **Yes, and it bites:** one note or warning where it happens, with a link.
- **Sometimes:** a section of its own, later in the page.
- **Rarely:** the reference page, and nowhere else.

A sentence in the main path that begins "Where", "Unless" or "With that
setting on" is usually an edge case that has not been moved yet.

## Before and after

This paragraph is from the [`sdlc init`](commands.md#sdlc-init) reference
entry. Every word is true, and as reference it earns its place. As a first
meeting with `init`, it answers six questions before the reader has asked one:

> `init` reads the repository, detects the stack, and writes
> `.sdlc/config.json` with commands that already match it, a backlog with one
> example story, the story schema, and a `## SDLC Contract` section appended to
> `CLAUDE.md`. Where the project has an `AGENTS.md` and no `CLAUDE.md`, or its
> `CLAUDE.md` is a link to `AGENTS.md`, the section goes in `AGENTS.md`
> instead, and no `CLAUDE.md` is made: Claude Code reads `AGENTS.md` only where
> there is no `CLAUDE.md`. init then prints `created  AGENTS.md (appended to)`.
> Beside them it writes `.sdlc/.gitignore`, which keeps each story's
> `trace.jsonl` on this machine ([the trace](the-loop.md#the-trace)); it is not
> in the list init prints. It never overwrites a file you wrote, and it never
> touches your own `.gitignore`.

[Getting started](getting-started.md#set-up-your-project) tells the same reader
what to do, shows what they will see, and says the one thing to know next:

> With both installed, you can set up the project you want to work on. From
> anywhere inside its repository, run `sdlc init`, or type `/sdlc:init` in
> Claude Code.
>
> *(The command, and all of what it prints.)*
>
> `init` works out what your project is built with (it recognises Go, Rust,
> Node and Python) and fills in the commands that build, test and lint it.

The `AGENTS.md` case became a short note with a link. The linked `CLAUDE.md`
and the ignore file stayed in the guide and the reference, where the reader
who needs them will look.

## Before you send a page

- You can say who the reader is, and the page gives them the main task before
  anything else.
- Every command and every line of output was run against a binary built from
  this tree.
- Every fact you cut is on the page it moved to, and the links to any heading
  you renamed still land.
- `./task markdownlint` and `./task linkcheck` pass, and so do the tests that
  read the pages. `go test ./internal/cli ./internal/plugin` runs most of them,
  and `./task check` runs them all. Where a test holds a phrase you would
  rather change, say so in the review and leave the test as it is.
