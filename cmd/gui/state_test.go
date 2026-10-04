// SCOPE:layer=feature,removal=feature — Tests for the native window's backend wiring
package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/db"
	"github.com/calionauta/gogogo/features/store/pbstore"
)

// The POC's whole claim is that the backend is reachable without HTTP.
// These tests exercise the real windowState against a real PocketBase —
// no native window, no rendering — so they run headless in CI.

const (
	nativeEmail    = "native@demo.app"
	nativePassword = "testpassword"
)

func newNativeState(t *testing.T) *windowState {
	t.Helper()

	tmpDir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	cfg := &config.Config{
		Host:          "127.0.0.1",
		Dev:           true,
		DataDir:       tmpDir,
		DBPath:        tmpDir + "/app.db",
		EncryptionKey: hex.EncodeToString(key),
	}

	app, err := db.Init(cfg)
	if err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	if err = app.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err = db.ApplySeeds(app, true); err != nil {
		t.Fatalf("ApplySeeds: %v", err)
	}

	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(nativeEmail)
	rec.SetPassword(nativePassword)
	rec.Set("verified", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save user: %v", err)
	}

	return newState(app, pbstore.New(app, todosCollection))
}

func signInNative(t *testing.T, s *windowState) {
	t.Helper()
	s.setEmail(nativeEmail)
	s.setPassword(nativePassword)
	s.signIn()
	if snap := s.snapshot(); !snap.signedIn {
		t.Fatalf("sign-in failed: %s", snap.authErr)
	}
}

// The end-to-end claim: sign in through auth.Login (no cookie, no
// RequestEvent), add a todo through the same EntityStore the HTTP
// handlers use, and read it back — all without an HTTP server running.
func TestNativeSignInAddAndList(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)

	if snap := s.snapshot(); snap.ownerID == "" {
		t.Fatal("ownerID is empty after a successful sign-in")
	}
	if snap := s.snapshot(); snap.authErr != "" {
		t.Fatalf("unexpected auth error: %s", snap.authErr)
	}
	s.mu.Lock()
	lingeringPassword := s.password
	s.mu.Unlock()
	if lingeringPassword != "" {
		t.Error("password must not linger in window state after sign-in")
	}

	s.setDraft("written from the native window")
	s.add()

	snap := s.snapshot()
	if snap.err != "" {
		t.Fatalf("add: %s", snap.err)
	}
	if len(snap.items) != 1 {
		t.Fatalf("got %d items, want 1", len(snap.items))
	}
	if snap.items[0].Title != "written from the native window" {
		t.Errorf("title = %q", snap.items[0].Title)
	}

	s.toggle(snap.items[0].ID)
	if snap = s.snapshot(); snap.err != "" {
		t.Fatalf("toggle: %s", snap.err)
	}
	if snap = s.snapshot(); !snap.items[0].Completed {
		t.Error("toggle did not persist to the store")
	}

	s.remove(snap.items[0].ID)
	if snap = s.snapshot(); snap.err != "" {
		t.Fatalf("remove: %s", snap.err)
	}
	if snap = s.snapshot(); len(snap.items) != 0 {
		t.Errorf("got %d items after delete, want 0", len(snap.items))
	}
}

// A rejected login must not leave the window in a signed-in state that
// would then run unscoped store calls.
func TestNativeBadLoginStaysSignedOut(t *testing.T) {
	s := newNativeState(t)

	s.setEmail(nativeEmail)
	s.setPassword("wrong")
	s.signIn()

	snap := s.snapshot()
	if snap.signedIn {
		t.Error("window reports signed in after a failed login")
	}
	if snap.ownerID != "" {
		t.Errorf("ownerID = %q, want empty", snap.ownerID)
	}

	// Mutations must be no-ops without an owner.
	s.setDraft("should never persist")
	s.add()
	if snap := s.snapshot(); len(snap.items) != 0 {
		t.Errorf("a signed-out window wrote %d items", len(snap.items))
	}
}

// Sign-out must drop the owner so a later sign-in cannot silently
// reuse the previous user's scope.
func TestNativeSignOutClearsOwner(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)

	s.signOut()
	snap := s.snapshot()
	if snap.signedIn || snap.ownerID != "" || len(snap.items) != 0 {
		t.Errorf("sign-out left state: signedIn=%v owner=%q items=%d",
			snap.signedIn, snap.ownerID, len(snap.items))
	}
}

// Two windows over the SAME store (two frontends, one collection):
// A's write becomes visible to B on B's next refresh — the poll path
// in main.go. Last writer wins on conflict, same as the web tier
// (PocketBase has no merge; CRDTStore would be the answer when merge
// semantics matter).
func TestNativeCrossStateVisibilityAndLastWriteWins(t *testing.T) {
	a := newNativeState(t)
	// Second window, same app handle + same collection: exactly what
	// the web app and the native window share in production.
	b := newState(a.app, pbstore.New(a.app, todosCollection))

	signInNative(t, a)
	b.setEmail(nativeEmail)
	b.setPassword(nativePassword)
	b.signIn()
	if snap := b.snapshot(); !snap.signedIn {
		t.Fatalf("B sign-in failed: %s", snap.authErr)
	}

	a.setDraft("from A")
	a.add()
	if snap := a.snapshot(); len(snap.items) != 1 {
		t.Fatalf("A has %d items, want 1", len(snap.items))
	}
	id := a.snapshot().items[0].ID

	// B hasn't refreshed: sees nothing. After refresh: sees A's row.
	if snap := b.snapshot(); len(snap.items) != 0 {
		t.Fatalf("B sees %d items before refresh, want 0", len(snap.items))
	}
	b.refresh()
	snap := b.snapshot()
	if len(snap.items) != 1 || snap.items[0].Title != "from A" {
		t.Fatalf("B after refresh: %+v, want A's row", snap.items)
	}

	// Conflict: B completes, A (stale) flips back. Last write wins —
	// deterministic here because the test serializes the two writes.
	b.toggle(id)
	a.refresh()
	a.toggle(id) // A's view said completed=true, so this writes false
	b.refresh()
	if snap := b.snapshot(); snap.items[0].Completed {
		t.Error("last write did not win: B still sees completed=true")
	}
	a.refresh()
	if snapA, snapB := a.snapshot(), b.snapshot(); snapA.items[0].Completed != snapB.items[0].Completed {
		t.Error("A and B converged differently after the same writes")
	}
}

// The threading contract (see state.go): UI callbacks, the poll
// goroutine, and snapshots run concurrently. Under -race this must be
// clean — refresh/add/toggle/signOut interleaved with snapshot reads.
func TestNativeConcurrentAccessNoRace(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)
	s.setDraft("seed")
	s.add()
	id := s.snapshot().items[0].ID

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 25 {
				s.refresh()
				_ = s.snapshot()
				s.toggle(id)
			}
		})
	}
	wg.Wait()
	if snap := s.snapshot(); snap.err != "" {
		t.Fatalf("concurrent use left error: %s", snap.err)
	}
}
