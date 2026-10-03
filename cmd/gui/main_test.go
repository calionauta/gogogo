// SCOPE:layer=feature,removal=feature — Tests for the gui poll loop and sign-out wiring.
package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// The poll loop is the POC's realtime-read path: refresh off the UI
// thread, then a serialized rebuild, then session persistence —
// repeating until done closes. With a 10ms tick this proves the full
// wiring headless: ticks fire, refreshUI runs, and another writer's
// rows appear without user action.
func TestPollLoopRefreshesAndRebuilds(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)

	// Second window, same backend (the "other frontend"), signed in
	// BEFORE the row exists: only the poll can make it appear.
	s2 := newState(s.app, s.st)
	signInSecondNative(t, s2)
	if n := len(s2.snapshot().items); n != 0 {
		t.Fatalf("setup: s2 sees %d items, want 0", n)
	}

	var rebuilds atomic.Int64
	done := make(chan struct{})
	defer close(done)
	go startPollLoop(s2, filepath.Join(t.TempDir(), "sess"), func() {
		rebuilds.Add(1)
	}, 10*time.Millisecond, done)

	// The "web app" writes while the poll runs.
	s.setDraft("polled-row")
	s.add()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if rebuilds.Load() >= 1 && len(s2.snapshot().items) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("poll loop: rebuilds=%d items=%d, want >=1 rebuild and 1 item",
				rebuilds.Load(), len(s2.snapshot().items))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The poll loop must stop when done closes — no goroutine leak.
// Quiescence check, not zero-delta: one tick already selected when
// done closes may still run its rebuild, but after that the count
// must freeze. A loop ignoring done would keep growing ~1 rebuild
// per 10ms tick and fail loudly.
func TestPollLoopStopsOnDone(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)

	var rebuilds atomic.Int64
	done := make(chan struct{})
	go startPollLoop(s, filepath.Join(t.TempDir(), "sess"), func() {
		rebuilds.Add(1)
	}, 10*time.Millisecond, done)

	time.Sleep(50 * time.Millisecond)
	close(done)
	time.Sleep(60 * time.Millisecond) // let an in-flight tick land
	frozen := rebuilds.Load()
	time.Sleep(60 * time.Millisecond)
	if got := rebuilds.Load(); got != frozen {
		t.Errorf("poll loop kept rebuilding after done closed: %d -> %d", frozen, got)
	}
}

// makeSignOut is the exact wiring behind the Sign out button: state
// reset AND session-file removal. A stale file would silently re-login
// on the next launch after an explicit sign-out.
func TestMakeSignOutClearsStateAndFile(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)
	path := filepath.Join(t.TempDir(), "sess")
	persistSessionIfNeeded(s, path)
	if _, err := loadToken(path); err != nil {
		t.Fatalf("setup: session was not persisted: %v", err)
	}

	makeSignOut(s, path)()

	if snap := s.snapshot(); snap.signedIn || snap.ownerID != "" {
		t.Errorf("sign-out left signedIn=%v owner=%q", snap.signedIn, snap.ownerID)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("sign-out left the session file behind")
	}
}

// persistSessionIfNeeded saves the fresh login token once and is a
// no-op afterwards (same token) or when signed out.
func TestPersistSessionSavesFreshTokenOnce(t *testing.T) {
	s := newNativeState(t)
	path := filepath.Join(t.TempDir(), "sess")

	persistSessionIfNeeded(s, path) // signed out: must not create a file
	if _, err := os.Stat(path); err == nil {
		t.Error("persist created a session file while signed out")
	}

	signInNative(t, s)
	persistSessionIfNeeded(s, path)
	first, err := loadToken(path)
	if err != nil || first == "" {
		t.Fatalf("persist did not save the token: %v", err)
	}

	info1, _ := os.Stat(path)
	persistSessionIfNeeded(s, path) // same token: no rewrite
	info2, _ := os.Stat(path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("persist rewrote an unchanged session file")
	}
}

func signInSecondNative(t *testing.T, s *windowState) {
	t.Helper()
	s.setEmail(nativeEmail)
	s.setPassword(nativePassword)
	s.signIn()
	if snap := s.snapshot(); !snap.signedIn {
		t.Fatalf("second sign-in failed: %s", snap.authErr)
	}
}
