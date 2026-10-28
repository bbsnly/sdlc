package store

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// The sections a story's bookkeeper stored for the root CODEMAP.md are kept
// from the storing until they are on trunk or a person has dropped them, at
// <git-dir>/sdlc/codemap/<ID>.md in the git directory every checkout shares.
// The story's worktree goes when the story lands, and its own git directory
// with it; a file in the working tree stops the landing (SDLC-E0032); and
// nothing here is ever committed or pushed.
//
// The store keeps bytes. What a section is, and whether these are any, is
// internal/codemap's to say and the caller's to ask again every time it reads
// them: the file is only as safe from a session as .git is.

// MapSectionsRel is where the sections are kept, under the common git
// directory.
const MapSectionsRel = "sdlc/codemap"

// mapSectionsMost is as much of a kept file as is read. It is over any set
// that could have been stored, so a file cut off here is one the caller's own
// bound refuses; and a file somebody put in its place is not read without end
// while a landing waits.
const mapSectionsMost = 1 << 20

const theMapSections = "the sections kept for CODEMAP.md"

// mapSectionsFile is where one story's sections are kept. The id passes
// CheckID before it is part of a path, and the directory is the one the
// claims are kept beside: worked out from git's own files, with the checks a
// rewritten pointer needs.
func (s *Store) mapSectionsFile(story string) (string, error) {
	if err := CheckID(story); err != nil {
		return "", err
	}
	dir, err := s.mapSectionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, story+".md"), nil
}

func (s *Store) mapSectionsDir() (string, error) {
	co, err := s.checkout()
	if err != nil || co.Common == "" {
		return "", sdlcerr.New(sdlcerr.StateUnreadable,
			theMapSections+" have nowhere to be",
			"they are kept in the repository's git directory, and "+quote(s.root)+" is not in a git repository").WithCause(err)
	}
	return filepath.Join(co.Common, filepath.FromSlash(MapSectionsRel)), nil
}

// WriteMapSections keeps content as the story's sections, in place of any it
// had.
func (s *Store) WriteMapSections(story string, content []byte) error {
	path, err := s.mapSectionsFile(story)
	if err != nil {
		return err
	}
	if err := fsx.WriteFileAtomic(path, content, 0o644); err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			theMapSections+" could not be written for "+story,
			quote(path)+" could not be written").WithCause(err)
	}
	return nil
}

// MapSections is the story's kept sections, and whether it has any. Only a
// plain file is read, and no more of it than mapSectionsMost: a link, a
// directory or a device in its place is an error, not nothing, because read
// as nothing a story's map change would vanish with nobody told.
func (s *Store) MapSections(story string) ([]byte, bool, error) {
	path, err := s.mapSectionsFile(story)
	if err != nil {
		return nil, false, err
	}
	unreadable := func(why string, cause error) ([]byte, bool, error) {
		return nil, false, sdlcerr.New(sdlcerr.StateUnreadable,
			theMapSections+" could not be read for "+story, quote(path)+" "+why).WithCause(cause)
	}
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return nil, false, nil
	case err != nil:
		return unreadable("could not be looked at", err)
	case !info.Mode().IsRegular():
		return unreadable("is not a plain file", nil)
	}
	f, err := os.Open(path)
	if err != nil {
		return unreadable("could not be opened", err)
	}
	defer func() { _ = f.Close() }()
	content, err := io.ReadAll(io.LimitReader(f, mapSectionsMost))
	if err != nil {
		return unreadable("could not be read", err)
	}
	return content, true, nil
}

// DropMapSections removes the story's kept sections. Dropping what is not
// there is not a failure: the caller drops after a commit whether or not a
// kill came between.
func (s *Store) DropMapSections(story string) error {
	path, err := s.mapSectionsFile(story)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			theMapSections+" could not be removed for "+story,
			quote(path)+" is still there").WithCause(err)
	}
	return nil
}

// StoriesWithMapSections is every story that has sections kept, in order. A
// name that is no story id, a directory and a leftover temporary file are not
// one story's sections.
func (s *Store) StoriesWithMapSections() ([]string, error) {
	dir, err := s.mapSectionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	// A directory that is not there holds no sections; a file in its place is
	// asked again, since Windows answers it with ENOTDIR, which is
	// ERROR_PATH_NOT_FOUND there and so reads as not there: a file at
	// codemap was listed as no sections on windows-latest (CI on 38e0ed0).
	if err != nil && (!os.IsNotExist(err) || inTheWay(dir)) {
		return nil, sdlcerr.New(sdlcerr.StateUnreadable,
			theMapSections+" could not be listed",
			quote(dir)+" could not be read").WithCause(err)
	}
	var out []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".md"); ok && e.Type().IsRegular() && CheckID(id) == nil {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

// inTheWay is whether something is at path, through a link, though it could
// not be listed as a directory. A link to nothing is not: like a directory
// that is not there, it holds no sections.
func inTheWay(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
