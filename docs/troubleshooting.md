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

It is also the code for a setting of the wrong type — `"3"` where a number belongs, or a
string where a list does. That is valid JSON, so a JSON checker finds nothing wrong with it; the
message names the setting, what it takes, and the line it is on.

Fix the JSON, or delete the file and run `sdlc init` again to get a fresh one.

### SDLC-E0005

A file the loop depends on could not be read: the iteration or the freeze under `.sdlc/state/`, a
story's `gate-record.json` under `.sdlc/stories/`, or a test file the freeze has to hash. The
message names it.

`sdlc` writes its own files whole or not at all, so one that will not parse was usually edited
or merged by hand — gate records are committed with the work, and a merge conflict in one is
enough. `sdlc doctor` lists every file it cannot read. Restore a committed one from git, fix
the permissions on one that cannot be opened, and run `sdlc stop` if it is the iteration file.

If `sdlc` itself left a file it cannot read, that is a bug: please open an issue at
<https://github.com/bbsnly/sdlc/issues> with the code above.

### SDLC-E0006

`sdlc` could not write to `.sdlc/`. State is written atomically, so nothing was left
half-finished — but nothing was recorded either.

Check that the directory is writable and that the disk is not full.

It is also the code for **another sdlc command is still running**. Every command that writes
loop state takes a lock first, and waits up to ten seconds for a command that holds it. The
message names that command and its process id. A command stopped with Ctrl+C gives the lock
back on its way out. If nothing is running, the lock was left by a command that could not: one
killed outright, or a machine that went down. It is broken open once it has gone two minutes
without its holder refreshing it, or delete the directory the message names.

### SDLC-E0007

The backlog file named by `backlog.path` in `.sdlc/config.json` does not exist.

Run `sdlc init` to create it, or point `backlog.path` at the file you already keep your
stories in.

### SDLC-E0008

The backlog exists but could not be parsed, or a story in it is missing a required field.
Every story needs an `id` and a `title`, and a `status` the loop knows: `todo`, `ready`,
`in_progress`, `awaiting_human`, `blocked`, `done` or `dropped`, spelled exactly so. A story
with any other status would never be picked, so the whole backlog is refused instead.

Fix the JSON and try again. The message names the story that stopped it.

### SDLC-E0009

No story in the backlog has the id you asked for.

Run `sdlc story list` to see the ids you can use. Ids are compared exactly, including case.

### SDLC-E0010

Nothing in the backlog can be started right now. The message says which: every story is
finished, or some are blocked or waiting on a dependency that is not done.

A finished backlog is the loop having done its job — write the next story. Otherwise run
`sdlc story list`, which shows each story's status and what is holding it back.

The same code refuses a story you named to `sdlc start` that could not have been picked: one
that is `dropped`, `blocked` or marked `done` in the backlog, or one whose `depends_on` names a
story that is not done. Naming a story puts it ahead of the priority order; it does not start
work the backlog says cannot start. Change the backlog if it is wrong, or finish the dependency
first.

### SDLC-E0011

The command you ran acts on the story currently being worked on, and no iteration is active.

Run `sdlc start` to begin one on the next runnable story. `sdlc cost` and `sdlc cost add` also
take `--story`, which is how a runner records what a session cost after its iteration ended.

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
bless whatever changed in between. If the tests genuinely have to change, a person runs
`sdlc unfreeze --reason "..."` in their own terminal first — the reason goes on the record, and
the hook refuses it from an agent — and then freezes again.

With `freeze.allow_new_test_files` on, `sdlc freeze` on the story's own freeze adds the test
files written since it was taken, and refuses only when there are none to add.

This is about the story you are working on. A freeze left behind by a story that has since
finished is not in the way: `sdlc stop` lifts it when the story is done, and `sdlc freeze`
replaces one that names a finished or dropped story rather than refusing over it.

If the freeze belongs to another story that is still under way, it is not this story's to lift,
and `sdlc unfreeze` refuses to. Finish that story — `sdlc stop`, then `sdlc start` with its id —
or mark it `dropped` in the backlog, and freeze again.

### SDLC-E0023

There is no freeze.

The test gate cannot pass until `sdlc freeze` has recorded what every acceptance test contains,
because without it a later edit to a test leaves no trace. If the message says the freeze belongs
to another story, that story is still in progress — finish or stop it, rather than unfreezing
work that is under way.

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

Restore them, or — if a test really did encode the wrong behaviour — have a person run
`sdlc unfreeze --reason "..."` in their own terminal and freeze again, so that the change is a
decision somebody can review rather than something that happened quietly.

`sdlc freeze` refuses with this code too, when the story's tests were frozen and
`.sdlc/state/tests.lock` is gone without the freeze ever being lifted. Freezing again would
lock the tests as they are now, whatever happened to them since. If the file was removed on
purpose, lift the freeze on the record with `sdlc unfreeze` first.

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

A blocking reviewer blocked — or perf did, on a performance budget the contract states.

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

### SDLC-E0034

A flag was given a value it cannot use, or the command line did not parse at all.

`sdlc cost add --usd` is the usual one: an amount that arrived from a shell substitution which
produced nothing is refused rather than recorded as zero, because a story that silently cost
nothing is the one wrong answer nobody questions. Check the command that produced the value.

An unknown command, an unknown flag, or the wrong number of arguments carries this code too, and
`sdlc <command> --help` says what the command takes.

### SDLC-E0035

`sdlc approve` was run for a story that is not waiting for anybody.

A story waits for a person only after the loop hands it over with `sdlc escalate`, and a
decision answers that. There is nothing to approve before it: an approval that answered no
question is one nobody asked for. `sdlc status` lists the stories that are waiting.

### SDLC-E0036

The story was handed to a person with `sdlc escalate`, and nobody has decided yet.

Starting it again would carry on past the question it stopped to ask. The person reads what
the escalation says — `sdlc status` shows it — and answers in their own terminal:

```console
$ sdlc approve US-001
$ sdlc approve US-001 --reject "the migration has no way back"
```

Either answer lets the story start again, and both go on its record.

### SDLC-E0037

The story's risk tier is one the project holds for a person before it is
committed, and nobody has approved the work being committed.

`human_gates.pre_commit_pause_tiers` in `.sdlc/config.json` names those tiers —
`high`, unless it says otherwise — and a story with no `risk_tier` is `low`. The
loop hands the story over and stops:

```console
$ sdlc escalate pre_commit_approval --message "ready to commit: what changed, and why"
```

A person reads the work and runs `sdlc approve` in their own terminal. An
approval is bound to the work as it stands, so a change made after it needs
approving again, and work that was sent back stays sent back until it changes.
The same refusal comes from the hook, before `git commit`, as
[`commit-gate`](enforcement.md#commit-gate).

### SDLC-E0038

A new story starts from trunk, and HEAD is somewhere else: on another branch, or
detached.

The loop commits every story straight to the branch `git.trunk_branch` names —
`main`, unless `.sdlc/config.json` says otherwise — so a story begun anywhere
else is not where its commit is recorded as landing. Switch back, and start
again:

```console
$ git switch main
$ sdlc start
```

Only new work is held to this. A story already under way is picked up again as
it is.

### SDLC-E0039

There is uncommitted work that belongs to no story, and a new story would take
it with it.

Gate 8 commits a story with everything in the working tree, so work left lying
around is reviewed and committed as part of whatever starts next. The refusal
names what it found. Commit it, stash it, or discard it:

```console
$ git stash --include-untracked
$ sdlc start
```

The loop's own files do not count: everything under `.sdlc/`, and the backlog,
whose statuses the loop writes. Straight after `sdlc init`, what is left is
usually the contract section it added to `CLAUDE.md` — commit it.

### SDLC-E0040

`git.remote` is on, and trunk is behind `origin`.

A story started on an old trunk is planned, tested and reviewed against code
that has already changed, and meets the difference only when it is committed.
Bring trunk up to date first:

```console
$ git pull --rebase
$ sdlc start
```

When `origin` cannot be asked at all, `sdlc start` says so and goes ahead:
working offline is not the same as being behind.

### SDLC-E0041

Trunk fails `commands.smoke` before the story has changed anything.

A story started on a broken trunk cannot tell its own failures from the ones
that were already there. The refusal carries the end of what the check printed.
Fix trunk, or revert the commit that broke it, and start again.

If the check itself is what is wrong — it fails on a trunk that works — change
`commands.smoke` in `.sdlc/config.json`. Leaving it out turns the check off.

### SDLC-E0042

The story's change is bigger than `thresholds.diff_size_cap`.

The cap is the most lines one story may add and remove, counted the way
`git diff --numstat` counts them against the last commit: tests included, a
moved file only for what changed in it, and `.sdlc/` and the backlog not at all.
`implementation`, `verification` and `code_review` all refuse a pass over it. A
change bigger than the project trusts one review to read is not made smaller by
reviewing it anyway, and it is not made better by being squeezed under the cap.

Record the gate as failed, and hand the story to a person to split:

```console
$ sdlc gate implementation fail --note "740 lines against a cap of 500"
$ sdlc escalate story_too_large --message "740 lines against a cap of 500: how should it be split?"
```

If the cap is wrong for this project, a person changes it in `.sdlc/config.json`.
`0` turns it off.

### SDLC-E0043

There are test files the freeze does not hold.

The named files match `paths.tests`, and they were not there when the tests were frozen. A test
that arrives after the freeze was not written before the code, and nothing says it was not
written to pass, so every gate the freeze guards — `tests_frozen`, `plan`, `implementation`,
`verification` and `commit` — refuses while one is in the tree.

If the project has `freeze.allow_new_test_files` on, run `sdlc freeze` again: it adds them to the
freeze, and from then on they are held like the rest. Otherwise remove them, or — if the story
really needs them — have a person run `sdlc unfreeze --reason "..."` in their own terminal and
freeze again.

### SDLC-E0044

Definition of ready cannot pass for a story with no acceptance criteria.

Gate 3 writes and freezes the tests from the story's `acceptance_criteria`, and every gate after
it checks the work against them. With none there is nothing to test, so the gate refuses a pass.
A criterion whose `text` is empty does not count.

Write the criteria in the backlog, each an observable behaviour, and pass the gate again. If it
is not clear what they should be, record `sdlc gate dor fail` and hand the question to a person
with `sdlc escalate spec_unclear --message "..."`.

### SDLC-E0045

`.sdlc/config.json` reads, and a setting in it is one `sdlc` cannot use. The message names every
one, so a single edit fixes them all:

- **`version` is higher than this `sdlc` reads.** A newer release wrote the file. Upgrade `sdlc`
  rather than lowering the number: this release would drop whatever it does not know, and a
  setting it drops is one that is not enforced.
- **`backlog.path` leaves the repository**, as an absolute path or one that climbs out with
  `..`. The backlog is committed with the work and protected while a story runs, and neither
  holds outside the repository. Give the path relative to the repository root.
- **A limit, threshold or budget is negative.** `0` is how each of them is turned off.

Change the settings and run `sdlc doctor`.

## Warnings the hook prints

These are not error codes. They arrive in the session as a system message, and
the tool call goes through: the hook fails open, always, because a bug in it
must not be able to stop your session. What it will not do is fail open
quietly.

Each one means a rule you are relying on is not running as it should.

### `the sdlc binary was not found, so nothing is being enforced`

The plugin is installed and the binary it hands every tool call to is not on
`PATH` or in the plugin's own `bin` directory. Nothing is refused until it is.
Install the binary, or run `/sdlc:next`, which offers to.

### `.sdlc/state/active could not be read, so nothing is being enforced`

The file naming the story under way is there but unreadable — a permission, a
directory where the file should be. Every rule is off until it reads.
`sdlc doctor` names it under "loop state"; `sdlc stop` removes it and ends the
iteration.

### `.sdlc/state/active does not name a story, so nothing is being enforced`

The file is empty, or names something that cannot be a story id — a path, an id
with `..` in it, a merge conflict left in place — so there is no story to hold
the session to. `sdlc start` never writes such a file, and `sdlc stop` removes
the file rather than empty it, so it was written some other way. Every rule is
off until it names a story. `sdlc doctor` names it under "loop state";
`sdlc stop` removes it and ends the iteration.

### `.sdlc/config.json could not be read, so tests are being recognised by the default patterns`

The configuration will not parse, so the loop cannot read your `paths.tests`.
Until it can, a test is whatever the default patterns call one — `tests/`,
`test/`, `*_test.go` and the rest listed under
[`paths.tests`](configuration.md#pathstests) — plus every file the freeze holds. Protected
paths, write scopes and the freeze all still hold, measured that way; a test
kept somewhere only your own patterns name is not recognised as one.
`sdlc doctor` names it under "configuration".

### `.sdlc/state/tests.lock could not be read, so every test file is being treated as frozen`

The freeze itself will not parse. Until it does, a file write or a shell command
that writes to anything that looks like a test is refused, frozen or not: a
freeze that cannot be read is not treated as no freeze. `sdlc doctor` names it
under "loop state".

Restore it if you keep a copy. Otherwise lift it with
`sdlc unfreeze --reason "..."` in your own terminal and run `sdlc freeze` again.
Deleting the file is not enough: `sdlc freeze` refuses a freeze that went
missing without being lifted.

### `.sdlc/state/tests.lock ... and nor could .sdlc/config.json, so every file the default patterns call a test is being treated as frozen`

The freeze will not parse, and neither will the configuration that says which
files are tests. Until one of them reads, a shell command that writes to
anything the default patterns call a test is refused. `sdlc doctor` names both:
the configuration, and the freeze under "loop state".
