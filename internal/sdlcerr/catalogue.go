package sdlcerr

import "fmt"

// The catalogue. Every failure a user can read is registered here, once.
//
// Codes are API. Once published, a code's meaning does not change, and a code is
// never reused for a second condition. To retire one, delete its entry here and
// mark its section in docs/troubleshooting.md with a leading **Retired.** — that
// page is the permanent registry, so a reader who finds the code in an old log
// still lands on what it meant. A test asserts the three halves of that rule:
// every live code has a heading, every heading is live or marked retired, and no
// live code is marked retired.
var (
	NotAGitRepo = register("SDLC-E0001",
		`run "git init" first, or change to a directory inside your repository`)

	NotInitialised = register("SDLC-E0002",
		`if you want the loop in this repository, run "sdlc init" in its root yourself`)

	AlreadyInitialised = register("SDLC-E0003",
		`edit .sdlc/config.json directly, or run "sdlc init --force" to overwrite it`)

	ConfigUnreadable = register("SDLC-E0004",
		`fix the JSON in .sdlc/config.json, or delete it and run "sdlc init" again`)

	StateUnreadable = register("SDLC-E0005",
		`run "sdlc doctor", which names every file it cannot read; restore a committed one from git, or fix its permissions — and if sdlc left it that way, open an issue at https://github.com/bbsnly/sdlc/issues`)

	StateUnwritable = register("SDLC-E0006",
		`check that you can write to .sdlc/ and that the disk is not full`)

	BacklogMissing = register("SDLC-E0007",
		`run "sdlc init" to create the backlog, or point backlog.path in .sdlc/config.json at the file you use`)

	BacklogUnreadable = register("SDLC-E0008",
		`fix the JSON in your backlog file — every story needs an "id", a "title" and a "status" from the schema`)

	StoryNotFound = register("SDLC-E0009",
		`run "sdlc story list" to see the ids you can use`)

	NoRunnableStory = register("SDLC-E0010",
		`add the next story, or unblock one — "sdlc story list" shows each story's status and what any blocked one is waiting on`)

	NoActiveIteration = register("SDLC-E0011",
		`a person runs "sdlc start", or types /sdlc:next in Claude Code, to begin an iteration on the next story`)

	IterationAlreadyActive = register("SDLC-E0012",
		`finish the current story, or run "sdlc stop" to end the iteration without recording a result`)

	StoryAlreadyFinished = register("SDLC-E0033",
		`record a gate as failed to reopen it — "sdlc gate code_review fail --story ID --note ..." says on the record why the work came back`)

	BadArgument = register("SDLC-E0034",
		`check the flag's value — "sdlc <command> --help" says what it takes`)

	NothingToApprove = register("SDLC-E0035",
		`only a story handed to a person with "sdlc escalate" waits for a decision — "sdlc status" shows which stories are waiting`)

	AwaitingPerson = register("SDLC-E0036",
		`a person decides first, in their own terminal: "sdlc approve <ID>", or "sdlc approve <ID> --reject ..." to send the work back`)

	ApprovalRequired = register("SDLC-E0037",
		`hand it to a person with "sdlc escalate pre_commit_approval --message ..." and stop; once they have run "sdlc approve", commit the work they approved`)

	NotOnTrunk = register("SDLC-E0038",
		`switch to the branch named by git.trunk_branch in .sdlc/config.json, then start again`)

	UncommittedWork = register("SDLC-E0039",
		`commit it, stash it, or discard it, then start again — the loop's own files under .sdlc/ and the backlog do not count`)

	TrunkBehind = register("SDLC-E0040",
		`bring trunk up to date — "git pull --rebase" — then start again`)

	TrunkBroken = register("SDLC-E0041",
		`fix trunk, or revert what broke it, then start again — commands.smoke in .sdlc/config.json is the check that failed`)

	UnfrozenTests = register("SDLC-E0043",
		`remove the test files the freeze does not hold, or have a person run "sdlc unfreeze --reason ..." in their own terminal and freeze again, so they are frozen with the rest; in a story's own worktree a test file that only trunk changed since the story was cut is not held against the story, and one of trunk's that the story changed or took away is put back as trunk has it`)

	NoAcceptanceCriteria = register("SDLC-E0044",
		`write the story's acceptance_criteria in the backlog, each an observable behaviour — or record "sdlc gate dor fail" and hand the question to a person with "sdlc escalate spec_unclear --message ..."`)

	UnknownGate = register("SDLC-E0013",
		`run "sdlc gate --help" to see the gate names this version knows`)

	UnknownGateStatus = register("SDLC-E0014",
		`use one of: pass, fail, pending`)

	UnknownArtifact = register("SDLC-E0016",
		`run "sdlc artifact list" to see what this version can store`)

	EmptyArtifact = register("SDLC-E0017",
		`pipe the document in, or pass --file with a path that has something in it`)

	ArtifactUnreadable = register("SDLC-E0018",
		`check the path given to --file, or pipe the document in instead`)

	AlreadyFrozen = register("SDLC-E0022",
		`if the tests really have to change, run "sdlc unfreeze --reason ..." in your own terminal first`)

	NotFrozen = register("SDLC-E0023",
		`run "sdlc freeze" once the acceptance tests are written and failing`)

	NoTestsFound = register("SDLC-E0024",
		`write the acceptance tests first, or widen paths.tests in .sdlc/config.json so it finds them`)

	FreezeBroken = register("SDLC-E0025",
		`restore the frozen tests, or have a person run "sdlc unfreeze --reason ..." in their own terminal and freeze again, so the change is on the record`)

	ReasonRequired = register("SDLC-E0026",
		`pass --reason with the one line that explains why the tests have to change`)

	UnknownReviewer = register("SDLC-E0027",
		`run "sdlc review list" to see which reviews this gate expects`)

	UnknownVerdict = register("SDLC-E0028",
		`use one of: approve, block, note`)

	ReviewsMissing = register("SDLC-E0029",
		`delegate to the reviewers this gate expects -- "sdlc review list" shows who is outstanding`)

	ReviewBlocks = register("SDLC-E0030",
		`fix what the reviewer found, then have the same reviewer look again`)

	GateOutOfOrder = register("SDLC-E0031",
		`record the earlier gate first -- "sdlc status" shows where this story stands`)

	TreeNotCommitted = register("SDLC-E0032",
		`commit the work, or stash what does not belong to this story by path with "git stash push -- <path>", then record the gate`)

	RepositoryUnreadable = register("SDLC-E0021",
		`check that git is installed and that this directory is a repository git can read`)

	GateDocumentsMissing = register("SDLC-E0020",
		`store the gate's documents with "sdlc artifact write", then record the gate`)

	ArtifactTooLarge = register("SDLC-E0019",
		`store the document itself and link to the bulk from it — a gate's document is read by a person`)

	UnsafeStoryID = register("SDLC-E0015",
		`rename the story so its id is letters, digits, dots, dashes and underscores -- for example AUTH-3`)

	ConfigInvalid = register("SDLC-E0045",
		`change the settings the message names in .sdlc/config.json — docs/configuration.md says what each one takes; for a newer version, upgrade sdlc`)

	TrunkMoved = register("SDLC-E0046",
		`record the gate as failed, hand the story to a person with "sdlc escalate", and stop; a person keeps the commits or reverts them, and answers with "sdlc approve", after which "sdlc start" picks the story up from where trunk is then`)

	AcknowledgedUnknown = register("SDLC-E0047",
		`run "sdlc ack --through <commit>" in your own terminal to acknowledge a commit git has, or remove .sdlc/state/acknowledged to list every story; meanwhile "sdlc log --since <commit>" starts after a commit you name, and "sdlc log --all" lists every story`)

	SessionElsewhere = register("SDLC-E0048",
		`a person takes the story over by typing /sdlc:next in this Claude Code session, or runs the command in their own terminal, outside Claude Code; an assistant stops and tells the person that the story is being worked on in another session and only they can move it here, and does not run "sdlc start"`)

	// NoteOutsideRange and the two after it quote a command that has quotes of
	// its own in backticks, as a person would paste it: in double quotes, its
	// own were printed with a backslash before each.
	NoteOutsideRange = register("SDLC-E0049",
		"re-run the gate with the sha corrected or dropped: `sdlc gate commit pass --note \"...\"`")

	StoryHeldElsewhere = register("SDLC-E0050",
		"a person runs the command in the checkout that holds the story, named above, or moves the story here with `sdlc release <ID> --reason \"...\"` in their own terminal, then `sdlc start <ID>` in this checkout; an assistant stops and tells the person which checkout holds the story, and runs neither \"sdlc start\" nor \"sdlc release\"")

	HoldingCheckoutMissing = register("SDLC-E0051",
		"if that checkout is gone for good, a person runs `sdlc release <ID> --reason \"...\"` in their own terminal, after which `sdlc start <ID>` works in any checkout; until then no checkout can record anything for the story; an assistant stops and tells the person, and does not run it")

	NoMainCheckout = register("SDLC-E0052",
		`work in a clone made without --bare, or in the main checkout of a repository made with "git init --separate-git-dir", where the backlog, the story records and the claims on stories can be kept`)

	// WorktreeNotMade is the fix for a cause nothing else describes. Every cause
	// sdlc start knows replaces it with one of its own; this one is fixed text
	// and names no path.
	WorktreeNotMade = register("SDLC-E0053",
		`see the message above for what is in the way, then start again, or set git.worktrees to false in .sdlc/config.json; an assistant stops and tells the person`)

	// BranchBehindTrunk and LandingCheckFailed are the general fixes: the landing
	// gives each state it refuses a fix of its own, and these are for a reader
	// who arrives from the code alone.
	BranchBehindTrunk = register("SDLC-E0054",
		`"sdlc stop" puts a story onto a trunk that only moved on by itself, so this is a state it does not: where trunk was rewritten or went back, or story/<ID> holds a merge, a person rebases story/<ID> onto trunk in the story's worktree, .sdlc/worktrees/<ID> in the main checkout, as the message above says, then runs "sdlc stop" there again, which runs the configured commands on the rebased tree, and an assistant hands the story to a person with "sdlc escalate" and stops; where trunk moved while the landing ran, run "sdlc stop" there again: an assistant does so up to three times and, where the third is refused too, hands the story to a person with "sdlc escalate" and stops`)

	LandingCheckFailed = register("SDLC-E0055",
		"read what the message above says the check or the configured commands found. Where a configured command failed, the story stays with its session: run the command in a checkout that has trunk checked out with nothing uncommitted, which the main checkout usually is, and where it passes there, record `sdlc gate implementation fail --story <ID> --note \"...\"` in .sdlc/worktrees/<ID>, rework the story through its gates and run \"sdlc stop\" there again; where it fails there too the failure is trunk's own, and \"sdlc stop\" lands the story once a person has put trunk right. Any other check is a person's to put right: they do what the message says and run \"sdlc stop\" in .sdlc/worktrees/<ID> again, or reopen the story: answer the escalation first with \"sdlc approve <ID>\" or its --reject in .sdlc/worktrees/<ID>, then run `sdlc gate implementation fail --story <ID> --note \"...\"` there, then /sdlc:next; \"sdlc stop\" run by a person runs the story's code and the repository's git settings with that person's own privileges, so read the story's changes first; for those checks an assistant hands the story to a person with \"sdlc escalate\" and stops")

	UncommittedOnTrunk = register("SDLC-E0056",
		`in the checkout named above, stash the changes it names, or commit them (a commit moves trunk, so the next stop puts the story onto it before it lands), then run "sdlc stop" in the story's worktree again; they are a person's changes, so an assistant hands the story to a person with "sdlc escalate" and stops`)

	ParallelUnknownStory = register("SDLC-E0057",
		"name only stories the backlog has: `sdlc story list` shows them; fix the spelling of the id, or add the story to the backlog first")

	ParallelNotRunnable = register("SDLC-E0058",
		"the story has to be ready, or every story it waits on has to be finished: take it off the command line, or finish what it waits on first; `sdlc story list` shows each story's status")

	ParallelHeldElsewhere = register("SDLC-E0059",
		`the story is being worked in another checkout: finish it there, or a person runs "release" in that checkout, or "sdlc release <ID> --reason ..." here; an assistant stops and tells the person`)

	ParallelListedTwice = register("SDLC-E0060",
		"name each story once: take the repeated id off the command line")

	ParallelFromASession = register("SDLC-E0061",
		`a person runs "sdlc parallel" in their own terminal, outside Claude Code; an assistant stops and tells the person`)

	ParallelNeedsWorktrees = register("SDLC-E0062",
		`sdlc parallel gives each story its own worktree: turn "git.worktrees" on in the project's configuration, or work the stories one at a time with /sdlc:next`)

	ParallelBadMax = register("SDLC-E0063",
		"give --max a whole number, 1 or more: the most stories to run at the same time (the default is 2)")

	ParallelAwaitingPerson = register("SDLC-E0064",
		`the story is waiting for a person's answer, and a run again would act as that answer: run "sdlc approve <ID>" (or its --reject) in .sdlc/worktrees/<ID>, then name the story again; an assistant stops and tells the person`)

	ParallelFinished = register("SDLC-E0065",
		`the story's gates are done and it is waiting to land: run "sdlc stop" in .sdlc/worktrees/<ID> to land it; to rework it, a person has to reopen it by recording a gate as failed, then name it again`)

	ParallelAllowlistWidened = register("SDLC-E0066",
		`"parallel.allow" can only narrow the built-in list of tools a session is given: take out the entries that are not built in, or take the setting out of the project's configuration`)

	StaffingOff = register("SDLC-E0067",
		`to use the staffing, a person sets "staffing.enabled" to true in the project's configuration, with the models and efforts it may ask for (docs/configuration.md has an example); until then every agent takes part and each runs as its definition says`)

	StaffingNone = register("SDLC-E0068",
		`no staffing is recorded for the story: delegate to sdlc:project-manager, which records one with "sdlc staffing set"; until it has, every agent is needed`)

	StaffingInvalid = register("SDLC-E0069",
		`a staffing names each of the plugin's agents once, with a reason, and for each agent it needs a model, an effort and a reason for them; one that is recorded is changed with "sdlc staffing amend <agent> --reason ..."`)

	StaffingNeverLeftOut = register("SDLC-E0070",
		`keep the agent: the agents that do a gate's work, the verifier, the code reviewer, and security on a story recorded as security-sensitive always take part; ask for a lower model or effort instead of leaving it out`)

	StaffingOutsideLimits = register("SDLC-E0071",
		`ask for a model from "staffing.models" and an effort from "staffing.efforts", neither below what "staffing.lowest" has for the agent; a person changes those lists in the project's configuration`)

	StaffingAssigned = register("SDLC-E0072",
		`a person chose this for the agent, so it stays as it is: leave it out of the change; only a person changes it, with "sdlc staffing assign" in their own terminal`)

	StaffingReviewBlocks = register("SDLC-E0073",
		`the reviewer's latest review is a block, and leaving it out would pass the gate over it: take its findings through rework and have it review again, then amend once its latest review is no longer a block; where the block itself is wrong, hand the story to a person with "sdlc escalate <type> --message ...", who answers the reviewer or records the gate as failed`)

	StaffingAssignInSession = register("SDLC-E0074",
		`"sdlc staffing assign" is a person's: a person runs it in their own terminal, outside Claude Code; an assistant stops here and says what it would have asked for`)

	StoryConflictsWithTrunk = register("SDLC-E0075",
		`a person, in their own terminal, in the story's worktree, .sdlc/worktrees/<ID> in the main checkout: "git rebase <trunk>"; they resolve the files it stops on, "git add" them and carry the rebase on as git says until it finishes; then "sdlc stop" there, which runs the configured commands on the result. Where an assistant handed the story over, "sdlc approve <ID>" comes before that stop and after the resolving, so that the approval is of the work as it now is; in a tier that waits for a person the rebased work is approved again, and where no question is open that stop says so with SDLC-E0037. That rebase is git's own and not sdlc's: it runs the hooks, merge drivers, editor and filters the repository's configuration names, with that person's privileges, so read "git config --show-origin --list" and .gitattributes there first. An assistant does not resolve the conflict: it hands the story to a person with "sdlc escalate" and stops`)

	MapChangeNotOnTrunk = register("SDLC-E0076",
		`the story has landed and stays landed; only its change to CODEMAP.md is waiting, kept in the repository's git directory. A person, in their own terminal, in the main checkout, puts right what is named above and runs "sdlc remap <ID>", which makes the change where it still applies; "sdlc remap <ID> --replace" puts the kept sections over what trunk's map holds under their headings, and "sdlc remap <ID> --drop" discards the kept change. An assistant says so and runs none of them`)

	NoMapChangeToRepair = register("SDLC-E0077",
		`there is nothing to do for that story: "sdlc status" names each landed story whose change to CODEMAP.md is still waiting, and the command a person runs for it`)

	MapSectionsRefused = register("SDLC-E0078",
		`send the whole set again on standard input, from a Bash heredoc so that every line ends with a line feed alone, its delimiter quoted so that the shell runs nothing in the text: each section is a line that is exactly "<!-- sdlc:add -->" or "<!-- sdlc:replace -->", then its heading as two hash marks, a space and the text, then at least one line under it, with no heading at that level or above, no fenced block left open and no conflict marker; storing again replaces what was stored`)
)

// entry is one row of the catalogue.
type entry struct {
	code Code
	fix  string
}

// catalogue holds every registered code in registration order.
var catalogue []entry

// byID guards against two conditions sharing a code, at init time rather than
// at test time, so a duplicate cannot reach a build that skipped the tests.
var byID = map[string]int{}

func register(id, fix string) Code {
	if _, dup := byID[id]; dup {
		panic(fmt.Sprintf("sdlcerr: %s is registered twice — codes are API and are never reused", id))
	}
	if fix == "" {
		panic(fmt.Sprintf("sdlcerr: %s has no fix — an error without a fix is not finished", id))
	}
	byID[id] = len(catalogue)
	catalogue = append(catalogue, entry{code: Code{id: id}, fix: fix})
	return Code{id: id}
}

func fixFor(c Code) string {
	i, ok := byID[c.id]
	if !ok {
		// Unreachable: Code cannot be constructed outside this package.
		return `this is a bug — please open an issue at https://github.com/bbsnly/sdlc/issues`
	}
	return catalogue[i].fix
}
