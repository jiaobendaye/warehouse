package webserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	InitStatic(os.DirFS("testdata"))
	os.Exit(m.Run())
}

// Helpers

func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func mockRESTHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/accessories", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	}))
	mux.Handle("/api/v1/nonexistent", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"not found"}}`))
	}))
	return mux
}

// Tests

func TestServer_Healthz_ReturnsOK(t *testing.T) {
	s := New(DefaultConfig(), noopHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" {
		t.Errorf("expected status=ok, got %q", body.Status)
	}
	if body.Version != "0.1.0" {
		t.Errorf("expected version=0.1.0, got %q", body.Version)
	}
}

func TestServer_ServesEmbeddedIndex(t *testing.T) {
	s := New(DefaultConfig(), noopHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Test") {
		t.Errorf("expected body to contain 'Test', got: %s", string(body))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html Content-Type, got: %s", ct)
	}
}

func TestServer_ServesREST(t *testing.T) {
	s := New(DefaultConfig(), mockRESTHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/accessories")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"data"`) {
		t.Errorf("expected JSON with data key, got: %s", string(body))
	}
}

func TestServer_RejectsRemoteByDefault(t *testing.T) {
	s := New(DefaultConfig(), noopHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no CORS header for disallowed origin, got: %s",
			resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestServer_AllowsRemoteWithHostFlag(t *testing.T) {
	t.Run("WARN_on_0000", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(os.Stderr)

		cfg := Config{Host: "0.0.0.0", Port: 0}
		s := New(cfg, noopHandler(), noopHandler())
		done := make(chan error, 1)
		go func() {
			done <- s.ListenAndServe()
		}()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.Shutdown(ctx) // ignore error — server may not have started
		}()

		// Give the goroutine a moment to log the WARN message.
		time.Sleep(200 * time.Millisecond)

		if !strings.Contains(buf.String(), "WARN") {
			t.Errorf("expected WARN log for 0.0.0.0, got: %s", buf.String())
		}
	})

	t.Run("allows_explicit_origin", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.AllowedOrigins = []string{"http://remote.example"}
		s := New(cfg, noopHandler(), noopHandler())
		ts := httptest.NewServer(s.Handler())
		defer ts.Close()

		req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
		req.Header.Set("Origin", "http://remote.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.Header.Get("Access-Control-Allow-Origin") != "http://remote.example" {
			t.Errorf("expected CORS header for allowed origin, got: %s",
				resp.Header.Get("Access-Control-Allow-Origin"))
		}
	})
}

func TestServer_CustomPort(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 19999}
	s := New(cfg, noopHandler(), noopHandler())

	if s.Addr() != "127.0.0.1:19999" {
		t.Errorf("expected Addr 127.0.0.1:19999, got %s", s.Addr())
	}

	// Verify the handler works regardless of the configured port.
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestServer_NotFoundReturns404(t *testing.T) {
	s := New(DefaultConfig(), mockRESTHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "NOT_FOUND") {
		t.Errorf("expected error code NOT_FOUND, got: %s", string(body))
	}
}

func TestServer_SPAFallback(t *testing.T) {
	s := New(DefaultConfig(), noopHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// GET a non-existent path that is NOT /api, /mcp, or /healthz.
	resp, err := http.Get(ts.URL + "/some/spa/route")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Test") {
		t.Errorf("expected SPA fallback to return index.html, got: %s", string(body))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html Content-Type, got: %s", ct)
	}
}

func TestServer_LocalhostCORSAllowed(t *testing.T) {
	s := New(DefaultConfig(), noopHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	tests := []struct {
		name   string
		origin string
	}{
		{"localhost", "http://localhost:17880"},
		{"localhost_no_port", "http://localhost"},
		{"127_0_0_1", "http://127.0.0.1:17880"},
		{"127_0_0_1_no_port", "http://127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
			req.Header.Set("Origin", tt.origin)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.Header.Get("Access-Control-Allow-Origin") != tt.origin {
				t.Errorf("expected CORS header %q, got: %q",
					tt.origin, resp.Header.Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestServer_NonLocalhostCORSRejected(t *testing.T) {
	s := New(DefaultConfig(), noopHandler(), noopHandler())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	origins := []string{
		"http://192.168.1.1:17880",
		"http://10.0.0.1:17880",
		"http://evil.example",
		"https://localhost:17880", // https, not http
	}

	for _, origin := range origins {
		t.Run(origin, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
			req.Header.Set("Origin", origin)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Errorf("expected no CORS header for origin %q, got: %q",
					origin, resp.Header.Get("Access-Control-Allow-Origin"))
			}
		})
	}
}
