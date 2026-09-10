// Package scaffold sets a repository up to take part in the loop.
//
// It writes four things: the configuration, the story schema editors validate
// against, a starter backlog, and the contract section the assistant reads from
// CLAUDE.md. It writes no .gitignore: what a project commits is the project's
// decision, and the loop's state is useful in version control for exactly the
// same reasons the code is.
package scaffold

import (
	"bytes"
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

//go:embed templates
var templates embed.FS

// ContractHeading is the section of a project's CLAUDE.md that makes it take
// part. Its presence is half the contract; .sdlc/config.json is the other half.
const ContractHeading = "## SDLC Contract"

// Paths this writes, relative to the repository root and slash-separated.
const (
	schemaPath   = config.Dir + "/templates/story.schema.json"
	claudeMDPath = "CLAUDE.md"
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
// force is set, and it never overwrites a backlog or a CLAUDE.md: those hold
// the user's own words.
func Init(root string, force bool) (*Result, error) {
	configPath := filepath.Join(root, filepath.FromSlash(config.File))
	if fsx.Exists(configPath) && !force {
		return nil, sdlcerr.New(sdlcerr.AlreadyInitialised,
			"this project is already set up for the loop",
			config.File+" is already there, and overwriting it would lose your settings")
	}

	stack, alsoSeen := Detect(root)
	res := &Result{Root: root, Stack: stack.Name, AlsoSeen: alsoSeen}

	cfg, err := renderConfig(stack)
	if err != nil {
		return nil, err
	}
	if err := res.write(root, config.File, cfg, true); err != nil {
		return nil, err
	}

	schema, err := templates.ReadFile("templates/story.schema.json")
	if err != nil {
		return nil, embedFailure(err)
	}
	if err := res.write(root, schemaPath, schema, true); err != nil {
		return nil, err
	}

	backlog, err := templates.ReadFile("templates/user_stories.json")
	if err != nil {
		return nil, embedFailure(err)
	}
	if err := res.write(root, config.Default().Backlog.Path, backlog, false); err != nil {
		return nil, err
	}

	if err := res.writeContract(root); err != nil {
		return nil, err
	}
	return res, nil
}

// write puts one file in place. When overwrite is false an existing file is
// kept and recorded, because the file holds something the user wrote.
func (r *Result) write(root, rel string, data []byte, overwrite bool) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
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

// writeContract adds the SDLC Contract section to the project's CLAUDE.md,
// appending to whatever is already there rather than replacing it. A CLAUDE.md
// that already has the section is left alone entirely: it has been filled in.
func (r *Result) writeContract(root string) error {
	contract, err := templates.ReadFile("templates/contract.md")
	if err != nil {
		return embedFailure(err)
	}
	path := filepath.Join(root, claudeMDPath)

	existing, err := os.ReadFile(path)
	if err == nil {
		if strings.Contains(string(existing), ContractHeading) {
			r.Kept = append(r.Kept, claudeMDPath)
			return nil
		}
		var b bytes.Buffer
		b.Write(existing)
		if !bytes.HasSuffix(existing, []byte("\n")) {
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.Write(contract)
		return r.writeRaw(root, claudeMDPath, b.Bytes(), "appended to")
	}

	header := "# " + filepath.Base(root) + "\n\n<!-- Project instructions for Claude Code. -->\n\n"
	return r.writeRaw(root, claudeMDPath, append([]byte(header), contract...), "")
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

// renderConfig fills the configuration template from the detected stack.
func renderConfig(stack Stack) ([]byte, error) {
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
		BacklogPath: defaults.Backlog.Path,
		TrunkBranch: defaults.Git.TrunkBranch,
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
