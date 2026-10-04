package main

import (
	"fmt"
	"log"

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
func startNATS(cfg *config.Config) nats.JetStreamLike {
	if !cfg.NATS.Enabled {
		return nil
	}
	if cfg.DagNats.Enabled {
		addr := fmt.Sprintf("127.0.0.1:%d", cfg.DagNats.NATSPort)
		if err := nats.ConnectExisting(addr); err != nil {
			log.Printf("WARN: NATS connect to DagNats-owned server failed, falling back to in-memory broadcaster: %v", err)
			return nil
		}
		return nats.JetStream()
	}
	if err := nats.StartEmbedded(cfg.NATS.StoreDir); err != nil {
		// Don't take the whole app down if embedded NATS can't start
		// (e.g. a read-only or full store dir). Fall back to the
		// in-memory broadcaster so realtime still works within the
		// instance.
		log.Printf("WARN: NATS startup failed, falling back to in-memory broadcaster: %v", err)
		return nil
	}
	js := nats.JetStream()
	return js
}
