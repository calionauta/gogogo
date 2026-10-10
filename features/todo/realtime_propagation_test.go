package todo_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freeTCPPort reserves an ephemeral TCP port and releases it, returning the
// number for a child process to bind.
//
// There is an unavoidable window between the close here and the child's bind,
// but it is far smaller than the fix it replaces: a hardcoded port collides
// with anything already listening (a stray dev server, another test package),
// which is a real and recurring failure. Passing PORT=0 is not an option — the
// binary's start banner prints the literal "0", not the port it actually bound.
func freeTCPPort() (int, error) {
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

// TestCrossSessionCreatePropagates is the regression guard for the exact
// bug the user hit: creating a todo in one tab must surface in another
// already-open tab. It boots the REAL production binary (the dev variant
// the user runs (unified build with NATS_ENABLED=true + DAGNATS_ENABLED=true)) and drives PocketBase's native
// /api/realtime — the path the browser actually uses. The custom-router
// test fixture (r.BuildMux) does NOT mount /api/realtime, so every prior
// "realtime" test exercised only the SSE hub, never the record-mutation
// broadcast. That blind spot is why create-vs-delete asymmetry slipped
// through: the e2e only asserted the pb_auth cookie was issued, not that a
// record change fans out to a second subscriber.
//
// DagNats is disabled here (DAGNATS_ENABLED=false) — the create event is
// delivered by PocketBase realtime regardless, and ResumeOnboarding
// early-returns when no durable run is active, so the test stays focused
// on the realtime fan-out that regressed.
func TestCrossSessionCreatePropagates(t *testing.T) {
	t.Parallel()
	base, cleanup := bootLiveServer(t)
	defer cleanup()
	ctx := context.Background()

	jarA, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jarB, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	clientA := &http.Client{Jar: jarA, Timeout: 30 * time.Second}
	clientB := &http.Client{Jar: jarB, Timeout: 30 * time.Second}
	loginUser(ctx, t, clientA, base, demoEmail, demoPassword)
	loginUser(ctx, t, clientB, base, demoEmail, demoPassword)

	// Tab B opens the realtime SSE stream (pb_auth cookie authenticates).
	// PocketBase assigns its own clientId and echoes it in PB_CONNECT —
	// that is the id the subscribe call must use (the URL clientId is
	// ignored), mirroring the browser's subscribe(msg.clientId).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/realtime?clientId=cross-session-sub", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := clientB.Do(req)
	if err != nil {
		t.Fatalf("open realtime SSE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("realtime SSE status=%d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	realClientID := pbConnectClientID(t, scanner)
	if realClientID == "" {
		t.Fatalf("never received PB_CONNECT with a clientId")
	}

	// Tab B subscribes to the `todos` topic using PB's assigned clientId.
	subBody, _ := json.Marshal(map[string]any{
		"clientId":      realClientID,
		"action":        "subscribe",
		"subscriptions": []string{"todos"},
	})
	subReq, subReqErr := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/realtime", bytes.NewReader(subBody))
	if subReqErr != nil {
		t.Fatalf("subscribe request: %v", subReqErr)
	}
	subReq.Header.Set("Content-Type", "application/json")
	if subResp, e := clientB.Do(subReq); e != nil {
		t.Fatalf("subscribe: %v", e)
	} else {
		subResp.Body.Close()
	}

	// Tab A creates a todo.
	createResp, err := doPostForm(ctx, clientA, base+"/api/todos", url.Values{titleField: {"PropagateMe"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	createResp.Body.Close()

	// Tab B must receive the `create` event carrying the new record.
	var transcript strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && scanner.Scan() {
		line := scanner.Text()
		transcript.WriteString(line + "\n")
		if strings.Contains(line, `"action":"create"`) && strings.Contains(line, "PropagateMe") {
			return
		}
	}
	t.Fatalf("cross-session create event not delivered; transcript tail:\n%s",
		tailString(transcript.String(), 800))
}

// TestCrossSessionFragmentScoped guards the app-side half of the same bug:
// after one session creates a todo, a DIFFERENT session's list fragment
// (the endpoint the realtime handler refetches on create) must return that
// todo. If listTodos' owner scoping regresses, the other tab would refetch
// and still see nothing — the create "doesn't show" even though the event
// arrived. This runs on the custom-router fixture (no realtime needed) so
// it is fast and a stable unit-level guard.
func TestCrossSessionFragmentScoped(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := context.Background()

	jarA, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jarB, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	clientA := &http.Client{Jar: jarA, Timeout: 15 * time.Second}
	clientB := &http.Client{Jar: jarB, Timeout: 15 * time.Second}
	loginUser(ctx, t, clientA, base, demoEmail, demoPassword)
	loginUser(ctx, t, clientB, base, demoEmail, demoPassword)

	createResp, err := doPostForm(ctx, clientA, base+"/api/todos", url.Values{titleField: {"SharedItem"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	createResp.Body.Close()

	fragReq, fragReqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/todos/fragment", nil)
	if fragReqErr != nil {
		t.Fatalf("fragment request: %v", fragReqErr)
	}
	fragResp, err := clientB.Do(fragReq)
	if err != nil {
		t.Fatalf("fragment GET: %v", err)
	}
	defer fragResp.Body.Close()
	body, _ := io.ReadAll(fragResp.Body)
	if !strings.Contains(string(body), "SharedItem") {
		t.Fatalf("cross-session fragment missing the other session's todo; tail:\n%s", tailString(string(body), 500))
	}
}

// TestRealtimeResyncFragmentMorphHeaders guards the client-side half of the
// same bug: the realtime handler refetches /api/todos/fragment on a record
// change and the browser morphs #todo-list in place. For that morph to target
// the right region (and NOT replace the whole document), the server must send
// datastar-selector + datastar-mode headers. If these regress, resync would
// either no-op (no selector) or blow away the page (whole-document morph).
func TestRealtimeResyncFragmentMorphHeaders(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := context.Background()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	loginUser(ctx, t, client, base, demoEmail, demoPassword)

	todoFragReq, todoFragReqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/todos/fragment", nil)
	if todoFragReqErr != nil {
		t.Fatalf("request: %v", todoFragReqErr)
	}
	resp, err := client.Do(todoFragReq)
	if err != nil {
		t.Fatalf("fragment GET: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("datastar-selector"); got != "#todo-list" {
		t.Fatalf("expected datastar-selector=#todo-list, got %q", got)
	}
	if got := resp.Header.Get("datastar-mode"); got != "outer" {
		t.Fatalf("expected datastar-mode=outer, got %q", got)
	}
}

// TestRealtimeResyncWiringRendered guards against reintroducing the exact
// crash the user hit: the page must wire the resync through a hidden @get
// button (whose click the runtime drives with a proper Datastar context),
// never a bare actions.get(url) call. A bare actions.get crashes with
// "Cannot read properties of undefined (reading 'delete')" because the get
// action expects a context (el/cleanups) only the runtime synthesizes when
// invoked via an attribute — so the other tab would never update.
func TestRealtimeResyncWiringRendered(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := context.Background()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	loginUser(ctx, t, client, base, demoEmail, demoPassword)

	// /todo is the demo app's HTML index (formerly at GET /; moved
	// when the landing-page refactor split marketing page from app
	// page). The page carries the PocketBase realtime wiring the
	// test wants to verify.
	indexReq, indexReqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/todo", nil)
	if indexReqErr != nil {
		t.Fatalf("request: %v", indexReqErr)
	}
	resp, err := client.Do(indexReq)
	if err != nil {
		t.Fatalf("GET /todo: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "pb-realtime-resync") {
		t.Fatalf("page missing pb-realtime-resync resync button")
	}
	// Templ's ResolveAttributeValue HTML-escapes the single quotes in the
	// attribute value ( ' -> &#39; ), so the rendered form is
	// @get(&#39;/api/todos/fragment&#39;). The browser unescapes it when reading
	// the attribute, so Datastar still sees @get('/api/todos/fragment').
	// Accept either rendering. The resync carries the client's current
	// ?filter= so the re-fetched list and its item count stay scoped to
	// the tab the user is on (all / active / completed).
	plain := "@get('/api/todos/fragment?filter=' + encodeURIComponent($filter || 'all'))"
	escaped := "@get(&#39;/api/todos/fragment?filter=&#39; + encodeURIComponent($filter || &#39;all&#39;))"
	if !strings.Contains(html, plain) && !strings.Contains(html, escaped) {
		if i := strings.Index(html, "pb-realtime-resync"); i >= 0 {
			t.Logf("button context: %q", html[i-60:i+220])
		}
		t.Fatalf("page missing @get resync wiring for /api/todos/fragment?filter= (checked %q and %q)", plain, escaped)
	}
	if strings.Contains(html, "actions.get('/api/todos/fragment')") {
		t.Fatalf("regression: page still uses crashing actions.get('/api/todos/fragment') instead of the @get button")
	}
}

// TestRealtimeNoOrphanIIFE guards against reintroducing the orphan
// `})();` that caused a SyntaxError in PbRealtimeRecords. The orphan
// was left over from a refactoring that removed the IIFE wrapper but
// left the closing `})();`. This made the ENTIRE module script fail
// to parse, so no PocketBase realtime EventSource was ever created
// and cross-tab sync was completely dead. The rendered HTML must
// never contain this sequence.
//
// We find the PbRealtimeRecords script block by looking for its
// distinctive string (the call to new EventSource('/api/realtime))
// and then check that the code between it and the closing </script>
// does NOT contain the orphan })(); — which appears in RealtimeStream's
// VALID IIFE but would be a syntax error in PbRealtimeRecords.
func TestRealtimeNoOrphanIIFE(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := context.Background()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	loginUser(ctx, t, client, base, demoEmail, demoPassword)

	todosReq, todosReqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/todo", nil)
	if todosReqErr != nil {
		t.Fatalf("request: %v", todosReqErr)
	}
	resp, err := client.Do(todosReq)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Locate the PbRealtimeRecords script block (it contains the
	// EventSource('/api/realtime') call unique to that component).
	marker := "EventSource('/api/realtime"
	idx := strings.Index(html, marker)
	if idx < 0 {
		t.Fatalf("PbRealtimeRecords script block not found (missing %q)", marker)
	}
	// Find the closing </script> after this marker.
	scriptEnd := strings.Index(html[idx:], "<"+"/script>")
	if scriptEnd < 0 {
		t.Fatalf("could not find </script> after PbRealtimeRecords block")
	}
	pbBlock := html[idx : idx+scriptEnd]

	// The orphan })(); would appear INSIDE the PbRealtimeRecords script
	// after the marker. Check that it's NOT there. (RealtimeStream has
	// its own valid IIFE elsewhere on the page, which we ignore.)
	if strings.Contains(pbBlock, "})()"+";") {
		t.Fatalf("PbRealtimeRecords script block contains orphan })(); — SyntaxError kills cross-tab realtime")
	}
	// Also verify the page carries the shared resync mechanism: the
	// data-pb-resync marker the script uses to find its hidden @get
	// button (the resync mechanism is the fix for the crash). Checked
	// page-wide — the marker lives on the button tag, which renders
	// before this script block.
	if !strings.Contains(html, "data-pb-resync") {
		t.Fatalf("page missing data-pb-resync resync mechanism")
	}
}

// liveServerBin is the production binary built once per test binary by
// TestMain. bootLiveServer used to shell out to `go build ./cmd/web` on every
// call, which cost ~8.5s of the package's runtime for a binary SHARED by the
// tests. Building once makes that a cost of the test binary, not of a test.
var liveServerBin string

// TestMain builds the production binary once for the tests that need to run the
// real server. A build failure is fatal for the whole package, which is correct:
// those tests cannot run without it, and `go build` failing is a broken package.
func TestMain(m *testing.M) {
	bin, dir, err := buildLiveServerBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build live server binary: %v\n", err)
		os.Exit(1)
	}
	liveServerBin = bin
	code := m.Run()
	// Remove the whole temp DIRECTORY, not just the binary inside it. Removing
	// only the binary left an empty gogogo-live-* dir behind on every run — 195
	// had accumulated in /tmp on this host before it was noticed. On a long-lived
	// or shared machine that is a slow inode leak, and it is the kind of thing
	// that only surfaces when a disk fills.
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// buildLiveServerBinary builds the production binary into a fresh temp dir and
// returns both the binary path and the DIRECTORY, so the caller can remove the
// directory and not just the file.
func buildLiveServerBinary() (binPath, dir string, err error) {
	dir, err = os.MkdirTemp("", "gogogo-live-")
	if err != nil {
		return "", "", err
	}
	bin := filepath.Join(dir, "gogogo_live")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", bin, "github.com/calionauta/gogogo/cmd/web")
	build.Stderr = os.Stderr
	if out, buildErr := build.Output(); buildErr != nil {
		return "", dir, fmt.Errorf("go build: %w\n%s", buildErr, out)
	}
	return bin, dir, nil
}

// bootLiveServer runs the prebuilt production binary (dev variant) as a
// subprocess and waits for it to accept /health. Returns the base URL and a
// cleanup that kills the process. This is the only faithful way to exercise
// PocketBase realtime (/api/realtime), which the unit fixture does not
// mount.
func bootLiveServer(t *testing.T) (string, func()) {
	t.Helper()
	if liveServerBin == "" {
		t.Fatal("live server binary was not built (TestMain did not run)")
	}
	bin := liveServerBin

	tmpDir := t.TempDir()
	// Ephemeral port. This was hardcoded to 8291, which made the test fail
	// whenever anything else on the machine held it — the same fixed-port
	// flake class already fixed for the DagNats tests. Binding 0 and reading
	// the real port back from the banner removes the collision entirely.
	port, portErr := freeTCPPort()
	if portErr != nil {
		t.Fatalf("reserve port: %v", portErr)
	}
	env := []string{
		"ENVIRONMENT=development",
		"HOST=127.0.0.1",
		"PORT=" + strconv.Itoa(port),
		"DATA_DIR=" + tmpDir,
		"DATABASE_PATH=" + filepath.Join(tmpDir, "app.db"),
		"ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"DAGNATS_ENABLED=false",
		"NATS_ENABLED=false",
		// A throwaway DATA_DIR has no superuser, so PocketBase would run its
		// first-run installer and call LaunchURL — popping a browser tab on
		// the developer's machine. router.Init suppresses that for a non-TTY
		// child, and this flag makes it unconditional: the child's TTY status
		// is inherited from whatever ran `go test`, so relying on detection
		// alone would let `go test` from a real terminal through.
		"GOGOGO_NO_BROWSER=1",
	}
	proc := exec.CommandContext(context.Background(), bin, "serve", "--http", "127.0.0.1:"+strconv.Itoa(port))
	// Build the env from scratch rather than appending to os.Environ(): an
	// inherited GOGOGO_NO_BROWSER=0 (or an ENVIRONMENT/PORT override) would
	// silently re-enable the browser launch this test exists to avoid.
	proc.Env = env
	proc.Stderr = os.Stderr
	if err := proc.Start(); err != nil {
		t.Fatalf("start live server: %v", err)
	}

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		healthReq, healthReqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/health", nil)
		if healthReqErr != nil {
			continue
		}
		resp, e := http.DefaultClient.Do(healthReq)
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base, func() { _ = proc.Process.Kill(); _, _ = proc.Process.Wait() }
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = proc.Process.Kill()
	t.Fatalf("live server did not become healthy on %s within 60s", base)
	return "", nil
}

// pbConnectClientID reads the SSE frame stream until the PB_CONNECT event and
// returns the clientId PocketBase assigned.
//
// Extracted from the test body to flatten four levels of nesting into guard
// clauses: the reader had `for` -> `if event` -> `if Scan` -> `if prefix` ->
// `if unmarshal`, which nestif flagged at complexity 6. Each step below returns
// as soon as it knows the answer.
func pbConnectClientID(t *testing.T, scanner *bufio.Scanner) string {
	t.Helper()

	for !scanner.Scan() {
		return "" // stream ended without the event
	}
	// Rewind-free scan: the caller's stream position is already past the
	// headers, so walk forward until the PB_CONNECT frame.
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "event:PB_CONNECT" {
			continue
		}
		if !scanner.Scan() {
			return ""
		}
		data := scanner.Text()
		if !strings.HasPrefix(data, "data:") {
			return ""
		}
		var m struct {
			ClientID string `json:"clientId"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data:")), &m); err != nil {
			return ""
		}
		return m.ClientID
	}
	return ""
}
