// Native-window store over the flat domain service.
//
// guiStore is now a thin adapter over features/todo/service (the same
// service the chi handlers call): list/create/toggle/delete go through
// the transport-free domain rules — idempotent create, fresh-read
// toggle, canDeleteRow, onboarding resume, Bus publish — instead of
// re-implementing the sqlc queries here. Notifications and docs reads
// stay local (they are not yet in the service); everything else shares
// the single implementation. The flat-deps note in the old header no
// longer applies: this package imports service, not handlers.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/service"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

const guiDirPerm = 0o755

func openGUIStore(cfg *config.Config) (*sql.DB, error) {
	path := cfg.Turso.DBPath
	if path == "" {
		path = filepath.Join(cfg.DataDir, "app.db")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, guiDirPerm); err != nil {
			return nil, fmt.Errorf("gui: create data dir %q: %w", dir, err)
		}
	}
	dsn := "file:" + path +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(ON)"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("gui: open %q: %w", path, err)
	}
	return sqlDB, nil
}

// guiStore adapts service.Service to the shape windowState tests drive.
// The service owns domain rules; this owns only the frontend's argument
// shaping (owner-scoped, single-owner role "" for self rows).
type guiStore struct {
	svc *service.Service
	q   *dbgen.Queries
	db  *sql.DB
}

func newGUIStore(q *dbgen.Queries, db *sql.DB, bus *todo.Bus, onboarding service.OnboardingResumer) guiStore {
	return guiStore{svc: service.New(q, nil, db, bus, onboarding), q: q, db: db}
}

func (s guiStore) svcOr() *service.Service {
	if s.svc != nil {
		return s.svc
	}
	return service.New(s.q, nil, s.db, nil, nil)
}

// List loads the owner's todos via the domain service.
func (s guiStore) List(ctx context.Context, owner, filter string) ([]todo.Todo, error) {
	items, err := s.svcOr().ListOwned(ctx, owner, filter)
	if err != nil {
		return nil, fmt.Errorf("gui: list: %w", err)
	}
	return items, nil
}

// Create writes a new todo via the domain service: idempotent, indexed,
// onboarding-resumed and Bus-published, exactly like the web path.
func (s guiStore) Create(ctx context.Context, title, owner, idemKey string) (todo.Todo, error) {
	id, err := s.svcOr().Create(ctx, owner, title, todo.VisibilityPrivate, idemKey)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("gui: create: %w", err)
	}
	row, gerr := s.q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: owner})
	if gerr != nil {
		return todo.Todo{}, fmt.Errorf("gui: read after create: %w", gerr)
	}
	return toTodo(row)
}

// SetCompleted flips one todo. The service reads fresh from the store
// (same as web) — the old GUI snapshot base is gone, so two frontends
// racing a row are last-write-wins on the current DB value, not on a
// stale local copy.
func (s guiStore) SetCompleted(ctx context.Context, owner, id string, completed bool) error {
	// Service.Toggle flips from a fresh read; we want "set to completed"
	// not "flip". Read current, then toggle only if it differs by using
	// the service flip path: fetch current, then flip matches desired.
	row, err := s.q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return fmt.Errorf("gui: toggle read: %w", err)
	}
	cur, err := service.ToVisibleOne(row)
	if err != nil {
		return err
	}
	if cur.Completed == completed {
		return nil
	}
	_, _, err = s.svcOr().Toggle(ctx, owner, id, "")
	return err
}

// Delete removes one todo via the domain service (role "" = owner self;
// a non-owner viewer's own-row rule is enforced by CanDeleteRow).
func (s guiStore) Delete(ctx context.Context, owner, id string) error {
	_, err := s.svcOr().Delete(ctx, owner, "", id)
	if errors.Is(err, service.ErrNotFound) {
		return errNotFound
	}
	if err != nil {
		return fmt.Errorf("gui: delete: %w", err)
	}
	return nil
}

// notifItem is the inbox slice the native window renders. Read-only:
// marking read is a web action in v1 (the same row the inbox stream
// patches); the native twin answers "what is waiting for me".
type notifItem struct {
	ID      string
	Subject string
	Body    string
	Created string
	Unread  bool
}

// ListNotifications loads the owner's in_app inbox rows, newest first —
// the same query and limit the web page renders.
func (s guiStore) ListNotifications(ctx context.Context, owner string) ([]notifItem, int64, error) {
	rows, err := s.q.ListNotificationsForOwner(ctx, dbgen.ListNotificationsForOwnerParams{
		Owner: owner, Limit: 50,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("gui: list notifications: %w", err)
	}
	unread, err := s.q.CountUnreadInApp(ctx, owner)
	if err != nil {
		return nil, 0, fmt.Errorf("gui: count unread: %w", err)
	}
	out := make([]notifItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, notifItem{
			ID: row.ID, Subject: row.Subject, Body: row.Body,
			Created: row.Created, Unread: !row.ReadAt.Valid,
		})
	}
	return out, unread, nil
}

// docItem is one shared collaborative doc (note or board) the native
// twin lists.
type docItem struct {
	ID     string
	Author string
	Title  string
}

// ListDocs loads shared notes or boards. kind is "notes" or "boards".
func (s guiStore) ListDocs(ctx context.Context, kind string) ([]docItem, error) {
	switch kind {
	case "notes":
		rows, err := s.q.ListNotes(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]docItem, 0, len(rows))
		for _, r := range rows {
			out = append(out, docItem{ID: r.DocID, Author: r.OwnerEmail, Title: "note"})
		}
		return out, nil
	case "boards":
		rows, err := s.q.ListBoards(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]docItem, 0, len(rows))
		for _, r := range rows {
			out = append(out, docItem{ID: r.DocID, Author: r.OwnerEmail, Title: "board"})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("gui: unknown doc kind %q", kind)
	}
}

// CreateDoc writes a new shared doc metadata row (note or board).
// Content (Loro doc) starts empty on the web; the native twin owns
// creation, the web owns editing — the same split the inbox uses.
func (s guiStore) CreateDoc(ctx context.Context, kind, owner string) (docItem, error) {
	now := dbschema.Now()
	id := dbschema.NewID()
	params := dbgen.UpsertNoteParams{DocID: id, Owner: owner, Updated: now, Created: now}
	switch kind {
	case "notes":
		if err := s.q.UpsertNote(ctx, params); err != nil {
			return docItem{}, fmt.Errorf("gui: create note: %w", err)
		}
	case "boards":
		if err := s.q.UpsertBoard(ctx, dbgen.UpsertBoardParams{
			DocID: id, Owner: owner, Updated: now, Created: now,
		}); err != nil {
			return docItem{}, fmt.Errorf("gui: create board: %w", err)
		}
	default:
		return docItem{}, fmt.Errorf("gui: unknown doc kind %q", kind)
	}
	return docItem{ID: id, Author: "", Title: kind}, nil
}

// errNotFound mirrors the store contract's not-found signal so callers
// can tell "row gone" from "row broken".
var errNotFound = errors.New("gui: not found")

// toTodo converts a sqlc row to the domain model via the domain service.
func toTodo(row dbgen.ExtTodoTodo) (todo.Todo, error) {
	return service.ToTodo(row)
}
