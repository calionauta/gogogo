package main

import (
	"fmt"
	"log/slog"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/nats"
)

// startNATS boots the embedded NATS server (when NATS is enabled) and
// returns a JetStreamContext wired to it, or nil if NATS is disabled or
// failed to start (the caller falls back to the in-memory broadcaster).
//
// Single-NATS convention: the DagNats engine already owns the embedded
// NATS on the conventional port (127.0.0.1:4222) and boots before this
// function runs (the unified build always compiles it; it just no-ops
// when DAGNATS_ENABLED=false). In that case we connect to the
// existing server instead of starting a second one — one NATS, two
// consumers (DagNats workflows + the realtime broadcaster).
// startNATS returns the connection and the JetStream handle: the connection
// bridges ephemeral hub signals across instances, the handle carries the CRUD
// wire. Both nil when NATS is off or failed to start.
func startNATS(cfg *config.Config) (*natsio.Conn, nats.JetStreamLike) {
	if !cfg.NATS.Enabled {
		return nil, nil
	}
	if cfg.DagNats.Enabled {
		addr := fmt.Sprintf("127.0.0.1:%d", cfg.DagNats.NATSPort)
		h, err := nats.ConnectExisting(addr)
		if err != nil {
			slog.Warn("NATS connect to DagNats-owned server failed, falling back to in-memory broadcaster", "error", err)
			return nil, nil
		}
		return h.Conn, h.JS
	}
	h, err := nats.StartEmbedded(cfg.NATS.StoreDir)
	if err != nil {
		// Don't take the whole app down if embedded NATS can't start
		// (e.g. a read-only or full store dir). Fall back to the
		// in-memory broadcaster so realtime still works within the
		// instance.
		slog.Warn("NATS startup failed, falling back to in-memory broadcaster", "error", err)
		return nil, nil
	}
	return h.Conn, h.JS
}
