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

func portfolioSnapshot(root string, now time.Time, policy portfolio.Policy, agents []*agent.Agent, runs []store.Run) (SnapshotView, error) {
	goalsPath := filepath.Join(root, "goals")
	goals, err := portfolio.DiscoverGoals(goalsPath)
	if err != nil {
		return SnapshotView{}, fmt.Errorf("load %s: %w", goalsPath, err)
	}

	resolvedByName := make(map[string]*portfolio.ResolvedAgent)
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
			return SnapshotView{}, fmt.Errorf("resolve %s: %w", item.FilePath, err)
		}
		resolvedByName[item.Name] = resolved
		if item.Schedule != "" {
			scheduled = append(scheduled, resolved)
		}
	}
	state, err := portfolio.BuildSnapshot(now, policy, goals, scheduled, runs)
	if err != nil {
		return SnapshotView{}, err
	}
	decisions := portfolio.Decide(state)
	return buildPortfolioView(now, policy, goals, agents, runs, state, resolvedByName, decisions), nil
}

func buildPortfolioView(now time.Time, policy portfolio.Policy, goals map[string]*portfolio.Goal, agents []*agent.Agent, runs []store.Run, state portfolio.Snapshot, resolvedByName map[string]*portfolio.ResolvedAgent, decisions []portfolio.Decision) SnapshotView {
	budget := policy.DailyBudget
	remaining := policy.DailyBudget - state.GlobalSpent
	maxPending := policy.MaxPending
	maxUnrated := policy.MaxUnrated
	view := SnapshotView{
		GeneratedAt: now,
		Mode:        "portfolio",
		Summary: Summary{
			DailyBudgetUSD: &budget,
			SpentUSD:       state.GlobalSpent,
			RemainingUSD:   &remaining,
			Pending:        state.Pending,
			MaxPending:     &maxPending,
			Unrated:        state.Unrated,
			MaxUnrated:     &maxUnrated,
		},
		Goals:      make([]GoalView, 0, len(goals)),
		Agents:     make([]AgentView, 0, len(agents)),
		RecentRuns: recentRunViews(runs),
	}

	totalCost := make(map[string]float64)
	useful := make(map[string]int)
	for _, run := range runs {
		totalCost[run.Goal] += run.CostUSD
		if run.Status != "satisfied" && run.LatestOutcome() != nil && run.LatestOutcome().Value == "useful" {
			useful[run.Goal]++
		}
	}
	for _, name := range portfolio.SortedGoalNames(goals) {
		goal := goals[name]
		goalView := GoalView{Name: goal.Name, Description: goal.Body, Weight: goal.Weight, Authority: goal.Authority, SpentUSD: state.GoalSpent[name], UsefulOutcomes: useful[name]}
		if goal.DailyBudget > 0 {
			cap := goal.DailyBudget
			goalView.DailyBudgetUSD = &cap
		}
		if useful[name] > 0 {
			costPerUseful := totalCost[name] / float64(useful[name])
			goalView.CostPerUsefulUSD = &costPerUseful
		}
		view.Goals = append(view.Goals, goalView)
	}

	latest := latestRuns(runs)
	decisionByName := make(map[string]portfolio.Decision, len(decisions))
	for _, decision := range decisions {
		decisionByName[decision.Agent.Agent.Name] = decision
	}
	for _, item := range agents {
		resolved := resolvedByName[item.Name]
		agentView := AgentView{Name: item.Name, Goal: item.Goal, Schedule: item.Schedule, Model: item.Model, Mode: item.Mode, Authority: legacyAuthority(item), BudgetUSD: item.Budget, ManualOnly: item.Schedule == ""}
		if resolved != nil {
			agentView.Authority = resolved.Authority
			agentView.RatedSamples = portfolio.StatsFor(state, resolved).Rated
		}
		if run, ok := latest[item.Name]; ok {
			value := runView(run)
			agentView.LatestRun = &value
		}
		if item.Schedule == "" {
			agentView.Decision = "manual only"
		} else if decision, ok := decisionByName[item.Name]; ok {
			score := decision.Score
			estimatedCost := decision.EstimatedCostUSD
			agentView.Score = &score
			agentView.EstimatedCostUSD = &estimatedCost
			agentView.Decision = decision.Reason
		}
		view.Agents = append(view.Agents, agentView)
	}
	sort.Slice(view.Agents, func(i, j int) bool { return view.Agents[i].Name < view.Agents[j].Name })
	return view
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
			Mode:       item.Mode,
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
