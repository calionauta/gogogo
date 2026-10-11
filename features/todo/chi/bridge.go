// SCOPE:feature — Chi NATS bridge (cross-instance convergence, step 5)
//
// RunCrudConsumer subscribes to the same app.crud.todo.> subjects the
// PocketBase stack publishes, applies each operation to Turso, and fans the
// record event out to local chi streams. Durable is per app + "chi", so the
// PB consumer (<app>-crud) and the chi consumer (<app>-crud-chi) hold
// independent cursors over the same wire during the staged migration.
//
// Identity boundary: payload UserIDs are strings. Integer strings are chi
// owners and apply; anything else (PocketBase record ids) is skipped with a
// warning — the two stacks have incompatible identity models, and faking a
// mapping would corrupt tenant scoping. Full convergence within the chi
// stack; best-effort across stacks until PocketBase goes away (step 7).
package chi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
)

// RunCrudConsumer subscribes and blocks until ctx is cancelled. Nil js
// (NATS disabled) is a no-op returning nil — the chi stack then converges
// in-process through the Hub only.
func RunCrudConsumer(
	ctx context.Context, js natsio.JetStreamContext, appName string, q *dbgen.Queries, hub *queue.SSEHub,
) error {
	if js == nil {
		return nil
	}
	sub, err := js.Subscribe(natswire.CrudSubjectAll, func(msg *natsio.Msg) {
		var p natswire.CrudPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			slog.Warn("chi crudconsumer: unmarshal", "error", err)
			return
		}
		if p.UserID == "" {
			return
		}
		applyCrudOp(ctx, q, hub, p)
		_ = msg.Ack()
	}, natsio.Durable(natswire.CrudDurable(appName)+"-chi"), natsio.ManualAck())
	if err != nil {
		return err
	}
	<-ctx.Done()
	return sub.Unsubscribe()
}

// applyCrudOp applies one wire operation to Turso and fans out locally.
// Every branch is idempotent (redeliveries converge, never duplicate) and
// owner-scoped (a forged op for another tenant matches nothing).
func applyCrudOp(ctx context.Context, q *dbgen.Queries, hub *queue.SSEHub, p natswire.CrudPayload) {
	owner, err := tenantOf(ctx, q, p.UserID)
	if err != nil {
		if isSkip(err) {
			// Unknown identity (including PocketBase-world ids, which live
			// in a different identity model): skip, do not map. Faking a
			// mapping would corrupt tenant scoping.
			slog.Warn("chi crudconsumer: unknown owner, skipping", "userId", p.UserID, "op", p.Op)
			return
		}
		slog.Warn("chi crudconsumer: owner lookup", "userId", p.UserID, "error", err)
		return
	}
	if p.Data == nil {
		return
	}
	var evt recordEvent
	switch p.Op {
	case natswire.CrudOpCreate:
		evt, err = applyCreate(ctx, q, owner, p.Data)
	case natswire.CrudOpToggle:
		evt, err = applyToggle(ctx, q, owner, p.Data)
	case natswire.CrudOpDelete:
		evt, err = applyDelete(ctx, q, owner, p.Data)
	case natswire.CrudOpClearCompleted:
		evt, err = applyClearCompleted(ctx, q, owner)
	default:
		slog.Warn("chi crudconsumer: unknown op", "op", p.Op)
		return
	}
	if err != nil {
		if isSkip(err) {
			return // redelivery / empty payload: nothing to do, stay quiet.
		}
		slog.Warn("chi crudconsumer: apply failed", "op", p.Op, "error", err)
		return
	}
	payload, merr := json.Marshal(evt)
	if merr != nil {
		slog.Error("chi crudconsumer: marshal fan-out", "error", merr)
		return
	}
	hub.BroadcastToUser(payload, p.UserID, "")
}

// tenantOf resolves a wire user id to a real app identity. Unknown ids —
// including PocketBase-world record ids, which live in a different identity
// model — are a skip, never a mapping.
func tenantOf(ctx context.Context, q *dbgen.Queries, userID string) (string, error) {
	if userID == "" {
		return "", errSkip
	}
	if _, err := q.GetUser(ctx, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errSkip
		}
		return "", fmt.Errorf("tenant lookup %s: %w", userID, err)
	}
	return userID, nil
}

func applyCreate(ctx context.Context, q *dbgen.Queries, owner string, data *natswire.CrudOpData) (recordEvent, error) {
	if data.Title == "" {
		return recordEvent{}, errSkip
	}
	if _, err := q.GetTodo(ctx, dbgen.GetTodoParams{ID: data.ID, Owner: owner}); err == nil {
		return recordEvent{}, errSkip // redelivery: row already stands.
	} else if !errors.Is(err, sql.ErrNoRows) {
		return recordEvent{}, fmt.Errorf("create lookup %s: %w", data.ID, err)
	}
	now := db.Now()
	id := data.ID
	if id == "" {
		id = db.NewID()
	}
	if _, err := q.CreateTodo(ctx, dbgen.CreateTodoParams{
		ID: id, Owner: owner, Title: data.Title,
		Completed: data.Completed, IdemKey: "", Created: now, Updated: now,
	}); err != nil {
		return recordEvent{}, fmt.Errorf("create apply %s: %w", id, err)
	}
	return recordEvent{Event: "created", ID: id, Title: data.Title}, nil
}

func applyToggle(ctx context.Context, q *dbgen.Queries, owner string, data *natswire.CrudOpData) (recordEvent, error) {
	if data.ID == "" {
		return recordEvent{}, errSkip
	}
	// Absolute post-state, matching what publishers send (see handleToggle:
	// Done is the state after the local write).
	if err := q.SetTodoCompleted(ctx, dbgen.SetTodoCompletedParams{
		Completed: data.Completed, Updated: db.Now(), ID: data.ID, Owner: owner,
	}); err != nil {
		return recordEvent{}, fmt.Errorf("toggle apply %s: %w", data.ID, err)
	}
	return recordEvent{Event: "toggled", ID: data.ID, Done: data.Completed}, nil
}

func applyDelete(ctx context.Context, q *dbgen.Queries, owner string, data *natswire.CrudOpData) (recordEvent, error) {
	if data.ID == "" {
		return recordEvent{}, errSkip
	}
	if err := q.DeleteTodo(ctx, dbgen.DeleteTodoParams{ID: data.ID, Owner: owner}); err != nil {
		return recordEvent{}, fmt.Errorf("delete apply %s: %w", data.ID, err)
	}
	return recordEvent{Event: "deleted", ID: data.ID}, nil
}

func applyClearCompleted(ctx context.Context, q *dbgen.Queries, owner string) (recordEvent, error) {
	if _, err := q.DeleteCompletedByOwner(ctx, owner); err != nil {
		return recordEvent{}, fmt.Errorf("clear apply: %w", err)
	}
	return recordEvent{Event: "cleared"}, nil
}

// errSkip marks "nothing to do" separately from failure: callers stay quiet
// on skips (redelivery, empty payload) and warn on errors.
var errSkip = errors.New("chi crudconsumer: skip")

// isSkip reports whether apply returned a skip (not a failure).
func isSkip(err error) bool { return errors.Is(err, errSkip) }
