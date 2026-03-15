package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/daemon"
	"github.com/level09/watchd/internal/runner"
	"github.com/level09/watchd/internal/store"
)

const version = "3.0.0"
const agentsDir = "agents"

func Run(args []string) error {
	if len(args) == 0 {
		return cmdStatus()
	}

	switch args[0] {
	case "init":
		return cmdInit()
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: watchd add <name>")
		}
		return cmdAdd(args[1])
	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: watchd run <name>")
		}
		return cmdRun(args[1])
	case "list":
		return cmdList()
	case "logs":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		return cmdLogs(name)
	case "costs":
		return cmdCosts()
	case "up":
		return cmdUp()
	case "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: watchd edit <name>")
		}
		return cmdEdit(args[1])
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		printHelp()
		return nil
	default:
		return fmt.Errorf("unknown command: %s (try watchd help)", args[0])
	}
}

func cmdInit() error {
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return err
	}
	fmt.Println("created agents/")

	example := filepath.Join(agentsDir, "example.md")
	if _, err := os.Stat(example); err == nil {
		fmt.Println("agents/example.md already exists")
		return nil
	}

	err := os.WriteFile(example, []byte(exampleAgent), 0644)
	if err != nil {
		return err
	}
	fmt.Println("created agents/example.md")
	fmt.Println("\nnext: watchd run example")
	return nil
}

func cmdAdd(name string) error {
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(agentsDir, name+".md")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	tmpl := fmt.Sprintf(`---
name: %s
schedule: ""
model: sonnet
permission_mode: default
max_turns: 10
---

# %s

Describe what this agent should do.
`, name, name)

	if err := os.WriteFile(path, []byte(tmpl), 0644); err != nil {
		return err
	}
	fmt.Printf("created %s\n", path)
	return nil
}

func cmdRun(name string) error {
	agents, err := agent.Discover(agentsDir)
	if err != nil {
		return err
	}

	a := agent.FindByName(agents, name)
	if a == nil {
		return fmt.Errorf("agent %q not found in %s/", name, agentsDir)
	}

	s := store.New(".")
	fmt.Printf("running %s...\n", a.Name)
	run, err := runner.Run(a, s)
	if err != nil {
		return err
	}

	printRun(run)
	return nil
}

func cmdList() error {
	agents, err := agent.Discover(agentsDir)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		fmt.Println("no agents found. run: watchd init")
		return nil
	}

	fmt.Printf("%-20s %-12s %-10s %s\n", "AGENT", "SCHEDULE", "MODEL", "MODE")
	for _, a := range agents {
		schedule := a.Schedule
		if schedule == "" {
			schedule = "-"
		}
		fmt.Printf("%-20s %-12s %-10s %s\n", a.Name, schedule, a.Model, a.Mode)
	}
	return nil
}

func cmdLogs(name string) error {
	s := store.New(".")
	runs, err := s.GetRuns(name, 20)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Println("no runs found")
		return nil
	}

	for _, r := range runs {
		icon := "✓"
		if r.Status == "error" {
			icon = "✗"
		}
		cost := ""
		if r.CostUSD > 0 {
			cost = fmt.Sprintf("  $%.4f", r.CostUSD)
		}
		ago := relativeTime(r.StartedAt)
		fmt.Printf("%s %-16s %-8s %s%s  %s\n",
			icon, r.Agent, r.Status,
			r.Duration.Round(time.Millisecond), cost, ago)

		if r.Error != "" {
			fmt.Printf("  error: %s\n", truncate(r.Error, 200))
		}
		if r.Result != "" && r.Status == "success" {
			fmt.Printf("  %s\n", truncate(r.Result, 500))
		}
	}
	return nil
}

func cmdCosts() error {
	s := store.New(".")
	runs, err := s.GetRuns("", 0)
	if err != nil {
		return err
	}

	totals := map[string]float64{}
	counts := map[string]int{}
	for _, r := range runs {
		totals[r.Agent] += r.CostUSD
		counts[r.Agent]++
	}

	if len(totals) == 0 {
		fmt.Println("no cost data yet")
		return nil
	}

	var grand float64
	fmt.Printf("%-20s %8s %6s\n", "AGENT", "COST", "RUNS")
	for name, total := range totals {
		fmt.Printf("%-20s $%7.4f %6d\n", name, total, counts[name])
		grand += total
	}
	fmt.Printf("%-20s $%7.4f %6d\n", "TOTAL", grand, len(runs))
	return nil
}

func cmdUp() error {
	s := store.New(".")
	return daemon.Start(agentsDir, s)
}

func cmdEdit(name string) error {
	path := filepath.Join(agentsDir, name+".md")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("agent %q not found", name)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdStatus() error {
	agents, err := agent.Discover(agentsDir)
	if err != nil || len(agents) == 0 {
		printHelp()
		return nil
	}

	s := store.New(".")
	fmt.Printf("%-20s %-12s %-8s %-12s %s\n", "AGENT", "SCHEDULE", "STATUS", "COST", "LAST RUN")
	for _, a := range agents {
		runs, _ := s.GetRuns(a.Name, 1)
		schedule := a.Schedule
		if schedule == "" {
			schedule = "-"
		}
		status := "-"
		cost := "-"
		lastRun := "never"
		if len(runs) > 0 {
			r := runs[0]
			status = r.Status
			if r.CostUSD > 0 {
				cost = fmt.Sprintf("$%.4f", r.CostUSD)
			}
			lastRun = relativeTime(r.StartedAt)
		}
		fmt.Printf("%-20s %-12s %-8s %-12s %s\n", a.Name, schedule, status, cost, lastRun)
	}
	return nil
}

func printHelp() {
	help := `watchd v` + version + ` - schedule AI agents

usage: watchd <command> [args]

commands:
  init          create agents/ directory with example
  add <name>    scaffold a new agent
  edit <name>   open agent in $EDITOR
  run <name>    run an agent once
  list          show all agents
  logs [name]   show run history
  costs         show cost breakdown
  up            start scheduler (run all scheduled agents)
  help          show this help
  version       show version`
	fmt.Println(help)
}

func printRun(run *store.Run) {
	if run == nil {
		return
	}
	icon := "✓"
	if run.Status == "error" {
		icon = "✗"
	}
	cost := ""
	if run.CostUSD > 0 {
		cost = fmt.Sprintf(" ($%.4f)", run.CostUSD)
	}
	fmt.Printf("%s %s in %s%s\n", icon, run.Agent, run.Duration.Round(time.Millisecond), cost)

	if run.Result != "" {
		// Show first 500 chars of result
		result := run.Result
		if len(result) > 500 {
			result = result[:500] + "..."
		}
		fmt.Println(result)
	}
	if run.Error != "" {
		fmt.Printf("error: %s\n", run.Error)
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}


const exampleAgent = `---
name: example
model: sonnet
permission_mode: default
max_turns: 5
---

# Example Agent

What is the current time? Reply in one sentence.
`
