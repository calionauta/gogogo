// The kiosk recipe's own DDL, embedded in its package.
//
// The files live here, not in internal/dbschema/migrations: a recipe's schema is
// its own, applied through the shared ledger under the recipe id, and it must
// travel with the code that owns it. `recipe install` also mirrors the same
// bytes flat into internal/db/recipe_schema/ so sqlc can see the table.
package kiosk

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql locale/*.json
var migrations embed.FS

// Migrations is the filesystem the seam's Migrator interface returns. It is a
// package-level value so the provider stays a value type.
var Migrations fs.FS = migrations

// Translations is the filesystem the seam's Translator interface returns: the
// recipe's own strings under locale/<tag>.json. The keys are UNPREFIXED — the
// engine prefixes them with this recipe's id, so `title` becomes
// `kiosk.title` and cannot collide with core or with another recipe.
var Translations fs.FS = migrations
