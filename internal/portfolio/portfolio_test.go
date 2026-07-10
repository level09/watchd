package portfolio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/level09/watchd/internal/agent"
)

func TestLoadPolicyDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watchd.yaml")
	if err := os.WriteFile(path, []byte("daily_budget: 1.5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.DailyBudget != 1.5 || p.Exploration != 0.15 || p.MaxUnrated != 5 || p.MaxPending != 3 {
		t.Fatalf("policy defaults = %+v", p)
	}
}

func TestRejectInvalidPolicy(t *testing.T) {
	tests := []string{
		"daily_budget: 0\n",
		"daily_budget: -1\n",
		"daily_budget: 1\nexploration: 1.1\n",
		"daily_budget: 1\nexploration: -0.1\n",
		"daily_budget: 1\nmax_unrated: -1\n",
		"daily_budget: .nan\n",
	}
	for _, content := range tests {
		path := filepath.Join(t.TempDir(), "watchd.yaml")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPolicy(path); err == nil {
			t.Fatalf("expected invalid policy for %q", content)
		}
	}
}

func TestDiscoverGoalsSemanticHash(t *testing.T) {
	dir := t.TempDir()
	writeGoal(t, dir, "product.md", `---
name: product
weight: 2
daily_budget: 0.50
authority: propose
---
Ship useful software.`)
	goals, err := DiscoverGoals(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := goals["product"]
	if first == nil || first.Weight != 2 || first.Authority != "propose" || first.Hash == "" {
		t.Fatalf("goal = %+v", first)
	}

	writeGoal(t, dir, "product.md", `---
name: product
weight: 9
daily_budget: 0.90
authority: propose
---
Ship useful software.`)
	goals, err = DiscoverGoals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if goals["product"].Hash != first.Hash {
		t.Fatal("weight or budget changed semantic hash")
	}

	writeGoal(t, dir, "product.md", `---
name: product
weight: 9
authority: observe
---
Ship useful software.`)
	goals, _ = DiscoverGoals(dir)
	if goals["product"].Hash == first.Hash {
		t.Fatal("authority change did not change semantic hash")
	}
}

func TestDiscoverGoalsReportsAllInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoal(t, dir, "bad-weight.md", "---\nweight: 0\n---\nGoal")
	writeGoal(t, dir, "bad-authority.md", "---\nauthority: dream\n---\nGoal")
	if _, err := DiscoverGoals(dir); err == nil {
		t.Fatal("expected discovery error")
	}
}

func TestResolvePortfolioAgent(t *testing.T) {
	goals := map[string]*Goal{
		"product": {Name: "product", Weight: 1, Authority: "act", Hash: "g1"},
	}
	p := &Policy{DailyBudget: 1, Exploration: 0.15, MaxUnrated: 5, MaxPending: 3}
	a := &agent.Agent{Name: "repair", Goal: "product", Budget: 0.25, Gate: true}
	resolved, err := Resolve(a, goals, p)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Authority != "propose" || resolved.Goal.Hash != "g1" {
		t.Fatalf("resolved = %+v", resolved)
	}

	if _, err := Resolve(&agent.Agent{Name: "missing", Goal: "other", Budget: 0.1}, goals, p); err == nil {
		t.Fatal("expected unknown goal error")
	}
	if _, err := Resolve(&agent.Agent{Name: "free", Goal: "product"}, goals, p); err == nil {
		t.Fatal("expected missing budget error")
	}
}

func writeGoal(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
