package daemon

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/portfolio"
	"github.com/level09/watchd/internal/store"
)

func TestPrintingRunShowsPortfolioResult(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })
	runFn := printingRun(func(*portfolio.ResolvedAgent, *store.Allocation, *store.Store) (*store.Run, error) {
		return &store.Run{Agent: "scan", Status: "success"}, nil
	})
	if _, err := runFn(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	w.Close()
	output, _ := io.ReadAll(r)
	if !strings.Contains(string(output), "scan") {
		t.Fatalf("output = %q", output)
	}
}

func TestRunPortfolioDueUsesScoreOrderAndActualSpend(t *testing.T) {
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.Local)
	goal := &portfolio.Goal{Name: "product", Weight: 1, Authority: "act", Hash: "g1"}
	alpha := daemonAgent("alpha", "a1", 0.10, goal)
	beta := daemonAgent("beta", "a2", 0.10, goal)
	s := store.New(t.TempDir())
	var order []string
	runFn := func(resolved *portfolio.ResolvedAgent, allocation *store.Allocation, s *store.Store) (*store.Run, error) {
		order = append(order, resolved.Agent.Name)
		run := &store.Run{
			Agent: resolved.Agent.Name, Goal: goal.Name, GoalHash: goal.Hash, AgentHash: resolved.Agent.Hash,
			Status: "success", CostUSD: 0.12, StartedAt: now.Add(time.Duration(len(order)) * time.Second), Allocation: allocation,
			OutcomeRatings: []store.OutcomeRating{{Value: "useful", Source: "verify", RatedAt: now}},
		}
		return run, s.SaveRun(run)
	}

	decisions, err := runPortfolioDue(now, portfolio.Policy{DailyBudget: 0.15, Exploration: 0, MaxUnrated: 5, MaxPending: 3}, map[string]*portfolio.Goal{"product": goal}, []*portfolio.ResolvedAgent{beta, alpha}, s, runFn)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 1 || order[0] != "alpha" {
		t.Fatalf("order = %v", order)
	}
	if len(decisions) != 2 || decisions[1].Reason != "global daily budget exhausted" {
		t.Fatalf("decisions = %+v", decisions)
	}
}

func TestRunPortfolioDuePausesForUnratedOutput(t *testing.T) {
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.Local)
	goal := &portfolio.Goal{Name: "product", Weight: 1, Authority: "observe", Hash: "g1"}
	scan := daemonAgent("scan", "a1", 0.10, goal)
	s := store.New(t.TempDir())
	calls := 0
	runFn := func(resolved *portfolio.ResolvedAgent, allocation *store.Allocation, s *store.Store) (*store.Run, error) {
		calls++
		run := &store.Run{Agent: "scan", Goal: "product", GoalHash: "g1", AgentHash: "a1", Status: "success", CostUSD: 0.05, StartedAt: now.Add(time.Duration(calls) * time.Second), Allocation: allocation}
		return run, s.SaveRun(run)
	}
	policy := portfolio.Policy{DailyBudget: 1, Exploration: 0.15, MaxUnrated: 5, MaxPending: 3}
	goals := map[string]*portfolio.Goal{"product": goal}
	if _, err := runPortfolioDue(now, policy, goals, []*portfolio.ResolvedAgent{scan}, s, runFn); err != nil {
		t.Fatal(err)
	}
	decisions, err := runPortfolioDue(now.Add(time.Minute), policy, goals, []*portfolio.ResolvedAgent{scan}, s, runFn)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || decisions[0].Reason != "agent has unrated output" {
		t.Fatalf("calls=%d decisions=%+v", calls, decisions)
	}
	runs, _ := s.GetRuns("scan", 1)
	if _, err := s.AppendOutcome(runs[0].ID, store.OutcomeRating{Value: "useful", Source: "human", RatedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runPortfolioDue(now.Add(3*time.Minute), policy, goals, []*portfolio.ResolvedAgent{scan}, s, runFn); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("rated agent was not resumed, calls=%d", calls)
	}
}

func TestRunPortfolioDueResetsBudgetNextDay(t *testing.T) {
	now := time.Date(2026, 7, 10, 23, 0, 0, 0, time.Local)
	goal := &portfolio.Goal{Name: "product", Weight: 1, Authority: "act", Hash: "g1"}
	scan := daemonAgent("scan", "a1", 0.10, goal)
	s := store.New(t.TempDir())
	prior := &store.Run{Agent: "other", Goal: "product", Status: "success", CostUSD: 0.10, StartedAt: now}
	if err := s.SaveRun(prior); err != nil {
		t.Fatal(err)
	}
	calls := 0
	runFn := func(resolved *portfolio.ResolvedAgent, allocation *store.Allocation, s *store.Store) (*store.Run, error) {
		calls++
		run := &store.Run{Agent: "scan", Goal: "product", GoalHash: "g1", AgentHash: "a1", Status: "success", StartedAt: now.Add(25 * time.Hour), Allocation: allocation}
		return run, s.SaveRun(run)
	}
	policy := portfolio.Policy{DailyBudget: 0.10, Exploration: 0.15, MaxUnrated: 5, MaxPending: 3}
	goals := map[string]*portfolio.Goal{"product": goal}
	if _, err := runPortfolioDue(now, policy, goals, []*portfolio.ResolvedAgent{scan}, s, runFn); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("agent ran after today's budget was exhausted")
	}
	if _, err := runPortfolioDue(now.Add(25*time.Hour), policy, goals, []*portfolio.ResolvedAgent{scan}, s, runFn); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("agent did not run after day rollover")
	}
}

func daemonAgent(name, hash string, budget float64, goal *portfolio.Goal) *portfolio.ResolvedAgent {
	return &portfolio.ResolvedAgent{
		Agent: &agent.Agent{Name: name, Goal: goal.Name, Hash: hash, Budget: budget, Model: "sonnet", Mode: "default"},
		Goal:  goal, Authority: goal.Authority,
	}
}
