// SCOPE:feature — HTTP room slice (GoAkt presence demo)
//
// Same routes, wire and page as the framework handlers: index/roster
// pages and JSON, heartbeat/acquire/release/leave/crash mutations. State
// lives in GoAkt grains (never a database); members act as their login
// email. The roster stays pull-based JSON — no SSE transport to port.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	roomcomponents "github.com/calionauta/gogogo/features/room/components"
	"github.com/calionauta/gogogo/internal/auth"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/observability"
)

// roomSystem is the grain surface this slice needs. *goakt.Host satisfies
// it in production; tests substitute a fake. One method per user intent.
type roomSystem interface {
	Roster(ctx context.Context, id string) (appgoakt.Roster, error)
	Acquire(ctx context.Context, id, name string) (appgoakt.AcquireResult, error)
	Tell(ctx context.Context, id string, msg any) error
}

// mutateTimeout bounds one click (tell + read-back) so a stalled grain
// cannot park an HTTP handler past the client's patience.
const mutateTimeout = 5 * time.Second

// rosterReadBudget bounds a whole retried roster read. A supervised grain
// restarts inside it, so a transient stall must not surface to the user.
const rosterReadBudget = 5 * time.Second

// rosterRetryDelay spaces the retries. Short on purpose: the read is a
// pure question, so retrying it is free and the budget (not the attempt
// count) is the real bound.
const rosterRetryDelay = 50 * time.Millisecond

// Handler serves the room demo on chi. State lives in grains, never here.
type Handler struct {
	cfg   *config.Config
	auth  *auth.Auth
	rooms roomSystem
	// readBudget bounds readRoster's retries (overridable in tests).
	readBudget time.Duration
}

// NewHandler builds the chi room handler. Nil deps panic: wire errors
// surface at boot, not per request.
func NewHandler(cfg *config.Config, a *auth.Auth, rooms roomSystem) *Handler {
	if cfg == nil || a == nil || rooms == nil {
		panic("room chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a, rooms: rooms, readBudget: rosterReadBudget}
}

// readRoster reads the roster, tolerating a transient grain stall (the
// supervised-restart window). The happy path is a single round trip;
// only a failure costs a retry, and the whole read is bounded by the
// request context plus the budget. Ported from the framework handler's
// readRoster so the resilience contract survives the stack swap.
func (h *Handler) readRoster(ctx context.Context, id string) (appgoakt.Roster, error) {
	ctx, cancel := context.WithTimeout(ctx, h.readBudget)
	defer cancel()
	var roster appgoakt.Roster
	err := retry.Do(
		func() error {
			var readErr error
			roster, readErr = h.rooms.Roster(ctx, id)
			return readErr
		},
		retry.Attempts(3),
		retry.Delay(rosterRetryDelay),
		retry.Context(ctx),
		retry.LastErrorOnly(true),
	)
	if err != nil {
		return appgoakt.Roster{}, err
	}
	return roster, nil
}

// Routes returns the chi router with the room pages and API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/room", h.handleIndex)
	r.Get("/room/roster", h.handleRoster)
	r.Post("/room/heartbeat", h.handleHeartbeat)
	r.Post("/room/acquire", h.handleAcquire)
	r.Post("/room/release", h.handleRelease)
	r.Post("/room/leave", h.handleLeave)
	r.Post("/room/crash", h.handleCrash)
	return r
}

func roomID(r *http.Request) string {
	if id := r.URL.Query().Get("room"); id != "" {
		return id
	}
	return "lobby"
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// unavailable logs the grain failure and answers 502 without leaking it:
// the previous generation swallowed the error whole, which is why a CI
// 502 here could not be attributed to a route or a cause from the log.
func (h *Handler) unavailable(ctx context.Context, w http.ResponseWriter, room, step string, err error) {
	obsArgs := append(observability.WithComponent("room", "feature"), "room", room, "step", step, "error", err)
	observability.Warn(ctx, "room: grain unavailable", obsArgs...)
	writeText(w, http.StatusBadGateway, "room engine unavailable")
}

func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

// owner resolves the member email or redirects guests to sign-in.
func (h *Handler) owner(w http.ResponseWriter, r *http.Request) (string, bool) {
	sess, err := h.auth.Session(r)
	if err != nil || sess.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return "", false
	}
	return sess.User.Email, true
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	email, ok := h.owner(w, r)
	if !ok {
		return
	}
	id := roomID(r)
	roster, err := h.readRoster(r.Context(), id)
	if err != nil {
		h.unavailable(r.Context(), w, id, "index", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = roomcomponents.Page(email, id, roster, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

func (h *Handler) handleRoster(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.owner(w, r); !ok {
		return
	}
	id := roomID(r)
	roster, err := h.readRoster(r.Context(), id)
	if err != nil {
		h.unavailable(r.Context(), w, id, "roster", err)
		return
	}
	writeJSON(w, http.StatusOK, roster)
}

// mutate applies a fire-and-forget grain message for the member, then
// answers with the fresh roster (one round trip per click).
func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, msg func(name string) any) {
	name, ok := h.owner(w, r)
	if !ok {
		return
	}
	id := roomID(r)
	ctx, cancel := context.WithTimeout(r.Context(), mutateTimeout)
	defer cancel()
	if err := h.rooms.Tell(ctx, id, msg(name)); err != nil {
		h.unavailable(r.Context(), w, id, "tell", err)
		return
	}
	roster, err := h.readRoster(ctx, id)
	if err != nil {
		h.unavailable(r.Context(), w, id, "read-after-tell", err)
		return
	}
	writeJSON(w, http.StatusOK, roster)
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name string) any { return appgoakt.RoomHeartbeat{Name: name} })
}

func (h *Handler) handleAcquire(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name string) any { return appgoakt.RoomAcquire{Name: name} })
}

func (h *Handler) handleRelease(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name string) any { return appgoakt.RoomRelease{Name: name} })
}

func (h *Handler) handleLeave(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name string) any { return appgoakt.RoomLeave{Name: name} })
}

func (h *Handler) handleCrash(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(_ string) any { return appgoakt.RoomCrash{} })
}
