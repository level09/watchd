package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAgentPersistsTheRunnerResult(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeServiceFixture(t, "agents/scan.md", "---\nname: scan\nmodel: haiku\n---\nInspect.")
	claudeDir := t.TempDir()
	claude := filepath.Join(claudeDir, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"found one\",\"total_cost_usd\":0.01}]'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", claudeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	run, err := New(root).RunAgent("scan")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "success" || run.Result != "found one" || run.CostUSD != 0.01 {
		t.Fatalf("run = %+v", run)
	}
	if _, err := os.Stat(filepath.Join(root, "runs", run.ID+".json")); err != nil {
		t.Fatalf("run ledger missing: %v", err)
	}
}

func TestRateOutcomeUsesHumanSource(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeServiceFixture(t, "agents/scan.md", "---\nname: scan\n---\nInspect.")
	writeServiceFixture(t, "runs/scan_1.json", `{"id":"scan_1","agent":"scan","status":"success"}`)

	run, err := New(root).RateOutcome("scan_1", "useful", "saved time")
	if err != nil {
		t.Fatal(err)
	}
	outcome := run.LatestOutcome()
	if outcome == nil || outcome.Value != "useful" || outcome.Source != "human" || outcome.Note != "saved time" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestRunAgentReportsMissingAgent(t *testing.T) {
	_, err := New(t.TempDir()).RunAgent("missing")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v", err)
	}
}

func writeServiceFixture(t *testing.T, name, content string) {
	t.Helper()
	path := name
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
