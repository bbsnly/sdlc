# Troubleshooting

Every error `sdlc` prints ends with a code. Search this page for that code; each one has a
heading, what it means, and what to do next.

Codes are stable. Once published, a code keeps its meaning forever, so a code you find in an
old issue or a log still points at the same condition.

## Error codes

### SDLC-E0001

You ran `sdlc` outside a Git repository. The loop keeps its state next to your code and
compares your working tree against `HEAD`, so it needs a repository to work in.

Run `git init`, or change to a directory inside the repository you meant to use.

### SDLC-E0002

This repository has no `.sdlc/config.json`, so it does not take part in the loop yet.

Run `sdlc init` in the root of the repository. It writes the configuration, a starter backlog,
and the contract section your project's `CLAUDE.md` needs.

### SDLC-E0003

`sdlc init` found an existing `.sdlc/config.json` and stopped rather than overwrite your
settings.

If you meant to change something, edit that file directly. If you want the defaults back, run
`sdlc init --force`.

### SDLC-E0004

`.sdlc/config.json` exists but could not be parsed. Usually this is a trailing comma or an
unquoted key left behind by a hand edit.

Fix the JSON, or delete the file and run `sdlc init` again to get a fresh one.

### SDLC-E0005

A file under `.sdlc/state/` could not be read. These files are written by `sdlc` itself, so a
corrupt one is a bug rather than something you did.

Please open an issue at <https://github.com/bbsnly/sdlc/issues> with the code above.

### SDLC-E0006

`sdlc` could not write to `.sdlc/`. State is written atomically, so nothing was left
half-finished — but nothing was recorded either.

Check that the directory is writable and that the disk is not full.

### SDLC-E0007

The backlog file named by `backlog.path` in `.sdlc/config.json` does not exist.

Run `sdlc init` to create it, or point `backlog.path` at the file you already keep your
stories in.

### SDLC-E0008

The backlog exists but could not be parsed, or a story in it is missing a required field.
Every story needs an `id` and a `title`.

Fix the JSON and try again. The message names the story that stopped it.

### SDLC-E0009

No story in the backlog has the id you asked for.

Run `sdlc story list` to see the ids you can use. Ids are compared exactly, including case.

### SDLC-E0010

Nothing in the backlog can be started right now: every story is finished, blocked, or waiting
on a dependency that is not done.

Run `sdlc story list` — it shows each story's status and what is holding it back — then add a
story or unblock one.

### SDLC-E0011

The command you ran acts on the story currently being worked on, and no iteration is active.

Run `sdlc start` to begin one on the next runnable story.

### SDLC-E0012

An iteration is already active on another story. The loop runs one story at a time on purpose:
that is what keeps a diff small enough to review honestly.

Finish the current story, or run `sdlc stop` to end the iteration without recording a result.

### SDLC-E0013

You named a gate this version does not know.

Run `sdlc gate --help` for the gate names. They are stable within a major version.

### SDLC-E0014

A gate result must be `pass`, `fail`, or `pending`.

### SDLC-E0015

A story id becomes a directory name under `.sdlc/stories/`, so it has to be usable as one. The
id you used contains a path separator, a `..`, or a character that is not safe in a filename.

Rename the story. Ids of letters, digits, dots, dashes and underscores always work — `AUTH-3`,
`US-001`, `billing.2` — and the backlog schema asks for the `PREFIX-123` shape.
