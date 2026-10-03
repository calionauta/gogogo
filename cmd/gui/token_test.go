// SCOPE:layer=feature,removal=feature — Tests for session token persistence.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Round-trip: save then load returns the same token, and the file is
// owner-only (0600) — the token is a bearer credential.
func TestTokenSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess")
	if err := saveToken(path, "tok123"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadToken(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "tok123" {
		t.Errorf("token = %q, want tok123", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file perm = %o, want 600", perm)
	}
}

// Refusing an empty token fail-fast: persisting "" would create a
// session file that can never resolve, failing on every future launch.
func TestTokenSaveRefusesEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess")
	if err := saveToken(path, ""); err == nil {
		t.Error("saveToken(\"\") = nil, want error")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("empty token created a session file")
	}
}

// Missing, unreadable, and blank sessions all mean "show login" —
// loadToken errors and restoreSession leaves the window signed out.
func TestTokenLoadMissingMeansLogin(t *testing.T) {
	s := newNativeState(t)
	if _, err := loadToken(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("loadToken(missing) = nil, want error")
	}
	blank := filepath.Join(t.TempDir(), "blank")
	if err := os.WriteFile(blank, []byte("  \n"), 0o600); err != nil {
		t.Fatalf("write blank: %v", err)
	}
	restoreSession(s, blank)
	if snap := s.snapshot(); snap.signedIn {
		t.Error("blank session file signed the window in")
	}
}

// A garbage token must clear the file and leave login showing —
// never a stuck "signed in but empty" frame, never a retry loop on
// the next launch.
func TestRestoreSessionClearsDeadToken(t *testing.T) {
	s := newNativeState(t)
	path := filepath.Join(t.TempDir(), "sess")
	if err := saveToken(path, "dead-token"); err != nil {
		t.Fatalf("save: %v", err)
	}
	restoreSession(s, path)
	if snap := s.snapshot(); snap.signedIn {
		t.Error("dead token signed the window in")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("dead token file was not cleared")
	}
}

// A live token restores the session without a password: sign-in,
// owner, and data all present on the first frame.
func TestRestoreSessionAdoptsLiveToken(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)
	s.setDraft("pre-existing")
	s.add()
	if snap := s.snapshot(); len(snap.items) != 1 {
		t.Fatalf("setup: %d items, want 1", len(snap.items))
	}
	s.mu.Lock()
	token := s.token
	s.mu.Unlock()
	if token == "" {
		t.Fatal("signed-in state has no token to persist")
	}

	path := filepath.Join(t.TempDir(), "sess")
	if err := saveToken(path, token); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Fresh window, same backend: must come up signed in with data.
	s2 := newState(s.app, s.st)
	restoreSession(s2, path)
	snap := s2.snapshot()
	if !snap.signedIn {
		t.Fatal("live token did not restore the session")
	}
	if snap.ownerID == "" {
		t.Error("restored session has no owner")
	}
	if len(snap.items) == 0 {
		t.Error("restored session loaded no todos")
	}
}
