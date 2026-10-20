// SCOPE:core — instance identity tests.
package instance

import (
	"sync"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"MacBook-Pro.local": "macbook-pro-local",
		"  APP_1  ":         "app-1",
		"a..b":              "a-b",
		"-x-":               "x",
		"":                  "",
		"gogogo-7f9c4d-abc": "gogogo-7f9c4d-abc",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got := sanitize(long); len(got) != maxLen {
		t.Errorf("sanitize(long) len = %d, want %d", len(got), maxLen)
	}
}

// TestIDEnvOverride pins the convention: the env var wins, and the value is
// usable as a durable-name suffix. Serial (no t.Parallel): it resets the
// package memo.
func TestIDEnvOverride(t *testing.T) {
	t.Setenv(EnvVar, "Prod-EU.1")
	once = sync.Once{}
	if got := ID(); got != "prod-eu-1" {
		t.Fatalf("ID = %q, want prod-eu-1", got)
	}
}

// TestIDFallsBackWhenUnset: without the env var the identity is still a
// valid, non-empty token (hostname or the random fallback).
func TestIDFallsBackWhenUnset(t *testing.T) {
	t.Setenv(EnvVar, "")
	once = sync.Once{}
	got := ID()
	if got == "" {
		t.Fatal("ID is empty without APP_INSTANCE_ID")
	}
	if want := sanitize(got); want != got {
		t.Fatalf("ID %q is not sanitized form %q", got, want)
	}
}
