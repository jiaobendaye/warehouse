// Package desktop provides Wails bindings and GUI support.
// server_manager.go handles the lifecycle of the HTTP and MCP servers
// from within the GUI (no build tag — used by both wails and non-wails paths).
package desktop

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jiaobendaye/warehouse/internal/webserver"
)

// portScanLimit caps how many consecutive ports we try when falling back from
// a taken port. Keeps the search bounded so a pathological case (every port
// in a range held) fails fast instead of spinning through 60k ports.
const portScanLimit = 100

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
// If the configured port is already in use, walks forward to the next free
// port (up to portScanLimit attempts) and binds there.
func (m *ServerManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return nil
	}

	host := m.cfg.Host
	startPort := m.cfg.Port

	ln, port, err := listenFreePort(host, startPort, portScanLimit)
	if err != nil {
		return fmt.Errorf("no free port near %d: %w", startPort, err)
	}
	if port != startPort {
		log.Printf("port %d in use; falling back to %d", startPort, port)
	}

	m.srv = webserver.New(webserver.Config{
		Host: host,
		Port: port,
	}, m.cfg.APIHandler, m.cfg.MCPHandler)

	m.running = true

	go func() {
		if err := m.srv.ServeWith(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	log.Printf("HTTP server started on %s", m.srv.Addr())
	return nil
}

// listenFreePort tries to bind to host:startPort. If that fails (typically
// because the port is taken), it walks forward — startPort+1, startPort+2,
// … — until it finds a free port or hits maxAttempts. Returns the live
// listener so the caller can hand it to http.Server.Serve.
func listenFreePort(host string, startPort, maxAttempts int) (net.Listener, int, error) {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	for i := 0; i < maxAttempts; i++ {
		port := startPort + i
		if port < 1 || port > 65535 {
			break
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if err == nil {
			return ln, port, nil
		}
	}
	return nil, 0, fmt.Errorf("no free port in [%d,%d]", startPort, startPort+maxAttempts-1)
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