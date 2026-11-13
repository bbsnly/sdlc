package store

// US-129 AC-10: the store's own reads and writes, at the sizes this
// repository has them: a gate record of 190 KB (US-109's when the story was
// written) and a backlog of about 130 stories in about 10,000 lines. Each
// iteration opens a store of its own, as each command does, so that what is
// measured is a read from the disk and never an answer a store kept (US-129
// red team, V11); building the files is off the clock.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
)

// us129BenchRecord is a record of id through the design review, padded with
// the agent runs a long story records to 190 KB.
func us129BenchRecord(tb testing.TB, id string) *model.Record {
	tb.Helper()
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	record := model.NewRecord(id, at)
	for _, g := range model.GateImplementation.Before() {
		record.SetGate(g, model.GatePass, "worked by the session", at)
	}
	for i := 0; ; i++ {
		when := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		agent := fmt.Sprintf("agent-%04d", i)
		record.Events = append(record.Events,
			model.Event{At: when, Type: model.EventAgentStart, Message: "sdlc:researcher started", AgentType: "sdlc:researcher", AgentID: agent},
			model.Event{At: when, Type: model.EventAgentEnd, Message: "sdlc:researcher finished", AgentType: "sdlc:researcher", AgentID: agent})
		if i%16 == 0 {
			raw, err := json.MarshalIndent(record, "", "  ")
			if err != nil {
				tb.Fatal(err)
			}
			if len(raw) >= 190<<10 {
				return record
			}
		}
	}
}

// us129BenchBacklog is a backlog of first and 129 finished stories, each with
// the criteria, non-goals and notes a real one carries, indented as the loop
// writes it: about 10,000 lines.
func us129BenchBacklog(tb testing.TB, first ...model.Story) []byte {
	tb.Helper()
	b := model.Backlog{Stories: first}
	words := strings.Repeat("the hook answers every call it is asked without reading a file twice ", 3)
	for i := range 129 {
		s := model.Story{
			ID: fmt.Sprintf("P-%03d", i+1), Title: fmt.Sprintf("Finished story %d", i+1), Status: model.StatusDone,
			AsA: "person working a story", IWant: words, SoThat: words, Notes: words,
		}
		for j := range 14 {
			s.AcceptanceCriteria = append(s.AcceptanceCriteria, model.AcceptanceCriterion{ID: fmt.Sprintf("AC-%d", j+1), Text: words})
		}
		for range 6 {
			s.NonGoals = append(s.NonGoals, words)
		}
		b.Stories = append(b.Stories, s)
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		tb.Fatal(err)
	}
	return append(raw, '\n')
}

// us129BenchProject is a project in a directory of the benchmark's with its
// configuration, the backlog and the record of A-1.
func us129BenchProject(b *testing.B) (string, config.Config) {
	b.Helper()
	root := b.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sdlc"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sdlc", "config.json"), []byte(`{"version":1}`), 0o644); err != nil {
		b.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		b.Fatal(err)
	}
	backlog := us129BenchBacklog(b, model.Story{ID: "A-1", Title: "Invoices", Status: model.StatusInProgress,
		AcceptanceCriteria: []model.AcceptanceCriterion{{ID: "AC-1", Text: "An invoice has a number."}}})
	if err := os.WriteFile(cfg.BacklogPath(root), backlog, 0o644); err != nil {
		b.Fatal(err)
	}
	if err := New(&config.Project{Root: root, Config: cfg}).SaveRecord(us129BenchRecord(b, "A-1")); err != nil {
		b.Fatal(err)
	}
	return root, cfg
}

func BenchmarkRecordRead(b *testing.B) {
	root, cfg := us129BenchProject(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		s := New(&config.Project{Root: root, Config: cfg})
		b.StartTimer()
		if _, err := s.Record("A-1"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecordWrite(b *testing.B) {
	root, cfg := us129BenchProject(b)
	record := us129BenchRecord(b, "A-1")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		s := New(&config.Project{Root: root, Config: cfg})
		b.StartTimer()
		if err := s.SaveRecord(record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBacklogLoad(b *testing.B) {
	root, cfg := us129BenchProject(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		s := New(&config.Project{Root: root, Config: cfg})
		b.StartTimer()
		backlog, err := s.Backlog()
		if err != nil {
			b.Fatal(err)
		}
		if len(backlog.Stories) != 130 {
			b.Fatalf("the backlog read has %d stories, want 130", len(backlog.Stories))
		}
	}
}
