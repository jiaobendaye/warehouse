package desktop

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// NewAPIProxy returns an http.Handler that forwards /api/*, /mcp/* and
// /healthz requests to the address returned by addrFn (typically
// srvMgr.Addr). It is intended to be passed as assetserver.Options.Handler
// so the GUI's fetch('/api/v1/...') reaches the same backend that browser
// mode uses.
//
// Any path outside the proxy whitelist returns 404; that lets the Wails
// asset server still try its embedded assets before giving up, and keeps
// non-API GETs (SPA routes etc.) from being silently swallowed by the
// proxy.
//
// The proxy is rebuilt lazily on every request so it tracks addrFn after
// the server starts — including when the configured port was busy and
// the manager fell back to a different one.
func NewAPIProxy(addrFn func() string) http.Handler {
	return &apiProxy{addrFn: addrFn}
}

type apiProxy struct {
	addrFn func() string

	mu       sync.Mutex
	lastAddr string
	proxy    *httputil.ReverseProxy
}

// getProxy returns a ReverseProxy targeting the current addrFn() value,
// rebuilding it whenever the address changes (port fallback). Returns nil
// when addrFn() returns "" (server not running yet).
func (p *apiProxy) getProxy() *httputil.ReverseProxy {
	addr := p.addrFn()
	if addr == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if addr != p.lastAddr {
		target := &url.URL{Scheme: "http", Host: addr}
		p.proxy = httputil.NewSingleHostReverseProxy(target)
		p.lastAddr = addr
	}
	return p.proxy
}

func (p *apiProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !shouldProxyPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	proxy := p.getProxy()
	if proxy == nil {
		log.Printf("apiProxy: backend server not running, %s %s", r.Method, r.URL.Path)
		http.Error(w, "backend server not running", http.StatusBadGateway)
		return
	}
	proxy.ServeHTTP(w, r)
}

// shouldProxyPath returns true for paths the embedded HTTP server serves:
// /api, /api/*, /mcp, /mcp/*, /healthz.
func shouldProxyPath(path string) bool {
	switch {
	case path == "/api" || strings.HasPrefix(path, "/api/"):
		return true
	case path == "/mcp" || strings.HasPrefix(path, "/mcp/"):
		return true
	case path == "/healthz":
		return true
	}
	return false
}