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
	ID           string        `json:"id,omitempty"`
	Agent        string        `json:"agent"`
	Status       string        `json:"status"`
	Result       string        `json:"result,omitempty"`
	Error        string        `json:"error,omitempty"`
	CostUSD      float64       `json:"cost_usd"`
	Duration     time.Duration `json:"duration_ms"`
	StartedAt    time.Time     `json:"started_at"`
	SessionID    string        `json:"session_id,omitempty"`
	Model        string        `json:"model,omitempty"`
	Turns        int           `json:"turns,omitempty"`
	InputTokens  int           `json:"input_tokens,omitempty"`
	OutputTokens int           `json:"output_tokens,omitempty"`
	// Provenance: which instructions produced this output
	PromptHash string `json:"prompt_hash,omitempty"`
	AgentHash  string `json:"agent_hash,omitempty"`
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

	// File per run: runs/agent_timestamp.json; the run ID is the filename base,
	// so SaveRun is an upsert by ID
	if run.ID == "" {
		run.ID = fmt.Sprintf("%s_%s", run.Agent, run.StartedAt.Format("2006-01-02_150405"))
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
		if run.ID == "" {
			run.ID = strings.TrimSuffix(entry.Name(), ".json")
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
