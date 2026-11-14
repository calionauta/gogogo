// The ownership boundary is checked against the core schema's own
// table set, so it cannot go stale inside this package.
package recipe

import "testing"

// TestEveryCoreTableIsRefused pins the boundary to tables that actually exist.
// The list this package used to carry inline was stale: it named `magic_links`
// and `config` (no migration creates either) and omitted `notes`, `boards`,
// `notifications` and `op_receipts` — so a recipe declaring one of those passed
// every gate while owning a core object.
func TestEveryCoreTableIsRefused(t *testing.T) {
	for _, table := range []string{
		"users", "sessions", "todos", "notes", "boards", "doc_snapshots",
		"notifications", "op_receipts", "security_events", "search_postings",
		"recovery_codes", "magic_tokens", "oauth_accounts", "casbin_rule",
		"schema_migrations",
	} {
		if !IsCoreTable(table) {
			t.Errorf("%q belongs to the core schema and must be refused to a recipe", table)
		}
	}
	// The prefix rule survives for the same reason it existed: a core table's
	// side indexes and child tables stay core.
	if !IsCoreTable("users_legacy") {
		t.Error("a table prefixed with a core name must stay core")
	}
	// The negative control: without it, "everything is core" would pass too.
	for _, table := range []string{"ext_timer_sessions", "ext_notes", "session_log"} {
		if IsCoreTable(table) {
			t.Errorf("%q is not a core table; refusing it would make the boundary a blanket ban", table)
		}
	}
	if IsCoreTable("") {
		t.Error("an empty name is not a core table")
	}
}
