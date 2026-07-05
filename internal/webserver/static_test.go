package webserver

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestResolveStaticRoot_DistAtTopLevel mirrors how production wires the
// embed: `//go:embed all:frontend/dist` produces an FS rooted at the package
// directory with the dist output nested under "frontend/dist". The resolver
// must walk down to find index.html, otherwise the SPA handler renders a
// directory listing of the project root instead of the index page.
func TestResolveStaticRoot_DistAtTopLevel(t *testing.T) {
	assets := fstest.MapFS{
		"frontend/dist/index.html":     &fstest.MapFile{Data: []byte("<html>dist</html>")},
		"frontend/dist/assets/app.js":  &fstest.MapFile{Data: []byte("app")},
		"main.go":                      &fstest.MapFile{Data: []byte("package main")},
	}

	got, err := resolveStaticRoot(assets)
	if err != nil {
		t.Fatalf("resolveStaticRoot: %v", err)
	}

	if _, err := fs.Stat(got, "index.html"); err != nil {
		t.Fatalf("resolved root missing index.html: %v", err)
	}
	if _, err := fs.Stat(got, "assets/app.js"); err != nil {
		t.Fatalf("resolved root missing assets/app.js: %v", err)
	}
	// And the caller-side files must NOT be reachable via the resolved root.
	if _, err := fs.Stat(got, "main.go"); err == nil {
		t.Errorf("resolved root leaked caller-side files (main.go reachable)")
	}
}

// TestResolveStaticRoot_IndexAtRoot covers the case where the FS is already
// rooted at the dist directory (e.g. os.DirFS("frontend/dist") or a Sub
// constructed at the call site). The resolver must use it as-is without
// descending further.
func TestResolveStaticRoot_IndexAtRoot(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>root</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("app")},
	}

	got, err := resolveStaticRoot(assets)
	if err != nil {
		t.Fatalf("resolveStaticRoot: %v", err)
	}

	data, err := fs.ReadFile(got, "index.html")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "root") {
		t.Errorf("expected root index.html, got %q", data)
	}
}

// TestResolveStaticRoot_NoIndexReturnsError ensures a malformed embed (no
// index.html anywhere reachable) signals failure rather than picking an
// arbitrary subdirectory.
func TestResolveStaticRoot_NoIndexReturnsError(t *testing.T) {
	assets := fstest.MapFS{
		"frontend/dist/app.js": &fstest.MapFile{Data: []byte("app")},
	}
	if _, err := resolveStaticRoot(assets); err == nil {
		t.Fatal("expected error when no index.html is reachable")
	}
}

// TestStaticHandler_ServesIndexFromEmbeddedDist is the regression test for
// the production bug: opening "/" used to render a directory listing
// because fs.Sub(assets, ".") is a no-op for //go:embed all:frontend/dist.
// We rebuild a handler backed by a production-shaped embed (MapFS) and
// assert the response is the index page, not a directory listing.
func TestStaticHandler_ServesIndexFromEmbeddedDist(t *testing.T) {
	assets := fstest.MapFS{
		"frontend/dist/index.html":    &fstest.MapFile{Data: []byte("<html>ok</html>")},
		"frontend/dist/assets/app.js": &fstest.MapFile{Data: []byte("app")},
	}

	sub, err := resolveStaticRoot(assets)
	if err != nil {
		t.Fatalf("resolveStaticRoot: %v", err)
	}

	// Replicate the SPA handler logic against the resolved root.
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	fileSrv := http.FileServer(http.FS(sub))

	ts := httptest.NewServer(fileSrv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if strings.Contains(body, "frontend/") {
		t.Errorf("response leaked embed prefix (directory listing): %q", body)
	}
	if !strings.Contains(body, "ok") {
		t.Errorf("expected embedded index.html content, got %q", body)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("expected text/html Content-Type, got %q",
			resp.Header.Get("Content-Type"))
	}

	_ = data // data is what the SPA fallback path would serve
}