// SCOPE:layer=feature,removal=feature — Room presence demo (GoAkt grains)
//
// Handler serves the room demo page at GET /room: a named roster with
// heartbeats plus an exactly-one presenter lock, both owned by a GoAkt
// room grain. The grain (not a database row) arbitrates: turn-based
// execution means two concurrent acquirers get exactly one winner, which
// no KV store or pub/sub gives. Roster is soft state — a restarted room
// rebuilds it from heartbeats within seconds.
//
// Auth follows todo/whiteboard: guests redirect to sign-in, members act
// as their login email (no separate display names to manage).
//
// The page polls JSON (GET /room/roster) instead of SSE: the demo values
// a small readable loop over another transport, and roster state is
// pull-friendly (whole-roster snapshots, no event log to replay).
package room

import (
	"context"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	appcfg "github.com/calionauta/gogogo/config"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
)

// roomSystem is the grain surface this feature needs. *goakt.Host
// satisfies it in production; tests substitute a fake. Interface at
// the consumer, one method per user intent.
type roomSystem interface {
	Roster(ctx context.Context, id string) (appgoakt.Roster, error)
	Acquire(ctx context.Context, id, name string) (appgoakt.AcquireResult, error)
	Tell(ctx context.Context, id string, msg any) error
}

// Handler serves the room demo. State lives in grains, never here.
type Handler struct {
	cfg   *appcfg.Config
	rooms roomSystem
}

// New constructs a Handler around a grain host (production) or fake.
func New(cfg *appcfg.Config, rooms roomSystem) *Handler {
	return &Handler{cfg: cfg, rooms: rooms}
}

// RegisterRoutesOn wires the room routes on a router (production passes
// se.Router; tests pass their own). Same shape as whiteboard's
// RegisterRoutesOn: methods are explicit per route (routeutil rule:
// never Router.Any).
func (h *Handler) RegisterRoutesOn(r *router.Router[*core.RequestEvent]) {
	r.GET("/room", h.handleIndex)
	r.GET("/room/roster", h.handleRoster)
	r.POST("/room/heartbeat", h.handleHeartbeat)
	r.POST("/room/acquire", h.handleAcquire)
	r.POST("/room/release", h.handleRelease)
	r.POST("/room/leave", h.handleLeave)
	r.POST("/room/crash", h.handleCrash)
}

// RegisterRoutes wires the room routes DIRECTLY on se.Router (same
// pattern as todo + whiteboard + landing + config).
func (h *Handler) RegisterRoutes(se *core.ServeEvent) {
	h.RegisterRoutesOn(se.Router)
}

// roomID reads the demo room from the query, defaulting to "lobby".
// Sanitizing happens grain-side too; this is just the default.
func roomID(c *core.RequestEvent) string {
	if id := c.Request.URL.Query().Get("room"); id != "" {
		return id
	}
	return "lobby"
}

func (h *Handler) handleIndex(c *core.RequestEvent) error {
	if c.Auth == nil {
		return c.Redirect(http.StatusSeeOther, "/login")
	}
	id := roomID(c)
	roster, err := h.rooms.Roster(c.Request.Context(), id)
	if err != nil {
		return c.String(http.StatusBadGateway, "room engine unavailable")
	}
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.WriteHeader(http.StatusOK)
	return Page(c.Auth.Email(), id, roster, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(c.Request.Context(), c.Response)
}

func (h *Handler) handleRoster(c *core.RequestEvent) error {
	if c.Auth == nil {
		return c.Redirect(http.StatusSeeOther, "/login")
	}
	roster, err := h.rooms.Roster(c.Request.Context(), roomID(c))
	if err != nil {
		return c.String(http.StatusBadGateway, "room engine unavailable")
	}
	return c.JSON(http.StatusOK, roster)
}

// mutate applies a fire-and-forget grain message for the authed member,
// then answers with the fresh roster (one round trip per click).
func (h *Handler) mutate(c *core.RequestEvent, msg func(name string) any) error {
	if c.Auth == nil {
		return c.Redirect(http.StatusSeeOther, "/login")
	}
	id := roomID(c)
	name := c.Auth.Email()
	if err := h.rooms.Tell(c.Request.Context(), id, msg(name)); err != nil {
		return c.String(http.StatusBadGateway, "room engine unavailable")
	}
	roster, err := h.rooms.Roster(c.Request.Context(), id)
	if err != nil {
		return c.String(http.StatusBadGateway, "room engine unavailable")
	}
	return c.JSON(http.StatusOK, roster)
}

func (h *Handler) handleHeartbeat(c *core.RequestEvent) error {
	return h.mutate(c, func(name string) any { return appgoakt.RoomHeartbeat{Name: name} })
}

func (h *Handler) handleAcquire(c *core.RequestEvent) error {
	return h.mutate(c, func(name string) any { return appgoakt.RoomAcquire{Name: name} })
}

func (h *Handler) handleRelease(c *core.RequestEvent) error {
	return h.mutate(c, func(name string) any { return appgoakt.RoomRelease{Name: name} })
}

func (h *Handler) handleLeave(c *core.RequestEvent) error {
	return h.mutate(c, func(name string) any { return appgoakt.RoomLeave{Name: name} })
}

func (h *Handler) handleCrash(c *core.RequestEvent) error {
	return h.mutate(c, func(_ string) any { return appgoakt.RoomCrash{} })
}
