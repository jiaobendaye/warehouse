package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Config holds the server configuration.
type Config struct {
	Host           string
	Port           int
	AllowedOrigins []string
}

// DefaultConfig returns a Config with standard defaults:
// Host=127.0.0.1, Port=17880.
func DefaultConfig() Config {
	return Config{
		Host: "127.0.0.1",
		Port: 17880,
	}
}

// Server is a process-internal HTTP server that hosts the static frontend,
// the REST API (delegated to chi), and the MCP endpoint.
type Server struct {
	httpServer *http.Server
	config     Config
	handler    http.Handler
}

// New builds a Server. apiHandler is the chi-based REST router from
// internal/api; mcpHandler is the HTTP handler from internal/mcp.
func New(cfg Config, apiHandler http.Handler, mcpHandler http.Handler) *Server {
	r := chi.NewRouter()

	// Global CORS middleware.
	r.Use(corsMiddleware(cfg.AllowedOrigins))

	// Health check.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": "0.1.0",
		})
	})

	// REST API (the internal chi router has its own middleware chain).
	r.Handle("/api", apiHandler)
	r.Handle("/api/*", apiHandler)

	// MCP endpoint (SSE and messages).
	r.Handle("/mcp", mcpHandler)
	r.Handle("/mcp/*", mcpHandler)

	// Static files with SPA fallback — catch-all for any unmatched path.
	r.Handle("/*", staticHandler())

	return &Server{
		config:  cfg,
		handler: r,
		httpServer: &http.Server{
			Addr:    fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Handler: r,
		},
	}
}

// ListenAndServe starts the HTTP server and blocks until Shutdown is called
// or an unrecoverable error occurs. If the configured host is "0.0.0.0" it
// logs a WARN about remote access being enabled.
func (s *Server) ListenAndServe() error {
	addr := s.httpServer.Addr
	host := s.config.Host
	port := s.config.Port

	log.Printf("starting server on %s", addr)
	if host == "0.0.0.0" {
		log.Printf("WARN: listening on 0.0.0.0 — remote access enabled (port %d)", port)
	}

	return s.httpServer.ListenAndServe()
}

// ServeWith accepts a pre-bound listener (used when port fallback has already
// chosen an alternative port) and serves on it. Updates Addr() to reflect the
// actual bound address so callers can report the real listen socket.
func (s *Server) ServeWith(ln net.Listener) error {
	s.httpServer.Addr = ln.Addr().String()
	if s.config.Host == "0.0.0.0" {
		log.Printf("WARN: listening on 0.0.0.0 — remote access enabled (port %s)",
			ln.Addr().String())
	}
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully shuts down the server with the given context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the configured listen address (host:port).
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

// Handler returns the full http.Handler (chi mux) so tests can wrap it in
// httptest.NewServer without starting a real listener.
func (s *Server) Handler() http.Handler {
	return s.handler
}
