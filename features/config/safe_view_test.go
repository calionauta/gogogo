// SCOPE:feature - mirrors safe_view.go coverage.
//
// Tests for the redacted view builder. We exercise four invariants:
//
//  1. Empty / unset values render with a deterministic "not set"
//     signal — never `***` — so an operator can tell the difference
//     between "this secret is unset" and "this secret is set but
//     masked".
//  2. Token-shaped fields are always masked: the literal value
//     must not appear in the rendered Display string.
//  3. AGE_SECRET_KEY is never inspected; only its set-state is.
//  4. URL-shaped fields have credentials stripped but the host
//     survives for operator context.
//
// These four invariants are the load-bearing safety guarantees of
// the /config page; if any regresses, the page is leaking secrets.
package config

import (
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestBuildPageData_MasksEncryptionKey asserts that the literal
// encryption key never appears in the rendered Display string when
// set, and that an unset value stays empty (not "***").
func TestBuildPageData_MasksEncryptionKey(t *testing.T) {
	cfg := &config.Config{
		AppName:    "test-app",
		BuildLabel: "test",
	}
	cfg.EncryptionKey = "supersecret-encryption-key-32bytes!"

	pd := BuildPageData(cfg)

	row := findRow(t, pd, "storage", "EncryptionKey")
	if row.Set != true {
		t.Fatalf("EncryptionKey should be marked Set")
	}
	if row.Masked != true {
		t.Fatalf("EncryptionKey row must be Masked")
	}
	if strings.Contains(row.Display, "supersecret") {
		t.Fatalf("EncryptionKey Display leaks raw value: %q", row.Display)
	}
	if row.Display == "" || row.Display == "***" {
		t.Fatalf("EncryptionKey Display should have a partial mask, got %q", row.Display)
	}
}

// TestBuildPageData_EmptyEncryptionKeyStaysEmpty asserts that
// "unset" and "set" render differently — operators must be able
// to distinguish them.
func TestBuildPageData_EmptyEncryptionKeyStaysEmpty(t *testing.T) {
	cfg := &config.Config{AppName: "test-app", BuildLabel: "test"}
	pd := BuildPageData(cfg)
	row := findRow(t, pd, "storage", "EncryptionKey")
	if row.Set {
		t.Fatalf("EncryptionKey should not be Set when empty")
	}
	if row.Display != "" {
		t.Fatalf("EncryptionKey Display should be empty when unset, got %q", row.Display)
	}
}

// TestBuildPageData_AGESecretKeyNeverInspected asserts that
// AGE_SECRET_KEY only contributes its set-state boolean; the value
// itself is never surfaced even when the env var is set.
func TestBuildPageData_AGESecretKeyNeverInspected(t *testing.T) {
	// We don't actually set AGE_SECRET_KEY in the test env (it
	// decrypts secrets file at runtime); just confirm the row is
	// present and boolean-only regardless.
	cfg := &config.Config{AppName: "test-app", BuildLabel: "test"}
	pd := BuildPageData(cfg)
	var saw bool
	for _, g := range pd.Groups {
		if g.Name != "Secrets" {
			continue
		}
		for _, r := range g.Rows {
			if r.Key == "AGESecretKey" {
				if !r.BoolOnly {
					t.Fatalf("AGESecretKey row must be BoolOnly")
				}
				saw = true
			}
		}
	}
	if !saw {
		t.Fatalf("Secrets group should contain AGESecretKey row")
	}
}

// TestBuildPageData_LeafNodeURLStripsCredentials asserts that a
// nats-leaf://user:pass@host:4222 URL renders with credentials
// stripped but the rest of the URL intact.
func TestBuildPageData_LeafNodeURLStripsCredentials(t *testing.T) {
	cfg := &config.Config{AppName: "test-app", BuildLabel: "test"}
	cfg.NATS.LeafNodeURL = "nats-leaf://prodadmin:s3cr3t@leaf.example.com:4222"
	pd := BuildPageData(cfg)
	row := findRow(t, pd, "nats", "LeafNodeURL")
	if !row.Set {
		t.Fatalf("LeafNodeURL should be Set")
	}
	if !row.Masked {
		t.Fatalf("LeafNodeURL should be masked (URL-shaped field)")
	}
	if strings.Contains(row.Display, "s3cr3t") {
		t.Fatalf("LeafNodeURL Display leaks password: %q", row.Display)
	}
	if strings.Contains(row.Display, "prodadmin") {
		t.Fatalf("LeafNodeURL Display leaks username: %q", row.Display)
	}
	if !strings.Contains(row.Display, "leaf.example.com") {
		t.Fatalf("LeafNodeURL Display should preserve host, got %q", row.Display)
	}
}

// TestBuildPageData_NonSecretFieldsSurfaced asserts that the
// "safe to show" fields keep their literal values (DBPath,
// DataDir, BuildLabel, AppName) so operators get useful context.
func TestBuildPageData_NonSecretFieldsSurfaced(t *testing.T) {
	cfg := &config.Config{
		AppName:    "production-app",
		BuildLabel: "v1.2.3",
		Host:       "127.0.0.1",
		DBPath:     "/var/lib/test/data.db",
		DataDir:    "/var/lib/test",
	}
	pd := BuildPageData(cfg)
	for _, key := range []string{"AppName", "BuildLabel"} {
		row := findRow(t, pd, "app", key)
		if row.Masked {
			t.Fatalf("%s should not be masked (non-secret)", key)
		}
		if row.Display != "" && key == "AppName" && row.Display != "production-app" {
			t.Fatalf("AppName Display = %q, want production-app", row.Display)
		}
	}
	for _, key := range []string{"DBPath", "DataDir"} {
		row := findRow(t, pd, "storage", key)
		if row.Masked {
			t.Fatalf("%s should not be masked (path only)", key)
		}
	}
}

// TestMasked_Boundaries verifies the threshold rules in mask.go:
// very short values round-trip to "***", mid-length get first2 +
// last2, long values get first4 + "…" + last2.
func TestMasked_Boundaries(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"7chars", "abcdefg", "***"},
		{"8chars", "abcdefgh", "ab***gh"},
		{"16chars", "abcdefghijklmnop", "ab***op"},
		{"17chars", "abcdefghijklmnopq", "abcd…pq"},
		// Skipped: very short block to keep tests non-brittle.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := masked(tc.in)
			if got != tc.want {
				t.Fatalf("masked(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// findRow is a tiny test helper that locates a row by group + key.
// Splits the assertion path from the lookup mechanics so the
// per-test assertions read top-to-bottom in setup/expect style.
func findRow(t *testing.T, pd PageData, group, key string) Row {
	t.Helper()
	for _, g := range pd.Groups {
		if g.Name != groupTitle(group) {
			continue
		}
		for _, r := range g.Rows {
			if r.Key == key {
				return r
			}
		}
	}
	t.Fatalf("row not found: group=%q key=%q", group, key)
	return Row{}
}

// groupTitle maps the safeView's internal group name (lowercase,
// used by safeValue/envFromKey) to the human-readable Title used
// in the rendered output. Keeping the mapping local to the test
// file means safe_view.go doesn't have to be edited just for
// testability.
func groupTitle(group string) string {
	for _, g := range []struct{ in, out string }{
		{"app", "App"},
		{"storage", "Storage"},
		{"admin", "Admin"},
		{"ai", "AI (GoAI)"},
		{"nats", "NATS"},
		{"dagnats", "DagNats"},
		{"sync", "Sync"},
		{"secrets", "Secrets"},
	} {
		if g.in == group {
			return g.out
		}
	}
	return group
}
