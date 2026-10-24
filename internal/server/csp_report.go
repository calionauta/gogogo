// SCOPE:core — CSP violation report sink.
//
// report-uri is still the most widely honoured CSP reporting mechanism.
// Reports are untrusted input: cap the body, never echo it, log only a few
// bounded fields, and answer 204 so a browser does not retry.
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/calionauta/gogogo/internal/observability"
)

const (
	cspReportPath = "/api/security/csp-report"
	cspReportMax  = 64 << 10
)

//nolint:tagliatelle // CSP report keys are hyphenated on the wire.
type cspReport struct {
	Report struct {
		DocumentURI       string `json:"document-uri"`
		ViolatedDirective string `json:"violated-directive"`
		BlockedURI        string `json:"blocked-uri"`
		SourceFile        string `json:"source-file"`
	} `json:"csp-report"`
}

func cspReportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cspReportMax))
	if err != nil {
		http.Error(w, "report too large", http.StatusRequestEntityTooLarge)
		return
	}
	var report cspReport
	if err := json.Unmarshal(body, &report); err != nil {
		// Browsers sometimes send a bare array; we do not need its shape to
		// record that a violation happened.
		observability.Warn(r.Context(), "csp: unparseable report",
			append(observability.WithComponent("csp", "core"), "bytes", len(body))...)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	observability.Warn(r.Context(), "csp: violation",
		append(observability.WithComponent("csp", "core"),
			"document", cleanReportURL(report.Report.DocumentURI),
			"directive", truncateCSP(sanitizeCSP(report.Report.ViolatedDirective)),
			"blocked", truncateCSP(sanitizeCSP(report.Report.BlockedURI)),
			"source", cleanReportURL(report.Report.SourceFile))...)
	w.WriteHeader(http.StatusNoContent)
}

// cleanReportURL drops the query and fragment before logging. A CSP report on
// a page that carries a one-time token in its URL would otherwise copy that
// token into the log — the report is about the blocked resource, not the
// secret that happened to be in the address bar.
func cleanReportURL(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return truncateCSP(sanitizeCSP(raw))
}

// sanitizeCSP strips control characters and line breaks so an attacker cannot
// forge a second log line; truncateCSP bounds the field.
func sanitizeCSP(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, s)
}

func truncateCSP(s string) string {
	const max = 512
	if len(s) > max {
		return s[:max]
	}
	return s
}
