package todo

import (
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/queue"
)

func TestSSEHubRegisterAndSend(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 10)

	hub.Register("client-1", "", ch)
	hub.Send("client-1", []byte("hello"))

	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Fatalf("expected 'hello', got %q", string(msg))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestSSEHubBufferThenReplay(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()

	// Send before register (goes to buffer)
	hub.Send("client-late", []byte("buffered-msg"))

	// Now register — should replay
	ch := make(chan []byte, 10)
	hub.Register("client-late", "", ch)

	select {
	case msg := <-ch:
		if string(msg) != "buffered-msg" {
			t.Fatalf("expected 'buffered-msg', got %q", string(msg))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout: buffered message not replayed")
	}
}

func TestSSEHubBroadcast(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch1 := make(chan []byte, 10)
	ch2 := make(chan []byte, 10)
	hub.Register("a", "", ch1)
	hub.Register("b", "", ch2)

	hub.Broadcast([]byte("broadcast!"))

	for name, ch := range map[string]chan []byte{"a": ch1, "b": ch2} {
		select {
		case msg := <-ch:
			if string(msg) != "broadcast!" {
				t.Fatalf("client %s: expected 'broadcast!', got %q", name, string(msg))
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout: client %s didn't receive broadcast", name)
		}
	}
}

func TestSSEHubBackpressure(t *testing.T) {
	t.Parallel()
	t.Helper()
	hub := queue.NewSSEHub()
	// Full channel (capacity 1, already has message)
	ch := make(chan []byte, 1)
	ch <- []byte("existing")
	hub.Register("slow", "", ch)

	// This should not block
	hub.Send("slow", []byte("overflow"))

	// Should still have the original message
	got := <-ch
	if string(got) != "existing" {
		t.Fatalf("expected original 'existing' message, got %q", string(got))
	}
}

func TestSSEHubUnregister(t *testing.T) {
	t.Parallel()
	t.Helper()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 10)
	hub.Register("gone", "", ch)
	hub.Unregister("gone")

	// Should not panic
	hub.Send("gone", []byte("ghost"))
	hub.Broadcast([]byte("nothing"))
}

// TestSSEHubBroadcastToUser verifies per-user scoping: a record mutation
// reaches only clients owned by the same user, and the originating client
// (excludeClientID) is skipped. This guards the userOf map initialization
// bug where BroadcastToUser silently delivered to nobody.
func TestSSEHubBroadcastToUser(t *testing.T) {
	t.Parallel()
	t.Helper()
	hub := queue.NewSSEHub()
	u1a := make(chan []byte, 10) // user u1, originator
	u1b := make(chan []byte, 10) // user u1, other tab
	u2c := make(chan []byte, 10) // user u2, different user
	hub.Register("u1a", "u1", u1a)
	hub.Register("u1b", "u1", u1b)
	hub.Register("u2c", "u2", u2c)

	const msg = "mutation"
	hub.BroadcastToUser([]byte(msg), "u1", "u1a")

	// u1b (same user, not excluded) must receive.
	select {
	case got := <-u1b:
		if string(got) != msg {
			t.Fatalf("u1b: expected %q, got %q", msg, string(got))
		}
	case <-time.After(time.Second):
		t.Fatal("u1b: same-user client did not receive scoped broadcast")
	}

	// u1a (excluded originator) must NOT receive.
	select {
	case got := <-u1a:
		t.Fatalf("u1a: excluded originator unexpectedly received %q", string(got))
	case <-time.After(100 * time.Millisecond):
	}

	// u2c (different user) must NOT receive.
	select {
	case got := <-u2c:
		t.Fatalf("u2c: cross-user leak, received %q", string(got))
	case <-time.After(100 * time.Millisecond):
	}
}
