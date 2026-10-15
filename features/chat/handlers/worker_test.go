// SCOPE:feature — HTTP chat worker tests (ported from features/chat/handler_test.go)
//
// The worker is the same code the legacy-handler era ran; the port keeps the
// red-proof on the HTTP copy: one Ask persists both sides of the
// conversation in order, the originator's hub gets the chat envelope
// (never the pool's), offline replays store once, and a down model
// still delivers an error envelope that releases the thinking spinner.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/chat/store"
	"github.com/calionauta/gogogo/internal/queue"
)

// memStore is the in-memory MessageStore: same contract as the DB-backed
// store (append-only, owner-scoped, idem-deduped, created-ordered) with
// zero I/O, so worker tests run in milliseconds.
type memStore struct {
	mu   sync.Mutex
	msgs []store.Message
	seen map[string]bool
}

func newMemStore() *memStore { return &memStore{seen: map[string]bool{}} }

func (s *memStore) Append(_ context.Context, owner string, m store.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.IdemKey != "" {
		if s.seen[owner+"\x00"+m.IdemKey] {
			return nil
		}
		s.seen[owner+"\x00"+m.IdemKey] = true
	}
	m.Created = time.Now()
	s.msgs = append(s.msgs, store.Message{
		Owner: owner, Role: m.Role, Text: m.Text, Components: m.Components,
		IdemKey: m.IdemKey, Created: m.Created,
	})
	return nil
}

func (s *memStore) List(_ context.Context, owner string) ([]store.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Message
	for _, m := range s.msgs {
		if m.Owner == owner {
			out = append(out, m)
		}
	}
	return out, nil
}

// TestWorkerPersistsBothSides proves the conversation contract: one Ask
// persists exactly two records (user prompt + assistant answer) in
// created order, and the originator's hub gets a chat envelope. Uses a
// real SSEHub and the mem store — no model.
func TestWorkerPersistsBothSides(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")
	mem := newMemStore()

	h := &Handler{hub: hub, store: mem, responder: stubResponder{raw: "hello there"}}
	payload, _ := json.Marshal(map[string]string{"prompt": "hi", "userId": "u1", "idem": "k1"})
	job := queue.Job{Type: "chat_ask", ClientID: "c1", Payload: payload}

	if err := h.handleChatJob(context.Background(), hub, job); err != nil {
		t.Fatalf("handleChatJob: %v", err)
	}
	msgs, err := mem.List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("store = %+v, want [user assistant]", msgs)
	}
	if msgs[0].Text != "hi" {
		t.Fatalf("user text = %q, want prompt", msgs[0].Text)
	}
	// Fallback answers render once: empty Text with the single text_note
	// component carrying the prose (setting both rendered everything
	// twice in one bubble).
	if msgs[1].Text != "" {
		t.Fatalf("fallback Text = %q, want empty (component carries it)", msgs[1].Text)
	}
	if len(msgs[1].Components) != 1 || msgs[1].Components[0].Component != "text_note" {
		t.Fatalf("fallback components = %+v, want 1 text_note", msgs[1].Components)
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
	mem := newMemStore()
	ctx := context.Background()
	if err := mem.Append(ctx, "u1", store.Message{Role: "user", Text: "mine"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := mem.List(ctx, "u2"); len(got) != 0 {
		t.Fatalf("u2 sees %d messages, want 0 (cross-user leak)", len(got))
	}
}

// TestWorkerDedupesOfflineReplay proves idempotency: the same idem key
// twice (SW queue replay) stores one user message, not two.
func TestWorkerDedupesOfflineReplay(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	mem := newMemStore()
	h := &Handler{hub: hub, store: mem, responder: stubResponder{raw: "ok"}}
	payload, _ := json.Marshal(map[string]string{"prompt": "hi", "userId": "u1", "idem": "same"})
	job := queue.Job{Type: "chat_ask", ClientID: "", Payload: payload}
	ctx := context.Background()
	if err := h.handleChatJob(ctx, hub, job); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.handleChatJob(ctx, hub, job); err != nil {
		t.Fatalf("replay must not error, got: %v", err)
	}
	msgs, _ := mem.List(ctx, "u1")
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
	mem := newMemStore()

	h := &Handler{hub: hub, store: mem, responder: stubResponder{err: errors.New("boom")}}
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

// TestWorkerAppendsAuthHintOnProviderAuthFailure is the red-proof for the
// split-triple outage on the chat path: a provider 401 must reach the tab
// WITH the key↔base mismatch hint. Reverting the worker to err.Error()
// drops the hint and fails this test.
func TestWorkerAppendsAuthHintOnProviderAuthFailure(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")
	mem := newMemStore()

	const providerErr = `llm: generate: 401 {"error":{"message":"Missing Authentication header","code":401}}`
	h := &Handler{hub: hub, store: mem, responder: stubResponder{err: errors.New(providerErr)}}
	payload, _ := json.Marshal(map[string]string{"prompt": "x", "userId": "u1", "idem": "k10"})
	job := queue.Job{Type: "chat_ask", ClientID: "c1", Payload: payload}

	_ = h.handleChatJob(context.Background(), hub, job)
	select {
	case raw := <-ch:
		got := string(raw)
		if !strings.Contains(got, "Missing Authentication header") {
			t.Fatalf("provider message lost from chat error, got: %s", got)
		}
		if !strings.Contains(got, "GOAI_API_KEY") || !strings.Contains(got, "GOAI_BASE_URL") {
			t.Fatalf("chat auth failure missing the key↔base hint, got: %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no error envelope arrived on hub channel")
	}
}
