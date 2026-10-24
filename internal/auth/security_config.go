// SCOPE:core — Auth deployment switches: public base URL and signup gate.
//
// Both are set once at boot, before the HTTP surface starts. Keeping them
// here (instead of reading env inside the auth stack) gives the config
// package one source of truth and lets a test exercise the exact production
// wiring without touching process globals.
package auth

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrSignupClosed is returned when public account creation is disabled and a
// caller tries to provision a new account (password signup or first OAuth
// login). Existing accounts keep signing in normally.
var ErrSignupClosed = errors.New("auth: signup closed")

// ErrPasswordTooLong bounds the password input. Argon2's memory cost is
// constant in input length, but an unbounded field is still needless work
// and an easy way to make password policy tests ambiguous.
var ErrPasswordTooLong = errors.New("auth: password too long")

// maxPasswordLen is intentionally generous: passphrases are encouraged,
// while a megabyte-scale secret is not a password.
const maxPasswordLen = 1024

// SetAppURL installs the public origin used by magic links. It must be an
// absolute http(s) URL with a host and no userinfo. Empty is allowed and
// means "no out-of-band links"; magic-link requests then fail closed instead
// of trusting the request Host.
func (a *Auth) SetAppURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		a.mu.Lock()
		a.appURL = ""
		a.mu.Unlock()
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("auth: APP_URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("auth: APP_URL must be an absolute http(s) URL without userinfo")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("auth: APP_URL must be an origin (no path, query or fragment)")
	}
	// A production magic link carries a login token; plain HTTP would put it
	// on the wire. Loopback stays allowed for local desktop/dev.
	if a.secure && u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return errors.New("auth: APP_URL must be https in production")
	}
	a.mu.Lock()
	a.appURL = strings.TrimRight(u.String(), "/")
	a.mu.Unlock()
	return nil
}

// isLoopbackHost reports whether host is loopback or a localhost name.
func isLoopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// publicBaseURL returns the configured public origin, or "" when links are
// disabled. Callers must not fall back to r.Host.
func (a *Auth) publicBaseURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.appURL
}

// SetSignupEnabled opens or closes public account creation. BootChi wires
// this from config; production defaults to closed.
func (a *Auth) SetSignupEnabled(enabled bool) {
	a.mu.Lock()
	a.signupDisabled = !enabled
	a.mu.Unlock()
}

// signupClosed reports whether new accounts may be created.
func (a *Auth) signupClosed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.signupDisabled
}
