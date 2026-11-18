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

// RecipeID is the id this unit's schema is applied under, and the id its
// declaration names itself with.
//
// It lives here, beside the DDL, not in the handlers package: the native shell
// (cmd/gui) boots the same migration set and has no business importing the HTTP
// handlers to spell it. That import dragged the whole handler tree — and with
// it internal/recipe/register, and with that a cgo-backed collab seam — into a
// binary whose release build is CGO_ENABLED=0, which broke every release from
// v0.63.0 to v0.71.2. A schema id is not a handler concern.
const RecipeID = "todo"
