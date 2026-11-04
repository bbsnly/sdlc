package model

import (
	"errors"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// The two importances a finding has, as the review policy names them.
const (
	FindingImportant = "important"
	FindingNit       = "nit"
)

// The three ways a finding is closed.
const (
	ClosedFixed    = "fixed"
	ClosedDeclined = "declined"
	ClosedReferred = "referred"
)

// MaxFindingsPerReview and MaxFindingBytes bound what a review lists and what a
// closing says. gate-record.json is read on every tool call the hook answers, and
// the detail belongs in the review's document, which nothing reads on every call.
const (
	MaxFindingsPerReview = 100
	MaxFindingBytes      = 1000
)

// Finding is one finding a review lists. ID is F-n, numbered per gate and role
// across every round, so that `sdlc finding close` names one finding whichever
// round raised it.
type Finding struct {
	ID         string    `json:"id"`
	Importance string    `json:"importance"`
	Path       string    `json:"path,omitempty"`
	Text       string    `json:"text"`
	Closings   []Closing `json:"closings,omitempty"`
}

// Closing is one closing of a finding. A later one replaces an earlier one, and
// both are kept, so a decline a reviewer then overruled stays on the record.
// Session says which session closed it, not which agent.
type Closing struct {
	At      string `json:"at"`
	As      string `json:"as"`
	Reason  string `json:"reason,omitempty"`
	Session string `json:"session,omitempty"`
}

// ParseFinding reads `<important|nit> [path]: <text>`. The value is split on
// its first ": " (a colon that ends it counts), and what comes before is a path
// only where it is one word: "the plan: step 2 is vague" is text with a colon in
// it, and so is a path with a space in it.
func ParseFinding(value string) (Finding, error) {
	importance, rest, _ := strings.Cut(strings.TrimLeftFunc(value, unicode.IsSpace), " ")
	if importance != FindingImportant && importance != FindingNit {
		why := "a finding starts with its importance, important or nit"
		if bare := strings.TrimSuffix(importance, ":"); bare == FindingImportant || bare == FindingNit {
			why = "the importance is followed by a space, not a colon"
		}
		return Finding{}, errors.New(why)
	}
	f := Finding{Importance: importance, Text: strings.TrimSpace(rest)}
	if head, text, ok := strings.Cut(rest+" ", ": "); ok && !strings.ContainsFunc(strings.TrimSpace(head), unicode.IsSpace) {
		p, err := findingPath(strings.TrimSpace(head))
		if err != nil {
			return Finding{}, err
		}
		f.Path, f.Text = p, strings.TrimSpace(text)
	}
	if f.Text == "" {
		return Finding{}, errors.New("a finding says what was found: the text after its importance, and after the colon that ends any path, is empty")
	}
	return f, nil
}

// findingPath is a finding's path as the loop keeps one: slash-separated,
// cleaned, and otherwise as the reviewer wrote it. The file it names, past any
// quoting and line number, is relative to the repository: one that leaves the
// repository, or names it from the root of a disk, is refused rather than kept,
// however it is decorated. A path in backticks or quotes was refused only bare,
// and so was kept to match nothing, which let a referral of it stand.
func findingPath(p string) (string, error) {
	if strings.Contains(p, `\`) {
		return "", errors.New("a finding's path is written with forward slashes")
	}
	// Both before and after cleaning, because the match reads the cleaned path,
	// and cleaning brings a decoration to the front: `./"/abs/a.go"` is kept as
	// `"/abs/a.go"`, an absolute path once its quotes are read past.
	for _, name := range []string{findingName(p), findingName(path.Clean(p))} {
		if err := refuseFindingName(p, name); err != nil {
			return "", err
		}
	}
	return path.Clean(p), nil
}

// refuseFindingName refuses the file a path names, past its decoration, where
// it is absolute, leaves the repository, or is the repository itself. Whether
// it is absolute is asked of the name cleaned, as FindingFile reads it: read
// before cleaning, `./C:/x/a.go` in backticks got past as relative, and the
// match then read it as the drive path C:/x/a.go.
func refuseFindingName(p, name string) error {
	clean := path.Clean(name)
	switch {
	case strings.HasPrefix(clean, "/") || len(clean) > 1 && clean[1] == ':' && unicode.IsLetter(rune(clean[0])):
		return errors.New("a finding's path is relative to the repository's root, not absolute")
	case slices.Contains(strings.Split(p, "/"), "..") || slices.Contains(strings.Split(name, "/"), ".."):
		return errors.New("a finding's path stays inside the repository, with no .. in it")
	case clean == ".":
		return errors.New("a finding's path names a file or a directory under the repository's root")
	}
	return nil
}

// lineSuffix is a place in a file written after its path: `:3`, `:3-4`, `:3:7`,
// `:3:7-9`, `:12-`, `:L3`, `#L3`, `#L3C5` or `#L3-L4`. A colon not followed by a
// number is part of the name, so `docs/a:b.md` stays whole. Made when first
// needed and not at start-up, which every tool call the hook answers pays for.
var lineSuffix = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`(:L?[0-9]+([-:][0-9]*)*|#L[0-9]+(C[0-9]+)?(-L?[0-9]+(C[0-9]+)?)?)$`)
})

// FindingFile is the file or directory a finding's path names, cleaned: a
// reviewer writes `internal/a.go:12`, puts the path in backticks or quotes,
// with or without the line number, or ends it with the sentence's punctuation.
// The parser refuses on it and the match against the story's changes reads it,
// so the two never disagree about which file a path names: with a normaliser
// each, a path the parser let through as harmless could name a changed file
// that the match did not see.
func FindingFile(p string) string { return path.Clean(findingName(p)) }

// findingName is FindingFile before it is cleaned, so that the parser sees a
// `..` that cleaning would fold away.
func findingName(p string) string {
	for {
		q := trimSentenceEnd(p)
		q = lineSuffix().ReplaceAllString(q, "")
		q = strings.Trim(q, "`'\"")
		if q == p {
			return p
		}
		p = q
	}
}

// trimSentenceEnd drops the `,`, `.`, `;` or `)` a sentence ends a path with.
// One that follows a dot or a slash is part of the path: trimmed, `a/..` would
// read as `a/` and get past the refusal of a path that leaves the repository.
func trimSentenceEnd(p string) string {
	for n := len(p); n > 1 && strings.ContainsRune(",.;)", rune(p[n-1])) && !strings.ContainsRune("./", rune(p[n-2])); n-- {
		p = p[:n-1]
	}
	return p
}

// FindingsMatchCounts reports whether a review's findings agree with its
// counts: the important ones number exactly --important, and the nits listed
// are no more than --nits, which counts those past the policy's cap too. Where
// a gate's findings hold the gates after it, a count above 0 lists what it counts.
func (r Review) FindingsMatchCounts() bool {
	important, nits := 0, 0
	for _, f := range r.Findings {
		if f.Importance == FindingImportant {
			important++
		} else {
			nits++
		}
	}
	counted := func(n *int) int {
		if n == nil {
			return 0
		}
		return *n
	}
	if len(r.Findings) == 0 && !r.Gate.HoldsFindings() {
		return true
	}
	return important == counted(r.Important) && nits <= counted(r.Nits) &&
		(len(r.Findings) > 0 || counted(r.Nits) == 0)
}

// HoldsFindings reports whether a gate's findings are closed before the story
// moves on: the design and code reviews', and no other's.
func (g Gate) HoldsFindings() bool { return g == GateDesignReview || g == GateCodeReview }

// numberFindings gives each finding of rev the next F-n of its gate and role.
func (r *Record) numberFindings(rev *Review) {
	next := 1
	for _, v := range r.Reviews {
		if v.Gate == rev.Gate && v.Role == rev.Role {
			next += len(v.Findings)
		}
	}
	for i := range rev.Findings {
		rev.Findings[i].ID = "F-" + strconv.Itoa(next+i)
	}
}
