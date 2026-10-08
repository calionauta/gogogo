// SCOPE:layer=feature,removal=feature — Shared plain-text notes (server-owned Loro Text)
// Depends on: internal/collab/ (CRDT transport), internal/queue/ (SSE hub).
// To remove: delete features/notes/ + router/notes.go, drop the
// registerNotesStack call in router/router.go, remove the Notes navbar link
// in features/auth/views.templ, delete ensureNotesCollection in db/seed.go,
// and remove the "notes" capability in internal/capabilities.
// Package notes implements minimal collaborative text editing: a textarea
// backed by a server-owned Loro Text CRDT. The browser sends character ops
// (insert/delete); the server serializes them under the doc mutex, persists
// the resolved snapshot to the PocketBase "notes" collection, and
// broadcasts the resolved text to every other connected client
// (exclude-origin) over a dedicated SSE hub. No JS CRDT library, no rich
// text widget — concurrent typing merges, formatting does not exist.
//
// Same-instance realtime only (mirrors the whiteboard topology):
// cross-instance NATS converges server state, but peer browsers reload the
// snapshot instead of receiving live pushes. Remote text never steals the
// caret: a focused textarea keeps local content until the next local edit.
package notes

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/queue"
)

const (
	notesListLimit = 50

	// wireNoteText is the SSE event type for resolved text. The browser
	// switches on `msg.type === "note-text"` and renders `msg.text`.
	wireNoteText = "note-text"

	// notesSyncSubject is the NATS subject space for cross-instance server
	// convergence. Separate from app.sync.> (whiteboard) so each
	// collection's snapshots land in its own persister.
	notesSyncSubject = "app.notes."
)

// NoteTextEvent is the wire envelope broadcast to peers: the resolved text
// after applying an op batch. Rev lets clients advance their base without
// a round-trip. Clients re-render from it.
type NoteTextEvent struct {
	Type string `json:"type"` // "note-text"
	Doc  string `json:"doc"`
	From string `json:"from"`
	Text string `json:"text"`
	Rev  uint64 `json:"rev"`
}

// opRequest is one POST body: an ordered batch of character ops applied
// atomically under the doc mutex. Base is the server revision the client
// computed positions against; a mismatch means the doc advanced underneath
// (concurrent peer) and the batch is refused with 409 + authoritative
// state instead of being misapplied — positions against a stale base
// delete or displace the wrong characters. Absent base means 0.
type opRequest struct {
	Base uint64          `json:"base,omitempty"`
	Ops  []collab.TextOp `json:"ops"`
}

// Handler serves the notes routes. It holds the shared DocStore (CRDT
// state), the persister (snapshot durability), the SSE hub (fan-out), and
// the optional NATS connection (cross-instance server convergence).
//
// peers tracks, per doc, the set of clientIDs on that doc's SSE stream —
// the authoritative source for the "X online" pill (same pattern as the
// whiteboard's handler_peers.go, minus cursors: text positions don't map
// to pixels without mirror-div geometry, which this feature deliberately
// does not do).
type Handler struct {
	app       core.App
	hub       *queue.SSEHub
	cfg       *config.Config
	docs      *collab.DocStore
	persister collab.Persister
	nc        *natsio.Conn // nil = SSE-only mode

	peers *collab.PeerSet

	// revs is the per-doc batch counter backing optimistic concurrency.
	// Every accepted op batch bumps it; ops carry the base they were
	// computed against. Memory-only on purpose: after a restart every
	// client mismatches once, gets 409 + authoritative state, and
	// re-syncs — self-healing, no persisted counter to corrupt.
	revsMu sync.Mutex
	revs   map[string]uint64
}

// New builds a notes handler. persister is the PocketBase notes collection
// (or an in-memory fake in tests). docs is the DocStore shared with the
// notes SyncWorker; nc is the NATS connection (nil for SSE-only mode).
func New(
	app core.App,
	hub *queue.SSEHub,
	persister collab.Persister,
	docs *collab.DocStore,
	nc *natsio.Conn,
	cfg *config.Config,
) *Handler {
	return &Handler{
		app:       app,
		hub:       hub,
		cfg:       cfg,
		docs:      docs,
		persister: persister,
		nc:        nc,
		peers:     collab.NewPeerSet(),
		revs:      make(map[string]uint64),
	}
}

// RegisterRoutes wires the notes HTTP + SSE routes on the router.
func (h *Handler) RegisterRoutes(se *core.ServeEvent) {
	h.RegisterRoutesOn(se.Router)
}

// RegisterRoutesOn wires the same routes on a raw router (used by tests
// via httptest.NewServer, and by router.Init in production).
func (h *Handler) RegisterRoutesOn(r *router.Router[*core.RequestEvent]) {
	r.GET("/notes", h.handleIndex)
	r.GET("/notes/new", h.handleNew)
	r.GET("/notes/{docID}", h.handleNote)
	r.GET("/api/notes/{docID}/stream", h.handleStream)
	r.POST("/api/notes/{docID}/op", h.handleOp)
	r.POST("/api/notes/{docID}/typing", h.handleTyping)
}

// handleIndex lists existing notes (from the PocketBase collection).
func (h *Handler) handleIndex(c *core.RequestEvent) error {
	if err := auth.RequireAuthOrRedirect(c); err != nil {
		return err
	}
	email := ""
	if c.Auth != nil {
		email = c.Auth.Email()
	}
	ids := []string{}
	if records, err := h.app.FindRecordsByFilter("notes", "", "-updated", notesListLimit, 0); err == nil {
		for _, rec := range records {
			ids = append(ids, rec.GetString("doc_id"))
		}
	} else {
		slog.Debug("notes: list unavailable (collection not seeded?)", "error", err)
	}
	return NotesIndex(email, ids, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(c.Request.Context(), c.Response)
}

// handleNew creates a fresh note id and redirects to it. Docs are created
// lazily on first op, so this only mints the id.
func (h *Handler) handleNew(c *core.RequestEvent) error {
	if err := auth.RequireAuthOrRedirect(c); err != nil {
		return err
	}
	return c.Redirect(http.StatusFound, "/notes/"+uuid.NewString())
}

// handleNote renders one note page with the resolved text. Rehydrates the
// in-memory CRDT from the persisted snapshot so a reloaded page converges
// onto saved state before receiving live updates.
func (h *Handler) handleNote(c *core.RequestEvent) error {
	if err := auth.RequireAuthOrRedirect(c); err != nil {
		return err
	}
	email := ""
	if c.Auth != nil {
		email = c.Auth.Email()
	}
	docID := c.Request.PathValue("docID")
	if docID == "" {
		return c.String(http.StatusBadRequest, "missing doc id")
	}
	if snap, ok := h.persister.LoadSnapshot(docID); ok {
		d := h.docs.GetOrCreate(docID)
		if err := d.ApplyUpdate(snap); err != nil {
			slog.Warn("notes: rehydrate failed", "doc", docID, "error", err)
		}
	}
	text := h.docs.GetOrCreate(docID).Text()
	h.revsMu.Lock()
	rev := h.revs[docID]
	h.revsMu.Unlock()
	return NotesPage(email, docID, text, rev, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(c.Request.Context(), c.Response)
}

// handleStream opens an SSE connection for one doc. The client receives
// resolved-text events; the initial text arrives immediately so a tab that
// opened mid-session starts from current state.
func (h *Handler) handleStream(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Warn("notes: stream auth load", "error", err)
	}
	docID := c.Request.PathValue("docID")
	clientID := c.Request.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}

	flusher, ok := c.Response.(http.Flusher)
	if !ok {
		return c.String(http.StatusInternalServerError, "streaming unsupported")
	}
	c.Response.Header().Set("Content-Type", "text/event-stream")
	c.Response.Header().Set("Cache-Control", "no-cache")
	c.Response.Header().Set("Connection", "keep-alive")
	c.Response.WriteHeader(http.StatusOK)
	fmt.Fprintf(c.Response, ": connected %s\n\n", docID)
	flusher.Flush()

	ch := make(chan []byte, config.DefaultClientQueueSize)
	h.hub.Register(clientID, "", ch)
	defer func() {
		h.hub.UnregisterIfCurrent(clientID, ch)
		h.peerLeave(docID, clientID)
	}()

	// Presence: join to the others, authoritative count to everyone
	// (including self, so a fresh tab seeds its pill without waiting).
	h.peerJoin(docID, clientID)
	if joinMsg, mErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: clientID, Type: "join"}); mErr == nil {
		h.hub.BroadcastExcept(joinMsg, clientID)
	} else {
		slog.Warn("notes: marshal join", "error", mErr)
	}
	h.broadcastPeerCount(docID)

	if text := h.docs.GetOrCreate(docID).Text(); text != "" {
		h.revsMu.Lock()
		rev := h.revs[docID]
		h.revsMu.Unlock()
		if payload, err := json.Marshal(NoteTextEvent{Type: wireNoteText, Doc: docID, Text: text, Rev: rev}); err == nil {
			fmt.Fprintf(c.Response, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}

	// Heartbeat: SSE handlers only detect disconnects on write; without it
	// a parked handler leaks a goroutine and a hub registration forever.
	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(c.Response, ": heartbeat\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		case msg := <-ch:
			fmt.Fprintf(c.Response, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// handleOp applies one op batch: merge into the shared Doc, persist the
// snapshot, publish update bytes for cross-instance convergence, and
// broadcast the resolved text to peers (exclude-origin — the originator
// already holds the text optimistically).
func (h *Handler) handleOp(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Warn("notes: op auth load", "error", err)
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
	var req opRequest
	if uErr := json.Unmarshal(body, &req); uErr != nil {
		return c.String(http.StatusBadRequest, "decode op: "+uErr.Error())
	}
	if len(req.Ops) == 0 {
		return c.String(http.StatusBadRequest, "no ops")
	}
	h.revsMu.Lock()
	current := h.revs[docID]
	if req.Base != current {
		h.revsMu.Unlock()
		// Stale base: refuse instead of misapplying. The response
		// carries the authoritative text + revision so the client
		// recomputes against the fresh base and retries (same
		// recoverable-conflict contract as whiteboard shape versions).
		text := h.docs.GetOrCreate(docID).Text()
		return c.JSON(http.StatusConflict, map[string]any{
			"ok":    false,
			"error": "stale",
			"text":  text,
			"rev":   current,
		})
	}
	text, err := h.applyOps(docID, from, req.Ops, current+1)
	if err != nil {
		h.revsMu.Unlock()
		return c.String(http.StatusBadRequest, "apply op: "+err.Error())
	}
	h.revs[docID] = current + 1
	rev := h.revs[docID]
	h.revsMu.Unlock()
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "text": text, "rev": rev})
}

// typingRequest is one typing-state report. Ephemeral by design:
// broadcast only, never persisted, never forwarded to NATS. A dropped
// "stopped" is covered by the client's expiry sweep, so the server keeps
// no per-peer typing state (no residue to GC, nothing to converge).
type typingRequest struct {
	Typing bool `json:"typing"`
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
	payload, mErr := json.Marshal(collab.PresenceMsg{
		Doc:  docID,
		User: from,
		Type: typ,
		TS:   time.Now().UnixMilli(),
	})
	if mErr != nil {
		return c.String(http.StatusInternalServerError, "marshal typing")
	}
	h.hub.BroadcastExcept(payload, from)
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// applyOps merges the batch under one version-vector window, persists,
// fans out to NATS (server convergence) and to the hub (live peers).
// rev is the revision this batch will carry (already reserved by the
// caller); on error nothing is broadcast and rev stays unbumped.
func (h *Handler) applyOps(docID, from string, ops []collab.TextOp, rev uint64) (string, error) {
	d := h.docs.GetOrCreate(docID)
	since := d.StateVersion()
	var text string
	var err error
	for _, op := range ops {
		if text, err = d.ApplyTextOp(op); err != nil {
			return "", err
		}
	}
	snapshot := d.EncodeSnapshot()
	if h.persister != nil {
		if pErr := h.persister.SaveSnapshot(docID, snapshot); pErr != nil {
			slog.Warn("notes: persist snapshot failed", "doc", docID, "error", pErr)
		}
	}
	payload, err := json.Marshal(NoteTextEvent{
		Type: wireNoteText, Doc: docID, From: from, Text: text, Rev: rev,
	})
	if err == nil {
		h.hub.BroadcastExcept(payload, from)
	} else {
		slog.Warn("notes: marshal text", "doc", docID, "error", err)
	}
	if h.nc != nil {
		if update, err := d.EncodeUpdate(since); err == nil {
			if nErr := h.nc.Publish(notesSyncSubject+docID, update); nErr != nil {
				slog.Warn("notes: nats publish", "doc", docID, "error", nErr)
			}
		} else {
			slog.Warn("notes: encode update", "doc", docID, "error", err)
		}
	}
	return text, nil
}
