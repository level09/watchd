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

func (s *Service) ApproveRun(id string) (*store.Run, error) {
	ledger := store.New(s.root)
	pending, err := ledger.GetRun(id)
	if err != nil {
		return nil, err
	}
	if pending.Status != "pending" {
		return nil, fmt.Errorf("run %s is %s, not pending", id, pending.Status)
	}
	agents, err := agent.Discover(filepath.Join(s.root, "agents"))
	if err != nil {
		return nil, err
	}
	a := agent.FindByName(agents, pending.Agent)
	if a == nil {
		return nil, fmt.Errorf("agent %q not found in agents/", pending.Agent)
	}
	if err := validatePendingStrategy(s.root, pending, a); err != nil {
		return nil, err
	}
	if a.Verify != "" {
		verification, verifyErr := runner.RunVerifier(a.Verify, a.VerificationTimeout())
		if verifyErr != nil {
			failure := &store.Run{Agent: a.Name, Goal: pending.Goal, GoalHash: pending.GoalHash, AgentHash: a.Hash, Allocation: pending.Allocation, Status: "error", Error: verifyErr.Error(), VerificationBefore: verification, StartedAt: time.Now()}
			if failure.Allocation == nil {
				failure.Allocation = &store.Allocation{Reason: "approval preflight failed before admission"}
			}
			if err := ledger.SaveRun(failure); err != nil {
				return nil, err
			}
			if a.Notify != "" {
				_ = runner.Notify(a.Notify, failure, "error")
			}
			return failure, verifyErr
		}
		if verification.Passed {
			pending.Status = "superseded"
			pending.VerificationAfter = verification
			if err := ledger.SaveRun(pending); err != nil {
				return nil, err
			}
			return pending, nil
		}
	}
	resolved, allocation, err := s.AdmitAgent(a, true)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return runner.Approve(a, pending, ledger)
	}
	return runner.ApproveResolvedWithAllocation(resolved, allocation, pending, ledger)
}

func validatePendingStrategy(root string, pending *store.Run, a *agent.Agent) error {
	if pending.AgentHash != "" && pending.AgentHash != a.Hash {
		return fmt.Errorf("run %s strategy changed: agent instructions no longer match the pending plan", pending.ID)
	}
	policy, err := portfolio.LoadPolicy(filepath.Join(root, "watchd.yaml"))
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
	goals, err := portfolio.DiscoverGoals(filepath.Join(root, "goals"))
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
