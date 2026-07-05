package webserver

import (
	"net/http"
	"net/url"
	"strings"
)

// corsMiddleware returns a CORS middleware that checks the request Origin
// against the supplied allow-list. When allowedOrigins is empty, the
// middleware falls back to allowing http://localhost:* and
// http://127.0.0.1:* (any port).
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	if len(allowedOrigins) > 0 {
		allow := make(map[string]bool, len(allowedOrigins))
		for _, o := range allowedOrigins {
			allow[strings.ToLower(strings.TrimRight(o, "/"))] = true
		}
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origin := r.Header.Get("Origin")
				if origin != "" {
					lo := strings.ToLower(strings.TrimRight(origin, "/"))
					if allow[lo] {
						setCORSHeaders(w, origin)
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

	// Default: allow localhost and 127.0.0.1 on any port.
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if isLocalhostOrigin(origin) {
					setCORSHeaders(w, origin)
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

// isLocalhostOrigin returns true when origin parses as an http:// URL with
// hostname "localhost" or "127.0.0.1". Port is ignored, so both
// http://localhost:17880 and http://localhost:9999 are accepted.
func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1")
}

// setCORSHeaders writes the standard CORS response headers.
func setCORSHeaders(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}
