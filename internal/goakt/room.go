// SCOPE:plugin — GoAkt entity actors (room authority demo)
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
	"sort"
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
//
// ClientID is the per-tab connection id (the SSE stream's clientID).
// Presence is tracked per CONNECTION, not per name: one person with two
// tabs is two connections, and closing one must not evict them from the
// roster. Name is the display identity (the login email) the roster shows
// and the presenter lock is keyed by.
type RoomJoin struct {
	Name     string
	ClientID string
}
type RoomHeartbeat struct {
	Name     string
	ClientID string
}
type RoomLeave struct {
	Name     string
	ClientID string
}
type RoomAcquire struct {
	Name     string
	ClientID string
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
//
// Presence is connection-scoped: conns maps clientID -> {name, lastSeen},
// and the roster derives one Member per NAME. Two tabs of one person are
// two conns and one member; the member (and any lock they hold) survives
// until their LAST connection goes. Without this, an SSE stream (whose
// disconnect is the leave signal) evicted a user the moment they closed
// one of their tabs — the failure mode a heartbeat cannot have, because a
// heartbeat is idempotent per name.
type Room struct {
	conns      map[string]connInfo
	presenter  string
	generation int
	expiry     time.Duration
}

// connInfo is one live connection: the display identity it belongs to and
// when it was last confirmed alive.
type connInfo struct {
	name string
	seen time.Time
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
	r.conns = make(map[string]connInfo)
	r.presenter = ""
	r.generation++
	return nil
}

// Receive dispatches one message at a time; no two handlers overlap.
func (r *Room) Receive(ctx *goakt.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case RoomJoin:
		r.touch(msg.Name, msg.ClientID)
	case RoomHeartbeat:
		r.touch(msg.Name, msg.ClientID)
	case RoomLeave:
		r.leave(msg.Name, msg.ClientID)
	case RoomAcquire:
		r.prune()
		// Acquiring SUCCEEDS when the lock is free OR when you already hold it:
		// re-acquiring your own lock is a no-op, and telling the holder "no"
		// made the button lie (200 with no change and no explanation).
		result := AcquireResult{OK: false, Holder: r.presenter}
		switch r.presenter {
		case "", msg.Name:
			r.presenter = msg.Name
			result = AcquireResult{OK: true, Holder: msg.Name}
		}
		if result.OK {
			// Taking the lock is also proof of presence: refresh the
			// acquiring tab's OWN connection, never a synthetic per-name
			// entry (that one would outlive every real tab and keep a
			// departed member on the roster).
			r.touch(msg.Name, msg.ClientID)
		}
		ctx.Response(result)
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

// PostStop needs no cleanup: roster is soft state, rebuilt by connections.
func (r *Room) PostStop(_ *goakt.Context) error { return nil }

// presenceKey is the connection a touch/heartbeat belongs to. A message
// that carries no ClientID (the pre-SSE heartbeat path, still served by
// POST /room/heartbeat) is folded into ONE synthetic connection per name,
// so it behaves exactly as the old per-name map did — idempotent, and
// unable to represent "two tabs of one person".
func presenceKey(name, clientID string) string {
	if clientID != "" {
		return clientID
	}
	return "name:" + name
}

// touch marks one connection alive. One person with two tabs is two
// entries; the roster folds them into one member (see roster).
func (r *Room) touch(name, clientID string) {
	if name == "" {
		return
	}
	key := presenceKey(name, clientID)
	r.conns[key] = connInfo{name: name, seen: time.Now()}
}

// leave drops ONE connection. The member stays while any other connection
// of theirs is alive, and the presenter lock is only surrendered when the
// holder has no connections left — closing one of two tabs must not evict
// the person or drop their lock.
func (r *Room) leave(name, clientID string) {
	delete(r.conns, presenceKey(name, clientID))
	if name != "" && r.hasConns(name) {
		return
	}
	if r.presenter == name {
		r.presenter = ""
	}
}

// hasConns reports whether any live connection belongs to name.
func (r *Room) hasConns(name string) bool {
	for _, c := range r.conns {
		if c.name == name {
			return true
		}
	}
	return false
}

// prune drops connections (and a presenter claim with no connection left)
// idle past the expiry. Lazy on read: no timers, no scheduler.
func (r *Room) prune() {
	now := time.Now()
	for key, c := range r.conns {
		if now.Sub(c.seen) > r.expiry {
			delete(r.conns, key)
		}
	}
	if r.presenter != "" && !r.hasConns(r.presenter) {
		r.presenter = ""
	}
}

func (r *Room) roster() Roster {
	r.prune()
	out := Roster{Generation: r.generation, Presenter: r.presenter}
	// One Member per NAME: fold every connection of a person into the
	// freshest sighting of them.
	seenByName := make(map[string]time.Time, len(r.conns))
	for _, c := range r.conns {
		if prev, ok := seenByName[c.name]; !ok || c.seen.After(prev) {
			seenByName[c.name] = c.seen
		}
	}
	out.Members = make([]Member, 0, len(seenByName))
	for name, seen := range seenByName {
		out.Members = append(out.Members, Member{Name: name, LastSeen: seen})
	}
	// Go map iteration is randomized per range, so an unsorted roster
	// reshuffled on every read (and every list flickered between two
	// orders). Order by name: stable across reads and instances, and
	// independent of join order, which is what a roster should look like.
	sort.Slice(out.Members, func(i, j int) bool { return out.Members[i].Name < out.Members[j].Name })
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
// state here (HTTP handlers resolve PIDs through it), hence the mutex
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

// SanitizeRoomID keeps actor names to stable path segments. Exported
// because the SSE fan-out tag must equal the actor name: the grain
// sanitizes internally, so a handler pushing to the RAW room id would
// broadcast on a tag no stream ever registered — every event landing
// nowhere while looking healthy.
func SanitizeRoomID(id string) string { return sanitizeRoomID(id) }

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
