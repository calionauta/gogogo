// SCOPE:core — the core schema's table set, as data.
//
// It lives beside the migrations because the ownership boundary is a fact about
// the schema, not about the recipe package: a plugin layer that embeds the core's
// table names is a stale copy of a fact it does not own. The copy this replaces
// had drifted twice — it named `magic_links` and `config`, which no migration
// creates, and it omitted `notes`, `boards`, `notifications` and `op_receipts`,
// which are real, so a recipe could declare one of those and pass every gate.
//
// tables_test.go compares this list to a migrated database in both directions, so
// a migration that adds a table without an ownership statement fails the build,
// and a dropped table cannot linger here as a phantom boundary.
package dbschema

// coreTables is sorted: the list is diffed in review and searched per declaration,
// and a sorted list is also how a duplicate stays invisible for one line only.
var coreTables = []string{
	"boards",
	"casbin_rule",
	"doc_snapshots",
	"magic_tokens",
	"notes",
	"notifications",
	"oauth_accounts",
	"op_receipts",
	"recovery_codes",
	// schema_migrations is the ledger itself: created by Migrate, owned by core,
	// and never claimable by a recipe for the same reason as the rest.
	"schema_migrations",
	"search_postings",
	"security_events",
	"sessions",
	"todos",
	"users",
}

// CoreTables returns every table the core schema owns. A recipe may never
// declare one: two owners of a table is what makes uninstall unsafe and
// migrations unmergeable.
//
// The slice is a copy, so a caller that sorts or filters it cannot reorder
// the boundary for the next caller.
func CoreTables() []string {
	return append([]string(nil), coreTables...)
}
