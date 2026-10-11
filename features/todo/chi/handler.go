// SCOPE:feature — Chi todo slice (flat-stack port, step 4)
//
// Package todo's chi Handler serves the todo data endpoints on the flat
// stack: chi routing, limen sessions, sqlc queries called directly, the same
// Templ components, the same Datastar SSE wire. Paths, statuses, redirects
// and fragment headers mirror features/todo/handlers (PocketBase) exactly —
// the client cannot tell which stack answered.
//
// Scope: data endpoints only (list/fragment/CRUD/confirm/stream). The full
// page (/todo index), AI suggest, retry demo and onboarding stay on the
// PocketBase handlers until step 6. Cross-instance sync (NATS publish) is
// out too — record propagation here is the chi SSE hub (step 5's realtime
// cutover), not PocketBase realtime.
package chi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/components"
	"github.com/calionauta/gogogo/internal/auth"
	ic "github.com/calionauta/gogogo/internal/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
	basecoat "github.com/calionauta/gogogo/web/skins/basecoat"
)

// SkinBasecoat mirrors handlers.SkinBasecoat without importing the
// PocketBase handler package: the chi slice must not depend on the stack
// it replaces.
const SkinBasecoat = "basecoat"

// Handler serves the todo data endpoints on chi. Q is the concrete
// sqlc queries (never an interface — a wrong call is a compile error);
// Hub is a chi-stack-private SSE hub so chi record events never leak
// into PocketBase-hub clients and vice versa.
type Handler struct {
	Q           *dbgen.Queries
	Auth        *auth.Auth
	Hub         *queue.SSEHub
	DefaultSkin string
	// Pub publishes mutations to JetStream for cross-instance convergence.
	// Nil (NATS disabled) is a valid zero value: Publish is nil-safe, so
	// the stack converges in-process through the Hub only. Set via
	// SetPublisher so tests can run hub-only without a NATS server.
	Pub *nats.CrudPublisher
	// BuildLabel/BuildCommit stamp the navbar version badge (from -ldflags
	// in production builds, "dev"/"unknown" otherwise). Zero values render
	// the same defaults the PocketBase index shows.
	BuildLabel  string
	BuildCommit string
	// OfflineSyncEnabled mirrors the config flag into the page signals.
	OfflineSyncEnabled bool
	// NATSEnabled switches the realtime label between the in-process hub
	// and JetStream. Set from config at boot.
	NATSEnabled bool
}

// SetPublisher installs the cross-instance publisher. Nil disables it.
func (h *Handler) SetPublisher(p *nats.CrudPublisher) {
	h.Pub = p
}

// NewHandler builds the chi todo handler. All deps required — a nil Q,
// Auth or Hub is a programming error, surfaced here and not per request.
func NewHandler(q *dbgen.Queries, a *auth.Auth, hub *queue.SSEHub, defaultSkin string) *Handler {
	if q == nil || a == nil || hub == nil {
		panic("todo: NewHandler with nil dependency")
	}
	return &Handler{Q: q, Auth: a, Hub: hub, DefaultSkin: defaultSkin}
}

// Routes returns the chi router with the todo data endpoints. Paths match
// the PocketBase registrations one for one so the cutover changes the
// server, never the client.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/api/todos", h.handleList)
	r.Get("/api/todos/fragment", h.handleFragment)
	r.Post("/api/todos", h.handleCreate)
	r.Post("/api/todos/{id}/toggle", h.handleToggle)
	r.Post("/api/todos/completed/delete", h.handleClearCompleted)
	r.Post("/api/todos/{id}/delete", h.handleDelete)
	r.Get("/api/todos/{id}/confirm-delete", h.handleConfirmDelete)
	r.Get("/api/todos/stream", h.handleStream)
	r.Get("/login", h.handleLoginGet)
	r.Post("/login", h.handleLoginPost)
	r.Post("/logout", h.handleLogoutPost)
	r.Get("/todo", h.handleIndex)
	return r
}

// ownerSession resolves both the owner id and the validated session in one
// call for page handlers, which need the user email for the navbar as well
// as the owner for scoping.
func (h *Handler) ownerSession(r *http.Request) (string, *auth.ValidatedSession, error) {
	sess, err := h.Auth.Session(r)
	if err != nil {
		return "", nil, err
	}
	if sess == nil || sess.User == nil || sess.User.ID == "" {
		return "", nil, errors.New("todo: empty session")
	}
	return sess.User.ID, sess, nil
}

// realtimeKind labels the active broadcast transport for the diagnostics
// panel, matching the PocketBase index contract.
func (h *Handler) realtimeKind() string {
	if h.NATSEnabled {
		return "JetStream"
	}
	return "in-memory"
}

// offlineLabel describes offline-sync behaviour for the page sub-header,
// in the same words as the PocketBase index (with the Turso store named).
func (h *Handler) offlineLabel() string {
	if !h.OfflineSyncEnabled {
		return "OFFLINE_SYNC_ENABLED=false — actions fail when network is down; offline mutations NOT queued"
	}
	if h.NATSEnabled {
		return "Turso + NATS_ENABLED=true — SW queues browser mutations + JetStream (NATS) streams CRUD ops across replicas"
	}
	return "Turso, SW queues browser mutations; single instance"
}

// ownerID resolves the authenticated owner's id (a UUID string, matching
// every owner column). Fail-closed: any error means "no owner", and callers
// redirect to /login — an empty owner must never reach a store call, where
// it would list every tenant's rows.
func (h *Handler) ownerID(r *http.Request) (string, error) {
	sess, err := h.Auth.Session(r)
	if err != nil {
		return "", err
	}
	if sess == nil || sess.User == nil || sess.User.ID == "" {
		return "", errors.New("todo: empty session")
	}
	return sess.User.ID, nil
}

// redirectLogin sends unauthenticated clients to the login page, mirroring
// the PocketBase handlers (303, never 401 — the Datastar client follows it).
func redirectLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

func (h *Handler) fail(w http.ResponseWriter, logMsg string, err error, publicMsg string) {
	slog.Error(logMsg, "error", err)
	writeText(w, http.StatusInternalServerError, publicMsg)
}

// toTodo maps a sqlc row onto the wire model. Timestamps are caller-written
// RFC3339 (db.TimeFormat); a row that does not parse is data corruption,
// flagged as a 500 rather than rendered half-empty.
func toTodo(row dbgen.Todo) (todo.Todo, error) {
	created, err := time.Parse(db.TimeFormat, row.Created)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s created %q: %w", row.ID, row.Created, err)
	}
	updated, err := time.Parse(db.TimeFormat, row.Updated)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s updated %q: %w", row.ID, row.Updated, err)
	}
	return todo.Todo{
		ID:        row.ID,
		Title:     row.Title,
		Completed: row.Completed,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

// listOwner loads the owner's todos and applies the list filter in Go:
// "" (all), "active", "completed". Unknown filters read as "all", matching
// the PocketBase filterOf default.
func (h *Handler) listOwner(ctx context.Context, owner string, filter string) ([]todo.Todo, error) {
	rows, err := h.Q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]todo.Todo, 0, len(rows))
	for _, row := range rows {
		item, err := toTodo(row)
		if err != nil {
			return nil, err
		}
		switch filter {
		case "active":
			if item.Completed {
				continue
			}
		case "completed":
			if !item.Completed {
				continue
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func filterOf(r *http.Request) string {
	if f := r.URL.Query().Get("filter"); f != "" {
		return f
	}
	return "all"
}

func (h *Handler) resolveSkin(r *http.Request) string {
	skin := h.DefaultSkin
	if q := r.URL.Query().Get("skin"); q != "" {
		skin = q
	}
	if skin == SkinBasecoat {
		return skin
	}
	return "daisyui"
}

func (h *Handler) renderListRegion(signals todo.Signals, skin string) templ.Component {
	if skin == SkinBasecoat {
		return basecoat.TodoListRegion(signals)
	}
	return components.TodoListRegion(signals)
}

func emitToast(sse *sdk.ServerSentEventGenerator, message, toastType string) error {
	return patchSSE(
		sse,
		ic.Toast(message, toastType, ic.NewToastID()),
		sdk.WithSelectorID("toast-container"),
		sdk.WithModeAppend(),
	)
}

// signalItemCount is the Datastar signal carrying the visible-row count.
// One const so a rename breaks loudly at every merge site instead of
// silently desyncing the header badge from the list.
const signalItemCount = "itemCount"

// patchSSE renders a component and patches it into the DOM over the stream.
// Datastar-go sinks take no context (PatchElements writes straight to the
// ResponseWriter); cancellation surfaces as a write error on disconnect,
// which every caller checks. Threading a ctx parameter here would be fake
// plumbing with no downstream sink.
func patchSSE(sse *sdk.ServerSentEventGenerator, comp templ.Component, opts ...sdk.PatchElementOption) error {
	return dshelpers.RenderAndPatch(sse, comp, opts...)
}

// recordEvent is the hub payload for todo mutations — the same shape the
// PocketBase stream dispatcher decodes, so a future shared client keeps
// working. Each stream re-lists scoped on receipt; the payload itself only
// names what changed.
type recordEvent struct {
	Event string `json:"event"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// publishSync fans a mutation out twice: Hub for this instance's streams,
// JetStream for other instances. Both are best-effort after the Turso write
// (the source of truth); either may lag without affecting correctness.
func (h *Handler) publishSync(
	r *http.Request, owner string, evt recordEvent, op nats.CrudOpType, data *nats.CrudOpData,
) {
	h.broadcastRecord(r, owner, evt)
	h.Pub.Publish(op, owner, data)
}

// broadcastRecord publishes a mutation to the owner's other streams,
// excluding the originating client when known (it already got the
// synchronous patch below).
func (h *Handler) broadcastRecord(r *http.Request, owner string, evt recordEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("todo: marshal record event", "error", err)
		return
	}
	exclude := r.URL.Query().Get("clientID")
	if exclude == "" {
		exclude = r.Header.Get("X-Client-ID")
	}
	h.Hub.BroadcastToUser(payload, owner, exclude)
}

// take no context; disconnects surface as write errors, checked per call.
//
//nolint:contextcheck // SSE emission below ends in datastar-go sinks, which
func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	filter := filterOf(r)
	todos, err := h.listOwner(r.Context(), owner, filter)
	if err != nil {
		h.fail(w, "todo: list failed", fmt.Errorf("filter %s: %w", filter, err), "error listing todos")
		return
	}
	signals := todo.Signals{Todos: todos, Filter: filter, ItemCount: len(todos)}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, signals); err != nil {
		h.fail(w, "todo: list signals failed", err, "error listing todos")
		return
	}
	if err := patchSSE(
		sse, h.renderListRegion(signals, h.resolveSkin(r)),
		sdk.WithSelector("#todo-list"),
	); err != nil {
		h.fail(w, "todo: list patch failed", err, "error listing todos")
	}
}

func (h *Handler) handleFragment(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	filter := filterOf(r)
	todos, err := h.listOwner(r.Context(), owner, filter)
	if err != nil {
		h.fail(w, "todo: fragment list failed", fmt.Errorf("filter %s: %w", filter, err), "error listing todos")
		return
	}
	signals := todo.Signals{Todos: todos, Filter: filter, ItemCount: len(todos)}
	var buf bytes.Buffer
	if err := h.renderListRegion(signals, h.resolveSkin(r)).Render(r.Context(), &buf); err != nil {
		h.fail(w, "todo: fragment render failed", err, "error rendering list")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("datastar-signals", fmt.Sprintf(`{"itemCount":%d}`, len(todos)))
	w.Header().Set("datastar-selector", "#todo-list")
	w.Header().Set("datastar-mode", "outer")
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}
