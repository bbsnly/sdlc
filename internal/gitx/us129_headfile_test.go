package gitx

// US-129 AC-2 for Head and Branch, which read the checkout's own HEAD file
// where the base asked git. Git trims the white space at the end of the file
// and after "ref:", so `ref:  refs/heads/main`, a trailing space or tab and a
// CRLF all read as main to rev-parse and symbolic-ref. Read more leniently
// than that, a trailing space made Head say no commits and Branch "main  ",
// and `start` then refused as not on trunk (US-129 verification, F-2; red
// team V23). Each answer here is what the base's git calls give in the same
// repository, with the invocation's memo and session and without them.

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// us129Try is git args in dir, and its output with the line ending taken
// off, or the error.
func us129Try(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
}

// us129BaseBranch is the base's Branch: symbolic-ref, and detached where
// rev-parse calls HEAD HEAD.
func us129BaseBranch(t *testing.T, root string) (string, bool) {
	t.Helper()
	if ref, err := us129Try(t, root, "symbolic-ref", "-q", "HEAD"); err == nil {
		name, _ := strings.CutPrefix(ref, "refs/heads/")
		if !strings.HasPrefix(ref, "refs/heads/") {
			name = ""
		}
		return name, true
	}
	if head, err := us129Try(t, root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && head == "HEAD" {
		return "", true
	}
	return "", false
}

// us129BaseHead is the base's Head: rev-parse, and no commits yet where that
// fails on a branch.
func us129BaseHead(t *testing.T, root string) (string, bool) {
	t.Helper()
	if hash, err := us129Try(t, root, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		return hash, true
	}
	if _, ok := us129BaseBranch(t, root); ok {
		return NoCommits, true
	}
	return "", false
}

func TestAC2HeadAndBranchReadEveryHEADFileGitReadsAsGitReadsIt(t *testing.T) {
	root := t.TempDir()
	us129Run(t, root, nil, "", "init", "--quiet", "--initial-branch=main")
	us129Write(t, root, "a.txt", "a\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "one")
	us129Run(t, root, nil, "", "tag", "v1")
	commit := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	shapes := map[string]string{
		"as git writes it":                      "ref: refs/heads/main\n",
		"a space at the end":                    "ref: refs/heads/main \n",
		"two spaces at the end":                 "ref: refs/heads/main  \n",
		"a tab at the end":                      "ref: refs/heads/main\t\n",
		"a CRLF":                                "ref: refs/heads/main\r\n",
		"no line ending":                        "ref: refs/heads/main",
		"two line endings":                      "ref: refs/heads/main\n\n",
		"two spaces after ref:":                 "ref:  refs/heads/main\n",
		"a tab after ref:":                      "ref:\trefs/heads/main\n",
		"no space after ref:":                   "ref:refs/heads/main\n",
		"an unborn branch and a space":          "ref: refs/heads/nothing \n",
		"an unborn branch and a CRLF":           "ref: refs/heads/nothing\r\n",
		"a tag and a space":                     "ref: refs/tags/v1 \n",
		"a detached commit":                     commit + "\n",
		"a detached commit and a space":         commit + " \n",
		"a detached commit and a CRLF":          commit + "\r\n",
		"a detached commit and no line ending":  commit,
		"a detached commit and two spaces":      commit + "  \n",
		"a detached commit and a tab":           commit + "\t\n",
		"a branch in another case and a space":  "ref: refs/heads/Main \n",
		"a branch name with a space in it":      "ref: refs/heads/ma in\n",
		"a branch name after two spaces, a tab": "ref:  refs/heads/main\t\n",
	}
	for name, ref := range us129RefusedRefs("refs/heads/main") {
		shapes["a ref git refuses: "+name] = "ref: " + ref + "\n"
	}
	headPath := filepath.Join(root, ".git", "HEAD")
	for name, body := range shapes {
		t.Run("AC-2 "+name, func(t *testing.T) {
			if err := os.WriteFile(headPath, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0o644) })
			wantHead, headOK := us129BaseHead(t, root)
			wantBranch, branchOK := us129BaseBranch(t, root)
			for _, memo := range []bool{false, true} {
				ctx := context.Background()
				if memo {
					ctx = WithMemo(ctx)
				}
				head, headErr := Head(ctx, root)
				branch, branchErr := Branch(ctx, root)
				Close(ctx)
				if (headErr == nil) != headOK || head != wantHead {
					t.Errorf("AC-2 with HEAD %q, memo %v: Head is %q (%v), git gives %q (answered %v)",
						body, memo, head, headErr, wantHead, headOK)
				}
				if (branchErr == nil) != branchOK || branch != wantBranch {
					t.Errorf("AC-2 with HEAD %q, memo %v: Branch is %q (%v), git gives %q (answered %v)",
						body, memo, branch, branchErr, wantBranch, branchOK)
				}
			}
		})
	}
}

// The same in a linked worktree, whose HEAD is in a git directory of its own
// and which the session, started for the repository, never reads HEAD for.
func TestAC2HeadAndBranchReadALinkedWorktreesHEADFileAsGitReadsIt(t *testing.T) {
	main := t.TempDir()
	us129Run(t, main, nil, "", "init", "--quiet", "--initial-branch=main")
	us129Write(t, main, "a.txt", "a\n")
	us129Run(t, main, nil, "", "add", "-A")
	us129Run(t, main, nil, "", "commit", "--quiet", "-m", "one")
	wt := filepath.Join(t.TempDir(), "story")
	us129Run(t, main, nil, "", "worktree", "add", "--quiet", "-b", "story/US-001", wt, "main")
	gitDir, _, err := GitDirOf(wt)
	if err != nil {
		t.Fatal(err)
	}
	shapes := map[string]string{
		"a space at the end":               "ref: refs/heads/story/US-001 \n",
		"two spaces after ref: and a CRLF": "ref:  refs/heads/story/US-001\r\n",
		"a tab at the end":                 "ref: refs/heads/story/US-001\t\n",
	}
	for name, ref := range us129RefusedRefs("refs/heads/story/US-001") {
		shapes["a ref git refuses: "+name] = "ref: " + ref + "\n"
	}
	for name, body := range shapes {
		t.Run("AC-2 "+name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			wantHead, headOK := us129BaseHead(t, wt)
			wantBranch, branchOK := us129BaseBranch(t, wt)
			ctx := WithMemo(context.Background())
			defer Close(ctx)
			// The main checkout first, so that the session is the one started
			// there.
			if _, err := Head(ctx, main); err != nil {
				t.Fatal(err)
			}
			head, headErr := Head(ctx, wt)
			branch, branchErr := Branch(ctx, wt)
			if (headErr == nil) != headOK || head != wantHead || (branchErr == nil) != branchOK || branch != wantBranch {
				t.Errorf("AC-2 with the worktree's HEAD %q: Head %q (%v), Branch %q (%v); git gives %q (answered %v) and %q (answered %v)",
					body, head, headErr, branch, branchErr, wantHead, headOK, wantBranch, branchOK)
			}
		})
	}
}

// us129RefusedRefs is ref spelt in each way git's ref-name rules
// (check-ref-format) refuse, by what is wrong with it. Each still names a
// branch to a reader that only cuts refs/heads/ off: `ref:
// refs/heads/../heads/main` read as the branch "../heads/main", where
// symbolic-ref fails and the base's Branch said so (US-129 verification 2,
// F-13).
func us129RefusedRefs(ref string) map[string]string {
	dir, last := path.Split(ref)
	return map[string]string{
		"a slash at the end":                      ref + "/",
		"a dot-dot component":                     "refs/heads/../" + strings.TrimPrefix(ref, "refs/"),
		"two dots":                                ref + "..x",
		"two slashes":                             dir + "/" + last,
		"a component starting with a dot":         dir + "." + last,
		"a component starting with a dot, deeper": ref + "/.x",
		"a .lock at the end":                      ref + ".lock",
		"a component ending in .lock":             ref + ".lock/x",
		"a dot at the end":                        ref + ".",
		"an at-brace":                             ref + "@{1}",
		"a lone at":                               "refs/heads/@",
		"a tilde":                                 ref + "~1",
		"a caret":                                 ref + "^0",
		"a colon":                                 ref + ":x",
		"a question mark":                         ref + "?",
		"an asterisk":                             ref + "*",
		"an open bracket":                         ref + "[x",
		"a backslash":                             ref + "\\x",
		"a control byte":                          ref + "\x01x",
		"a delete byte":                           ref + "\x7fx",
		"no name after refs/heads/":               "refs/heads/",
		"refs/heads alone":                        "refs/heads",
		"a tag side that climbs out":              "refs/tags/../" + strings.TrimPrefix(ref, "refs/"),
	}
}
