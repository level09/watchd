package store

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRunEvidenceRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	now := time.Date(2026, 7, 10, 8, 30, 0, 0, time.UTC)
	run := &Run{
		Agent:     "scan",
		Goal:      "product",
		GoalHash:  "goal123",
		AgentHash: "agent123",
		Status:    "success",
		StartedAt: now,
		Allocation: &Allocation{
			Score:            7.5,
			ReservedUSD:      0.25,
			EstimatedCostUSD: 0.12,
			RemainingUSD:     0.80,
			Reason:           "highest verified return",
		},
		OutcomeRatings: []OutcomeRating{
			{Value: "neutral", Source: "verify", RatedAt: now},
			{Value: "useful", Source: "human", Note: "found the release blocker", RatedAt: now.Add(time.Minute)},
		},
		VerificationBefore: &Verification{Command: "test -f ready", Passed: false, ExitCode: 1, DurationMS: 4},
		VerificationAfter:  &Verification{Command: "test -f ready", Passed: true, ExitCode: 0, DurationMS: 3},
	}

	if err := s.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != "product" || got.GoalHash != "goal123" || got.Allocation == nil {
		t.Fatalf("portfolio evidence lost: %+v", got)
	}
	if got.VerificationBefore == nil || got.VerificationAfter == nil || !got.VerificationAfter.Passed {
		t.Fatalf("verification evidence lost: %+v", got)
	}
	latest := got.LatestOutcome()
	if latest == nil || latest.Value != "useful" || latest.Note != "found the release blocker" {
		t.Fatalf("latest outcome = %+v", latest)
	}
}

func TestAppendOutcome(t *testing.T) {
	s := New(t.TempDir())
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	run := &Run{Agent: "scan", Goal: "product", Status: "success", StartedAt: now}
	if err := s.SaveRun(run); err != nil {
		t.Fatal(err)
	}

	got, err := s.AppendOutcome(run.ID, OutcomeRating{
		Value: "useful", Source: "human", Note: "actionable", RatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if latest := got.LatestOutcome(); latest == nil || latest.Value != "useful" {
		t.Fatalf("latest outcome = %+v", latest)
	}

	pending := &Run{Agent: "scan", Goal: "product", Status: "pending", StartedAt: now.Add(time.Hour)}
	if err := s.SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendOutcome(pending.ID, OutcomeRating{Value: "neutral", Source: "human", RatedAt: now}); err == nil {
		t.Fatal("expected pending outcome to be rejected")
	}
	if _, err := s.AppendOutcome(run.ID, OutcomeRating{Value: "great", Source: "human", RatedAt: now}); err == nil {
		t.Fatal("expected invalid outcome value to be rejected")
	}
}

func TestSaveRunDoesNotCollideWithinSecond(t *testing.T) {
	s := New(t.TempDir())
	started := time.Date(2026, 7, 10, 10, 0, 0, 123, time.UTC)
	first := &Run{Agent: "scan", Status: "success", StartedAt: started}
	second := &Run{Agent: "scan", Status: "satisfied", StartedAt: started.Add(time.Millisecond)}
	if err := s.SaveRun(first); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRun(second); err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("run IDs collided: %q", first.ID)
	}
	runs, err := s.GetRuns("scan", 0)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
}

func TestGetRunsReportsCorruptLedgerEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "runs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runs", "corrupt.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir).GetRuns("", 0); err == nil || !strings.Contains(err.Error(), "corrupt.json") {
		t.Fatalf("corrupt ledger error = %v", err)
	}
}

func TestSaveRunLeavesNoPartialFiles(t *testing.T) {
	dir := t.TempDir()
	run := &Run{Agent: "scan", Status: "success", StartedAt: time.Now()}
	if err := New(dir).SaveRun(run); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("ledger entries = %+v", entries)
	}
}
