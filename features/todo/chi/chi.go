// SCOPE:feature — Chi todo slice (flat stack)
//
// Package todo's chi Handler serves the todo surface on the flat stack:
// chi routing, owned auth sessions, sqlc queries called directly, the same
// Templ components, the same Datastar SSE wire. Paths, statuses, redirects
// and fragment headers follow the documented HTTP/SSE contracts in the
// contract tests — the client cannot tell which stack answered.
//
// Scope: auth pages (login/logout), the todo index page, and the data
// endpoints (list/fragment/CRUD/confirm/stream). AI suggest, retry demo
// and onboarding live elsewhere; cross-instance sync goes through the
// NATS bridge (bridge.go) while the SSE hub carries record sync.
package chi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/rbac"
	basecoat "github.com/calionauta/gogogo/web/skins/basecoat"
)

// SkinBasecoat is duplicated (not imported) from the framework handler
// package so this package stays framework-free.
const SkinBasecoat = "basecoat"

// Handler serves the todo data endpoints on chi. Q is the concrete
// sqlc queries (never an interface — a wrong call is a compile error);
// Hub is a chi-stack-private SSE hub so chi record events never leak
// into other stacks' clients and vice versa.
type Handler struct {
	Q           *dbgen.Queries
	Auth        *auth.Auth
	Hub         *queue.SSEHub
	DefaultSkin string
	// LLM is the real suggest client (nil = disabled, 503 on the route).
	LLM *llm.Client
	// SimulatedLLM is the keyless fake used by the demo stepper.
	SimulatedLLM *llm.Client
	// Queue carries suggest/retry jobs. Nil is valid until wired:
	// enqueue fails closed with 500 naming the missing wiring.
	Queue *queue.Queue
	// Pub publishes mutations to JetStream for cross-instance convergence.
	// Nil (NATS disabled) is a valid zero value: Publish is nil-safe, so
	// the stack converges in-process through the Hub only. Set via
	// SetPublisher so tests can run hub-only without a NATS server.
	Pub *natswire.CrudPublisher
	// BuildLabel/BuildCommit stamp the navbar version badge (from -ldflags
	// in production builds, "dev"/"unknown" otherwise).
	BuildLabel  string
	BuildCommit string
	// OfflineSyncEnabled mirrors the config flag into the page signals.
	OfflineSyncEnabled bool
	// NATSEnabled switches the realtime label between the in-process hub
	// and JetStream. Set from config at boot.
	NATSEnabled bool
	// Onboarding is the durable-workflow resumer the create path calls.
	// It is an interface, not a concrete handler, on purpose: the engine
	// lives in features/onboarding, and this package must not import it —
	// trimming the engine deletes its package without touching a line here.
	// Nil (engine disabled, trimmed, or never wired) is a no-op.
	Onboarding OnboardingResumer
}

// OnboardingResumer is what the create path needs from the onboarding flow:
// resume it when a user with a pending run adds their first todo. One
// method on purpose, so the engine depends on nothing here and this package
// depends on nothing there — the method shape is the entire contract.
type OnboardingResumer interface {
	ResumeOnboarding(owner string)
}

// SetOnboarding wires the durable-workflow resumer the create path resumes.
// Nil disables the resume hook without failing boot.
func (h *Handler) SetOnboarding(o OnboardingResumer) { h.Onboarding = o }

// SetPublisher installs the cross-instance publisher. Nil disables it.
func (h *Handler) SetPublisher(p *natswire.CrudPublisher) {
	h.Pub = p
}

// SetLLMClient wires the real suggest client. Nil disables AI features.
func (h *Handler) SetLLMClient(c *llm.Client) { h.LLM = c }

// SetSimulatedLLMClient wires the in-process fake LLM for the demo path.
func (h *Handler) SetSimulatedLLMClient(c *llm.Client) { h.SimulatedLLM = c }

// SetQueue wires the suggest/retry queue. Nil fails closed on enqueue.
func (h *Handler) SetQueue(q *queue.Queue) { h.Queue = q }

// NewHandler builds the chi todo handler. All deps required — a nil Q,
// Auth or Hub is a programming error, surfaced here and not per request.
func NewHandler(q *dbgen.Queries, a *auth.Auth, hub *queue.SSEHub, defaultSkin string) *Handler {
	if q == nil || a == nil || hub == nil {
		panic("todo: NewHandler with nil dependency")
	}
	return &Handler{Q: q, Auth: a, Hub: hub, DefaultSkin: defaultSkin}
}

// CreateTodoForOnboarding programmatically creates a todo for the DagNats
// onboarding worker (no-op when DAGNATS_ENABLED=false). owner MUST be
// non-empty — the store scopes every row by owner, and an empty one would
// make the example todos invisible to the user who triggered the run.
//
// It deliberately goes straight to the store rather than through the
// request path: the worker is not an HTTP caller, so there is no form to
// parse, no SSE to patch, and no session to resolve.
func (h *Handler) CreateTodoForOnboarding(ctx context.Context, title, owner string) error {
	if owner == "" {
		return errors.New("todo: onboarding create requires an owner")
	}
	now := dbschema.Now()
	if _, err := h.Q.CreateTodo(ctx, dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: title,
		Completed: false, IdemKey: "", Created: now, Updated: now,
	}); err != nil {
		return fmt.Errorf("onboarding create %q: %w", title, err)
	}
	return nil
}

// Routes returns the chi router with the todo pages and data endpoints.

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
	r.Post("/api/todos/suggest", h.handleSuggest)
	r.Post("/api/todos/suggest-simulated", h.handleSuggestSimulated)
	r.Post("/api/todos/retry-demo", h.handleRetryDemo)
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

// isViewerSession reports whether the request carries the restricted demo
// role. Display hint only (delete affordances + paid-action visibility) —
// enforcement stays in the RBAC policy, so callers never branch access on
// this. Unknown/missing sessions are not viewers (fail-open display).
func isViewerSession(sess *auth.ValidatedSession) bool {
	return sess != nil && rbac.IsViewer(sess.Role)
}

// isViewer resolves the viewer flag for request paths carrying a session.
// One extra indexed user lookup per render; mutations already issue
// several writes, so this does not move their cost profile.
func (h *Handler) isViewer(r *http.Request) bool {
	sess, err := h.Auth.Session(r)
	if err != nil {
		return false
	}
	return isViewerSession(sess)
}

// viewerOwner resolves the viewer flag for stream paths, which know the
// owner id but carry no session. A lookup miss fails open (affordance
// shown, policy still enforces) — display must never lock out, only
// enforcement denies.
func (h *Handler) viewerOwner(ctx context.Context, owner string) bool {
	if owner == "" || h.Q == nil {
		return false
	}
	u, err := h.Q.GetUser(ctx, owner)
	if err != nil {
		return false
	}
	return rbac.IsViewer(u.Role)
}

// dagnatsUIEnabled reports the durable-workflow UI truth: REGISTRATION,
// not config. Nil Onboarding (engine disabled, trimmed, or never wired)
// hides the Durable Workflow tab; bootOnboarding only registers when the
// engine is enabled, so a stale resumer under a disabled engine can never
// show the tab.
func (h *Handler) dagnatsUIEnabled() bool { return h.Onboarding != nil }

// realtimeKind labels the active broadcast transport for the diagnostics
// panel.
func (h *Handler) realtimeKind() string {
	if h.NATSEnabled {
		return "JetStream"
	}
	return "in-memory"
}

// offlineLabel describes offline-sync behaviour for the page sub-header.
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
// (303, never 401 — the Datastar client follows redirects).
func redirectLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

func (h *Handler) fail(ctx context.Context, w http.ResponseWriter, logMsg string, err error, publicMsg string) {
	obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
	observability.Error(ctx, logMsg, obsArgs...)
	writeText(w, http.StatusInternalServerError, publicMsg)
}

// toTodo maps a sqlc row onto the wire model. Timestamps are caller-written
// RFC3339 (dbschema.TimeFormat); a row that does not parse is data corruption,
// flagged as a 500 rather than rendered half-empty.
func toTodo(row dbgen.Todo) (todo.Todo, error) {
	created, err := time.Parse(dbschema.TimeFormat, row.Created)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s created %q: %w", row.ID, row.Created, err)
	}
	updated, err := time.Parse(dbschema.TimeFormat, row.Updated)
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
// "" (all), "active", "completed". Unknown filters read as "all".
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

// resolveSkin reports the skin to render with. Basecoat is opt-in through
// the query string and falls back to the configured default; anything else
// normalises to daisyui. Each basecoat test is its own block on purpose:
// trimming the extra skin then removes them without leaving an unused local,
// which a normalise-then-special-case shape would.
func (h *Handler) resolveSkin(r *http.Request) string {
	if q := r.URL.Query().Get("skin"); q != "" {
		if q == SkinBasecoat {
			return q
		}
		return "daisyui"
	}
	if h.DefaultSkin == SkinBasecoat {
		return SkinBasecoat
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

// recordEvent is the hub payload for todo mutations: what changed
// (created/toggled/deleted/cleared) plus the item data. Each stream
// re-lists scoped on receipt; the payload itself only names the change.
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
	r *http.Request, owner string, evt recordEvent, op natswire.CrudOpType, data *natswire.CrudOpData,
) {
	h.broadcastRecord(r, owner, evt)
	h.Pub.Publish(op, owner, data)
}

// broadcastRecord publishes a mutation to the owner's other streams,
// excluding the originating client when known (it already got the
// synchronous patch below).
//
// Offline replays are the exception: a replayed request runs in the
// background, long after the originating interaction, and its response
// is a bare "replayed" acknowledgment — no synchronous patch. Excluding
// the queued clientID would leave the tab that queued the mutation with
// only its optimistic row forever. So replays fan out to every stream,
// originator included.
func (h *Handler) broadcastRecord(r *http.Request, owner string, evt recordEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
		observability.Error(r.Context(), "todo: marshal record event", obsArgs...)
		return
	}
	exclude := r.URL.Query().Get("clientID")
	if exclude == "" {
		exclude = r.Header.Get("X-Client-ID")
	}
	if r.Header.Get("X-Offline-Replay") == "1" {
		exclude = ""
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
		h.fail(r.Context(), w, "todo: list failed", fmt.Errorf("filter %s: %w", filter, err), "error listing todos")
		return
	}
	signals := todo.Signals{
		Todos: todos, Filter: filter, ItemCount: len(todos),
		DagNatsEnabled: h.dagnatsUIEnabled(),
		IsViewer:       h.isViewer(r),
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, signals); err != nil {
		h.fail(r.Context(), w, "todo: list signals failed", err, "error listing todos")
		return
	}
	if err := patchSSE(
		sse, h.renderListRegion(signals, h.resolveSkin(r)),
		sdk.WithSelector("#todo-list"),
	); err != nil {
		h.fail(r.Context(), w, "todo: list patch failed", err, "error listing todos")
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
		h.fail(r.Context(), w, "todo: fragment list failed", fmt.Errorf("filter %s: %w", filter, err), "error listing todos")
		return
	}
	signals := todo.Signals{
		Todos: todos, Filter: filter, ItemCount: len(todos),
		DagNatsEnabled: h.dagnatsUIEnabled(),
		IsViewer:       h.isViewer(r),
	}
	var buf bytes.Buffer
	if err := h.renderListRegion(signals, h.resolveSkin(r)).Render(r.Context(), &buf); err != nil {
		h.fail(r.Context(), w, "todo: fragment render failed", err, "error rendering list")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("datastar-signals", fmt.Sprintf(`{"itemCount":%d}`, len(todos)))
	w.Header().Set("datastar-selector", "#todo-list")
	w.Header().Set("datastar-mode", "outer")
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}
