// SCOPE:feature — todo NATS bridge (cross-instance convergence)
//
// RunCrudConsumer subscribes to the app.crud.todo.> subjects, applies each
// operation to Turso, and fans the record event out to local chi streams.
// Its durable is per app AND per instance: two instances of the same app
// need independent cursors, or only the instance that binds first converges.
//
// Identity boundary: payload UserIDs are strings resolved against app
// users; anything else is skipped with a warning — faking a mapping would
// corrupt tenant scoping. Convergence is complete within one identity
// model; foreign identities never apply.
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// RunCrudConsumer subscribes and blocks until ctx is cancelled. Nil js
// (NATS disabled) is a no-op returning nil — the chi stack then converges
// in-process through the Hub only.
//
// instanceID names the durable: one consumer PER INSTANCE, so every
// instance applies every remote op to its own database. A durable shared
// by two instances is rejected by the server ("consumer is already bound")
// and the losing instance silently stops converging.
func RunCrudConsumer(
	ctx context.Context, js natsio.JetStreamContext, appName, instanceID string,
	q *dbgen.Queries, ix SearchIndexer, db *sql.DB, hub *queue.SSEHub,
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
		// Our own publication: the local DB already has the row and the
		// handler already fanned out to local streams. Applying it again is
		// idempotent, but broadcasting would deliver a SECOND record event
		// to every local tab (measured as a double re-list per mutation).
		// Empty Origin is a pre-field publisher: treat as remote.
		if p.Origin != "" && p.Origin == instanceID {
			_ = msg.Ack()
			return
		}
		applyCrudOp(ctx, q, ix, db, hub, p)
		_ = msg.Ack()
	}, natsio.Durable(natswire.CrudInstanceDurable(appName, instanceID)),
		natsio.ManualAck(),
		// A rolled-out instance never comes back under the same name; the
		// inactive durable would otherwise linger forever.
		natsio.InactiveThreshold(time.Hour))
	if err != nil {
		return err
	}
	<-ctx.Done()
	return sub.Unsubscribe()
}

// applyCrudOp applies one wire operation to Turso and fans out locally.
// Every branch is idempotent (redeliveries converge, never duplicate).
// Reads are visibility-scoped (a forged op for a row the sender cannot see
// matches nothing); writes land by id only after that read proved the row
// visible — the origin instance already authorized the act.
func applyCrudOp(
	ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	hub *queue.SSEHub, p natswire.CrudPayload,
) {
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
	var shared bool
	switch p.Op {
	case natswire.CrudOpCreate:
		evt, shared, err = applyCreate(ctx, q, ix, db, owner, p.Data)
	case natswire.CrudOpToggle:
		evt, shared, err = applyToggle(ctx, q, owner, p.Data)
	case natswire.CrudOpDelete:
		evt, shared, err = applyDelete(ctx, q, ix, db, owner, p.Data)
	case natswire.CrudOpClearCompleted:
		evt, err = applyClearCompleted(ctx, q, ix, db, owner)
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
	// Shared rows refresh every login's streams (each re-lists its own
	// visible set); private rows reach only the owner's tabs.
	if shared {
		hub.BroadcastToAllUsers(payload, "")
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

func applyCreate(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB, owner string,
	data *natswire.CrudOpData,
) (recordEvent, bool, error) {
	if data.Title == "" {
		return recordEvent{}, false, errSkip
	}
	if _, err := q.GetTodo(ctx, dbgen.GetTodoParams{ID: data.ID, Owner: owner}); err == nil {
		return recordEvent{}, false, errSkip // redelivery: row already stands.
	} else if !errors.Is(err, sql.ErrNoRows) {
		return recordEvent{}, false, fmt.Errorf("create lookup %s: %w", data.ID, err)
	}
	// Wire-compat: pre-visibility publishers send no flag — private default
	// preserves the old listing behaviour for them.
	visibility := data.Visibility
	if visibility != todo.VisibilityPublic {
		visibility = todo.VisibilityPrivate
	}
	now := dbschema.Now()
	id := data.ID
	if id == "" {
		id = dbschema.NewID()
	}
	if err := createTodoIndexed(ctx, q, ix, db, dbgen.CreateTodoParams{
		ID: id, Owner: owner, Title: data.Title,
		Completed: data.Completed, IdemKey: "", Visibility: visibility,
		Created: now, Updated: now,
	}); err != nil {
		return recordEvent{}, false, fmt.Errorf("create apply %s: %w", id, err)
	}
	return recordEvent{Event: "created", ID: id, Title: data.Title}, visibility == todo.VisibilityPublic, nil
}

func applyToggle(ctx context.Context, q *dbgen.Queries, owner string,
	data *natswire.CrudOpData,
) (recordEvent, bool, error) {
	if data.ID == "" {
		return recordEvent{}, false, errSkip
	}
	// Visibility-scoped read first: the row must be the sender's own or
	// shared. The write then lands by id — a public row owned by someone
	// else would match nothing under an owner guard.
	row, err := q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: data.ID, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return recordEvent{}, false, errSkip
		}
		return recordEvent{}, false, fmt.Errorf("toggle lookup %s: %w", data.ID, err)
	}
	// Absolute post-state, matching what publishers send (see handleToggle:
	// Done is the state after the local write).
	if err := q.SetTodoCompletedByID(ctx, dbgen.SetTodoCompletedByIDParams{
		Completed: data.Completed, Updated: dbschema.Now(), ID: data.ID,
	}); err != nil {
		return recordEvent{}, false, fmt.Errorf("toggle apply %s: %w", data.ID, err)
	}
	return recordEvent{Event: "toggled", ID: data.ID, Done: data.Completed}, row.Visibility == todo.VisibilityPublic, nil
}

func applyDelete(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB, owner string,
	data *natswire.CrudOpData,
) (recordEvent, bool, error) {
	if data.ID == "" {
		return recordEvent{}, false, errSkip
	}
	// Same visibility-scoped read: only a visible row deletes (the origin
	// instance already enforced viewer-never/owner-or-admin).
	row, err := q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: data.ID, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return recordEvent{}, false, errSkip
		}
		return recordEvent{}, false, fmt.Errorf("delete lookup %s: %w", data.ID, err)
	}
	if err := deleteTodoIndexed(ctx, q, ix, db, owner, data.ID); err != nil {
		return recordEvent{}, false, fmt.Errorf("delete apply %s: %w", data.ID, err)
	}
	return recordEvent{Event: "deleted", ID: data.ID}, row.Visibility == todo.VisibilityPublic, nil
}

func applyClearCompleted(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	owner string,
) (recordEvent, error) {
	if _, err := clearCompletedIndexed(ctx, q, ix, db, owner); err != nil {
		return recordEvent{}, fmt.Errorf("clear apply: %w", err)
	}
	return recordEvent{Event: "cleared"}, nil
}

// errSkip marks "nothing to do" separately from failure: callers stay quiet
// on skips (redelivery, empty payload) and warn on errors.
var errSkip = errors.New("chi crudconsumer: skip")

// isSkip reports whether apply returned a skip (not a failure).
func isSkip(err error) bool { return errors.Is(err, errSkip) }
