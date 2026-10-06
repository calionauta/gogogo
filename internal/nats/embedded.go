// SCOPE:layer=infra,removal=plugin — NATS JetStream + Leaf Node + CRUD proxy
// Starts embedded NATS server or connects as Leaf Node.
package nats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsio "github.com/nats-io/nats.go"
)

// Handle is one NATS connection set: the embedded (or leaf-node) server, the
// client connection, and the JetStream context. Callers hold it explicitly
// instead of reading package state.
//
// This used to be three mutable package globals (`NS`, `NC`, `JS`) that
// `StartEmbedded`, `StartLeafNode`, `ConnectExisting` and `Stop` assigned. Two
// concurrent starts wrote the same three variables and raced (race detector:
// "Write at ... embedded.go:29"), and a parallel test's `Stop()` tore the
// server out from under its neighbours (`add stream: nats: connection
// closed`). A value returned to the caller has neither problem: two `Handle`s
// are independent, and dropping one closes only its own resources.
type Handle struct {
	// Server is the embedded/leaf-node server this handle started. It is nil
	// when the handle was produced by ConnectExisting (an external server, not
	// ours to shut down).
	Server *server.Server
	// Conn is the client connection. Always non-nil on a usable handle.
	Conn *natsio.Conn
	// JS is the JetStream context bound to Conn.
	JS natsio.JetStreamContext
}

// URL is the client URL this handle connects to. Empty for a zero handle.
func (h *Handle) URL() string {
	if h == nil {
		return ""
	}
	if h.Server != nil {
		return h.Server.ClientURL()
	}
	if h.Conn != nil {
		return h.Conn.ConnectedUrl()
	}
	return ""
}

// Close shuts down the server this handle started (if any) and closes its
// connection. Safe to call on a nil handle, and safe to call twice.
//
// It does NOT touch the current-handle registry, so closing a stale handle can
// never clear a newer one — the bug a bare `Stop()` had.
func (h *Handle) Close() {
	if h == nil {
		return
	}
	if h.Conn != nil {
		h.Conn.Close()
	}
	if h.Server != nil {
		h.Server.Shutdown()
	}
}

// currentMu guards current. Package state is now a single pointer that is
// only ever read/written under this lock, so the accessors below are
// race-free; the previous three bare globals were not.
var (
	currentMu sync.RWMutex
	current   *Handle
)

// Current returns the handle installed by the most recent successful
// StartEmbedded / StartLeafNode / ConnectExisting, or nil when nothing has
// been started.
//
// It exists for the call sites that genuinely cannot thread the handle
// themselves: the PocketBase OnServe closures in router/, which read the
// connection when the serve event fires — long after the boot function that
// created it returned.
func Current() *Handle {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return current
}

// setCurrent installs h (which may be nil) as the handle the accessors return.
func setCurrent(h *Handle) {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = h
}

// StartEmbedded starts an embedded NATS server with JetStream enabled and
// connects to it, returning the handle. The realtime broadcaster (the TODOS
// stream) requires JetStream: without it, AddStream has no responder and the
// app falls back to the in-memory broadcaster with the "nats: no responders"
// log line.
//
// On failure the partially-built handle is torn down before returning, so a
// failed start leaks neither a server nor a connection.
func StartEmbedded(storeDir string) (*Handle, error) {
	ns, err := server.NewServer(&server.Options{
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
		StoreDir:  storeDir,
		JetStream: true,
	})
	if err != nil {
		return nil, err
	}
	ns.Start()

	h := &Handle{Server: ns}
	if err := h.dial(ns.ClientURL()); err != nil {
		h.Close()
		return nil, err
	}

	// Wait for the embedded server to accept client connections, then for
	// JetStream to be fully initialized. JetStream's store restore can finish a
	// moment after Start() returns; if a caller issues AddStream before it's up,
	// the request hits "no responders" and the broadcaster falls back to
	// in-memory.
	const natsReadyTimeout = 10 * time.Second
	if !ns.ReadyForConnections(natsReadyTimeout) {
		h.Close()
		return nil, errors.New("nats: embedded server never became ready")
	}
	if err := waitForJetStream(h.JS, natsReadyTimeout); err != nil {
		h.Close()
		return nil, err
	}

	setCurrent(h)
	slog.Info("NATS embedded server started (JetStream enabled)", "url", ns.ClientURL())
	return h, nil
}

// StartLeafNode starts an embedded NATS server configured as a Leaf Node that
// syncs with a central NATS server at centralURL. The local server persists
// JetStream streams on disk and replays buffered events to the central
// automatically when connectivity returns — this is the edge sync primitive
// for the desktop/mobile app (offline-first).
//
// storeDir holds the local JetStream file storage; centralURL is the ws/nats
// URL of the central server (e.g. nats://demo.example.com:7422).
func StartLeafNode(storeDir, centralURL string) (*Handle, error) {
	u, err := url.Parse(centralURL)
	if err != nil {
		return nil, fmt.Errorf("parse leaf node central url: %w", err)
	}
	ns, err := server.NewServer(&server.Options{
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
		StoreDir:  storeDir,
		JetStream: true,
		LeafNode: server.LeafNodeOpts{
			Remotes: []*server.RemoteLeafOpts{
				{URLs: []*url.URL{u}},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	ns.Start()

	h := &Handle{Server: ns}
	if err := h.dial(ns.ClientURL()); err != nil {
		h.Close()
		return nil, err
	}

	if !ns.ReadyForConnections(10 * time.Second) {
		h.Close()
		return nil, errors.New("nats: leaf node server never became ready")
	}
	if err := waitForJetStream(h.JS, 10*time.Second); err != nil {
		h.Close()
		return nil, err
	}

	setCurrent(h)
	slog.Info("NATS leaf node started (syncing with central)", "central", centralURL, "local", ns.ClientURL())
	return h, nil
}

// dial connects h.Conn to url and binds h.JS. On error h is left with a nil
// Conn so the caller's Close() stays safe.
func (h *Handle) dial(url string) error {
	nc, err := natsio.Connect(url)
	if err != nil {
		return err
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return err
	}
	h.Conn = nc
	h.JS = js
	return nil
}

// waitForJetStream polls AccountInfo until the JetStream API is serving
// or the timeout expires. AccountInfo issues a request to $JS.API.INFO,
// which errors with "no responders" until JetStream is initialized.
func waitForJetStream(js natsio.JetStreamContext, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := js.AccountInfo(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("nats: jetstream not ready within %s", timeout)
		case <-ticker.C:
		}
	}
}

// ConnectExisting connects to an already-running NATS server (e.g. the one
// booted by the DagNats engine) and returns a handle for it. This is the
// single-NATS setup: instead of starting a second embedded server, the
// realtime broadcaster reuses the JetStream instance DagNats already owns on
// the conventional port.
//
// RetryOnFailedConnect makes nats.Connect block until the server is reachable
// (the library retries internally) rather than returning immediately — so
// callers get a clean synchronous "NATS is ready" point with no polling loop
// of their own. If the server never comes up, the call returns an error after
// the configured timeout and the caller falls back to the in-memory
// broadcaster.
//
// The returned handle's Server is nil: this connection does not own the
// server, so Close() will not shut it down.
func ConnectExisting(url string) (*Handle, error) {
	nc, err := natsio.Connect(
		url,
		natsio.RetryOnFailedConnect(true),
		natsio.Timeout(15*time.Second), //nolint:mnd // 15s is the standard NATS connection timeout
	)
	if err != nil {
		return nil, err
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, err
	}
	h := &Handle{Conn: nc, JS: js}
	if err := waitForJetStream(js, 10*time.Second); err != nil {
		h.Close()
		return nil, err
	}

	setCurrent(h)
	slog.Info("NATS connected to existing server (JetStream enabled)", "url", url)
	return h, nil
}

// Stop shuts down the current handle's server (if it started one) and closes
// its connection. It is the compatible shim for callers that used the old
// package-level Stop; new code should hold its Handle and call Close.
func Stop() {
	currentMu.Lock()
	h := current
	current = nil
	currentMu.Unlock()
	h.Close()
}

// JetStream returns the current handle's JetStream context, or nil when
// nothing has been started.
func JetStream() natsio.JetStreamContext {
	if h := Current(); h != nil {
		return h.JS
	}
	return nil
}

// Conn returns the current handle's client connection, or nil when nothing
// has been started. Callers that need the raw connection (e.g. to subscribe a
// SyncWorker) use this.
func Conn() *natsio.Conn {
	if h := Current(); h != nil {
		return h.Conn
	}
	return nil
}

// ClientURL returns the current handle's URL, or "" when nothing has started.
func ClientURL() string {
	return Current().URL()
}
