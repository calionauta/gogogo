// SCOPE:feature — todo presence propagation (red-proof for the live "N online" pill)
//
// Regression test for the reported defect: opening /todo as a second user
// (demo2 in another browser) left the first tab stuck at "1 online" until a
// refresh. Connect and disconnect must broadcast the presence count to every
// login's stream; only the page seed and the per-stream handshake write it
// otherwise.
package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// presenceBudget bounds every parked stream read: the hub heartbeats, so an
// unbounded read hangs instead of failing.
const presenceBudget = 10 * time.Second

// TestHandlersTodoPresencePropagates opens two streams as two different
// users and asserts the first tab learns the second tab's arrival AND
// departure without reconnecting.
func TestHandlersTodoPresencePropagates(t *testing.T) {
	f := newHandlersFixture(t)
	srv := httptest.NewServer(f.r)
	defer srv.Close()

	clientA := presenceClient(t, srv.URL, "presence-a@example.com")
	clientB := presenceClient(t, srv.URL, "presence-b@example.com")

	// Tab A opens its stream first and drains its handshake (1 online).
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	respA := openPresenceStream(ctxA, t, clientA, srv.URL, "presence-tab-a")
	defer respA.Body.Close()
	sawA := make(chan string, 16)
	go scanSignals(respA, sawA)
	if !waitSignal(sawA, `"connectedClients":1`) {
		t.Fatalf("tab A never saw its 1-online handshake")
	}

	// Tab B (a DIFFERENT user, the reported shape) connects. A must learn
	// "2 online" live — before the fix only B's own handshake carried it.
	ctxB, cancelB := context.WithCancel(context.Background())
	respB := openPresenceStream(ctxB, t, clientB, srv.URL, "presence-tab-b")
	defer respB.Body.Close()
	// Drain B's own handshake first: it proves B is fully connected (past
	// the handshake merges), so the later cancel lands on the idle loop
	// instead of racing an in-flight handshake write.
	sawB := make(chan string, 16)
	go scanSignals(respB, sawB)
	if !waitSignal(sawB, `"connectedClients":2`) {
		t.Fatalf("tab B never saw its 2-online handshake")
	}
	if !waitSignal(sawA, `"connectedClients":2`) {
		t.Fatalf("tab A never learned tab B joined (presence not broadcast on connect)")
	}

	// Tab B leaves. A must fall back to "1 online" without reconnecting.
	cancelB()
	respB.Body.Close()
	if !waitSignal(sawA, `"connectedClients":1`) {
		t.Fatalf("tab A never learned tab B left (presence not broadcast on disconnect)")
	}
}

// presenceClient signs one user up over the real HTTP surface and returns a
// client holding its session cookie.
func presenceClient(t *testing.T, baseURL, email string) *http.Client {
	t.Helper()
	jar, jarErr := cookiejar.New(nil)
	if jarErr != nil {
		t.Fatalf("jar: %v", jarErr)
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	body := strings.NewReader(`{"email":` + strconv.Quote(email) + `,"password":"Correct-horse-99!"}`)
	signupReq, reqErr := http.NewRequestWithContext(context.Background(),
		http.MethodPost, baseURL+"/api/auth/signup/credential", body)
	if reqErr != nil {
		t.Fatalf("signup request: %v", reqErr)
	}
	signupReq.Header.Set("Content-Type", "application/json")
	resp, doErr := client.Do(signupReq)
	if doErr != nil {
		t.Fatalf("signup %s: %v", email, doErr)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signup %s status = %d", email, resp.StatusCode)
	}
	return client
}

// openPresenceStream opens one todo SSE stream for clientID on client.
func openPresenceStream(
	ctx context.Context, t *testing.T, client *http.Client, baseURL, clientID string,
) *http.Response {
	t.Helper()
	req, reqErr := http.NewRequestWithContext(ctx,
		http.MethodGet, baseURL+"/api/todos/stream?clientID="+clientID, nil)
	if reqErr != nil {
		t.Fatalf("stream request: %v", reqErr)
	}
	resp, doErr := client.Do(req)
	if doErr != nil {
		t.Fatalf("open stream %s: %v", clientID, doErr)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("stream %s status = %d", clientID, resp.StatusCode)
	}
	return resp
}

// scanSignals forwards raw stream lines to ch until the response ends.
func scanSignals(resp *http.Response, ch chan<- string) {
	defer close(ch)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		ch <- scanner.Text()
	}
}

// waitSignal reports whether any forwarded line contains want in budget.
func waitSignal(ch <-chan string, want string) bool {
	deadline := time.After(presenceBudget)
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return false
			}
			if strings.Contains(line, want) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
