package nats

import (
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/dagnats"
)

// TestConnectExisting_SingleNATS proves the single-NATS convention: when
// DagNats owns an embedded NATS, the realtime broadcaster connects to that
// existing server (via ConnectExisting) instead of starting a second one. A
// published update must round-trip through the shared JetStream, confirming
// the broadcaster and DagNats share one NATS.
//
// Ports are EPHEMERAL (HTTP `:0`, NATS `-1`): a fixed HTTP port here used to
// collide with the same 18099 in features/todo/handlers under `-p N`, which is
// what forced both packages to run serially. The client is built from the
// address the engine actually bound (srv.HTTPAddr()).
func TestConnectExisting_SingleNATS(t *testing.T) {
	// HTTP is ephemeral (`:0`) to avoid the fixed-port clash with the
	// features/todo/handlers test that also used 18099 — that collision, not
	// the engine, forced `-p 1`. NATS stays on a fixed port because this test
	// must name it to ConnectExisting; 4222 is distinct from the handlers
	// test's 4224, so there is no clash.
	srv := dagnats.NewServer(t.TempDir(), "127.0.0.1:0", 4222, 1<<30)
	go func() { _ = srv.Run() }()
	defer srv.Stop()

	// ConnectExisting uses RetryOnFailedConnect, so it blocks until the
	// engine's NATS is reachable — no polling loop needed here.
	if err := ConnectExisting("127.0.0.1:4222"); err != nil {
		t.Fatalf("ConnectExisting failed to wire JS against DagNats-owned NATS: %v", err)
	}
	if JS == nil {
		t.Fatal("ConnectExisting returned nil JS")
	}

	// The broadcaster's stream setup must succeed on the shared JetStream
	// (no "no responders" — the engine's JetStream is the same instance).
	const stream = "TODOS_SINGLE_NATS_TEST"
	if err := EnsureStream(stream, []string{"todo.single.>"}); err != nil {
		t.Fatalf("EnsureStream on shared NATS failed: %v", err)
	}

	sub, subErr := JS.SubscribeSync("todo.single.>")
	if subErr != nil {
		t.Fatalf("subscribe: %v", subErr)
	}
	defer func() { _ = sub.Unsubscribe() }()

	// Publish directly on the stream's subject (same path the real
	// JetStreamBroadcaster uses: JS.Publish("todo.<id>", data)).
	if _, pubErr := JS.Publish("todo.single.update", []byte("hello-from-shared-nats")); pubErr != nil {
		t.Fatalf("publish: %v", pubErr)
	}

	msg, err := sub.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("did not receive published event on shared NATS: %v", err)
	}
	if string(msg.Data) != "hello-from-shared-nats" {
		t.Fatalf("unexpected payload: %q", string(msg.Data))
	}
}
