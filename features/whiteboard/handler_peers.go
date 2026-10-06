// SCOPE:layer=feature,removal=feature — Collaborative whiteboard (Loro CRDT canvas)
// Depends on: internal/collab/ (CRDT), internal/queue/ (SSE hub).
//
// Presence/peer bookkeeping, split out of handler.go to stay within the
// 500-line budget. The peer set is the authoritative source for the "X online"
// count, so it is kept together and separate from the HTTP/SSE route handlers.
package whiteboard

import (
	"encoding/json"
	"log/slog"

	"github.com/calionauta/gogogo/internal/collab"
)

// peerJoin registers clientID as connected to docID and returns the list
// of clientIDs that were ALREADY on the doc (the peers the new client
// should learn about). It is called on SSE connect. The returned slice
// (possibly empty) is sent to the new client as a "snapshot" presence
// event so it can seed its peer count without waiting for future joins.
func (h *Handler) peerJoin(docID, clientID string) []string {
	h.peersMu.Lock()
	defer h.peersMu.Unlock()
	set := h.peers[docID]
	if set == nil {
		set = make(map[string]struct{})
		h.peers[docID] = set
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

// peerLeave removes clientID from docID's peer set and broadcasts a
// "leave" to every remaining client. Called on SSE disconnect.
//
// Before removing, it checks whether the clientID is still registered in
// the SSE hub (meaning a new handler re-registered it during an EventSource
// reconnect). If so, the leave is skipped — the new handler's peerJoin
// already re-added the client, and broadcasting a "leave" would make other
// tabs briefly drop the peer count, then re-add it on the next "join".
// This prevents the 1→0→1→0 oscillation seen in the reconnect race.
func (h *Handler) peerLeave(docID, clientID string) {
	// Guard: if the client reconnected (a new handler registered the same
	// clientID), don't remove it from the peer set. The new handler's
	// peerJoin already added it, and broadcasting a "leave" would make
	// other tabs briefly drop their peer count (fluctuation on reconnect).
	if h.hub.IsRegistered(clientID) {
		return
	}
	h.peersMu.Lock()
	if set, ok := h.peers[docID]; ok {
		delete(set, clientID)
		if len(set) == 0 {
			delete(h.peers, docID)
		}
	}
	h.peersMu.Unlock()
	leaveMsg, lErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: clientID, Type: "leave"})
	if lErr != nil {
		slog.Warn("whiteboard: marshal leave", "error", lErr)
		return
	}
	h.hub.BroadcastExcept(leaveMsg, clientID)
	// Re-broadcast the authoritative count to the remaining clients so the
	// "X online" number drops consistently everywhere (no stale +1 from a
	// missed leave).
	h.broadcastPeerCount(docID)
}

// peerList returns the current set of clientIDs connected to docID
// (including the caller). Callers must NOT hold peersMu.
func (h *Handler) peerList(docID string) []string {
	h.peersMu.Lock()
	defer h.peersMu.Unlock()
	set := h.peers[docID]
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// broadcastPeerCount sends the authoritative, full peer set for docID to
// every connected client (including the originator). Clients render the
// "X online" count directly from this event instead of incrementing
// counters client-side, so every tab agrees on the same number even when
// a leave is missed or a tab reconnects with a fresh client id.
func (h *Handler) broadcastPeerCount(docID string) {
	msg, err := json.Marshal(collab.PresenceMsg{Doc: docID, Type: "count", Peers: h.peerList(docID)})
	if err != nil {
		slog.Warn("whiteboard: marshal peer count", "error", err)
		return
	}
	h.hub.Broadcast(msg)
}
