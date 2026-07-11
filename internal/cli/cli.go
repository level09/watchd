package cli

import (
	"bufio"
	_ "embed"
	"fmt"
	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/daemon"
	"github.com/level09/watchd/internal/portfolio"
	"github.com/level09/watchd/internal/runner"
	"github.com/level09/watchd/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const version = "1.2.0"
const agentsDir = "agents"

//go:embed help.txt
var helpText string

//go:embed example.md
var exampleAgent string

func Run(args []string) error {
	if len(args) == 0 {
		return cmdStatus()
	}
	switch args[0] {
	case "init":
		return cmdInit()
	case "add":
		return oneArg(args, "watchd add <name>", cmdAdd)
	case "run":
		return oneArg(args, "watchd run <name>", cmdRun)
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
	case "portfolio":
		return cmdPortfolio()
	case "outcome":
		if len(args) < 3 {
			return fmt.Errorf("usage: watchd outcome <run-id> useful|neutral|harmful [note]")
		}
		return cmdOutcome(args[1], args[2], strings.Join(args[3:], " "))
	case "rate":
		return cmdRate()
	case "check":
		return oneArg(args, "watchd check <agent>", cmdCheck)
	case "pending":
		return cmdPending()
	case "approve":
		return oneArg(args, "watchd approve <run-id>", cmdApprove)
	case "reject":
		return oneArg(args, "watchd reject <run-id>", cmdReject)
	case "up":
		return cmdUp()
	case "edit":
		return oneArg(args, "watchd edit <name>", cmdEdit)
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
func oneArg(args []string, usage string, command func(string) error) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: %s", usage)
	}
	return command(args[1])
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
	a, err := loadAgent(name)
	if err != nil {
		return err
	}
	resolved, allocation, err := admitAgent(a, false)
	if err != nil {
		return err
	}
	s := store.New(".")
	fmt.Printf("running %s...\n", a.Name)
	var run *store.Run
	if resolved == nil {
		run, err = runner.Run(a, s)
	} else {
		run, err = runner.RunResolved(resolved, allocation, s)
	}
	if err != nil {
		return err
	}
	runner.PrintRun(run)
	return nil
}
func cmdList() error {
	agents, err := agent.Discover(agentsDir)
	if err != nil {
		return err
	}
	reasons, err := eligibility()
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		fmt.Println("no agents found. run: watchd init")
		return nil
	}
	fmt.Printf("%-20s %-16s %-12s %-10s %-10s %s\n", "AGENT", "GOAL", "SCHEDULE", "MODEL", "MODE", "ELIGIBILITY")
	for _, a := range agents {
		fmt.Printf("%-20s %-16s %-12s %-10s %-10s %s\n", a.Name, orDash(a.Goal), orDash(a.Schedule), a.Model, a.Mode, orDash(reasons[a.Name]))
	}
	return nil
}
func cmdOutcome(id, value, note string) error {
	run, err := store.New(".").AppendOutcome(id, store.OutcomeRating{
		Value: value, Source: "human", Note: note, RatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	if value == "harmful" {
		a, err := loadAgent(run.Agent)
		if err != nil {
			return err
		}
		if a.Notify != "" {
			if err := runner.Notify(a.Notify, run, value); err != nil {
				return fmt.Errorf("notify %s: %w", run.Agent, err)
			}
		}
	}
	fmt.Printf("rated %s %s\n", run.ID, value)
	return nil
}
func cmdRate() error {
	runs, err := store.New(".").GetRuns("", 0)
	if err != nil {
		return err
	}
	var unrated []store.Run
	for _, r := range runs {
		if r.Status == "success" && r.LatestOutcome() == nil {
			unrated = append(unrated, r)
		}
	}
	if len(unrated) == 0 {
		fmt.Println("nothing unrated. the ledger is clean.")
		return nil
	}
	values := map[string]string{"u": "useful", "n": "neutral", "h": "harmful"}
	reader := bufio.NewReader(os.Stdin)
	rated := 0
	for i, r := range unrated {
		fmt.Printf("\n[%d/%d] %s  $%.4f  %s\n", i+1, len(unrated), r.Agent, r.CostUSD, relativeTime(r.StartedAt))
		fmt.Printf("  %s\n", truncate(r.Result, 500))
		fmt.Print("  [u]seful [n]eutral [h]armful [s]kip [q]uit, note after value: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		key, note, _ := strings.Cut(strings.TrimSpace(line), " ")
		if key == "q" {
			break
		}
		value, ok := values[key]
		if !ok {
			continue
		}
		if err := cmdOutcome(r.ID, value, note); err != nil {
			return err
		}
		rated++
	}
	fmt.Printf("\nrated %d of %d\n", rated, len(unrated))
	return nil
}
func cmdCheck(name string) error {
	a, err := loadAgent(name)
	if err != nil {
		return err
	}
	if a.Verify == "" {
		return fmt.Errorf("agent %q has no verifier", name)
	}
	verification, verifyErr := runner.RunVerifier(a.Verify, a.VerificationTimeout())
	fmt.Printf("goal: %s\ncommand: %s\nduration: %dms\n", a.Goal, verification.Command, verification.DurationMS)
	if verification.Output != "" {
		fmt.Println(verification.Output)
	}
	if verifyErr != nil {
		return verifyErr
	}
	if !verification.Passed {
		fmt.Println("unsatisfied")
		return fmt.Errorf("goal %q is unsatisfied", a.Goal)
	}
	fmt.Println("satisfied")
	return nil
}
func cmdPortfolio() error {
	policy, goals, resolved, runs, err := loadPortfolio()
	if err != nil {
		return err
	}
	scheduled := scheduledAgents(resolved)
	snapshot, err := portfolio.BuildSnapshot(time.Now(), *policy, goals, scheduled, runs)
	if err != nil {
		return err
	}
	remaining := policy.DailyBudget - snapshot.GlobalSpent
	fmt.Printf("portfolio  spent $%.4f  remaining $%.4f  pending %d/%d  unrated %d/%d\n",
		snapshot.GlobalSpent, remaining, snapshot.Pending, policy.MaxPending, snapshot.Unrated, policy.MaxUnrated)
	useful, totalCost := map[string]int{}, map[string]float64{}
	for i := range runs {
		totalCost[runs[i].Goal] += runs[i].CostUSD
		if runs[i].Status != "satisfied" && runs[i].LatestOutcome() != nil && runs[i].LatestOutcome().Value == "useful" {
			useful[runs[i].Goal]++
		}
	}
	fmt.Printf("%-16s %8s %10s %10s %10s %s\n", "GOAL", "WEIGHT", "SPENT", "CAP", "OUTCOMES", "EFFICIENCY")
	for _, name := range portfolio.SortedGoalNames(goals) {
		efficiency := "-"
		cap := "-"
		if useful[name] > 0 {
			efficiency = fmt.Sprintf("$%.4f/useful", totalCost[name]/float64(useful[name]))
		}
		if goals[name].DailyBudget > 0 {
			cap = fmt.Sprintf("$%.4f", goals[name].DailyBudget)
		}
		fmt.Printf("%-16s %8.2f $%9.4f %10s %8d useful %s\n", name, goals[name].Weight, snapshot.GoalSpent[name], cap, useful[name], efficiency)
	}
	fmt.Printf("\n%-20s %-16s %10s %10s %10s %s\n", "AGENT", "GOAL", "SCORE", "RATED", "AVG COST", "DECISION")
	for _, decision := range portfolio.Decide(snapshot) {
		stats := portfolio.StatsFor(snapshot, decision.Agent)
		fmt.Printf("%-20s %-16s %10.3f %8d rated $%9.4f %s\n", decision.Agent.Agent.Name,
			decision.Agent.Goal.Name, decision.Score, stats.Rated, decision.EstimatedCostUSD, decision.Reason)
	}
	var manual []string
	for _, a := range resolved {
		if a.Agent.Schedule == "" {
			manual = append(manual, a.Agent.Name)
		}
	}
	if len(manual) > 0 {
		fmt.Printf("\nmanual portfolio agents: %s\n", strings.Join(manual, ", "))
	}
	return nil
}
func loadPortfolio() (*portfolio.Policy, map[string]*portfolio.Goal, []*portfolio.ResolvedAgent, []store.Run, error) {
	policy, err := portfolio.LoadPolicy("watchd.yaml")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if policy == nil {
		return nil, nil, nil, nil, fmt.Errorf("watchd.yaml not found")
	}
	goals, err := portfolio.DiscoverGoals("goals")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	agents, err := agent.DiscoverStrict(agentsDir)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	resolved := make([]*portfolio.ResolvedAgent, 0, len(agents))
	for _, a := range agents {
		if a.Goal == "" {
			if a.Schedule != "" {
				return nil, nil, nil, nil, fmt.Errorf("agent %q has no goal in portfolio mode", a.Name)
			}
			continue
		}
		r, err := portfolio.Resolve(a, goals, policy)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		resolved = append(resolved, r)
	}
	runs, err := store.New(".").GetRuns("", 0)
	return policy, goals, resolved, runs, err
}
func loadAgent(name string) (*agent.Agent, error) {
	agents, err := agent.Discover(agentsDir)
	if err != nil {
		return nil, err
	}
	a := agent.FindByName(agents, name)
	if a == nil {
		return nil, fmt.Errorf("agent %q not found in %s/", name, agentsDir)
	}
	return a, nil
}
func eligibility() (map[string]string, error) {
	policy, err := portfolio.LoadPolicy("watchd.yaml")
	if err != nil || policy == nil {
		return nil, err
	}
	policy, goals, agents, runs, err := loadPortfolio()
	if err != nil {
		return nil, err
	}
	snapshot, err := portfolio.BuildSnapshot(time.Now(), *policy, goals, scheduledAgents(agents), runs)
	if err != nil {
		return nil, err
	}
	reasons := make(map[string]string, len(agents))
	allAgents, err := agent.DiscoverStrict(agentsDir)
	if err != nil {
		return nil, err
	}
	for _, a := range allAgents {
		if a.Schedule == "" {
			reasons[a.Name] = "manual only"
		}
	}
	for _, decision := range portfolio.Decide(snapshot) {
		reasons[decision.Agent.Agent.Name] = decision.Reason
	}
	return reasons, nil
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
		icon := runIcon(r.Status)
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
		if outcome := r.LatestOutcome(); outcome != nil {
			fmt.Printf("  outcome: %s\n", outcome.Value)
		}
		if r.Allocation != nil {
			fmt.Printf("  allocation: %s\n", r.Allocation.Reason)
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
func cmdPending() error {
	s := store.New(".")
	runs, err := s.GetRuns("", 0)
	if err != nil {
		return err
	}
	found := false
	for _, r := range runs {
		if r.Status != "pending" {
			continue
		}
		if !found {
			fmt.Printf("%-36s %-16s %s\n", "RUN", "AGENT", "PROPOSED")
			found = true
		}
		fmt.Printf("%-36s %-16s %s\n", r.ID, r.Agent, truncate(r.Result, 80))
	}
	if !found {
		fmt.Println("nothing pending")
		return nil
	}
	fmt.Println("\napprove with: watchd approve <run-id>")
	return nil
}
func cmdApprove(id string) error {
	s := store.New(".")
	pending, err := s.GetRun(id)
	if err != nil {
		return err
	}
	if pending.Status != "pending" {
		return fmt.Errorf("run %s is %s, not pending", id, pending.Status)
	}
	agents, err := agent.Discover(agentsDir)
	if err != nil {
		return err
	}
	a := agent.FindByName(agents, pending.Agent)
	if a == nil {
		return fmt.Errorf("agent %q not found in %s/", pending.Agent, agentsDir)
	}
	if err := validatePendingStrategy(pending, a); err != nil {
		return err
	}
	if a.Verify != "" {
		verification, verifyErr := runner.RunVerifier(a.Verify, a.VerificationTimeout())
		if verifyErr != nil {
			failure := &store.Run{Agent: a.Name, Goal: pending.Goal, GoalHash: pending.GoalHash, AgentHash: a.Hash, Allocation: pending.Allocation, Status: "error", Error: verifyErr.Error(), VerificationBefore: verification, StartedAt: time.Now()}
			if failure.Allocation == nil {
				failure.Allocation = &store.Allocation{Reason: "approval preflight failed before admission"}
			}
			if err := s.SaveRun(failure); err != nil {
				return err
			}
			if a.Notify != "" {
				_ = runner.Notify(a.Notify, failure, "error")
			}
			return verifyErr
		}
		if verification.Passed {
			pending.Status = "superseded"
			pending.VerificationAfter = verification
			if err := s.SaveRun(pending); err != nil {
				return err
			}
			fmt.Printf("superseded %s: goal already satisfied\n", id)
			return nil
		}
	}
	resolved, allocation, err := admitAgent(a, true)
	if err != nil {
		return err
	}
	fmt.Printf("approving %s, executing plan...\n", id)
	var run *store.Run
	if resolved == nil {
		run, err = runner.Approve(a, pending, s)
	} else {
		run, err = runner.ApproveResolvedWithAllocation(resolved, allocation, pending, s)
	}
	if err != nil {
		return err
	}
	runner.PrintRun(run)
	return nil
}
func validatePendingStrategy(pending *store.Run, a *agent.Agent) error {
	if pending.AgentHash != "" && pending.AgentHash != a.Hash {
		return fmt.Errorf("run %s strategy changed: agent instructions no longer match the pending plan", pending.ID)
	}
	policy, err := portfolio.LoadPolicy("watchd.yaml")
	if err != nil {
		return err
	}
	if policy == nil {
		if pending.GoalHash != "" || (pending.Goal != "" && a.Goal == "") {
			return fmt.Errorf("run %s strategy changed: goal no longer matches the pending plan", pending.ID)
		}
		return nil
	}
	if a.Goal == "" {
		if pending.GoalHash != "" || pending.Goal != "" {
			return fmt.Errorf("run %s strategy changed: goal no longer matches the pending plan", pending.ID)
		}
		return nil
	}
	goals, err := portfolio.DiscoverGoals("goals")
	if err != nil {
		return err
	}
	resolved, err := portfolio.Resolve(a, goals, policy)
	if err != nil {
		return err
	}
	if pending.GoalHash != "" && pending.GoalHash != resolved.Goal.Hash {
		return fmt.Errorf("run %s strategy changed: goal definition no longer matches the pending plan", pending.ID)
	}
	if pending.Goal != "" && pending.Goal != resolved.Goal.Name {
		return fmt.Errorf("run %s strategy changed: goal reference no longer matches the pending plan", pending.ID)
	}
	return nil
}
func scheduledAgents(agents []*portfolio.ResolvedAgent) []*portfolio.ResolvedAgent {
	result := make([]*portfolio.ResolvedAgent, 0, len(agents))
	for _, a := range agents {
		if a.Agent.Schedule != "" {
			result = append(result, a)
		}
	}
	return result
}
func cmdReject(id string) error {
	s := store.New(".")
	run, err := s.GetRun(id)
	if err != nil {
		return err
	}
	if run.Status != "pending" {
		return fmt.Errorf("run %s is %s, not pending", id, run.Status)
	}
	run.Status = "rejected"
	run.OutcomeRatings = append(run.OutcomeRatings, store.OutcomeRating{Value: "neutral", Source: "verify", Note: "proposal rejected", RatedAt: time.Now()})
	if err := s.SaveRun(run); err != nil {
		return err
	}
	fmt.Printf("rejected %s\n", id)
	return nil
}
func admitAgent(a *agent.Agent, approval bool) (*portfolio.ResolvedAgent, *store.Allocation, error) {
	policy, err := portfolio.LoadPolicy("watchd.yaml")
	if err != nil || policy == nil {
		return nil, nil, err
	}
	if a.Goal == "" {
		if a.Budget <= 0 {
			return nil, nil, fmt.Errorf("legacy agent %q needs a positive budget in portfolio mode", a.Name)
		}
		runs, err := store.New(".").GetRuns("", 0)
		if err != nil {
			return nil, nil, err
		}
		spent := 0.0
		now := time.Now()
		for _, run := range runs {
			local := run.StartedAt.In(now.Location())
			if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
				spent += run.CostUSD
			}
		}
		if policy.DailyBudget-spent < a.Budget {
			return nil, nil, fmt.Errorf("global daily budget exhausted")
		}
		authority := "act"
		if a.Gate {
			authority = "propose"
		}
		resolved := &portfolio.ResolvedAgent{Agent: a, Authority: authority}
		return resolved, &store.Allocation{ReservedUSD: a.Budget, EstimatedCostUSD: a.Budget, RemainingUSD: policy.DailyBudget - spent, Reason: "manual legacy run"}, nil
	}
	goals, err := portfolio.DiscoverGoals("goals")
	if err != nil {
		return nil, nil, err
	}
	resolved, err := portfolio.Resolve(a, goals, policy)
	if err != nil {
		return nil, nil, err
	}
	if approval {
		if resolved.Authority == "observe" {
			return nil, nil, fmt.Errorf("agent %q cannot be approved: goal authority is observe", a.Name)
		}
		copy := *resolved
		copy.Authority = "act"
		resolved = &copy
	}
	runs, err := store.New(".").GetRuns("", 0)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := portfolio.BuildSnapshot(time.Now(), *policy, goals, []*portfolio.ResolvedAgent{resolved}, runs)
	if err != nil {
		return nil, nil, err
	}
	decision := portfolio.Decide(snapshot)[0]
	if !decision.Admit {
		return nil, nil, fmt.Errorf("agent %q not admitted: %s", a.Name, decision.Reason)
	}
	allocation := &store.Allocation{
		Score: decision.Score, ReservedUSD: decision.ReservedUSD, EstimatedCostUSD: decision.EstimatedCostUSD,
		RemainingUSD: decision.RemainingUSD, Reason: decision.Reason,
	}
	return resolved, allocation, nil
}
func cmdUp() error { return daemon.Start(agentsDir, store.New(".")) }
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
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		printHelp()
		return nil
	}
	reasons, err := eligibility()
	if err != nil {
		return err
	}
	s := store.New(".")
	fmt.Printf("%-20s %-16s %-12s %-8s %-12s %-12s %s\n", "AGENT", "GOAL", "SCHEDULE", "STATUS", "COST", "LAST RUN", "ELIGIBILITY")
	for _, a := range agents {
		runs, err := s.GetRuns(a.Name, 1)
		if err != nil {
			return err
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
		fmt.Printf("%-20s %-16s %-12s %-8s %-12s %-12s %s\n", a.Name, orDash(a.Goal), orDash(a.Schedule), status, cost, lastRun, orDash(reasons[a.Name]))
	}
	return nil
}
func printHelp() { fmt.Print(strings.ReplaceAll(helpText, "{{VERSION}}", version)) }
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
func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
func runIcon(status string) string {
	switch status {
	case "error", "harmful":
		return "✗"
	case "pending":
		return "⏸"
	case "approved":
		return "▶"
	case "incomplete":
		return "!"
	case "rejected", "superseded":
		return "-"
	default:
		return "✓"
	}
}
