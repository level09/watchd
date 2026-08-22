package server

import (
	"context"
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
		{path: "/", contentType: "text/html", body: "Loading watchd..."},
		{path: "/app.js", contentType: "text/javascript", body: "watchd"},
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
	if response.Code != http.StatusInternalServerError ||
		!strings.Contains(response.Body.String(), `"code":"snapshot_invalid"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
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

func TestServeStopsOnContextCancellation(t *testing.T) {
	server, err := New(t.TempDir(), "127.0.0.1:0", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.Serve(ctx) }()
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
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
