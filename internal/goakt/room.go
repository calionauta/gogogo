// SCOPE:layer=infra,removal=plugin — GoAkt entity actors (room authority demo)
//
// Package goakt hosts the template's actor system: one room grain per
// chat/whiteboard-style room, holding the roster and the presenter lock
// that no broadcast store can arbitrate (two acquirers, exactly one
// winner). Turn-based execution serializes access, so the roster needs
// no mutex; supervision restarts crashed rooms, and heartbeats rebuild
// the roster afterwards — soft state by design, never persisted.
package goakt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/passivation"
	"github.com/tochemey/goakt/v4/supervisor"
)

// StaleAfter is how long a member or presenter claim survives without a
// heartbeat. Expiry is evaluated lazily on read (roster, acquire), so no
// timers or schedulers are involved.
const StaleAfter = 30 * time.Second

// AskTimeout bounds every grain query from HTTP handlers.
const AskTimeout = 2 * time.Second

// IdlePassivateAfter is the inactivity period after which an idle room
// passivates (memory freed; next message reinstates it and heartbeats
// rebuild the roster).
const IdlePassivateAfter = 10 * time.Minute

// Member is one roster entry.
type Member struct {
	Name     string    `json:"name"`
	LastSeen time.Time `json:"lastSeen"`
}

// Roster is the query response for a room.
type Roster struct {
	Members    []Member `json:"members"`
	Presenter  string   `json:"presenter,omitempty"`
	Generation int      `json:"generation"`
}

// AcquireResult answers a presenter-lock attempt.
type AcquireResult struct {
	OK     bool   `json:"ok"`
	Holder string `json:"holder,omitempty"`
}

// Room messages. Plain structs: local delivery passes them by reference,
// and Ask replies come back through ctx.Response.
type RoomJoin struct {
	Name string
}
type RoomHeartbeat struct {
	Name string
}
type RoomLeave struct {
	Name string
}
type RoomAcquire struct {
	Name string
}
type RoomRelease struct {
	Name string
}
type RoomRoster struct{}

// RoomCrash is the demo hook: handling it panics, the supervisor
// restarts the room, and the roster rebuilds from heartbeats.
type RoomCrash struct{}

// Room is one chat-room grain: roster plus exactly-one presenter lock.
// All fields are touched only inside Receive (turn-based), so no mutex.
type Room struct {
	members    map[string]time.Time
	presenter  string
	generation int
	expiry     time.Duration
}

// NewRoom builds a room grain with the production expiry.
func NewRoom() *Room {
	return &Room{expiry: StaleAfter}
}

// newRoomWithExpiry is the test seam (short expiries keep tests fast).
func newRoomWithExpiry(d time.Duration) *Room {
	return &Room{expiry: d}
}

// PreStart runs on spawn and on every restart (documented contract), so
// it must be safe to run more than once: fresh roster, bumped generation.
// Generation is how the demo page shows a restart happened.
func (r *Room) PreStart(_ *goakt.Context) error {
	r.members = make(map[string]time.Time)
	r.presenter = ""
	r.generation++
	return nil
}

// Receive dispatches one message at a time; no two handlers overlap.
func (r *Room) Receive(ctx *goakt.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case RoomJoin:
		r.touch(msg.Name)
	case RoomHeartbeat:
		r.touch(msg.Name)
	case RoomLeave:
		delete(r.members, msg.Name)
		if r.presenter == msg.Name {
			r.presenter = ""
		}
	case RoomAcquire:
		r.prune()
		if r.presenter == "" {
			r.presenter = msg.Name
			r.touch(msg.Name)
			ctx.Response(AcquireResult{OK: true, Holder: msg.Name})
		} else {
			ctx.Response(AcquireResult{OK: false, Holder: r.presenter})
		}
	case RoomRelease:
		if r.presenter == msg.Name {
			r.presenter = ""
		}
	case RoomRoster:
		ctx.Response(r.roster())
	case RoomCrash:
		panic("room crash requested (demo hook)")
	}
}

// PostStop needs no cleanup: roster is soft state, rebuilt by heartbeats.
func (r *Room) PostStop(_ *goakt.Context) error { return nil }

func (r *Room) touch(name string) {
	if name == "" {
		return
	}
	r.members[name] = time.Now()
}

// prune drops members (and a presenter claim) idle past the expiry.
func (r *Room) prune() {
	now := time.Now()
	for name, seen := range r.members {
		if now.Sub(seen) > r.expiry {
			delete(r.members, name)
			if r.presenter == name {
				r.presenter = ""
			}
		}
	}
}

func (r *Room) roster() Roster {
	r.prune()
	out := Roster{Generation: r.generation, Presenter: r.presenter}
	for name, seen := range r.members {
		out.Members = append(out.Members, Member{Name: name, LastSeen: seen})
	}
	return out
}

// defaultHost is the process-wide room host, set by cmd/web at boot
// (mirroring nats.Conn()). Nil until StartDefault runs or after
// StopDefault — registerRoomStack treats nil as disabled.
var (
	defaultMu   sync.Mutex
	defaultHost *Host
)

// SetDefault publishes (or clears, with nil) the process-wide host.
// cmd/web calls it on boot/shutdown; router resolves through Default.
func SetDefault(h *Host) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultHost = h
}

// Default returns the process-wide host, or nil when the engine is
// disabled or never booted.
func Default() *Host {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	return defaultHost
}

// Host owns the actor system and the room PIDs. rooms is the only shared
// state here (HTTP handlers resolve PIDs through it), hence the mutex —
// everything inside a Room stays lock-free by turn-based execution.
type Host struct {
	mu     sync.Mutex
	system goakt.ActorSystem
	sup    *supervisor.Supervisor
	rooms  map[string]*goakt.PID
	expiry time.Duration
}

// New builds the host. The system starts in Start (lifecycle-owned,
// mirroring startDagNats), never here — no goroutines in constructors.
func New() *Host {
	return &Host{
		sup: supervisor.NewSupervisor(
			supervisor.WithAnyErrorDirective(supervisor.RestartDirective),
			supervisor.WithRetry(3, time.Minute),
		),
		rooms:  make(map[string]*goakt.PID),
		expiry: StaleAfter,
	}
}

// Start boots the standalone actor system: single process, no network,
// no discovery — distribution is out of scope for this demo unit.
func (h *Host) Start(ctx context.Context) error {
	system, err := goakt.NewActorSystem("gogogo-rooms")
	if err != nil {
		return fmt.Errorf("goakt: new actor system: %w", err)
	}
	if err := system.Start(ctx); err != nil {
		return fmt.Errorf("goakt: start actor system: %w", err)
	}
	h.mu.Lock()
	h.system = system
	h.mu.Unlock()
	return nil
}

// Stop terminates the actor system; it does not terminate the process.
func (h *Host) Stop(ctx context.Context) error {
	h.mu.Lock()
	system := h.system
	h.mu.Unlock()
	if system == nil {
		return nil
	}
	return system.Stop(ctx)
}

// room resolves (spawning on demand) the grain for id. Spawn carries the
// supervisor (restart on panic, 3-try budget) and the idle-passivation
// strategy. Room ids are sanitized: actor names must be stable path
// segments, never raw user input.
func (h *Host) room(ctx context.Context, id string) (*goakt.PID, error) {
	id = sanitizeRoomID(id)
	h.mu.Lock()
	defer h.mu.Unlock()
	if pid, ok := h.rooms[id]; ok {
		return pid, nil
	}
	if h.system == nil {
		return nil, errors.New("goakt: host not started")
	}
	var room *Room
	if h.expiry == StaleAfter {
		room = NewRoom()
	} else {
		// Tests override h.expiry directly (same package) for fast,
		// deterministic expiry; production always uses NewRoom.
		room = newRoomWithExpiry(h.expiry)
	}
	pid, err := h.system.Spawn(ctx, "room-"+id, room,
		goakt.WithSupervisor(h.sup),
		goakt.WithPassivationStrategy(passivation.NewTimeBasedStrategy(IdlePassivateAfter)),
	)
	if err != nil {
		return nil, fmt.Errorf("goakt: spawn room %q: %w", id, err)
	}
	h.rooms[id] = pid
	return pid, nil
}

// askRoom resolves the grain and queries it with a bounded timeout.
// Roster and Acquire share it so the resolve→ask→assert shape exists once.
func (h *Host) askRoom(ctx context.Context, id string, msg any) (any, error) {
	pid, err := h.room(ctx, id)
	if err != nil {
		return nil, err
	}
	return goakt.Ask(ctx, pid, msg, AskTimeout)
}

// Roster queries the room grain (Ask with a bounded timeout).
func (h *Host) Roster(ctx context.Context, id string) (Roster, error) {
	raw, err := h.askRoom(ctx, id, RoomRoster{})
	if err != nil {
		return Roster{}, fmt.Errorf("goakt: roster %q: %w", id, err)
	}
	out, ok := raw.(Roster)
	if !ok {
		return Roster{}, fmt.Errorf("goakt: roster %q: unexpected reply %T", id, raw)
	}
	return out, nil
}

// Acquire attempts the presenter lock (Ask: the caller needs the verdict).
func (h *Host) Acquire(ctx context.Context, id, name string) (AcquireResult, error) {
	raw, err := h.askRoom(ctx, id, RoomAcquire{Name: name})
	if err != nil {
		return AcquireResult{}, fmt.Errorf("goakt: acquire %q: %w", id, err)
	}
	out, ok := raw.(AcquireResult)
	if !ok {
		return AcquireResult{}, fmt.Errorf("goakt: acquire %q: unexpected reply %T", id, raw)
	}
	return out, nil
}

// Tell delivers a fire-and-forget room message (join, heartbeat, leave,
// release, crash). Callers never wait: roster reads go through Roster.
func (h *Host) Tell(ctx context.Context, id string, msg any) error {
	pid, err := h.room(ctx, id)
	if err != nil {
		return err
	}
	return goakt.Tell(ctx, pid, msg)
}

// sanitizeRoomID keeps actor names to stable path segments.
func sanitizeRoomID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "lobby"
	}
	return b.String()
}
