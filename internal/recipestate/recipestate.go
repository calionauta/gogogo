// Package recipestate is the one place the recipe_state table becomes a
// predicate: the boot switch (which recipes mount and start workers) and the
// admin Units table (which rows read "disabled") both ask it, so the
// missing-row rule cannot drift between them.
//
// The switch is evaluated once at boot on purpose — a disabled recipe must not
// start its workers, consumers or tickers — so this package reads, it never
// caches and never writes. The write side is the admin mutation
// (SetRecipeState), which is a different decision with an audit row.
package recipestate

import (
	"context"
	"fmt"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// States is the table as the switch needs it: recipe id → enabled. The zero
// value is usable and means "nothing recorded", which is everything enabled.
type States map[string]bool

// Load reads the runtime switches. A nil query set (the MCP, a fixture) yields
// an empty States rather than an error: the caller's default is already
// "enabled", and a missing table must not stop a boot. The error is returned
// for the caller to log; the States returned alongside it is still usable.
func Load(ctx context.Context, q *dbgen.Queries) (States, error) {
	states := States{}
	if q == nil {
		return states, nil
	}
	rows, err := q.ListRecipeStates(ctx)
	if err != nil {
		return states, fmt.Errorf("recipestate: list: %w", err)
	}
	for _, row := range rows {
		states[row.ID] = row.Enabled != 0
	}
	return states, nil
}

// Enabled reports whether id may mount. A missing row means enabled: installing
// a recipe must not require a second write before it mounts, and a disabled one
// is an explicit row, never an absence.
func (s States) Enabled(id string) bool {
	enabled, ok := s[id]
	return !ok || enabled
}
