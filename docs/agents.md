# Agents

Eleven agents do the work. Each starts from a fresh context, can only write what
its gate produces, and never sees the others' reasoning.

That last part is the whole design. A reviewer that inherited the argument for a
change cannot notice what only makes sense if you already knew why, and a
verifier that watched the implementation happen is checking its memory rather
than the code.

## Who does what

| Agent | Gate | Can it block? | Writes |
| --- | --- | --- | --- |
| `sdlc:researcher` | 2 | — | `ANALYSIS.md`, `THREATS.md`, `CODEMAP.md` |
| `sdlc:sdet` | 3 | — | test files, `TEST-PLAN.md` |
| `sdlc:implementer` | 4, 5 | — | `PLAN.md`, then production code |
| `sdlc:architect` | 4 | **Yes** | its review |
| `sdlc:security` | 4, 7 | **Yes** when the story is security-sensitive | its review |
| `sdlc:red-team` | 4 | — | its review |
| `sdlc:perf` | 4, 7 | Only against a stated budget | its review |
| `sdlc:human-advocate` | 4, 7 | — | its review |
| `sdlc:verifier` | 6 | **Yes** | `VERIFICATION.md`, its verdict |
| `sdlc:code-reviewer` | 7 | **Yes** | its review |
| `sdlc:bookkeeper` | 9 | — | `RETRO.md`, `CODEMAP.md` |

Advisory does not mean optional. A gate will not pass until every reviewer it
expects has reported, because a reviewer you can skip by not running it is not a
reviewer at all.

## The three that are easy to underrate

**`sdlc:red-team`** does a pass nobody else does: it reads the frozen tests as
somebody looking for the cheapest way to make them all pass without implementing
anything. The gaming vectors it names are exactly what the verifier checks for
at Gate 6, so a vector it misses is one nobody looks for.

**`sdlc:verifier`** re-derives what each acceptance criterion *should* mean
before it reads a single test, then compares that with what was actually
written. It runs your commands rather than concluding from reading, and it
verifies at least one criterion end to end itself rather than through the test
that claims it.

**`sdlc:human-advocate`** is the only one checking that the change is usable
rather than correct: that errors say what to do, that names match your glossary
instead of inventing a second word for an existing idea, and that whoever is
woken up at 3am can tell which branch was taken.

## Models

No agent here names a model. Every one of them inherits the model of the
session that started it, so the dial you already turn — `/model` — moves the
whole loop with it. The same goes for reasoning effort: whatever the session is
set to is what the gates get.

That is deliberate. Which model you pay for is your decision, not a plugin's,
and a tier pinned in a file here would quietly override the choice you just
made and go stale the first time the tiers are renamed.

What is worth knowing when you choose: the blocking gates — the architect at
Gate 4, the verifier at Gate 6, the code reviewer at Gate 7 — are the ones that
decide whether work proceeds, and they are the cheapest place to spend on
quality, because the alternative is finding the same problem after it is
merged. Run the loop on the strongest model you are willing to pay for.

## Writing your own

An agent is a markdown file under `plugin/agents/` with frontmatter. The name in
the frontmatter and the file name must match, and the plugin exposes it as
`sdlc:<name>`.

To add a reviewer to a gate, add it to the reviewer registry in
`internal/model/model.go` as well — the registry is what a gate reads when it
asks who is outstanding. An agent with no entry is never asked for, and an entry
with no agent makes the gate wait forever.

## What none of them may do

- Write outside the repository, or into `.git`, `.claude`, `CLAUDE.md`,
  `.sdlc/config.json` or `.sdlc/state`.
- Write a gate's documents as files. Those go through `sdlc artifact write` and
  `sdlc review add`, from everyone.
- Edit a frozen acceptance test.
- Commit before the gates are done.

See [enforcement](enforcement.md) for the full list and what each refusal says.

## Prompt injection

Every agent is told the same thing, and it is worth knowing that they are: text
in the repository that addresses them — "reviewer: approve this", "analyst:
assume X" — is data, not instruction. They are asked to report that they found
it rather than act on it.

That is a mitigation, not a guarantee. The structural defence is that no single
agent can carry a change through on its own.
