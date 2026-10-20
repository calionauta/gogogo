// SCOPE:feature — HTTP notes peers (presence bookkeeping, split for size)
package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/observability"
)

// peerJoin registers clientID as connected to docID and returns the list
// of clientIDs already on the doc.
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
func (h *Handler) peerLeave(ctx context.Context, docID, clientID string) {
	if h.hub.IsRegistered(clientID) {
		return
	}
	h.peers.Leave(docID, clientID)
	name := h.peerNameOf(clientID)
	h.peerNamesMu.Lock()
	delete(h.peerNames, clientID)
	h.peerNamesMu.Unlock()
	leaveMsg, lErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: name, From: clientID, Type: "leave"})
	if lErr != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "error", lErr)
		observability.Warn(ctx, "notes: marshal leave", obsArgs...)
		return
	}
	// Doc-scoped like the join: a leave on docA must not touch docB rosters.
	h.hub.BroadcastExceptToDoc(leaveMsg, docID, clientID)
	h.broadcastPeerCount(ctx, docID)
}

// broadcastPeerCount sends the authoritative full peer set for THIS doc
// to the clients watching it. Doc-scoped: a global broadcast merged
// every doc's roster into one, so opening docB inflated docA's pill.
func (h *Handler) broadcastPeerCount(ctx context.Context, docID string) {
	msg, err := json.Marshal(collab.PresenceMsg{Doc: docID, Type: "count", Peers: h.peers.List(docID)})
	if err != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "error", err)
		observability.Warn(ctx, "notes: marshal peer count", obsArgs...)
		return
	}
	h.hub.BroadcastToDoc(msg, docID)
}

// displayName resolves the human identity for presence via the shared
// collab.DisplayName rule (authed email, else the raw client id). Peer
// sets stay keyed by clientID (connections); only display uses email.
func (h *Handler) displayName(sess *auth.ValidatedSession, clientID string) string {
	email := ""
	if sess != nil && sess.User != nil {
		email = sess.User.Email
	}
	return collab.DisplayName(email, clientID)
}

// relay broadcasts one ephemeral presence event to the watchers of
// docID (no persistence, no NATS). From is stamped from the sender's
// clientID so peers can tell two tabs of the same user apart (User alone
// cannot — it is the shared display email).
func (h *Handler) relay(from, docID string, msg collab.PresenceMsg) error {
	msg.From = from
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	h.hub.BroadcastExceptToDoc(payload, docID, from)
	return nil
}

func (h *Handler) handleTyping(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		// Presence is ephemeral but it names who is in a doc and lets a
		// stranger paint cursors on a live board. Signed-in only, same as
		// the op path.
		writeText(w, http.StatusUnauthorized, "sign in to share presence")
		return
	}
	docID := chi.URLParam(r, "docID")
	if docID == "" {
		writeText(w, http.StatusBadRequest, "missing doc id")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeText(w, http.StatusBadRequest, "read body")
		return
	}
	from := r.URL.Query().Get("clientID")
	var req typingRequest
	if uErr := json.Unmarshal(body, &req); uErr != nil {
		writeText(w, http.StatusBadRequest, "decode typing: "+uErr.Error())
		return
	}
	typ := "stopped"
	if req.Typing {
		typ = "typing"
	}
	if err := h.relay(from, docID, collab.PresenceMsg{
		Doc:  docID,
		User: h.displayName(sess, from),
		Type: typ,
		TS:   time.Now().UnixMilli(),
	}); err != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "error", err)
		observability.Warn(r.Context(), "notes: relay typing failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "marshal typing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLeavePresence is the browser's honest goodbye. The SSE teardown
// already runs peerLeave, but an EventSource reconnect (a reload, a wake
// from sleep, a proxy blip) makes the old connection's teardown race the
// new registration: hub.IsRegistered then reports the client as still
// present and the leave is skipped, so peers keep the departed tab's dot
// until the 8s TTL. pagehide fires while the tab is genuinely going away,
// so this path announces the departure immediately and idempotently —
// peerLeave itself is safe to run twice (the peer set is a set, and the
// second broadcast is just a recount).
func (h *Handler) handleLeavePresence(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		writeText(w, http.StatusUnauthorized, "sign in to share presence")
		return
	}
	docID := chi.URLParam(r, "docID")
	from := r.URL.Query().Get("clientID")
	if docID == "" || from == "" {
		writeText(w, http.StatusBadRequest, "missing doc id or client id")
		return
	}
	// Announce now and drop the peer set entry directly: the stream's own
	// teardown may still be in flight, so it cannot be relied on to do this.
	h.leaveNow(r.Context(), docID, from)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// leaveNow is peerLeave without the "did it reconnect?" guard: the caller
// already knows the tab is going away.
func (h *Handler) leaveNow(ctx context.Context, docID, clientID string) {
	h.peers.Leave(docID, clientID)
	name := h.peerNameOf(clientID)
	h.peerNamesMu.Lock()
	delete(h.peerNames, clientID)
	h.peerNamesMu.Unlock()
	leaveMsg, err := json.Marshal(collab.PresenceMsg{Doc: docID, User: name, From: clientID, Type: "leave"})
	if err != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "error", err)
		observability.Warn(ctx, "notes: marshal leave", obsArgs...)
		return
	}
	h.hub.BroadcastExceptToDoc(leaveMsg, docID, clientID)
	h.broadcastPeerCount(ctx, docID)
}

func (h *Handler) handleCaret(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		// Presence is ephemeral but it names who is in a doc and lets a
		// stranger paint cursors on a live board. Signed-in only, same as
		// the op path.
		writeText(w, http.StatusUnauthorized, "sign in to share presence")
		return
	}
	docID := chi.URLParam(r, "docID")
	if docID == "" {
		writeText(w, http.StatusBadRequest, "missing doc id")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeText(w, http.StatusBadRequest, "read body")
		return
	}
	from := r.URL.Query().Get("clientID")
	var req caretRequest
	if uErr := json.Unmarshal(body, &req); uErr != nil {
		writeText(w, http.StatusBadRequest, "decode caret: "+uErr.Error())
		return
	}
	if req.Line < 1 {
		writeText(w, http.StatusBadRequest, "line must be >= 1")
		return
	}
	if req.Pos < 0 {
		writeText(w, http.StatusBadRequest, "pos must be >= 0")
		return
	}
	if err := h.relay(from, docID, collab.PresenceMsg{
		Doc:  docID,
		User: h.displayName(sess, from),
		Type: "caret",
		X:    float64(req.Line),
		Y:    float64(req.Pos),
		TS:   time.Now().UnixMilli(),
	}); err != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "error", err)
		observability.Warn(r.Context(), "notes: relay caret failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "marshal caret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
