// SCOPE:layer=feature,removal=plugin — tests for the DagNats proxy's response
// body rewriting (absolute-path prefixing + Content-Length consistency).
package router_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/router"
)

// TestRewriteDagNatsPaths_ContentLengthMatchesBody pins the invariant that
// backs the header write: Content-Length must equal the byte length of the
// REWRITTEN body, not the original. A mismatch makes the browser truncate or
// hang the response.
//
// This is also the equivalence proof for replacing the hand-rolled itoa with
// strconv.Itoa: the header value must be the decimal of len(rewritten), for
// both a value with a single digit and one well into multi-digit territory.
func TestRewriteDagNatsPaths_ContentLengthMatchesBody(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"short", `<a href="/console/x">y</a>`},
		{"long", strings.Repeat(`<script src="/console/app.js"></script>`, 400)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{"Content-Type": []string{"text/html"}},
				Body:   io.NopCloser(strings.NewReader(tc.body)),
			}
			if err := router.RewriteDagNatsPathsForTest(resp); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read rewritten body: %v", err)
			}

			// The rewrite must have happened (absolute path prefixed).
			if !strings.Contains(string(got), "/dagnats/") {
				t.Fatalf("body was not rewritten: %q", string(got)[:min(len(got), 120)])
			}

			wantLen := strconv.Itoa(len(got))
			if h := resp.Header.Get("Content-Length"); h != wantLen {
				t.Fatalf("Content-Length header = %q, want %q (body len %d)", h, wantLen, len(got))
			}
			if resp.ContentLength != int64(len(got)) {
				t.Fatalf("resp.ContentLength = %d, want %d", resp.ContentLength, len(got))
			}
		})
	}
}

// TestRewriteDagNatsPaths_NonRewritablePassthrough pins that non-HTML/JS/CSS
// content types are returned untouched (no body read, no header mutation).
func TestRewriteDagNatsPaths_NonRewritablePassthrough(t *testing.T) {
	const body = `{"path":"/console/thing"}`
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
	if err := router.RewriteDagNatsPathsForTest(resp); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != body {
		t.Fatalf("JSON body was mutated: %q", string(got))
	}
	if h := resp.Header.Get("Content-Length"); h != "" {
		t.Fatalf("Content-Length set for passthrough: %q", h)
	}
}

// TestRewriteDagNatsPaths_LocationPrefixed proves the redirect header is also
// rewritten, so the dashboard's own absolute redirects stay under /dagnats/.
func TestRewriteDagNatsPaths_LocationPrefixed(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"Location":     []string{"/console/login"},
		},
		Body: io.NopCloser(strings.NewReader("")),
	}
	if err := router.RewriteDagNatsPathsForTest(resp); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if got := resp.Header.Get("Location"); got != "/dagnats/console/login" {
		t.Fatalf("Location = %q, want /dagnats/console/login", got)
	}
}
