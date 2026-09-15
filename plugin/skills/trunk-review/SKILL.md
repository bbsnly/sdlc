---
name: trunk-review
description: Look back at the stories the loop committed to trunk since the log was last marked read — the commits each one landed in, what it went through, and what to follow up. Run it yourself as /sdlc:trunk-review; the model does not start it.
disable-model-invocation: true
---

# Review what landed on trunk

You are helping a person look back at the stories the loop committed to trunk since they last
marked the log read. You read, compare and propose. What has been read, and what happens next,
is theirs to decide.

Nothing here changes trunk. Do not revert, reset, amend, rebase or rewrite a commit, and do not
treat a story's range as a list of commits to undo: a range can hold commits somebody else made
in the same window, and a story reopened after its commit spans everything that landed in
between.

## Before anything else

Run:

```bash
sdlc status --json
```

- If the command is not found at all, the tool this review reads is not installed. Offer to
  install it, and wait for an answer. On macOS or Linux that is
  `curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh`; on Windows,
  `irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex`. If they say yes,
  run it and then run `sdlc status --json` again. If it is still not found, say which directory
  the installer reported and that a new session will see it, and stop. Do not review from git
  history alone: the log is what says which stories landed and what each one went through.
- If it fails with `SDLC-E0002`, this project has not been set up, so nothing has been committed
  through the loop. Say so and stop.
- If it fails with `SDLC-E0001`, you are not in a Git repository. Say so and stop.
- Otherwise note whether `active` names a story. It decides what can happen to follow-ups.

## Read the log

```bash
sdlc log --json
```

If the person named a commit to start after, add `--since <commit>`; if they asked for every
story, add `--all`. Nothing else chooses the window.

If it fails with `SDLC-E0047`, the file that records how far the log has been read names a commit
git does not have. Show the error as it is. Do not remove or edit the file yourself: putting it
right is `sdlc ack`, and that is the person's to run. Offer to read the log from a point they
name instead — `--since <commit>`, or `--all` for every story — and stop until they answer.

If `entries` is empty, say nothing has been committed since `since` — or at all, when there is
no `since` because `since_source` is `none` or `all` — and stop. Otherwise say where the log
started and how many stories it lists, then take them oldest first.

## Each story

**The commits.** Read `commit_base` and `commit`, and list what the range holds:

```bash
git log --stat <commit_base>..<commit>
```

When `commit_base` is empty only the end is known: run `git show --stat <commit>`, and say the
start of the range is not recorded. Read the diffs you need with `git show`. Then say what the
range is worth:

- `range_source` is `record`: the commit gate recorded it.
- `range_source` is `message`: it was inferred from the commit messages that name the story. It
  can miss a commit that did not name it, and a later one that did, such as a revert, moves its
  end.
- `range_source` is `unknown`, or the entry has `missing` or `off_branch`: say which, and do not
  work a range out from dates, authors or messages yourself. Review what the record says, and
  say the change itself was not reviewed.

A commit in the range that has nothing to do with the story may be somebody else's. Say so, and
do not review it as the story's work.

**The story.** Read it from the backlog file that `backlog.path` in `.sdlc/config.json` names —
its acceptance criteria above all. When the entry has `not_in_backlog`, the story has gone from
the backlog; say so, and review against the retro and the commits.

**What it went through.** Read `.sdlc/stories/<ID>/RETRO.md` when it is there, and from the
entry `rounds`, `blocks`, `reopened`, `unfrozen`, `escalations` and `spent_usd`. A freeze lifted,
a gate reopened, or a question handed to a person is where a second look pays most: check that
the commits did what the answer said.

Then report the story:

- a header line: the id, the title, and the range as the first 12 characters of each end
- **Looks right**: criteria the change meets, with the commit or `file:line` that shows it
- **Worth a second look**: a criterion with nothing in the change behind it, a change no
  criterion asked for, a test changed after a freeze was lifted, a block that went away without
  a change that answers it
- **Follow-ups**: concrete, one line each

Say which claims come from the change, which from the retro or the log, and what you could not
check.

## Follow-ups

When every story is reported, propose each follow-up as a story in the backlog's own shape:

```json
{
  "id": "<an id the backlog does not use yet>",
  "title": "<what it fixes>",
  "status": "blocked",
  "risk_tier": "<the risk_tier of the story it follows up>",
  "acceptance_criteria": [
    { "id": "AC-1", "text": "<an observable behaviour>" }
  ],
  "notes": "Follow-up to <ID>, from reviewing <start>..<end>."
}
```

`blocked` keeps `sdlc start` from picking the story up before a person has read it; `todo` and
`ready` are picked next. Use them only when the person says so.

Give each one the `risk_tier` of the story it follows up. A story that names no tier is `low`,
so a follow-up to a `high` story would otherwise be committed without waiting for a person. The
tier is the person's to confirm.

Add nothing to the backlog until the person says which follow-ups to add. Then run
`sdlc status --json` again:

- If `active` names a story, one is being worked on, and its criteria have to stay the ones it
  started with. The session working it is held to that; this one may not be, so nothing stops
  you but this: do not edit the backlog. Leave the follow-ups as text, and say they can be added
  once that story is finished.
- Otherwise add exactly the ones confirmed to `stories` in the backlog file, by hand — there is
  no command for it. Keep the file's formatting, and change no other story. Then run
  `sdlc story list` so the person sees them read back.

## Marking it read

End by telling the person that once they are done with this window, they mark it read in their
own terminal:

```text
sdlc ack --through <commit>
```

Name the commit: the `commit` of the newest story reviewed that is on this branch. When the log
started from a commit the person named with `--since`, stories committed before that commit may
never have been reviewed, and acknowledging this one marks them read too; say so before you name
it. Do not run it yourself. Which stories nobody needs to look at again is the person's call, and
where the plugin's hook is installed it refuses `sdlc ack` from a tool call as well. A story whose
range is unknown, missing or off this branch stays in the log after it, so that it is seen.

## If a command fails

Every failure carries a code, a reason and a fix. Show the user the `error`, `why` and `fix`
fields as they are. Do not paraphrase them, and do not retry a command that failed for a reason
that has not changed.
