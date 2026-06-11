package store

import (
	"testing"
	"time"
)

func TestSaveRunUpsertByID(t *testing.T) {
	s := New(t.TempDir())

	run := &Run{Agent: "scan", Status: "pending", Result: "plan", StartedAt: time.Now()}
	if err := s.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if run.ID == "" {
		t.Fatal("SaveRun must assign an ID")
	}

	got, err := s.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" {
		t.Errorf("status = %q", got.Status)
	}

	// Same ID overwrites the same file
	got.Status = "approved"
	if err := s.SaveRun(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetRun(run.ID)
	if again.Status != "approved" {
		t.Errorf("upsert failed, status = %q", again.Status)
	}

	runs, _ := s.GetRuns("scan", 0)
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}
	if runs[0].ID != run.ID {
		t.Errorf("GetRuns ID = %q, want %q", runs[0].ID, run.ID)
	}
}
