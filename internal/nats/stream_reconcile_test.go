// SCOPE:plugin — stream reconciliation on an EXISTING stream (red-proof).
//
// THE BUG THESE TESTS EXIST FOR. Both stream creators compared the AddStream
// error to the literal "stream already exists". That message does not exist:
// the server answers "stream name already in use" (the sentinel
// ErrStreamNameAlreadyInUse), so the comparison never matched — dead code that
// only looked like handling. What it was meant to tolerate only happens when
// the CONFIG CHANGED (an identical re-add returns nil), i.e. exactly the
// upgrade case: a release that edits MaxAge/MaxBytes. Treated as fatal it
// silently disabled the CRUD publisher and failed the realtime broadcaster.
package nats_test

import (
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/internal/nats"
)

// TestEnsureStreamOnReconcilesChangedConfig is the red-proof: the same stream
// name with a different bound must succeed (and the new bound must take
// effect), not fail.
func TestEnsureStreamOnReconcilesChangedConfig(t *testing.T) {
	t.Parallel()
	h, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(h.Close)

	cfg := func(maxAge time.Duration) *natsio.StreamConfig {
		return &natsio.StreamConfig{
			Name: "RECONCILE", Subjects: []string{"reconcile.>"},
			Storage: natsio.FileStorage, MaxAge: maxAge,
		}
	}
	if err := nats.EnsureStreamOn(h.JS, cfg(24*time.Hour)); err != nil {
		t.Fatalf("first stream create: %v", err)
	}
	if err := nats.EnsureStreamOn(h.JS, cfg(48*time.Hour)); err != nil {
		t.Fatalf("re-add with a changed config (the upgrade case): %v", err)
	}
	info, ierr := h.JS.StreamInfo("RECONCILE")
	if ierr != nil {
		t.Fatalf("stream vanished: %v", ierr)
	}
	if info.Config.MaxAge != 48*time.Hour {
		t.Errorf("MaxAge = %v after reconcile, want the new 48h — a changed bound must take effect", info.Config.MaxAge)
	}
}

// TestEnsureStreamOnIsIdempotent pins the identical-config path: a plain
// restart must be a no-op, since that is the far more common case.
func TestEnsureStreamOnIsIdempotent(t *testing.T) {
	t.Parallel()
	h, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(h.Close)
	cfg := &natsio.StreamConfig{
		Name: "IDEMPOTENT", Subjects: []string{"idem.>"},
		Storage: natsio.FileStorage, MaxAge: time.Hour,
	}
	for i := range 3 {
		if err := nats.EnsureStreamOn(h.JS, cfg); err != nil {
			t.Fatalf("create %d: %v", i+1, err)
		}
	}
}

// TestEnsureStreamOnPassesRealErrorsThrough pins the other half: only the
// already-exists case is absorbed. A genuine misconfiguration must still fail,
// or a typo in a stream name would boot silently with no stream at all.
func TestEnsureStreamOnPassesRealErrorsThrough(t *testing.T) {
	t.Parallel()
	h, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(h.Close)
	// An invalid subject (spaces are not allowed) is a real error.
	bad := &natsio.StreamConfig{
		Name: "BADSUBJ", Subjects: []string{"not a subject"},
		Storage: natsio.FileStorage,
	}
	if err := nats.EnsureStreamOn(h.JS, bad); err == nil {
		t.Error("a genuine misconfiguration was absorbed as if the stream existed")
	}
}

// TestEnsureStreamToleratesExistingStream pins the process-global twin, whose
// doc comment claimed this behaviour while returning the error.
func TestEnsureStreamToleratesExistingStream(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(h.Close)
	if err := nats.EnsureStream("GLOBALREC", []string{"globalrec.>"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// A second call must succeed. (With an identical config AddStream returns
	// nil, so this pins idempotence rather than the reconcile path — the point
	// is that the "already exists" case is never surfaced as a failure.)
	if err := nats.EnsureStream("GLOBALREC", []string{"globalrec.>"}); err != nil {
		t.Fatalf("re-ensure of an existing stream failed: %v", err)
	}
}
