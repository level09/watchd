package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/level09/watchd/internal/agent"
	"github.com/level09/watchd/internal/app"
	"github.com/level09/watchd/internal/portfolio"
)

//go:embed ui/*
var uiFiles embed.FS

type Server struct {
	http     *http.Server
	listener net.Listener
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type agentDraftRequest struct {
	Name            string  `json:"name"`
	Goal            string  `json:"goal"`
	Schedule        string  `json:"schedule"`
	Model           string  `json:"model"`
	Budget          float64 `json:"budget"`
	Instructions    string  `json:"instructions"`
	ApprovedContent string  `json:"approved_content"`
}

type agentPreviewResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type runRequest struct {
	Agent string `json:"agent"`
}

type outcomeRequest struct {
	RunID string `json:"run_id"`
	Value string `json:"value"`
	Note  string `json:"note"`
}

func New(root, addr string, now func() time.Time) (*Server, error) {
	if err := validateLoopback(addr); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	return &Server{
		listener: listener,
		http: &http.Server{
			Handler:           newHandler(root, now),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}, nil
}

func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *Server) Serve(ctx context.Context) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.http.Serve(s.listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func validateLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if host == "localhost" || host == "::1" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("listen address %q is not loopback", addr)
}

func newHandler(root string, now func() time.Time) http.Handler {
	mux := http.NewServeMux()
	jobs := newJobManager(root)
	mux.HandleFunc("GET /{$}", serveAsset("ui/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.js", serveAsset("ui/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /style.css", serveAsset("ui/style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("POST /api/v1/agents/preview", func(w http.ResponseWriter, r *http.Request) {
		draft, _, ok := readAgentDraft(w, r)
		if !ok {
			return
		}
		content, err := draft.Render()
		if err != nil {
			writeError(w, http.StatusBadRequest, "agent_invalid", err.Error())
			return
		}
		if err := validateAgentDraft(root, draft); err != nil {
			writeError(w, http.StatusBadRequest, "agent_invalid", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, agentPreviewResponse{
			Path: filepath.ToSlash(filepath.Join("agents", draft.Name+".md")), Content: string(content),
		})
	})
	mux.HandleFunc("POST /api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		draft, request, ok := readAgentDraft(w, r)
		if !ok {
			return
		}
		content, err := draft.Render()
		if err != nil {
			writeError(w, http.StatusBadRequest, "agent_invalid", err.Error())
			return
		}
		if err := validateAgentDraft(root, draft); err != nil {
			writeError(w, http.StatusBadRequest, "agent_invalid", err.Error())
			return
		}
		if request.ApprovedContent != string(content) {
			writeError(w, http.StatusConflict, "preview_changed", "approved preview does not match the agent draft")
			return
		}
		path, err := agent.Create(filepath.Join(root, "agents"), draft)
		if err != nil {
			status := http.StatusInternalServerError
			code := "agent_create_failed"
			if errors.Is(err, agent.ErrExists) {
				status = http.StatusConflict
				code = "agent_exists"
			}
			writeError(w, status, code, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, agentPreviewResponse{
			Path: filepath.ToSlash(filepath.Join("agents", filepath.Base(path))), Content: string(content),
		})
	})
	mux.HandleFunc("POST /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		var request runRequest
		if !readJSONRequest(w, r, &request) {
			return
		}
		if strings.TrimSpace(request.Agent) == "" {
			writeError(w, http.StatusBadRequest, "run_invalid", "agent is required")
			return
		}
		job, err := jobs.Start(request.Agent)
		if err != nil {
			status, code := http.StatusInternalServerError, "run_failed"
			switch {
			case errors.Is(err, os.ErrNotExist):
				status, code = http.StatusNotFound, "agent_not_found"
			case errors.Is(err, errAgentRunning):
				status, code = http.StatusConflict, "run_in_progress"
			}
			writeError(w, status, code, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	})
	mux.HandleFunc("GET /api/v1/runs/{jobID}", func(w http.ResponseWriter, r *http.Request) {
		job, ok := jobs.Get(r.PathValue("jobID"))
		if !ok {
			writeError(w, http.StatusNotFound, "job_not_found", "run job not found")
			return
		}
		writeJSON(w, http.StatusOK, job)
	})
	mux.HandleFunc("POST /api/v1/outcomes", func(w http.ResponseWriter, r *http.Request) {
		var request outcomeRequest
		if !readJSONRequest(w, r, &request) {
			return
		}
		if strings.TrimSpace(request.RunID) == "" || strings.TrimSpace(request.Value) == "" {
			writeError(w, http.StatusBadRequest, "outcome_invalid", "run_id and value are required")
			return
		}
		run, err := jobs.service.RateOutcome(request.RunID, request.Value, request.Note)
		if err != nil {
			status, code := http.StatusBadRequest, "outcome_invalid"
			if strings.Contains(err.Error(), "not found") {
				status, code = http.StatusNotFound, "run_not_found"
			}
			writeError(w, status, code, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, run)
	})
	mux.HandleFunc("GET /api/v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := app.Snapshot(root, now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "snapshot_invalid", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(snapshot); err != nil {
			return
		}
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func readAgentDraft(w http.ResponseWriter, r *http.Request) (agent.Draft, agentDraftRequest, bool) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin_forbidden", "request origin must match the dashboard")
		return agent.Draft{}, agentDraftRequest{}, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "json_required", "content type must be application/json")
		return agent.Draft{}, agentDraftRequest{}, false
	}
	var request agentDraftRequest
	if !decodeJSON(w, r, &request) {
		return agent.Draft{}, agentDraftRequest{}, false
	}
	return agent.Draft{
		Name: request.Name, Goal: request.Goal, Schedule: request.Schedule,
		Model: request.Model, Budget: request.Budget, Instructions: request.Instructions,
	}, request, true
}

func readJSONRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin_forbidden", "request origin must match the dashboard")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "json_required", "content type must be application/json")
		return false
	}
	return decodeJSON(w, r, value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "request_invalid", err.Error())
		return false
	}
	return true
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == "http" && strings.EqualFold(parsed.Host, r.Host)
}

func validateAgentDraft(root string, draft agent.Draft) error {
	policy, err := portfolio.LoadPolicy(filepath.Join(root, "watchd.yaml"))
	if err != nil || policy == nil {
		return err
	}
	goal := strings.TrimSpace(draft.Goal)
	if strings.TrimSpace(draft.Schedule) != "" && goal == "" {
		return fmt.Errorf("goal is required for a scheduled portfolio agent")
	}
	if goal == "" {
		return nil
	}
	goals, err := portfolio.DiscoverGoals(filepath.Join(root, "goals"))
	if err != nil {
		return err
	}
	if goals[goal] == nil {
		return fmt.Errorf("goal %q does not exist", goal)
	}
	return nil
}

func serveAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		asset, err := fs.ReadFile(uiFiles, name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "asset_unavailable", "asset is unavailable")
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(asset)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	response := errorEnvelope{}
	response.Error.Code = code
	response.Error.Message = message
	writeJSON(w, status, response)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
