// SCOPE:feature - REMOVE if not using the room demo (goakt unit).
package router

import (
	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/room"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
)

// registerRoomStack wires the room demo: one grain per room behind
// plain HTTP routes. host comes from cmd/web (nil when GOAKT_ENABLED
// is false); the guard lives here, not at the call site, so trimming
// is dropping one call line in router.go — see the unit's extraDrops.
func registerRoomStack(se *core.ServeEvent, cfg *config.Config) {
	host := appgoakt.Default()
	if !cfg.GoAkt.Enabled || host == nil {
		return
	}
	room.New(cfg, host).RegisterRoutes(se)
}
