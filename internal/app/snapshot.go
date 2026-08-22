package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/portfolio"
	"github.com/level09/watchd/internal/store"
)

func Snapshot(root string, now time.Time) (SnapshotView, error) {
	agentsPath := filepath.Join(root, "agents")
	agents, err := agent.DiscoverStrict(agentsPath)
	if err != nil {
		return SnapshotView{}, fmt.Errorf("load %s: %w", agentsPath, err)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })

	policyPath := filepath.Join(root, "watchd.yaml")
	policy, err := portfolio.LoadPolicy(policyPath)
	if err != nil {
		return SnapshotView{}, fmt.Errorf("load %s: %w", policyPath, err)
	}
	runs, err := store.New(root).GetRuns("", 0)
	if err != nil {
		return SnapshotView{}, err
	}

	if policy == nil {
		return legacySnapshot(now, agents, runs), nil
	}
	return portfolioSnapshot(root, now, *policy, agents, runs)
}

func legacySnapshot(now time.Time, agents []*agent.Agent, runs []store.Run) SnapshotView {
	summary := runSummary(now, runs)
	latest := latestRuns(runs)
	view := SnapshotView{
		GeneratedAt: now,
		Mode:        "legacy",
		Summary:     summary,
		Goals:       []GoalView{},
		Agents:      make([]AgentView, 0, len(agents)),
		RecentRuns:  recentRunViews(runs),
	}
	for _, item := range agents {
		agentView := AgentView{
			Name:       item.Name,
			Schedule:   item.Schedule,
			Model:      item.Model,
			Authority:  legacyAuthority(item),
			BudgetUSD:  item.Budget,
			ManualOnly: item.Schedule == "",
		}
		if run, ok := latest[item.Name]; ok {
			value := runView(run)
			agentView.LatestRun = &value
		}
		view.Agents = append(view.Agents, agentView)
	}
	return view
}

func portfolioSnapshot(root string, now time.Time, policy portfolio.Policy, agents []*agent.Agent, runs []store.Run) (SnapshotView, error) {
	goals, err := portfolio.DiscoverGoals(filepath.Join(root, "goals"))
	if err != nil {
		return SnapshotView{}, fmt.Errorf("load goals: %w", err)
	}
	resolvedByName := make(map[string]*portfolio.ResolvedAgent, len(agents))
	var scheduled []*portfolio.ResolvedAgent
	for _, item := range agents {
		if item.Goal == "" {
			if item.Schedule != "" {
				return SnapshotView{}, fmt.Errorf("agent %q has no goal in portfolio mode", item.Name)
			}
			continue
		}
		resolved, err := portfolio.Resolve(item, goals, &policy)
		if err != nil {
			return SnapshotView{}, err
		}
		resolvedByName[item.Name] = resolved
		if item.Schedule != "" {
			scheduled = append(scheduled, resolved)
		}
	}
	snapshot, err := portfolio.BuildSnapshot(now, policy, goals, scheduled, runs)
	if err != nil {
		return SnapshotView{}, err
	}
	decisions := portfolio.Decide(snapshot)
	decisionsByName := make(map[string]portfolio.Decision, len(decisions))
	for _, decision := range decisions {
		decisionsByName[decision.Agent.Agent.Name] = decision
	}

	dailyBudget := policy.DailyBudget
	remaining := policy.DailyBudget - snapshot.GlobalSpent
	maxPending, maxUnrated := policy.MaxPending, policy.MaxUnrated
	view := SnapshotView{
		GeneratedAt: now,
		Mode:        "portfolio",
		Summary: Summary{
			DailyBudgetUSD: &dailyBudget,
			SpentUSD:       snapshot.GlobalSpent,
			RemainingUSD:   &remaining,
			Pending:        snapshot.Pending,
			MaxPending:     &maxPending,
			Unrated:        snapshot.Unrated,
			MaxUnrated:     &maxUnrated,
		},
		Goals:      goalViews(goals, snapshot, runs),
		Agents:     agentViews(agents, resolvedByName, decisionsByName, snapshot, runs),
		RecentRuns: recentRunViews(runs),
	}
	return view, nil
}

func goalViews(goals map[string]*portfolio.Goal, snapshot portfolio.Snapshot, runs []store.Run) []GoalView {
	names := portfolio.SortedGoalNames(goals)
	views := make([]GoalView, 0, len(names))
	for _, name := range names {
		goal := goals[name]
		var dailyBudget *float64
		if goal.DailyBudget > 0 {
			budget := goal.DailyBudget
			dailyBudget = &budget
		}
		useful, cost := 0, 0.0
		for _, run := range runs {
			if run.Goal != name {
				continue
			}
			cost += run.CostUSD
			if run.Status != "satisfied" && run.LatestOutcome() != nil && run.LatestOutcome().Value == "useful" {
				useful++
			}
		}
		var costPerUseful *float64
		if useful > 0 {
			value := cost / float64(useful)
			costPerUseful = &value
		}
		views = append(views, GoalView{Name: goal.Name, Description: goal.Body, Weight: goal.Weight, Authority: goal.Authority, DailyBudgetUSD: dailyBudget, SpentUSD: snapshot.GoalSpent[name], UsefulOutcomes: useful, CostPerUsefulUSD: costPerUseful})
	}
	return views
}

func agentViews(agents []*agent.Agent, resolved map[string]*portfolio.ResolvedAgent, decisions map[string]portfolio.Decision, snapshot portfolio.Snapshot, runs []store.Run) []AgentView {
	latest := latestRuns(runs)
	views := make([]AgentView, 0, len(agents))
	for _, item := range agents {
		view := AgentView{Name: item.Name, Goal: item.Goal, Schedule: item.Schedule, Model: item.Model, Authority: legacyAuthority(item), BudgetUSD: item.Budget, ManualOnly: item.Schedule == ""}
		if resolvedAgent := resolved[item.Name]; resolvedAgent != nil {
			view.Authority = resolvedAgent.Authority
		}
		if view.ManualOnly {
			view.Decision = "manual only"
		} else if decision, ok := decisions[item.Name]; ok {
			score, estimate := decision.Score, decision.EstimatedCostUSD
			view.Score = &score
			view.EstimatedCostUSD = &estimate
			view.Decision = decision.Reason
			view.RatedSamples = portfolio.StatsFor(snapshot, decision.Agent).Rated
		}
		if run, ok := latest[item.Name]; ok {
			value := runView(run)
			view.LatestRun = &value
		}
		views = append(views, view)
	}
	return views
}

func latestRuns(runs []store.Run) map[string]store.Run {
	latest := make(map[string]store.Run)
	for _, run := range runs {
		if _, ok := latest[run.Agent]; !ok {
			latest[run.Agent] = run
		}
	}
	return latest
}

func recentRunViews(runs []store.Run) []RunView {
	limit := len(runs)
	if limit > 50 {
		limit = 50
	}
	views := make([]RunView, 0, limit)
	for _, run := range runs[:limit] {
		views = append(views, runView(run))
	}
	return views
}

func runView(run store.Run) RunView {
	view := RunView{ID: run.ID, Agent: run.Agent, Goal: run.Goal, Status: run.Status, Result: trimBytes(run.Result, 500), Error: trimBytes(run.Error, 200), CostUSD: run.CostUSD, DurationMS: run.Duration.Milliseconds(), StartedAt: run.StartedAt}
	if outcome := run.LatestOutcome(); outcome != nil {
		view.Outcome = outcome.Value
	}
	if run.Allocation != nil {
		view.AllocationReason = run.Allocation.Reason
	}
	return view
}

func runSummary(now time.Time, runs []store.Run) Summary {
	var summary Summary
	for _, run := range runs {
		if sameLocalDay(run.StartedAt, now) {
			summary.SpentUSD += run.CostUSD
		}
		if run.Status == "pending" {
			summary.Pending++
		}
		if run.Status == "success" && run.LatestOutcome() == nil {
			summary.Unrated++
		}
	}
	return summary
}

func trimBytes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func legacyAuthority(item *agent.Agent) string {
	if item.Gate {
		return "propose"
	}
	return "act"
}

func sameLocalDay(value, now time.Time) bool {
	value = value.In(now.Location())
	return value.Year() == now.Year() && value.YearDay() == now.YearDay()
}
