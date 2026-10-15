// SCOPE:core — Todo CRUD wire protocol (shared vocabulary, no framework types)
//
// Package natswire owns the app.crud.todo.> wire: subjects, payload types
// and the publisher. Every stack publishes and consumes the SAME subjects
// with separate durables, so mixed deployments converge. It deliberately
// imports nothing app-specific, so flat-stack code can depend on it
// without dragging any framework along.
package natswire

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	natsio "github.com/nats-io/nats.go"
)

// CRUD stream and subject constants.
const (
	crudStreamName    = "APP_CRUD"
	crudSubjectPrefix = "app.crud.todo."
	crudDurableFmt    = "%s-crud"
)

// CrudSubjectAll is the wildcard subject for todo CRUD operations. Every
// consumer subscribes with its own durable (separate consumer groups), so
// one wire serves all stacks.
const CrudSubjectAll = crudSubjectPrefix + ">"

// CrudDurable returns the durable consumer name for an app: the exact
// historical "<app>-crud" name, so existing consumers keep their cursors
// across this refactor. Chi uses CrudDurable(app)+"-chi" for its independent
// cursor over the same subjects.
func CrudDurable(appName string) string {
	return fmt.Sprintf(crudDurableFmt, appName)
}

// CrudOpType is the kind of CRUD operation published to NATS.
type CrudOpType string

const (
	CrudOpCreate         CrudOpType = "create"
	CrudOpToggle         CrudOpType = "toggle"
	CrudOpDelete         CrudOpType = "delete"
	CrudOpClearCompleted CrudOpType = "clear_completed"
)

// CrudPayload is the wire format for a CRUD operation published to NATS.
type CrudPayload struct {
	Op     CrudOpType  `json:"op"`
	UserID string      `json:"userId"`
	Data   *CrudOpData `json:"data,omitempty"`
}

// CrudOpData carries the per-operation data. ID is REQUIRED for
// toggle/delete (must match the row id on the target instance). For
// create, the handler includes the locally generated ID so the remote
// consumer reuses the same one. Visibility travels on create so a
// 'public' row fans out to every login on the receiving instance;
// empty reads as "private" (pre-visibility publishers).
type CrudOpData struct {
	ID         string `json:"id,omitempty"`
	Title      string `json:"title,omitempty"`
	Completed  bool   `json:"completed,omitempty"`
	Visibility string `json:"visibility,omitempty"`
}

// CrudPublisher publishes todo CRUD operations to JetStream.
// Each handler (create/toggle/delete) calls Publish AFTER writing to its
// database, so the HTTP response is fast (the local write is the source of
// truth). The NATS publication is best-effort convergence for other
// instances (or the server, for desktop edges).
//
// Safe to call with a nil receiver (NATS unavailable) — it's a no-op.
type CrudPublisher struct {
	js   natsio.JetStreamContext
	mu   sync.Mutex
	done bool
}

// NewCrudPublisher ensures the APP_CRUD stream exists and returns a
// publisher. Returns nil when js is nil (NATS disabled), so callers
// can call Publish on nil safely (no-op).
func NewCrudPublisher(js natsio.JetStreamContext) *CrudPublisher {
	if js == nil {
		return nil
	}
	if _, err := js.AddStream(&natsio.StreamConfig{
		Name:     crudStreamName,
		Subjects: []string{crudSubjectPrefix + ">"},
		Storage:  natsio.FileStorage,
		// Bounded transport buffer: the database (not this stream) is the
		// truth — replaying month-old CRUD ops would resurrect
		// already-applied mutations. A week covers any realistic
		// consumer outage; MaxBytes caps disk either way.
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: 512 << 20,
	}); err != nil {
		if err.Error() != "stream already exists" {
			slog.Warn("crudproxy: add stream failed", "error", err)
			return nil
		}
	}
	return &CrudPublisher{js: js}
}

// Publish sends a CRUD operation to JetStream. Non-blocking from the
// caller's perspective: logs on error but returns immediately. The
// handler has already written to its database, so this is additive sync.
func (p *CrudPublisher) Publish(op CrudOpType, userID string, data *CrudOpData) {
	if p == nil {
		return
	}
	payload, err := json.Marshal(CrudPayload{Op: op, UserID: userID, Data: data})
	if err != nil {
		slog.Warn("crudproxy: marshal payload", "op", op, "error", err)
		return
	}
	subject := crudSubjectPrefix + string(op)
	if _, pubErr := p.js.Publish(subject, payload); pubErr != nil {
		slog.Warn("crudproxy: publish failed", "op", op, "subject", subject, "error", pubErr)
	}
}

// Close drains the publisher. Idempotent.
func (p *CrudPublisher) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return
	}
	p.done = true
}
