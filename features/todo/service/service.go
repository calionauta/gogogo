// Transport-free todo domain service — one rule, every frontend.
//
// The chi handlers (HTTP + Datastar SSE) and cmd/gui (gogpu/ui native
// window) must never re-implement Create/Toggle/Delete/List: idempotent
// create, indexed writes, onboarding resume, Bus fan-out (Created/Updated/
// Deleted/Cleared), exactly-once toggle via op receipts, and the
// canDeleteRow rule live here exactly once. Adapters own transport only:
// handlers parse forms, check sessions, patch SSE; the native window owns
// pixels. A mutation through either frontend now triggers the same
// onboarding hook and the same Bus event, instead of the native add()
// silently skipping both.
//
// Flat-deps note: this package lives inside features/todo (same feature),
// so both handlers and cmd/gui may import it without a cross-feature edge.
// To remove: delete this directory with features/todo/.
package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// Indexer is the search-postings write half. Identical shape to
// handlers.SearchIndexer, declared here so the service does not import
// handlers (and handlers may import the service).
type Indexer interface {
	IndexTodo(ctx context.Context, tx *sql.Tx, owner, docID, title, visibility string) error
	DeleteTodo(ctx context.Context, tx *sql.Tx, owner, docID string) error
}

// OnboardingResumer resumes the durable onboarding workflow when a user
// with a pending run creates their first todo. Nil disables the hook.
type OnboardingResumer interface {
	ResumeOnboarding(owner string)
}

var (
	ErrTitleRequired    = errors.New("todo: title required")
	ErrInvalidIdemKey   = errors.New("todo: invalid idem_key")
	ErrNotFound         = errors.New("todo: not found")
	ErrForbidden        = errors.New("todo: cannot delete this todo")
	errOpAlreadyApplied = errors.New("todo: operation already applied")
)

// Service is the single write/read path for todo facts.
// Zero Bus/Onboarding/Index means the side effect is skipped, never
// "feature missing fails the mutation".
type Service struct {
	Q          *dbgen.Queries
	Index      Indexer
	DB         *sql.DB
	Bus        *todo.Bus
	Onboarding OnboardingResumer
}

func New(q *dbgen.Queries, ix Indexer, db *sql.DB, bus *todo.Bus, onboarding OnboardingResumer) *Service {
	return &Service{Q: q, Index: ix, DB: db, Bus: bus, Onboarding: onboarding}
}
