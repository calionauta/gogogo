package todo_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// sseBufferSize is the read buffer for the SSE stream pump. Matches the
// goqite channel buffer so each Read pulls at most one full event.
const sseBufferSize = 4096

// clientIDSuffixFormat is a stable per-second suffix so the same test
// run yields stable clientIDs (useful when debugging SSE traffic dumps).
const clientIDSuffixFormat = "150405.000"

// TestIntegration_CreateEnqueuesNotification opens an SSE stream, creates
// a todo via HTTP, and asserts the "todo_created" notification arrives
// on the stream within a reasonable timeout. Exercises the full path:
//
//	HTTP POST → handler → goqite Enqueue → Hub Broadcast → SSE stream

// TestIntegration_CreateEmitsToast verifies the asynchronous toast
// emitted by the worker after handleCreate enqueues a "todo_created"
// job. The HTTP response itself only patches the todo list; the toast
// arrives via the SSE stream once the worker picks up the job. This
// exercises the full HTTP → queue → worker → SSE pipeline that the
// SSE-aware retry path is designed for.

// TestIntegration_CreateRendersInList verifies the create path is
// synchronous: the HTTP response patches the todo list with the new
// todo immediately (no queue round-trip). Realtime fan-out to other
// clients is handled by the broadcaster separately.
func TestIntegration_CreateRendersInList(t *testing.T) {
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := newTestCtx(t)

	client := loginClient(ctx, t, base)
	resp, err := doPostForm(ctx, client, base+"/api/todos", url.Values{titleField: {buyMilk}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, buyMilk) {
		t.Fatalf("created todo not present in list patch: %s", tailString(body, 400))
	}
}

// parseSSEData extracts the `data:` payloads from a raw SSE transcript.
// Datastar emits `event: datastar\ndata: <payload>\n\n` blocks; we return
// the payload strings so callers can scan them for the lastRetry signal.
func parseSSEData(transcript string) []string {
	var out []string
	for block := range strings.SplitSeq(transcript, "\n\n") {
		for line := range strings.SplitSeq(block, "\n") {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "data:"); ok {
				out = append(out, strings.TrimSpace(after))
			}
		}
	}
	return out
}

// extractJSONString reads a JSON string literal starting at s. The
// caller passes s positioned just BEFORE the opening quote of the value
// (i.e. s begins with `":"<json>"`). It returns the unescaped
// contents and whether parsing succeeded. Handles escaped quotes so the
// embedded retry JSON (`"{\"attempt\":1}"`) is decoded correctly.
func extractJSONString(s string) (string, bool) {
	i := strings.Index(s, "\"")
	if i < 0 {
		return "", false
	}
	s = s[i+1:]
	var sb strings.Builder
	for j := 0; j < len(s); j++ {
		c := s[j]
		if c == '\\' && j+1 < len(s) {
			sb.WriteByte(s[j+1])
			j++
			continue
		}
		if c == '"' {
			return sb.String(), true
		}
		sb.WriteByte(c)
	}
	return "", false
}

// openSSE opens the SSE stream with a fresh context derived from the
// openSSEWithCtx opens the SSE stream under the provided context. Used
// when the caller needs to share the context across multiple calls.
func openSSEWithCtx(ctx context.Context, t *testing.T, base, clientID string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/todos/stream?clientID="+clientID, nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("SSE status=%d", resp.StatusCode)
	}
	return resp
}

// openSSEWithClient is the authenticated variant of openSSEWithCtx: it
// issues the SSE GET through a caller-supplied *http.Client that already
// holds the gogogo_auth cookie (see loginUser). Use it when the test must
// exercise the real authenticated SSE path (handleSSEStreamWithAuth →
// LoadAppAuth) rather than the unscoped DefaultClient stream.
func openSSEWithClient(ctx context.Context, t *testing.T, client *http.Client, base, clientID string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/todos/stream?clientID="+clientID, nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		t.Fatalf("SSE status=%d", resp.StatusCode)
	}
	return resp
}

// pumpSSEUntil reads the SSE stream until the predicate returns true
// or the timeout expires. The accumulated bytes are returned so
// callers can run multiple substring assertions on the full transcript.
func pumpSSEUntil(t *testing.T, stream *http.Response, timeout time.Duration, stop func(string) bool) string {
	t.Helper()
	buf := make([]byte, sseBufferSize)
	var full strings.Builder
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := stream.Body.Read(buf)
		if n > 0 {
			full.WriteString(string(buf[:n]))
			if stop(full.String()) {
				return full.String()
			}
		}
		if err != nil {
			break
		}
	}
	return full.String()
}

// tailString returns the last n bytes of s, or all of s if shorter.
// Used by Logf calls so test output doesn't drown in stream dumps.
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// loginUser POSTs the credentials to /login so the supplied *http.Client
// holds both the gogogo_auth and pb_auth cookies (see features/auth/auth.go).
// Tests that need an authenticated SSE stream or authed mutations call this
// first.
func loginUser(ctx context.Context, t *testing.T, client *http.Client, base, email, password string) {
	t.Helper()
	resp, err := doPostForm(ctx, client, base+"/login", url.Values{"email": {email}, "password": {password}})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// doPostForm issues an authenticated form POST (used for /api/todos and other
// authed routes) and returns the raw response so callers can inspect the
// status code.
func doPostForm(ctx context.Context, client *http.Client, urlStr string, values url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return client.Do(req)
}
