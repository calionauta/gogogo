// Outbound webhook sink: HMAC-signed JSON with bounded retries.
//
// Delivery is best-effort in this first cut; the durable outbox is the next
// step (docs/hooks-analytics.md). The signature and idempotency header are
// already here so a receiver can build the same verification it would use for
// a durable delivery.
package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/internal/observability"
)

// Webhook posts every event as JSON. Secret, when set, becomes
// X-Gogogo-Signature: sha256=<hex hmac>.
type Webhook struct {
	URL     string
	Secret  string
	Client  *http.Client
	Retries int
}

// Deliver implements Sink.
func (w *Webhook) Deliver(ctx context.Context, event Event) {
	if w == nil || w.URL == "" {
		return
	}
	body, err := json.Marshal(event)
	if err != nil {
		observability.Warn(ctx, "events: webhook marshal",
			append(observability.WithComponent("events", "servant"), "error", err)...)
		return
	}
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	attempts := w.Retries
	if attempts <= 0 {
		attempts = 3
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
		if rerr != nil {
			observability.Warn(ctx, "events: webhook request",
				append(observability.WithComponent("events", "servant"), "error", rerr)...)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Gogogo-Event", event.Name)
		req.Header.Set("X-Idempotency-Key", event.Name+"-"+event.At.UTC().Format(time.RFC3339Nano))
		if w.Secret != "" {
			mac := hmac.New(sha256.New, []byte(w.Secret))
			_, _ = mac.Write(body)
			req.Header.Set("X-Gogogo-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, derr := client.Do(req)
		if derr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
			derr = fmt.Errorf("webhook status %d", resp.StatusCode)
		}
		observability.Warn(ctx, "events: webhook delivery failed",
			append(observability.WithComponent("events", "servant"),
				"event", event.Name, "attempt", attempt, "error", derr)...)
		if attempt < attempts && !waitBackoff(ctx, time.Duration(attempt)*200*time.Millisecond) {
			return // shutdown, not another attempt
		}
	}
}

// waitBackoff is a cancellable pause between attempts. time.Sleep cannot be
// interrupted, so it would keep a shutting-down process alive for its duration.
func waitBackoff(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
