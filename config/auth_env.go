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
// is consumed). ADMIN_EMAILS promotes existing rows to admin at boot.
func (cfg *Config) loadAuthEnv() {
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
}
