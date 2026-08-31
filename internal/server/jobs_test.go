package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJobManagerRejectsUnknownAgent(t *testing.T) {
	m := newJobManager(t.TempDir())
	if _, err := m.Start("missing"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v", err)
	}
}

func TestJobManagerRejectsDuplicateAgentRuns(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "agents/scan.md", "---\nname: scan\n---\nInspect.\n")
	m := newJobManager(root)
	job, err := m.Start("scan")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "queued" && job.Status != "running" {
		t.Fatalf("status = %q", job.Status)
	}
	if _, err := m.Start("scan"); err == nil {
		t.Fatal("duplicate run was accepted")
	}
}

func TestJobManagerReturnsMissingJob(t *testing.T) {
	if job, ok := newJobManager(t.TempDir()).Get("missing"); ok || job != nil {
		t.Fatalf("job=%+v ok=%v", job, ok)
	}
}

func TestJobManagerCompletesRun(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFixture(t, root, "agents/scan.md", "---\nname: scan\nmodel: haiku\n---\nInspect.\n")
	bin := t.TempDir()
	claude := filepath.Join(bin, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"ready\"}]'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := newJobManager(root)
	job, err := manager.Start("scan")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := manager.Get(job.ID)
		if ok && current.Status == "complete" {
			if current.Run == nil || current.Run.Result != "ready" {
				t.Fatalf("job = %+v", current)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, _ := manager.Get(job.ID)
	t.Fatalf("job did not complete: %+v", current)
}
