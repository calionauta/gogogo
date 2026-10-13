// SCOPE:feature — Native-window store over the flat data layer
//
// The window's claim is that the domain is transport-agnostic: the same
// todos the chi handlers read and write are reachable here with no HTTP,
// no cookie, no router. An EntityStore used to prove that over embedded
// records; the flat proof runs the same sqlc queries the chi slice runs
// (features/todo/handlers), against the same Turso file. The thin guiStore
// wrapper exists so windowState keeps the store-shaped seam its tests
// drive — copying the three queries beats importing the handler (the
// flat-deps gate forbids a features/todo/handlers → cmd/gui edge anyway).
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// guiDirPerm is the mode for the database parent dir created on first boot
// (mirrors internal/db: group/other read+execute for sidecar inspection,
// the file itself keeps the process umask).
const guiDirPerm = 0o755

// openGUIStore opens the app database file through the ncruces `sqlite3`
// driver (pure Go, zero CGO) with the same pragmas the web stack carries:
// busy_timeout + WAL + foreign_keys ride the DSN so every pooled
// connection gets them (docs/code-quality.md rule). It must stay on this
// driver: internal/db opens through tursogo, and importing that package
// links purego whose fakecgo collides with goffi's at CGO_ENABLED=0 link
// time (see cmd/gui/main.go).
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

// guiStore is the cmd/gui slice of the flat persistence layer: owner-scoped
// list, idempotent create, completed flip, delete.
type guiStore struct {
	q *dbgen.Queries
}

// List loads the owner's todos, filter "" (all), "active", "completed",
// newest first (the query orders; the filter is applied in Go, same as
// the chi slice).
func (s guiStore) List(ctx context.Context, owner, filter string) ([]todo.Todo, error) {
	rows, err := s.q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("gui: list: %w", err)
	}
	out := make([]todo.Todo, 0, len(rows))
	for _, row := range rows {
		item, terr := toTodo(row)
		if terr != nil {
			return nil, terr
		}
		switch filter {
		case "", "all":
			out = append(out, item)
		case "active":
			if !item.Completed {
				out = append(out, item)
			}
		case "completed":
			if item.Completed {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

// Create writes a new todo scoped to owner. idemKey is the offline-replay
// dedup token (this window is online-only, but the web path's create is
// idempotent on it and sharing the call keeps them identical): a replayed
// create returns the original row instead of a duplicate.
func (s guiStore) Create(ctx context.Context, title, owner, idemKey string) (todo.Todo, error) {
	existing, err := s.q.GetTodoByIdemKey(ctx, dbgen.GetTodoByIdemKeyParams{
		Owner: owner, IdemKey: idemKey,
	})
	switch {
	case err == nil:
		return toTodo(existing)
	case !errors.Is(err, sql.ErrNoRows):
		return todo.Todo{}, fmt.Errorf("gui: dedupe lookup: %w", err)
	}
	now := dbschema.Now()
	id := dbschema.NewID()
	if _, cerr := s.q.CreateTodo(ctx, dbgen.CreateTodoParams{
		ID: id, Owner: owner, Title: title,
		Completed: false, IdemKey: idemKey, Created: now, Updated: now,
	}); cerr != nil {
		return todo.Todo{}, fmt.Errorf("gui: create: %w", cerr)
	}
	row, gerr := s.q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: owner})
	if gerr != nil {
		return todo.Todo{}, fmt.Errorf("gui: read after create: %w", gerr)
	}
	return toTodo(row)
}

// SetCompleted flips one todo owned by owner. The caller supplies the
// target state (last-write-wins on two frontends racing the row, same
// contract the web tier has).
func (s guiStore) SetCompleted(ctx context.Context, owner, id string, completed bool) error {
	if _, err := s.q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: owner}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return fmt.Errorf("gui: toggle read: %w", err)
	}
	if err := s.q.SetTodoCompleted(ctx, dbgen.SetTodoCompletedParams{
		Completed: completed, Updated: dbschema.Now(), ID: id, Owner: owner,
	}); err != nil {
		return fmt.Errorf("gui: toggle write: %w", err)
	}
	return nil
}

// Delete removes one todo owned by owner. Idempotent on retry: a second
// delete on a missing id reports not-found.
func (s guiStore) Delete(ctx context.Context, owner, id string) error {
	if _, err := s.q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: owner}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return fmt.Errorf("gui: delete read: %w", err)
	}
	if err := s.q.DeleteTodo(ctx, dbgen.DeleteTodoParams{ID: id, Owner: owner}); err != nil {
		return fmt.Errorf("gui: delete: %w", err)
	}
	return nil
}

// errNotFound mirrors the store contract's not-found signal so callers
// can tell "row gone" from "row broken".
var errNotFound = errors.New("gui: not found")

// toTodo converts a sqlc row to the domain model. Copied from the chi
// slice (same conversion, same TimeFormat) rather than shared: the
// flat-deps gate keeps packages from reaching sideways for helpers.
func toTodo(row dbgen.Todo) (todo.Todo, error) {
	created, err := time.Parse(dbschema.TimeFormat, row.Created)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s created %q: %w", row.ID, row.Created, err)
	}
	updated, err := time.Parse(dbschema.TimeFormat, row.Updated)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s updated %q: %w", row.ID, row.Updated, err)
	}
	return todo.Todo{
		ID:        row.ID,
		Title:     row.Title,
		Completed: row.Completed,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}
