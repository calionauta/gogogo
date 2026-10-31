// SCOPE:feature — Whiteboard presence seam (fallback vs injected actor).
package handlers

import (
	"context"
	"slices"
	"testing"
)

type fakePresence struct {
	joined, left []string
	peers        []string
}

func (f *fakePresence) Join(_ context.Context, docID, clientID string) ([]string, error) {
	f.joined = append(f.joined, docID+"/"+clientID)
	return f.peers, nil
}

func (f *fakePresence) Leave(_ context.Context, docID, clientID string) ([]string, error) {
	f.left = append(f.left, docID+"/"+clientID)
	return f.peers, nil
}
func (f *fakePresence) List(_ context.Context, _ string) ([]string, error) { return f.peers, nil }

// TestWhiteboardPresenceSeam pins that SetPresence replaces the fallback and
// that join/leave call it with the document and client id the SSE stream uses.
// Nil keeps the fallback (the trimmed/disabled path).
func TestWhiteboardPresenceSeam(t *testing.T) {
	h := newWBHandler(t)
	fake := &fakePresence{peers: []string{"c2"}}
	h.SetPresence(fake)
	peers, err := h.peerJoin(context.Background(), "doc-a", "c1")
	if err != nil || !slices.Equal(peers, []string{"c2"}) {
		t.Fatalf("join = %v, %v; want [c2]", peers, err)
	}
	if !slices.Equal(fake.joined, []string{"doc-a/c1"}) {
		t.Fatalf("presence joins = %v", fake.joined)
	}
	if _, lerr := h.presence.Leave(context.Background(), "doc-a", "c1"); lerr != nil {
		t.Fatalf("leave: %v", lerr)
	}
	if !slices.Equal(fake.left, []string{"doc-a/c1"}) {
		t.Fatalf("presence leaves = %v", fake.left)
	}

	// Nil is the documented fallback: a handler built with the default
	// PeerSet keeps working when the actor is absent.
	fallback := newWBHandler(t)
	fallback.SetPresence(nil)
	peers, err = fallback.peerJoin(context.Background(), "doc-b", "c9")
	if err != nil || len(peers) != 0 {
		t.Fatalf("fallback join = %v, %v; want empty", peers, err)
	}
	got, lerr := fallback.presence.List(context.Background(), "doc-b")
	if lerr != nil || !slices.Equal(got, []string{"c9"}) {
		t.Fatalf("fallback list = %v, %v; want [c9]", got, lerr)
	}
}
