// SCOPE:feature — HTTP whiteboard slice (Loro CRDT canvas)
//
// Same routes, wire and components as the framework handlers: index/new/
// board pages, list fragment, per-doc stream, shape updates, presence and
// snapshots. Board metadata lives in Turso (owner-scoped lists, unlike the
// previous stack's unfiltered list — see listBoards); Loro docs converge
// in the shared DocStore with snapshots in doc_snapshots.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/whiteboard/components"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// maxBodyBytes caps op/presence POST bodies (1 MiB). Ops and cursors are
// bytes, never megabytes; without a cap a huge body is a memory sink.
const maxBodyBytes = 1 << 20

// wireShapes is BOTH the SSE event type and the JSON field name for a shape
// list. The browser switches on `msg.type === "shapes"` and then reads
// `msg.shapes`, so the two are one contract — naming it once keeps them
// from drifting apart.
const wireShapes = "shapes"

// Handler serves the whiteboard routes on chi. It holds the shared
// WebSyncWorker (CRDT convergence + persistence) and the SSE hub.
type Handler struct {
	q        *dbgen.Queries
	auth     *auth.Auth
	hub      *queue.SSEHub
	cfg      *config.Config
	worker   *collab.WebSyncWorker
	presence collab.PresenceSeam

	// registry answers "does this doc id have a metadata row?". Nil means
	// the production rule (the boards table). Only a transport fixture sets
	// it — see permissions.go.
	registry docRegistry
}

// NewHandler builds the chi whiteboard handler. docs is the shared DocStore
// (pass collab.NewDocStore()); persister is the Turso snapshot store. Nil
// deps panic: wire errors surface at boot, not per request.
func NewHandler(
	q *dbgen.Queries, a *auth.Auth, hub *queue.SSEHub,
	persister collab.Persister, docs *collab.DocStore, cfg *config.Config,
) *Handler {
	if q == nil || a == nil || hub == nil || docs == nil || cfg == nil {
		panic("whiteboard chi: NewHandler with nil dependency")
	}
	return &Handler{
		q: q, auth: a, hub: hub, cfg: cfg,
		worker:   collab.NewWebSyncWorker(hub, persister, docs, nil),
		presence: collab.NewPeerPresence(),
	}
}

// Routes returns the chi router with the whiteboard pages and API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/whiteboard", h.handleIndex)
	r.Get("/whiteboard/new", h.handleNew)
	r.Get("/whiteboard/{docID}", h.handleBoard)
	r.Get("/api/whiteboards/fragment", h.handleListFragment)
	r.Get("/api/whiteboard/{docID}/stream", h.handleStream)
	r.Post("/api/whiteboard/{docID}/update", h.handleUpdate)
	r.Post("/api/whiteboard/{docID}/presence", h.handlePresence)
	r.Post("/api/whiteboard/{docID}/delete", h.handleDelete)
	r.Get("/api/whiteboard/{docID}/snapshot", h.handleSnapshot)
	return r
}

// session returns the validated session or nil: page handlers redirect on
// nil, API handlers stamp anonymously. One choke point so no handler
// invents its own anonymous semantics.
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

// listBoards returns every shared board, newest first, stamped with the
// author and the caller's delete right. The listing is NOT owner-scoped
// any more: the index is shared by design, and seeing a board you cannot
// delete is the point of the demo (edit yes, delete no).
func (h *Handler) listBoards(ctx context.Context, requester, role string) ([]components.BoardMeta, error) {
	rows, err := h.q.ListBoards(ctx)
	if err != nil {
		return nil, err
	}
	boards := make([]components.BoardMeta, 0, len(rows))
	for _, row := range rows {
		boards = append(boards, boardMeta(row, requester, role))
	}
	return boards, nil
}

// boardMeta stamps one list row: the identity fields the page renders plus
// the delete right the caller has (single home: canDeleteDoc). Both the
// index and the fragment go through it, so a row can never be listed with
// a right the other path would not grant.
func boardMeta(row dbgen.ListBoardsRow, requester, role string) components.BoardMeta {
	return components.BoardMeta{
		DocID:     row.DocID,
		DocVer:    int(row.Version),
		Author:    row.OwnerEmail,
		IsMine:    requester != "" && row.Owner == requester,
		CanDelete: canDeleteDoc(role, requester, row.Owner),
	}
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	boards, err := h.listBoards(r.Context(), sess.User.ID, sess.Role)
	if err != nil {
		// Not an empty list: a failed query would render as "no boards",
		// indistinguishable from the truth and invisible to the operator.
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "user", sess.User.ID, "error", err)
		observability.Error(r.Context(), "whiteboard: list boards failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing boards")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.BoardListWithRealtime(sess.User.Email, sess.Role, boards,
		h.cfg.BuildLabel, h.cfg.BuildCommit, h.cfg.OfflineSync.Enabled, demo.On(r)).Render(r.Context(), w)
}

func (h *Handler) handleNew(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	docID := uuid.NewString()
	now := dbschema.Now()
	if err := h.q.UpsertBoard(r.Context(), dbgen.UpsertBoardParams{
		DocID: docID, Owner: sess.User.ID, Title: "", Updated: now, Created: now,
	}); err != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "doc", docID, "error", err)
		observability.Warn(r.Context(), "whiteboard: save new board", obsArgs...)
	}
	http.Redirect(w, r, "/whiteboard/"+docID, http.StatusFound)
}

func (h *Handler) handleListFragment(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	boards, err := h.listBoards(r.Context(), sess.User.ID, sess.Role)
	if err != nil {
		// Same contract as the index handler: an empty fragment is a lie the
		// user cannot detect and the operator cannot diagnose.
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "user", sess.User.ID, "error", err)
		observability.Error(r.Context(), "whiteboard: list fragment failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing boards")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("datastar-selector", "#whiteboard-list")
	w.Header().Set("datastar-mode", "outer")
	w.WriteHeader(http.StatusOK)
	_ = components.WhiteboardListFragment(boards).Render(r.Context(), w)
}

func (h *Handler) handleBoard(w http.ResponseWriter, r *http.Request) {
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
	// Opening a board id nobody created yet REGISTERS it to this visitor (they
	// become the author): a shared link, a fresh id, both tabs. The write path
	// then requires the row, so an unregistered id cannot take a write.
	if !h.claimDoc(r.Context(), docID, sess.User.ID) {
		writeText(w, http.StatusInternalServerError, "open board")
		return
	}
	if snap, ok := h.worker.LoadSnapshot(docID); ok {
		slog.Info("whiteboard: rehydrated doc", "doc", docID, "bytes", len(snap))
	}
	meta := h.docMeta(r.Context(), docID, sess.User.ID)
	// Seed the canvas with the shapes the server already has: the stream is the
	// live channel, but an offline reload never reaches it, and without this the
	// board painted empty until the connection came back.
	shapesJSON := []byte("[]")
	if shapes := h.worker.Shapes(docID); len(shapes) > 0 {
		if raw, merr := json.Marshal(shapes); merr == nil {
			shapesJSON = raw
		} else {
			obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", merr)
			observability.Warn(r.Context(), "whiteboard: marshal page shapes", obsArgs...)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Board(sess.User.Email, sess.Role, meta,
		h.cfg.BuildLabel, h.cfg.BuildCommit, h.cfg.OfflineSync.Enabled, demo.On(r),
		string(shapesJSON)).Render(r.Context(), w)
}

// docMeta assembles the row the page and the index both render: the doc id
// plus who started it and whether the caller may delete it.
//
// A board with no metadata row (a hand-typed id, or a row deleted while its
// URL was open) renders as authorless and undeletable: the page still
// works, it just cannot claim an author or a right it does not have.
func (h *Handler) docMeta(ctx context.Context, docID, requester string) components.BoardMeta {
	meta := components.BoardMeta{DocID: docID}
	row, err := h.q.GetBoardMeta(ctx, docID)
	if err != nil {
		return meta
	}
	meta.Author = row.OwnerEmail
	meta.IsMine = requester != "" && row.Owner == requester
	return meta
}

// handleDelete removes a board the requester authored (or administers):
// metadata row first, then the resolved snapshot and the cached doc, so a
// half-failure leaves a board that is listable and still openable rather
// than one that rehydrates after its index row is gone.
//
// The response is the re-rendered list fragment (Datastar outer patch on
// #whiteboard-list), so the deleting tab and its peers converge on the
// same list without a reload; peers still holding the board open keep
// their own copy until they reload, which is the honest state of a
// deleted board.
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
	meta, err := h.q.GetBoardMeta(r.Context(), docID)
	if err != nil {
		writeText(w, http.StatusNotFound, "board not found")
		return
	}
	if !canDeleteDoc(sess.Role, sess.User.ID, meta.Owner) {
		writeText(w, http.StatusForbidden, "only the author or an admin can delete this board")
		return
	}
	if derr := h.q.DeleteBoard(r.Context(), docID); derr != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "doc", docID, "error", derr)
		observability.Error(r.Context(), "whiteboard: delete board failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if ferr := h.worker.Forget(docID); ferr != nil {
		// The row is gone, so the board is deleted as far as every list and
		// reader is concerned; a leftover snapshot is garbage, not a visible
		// bug. Log it loudly rather than failing a delete that did happen.
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "doc", docID, "error", ferr)
		observability.Warn(r.Context(), "whiteboard: forget doc snapshot failed", obsArgs...)
	}
	boards, lerr := h.listBoards(r.Context(), sess.User.ID, sess.Role)
	if lerr != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "doc", docID, "error", lerr)
		observability.Error(r.Context(), "whiteboard: list after delete failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing boards")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("datastar-selector", "#whiteboard-list")
	w.Header().Set("datastar-mode", "outer")
	w.WriteHeader(http.StatusOK)
	_ = components.WhiteboardListFragment(boards).Render(r.Context(), w)
}

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
		//nolint:contextcheck // Hub fan-out calls take no context; disconnects
		// surface when the next write fails, which ServeStream handles.
		func(send func([]byte) error) {
			if session := collab.SessionEvent(docID, authed); session != nil {
				_ = send(session)
			}
			others, joinErr := h.peerJoin(r.Context(), docID, clientID)
			if joinErr != nil {
				obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", joinErr)
				observability.Warn(r.Context(), "whiteboard: peer join failed", obsArgs...)
				return
			}
			joinMsg, mErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: clientID, Type: "join"})
			if mErr != nil {
				obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", mErr)
				observability.Warn(r.Context(), "whiteboard: marshal join", obsArgs...)
				return
			}
			h.hub.BroadcastExceptToDoc(joinMsg, docID, clientID)
			if len(others) > 0 {
				snap, sErr := json.Marshal(collab.PresenceMsg{Doc: docID, User: clientID, Type: "snapshot", Peers: others})
				if sErr != nil {
					obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", sErr)
					observability.Warn(r.Context(), "whiteboard: marshal snapshot", obsArgs...)
				} else if err := send(snap); err != nil {
					return
				}
			}
			h.broadcastPeerCount(r.Context(), docID)
			if shapes := h.worker.Shapes(docID); len(shapes) > 0 {
				payload, err := json.Marshal(collab.WebShapesEvent{Type: wireShapes, Doc: docID, From: "", Shapes: shapes})
				if err != nil {
					obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", err)
					observability.Warn(r.Context(), "whiteboard: marshal initial shapes", obsArgs...)
				} else {
					_ = send(payload)
				}
			}
		},
		//nolint:contextcheck // leave chain ends in Hub.Broadcast, which
		// takes no context; disconnects surface on the next failed write.
		func() { h.peerLeave(r.Context(), docID, clientID) }); serr != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "doc", docID, "error", serr)
		observability.Warn(r.Context(), "whiteboard: stream failed", obsArgs...)
		return
	}
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if h.session(r) == nil {
		// Writes are never anonymous. RBAC gates this path too, but it is
		// opt-in (RBAC_ENABLED defaults to false), so the handler must hold
		// on its own: without this a cookie-less client drew into any board
		// id it could guess. 401 is what the shared session banner reads.
		writeText(w, http.StatusUnauthorized, "sign in to draw")
		return
	}
	docID := chi.URLParam(r, "docID")
	if docID == "" {
		writeText(w, http.StatusBadRequest, "missing doc id")
		return
	}
	if !h.registeredDoc(r.Context(), docID) {
		// The board must exist in the index before its body changes; without
		// this an invented id persisted shapes no list ever showed.
		writeText(w, http.StatusNotFound, "board not found")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeText(w, http.StatusBadRequest, "read body")
		return
	}
	from := r.URL.Query().Get("clientID")
	var op collab.ShapeOp
	if uErr := json.Unmarshal(body, &op); uErr != nil {
		writeText(w, http.StatusBadRequest, "decode op: "+uErr.Error())
		return
	}
	shapes, err := h.worker.ApplyOp(docID, from, op)
	if err != nil {
		if errors.Is(err, collab.ErrShapeConflict) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":       false,
				"error":    "stale",
				wireShapes: shapes,
			})
			return
		}
		writeText(w, http.StatusBadRequest, "apply op: "+err.Error())
		return
	}
	if berr := h.q.BumpBoardVersion(r.Context(), dbgen.BumpBoardVersionParams{
		Updated: dbschema.Now(), DocID: docID,
	}); berr != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "doc", docID, "error", berr)
		observability.Warn(r.Context(), "whiteboard: bump version", obsArgs...)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"count":    len(shapes),
		wireShapes: shapes,
	})
}

func (h *Handler) handlePresence(w http.ResponseWriter, r *http.Request) {
	if h.session(r) == nil {
		writeText(w, http.StatusUnauthorized, "sign in to share presence")
		return
	}
	sess := h.session(r)
	docID := chi.URLParam(r, "docID")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeText(w, http.StatusBadRequest, "read body")
		return
	}
	from := r.URL.Query().Get("clientID")
	// Re-tag with the doc id and the server-authenticated user: stamping
	// here (not trusting the client's User field) stops cursor spoofing.
	var msg collab.PresenceMsg
	if err := json.Unmarshal(body, &msg); err != nil {
		writeText(w, http.StatusBadRequest, "decode presence")
		return
	}
	email := ""
	if sess != nil {
		email = sess.User.Email
	}
	msg.Doc = docID
	msg.User = collab.DisplayName(email, from)
	// Per-tab sender id (see PresenceMsg.From): cursor/draft frames from
	// two tabs of the same user must not share one client-side key.
	msg.From = from
	msg.TS = time.Now().UnixMilli()
	data, mErr := json.Marshal(msg)
	if mErr != nil {
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "error", mErr)
		observability.Warn(r.Context(), "whiteboard: marshal presence failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "marshal presence")
		return
	}
	// Doc-scoped: a cursor/draft on docA must never move dots on docB.
	h.hub.BroadcastExceptToDoc(data, docID, from)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.session(r) == nil {
		// The snapshot IS the board body. Shared means every signed-in user
		// reads it, never that an anonymous client does.
		writeText(w, http.StatusUnauthorized, "sign in to read")
		return
	}
	docID := chi.URLParam(r, "docID")
	writeJSON(w, http.StatusOK, h.worker.Shapes(docID))
}
