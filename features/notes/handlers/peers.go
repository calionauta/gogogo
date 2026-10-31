// SCOPE:feature — HTTP notes presence (peer bookkeeping, stream seeding, split for size)
package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/calionauta/gogogo/internal/auth"
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

// forgetPeerName drops a departed connection's display name. Without it the map
// grows for the life of the process — one entry per tab ever opened.
func (h *Handler) forgetPeerName(clientID string) {
	h.peerNamesMu.Lock()
	defer h.peerNamesMu.Unlock()
	delete(h.peerNames, clientID)
}

// peerLeave removes clientID from docID's roster and broadcasts the departure
// plus a recount. Skipped when the clientID re-registered during an
// EventSource reconnect (the new handler owns it; broadcasting a leave would
// make the pill oscillate 1→0→1→0).
func (h *Handler) peerLeave(ctx context.Context, docID, clientID string) {
	// The stream's request context is already cancelled when this runs; an
	// actor Ask on it would fail and leave the closed tab on the roster.
	ctx = context.WithoutCancel(ctx)
	if h.hub.IsRegistered(clientID) {
		return
	}
	peers, err := h.presence.Leave(ctx, docID, clientID)
	if err != nil {
		collab.WarnPresence(ctx, "notes", "leave failed", docID, err)
		return
	}
	name := h.peerNameOf(clientID)
	h.forgetPeerName(clientID)
	// Doc-scoped like the join: a leave on docA must not touch docB rosters.
	collab.BroadcastPresence(ctx, h.hub, "notes", docID, clientID,
		collab.PresenceMsg{Doc: docID, User: name, Type: "leave"})
	// The Leave answer IS the authoritative remaining set: re-listing would
	// pay a second Ask for the same truth.
	collab.BroadcastRoster(ctx, h.hub, "notes", docID, peers)
}

// broadcastPeerCount sends the authoritative roster for THIS doc to the tabs
// watching it. Doc-scoped: a global broadcast merged every doc's roster into
// one, so opening docB inflated docA's pill.
func (h *Handler) broadcastPeerCount(ctx context.Context, docID string) {
	peers, err := h.presence.List(ctx, docID)
	if err != nil {
		collab.WarnPresence(ctx, "notes", "count list failed", docID, err)
		return
	}
	collab.BroadcastRoster(ctx, h.hub, "notes", docID, peers)
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

// relay broadcasts one ephemeral presence event to the watchers of docID (no
// persistence). From is stamped from the sender's clientID so peers can tell
// two tabs of the same user apart (User alone cannot — it is the shared
// display email).
func (h *Handler) relay(ctx context.Context, from, docID string, msg collab.PresenceMsg) {
	collab.BroadcastPresence(ctx, h.hub, "notes", docID, from, msg)
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
	h.relay(r.Context(), from, docID, collab.PresenceMsg{
		Doc:  docID,
		User: h.displayName(sess, from),
		Type: typ,
		TS:   time.Now().UnixMilli(),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLeavePresence is the browser's honest goodbye. The SSE teardown
// already runs peerLeave, but an EventSource reconnect (a reload, a wake
// from sleep, a proxy blip) makes the old connection's teardown race the
// new registration: hub.IsRegistered then reports the client as still
// present and the leave is skipped, so peers keep the departed tab's dot
// until the lease expires. pagehide fires while the tab is genuinely going
// away, so this path announces the departure immediately and idempotently —
// leaveNow itself is safe to run twice (the roster is a set, and the second
// broadcast is just a recount).
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
	// Announce now and drop the roster entry directly: the stream's own
	// teardown may still be in flight, so it cannot be relied on to do this.
	h.leaveNow(r.Context(), docID, from)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// leaveNow is peerLeave without the "did it reconnect?" guard: the caller
// already knows the tab is going away.
func (h *Handler) leaveNow(ctx context.Context, docID, clientID string) {
	ctx = context.WithoutCancel(ctx)
	peers, err := h.presence.Leave(ctx, docID, clientID)
	if err != nil {
		collab.WarnPresence(ctx, "notes", "leave (pagehide) failed", docID, err)
		return
	}
	name := h.peerNameOf(clientID)
	h.forgetPeerName(clientID)
	collab.BroadcastPresence(ctx, h.hub, "notes", docID, clientID,
		collab.PresenceMsg{Doc: docID, User: name, Type: "leave"})
	collab.BroadcastRoster(ctx, h.hub, "notes", docID, peers)
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
	h.relay(r.Context(), from, docID, collab.PresenceMsg{
		Doc:  docID,
		User: h.displayName(sess, from),
		Type: "caret",
		X:    float64(req.Line),
		Y:    float64(req.Pos),
		TS:   time.Now().UnixMilli(),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleStream serves the note's SSE stream: session frame, live roster, the
// current text, then the hub's heartbeat loop.
//
// It lives beside the presence helpers because everything it sends on open is a
// presence frame plus the seed the roster accompanies; the channel and its
// bookkeeping are one subject.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		// A stream is a READ of the doc body: every signed-in user may read
		// every shared note, but an anonymous tab must not. The browser's
		// EventSource cannot sign in, so 401 makes it stop reconnecting
		// (the page itself already redirects to /login).
		writeText(w, http.StatusUnauthorized, "sign in to read")
		return
	}
	docID := chi.URLParam(r, "docID")
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	h.setPeerName(clientID, h.displayName(sess, clientID))
	authed := sess != nil

	if serr := h.hub.ServeStream(w, r, clientID, docID,
		func(send func([]byte) error) {
			h.seedStream(r, send, docID, clientID, sess, authed)
		},
		//nolint:contextcheck // SSE-sink boundary: the leave chain ends in
		// hub.BroadcastToDoc/BroadcastExceptToDoc, which take no context
		// (fan-out to other connections; a disconnect surfaces on their next
		// failed write). Growing ctx on the hub is a repo-wide refactor.
		func() { h.peerLeave(r.Context(), docID, clientID) }); serr != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", serr)
		observability.Warn(r.Context(), "notes: stream failed", obsArgs...)
		return
	}
}

// seedStream sends everything a joining tab needs on its own connection: the
// session frame, the roster it walked into, its own join announcement, the
// authoritative count, and the current note text.
func (h *Handler) seedStream(
	r *http.Request, send func([]byte) error, docID, clientID string,
	sess *auth.ValidatedSession, authed bool,
) {
	if session := collab.SessionEvent(docID, authed); session != nil {
		_ = send(session)
	}
	others, joinErr := h.presence.Join(r.Context(), docID, clientID)
	if joinErr != nil {
		collab.WarnPresence(r.Context(), "notes", "join failed", docID, joinErr)
		return
	}
	if len(others) > 0 {
		// The joiner seeds its pill from the authoritative roster; a peer that
		// was already here gets the join frame below and the recount after it.
		if snap, sErr := json.Marshal(collab.PresenceMsg{Doc: docID, Type: "snapshot", Peers: others}); sErr == nil {
			_ = send(snap)
		}
	}
	// Doc-scoped: a join on docA must not recreate rosters on docB.
	collab.BroadcastPresence(r.Context(), h.hub, "notes", docID, clientID,
		collab.PresenceMsg{Doc: docID, User: h.displayName(sess, clientID), From: clientID, Type: "join"})
	h.broadcastPeerCount(r.Context(), docID)
	if text := h.docs.GetOrCreate(docID).Text(); text != "" {
		h.revsMu.Lock()
		rev := h.revs[docID]
		h.revsMu.Unlock()
		if payload, err := json.Marshal(NoteTextEvent{Type: wireNoteText, Doc: docID, Text: text, Rev: rev}); err == nil {
			_ = send(payload)
		}
	}
}
