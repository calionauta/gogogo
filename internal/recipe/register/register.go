// The wiring contract between installed recipe code and the
// composition root.
//
// Why a separate package: the engine (internal/recipe) must stay importable
// without the app's config/auth/store types, and the recipe packages must not
// import internal/server (that would be a cycle, since server imports them
// through the generated registration). This package is the one place both sides
// can name.
//
// The contract is deliberately small. A field here is a promise that every
// recipe may rely on it, so it grows by a decision, not by convenience.
package register

import (
	"database/sql"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
)

// Deps is what an installed recipe may ask the composition root for. The
// composition root builds it from the same values it hands its own handlers, so
// a recipe is not a special case at boot.
type Deps struct {
	Cfg     *config.Config
	Auth    *auth.Auth
	Queries *dbgen.Queries
	Hub     *queue.SSEHub
	// SQLDB is the store the composition root migrates. A recipe that owns
	// tables reads it here; the recipe's migration set is applied by the root
	// (recipe.Migrator), never by the recipe itself.
	SQLDB *sql.DB
	// Slots is where a recipe declares its content for a page slot. It is
	// nil in a caller that offers no slots, and Add is nil-safe on the
	// registry, so a recipe registers unconditionally.
	Slots *recipe.SlotRegistry
}

// Func is what a recipe package exports as `Register`. The generated
// registration file calls it once per installed and not-removed receipt.
//
// A recipe builds its handler here and hands its provider to the seam; it never
// mounts anything itself, so collisions stay the registry's to report.
type Func func(reg *recipe.Registry, d Deps)
