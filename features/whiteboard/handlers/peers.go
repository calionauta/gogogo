// SCOPE:feature — HTTP whiteboard peer bookkeeping (split for size)
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/observability"
)

// SetPresence swaps the presence backend. The composition root installs the
// shared federated seam (actor-backed when GoAkt runs, PeerSet otherwise), so
// this feature never owns a roster of its own. Nil keeps the built-in fallback:
// a trimmed or engine-less tree still serves presence locally.
func (h *Handler) SetPresence(p collab.PresenceSeam) {
	if p != nil {
		h.presence = p
	}
}

// peerLeave removes clientID, guarding against EventSource reconnect races:
// if the hub re-registered the same clientID already, the new handler owns
// it and no leave is broadcast (otherwise tabs oscillate 1→0→1→0).
func (h *Handler) peerLeave(ctx context.Context, docID, clientID string) {
	// The stream's request context is already cancelled when this runs; an
	// actor Ask on it would fail and leave the closed tab on the roster.
	ctx = context.WithoutCancel(ctx)
	if h.hub.IsRegistered(clientID) {
		return
	}
	peers, err := h.presence.Leave(ctx, docID, clientID)
	if err != nil {
		collab.WarnPresence(ctx, "whiteboard", "leave failed", docID, err)
		return
	}
	collab.BroadcastPresence(ctx, h.hub, "whiteboard", docID, clientID,
		collab.PresenceMsg{Doc: docID, User: clientID, Type: "leave"})
	// The Leave answer IS the authoritative remaining set: re-listing would
	// pay a second Ask for the same truth.
	collab.BroadcastRoster(ctx, h.hub, "whiteboard", docID, peers)
}

// broadcastPeerCount sends the authoritative peer set for THIS doc to
// the clients watching it. Doc-scoped: a global broadcast merged every
// doc's roster into one, so joining docA inflated docB's "X online".
func (h *Handler) broadcastPeerCount(ctx context.Context, docID string) {
	peers, err := h.presence.List(ctx, docID)
	if err != nil {
		collab.WarnPresence(ctx, "whiteboard", "list failed", docID, err)
		return
	}
	collab.BroadcastRoster(ctx, h.hub, "whiteboard", docID, peers)
}

// handleStream serves the board's SSE stream: session frame, live roster, and
// the initial shape set, then hands off to the hub's heartbeat loop.
//
// It lives beside the presence helpers because everything it sends is either a
// presence frame or the roster it seeds — the channel and the bookkeeping are
// one subject, and splitting a 60-line function across files is not a seam.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		// The stream carries the board body (shapes + peers). Shared means
		// every signed-in user reads it, never an anonymous tab; 401 stops
		// EventSource from reconnecting in a loop.
		writeText(w, http.StatusUnauthorized, "sign in to read")
		return
	}
	docID := chi.URLParam(r, "docID")
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	authed := sess != nil

	if serr := h.hub.ServeStream(w, r, clientID, docID,
		func(send func([]byte) error) {
			h.seedStream(r, send, docID, clientID, authed)
		},
		//nolint:contextcheck // SSE-sink boundary: the leave chain ends in
		// hub.BroadcastToDoc/BroadcastExceptToDoc, which take no context
		// (fan-out to other connections; a disconnect surfaces on their next
		// failed write). Growing ctx on the hub is a repo-wide refactor.
		func() { h.peerLeave(r.Context(), docID, clientID) }); serr != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "doc", docID, "error", serr)
		observability.Warn(r.Context(), "whiteboard: stream failed", obsArgs...)
		return
	}
}

// seedStream sends everything a joining tab needs on its own connection: the
// session frame, the join announcement, the roster it walked into, the
// authoritative count, and the shapes already on the board.
func (h *Handler) seedStream(
	r *http.Request, send func([]byte) error, docID, clientID string, authed bool,
) {
	if session := collab.SessionEvent(docID, authed); session != nil {
		_ = send(session)
	}
	others, joinErr := h.presence.Join(r.Context(), docID, clientID)
	if joinErr != nil {
		collab.WarnPresence(r.Context(), "whiteboard", "join failed", docID, joinErr)
		return
	}
	collab.BroadcastPresence(r.Context(), h.hub, "whiteboard", docID, clientID,
		collab.PresenceMsg{Doc: docID, User: clientID, Type: "join"})
	if len(others) > 0 {
		// The joiner seeds its pill from the authoritative roster rather than
		// waiting for the next recount.
		if snap, sErr := json.Marshal(collab.PresenceMsg{
			Doc: docID, User: clientID, Type: "snapshot", Peers: others,
		}); sErr != nil {
			collab.WarnPresence(r.Context(), "whiteboard", "snapshot marshal failed", docID, sErr)
		} else if err := send(snap); err != nil {
			return
		}
	}
	h.broadcastPeerCount(r.Context(), docID)
	if shapes := h.worker.Shapes(docID); len(shapes) > 0 {
		payload, err := json.Marshal(collab.WebShapesEvent{Type: wireShapes, Doc: docID, From: "", Shapes: shapes})
		if err != nil {
			collab.WarnPresence(r.Context(), "whiteboard", "initial shapes marshal failed", docID, err)
			return
		}
		_ = send(payload)
	}
}
