// SCOPE:core — HTTP edge middleware: security headers and the opt-in access
// log. Both wrap the whole handler (RBAC included), not the chi router: an
// authorization 403 and a router 404 must carry the same headers and appear
// in the same log line as a feature response.
package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/calionauta/gogogo/config"
)

// securityHeaders sets the three headers a browser honours without needing a
// nonce. No CSP: the templates keep inline JS next to their markup, so a
// policy would need nonce plumbing before it can be correct.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

// statusWriter is gone on purpose: a plain struct that embeds
// http.ResponseWriter hides Flush, and a hidden Flush turns every SSE handler
// into a 500. The access log uses chi's wrapper, which forwards Flusher,
// Hijacker and Pusher when the underlying writer has them.
//
// accessLog emits one structured line per request after the handler returns.
// It logs streams at close, with their real duration.
func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		log.Info("http: request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"stream", strings.HasPrefix(ww.Header().Get("Content-Type"), "text/event-stream"),
		)
	})
}

// edgeHandler composes the outermost wrappers. Order matters: the headers
// cover every response (including the log's own), the access log covers RBAC
// and the router's 404, and nothing here buffers a response.
func edgeHandler(cfg *config.Config, log *slog.Logger, next http.Handler) http.Handler {
	if cfg.HTTPAccessLog {
		next = accessLog(log, next)
	}
	return securityHeaders(next)
}
