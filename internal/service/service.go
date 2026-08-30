package service

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/portfolio"
	"github.com/level09/watchd/internal/runner"
	"github.com/level09/watchd/internal/store"
)

type Service struct {
	root string
}

func New(root string) *Service { return &Service{root: root} }

func (s *Service) LoadAgent(name string) (*agent.Agent, error) {
	return agent.Load(filepath.Join(s.root, "agents", name+".md"))
}

func (s *Service) RunAgent(name string) (*store.Run, error) {
	a, err := s.LoadAgent(name)
	if err != nil {
		return nil, err
	}
	resolved, allocation, err := s.AdmitAgent(a, false)
	if err != nil {
		return nil, err
	}
	ledger := store.New(s.root)
	if resolved == nil {
		return runner.Run(a, ledger)
	}
	return runner.RunResolved(resolved, allocation, ledger)
}

func (s *Service) RateOutcome(id, value, note string) (*store.Run, error) {
	run, err := store.New(s.root).AppendOutcome(id, store.OutcomeRating{
		Value: value, Source: "human", Note: note, RatedAt: time.Now(),
	})
	if err != nil {
		return nil, err
	}
	if value == "harmful" {
		a, err := agent.Load(filepath.Join(s.root, "agents", run.Agent+".md"))
		if err != nil {
			return nil, err
		}
		if a.Notify != "" {
			if err := runner.Notify(a.Notify, run, value); err != nil {
				return nil, fmt.Errorf("notify %s: %w", run.Agent, err)
			}
		}
	}
	return run, nil
}

func (s *Service) AdmitAgent(a *agent.Agent, approval bool) (*portfolio.ResolvedAgent, *store.Allocation, error) {
	policy, err := portfolio.LoadPolicy(filepath.Join(s.root, "watchd.yaml"))
	if err != nil || policy == nil {
		return nil, nil, err
	}
	if a.Goal == "" {
		if a.Budget <= 0 {
			return nil, nil, fmt.Errorf("legacy agent %q needs a positive budget in portfolio mode", a.Name)
		}
		runs, err := store.New(s.root).GetRuns("", 0)
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
		return &portfolio.ResolvedAgent{Agent: a, Authority: authority}, &store.Allocation{
			ReservedUSD: a.Budget, EstimatedCostUSD: a.Budget,
			RemainingUSD: policy.DailyBudget - spent, Reason: "manual legacy run",
		}, nil
	}
	goals, err := portfolio.DiscoverGoals(filepath.Join(s.root, "goals"))
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
	runs, err := store.New(s.root).GetRuns("", 0)
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
	return resolved, &store.Allocation{
		Score: decision.Score, ReservedUSD: decision.ReservedUSD,
		EstimatedCostUSD: decision.EstimatedCostUSD, RemainingUSD: decision.RemainingUSD,
		Reason: decision.Reason,
	}, nil
}
