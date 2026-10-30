// SCOPE:core — Distribution and auth mode constants, plus the boot-time
// validation that refuses an incoherent or unsafe pair.
package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Distribution gate (LICENSE_MODE). It gates the app, never local accounts:
// identity is always local, activation only decides whether the app runs.
const (
	LicenseNone     = "none"    // no activation: install and use
	LicenseRequired = "license" // owner-minted token, pasted at /activate
	LicenseAccount  = "account" // token bound to a server user id
)

// Auth mode (AUTH_MODE). "open" is the no-login distribution: one local
// owner, no session, no signup.
const (
	AuthLocal = "local"
	AuthOpen  = "open"
)

// ValidateModes normalises the mode strings and refuses combinations that
// would either ship a half-open gate or expose a nameless owner to the
// network. BootChi calls it, so a bad env fails before anything listens.
func (c *Config) ValidateModes() error {
	switch strings.ToLower(strings.TrimSpace(c.LicenseMode)) {
	case "", LicenseNone:
		c.LicenseMode = LicenseNone
	case LicenseRequired, LicenseAccount:
		c.LicenseMode = strings.ToLower(strings.TrimSpace(c.LicenseMode))
	default:
		return fmt.Errorf("config: LICENSE_MODE=%q (want %s | %s | %s)",
			c.LicenseMode, LicenseNone, LicenseRequired, LicenseAccount)
	}
	// A gated shell (the desktop) needs the verification key. A web deploy in
	// account mode is an issuer, not a gate: it needs the private key instead.
	if c.LicenseMode != LicenseNone {
		if c.LicenseGate && strings.TrimSpace(c.LicensePublicKey) == "" {
			return fmt.Errorf(
				"config: LICENSE_MODE=%s with the gate needs LICENSE_PUBLIC_KEY "+
					"(cmd/license keygen prints it)", c.LicenseMode)
		}
		if !c.LicenseGate && strings.TrimSpace(c.LicensePrivateKey) == "" &&
			strings.TrimSpace(c.LicensePublicKey) == "" {
			return fmt.Errorf(
				"config: LICENSE_MODE=%s needs LICENSE_PRIVATE_KEY (issuer) or "+
					"LICENSE_PUBLIC_KEY (gate)", c.LicenseMode)
		}
	}

	switch strings.ToLower(strings.TrimSpace(c.AuthMode)) {
	case "", AuthLocal:
		c.AuthMode = AuthLocal
	case AuthOpen:
		c.AuthMode = AuthOpen
	default:
		return fmt.Errorf("config: AUTH_MODE=%q (want %s | %s)", c.AuthMode, AuthLocal, AuthOpen)
	}
	// Open auth means every request is the local owner. Served beyond
	// loopback that is an unauthenticated instance for anyone who can reach
	// the port, so it needs an explicit opt-in.
	if c.AuthMode == AuthOpen && !c.AllowOpenRemote && !isLoopbackHost(c.Host) {
		return errors.New("config: AUTH_MODE=open has no login; bind HOST=127.0.0.1 or set AUTH_OPEN_ALLOW_REMOTE=true")
	}
	return nil
}

// splitCSV trims a comma list and drops empties (CLOUD_SYNC_ALLOW).
func splitCSV(raw string) []string {
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isLoopbackHost reports whether a listen host stays on the machine.
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || h == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
