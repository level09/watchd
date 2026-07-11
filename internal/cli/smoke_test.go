package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/level09/watchd/internal/store"
)

func TestPortfolioSmoke(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ready := filepath.Join(dir, "ready")
	count := filepath.Join(dir, "claude-count")
	shimDir := filepath.Join(dir, "bin")
	writeFile(t, "watchd.yaml", "daily_budget: 1.00\nexploration: 0.20\n")
	writeFile(t, "goals/product.md", "---\nname: product\nweight: 3\nauthority: act\n---\nKeep the product releasable.")
	writeFile(t, "goals/health.md", "---\nname: health\nweight: 1\nauthority: observe\n---\nSurface useful health observations without taking action.")
	writeFile(t, "goals/review.md", "---\nname: review\nweight: 1\nauthority: propose\n---\nRequire human review before action.")
	writeFile(t, "agents/repair.md", "---\nname: repair\ngoal: product\nbudget: 0.20\nverify: test -f "+ready+"\n---\nRepair the failing state.")
	writeFile(t, "agents/research.md", "---\nname: research\ngoal: health\nbudget: 0.10\n---\nReport one useful observation.")
	writeFile(t, "agents/neutral.md", "---\nname: neutral\ngoal: product\nbudget: 0.10\n---\nReport a result to be rated.")
	writeFile(t, "agents/proposal.md", "---\nname: proposal\ngoal: review\nbudget: 0.10\ngate: true\n---\nPropose a safe action for review.")
	writeFile(t, "bin/claude", "#!/bin/sh\nprintf x >> \"$WATCHD_SMOKE_COUNT\"\nif [ -n \"$WATCHD_SMOKE_READY\" ]; then touch \"$WATCHD_SMOKE_READY\"; fi\nprintf '%s' '[{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"done\",\"num_turns\":1,\"total_cost_usd\":0.05,\"session_id\":\"smoke\"}]'\n")
	if err := os.Chmod(filepath.Join(shimDir, "claude"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WATCHD_SMOKE_READY", ready)
	t.Setenv("WATCHD_SMOKE_COUNT", count)

	if _, err := captureOutput(t, func() error { return Run([]string{"run", "repair"}) }); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"run", "repair"}) }); err != nil {
		t.Fatal(err)
	}
	invocations, err := os.ReadFile(count)
	if err != nil || string(invocations) != "x" {
		t.Fatalf("claude invocations = %q err=%v", invocations, err)
	}
	repairRuns, err := store.New(".").GetRuns("repair", 0)
	if err != nil || len(repairRuns) != 2 {
		t.Fatalf("repair runs = %+v err=%v", repairRuns, err)
	}
	if repairRuns[0].Status != "satisfied" || repairRuns[1].LatestOutcome() == nil || repairRuns[1].LatestOutcome().Value != "useful" {
		t.Fatalf("repair evidence = %+v", repairRuns)
	}

	t.Setenv("WATCHD_SMOKE_READY", "")
	if _, err := captureOutput(t, func() error { return Run([]string{"run", "research"}) }); err != nil {
		t.Fatal(err)
	}
	researchRuns, _ := store.New(".").GetRuns("research", 1)
	if len(researchRuns) != 1 {
		t.Fatalf("research runs = %+v", researchRuns)
	}
	if _, err := captureOutput(t, func() error {
		return Run([]string{"outcome", researchRuns[0].ID, "useful", "changed", "today's", "decision"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"run", "neutral"}) }); err != nil {
		t.Fatal(err)
	}
	neutralRuns, _ := store.New(".").GetRuns("neutral", 1)
	if len(neutralRuns) != 1 || neutralRuns[0].Status != "success" {
		t.Fatalf("neutral run = %+v", neutralRuns)
	}
	if _, err := captureOutput(t, func() error {
		return Run([]string{"outcome", neutralRuns[0].ID, "neutral", "not actionable yet"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"run", "proposal"}) }); err != nil {
		t.Fatal(err)
	}
	proposalRuns, _ := store.New(".").GetRuns("proposal", 1)
	if len(proposalRuns) != 1 || proposalRuns[0].Status != "pending" {
		t.Fatalf("proposal gate = %+v", proposalRuns)
	}
	if _, err := captureOutput(t, func() error { return Run([]string{"approve", proposalRuns[0].ID}) }); err != nil {
		t.Fatal(err)
	}
	proposalRuns, _ = store.New(".").GetRuns("proposal", 0)
	if len(proposalRuns) != 2 || proposalRuns[0].Status != "success" || proposalRuns[1].Status != "approved" {
		t.Fatalf("proposal approval = %+v", proposalRuns)
	}
	invocations, err = os.ReadFile(count)
	if err != nil || string(invocations) != "xxxxx" {
		t.Fatalf("final claude invocations = %q err=%v", invocations, err)
	}
	output, err := captureOutput(t, func() error { return Run([]string{"portfolio"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"product", "health", "review", "repair", "research", "neutral", "proposal", "spent $0.2500"} {
		if !strings.Contains(output, want) {
			t.Fatalf("portfolio output missing %q: %s", want, output)
		}
	}
}
