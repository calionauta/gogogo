// SCOPE:core — CSP report sink contract.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSPReportSink(t *testing.T) {
	body := `{"csp-report":{"document-uri":"https://app.example/todo",` +
		`"violated-directive":"script-src","blocked-uri":"https://evil.example/x.js"}}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, cspReportPath,
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/csp-report")
	rec := httptest.NewRecorder()
	cspReportHandler(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("report = %d, want 204", rec.Code)
	}

	get := httptest.NewRecorder()
	cspReportHandler(get, httptest.NewRequestWithContext(context.Background(), http.MethodGet, cspReportPath, nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d, want 405", get.Code)
	}

	big := httptest.NewRequestWithContext(context.Background(), http.MethodPost, cspReportPath,
		strings.NewReader(strings.Repeat("x", cspReportMax+1)))
	tooBig := httptest.NewRecorder()
	cspReportHandler(tooBig, big)
	if tooBig.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d, want 413", tooBig.Code)
	}
}

func TestCSPReportDropsURLSecrets(t *testing.T) {
	got := cleanReportURL("https://app.example/reset?token=SUPER-SECRET#frag")
	if strings.Contains(got, "SUPER-SECRET") || strings.Contains(got, "?") || strings.Contains(got, "#") {
		t.Fatalf("report URL leaked query/fragment: %q", got)
	}
	if got != "https://app.example/reset" {
		t.Fatalf("cleanReportURL = %q", got)
	}
}
