// SCOPE:layer=feature,removal=feature — Shared plain-text notes (server-owned Loro Text)
// Presence/peer bookkeeping, split out of handler.go. Same contract as the
// whiteboard's handler_peers.go (join → others, count → all, leave +
// recount on disconnect, reconnect-race guard), minus cursor coordinates:
// a textarea has no canvas to place remote cursors on, so presence here is
// the "X online" pill only.
package notes

import (
	"encoding/json"
	"log/slog"

	"github.com/calionauta/gogogo/internal/collab"
)

// peerJoin registers clientID as connected to docID and returns the list
// of clientIDs already on the doc. Called on SSE connect.
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
// "leave" plus a recount. Skipped when the clientID re-registered during
// an EventSource reconnect (same race guard as the whiteboard).
func (h *Handler) peerLeave(docID, clientID string) {
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
		slog.Warn("notes: marshal leave", "error", lErr)
		return
	}
	h.hub.BroadcastExcept(leaveMsg, clientID)
	h.broadcastPeerCount(docID)
}

// peerList returns the current clientIDs on docID. Callers must NOT hold
// peersMu.
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

// broadcastPeerCount sends the authoritative full peer set to every
// connected client. Clients render the pill from this event instead of
// counting joins locally, so all tabs agree even when an event is missed.
func (h *Handler) broadcastPeerCount(docID string) {
	msg, err := json.Marshal(collab.PresenceMsg{Doc: docID, Type: "count", Peers: h.peerList(docID)})
	if err != nil {
		slog.Warn("notes: marshal peer count", "error", err)
		return
	}
	h.hub.Broadcast(msg)
}
