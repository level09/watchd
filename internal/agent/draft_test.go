package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftRendersCanonicalAgentAndCreatesItOnce(t *testing.T) {
	draft := Draft{
		Name:         "repo-health",
		Goal:         "product",
		Schedule:     "1h",
		Model:        "sonnet",
		Budget:       0.2,
		Instructions: "Find one release blocker.",
	}
	want := `---
name: repo-health
goal: product
schedule: 1h
model: sonnet
permission_mode: default
max_turns: 10
budget: 0.2
---
# repo-health
Find one release blocker.
`

	content, err := draft.Render()
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("content:\n%s", content)
	}

	dir := filepath.Join(t.TempDir(), "agents")
	path, err := Create(dir, draft)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("created content:\n%s", data)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("created agent is invalid: %v", err)
	}
	if _, err := Create(dir, draft); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate create error = %v", err)
	}
}

func TestDraftRejectsUnsafeOrIncompleteInput(t *testing.T) {
	cases := []struct {
		name  string
		draft Draft
		want  string
	}{
		{name: "path", draft: Draft{Name: "../escape", Instructions: "Inspect."}, want: "name"},
		{name: "uppercase", draft: Draft{Name: "BadName", Instructions: "Inspect."}, want: "name"},
		{name: "surrounding spaces", draft: Draft{Name: " valid ", Instructions: "Inspect."}, want: "name"},
		{name: "empty instructions", draft: Draft{Name: "valid"}, want: "instructions"},
		{name: "negative budget", draft: Draft{Name: "valid", Instructions: "Inspect.", Budget: -1}, want: "budget"},
		{name: "bad schedule", draft: Draft{Name: "valid", Instructions: "Inspect.", Schedule: "hourly"}, want: "schedule"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.draft.Render()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Render() error = %v", err)
			}
		})
	}
}
