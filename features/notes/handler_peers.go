// SCOPE:layer=feature,removal=feature — Shared plain-text notes (server-owned Loro Text)
// Presence/peer bookkeeping. Same contract as the whiteboard's
// handler_peers.go, sharing the collab.PeerSet mechanics: join to the
// others, authoritative count to all, leave + recount on disconnect with
// the reconnect-race guard. No cursor coordinates (see Handler doc).
package notes

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/features/auth"
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

// displayName resolves the human identity for presence via the shared
// collab.DisplayName rule (authed email, else the raw client id). Peer
// sets stay keyed by clientID (connections); only display uses email.
func displayName(c *core.RequestEvent, clientID string) string {
	email := ""
	if c.Auth != nil {
		email = c.Auth.Email()
	}
	return collab.DisplayName(email, clientID)
}

// relay broadcasts one ephemeral presence event (no persistence, no
// NATS). Typing and caret share it: same envelope, same error policy,
// one place to change when the wire evolves.
func (h *Handler) relay(from string, msg collab.PresenceMsg) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	h.hub.BroadcastExcept(payload, from)
	return nil
}

// typingRequest is one typing-state report. Ephemeral by design:
// broadcast only, never persisted, never forwarded to NATS. A dropped
// "stopped" is covered by the client's expiry sweep, so the server keeps
// no per-peer typing state (no residue to GC, nothing to converge).
type typingRequest struct {
	Typing bool `json:"typing"`
}

// caretRequest is one caret report: line index (1-based, by \n) plus the
// UTF-16 offset in the reporter's text. Lines are dimension-independent
// (same text, same lines everywhere); the offset lets each peer map the
// caret to ITS OWN pixels via mirror-div (fonts, zoom and wrapping differ
// per viewport, so no server-side geometry could ever be right for all).
// Broadcast only, never persisted, never forwarded to NATS.
type caretRequest struct {
	Line int `json:"line"`
	Pos  int `json:"pos"`
}

// caretOrNil passes a client caret through only when sane; anything
// else (including nil) becomes an omitted field, never a 400.
func caretOrNil(c *CaretReport) *CaretReport {
	if !validCaret(c) {
		return nil
	}
	return c
}

// handleTyping relays a typing state to peers on the doc's stream
// (exclude-origin). Production note: any authenticated user may report
// for any doc — same trust level as ops (no per-note ACL yet); throttle
// lives client-side (at most one report per 2.5s of typing).
func (h *Handler) handleTyping(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Warn("notes: typing auth load", "error", err)
	}
	docID := c.Request.PathValue("docID")
	if docID == "" {
		return c.String(http.StatusBadRequest, "missing doc id")
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return c.String(http.StatusBadRequest, "read body")
	}
	from := c.Request.URL.Query().Get("clientID")
	var req typingRequest
	if uErr := json.Unmarshal(body, &req); uErr != nil {
		return c.String(http.StatusBadRequest, "decode typing: "+uErr.Error())
	}
	typ := "stopped"
	if req.Typing {
		typ = "typing"
	}
	if err := h.relay(from, collab.PresenceMsg{
		Doc:  docID,
		User: displayName(c, from),
		Type: typ,
		TS:   time.Now().UnixMilli(),
	}); err != nil {
		slog.Warn("notes: relay typing failed", "error", err)
		return c.String(http.StatusInternalServerError, "marshal typing")
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// handleCaret relays a caret line to peers on the doc's stream
// (exclude-origin). Non-positive lines are rejected; the line is capped
// downstream by clients, never trusted for indexing server-side (the
// server stores nothing — it only relays the number).
func (h *Handler) handleCaret(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Warn("notes: caret auth load", "error", err)
	}
	docID := c.Request.PathValue("docID")
	if docID == "" {
		return c.String(http.StatusBadRequest, "missing doc id")
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return c.String(http.StatusBadRequest, "read body")
	}
	from := c.Request.URL.Query().Get("clientID")
	var req caretRequest
	if uErr := json.Unmarshal(body, &req); uErr != nil {
		return c.String(http.StatusBadRequest, "decode caret: "+uErr.Error())
	}
	if req.Line < 1 {
		return c.String(http.StatusBadRequest, "line must be >= 1")
	}
	if req.Pos < 0 {
		return c.String(http.StatusBadRequest, "pos must be >= 0")
	}
	if err := h.relay(from, collab.PresenceMsg{
		Doc:  docID,
		User: displayName(c, from),
		Type: "caret",
		X:    float64(req.Line),
		Y:    float64(req.Pos),
		TS:   time.Now().UnixMilli(),
	}); err != nil {
		slog.Warn("notes: relay caret failed", "error", err)
		return c.String(http.StatusInternalServerError, "marshal caret")
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}
