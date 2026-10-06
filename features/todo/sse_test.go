package todo_test

import (
	"context"
	"io"
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

// sseAbsenceWindow is how long a negative assertion drains the SSE stream
// before concluding the event never arrives. One constant instead of a
// literal per call site so the cost of every absence check is visible and
// tunable in one place.
//
// 500ms, not seconds: the events these checks rule out (a record event
// leaking onto the hub) travel an in-process, synchronous path —
// handleCreate → broadcaster.PublishTodoUpdate → hub.Broadcast — so a
// regression shows up in single-digit milliseconds. A negative assertion
// cannot short-circuit and must genuinely wait out its window, which is why
// this number is set to the delivery latency it guards with two orders of
// magnitude of margin, not to "long enough to feel safe".
const sseAbsenceWindow = 500 * time.Millisecond

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
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := newTestCtx(t)

	client := loginClient(ctx, t, base)
	resp, err := doPostForm(ctx, client, base+"/api/todos", url.Values{titleField: {buyMilk}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/todos/stream?clientID="+clientID, nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/todos/stream?clientID="+clientID, nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("SSE status=%d", resp.StatusCode)
	}
	return resp
}

// pumpSSEUntil reads the SSE stream until the predicate returns true
// or the timeout expires. The accumulated bytes are returned so
// callers can run multiple substring assertions on the full transcript.
//
// The timeout is enforced even while a Read is parked. A plain
// `for time.Now().Before(deadline) { stream.Body.Read(...) }` loop does NOT
// honour its deadline: Read blocks until the next event arrives, so the
// condition is only re-evaluated after that event — for a stream with nothing
// to say, the SSE heartbeat (config.DefaultSSEHeartbeatInterval, 15s). A
// caller asking for a 6s window would get 15s. Reading in a goroutine and
// selecting on a timer makes the deadline real; on expiry the body is closed
// to unblock the reader (callers close it again in their defer, harmlessly).
func pumpSSEUntil(t *testing.T, stream *http.Response, timeout time.Duration, stop func(string) bool) string {
	t.Helper()
	var full strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, sseBufferSize)
		for {
			n, err := stream.Body.Read(buf)
			if n > 0 {
				full.Write(buf[:n])
				if stop(full.String()) {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		_ = stream.Body.Close()
		<-done
	}
	// Safe to read: both branches above are past <-done, so the reader has
	// stopped writing to full.
	return full.String()
}

// sseSignalPayloads returns the JSON object of every Datastar signals patch in
// the transcript, i.e. the `data:` payloads that begin with the
// `signals ` dataline literal (datastar.SignalsDatalineLiteral), with that
// prefix stripped so the result is directly json.Unmarshal-able.
//
// Two things make the naive version wrong, and both are silent:
//
//   - json.Unmarshal(transcript, ...) never succeeds — a transcript is
//     `event:`/`data:` lines, not JSON — so a predicate written that way can
//     never fire and just burns the full timeout.
//   - Even a correctly extracted `data:` payload is prefixed with `signals `,
//     so it still is not JSON until the prefix is removed.
func sseSignalPayloads(transcript string) []string {
	var out []string
	for _, payload := range parseSSEData(transcript) {
		if after, ok := strings.CutPrefix(payload, "signals "); ok {
			out = append(out, after)
		}
	}
	return out
}

// pumpSSEFor drains the SSE stream for the whole window and returns the
// transcript, for tests that assert something does NOT arrive.
//
// A negative assertion cannot short-circuit — there is no successful moment to
// stop on, because the event is either absent or late. Using pumpSSEUntil for
// one wastes the full timeout AND reads as an ordinary wait, which is what
// made two 6s timers in TestTodoRecordsNotBroadcastViaHub look like a slow
// suite instead of a deliberate absence check. Naming the intent also lets the
// window be the only number to tune.
func pumpSSEFor(t *testing.T, stream *http.Response, window time.Duration) string {
	t.Helper()
	return pumpSSEUntil(t, stream, window, func(string) bool { return false })
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

// TestPumpSSEUntil_HonorsDeadlineWhileReadParked is the regression guard for
// the deadline bug that made every absence check cost the SSE heartbeat.
//
// A parked Read must not outlive the requested window: the reader blocks until
// the next event, so a loop that only checks its deadline between Reads returns
// late — for a silent stream, one heartbeat (config.DefaultSSEHeartbeatInterval,
// 15s) late. The old implementation asked for 6s and took 15.5s.
//
// Red-proof: revert pumpSSEUntil to the `for time.Now().Before(deadline) {
// Read }` form and this fails (measured ~15s instead of ~250ms).
func TestPumpSSEUntil_HonorsDeadlineWhileReadParked(t *testing.T) {
	t.Parallel()
	// A body that never produces a byte nor an error: every Read parks until
	// the deadline fires and pumpSSEUntil closes it.
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	stream := &http.Response{Body: pr}

	const want = 250 * time.Millisecond
	start := time.Now()
	got := pumpSSEUntil(t, stream, want, func(string) bool { return false })
	elapsed := time.Since(start)

	if got != "" {
		t.Fatalf("expected empty transcript from a silent stream, got %q", got)
	}
	// Generous upper bound: the point is to catch a 15s heartbeat wait, not
	// to assert scheduler precision on a loaded CI box.
	if elapsed > 2*time.Second {
		t.Fatalf("pumpSSEUntil waited %v for a %v window — the deadline is not enforced while Read is parked", elapsed, want)
	}
	if elapsed < want {
		t.Fatalf("pumpSSEUntil returned in %v, before its %v window elapsed", elapsed, want)
	}
}
