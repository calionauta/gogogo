// SCOPE:feature — HTTP todo slice (flat stack)
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
package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

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
	"github.com/calionauta/gogogo/internal/instance"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
	basecoat "github.com/calionauta/gogogo/web/skins/basecoat"
)

// SkinBasecoat and SkinShadcn are duplicated (not imported) from the
// framework handler package so this package stays framework-free.
const (
	SkinBasecoat = "basecoat"
	SkinShadcn   = "shadcn"
)

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
	// bus is the ONE publish point for todo facts: the hub sink always, the
	// NATS wire when AttachJetStream wires it. Constructed with the handler,
	// so handlers cannot fan out through a private path.
	bus *todo.Bus
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
	// Search reads the search plugin's rankings, and Index writes its
	// postings. Both are interfaces declared here with stdlib types only
	// (the same rule as OnboardingResumer): the plugin lives in
	// internal/search, and trimming it deletes its package and its two
	// files in this one without touching these declarations. Nil (plugin
	// trimmed, or never wired) means no search box and no index writes.
	//
	// It is a SLICE, keyed by mode name, because the layer is a runtime choice:
	// SEARCH_MODE picks the default a tab starts on, and the box's mode selector
	// switches between the wired ones without a restart. One entry is the normal
	// case for a trimmed build (substring only), which is also why every reader
	// degrades to the first entry when a requested mode is absent.
	Search SearchLayers
	Index  SearchIndexer
	// DB is the pool the index writes ride a transaction on. Only read
	// when Index is non-nil: with no index there is nothing to make atomic,
	// and the mutation keeps its plain autocommit path.
	DB *sql.DB
}

// SearchIndexer is the write half: postings kept in step with the todo
// rows they describe. Split from Searcher on purpose, mirroring the
// plugin's own Source/Index split — the substring and fuzzy layers need no
// index and no extra write, so their mode wires no SearchIndexer at all.
// Every method takes the caller's transaction, so a row and its postings
// commit or roll back together.
type SearchIndexer interface {
	IndexTodo(ctx context.Context, tx *sql.Tx, owner, docID, title string) error
	DeleteTodo(ctx context.Context, tx *sql.Tx, owner, docID string) error
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

// SetSearch wires the search plugin: the readers every list honours, the
// optional indexer, and the pool its transactions ride. A nil indexer is
// the norm (substring and fuzzy need no postings) and simply leaves the
// mutations on their autocommit path. An empty set is the trimmed plugin.
func (h *Handler) SetSearch(layers SearchLayers, ix SearchIndexer, db *sql.DB) {
	h.Search, h.Index, h.DB = layers, ix, db
}

// SetPublisher installs the cross-instance publisher. Nil disables it.
func (h *Handler) SetPublisher(p *natswire.CrudPublisher) {
	if p == nil {
		h.bus.SetWire(nil)
		return
	}
	h.bus.SetWire(wireSink{pub: p})
}

// Bus exposes the event bus so the cross-instance consumer and embedded
// frontends can publish locally / subscribe in-process.
func (h *Handler) Bus() *todo.Bus { return h.bus }

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
	return &Handler{
		Q: q, Auth: a, Hub: hub, DefaultSkin: defaultSkin,
		bus: todo.NewBus(hubSink{hub: hub}, instance.ID()),
	}
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
	if err := createTodoIndexed(ctx, h.Q, h.Index, h.DB, dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: title,
		Completed: false, IdemKey: "", Visibility: todo.VisibilityPrivate, Created: now, Updated: now,
	}); err != nil {
		return fmt.Errorf("onboarding create %q: %w", title, err)
	}
	return nil
}

// Routes returns the chi router with the todo pages and data endpoints.

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/api/todos", h.handleList)
	r.Get("/api/todos/search", h.handleSearch)
	r.Get("/api/todos/fragment", h.handleFragment)
	r.Post("/api/todos", h.handleCreate)
	r.Post("/api/todos/{id}/toggle", h.handleToggle)
	r.Post("/api/todos/completed/delete", h.handleClearCompleted)
	r.Post("/api/todos/{id}/delete", h.handleDelete)
	r.Get("/api/todos/{id}/confirm-delete", h.handleConfirmDelete)
	r.Get("/api/todos/stream", h.handleStream)
	r.Get("/api/todos/events", h.handleEvents)
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

// resolveSkin reports the skin to render with. Basecoat is opt-in through
// the query string and falls back to the configured default; anything else
// normalises to daisyui. Each basecoat test is its own block on purpose:
// trimming the extra skin then removes them without leaving an unused local,
// which a normalise-then-special-case shape would.
func (h *Handler) resolveSkin(r *http.Request) string {
	if q := r.URL.Query().Get("skin"); q != "" {
		switch q {
		case SkinBasecoat, SkinShadcn:
			return q
		}
		return "daisyui"
	}
	switch h.DefaultSkin {
	case SkinBasecoat, SkinShadcn:
		return h.DefaultSkin
	}
	return "daisyui"
}

func (h *Handler) renderListRegion(signals todo.Signals, skin string) templ.Component {
	if skin == SkinBasecoat {
		return basecoat.TodoListRegion(signals)
	}
	return components.TodoListRegion(signals)
}

// emitToast appends one toast to #toast-container; it self-removes after 3s
// (8s for errors) and needs no action from the user. Used by the todo page's
// own mutations, where the list visibly updates in the same response.
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

// Record-event names: the browser contract carried on the hub. Kept as
// constants because the mapping exists in both directions (publish and
// consume) and a typo would silently stop a re-list.
const (
	recordCreated = "created"
	recordToggled = "toggled"
	recordDeleted = "deleted"
	recordCleared = "cleared"
)

// recordEvent is the hub payload for todo mutations: what changed
// (created/toggled/deleted/cleared) plus the item data. Each stream
// re-lists scoped on receipt; the payload itself only names the change.
type recordEvent struct {
	Event string `json:"event"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// publish fans one domain fact out to every adapter: the local hub, the
// in-process subscribers, and (when wired) the cross-instance wire. One call
// site per mutation, so the transports cannot drift apart again.
func (h *Handler) publish(ctx context.Context, r *http.Request, ev todo.Event) {
	ev.OriginClient = originClient(r)
	h.bus.Publish(ctx, ev)
}

// originClient is the tab that triggered the mutation, excluded from the
// hub fan-out (it already got the synchronous patch).
//
// Offline replays are the exception: a replayed request runs in the
// background, long after the originating interaction, and its response is a
// bare "replayed" acknowledgment — no synchronous patch. Excluding the
// queued clientID would leave the queuing tab with only its optimistic row
// forever, so a replay fans out to every stream, originator included.
func originClient(r *http.Request) string {
	if r.Header.Get("X-Offline-Replay") == "1" {
		return ""
	}
	if id := r.URL.Query().Get("clientID"); id != "" {
		return id
	}
	return r.Header.Get("X-Client-ID")
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
	filter, scope := filterOf(r), scopeOf(r)
	_, role := h.roleOf(r)
	// Remember the tab's listing slice on its stream registration, so
	// record-event re-lists preserve what the tab was looking at.
	h.Hub.SetView(r.URL.Query().Get("clientID"), filter, scope, searchQueryOf(r), searchModeOf(r))
	todos, err := h.listVisible(r.Context(), owner, role, filter, scope)
	if err != nil {
		h.fail(r.Context(), w, "todo: list failed",
			fmt.Errorf("filter %s scope %s: %w", filter, scope, err), "error listing todos")
		return
	}
	todos, err = h.narrowBySearch(r.Context(), owner, searchQueryOf(r), searchModeOf(r), todos)
	if err != nil {
		h.fail(r.Context(), w, "todo: search failed", err, "error listing todos")
		return
	}
	signals := h.displaySignals(r, todos, filter, scope)
	signals.SearchMode = h.activeSearchMode(searchModeOf(r))
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
	filter, scope := filterOf(r), scopeOf(r)
	_, role := h.roleOf(r)
	h.Hub.SetView(r.URL.Query().Get("clientID"), filter, scope, searchQueryOf(r), searchModeOf(r))
	todos, err := h.listVisible(r.Context(), owner, role, filter, scope)
	if err != nil {
		h.fail(r.Context(), w, "todo: fragment list failed",
			fmt.Errorf("filter %s scope %s: %w", filter, scope, err), "error listing todos")
		return
	}
	todos, err = h.narrowBySearch(r.Context(), owner, searchQueryOf(r), searchModeOf(r), todos)
	if err != nil {
		h.fail(r.Context(), w, "todo: fragment search failed", err, "error listing todos")
		return
	}
	signals := h.displaySignals(r, todos, filter, scope)
	signals.SearchMode = h.activeSearchMode(searchModeOf(r))
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
