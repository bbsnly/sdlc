package checklegs

import (
	"strings"
	"testing"
)

// awsLikeKey is the exact string shape ANALYSIS.md and THREATS.md used to
// reproduce gitleaks' aws-access-token rule firing on a gitignored file:
// AKIA followed by sixteen varied alphanumerics. A low-entropy run of one
// repeated character (tried first while building this fixture) is filtered
// out by the rule's own entropy check and never reported at all, which
// would have made this test pass for the wrong reason.
//
// The key is split in the source because gitleaks reports the literal in this
// very file, which made `./task secrets` fail on the committed tree; the
// planted files still hold the full twenty-character key at run time.
const awsLikeKey = "AKIA" + "ABCDEFGHIJKLMNOP"

// TestAC2SecretsLegIgnoresAGitignoredSecretButStillCatchesAnUntrackedOne
// plants two files shaped alike -- each holding a string gitleaks' own
// aws-access-token rule reports -- one gitignored, one merely not yet
// `git add`ed. AC-2, resolved in ANALYSIS.md from its own WHEN clause
// ("no uncommitted change") together with this codebase's definition of
// "uncommitted" (internal/gitx/landing.go:385-399), means no leg may fail on
// a gitignored file; it says nothing about the untracked-and-not-yet-added
// case, which THREATS.md names as a control this story must not weaken
// (gitleaks dir's whole purpose beyond `gitleaks git`, Taskfile.yml:465-468).
//
// It fails today on its gitignored assertion: `gitleaks dir --help` (pinned
// v8.30.1) offers no flag that reads .gitignore, so the gitignored file is
// reported exactly as THREATS.md reproduced it. Its untracked assertion
// already holds today -- that half is a non-regression pin on the control
// THREATS.md flags, not new criterion coverage, carried in this same test so
// a fix cannot satisfy one half by weakening the other.
func TestAC2SecretsLegIgnoresAGitignoredSecretButStillCatchesAnUntrackedOne(t *testing.T) {
	requireGoModule(t, "github.com/zricethezav/gitleaks/v8", pinnedVersion(t, "GITLEAKS_VERSION"),
		"run `./task secrets` once with network access to fetch it")

	root := t.TempDir()
	gitInit(t, root)
	write(t, root, ".gitignore", "/ignored/\n")
	write(t, root, "ignored/secret.txt", "token="+awsLikeKey+"\n")
	write(t, root, "pending-secret.txt", "token="+awsLikeKey+"\n")
	// Neither secret file is staged: the gitignored one cannot be (git
	// refuses to add a path .gitignore names), and the other must stay
	// exactly "untracked, not yet added" for this test to mean what it says.

	out, err := runTask(t, root, "secrets")

	// Non-regression pin: the control THREATS.md says this story must not
	// weaken. Already true today.
	if err == nil {
		t.Fatalf("secrets must still fail: pending-secret.txt is untracked and not "+
			"ignored, exactly the case gitleaks dir exists to catch before a commit:\n%s", out)
	}
	if !strings.Contains(out, "pending-secret.txt") {
		t.Errorf("secrets' output never named pending-secret.txt; the untracked, "+
			"not-yet-added case must still be caught:\n%s", out)
	}

	// AC-2 itself: fails today.
	if strings.Contains(out, "ignored/secret.txt") {
		t.Errorf("secrets' output named ignored/secret.txt, which .gitignore names and "+
			"which AC-2 (read via its own WHEN clause in ANALYSIS.md) says no leg may "+
			"fail on:\n%s", out)
	}
}
