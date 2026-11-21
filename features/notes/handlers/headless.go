// Note create and JSON read paths.
//
// The create capability has two faces over one helper: the page link a human
// clicks (GET /notes/new, redirect into the page) and the headless POST that
// answers the new id as JSON so an agent can chain a write. They are split from
// chi.go by concern, not by audience — createNote is the single place a note is
// minted either way.
//
// The read path closes the loop: op and delete are writable headless, so the
// merged text and the revision an edit bases itself on must be readable as
// JSON too. The page route renders HTML and is not a program's contract.
package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/observability"
)

// JSON keys the headless create/read paths answer with. Named so the write and
// the read cannot drift, and so a key rename is one edit.
const (
	jsonKeyDocID = "docID"
	jsonKeyText  = "text"
)

// handleNew is the browser's create shortcut: it mints the note and redirects
// into the page.
func (h *Handler) handleNew(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	docID, err := h.createNote(r.Context(), sess.User.ID)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "create note failed")
		return
	}
	http.Redirect(w, r, "/notes/"+docID, http.StatusFound)
}

// handleCreate is the headless create: the caller gets the new id in the reply
// instead of a redirect it would have to parse. The row must exist before the
// id is returned, because every write path requires a registered doc.
func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		writeText(w, http.StatusUnauthorized, "sign in to create a note")
		return
	}
	docID, err := h.createNote(r.Context(), sess.User.ID)
	if err != nil {
		writeText(w, http.StatusInternalServerError, "create note failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		jsonKeyDocID: docID,
		"path":       "/notes/" + docID,
	})
}

// createNote is the one place a note is minted: the id is generated, the row
// written, and a failure returned (not swallowed) so no caller hands out an id
// whose row does not exist.
func (h *Handler) createNote(ctx context.Context, owner string) (string, error) {
	docID := uuid.NewString()
	now := dbschema.Now()
	if err := h.q.UpsertNote(ctx, dbgen.UpsertNoteParams{
		DocID: docID, Owner: owner, Updated: now, Created: now,
	}); err != nil {
		obsArgs := append(observability.WithComponent("notes", "feature"), "doc", docID, "error", err)
		observability.Warn(ctx, "notes: save new note", obsArgs...)
		return "", err
	}
	return docID, nil
}

// revision is the note's current op counter (0 before the first batch).
func (h *Handler) revision(docID string) uint64 {
	h.revsMu.Lock()
	defer h.revsMu.Unlock()
	return h.revs[docID]
}

// handleRead is the note's JSON read path: the merged text plus the revision an
// edit must base itself on. Without it an agent could write a note and never
// read it back (the page route renders HTML). The doc store rehydrates from the
// snapshot on a miss, so this returns the persisted text, not an empty doc.
func (h *Handler) handleRead(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		writeText(w, http.StatusUnauthorized, "sign in to read a note")
		return
	}
	docID := chi.URLParam(r, "docID")
	if docID == "" {
		writeText(w, http.StatusBadRequest, "missing doc id")
		return
	}
	if !h.registeredDoc(r.Context(), docID) {
		writeText(w, http.StatusNotFound, "note not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		jsonKeyDocID: docID,
		jsonKeyText:  h.docs.GetOrCreate(docID).Text(),
		"rev":        h.revision(docID),
	})
}
