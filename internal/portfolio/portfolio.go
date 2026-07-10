package portfolio

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/level09/watchd/internal/agent"
	"gopkg.in/yaml.v3"
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
