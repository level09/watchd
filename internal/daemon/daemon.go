package daemon

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/runner"
	"github.com/level09/watchd/internal/store"
)

func Start(agentsDir string, s *store.Store) error {
	agents, err := agent.Discover(agentsDir)
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

	fmt.Printf("watchd: starting with %d agent(s)\n", len(scheduled))
	for _, a := range scheduled {
		fmt.Printf("  %s  %s\n", a.Name, a.Schedule)
	}

	// Track next run time for each agent
	type entry struct {
		agent   *agent.Agent
		nextRun time.Time
	}

	entries := make([]entry, len(scheduled))
	now := time.Now()
	for i, a := range scheduled {
		interval, err := parseInterval(a.Schedule)
		if err != nil {
			return fmt.Errorf("bad schedule for %s: %w", a.Name, err)
		}
		entries[i] = entry{agent: a, nextRun: now.Add(interval)}
	}

	// Run initial pass immediately
	for _, a := range scheduled {
		fmt.Printf("watchd: running %s...\n", a.Name)
		run, _ := runner.Run(a, s)
		printRunResult(run)
	}

	// Signal handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	fmt.Println("watchd: waiting for schedules (ctrl+c to stop)")

	for {
		select {
		case <-stop:
			fmt.Println("\nwatchd: shutting down")
			return nil
		case now := <-ticker.C:
			for i, e := range entries {
				if now.Before(e.nextRun) {
					continue
				}

				fmt.Printf("watchd: running %s...\n", e.agent.Name)
				run, _ := runner.Run(e.agent, s)
				printRunResult(run)

				interval, _ := parseInterval(e.agent.Schedule)
				entries[i].nextRun = now.Add(interval)
			}
		}
	}
}

func printRunResult(run *store.Run) {
	if run == nil {
		return
	}
	icon := "✓"
	switch run.Status {
	case "error":
		icon = "✗"
	case "pending":
		icon = "⏸"
	}
	cost := ""
	if run.CostUSD > 0 {
		cost = fmt.Sprintf(" $%.4f", run.CostUSD)
	}
	fmt.Printf("  %s %s in %s%s\n", icon, run.Agent, run.Duration.Round(time.Millisecond), cost)
	if run.Error != "" {
		fmt.Printf("    error: %s\n", truncate(run.Error, 200))
	}
	if run.Status == "pending" {
		fmt.Printf("    awaiting approval: watchd approve %s\n", run.ID)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseInterval(schedule string) (time.Duration, error) {
	if schedule == "" {
		return 0, fmt.Errorf("empty schedule")
	}

	// Simple interval: 30s, 5m, 2h, 1d
	d, err := time.ParseDuration(schedule)
	if err == nil {
		return d, nil
	}

	// Handle "d" suffix (not supported by ParseDuration)
	if len(schedule) > 1 && schedule[len(schedule)-1] == 'd' {
		var n int
		if _, err := fmt.Sscanf(schedule, "%dd", &n); err == nil {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}

	return 0, fmt.Errorf("invalid interval %q (use 30s, 5m, 2h, 1d)", schedule)
}
