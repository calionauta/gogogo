// SCOPE:core — HTTP edge middleware tests: security headers + access log.
package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestSecurityHeadersOnEveryResponse pins the headers a browser needs on
// every surface, including the ones the router answers without a feature:
// a 404 and a 405 must not be the only responses missing them.
func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	methodHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	cases := []struct {
		name string
		h    http.Handler
		want int
	}{
		{"ok", okHandler, http.StatusOK},
		{"not found", http.NotFoundHandler(), http.StatusNotFound},
		{"method", methodHandler, http.StatusMethodNotAllowed},
	}
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"X-Frame-Options":        "SAMEORIGIN",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			securityHeaders(tc.h).ServeHTTP(rec, httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, "/anything", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			for name, value := range want {
				if got := rec.Header().Get(name); got != value {
					t.Errorf("%s = %q, want %q", name, got, value)
				}
			}
		})
	}
}

// flushSpy is a ResponseWriter that implements http.Flusher. The access log
// must forward Flush: every SSE handler in the tree type-asserts Flusher, and
// a wrapper that hides it turns a live stream into a 500.
type flushSpy struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flushSpy) Flush() {
	f.flushes++
	f.ResponseRecorder.Flush()
}

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func TestAccessLogPreservesFlusher(t *testing.T) {
	var buf bytes.Buffer
	h := accessLog(newTestLogger(&buf), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("access log stripped http.Flusher")
			return
		}
		flusher.Flush()
		w.WriteHeader(http.StatusOK)
	}))
	spy := &flushSpy{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(spy, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/stream", nil))
	if spy.flushes != 1 {
		t.Fatalf("flushes = %d, want 1 (wrapper must forward Flush)", spy.flushes)
	}
	out := buf.String()
	for _, want := range []string{"method=GET", "path=/stream", "status=200"} {
		if !strings.Contains(out, want) {
			t.Errorf("log line missing %q: %s", want, out)
		}
	}
}

// TestAccessLogRecordsStatus pins the status the edge decided, including the
// 401/403 the RBAC wrapper answers before any feature runs.
func TestAccessLogRecordsStatus(t *testing.T) {
	var buf bytes.Buffer
	h := accessLog(newTestLogger(&buf), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/private", nil))
	out := buf.String()
	for _, want := range []string{"path=/private", "status=401"} {
		if !strings.Contains(out, want) {
			t.Errorf("log line missing %q: %s", want, out)
		}
	}
}

// TestEdgeKeepsStreamFlushable drives the real chain order used at boot
// (security headers outside the access log) and proves a streaming handler
// still reaches http.Flusher through both wrappers.
func TestEdgeKeepsStreamFlushable(t *testing.T) {
	called := false
	stream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("Flusher lost through the edge chain")
			return
		}
		called = true
	})
	var buf bytes.Buffer
	chain := securityHeaders(accessLog(newTestLogger(&buf), stream))
	spy := &flushSpy{ResponseRecorder: httptest.NewRecorder()}
	chain.ServeHTTP(spy, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/events", nil))
	if !called {
		t.Fatal("stream handler did not run")
	}
}

// TestEdgeHandlerAccessLogOptIn pins the default: a template must not print a
// line per request unless the operator asked for it. The headers stay on in
// both modes.
func TestEdgeHandlerAccessLogOptIn(t *testing.T) {
	var buf bytes.Buffer
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := &config.Config{}
	rec := httptest.NewRecorder()
	edgeHandler(cfg, newTestLogger(&buf), next).ServeHTTP(rec, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/quiet", nil))
	if buf.Len() != 0 {
		t.Errorf("access log is not opt-in: %s", buf.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("security headers missing when access log is off")
	}

	cfg.HTTPAccessLog = true
	rec = httptest.NewRecorder()
	edgeHandler(cfg, newTestLogger(&buf), next).ServeHTTP(rec, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/quiet", nil))
	if !strings.Contains(buf.String(), "status=204") {
		t.Errorf("access log did not record the request: %s", buf.String())
	}
}

// TestSameOriginGuard pins the CSRF fence: an unsafe cross-origin browser
// request is refused only when it carries an Origin, because CLI clients,
// webhooks and the desktop webview send none (or a custom scheme).
func TestSameOriginGuard(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})
	cases := []struct {
		name    string
		method  string
		host    string
		origin  string
		want    int
		reached bool
	}{
		{"same origin", http.MethodPost, "app.example.com", "https://app.example.com", http.StatusNoContent, true},
		{"cross origin", http.MethodPost, "app.example.com", "https://evil.example", http.StatusForbidden, false},
		{"no origin", http.MethodPost, "app.example.com", "", http.StatusNoContent, true},
		{"opaque origin", http.MethodPost, "app.example.com", "null", http.StatusForbidden, false},
		{"custom scheme", http.MethodPost, "localhost:34115", "wails://wails", http.StatusNoContent, true},
		{"local to local", http.MethodPost, "127.0.0.1:8080", "http://localhost:3001", http.StatusNoContent, true},
		{"read is not checked", http.MethodGet, "app.example.com", "https://evil.example", http.StatusNoContent, true},
		{"unparsable origin", http.MethodPost, "app.example.com", "://bad", http.StatusForbidden, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequestWithContext(context.Background(), tc.method, "/api/todos", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			sameOriginGuard(next).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if reached != tc.reached {
				t.Errorf("handler reached = %v, want %v", reached, tc.reached)
			}
		})
	}
}
