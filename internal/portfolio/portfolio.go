package portfolio

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/store"
	"gopkg.in/yaml.v3"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Policy struct {
	DailyBudget float64
	Exploration float64
	MaxUnrated  int
	MaxPending  int
}
type Goal struct {
	Name        string
	Weight      float64
	DailyBudget float64
	Authority   string
	Body        string
	Hash        string
	FilePath    string
}
type ResolvedAgent struct {
	Agent     *agent.Agent
	Goal      *Goal
	Authority string
}
type StrategyStats struct {
	Rated      int
	Errors     int
	Useful     int
	Harmful    int
	CostTotal  float64
	CostCount  int
	HasUnrated bool
}
type Snapshot struct {
	Now          time.Time
	Policy       Policy
	Goals        map[string]*Goal
	Agents       []*ResolvedAgent
	Runs         []store.Run
	GlobalSpent  float64
	GoalSpent    map[string]float64
	Pending      int
	Unrated      int
	TotalSamples int
	Stats        map[string]StrategyStats
}
type Decision struct {
	Agent            *ResolvedAgent
	Admit            bool
	Score            float64
	ReservedUSD      float64
	EstimatedCostUSD float64
	RemainingUSD     float64
	Reason           string
}
type rawPolicy struct {
	DailyBudget *float64 `yaml:"daily_budget"`
	Exploration *float64 `yaml:"exploration"`
	MaxUnrated  *int     `yaml:"max_unrated"`
	MaxPending  *int     `yaml:"max_pending"`
}

func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var raw rawPolicy
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if raw.DailyBudget == nil || !positiveFinite(*raw.DailyBudget) {
		return nil, fmt.Errorf("daily_budget must be a positive finite number")
	}
	p := &Policy{DailyBudget: *raw.DailyBudget, Exploration: 0.15, MaxUnrated: 5, MaxPending: 3}
	if raw.Exploration != nil {
		p.Exploration = *raw.Exploration
	}
	if raw.MaxUnrated != nil {
		p.MaxUnrated = *raw.MaxUnrated
	}
	if raw.MaxPending != nil {
		p.MaxPending = *raw.MaxPending
	}
	if math.IsNaN(p.Exploration) || math.IsInf(p.Exploration, 0) || p.Exploration < 0 || p.Exploration > 1 {
		return nil, fmt.Errorf("exploration must be between zero and one")
	}
	if p.MaxUnrated < 0 || p.MaxPending < 0 {
		return nil, fmt.Errorf("review limits cannot be negative")
	}
	return p, nil
}

type rawGoal struct {
	Name        string   `yaml:"name"`
	Weight      *float64 `yaml:"weight"`
	DailyBudget *float64 `yaml:"daily_budget"`
	Authority   string   `yaml:"authority"`
}

func DiscoverGoals(dir string) (map[string]*Goal, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("goals directory %q not found", dir)
		}
		return nil, err
	}
	goals := make(map[string]*Goal)
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		goal, err := loadGoal(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if _, exists := goals[goal.Name]; exists {
			errs = append(errs, fmt.Errorf("duplicate goal %q", goal.Name))
			continue
		}
		goals[goal.Name] = goal
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return goals, nil
}
func loadGoal(path string) (*Goal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	frontmatter, body, err := splitMarkdown(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	var raw rawGoal
	if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: invalid frontmatter: %w", path, err)
	}
	if raw.Name == "" {
		raw.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	weight := 1.0
	if raw.Weight != nil {
		weight = *raw.Weight
	}
	if !positiveFinite(weight) {
		return nil, fmt.Errorf("parsing %s: weight must be a positive finite number", path)
	}
	dailyBudget := 0.0
	if raw.DailyBudget != nil {
		dailyBudget = *raw.DailyBudget
		if !positiveFinite(dailyBudget) {
			return nil, fmt.Errorf("parsing %s: daily_budget must be a positive finite number", path)
		}
	}
	authority := raw.Authority
	if authority == "" {
		authority = "propose"
	}
	if authority != "observe" && authority != "propose" && authority != "act" {
		return nil, fmt.Errorf("parsing %s: invalid authority %q", path, authority)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("parsing %s: empty goal body", path)
	}
	goal := &Goal{Name: raw.Name, Weight: weight, DailyBudget: dailyBudget, Authority: authority, Body: body, FilePath: path}
	goal.Hash = semanticHash(goal.Name, goal.Authority, goal.Body)
	return goal, nil
}
func Resolve(a *agent.Agent, goals map[string]*Goal, policy *Policy) (*ResolvedAgent, error) {
	if policy == nil {
		return &ResolvedAgent{Agent: a, Authority: legacyAuthority(a)}, nil
	}
	if a.Goal == "" {
		return nil, fmt.Errorf("agent %q has no goal in portfolio mode", a.Name)
	}
	goal := goals[a.Goal]
	if goal == nil {
		return nil, fmt.Errorf("agent %q references unknown goal %q", a.Name, a.Goal)
	}
	if !positiveFinite(a.Budget) {
		return nil, fmt.Errorf("agent %q needs a positive budget in portfolio mode", a.Name)
	}
	authority := goal.Authority
	if a.Gate && authority == "act" {
		authority = "propose"
	}
	return &ResolvedAgent{Agent: a, Goal: goal, Authority: authority}, nil
}
func BuildSnapshot(now time.Time, policy Policy, goals map[string]*Goal, agents []*ResolvedAgent, runs []store.Run) (Snapshot, error) {
	if !positiveFinite(policy.DailyBudget) {
		return Snapshot{}, fmt.Errorf("daily budget must be a positive finite number")
	}
	if math.IsNaN(policy.Exploration) || math.IsInf(policy.Exploration, 0) || policy.Exploration < 0 || policy.Exploration > 1 {
		return Snapshot{}, fmt.Errorf("exploration must be between zero and one")
	}
	if policy.MaxUnrated < 0 || policy.MaxPending < 0 {
		return Snapshot{}, fmt.Errorf("review limits cannot be negative")
	}
	for name, goal := range goals {
		if goal == nil || !positiveFinite(goal.Weight) {
			return Snapshot{}, fmt.Errorf("goal %q has invalid weight", name)
		}
		if goal.DailyBudget < 0 || math.IsNaN(goal.DailyBudget) || math.IsInf(goal.DailyBudget, 0) {
			return Snapshot{}, fmt.Errorf("goal %q has invalid daily budget", name)
		}
	}
	snapshot := Snapshot{
		Now: now, Policy: policy, Goals: goals, Agents: agents, Runs: runs,
		GoalSpent: make(map[string]float64), Stats: make(map[string]StrategyStats),
	}
	for _, run := range runs {
		if sameLocalDay(run.StartedAt, now) {
			snapshot.GlobalSpent += run.CostUSD
			snapshot.GoalSpent[run.Goal] += run.CostUSD
		}
		if run.Goal != "" && run.Status == "pending" {
			snapshot.Pending++
		}
		if run.Goal != "" && run.Status == "success" && run.LatestOutcome() == nil {
			snapshot.Unrated++
		}
	}
	for _, resolved := range agents {
		if resolved == nil || resolved.Agent == nil || resolved.Goal == nil {
			return Snapshot{}, fmt.Errorf("nil resolved agent")
		}
		if !positiveFinite(resolved.Agent.Budget) {
			return Snapshot{}, fmt.Errorf("agent %q has invalid budget", resolved.Agent.Name)
		}
		stats := strategyStats(resolved, runs)
		snapshot.Stats[strategyKey(resolved)] = stats
		snapshot.TotalSamples += stats.Rated + stats.Errors
	}
	return snapshot, nil
}
func Decide(snapshot Snapshot) []Decision {
	remaining := snapshot.Policy.DailyBudget - snapshot.GlobalSpent
	goalRemaining := make(map[string]float64, len(snapshot.Goals))
	for name, goal := range snapshot.Goals {
		if goal.DailyBudget > 0 {
			goalRemaining[name] = goal.DailyBudget - snapshot.GoalSpent[name]
		} else {
			goalRemaining[name] = math.Inf(1)
		}
	}
	var candidates []Decision
	var skipped []Decision
	for _, resolved := range snapshot.Agents {
		decision := scoreDecision(snapshot, resolved)
		if reason := ineligible(snapshot, resolved, remaining, goalRemaining[resolved.Goal.Name], snapshot.Pending, snapshot.Unrated); reason != "" {
			decision.Reason = reason
			skipped = append(skipped, decision)
		} else {
			candidates = append(candidates, decision)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Agent.Goal.Name != candidates[j].Agent.Goal.Name {
			return candidates[i].Agent.Goal.Name < candidates[j].Agent.Goal.Name
		}
		return candidates[i].Agent.Agent.Name < candidates[j].Agent.Agent.Name
	})
	pending := snapshot.Pending
	unrated := snapshot.Unrated
	var admitted []Decision
	for _, decision := range candidates {
		budget := decision.Agent.Agent.Budget
		goalName := decision.Agent.Goal.Name
		decision.RemainingUSD = remaining
		if reason := ineligible(snapshot, decision.Agent, remaining, goalRemaining[goalName], pending, unrated); reason != "" {
			decision.Reason = reason
			skipped = append(skipped, decision)
			continue
		}
		decision.Admit = true
		decision.ReservedUSD = budget
		decision.Reason = "highest verified return"
		admitted = append(admitted, decision)
		remaining -= budget
		goalRemaining[goalName] -= budget
		if decision.Agent.Authority == "propose" {
			pending++
		} else if decision.Agent.Agent.Verify == "" {
			unrated++
		}
	}
	sort.Slice(skipped, func(i, j int) bool {
		if skipped[i].Agent.Goal.Name != skipped[j].Agent.Goal.Name {
			return skipped[i].Agent.Goal.Name < skipped[j].Agent.Goal.Name
		}
		return skipped[i].Agent.Agent.Name < skipped[j].Agent.Agent.Name
	})
	return append(admitted, skipped...)
}
func ineligible(snapshot Snapshot, resolved *ResolvedAgent, remaining, goalRemaining float64, pending, unrated int) string {
	if remaining+1e-9 < resolved.Agent.Budget {
		return "global daily budget exhausted"
	}
	if goalRemaining+1e-9 < resolved.Agent.Budget {
		return "goal daily budget exhausted"
	}
	if resolved.Authority == "propose" && pending >= snapshot.Policy.MaxPending {
		return "pending review cap reached"
	}
	if snapshot.Stats[strategyKey(resolved)].HasUnrated {
		return "agent has unrated output"
	}
	if resolved.Agent.Verify == "" && unrated >= snapshot.Policy.MaxUnrated {
		return "unrated review cap reached"
	}
	return ""
}
func scoreDecision(snapshot Snapshot, resolved *ResolvedAgent) Decision {
	stats := snapshot.Stats[strategyKey(resolved)]
	samples := stats.Rated + stats.Errors
	expected := float64(1+stats.Useful-stats.Harmful) / float64(2+samples)
	estimated := resolved.Agent.Budget
	if stats.CostCount > 0 {
		estimated = stats.CostTotal / float64(stats.CostCount)
	}
	if estimated < 0.01 {
		estimated = 0.01
	}
	uncertainty := math.Sqrt(math.Log(float64(snapshot.TotalSamples)+2) / float64(samples+1))
	score := resolved.Goal.Weight * (expected + snapshot.Policy.Exploration*uncertainty) / estimated
	return Decision{Agent: resolved, Score: score, EstimatedCostUSD: estimated}
}
func strategyStats(resolved *ResolvedAgent, runs []store.Run) StrategyStats {
	var stats StrategyStats
	for i := range runs {
		run := &runs[i]
		if run.Agent != resolved.Agent.Name || run.AgentHash != resolved.Agent.Hash || run.GoalHash != resolved.Goal.Hash {
			continue
		}
		if run.CostUSD > 0 {
			stats.CostTotal += run.CostUSD
			stats.CostCount++
		}
		if latest := run.LatestOutcome(); latest != nil {
			stats.Rated++
			switch latest.Value {
			case "useful":
				stats.Useful++
			case "harmful":
				stats.Harmful++
			}
		} else if run.Status == "error" {
			stats.Errors++
		} else if run.Status == "success" {
			stats.HasUnrated = true
		}
	}
	return stats
}
func strategyKey(resolved *ResolvedAgent) string {
	return resolved.Agent.Name + "\x00" + resolved.Agent.Hash + "\x00" + resolved.Goal.Hash
}
func StatsFor(snapshot Snapshot, resolved *ResolvedAgent) StrategyStats {
	return snapshot.Stats[strategyKey(resolved)]
}
func sameLocalDay(a, b time.Time) bool {
	a = a.In(b.Location())
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
func legacyAuthority(a *agent.Agent) string {
	if a.Gate {
		return "propose"
	}
	return "act"
}
func splitMarkdown(content string) (string, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", fmt.Errorf("missing YAML frontmatter")
	}
	parts := strings.SplitN(content[4:], "\n---", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}
	return parts[0], parts[1], nil
}
func semanticHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
func SortedGoalNames(goals map[string]*Goal) []string {
	names := make([]string, 0, len(goals))
	for name := range goals {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
