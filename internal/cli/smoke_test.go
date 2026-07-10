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
	writeFile(t, "agents/repair.md", "---\nname: repair\ngoal: product\nbudget: 0.20\nverify: test -f "+ready+"\n---\nRepair the failing state.")
	writeFile(t, "agents/research.md", "---\nname: research\ngoal: health\nbudget: 0.10\n---\nReport one useful observation.")
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
	output, err := captureOutput(t, func() error { return Run([]string{"portfolio"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"product", "health", "repair", "research", "spent $0.1000"} {
		if !strings.Contains(output, want) {
			t.Fatalf("portfolio output missing %q: %s", want, output)
		}
	}
}
