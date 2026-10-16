// Split from config.go for the file-size gate (500 lines): the AI
// credits plugin's env knobs and their parsers. Config keeps one Credits
// field.
package config

import (
	"encoding/hex"
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
}

const creditsEncKeyBytes = 32

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
		out[k] = v
	}
	return out
}
