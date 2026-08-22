package app

import "time"

type SnapshotView struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Mode        string      `json:"mode"`
	Summary     Summary     `json:"summary"`
	Goals       []GoalView  `json:"goals"`
	Agents      []AgentView `json:"agents"`
	RecentRuns  []RunView   `json:"recent_runs"`
}

type Summary struct {
	DailyBudgetUSD *float64 `json:"daily_budget_usd"`
	SpentUSD       float64  `json:"spent_usd"`
	RemainingUSD   *float64 `json:"remaining_usd"`
	Pending        int      `json:"pending"`
	MaxPending     *int     `json:"max_pending"`
	Unrated        int      `json:"unrated"`
	MaxUnrated     *int     `json:"max_unrated"`
}

type GoalView struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Weight           float64  `json:"weight"`
	Authority        string   `json:"authority"`
	DailyBudgetUSD   *float64 `json:"daily_budget_usd"`
	SpentUSD         float64  `json:"spent_usd"`
	UsefulOutcomes   int      `json:"useful_outcomes"`
	CostPerUsefulUSD *float64 `json:"cost_per_useful_usd"`
}

type AgentView struct {
	Name             string   `json:"name"`
	Goal             string   `json:"goal,omitempty"`
	Schedule         string   `json:"schedule,omitempty"`
	Model            string   `json:"model"`
	Authority        string   `json:"authority"`
	BudgetUSD        float64  `json:"budget_usd"`
	ManualOnly       bool     `json:"manual_only"`
	Score            *float64 `json:"score"`
	RatedSamples     int      `json:"rated_samples"`
	EstimatedCostUSD *float64 `json:"estimated_cost_usd"`
	Decision         string   `json:"decision,omitempty"`
	LatestRun        *RunView `json:"latest_run,omitempty"`
}

type RunView struct {
	ID               string    `json:"id"`
	Agent            string    `json:"agent"`
	Goal             string    `json:"goal,omitempty"`
	Status           string    `json:"status"`
	Result           string    `json:"result,omitempty"`
	Error            string    `json:"error,omitempty"`
	CostUSD          float64   `json:"cost_usd"`
	DurationMS       int64     `json:"duration_ms"`
	StartedAt        time.Time `json:"started_at"`
	Outcome          string    `json:"outcome,omitempty"`
	AllocationReason string    `json:"allocation_reason,omitempty"`
}
