package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Run struct {
	Agent     string        `json:"agent"`
	Status    string        `json:"status"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	CostUSD   float64       `json:"cost_usd"`
	Duration  time.Duration `json:"duration_ms"`
	StartedAt time.Time     `json:"started_at"`
	SessionID string        `json:"session_id,omitempty"`
}

type Store struct {
	dir string
}

func New(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) SaveRun(run *Run) error {
	runsDir := filepath.Join(s.dir, "runs")
	os.MkdirAll(runsDir, 0755)

	// File per run: runs/agent_timestamp.json
	filename := fmt.Sprintf("%s_%s.json",
		run.Agent,
		run.StartedAt.Format("2006-01-02_150405"),
	)

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(runsDir, filename), data, 0644)
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

		// Filter by agent name if specified
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
		runs = append(runs, run)
	}

	// Sort by time descending
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})

	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	return runs, nil
}

func (s *Store) TotalCost(agentName string) float64 {
	runs, _ := s.GetRuns(agentName, 0)
	var total float64
	for _, r := range runs {
		total += r.CostUSD
	}
	return total
}
