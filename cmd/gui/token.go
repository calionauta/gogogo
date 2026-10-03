// SCOPE:layer=feature,removal=feature — Session token persistence for the gogpu/ui POC
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tokenFileName is the session file inside the app's DataDir. It holds
// the PocketBase auth token minted at login ("remember me"). Mode 0600:
// the token is a bearer credential — anyone who reads it IS the user.
//
//nolint:gosec // G101: this is a filename, not a credential value.
const tokenFileName = ".gui-session-token"

// sessionFileMode restricts the persisted token to the owner: the
// token is a bearer credential, so group/other must have no access.
const sessionFileMode = 0o600

func tokenPath(dataDir string) string {
	return filepath.Join(dataDir, tokenFileName)
}

// saveToken persists the session token with owner-only permissions.
// Best-effort by design: callers log and continue on error (a missing
// "remember me" is an inconvenience, not a login failure).
func saveToken(path, token string) error {
	if token == "" {
		return fmt.Errorf("gui: refusing to persist an empty session token")
	}
	if err := os.WriteFile(path, []byte(token+"\n"), sessionFileMode); err != nil {
		return fmt.Errorf("gui: save session token: %w", err)
	}
	return nil
}

// loadToken reads a persisted session token. Empty (with error) when
// there is no session, the file is unreadable, or it holds nothing
// parseable — all three mean "show the login form".
func loadToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("gui: no saved session: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("gui: saved session is empty")
	}
	return token, nil
}

// clearToken removes the persisted session. Called on sign-out and when
// a saved token no longer resolves (expired/revoked) — a dead token
// must never linger only to fail again on the next launch.
func clearToken(path string) {
	_ = os.Remove(path)
}
