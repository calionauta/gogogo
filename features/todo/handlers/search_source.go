// SCOPE:feature — the todo feature's half of the search seam: its own rows,
// and the cheap SQL filter over them. To remove: delete with features/todo/.
package handlers

import (
	"context"
	"fmt"
	"strings"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/search"
)

// SearchTitleField is the field name both halves spell the same way.
const SearchTitleField = "title"

// TodoSource implements search.Source: it owns the todo table, so the plugin
// never learns where a todo lives and this package never learns how postings
// are stored. The two meet at the field name.
type TodoSource struct {
	Q *dbgen.Queries
}

// NewTodoSource builds the seam. Nil queries is a wiring bug, not a
// degradation: the handler that owns them panics on nil too.
func NewTodoSource(q *dbgen.Queries) *TodoSource {
	if q == nil {
		panic("todo: NewTodoSource with nil queries")
	}
	return &TodoSource{Q: q}
}

// Candidates returns the owner's rows for the in-memory layer to score.
// Owner-scoped by the query itself, so no layer above can widen the scope it
// was handed.
func (s *TodoSource) Candidates(ctx context.Context, q search.Query) ([]search.Doc, error) {
	rows, err := s.Q.ListTodosByOwner(ctx, q.Owner)
	if err != nil {
		return nil, fmt.Errorf("todo: search candidates: %w", err)
	}
	out := make([]search.Doc, 0, len(rows))
	for _, r := range rows {
		out = append(out, search.Doc{ID: r.ID, Fields: map[string]string{SearchTitleField: r.Title}})
	}
	return out, nil
}

// Substring is layer 1: the SQL filter, using the index the todo list already
// has. LIKE metacharacters in the user's own text are blanked rather than
// escaped (the statement carries no ESCAPE clause): a literal '%' typed into
// a search box should match nothing here, not everything, and the two
// smarter layers treat it as text anyway.
func (s *TodoSource) Substring(ctx context.Context, q search.Query) ([]search.Hit, error) {
	rows, err := s.Q.SearchTodosByOwner(ctx, dbgen.SearchTodosByOwnerParams{
		Owner: q.Owner, LOWER: "%" + blankLikeMeta(q.Text) + "%", Limit: int64(limitOr(q.Limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("todo: substring search: %w", err)
	}
	out := make([]search.Hit, 0, len(rows))
	for _, r := range rows {
		out = append(out, search.Hit{ID: r.ID, Score: 1})
	}
	return out, nil
}

// blankLikeMeta removes LIKE wildcards from a user's text.
func blankLikeMeta(text string) string {
	return strings.NewReplacer("%", " ", "_", " ").Replace(strings.ToLower(text))
}

// defaultSearchLimit is what an unset limit means: enough rows to fill the
// list a person is scanning, small enough that layer 1 never reads a table
// to answer one keystroke.
const defaultSearchLimit = 50

// limitOr turns an unset limit into something a SQL LIMIT accepts.
func limitOr(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	return limit
}
