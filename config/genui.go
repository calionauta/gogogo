// Single source of truth for the AI answer surfaces (GENUI_*_SURFACES env).
// Split from config.go the way ask_router.go and review.go are: the Config
// struct keeps one GenUI field.
//
// Ask AI and Chat are two views over ONE surface vocabulary (see
// features/genui/components/surface.go). The difference between them is the
// interaction shape — one-shot answer vs persisted conversation — not which
// kinds of answer they may produce, so the vocabulary is configuration both
// legs read rather than two prompts that drift.
package config

import (
	"os"
	"strings"
)

// GenUIConfig holds the per-leg answer surfaces.
//
// Empty is the compatibility contract: an absent variable keeps the leg's
// documented default (Ask = catalog + native, Chat = catalog + custom +
// native). Native panels are appended by the normalizer and cannot be turned
// off from here — an answer about the user's own data should come from the
// app's data, on both legs, always.
type GenUIConfig struct {
	// AskSurfaces is GENUI_ASK_SURFACES: a CSV of catalog|custom|native.
	AskSurfaces string
	// ChatSurfaces is GENUI_CHAT_SURFACES: the same CSV for the chat leg.
	ChatSurfaces string
}

// loadGenUIEnv fills GenUI from env. The value is validated at the
// composition root (components.ParseSurfaces), not here: an unknown surface
// name must fail the boot loudly, and the boot is where the error can be
// reported, not a config loader whose only vocabulary is "logs a warning".
//
// Reads the env directly (not through getEnv) because "absent" and "empty"
// are the same thing here: both mean "use the leg's default", which is what
// ParseSurfaces does with an empty string.
func (c *Config) loadGenUIEnv() {
	c.GenUI.AskSurfaces = strings.TrimSpace(os.Getenv("GENUI_ASK_SURFACES"))
	c.GenUI.ChatSurfaces = strings.TrimSpace(os.Getenv("GENUI_CHAT_SURFACES"))
}
