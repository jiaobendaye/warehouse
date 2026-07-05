package webserver

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// InitStatic must be called once (from main) with the frontend build output.
// After this call, the SPA handler returned by staticHandler() serves from
// the provided filesystem. Safe to call only once.
// If assets is nil or missing index.html, a minimal fallback page is used.
func InitStatic(assets fs.FS) {
	if staticSub != nil {
		return // already initialized
	}
	if assets == nil {
		staticIndex = []byte(fallbackHTML)
		return
	}
	var err error
	staticSub, err = fs.Sub(assets, ".")
	if err != nil {
		staticIndex = []byte(fallbackHTML)
		return
	}
	staticFileSrv = http.FileServer(http.FS(staticSub))
	staticIndex, err = fs.ReadFile(staticSub, "index.html")
	if err != nil {
		staticIndex = []byte(fallbackHTML)
	}
}

const fallbackHTML = `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><title>Warehouse</title></head><body><div id="root">Warehouse — 前端未构建，请运行 pnpm build</div></body></html>`

var (
	staticSub     fs.FS
	staticFileSrv http.Handler
	staticIndex   []byte
)

// staticHandler returns an http.Handler that serves the embedded frontend.
func staticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if p == "." || p == "" {
			staticFileSrv.ServeHTTP(w, r)
			return
		}

		if _, err := staticSub.Open(p); err == nil {
			staticFileSrv.ServeHTTP(w, r)
			return
		}

		// SPA fallback.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(staticIndex))
	})
}