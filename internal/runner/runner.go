package runner

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/store"
)

// ClaudeEvent is one event in the JSON array output from claude -p --output-format json
type ClaudeEvent struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	Result       string  `json:"result"`
	DurationMs   int     `json:"duration_ms"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	SessionID    string  `json:"session_id"`
}

func Run(a *agent.Agent, s *store.Store) (*store.Run, error) {
	args := []string{
		"-p", a.Prompt,
		"--output-format", "json",
		"--model", a.Model,
		"--permission-mode", a.Mode,
	}

	if a.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", a.MaxTurns))
	}

	// Restrict tools to minimize cost. If agent specifies tools, use those.
	// Otherwise default to a minimal set to avoid loading all MCP servers.
	tools := a.Tools
	if len(tools) == 0 {
		tools = []string{"Bash", "Read", "Write", "Glob", "Grep", "WebSearch", "WebFetch"}
	}
	for _, tool := range tools {
		args = append(args, "--allowedTools", tool)
	}

	if a.MCPConfig != "" {
		args = append(args, "--mcp-config", a.MCPConfig, "--strict-mcp-config")
	} else {
		// No MCP config = don't load any MCP servers (saves ~$0.05/run)
		args = append(args, "--strict-mcp-config")
	}

	start := time.Now()
	cmd := exec.Command("claude", args...)
	output, err := cmd.Output()
	duration := time.Since(start)

	run := &store.Run{
		Agent:     a.Name,
		StartedAt: start,
		Duration:  duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			run.Status = "error"
			run.Error = string(exitErr.Stderr)
		} else {
			run.Status = "error"
			run.Error = err.Error()
		}
	} else {
		// Parse JSON array of events, find the result event
		var events []ClaudeEvent
		if err := json.Unmarshal(output, &events); err != nil {
			// Fallback: treat as plain text
			run.Status = "success"
			run.Result = string(output)
		} else {
			run.Status = "success"
			for _, e := range events {
				if e.Type == "result" {
					run.Result = e.Result
					run.CostUSD = e.TotalCostUSD
					run.SessionID = e.SessionID
					if e.Subtype == "error" {
						run.Status = "error"
						run.Error = e.Result
					}
					break
				}
			}
		}
	}

	// Check budget
	if a.Budget > 0 && run.CostUSD > a.Budget {
		fmt.Printf("⚠ %s cost $%.4f exceeds budget $%.2f\n", a.Name, run.CostUSD, a.Budget)
	}

	if s != nil {
		s.SaveRun(run)
	}

	return run, nil
}
