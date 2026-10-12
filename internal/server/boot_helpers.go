// SCOPE:core — Flat-stack boot helpers (demo seed, TOTP key wiring).
package server

import (
	"context"
	"fmt"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/demo"
)

// seedDemoAccounts signs the advertised demo pair up (idempotent: every
// boot after the first is a no-op). Split out of BootChi so the composition
// stays under the funlen gate; behavior unchanged.
func seedDemoAccounts(a *auth.Auth) {
	demoCtx, cancelDemo := context.WithCancel(context.Background())
	demo.Ensure(demoCtx, a)
	cancelDemo()
}

// wireTOTPKey installs AUTH_ENC_KEY when configured; a malformed key fails
// the boot rather than running half-encrypted. Empty keeps legacy storage.
func wireTOTPKey(a *auth.Auth, key []byte) error {
	if len(key) == 0 {
		return nil
	}
	if err := a.SetTOTPEncKey(key); err != nil {
		return fmt.Errorf("server: chi auth: %w", err)
	}
	return nil
}
