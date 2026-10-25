package config

// US-099 AC-3: the project's configuration names the paths in which the lines
// of an unresolved conflict are meant to be, and a story that adds them there
// is not refused for it.
//
// What these hold the package to:
//
//	Paths.ConflictMarkersIntended []string   `json:"conflict_markers_intended"`, under "paths"
//	func (p Paths) IntendsConflictMarkers(rel string) bool
//
// rel is a repository-relative, slash-separated path as git names it. A
// pattern is matched against the whole path, with "**" as a segment of its
// own that stands for any number of directories, as for the test paths; and
// exactly, not without regard to case, since folding is the unsafe direction
// for an exemption (decision 5 of 2026-10-24). The key is absent from what
// `sdlc init` writes, so a project that never heard of it has no path exempt.
// A list that cannot mean what it says does not load, under SDLC-E0045.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

const intendedKey = "conflict_markers_intended"

func TestAC3TheConfigurationNamesThePathsWhereConflictMarkersAreIntended(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"paths":{"`+intendedKey+`":["docs/**","notes/merging.md"]}}`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"docs/**", "notes/merging.md"}; !slices.Equal(cfg.Paths.ConflictMarkersIntended, want) {
		t.Errorf("Paths.ConflictMarkersIntended = %q, want %q", cfg.Paths.ConflictMarkersIntended, want)
	}
	if len(cfg.Paths.Tests.FileGlobs) == 0 || len(cfg.Paths.Src) == 0 {
		t.Errorf("naming the key emptied the rest of paths: %+v", cfg.Paths)
	}
	if unknown := UnknownKeys(root); len(unknown) != 0 {
		t.Errorf("UnknownKeys = %q, want the key to be one the configuration has", unknown)
	}
	for _, rel := range []string{"docs/merging.md", "docs/a/b/c.md", "notes/merging.md"} {
		if !cfg.Paths.IntendsConflictMarkers(rel) {
			t.Errorf("a project that lists docs/** and notes/merging.md does not intend them in %s", rel)
		}
	}
}

func TestAC3NoPathIntendsConflictMarkersUntilTheConfigurationSaysSo(t *testing.T) {
	if got := Default().Paths.ConflictMarkersIntended; len(got) != 0 {
		t.Errorf("the default configuration intends conflict markers in %q", got)
	}
	root := repo(t)
	writeConfig(t, root, `{"paths":{"src":["lib/"]}}`)
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Paths.ConflictMarkersIntended) != 0 {
		t.Errorf("a file that never mentions the key got %q", cfg.Paths.ConflictMarkersIntended)
	}
	for _, rel := range []string{"docs/merging.md", "notes.txt", "CODEMAP.md", "a.patch"} {
		if cfg.Paths.IntendsConflictMarkers(rel) {
			t.Errorf("with nothing listed, conflict markers are intended in %s", rel)
		}
	}
	if (Paths{}).IntendsConflictMarkers("") {
		t.Error("with nothing listed, conflict markers are intended in the empty path")
	}
}

func TestAC3AMisspeltKeyForTheIntendedPathsIsNamedAsUnknown(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"paths":{"conflict_marker_intended":["docs/**"]},"conflict_markers_intended":["docs/**"]}`)
	got := UnknownKeys(root)
	for _, want := range []string{"paths.conflict_marker_intended", "conflict_markers_intended"} {
		if !slices.Contains(got, want) {
			t.Errorf("UnknownKeys = %q, does not name %s: it changes nothing, and a path believed exempt was not", got, want)
		}
	}
}

// THREATS T9: a list that cannot mean what it says is refused when the
// configuration loads, naming the setting, so that doctor shows it and no
// landing runs on a protection a person believed in that was not there.
func TestAC3IntendedPathsThatCannotMeanWhatTheySayAreRefusedAtLoad(t *testing.T) {
	for body, why := range map[string]string{
		`{"paths":{"` + intendedKey + `":[""]}}`:               "paths." + intendedKey,
		`{"paths":{"` + intendedKey + `":["/etc/**"]}}`:        "paths." + intendedKey,
		`{"paths":{"` + intendedKey + `":["../docs/**"]}}`:     "paths." + intendedKey,
		`{"paths":{"` + intendedKey + `":["docs/../**"]}}`:     "paths." + intendedKey,
		`{"paths":{"` + intendedKey + `":["docs\\**"]}}`:       "paths." + intendedKey,
		`{"paths":{"` + intendedKey + `":["./docs/**"]}}`:      "paths." + intendedKey,
		`{"paths":{"` + intendedKey + `":["docs//a.md"]}}`:     "paths." + intendedKey,
		`{"paths":{"` + intendedKey + `":["docs/**","/abs"]}}`: "paths." + intendedKey,
	} {
		root := repo(t)
		writeConfig(t, root, body)
		_, err := Load(root)
		if err == nil {
			t.Errorf("Load accepted %s", body)
			continue
		}
		var e *sdlcerr.Error
		if !errors.As(err, &e) {
			t.Errorf("%s: err = %v, want an sdlcerr.Error", body, err)
			continue
		}
		if e.Code != sdlcerr.ConfigInvalid {
			t.Errorf("%s: code = %s, want %s", body, e.Code, sdlcerr.ConfigInvalid)
		}
		if said := e.What + " " + e.Why + " " + err.Error(); !strings.Contains(said, why) {
			t.Errorf("%s: the refusal does not name %q: %q", body, why, said)
		}
	}
}

func TestAC3IntendedPathsThatAreFilesOrGlobsLoad(t *testing.T) {
	for _, list := range []string{
		`["notes.txt"]`,
		`["docs/**"]`,
		`["**/*.patch"]`,
		`["testdata/*.diff","docs/merging.md"]`,
		`["docs/price list.md"]`,
		`[]`,
	} {
		root := repo(t)
		writeConfig(t, root, `{"paths":{"`+intendedKey+`":`+list+`}}`)
		if _, err := Load(root); err != nil {
			t.Errorf("Load refused %s: %v", list, err)
		}
	}
}

// Decision 5: globs with "**" as for the test paths, matched exactly.
func TestAC3IntendedPathsAreMatchedByGlobAndExactlyWithoutFoldingCase(t *testing.T) {
	for _, c := range []struct {
		name     string
		patterns []string
		yes      []string
		no       []string
	}{
		{"a directory and everything under it", []string{"docs/**"},
			[]string{"docs/a.md", "docs/x/y/z.md", "docs/price list.md"},
			[]string{"docs", "Docs/a.md", "DOCS/a.md", "docs2/a.md", "xdocs/a.md", "sub/docs/a.md", "a.md", ""}},
		{"one file", []string{"notes.txt"},
			[]string{"notes.txt"},
			[]string{"Notes.txt", "NOTES.TXT", "notes.txt.bak", "notes.tx", "notes"}},
		{"a file by its extension at any depth", []string{"**/*.patch"},
			[]string{"a.patch", "x/y/a.patch"},
			[]string{"a.patchx", "a.PATCH", "a.patch/b", "patch"}},
		{"one directory's files and not those below", []string{"testdata/*.diff"},
			[]string{"testdata/a.diff"},
			[]string{"testdata/x/a.diff", "Testdata/a.diff", "testdata/a.DIFF", "other/testdata/a.diff"}},
		{"a file in a directory", []string{"notes/merging.md"},
			[]string{"notes/merging.md"},
			[]string{"notes/Merging.md", "Notes/merging.md", "merging.md", "notes/sub/merging.md"}},
		{"any one of several", []string{"a.txt", "docs/**"},
			[]string{"a.txt", "docs/b.md"},
			[]string{"b.txt", "A.txt"}},
		{"nothing listed", nil, nil, []string{"a.txt", "docs/b.md"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := Paths{ConflictMarkersIntended: c.patterns}
			for _, rel := range c.yes {
				if !p.IntendsConflictMarkers(rel) {
					t.Errorf("%q does not intend conflict markers in %q", c.patterns, rel)
				}
			}
			for _, rel := range c.no {
				if p.IntendsConflictMarkers(rel) {
					t.Errorf("%q intends conflict markers in %q", c.patterns, rel)
				}
			}
		})
	}
}

// More of THREATS T9: an entry with a control character in it, one that
// path.Match cannot read, and one that is only a dot are as unable to mean what
// they say as the others, and each is refused at load under the setting's name
// with the entry quoted, so that a person can find it.
func TestAC3IntendedPathsWithAControlCharacterOrABrokenPatternAreRefusedAtLoad(t *testing.T) {
	for _, c := range []struct{ name, list, entry string }{
		{"AC-3 a control character", `["docs/a\u0007b.md"]`, `"docs/a\ab.md"`},
		{"AC-3 a line break", `["docs/a\nb.md"]`, `"docs/a\nb.md"`},
		{"AC-3 a bracket never closed", `["docs/["]`, `"docs/["`},
		{"AC-3 a range never closed", `["notes/[a-"]`, `"notes/[a-"`},
		{"AC-3 a backslash where an escape would be", `["docs/x\\/y"]`, `"docs/x\\/y"`},
		{"AC-3 only a dot", `["."]`, `"."`},
		{"AC-3 a dot segment", `["docs/./a.md"]`, `"docs/./a.md"`},
		{"AC-3 a slash at the end", `["docs/"]`, `"docs/"`},
		{"AC-3 a bad entry after good ones", `["docs/**","notes.txt","docs/["]`, `"docs/["`},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, `{"paths":{"`+intendedKey+`":`+c.list+`}}`)
			_, err := Load(root)
			var e *sdlcerr.Error
			if !errors.As(err, &e) || e.Code != sdlcerr.ConfigInvalid {
				t.Fatalf("Load answered %v, want a refusal under %s", err, sdlcerr.ConfigInvalid)
			}
			said := e.What + " " + e.Why + " " + err.Error()
			if !strings.Contains(said, "paths."+intendedKey) || !strings.Contains(said, c.entry) {
				t.Errorf("the refusal does not name paths.%s and the entry %s: %q", intendedKey, c.entry, said)
			}
		})
	}
}

// A path git has no file at -- an empty, "." or ".." segment -- is never
// exempt, whatever is listed: a pattern such as docs/* matches the segment
// "..", and the exemption must not follow it out of docs.
func TestAC3APathWithAnEmptyOrDotSegmentIsNeverExemptWhateverIsListed(t *testing.T) {
	p := Paths{ConflictMarkersIntended: []string{"**", "docs/*", "docs/**", "**/*.md"}}
	for _, rel := range []string{"docs/.", "docs/..", "docs/../a.md", "docs/./a.md", "docs//a.md", "docs/", "/docs/a.md", ".", "..", "./a.md", "../a.md", "a/../docs/b.md"} {
		if p.IntendsConflictMarkers(rel) {
			t.Errorf("conflict markers are intended in %q", rel)
		}
	}
	for _, rel := range []string{"docs/a.md", "a.md", "x/y/z.md"} {
		if !p.IntendsConflictMarkers(rel) {
			t.Errorf("conflict markers are not intended in %q, which the list names", rel)
		}
	}
}
