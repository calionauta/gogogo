// SCOPE:core — HTTP edge middleware: security headers and the opt-in access
// log. Both wrap the whole handler (RBAC included), not the chi router: an
// authorization 403 and a router 404 must carry the same headers and appear
// in the same log line as a feature response.
package server

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/observability"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// securityHeaders sets the browser security headers on every response. The
// CSP carries a per-request nonce: inline templates (and Datastar's data-*
// evaluator, which reads data-nonce from <html>) use it instead of
// 'unsafe-inline' or 'unsafe-eval'. HSTS is production-only; browsers ignore
// it over plain HTTP, but a dev deploy should not pin localhost.
func securityHeaders(cfg *config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce, err := newCSPNonce()
		if err != nil {
			// Entropy failure is not a reason to serve a policy that would
			// block every inline script; fail open on the header, never on
			// the request.
			observability.Error(r.Context(), "server: csp nonce",
				append(observability.WithComponent("server", "core"), "error", err)...)
		} else {
			r = r.WithContext(templ.WithNonce(r.Context(), nonce))
			w.Header().Set("Content-Security-Policy", cspPolicy(nonce))
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if cfg != nil && !cfg.Dev {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// cspNonceBytes is 128 bits: enough that a nonce cannot be guessed, short
// enough to keep the header small.
const cspNonceBytes = 16

// newCSPNonce returns 128 bits of entropy, base64url without padding.
func newCSPNonce() (string, error) {
	raw := make([]byte, cspNonceBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// cspPolicy is deliberately strict on scripts and permissive only where the
// markup needs it: inline style attributes are used throughout the shell,
// and the whiteboard stores drawing previews as data:/blob: images.
func cspPolicy(nonce string) string {
	return "default-src 'self'; " +
		"script-src 'self' 'nonce-" + nonce + "'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; " +
		"font-src 'self'; media-src 'self'; worker-src 'self'; " +
		"connect-src 'self'; manifest-src 'self'; " +
		"report-uri " + cspReportPath + "; " +
		"object-src 'none'; base-uri 'none'; form-action 'self'; " +
		"frame-ancestors 'self'"
}

// sameOriginGuard is the CSRF fence for unsafe methods. SameSite=Lax on the
// session cookie already keeps it off cross-site POSTs; this refuses the
// request before it reaches a handler even if a browser or a future cookie
// change weakens that.
//
// Only requests that carry an Origin are checked: CLI clients, webhooks and
// the desktop webview send none, and a custom scheme (wails://) cannot be
// forged by a web page. Two loopback origins are treated as the same trust
// boundary so a local dev front-end and the desktop proxy keep working.
func sameOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		// A custom scheme (wails://) cannot be forged by a web page, and the
		// desktop webview may report itself as cross-site relative to the
		// localhost proxy. Honour that origin before the fetch-metadata
		// check, or the desktop's writes become 403.
		origin := r.Header.Get("Origin")
		if origin != "" {
			if ou, oerr := url.Parse(origin); oerr == nil && ou.Scheme != "" &&
				ou.Host != "" && ou.Scheme != schemeHTTP && ou.Scheme != schemeHTTPS {
				next.ServeHTTP(w, r)
				return
			}
		}
		// Fetch metadata is sent by every modern browser, including the
		// rare request that omits Origin. A cross-site unsafe write is never
		// a legitimate app call, so refuse it before the Origin comparison.
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if sameOriginHost(u, r.Host) ||
			(isLocalHost(u.Hostname()) && isLocalHost(hostnameOf(r.Host))) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

// sameOriginHost compares an Origin with the request Host, treating the
// scheme's default port as present/absent on either side. A proxy that keeps
// ":443" in Host is still the same origin as a browser's "https://host".
func sameOriginHost(origin *url.URL, reqHost string) bool {
	if strings.EqualFold(origin.Host, reqHost) {
		return true
	}
	return strings.EqualFold(
		stripDefaultPort(origin.Host, origin.Scheme),
		stripDefaultPort(reqHost, origin.Scheme),
	)
}

// stripDefaultPort drops :443 for https and :80 for http; any other port is
// preserved. An unparsable value is compared as-is.
func stripDefaultPort(hostport, scheme string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	if (scheme == schemeHTTPS && port == "443") || (scheme == schemeHTTP && port == "80") {
		return host
	}
	return net.JoinHostPort(host, port)
}

// isLocalHost reports whether host is loopback or a localhost name. Both
// sides of a loopback pair share the same trust boundary: a local dev page
// talking to a local server is not the cross-site case this guard exists for.
func isLocalHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostnameOf strips an optional port from a Host header value.
func hostnameOf(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
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
	next = sameOriginGuard(next)
	if cfg.HTTPAccessLog {
		// Outside the guard on purpose: a rejected cross-origin write is
		// exactly the request an operator wants to see in the log.
		next = accessLog(log, next)
	}
	return securityHeaders(cfg, next)
}
