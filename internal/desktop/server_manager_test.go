package desktop

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// grabPort binds a throwaway listener to host:0, reads the chosen port, and
// closes it. There's a tiny TOCTOU window before the real listener claims the
// port — acceptable for these tests because nothing else binds in that window.
func grabPort(t *testing.T, host string) int {
	t.Helper()
	ln, err := net.Listen("tcp", host+":0")
	if err != nil {
		t.Fatalf("grab port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// noopHandler returns a 200 OK handler so the embedded router has something
// to dispatch to.
func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// TestServerManager_FallsBackWhenPortBusy confirms that when cfg.Port is
// already taken, Start() walks forward to the next free port and reports it.
func TestServerManager_FallsBackWhenPortBusy(t *testing.T) {
	host := "127.0.0.1"
	busy := grabPort(t, host)

	// Hold the port for the duration of the test.
	blocker, err := net.Listen("tcp", host+":"+strconv.Itoa(busy))
	if err != nil {
		t.Fatalf("hold port %d: %v", busy, err)
	}
	defer blocker.Close()

	mgr := NewServerManager(ServerConfig{
		Host:       host,
		Port:       busy,
		APIHandler: noopHandler(),
		MCPHandler: noopHandler(),
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()

	if !mgr.IsRunning() {
		t.Fatal("expected manager to be running")
	}

	addr := mgr.Addr()
	if addr == "" {
		t.Fatal("expected non-empty Addr")
	}
	if strings.HasSuffix(addr, ":"+strconv.Itoa(busy)) {
		t.Errorf("still bound to busy port %d (addr=%q)", busy, addr)
	}

	// The chosen port should be exactly busy+1 (the next attempt in the scan).
	want := host + ":" + strconv.Itoa(busy+1)
	if addr != want {
		t.Errorf("expected fallback addr %q, got %q", want, addr)
	}
}

// TestServerManager_FallsBackOverOccupiedRange verifies that several taken
// ports in a row are skipped — Start() should walk past them all to the
// first free one.
func TestServerManager_FallsBackOverOccupiedRange(t *testing.T) {
	host := "127.0.0.1"
	start := grabPort(t, host)

	blockers := make([]net.Listener, 0, 3)
	defer func() {
		for _, ln := range blockers {
			_ = ln.Close()
		}
	}()
	for i := 0; i < 3; i++ {
		ln, err := net.Listen("tcp", host+":"+strconv.Itoa(start+i))
		if err != nil {
			t.Fatalf("hold port %d: %v", start+i, err)
		}
		blockers = append(blockers, ln)
	}

	mgr := NewServerManager(ServerConfig{
		Host:       host,
		Port:       start,
		APIHandler: noopHandler(),
		MCPHandler: noopHandler(),
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()

	want := host + ":" + strconv.Itoa(start+3)
	if got := mgr.Addr(); got != want {
		t.Errorf("expected addr %q (skip 3 occupied ports), got %q", want, got)
	}
}

// TestServerManager_GivesUpAfterScanLimit makes sure Start returns an error
// rather than spinning forever when every port in the scan range is taken.
func TestServerManager_GivesUpAfterScanLimit(t *testing.T) {
	host := "127.0.0.1"
	start := grabPort(t, host)

	blockers := make([]net.Listener, 0, portScanLimit)
	defer func() {
		for _, ln := range blockers {
			_ = ln.Close()
		}
	}()
	for i := 0; i < portScanLimit; i++ {
		ln, err := net.Listen("tcp", host+":"+strconv.Itoa(start+i))
		if err != nil {
			t.Fatalf("hold port %d: %v", start+i, err)
		}
		blockers = append(blockers, ln)
	}

	mgr := NewServerManager(ServerConfig{
		Host:       host,
		Port:       start,
		APIHandler: noopHandler(),
		MCPHandler: noopHandler(),
	})
	err := mgr.Start()
	if err == nil {
		_ = mgr.Stop()
		t.Fatal("expected error when scan range is exhausted")
	}
	if mgr.IsRunning() {
		t.Error("manager must not be running after Start failed")
	}
}

// TestServerManager_StartIsIdempotent guards against double-start — calling
// Start twice should not bind a second port.
func TestServerManager_StartIsIdempotent(t *testing.T) {
	host := "127.0.0.1"
	port := grabPort(t, host)

	mgr := NewServerManager(ServerConfig{
		Host:       host,
		Port:       port,
		APIHandler: noopHandler(),
		MCPHandler: noopHandler(),
	})
	if err := mgr.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer func() { _ = mgr.Stop() }()

	first := mgr.Addr()
	if err := mgr.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if mgr.Addr() != first {
		t.Errorf("second Start changed addr: %q → %q", first, mgr.Addr())
	}
}