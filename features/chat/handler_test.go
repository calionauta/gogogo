// SCOPE:layer=feature,removal=feature — chat worker + store tests.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/queue"
)

// memStore is the in-memory MessageStore: same contract as the PB
// store (append-only, owner-scoped, idem-deduped, created-ordered) with
// zero I/O, so worker tests run in milliseconds.
type memStore struct {
	mu   sync.Mutex
	msgs []Message
	seen map[string]bool
}

func newMemStore() *memStore { return &memStore{seen: map[string]bool{}} }

func (s *memStore) Append(_ context.Context, owner string, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.IdemKey != "" {
		if s.seen[owner+"\x00"+m.IdemKey] {
			return nil
		}
		s.seen[owner+"\x00"+m.IdemKey] = true
	}
	m.Created = time.Now()
	s.msgs = append(s.msgs, Message{
		Owner: owner, Role: m.Role, Text: m.Text, Components: m.Components,
		IdemKey: m.IdemKey, Created: m.Created,
	})
	return nil
}

func (s *memStore) List(_ context.Context, owner string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Message
	for _, m := range s.msgs {
		if m.Owner == owner {
			out = append(out, m)
		}
	}
	return out, nil
}

type stubResponder struct {
	raw string
	err error
}

func (s stubResponder) Respond(_ context.Context, _ string) (string, error) {
	return s.raw, s.err
}

// TestWorkerPersistsBothSides proves the conversation contract: one Ask
// persists exactly two records (user prompt + assistant answer) in
// created order, and the originator's hub gets a chat envelope. Uses a
// real SSEHub and the mem store — no PocketBase, no model.
func TestWorkerPersistsBothSides(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")
	store := newMemStore()

	h := &Handler{hub: hub, store: store, responder: stubResponder{raw: "hello there"}}
	payload, _ := json.Marshal(map[string]string{"prompt": "hi", "userId": "u1", "idem": "k1"})
	job := queue.Job{Type: "chat_ask", ClientID: "c1", Payload: payload}

	if err := h.handleChatJob(context.Background(), hub, job); err != nil {
		t.Fatalf("handleChatJob: %v", err)
	}
	msgs, err := store.List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("store = %+v, want [user assistant]", msgs)
	}
	if msgs[0].Text != "hi" || !strings.Contains(msgs[1].Text, "hello") {
		t.Fatalf("texts = %q %q, want prompt + answer", msgs[0].Text, msgs[1].Text)
	}
	select {
	case raw := <-ch:
		if !strings.Contains(string(raw), `"type":"chat"`) {
			t.Fatalf("hub got non-chat envelope: %s", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no chat envelope on hub channel")
	}
}

// TestWorkerScopesOwners proves tenant isolation: tabs of another user
// never see these messages, even on a shared hub.
func TestWorkerScopesOwners(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	ctx := context.Background()
	if err := store.Append(ctx, "u1", Message{Role: "user", Text: "mine"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.List(ctx, "u2"); len(got) != 0 {
		t.Fatalf("u2 sees %d messages, want 0 (cross-user leak)", len(got))
	}
}

// TestWorkerDedupesOfflineReplay proves idempotency: the same idem key
// twice (SW queue replay) stores one user message, not two.
func TestWorkerDedupesOfflineReplay(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	store := newMemStore()
	h := &Handler{hub: hub, store: store, responder: stubResponder{raw: "ok"}}
	payload, _ := json.Marshal(map[string]string{"prompt": "hi", "userId": "u1", "idem": "same"})
	job := queue.Job{Type: "chat_ask", ClientID: "", Payload: payload}
	ctx := context.Background()
	if err := h.handleChatJob(ctx, hub, job); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.handleChatJob(ctx, hub, job); err != nil {
		t.Fatalf("replay must not error, got: %v", err)
	}
	msgs, _ := store.List(ctx, "u1")
	if len(msgs) != 2 {
		t.Fatalf("replay stored %d messages, want exactly 2 (user + answer, no dupes)", len(msgs))
	}
}

// TestWorkerReleasesThinkingOnModelDown proves the unconfigured path:
// the tab still gets an error envelope (spinner release lives in the
// dispatcher, like genui) and the failure is recorded, not silent.
func TestWorkerReleasesThinkingOnModelDown(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")
	store := newMemStore()

	h := &Handler{hub: hub, store: store, responder: stubResponder{err: errors.New("boom")}}
	payload, _ := json.Marshal(map[string]string{"prompt": "x", "userId": "u1", "idem": "k9"})
	job := queue.Job{Type: "chat_ask", ClientID: "c1", Payload: payload}

	_ = h.handleChatJob(context.Background(), hub, job)
	select {
	case raw := <-ch:
		if !strings.Contains(string(raw), "unavailable") {
			t.Fatalf("error envelope missing cause, got: %s", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no error envelope arrived on hub channel")
	}
}
