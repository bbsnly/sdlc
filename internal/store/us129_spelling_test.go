package store

// US-129 AC-4, the second spelling of a loop file's path at the same length
// (US-129's design review: security, round 6; PLAN.md section 10.5): the
// invocation's Reads keeps what it read of the backlog, a gate record or the
// freeze under the path it was asked with, and a write through another
// spelling of that path keeps and drops under that other spelling only. The
// next read under the first is then answered only if the file is the one that
// was read, by identity, size and modification time. A write at the same
// length, on a clock too coarse to tell the two writes apart, leaves the
// identity alone to say the file changed. The store writes by rename, so the
// file is a new one; on macOS and Linux os.Stat reads its identity at once and
// this passes, while on Windows a FileInfo from os.Stat looks its identity up
// by path only when asked, so the kept read and the new stat both name the new
// file and the stale bytes would be handed out. The row can be red only there.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// us129Spelling is two paths of one directory.
type us129Spelling struct {
	name     string
	one, two string
}

// us129LoopFile is one of the files the invocation's Reads keeps: how it is
// set up, read and written through a store, and two values of the same length.
type us129LoopFile struct {
	name          string
	path          func(t *testing.T, s *Store) string
	setup         func(s *Store, value string) error
	read          func(s *Store) (string, error)
	write         func(s *Store, value string) error
	first, second string
}

func us129LoopFiles() []us129LoopFile {
	return []us129LoopFile{
		{
			name: "gate record",
			path: func(t *testing.T, s *Store) string {
				t.Helper()
				dir, err := s.StoryDir("US-001")
				if err != nil {
					t.Fatal(err)
				}
				return filepath.Join(dir, recordFile)
			},
			setup: func(s *Store, value string) error {
				r := model.NewRecord("US-001", us129At)
				r.SetGate(model.GateDoR, model.GatePass, value, us129At)
				return s.SaveRecord(r)
			},
			read: func(s *Store) (string, error) {
				r, err := s.Record("US-001")
				if err != nil {
					return "", err
				}
				return r.Gates[model.GateDoR].Note, nil
			},
			write: func(s *Store, value string) error {
				r, err := s.Record("US-001")
				if err != nil {
					return err
				}
				r.SetGate(model.GateDoR, model.GatePass, value, us129At)
				return s.SaveRecord(r)
			},
			first:  "written through spelling one",
			second: "written through spelling two",
		},
		{
			name: "backlog",
			path: func(_ *testing.T, s *Store) string { return s.BacklogPath() },
			setup: func(s *Store, value string) error {
				backlog := `{"stories":[{"id":"US-001","title":"Invoices","status":"` + value + `",` +
					`"updated":"` + model.Timestamp(us129At) + `",` +
					`"acceptance_criteria":[{"id":"AC-1","text":"An invoice has a number."}]}]}` + "\n"
				return os.WriteFile(s.BacklogPath(), []byte(backlog), 0o644)
			},
			read: func(s *Store) (string, error) {
				b, err := s.Backlog()
				if err != nil {
					return "", err
				}
				story, ok := b.Find("US-001")
				if !ok {
					return "", os.ErrNotExist
				}
				return string(story.Status), nil
			},
			write: func(s *Store, value string) error {
				return s.SetStoryStatus("US-001", model.Status(value))
			},
			first:  string(model.StatusBlocked),
			second: string(model.StatusDropped),
		},
		{
			name: "tests.lock",
			path: func(_ *testing.T, s *Store) string {
				return filepath.Join(s.root, filepath.FromSlash(lockFile))
			},
			setup: func(s *Store, value string) error {
				return s.SaveLock(model.NewLock("US-001", map[string]string{"a_test.go": value}, us129At))
			},
			read: func(s *Store) (string, error) {
				l, err := s.Lock()
				if err != nil || l == nil {
					return "", err
				}
				return l.Files["a_test.go"], nil
			},
			write: func(s *Store, value string) error {
				return s.SaveLock(model.NewLock("US-001", map[string]string{"a_test.go": value}, us129At))
			},
			first:  strings.Repeat("1", 64),
			second: strings.Repeat("2", 64),
		},
	}
}

func TestAC4ALoopFileWrittenAtTheSameLengthUnderAnotherSpellingOfItsPathIsReadAsWritten(t *testing.T) {
	spellings, none := us129Spellings(t)
	if len(spellings) == 0 {
		t.Skip(none)
	}
	for _, sp := range spellings {
		for i, way := range [][2]string{{sp.one, sp.two}, {sp.two, sp.one}} {
			for _, f := range us129LoopFiles() {
				project := strings.ReplaceAll(f.name, " ", "-") + "-read-under-" + [2]string{"the-first", "the-second"}[i]
				readAt := filepath.Join(way[0], project)
				writeAt := filepath.Join(way[1], project)
				t.Run(sp.name+"/"+project, func(t *testing.T) {
					if err := os.MkdirAll(readAt, 0o755); err != nil {
						t.Fatal(err)
					}
					s := us129Project(t, readAt).WithClock(func() time.Time { return us129At })
					if err := f.setup(s, f.first); err != nil {
						t.Fatal(err)
					}
					s.Reads().Forget()
					if got, err := f.read(s); err != nil || got != f.first {
						t.Fatalf("read under %s before the write = %q, %v; want %q", readAt, got, err, f.first)
					}
					path := f.path(t, s)
					before, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					other, err := s.At(writeAt)
					if err != nil {
						t.Fatal(err)
					}
					if err := f.write(other, f.second); err != nil {
						t.Fatal(err)
					}
					after, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if after.Size() != before.Size() {
						t.Fatalf("the write under %s changed the %s from %d to %d bytes; the row needs the same length", writeAt, f.name, before.Size(), after.Size())
					}
					// A clock too coarse to tell the two writes apart: the
					// modification time says nothing, and only the identity can.
					if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
						t.Fatal(err)
					}
					got, err := f.read(s)
					if err != nil {
						t.Fatal(err)
					}
					if got != f.second {
						t.Errorf("the %s written under %s and read again under %s = %q; want %q, what was written", f.name, writeAt, readAt, got, f.second)
					}
				})
			}
		}
	}
}
