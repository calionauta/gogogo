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
	// Raw 32-byte values are not keys anymore (no legacy shapes): they
	// parse to nil and the plugin fails closed at BYOK configuration.
	if got := parseCreditsEncKey(string(key)); got != nil {
		t.Fatalf("raw key = %x, want nil", got)
	}
	if got := parseCreditsEncKey("short"); got != nil {
		t.Fatalf("short key = %x, want nil", got)
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

// TestRBACPermissiveParsing pins the rollout flag wiring: RBAC_PERMISSIVE
// parses like every other bool knob, defaulting to enforcing (false).
// Red-proof: drop the envBool line in Load and the true-case goes red;
// default it true and the default-case goes red (production must enforce).
func TestRBACPermissiveParsing(t *testing.T) {
	// Not parallel: t.Setenv forbids t.Parallel.
	t.Setenv("RBAC_ENABLED", "true")
	t.Setenv("RBAC_PERMISSIVE", "true")
	if got := Load(); !got.RBACPermissive {
		t.Errorf("RBACPermissive with RBAC_PERMISSIVE=true = false, want true")
	}
	t.Setenv("RBAC_PERMISSIVE", "false")
	if got := Load(); got.RBACPermissive {
		t.Errorf("RBACPermissive with RBAC_PERMISSIVE=false = true, want false")
	}
	// Unset must default to enforcing: production enforces unless the
	// operator opts into audit mode explicitly.
	if err := os.Unsetenv("RBAC_PERMISSIVE"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if got := Load(); got.RBACPermissive {
		t.Errorf("RBACPermissive unset = true, want default false")
	}
}

// TestReviewEnvParsing pins the jev-compatible judge knobs: working
// demo defaults when unset, operator overrides via env, out-of-range
// and unparseable thresholds falling back to default (a typo must not
// brick the boot, and must never silently become auto-accept-everything
// at 0.0). Red-proof: drop the clamp in loadReviewEnv and the
// out-of-range cases go red.
func TestReviewEnvParsing(t *testing.T) {
	// Not parallel: t.Setenv forbids t.Parallel.
	t.Setenv("JEV_API_URL", "")
	t.Setenv("JEV_MODEL", "")
	t.Setenv("JEV_API_KEY", "")
	t.Setenv("JEV_TIMEOUT", "")
	t.Setenv("JEV_CONFIDENCE_THRESHOLD", "")
	got := Load()
	if got.Review.APIURL != DefaultJevAPIURL {
		t.Errorf("default APIURL = %q, want demo %q", got.Review.APIURL, DefaultJevAPIURL)
	}
	if got.Review.Model != DefaultJevModel {
		t.Errorf("default Model = %q, want demo %q", got.Review.Model, DefaultJevModel)
	}
	if got.Review.ConfidenceThreshold != DefaultReviewConfidenceThreshold {
		t.Errorf("default threshold = %v, want %v", got.Review.ConfidenceThreshold, DefaultReviewConfidenceThreshold)
	}

	t.Setenv("JEV_API_URL", "http://127.0.0.1:8000/v1/classifier")
	t.Setenv("JEV_MODEL", "jev-latest")
	t.Setenv("JEV_CONFIDENCE_THRESHOLD", "0.9")
	got = Load()
	if got.Review.APIURL != "http://127.0.0.1:8000/v1/classifier" {
		t.Errorf("override APIURL = %q", got.Review.APIURL)
	}
	if got.Review.Model != "jev-latest" {
		t.Errorf("override Model = %q", got.Review.Model)
	}
	if got.Review.ConfidenceThreshold != 0.9 {
		t.Errorf("override threshold = %v, want 0.9", got.Review.ConfidenceThreshold)
	}

	for _, bad := range []string{"2.0", "-0.5", "abc"} {
		t.Setenv("JEV_CONFIDENCE_THRESHOLD", bad)
		if got := Load(); got.Review.ConfidenceThreshold != DefaultReviewConfidenceThreshold {
			t.Errorf("threshold %q = %v, want default %v", bad, got.Review.ConfidenceThreshold, DefaultReviewConfidenceThreshold)
		}
	}
}

// TestChatRouterEnvParsing pins the Chat router's opt-in knob: OFF unless
// JEV_ROUTER_ENABLED is set (the compatibility guarantee — an existing
// install must be untouched by this feature), and a threshold that resets
// to the default rather than failing the boot. Red-proof: default Enabled
// to true in loadChatRouterEnv and the first assertion goes red.
func TestChatRouterEnvParsing(t *testing.T) {
	// Not parallel: t.Setenv forbids t.Parallel.
	t.Setenv("JEV_ROUTER_ENABLED", "")
	t.Setenv("JEV_ROUTER_CONFIDENCE_THRESHOLD", "")
	got := Load()
	if got.ChatRouter.Enabled {
		t.Error("ChatRouter.Enabled unset = true, want the opt-in default false")
	}
	if got.ChatRouter.ConfidenceThreshold != DefaultChatRouterConfidenceThreshold {
		t.Errorf("default threshold = %v, want %v", got.ChatRouter.ConfidenceThreshold, DefaultChatRouterConfidenceThreshold)
	}

	t.Setenv("JEV_ROUTER_ENABLED", "true")
	t.Setenv("JEV_ROUTER_CONFIDENCE_THRESHOLD", "0.9")
	got = Load()
	if !got.ChatRouter.Enabled {
		t.Error("JEV_ROUTER_ENABLED=true = false")
	}
	if got.ChatRouter.ConfidenceThreshold != 0.9 {
		t.Errorf("override threshold = %v, want 0.9", got.ChatRouter.ConfidenceThreshold)
	}

	t.Setenv("JEV_ROUTER_ENABLED", "off")
	if got := Load(); got.ChatRouter.Enabled {
		t.Error(`JEV_ROUTER_ENABLED="off" = true, want envBool's false`)
	}
	for _, bad := range []string{"2.0", "-0.5", "abc"} {
		t.Setenv("JEV_ROUTER_CONFIDENCE_THRESHOLD", bad)
		if got := Load(); got.ChatRouter.ConfidenceThreshold != DefaultChatRouterConfidenceThreshold {
			t.Errorf("threshold %q = %v, want default %v",
				bad, got.ChatRouter.ConfidenceThreshold, DefaultChatRouterConfidenceThreshold)
		}
	}
}
