# Commands

Every command, every flag. `sdlc <command> --help` says the same thing in your
terminal.

Two rules run through all of them:

- **`--json` works everywhere.** One line of JSON on standard output, for a
  script or a skill to read. Failures come back as JSON too, with the same
  fields as the prose.
- **Failures carry four things**: what happened, why, what to do about it, and a
  code you can look up in [troubleshooting](troubleshooting.md).

## `sdlc init`

Set this repository up to run the loop.

```console
$ sdlc init
$ sdlc init --force
```

| Flag | What it does |
| --- | --- |
| `--force` | restore the default settings over an existing setup, keeping your stories, where they are (`backlog.path`) and your trunk branch (`git.trunk_branch`); refused while a story is being worked on ([SDLC-E0012](troubleshooting.md#sdlc-e0012)) |

Reads the repository, detects the stack, and writes `.sdlc/config.json` with
commands that already match it, a backlog with one example story, the story
schema, and a `## SDLC Contract` section appended to `CLAUDE.md`. It never
overwrites a file you wrote, and it writes no `.gitignore`.

It recognises Go (`go.mod`), Rust (`Cargo.toml`), Node (`package.json`) and
Python (`pyproject.toml`, `setup.py` or `requirements.txt`), tried in that order
at the root of the repository, so a Go service with a `package.json` for its
front-end tooling is a Go project. Anything else gets no commands and the
common test directories, for you to fill in.

## `sdlc status`

Show what the loop is working on and where it has got to.

```console
$ sdlc status
$ sdlc status --json
```

Reports the active story, every gate's outcome, the state of the test freeze and
whether it is still intact, the backlog counts, and what `sdlc start` would pick
up next. It changes nothing, so it is safe to run at any point.

An active story with no gate left reads as `finished` rather than `in progress`,
because there is nothing left to work on it: what it wants is `sdlc stop`.

An active story that has gone from the backlog reads as `(not in the backlog)`,
with `"not_in_backlog": true` in `--json`. The commit gate refuses such a story,
so put it back, or run `sdlc stop` to end the iteration.

Three fields in `--json` are worth knowing by name, because a skill reads them to
decide what to do:

| Field | What it says |
| --- | --- |
| `next_gate` | the gate to work now — the first one that has not passed. Absent when every gate is behind you, or when no story is active |
| `next` | the story `sdlc start` would pick up. Only present when nothing is active, and absent while a story waits for a person |
| `waiting` | every story handed to a person with `sdlc escalate` and not yet answered, with the question it asks |

In a repository with linked worktrees, status also lists every story a checkout
holds, on a `held` line naming the checkout, and `held` in `--json` has one
entry per story, with its `checkout`, `here` for the one you ran it in, `gone`
for a checkout that is no longer there, and `contenders` when more than one
checkout names the story and none holds it. `next up` passes over what another
checkout holds. A story this checkout names as its own that another holds, or
that nobody holds, says so on its line, because `sdlc start` here will not carry
it on, and status then ends with what a person does about it instead of sending
them to carry on ([more than one checkout](the-loop.md#more-than-one-checkout)).

## `sdlc story list`

List the backlog, and say what is holding each story back.

```console
$ sdlc story list
$ sdlc story list --json
```

Marks the story `sdlc start` would choose and shows every story's status. A
story waiting on a `depends_on` story that is not done names it, and says when
that one is dropped or not in the backlog, because then it never will be.

In a repository with linked worktrees, a story a checkout holds ends with where
it is held, and `held_by` in `--json` names that checkout:

| Ends with | What it means |
| --- | --- |
| `(held in "<checkout>")` | another checkout holds it |
| `(held in "<checkout>", this checkout)` | the checkout you ran it in holds it |
| `(held in "<checkout>", gone: see sdlc release <ID> --reason "...")` | the checkout that holds it no longer exists ([SDLC-E0051](troubleshooting.md#sdlc-e0051)) |
| `(held by none: more than one checkout names it ("<checkout>", "<checkout>"))` | several checkouts name it and none holds it until a person runs `sdlc stop` in all but one |

The mark is what `sdlc start` would pick in the checkout you run it in, which is
never a story another checkout holds.

## `sdlc log`

List the stories committed to trunk, and what each one went through.

```console
$ sdlc log
$ sdlc log --since HEAD~20
$ sdlc log --all --json
```

| Flag | What it does |
| --- | --- |
| `--since` | list the stories committed after this commit — a hash, a tag, or a name like `HEAD~20` — instead of after the last one acknowledged |
| `--all` | list every committed story, whatever has been acknowledged. Not together with `--since` |

One entry per story whose commit gate has passed, oldest first: the commits it
landed in, how many rounds each gate's reviews took, every block, and what is
worth a second look — a freeze lifted with `sdlc unfreeze`, gates reopened, a
question handed to a person and whether it was answered, and what the story
cost. It changes nothing.

The log starts after the last commit a person acknowledged with
[`sdlc ack`](#sdlc-ack), the one `.sdlc/state/acknowledged` names: a story whose
commits end in that commit's history has been read. With nothing acknowledged,
it lists every story. A file that names no commit git has is refused —
[SDLC-E0047](troubleshooting.md#sdlc-e0047) — rather than read as nothing
acknowledged.

The range is the one the commit gate recorded; see
[`sdlc gate`](#sdlc-gate-gate-status). For a story that landed from its
worktree, it is the commits that reached trunk, as they are after any rebase,
and it prints the same in every checkout. The record's `landed` event says how
trunk moved, and, where some of the reviewed change was on trunk before the
landing, the commit it began at. A story committed by an `sdlc` that did
not record one is found by its commit messages instead: the newest commit in
HEAD's history whose message names the story's id as a whole word ends the
range, the commit before the oldest one starts it, and the entry says the range
was inferred. A later commit that names the id, such as `Revert US-001`, moves
that end, and can bring a story already acknowledged back into the log. When no
message names the story either, its range is `unknown`, and nothing is guessed.
A story whose commit gate recorded no range on purpose, because no single range
holds only the commits the loop watched it land, is `unknown` too, and says so:
`range      unknown: the loop watched this story's commits, but no single range
holds only them`, with `range_unknown` in `--json`. Its commit messages are
not read for it. A recorded range whose commit gate saw HEAD past its end
says so beneath it, in the gate's own words — `range      head is <head>, past
<end>: nothing that landed after <end> was watched` — with the full HEAD as
`head_past_commit` in `--json`. A story that cannot be placed — its range is
unknown, or ends in a commit that is not on this branch (`off_branch`) or not in
this repository (`missing`) — is always listed.

In `--json`, `range_source` is `record`, `message` or `unknown`, and
`since_source` says where the log started: `acknowledged`, `flag` for
`--since`, `all` for `--all`, or `none` when nothing is acknowledged. `since` is
the full commit it started after, and `spent_usd` is always there, `0` for a
story with no spend recorded.

An entry for a story that is not finished, in a repository with linked
worktrees, names the checkout that holds it on a `held` line, and as `held_by`
in `--json`.

A range is what the loop saw the story's session land, as far as the hook could
tell a command was a commit: a person's commit made in the same window is left
out of it, and a story whose commits somebody else's sits between has no range at
all. It is not proof, and it is not complete: a commit the hook did not watch is
missing from it, and a range recorded where the hook watched nothing is the
whole window, which can hold anybody's commits.
[Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md#ranges-to-read-with-care)
says what to read with care. A story reopened after it was committed spans
everything that landed in between. It is a list to read, not a list to revert.

To go through the log with the assistant, run `/sdlc:trunk-review`: see
[Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md).

## `sdlc ack`

Mark the log read up to a commit, so it starts after it.

```console
$ sdlc ack --through HEAD
$ sdlc ack --through 3f2a9c1e0b7d
```

| Flag | What it does |
| --- | --- |
| `--through` | the commit you have read the log up to: HEAD, or one in its history. Required |

Run it in your own terminal: the hook refuses it from a tool call, because an
agent that could run it would be deciding which stories nobody needs to look at.
It writes the full commit to `.sdlc/state/acknowledged`, and `sdlc log` starts
after it from then on. Nothing else changes.

There is no default. A commit git cannot read, or one that is not on this branch,
is refused with [SDLC-E0034](troubleshooting.md#sdlc-e0034). The output names the
commit acknowledged before, and says when the new one comes before it, because
the log then lists the stories in between again; in `--json`, `back` is `true`.
`through` is a full commit. `previous` is too, or the file's text when git does
not have that commit, and it is absent when nothing was acknowledged. Running
`sdlc ack` is how a file that names no commit
([SDLC-E0047](troubleshooting.md#sdlc-e0047)) is put right.

The file says how far the person using this clone has read, so whether it is
committed is your call.

## `sdlc start`

Begin an iteration on the next story, or on the one you name.

```console
$ sdlc start
$ sdlc start AUTH-3
```

Picks up a story already under way, or takes the next runnable one: resume
first, then priority, then id. Refuses if an iteration is already running on
another story; on the same story it resumes and changes nothing about the story.
With `--json` it reports `resume` and `next_gate`, the first gate that has not
passed, so a session picking a story back up carries on from there, and
`session`, the Claude Code session it recorded, empty when it ran in none.

Run in Claude Code, it records the session it runs in, in
`.sdlc/state/session`. That session and its agents are the ones the hook holds
to the story; every other session in the repository is left alone, except that
the `sdlc` commands that change the story refuse to run there
([SDLC-E0048](troubleshooting.md#sdlc-e0048)). Starting or resuming the story
in another session moves it there. Run outside Claude Code, it records no
session. A new story then has nothing enforced in any session until one picks
it up with `/sdlc:next`, and `sdlc status` says so meanwhile. A
story resumed that way keeps the session already recorded, since the command
cannot tell that the story has moved.

Naming a story chooses it over the priority order, and nothing else: a story
that is `dropped`, `blocked` or marked `done`, or that waits on a `depends_on`
story that is not done, is refused as it would be passed over —
[SDLC-E0010](troubleshooting.md#sdlc-e0010).

It also refuses a story whose gates have all passed, rather than putting
finished work back in progress. The way back into a finished story is to record
the gate that failed — see [SDLC-E0033](troubleshooting.md#sdlc-e0033).

With [`git.worktrees`](configuration.md#git) on, the default, a new story begins
in a worktree of its own. Start makes `.sdlc/worktrees/<ID>` in the main
checkout, on a new branch `story/<ID>` cut from the local trunk branch, runs
`commands.smoke` there, and records the story as held by that worktree, whose
`.sdlc/state` it writes rather than this checkout's. It says where, under the
story's title:

```text
Worktree: "/home/dev/project/.sdlc/worktrees/US-001"
Branch: story/US-001
```

and ends `Next: a person opens Claude Code in the worktree above and types
/sdlc:next there to work the story; /sdlc:next typed in a new session in this
checkout begins the next story instead.` A new session opened in the checkout
start ran in holds no story, so `/sdlc:next` there starts again, and passes
this story over as held. A Claude Code session that ran start itself holds the
story, and `/sdlc:next` there carries it on, in this checkout or in the
worktree.
Run in the story's worktree, start ends `Next: a person types /sdlc:next in
Claude Code, opened in this checkout or in the worktree above, to work the
story there.` With `--json` it adds `worktree`, the worktree's absolute path,
`branch`, and `main`, the main checkout's absolute path, where the backlog and
the story's record are kept.
Picking up a story its worktree holds prints the same path and branch, from the
main checkout or from the worktree, and makes nothing. A story the main
checkout holds, or a worktree made by hand, prints neither, and the output is
as it was with `git.worktrees` false.

In a repository with linked worktrees, the checkout that starts a story holds
it until it is finished or a person runs [`sdlc release`](#sdlc-release). With
no story named, start passes over what another checkout holds, and says on
standard error which story it passed over because the checkout holding it is
gone, as it is for a single checkout moved while its story was stopped: if this
checkout was moved from there, what it has not committed is that story's own.
Naming a story another checkout holds is refused with
[SDLC-E0050](troubleshooting.md#sdlc-e0050), or
[SDLC-E0051](troubleshooting.md#sdlc-e0051) when the checkout holding it is
gone, except a story held in its own worktree under `.sdlc/worktrees/`: named
in any other checkout, the main checkout included, it is picked up there, in
its worktree. Picked up in
another Claude Code session, the story is taken over, and the session that was
working it is refused the story's `sdlc` commands from then on
([SDLC-E0048](troubleshooting.md#sdlc-e0048)). A Claude Code session already
working a story in one checkout is refused another in the next
([SDLC-E0050](troubleshooting.md#sdlc-e0050)). See
[more than one checkout](the-loop.md#more-than-one-checkout).

Before it begins a new story — not one it is picking up again — it checks that
trunk is somewhere to start from. With `git.worktrees` on: trunk is not behind
`origin` when `git.remote` is on
([SDLC-E0040](troubleshooting.md#sdlc-e0040)), and `commands.smoke` passes in
the new worktree ([SDLC-E0041](troubleshooting.md#sdlc-e0041)); a worktree that
cannot be made is [SDLC-E0053](troubleshooting.md#sdlc-e0053), and a refused
start takes its worktree and branch away again, or says what it could not. With
it off: HEAD is on
`git.trunk_branch`, nothing is uncommitted outside `.sdlc/` and the backlog,
trunk is not behind `origin` when `git.remote` is on, and `commands.smoke`
passes. Each has its own refusal, from
[SDLC-E0038](troubleshooting.md#sdlc-e0038) to
[SDLC-E0041](troubleshooting.md#sdlc-e0041).

## `sdlc stop`

End the current iteration without recording a result.

```console
$ sdlc stop
```

Stopping does not undo anything. The gates already recorded stay recorded, the
story stays in progress, and starting again resumes it.

A story whose gates have all passed is a different case, and `stop` says so: it
is done, the test freeze it was holding is lifted, what the hook wrote down about
its commits is removed from the repository's git directory, and the next
`sdlc start` moves on to the next story instead of reopening it. An unfinished
story keeps its freeze and what was written down, so stopping and starting again
cannot be a way round either.

In a checkout whose `.sdlc/state/active` names a story another checkout holds,
or that more than one checkout names, stop puts this checkout's copy down and
writes nothing to the story's record, and its last line says where the story
carries on rather than that `sdlc start` picks it up here.

### Landing a story from its worktree

In a story's own worktree, `.sdlc/worktrees/<ID>`, with every gate passed, stop
**lands** the story instead: it moves trunk to the story's commits by
fast-forward, and the story is done
([the loop](the-loop.md#more-than-one-checkout) lists what it checks first). The
same holds with no iteration running, for a finished story the worktree holds,
such as one a person answered for after an escalation.

```console
$ sdlc stop
sdlc: running commands.test on 8a0e5d6c1f2b in .sdlc/worktrees/US-001
main moved from 3f1c2a9d4b7e to 8a0e5d6c1f2b by fast-forward; the files in "/home/dev/project", which has main checked out, moved with it.
The backlog and US-001's record are changed in the main checkout and not committed.
Ended the iteration on US-001. Every gate passed, so the story is done and `sdlc start` moves on to the next one.
Removed .sdlc/worktrees/US-001, with the files git ignored in it, and story/US-001.
```

The `running` lines go to standard error, one for each configured command the
landing runs, and only where the story was rebased or its change is not the one
reviewed ([configuration](configuration.md#at-landing)). Where no checkout has
trunk checked out, the first line ends `no checkout has main checked out, so
only the branch moved.` instead. Where trunk already held the story, it says
`US-001 was already on main at 8a0e5d6c1f2b; recorded it as landed.` Where
some of the reviewed change was on trunk before, a line follows that names the
commit it began at and the range the record keeps as what landed. With no
iteration running, the third line starts `Every gate on US-001 passed` instead
of `Ended the iteration`. A worktree holding changes nobody committed, or one a
person locked, is kept, and the last line says so, names the files, and sends
you to `sdlc doctor`:

```text
Kept .sdlc/worktrees/US-001 and story/US-001: it holds changes nobody committed, "notes.txt". `sdlc doctor` in the main checkout says what to do with it.
```

With `--json` these lines go to standard error, and standard output has `ok`,
`story`, `was_active` and `done`, which is `true`.

Every refusal leaves trunk where it was, and ends with `Nothing landed: main is
unchanged, and the iteration on US-001 is still running`, or, with no iteration
running, `... and US-001 waits in .sdlc/worktrees/US-001`. Trunk has moved past
the story: [SDLC-E0054](troubleshooting.md#sdlc-e0054). The worktree, its
commits or a command are not fit to land:
[SDLC-E0055](troubleshooting.md#sdlc-e0055). A checkout with trunk checked out
has changes nobody committed that the fast-forward would write over:
[SDLC-E0056](troubleshooting.md#sdlc-e0056). The change is not the one the
reviewers saw: [SDLC-E0029](troubleshooting.md#sdlc-e0029). Another stop is
landing it already: [SDLC-E0006](troubleshooting.md#sdlc-e0006). The others each
have an "At landing" paragraph in [troubleshooting](troubleshooting.md).

Stop in a story's worktree always lands a finished story. To put one down
without landing it, hand it to a person with
[`sdlc escalate`](#sdlc-escalate).

## `sdlc gate GATE STATUS`

Record the outcome of one gate.

```console
$ sdlc gate dor pass --note "criteria are testable"
$ sdlc gate analysis pass --security-sensitive --note "token crosses a boundary"
$ sdlc gate code_review fail --note "AC-2 is untested"
```

| Flag | What it does |
| --- | --- |
| `--note` | why the gate came out this way, in one line |
| `--story` | record against this story instead of the one being worked on. The id has to be in the backlog; a mistyped one is refused rather than starting a record nothing reads |
| `--security-sensitive` | the story touches a trust boundary, so the security review blocks. Set it at the analysis gate; unset is assumed to mean yes |

Gates: `dor`, `analysis`, `tests_frozen`, `plan`, `design_review`,
`implementation`, `verification`, `verifier_review`, `code_review`, `commit`,
`retro`. Outcomes: `pass`, `fail`, `pending`.

`gate` records a decision; it does not make one. But it does refuse to record a
**pass** that is not true: see [the loop](the-loop.md) for what each gate
requires. A `fail` is always recordable — a gate can fail precisely because its
work could not be done, and the loop has to have somewhere to put that.

The first `code_review` pass records HEAD at that moment, where the story's
commits can start. Each `commit` pass records the range the story landed in and
says so: `landed in <start>..<end>`, with each hash cut to 12 characters, or
`commit_base` and `commit` in full with `--json`. In a story's own worktree it
says `committed on story/<ID> in <start>..<end>` instead, because trunk has none
of it until the story lands, and the pass that leaves no gate unpassed ends
`` Every gate on <ID> has passed. `sdlc stop` in .sdlc/worktrees/<ID> lands it on <trunk>. ``
When the story lands, the record's range is replaced by the commits that reached
trunk.

The range is worked out from what the hook watched. Each time it lets the
session's command through and can tell the command commits, it writes down the
HEAD the command starts from, in the repository's git directory at
`<git-dir>/sdlc/commits/<story>/` — never in the working tree, never committed,
and never pushed. The story's commits are the ones since the code review whose
first parent is a HEAD the hook wrote down, and the range is the one pair of
commits that holds exactly those: a person's commit made after the story's is
left out of it. The gate puts the pair back to git before it records it; where
no single range holds only the watched commits — somebody else's commit sits
between two of the story's, or a merge brought in a side branch — it records no
range, sets `range_unknown` in `--json`, and says so in place of `landed in`:

```text
  no range recorded: the loop watched this story's commits, but no single range holds only them
```

When the range it records ends before HEAD, it says which commit HEAD is:

```text
  landed in 3f1c2a9d4b7e..8a0e5d6c1f2b
  head is c4d9e2f7a130, past 8a0e5d6c1f2b: nothing that landed after 8a0e5d6c1f2b was watched
```

It keeps that HEAD on the story's record, so the fact outlasts the output:
`head_past_commit` in `--json` holds it in full, and [`sdlc log`](#sdlc-log)
repeats the line. Each pass replaces what the last one kept, and a range whose
end is HEAD keeps nothing.

That is usually a person's commit made after the session's, and is then as it
should be. It is also the only sign of the two cases where the range stops short
of the story's work: a command that committed twice, of which the hook watched
only the start, and history rewritten under the loop. When the range's end *is*
HEAD there is no signal at all that anything unwatched landed: a commit the hook
did not see, made straight on a HEAD it did, joins the range and nothing says
so. What the hook wrote down is removed when the finished story is stopped, so
look at it before `sdlc stop` if the line gives you a reason to.

Where the hook watched nothing — the plugin is not installed, enforcement is
switched off for the session, or the story was already under way when this
version arrived — the range is what it always was: from HEAD when code review
first passed to HEAD when the commit gate passes, which can hold commits
somebody else made in that window. The start is left out, and the output says
`landed in <end>`, when it is not known: the repository had no commits yet when
code review first passed, or an older `sdlc` passed it.

A hash in the note has to be inside the range this pass records, or the pass is
refused with [SDLC-E0049](troubleshooting.md#sdlc-e0049): seven to forty
hexadecimal characters that git resolves to a commit count as naming it, and
anything else is prose. Where the start is not known, the one commit the record
names is the range the note is held to. The note needs no hash. The check is
skipped when no range is recorded, so it is a consistency check between the
note and the record, not evidence of which commits are the story's.

The end is forgotten when the commit gate stops standing, and every pass records
its answer over the last one's. A pass that records a watched range moves the
start to that range's start as well, so `commit_base` is then no longer where
code review began. When the hook watched nothing the start stays, even when a
finished story is reopened later, so that story's range can also span
everything that landed on trunk in between.

Recording the last gate is also what finishes the story: when no gate is left
unpassed, the story's status becomes `done` and it leaves the backlog. That is
read from the record rather than from the gate's name, so a gate recorded as
failed afterwards — a code review reopened on work already committed — puts the
story back to `in_progress`, where the rework belongs.

A gate recorded as failed `loop.max_rework_rounds` times since a person last
answered for the story hands the story to that person, as
[`sdlc escalate`](#sdlc-escalate) `gate_failing` would, and says so —
`handed_over` in `--json`.

## `sdlc artifact list`

List the documents this version can store.

```console
$ sdlc artifact list
```

## `sdlc artifact write NAME`

Store one of a gate's documents for the story being worked on.

```console
$ sdlc artifact write analysis < analysis.md
$ sdlc artifact write threats --file /tmp/threats.md
$ sdlc artifact write plan <<'SDLC_DOCUMENT'
# Plan
...
SDLC_DOCUMENT
```

| Flag | What it does |
| --- | --- |
| `--file` | read the document from this path instead of standard input |
| `--story` | store against this story instead of the one being worked on. The id has to be in the backlog |

Names: `analysis`, `threats`, `test_plan`, `plan`, `verification`, `retro`.

Writing a document again replaces it: a gate that ran twice has one record, not
two. An empty document is refused, and so is one larger than a megabyte —
storing a truncated document would leave every later gate reviewing something
that stops mid-sentence.

This is the only way these files are written. The hook refuses anyone who edits
them in place, including the agent whose gate it is. See
[enforcement](enforcement.md).

## `sdlc review list`

Show which reviews each gate expects and where they have got to.

```console
$ sdlc review list
$ sdlc review list --gate design_review --json
```

| Flag | What it does |
| --- | --- |
| `--gate` | only this gate's reviews |
| `--story` | for this story instead of the one being worked on |

Says, for every expected review, whether it can block the gate, what the latest
verdict was, and whether it is **stale** — the reviewer looked at something that
has changed since. A `blocking` reviewer has to approve; an `advisory` one only
has to report; perf is `on budget`: it never has to approve, and its block
stops the gate, because it blocks only on a performance budget the contract
states.

## `sdlc review add GATE ROLE VERDICT`

Record one reviewer's conclusion.

```console
$ sdlc review add design_review architect block --note "AC-3 has no step" < review.md
$ sdlc review add code_review perf note --file /tmp/perf.md
```

| Flag | What it does |
| --- | --- |
| `--file` | read the review from this path instead of standard input |
| `--note` | the headline finding, in one line |
| `--story` | record against this story instead of the one being worked on. The id has to be in the backlog |

Verdicts: `approve`, `block`, `note`.

The command stamps the review with what was in front of the reviewer — the
story's entry in the backlog at `dor`, the plan's content at `design_review`,
the working tree at `verifier_review` and `code_review`. That is what makes an
approval go stale when the thing it approved changes. The story and the tree
both leave out the `status` and `updated` fields `sdlc` writes into the backlog
itself, and the tree leaves out `.sdlc/`; see
[Why approvals go stale](the-loop.md#why-approvals-go-stale).

A block that stops the gate, recorded `loop.max_review_rounds` times by the same
reviewer since a person last answered for the story, hands the story to that
person as `review_not_converging`, and says so — `handed_over` in `--json`.

Rounds are kept rather than overwritten, so "what did the architect say last
time" stays answerable.

## `sdlc freeze`

Lock the acceptance tests by content.

```console
$ sdlc freeze
$ sdlc freeze --json
```

Records the sha256 of every file that `paths.tests` in your configuration calls
a test, asking git which files are in the working tree so that build output and
vendored code cannot be mistaken for one.

Refuses if a freeze already exists. Taking a second one over the top would
quietly bless whatever changed in between.

The one exception is `freeze.allow_new_test_files`. With it on, running
`sdlc freeze` again on the story's own freeze adds the test files written since,
provided none of the frozen ones has changed, and lists what it added; `--json`
names them in `added`.

## `sdlc unfreeze`

Lift the test freeze, on the record.

```console
$ sdlc unfreeze --reason "AC-2's test asserted the old error message"
```

| Flag | What it does |
| --- | --- |
| `--reason` | why the frozen tests have to change. Required |

The reason is required because that is the whole point. Lifting the freeze is
sometimes right — a test encoded the wrong behaviour — and it is also exactly
the move an agent would make to reach green. Recording why is what tells the two
apart.

It also sends the story back to Gate 3. The tests are about to change, so
`tests_frozen`, and every gate passed after it, go back to `pending` with the
reason as their note. They are listed under `reopened`, in `--json` as well.

## `sdlc escalate`

Hand the story to a person, and end the iteration.

```console
$ sdlc escalate pre_commit_approval --message "the migration has no down step"
```

| Flag | What it does |
| --- | --- |
| `--message` | what the person is being asked to decide. Required |

The type is a word for the kind of question — `pre_commit_approval`,
`spec_unclear`, `loop_stalled` — and the message is the question. It goes on the
story's record, bound to the work as it stands. The story becomes
`awaiting_human`, and the iteration ends, so the session stops rather than
carrying on past the question. `sdlc start` refuses the story until somebody
answers, and `sdlc status` lists it as waiting.

Nothing else starts in that checkout in the meantime either: `sdlc start` picks
up a story already under way before a new one, so it picks the waiting story and
refuses it. That is deliberate. The work waiting for an answer is still in the
tree, and a second story started on top of it would end up in the same commit.
With [`git.worktrees`](configuration.md#git) on, a story waiting in its own
worktree is held there, and `sdlc start` in the main checkout passes it over
and begins the next story in a worktree of its own.

## `sdlc approve`

A person's answer to an escalation.

```console
$ sdlc approve US-001
$ sdlc approve US-001 --reject "the migration has no way back"
```

| Flag | What it does |
| --- | --- |
| `--reject` | send the work back instead, saying why |

Run it in your own terminal: the hook refuses it from a tool call in any session
while a story waits for an answer or is under way, because an agent that could
answer would be approving its own work. Without a story id it
answers for the story being worked on. The answer goes on the record with the
tree it was given for, the story goes back to `in_progress`, and `sdlc start`
picks it up again.

## `sdlc release`

Let a story go from the checkout holding it, so that any checkout can start it.

```console
$ sdlc release US-001 --reason "the worktree it was started in was deleted"
Released US-001: the checkout at "/home/dev/project-wt" no longer holds it. "sdlc start US-001" picks it up in any checkout.
```

| Flag | What it does |
| --- | --- |
| `--reason` | why the story is moving to another checkout. Required |

Every checkout of a repository, the main checkout and each linked worktree, works
the one backlog. A story is held by the checkout that started it until it is
finished or dropped, and no other checkout can start it, record a gate for it, or
answer for it ([SDLC-E0050](troubleshooting.md#sdlc-e0050)). Release it when that
checkout is gone ([SDLC-E0051](troubleshooting.md#sdlc-e0051)), or when you want
the story worked somewhere else.

Run it in your own terminal: the hook refuses it from a tool call, because an
agent that could run it could take another checkout's story. It refuses while an
iteration is running on the story ([SDLC-E0012](troubleshooting.md#sdlc-e0012)):
`sdlc stop` in the checkout named ends it first. A story nothing holds is not an
error: it says `Nothing to release` and changes nothing.

Nothing is moved. Anything the checkout that held the story has not committed
stays there, and the output says so while that checkout exists. The reason goes
on the story's record; the checkout's path does not. `sdlc start` and
`sdlc approve` then say that work the old checkout had not committed stayed
with it, and that if this checkout is that one, moved here, the work is here. A
freeze does not move either: in a checkout that picks the story up without it,
`sdlc unfreeze --reason ...` lifts it and `sdlc freeze` takes it again. Release
says so only where the checkout it runs in does not hold the story's freeze. A
checkout that was moved took its freeze with it, and start and approve then say
nothing about where the work is. In `--json` the output is `ok`, `story` and
`checkout`, the checkout that held it, absent when none did.

## `sdlc cost`

What the story has cost so far, against what it was expected to.

```console
$ sdlc cost
US-001  $12.40 spent of $60.00 (20.7%)

$ sdlc cost add --usd 1.42 --note "gate 4, five reviewers"
US-001  $13.82 spent of $60.00 (23%)
```

### `sdlc cost add`

Records one amount against the story being worked on, or the one `--story` names.

| Flag | What it does |
| --- | --- |
| `--usd` | the amount, in US dollars. Required |
| `--note` | what it was spent on |
| `--story` | the story it was spent on, when no iteration is running on it. `sdlc cost` takes it too |

`budget.per_story_usd` in `.sdlc/config.json` says what one story is expected to cost, and
`alert_fractions` says where along the way to say so. An alert goes to standard error, once,
on the entry that crosses it, with `--json` as well as without — so a runner reading standard
output for the number still sees it, and the same warning does not repeat on every entry
afterwards.

**Nothing here blocks.** A budget that stops a story halfway leaves the work stranded between
gates, which costs more than the overspend it prevents. The entry that uses up the budget says
so, whatever `alert_fractions` holds, and names `sdlc stop` if that is the call.

Where the number comes from is the runner's business. A headless session reports its cost only
once it is over, and by then the iteration it ran may have ended, so the runner notes the story
first and names it:

```console
$ story=$(sdlc status --json | jq -r '.active // .next.story')
$ cost=$(claude -p "/sdlc:next" --output-format json | jq .total_cost_usd)
$ sdlc cost add --story "$story" --usd "$cost"
```

An interactive session has `/cost`. An empty or unparseable amount is refused rather than
recorded as zero, because a story that silently cost nothing is the one wrong answer nobody
questions.

## `sdlc doctor`

Check this project and say how to fix what is wrong.

```console
$ sdlc doctor
$ sdlc doctor --json
```

Looks at the git repository, the git command itself, the configuration —
including a setting it does not know, such as a misspelled one — the backlog,
the loop's state files the hook reads on every tool call and the commit
[`sdlc ack`](#sdlc-ack) recorded, the contract in `CLAUDE.md`, the programs your
configured commands would run, and whether `sdlc` is on your `PATH`. Every
problem comes with the command that fixes it. Exits non-zero when something is
wrong, so it works in a script.

In a repository with linked worktrees it names the checkout holding each story
under "loop state", in whichever checkout you run it. In a linked worktree it
reads the backlog and the records from the main checkout, and reports
[SDLC-E0052](troubleshooting.md#sdlc-e0052) when the repository has none.

With [`git.worktrees`](configuration.md#git) on, the git command check is a
problem below git 2.17, the oldest that can remove a worktree again: install a
newer git, or set `git.worktrees` to false to begin each story in the checkout
`sdlc start` runs in. A `story worktrees` check appears only when there is
something to do:

- trunk has no `.sdlc/config.json` committed, so `sdlc start` would refuse to
  begin a story ([SDLC-E0053](troubleshooting.md#sdlc-e0053)): commit it and
  `CLAUDE.md` to trunk, or set `git.worktrees` to false in this checkout's
  `.sdlc/config.json`;
- trunk tracks files under `.sdlc/state`, the loop's own state, which every
  story worktree writes to, so no story worked in a worktree could land
  ([SDLC-E0055](troubleshooting.md#sdlc-e0055)): a person stops tracking them on
  trunk with `git rm -r --cached .sdlc/state` in the main checkout, adds
  `/.sdlc/state/` to `.gitignore`, and commits both;
- git lists a worktree under `.sdlc/worktrees/` that no checkout holds, such as
  a finished story's or one an interrupted start left. It says what is true of
  each, and what a person does about it, in the order a person takes it, and
  never counts the loop's own files under `.sdlc/state` as somebody's changes:
  - its directory is gone: `git worktree prune` in the main checkout makes git
    forget it;
  - it has changes nobody committed, which may be somebody's work: keep what is
    wanted, committed on `story/<ID>` or copied elsewhere, before removing
    anything;
  - `story/<ID>` holds commits trunk does not have, which are the story's work,
    and come after the changes where there are both: bring them to trunk, with
    trunk checked out, with `git merge --ff-only story/<ID>` in the main
    checkout, before removing either, and if git cannot fast-forward, first
    rebase `story/<ID>` on trunk in its worktree and run the tests;
  - it is on a branch other than `story/<ID>`, which start did not make: the
    branch is named as a shell reads it back without expanding anything, in
    single quotes where it needs them, and no command is built from it. A
    person looks at what it holds, with
    `git -C .sdlc/worktrees/<ID> log --oneline <trunk>..HEAD` in the main
    checkout, before merging or removing anything;
  - it is on no branch, and its HEAD has commits trunk does not have, as a
    rebase stopped part-way leaves it: "a commit on no branch is lost when its
    worktree is removed", so no command removes it. A person looks at them
    with that `git log`, and keeps what is wanted on a branch, with
    `git -C .sdlc/worktrees/<ID> branch <new name>`, before merging or removing
    anything;
  - git could not read what it holds: a person looks with that `git log`
    before removing anything;
  - its HEAD, on `story/<ID>` or on no branch, has nothing trunk does not: if
    no `sdlc start` is running for the story, remove it with
    `git worktree remove .sdlc/worktrees/<ID>`, and `git branch -d story/<ID>`
    where it is on that branch, in the main checkout. Where the loop's own
    files are all it has, it says they are safe to lose if git refuses over
    them.

Where there is more than one thing to say, each is a line of its own, followed
by its own `fix:` line, and in `--json` the detail and the fix hold the lines in
the same order, one to a line.

It never suggests `git branch -D` or `--force`. See
[more than one checkout](the-loop.md#more-than-one-checkout).

It does not run your test suite. This is meant to be the fast answer to "why is
this not working".

## `sdlc version`

Print the version of sdlc you are running.

```console
$ sdlc version
$ sdlc version --short
$ sdlc version --json
```

| Flag | What it does |
| --- | --- |
| `--short` | print the version alone, with nothing around it |

With `--json` it prints `version`, `commit`, `date` and `dirty`, and with
`--short` as well, `version` alone.

## `sdlc hook EVENT`

Not for you to run. It is what the plugin's hook script calls, reading the event
on standard input. It is documented here only so that a line in a log makes
sense.
