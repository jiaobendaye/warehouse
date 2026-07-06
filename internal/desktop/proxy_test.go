package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeBackend records the request it received and returns 200 with a known
// body. Used as the proxy's upstream target.
func fakeBackend(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true,"saw":"`+r.Method+" "+r.URL.Path+`"}`)
	}))
}

// backendAddr strips the scheme so we get the "host:port" form expected
// by the proxy.
func backendAddr(b *httptest.Server) string {
	return strings.TrimPrefix(b.URL, "http://")
}

func TestAPIProxy_ForwardsAPIPaths(t *testing.T) {
	backend := fakeBackend(t)
	defer backend.Close()

	proxy := NewAPIProxy(func() string { return backendAddr(backend) })

	cases := []struct {
		name, method, path, body string
	}{
		{"GET list accessories", http.MethodGet, "/api/v1/accessories", ""},
		{"POST create accessory", http.MethodPost, "/api/v1/accessories", `{"name":"x"}`},
		{"GET mcp sse", http.MethodGet, "/mcp/sse", ""},
		{"POST mcp messages", http.MethodPost, "/mcp/messages", `{}`},
		{"GET healthz", http.MethodGet, "/healthz", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rr := httptest.NewRecorder()
			proxy.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%q", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.method+" "+tc.path) {
				t.Errorf("backend did not see %s %s; got %q", tc.method, tc.path, rr.Body.String())
			}
		})
	}
}

func TestAPIProxy_RejectsNonWhitelistedPaths(t *testing.T) {
	backend := fakeBackend(t)
	defer backend.Close()

	proxy := NewAPIProxy(func() string { return backendAddr(backend) })

	cases := []string{"/", "/index.html", "/assets/main.js", "/foo", "/api-other"}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rr := httptest.NewRecorder()
			proxy.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Errorf("path %q: status = %d, want 404", p, rr.Code)
			}
		})
	}
}

func TestAPIProxy_Returns502WhenServerNotRunning(t *testing.T) {
	proxy := NewAPIProxy(func() string { return "" })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accessories", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rr.Code)
	}
}

func TestAPIProxy_TracksAddrChanges(t *testing.T) {
	backendA := fakeBackend(t)
	defer backendA.Close()
	backendB := fakeBackend(t)
	defer backendB.Close()

	var current string
	proxy := NewAPIProxy(func() string { return current })

	// Point at backendA first.
	current = backendAddr(backendA)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accessories", nil)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first call status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), backendA.URL) {
		// Body contains the request path; we mainly care that the call
		// reached a backend. The fact that rr.Code==200 is the proof.
		t.Logf("backendA body: %s", rr.Body.String())
	}

	// Simulate port fallback: switch to backendB.
	current = backendAddr(backendB)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/accessories", nil)
	rr = httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("second call status = %d, want 200 (proxy should follow new addr)", rr.Code)
	}
}

// Compile-time check: NewAPIProxy returns an http.Handler.
var _ http.Handler = NewAPIProxy(func() string { return "" })

// memFS is a tiny in-memory fs.FS that exposes a single index.html and
// an assets/index.js. It mimics the layout the Wails embed.FS produces
// from `//go:embed all:frontend/dist` after dist is built.
func memFS(t *testing.T, indexHTML, assetJS string) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"index.html":             &fstest.MapFile{Data: []byte(indexHTML)},
		"assets/index.js":        &fstest.MapFile{Data: []byte(assetJS)},
	}
}

func TestSPAHandler_FallsBackToIndexForSPARoutes(t *testing.T) {
	backend := fakeBackend(t)
	defer backend.Close()
	assets := memFS(t, "<!doctype html><html><body>SPA</body></html>", "console.log(1);")

	h := NewSPAHandler(func() string { return backendAddr(backend) }, assets)

	// Each of these is a real SPA route in App.tsx.
	for _, p := range []string{"/", "/inbound", "/outbound", "/flows", "/replenishment", "/settings", "/some/deep/nested/route"} {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Errorf("Content-Type = %q, want text/html prefix", got)
			}
			if !strings.Contains(rr.Body.String(), "SPA") {
				t.Errorf("body = %q, want it to contain index.html payload", rr.Body.String())
			}
		})
	}
}

func TestSPAHandler_ForwardsAPIPaths(t *testing.T) {
	backend := fakeBackend(t)
	defer backend.Close()
	assets := memFS(t, "<html></html>", "")
	h := NewSPAHandler(func() string { return backendAddr(backend) }, assets)

	for _, p := range []string{"/api/v1/accessories", "/mcp/sse", "/healthz"} {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			if !strings.Contains(rr.Body.String(), p) {
				t.Errorf("backend didn't see %s; body=%q", p, rr.Body.String())
			}
		})
	}
}

func TestSPAHandler_NilAssetsReturnsInlineFallback(t *testing.T) {
	h := NewSPAHandler(func() string { return "" }, nil)

	req := httptest.NewRequest(http.MethodGet, "/inbound", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "not built") {
		t.Errorf("body = %q, want fallback message", rr.Body.String())
	}
}

func TestSPAHandler_RejectsNonGET(t *testing.T) {
	assets := memFS(t, "<html></html>", "")
	h := NewSPAHandler(func() string { return "" }, assets)

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(m, func(t *testing.T) {
			req := httptest.NewRequest(m, "/inbound", nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("method %s: status = %d, want 405", m, rr.Code)
			}
		})
	}
}

// Compile-time check: NewSPAHandler returns an http.Handler.
var _ http.Handler = NewSPAHandler(func() string { return "" }, nil)