// SCOPE:core — Queue + Retry demo end-to-end red-proof.
//
// The demo once stuck on step 1 forever: the todo stream listened on a
// standalone hub while the queue workers broadcast on the queue's own
// hub, so no worker feedback ever reached any tab. This test boots the
// production composition (BootChi), opens a real todo stream, runs the
// demo, and pins that the worker's completion reaches that same stream.
package server

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
)

// TestRetryDemoReachesStream pins the full Queue + Retry loop on the
// production wiring: POST /api/todos/retry-demo drives the worker's
// fail-twice-then-succeed job, and the tab's stream must carry the
// completion (the "Queue + retry OK" toast).
func TestRetryDemoReachesStream(t *testing.T) {
	cfg := config.Load()
	cfg.Dev = true
	cfg.SignupEnabled = true
	cfg.RBACEnabled = false
	tmp := t.TempDir()
	cfg.Turso.DBPath = tmp + "/e2e-retry.db"
	cfg.DataDir = tmp + "/data"
	cfg.NATS.Enabled = false
	cfg.DagNats.Enabled = false
	cfg.GoAkt.Enabled = false

	stack, err := BootChi(cfg)
	if err != nil {
		t.Fatalf("BootChi: %v", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := serveChiOnListener(ln, srvHandler(stack.mux), stack)
	defer stop()
	base := "http://" + ln.Addr().String()

	// Open the tab's stream first: the worker's feedback must land here.
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, err := http.NewRequestWithContext(streamCtx, http.MethodGet,
		base+"/api/todos/stream?clientID=e2e-retry-tab", nil)
	if err != nil {
		t.Fatalf("stream req: %v", err)
	}
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(streamResp.Body)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		var sb strings.Builder
		for sc.Scan() {
			sb.WriteString(sc.Text())
			sb.WriteString("\n")
			if strings.Contains(sb.String(), "Queue + retry OK") {
				found <- sb.String()
				return
			}
			if sb.Len() > 1<<20 {
				sb.Reset()
			}
		}
	}()

	// Give the stream a moment to register before the worker broadcasts.
	time.Sleep(300 * time.Millisecond)

	postReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/api/todos/retry-demo?clientID=e2e-retry-tab", nil)
	if err != nil {
		t.Fatalf("demo req: %v", err)
	}
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST retry-demo: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("POST retry-demo status = %d", postResp.StatusCode)
	}

	select {
	case <-found:
	case <-time.After(25 * time.Second):
		t.Fatal("timed out waiting for the Queue + Retry completion on the tab stream " +
			"(worker feedback never arrived: hub mismatch or lost clientID)")
	}
}
