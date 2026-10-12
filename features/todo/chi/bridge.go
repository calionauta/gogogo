// SCOPE:feature — Chi NATS bridge (cross-instance convergence)
//
// RunCrudConsumer subscribes to the app.crud.todo.> subjects, applies each
// operation to Turso, and fans the record event out to local chi streams.
// Its durable is per app + "chi", independent from the server stack's
// cursor over the same wire.
//
// Identity boundary: payload UserIDs are strings resolved against app
// users; anything else is skipped with a warning — faking a mapping would
// corrupt tenant scoping. Convergence is complete within one identity
// model; foreign identities never apply.
package chi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	natsio "github.com/nats-io/nats.go"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/observability"
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
			obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
			observability.Warn(ctx, "chi crudconsumer: unmarshal", obsArgs...)
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
			// Unknown identity: skip, do not map. Faking a mapping would
			// corrupt tenant scoping.
			obsArgs := append(observability.WithComponent("todo", "feature"), "userId", p.UserID, "op", p.Op)
			observability.Warn(ctx, "chi crudconsumer: unknown owner, skipping", obsArgs...)
			return
		}
		obsArgs := append(observability.WithComponent("todo", "feature"), "userId", p.UserID, "error", err)
		observability.Warn(ctx, "chi crudconsumer: owner lookup", obsArgs...)
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
		obsArgs := append(observability.WithComponent("todo", "feature"), "op", p.Op)
		observability.Warn(ctx, "chi crudconsumer: unknown op", obsArgs...)
		return
	}
	if err != nil {
		if isSkip(err) {
			return // redelivery / empty payload: nothing to do, stay quiet.
		}
		obsArgs := append(observability.WithComponent("todo", "feature"), "op", p.Op, "error", err)
		observability.Warn(ctx, "chi crudconsumer: apply failed", obsArgs...)
		return
	}
	payload, merr := json.Marshal(evt)
	if merr != nil {
		obsArgs := append(observability.WithComponent("todo", "feature"), "error", merr)
		observability.Error(ctx, "chi crudconsumer: marshal fan-out", obsArgs...)
		return
	}
	hub.BroadcastToUser(payload, p.UserID, "")
}

// tenantOf resolves a wire user id to a real app identity. Unknown ids
// are a skip, never a mapping.
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
	now := dbschema.Now()
	id := data.ID
	if id == "" {
		id = dbschema.NewID()
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
		Completed: data.Completed, Updated: dbschema.Now(), ID: data.ID, Owner: owner,
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
