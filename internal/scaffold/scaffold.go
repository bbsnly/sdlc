// Package scaffold sets a repository up to take part in the loop.
//
// It writes five things: the configuration, the story schema editors validate
// against, a starter backlog, the contract section the assistant reads from
// CLAUDE.md or AGENTS.md, and the review policy every reviewer reads. Beside
// them it writes .sdlc/.gitignore, which ignores each story's trace and nothing
// else, and it never writes the project's own .gitignore: what a project
// commits is the project's decision, and the loop's state is useful in version
// control for exactly the same reasons the code is. A trace is the exception:
// it is telemetry of the machine that made it, which the hook appends to on
// every tool call, so tracked it leaves the tree dirty for good (US-112,
// AC-26). The other exception is .sdlc/state: every story worktree writes its
// own there, so a story worked in one cannot land on a trunk that tracks it.
// Start ignores it inside each new worktree, and doctor says when trunk tracks
// it; neither writes the project's own .gitignore.
package scaffold

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

//go:embed templates
var templates embed.FS

//go:embed templates/review-policy.md
var reviewPolicyTemplate string

// ContractHeading is the section of a project's CLAUDE.md or AGENTS.md that
// makes it take part. Its presence is half the contract; .sdlc/config.json is the other half.
const ContractHeading = "## SDLC Contract"

// HasContract reports whether text has the contract heading on a line of its
// own. Finding the words anywhere is not enough: "### SDLC Contract" is a
// subsection of something else, and a sentence that mentions the heading is not
// the section it names.
func HasContract(text string) bool {
	for line := range strings.Lines(text) {
		if strings.TrimRight(line, " \t\r\n") == ContractHeading {
			return true
		}
	}
	return false
}

// Paths this writes, relative to the repository root and slash-separated.
const (
	schemaPath   = config.Dir + "/templates/story.schema.json"
	claudeMDPath = "CLAUDE.md"
	agentsFile   = "AGENTS.md"
)

// Result says what init did, in enough detail to print without asking the disk
// again.
type Result struct {
	Root     string
	Stack    string   // the stack that was detected, or "" if none was
	AlsoSeen []string // other stacks present, when the repository is mixed
	Created  []string // repository-relative paths written
	Kept     []string // paths left exactly as they were
}

// Init sets up root. It refuses to overwrite an existing configuration unless
// force is set, and it never overwrites a backlog, a CLAUDE.md or an
// AGENTS.md: those hold the user's own words.
func Init(root string, force bool) (*Result, error) {
	return InitAt(root, root, force)
}

// InitAt is Init with the backlog and its schema in shared: the main checkout
// of the repository, where every checkout of it reads them. It is root except
// in a linked worktree, whose own copy of the backlog nothing reads.
func InitAt(root, shared string, force bool) (*Result, error) {
	configPath := filepath.Join(root, filepath.FromSlash(config.File))
	if fsx.Exists(configPath) && !force {
		return nil, sdlcerr.New(sdlcerr.AlreadyInitialised,
			"this project is already set up for the loop",
			config.File+" is already there, and overwriting it would lose your settings")
	}

	stack, alsoSeen := Detect(root)
	res := &Result{Root: root, Stack: stack.Name, AlsoSeen: alsoSeen}

	// --force restores the settings, and two values in the file are not
	// settings but facts about the repository: where its stories are and what
	// its trunk is called. Resetting backlog.path pointed the loop at a fresh
	// example backlog and left the real one unread, and resetting the trunk
	// made `sdlc start` refuse every story on a trunk that is not "main". A
	// configuration too broken to read has nothing to keep.
	//
	// A trunk nobody has named yet is taken from the repository for the same
	// reason: writing "main" into a project on master had its first
	// `sdlc start` refused for not being on trunk.
	defaults := config.Default()
	backlogPath, trunk := defaults.Backlog.Path, defaults.Git.TrunkBranch
	policyPath, namedPolicy := defaults.ReviewPolicyPath(), ""
	if found := gitx.Trunk(context.Background(), root); found != "" {
		trunk = found
	}
	if force {
		if old, err := config.Load(root); err == nil {
			backlogPath, trunk = old.Backlog.Path, old.Git.TrunkBranch
			policyPath, namedPolicy = old.ReviewPolicyPath(), old.Reviews.Policy
		}
	}

	cfg, err := renderConfig(stack, backlogPath, trunk)
	if err != nil {
		return nil, err
	}
	if err := res.write(root, config.File, keepPolicyPath(cfg, namedPolicy), true); err != nil {
		return nil, err
	}

	schema, err := templates.ReadFile("templates/story.schema.json")
	if err != nil {
		return nil, embedFailure(err)
	}
	if err := res.write(shared, schemaPath, schema, true); err != nil {
		return nil, err
	}

	backlog, err := templates.ReadFile("templates/user_stories.json")
	if err != nil {
		return nil, embedFailure(err)
	}
	if err := res.write(shared, backlogPath, backlog, false); err != nil {
		return nil, err
	}
	// Where the stories are kept, once the schema has made the directory, and
	// beside what is listed rather than through it: init's printed list is API.
	if err := IgnoreTraces(shared); err != nil {
		return nil, err
	}

	if err := res.writeContract(root); err != nil {
		return nil, err
	}
	if err := res.write(root, policyPath, renderReviewPolicy(stack), false); err != nil {
		return nil, err
	}
	return res, nil
}

// keepPolicyPath puts a path a person named back into a rendered configuration,
// as `--force` keeps the backlog's. Through quoteJSON because %q's escapes are not
// all JSON. A reworded template line would make this a no-op, which the force
// tests catch.
func keepPolicyPath(cfg []byte, named string) []byte {
	if named == "" {
		return cfg
	}
	return bytes.Replace(cfg, []byte(`"reviews": { `), []byte(`"reviews": { "policy": `+quoteJSON(named)+`, `), 1)
}

// renderReviewPolicy is the template with the commands init wrote for this stack
// under what not to review, or none for a stack with no commands.
func renderReviewPolicy(stack Stack) []byte {
	var list strings.Builder
	for _, key := range slices.Sorted(maps.Keys(stack.Commands)) {
		if c := stack.Commands[key]; c != "" {
			list.WriteString("- `" + key + "`: `" + c + "`\n")
		}
	}
	named := ""
	if list.Len() > 0 {
		named = "\nThe commands `sdlc init` wrote:\n\n" + list.String()
	}
	return []byte(strings.Replace(reviewPolicyTemplate, "{{commands}}\n", named, 1))
}

// traceIgnore is the one line .sdlc/.gitignore holds: every story's trace,
// as the hook names it.
const traceIgnore = "/stories/*/" + model.TraceFile + "\n"

// TraceIgnoreFile is the file IgnoreTraces writes, from the project's root,
// slash-separated as git status names it.
const TraceIgnoreFile = config.Dir + "/.gitignore"

// IgnoreTraces writes <root>/.sdlc/.gitignore, ignoring every story's trace,
// where there is no file at that name. One that is there is the project's and
// is left byte for byte, so a project that took the line out keeps it out.
// Init writes it, and so does start, for a project initialised before there
// were traces.
func IgnoreTraces(root string) error {
	dir := filepath.Join(root, filepath.FromSlash(config.Dir))
	path := filepath.Join(root, filepath.FromSlash(TraceIgnoreFile))
	rel := TraceIgnoreFile
	// Not through a link to a directory elsewhere, as the store writes
	// nothing there either.
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return sdlcerr.New(sdlcerr.StateUnwritable, rel+" could not be written",
			config.Dir+" is not a directory in the project")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // G302: an ignore file git and the person read
	if errors.Is(err, fs.ErrExist) || err != nil && fsx.Exists(path) {
		// Windows refuses a directory at the name as access denied, not as
		// there already; asking the disk sends both to "the project's own".
		return nil
	}
	if err == nil {
		_, err = f.WriteString(traceIgnore)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable, rel+" could not be written",
			"its directory may not be writable, or the disk may be full").WithCause(err)
	}
	return nil
}

// write puts one file in place, under dir. When overwrite is false an existing
// file is kept and recorded, because the file holds something the user wrote.
// A file outside the root being set up is recorded by its full path.
func (r *Result) write(dir, rel string, data []byte, overwrite bool) error {
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if dir != r.Root {
		rel = path
	}
	if !overwrite && fsx.Exists(path) {
		r.Kept = append(r.Kept, rel)
		return nil
	}
	if err := fsx.WriteFileAtomic(path, data, 0o644); err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			rel+" could not be written",
			"its directory may not be writable, or the disk may be full").WithCause(err)
	}
	r.Created = append(r.Created, rel)
	return nil
}

// writeContract adds the SDLC Contract section to the file Claude Code reads
// the project's instructions from: CLAUDE.md, or AGENTS.md where there is no
// CLAUDE.md or CLAUDE.md is a link to it. It appends to whatever is there
// rather than replacing it, and where either file already has the section
// neither is touched: it has been filled in.
//
// Claude Code reads AGENTS.md only where there is no CLAUDE.md, so a CLAUDE.md
// created beside one would hide the project's own instructions. Nothing is
// written through a link: WriteFileAtomic renames over it, so a shared
// AGENTS.md is never given this project's contract.
func (r *Result) writeContract(root string) error {
	contract, err := templates.ReadFile("templates/contract.md")
	if err != nil {
		return embedFailure(err)
	}
	claudePath, agentsPath := filepath.Join(root, claudeMDPath), filepath.Join(root, agentsFile)
	claudeLink, claudeErr := os.Lstat(claudePath)
	claude, claudeText := RegularFile(claudePath)
	agents, agentsText := RegularFile(agentsPath)
	same := claude != nil && agents != nil && os.SameFile(claude, agents)
	linked := same && claudeLink.Mode()&os.ModeSymlink != 0

	switch {
	case !linked && HasContract(claudeText):
		r.Kept = append(r.Kept, claudeMDPath)
		return nil
	case HasContract(agentsText):
		r.Kept = append(r.Kept, agentsFile)
		return nil
	case linked:
		return r.appendContract(root, agentsFile, agentsText, contract, agentsNote(agentsPath, "appended to; CLAUDE.md links to it"))
	case claude != nil:
		note := "appended to"
		if same {
			note = "appended to; it was a hard link to AGENTS.md, which keeps its old text"
		}
		return r.appendContract(root, claudeMDPath, claudeText, contract, note)
	case claudeErr != nil && agents != nil:
		return r.appendContract(root, agentsFile, agentsText, contract, agentsNote(agentsPath, "appended to"))
	}
	header := "# " + filepath.Base(root) + "\n\n<!-- Project instructions for Claude Code. -->\n\n"
	return r.writeRaw(root, claudeMDPath, append([]byte(header), contract...), "")
}

// RegularFile is the file at path and its text, where Stat, following links,
// says it is a regular file that can be read; nil otherwise.
func RegularFile(path string) (os.FileInfo, string) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ""
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}
	return info, string(text)
}

// agentsNote is note, or what the write did to an AGENTS.md that is a link:
// put a file of the project's own in its place.
func agentsNote(path, note string) string {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "appended to a copy of the file it linked to, which is unchanged"
	}
	return note
}

// appendContract writes text with the contract after it, a blank line
// between.
func (r *Result) appendContract(root, rel, text string, contract []byte, note string) error {
	var b bytes.Buffer
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.Write(contract)
	return r.writeRaw(root, rel, b.Bytes(), note)
}

func (r *Result) writeRaw(root, rel string, data []byte, note string) error {
	if err := fsx.WriteFileAtomic(filepath.Join(root, filepath.FromSlash(rel)), data, 0o644); err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			rel+" could not be written",
			"its directory may not be writable, or the disk may be full").WithCause(err)
	}
	label := rel
	if note != "" {
		label = rel + " (" + note + ")"
	}
	r.Created = append(r.Created, label)
	return nil
}

// renderConfig fills the configuration template from the detected stack, with
// the backlog and trunk the project already has.
func renderConfig(stack Stack, backlogPath, trunk string) ([]byte, error) {
	raw, err := templates.ReadFile("templates/config.json.tmpl")
	if err != nil {
		return nil, embedFailure(err)
	}
	tmpl, err := template.New("config").Funcs(template.FuncMap{
		"q":        quoteJSON,
		"jsonList": jsonList,
		"cmd":      func(m map[string]string, k string) string { return m[k] },
	}).Parse(string(raw))
	if err != nil {
		return nil, embedFailure(err)
	}

	defaults := config.Default()
	data := struct {
		BacklogPath string
		TrunkBranch string
		StackNote   string
		Commands    map[string]string
		TestDirs    []string
		TestGlobs   []string
		SrcDirs     []string
	}{
		BacklogPath: backlogPath,
		TrunkBranch: trunk,
		StackNote:   stackNote(stack),
		Commands:    stack.Commands,
		TestDirs:    orDefault(stack.TestDirs, defaults.Paths.Tests.Dirs),
		TestGlobs:   orDefault(stack.TestGlobs, defaults.Paths.Tests.FileGlobs),
		SrcDirs:     orDefault(stack.SrcDirs, defaults.Paths.Src),
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, embedFailure(err)
	}
	return out.Bytes(), nil
}

func stackNote(stack Stack) string {
	if stack.Name == "" {
		return "No stack was recognised, so these are empty. Fill in the ones your project has, " +
			"then run 'sdlc doctor'."
	}
	return "Guessed from a " + stack.Name + " project. Check them, then run 'sdlc doctor'."
}

func orDefault[T any](v, fallback []T) []T {
	if len(v) == 0 {
		return fallback
	}
	return v
}

// quoteJSON renders a Go string as a JSON string literal. The template writes
// JSON, so a command containing a quote or a backslash has to be escaped by the
// same rules that will parse it.
func quoteJSON(s string) string {
	out, err := json.Marshal(s)
	if err != nil { // unreachable: a Go string always marshals
		return `""`
	}
	return string(out)
}

func jsonList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	out, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(out)
}

// embedFailure covers the templates compiled into the binary going wrong, which
// means the binary itself is broken rather than the user's project.
func embedFailure(err error) error {
	return sdlcerr.New(sdlcerr.StateUnwritable,
		"the built-in project templates could not be used",
		"this binary was built wrong; the templates are compiled into it").
		WithFix(`please open an issue at https://github.com/bbsnly/sdlc/issues with the code above`).
		WithCause(err)
}
