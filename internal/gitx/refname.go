package gitx

import (
	"context"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// refnameRule is the rule of git's check-ref-format that name breaks, as the
// end of a sentence that begins "git takes no ref name that", and "" for a name
// git takes as a ref. It holds to what check-ref-format with no option does,
// which `git help check-ref-format` documents: at least two components, none
// empty, none starting with a dot or ending in ".lock", no "..", no "@{", no
// control character, space, "~", "^", ":", "?", "*", "[" or backslash, no end
// in a dot, and not "@" alone. Bytes from 0x80 up are taken, as git takes
// them, whatever encoding they are in.
//
// It is git's rule asked without a process: BranchTip asked git once for
// every branch it read, the landing reads several branches for each check, and
// a run of the cli tests started 7,000 check-ref-format processes, on systems
// where a process costs milliseconds. A NUL is refused, as git could not be
// handed one on its command line at all.
func refnameRule(name string) string {
	if name == "@" {
		return `is "@" alone`
	}
	components := 0
	for component := range strings.SplitSeq(name, "/") {
		if rule := componentRule(component); rule != "" {
			return rule
		}
		components++
	}
	switch {
	case strings.HasSuffix(name, "."):
		return "ends in a dot"
	case components < 2:
		return "has only one component"
	}
	return ""
}

// componentRule is the rule one component of a ref name breaks, as
// refnameRule words it, and "" for one git takes.
func componentRule(c string) string {
	switch {
	case c == "":
		return "has an empty component, as a slash at either end or two slashes together make"
	case c[0] == '.':
		return "has a component that starts with a dot"
	case strings.HasSuffix(c, ".lock"):
		return `has a component that ends in ".lock"`
	case strings.Contains(c, ".."):
		return `holds ".."`
	case strings.Contains(c, "@{"):
		return `holds "@{"`
	}
	for i := range len(c) {
		if b := c[i]; b < 0x20 || b == 0x7f || strings.IndexByte(" ~^:?*[\\", b) >= 0 {
			return "holds a control character, a space, or one of ~ ^ : ? * [ \\"
		}
	}
	return ""
}

// branchRule is refnameRule for what `git check-ref-format --branch` is asked of
// branch, and ok false where only git can say: a name that holds "@" can be
// one git reads as a branch it names in other words, "@{-1}" for the branch
// checked out before this one, and what it names is in the repository's
// reflog, not in the name.
func branchRule(branch string) (rule string, ok bool) {
	switch {
	case strings.Contains(branch, "@"):
		return "", false
	case strings.HasPrefix(branch, "-"):
		return "starts with a dash", true
	case branch == argHead:
		return `is "HEAD"`, true
	}
	return refnameRule(headsPrefix + branch), true
}

// takesAsBranch refuses a name `git check-ref-format --branch` refuses, in
// root, asking git only where branchRule cannot say.
func takesAsBranch(ctx context.Context, root, branch string) error {
	rule, ok := branchRule(branch)
	if !ok {
		_, err := git(ctx, root, nil, "check-ref-format", "--branch", branch)
		return err
	}
	if rule != "" {
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(branch)+" is not a name git takes for a branch",
			"a branch name that "+rule+" is one git refuses")
	}
	return nil
}
