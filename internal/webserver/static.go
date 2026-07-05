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
//
// `assets` may be rooted either directly at the dist directory (e.g.
// os.DirFS("frontend/dist")) or one level up containing a dist subdirectory
// (e.g. an embed.FS from `//go:embed all:frontend/dist`, which is rooted at
// the package directory). InitStatic walks to find the directory that
// actually contains index.html so it works either way. If neither layout
// is found, a minimal fallback page is used.
func InitStatic(assets fs.FS) {
	if staticSub != nil {
		return // already initialized
	}
	if assets == nil {
		staticIndex = []byte(fallbackHTML)
		return
	}
	sub, err := resolveStaticRoot(assets)
	if err != nil {
		staticIndex = []byte(fallbackHTML)
		return
	}
	staticSub = sub
	staticFileSrv = http.FileServer(http.FS(staticSub))
	if data, err := fs.ReadFile(staticSub, "index.html"); err == nil {
		staticIndex = data
	} else {
		staticIndex = []byte(fallbackHTML)
	}
}

// resolveStaticRoot picks the subtree of `assets` that contains index.html.
// It first checks the root, then performs a bounded BFS into subdirectories.
// This keeps InitStatic agnostic to whether the caller passed a dist-rooted
// FS, a one-level-up FS, or a multi-level-up FS (e.g. an embed.FS produced
// by `//go:embed all:frontend/dist`, which is rooted at the package dir and
// nests the dist output under frontend/dist).
func resolveStaticRoot(assets fs.FS) (fs.FS, error) {
	if _, err := fs.Stat(assets, "index.html"); err == nil {
		return assets, nil
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
			if _, err := fs.Stat(sub, "index.html"); err == nil {
				return sub, nil
			}
			queue = append(queue, frame{fsys: sub, depth: cur.depth + 1})
		}
	}
	return nil, fs.ErrNotExist
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