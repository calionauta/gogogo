// SCOPE:feature — the todo search seam's contract (rows, filter, scoping).
package handlers

import (
	"context"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/search"
)

// TestTodoSourceFiltersAndScopes pins the consumer half: layer 1 finds a
// middle substring that an exact match would miss, the in-memory layer gets
// this owner's rows to score, and neither ever sees another owner's title.
// Red-proof: drop the owner predicate from either query and the leak
// assertion fails.
func TestTodoSourceFiltersAndScopes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHandlersFixture(t)
	mine := f.ownerOf(t, f.signup("searcher@example.com"))
	theirs := f.ownerOf(t, f.signup("other@example.com"))
	create(t, f, mine, "Ship the smoke guard")
	create(t, f, mine, "Read the parser diff")
	create(t, f, theirs, "Smoke signals for another owner")

	src := NewTodoSource(f.q)

	hits, err := src.Substring(ctx, search.Query{Owner: mine, Text: "smoke", Field: SearchTitleField})
	if err != nil {
		t.Fatalf("substring: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want exactly my own matching row", hits)
	}

	docs, err := src.Candidates(ctx, search.Query{Owner: mine, Field: SearchTitleField})
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("candidates = %d rows, want my two", len(docs))
	}
	for _, d := range docs {
		if d.Fields[SearchTitleField] == "Smoke signals for another owner" {
			t.Error("another owner's title reached the matcher")
		}
	}
}

// create inserts one todo for an owner, the way the feature's own handlers do.
func create(t *testing.T, f *handlersFixture, owner, title string) {
	t.Helper()
	if _, err := f.q.CreateTodo(context.Background(), dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: title, Visibility: "private",
	}); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
}
