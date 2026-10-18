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
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/calionauta/gogogo/config"
	todohandlers "github.com/calionauta/gogogo/features/todo/handlers"
	"github.com/calionauta/gogogo/internal/search"
)

// bootSearch wires the search plugin: every layer this build can serve, plus the
// index heal that makes them interchangeable.
//
// ALL THREE LAYERS ARE ALWAYS WIRED. The mode is a runtime choice (the search
// box's `Match` picker switches per request), so pinning one at boot would leave
// the others unreachable without a restart; SEARCH_MODE now only decides which
// one a tab STARTS on. That also means the bm25 index is always built and its
// write path always on: one postings row per distinct term per mutation. The
// index is the cost of offering relevance ranking, and it is the trade this
// build makes deliberately — see docs/features.md for the alternative
// (SEARCH_MODE=bm25-only) if a deployment wants to avoid that write.
//
// The heal closes the one gap the runtime choice opens: postings are a cache of
// a Go-side pure function (search.Stem over search.Tokenize), so rows created
// while bm25 was off have no postings and SQL cannot compute them — relevance
// search would silently find nothing for every pre-existing todo. The heal
// re-indexes exactly those rows at boot, through the same IndexDoc the mutation
// path uses, which is what makes a healed index indistinguishable from an
// incrementally written one.
//
// An error here is a config/plugin disagreement about the valid mode names,
// the one bug that must not boot into silently different search behaviour:
// config's normalizer already maps a typo onto the substring default, so the
// plugin only ever sees a name it knows.
func bootSearch(ctx context.Context, cfg *config.Config, todoH *todohandlers.Handler, sqlDB *sql.DB) error {
	mode := cfg.Search.Mode
	src := todohandlers.NewTodoSource(todoH.Q)

	// New is still called for the configured mode: it is the validation gate,
	// so a config/plugin disagreement fails the boot loudly instead of
	// silently falling back inside a layer map.
	idx := search.SQLIndex{DB: sqlDB}
	if _, err := search.New(mode, src, idx); err != nil {
		return fmt.Errorf("server: search layer %q: %w", mode, err)
	}
	layers := search.NewAll(src, idx)
	set := todohandlers.SearchLayers{
		ByMode:  make(map[string]todohandlers.Searcher, len(layers)),
		Default: mode,
		Order:   search.Modes(),
	}
	for name, layer := range layers {
		set.ByMode[name] = todohandlers.NewTodoSearcher(layer)
	}
	ix := todohandlers.NewTodoIndexer(idx)
	todoH.SetSearch(set, ix, sqlDB)

	if err := todohandlers.HealSearchIndex(ctx, todoH.Q, ix, sqlDB); err != nil {
		return fmt.Errorf("server: search index heal: %w", err)
	}
	slog.Info("server: todo search wired",
		"default", mode, "modes", len(set.ByMode))
	return nil
}
