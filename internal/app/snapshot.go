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
	return SnapshotView{}, fmt.Errorf("portfolio snapshots are not available")
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
