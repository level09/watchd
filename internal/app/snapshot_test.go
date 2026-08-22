package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
