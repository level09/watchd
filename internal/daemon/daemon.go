package daemon

import (
	"fmt"
	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/portfolio"
	"github.com/level09/watchd/internal/runner"
	"github.com/level09/watchd/internal/store"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type portfolioRunFunc func(*portfolio.ResolvedAgent, *store.Allocation, *store.Store) (*store.Run, error)
type entry struct {
	agent    *agent.Agent
	resolved *portfolio.ResolvedAgent
	nextRun  time.Time
}

func Start(agentsDir string, s *store.Store) error {
	policy, err := portfolio.LoadPolicy("watchd.yaml")
	if err != nil {
		return err
	}
	var agents []*agent.Agent
	if policy == nil {
		agents, err = agent.Discover(agentsDir)
	} else {
		agents, err = agent.DiscoverStrict(agentsDir)
	}
	if err != nil {
		return err
	}
	scheduled := []*agent.Agent{}
	for _, a := range agents {
		if a.Schedule != "" {
			scheduled = append(scheduled, a)
		}
	}
	if len(scheduled) == 0 {
		return fmt.Errorf("no scheduled agents found")
	}
	var goals map[string]*portfolio.Goal
	resolvedByName := map[string]*portfolio.ResolvedAgent{}
	if policy != nil {
		goals, err = portfolio.DiscoverGoals("goals")
		if err != nil {
			return err
		}
		for _, a := range scheduled {
			resolved, err := portfolio.Resolve(a, goals, policy)
			if err != nil {
				return err
			}
			resolvedByName[a.Name] = resolved
		}
	}
	fmt.Printf("watchd: starting with %d agent(s)\n", len(scheduled))
	for _, a := range scheduled {
		fmt.Printf("  %s  %s\n", a.Name, a.Schedule)
	}
	entries := make([]entry, len(scheduled))
	now := time.Now()
	for i, a := range scheduled {
		interval, err := parseInterval(a.Schedule)
		if err != nil {
			return fmt.Errorf("bad schedule for %s: %w", a.Name, err)
		}
		nextRun := now.Add(interval)
		if policy != nil {
			nextRun = now
		}
		entries[i] = entry{agent: a, resolved: resolvedByName[a.Name], nextRun: nextRun}
	}
	if policy == nil {
		for _, a := range scheduled {
			fmt.Printf("watchd: running %s...\n", a.Name)
			run, _ := runner.Run(a, s)
			runner.PrintRun(run)
		}
	} else {
		decisions, err := runPortfolioDue(now, *policy, goals, dueResolved(entries, now), s, printingRun(runner.RunResolved))
		if err != nil {
			return err
		}
		advanceAdmitted(entries, decisions, now)
		printDecisions(decisions, map[string]string{})
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastSkip := map[string]string{}
	fmt.Println("watchd: waiting for schedules (ctrl+c to stop)")
	for {
		select {
		case <-stop:
			fmt.Println("\nwatchd: shutting down")
			return nil
		case now := <-ticker.C:
			if policy != nil {
				decisions, err := runPortfolioDue(now, *policy, goals, dueResolved(entries, now), s, printingRun(runner.RunResolved))
				if err != nil {
					return err
				}
				advanceAdmitted(entries, decisions, now)
				printDecisions(decisions, lastSkip)
				continue
			}
			for i, e := range entries {
				if now.Before(e.nextRun) {
					continue
				}
				fmt.Printf("watchd: running %s...\n", e.agent.Name)
				run, _ := runner.Run(e.agent, s)
				runner.PrintRun(run)
				interval, _ := parseInterval(e.agent.Schedule)
				entries[i].nextRun = now.Add(interval)
			}
		}
	}
}
func printingRun(runFn portfolioRunFunc) portfolioRunFunc {
	return func(agent *portfolio.ResolvedAgent, allocation *store.Allocation, s *store.Store) (*store.Run, error) {
		run, err := runFn(agent, allocation, s)
		runner.PrintRun(run)
		return run, err
	}
}
func runPortfolioDue(now time.Time, policy portfolio.Policy, goals map[string]*portfolio.Goal, due []*portfolio.ResolvedAgent, s *store.Store, runFn portfolioRunFunc) ([]portfolio.Decision, error) {
	remaining := append([]*portfolio.ResolvedAgent(nil), due...)
	var admitted []portfolio.Decision
	for len(remaining) > 0 {
		runs, err := s.GetRuns("", 0)
		if err != nil {
			return nil, err
		}
		snapshot, err := portfolio.BuildSnapshot(now, policy, goals, remaining, runs)
		if err != nil {
			return nil, err
		}
		decisions := portfolio.Decide(snapshot)
		var next *portfolio.Decision
		for i := range decisions {
			if decisions[i].Admit {
				next = &decisions[i]
				break
			}
		}
		if next == nil {
			return append(admitted, decisions...), nil
		}
		allocation := &store.Allocation{
			Score: next.Score, ReservedUSD: next.ReservedUSD, EstimatedCostUSD: next.EstimatedCostUSD,
			RemainingUSD: next.RemainingUSD, Reason: next.Reason,
		}
		if _, err := runFn(next.Agent, allocation, s); err != nil {
			return nil, err
		}
		admitted = append(admitted, *next)
		name := next.Agent.Agent.Name
		filtered := remaining[:0]
		for _, candidate := range remaining {
			if candidate.Agent.Name != name {
				filtered = append(filtered, candidate)
			}
		}
		remaining = filtered
	}
	return admitted, nil
}
func dueResolved(entries []entry, now time.Time) []*portfolio.ResolvedAgent {
	var due []*portfolio.ResolvedAgent
	for _, entry := range entries {
		if entry.resolved != nil && !now.Before(entry.nextRun) {
			due = append(due, entry.resolved)
		}
	}
	return due
}
func advanceAdmitted(entries []entry, decisions []portfolio.Decision, now time.Time) {
	for _, decision := range decisions {
		if !decision.Admit {
			continue
		}
		for i := range entries {
			if entries[i].agent.Name == decision.Agent.Agent.Name {
				interval, _ := parseInterval(entries[i].agent.Schedule)
				entries[i].nextRun = now.Add(interval)
			}
		}
	}
}
func printDecisions(decisions []portfolio.Decision, lastSkip map[string]string) {
	for _, decision := range decisions {
		name := decision.Agent.Agent.Name
		if decision.Admit {
			delete(lastSkip, name)
			continue
		}
		if lastSkip[name] != decision.Reason {
			fmt.Printf("  - %s skipped: %s\n", name, decision.Reason)
			lastSkip[name] = decision.Reason
		}
	}
}
func parseInterval(schedule string) (time.Duration, error) {
	if schedule == "" {
		return 0, fmt.Errorf("empty schedule")
	}
	d, err := time.ParseDuration(schedule)
	if err == nil {
		return d, nil
	}
	if len(schedule) > 1 && schedule[len(schedule)-1] == 'd' {
		var n int
		if _, err := fmt.Sscanf(schedule, "%dd", &n); err == nil {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	return 0, fmt.Errorf("invalid interval %q (use 30s, 5m, 2h, 1d)", schedule)
}
