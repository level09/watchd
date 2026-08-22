package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/level09/watchd/internal/app"
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
	mux.HandleFunc("GET /{$}", serveAsset("ui/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.js", serveAsset("ui/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /style.css", serveAsset("ui/style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /api/v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := app.Snapshot(root, now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "snapshot_invalid", "snapshot is invalid")
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
