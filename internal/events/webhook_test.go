package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookSignsRetriesAndCarriesIdempotency(t *testing.T) {
	var attempts atomic.Int32
	var signature atomic.Value
	var idempotency atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature.Store(r.Header.Get("X-Gogogo-Signature"))
		idempotency.Store(r.Header.Get("X-Idempotency-Key"))
		_, _ = io.Copy(io.Discard, r.Body)
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError) // one transient failure
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	event := Event{
		Name:   "capability.interaction",
		At:     time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC),
		Fields: map[string]any{"capability": "kiosk"},
	}
	hook := &Webhook{URL: server.URL, Secret: "s3cret", Retries: 2}
	hook.Deliver(context.Background(), event)

	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry after the 500)", got)
	}
	sig, _ := signature.Load().(string)
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	_, _ = mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Errorf("signature = %q, want %q", sig, want)
	}
	if key, _ := idempotency.Load().(string); key == "" {
		t.Error("missing X-Idempotency-Key on the delivered event")
	}
}
