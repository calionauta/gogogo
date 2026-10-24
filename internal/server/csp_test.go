// SCOPE:core — CSP nonce plumbing contract across the real HTTP stack.
package server

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var cspNonceRE = regexp.MustCompile(`'nonce-([A-Za-z0-9_-]+)'`)

// TestCSPNonceReachesDocumentAndScripts pins the two halves that must agree:
// the response header, <html data-nonce> (Datastar's evaluator) and the
// inline <script nonce>. A header without the attributes blocks the page; an
// attribute without the header is silently useless.
func TestCSPNonceReachesDocumentAndScripts(t *testing.T) {
	base, cookie := bootPages(t)
	for _, route := range []string{"/login", "/todo"} {
		t.Run(route, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+route, nil)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if route != "/login" {
				req.AddCookie(cookie)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", route, err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			m := cspNonceRE.FindStringSubmatch(resp.Header.Get("Content-Security-Policy"))
			if m == nil {
				t.Fatalf("%s: CSP carries no nonce", route)
			}
			nonce := m[1]
			if !strings.Contains(string(body), `data-nonce="`+nonce+`"`) {
				t.Errorf("%s: <html data-nonce> does not match the header", route)
			}
			if !strings.Contains(string(body), `nonce="`+nonce+`"`) {
				t.Errorf("%s: inline scripts do not carry the header nonce", route)
			}
		})
	}
}
