package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// freeTCPPortForTest reserves an ephemeral TCP port and releases it, returning
// the number for the child binary to bind. There is a small window between the
// close here and the child's bind, but that is far narrower than the collision
// a hardcoded port causes whenever anything else already holds it.
func freeTCPPortForTest() (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		_ = l.Close()
		return 0, fmt.Errorf("listener addr %T is not *net.TCPAddr", l.Addr())
	}
	port := addr.Port
	if closeErr := l.Close(); closeErr != nil {
		return 0, closeErr
	}
	return port, nil
}

// Seeded demo credentials (db/seed.go). /todo is auth-gated: without a session
// it 303-redirects to /login, so any assertion about its HTML must run against
// a logged-in client.
const (
	demoEmail    = "demo@demo.app"
	demoPassword = "demo1234456"
)

// TestSmoke_BootedBinaryServesAndSyncs boots the REAL compiled binary (not a
// httptest server) and drives the full realtime path end-to-end:
//
//  1. /health returns 200 (the container healthcheck contract)
//  2. GET /todo (authenticated) renders the page with the centralized
//     #sse-opener + datastar-ready script, so the browser will actually open
//     the SSE stream
//  3. opening GET /api/todos/stream returns 200 + text/event-stream, proving
//     the SSE transport is wired and a browser would connect (the exact
//     regression we shipped: "SSE never auto-connected")
//
// /todo is auth-gated (303 to /login without a session), so the assertions run
// against a logged-in cookie-jar client using the seeded demo user.
//
// Running against the actual artifact catches build/flag/wiring breakage that
// the in-process httptest fixtures cannot (they bypass the binary's serve
// flags and the rendered HTML).
//
// The mutation-via-broadcast path (create in tab A → arrives in tab B) is
// covered by the in-process broadcast_probe_test.go; this test proves the
// binary itself boots and exposes the realtime transport.
//
// It is gated behind RUN_SMOKE=1 because it spawns a real server process that
// binds a TCP port. CI sets RUN_SMOKE=1; local `go test ./...` skips it so
// developers don't need a free port (the rendered-HTML assertion is also
// covered by TestLayoutRendersSSEOpener, which needs no server).
func TestSmoke_BootedBinaryServesAndSyncs(t *testing.T) {
	if os.Getenv("RUN_SMOKE") != "1" {
		t.Skip("set RUN_SMOKE=1 to boot the real binary and run the end-to-end smoke test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH; skipping binary smoke test")
	}

	dir := t.TempDir()
	binPath := filepath.Join(dir, "gogogo-smoke")
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binPath, ".")
	build.Stderr = &bytes.Buffer{}
	if err := build.Run(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, build.Stderr)
	}

	// Ephemeral port: a hardcoded one collides with anything else on the
	// machine (a stray dev server, a parallel test package).
	port, portErr := freeTCPPortForTest()
	if portErr != nil {
		t.Fatalf("reserve port: %v", portErr)
	}
	dataDir := filepath.Join(dir, "data")
	// The queue boots before PocketBase creates --dir, and it opens
	// DATABASE_PATH eagerly; a missing parent dir fails with "unable to open
	// database file: lstat <dir>: no such file or directory". scripts/smoke.mjs
	// mkdirs its runtime dir for the same reason — mirror it here instead of
	// depending on a data/ dir happening to exist in the process cwd.
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "serve", "--http", fmt.Sprintf("127.0.0.1:%d", port), "--dir", dataDir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// DATA_DIR/DATABASE_PATH must be absolute and inside the temp dir. The
	// binary's defaults are relative (data/data.db, data/), so without these
	// the goqite queue fails with "unable to open database file: lstat data:
	// no such file or directory" whenever the process cwd has no data/ dir —
	// an environment-dependent pass/fail. Pin both to the same temp tree
	// scripts/smoke.mjs uses.
	cmd.Env = append(os.Environ(),
		"DATA_DIR="+dataDir,
		"DATABASE_PATH="+filepath.Join(dataDir, "app.db"),
		// Disable the background subsystems this test does not exercise. The
		// DagNats engine binds fixed ports (HTTP :8090, NATS :4222); leaving
		// it on makes the test fail whenever anything else on the machine —
		// another test package, a stray dev server — already holds them.
		"NATS_ENABLED=false",
		"DAGNATS_ENABLED=false",
		// Not belt-and-suspenders: the child's TTY status is inherited from
		// whatever ran `go test`, so `make test` from a real terminal would
		// otherwise let router.Init's interactive() detection pass and open a
		// browser tab. An explicit flag is what makes this deterministic
		// regardless of how the suite was invoked. Note it must come AFTER
		// os.Environ() to win over any inherited value.
		"GOGOGO_NO_BROWSER=1",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start binary: %v", err)
	}
	defer func() {
		if cmd.Process == nil {
			return
		}
		if kerr := cmd.Process.Kill(); kerr != nil {
			t.Logf("smoke server kill: %v", kerr)
		}
		if _, werr := cmd.Process.Wait(); werr != nil {
			t.Logf("smoke server wait: %v", werr)
		}
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	if err := waitForHealthy(ctx, base); err != nil {
		t.Fatalf("binary never became healthy: %v", err)
	}

	// /todo requires a session; use a cookie jar so the login Set-Cookie is
	// replayed on subsequent requests. The demo user is seeded on first boot.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	loginUser(ctx, t, client, base)

	assertPageWiresSSE(ctx, t, client, base)
	assertSSETransportOpens(ctx, t, client, base)
}

// loginUser signs in the seeded demo user so auth-gated routes render the app
// page instead of 303-redirecting to /login.
func loginUser(ctx context.Context, t *testing.T, client *http.Client, base string) {
	t.Helper()
	form := url.Values{"email": {demoEmail}, "password": {demoPassword}, "next": {"/todo"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 200 or 303", resp.StatusCode)
	}
}

// waitForHealthy polls /health until it returns 200 or ctx expires.
func waitForHealthy(ctx context.Context, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return err
	}
	return waitFor(ctx, func() bool {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

// assertPageWiresSSE checks the rendered /todo page opens the realtime
// stream. (Pre-refactor: indexed at GET /; moved to /todo when the
// landing-page refactor split marketing from app.)
func assertPageWiresSSE(ctx context.Context, t *testing.T, client *http.Client, base string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/todo", nil)
	if err != nil {
		t.Fatalf("build /todo request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /todo: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /todo status = %d, want 200 (session not established?)", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /todo body: %v", err)
	}
	html := string(body)
	for _, want := range []string{`id="sse-opener"`, `datastar-ready`, `/api/todos/stream`} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered /todo missing %q — the SSE opener wiring is broken; browsers would not connect", want)
		}
	}
	if strings.Contains(html, "data-on:load=") {
		msg := "rendered / still uses a data-on:load= attribute; the SSE stream " +
			"would never open because Datastar v1 does not fire on:load on <div>/<body>"
		t.Errorf("%s", msg)
	}
}

// assertSSETransportOpens checks the SSE endpoint returns the right status
// and content type for a connected client.
func assertSSETransportOpens(ctx context.Context, t *testing.T, client *http.Client, base string) {
	t.Helper()
	clientID := "smoke-" + time.Now().Format("150405")
	url := base + "/api/todos/stream?clientID=" + clientID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	// Authed client keeps the stream scoped to the demo user's todos. The
	// endpoint deliberately opens for anonymous clients too (public demo
	// events), but with an empty todo scope — so use the session to exercise
	// the real path.
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE stream status=%d, want 200 (stream did not open)", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("SSE stream Content-Type=%q, want text/event-stream", resp.Header.Get("Content-Type"))
	}
}

// waitFor polls cond every 200ms until it returns true or ctx expires.
func waitFor(ctx context.Context, cond func() bool) error {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if cond() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
