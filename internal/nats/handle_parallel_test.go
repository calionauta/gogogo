// SCOPE:layer=infra,removal=plugin — NATS JetStream + Leaf Node + CRUD proxy
package nats_test

import (
	"sync"
	"testing"

	"github.com/calionauta/gogogo/internal/nats"
)

// TestHandleConcurrentStartIsIndependent is the regression guard for the
// package-global refactor.
//
// Before the Handle change, StartEmbedded assigned the package variables
// `NS/NC/JS` on entry (`NS, NC, JS = nil, nil, nil`), so two concurrent starts
// wrote the same three globals — a real data race the detector flagged at
// embedded.go:29 — and a test's Stop() cleared the globals (and shut the
// server down) under its neighbours, which is why this package could not use
// t.Parallel().
//
// With a Handle per start, the two starts share nothing. This asserts both
// halves: no race, and closing one handle does not disturb the other.
func TestHandleConcurrentStartIsIndependent(t *testing.T) {
	t.Parallel()

	const n = 3
	handles := make([]*nats.Handle, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			handles[i], errs[i] = nats.StartEmbedded(t.TempDir())
		})
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("handle %d: %v", i, errs[i])
		}
		if handles[i].JS == nil || handles[i].Conn == nil {
			t.Fatalf("handle %d is not usable", i)
		}
	}
	t.Cleanup(func() {
		for _, h := range handles {
			h.Close()
		}
	})

	// Closing one handle must not break the others: no shared server, no
	// shared connection, no shared global to clear.
	handles[0].Close()
	for i := 1; i < n; i++ {
		if _, err := handles[i].JS.AccountInfo(); err != nil {
			t.Fatalf("handle %d unusable after closing handle 0 — state is still shared: %v", i, err)
		}
	}

	// A handle from ConnectExisting owns no server: Close must not try to
	// shut down something it does not own. Use handle 1's URL (a live server)
	// and confirm the original stays up.
	ext, err := nats.ConnectExisting(handles[1].URL())
	if err != nil {
		t.Fatalf("ConnectExisting: %v", err)
	}
	if ext.Server != nil {
		t.Error("ConnectExisting produced a handle claiming to own a server it did not start")
	}
	ext.Close()
	if _, err := handles[1].JS.AccountInfo(); err != nil {
		t.Fatalf("closing the external handle shut down the server it did not own: %v", err)
	}
}
