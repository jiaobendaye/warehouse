package desktop

import (
	"bytes"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
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

// NewSPAHandler returns an http.Handler suitable for
// assetserver.Options.Handler. It runs the same proxy chain as
// NewAPIProxy for /api, /mcp and /healthz, and falls back to the
// embedded index.html for every other path so SPA deep links like
// /inbound or /replenishment don't 404 on hard refresh.
//
// Wails invokes this handler only when the embed.FS has no matching
// asset (see assetserver.Options docs): real built files like
// /assets/index-*.js are served by the asset server before this is
// called. So at the point of fallback the request is by construction
// not a static asset — serving index.html is the right move.
//
// # Maintenance contract
//
// This handler is intentionally path-agnostic on the SPA side: any
// non-API request that the embed.FS doesn't recognise falls through
// to index.html. That means **adding a new SPA route to App.tsx
// requires no change here** — the route simply won't be a real file
// in the build output, Wails will route it to this handler, and the
// React router picks it up from index.html.
//
// The only paths this handler distinguishes are the proxy whitelist
// (/api, /mcp, /healthz). Those prefixes are stable: new REST
// endpoints and new MCP methods are added *under* the existing
// prefixes, so the whitelist does not need to be touched when the
// API surface grows. The only thing that does require editing the
// whitelist is adding a brand-new top-level backend prefix (e.g. a
// hypothetical /admin or /metrics), and that is a deliberate,
// reviewable change rather than a per-route one.
func NewSPAHandler(addrFn func() string, assets fs.FS) http.Handler {
	return &spaHandler{
		proxy:     &apiProxy{addrFn: addrFn},
		assets:    assets,
		indexHTML: loadIndexHTML(assets),
	}
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

// spaHandler is the assetserver.Options.Handler installed in Wails GUI mode.
// Wails only invokes it after the embed.FS has already reported
// os.ErrNotExist for a GET, or for any non-GET request. That means
// serving index.html on a hit here is safe — real static assets
// (assets/*.js, favicon) are still served by Wails itself.
type spaHandler struct {
	proxy     *apiProxy
	assets    fs.FS
	indexHTML []byte
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Wails only sends us requests that the embed.FS couldn't satisfy.
	// For non-GET, return 405 — the embedded HTTP server doesn't accept
	// these on SPA paths anyway, and Wails itself rejects them upstream
	// for non-asset requests.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Non-GET could conceivably target the API; if so, let the proxy try.
	if r.Method != http.MethodGet && shouldProxyPath(r.URL.Path) {
		h.proxy.ServeHTTP(w, r)
		return
	}

	// /api, /mcp, /healthz reach the embedded HTTP server via the proxy.
	if shouldProxyPath(r.URL.Path) {
		h.proxy.ServeHTTP(w, r)
		return
	}

	// Anything else: serve index.html. /inbound, /outbound, /flows,
	// /replenishment, /settings — all SPA routes that don't have a
	// physical file in embed.FS.
	if len(h.indexHTML) == 0 {
		// Defensive: if InitStatic-like resolution failed to find
		// index.html, fall back to a tiny inline page rather than 404.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Warehouse frontend not built.</body></html>"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(h.indexHTML))
}

// loadIndexHTML locates index.html inside an embed.FS. The dist output
// may be nested under "frontend/dist/" depending on how the caller
// embedded it, so we walk a few levels deep — same heuristic as
// webserver.resolveStaticRoot, kept duplicated here so the desktop
// package doesn't have to depend on webserver internals.
func loadIndexHTML(assets fs.FS) []byte {
	if assets == nil {
		return nil
	}
	if data, err := fs.ReadFile(assets, "index.html"); err == nil {
		return data
	}
	const maxDepth = 4
	type frame struct {
		fsys  fs.FS
		depth int
	}
	queue := []frame{{fsys: assets, depth: 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxDepth {
			continue
		}
		entries, err := fs.ReadDir(cur.fsys, ".")
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub, err := fs.Sub(cur.fsys, e.Name())
			if err != nil {
				continue
			}
			if data, err := fs.ReadFile(sub, "index.html"); err == nil {
				return data
			}
			queue = append(queue, frame{fsys: sub, depth: cur.depth + 1})
		}
	}
	return nil
}

// keep import 'path' used — it's used by code added in the future and
// also documents that this file deals with URL paths.
var _ = path.Clean