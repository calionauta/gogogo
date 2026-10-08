// SCOPE:layer=feature,removal=feature — Shared plain-text notes (server-owned Loro Text)
// Presence/peer bookkeeping. Same contract as the whiteboard's
// handler_peers.go, sharing the collab.PeerSet mechanics: join to the
// others, authoritative count to all, leave + recount on disconnect with
// the reconnect-race guard. No cursor coordinates (see Handler doc).
package notes

import (
	"encoding/json"
	"log/slog"

	"github.com/calionauta/gogogo/internal/collab"
)

// peerJoin registers clientID as connected to docID and returns the list
// of clientIDs already on the doc. Called on SSE connect.
func (h *Handler) peerJoin(docID, clientID string) []string {
	return h.peers.Join(docID, clientID)
}

// setPeerName captures the display name for a clientID; peerLeave runs
// without the request, so without this the leave event could not carry
// the same identity the join announced.
func (h *Handler) setPeerName(clientID, name string) {
	h.peerNamesMu.Lock()
	defer h.peerNamesMu.Unlock()
	h.peerNames[clientID] = name
}

// peerNameOf returns the captured display name, falling back to the id.
func (h *Handler) peerNameOf(clientID string) string {
	h.peerNamesMu.Lock()
	defer h.peerNamesMu.Unlock()
	if name, ok := h.peerNames[clientID]; ok {
		return name
	}
	return clientID
}

// peerLeave removes clientID from docID's peer set and broadcasts a
// "leave" plus a recount. Skipped when the clientID re-registered during
// an EventSource reconnect.
func (h *Handler) peerLeave(docID, clientID string) {
	if h.hub.IsRegistered(clientID) {
		return
	}
	h.peers.Leave(docID, clientID)
	name := h.peerNameOf(clientID)
	h.peerNamesMu.Lock()
	delete(h.peerNames, clientID)
	h.peerNamesMu.Unlock()
	leaveMsg, lErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: name, Type: "leave"})
	if lErr != nil {
		slog.Warn("notes: marshal leave", "error", lErr)
		return
	}
	h.hub.BroadcastExcept(leaveMsg, clientID)
	h.broadcastPeerCount(docID)
}

// peerList returns the current clientIDs on docID (including the caller).
func (h *Handler) peerList(docID string) []string {
	return h.peers.List(docID)
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
