// Single source of truth for the Chat answer surfaces (GENUI_CHAT_SURFACES).
// Split from config.go the way chat_router.go and review.go are: the Config
// struct keeps one GenUI field.
//
// Chat is the one demo: one surface vocabulary (catalog + custom + native,
// see features/genui/components/surface.go) with a persisted conversation.
// The Ask one-shot leg is deleted — its demonstrable strictness lives in
// Chat through the surface set, not a second page.
package config

import (
	"os"
	"strings"
)

// GenUIConfig holds the Chat answer surfaces.
//
// Empty is the compatibility contract: an absent variable keeps the
// documented default (Chat = catalog + custom + native). Every band toggles
// independently — "none" is the prose-only set — and the tab toggles the same
// names per request from the composer.
type GenUIConfig struct {
	// ChatSurfaces is GENUI_CHAT_SURFACES: a CSV of catalog|custom|native|none.
	ChatSurfaces string
}

// loadGenUIEnv fills GenUI from env. The value is validated at the
// composition root (components.ParseSurfaces), not here: an unknown surface
// name must fail the boot loudly, and the boot is where the error can be
// reported, not a config loader whose only vocabulary is "logs a warning".
//
// Reads the env directly (not through getEnv) because "absent" and "empty"
// are the same thing here: both mean "use the default", which is what
// ParseSurfaces does with an empty string.
func (c *Config) loadGenUIEnv() {
	c.GenUI.ChatSurfaces = strings.TrimSpace(os.Getenv("GENUI_CHAT_SURFACES"))
}
