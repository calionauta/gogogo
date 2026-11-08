// Transport-free todo domain service — one rule, every frontend.
package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// --- list -----------------------------------------------------------------

// ListVisible loads own + public rows, applies filter/scope in Go, and
// stamps IsMine/CanDelete per row. Shared by HTTP rendering and the native
// window; unknown filter/scope read as "all".
func (s *Service) ListVisible(ctx context.Context, owner, role, filter, scope string) ([]todo.Todo, error) {
	rows, err := s.Q.ListVisibleTodos(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]todo.Todo, 0, len(rows))
	for _, row := range rows {
		item, err := ToVisibleTodo(row)
		if err != nil {
			return nil, err
		}
		if owner != "" {
			switch scope {
			case "mine":
				if item.Owner != owner {
					continue
				}
			case "shared":
				if item.Owner == owner || !item.IsShared() {
					continue
				}
			}
		}
		switch filter {
		case "active":
			if item.Completed {
				continue
			}
		case "completed":
			if !item.Completed {
				continue
			}
		}
		item.IsMine = owner != "" && item.Owner == owner
		item.CanDelete = CanDeleteRow(role, owner, item)
		out = append(out, item)
	}
	return out, nil
}

// ListOwned is the owner-scoped read for frontends that are always a
// single owner (cmd/gui). Same ordering/filtering as the web list for a
// given filter, without visibility join semantics.
func (s *Service) ListOwned(ctx context.Context, owner, filter string) ([]todo.Todo, error) {
	rows, err := s.Q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]todo.Todo, 0, len(rows))
	for _, row := range rows {
		item, err := ToTodo(row)
		if err != nil {
			return nil, err
		}
		switch filter {
		case "", "all":
		case "active":
			if item.Completed {
				continue
			}
		case "completed":
			if !item.Completed {
				continue
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// --- mutations --------------------------------------------------------------

// Create validates, dedupes by idemKey, inserts (indexed), resumes
// onboarding and publishes Created before any replay short-circuit, so
// live subscribers learn of the fact even when the caller answers
// "replayed". Returns the row id; a replay returns the original id.
func (s *Service) Create(ctx context.Context, owner, title, visibility, idemKey string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ErrTitleRequired
	}
	visibility = normalizeVisibility(visibility)
	if idemKey != "" && !ValidIdemKey(idemKey) {
		return "", ErrInvalidIdemKey
	}

	createdID := ""
	if idemKey != "" {
		dedupe, derr := s.Q.GetTodoByIdemKey(ctx, dbgen.GetTodoByIdemKeyParams{Owner: owner, IdemKey: idemKey})
		switch {
		case derr == nil:
			createdID = dedupe.ID
		case !errors.Is(derr, sql.ErrNoRows):
			return "", fmt.Errorf("todo: dedupe lookup: %w", derr)
		}
	}
	if createdID == "" {
		id, err := s.insertNew(ctx, owner, title, visibility, idemKey)
		if err != nil {
			return "", err
		}
		createdID = id
	}

	if s.Onboarding != nil {
		s.Onboarding.ResumeOnboarding(owner)
	}
	if s.Bus != nil {
		s.Bus.Publish(ctx, todo.Event{Kind: todo.Created, ID: createdID, Title: title, Owner: owner, Visibility: visibility})
	}
	return createdID, nil
}

func normalizeVisibility(v string) string {
	if v == "" {
		return todo.VisibilityPrivate
	}
	if v != todo.VisibilityPublic && v != todo.VisibilityPrivate {
		return todo.VisibilityPrivate
	}
	return v
}

func (s *Service) insertNew(ctx context.Context, owner, title, visibility, idemKey string) (string, error) {
	now := dbschema.Now()
	newID := dbschema.NewID()
	err := s.createIndexed(ctx, dbgen.CreateTodoParams{
		ID: newID, Owner: owner, Title: title,
		Completed: false, IdemKey: idemKey, Visibility: visibility,
		Created: now, Updated: now,
	})
	if err != nil && idemKey != "" {
		params := dbgen.GetTodoByIdemKeyParams{Owner: owner, IdemKey: idemKey}
		if existing, rerr := s.Q.GetTodoByIdemKey(ctx, params); rerr == nil {
			return existing.ID, nil
		}
		return "", fmt.Errorf("todo: save: %w", err)
	}
	if err != nil {
		return "", fmt.Errorf("todo: save: %w", err)
	}
	return newID, nil
}

// Toggle flips the row from a FRESH store read (never a frontend
// snapshot), exactly-once when opKey is set. Returns the pre-toggle item
// and whether this delivery applied (false = replay, do not re-patch).
func (s *Service) Toggle(ctx context.Context, owner, id, opKey string) (todo.Todo, bool, error) {
	row, err := s.Q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return todo.Todo{}, false, ErrNotFound
		}
		return todo.Todo{}, false, fmt.Errorf("todo: toggle read id %s: %w", id, err)
	}
	current, err := ToVisibleOne(row)
	if err != nil {
		return todo.Todo{}, false, fmt.Errorf("todo: toggle map id %s: %w", id, err)
	}
	now := dbschema.Now()
	applied, serr := s.applyOnce(ctx, owner, opKey, func(ctx context.Context, q *dbgen.Queries) error {
		return q.SetTodoCompletedByID(ctx, dbgen.SetTodoCompletedByIDParams{
			Completed: !current.Completed, Updated: now, ID: id,
		})
	})
	if serr != nil {
		return todo.Todo{}, false, fmt.Errorf("todo: toggle save id %s: %w", id, serr)
	}
	if !applied {
		return current, false, nil
	}
	visibility := todo.VisibilityPrivate
	if current.IsShared() {
		visibility = todo.VisibilityPublic
	}
	if s.Bus != nil {
		ev := todo.Event{
			Kind: todo.Updated, ID: id, Done: !current.Completed,
			Owner: owner, Visibility: visibility,
		}
		s.Bus.Publish(ctx, ev)
	}
	return current, true, nil
}

// Delete removes a visible row the requester may delete. Mirrors the HTTP
// rule: viewer never deletes (even own row is allowed only for non-viewer
// own rows via CanDeleteRow, which the HTTP layer already enforces before
// calling — the service enforces it again so the native frontend cannot
// bypass it).
func (s *Service) Delete(ctx context.Context, owner, role, id string) (todo.Todo, error) {
	row, err := s.Q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return todo.Todo{}, ErrNotFound
		}
		return todo.Todo{}, fmt.Errorf("todo: delete read id %s: %w", id, err)
	}
	item, err := ToVisibleOne(row)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo: delete map id %s: %w", id, err)
	}
	if !CanDeleteRow(role, owner, item) {
		return todo.Todo{}, ErrForbidden
	}
	if derr := s.deleteIndexed(ctx, owner, item.ID); derr != nil {
		return todo.Todo{}, fmt.Errorf("todo: delete id %s: %w", id, derr)
	}
	visibility := todo.VisibilityPrivate
	if item.IsShared() {
		visibility = todo.VisibilityPublic
	}
	if s.Bus != nil {
		s.Bus.Publish(ctx, todo.Event{Kind: todo.Deleted, ID: item.ID, Owner: owner, Visibility: visibility})
	}
	return item, nil
}

// ClearCompleted deletes the owner's completed rows (indexed) and
// publishes Cleared. Audience stays owner-scoped.
func (s *Service) ClearCompleted(ctx context.Context, owner string) (int64, error) {
	count, err := s.clearIndexed(ctx, owner)
	if err != nil {
		return 0, fmt.Errorf("todo: clear: %w", err)
	}
	if s.Bus != nil {
		s.Bus.Publish(ctx, todo.Event{Kind: todo.Cleared, Owner: owner, Visibility: todo.VisibilityPrivate})
	}
	return count, nil
}

// CreateForOnboarding is the worker path (DagNats): no idempotency key,
// no publish, no onboarding resume (the worker itself is the flow).
func (s *Service) CreateForOnboarding(ctx context.Context, title, owner string) error {
	if owner == "" {
		return errors.New("todo: onboarding create requires an owner")
	}
	now := dbschema.Now()
	if err := s.createIndexed(ctx, dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: title,
		Completed: false, IdemKey: "", Visibility: todo.VisibilityPrivate, Created: now, Updated: now,
	}); err != nil {
		return fmt.Errorf("onboarding create %q: %w", title, err)
	}
	return nil
}

// --- exactly-once -----------------------------------------------------------

const maxOpKeyLen = 128

func (s *Service) applyOnce(
	ctx context.Context, owner, key string,
	work func(context.Context, *dbgen.Queries) error,
) (bool, error) {
	if key == "" || s.DB == nil {
		return true, work(ctx, s.Q)
	}
	err := dbschema.Transact(ctx, s.DB, "todo op receipt", func(ctx context.Context, tx *sql.Tx) error {
		q := s.Q.WithTx(tx)
		claimed, cerr := q.ClaimOpReceipt(ctx, dbgen.ClaimOpReceiptParams{
			Owner: owner, OpKey: key, Created: dbschema.Now(),
		})
		if cerr != nil {
			return cerr
		}
		if claimed == 0 {
			return errOpAlreadyApplied
		}
		return work(ctx, q)
	})
	if errors.Is(err, errOpAlreadyApplied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// HashOpKey mirrors handlers.idempotencyKey's hashing rule for callers
// (HTTP layer) that pre-hash over-long tokens; kept here so a future
// native outbox shares the rule.
func HashOpKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= maxOpKeyLen {
		return key
	}
	// sha256 + hex, same shaping as the outbox key rule.
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}
