// SCOPE:feature — the todo feature's half of the search seam: its own rows,
// and the cheap SQL filter over them. To remove: delete with features/todo/.
package handlers

import (
	"context"
	"database/sql"
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

// Candidates returns the rows the viewer may see (own + public) for the
// in-memory layer to score. The query itself is the visibility scope, so no
// layer above can widen it.
func (s *TodoSource) Candidates(ctx context.Context, q search.Query) ([]search.Doc, error) {
	rows, err := s.Q.ListVisibleTodos(ctx, q.Owner)
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
	rows, err := s.Q.SearchTodosVisible(ctx, dbgen.SearchTodosVisibleParams{
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

// TodoSearcher adapts the plugin to the handler's Searcher seam: it runs one
// already-built layer and reduces each Hit to the id the list needs. The
// handler knows nothing of modes or tiers, and the plugin knows nothing of
// HTTP — the two meet at the ranked id list.
type TodoSearcher struct {
	S search.Searcher
}

// NewTodoSearcher wraps a layer. Nil is a wiring bug, not a degradation.
func NewTodoSearcher(s search.Searcher) *TodoSearcher {
	if s == nil {
		panic("todo: NewTodoSearcher with nil searcher")
	}
	return &TodoSearcher{S: s}
}

// SearchIDs runs the layer and returns ranked ids, best first. Ranking is
// preserved (the list reorders itself to match), and the owner scope comes
// from the plugin's own query — a search can only ever narrow what the
// caller was already allowed to see.
func (t *TodoSearcher) SearchIDs(ctx context.Context, owner, query string) ([]string, error) {
	hits, err := t.S.Search(ctx, search.Query{
		Owner: owner, Text: query, Field: SearchTitleField, Limit: defaultSearchLimit,
		IncludePublic: true,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	return ids, nil
}

// TodoIndexer adapts the plugin's postings index to the handler's write
// seam. It only ever exists in bm25 mode: the substring and fuzzy layers add
// no index and no write path, so their mode wires no indexer and the
// mutations keep their plain autocommit path.
type TodoIndexer struct {
	IX search.SQLIndex
}

// NewTodoIndexer wraps the plugin's postings index for the handler's write
// seam. A zero-value index (nil DB) is a wiring bug: the handler only ever
// builds one in bm25 mode, where the pool is necessarily present.
func NewTodoIndexer(ix search.SQLIndex) TodoIndexer { return TodoIndexer{IX: ix} }

// IndexTodo writes (replaces) a row's postings inside the caller's
// transaction, so the row and its index entry commit or roll back together.
func (i TodoIndexer) IndexTodo(ctx context.Context, tx *sql.Tx, owner, docID, title, visibility string) error {
	return i.IX.IndexDoc(ctx, tx, owner, searchDocKind, docID, SearchTitleField, title, visibility)
}

// DeleteTodo drops a row's postings inside the caller's transaction.
func (i TodoIndexer) DeleteTodo(ctx context.Context, tx *sql.Tx, owner, docID string) error {
	return i.IX.DeleteDoc(ctx, tx, owner, searchDocKind, docID)
}

// searchDocKind is the `kind` column value for todo rows: one spelling, so
// the index and any future consumer sharing the table cannot disagree.
const searchDocKind = "todo"
