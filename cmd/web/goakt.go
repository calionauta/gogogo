// SCOPE:layer=infra,removal=plugin — GoAkt lifecycle (room actor system)
package main

import (
	"context"
	"log/slog"

	"github.com/calionauta/gogogo/config"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
)

// goAktHost is the process-wide room actor host. Standalone mode: no
// network, no discovery, no cluster — rooms live in this process only.
var goAktHost *appgoakt.Host

// startGoAkt boots the room actor system. It no-ops when GOAKT_ENABLED
// is false, and it is idempotent: a second call while a host is live
// returns without spawning a second system (which would leak the first).
// The room routes guard the same flag, so a disabled engine can never
// be reached.
func startGoAkt(ctx context.Context, cfg *config.Config) {
	if !cfg.GoAkt.Enabled {
		return
	}
	if goAktHost != nil {
		return
	}
	h := appgoakt.New()
	if err := h.Start(ctx); err != nil {
		slog.Warn("goakt: actor system failed to start, room demo disabled", "error", err)
		return
	}
	goAktHost = h
	appgoakt.SetDefault(h)
}

// shutdownGoAkt stops the actor system. Nil-safe: a boot that never ran
// (or failed) leaves nothing to stop.
func shutdownGoAkt() {
	h := goAktHost
	goAktHost = nil
	appgoakt.SetDefault(nil)
	if h != nil {
		if err := h.Stop(context.Background()); err != nil {
			slog.Warn("goakt: actor system stop failed", "error", err)
		}
	}
}
