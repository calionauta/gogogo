// SCOPE:feature,removal=feature - Alt (sqlc + sqlite/tursogo compatible) todo repo adapter
package alt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	_ "github.com/tursodatabase/libsql-client-go/libsql"

	todomodel "github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/internal/ports"
	"github.com/calionauta/gogogo/features/todo/internal/repos/alt/sqlctodo"
)

type TodoRepoSQLC struct {
	q  *sqlctodo.Queries
	db *sql.DB
}

func NewTodoRepoSQLC(dbPath string) (*TodoRepoSQLC, error) {
	return NewTodoRepoSQLCWithConfig(dbPath, os.Getenv("TURSO_DATABASE_URL"), os.Getenv("TURSO_AUTH_TOKEN"))
}

func NewTodoRepoSQLCWithConfig(dbPath, dbURL, authToken string) (*TodoRepoSQLC, error) {
	var db *sql.DB
	var err error
	if dbURL != "" {
		// Use libsql (tursogo) - supports libsql:// URLs
		dsn := dbURL
		if authToken != "" {
			dsn = fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
		}
		db, err = sql.Open("libsql", dsn)
	} else {
		if dbPath == "" {
			dbPath = ":memory:"
		}
		db, err = sql.Open("sqlite3", dbPath)
	}
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &TodoRepoSQLC{q: sqlctodo.New(db), db: db}, nil
}

func (r *TodoRepoSQLC) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS todos (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  completed INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  owner_id TEXT NOT NULL
)`)
	return err
}

func (r *TodoRepoSQLC) List(ctx context.Context, ownerID, filter string) ([]todomodel.Todo, error) {
	var rows []sqlctodo.Todo
	var err error
	switch filter {
	case "active":
		rows, err = r.q.ListTodosByFilter(ctx, sqlctodo.ListTodosByFilterParams{
			OwnerID:   ownerID,
			Completed: 0,
		})
	case "completed":
		rows, err = r.q.ListTodosByFilter(ctx, sqlctodo.ListTodosByFilterParams{
			OwnerID:   ownerID,
			Completed: 1,
		})
	case "all", "":
		rows, err = r.q.ListTodos(ctx, ownerID)
	default:
		rows, err = r.q.ListTodos(ctx, ownerID)
	}
	if err != nil {
		return nil, err
	}
	res := make([]todomodel.Todo, len(rows))
	for i, row := range rows {
		res[i] = todomodel.Todo{
			ID:        row.ID,
			Title:     row.Title,
			Completed: row.Completed != 0,
		}
	}
	return res, nil
}

func (r *TodoRepoSQLC) Create(ctx context.Context, t todomodel.Todo, ownerID, idemKey string) (todomodel.Todo, error) {
	id := fmt.Sprintf("todo-%s-%s", idemKey, t.Title)
	now := time.Now().UTC().Format(time.RFC3339)
	err := r.q.CreateTodo(ctx, sqlctodo.CreateTodoParams{
		ID:        id,
		Title:     t.Title,
		Completed: 0,
		CreatedAt: now,
		UpdatedAt: now,
		OwnerID:   ownerID,
	})
	if err != nil {
		return todomodel.Todo{}, err
	}
	t.ID = id
	return t, nil
}

func (r *TodoRepoSQLC) Get(ctx context.Context, ownerID, id string) (todomodel.Todo, error) {
	row, err := r.q.GetTodo(ctx, sqlctodo.GetTodoParams{ID: id, OwnerID: ownerID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return todomodel.Todo{}, errors.New("not found")
		}
		return todomodel.Todo{}, err
	}
	return todomodel.Todo{ID: row.ID, Title: row.Title, Completed: row.Completed != 0}, nil
}

func (r *TodoRepoSQLC) Update(ctx context.Context, ownerID, id string, patch map[string]any) (todomodel.Todo, error) {
	row, err := r.q.GetTodo(ctx, sqlctodo.GetTodoParams{ID: id, OwnerID: ownerID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return todomodel.Todo{}, errors.New("not found")
		}
		return todomodel.Todo{}, err
	}
	completed := row.Completed
	title := row.Title
	if v, ok := patch["completed"]; ok {
		if b, ok := v.(bool); ok && b {
			completed = 1
		} else if ok && !b {
			completed = 0
		}
	}
	if v, ok := patch["title"]; ok {
		if s, ok := v.(string); ok {
			title = s
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	err = r.q.UpdateTodo(ctx, sqlctodo.UpdateTodoParams{
		ID:        id,
		OwnerID:   ownerID,
		Title:     title,
		Completed: completed,
		UpdatedAt: now,
	})
	if err != nil {
		return todomodel.Todo{}, err
	}
	return todomodel.Todo{ID: id, Title: title, Completed: completed != 0}, nil
}

func (r *TodoRepoSQLC) Delete(ctx context.Context, ownerID, id string) error {
	return r.q.DeleteTodo(ctx, sqlctodo.DeleteTodoParams{ID: id, OwnerID: ownerID})
}

func (r *TodoRepoSQLC) ClearCompleted(ctx context.Context, ownerID string) (int, error) {
	res, err := r.q.ClearCompleted(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	return int(res), nil
}

func (r *TodoRepoSQLC) Count(ctx context.Context, ownerID string) (int, error) {
	n, err := r.q.CountTodos(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

var _ ports.TodoRepo = (*TodoRepoSQLC)(nil)
