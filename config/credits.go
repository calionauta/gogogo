// Split from config.go for the file-size gate (500 lines): the AI
// credits plugin's env knobs and their parsers. Config keeps one Credits
// field.
package config

import (
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// CreditsConfig holds the env-driven knobs for the AI credits plugin. Every
// field defaults to billing-off; the plugin is only live when Enabled is
// true AND the lib's schema can be applied to the app SQLite database.
type CreditsConfig struct {
	Enabled             bool
	EncKey              []byte // CREDITS_ENC_KEY, 32 bytes; empty = BYOK store disabled
	DefaultMode         string // managed | byok | explicit
	MonthlyCredits      int64
	Model               string            // default managed model id (fallback GOAI_MODEL)
	PricingFile         string            // optional JSON prices file; empty = lib defaults
	ByokProviders       map[string]string // provider -> OpenAI-compatible base URL
	StripeSecretKey     string
	StripeWebhookSecret string
	StripeSuccessURL    string
	StripeCancelURL     string
	// StripeAPIBase overrides the Stripe API base URL. Empty means the live
	// api.stripe.com; a local emulator or tunnel is the intended use. It is
	// what lets `make verify-local` click a real checkout with no key, and it
	// is why the lib's CreateCheckout has an offline test at all.
	StripeAPIBase string
}

const creditsEncKeyBytes = 32

// loadCreditsEnv reads the ai-credits knobs. It lives here rather than in
// Load() because every secret is read as os.Getenv on this file, and Load()
// has a statement budget the gate enforces.
func (c *Config) loadCreditsEnv() {
	c.Credits.Enabled = envBool("CREDITS_ENABLED", false)
	c.Credits.DefaultMode = getEnv("CREDITS_DEFAULT_MODE", "explicit")
	c.Credits.MonthlyCredits = int64(envInt("CREDITS_MONTHLY_CREDITS", 0))
	c.Credits.PricingFile = getEnv("CREDITS_PRICING_FILE", "")
	c.Credits.Model = getEnv("CREDITS_MODEL", getEnv("GOAI_MODEL", "gpt-4o-mini"))
	c.Credits.ByokProviders = parseProviders(getEnv("BYOK_PROVIDERS", ""))
	if k := os.Getenv("CREDITS_ENC_KEY"); k != "" {
		c.Credits.EncKey = parseCreditsEncKey(k)
	}
	c.Credits.StripeSecretKey = os.Getenv("STRIPE_SECRET_KEY")
	c.Credits.StripeWebhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	c.Credits.StripeSuccessURL = getEnv("STRIPE_SUCCESS_URL", "http://localhost:3000/credits?status=success")
	c.Credits.StripeCancelURL = getEnv("STRIPE_CANCEL_URL", "http://localhost:3000/credits?status=cancelled")
	c.Credits.StripeAPIBase = getEnv("STRIPE_API_BASE", "")
}

// parseCreditsEncKey accepts the secret-manager representation (64 hex
// chars for 32 key bytes) and nothing else. Anything else yields nil, and
// the credits plugin fails closed when a BYOK provider is configured
// without a valid key.
func parseCreditsEncKey(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if len(raw) == hex.EncodedLen(creditsEncKeyBytes) {
		if key, err := hex.DecodeString(raw); err == nil {
			return key
		}
	}
	return nil
}

// parseProviders parses the BYOK_PROVIDERS env var
// ("openai=https://api.openai.com/v1,anthropic=https://api.anthropic.com/v1")
// into a provider->base URL map. Malformed entries are dropped.
func parseProviders(raw string) map[string]string {
	out := map[string]string{}
	for part := range strings.SplitSeq(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || k == "" || v == "" {
			continue
		}
		if !validProviderURL(v) {
			slog.Warn("config: dropping malformed BYOK provider URL", "provider", k)
			continue
		}
		out[k] = v
	}
	return out
}

// validProviderURL keeps the relay from being configured with a non-HTTP
// scheme or a URL that cannot be dialed. The value is operator-supplied, so
// this is a guardrail against typos and file:/unix: shapes, not an SSRF
// boundary (a localhost provider is a legitimate BYOK choice).
func validProviderURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
