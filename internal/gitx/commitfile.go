package gitx

import (
	"context"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// commitFileSettings are handed to git outright for the commit: nothing is
// signed, whatever the repository configures. A signing program named there is
// one a session can write, and it would run as whoever ran the stop.
var commitFileSettings = []string{"commit.gpgSign=false"}

// CommitFile makes a commit whose one parent is parent, a commit's full hash,
// and whose tree is parent's with the file at path holding content as a plain
// file. path is one name at the root of the repository. It returns the new
// commit's full hash, moves no ref, and reads and writes no index and no
// working tree: the caller moves trunk with FastForward.
//
// What reaches trunk through it is text a model wrote and no reviewer read, so
// it is held to the result and not to the request. The tree is built first and
// compared with the parent's: where it differs at any other path, or at none,
// there is no commit, and the error names the paths. Every call is sealed: no
// hook runs, no filter sees the content, nothing is signed, and with no
// committer git fails rather than make one up.
func CommitFile(ctx context.Context, root, parent, path string, content []byte, message string) (string, error) {
	if err := fullHashes(parent); err != nil {
		return "", err
	}
	if err := rootName(path); err != nil {
		return "", err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "", cannotCommit(path, "the commit has no message")
	}

	p := plumbing{ctx: ctx, root: root}
	kind := p.git(nil, nil, "cat-file", "-t", parent)
	listing := p.git(nil, nil, "ls-tree", "-z", "--full-tree", parent)
	if p.err != nil {
		return "", p.err
	}
	if kind != "commit" {
		return "", cannotCommit(path, parent[:12]+" is a "+kind+", and the parent has to be a commit")
	}
	entries, at, err := treeWith(strings.TrimRight(listing, "\x00"), path)
	if err != nil {
		return "", err
	}
	// The blob is written only once the listing has been checked, and its
	// hash goes into the one entry: put in by replacing a marker across the
	// listing, it renamed a file whose own name held the marker.
	blob := p.git(nil, content, "hash-object", "-w", "--stdin", "--no-filters")
	entries[at] = "100644 blob " + blob + "\t" + path
	built := p.git(nil, []byte(strings.Join(entries, "\x00")+"\x00"), "mktree", "-z")
	if p.err != nil {
		return "", p.err
	}

	changed, err := ChangedBetween(ctx, root, parent, built)
	if err != nil {
		return "", err
	}
	var others []string
	for _, c := range changed {
		if c != path {
			others = append(others, sdlcerr.QuotePath(c))
		}
	}
	switch {
	case len(others) > 0:
		return "", cannotCommit(path, "putting a file there would also change "+strings.Join(others, ", ")+
			", and the commit may change that one path only")
	case len(changed) == 0:
		return "", cannotCommit(path, "the commit "+parent[:12]+" already holds it exactly so, and a commit that changes nothing is not made")
	}

	commit := p.git(commitFileSettings, []byte(message+"\n"), "commit-tree", built, "-p", parent, "--no-gpg-sign", "-F", "-")
	return commit, p.err
}

// treeWith is the entries of a commit's root listing, as `ls-tree -z` printed
// it, and which of them is the file at path: the caller writes that one. It
// refuses an entry there that is a link or a submodule, and a name beside it
// that differs only by case: on a disk that folds case the two are one file in
// every checkout, and the commit would leave trunk with both.
func treeWith(listing, path string) (entries []string, at int, err error) {
	at = -1
	for line := range strings.SplitSeq(listing, "\x00") {
		meta, name, _ := strings.Cut(line, "\t")
		switch {
		case line == "":
			continue
		case name == path:
			if mode, _, _ := strings.Cut(meta, " "); mode == "120000" || mode == "160000" {
				return nil, 0, cannotCommit(path, "it is a link or a submodule there (mode "+mode+"), not a plain file to put text in")
			}
			at = len(entries)
		case strings.EqualFold(name, path):
			return nil, 0, cannotCommit(path, "the commit has "+sdlcerr.QuotePath(name)+
				", which is the same file as "+path+" wherever the disk does not tell upper case from lower")
		}
		entries = append(entries, line)
	}
	if at < 0 {
		at = len(entries)
		entries = append(entries, "")
	}
	return entries, at, nil
}

// rootName refuses a path that is not one plain name at the root of the
// repository. The one caller passes a constant; it is checked all the same,
// because the path goes into a tree git is asked to make.
func rootName(path string) error {
	if err := notAnOption(msgAPath, path); err != nil {
		return err
	}
	if path == "." || path == ".." || strings.EqualFold(path, ".git") || strings.ContainsAny(path, "/\\\n\x00") {
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(path)+" cannot be committed here",
			"only one plain file name at the root of the repository is: no directory, no parent, nothing of git's own")
	}
	return nil
}

func cannotCommit(path, why string) error {
	return sdlcerr.New(sdlcerr.RepositoryUnreadable, sdlcerr.QuotePath(path)+" was not committed", why)
}

// plumbing runs sealed git calls one after another and keeps the first
// failure: after one, the calls that follow are not made.
type plumbing struct {
	ctx  context.Context //nolint:containedctx // lives for one CommitFile call
	root string
	err  error
}

func (p *plumbing) git(settings []string, stdin []byte, args ...string) string {
	if p.err != nil {
		return ""
	}
	out, err := sealedCall(p.ctx, p.root, settings, stdin, nil, args...)
	p.err = err
	return strings.TrimSpace(string(out))
}
