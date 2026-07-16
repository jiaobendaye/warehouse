// Integration tests for GET /api/v1/exports/{filename}.
//
// The handler serves files written by the MCP export tools, so the
// happy-path test mirrors that flow: write a file into ExportsDir via
// the API/Services plumbing, hit the endpoint, verify the bytes round-
// trip with the right headers. The negative cases pin down the
// security boundaries (filename regex, path-traversal guard, 404).
package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/api"
	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// newRouterWithExports mirrors newRouter but plumbs ExportsDir so the
// /api/v1/exports/{filename} endpoint is mounted. Returns the handler
// plus the exports directory so tests can both write and read.
//
// We construct the wiring inline (rather than factoring out a helper)
// because only this test file needs an ExportsDir — adding a parameter
// to newRouter would force every existing test to set it.
func newRouterWithExports(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	exportsDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(exportsDir, 0o755); err != nil {
		t.Fatalf("mkdir exports: %v", err)
	}

	accRepo := repo.NewAccessoryRepo(d)
	flowRepo := repo.NewFlowRepo(d)
	svcs := api.Services{
		Accessory:     service.NewAccessoryService(d, accRepo, flowRepo),
		Stock:         service.NewStockService(accRepo, flowRepo, d),
		Flow:          service.NewFlowService(flowRepo),
		Replenishment: service.NewReplenishmentService(accRepo),
		ExportsDir:    exportsDir,
	}
	return api.NewRouter(svcs, api.RouterOptions{AllowedOrigins: []string{"*"}}), exportsDir
}

// TestExportsEndpoint_RoundTrip writes a synthetic .xlsx into the
// exports dir, GETs it via the API, and verifies the body bytes +
// Content-Disposition match what the handler should produce. This is
// the contract the MCP export tools depend on.
//
// We use a synthetic body (not a real xlsx) because the HTTP plumbing
// doesn't need to know it's a spreadsheet; the real export pipeline
// is covered by the MCP integration tests.
func TestExportsEndpoint_RoundTrip(t *testing.T) {
	h, dir := newRouterWithExports(t)

	filename := "accessories_20260706_153045.xlsx"
	body := []byte("PK\x03\x04fake-xlsx-body")
	wantSHA := sha256.Sum256(body)
	wantSHAHex := hex.EncodeToString(wantSHA[:])

	if err := os.WriteFile(filepath.Join(dir, filename), body, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/"+filename, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, got)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="`+filename+`"` {
		t.Errorf("Content-Disposition = %q, want attachment with filename", cd)
	}

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("body mismatch: got %q, want %q", got, body)
	}
	if gotSHA := sha256.Sum256(got); gotSHA != wantSHA {
		t.Errorf("body sha256 mismatch: got %x, want %s", gotSHA, wantSHAHex)
	}
}

// TestExportsEndpoint_RelativeExportsDir pins down the coordinate-
// system fix: when ExportsDir is a relative path (the way main.go
// wires it from config.DBPath, e.g. "data/exports"), legitimate
// filenames must NOT trip the "filename escapes exports directory"
// defence-in-depth check.
//
// The bug this test guards: an earlier version called filepath.Clean
// on the relative ExportsDir but filepath.Abs on the joined path,
// comparing an absolute string against a relative prefix and falsely
// rejecting every request with 400 BAD_FILENAME.
func TestExportsEndpoint_RelativeExportsDir(t *testing.T) {
	// Build services + router with a RELATIVE exports dir. We have
	// to chdir into a temp dir so the relative path resolves
	// predictably, then restore the original cwd.
	absDir := t.TempDir()
	relDir := "exports" // relative — this is the whole point
	if err := os.MkdirAll(filepath.Join(absDir, relDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(absDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })

	d, err := db.Open(filepath.Join(absDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	accRepo := repo.NewAccessoryRepo(d)
	flowRepo := repo.NewFlowRepo(d)
	svcs := api.Services{
		Accessory:     service.NewAccessoryService(d, accRepo, flowRepo),
		Stock:         service.NewStockService(accRepo, flowRepo, d),
		Flow:          service.NewFlowService(flowRepo),
		Replenishment: service.NewReplenishmentService(accRepo),
		ExportsDir:    relDir,
	}
	h := api.NewRouter(svcs, api.RouterOptions{AllowedOrigins: []string{"*"}})

	filename := "accessories_20260706_153045.xlsx"
	body := []byte("PK\x03\x04fake-xlsx-body")
	if err := os.WriteFile(filepath.Join(absDir, relDir, filename), body, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/"+filename, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 (relative ExportsDir must work); body=%s", resp.StatusCode, got)
	}
}

// TestExportsEndpoint_PathTraversal pins down the filename regex:
// callers must not be able to escape ExportsDir by crafting a URL.
//
// The router itself already filters some "bad" patterns:
//
//   - "../etc/passwd" → net/http collapses ".." before routing; chi
//     never matches our handler. Result: 404, not 400.
//   - "subdir/foo.xlsx" → chi's {filename} segment does not match
//     across slashes, so the route is unmatched. Result: 404.
//   - "/etc/passwd" → same segment-mismatch reason, 404.
//
// Those are also fine — we just verify they don't return 200. The
// cases that DO reach our handler and must be rejected with 400 are
// single-segment names that look "almost right" but fail the regex:
// missing extension, wrong extension, or characters we don't allow.
func TestExportsEndpoint_PathTraversal(t *testing.T) {
	h, _ := newRouterWithExports(t)

	t.Run("router-level-rejection", func(t *testing.T) {
		// Either the router or our handler rejects these — both
		// are correct outcomes, as long as the file isn't served.
		for _, bad := range []string{"../etc/passwd", "subdir/foo.xlsx", "/etc/passwd"} {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/"+bad, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Errorf("%q got 200, want 400 or 404", bad)
			}
		}
	})

	t.Run("handler-rejects-bad-filename", func(t *testing.T) {
		// Single-segment names that DO route to our handler but
		// fail the regex → must be 400. (Anything with ".." or a
		// "/" gets collapsed by net/http before routing and never
		// reaches us; that's the previous subtest.)
		for _, bad := range []string{
			"noext",
			"accessories_20260706_153045",    // no .xlsx suffix
			"accessories_20260706_153045.txt", // wrong extension
		} {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/"+bad, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("%q got %d, want 400; body=%s", bad, resp.StatusCode, body)
			}
		}
	})
}

// TestExportsEndpoint_NotFound verifies a filename that passes
// validation but isn't on disk returns 404. Important so callers can
// distinguish "wrong name" from "real error".
func TestExportsEndpoint_NotFound(t *testing.T) {
	h, _ := newRouterWithExports(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/never_written_20990101_000000.xlsx", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, got)
	}
}

// TestExportsEndpoint_NotMountedWhenDirEmpty guards the
// "ExportsDir empty → no route" branch. Without this test a future
// refactor could break the conditional mount and the breakage would
// only show up in test setups that don't pass ExportsDir.
func TestExportsEndpoint_NotMountedWhenDirEmpty(t *testing.T) {
	// newRouter (no ExportsDir) — confirms the route returns 404 from
	// the router, not 400 from our handler. chi's default for an
	// unmatched route is 404.
	h := newRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/exports/anything.xlsx", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (route should be unmounted)", resp.StatusCode)
	}
}