package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/level09/watchd/internal/store"
)

func TestOutcomeCommandAppendsRating(t *testing.T) {
	t.Chdir(t.TempDir())
	s := store.New(".")
	run := &store.Run{Agent: "scan", Goal: "product", Status: "success", StartedAt: time.Now()}
	if err := s.SaveRun(run); err != nil {
		t.Fatal(err)
	}

	if _, err := captureOutput(t, func() error {
		return Run([]string{"outcome", run.ID, "useful", "saved", "review", "time"})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest := got.LatestOutcome()
	if latest == nil || latest.Value != "useful" || latest.Note != "saved review time" || latest.Source != "human" {
		t.Fatalf("outcome = %+v", latest)
	}
	if err := Run([]string{"outcome", run.ID, "excellent"}); err == nil {
		t.Fatal("expected invalid outcome error")
	}
}

func TestCheckCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "agents/pass.md", `---
name: pass
goal: product
budget: 0.10
verify: true
---
Check.`)
	writeFile(t, "agents/fail.md", `---
name: fail
goal: product
budget: 0.10
verify: false
---
Check.`)

	output, err := captureOutput(t, func() error { return Run([]string{"check", "pass"}) })
	if err != nil || !strings.Contains(output, "satisfied") || !strings.Contains(output, "true") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"check", "fail"}) }); err == nil {
		t.Fatal("expected failing verifier error")
	}
}

func TestPortfolioCommandExplainsAllocation(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "watchd.yaml", "daily_budget: 1.00\nexploration: 0.15\n")
	writeFile(t, "goals/product.md", `---
name: product
weight: 2
authority: observe
---
Ship useful software.`)
	writeFile(t, "agents/alpha.md", `---
name: alpha
goal: product
schedule: 1h
budget: 0.10
---
Find release blockers.`)
	writeFile(t, "agents/beta.md", `---
name: beta
goal: product
schedule: 1h
budget: 0.20
---
Find weak documentation.`)

	output, err := captureOutput(t, func() error { return Run([]string{"portfolio"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"$1.0000", "product", "alpha", "beta", "highest verified return"} {
		if !strings.Contains(output, want) {
			t.Fatalf("portfolio output missing %q: %s", want, output)
		}
	}
}

func TestListShowsGoal(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "agents/scan.md", `---
name: scan
goal: product
budget: 0.10
---
Scan.`)
	output, err := captureOutput(t, func() error { return Run([]string{"list"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "GOAL") || !strings.Contains(output, "product") {
		t.Fatalf("list output = %q", output)
	}
}

func TestManualRunUsesPortfolioAdmission(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "watchd.yaml", "daily_budget: 0.05\n")
	writeFile(t, "goals/product.md", "---\nname: product\nauthority: act\n---\nShip useful software.")
	writeFile(t, "agents/repair.md", `---
name: repair
goal: product
budget: 0.10
verify: true
---
Repair.`)
	if _, err := captureOutput(t, func() error { return Run([]string{"run", "repair"}) }); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected budget admission error, got %v", err)
	}

	writeFile(t, "watchd.yaml", "daily_budget: 0.20\n")
	if _, err := captureOutput(t, func() error { return Run([]string{"run", "repair"}) }); err != nil {
		t.Fatal(err)
	}
	runs, err := store.New(".").GetRuns("repair", 0)
	if err != nil || len(runs) != 1 || runs[0].Status != "satisfied" || runs[0].Allocation == nil {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
}

func TestApprovalUsesPortfolioAdmission(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "watchd.yaml", "daily_budget: 0.10\n")
	writeFile(t, "goals/product.md", "---\nname: product\nauthority: propose\n---\nShip useful software.")
	writeFile(t, "agents/repair.md", "---\nname: repair\ngoal: product\nbudget: 0.20\n---\nRepair.")
	s := store.New(".")
	pending := &store.Run{Agent: "repair", Goal: "product", GoalHash: "g", AgentHash: "a", Status: "pending", SessionID: "session", StartedAt: time.Now()}
	if err := s.SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"approve", pending.ID}) }); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected approval budget error, got %v", err)
	}
}

func TestRejectRecordsNeutralOutcome(t *testing.T) {
	t.Chdir(t.TempDir())
	s := store.New(".")
	pending := &store.Run{Agent: "repair", Goal: "product", Status: "pending", StartedAt: time.Now()}
	if err := s.SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"reject", pending.ID}) }); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetRun(pending.ID)
	if latest := got.LatestOutcome(); got.Status != "rejected" || latest == nil || latest.Value != "neutral" || latest.Source != "verify" {
		t.Fatalf("rejected run = %+v", got)
	}
}

func captureOutput(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = old
	data, readErr := io.ReadAll(r)
	r.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(data), runErr
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	path := filepath.Clean(name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
