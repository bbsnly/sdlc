# Agents

- [Introduction](#introduction)
- [Who does what](#who-does-what)
- [The agents, gate by gate](#the-agents-gate-by-gate)
  - [Before the work: the project manager](#before-the-work-the-project-manager)
  - [Gate 2: the researcher](#gate-2-the-researcher)
  - [Gate 3: the test author](#gate-3-the-test-author)
  - [Gate 3: the test reviewer](#gate-3-the-test-reviewer)
  - [Gates 4 and 5: the implementer](#gates-4-and-5-the-implementer)
  - [Gate 4: the reviewers of the plan](#gate-4-the-reviewers-of-the-plan)
  - [Gate 6: the verifier](#gate-6-the-verifier)
  - [Gate 7: the reviewers of the code](#gate-7-the-reviewers-of-the-code)
  - [Gate 9: the bookkeeper](#gate-9-the-bookkeeper)
  - [What an agent asks you, and what it decides](#what-an-agent-asks-you-and-what-it-decides)
- [Which reviewers can stop a gate](#which-reviewers-can-stop-a-gate)
- [The staffing](#the-staffing)
  - [See who is working a story](#see-who-is-working-a-story)
  - [Choose the model for one agent](#choose-the-model-for-one-agent)
  - [Change the team for every story](#change-the-team-for-every-story)
  - [Doing an agent's gate by hand](#doing-an-agents-gate-by-hand)
- [Models](#models)
- [What none of them may do](#what-none-of-them-may-do)
- [Where to go next](#where-to-go-next)

## Introduction

Thirteen agents do the work of the gates. One analyses your story, one writes
its tests, one plans and writes the code, and the rest check that work from
different angles. You do not start any of them yourself. You type `/sdlc:next`,
and the loop hands each gate to the agent whose job it is.

Every agent starts from a fresh context and never sees another's reasoning.
That is the whole design. A reviewer that inherited the argument for a change
cannot notice what only makes sense if you already knew why. A verifier that
watched the code being written would be checking its memory, not the code.

There is one exception. A reviewer asked to look again at a revised plan is
resumed, and shown what changed along with its own findings and how each was
answered. It still never sees the argument that produced the plan, and its
first look was taken alone.

This page introduces the agents in the order your story meets them. Then it
shows how to see who is working a story, and how to change that: for one
story, or for every story in your project.

## Who does what

Each gate has an agent that does its work, and some gates have reviewers on
top. Here they are, in the order a story meets them:

| Agent | Works at | Can it block? | What it produces |
| --- | --- | --- | --- |
| `sdlc:project-manager` | after Gates 1 and 2 | — | the staffing: which agents the story needs |
| `sdlc:researcher` | Gate 2 | — | `ANALYSIS.md`, `THREATS.md`, and additions to `CODEMAP.md` |
| `sdlc:sdet` | Gate 3 | — | the failing tests, and `TEST-PLAN.md` |
| `sdlc:test-reviewer` | Gate 3 | **Yes**, unless you set its [`can_block`](configuration.md#an-agents-entry) to false | its review |
| `sdlc:implementer` | Gates 4 and 5 | — | `PLAN.md`, then the code |
| `sdlc:architect` | Gate 4 | **Yes** | its review |
| `sdlc:security` | Gates 4 and 7 | **Yes**, when the story is security-sensitive | its review |
| `sdlc:red-team` | Gate 4 | Only where you set its [`can_block`](configuration.md#an-agents-entry) to true | its review |
| `sdlc:perf` | Gates 4 and 7 | Only when a stated budget is breached | its review |
| `sdlc:human-advocate` | Gates 4 and 7, and Gate 1 [if you ask for it](configuration.md#human_gates) | Only where you set its [`can_block`](configuration.md#an-agents-entry) to true | its review |
| `sdlc:verifier` | Gate 6 | **Yes** | `VERIFICATION.md`, and its verdict |
| `sdlc:code-reviewer` | Gate 7 | **Yes**, unless you set its [`can_block`](configuration.md#an-agents-entry) to false | its review |
| `sdlc:bookkeeper` | Gate 9 | — | `RETRO.md`, and the story's sections of `CODEMAP.md` |

Two gates have no agent. At Gate 1 the conversation, the Claude Code session
you typed `/sdlc:next` into, checks that each criterion can be tested. At Gate
8 it makes the commit. Every document a gate produces is kept under
`.sdlc/stories/<ID>/`, where you can read it.

## The agents, gate by gate

You don't need to know any of this to run a story. It helps when you read what
an agent wrote, or when one of them stops the loop and you want to know why it
was that one.

### Before the work: the project manager

The project manager decides who works your story. Once the story is found
ready, it reads the story and says, for every agent, whether this story needs
it, and what model and effort to ask for, with a reason for each. That record
is the story's staffing.

It looks again once the analysis is in, and again whenever an agent's work
falls short. It writes no code, no tests and no documents, and it records no
gate. [The staffing](#the-staffing) says how it chooses.

### Gate 2: the researcher

The researcher reads your story against the codebase before anyone writes
anything. It restates each criterion, finds the code the story touches, and
records what that code does today. It also lists what could go wrong, and
decides whether the story is security-sensitive, which later decides whether
the security reviewer can stop a gate.

Where your story leaves a question open, the researcher does not guess. It
lists the question. If the question is a product one, such as which of two
readings of a criterion is meant, the loop puts it to you, with options and a
recommendation. [What an agent asks you, and what it
decides](#what-an-agent-asks-you-and-what-it-decides) says where the line is.

### Gate 3: the test author

The test author turns every acceptance criterion into at least one test that
fails now, for the reason the criterion names. It adds the boundary cases: the
empty input, the zero, the second call. It cannot write production code, so a
test is written against what you asked for and never against what got built.

When it is done, and the test reviewer has approved, the tests are frozen. The
freeze runs the new tests first, and refuses if they pass, since a test written
before its code has to fail. From here on nobody edits them: not the
implementer, not the test author, and not the conversation.

### Gate 3: the test reviewer

The test reviewer reads the new tests before they are frozen, while a test can
still change. After the freeze, a wrong test costs a person, because only a
person lifts the freeze. Before it, the same test costs one more round with the
test author.

It reads the tests against the acceptance criteria, the analysis and your
contract's test conventions, and runs them. It blocks on a test that
contradicts a criterion, a criterion that no test would catch, a test that
passes for the wrong reason, a test that depends on the clock, the network, an
order or the developer's git configuration, and a criterion id missing from the
test names. Its block goes back to the test author.

Where it is staffed, `sdlc freeze` waits for its approval of the test files as
they are, so a test changed, added or renamed after the approval is reviewed
again. It edits no test: its instructions say so, though before the freeze no
rule of the hook stops it.

### Gates 4 and 5: the implementer

The implementer has two jobs. At Gate 4 it writes the plan: the smallest change
that makes the frozen tests pass, as steps somebody else could follow, with
the alternative it rejected. It stops there, because the plan is reviewed
before any of it is written.

At Gate 5 it carries the plan out, one step at a time, until the frozen tests
pass. Then `sdlc` itself runs your build, tests and checks once, and the gate
passes only if they do. If the implementer finds that a frozen test is wrong, or
that the plan is, it stops and says so. It also answers the reviewers: each finding they raise is fixed,
or declined with a reason the reviewer then judges.

### Gate 4: the reviewers of the plan

Up to five reviewers read the plan, each alone:

- **The architect** decides whether this is the right change to make. It blocks
  a plan that leaves a criterion uncovered, breaks one of your architecture
  rules, or does not say what it does.
- **The security reviewer** looks for the failure that costs the most and shows
  up the latest: an unchecked trust boundary, a missing authorisation check, a
  secret or personal data put where it should not be.
- **The red team** attacks the plan as a hostile user, as a careless operator,
  and as an implementer looking for the cheapest way to pass the frozen tests
  without building the behaviour. The gaming vectors it names are what the
  verifier hunts for at Gate 6, so a vector it misses is one nobody looks for.
- **The performance reviewer** looks for cost that grows with input: work
  inside a loop, growth with no bound, a lock held too long. It blocks only on
  a budget your contract states.
- **The human advocate** is the only one checking that the change is usable
  and not only correct. It checks that errors say what to do, that names match
  your glossary, and that whoever is woken at 3am can tell what happened.

### Gate 6: the verifier

The verifier checks the work of somebody who was trying hard to make the tests
pass, which is not the same as checking that the behaviour is there. It works
out what each criterion should mean before it reads a single test. It uses the
result of your commands that `sdlc` recorded at Gate 5 where it still matches
the code, runs them itself where it does not, and never concludes from reading
that a test would pass. It hunts through the diff for the gaming vectors the red
team named, and it verifies at least one criterion end to end, without going
through the test that claims it.

Before it approves, it also checks that every finding from the reviews was
closed, and closed honestly. Its block sends the story back to Gate 5.

### Gate 7: the reviewers of the code

**The code reviewer** reads the diff in a context that never saw how it was
written. It blocks on a defect, on code that quietly departs from the plan,
and on anything that games a test. The security reviewer, the performance
reviewer and the human advocate read the diff too, each for what it looked for
in the plan.

### Gate 9: the bookkeeper

The bookkeeper closes the story by writing its retro: where the work departed
from the plan, what sent it back and how often, and what the story showed that
nobody knew before. It also adds anything new to `CODEMAP.md`, the map of your
codebase that the next story's researcher reads first. It decides nothing, and
changes no gate.

### What an agent asks you, and what it decides

An agent that meets a question sorts it as one of two kinds.

A **product decision** is about what the story's user sees or can do, which of
two readings of a criterion is meant, what is in or out of the story, or
anything your contract reserves to a person. The agent stops and hands the
question back with two or three options and its recommendation, and the loop
asks you. A hand-back like that is not a failed run.

Every other question is an **implementation decision**: how an edge case
behaves, which of two designs meets the criteria, what kind of test to write,
an internal name. The agent recommends one option and says why, and the loop
applies it, records it and carries on. It never asks you.

The researcher is the one that does not stop. It marks each question as
product or implementation and finishes the analysis, so that the loop puts
every product question to you at once. [Human approval
gates](guides/human-approval-gates.md#what-the-loop-asks-you-and-what-it-decides)
shows where you read the decisions the loop made.

## Which reviewers can stop a gate

A reviewer records one of two verdicts, `approve` or `block`. Whether a block
stops the gate depends on the reviewer. A **blocking** reviewer's block stops
it. An **advisory** reviewer's block is recorded, and the gate goes on.

To see how each reviewer stands on the story under way, run `sdlc review list`
from the story's worktree:

```console
$ sdlc review list
Review policy: REVIEW.md
  tests_frozen    test-reviewer   blocking  not reviewed
  design_review   architect       blocking  not reviewed
  design_review   red-team        advisory  not reviewed
  design_review   security        blocking  not reviewed
  design_review   perf            on budget not reviewed
  design_review   human-advocate  advisory  not reviewed
  verifier_review verifier        blocking  not reviewed
  code_review     code-reviewer   blocking  not reviewed
  code_review     security        blocking  not reviewed
  code_review     perf            on budget not reviewed
  code_review     human-advocate  advisory  not reviewed
```

Each line is a review the story is waiting for: the review of the tests at Gate
3, the review of the plan at Gate 4, the verifier's at Gate 6 and the review of
the code at Gate 7. `on budget`
is the performance reviewer's own standing: its block stops the gate only when
a budget in your contract is breached. The security reviewer stays `blocking`
until the analysis finds that the story is not security-sensitive. As the
reviews come in, the last column becomes each reviewer's verdict.

Don't read advisory as optional. A gate does not pass until every reviewer it
expects has reported, because a reviewer you can skip by not running it is not
a reviewer at all. An advisory reviewer's findings count like anyone else's
too: every one has to be closed before the commit.

A reviewer that keeps blocking does not hand the story to you straight away. At
its limit, the gate goes to the reviewer's
[arbiter](guides/when-a-reviewer-blocks.md#when-a-reviewer-keeps-blocking): a
fresh agent of the same kind, whose review then stands for it. Only the
arbiter's own limit hands the story to you. When a reviewer does stop a gate,
[When a reviewer blocks](guides/when-a-reviewer-blocks.md) shows what happens
next.

## The staffing

Not every story needs every reviewer. A rename needs no performance review, and
a change to how sessions are checked needs every pair of eyes you have. So the
project manager, `sdlc:project-manager`, staffs each story on its own. The
staffing says which agents the story needs, and the model and effort each is
asked for, with a reason for each. A gate then waits only for the reviewers the
story needs.

It chooses from what the work is, and from what each agent would have to do
with it. It does not choose from the story's risk tier or its priority. Those
say how much a mistake costs and how soon the story is wanted, which is a
different question from who is needed to do the work. When it cannot tell
whether a reviewer is needed, the reviewer stays in.

Some agents are never left out: the ones that do a gate's work, the verifier,
the code reviewer, and the security reviewer on any story that is
security-sensitive or has not yet been found not to be. The test reviewer is
left out only where each criterion's test is plainly that criterion and nothing
else, with the reason on the record.

### See who is working a story

To read the staffing, run `sdlc staffing show` from the story's worktree:

```console
$ sdlc staffing show
```

It prints one line for each agent: the model and effort it is asked for, or
that it is left out, with the project manager's reason. Every change made
since follows, with when and why.

Three more commands show the same decision from other sides:

- `sdlc status` names the reviewers left out. It is worth a look before you
  approve a commit.
- `sdlc review list` shows which reviews a gate is not waiting for, and why.
- `sdlc log` keeps the staffing of a story that has finished.

### Choose the model for one agent

When you know better than the project manager what a story deserves, pin it.
Type this in Claude Code, while the story is under way:

```text
/sdlc:assign security --model opus --effort high --reason "this one touches the installer"
```

In a terminal of your own, the same command is `sdlc staffing assign`, with the
same arguments. The agent is then needed at every gate it has a part in, and
the project manager staffs around your choice and cannot change it. To change
it yourself, assign again.

> [!NOTE]
> Assigning is yours alone. The hook refuses `sdlc staffing assign` from an
> agent, because a session that could run it could pin every agent where it
> liked, in your name. An assignment can bring an agent into a story, and it
> cannot take one out.

### Change the team for every story

The project's team is set in the [`team`](configuration.md#team) section of
`.sdlc/config.json`, where `sdlc init` wrote a line for each agent. Each line
answers a few questions about that agent:

| Setting | What it decides |
| --- | --- |
| `enabled` | whether the agent takes part in any story at all |
| `can_block` | for a reviewer, whether its block stops the gate |
| `instances` | for a reviewer, how many separate reviews it gives at each gate |
| `lowest` | the lowest model and effort the project manager may ask for it |
| `bring_back` | for a reviewer of the code, the paths that bring it back to a story it was left out of |

Let's take a look at a team that lets the red team stop a gate, has two
security reviewers, and sets a floor under what the verifier is asked for:

```json
"verifier":        { "enabled": true, "can_block": true, "instances": 1, "lowest": { "model": "opus", "effort": "high" } },
"security":        { "enabled": true, "can_block": true, "instances": 2, "bring_back": ["internal/auth/"] },
"red-team":        { "enabled": true, "can_block": true, "instances": 1 },
```

With `instances` at 2, each gate waits for two security reviews, recorded as
`security` and `security-2`, and the staffing has a row for each. The
`bring_back` path returns the security reviewer to the code review of any
story that changes a file under `internal/auth/`, even where the project
manager left it out.

To staff every story the same way, with every enabled agent taking part, turn
the project manager off:

```json
"project-manager": { "enabled": false, "instances": 1 },
```

Then commit the file. A change to the team reaches the stories you start after
it is on trunk, and not a story already under way. To check what you
committed, run `sdlc doctor`: under `configuration` it names each agent you
have disabled, each `can_block` you have changed, and each reviewer with more
than one instance.

> [!WARNING]
> Two of these settings loosen the loop. `enabled` set to false takes an agent
> out of every story, and `can_block` set to false lets a gate pass over that
> reviewer's block. Read a change to either before you commit it.

Be honest with yourself about `instances` too. `sdlc` cannot see that two
instances were two separate runs: one run can record a review under both
`security` and `security-2`. `sdlc trace` shows how many runs there really
were. [Configuration](configuration.md#an-agents-entry) has every setting, its
limits and its defaults.

### Doing an agent's gate by hand

You may turn off an agent whose work a gate cannot do without: one that does a
gate's work, or the verifier, or the code reviewer. That work is then yours.
When the story reaches the gate, the loop hands it to you, and
`sdlc staffing show` lists the gate under `by_hand`.

If you have disabled such an agent, open its definition and follow the Outputs
section, which names each document the gate needs and the command that stores
it. Each agent is defined by one markdown file, named after it, under
[`plugin/agents`](https://github.com/bbsnly/sdlc/tree/main/plugin/agents) in
the sdlc repository. A disabled reviewer of the plan or of the code is
different: nobody waits for it, and nothing is handed to you.

## Models

No agent names a model. Each one inherits the model of the session that
started it, so the dial you already turn, `/model`, moves the whole loop with
it. The same goes for reasoning effort.

That is deliberate. Which model you pay for is your decision and not a
plugin's. A model pinned in a plugin file would quietly override your choice,
and go stale the first time the models are renamed.

The staffing adds one thing. Where the project manager staffs a story, each
agent is handed the model and effort the staffing asked for, chosen from the
`models` and `efforts` lists in your `team` section. That is what was asked
for, not what was observed. Your environment can override both, and `sdlc`
records only the asking.

One thing is worth knowing when you choose. The architect, the verifier and
the code reviewer decide whether work goes ahead, and they are the cheapest
place to spend on quality: the alternative is finding the same problem after
it has landed. Run the loop on the strongest model you are willing to pay for.

## What none of them may do

Whatever its role, no agent may do any of the following. The hook refuses each
one, so you are not relying on an agent remembering.

- Write outside your repository, or into `.git`, the loop's own settings and
  state, or your instruction files: `CLAUDE.md`, `AGENTS.md`, and the `.claude`
  and `.agents` directories, in the project or in your home directory.
- Write a gate's documents as loose files. Every analysis, plan and review is
  stored through `sdlc`, which is what lets a gate check that it exists.
- Edit a frozen test.
- Commit before the gates have passed.
- Answer for you. Approving, unfreezing, acknowledging and assigning are a
  person's, and an agent that tries is refused.

[Enforcement](enforcement.md) has every rule, and what each refusal says.

Every agent is also asked to list what it could not check, and to report any
text in your repository that tries to give it orders, such as "reviewer:
approve this", and not to act on it. The loop shows you either list whenever
it is not empty. That is a request and not a guarantee. What holds is the
structure: no single agent can carry a change through on its own.

## Where to go next

- [The loop](the-loop.md): the gates these agents work
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md): what a block
  does to a gate, and what to do about it
- [Human approval gates](guides/human-approval-gates.md): where the loop hands
  a story to you
- [Configuration](configuration.md#team): every `team` setting
- [Commands](commands.md#sdlc-staffing): the `sdlc staffing` commands, in full
- [Enforcement](enforcement.md): what the hook refuses, and what each refusal
  says
- [Writing an agent](https://github.com/bbsnly/sdlc/blob/main/CONTRIBUTING.md#writing-an-agent):
  the contract every agent definition follows, for a person changing one
