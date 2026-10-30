// SCOPE:feature — Entitlement gate for the native window.
package main

import (
	"fmt"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/license"
)

// entitlementCheck refuses to open the window when the release is gated and
// no valid token is stored. The desktop shell owns the activation page; this
// binary is a second frontend and only verifies, so a licensed product cannot
// be bypassed by launching gogogo-gui instead.
func entitlementCheck(cfg *config.Config) error {
	if !cfg.LicenseGate || cfg.LicenseMode == config.LicenseNone {
		return nil
	}
	pub, err := license.ParsePublicKey(cfg.LicensePublicKey)
	if err != nil {
		return fmt.Errorf("gogogo-gui: %w", err)
	}
	st, oerr := license.Open(cfg.LicenseFile, pub)
	if oerr != nil {
		return fmt.Errorf("gogogo-gui: %w", oerr)
	}
	if !st.Valid(time.Now()) {
		return fmt.Errorf(
			"gogogo-gui: activation required — open the desktop app to activate, "+
				"or mint a token with cmd/license (LICENSE_FILE %s)", cfg.LicenseFile)
	}
	return nil
}
