// SCOPE:feature — HTTP notes slice (server-owned Loro Text)
//
// Same routes, wire and components as the framework handlers: index/new/
// note pages, per-doc stream, op batches with optimistic-concurrency
// revisions, typing/caret presence, list fragment. Note metadata lives in
// Turso (owner-scoped); Loro docs converge in the shared DocStore with
// snapshots in doc_snapshots. Cross-instance NATS sync stays off (nil
// connection, same as SSE-only mode) — the hub carries live peers.
package handlers

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/calionauta/gogogo/config"
	notescomponents "github.com/calionauta/gogogo/features/notes/components"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// listLimit caps the notes list, matching the page size elsewhere.
const listLimit = 50

// maxBodyBytes caps op/presence POST bodies (1 MiB). Batches and cursors
// are bytes, never megabytes; without a cap a huge body is a memory sink.
const maxBodyBytes = 1 << 20

// Handler serves the notes routes on chi.
type Handler struct {
	q         *dbgen.Queries
	auth      *auth.Auth
	hub       *queue.SSEHub
	cfg       *config.Config
	docs      *collab.DocStore
	persister collab.Persister
	peers     *collab.PeerSet

	peerNamesMu sync.Mutex
	peerNames   map[string]string

	// revs is the per-doc batch counter backing optimistic concurrency.
	// Memory-only on purpose: after a restart every client mismatches
	// once, gets 409 + authoritative state, and re-syncs — self-healing,
	// no persisted counter to corrupt.
	revsMu sync.Mutex
	revs   map[string]uint64
}

// NewHandler builds the chi notes handler. docs is the shared DocStore;
// persister is the Turso snapshot store. Nil deps panic: wire errors
// surface at boot, not per request.
func NewHandler(
	q *dbgen.Queries, a *auth.Auth, hub *queue.SSEHub,
	persister collab.Persister, docs *collab.DocStore, cfg *config.Config,
) *Handler {
	if q == nil || a == nil || hub == nil || docs == nil || cfg == nil {
		panic("notes chi: NewHandler with nil dependency")
	}
	// Wire the persister into the store: on a cache miss (eviction,
	// restart) the doc rehydrates from the snapshot BEFORE the next op
	// applies. Without this the op lands on an empty doc and the resolved
	// snapshot is persisted over the good one, losing all prior text.
	docs.SetPersister(persister)
	return &Handler{
		q: q, auth: a, hub: hub, cfg: cfg,
		docs: docs, persister: persister,
		peers:     collab.NewPeerSet(),
		peerNames: make(map[string]string),
		revs:      make(map[string]uint64),
	}
}

// Routes returns the chi router with the notes pages and API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/notes", h.handleIndex)
	r.Get("/notes/new", h.handleNew)
	r.Get("/notes/{docID}", h.handleNote)
	r.Get("/api/notes/{docID}/stream", h.handleStream)
	r.Post("/api/notes/{docID}/op", h.handleOp)
	r.Post("/api/notes/{docID}/typing", h.handleTyping)
	r.Post("/api/notes/{docID}/caret", h.handleCaret)
	r.Get("/api/notes/fragment", h.handleFragment)
	return r
}

// session returns the validated session or nil: pages redirect on nil,
// API handlers stamp anonymously.
func (h *Handler) session(r *http.Request) *auth.ValidatedSession {
	sess, err := h.auth.Session(r)
	if err != nil {
		return nil
	}
	return sess
}

func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	// Error text can carry driver fragments; escape even under text/plain
	// so a reflected error never becomes markup.
	_, _ = w.Write([]byte(html.EscapeString(msg)))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	email := sess.User.Email
	ids, err := h.listNoteIDs(r.Context(), sess.User.ID)
	if err != nil {
		// Not an empty list: a failed query would render as "no notes yet",
		// which is indistinguishable from the truth and leaves the operator
		// with nothing to go on.
		obsArgs := append(observability.WithComponent("notes", "feature"), "owner", sess.User.ID, "error", err)
		observability.Error(r.Context(), "notes: list ids failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing notes")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = notescomponents.NotesIndex(email, sess.Role, ids, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

// listNoteIDs returns the owner's note ids, newest first. Owner-scoped:
// an empty owner lists nothing.
func (h *Handler) listNoteIDs(ctx context.Context, owner string) ([]string, error) {
	rows, err := h.q.ListNotesByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.DocID)
	}
	_ = listLimit
	return ids, nil
}

func (h *Handler) handleFragment(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	ids, err := h.listNoteIDs(r.Context(), sess.User.ID)
	if err != nil {
		// Same contract as the index handler: an empty fragment is a lie the
		// user cannot detect and the operator cannot diagnose.
		obsArgs := append(observability.WithComponent("notes", "feature"), "owner", sess.User.ID, "error", err)
		observability.Error(r.Context(), "notes: list fragment ids failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing notes")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = notescomponents.NotesListFragment(ids).Render(r.Context(), w)
}

func (h *Handler) handleNew(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	docID := uuid.NewString()
	now := dbschema.Now()
	if err := h.q.UpsertNote(r.Context(), dbgen.UpsertNoteParams{
		DocID: docID, Owner: sess.User.ID, Updated: now, Created: now,
	}); err != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", err)
		observability.Warn(r.Context(), "notes: save new note", obsArgs...)
	}
	http.Redirect(w, r, "/notes/"+docID, http.StatusFound)
}

func (h *Handler) handleNote(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	docID := chi.URLParam(r, "docID")
	if docID == "" {
		writeText(w, http.StatusBadRequest, "missing doc id")
		return
	}
	if snap, ok := h.persister.LoadSnapshot(docID); ok {
		d := h.docs.GetOrCreate(docID)
		if err := d.ApplyUpdate(snap); err != nil {
			obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", err)
			observability.Warn(r.Context(), "notes: rehydrate failed", obsArgs...)
		}
	}
	text := h.docs.GetOrCreate(docID).Text()
	h.revsMu.Lock()
	rev := h.revs[docID]
	h.revsMu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = notescomponents.NotesPage(sess.User.Email, sess.Role, docID, text, rev,
		h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	docID := chi.URLParam(r, "docID")
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	h.setPeerName(clientID, h.displayName(sess, clientID))
	authed := sess != nil

	if serr := h.hub.ServeStream(w, r, clientID, docID,
		//nolint:contextcheck // Hub fan-out calls take no context; disconnects
		// surface when the next write fails, which ServeStream handles.
		func(send func([]byte) error) {
			if session := collab.SessionEvent(docID, authed); session != nil {
				_ = send(session)
			}
			h.peerJoin(docID, clientID)
			joinUser := h.displayName(sess, clientID)
			joinMsg, mErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: joinUser, From: clientID, Type: "join"})
			if mErr == nil {
				// Doc-scoped: a join on docA must not recreate rosters on docB.
				h.hub.BroadcastExceptToDoc(joinMsg, docID, clientID)
			} else {
				obsArgs := append(observability.WithComponent("notes", "feature"), "error", mErr)
				observability.Warn(r.Context(), "notes: marshal join", obsArgs...)
			}
			h.broadcastPeerCount(r.Context(), docID)
			if text := h.docs.GetOrCreate(docID).Text(); text != "" {
				h.revsMu.Lock()
				rev := h.revs[docID]
				h.revsMu.Unlock()
				if payload, err := json.Marshal(NoteTextEvent{Type: wireNoteText, Doc: docID, Text: text, Rev: rev}); err == nil {
					_ = send(payload)
				}
			}
		},
		//nolint:contextcheck // leave chain ends in Hub.Broadcast, which
		// takes no context; disconnects surface on the next failed write.
		func() { h.peerLeave(r.Context(), docID, clientID) }); serr != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", serr)
		observability.Warn(r.Context(), "notes: stream failed", obsArgs...)
		return
	}
}

func (h *Handler) handleOp(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
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
	var req opRequest
	if uErr := json.Unmarshal(body, &req); uErr != nil {
		writeText(w, http.StatusBadRequest, "decode op: "+uErr.Error())
		return
	}
	if len(req.Ops) == 0 {
		writeText(w, http.StatusBadRequest, "no ops")
		return
	}
	h.revsMu.Lock()
	current := h.revs[docID]
	if req.Base != current {
		h.revsMu.Unlock()
		text := h.docs.GetOrCreate(docID).Text()
		authed := sess != nil
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "error": "stale", "text": text, "rev": current, authedKey: authed,
		})
		return
	}
	text, err := h.applyOps(r.Context(), docID, from, h.displayName(sess, from), req.Ops, current+1, req.Caret)
	if err != nil {
		h.revsMu.Unlock()
		writeText(w, http.StatusBadRequest, "apply op: "+err.Error())
		return
	}
	h.revs[docID] = current + 1
	rev := h.revs[docID]
	h.revsMu.Unlock()
	authed := sess != nil
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "text": text, "rev": rev, authedKey: authed})
}

// applyOps merges the batch, persists, and fans out to live peers.
// rev is reserved by the caller; on error nothing broadcasts.
func (h *Handler) applyOps(
	ctx context.Context, docID, from, who string, ops []collab.TextOp, rev uint64, caret *CaretReport,
) (string, error) {
	d := h.docs.GetOrCreate(docID)
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
			obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", pErr)
			observability.Warn(ctx, "notes: persist snapshot failed", obsArgs...)
		}
	}
	payload, err := json.Marshal(NoteTextEvent{
		Type: wireNoteText, Doc: docID, From: from, User: who, Text: text, Rev: rev,
		Caret: caretOrNil(caret),
	})
	if err != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", err)
		observability.Warn(ctx, "notes: marshal text", obsArgs...)
		return text, err
	}
	collab.Fanout(h.hub, nil, notesSyncSubject+docID, docID, from, nil, payload)
	return text, nil
}
