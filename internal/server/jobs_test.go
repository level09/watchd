package server

import (
	"strings"
	"testing"
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
