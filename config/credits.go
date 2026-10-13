// Split from config.go for the file-size gate (500 lines): the AI
// credits plugin's env knobs. Config keeps one Credits field.
package config

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
