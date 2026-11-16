// GoAkt lifecycle (room actor system)
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

// startGoAkt adopts the room actor system the recipe already booted.
//
// WHY ADOPT, NOT START. The room recipe builds and starts its host during
// BootChi (features/room/handlers/register.go) and publishes it as the process
// default, because it is the thing that serves the routes. Booting a SECOND
// host here gave the process two actor systems with the same name: the presence
// upgrade captured the first, this function then overwrote the default with the
// second, and shutdown stopped only the second — leaking the first. The failure
// is invisible until the discarded system is the one holding a live grain.
//
// A tree where the room recipe is trimmed registers nothing, so Start is still
// the fallback: a host here exists exactly once, whichever path created it.
func startGoAkt(ctx context.Context, cfg *config.Config) {
	if !cfg.GoAkt.Enabled {
		return
	}
	if goAktHost != nil {
		return
	}
	// The room recipe's host wins: it is the one the routes were built against.
	if existing := appgoakt.Default(); existing != nil {
		goAktHost = existing
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
