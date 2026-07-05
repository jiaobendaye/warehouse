// Package desktop provides Wails bindings and GUI support.
// server_manager.go handles the lifecycle of the HTTP and MCP servers
// from within the GUI (no build tag — used by both wails and non-wails paths).
package desktop

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jiaobendaye/warehouse/internal/webserver"
)

// ServerManager controls the lifecycle of the embedded HTTP server
// (REST API + static frontend + MCP endpoint). Both the Wails GUI and
// the non-Wails fallback use this to start/stop servers at runtime.
type ServerManager struct {
	mu       sync.Mutex
	cfg      ServerConfig
	srv      *webserver.Server
	running  bool
}

// ServerConfig carries the knobs the ServerManager needs.
type ServerConfig struct {
	Host       string
	Port       int
	APIHandler http.Handler
	MCPHandler http.Handler
}

// NewServerManager creates an idle manager. Call Start to launch servers.
func NewServerManager(cfg ServerConfig) *ServerManager {
	return &ServerManager{cfg: cfg}
}

// Start launches the HTTP server. Safe to call when already running (no-op).
func (m *ServerManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return nil
	}

	m.srv = webserver.New(webserver.Config{
		Host: m.cfg.Host,
		Port: m.cfg.Port,
	}, m.cfg.APIHandler, m.cfg.MCPHandler)

	go func() {
		if err := m.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	m.running = true
	log.Printf("HTTP server started on %s:%d", m.cfg.Host, m.cfg.Port)
	return nil
}

// Stop gracefully shuts down the HTTP server. Safe to call when not running.
func (m *ServerManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	m.running = false
	log.Println("HTTP server stopped")
	return nil
}

// IsRunning reports whether the server is currently accepting connections.
func (m *ServerManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Addr returns the address the server is listening on, or "" when stopped.
func (m *ServerManager) Addr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || m.srv == nil {
		return ""
	}
	return m.srv.Addr()
}