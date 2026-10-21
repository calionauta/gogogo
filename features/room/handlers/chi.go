// SCOPE:feature — HTTP room slice (GoAkt presence demo)
//
// Index/roster pages and JSON, heartbeat/acquire/release/leave/crash
// mutations, plus the /room/stream SSE feed. State lives in GoAkt grains
// (never a database); members act as their login email. The roster is
// PUSHED: every mutation re-reads the grain and fans the whole snapshot out
// to the room's tabs (see stream.go), and the clicker gets the same state
// back as a Datastar card patch. /room/roster stays as the JSON handshake
// and for non-browser clients; nothing polls it on a timer.
package handlers

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/go-chi/chi/v5"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	roomcomponents "github.com/calionauta/gogogo/features/room/components"
	"github.com/calionauta/gogogo/internal/auth"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/web/skins"
)

// roomSystem is the grain surface this slice needs. *goakt.Host satisfies
// it in production; tests substitute a fake. One method per user intent.
type roomSystem interface {
	Roster(ctx context.Context, id string) (appgoakt.Roster, error)
	Tell(ctx context.Context, id string, msg any) error
}

// mutateTimeout bounds one click (tell + read-back) so a stalled grain
// cannot park an HTTP handler past the client's patience.
const mutateTimeout = 5 * time.Second

// rosterReadBudget bounds a whole retried roster read. A supervised grain
// restarts inside it, so a transient stall must not surface to the user.
const rosterReadBudget = 5 * time.Second

// rejoinRetryDelay spaces the crash-rejoin retries. Matches rosterRetryDelay:
// the supervised restart is measured in tens of milliseconds, and the budget
// (not the attempt count) is the real bound.
const rejoinRetryDelay = 50 * time.Millisecond

// rosterRetryDelay spaces the retries. Short on purpose: the read is a
// pure question, so retrying it is free and the budget (not the attempt
// count) is the real bound.
const rosterRetryDelay = 50 * time.Millisecond

// Handler serves the room demo on chi. Roster state lives in grains, never
// here; the ONE thing this handler remembers is who is watching, because a
// crashed grain restarts EMPTY and presence is the connection — see
// rejoinWatchers.
type Handler struct {
	cfg   *config.Config
	auth  *auth.Auth
	rooms roomSystem
	hub   *queue.SSEHub
	// readBudget bounds readRoster's retries (overridable in tests).
	readBudget time.Duration
	// watchers maps a room to its live streams (clientID -> member email).
	// The hub cannot supply this: room streams deliberately register with no
	// userID (BroadcastToAllUsers would otherwise feed todo record envelopes
	// into a Datastar connection), so the name is kept where it is known.
	mu       sync.Mutex
	watchers map[string]map[string]string
}

// NewHandler builds the chi room handler. Nil deps panic: wire errors
// surface at boot, not per request.
func NewHandler(cfg *config.Config, a *auth.Auth, rooms roomSystem, hub *queue.SSEHub) *Handler {
	if cfg == nil || a == nil || rooms == nil || hub == nil {
		panic("room chi: NewHandler with nil dependency")
	}
	return &Handler{
		cfg: cfg, auth: a, rooms: rooms, hub: hub,
		readBudget: rosterReadBudget,
		watchers:   make(map[string]map[string]string),
	}
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
// Routes returns the room router. A nil Handler (engine disabled or failed to
// start) returns nil: BootChi calls this unconditionally and MountFeature
// treats nil as "nothing to mount", so /room stays a 404 instead of panicking.
func (h *Handler) Routes() chi.Router {
	if h == nil {
		return nil
	}
	r := chi.NewRouter()
	r.Get("/room", h.handleIndex)
	r.Get("/room/roster", h.handleRoster)
	r.Get("/room/stream", h.handleStream)
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

// owner resolves the member email + role, or redirects guests to
// sign-in. The role rides along so the page can show it and gate the
// operator surfaces (navbar); it is a display value, never a gate.
func (h *Handler) owner(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	sess, err := h.auth.Session(r)
	if err != nil || sess.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return "", "", false
	}
	return sess.User.Email, sess.Role, true
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	email, role, ok := h.owner(w, r)
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
	_ = roomcomponents.Page(
		email, role, id, roster, h.cfg.BuildLabel, h.cfg.BuildCommit, skins.Resolve(r, h.cfg.Skin),
	).Render(r.Context(), w)
}

func (h *Handler) handleRoster(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.owner(w, r); !ok {
		return
	}
	id := appgoakt.SanitizeRoomID(roomID(r))
	roster, err := h.readRoster(r.Context(), id)
	if err != nil {
		h.unavailable(r.Context(), w, id, "roster", err)
		return
	}
	writeJSON(w, http.StatusOK, roster)
}

// mutate applies a grain message for the member, pushes the resulting
// roster to every watching tab, and answers the CLICKER with the patched
// card (Datastar) — one round trip per click, and the clicker and its peers
// converge on one state. The push covers the peers; the patch covers the
// originator, which BroadcastToDoc does not echo back to itself.
func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, msg func(name, clientID string) any) {
	name, _, ok := h.owner(w, r)
	if !ok {
		return
	}
	id := appgoakt.SanitizeRoomID(roomID(r))
	clientID := r.URL.Query().Get("clientID")
	ctx, cancel := context.WithTimeout(r.Context(), mutateTimeout)
	defer cancel()
	if err := h.rooms.Tell(ctx, id, msg(name, clientID)); err != nil {
		h.unavailable(r.Context(), w, id, "tell", err)
		return
	}
	// The clicker is excluded from the broadcast: it gets the same roster in
	// the response below, rendered WITH its identity. A broadcast carries no
	// identity, so without the exclusion the clicker would briefly render its
	// own row without "(you)" before the personal patch corrected it.
	roster, err := h.pushRosterExcept(ctx, id, clientID)
	if err != nil {
		h.unavailable(r.Context(), w, id, "read-after-tell", err)
		return
	}
	h.answerRoster(w, r, id, roster, name)
}

// answerRoster replies with the card patch (Datastar) when the caller is a
// Datastar action, and with plain JSON otherwise. The JSON branch is what
// the contract tests and any non-Datastar client (curl, the crash smoke)
// read, so one endpoint serves both without negotiating.
func (h *Handler) answerRoster(
	w http.ResponseWriter, r *http.Request, id string, roster appgoakt.Roster, email string,
) {
	if r.Header.Get("Datastar-Request") != "true" {
		writeJSON(w, http.StatusOK, roster)
		return
	}
	// ONE payload builder for the clicker and the broadcast (sendRosterPatch),
	// so "what I see after my click" and "what peers see" cannot diverge.
	sse := sdk.NewSSE(w, r)
	if err := sendRosterPatch(r.Context(), sse, id, roster, email); err != nil {
		obsArgs := append(observability.WithComponent("room", "feature"), "room", id, "error", err)
		observability.Warn(r.Context(), "room: answer patch failed", obsArgs...)
	}
}

// The heartbeat is kept as a route because clients that predate the stream
// still send it, and because a name-only touch is how the synthetic
// per-name presence entry (see goakt.presenceKey) stays alive. The stream
// does not use it: a live connection IS the liveness signal.
func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name, clientID string) any {
		return appgoakt.RoomHeartbeat{Name: name, ClientID: clientID}
	})
}

func (h *Handler) handleAcquire(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name, clientID string) any {
		return appgoakt.RoomAcquire{Name: name, ClientID: clientID}
	})
}

func (h *Handler) handleRelease(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name, _ string) any { return appgoakt.RoomRelease{Name: name} })
}

func (h *Handler) handleLeave(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(name, clientID string) any {
		return appgoakt.RoomLeave{Name: name, ClientID: clientID}
	})
}

func (h *Handler) handleCrash(w http.ResponseWriter, r *http.Request) {
	name, _, ok := h.owner(w, r)
	if !ok {
		return
	}
	id := appgoakt.SanitizeRoomID(roomID(r))
	ctx, cancel := context.WithTimeout(r.Context(), mutateTimeout)
	defer cancel()
	if err := h.rooms.Tell(ctx, id, appgoakt.RoomCrash{}); err != nil {
		h.unavailable(r.Context(), w, id, "crash", err)
		return
	}
	// The grain restarts EMPTY by design, and presence is the connection:
	// without re-announcing, every tab would disappear from the roster until
	// it reconnected (the demo exists to watch it rebuild). The connections
	// are still there — tell the fresh grain about them, then push.
	if err := h.rejoinWatchers(ctx, id); err != nil {
		h.unavailable(r.Context(), w, id, "crash-rejoin", err)
		return
	}
	roster, err := h.pushRoster(ctx, id)
	if err != nil {
		h.unavailable(r.Context(), w, id, "crash-rebuild", err)
		return
	}
	h.answerRoster(w, r, id, roster, name)
}

// rejoinWatchers replays every live stream's join into a restarted grain.
//
// The crash tell returns when the actor PROCESSED the message, not when its
// supervisor finished restarting it: GoAkt answers "actor is not alive" for
// that window. Under `-race` the window is wide enough to lose the race, so
// the rejoin retries within the caller's budget until the fresh actor accepts
// the joins. Re-telling is safe: presence is idempotent per clientID, and a
// tab that opened in the meantime simply joins twice.
func (h *Handler) rejoinWatchers(ctx context.Context, id string) error {
	watchers := h.watchersOf(id)
	return retry.Do(
		func() error {
			for clientID, email := range watchers {
				if err := h.rooms.Tell(ctx, id, appgoakt.RoomJoin{Name: email, ClientID: clientID}); err != nil {
					return err
				}
			}
			return nil
		},
		retry.Attempts(100),
		retry.Delay(rejoinRetryDelay),
		retry.Context(ctx),
		retry.LastErrorOnly(true),
	)
}

// addWatcher records one live stream. Called after the grain join, so the
// map never names a tab the grain refused.
func (h *Handler) addWatcher(id, clientID, email string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.watchers[id] == nil {
		h.watchers[id] = make(map[string]string)
	}
	h.watchers[id][clientID] = email
}

// dropWatcher forgets one stream (its last one — the reconnect guard makes
// sure a superseded handler cannot drop its replacement).
func (h *Handler) dropWatcher(id, clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.watchers[id], clientID)
	if len(h.watchers[id]) == 0 {
		delete(h.watchers, id)
	}
}

// watchersOf snapshots the room's live streams, so a crash never holds the
// handler lock while talking to the grain.
func (h *Handler) watchersOf(id string) map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]string, len(h.watchers[id]))
	maps.Copy(out, h.watchers[id])
	return out
}
