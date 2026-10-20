// SCOPE:core — ServeStream disconnect authority test (split for size).
package queue

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestSSEHub_ServeStreamSkipsDisconnectWhenSuperseded pins that onDisconnect
// is a DOMAIN event (notes/whiteboard peerLeave) and rides the same
// authority as registration: a handler whose clientID was re-registered by a
// newer connection must not announce a departure for the live tab.
//
// RED-PROOFED: call onDisconnect unconditionally (the pre-fix shape) and
// `leaves` becomes 1 for the superseded handler.
func TestSSEHub_ServeStreamSkipsDisconnectWhenSuperseded(t *testing.T) {
	h := NewSSEHub()
	var leaves atomic.Int32
	handlerDone := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/s", func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		_ = h.ServeStream(w, r, "cli", "", nil, func() { leaves.Add(1) })
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/s", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	defer resp.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !h.IsRegistered("cli") {
		if time.Now().After(deadline) {
			t.Fatal("stream never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A newer connection takes over the same clientID (a reconnect), then the
	// old handler is torn down.
	chNew := make(chan []byte, 4)
	h.Register("cli", "", chNew)
	cancel()
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("superseded handler never returned")
	}
	if got := leaves.Load(); got != 0 {
		t.Fatalf("superseded handler announced %d departure(s), want 0", got)
	}
	if !h.IsRegistered("cli") {
		t.Fatal("the superseding registration was removed by the stale handler")
	}
	h.Unregister("cli")
}
