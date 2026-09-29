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

If the message names `GIT_CEILING_DIRECTORIES`, you may well be inside a repository: that
variable tells git, and `sdlc`, not to look in the directory it names, and one of those is
above you. Take that directory out of it, or unset it. `git init` would start a second
repository inside the first. The hook stops at the same place, so no rule applies there either.

### SDLC-E0002

This repository has no `.sdlc/config.json`, so it does not take part in the loop yet.

If you want the loop in this repository, run `sdlc init` in its root yourself. It writes the
configuration, a starter backlog, and the contract section your project's `CLAUDE.md` needs.

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
It is refused, too, when `.sdlc/` or a directory in it is a link to somewhere
outside the repository, as a clone can bring one: the loop keeps its state inside
the repository. Replace the link with a directory of the same name.

It is also the code for **another sdlc command is still running**. Every command that writes
loop state takes a lock first, and waits up to ten seconds for a command that holds it. The
message names that command and its process id. A command stopped with Ctrl+C gives the lock
back on its way out. If nothing is running, the lock was left by a command that could not: one
killed outright, or a machine that went down. It is broken open once it has gone two minutes
without its holder refreshing it and that process is no longer running, or delete the
directory the message names. A holder that is still running, however long ago it took the
lock, is waited for: a laptop asleep in the middle of a command still has a change to write.

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

A person runs `sdlc start`, or types `/sdlc:next` in Claude Code, to begin one on the next
runnable story. `sdlc cost` and `sdlc cost add` also
take `--story`, which is how a runner records what a session cost after its iteration ended.

### SDLC-E0012

An iteration is already active on another story. A checkout works one story at a time, and so
does a Claude Code session: the gates measure the checkout's working tree, and the hook holds a
session to the one story it started.

Finish the current story, or run `sdlc stop` to end the iteration without recording a result. To
work a second story at the same time, start it from the main checkout in a second Claude Code
session: with [`git.worktrees`](configuration.md#git) on, the default, it gets a worktree of its
own. In a story's worktree that is the whole of the fix, since `sdlc stop` there would end the
story the worktree was made for.

`sdlc init --force` is refused with this code too. The story under way is held to the settings it
started under, so the defaults go back once it has ended.

`sdlc release` is refused with this code too, while an iteration is running on the story in any
checkout: this time it is the same story, not another. Run `sdlc stop` in each checkout the
message names, then release it.

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

When the message says `.sdlc/state/active does not name a story`, no story is at fault: the file
naming the story under way holds something `sdlc start` never writes. `sdlc stop` removes it and
ends the iteration; `sdlc start` then begins again.

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

The gate records that this story reached trunk; in a story's worktree, that it is committed on
its branch, `story/<ID>`, and the refusal says so. Commit the change, or stash what does not
belong to this story by path, `git stash push -- <path>`, and then record the gate. A stash of
the whole work tree is refused while a story is being worked on, because it takes the loop's own
record with it.

### SDLC-E0033

The story you asked to start has passed every gate. Starting it would put work that is already
done back in progress, with nothing on the record saying why.

If the iteration is still open, `sdlc stop` ends it. If the work genuinely has to come back —
a review reopened, a bug found after the commit — record the gate that failed:

```console
$ sdlc gate code_review fail --story US-001 --note "AC-2 turned out to be untested"
```

`--story` names it because no iteration is running. That puts the story back to `in_progress`
and says on the record why it came back. Being done is a reading of the gate record, not a door
that locks behind you.

### SDLC-E0034

A flag was given a value it cannot use, or the command line did not parse at all.

`sdlc cost add --usd` is the usual one: an amount that arrived from a shell substitution which
produced nothing is refused rather than recorded as zero, because a story that silently cost
nothing is the one wrong answer nobody questions. Check the command that produced the value.

`sdlc log --since` carries it for something git cannot read as a commit, and so does
`--since` given together with `--all`. `sdlc ack` carries it when `--through` is missing, names
something git cannot read as a commit, or names one that is not on this branch.

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

This check is made only with [`git.worktrees`](configuration.md#git) set to
false. With it on, the default, a new story begins in a worktree of its own, cut
from trunk, and the branch this checkout is on does not matter.

With it off, the loop commits every story straight to the branch
`git.trunk_branch` names in `.sdlc/config.json` — the trunk `sdlc init` found —
so a story begun anywhere else is not where its commit is recorded as landing.
Switch back, and start again; if the name is not your trunk's, correct it in the
file first:

```console
$ git switch main
$ sdlc start
```

Only new work is held to this. A story already under way is picked up again as
it is.

In a linked worktree made by hand with `git worktree add`, switching to trunk
fails, because git keeps a branch checked out in one checkout at a time and
trunk is checked out in the main checkout. With `git.worktrees` false, a new
story begins in the checkout that has trunk checked out; a linked worktree works
the stories it already holds, and picks up a story under way that a person
released to it. Turn `git.worktrees` back on, or leave it out, and `sdlc start`
gives each new story a worktree of its own. See
[more than one checkout](the-loop.md#more-than-one-checkout).

### SDLC-E0039

There is uncommitted work that belongs to no story, and a new story would take
it with it.

This check is made only with [`git.worktrees`](configuration.md#git) set to
false. With it on, the default, a new story begins in a worktree cut from
trunk's commits, so what is uncommitted in this checkout stays here and is not
part of the story.

With it off, Gate 8 commits a story with everything in the working tree, so work
left lying around is reviewed and committed as part of whatever starts next. The
refusal names what it found. Commit it, stash it, or discard it:

```console
$ git stash --include-untracked
$ sdlc start
```

The loop's own files do not count: everything under `.sdlc/`, and the backlog,
whose statuses the loop writes. Straight after `sdlc init`, what is left is
usually the contract section it added to `CLAUDE.md` — commit it.

If `sdlc start` first said that it passed over a story whose checkout no longer
exists, the work it found may be that story's own: this checkout, moved while
its story was stopped. Do not discard it. A person releases the story here and
starts it again, as [SDLC-E0051](#sdlc-e0051) says; otherwise commit it or stash
it.

Clearing untracked files with `git clean`, leave out `-x`. The story worktrees
under `.sdlc/worktrees/` are ignored, and `git clean -x` with `-f` given twice
removes every one of them, with the work nobody committed there.

### SDLC-E0040

`git.remote` is on, and trunk is behind `origin`.

A story started on an old trunk is planned, tested and reviewed against code
that has already changed, and meets the difference only when it is committed.
The check counts what `origin` has that the local branch `git.trunk_branch`
names does not, whichever branch this checkout is on. Bring trunk up to date
first:

```console
$ git pull --rebase
$ sdlc start
```

That is with trunk checked out. With [`git.worktrees`](configuration.md#git) on,
this checkout may be on another branch, and `git pull` brings up to date only
the branch it runs on. Run `git pull --ff-only` in a checkout that has trunk
checked out; where none has, `git fetch origin <trunk>:<trunk>`, with the
branch `git.trunk_branch` names, brings trunk up to date without checking it
out.

When `origin` cannot be asked at all, `sdlc start` says so and goes ahead:
working offline is not the same as being behind.

### SDLC-E0041

Trunk fails `commands.smoke` before the story has changed anything.

A story started on a broken trunk cannot tell its own failures from the ones
that were already there. The refusal carries the end of what the check printed.
Fix trunk, or revert the commit that broke it, and start again.

If the check itself is what is wrong — it fails on a trunk that works — change
`commands.smoke` in `.sdlc/config.json`. Leaving it out turns the check off.

With [`git.worktrees`](configuration.md#git) on, the default, the check runs in
the story's new worktree, `.sdlc/worktrees/<ID>`, and a failure removes the
worktree and its branch again; where it cannot, the message says what is left,
and [SDLC-E0053](#sdlc-e0053) says how to take it away. A new worktree has only
what trunk commits: no installed dependencies, such as `node_modules` or a
virtual environment, and no generated files. A check that passes in your own
checkout can fail there for that alone. Make `commands.smoke` install what it
needs first, for example
`"smoke": "npm ci && npm run build --silent"`, and commit that to trunk: the new
worktree reads trunk's committed `.sdlc/config.json`, not the copy in your
checkout, so an edit nobody committed does not reach it. Or set `git.worktrees`
to false in this checkout's `.sdlc/config.json`, which `sdlc start` reads where
it runs, and the check runs here as it did before.

Tools that look upward from where they run find the main checkout's copies from
inside `.sdlc/worktrees/<ID>`. Node resolves a package the worktree does not
have from `node_modules` in the main checkout, so a Node check can pass on the
packages you installed rather than on trunk's lockfile, and every later gate
then runs on them too. A smoke command that installs what it needs gives the
worktree its own.

`git worktree add` runs the repository's `post-checkout` hook, if it has one,
before the check. Removing the worktree cannot undo what that hook did.

### SDLC-E0042

**Retired.** The loop no longer limits how large a change may be, so nothing raises this
code. It refused a change over the loop's size limit, and the limit went because it only ever
cost work and the loop needs to move fast. The number is not given to any other.

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
- **`version` is `0` or less.** No release wrote that. Set it to `1`, or leave it out and `1` is
  what it reads.
- **`backlog.path` leaves the repository**, as an absolute path or one that climbs out with
  `..`. The backlog is committed with the work and protected while a story runs, and neither
  holds outside the repository. Give the path relative to the repository root.
- **A limit, threshold or budget is negative.** `0` is how each of them is turned off.

Change the settings and run `sdlc doctor`.

### SDLC-E0046

HEAD has moved since the story started.

The story's work is committed at the commit gate, so a commit before it reached trunk with no
gate passed. The gates between also measure the change against HEAD: work already committed is
not in the diff the reviewers read. The hook refuses the ways to commit it can read, and this
catches the rest at the next gate before the commit.

Record the gate as failed and hand the story to a person:

```console
$ sdlc gate implementation fail --note "HEAD moved: work was committed before the review"
$ sdlc escalate trunk_moved --message "a commit reached trunk before the review: keep it or revert it?"
```

The person keeps the commits, such as their own fix or a pull from the remote, or reverts them,
and answers in their own terminal. `sdlc start` then picks the story up from where trunk is:

```console
$ sdlc approve US-001
$ sdlc start US-001
```

If nobody escalated, because the person moved trunk themselves, `sdlc stop` and then `sdlc start`
in their own terminal do the same. A story's own commit, made once its code review has passed,
is not a move: a gate reopened after it measures from that commit.

### SDLC-E0047

`.sdlc/state/acknowledged` names a commit git does not have.

`sdlc log` starts after the last commit a person acknowledged, and the file that holds it names
something git cannot read as a commit: history was rewritten, this clone does not have the
commit, or the file was edited. The log refuses rather than list every story again, which would
bury the new ones under everything already read.

Run `sdlc ack --through <commit>` in your own terminal to acknowledge a commit git has, or remove
the file to list every story again. Until then, `sdlc log --since` starts after a commit you name,
and `sdlc log --all` lists every committed story.

### SDLC-E0048

An `sdlc` command that changes a story ran in a Claude Code session other than the one working it.

`sdlc start` records the Claude Code session it runs in, in `.sdlc/state/session`. This command ran
with `CLAUDE_CODE_SESSION_ID` naming another session, so what it wrote would have gone into a story
that session is not working. Nothing was changed. The refusal keeps one session's `sdlc` commands
out of a story another session is working; it is not a barrier against an assistant set on getting
round it.

If you are the person running the session, type `/sdlc:next` in it to take the story over. It picks
the story up and records this session as the one working it, and from then on the session it came
from is refused instead. To answer for the story yourself, with `sdlc approve`, `sdlc unfreeze` or
`sdlc stop`, run the command in your own terminal, outside Claude Code.

If you are an assistant, stop and tell the person that the story is being worked on in another
Claude Code session and that only they can move it here. Do not run `sdlc start`.

### SDLC-E0049

The commit gate's note names a commit its range does not hold.

The note on `sdlc gate commit pass` and the range the gate records both say which commits landed,
and one record cannot say two things. Any run of seven to forty hexadecimal characters in the
note that git resolves to a commit is read as naming that commit, and every one of them has to be
inside the range this pass records. A run git has never heard of is prose and is ignored, and so
is anything shorter. The message names the sha and the range, or, where the record knows no
start, the one commit it names. Nothing was recorded, and the gate does not stand passed.

Re-run the gate with the sha corrected or dropped: `sdlc gate commit pass --note "..."`. For
example:

```console
$ sdlc gate commit pass --note "on trunk"
```

The note needs no hash at all: the range is what the record keeps. The check runs only when the
gate records a range, so it is a consistency check between the note and the record, not evidence
of which commits are the story's.

### SDLC-E0050

A command named a story that another checkout of this repository holds.

Every linked worktree of a repository shares the main checkout's backlog and story records, and a
story is held by one checkout at a time: the one that started it. The message names that checkout.
`sdlc start` does not pick up a story another checkout holds, and `sdlc gate`, `sdlc artifact
write`, `sdlc review add`, `sdlc cost add`, `sdlc approve`, `sdlc escalate`, `sdlc freeze` and
`sdlc unfreeze` refuse it, because what each records is measured on the holding checkout's working
tree. `sdlc start` also refuses a Claude Code session that already holds a story in another
checkout: one session works one story. Open a new Claude Code session in this checkout to work
another story here, or go back to the checkout the message names to carry on with that one.
Nothing was changed.

A story held in its own story worktree, `.sdlc/worktrees/<ID>`, is the one exception for
`sdlc start`: named in any other checkout, the main checkout included, it is picked up there, in
its worktree, and a session already working it carries on. A worktree made by hand is not a story
worktree, and naming its story is refused as above.

The same code covers two cases that start from this checkout's own `.sdlc/state/active`:

- It names a story another checkout claims, usually because this worktree was made from a commit
  that carried `.sdlc/state/`. Run `sdlc stop` here to put this checkout's stale copy down; the
  story carries on where it is held.
- It names a story no checkout claims, and so does another checkout's. Neither holds it until a
  person decides. The message lists each of them with when its `active` was written and what its
  `.sdlc/state/session` names. Keep the one whose session you recognise as the one you are working
  in, and run `sdlc stop` in each of the others. The details are a courtesy, not a proof: anyone
  who can write those files can make two of them look alike.

If you are the person running the session, run the command in the checkout the message names, or
move the story here: run `sdlc release <ID> --reason "..."` in your own terminal, outside Claude
Code, in any checkout, then `sdlc start <ID>` in this one. Releasing leaves behind anything the
other checkout has not committed.

If you are an assistant, stop and tell the person which checkout holds the story. Do not run
`sdlc start` or `sdlc release`.

### SDLC-E0051

The checkout that holds the story no longer exists.

A claim on a story lasts until the story is `done` or `dropped`, or until a person releases it:
`sdlc stop`, `sdlc escalate` and the Stop hook's hand-over all leave it in place, because the
story's uncommitted work is in that checkout. This one names a directory that is gone, or is no
longer a checkout of this repository. No checkout can start the story or record anything for it
until the claim is released. Nothing was changed.

A repository with one checkout, moved or copied with `mv` or `cp` while its story is running, does
not meet this: where git has no linked worktree entry and this checkout's `.sdlc/state/active`
names the story, a claim naming the old path is read as this checkout's, and the next `sdlc`
command that writes rewrites it.

Moved or copied while the story is stopped or waiting for a person, it is held by the old path,
because the claim alone cannot tell a moved checkout from a worktree that was removed and pruned.
Then:

- `sdlc start` with no story named passes the story over, as it passes over any story another
  checkout holds, and first says so on standard error, with the commands below. It then begins the
  next story, or refuses over the uncommitted work, which is the stopped story's own: do not
  discard it.
- `sdlc start <ID>` and `sdlc approve <ID>` are refused with this code.

If that checkout is gone for good, or this is the same checkout moved, a person runs
`sdlc release <ID> --reason "..."` in their own terminal, after which `sdlc start <ID>` works in
any checkout; until then no checkout can record anything for the story. For example:

```console
$ sdlc release US-001 --reason "the worktree was deleted with its disk"
```

Then they run the refused command again: `sdlc approve US-001` for a story waiting for a person,
`sdlc start US-001` otherwise, in any checkout. If the story's tests were frozen and the checkout
that holds the story's work is a new one, the freeze is on the story's record and not in that
checkout's `.sdlc/state/tests.lock`: until a person runs `sdlc unfreeze --reason "..."` and then
`sdlc freeze` there, every test file is treated as frozen. A checkout that was moved took its
`tests.lock` with it, and its freeze stands.

If you are an assistant, stop and tell the person. Do not run `sdlc release`.

### SDLC-E0052

This checkout's repository has no main checkout in which to keep the backlog.

The backlog, the story records and the claims on stories are kept in the repository's main
checkout, the directory whose `.git` is the repository's own git directory. This checkout is a
linked worktree of a repository that has none, or whose main checkout cannot be found from git's
own files:

- a bare repository, where `core.bare` is true;
- a repository made with `git init --separate-git-dir`, whose git directory is not a checkout's
  `.git`;
- a submodule's worktree, whose git directory sits inside another repository's.

The message says which check failed. Every command that reads or writes the backlog is refused,
`sdlc init` included. `sdlc stop` still works, to end an iteration: it clears this checkout's own
state and writes nothing to any record. `sdlc doctor` reports the problem.

Work in a clone made without `--bare`, or in the main checkout of a repository made with
`--separate-git-dir`.

### SDLC-E0053

`sdlc start` could not make the new story's worktree, and nothing was started.

With [`git.worktrees`](configuration.md#git) on, the default, a new story begins in a worktree
of its own, `.sdlc/worktrees/<ID>` in the main checkout, on a new branch `story/<ID>` cut from
the local trunk branch. The message says what was in the way. A refused start removes what it
made, the worktree and the branch, and changes nothing in the backlog, the story's record or the
claims.

Each cause has its own fix, and the message gives it. Setting `git.worktrees` to false in
`.sdlc/config.json` gets past any of them: the story then starts in the checkout you run
`sdlc start` in, as it did before.

- **Trunk has no committed `.sdlc/config.json`.** This is the usual first run after `sdlc init`.
  The worktree holds only what trunk commits, and reads its settings from there. Commit
  `.sdlc/config.json` and `CLAUDE.md` to trunk, then start again. `sdlc doctor` says so too,
  under `story worktrees`.
- **The `.sdlc/config.json` trunk commits cannot be read.** Correct it and commit it to trunk,
  then start again.
- **Trunk is not a branch with a commit here.** In the main checkout, create the branch
  `git.trunk_branch` names, with a commit on it, or set `git.trunk_branch` to the branch that is
  trunk. Where `git.trunk_branch` is empty or starts with `-`, set it to the name of the trunk
  branch.
- **Git will not take `story/<ID>` as a branch name**, for an id such as `A..B` or one ending in
  `.lock`; `git check-ref-format --branch story/<ID>` says whether it does. Rename the story, or
  set `git.worktrees` to false.
- **`.sdlc/worktrees`, or the story's directory in it, is a file or a link.** sdlc never writes
  through a link. Move the file or the link named in the message out of the way.
- **`.sdlc/worktrees/.gitignore` does not hide the worktrees.** It has no line `*`, or has a
  line starting with `!` that shows something again, and without it `git status` and
  `git add -A` in the main checkout take in every story worktree. sdlc writes that file only
  where there is none, and never rewrites one already there. Put a line `*` in it and nothing
  that starts with `!`, or remove the file, then start again.
- **The story's directory already has files in it.** Start never writes over or removes what it
  did not make: move them somewhere else, then start again.
- **The backlog kept changing while start was making the worktree**, because other starts ran,
  or the backlog was edited, at the same moment. Run `sdlc start` again.
- **The story's branch or worktree is already there**, below.

Where a refused start could not take away all it made, the message says what is left. On
Windows that is most often a file something still has open in the worktree. First close whatever
has a file open in `.sdlc/worktrees/<ID>`, then, in the main checkout, run
`git worktree remove .sdlc/worktrees/<ID>` and `git branch -d story/<ID>`, and then follow the
fix for the cause.

A start that was interrupted leaves a branch or a worktree behind, and so does a story that
finished on its branch, until a person removes it. A start that failed once it had named the
story in its new worktree says so under the code of what failed, and leaves both: put that
right, and then `sdlc start <ID>` picks the story up there, or, where the story was not yet in
progress, `sdlc start <ID>` finds them in the way and says so with this code.

Before removing anything, check that no `sdlc start` is still running for the story; if one is,
let it finish and run `sdlc start` again. Then, in the main checkout, with trunk checked out
(`main` here):

```console
$ git log --oneline main..story/US-001
```

Commits it lists are the story's work. Bring them to trunk first, or keep them on a branch of
another name with `git branch -m story/US-001 <new name>`:

```console
$ git merge --ff-only story/US-001
```

If git says it cannot fast-forward, trunk has moved since the story was cut: in the story's
worktree, run `git rebase main` and the project's tests, then merge again.

The worktree may be on no branch: a rebase stopped part-way leaves it so, and so does a HEAD
somebody detached. A commit on no branch is lost when its worktree is removed, and
`git worktree remove` does not ask for `--force` first. The message then says the worktree is
on no branch and how many commits its HEAD has that trunk does not, and gives no command that
removes the worktree or the branch. Look at them first:

```console
$ git -C .sdlc/worktrees/US-001 log --oneline main..HEAD
```

Where a rebase stopped part-way, finish it or abort it in the worktree. Otherwise keep what is
wanted on a branch, and bring that to trunk as above:

```console
$ git -C .sdlc/worktrees/US-001 branch <new name>
```

With nothing listed by either, or once it is on trunk, remove both:

```console
$ git worktree remove .sdlc/worktrees/US-001
$ git branch -d story/US-001
```

`git worktree remove` refuses a worktree with changes nobody committed, and suggests `--force`.
Look at them first, with `git -C .sdlc/worktrees/US-001 status`: anything outside `.sdlc/state`
may be the story's work. Only the loop's own files under `.sdlc/state` are safe to remove with
`--force`. `git branch -d` compares the branch with the one the main checkout has checked out,
which is why that has to be trunk; where it says the branch is not fully merged, its commits are
the story's work. Never use `git branch -D`, which deletes them. Where the worktree's directory
was deleted by hand, `git worktree prune` clears git's record of it.

Do not tidy up with `git clean`. `.sdlc/worktrees/` ignores itself, so that `git status` in the
main checkout does not list it, and `git clean -x` with `-f` given twice removes every story
worktree in it, with the work nobody committed there.

`git worktree add` runs the repository's `post-checkout` hook, if it has one. A refused start
removes the worktree and the branch it made, and cannot undo what that hook did.

If you are an assistant, stop and tell the person what the message says. Every fix here is
theirs.

## Warnings the hook prints

These are not error codes. They arrive in the session working a story as a
system message, and the tool call goes through: the hook fails open, always,
because a bug in it must not be able to stop your session. What it will not do
is fail open quietly in that session — with one exception it cannot warn you
about, because by then it is no longer running: a hook that outlasts the
timeout Claude Code gives it is killed, and a killed hook says nothing. With
`SDLC_DEBUG_FILE` set it leaves a `hook considering` line with no
`hook decision` after it, and `SDLC_TRACE=1` says how long each call took; a
frozen test that was changed is still refused at the next `sdlc gate`, with
`SDLC-E0025`. Every other session hears nothing from the plugin at all.

Each one means a rule you are relying on is not running as it should.

### `the sdlc binary was not found, so nothing is being enforced`

The plugin is installed and the binary it hands every tool call to was not
found: not at `SDLC_BIN`, in the plugin's own `bin` directory, on `PATH`, or
where the installers put it (`~/.local/bin`, or `%LOCALAPPDATA%\Programs\sdlc\bin`
on Windows). Nothing is refused until it is. Install the binary, or run
`/sdlc:next`, which offers to. Only the session working a story shows this: the
one `sdlc start` recorded in `.sdlc/state/session`, in a project its own
directory is in. Every other session says nothing, including one started in a
directory above that project.

A desktop app such as Claude Desktop keeps the `PATH` it was started with, so a
binary a new terminal finds, and `sdlc doctor` passes for, can still be missing
here. Installed to the default directory, it is found without `PATH`. Installed
anywhere else, quit the app fully (from the tray or the menu bar too) and reopen
it after changing `PATH`, or set `SDLC_BIN` to the full path of the binary.

On Windows, `npx @bbsnly/sdlc install` does not change `PATH`; it prints the
lines that do. The hook does not need them for the default directory. They are
for a terminal, so that `sdlc` runs by name there. Run them in PowerShell, with
your install directory in the second:

```powershell
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$key.SetValue('Path', "$env:LOCALAPPDATA\Programs\sdlc\bin;" + $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames'), 'ExpandString')
[Environment]::SetEnvironmentVariable('SDLC_PATH_REFRESH', $null, 'User')
```

### `.sdlc/state/active could not be read, so nothing is being enforced`

The file naming the story under way is there but unreadable — a permission, a
directory where the file should be. Every rule is off until it reads. Only the
session working the story is told. `sdlc doctor` names it under "loop state";
`sdlc stop` removes it and ends the iteration.

### `.sdlc/state/active does not name a story, so nothing is being enforced`

The file is empty, or names something that cannot be a story id — a path, an id
with `..` in it, a merge conflict left in place — so there is no story to hold
the session to. `sdlc start` never writes such a file, and `sdlc stop` removes
the file rather than empty it, so it was written some other way. Every rule is
off until it names a story. Only the session working the story is told.
`sdlc doctor` names it under "loop state"; `sdlc stop` removes it and ends the
iteration.

### Nothing is enforced, and nothing is said

When `.sdlc/state/session` is missing, cannot be read, or names something that
cannot be a session id, no session is working the story, so the hook holds none
to it and tells none. `sdlc status` and `sdlc doctor` say that no Claude Code
session is working the story. Run `/sdlc:next` in the session that should work
it, which records that session again.

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

### `.sdlc/state/tests.lock does not hold the freeze ... record says its tests are under`

The story's record says its tests were frozen and nothing has lifted that, but
`tests.lock` is gone, or names another story, or names none. Until it holds the
freeze again, a file write or a shell command that writes to anything that looks
like a test is refused, as it is for a freeze that will not read: taking the
freeze out of `tests.lock` does not take it off the tests. `sdlc doctor` names it
under "loop state".

Restore it if you keep a copy. Otherwise a person lifts the freeze on the record
with `sdlc unfreeze --reason "..."` in their own terminal and runs `sdlc freeze`
again.

### `.sdlc/state/tests.lock ... and nor could .sdlc/config.json, so every file the default patterns call a test is being treated as frozen`

The freeze will not parse, and neither will the configuration that says which
files are tests. Until one of them reads, a shell command that writes to
anything the default patterns call a test is refused. `sdlc doctor` names both:
the configuration, and the freeze under "loop state".
