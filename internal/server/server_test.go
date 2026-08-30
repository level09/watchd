package server

import (
	"bytes"
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

func TestAgentPreviewAndCreateWritesApprovedContent(t *testing.T) {
	root := t.TempDir()
	handler := newHandler(root, time.Now)
	payload := []byte(`{"name":"repo-health","schedule":"1h","model":"sonnet","budget":0.2,"instructions":"Find one release blocker."}`)

	preview := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/preview", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(preview, request)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var previewBody struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewBody); err != nil {
		t.Fatal(err)
	}
	if previewBody.Path != "agents/repo-health.md" || !strings.Contains(previewBody.Content, "name: repo-health") {
		t.Fatalf("preview = %+v", previewBody)
	}
	if _, err := os.Stat(filepath.Join(root, previewBody.Path)); !os.IsNotExist(err) {
		t.Fatalf("preview wrote a file: %v", err)
	}

	createPayload := []byte(`{"name":"repo-health","schedule":"1h","model":"sonnet","budget":0.2,"instructions":"Find one release blocker.","approved_content":` + string(mustJSON(t, previewBody.Content)) + `}`)
	created := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(createPayload))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(root, previewBody.Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != previewBody.Content {
		t.Fatalf("created content:\n%s", data)
	}
}

func TestAgentCreateRejectsChangedPreviewAndExistingFile(t *testing.T) {
	root := t.TempDir()
	handler := newHandler(root, time.Now)
	payload := `{"name":"safe","model":"sonnet","instructions":"Inspect.","approved_content":"changed"}`

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "preview") {
		t.Fatalf("mismatch status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "agents", "safe.md")); !os.IsNotExist(err) {
		t.Fatalf("mismatch wrote a file: %v", err)
	}

	writeFixture(t, root, "agents/safe.md", "existing")
	previewContent := "---\nname: safe\nschedule: \"\"\nmodel: sonnet\npermission_mode: default\nmax_turns: 10\n---\n# safe\nInspect.\n"
	payload = `{"name":"safe","model":"sonnet","instructions":"Inspect.","approved_content":` + string(mustJSON(t, previewContent)) + `}`
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "already exists") {
		t.Fatalf("existing status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentWritesRejectCrossOriginAndNonJSONRequests(t *testing.T) {
	handler := newHandler(t.TempDir(), time.Now)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/preview", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://example.com")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/agents/preview", strings.NewReader("name=safe"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentPreviewRequiresARealGoalForScheduledPortfolioAgent(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "watchd.yaml", "daily_budget: 1\n")
	writeFixture(t, root, "goals/product.md", "---\nname: product\n---\nShip useful software.")
	handler := newHandler(root, time.Now)

	for _, tc := range []struct {
		name    string
		payload string
		status  int
	}{
		{name: "missing", payload: `{"name":"scan","schedule":"1h","instructions":"Inspect."}`, status: http.StatusBadRequest},
		{name: "unknown", payload: `{"name":"scan","goal":"missing","schedule":"1h","instructions":"Inspect."}`, status: http.StatusBadRequest},
		{name: "known", payload: `{"name":"scan","goal":"product","schedule":"1h","instructions":"Inspect."}`, status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/preview", strings.NewReader(tc.payload))
			request.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

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

func TestDashboardServesAgentCreationFlow(t *testing.T) {
	handler := newHandler(t.TempDir(), time.Now)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, hook := range []string{`id="open-agent-form"`, `id="agent-form"`, `id="agent-preview"`, `id="run-dialog"`, "Preview file", "Create agent", "Run now"} {
		if !strings.Contains(response.Body.String(), hook) {
			t.Fatalf("dashboard missing creation control %q", hook)
		}
	}

	script := httptest.NewRecorder()
	handler.ServeHTTP(script, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	for _, endpoint := range []string{"/api/v1/agents/preview", "/api/v1/agents", "/api/v1/runs", "/api/v1/outcomes"} {
		if !strings.Contains(script.Body.String(), endpoint) {
			t.Fatalf("dashboard script missing %q", endpoint)
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

func TestRunAPIQueuesKnownAgentAndRejectsUnknownAgent(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "agents/scan.md", "---\nname: scan\n---\nInspect.\n")
	handler := newHandler(root, time.Now)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{"agent":"missing"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{"agent":"scan"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"agent":"scan"`) {
		t.Fatalf("queue status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOutcomeAPIValidatesAndRatesRun(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "runs/run-1.json", `{"id":"run-1","agent":"scan","status":"success","result":"done"}`)
	handler := newHandler(root, time.Now)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/outcomes", strings.NewReader(`{"run_id":"run-1","value":"useful","note":"clear"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"source":"human"`) {
		t.Fatalf("rate status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/outcomes", strings.NewReader(`{"run_id":"run-1","value":"bad"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", response.Code, response.Body.String())
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

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
