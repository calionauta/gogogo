// SCOPE:core — Tests for the flat-stack dependency guard.
package main

import "testing"

func TestBannedPrefixes(t *testing.T) {
	for _, tc := range []struct {
		path   string
		banned bool
	}{
		{"github.com/pocketbase/pocketbase/core", true},
		{"github.com/pocketbase/dbx", true},
		{"github.com/thecodearcher/limen/adapters/sql", true},
		{"modernc.org/sqlite", true},
		{"turso.tech/database/tursogo", false},
		{"github.com/go-chi/chi/v5", false},
		{"github.com/ncruces/go-sqlite3/driver", false},
		{"github.com/calionauta/gogogo/internal/db", false},
	} {
		if got := banned(tc.path); got != tc.banned {
			t.Errorf("banned(%q) = %v, want %v", tc.path, got, tc.banned)
		}
	}
}

func TestLegacyAllowlist(t *testing.T) {
	if !legacy("db/pocketbase.go") {
		t.Error("db/pocketbase.go must stay allowlisted until step 7")
	}
	if legacy("internal/db/db.go") {
		t.Error("internal/db/db.go must never be allowlisted")
	}
	if legacy("features/todo/chi/handler.go") {
		t.Error("chi handler must never be allowlisted")
	}
}

func TestRunGreenOnThisTree(t *testing.T) {
	if err := run(); err != nil {
		t.Fatalf("run() = %v, want nil on the migrated tree", err)
	}
}
