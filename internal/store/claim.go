package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// claimSchema names the format of a claim on a story. The file is API: a
// person may read it, and a later release has to.
const claimSchema = "sdlc/claim/1"

// claimsRel is where the claims on stories are kept, relative to the git
// directory every checkout of the repository shares. There, and not under
// .sdlc, because git never shows, commits or pushes it, and the claim names a
// checkout's path, which is nobody else's business.
const claimsRel = "sdlc/claims"

// claim is one checkout's hold on one story.
type claim struct {
	Schema   string `json:"schema"`
	Story    string `json:"story"`
	Checkout string `json:"checkout"`
}

// A Holder is the checkout holding a story.
type Holder struct {
	// Checkout is the checkout that holds the story, and empty where more than
	// one checkout names it and none claims it.
	Checkout string
	// Contenders are the checkouts whose .sdlc/state/active names a story no
	// checkout claims, where there is more than one of them. None of them holds
	// it until a person puts all but one down: taking the first by path handed
	// the story to whichever checkout sorted first, however it came to name it.
	Contenders []Contender
	// Here is whether the checkout is this one.
	Here bool
	// Live is whether the checkout is still a checkout of this repository.
	Live bool
	// Claimed is whether a claim says so, rather than the checkout's active
	// alone, as an iteration running when this release was installed has.
	Claimed bool
}

// A Contender is one checkout naming a story that more than one names.
type Contender struct {
	Checkout      string
	ActiveWritten time.Time
	// Session is what the checkout's .sdlc/state/session names, and empty
	// where it names none.
	Session string
	// ThisSession is whether Session is the session the command runs in.
	ThisSession bool
	Here        bool
}

// Tied reports whether more than one checkout names the story and none holds it.
func (h Holder) Tied() bool { return len(h.Contenders) > 0 }

// claimsPath is the directory the claims are kept in, and false where the
// repository has no git directory to keep them in.
func (s *Store) claimsPath() (string, bool) {
	co, err := s.checkout()
	if err != nil || co.Common == "" {
		return "", false
	}
	return filepath.Join(co.Common, filepath.FromSlash(claimsRel)), true
}

// readClaims is every claim that reads, by story. A file that does not read,
// or reads as a claim on a story other than the one it is named for, is no
// claim: the path in it is later used to find a checkout's state, and a claim
// that names a story it is not filed under says nothing reliable.
func (s *Store) readClaims() map[string]claim {
	dir, ok := s.claimsPath()
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string]claim{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || CheckID(id) != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var c claim
		if json.Unmarshal(raw, &c) != nil || c.Schema != claimSchema || c.Story != id || !filepath.IsAbs(c.Checkout) {
			continue
		}
		out[id] = c
	}
	return out
}

// checkouts is every checkout of the repository: the main checkout, then each
// linked worktree git has an entry for.
func (s *Store) checkouts(co config.Checkout) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if key := config.PathKey(p); key != "" && !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	add(co.Main)
	for _, wt := range co.Worktrees() {
		add(wt)
	}
	add(co.Root)
	return out
}

// Holders says which checkout holds each story that one holds.
//
// A story is held by the checkout a claim names, until it is done or dropped
// or a person releases it. A story no claim names is held by the one checkout
// whose .sdlc/state/active names it, while the backlog has it in progress or
// waiting for a person: that is an iteration that was running when this
// release was installed, and it carries on. Where more than one checkout names
// such a story, none of them holds it.
func (s *Store) Holders() (map[string]Holder, error) {
	co, err := s.checkout()
	if err != nil {
		return nil, err
	}
	b, err := s.Backlog()
	if err != nil {
		return nil, err
	}
	all := s.checkouts(co)
	out := s.claimedHolders(b, co, all)
	s.addNamedHolders(out, b, co, all)
	return out, nil
}

// claimedHolders is the holder of each story a claim names.
func (s *Store) claimedHolders(b *model.Backlog, co config.Checkout, all []string) map[string]Holder {
	known := make(map[string]bool, len(all))
	for _, p := range all {
		known[config.PathKey(p)] = true
	}
	live := func(p string) bool {
		return known[config.PathKey(p)] && fsx.Exists(filepath.Join(p, ".git"))
	}
	out := map[string]Holder{}
	for id, c := range s.readClaims() {
		story, ok := b.Find(id)
		if !ok || story.Status == model.StatusDone || story.Status == model.StatusDropped {
			continue
		}
		h := Holder{Checkout: c.Checkout, Here: config.SamePath(c.Checkout, co.Root),
			Live: live(c.Checkout), Claimed: true}
		if !h.Live && movedHere(co, c) {
			h.Checkout, h.Here, h.Live = co.Root, true, true
		}
		out[story.ID] = h
	}
	return out
}

// addNamedHolders adds to out the stories that no claim names but a checkout's
// active does.
func (s *Store) addNamedHolders(out map[string]Holder, b *model.Backlog, co config.Checkout, all []string) {
	named := map[string][]Contender{}
	var order []string
	for _, dir := range all {
		id, written := activeIn(dir)
		if id == "" {
			continue
		}
		story, ok := b.Find(id)
		if !ok || story.Status != model.StatusInProgress && story.Status != model.StatusAwaitingHuman {
			continue
		}
		if _, claimed := out[story.ID]; claimed {
			continue
		}
		if _, seen := named[story.ID]; !seen {
			order = append(order, story.ID)
		}
		session := sessionIn(dir)
		named[story.ID] = append(named[story.ID], Contender{Checkout: dir, ActiveWritten: written,
			Session: session, ThisSession: session != "" && session == s.CommandSession(),
			Here: config.SamePath(dir, co.Root)})
	}
	for _, id := range order {
		cs := named[id]
		if len(cs) == 1 {
			out[id] = Holder{Checkout: cs[0].Checkout, Here: cs[0].Here, Live: true}
			continue
		}
		out[id] = Holder{Contenders: cs}
	}
}

// HeldElsewhere is the holder of a story another checkout holds, or that more
// than one checkout names, and false where this checkout holds it or nobody
// does.
func (s *Store) HeldElsewhere(id string) (Holder, bool, error) {
	if !s.OthersMayHold() {
		return Holder{}, false, nil
	}
	holders, err := s.Holders()
	if err != nil {
		return Holder{}, false, err
	}
	h, ok := holders[id]
	if !ok || h.Here && !h.Tied() {
		return Holder{}, false, nil
	}
	return h, true, nil
}

// OthersMayHold reports whether a checkout other than this one could hold a
// story: the repository has a linked worktree, or a claim names another
// checkout. Asked without the backlog, so that a single checkout's commands go
// on working while a person has the backlog half edited, as they always did;
// a checkout that cannot be placed says so through Holders.
func (s *Store) OthersMayHold() bool {
	co, err := s.checkout()
	if err != nil || co.HasWorktrees() {
		return true
	}
	for _, c := range s.readClaims() {
		if !config.SamePath(c.Checkout, co.Root) && !movedHere(co, c) {
			return true
		}
	}
	return false
}

// movedHere reports whether a claim naming no checkout of this repository is
// this checkout's own, still naming the path it had before it was moved or
// copied: there is no linked worktree it could be instead, and this checkout's
// active names the story. Read as a checkout that is gone, a moved project's
// own story met SDLC-E0051 on every command that writes. Start rewrites the
// claim to this checkout's path. A worktree git still has an entry for, and a
// story this checkout is not running, stay SDLC-E0051.
func movedHere(co config.Checkout, c claim) bool {
	if co.HasWorktrees() {
		return false
	}
	id, _ := activeIn(co.Root)
	return id == c.Story
}

// ShowsHolders reports whether output names the checkout holding each story:
// where the repository has a linked worktree, or a claim names a checkout that
// is not the main one, as one does after its worktree was removed. A single
// checkout prints what it always has.
func (s *Store) ShowsHolders(holders map[string]Holder) bool {
	co, err := s.checkout()
	if err != nil {
		return false
	}
	if co.HasWorktrees() {
		return true
	}
	for _, h := range holders {
		if h.Checkout != "" && !config.SamePath(h.Checkout, co.Main) {
			return true
		}
	}
	return false
}

// HasLinkedWorktree reports whether the repository has a linked worktree.
func (s *Store) HasLinkedWorktree() bool {
	co, err := s.checkout()
	return err == nil && co.HasWorktrees()
}

// CheckoutPath is this checkout, as a claim names it.
func (s *Store) CheckoutPath() string {
	co, _ := s.checkout()
	return co.Root
}

// Claim records this checkout as the one holding the story. A claim already
// this checkout's is left as it is.
func (s *Store) Claim(id string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	dir, ok := s.claimsPath()
	if !ok {
		return nil
	}
	here := s.CheckoutPath()
	if c, ok := s.readClaims()[id]; ok && config.SamePath(c.Checkout, here) {
		return nil
	}
	data, err := encodeJSON(claim{Schema: claimSchema, Story: id, Checkout: here})
	if err == nil {
		err = fsx.WriteFileAtomic(filepath.Join(dir, id+".json"), data, 0o644)
	}
	if err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"the claim on "+quote(id)+couldNotBeWritten,
			"it is kept in "+sdlcerr.QuotePath(dir)+", which may not be writable").WithCause(err)
	}
	return nil
}

// ReleaseClaim removes the claim on a story, where there is one.
func (s *Store) ReleaseClaim(id string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	dir, ok := s.claimsPath()
	if !ok {
		return nil
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"the claim on "+quote(id)+couldNotBeRemoved,
			"it is kept in "+sdlcerr.QuotePath(dir)+", which may not be writable").WithCause(err)
	}
	return nil
}

// keepClaim writes the claim a checkout holds a story by: an iteration running
// when this release was installed holds its story by its active alone, and
// ending that iteration, which removes the active, must not let the story go.
// The same goes for a claim still naming the path a moved checkout had, which
// is this checkout's only while its active names the story. Claim leaves a
// claim that already names this checkout as it is.
func (s *Store) keepClaim(id string) error {
	holders, err := s.Holders()
	if err != nil {
		return err
	}
	if h, ok := holders[id]; ok && h.Here && !h.Tied() {
		return s.Claim(id)
	}
	return nil
}

// KeepClaim is keepClaim, for a command about to end an iteration.
func (s *Store) KeepClaim(id string) error { return s.keepClaim(id) }

// TidyClaims writes the claim for an iteration this checkout holds by its
// active alone, and removes claims on stories that are done, dropped or gone.
// The caller holds the repository's lock.
//
// It is housekeeping, and fails soft: nothing it cannot do is the command's
// failure, and the next command that writes tries again.
func (s *Store) TidyClaims() {
	if err := s.tidyClaims(); err != nil {
		slog.Debug("could not tidy the claims on stories", "err", err)
	}
}

func (s *Store) tidyClaims() error {
	dir, ok := s.claimsPath()
	if !ok {
		return nil
	}
	b, err := s.Backlog()
	if err != nil {
		return err
	}
	for id := range s.readClaims() {
		if story, ok := b.Find(id); !ok || story.Status == model.StatusDone || story.Status == model.StatusDropped {
			if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	active, err := s.Active()
	if err != nil || active == "" {
		return err
	}
	return s.keepClaim(active)
}

// SessionScanLimit is how many linked worktrees the hook reads, on every tool
// call, for a session whose story no claim records yet. The checkouts claims
// name are read whatever their number; this bounds only what an iteration
// running when this release was installed costs until its first command that
// writes turns its hold into a claim.
const SessionScanLimit = 64

// SessionHolder is the other checkout a Claude Code session holds a story in,
// and that story. The checkouts claims name are asked first, all of them:
// only the tool writes a claim, and a session could not move itself out of
// their reach by adding worktrees. Then, for an iteration held by its active
// alone, the first limit linked worktrees; 0 is every one.
//
// A checkout whose session file names the session holds nothing unless it
// holds the story its active names: a worktree made from a commit that
// carried .sdlc/state has the last story's session in it, and that session
// working on in the main checkout is not held in the worktree.
func (s *Store) SessionHolder(session string, limit int) (checkout, story string) {
	if !CheckSession(session) {
		return "", ""
	}
	co, err := s.checkout()
	if err != nil {
		return "", ""
	}
	candidates := s.sessionCandidates(co, limit)

	var holders map[string]Holder
	for _, dir := range candidates {
		id := activeOfSession(dir, session)
		if id == "" {
			continue
		}
		if holders == nil {
			if holders, err = s.Holders(); err != nil {
				return "", ""
			}
		}
		h, ok := holders[id]
		if !ok {
			continue
		}
		if h.holdsIn(dir) {
			return dir, id
		}
	}
	return "", ""
}

// sessionCandidates is the other checkouts SessionHolder asks, in the order it
// asks them: the main one, those claims name, then the first limit linked
// worktrees.
func (s *Store) sessionCandidates(co config.Checkout, limit int) []string {
	var candidates []string
	seen := map[string]bool{config.PathKey(co.Root): true}
	add := func(p string) {
		if key := config.PathKey(p); key != "" && !seen[key] {
			seen[key] = true
			candidates = append(candidates, p)
		}
	}
	add(co.Main)
	for _, c := range s.readClaims() {
		add(c.Checkout)
	}
	for _, wt := range co.WorktreesUpTo(limit) {
		add(wt)
	}
	return candidates
}

// activeOfSession is the story dir's active names, where dir's session file
// names session, and "" otherwise.
func activeOfSession(dir, session string) string {
	if sessionIn(dir) != session {
		return ""
	}
	id, _ := activeIn(dir)
	return id
}

// holdsIn reports whether dir is the holder's checkout or one of the
// contenders'.
func (h Holder) holdsIn(dir string) bool {
	return config.SamePath(h.Checkout, dir) ||
		slices.ContainsFunc(h.Contenders, func(c Contender) bool { return config.SamePath(c.Checkout, dir) })
}

// activeIn is the story a checkout's .sdlc/state/active names, and when it was
// written, or "" where it names none.
func activeIn(dir string) (string, time.Time) {
	path := filepath.Join(dir, filepath.FromSlash(activeFile))
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}
	}
	id := strings.TrimSpace(string(raw))
	if CheckID(id) != nil {
		return "", time.Time{}
	}
	var written time.Time
	if info, err := os.Stat(path); err == nil {
		written = info.ModTime()
	}
	return id, written
}

// ActiveIn is the story the checkout at dir is running an iteration on, or "".
func ActiveIn(dir string) string {
	id, _ := activeIn(dir)
	return id
}

// sessionIn is the session a checkout's .sdlc/state/session names, or "".
func sessionIn(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(sessionFile)))
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(raw))
	if !CheckSession(id) {
		return ""
	}
	return id
}

// encodeJSON is v as indented JSON with a trailing newline, with nothing
// escaped for HTML: a path with an ampersand in it is written as it is.
func encodeJSON(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// HeldElsewhereError refuses a command on a story this checkout does not hold.
// reason says why the command belongs where the story is held. Paths are in the
// what and the why, quoted; a fix is fixed text, and names none.
func HeldElsewhereError(id string, h Holder, reason string) error {
	switch {
	case h.Tied():
		// The fix asks a person to keep the checkout whose session they
		// recognise, so the caller's own checkout and session are marked as
		// such, each where it cannot be read as describing the other.
		named := make([]string, len(h.Contenders))
		for i, c := range h.Contenders {
			session := "names no session"
			switch {
			case c.ThisSession:
				session = "names the session " + quote(c.Session) + ", which is this session"
			case c.Session != "":
				session = "names the session " + quote(c.Session)
			}
			here := ""
			if c.Here {
				here = "this checkout; "
			}
			named[i] = sdlcerr.QuotePath(c.Checkout) + " (" + here + "its .sdlc/state/active was written " +
				c.ActiveWritten.UTC().Format(time.RFC3339) + ", and its .sdlc/state/session " + session + ")"
		}
		return sdlcerr.New(sdlcerr.StoryHeldElsewhere,
			quote(id)+" is named by more than one checkout of this repository, and none of them holds it",
			"each of these has it as its current story and no claim says which is working it: "+
				strings.Join(named, "; ")).
			WithFix(`a person keeps the checkout whose session they recognise as the one they are working in, ` +
				`runs "sdlc stop" in each of the others named above, and carries on; an assistant stops and tells the person`)
	case !h.Live:
		return sdlcerr.New(sdlcerr.HoldingCheckoutMissing,
			"the checkout holding "+quote(id)+" no longer exists",
			"the claim on it names "+sdlcerr.QuotePath(h.Checkout)+", which is not a checkout of this repository any more: "+
				"it was removed, or this checkout was moved from there")
	}
	return sdlcerr.New(sdlcerr.StoryHeldElsewhere,
		quote(id)+" is held by another checkout of this repository",
		"the checkout at "+sdlcerr.QuotePath(h.Checkout)+" holds it, and "+reason)
}
