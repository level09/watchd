package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAgent(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPortfolioAgent(t *testing.T) {
	path := writeAgent(t, `---
name: repair
goal: product
budget: 0.25
verify: go test ./...
verify_timeout: 90s
---
Repair the failure.`)
	a, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.Goal != "product" || a.Verify != "go test ./..." {
		t.Fatalf("portfolio fields lost: %+v", a)
	}
	if got := a.VerificationTimeout(); got != 90*time.Second {
		t.Fatalf("verification timeout = %s", got)
	}
}

func TestVerificationTimeoutDefault(t *testing.T) {
	path := writeAgent(t, `---
goal: product
budget: 0.25
verify: test -f ready
---
Repair it.`)
	a, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.VerificationTimeout(); got != 2*time.Minute {
		t.Fatalf("verification timeout = %s", got)
	}
}

func TestRejectInvalidVerifierConfiguration(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{name: "verifier without goal", yaml: "verify: true"},
		{name: "timeout without verifier", yaml: "goal: product\nverify_timeout: 1m"},
		{name: "invalid timeout", yaml: "goal: product\nverify: true\nverify_timeout: soon"},
		{name: "zero timeout", yaml: "goal: product\nverify: true\nverify_timeout: 0s"},
		{name: "negative timeout", yaml: "goal: product\nverify: true\nverify_timeout: -1s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeAgent(t, "---\n"+tt.yaml+"\n---\nRun.")
			if _, err := Load(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDiscoverStrictRejectsInvalidAgent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverStrict(dir); err == nil {
		t.Fatal("expected strict discovery error")
	}
	if agents, err := Discover(dir); err != nil || len(agents) != 0 {
		t.Fatalf("legacy discovery = %v, %v", agents, err)
	}
}
