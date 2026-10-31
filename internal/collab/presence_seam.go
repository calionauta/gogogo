// SCOPE:plugin — Per-document presence seam (PeerSet fallback / actor adapter).
package collab

import "context"

// Presence is the whiteboard/notes presence surface: who is on one document.
// The in-process PeerSet implements it as the fallback; internal/goakt
// implements it with one actor per document when GoAkt is enabled. The
// interface lives here (not in a feature) so a trimmed tree that drops
// GoAkt still compiles against the fallback.
//
// Four methods, not the usual 1-3: this is a roster lifecycle, not a knob. A
// narrower interface would push the caller into type-asserting on
// "is this an actor?" to decide whether ForgetDoc exists, which is exactly the
// coupling the seam removes. All four are used by BOTH features, none is a
// pass-through, and there is no optional method.
type PresenceSeam interface {
	// Join registers clientID on docID and returns the peers already there.
	Join(ctx context.Context, docID, clientID string) ([]string, error)
	// Leave removes clientID and returns the remaining peers.
	Leave(ctx context.Context, docID, clientID string) ([]string, error)
	// List returns the peers on docID.
	List(ctx context.Context, docID string) ([]string, error)
	// ForgetDoc drops docID's whole roster: the document was deleted, so a
	// reconnect to the same id must not resurrect its departed client ids.
	ForgetDoc(ctx context.Context, docID string) error
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

func (p peerSeam) ForgetDoc(_ context.Context, docID string) error {
	p.peers.ForgetDoc(docID)
	return nil
}
