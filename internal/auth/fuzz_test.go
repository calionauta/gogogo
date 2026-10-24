// SCOPE:core — Fuzz seeds for the pure auth parsers (no DB, no HTTP).
package auth

import "testing"

func FuzzNormalizeEmail(f *testing.F) {
	for _, seed := range []string{"a@example.com", "A@Example.COM", "", "not-an-email", "a@b", " user+tag@example.com "} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := normalizeEmail(raw)
		if err == nil && got != raw {
			// Normalization must be idempotent on an accepted address.
			again, aerr := normalizeEmail(got)
			if aerr != nil || again != got {
				t.Fatalf("normalize not idempotent: %q -> %q -> %q (err %v)", raw, got, again, aerr)
			}
		}
	})
}

func FuzzVerifyPassword(f *testing.F) {
	for _, seed := range []struct{ encoded, password string }{
		{"", ""},
		{"argon2id$v=1$c2FsdA$aGFzaA", "Correct-horse-99!"},
		{"not-a-hash", "x"},
		{"argon2id$v=1$%%$%%", "x"},
	} {
		f.Add(seed.encoded, seed.password)
	}
	f.Fuzz(func(_ *testing.T, encoded, password string) {
		// The contract is "never panic"; unknown formats fail closed.
		_, _ = verifyPassword(encoded, password)
	})
}

func FuzzValidatePassword(f *testing.F) {
	for _, seed := range []string{"", "short", "Correct-horse-99!"} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, password string) {
		_ = validatePassword(password)
	})
}
