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
	q      *dbgen.Queries
	auth   *auth.Auth
	hub    *queue.SSEHub
	cfg    *config.Config
	worker *collab.WebSyncWorker
	peers  *collab.PeerSet
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
		worker: collab.NewWebSyncWorker(hub, persister, docs, nil),
		peers:  collab.NewPeerSet(),
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

// listBoards returns the owner's boards, newest first. Owner-scoped by
// design: an empty owner lists nothing (never every tenant's boards).
func (h *Handler) listBoards(ctx context.Context, owner string) ([]components.BoardMeta, error) {
	rows, err := h.q.ListBoardsByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	boards := make([]components.BoardMeta, 0, len(rows))
	for _, row := range rows {
		boards = append(boards, components.BoardMeta{DocID: row.DocID, DocVer: int(row.Version)})
	}
	return boards, nil
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	boards, err := h.listBoards(r.Context(), sess.User.ID)
	if err != nil {
		// Not an empty list: a failed query would render as "no boards",
		// indistinguishable from the truth and invisible to the operator.
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "owner", sess.User.ID, "error", err)
		observability.Error(r.Context(), "whiteboard: list boards failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "error listing boards")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.BoardListWithRealtime(sess.User.Email, boards,
		h.cfg.BuildLabel, h.cfg.BuildCommit, h.cfg.OfflineSync.Enabled).Render(r.Context(), w)
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
	boards, err := h.listBoards(r.Context(), sess.User.ID)
	if err != nil {
		// Same contract as the index handler: an empty fragment is a lie the
		// user cannot detect and the operator cannot diagnose.
		obsArgs := append(observability.WithComponent("whiteboard", "feature"), "owner", sess.User.ID, "error", err)
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
	if snap, ok := h.worker.LoadSnapshot(docID); ok {
		slog.Info("whiteboard: rehydrated doc", "doc", docID, "bytes", len(snap))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Board(sess.User.Email, docID,
		h.cfg.BuildLabel, h.cfg.BuildCommit, h.cfg.OfflineSync.Enabled).Render(r.Context(), w)
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
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
			others := h.peerJoin(docID, clientID)
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
	docID := chi.URLParam(r, "docID")
	writeJSON(w, http.StatusOK, h.worker.Shapes(docID))
}
