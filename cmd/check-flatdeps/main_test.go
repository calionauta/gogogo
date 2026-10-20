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

// TestEveryFlatPackageIsFrameworkFree drives run() over the real tree:
// the legacy-driver allowlist is gone with the framework boot, so the
// banned-prefix closure is the whole guard.
func TestEveryFlatPackageIsFrameworkFree(t *testing.T) {
	if err := run(); err != nil {
		t.Fatalf("run() = %v, want nil (framework-free closure)", err)
	}
}

func TestRunGreenOnThisTree(t *testing.T) {
	if err := run(); err != nil {
		t.Fatalf("run() = %v, want nil on the migrated tree", err)
	}
}

// TestSharedLeavesAreTheOnlyCrossFeatureImports pins the allowlist. Adding a
// fourth shared leaf is a design decision (a feature must never reach into
// another feature's domain), so it has to show up in this diff — the same
// reason the flat package list is closed.
func TestSharedLeavesAreTheOnlyCrossFeatureImports(t *testing.T) {
	want := []string{
		featureModule + "authviews",
		featureModule + "sounds",
		featureModule + "genui/components",
	}
	if len(sharedLeaves) != len(want) {
		t.Fatalf("sharedLeaves = %v, want %v", sharedLeaves, want)
	}
	for i := range want {
		if sharedLeaves[i] != want[i] {
			t.Errorf("sharedLeaves[%d] = %q, want %q", i, sharedLeaves[i], want[i])
		}
		if !sharedLeaf(want[i]) || !sharedLeaf(want[i]+"/sub") {
			t.Errorf("sharedLeaf(%q) = false, want true", want[i])
		}
	}
	for _, domain := range []string{
		featureModule + "todo",
		featureModule + "todo/handlers",
		featureModule + "notes/components",
	} {
		if sharedLeaf(domain) {
			t.Errorf("sharedLeaf(%q) = true — a feature domain must never be a shared leaf", domain)
		}
	}
}

// TestTopFeature keeps the scope extraction honest: only top-level features
// count, so features/todo/handlers is still `todo`.
func TestTopFeature(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{featureModule + "todo", "todo"},
		{featureModule + "todo/handlers", "todo"},
		{featureModule + "genui/components", "genui"},
		{"github.com/calionauta/gogogo/internal/auth", ""},
		{"net/http", ""},
	} {
		if got := topFeature(tc.path); got != tc.want {
			t.Errorf("topFeature(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
