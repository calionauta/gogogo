// The beacon is a conditional surface: mounted only when a provider is
// configured, so analytics-off deployments expose no endpoint at all.
package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/calionauta/gogogo/config"
)

func bootAnalyticsStack(t *testing.T, provider, key string) *ChiStack {
	t.Helper()
	cfg := config.Load()
	cfg.Dev = true
	cfg.SignupEnabled = true
	cfg.RBACEnabled = false
	cfg.DemoAccounts = false
	cfg.AppShape = "web"
	cfg.Turso.DBPath = filepath.Join(t.TempDir(), "analytics.db")
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.NATS.Enabled = false
	cfg.DagNats.Enabled = false
	cfg.GoAkt.Enabled = false
	cfg.Events.AnalyticsProvider = provider
	cfg.Events.AnalyticsKey = key
	stack, err := BootChi(cfg)
	if err != nil {
		t.Fatalf("BootChi: %v", err)
	}
	t.Cleanup(func() {
		stack.cancelConsumer()
		for _, stop := range stack.shutdown {
			stop()
		}
		_ = stack.sqlDB.Close()
	})
	return stack
}

func TestAnalyticsBeaconMountedOnlyWithAProvider(t *testing.T) {
	on := bootAnalyticsStack(t, "posthog", "phc_test")
	body := bytes.NewBufferString(`{"name":"nav.todo","path":"/todo"}`)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, analyticsPath, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	on.mux.ServeHTTP(rec, req)
	// No session: the handler drops the beacon quietly instead of 401ing a
	// background post, and drops it before any validation.
	if rec.Code != http.StatusNoContent {
		t.Errorf("POST %s without a session = %d, want 204", analyticsPath, rec.Code)
	}

	off := bootAnalyticsStack(t, "none", "")
	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, analyticsPath,
		bytes.NewBufferString(`{"name":"nav.todo"}`))
	rec = httptest.NewRecorder()
	off.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST %s with analytics off = %d, want 404 (no beacon surface)", analyticsPath, rec.Code)
	}
}
