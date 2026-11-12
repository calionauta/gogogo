package handlers

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/queue"
)

// TestHandlersChatStampsCurrentAnswer pins the write half of catalog versioning:
// the stored answer carries the fingerprint of the vocabulary that validated it,
// which is the only thing that lets a reload tell "renders as stored" from "the
// catalog moved". Revert the stamp and every stored answer looks pre-versioning.
func TestHandlersChatStampsCurrentAnswer(t *testing.T) {
	mem := newMemStore()
	h := &Handler{hub: queue.NewSSEHub(), store: mem, responder: stubResponder{raw: "ok"}}
	payload, _ := json.Marshal(map[string]string{"prompt": "hi", "userId": "u1", "idem": "stamp"})
	if err := h.handleChatJob(context.Background(), h.hub, queue.Job{Type: "chat_ask", Payload: payload}); err != nil {
		t.Fatalf("handleChatJob: %v", err)
	}
	msgs, err := mem.List(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	var answer *store.Message
	for i, m := range msgs {
		if m.Role == "assistant" {
			answer = &msgs[i]
		}
	}
	if answer == nil {
		t.Fatal("the answer was not stored")
	}
	if answer.CatalogVersion != comp.CatalogVersion() {
		t.Fatalf("stored catalog version = %q, want %q", answer.CatalogVersion, comp.CatalogVersion())
	}
}

// TestHandlersChatDropsRetiredComponentFromOldAnswer is the degradation
// contract: an answer stamped with a previous catalog loses only the component
// that no longer exists. Revert catalogCompatible and the unknown name makes
// RenderAll fail, so "kept note" disappears too.
func TestHandlersChatDropsRetiredComponentFromOldAnswer(t *testing.T) {
	mux, a, mem := chatFixture(t, stubResponder{raw: "{}"})
	cookie := chatSignup(t, mux, "catalog-old@example.com")
	owner := sessionOwner(t, a, cookie)

	ctx := context.Background()
	if err := mem.Append(ctx, owner, store.Message{
		Role:           "assistant",
		CatalogVersion: "00000000",
		Components: []comp.Directive{
			{Component: "retired_widget", Props: map[string]any{"text": "gone"}},
			{Component: "text_note", Props: map[string]any{"text": "kept note"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/chat", nil)
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /chat status = %d", rec.Code)
	}
	got := html.UnescapeString(rec.Body.String())
	if !strings.Contains(got, "kept note") {
		t.Fatalf("history dropped the surviving component of a stale answer: %.600q", got)
	}
	if strings.Contains(got, "gone") {
		t.Error("history rendered the retired component")
	}
}
