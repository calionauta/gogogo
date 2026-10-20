// SCOPE:core — SSE Hub ephemeral cross-instance bridge (chat, genui, progress)
package queue

import (
	"encoding/json"
	"log/slog"

	natsio "github.com/nats-io/nats.go"
)

// EphemeralSubject is the core-NATS subject (no JetStream: ephemeral means
// losable) that carries the hub's ephemeral signals between instances.
// Record changes have their own wire (APP_CRUD) and collaborative docs their
// own (app.sync.<doc>); those MUST NOT ride this subject or they would be
// delivered twice.
const EphemeralSubject = "app.ephemeral"

// ephemeralEnvelope is one hub event on the wire. ClientID empty means
// broadcast to every local tab; otherwise only that tab (a no-op on instances
// that do not hold it).
type ephemeralEnvelope struct {
	ClientID string `json:"client,omitempty"`
	Data     []byte `json:"data"`
}

type ephemeralBridge struct {
	nc  *natsio.Conn
	sub *natsio.Subscription
}

// AttachEphemeral makes Send/Broadcast cross instances. Producers publish once
// and every instance (including the publisher's own) delivers through its
// local hub — one path, so no origin tag or echo filter is needed.
//
// Only Send and Broadcast are bridged. BroadcastToUser/BroadcastToAllUsers
// (record events) and the doc-scoped methods already have their own
// cross-instance wires; bridging them here would duplicate every record and
// every collab update.
//
// A nil connection (NATS disabled) leaves the hub local, unchanged.
func (h *SSEHub) AttachEphemeral(nc *natsio.Conn) error {
	if nc == nil {
		return nil
	}
	h.mu.Lock()
	already := h.ephemeral != nil
	h.mu.Unlock()
	if already {
		return nil
	}
	sub, err := nc.Subscribe(EphemeralSubject, func(m *natsio.Msg) {
		var env ephemeralEnvelope
		if uerr := json.Unmarshal(m.Data, &env); uerr != nil {
			slog.Warn("ssehub: ephemeral envelope decode", "error", uerr)
			return
		}
		h.deliverLocal(env.ClientID, env.Data)
	})
	if err != nil {
		return err
	}
	// Flush the SUB before returning: without it a publish immediately after
	// attach can race the server's interest registration and be dropped (the
	// mixed-instance tests caught this as a flaky "B never received").
	if err := nc.Flush(); err != nil {
		_ = sub.Unsubscribe()
		return err
	}
	h.mu.Lock()
	h.ephemeral = &ephemeralBridge{nc: nc, sub: sub}
	h.mu.Unlock()
	return nil
}

// publishEphemeral sends one event on the wire. It reports whether the event
// was handed to NATS; a publish failure falls back to local delivery so the
// tabs on this instance never miss a signal because the bus is down.
func (h *SSEHub) publishEphemeral(clientID string, data []byte) bool {
	h.mu.RLock()
	b := h.ephemeral
	h.mu.RUnlock()
	if b == nil {
		return false
	}
	payload, err := json.Marshal(ephemeralEnvelope{ClientID: clientID, Data: data})
	if err != nil {
		return false
	}
	if pubErr := b.nc.Publish(EphemeralSubject, payload); pubErr != nil {
		slog.Warn("ssehub: ephemeral publish failed, delivering locally", "error", pubErr)
		return false
	}
	return true
}

// deliverLocal routes a decoded envelope into the local hub: one tab when
// clientID names it, every connected tab otherwise.
//
// A targeted envelope is DROPPED when this instance does not hold the tab. The
// normal Send path buffers for a reconnecting tab, which is right for a local
// producer (the tab will come back HERE) but wrong for a bridged one: the tab
// lives on another instance, so buffering would hand it a stale answer on a
// later reconnect — and grow a buffer keyed by a client this instance never
// had.
func (h *SSEHub) deliverLocal(clientID string, data []byte) {
	if clientID == "" {
		h.broadcastLocal(data)
		return
	}
	h.mu.RLock()
	ch, registered := h.clients[clientID]
	h.mu.RUnlock()
	if !registered {
		return
	}
	select {
	case ch <- data:
	default:
		h.onDrop(clientID, data, "slow-client-bridged")
	}
}
