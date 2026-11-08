// Exactly-once effect for a mutation delivered more than once.
//
// The receipt transaction lives in the domain service
// (features/todo/service). This file keeps the HTTP shape: reading the
// Idempotency-Key header, with over-long keys hashed by service.HashOpKey.
package handlers

import (
	"net/http"
	"strings"

	"github.com/calionauta/gogogo/features/todo/service"
)

// idempotencyKey reads the outbox's token for this intent. Empty means the
// caller opted out: the mutation keeps at-least-once semantics.
// Over-long keys are hashed, not truncated, by service.HashOpKey.
func idempotencyKey(r *http.Request) string {
	return service.HashOpKey(strings.TrimSpace(r.Header.Get("Idempotency-Key")))
}
