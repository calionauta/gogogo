// SCOPE:feature — Notes presence seam (fallback vs injected actor).
//
// It is the same seam the whiteboard uses (collab.PresenceSeam): one owner
// per document, whichever backend backs it. These tests pin the notes half of
// that contract, which is what makes "both collaborative features share one
// roster" a fact instead of a comment.
package handlers

import (
	"context"
	"slices"
	"testing"
)

type fakeNotesPresence struct {
	joined, left []string
	forgot       []string
	peers        []string
	leaveCtxErr  error
}

func (f *fakeNotesPresence) Join(_ context.Context, docID, clientID string) ([]string, error) {
	f.joined = append(f.joined, docID+"/"+clientID)
	return f.peers, nil
}

func (f *fakeNotesPresence) Leave(ctx context.Context, docID, clientID string) ([]string, error) {
	f.left = append(f.left, docID+"/"+clientID)
	f.leaveCtxErr = ctx.Err()
	return f.peers, nil
}

func (f *fakeNotesPresence) List(_ context.Context, _ string) ([]string, error) { return f.peers, nil }

func (f *fakeNotesPresence) ForgetDoc(_ context.Context, docID string) error {
	f.forgot = append(f.forgot, docID)
	return nil
}

// TestNotesPresenceSeam pins that SetPresence replaces the in-process
// fallback, that join/leave reach it with the document and connection id the
// SSE stream uses, and that a leave runs on a non-cancelled context — the
// stream's request context is already done when its teardown fires, and an
// actor Ask on it never reaches the grain (that bug left closed tabs on the
// roster until the whiteboard caught it in CI).
//
// RED-PROOFED: drop context.WithoutCancel from peerLeave/leaveNow and
// leaveCtxErr is context.Canceled; ignore SetPresence and joined stays empty.
func TestNotesPresenceSeam(t *testing.T) {
	h := newNotesHandler(t)
	fake := &fakeNotesPresence{peers: []string{"c2"}}
	h.SetPresence(fake)

	peers, err := h.presence.Join(context.Background(), "doc-a", "c1")
	if err != nil || !slices.Equal(peers, []string{"c2"}) {
		t.Fatalf("join = %v, %v; want [c2]", peers, err)
	}
	if !slices.Equal(fake.joined, []string{"doc-a/c1"}) {
		t.Fatalf("presence joins = %v", fake.joined)
	}

	// A cancelled request context (the teardown shape) must still reach the
	// presence backend: peerLeave strips the cancellation.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	h.peerLeave(cancelled, "doc-a", "c2")
	if fake.leaveCtxErr != nil {
		t.Fatalf("peerLeave saw a cancelled context: %v", fake.leaveCtxErr)
	}
	if !slices.Equal(fake.left, []string{"doc-a/c2"}) {
		t.Fatalf("presence leaves = %v", fake.left)
	}

	// leaveNow is the unconditional goodbye; it has the same context rule.
	fake.leaveCtxErr = nil
	h.leaveNow(cancelled, "doc-a", "c3")
	if fake.leaveCtxErr != nil {
		t.Fatalf("leaveNow saw a cancelled context: %v", fake.leaveCtxErr)
	}
	if !slices.Equal(fake.left, []string{"doc-a/c2", "doc-a/c3"}) {
		t.Fatalf("presence leaves after leaveNow = %v", fake.left)
	}

	// Nil keeps the documented fallback: a trimmed/disabled engine still
	// serves presence in-process.
	fallback := newNotesHandler(t)
	fallback.SetPresence(nil)
	peers, err = fallback.presence.Join(context.Background(), "doc-b", "c9")
	if err != nil || len(peers) != 0 {
		t.Fatalf("fallback join = %v, %v; want empty", peers, err)
	}
	got, lerr := fallback.presence.List(context.Background(), "doc-b")
	if lerr != nil || !slices.Equal(got, []string{"c9"}) {
		t.Fatalf("fallback list = %v, %v; want [c9]", got, lerr)
	}

	// A deleted note drops its roster.
	if ferr := fallback.presence.ForgetDoc(context.Background(), "doc-b"); ferr != nil {
		t.Fatalf("fallback forget: %v", ferr)
	}
	if left, _ := fallback.presence.List(context.Background(), "doc-b"); len(left) != 0 {
		t.Fatalf("roster survived ForgetDoc: %v", left)
	}
}

// TestNotesPeerNamesDoNotLeak pins the display-name map's lifetime: a leave
// forgets its own entry (the map is keyed by connection, so a long-lived
// process would otherwise grow one string per connection forever).
func TestNotesPeerNamesDoNotLeak(t *testing.T) {
	h := newNotesHandler(t)
	h.setPeerName("cli-1", "Demo One")
	if got := h.peerNameOf("cli-1"); got != "Demo One" {
		t.Fatalf("captured name = %q", got)
	}

	h.leaveNow(context.Background(), "doc-a", "cli-1")

	if got := h.peerNameOf("cli-1"); got != "cli-1" {
		t.Fatalf("name survived leave: %q (want the id fallback)", got)
	}
	h.peerNamesMu.Lock()
	n := len(h.peerNames)
	h.peerNamesMu.Unlock()
	if n != 0 {
		t.Fatalf("peerNames still holds %d entries after leave", n)
	}
}
