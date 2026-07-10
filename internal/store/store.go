package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Run struct {
	ID                 string          `json:"id,omitempty"`
	Agent              string          `json:"agent"`
	Goal               string          `json:"goal,omitempty"`
	GoalHash           string          `json:"goal_hash,omitempty"`
	Status             string          `json:"status"`
	Result             string          `json:"result,omitempty"`
	Error              string          `json:"error,omitempty"`
	CostUSD            float64         `json:"cost_usd"`
	Duration           time.Duration   `json:"duration_ms"`
	StartedAt          time.Time       `json:"started_at"`
	SessionID          string          `json:"session_id,omitempty"`
	Model              string          `json:"model,omitempty"`
	Turns              int             `json:"turns,omitempty"`
	InputTokens        int             `json:"input_tokens,omitempty"`
	OutputTokens       int             `json:"output_tokens,omitempty"`
	Allocation         *Allocation     `json:"allocation,omitempty"`
	OutcomeRatings     []OutcomeRating `json:"outcome_ratings,omitempty"`
	VerificationBefore *Verification   `json:"verification_before,omitempty"`
	VerificationAfter  *Verification   `json:"verification_after,omitempty"`
	PromptHash         string          `json:"prompt_hash,omitempty"`
	AgentHash          string          `json:"agent_hash,omitempty"`
}
type Allocation struct {
	Score            float64 `json:"score"`
	ReservedUSD      float64 `json:"reserved_usd"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	RemainingUSD     float64 `json:"remaining_usd_before"`
	Reason           string  `json:"reason"`
}
type OutcomeRating struct {
	Value   string    `json:"value"`
	Source  string    `json:"source"`
	Note    string    `json:"note,omitempty"`
	RatedAt time.Time `json:"rated_at"`
}
type Verification struct {
	Command    string `json:"command"`
	Passed     bool   `json:"passed"`
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

func (r *Run) LatestOutcome() *OutcomeRating {
	if len(r.OutcomeRatings) == 0 {
		return nil
	}
	return &r.OutcomeRatings[len(r.OutcomeRatings)-1]
}

type Store struct {
	dir string
}

func New(dir string) *Store { return &Store{dir: dir} }
func (s *Store) SaveRun(run *Run) error {
	runsDir := filepath.Join(s.dir, "runs")
	os.MkdirAll(runsDir, 0755)
	if run.ID == "" {
		run.ID = fmt.Sprintf("%s_%s", run.Agent, run.StartedAt.Format("2006-01-02_150405.000000000"))
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runsDir, run.ID+".json"), data, 0644)
}
func (s *Store) GetRun(id string) (*Run, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "runs", id+".json"))
	if err != nil {
		return nil, fmt.Errorf("run %q not found", id)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	run.ID = id
	return &run, nil
}
func (s *Store) GetRuns(agentName string, limit int) ([]Run, error) {
	runsDir := filepath.Join(s.dir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var runs []Run
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if agentName != "" {
			name := entry.Name()
			if len(name) < len(agentName)+1 || name[:len(agentName)+1] != agentName+"_" {
				continue
			}
		}
		data, err := os.ReadFile(filepath.Join(runsDir, entry.Name()))
		if err != nil {
			continue
		}
		var run Run
		if err := json.Unmarshal(data, &run); err != nil {
			continue
		}
		if run.ID == "" {
			run.ID = strings.TrimSuffix(entry.Name(), ".json")
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}
func (s *Store) AppendOutcome(id string, rating OutcomeRating) (*Run, error) {
	if rating.Value != "useful" && rating.Value != "neutral" && rating.Value != "harmful" {
		return nil, fmt.Errorf("invalid outcome %q (use useful, neutral, or harmful)", rating.Value)
	}
	if rating.Source != "human" && rating.Source != "verify" {
		return nil, fmt.Errorf("invalid outcome source %q", rating.Source)
	}
	run, err := s.GetRun(id)
	if err != nil {
		return nil, err
	}
	if run.Status == "pending" || run.Status == "approved" {
		return nil, fmt.Errorf("run %s is %s, not terminal", id, run.Status)
	}
	if rating.RatedAt.IsZero() {
		rating.RatedAt = time.Now()
	}
	run.OutcomeRatings = append(run.OutcomeRatings, rating)
	if err := s.SaveRun(run); err != nil {
		return nil, err
	}
	return run, nil
}
