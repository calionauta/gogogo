package installer

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecipeInvokeRefusesUnknownIDBeforeDialing proves the fail-fast
// half: an extension the checkout does not know never reaches the
// network.
func TestRecipeInvokeRefusesUnknownIDBeforeDialing(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := runRecipeInvoke(context.Background(), []string{
		"--dir", filepath.Join("..", ".."),
		"--server", srv.URL, "--session", "tok",
		"ghost", "create",
	}, &buf); err == nil {
		t.Fatal("unknown recipe must refuse before dialing")
	}
	if hits != 0 {
		t.Fatalf("unknown-id invoke dialed %d times", hits)
	}
}

// TestRecipeInvokeForwardsAndParses proves the happy path: POST to
// /api/ext/invoke with the session cookie, JSON body, and a 2xx answer
// echoed back to stdout.
func TestRecipeInvokeForwardsAndParses(t *testing.T) {
	var gotCookie string
	var gotPath string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if c, err := r.Cookie("gogogo_auth"); err == nil {
			gotCookie = c.Value
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("created ok"))
	}))
	defer srv.Close()

	// "todo" is a capability id known to the checkout, so the run reaches
	// the server (its verb check is server-side for built-ins).
	var buf bytes.Buffer
	if err := runRecipeInvoke(context.Background(), []string{
		"--dir", filepath.Join("..", ".."),
		"--server", srv.URL, "--session", "tok-9",
		"--param", "title=Ship it",
		"--format", "json",
		"todo", "create",
	}, &buf); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if gotPath != "/api/ext/invoke" {
		t.Errorf("path = %s, want /api/ext/invoke", gotPath)
	}
	if gotCookie != "tok-9" {
		t.Errorf("cookie = %q, want tok-9", gotCookie)
	}
	if !strings.Contains(gotBody, `"title":"Ship it"`) {
		t.Errorf("params missing in body: %s", gotBody)
	}
	if !strings.Contains(buf.String(), `"body":"created ok"`) {
		t.Errorf("json stdout missing body: %q", buf.String())
	}
}

// TestRecipeInvokeRefusesNonLoopback proves the SSRF guard: a public
// host is refused before any dial.
func TestRecipeInvokeRefusesNonLoopback(t *testing.T) {
	var buf bytes.Buffer
	if err := runRecipeInvoke(context.Background(), []string{
		"--dir", filepath.Join("..", ".."),
		"--server", "http://example.com:8080", "--session", "tok",
		"todo", "create",
	}, &buf); err == nil {
		t.Fatal("non-loopback server must refuse")
	}
}

// TestRecipeInvokeRefusesReceiptVerbNotDeclared proves the local verb
// check: a receipt recipe that declares no "explode" verb refuses before
// dialing.
func TestRecipeInvokeRefusesReceiptVerbNotDeclared(t *testing.T) {
	dir := t.TempDir()
	yml := `id: probe
upstream: {repo: "github.com/x/y", ref: "v1"}
files: {}
declaration:
  tables: ["ext_probe_cards"]
  routes: [{method: GET, pattern: /api/probe}, {method: POST, pattern: /api/probe}]
  actions: [{verb: create, route: {method: POST, pattern: /api/probe}}]
`
	if err := os.MkdirAll(filepath.Join(dir, ".recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".recipes", "probe.yml"), []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := runRecipeInvoke(context.Background(), []string{
		"--dir", dir,
		"--server", srv.URL, "--session", "tok",
		"probe", "explode",
	}, &buf); err == nil {
		t.Fatal("undeclared receipt verb must refuse before dialing")
	}
	if hits != 0 {
		t.Fatalf("undeclared-verb invoke dialed %d times", hits)
	}
}
