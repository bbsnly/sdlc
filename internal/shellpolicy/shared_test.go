package shellpolicy

// US-029.1, steps 13(c), 13(e) and 13(f). The hook says where a word lands
// among the repository's other checkouts through State.Shared; these give it a
// fixed answer and hold the rules to the plan: another checkout's backlog,
// records, configuration, loop state and git directory are refused to the
// shell and its files are not; a commit there is refused whatever the gates
// say; and releasing a story is a person's decision.

import (
	"strings"
	"testing"
)

// Three places outside the project /work/project: /work/main is another
// checkout of its repository, /work/own/gitdir the project's own git
// directory, and everything else is nothing of the repository's.
func sharedLandings(word string) []Landing {
	if rest, ok := strings.CutPrefix(word, "/work/own/gitdir"); ok && (rest == "" || rest[0] == '/') {
		return []Landing{{Rel: strings.TrimPrefix(".git"+rest, "/"), Own: true}}
	}
	if word == "/work/main" {
		return []Landing{{Rel: "."}}
	}
	if rest, ok := strings.CutPrefix(word, "/work/main/"); ok {
		return []Landing{{Rel: rest}}
	}
	return nil
}

// withShared is s with the project at /work/project and its repository's
// other checkouts answered by landings.
func withShared(s State, landings func(string) []Landing) State {
	s.Backlog = "user_stories.json"
	s.Resolve = func(word string) string {
		if rest, ok := cutProject(word); ok {
			return rest
		}
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	s.Shared = landings
	return s
}

// T2, R2-4 and 13(c): in another checkout the shell may not write what the
// loop keeps, by any name the rules know it by.
func TestAC7TheShellIsRefusedWhatAnotherCheckoutKeepsAndNotItsOtherFiles(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, tc := range []struct{ command, rule string }{
		{"echo '{}' > /work/main/.sdlc/stories/A-1/gate-record.json", "loop-state-through-the-tool"},
		{"rm -rf /work/main/.sdlc/stories", "loop-state-through-the-tool"},
		{"echo x > /work/main/.sdlc/state/active", "loop-state-through-the-tool"},
		{"echo '{}' > /work/main/.sdlc/config.json", "loop-state-through-the-tool"},
		{"echo '{}' > /work/main/user_stories.json", "protected-path-through-the-tool"},
		{"echo x >> /work/main/CLAUDE.md", "protected-path-through-the-tool"},
		{"cp evil.json /work/main/.claude/settings.json", "protected-path-through-the-tool"},
	} {
		refused(t, tc.command, s, tc.rule)
	}
	f := refused(t, "echo '{}' > /work/main/.sdlc/config.json", s, "loop-state-through-the-tool")
	if !strings.Contains(f.Reason, "another checkout") {
		t.Errorf("the refusal of another checkout's configuration does not say whose it is: %s", f.Reason)
	}
	if f, ok := inspect(t, "rm /work/main/.git/sdlc/claims/A-1.json", s); !ok {
		t.Error("a claim in the common git directory was removed by the shell")
	} else if f.Rule == "" {
		t.Errorf("finding = %+v", f)
	}

	for _, command := range []string{
		"echo x > /work/main/src/app.go",
		"cat /work/main/.sdlc/stories/A-1/gate-record.json",
		"echo x > /work/elsewhere/.sdlc/state/active",
	} {
		allowed(t, command, s)
	}
}

// 13(c), human-advocate R2-2: where the checkouts cannot be told apart, every
// word outside the project that might be the loop's evidence is refused by
// name, with a reason that says so and sends the reader to sdlc doctor.
func TestT12AnUnplacedWordIsRefusedByNameWithItsOwnReason(t *testing.T) {
	s := withShared(ready, func(word string) []Landing {
		if isAbsolute(word) {
			return []Landing{{Rel: word}}
		}
		return nil
	})
	f := refused(t, "echo '{}' > /srv/anywhere/.sdlc/stories/A-1/gate-record.json", s, "loop-state-through-the-tool")
	if !strings.Contains(f.Reason, "could not be found") || !strings.Contains(f.Route, "sdlc doctor") {
		t.Errorf("the unplaced refusal does not say why, or where to look: %+v", f)
	}
	allowed(t, "echo x > /srv/anywhere/src/app.go", s)
}

// 13(f), red-team R2-1: the gates cover the checkout the story is held in, so a
// commit in another checkout is refused whatever they say, by its git
// directory or its work tree, however either is spelled.
func TestT2ACommitInAnotherCheckoutIsRefusedWhateverTheGatesSay(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		"git -C /work/main commit -m x",
		"git --git-dir=/work/main/.git commit -m x",
		"git --git-dir /work/main/.git commit -m x",
		"git --work-tree=/work/main commit -m x",
		"git --work-tree /work/main commit -m x",
		"git -c user.name=x --work-tree=/work/main commit -m x",
		"git -C /work --work-tree=main commit -m x",
		"git --git-dir=/work/own/gitdir --work-tree=/work/main commit -m x",
		"git --work-tree=/work/main merge side",
	} {
		refused(t, command, s, "commit-in-another-checkout")
	}
}

// A linked worktree keeps its git directory outside itself, and a commit
// through it into its own work tree is its own: it meets the ordinary gate.
func TestT2ACommitThroughTheProjectsOwnGitDirectoryMeetsTheCommitGate(t *testing.T) {
	allowed(t, "git --git-dir=/work/own/gitdir commit -m x", withShared(ready, sharedLandings))
	refused(t, "git --git-dir=/work/own/gitdir commit -m x", withShared(notReady, sharedLandings), "commit-gate")
	refused(t, "git commit -m x", withShared(notReady, sharedLandings), "commit-gate")
	allowed(t, "git commit -m x", withShared(ready, sharedLandings))
}

// AC-8 and 13(e): the command that lets a story go is a person's, in every
// spelling a session could reach it by.
func TestAC8ReleasingAStoryIsRefusedToASession(t *testing.T) {
	for _, command := range []string{
		`sdlc release US-001 --reason "moved"`,
		`./sdlc release US-001 --reason moved`,
		`/usr/local/bin/sdlc release US-001 --reason moved`,
		`env sdlc release US-001 --reason moved`,
		`sdlc --json release US-001 --reason moved`,
		`bash -c 'sdlc release US-001 --reason moved'`,
		`echo US-001 | xargs sdlc release --reason moved`,
	} {
		refused(t, command, ready, "release-is-a-human-decision")
		f, ok := HumanDecisions(command, false, func(string) bool { return false })
		if !ok || f.Rule != "release-is-a-human-decision" {
			t.Errorf("HumanDecisions(%q) = %+v, %v; want it refused to a session that holds nothing", command, f, ok)
		}
	}
	allowed(t, "sdlc status", ready)
}
