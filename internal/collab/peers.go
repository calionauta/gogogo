// SCOPE:layer=infra,removal=plugin — shared peer-set mechanics for collab features
package collab

import "sync"

// PeerSet tracks, per doc, the set of clientIDs on that doc's SSE stream.
// It is the authoritative source for the "X online" pill: every tab
// renders the count from broadcast events computed off this set, so all
// tabs agree even when an event is missed. Extracted from the whiteboard
// (its second copy in notes was the duplication the codebase audit
// flagged); both handlers keep their own broadcast wiring, only the set
// mechanics are shared.
type PeerSet struct {
	mu   sync.Mutex
	docs map[string]map[string]struct{}
}

// NewPeerSet returns an empty peer set.
func NewPeerSet() *PeerSet {
	return &PeerSet{docs: make(map[string]map[string]struct{})}
}

// Join registers clientID on docID and returns the clientIDs already
// present (the peers a new client should learn about). Called on SSE
// connect.
func (p *PeerSet) Join(docID, clientID string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	set := p.docs[docID]
	if set == nil {
		set = make(map[string]struct{})
		p.docs[docID] = set
	}
	others := make([]string, 0, len(set))
	for id := range set {
		if id != clientID {
			others = append(others, id)
		}
	}
	set[clientID] = struct{}{}
	return others
}

// Leave removes clientID from docID's set. Unconditional: callers keep
// their own reconnect guard (hub.IsRegistered) because the set cannot
// know a client re-registered during an EventSource reconnect.
func (p *PeerSet) Leave(docID, clientID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if set, ok := p.docs[docID]; ok {
		delete(set, clientID)
		if len(set) == 0 {
			delete(p.docs, docID)
		}
	}
}

// List returns the clientIDs on docID (including the caller).
func (p *PeerSet) List(docID string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	set := p.docs[docID]
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}
