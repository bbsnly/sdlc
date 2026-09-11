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

Nothing in the backlog can be started right now. The message says which: every story is
finished, or some are blocked or waiting on a dependency that is not done.

A finished backlog is the loop having done its job — write the next story. Otherwise run
`sdlc story list`, which shows each story's status and what is holding it back.

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

### SDLC-E0016

You asked to store a document the loop does not know about.

Run `sdlc artifact list` for the names this version accepts. They are tied to the gate that
produces them, so a new one arrives with the gate that needs it.

### SDLC-E0017

The document you asked to store was empty.

The content is read from standard input, or from the path given to `--file`. If you are piping
from another command, check that it produced anything.

### SDLC-E0018

The document could not be read.

With `--file`, the path does not exist or cannot be opened; check it, or drop the flag and pipe
the document in instead. Without it, reading from standard input stopped part way through, which
usually means whatever was producing the document failed.

### SDLC-E0019

The document is bigger than the largest one `sdlc` will store.

A gate's document is prose that a person reads and that later gates review, so a megabyte is
already generous. Storing a truncated one would be worse than refusing: every gate after this
one would review something that stops mid-sentence. Keep the document itself here and link to
the bulk — the log, the dataset, the capture — from inside it.

### SDLC-E0020

You recorded a pass for a gate whose documents are not there.

A gate's documents are what the gates after it read — not the summary that said they exist. Store
them with `sdlc artifact write`, then record the gate. The error names the ones that are missing.

Only a pass is held to this. A gate can fail because its work could not be done, and that has to
stay recordable.

### SDLC-E0021

The repository's files could not be listed.

`sdlc freeze` asks git which files are part of the working tree, so that build output and
vendored code cannot be mistaken for acceptance tests. Check that git is installed and on your
`PATH`, and that this directory is a repository git can read. `sdlc doctor` checks both.

### SDLC-E0022

The tests are already frozen.

A freeze covers one story's acceptance tests, and taking a second one over the top would quietly
bless whatever changed in between. If the tests genuinely have to change, run
`sdlc unfreeze --reason "..."` first — the reason goes on the record — and freeze again.

### SDLC-E0023

There is no freeze.

The test gate cannot pass until `sdlc freeze` has recorded what every acceptance test contains,
because without it a later edit to a test leaves no trace. If the message says the freeze belongs
to another story, that freeze is stale: unfreeze it and take one for this story.

### SDLC-E0024

No test files were found.

Either the acceptance tests have not been written yet, or `paths.tests` in `.sdlc/config.json`
does not describe where this project keeps them. It matches a file by the directory it is in
(`dirs`) or by its name (`file_globs`), and `sdlc init` fills both in from the stack it detected.

### SDLC-E0025

A frozen test is not what was frozen.

The named files changed, or are gone, since the freeze was taken. That is the situation the
freeze exists to make visible: an agent that can edit its own acceptance tests will eventually
edit them, and every gate after that is theatre.

Restore them, or — if a test really did encode the wrong behaviour — run
`sdlc unfreeze --reason "..."` and freeze again, so that the change is a decision somebody can
review rather than something that happened quietly.

### SDLC-E0026

Lifting the freeze needs a reason.

`--reason` takes one line saying which acceptance criterion the test got wrong. Lifting the
freeze is sometimes right and is also exactly the move an agent would make to reach green;
recording why is what tells the two apart.

### SDLC-E0027

That gate does not expect a review from that role.

Reviews are registered per gate: the architect reviews the design, the code reviewer reviews the
diff, and so on. `sdlc review list` shows which reviews each gate expects and where each has got
to. If a role you want is not there, it is not part of this gate.

### SDLC-E0028

A reviewer approves, blocks, or leaves a note.

`approve` is what lets a blocking reviewer's gate pass. `block` stops it. `note` records a
finding that is worth having on the record but does not stand in the way — it is what an
advisory reviewer usually files.

### SDLC-E0029

The gate's reviews are not in.

Either a reviewer has not reported, or one reported on something that has changed since. The
second case is the one worth understanding: a review is stamped with what was in front of it —
the plan's content at the design gate, the whole tree at the code gate — so revising the plan or
touching the code makes every earlier approval stale. That is deliberate. An approval of a plan
that has since changed is not an approval.

Send the change back to the reviewers that are outstanding; `sdlc review list` names them.

### SDLC-E0030

A blocking reviewer blocked.

This is the loop working. Fix what the reviewer found, then have the **same** reviewer look
again — its new verdict replaces the old one, and the gate reads the latest. Recording the gate
as failed and moving on is not an option the tool offers, because it is the one that would make
every review optional.

### SDLC-E0031

A gate was recorded out of order.

Each gate is done by somebody who could only do it because the one before it happened: the plan
is written against frozen tests, the code is written against a reviewed plan, the review reads a
verified implementation. `sdlc status` shows where the story stands.

If an earlier gate failed, record it again once it genuinely passes. There is no flag to skip it.

### SDLC-E0032

The commit gate cannot pass while there is uncommitted work.

The gate records that this story reached trunk. Commit the change, or stash what does not belong
to this story, and then record the gate.

### SDLC-E0033

The story you asked to start has passed every gate. Starting it would put work that is already
done back in progress, with nothing on the record saying why.

If the iteration is still open, `sdlc stop` ends it. If the work genuinely has to come back —
a review reopened, a bug found after the commit — record the gate that failed:

```console
$ sdlc gate code_review fail --note "AC-2 turned out to be untested"
```

That puts the story back to `in_progress` and says on the record why it came back. Being done
is a reading of the gate record, not a door that locks behind you.
