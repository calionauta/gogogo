package config

import (
	"encoding/hex"
	"os"
	"strings"
)

// loadAuthEnv reads the auth env. Split out of Load so the config file
// stays under the file-sizes gate; behavior unchanged.
//
// AUTH_ENC_KEY seals TOTP secrets (64 hex chars = 32 bytes). Malformed
// values are dropped to empty (fail-safe: never a truncated key); a
// set-but-malformed value fails the boot instead (checked where the key
// is consumed). ADMIN_EMAILS promotes existing rows to admin at boot,
// VIEWER_EMAILS to the restricted viewer role (read + create + toggle
// on todos; no deletes, no paid actions). Both are promotion-only and
// never provision: unknown addresses are skipped, and an address in
// both lists ends admin (admin promotion runs last).
// SMTPConfig is the mail relay for verification/reset messages.
type SMTPConfig struct {
	Addr     string
	From     string
	Username string
	Password string
}

func (cfg *Config) loadAuthEnv() {
	cfg.Auth.MagicLinkBindBrowser = envBool("MAGIC_LINK_BIND_BROWSER", false)
	cfg.Auth.RequireEmailVerification = envBool("REQUIRE_EMAIL_VERIFICATION", false)
	cfg.Auth.SessionIdleTimeout = envDuration("SESSION_IDLE_TIMEOUT", 0)
	cfg.Auth.SMTP = SMTPConfig{
		Addr:     getEnv("SMTP_ADDR", ""),
		From:     getEnv("SMTP_FROM", ""),
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
	}
	if raw := os.Getenv("AUTH_ENC_KEY"); raw != "" {
		if key, err := hex.DecodeString(strings.TrimSpace(raw)); err == nil && len(key) == 32 {
			cfg.Auth.TOTPEncKey = key
		}
	}
	if raw := os.Getenv("ADMIN_EMAILS"); raw != "" {
		for e := range strings.SplitSeq(raw, ",") {
			if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
				cfg.AdminEmails = append(cfg.AdminEmails, e)
			}
		}
	}
	if raw := os.Getenv("VIEWER_EMAILS"); raw != "" {
		for e := range strings.SplitSeq(raw, ",") {
			if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
				cfg.ViewerEmails = append(cfg.ViewerEmails, e)
			}
		}
	}
}
