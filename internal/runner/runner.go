package runner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/store"
)

// ClaudeEvent is one event from claude -p --output-format json.
// Current CLI versions return an array of events; the docs also describe a
// single-object shape, so parseOutput accepts both.
type ClaudeEvent struct {
	Type         string   `json:"type"`
	Subtype      string   `json:"subtype"`
	IsError      bool     `json:"is_error"`
	Result       string   `json:"result"`
	NumTurns     int      `json:"num_turns"`
	TotalCostUSD float64  `json:"total_cost_usd"`
	SessionID    string   `json:"session_id"`
	Errors       []string `json:"errors"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Read-only tool set for gated first passes. This is the enforcement layer:
// instructions alone (including plan mode, which ends -p runs on a tool call
// with an empty result) are advisory.
var readOnlyTools = []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"}

const gateSection = "\n\n---\n## Review gate\nThis is a gated dry run: you have read-only " +
	"tools and must not attempt side effects. Investigate as needed, then end your final " +
	"message with the exact plan of actions you propose (commands, files, content). " +
	"A human reviews this plan before execution."

func Run(a *agent.Agent, s *store.Store) (*store.Run, error) {
	prompt := a.Prompt
	if a.Memory {
		prompt += memorySection(a.Name)
	}

	tools := a.Tools
	if a.Gate {
		tools = readOnlyTools
		prompt += gateSection
	}

	args := append([]string{"-p", prompt, "--permission-mode", a.Mode}, commonArgs(a, tools)...)
	run := invoke(a, prompt, args)

	if a.Memory && run.Result != "" {
		run.Result = extractMemory(a.Name, run.Result)
	}

	if a.Gate && run.Status == "success" {
		run.Status = "pending"
	}

	if a.Budget > 0 && run.CostUSD > a.Budget {
		fmt.Printf("⚠ %s cost $%.4f exceeds budget $%.2f\n", a.Name, run.CostUSD, a.Budget)
	}

	finish(a, run, s)
	return run, nil
}

// Approve resumes a pending gated run with the agent's real permission mode
// and tool set so the plan from the first pass actually executes.
func Approve(a *agent.Agent, pending *store.Run, s *store.Store) (*store.Run, error) {
	if pending.SessionID == "" {
		return nil, fmt.Errorf("run %s has no session to resume", pending.ID)
	}

	prompt := "Approved. Execute the plan from your previous response."
	args := append([]string{
		"-p", prompt,
		"--resume", pending.SessionID,
		"--permission-mode", a.Mode,
	}, commonArgs(a, a.Tools)...)
	run := invoke(a, prompt, args)

	if s != nil {
		pending.Status = "approved"
		s.SaveRun(pending)
	}
	finish(a, run, s)
	return run, nil
}

func finish(a *agent.Agent, run *store.Run, s *store.Store) {
	if s != nil {
		s.SaveRun(run) // assigns run.ID
	}
	if a.Notify != "" && (run.Status == "pending" || run.Status == "error") {
		notify(a.Notify, run)
	}
}

func commonArgs(a *agent.Agent, tools []string) []string {
	args := []string{
		"--output-format", "json",
		"--model", a.Model,
	}

	if a.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", a.MaxTurns))
	}
	if a.Budget > 0 {
		// Native enforcement: the CLI stops mid-run instead of warning after
		// the money is spent
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", a.Budget))
	}

	// Restrict tools to minimize cost. If agent specifies tools, use those.
	// Otherwise default to a minimal set to avoid loading all MCP servers.
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

	return args
}

func invoke(a *agent.Agent, prompt string, args []string) *store.Run {
	start := time.Now()
	cmd := exec.Command("claude", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	duration := time.Since(start)

	run := &store.Run{
		Agent:      a.Name,
		Model:      a.Model,
		StartedAt:  start,
		Duration:   duration,
		PromptHash: shortHash([]byte(prompt)),
		AgentHash:  a.Hash,
	}

	if err != nil {
		// claude exits non-zero on failures like a budget kill but still
		// emits the result JSON (cost, session, errors) on stdout
		if out := bytes.TrimSpace(stdout.Bytes()); len(out) > 0 && (out[0] == '[' || out[0] == '{') {
			parseOutput(out, run)
		}
		run.Status = "error"
		if run.Error == "" {
			run.Error = strings.TrimSpace(stderr.String())
		}
		if run.Error == "" {
			run.Error = err.Error()
		}
		return run
	}

	parseOutput(stdout.Bytes(), run)
	return run
}

func parseOutput(output []byte, run *store.Run) {
	var result *ClaudeEvent

	var events []ClaudeEvent
	if err := json.Unmarshal(output, &events); err == nil {
		for i := range events {
			if events[i].Type == "result" {
				result = &events[i]
				break
			}
		}
	} else {
		var single ClaudeEvent
		if err := json.Unmarshal(output, &single); err == nil && single.Type == "result" {
			result = &single
		}
	}

	if result == nil {
		// Fallback: treat as plain text
		run.Status = "success"
		run.Result = strings.TrimSpace(string(output))
		return
	}

	run.Result = result.Result
	run.CostUSD = result.TotalCostUSD
	run.SessionID = result.SessionID
	run.Turns = result.NumTurns
	run.InputTokens = result.Usage.InputTokens
	run.OutputTokens = result.Usage.OutputTokens

	// is_error can be true even with subtype "success" (e.g. auth failures);
	// subtypes like error_max_turns carry is_error false; budget kills exit
	// non-zero with the reason only in the errors array
	if result.IsError || strings.HasPrefix(result.Subtype, "error") || len(result.Errors) > 0 {
		run.Status = "error"
		run.Error = strings.Join(result.Errors, "; ")
		if run.Error == "" {
			run.Error = result.Result
		}
		if run.Error == "" {
			run.Error = result.Subtype
		}
	} else {
		run.Status = "success"
	}
}

const memoryMarker = "===MEMORY==="

// memorySection injects the agent's curated memory and asks for an updated
// version back as a marked section; watchd writes the file itself. The agent
// never touches the file: tool-based writes proved unreliable (the CLI's own
// auto-memory directory captures them) and would not work on read-only gated
// passes. Curated memory also avoids re-injecting raw past output, which
// anchors the model on dead ends and is a prompt-injection surface.
func memorySection(name string) string {
	content, err := os.ReadFile(memoryPath(name))

	var b strings.Builder
	b.WriteString("\n\n---\n## Memory\nYour memory from previous runs:\n\n")
	if err != nil || len(content) == 0 {
		b.WriteString("(empty, this is your first run)\n")
	} else {
		b.Write(content)
		b.WriteString("\n")
	}
	b.WriteString("\nAt the very end of your final message, output a line containing exactly " +
		memoryMarker + " followed by the complete updated memory file: a '## " +
		time.Now().Format("2006-01-02") + "' entry with short bullets for this run " +
		"(findings, what to skip next time, corrections), then the prior entries still " +
		"worth keeping, pruned of stale items, under 200 lines total. Always include " +
		"today's entry, even if it is a single bullet like '- nothing new'.\n")
	return b.String()
}

// extractMemory splits the marked memory section out of the result, persists
// it, and returns the result without it.
func extractMemory(name, result string) string {
	idx := strings.LastIndex(result, memoryMarker)
	if idx < 0 {
		return result
	}

	memory := strings.TrimSpace(result[idx+len(memoryMarker):])
	if memory != "" {
		os.MkdirAll("memory", 0755)
		if err := os.WriteFile(memoryPath(name), []byte(memory+"\n"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "saving memory for %s: %v\n", name, err)
		}
	}
	return strings.TrimSpace(result[:idx])
}

func memoryPath(name string) string {
	return filepath.Join("memory", name+".md")
}

func notify(cmdStr string, run *store.Run) {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Env = append(os.Environ(),
		"WATCHD_AGENT="+run.Agent,
		"WATCHD_RUN_ID="+run.ID,
		"WATCHD_STATUS="+run.Status,
		"WATCHD_RESULT="+truncate(run.Result+run.Error, 500),
	)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "notify failed for %s: %v\n", run.Agent, err)
	}
}

func shortHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
