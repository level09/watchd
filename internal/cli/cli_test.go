package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/level09/watchd/internal/portfolio"
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

func TestHarmfulOutcomeNotifies(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	notice := filepath.Join(dir, "notice")
	writeFile(t, "agents/scan.md", "---\nname: scan\nnotify: \"printf '%s' \\\"$WATCHD_STATUS\\\" > "+notice+"\"\n---\nScan.")
	s := store.New(".")
	run := &store.Run{Agent: "scan", Goal: "product", Status: "success", StartedAt: time.Now()}
	if err := s.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"outcome", run.ID, "harmful", "unsafe"}) }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(notice)
	if err != nil || string(data) != "harmful" {
		t.Fatalf("notice=%q err=%v", data, err)
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
	writeFile(t, "agents/edge-scout.md", "---\nname: edge-scout\n---\nResearch manually.")
	s := store.New(".")
	a, _ := loadAgent("alpha")
	goals, _ := portfolio.DiscoverGoals("goals")
	useful := &store.Run{
		Agent: "alpha", Goal: "product", AgentHash: a.Hash, GoalHash: goals["product"].Hash, Status: "success", CostUSD: 0.05, StartedAt: time.Now(),
		OutcomeRatings: []store.OutcomeRating{{Value: "useful", Source: "human", RatedAt: time.Now()}},
	}
	if err := s.SaveRun(useful); err != nil {
		t.Fatal(err)
	}

	output, err := captureOutput(t, func() error { return Run([]string{"portfolio"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "edge-scout") {
		t.Fatalf("manual legacy agent appeared in portfolio output: %q", output)
	}
	for _, want := range []string{"remaining $0.9500", "product", "alpha", "beta", "1 useful", "$0.0500/useful", "1 rated", "$0.0500", "highest verified return"} {
		if !strings.Contains(output, want) {
			t.Fatalf("portfolio output missing %q: %s", want, output)
		}
	}
}

func TestListShowsGoal(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "watchd.yaml", "daily_budget: 1.00\n")
	writeFile(t, "goals/product.md", "---\nname: product\n---\nShip.")
	writeFile(t, "agents/scan.md", `---
name: scan
goal: product
schedule: 1h
budget: 0.10
---
Scan.`)
	writeFile(t, "agents/edge-scout.md", "---\nname: edge-scout\n---\nResearch manually.")
	output, err := captureOutput(t, func() error { return Run([]string{"list"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "GOAL") || !strings.Contains(output, "ELIGIBILITY") || !strings.Contains(output, "product") || !strings.Contains(output, "highest verified return") {
		t.Fatalf("list output = %q", output)
	}
	output, err = captureOutput(t, func() error { return Run(nil) })
	if err != nil || !strings.Contains(output, "product") || !strings.Contains(output, "highest verified return") {
		t.Fatalf("status output = %q, err=%v", output, err)
	}
}

func TestLogsShowOutcomeAndAllocation(t *testing.T) {
	t.Chdir(t.TempDir())
	run := &store.Run{Agent: "scan", Status: "success", StartedAt: time.Now(),
		Allocation:     &store.Allocation{Reason: "highest verified return"},
		OutcomeRatings: []store.OutcomeRating{{Value: "useful", Source: "human", RatedAt: time.Now()}},
	}
	if err := store.New(".").SaveRun(run); err != nil {
		t.Fatal(err)
	}
	output, err := captureOutput(t, func() error { return Run([]string{"logs", "scan"}) })
	if err != nil || !strings.Contains(output, "useful") || !strings.Contains(output, "highest verified return") {
		t.Fatalf("logs output = %q, err=%v", output, err)
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
	pending := &store.Run{Agent: "repair", Goal: "product", Status: "pending", SessionID: "session", StartedAt: time.Now()}
	if err := s.SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"approve", pending.ID}) }); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected approval budget error, got %v", err)
	}
}

func TestApprovalSupersedesRecoveredGoalBeforeBudgetAdmission(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ready := filepath.Join(dir, "ready")
	if err := os.WriteFile(ready, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "watchd.yaml", "daily_budget: 0.01\n")
	writeFile(t, "goals/product.md", "---\nname: product\nauthority: propose\n---\nShip useful software.")
	writeFile(t, "agents/repair.md", "---\nname: repair\ngoal: product\nbudget: 0.20\nverify: test -f "+ready+"\n---\nRepair.")
	pending := &store.Run{Agent: "repair", Goal: "product", Status: "pending", SessionID: "session", StartedAt: time.Now()}
	if err := store.New(".").SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"approve", pending.ID}) }); err != nil {
		t.Fatal(err)
	}
	got, err := store.New(".").GetRun(pending.ID)
	if err != nil || got.Status != "superseded" || got.VerificationAfter == nil || !got.VerificationAfter.Passed {
		t.Fatalf("pending after approval = %+v err=%v", got, err)
	}
}

func TestApprovalRejectsChangedStrategy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "watchd.yaml", "daily_budget: 1.00\n")
	writeFile(t, "goals/product.md", "---\nname: product\nauthority: propose\n---\nShip useful software.")
	writeFile(t, "agents/repair.md", "---\nname: repair\ngoal: product\nbudget: 0.20\n---\nNew instructions.")
	pending := &store.Run{Agent: "repair", Goal: "product", AgentHash: "old-agent", GoalHash: "old-goal", Status: "pending", SessionID: "session", StartedAt: time.Now()}
	if err := store.New(".").SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"approve", pending.ID}) }); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected changed strategy error, got %v", err)
	}
	got, _ := store.New(".").GetRun(pending.ID)
	if got.Status != "pending" {
		t.Fatalf("stale pending status = %q", got.Status)
	}
}

func TestApprovalCannotPromoteObserveGoalToAction(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "watchd.yaml", "daily_budget: 1.00\n")
	writeFile(t, "goals/health.md", "---\nname: health\nauthority: observe\n---\nObserve health safely.")
	writeFile(t, "agents/health.md", "---\nname: health\ngoal: health\nbudget: 0.20\n---\nObserve.")
	pending := &store.Run{Agent: "health", Goal: "health", Status: "pending", SessionID: "session", StartedAt: time.Now()}
	if err := store.New(".").SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"approve", pending.ID}) }); err == nil || !strings.Contains(err.Error(), "observe") {
		t.Fatalf("expected observe authority error, got %v", err)
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

func TestManualLegacyRunStoresPortfolioAdmission(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "watchd.yaml", "daily_budget: 0.20\n")
	writeFile(t, "agents/legacy.md", "---\nname: legacy\nbudget: 0.10\n---\nRun once.")
	writeFile(t, "bin/claude", "#!/bin/sh\nprintf '%s' '[{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"done\",\"total_cost_usd\":0.05}]'\n")
	if err := os.Chmod(filepath.Join("bin", "claude"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := captureOutput(t, func() error { return Run([]string{"run", "legacy"}) }); err != nil {
		t.Fatal(err)
	}
	runs, _ := store.New(".").GetRuns("legacy", 1)
	if len(runs) != 1 || runs[0].Allocation == nil || runs[0].Allocation.Reason != "manual legacy run" {
		t.Fatalf("legacy run = %+v", runs)
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
