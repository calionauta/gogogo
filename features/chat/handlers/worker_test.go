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
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/llm"
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

func (s *memStore) Clear(_ context.Context, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.msgs[:0]
	for _, m := range s.msgs {
		if m.Owner != owner {
			kept = append(kept, m)
			continue
		}
		if m.IdemKey != "" {
			delete(s.seen, owner+"\x00"+m.IdemKey)
		}
	}
	s.msgs = kept
	return nil
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

// TestWorkerDeliversComponentsForMalformedAnswer is the reported chat
// symptom, end to end through the worker: the model parks a section's
// children inside "props" under the envelope key (valid JSON, rejected by
// strict prop decoding), and the tab must receive nested components — the
// raw JSON used to land in a prose bubble instead. Reverting the tolerant
// reader makes the delivered envelope carry the payload text.
func TestWorkerDeliversComponentsForMalformedAnswer(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")
	mem := newMemStore()

	raw := `{"components":[` +
		`{"type":"section","props":{"title":"Today's Plan","components":[` +
		`{"type":"section","props":{"title":"Morning","components":[` +
		`{"type":"plan_cards","props":{"plans":[{"title":"Breakfast","detail":"7:30 AM"}]}}]}}]}}]}`
	h := &Handler{hub: hub, store: mem, responder: stubResponder{raw: raw}}
	payload, _ := json.Marshal(map[string]string{"prompt": "plan my day", "userId": "u1", "idem": "k7"})
	job := queue.Job{Type: "chat_ask", ClientID: "c1", Payload: payload}

	if err := h.handleChatJob(context.Background(), hub, job); err != nil {
		t.Fatalf("handleChatJob: %v", err)
	}
	msgs, err := mem.List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("store holds %d messages, want user + answer", len(msgs))
	}
	answer := msgs[1]
	if len(answer.Components) != 1 || answer.Components[0].Component != "section" {
		t.Fatalf("persisted answer = %+v, want one section directive", answer.Components)
	}
	if len(answer.Components[0].Children) != 1 || answer.Components[0].Children[0].Component != "section" {
		t.Fatalf("nested children = %+v, want the inner section", answer.Components[0].Children)
	}
	select {
	case rawEnv := <-ch:
		got := string(rawEnv)
		if !strings.Contains(got, `"Component":"section"`) {
			t.Fatalf("envelope missing the rendered directive, got: %s", got)
		}
		if strings.Contains(got, `components":[`) {
			t.Fatalf("envelope carries the raw model JSON instead of components: %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no chat envelope arrived on hub channel")
	}
}

// TestWorkerNamesTheHydrationStage pins the per-answer status: an answer
// with panels first names the stage ("Loading your data…", spinner kept),
// then delivers the hydrated answer. Revert the worker to silent hydration
// and the first envelope is the answer itself. Pure-text answers send no
// stage — the thinking bubble is enough for them.
func TestWorkerNamesTheHydrationStage(t *testing.T) {
	t.Parallel()
	if err := comp.InstallPanelRegistry([]comp.PanelProvider{stubPanel{}}); err != nil {
		t.Fatalf("install panel registry: %v", err)
	}
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")
	mem := newMemStore()

	raw := `{"components":[{"type":"todo_list","props":{"filter":"open"}}]}`
	h := &Handler{hub: hub, store: mem, responder: stubResponder{raw: raw}}
	payload, _ := json.Marshal(map[string]string{"prompt": "my todos", "userId": "u1", "idem": "k-stage"})
	job := queue.Job{Type: "chat_ask", ClientID: "c1", Payload: payload}

	if err := h.handleChatJob(context.Background(), hub, job); err != nil {
		t.Fatalf("handleChatJob: %v", err)
	}
	decode := func(b []byte) chatEnvelope {
		t.Helper()
		var j queue.Job
		if err := json.Unmarshal(b, &j); err != nil {
			t.Fatalf("decode job: %v", err)
		}
		var env chatEnvelope
		if err := json.Unmarshal(j.Payload, &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		return env
	}
	select {
	case first := <-ch:
		if got := decode(first); got.Working == "" {
			t.Fatalf("first envelope names no stage (working=%q)", got.Working)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no working envelope arrived on hub channel")
	}
	select {
	case second := <-ch:
		if got := decode(second); got.Working != "" || got.Directives == nil {
			t.Fatalf("second envelope is not the answer: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no answer envelope arrived on hub channel")
	}
}

// TestResponderForPrefersModelLegOverStub pins the per-request dispatcher:
// without a model leg the handler keeps its injected stub (tests, demos
// without keys); once boot installs the leg, the stub is out of the path
// and the tab's surfaces drive a rebuilt responder. Revert responderFor to
// the bare stub and the leg case answers with stub text.
func TestResponderForPrefersModelLegOverStub(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := &Handler{responder: stubResponder{raw: "stub answer"}}
	if got, err := h.responderFor(comp.DefaultSurfacesChat).Respond(ctx, "x"); err != nil || got != "stub answer" {
		t.Fatalf("no leg: got %q err %v, want the stub answer", got, err)
	}

	sim := llm.NewSimulatedWithDelay(time.Millisecond)
	defer sim.Close()
	h.SetModelLeg(nil, sim, nil, comp.DefaultSurfacesChat)
	got, err := h.responderFor(comp.DefaultSurfacesChat).Respond(ctx, "x")
	if err != nil {
		t.Fatalf("model leg: %v", err)
	}
	if !strings.Contains(got, "html_block") {
		t.Fatalf("model leg answered %q, want the demo responder (html_block), not the stub", got)
	}
	if strings.Contains(got, "stub answer") {
		t.Fatalf("model leg leaked the stub answer: %q", got)
	}
}
