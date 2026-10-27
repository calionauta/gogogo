// SCOPE:plugin — delivery transport: notify adapter contract.
package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/delivery"
)

// TestSendPostsTheDocumentedTrigger pins the wire the engine actually accepts:
// path, API-key header, subscriber identity, payload and transaction id. A
// wrong field name here fails at the first real send, not in review.
func TestSendPostsTheDocumentedTrigger(t *testing.T) {
	var gotPath, gotAuth string
	var got triggerRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"acknowledged":true,"status":"processed"}`))
	}))
	defer server.Close()

	client := New(server.URL, "key-123")
	receipt, err := client.Send(context.Background(), delivery.Message{
		RecipientID:   "user-1",
		To:            "ada@example.com",
		Subject:       "Prazo hoje",
		Body:          "O item vence hoje.",
		TransactionID: "reminder:1",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.Status != delivery.StatusSent || receipt.Provider != "notify" {
		t.Fatalf("receipt = %+v, want sent/notify", receipt)
	}
	if gotPath != "/v1/events/trigger" {
		t.Errorf("path = %q, want /v1/events/trigger", gotPath)
	}
	if gotAuth != "ApiKey key-123" {
		t.Errorf("authorization = %q, want ApiKey key-123", gotAuth)
	}
	if got.Name != TriggerName {
		t.Errorf("name = %q, want %q", got.Name, TriggerName)
	}
	if got.To.SubscriberID != "user-1" || got.To.Email != "ada@example.com" {
		t.Errorf("to = %+v, want subscriber user-1 + email", got.To)
	}
	if got.Payload.Subject != "Prazo hoje" || got.Payload.Body != "O item vence hoje." {
		t.Errorf("payload = %+v, want the rendered subject/body", got.Payload)
	}
	if got.TransactionID != "reminder:1" {
		t.Errorf("transactionId = %q, want reminder:1", got.TransactionID)
	}
}

// TestSendReportsAnErrorStatus pins the failure half: a refused trigger is an
// error receipt, never a silent success.
func TestSendReportsAnErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":["workflow is inactive"]}`))
	}))
	defer server.Close()

	receipt, err := New(server.URL, "key").Send(context.Background(), delivery.Message{
		RecipientID: "u", To: "a@b.c", TransactionID: "t",
	})
	if err == nil {
		t.Fatal("Send accepted a 422; want an error")
	}
	if receipt.Status != delivery.StatusError || receipt.Err == nil {
		t.Fatalf("receipt = %+v, want status error with Err set", receipt)
	}
}
