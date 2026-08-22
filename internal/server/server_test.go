package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandlerServesDashboardRoutes(t *testing.T) {
	root := t.TempDir()
	handler := newHandler(root, func() time.Time {
		return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	})

	cases := []struct {
		path        string
		contentType string
		body        string
	}{
		{path: "/", contentType: "text/html", body: "Portfolio summary"},
		{path: "/app.js", contentType: "text/javascript", body: "refreshInterval"},
		{path: "/style.css", contentType: "text/css", body: "sans-serif"},
		{path: "/api/v1/snapshot", contentType: "application/json", body: `"mode":"legacy"`},
		{path: "/healthz", contentType: "text/plain", body: "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, tc.contentType) {
				t.Fatalf("content type=%q", contentType)
			}
			if !strings.Contains(response.Body.String(), tc.body) {
				t.Fatalf("body=%s", response.Body.String())
			}
		})
	}
}

func TestDashboardContainsRequiredProjectionHooks(t *testing.T) {
	page, err := uiFiles.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"summary", "goals", "agents", "activity", "freshness", "error"} {
		if !strings.Contains(string(page), `id="`+id+`"`) {
			t.Fatalf("dashboard missing %q", id)
		}
	}

	script, err := uiFiles.ReadFile("ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"/api/v1/snapshot", "5000", "renderSnapshot", "showError"} {
		if !strings.Contains(string(script), hook) {
			t.Fatalf("dashboard script missing %q", hook)
		}
	}
}

func TestDashboardKeepsLastSuccessfulFreshness(t *testing.T) {
	script, err := uiFiles.ReadFile("ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{
		"let lastSuccessfulRefresh =",
		"lastSuccessfulRefresh = `Updated ${new Date(snapshot.generated_at).toLocaleTimeString()}`",
		"elements.freshness.textContent = `${lastSuccessfulRefresh} (stale)`",
	} {
		if !strings.Contains(string(script), hook) {
			t.Fatalf("dashboard freshness handling missing %q", hook)
		}
	}
}

func TestHandlerRejectsInvalidRequests(t *testing.T) {
	root := t.TempDir()
	handler := newHandler(root, time.Now)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/snapshot", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandlerReportsInvalidSnapshotWithoutFailingHealth(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "agents/broken.md", "not frontmatter")
	handler := newHandler(root, time.Now)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body errorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "snapshot_invalid" {
		t.Fatalf("error code = %q", body.Error.Code)
	}
	path := filepath.Join(root, "agents", "broken.md")
	if !strings.Contains(body.Error.Message, path) {
		t.Fatalf("error message %q does not name %q", body.Error.Message, path)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNewRejectsNonLoopbackAddresses(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:7337", "192.0.2.1:7337", ":7337"} {
		t.Run(addr, func(t *testing.T) {
			server, err := New(t.TempDir(), addr, time.Now)
			if err == nil {
				server.listener.Close()
				t.Fatal("New succeeded")
			}
		})
	}
}

func TestValidateLoopbackAcceptsLoopbackAddresses(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7337", "127.0.0.2:7337", "localhost:7337", "[::1]:7337"} {
		t.Run(addr, func(t *testing.T) {
			if err := validateLoopback(addr); err != nil {
				t.Fatalf("validateLoopback(%q) = %v", addr, err)
			}
		})
	}
}

func TestServeStopsOnContextCancellation(t *testing.T) {
	server, err := New(t.TempDir(), "127.0.0.1:0", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.Serve(ctx) }()
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop after cancellation")
	}
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := root + "/" + name
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
