// SCOPE:feature — HTTP notes slice (server-owned Loro Text)
//
// Same routes, wire and components as the framework handlers: index/new/
// note pages, per-doc stream, op batches with optimistic-concurrency
// revisions, typing/caret presence, list fragment, delete. Note metadata
// lives in Turso and LISTS SHARED (every signed-in user sees every note,
// author named); Loro docs converge in the shared DocStore with snapshots
// in doc_snapshots. Cross-instance NATS sync stays off (nil connection,
// same as SSE-only mode) — the hub carries live peers.
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
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

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
	// presence is the roster seam: the GoAkt grain per document when the
	// engine runs, the in-process PeerSet otherwise (SetPresence swaps it).
	// Same seam the whiteboard uses, so the two collaborative features cannot
	// diverge on who is present or how a leave is announced.
	presence collab.PresenceSeam

	// peerNamesMu guards peerNames, the clientID → display name map a leave
	// needs (the leave runs after the request is gone). Entries are deleted on
	// leave; the doc-delete path clears the names it can, since those tabs
	// never reconnect to a deleted doc to trigger their own leave.
	peerNamesMu sync.Mutex
	peerNames   map[string]string

	// registry answers "does this doc id have a metadata row?". Nil means
	// the production rule (the notes table). Only a transport fixture sets
	// it, so the write contract stays on for every real request — see
	// permissions.go.
	registry docRegistry

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
		presence:  collab.NewPeerPresence(),
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
	r.Post("/api/notes/{docID}/delete", h.handleDelete)
	r.Get("/api/notes/{docID}/stream", h.handleStream)
	r.Post("/api/notes/{docID}/op", h.handleOp)
	r.Post("/api/notes/{docID}/typing", h.handleTyping)
	r.Post("/api/notes/{docID}/caret", h.handleCaret)
	r.Post("/api/notes/{docID}/leave", h.handleLeavePresence)
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
	notes, err := h.listNotes(r.Context(), sess.User.ID, sess.Role)
	if err != nil {
		// Not an empty list: a failed query would render as "no notes yet",
		// which is indistinguishable from the truth and leaves the operator
		// with nothing to go on.
		obsArgs := append(observability.WithComponent("notes", "feature"), "user", sess.User.ID, "error", err)
		observability.Error(r.Context(), "notes: list notes failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing notes")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = notescomponents.NotesIndex(
		email, sess.Role, notes, h.cfg.BuildLabel, h.cfg.BuildCommit, demo.On(r),
	).Render(r.Context(), w)
}

// listNotes returns every shared note, newest first, stamped with the
// author and the caller's delete right. The listing is NOT owner-scoped
// any more: the index is shared by design, and seeing a note you cannot
// delete is the point of the demo (edit yes, delete no).
func (h *Handler) listNotes(ctx context.Context, requester, role string) ([]notescomponents.NoteMeta, error) {
	rows, err := h.q.ListNotes(ctx)
	if err != nil {
		return nil, err
	}
	notes := make([]notescomponents.NoteMeta, 0, len(rows))
	for _, row := range rows {
		notes = append(notes, notescomponents.NoteMeta{
			DocID:     row.DocID,
			Author:    row.OwnerEmail,
			IsMine:    requester != "" && row.Owner == requester,
			CanDelete: canDeleteDoc(role, requester, row.Owner),
		})
	}
	return notes, nil
}

func (h *Handler) handleFragment(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	notes, err := h.listNotes(r.Context(), sess.User.ID, sess.Role)
	if err != nil {
		// Same contract as the index handler: an empty fragment is a lie the
		// user cannot detect and the operator cannot diagnose.
		obsArgs := append(observability.WithComponent("notes", "feature"), "user", sess.User.ID, "error", err)
		observability.Error(r.Context(), "notes: list fragment failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing notes")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = notescomponents.NotesListFragment(notes).Render(r.Context(), w)
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
	// Opening a doc id nobody created yet REGISTERS it to this visitor (they
	// become the author). That is the app's own bootstrap: a shared link, or
	// two tabs on a fresh id. The write paths then require the row to exist,
	// so an unregistered id can never take a write (see claimDoc).
	if !h.claimDoc(r.Context(), docID, sess.User.ID) {
		writeText(w, http.StatusInternalServerError, "open note")
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
	meta := h.docMeta(r.Context(), docID, sess.User.ID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = notescomponents.NotesPage(sess.User.Email, sess.Role, meta, text, rev,
		h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

// docMeta assembles the row the page and the index both render: the doc id
// plus who started it and whether the caller may delete it.
//
// A doc with no metadata row (a hand-typed id, or a row deleted while its
// URL was open) renders as authorless and undeletable: the page still
// works, it just cannot claim an author or a right it does not have.
func (h *Handler) docMeta(ctx context.Context, docID, requester string) notescomponents.NoteMeta {
	meta := notescomponents.NoteMeta{DocID: docID}
	row, err := h.q.GetNoteMeta(ctx, docID)
	if err != nil {
		return meta
	}
	meta.Author = row.OwnerEmail
	meta.IsMine = requester != "" && row.Owner == requester
	return meta
}

// handleDelete removes a note the requester authored (or administers):
// metadata row first, then the resolved snapshot and the cached doc, so a
// half-failure leaves a doc that is listable and still openable rather
// than one that rehydrates after its index row is gone.
//
// The response is the re-rendered list fragment (Datastar outer patch on
// #notes-list), so the deleting tab and its peers converge on the same
// list without a reload; peers holding the doc open keep their own copy
// until they reload, which is the honest state of a deleted note.
func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
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
	meta, err := h.q.GetNoteMeta(r.Context(), docID)
	if err != nil {
		writeText(w, http.StatusNotFound, "note not found")
		return
	}
	if !canDeleteDoc(sess.Role, sess.User.ID, meta.Owner) {
		writeText(w, http.StatusForbidden, "only the author or an admin can delete this note")
		return
	}
	if derr := h.q.DeleteNote(r.Context(), docID); derr != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", derr)
		observability.Error(r.Context(), "notes: delete note failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if ferr := h.docs.Forget(docID); ferr != nil {
		// The row is gone, so the note is deleted as far as every list and
		// reader is concerned; a leftover snapshot is garbage, not a visible
		// bug. Log it loudly rather than failing a delete that did happen.
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", ferr)
		observability.Warn(r.Context(), "notes: forget doc snapshot failed", obsArgs...)
	}
	// Drop the roster too: the note no longer exists, so its departed client
	// ids must not linger in memory or be resurrected by a reconnect to the
	// same id. Best-effort — the delete already happened. The display-name map
	// cannot be cleared per doc (it is keyed by clientID), so entries for tabs
	// that never come back are bounded by the process lifetime; every ordinary
	// leave does delete its own entry.
	if rerr := h.presence.ForgetDoc(r.Context(), docID); rerr != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", rerr)
		observability.Warn(r.Context(), "notes: forget doc roster failed", obsArgs...)
	}
	h.revsMu.Lock()
	delete(h.revs, docID)
	h.revsMu.Unlock()
	notes, lerr := h.listNotes(r.Context(), sess.User.ID, sess.Role)
	if lerr != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", lerr)
		observability.Error(r.Context(), "notes: list after delete failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing notes")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("datastar-selector", "#notes-list")
	w.Header().Set("datastar-mode", "outer")
	w.WriteHeader(http.StatusOK)
	_ = notescomponents.NotesListFragment(notes).Render(r.Context(), w)
}

func (h *Handler) handleOp(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		// Writes are never anonymous. The op path is gated by the RBAC
		// middleware too, but RBAC is opt-in (RBAC_ENABLED defaults to
		// false), so the handler must not depend on it: without this a
		// cookie-less client wrote text into any doc id it could guess and
		// persisted a snapshot for it. 401 (not 403) is what the shared
		// session banner keys on to say "sign in again".
		writeText(w, http.StatusUnauthorized, "sign in to edit")
		return
	}
	docID := chi.URLParam(r, "docID")
	if docID == "" {
		writeText(w, http.StatusBadRequest, "missing doc id")
		return
	}
	if !h.registeredDoc(r.Context(), docID) {
		// The doc must exist in the index before its body can change. Without
		// this a session that invents an id wrote a snapshot no list ever
		// showed (an orphan, resurrectable by guessing the id again).
		writeText(w, http.StatusNotFound, "note not found")
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
