// SCOPE:core — the one place the search plugin's three layers meet the
// composition root. Split from chi.go for the function-length and
// file-size gates, mirroring bootOnboarding/bootReview.
//
// The plugin is removable: this file plus the two files the search installer
// unit lists (features/todo/handlers/search_source.go and search.go) are the
// whole seam. Trimming rebuilds the handler with a nil Searcher, and every
// caller degrades to the plain list the template already renders.
package server

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/calionauta/gogogo/config"
	todohandlers "github.com/calionauta/gogogo/features/todo/handlers"
	"github.com/calionauta/gogogo/internal/search"
)

// bootSearch builds the configured layer and wires both halves onto the todo
// handler. Every mode needs the consumer's Source — even substring, whose SQL
// lives in the consumer (the plugin supplies the seam, not the query). Only
// bm25 needs the postings index, so only bm25 wires a write path; the
// substring default costs no index and no extra write on the mutation path.
//
// An error here is a config/plugin disagreement about the valid mode names,
// the one bug that must not boot into silently different search behaviour:
// config's normalizer already maps a typo onto the substring default, so the
// plugin only ever sees a name it knows.
func bootSearch(cfg *config.Config, todoH *todohandlers.Handler, sqlDB *sql.DB) error {
	mode := cfg.Search.Mode
	src := todohandlers.NewTodoSource(todoH.Q)

	// The index is a bm25-only cost: a table to read and a write on every
	// mutation. Every other mode leaves it nil, which is what makes the
	// substring default free.
	var idx search.SQLIndex
	var ix todohandlers.SearchIndexer
	if mode == config.SearchModeBM25 {
		idx = search.SQLIndex{DB: sqlDB}
		ix = todohandlers.NewTodoIndexer(idx)
	}

	layer, err := search.New(mode, src, idx)
	if err != nil {
		return fmt.Errorf("server: search layer %q: %w", mode, err)
	}
	todoH.SetSearch(todohandlers.NewTodoSearcher(layer), ix, sqlDB)
	slog.Info("server: todo search wired", "mode", mode, "indexed", ix != nil)
	return nil
}
