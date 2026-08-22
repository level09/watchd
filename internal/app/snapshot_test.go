package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/portfolio"
	"github.com/level09/watchd/internal/store"
)

func TestSnapshotLegacy(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	writeFixture(t, root, "agents/manual.md", "---\nname: manual\nmodel: haiku\n---\nInspect on request.")
	writeFixture(t, root, "agents/scheduled.md", "---\nname: scheduled\nschedule: 1h\nbudget: 0.10\ngate: true\n---\nInspect hourly.")
	saveFixtureRun(t, root, store.Run{
		Agent: "scheduled", Status: "success", CostUSD: 0.04,
		StartedAt:      now.Add(-time.Hour),
		OutcomeRatings: []store.OutcomeRating{{Value: "useful", Source: "human", RatedAt: now}},
	})

	got, err := Snapshot(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "legacy" || !got.GeneratedAt.Equal(now) {
		t.Fatalf("snapshot = %+v", got)
	}
	if got.Summary.DailyBudgetUSD != nil {
		t.Fatalf("summary = %+v", got.Summary)
	}
	if len(got.Agents) != 2 || got.Agents[0].Name != "manual" || !got.Agents[0].ManualOnly {
		t.Fatalf("agents = %+v", got.Agents)
	}
	if got.Agents[1].Authority != "propose" || got.Agents[1].LatestRun == nil {
		t.Fatalf("scheduled = %+v", got.Agents[1])
	}
}

func TestSnapshotNamesInvalidAgentFile(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "agents/broken.md", "not frontmatter")
	_, err := Snapshot(root, time.Now())
	if err == nil || !strings.Contains(err.Error(), "agents/broken.md") {
		t.Fatalf("error = %v", err)
	}
}

func TestSnapshotNamesScheduledAgentWithoutGoalFile(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "watchd.yaml", "daily_budget: 1\n")
	writeFixture(t, root, "goals/product.md", "---\nname: product\n---\nShip.")
	writeFixture(t, root, "agents/scheduled.md", "---\nname: scheduled\nschedule: 1h\n---\nRun.")

	_, err := Snapshot(root, time.Now())
	path := filepath.Join(root, "agents", "scheduled.md")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %v", err)
	}
}

func TestSnapshotPortfolioPreservesAllocation(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	writeFixture(t, root, "watchd.yaml", "daily_budget: 1\n")
	writeFixture(t, root, "goals/product.md", "---\nname: product\nweight: 2\nauthority: observe\n---\nShip useful software.")
	writeFixture(t, root, "agents/alpha.md", "---\nname: alpha\ngoal: product\nschedule: 1h\nbudget: 0.10\n---\nFind release blockers.")
	writeFixture(t, root, "agents/manual.md", "---\nname: manual\ngoal: product\nbudget: 0.10\n---\nInspect on request.")

	agents, err := agent.DiscoverStrict(filepath.Join(root, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	goals, err := portfolio.DiscoverGoals(filepath.Join(root, "goals"))
	if err != nil {
		t.Fatal(err)
	}
	alpha := agent.FindByName(agents, "alpha")
	saveFixtureRun(t, root, store.Run{
		Agent: "alpha", Goal: "product", AgentHash: alpha.Hash, GoalHash: goals["product"].Hash,
		Status: "success", CostUSD: 0.05, StartedAt: now,
		OutcomeRatings: []store.OutcomeRating{{Value: "useful", Source: "human", RatedAt: now}},
	})

	got, err := Snapshot(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "portfolio" || *got.Summary.RemainingUSD != 0.95 {
		t.Fatalf("summary = %+v", got.Summary)
	}
	if len(got.Goals) != 1 || got.Goals[0].UsefulOutcomes != 1 || *got.Goals[0].CostPerUsefulUSD != 0.05 {
		t.Fatalf("goals = %+v", got.Goals)
	}
	if got.Agents[0].Name != "alpha" || got.Agents[0].Decision != "highest verified return" || got.Agents[0].RatedSamples != 1 {
		t.Fatalf("alpha = %+v", got.Agents[0])
	}
	if got.Agents[1].Name != "manual" || got.Agents[1].Decision != "manual only" {
		t.Fatalf("manual = %+v", got.Agents[1])
	}
}

func TestSnapshotLimitsRecentRuns(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 55; i++ {
		saveFixtureRun(t, root, store.Run{
			ID:        "run-" + strconv.Itoa(i),
			Agent:     "scheduled",
			Status:    "success",
			StartedAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}

	got, err := Snapshot(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.RecentRuns) != 50 || !got.RecentRuns[0].StartedAt.Equal(now) {
		t.Fatalf("runs = %+v", got.RecentRuns)
	}
}

func TestSnapshotNamesCorruptRunFile(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "runs/corrupt.json", "{")

	_, err := Snapshot(root, time.Now())
	if err == nil || !strings.Contains(err.Error(), "corrupt.json") {
		t.Fatalf("error = %v", err)
	}
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func saveFixtureRun(t *testing.T, root string, run store.Run) {
	t.Helper()
	if err := store.New(root).SaveRun(&run); err != nil {
		t.Fatal(err)
	}
}
