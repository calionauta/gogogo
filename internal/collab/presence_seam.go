// SCOPE:plugin — Per-document presence seam (PeerSet fallback / actor adapter).
package collab

import "context"

// Presence is the whiteboard/notes presence surface: who is on one document.
// The in-process PeerSet implements it as the fallback; internal/goakt
// implements it with one actor per document when GoAkt is enabled. The
// interface lives here (not in a feature) so a trimmed tree that drops
// GoAkt still compiles against the fallback.
type PresenceSeam interface {
	// Join registers clientID on docID and returns the peers already there.
	Join(ctx context.Context, docID, clientID string) ([]string, error)
	// Leave removes clientID and returns the remaining peers.
	Leave(ctx context.Context, docID, clientID string) ([]string, error)
	// List returns the peers on docID.
	List(ctx context.Context, docID string) ([]string, error)
}

// peerPresence adapts the mutex-backed PeerSet to Presence. Every call is
// synchronous, which is the fallback's whole point: no mailbox, no timeout.
type peerSeam struct{ peers *PeerSet }

// NewPeerPresence builds the in-process fallback.
func NewPeerPresence() PresenceSeam { return peerSeam{peers: NewPeerSet()} }

func (p peerSeam) Join(_ context.Context, docID, clientID string) ([]string, error) {
	return p.peers.Join(docID, clientID), nil
}

func (p peerSeam) Leave(_ context.Context, docID, clientID string) ([]string, error) {
	p.peers.Leave(docID, clientID)
	return p.peers.List(docID), nil
}

func (p peerSeam) List(_ context.Context, docID string) ([]string, error) {
	return p.peers.List(docID), nil
}
