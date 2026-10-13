// SCOPE:feature — HTTP whiteboard peer bookkeeping (split for size)
package handlers

import (
	"context"
	"encoding/json"

	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/observability"
)

// peerJoin registers clientID on docID and returns the peers already
// there, so a new client seeds its count without waiting.
func (h *Handler) peerJoin(docID, clientID string) []string {
	return h.peers.Join(docID, clientID)
}

// peerLeave removes clientID, guarding against EventSource reconnect races:
// if the hub re-registered the same clientID already, the new handler owns
// it and no leave is broadcast (otherwise tabs oscillate 1→0→1→0).
func (h *Handler) peerLeave(ctx context.Context, docID, clientID string) {
	if h.hub.IsRegistered(clientID) {
		return
	}
	h.peers.Leave(docID, clientID)
	leaveMsg, lErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: clientID, Type: "leave"})
	if lErr != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", lErr)
		observability.Warn(ctx, "whiteboard: marshal leave", obsArgs...)
		return
	}
	h.hub.BroadcastExcept(leaveMsg, clientID)
	h.broadcastPeerCount(ctx, docID)
}

// broadcastPeerCount sends the authoritative peer set to every connected
// client. Clients render "X online" directly from it instead of counting
// locally, so every tab agrees even after missed leaves or reconnects.
func (h *Handler) broadcastPeerCount(ctx context.Context, docID string) {
	msg, err := json.Marshal(collab.PresenceMsg{Doc: docID, Type: "count", Peers: h.peers.List(docID)})
	if err != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", err)
		observability.Warn(ctx, "whiteboard: marshal peer count", obsArgs...)
		return
	}
	//nolint:contextcheck // SSEHub Broadcast takes no context; this is a
	// best-effort fan-out, failures surface as the next poll.
	h.hub.Broadcast(msg)
}
