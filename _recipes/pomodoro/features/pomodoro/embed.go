// The pomodoro recipe's own DDL, embedded in its package.
//
// The files live here, not in internal/dbschema/migrations: a recipe's schema is
// its own, applied through the shared ledger under the recipe id, and it must
// travel with the code that owns it. `recipe install` also mirrors the same
// bytes flat into internal/db/recipe_schema/ so sqlc can see the table.
package pomodoro

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrations is the filesystem the seam's Migrator interface returns. It is a
// package-level value so the provider stays a value type.
var Migrations fs.FS = migrations
