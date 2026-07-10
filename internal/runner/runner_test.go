package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/portfolio"
	"github.com/level09/watchd/internal/store"
)

func TestParseOutputArray(t *testing.T) {
	output := []byte(`[
		{"type":"system","subtype":"init","session_id":"abc"},
		{"type":"result","subtype":"success","is_error":false,"result":"ok","num_turns":2,
		 "total_cost_usd":0.05,"session_id":"abc","usage":{"input_tokens":10,"output_tokens":173}}
	]`)
	run := &store.Run{}
	parseOutput(output, run)

	if run.Status != "success" || run.Result != "ok" {
		t.Errorf("status=%q result=%q", run.Status, run.Result)
	}
	if run.CostUSD != 0.05 || run.SessionID != "abc" {
		t.Errorf("cost=%v session=%q", run.CostUSD, run.SessionID)
	}
	if run.InputTokens != 10 || run.OutputTokens != 173 || run.Turns != 2 {
		t.Errorf("tokens=%d/%d turns=%d", run.InputTokens, run.OutputTokens, run.Turns)
	}
}

func TestParseOutputSingleObject(t *testing.T) {
	output := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"ok","total_cost_usd":0.01,"session_id":"xyz"}`)
	run := &store.Run{}
	parseOutput(output, run)

	if run.Status != "success" || run.Result != "ok" || run.SessionID != "xyz" {
		t.Errorf("single-object shape not parsed: %+v", run)
	}
}

func TestParseOutputIsErrorWithSuccessSubtype(t *testing.T) {
	// Observed on CLI v2.1.173: auth failure has subtype "success" but is_error true
	output := []byte(`[{"type":"result","subtype":"success","is_error":true,"result":"Not logged in","session_id":"s"}]`)
	run := &store.Run{}
	parseOutput(output, run)

	if run.Status != "error" || run.Error != "Not logged in" {
		t.Errorf("is_error not honored: status=%q error=%q", run.Status, run.Error)
	}
}

func TestParseOutputErrorsArray(t *testing.T) {
	// Budget kills exit non-zero with subtype success, is_error false,
	// and the reason only in the errors array
	output := []byte(`[{"type":"result","subtype":"success","is_error":false,"result":"","session_id":"s",
		"total_cost_usd":0.056,"errors":["Reached maximum budget ($0.02)"]}]`)
	run := &store.Run{}
	parseOutput(output, run)

	if run.Status != "error" || !strings.Contains(run.Error, "maximum budget") {
		t.Errorf("errors array not surfaced: status=%q error=%q", run.Status, run.Error)
	}
	if run.CostUSD != 0.056 {
		t.Errorf("cost lost on budget kill: %v", run.CostUSD)
	}
}

func TestParseOutputPlainTextFallback(t *testing.T) {
	run := &store.Run{}
	parseOutput([]byte("just text\n"), run)

	if run.Status != "success" || run.Result != "just text" {
		t.Errorf("fallback broken: %+v", run)
	}
}

func TestMemorySection(t *testing.T) {
	t.Chdir(t.TempDir())

	got := memorySection("scan")
	if !strings.Contains(got, "first run") || !strings.Contains(got, memoryMarker) {
		t.Errorf("empty-memory section wrong: %q", got)
	}

	os.MkdirAll("memory", 0755)
	os.WriteFile(filepath.Join("memory", "scan.md"), []byte("- skip repo X, archived"), 0644)
	got = memorySection("scan")
	if !strings.Contains(got, "skip repo X") {
		t.Error("memory file contents not injected")
	}
}

func TestExtractMemory(t *testing.T) {
	t.Chdir(t.TempDir())

	// No marker: result untouched, no file written
	if got := extractMemory("scan", "plain result"); got != "plain result" {
		t.Errorf("result mangled without marker: %q", got)
	}
	if _, err := os.Stat(memoryPath("scan")); err == nil {
		t.Error("memory file written without marker")
	}

	result := "found 2 new posts\n\n" + memoryMarker + "\n## 2026-06-11\n- posts A, B reported"
	if got := extractMemory("scan", result); got != "found 2 new posts" {
		t.Errorf("result not cleaned: %q", got)
	}
	data, err := os.ReadFile(memoryPath("scan"))
	if err != nil || !strings.Contains(string(data), "posts A, B reported") {
		t.Errorf("memory not persisted: %v %q", err, data)
	}
}

func TestRunResolvedSkipsSatisfiedGoal(t *testing.T) {
	t.Chdir(t.TempDir())
	invoked := 0
	withFakeClaude(t, func(a *agent.Agent, prompt string, args []string) *store.Run {
		invoked++
		return successfulRun(a)
	})

	resolved := testResolved("act", "true")
	run, err := RunResolved(resolved, &store.Allocation{Score: 2, ReservedUSD: 0.2}, store.New("."))
	if err != nil {
		t.Fatal(err)
	}
	if invoked != 0 || run.Status != "satisfied" || run.VerificationBefore == nil || !run.VerificationBefore.Passed {
		t.Fatalf("run = %+v invoked=%d", run, invoked)
	}
}

func TestRunResolvedRecordsVerifiedOutcome(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ready := filepath.Join(dir, "ready")
	withFakeClaude(t, func(a *agent.Agent, prompt string, args []string) *store.Run {
		if err := os.WriteFile(ready, []byte("ok"), 0644); err != nil {
			t.Fatal(err)
		}
		return successfulRun(a)
	})

	resolved := testResolved("act", "test -f "+ready)
	run, err := RunResolved(resolved, &store.Allocation{Score: 2, ReservedUSD: 0.2}, store.New("."))
	if err != nil {
		t.Fatal(err)
	}
	latest := run.LatestOutcome()
	if run.Status != "success" || run.VerificationAfter == nil || !run.VerificationAfter.Passed || latest == nil || latest.Value != "useful" || latest.Source != "verify" {
		t.Fatalf("run = %+v latest=%+v", run, latest)
	}
}

func TestRunResolvedRecordsIncompleteOutcome(t *testing.T) {
	t.Chdir(t.TempDir())
	withFakeClaude(t, func(a *agent.Agent, prompt string, args []string) *store.Run { return successfulRun(a) })
	resolved := testResolved("act", "false")
	run, err := RunResolved(resolved, nil, store.New("."))
	if err != nil {
		t.Fatal(err)
	}
	latest := run.LatestOutcome()
	if run.Status != "incomplete" || latest == nil || latest.Value != "neutral" {
		t.Fatalf("run = %+v latest=%+v", run, latest)
	}
}

func TestRunResolvedEnforcesAuthority(t *testing.T) {
	tests := []struct {
		authority string
		status    string
		gateText  bool
	}{
		{authority: "observe", status: "success", gateText: false},
		{authority: "propose", status: "pending", gateText: true},
	}
	for _, tt := range tests {
		t.Run(tt.authority, func(t *testing.T) {
			t.Chdir(t.TempDir())
			withFakeClaude(t, func(a *agent.Agent, prompt string, args []string) *store.Run {
				if strings.Contains(prompt, "Review gate") != tt.gateText {
					t.Fatalf("prompt authority mismatch: %q", prompt)
				}
				for _, forbidden := range []string{"Write", "Bash"} {
					if containsArg(args, forbidden) {
						t.Fatalf("%s authority received %s", tt.authority, forbidden)
					}
				}
				return successfulRun(a)
			})
			resolved := testResolved(tt.authority, "")
			run, err := RunResolved(resolved, nil, store.New("."))
			if err != nil || run.Status != tt.status {
				t.Fatalf("run = %+v err=%v", run, err)
			}
		})
	}
}

func TestApproveResolvedSupersedesRecoveredGoal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ready := filepath.Join(dir, "ready")
	if err := os.WriteFile(ready, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	invoked := 0
	withFakeClaude(t, func(a *agent.Agent, prompt string, args []string) *store.Run {
		invoked++
		return successfulRun(a)
	})
	s := store.New(".")
	pending := &store.Run{Agent: "repair", Goal: "product", Status: "pending", SessionID: "s1", StartedAt: time.Now()}
	if err := s.SaveRun(pending); err != nil {
		t.Fatal(err)
	}
	resolved := testResolved("propose", "test -f "+ready)
	run, err := ApproveResolved(resolved, pending, s)
	if err != nil {
		t.Fatal(err)
	}
	if invoked != 0 || run.Status != "superseded" {
		t.Fatalf("run = %+v invoked=%d", run, invoked)
	}
}

func withFakeClaude(t *testing.T, fake func(*agent.Agent, string, []string) *store.Run) {
	t.Helper()
	old := invokeClaude
	invokeClaude = fake
	t.Cleanup(func() { invokeClaude = old })
}

func successfulRun(a *agent.Agent) *store.Run {
	return &store.Run{Agent: a.Name, Status: "success", Result: "done", StartedAt: time.Now(), SessionID: "session"}
}

func testResolved(authority, verify string) *portfolio.ResolvedAgent {
	goal := &portfolio.Goal{Name: "product", Weight: 1, Authority: authority, Body: "Improve the product.", Hash: "goal123"}
	a := &agent.Agent{Name: "repair", Goal: "product", Hash: "agent123", Model: "sonnet", Mode: "default", Budget: 0.2, Verify: verify}
	return &portfolio.ResolvedAgent{Agent: a, Goal: goal, Authority: authority}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
