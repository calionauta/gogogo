package config

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"
	"time"
)

func TestParseCreditsEncKey(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, 32)
	hexKey := hex.EncodeToString(key)

	if got := parseCreditsEncKey(hexKey); !bytes.Equal(got, key) {
		t.Fatalf("hex key = %x, want %x", got, key)
	}
	if got := parseCreditsEncKey(string(key)); !bytes.Equal(got, key) {
		t.Fatalf("raw key = %x, want %x", got, key)
	}
}

// TestEnvDuration covers the reader behind DAGNATS_GREET_PACING. The default
// matters: this knob exists so demonstration pacing can be tuned WITHOUT
// deleting it, so a bad or non-positive value must fall back rather than
// silently zeroing the pause (an accidental 0 would make the onboarding
// stepper flash past, which is the bug the pause exists to prevent).
func TestEnvDuration(t *testing.T) {
	// Not parallel: t.Setenv forbids t.Parallel.

	cases := []struct {
		name string
		set  string
		want time.Duration
	}{
		{"unset falls back", "", 1500 * time.Millisecond},
		{"parses ms", "250ms", 250 * time.Millisecond},
		{"parses s", "2s", 2 * time.Second},
		{"garbage falls back", "not-a-duration", 1500 * time.Millisecond},
		{"zero is refused", "0s", 1500 * time.Millisecond},
		{"negative is refused", "-1s", 1500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const key = "GOGOGO_TEST_ENV_DURATION"
			if tc.set == "" {
				if err := os.Unsetenv(key); err != nil {
					t.Fatalf("unset: %v", err)
				}
			} else {
				t.Setenv(key, tc.set)
			}
			if got := envDuration(key, 1500*time.Millisecond); got != tc.want {
				t.Errorf("envDuration(%q) = %v, want %v", tc.set, got, tc.want)
			}
		})
	}
}

// TestBuildTag pins the stale-tab identity: label + commit joined, dev
// default inert ("dev/"), nil-safe for callers without a config.
func TestBuildTag(t *testing.T) {
	t.Parallel()
	var nilCfg *Config
	if got := nilCfg.BuildTag(); got != "dev/" {
		t.Errorf("nil BuildTag = %q, want %q", got, "dev/")
	}
	cases := []struct {
		name   string
		label  string
		commit string
		want   string
	}{
		{"dev defaults are inert", "dev", "", "dev/"},
		{"prod carries tag and sha", "v0.21.0", "abc123", "v0.21.0/abc123"},
		{"label change alone retires tabs", "v0.22.0", "abc123", "v0.22.0/abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Config{BuildLabel: tc.label, BuildCommit: tc.commit}
			if got := c.BuildTag(); got != tc.want {
				t.Errorf("BuildTag = %q, want %q", got, tc.want)
			}
		})
	}
}
