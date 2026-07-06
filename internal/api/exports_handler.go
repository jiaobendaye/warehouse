// Package api — exports handler.
//
// GET /api/v1/exports/{filename} serves back a previously-written export
// file from the on-disk ExportsDir. The companion write paths are the
// MCP accessory.export / replenishment.export tools (and any future
// internal exporter); we deliberately do NOT regenerate the xlsx here —
// the file already exists, the caller wants the bytes it just learned
// about, full stop.
//
// Why a dedicated endpoint instead of reusing the live export endpoints
// (/api/v1/accessories/export etc.):
//
//   - Those endpoints regenerate the xlsx on every call, so a caller
//     that wants the exact snapshot the MCP tool produced would get a
//     different file (timestamps, races with concurrent writes).
//   - Serving from disk via http.ServeFile means we stream the body,
//     don't load it into memory, and let the standard library handle
//     range requests, ETag, Last-Modified.
//
// Security: filename validation is strict. Only [A-Za-z0-9._-] and an
// .xlsx suffix are accepted; anything else (including "..", "/", empty
// strings, leading dots) gets a 400. After validation we still resolve
// the final path and re-check it lives inside ExportsDir via
// filepath.Clean — defence in depth against symlinks and any future
// filename pattern we forget to whitelist.
package api

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

// exportFilenamePattern restricts accepted filenames to the shape the
// MCP tools actually produce: <prefix>_<YYYYMMDD>_<HHMMSS>.xlsx. Any
// deviation is rejected with 400. Keeping the pattern narrow means a
// successful match proves the file came from one of our writers (or a
// human typing the same format), not from an arbitrary upload attempt.
var exportFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.xlsx$`)

// ExportsHandler serves files from ExportsDir under GET
// /api/v1/exports/{filename}. A zero-value handler is unusable;
// NewExportsHandler panics if ExportsDir is empty so wiring mistakes
// surface at startup rather than as confusing 500s on the first
// request.
type ExportsHandler struct {
	exportsDir string
}

// NewExportsHandler builds the handler. Panics if dir is empty —
// callers that don't want the static endpoint should not construct
// this handler.
func NewExportsHandler(dir string) *ExportsHandler {
	if dir == "" {
		panic("api.NewExportsHandler: ExportsDir must not be empty")
	}
	return &ExportsHandler{exportsDir: dir}
}

// ServeHTTP — GET /api/v1/exports/{filename}
//
// Behaviour:
//
//   - Bad filename (regex mismatch, traversal, missing suffix) → 400.
//   - Filename passes validation but file is not on disk → 404.
//   - File exists → 200 with the xlsx body, Content-Type and
//     Content-Disposition set the same way the live export endpoints
//     do, so a browser save-as produces an identical filename.
//
// We do not require the file to have been produced by an MCP export
// tool today — the regex is a strong hint, but a future internal job
// that writes a different prefix can still be served by widening the
// pattern in one place.
func (h *ExportsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if filename == "" || !exportFilenamePattern.MatchString(filename) {
		WriteError(w, http.StatusBadRequest, "BAD_FILENAME",
			"filename must match "+exportFilenamePattern.String())
		return
	}

	// Belt-and-braces: even with the regex, resolve and re-check the
	// final path lives inside ExportsDir. Defends against a future
	// loosening of the pattern (e.g. allowing subdirs) that would
	// otherwise be exploitable.
	//
	// Both sides of the comparison MUST go through filepath.Abs so
	// they share a coordinate system: ExportsDir is typically passed
	// in as a relative path (e.g. "data/exports" — that's how
	// main.go wires it from config.DBPath), while filepath.Abs of
	// the joined path resolves against cwd. Comparing an absolute
	// "abs" against a relative "cleanRoot" would falsely flag every
	// legitimate request as path-traversal.
	absRoot, err := filepath.Abs(h.exportsDir)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	cleanRoot := filepath.Clean(absRoot)

	abs, err := filepath.Abs(filepath.Join(absRoot, filename))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	if !strings.HasPrefix(abs, cleanRoot+string(filepath.Separator)) && abs != cleanRoot {
		WriteError(w, http.StatusBadRequest, "BAD_FILENAME",
			"filename escapes exports directory")
		return
	}

	// http.ServeFile handles Content-Type sniffing, Last-Modified,
	// ETag, range requests, and 404-on-missing for us. We add the
	// download disposition so browsers behave the same way they do
	// for the live export endpoints.
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	http.ServeFile(w, r, abs)
}