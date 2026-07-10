package portfolio

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/store"
)

func TestLoadPolicyDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watchd.yaml")
	if err := os.WriteFile(path, []byte("daily_budget: 1.5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.DailyBudget != 1.5 || p.Exploration != 0.15 || p.MaxUnrated != 5 || p.MaxPending != 3 {
		t.Fatalf("policy defaults = %+v", p)
	}
}

func TestRejectInvalidPolicy(t *testing.T) {
	tests := []string{
		"daily_budget: 0\n",
		"daily_budget: -1\n",
		"daily_budget: 1\nexploration: 1.1\n",
		"daily_budget: 1\nexploration: -0.1\n",
		"daily_budget: 1\nmax_unrated: -1\n",
		"daily_budget: .nan\n",
	}
	for _, content := range tests {
		path := filepath.Join(t.TempDir(), "watchd.yaml")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPolicy(path); err == nil {
			t.Fatalf("expected invalid policy for %q", content)
		}
	}
}

func TestDiscoverGoalsSemanticHash(t *testing.T) {
	dir := t.TempDir()
	writeGoal(t, dir, "product.md", `---
name: product
weight: 2
daily_budget: 0.50
authority: propose
---
Ship useful software.`)
	goals, err := DiscoverGoals(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := goals["product"]
	if first == nil || first.Weight != 2 || first.Authority != "propose" || first.Hash == "" {
		t.Fatalf("goal = %+v", first)
	}

	writeGoal(t, dir, "product.md", `---
name: product
weight: 9
daily_budget: 0.90
authority: propose
---
Ship useful software.`)
	goals, err = DiscoverGoals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if goals["product"].Hash != first.Hash {
		t.Fatal("weight or budget changed semantic hash")
	}

	writeGoal(t, dir, "product.md", `---
name: product
weight: 9
authority: observe
---
Ship useful software.`)
	goals, _ = DiscoverGoals(dir)
	if goals["product"].Hash == first.Hash {
		t.Fatal("authority change did not change semantic hash")
	}
}

func TestDiscoverGoalsReportsAllInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoal(t, dir, "bad-weight.md", "---\nweight: 0\n---\nGoal")
	writeGoal(t, dir, "bad-authority.md", "---\nauthority: dream\n---\nGoal")
	if _, err := DiscoverGoals(dir); err == nil {
		t.Fatal("expected discovery error")
	}
}

func TestResolvePortfolioAgent(t *testing.T) {
	goals := map[string]*Goal{
		"product": {Name: "product", Weight: 1, Authority: "act", Hash: "g1"},
	}
	p := &Policy{DailyBudget: 1, Exploration: 0.15, MaxUnrated: 5, MaxPending: 3}
	a := &agent.Agent{Name: "repair", Goal: "product", Budget: 0.25, Gate: true}
	resolved, err := Resolve(a, goals, p)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Authority != "propose" || resolved.Goal.Hash != "g1" {
		t.Fatalf("resolved = %+v", resolved)
	}

	if _, err := Resolve(&agent.Agent{Name: "missing", Goal: "other", Budget: 0.1}, goals, p); err == nil {
		t.Fatal("expected unknown goal error")
	}
	if _, err := Resolve(&agent.Agent{Name: "free", Goal: "product"}, goals, p); err == nil {
		t.Fatal("expected missing budget error")
	}
}

func writeGoal(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAllocatePrefersUsefulValuePerDollar(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	goal := &Goal{Name: "product", Weight: 1, Authority: "act", Hash: "g1"}
	cheap := resolved("cheap", "a1", 0.10, goal)
	expensive := resolved("expensive", "a2", 0.50, goal)
	runs := []store.Run{
		ratedRun(now, cheap, 0.08, "useful"),
		ratedRun(now, expensive, 0.45, "useful"),
	}
	snapshot, err := BuildSnapshot(now, Policy{DailyBudget: 2, Exploration: 0, MaxUnrated: 5, MaxPending: 3}, map[string]*Goal{"product": goal}, []*ResolvedAgent{expensive, cheap}, runs)
	if err != nil {
		t.Fatal(err)
	}
	decisions := Decide(snapshot)
	if len(decisions) != 2 || decisions[0].Agent.Agent.Name != "cheap" || !decisions[0].Admit {
		t.Fatalf("decisions = %+v", decisions)
	}
}

func TestAllocateRespectsWeightAndGoalCap(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	highGoal := &Goal{Name: "health", Weight: 3, DailyBudget: 0.15, Authority: "observe", Hash: "gh"}
	lowGoal := &Goal{Name: "product", Weight: 1, Authority: "act", Hash: "gp"}
	high := resolved("health-scan", "ah", 0.10, highGoal)
	low := resolved("repo-scan", "ap", 0.10, lowGoal)
	runs := []store.Run{{Agent: "prior-health", Goal: "health", CostUSD: 0.10, StartedAt: now.Add(-time.Hour), Status: "success"}}
	snapshot, err := BuildSnapshot(now, Policy{DailyBudget: 1, Exploration: 0, MaxUnrated: 5, MaxPending: 3}, map[string]*Goal{"health": highGoal, "product": lowGoal}, []*ResolvedAgent{low, high}, runs)
	if err != nil {
		t.Fatal(err)
	}
	decisions := Decide(snapshot)
	if decisions[0].Agent.Agent.Name != "repo-scan" {
		t.Fatalf("goal cap should skip health agent: %+v", decisions)
	}
	if decisions[1].Reason != "goal daily budget exhausted" {
		t.Fatalf("skip reason = %q", decisions[1].Reason)
	}
}

func TestAllocateExploresNewStrategy(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	goal := &Goal{Name: "product", Weight: 1, Authority: "act", Hash: "g1"}
	known := resolved("known", "a1", 0.10, goal)
	newAgent := resolved("new", "a2", 0.10, goal)
	runs := []store.Run{
		ratedRun(now.Add(-2*time.Hour), known, 0.10, "neutral"),
		ratedRun(now.Add(-time.Hour), known, 0.10, "neutral"),
	}
	snapshot, err := BuildSnapshot(now, Policy{DailyBudget: 1, Exploration: 1, MaxUnrated: 5, MaxPending: 3}, map[string]*Goal{"product": goal}, []*ResolvedAgent{known, newAgent}, runs)
	if err != nil {
		t.Fatal(err)
	}
	decisions := Decide(snapshot)
	if decisions[0].Agent.Agent.Name != "new" {
		t.Fatalf("new strategy did not receive exploration: %+v", decisions)
	}
}

func TestAllocateEnforcesReviewDebt(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	goal := &Goal{Name: "product", Weight: 1, Authority: "propose", Hash: "g1"}
	a := resolved("scan", "a1", 0.10, goal)
	runs := []store.Run{
		{Agent: "other", Goal: "product", Status: "pending", StartedAt: now.Add(-time.Hour)},
		{Agent: "scan", Goal: "product", GoalHash: "g1", AgentHash: "a1", Status: "success", StartedAt: now.Add(-2 * time.Hour)},
	}
	snapshot, err := BuildSnapshot(now, Policy{DailyBudget: 1, Exploration: 0.15, MaxUnrated: 1, MaxPending: 1}, map[string]*Goal{"product": goal}, []*ResolvedAgent{a}, runs)
	if err != nil {
		t.Fatal(err)
	}
	decisions := Decide(snapshot)
	if decisions[0].Admit || decisions[0].Reason != "pending review cap reached" {
		t.Fatalf("decision = %+v", decisions[0])
	}

	goal.Authority = "observe"
	snapshot, _ = BuildSnapshot(now, Policy{DailyBudget: 1, Exploration: 0.15, MaxUnrated: 1, MaxPending: 2}, map[string]*Goal{"product": goal}, []*ResolvedAgent{a}, runs)
	decisions = Decide(snapshot)
	if decisions[0].Admit || decisions[0].Reason != "agent has unrated output" {
		t.Fatalf("decision = %+v", decisions[0])
	}
}

func TestScoreIgnoresOldStrategyAndPenalizesHarm(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	goal := &Goal{Name: "product", Weight: 1, Authority: "act", Hash: "g2"}
	clean := resolved("clean", "a2", 0.10, goal)
	harmful := resolved("harmful", "a3", 0.10, goal)
	runs := []store.Run{
		{Agent: "clean", Goal: "product", GoalHash: "g1", AgentHash: "a1", CostUSD: 0.10, Status: "success", StartedAt: now.Add(-time.Hour), OutcomeRatings: []store.OutcomeRating{{Value: "harmful", Source: "human", RatedAt: now}}},
		ratedRun(now.Add(-time.Hour), harmful, 0.10, "harmful"),
	}
	snapshot, err := BuildSnapshot(now, Policy{DailyBudget: 1, Exploration: 0, MaxUnrated: 5, MaxPending: 3}, map[string]*Goal{"product": goal}, []*ResolvedAgent{harmful, clean}, runs)
	if err != nil {
		t.Fatal(err)
	}
	decisions := Decide(snapshot)
	if decisions[0].Agent.Agent.Name != "clean" {
		t.Fatalf("old strategy leaked or harm was not penalized: %+v", decisions)
	}
}

func TestDecideUsesStableTieBreakers(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	goal := &Goal{Name: "product", Weight: 1, Authority: "act", Hash: "g1"}
	a := resolved("alpha", "a1", 0.10, goal)
	b := resolved("beta", "a2", 0.10, goal)
	snapshot, err := BuildSnapshot(now, Policy{DailyBudget: 1, Exploration: 0, MaxUnrated: 5, MaxPending: 3}, map[string]*Goal{"product": goal}, []*ResolvedAgent{b, a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := Decide(snapshot)
	second := Decide(snapshot)
	if first[0].Agent.Agent.Name != "alpha" || second[0].Agent.Agent.Name != "alpha" {
		t.Fatalf("unstable decisions: %+v %+v", first, second)
	}
}

func TestBuildSnapshotRejectsNonFiniteInputs(t *testing.T) {
	goal := &Goal{Name: "product", Weight: math.Inf(1), Authority: "act", Hash: "g1"}
	a := resolved("scan", "a1", 0.10, goal)
	if _, err := BuildSnapshot(time.Now(), Policy{DailyBudget: 1}, map[string]*Goal{"product": goal}, []*ResolvedAgent{a}, nil); err == nil {
		t.Fatal("expected non-finite weight error")
	}
}

func resolved(name, hash string, budget float64, goal *Goal) *ResolvedAgent {
	return &ResolvedAgent{Agent: &agent.Agent{Name: name, Hash: hash, Goal: goal.Name, Budget: budget}, Goal: goal, Authority: goal.Authority}
}

func ratedRun(at time.Time, a *ResolvedAgent, cost float64, value string) store.Run {
	return store.Run{
		Agent: a.Agent.Name, Goal: a.Goal.Name, GoalHash: a.Goal.Hash, AgentHash: a.Agent.Hash,
		CostUSD: cost, Status: "success", StartedAt: at,
		OutcomeRatings: []store.OutcomeRating{{Value: value, Source: "human", RatedAt: at}},
	}
}
