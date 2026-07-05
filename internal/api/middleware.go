package api

import (
	"log"
	"net/http"
	"runtime/debug"
	"strings"
)

// Recoverer catches panics in downstream handlers, logs the stack trace, and
// returns a clean 500 JSON envelope. Installed early so every route is
// protected.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				log.Printf("panic: %v\n%s", rv, debug.Stack())
				WriteError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingResponseWriter captures the status code so RequestLogger can record
// it. Body is discarded — we only need the metadata.
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (lw *loggingResponseWriter) WriteHeader(code int) {
	lw.status = code
	lw.ResponseWriter.WriteHeader(code)
}

func (lw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lw.status == 0 {
		lw.status = http.StatusOK
	}
	return lw.ResponseWriter.Write(b)
}

// RequestLogger writes one log line per request: method, path, status,
// duration. Uses log.Printf so output goes to the same stream as recoverer
// and startup logs.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lw := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(lw, r)
		if lw.status == 0 {
			lw.status = http.StatusOK
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lw.status, r.RemoteAddr)
	})
}

// CORSOptions configures the CORS middleware.
type CORSOptions struct {
	// AllowedOrigins is the explicit allow-list (lowercased, no trailing
	// slash). When empty, the middleware falls back to same-origin only.
	AllowedOrigins []string
}

// CORS implements the basic Access-Control-* protocol. Same-origin requests
// always pass; cross-origin requests pass only when the Origin header is in
// the allow-list. We do not support wildcard with credentials.
func CORS(opts CORSOptions) func(http.Handler) http.Handler {
	allow := make(map[string]bool, len(opts.AllowedOrigins))
	for _, o := range opts.AllowedOrigins {
		allow[strings.ToLower(strings.TrimRight(o, "/"))] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				lo := strings.ToLower(strings.TrimRight(origin, "/"))
				if allow[lo] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
