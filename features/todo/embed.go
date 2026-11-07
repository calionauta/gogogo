// The todo unit's own DDL, embedded in its package.
//
// The files live here, not in internal/dbschema/migrations: a recipe's schema is
// its own, applied through the shared ledger under the recipe id, and it must
// travel with the code that owns it. The mirrored copy of these bytes under
// internal/db/recipe_schema/ is what sqlc reads; `recipe status` holds the two
// byte-identical for installed recipes, and the same copy here is committed by
// hand for this in-tree unit.
package todo

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrations is the filesystem the seam's Migrator interface returns. It is a
// package-level value so the provider stays a value type.
var Migrations fs.FS = migrations
